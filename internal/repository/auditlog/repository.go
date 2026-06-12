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
	var fromArg any
	if filter.From != nil {
		fromArg = *filter.From
	}
	var toArg any
	if filter.To != nil {
		toArg = *filter.To
	}
	query := `
		SELECT id, actor_id, actor_name, roles, teams, cluster_id, namespace, resource_kind,
			resource_name, action, result, summary, request_path, request_method,
			request_id, source_ip, metadata, created_at
		FROM audit_logs
		WHERE (? = '' OR actor_id = ?)
		  AND (? = '' OR actor_name = ?)
		  AND (? = '' OR cluster_id = ?)
		  AND (? = '' OR namespace = ?)
		  AND (? = '' OR resource_kind = ?)
		  AND (? = '' OR resource_name = ?)
		  AND (? = '' OR action = ?)
		  AND (? = '' OR result = ?)
		  AND (? = '' OR request_id = ?)
		  AND (? = '' OR request_path = ?)
		  AND (? = '' OR request_method = ?)
		  AND (? = '' OR source_ip = ?)
		  AND (? = '' OR metadata::jsonb ->> 'approvalRequestId' = ? OR metadata::jsonb ->> 'approvalId' = ?)
		  AND (? = '' OR metadata::jsonb ->> 'agentRunId' = ? OR metadata::jsonb ->> 'runId' = ? OR metadata::jsonb ->> 'externalRunId' = ?)
		  AND (? = '' OR metadata::jsonb ->> 'rootCauseRunId' = ? OR metadata::jsonb ->> 'rootCauseId' = ?)
		  AND (? = '' OR metadata::jsonb ->> ? = ?)
		  AND (? IS NULL OR created_at >= ?)
		  AND (? IS NULL OR created_at <= ?)
		ORDER BY created_at DESC
		LIMIT ?
	`
	rows, err := r.db.WithContext(ctx).Raw(
		query,
		filter.ActorID,
		filter.ActorID,
		filter.ActorName,
		filter.ActorName,
		filter.ClusterID,
		filter.ClusterID,
		filter.Namespace,
		filter.Namespace,
		filter.ResourceKind,
		filter.ResourceKind,
		filter.ResourceName,
		filter.ResourceName,
		filter.Action,
		filter.Action,
		filter.Result,
		filter.Result,
		filter.RequestID,
		filter.RequestID,
		filter.RequestPath,
		filter.RequestPath,
		filter.RequestMethod,
		filter.RequestMethod,
		filter.SourceIP,
		filter.SourceIP,
		filter.ApprovalRequestID,
		filter.ApprovalRequestID,
		filter.ApprovalRequestID,
		filter.AgentRunID,
		filter.AgentRunID,
		filter.AgentRunID,
		filter.AgentRunID,
		filter.RootCauseRunID,
		filter.RootCauseRunID,
		filter.RootCauseRunID,
		filter.MetadataKey,
		filter.MetadataKey,
		filter.MetadataValue,
		fromArg,
		fromArg,
		toArg,
		toArg,
		filter.Limit,
	).Rows()
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

func (r *Repository) Summary(ctx context.Context, filter domainaudit.Filter, retentionDays int) (domainaudit.Summary, error) {
	if retentionDays <= 0 {
		retentionDays = 90
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -retentionDays)
	var fromArg any
	if filter.From != nil {
		fromArg = *filter.From
	}
	var toArg any
	if filter.To != nil {
		toArg = *filter.To
	}
	row := r.db.WithContext(ctx).Raw(`
		SELECT COUNT(*), MIN(created_at), MAX(created_at),
			COUNT(*) FILTER (WHERE created_at < ?)
		FROM audit_logs
		WHERE (? = '' OR actor_id = ?)
		  AND (? = '' OR actor_name = ?)
		  AND (? = '' OR cluster_id = ?)
		  AND (? = '' OR namespace = ?)
		  AND (? = '' OR resource_kind = ?)
		  AND (? = '' OR resource_name = ?)
		  AND (? = '' OR action = ?)
		  AND (? = '' OR result = ?)
		  AND (? = '' OR request_id = ?)
		  AND (? = '' OR request_path = ?)
		  AND (? = '' OR request_method = ?)
		  AND (? = '' OR source_ip = ?)
		  AND (? = '' OR metadata::jsonb ->> 'approvalRequestId' = ? OR metadata::jsonb ->> 'approvalId' = ?)
		  AND (? = '' OR metadata::jsonb ->> 'agentRunId' = ? OR metadata::jsonb ->> 'runId' = ? OR metadata::jsonb ->> 'externalRunId' = ?)
		  AND (? = '' OR metadata::jsonb ->> 'rootCauseRunId' = ? OR metadata::jsonb ->> 'rootCauseId' = ?)
		  AND (? = '' OR metadata::jsonb ->> ? = ?)
		  AND (? IS NULL OR created_at >= ?)
		  AND (? IS NULL OR created_at <= ?)
	`,
		cutoff,
		filter.ActorID,
		filter.ActorID,
		filter.ActorName,
		filter.ActorName,
		filter.ClusterID,
		filter.ClusterID,
		filter.Namespace,
		filter.Namespace,
		filter.ResourceKind,
		filter.ResourceKind,
		filter.ResourceName,
		filter.ResourceName,
		filter.Action,
		filter.Action,
		filter.Result,
		filter.Result,
		filter.RequestID,
		filter.RequestID,
		filter.RequestPath,
		filter.RequestPath,
		filter.RequestMethod,
		filter.RequestMethod,
		filter.SourceIP,
		filter.SourceIP,
		filter.ApprovalRequestID,
		filter.ApprovalRequestID,
		filter.ApprovalRequestID,
		filter.AgentRunID,
		filter.AgentRunID,
		filter.AgentRunID,
		filter.AgentRunID,
		filter.RootCauseRunID,
		filter.RootCauseRunID,
		filter.RootCauseRunID,
		filter.MetadataKey,
		filter.MetadataKey,
		filter.MetadataValue,
		fromArg,
		fromArg,
		toArg,
		toArg,
	).Row()
	var summary domainaudit.Summary
	var oldest sql.NullTime
	var newest sql.NullTime
	if err := row.Scan(&summary.Total, &oldest, &newest, &summary.ExpiredEntryCount); err != nil {
		return domainaudit.Summary{}, fmt.Errorf("summarize audit logs: %w", err)
	}
	summary.RetentionDays = retentionDays
	summary.RetentionCutoff = &cutoff
	if oldest.Valid {
		value := oldest.Time.UTC()
		summary.OldestEntryAt = &value
	}
	if newest.Valid {
		value := newest.Time.UTC()
		summary.NewestEntryAt = &value
	}
	summary.ExportRecommended = summary.ExpiredEntryCount > 0
	if summary.ExpiredEntryCount > 0 {
		summary.RecommendedNextAction = "export_then_purge_expired_audit_logs"
	} else {
		summary.RecommendedNextAction = "monitor_retention_window"
	}
	return summary, nil
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
