package delivery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"time"

	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	repoworkflow "github.com/opensoha/soha/internal/repository/workflow"
	"gorm.io/gorm"
)

func (r *Repository) createDeliveryBundle(ctx context.Context, item domaindelivery.ReleaseBundle) (domaindelivery.ReleaseBundle, error) {
	node, _ := domainworkflow.NodeExecutionFrom(ctx)
	item.ID, item.Metadata = node.ResourceID("bundle"), node.Metadata(item.Metadata)
	var stored domaindelivery.ReleaseBundle
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := repoworkflow.LockDeliveryNode(ctx, tx, item.ApplicationID, item.ApplicationEnvironmentID); err != nil {
			return err
		}
		repo := New(tx)
		var err error
		stored, err = repo.GetReleaseBundle(ctx, item.ID)
		if err == nil {
			return nil
		}
		if !errors.Is(err, apperrors.ErrNotFound) {
			return err
		}
		stored, err = repo.createReleaseBundle(ctx, item)
		return err
	})
	return stored, err
}

func (r *Repository) createDeliveryTask(ctx context.Context, item domaindelivery.ExecutionTask) (domaindelivery.ExecutionTask, error) {
	node, _ := domainworkflow.NodeExecutionFrom(ctx)
	item.ID, item.Payload, item.MaxRetries = node.ResourceID("task"), node.Metadata(item.Payload), 0
	var stored domaindelivery.ExecutionTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := repoworkflow.LockDeliveryNode(ctx, tx, item.ApplicationID, item.ApplicationEnvironmentID); err != nil {
			return err
		}
		repo := New(tx)
		var err error
		stored, err = repo.GetExecutionTask(ctx, item.ID)
		if err == nil {
			return nil
		}
		if !errors.Is(err, apperrors.ErrNotFound) {
			return err
		}
		if err := lockHelmDelivery(ctx, tx, item); err != nil {
			return err
		}
		stored, err = repo.createExecutionTask(ctx, item)
		return err
	})
	return stored, err
}

func (r *Repository) rejectUnscopedDeliveryTask(ctx context.Context, item domaindelivery.ExecutionTask) error {
	if item.Payload["workflowScope"] == domainworkflow.ScopeDeliveryBatch {
		return fmt.Errorf("%w: delivery task requires a server-side node execution", apperrors.ErrConflict)
	}
	runID, _ := item.Payload["workflowRunId"].(string)
	if runID == "" {
		return nil
	}
	var scope string
	err := r.db.WithContext(ctx).Raw(`SELECT scope FROM workflow_runs WHERE id = ?`, runID).Row().Scan(&scope)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if scope == domainworkflow.ScopeDeliveryBatch {
		return fmt.Errorf("%w: batch nodes cannot use the application task entry point", apperrors.ErrConflict)
	}
	return nil
}

func (r *Repository) updateDeliveryTask(ctx context.Context, item domaindelivery.ExecutionTask) (domaindelivery.ExecutionTask, error) {
	var stored domaindelivery.ExecutionTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var id string
		if err := tx.Raw(`SELECT id FROM execution_tasks WHERE id = ? FOR UPDATE`, item.ID).Row().Scan(&id); err != nil {
			return err
		}
		repo := New(tx)
		current, err := repo.GetExecutionTask(ctx, id)
		if err != nil {
			return err
		}
		if current.CallbackToken != item.CallbackToken || current.AttemptCount > item.AttemptCount {
			return fmt.Errorf("%w: stale delivery task attempt", apperrors.ErrConflict)
		}
		switch current.Status {
		case "completed", "failed", "canceled", "callback_timeout":
			stored = current
			return nil
		}
		if current.ProviderKind == "external_pipeline.gitlab" {
			if err := mergeExternalPipelineTask(current, &item); err != nil {
				return err
			}
		}
		if current.Status == "canceling" && (item.Status == "queued" || item.Status == "dispatching" || item.Status == "running") {
			stored = current
			return nil
		}
		// Preserve immutable task provenance even if a stale adapter retained a
		// payload copy from before the node execution was attached.
		item.Payload = maps.Clone(item.Payload)
		for _, key := range []string{"workflowScope", "workflowRunId", "workflowNodeId", "workflowAttempt", "workflowFencingToken"} {
			item.Payload[key] = current.Payload[key]
		}
		item.Result = maps.Clone(item.Result)
		if item.Result == nil {
			item.Result = map[string]any{}
		}
		for _, key := range []string{"k8sJobClusterId", "k8sJobNamespace", "k8sJobName", "k8sJobImage", "k8sJobGitImage", "k8sJobTimeoutSeconds", "cancelRequestedAt", "cancelReason"} {
			if value, exists := current.Result[key]; exists {
				item.Result[key] = value
			}
		}
		stored, err = repo.updateExecutionTask(ctx, item, true)
		return err
	})
	return stored, err
}

