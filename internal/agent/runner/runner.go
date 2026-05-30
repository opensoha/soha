package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	cfgpkg "github.com/soha/soha/internal/agent/config"
	"go.uber.org/zap"
)

const (
	commandHeartbeatInterval = 10 * time.Second
	runnerStatusPollInterval = 2 * time.Second
)

var agentRunHeartbeatInterval = commandHeartbeatInterval

type ExecutionTask struct {
	ID                       string         `json:"id"`
	ApplicationID            string         `json:"applicationId"`
	ApplicationEnvironmentID string         `json:"applicationEnvironmentId"`
	TaskKind                 string         `json:"taskKind"`
	ProviderKind             string         `json:"providerKind"`
	Status                   string         `json:"status"`
	CallbackToken            string         `json:"callbackToken"`
	Payload                  map[string]any `json:"payload"`
}

type DockerOperation struct {
	ID                string         `json:"id"`
	HostID            string         `json:"hostId,omitempty"`
	ProjectID         string         `json:"projectId,omitempty"`
	ServiceID         string         `json:"serviceId,omitempty"`
	OperationKind     string         `json:"operationKind"`
	Status            string         `json:"status"`
	ClaimedByWorkerID string         `json:"claimedByWorkerId,omitempty"`
	Payload           map[string]any `json:"payload"`
	TimeoutSeconds    int            `json:"timeoutSeconds"`
}

type AgentRun struct {
	ID                string           `json:"id"`
	ProviderID        string           `json:"providerId"`
	ProviderKind      string           `json:"providerKind"`
	CapabilityID      string           `json:"capabilityId"`
	SkillIDs          []string         `json:"skillIds,omitempty"`
	SessionID         string           `json:"sessionId,omitempty"`
	Status            string           `json:"status"`
	Scope             map[string]any   `json:"scope,omitempty"`
	Toolset           map[string]any   `json:"toolset,omitempty"`
	ToolBindings      []map[string]any `json:"toolBindings,omitempty"`
	SkillBindings     []map[string]any `json:"skillBindings,omitempty"`
	Input             map[string]any   `json:"input,omitempty"`
	Output            map[string]any   `json:"output,omitempty"`
	CallbackToken     string           `json:"callbackToken"`
	TimeoutSeconds    int              `json:"timeoutSeconds"`
	AnalysisArtifacts []map[string]any `json:"analysisArtifacts,omitempty"`
}

func (r AgentRun) withPrefetchedToolContext(items []AgentToolCallResult) AgentRun {
	if len(items) == 0 {
		return r
	}
	input := copyStringAnyMap(r.Input)
	input["prefetchedToolResults"] = agentToolCallResultMaps(items)
	input["toolContextRule"] = "Use prefetchedToolResults as soha-controlled read-only evidence. Do not call soha data sources directly."
	r.Input = input
	return r
}

type claimRequest struct {
	AgentID         string   `json:"agentId"`
	ProviderKinds   []string `json:"providerKinds"`
	RuntimeEndpoint string   `json:"runtimeEndpoint"`
}

type agentRunClaimRequest struct {
	AgentID     string   `json:"agentId"`
	ProviderIDs []string `json:"providerIds,omitempty"`
	Kinds       []string `json:"kinds,omitempty"`
}

type dockerClaimRequest struct {
	WorkerID       string   `json:"workerId"`
	AgentID        string   `json:"agentId"`
	HostIDs        []string `json:"hostIds,omitempty"`
	OperationKinds []string `json:"operationKinds,omitempty"`
}

type claimResponse struct {
	Data ExecutionTask `json:"data"`
}

type dockerClaimResponse struct {
	Data DockerOperation `json:"data"`
}

type agentRunClaimResponse struct {
	Data AgentRun `json:"data"`
}

type callbackRequest struct {
	CallbackToken string         `json:"callbackToken"`
	Status        string         `json:"status"`
	Payload       map[string]any `json:"payload"`
}

