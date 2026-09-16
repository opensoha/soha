package networkruntime

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	runtime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	networkrepo "github.com/opensoha/soha/internal/repository/networkaccess"
	"gorm.io/gorm"
)

func lockManagedVPNIntent(tx *gorm.DB, connection runtime.VPNConnection) error {
	m := connection.Managed
	if m == nil {
		return nil
	}
	intent, err := scanVPNIntent(tx.Raw("SELECT "+vpnIntentColumns+" FROM network_vpn_connection_intents WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? FOR UPDATE", m.Intent.ID).Row())
	if err != nil {
		return err
	}
	if !validVPNIntent(intent, connection.RuntimeID, connection.CredentialID, connection.SubjectID, connection.DeviceID, m.Intent.TokenHash, connection.CreatedAt) {
		return invalidVPNIntent()
	}
	if err := activeVPNAuthSession(tx, intent.AuthSessionID, intent.SubjectID, connection.CreatedAt); err != nil {
		return err
	}
	var profileRaw, policyRaw []byte
	if err := tx.Raw(`SELECT published_configuration FROM network_vpn_documents WHERE id = ? AND kind = 'profile' AND tenant_id = 'default' AND workspace_id = 'default' AND deleted_at IS NULL AND published_revision = ? FOR SHARE`, intent.ProfileID, intent.ProfileRevision).Row().Scan(&profileRaw); err != nil {
		return invalidVPNIntent()
	}
	if err := tx.Raw(`SELECT published_configuration FROM network_vpn_documents WHERE id = ? AND kind = 'selection-policy' AND tenant_id = 'default' AND workspace_id = 'default' AND deleted_at IS NULL AND published_revision = ? FOR SHARE`, intent.SelectionPolicyID, intent.SelectionPolicyRevision).Row().Scan(&policyRaw); err != nil {
		return invalidVPNIntent()
	}
	var profile domain.VPNProfileConfig
	var policy domain.VPNSelectionPolicyConfig
	if json.Unmarshal(profileRaw, &profile) != nil || json.Unmarshal(policyRaw, &policy) != nil {
		return invalidVPNIntent()
	}
	if !managedVPNConfigurationMatches(profile, policy, connection) {
		return invalidVPNIntent()
	}
	if intent.Selection == domain.VPNSelectionManual && (!profile.AllowManualSelection || (intent.RequestedGatewayID != connection.GatewayID && !policy.AllowManualFallback)) {
		return invalidVPNIntent()
	}
	subject, err := networkrepo.New(tx).GetSubject(tx.Statement.Context, connection.SubjectID)
	if err != nil {
		return err
	}
	device, err := networkrepo.New(tx).GetDevice(tx.Statement.Context, connection.DeviceID)
	if err != nil {
		return err
	}
	if subject.Status != domain.StatusActive || !profile.Assignments.Matches(subject, device) {
		return invalidVPNIntent()
	}
	return nil
}

func lockVPNCapacity(tx *gorm.DB, connection runtime.VPNConnection) error {
	if connection.Managed != nil {
		ready, err := vpnGatewayExecutionReady(tx, connection.GatewayRuntimeID, "", connection.CreatedAt)
		if err != nil {
			return err
		}
		if !ready {
			return apperrors.NewBusiness(apperrors.ErrConflict, "gateway_configuration_not_ready", "Gateway execution is not ready.", "网关执行状态尚未就绪。")
		}
	}

	var accepting bool
	var maximum, count int
	if err := tx.Raw(`SELECT accept_new_connections, max_sessions FROM network_access_gateways WHERE id = ? AND tenant_id = 'default' AND workspace_id = 'default' FOR UPDATE`, connection.GatewayID).Row().Scan(&accepting, &maximum); err != nil {
		return err
	}
	if !accepting {
		return apperrors.NewBusiness(apperrors.ErrConflict, "gateway_draining", "The VPN gateway is not accepting new connections.", "该 VPN 接入点暂不接受新连接。")
	}
	if maximum == 0 {
		return nil
	}
	if err := tx.Raw(`SELECT count(*) FROM network_wireguard_peers WHERE tenant_id = 'default' AND workspace_id = 'default' AND gateway_id = ? AND runtime_id <> ? AND status = 'active' AND expires_at > ?`, connection.GatewayID, connection.RuntimeID, connection.CreatedAt).Row().Scan(&count); err != nil {
		return err
	}
	if count >= maximum {
		return apperrors.NewBusiness(apperrors.ErrConflict, "gateway_capacity_exhausted", "The VPN gateway has reached its session limit.", "该 VPN 接入点已达到连接容量上限。")
	}
	return nil
}

