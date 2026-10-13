package copilot

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	guest "github.com/opensoha/soha-contracts/agent/guest"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

func (r *Repository) LastPersonalRuntimeID(ctx context.Context, item domain.PersonalEnvironmentRecord) (string, error) {
	var id string
	err := dbtx.DB(ctx, r.db).Raw(`SELECT id FROM execution_tasks WHERE task_kind='vm_ssh_command' AND payload->>'actorId'=? AND payload->>'vmId'=? AND payload->>'personalRuntimeSource'='user' ORDER BY created_at DESC,id DESC LIMIT 1`, item.CreatedBy, item.ComputeRef).Row().Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}
func (r *Repository) GetPersonalBrowserControl(ctx context.Context, item domain.PersonalEnvironmentRecord) (*domain.PersonalRuntimeBrowserControl, error) {
	control := &domain.PersonalRuntimeBrowserControl{}
	err := dbtx.DB(ctx, r.db).Raw(`SELECT control_id,generation,owner_mode,task_id,instance_id,expires_at FROM ai_personal_browser_controls WHERE assistant_id=? AND owner_id=?`, item.AssistantID, item.CreatedBy).Row().Scan(&control.ControlID, &control.Generation, &control.Owner, &control.TaskID, &control.InstanceID, &control.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return control, err
}

func (r *Repository) ReservePersonalRuntime(ctx context.Context, env domain.PersonalEnvironmentRecord, op guest.RuntimeOperation) (domain.PersonalEnvironmentRecord, error) {
	result := dbtx.DB(ctx, r.db).Exec(`UPDATE ai_personal_environments SET operation_id=?,operation_until=now()+interval '70 seconds',active_task_id=?,state='pending',version=version+1,updated_at=now()
 WHERE assistant_id=? AND created_by=? AND version=? AND backend='vm' AND data_initialized AND state IN ('ready','quota_exceeded') AND operation_id='' AND active_task_id='' AND (lease->>'expiresAt')::TIMESTAMPTZ>?`, op.OperationID, op.Request.TaskID, env.AssistantID, env.CreatedBy, env.Version, time.Now().Add(15*time.Second))
	if err := guestMutationResult(result, apperrors.ErrConflict); err != nil {
		return env, err
	}
	return r.GetPersonalEnvironment(ctx, env.CreatedBy, env.AssistantID)
}

func (r *Repository) CompletePersonalRuntime(ctx context.Context, op guest.RuntimeOperation, result guest.RuntimeResult, confirmed bool) error {
	if confirmed && r.CheckPersonalBrowserOperation(ctx, op) != nil {
		confirmed = false
	}
	state := "unknown"
	var usage any
	var observed any
	if confirmed && result.ValidateFor(op) == nil {
		state = "ready"
		if result.Environment.State == "quota_exceeded" {
			state = "quota_exceeded"
		}
		raw, err := json.Marshal(result.Environment.Snapshot.ResourceUsage)
		if err != nil {
			return err
		}
		usage = string(raw)
		observed = result.Environment.ObservedAt
	}
	return dbtx.Within(ctx, r.db, func(txCtx context.Context) error {
		tx := dbtx.DB(txCtx, r.db)
		update := tx.Exec(`UPDATE ai_personal_environments SET state=?,usage=COALESCE(CAST(? AS jsonb),usage),observed_at=COALESCE(?::TIMESTAMPTZ,observed_at),version=version+1,updated_at=now(),
 operation_id=CASE WHEN ?='unknown' THEN operation_id ELSE '' END,operation_until=CASE WHEN ?='unknown' THEN operation_until ELSE NULL END,active_task_id=CASE WHEN ?='unknown' THEN active_task_id ELSE '' END
 WHERE assistant_id=? AND created_by=? AND backend='vm' AND operation_id=? AND compute_ref=? AND compute_generation=? AND data_ref=? AND policy_revision=?`, state, usage, observed, state, state, state, op.Binding.AssistantID, op.Binding.OwnerID, op.OperationID, op.Binding.ComputeRef, op.Binding.ComputeGeneration, op.Binding.DataRef, op.Binding.PolicyRevision)
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected == 0 {
			return nil
		} // A retained callback cannot replace a later reservation.
		if confirmed && result.Browser != nil {
			b := result.Browser
			return tx.Exec(`UPDATE ai_personal_browser_controls SET instance_id=?,updated_at=now() WHERE assistant_id=? AND owner_id=? AND control_id=? AND generation=?`, b.InstanceID, op.Binding.AssistantID, op.Binding.OwnerID, b.ControlID, b.Generation).Error
		}
		return nil
	})
}

// The caller has read the original owner-scoped SSH terminal receipt. This CAS
// only releases its expired reservation; it cannot replay or mark data ready.
func (r *Repository) RecoverTerminalPersonalRuntime(ctx context.Context, item domain.PersonalEnvironmentRecord) (domain.PersonalEnvironmentRecord, error) {
	result := dbtx.DB(ctx, r.db).Exec(`UPDATE ai_personal_environments SET operation_id='',operation_until=NULL,active_task_id='',version=version+1,updated_at=now() WHERE assistant_id=? AND created_by=? AND version=? AND backend='vm' AND state='unknown' AND operation_id=? AND operation_until<=now() AND compute_ref=? AND compute_generation=? AND data_ref=? AND policy_revision=? AND NOT EXISTS(SELECT 1 FROM virtualization_tasks WHERE id=ai_personal_environments.operation_id)`, item.AssistantID, item.CreatedBy, item.Version, item.OperationID, item.ComputeRef, item.ComputeGeneration, item.DataRef, item.PolicyRevision)
	if err := guestMutationResult(result, apperrors.ErrConflict); err != nil {
		return item, err
	}
	return r.GetPersonalEnvironment(ctx, item.CreatedBy, item.AssistantID)
}
