package workflow

import (
	"context"
	"database/sql"
	"errors"

	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const workflowRequestColumns = `id,actor_id,request_key,input_digest,application_id,binding_id,run_id,status,error_code,created_at,updated_at`

func (r *Repository) ClaimTriggerRequest(ctx context.Context, input domainworkflow.TriggerRequest) (domainworkflow.TriggerRequest, bool, error) {
	result := r.db.WithContext(ctx).Exec(`INSERT INTO workflow_request_receipts
 (id,actor_id,request_key,input_digest,application_id,binding_id,run_id,status,created_at,updated_at)
 VALUES (?,?,?,?,?,?,?,'unknown',?,?) ON CONFLICT (actor_id,request_key) DO NOTHING`, input.ID, input.ActorID, input.Key, input.InputDigest, input.ApplicationID, input.BindingID, input.RunID, input.CreatedAt, input.UpdatedAt)
	if result.Error != nil {
		return domainworkflow.TriggerRequest{}, false, result.Error
	}
	stored, err := r.GetTriggerRequest(ctx, input.ActorID, input.Key)
	if err != nil {
		return stored, false, err
	}
	if stored.InputDigest != input.InputDigest || stored.ApplicationID != input.ApplicationID || stored.BindingID != input.BindingID {
		return stored, false, apperrors.ErrConflict
	}
	return stored, result.RowsAffected == 1, nil
}

func (r *Repository) GetTriggerRequest(ctx context.Context, actor, key string) (domainworkflow.TriggerRequest, error) {
	var item domainworkflow.TriggerRequest
	err := r.db.WithContext(ctx).Raw(`SELECT `+workflowRequestColumns+` FROM workflow_request_receipts WHERE actor_id=? AND request_key=?`, actor, key).Row().Scan(&item.ID, &item.ActorID, &item.Key, &item.InputDigest, &item.ApplicationID, &item.BindingID, &item.RunID, &item.Status, &item.ErrorCode, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, apperrors.ErrNotFound
	}
	return item, err
}

func (r *Repository) FinishTriggerRequest(ctx context.Context, id, status, code string) error {
	if status != "accepted" && status != "rejected" {
		return apperrors.ErrInvalidArgument
	}
	result := r.db.WithContext(ctx).Exec(`UPDATE workflow_request_receipts SET status=?,error_code=?,updated_at=NOW() WHERE id=? AND status='unknown'`, status, code, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	var current, storedCode string
	err := r.db.WithContext(ctx).Raw(`SELECT status,error_code FROM workflow_request_receipts WHERE id=?`, id).Row().Scan(&current, &storedCode)
	if errors.Is(err, sql.ErrNoRows) {
		return apperrors.ErrNotFound
	}
	if err != nil {
		return err
	}
	if current != status || storedCode != code {
		return apperrors.ErrConflict
	}
	return nil
}
