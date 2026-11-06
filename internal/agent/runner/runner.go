package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	resourceruntime "github.com/opensoha/soha-contracts/resource/runtime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"go.uber.org/zap"
)

const (
	commandHeartbeatInterval = 10 * time.Second
	runnerStatusPollInterval = 2 * time.Second
	metricScopeExecution     = "execution"
	metricScopeDocker        = "docker"
	metricScopeAgentRuntime  = "agent_runtime"
)

var agentRunHeartbeatInterval = commandHeartbeatInterval

type ExecutionTask = sohaapi.ExecutionTask

type DockerOperation = sohaapi.DockerOperation

type AgentRun = sohaapi.AgentRun

func agentRunWithPrefetchedToolContext(run AgentRun, items []AgentToolCallResult) AgentRun {
	if len(items) == 0 {
		return run
	}
	input := copyStringAnyMap(run.Input)
	input["prefetchedToolResults"] = agentToolCallResultMaps(items)
	input["toolContextRule"] = "Use prefetchedToolResults as soha-controlled read-only evidence. Do not call soha data sources directly."
	run.Input = input
	return run
}

type claimRequest = sohaapi.ExecutionTaskClaimRequest

type agentRunClaimRequest = sohaapi.AgentRunClaimRequest

type dockerClaimRequest = sohaapi.DockerOperationClaimRequest

type claimResponse = sohaapi.ExecutionTaskEnvelope

type dockerClaimResponse = sohaapi.DockerOperationEnvelope

type agentRunClaimResponse = sohaapi.AgentRunEnvelope

type callbackRequest = sohaapi.ExecutionCallbackRequest

type agentRunCallbackRequest = sohaapi.AgentRunCallbackRequest

type agentRunToolCallRequest = sohaapi.AgentRunToolCallRequest

type dockerCallbackRequest = sohaapi.DockerOperationCallbackRequest

type callbackResponse = sohaapi.ExecutionTaskEnvelope

type dockerCallbackResponse = sohaapi.DockerOperationEnvelope

type agentRunCallbackResponse = sohaapi.AgentRunEnvelope

type AgentToolCallResult = sohaapi.AgentToolCallResult

type agentRunToolCallResponse = sohaapi.AgentToolCallResultEnvelope

type workspaceSpec struct {
	Path          string
	CommandDir    string
	ArtifactFiles []string
	Checkout      checkoutSpec
}

type checkoutSpec struct {
	Enabled        bool
	RepositoryURL  string
	RepositoryPath string
	RefType        string
	RefName        string
	DefaultBranch  string
}

type Runner struct {
	cfg                 cfgpkg.ControlPlaneConfig
	httpClient          *http.Client
	logger              *zap.Logger
	executionSlots      chan struct{}
	mu                  sync.RWMutex
	active              map[string]*activeTaskState
	metrics             *runnerMetrics
	providerRegistry    *DynamicAgentProviderRegistry
	providerConformance AgentProviderConformanceProbe
	outpost             *outpostRuntime
	helmExecutor        HelmExecutor
	helmClusterID       string
	helmClaimToken      string
	helmAllowedActions  []string
	manifestExecutor    ManifestExecutor
	manifestClusterID   string
	sshClusterID        string
	chatToolGrants      sync.Map
	buildpacksBlocked   atomic.Bool
}

type ManifestExecutor interface {
	ExecuteManifestTask(context.Context, sohaapi.ManifestExecutionTaskPayload) (sohaapi.ManifestExecutionTaskResult, error)
}

type ActiveTask struct {
	TaskID                   string    `json:"taskId"`
	ApplicationID            string    `json:"applicationId"`
	ApplicationEnvironmentID string    `json:"applicationEnvironmentId,omitempty"`
	TaskKind                 string    `json:"taskKind"`
	ProviderKind             string    `json:"providerKind"`
	Status                   string    `json:"status"`
	CurrentCommand           string    `json:"currentCommand,omitempty"`
	CommandIndex             int       `json:"commandIndex,omitempty"`
	CommandCount             int       `json:"commandCount,omitempty"`
	WorkspacePath            string    `json:"workspacePath,omitempty"`
	StartedAt                time.Time `json:"startedAt"`
	UpdatedAt                time.Time `json:"updatedAt"`
	StopSource               string    `json:"stopSource,omitempty"`
	StopReason               string    `json:"stopReason,omitempty"`
}

type activeTaskState struct {
	snapshot ActiveTask
	cancel   context.CancelFunc
}

type MetricsSnapshot struct {
	ActiveTasks  int                  `json:"activeTasks"`
	Execution    RunnerMetricSnapshot `json:"execution"`
	Docker       RunnerMetricSnapshot `json:"docker"`
	AgentRuntime RunnerMetricSnapshot `json:"agentRuntime"`
}

type RunnerMetricSnapshot struct {
	Claims           int64  `json:"claims"`
	ClaimMisses      int64  `json:"claimMisses"`
	Started          int64  `json:"started"`
	Heartbeats       int64  `json:"heartbeats"`
	CallbackAttempts int64  `json:"callbackAttempts"`
	CallbackFailures int64  `json:"callbackFailures"`
	FinalCallbacks   int64  `json:"finalCallbacks"`
	Completed        int64  `json:"completed"`
	Failed           int64  `json:"failed"`
	Canceled         int64  `json:"canceled"`
	TimedOut         int64  `json:"timedOut"`
	Denied           int64  `json:"denied"`
	LastHeartbeatAt  string `json:"lastHeartbeatAt,omitempty"`
	LastCallbackAt   string `json:"lastCallbackAt,omitempty"`
}

type runnerMetrics struct {
	mu           sync.RWMutex
	execution    runnerMetricSet
	docker       runnerMetricSet
	agentRuntime runnerMetricSet
}

type runnerMetricSet struct {
	claims           int64
	claimMisses      int64
	started          int64
	heartbeats       int64
	callbackAttempts int64
	callbackFailures int64
	finalCallbacks   int64
	completed        int64
	failed           int64
	canceled         int64
	timedOut         int64
	denied           int64
	lastHeartbeatAt  string
	lastCallbackAt   string
}

func New(cfg cfgpkg.ControlPlaneConfig, logger *zap.Logger) *Runner {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5 * time.Second
	}
	if cfg.MaxConcurrency <= 0 {
		cfg.MaxConcurrency = 1
	}
	if cfg.DefaultTimeout <= 0 {
		cfg.DefaultTimeout = 30 * time.Minute
	}
	if cfg.CallbackRetry.MaxAttempts <= 0 {
		cfg.CallbackRetry.MaxAttempts = 3
	}
	if cfg.CallbackRetry.Backoff <= 0 {
		cfg.CallbackRetry.Backoff = 500 * time.Millisecond
	}
	if logger == nil {
		logger = zap.NewNop()
	}
	logger = logger.Named("runner")
	providerRegistry, err := NewDynamicAgentProviderRegistry(DefaultAgentProviderRegistry())
	if err != nil {
		panic(fmt.Sprintf("invalid built-in agent provider registry: %v", err))
	}
	runner := &Runner{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		logger:           logger,
		executionSlots:   make(chan struct{}, cfg.MaxConcurrency),
		active:           map[string]*activeTaskState{},
		metrics:          &runnerMetrics{},
		providerRegistry: providerRegistry,
	}
	runner.providerConformance = runnerProviderProbe{runner: runner}
	return runner
}

func (r *Runner) SetManifestExecutor(executor ManifestExecutor, clusterID string) {
	r.manifestExecutor = executor
	r.manifestClusterID = strings.TrimSpace(clusterID)
}

func (r *Runner) SetSSHExecutor(clusterID string, enabled bool) {
	if enabled {
		r.sshClusterID = strings.TrimSpace(clusterID)
	}
}

func (r *Runner) ApplyAgentProviderRegistrySnapshot(snapshot AgentProviderRegistry, now time.Time) AgentProviderRegistryApplyResult {
	if r == nil || r.providerRegistry == nil {
		return AgentProviderRegistryApplyResult{Revision: snapshot.Revision, Reason: "provider registry is unavailable", ObservedAt: now.UTC()}
	}
	identity := AgentFleetIdentity{
		RunnerID: agentRuntimeWorkerID(r.cfg), Environment: r.cfg.AgentRuntime.Environment,
		Platform: runtime.GOOS, Architecture: runtime.GOARCH, Labels: r.cfg.AgentRuntime.Labels,
	}
	return r.providerRegistry.ApplyDesired(
		context.Background(), snapshot, identity, r.providerConformance,
		r.cfg.AgentRuntime.ConformanceTimeout, now,
	)
}

func (r *Runner) applyAgentProviderRegistrySnapshot(
	ctx context.Context, snapshot AgentProviderRegistry, now time.Time,
) AgentProviderRegistryApplyResult {
	if r == nil || r.providerRegistry == nil {
		return AgentProviderRegistryApplyResult{Revision: snapshot.Revision, DesiredRevision: snapshot.Revision, RolloutState: "rejected", Reason: "provider registry is unavailable", ObservedAt: now.UTC()}
	}
	identity := AgentFleetIdentity{
		RunnerID: agentRuntimeWorkerID(r.cfg), Environment: r.cfg.AgentRuntime.Environment,
		Platform: runtime.GOOS, Architecture: runtime.GOARCH, Labels: r.cfg.AgentRuntime.Labels,
	}
	return r.providerRegistry.ApplyDesired(
		ctx, snapshot, identity, r.providerConformance,
		r.cfg.AgentRuntime.ConformanceTimeout, now,
	)
}

func (r *Runner) SetAgentProviderConformanceProbe(probe AgentProviderConformanceProbe) {
	if r != nil {
		r.providerConformance = probe
	}
}

func (r *Runner) AgentProviderRuntimeStatuses() []AgentProviderRuntimeStatus {
	if r == nil || r.providerRegistry == nil {
		return []AgentProviderRuntimeStatus{}
	}
	return r.providerRegistry.Statuses()
}

func (r *Runner) apiClient() *sohaapi.Client {
	return sohaapi.NewClient(
		r.cfg.BaseURL,
		sohaapi.WithBearerToken(r.cfg.BearerToken),
		sohaapi.WithHTTPClient(r.httpClient),
	)
}

func (r *Runner) tryAcquireExecutionSlot() bool {
	if r == nil || r.executionSlots == nil {
		return true
	}
	select {
	case r.executionSlots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (r *Runner) releaseExecutionSlot() {
	if r == nil || r.executionSlots == nil {
		return
	}
	select {
	case <-r.executionSlots:
	default:
	}
}

func (r *Runner) MetricsSnapshot() MetricsSnapshot {
	activeTasks := 0
	if r != nil {
		r.mu.RLock()
		activeTasks = len(r.active)
		r.mu.RUnlock()
	}
	if r == nil || r.metrics == nil {
		return MetricsSnapshot{ActiveTasks: activeTasks}
	}
	return r.metrics.snapshot(activeTasks)
}

func (m *runnerMetrics) snapshot(activeTasks int) MetricsSnapshot {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return MetricsSnapshot{
		ActiveTasks:  activeTasks,
		Execution:    m.execution.snapshot(),
		Docker:       m.docker.snapshot(),
		AgentRuntime: m.agentRuntime.snapshot(),
	}
}

func (m *runnerMetrics) markClaim(scope string, hit bool) {
	m.withScope(scope, func(current *runnerMetricSet) {
		if hit {
			current.claims++
			return
		}
		current.claimMisses++
	})
}

func (m *runnerMetrics) markStarted(scope string) {
	m.withScope(scope, func(current *runnerMetricSet) {
		current.started++
	})
}

func (m *runnerMetrics) markHeartbeat(scope string) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	m.withScope(scope, func(current *runnerMetricSet) {
		current.heartbeats++
		current.lastHeartbeatAt = now
	})
}

func (m *runnerMetrics) markCallbackAttempt(scope string) {
	m.withScope(scope, func(current *runnerMetricSet) {
		current.callbackAttempts++
	})
}

func (m *runnerMetrics) markCallbackFailure(scope string) {
	m.withScope(scope, func(current *runnerMetricSet) {
		current.callbackFailures++
	})
}

func (m *runnerMetrics) markCallbackSuccess(scope, status string) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	m.withScope(scope, func(current *runnerMetricSet) {
		current.lastCallbackAt = now
		if strings.TrimSpace(status) == "running" {
			current.heartbeats++
			current.lastHeartbeatAt = now
		}
		if isFinalStatus(status) {
			current.finalCallbacks++
		}
	})
}

func (m *runnerMetrics) markOutcome(scope, status string) {
	m.withScope(scope, func(current *runnerMetricSet) {
		switch strings.TrimSpace(status) {
		case "completed":
			current.completed++
		case "failed":
			current.failed++
		case "canceled":
			current.canceled++
		case "callback_timeout":
			current.timedOut++
		case "denied":
			current.denied++
		}
	})
}

