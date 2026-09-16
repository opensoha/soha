package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/repository/deliverysource"
	"gorm.io/gorm"
)

func (r *Repository) ListDeliveryWorkflows(ctx context.Context) ([]domainworkflow.DeliveryWorkflow, error) {
	rows, err := r.db.WithContext(ctx).Raw(`SELECT id, version, definition, created_by, created_at, updated_at FROM delivery_workflows ORDER BY updated_at DESC`).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := []domainworkflow.DeliveryWorkflow{}
	for rows.Next() {
		item, err := scanDeliveryWorkflow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetDeliveryWorkflow(ctx context.Context, id string) (domainworkflow.DeliveryWorkflow, error) {
	return scanDeliveryWorkflow(r.db.WithContext(ctx).Raw(`SELECT id, version, definition, created_by, created_at, updated_at FROM delivery_workflows WHERE id = ?`, id).Row())
}

func (r *Repository) SaveDeliveryWorkflow(ctx context.Context, item domainworkflow.DeliveryWorkflow, expectedVersion int64) (domainworkflow.DeliveryWorkflow, error) {
	body, err := json.Marshal(item.Definition)
	if err != nil {
		return item, err
	}
	if expectedVersion == 0 {
		row := r.db.WithContext(ctx).Raw(`INSERT INTO delivery_workflows (id, definition, created_by) VALUES (?, ?::jsonb, ?)
            RETURNING id, version, definition, created_by, created_at, updated_at`, item.ID, string(body), item.CreatedBy).Row()
		return scanDeliveryWorkflow(row)
	}
	var updated domainworkflow.DeliveryWorkflow
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var version int64
		if err := tx.Raw(`SELECT version FROM delivery_workflows WHERE id = ? FOR UPDATE`, item.ID).Row().Scan(&version); err != nil {
			return err
		}
		if err := deliverysource.CheckWrite(tx, "Workflow", item.ID); err != nil {
			return err
		}
		row := tx.Raw(`UPDATE delivery_workflows SET definition = ?::jsonb, version = version + 1, updated_at = NOW()
        WHERE id = ? AND version = ? RETURNING id, version, definition, created_by, created_at, updated_at`, string(body), item.ID, expectedVersion).Row()
		var err error
		updated, err = scanDeliveryWorkflow(row)
		return err
	})
	if errors.Is(err, apperrors.ErrNotFound) || errors.Is(err, sql.ErrNoRows) {
		return item, fmt.Errorf("%w: delivery workflow changed; reload before saving", apperrors.ErrConflict)
	}
	return updated, err
}

type deliveryScanner interface{ Scan(...any) error }

func scanDeliveryWorkflow(row deliveryScanner) (domainworkflow.DeliveryWorkflow, error) {
	var item domainworkflow.DeliveryWorkflow
	var body []byte
	if err := row.Scan(&item.ID, &item.Version, &body, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return item, fmt.Errorf("%w: delivery workflow not found", apperrors.ErrNotFound)
		}
		return item, err
	}
	if err := json.Unmarshal(body, &item.Definition); err != nil {
		return item, err
	}
	return item, nil
}

