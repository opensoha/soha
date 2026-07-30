package manifest

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func (r *Repository) SetDesiredRevision(ctx context.Context, next domainmanifest.Deployment, expectedGeneration int64) (domainmanifest.Deployment, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var currentGeneration int64
		err := tx.Raw(`SELECT generation FROM manifest_deployments WHERE binding_id = ? FOR UPDATE`, next.BindingID).Row().Scan(&currentGeneration)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			if expectedGeneration != 0 {
				return apperrors.ErrConflict
			}
			return insertDeployment(tx, next)
		case err != nil:
			return err
		case currentGeneration != expectedGeneration:
			return apperrors.ErrConflict
		default:
			result := tx.Exec(`
				UPDATE manifest_deployments
				SET desired_revision = ?, desired_digest = ?, reconcile_policy = ?, drift_policy = ?,
					deletion_policy = ?, generation = generation + 1, updated_at = ?
				WHERE binding_id = ? AND generation = ?
			`, next.Spec.DesiredRevision, next.Spec.DesiredDigest, next.Spec.ReconcilePolicy,
				next.Spec.DriftPolicy, next.Spec.DeletionPolicy, next.UpdatedAt, next.BindingID, expectedGeneration)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return apperrors.ErrConflict
			}
			return tx.Exec(`
				UPDATE manifest_deployment_status
				SET phase = 'pending', last_error_code = '', last_error_message = ''
				WHERE deployment_id = (SELECT id FROM manifest_deployments WHERE binding_id = ?)
			`, next.BindingID).Error
		}
	})
	if err != nil {
		if errors.Is(err, apperrors.ErrConflict) {
			return domainmanifest.Deployment{}, fmt.Errorf("%w: manifest deployment generation changed", apperrors.ErrConflict)
		}
		return domainmanifest.Deployment{}, fmt.Errorf("set manifest desired revision: %w", err)
	}
	return r.GetDeploymentByBinding(ctx, next.BindingID)
}

