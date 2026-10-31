package aigateway

import (
	"context"
	"database/sql"
	"errors"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

func (r *Repository) GetTokenRotation(ctx context.Context, id string) (domainaigateway.TokenRotationRecord, error) {
	var record domainaigateway.TokenRotationRecord
	err := dbtx.DB(ctx, r.db).Raw(`SELECT rotation_request_id,actor_id,owner_id,token_kind,previous_token_id,replacement_token_id,rotated_at FROM gateway_token_rotations WHERE rotation_request_id=?`, id).Row().Scan(&record.RotationRequestID, &record.ActorID, &record.OwnerID, &record.TokenKind, &record.PreviousTokenID, &record.ReplacementTokenID, &record.RotatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return record, apperrors.ErrNotFound
	}
	record.Status = "completed"
	return record, err
}

func (r *Repository) beginTokenRotation(ctx context.Context, record domainaigateway.TokenRotationRecord) error {
	// The request lock also serializes attempts to reuse one ID for different Keys.
	if err := dbtx.DB(ctx, r.db).Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "gateway-token-rotation:"+record.RotationRequestID).Error; err != nil {
		return err
	}
	existing, err := r.GetTokenRotation(ctx, record.RotationRequestID)
	if err == nil {
		if existing.ActorID != record.ActorID || existing.OwnerID != record.OwnerID || existing.TokenKind != record.TokenKind || existing.PreviousTokenID != record.PreviousTokenID {
			return apperrors.ErrConflict
		}
		return apperrors.NewBusiness(apperrors.ErrConflict, "token_rotation_completed", "This Key was already rotated; query the rotation record using an independent credential", "")
	}
	if !errors.Is(err, apperrors.ErrNotFound) {
		return err
	}
	return nil
}
func (r *Repository) saveTokenRotation(ctx context.Context, record domainaigateway.TokenRotationRecord) error {
	return dbtx.DB(ctx, r.db).Exec(`INSERT INTO gateway_token_rotations(rotation_request_id,actor_id,owner_id,token_kind,previous_token_id,replacement_token_id,rotated_at) VALUES(?,?,?,?,?,?,?)`, record.RotationRequestID, record.ActorID, record.OwnerID, record.TokenKind, record.PreviousTokenID, record.ReplacementTokenID, record.RotatedAt).Error
}
func (r *Repository) RotatePersonalToken(ctx context.Context, previousID string, replacement domainaigateway.PersonalAccessToken, record domainaigateway.TokenRotationRecord) (domainaigateway.PersonalAccessToken, error) {
	var created domainaigateway.PersonalAccessToken
	err := dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		if err := r.beginTokenRotation(ctx, record); err != nil {
			return err
		}
		var id string
		err := dbtx.DB(ctx, r.db).Raw(`SELECT id FROM personal_access_tokens WHERE id=? AND user_id=? AND revoked_at IS NULL FOR UPDATE`, previousID, replacement.UserID).Row().Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return apperrors.ErrConflict
		}
		if err != nil {
			return err
		}
		created, err = r.CreatePersonalAccessToken(ctx, replacement)
		if err != nil {
			return err
		}
		if err = r.RevokePersonalAccessToken(ctx, replacement.UserID, previousID); err != nil {
			return err
		}
		return r.saveTokenRotation(ctx, record)
	})
	return created, err
}
func (r *Repository) RotateServiceToken(ctx context.Context, previousID string, replacement domainaigateway.ServiceAccountToken, record domainaigateway.TokenRotationRecord) (domainaigateway.ServiceAccountToken, error) {
	var created domainaigateway.ServiceAccountToken
	err := dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		if err := r.beginTokenRotation(ctx, record); err != nil {
			return err
		}
		var id string
		err := dbtx.DB(ctx, r.db).Raw(`SELECT id FROM service_account_tokens WHERE id=? AND service_account_id=? AND revoked_at IS NULL FOR UPDATE`, previousID, replacement.ServiceAccountID).Row().Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			return apperrors.ErrConflict
		}
		if err != nil {
			return err
		}
		// An account disabled during rotation cannot acquire a fresh credential.
		if err = dbtx.DB(ctx, r.db).Raw(`SELECT id FROM service_accounts WHERE id=? AND status='active' FOR SHARE`, replacement.ServiceAccountID).Row().Scan(&id); errors.Is(err, sql.ErrNoRows) {
			return apperrors.ErrConflict
		}
		if err != nil {
			return err
		}
		created, err = r.CreateServiceAccountToken(ctx, replacement)
		if err != nil {
			return err
		}
		if err = r.RevokeServiceAccountToken(ctx, previousID); err != nil {
			return err
		}
		return r.saveTokenRotation(ctx, record)
	})
	return created, err
}
