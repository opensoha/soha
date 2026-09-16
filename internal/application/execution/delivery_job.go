package execution

import (
	"context"
	"fmt"
	"maps"
	"strconv"
	"time"

	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type deliveryJobRepository interface {
	PrepareDeliveryJob(context.Context, string, map[string]any) (domaindelivery.ExecutionTask, error)
}

func (s *Service) dispatchDeliveryJob(ctx context.Context, task domaindelivery.ExecutionTask, now time.Time) (domaindelivery.ExecutionTask, error) {
	repo, ok := s.repo.(deliveryJobRepository)
	if !ok {
		return task, fmt.Errorf("%w: durable Job dispatch is not configured", apperrors.ErrConflict)
	}
	options := s.jobRuntimeOptions()
	prepared, err := repo.PrepareDeliveryJob(ctx, task.ID, map[string]any{
		"k8sJobClusterId": s.resolveExecutionJobClusterID(task),
		"k8sJobNamespace": firstNonEmpty(valueAsString(task.Payload["jobNamespace"]), options.Namespace),
		"k8sJobName":      DeliveryJobName(task.ID),
		"k8sJobImage":     options.Image, "k8sJobGitImage": options.GitImage,
		"k8sJobTimeoutSeconds": effectiveTimeoutSeconds(task),
		"k8sJobStatus":         "dispatching",
	})
	if err != nil {
		return task, err
	}
	if prepared.Status != "dispatching" {
		return prepared, nil
	}
	clusterID, namespace, name := executionJobRef(prepared)
	if clusterID == "" || namespace == "" || name == "" {
		return prepared, fmt.Errorf("%w: persisted Job identity is incomplete", apperrors.ErrConflict)
	}
	_, err = s.clusters.CreateExecutionJob(ctx, clusterID, deliveryJobRequest(prepared))
	if err != nil {
		// A timeout may follow a successful API write. Keep the stored identity
		// for inspection; never report failure or issue a differently named Job.
		return prepared, err
	}
	prepared.Status, prepared.LastHeartbeatAt, prepared.UpdatedAt = "running", &now, now
	prepared.Result = maps.Clone(prepared.Result)
	prepared.Result["k8sJobStatus"] = "running"
	return s.updateExecutionTask(ctx, prepared)
}

func deliveryJobRequest(task domaindelivery.ExecutionTask) ExecutionJobRequest {
	_, namespace, name := executionJobRef(task)
	// Old Jobs keep their original request digest when recovered after upgrade.
	timeout, _ := strconv.Atoi(valueAsString(task.Result["k8sJobTimeoutSeconds"]))
	return ExecutionJobRequest{
		TaskID: task.ID, TaskKind: task.TaskKind, Name: name, Retain: true, Namespace: namespace,
		Commands: valueAsStringSlice(task.Payload["commands"]),
		Runtime:  valueAsMap(task.Payload["runtime"]), Workspace: valueAsMap(task.Payload["workspace"]),
		DefaultImage: valueAsString(task.Result["k8sJobImage"]), DefaultGitImage: valueAsString(task.Result["k8sJobGitImage"]),
		TimeoutSeconds: timeout,
	}
}