type agentRunCallbackRequest struct {
	RunID             string           `json:"runId"`
	CallbackToken     string           `json:"callbackToken"`
	AgentID           string           `json:"agentId"`
	Status            string           `json:"status"`
	Payload           map[string]any   `json:"payload"`
	ToolExecutions    []map[string]any `json:"toolExecutions,omitempty"`
	AnalysisArtifacts []map[string]any `json:"analysisArtifacts,omitempty"`
	ExternalRunID     string           `json:"externalRunId,omitempty"`
	ErrorMessage      string           `json:"errorMessage,omitempty"`
}

type agentRunToolCallRequest struct {
	RunID         string         `json:"runId"`
	CallbackToken string         `json:"callbackToken"`
	AgentID       string         `json:"agentId"`
	ToolBindingID string         `json:"toolBindingId,omitempty"`
	AdapterID     string         `json:"adapterId,omitempty"`
	ToolName      string         `json:"toolName,omitempty"`
	Input         map[string]any `json:"input,omitempty"`
}

type dockerCallbackRequest struct {
	OperationID string         `json:"operationId"`
	WorkerID    string         `json:"workerId"`
	Status      string         `json:"status"`
	Payload     map[string]any `json:"payload"`
	Logs        []string       `json:"logs"`
}

type callbackResponse struct {
	Data ExecutionTask `json:"data"`
}

type dockerCallbackResponse struct {
	Data DockerOperation `json:"data"`
}

type agentRunCallbackResponse struct {
	Data AgentRun `json:"data"`
}

type AgentToolCallResult struct {
	RunID         string         `json:"runId"`
	ToolExecution map[string]any `json:"toolExecution"`
	Output        map[string]any `json:"output,omitempty"`
}

type agentRunToolCallResponse struct {
	Data AgentToolCallResult `json:"data"`
}

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
	cfg        cfgpkg.ControlPlaneConfig
	httpClient *http.Client
	logger     *zap.Logger
	mu         sync.RWMutex
	active     map[string]*activeTaskState
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

func New(cfg cfgpkg.ControlPlaneConfig, logger *zap.Logger) *Runner {
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = 5 * time.Second
	}
	return &Runner{
		cfg: cfg,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
		logger: logger,
		active: map[string]*activeTaskState{},
	}
}

func (r *Runner) Start(ctx context.Context) {
	if !r.cfg.Enabled || strings.TrimSpace(r.cfg.BaseURL) == "" || strings.TrimSpace(r.cfg.BearerToken) == "" {
		return
	}
	go r.loop(ctx)
	if r.cfg.Docker.Enabled {
		go r.dockerLoop(ctx)
	}
	if r.cfg.AgentRuntime.Enabled {
		go r.agentRuntimeLoop(ctx)
	}
}

func (r *Runner) loop(ctx context.Context) {
	ticker := time.NewTicker(r.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			task, ok := r.claim(ctx)
			if !ok {
				continue
			}
			r.execute(ctx, task)
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
			run, ok := r.claimAgentRun(ctx)
			if !ok {
				continue
			}
			r.executeAgentRun(ctx, run)
		}
	}
}

func (r *Runner) claim(ctx context.Context) (ExecutionTask, bool) {
	body, _ := json.Marshal(claimRequest{
		AgentID:         firstNonEmpty(strings.TrimSpace(r.cfg.AgentID), "local-agent"),
		ProviderKinds:   r.cfg.ProviderKinds,
		RuntimeEndpoint: strings.TrimSpace(r.cfg.RuntimeEndpoint),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(r.cfg.BaseURL, "/")+"/api/v1/delivery/execution-tasks/claim", bytes.NewReader(body))
	if err != nil {
		return ExecutionTask{}, false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.cfg.BearerToken)
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return ExecutionTask{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return ExecutionTask{}, false
	}
	var payload claimResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ExecutionTask{}, false
	}
	if strings.TrimSpace(payload.Data.ID) == "" {
		return ExecutionTask{}, false
	}
	return payload.Data, true
}

