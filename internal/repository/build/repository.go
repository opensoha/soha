package build

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	domainbuild "github.com/soha/soha/internal/domain/build"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) List(ctx context.Context, filter domainbuild.Filter) ([]domainbuild.Record, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	query := `
		SELECT id, project_id, source_system, status, metadata, started_at, finished_at, created_at
		FROM build_records
	`
	args := []any{}
	if filter.ApplicationID != "" {
		query += ` WHERE project_id = ?`
		args = append(args, filter.ApplicationID)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query build records: %w", err)
	}
	defer rows.Close()

	items := make([]domainbuild.Record, 0, limit)
	for rows.Next() {
		item, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) Create(ctx context.Context, input domainbuild.TriggerInput, metadata map[string]any) (domainbuild.Record, error) {
	if metadata == nil {
		metadata = map[string]any{}
	}
	record := domainbuild.Record{
		ID:            fmt.Sprintf("build:%s:%d", input.ApplicationID, time.Now().UTC().UnixNano()),
		ApplicationID: input.ApplicationID,
		SourceSystem:  "manual",
		Status:        "queued",
		Metadata:      metadata,
		CreatedAt:     time.Now().UTC(),
	}
	payload, err := json.Marshal(record.Metadata)
	if err != nil {
		return domainbuild.Record{}, fmt.Errorf("marshal build metadata: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO build_records (id, project_id, source_system, status, metadata, started_at, finished_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, record.ID, record.ApplicationID, record.SourceSystem, record.Status, string(payload), nil, nil, record.CreatedAt, record.CreatedAt).Error; err != nil {
		return domainbuild.Record{}, fmt.Errorf("create build record: %w", err)
	}
	return record, nil
}

func (r *Repository) GetByExecutionTaskID(ctx context.Context, executionTaskID string) (domainbuild.Record, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, project_id, source_system, status, metadata, started_at, finished_at, created_at
		FROM build_records
		WHERE metadata ->> 'executionTaskId' = ?
		ORDER BY created_at DESC
		LIMIT 1
	`, executionTaskID).Row()
	return scanRecordRow(row)
}

func (r *Repository) Update(ctx context.Context, record domainbuild.Record) (domainbuild.Record, error) {
	payload, err := json.Marshal(record.Metadata)
	if err != nil {
		return domainbuild.Record{}, fmt.Errorf("marshal build metadata: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		UPDATE build_records
		SET status = ?, metadata = ?, started_at = ?, finished_at = ?, updated_at = ?
		WHERE id = ?
	`, record.Status, string(payload), record.StartedAt, record.FinishedAt, time.Now().UTC(), record.ID).Error; err != nil {
		return domainbuild.Record{}, fmt.Errorf("update build record: %w", err)
	}
	return record, nil
}

func scanRecord(rows *sql.Rows) (domainbuild.Record, error) {
	var item domainbuild.Record
	var payload []byte
	var startedAt sql.NullTime
	var finishedAt sql.NullTime
	if err := rows.Scan(&item.ID, &item.ApplicationID, &item.SourceSystem, &item.Status, &payload, &startedAt, &finishedAt, &item.CreatedAt); err != nil {
		return domainbuild.Record{}, fmt.Errorf("scan build record: %w", err)
	}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &item.Metadata)
	}
	if startedAt.Valid {
		value := startedAt.Time
		item.StartedAt = &value
	}
	if finishedAt.Valid {
		value := finishedAt.Time
		item.FinishedAt = &value
	}
	return item, nil
}

func scanRecordRow(row *sql.Row) (domainbuild.Record, error) {
	var item domainbuild.Record
	var payload []byte
	var startedAt sql.NullTime
	var finishedAt sql.NullTime
	if err := row.Scan(&item.ID, &item.ApplicationID, &item.SourceSystem, &item.Status, &payload, &startedAt, &finishedAt, &item.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return domainbuild.Record{}, fmt.Errorf("build record not found")
		}
		return domainbuild.Record{}, fmt.Errorf("scan build record row: %w", err)
	}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &item.Metadata)
	}
	if startedAt.Valid {
		value := startedAt.Time
		item.StartedAt = &value
	}
	if finishedAt.Valid {
		value := finishedAt.Time
		item.FinishedAt = &value
	}
	return item, nil
}
