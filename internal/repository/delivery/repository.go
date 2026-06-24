package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

var ErrNotFound = fmt.Errorf("%w: delivery record not found", apperrors.ErrNotFound)

const artifactSelectColumns = `
	SELECT id, execution_task_id, release_bundle_id, workflow_run_id, workflow_node_id, application_id, application_environment_id, artifact_kind, name, ref, digest, path, status, size_bytes, metadata, retention_until, created_at, updated_at
	FROM execution_artifacts
`

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListReleaseBundles(ctx context.Context, filter domaindelivery.ReleaseBundleFilter) ([]domaindelivery.ReleaseBundle, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	query := `
		SELECT id, application_id, application_environment_id, version, source_type, status, artifact_ref, artifact_digest, metadata, created_at, updated_at
		FROM release_bundles
	`
	args := []any{}
	clauses := make([]string, 0, 2)
	if value := strings.TrimSpace(filter.ApplicationID); value != "" {
		clauses = append(clauses, "application_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.ApplicationEnvironmentID); value != "" {
		clauses = append(clauses, "application_environment_id = ?")
		args = append(args, value)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query release bundles: %w", err)
	}
	defer rows.Close()

	items := make([]domaindelivery.ReleaseBundle, 0, limit)
	for rows.Next() {
		item, scanErr := scanReleaseBundle(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetReleaseBundle(ctx context.Context, id string) (domaindelivery.ReleaseBundle, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, application_id, application_environment_id, version, source_type, status, artifact_ref, artifact_digest, metadata, created_at, updated_at
		FROM release_bundles
		WHERE id = ?
		LIMIT 1
	`, strings.TrimSpace(id)).Row()
	return scanReleaseBundleRow(row)
}

func (r *Repository) CreateReleaseBundle(ctx context.Context, item domaindelivery.ReleaseBundle) (domaindelivery.ReleaseBundle, error) {
	payload, err := json.Marshal(item.Metadata)
	if err != nil {
		return domaindelivery.ReleaseBundle{}, fmt.Errorf("marshal release bundle metadata: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO release_bundles (id, application_id, application_environment_id, version, source_type, status, artifact_ref, artifact_digest, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.ApplicationID, nullableString(item.ApplicationEnvironmentID), item.Version, item.SourceType, item.Status, nullableString(item.ArtifactRef), nullableString(item.ArtifactDigest), string(payload), item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domaindelivery.ReleaseBundle{}, fmt.Errorf("create release bundle: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateReleaseBundle(ctx context.Context, item domaindelivery.ReleaseBundle) (domaindelivery.ReleaseBundle, error) {
	payload, err := json.Marshal(item.Metadata)
	if err != nil {
		return domaindelivery.ReleaseBundle{}, fmt.Errorf("marshal release bundle metadata: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE release_bundles
		SET status = ?, artifact_ref = ?, artifact_digest = ?, metadata = ?, updated_at = ?
		WHERE id = ?
	`, item.Status, nullableString(item.ArtifactRef), nullableString(item.ArtifactDigest), string(payload), item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domaindelivery.ReleaseBundle{}, fmt.Errorf("update release bundle: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domaindelivery.ReleaseBundle{}, ErrNotFound
	}
	return item, nil
}

func (r *Repository) ListExecutionTasks(ctx context.Context, filter domaindelivery.ExecutionTaskFilter) ([]domaindelivery.ExecutionTask, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	query := `
		SELECT id, release_bundle_id, application_id, application_environment_id, task_kind, provider_kind, target_kind, status, queue_key, lock_key,
		       max_retries, attempt_count, timeout_seconds, callback_token, claimed_by_agent_id, runtime_endpoint, runtime_cluster_id, stop_transport, payload, result, started_at, last_heartbeat_at, last_runtime_seen_at, finished_at, created_at, updated_at
		FROM execution_tasks
	`
	args := []any{}
	clauses := make([]string, 0, 5)
	if value := strings.TrimSpace(filter.ApplicationID); value != "" {
		clauses = append(clauses, "application_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.ApplicationEnvironmentID); value != "" {
		clauses = append(clauses, "application_environment_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.ReleaseBundleID); value != "" {
		clauses = append(clauses, "release_bundle_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Status); value != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.ProviderKind); value != "" {
		clauses = append(clauses, "provider_kind = ?")
		args = append(args, value)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += ` ORDER BY created_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query execution tasks: %w", err)
	}
	defer rows.Close()

	items := make([]domaindelivery.ExecutionTask, 0, limit)
	for rows.Next() {
		item, scanErr := scanExecutionTask(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetExecutionTask(ctx context.Context, id string) (domaindelivery.ExecutionTask, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, release_bundle_id, application_id, application_environment_id, task_kind, provider_kind, target_kind, status, queue_key, lock_key,
		       max_retries, attempt_count, timeout_seconds, callback_token, claimed_by_agent_id, runtime_endpoint, runtime_cluster_id, stop_transport, payload, result, started_at, last_heartbeat_at, last_runtime_seen_at, finished_at, created_at, updated_at
		FROM execution_tasks
		WHERE id = ?
		LIMIT 1
	`, strings.TrimSpace(id)).Row()
	return scanExecutionTaskRow(row)
}

func (r *Repository) GetExecutionTaskByCallbackToken(ctx context.Context, token string) (domaindelivery.ExecutionTask, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, release_bundle_id, application_id, application_environment_id, task_kind, provider_kind, target_kind, status, queue_key, lock_key,
		       max_retries, attempt_count, timeout_seconds, callback_token, claimed_by_agent_id, runtime_endpoint, runtime_cluster_id, stop_transport, payload, result, started_at, last_heartbeat_at, last_runtime_seen_at, finished_at, created_at, updated_at
		FROM execution_tasks
		WHERE callback_token = ?
		LIMIT 1
	`, strings.TrimSpace(token)).Row()
	return scanExecutionTaskRow(row)
}

func (r *Repository) ClaimExecutionTask(ctx context.Context, providerKinds []string, agentID, runtimeEndpoint string) (domaindelivery.ExecutionTask, error) {
	if len(providerKinds) == 0 {
		return domaindelivery.ExecutionTask{}, ErrNotFound
	}

	var task domaindelivery.ExecutionTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rows, queryErr := tx.Raw(`
			SELECT id, release_bundle_id, application_id, application_environment_id, task_kind, provider_kind, target_kind, status, queue_key, lock_key,
			       max_retries, attempt_count, timeout_seconds, callback_token, claimed_by_agent_id, runtime_endpoint, runtime_cluster_id, stop_transport, payload, result, started_at, last_heartbeat_at, last_runtime_seen_at, finished_at, created_at, updated_at
			FROM execution_tasks
			WHERE status = 'queued' AND provider_kind IN ?
			ORDER BY created_at ASC
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		`, providerKinds).Rows()
		if queryErr != nil {
			return fmt.Errorf("claim execution task query: %w", queryErr)
		}
		defer rows.Close()
		if !rows.Next() {
			return ErrNotFound
		}
		item, scanErr := scanExecutionTask(rows)
		if scanErr != nil {
			return scanErr
		}
		now := time.Now().UTC()
		item.Status = "dispatching"
		item.AttemptCount++
		item.StartedAt = &now
		item.LastHeartbeatAt = &now
		item.LastRuntimeSeenAt = &now
		item.ClaimedByAgentID = strings.TrimSpace(agentID)
		item.RuntimeEndpoint = strings.TrimSpace(runtimeEndpoint)
		item.UpdatedAt = now
		item.Result = mergeMaps(item.Result, map[string]any{
			"claimedByAgent":  strings.TrimSpace(agentID),
			"claimedAt":       now.Format(time.RFC3339),
			"runtimeEndpoint": strings.TrimSpace(runtimeEndpoint),
		})
		payload, marshalErr := json.Marshal(item.Payload)
		if marshalErr != nil {
			return fmt.Errorf("marshal claimed execution task payload: %w", marshalErr)
		}
		result, marshalErr := json.Marshal(item.Result)
		if marshalErr != nil {
			return fmt.Errorf("marshal claimed execution task result: %w", marshalErr)
		}
		update := tx.Exec(`
			UPDATE execution_tasks
			SET status = ?, attempt_count = ?, claimed_by_agent_id = ?, runtime_endpoint = ?, last_runtime_seen_at = ?, payload = ?, result = ?, started_at = ?, last_heartbeat_at = ?, updated_at = ?
			WHERE id = ?
		`, item.Status, item.AttemptCount, nullableString(item.ClaimedByAgentID), nullableString(item.RuntimeEndpoint), item.LastRuntimeSeenAt, string(payload), string(result), item.StartedAt, item.LastHeartbeatAt, item.UpdatedAt, item.ID)
		if update.Error != nil {
			return fmt.Errorf("claim execution task update: %w", update.Error)
		}
		task = item
		return nil
	})
	if errors.Is(err, ErrNotFound) {
		return domaindelivery.ExecutionTask{}, ErrNotFound
	}
	if err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	return task, nil
}

func (r *Repository) CreateExecutionTask(ctx context.Context, item domaindelivery.ExecutionTask) (domaindelivery.ExecutionTask, error) {
	payload, err := json.Marshal(item.Payload)
	if err != nil {
		return domaindelivery.ExecutionTask{}, fmt.Errorf("marshal execution task payload: %w", err)
	}
	result, err := json.Marshal(item.Result)
	if err != nil {
		return domaindelivery.ExecutionTask{}, fmt.Errorf("marshal execution task result: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO execution_tasks (id, release_bundle_id, application_id, application_environment_id, task_kind, provider_kind, target_kind, status, queue_key, lock_key, max_retries, attempt_count, timeout_seconds, callback_token, claimed_by_agent_id, runtime_endpoint, runtime_cluster_id, stop_transport, payload, result, started_at, last_heartbeat_at, last_runtime_seen_at, finished_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, nullableString(item.ReleaseBundleID), item.ApplicationID, nullableString(item.ApplicationEnvironmentID), item.TaskKind, item.ProviderKind, item.TargetKind, item.Status, nullableString(item.QueueKey), nullableString(item.LockKey), item.MaxRetries, item.AttemptCount, item.TimeoutSeconds, nullableString(item.CallbackToken), nullableString(item.ClaimedByAgentID), nullableString(item.RuntimeEndpoint), nullableString(item.RuntimeClusterID), nullableString(item.StopTransport), string(payload), string(result), item.StartedAt, item.LastHeartbeatAt, item.LastRuntimeSeenAt, item.FinishedAt, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domaindelivery.ExecutionTask{}, fmt.Errorf("create execution task: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateExecutionTask(ctx context.Context, item domaindelivery.ExecutionTask) (domaindelivery.ExecutionTask, error) {
	payload, err := json.Marshal(item.Payload)
	if err != nil {
		return domaindelivery.ExecutionTask{}, fmt.Errorf("marshal execution task payload: %w", err)
	}
	result, err := json.Marshal(item.Result)
	if err != nil {
		return domaindelivery.ExecutionTask{}, fmt.Errorf("marshal execution task result: %w", err)
	}
	res := r.db.WithContext(ctx).Exec(`
		UPDATE execution_tasks
		SET status = ?, max_retries = ?, attempt_count = ?, timeout_seconds = ?, callback_token = ?, claimed_by_agent_id = ?, runtime_endpoint = ?, runtime_cluster_id = ?, stop_transport = ?, payload = ?, result = ?, started_at = ?, last_heartbeat_at = ?, last_runtime_seen_at = ?, finished_at = ?, updated_at = ?
		WHERE id = ?
	`, item.Status, item.MaxRetries, item.AttemptCount, item.TimeoutSeconds, nullableString(item.CallbackToken), nullableString(item.ClaimedByAgentID), nullableString(item.RuntimeEndpoint), nullableString(item.RuntimeClusterID), nullableString(item.StopTransport), string(payload), string(result), item.StartedAt, item.LastHeartbeatAt, item.LastRuntimeSeenAt, item.FinishedAt, item.UpdatedAt, item.ID)
	if res.Error != nil {
		return domaindelivery.ExecutionTask{}, fmt.Errorf("update execution task: %w", res.Error)
	}
	if res.RowsAffected == 0 {
		return domaindelivery.ExecutionTask{}, ErrNotFound
	}
	return item, nil
}

func (r *Repository) CreateExecutionLog(ctx context.Context, item domaindelivery.ExecutionLog) error {
	payload, err := json.Marshal(item.Metadata)
	if err != nil {
		return fmt.Errorf("marshal execution log metadata: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO execution_logs (id, execution_task_id, log_level, message, metadata, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, item.ID, item.ExecutionTaskID, item.LogLevel, item.Message, string(payload), item.CreatedAt).Error; err != nil {
		return fmt.Errorf("create execution log: %w", err)
	}
	return nil
}

func (r *Repository) ListExecutionLogs(ctx context.Context, taskID string, limit int) ([]domaindelivery.ExecutionLog, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, execution_task_id, log_level, message, metadata, created_at
		FROM execution_logs
		WHERE execution_task_id = ?
		ORDER BY created_at ASC
		LIMIT ?
	`, strings.TrimSpace(taskID), limit).Rows()
	if err != nil {
		return nil, fmt.Errorf("query execution logs: %w", err)
	}
	defer rows.Close()

	items := make([]domaindelivery.ExecutionLog, 0, limit)
	for rows.Next() {
		item, scanErr := scanExecutionLog(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) CreateExecutionCallback(ctx context.Context, item domaindelivery.ExecutionCallback) error {
	payload, err := json.Marshal(item.Payload)
	if err != nil {
		return fmt.Errorf("marshal execution callback payload: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO execution_callbacks (id, execution_task_id, provider_kind, status, payload, created_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, item.ID, item.ExecutionTaskID, item.ProviderKind, item.Status, string(payload), item.CreatedAt).Error; err != nil {
		return fmt.Errorf("create execution callback: %w", err)
	}
	return nil
}

func (r *Repository) ListExecutionArtifacts(ctx context.Context, taskID string) ([]domaindelivery.ExecutionArtifact, error) {
	return r.ListArtifacts(ctx, domaindelivery.ArtifactFilter{ExecutionTaskID: strings.TrimSpace(taskID), Limit: 500})
}

func (r *Repository) ListExecutionArtifactsByBundle(ctx context.Context, bundleID string) ([]domaindelivery.ExecutionArtifact, error) {
	return r.ListArtifacts(ctx, domaindelivery.ArtifactFilter{ReleaseBundleID: strings.TrimSpace(bundleID), Limit: 500})
}

func (r *Repository) ListArtifacts(ctx context.Context, filter domaindelivery.ArtifactFilter) ([]domaindelivery.ExecutionArtifact, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 100
	}
	query := artifactSelectColumns
	args := []any{}
	clauses := make([]string, 0, 8)
	if value := strings.TrimSpace(filter.ApplicationID); value != "" {
		clauses = append(clauses, "application_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.ApplicationEnvironmentID); value != "" {
		clauses = append(clauses, "application_environment_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.WorkflowRunID); value != "" {
		clauses = append(clauses, "workflow_run_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.WorkflowNodeID); value != "" {
		clauses = append(clauses, "workflow_node_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.ReleaseBundleID); value != "" {
		clauses = append(clauses, "release_bundle_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.ExecutionTaskID); value != "" {
		clauses = append(clauses, "execution_task_id = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Kind); value != "" {
		clauses = append(clauses, "artifact_kind = ?")
		args = append(args, value)
	}
	if value := strings.TrimSpace(filter.Status); value != "" {
		clauses = append(clauses, "status = ?")
		args = append(args, value)
	}
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY created_at ASC LIMIT ?"
	args = append(args, limit)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, fmt.Errorf("query execution artifacts: %w", err)
	}
	defer rows.Close()

	items := make([]domaindelivery.ExecutionArtifact, 0)
	for rows.Next() {
		item, scanErr := scanExecutionArtifact(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) UpsertExecutionArtifact(ctx context.Context, item domaindelivery.ExecutionArtifact) (domaindelivery.ExecutionArtifact, error) {
	if strings.TrimSpace(item.ID) == "" {
		item.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = now
	}
	if item.ModifiedAt != nil && !item.ModifiedAt.IsZero() {
		item.UpdatedAt = item.ModifiedAt.UTC()
	}
	modifiedAt := item.UpdatedAt
	item.ModifiedAt = &modifiedAt
	payload, err := json.Marshal(item.Metadata)
	if err != nil {
		return domaindelivery.ExecutionArtifact{}, fmt.Errorf("marshal execution artifact metadata: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO execution_artifacts (id, execution_task_id, release_bundle_id, workflow_run_id, workflow_node_id, application_id, application_environment_id, artifact_kind, name, ref, digest, path, status, size_bytes, metadata, retention_until, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			execution_task_id = EXCLUDED.execution_task_id,
			release_bundle_id = EXCLUDED.release_bundle_id,
			workflow_run_id = EXCLUDED.workflow_run_id,
			workflow_node_id = EXCLUDED.workflow_node_id,
			application_id = EXCLUDED.application_id,
			application_environment_id = EXCLUDED.application_environment_id,
			artifact_kind = EXCLUDED.artifact_kind,
			name = EXCLUDED.name,
			ref = EXCLUDED.ref,
			digest = EXCLUDED.digest,
			path = EXCLUDED.path,
			status = EXCLUDED.status,
			size_bytes = EXCLUDED.size_bytes,
			metadata = EXCLUDED.metadata,
			retention_until = EXCLUDED.retention_until,
			updated_at = EXCLUDED.updated_at
	`, item.ID, nullableString(item.ExecutionTaskID), nullableString(item.ReleaseBundleID), nullableString(item.WorkflowRunID), nullableString(item.WorkflowNodeID), nullableString(item.ApplicationID), nullableString(item.ApplicationEnvironmentID), item.Kind, nullableString(item.Name), nullableString(item.Ref), nullableString(item.Digest), nullableString(item.Path), nullableString(item.Status), item.SizeBytes, string(payload), nullableTime(item.RetentionUntil), item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domaindelivery.ExecutionArtifact{}, fmt.Errorf("upsert execution artifact: %w", err)
	}
	return item, nil
}

func (r *Repository) ListDeliveryBlueprints(ctx context.Context) ([]domaindelivery.DeliveryBlueprint, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, blueprint_key, name, description, application_draft, build_sources, environment_bindings, file_templates, execution_hints, post_create_actions, enabled, created_at, updated_at
		FROM delivery_blueprints
		ORDER BY created_at DESC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query delivery blueprints: %w", err)
	}
	defer rows.Close()

	items := make([]domaindelivery.DeliveryBlueprint, 0)
	for rows.Next() {
		item, scanErr := scanDeliveryBlueprint(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetDeliveryBlueprint(ctx context.Context, id string) (domaindelivery.DeliveryBlueprint, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, blueprint_key, name, description, application_draft, build_sources, environment_bindings, file_templates, execution_hints, post_create_actions, enabled, created_at, updated_at
		FROM delivery_blueprints
		WHERE id = ?
		LIMIT 1
	`, strings.TrimSpace(id)).Row()
	return scanDeliveryBlueprintRow(row)
}

func (r *Repository) CreateDeliveryBlueprint(ctx context.Context, input domaindelivery.DeliveryBlueprintInput) (domaindelivery.DeliveryBlueprint, error) {
	item := normalizeDeliveryBlueprintInput(input)
	if err := r.saveDeliveryBlueprint(ctx, item, true); err != nil {
		return domaindelivery.DeliveryBlueprint{}, err
	}
	return item, nil
}

func (r *Repository) UpdateDeliveryBlueprint(ctx context.Context, id string, input domaindelivery.DeliveryBlueprintInput) (domaindelivery.DeliveryBlueprint, error) {
	item := normalizeDeliveryBlueprintInput(input)
	item.ID = strings.TrimSpace(id)
	item.CreatedAt = fetchCreatedAt(ctx, r.db, "delivery_blueprints", item.ID)
	if item.CreatedAt.IsZero() {
		return domaindelivery.DeliveryBlueprint{}, ErrNotFound
	}
	if err := r.saveDeliveryBlueprint(ctx, item, false); err != nil {
		return domaindelivery.DeliveryBlueprint{}, err
	}
	return item, nil
}

func (r *Repository) saveDeliveryBlueprint(ctx context.Context, item domaindelivery.DeliveryBlueprint, create bool) error {
	applicationDraft, err := json.Marshal(item.ApplicationDraft)
	if err != nil {
		return fmt.Errorf("marshal delivery blueprint application draft: %w", err)
	}
	buildSources, err := json.Marshal(item.BuildSources)
	if err != nil {
		return fmt.Errorf("marshal delivery blueprint build sources: %w", err)
	}
	environmentBindings, err := json.Marshal(item.EnvironmentBindings)
	if err != nil {
		return fmt.Errorf("marshal delivery blueprint environment bindings: %w", err)
	}
	fileTemplates, err := json.Marshal(item.Files)
	if err != nil {
		return fmt.Errorf("marshal delivery blueprint file templates: %w", err)
	}
	executionHints, err := json.Marshal(item.ExecutionHints)
	if err != nil {
		return fmt.Errorf("marshal delivery blueprint execution hints: %w", err)
	}
	postCreateActions, err := json.Marshal(item.PostCreateActions)
	if err != nil {
		return fmt.Errorf("marshal delivery blueprint post-create actions: %w", err)
	}
	if create {
		if err := r.db.WithContext(ctx).Exec(`
			INSERT INTO delivery_blueprints (id, blueprint_key, name, description, application_draft, build_sources, environment_bindings, file_templates, execution_hints, post_create_actions, enabled, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, item.ID, item.Key, item.Name, nullableString(item.Description), string(applicationDraft), string(buildSources), string(environmentBindings), string(fileTemplates), string(executionHints), string(postCreateActions), item.Enabled, item.CreatedAt, item.UpdatedAt).Error; err != nil {
			return fmt.Errorf("create delivery blueprint: %w", err)
		}
		return nil
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE delivery_blueprints
		SET blueprint_key = ?, name = ?, description = ?, application_draft = ?, build_sources = ?, environment_bindings = ?, file_templates = ?, execution_hints = ?, post_create_actions = ?, enabled = ?, updated_at = ?
		WHERE id = ?
	`, item.Key, item.Name, nullableString(item.Description), string(applicationDraft), string(buildSources), string(environmentBindings), string(fileTemplates), string(executionHints), string(postCreateActions), item.Enabled, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return fmt.Errorf("update delivery blueprint: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) CreateDeliveryDraft(ctx context.Context, input domaindelivery.DeliveryDraftInput, createdBy string) (domaindelivery.DeliveryDraft, error) {
	item := normalizeDeliveryDraftInput(input, createdBy)
	if err := r.saveDeliveryDraft(ctx, item, true); err != nil {
		return domaindelivery.DeliveryDraft{}, err
	}
	return item, nil
}

func (r *Repository) GetDeliveryDraft(ctx context.Context, id string) (domaindelivery.DeliveryDraft, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, source, status, application_draft, services, build_sources, environment_bindings, file_templates, execution_hints, post_create_actions, created_by, confirmed_at, created_at, updated_at
		FROM delivery_drafts
		WHERE id = ?
		LIMIT 1
	`, strings.TrimSpace(id)).Row()
	return scanDeliveryDraftRow(row)
}

func (r *Repository) UpdateDeliveryDraft(ctx context.Context, item domaindelivery.DeliveryDraft) (domaindelivery.DeliveryDraft, error) {
	item.ID = strings.TrimSpace(item.ID)
	if item.ID == "" {
		return domaindelivery.DeliveryDraft{}, ErrNotFound
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = fetchCreatedAt(ctx, r.db, "delivery_drafts", item.ID)
	}
	if item.CreatedAt.IsZero() {
		return domaindelivery.DeliveryDraft{}, ErrNotFound
	}
	item.UpdatedAt = time.Now().UTC()
	if err := r.saveDeliveryDraft(ctx, item, false); err != nil {
		return domaindelivery.DeliveryDraft{}, err
	}
	return item, nil
}

func (r *Repository) saveDeliveryDraft(ctx context.Context, item domaindelivery.DeliveryDraft, create bool) error {
	applicationDraft, err := json.Marshal(item.ApplicationDraft)
	if err != nil {
		return fmt.Errorf("marshal delivery draft application draft: %w", err)
	}
	services, err := json.Marshal(item.Services)
	if err != nil {
		return fmt.Errorf("marshal delivery draft services: %w", err)
	}
	buildSources, err := json.Marshal(item.BuildSources)
	if err != nil {
		return fmt.Errorf("marshal delivery draft build sources: %w", err)
	}
	environmentBindings, err := json.Marshal(item.EnvironmentBindings)
	if err != nil {
		return fmt.Errorf("marshal delivery draft environment bindings: %w", err)
	}
	fileTemplates, err := json.Marshal(item.Files)
	if err != nil {
		return fmt.Errorf("marshal delivery draft file templates: %w", err)
	}
	executionHints, err := json.Marshal(item.ExecutionHints)
	if err != nil {
		return fmt.Errorf("marshal delivery draft execution hints: %w", err)
	}
	postCreateActions, err := json.Marshal(item.PostCreateActions)
	if err != nil {
		return fmt.Errorf("marshal delivery draft post-create actions: %w", err)
	}
	if create {
		if err := r.db.WithContext(ctx).Exec(`
			INSERT INTO delivery_drafts (id, source, status, application_draft, services, build_sources, environment_bindings, file_templates, execution_hints, post_create_actions, created_by, confirmed_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, item.ID, item.Source, item.Status, string(applicationDraft), string(services), string(buildSources), string(environmentBindings), string(fileTemplates), string(executionHints), string(postCreateActions), nullableString(item.CreatedBy), item.ConfirmedAt, item.CreatedAt, item.UpdatedAt).Error; err != nil {
			return fmt.Errorf("create delivery draft: %w", err)
		}
		return nil
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE delivery_drafts
		SET source = ?, status = ?, application_draft = ?, services = ?, build_sources = ?, environment_bindings = ?, file_templates = ?, execution_hints = ?, post_create_actions = ?, created_by = ?, confirmed_at = ?, updated_at = ?
		WHERE id = ?
	`, item.Source, item.Status, string(applicationDraft), string(services), string(buildSources), string(environmentBindings), string(fileTemplates), string(executionHints), string(postCreateActions), nullableString(item.CreatedBy), item.ConfirmedAt, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return fmt.Errorf("update delivery draft: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) CreateDeliveryPlan(ctx context.Context, input domaindelivery.DeliveryPlanInput, createdBy string) (domaindelivery.DeliveryPlan, error) {
	item := normalizeDeliveryPlanInput(input, createdBy)
	if err := r.saveDeliveryPlan(ctx, item, true); err != nil {
		return domaindelivery.DeliveryPlan{}, err
	}
	return item, nil
}

func (r *Repository) GetDeliveryPlan(ctx context.Context, id string) (domaindelivery.DeliveryPlan, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, source, status, application_id, application_name, application_environment_id, environment_key, action, target_id, target_summary, build_source_id, release_bundle_id, ref_type, ref_name, image_tag, release_name, container_name, reason, risk_level, requires_approval, impact, rollback_strategy, variables, build_args, created_by, confirmed_at, created_at, updated_at
		FROM delivery_plans
		WHERE id = ?
		LIMIT 1
	`, strings.TrimSpace(id)).Row()
	return scanDeliveryPlanRow(row)
}

func (r *Repository) UpdateDeliveryPlan(ctx context.Context, item domaindelivery.DeliveryPlan) (domaindelivery.DeliveryPlan, error) {
	item.ID = strings.TrimSpace(item.ID)
	if item.ID == "" {
		return domaindelivery.DeliveryPlan{}, ErrNotFound
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = fetchCreatedAt(ctx, r.db, "delivery_plans", item.ID)
	}
	if item.CreatedAt.IsZero() {
		return domaindelivery.DeliveryPlan{}, ErrNotFound
	}
	item.UpdatedAt = time.Now().UTC()
	if err := r.saveDeliveryPlan(ctx, item, false); err != nil {
		return domaindelivery.DeliveryPlan{}, err
	}
	return item, nil
}

func (r *Repository) saveDeliveryPlan(ctx context.Context, item domaindelivery.DeliveryPlan, create bool) error {
	impact, err := json.Marshal(item.Impact)
	if err != nil {
		return fmt.Errorf("marshal delivery plan impact: %w", err)
	}
	variables, err := json.Marshal(item.Variables)
	if err != nil {
		return fmt.Errorf("marshal delivery plan variables: %w", err)
	}
	buildArgs, err := json.Marshal(item.BuildArgs)
	if err != nil {
		return fmt.Errorf("marshal delivery plan build args: %w", err)
	}
	if create {
		if err := r.db.WithContext(ctx).Exec(`
			INSERT INTO delivery_plans (id, source, status, application_id, application_name, application_environment_id, environment_key, action, target_id, target_summary, build_source_id, release_bundle_id, ref_type, ref_name, image_tag, release_name, container_name, reason, risk_level, requires_approval, impact, rollback_strategy, variables, build_args, created_by, confirmed_at, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, item.ID, item.Source, item.Status, item.ApplicationID, nullableString(item.ApplicationName), item.ApplicationEnvironmentID, nullableString(item.EnvironmentKey), item.Action, nullableString(item.TargetID), nullableString(item.TargetSummary), nullableString(item.BuildSourceID), nullableString(item.ReleaseBundleID), nullableString(item.RefType), nullableString(item.RefName), nullableString(item.ImageTag), nullableString(item.ReleaseName), nullableString(item.ContainerName), nullableString(item.Reason), nullableString(item.RiskLevel), item.RequiresApproval, string(impact), nullableString(item.RollbackStrategy), string(variables), string(buildArgs), nullableString(item.CreatedBy), item.ConfirmedAt, item.CreatedAt, item.UpdatedAt).Error; err != nil {
			return fmt.Errorf("create delivery plan: %w", err)
		}
		return nil
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE delivery_plans
		SET source = ?, status = ?, application_id = ?, application_name = ?, application_environment_id = ?, environment_key = ?, action = ?, target_id = ?, target_summary = ?, build_source_id = ?, release_bundle_id = ?, ref_type = ?, ref_name = ?, image_tag = ?, release_name = ?, container_name = ?, reason = ?, risk_level = ?, requires_approval = ?, impact = ?, rollback_strategy = ?, variables = ?, build_args = ?, created_by = ?, confirmed_at = ?, updated_at = ?
		WHERE id = ?
	`, item.Source, item.Status, item.ApplicationID, nullableString(item.ApplicationName), item.ApplicationEnvironmentID, nullableString(item.EnvironmentKey), item.Action, nullableString(item.TargetID), nullableString(item.TargetSummary), nullableString(item.BuildSourceID), nullableString(item.ReleaseBundleID), nullableString(item.RefType), nullableString(item.RefName), nullableString(item.ImageTag), nullableString(item.ReleaseName), nullableString(item.ContainerName), nullableString(item.Reason), nullableString(item.RiskLevel), item.RequiresApproval, string(impact), nullableString(item.RollbackStrategy), string(variables), string(buildArgs), nullableString(item.CreatedBy), item.ConfirmedAt, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return fmt.Errorf("update delivery plan: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func scanReleaseBundle(rows *sql.Rows) (domaindelivery.ReleaseBundle, error) {
	var item domaindelivery.ReleaseBundle
	var applicationEnvironmentID sql.NullString
	var artifactRef sql.NullString
	var artifactDigest sql.NullString
	var metadata []byte
	if err := rows.Scan(&item.ID, &item.ApplicationID, &applicationEnvironmentID, &item.Version, &item.SourceType, &item.Status, &artifactRef, &artifactDigest, &metadata, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaindelivery.ReleaseBundle{}, fmt.Errorf("scan release bundle: %w", err)
	}
	item.ApplicationEnvironmentID = applicationEnvironmentID.String
	item.ArtifactRef = artifactRef.String
	item.ArtifactDigest = artifactDigest.String
	_ = json.Unmarshal(metadata, &item.Metadata)
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func scanReleaseBundleRow(row *sql.Row) (domaindelivery.ReleaseBundle, error) {
	var item domaindelivery.ReleaseBundle
	var applicationEnvironmentID sql.NullString
	var artifactRef sql.NullString
	var artifactDigest sql.NullString
	var metadata []byte
	if err := row.Scan(&item.ID, &item.ApplicationID, &applicationEnvironmentID, &item.Version, &item.SourceType, &item.Status, &artifactRef, &artifactDigest, &metadata, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domaindelivery.ReleaseBundle{}, ErrNotFound
		}
		return domaindelivery.ReleaseBundle{}, fmt.Errorf("scan release bundle row: %w", err)
	}
	item.ApplicationEnvironmentID = applicationEnvironmentID.String
	item.ArtifactRef = artifactRef.String
	item.ArtifactDigest = artifactDigest.String
	_ = json.Unmarshal(metadata, &item.Metadata)
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func scanExecutionTask(rows *sql.Rows) (domaindelivery.ExecutionTask, error) {
	var item domaindelivery.ExecutionTask
	var releaseBundleID sql.NullString
	var applicationEnvironmentID sql.NullString
	var queueKey sql.NullString
	var lockKey sql.NullString
	var callbackToken sql.NullString
	var claimedByAgentID sql.NullString
	var runtimeEndpoint sql.NullString
	var runtimeClusterID sql.NullString
	var stopTransport sql.NullString
	var payload []byte
	var result []byte
	var startedAt sql.NullTime
	var lastHeartbeatAt sql.NullTime
	var lastRuntimeSeenAt sql.NullTime
	var finishedAt sql.NullTime
	if err := rows.Scan(&item.ID, &releaseBundleID, &item.ApplicationID, &applicationEnvironmentID, &item.TaskKind, &item.ProviderKind, &item.TargetKind, &item.Status, &queueKey, &lockKey, &item.MaxRetries, &item.AttemptCount, &item.TimeoutSeconds, &callbackToken, &claimedByAgentID, &runtimeEndpoint, &runtimeClusterID, &stopTransport, &payload, &result, &startedAt, &lastHeartbeatAt, &lastRuntimeSeenAt, &finishedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaindelivery.ExecutionTask{}, fmt.Errorf("scan execution task: %w", err)
	}
	item.ReleaseBundleID = releaseBundleID.String
	item.ApplicationEnvironmentID = applicationEnvironmentID.String
	item.QueueKey = queueKey.String
	item.LockKey = lockKey.String
	item.CallbackToken = callbackToken.String
	item.ClaimedByAgentID = claimedByAgentID.String
	item.RuntimeEndpoint = runtimeEndpoint.String
	item.RuntimeClusterID = runtimeClusterID.String
	item.StopTransport = stopTransport.String
	_ = json.Unmarshal(payload, &item.Payload)
	_ = json.Unmarshal(result, &item.Result)
	if item.Payload == nil {
		item.Payload = map[string]any{}
	}
	if item.Result == nil {
		item.Result = map[string]any{}
	}
	item.Artifacts = buildExecutionArtifacts(item)
	if startedAt.Valid {
		value := startedAt.Time
		item.StartedAt = &value
	}
	if lastHeartbeatAt.Valid {
		value := lastHeartbeatAt.Time
		item.LastHeartbeatAt = &value
	}
	if lastRuntimeSeenAt.Valid {
		value := lastRuntimeSeenAt.Time
		item.LastRuntimeSeenAt = &value
	}
	if finishedAt.Valid {
		value := finishedAt.Time
		item.FinishedAt = &value
	}
	return item, nil
}

func scanExecutionTaskRow(row *sql.Row) (domaindelivery.ExecutionTask, error) {
	var item domaindelivery.ExecutionTask
	var releaseBundleID sql.NullString
	var applicationEnvironmentID sql.NullString
	var queueKey sql.NullString
	var lockKey sql.NullString
	var callbackToken sql.NullString
	var claimedByAgentID sql.NullString
	var runtimeEndpoint sql.NullString
	var runtimeClusterID sql.NullString
	var stopTransport sql.NullString
	var payload []byte
	var result []byte
	var startedAt sql.NullTime
	var lastHeartbeatAt sql.NullTime
	var lastRuntimeSeenAt sql.NullTime
	var finishedAt sql.NullTime
	if err := row.Scan(&item.ID, &releaseBundleID, &item.ApplicationID, &applicationEnvironmentID, &item.TaskKind, &item.ProviderKind, &item.TargetKind, &item.Status, &queueKey, &lockKey, &item.MaxRetries, &item.AttemptCount, &item.TimeoutSeconds, &callbackToken, &claimedByAgentID, &runtimeEndpoint, &runtimeClusterID, &stopTransport, &payload, &result, &startedAt, &lastHeartbeatAt, &lastRuntimeSeenAt, &finishedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domaindelivery.ExecutionTask{}, ErrNotFound
		}
		return domaindelivery.ExecutionTask{}, fmt.Errorf("scan execution task row: %w", err)
	}
	item.ReleaseBundleID = releaseBundleID.String
	item.ApplicationEnvironmentID = applicationEnvironmentID.String
	item.QueueKey = queueKey.String
	item.LockKey = lockKey.String
	item.CallbackToken = callbackToken.String
	item.ClaimedByAgentID = claimedByAgentID.String
	item.RuntimeEndpoint = runtimeEndpoint.String
	item.RuntimeClusterID = runtimeClusterID.String
	item.StopTransport = stopTransport.String
	_ = json.Unmarshal(payload, &item.Payload)
	_ = json.Unmarshal(result, &item.Result)
	if item.Payload == nil {
		item.Payload = map[string]any{}
	}
	if item.Result == nil {
		item.Result = map[string]any{}
	}
	item.Artifacts = buildExecutionArtifacts(item)
	if startedAt.Valid {
		value := startedAt.Time
		item.StartedAt = &value
	}
	if lastHeartbeatAt.Valid {
		value := lastHeartbeatAt.Time
		item.LastHeartbeatAt = &value
	}
	if lastRuntimeSeenAt.Valid {
		value := lastRuntimeSeenAt.Time
		item.LastRuntimeSeenAt = &value
	}
	if finishedAt.Valid {
		value := finishedAt.Time
		item.FinishedAt = &value
	}
	return item, nil
}

func scanExecutionLog(rows *sql.Rows) (domaindelivery.ExecutionLog, error) {
	var item domaindelivery.ExecutionLog
	var metadata []byte
	if err := rows.Scan(&item.ID, &item.ExecutionTaskID, &item.LogLevel, &item.Message, &metadata, &item.CreatedAt); err != nil {
		return domaindelivery.ExecutionLog{}, fmt.Errorf("scan execution log: %w", err)
	}
	_ = json.Unmarshal(metadata, &item.Metadata)
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func scanExecutionArtifact(rows *sql.Rows) (domaindelivery.ExecutionArtifact, error) {
	var item domaindelivery.ExecutionArtifact
	var executionTaskID sql.NullString
	var releaseBundleID sql.NullString
	var workflowRunID sql.NullString
	var workflowNodeID sql.NullString
	var applicationID sql.NullString
	var applicationEnvironmentID sql.NullString
	var name sql.NullString
	var ref sql.NullString
	var digest sql.NullString
	var path sql.NullString
	var status sql.NullString
	var metadata []byte
	var retentionUntil sql.NullTime
	if err := rows.Scan(&item.ID, &executionTaskID, &releaseBundleID, &workflowRunID, &workflowNodeID, &applicationID, &applicationEnvironmentID, &item.Kind, &name, &ref, &digest, &path, &status, &item.SizeBytes, &metadata, &retentionUntil, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaindelivery.ExecutionArtifact{}, fmt.Errorf("scan execution artifact: %w", err)
	}
	item.ExecutionTaskID = executionTaskID.String
	item.ReleaseBundleID = releaseBundleID.String
	item.WorkflowRunID = workflowRunID.String
	item.WorkflowNodeID = workflowNodeID.String
	item.ApplicationID = applicationID.String
	item.ApplicationEnvironmentID = applicationEnvironmentID.String
	item.Name = name.String
	item.Ref = ref.String
	item.Digest = digest.String
	item.Path = path.String
	item.Status = status.String
	if retentionUntil.Valid {
		value := retentionUntil.Time
		item.RetentionUntil = &value
	}
	if !item.UpdatedAt.IsZero() {
		value := item.UpdatedAt
		item.ModifiedAt = &value
	}
	_ = json.Unmarshal(metadata, &item.Metadata)
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func scanDeliveryBlueprint(rows *sql.Rows) (domaindelivery.DeliveryBlueprint, error) {
	var item domaindelivery.DeliveryBlueprint
	var description sql.NullString
	var applicationDraft []byte
	var buildSources []byte
	var environmentBindings []byte
	var fileTemplates []byte
	var executionHints []byte
	var postCreateActions []byte
	if err := rows.Scan(&item.ID, &item.Key, &item.Name, &description, &applicationDraft, &buildSources, &environmentBindings, &fileTemplates, &executionHints, &postCreateActions, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaindelivery.DeliveryBlueprint{}, fmt.Errorf("scan delivery blueprint: %w", err)
	}
	item.Description = description.String
	_ = json.Unmarshal(applicationDraft, &item.ApplicationDraft)
	_ = json.Unmarshal(buildSources, &item.BuildSources)
	_ = json.Unmarshal(environmentBindings, &item.EnvironmentBindings)
	_ = json.Unmarshal(fileTemplates, &item.Files)
	_ = json.Unmarshal(executionHints, &item.ExecutionHints)
	_ = json.Unmarshal(postCreateActions, &item.PostCreateActions)
	if item.ApplicationDraft.Metadata == nil {
		item.ApplicationDraft.Metadata = map[string]any{}
	}
	if item.ExecutionHints == nil {
		item.ExecutionHints = map[string]any{}
	}
	return item, nil
}

func scanDeliveryBlueprintRow(row *sql.Row) (domaindelivery.DeliveryBlueprint, error) {
	var item domaindelivery.DeliveryBlueprint
	var description sql.NullString
	var applicationDraft []byte
	var buildSources []byte
	var environmentBindings []byte
	var fileTemplates []byte
	var executionHints []byte
	var postCreateActions []byte
	if err := row.Scan(&item.ID, &item.Key, &item.Name, &description, &applicationDraft, &buildSources, &environmentBindings, &fileTemplates, &executionHints, &postCreateActions, &item.Enabled, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domaindelivery.DeliveryBlueprint{}, ErrNotFound
		}
		return domaindelivery.DeliveryBlueprint{}, fmt.Errorf("scan delivery blueprint row: %w", err)
	}
	item.Description = description.String
	_ = json.Unmarshal(applicationDraft, &item.ApplicationDraft)
	_ = json.Unmarshal(buildSources, &item.BuildSources)
	_ = json.Unmarshal(environmentBindings, &item.EnvironmentBindings)
	_ = json.Unmarshal(fileTemplates, &item.Files)
	_ = json.Unmarshal(executionHints, &item.ExecutionHints)
	_ = json.Unmarshal(postCreateActions, &item.PostCreateActions)
	if item.ApplicationDraft.Metadata == nil {
		item.ApplicationDraft.Metadata = map[string]any{}
	}
	if item.ExecutionHints == nil {
		item.ExecutionHints = map[string]any{}
	}
	return item, nil
}

func scanDeliveryDraftRow(row *sql.Row) (domaindelivery.DeliveryDraft, error) {
	var item domaindelivery.DeliveryDraft
	var applicationDraft []byte
	var services []byte
	var buildSources []byte
	var environmentBindings []byte
	var fileTemplates []byte
	var executionHints []byte
	var postCreateActions []byte
	var createdBy sql.NullString
	var confirmedAt sql.NullTime
	if err := row.Scan(&item.ID, &item.Source, &item.Status, &applicationDraft, &services, &buildSources, &environmentBindings, &fileTemplates, &executionHints, &postCreateActions, &createdBy, &confirmedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domaindelivery.DeliveryDraft{}, ErrNotFound
		}
		return domaindelivery.DeliveryDraft{}, fmt.Errorf("scan delivery draft row: %w", err)
	}
	item.CreatedBy = createdBy.String
	if confirmedAt.Valid {
		value := confirmedAt.Time
		item.ConfirmedAt = &value
	}
	_ = json.Unmarshal(applicationDraft, &item.ApplicationDraft)
	_ = json.Unmarshal(services, &item.Services)
	_ = json.Unmarshal(buildSources, &item.BuildSources)
	_ = json.Unmarshal(environmentBindings, &item.EnvironmentBindings)
	_ = json.Unmarshal(fileTemplates, &item.Files)
	_ = json.Unmarshal(executionHints, &item.ExecutionHints)
	_ = json.Unmarshal(postCreateActions, &item.PostCreateActions)
	if item.ApplicationDraft.Metadata == nil {
		item.ApplicationDraft.Metadata = map[string]any{}
	}
	if item.ExecutionHints == nil {
		item.ExecutionHints = map[string]any{}
	}
	return item, nil
}

func scanDeliveryPlanRow(row *sql.Row) (domaindelivery.DeliveryPlan, error) {
	var item domaindelivery.DeliveryPlan
	var applicationName sql.NullString
	var environmentKey sql.NullString
	var targetID sql.NullString
	var targetSummary sql.NullString
	var buildSourceID sql.NullString
	var releaseBundleID sql.NullString
	var refType sql.NullString
	var refName sql.NullString
	var imageTag sql.NullString
	var releaseName sql.NullString
	var containerName sql.NullString
	var reason sql.NullString
	var riskLevel sql.NullString
	var rollbackStrategy sql.NullString
	var impact []byte
	var variables []byte
	var buildArgs []byte
	var createdBy sql.NullString
	var confirmedAt sql.NullTime
	if err := row.Scan(&item.ID, &item.Source, &item.Status, &item.ApplicationID, &applicationName, &item.ApplicationEnvironmentID, &environmentKey, &item.Action, &targetID, &targetSummary, &buildSourceID, &releaseBundleID, &refType, &refName, &imageTag, &releaseName, &containerName, &reason, &riskLevel, &item.RequiresApproval, &impact, &rollbackStrategy, &variables, &buildArgs, &createdBy, &confirmedAt, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domaindelivery.DeliveryPlan{}, ErrNotFound
		}
		return domaindelivery.DeliveryPlan{}, fmt.Errorf("scan delivery plan row: %w", err)
	}
	item.ApplicationName = applicationName.String
	item.EnvironmentKey = environmentKey.String
	item.TargetID = targetID.String
	item.TargetSummary = targetSummary.String
	item.BuildSourceID = buildSourceID.String
	item.ReleaseBundleID = releaseBundleID.String
	item.RefType = refType.String
	item.RefName = refName.String
	item.ImageTag = imageTag.String
	item.ReleaseName = releaseName.String
	item.ContainerName = containerName.String
	item.Reason = reason.String
	item.RiskLevel = riskLevel.String
	item.RollbackStrategy = rollbackStrategy.String
	item.CreatedBy = createdBy.String
	if confirmedAt.Valid {
		value := confirmedAt.Time
		item.ConfirmedAt = &value
	}
	_ = json.Unmarshal(impact, &item.Impact)
	_ = json.Unmarshal(variables, &item.Variables)
	_ = json.Unmarshal(buildArgs, &item.BuildArgs)
	if item.Impact == nil {
		item.Impact = map[string]any{}
	}
	if item.Variables == nil {
		item.Variables = map[string]any{}
	}
	if item.BuildArgs == nil {
		item.BuildArgs = map[string]any{}
	}
	return item, nil
}

func normalizeDeliveryBlueprintInput(input domaindelivery.DeliveryBlueprintInput) domaindelivery.DeliveryBlueprint {
	now := time.Now().UTC()
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	draft := input.ApplicationDraft
	draft.Name = strings.TrimSpace(draft.Name)
	draft.Key = strings.TrimSpace(draft.Key)
	draft.Group = strings.TrimSpace(draft.Group)
	draft.BusinessLineID = strings.TrimSpace(draft.BusinessLineID)
	draft.Language = strings.TrimSpace(draft.Language)
	if draft.Language == "" {
		draft.Language = "node"
	}
	draft.Description = strings.TrimSpace(draft.Description)
	draft.OwnerTeam = strings.TrimSpace(draft.OwnerTeam)
	draft.RepositoryProvider = strings.TrimSpace(draft.RepositoryProvider)
	draft.RepositoryProjectID = strings.TrimSpace(draft.RepositoryProjectID)
	draft.RepositoryPath = strings.TrimSpace(draft.RepositoryPath)
	draft.DefaultBranch = strings.TrimSpace(draft.DefaultBranch)
	if draft.DefaultBranch == "" {
		draft.DefaultBranch = "main"
	}
	draft.DefaultTag = strings.TrimSpace(draft.DefaultTag)
	draft.BuildImage = strings.TrimSpace(draft.BuildImage)
	draft.BuildContextDir = strings.TrimSpace(draft.BuildContextDir)
	draft.DockerfilePath = strings.TrimSpace(draft.DockerfilePath)
	if draft.Metadata == nil {
		draft.Metadata = map[string]any{}
	}
	buildSources := make([]domainapp.BuildSourceInput, 0, len(input.BuildSources))
	for _, source := range input.BuildSources {
		buildSources = append(buildSources, domainapp.BuildSourceInput{
			ID:         strings.TrimSpace(source.ID),
			Name:       strings.TrimSpace(source.Name),
			Type:       source.Type,
			Enabled:    source.Enabled,
			IsDefault:  source.IsDefault,
			BuildImage: strings.TrimSpace(source.BuildImage),
			DefaultTag: strings.TrimSpace(source.DefaultTag),
			Config:     source.Config,
		})
	}
	environmentBindings := make([]domaindelivery.BlueprintEnvironmentBindingTemplate, 0, len(input.EnvironmentBindings))
	for _, binding := range input.EnvironmentBindings {
		environmentBindings = append(environmentBindings, domaindelivery.BlueprintEnvironmentBindingTemplate{
			EnvironmentID:      strings.TrimSpace(binding.EnvironmentID),
			EnvironmentKey:     strings.TrimSpace(binding.EnvironmentKey),
			BusinessLineID:     strings.TrimSpace(binding.BusinessLineID),
			StrategyProfileID:  strings.TrimSpace(binding.StrategyProfileID),
			PromotionPolicyID:  strings.TrimSpace(binding.PromotionPolicyID),
			ArtifactPolicyID:   strings.TrimSpace(binding.ArtifactPolicyID),
			WorkflowTemplateID: strings.TrimSpace(binding.WorkflowTemplateID),
			BuildPolicy:        binding.BuildPolicy,
			ReleasePolicy:      binding.ReleasePolicy,
			ResourceSelector:   binding.ResourceSelector,
			Targets:            binding.Targets,
		})
	}
	files := make([]domaindelivery.BlueprintFileTemplate, 0, len(input.Files))
	for _, file := range input.Files {
		files = append(files, domaindelivery.BlueprintFileTemplate{
			Path:     strings.TrimSpace(file.Path),
			Kind:     strings.TrimSpace(file.Kind),
			Content:  file.Content,
			Required: file.Required,
			Purpose:  strings.TrimSpace(file.Purpose),
		})
	}
	executionHints := input.ExecutionHints
	if executionHints == nil {
		executionHints = map[string]any{}
	}
	postCreateActions := make([]string, 0, len(input.PostCreateActions))
	for _, action := range input.PostCreateActions {
		trimmed := strings.TrimSpace(action)
		if trimmed != "" {
			postCreateActions = append(postCreateActions, trimmed)
		}
	}
	return domaindelivery.DeliveryBlueprint{
		ID:                  id,
		Key:                 strings.TrimSpace(input.Key),
		Name:                strings.TrimSpace(input.Name),
		Description:         strings.TrimSpace(input.Description),
		ApplicationDraft:    draft,
		BuildSources:        buildSources,
		EnvironmentBindings: environmentBindings,
		Files:               files,
		ExecutionHints:      executionHints,
		PostCreateActions:   postCreateActions,
		Enabled:             input.Enabled,
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

func normalizeDeliveryDraftInput(input domaindelivery.DeliveryDraftInput, createdBy string) domaindelivery.DeliveryDraft {
	now := time.Now().UTC()
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	source := strings.TrimSpace(input.Source)
	switch source {
	case domaindelivery.DeliveryDraftSourceAI, domaindelivery.DeliveryDraftSourceBlueprint:
	default:
		source = domaindelivery.DeliveryDraftSourceManual
	}
	blueprintLike := normalizeDeliveryBlueprintInput(domaindelivery.DeliveryBlueprintInput{
		ID:                  id,
		Key:                 strings.TrimSpace(input.ApplicationDraft.Key),
		Name:                strings.TrimSpace(input.ApplicationDraft.Name),
		ApplicationDraft:    input.ApplicationDraft,
		BuildSources:        input.BuildSources,
		EnvironmentBindings: input.EnvironmentBindings,
		Files:               input.Files,
		ExecutionHints:      input.ExecutionHints,
		PostCreateActions:   input.PostCreateActions,
		Enabled:             true,
	})
	services := make([]domaindelivery.DeliveryDraftService, 0, len(input.Services))
	for _, service := range input.Services {
		kind := service.ServiceKind
		if kind == "" {
			kind = domainapp.ServiceKindKubernetesWorkload
		}
		metadata := service.Metadata
		if metadata == nil {
			metadata = map[string]any{}
		}
		services = append(services, domaindelivery.DeliveryDraftService{
			ID:                  strings.TrimSpace(service.ID),
			Key:                 strings.TrimSpace(service.Key),
			Name:                strings.TrimSpace(service.Name),
			Description:         strings.TrimSpace(service.Description),
			ServiceKind:         kind,
			OwnerTeam:           strings.TrimSpace(service.OwnerTeam),
			RepositoryProvider:  strings.TrimSpace(service.RepositoryProvider),
			RepositoryProjectID: strings.TrimSpace(service.RepositoryProjectID),
			RepositoryPath:      strings.TrimSpace(service.RepositoryPath),
			DefaultBranch:       strings.TrimSpace(service.DefaultBranch),
			BuildSourceID:       strings.TrimSpace(service.BuildSourceID),
			Enabled:             service.Enabled,
			Metadata:            metadata,
			Containers:          service.Containers,
		})
	}
	return domaindelivery.DeliveryDraft{
		ID:                  id,
		Source:              source,
		Status:              domaindelivery.DeliveryDraftStatusDraft,
		ApplicationDraft:    blueprintLike.ApplicationDraft,
		Services:            services,
		BuildSources:        blueprintLike.BuildSources,
		EnvironmentBindings: blueprintLike.EnvironmentBindings,
		Files:               blueprintLike.Files,
		ExecutionHints:      blueprintLike.ExecutionHints,
		PostCreateActions:   blueprintLike.PostCreateActions,
		CreatedBy:           strings.TrimSpace(createdBy),
		CreatedAt:           now,
		UpdatedAt:           now,
	}
}

func normalizeDeliveryPlanInput(input domaindelivery.DeliveryPlanInput, createdBy string) domaindelivery.DeliveryPlan {
	now := time.Now().UTC()
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	source := strings.TrimSpace(input.Source)
	if source != domaindelivery.DeliveryPlanSourceAI {
		source = domaindelivery.DeliveryPlanSourceManual
	}
	action := input.Action
	if strings.TrimSpace(string(action)) == "" {
		action = domaindelivery.ApplicationDeliveryActionBuildDeploy
	}
	refType := strings.TrimSpace(input.RefType)
	if refType == "" {
		refType = "branch"
	}
	impact := input.Impact
	if impact == nil {
		impact = map[string]any{}
	}
	variables := input.Variables
	if variables == nil {
		variables = map[string]any{}
	}
	buildArgs := input.BuildArgs
	if buildArgs == nil {
		buildArgs = map[string]any{}
	}
	return domaindelivery.DeliveryPlan{
		ID:                       id,
		Source:                   source,
		Status:                   domaindelivery.DeliveryPlanStatusDraft,
		ApplicationID:            strings.TrimSpace(input.ApplicationID),
		ApplicationName:          strings.TrimSpace(input.ApplicationName),
		ApplicationEnvironmentID: strings.TrimSpace(input.ApplicationEnvironmentID),
		EnvironmentKey:           strings.TrimSpace(input.EnvironmentKey),
		Action:                   action,
		TargetID:                 strings.TrimSpace(input.TargetID),
		TargetSummary:            strings.TrimSpace(input.TargetSummary),
		BuildSourceID:            strings.TrimSpace(input.BuildSourceID),
		ReleaseBundleID:          strings.TrimSpace(input.ReleaseBundleID),
		RefType:                  refType,
		RefName:                  strings.TrimSpace(input.RefName),
		ImageTag:                 strings.TrimSpace(input.ImageTag),
		ReleaseName:              strings.TrimSpace(input.ReleaseName),
		ContainerName:            strings.TrimSpace(input.ContainerName),
		Reason:                   strings.TrimSpace(input.Reason),
		RiskLevel:                strings.TrimSpace(input.RiskLevel),
		RequiresApproval:         input.RequiresApproval,
		Impact:                   impact,
		RollbackStrategy:         strings.TrimSpace(input.RollbackStrategy),
		Variables:                variables,
		BuildArgs:                buildArgs,
		CreatedBy:                strings.TrimSpace(createdBy),
		CreatedAt:                now,
		UpdatedAt:                now,
	}
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
}

func nullableTime(value *time.Time) any {
	if value == nil || value.IsZero() {
		return nil
	}
	return value.UTC()
}

func fetchCreatedAt(ctx context.Context, db *gorm.DB, tableName, id string) time.Time {
	var createdAt time.Time
	if err := db.WithContext(ctx).Raw(fmt.Sprintf(`SELECT created_at FROM %s WHERE id = ?`, tableName), id).Row().Scan(&createdAt); err != nil {
		return time.Time{}
	}
	return createdAt
}

func mergeMaps(base, overlay map[string]any) map[string]any {
	next := map[string]any{}
	for key, value := range base {
		next[key] = value
	}
	for key, value := range overlay {
		next[key] = value
	}
	return next
}

func buildExecutionArtifacts(task domaindelivery.ExecutionTask) []domaindelivery.ExecutionArtifact {
	items := make([]domaindelivery.ExecutionArtifact, 0)

	appendArtifact := func(item domaindelivery.ExecutionArtifact) {
		if strings.TrimSpace(item.Kind) == "" {
			return
		}
		items = append(items, item)
	}

	if artifactMap, ok := task.Result["artifact"].(map[string]any); ok {
		appendArtifact(executionArtifactFromMap(artifactMap))
	}
	for _, artifactMap := range valueAsMapSlice(task.Result["artifacts"]) {
		appendArtifact(executionArtifactFromMap(artifactMap))
	}
	for _, artifactMap := range valueAsMapSlice(task.Result["workspaceArtifacts"]) {
		item := executionArtifactFromMap(artifactMap)
		if strings.TrimSpace(item.Kind) == "" {
			item.Kind = "workspace_file"
		}
		appendArtifact(item)
	}
	if jobName := strings.TrimSpace(fmt.Sprint(task.Result["k8sJobName"])); jobName != "" {
		appendArtifact(domaindelivery.ExecutionArtifact{
			Kind:   "k8s_job",
			Name:   jobName,
			Status: strings.TrimSpace(fmt.Sprint(task.Result["k8sJobStatus"])),
			Metadata: map[string]any{
				"clusterId": task.Result["k8sJobClusterId"],
				"namespace": task.Result["k8sJobNamespace"],
			},
		})
	}
	if len(items) == 0 {
		if image := strings.TrimSpace(fmt.Sprint(task.Result["image"])); image != "" {
			appendArtifact(domaindelivery.ExecutionArtifact{
				Kind:   "image",
				Ref:    image,
				Digest: strings.TrimSpace(fmt.Sprint(task.Result["imageDigest"])),
				Status: task.Status,
			})
		}
	}
	return items
}

func executionArtifactFromMap(raw map[string]any) domaindelivery.ExecutionArtifact {
	item := domaindelivery.ExecutionArtifact{
		ID:              strings.TrimSpace(fmt.Sprint(raw["id"])),
		ExecutionTaskID: strings.TrimSpace(fmt.Sprint(raw["executionTaskId"])),
		ReleaseBundleID: strings.TrimSpace(fmt.Sprint(raw["releaseBundleId"])),
		WorkflowRunID:   strings.TrimSpace(fmt.Sprint(raw["workflowRunId"])),
		WorkflowNodeID:  strings.TrimSpace(fmt.Sprint(raw["workflowNodeId"])),
		Kind:            strings.TrimSpace(fmt.Sprint(raw["kind"])),
		Name:            strings.TrimSpace(fmt.Sprint(raw["name"])),
		Ref:             strings.TrimSpace(fmt.Sprint(raw["ref"])),
		Digest:          strings.TrimSpace(fmt.Sprint(raw["digest"])),
		Path:            strings.TrimSpace(fmt.Sprint(raw["path"])),
		Status:          strings.TrimSpace(fmt.Sprint(raw["status"])),
		SizeBytes:       toInt64(raw["sizeBytes"]),
		Metadata:        map[string]any{},
	}
	if createdAt := parseRFC3339(raw["createdAt"]); createdAt != nil {
		item.CreatedAt = *createdAt
	}
	if updatedAt := parseRFC3339(raw["updatedAt"]); updatedAt != nil {
		item.UpdatedAt = *updatedAt
	}
	if modifiedAt := parseRFC3339(raw["modifiedAt"]); modifiedAt != nil {
		item.ModifiedAt = modifiedAt
	}
	if retentionUntil := parseRFC3339(raw["retentionUntil"]); retentionUntil != nil {
		item.RetentionUntil = retentionUntil
	}
	for key, value := range raw {
		switch key {
		case "id", "executionTaskId", "releaseBundleId", "workflowRunId", "workflowNodeId", "kind", "name", "ref", "digest", "path", "status", "sizeBytes", "createdAt", "updatedAt", "modifiedAt", "retentionUntil":
		default:
			item.Metadata[key] = value
		}
	}
	if len(item.Metadata) == 0 {
		item.Metadata = nil
	}
	return item
}

func valueAsMapSlice(raw any) []map[string]any {
	switch value := raw.(type) {
	case []map[string]any:
		return value
	case []any:
		items := make([]map[string]any, 0, len(value))
		for _, item := range value {
			mapped, ok := item.(map[string]any)
			if ok {
				items = append(items, mapped)
			}
		}
		return items
	default:
		return nil
	}
}

func toInt64(raw any) int64 {
	switch value := raw.(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		return int64(value)
	default:
		return 0
	}
}

func parseRFC3339(raw any) *time.Time {
	text := strings.TrimSpace(fmt.Sprint(raw))
	if text == "" {
		return nil
	}
	value, err := time.Parse(time.RFC3339, text)
	if err != nil {
		return nil
	}
	return &value
}
