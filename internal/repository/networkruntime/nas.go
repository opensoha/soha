package networkruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	domainnetworkruntime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	networkaccessrepo "github.com/opensoha/soha/internal/repository/networkaccess"
	"gorm.io/gorm"
)

func (r *Repository) NASBinding(ctx context.Context, runtimeID, nasID string) (domainnetworkaccess.NASBinding, error) {
	return networkaccessrepo.New(r.db).FindActiveNASBinding(ctx, runtimeID, nasID)
}

func (r *Repository) NetworkSubject(ctx context.Context, subjectID string) (domainnetworkaccess.Subject, error) {
	return networkaccessrepo.New(r.db).GetSubject(ctx, subjectID)
}

func (r *Repository) NetworkDevice(ctx context.Context, deviceID string) (domainnetworkaccess.Device, error) {
	return networkaccessrepo.New(r.db).GetDevice(ctx, deviceID)
}

func (r *Repository) NetworkSite(ctx context.Context, siteID string) (domainnetworkaccess.Site, error) {
	return networkaccessrepo.New(r.db).GetSite(ctx, siteID)
}

func (r *Repository) SiteProfileBinding(ctx context.Context, siteID, accessProfile string) (domainnetworkaccess.SiteProfileBinding, error) {
	return networkaccessrepo.New(r.db).FindSiteProfileBinding(ctx, siteID, accessProfile)
}

func (r *Repository) SaveNASAuthorization(ctx context.Context, authorization domainnetworkruntime.NASAuthorization) (domainnetworkruntime.NASAuthorization, error) {
	var saved domainnetworkruntime.NASAuthorization
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-nas-authorization:"+authorization.RuntimeID+":"+authorization.RequestID).Error; err != nil {
			return err
		}
		var existing nasAuthorizationRow
		err := tx.Where("tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND request_id = ?", authorization.RuntimeID, authorization.RequestID).First(&existing).Error
		if err == nil {
			if existing.RequestHash != authorization.RequestHash {
				return apperrors.NewBusiness(apperrors.ErrConflict, "nas_request_reused", "The NAS request ID was already used with different content.", "NAS 请求 ID 已被不同内容使用。")
			}
			decoded, err := existing.domain()
			saved = decoded
			return err
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if authorization.Decision == domainnetworkaccess.DecisionAllow {
			status := nasSessionStatus(authorization.AccessProfile)
			if err := tx.Exec(`INSERT INTO network_runtime_sessions (id, runtime_id, subject_id, device_id, site_id, mode, access_profile, status, policy_version, configuration_version, posture_version, valid_until, nas_id, authentication_method, created_at, updated_at) VALUES (?, ?, ?, ?, (SELECT site_id FROM network_access_nas_bindings WHERE tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND nas_id = ? AND status = 'active' LIMIT 1), ?, ?, ?, ?, 0, 0, ?, ?, ?, ?, ?)`, authorization.SessionID, authorization.RuntimeID, authorization.SubjectID, authorization.DeviceID, authorization.RuntimeID, authorization.NASID, domainnetworkaccess.ModeInternalDirect, authorization.AccessProfile, status, authorization.PolicyVersion, authorization.ValidUntil, authorization.NASID, authorization.AuthenticationMethod, authorization.CreatedAt, authorization.CreatedAt).Error; err != nil {
				return err
			}
		}
		row, err := nasAuthorizationRowFromDomain(authorization)
		if err != nil {
			return err
		}
		if err := tx.Create(&row).Error; err != nil {
			return err
		}
		saved = authorization
		return nil
	})
	return saved, err
}

func (r *Repository) ClaimNASSessionCommand(ctx context.Context, runtimeID string, now time.Time) (domainnetworkaccess.SessionCommand, error) {
	return networkaccessrepo.New(r.db).ClaimNASSessionCommand(ctx, runtimeID, now)
}

