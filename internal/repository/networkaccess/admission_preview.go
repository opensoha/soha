package networkaccess

import (
	"context"
	"database/sql"
	"errors"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func (r *Repository) AdmissionPreview(ctx context.Context, deviceID, bindingID string) (domain.AdmissionPreviewSnapshot, error) {
	var result domain.AdmissionPreviewSnapshot
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		repo := New(tx)
		var err error
		if result.Device, err = repo.GetDevice(ctx, deviceID); err != nil {
			return err
		}
		if result.Subject, err = repo.GetSubject(ctx, result.Device.OwnerUserID); err != nil {
			return err
		}
		if result.Binding, err = repo.GetNASBinding(ctx, bindingID); err != nil {
			return err
		}
		if result.Site, err = repo.GetSite(ctx, result.Binding.SiteID); err != nil {
			return err
		}
		result.Policies, err = repo.GetPolicySnapshot(ctx)
		if err != nil && !errors.Is(err, apperrors.ErrNotFound) {
			return err
		}
		profiles, err := repo.ListSiteProfileBindings(ctx, domain.SiteProfileBindingFilter{SiteID: result.Site.ID, Limit: 5})
		if err != nil {
			return err
		}
		result.Profiles = make(map[string]domain.SiteProfileBinding, len(profiles))
		for _, profile := range profiles {
			result.Profiles[profile.AccessProfile] = profile
		}
		if result.ResponsePolicies, err = repo.PublishedEndpointResponsePolicies(ctx); err != nil {
			return err
		}
		result.Presence, err = repo.EndpointPresence(ctx, deviceID)
		if err != nil && !errors.Is(err, apperrors.ErrNotFound) {
			return err
		}
		result.Restriction, err = repo.EndpointRestriction(ctx, deviceID, result.Site.ID)
		return err
	}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	return result, err
}
