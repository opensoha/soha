package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	domaindocument "github.com/opensoha/soha/internal/domain/deliverydocument"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func readTemplateSyncRun(db *gorm.DB, source, id string) (domaindocument.StoredSyncRun, error) {
	var item domaindocument.StoredSyncRun
	var data []byte
	err := db.Raw(`SELECT definition, idempotency_key, COALESCE(apply_key, ''), repository_id FROM delivery_template_sync_runs WHERE source_id = ? AND id = ?`, source, id).Row().Scan(&data, &item.IdempotencyKey, &item.ApplyKey, &item.RepositoryID)
	if errors.Is(err, sql.ErrNoRows) {
		return item, ErrNotFound
	}
	if err != nil {
		return item, err
	}
	err = json.Unmarshal(data, &item.SyncRun)
	return item, err
}

func (r *Repository) GetTemplateSyncRun(ctx context.Context, source, id string) (domaindocument.StoredSyncRun, error) {
	var run domaindocument.StoredSyncRun
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := readTemplateSource(tx, source, true); err != nil {
			return err
		}
		var err error
		run, err = readTemplateSyncRun(tx, source, id)
		if err != nil {
			return err
		}
		return expireTemplateSyncRun(tx, &run)
	})
	return run, err
}

func (r *Repository) ListTemplateSyncRuns(ctx context.Context, source string, offset, limit int) ([]domaindocument.SyncRun, error) {
	rows, err := r.db.WithContext(ctx).Raw(`SELECT definition - 'preview' - 'removed' - 'result' FROM delivery_template_sync_runs WHERE source_id = ? ORDER BY created_at DESC, id OFFSET ? LIMIT ?`, source, offset, limit).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := []domaindocument.SyncRun{}
	for rows.Next() {
		var data []byte
		var item domaindocument.SyncRun
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) BeginTemplateSync(ctx context.Context, source, actor string, input domaindocument.SyncInput) (domaindocument.StoredSyncRun, bool, error) {
	var run domaindocument.StoredSyncRun
	created := false
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		item, err := readTemplateSource(tx, source, true)
		if err != nil {
			return err
		}
		var id string
		err = tx.Raw(`SELECT id FROM delivery_template_sync_runs WHERE source_id = ? AND actor_id = ? AND idempotency_key = ?`, source, actor, input.IdempotencyKey).Row().Scan(&id)
		if err == nil {
			run, err = readTemplateSyncRun(tx, source, id)
			if err != nil {
				return err
			}
			if run.SourceGeneration != input.ExpectedGeneration || run.RequestedCommit != input.ResolvedCommit {
				return fmt.Errorf("%w: sync key belongs to another generation or commit", apperrors.ErrConflict)
			}
			return expireTemplateSyncRun(tx, &run)
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if !item.Enabled || item.Generation != input.ExpectedGeneration {
			return fmt.Errorf("%w: source changed or is disabled", apperrors.ErrConflict)
		}
		if err := checkTemplateSyncAvailable(tx, item); err != nil {
			return err
		}
		now := time.Now().UTC()
		run = domaindocument.StoredSyncRun{SyncRun: domaindocument.SyncRun{ID: uuid.NewString(), SourceID: source, SourceGeneration: item.Generation, ActorID: actor, Status: "running", CreatedAt: now, UpdatedAt: now}, IdempotencyKey: input.IdempotencyKey, RepositoryID: item.RepositoryID}
		run.RequestedCommit = input.ResolvedCommit
		data, err := json.Marshal(run.SyncRun)
		if err != nil {
			return err
		}
		if err := tx.Exec(`INSERT INTO delivery_template_sync_runs (id, source_id, actor_id, idempotency_key, source_generation, repository_id, status, definition, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?, ?)`, run.ID, source, actor, input.IdempotencyKey, item.Generation, item.RepositoryID, run.Status, string(data), now, now).Error; err != nil {
			return err
		}
		item.LastSyncRunID, item.UpdatedAt = run.ID, now
		created = true
		return writeTemplateSource(tx, item)
	})
	return run, created, err
}

func checkTemplateSyncAvailable(tx *gorm.DB, source domaindocument.Source) error {
	if source.LastSyncRunID == "" {
		return nil
	}
	previous, err := readTemplateSyncRun(tx, source.ID, source.LastSyncRunID)
	if err != nil {
		return err
	}
	if err := expireTemplateSyncRun(tx, &previous); err != nil {
		return err
	}
	if previous.Status == "running" {
		return fmt.Errorf("%w: source sync is already running", apperrors.ErrConflict)
	}
	return nil
}

func expireTemplateSyncRun(tx *gorm.DB, run *domaindocument.StoredSyncRun) error {
	if run.Status != "running" || time.Since(run.CreatedAt) < 2*time.Minute {
		return nil
	}
	run.Status, run.ErrorCode, run.ErrorMessage = "failed", "sync_interrupted", "Sync was interrupted; start a new preview."
	return writeTemplateSyncRun(tx, run)
}

func writeTemplateSyncRun(tx *gorm.DB, run *domaindocument.StoredSyncRun) error {
	run.UpdatedAt = time.Now().UTC()
	data, err := json.Marshal(run.SyncRun)
	if err != nil {
		return err
	}
	return tx.Exec(`UPDATE delivery_template_sync_runs SET status = ?, definition = ?::jsonb, apply_key = NULLIF(?, ''), updated_at = ? WHERE id = ?`, run.Status, string(data), run.ApplyKey, run.UpdatedAt, run.ID).Error
}

func (r *Repository) FinishTemplateSync(ctx context.Context, run domaindocument.StoredSyncRun) (domaindocument.StoredSyncRun, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		source, err := readTemplateSource(tx, run.SourceID, true)
		if err != nil {
			return err
		}
		stored, err := readTemplateSyncRun(tx, run.SourceID, run.ID)
		if err != nil {
			return err
		}
		if stored.Status != "running" {
			run = stored
			return nil
		}
		if source.Generation != run.SourceGeneration || source.LastSyncRunID != run.ID {
			run.Status, run.Preview, run.Removed = "stale", nil, nil
			run.ErrorCode, run.ErrorMessage = "source_changed", "Source changed; start a new preview."
		}
		return writeTemplateSyncRun(tx, &run)
	})
	return run, err
}
