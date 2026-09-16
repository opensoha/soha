package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"time"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
	repoworkflow "github.com/opensoha/soha/internal/repository/workflow"
	"gorm.io/gorm"
)

// The queued -> dispatching transition grants exactly one POST attempt. A crash
// after this transaction is reconciled by task variables, never by resubmitting.
func (r *Repository) BeginExternalPipelineDispatch(ctx context.Context, taskID string) (domaindelivery.ExecutionTask, bool, error) {
	var stored domaindelivery.ExecutionTask
	won := false
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
		if current.TaskKind != "build" || current.ProviderKind != domainbuild.ExternalPipelineProvider {
			return apperrors.ErrInvalidArgument
		}
		stored = current
		if current.Status != "queued" {
			return nil
		}
		if err := repoworkflow.LockDeliveryDispatch(ctx, tx, current.Payload); err != nil {
			return err
		}
		now := time.Now().UTC()
		current.Status, current.AttemptCount, current.MaxRetries = "dispatching", 1, 0
		current.StartedAt, current.LastHeartbeatAt, current.UpdatedAt = &now, &now, now
		current.Result = maps.Clone(current.Result)
		if current.Result == nil {
			current.Result = map[string]any{}
		}
		current.Result["externalPipeline"] = sohaapi.ExternalPipelineRun{Status: "dispatch_unknown"}
		stored, err = repo.updateExecutionTask(ctx, current, true)
		won = err == nil
		return err
	})
	return stored, won && err == nil, err
}

func mergeExternalPipelineTask(current domaindelivery.ExecutionTask, item *domaindelivery.ExecutionTask) error {
	var previous, incoming sohaapi.ExternalPipelineRun
	oldData, _ := json.Marshal(current.Result["externalPipeline"])
	newData, _ := json.Marshal(item.Result["externalPipeline"])
	if json.Unmarshal(oldData, &previous) != nil || json.Unmarshal(newData, &incoming) != nil {
		return fmt.Errorf("%w: invalid pipeline state", apperrors.ErrConflict)
	}
	if previous.RunID != "" && incoming.RunID != "" && previous.RunID != incoming.RunID {
		return fmt.Errorf("%w: pipeline identity cannot change", apperrors.ErrConflict)
	}
	if previous.RunID != "" && incoming.RunID == "" {
		// A delayed unknown-dispatch poll cannot erase a returned remote identity.
		*item = current
		return nil
	}
	if item.Status == "completed" || item.Status == "failed" || item.Status == "canceled" {
		if !incoming.StopConfirmed {
			return fmt.Errorf("%w: pipeline stop is not confirmed", apperrors.ErrConflict)
		}
	}
	item.Payload, item.ProviderKind = maps.Clone(current.Payload), current.ProviderKind
	item.Result = maps.Clone(item.Result)
	if failure, exists := current.Result["pipelineFailure"]; exists {
		item.Result["pipelineFailure"] = failure
	}
	if current.Status == "canceling" {
		item.Status = "canceling"
		if incoming.StopConfirmed {
			item.Status = "canceled"
			if current.Result["cancelReason"] == "execution_timeout" || item.Result["pipelineFailure"] != nil {
				item.Status = "failed"
			}
		}
	}
	return nil
}
