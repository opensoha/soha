package manifest

import (
	"context"
	"encoding/json"

	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

// Adoption changes this environment only. It never publishes a shared package
// revision, changes a deployment generation, or rewrites another environment.
func (r *Repository) SetTemplateSelection(ctx context.Context, bindingID string, expected int64, selection domainmanifest.TemplateSelection) error {
	data, err := json.Marshal(selection)
	if err != nil {
		return err
	}
	result := dbtx.DB(ctx, r.db).Exec(`UPDATE manifest_bindings SET template_selection=?::jsonb, version=version+1, updated_at=clock_timestamp() WHERE id=? AND version=?`, string(data), bindingID, expected)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return apperrors.ErrConflict
	}
	return nil
}
