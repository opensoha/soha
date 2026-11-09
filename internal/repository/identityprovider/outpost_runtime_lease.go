package identityprovider

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

// RecordOutpostRuntimeLease serializes with version resolution and credential
// rotation. A signed response is never returned before its grant is durable.
func (r *Repository) RecordOutpostRuntimeLease(ctx context.Context, id, tokenHash, digest string, version int64, issuedAt, expiresAt time.Time) error {
	if id == "" || tokenHash == "" || digest == "" || version <= 0 || !expiresAt.After(issuedAt) {
		return fmt.Errorf("%w: invalid outpost runtime grant", apperrors.ErrInvalidArgument)
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "identity-outpost-runtime:"+id).Error; err != nil {
			return err
		}
		var lockedID string
		err := tx.Raw(`SELECT id FROM identity_outposts WHERE id = ? AND token_hash = ? FOR UPDATE`, id, tokenHash).Row().Scan(&lockedID)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: outpost registration token changed", apperrors.ErrAccessDenied)
		}
		if err != nil {
			return err
		}
		result := tx.Exec(`
   INSERT INTO identity_outpost_runtime_leases (outpost_id, token_hash, configuration_digest, configuration_version, issued_at, expires_at)
   SELECT ?, ?, ?, ?, ?, ? FROM identity_outpost_runtime_versions
   WHERE outpost_id = ? AND configuration_digest = ? AND configuration_version = ?
   ON CONFLICT (outpost_id) DO UPDATE SET
    token_hash = EXCLUDED.token_hash, configuration_digest = EXCLUDED.configuration_digest,
    configuration_version = EXCLUDED.configuration_version, issued_at = EXCLUDED.issued_at, expires_at = EXCLUDED.expires_at
   WHERE identity_outpost_runtime_leases.token_hash <> EXCLUDED.token_hash
      OR identity_outpost_runtime_leases.issued_at <= EXCLUDED.issued_at
  `, id, tokenHash, digest, version, issuedAt, expiresAt, id, digest, version)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return fmt.Errorf("%w: outpost configuration changed during claim", apperrors.ErrConflict)
		}
		return nil
	})
}

func (r *Repository) ValidateOutpostRuntimeLease(ctx context.Context, id, tokenHash string, version int64, reportedExpiry *time.Time) error {
	var valid bool
	err := r.db.WithContext(ctx).Raw(`
  SELECT EXISTS (
   SELECT 1 FROM identity_outpost_runtime_leases l
   JOIN identity_outposts o ON o.id = l.outpost_id AND o.token_hash = l.token_hash
   WHERE l.outpost_id = ? AND l.token_hash = ? AND l.configuration_version = ?
     AND l.issued_at <= CURRENT_TIMESTAMP AND l.expires_at > CURRENT_TIMESTAMP
     AND (?::timestamptz IS NULL OR (?::timestamptz > CURRENT_TIMESTAMP AND ?::timestamptz <= l.expires_at))
  )`, id, tokenHash, version, reportedExpiry, reportedExpiry, reportedExpiry).Row().Scan(&valid)
	if err != nil {
		return err
	}
	if !valid {
		return fmt.Errorf("%w: outpost configuration lease is not granted or has expired", apperrors.ErrConflict)
	}
	return nil
}
