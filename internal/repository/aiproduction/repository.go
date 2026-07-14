package aiproduction

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	appaiproduction "github.com/opensoha/soha/internal/application/aiproduction"
	"gorm.io/gorm"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db: db} }

func put(ctx context.Context, db *gorm.DB, query string, args ...any) error {
	if err := db.WithContext(ctx).Exec(query, args...).Error; err != nil {
		return fmt.Errorf("persist AI production resource: %w", err)
	}
	return nil
}
func list[T any](ctx context.Context, db *gorm.DB, query string) ([]T, error) {
	rows, err := db.WithContext(ctx).Raw(query).Rows()
	if err != nil {
		return nil, fmt.Errorf("list AI production resources: %w", err)
	}
	defer rows.Close()
	items := make([]T, 0)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, err
		}
		var item T
		if err := json.Unmarshal(payload, &item); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
func get[T any](ctx context.Context, db *gorm.DB, query, id string) (T, error) {
	var zero T
	var payload []byte
	if err := db.WithContext(ctx).Raw(query, id).Row().Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return zero, appaiproduction.ErrNotFound
		}
		return zero, err
	}
	var item T
	if err := json.Unmarshal(payload, &item); err != nil {
		return zero, err
	}
	return item, nil
}

func (r *Repository) ListRollouts(ctx context.Context) ([]appaiproduction.ProviderRollout, error) {
	return list[appaiproduction.ProviderRollout](ctx, r.db, `SELECT payload FROM ai_provider_rollouts ORDER BY updated_at DESC`)
}
func (r *Repository) PutRollout(ctx context.Context, item appaiproduction.ProviderRollout) error {
	payload, err := json.Marshal(item)
	if err != nil {
		return err
	}
	return put(ctx, r.db, `INSERT INTO ai_provider_rollouts(id,status,payload,created_at,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=EXCLUDED.status,payload=EXCLUDED.payload,updated_at=EXCLUDED.updated_at`, item.ID, item.Status, payload, item.CreatedAt, item.UpdatedAt)
}
func (r *Repository) GetRollout(ctx context.Context, id string) (appaiproduction.ProviderRollout, error) {
	return get[appaiproduction.ProviderRollout](ctx, r.db, `SELECT payload FROM ai_provider_rollouts WHERE id=?`, id)
}
func (r *Repository) ListConformanceRuns(ctx context.Context) ([]appaiproduction.ConformanceRun, error) {
	return list[appaiproduction.ConformanceRun](ctx, r.db, `SELECT payload FROM ai_provider_conformance_runs ORDER BY updated_at DESC`)
}
func (r *Repository) PutConformanceRun(ctx context.Context, item appaiproduction.ConformanceRun) error {
	payload, err := json.Marshal(item)
	if err != nil {
		return err
	}
	return put(ctx, r.db, `INSERT INTO ai_provider_conformance_runs(id,status,payload,created_at,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=EXCLUDED.status,payload=EXCLUDED.payload,updated_at=EXCLUDED.updated_at`, item.ID, item.Status, payload, item.CreatedAt, item.UpdatedAt)
}
func (r *Repository) ListEnvironmentTemplates(ctx context.Context) ([]appaiproduction.EnvironmentTemplate, error) {
	return list[appaiproduction.EnvironmentTemplate](ctx, r.db, `SELECT payload FROM ai_environment_templates ORDER BY updated_at DESC`)
}
func (r *Repository) PutEnvironmentTemplate(ctx context.Context, item appaiproduction.EnvironmentTemplate) error {
	payload, err := json.Marshal(item)
	if err != nil {
		return err
	}
	return put(ctx, r.db, `INSERT INTO ai_environment_templates(id,status,payload,created_at,updated_at) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=EXCLUDED.status,payload=EXCLUDED.payload,updated_at=EXCLUDED.updated_at`, item.ID, item.Status, payload, item.CreatedAt, item.UpdatedAt)
}
func (r *Repository) ListEnvironmentLeases(ctx context.Context) ([]appaiproduction.EnvironmentLease, error) {
	return list[appaiproduction.EnvironmentLease](ctx, r.db, `SELECT payload FROM ai_environment_leases ORDER BY updated_at DESC`)
}
func (r *Repository) GetEnvironmentLease(ctx context.Context, id string) (appaiproduction.EnvironmentLease, error) {
	return get[appaiproduction.EnvironmentLease](ctx, r.db, `SELECT payload FROM ai_environment_leases WHERE id=?`, id)
}
func (r *Repository) PutEnvironmentLease(ctx context.Context, item appaiproduction.EnvironmentLease) error {
	payload, err := json.Marshal(item)
	if err != nil {
		return err
	}
	return put(ctx, r.db, `INSERT INTO ai_environment_leases(id,template_id,status,expires_at,payload,created_at,updated_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=EXCLUDED.status,expires_at=EXCLUDED.expires_at,payload=EXCLUDED.payload,updated_at=EXCLUDED.updated_at`, item.ID, item.TemplateID, item.Status, item.ExpiresAt, payload, item.CreatedAt, item.UpdatedAt)
}
func (r *Repository) ListOperations(ctx context.Context) ([]appaiproduction.Operation, error) {
	return list[appaiproduction.Operation](ctx, r.db, `SELECT payload FROM ai_production_operations ORDER BY updated_at DESC`)
}
func (r *Repository) PutOperation(ctx context.Context, item appaiproduction.Operation) error {
	payload, err := json.Marshal(item)
	if err != nil {
		return err
	}
	return put(ctx, r.db, `INSERT INTO ai_production_operations(id,kind,category,status,payload,created_at,updated_at) VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET status=EXCLUDED.status,payload=EXCLUDED.payload,updated_at=EXCLUDED.updated_at`, item.ID, item.Kind, item.Category, item.Status, payload, item.CreatedAt, item.UpdatedAt)
}
func (r *Repository) ListRunbookEvidence(ctx context.Context) ([]appaiproduction.RunbookEvidence, error) {
	return list[appaiproduction.RunbookEvidence](ctx, r.db, `SELECT payload FROM ai_runbook_evidence ORDER BY created_at DESC`)
}
