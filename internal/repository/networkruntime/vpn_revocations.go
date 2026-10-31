package networkruntime

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type vpnRevocationRow struct {
	TenantID, WorkspaceID, SubjectID, DeviceID, SiteID string
	SessionID                                          string
	Epoch                                              int64
	Active                                             bool
	ReasonCode, CreatedBy                              string
	CreatedAt                                          time.Time
	ReleasedBy                                         *string
	ReleasedAt                                         *time.Time
	Executions                                         jsonDocument `gorm:"type:jsonb"`
}

func (vpnRevocationRow) TableName() string { return "network_vpn_revocations" }

func vpnRevocationScope(tx *gorm.DB, subjectID, deviceID, siteID string) *gorm.DB {
	return tx.Where("tenant_id = 'default' AND workspace_id = 'default' AND subject_id = ? AND device_id = ? AND site_id = ?", subjectID, deviceID, siteID)
}

func (r *Repository) VPNRestrictionActive(ctx context.Context, subjectID, deviceID, siteID string) (bool, error) {
	var active bool
	err := vpnRevocationScope(r.db.WithContext(ctx).Model(&vpnRevocationRow{}), subjectID, deviceID, siteID).Select("COALESCE(bool_or(active), false)").Scan(&active).Error
	return active, err
}

func ensureVPNNotRevoked(tx *gorm.DB, subjectID, deviceID, siteID string) error {
	if err := ensureSubjectNotDLPRestricted(tx, subjectID); err != nil {
		return err
	}
	active, err := New(tx).VPNRestrictionActive(tx.Statement.Context, subjectID, deviceID, siteID)
	if err != nil {
		return err
	}
	if active {
		return apperrors.NewBusiness(apperrors.ErrAccessDenied, "vpn_administratively_revoked", "VPN access for this subject, device and site is restricted until an administrator releases it.", "该用户、设备和站点的 VPN 访问已被撤销，需管理员显式解除后重新授权。")
	}
	return nil
}

func (r *Repository) GetVPNRevocation(ctx context.Context, session domain.Session) (domain.VPNRevocation, error) {
	var row vpnRevocationRow
	err := vpnRevocationScope(r.db.WithContext(ctx), session.SubjectID, session.DeviceID, session.SiteID).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domain.VPNRevocation{}, apperrors.ErrNotFound
	}
	if err != nil {
		return domain.VPNRevocation{}, err
	}
	result := domain.VPNRevocation{SessionID: row.SessionID, SubjectID: row.SubjectID, DeviceID: row.DeviceID, SiteID: row.SiteID, Epoch: row.Epoch, Active: row.Active, ReasonCode: row.ReasonCode, CreatedAt: row.CreatedAt, ReleasedAt: row.ReleasedAt, Executions: []domain.VPNRevocationExecution{}}
	if err := json.Unmarshal(row.Executions, &result.Executions); err != nil {
		return domain.VPNRevocation{}, err
	}
	for i := range result.Executions {
		target := &result.Executions[i]
		var configuration configurationRow
		err := r.db.WithContext(ctx).Where("tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND configuration_version = ?", target.RuntimeID, target.ConfigurationVersion).Take(&configuration).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			target.ApplyStatus = "unknown"
			continue
		}
		if err != nil {
			return domain.VPNRevocation{}, err
		}
		target.ApplyStatus, target.ReasonCode, target.AppliedAt = configuration.ApplyStatus, stringValue(configuration.ReasonCode), configuration.AppliedAt
	}
	return result, nil
}

func (r *Repository) RevokeVPNSession(ctx context.Context, session domain.Session, actorID string, now time.Time) (domain.VPNRevocation, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockWireGuardMutations(tx); err != nil {
			return err
		}
		var current sessionRow
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = 'default' AND workspace_id = 'default' AND id = ?", session.ID).Take(&current).Error; err != nil {
			return sessionError(err)
		}
		if current.SubjectID != session.SubjectID || current.DeviceID != session.DeviceID || current.SiteID != session.SiteID || current.RuntimeID != session.RuntimeID {
			return apperrors.ErrConflict
		}
		var peerCount int64
		if err := tx.Table("network_wireguard_peers").Where("tenant_id = 'default' AND workspace_id = 'default' AND session_id = ?", current.ID).Count(&peerCount).Error; err != nil {
			return err
		}
		if peerCount == 0 {
			return apperrors.ErrConflict
		}
		return persistVPNRevocation(tx, current, actorID, now)
	})
	if err != nil {
		return domain.VPNRevocation{}, err
	}
	return r.GetVPNRevocation(ctx, session)
}

