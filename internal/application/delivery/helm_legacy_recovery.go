package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	dd "github.com/opensoha/soha/internal/domain/delivery"
	di "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type legacyHelmPlanRecoveryRepository interface {
	RecoverLegacyHelmPlan(context.Context, dd.DeliveryPlan, di.Principal, []string, func([]dd.ExecutionTask) (bool, error)) (dd.DeliveryPlan, error)
}

func (s *Service) recoverLegacyHelmPlanConfirmation(ctx context.Context, p di.Principal, plan dd.DeliveryPlan) (dd.DeliveryPlanConfirmResult, bool, error) {
	result := dd.DeliveryPlanConfirmResult{Plan: plan}
	if plan.Status != dd.DeliveryPlanStatusConfirming || !standaloneHelmDeployPlan(plan) {
		return result, false, nil
	}
	evidence, ok := s.repository.(serviceReferenceImpactRepository)
	if !ok {
		return result, false, nil
	}
	frozen, err := evidence.GetDeliveryPlanServiceReferenceSnapshot(ctx, plan.ID)
	if errors.Is(err, apperrors.ErrNotFound) || err == nil && frozen.Version != 1 {
		return result, false, nil
	}
	if err != nil {
		return result, true, err
	}
	repo, ok := s.repository.(legacyHelmPlanRecoveryRepository)
	if !ok || frozen.ApplicationID != plan.ApplicationID || frozen.SourceBindingID != plan.ApplicationEnvironmentID {
		return result, true, staleServiceReferencePlan()
	}
	ids := make([]string, 0, len(plan.HelmSnapshots))
	snapshots := make(map[string]sohaapi.HelmDeliverySnapshot, len(plan.HelmSnapshots))
	for _, snapshot := range plan.HelmSnapshots {
		id := helmTaskID(ctx, snapshot, sohaapi.Apply)
		if _, duplicate := snapshots[id]; duplicate || snapshot.DeliveryPlanID != plan.ID || snapshot.TargetID == "" {
			return result, true, apperrors.ErrConflict
		}
		ids = append(ids, id)
		snapshots[id] = snapshot
	}
	updated, err := repo.RecoverLegacyHelmPlan(ctx, plan, p, ids, func(tasks []dd.ExecutionTask) (bool, error) {
		return validateLegacyHelmReceipts(plan, snapshots, tasks)
	})
	if err != nil {
		return result, true, err
	}
	result.Plan = updated
	if updated.Status != dd.DeliveryPlanStatusConfirmed {
		return result, true, staleServiceReferencePlan()
	}
	result.Result = dd.ApplicationDeliveryActionResult{Action: plan.Action, ApplicationID: plan.ApplicationID, ApplicationEnvironmentID: plan.ApplicationEnvironmentID}
	result.Result.RelatedIDs.ExecutionTaskIDs, result.Result.RelatedIDs.ExecutionTaskID = ids, ids[0]
	return result, true, nil
}

func validateLegacyHelmReceipts(plan dd.DeliveryPlan, snapshots map[string]sohaapi.HelmDeliverySnapshot, tasks []dd.ExecutionTask) (bool, error) {
	for _, task := range tasks {
		snapshot, expected := snapshots[task.ID]
		if !expected || !matchesHelmApplyTask(task, plan, snapshot) || !legacyHelmTaskStopped(task) {
			return false, fmt.Errorf("%w: legacy Helm task needs cancellation or a terminal executor receipt before recovering the Plan", apperrors.ErrConflict)
		}
	}
	return len(tasks) == len(snapshots), nil
}

func legacyHelmTaskStopped(task dd.ExecutionTask) bool {
	if task.FinishedAt == nil || !dd.BuildOperationState(task, time.Now()).Terminal {
		return false
	}
	if task.Status == "canceled" && task.AttemptCount == 0 && task.StartedAt == nil {
		return true // The existing queued-task cancellation never handed work to an executor.
	}
	var receipt sohaapi.HelmExecutionTaskResult
	raw, err := json.Marshal(task.Result["helm"])
	return err == nil && json.Unmarshal(raw, &receipt) == nil && receipt.Stopped
}
