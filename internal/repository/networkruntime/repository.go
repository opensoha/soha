package networkruntime

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	domainnetworkruntime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct{ db *gorm.DB }

func New(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) CreateEnrollment(ctx context.Context, challenge domainnetworkruntime.EnrollmentChallenge) error {
	return r.db.WithContext(ctx).Create(enrollmentRowFromDomain(challenge)).Error
}

func (r *Repository) GetEnrollment(ctx context.Context, id string) (domainnetworkruntime.EnrollmentChallenge, error) {
	var row enrollmentRow
	if err := r.db.WithContext(ctx).Where("tenant_id = 'default' AND workspace_id = 'default' AND id = ?", id).First(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domainnetworkruntime.EnrollmentChallenge{}, apperrors.ErrNotFound
		}
		return domainnetworkruntime.EnrollmentChallenge{}, err
	}
	return row.domain(), nil
}

func (r *Repository) ListEnrollments(ctx context.Context, limit int) ([]domainnetworkruntime.EnrollmentChallenge, error) {
	var rows []enrollmentRow
	if err := r.db.WithContext(ctx).Where("tenant_id = 'default' AND workspace_id = 'default'").Order("created_at DESC, id DESC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]domainnetworkruntime.EnrollmentChallenge, 0, len(rows))
	for _, row := range rows {
		items = append(items, row.domain())
	}
	return items, nil
}

func (r *Repository) RevokeEnrollment(ctx context.Context, id string, now time.Time) error {
	result := r.db.WithContext(ctx).Model(&enrollmentRow{}).
		Where("tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND status = ?", id, domainnetworkruntime.EnrollmentPending).
		Updates(map[string]any{"status": domainnetworkruntime.EnrollmentRevoked, "revoked_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return apperrors.ErrConflict
	}
	return nil
}

func (r *Repository) LatestSnapshot(ctx context.Context) (domainnetworkruntime.PolicySnapshot, error) {
	var version int
	var hash string
	var policies, protected []byte
	var publishedAt time.Time
	err := r.db.WithContext(ctx).Raw(`SELECT policy_version, content_hash, policies, protected_resource_ids, published_at FROM network_access_policy_snapshots WHERE tenant_id = 'default' AND workspace_id = 'default' ORDER BY policy_version DESC LIMIT 1`).Row().Scan(&version, &hash, &policies, &protected, &publishedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domainnetworkruntime.PolicySnapshot{}, apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "policy_snapshot_unavailable", "No published network policy snapshot is available.", "没有可用的已发布网络策略快照。")
	}
	if err != nil {
		return domainnetworkruntime.PolicySnapshot{}, err
	}
	var protectedIDs []string
	if err := json.Unmarshal(protected, &protectedIDs); err != nil {
		return domainnetworkruntime.PolicySnapshot{}, fmt.Errorf("decode protected resource IDs: %w", err)
	}
	return domainnetworkruntime.PolicySnapshot{PolicyVersion: version, ContentHash: hash, Policies: slices.Clone(policies), ProtectedResourceIDs: protectedIDs, PublishedAt: publishedAt.UTC()}, nil
}

func (r *Repository) ConsumeEnrollment(ctx context.Context, consumption domainnetworkruntime.EnrollmentConsumption, snapshot domainnetworkruntime.PolicySnapshot, configurationExpiresAt time.Time) (domainnetworkruntime.Credential, domainnetworkruntime.Configuration, error) {
	var credential domainnetworkruntime.Credential
	var configuration domainnetworkruntime.Configuration
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		enrollment, err := consumeEnrollmentChallenge(tx, consumption)
		if err != nil {
			return err
		}
		previous, rotationReason, affectedRuntimeIDs, err := prepareCredentialRotation(tx, enrollment, consumption)
		if err != nil {
			return err
		}
		row, err := insertEnrollmentCredential(tx, enrollment, consumption)
		if err != nil {
			return err
		}
		if err := applyCredentialRotation(tx, previous, rotationReason, affectedRuntimeIDs, consumption); err != nil {
			return err
		}
		configuration, err = refreshEnrollmentConfigurations(tx, affectedRuntimeIDs, consumption, snapshot, configurationExpiresAt)
		if err != nil {
			return err
		}
		credential = row.domain()
		return nil
	})
	return credential, configuration, err
}