func (r *Repository) CreateDeliveryBatch(ctx context.Context, batch domainworkflow.DeliveryBatch, run domainworkflow.Run, key, digest string) (domainworkflow.DeliveryBatch, domainworkflow.Run, error) {
	var storedBatch domainworkflow.DeliveryBatch
	var storedRun domainworkflow.Run
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Scope the idempotency key to its actor; serialize only identical keys.
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, batch.CreatedBy+":"+key).Error; err != nil {
			return err
		}
		repo := New(tx)
		var existingID, existingDigest string
		err := tx.Raw(`SELECT id, request_digest FROM delivery_batches WHERE created_by = ? AND idempotency_key = ?`, batch.CreatedBy, key).Row().Scan(&existingID, &existingDigest)
		if err == nil {
			if existingDigest != digest {
				return fmt.Errorf("%w: idempotency key was already used for a different request", apperrors.ErrConflict)
			}
			storedBatch, storedRun, err = repo.GetDeliveryBatch(ctx, existingID)
			return err
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if run.ID != batch.RootRunID || run.Scope != domainworkflow.ScopeDeliveryBatch || run.DeliveryBatchID != batch.ID || run.ApplicationID != "" {
			return fmt.Errorf("%w: batch must own exactly one scoped root run", apperrors.ErrInvalidArgument)
		}
		storedRun, err = repo.createRun(ctx, run)
		if err != nil {
			return err
		}
		body, err := json.Marshal(batch)
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO delivery_batches (id, root_run_id, created_by, idempotency_key, request_digest, snapshot, created_at)
            VALUES (?, ?, ?, ?, ?, ?::jsonb, ?)`, batch.ID, batch.RootRunID, batch.CreatedBy, key, digest, string(body), batch.CreatedAt).Error; err != nil {
			return err
		}
		storedBatch = batch
		return nil
	})
	return storedBatch, storedRun, err
}

func (r *Repository) GetDeliveryBatch(ctx context.Context, id string) (domainworkflow.DeliveryBatch, domainworkflow.Run, error) {
	var batch domainworkflow.DeliveryBatch
	var body []byte
	err := r.db.WithContext(ctx).Raw(`SELECT snapshot FROM delivery_batches WHERE id = ?`, id).Row().Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return batch, domainworkflow.Run{}, fmt.Errorf("%w: delivery batch not found", apperrors.ErrNotFound)
	}
	if err != nil {
		return batch, domainworkflow.Run{}, err
	}
	if err := json.Unmarshal(body, &batch); err != nil {
		return batch, domainworkflow.Run{}, err
	}
	run, err := r.Get(ctx, batch.RootRunID)
	return batch, run, err
}

func (r *Repository) FindDeliveryBatch(ctx context.Context, actorID, key, digest string) (domainworkflow.DeliveryBatch, domainworkflow.Run, error) {
	var id, existingDigest string
	err := r.db.WithContext(ctx).Raw(`SELECT id, request_digest FROM delivery_batches WHERE created_by = ? AND idempotency_key = ?`, actorID, key).Row().Scan(&id, &existingDigest)
	if errors.Is(err, sql.ErrNoRows) {
		return domainworkflow.DeliveryBatch{}, domainworkflow.Run{}, apperrors.ErrNotFound
	}
	if err != nil {
		return domainworkflow.DeliveryBatch{}, domainworkflow.Run{}, err
	}
	if existingDigest != digest {
		return domainworkflow.DeliveryBatch{}, domainworkflow.Run{}, fmt.Errorf("%w: idempotency key was already used for a different request", apperrors.ErrConflict)
	}
	return r.GetDeliveryBatch(ctx, id)
}

func (r *Repository) ListDeliveryBatchIDs(ctx context.Context, applicationID, serviceID, workflowID string, limit int) ([]string, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	query, args := `SELECT id FROM delivery_batches WHERE TRUE`, []any{}
	if applicationID != "" || serviceID != "" {
		query += ` AND EXISTS (SELECT 1 FROM jsonb_array_elements(snapshot->'targets') AS target WHERE TRUE`
		if applicationID != "" {
			query += ` AND target->'target'->>'applicationId' = ?`
			args = append(args, applicationID)
		}
		if serviceID != "" {
			query += ` AND target->'target'->>'serviceId' = ?`
			args = append(args, serviceID)
		}
		query += `)`
	}
	if workflowID != "" {
		query += ` AND snapshot->>'workflowId' = ?`
		args = append(args, workflowID)
	}
	query += ` ORDER BY created_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	var ids []string
	err := r.db.WithContext(ctx).Raw(query, args...).Scan(&ids).Error
	return ids, err
}