func (m *runnerMetrics) withScope(scope string, mutate func(*runnerMetricSet)) {
	if m == nil || mutate == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	switch scope {
	case metricScopeDocker:
		mutate(&m.docker)
	case metricScopeAgentRuntime:
		mutate(&m.agentRuntime)
	default:
		mutate(&m.execution)
	}
}

func (s runnerMetricSet) snapshot() RunnerMetricSnapshot {
	return RunnerMetricSnapshot{
		Claims:           s.claims,
		ClaimMisses:      s.claimMisses,
		Started:          s.started,
		Heartbeats:       s.heartbeats,
		CallbackAttempts: s.callbackAttempts,
		CallbackFailures: s.callbackFailures,
		FinalCallbacks:   s.finalCallbacks,
		Completed:        s.completed,
		Failed:           s.failed,
		Canceled:         s.canceled,
		TimedOut:         s.timedOut,
		Denied:           s.denied,
		LastHeartbeatAt:  s.lastHeartbeatAt,
		LastCallbackAt:   s.lastCallbackAt,
	}
}

func (r *Runner) Start(ctx context.Context) {
	if !r.cfg.Enabled || strings.TrimSpace(r.cfg.BaseURL) == "" || strings.TrimSpace(r.cfg.BearerToken) == "" {
		return
	}
	if len(r.cfg.ProviderKinds) > 0 || r.cfg.Buildpacks.Enabled || r.sshClusterID != "" {
		go r.loop(ctx)
	}
	if r.cfg.Docker.Enabled {
		go r.dockerLoop(ctx)
	}
	if r.cfg.AgentRuntime.Enabled {
		go r.agentProviderRegistryLoop(ctx)
		go r.agentRuntimeLoop(ctx)
	}
	r.startOutpost(ctx)
}

func (r *Runner) loop(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !r.tryAcquireExecutionSlot() {
				continue
			}
			task, ok := r.claim(ctx)
			if !ok {
				r.releaseExecutionSlot()
				continue
			}
			go func(current ExecutionTask) {
				defer r.releaseExecutionSlot()
				r.executeClaimedTask(ctx, current)
			}(task)
		}
	}
}

func (r *Runner) dockerLoop(ctx context.Context) {
	interval := r.cfg.Docker.PollInterval
	if interval <= 0 {
		interval = r.cfg.PollInterval
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			operation, ok := r.claimDockerOperation(ctx)
			if !ok {
				continue
			}
			r.executeDockerOperation(ctx, operation)
		}
	}
}

func (r *Runner) agentRuntimeLoop(ctx context.Context) {
	interval := r.cfg.AgentRuntime.PollInterval
	if interval <= 0 {
		interval = r.cfg.PollInterval
	}
	if interval <= 0 {
		interval = 5 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !r.tryAcquireExecutionSlot() {
				continue
			}
			run, ok := r.claimAgentRun(ctx)
			if !ok {
				r.releaseExecutionSlot()
				continue
			}
			go func(current AgentRun) {
				defer r.releaseExecutionSlot()
				r.executeAgentRun(ctx, current)
			}(run)
		}
	}
}

func (r *Runner) claim(ctx context.Context) (ExecutionTask, bool) {
	if r.cfg.Buildpacks.Enabled && r.buildpacksBlocked.Load() {
		return ExecutionTask{}, false
	}
	if task, ok := r.claimHelmTask(ctx); ok {
		return task, true
	}
	providerKinds := append([]string(nil), r.cfg.ProviderKinds...)
	if r.cfg.Buildpacks.Enabled {
		providerKinds = []string{"buildpacks_runner." + r.cfg.AgentID}
	}
	if r.sshClusterID != "" {
		providerKinds = append(providerKinds, "vm_ssh."+r.sshClusterID)
	}
	if r.manifestExecutor != nil && r.manifestClusterID != "" {
		providerKinds = append(providerKinds, "manifest_agent."+r.manifestClusterID, resourceruntime.ManifestAgentProviderPrefix+r.manifestClusterID)
	}
	task, err := r.apiClient().ClaimExecutionTask(ctx, claimRequest{
		AgentID:         firstNonEmpty(strings.TrimSpace(r.cfg.AgentID), "local-agent"),
		ProviderKinds:   providerKinds,
		RuntimeEndpoint: strings.TrimSpace(r.cfg.RuntimeEndpoint),
	})
	if err != nil {
		r.metrics.markClaim(metricScopeExecution, false)
		return ExecutionTask{}, false
	}
	if strings.TrimSpace(task.ID) == "" {
		r.metrics.markClaim(metricScopeExecution, false)
		return ExecutionTask{}, false
	}
	r.metrics.markClaim(metricScopeExecution, true)
	return task, true
}

func (r *Runner) claimAgentRun(ctx context.Context) (AgentRun, bool) {
	run, err := r.apiClient().ClaimAgentRun(ctx, agentRunClaimRequest{
		AgentID:     agentRuntimeWorkerID(r.cfg),
		ProviderIDs: r.cfg.AgentRuntime.ProviderIDs,
		Kinds:       r.cfg.AgentRuntime.ProviderKinds,
	})
	if err != nil {
		r.metrics.markClaim(metricScopeAgentRuntime, false)
		return AgentRun{}, false
	}
	if strings.TrimSpace(run.ID) == "" {
		r.metrics.markClaim(metricScopeAgentRuntime, false)
		return AgentRun{}, false
	}
	r.metrics.markClaim(metricScopeAgentRuntime, true)
	return run, true
}

func (r *Runner) claimDockerOperation(ctx context.Context) (DockerOperation, bool) {
	workerID := dockerWorkerID(r.cfg)
	operationKinds := normalizeDockerOperationKinds(r.cfg.Docker.OperationKinds)
	if len(operationKinds) == 0 {
		r.metrics.markClaim(metricScopeDocker, false)
		return DockerOperation{}, false
	}
	operation, err := r.apiClient().ClaimDockerOperation(ctx, dockerClaimRequest{
		WorkerID:               workerID,
		AgentID:                firstNonEmpty(strings.TrimSpace(r.cfg.AgentID), "local-agent"),
		HostIDs:                r.cfg.Docker.HostIDs,
		OperationKinds:         operationKinds,
		CallbackTokenSupported: true,
		RuntimeProtocols:       supportedDockerRuntimeProtocols(),
	})
	if err != nil {
		r.metrics.markClaim(metricScopeDocker, false)
		return DockerOperation{}, false
	}
	if strings.TrimSpace(operation.ID) == "" {
		r.metrics.markClaim(metricScopeDocker, false)
		return DockerOperation{}, false
	}
	r.metrics.markClaim(metricScopeDocker, true)
	return operation, true
}

func (r *Runner) execute(ctx context.Context, task ExecutionTask) {
	if task.TaskKind == "vm_ssh_command" {
		r.executeSSHCommand(ctx, task)
		return
	}
	if strings.HasPrefix(task.TaskKind, "helm_") {
		taskCtx, cancel := r.executionTaskContext(ctx, task)
		defer cancel()
		r.metrics.markStarted(metricScopeExecution)
		r.executeHelmTask(taskCtx, task)
		return
	}
	agentID := firstNonEmpty(strings.TrimSpace(r.cfg.AgentID), "local-agent")
	r.metrics.markStarted(metricScopeExecution)
	secretCtx, err := r.redeemSecretLease(ctx, task.SecretLease, agentID)
	if err != nil {
		r.finalCallback(ctx, task, "failed", map[string]any{"error": err.Error(), "logs": []string{err.Error()}, "agentId": agentID})
		r.metrics.markOutcome(metricScopeExecution, "failed")
		return
	}
	task.SecretLease = nil
	ctx = secretCtx
	taskCtx, cancelTask := r.executionTaskContext(ctx, task)
	defer cancelTask()
	if strings.HasPrefix(task.TaskKind, "manifest_") {
		r.executeManifestTask(taskCtx, task)
		return
	}
	commands := extractCommands(task.Payload)
	if len(commands) == 0 {
		r.finalCallback(ctx, task, "failed", map[string]any{
			"logs":  []string{"no executable commands were found in task payload"},
			"error": "no executable commands were found in task payload",
		})
		r.metrics.markOutcome(metricScopeExecution, "failed")
		return
	}

	commandCount := len(commands)
	r.registerActiveTask(task, cancelTask)
	defer r.unregisterActiveTask(task.ID)
	r.updateActiveTask(task.ID, func(item *ActiveTask) {
		item.Status = "preparing"
	})

	workspacePath, commandDir, workspaceArtifacts, workspaceLogs, workspaceErr := r.prepareWorkspace(taskCtx, task)
	if len(workspaceLogs) > 0 {
		r.updateActiveTask(task.ID, func(item *ActiveTask) {
			item.Status = "running"
			item.WorkspacePath = workspacePath
		})
		remoteTask, ok := r.callback(taskCtx, task, "running", map[string]any{
			"logs":          workspaceLogs,
			"agentId":       agentID,
			"workspacePath": workspacePath,
			"heartbeatAt":   time.Now().UTC().Format(time.RFC3339),
		})
		if ok && shouldStopLocalExecution(remoteTask.Status) {
			return
		}
	}
	if workspaceErr != nil {
		status := "failed"
		errorMessage := workspaceErr.Error()
		payload := map[string]any{
			"logs":          []string{errorMessage},
			"error":         errorMessage,
			"failureStage":  workspaceFailureStage(workspaceErr),
			"agentId":       agentID,
			"workspacePath": workspacePath,
		}
		if errors.Is(taskCtx.Err(), context.DeadlineExceeded) {
			timeout := r.executionTaskTimeout(task)
			status = "callback_timeout"
			errorMessage = fmt.Sprintf("execution task timed out after %s", timeout)
			payload["logs"] = []string{errorMessage}
			payload["error"] = errorMessage
			payload["timeoutSeconds"] = ceilDurationSeconds(timeout)
			payload["timeout"] = timeout.String()
		}
		r.updateActiveTask(task.ID, func(item *ActiveTask) {
			item.Status = status
			item.WorkspacePath = workspacePath
			item.StopReason = errorMessage
		})
		r.finalCallback(ctx, task, status, payload)
		r.metrics.markOutcome(metricScopeExecution, status)
		return
	}

	for index, command := range commands {
		if !r.executeTaskCommand(ctx, taskCtx, task, command, commandDir, workspacePath, index, commandCount) {
			return
		}
	}

	payload := map[string]any{
		"agentId":       agentID,
		"completedAt":   time.Now().UTC().Format(time.RFC3339),
		"workspacePath": workspacePath,
	}
	if image := resolveImageFromCommands(task.Payload, commands); image != "" {
		payload["image"] = image
		if digest := imageDigestFromWorkspaceArtifacts(workspacePath, workspaceArtifacts); digest != "" {
			payload["imageDigest"] = digest
		}
		payload["artifact"] = buildImageArtifact(payload, image)
		payload["artifacts"] = buildArtifactList(payload, image)
	}
	if len(workspaceArtifacts) > 0 {
		payload["workspaceArtifacts"] = workspaceArtifacts
	}
	r.updateActiveTask(task.ID, func(item *ActiveTask) {
		item.Status = "completed"
		item.CurrentCommand = ""
		item.StopReason = ""
	})
	r.finalCallback(ctx, task, "completed", payload)
	r.metrics.markOutcome(metricScopeExecution, "completed")
}

