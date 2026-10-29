package networkruntime

import (
	"context"
	"database/sql"
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
	if authorization.AttributeVersion != "" && authorization.AttributeVersion != domainnetworkaccess.AdmissionDecisionVersion(authorization.InputsHash, authorization.PolicyVersion, authorization.ResponsePolicyHash, authorization.PresenceHash, authorization.RestrictionEpoch) {
		return domainnetworkruntime.NASAuthorization{}, staleNASAuthorization()
	}
	var saved domainnetworkruntime.NASAuthorization
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockNASAuthorizationPolicy(tx, authorization); err != nil {
			return err
		}

		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-nas-accounting:"+authorization.RuntimeID+":"+authorization.NASID).Error; err != nil {
			return err
		}
		if err := validateEndpointAuthorization(tx, authorization); err != nil {
			return err
		}
		var restartedAt *time.Time
		if err := tx.Raw(`SELECT observed_at FROM network_nas_accounting_epochs WHERE tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND nas_id = ?`, authorization.RuntimeID, authorization.NASID).Row().Scan(&restartedAt); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if restartedAt != nil && authorization.CreatedAt.Before(*restartedAt) {
			return staleNASAuthorization()
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-nas-authorization:"+authorization.RuntimeID+":"+authorization.RequestID).Error; err != nil {
			return err
		}
		decoded, exists, err := replayNASAuthorization(tx, authorization)
		if err != nil {
			return err
		}
		if exists {
			saved = decoded
			return nil
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

func replayNASAuthorization(tx *gorm.DB, authorization domainnetworkruntime.NASAuthorization) (domainnetworkruntime.NASAuthorization, bool, error) {
	var existing nasAuthorizationRow
	err := tx.Where("tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND request_id = ?", authorization.RuntimeID, authorization.RequestID).First(&existing).Error
	if err == nil {
		if existing.RequestHash != authorization.RequestHash {
			return domainnetworkruntime.NASAuthorization{}, false, apperrors.NewBusiness(apperrors.ErrConflict, "nas_request_reused", "The NAS request ID was already used with different content.", "NAS 请求 ID 已被不同内容使用。")
		}
		decoded, err := existing.domain()
		if err != nil {
			return domainnetworkruntime.NASAuthorization{}, false, err
		}
		if !nasAuthorizationReplayMatches(decoded, authorization) {
			return domainnetworkruntime.NASAuthorization{}, false, staleNASAuthorization()
		}
		if decoded.Decision == domainnetworkaccess.DecisionAllow {
			var current bool
			if err := tx.Raw(`SELECT EXISTS (SELECT 1 FROM network_runtime_sessions WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND runtime_id = ? AND policy_version = ? AND access_profile = ? AND status = ? AND valid_until > ?)`, decoded.SessionID, decoded.RuntimeID, decoded.PolicyVersion, decoded.AccessProfile, nasSessionStatus(decoded.AccessProfile), authorization.CreatedAt).Scan(&current).Error; err != nil {
				return domainnetworkruntime.NASAuthorization{}, false, err
			}
			if !current {
				return domainnetworkruntime.NASAuthorization{}, false, staleNASAuthorization()
			}
		}
		return decoded, true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return domainnetworkruntime.NASAuthorization{}, false, err
	}
	return domainnetworkruntime.NASAuthorization{}, false, nil
}

func validateEndpointAuthorization(tx *gorm.DB, authorization domainnetworkruntime.NASAuthorization) error {
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-endpoint-device:"+authorization.DeviceID).Error; err != nil {
		return err
	}
	repo := networkaccessrepo.New(tx)
	policies, err := repo.PublishedEndpointResponsePolicies(tx.Statement.Context)
	if err != nil {
		return err
	}
	// Existing callers without response metadata are safe only before any policy is published.
	if authorization.ResponsePolicyHash != domainnetworkaccess.ResponsePolicyHash(policies) && (authorization.ResponsePolicyHash != "" || len(policies) > 0) {
		return staleNASAuthorization()
	}
	presence, err := repo.EndpointPresence(tx.Statement.Context, authorization.DeviceID)
	if err != nil && !errors.Is(err, apperrors.ErrNotFound) {
		return err
	}
	if presence.PayloadHash != authorization.PresenceHash {
		return staleNASAuthorization()
	}
	binding, err := repo.FindActiveNASBinding(tx.Statement.Context, authorization.RuntimeID, authorization.NASID)
	if err != nil {
		return err
	}
	if authorization.InputsHash != "" {
		if err := validateNASAdmissionInputs(tx, authorization, binding); err != nil {
			return err
		}
	}
	floor, err := repo.EndpointRestriction(tx.Statement.Context, authorization.DeviceID, binding.SiteID)
	if err != nil {
		return err
	}
	if floor.Epoch != authorization.RestrictionEpoch || authorization.Decision == domainnetworkaccess.DecisionAllow && domainnetworkaccess.RestrictAccessProfile(authorization.AccessProfile, floor.AccessProfile) != authorization.AccessProfile {
		return staleNASAuthorization()
	}
	return nil
}