// ClaimManagedRun is used by the existing workflow worker pool. A crash leaves
// a leased Run in the database; expiry makes that same Run recoverable.
func (r *Repository) ClaimManagedRun(ctx context.Context, owner string, ttl time.Duration) (domainworkflow.Run, error) {
	if owner == "" || ttl <= 0 {
		return domainworkflow.Run{}, apperrors.ErrInvalidArgument
	}
	query := `WITH candidate AS (
        SELECT id FROM workflow_runs WHERE scope IN ('delivery_batch', 'capability_task')
          AND status NOT IN ('completed', 'partially_completed', 'failed', 'canceled', 'inconclusive', 'blocked')
          AND (scope != 'capability_task' OR COALESCE((metadata->>'capabilityNextPollAt')::timestamptz, NOW()) <= NOW())
          AND (lease_until IS NULL OR lease_until <= NOW())
        ORDER BY updated_at, id FOR UPDATE SKIP LOCKED LIMIT 1
    ) UPDATE workflow_runs SET lease_owner = ?, lease_until = NOW() + (? * INTERVAL '1 millisecond'),
          fencing_token = fencing_token + 1, version = version + 1
      WHERE id = (SELECT id FROM candidate) RETURNING ` + workflowColumns
	return scanWorkflowRow(r.db.WithContext(ctx).Raw(query, owner, ttl.Milliseconds()).Row())
}

func (r *Repository) SaveManagedRun(ctx context.Context, run domainworkflow.Run, releaseLease bool) (domainworkflow.Run, error) {
	if run.Scope != domainworkflow.ScopeDeliveryBatch && run.Scope != domainworkflow.ScopeCapabilityTask {
		return run, apperrors.ErrInvalidArgument
	}
	metadata, err := json.Marshal(persistWorkflowMetadata(run.Metadata, run.NodeRuns, run.GatewayAuthorization))
	if err != nil {
		return run, err
	}
	steps, err := json.Marshal(run.Steps)
	if err != nil {
		return run, err
	}
	query := `UPDATE workflow_runs SET status = ?, steps = ?::json, metadata = ?::json, version = version + 1, updated_at = NOW()`
	if releaseLease {
		query += `, lease_owner = '', lease_until = NULL`
	}
	query += ` WHERE id = ? AND scope = ? AND version = ? AND fencing_token = ? AND lease_owner = ? AND lease_until > NOW() RETURNING ` + workflowColumns
	updated, err := scanWorkflowRow(r.db.WithContext(ctx).Raw(query, run.Status, string(steps), string(metadata), run.ID, run.Scope, run.Version, run.FencingToken, run.LeaseOwner).Row())
	if errors.Is(err, apperrors.ErrNotFound) {
		return run, fmt.Errorf("%w: delivery run changed or its execution lease expired", apperrors.ErrConflict)
	}
	return updated, err
}

func (r *Repository) StopManagedRun(ctx context.Context, runID, reason, summary string) (domainworkflow.Run, error) {
	if reason != "user" && reason != "failure" {
		return domainworkflow.Run{}, apperrors.ErrInvalidArgument
	}
	// The first cause remains authoritative. Stop invalidates a dispatcher's CAS
	// without pretending that any in-flight executor has acknowledged cancellation.
	query := `UPDATE workflow_runs SET stop_reason = CASE WHEN stop_reason = '' THEN ? ELSE stop_reason END, stop_summary = CASE WHEN stop_reason = '' THEN ? ELSE stop_summary END, status = 'canceling', version = version + 1, updated_at = NOW()
        WHERE id = ? AND scope IN ('delivery_batch', 'capability_task') AND (stop_reason = '' OR status = 'blocked')
          AND status NOT IN ('completed', 'partially_completed', 'failed', 'canceled', 'inconclusive') RETURNING ` + workflowColumns
	run, err := scanWorkflowRow(r.db.WithContext(ctx).Raw(query, reason, summary, runID).Row())
	if errors.Is(err, apperrors.ErrNotFound) {
		return r.Get(ctx, runID)
	}
	return run, err
}
