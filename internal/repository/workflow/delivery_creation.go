package workflow

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func (r *Repository) FindDeliveryWorkflowCreation(ctx context.Context, actor, key, digest string) (domainworkflow.DeliveryWorkflow, error) {
	var storedDigest string
	var receipt []byte
	err := r.db.WithContext(ctx).Raw(`SELECT creation_digest, creation_receipt FROM delivery_workflows WHERE created_by = ? AND creation_key = ?`, actor, key).Row().Scan(&storedDigest, &receipt)
	if errors.Is(err, sql.ErrNoRows) {
		return domainworkflow.DeliveryWorkflow{}, apperrors.ErrNotFound
	}
	if err != nil {
		return domainworkflow.DeliveryWorkflow{}, err
	}
	if storedDigest != digest {
		return domainworkflow.DeliveryWorkflow{}, fmt.Errorf("%w: workflow creation key was used with different input", apperrors.ErrConflict)
	}
	var item domainworkflow.DeliveryWorkflow
	if err := json.Unmarshal(receipt, &item); err != nil {
		return item, err
	}
	return item, nil
}

func (r *Repository) CreateDeliveryWorkflowIdempotent(ctx context.Context, item domainworkflow.DeliveryWorkflow, key, digest string) (domainworkflow.DeliveryWorkflow, error) {
	var result domainworkflow.DeliveryWorkflow
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?, 0))`, "delivery.workflow.create:"+item.CreatedBy+":"+key).Error; err != nil {
			return err
		}
		repo := New(tx)
		found, err := repo.FindDeliveryWorkflowCreation(ctx, item.CreatedBy, key, digest)
		if err == nil {
			result = found
			return nil
		}
		if !errors.Is(err, apperrors.ErrNotFound) {
			return err
		}
		result, err = repo.SaveDeliveryWorkflow(ctx, item, 0)
		if err != nil {
			return err
		}
		receipt, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return tx.Exec(`UPDATE delivery_workflows SET creation_key = ?, creation_digest = ?, creation_receipt = ?::jsonb WHERE id = ?`, key, digest, string(receipt), result.ID).Error
	})
	return result, err
}