func (r *Runner) claimAgentRun(ctx context.Context) (AgentRun, bool) {
	body, _ := json.Marshal(agentRunClaimRequest{
		AgentID:     agentRuntimeWorkerID(r.cfg),
		ProviderIDs: r.cfg.AgentRuntime.ProviderIDs,
		Kinds:       r.cfg.AgentRuntime.ProviderKinds,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(r.cfg.BaseURL, "/")+"/api/v1/copilot/agent-runs/claim", bytes.NewReader(body))
	if err != nil {
		return AgentRun{}, false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.cfg.BearerToken)
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return AgentRun{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return AgentRun{}, false
	}
	var payload agentRunClaimResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return AgentRun{}, false
	}
	if strings.TrimSpace(payload.Data.ID) == "" {
		return AgentRun{}, false
	}
	return payload.Data, true
}

func (r *Runner) claimDockerOperation(ctx context.Context) (DockerOperation, bool) {
	workerID := dockerWorkerID(r.cfg)
	body, _ := json.Marshal(dockerClaimRequest{
		WorkerID:       workerID,
		AgentID:        firstNonEmpty(strings.TrimSpace(r.cfg.AgentID), "local-agent"),
		HostIDs:        r.cfg.Docker.HostIDs,
		OperationKinds: r.cfg.Docker.OperationKinds,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(r.cfg.BaseURL, "/")+"/api/v1/docker/operations/claim", bytes.NewReader(body))
	if err != nil {
		return DockerOperation{}, false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.cfg.BearerToken)
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return DockerOperation{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return DockerOperation{}, false
	}
	var payload dockerClaimResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return DockerOperation{}, false
	}
	if strings.TrimSpace(payload.Data.ID) == "" {
		return DockerOperation{}, false
	}
	return payload.Data, true
}

