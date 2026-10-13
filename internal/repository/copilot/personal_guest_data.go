package copilot

import (
	"context"
	"encoding/json"
	environment "github.com/opensoha/soha-contracts/agent/environment"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"time"
)

func (r *Repository) ReservePersonalGuestData(ctx context.Context, env domain.PersonalEnvironmentRecord, operationID string) error {
	return guestMutationResult(dbtx.DB(ctx, r.db).Exec(`UPDATE ai_personal_environments SET operation_id=?,operation_until=now()+interval '15 minutes',version=version+1,updated_at=now() WHERE assistant_id=? AND created_by=? AND version=? AND operation_id=? AND state='pending' AND active_task_id=''`, operationID, env.AssistantID, env.CreatedBy, env.Version, env.OperationID), apperrors.ErrConflict)
}

func (r *Repository) GuestDataCheckpoint(ctx context.Context, plan guest.DataPlan, phase string, result guest.DataResult) error {
	tx := dbtx.DB(ctx, r.db)
	args := []any{plan.Binding.AssistantID, plan.Binding.OwnerID, plan.OperationID, plan.Binding.DataRef, plan.Binding.ComputeGeneration, plan.Binding.PolicyRevision}
	where := ` WHERE assistant_id=? AND created_by=? AND operation_id=? AND data_ref=? AND compute_generation=? AND policy_revision=? AND state='pending'`
	switch phase {
	case "dispatched":
		return guestMutationResult(tx.Exec(`UPDATE ai_personal_environments SET data_creation_attempted=TRUE,updated_at=now()`+where, args...), apperrors.ErrConflict)
	case "complete":
		if result.DataRef != plan.Binding.DataRef || result.FilesystemID != result.DataRef || result.DiskQuota != plan.DiskQuotaBytes || result.DiskUsed < 0 || result.DiskUsed > result.DiskQuota {
			return apperrors.ErrConflict
		}
		usage, err := json.Marshal(environment.Usage{DiskBytes: result.DiskUsed})
		if err != nil {
			return err
		}
		values := append([]any{string(usage), time.Now().UTC()}, args...)
		return guestMutationResult(tx.Exec(`UPDATE ai_personal_environments SET data_initialized=TRUE,usage=CAST(? AS jsonb),observed_at=?,operation_id='',operation_until=NULL,version=version+1,updated_at=now()`+where, values...), apperrors.ErrConflict)
	default:
		return apperrors.ErrInvalidArgument
	}
}

func (r *Repository) PersonalGuestDataLease(ctx context.Context, binding guest.Binding) (guest.DataPlan, error) {
	env, err := r.GetPersonalEnvironment(ctx, binding.OwnerID, binding.AssistantID)
	if err != nil {
		return guest.DataPlan{}, err
	}
	if !env.DataInitialized || env.DataRef != binding.DataRef || env.ComputeGeneration != binding.ComputeGeneration || env.PolicyRevision != binding.PolicyRevision || env.OperationID != "" {
		return guest.DataPlan{}, apperrors.ErrAccessDenied
	}
	return guest.DataPlan{SchemaVersion: guest.DataPlanVersion, OperationID: binding.ID, Binding: binding, DiskQuotaBytes: env.Lease.Quota.DiskBytes}, nil
}