func persistVPNRevocation(tx *gorm.DB, session sessionRow, actorID string, now time.Time) error {
	var previous vpnRevocationRow
	err := vpnRevocationScope(tx, session.SubjectID, session.DeviceID, session.SiteID).Take(&previous).Error
	if err == nil && previous.Active {
		return nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err := revokePendingVPNAuthorizations(tx, session, now); err != nil {
		return err
	}
	runtimeIDs, err := revokeVPNScope(tx, session, now)
	if err != nil {
		return err
	}
	snapshot, err := New(tx).LatestSnapshot(tx.Statement.Context)
	if err != nil {
		return err
	}
	executions := make([]domain.VPNRevocationExecution, 0, len(runtimeIDs))
	for _, runtimeID := range runtimeIDs {
		configuration, err := insertConfiguration(tx, runtimeID, snapshot, now.Add(5*time.Minute), now)
		if err != nil {
			return err
		}
		executions = append(executions, domain.VPNRevocationExecution{RuntimeID: runtimeID, ConfigurationVersion: configuration.ConfigurationVersion, ApplyStatus: "pending"})
	}
	raw, err := json.Marshal(executions)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO network_vpn_revocations (subject_id, device_id, site_id, session_id, epoch, active, reason_code, created_by, created_at, executions)
		VALUES (?, ?, ?, ?, 1, true, 'administrator_revoked', ?, ?, ?::jsonb)
		ON CONFLICT (tenant_id, workspace_id, subject_id, device_id, site_id) DO UPDATE SET
		session_id = EXCLUDED.session_id, epoch = network_vpn_revocations.epoch + 1, active = true,
		reason_code = EXCLUDED.reason_code, created_by = EXCLUDED.created_by, created_at = EXCLUDED.created_at,
		released_at = NULL, released_by = NULL, executions = EXCLUDED.executions`, session.SubjectID, session.DeviceID, session.SiteID, session.ID, actorID, now, string(raw)).Error
}

func revokeVPNScope(tx *gorm.DB, session sessionRow, now time.Time) ([]string, error) {
	var peers []struct{ RuntimeID, GatewayRuntimeID, SessionID string }
	if err := tx.Raw(`SELECT p.runtime_id, g.runtime_id AS gateway_runtime_id, p.session_id FROM network_wireguard_peers p
		JOIN network_runtime_sessions s ON s.id = p.session_id AND s.tenant_id = p.tenant_id AND s.workspace_id = p.workspace_id
		JOIN network_access_gateways g ON g.id = p.gateway_id AND g.tenant_id = p.tenant_id AND g.workspace_id = p.workspace_id
		WHERE s.tenant_id = 'default' AND s.workspace_id = 'default' AND s.subject_id = ? AND s.device_id = ? AND s.site_id = ? AND (p.status = 'active' OR p.session_id = ?) LIMIT 1025`, session.SubjectID, session.DeviceID, session.SiteID, session.ID).Scan(&peers).Error; err != nil {
		return nil, err
	}
	if len(peers) > 1024 {
		return nil, apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "vpn_revocation_scope_limit", "The VPN restriction scope exceeds the bounded session limit.", "VPN 限制范围超过会话数量上限。")
	}
	runtimeIDs, sessionIDs := []string{}, []string{}
	for _, peer := range peers {
		runtimeIDs = append(runtimeIDs, peer.RuntimeID, peer.GatewayRuntimeID)
		sessionIDs = append(sessionIDs, peer.SessionID)
	}
	// Include the original endpoint even when its lease expired before this request.
	runtimeIDs = append(runtimeIDs, session.RuntimeID)
	slices.Sort(runtimeIDs)
	runtimeIDs = slices.Compact(runtimeIDs)
	for _, runtimeID := range runtimeIDs {
		if runtimeID == "" {
			return nil, apperrors.ErrConflict
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-runtime:"+runtimeID).Error; err != nil {
			return nil, err
		}
	}
	if len(sessionIDs) == 0 {
		return runtimeIDs, nil
	}
	for _, table := range []string{"network_runtime_sessions", "network_runtime_leases", "network_wireguard_peers"} {
		column := "session_id"
		if table == "network_runtime_sessions" {
			column = "id"
		}
		if err := tx.Table(table).Where("tenant_id = 'default' AND workspace_id = 'default' AND "+column+" IN ?", sessionIDs).
			Updates(map[string]any{"status": "revoked", "revoked_at": now, "revoke_reason": "administrator_revoked", "updated_at": now}).Error; err != nil {
			return nil, err
		}
	}
	return runtimeIDs, nil
}

func revokePendingVPNAuthorizations(tx *gorm.DB, session sessionRow, now time.Time) error {
	if err := tx.Exec(`UPDATE network_access_grants SET status = 'revoked', token_hash = NULL, revoked_at = ?, reason_code = 'administrator_revoked', updated_at = ?
		WHERE tenant_id = 'default' AND workspace_id = 'default' AND subject_id = ? AND device_id = ? AND site_id = ? AND status = 'issued'`, now, now, session.SubjectID, session.DeviceID, session.SiteID).Error; err != nil {
		return err
	}
	return tx.Exec(`UPDATE network_vpn_connection_intents i SET status = 'rejected', token_hash = NULL
		FROM network_vpn_documents d WHERE i.tenant_id = 'default' AND i.workspace_id = 'default'
		AND d.tenant_id = i.tenant_id AND d.workspace_id = i.workspace_id AND d.kind = 'profile' AND d.id = i.profile_id
		AND i.subject_id = ? AND i.device_id = ? AND d.published_configuration->>'siteId' = ? AND i.status = 'issued'`, session.SubjectID, session.DeviceID, session.SiteID).Error
}

func (r *Repository) ReleaseVPNRevocation(ctx context.Context, session domain.Session, expected int64, actorID string, now time.Time) (domain.VPNRevocation, error) {
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockWireGuardMutations(tx); err != nil {
			return err
		}
		if err := revokePendingVPNAuthorizations(tx, sessionRow{SubjectID: session.SubjectID, DeviceID: session.DeviceID, SiteID: session.SiteID}, now); err != nil {
			return err
		}
		result := vpnRevocationScope(tx.Model(&vpnRevocationRow{}), session.SubjectID, session.DeviceID, session.SiteID).
			Where("epoch = ? AND active = true", expected).
			Updates(map[string]any{"active": false, "epoch": expected + 1, "released_by": actorID, "released_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return apperrors.ErrConflict
		}
		return nil
	})
	if err != nil {
		return domain.VPNRevocation{}, err
	}
	return r.GetVPNRevocation(ctx, session)
}
