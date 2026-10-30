package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/helmrelease"
	resourceruntime "github.com/opensoha/soha-contracts/resource/runtime"
)

type HelmExecutor interface {
	ExecuteHelmDelivery(context.Context, sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskResult, error)
}

func (r *Runner) SetHelmExecutor(executor HelmExecutor, clusterID, token string) {
	r.helmExecutor, r.helmClusterID, r.helmClaimToken = executor, strings.TrimSpace(clusterID), strings.TrimSpace(token)
}

func (r *Runner) claimHelmTask(ctx context.Context) (ExecutionTask, bool) {
	if r.helmExecutor == nil || r.helmClusterID == "" || r.helmClaimToken == "" {
		return ExecutionTask{}, false
	}
	client := sohaapi.NewClient(r.cfg.BaseURL, sohaapi.WithBearerToken(r.helmClaimToken), sohaapi.WithHTTPClient(r.httpClient))
	providers := []string{"helm_agent." + r.helmClusterID, helmrelease.InitializationProviderPrefix + r.helmClusterID}
	if r.manifestExecutor != nil && r.manifestClusterID == r.helmClusterID {
		providers = append(providers, resourceruntime.ManifestInitializationProviderPrefix+r.manifestClusterID)
		providers = append(providers, protectedManifestProviders(r.manifestClusterID)...)
	}
	task, err := claimExecutionWithManifestAdmission(ctx, client, claimRequest{AgentID: r.cfg.AgentID, ProviderKinds: providers, RuntimeEndpoint: r.cfg.RuntimeEndpoint})
	r.rejectUnadmittedManifestClaim(ctx, task, err)
	ok := err == nil && task.ID != ""
	r.metrics.markClaim(metricScopeExecution, ok)
	return task, ok
}

func (r *Runner) executeHelmTask(ctx context.Context, task ExecutionTask) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	r.registerActiveTask(task, cancel)
	defer r.unregisterActiveTask(task.ID)
	result := sohaapi.HelmExecutionTaskResult{Stopped: true}
	var payload sohaapi.HelmExecutionTaskPayload
	encoded, err := json.Marshal(task.Payload["helm"])
	if err == nil {
		err = json.Unmarshal(encoded, &payload)
	}
	if err == nil {
		err = helmrelease.ValidateTaskIdentity(payload)
	}
	expectedProvider := "helm_agent." + r.helmClusterID
	if helmrelease.IsInitialization(payload.Snapshot) {
		expectedProvider = helmrelease.InitializationProviderPrefix + r.helmClusterID
	}
	if err == nil && (r.helmExecutor == nil || payload.Snapshot.ClusterID != r.helmClusterID || task.ProviderKind != expectedProvider || task.TaskKind != "helm_"+string(payload.Action)) {
		err = fmt.Errorf("helm runtime identity mismatch")
	}
	if err == nil {
		current, ok := r.callback(ctx, task, "running", map[string]any{"helm": sohaapi.HelmExecutionTaskResult{}})
		if !ok {
			err = fmt.Errorf("helm task start was not confirmed")
		} else if current.CallbackToken != task.CallbackToken {
			return
		} else if current.Status == "canceling" {
			err = context.Canceled
		} else if current.Status != "running" {
			return
		} else {
			result, err = r.runHelmWithHeartbeat(ctx, task, payload)
		}
	}
	if !result.Stopped {
		return
	}
	status := "completed"
	if err != nil {
		status = "failed"
	}
	if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		status = "canceled"
	} else if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
		status = "callback_timeout"
	}
	result.Diagnostics = nil
	r.finalCallback(context.WithoutCancel(ctx), task, status, map[string]any{"helm": result})
	r.metrics.markOutcome(metricScopeExecution, status)
}

func (r *Runner) runHelmWithHeartbeat(ctx context.Context, task ExecutionTask, payload sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskResult, error) {
	ctx, cancel := context.WithCancel(ctx)
	done, stopped := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(stopped)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				current, ok := r.callback(ctx, task, "running", map[string]any{"helm": sohaapi.HelmExecutionTaskResult{}})
				if !ok || current.CallbackToken != task.CallbackToken || current.Status != "running" {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { close(done); cancel(); <-stopped }()
	for {
		result, err := r.helmExecutor.ExecuteHelmDelivery(ctx, payload)
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if err != nil || payload.Action != sohaapi.Observe || result.Ready {
			return result, err
		}
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}
