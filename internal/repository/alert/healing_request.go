package alert

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	domainalert "github.com/opensoha/soha/internal/domain/alert"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func (r *Repository) ClaimHealingRequest(ctx context.Context, input domainalert.HealingRequest, run domainalert.HealingRunInput) (domainalert.HealingRun, error) {
	var stored domainalert.HealingRun
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		claim := tx.Exec(`INSERT INTO healing_request_receipts
 (id,actor_id,credential_hash,request_key,event_id,policy_id,input_digest,run_id,created_at)
 VALUES (?,?,?,?,?,?,?,?,?) ON CONFLICT (actor_id,request_key) DO NOTHING`, input.ID, input.ActorID, input.CredentialHash, input.Key, input.EventID, input.PolicyID, input.InputDigest, input.RunID, input.CreatedAt)
		if claim.Error != nil {
			return claim.Error
		}
		repo := &Repository{db: tx}
		binding, err := repo.GetHealingRequest(ctx, input.ActorID, input.Key)
		if err != nil {
			return err
		}
		if binding.CredentialHash != input.CredentialHash {
			return apperrors.ErrAccessDenied
		}
		if binding.EventID != input.EventID || binding.PolicyID != input.PolicyID || binding.InputDigest != input.InputDigest {
			return apperrors.ErrConflict
		}
		if claim.RowsAffected == 1 {
			stored, err = repo.CreateHealingRun(ctx, run)
		} else {
			// A missing run is a retained tombstone, never permission to recreate.
			stored, err = repo.GetHealingRun(ctx, binding.RunID)
		}
		return err
	})
	return stored, err
}

func (r *Repository) GetHealingRequest(ctx context.Context, actor, key string) (domainalert.HealingRequest, error) {
	var item domainalert.HealingRequest
	err := r.db.WithContext(ctx).Raw(`SELECT id,actor_id,credential_hash,request_key,event_id,policy_id,input_digest,run_id,created_at
 FROM healing_request_receipts WHERE actor_id=? AND request_key=?`, actor, key).Row().Scan(
		&item.ID, &item.ActorID, &item.CredentialHash, &item.Key, &item.EventID, &item.PolicyID, &item.InputDigest, &item.RunID, &item.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, apperrors.ErrNotFound
	}
	return item, err
}

// CAS protects all decisions, including retries and dispatch completion. A
// stale approver/worker cannot clear a newer generation or overwrite rejection.
func (r *Repository) CompareAndSwapHealingRun(ctx context.Context, before, after domainalert.HealingRun) (domainalert.HealingRun, bool, error) {
	if before.ID == "" || after.ID != before.ID || before.ExecutionGeneration < 1 {
		return domainalert.HealingRun{}, false, apperrors.ErrInvalidArgument
	}
	result, err := json.Marshal(after.Result)
	if err != nil {
		return domainalert.HealingRun{}, false, err
	}
	updated := r.db.WithContext(ctx).Exec(`UPDATE healing_runs SET
 status=?,approval_status=?,approval_comment=?,approved_by=?,workflow_run_id=?,result=?,started_at=?,completed_at=?,
 execution_generation=?,submission_status=?,intent_digest=?,updated_at=?
 WHERE id=? AND execution_generation=? AND COALESCE(approval_status,'')=? AND submission_status=?
 AND COALESCE(workflow_run_id,'')=? AND updated_at=?`,
		after.Status, after.ApprovalStatus, nullableString(after.ApprovalComment), nullableString(after.ApprovedBy), nullableString(after.WorkflowRunID), string(result), nullableTime(after.StartedAt), nullableTime(after.CompletedAt),
		after.ExecutionGeneration, after.SubmissionStatus, after.IntentDigest, time.Now().UTC(), before.ID, before.ExecutionGeneration, before.ApprovalStatus, before.SubmissionStatus, before.WorkflowRunID, before.UpdatedAt)
	if updated.Error != nil {
		return domainalert.HealingRun{}, false, updated.Error
	}
	item, err := r.GetHealingRun(ctx, before.ID)
	return item, updated.RowsAffected == 1, err
}