func consumeEnrollmentChallenge(tx *gorm.DB, consumption domainnetworkruntime.EnrollmentConsumption) (enrollmentRow, error) {
	var enrollment enrollmentRow
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND challenge_id = ?", consumption.EnrollmentID, consumption.ChallengeID).First(&enrollment).Error; err != nil {
		return enrollmentRow{}, enrollmentError(err)
	}
	if enrollment.Status != domainnetworkruntime.EnrollmentPending || !enrollment.ExpiresAt.After(consumption.ConsumedAt) {
		return enrollmentRow{}, apperrors.NewBusiness(apperrors.ErrGone, "enrollment_expired", "The enrollment challenge is no longer active.", "注册挑战已失效。")
	}
	if enrollment.RuntimeID != consumption.RuntimeID || enrollment.RuntimeKind != consumption.RuntimeKind || enrollment.DeviceID != consumption.DeviceID {
		return enrollmentRow{}, apperrors.NewBusiness(apperrors.ErrUnauthorized, "enrollment_binding_mismatch", "The enrollment challenge does not match this runtime.", "注册挑战与当前运行时不匹配。")
	}
	if len(enrollment.ChallengeHash) != len(consumption.TokenHash) || subtle.ConstantTimeCompare([]byte(enrollment.ChallengeHash), []byte(consumption.TokenHash)) != 1 {
		return enrollmentRow{}, apperrors.NewBusiness(apperrors.ErrUnauthorized, "invalid_enrollment_token", "The enrollment token is invalid.", "注册令牌无效。")
	}
	if consumption.NotBefore.After(consumption.ConsumedAt) || !consumption.ExpiresAt.After(consumption.ConsumedAt) {
		return enrollmentRow{}, apperrors.NewBusiness(apperrors.ErrUnauthorized, "invalid_runtime_certificate", "The runtime certificate is not currently valid.", "运行时证书当前无效。")
	}
	return enrollment, nil
}

func prepareCredentialRotation(tx *gorm.DB, enrollment enrollmentRow, consumption domainnetworkruntime.EnrollmentConsumption) (credentialRow, string, []string, error) {
	if err := lockWireGuardMutations(tx); err != nil {
		return credentialRow{}, "", nil, err
	}
	if err := expireWireGuardPeers(tx, consumption.ConsumedAt); err != nil {
		return credentialRow{}, "", nil, err
	}
	var previous credentialRow
	previousErr := tx.Where("tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND status = ?", consumption.RuntimeID, domainnetworkruntime.CredentialActive).Order("generation DESC").First(&previous).Error
	if previousErr != nil && !errors.Is(previousErr, gorm.ErrRecordNotFound) {
		return credentialRow{}, "", nil, previousErr
	}
	rotationReason := credentialRotationReason(previous, previousErr == nil, enrollment, consumption)
	affectedRuntimeIDs := []string{consumption.RuntimeID}
	if rotationReason != "" {
		peerRuntimeIDs, err := relatedWireGuardRuntimeIDs(tx, consumption.RuntimeID, previous.RuntimeKind)
		if err != nil {
			return credentialRow{}, "", nil, err
		}
		affectedRuntimeIDs = append(affectedRuntimeIDs, peerRuntimeIDs...)
	}
	for _, runtimeID := range orderedUniqueRuntimeIDs(affectedRuntimeIDs...) {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-runtime:"+runtimeID).Error; err != nil {
			return credentialRow{}, "", nil, err
		}
	}
	return previous, rotationReason, affectedRuntimeIDs, nil
}

func insertEnrollmentCredential(tx *gorm.DB, enrollment enrollmentRow, consumption domainnetworkruntime.EnrollmentConsumption) (credentialRow, error) {
	var generation int
	if err := tx.Raw(`SELECT COALESCE(MAX(generation), 0) + 1 FROM network_runtime_credentials WHERE tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ?`, consumption.RuntimeID).Scan(&generation).Error; err != nil {
		return credentialRow{}, err
	}
	if err := tx.Model(&credentialRow{}).Where("tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND status = ?", consumption.RuntimeID, domainnetworkruntime.CredentialActive).
		Updates(map[string]any{"status": domainnetworkruntime.CredentialRevoked, "revoked_at": consumption.ConsumedAt}).Error; err != nil {
		return credentialRow{}, err
	}
	capabilities, err := json.Marshal(consumption.Capabilities)
	if err != nil {
		return credentialRow{}, err
	}
	row := credentialRow{
		ID: uuid.NewString(), TenantID: "default", WorkspaceID: "default", EnrollmentID: enrollment.ID,
		RuntimeID: consumption.RuntimeID, RuntimeKind: consumption.RuntimeKind, DeviceID: enrollment.DeviceID, SubjectID: enrollment.SubjectID,
		CertificateFingerprint: consumption.CertificateFingerprint, PublicKeyFingerprint: consumption.PublicKeyFingerprint,
		WireGuardPublicKey: consumption.WireGuardPublicKey,
		CertificateSerial:  consumption.CertificateSerial, CertificateAuthorityKeyID: consumption.CertificateAuthorityKeyID,
		Generation: generation, Capabilities: jsonDocument(capabilities),
		Status: domainnetworkruntime.CredentialActive, NotBefore: consumption.NotBefore, ExpiresAt: consumption.ExpiresAt, CreatedAt: consumption.ConsumedAt,
	}
	if err := tx.Create(&row).Error; err != nil {
		return credentialRow{}, err
	}
	if err := tx.Model(&enrollment).Updates(map[string]any{"status": domainnetworkruntime.EnrollmentConsumed, "consumed_at": consumption.ConsumedAt}).Error; err != nil {
		return credentialRow{}, err
	}
	return row, nil
}

func applyCredentialRotation(tx *gorm.DB, previous credentialRow, reason string, affectedRuntimeIDs []string, consumption domainnetworkruntime.EnrollmentConsumption) error {
	if reason == "" {
		return nil
	}
	if previous.RuntimeKind != "gateway" {
		return revokeRuntimePeers(tx, consumption.RuntimeID, consumption.ConsumedAt, reason)
	}
	for _, runtimeID := range affectedRuntimeIDs {
		if runtimeID == consumption.RuntimeID {
			continue
		}
		if err := revokeRuntimePeers(tx, runtimeID, consumption.ConsumedAt, reason); err != nil {
			return err
		}
	}
	return nil
}

