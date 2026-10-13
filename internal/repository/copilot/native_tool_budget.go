package copilot

import (
	"context"
	"fmt"
	"time"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func nativeBudgetConflict() error {
	return fmt.Errorf("%w: Native tool budget exhausted or unavailable", apperrors.ErrConflict)
}

// Root reservations commit before checkpointing and dispatch. A lost checkpoint
// conservatively keeps the slot; the next takeover reads the same ledger.
func (r *Repository) ReserveNativeToolCall(ctx context.Context, lease domaincopilot.NativeLease, limit int) (domaincopilot.NativeToolUsage, error) {
	var usage domaincopilot.NativeToolUsage
	if limit <= 0 || limit > domaincopilot.NativeWorkToolCallLimit {
		return usage, apperrors.ErrInvalidArgument
	}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := &Repository{db: tx}
		row, err := repo.nativeTaskParentRow(ctx, lease.Run.ID, true)
		if err != nil {
			return err
		}
		deadline := row.QueuedAt.Add(time.Duration(row.TimeoutSeconds) * time.Second)
		if row.CreatedBy != lease.Run.CreatedBy || row.SessionID != lease.Run.SessionID || row.NativeRequestHash != lease.RequestHash || row.NativeVersion != lease.Version || row.NativeFence != lease.Fence || row.ClaimedByAgentID != lease.Run.ClaimedByAgentID || row.NativeLeaseUntil == nil || !row.NativeLeaseUntil.After(time.Now()) || nativeTaskStopReason(row, deadline) != "" {
			return fmt.Errorf("%w: Native reservation lease changed", apperrors.ErrConflict)
		}
		result := tx.Raw(`UPDATE ai_native_tool_budget SET work_calls=work_calls+1,work_limit=LEAST(work_limit,?),updated_at=now()
 WHERE run_id=? AND work_calls<work_limit AND work_calls<?
 RETURNING work_calls,cleanup_calls,work_limit,cleanup_limit`, limit, lease.Run.ID, limit).Scan(&usage)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return nativeBudgetConflict()
		}
		return nil
	})
	return usage, err
}

// The caller holds the existing parent stop lock. Only the independent ledger
// is updated here, on an autocommitted connection: rolling back the parent guard
// after a provider error cannot erase the attempt. No checkpoint CAS is changed.
func (r *Repository) ReserveNativeChildToolCall(ctx context.Context, parent domaincopilot.NativeTaskParent, cancellation bool) error {
	row, err := r.nativeTaskParentRow(ctx, parent.RunID, false)
	if err != nil {
		return err
	}
	if err = validateNativeTaskParent(parent, row); err != nil {
		return err
	}
	if !cancellation && nativeTaskStopReason(row, parent.Deadline) != "" {
		return fmt.Errorf("%w: Native parent is stopped", apperrors.ErrConflict)
	}
	query := `UPDATE ai_native_tool_budget SET work_calls=work_calls+1,updated_at=now() WHERE run_id=? AND work_calls<work_limit`
	if cancellation {
		query = `UPDATE ai_native_tool_budget SET cleanup_calls=cleanup_calls+1,updated_at=now() WHERE run_id=? AND cleanup_calls<cleanup_limit`
	}
	result := r.db.WithContext(ctx).Exec(query, parent.RunID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return nativeBudgetConflict()
	}
	return nil
}
