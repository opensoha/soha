package memory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	appmemory "github.com/opensoha/soha/internal/application/memory"
	"gorm.io/gorm"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) PutRecord(ctx context.Context, record appmemory.Record) error {
	payload, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("encode memory record: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`INSERT INTO ai_memory_records(id,owner_type,owner_id,scope_hash,status,expires_at,payload,created_at,deleted_at) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET owner_type=EXCLUDED.owner_type,owner_id=EXCLUDED.owner_id,scope_hash=EXCLUDED.scope_hash,status=EXCLUDED.status,expires_at=EXCLUDED.expires_at,payload=EXCLUDED.payload,deleted_at=EXCLUDED.deleted_at`, record.ID, record.OwnerType, record.OwnerID, record.ScopeHash, record.Status, record.ExpiresAt, payload, record.CreatedAt, record.DeletedAt).Error; err != nil {
		return fmt.Errorf("put memory record: %w", err)
	}
	return nil
}

func (r *Repository) ListRecords(ctx context.Context, ownerType, ownerID string, now time.Time) ([]appmemory.Record, error) {
	rows, err := r.db.WithContext(ctx).Raw(`SELECT payload FROM ai_memory_records WHERE owner_type=? AND owner_id=? AND status='active' AND expires_at>? ORDER BY created_at DESC`, ownerType, ownerID, now).Rows()
	if err != nil {
		return nil, fmt.Errorf("list memory records: %w", err)
	}
	defer rows.Close()
	items := make([]appmemory.Record, 0)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan memory record: %w", err)
		}
		var item appmemory.Record
		if err := json.Unmarshal(payload, &item); err != nil {
			return nil, fmt.Errorf("decode memory record: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate memory records: %w", err)
	}
	return items, nil
}

func (r *Repository) GetRecord(ctx context.Context, id string) (appmemory.Record, error) {
	var payload []byte
	row := r.db.WithContext(ctx).Raw(`SELECT payload FROM ai_memory_records WHERE id=?`, id).Row()
	if err := row.Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return appmemory.Record{}, appmemory.ErrNotFound
		}
		return appmemory.Record{}, fmt.Errorf("get memory record: %w", err)
	}
	var item appmemory.Record
	if err := json.Unmarshal(payload, &item); err != nil {
		return appmemory.Record{}, fmt.Errorf("decode memory record: %w", err)
	}
	return item, nil
}

func (r *Repository) DeleteRecord(ctx context.Context, id string, now time.Time) error {
	result := r.db.WithContext(ctx).Exec(`UPDATE ai_memory_records SET status='deleted',deleted_at=?,payload=jsonb_set(jsonb_set(payload,'{status}','"deleted"'::jsonb),'{deletedAt}',to_jsonb(?::timestamptz)) WHERE id=? AND status<>'deleted'`, now, now, id)
	if result.Error != nil {
		return fmt.Errorf("delete memory record: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return appmemory.ErrNotFound
	}
	return nil
}

func (r *Repository) PutPolicy(ctx context.Context, policy appmemory.Policy) error {
	payload, err := json.Marshal(policy)
	if err != nil {
		return fmt.Errorf("encode memory policy: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`INSERT INTO ai_memory_policies(id,version,payload,updated_at) VALUES(?,?,?,NOW()) ON CONFLICT(id,version) DO UPDATE SET payload=EXCLUDED.payload,updated_at=NOW()`, policy.ID, policy.Version, payload).Error; err != nil {
		return fmt.Errorf("put memory policy: %w", err)
	}
	return nil
}

func (r *Repository) GetPolicy(ctx context.Context, id, version string) (appmemory.Policy, error) {
	var payload []byte
	if err := r.db.WithContext(ctx).Raw(`SELECT payload FROM ai_memory_policies WHERE id=? AND version=?`, id, version).Row().Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return appmemory.Policy{}, appmemory.ErrNotFound
		}
		return appmemory.Policy{}, fmt.Errorf("get memory policy: %w", err)
	}
	var policy appmemory.Policy
	if err := json.Unmarshal(payload, &policy); err != nil {
		return appmemory.Policy{}, fmt.Errorf("decode memory policy: %w", err)
	}
	return policy, nil
}

func (r *Repository) ListPolicies(ctx context.Context) ([]appmemory.Policy, error) {
	rows, err := r.db.WithContext(ctx).Raw(`SELECT payload FROM ai_memory_policies ORDER BY id,version`).Rows()
	if err != nil {
		return nil, fmt.Errorf("list memory policies: %w", err)
	}
	defer rows.Close()
	items := make([]appmemory.Policy, 0)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan memory policy: %w", err)
		}
		var item appmemory.Policy
		if err := json.Unmarshal(payload, &item); err != nil {
			return nil, fmt.Errorf("decode memory policy: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
