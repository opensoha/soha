package delivery

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

// CheckHelmResourceOwners shares Manifest's cluster admission transaction. A
// failed/uncertain attempt retains its possible resources until a later deployed
// revision supersedes it. Historical hooks remain reserved because Helm may keep them.
func CheckHelmResourceOwners(tx *gorm.DB, clusterID string, owner *sohaapi.HelmDeliverySnapshot, keys []string) error {
	rows, err := tx.Raw(`WITH candidates AS (
 SELECT task.*, task.payload::jsonb->'helm'->'snapshot' AS snapshot
 FROM execution_tasks task WHERE task.task_kind = 'helm_apply'
 AND task.payload::jsonb->'helm'->'snapshot'->>'clusterId' = ?
 AND (task.status IN ('queued','dispatching','running','canceling') OR task.attempt_count > 0)
), successful AS (
 SELECT snapshot->>'namespace' AS namespace, snapshot->>'releaseName' AS release_name, MAX(created_at) AS created_at
 FROM candidates WHERE status = 'completed' AND result::jsonb->'helm'->>'status' = 'deployed'
 AND result::jsonb->'helm'->>'stopped' = 'true'
 GROUP BY snapshot->>'namespace', snapshot->>'releaseName'
)
 SELECT candidates.snapshot::text,
 (successful.created_at IS NULL OR candidates.created_at >= successful.created_at OR candidates.status IN ('queued','dispatching','running','canceling'))
 FROM candidates LEFT JOIN successful ON successful.namespace = candidates.snapshot->>'namespace'
 AND successful.release_name = candidates.snapshot->>'releaseName'`, clusterID).Rows()
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var encoded string
		var current bool
		if err := rows.Scan(&encoded, &current); err != nil {
			return err
		}
		var snapshot sohaapi.HelmDeliverySnapshot
		if err := json.Unmarshal([]byte(encoded), &snapshot); err != nil {
			return err
		}
		if owner != nil && owner.ApplicationID == snapshot.ApplicationID && owner.ServiceID == snapshot.ServiceID && owner.ApplicationEnvironmentID == snapshot.ApplicationEnvironmentID && owner.TargetID == snapshot.TargetID && owner.Namespace == snapshot.Namespace && owner.ReleaseName == snapshot.ReleaseName {
			continue
		}
		if !current {
			snapshot.Resources = slices.DeleteFunc(snapshot.Resources, func(r sohaapi.HelmDeliveryResource) bool { return !r.Hook })
		}
		for _, key := range domainworkflow.HelmResourceKeys(snapshot) {
			if slices.Contains(keys, key) {
				return fmt.Errorf("%w: resource is owned by another Helm delivery target", apperrors.ErrConflict)
			}
		}
	}
	return rows.Err()
}

func lockHelmDelivery(ctx context.Context, tx *gorm.DB, task domaindelivery.ExecutionTask) error {
	if task.TaskKind != "helm_apply" {
		return nil
	}
	encoded, err := json.Marshal(task.Payload["helm"])
	if err != nil {
		return err
	}
	var payload sohaapi.HelmExecutionTaskPayload
	if err := json.Unmarshal(encoded, &payload); err != nil {
		return err
	}
	snapshot := payload.Snapshot
	if snapshot.ClusterID == "" || snapshot.DeliveryPlanID == "" || snapshot.ApplicationID != task.ApplicationID || snapshot.ApplicationEnvironmentID != task.ApplicationEnvironmentID || payload.Action != sohaapi.Apply || payload.Prepared != nil {
		return apperrors.ErrConflict
	}
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, "manifest-admission:"+snapshot.ClusterID).Error; err != nil {
		return err
	}
	var busy bool
	if err := tx.Raw(`SELECT EXISTS (SELECT 1 FROM execution_tasks WHERE task_kind = 'helm_apply' AND queue_key = ?
 AND id <> ? AND status IN ('queued','dispatching','running','canceling'))`, task.QueueKey, task.ID).Row().Scan(&busy); err != nil {
		return err
	}
	if busy {
		return fmt.Errorf("%w: Helm release has an unfinished operation", apperrors.ErrConflict)
	}
	node, _ := domainworkflow.NodeExecutionFrom(ctx)
	if err := tx.Raw(`SELECT EXISTS (
 SELECT 1 FROM execution_tasks task JOIN workflow_runs run ON run.id = task.payload->>'workflowRunId'
 WHERE task.task_kind = 'helm_apply' AND task.queue_key = ? AND task.payload->>'workflowScope' = 'delivery_batch'
 AND run.id <> ? AND run.status NOT IN ('completed','partially_completed','failed','canceled')
 AND NOT EXISTS (SELECT 1 FROM jsonb_array_elements(COALESCE(run.metadata::jsonb->'nodeRuns','[]'::jsonb)) node
 WHERE node->>'targetId' = task.payload->>'workflowTargetId' AND node->>'stage' = 'health'
 AND node->>'status' IN ('completed','failed','skipped','canceled')))
 `, task.QueueKey, node.RunID).Row().Scan(&busy); err != nil {
		return err
	}
	if busy {
		return fmt.Errorf("%w: Helm release belongs to an unfinished delivery", apperrors.ErrConflict)
	}
	keys := domainworkflow.HelmResourceKeys(snapshot)
	if err := CheckManifestResourceOwners(tx, snapshot.ClusterID, "", keys); err != nil {
		return err
	}
	return CheckHelmResourceOwners(tx, snapshot.ClusterID, &snapshot, keys)
}

func (r *Repository) createHelmTask(ctx context.Context, task domaindelivery.ExecutionTask) (domaindelivery.ExecutionTask, error) {
	var stored domaindelivery.ExecutionTask
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockHelmDelivery(ctx, tx, task); err != nil {
			return err
		}
		var err error
		stored, err = New(tx).createExecutionTask(ctx, task)
		return err
	})
	return stored, err
}