func (r *Repository) CompleteNASSessionCommand(ctx context.Context, runtimeID string, result networkprotocol.NASSessionCommandResult, now time.Time) error {
	return networkaccessrepo.New(r.db).CompleteNASSessionCommand(ctx, runtimeID, result, now)
}

func nasSessionStatus(profile string) string {
	switch profile {
	case domainnetworkaccess.ProfileFull:
		return "active"
	case domainnetworkaccess.ProfileQuarantine:
		return "quarantine"
	case domainnetworkaccess.ProfileOnboarding:
		return "pending"
	default:
		return "restricted"
	}
}

type nasAuthorizationRow struct {
	TenantID             string        `gorm:"primaryKey;column:tenant_id"`
	WorkspaceID          string        `gorm:"primaryKey;column:workspace_id"`
	RuntimeID            string        `gorm:"primaryKey;column:runtime_id"`
	RequestID            string        `gorm:"primaryKey;column:request_id"`
	RequestHash          string        `gorm:"column:request_hash"`
	NASID                string        `gorm:"column:nas_id"`
	SubjectID            string        `gorm:"column:subject_id"`
	DeviceID             string        `gorm:"column:device_id"`
	AuthenticationMethod string        `gorm:"column:authentication_method"`
	SessionID            string        `gorm:"column:session_id"`
	Decision             string        `gorm:"column:decision"`
	AccessProfile        string        `gorm:"column:access_profile"`
	PolicyVersion        int           `gorm:"column:policy_version"`
	ReasonCode           string        `gorm:"column:reason_code"`
	RadiusAttributes     *jsonDocument `gorm:"column:radius_attributes;type:jsonb"`
	ValidUntil           time.Time     `gorm:"column:valid_until"`
	CreatedAt            time.Time     `gorm:"column:created_at"`
}

func (nasAuthorizationRow) TableName() string { return "network_nas_authorizations" }

func nasAuthorizationRowFromDomain(value domainnetworkruntime.NASAuthorization) (nasAuthorizationRow, error) {
	row := nasAuthorizationRow{TenantID: "default", WorkspaceID: "default", RuntimeID: value.RuntimeID, RequestID: value.RequestID, RequestHash: value.RequestHash, NASID: value.NASID, SubjectID: value.SubjectID, DeviceID: value.DeviceID, AuthenticationMethod: value.AuthenticationMethod, SessionID: value.SessionID, Decision: value.Decision, AccessProfile: value.AccessProfile, PolicyVersion: value.PolicyVersion, ReasonCode: value.ReasonCode, ValidUntil: value.ValidUntil, CreatedAt: value.CreatedAt}
	if value.RadiusAttributes != nil {
		encoded, err := json.Marshal(value.RadiusAttributes)
		if err != nil {
			return row, err
		}
		document := jsonDocument(encoded)
		row.RadiusAttributes = &document
	}
	return row, nil
}

func (row nasAuthorizationRow) domain() (domainnetworkruntime.NASAuthorization, error) {
	value := domainnetworkruntime.NASAuthorization{RequestID: row.RequestID, RequestHash: row.RequestHash, RuntimeID: row.RuntimeID, NASID: row.NASID, SubjectID: row.SubjectID, DeviceID: row.DeviceID, AuthenticationMethod: row.AuthenticationMethod, SessionID: row.SessionID, Decision: row.Decision, AccessProfile: row.AccessProfile, PolicyVersion: row.PolicyVersion, ReasonCode: row.ReasonCode, ValidUntil: row.ValidUntil, CreatedAt: row.CreatedAt}
	if row.RadiusAttributes != nil {
		var attributes networkprotocol.RadiusAttributes
		if err := json.Unmarshal(*row.RadiusAttributes, &attributes); err != nil {
			return value, fmt.Errorf("decode NAS authorization RADIUS attributes: %w", err)
		}
		value.RadiusAttributes = &attributes
	}
	return value, nil
}