func (r *Runner) execute(ctx context.Context, task ExecutionTask) {
	taskCtx, cancelTask := context.WithCancel(ctx)
	defer cancelTask()
	commands := extractCommands(task.Payload)
	if len(commands) == 0 {
		r.callback(taskCtx, task, "failed", map[string]any{
			"logs":  []string{"no executable commands were found in task payload"},
			"error": "no executable commands were found in task payload",
		})
		return
	}

	agentID := firstNonEmpty(strings.TrimSpace(r.cfg.AgentID), "local-agent")
	logs := make([]string, 0, len(commands)*3)
	commandCount := len(commands)
	r.registerActiveTask(task, cancelTask)
	defer r.unregisterActiveTask(task.ID)
	r.updateActiveTask(task.ID, func(item *ActiveTask) {
		item.Status = "preparing"
	})

	workspacePath, commandDir, workspaceArtifacts, workspaceLogs, workspaceErr := r.prepareWorkspace(taskCtx, task)
	if len(workspaceLogs) > 0 {
		logs = append(logs, workspaceLogs...)
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
		r.updateActiveTask(task.ID, func(item *ActiveTask) {
			item.Status = "failed"
			item.WorkspacePath = workspacePath
			item.StopReason = workspaceErr.Error()
		})
		r.callback(taskCtx, task, "failed", map[string]any{
			"logs":          []string{workspaceErr.Error()},
			"error":         workspaceErr.Error(),
			"agentId":       agentID,
			"workspacePath": workspacePath,
		})
		return
	}

	for index, command := range commands {
		r.updateActiveTask(task.ID, func(item *ActiveTask) {
			item.Status = "running"
			item.CurrentCommand = command
			item.CommandIndex = index + 1
			item.CommandCount = commandCount
			item.WorkspacePath = workspacePath
		})
		remoteTask, ok := r.callback(taskCtx, task, "running", extendMap(
			buildHeartbeatPayload(agentID, command, index+1, commandCount),
			map[string]any{"workspacePath": workspacePath},
		))
		if ok && shouldStopLocalExecution(remoteTask.Status) {
			return
		}
		commandLogs := []string{"$ " + command}
		logs = append(logs, commandLogs[0])

		commandCtx, cancelCommand := context.WithCancel(taskCtx)
		cmd := exec.CommandContext(commandCtx, "/bin/sh", "-lc", command)
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
			logs = append(logs, value)
		}
		if value := strings.TrimSpace(stderr.String()); value != "" {
			commandLogs = append(commandLogs, value)
			logs = append(logs, value)
		}
		remoteTask, ok = r.callback(taskCtx, task, "running", extendMap(
			buildHeartbeatPayload(agentID, command, index+1, commandCount),
			map[string]any{
				"logs":          commandLogs,
				"workspacePath": workspacePath,
			},
		))
		if ok && shouldStopLocalExecution(remoteTask.Status) {
			return
		}
		if remoteStatus != "" {
			stopSource, stopReason := r.stopInfo(task.ID)
			if stopSource == "local_api" {
				r.updateActiveTask(task.ID, func(item *ActiveTask) {
					item.Status = "canceled"
					item.StopSource = stopSource
					item.StopReason = stopReason
				})
				r.callback(ctx, task, "canceled", map[string]any{
					"agentId":       agentID,
					"workspacePath": workspacePath,
					"canceledAt":    time.Now().UTC().Format(time.RFC3339),
					"cancelReason":  stopReason,
				})
			}
			return
		}
		if err != nil {
			if errors.Is(err, context.Canceled) && remoteStatus != "" {
				return
			}
			r.updateActiveTask(task.ID, func(item *ActiveTask) {
				item.Status = "failed"
				item.StopReason = err.Error()
			})
			r.callback(taskCtx, task, "failed", map[string]any{
				"logs":           []string{fmt.Sprintf("command failed: %v", err)},
				"error":          err.Error(),
				"agentId":        agentID,
				"currentCommand": command,
				"workspacePath":  workspacePath,
			})
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
		payload["artifact"] = buildImageArtifact(task.Payload, image)
		payload["artifacts"] = buildArtifactList(task.Payload, image)
	}
	if len(workspaceArtifacts) > 0 {
		payload["workspaceArtifacts"] = workspaceArtifacts
	}
	r.updateActiveTask(task.ID, func(item *ActiveTask) {
		item.Status = "completed"
		item.CurrentCommand = ""
		item.StopReason = ""
	})
	r.callback(taskCtx, task, "completed", payload)
}

func (r *Runner) executeDockerOperation(ctx context.Context, operation DockerOperation) {
	workerID := dockerWorkerID(r.cfg)
	logs := []string{fmt.Sprintf("docker operation %s started: %s", operation.ID, operation.OperationKind)}
	r.dockerCallback(ctx, operation, "running", map[string]any{
		"agentId":        firstNonEmpty(strings.TrimSpace(r.cfg.AgentID), "local-agent"),
		"workerId":       workerID,
		"heartbeatAt":    time.Now().UTC().Format(time.RFC3339),
		"dockerVersion":  commandVersion(ctx, "docker", "--version"),
		"composeVersion": dockerComposeVersion(ctx),
	}, logs)

	var err error
	var commandLogs []string
	switch operation.OperationKind {
	case "container_start", "project_deploy":
		commandLogs, err = r.executeComposeAction(ctx, operation)
	case "service_action":
		commandLogs, err = r.executeComposeServiceAction(ctx, operation)
	case "port_reserve":
		commandLogs = []string{"port mapping reserved in control plane; no local Docker command required"}
	case "host_sync":
		commandLogs = []string{"host runtime heartbeat reported by docker runner"}
	default:
		err = fmt.Errorf("unsupported docker operation kind %q", operation.OperationKind)
	}
	logs = append(logs, commandLogs...)
	if err != nil {
		r.dockerCallback(ctx, operation, "failed", map[string]any{
			"agentId":  firstNonEmpty(strings.TrimSpace(r.cfg.AgentID), "local-agent"),
			"workerId": workerID,
			"error":    err.Error(),
		}, append(logs, "docker operation failed: "+err.Error()))
		return
	}

	payload := map[string]any{
		"agentId":        firstNonEmpty(strings.TrimSpace(r.cfg.AgentID), "local-agent"),
		"workerId":       workerID,
		"completedAt":    time.Now().UTC().Format(time.RFC3339),
		"dockerVersion":  commandVersion(ctx, "docker", "--version"),
		"composeVersion": dockerComposeVersion(ctx),
	}
	if operation.ProjectID != "" {
		services, serviceErr := r.collectComposeServices(ctx, operation)
		if serviceErr == nil && len(services) > 0 {
			payload["services"] = services
		}
	}
	r.dockerCallback(ctx, operation, "completed", payload, logs)
}

