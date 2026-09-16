package workflow

import (
	"context"
	"fmt"
	"strings"

	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

// ListExecutionHistoryCandidates reads only positions. The application service
// authorizes and filters projections before selecting a user-visible page.
func (r *Repository) ListExecutionHistoryCandidates(ctx context.Context, f domainworkflow.ExecutionHistoryFilter, after *domainworkflow.ExecutionHistoryPosition, limit int) ([]domainworkflow.ExecutionHistoryPosition, error) {
	parts, args := []string{}, []any{}
	if f.IncludeWorkflows {
		query, values := executionBatchCandidates(f)
		parts, args = append(parts, query), append(args, values...)
		if f.WorkflowID == "" {
			query, values = executionWorkflowCandidates(f)
			parts, args = append(parts, query), append(args, values...)
		}
	}
	if f.IncludeBuilds && f.WorkflowID == "" {
		query, values := executionBuildCandidates(f)
		parts, args = append(parts, query), append(args, values...)
	}
	if len(parts) == 0 {
		return []domainworkflow.ExecutionHistoryPosition{}, nil
	}
	query := "SELECT kind, id, created_at FROM (" + strings.Join(parts, " UNION ALL ") + ") history"
	if after != nil {
		query += " WHERE (created_at, kind, id) < (?, ?, ?)"
		args = append(args, after.CreatedAt, after.Kind, after.ID)
	}
	query += " ORDER BY created_at DESC, kind DESC, id DESC LIMIT ?"
	args = append(args, limit)
	var items []domainworkflow.ExecutionHistoryPosition
	if err := r.db.WithContext(ctx).Raw(query, args...).Scan(&items).Error; err != nil {
		return nil, fmt.Errorf("query execution history: %w", err)
	}
	return items, nil
}

func executionBatchCandidates(f domainworkflow.ExecutionHistoryFilter) (string, []any) {
	query := "SELECT 'batch' AS kind, id, created_at FROM delivery_batches WHERE TRUE"
	args := []any{}
	if f.WorkflowID != "" {
		query += " AND snapshot->>'workflowId' = ?"
		args = append(args, f.WorkflowID)
	}
	conditions := []string{}
	for _, field := range []struct{ path, value string }{
		{"t->'target'->>'applicationId'", f.ApplicationID},
		{"t->'target'->>'serviceId'", f.ServiceID},
		{"t->'target'->>'applicationEnvironmentId'", f.ApplicationEnvironmentID},
		{"t->>'buildSourceId'", f.BuildSourceID},
	} {
		if field.value != "" {
			conditions = append(conditions, field.path+" = ?")
			args = append(args, field.value)
		}
	}
	if len(conditions) > 0 {
		query += " AND EXISTS (SELECT 1 FROM jsonb_array_elements(snapshot::jsonb->'targets') t WHERE " + strings.Join(conditions, " AND ") + ")"
	}
	return query, args
}

func executionWorkflowCandidates(f domainworkflow.ExecutionHistoryFilter) (string, []any) {
	query := "SELECT 'application' AS kind, w.id, w.created_at FROM workflow_runs w WHERE w.scope = 'application' AND NOT EXISTS (SELECT 1 FROM delivery_batches d WHERE d.root_run_id = w.id)"
	args := []any{}
	for _, field := range []struct{ path, value string }{
		{"w.application_id", f.ApplicationID}, {"w.metadata->>'serviceId'", f.ServiceID},
		{"w.metadata->>'bindingId'", f.ApplicationEnvironmentID}, {"w.metadata->>'buildSourceId'", f.BuildSourceID},
	} {
		if field.value != "" {
			query += " AND " + field.path + " = ?"
			args = append(args, field.value)
		}
	}
	return query, args
}

func executionBuildCandidates(f domainworkflow.ExecutionHistoryFilter) (string, []any) {
	query := "SELECT 'build' AS kind, b.id, b.created_at FROM build_records b WHERE TRUE"
	if f.IncludeWorkflows {
		query += ` AND NOT EXISTS (
			SELECT 1 FROM workflow_runs w WHERE
			(w.id = b.metadata->>'workflowRunId' OR EXISTS (
				SELECT 1 FROM jsonb_array_elements(COALESCE(NULLIF(w.metadata::jsonb->'nodeRuns', 'null'::jsonb), '[]'::jsonb)) n
				WHERE n->>'buildRecordId' = b.id AND (n->>'stage' = 'build' OR n->>'type' = 'build')
			)) AND (
				(w.scope = 'application' AND w.application_id = b.project_id)
				OR EXISTS (SELECT 1 FROM delivery_batches d,
					jsonb_array_elements(d.snapshot::jsonb->'targets') t
					WHERE d.root_run_id = w.id AND t->'target'->>'applicationId' = b.project_id
					AND (t->>'buildNodeId' = b.metadata->>'workflowNodeId' OR EXISTS (
						SELECT 1 FROM jsonb_array_elements(COALESCE(NULLIF(w.metadata::jsonb->'nodeRuns', 'null'::jsonb), '[]'::jsonb)) n
						WHERE n->>'buildRecordId' = b.id AND n->>'nodeId' = t->>'buildNodeId'
						AND (n->>'stage' = 'build' OR n->>'type' = 'build')
					)))
			)
		)`
	}
	args := []any{}
	for _, field := range []struct{ path, value string }{
		{"b.project_id", f.ApplicationID}, {"b.metadata->>'serviceId'", f.ServiceID},
		{"b.metadata->>'applicationEnvironmentId'", f.ApplicationEnvironmentID}, {"COALESCE(NULLIF(b.metadata->>'buildSourceId', ''), 'default:' || b.project_id)", f.BuildSourceID},
	} {
		if field.value != "" {
			query += " AND " + field.path + " = ?"
			args = append(args, field.value)
		}
	}
	return query, args
}
