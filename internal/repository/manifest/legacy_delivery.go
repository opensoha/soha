package manifest

import (
	"context"
	"fmt"

	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (r *Repository) ValidateLegacyManifestDelivery(ctx context.Context, payload domainmanifest.TaskPayload) error {
	switch payload.Action {
	case domainmanifest.TaskActionApply, domainmanifest.TaskActionRepair, domainmanifest.TaskActionRollback, domainmanifest.TaskActionAdopt, domainmanifest.TaskActionRolloutControl:
	default:
		return nil
	}
	var associated bool
	if err := r.db.WithContext(ctx).Raw(`SELECT EXISTS (SELECT 1 FROM release_targets
 WHERE executor_kind = 'manifest_ssa' AND config_ref = ?)`, payload.BindingID).Row().Scan(&associated); err != nil {
		return fmt.Errorf("check Manifest publication association: %w", err)
	}
	if associated {
		return fmt.Errorf("%w: associated targets require a current delivery Plan", apperrors.ErrConflict)
	}
	return nil
}