func refreshEnrollmentConfigurations(tx *gorm.DB, runtimeIDs []string, consumption domainnetworkruntime.EnrollmentConsumption, snapshot domainnetworkruntime.PolicySnapshot, expiresAt time.Time) (domainnetworkruntime.Configuration, error) {
	var configuration domainnetworkruntime.Configuration
	for _, runtimeID := range orderedUniqueRuntimeIDs(runtimeIDs...) {
		created, err := insertConfiguration(tx, runtimeID, snapshot, expiresAt, consumption.ConsumedAt)
		if err != nil {
			return domainnetworkruntime.Configuration{}, err
		}
		if runtimeID == consumption.RuntimeID {
			configuration = created
		}
	}
	return configuration, nil
}

func (r *Repository) ActiveCredential(ctx context.Context, fingerprint string, now time.Time) (domainnetworkruntime.Credential, error) {
	var row credentialRow
	err := r.db.WithContext(ctx).Where("tenant_id = 'default' AND workspace_id = 'default' AND certificate_fingerprint = ? AND status = ? AND not_before <= ? AND expires_at > ?", fingerprint, domainnetworkruntime.CredentialActive, now, now).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domainnetworkruntime.Credential{}, apperrors.NewBusiness(apperrors.ErrUnauthorized, "runtime_credential_inactive", "The runtime credential is not active.", "运行时凭据未激活。")
	}
	if err != nil {
		return domainnetworkruntime.Credential{}, err
	}
	return row.domain(), nil
}

func (r *Repository) ActiveEndpointCredential(ctx context.Context, serial, authorityKeyID string, now time.Time) (domainnetworkruntime.Credential, error) {
	var row credentialRow
	err := r.db.WithContext(ctx).Where("tenant_id = 'default' AND workspace_id = 'default' AND runtime_kind = 'endpoint' AND certificate_serial = ? AND certificate_authority_key_id = ? AND status = ? AND not_before <= ? AND expires_at > ?", serial, authorityKeyID, domainnetworkruntime.CredentialActive, now, now).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domainnetworkruntime.Credential{}, apperrors.NewBusiness(apperrors.ErrUnauthorized, "runtime_credential_inactive", "The runtime credential is not active.", "运行时凭据未激活。")
	}
	if err != nil {
		return domainnetworkruntime.Credential{}, err
	}
	return row.domain(), nil
}

func (r *Repository) TouchCredential(ctx context.Context, credentialID string, now time.Time) error {
	return r.db.WithContext(ctx).Model(&credentialRow{}).Where("id = ? AND status = ?", credentialID, domainnetworkruntime.CredentialActive).Update("last_control_seen_at", now).Error
}

func (r *Repository) EnsureConfiguration(ctx context.Context, runtimeID string, snapshot domainnetworkruntime.PolicySnapshot, now, expiresAt time.Time) (domainnetworkruntime.Configuration, error) {
	var result domainnetworkruntime.Configuration
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-runtime:"+runtimeID).Error; err != nil {
			return err
		}
		latest, err := latestConfiguration(tx, runtimeID)
		if err == nil && latest.PolicyVersion == snapshot.PolicyVersion && latest.ValidUntil.After(now) {
			profile, profileErr := currentRuntimeMihomoProfile(tx, runtimeID, now)
			if profileErr != nil {
				return profileErr
			}
			if sameMihomoRevision(latest.Desired.Mihomo, profile) {
				result = latest
				return nil
			}
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		created, err := insertConfiguration(tx, runtimeID, snapshot, expiresAt, now)
		result = created
		return err
	})
	return result, err
}

func currentRuntimeMihomoProfile(tx *gorm.DB, runtimeID string, now time.Time) (*domainnetworkaccess.MihomoProfile, error) {
	credential, err := activeRuntimeCredential(tx, runtimeID, now)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return activeEndpointMihomoProfile(tx, credential)
}

func sameMihomoRevision(desired *networkprotocol.MihomoConfiguration, profile *domainnetworkaccess.MihomoProfile) bool {
	if desired == nil || profile == nil {
		return desired == nil && profile == nil
	}
	return desired.ProfileID == profile.ID && desired.ProfileRevision == profile.Revision
}

func (r *Repository) ApplyConfiguration(ctx context.Context, runtimeID string, applied networkprotocol.ConfigurationApplied, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-runtime:"+runtimeID).Error; err != nil {
			return err
		}
		latest, err := latestConfiguration(tx, runtimeID)
		if err != nil {
			return err
		}
		if latest.ConfigurationVersion != applied.ConfigurationVersion || latest.PolicyVersion != applied.PolicyVersion {
			return apperrors.NewBusiness(apperrors.ErrConflict, "stale_configuration", "Only the latest desired configuration can be acknowledged.", "只能确认最新的期望配置。")
		}
		if latest.ApplyStatus != "pending" {
			if latest.ApplyStatus == applied.Status && latest.ReadbackHash == applied.ReadbackHash && latest.ReasonCode == applied.ReasonCode {
				return nil
			}
			return apperrors.NewBusiness(apperrors.ErrConflict, "configuration_ack_conflict", "The configuration was already acknowledged differently.", "该配置已使用不同结果确认。")
		}
		return tx.Model(&configurationRow{}).
			Where("tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND configuration_version = ?", runtimeID, applied.ConfigurationVersion).
			Updates(map[string]any{"apply_status": applied.Status, "readback_hash": applied.ReadbackHash, "reason_code": nullString(applied.ReasonCode), "applied_at": now}).Error
	})
}

