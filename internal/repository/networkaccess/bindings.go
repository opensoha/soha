package networkaccess

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

const nasBindingColumns = `id, nas_id, runtime_id, site_id, name, COALESCE(access_medium, ''), COALESCE(device_type, ''), COALESCE(ssid, ''), COALESCE(management_address, ''), status, coa_supported, disconnect_supported, created_at, updated_at`

func (r *Repository) ListNASBindings(ctx context.Context, filter domainnetworkaccess.NASBindingFilter) ([]domainnetworkaccess.NASBinding, error) {
	query := `SELECT ` + nasBindingColumns + ` FROM network_access_nas_bindings WHERE tenant_id = 'default' AND workspace_id = 'default'`
	args := []any{}
	query, args = addFilter(query, args, "site_id", filter.SiteID)
	query, args = addFilter(query, args, "runtime_id", filter.RuntimeID)
	query, args = addFilter(query, args, "status", filter.Status)
	query += ` ORDER BY name ASC, id ASC LIMIT ?`
	args = append(args, filter.Limit)
	return queryMany(ctx, r.db, query, args, scanNASBinding)
}

func (r *Repository) GetNASBinding(ctx context.Context, id string) (domainnetworkaccess.NASBinding, error) {
	return scanOne(r.db.WithContext(ctx).Raw(`SELECT `+nasBindingColumns+` FROM network_access_nas_bindings WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? LIMIT 1`, id).Row(), scanNASBinding, "NAS binding")
}

func (r *Repository) FindActiveNASBinding(ctx context.Context, runtimeID, nasID string) (domainnetworkaccess.NASBinding, error) {
	return scanOne(r.db.WithContext(ctx).Raw(`SELECT `+nasBindingColumns+` FROM network_access_nas_bindings WHERE tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND nas_id = ? AND status = 'active' LIMIT 1`, runtimeID, nasID).Row(), scanNASBinding, "active NAS binding")
}

