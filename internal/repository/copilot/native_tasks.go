package copilot

import (
	"context"
	"fmt"
	"time"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

type nativeTaskParentRow struct {
	CreatedBy, SessionID, NativeRequestHash, Status, ClaimedByAgentID string
	NativePhase, StopReason                                           string
	NativeVersion, NativeFence                                        int64
	QueuedAt                                                          time.Time
	TimeoutSeconds                                                    int
	NativeCancelAt, NativeLeaseUntil                                  *time.Time
}

func (r *Repository) nativeTaskParentRow(ctx context.Context, id string, locked bool) (nativeTaskParentRow, error) {
	var row nativeTaskParentRow
	query := `SELECT created_by,session_id,native_request_hash,status,native_phase,output->>'stopReason' AS stop_reason,queued_at,timeout_seconds,native_cancel_at,native_version,native_fence,native_lease_until,claimed_by_agent_id FROM ai_agent_runs WHERE id=? AND (provider_kind='native' OR (provider_kind='dsh' AND input->>'_sohaExecutionOwner'='dsh-core'))`
	if locked {
		query += ` FOR UPDATE`
	}
	result := dbtx.DB(ctx, r.db).Raw(query, id).Scan(&row)
	if result.Error != nil {
		return row, result.Error
	}
	if result.RowsAffected != 1 || row.NativeRequestHash == "" || row.TimeoutSeconds <= 0 {
		return row, fmt.Errorf("%w: Native parent is unavailable", apperrors.ErrConflict)
	}
	return row, nil
}

func nativeTaskStopReason(row nativeTaskParentRow, deadline time.Time) string {
	if row.NativeCancelAt != nil || row.Status == "canceled" {
		return "user"
	}
	now := time.Now()
	if row.Status != "running" || row.NativePhase == "cancelling" || row.StopReason == "unknown_outcome" ||
		(row.NativePhase == "paused" && row.StopReason != "waiting_input") ||
		row.NativeLeaseUntil == nil || !row.NativeLeaseUntil.After(now) || !deadline.After(now) {
		return "failure"
	}
	return ""
}

func validateNativeTaskParent(parent domaincopilot.NativeTaskParent, row nativeTaskParentRow) error {
	if parent.RunID == "" || parent.OwnerID != row.CreatedBy || parent.SessionID != row.SessionID || parent.RequestHash != row.NativeRequestHash || !parent.Deadline.Equal(row.QueuedAt.Add(time.Duration(row.TimeoutSeconds)*time.Second)) {
		return fmt.Errorf("%w: Native parent identity changed", apperrors.ErrConflict)
	}
	return nil
}

// Child registration joins the same database transaction as the parent fence.
// No child can survive a failed registration or a concurrent cancellation.
func (r *Repository) WithNativeTaskCreation(ctx context.Context, expected domaincopilot.NativeLease, register func(context.Context, domaincopilot.NativeTaskParent) error) error {
	return dbtx.Within(ctx, r.db, func(txCtx context.Context) error {
		row, err := r.nativeTaskParentRow(txCtx, expected.Run.ID, true)
		if err != nil {
			return err
		}
		parent := domaincopilot.NativeTaskParent{RunID: expected.Run.ID, OwnerID: row.CreatedBy, SessionID: row.SessionID, RequestHash: row.NativeRequestHash, Deadline: row.QueuedAt.Add(time.Duration(row.TimeoutSeconds) * time.Second)}
		if row.CreatedBy != expected.Run.CreatedBy || row.SessionID != expected.Run.SessionID || row.NativeVersion != expected.Version || row.NativeFence != expected.Fence || row.ClaimedByAgentID != expected.Run.ClaimedByAgentID || nativeTaskStopReason(row, parent.Deadline) != "" {
			return fmt.Errorf("%w: Native child registration lease changed", apperrors.ErrConflict)
		}
		return register(txCtx, parent)
	})
}

func (r *Repository) NativeTaskStopReason(ctx context.Context, parent domaincopilot.NativeTaskParent) (string, error) {
	row, err := r.nativeTaskParentRow(ctx, parent.RunID, false)
	if err != nil {
		return "", err
	}
	if err = validateNativeTaskParent(parent, row); err != nil {
		return "", err
	}
	return nativeTaskStopReason(row, parent.Deadline), nil
}

// Reuse the existing Workflow serialization: stop and approval cannot pass this
// fence concurrently. Cancellation controls may still reconcile an ended run.
// The callback keeps its own transaction and domain receipt; only creation joins.
func (r *Repository) WithNativeTaskParent(ctx context.Context, parent domaincopilot.NativeTaskParent, cancellation bool, execute func(context.Context) error) error {
	deadline := parent.Deadline
	if cancellation {
		// Cleanup may outlive the parent budget, but each reconciliation stays bounded.
		deadline = time.Now().Add(30 * time.Second)
	}
	ctx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	return dbtx.Within(ctx, r.db, func(txCtx context.Context) error {
		row, err := r.nativeTaskParentRow(txCtx, parent.RunID, true)
		if err != nil {
			return err
		}
		if err = validateNativeTaskParent(parent, row); err != nil {
			return err
		}
		if !cancellation && nativeTaskStopReason(row, parent.Deadline) != "" {
			return fmt.Errorf("%w: Native parent is stopped", apperrors.ErrConflict)
		}
		return execute(ctx)
	})
}