func (r *Runner) executeTaskCommand(ctx, taskCtx context.Context, task ExecutionTask, command, commandDir, workspacePath string, index, commandCount int) bool {
	agentID := firstNonEmpty(strings.TrimSpace(r.cfg.AgentID), "local-agent")
	pipelineStage := commandPipelineStage(command)
	r.updateActiveTask(task.ID, func(item *ActiveTask) {
		item.Status = "running"
		item.CurrentCommand = command
		item.CommandIndex = index + 1
		item.CommandCount = commandCount
		item.WorkspacePath = workspacePath
	})
	remoteTask, ok := r.callback(taskCtx, task, "running", extendMap(
		buildHeartbeatPayload(agentID, command, index+1, commandCount),
		map[string]any{"workspacePath": workspacePath, "pipelineStage": pipelineStage},
	))
	if ok && shouldStopLocalExecution(remoteTask.Status) {
		return false
	}
	commandLogs := []string{"$ " + command}

	commandCtx, cancelCommand := context.WithCancel(taskCtx)
	// Execution tasks are claimed from the authenticated control plane. Do not
	// feed user-provided fragments into this shell command path without first
	// converting them to argv-style commands or a strict template allowlist.
	r.logger.Info("execution task command started",
		zap.String("task_id", task.ID),
		zap.String("task_kind", task.TaskKind),
		zap.String("provider_kind", task.ProviderKind),
		zap.String("agent_id", agentID),
		zap.String("command_source", "control_plane_claim"),
		zap.String("command_fingerprint", commandFingerprint(command)),
		zap.Int("command_index", index+1),
		zap.Int("command_count", commandCount),
		zap.String("workspace_path", workspacePath),
	)
	cmd := exec.CommandContext(commandCtx, "/bin/sh", "-lc", command)
	configureCommandCancellation(cmd)
	if environment := secretEnvironment(commandCtx); environment != nil {
		cmd.Env = environment
	}
	if commandDir != "" {
		cmd.Dir = commandDir
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	done := make(chan struct{})
	stopReason := make(chan string, 1)
	go r.streamHeartbeats(commandCtx, cancelCommand, done, stopReason, task, agentID, command, index+1, commandCount, workspacePath)
	go r.watchRunnerStatus(commandCtx, cancelCommand, done, stopReason, task)
	err := cmd.Run()
	close(done)
	cancelCommand()
	remoteStatus := drainStopReason(stopReason)

	if value := strings.TrimSpace(stdout.String()); value != "" {
		commandLogs = append(commandLogs, value)
	}
	if value := strings.TrimSpace(stderr.String()); value != "" {
		commandLogs = append(commandLogs, value)
	}
	remoteTask, ok = r.callback(taskCtx, task, "running", extendMap(
		buildHeartbeatPayload(agentID, command, index+1, commandCount),
		map[string]any{
			"logs":          commandLogs,
			"workspacePath": workspacePath,
			"pipelineStage": pipelineStage,
		},
	))
	if ok && shouldStopLocalExecution(remoteTask.Status) {
		return false
	}
	if remoteStatus != "" {
		stopSource, stopReason := r.stopInfo(task.ID)
		if stopSource == "local_api" {
			r.updateActiveTask(task.ID, func(item *ActiveTask) {
				item.Status = "canceled"
				item.StopSource = stopSource
				item.StopReason = stopReason
			})
			r.finalCallback(ctx, task, "canceled", map[string]any{
				"agentId":       agentID,
				"workspacePath": workspacePath,
				"canceledAt":    time.Now().UTC().Format(time.RFC3339),
				"cancelReason":  stopReason,
			})
			r.metrics.markOutcome(metricScopeExecution, "canceled")
		}
		return false
	}
	if errors.Is(taskCtx.Err(), context.Canceled) {
		stopSource, stopReason := r.stopInfo(task.ID)
		if stopSource == "local_api" {
			r.updateActiveTask(task.ID, func(item *ActiveTask) {
				item.Status = "canceled"
				item.StopSource = stopSource
				item.StopReason = stopReason
			})
			r.finalCallback(ctx, task, "canceled", map[string]any{
				"agentId":       agentID,
				"workspacePath": workspacePath,
				"canceledAt":    time.Now().UTC().Format(time.RFC3339),
				"cancelReason":  stopReason,
			})
			r.metrics.markOutcome(metricScopeExecution, "canceled")
			return false
		}
	}
	if errors.Is(taskCtx.Err(), context.DeadlineExceeded) {
		timeout := r.executionTaskTimeout(task)
		timeoutSeconds := ceilDurationSeconds(timeout)
		message := fmt.Sprintf("execution task timed out after %s", timeout)
		r.updateActiveTask(task.ID, func(item *ActiveTask) {
			item.Status = "callback_timeout"
			item.StopReason = message
		})
		r.finalCallback(ctx, task, "callback_timeout", map[string]any{
			"logs":           []string{message},
			"error":          message,
			"agentId":        agentID,
			"currentCommand": command,
			"workspacePath":  workspacePath,
			"timeoutSeconds": timeoutSeconds,
			"timeout":        timeout.String(),
		})
		r.metrics.markOutcome(metricScopeExecution, "callback_timeout")
		return false
	}
	if err != nil {
		if errors.Is(err, context.Canceled) && remoteStatus != "" {
			return false
		}
		r.updateActiveTask(task.ID, func(item *ActiveTask) {
			item.Status = "failed"
			item.StopReason = err.Error()
		})
		r.finalCallback(ctx, task, "failed", map[string]any{
			"logs":           []string{fmt.Sprintf("command failed: %v", err)},
			"error":          err.Error(),
			"agentId":        agentID,
			"currentCommand": command,
			"failureStage":   pipelineStage,
			"workspacePath":  workspacePath,
		})
		r.metrics.markOutcome(metricScopeExecution, "failed")
		return false
	}

	return true
}

func (r *Runner) executeDockerOperation(ctx context.Context, operation DockerOperation) {
	workerID := dockerWorkerID(r.cfg)
	r.metrics.markStarted(metricScopeDocker)
	if !dockerOperationKindAllowed(r.cfg.Docker.OperationKinds, operation.OperationKind) {
		err := fmt.Errorf("docker operation kind %q is not allowlisted", operation.OperationKind)
		r.logger.Warn("docker operation denied",
			zap.String("operation_id", operation.ID),
			zap.String("operation_kind", operation.OperationKind),
			zap.String("worker_id", workerID),
			zap.String("reason", err.Error()),
		)
		r.dockerCallback(ctx, operation, "failed", dockerRuntimePayload(ctx, r.cfg, map[string]any{
			"error":    err.Error(),
			"workerId": workerID,
		}), []string{"docker operation denied: " + err.Error()})
		r.metrics.markOutcome(metricScopeDocker, "denied")
		return
	}
	if err := validateDockerOperationPayload(operation); err != nil {
		r.dockerCallback(ctx, operation, "failed", dockerRuntimePayload(ctx, r.cfg, map[string]any{"error": err.Error(), "workerId": workerID}), []string{"docker operation payload rejected: " + err.Error()})
		r.metrics.markOutcome(metricScopeDocker, "denied")
		return
	}
	timeoutSeconds := operation.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 1800
	}
	taskCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()
	logs := []string{fmt.Sprintf("docker operation %s started: %s", operation.ID, operation.OperationKind)}
	remote, ok := r.dockerCallback(taskCtx, operation, "running", dockerRuntimePayload(taskCtx, r.cfg, map[string]any{
		"heartbeatAt": time.Now().UTC().Format(time.RFC3339),
	}), logs)
	if !ok || remote.ID != operation.ID || remote.Status != "running" && !shouldStopDockerExecution(remote.Status) {
		r.logger.Warn("Docker execution start was not confirmed by control plane", zap.String("operation_id", operation.ID))
		r.metrics.markOutcome(metricScopeDocker, "denied")
		return
	}
	if shouldStopDockerExecution(remote.Status) {
		r.acknowledgeDockerCancellation(ctx, operation, remote.Status)
		r.metrics.markOutcome(metricScopeDocker, strings.TrimSpace(remote.Status))
		return
	}

	done := make(chan struct{})
	stopReason := make(chan string, 1)
	go r.streamDockerHeartbeats(taskCtx, cancel, done, stopReason, operation)

	var err error
	var commandLogs []string
	switch operation.OperationKind {
	case "host_provision":
		commandLogs, err = r.validateDockerHostProvision(taskCtx)
	case "container_start", "project_deploy":
		commandLogs, err = r.executeComposeAction(taskCtx, operation)
	case "service_action":
		commandLogs, err = r.executeComposeServiceAction(taskCtx, operation)
	case "port_reserve":
		commandLogs = []string{"port mapping reserved in control plane; no local Docker command required"}
	case "host_sync":
		commandLogs = []string{"host runtime heartbeat reported by docker runner"}
	default:
		err = fmt.Errorf("unsupported docker operation kind %q", operation.OperationKind)
	}
	close(done)
	if remoteStatus := drainStopReason(stopReason); remoteStatus != "" {
		r.acknowledgeDockerCancellation(ctx, operation, remoteStatus)
		r.metrics.markOutcome(metricScopeDocker, remoteStatus)
		return
	}
	logs = append(logs, commandLogs...)
	if err != nil {
		status := "failed"
		if errors.Is(taskCtx.Err(), context.DeadlineExceeded) {
			status = "callback_timeout"
		}
		r.dockerCallback(ctx, operation, status, dockerRuntimePayload(ctx, r.cfg, map[string]any{
			"error":    err.Error(),
			"workerId": workerID,
		}), append(logs, "docker operation failed: "+err.Error()))
		r.metrics.markOutcome(metricScopeDocker, status)
		return
	}

	payload := dockerRuntimePayload(ctx, r.cfg, map[string]any{
		"completedAt": time.Now().UTC().Format(time.RFC3339),
	})
	if operation.Payload["action"] == "validate" {
		payload["validatedRenderedDigest"] = operation.Payload["renderedDigest"]
	}
	if operation.Payload["action"] == "delivery_deploy" {
		payload["appliedRenderedDigest"] = operation.Payload["renderedDigest"]
	}
	if operation.ProjectID != "" && operation.Payload["action"] != "validate" {
		services, serviceErr := r.collectComposeServices(ctx, operation)
		if serviceErr == nil && len(services) > 0 {
			payload["services"] = services
		}
		if _, bound, _ := dockerOperationBinding(operation); bound {
			payload["runtimeInventoryConfirmed"] = serviceErr == nil
			if serviceErr == nil {
				payload["services"] = services
			}
			if serviceErr != nil {
				payload["runtimeInventoryMessage"] = "Container identity observation is unconfirmed; no inventory adoption was performed"
			}
		}
	}
	r.dockerCallback(ctx, operation, "completed", payload, logs)
	r.metrics.markOutcome(metricScopeDocker, "completed")
}

func (r *Runner) validateDockerHostProvision(ctx context.Context) ([]string, error) {
	logs := []string{"validating docker runtime before marking host provision ready"}
	dockerInfo, err := runCommand(ctx, "", "docker", "info", "--format", "{{.ServerVersion}} {{.Architecture}}")
	logs = append(logs, dockerInfo...)
	if err != nil {
		return logs, fmt.Errorf("docker runtime unavailable: %w", err)
	}
	composeInfo, err := runCommand(ctx, "", "docker", "compose", "version")
	logs = append(logs, composeInfo...)
	if err != nil {
		return logs, fmt.Errorf("docker compose unavailable: %w", err)
	}
	logs = append(logs, "docker runtime validated")
	return logs, nil
}

func (r *Runner) streamDockerHeartbeats(ctx context.Context, cancel context.CancelFunc, done <-chan struct{}, stopReason chan<- string, operation DockerOperation) {
	ticker := time.NewTicker(commandHeartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			if remoteOperation, ok := r.dockerCallback(ctx, operation, "running", dockerRuntimePayload(ctx, r.cfg, map[string]any{
				"heartbeatAt": time.Now().UTC().Format(time.RFC3339),
			}), nil); ok && shouldStopDockerExecution(remoteOperation.Status) {
				select {
				case stopReason <- strings.TrimSpace(remoteOperation.Status):
				default:
				}
				cancel()
				return
			}
			if remoteOperation, ok := r.fetchDockerRunnerStatus(ctx, operation.ID); ok && shouldStopDockerExecution(remoteOperation.Status) {
				select {
				case stopReason <- strings.TrimSpace(remoteOperation.Status):
				default:
				}
				cancel()
				return
			}
		}
	}
}

func (r *Runner) executeAgentRun(ctx context.Context, run AgentRun) {
	workerID := agentRuntimeWorkerID(r.cfg)
	startedAt := time.Now().UTC()
	r.metrics.markStarted(metricScopeAgentRuntime)
	secretCtx, err := r.redeemSecretLease(ctx, run.SecretLease, workerID)
	if err != nil {
		r.agentRunCallback(ctx, run, "failed", map[string]any{"agentId": workerID, "workerId": workerID, "error": err.Error()}, nil, nil, "", err.Error())
		r.metrics.markOutcome(metricScopeAgentRuntime, "failed")
		return
	}
	run.SecretLease = nil
	ctx = secretCtx
	if remoteRun, ok := r.agentRunCallback(ctx, run, "running", map[string]any{
		"agentId":     firstNonEmpty(strings.TrimSpace(r.cfg.AgentID), "local-agent"),
		"workerId":    workerID,
		"heartbeatAt": startedAt.Format(time.RFC3339),
		"providerId":  run.ProviderID,
	}, nil, nil, "", ""); ok && shouldStopLocalExecution(remoteRun.Status) {
		r.acknowledgeAgentCancellation(ctx, run, remoteRun.Status)
		return
	}

	timeoutSeconds := run.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 600
	}
	taskCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutSeconds)*time.Second)
	defer cancel()

	done := make(chan struct{})
	stopReason := make(chan string, 1)
	go r.streamAgentRunHeartbeats(taskCtx, cancel, done, stopReason, run, workerID)

	executor := r.resolveAgentProviderExecutor(run)
	output, logs, err := executor(taskCtx, run)
	close(done)
	remoteStatus := drainStopReason(stopReason)
	if remoteStatus != "" {
		r.acknowledgeAgentCancellation(ctx, run, remoteStatus)
		return
	}
	if run.CapabilityID == "general" {
		r.finishAgentChatRun(ctx, taskCtx, run, output, err)
		return
	}
	if err != nil {
		status := "failed"
		errorMessage := err.Error()
		if errors.Is(taskCtx.Err(), context.DeadlineExceeded) {
			status = "callback_timeout"
			errorMessage = fmt.Sprintf("agent run timed out after %d seconds", timeoutSeconds)
		}
		safeErrorMessage := redactAgentRuntimeText(errorMessage)
		safeLogs := redactAgentRuntimeLogs(logs)
		safeOutput := redactAgentRuntimeValue(output).(map[string]any)
		completedAt := time.Now().UTC()
		toolExecution := agentRunToolExecution(run, startedAt, completedAt, status, safeErrorMessage, safeOutput)
		artifact := agentRunFailedArtifact(run, safeOutput, safeLogs, []map[string]any{toolExecution}, status, safeErrorMessage)
		r.agentRunCallback(ctx, run, status, map[string]any{
			"agentId":    firstNonEmpty(strings.TrimSpace(r.cfg.AgentID), "local-agent"),
			"workerId":   workerID,
			"logs":       safeLogs,
			"error":      safeErrorMessage,
			"providerId": run.ProviderID,
		}, []map[string]any{toolExecution}, []map[string]any{artifact}, "", safeErrorMessage)
		r.metrics.markOutcome(metricScopeAgentRuntime, status)
		return
	}
	completedAt := time.Now().UTC()
	toolExecution := agentRunToolExecution(run, startedAt, completedAt, "completed", agentRunCompletionSummary(run), output)
	artifact := agentRunArtifact(run, output, logs, []map[string]any{toolExecution})
	payload := map[string]any{
		"agentId":     firstNonEmpty(strings.TrimSpace(r.cfg.AgentID), "local-agent"),
		"workerId":    workerID,
		"providerId":  run.ProviderID,
		"completedAt": completedAt.Format(time.RFC3339),
		"summary":     firstNonEmpty(stringMapValue(output, "summary"), stringMapValue(output, "rawOutput")),
		"rawOutput":   stringMapValue(output, "rawOutput"),
		"logs":        logs,
	}
	r.agentRunCallback(ctx, run, "completed", payload, []map[string]any{toolExecution}, []map[string]any{artifact}, firstNonEmpty(stringMapValue(output, "externalRunId"), run.ID), "")
	r.metrics.markOutcome(metricScopeAgentRuntime, "completed")
}

