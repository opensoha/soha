package plugin

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	domainplugin "github.com/opensoha/soha/internal/domain/plugin"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

type Repository struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListInstalled(ctx context.Context) ([]domainplugin.InstalledPlugin, error) {
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT id, name, version, publisher, type, status, source, manifest, checksum_status, signature_status,
		       requested_permissions, configured_secret_refs, installed_by, installed_at, updated_at,
		       enabled_at, disabled_at, metadata
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
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, name, version, publisher, type, status, source, manifest, checksum_status, signature_status,
		       requested_permissions, configured_secret_refs, installed_by, installed_at, updated_at,
		       enabled_at, disabled_at, metadata
		FROM installed_plugins
		WHERE id = ?
		LIMIT 1
	`, pluginID).Row()
	item, err := scanInstalledPlugin(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domainplugin.InstalledPlugin{}, fmt.Errorf("%w: plugin not found", apperrors.ErrNotFound)
		}
		return domainplugin.InstalledPlugin{}, err
	}
	return item, nil
}

func (r *Repository) UpsertInstalled(ctx context.Context, item domainplugin.InstalledPlugin) (domainplugin.InstalledPlugin, error) {
	manifest, err := marshalJSON(item.Manifest)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	requestedPermissions, err := marshalJSON(item.RequestedPermissions)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	configuredSecretRefs, err := marshalJSON(item.ConfiguredSecretRefs)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	metadata, err := marshalJSON(item.Metadata)
	if err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO installed_plugins (
			id, name, version, publisher, type, status, source, manifest, checksum_status, signature_status,
			requested_permissions, configured_secret_refs, installed_by, installed_at, updated_at,
			enabled_at, disabled_at, metadata
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?::jsonb, ?, ?, ?::jsonb, ?::jsonb, ?, ?, ?, ?, ?, ?::jsonb)
		ON CONFLICT (id) DO UPDATE SET
			name = EXCLUDED.name,
			version = EXCLUDED.version,
			publisher = EXCLUDED.publisher,
			type = EXCLUDED.type,
			status = EXCLUDED.status,
			source = EXCLUDED.source,
			manifest = EXCLUDED.manifest,
			checksum_status = EXCLUDED.checksum_status,
			signature_status = EXCLUDED.signature_status,
			requested_permissions = EXCLUDED.requested_permissions,
			configured_secret_refs = EXCLUDED.configured_secret_refs,
			updated_at = EXCLUDED.updated_at,
			enabled_at = EXCLUDED.enabled_at,
			disabled_at = EXCLUDED.disabled_at,
			metadata = EXCLUDED.metadata
	`, item.ID, item.Name, item.Version, item.Publisher, item.Type, item.Status, item.Source, manifest, item.ChecksumStatus,
		item.SignatureStatus, requestedPermissions, configuredSecretRefs, item.InstalledBy, item.InstalledAt, item.UpdatedAt,
		item.EnabledAt, item.DisabledAt, metadata).Error; err != nil {
		return domainplugin.InstalledPlugin{}, err
	}
	return r.GetInstalled(ctx, item.ID)
}

func (r *Repository) DeleteInstalled(ctx context.Context, pluginID string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM installed_plugins WHERE id = ?`, pluginID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: plugin not found", apperrors.ErrNotFound)
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanInstalledPlugin(row scanner) (domainplugin.InstalledPlugin, error) {
	var item domainplugin.InstalledPlugin
	var manifestRaw, permissionsRaw, secretRefsRaw, metadataRaw []byte
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
