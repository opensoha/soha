package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/helmrelease"
)

const helmAgentProviderPrefix = "helm_agent."

type HelmExecutor interface {
	ExecuteHelmDelivery(context.Context, sohaapi.HelmExecutionTaskPayload) (sohaapi.HelmExecutionTaskResult, error)
}

func (r *Runner) SetHelmExecutor(executor HelmExecutor, clusterID, token string, allowedActions []string) {
	if nilExecutionPort(executor) {
		executor = nil
	}
	r.helmExecutor, r.helmClusterID, r.helmClaimToken = executor, strings.TrimSpace(clusterID), strings.TrimSpace(token)
	r.helmAllowedActions = slices.Clone(allowedActions)
}

func (r *Runner) helmExecutionClaim() clusterExecutionClaim {
	if r.helmExecutor == nil || r.helmClusterID == "" || r.helmClaimToken == "" {
		return clusterExecutionClaim{}
	}
	return clusterExecutionClaim{
		clusterID: r.helmClusterID,
		token:     r.helmClaimToken,
		providers: []string{helmAgentProviderPrefix + r.helmClusterID, helmrelease.InitializationProviderPrefix + r.helmClusterID},
	}
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
	expectedProvider := helmAgentProviderPrefix + r.helmClusterID
	if helmrelease.IsInitialization(payload.Snapshot) {
		expectedProvider = helmrelease.InitializationProviderPrefix + r.helmClusterID
	}
	if err == nil && (r.helmExecutor == nil || payload.Snapshot.ClusterID != r.helmClusterID || task.ProviderKind != expectedProvider || task.TaskKind != "helm_"+string(payload.Action)) {
		err = fmt.Errorf("helm runtime identity mismatch")
	}
	if err == nil && payload.Action == sohaapi.Apply && !r.helmMutationAllowed(payload.Snapshot.Operation) {
		err = fmt.Errorf("Helm delivery mutation is not allowlisted")
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

func (r *Runner) helmMutationAllowed(operation sohaapi.HelmDeliverySnapshotOperation) bool {
	var action string
	switch operation {
	case sohaapi.HelmDeliverySnapshotOperationInstall:
		action = "platform.helm_releases.install"
	case sohaapi.HelmDeliverySnapshotOperationUpgrade:
		action = "platform.helm_releases.values_update"
	case sohaapi.HelmDeliverySnapshotOperationRollback:
		action = "platform.helm_releases.rollback"
	default:
		return false
	}
	return slices.ContainsFunc(r.helmAllowedActions, func(configured string) bool {
		return strings.EqualFold(strings.TrimSpace(configured), action)
	})
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