func createManagedVPNGrant(tx *gorm.DB, connection *runtime.VPNConnection) (*accessGrantRow, error) {
	if connection.Managed == nil || connection.Mode == domain.ModeExternalVPN {
		return nil, nil
	}
	intent := connection.Managed.Intent
	grant := runtime.AccessGrant{ID: "managed-" + intent.ID, SubjectID: connection.SubjectID, AuthSessionID: intent.AuthSessionID, DeviceID: connection.DeviceID, SiteID: connection.SiteID, NetworkSpaceID: connection.NetworkSpaceID, Mode: connection.Mode, ResourceIDs: slices.Clone(connection.ResourceIDs), PolicyVersion: connection.PolicyVersion, Status: runtime.AccessGrantIssued, TokenHash: intent.TokenHash, ReasonCode: "managed_profile_allowed", ExpiresAt: connection.CreatedAt.Add(2 * time.Minute), CreatedBy: connection.SubjectID, CreatedAt: connection.CreatedAt, UpdatedAt: connection.CreatedAt, ResourceLeaseIDs: []string{}}
	row, err := accessGrantRowFromDomain(grant)
	if err != nil {
		return nil, err
	}
	if err := tx.Create(&row).Error; err != nil {
		return nil, err
	}
	connection.AccessGrantID, connection.AccessGrantTokenHash = grant.ID, grant.TokenHash
	locked, err := lockAccessGrant(tx, *connection)
	return &locked, err
}

func managedVPNResult(connection runtime.VPNConnection, result networkprotocol.VPNConnectResult) networkprotocol.VPNManagedConnectResult {
	m := connection.Managed
	result.RequestID = m.RequestID
	return networkprotocol.VPNManagedConnectResult{VPNConnectResult: result, ProfileID: m.Intent.ProfileID, ProfileRevision: m.Intent.ProfileRevision, SelectionPolicyRevision: m.Intent.SelectionPolicyRevision, Selection: m.Intent.Selection, SelectionReason: m.Decision.ReasonCode, SiteID: connection.SiteID, NetworkSpaceID: connection.NetworkSpaceID, Mode: connection.Mode, ResourceIDs: append([]string{}, connection.ResourceIDs...), DecisionID: m.Decision.ID, FailoverOnDisconnect: m.Policy.FailoverOnDisconnect, RetryCooldownSeconds: m.Policy.RetryCooldownSeconds, MaxAttempts: m.Policy.MaxAttempts}
}

func consumeManagedVPNIntent(tx *gorm.DB, connection runtime.VPNConnection, result networkprotocol.VPNConnectResult) error {
	if connection.Managed == nil {
		return nil
	}
	m := connection.Managed
	encoded, err := json.Marshal(managedVPNResult(connection, result))
	if err != nil {
		return err
	}
	updated := tx.Exec(`UPDATE network_vpn_connection_intents SET status = 'consumed', token_hash = NULL, consumed_at = ?, session_id = ?, request_id = ?, request_hash = ?, managed_result = ?::jsonb WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND status = 'issued' AND token_hash = ?`, connection.CreatedAt, connection.SessionID, m.RequestID, m.RequestHash, string(encoded), m.Intent.ID, m.Intent.TokenHash)
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return invalidVPNIntent()
	}
	decision := m.Decision
	decision.EffectiveGatewayID, decision.SessionID, decision.State, decision.UpdatedAt = connection.GatewayID, connection.SessionID, "connecting", connection.CreatedAt
	return writeVPNDecision(tx, decision)
}

