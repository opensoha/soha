package copilot

import (
	"context"
	"fmt"

	domain "github.com/opensoha/soha/internal/domain/copilot"
)

func (r *Repository) DisabledRuntimeAssets(ctx context.Context, refs []domain.RuntimeAssetRef) ([]domain.RuntimeAssetRef, error) {
	if len(refs) == 0 {
		return nil, nil
	}
	keys := make([][]any, 0, len(refs))
	for _, ref := range refs {
		keys = append(keys, []any{ref.Kind, ref.ID, ref.Version})
	}
	var disabled []domain.RuntimeAssetRef
	err := r.db.WithContext(ctx).Raw(`
		SELECT kind, asset_id AS id, version FROM ai_workbench_runtime_asset_state
		WHERE (kind, asset_id, version) IN ? AND NOT enabled`, keys).Scan(&disabled).Error
	if err != nil {
		return nil, fmt.Errorf("read disabled runtime assets: %w", err)
	}
	return disabled, nil
}

func (r *Repository) SetRuntimeAssetEnabled(ctx context.Context, actor string, ref domain.RuntimeAssetRef, enabled bool) error {
	err := r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_workbench_runtime_asset_state (kind, asset_id, version, enabled, updated_by)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (kind, asset_id, version) DO UPDATE
		SET enabled = EXCLUDED.enabled, updated_by = EXCLUDED.updated_by, updated_at = now()`,
		ref.Kind, ref.ID, ref.Version, enabled, actor).Error
	if err != nil {
		return fmt.Errorf("save runtime asset state: %w", err)
	}
	return nil
}
