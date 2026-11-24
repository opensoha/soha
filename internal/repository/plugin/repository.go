package plugin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

const installedColumns = `id, name, version, publisher, type, status, source, manifest, checksum_status, signature_status,
	requested_permissions, configured_secret_refs, installed_by, installed_at, updated_at,
	enabled_at, disabled_at, metadata, source_info`

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListInstalled(ctx context.Context) ([]domainplugin.InstalledPlugin, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT ` + installedColumns + `
		FROM installed_plugins
		ORDER BY installed_at DESC, id ASC
	`).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := []domainplugin.InstalledPlugin{}
	for rows.Next() {
		item, err := scanInstalledPlugin(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) GetInstalled(ctx context.Context, pluginID string) (domainplugin.InstalledPlugin, error) {
	return r.getInstalled(ctx, pluginID, false)
}

func (r *Repository) getInstalled(ctx context.Context, pluginID string, lock bool) (domainplugin.InstalledPlugin, error) {
	query := `SELECT ` + installedColumns + ` FROM installed_plugins WHERE id = ?`
	if lock {
		query += ` FOR UPDATE`
	}
	row := dbtx.DB(ctx, r.db).Raw(query, pluginID).Row()
	item, err := scanInstalledPlugin(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainplugin.InstalledPlugin{}, fmt.Errorf("%w: plugin not found", apperrors.ErrNotFound)
		}
		return domainplugin.InstalledPlugin{}, err
	}
	return item, nil
}

// UpsertInstalled writes direct fixtures and seed data.
func (r *Repository) UpsertInstalled(ctx context.Context, item domainplugin.InstalledPlugin) (domainplugin.InstalledPlugin, error) {
	_, err := r.writeInstalled(ctx, item, `ON CONFLICT (id) DO UPDATE SET
		name = EXCLUDED.name, version = EXCLUDED.version, publisher = EXCLUDED.publisher,
		type = EXCLUDED.type, status = EXCLUDED.status, source = EXCLUDED.source,
		manifest = EXCLUDED.manifest, checksum_status = EXCLUDED.checksum_status,
		signature_status = EXCLUDED.signature_status, requested_permissions = EXCLUDED.requested_permissions,
		configured_secret_refs = EXCLUDED.configured_secret_refs, updated_at = EXCLUDED.updated_at,
		enabled_at = EXCLUDED.enabled_at, disabled_at = EXCLUDED.disabled_at, metadata = EXCLUDED.metadata,
		source_info = EXCLUDED.source_info`)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	return r.GetInstalled(ctx, item.ID)
}

// CreateInstalled reserves the plugin ID before executing any install-only side effects.
func (r *Repository) CreateInstalled(ctx context.Context, item domainplugin.InstalledPlugin, onCreate func(context.Context) error) (domainplugin.InstalledPlugin, bool, error) {
	var installed domainplugin.InstalledPlugin
	var created bool
	err := dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		var err error
		created, err = r.writeInstalled(ctx, item, "ON CONFLICT (id) DO NOTHING")
		if err != nil {
			return err
		}
		if created && onCreate != nil {
			if err := onCreate(ctx); err != nil {
				return err
			}
		}
		installed, err = r.GetInstalled(ctx, item.ID)
		return err
	})
	if err != nil {
		return domainplugin.InstalledPlugin{}, false, err
	}
	return installed, created, nil
}

// UpdateInstalled locks and verifies the original snapshot before running update side effects.
func (r *Repository) UpdateInstalled(ctx context.Context, expected, item domainplugin.InstalledPlugin, beforeUpdate func(context.Context, *domainplugin.InstalledPlugin) error) (domainplugin.InstalledPlugin, error) {
	var updated domainplugin.InstalledPlugin
	err := dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		current, err := r.getInstalled(ctx, expected.ID, true)
		if errors.Is(err, apperrors.ErrNotFound) {
			return fmt.Errorf("%w: plugin was removed while updating", apperrors.ErrConflict)
		}
		if err != nil {
			return err
		}
		if !current.InstalledAt.Equal(expected.InstalledAt) || !current.UpdatedAt.Equal(expected.UpdatedAt) ||
			current.Source != expected.Source || !sameSourceInfo(current.SourceInfo, expected.SourceInfo) {
			return fmt.Errorf("%w: plugin changed while updating", apperrors.ErrConflict)
		}
		if err := validateInstallationUpdate(current, item); err != nil {
			return err
		}
		if beforeUpdate != nil {
			if err := beforeUpdate(ctx, &item); err != nil {
				return err
			}
			if err := validateInstallationUpdate(current, item); err != nil {
				return err
			}
		}
		updated, err = r.updateInstalled(ctx, item)
		return err
	})
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	return updated, nil
}

func sameSourceInfo(current, expected *domainplugin.PluginSourceInfo) bool {
	if current == nil || expected == nil {
		return current == expected
	}
	return *current == *expected
}

func validateInstallationUpdate(current, item domainplugin.InstalledPlugin) error {
	if current.ID != item.ID {
		return fmt.Errorf("%w: plugin ID changed while updating", apperrors.ErrConflict)
	}
	source, next := current.SourceInfo, item.SourceInfo
	if source != nil && (next == nil || source.Kind != next.Kind ||
		source.CanonicalSource != next.CanonicalSource || source.SourceID != next.SourceID ||
		source.RepositoryURL != next.RepositoryURL) {
		return fmt.Errorf("%w: plugin source binding cannot be changed", apperrors.ErrConflict)
	}
	return nil
}

func (r *Repository) updateInstalled(ctx context.Context, item domainplugin.InstalledPlugin) (domainplugin.InstalledPlugin, error) {
	fields, err := marshalInstalledJSON(item)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	// Advance the revision even when two updates have the same PostgreSQL timestamp.
	row := dbtx.DB(ctx, r.db).Raw(`
		UPDATE installed_plugins SET
			name = ?, version = ?, publisher = ?, type = ?, status = ?, source = ?,
			manifest = ?::jsonb, checksum_status = ?, signature_status = ?, requested_permissions = ?::jsonb,
			configured_secret_refs = ?::jsonb, updated_at = GREATEST(?::timestamp, updated_at + INTERVAL '1 microsecond'),
			enabled_at = ?, disabled_at = ?, metadata = ?::jsonb, source_info = ?::jsonb
		WHERE id = ?
		RETURNING `+installedColumns,
		item.Name, item.Version, item.Publisher, item.Type, item.Status, item.Source,
		fields.manifest, item.ChecksumStatus, item.SignatureStatus, fields.permissions,
		fields.secretRefs, item.UpdatedAt, item.EnabledAt, item.DisabledAt, fields.metadata, fields.sourceInfo, item.ID,
	).Row()
	return scanInstalledPlugin(row)
}

func (r *Repository) writeInstalled(ctx context.Context, item domainplugin.InstalledPlugin, conflictClause string) (bool, error) {
	fields, err := marshalInstalledJSON(item)
	if err != nil {
		return false, err
	}
	result := dbtx.DB(ctx, r.db).Exec(`
		INSERT INTO installed_plugins (`+installedColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?, ?, ?::jsonb, ?::jsonb, ?, ?, ?, ?, ?, ?::jsonb, ?::jsonb)
	`+conflictClause, item.ID, item.Name, item.Version, item.Publisher, item.Type, item.Status, item.Source, fields.manifest, item.ChecksumStatus,
		item.SignatureStatus, fields.permissions, fields.secretRefs, item.InstalledBy, item.InstalledAt, item.UpdatedAt,
		item.EnabledAt, item.DisabledAt, fields.metadata, fields.sourceInfo)
	return result.RowsAffected == 1, result.Error
}