func (r *Repository) CreateNASBinding(ctx context.Context, item domainnetworkaccess.NASBinding) (domainnetworkaccess.NASBinding, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ensureNASRuntime(tx, item.RuntimeID); err != nil {
			return err
		}
		return tx.Exec(`INSERT INTO network_access_nas_bindings (id, nas_id, runtime_id, site_id, name, access_medium, device_type, ssid, management_address, status, coa_supported, disconnect_supported, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, item.ID, item.NASID, item.RuntimeID, item.SiteID, item.Name, nullableText(item.AccessMedium), nullableText(item.DeviceType), nullableText(item.SSID), nullableText(item.ManagementAddress), item.Status, item.CoASupported, item.DisconnectSupported, item.CreatedAt, item.UpdatedAt).Error
	})
	if err != nil {
		return domainnetworkaccess.NASBinding{}, normalizeDatabaseError(err)
	}
	return r.GetNASBinding(ctx, item.ID)
}

func (r *Repository) UpdateNASBinding(ctx context.Context, id string, input domainnetworkaccess.NASBindingInput, updatedAt time.Time) (domainnetworkaccess.NASBinding, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := ensureNASRuntime(tx, input.RuntimeID); err != nil {
			return err
		}
		return mutationError(tx.Exec(`UPDATE network_access_nas_bindings SET nas_id = ?, runtime_id = ?, site_id = ?, name = ?, access_medium = ?, device_type = ?, ssid = ?, management_address = ?, status = ?, coa_supported = ?, disconnect_supported = ?, updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ?`, input.NASID, input.RuntimeID, input.SiteID, input.Name, nullableText(input.AccessMedium), nullableText(input.DeviceType), nullableText(input.SSID), nullableText(input.ManagementAddress), input.Status, input.CoASupported, input.DisconnectSupported, updatedAt, id), "NAS binding")
	})
	if err != nil {
		return domainnetworkaccess.NASBinding{}, normalizeDatabaseError(err)
	}
	return r.GetNASBinding(ctx, id)
}

func (r *Repository) DeleteNASBinding(ctx context.Context, id string) error {
	return mutationError(r.db.WithContext(ctx).Exec(`DELETE FROM network_access_nas_bindings WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ?`, id), "NAS binding")
}

const siteProfileBindingColumns = `id, site_id, access_profile, vlan_id, filter_id, session_timeout_seconds, created_at, updated_at`

func (r *Repository) ListSiteProfileBindings(ctx context.Context, filter domainnetworkaccess.SiteProfileBindingFilter) ([]domainnetworkaccess.SiteProfileBinding, error) {
	query := `SELECT ` + siteProfileBindingColumns + ` FROM network_access_site_profile_bindings WHERE tenant_id = 'default' AND workspace_id = 'default'`
	args := []any{}
	query, args = addFilter(query, args, "site_id", filter.SiteID)
	query, args = addFilter(query, args, "access_profile", filter.AccessProfile)
	query += ` ORDER BY site_id ASC, access_profile ASC, id ASC LIMIT ?`
	args = append(args, filter.Limit)
	return queryMany(ctx, r.db, query, args, scanSiteProfileBinding)
}

func (r *Repository) GetSiteProfileBinding(ctx context.Context, id string) (domainnetworkaccess.SiteProfileBinding, error) {
	return scanOne(r.db.WithContext(ctx).Raw(`SELECT `+siteProfileBindingColumns+` FROM network_access_site_profile_bindings WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? LIMIT 1`, id).Row(), scanSiteProfileBinding, "site profile binding")
}

func (r *Repository) FindSiteProfileBinding(ctx context.Context, siteID, accessProfile string) (domainnetworkaccess.SiteProfileBinding, error) {
	return scanOne(r.db.WithContext(ctx).Raw(`SELECT `+siteProfileBindingColumns+` FROM network_access_site_profile_bindings WHERE tenant_id = 'default' AND workspace_id = 'default' AND site_id = ? AND access_profile = ? LIMIT 1`, siteID, accessProfile).Row(), scanSiteProfileBinding, "site profile binding")
}

func (r *Repository) CreateSiteProfileBinding(ctx context.Context, item domainnetworkaccess.SiteProfileBinding) (domainnetworkaccess.SiteProfileBinding, error) {
	err := r.db.WithContext(ctx).Exec(`INSERT INTO network_access_site_profile_bindings (id, site_id, access_profile, vlan_id, filter_id, session_timeout_seconds, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, item.ID, item.SiteID, item.AccessProfile, nullablePositiveInt(item.VLANID), nullableText(item.FilterID), item.SessionTimeoutSeconds, item.CreatedAt, item.UpdatedAt).Error
	if err != nil {
		return domainnetworkaccess.SiteProfileBinding{}, normalizeDatabaseError(err)
	}
	return r.GetSiteProfileBinding(ctx, item.ID)
}

func (r *Repository) UpdateSiteProfileBinding(ctx context.Context, id string, input domainnetworkaccess.SiteProfileBindingInput, updatedAt time.Time) (domainnetworkaccess.SiteProfileBinding, error) {
	result := r.db.WithContext(ctx).Exec(`UPDATE network_access_site_profile_bindings SET site_id = ?, access_profile = ?, vlan_id = ?, filter_id = ?, session_timeout_seconds = ?, updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ?`, input.SiteID, input.AccessProfile, nullablePositiveInt(input.VLANID), nullableText(input.FilterID), input.SessionTimeoutSeconds, updatedAt, id)
	if err := mutationError(result, "site profile binding"); err != nil {
		return domainnetworkaccess.SiteProfileBinding{}, err
	}
	return r.GetSiteProfileBinding(ctx, id)
}

func (r *Repository) DeleteSiteProfileBinding(ctx context.Context, id string) error {
	return mutationError(r.db.WithContext(ctx).Exec(`DELETE FROM network_access_site_profile_bindings WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ?`, id), "site profile binding")
}

func ensureNASRuntime(tx *gorm.DB, runtimeID string) error {
	var exists bool
	if err := tx.Raw(`SELECT EXISTS (SELECT 1 FROM network_runtime_credentials WHERE tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND runtime_kind = 'nas')`, runtimeID).Scan(&exists).Error; err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("%w: registered NAS runtime not found", apperrors.ErrConflict)
	}
	return nil
}

func scanNASBinding(row scanner) (domainnetworkaccess.NASBinding, error) {
	var item domainnetworkaccess.NASBinding
	err := row.Scan(&item.ID, &item.NASID, &item.RuntimeID, &item.SiteID, &item.Name, &item.AccessMedium, &item.DeviceType, &item.SSID, &item.ManagementAddress, &item.Status, &item.CoASupported, &item.DisconnectSupported, &item.CreatedAt, &item.UpdatedAt)
	return item, err
}

func scanSiteProfileBinding(row scanner) (domainnetworkaccess.SiteProfileBinding, error) {
	var item domainnetworkaccess.SiteProfileBinding
	var vlan sql.NullInt64
	var filter sql.NullString
	if err := row.Scan(&item.ID, &item.SiteID, &item.AccessProfile, &vlan, &filter, &item.SessionTimeoutSeconds, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return item, err
	}
	if vlan.Valid {
		item.VLANID = int(vlan.Int64)
	}
	if filter.Valid {
		item.FilterID = filter.String
	}
	return item, nil
}

func nullablePositiveInt(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

func nullableText(value string) any {
	if value == "" {
		return nil
	}
	return value
}
