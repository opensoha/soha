package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	resourceruntime "github.com/opensoha/soha-contracts/resource/runtime"
)

func (r *Runner) executeManifestTask(ctx context.Context, task ExecutionTask) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	r.registerActiveTask(task, cancel)
	defer r.unregisterActiveTask(task.ID)
	if r.manifestExecutor == nil {
		r.finalCallback(ctx, task, "failed", map[string]any{"error": "manifest execution is unavailable"})
		r.metrics.markOutcome(metricScopeExecution, "failed")
		return
	}
	payload, err := decodeManifestPayload(task.Payload)
	if err != nil {
		r.finalCallback(ctx, task, "failed", map[string]any{"error": "invalid manifest task payload"})
		r.metrics.markOutcome(metricScopeExecution, "failed")
		return
	}
	if err := validateManifestProtocol(task.ProviderKind, r.manifestClusterID, payload); err != nil {
		r.finalCallback(ctx, task, "failed", map[string]any{"error": "manifest protocol and root identity do not match"})
		r.metrics.markOutcome(metricScopeExecution, "failed")
		return
	}
	if payload.ClusterID != "" && payload.ClusterID != r.manifestClusterID {
		r.finalCallback(ctx, task, "failed", map[string]any{"error": "manifest task targets a different cluster"})
		r.metrics.markOutcome(metricScopeExecution, "failed")
		return
	}

	remoteTask, ok := r.callback(ctx, task, "running", map[string]any{
		"action":      payload.Action,
		"generation":  payload.Generation,
		"heartbeatAt": time.Now().UTC().Format(time.RFC3339),
	})
	if !ok {
		r.finalCallback(ctx, task, "failed", map[string]any{"error": "manifest task state could not be confirmed"})
		r.metrics.markOutcome(metricScopeExecution, "failed")
		return
	}
	if shouldStopLocalExecution(remoteTask.Status) {
		r.metrics.markOutcome(metricScopeExecution, remoteTask.Status)
		return
	}
	r.updateActiveTask(task.ID, func(item *ActiveTask) { item.Status = "running" })
	result, executeErr := r.runManifestWithHeartbeat(ctx, task, payload)
	resultPayload := manifestResultMap(result)
	status := "completed"
	if executeErr != nil {
		status = "failed"
		resultPayload["error"] = publicManifestError(executeErr)
	}
	if errors.Is(executeErr, resourceruntime.ErrArgoStopUnconfirmed) {
		status = "failed"
		resultPayload["error"] = "Argo CD operation stop is not confirmed"
	} else if errors.Is(executeErr, resourceruntime.ErrRolloutStopUnconfirmed) {
		status = "failed"
		resultPayload["error"] = "Rollout traffic stop is not confirmed"
	} else if errors.Is(executeErr, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		status = "canceled"
		resultPayload["error"] = "manifest execution canceled; some resources may already have been applied"
	} else if errors.Is(executeErr, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		status = "callback_timeout"
		resultPayload["error"] = "manifest execution timed out"
	}
	r.finalCallback(context.WithoutCancel(ctx), task, status, resultPayload)
	r.metrics.markOutcome(metricScopeExecution, status)
}

func validateManifestProtocol(provider, clusterID string, payload sohaapi.ManifestExecutionTaskPayload) error {
	if err := resourceruntime.ValidateManifestInitialization(payload); err != nil {
		return err
	}
	if strings.HasPrefix(provider, resourceruntime.ManifestInitializationProviderPrefix) {
		if provider != resourceruntime.ManifestInitializationProviderPrefix+clusterID || payload.ClusterID != clusterID || payload.InitializationPlanID == "" {
			return fmt.Errorf("initialization protocol identity mismatch")
		}
	} else if payload.InitializationPlanID != "" {
		return fmt.Errorf("initialization cannot use legacy protocol")
	}

	if strings.HasPrefix(provider, resourceruntime.RootCRAgentProviderPrefix) {
		if provider != resourceruntime.RootCRAgentProviderPrefix+clusterID || payload.ClusterID != clusterID || payload.RootResource == nil {
			return fmt.Errorf("root CR requires the protected cluster protocol and identity")
		}
	} else if payload.RootResource != nil {
		return fmt.Errorf("root CR cannot use the legacy manifest protocol")
	}
	return nil
}

func (r *Runner) runManifestWithHeartbeat(ctx context.Context, task ExecutionTask, payload sohaapi.ManifestExecutionTaskPayload) (sohaapi.ManifestExecutionTaskResult, error) {
	runCtx, cancel := context.WithCancel(ctx)
	done, stopReason := make(chan struct{}), make(chan string, 1)
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		r.streamHeartbeats(runCtx, cancel, done, stopReason, task, r.cfg.AgentID, task.TaskKind, 0, 0, "")
	}()
	go func() { defer workers.Done(); r.watchRunnerStatus(runCtx, cancel, done, stopReason, task) }()
	defer func() { close(done); cancel(); workers.Wait() }()
	result, err := r.manifestExecutor.ExecuteManifestTask(runCtx, payload)
	if runCtx.Err() != nil && !errors.Is(err, resourceruntime.ErrArgoStopUnconfirmed) && !errors.Is(err, resourceruntime.ErrRolloutStopUnconfirmed) {
		return result, runCtx.Err()
	}
	return result, err
}

func decodeManifestPayload(value map[string]any) (sohaapi.ManifestExecutionTaskPayload, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return sohaapi.ManifestExecutionTaskPayload{}, err
	}
	var payload sohaapi.ManifestExecutionTaskPayload
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return sohaapi.ManifestExecutionTaskPayload{}, err
	}
	if strings.TrimSpace(payload.PackageID) == "" || payload.Generation < 1 || strings.TrimSpace(payload.IdempotencyKey) == "" {
		return sohaapi.ManifestExecutionTaskPayload{}, fmt.Errorf("required manifest task identity is missing")
	}
	return payload, nil
}

func manifestResultMap(value sohaapi.ManifestExecutionTaskResult) map[string]any {
	encoded, _ := json.Marshal(value)
	result := map[string]any{}
	_ = json.Unmarshal(encoded, &result)
	return result
}

func publicManifestError(err error) string {
	return "manifest execution failed"
}
