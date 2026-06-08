package operationlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, entry domainoperation.Entry) error {
	targetScope, err := json.Marshal(entry.TargetScope)
	if err != nil {
		return fmt.Errorf("marshal operation target scope: %w", err)
	}
	metadata, err := json.Marshal(entry.Metadata)
	if err != nil {
		return fmt.Errorf("marshal operation metadata: %w", err)
	}
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO operation_logs (
			id, actor_id, actor_name, operation_type, target_scope, result, summary, request_path,
			request_method, request_id, source_ip, metadata, created_at
		) VALUES (
			?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
		)
	`,
		entry.ID,
		entry.ActorID,
		entry.ActorName,
		entry.OperationType,
		string(targetScope),
		entry.Result,
		entry.Summary,
		entry.RequestPath,
		entry.RequestMethod,
		entry.RequestID,
		entry.SourceIP,
		string(metadata),
		entry.CreatedAt,
	).Error
}

func (r *Repository) List(ctx context.Context, filter domainoperation.Filter) ([]domainoperation.Entry, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, actor_id, actor_name, operation_type, target_scope, result, summary, request_path,
		       request_method, request_id, source_ip, metadata, created_at
		FROM operation_logs
		WHERE (? = '' OR operation_type = ?)
		  AND (? = '' OR result = ?)
		ORDER BY created_at DESC
		LIMIT ?
	`, filter.OperationType, filter.OperationType, filter.Result, filter.Result, filter.Limit).Rows()
	if err != nil {
		return nil, fmt.Errorf("query operation logs: %w", err)
	}
	defer rows.Close()

	items := make([]domainoperation.Entry, 0, filter.Limit)
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
	if err := rows.Scan(
		&item.ID,
		&item.ActorID,
		&item.ActorName,
		&item.OperationType,
		&targetScope,
		&item.Result,
		&item.Summary,
		&item.RequestPath,
		&item.RequestMethod,
		&item.RequestID,
		&item.SourceIP,
		&metadata,
		&createdAt,
	); err != nil {
		return domainoperation.Entry{}, err
	}
	item.CreatedAt = createdAt
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
