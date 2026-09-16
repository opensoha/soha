package delivery

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

func (r *Repository) FindDeliveryDraftCreation(ctx context.Context, actor, key, digest string) (domaindelivery.DeliveryDraft, error) {
	var existingDigest string
	var receipt []byte
	err := dbtx.DB(ctx, r.db).Raw(`SELECT creation_digest, creation_receipt FROM delivery_drafts WHERE created_by = ? AND creation_key = ?`, actor, key).Row().Scan(&existingDigest, &receipt)
	if errors.Is(err, sql.ErrNoRows) {
		return domaindelivery.DeliveryDraft{}, apperrors.ErrNotFound
	}
	if err != nil {
		return domaindelivery.DeliveryDraft{}, err
	}
	if existingDigest != digest {
		return domaindelivery.DeliveryDraft{}, fmt.Errorf("%w: draft creation key was used for another input", apperrors.ErrConflict)
	}
	var item domaindelivery.DeliveryDraft
	err = json.Unmarshal(receipt, &item)
	return item, err
}

func (r *Repository) CreateDeliveryDraftIdempotent(ctx context.Context, input domaindelivery.DeliveryDraftInput, actor, digest string) (domaindelivery.DeliveryDraft, error) {
	var item domaindelivery.DeliveryDraft
	err := dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		if err := dbtx.DB(ctx, r.db).Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "delivery-draft-create:"+actor+":"+input.IdempotencyKey).Error; err != nil {
			return err
		}
		existing, err := r.FindDeliveryDraftCreation(ctx, actor, input.IdempotencyKey, digest)
		if err == nil {
			item = existing
			return nil
		}
		if !errors.Is(err, apperrors.ErrNotFound) {
			return err
		}
		item, err = r.CreateDeliveryDraft(ctx, input, actor)
		if err != nil {
			return err
		}
		receipt, err := json.Marshal(item)
		if err != nil {
			return err
		}
		return dbtx.DB(ctx, r.db).Exec(`UPDATE delivery_drafts SET creation_key = ?, creation_digest = ?, creation_receipt = ?::jsonb WHERE id = ?`, input.IdempotencyKey, digest, string(receipt), item.ID).Error
	})
	return item, err
}

func (r *Repository) GetDeliveryDraftConfirmation(ctx context.Context, id string) (domaindelivery.DeliveryDraftConfirmResult, error) {
	var raw []byte
	err := dbtx.DB(ctx, r.db).Raw(`SELECT confirmation_receipt FROM delivery_drafts WHERE id = ? AND confirmation_receipt IS NOT NULL`, id).Row().Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return domaindelivery.DeliveryDraftConfirmResult{}, apperrors.ErrNotFound
	}
	if err != nil {
		return domaindelivery.DeliveryDraftConfirmResult{}, err
	}
	var receipt domaindelivery.DeliveryDraftConfirmResult
	err = json.Unmarshal(raw, &receipt)
	return receipt, err
}

func (r *Repository) WithDeliveryDraftConfirmation(ctx context.Context, id string, apply func(context.Context, domaindelivery.DeliveryDraft, *domaindelivery.DeliveryDraftConfirmResult) (domaindelivery.DeliveryDraftConfirmResult, error)) (domaindelivery.DeliveryDraftConfirmResult, error) {
	var result domaindelivery.DeliveryDraftConfirmResult
	err := dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		var locked string
		err := dbtx.DB(ctx, r.db).Raw(`SELECT id FROM delivery_drafts WHERE id = ? FOR UPDATE`, id).Row().Scan(&locked)
		if errors.Is(err, sql.ErrNoRows) {
			return apperrors.ErrNotFound
		}
		if err != nil {
			return err
		}
		draft, err := r.GetDeliveryDraft(ctx, id)
		if err != nil {
			return err
		}
		previous, err := r.GetDeliveryDraftConfirmation(ctx, id)
		var receipt *domaindelivery.DeliveryDraftConfirmResult
		if err == nil {
			receipt = &previous
		} else if !errors.Is(err, apperrors.ErrNotFound) {
			return err
		}
		result, err = apply(ctx, draft, receipt)
		if err != nil || receipt != nil {
			return err
		}
		if result.Draft.ID != id || result.Draft.Status != domaindelivery.DeliveryDraftStatusConfirmed {
			return apperrors.ErrConflict
		}
		result.Draft, err = r.UpdateDeliveryDraft(ctx, result.Draft)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(result)
		if err != nil {
			return err
		}
		return dbtx.DB(ctx, r.db).Exec(`UPDATE delivery_drafts SET confirmation_receipt = ?::jsonb WHERE id = ?`, string(raw), id).Error
	})
	return result, err
}
