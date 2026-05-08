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
	"strings"
	"sync"
	"time"

	cfgpkg "github.com/kubecrux/kubecrux/internal/agent/config"
	"go.uber.org/zap"
)

const (
	commandHeartbeatInterval = 10 * time.Second
	runnerStatusPollInterval = 2 * time.Second
)

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

type claimRequest struct {
	AgentID         string   `json:"agentId"`
	ProviderKinds   []string `json:"providerKinds"`
	RuntimeEndpoint string   `json:"runtimeEndpoint"`
}

type claimResponse struct {
	Data ExecutionTask `json:"data"`
}

type callbackRequest struct {
	CallbackToken string         `json:"callbackToken"`
	Status        string         `json:"status"`
	Payload       map[string]any `json:"payload"`
}

type callbackResponse struct {
	Data ExecutionTask `json:"data"`
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

func firstNonEmptyStringSlice(candidates ...[]string) []string {
	for _, items := range candidates {
		if len(items) > 0 {
			return items
		}
	}
	return nil
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
