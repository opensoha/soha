package delivery

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/google/uuid"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	di "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"github.com/opensoha/soha/internal/platform/operationentry"
	repooperation "github.com/opensoha/soha/internal/repository/operationlog"
)

// RecoverLegacyHelmPlan closes an old confirming state after the legacy writers
// have drained. Current admission cannot create or dispatch a v1 apply.
func (r *Repository) RecoverLegacyHelmPlan(ctx context.Context, plan dd.DeliveryPlan, actor di.Principal, taskIDs []string, validate func([]dd.ExecutionTask) (bool, error)) (dd.DeliveryPlan, error) {
	var updated dd.DeliveryPlan
	err := r.WithServiceReferenceScope(ctx, plan.ApplicationID, func(ctx context.Context) error {
		tx := dbtx.DB(ctx, r.db)
		var id string
		err := tx.Raw(`
			SELECT p.id FROM delivery_plans p
			JOIN delivery_plan_service_reference_impacts i ON i.plan_id=p.id
			WHERE p.id=? AND p.application_id=? AND p.application_environment_id=?
			  AND p.source<>'delivery_batch' AND p.status='confirming' AND p.updated_at=?
			  AND i.snapshot->>'version'='1'
			FOR UPDATE OF p`, plan.ID, plan.ApplicationID, plan.ApplicationEnvironmentID, plan.UpdatedAt).Row().Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: legacy Helm Plan changed; reload before recovering", apperrors.ErrConflict)
		}
		if err != nil {
			return err
		}
		tasks, err := r.lockLegacyHelmTasks(ctx, taskIDs)
		if err != nil {
			return err
		}
		complete, err := validate(tasks)
		if err != nil {
			return err
		}
		status := dd.DeliveryPlanStatusDraft
		if complete {
			status = dd.DeliveryPlanStatusConfirmed
		}
		if err := tx.Exec(`
			UPDATE delivery_plans SET status=?, updated_at=clock_timestamp(),
			  confirmed_at=CASE WHEN ? THEN clock_timestamp() ELSE confirmed_at END
			WHERE id=?`, status, complete, id).Error; err != nil {
			return err
		}
		states := make(map[string]string, len(tasks))
		for _, task := range tasks {
			states[task.ID] = task.Status
		}
		entry := operationentry.New(ctx, actor, "delivery.plan.legacy_helm.recover",
			map[string]any{"resourceKind": "DeliveryPlan", "planId": id}, "success", "recovered legacy Helm Plan confirmation",
			map[string]any{"planStatus": status, "taskStatuses": states, "missingTaskCount": len(taskIDs) - len(tasks)})
		entry.ID = uuid.NewString()
		if err := repooperation.New(r.db).Create(ctx, entry); err != nil {
			return err
		}
		updated, err = r.GetDeliveryPlan(ctx, id)
		return err
	})
	return updated, err
}

func (r *Repository) lockLegacyHelmTasks(ctx context.Context, taskIDs []string) ([]dd.ExecutionTask, error) {
	var found []string
	if err := dbtx.DB(ctx, r.db).Raw(`SELECT id FROM execution_tasks WHERE id IN ? ORDER BY id FOR UPDATE`, taskIDs).Scan(&found).Error; err != nil {
		return nil, err
	}
	tasks := make([]dd.ExecutionTask, 0, len(found))
	for _, id := range found {
		task, err := r.GetExecutionTask(ctx, id)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, nil
}
