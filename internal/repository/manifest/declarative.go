package manifest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func (r *Repository) GetSource(ctx context.Context, packageID string) (domainmanifest.Source, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT source.id, source.package_id, source.mode, COALESCE(source.repository_id, ''),
			COALESCE(source.ref_type, ''), source.ref_value, source.source_path,
			source.include_patterns, source.exclude_patterns, source.sync_policy,
			COALESCE(source.poll_interval_seconds, 0), source.auto_publish, source.auto_deploy,
			COALESCE(status.last_resolved_commit, ''), COALESCE(status.last_tree_digest, ''),
			COALESCE(status.last_canonical_digest, ''), status.last_successful_sync_at,
			COALESCE(status.last_error_code, ''), COALESCE(status.last_error_message, ''),
			source.generation, source.created_at, source.updated_at
		FROM manifest_sources source
		LEFT JOIN manifest_source_status status ON status.source_id = source.id
		WHERE source.package_id = ?
		LIMIT 1
	`, strings.TrimSpace(packageID)).Row()
	item, err := scanSource(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domainmanifest.Source{}, apperrors.ErrNotFound
	}
	return item, err
}

func (r *Repository) UpdateSource(ctx context.Context, packageID string, input domainmanifest.SourceInput) (domainmanifest.Source, error) {
	includePatterns, err := json.Marshal(input.IncludePatterns)
	if err != nil {
		return domainmanifest.Source{}, fmt.Errorf("encode manifest source include patterns: %w", err)
	}
	excludePatterns, err := json.Marshal(input.ExcludePatterns)
	if err != nil {
		return domainmanifest.Source{}, fmt.Errorf("encode manifest source exclude patterns: %w", err)
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE manifest_sources
		SET mode = ?, repository_id = NULLIF(?, ''), ref_type = NULLIF(?, ''), ref_value = ?,
			source_path = ?, include_patterns = ?::jsonb, exclude_patterns = ?::jsonb,
			sync_policy = ?, poll_interval_seconds = NULLIF(?, 0), auto_publish = ?, auto_deploy = ?,
			generation = generation + 1, updated_at = CURRENT_TIMESTAMP
		WHERE package_id = ? AND generation = ?
	`, input.Mode, input.RepositoryID, input.RefType, input.RefValue, input.Path,
		string(includePatterns), string(excludePatterns), input.SyncPolicy, input.PollIntervalSeconds,
		input.AutoPublish, input.AutoDeploy, strings.TrimSpace(packageID), input.ExpectedGeneration)
	if result.Error != nil {
		if hasSQLState(result.Error, "23503") {
			return domainmanifest.Source{}, fmt.Errorf("%w: manifest source repository does not exist", apperrors.ErrInvalidArgument)
		}
		return domainmanifest.Source{}, fmt.Errorf("update manifest source: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		if _, err := r.GetSource(ctx, packageID); err != nil {
			return domainmanifest.Source{}, err
		}
		return domainmanifest.Source{}, fmt.Errorf("%w: manifest source generation changed", apperrors.ErrConflict)
	}
	return r.GetSource(ctx, packageID)
}

type sqlStateError interface {
	SQLState() string
}

func hasSQLState(err error, state string) bool {
	var stateErr sqlStateError
	return errors.As(err, &stateErr) && stateErr.SQLState() == state
}

func scanSource(source scanner) (domainmanifest.Source, error) {
	var item domainmanifest.Source
	var includePatterns, excludePatterns []byte
	if err := source.Scan(
		&item.ID, &item.PackageID, &item.Mode, &item.RepositoryID, &item.RefType, &item.RefValue,
		&item.Path, &includePatterns, &excludePatterns, &item.SyncPolicy, &item.PollIntervalSeconds,
		&item.AutoPublish, &item.AutoDeploy, &item.LastResolvedCommit, &item.LastTreeDigest, &item.LastCanonicalDigest,
		&item.LastSuccessfulSyncAt, &item.LastErrorCode,
		&item.LastErrorMessage, &item.Generation, &item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return domainmanifest.Source{}, err
	}
	if err := json.Unmarshal(includePatterns, &item.IncludePatterns); err != nil {
		return domainmanifest.Source{}, fmt.Errorf("decode manifest source include patterns: %w", err)
	}
	if err := json.Unmarshal(excludePatterns, &item.ExcludePatterns); err != nil {
		return domainmanifest.Source{}, fmt.Errorf("decode manifest source exclude patterns: %w", err)
	}
	return item, nil
}