func (r *Runner) streamAgentRunHeartbeats(ctx context.Context, cancel context.CancelFunc, done <-chan struct{}, stopReason chan<- string, run AgentRun, workerID string) {
	interval := agentRunHeartbeatInterval
	if interval <= 0 {
		interval = commandHeartbeatInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			remoteRun, ok := r.agentRunCallback(ctx, run, "running", map[string]any{
				"agentId":      firstNonEmpty(strings.TrimSpace(r.cfg.AgentID), "local-agent"),
				"workerId":     workerID,
				"heartbeatAt":  time.Now().UTC().Format(time.RFC3339),
				"providerId":   run.ProviderID,
				"capabilityId": run.CapabilityID,
			}, nil, nil, "", "")
			if ok && shouldStopLocalExecution(remoteRun.Status) {
				select {
				case stopReason <- strings.TrimSpace(remoteRun.Status):
				default:
				}
				cancel()
				return
			}
		}
	}
}

func (r *Runner) executeHermesAgentRun(ctx context.Context, run AgentRun) (map[string]any, []string, error) {
	return r.executeCLIAgentRun(ctx, run, r.agentProviderCommandSpec("hermes"))
}

type agentProviderExecutor func(context.Context, AgentRun) (map[string]any, []string, error)

type agentProviderCommandSpec = AgentProviderCommandSpec

func (r *Runner) resolveAgentProviderExecutor(run AgentRun) agentProviderExecutor {
	if run.CapabilityID == "general" {
		return r.executeChatProviderRun
	}
	providerKey := normalizedAgentProviderKey(run)
	var executor agentProviderExecutor
	switch providerKey {
	case "hermes":
		executor = r.executeHermesAgentRun
	default:
		spec := r.agentProviderCommandSpec(providerKey)
		if strings.TrimSpace(spec.Command) == "" {
			return func(context.Context, AgentRun) (map[string]any, []string, error) {
				return map[string]any{
					"provider":     run.ProviderID,
					"capabilityId": run.CapabilityID,
					"error":        fmt.Sprintf("agent provider %q is not configured on this runner", firstNonEmpty(run.ProviderID, run.ProviderKind)),
				}, nil, fmt.Errorf("agent provider %q is not configured on this runner", firstNonEmpty(run.ProviderID, run.ProviderKind))
			}
		}
		executor = func(ctx context.Context, current AgentRun) (map[string]any, []string, error) {
			return r.executeCLIAgentRun(ctx, current, spec)
		}
	}
	if r.providerRegistry == nil {
		return executor
	}
	if _, registered := r.providerRegistry.Resolve(providerKey, ""); !registered {
		return executor
	}
	return func(ctx context.Context, current AgentRun) (map[string]any, []string, error) {
		provider, err := r.providerRegistry.Acquire(providerKey, "")
		if err != nil {
			return map[string]any{"provider": providerKey, "error": err.Error()}, nil, err
		}
		defer r.providerRegistry.Release(provider.ID)
		return executor(ctx, current)
	}
}

func (r *Runner) executeCLIAgentRun(ctx context.Context, run AgentRun, spec agentProviderCommandSpec) (map[string]any, []string, error) {
	command := strings.TrimSpace(spec.Command)
	if command == "" {
		return map[string]any{
			"provider":     run.ProviderID,
			"capabilityId": run.CapabilityID,
			"error":        fmt.Sprintf("agent provider %q command is not configured", firstNonEmpty(run.ProviderID, run.ProviderKind)),
		}, nil, fmt.Errorf("agent provider %q command is not configured", firstNonEmpty(run.ProviderID, run.ProviderKind))
	}
	prefetchedTools := r.prefetchAgentRunToolContext(ctx, run)
	if len(prefetchedTools) > 0 {
		run = agentRunWithPrefetchedToolContext(run, prefetchedTools)
	}
	prompt := buildAgentProviderPrompt(run)
	args := append([]string{}, spec.Args...)
	promptArg := strings.TrimSpace(spec.PromptArg)
	if promptArg == "" {
		args = append(args, prompt)
	} else {
		args = append(args, promptArg, prompt)
	}
	if skillArg := strings.TrimSpace(spec.SkillArg); skillArg != "" {
		for _, skill := range commandSkillArgs(run, spec) {
			if trimmed := strings.TrimSpace(skill); trimmed != "" {
				args = append(args, skillArg, trimmed)
			}
		}
	}
	if providerSkillArg := strings.TrimSpace(spec.ProviderSkillArg); providerSkillArg != "" {
		for _, skill := range commandProviderSkillArgs(run, spec) {
			args = append(args, providerSkillArg, skill)
		}
	}
	workspaceRoot := strings.TrimSpace(r.cfg.AgentRuntime.WorkspaceRoot)
	if workspaceRoot == "" {
		workspaceRoot = ".soha/agent-runtime"
	}
	absRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve agent runtime workspace: %w", err)
	}
	workspace, err := resolveWorkspacePath(absRoot, run.ID)
	if err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create agent runtime workspace: %w", err)
	}
	logs, err := runCommand(ctx, workspace, command, args...)
	output := map[string]any{
		"provider":     run.ProviderID,
		"providerKind": run.ProviderKind,
		"capabilityId": run.CapabilityID,
		"skillIds":     run.SkillIDs,
		"logs":         logs,
	}
	if len(prefetchedTools) > 0 {
		output["prefetchedToolResults"] = agentToolCallResultMaps(prefetchedTools)
	}
	rawOutput := strings.TrimSpace(joinCommandOutput(logs))
	if rawOutput != "" {
		output["rawOutput"] = rawOutput
		output["summary"] = summarizeAgentOutput(rawOutput)
	}
	for key, value := range parseAgentOutputJSON(rawOutput) {
		output[key] = value
	}
	return output, logs, err
}

func (r *Runner) prefetchAgentRunToolContext(ctx context.Context, run AgentRun) []AgentToolCallResult {
	items := make([]AgentToolCallResult, 0)
	for _, binding := range run.ToolBindings {
		if len(items) >= 3 {
			break
		}
		if !agentToolBindingPrefetchable(binding) {
			continue
		}
		result, ok := r.agentRunToolCall(ctx, run, binding, map[string]any{"limit": 20})
		if !ok {
			continue
		}
		items = append(items, result)
	}
	return items
}

func (r *Runner) executeComposeAction(ctx context.Context, operation DockerOperation) ([]string, error) {
	action := firstNonEmpty(strings.TrimSpace(fmt.Sprint(operation.Payload["action"])), "deploy")
	if action == "validate" {
		return r.validateDeliveryCompose(ctx, operation)
	}
	if err := validateBoundProjectNamespace(ctx, operation); err != nil {
		return nil, err
	}
	dir, logs, err := r.prepareComposeWorkspace(operation)
	if err != nil {
		return logs, err
	}
	if action == "redeploy" && isSingleContainerSource(operation) {
		return r.executeSingleContainerRedeploy(ctx, dir, operation, logs)
	}
	buildWorkspace := ""
	if strings.TrimSpace(fmt.Sprint(operation.Payload["sourceKind"])) == "git_dockerfile" {
		buildLogs, workspace, buildErr := r.executeGitDockerfileBuild(ctx, dir, operation)
		logs = append(logs, buildLogs...)
		if buildErr != nil {
			if cleanupErr := cleanupGitBuildWorkspace(workspace, true); cleanupErr != nil {
				logs = append(logs, "git build workspace cleanup failed: "+cleanupErr.Error())
			}
			return logs, buildErr
		}
		buildWorkspace = workspace
	}
	args := composeArgsForAction(action)
	if len(args) == 0 {
		if cleanupErr := cleanupGitBuildWorkspace(buildWorkspace, true); cleanupErr != nil {
			logs = append(logs, "git build workspace cleanup failed: "+cleanupErr.Error())
		}
		return logs, fmt.Errorf("unsupported compose action %q", action)
	}
	commandLogs, err := runComposeOperation(ctx, dir, operation, args)
	logs = append(logs, commandLogs...)
	logs = appendGitBuildCleanupLogs(logs, buildWorkspace, err != nil)
	return logs, err
}

func (r *Runner) executeSingleContainerRedeploy(ctx context.Context, dir string, operation DockerOperation, logs []string) ([]string, error) {
	buildWorkspace := ""
	if strings.TrimSpace(fmt.Sprint(operation.Payload["sourceKind"])) == "git_dockerfile" {
		buildLogs, workspace, err := r.executeGitDockerfileBuild(ctx, dir, operation)
		logs = append(logs, buildLogs...)
		if err != nil {
			return appendGitBuildCleanupLogs(logs, workspace, true), err
		}
		buildWorkspace = workspace
	} else {
		pullLogs, err := runComposeOperation(ctx, dir, operation, []string{"compose", "-f", "compose.yaml", "pull"})
		logs = append(logs, pullLogs...)
		if err != nil {
			return logs, err
		}
	}

	downLogs, err := runComposeOperation(ctx, dir, operation, []string{"compose", "-f", "compose.yaml", "down", "--remove-orphans"})
	logs = append(logs, downLogs...)
	if err != nil {
		return appendGitBuildCleanupLogs(logs, buildWorkspace, true), err
	}
	upLogs, err := runComposeOperation(ctx, dir, operation, []string{"compose", "-f", "compose.yaml", "up", "-d", "--force-recreate"})
	logs = append(logs, upLogs...)
	return appendGitBuildCleanupLogs(logs, buildWorkspace, err != nil), err
}

func isSingleContainerSource(operation DockerOperation) bool {
	sourceKind := strings.TrimSpace(fmt.Sprint(operation.Payload["sourceKind"]))
	return sourceKind == "single_container" || sourceKind == "git_dockerfile"
}

func appendGitBuildCleanupLogs(logs []string, workspace string, preserveResolvedCommit bool) []string {
	if workspace == "" {
		return logs
	}
	if err := cleanupGitBuildWorkspace(workspace, preserveResolvedCommit); err != nil {
		return append(logs, "git build workspace cleanup failed: "+err.Error())
	}
	if preserveResolvedCommit {
		return append(logs, "git build checkout cleaned; resolved commit retained")
	}
	return append(logs, "git build workspace cleaned")
}

func (r *Runner) executeComposeServiceAction(ctx context.Context, operation DockerOperation) ([]string, error) {
	if _, bound, err := boundServiceTarget(operation); err != nil || bound {
		if err != nil {
			return nil, err
		}
		return executeBoundServiceAction(ctx, operation)
	}
	action := strings.TrimSpace(fmt.Sprint(operation.Payload["action"]))
	serviceName := strings.TrimSpace(fmt.Sprint(operation.Payload["serviceName"]))
	if serviceName == "" {
		return nil, fmt.Errorf("serviceName is required for docker service action")
	}
	dir, logs, err := r.prepareComposeWorkspace(operation)
	if err != nil {
		return logs, err
	}
	args := composeServiceArgsForAction(action, serviceName)
	if len(args) == 0 {
		return logs, fmt.Errorf("unsupported docker service action %q", action)
	}
	commandLogs, err := runComposeOperation(ctx, dir, operation, args)
	logs = append(logs, commandLogs...)
	return logs, err
}