type installedJSON struct {
	manifest, permissions, secretRefs, metadata string
	sourceInfo                                  *string
}

func marshalInstalledJSON(item domainplugin.InstalledPlugin) (installedJSON, error) {
	var fields installedJSON
	var err error
	fields.manifest, err = marshalJSON(item.Manifest)
	if err != nil {
		return installedJSON{}, err
	}
	fields.permissions, err = marshalJSON(item.RequestedPermissions)
	if err != nil {
		return installedJSON{}, err
	}
	fields.secretRefs, err = marshalJSON(item.ConfiguredSecretRefs)
	if err != nil {
		return installedJSON{}, err
	}
	fields.metadata, err = marshalJSON(item.Metadata)
	if err != nil {
		return installedJSON{}, err
	}
	if item.SourceInfo != nil {
		sourceInfo, err := marshalJSON(item.SourceInfo)
		if err != nil {
			return installedJSON{}, err
		}
		fields.sourceInfo = &sourceInfo
	}
	return fields, nil
}

func (r *Repository) RemoveInstalled(ctx context.Context, pluginID string, retire func(context.Context, domainplugin.InstalledPlugin) error) (domainplugin.InstalledPlugin, error) {
	var item domainplugin.InstalledPlugin
	err := dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		row := dbtx.DB(ctx, r.db).Raw(`
			DELETE FROM installed_plugins WHERE id = ?
			RETURNING `+installedColumns, pluginID).Row()
		removed, err := scanInstalledPlugin(row)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: plugin not found", apperrors.ErrNotFound)
		}
		if err != nil {
			return err
		}
		item = removed
		return retire(ctx, item)
	})
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	return item, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanInstalledPlugin(row scanner) (domainplugin.InstalledPlugin, error) {
	var item domainplugin.InstalledPlugin
	var manifestRaw, permissionsRaw, secretRefsRaw, metadataRaw, sourceInfoRaw []byte
	var enabledAt, disabledAt sql.NullTime
	if err := row.Scan(
		&item.ID,
		&item.Name,
		&item.Version,
		&item.Publisher,
		&item.Type,
		&item.Status,
		&item.Source,
		&manifestRaw,
		&item.ChecksumStatus,
		&item.SignatureStatus,
		&permissionsRaw,
		&secretRefsRaw,
		&item.InstalledBy,
		&item.InstalledAt,
		&item.UpdatedAt,
		&enabledAt,
		&disabledAt,
		&metadataRaw,
		&sourceInfoRaw,
	); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	if err := unmarshalJSON(manifestRaw, &item.Manifest); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	var permissions domainplugin.PluginPermissionRequest
	if err := unmarshalJSON(permissionsRaw, &permissions); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	item.RequestedPermissions = &permissions
	if err := unmarshalJSON(secretRefsRaw, &item.ConfiguredSecretRefs); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	if err := unmarshalJSON(metadataRaw, &item.Metadata); err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	if len(sourceInfoRaw) != 0 {
		if err := json.Unmarshal(sourceInfoRaw, &item.SourceInfo); err != nil {
			return domainplugin.InstalledPlugin{}, err
		}
	}
	if enabledAt.Valid {
		item.EnabledAt = &enabledAt.Time
	}
	if disabledAt.Valid {
		item.DisabledAt = &disabledAt.Time
	}
	if item.ConfiguredSecretRefs == nil {
		item.ConfiguredSecretRefs = map[string]string{}
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func marshalJSON(value any) (string, error) {
	if value == nil {
		value = map[string]any{}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func unmarshalJSON(raw []byte, out any) error {
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	return json.Unmarshal(raw, out)
}