func (r *Repository) RenewLeases(ctx context.Context, runtimeID string, request networkprotocol.LeaseRenewRequest, snapshot domainnetworkruntime.PolicySnapshot, now time.Time, ttl time.Duration) (networkprotocol.LeaseRenewResult, error) {
	result := networkprotocol.LeaseRenewResult{PolicyVersion: snapshot.PolicyVersion, NetworkLeases: []networkprotocol.NetworkLease{}, ResourceLeases: []networkprotocol.ResourceLease{}, RevokedLeaseIDs: []string{}}
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		session, gatewayRuntimeID, err := lockRenewalSession(tx, runtimeID, request, snapshot.PolicyVersion, now)
		if err != nil {
			return err
		}
		validUntil, err := renewSessionTransport(tx, runtimeID, gatewayRuntimeID, session, now, ttl)
		if err != nil {
			return err
		}
		var networkLeaseRenewed bool
		result, networkLeaseRenewed, err = renewRequestedLeases(tx, result, request, session, snapshot.PolicyVersion, validUntil, now)
		if err != nil {
			return err
		}
		return refreshRenewalConfigurations(tx, runtimeID, gatewayRuntimeID, session.ID, snapshot, validUntil, now, networkLeaseRenewed || len(result.ResourceLeases) != 0)
	})
	return result, err
}

func lockRenewalSession(tx *gorm.DB, runtimeID string, request networkprotocol.LeaseRenewRequest, policyVersion int, now time.Time) (sessionRow, string, error) {
	if err := lockWireGuardMutations(tx); err != nil {
		return sessionRow{}, "", err
	}
	gatewayRuntimeID, err := vpnGatewayRuntimeForSession(tx, runtimeID, request.SessionID)
	if err != nil {
		return sessionRow{}, "", err
	}
	for _, lockedRuntimeID := range orderedRuntimeIDs(runtimeID, gatewayRuntimeID) {
		if lockedRuntimeID == "" {
			continue
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-runtime:"+lockedRuntimeID).Error; err != nil {
			return sessionRow{}, "", err
		}
	}
	var session sessionRow
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND runtime_id = ?", request.SessionID, runtimeID).First(&session).Error; err != nil {
		return sessionRow{}, "", sessionError(err)
	}
	if !renewableSession(session.Status) || !session.ValidUntil.After(now) {
		return sessionRow{}, "", apperrors.NewBusiness(apperrors.ErrGone, "network_session_inactive", "The network session is no longer renewable.", "网络会话已不可续租。")
	}
	if !currentRenewalState(tx, session, request, policyVersion) {
		return sessionRow{}, "", apperrors.NewBusiness(apperrors.ErrConflict, "stale_session_state", "The session policy, posture or configuration is stale.", "会话策略、设备状态或配置已过期。")
	}
	return session, gatewayRuntimeID, nil
}

func currentRenewalState(tx *gorm.DB, session sessionRow, request networkprotocol.LeaseRenewRequest, policyVersion int) bool {
	var currentPostureVersion int
	err := tx.Raw(`SELECT posture_version FROM network_access_devices
		WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND owner_user_id::text = ?
		AND (? NOT IN (?, ?) OR COALESCE(site_id, '') = ?) FOR SHARE`, session.DeviceID, session.SubjectID,
		session.Mode, domainnetworkaccess.ModeInternalDirect, domainnetworkaccess.ModeInternalZTNA, session.SiteID).Row().Scan(&currentPostureVersion)
	return err == nil && session.PostureVersion > 0 && session.PostureVersion == currentPostureVersion && (request.PostureVersion == nil || currentPostureVersion == *request.PostureVersion) && session.ConfigurationVersion == request.ObservedConfigurationVersion && session.PolicyVersion == policyVersion
}

