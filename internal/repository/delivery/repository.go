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
	domaindelivery "github.com/kubecrux/kubecrux/internal/domain/delivery"
	"gorm.io/gorm"
)

var ErrNotFound = errors.New("delivery record not found")

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
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, execution_task_id, release_bundle_id, application_id, application_environment_id, artifact_kind, name, ref, digest, path, status, size_bytes, metadata, created_at, updated_at
		FROM execution_artifacts
		WHERE execution_task_id = ?
		ORDER BY created_at ASC
	`, strings.TrimSpace(taskID)).Rows()
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

func (r *Repository) ListExecutionArtifactsByBundle(ctx context.Context, bundleID string) ([]domaindelivery.ExecutionArtifact, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, execution_task_id, release_bundle_id, application_id, application_environment_id, artifact_kind, name, ref, digest, path, status, size_bytes, metadata, created_at, updated_at
		FROM execution_artifacts
		WHERE release_bundle_id = ?
		ORDER BY created_at ASC
	`, strings.TrimSpace(bundleID)).Rows()
	if err != nil {
		return nil, fmt.Errorf("query execution artifacts by bundle: %w", err)
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
	payload, err := json.Marshal(item.Metadata)
	if err != nil {
		return domaindelivery.ExecutionArtifact{}, fmt.Errorf("marshal execution artifact metadata: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO execution_artifacts (id, execution_task_id, release_bundle_id, application_id, application_environment_id, artifact_kind, name, ref, digest, path, status, size_bytes, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (id) DO UPDATE SET
			execution_task_id = EXCLUDED.execution_task_id,
			release_bundle_id = EXCLUDED.release_bundle_id,
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
			updated_at = EXCLUDED.updated_at
	`, item.ID, nullableString(item.ExecutionTaskID), nullableString(item.ReleaseBundleID), nullableString(item.ApplicationID), nullableString(item.ApplicationEnvironmentID), item.Kind, nullableString(item.Name), nullableString(item.Ref), nullableString(item.Digest), nullableString(item.Path), nullableString(item.Status), item.SizeBytes, string(payload), time.Now().UTC(), time.Now().UTC()).Error; err != nil {
		return domaindelivery.ExecutionArtifact{}, fmt.Errorf("upsert execution artifact: %w", err)
	}
	return item, nil
}

func (r *Repository) ListApprovalPolicies(ctx context.Context) ([]domaindelivery.ApprovalPolicy, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, policy_key, name, description, mode, required_approvals, sla_minutes, approver_roles, change_window, enabled, metadata, created_at, updated_at
		FROM approval_policies
		ORDER BY created_at DESC
	`).Rows()
	if err != nil {
		return nil, fmt.Errorf("query approval policies: %w", err)
	}
	defer rows.Close()

	items := make([]domaindelivery.ApprovalPolicy, 0)
	for rows.Next() {
		item, scanErr := scanApprovalPolicy(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetApprovalPolicy(ctx context.Context, id string) (domaindelivery.ApprovalPolicy, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, policy_key, name, description, mode, required_approvals, sla_minutes, approver_roles, change_window, enabled, metadata, created_at, updated_at
		FROM approval_policies
		WHERE id = ?
		LIMIT 1
	`, strings.TrimSpace(id)).Row()
	return scanApprovalPolicyRow(row)
}

func (r *Repository) CreateApprovalPolicy(ctx context.Context, input domaindelivery.ApprovalPolicyInput) (domaindelivery.ApprovalPolicy, error) {
	item := normalizeApprovalPolicyInput(input)
	roles, err := json.Marshal(item.ApproverRoles)
	if err != nil {
		return domaindelivery.ApprovalPolicy{}, fmt.Errorf("marshal approval policy approver roles: %w", err)
	}
	window, err := json.Marshal(item.ChangeWindow)
	if err != nil {
		return domaindelivery.ApprovalPolicy{}, fmt.Errorf("marshal approval policy change window: %w", err)
	}
	metadata, err := json.Marshal(item.Metadata)
	if err != nil {
		return domaindelivery.ApprovalPolicy{}, fmt.Errorf("marshal approval policy metadata: %w", err)
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO approval_policies (id, policy_key, name, description, mode, required_approvals, sla_minutes, approver_roles, change_window, enabled, metadata, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Key, item.Name, nullableString(item.Description), item.Mode, item.RequiredApprovals, item.SLAMinutes, string(roles), string(window), item.Enabled, string(metadata), item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domaindelivery.ApprovalPolicy{}, fmt.Errorf("create approval policy: %w", err)
	}
	return item, nil
}

func (r *Repository) UpdateApprovalPolicy(ctx context.Context, id string, input domaindelivery.ApprovalPolicyInput) (domaindelivery.ApprovalPolicy, error) {
	item := normalizeApprovalPolicyInput(input)
	item.ID = strings.TrimSpace(id)
	roles, err := json.Marshal(item.ApproverRoles)
	if err != nil {
		return domaindelivery.ApprovalPolicy{}, fmt.Errorf("marshal approval policy approver roles: %w", err)
	}
	window, err := json.Marshal(item.ChangeWindow)
	if err != nil {
		return domaindelivery.ApprovalPolicy{}, fmt.Errorf("marshal approval policy change window: %w", err)
	}
	metadata, err := json.Marshal(item.Metadata)
	if err != nil {
		return domaindelivery.ApprovalPolicy{}, fmt.Errorf("marshal approval policy metadata: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE approval_policies
		SET policy_key = ?, name = ?, description = ?, mode = ?, required_approvals = ?, sla_minutes = ?, approver_roles = ?, change_window = ?, enabled = ?, metadata = ?, updated_at = ?
		WHERE id = ?
	`, item.Key, item.Name, nullableString(item.Description), item.Mode, item.RequiredApprovals, item.SLAMinutes, string(roles), string(window), item.Enabled, string(metadata), item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domaindelivery.ApprovalPolicy{}, fmt.Errorf("update approval policy: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return domaindelivery.ApprovalPolicy{}, ErrNotFound
	}
	item.CreatedAt = fetchCreatedAt(ctx, r.db, "approval_policies", item.ID)
	return item, nil
}

func (r *Repository) DeleteApprovalPolicy(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM approval_policies WHERE id = ?`, strings.TrimSpace(id))
	if result.Error != nil {
		return fmt.Errorf("delete approval policy: %w", result.Error)
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
	var applicationID sql.NullString
	var applicationEnvironmentID sql.NullString
	var name sql.NullString
	var ref sql.NullString
	var digest sql.NullString
	var path sql.NullString
	var status sql.NullString
	var metadata []byte
	var updatedAt time.Time
	if err := rows.Scan(&item.ID, &executionTaskID, &releaseBundleID, &applicationID, &applicationEnvironmentID, &item.Kind, &name, &ref, &digest, &path, &status, &item.SizeBytes, &metadata, &item.ModifiedAt, &updatedAt); err != nil {
		return domaindelivery.ExecutionArtifact{}, fmt.Errorf("scan execution artifact: %w", err)
	}
	item.ExecutionTaskID = executionTaskID.String
	item.ReleaseBundleID = releaseBundleID.String
	item.ApplicationID = applicationID.String
	item.ApplicationEnvironmentID = applicationEnvironmentID.String
	item.Name = name.String
	item.Ref = ref.String
	item.Digest = digest.String
	item.Path = path.String
	item.Status = status.String
	_ = json.Unmarshal(metadata, &item.Metadata)
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func scanApprovalPolicy(rows *sql.Rows) (domaindelivery.ApprovalPolicy, error) {
	var item domaindelivery.ApprovalPolicy
	var description sql.NullString
	var roles []byte
	var changeWindow []byte
	var metadata []byte
	if err := rows.Scan(&item.ID, &item.Key, &item.Name, &description, &item.Mode, &item.RequiredApprovals, &item.SLAMinutes, &roles, &changeWindow, &item.Enabled, &metadata, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return domaindelivery.ApprovalPolicy{}, fmt.Errorf("scan approval policy: %w", err)
	}
	item.Description = description.String
	_ = json.Unmarshal(roles, &item.ApproverRoles)
	_ = json.Unmarshal(changeWindow, &item.ChangeWindow)
	_ = json.Unmarshal(metadata, &item.Metadata)
	if item.ChangeWindow == nil {
		item.ChangeWindow = map[string]any{}
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func scanApprovalPolicyRow(row *sql.Row) (domaindelivery.ApprovalPolicy, error) {
	var item domaindelivery.ApprovalPolicy
	var description sql.NullString
	var roles []byte
	var changeWindow []byte
	var metadata []byte
	if err := row.Scan(&item.ID, &item.Key, &item.Name, &description, &item.Mode, &item.RequiredApprovals, &item.SLAMinutes, &roles, &changeWindow, &item.Enabled, &metadata, &item.CreatedAt, &item.UpdatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domaindelivery.ApprovalPolicy{}, ErrNotFound
		}
		return domaindelivery.ApprovalPolicy{}, fmt.Errorf("scan approval policy row: %w", err)
	}
	item.Description = description.String
	_ = json.Unmarshal(roles, &item.ApproverRoles)
	_ = json.Unmarshal(changeWindow, &item.ChangeWindow)
	_ = json.Unmarshal(metadata, &item.Metadata)
	if item.ChangeWindow == nil {
		item.ChangeWindow = map[string]any{}
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func normalizeApprovalPolicyInput(input domaindelivery.ApprovalPolicyInput) domaindelivery.ApprovalPolicy {
	now := time.Now().UTC()
	id := strings.TrimSpace(input.ID)
	if id == "" {
		id = uuid.NewString()
	}
	if input.ChangeWindow == nil {
		input.ChangeWindow = map[string]any{}
	}
	if input.Metadata == nil {
		input.Metadata = map[string]any{}
	}
	mode := strings.TrimSpace(input.Mode)
	if mode == "" {
		mode = "single"
	}
	required := input.RequiredApprovals
	if required <= 0 {
		required = 1
	}
	sla := input.SLAMinutes
	if sla <= 0 {
		sla = 60
	}
	return domaindelivery.ApprovalPolicy{
		ID:                id,
		Key:               strings.TrimSpace(input.Key),
		Name:              strings.TrimSpace(input.Name),
		Description:       strings.TrimSpace(input.Description),
		Mode:              mode,
		RequiredApprovals: required,
		SLAMinutes:        sla,
		ApproverRoles:     input.ApproverRoles,
		ChangeWindow:      input.ChangeWindow,
		Enabled:           input.Enabled,
		Metadata:          input.Metadata,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
}

func nullableString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.TrimSpace(value)
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
		Kind:      strings.TrimSpace(fmt.Sprint(raw["kind"])),
		Name:      strings.TrimSpace(fmt.Sprint(raw["name"])),
		Ref:       strings.TrimSpace(fmt.Sprint(raw["ref"])),
		Digest:    strings.TrimSpace(fmt.Sprint(raw["digest"])),
		Path:      strings.TrimSpace(fmt.Sprint(raw["path"])),
		Status:    strings.TrimSpace(fmt.Sprint(raw["status"])),
		SizeBytes: toInt64(raw["sizeBytes"]),
		Metadata:  map[string]any{},
	}
	if modifiedAt := parseRFC3339(raw["modifiedAt"]); modifiedAt != nil {
		item.ModifiedAt = modifiedAt
	}
	for key, value := range raw {
		switch key {
		case "kind", "name", "ref", "digest", "path", "status", "sizeBytes", "modifiedAt":
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
