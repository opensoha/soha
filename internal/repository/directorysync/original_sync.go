package directorysync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	domain "github.com/opensoha/soha/internal/domain/directorysync"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (r *Repository) ClaimSyncRequest(ctx context.Context, run domain.Run) (domain.Run, bool, error) {
	if run.IdempotencyKey == "" || run.RequestCredentialID == "" || run.RequestedBy == "" || run.Status != domain.RunQueued {
		return domain.Run{}, false, apperrors.ErrInvalidArgument
	}
	stats, _ := json.Marshal(run.Stats)
	// The existing unique indexes arbitrate both one active run per connection
	// and one original key. A different active request is a conflict, not a retry.
	result := r.db.WithContext(ctx).Exec(`INSERT INTO directory_sync_runs
		(id,connection_id,trigger,mode,include_people,status,idempotency_key,stats,requested_by,request_credential_id,created_at)
		VALUES (?,?,?,?,?,?,?,?::jsonb,?,?,?) ON CONFLICT DO NOTHING`, run.ID, run.ConnectionID, run.Trigger, run.Mode,
		run.IncludePeople, run.Status, run.IdempotencyKey, string(stats), run.RequestedBy, run.RequestCredentialID, run.CreatedAt)
	if result.Error != nil {
		return domain.Run{}, false, result.Error
	}
	original, err := r.GetSyncRequest(ctx, run.ConnectionID, run.IdempotencyKey)
	if errors.Is(err, apperrors.ErrNotFound) && result.RowsAffected == 0 {
		return domain.Run{}, false, apperrors.ErrConflict
	}
	return original, result.RowsAffected == 1, err
}

func (r *Repository) TouchSyncRequest(ctx context.Context, id string) (bool, error) {
	var requested bool
	err := r.db.WithContext(ctx).Raw(`UPDATE directory_sync_runs SET heartbeat_at=? WHERE id=? AND request_credential_id<>'' AND status='running' RETURNING cancel_requested`, time.Now().UTC(), id).Row().Scan(&requested)
	if errors.Is(err, sql.ErrNoRows) {
		return false, apperrors.ErrConflict
	}
	return requested, err
}

func (r *Repository) RequestSyncCancellation(ctx context.Context, id string) (domain.Run, error) {
	// A running creator retains the active index until it actually stops.
	// A queued cancellation wins the same row CAS as queued -> running.
	result := r.db.WithContext(ctx).Exec(`UPDATE directory_sync_runs SET cancel_requested=true,
		status=CASE WHEN status='queued' THEN 'canceled' ELSE status END,
		finished_at=CASE WHEN status='queued' THEN ? ELSE finished_at END
		WHERE id=? AND request_credential_id<>'' AND status IN ('queued','running')`, time.Now().UTC(), id)
	if result.Error != nil {
		return domain.Run{}, result.Error
	}
	if result.RowsAffected != 1 {
		return domain.Run{}, apperrors.ErrConflict
	}
	return r.GetRun(ctx, id)
}

func (r *Repository) FinishCanceledSyncRequest(ctx context.Context, id, summary string) error {
	now := time.Now().UTC()
	result := r.db.WithContext(ctx).Exec(`UPDATE directory_sync_runs SET creator_stopped_at=?,status='canceled',finished_at=?,error_code='canceled',error_summary=?
		WHERE id=? AND request_credential_id<>'' AND status='running' AND cancel_requested=true AND creator_stopped_at IS NULL`, now, now, summary, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return apperrors.ErrConflict
	}
	return nil
}

func (r *Repository) GetSyncRequest(ctx context.Context, connectionID, key string) (domain.Run, error) {
	if connectionID == "" || key == "" {
		return domain.Run{}, apperrors.ErrInvalidArgument
	}
	var id string
	err := r.db.WithContext(ctx).Raw(`SELECT id FROM directory_sync_runs WHERE connection_id=? AND idempotency_key=?`, connectionID, key).Row().Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Run{}, apperrors.ErrNotFound
	}
	if err != nil {
		return domain.Run{}, err
	}
	return r.GetRun(ctx, id)
}
