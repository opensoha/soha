package runner

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

var buildpacksOutputImage = regexp.MustCompile(`^[a-z0-9][a-z0-9._:/-]*:[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)

func (r *Runner) executeClaimedTask(ctx context.Context, task ExecutionTask) {
	if strings.HasPrefix(task.ProviderKind, "buildpacks_runner.") {
		r.executeBuildpacksTask(ctx, task)
		return
	}
	r.execute(ctx, task)
}

func (r *Runner) executeBuildpacksTask(ctx context.Context, task ExecutionTask) {
	r.metrics.markStarted(metricScopeExecution)
	secretCtx, err := r.redeemSecretLease(ctx, task.SecretLease, r.cfg.AgentID)
	if err != nil {
		r.finalCallback(ctx, task, "failed", map[string]any{"error": "Buildpacks secret lease could not be redeemed"})
		r.metrics.markOutcome(metricScopeExecution, "failed")
		return
	}
	task.SecretLease = nil
	taskCtx, cancel := r.executionTaskContext(secretCtx, task)
	defer cancel()
	r.registerActiveTask(task, cancel)
	defer r.unregisterActiveTask(task.ID)
	result, runErr := r.runBuildpacksWithHeartbeat(taskCtx, task)
	status := "completed"
	if runErr != nil {
		status = "failed"
		result["error"] = runErr.Error()
	}
	if result["stopUnconfirmed"] == true {
		// Keep the task nonterminal until runtime cleanup is actually confirmed.
		_, _ = r.callback(context.WithoutCancel(secretCtx), task, "canceling", result)
		return
	}
	if errors.Is(runErr, context.DeadlineExceeded) || errors.Is(taskCtx.Err(), context.DeadlineExceeded) {
		status, result["error"] = "callback_timeout", "Buildpacks execution timed out"
	} else if errors.Is(runErr, context.Canceled) || errors.Is(taskCtx.Err(), context.Canceled) {
		status, result["error"] = "canceled", "Buildpacks execution canceled"
	}
	result["agentId"] = r.cfg.AgentID
	r.finalCallback(context.WithoutCancel(secretCtx), task, status, result)
	r.metrics.markOutcome(metricScopeExecution, status)
}

func (r *Runner) runBuildpacksWithHeartbeat(ctx context.Context, task ExecutionTask) (map[string]any, error) {
	runCtx, cancel := context.WithCancel(ctx)
	done, stopReason := make(chan struct{}), make(chan string, 1)
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		r.streamHeartbeats(runCtx, cancel, done, stopReason, task, r.cfg.AgentID, "Buildpacks", 1, 1, "")
	}()
	go func() { defer workers.Done(); r.watchRunnerStatus(runCtx, cancel, done, stopReason, task) }()
	defer func() { close(done); cancel(); workers.Wait() }()
	result, err := r.runBuildpacks(runCtx, task)
	if result == nil {
		result = map[string]any{}
	}
	if runCtx.Err() != nil {
		return result, runCtx.Err()
	}
	return result, err
}

func (r *Runner) validateBuildpacksTask(ctx context.Context, task ExecutionTask) (sohaapi.BuildpacksExecutionSpec, string, error) {
	spec, err := decodeBuildpacksSpec(task)
	if err != nil {
		return spec, "", err
	}
	capability := r.BuildpacksCapability(ctx, task.ApplicationID)
	if !capability.Ready || task.ProviderKind != capability.ProviderKind {
		return spec, "", fmt.Errorf("buildpacks runner is unavailable for this application")
	}
	configured := capability.Configuration
	if firstNonEmpty(string(spec.Runtime), "pack") != firstNonEmpty(string(capability.Runtime), "pack") || spec.RuntimeVersion != capability.RuntimeVersion {
		return spec, "", fmt.Errorf("buildpacks runtime differs from the frozen configuration")
	}
	if spec.Configuration.BuilderImage != configured.BuilderImage || spec.Configuration.RunImage != configured.RunImage || spec.Configuration.Platform != configured.Platform || spec.LifecycleImage != capability.LifecycleImage || spec.PackVersion != capability.PackVersion {
		return spec, "", fmt.Errorf("buildpacks toolchain differs from the frozen configuration")
	}
	image, _ := task.Payload["image"].(string)
	if !buildpacksOutputImage.MatchString(image) || len(extractCommands(task.Payload)) != 0 {
		return spec, "", fmt.Errorf("invalid Buildpacks output image or command override")
	}
	return spec, image, nil
}

func (r *Runner) runBuildpacks(ctx context.Context, task ExecutionTask) (result map[string]any, runErr error) {
	spec, image, err := r.validateBuildpacksTask(ctx, task)
	if err != nil {
		return nil, err
	}
	ids, err := r.buildpacksContainers(ctx)
	if err != nil || len(ids) != 0 {
		return nil, fmt.Errorf("buildpacks daemon contains unowned containers; operator recovery is required")
	}
	if err := r.trimBuildpacksCache(ctx); err != nil {
		return nil, err
	}
	root, err := r.createBuildpacksWorkspace()
	if err != nil {
		return nil, err
	}
	home, source := filepath.Join(root, "credentials"), filepath.Join(root, "source")
	defer func() { runErr = errors.Join(runErr, r.cleanupBuildpacksWorkspace(root)) }()
	if err := prepareBuildpacksEnvironment(ctx, home, spec.Environment); err != nil {
		return nil, err
	}
	if err := r.checkoutBuildpacksSources(ctx, task, source, home); err != nil {
		return nil, err
	}
	path, err := resolveRepositoryBuildPath(source, spec.ContextDir, true)
	if err != nil {
		return nil, fmt.Errorf("buildpacks project directory is unavailable or escapes the source")
	}
	remote, ok := r.callback(ctx, task, "running", map[string]any{"pipelineStage": "build", "heartbeatAt": time.Now().UTC().Format(time.RFC3339)})
	if !ok {
		return nil, fmt.Errorf("buildpacks task state could not be confirmed")
	}
	if remote.Status == "canceling" || shouldStopLocalExecution(remote.Status) {
		return nil, context.Canceled
	}
	// The dedicated pool has one slot. Only containers created after the empty
	// daemon check belong to this build, including orphaned lifecycle containers.
	defer func() {
		if err := r.cleanupBuildpacksContainers(ctx); err != nil {
			r.buildpacksBlocked.Store(true)
			result, runErr = map[string]any{"stopUnconfirmed": true, "error": "Buildpacks container cleanup requires operator recovery"}, err
			return
		}
		if err := r.trimBuildpacksCache(ctx); err != nil {
			r.buildpacksBlocked.Store(true)
			runErr = errors.Join(runErr, fmt.Errorf("buildpacks cache cleanup requires operator recovery: %w", err))
		}
	}()
	var logs string
	if r.cfg.Buildpacks.Runtime == "podman" {
		logs, err = r.runBuildpacksLifecycle(ctx, image, root, path, spec)
	} else {
		args := r.buildpacksArguments(task.ApplicationID, image, root, path, spec)
		logs, err = buildpacksCommand(ctx, "", r.buildpacksEnvironment(home), "pack", args...)
	}
	if err != nil {
		return map[string]any{"logs": []string{logs}, "failureStage": "build"}, err
	}
	result, err = r.buildpacksResult(ctx, image, root, home)
	if err != nil {
		return map[string]any{"failureStage": "publish"}, err
	}
	result["logs"], result["workspacePath"], result["buildpacks"] = []string{logs}, root, spec
	return result, nil
}

func (r *Runner) cleanupBuildpacksWorkspace(root string) error {
	if err := errors.Join(os.RemoveAll(filepath.Join(root, "credentials")), os.RemoveAll(filepath.Join(root, "source"))); err != nil {
		r.buildpacksBlocked.Store(true)
		return fmt.Errorf("buildpacks workspace cleanup requires operator recovery")
	}
	return nil
}

func (r *Runner) createBuildpacksWorkspace() (string, error) {
	root, err := filepath.Abs(r.cfg.WorkspaceRoot)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return "", err
	}
	root, err = os.MkdirTemp(root, "buildpacks-")
	if err != nil {
		return "", err
	}
	ready := false
	defer func() {
		if !ready {
			_ = os.RemoveAll(root)
		}
	}()
	for _, dir := range []string{"credentials", "source", "report", "sbom"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0o700); err != nil {
			return "", err
		}
	}
	if err := os.WriteFile(filepath.Join(root, "credentials", "project.toml"), nil, 0o600); err != nil {
		return "", err
	}
	ready = true
	return root, nil
}

func (r *Runner) buildpacksArguments(applicationID, image, root, path string, spec sohaapi.BuildpacksExecutionSpec) []string {
	home := filepath.Join(root, "credentials")
	cache := fmt.Sprintf("soha-cnb-%x", sha256.Sum256([]byte(applicationID+"\x00"+spec.Configuration.BuilderImage+"\x00"+spec.Configuration.RunImage+"\x00"+spec.LifecycleImage+"\x00"+string(spec.Configuration.Platform))))
	args := []string{"build", image, "--path", path, "--builder", spec.Configuration.BuilderImage, "--run-image", spec.Configuration.RunImage, "--lifecycle-image", spec.LifecycleImage, "--platform", string(spec.Configuration.Platform), "--publish", "--pull-policy", "never", "--descriptor", filepath.Join(home, "project.toml"), "--env-file", filepath.Join(home, "build.env"), "--report-output-dir", filepath.Join(root, "report"), "--sbom-output-dir", filepath.Join(root, "sbom"), "--cache", "type=build;format=volume;name=" + cache + "-build", "--cache", "type=launch;format=volume;name=" + cache + "-launch"}
	if spec.Configuration.ProcessType != "" {
		args = append(args, "--default-process", spec.Configuration.ProcessType)
	}
	for _, registry := range r.cfg.Buildpacks.InsecureRegistries {
		args = append(args, "--insecure-registry", registry)
	}
	return args
}
