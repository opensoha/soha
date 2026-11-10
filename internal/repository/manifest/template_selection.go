package manifest

import (
	"context"
	"encoding/json"
	"errors"

	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/gorm"
)

// Adoption changes this environment only. It never publishes a shared package
// revision, changes a deployment generation, or rewrites another environment.
func (r *Repository) SetTemplateSelection(ctx context.Context, bindingID string, expected int64, selection domainmanifest.TemplateSelection) error {
	data, err := json.Marshal(selection)
	if err != nil {
		return err
	}
	return dbtx.DB(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if _, err := lockBindingConfiguration(tx, bindingID, "", false); err != nil {
			if errors.Is(err, apperrors.ErrNotFound) {
				return apperrors.ErrConflict
			}
			return err
		}
		result := tx.Exec(`UPDATE manifest_bindings SET template_selection=?::jsonb, version=version+1, updated_at=clock_timestamp() WHERE id=? AND version=?`, string(data), bindingID, expected)
		if result.Error == nil && result.RowsAffected != 1 {
			return apperrors.ErrConflict
		}
		return result.Error
	})
}
