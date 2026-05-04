package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domainworkflow "github.com/kubecrux/kubecrux/internal/domain/workflow"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) List(ctx context.Context, applicationID string, limit int) ([]domainworkflow.Run, error) {
	if limit <= 0 {
		limit = 50
	}
	query := `
		SELECT id, application_id, workflow_name, cluster_id, namespace, deployment_name, status, steps, metadata, created_at, updated_at
		FROM workflow_runs
	`
	args := []any{}
	if applicationID != "" {
		query += ` WHERE application_id = ?`
		args = append(args, applicationID)
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query workflow runs: %w", err)
	}
	defer rows.Close()
	items := make([]domainworkflow.Run, 0, limit)
	for rows.Next() {
		item, err := scanWorkflow(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) Get(ctx context.Context, runID string) (domainworkflow.Run, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, application_id, workflow_name, cluster_id, namespace, deployment_name, status, steps, metadata, created_at, updated_at
		FROM workflow_runs
		WHERE id = ?
		LIMIT 1
	`, runID).Row()
	return scanWorkflowRow(row)
}

func (r *Repository) Create(ctx context.Context, item domainworkflow.Run) (domainworkflow.Run, error) {
	item.Metadata = persistWorkflowMetadata(item.Metadata, item.NodeRuns)
	steps, err := json.Marshal(item.Steps)
	if err != nil {
		return domainworkflow.Run{}, fmt.Errorf("marshal workflow steps: %w", err)
	}
	metadata, err := json.Marshal(item.Metadata)
	if err != nil {
		return domainworkflow.Run{}, fmt.Errorf("marshal workflow metadata: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO workflow_runs (id, application_id, workflow_name, cluster_id, namespace, deployment_name, status, steps, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.ApplicationID, item.WorkflowName, nullable(item.ClusterID), nullable(item.Namespace), nullable(item.DeploymentName), item.Status, string(steps), string(metadata), parseTime(item.CreatedAt), parseTime(item.UpdatedAt)).Error; err != nil {
		return domainworkflow.Run{}, fmt.Errorf("create workflow run: %w", err)
	}
	return item, nil
}

func (r *Repository) Update(ctx context.Context, item domainworkflow.Run) (domainworkflow.Run, error) {
	item.Metadata = persistWorkflowMetadata(item.Metadata, item.NodeRuns)
	steps, err := json.Marshal(item.Steps)
	if err != nil {
		return domainworkflow.Run{}, fmt.Errorf("marshal workflow steps: %w", err)
	}
	metadata, err := json.Marshal(item.Metadata)
	if err != nil {
		return domainworkflow.Run{}, fmt.Errorf("marshal workflow metadata: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		UPDATE workflow_runs
		SET status = ?, steps = ?, metadata = ?, updated_at = ?
		WHERE id = ?
	`, item.Status, string(steps), string(metadata), parseTime(item.UpdatedAt), item.ID).Error; err != nil {
		return domainworkflow.Run{}, fmt.Errorf("update workflow run: %w", err)
	}
	return item, nil
}

func (r *Repository) CreateApproval(ctx context.Context, item domainworkflow.Approval) error {
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO workflow_approvals (id, workflow_run_id, node_id, action, comment, actor_id, actor_name, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.WorkflowRunID, item.NodeID, item.Action, nullable(item.Comment), item.ActorID, nullable(item.ActorName), item.CreatedAt).Error; err != nil {
		return fmt.Errorf("create workflow approval: %w", err)
	}
	return nil
}

func (r *Repository) DeleteByIDs(ctx context.Context, ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	if err := r.db.WithContext(ctx).Exec(`DELETE FROM workflow_runs WHERE id IN ?`, ids).Error; err != nil {
		return fmt.Errorf("delete workflow runs: %w", err)
	}
	return nil
}

func scanWorkflow(rows *sql.Rows) (domainworkflow.Run, error) {
	var item domainworkflow.Run
	var clusterID sql.NullString
	var namespace sql.NullString
	var deploymentName sql.NullString
	var steps []byte
	var metadata []byte
	var createdAt time.Time
	var updatedAt time.Time
	if err := rows.Scan(&item.ID, &item.ApplicationID, &item.WorkflowName, &clusterID, &namespace, &deploymentName, &item.Status, &steps, &metadata, &createdAt, &updatedAt); err != nil {
		return domainworkflow.Run{}, fmt.Errorf("scan workflow run: %w", err)
	}
	if clusterID.Valid {
		item.ClusterID = clusterID.String
	}
	if namespace.Valid {
		item.Namespace = namespace.String
	}
	if deploymentName.Valid {
		item.DeploymentName = deploymentName.String
	}
	if len(steps) > 0 {
		_ = json.Unmarshal(steps, &item.Steps)
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &item.Metadata)
	}
	item.NodeRuns = extractWorkflowNodeRuns(item.Metadata)
	item.CreatedAt = createdAt.Format(time.RFC3339)
	item.UpdatedAt = updatedAt.Format(time.RFC3339)
	return item, nil
}

