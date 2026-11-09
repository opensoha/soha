package networkaccess

import (
	"context"
	identity "github.com/opensoha/soha/internal/domain/identity"
	access "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// MihomoBundleValidator owns publication identity, revision and recipient checks.
// Device facts come from the network access store, never from profile input.
type MihomoBundleValidator interface {
	ValidateMihomoBundle(context.Context, identity.Principal, access.MihomoProfileInput, access.Device, *access.MihomoProfile) error
}

// SetMihomoBundleValidator is called during construction, before API exposure.
func (s *Service) SetMihomoBundleValidator(validator MihomoBundleValidator) {
	s.mihomoBundles = validator
}

func (s *Service) validateMihomoBundle(ctx context.Context, p identity.Principal, input access.MihomoProfileInput, previous *access.MihomoProfile) error {
	if input.BundleID == "" {
		return nil
	}
	if isNilDependency(s.mihomoBundles) {
		return apperrors.ErrServiceUnavailable
	}
	var device access.Device
	if input.DeviceID != "" {
		var err error
		device, err = s.store.GetDevice(ctx, input.DeviceID)
		if err != nil {
			return err
		}
		if device.ID != input.DeviceID || device.OwnerUserID == "" {
			return apperrors.ErrAccessDenied
		}
	}
	return s.mihomoBundles.ValidateMihomoBundle(ctx, p, input, device, previous)
}