func renewSessionTransport(tx *gorm.DB, runtimeID, gatewayRuntimeID string, session sessionRow, now time.Time, ttl time.Duration) (time.Time, error) {
	validUntil := minTime(now.Add(ttl), session.ValidUntil)
	if gatewayRuntimeID == "" {
		return validUntil, nil
	}
	endpointCredential, err := activeRuntimeCredential(tx, runtimeID, now)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return time.Time{}, apperrors.NewBusiness(apperrors.ErrGone, "network_session_inactive", "The endpoint credential is no longer active.", "端点凭据已失效。")
	}
	if err != nil {
		return time.Time{}, err
	}
	gatewayCredential, err := activeRuntimeCredential(tx, gatewayRuntimeID, now)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return time.Time{}, apperrors.NewBusiness(apperrors.ErrGone, "network_session_inactive", "The gateway credential is no longer active.", "网关凭据已失效。")
	}
	if err != nil {
		return time.Time{}, err
	}
	validUntil = minTime(now.Add(ttl), minTime(endpointCredential.ExpiresAt, gatewayCredential.ExpiresAt))
	if err := tx.Model(&sessionRow{}).Where("id = ?", session.ID).Updates(map[string]any{"valid_until": validUntil, "updated_at": now}).Error; err != nil {
		return time.Time{}, err
	}
	if err := tx.Exec(`UPDATE network_wireguard_peers SET expires_at = ?, updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND session_id = ? AND status = 'active'`, validUntil, now, session.ID).Error; err != nil {
		return time.Time{}, err
	}
	return validUntil, nil
}

func renewRequestedLeases(tx *gorm.DB, result networkprotocol.LeaseRenewResult, request networkprotocol.LeaseRenewRequest, session sessionRow, policyVersion int, validUntil, now time.Time) (networkprotocol.LeaseRenewResult, bool, error) {
	var leases []leaseRow
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = 'default' AND workspace_id = 'default' AND session_id = ? AND id IN ?", request.SessionID, request.LeaseIDs).Find(&leases).Error; err != nil {
		return result, false, err
	}
	byID := make(map[string]leaseRow, len(leases))
	for _, lease := range leases {
		byID[lease.ID] = lease
	}
	result.ValidUntil = validUntil
	networkLeaseRenewed := false
	for _, id := range request.LeaseIDs {
		lease, exists := byID[id]
		if !renewableLease(lease, exists, session, policyVersion, now) {
			result.RevokedLeaseIDs = append(result.RevokedLeaseIDs, id)
			if err := rejectLeaseRenewal(tx, id, lease, exists, now); err != nil {
				return result, false, err
			}
			continue
		}
		isNetwork, err := activateRenewedLease(tx, &result, lease, validUntil, now)
		if err != nil {
			return result, false, err
		}
		networkLeaseRenewed = networkLeaseRenewed || isNetwork
	}
	return result, networkLeaseRenewed, nil
}

func renewableLease(lease leaseRow, exists bool, session sessionRow, policyVersion int, now time.Time) bool {
	return exists && lease.SubjectID == session.SubjectID && lease.DeviceID == session.DeviceID && lease.PolicyVersion == policyVersion && (lease.Status == "issued" || lease.Status == "active") && lease.ExpiresAt.After(now)
}

func rejectLeaseRenewal(tx *gorm.DB, id string, lease leaseRow, exists bool, now time.Time) error {
	if !exists || lease.Status == "revoked" {
		return nil
	}
	return tx.Model(&leaseRow{}).Where("id = ?", id).Updates(map[string]any{"status": "revoked", "revoked_at": now, "revoke_reason": "renewal_rejected", "updated_at": now}).Error
}

func activateRenewedLease(tx *gorm.DB, result *networkprotocol.LeaseRenewResult, lease leaseRow, validUntil, now time.Time) (bool, error) {
	if err := tx.Model(&leaseRow{}).Where("id = ?", lease.ID).Updates(map[string]any{"status": "active", "expires_at": validUntil, "updated_at": now}).Error; err != nil {
		return false, err
	}
	lease.Status, lease.ExpiresAt, lease.UpdatedAt = "active", validUntil, now
	if lease.LeaseKind == "network" {
		item, err := lease.networkLease()
		if err != nil {
			return false, err
		}
		result.NetworkLeases = append(result.NetworkLeases, item)
		return true, nil
	}
	item, err := lease.resourceLease()
	if err != nil {
		return false, err
	}
	result.ResourceLeases = append(result.ResourceLeases, item)
	return false, nil
}

func refreshRenewalConfigurations(tx *gorm.DB, runtimeID, gatewayRuntimeID, sessionID string, snapshot domainnetworkruntime.PolicySnapshot, validUntil, now time.Time, changed bool) error {
	if gatewayRuntimeID == "" || !changed {
		return nil
	}
	endpointConfiguration, err := insertConfiguration(tx, runtimeID, snapshot, validUntil, now)
	if err != nil {
		return err
	}
	if err := tx.Model(&sessionRow{}).Where("id = ?", sessionID).Updates(map[string]any{"configuration_version": endpointConfiguration.ConfigurationVersion, "updated_at": now}).Error; err != nil {
		return err
	}
	_, err = insertConfiguration(tx, gatewayRuntimeID, snapshot, validUntil, now)
	return err
}