func (r *Runner) prepareComposeWorkspace(operation DockerOperation) (string, []string, error) {
	root := strings.TrimSpace(r.cfg.Docker.ComposeRoot)
	if root == "" {
		root = ".soha/docker"
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", nil, fmt.Errorf("resolve docker compose root: %w", err)
	}
	slug := firstNonEmpty(strings.TrimSpace(fmt.Sprint(operation.Payload["projectSlug"])), operation.ProjectID, operation.ID)
	binding, bound, bindErr := dockerOperationBinding(operation)
	if bindErr != nil {
		return "", nil, bindErr
	}
	if bound {
		slug = binding.Namespace
	}
	dir, err := resolveWorkspacePath(absRoot, slug)
	if err != nil {
		return "", nil, err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return dir, nil, fmt.Errorf("create docker compose workspace: %w", err)
	}
	composeContent := strings.TrimSpace(fmt.Sprint(operation.Payload["composeContent"]))
	if composeContent == "" {
		return dir, nil, fmt.Errorf("composeContent is required")
	}
	composeContent, err = boundComposeContent(operation, composeContent)
	if err != nil {
		return dir, nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, "compose.yaml"), []byte(composeContent+"\n"), 0o600); err != nil {
		return dir, nil, fmt.Errorf("write compose.yaml: %w", err)
	}
	envContent := strings.TrimSpace(fmt.Sprint(operation.Payload["envContent"]))
	envPath := filepath.Join(dir, ".env")
	if envContent != "" {
		if err := os.WriteFile(envPath, []byte(envContent+"\n"), 0o600); err != nil {
			return dir, nil, fmt.Errorf("write .env: %w", err)
		}
	} else if err := os.Remove(envPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return dir, nil, fmt.Errorf("remove stale .env: %w", err)
	}
	return dir, []string{fmt.Sprintf("compose workspace prepared at %s", dir)}, nil
}

func composeArgsForAction(action string) []string {
	base := []string{"compose", "-f", "compose.yaml"}
	switch strings.TrimSpace(action) {
	case "validate":
		return append(base, "config", "--quiet")
	case "delivery_deploy":
		return append(base, "up", "-d", "--no-build", "--pull", "always", "--wait")
	case "", "deploy", "redeploy", "start":
		return append(base, "up", "-d")
	case "restart":
		return append(base, "restart")
	case "stop":
		return append(base, "stop")
	case "down", "destroy":
		return append(base, "down", "--remove-orphans")
	case "pull":
		return append(base, "pull")
	case "build":
		return append(base, "build")
	default:
		return nil
	}
}

func composeServiceArgsForAction(action, serviceName string) []string {
	base := []string{"compose", "-f", "compose.yaml"}
	switch strings.TrimSpace(action) {
	case "start":
		return append(base, "up", "-d", serviceName)
	case "restart":
		return append(base, "restart", serviceName)
	case "stop":
		return append(base, "stop", serviceName)
	case "logs":
		return append(base, "logs", "--tail", "200", serviceName)
	default:
		return nil
	}
}

func (r *Runner) collectComposeServices(ctx context.Context, operation DockerOperation) ([]map[string]any, error) {
	if binding, bound, err := dockerOperationBinding(operation); err != nil || bound {
		if err != nil {
			return nil, err
		}
		return collectBoundServices(ctx, binding)
	}
	dir, _, err := r.prepareComposeWorkspace(operation)
	if err != nil {
		return nil, err
	}
	args := []string{"compose", "-f", "compose.yaml", "ps", "--format", "json"}
	commandLogs, err := runComposeOperation(ctx, dir, operation, args)
	if err != nil {
		return nil, err
	}
	services := make([]map[string]any, 0)
	for _, line := range commandLogs {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "$ ") {
			continue
		}
		for _, item := range strings.Split(trimmed, "\n") {
			raw := strings.TrimSpace(item)
			if raw == "" {
				continue
			}
			services = append(services, dockerServiceRecordsFromJSON(raw)...)
		}
	}
	return services, nil
}

func (r *Runner) streamHeartbeats(ctx context.Context, cancel context.CancelFunc, done <-chan struct{}, stopReason chan<- string, task ExecutionTask, agentID, command string, commandIndex, commandCount int, workspacePath string) {
	ticker := time.NewTicker(commandHeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			remoteTask, ok := r.callback(ctx, task, "running", extendMap(
				buildHeartbeatPayload(agentID, command, commandIndex, commandCount),
				map[string]any{"workspacePath": workspacePath},
			))
			if ok && shouldStopLocalExecution(remoteTask.Status) {
				select {
				case stopReason <- strings.TrimSpace(remoteTask.Status):
				default:
				}
				cancel()
				return
			}
		}
	}
}

func (r *Runner) watchRunnerStatus(ctx context.Context, cancel context.CancelFunc, done <-chan struct{}, stopReason chan<- string, task ExecutionTask) {
	ticker := time.NewTicker(runnerStatusPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			remoteTask, ok := r.fetchRunnerTaskStatus(ctx, task.ID)
			if ok && (shouldStopLocalExecution(remoteTask.Status) || strings.HasPrefix(task.ProviderKind, "buildpacks_runner.") && remoteTask.Status == "canceling") {
				r.updateActiveTask(task.ID, func(item *ActiveTask) {
					item.Status = remoteTask.Status
					item.StopSource = "control_plane"
					item.StopReason = remoteTask.Status
				})
				select {
				case stopReason <- strings.TrimSpace(remoteTask.Status):
				default:
				}
				cancel()
				return
			}
		}
	}
}

func (r *Runner) ListActiveTasks() []ActiveTask {
	r.mu.RLock()
	defer r.mu.RUnlock()
	items := make([]ActiveTask, 0, len(r.active))
	for _, item := range r.active {
		items = append(items, item.snapshot)
	}
	return items
}

func (r *Runner) GetActiveTask(taskID string) (ActiveTask, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, ok := r.active[strings.TrimSpace(taskID)]
	if !ok {
		return ActiveTask{}, false
	}
	return item.snapshot, true
}

func (r *Runner) CancelActiveTask(taskID, reason string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.active[strings.TrimSpace(taskID)]
	if !ok || item == nil || item.cancel == nil {
		return false
	}
	if strings.TrimSpace(reason) == "" {
		reason = "canceled from agent runtime API"
	}
	item.snapshot.Status = "canceling"
	item.snapshot.StopSource = "local_api"
	item.snapshot.StopReason = strings.TrimSpace(reason)
	item.snapshot.UpdatedAt = time.Now().UTC()
	item.cancel()
	return true
}

func (r *Runner) callback(ctx context.Context, task ExecutionTask, status string, payload map[string]any) (ExecutionTask, bool) {
	payload, _ = redactResolvedSecretValues(ctx, redactAgentRuntimeValue(payload)).(map[string]any)
	var result ExecutionTask
	ok := r.withCallbackRetry(ctx, metricScopeExecution, status, func() error {
		next, err := r.apiClient().RecordExecutionCallback(ctx, callbackRequest{
			CallbackToken: task.CallbackToken,
			Status:        status,
			Payload:       payload,
		})
		if err != nil {
			return err
		}
		result = next
		return nil
	})
	if !ok {
		return ExecutionTask{}, false
	}
	if strings.TrimSpace(result.ID) == "" {
		return ExecutionTask{}, false
	}
	return result, true
}

func (r *Runner) finalCallback(ctx context.Context, task ExecutionTask, status string, payload map[string]any) (ExecutionTask, bool) {
	finalCtx, cancel := r.finalCallbackContext(ctx)
	defer cancel()
	return r.callback(finalCtx, task, status, payload)
}

func (r *Runner) dockerCallback(ctx context.Context, operation DockerOperation, status string, payload map[string]any, logs []string) (DockerOperation, bool) {
	redactedPayload, _ := redactAgentRuntimeValue(payload).(map[string]any)
	var result DockerOperation
	ok := r.withCallbackRetry(ctx, metricScopeDocker, status, func() error {
		next, err := r.apiClient().RecordDockerOperationCallback(ctx, dockerCallbackRequest{
			OperationID:              operation.ID,
			CancellationAcknowledged: status == "canceled" && boolValue(payload["cancellationAcknowledged"], false),
			WorkerID:                 dockerWorkerID(r.cfg),
			CallbackToken:            operation.CallbackToken,
			Status:                   status,
			Payload:                  redactedPayload,
			Logs:                     redactAgentRuntimeLogs(logs),
		})
		if err != nil {
			return err
		}
		result = next
		return nil
	})
	if !ok {
		return DockerOperation{}, false
	}
	if strings.TrimSpace(result.ID) == "" {
		return DockerOperation{}, false
	}
	return result, true
}

func (r *Runner) agentRunCallback(ctx context.Context, run AgentRun, status string, payload map[string]any, toolExecutions []map[string]any, analysisArtifacts []map[string]any, externalRunID string, errorMessage string) (AgentRun, bool) {
	payload, _ = redactResolvedSecretValues(ctx, redactAgentRuntimeValue(payload)).(map[string]any)
	toolExecutions, _ = redactResolvedSecretValues(ctx, redactAgentRuntimeValue(toolExecutions)).([]map[string]any)
	analysisArtifacts, _ = redactResolvedSecretValues(ctx, redactAgentRuntimeValue(analysisArtifacts)).([]map[string]any)
	externalRunID, _ = redactResolvedSecretValues(ctx, redactAgentRuntimeText(externalRunID)).(string)
	errorMessage, _ = redactResolvedSecretValues(ctx, redactAgentRuntimeText(errorMessage)).(string)
	var result AgentRun
	ok := r.withCallbackRetry(ctx, metricScopeAgentRuntime, status, func() error {
		next, err := r.apiClient().RecordAgentRunCallback(ctx, agentRunCallbackRequest{
			RunID:             run.ID,
			CallbackToken:     run.CallbackToken,
			AgentID:           agentRuntimeWorkerID(r.cfg),
			Status:            status,
			Payload:           payload,
			ToolExecutions:    toolExecutions,
			AnalysisArtifacts: analysisArtifacts,
			ExternalRunID:     externalRunID,
			ErrorMessage:      errorMessage,
		})
		if err != nil {
			return err
		}
		result = next
		return nil
	})
	if !ok {
		return AgentRun{}, false
	}
	if strings.TrimSpace(result.ID) == "" {
		return AgentRun{}, false
	}
	if shouldStopLocalExecution(status) && payload["cancellationAcknowledged"] != true && result.Status == "canceled" && result.Output["cancellationPending"] == true {
		r.acknowledgeAgentCancellation(ctx, run, result.Status)
	}
	return result, true
}

func (r *Runner) agentRunToolCall(ctx context.Context, run AgentRun, binding map[string]any, input map[string]any) (AgentToolCallResult, bool) {
	result, err := r.apiClient().RecordAgentRunToolCall(ctx, agentRunToolCallRequest{
		RunID:         run.ID,
		CallbackToken: run.CallbackToken,
		AgentID:       agentRuntimeWorkerID(r.cfg),
		ToolBindingID: strings.TrimSpace(fmt.Sprint(binding["id"])),
		AdapterID:     strings.TrimSpace(fmt.Sprint(binding["adapterId"])),
		ToolName:      strings.TrimSpace(fmt.Sprint(binding["toolName"])),
		Input:         input,
	})
	if err != nil {
		return AgentToolCallResult{}, false
	}
	if strings.TrimSpace(result.RunID) == "" {
		return AgentToolCallResult{}, false
	}
	return result, true
}

func (r *Runner) withCallbackRetry(ctx context.Context, scope, status string, call func() error) bool {
	if call == nil {
		return false
	}
	attempts := r.cfg.CallbackRetry.MaxAttempts
	if attempts <= 0 {
		attempts = 1
	}
	backoff := r.cfg.CallbackRetry.Backoff
	if backoff <= 0 {
		backoff = 500 * time.Millisecond
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		r.metrics.markCallbackAttempt(scope)
		if err := call(); err != nil {
			lastErr = err
			r.metrics.markCallbackFailure(scope)
			if ctx.Err() != nil || attempt == attempts {
				break
			}
			select {
			case <-ctx.Done():
				return false
			case <-time.After(time.Duration(attempt) * backoff):
			}
			continue
		}
		r.metrics.markCallbackSuccess(scope, status)
		return true
	}
	if lastErr != nil {
		r.logger.Warn("runner callback failed",
			zap.String("scope", scope),
			zap.String("status", status),
			zap.Int("attempts", attempts),
			zap.String("reason", lastErr.Error()),
		)
	}
	return false
}