func insertDeployment(tx *gorm.DB, item domainmanifest.Deployment) error {
	if err := tx.Exec(`
		INSERT INTO manifest_deployments (
			id, package_id, binding_id, generation, desired_revision, desired_digest,
			reconcile_policy, drift_policy, deletion_policy, created_at, updated_at
		) VALUES (?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.PackageID, item.BindingID, item.Spec.DesiredRevision, item.Spec.DesiredDigest,
		item.Spec.ReconcilePolicy, item.Spec.DriftPolicy, item.Spec.DeletionPolicy,
		item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return err
	}
	return tx.Exec(`
		INSERT INTO manifest_deployment_status (deployment_id, observed_generation, phase)
		VALUES (?, 0, 'pending')
	`, item.ID).Error
}

func (r *Repository) GetDeploymentByBinding(ctx context.Context, bindingID string) (domainmanifest.Deployment, error) {
	item, err := scanDeployment(r.db.WithContext(ctx).Raw(deploymentSelectQuery+` WHERE deployment.binding_id = ? LIMIT 1`, strings.TrimSpace(bindingID)).Row())
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

func (r *Repository) CreateOperationTask(ctx context.Context, run domainmanifest.OperationRun, task domaindelivery.ExecutionTask) (string, bool, error) {
	payload, err := json.Marshal(task.Payload)
	if err != nil {
		return "", false, fmt.Errorf("encode manifest task payload: %w", err)
	}
	result, err := json.Marshal(task.Result)
	if err != nil {
		return "", false, fmt.Errorf("encode manifest task result: %w", err)
	}
	created := false
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`
			INSERT INTO execution_tasks (
				id, release_bundle_id, application_id, application_environment_id,
				task_kind, provider_kind, target_kind, status, queue_key, lock_key,
				max_retries, attempt_count, timeout_seconds, callback_token,
				payload, result, created_at, updated_at
			) VALUES (?, NULL, ?, NULLIF(?, ''), ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?::jsonb, ?, ?)
		`, task.ID, task.ApplicationID, task.ApplicationEnvironmentID, task.TaskKind,
			task.ProviderKind, task.TargetKind, task.Status, task.QueueKey, task.LockKey,
			task.MaxRetries, task.AttemptCount, task.TimeoutSeconds, task.CallbackToken,
			string(payload), string(result), task.CreatedAt, task.UpdatedAt).Error; err != nil {
			return err
		}
		insert := tx.Exec(`
			INSERT INTO manifest_operation_runs (
				id, package_id, binding_id, deployment_id, generation, action,
				idempotency_key, execution_task_id, created_at
			) VALUES (?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?, ?)
			ON CONFLICT (idempotency_key) DO NOTHING
		`, run.ID, run.PackageID, run.BindingID, run.DeploymentID, run.Generation,
			run.Action, run.IdempotencyKey, run.ExecutionTaskID, run.CreatedAt)
		if insert.Error != nil {
			return insert.Error
		}
		if insert.RowsAffected == 0 {
			return tx.Exec(`DELETE FROM execution_tasks WHERE id = ?`, task.ID).Error
		}
		created = true
		return nil
	})
	if err != nil {
		return "", false, fmt.Errorf("create manifest operation task: %w", err)
	}
	if created {
		return task.ID, true, nil
	}
	var existingTaskID string
	if err := r.db.WithContext(ctx).Raw(`SELECT execution_task_id FROM manifest_operation_runs WHERE idempotency_key = ?`, run.IdempotencyKey).Row().Scan(&existingTaskID); err != nil {
		return "", false, fmt.Errorf("load existing manifest operation task: %w", err)
	}
	return existingTaskID, false, nil
}

func (r *Repository) UpdateDeploymentStatus(ctx context.Context, deploymentID string, generation int64, status domainmanifest.DeploymentStatus) error {
	drift, err := json.Marshal(status.Drift)
	if err != nil {
		return fmt.Errorf("encode manifest drift report: %w", err)
	}
	if status.Drift == nil {
		drift = []byte(`{}`)
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Exec(`
			UPDATE manifest_deployment_status status
			SET observed_generation = ?, applied_revision = NULLIF(?, 0), applied_digest = ?,
				last_known_good_revision = NULLIF(?, 0), phase = ?, last_reconciled_at = ?,
				last_execution_task_id = NULLIF(?, ''), drift = ?::jsonb,
				last_error_code = ?, last_error_message = ?
			FROM manifest_deployments deployment
			WHERE status.deployment_id = deployment.id AND deployment.id = ? AND deployment.generation = ?
		`, status.ObservedGeneration, status.AppliedRevision, status.AppliedDigest,
			status.LastKnownGoodRevision, status.Phase, status.LastReconciledAt,
			status.LastExecutionTaskID, string(drift), status.LastErrorCode,
			status.LastErrorMessage, deploymentID, generation)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return apperrors.ErrConflict
		}
		if err := replaceConditions(tx, deploymentID, status.Conditions); err != nil {
			return err
		}
		return replaceInventory(tx, deploymentID, generation, status.Inventory)
	})
}

func replaceConditions(tx *gorm.DB, deploymentID string, items []domainmanifest.Condition) error {
	if err := tx.Exec(`DELETE FROM manifest_deployment_conditions WHERE deployment_id = ?`, deploymentID).Error; err != nil {
		return err
	}
	for _, item := range items {
		evidence, err := json.Marshal(item.EvidenceRefs)
		if err != nil {
			return err
		}
		if err := tx.Exec(`
			INSERT INTO manifest_deployment_conditions (
				deployment_id, condition_type, status, reason, message,
				observed_generation, last_transition_at, evidence_refs
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?::jsonb)
		`, deploymentID, item.Type, item.Status, item.Reason, item.Message,
			item.ObservedGeneration, item.LastTransitionAt, string(evidence)).Error; err != nil {
			return err
		}
	}
	return nil
}

func replaceInventory(tx *gorm.DB, deploymentID string, generation int64, items []domainmanifest.ResourceInventory) error {
	if err := tx.Exec(`DELETE FROM manifest_resource_inventory WHERE deployment_id = ? AND generation = ?`, deploymentID, generation).Error; err != nil {
		return err
	}
	for _, item := range items {
		if err := tx.Exec(`
			INSERT INTO manifest_resource_inventory (
				deployment_id, generation, api_version, kind, namespace, name, uid,
				resource_version, desired_object_digest, observed_object_digest, health, last_observed_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, deploymentID, generation, item.APIVersion, item.Kind, item.Namespace, item.Name,
			item.UID, item.ResourceVersion, item.DesiredObjectDigest, item.ObservedObjectDigest,
			item.Health, item.LastObservedAt).Error; err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ListContinuousDeployments(ctx context.Context, limit int) ([]domainmanifest.Deployment, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT deployment.id
		FROM manifest_deployments deployment
		JOIN manifest_deployment_status status ON status.deployment_id = deployment.id
		WHERE deployment.reconcile_policy = 'continuous'
			AND (status.last_reconciled_at IS NULL OR status.last_reconciled_at <= NOW() - INTERVAL '60 seconds')
			AND NOT EXISTS (
				SELECT 1 FROM manifest_operation_runs run
				JOIN execution_tasks task ON task.id = run.execution_task_id
				WHERE run.deployment_id = deployment.id AND run.generation = deployment.generation
					AND run.action = 'observe' AND task.status IN ('queued', 'dispatching', 'running')
			)
		ORDER BY COALESCE(status.last_reconciled_at, deployment.created_at) ASC LIMIT ?
	`, limit).Rows()
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	items := make([]domainmanifest.Deployment, 0, len(ids))
	for _, id := range ids {
		item, err := r.GetDeployment(ctx, id)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) ApplyAdoptedFiles(ctx context.Context, deploymentID string, generation int64, files []domainmanifest.File, actor string) error {
	encoded, err := json.Marshal(files)
	if err != nil {
		return err
	}
	result := r.db.WithContext(ctx).Exec(`
		UPDATE manifest_packages package
		SET files = ?::jsonb, status = 'draft', updated_by = ?, updated_at = CURRENT_TIMESTAMP
		FROM manifest_deployments deployment
		WHERE deployment.package_id = package.id AND deployment.id = ? AND deployment.generation = ?
	`, string(encoded), firstNonEmpty(actor, "system:manifest-adopt"), deploymentID, generation)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return apperrors.ErrConflict
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
