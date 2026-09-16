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
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	repodelivery "github.com/opensoha/soha/internal/repository/delivery"
	repoworkflow "github.com/opensoha/soha/internal/repository/workflow"
	"gorm.io/gorm"
)

func (r *Repository) SetDesiredRevision(ctx context.Context, next domainmanifest.Deployment, expectedGeneration int64) (domainmanifest.Deployment, error) {
	snapshot, err := json.Marshal(next.Spec.DeliverySnapshot)
	if err != nil {
		return domainmanifest.Deployment{}, fmt.Errorf("encode delivery snapshot: %w", err)
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if next.Spec.DeliverySnapshot != nil {
			if err := lockBatchDeployment(ctx, tx, *next.Spec.DeliverySnapshot); err != nil {
				return err
			}
		}
		clusterID, err := lockManifestTarget(ctx, tx, next.BindingID)
		if err != nil {
			return err
		}
		if next.Spec.DeliverySnapshot != nil {
			if err := checkManifestResourceOwners(tx, clusterID, next.BindingID, next.Spec.DeliverySnapshot.Documents, next.Spec.DeliverySnapshot.GitOpsDocuments); err != nil {
				return err
			}
			if err := lockDeliverySnapshot(tx, *next.Spec.DeliverySnapshot); err != nil {
				return err
			}
		}
		var currentGeneration int64
		err = tx.Raw(`SELECT generation FROM manifest_deployments WHERE binding_id = ? FOR UPDATE`, next.BindingID).Row().Scan(&currentGeneration)
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
					deletion_policy = ?, delivery_snapshot = ?::jsonb, generation = generation + 1, updated_at = ?
				WHERE binding_id = ? AND generation = ?
			`, next.Spec.DesiredRevision, next.Spec.DesiredDigest, next.Spec.ReconcilePolicy,
				next.Spec.DriftPolicy, next.Spec.DeletionPolicy, string(snapshot), next.UpdatedAt, next.BindingID, expectedGeneration)
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
			return domainmanifest.Deployment{}, fmt.Errorf("manifest desired revision conflict: %w", err)
		}
		return domainmanifest.Deployment{}, fmt.Errorf("set manifest desired revision: %w", err)
	}
	return r.GetDeploymentByBinding(ctx, next.BindingID)
}

func lockBatchDeployment(ctx context.Context, tx *gorm.DB, snapshot domainmanifest.DeliverySnapshot) error {
	plan, err := repodelivery.New(tx).GetDeliveryPlan(ctx, snapshot.DeliveryPlanID)
	node, worker := domainworkflow.NodeExecutionFrom(ctx)
	if !worker && (errors.Is(err, apperrors.ErrNotFound) || err == nil && plan.Source != domainworkflow.ScopeDeliveryBatch) {
		return nil
	}
	if err != nil {
		return err
	}
	if !worker || node.Stage != "deploy" || plan.Source != domainworkflow.ScopeDeliveryBatch || plan.Status != domaindelivery.DeliveryPlanStatusConfirming || plan.Impact["workflowRunId"] != node.RunID || plan.Impact["workflowTargetId"] != node.TargetID {
		return fmt.Errorf("%w: batch deployment requires its current delivery worker", apperrors.ErrConflict)
	}
	return repoworkflow.LockDeliveryNode(ctx, tx, plan.ApplicationID, plan.ApplicationEnvironmentID)
}

func insertDeployment(tx *gorm.DB, item domainmanifest.Deployment) error {
	snapshot, err := json.Marshal(item.Spec.DeliverySnapshot)
	if err != nil {
		return fmt.Errorf("encode delivery snapshot: %w", err)
	}
	if err := tx.Exec(`
		INSERT INTO manifest_deployments (
			id, package_id, binding_id, generation, desired_revision, desired_digest,
			reconcile_policy, drift_policy, deletion_policy, delivery_snapshot, created_at, updated_at
		) VALUES (?, ?, ?, 1, ?, ?, ?, ?, ?, ?::jsonb, ?, ?)
	`, item.ID, item.PackageID, item.BindingID, item.Spec.DesiredRevision, item.Spec.DesiredDigest,
		item.Spec.ReconcilePolicy, item.Spec.DriftPolicy, item.Spec.DeletionPolicy, string(snapshot),
		item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return err
	}
	return tx.Exec(`
		INSERT INTO manifest_deployment_status (deployment_id, observed_generation, phase)
		VALUES (?, 0, 'pending')
	`, item.ID).Error
}

func lockDeliverySnapshot(tx *gorm.DB, snapshot domainmanifest.DeliverySnapshot) error {
	var id string
	err := tx.Raw(`
		SELECT binding.id FROM manifest_bindings binding
		JOIN manifest_packages package ON package.id = binding.package_id
		WHERE binding.id = ? AND binding.version = ? AND binding.enabled = TRUE
		  AND binding.package_id = ? AND binding.application_environment_id = ?
		  AND binding.cluster_id = ? AND binding.namespace = ?
		  AND package.updated_at = ? AND package.archived_at IS NULL
		FOR SHARE OF package, binding
	`, snapshot.BindingID, snapshot.BindingVersion, snapshot.PackageID, snapshot.ApplicationEnvironmentID,
		snapshot.ClusterID, snapshot.Namespace, snapshot.PackageUpdatedAt).Row().Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("%w: Manifest inputs changed", apperrors.ErrConflict)
	}
	return err
}

func (r *Repository) GetRevisionSourceCommit(ctx context.Context, packageID string, revision int) (string, error) {
	var commit string
	err := r.db.WithContext(ctx).Raw(`SELECT resolved_commit FROM manifest_sync_runs WHERE package_id = ? AND revision = ? AND status = 'succeeded' ORDER BY created_at DESC LIMIT 1`, packageID, revision).Row().Scan(&commit)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return commit, err
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
	created := false
	var taskID string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, ok := domainworkflow.NodeExecutionFrom(ctx); ok {
			if err := repoworkflow.LockDeliveryNode(ctx, tx, task.ApplicationID, task.ApplicationEnvironmentID); err != nil {
				return err
			}
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, "manifest-operation:"+run.IdempotencyKey).Error; err != nil {
			return err
		}
		err := tx.Raw(`SELECT execution_task_id FROM manifest_operation_runs WHERE idempotency_key = ?`, run.IdempotencyKey).Row().Scan(&taskID)
		if err == nil {
			if node, ok := domainworkflow.NodeExecutionFrom(ctx); ok && node.ResourceID("task") != taskID {
				return fmt.Errorf("%w: manifest operation belongs to another delivery node", apperrors.ErrConflict)
			}
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if err := lockManifestOperation(ctx, tx, run, task); err != nil {
			return err
		}
		stored, err := repodelivery.New(tx).CreateExecutionTask(ctx, task)
		if err != nil {
			return err
		}
		taskID = stored.ID
		if err := tx.Exec(`
			INSERT INTO manifest_operation_runs (
				id, package_id, binding_id, deployment_id, generation, action,
				idempotency_key, execution_task_id, created_at
			) VALUES (?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, ?, ?, ?)
		`, run.ID, run.PackageID, run.BindingID, run.DeploymentID, run.Generation,
			run.Action, run.IdempotencyKey, taskID, run.CreatedAt).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return "", false, fmt.Errorf("create manifest operation task: %w", err)
	}
	return taskID, created, nil
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
		finalizers := item.Finalizers
		if finalizers == nil {
			finalizers = []string{}
		}
		encodedFinalizers, err := json.Marshal(finalizers)
		if err != nil {
			return err
		}
		if err := tx.Exec(`
			INSERT INTO manifest_resource_inventory (
				deployment_id, generation, api_version, kind, namespace, name, uid,
				resource_version, desired_object_digest, observed_object_digest, health, last_observed_at,
				resource_generation, observed_resource_generation, deleting_at, finalizers
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?::jsonb)
		`, deploymentID, generation, item.APIVersion, item.Kind, item.Namespace, item.Name,
			item.UID, item.ResourceVersion, item.DesiredObjectDigest, item.ObservedObjectDigest,
			item.Health, item.LastObservedAt, item.ResourceGeneration, item.ObservedResourceGeneration,
			item.DeletingAt, string(encodedFinalizers)).Error; err != nil {
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
			AND (status.last_reconciled_at IS NULL OR status.last_reconciled_at <= NOW() -
				CASE WHEN status.phase = 'reconciling' THEN INTERVAL '10 seconds' ELSE INTERVAL '60 seconds' END)
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