func (r *Runner) finalCallbackContext(parent context.Context) (context.Context, context.CancelFunc) {
	timeout := r.cfg.CallbackRetry.Backoff
	if timeout <= 0 {
		timeout = 500 * time.Millisecond
	}
	timeout = timeout * time.Duration(maxInt(r.cfg.CallbackRetry.MaxAttempts, 1)+1)
	if timeout < 2*time.Second {
		timeout = 2 * time.Second
	}
	if timeout > 30*time.Second {
		timeout = 30 * time.Second
	}
	return context.WithTimeout(context.WithoutCancel(parent), timeout)
}

func (r *Runner) executionTaskContext(ctx context.Context, task ExecutionTask) (context.Context, context.CancelFunc) {
	timeout := r.executionTaskTimeout(task)
	if timeout <= 0 {
		return context.WithCancel(ctx)
	}
	return context.WithTimeout(ctx, timeout)
}

func (r *Runner) executionTaskTimeout(task ExecutionTask) time.Duration {
	if task.TimeoutSeconds > 0 {
		return time.Duration(task.TimeoutSeconds) * time.Second
	}
	timeout := durationFromPayload(task.Payload, "timeoutSeconds", "timeout_seconds")
	if timeout <= 0 {
		timeout = durationFromPayload(task.Payload, "timeout", "timeoutDuration")
	}
	if timeout <= 0 {
		timeout = r.cfg.DefaultTimeout
	}
	return timeout
}

func (r *Runner) fetchRunnerTaskStatus(ctx context.Context, taskID string) (ExecutionTask, bool) {
	task, err := r.apiClient().GetExecutionTaskRunnerStatus(ctx, taskID)
	if err != nil {
		return ExecutionTask{}, false
	}
	if strings.TrimSpace(task.ID) == "" {
		return ExecutionTask{}, false
	}
	return task, true
}

func (r *Runner) fetchDockerRunnerStatus(ctx context.Context, operationID string) (DockerOperation, bool) {
	operation, err := r.apiClient().GetDockerOperationRunnerStatus(ctx, operationID)
	if err != nil {
		return DockerOperation{}, false
	}
	if strings.TrimSpace(operation.ID) == "" {
		return DockerOperation{}, false
	}
	return operation, true
}

func (r *Runner) prepareWorkspace(ctx context.Context, task ExecutionTask) (string, string, []map[string]any, []string, error) {
	spec := parseWorkspaceSpec(task)
	root := strings.TrimSpace(r.cfg.WorkspaceRoot)
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", "", nil, nil, fmt.Errorf("resolve workspace root: %w", err)
	}
	workspacePath, err := resolveWorkspacePath(absRoot, firstNonEmpty(spec.Path, task.ApplicationID, task.ID))
	if err != nil {
		return "", "", nil, nil, err
	}
	logs := []string{fmt.Sprintf("workspace prepared at %s", workspacePath)}

	if err := os.MkdirAll(filepath.Dir(workspacePath), 0o755); err != nil {
		return workspacePath, "", nil, logs, fmt.Errorf("create workspace parent: %w", err)
	}
	if spec.Checkout.Enabled || strings.TrimSpace(spec.Checkout.RepositoryURL) != "" {
		checkoutLogs, checkoutErr := r.ensureCheckout(ctx, workspacePath, spec.Checkout)
		logs = append(logs, checkoutLogs...)
		if checkoutErr != nil {
			return workspacePath, "", nil, logs, checkoutErr
		}
	} else if err := os.MkdirAll(workspacePath, 0o755); err != nil {
		return workspacePath, "", nil, logs, fmt.Errorf("create workspace: %w", err)
	}

	commandDir := workspacePath
	if strings.TrimSpace(spec.CommandDir) != "" {
		commandDir, err = resolveWorkspacePath(workspacePath, spec.CommandDir)
		if err != nil {
			return workspacePath, "", nil, logs, err
		}
		info, statErr := os.Stat(commandDir)
		if statErr != nil {
			return workspacePath, "", nil, logs, fmt.Errorf("commandDir %s is not available: %w", commandDir, statErr)
		}
		if !info.IsDir() {
			return workspacePath, "", nil, logs, fmt.Errorf("commandDir %s is not a directory", commandDir)
		}
	}
	return workspacePath, commandDir, collectWorkspaceArtifacts(workspacePath, spec.ArtifactFiles), logs, nil
}

func (r *Runner) ensureCheckout(ctx context.Context, workspacePath string, spec checkoutSpec) ([]string, error) {
	logs := make([]string, 0, 6)
	hasRepo := hasGitRepository(workspacePath)
	if !hasRepo && strings.TrimSpace(spec.RepositoryURL) != "" {
		if empty, err := isEmptyDirectory(workspacePath); err == nil && empty {
			_ = os.Remove(workspacePath)
		}
		commandLogs, err := runCommand(ctx, "", "git", "clone", spec.RepositoryURL, workspacePath)
		logs = append(logs, commandLogs...)
		if err != nil {
			return logs, fmt.Errorf("git clone failed: %w", err)
		}
		hasRepo = true
	}
	if !hasRepo {
		if strings.TrimSpace(spec.RepositoryURL) == "" && !spec.Enabled {
			if err := os.MkdirAll(workspacePath, 0o755); err != nil {
				return logs, fmt.Errorf("create workspace: %w", err)
			}
			return logs, nil
		}
		return logs, fmt.Errorf("workspace %s does not contain a git repository and no repositoryURL was provided", workspacePath)
	}

	if strings.TrimSpace(spec.RepositoryURL) != "" || spec.Enabled {
		commandLogs, err := runCommand(ctx, "", "git", "-C", workspacePath, "fetch", "--all", "--tags", "--prune")
		logs = append(logs, commandLogs...)
		if err != nil {
			return logs, fmt.Errorf("git fetch failed: %w", err)
		}
	}

	refType := firstNonEmpty(spec.RefType, "branch")
	refName := strings.TrimSpace(spec.RefName)
	if refName == "" && refType == "branch" {
		refName = strings.TrimSpace(spec.DefaultBranch)
	}
	if refName == "" {
		return logs, nil
	}

	switch refType {
	case "tag":
		commandLogs, err := runCommand(ctx, "", "git", "-C", workspacePath, "checkout", "--force", "tags/"+refName)
		logs = append(logs, commandLogs...)
		if err != nil {
			return logs, fmt.Errorf("git checkout tag %s failed: %w", refName, err)
		}
	case "commit":
		commandLogs, err := runCommand(ctx, "", "git", "-C", workspacePath, "checkout", "--force", refName)
		logs = append(logs, commandLogs...)
		if err != nil {
			return logs, fmt.Errorf("git checkout commit %s failed: %w", refName, err)
		}
	default:
		commandLogs, err := runCommand(ctx, "", "git", "-C", workspacePath, "checkout", "--force", "-B", refName, "origin/"+refName)
		logs = append(logs, commandLogs...)
		if err == nil {
			return logs, nil
		}
		commandLogs, fallbackErr := runCommand(ctx, "", "git", "-C", workspacePath, "checkout", "--force", refName)
		logs = append(logs, commandLogs...)
		if fallbackErr != nil {
			return logs, fmt.Errorf("git checkout branch %s failed: %w", refName, err)
		}
	}
	return logs, nil
}

func parseWorkspaceSpec(task ExecutionTask) workspaceSpec {
	spec := workspaceSpec{
		Path: firstNonEmpty(
			strings.TrimSpace(fmt.Sprint(task.Payload["repositoryPath"])),
			strings.TrimSpace(task.ApplicationID),
			strings.TrimSpace(task.ID),
		),
	}
	raw, ok := task.Payload["workspace"].(map[string]any)
	if !ok {
		return spec
	}
	spec.Path = firstNonEmpty(
		strings.TrimSpace(fmt.Sprint(raw["path"])),
		strings.TrimSpace(fmt.Sprint(raw["relativePath"])),
		spec.Path,
	)
	spec.CommandDir = firstNonEmpty(
		strings.TrimSpace(fmt.Sprint(raw["commandDir"])),
		strings.TrimSpace(fmt.Sprint(raw["workingDir"])),
	)
	spec.ArtifactFiles = firstNonEmptyStringSlice(valueAsStringSlice(raw["artifactFiles"]), valueAsStringSlice(task.Payload["artifactFiles"]))
	if checkoutRaw, ok := raw["checkout"].(map[string]any); ok {
		spec.Checkout = checkoutSpec{
			Enabled:        boolValue(checkoutRaw["enabled"], false),
			RepositoryURL:  firstNonEmpty(strings.TrimSpace(fmt.Sprint(checkoutRaw["repositoryURL"])), strings.TrimSpace(fmt.Sprint(checkoutRaw["repositoryUrl"]))),
			RepositoryPath: strings.TrimSpace(fmt.Sprint(checkoutRaw["repositoryPath"])),
			RefType:        firstNonEmpty(strings.TrimSpace(fmt.Sprint(checkoutRaw["refType"])), strings.TrimSpace(fmt.Sprint(task.Payload["refType"]))),
			RefName:        firstNonEmpty(strings.TrimSpace(fmt.Sprint(checkoutRaw["refName"])), strings.TrimSpace(fmt.Sprint(task.Payload["refName"]))),
			DefaultBranch:  strings.TrimSpace(fmt.Sprint(checkoutRaw["defaultBranch"])),
		}
	}
	if spec.Checkout.RefType == "" {
		spec.Checkout.RefType = strings.TrimSpace(fmt.Sprint(task.Payload["refType"]))
	}
	if spec.Checkout.RefName == "" {
		spec.Checkout.RefName = strings.TrimSpace(fmt.Sprint(task.Payload["refName"]))
	}
	if spec.Checkout.DefaultBranch == "" {
		spec.Checkout.DefaultBranch = strings.TrimSpace(fmt.Sprint(task.Payload["defaultBranch"]))
	}
	return spec
}

func runCommand(ctx context.Context, dir, name string, args ...string) ([]string, error) {
	command := exec.CommandContext(ctx, name, args...)
	configureCommandCancellation(command)
	if environment := secretEnvironment(ctx); environment != nil {
		command.Env = environment
	}
	if strings.TrimSpace(dir) != "" {
		command.Dir = dir
	}
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()

	logs := []string{"$ " + strings.TrimSpace(strings.Join(append([]string{name}, args...), " "))}
	if value := strings.TrimSpace(stdout.String()); value != "" {
		logs = append(logs, value)
	}
	if value := strings.TrimSpace(stderr.String()); value != "" {
		logs = append(logs, value)
	}
	return logs, err
}

func resolveWorkspacePath(root, relative string) (string, error) {
	if filepath.IsAbs(relative) {
		return "", fmt.Errorf("absolute workspace paths are not allowed: %s", relative)
	}
	cleaned := filepath.Clean(relative)
	if cleaned == "." {
		cleaned = ""
	}
	if strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("workspace path escapes root: %s", relative)
	}
	full := filepath.Clean(filepath.Join(root, cleaned))
	root = filepath.Clean(root)
	if full != root && !strings.HasPrefix(full, root+string(os.PathSeparator)) {
		return "", fmt.Errorf("workspace path escapes root: %s", relative)
	}
	return full, nil
}

func collectWorkspaceArtifacts(workspacePath string, files []string) []map[string]any {
	items := make([]map[string]any, 0, len(files))
	for _, file := range files {
		trimmed := strings.TrimSpace(file)
		if trimmed == "" {
			continue
		}
		full, err := resolveWorkspacePath(workspacePath, trimmed)
		if err != nil {
			items = append(items, map[string]any{"path": trimmed, "status": "invalid"})
			continue
		}
		info, statErr := os.Stat(full)
		if statErr != nil {
			items = append(items, map[string]any{"path": trimmed, "status": "missing"})
			continue
		}
		items = append(items, map[string]any{
			"path":       trimmed,
			"status":     "completed",
			"sizeBytes":  info.Size(),
			"modifiedAt": info.ModTime().UTC().Format(time.RFC3339),
		})
	}
	return items
}

var imageDigestPattern = regexp.MustCompile(`sha256:[a-fA-F0-9]{64}`)

func imageDigestFromWorkspaceArtifacts(workspacePath string, artifacts []map[string]any) string {
	for _, artifact := range artifacts {
		path := strings.TrimSpace(fmt.Sprint(artifact["path"]))
		if path != ".soha-image-digest" || strings.TrimSpace(fmt.Sprint(artifact["status"])) != "completed" {
			continue
		}
		full, err := resolveWorkspacePath(workspacePath, path)
		if err != nil {
			return ""
		}
		content, err := os.ReadFile(full)
		if err != nil {
			return ""
		}
		return imageDigestPattern.FindString(string(content))
	}
	return ""
}

func workspaceFailureStage(err error) string {
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "git clone") || strings.Contains(message, "git fetch") || strings.Contains(message, "git checkout") || strings.Contains(message, "git repository") {
		return "checkout"
	}
	return "prepare"
}