func (r *Repository) RevokeLeases(ctx context.Context, runtimeID string, revoke networkprotocol.LeaseRevoke, snapshot domainnetworkruntime.PolicySnapshot, now, configurationExpiresAt time.Time) (int64, error) {
	var affected int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockWireGuardMutations(tx); err != nil {
			return err
		}
		gatewayRuntimeID, err := vpnGatewayRuntimeForSession(tx, runtimeID, revoke.SessionID)
		if err != nil {
			return err
		}
		for _, lockedRuntimeID := range orderedRuntimeIDs(runtimeID, gatewayRuntimeID) {
			if lockedRuntimeID == "" {
				continue
			}
			if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-runtime:"+lockedRuntimeID).Error; err != nil {
				return err
			}
		}
		var session sessionRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND runtime_id = ?", revoke.SessionID, runtimeID).First(&session).Error; err != nil {
			return sessionError(err)
		}
		var networkLeaseCount int64
		if err := tx.Model(&leaseRow{}).Where("tenant_id = 'default' AND workspace_id = 'default' AND session_id = ? AND id IN ? AND lease_kind = 'network' AND status IN ?", revoke.SessionID, revoke.LeaseIDs, []string{"issued", "active", "revoking"}).Count(&networkLeaseCount).Error; err != nil {
			return err
		}
		status := "revoking"
		if !revoke.EffectiveAt.After(now) {
			status = "revoked"
		}
		result := tx.Model(&leaseRow{}).
			Where("tenant_id = 'default' AND workspace_id = 'default' AND session_id = ? AND id IN ? AND status IN ?", revoke.SessionID, revoke.LeaseIDs, []string{"issued", "active", "revoking"}).
			Updates(map[string]any{"status": status, "revoked_at": revoke.EffectiveAt, "revoke_reason": revoke.ReasonCode, "updated_at": now})
		affected = result.RowsAffected
		if result.Error != nil || affected == 0 || gatewayRuntimeID == "" || networkLeaseCount == 0 {
			return result.Error
		}
		// ponytail: without a push scheduler, a requested future VPN revoke is enforced early rather than risking fail-open access.
		if err := revokeRuntimePeers(tx, runtimeID, now, revoke.ReasonCode); err != nil {
			return err
		}
		endpointConfiguration, err := insertConfiguration(tx, runtimeID, snapshot, configurationExpiresAt, now)
		if err != nil {
			return err
		}
		if err := tx.Model(&sessionRow{}).Where("id = ?", session.ID).Updates(map[string]any{"configuration_version": endpointConfiguration.ConfigurationVersion, "updated_at": now}).Error; err != nil {
			return err
		}
		_, err = insertConfiguration(tx, gatewayRuntimeID, snapshot, configurationExpiresAt, now)
		return err
	})
	return affected, err
}

func insertConfiguration(tx *gorm.DB, runtimeID string, snapshot domainnetworkruntime.PolicySnapshot, expiresAt, createdAt time.Time) (domainnetworkruntime.Configuration, error) {
	desired, err := desiredConfigurationForRuntime(tx, runtimeID, snapshot, createdAt, expiresAt)
	if err != nil {
		return domainnetworkruntime.Configuration{}, err
	}
	return insertDesiredConfiguration(tx, runtimeID, desired, createdAt)
}

func insertDesiredConfiguration(tx *gorm.DB, runtimeID string, desired networkprotocol.ConfigurationDesired, createdAt time.Time) (domainnetworkruntime.Configuration, error) {
	var version int
	if err := tx.Raw(`SELECT COALESCE(MAX(configuration_version), 0) + 1 FROM network_runtime_configurations WHERE tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ?`, runtimeID).Scan(&version).Error; err != nil {
		return domainnetworkruntime.Configuration{}, err
	}
	desired.ConfigurationVersion = version
	desired.ValidUntil = desired.ValidUntil.UTC()
	payload, err := json.Marshal(desired)
	if err != nil {
		return domainnetworkruntime.Configuration{}, err
	}
	digest := sha256.Sum256(payload)
	row := configurationRow{
		TenantID: "default", WorkspaceID: "default", RuntimeID: runtimeID, ConfigurationVersion: version,
		PolicyVersion: desired.PolicyVersion, DesiredPayload: jsonDocument(payload), DesiredHash: fmt.Sprintf("sha256:%x", digest),
		ValidUntil: desired.ValidUntil, ApplyStatus: "pending", CreatedAt: createdAt.UTC(),
	}
	if err := tx.Create(&row).Error; err != nil {
		return domainnetworkruntime.Configuration{}, err
	}
	return row.domain()
}

func latestConfiguration(tx *gorm.DB, runtimeID string) (domainnetworkruntime.Configuration, error) {
	var row configurationRow
	err := tx.Where("tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ?", runtimeID).Order("configuration_version DESC").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domainnetworkruntime.Configuration{}, sql.ErrNoRows
	}
	if err != nil {
		return domainnetworkruntime.Configuration{}, err
	}
	return row.domain()
}

func enrollmentError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperrors.NewBusiness(apperrors.ErrUnauthorized, "invalid_enrollment_challenge", "The enrollment challenge is invalid.", "注册挑战无效。")
	}
	return err
}

func sessionError(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperrors.NewBusiness(apperrors.ErrAccessDenied, "network_session_not_found", "The network session is not available to this runtime.", "当前运行时不可访问该网络会话。")
	}
	return err
}

func renewableSession(status string) bool {
	return status == "active" || status == "restricted" || status == "quarantine"
}

func minTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}

func nullString(value string) any {
	if value == "" {
		return nil
	}
	return value
}

type jsonDocument []byte

func (value jsonDocument) Value() (driver.Value, error) {
	if !json.Valid(value) {
		return nil, fmt.Errorf("invalid JSON document")
	}
	return string(value), nil
}