func (r *Runner) executeAgentRun(ctx context.Context, run AgentRun) {
	workerID := agentRuntimeWorkerID(r.cfg)
	startedAt := time.Now().UTC()
	if remoteRun, ok := r.agentRunCallback(ctx, run, "running", map[string]any{
		"agentId":     firstNonEmpty(strings.TrimSpace(r.cfg.AgentID), "local-agent"),
		"workerId":    workerID,
		"heartbeatAt": startedAt.Format(time.RFC3339),
		"providerId":  run.ProviderID,
	}, nil, nil, "", ""); ok && shouldStopLocalExecution(remoteRun.Status) {
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

type agentProviderCommandSpec struct {
	Command          string
	Args             []string
	PromptArg        string
	SkillArg         string
	ProviderSkillArg string
}

func (r *Runner) resolveAgentProviderExecutor(run AgentRun) agentProviderExecutor {
	providerKey := normalizedAgentProviderKey(run)
	switch providerKey {
	case "hermes":
		return r.executeHermesAgentRun
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
		return func(ctx context.Context, current AgentRun) (map[string]any, []string, error) {
			return r.executeCLIAgentRun(ctx, current, spec)
		}
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
		run = run.withPrefetchedToolContext(prefetchedTools)
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
	dir, logs, err := r.prepareComposeWorkspace(operation)
	if err != nil {
		return logs, err
	}
	args := composeArgsForAction(action)
	if len(args) == 0 {
		return logs, fmt.Errorf("unsupported compose action %q", action)
	}
	commandLogs, err := runCommand(ctx, dir, "docker", args...)
	logs = append(logs, commandLogs...)
	return logs, err
}

func (r *Runner) executeComposeServiceAction(ctx context.Context, operation DockerOperation) ([]string, error) {
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
	commandLogs, err := runCommand(ctx, dir, "docker", args...)
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
	dir, _, err := r.prepareComposeWorkspace(operation)
	if err != nil {
		return nil, err
	}
	commandLogs, err := runCommand(ctx, dir, "docker", "compose", "-f", "compose.yaml", "ps", "--format", "json")
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
			if ok && shouldStopLocalExecution(remoteTask.Status) {
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
	body, _ := json.Marshal(callbackRequest{
		CallbackToken: task.CallbackToken,
		Status:        status,
		Payload:       payload,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(r.cfg.BaseURL, "/")+"/api/v1/delivery/execution-callbacks", bytes.NewReader(body))
	if err != nil {
		return ExecutionTask{}, false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return ExecutionTask{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return ExecutionTask{}, false
	}
	var result callbackResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ExecutionTask{}, false
	}
	if strings.TrimSpace(result.Data.ID) == "" {
		return ExecutionTask{}, false
	}
	return result.Data, true
}

func (r *Runner) dockerCallback(ctx context.Context, operation DockerOperation, status string, payload map[string]any, logs []string) (DockerOperation, bool) {
	body, _ := json.Marshal(dockerCallbackRequest{
		OperationID: operation.ID,
		WorkerID:    dockerWorkerID(r.cfg),
		Status:      status,
		Payload:     payload,
		Logs:        logs,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(r.cfg.BaseURL, "/")+"/api/v1/docker/operation-callbacks", bytes.NewReader(body))
	if err != nil {
		return DockerOperation{}, false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.cfg.BearerToken)
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return DockerOperation{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return DockerOperation{}, false
	}
	var result dockerCallbackResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return DockerOperation{}, false
	}
	if strings.TrimSpace(result.Data.ID) == "" {
		return DockerOperation{}, false
	}
	return result.Data, true
}

func (r *Runner) agentRunCallback(ctx context.Context, run AgentRun, status string, payload map[string]any, toolExecutions []map[string]any, analysisArtifacts []map[string]any, externalRunID string, errorMessage string) (AgentRun, bool) {
	body, _ := json.Marshal(agentRunCallbackRequest{
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(r.cfg.BaseURL, "/")+"/api/v1/copilot/agent-runs/callback", bytes.NewReader(body))
	if err != nil {
		return AgentRun{}, false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.cfg.BearerToken)
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return AgentRun{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return AgentRun{}, false
	}
	var result agentRunCallbackResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return AgentRun{}, false
	}
	if strings.TrimSpace(result.Data.ID) == "" {
		return AgentRun{}, false
	}
	return result.Data, true
}

func (r *Runner) agentRunToolCall(ctx context.Context, run AgentRun, binding map[string]any, input map[string]any) (AgentToolCallResult, bool) {
	body, _ := json.Marshal(agentRunToolCallRequest{
		RunID:         run.ID,
		CallbackToken: run.CallbackToken,
		AgentID:       agentRuntimeWorkerID(r.cfg),
		ToolBindingID: strings.TrimSpace(fmt.Sprint(binding["id"])),
		AdapterID:     strings.TrimSpace(fmt.Sprint(binding["adapterId"])),
		ToolName:      strings.TrimSpace(fmt.Sprint(binding["toolName"])),
		Input:         input,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(r.cfg.BaseURL, "/")+"/api/v1/copilot/agent-runs/tool-call", bytes.NewReader(body))
	if err != nil {
		return AgentToolCallResult{}, false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.cfg.BearerToken)
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return AgentToolCallResult{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return AgentToolCallResult{}, false
	}
	var result agentRunToolCallResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return AgentToolCallResult{}, false
	}
	if strings.TrimSpace(result.Data.RunID) == "" {
		return AgentToolCallResult{}, false
	}
	return result.Data, true
}

func (r *Runner) fetchRunnerTaskStatus(ctx context.Context, taskID string) (ExecutionTask, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(r.cfg.BaseURL, "/")+"/api/v1/delivery/execution-tasks/"+strings.TrimSpace(taskID)+"/runner-status", nil)
	if err != nil {
		return ExecutionTask{}, false
	}
	req.Header.Set("Authorization", "Bearer "+r.cfg.BearerToken)
	resp, err := r.httpClient.Do(req)
	if err != nil {
		return ExecutionTask{}, false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return ExecutionTask{}, false
	}
	var payload callbackResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return ExecutionTask{}, false
	}
	if strings.TrimSpace(payload.Data.ID) == "" {
		return ExecutionTask{}, false
	}
	return payload.Data, true
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
	if providerKey == "hermes" {
		command := strings.TrimSpace(r.cfg.AgentRuntime.HermesCommand)
		if command == "" {
			command = "hermes"
		}
		return agentProviderCommandSpec{Command: command, Args: []string{"chat", "-Q"}, PromptArg: "-q", ProviderSkillArg: "-s"}
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

func dockerWorkerID(cfg cfgpkg.ControlPlaneConfig) string {
	return firstNonEmpty(strings.TrimSpace(cfg.Docker.WorkerID), strings.TrimSpace(cfg.AgentID), "local-docker-runner")
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