func commandPipelineStage(command string) string {
	command = strings.ToLower(strings.TrimSpace(command))
	switch {
	case strings.Contains(command, "docker push"):
		return "push"
	case strings.Contains(command, "digest-file"), strings.Contains(command, "image inspect"):
		return "publish"
	case strings.Contains(command, "docker build"), strings.Contains(command, "buildx build"), strings.HasPrefix(command, "executor "):
		return "build"
	default:
		return "build"
	}
}

func hasGitRepository(path string) bool {
	info, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil && info.IsDir()
}

func isEmptyDirectory(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%s is not a directory", path)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, err
	}
	return len(entries) == 0, nil
}

func extractCommands(payload map[string]any) []string {
	raw, ok := payload["commands"]
	if !ok || raw == nil {
		return nil
	}
	switch value := raw.(type) {
	case []string:
		return append([]string(nil), value...)
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			text := strings.TrimSpace(fmt.Sprint(item))
			if text != "" {
				items = append(items, text)
			}
		}
		return items
	default:
		return nil
	}
}

func commandFingerprint(command string) string {
	trimmed := strings.TrimSpace(command)
	if trimmed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(trimmed))
	return fmt.Sprintf("sha256:%x", sum[:8])
}

func resolveImageFromCommands(payload map[string]any, commands []string) string {
	if value := strings.TrimSpace(fmt.Sprint(payload["image"])); value != "" {
		return value
	}
	for _, command := range commands {
		parts := strings.Fields(command)
		for index := 0; index < len(parts)-1; index++ {
			if parts[index] == "-t" {
				return strings.TrimSpace(parts[index+1])
			}
		}
	}
	return ""
}

func buildImageArtifact(payload map[string]any, image string) map[string]any {
	artifact := map[string]any{
		"kind":   "image",
		"ref":    image,
		"status": "completed",
	}
	if digest := strings.TrimSpace(fmt.Sprint(payload["imageDigest"])); digest != "" && digest != "pending" {
		artifact["digest"] = digest
	}
	return artifact
}

func buildArtifactList(payload map[string]any, image string) []map[string]any {
	items := valueAsMapSlice(payload["artifacts"])
	if len(items) == 0 {
		return []map[string]any{buildImageArtifact(payload, image)}
	}
	next := make([]map[string]any, 0, len(items))
	updated := false
	for _, item := range items {
		copyItem := map[string]any{}
		for key, value := range item {
			copyItem[key] = value
		}
		if !updated && strings.TrimSpace(fmt.Sprint(copyItem["kind"])) == "image" {
			copyItem["ref"] = image
			copyItem["status"] = "completed"
			if digest := strings.TrimSpace(fmt.Sprint(payload["imageDigest"])); digest != "" && digest != "pending" {
				copyItem["digest"] = digest
			}
			updated = true
		}
		next = append(next, copyItem)
	}
	if !updated {
		next = append(next, buildImageArtifact(payload, image))
	}
	return next
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func stringMapValue(values map[string]any, key string) string {
	if values == nil {
		return ""
	}
	value, ok := values[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func firstNonEmptyStringSlice(candidates ...[]string) []string {
	for _, items := range candidates {
		if len(items) > 0 {
			return items
		}
	}
	return nil
}

func compactStringSlice(items []string) []string {
	out := make([]string, 0, len(items))
	for _, item := range items {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func agentRuntimeWorkerID(cfg cfgpkg.ControlPlaneConfig) string {
	return firstNonEmpty(strings.TrimSpace(cfg.AgentRuntime.WorkerID), strings.TrimSpace(cfg.AgentID), "local-hermes-runner")
}

func normalizedAgentProviderKey(run AgentRun) string {
	return strings.ToLower(firstNonEmpty(strings.TrimSpace(run.ProviderKind), strings.TrimSpace(run.ProviderID)))
}

func (r *Runner) agentProviderCommandSpec(providerKey string) agentProviderCommandSpec {
	providerKey = strings.ToLower(strings.TrimSpace(providerKey))
	if providerKey == "" {
		return agentProviderCommandSpec{}
	}
	if spec, ok := r.cfg.AgentRuntime.Providers[providerKey]; ok {
		return normalizeAgentProviderCommandSpec(spec)
	}
	if r.providerRegistry != nil {
		if provider, ok := r.providerRegistry.Resolve(providerKey, ""); ok && !provider.Draining && provider.Runtime.Kind == "cli" {
			spec := provider.commandSpec()
			command := strings.TrimSpace(r.cfg.AgentRuntime.HermesCommand)
			if command != "" && provider.Kind == "hermes" {
				spec.Command = command
			}
			return spec
		}
	}
	if provider, ok := defaultAgentProviderDefinition(providerKey); ok {
		spec := provider.DefaultCommand
		command := strings.TrimSpace(r.cfg.AgentRuntime.HermesCommand)
		if command != "" && provider.Kind == "hermes" {
			spec.Command = command
		}
		return spec
	}
	return agentProviderCommandSpec{}
}

func normalizeAgentProviderCommandSpec(input cfgpkg.AgentProviderConfig) agentProviderCommandSpec {
	return agentProviderCommandSpec{
		Command:          strings.TrimSpace(input.Command),
		Args:             compactStringSlice(input.Args),
		PromptArg:        strings.TrimSpace(input.PromptArg),
		SkillArg:         strings.TrimSpace(input.SkillArg),
		ProviderSkillArg: strings.TrimSpace(input.ProviderSkillArg),
	}
}

func providerSkillRefs(bindings []map[string]any) []string {
	out := make([]string, 0, len(bindings))
	seen := map[string]struct{}{}
	for _, binding := range bindings {
		value := strings.TrimSpace(fmt.Sprint(binding["providerSkillRef"]))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func commandSkillArgs(run AgentRun, spec agentProviderCommandSpec) []string {
	// Backward compatibility: older Hermes configs used skill_arg=-s and left
	// provider_skill_arg empty. In that case, pass provider-native refs through
	// the skill arg so Hermes still receives its own skill names.
	if strings.TrimSpace(spec.ProviderSkillArg) == "" {
		if refs := providerSkillRefs(run.SkillBindings); len(refs) > 0 {
			return refs
		}
	}
	return compactStringSlice(run.SkillIDs)
}

func commandProviderSkillArgs(run AgentRun, spec agentProviderCommandSpec) []string {
	refs := providerSkillRefs(run.SkillBindings)
	if len(refs) > 0 {
		return refs
	}
	if strings.TrimSpace(spec.SkillArg) == "" {
		return compactStringSlice(run.SkillIDs)
	}
	return nil
}

func agentRunCompletionSummary(run AgentRun) string {
	return fmt.Sprintf("%s agent run completed", firstNonEmpty(run.ProviderKind, run.ProviderID, "provider"))
}

func buildAgentProviderPrompt(run AgentRun) string {
	payload := map[string]any{
		"contract":      "soha.agentRuntime.v1",
		"providerId":    run.ProviderID,
		"providerKind":  run.ProviderKind,
		"capabilityId":  run.CapabilityID,
		"skillIds":      run.SkillIDs,
		"scope":         run.Scope,
		"toolset":       run.Toolset,
		"toolBindings":  run.ToolBindings,
		"skillBindings": run.SkillBindings,
		"input":         run.Input,
		"outputSchema":  "Return concise text or JSON with summary, recommendations, evidence, hypotheses, toolExecutions, and analysisArtifacts. soha will normalize the final result into AnalysisArtifact.",
		"resultRule":    "Do not execute destructive actions. Use only read-only context and report evidence, hypotheses, recommendations, and next steps.",
		"toolCallRule":  "Tool bindings are authorization hints. Provider adapters must request actual tool execution through the soha runner tool-call gateway and must not call soha data sources directly.",
	}
	if len(run.ToolBindings) == 0 {
		delete(payload, "toolBindings")
	}
	if len(run.SkillBindings) == 0 {
		delete(payload, "skillBindings")
	}
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Sprintf("Run soha %s analysis for scope %v. Question: %s", run.CapabilityID, run.Scope, fmt.Sprint(run.Input["question"]))
	}
	return "You are executing a soha Agent Runtime analysis task. Analyze the provided context and return an operational report.\n\n" + string(encoded)
}

func agentToolBindingPrefetchable(binding map[string]any) bool {
	toolName := strings.TrimSpace(fmt.Sprint(binding["toolName"]))
	switch toolName {
	case "events.query", "logs.query", "metrics.query", "traces.query",
		"delivery.releases.list", "delivery.builds.list", "delivery.execution_tasks.list",
		"platform.resources.snapshot", "docker.operations.list", "docker.services.list",
		"virtualization.operations.list", "alerts.list", "oncall.routes.resolve":
		return true
	default:
		return false
	}
}

func agentToolCallResultMaps(items []AgentToolCallResult) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"runId":         item.RunID,
			"toolExecution": item.ToolExecution,
			"output":        item.Output,
		})
	}
	return out
}

func copyStringAnyMap(input map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range input {
		out[key] = value
	}
	return out
}

func joinCommandOutput(logs []string) string {
	lines := make([]string, 0, len(logs))
	for _, log := range logs {
		trimmed := strings.TrimSpace(log)
		if trimmed == "" || strings.HasPrefix(trimmed, "$ ") {
			continue
		}
		lines = append(lines, trimmed)
	}
	return strings.Join(lines, "\n")
}

func summarizeAgentOutput(output string) string {
	output = strings.TrimSpace(output)
	if output == "" {
		return ""
	}
	runes := []rune(output)
	if len(runes) <= 600 {
		return output
	}
	return string(runes[:600]) + "..."
}

func parseAgentOutputJSON(output string) map[string]any {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" || !strings.HasPrefix(trimmed, "{") {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return nil
	}
	return payload
}

func agentRunToolExecution(run AgentRun, startedAt, completedAt time.Time, status, summary string, output map[string]any) map[string]any {
	input := map[string]any{
		"capabilityId": run.CapabilityID,
		"skillIds":     run.SkillIDs,
		"scope":        run.Scope,
	}
	if len(run.ToolBindings) > 0 {
		input["toolBindings"] = run.ToolBindings
	}
	if len(run.SkillBindings) > 0 {
		input["skillBindings"] = run.SkillBindings
	}
	return map[string]any{
		"id":          "tool:" + run.ID,
		"adapterId":   "agent." + firstNonEmpty(run.ProviderKind, run.ProviderID),
		"toolName":    firstNonEmpty(run.ProviderKind, run.ProviderID) + ".analysis",
		"status":      status,
		"summary":     summary,
		"input":       input,
		"output":      output,
		"startedAt":   startedAt.Format(time.RFC3339),
		"completedAt": completedAt.Format(time.RFC3339),
	}
}

func agentRunArtifact(run AgentRun, output map[string]any, logs []string, toolExecutions []map[string]any) map[string]any {
	summary := firstNonEmpty(stringMapValue(output, "summary"), stringMapValue(output, "rawOutput"))
	if summary == "" {
		summary = fmt.Sprintf("%s analysis completed by %s", run.CapabilityID, run.ProviderID)
	}
	snapshot := mapValue(output["dataSourceSnapshot"])
	if snapshot == nil {
		snapshot = map[string]any{}
	}
	for key, value := range map[string]any{
		"providerId":   run.ProviderID,
		"providerKind": run.ProviderKind,
		"capabilityId": run.CapabilityID,
		"skillIds":     run.SkillIDs,
		"toolset":      run.Toolset,
		"logCount":     len(logs),
	} {
		if _, exists := snapshot[key]; !exists {
			snapshot[key] = value
		}
	}
	if len(run.ToolBindings) > 0 {
		if _, exists := snapshot["toolBindings"]; !exists {
			snapshot["toolBindings"] = run.ToolBindings
		}
	}
	if len(run.SkillBindings) > 0 {
		if _, exists := snapshot["skillBindings"]; !exists {
			snapshot["skillBindings"] = run.SkillBindings
		}
	}
	artifactToolExecutions := valueAsMapSlice(output["toolExecutions"])
	artifactToolExecutions = append(artifactToolExecutions, toolExecutions...)
	return map[string]any{
		"kind":       firstNonEmpty(run.CapabilityID, "agent_analysis"),
		"runId":      run.ID,
		"title":      fmt.Sprintf("%s analysis", firstNonEmpty(run.CapabilityID, "agent")),
		"summary":    summary,
		"scope":      run.Scope,
		"evidence":   mapSliceValue(output["evidence"]),
		"hypotheses": mapSliceValue(output["hypotheses"]),
		"recommendations": firstNonEmptyStringSlice(
			valueAsStringSlice(output["recommendations"]),
			valueAsStringSlice(output["nextSteps"]),
		),
		"toolExecutions":     artifactToolExecutions,
		"graph":              mapValue(output["graph"]),
		"dataSourceSnapshot": snapshot,
	}
}

func agentRunFailedArtifact(run AgentRun, output map[string]any, logs []string, toolExecutions []map[string]any, status, errorMessage string) map[string]any {
	if output == nil {
		output = map[string]any{}
	}
	next := map[string]any{}
	for key, value := range output {
		next[key] = value
	}
	next["summary"] = fmt.Sprintf("%s analysis %s: %s", firstNonEmpty(run.CapabilityID, "agent"), status, errorMessage)
	next["recommendations"] = firstNonEmptyStringSlice(
		valueAsStringSlice(next["recommendations"]),
		[]string{"Review the agent runtime logs, provider configuration, skill binding, and tool binding context before retrying."},
	)
	snapshot := mapValue(next["dataSourceSnapshot"])
	if snapshot == nil {
		snapshot = map[string]any{}
	}
	snapshot["status"] = status
	snapshot["error"] = errorMessage
	next["dataSourceSnapshot"] = snapshot
	return agentRunArtifact(run, next, logs, toolExecutions)
}

func redactAgentRuntimeLogs(logs []string) []string {
	if len(logs) == 0 {
		return nil
	}
	out := make([]string, 0, len(logs))
	for _, item := range logs {
		out = append(out, redactAgentRuntimeText(item))
	}
	return out
}

func redactAgentRuntimeValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if agentRuntimeSensitiveKey(key) {
				out[key] = "[REDACTED]"
				continue
			}
			out[key] = redactAgentRuntimeValue(item)
		}
		return out
	case []map[string]any:
		out := make([]map[string]any, 0, len(typed))
		for _, item := range typed {
			redacted, _ := redactAgentRuntimeValue(item).(map[string]any)
			out = append(out, redacted)
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, item := range typed {
			out = append(out, redactAgentRuntimeValue(item))
		}
		return out
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			out = append(out, redactAgentRuntimeText(item))
		}
		return out
	case string:
		return redactAgentRuntimeText(typed)
	default:
		return typed
	}
}

func agentRuntimeSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	for _, needle := range []string{"token", "password", "passwd", "secret", "credential", "apikey", "api_key", "authorization", "kubeconfig", "envvar", "environmentvariable"} {
		if strings.Contains(normalized, needle) {
			return true
		}
	}
	return false
}

var agentRuntimeSensitiveAssignments = regexp.MustCompile(`(?i)(token|password|passwd|secret|authorization|api[_-]?key)=([^ \t\n,;]+)`)

func redactAgentRuntimeText(value string) string {
	return agentRuntimeSensitiveAssignments.ReplaceAllString(value, "$1=[REDACTED]")
}

func buildHeartbeatPayload(agentID, command string, commandIndex, commandCount int) map[string]any {
	return map[string]any{
		"agentId":        strings.TrimSpace(agentID),
		"heartbeatAt":    time.Now().UTC().Format(time.RFC3339),
		"currentCommand": strings.TrimSpace(command),
		"commandIndex":   commandIndex,
		"commandCount":   commandCount,
	}
}

func extendMap(base, overlay map[string]any) map[string]any {
	next := map[string]any{}
	for key, value := range base {
		next[key] = value
	}
	for key, value := range overlay {
		if value == nil || value == "" {
			continue
		}
		next[key] = value
	}
	return next
}

func valueAsStringSlice(raw any) []string {
	switch value := raw.(type) {
	case []string:
		items := make([]string, 0, len(value))
		for _, item := range value {
			if trimmed := strings.TrimSpace(item); trimmed != "" {
				items = append(items, trimmed)
			}
		}
		return items
	case []any:
		items := make([]string, 0, len(value))
		for _, item := range value {
			if trimmed := strings.TrimSpace(fmt.Sprint(item)); trimmed != "" {
				items = append(items, trimmed)
			}
		}
		return items
	default:
		return nil
	}
}

func mapSliceValue(raw any) []map[string]any {
	switch value := raw.(type) {
	case []map[string]any:
		return append([]map[string]any(nil), value...)
	case []any:
		items := make([]map[string]any, 0, len(value))
		for _, item := range value {
			if current, ok := item.(map[string]any); ok {
				items = append(items, current)
			}
		}
		return items
	default:
		return nil
	}
}

func valueAsMapSlice(raw any) []map[string]any {
	switch value := raw.(type) {
	case []map[string]any:
		return value
	case []any:
		items := make([]map[string]any, 0, len(value))
		for _, item := range value {
			mapped, ok := item.(map[string]any)
			if ok {
				items = append(items, mapped)
			}
		}
		return items
	default:
		return nil
	}
}

func mapValue(raw any) map[string]any {
	switch value := raw.(type) {
	case map[string]any:
		if len(value) == 0 {
			return nil
		}
		return value
	default:
		return nil
	}
}

func boolValue(raw any, fallback bool) bool {
	switch value := raw.(type) {
	case bool:
		return value
	case string:
		switch strings.TrimSpace(strings.ToLower(value)) {
		case "true", "1", "yes", "y", "on":
			return true
		case "false", "0", "no", "n", "off":
			return false
		default:
			return fallback
		}
	default:
		return fallback
	}
}

func shouldStopLocalExecution(status string) bool {
	switch strings.TrimSpace(status) {
	case "canceled", "callback_timeout", "failed", "completed":
		return true
	default:
		return false
	}
}

func isFinalStatus(status string) bool {
	return shouldStopLocalExecution(status)
}

func durationFromPayload(payload map[string]any, keys ...string) time.Duration {
	for _, key := range keys {
		raw, ok := payload[key]
		if !ok || raw == nil {
			continue
		}
		if duration := durationFromAny(raw, key); duration > 0 {
			return duration
		}
	}
	return 0
}

func durationFromAny(raw any, key string) time.Duration {
	switch value := raw.(type) {
	case time.Duration:
		return value
	case int:
		return secondsDuration(value, key)
	case int64:
		return secondsDuration(value, key)
	case int32:
		return secondsDuration(value, key)
	case float64:
		return secondsDuration(value, key)
	case float32:
		return secondsDuration(value, key)
	case string:
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return 0
		}
		if duration, err := time.ParseDuration(trimmed); err == nil {
			return duration
		}
		seconds, err := strconv.ParseFloat(trimmed, 64)
		if err != nil {
			return 0
		}
		return secondsDuration(seconds, key)
	default:
		return 0
	}
}

func secondsDuration(value any, key string) time.Duration {
	var seconds float64
	switch typed := value.(type) {
	case int:
		seconds = float64(typed)
	case int64:
		seconds = float64(typed)
	case int32:
		seconds = float64(typed)
	case float64:
		seconds = typed
	case float32:
		seconds = float64(typed)
	default:
		return 0
	}
	if seconds <= 0 {
		return 0
	}
	duration := time.Duration(seconds * float64(time.Second))
	if strings.Contains(strings.ToLower(key), "millis") {
		duration = time.Duration(seconds * float64(time.Millisecond))
	}
	return duration
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func ceilDurationSeconds(duration time.Duration) int {
	if duration <= 0 {
		return 0
	}
	seconds := int(duration / time.Second)
	if duration%time.Second != 0 {
		seconds++
	}
	return seconds
}

func dockerWorkerID(cfg cfgpkg.ControlPlaneConfig) string {
	return firstNonEmpty(strings.TrimSpace(cfg.Docker.WorkerID), strings.TrimSpace(cfg.AgentID), "local-docker-runner")
}

func dockerOperationKindAllowed(allowedKinds []string, operationKind string) bool {
	normalizedKind := strings.TrimSpace(operationKind)
	if normalizedKind == "" {
		return false
	}
	for _, allowed := range normalizeDockerOperationKinds(allowedKinds) {
		if allowed == "*" || allowed == normalizedKind {
			return true
		}
	}
	return false
}

func normalizeDockerOperationKinds(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		normalized := strings.TrimSpace(value)
		if normalized == "" {
			continue
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func dockerRuntimePayload(ctx context.Context, cfg cfgpkg.ControlPlaneConfig, extra map[string]any) map[string]any {
	hostArch := normalizeRunnerArchitecture(runtime.GOARCH)
	dockerArch := dockerArchitecture(ctx)
	payload := map[string]any{
		"agentId":            firstNonEmpty(strings.TrimSpace(cfg.AgentID), "local-agent"),
		"workerId":           dockerWorkerID(cfg),
		"endpoint":           strings.TrimSpace(cfg.RuntimeEndpoint),
		"hostArchitecture":   hostArch,
		"dockerArchitecture": dockerArch,
		"architecture":       firstNonEmpty(dockerArch, hostArch),
		"dockerVersion":      commandVersion(ctx, "docker", "--version"),
		"composeVersion":     dockerComposeVersion(ctx),
	}
	for key, value := range extra {
		payload[key] = value
	}
	return payload
}

func commandVersion(ctx context.Context, name string, args ...string) string {
	commandLogs, err := runCommand(ctx, "", name, args...)
	if err != nil || len(commandLogs) == 0 {
		return ""
	}
	for _, line := range commandLogs {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "$ ") {
			continue
		}
		return trimmed
	}
	return ""
}

func dockerComposeVersion(ctx context.Context) string {
	return commandVersion(ctx, "docker", "compose", "version")
}

func dockerArchitecture(ctx context.Context) string {
	value := commandVersion(ctx, "docker", "info", "--format", "{{.Architecture}}")
	return normalizeRunnerArchitecture(value)
}

func normalizeRunnerArchitecture(value string) string {
	arch := strings.ToLower(strings.TrimSpace(value))
	arch = strings.TrimPrefix(arch, "linux/")
	switch arch {
	case "amd64", "x86_64", "x64", "x86":
		return "amd64"
	case "arm64", "aarch64":
		return "arm64"
	default:
		return ""
	}
}

func composeServiceStatus(record map[string]any) string {
	state := strings.ToLower(firstNonEmpty(strings.TrimSpace(fmt.Sprint(record["State"])), strings.TrimSpace(fmt.Sprint(record["Status"]))))
	switch {
	case strings.Contains(state, "running"):
		return "running"
	case strings.Contains(state, "exited"), strings.Contains(state, "stopped"):
		return "stopped"
	case strings.Contains(state, "paused"):
		return "paused"
	case state == "":
		return "unknown"
	default:
		return state
	}
}

func dockerServiceRecordsFromJSON(raw string) []map[string]any {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return nil
	}
	var records []map[string]any
	if strings.HasPrefix(trimmed, "[") {
		_ = json.Unmarshal([]byte(trimmed), &records)
	} else {
		record := map[string]any{}
		if err := json.Unmarshal([]byte(trimmed), &record); err == nil {
			records = []map[string]any{record}
		}
	}
	out := make([]map[string]any, 0, len(records))
	for _, record := range records {
		name := firstNonEmpty(strings.TrimSpace(fmt.Sprint(record["Service"])), strings.TrimSpace(fmt.Sprint(record["Name"])))
		if name == "" {
			continue
		}
		out = append(out, map[string]any{
			"name":        name,
			"image":       firstNonEmpty(strings.TrimSpace(fmt.Sprint(record["Image"])), strings.TrimSpace(fmt.Sprint(record["Repository"]))),
			"status":      composeServiceStatus(record),
			"containerId": strings.TrimSpace(fmt.Sprint(record["ID"])),
			"config":      record,
		})
	}
	return out
}

func drainStopReason(stopReason <-chan string) string {
	select {
	case reason := <-stopReason:
		return strings.TrimSpace(reason)
	default:
		return ""
	}
}

func (r *Runner) registerActiveTask(task ExecutionTask, cancel context.CancelFunc) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now().UTC()
	r.active[strings.TrimSpace(task.ID)] = &activeTaskState{
		snapshot: ActiveTask{
			TaskID:                   task.ID,
			ApplicationID:            task.ApplicationID,
			ApplicationEnvironmentID: task.ApplicationEnvironmentID,
			TaskKind:                 task.TaskKind,
			ProviderKind:             task.ProviderKind,
			Status:                   "queued",
			StartedAt:                now,
			UpdatedAt:                now,
		},
		cancel: cancel,
	}
}

func (r *Runner) updateActiveTask(taskID string, mutate func(item *ActiveTask)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.active[strings.TrimSpace(taskID)]
	if !ok || item == nil {
		return
	}
	mutate(&item.snapshot)
	item.snapshot.UpdatedAt = time.Now().UTC()
}

func (r *Runner) unregisterActiveTask(taskID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.active, strings.TrimSpace(taskID))
}

func (r *Runner) stopInfo(taskID string) (string, string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, ok := r.active[strings.TrimSpace(taskID)]
	if !ok || item == nil {
		return "", ""
	}
	return item.snapshot.StopSource, item.snapshot.StopReason
}

func shouldStopDockerExecution(status string) bool {
	return strings.TrimSpace(status) == "canceling" || shouldStopLocalExecution(status)
}

func (r *Runner) acknowledgeDockerCancellation(ctx context.Context, operation DockerOperation, status string) {
	if status != "canceling" && status != "canceled" {
		return
	}
	finalCtx, cancel := r.finalCallbackContext(ctx)
	defer cancel()
	r.dockerCallback(finalCtx, operation, "canceled", map[string]any{"cancellationAcknowledged": true}, []string{"runner commands stopped; applied runtime changes are retained"})
}
