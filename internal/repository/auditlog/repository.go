package auditlog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Create(ctx context.Context, entry domainaudit.Entry) error {
	roles, err := json.Marshal(entry.Roles)
	if err != nil {
		return fmt.Errorf("marshal audit roles: %w", err)
	}
	teams, err := json.Marshal(entry.Teams)
	if err != nil {
		return fmt.Errorf("marshal audit teams: %w", err)
	}
	metadata, err := json.Marshal(entry.Metadata)
	if err != nil {
		return fmt.Errorf("marshal audit metadata: %w", err)
	}
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO audit_logs (
			id, actor_id, actor_name, roles, teams, cluster_id, namespace, resource_kind,
			resource_name, action, result, summary, request_path, request_method,
			request_id, source_ip, metadata, created_at
		) VALUES (
			?, ?, ?, ?, ?, ?, ?, ?,
			?, ?, ?, ?, ?, ?,
			?, ?, ?, ?
		)
	`,
		entry.ID,
		entry.ActorID,
		entry.ActorName,
		string(roles),
		string(teams),
		entry.ClusterID,
		entry.Namespace,
		entry.ResourceKind,
		entry.ResourceName,
		entry.Action,
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

func (r *Repository) List(ctx context.Context, filter domainaudit.Filter) ([]domainaudit.Entry, error) {
	if filter.Limit <= 0 {
		filter.Limit = 50
	}
	query := `
		SELECT id, actor_id, actor_name, roles, teams, cluster_id, namespace, resource_kind,
			resource_name, action, result, summary, request_path, request_method,
			request_id, source_ip, metadata, created_at
		FROM audit_logs
		WHERE (? = '' OR action = ?)
		  AND (? = '' OR result = ?)
		ORDER BY created_at DESC
		LIMIT ?
	`
	rows, err := r.db.WithContext(ctx).Raw(query, filter.Action, filter.Action, filter.Result, filter.Result, filter.Limit).Rows()
	if err != nil {
		return nil, fmt.Errorf("query audit logs: %w", err)
	}
	defer rows.Close()

	entries := make([]domainaudit.Entry, 0, filter.Limit)
	for rows.Next() {
		entry, err := scanEntry(rows)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func scanEntry(rows *sql.Rows) (domainaudit.Entry, error) {
	var entry domainaudit.Entry
	var roles []byte
	var teams []byte
	var metadata []byte
	var createdAt time.Time
	if err := rows.Scan(
		&entry.ID,
		&entry.ActorID,
		&entry.ActorName,
		&roles,
		&teams,
		&entry.ClusterID,
		&entry.Namespace,
		&entry.ResourceKind,
		&entry.ResourceName,
		&entry.Action,
		&entry.Result,
		&entry.Summary,
		&entry.RequestPath,
		&entry.RequestMethod,
		&entry.RequestID,
		&entry.SourceIP,
		&metadata,
		&createdAt,
	); err != nil {
		return domainaudit.Entry{}, fmt.Errorf("scan audit log: %w", err)
	}
	if len(roles) > 0 {
		_ = json.Unmarshal(roles, &entry.Roles)
	}
	if len(teams) > 0 {
		_ = json.Unmarshal(teams, &entry.Teams)
	}
	entry.CreatedAt = createdAt
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &entry.Metadata)
	}
	return entry, nil
}
