package manifest

import (
	"context"
	"encoding/json"
	"fmt"

	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	repodelivery "github.com/opensoha/soha/internal/repository/delivery"
	"gorm.io/gorm"
)

// Durable deployment intent and operation tasks retain ownership through restart
// and cancel acknowledgement. Approval/preflight do not acquire execution ownership.
func lockManifestTarget(ctx context.Context, tx *gorm.DB, bindingID string) (string, error) {
	// Package updates lock package before its bindings; keep that order here.
	var packageID string
	if err := tx.Raw(`SELECT package_id FROM manifest_bindings WHERE id = ?`, bindingID).Row().Scan(&packageID); err != nil {
		return "", err
	}
	if err := tx.Raw(`SELECT id FROM manifest_packages WHERE id = ? FOR SHARE`, packageID).Row().Scan(&packageID); err != nil {
		return "", err
	}
	var clusterID string
	if err := tx.Raw(`SELECT cluster_id FROM manifest_bindings WHERE id = ? FOR SHARE`, bindingID).Row().Scan(&clusterID); err != nil {
		return "", err
	}
	// ponytail: serialize only the short admission transaction per cluster; use
	// sorted resource locks if concurrent admission becomes a measured bottleneck.
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, "manifest-admission:"+clusterID).Error; err != nil {
		return "", err
	}
	var busy bool
	if err := tx.Raw(`SELECT EXISTS (
 SELECT 1 FROM manifest_operation_runs operation JOIN execution_tasks task ON task.id = operation.execution_task_id
 WHERE operation.binding_id = ? AND operation.action IN ('apply','repair','rollback','adopt')
 AND task.status IN ('queued','dispatching','running','canceling'))`, bindingID).Row().Scan(&busy); err != nil {
		return "", err
	}
	if busy {
		return "", fmt.Errorf("%w: Manifest target has an unfinished operation", apperrors.ErrConflict)
	}
	node, _ := domainworkflow.NodeExecutionFrom(ctx)
	if err := tx.Raw(`SELECT EXISTS (
 SELECT 1 FROM manifest_deployments deployment
 JOIN delivery_plans plan ON plan.id = deployment.delivery_snapshot->>'deliveryPlanId'
 JOIN workflow_runs run ON run.id = plan.impact->>'workflowRunId'
 WHERE deployment.binding_id = ? AND plan.source = 'delivery_batch' AND run.id <> ?
 AND run.status NOT IN ('completed','partially_completed','failed','canceled')
 AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(run.metadata::jsonb->'nodeRuns', '[]'::jsonb)) node
 WHERE node->>'targetId' = plan.impact->>'workflowTargetId' AND node->>'stage' = 'health'
 AND node->>'status' IN ('completed','failed','skipped','canceled'))
 )`, bindingID, node.RunID).Row().Scan(&busy); err != nil {
		return "", err
	}
	if busy {
		return "", fmt.Errorf("%w: Manifest target belongs to an unfinished delivery", apperrors.ErrConflict)
	}
	return clusterID, nil
}

func checkManifestResourceOwners(tx *gorm.DB, clusterID, bindingID string, documents ...[]domainmanifest.RenderedDocument) error {
	keys := domainmanifest.ResourceKeys(clusterID, documents...)
	if err := repodelivery.CheckManifestResourceOwners(tx, clusterID, bindingID, keys); err != nil {
		return err
	}
	return repodelivery.CheckHelmResourceOwners(tx, clusterID, nil, keys)
}

func lockManifestOperation(ctx context.Context, tx *gorm.DB, run domainmanifest.OperationRun, task domaindelivery.ExecutionTask) error {
	switch run.Action {
	case domainmanifest.TaskActionApply, domainmanifest.TaskActionRepair, domainmanifest.TaskActionRollback, domainmanifest.TaskActionAdopt:
	default:
		return nil
	}
	clusterID, err := lockManifestTarget(ctx, tx, run.BindingID)
	if err != nil {
		return err
	}
	data, err := json.Marshal(task.Payload)
	if err != nil {
		return err
	}
	var payload domainmanifest.TaskPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		return err
	}
	if payload.ClusterID != clusterID || payload.BindingID != run.BindingID {
		return fmt.Errorf("%w: Manifest task scope changed", apperrors.ErrConflict)
	}
	if run.DeploymentID != "" {
		var current bool
		if err := tx.Raw(`SELECT EXISTS (SELECT 1 FROM manifest_deployments WHERE id = ? AND binding_id = ? AND generation = ?)`, run.DeploymentID, run.BindingID, run.Generation).Row().Scan(&current); err != nil {
			return err
		}
		if !current {
			return fmt.Errorf("%w: Manifest task generation changed", apperrors.ErrConflict)
		}
	}
	return checkManifestResourceOwners(tx, clusterID, run.BindingID, payload.Documents, payload.GitOpsDocuments)
}
