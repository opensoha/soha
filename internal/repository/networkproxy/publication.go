package networkproxy

import (
	"context"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
	"time"
)

// GrantConfigurationLease records only a lease for the exact configuration
// returned by the control plane. A later revision or protection change makes
// this grant ineligible for employee projections until the runtime polls again.
func (r *Repository) GrantConfigurationLease(ctx context.Context, id string, revision int64, digest string, until, now time.Time) error {
	if !until.After(now) || until.After(now.Add(5*time.Minute)) {
		return apperrors.ErrConflict
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		row, err := lockedInstance(tx, id)
		if err != nil {
			return err
		}
		if row.DesiredRevision != revision || configurationForDigest(row).Digest() != digest {
			return apperrors.ErrConflict
		}
		return tx.Model(&row).Updates(map[string]any{"configuration_valid_until": until, "configuration_lease_revision": revision, "configuration_lease_hash": digest}).Error
	})
}
func configurationLease(row instanceRow) *time.Time {
	if row.ConfigurationLeaseRevision != row.DesiredRevision || row.ConfigurationLeaseHash != configurationForDigest(row).Digest() {
		return nil
	}
	return row.ConfigurationValidUntil
}