func (r *Repository) ManagedVPNResult(ctx context.Context, intentID, runtimeID, requestID, hash string) (networkprotocol.VPNManagedConnectResult, bool, error) {
	intent, err := r.GetVPNIntent(ctx, intentID)
	if err != nil {
		return networkprotocol.VPNManagedConnectResult{}, false, err
	}
	if intent.Status == "issued" {
		return networkprotocol.VPNManagedConnectResult{}, false, nil
	}
	if intent.RuntimeID != runtimeID || intent.RequestID != requestID || intent.RequestHash != hash || len(intent.ManagedResult) == 0 {
		return networkprotocol.VPNManagedConnectResult{}, false, invalidVPNIntent()
	}
	var result networkprotocol.VPNManagedConnectResult
	err = json.Unmarshal(intent.ManagedResult, &result)
	return result, true, err
}

func (r *Repository) SaveVPNDecision(ctx context.Context, decision domain.VPNDecision) error {
	return writeVPNDecision(r.db.WithContext(ctx), decision)
}

func writeVPNDecision(tx *gorm.DB, decision domain.VPNDecision) error {
	encoded, err := json.Marshal(decision)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO network_vpn_decisions (id, profile_id, site_id, network_space_id, subject_id, device_id, gateway_id, session_id, state, payload, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?::jsonb, ?, ?)
		ON CONFLICT (id) DO UPDATE SET gateway_id = EXCLUDED.gateway_id, session_id = EXCLUDED.session_id, state = EXCLUDED.state, payload = EXCLUDED.payload, updated_at = EXCLUDED.updated_at
		WHERE network_vpn_decisions.tenant_id = 'default' AND network_vpn_decisions.workspace_id = 'default' AND (network_vpn_decisions.state = 'selected' OR (network_vpn_decisions.state = 'connecting' AND EXCLUDED.state = 'connecting'))`, decision.ID, decision.ProfileID, decision.SiteID, decision.NetworkSpaceID, decision.SubjectID, decision.DeviceID, decision.EffectiveGatewayID, decision.SessionID, decision.State, string(encoded), decision.CreatedAt, decision.UpdatedAt).Error
}

func (r *Repository) FailManagedVPN(ctx context.Context, connection runtime.VPNConnection, result networkprotocol.VPNConnectResult) error {
	if connection.Managed == nil {
		return errors.New("managed VPN context is required")
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		m := connection.Managed
		encoded, err := json.Marshal(managedVPNResult(connection, result))
		if err != nil {
			return err
		}
		updated := tx.Exec(`UPDATE network_vpn_connection_intents SET status = 'rejected', token_hash = NULL, request_id = ?, request_hash = ?, managed_result = ?::jsonb WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND runtime_id = ? AND status = 'issued'`, m.RequestID, m.RequestHash, string(encoded), m.Intent.ID, connection.RuntimeID)
		if updated.Error != nil {
			return updated.Error
		}
		if updated.RowsAffected != 1 {
			return invalidVPNIntent()
		}
		decision := m.Decision
		decision.State, decision.UpdatedAt = "failed", connection.CreatedAt
		return writeVPNDecision(tx, decision)
	})
}

func managedVPNConfigurationMatches(profile domain.VPNProfileConfig, policy domain.VPNSelectionPolicyConfig, c runtime.VPNConnection) bool {
	m := c.Managed
	return reflect.DeepEqual(profile, m.Profile) && reflect.DeepEqual(policy, m.Policy) && profile.Enabled && profile.SelectionPolicyID == m.Intent.SelectionPolicyID && profile.SiteID == c.SiteID && profile.NetworkSpaceID == c.NetworkSpaceID && profile.Mode == c.Mode && slices.Equal(profile.ResourceIDs, c.ResourceIDs) && slices.Contains(profile.GatewayIDs, c.GatewayID)
}