func (value *jsonDocument) Scan(raw any) error {
	switch typed := raw.(type) {
	case []byte:
		*value = append((*value)[:0], typed...)
	case string:
		*value = append((*value)[:0], typed...)
	default:
		return fmt.Errorf("scan JSON document from %T", raw)
	}
	return nil
}

type enrollmentRow struct {
	ID            string     `gorm:"primaryKey;column:id"`
	TenantID      string     `gorm:"column:tenant_id"`
	WorkspaceID   string     `gorm:"column:workspace_id"`
	ChallengeID   string     `gorm:"column:challenge_id"`
	ChallengeHash string     `gorm:"column:challenge_hash"`
	RuntimeID     string     `gorm:"column:runtime_id"`
	RuntimeKind   string     `gorm:"column:runtime_kind"`
	DeviceID      string     `gorm:"column:device_id"`
	SubjectID     string     `gorm:"column:subject_id"`
	Status        string     `gorm:"column:status"`
	ExpiresAt     time.Time  `gorm:"column:expires_at"`
	ConsumedAt    *time.Time `gorm:"column:consumed_at"`
	RevokedAt     *time.Time `gorm:"column:revoked_at"`
	CreatedBy     string     `gorm:"column:created_by"`
	CreatedAt     time.Time  `gorm:"column:created_at"`
}

func (enrollmentRow) TableName() string { return "network_runtime_enrollments" }

func enrollmentRowFromDomain(item domainnetworkruntime.EnrollmentChallenge) *enrollmentRow {
	return &enrollmentRow{ID: item.ID, TenantID: "default", WorkspaceID: "default", ChallengeID: item.ChallengeID, ChallengeHash: item.ChallengeHash, RuntimeID: item.RuntimeID, RuntimeKind: item.RuntimeKind, DeviceID: item.DeviceID, SubjectID: item.SubjectID, Status: item.Status, ExpiresAt: item.ExpiresAt, ConsumedAt: item.ConsumedAt, RevokedAt: item.RevokedAt, CreatedBy: item.CreatedBy, CreatedAt: item.CreatedAt}
}

func (row enrollmentRow) domain() domainnetworkruntime.EnrollmentChallenge {
	return domainnetworkruntime.EnrollmentChallenge{ID: row.ID, ChallengeID: row.ChallengeID, ChallengeHash: row.ChallengeHash, RuntimeID: row.RuntimeID, RuntimeKind: row.RuntimeKind, DeviceID: row.DeviceID, SubjectID: row.SubjectID, Status: row.Status, ExpiresAt: row.ExpiresAt, ConsumedAt: row.ConsumedAt, RevokedAt: row.RevokedAt, CreatedBy: row.CreatedBy, CreatedAt: row.CreatedAt}
}

type credentialRow struct {
	ID                        string       `gorm:"primaryKey;column:id"`
	TenantID                  string       `gorm:"column:tenant_id"`
	WorkspaceID               string       `gorm:"column:workspace_id"`
	EnrollmentID              string       `gorm:"column:enrollment_id"`
	RuntimeID                 string       `gorm:"column:runtime_id"`
	RuntimeKind               string       `gorm:"column:runtime_kind"`
	DeviceID                  string       `gorm:"column:device_id"`
	SubjectID                 string       `gorm:"column:subject_id"`
	CertificateFingerprint    string       `gorm:"column:certificate_fingerprint"`
	PublicKeyFingerprint      string       `gorm:"column:public_key_fingerprint"`
	WireGuardPublicKey        string       `gorm:"column:wireguard_public_key"`
	CertificateSerial         string       `gorm:"column:certificate_serial"`
	CertificateAuthorityKeyID string       `gorm:"column:certificate_authority_key_id"`
	Generation                int          `gorm:"column:generation"`
	Capabilities              jsonDocument `gorm:"column:capabilities;type:jsonb"`
	Status                    string       `gorm:"column:status"`
	NotBefore                 time.Time    `gorm:"column:not_before"`
	ExpiresAt                 time.Time    `gorm:"column:expires_at"`
	RevokedAt                 *time.Time   `gorm:"column:revoked_at"`
	LastControlSeenAt         *time.Time   `gorm:"column:last_control_seen_at"`
	CreatedAt                 time.Time    `gorm:"column:created_at"`
}

func (credentialRow) TableName() string { return "network_runtime_credentials" }

func (row credentialRow) domain() domainnetworkruntime.Credential {
	var capabilities []string
	_ = json.Unmarshal(row.Capabilities, &capabilities)
	return domainnetworkruntime.Credential{ID: row.ID, EnrollmentID: row.EnrollmentID, RuntimeID: row.RuntimeID, RuntimeKind: row.RuntimeKind, DeviceID: row.DeviceID, SubjectID: row.SubjectID, CertificateFingerprint: row.CertificateFingerprint, PublicKeyFingerprint: row.PublicKeyFingerprint, WireGuardPublicKey: row.WireGuardPublicKey, CertificateSerial: row.CertificateSerial, CertificateAuthorityKeyID: row.CertificateAuthorityKeyID, Generation: row.Generation, Capabilities: capabilities, Status: row.Status, NotBefore: row.NotBefore, ExpiresAt: row.ExpiresAt, RevokedAt: row.RevokedAt, LastControlSeenAt: row.LastControlSeenAt, CreatedAt: row.CreatedAt}
}

