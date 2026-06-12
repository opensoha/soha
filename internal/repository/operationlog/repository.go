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
	var fromArg any
	if filter.From != nil {
		fromArg = *filter.From
	}
	var toArg any
	if filter.To != nil {
		toArg = *filter.To
	}
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, actor_id, actor_name, operation_type, target_scope, result, summary, request_path,
		       request_method, request_id, source_ip, metadata, created_at
		FROM operation_logs
		WHERE (? = '' OR actor_id = ?)
		  AND (? = '' OR operation_type = ?)
		  AND (? = '' OR target_scope::jsonb ->> 'clusterId' = ? OR target_scope::jsonb ->> 'clusterID' = ? OR target_scope::jsonb ->> 'cluster' = ?)
		  AND (? = '' OR target_scope::jsonb ->> 'namespace' = ?)
		  AND (? = '' OR target_scope::jsonb ->> 'resourceKind' = ? OR target_scope::jsonb ->> 'kind' = ?)
		  AND (? = '' OR target_scope::jsonb ->> 'resourceName' = ? OR target_scope::jsonb ->> 'name' = ?)
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
	`,
		filter.ActorID,
		filter.ActorID,
		filter.OperationType,
		filter.OperationType,
		filter.ClusterID,
		filter.ClusterID,
		filter.ClusterID,
		filter.ClusterID,
		filter.Namespace,
		filter.Namespace,
		filter.ResourceKind,
		filter.ResourceKind,
		filter.ResourceKind,
		filter.ResourceName,
		filter.ResourceName,
		filter.ResourceName,
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

func (r *Repository) Summary(ctx context.Context, filter domainoperation.Filter, retentionDays int) (domainoperation.Summary, error) {
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
			COUNT(*) FILTER (WHERE created_at < ?),
			COUNT(*) FILTER (WHERE result = 'failure')
		FROM operation_logs
		WHERE (? = '' OR actor_id = ?)
		  AND (? = '' OR operation_type = ?)
		  AND (? = '' OR target_scope::jsonb ->> 'clusterId' = ? OR target_scope::jsonb ->> 'clusterID' = ? OR target_scope::jsonb ->> 'cluster' = ?)
		  AND (? = '' OR target_scope::jsonb ->> 'namespace' = ?)
		  AND (? = '' OR target_scope::jsonb ->> 'resourceKind' = ? OR target_scope::jsonb ->> 'kind' = ?)
		  AND (? = '' OR target_scope::jsonb ->> 'resourceName' = ? OR target_scope::jsonb ->> 'name' = ?)
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
		filter.OperationType,
		filter.OperationType,
		filter.ClusterID,
		filter.ClusterID,
		filter.ClusterID,
		filter.ClusterID,
		filter.Namespace,
		filter.Namespace,
		filter.ResourceKind,
		filter.ResourceKind,
		filter.ResourceKind,
		filter.ResourceName,
		filter.ResourceName,
		filter.ResourceName,
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
	var summary domainoperation.Summary
	var oldest sql.NullTime
	var newest sql.NullTime
	if err := row.Scan(&summary.Total, &oldest, &newest, &summary.ExpiredEntryCount, &summary.FailureCount); err != nil {
		return domainoperation.Summary{}, fmt.Errorf("summarize operation logs: %w", err)
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
	summary.ExportRecommended = summary.ExpiredEntryCount > 0 || summary.FailureCount > 0
	if summary.ExpiredEntryCount > 0 {
		summary.RecommendedNextAction = "export_then_purge_expired_operation_logs"
	} else if summary.FailureCount > 0 {
		summary.RecommendedNextAction = "inspect_failed_operations"
	} else {
		summary.RecommendedNextAction = "monitor_operation_window"
	}
	return summary, nil
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
