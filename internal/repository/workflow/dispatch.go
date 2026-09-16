package workflow

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

// LockDeliveryNode must be called inside the transaction creating a node's
// records. Stop uses the same row lock, so creation and stop have one order.
func LockDeliveryNode(ctx context.Context, tx *gorm.DB, applicationID, environmentID string) error {
	node, ok := domainworkflow.NodeExecutionFrom(ctx)
	if !ok || node.RunID == "" || node.NodeID == "" || node.Attempt != 1 {
		return fmt.Errorf("%w: a current delivery node execution is required", apperrors.ErrConflict)
	}
	run, err := scanWorkflowRow(tx.WithContext(ctx).Raw(`SELECT `+workflowColumns+` FROM workflow_runs
		WHERE id = ? AND scope = 'delivery_batch' AND version = ? AND fencing_token = ? AND lease_owner = ?
		AND lease_until > NOW() AND stop_reason = '' AND status NOT IN ('completed', 'partially_completed', 'failed', 'canceled', 'canceling') FOR UPDATE`, node.RunID, node.Version, node.FencingToken, node.LeaseOwner).Row())
	if errors.Is(err, apperrors.ErrNotFound) {
		return fmt.Errorf("%w: delivery was stopped or the worker lease changed", apperrors.ErrConflict)
	}
	if err != nil {
		return err
	}
	matched := false
	for _, entry := range run.NodeRuns {
		if entry.NodeID == node.NodeID && entry.TargetID == node.TargetID && entry.Stage == node.Stage && (entry.Status == "running" || entry.Status == "waiting_execution" || entry.Status == "waiting_approval") {
			matched = true
			break
		}
	}
	if !matched {
		return fmt.Errorf("%w: delivery node is not dispatchable", apperrors.ErrConflict)
	}
	batch, _, err := New(tx).GetDeliveryBatch(ctx, run.DeliveryBatchID)
	if err != nil {
		return err
	}
	for _, target := range batch.Targets {
		if target.Target.ID == node.TargetID && target.Target.ApplicationID == applicationID && target.Target.ApplicationEnvironmentID == environmentID {
			return nil
		}
	}
	return fmt.Errorf("%w: task does not belong to its frozen delivery target", apperrors.ErrAccessDenied)
}

// LockDeliveryDispatch protects executor claim, independently of the short Run
// worker lease: an already-created task survives worker recovery.
func LockDeliveryDispatch(ctx context.Context, tx *gorm.DB, payload map[string]any) error {
	if payload["workflowScope"] != domainworkflow.ScopeDeliveryBatch {
		return nil
	}
	var id string
	err := tx.WithContext(ctx).Raw(`SELECT id FROM workflow_runs WHERE id = ? AND scope = 'delivery_batch'
		AND stop_reason = '' AND status NOT IN ('completed', 'partially_completed', 'failed', 'canceled', 'canceling') FOR UPDATE`, payload["workflowRunId"]).Row().Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: delivery no longer permits executor dispatch", apperrors.ErrConflict)
	}
	return err
}
