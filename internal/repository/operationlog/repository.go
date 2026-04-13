package operationlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	domainoperation "github.com/kubecrux/kubecrux/internal/domain/operation"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) List(ctx context.Context, limit int) ([]domainoperation.Entry, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, actor_id, operation_type, target_scope, result, summary, metadata, created_at
		FROM operation_logs
		ORDER BY created_at DESC
		LIMIT ?
	`, limit).Rows()
	if err != nil {
		return nil, fmt.Errorf("query operation logs: %w", err)
	}
	defer rows.Close()

	items := make([]domainoperation.Entry, 0, limit)
	for rows.Next() {
		item, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanEntry(rows *sql.Rows) (domainoperation.Entry, error) {
	var item domainoperation.Entry
	var targetScope []byte
	var metadata []byte
	var createdAt time.Time
	if err := rows.Scan(&item.ID, &item.ActorID, &item.OperationType, &targetScope, &item.Result, &item.Summary, &metadata, &createdAt); err != nil {
		return domainoperation.Entry{}, err
	}
	item.CreatedAt = createdAt.Format(time.RFC3339)
	if len(targetScope) > 0 {
		_ = json.Unmarshal(targetScope, &item.TargetScope)
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &item.Metadata)
	}
	if item.TargetScope == nil {
		item.TargetScope = map[string]any{}
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}