func scanWorkflowRow(row *sql.Row) (domainworkflow.Run, error) {
	var item domainworkflow.Run
	var clusterID sql.NullString
	var namespace sql.NullString
	var deploymentName sql.NullString
	var steps []byte
	var metadata []byte
	var createdAt time.Time
	var updatedAt time.Time
	if err := row.Scan(&item.ID, &item.ApplicationID, &item.WorkflowName, &clusterID, &namespace, &deploymentName, &item.Status, &steps, &metadata, &createdAt, &updatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainworkflow.Run{}, fmt.Errorf("workflow run not found")
		}
		return domainworkflow.Run{}, fmt.Errorf("scan workflow run row: %w", err)
	}
	if clusterID.Valid {
		item.ClusterID = clusterID.String
	}
	if namespace.Valid {
		item.Namespace = namespace.String
	}
	if deploymentName.Valid {
		item.DeploymentName = deploymentName.String
	}
	if len(steps) > 0 {
		_ = json.Unmarshal(steps, &item.Steps)
	}
	if len(metadata) > 0 {
		_ = json.Unmarshal(metadata, &item.Metadata)
	}
	item.NodeRuns = extractWorkflowNodeRuns(item.Metadata)
	item.CreatedAt = createdAt.Format(time.RFC3339)
	item.UpdatedAt = updatedAt.Format(time.RFC3339)
	return item, nil
}

func persistWorkflowMetadata(metadata map[string]any, nodeRuns []domainworkflow.NodeRun) map[string]any {
	next := make(map[string]any, len(metadata)+2)
	for key, value := range metadata {
		next[key] = value
	}
	if len(nodeRuns) > 0 {
		next["nodeRuns"] = nodeRuns
		statuses := make(map[string]string, len(nodeRuns))
		for _, item := range nodeRuns {
			statuses[item.NodeID] = item.Status
		}
		next["nodeStatus"] = statuses
	}
	return next
}

func extractWorkflowNodeRuns(metadata map[string]any) []domainworkflow.NodeRun {
	if len(metadata) == 0 {
		return nil
	}
	raw, ok := metadata["nodeRuns"]
	if !ok || raw == nil {
		return nil
	}
	switch value := raw.(type) {
	case []domainworkflow.NodeRun:
		return append([]domainworkflow.NodeRun(nil), value...)
	case []any:
		items := make([]domainworkflow.NodeRun, 0, len(value))
		for _, entry := range value {
			valueMap, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			items = append(items, domainworkflow.NodeRun{
				NodeID:     stringValue(valueMap["nodeId"]),
				Name:       stringValue(valueMap["name"]),
				Type:       stringValue(valueMap["type"]),
				Status:     stringValue(valueMap["status"]),
				Summary:    stringValue(valueMap["summary"]),
				StartedAt:  stringValue(valueMap["startedAt"]),
				FinishedAt: stringValue(valueMap["finishedAt"]),
			})
		}
		return items
	default:
		return nil
	}
}

func stringValue(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}

func nullable(value string) any {
	if value == "" {
		return nil
	}
	return value
}

func parseTime(value string) time.Time {
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed
	}
	return time.Now().UTC()
}
