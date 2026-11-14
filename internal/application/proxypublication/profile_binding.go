package proxypublication

import (
	"context"
	"slices"

	identity "github.com/opensoha/soha/internal/domain/identity"
	access "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// ValidateMihomoBundle validates a profile reference at save time. Recipient
// authorization is also repeated at device projection time.
func (b *Bundles) ValidateMihomoBundle(ctx context.Context, principal identity.Principal, input access.MihomoProfileInput, device access.Device, previous *access.MihomoProfile) error {
	record, err := b.store.FindBundle(ctx, input.BundleID)
	if err != nil {
		return err
	}
	if !retainsMihomoBinding(input, previous) {
		if err = b.auth.Authorize(ctx, principal, record.InstanceID, "view"); err != nil {
			return err
		}
	}
	current, err := b.currentBundle(ctx, record.InstanceID, input.BundleID)
	if err != nil {
		return err
	}
	if string(current.Public.DeliveryMode) != input.DeliveryMode || current.Public.OrgRevision != input.OrgRevision || current.Public.SourceRevision != input.SourceRevision {
		return apperrors.ErrConflict
	}
	if input.DeviceID == "" || device.ID != input.DeviceID || !slices.Contains(current.Scope.DeviceIDs, device.ID) || !slices.Contains(current.Scope.UserIDs, device.OwnerUserID) {
		return apperrors.ErrAccessDenied
	}
	return nil
}

// previous is loaded by the profile service; public input cannot opt into retention.
func retainsMihomoBinding(input access.MihomoProfileInput, previous *access.MihomoProfile) bool {
	if previous == nil || previous.ID == "" || previous.Revision < 1 || previous.BundleID == "" {
		return false
	}
	return input.BundleID == previous.BundleID && input.DeviceID == previous.DeviceID && input.DeliveryMode == previous.DeliveryMode && input.OrgRevision == previous.OrgRevision && input.SourceRevision == previous.SourceRevision && input.SourceType == previous.SourceType && input.Mode == previous.Mode
}