func staleNASAuthorization() error {
	return apperrors.NewBusiness(apperrors.ErrConflict, "nas_authorization_stale", "The NAS authorization is no longer current; authenticate with a new request ID.", "NAS 授权已失效，请使用新请求 ID 重新认证。")
}

func nasAuthorizationReplayMatches(saved, current domainnetworkruntime.NASAuthorization) bool {
	if !saved.ValidUntil.After(current.CreatedAt) || saved.PolicyVersion != current.PolicyVersion || saved.Decision != current.Decision || saved.AccessProfile != current.AccessProfile || saved.ReasonCode != current.ReasonCode || saved.CredentialID != current.CredentialID || saved.StationID != current.StationID || saved.InputsHash != current.InputsHash {
		return false
	}
	if saved.RadiusAttributes == nil || current.RadiusAttributes == nil {
		return saved.RadiusAttributes == nil && current.RadiusAttributes == nil
	}
	return *saved.RadiusAttributes == *current.RadiusAttributes
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
	AttributeVersion     string        `gorm:"column:decision_attribute_version"`
	InputsHash           string        `gorm:"column:admission_inputs_hash"`
	CredentialID         string        `gorm:"column:credential_id"`
	StationID            string        `gorm:"column:station_id"`
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
	row.CredentialID, row.StationID = value.CredentialID, value.StationID
	row.InputsHash = value.InputsHash
	row.AttributeVersion = value.AttributeVersion
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
	value.CredentialID, value.StationID = row.CredentialID, row.StationID
	value.InputsHash = row.InputsHash
	value.AttributeVersion = row.AttributeVersion
	if row.RadiusAttributes != nil {
		var attributes networkprotocol.RadiusAttributes
		if err := json.Unmarshal(*row.RadiusAttributes, &attributes); err != nil {
			return value, fmt.Errorf("decode NAS authorization RADIUS attributes: %w", err)
		}
		value.RadiusAttributes = &attributes
	}
	return value, nil
}

func validateNASAdmissionInputs(tx *gorm.DB, authorization domainnetworkruntime.NASAuthorization, binding domainnetworkaccess.NASBinding) error {
	repo := networkaccessrepo.New(tx)
	subject, err := repo.GetSubject(tx.Statement.Context, authorization.SubjectID)
	if err != nil {
		return err
	}
	device, err := repo.GetDevice(tx.Statement.Context, authorization.DeviceID)
	if err != nil {
		return err
	}
	site, err := repo.GetSite(tx.Statement.Context, binding.SiteID)
	if err != nil {
		return err
	}
	if authorization.Decision == domainnetworkaccess.DecisionAllow && site.DeviceGovernance == domainnetworkaccess.DeviceGovernanceVerified {
		if device.ManagementVerifiedUntil == nil || device.ManagementIssuerKey != site.ManagementIssuerKey || authorization.ValidUntil.After(*device.ManagementVerifiedUntil) || !device.ManagementVerifiedUntil.After(time.Now()) {
			return staleNASAuthorization()
		}
	}
	var profile *domainnetworkaccess.SiteProfileBinding
	if authorization.Decision == domainnetworkaccess.DecisionAllow {
		current, err := repo.FindSiteProfileBinding(tx.Statement.Context, binding.SiteID, authorization.AccessProfile)
		if err != nil {
			return err
		}
		profile = &current
	}
	if authorization.InputsHash != domainnetworkaccess.AdmissionAttributesHash(subject, device, site, binding, profile) {
		return staleNASAuthorization()
	}
	if authorization.Decision == domainnetworkaccess.DecisionAllow && authorization.CredentialID != "" {
		active, err := repo.EndpointCredentialCurrent(tx.Statement.Context, authorization.CredentialID, device.ID, subject.UserID, authorization.CreatedAt)
		if err != nil {
			return err
		}
		if !active {
			return staleNASAuthorization()
		}
	}
	return nil
}

func validateNASPolicyVersion(tx *gorm.DB, expected int) error {
	var policyVersion int
	if err := tx.Raw(`SELECT COALESCE(MAX(policy_version), 0) FROM network_access_policy_snapshots WHERE tenant_id = 'default' AND workspace_id = 'default'`).Scan(&policyVersion).Error; err != nil {
		return err
	}
	if policyVersion != expected {
		return staleNASAuthorization()
	}
	return nil
}

func lockNASAuthorizationPolicy(tx *gorm.DB, authorization domainnetworkruntime.NASAuthorization) error {
	if err := lockWireGuardMutations(tx); err != nil {
		return err
	}
	if err := ensureSubjectNotDLPRestricted(tx, authorization.SubjectID); err != nil {
		return err
	}
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-endpoint-response-policies:default:default").Error; err != nil {
		return err
	}
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-access-policy-snapshot:default:default").Error; err != nil {
		return err
	}
	if err := validateNASPolicyVersion(tx, authorization.PolicyVersion); err != nil {
		return err
	}

	return nil
}
