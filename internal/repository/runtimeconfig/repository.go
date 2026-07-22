package runtimeconfig

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	domainruntimeconfig "github.com/opensoha/soha/internal/domain/runtimeconfig"
	"gorm.io/gorm"
)

const stateID = "default"

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) LoadState(ctx context.Context) (domainruntimeconfig.State, error) {
	var state domainruntimeconfig.State
	var raw []byte
	err := r.db.WithContext(ctx).Raw(`
		SELECT version, COALESCE(active_revision_id, ''), overrides, COALESCE(updated_by, ''), updated_at
		FROM runtime_config_state WHERE id = ?
	`, stateID).Row().Scan(&state.Version, &state.ActiveRevisionID, &raw, &state.UpdatedBy, &state.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domainruntimeconfig.State{Overrides: map[string]any{}}, nil
	}
	if err != nil {
		return domainruntimeconfig.State{}, fmt.Errorf("load runtime config state: %w", err)
	}
	if err := decodeMap(raw, &state.Overrides); err != nil {
		return domainruntimeconfig.State{}, fmt.Errorf("decode runtime config state: %w", err)
	}
	return state, nil
}

func (r *Repository) Commit(ctx context.Context, input domainruntimeconfig.Commit) (domainruntimeconfig.State, error) {
	var result domainruntimeconfig.State
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			INSERT INTO runtime_config_state (id, version, overrides, updated_by)
			VALUES (?, 0, '{}'::jsonb, 'system') ON CONFLICT (id) DO NOTHING
		`, stateID).Error; err != nil {
			return err
		}
		var currentVersion int64
		if err := tx.Raw(`SELECT version FROM runtime_config_state WHERE id = ? FOR UPDATE`, stateID).Row().Scan(&currentVersion); err != nil {
			return err
		}
		if currentVersion != input.ExpectedVersion {
			return domainruntimeconfig.ErrVersionConflict
		}
		changes, err := json.Marshal(input.Revision.Changes)
		if err != nil {
			return fmt.Errorf("marshal runtime config changes: %w", err)
		}
		snapshot, err := json.Marshal(input.Revision.Snapshot)
		if err != nil {
			return fmt.Errorf("marshal runtime config snapshot: %w", err)
		}
		items, err := json.Marshal(input.Application.Items)
		if err != nil {
			return fmt.Errorf("marshal runtime config application items: %w", err)
		}
		if err := tx.Exec(`
			INSERT INTO runtime_config_revisions
				(id, version, status, changes, snapshot, actor, reason, rollback_of_revision_id, created_at)
			VALUES (?, ?, ?, ?::jsonb, ?::jsonb, ?, ?, NULLIF(?, ''), ?)
		`, input.Revision.ID, input.Revision.Version, input.Revision.Status, string(changes), string(snapshot), input.Revision.Actor, input.Revision.Reason, input.Revision.RollbackOfRevisionID, input.Revision.CreatedAt).Error; err != nil {
			return err
		}
		if err := tx.Exec(`
			INSERT INTO runtime_config_applications
				(id, revision_id, version, status, items, error, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?::jsonb, NULLIF(?, ''), ?, ?)
		`, input.Application.ID, input.Application.RevisionID, input.Application.Version, input.Application.Status, string(items), input.Application.Error, input.Application.CreatedAt, input.Application.UpdatedAt).Error; err != nil {
			return err
		}
		if err := tx.Exec(`
			UPDATE runtime_config_state
			SET version = ?, active_revision_id = ?, overrides = ?::jsonb, updated_by = ?, updated_at = ?
			WHERE id = ?
		`, input.Revision.Version, input.Revision.ID, string(snapshot), input.Revision.Actor, input.Revision.CreatedAt, stateID).Error; err != nil {
			return err
		}
		result = domainruntimeconfig.State{Version: input.Revision.Version, ActiveRevisionID: input.Revision.ID, Overrides: cloneMap(input.Revision.Snapshot), UpdatedBy: input.Revision.Actor, UpdatedAt: input.Revision.CreatedAt}
		return nil
	})
	if err != nil {
		return domainruntimeconfig.State{}, err
	}
	return result, nil
}

func (r *Repository) UpdateApplication(ctx context.Context, application domainruntimeconfig.Application) error {
	items, err := json.Marshal(application.Items)
	if err != nil {
		return fmt.Errorf("marshal runtime config application items: %w", err)
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			UPDATE runtime_config_applications SET status = ?, items = ?::jsonb, error = NULLIF(?, ''), updated_at = ? WHERE id = ?
		`, application.Status, string(items), application.Error, application.UpdatedAt, application.ID).Error; err != nil {
			return err
		}
		return tx.Exec(`UPDATE runtime_config_revisions SET status = ? WHERE id = ?`, application.Status, application.RevisionID).Error
	})
}