func (r *Repository) ListBindings(ctx context.Context, packageID string) ([]domainmanifest.EnvironmentBinding, error) {
	rows, err := r.db.WithContext(ctx).Raw(bindingSelect+` WHERE package_id = ? ORDER BY created_at, id`, strings.TrimSpace(packageID)).Rows()
	if err != nil {
		return nil, fmt.Errorf("list manifest bindings: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]domainmanifest.EnvironmentBinding, 0)
	for rows.Next() {
		item, scanErr := scanEnvironmentBinding(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetBinding(ctx context.Context, bindingID string) (domainmanifest.EnvironmentBinding, error) {
	item, err := scanEnvironmentBinding(r.db.WithContext(ctx).Raw(bindingSelect+` WHERE id = ? LIMIT 1`, strings.TrimSpace(bindingID)).Row())
	if errors.Is(err, sql.ErrNoRows) {
		return domainmanifest.EnvironmentBinding{}, apperrors.ErrNotFound
	}
	return item, err
}

func (r *Repository) CreateBinding(ctx context.Context, item domainmanifest.EnvironmentBinding) (domainmanifest.EnvironmentBinding, error) {
	overlay, err := json.Marshal(item.Overlay)
	if err != nil {
		return domainmanifest.EnvironmentBinding{}, fmt.Errorf("encode manifest binding overlay: %w", err)
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			INSERT INTO manifest_bindings (
				id, package_id, application_environment_id, environment_key, cluster_id, namespace,
				overlay, rollout_strategy_id, verification_policy_id, drift_policy, deletion_policy,
				enabled, version, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?::jsonb, ?, ?, ?, ?, ?, ?, ?, ?)
		`, item.ID, item.PackageID, item.ApplicationEnvironmentID, item.EnvironmentKey, item.ClusterID,
			item.Namespace, string(overlay), item.RolloutStrategyID, item.VerificationPolicyID,
			item.DriftPolicy, item.DeletionPolicy, item.Enabled, item.Version, item.CreatedAt, item.UpdatedAt).Error; err != nil {
			return err
		}
		return syncBindingProjection(tx, item.PackageID)
	})
	if err != nil {
		return domainmanifest.EnvironmentBinding{}, fmt.Errorf("create manifest binding: %w", err)
	}
	return r.GetBinding(ctx, item.ID)
}

func (r *Repository) UpdateBinding(ctx context.Context, item domainmanifest.EnvironmentBinding, expectedVersion int64) (domainmanifest.EnvironmentBinding, error) {
	overlay, err := json.Marshal(item.Overlay)
	if err != nil {
		return domainmanifest.EnvironmentBinding{}, fmt.Errorf("encode manifest binding overlay: %w", err)
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Exec(`
			UPDATE manifest_bindings
			SET application_environment_id = ?, environment_key = ?, cluster_id = ?, namespace = ?,
				overlay = ?::jsonb, rollout_strategy_id = ?, verification_policy_id = ?, drift_policy = ?,
				deletion_policy = ?, enabled = ?, version = version + 1, updated_at = ?
			WHERE id = ? AND version = ?
		`, item.ApplicationEnvironmentID, item.EnvironmentKey, item.ClusterID, item.Namespace, string(overlay),
			item.RolloutStrategyID, item.VerificationPolicyID, item.DriftPolicy, item.DeletionPolicy,
			item.Enabled, item.UpdatedAt, item.ID, expectedVersion)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return apperrors.ErrConflict
		}
		return syncBindingProjection(tx, item.PackageID)
	})
	if err != nil {
		if errors.Is(err, apperrors.ErrConflict) {
			if _, getErr := r.GetBinding(ctx, item.ID); getErr != nil {
				return domainmanifest.EnvironmentBinding{}, getErr
			}
			return domainmanifest.EnvironmentBinding{}, fmt.Errorf("%w: manifest binding version changed", apperrors.ErrConflict)
		}
		return domainmanifest.EnvironmentBinding{}, fmt.Errorf("update manifest binding: %w", err)
	}
	return r.GetBinding(ctx, item.ID)
}

func (r *Repository) DeleteBinding(ctx context.Context, bindingID string) error {
	existing, err := r.GetBinding(ctx, bindingID)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Exec(`DELETE FROM manifest_bindings WHERE id = ?`, existing.ID)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return apperrors.ErrNotFound
		}
		return syncBindingProjection(tx, existing.PackageID)
	})
}

const bindingSelect = `SELECT id, package_id, application_environment_id, environment_key, cluster_id, namespace,
	overlay, rollout_strategy_id, verification_policy_id, drift_policy, deletion_policy, enabled, version,
	created_at, updated_at FROM manifest_bindings`

func scanEnvironmentBinding(source scanner) (domainmanifest.EnvironmentBinding, error) {
	var item domainmanifest.EnvironmentBinding
	var overlay []byte
	if err := source.Scan(
		&item.ID, &item.PackageID, &item.ApplicationEnvironmentID, &item.EnvironmentKey,
		&item.ClusterID, &item.Namespace, &overlay, &item.RolloutStrategyID,
		&item.VerificationPolicyID, &item.DriftPolicy, &item.DeletionPolicy, &item.Enabled,
		&item.Version, &item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return domainmanifest.EnvironmentBinding{}, err
	}
	if err := json.Unmarshal(overlay, &item.Overlay); err != nil {
		return domainmanifest.EnvironmentBinding{}, fmt.Errorf("decode manifest binding overlay: %w", err)
	}
	if item.Overlay == nil {
		item.Overlay = map[string]string{}
	}
	return item, nil
}

func syncBindingProjection(tx *gorm.DB, packageID string) error {
	return tx.Exec(`
		UPDATE manifest_packages package
		SET bindings = COALESCE((
			SELECT jsonb_agg(jsonb_build_object(
				'id', binding.id,
				'applicationEnvironmentId', binding.application_environment_id,
				'environmentKey', binding.environment_key,
				'clusterId', binding.cluster_id,
				'namespace', binding.namespace,
				'overlay', binding.overlay,
				'status', 'not_deployed'
			) ORDER BY binding.created_at, binding.id)
			FROM manifest_bindings binding
			WHERE binding.package_id = package.id
		), '[]'::jsonb), updated_at = CURRENT_TIMESTAMP
		WHERE package.id = ?
	`, packageID).Error
}

func syncLegacyBindingRelations(tx *gorm.DB, packageID string, bindings []domainmanifest.Binding, updatedAt time.Time) error {
	ids := make([]string, 0, len(bindings))
	for _, binding := range bindings {
		overlay := binding.Overlay
		if overlay == nil {
			overlay = map[string]string{}
		}
		encodedOverlay, err := json.Marshal(overlay)
		if err != nil {
			return fmt.Errorf("encode legacy manifest binding overlay: %w", err)
		}
		result := tx.Exec(`
			INSERT INTO manifest_bindings (
				id, package_id, application_environment_id, environment_key, cluster_id, namespace,
				overlay, drift_policy, deletion_policy, enabled, version, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?::jsonb, 'report', 'orphan', true, 1, ?, ?)
			ON CONFLICT (id) DO UPDATE SET
				application_environment_id = EXCLUDED.application_environment_id,
				environment_key = EXCLUDED.environment_key,
				cluster_id = EXCLUDED.cluster_id,
				namespace = EXCLUDED.namespace,
				overlay = EXCLUDED.overlay,
				version = manifest_bindings.version + CASE WHEN
					(manifest_bindings.application_environment_id, manifest_bindings.environment_key,
						manifest_bindings.cluster_id, manifest_bindings.namespace, manifest_bindings.overlay)
					IS DISTINCT FROM (EXCLUDED.application_environment_id, EXCLUDED.environment_key,
						EXCLUDED.cluster_id, EXCLUDED.namespace, EXCLUDED.overlay)
					THEN 1 ELSE 0 END,
				updated_at = CASE WHEN
					(manifest_bindings.application_environment_id, manifest_bindings.environment_key,
						manifest_bindings.cluster_id, manifest_bindings.namespace, manifest_bindings.overlay)
					IS DISTINCT FROM (EXCLUDED.application_environment_id, EXCLUDED.environment_key,
						EXCLUDED.cluster_id, EXCLUDED.namespace, EXCLUDED.overlay)
					THEN EXCLUDED.updated_at ELSE manifest_bindings.updated_at END
			WHERE manifest_bindings.package_id = EXCLUDED.package_id
		`, binding.ID, packageID, binding.ApplicationEnvironmentID, binding.EnvironmentKey, binding.ClusterID,
			binding.Namespace, string(encodedOverlay), updatedAt, updatedAt)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("%w: manifest binding belongs to another package", apperrors.ErrConflict)
		}
		ids = append(ids, binding.ID)
	}
	var result *gorm.DB
	if len(ids) == 0 {
		result = tx.Exec(`DELETE FROM manifest_bindings WHERE package_id = ?`, packageID)
	} else {
		result = tx.Exec(`DELETE FROM manifest_bindings WHERE package_id = ? AND id NOT IN ?`, packageID, ids)
	}
	if result.Error != nil {
		return result.Error
	}
	return syncBindingProjection(tx, packageID)
}

func (r *Repository) ListDeployments(ctx context.Context, filter domainmanifest.DeploymentFilter) (domainmanifest.DeploymentPage, error) {
	page, pageSize := normalizeDeploymentPage(filter.Page, filter.PageSize)
	where, args, empty := deploymentWhere(filter)
	if empty {
		return domainmanifest.DeploymentPage{Items: []domainmanifest.Deployment{}, Page: page, PageSize: pageSize}, nil
	}
	var total int
	if err := r.db.WithContext(ctx).Raw(deploymentCountQuery+where, args...).Scan(&total).Error; err != nil {
		return domainmanifest.DeploymentPage{}, fmt.Errorf("count manifest deployments: %w", err)
	}
	queryArgs := append(append([]any{}, args...), pageSize, (page-1)*pageSize)
	rows, err := r.db.WithContext(ctx).Raw(deploymentSelectQuery+where+` ORDER BY deployment.updated_at DESC LIMIT ? OFFSET ?`, queryArgs...).Rows()
	if err != nil {
		return domainmanifest.DeploymentPage{}, fmt.Errorf("list manifest deployments: %w", err)
	}
	items := make([]domainmanifest.Deployment, 0, pageSize)
	for rows.Next() {
		item, scanErr := scanDeployment(rows)
		if scanErr != nil {
			_ = rows.Close()
			return domainmanifest.DeploymentPage{}, scanErr
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return domainmanifest.DeploymentPage{}, err
	}
	if err := rows.Close(); err != nil {
		return domainmanifest.DeploymentPage{}, err
	}
	for index := range items {
		if err := r.loadDeploymentDetails(ctx, &items[index]); err != nil {
			return domainmanifest.DeploymentPage{}, err
		}
	}
	return domainmanifest.DeploymentPage{Items: items, Total: total, Page: page, PageSize: pageSize}, nil
}

func (r *Repository) GetDeployment(ctx context.Context, deploymentID string) (domainmanifest.Deployment, error) {
	item, err := scanDeployment(r.db.WithContext(ctx).Raw(deploymentSelectQuery+` WHERE deployment.id = ? LIMIT 1`, strings.TrimSpace(deploymentID)).Row())
	if errors.Is(err, sql.ErrNoRows) {
		return domainmanifest.Deployment{}, apperrors.ErrNotFound
	}
	if err != nil {
		return domainmanifest.Deployment{}, err
	}
	if err := r.loadDeploymentDetails(ctx, &item); err != nil {
		return domainmanifest.Deployment{}, err
	}
	return item, nil
}

const deploymentJoins = ` FROM manifest_deployments deployment
	JOIN manifest_bindings binding ON binding.id = deployment.binding_id
	JOIN manifest_packages package ON package.id = deployment.package_id
	JOIN manifest_sources source ON source.package_id = package.id
	LEFT JOIN manifest_deployment_status status ON status.deployment_id = deployment.id`

const deploymentSelectQuery = `SELECT deployment.id, deployment.package_id, deployment.binding_id,
	deployment.generation, deployment.desired_revision, deployment.desired_digest,
	deployment.reconcile_policy, deployment.drift_policy, deployment.deletion_policy,
	COALESCE(status.observed_generation, 0), COALESCE(status.applied_revision, 0),
	COALESCE(status.applied_digest, ''), COALESCE(status.last_known_good_revision, 0),
	COALESCE(status.phase, 'pending'), status.last_reconciled_at,
	COALESCE(status.last_execution_task_id, ''), COALESCE(status.drift, '{}'::jsonb),
	COALESCE(status.last_error_code, ''), COALESCE(status.last_error_message, ''),
	deployment.created_at, deployment.updated_at` + deploymentJoins

const deploymentCountQuery = `SELECT COUNT(*)` + deploymentJoins

func deploymentWhere(filter domainmanifest.DeploymentFilter) (string, []any, bool) {
	where := ` WHERE package.archived_at IS NULL`
	args := make([]any, 0, 8)
	if packageID := strings.TrimSpace(filter.PackageID); packageID != "" {
		where += ` AND deployment.package_id = ?`
		args = append(args, packageID)
	}
	if applicationID := strings.TrimSpace(filter.ApplicationID); applicationID != "" {
		where += ` AND package.application_id = ?`
		args = append(args, applicationID)
	} else if filter.ApplicationIDs != nil {
		if len(filter.ApplicationIDs) == 0 {
			return "", nil, true
		}
		where += ` AND package.application_id IN ?`
		args = append(args, filter.ApplicationIDs)
	}
	for _, item := range []struct {
		value  string
		clause string
	}{
		{filter.ApplicationEnvironmentID, ` AND binding.application_environment_id = ?`},
		{filter.ClusterID, ` AND binding.cluster_id = ?`},
		{filter.Namespace, ` AND binding.namespace = ?`},
		{filter.SourceMode, ` AND source.mode = ?`},
		{filter.Phase, ` AND COALESCE(status.phase, 'pending') = ?`},
	} {
		if value := strings.TrimSpace(item.value); value != "" {
			where += item.clause
			args = append(args, value)
		}
	}
	return where, args, false
}

func normalizeDeploymentPage(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	return page, pageSize
}

func scanDeployment(source scanner) (domainmanifest.Deployment, error) {
	var item domainmanifest.Deployment
	var drift []byte
	if err := source.Scan(
		&item.ID, &item.PackageID, &item.BindingID, &item.Generation,
		&item.Spec.DesiredRevision, &item.Spec.DesiredDigest, &item.Spec.ReconcilePolicy,
		&item.Spec.DriftPolicy, &item.Spec.DeletionPolicy, &item.Status.ObservedGeneration,
		&item.Status.AppliedRevision, &item.Status.AppliedDigest, &item.Status.LastKnownGoodRevision,
		&item.Status.Phase, &item.Status.LastReconciledAt, &item.Status.LastExecutionTaskID, &drift,
		&item.Status.LastErrorCode,
		&item.Status.LastErrorMessage, &item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return domainmanifest.Deployment{}, err
	}
	item.Status.Conditions = []domainmanifest.Condition{}
	item.Status.Inventory = []domainmanifest.ResourceInventory{}
	if len(drift) > 0 && string(drift) != "{}" {
		var report domainmanifest.DriftReport
		if err := json.Unmarshal(drift, &report); err != nil {
			return domainmanifest.Deployment{}, fmt.Errorf("decode manifest deployment drift: %w", err)
		}
		item.Status.Drift = &report
	}
	return item, nil
}

func (r *Repository) loadDeploymentDetails(ctx context.Context, item *domainmanifest.Deployment) error {
	conditions, err := r.listConditions(ctx, item.ID)
	if err != nil {
		return err
	}
	inventory, err := r.listInventory(ctx, item.ID, item.Generation)
	if err != nil {
		return err
	}
	item.Status.Conditions = conditions
	item.Status.Inventory = inventory
	return nil
}

func (r *Repository) listConditions(ctx context.Context, deploymentID string) ([]domainmanifest.Condition, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT condition_type, status, reason, message, observed_generation, last_transition_at, evidence_refs
		FROM manifest_deployment_conditions WHERE deployment_id = ? ORDER BY condition_type
	`, deploymentID).Rows()
	if err != nil {
		return nil, fmt.Errorf("list manifest deployment conditions: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]domainmanifest.Condition, 0)
	for rows.Next() {
		var item domainmanifest.Condition
		var evidenceRefs []byte
		if err := rows.Scan(&item.Type, &item.Status, &item.Reason, &item.Message, &item.ObservedGeneration, &item.LastTransitionAt, &evidenceRefs); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(evidenceRefs, &item.EvidenceRefs); err != nil {
			return nil, fmt.Errorf("decode manifest condition evidence refs: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) listInventory(ctx context.Context, deploymentID string, generation int64) ([]domainmanifest.ResourceInventory, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT deployment_id, generation, api_version, kind, namespace, name, uid, resource_version,
			desired_object_digest, observed_object_digest, health, last_observed_at
		FROM manifest_resource_inventory
		WHERE deployment_id = ? AND generation = ?
		ORDER BY api_version, kind, namespace, name
	`, deploymentID, generation).Rows()
	if err != nil {
		return nil, fmt.Errorf("list manifest resource inventory: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]domainmanifest.ResourceInventory, 0)
	for rows.Next() {
		var item domainmanifest.ResourceInventory
		if err := rows.Scan(
			&item.DeploymentID, &item.Generation, &item.APIVersion, &item.Kind, &item.Namespace,
			&item.Name, &item.UID, &item.ResourceVersion, &item.DesiredObjectDigest,
			&item.ObservedObjectDigest, &item.Health, &item.LastObservedAt,
		); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}
