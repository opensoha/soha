package networkruntime

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	domainnetworkruntime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

func (r *Repository) CreateAccessGrant(ctx context.Context, grant domainnetworkruntime.AccessGrant) error {
	row, err := accessGrantRowFromDomain(grant)
	if err != nil {
		return err
	}
	return r.db.WithContext(ctx).Create(&row).Error
}

func (r *Repository) GetAccessGrant(ctx context.Context, id string, now time.Time) (domainnetworkruntime.AccessGrant, error) {
	if err := expireAccessGrants(r.db.WithContext(ctx), now); err != nil {
		return domainnetworkruntime.AccessGrant{}, err
	}
	var row accessGrantRow
	if err := r.db.WithContext(ctx).Where("tenant_id = 'default' AND workspace_id = 'default' AND id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domainnetworkruntime.AccessGrant{}, apperrors.ErrNotFound
		}
		return domainnetworkruntime.AccessGrant{}, err
	}
	return row.domain()
}

func (r *Repository) ListAccessGrants(ctx context.Context, filter domainnetworkruntime.AccessGrantFilter, now time.Time) ([]domainnetworkruntime.AccessGrant, error) {
	db := r.db.WithContext(ctx)
	if err := expireAccessGrants(db, now); err != nil {
		return nil, err
	}
	query := db.Where("tenant_id = 'default' AND workspace_id = 'default'")
	if filter.SubjectID != "" {
		query = query.Where("subject_id = ?", filter.SubjectID)
	}
	if filter.DeviceID != "" {
		query = query.Where("device_id = ?", filter.DeviceID)
	}
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	var rows []accessGrantRow
	if err := query.Order("created_at DESC, id DESC").Limit(filter.Limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]domainnetworkruntime.AccessGrant, 0, len(rows))
	for _, row := range rows {
		item, err := row.domain()
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *Repository) RevokeAccessGrant(ctx context.Context, id string, now time.Time) error {
	db := r.db.WithContext(ctx)
	if err := expireAccessGrants(db, now); err != nil {
		return err
	}
	result := db.Model(&accessGrantRow{}).
		Where("tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND status = ?", id, domainnetworkruntime.AccessGrantIssued).
		Updates(map[string]any{"status": domainnetworkruntime.AccessGrantRevoked, "token_hash": nil, "revoked_at": now, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return apperrors.ErrConflict
	}
	return nil
}

func expireAccessGrants(db *gorm.DB, now time.Time) error {
	return db.Model(&accessGrantRow{}).
		Where("tenant_id = 'default' AND workspace_id = 'default' AND status = ? AND expires_at <= ?", domainnetworkruntime.AccessGrantIssued, now).
		Updates(map[string]any{"status": domainnetworkruntime.AccessGrantExpired, "token_hash": nil, "updated_at": now}).Error
}

type accessGrantRow struct {
	ID               string       `gorm:"primaryKey;column:id"`
	TenantID         string       `gorm:"column:tenant_id"`
	WorkspaceID      string       `gorm:"column:workspace_id"`
	SubjectID        string       `gorm:"column:subject_id"`
	AuthSessionID    string       `gorm:"column:auth_session_id"`
	DeviceID         string       `gorm:"column:device_id"`
	SiteID           string       `gorm:"column:site_id"`
	NetworkSpaceID   string       `gorm:"column:network_space_id"`
	Mode             string       `gorm:"column:mode"`
	ResourceIDs      jsonDocument `gorm:"column:resource_ids;type:jsonb"`
	PolicyVersion    int          `gorm:"column:policy_version"`
	Status           string       `gorm:"column:status"`
	TokenHash        *string      `gorm:"column:token_hash"`
	SessionID        *string      `gorm:"column:session_id"`
	ResourceLeaseIDs jsonDocument `gorm:"column:resource_lease_ids;type:jsonb"`
	ReasonCode       string       `gorm:"column:reason_code"`
	ExpiresAt        time.Time    `gorm:"column:expires_at"`
	ConsumedAt       *time.Time   `gorm:"column:consumed_at"`
	RevokedAt        *time.Time   `gorm:"column:revoked_at"`
	CreatedBy        string       `gorm:"column:created_by"`
	CreatedAt        time.Time    `gorm:"column:created_at"`
	UpdatedAt        time.Time    `gorm:"column:updated_at"`
}

func (accessGrantRow) TableName() string { return "network_access_grants" }

func accessGrantRowFromDomain(grant domainnetworkruntime.AccessGrant) (accessGrantRow, error) {
	resources, err := json.Marshal(grant.ResourceIDs)
	if err != nil {
		return accessGrantRow{}, err
	}
	leases, err := json.Marshal(grant.ResourceLeaseIDs)
	if err != nil {
		return accessGrantRow{}, err
	}
	row := accessGrantRow{
		ID: grant.ID, TenantID: "default", WorkspaceID: "default", SubjectID: grant.SubjectID,
		AuthSessionID: grant.AuthSessionID, DeviceID: grant.DeviceID, SiteID: grant.SiteID, NetworkSpaceID: grant.NetworkSpaceID,
		Mode: grant.Mode, ResourceIDs: jsonDocument(resources), PolicyVersion: grant.PolicyVersion, Status: grant.Status,
		ReasonCode: grant.ReasonCode, ExpiresAt: grant.ExpiresAt, ConsumedAt: grant.ConsumedAt, RevokedAt: grant.RevokedAt,
		CreatedBy: grant.CreatedBy, CreatedAt: grant.CreatedAt, UpdatedAt: grant.UpdatedAt, ResourceLeaseIDs: jsonDocument(leases),
	}
	if grant.TokenHash != "" {
		row.TokenHash = &grant.TokenHash
	}
	if grant.SessionID != "" {
		row.SessionID = &grant.SessionID
	}
	return row, nil
}

func (row accessGrantRow) domain() (domainnetworkruntime.AccessGrant, error) {
	var resources, leases []string
	if err := json.Unmarshal(row.ResourceIDs, &resources); err != nil {
		return domainnetworkruntime.AccessGrant{}, err
	}
	if err := json.Unmarshal(row.ResourceLeaseIDs, &leases); err != nil {
		return domainnetworkruntime.AccessGrant{}, err
	}
	grant := domainnetworkruntime.AccessGrant{
		ID: row.ID, SubjectID: row.SubjectID, AuthSessionID: row.AuthSessionID, DeviceID: row.DeviceID,
		SiteID: row.SiteID, NetworkSpaceID: row.NetworkSpaceID, Mode: row.Mode, ResourceIDs: resources,
		PolicyVersion: row.PolicyVersion, Status: row.Status, ResourceLeaseIDs: leases, ReasonCode: row.ReasonCode,
		ExpiresAt: row.ExpiresAt, ConsumedAt: row.ConsumedAt, RevokedAt: row.RevokedAt, CreatedBy: row.CreatedBy,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	if row.TokenHash != nil {
		grant.TokenHash = *row.TokenHash
	}
	if row.SessionID != nil {
		grant.SessionID = *row.SessionID
	}
	return grant, nil
}
