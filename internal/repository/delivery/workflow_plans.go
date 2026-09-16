package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	repoworkflow "github.com/opensoha/soha/internal/repository/workflow"
	"gorm.io/gorm"
)

func (r *Repository) createBatchPlan(ctx context.Context, input domaindelivery.DeliveryPlanInput, actor string) (domaindelivery.DeliveryPlan, error) {
	node, _ := domainworkflow.NodeExecutionFrom(ctx)
	if node.Stage != "plan" {
		return domaindelivery.DeliveryPlan{}, apperrors.ErrConflict
	}
	var item domaindelivery.DeliveryPlan
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := repoworkflow.LockDeliveryNode(ctx, tx, input.ApplicationID, input.ApplicationEnvironmentID); err != nil {
			return err
		}
		repo := New(tx)
		var err error
		item, err = repo.GetDeliveryPlan(ctx, node.ResourceID("plan"))
		if err == nil {
			return nil
		}
		if !errors.Is(err, apperrors.ErrNotFound) {
			return err
		}
		input.ID, input.Source = node.ResourceID("plan"), domainworkflow.ScopeDeliveryBatch
		input.Impact = node.Metadata(input.Impact)
		input.Impact["workflowTargetId"] = node.TargetID
		item = normalizeDeliveryPlanInput(input, actor)
		item.Source = domainworkflow.ScopeDeliveryBatch
		if err := repo.saveDeliveryPlan(ctx, item, true); err != nil {
			return err
		}
		item, err = repo.GetDeliveryPlan(ctx, item.ID)
		return err
	})
	return item, err
}

func (r *Repository) updateBatchPlan(ctx context.Context, input domaindelivery.DeliveryPlan) (domaindelivery.DeliveryPlan, error) {
	var item domaindelivery.DeliveryPlan
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := New(tx)
		current, err := repo.GetDeliveryPlan(ctx, input.ID)
		if err != nil {
			return err
		}
		if current.Source != domainworkflow.ScopeDeliveryBatch {
			return apperrors.ErrConflict
		}
		node, worker := domainworkflow.NodeExecutionFrom(ctx)
		if worker {
			if current.Impact["workflowRunId"] != node.RunID || current.Impact["workflowTargetId"] != node.TargetID {
				return apperrors.ErrConflict
			}
			if err := repoworkflow.LockDeliveryNode(ctx, tx, current.ApplicationID, current.ApplicationEnvironmentID); err != nil {
				return err
			}
		} else if err := repoworkflow.LockDeliveryDispatch(ctx, tx, current.Impact); err != nil {
			return err
		}
		if err := tx.Exec(`SELECT id FROM delivery_plans WHERE id = ? FOR UPDATE`, input.ID).Error; err != nil {
			return err
		}
		current, err = repo.GetDeliveryPlan(ctx, input.ID)
		if err != nil {
			return err
		}
		if input.ExpectedUpdatedAt == nil || !input.ExpectedUpdatedAt.Equal(current.UpdatedAt) {
			return fmt.Errorf("%w: delivery plan changed", apperrors.ErrConflict)
		}
		if err := validateBatchPlanTransition(current, input, node, worker); err != nil {
			return err
		}
		// The approved plan content and provenance are immutable. Only state and
		// append-only approval facts can change after the plan was created.
		item = current
		item.Status, item.ConfirmedAt = input.Status, input.ConfirmedAt
		item.Impact["approval"] = input.Impact["approval"]
		item.UpdatedAt = time.Now().UTC()
		if err := repo.saveDeliveryPlan(ctx, item, false); err != nil {
			return err
		}
		item, err = repo.GetDeliveryPlan(ctx, item.ID)
		return err
	})
	return item, err
}

func validateBatchPlanTransition(current, next domaindelivery.DeliveryPlan, node domainworkflow.NodeExecution, worker bool) error {
	before, _ := current.Impact["approval"].([]any)
	after, _ := next.Impact["approval"].([]any)
	if len(after) < len(before) || len(after) > len(before)+1 {
		return apperrors.ErrConflict
	}
	a, _ := json.Marshal(before)
	b, _ := json.Marshal(after[:len(before)])
	if string(a) != string(b) && len(before) > 0 {
		return apperrors.ErrConflict
	}
	appended := len(after) == len(before)+1
	decision := ""
	if appended {
		latest, _ := after[len(after)-1].(map[string]any)
		decision, _ = latest["status"].(string)
	}
	if !worker {
		if current.Status == domaindelivery.DeliveryPlanStatusWaitingApproval && next.Status == domaindelivery.DeliveryPlanStatusDraft && appended && (decision == "approved" || decision == "rejected") {
			return nil
		}
		return apperrors.ErrConflict
	}
	if node.Stage == "plan" && current.Status == domaindelivery.DeliveryPlanStatusDraft && next.Status == domaindelivery.DeliveryPlanStatusWaitingApproval && appended && decision == "requested" {
		return nil
	}
	if node.Stage == "deploy" && !appended {
		return validateBatchPlanDeployTransition(current.Status, next.Status)
	}
	return apperrors.ErrConflict
}

func validateBatchPlanDeployTransition(current, next string) error {
	switch current {
	case domaindelivery.DeliveryPlanStatusDraft:
		if next == domaindelivery.DeliveryPlanStatusConfirming {
			return nil
		}
	case domaindelivery.DeliveryPlanStatusConfirming:
		if next == domaindelivery.DeliveryPlanStatusConfirming || next == domaindelivery.DeliveryPlanStatusConfirmed || next == domaindelivery.DeliveryPlanStatusDraft {
			return nil
		}
	}
	return apperrors.ErrConflict
}
