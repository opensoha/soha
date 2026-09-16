package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/gorm"
)

func (r *Repository) FindCapabilityRun(ctx context.Context, actor, key, digest string) (domainworkflow.Run, error) {
	run, err := scanWorkflowRow(r.db.WithContext(ctx).Raw(`SELECT `+workflowColumns+` FROM workflow_runs WHERE scope = 'capability_task' AND metadata->>'capabilityActorId' = ? AND metadata->>'capabilityIdempotencyKey' = ?`, actor, key).Row())
	if err != nil {
		return run, err
	}
	intent, err := domainworkflow.CapabilityIntentFrom(run)
	if err != nil {
		return run, err
	}
	initialDigest, _ := run.Metadata["capabilityInitialDigest"].(string)
	if initialDigest == "" {
		initialDigest = intent.Digest
	}
	if initialDigest != digest {
		return run, fmt.Errorf("%w: task idempotency key belongs to a different plan", apperrors.ErrConflict)
	}
	return run, nil
}

func (r *Repository) CreateCapabilityRun(ctx context.Context, run domainworkflow.Run) (domainworkflow.Run, error) {
	intent, err := domainworkflow.CapabilityIntentFrom(run)
	if err != nil || run.ApplicationID != "" || run.DeliveryBatchID != "" || intent.Input.IdempotencyKey == "" {
		return run, apperrors.ErrInvalidArgument
	}
	var stored domainworkflow.Run
	err = dbtx.DB(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, "capability:"+intent.ActorID+":"+intent.Input.IdempotencyKey).Error; err != nil {
			return err
		}
		repo := New(tx)
		existing, err := repo.FindCapabilityRun(ctx, intent.ActorID, intent.Input.IdempotencyKey, intent.Digest)
		if err == nil {
			stored = existing
			return nil
		}
		if !errors.Is(err, apperrors.ErrNotFound) && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		run.Metadata["capabilityActorId"] = intent.ActorID
		run.Metadata["capabilityInitialDigest"] = intent.Digest
		run.Metadata["capabilityIdempotencyKey"] = intent.Input.IdempotencyKey
		stored, err = repo.createRun(ctx, run)
		return err
	})
	return stored, err
}

func (r *Repository) ReviseCapabilityRun(ctx context.Context, id string, version int64, revise func(domainworkflow.Run) (domainworkflow.Run, error)) (domainworkflow.Run, error) {
	var updated domainworkflow.Run
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		run, err := scanWorkflowRow(tx.Raw(`SELECT `+workflowColumns+` FROM workflow_runs WHERE id = ? AND scope = 'capability_task' AND version = ? AND status IN ('completed','failed','canceled','inconclusive','blocked') AND (lease_until IS NULL OR lease_until <= NOW()) FOR UPDATE`, id, version).Row())
		if err != nil {
			return fmt.Errorf("%w: task changed or is still executing", apperrors.ErrConflict)
		}
		run, err = revise(run)
		if err != nil {
			return err
		}
		metadata, err := json.Marshal(persistWorkflowMetadata(run.Metadata, run.NodeRuns, run.GatewayAuthorization))
		if err != nil {
			return err
		}
		updated, err = scanWorkflowRow(tx.Raw(`UPDATE workflow_runs SET metadata = ?::json, status = 'queued', stop_reason = '', stop_summary = '', lease_owner = '', lease_until = NULL, version = version + 1, fencing_token = fencing_token + 1, updated_at = NOW() WHERE id = ? AND version = ? RETURNING `+workflowColumns, string(metadata), id, version).Row())
		return err
	})
	return updated, err
}

func (r *Repository) ListCapabilityRuns(ctx context.Context, actor string, limit int) ([]domainworkflow.Run, error) {
	if limit < 1 || limit > 100 {
		limit = 20
	}
	query := `SELECT ` + workflowColumns + ` FROM workflow_runs WHERE scope = 'capability_task'`
	args := []any{}
	if actor != "" {
		query += ` AND metadata->>'capabilityActorId' = ?`
		args = append(args, actor)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	runs := []domainworkflow.Run{}
	for rows.Next() {
		run, err := scanWorkflow(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

// Serialize dispatch, cancellation and approval against the same existing Run.
// Domain writes keep their own idempotency records for crash recovery; this lock
// prevents a stale worker or a concurrently canceled plan from starting a step.
func (r *Repository) UpdateCapabilityNode(ctx context.Context, expected domainworkflow.Run, nodeID string, advance func(domainworkflow.Run, domainworkflow.NodeRun) (domainworkflow.NodeRun, error)) (domainworkflow.Run, error) {
	var updated domainworkflow.Run
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		current, err := scanWorkflowRow(tx.Raw(`SELECT `+workflowColumns+` FROM workflow_runs WHERE id = ? AND scope = 'capability_task' AND version = ? AND fencing_token = ? AND lease_owner = ? AND lease_until > NOW() FOR UPDATE`, expected.ID, expected.Version, expected.FencingToken, expected.LeaseOwner).Row())
		if err != nil {
			return fmt.Errorf("%w: capability dispatch lease changed", apperrors.ErrConflict)
		}
		index := -1
		for i, node := range current.NodeRuns {
			if node.NodeID == nodeID {
				index = i
				break
			}
		}
		if index < 0 {
			return apperrors.ErrInvalidArgument
		}
		node, err := advance(current, current.NodeRuns[index])
		if err != nil {
			return err
		}
		current.NodeRuns[index] = node
		updated, err = New(tx).SaveManagedRun(ctx, current, false)
		return err
	})
	return updated, err
}

// Approval is an independent HTTP request and does not hold a worker lease.
// It must still observe the same stop state and frozen node before dispatch.
func (r *Repository) WithCapabilityApproval(ctx context.Context, runID string, cancellation bool, checkAndExecute func(domainworkflow.Run) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		run, err := scanWorkflowRow(tx.Raw(`SELECT `+workflowColumns+` FROM workflow_runs WHERE id = ? AND scope = 'capability_task' AND (stop_reason = '' OR ?) AND status NOT IN ('completed','partially_completed','failed','canceled','inconclusive') FOR UPDATE`, runID, cancellation).Row())
		if err != nil {
			return fmt.Errorf("%w: capability task is stopped or unavailable", apperrors.ErrConflict)
		}
		return checkAndExecute(run)
	})
}
