package copilot

import (
	"context"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

func (r *Repository) CurrentPersonalGuest(ctx context.Context, owner, assistant string) (domain.PersonalGuestRecord, error) {
	return scanPersonalGuest(dbtx.DB(ctx, r.db).Raw(`SELECT `+personalGuestColumns+` FROM ai_personal_guests WHERE assistant_id=? AND binding->>'ownerId'=? ORDER BY binding_version DESC LIMIT 1`, assistant, owner).Row())
}

func (r *Repository) ReservePersonalGuestTools(ctx context.Context, env domain.PersonalEnvironmentRecord, operation string) error {
	return guestMutationResult(dbtx.DB(ctx, r.db).Exec(`UPDATE ai_personal_environments SET operation_id=?,operation_until=now()+interval '15 minutes',version=version+1,updated_at=now()
 WHERE assistant_id=? AND created_by=? AND version=? AND backend='vm' AND provider='pve' AND data_initialized AND operation_id='' AND active_task_id='' AND state IN ('pending','ready','unknown','quota_exceeded')`, operation, env.AssistantID, env.CreatedBy, env.Version), apperrors.ErrConflict)
}

func (r *Repository) GuestToolsCheckpoint(ctx context.Context, plan guest.ToolsPlan) error {
	// Installation is not proof that isolated task or browser execution works.
	return guestMutationResult(dbtx.DB(ctx, r.db).Exec(`UPDATE ai_personal_environments SET state='pending',operation_id='',operation_until=NULL,version=version+1,updated_at=now()
 WHERE assistant_id=? AND created_by=? AND operation_id=? AND backend='vm' AND data_initialized AND data_ref=? AND compute_generation=? AND policy_revision=?`, plan.Binding.AssistantID, plan.Binding.OwnerID, plan.OperationID, plan.Binding.DataRef, plan.Binding.ComputeGeneration, plan.Binding.PolicyRevision), apperrors.ErrConflict)
}
