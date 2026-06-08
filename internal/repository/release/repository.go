package release

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	domainrelease "github.com/opensoha/soha/internal/domain/release"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) List(ctx context.Context, filter domainrelease.Filter) ([]domainrelease.Record, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	query := `
		SELECT id, project_id, cluster_id, namespace, release_name, status, metadata, deployed_at, created_at
		FROM deploy_records
	`
	args := []any{}
	clauses := make([]string, 0, 2)
	if filter.ApplicationID != "" {
		clauses = append(clauses, "project_id = ?")
		args = append(args, filter.ApplicationID)
	}
	if filter.ClusterID != "" {
		clauses = append(clauses, "cluster_id = ?")
		args = append(args, filter.ClusterID)
	}
	if len(clauses) > 0 {
		query += " WHERE " + clauses[0]
		for i := 1; i < len(clauses); i++ {
			query += " AND " + clauses[i]
		}
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)

	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query release records: %w", err)
	}
	defer rows.Close()

	items := make([]domainrelease.Record, 0, limit)
	for rows.Next() {
		item, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) Create(ctx context.Context, record domainrelease.Record) (domainrelease.Record, error) {
	payload, err := json.Marshal(record.Metadata)
	if err != nil {
		return domainrelease.Record{}, fmt.Errorf("marshal release metadata: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO deploy_records (id, project_id, cluster_id, namespace, release_name, status, metadata, deployed_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, record.ID, record.ApplicationID, record.ClusterID, record.Namespace, record.DeploymentName, record.Status, string(payload), record.DeployedAt, record.CreatedAt).Error; err != nil {
		return domainrelease.Record{}, fmt.Errorf("create release record: %w", err)
	}
	return record, nil
}

func (r *Repository) GetByExecutionTaskID(ctx context.Context, executionTaskID string) (domainrelease.Record, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, project_id, cluster_id, namespace, release_name, status, metadata, deployed_at, created_at
		FROM deploy_records
		WHERE metadata ->> 'executionTaskId' = ?
		ORDER BY created_at DESC
		LIMIT 1
	`, executionTaskID).Row()
	return scanRecordRow(row)
}

func (r *Repository) Update(ctx context.Context, record domainrelease.Record) (domainrelease.Record, error) {
	payload, err := json.Marshal(record.Metadata)
	if err != nil {
		return domainrelease.Record{}, fmt.Errorf("marshal release metadata: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		UPDATE deploy_records
		SET status = ?, metadata = ?, deployed_at = ?
		WHERE id = ?
	`, record.Status, string(payload), record.DeployedAt, record.ID).Error; err != nil {
		return domainrelease.Record{}, fmt.Errorf("update release record: %w", err)
	}
	return record, nil
}

func (r *Repository) DeleteByIDs(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if err := r.db.WithContext(ctx).Exec(`DELETE FROM deploy_records WHERE id IN ?`, ids).Error; err != nil {
		return fmt.Errorf("delete release records: %w", err)
	}
	return nil
}

func scanRecord(rows *sql.Rows) (domainrelease.Record, error) {
	var item domainrelease.Record
	var payload []byte
	var deployedAt sql.NullTime
	if err := rows.Scan(&item.ID, &item.ApplicationID, &item.ClusterID, &item.Namespace, &item.DeploymentName, &item.Status, &payload, &deployedAt, &item.CreatedAt); err != nil {
		return domainrelease.Record{}, fmt.Errorf("scan release record: %w", err)
	}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &item.Metadata)
	}
	if deployedAt.Valid {
		value := deployedAt.Time
		item.DeployedAt = &value
	}
	return item, nil
}

func scanRecordRow(row *sql.Row) (domainrelease.Record, error) {
	var item domainrelease.Record
	var payload []byte
	var deployedAt sql.NullTime
	if err := row.Scan(&item.ID, &item.ApplicationID, &item.ClusterID, &item.Namespace, &item.DeploymentName, &item.Status, &payload, &deployedAt, &item.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return domainrelease.Record{}, fmt.Errorf("release record not found")
		}
		return domainrelease.Record{}, fmt.Errorf("scan release record row: %w", err)
	}
	if len(payload) > 0 {
		_ = json.Unmarshal(payload, &item.Metadata)
	}
	if deployedAt.Valid {
		value := deployedAt.Time
		item.DeployedAt = &value
	}
	return item, nil
}
