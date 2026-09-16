package execution

import (
	"context"
	"fmt"
	"time"

	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type deliveryTaskStopRepository interface {
	RequestDeliveryTaskStop(context.Context, string, string) (domaindelivery.ExecutionTask, error)
}

const executionTimeoutReason = "execution_timeout"

func executionTimeoutMessage(task domaindelivery.ExecutionTask) string {
	return fmt.Sprintf("execution task exceeded its %d second execution limit", effectiveTimeoutSeconds(task))
}

func deliveryTaskExecutionExpired(task domaindelivery.ExecutionTask, now time.Time) bool {
	if task.Payload["workflowScope"] != domainworkflow.ScopeDeliveryBatch && task.ProviderKind != domainbuild.ExternalPipelineProvider {
		return false
	}
	if task.Status != "dispatching" && task.Status != "running" {
		return false
	}
	reference := task.CreatedAt
	if task.StartedAt != nil && !task.StartedAt.IsZero() {
		reference = *task.StartedAt
	}
	return !reference.IsZero() && !now.Before(reference.Add(time.Duration(effectiveTimeoutSeconds(task))*time.Second))
}

func (s *Service) cancelDeliveryTask(ctx context.Context, taskID, reason string) (domaindelivery.ExecutionTask, error) {
	repo, ok := s.repo.(deliveryTaskStopRepository)
	if !ok {
		return domaindelivery.ExecutionTask{}, fmt.Errorf("%w: durable task cancellation is not configured", apperrors.ErrConflict)
	}
	task, err := repo.RequestDeliveryTaskStop(ctx, taskID, reason)
	if err != nil {
		return task, err
	}
	if isStrictTerminalTaskStatus(task.Status) {
		return task, s.finishDeliveryTaskStop(ctx, task)
	}
	if task.ProviderKind == domainbuild.ExternalPipelineProvider {
		return s.reconcileExternalPipeline(ctx, task, time.Now().UTC())
	}
	if task.ProviderKind != "k8s_job_runner" {
		return task, s.stopRemoteRuntimeTask(ctx, task.ID, task.Result, reason)
	}
	runtime, ok := s.clusters.(DeliveryJobStopRuntime)
	if !ok {
		return task, fmt.Errorf("%w: Kubernetes stop confirmation is not configured", apperrors.ErrConflict)
	}
	clusterID, _, _ := executionJobRef(task)
	inspection, err := runtime.StopDeliveryJob(ctx, clusterID, deliveryJobRequest(task))
	if err != nil {
		return task, err
	}
	if inspection.State == ExecutionJobSucceeded || inspection.State == ExecutionJobFailed {
		_, err := s.reconcileK8sJobExecution(ctx, task, time.Now().UTC())
		if err != nil {
			return task, err
		}
		return s.repo.GetExecutionTask(ctx, task.ID)
	}
	if inspection.State != ExecutionJobCanceled {
		return task, nil
	}
	now := time.Now().UTC()
	task.Status, task.FinishedAt, task.UpdatedAt = "canceled", &now, now
	if task.Result["cancelReason"] == executionTimeoutReason {
		task.Status = "failed"
		task.Result = mergeMaps(task.Result, map[string]any{"error": executionTimeoutMessage(task), "timedOutAt": now.Format(time.RFC3339)})
	}
	task, err = s.updateExecutionTask(ctx, task)
	if err != nil {
		return task, err
	}
	return task, s.finishDeliveryTaskStop(ctx, task)
}

func (s *Service) finishDeliveryTaskStop(ctx context.Context, task domaindelivery.ExecutionTask) error {
	if !isStrictTerminalTaskStatus(task.Status) {
		return nil
	}
	if s.secretLeases != nil {
		_ = s.secretLeases.RevokeSubjectLeases(ctx, "execution_task", task.ID)
	}
	if task.ReleaseBundleID != "" {
		if err := s.updateTaskReleaseBundle(ctx, task, time.Now().UTC()); err != nil {
			return err
		}
	}
	if err := s.persistArtifacts(ctx, task); err != nil {
		return err
	}
	switch task.TaskKind {
	case "build":
		_ = s.syncBuildRecord(ctx, task)
	case "release":
		_ = s.syncReleaseRecord(ctx, task)
	}
	return s.notifyExecutionTaskSinks(ctx, task)
}