func (r *Repository) RequestDeliveryTaskStop(ctx context.Context, taskID, reason string) (domaindelivery.ExecutionTask, error) {
	var stored domaindelivery.ExecutionTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var id string
		if err := tx.Raw(`SELECT id FROM execution_tasks WHERE id = ? FOR UPDATE`, taskID).Row().Scan(&id); err != nil {
			return err
		}
		repo := New(tx)
		current, err := repo.GetExecutionTask(ctx, id)
		if err != nil {
			return err
		}
		if !domaindelivery.RequiresStopConfirmation(current) {
			return fmt.Errorf("%w: delivery task is required", apperrors.ErrConflict)
		}
		stored = current
		switch current.Status {
		case "completed", "failed", "canceled", "callback_timeout", "canceling":
			return nil
		}
		now := time.Now().UTC()
		if current.Status == "queued" {
			// Claim locks this row before changing queued -> dispatching.
			current.Status, current.FinishedAt = "canceled", &now
		} else {
			current.Status = "canceling"
		}
		current.UpdatedAt = now
		current.Result = maps.Clone(current.Result)
		if current.Result == nil {
			current.Result = map[string]any{}
		}
		current.Result["cancelRequestedAt"], current.Result["cancelReason"] = now.Format(time.RFC3339), reason
		stored, err = repo.updateExecutionTask(ctx, current, true)
		return err
	})
	return stored, err
}

// PrepareDeliveryJob durably records the external identity before any API call.
// Task -> Run is the same lock order as Agent claim. Repeated dispatch preserves
// the first request's cluster, namespace and runtime defaults.
func (r *Repository) PrepareDeliveryJob(ctx context.Context, taskID string, reference map[string]any) (domaindelivery.ExecutionTask, error) {
	var stored domaindelivery.ExecutionTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var id string
		if err := tx.Raw(`SELECT id FROM execution_tasks WHERE id = ? FOR UPDATE`, taskID).Row().Scan(&id); err != nil {
			return err
		}
		repo := New(tx)
		current, err := repo.GetExecutionTask(ctx, id)
		if err != nil {
			return err
		}
		if current.Payload["workflowScope"] != domainworkflow.ScopeDeliveryBatch || current.ProviderKind != "k8s_job_runner" {
			return fmt.Errorf("%w: a delivery Kubernetes Job task is required", apperrors.ErrConflict)
		}
		stored = current
		if current.Status != "queued" && current.Status != "dispatching" {
			return nil
		}
		if err := repoworkflow.LockDeliveryDispatch(ctx, tx, current.Payload); err != nil {
			return err
		}
		if current.Status == "dispatching" {
			return nil
		}
		now := time.Now().UTC()
		current.Status, current.AttemptCount = "dispatching", 1
		current.StartedAt, current.LastHeartbeatAt, current.UpdatedAt = &now, &now, now
		current.Result = maps.Clone(current.Result)
		if current.Result == nil {
			current.Result = map[string]any{}
		}
		maps.Copy(current.Result, reference)
		stored, err = repo.updateExecutionTask(ctx, current, true)
		return err
	})
	return stored, err
}