func (r *Repository) ListRevisions(ctx context.Context, limit int) ([]domainruntimeconfig.Revision, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, version, status, changes, snapshot, actor, COALESCE(reason, ''),
			COALESCE(rollback_of_revision_id, ''), created_at
		FROM runtime_config_revisions ORDER BY version DESC LIMIT ?
	`, limit).Rows()
	if err != nil {
		return nil, fmt.Errorf("list runtime config revisions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]domainruntimeconfig.Revision, 0, limit)
	for rows.Next() {
		item, err := scanRevision(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetRevisionByVersion(ctx context.Context, version int64) (domainruntimeconfig.Revision, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, version, status, changes, snapshot, actor, COALESCE(reason, ''),
			COALESCE(rollback_of_revision_id, ''), created_at
		FROM runtime_config_revisions WHERE version = ?
	`, version).Row()
	item, err := scanRevision(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domainruntimeconfig.Revision{}, domainruntimeconfig.ErrNotFound
	}
	return item, err
}

func (r *Repository) GetApplication(ctx context.Context, id string) (domainruntimeconfig.Application, error) {
	var item domainruntimeconfig.Application
	var raw []byte
	err := r.db.WithContext(ctx).Raw(`
		SELECT id, revision_id, version, status, items, COALESCE(error, ''), created_at, updated_at
		FROM runtime_config_applications WHERE id = ?
	`, id).Row().Scan(&item.ID, &item.RevisionID, &item.Version, &item.Status, &raw, &item.Error, &item.CreatedAt, &item.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domainruntimeconfig.Application{}, domainruntimeconfig.ErrNotFound
	}
	if err != nil {
		return domainruntimeconfig.Application{}, fmt.Errorf("get runtime config application: %w", err)
	}
	if err := json.Unmarshal(raw, &item.Items); err != nil {
		return domainruntimeconfig.Application{}, fmt.Errorf("decode runtime config application: %w", err)
	}
	return item, nil
}

type scanner interface {
	Scan(...any) error
}

func scanRevision(row scanner) (domainruntimeconfig.Revision, error) {
	var item domainruntimeconfig.Revision
	var changes, snapshot []byte
	if err := row.Scan(&item.ID, &item.Version, &item.Status, &changes, &snapshot, &item.Actor, &item.Reason, &item.RollbackOfRevisionID, &item.CreatedAt); err != nil {
		return domainruntimeconfig.Revision{}, err
	}
	if err := json.Unmarshal(changes, &item.Changes); err != nil {
		return domainruntimeconfig.Revision{}, fmt.Errorf("decode runtime config revision changes: %w", err)
	}
	if err := decodeMap(snapshot, &item.Snapshot); err != nil {
		return domainruntimeconfig.Revision{}, fmt.Errorf("decode runtime config revision snapshot: %w", err)
	}
	return item, nil
}

func decodeMap(raw []byte, target *map[string]any) error {
	*target = map[string]any{}
	if len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, target)
}

func cloneMap(source map[string]any) map[string]any {
	out := make(map[string]any, len(source))
	for key, value := range source {
		out[key] = value
	}
	return out
}