type configurationRow struct {
	TenantID             string       `gorm:"primaryKey;column:tenant_id"`
	WorkspaceID          string       `gorm:"primaryKey;column:workspace_id"`
	RuntimeID            string       `gorm:"primaryKey;column:runtime_id"`
	ConfigurationVersion int          `gorm:"primaryKey;column:configuration_version"`
	PolicyVersion        int          `gorm:"column:policy_version"`
	DesiredPayload       jsonDocument `gorm:"column:desired_payload;type:jsonb"`
	DesiredHash          string       `gorm:"column:desired_hash"`
	ValidUntil           time.Time    `gorm:"column:valid_until"`
	ApplyStatus          string       `gorm:"column:apply_status"`
	ReadbackHash         *string      `gorm:"column:readback_hash"`
	ReasonCode           *string      `gorm:"column:reason_code"`
	AppliedAt            *time.Time   `gorm:"column:applied_at"`
	CreatedAt            time.Time    `gorm:"column:created_at"`
}

func (configurationRow) TableName() string { return "network_runtime_configurations" }

func (row configurationRow) domain() (domainnetworkruntime.Configuration, error) {
	var desired networkprotocol.ConfigurationDesired
	if err := json.Unmarshal(row.DesiredPayload, &desired); err != nil {
		return domainnetworkruntime.Configuration{}, fmt.Errorf("decode desired configuration: %w", err)
	}
	return domainnetworkruntime.Configuration{RuntimeID: row.RuntimeID, ConfigurationVersion: row.ConfigurationVersion, PolicyVersion: row.PolicyVersion, Desired: desired, DesiredHash: row.DesiredHash, ValidUntil: row.ValidUntil, ApplyStatus: row.ApplyStatus, ReadbackHash: stringValue(row.ReadbackHash), ReasonCode: stringValue(row.ReasonCode), AppliedAt: row.AppliedAt, CreatedAt: row.CreatedAt}, nil
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

type sessionRow struct {
	ID                   string    `gorm:"primaryKey;column:id"`
	RuntimeID            string    `gorm:"column:runtime_id"`
	SubjectID            string    `gorm:"column:subject_id"`
	DeviceID             string    `gorm:"column:device_id"`
	SiteID               string    `gorm:"column:site_id"`
	Mode                 string    `gorm:"column:mode"`
	Status               string    `gorm:"column:status"`
	PolicyVersion        int       `gorm:"column:policy_version"`
	ConfigurationVersion int       `gorm:"column:configuration_version"`
	PostureVersion       int       `gorm:"column:posture_version"`
	ValidUntil           time.Time `gorm:"column:valid_until"`
}

func (sessionRow) TableName() string { return "network_runtime_sessions" }

type leaseRow struct {
	ID             string       `gorm:"primaryKey;column:id"`
	SessionID      string       `gorm:"column:session_id"`
	LeaseKind      string       `gorm:"column:lease_kind"`
	SubjectID      string       `gorm:"column:subject_id"`
	DeviceID       string       `gorm:"column:device_id"`
	NetworkSpaceID *string      `gorm:"column:network_space_id"`
	CIDRs          jsonDocument `gorm:"column:cidrs;type:jsonb"`
	ResourceIDs    jsonDocument `gorm:"column:resource_ids;type:jsonb"`
	PolicyVersion  int          `gorm:"column:policy_version"`
	Status         string       `gorm:"column:status"`
	IssuedAt       time.Time    `gorm:"column:issued_at"`
	ExpiresAt      time.Time    `gorm:"column:expires_at"`
	UpdatedAt      time.Time    `gorm:"column:updated_at"`
}

func (leaseRow) TableName() string { return "network_runtime_leases" }

func (row leaseRow) networkLease() (networkprotocol.NetworkLease, error) {
	var cidrs []string
	if err := json.Unmarshal(row.CIDRs, &cidrs); err != nil {
		return networkprotocol.NetworkLease{}, err
	}
	spaceID := ""
	if row.NetworkSpaceID != nil {
		spaceID = *row.NetworkSpaceID
	}
	return networkprotocol.NetworkLease{ID: row.ID, SessionID: row.SessionID, SubjectID: row.SubjectID, DeviceID: row.DeviceID, NetworkSpaceID: spaceID, CIDRs: cidrs, PolicyVersion: row.PolicyVersion, IssuedAt: row.IssuedAt, ExpiresAt: row.ExpiresAt}, nil
}

func (row leaseRow) resourceLease() (networkprotocol.ResourceLease, error) {
	var resourceIDs []string
	if err := json.Unmarshal(row.ResourceIDs, &resourceIDs); err != nil {
		return networkprotocol.ResourceLease{}, err
	}
	return networkprotocol.ResourceLease{ID: row.ID, SessionID: row.SessionID, SubjectID: row.SubjectID, DeviceID: row.DeviceID, ResourceIDs: resourceIDs, PolicyVersion: row.PolicyVersion, IssuedAt: row.IssuedAt, ExpiresAt: row.ExpiresAt}, nil
}
