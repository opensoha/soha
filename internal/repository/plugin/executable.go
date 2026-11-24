package plugin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
)

func (r *Repository) PreviousRelease(ctx context.Context, pluginID string) (domainplugin.InstalledPlugin, error) {
	var item domainplugin.InstalledPlugin
	var snapshot []byte
	err := dbtx.DB(ctx, r.db).Raw("SELECT snapshot FROM plugin_previous_releases WHERE plugin_id = ?", pluginID).Row().Scan(&snapshot)
	if errors.Is(err, sql.ErrNoRows) {
		return item, apperrors.ErrNotFound
	}
	if err != nil {
		return item, err
	}
	err = json.Unmarshal(snapshot, &item)
	return item, err
}

// RetainRelease is called within the same DB transaction as the installation CAS.
func (r *Repository) RetainRelease(ctx context.Context, item domainplugin.InstalledPlugin) error {
	item.ConfiguredSecretRefs = nil
	item.Metadata = nil
	item.RuntimeReady = nil
	item.ReferenceCount = nil
	item.RollbackVersion = ""
	item.RollbackRequiresPermissionConfirmation = false
	snapshot, err := json.Marshal(item)
	if err != nil {
		return err
	}
	return dbtx.DB(ctx, r.db).Exec(`
		INSERT INTO plugin_previous_releases (plugin_id, snapshot) VALUES (?, ?::jsonb)
		ON CONFLICT (plugin_id) DO UPDATE SET snapshot = EXCLUDED.snapshot
	`, item.ID, string(snapshot)).Error
}
