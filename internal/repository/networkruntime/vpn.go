package networkruntime

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	domainnetworkruntime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	networkaccessrepo "github.com/opensoha/soha/internal/repository/networkaccess"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (r *Repository) NetworkSpace(ctx context.Context, spaceID string) (domainnetworkaccess.Space, error) {
	return networkaccessrepo.New(r.db).GetSpace(ctx, spaceID)
}

func (r *Repository) NetworkResource(ctx context.Context, resourceID string) (domainnetworkaccess.Resource, error) {
	return networkaccessrepo.New(r.db).GetResource(ctx, resourceID)
}

func (r *Repository) MihomoSource(ctx context.Context, credentialID, profileID string, now time.Time) (string, string, int, error) {
	var sourceType, ciphertext string
	var revision int
	err := r.db.WithContext(ctx).Raw(`SELECT p.source_type, COALESCE(p.subscription_url_ciphertext, p.manual_node_ciphertext), p.revision
		FROM network_mihomo_profiles p
		JOIN network_runtime_credentials c ON c.tenant_id = p.tenant_id AND c.workspace_id = p.workspace_id AND c.device_id = p.device_id
		WHERE p.tenant_id = 'default' AND p.workspace_id = 'default' AND p.id = ? AND p.status = 'active'
		AND p.mode = 'managed_follow' AND ((p.source_type = 'managed_subscription' AND p.subscription_url_ciphertext IS NOT NULL) OR (p.source_type = 'manual_node' AND p.manual_node_ciphertext IS NOT NULL))
		AND c.id = ? AND c.runtime_kind = 'endpoint' AND c.status = 'active' AND c.not_before <= ? AND c.expires_at > ?
		AND c.capabilities @> '["mihomo"]'::jsonb
		LIMIT 1`, profileID, credentialID, now, now).Row().Scan(&sourceType, &ciphertext, &revision)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", 0, apperrors.ErrNotFound
	}
	return sourceType, ciphertext, revision, err
}

func (r *Repository) MihomoSubscription(ctx context.Context, credentialID, profileID string, now time.Time) (string, int, error) {
	sourceType, ciphertext, revision, err := r.MihomoSource(ctx, credentialID, profileID, now)
	if err != nil || sourceType == domainnetworkaccess.MihomoSourceManagedSubscription {
		return ciphertext, revision, err
	}
	return "", 0, apperrors.ErrNotFound
}

func (r *Repository) ActiveVPNGateway(ctx context.Context, siteID, requestedGatewayID string, now time.Time) (domainnetworkaccess.Gateway, domainnetworkruntime.Credential, error) {
	var targetGatewayID string
	err := r.db.WithContext(ctx).Raw(`SELECT id FROM network_access_gateways
		WHERE tenant_id = 'default' AND workspace_id = 'default' AND site_id = ? AND administrative_status = 'active' AND runtime_id IS NOT NULL
		ORDER BY id LIMIT 1`, siteID).Row().Scan(&targetGatewayID)
	if errors.Is(err, sql.ErrNoRows) {
		return domainnetworkaccess.Gateway{}, domainnetworkruntime.Credential{}, fmt.Errorf("%w: active VPN gateway not found", apperrors.ErrNotFound)
	}
	if err != nil {
		return domainnetworkaccess.Gateway{}, domainnetworkruntime.Credential{}, err
	}
	gatewayID := requestedGatewayID
	if gatewayID == "" {
		gatewayID = targetGatewayID
	}
	var runtimeID, selectedRoot, targetRoot string
	err = r.db.WithContext(ctx).Raw(`SELECT runtime_id, COALESCE(hub_gateway_id, id) FROM network_access_gateways
		WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND administrative_status = 'active' AND runtime_id IS NOT NULL`, gatewayID).
		Row().Scan(&runtimeID, &selectedRoot)
	if errors.Is(err, sql.ErrNoRows) {
		return domainnetworkaccess.Gateway{}, domainnetworkruntime.Credential{}, fmt.Errorf("%w: requested VPN gateway not found", apperrors.ErrNotFound)
	}
	if err != nil {
		return domainnetworkaccess.Gateway{}, domainnetworkruntime.Credential{}, err
	}
	err = r.db.WithContext(ctx).Raw(`SELECT COALESCE(hub_gateway_id, id) FROM network_access_gateways
		WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND administrative_status = 'active'`, targetGatewayID).
		Row().Scan(&targetRoot)
	if err != nil || selectedRoot != targetRoot {
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return domainnetworkaccess.Gateway{}, domainnetworkruntime.Credential{}, err
		}
		return domainnetworkaccess.Gateway{}, domainnetworkruntime.Credential{}, fmt.Errorf("%w: requested VPN gateway is outside the target topology", apperrors.ErrNotFound)
	}
	gateway, err := networkaccessrepo.New(r.db).GetGateway(ctx, gatewayID)
	if err != nil {
		return domainnetworkaccess.Gateway{}, domainnetworkruntime.Credential{}, err
	}
	var credential credentialRow
	err = r.db.WithContext(ctx).Where("tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND runtime_kind = 'gateway' AND status = ? AND not_before <= ? AND expires_at > ?", runtimeID, domainnetworkruntime.CredentialActive, now, now).Order("generation DESC").First(&credential).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return domainnetworkaccess.Gateway{}, domainnetworkruntime.Credential{}, fmt.Errorf("%w: active VPN gateway credential not found", apperrors.ErrNotFound)
	}
	if err != nil {
		return domainnetworkaccess.Gateway{}, domainnetworkruntime.Credential{}, err
	}
	return gateway, credential.domain(), nil
}

func (r *Repository) SaveVPNConnection(ctx context.Context, connection domainnetworkruntime.VPNConnection, snapshot domainnetworkruntime.PolicySnapshot) (networkprotocol.VPNConnectResult, error) {
	var result networkprotocol.VPNConnectResult
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		started, done, err := beginVPNRequest(tx, connection, snapshot)
		if err != nil {
			return err
		}
		if done {
			result = started
			return nil
		}
		state, poolExhausted, err := prepareVPNConnection(tx, &connection)
		if err != nil {
			return err
		}
		if poolExhausted {
			connection.Decision, connection.AccessProfile, connection.ReasonCode = domainnetworkaccess.DecisionDeny, domainnetworkaccess.ProfileDeny, "overlay_pool_exhausted"
			connection.SessionID, connection.GatewayID = "", ""
			result = vpnConnectResult(connection, nil, nil, 0)
			return insertVPNRequest(tx, connection, result)
		}
		networkLeases, resourceLeases, err := buildVPNLeases(&connection, state.space)
		if err != nil {
			return err
		}
		configurationVersion, err := persistVPNConnection(tx, connection, snapshot, state, networkLeases, resourceLeases)
		if err != nil {
			return err
		}
		result = vpnConnectResult(connection, networkLeases, resourceLeases, configurationVersion)
		return insertVPNRequest(tx, connection, result)
	})
	return result, err
}

type vpnConnectionState struct {
	endpointCredential credentialRow
	gateway            gatewayRuntimeRow
	gatewayCredential  credentialRow
	space              domainnetworkaccess.Space
	grant              *accessGrantRow
	address            netip.Addr
}

func beginVPNRequest(tx *gorm.DB, connection domainnetworkruntime.VPNConnection, snapshot domainnetworkruntime.PolicySnapshot) (networkprotocol.VPNConnectResult, bool, error) {
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-vpn-request:"+connection.RuntimeID+":"+connection.RequestID).Error; err != nil {
		return networkprotocol.VPNConnectResult{}, false, err
	}
	if err := lockWireGuardMutations(tx); err != nil {
		return networkprotocol.VPNConnectResult{}, false, err
	}
	var existing vpnRequestRow
	err := tx.Where("tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND request_id = ?", connection.RuntimeID, connection.RequestID).First(&existing).Error
	if err == nil {
		if existing.RequestHash != connection.RequestHash {
			return networkprotocol.VPNConnectResult{}, false, apperrors.NewBusiness(apperrors.ErrConflict, "vpn_request_reused", "The VPN request ID was already used with different content.", "VPN 请求 ID 已被不同内容使用。")
		}
		result, err := existing.domain()
		return result, true, err
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return networkprotocol.VPNConnectResult{}, false, err
	}
	if connection.Decision != domainnetworkaccess.DecisionAllow {
		result := vpnConnectResult(connection, nil, nil, 0)
		return result, true, insertVPNRequest(tx, connection, result)
	}
	if snapshot.PolicyVersion != connection.PolicyVersion || !connection.ValidUntil.After(connection.CreatedAt) {
		return networkprotocol.VPNConnectResult{}, false, apperrors.NewBusiness(apperrors.ErrConflict, "stale_vpn_decision", "The VPN decision is stale.", "VPN 决策已过期。")
	}
	return networkprotocol.VPNConnectResult{}, false, nil
}

func prepareVPNConnection(tx *gorm.DB, connection *domainnetworkruntime.VPNConnection) (vpnConnectionState, bool, error) {
	if err := lockVPNRuntimes(tx, *connection); err != nil {
		return vpnConnectionState{}, false, err
	}
	endpointCredential, gateway, gatewayCredential, space, err := lockVPNInputs(tx, *connection)
	if err != nil {
		return vpnConnectionState{}, false, err
	}
	state := vpnConnectionState{endpointCredential: endpointCredential, gateway: gateway, gatewayCredential: gatewayCredential, space: space}
	if connection.Mode != domainnetworkaccess.ModeExternalVPN {
		grant, err := lockAccessGrant(tx, *connection)
		if err != nil {
			return vpnConnectionState{}, false, err
		}
		state.grant = &grant
		connection.ValidUntil = minTime(connection.ValidUntil, grant.ExpiresAt)
	}
	if endpointCredential.ExpiresAt.Before(connection.ValidUntil) || gatewayCredential.ExpiresAt.Before(connection.ValidUntil) {
		return vpnConnectionState{}, false, apperrors.NewBusiness(apperrors.ErrConflict, "stale_vpn_decision", "The VPN credential lifetime changed.", "VPN 凭据有效期已变化。")
	}
	if err := replaceActiveVPNPeer(tx, *connection); err != nil {
		return vpnConnectionState{}, false, err
	}
	overlay, err := validatedOverlay(gateway.OverlayCIDR)
	if err != nil {
		return vpnConnectionState{}, false, apperrors.NewBusiness(apperrors.ErrConflict, "gateway_configuration_invalid", "The VPN gateway overlay is invalid.", "VPN 网关 overlay 配置无效。")
	}
	used, err := usedOverlayAddresses(tx, connection.GatewayID)
	if err != nil {
		return vpnConnectionState{}, false, err
	}
	state.address, err = allocateOverlayAddress(overlay, used)
	return state, err != nil, nil
}

func lockVPNRuntimes(tx *gorm.DB, connection domainnetworkruntime.VPNConnection) error {
	for _, runtimeID := range orderedRuntimeIDs(connection.RuntimeID, connection.GatewayRuntimeID) {
		if runtimeID == "" {
			return apperrors.NewBusiness(apperrors.ErrConflict, "gateway_unavailable", "The VPN gateway is unavailable.", "VPN 网关不可用。")
		}
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-runtime:"+runtimeID).Error; err != nil {
			return err
		}
	}
	return tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, "network-wireguard-gateway:"+connection.GatewayID).Error
}

func replaceActiveVPNPeer(tx *gorm.DB, connection domainnetworkruntime.VPNConnection) error {
	if err := expireWireGuardPeers(tx, connection.CreatedAt); err != nil {
		return err
	}
	var previousGatewayID string
	err := tx.Raw(`SELECT gateway_id FROM network_wireguard_peers WHERE tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND status = 'active' LIMIT 1`, connection.RuntimeID).Row().Scan(&previousGatewayID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if previousGatewayID != "" && previousGatewayID != connection.GatewayID {
		return apperrors.NewBusiness(apperrors.ErrConflict, "gateway_switch_not_supported", "Switching an active VPN session between gateways is not supported.", "暂不支持在活动 VPN 会话间切换网关。")
	}
	if previousGatewayID == "" {
		return nil
	}
	return revokeRuntimePeers(tx, connection.RuntimeID, connection.CreatedAt, "vpn_reconnected")
}

func buildVPNLeases(connection *domainnetworkruntime.VPNConnection, space domainnetworkaccess.Space) ([]networkprotocol.NetworkLease, []networkprotocol.ResourceLease, error) {
	switch connection.Mode {
	case domainnetworkaccess.ModeExternalVPN:
		if len(connection.ResourceIDs) != 0 {
			return nil, nil, apperrors.NewBusiness(apperrors.ErrConflict, "stale_vpn_decision", "The VPN resource scope is invalid.", "VPN 资源范围无效。")
		}
	case domainnetworkaccess.ModeInternalZTNA, domainnetworkaccess.ModeExternalVPNZTNA, domainnetworkaccess.ModeExternalDirectZTNA:
		resourceIDs := slices.Clone(connection.ResourceIDs)
		slices.Sort(resourceIDs)
		if len(resourceIDs) == 0 || len(slices.Compact(resourceIDs)) != len(resourceIDs) {
			return nil, nil, apperrors.NewBusiness(apperrors.ErrConflict, "stale_vpn_decision", "The VPN resource scope is invalid.", "VPN 资源范围无效。")
		}
		connection.ResourceIDs = resourceIDs
	default:
		return nil, nil, apperrors.NewBusiness(apperrors.ErrConflict, "stale_vpn_decision", "The VPN access mode is invalid.", "VPN 访问模式无效。")
	}
	networkLeases := []networkprotocol.NetworkLease{}
	resourceLeases := []networkprotocol.ResourceLease{}
	if connection.Mode == domainnetworkaccess.ModeExternalVPN || connection.Mode == domainnetworkaccess.ModeExternalVPNZTNA {
		networkLeases = append(networkLeases, networkprotocol.NetworkLease{
			ID: vpnLeaseID(connection.RuntimeID, connection.RequestID, "network"), SessionID: connection.SessionID,
			SubjectID: connection.SubjectID, DeviceID: connection.DeviceID, NetworkSpaceID: connection.NetworkSpaceID,
			CIDRs: slices.Clone(space.CIDRs), PolicyVersion: connection.PolicyVersion, IssuedAt: connection.CreatedAt, ExpiresAt: connection.ValidUntil,
		})
	}
	if connection.Mode != domainnetworkaccess.ModeExternalVPN {
		resourceLeases = append(resourceLeases, networkprotocol.ResourceLease{
			ID: vpnLeaseID(connection.RuntimeID, connection.RequestID, "resource"), SessionID: connection.SessionID,
			SubjectID: connection.SubjectID, DeviceID: connection.DeviceID, ResourceIDs: slices.Clone(connection.ResourceIDs),
			PolicyVersion: connection.PolicyVersion, IssuedAt: connection.CreatedAt, ExpiresAt: connection.ValidUntil,
		})
	}
	return networkLeases, resourceLeases, nil
}

func persistVPNConnection(tx *gorm.DB, connection domainnetworkruntime.VPNConnection, snapshot domainnetworkruntime.PolicySnapshot, state vpnConnectionState, networkLeases []networkprotocol.NetworkLease, resourceLeases []networkprotocol.ResourceLease) (int, error) {
	if err := insertVPNSession(tx, connection); err != nil {
		return 0, err
	}
	for _, lease := range networkLeases {
		if err := insertNetworkLease(tx, lease); err != nil {
			return 0, err
		}
	}
	for _, lease := range resourceLeases {
		if err := insertResourceLease(tx, lease); err != nil {
			return 0, err
		}
	}
	addressCIDR := netip.PrefixFrom(state.address, 32).String()
	if err := tx.Exec(`INSERT INTO network_wireguard_peers
		(id, gateway_id, runtime_id, device_id, session_id, public_key, overlay_address, status, expires_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?::inet, 'active', ?, ?, ?)`, uuid.NewString(), connection.GatewayID, connection.RuntimeID,
		connection.DeviceID, connection.SessionID, connection.EndpointPublicKey, addressCIDR, connection.ValidUntil, connection.CreatedAt, connection.CreatedAt).Error; err != nil {
		return 0, err
	}
	configurationVersion, err := persistVPNConfigurations(tx, connection, snapshot, state, networkLeases, resourceLeases, addressCIDR)
	if err != nil {
		return 0, err
	}
	if err := consumeVPNGrant(tx, state.grant, connection, resourceLeases); err != nil {
		return 0, err
	}
	return configurationVersion, nil
}

func persistVPNConfigurations(tx *gorm.DB, connection domainnetworkruntime.VPNConnection, snapshot domainnetworkruntime.PolicySnapshot, state vpnConnectionState, networkLeases []networkprotocol.NetworkLease, resourceLeases []networkprotocol.ResourceLease, addressCIDR string) (int, error) {
	mihomoProfile, err := activeEndpointMihomoProfile(tx, state.endpointCredential)
	if err != nil {
		return 0, err
	}
	endpointDesired, err := endpointVPNDesired(tx, connection, state.gateway, state.endpointCredential, state.gatewayCredential, networkLeases, resourceLeases, addressCIDR, snapshot, mihomoProfile)
	if err != nil {
		return 0, err
	}
	endpointConfig, err := insertDesiredConfiguration(tx, connection.RuntimeID, endpointDesired, connection.CreatedAt)
	if err != nil {
		return 0, err
	}
	gatewayDesired, err := gatewayVPNDesired(tx, state.gateway, state.gatewayCredential, snapshot, connection.ValidUntil, connection.CreatedAt)
	if err != nil {
		return 0, err
	}
	if _, err := insertDesiredConfiguration(tx, state.gateway.RuntimeID, gatewayDesired, connection.CreatedAt); err != nil {
		return 0, err
	}
	if err := tx.Model(&sessionRow{}).Where("id = ?", connection.SessionID).Update("configuration_version", endpointConfig.ConfigurationVersion).Error; err != nil {
		return 0, err
	}
	return endpointConfig.ConfigurationVersion, nil
}

func consumeVPNGrant(tx *gorm.DB, grant *accessGrantRow, connection domainnetworkruntime.VPNConnection, resourceLeases []networkprotocol.ResourceLease) error {
	if grant == nil {
		return nil
	}
	leaseIDs := make([]string, len(resourceLeases))
	for index := range resourceLeases {
		leaseIDs[index] = resourceLeases[index].ID
	}
	encodedLeaseIDs, err := json.Marshal(leaseIDs)
	if err != nil {
		return err
	}
	updated := tx.Model(&accessGrantRow{}).
		Where("tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND status = ?", grant.ID, domainnetworkruntime.AccessGrantIssued).
		Updates(map[string]any{"status": domainnetworkruntime.AccessGrantConsumed, "token_hash": nil, "session_id": connection.SessionID, "resource_lease_ids": jsonDocument(encodedLeaseIDs), "consumed_at": connection.CreatedAt, "updated_at": connection.CreatedAt})
	if updated.Error != nil {
		return updated.Error
	}
	if updated.RowsAffected != 1 {
		return invalidAccessGrant()
	}
	return nil
}

func lockAccessGrant(tx *gorm.DB, connection domainnetworkruntime.VPNConnection) (accessGrantRow, error) {
	if connection.AccessGrantID == "" || connection.AccessGrantTokenHash == "" {
		return accessGrantRow{}, invalidAccessGrant()
	}
	var row accessGrantRow
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("tenant_id = 'default' AND workspace_id = 'default' AND id = ?", connection.AccessGrantID).
		First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return accessGrantRow{}, invalidAccessGrant()
	}
	if err != nil {
		return accessGrantRow{}, err
	}
	if !accessGrantMatches(row, connection) {
		return accessGrantRow{}, invalidAccessGrant()
	}
	return row, nil
}

func accessGrantMatches(row accessGrantRow, connection domainnetworkruntime.VPNConnection) bool {
	resources, err := decodeUniqueSortedStrings(row.ResourceIDs)
	if err != nil {
		return false
	}
	requested, err := canonicalUniqueStrings(connection.ResourceIDs)
	return err == nil && row.TokenHash != nil && row.Status == domainnetworkruntime.AccessGrantIssued && row.ExpiresAt.After(connection.CreatedAt) &&
		subtle.ConstantTimeCompare([]byte(*row.TokenHash), []byte(connection.AccessGrantTokenHash)) == 1 &&
		row.SubjectID == connection.SubjectID && row.DeviceID == connection.DeviceID && row.SiteID == connection.SiteID &&
		row.NetworkSpaceID == connection.NetworkSpaceID && row.Mode == connection.Mode && row.PolicyVersion == connection.PolicyVersion &&
		slices.Equal(resources, requested)
}

func decodeUniqueSortedStrings(document jsonDocument) ([]string, error) {
	var values []string
	if err := json.Unmarshal(document, &values); err != nil {
		return nil, err
	}
	return canonicalUniqueStrings(values)
}

func canonicalUniqueStrings(values []string) ([]string, error) {
	values = slices.Clone(values)
	for _, value := range values {
		if value == "" || strings.TrimSpace(value) != value {
			return nil, fmt.Errorf("values must be non-empty and canonical")
		}
	}
	slices.Sort(values)
	if len(values) == 0 || len(slices.Compact(values)) != len(values) {
		return nil, fmt.Errorf("values must be non-empty and unique")
	}
	return values, nil
}

func invalidAccessGrant() error {
	return apperrors.NewBusiness(apperrors.ErrAccessDenied, "access_grant_invalid", "The network access grant is invalid or expired.", "网络访问授权无效或已过期。")
}

func desiredConfigurationForRuntime(tx *gorm.DB, runtimeID string, snapshot domainnetworkruntime.PolicySnapshot, now, expiresAt time.Time) (networkprotocol.ConfigurationDesired, error) {
	fallback := onboardingDesired(snapshot, expiresAt)
	if err := expireWireGuardPeers(tx, now); err != nil {
		return networkprotocol.ConfigurationDesired{}, err
	}
	credential, err := activeRuntimeCredential(tx, runtimeID, now)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fallback, nil
	}
	if err != nil {
		return networkprotocol.ConfigurationDesired{}, err
	}
	expiresAt = minTime(expiresAt, credential.ExpiresAt)
	fallback.ValidUntil = expiresAt.UTC()
	var mihomoProfile *domainnetworkaccess.MihomoProfile
	if credential.RuntimeKind == "endpoint" {
		mihomoProfile, err = activeEndpointMihomoProfile(tx, credential)
		if err != nil {
			return networkprotocol.ConfigurationDesired{}, err
		}
		fallback.Mihomo, err = mihomoDesired(mihomoProfile, nil, "")
		if err != nil {
			return networkprotocol.ConfigurationDesired{}, err
		}
	}
	if credential.WireGuardPublicKey == "" || !credentialHasCapability(credential, "wireguard") {
		return fallback, nil
	}
	switch credential.RuntimeKind {
	case "gateway":
		return desiredGatewayConfiguration(tx, runtimeID, credential, snapshot, fallback, now, expiresAt)
	case "endpoint":
		return desiredEndpointConfiguration(tx, runtimeID, credential, snapshot, fallback, mihomoProfile, now, expiresAt)
	default:
		return fallback, nil
	}
}

func desiredGatewayConfiguration(tx *gorm.DB, runtimeID string, credential credentialRow, snapshot domainnetworkruntime.PolicySnapshot, fallback networkprotocol.ConfigurationDesired, now, expiresAt time.Time) (networkprotocol.ConfigurationDesired, error) {
	gateway, err := gatewayRuntimeByRuntimeID(tx, runtimeID)
	if errors.Is(err, sql.ErrNoRows) {
		return fallback, nil
	}
	if err != nil {
		return networkprotocol.ConfigurationDesired{}, err
	}
	if _, err := validatedOverlay(gateway.OverlayCIDR); err != nil {
		return fallback, nil
	}
	return gatewayVPNDesired(tx, gateway, credential, snapshot, expiresAt, now)
}

func desiredEndpointConfiguration(tx *gorm.DB, runtimeID string, credential credentialRow, snapshot domainnetworkruntime.PolicySnapshot, fallback networkprotocol.ConfigurationDesired, mihomoProfile *domainnetworkaccess.MihomoProfile, now, expiresAt time.Time) (networkprotocol.ConfigurationDesired, error) {
	state, err := activeEndpointVPNState(tx, runtimeID, now)
	if errors.Is(err, sql.ErrNoRows) {
		return fallback, nil
	}
	if err != nil {
		return networkprotocol.ConfigurationDesired{}, err
	}
	gateway, err := gatewayRuntimeByID(tx, state.GatewayID)
	if errors.Is(err, sql.ErrNoRows) {
		return fallback, nil
	}
	if err != nil {
		return networkprotocol.ConfigurationDesired{}, err
	}
	gatewayCredential, err := activeRuntimeCredential(tx, gateway.RuntimeID, now)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fallback, nil
	}
	if err != nil {
		return networkprotocol.ConfigurationDesired{}, err
	}
	state.Connection.ValidUntil = minTime(expiresAt, minTime(state.Connection.ValidUntil, gatewayCredential.ExpiresAt))
	for index := range state.NetworkLeases {
		state.NetworkLeases[index].ExpiresAt = minTime(state.NetworkLeases[index].ExpiresAt, state.Connection.ValidUntil)
	}
	for index := range state.ResourceLeases {
		state.ResourceLeases[index].ExpiresAt = minTime(state.ResourceLeases[index].ExpiresAt, state.Connection.ValidUntil)
	}
	return endpointVPNDesired(tx, state.Connection, gateway, credential, gatewayCredential, state.NetworkLeases, state.ResourceLeases, state.Address, snapshot, mihomoProfile)
}

func onboardingDesired(snapshot domainnetworkruntime.PolicySnapshot, validUntil time.Time) networkprotocol.ConfigurationDesired {
	return networkprotocol.ConfigurationDesired{
		PolicyVersion: snapshot.PolicyVersion, ValidUntil: validUntil.UTC(), AccessProfile: domainnetworkaccess.ProfileOnboarding,
		ProtectedResourceIDs: slices.Clone(snapshot.ProtectedResourceIDs), NetworkLeases: []networkprotocol.NetworkLease{}, ResourceLeases: []networkprotocol.ResourceLease{},
	}
}

func activeRuntimeCredential(tx *gorm.DB, runtimeID string, now time.Time) (credentialRow, error) {
	var credential credentialRow
	err := tx.Where("tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND status = 'active' AND not_before <= ? AND expires_at > ?", runtimeID, now, now).Order("generation DESC").First(&credential).Error
	return credential, err
}

type activeEndpointState struct {
	GatewayID      string
	Address        string
	Connection     domainnetworkruntime.VPNConnection
	NetworkLeases  []networkprotocol.NetworkLease
	ResourceLeases []networkprotocol.ResourceLease
}

func activeEndpointVPNState(tx *gorm.DB, runtimeID string, now time.Time) (activeEndpointState, error) {
	var state activeEndpointState
	err := tx.Raw(`SELECT p.gateway_id, p.overlay_address::text, s.id, s.subject_id, s.device_id, s.site_id, s.mode,
		s.access_profile, s.policy_version, LEAST(p.expires_at, s.valid_until)
		FROM network_wireguard_peers p JOIN network_runtime_sessions s ON s.id = p.session_id
		WHERE p.tenant_id = 'default' AND p.workspace_id = 'default' AND p.runtime_id = ? AND p.status = 'active' AND p.expires_at > ?
		AND s.status IN ('pending', 'active', 'restricted', 'quarantine') AND s.valid_until > ?
		LIMIT 1`, runtimeID, now, now).Row().Scan(
		&state.GatewayID, &state.Address, &state.Connection.SessionID, &state.Connection.SubjectID, &state.Connection.DeviceID,
		&state.Connection.SiteID, &state.Connection.Mode, &state.Connection.AccessProfile, &state.Connection.PolicyVersion,
		&state.Connection.ValidUntil)
	if err != nil {
		return state, err
	}
	state.NetworkLeases, state.ResourceLeases, err = activeSessionLeases(tx, state.Connection.SessionID, now)
	if err != nil {
		return state, err
	}
	if len(state.NetworkLeases) == 0 && len(state.ResourceLeases) == 0 {
		return state, sql.ErrNoRows
	}
	state.Connection.RuntimeID, state.Connection.GatewayID = runtimeID, state.GatewayID
	if len(state.NetworkLeases) != 0 {
		state.Connection.NetworkSpaceID = state.NetworkLeases[0].NetworkSpaceID
	}
	for _, lease := range state.NetworkLeases {
		state.Connection.ValidUntil = minTime(state.Connection.ValidUntil, lease.ExpiresAt)
	}
	for _, lease := range state.ResourceLeases {
		state.Connection.ResourceIDs = append(state.Connection.ResourceIDs, lease.ResourceIDs...)
		state.Connection.ValidUntil = minTime(state.Connection.ValidUntil, lease.ExpiresAt)
	}
	return state, nil
}

func activeSessionLeases(tx *gorm.DB, sessionID string, now time.Time) ([]networkprotocol.NetworkLease, []networkprotocol.ResourceLease, error) {
	var rows []leaseRow
	if err := tx.Where("tenant_id = 'default' AND workspace_id = 'default' AND session_id = ? AND status IN ('issued', 'active') AND expires_at > ?", sessionID, now).Order("lease_kind, id").Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	networkLeases := []networkprotocol.NetworkLease{}
	resourceLeases := []networkprotocol.ResourceLease{}
	for _, row := range rows {
		switch row.LeaseKind {
		case "network":
			lease, err := row.networkLease()
			if err != nil {
				return nil, nil, err
			}
			networkLeases = append(networkLeases, lease)
		case "resource":
			lease, err := row.resourceLease()
			if err != nil {
				return nil, nil, err
			}
			resourceLeases = append(resourceLeases, lease)
		}
	}
	return networkLeases, resourceLeases, nil
}

type gatewayRuntimeRow struct {
	ID                         string
	RuntimeID                  string
	SiteID                     string
	HubGatewayID               string
	PublicEndpointHost         string
	PublicEndpointPort         int
	OverlayCIDR                string
	RoutingMode                string
	AdvertisedCIDRs            []string
	MTU                        int
	PersistentKeepaliveSeconds int
	DNSServers                 []string
}

type rowScanner interface{ Scan(...any) error }

func gatewayRuntimeByID(tx *gorm.DB, gatewayID string) (gatewayRuntimeRow, error) {
	return scanGatewayRuntime(tx.Raw(`SELECT id, runtime_id, site_id, COALESCE(hub_gateway_id, ''), public_endpoint_host, public_endpoint_port, overlay_cidr::text,
		routing_mode, advertised_cidrs, mtu, persistent_keepalive_seconds, dns_servers FROM network_access_gateways
		WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND administrative_status = 'active'`, gatewayID).Row())
}

func gatewayRuntimeByRuntimeID(tx *gorm.DB, runtimeID string) (gatewayRuntimeRow, error) {
	return scanGatewayRuntime(tx.Raw(`SELECT id, runtime_id, site_id, COALESCE(hub_gateway_id, ''), public_endpoint_host, public_endpoint_port, overlay_cidr::text,
		routing_mode, advertised_cidrs, mtu, persistent_keepalive_seconds, dns_servers FROM network_access_gateways
		WHERE tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND administrative_status = 'active'`, runtimeID).Row())
}

func scanGatewayRuntime(row rowScanner) (gatewayRuntimeRow, error) {
	var gateway gatewayRuntimeRow
	var advertised, dns []byte
	err := row.Scan(&gateway.ID, &gateway.RuntimeID, &gateway.SiteID, &gateway.HubGatewayID, &gateway.PublicEndpointHost, &gateway.PublicEndpointPort,
		&gateway.OverlayCIDR, &gateway.RoutingMode, &advertised, &gateway.MTU, &gateway.PersistentKeepaliveSeconds, &dns)
	if err != nil {
		return gateway, err
	}
	if err := json.Unmarshal(dns, &gateway.DNSServers); err != nil {
		return gateway, fmt.Errorf("decode gateway DNS servers: %w", err)
	}
	if err := json.Unmarshal(advertised, &gateway.AdvertisedCIDRs); err != nil {
		return gateway, fmt.Errorf("decode gateway advertised CIDRs: %w", err)
	}
	return gateway, nil
}

func validatedOverlay(raw string) (netip.Prefix, error) {
	overlay, err := netip.ParsePrefix(raw)
	if err != nil || !overlay.Addr().Is4() || overlay != overlay.Masked() || overlay.Bits() < 16 || overlay.Bits() > 30 {
		return netip.Prefix{}, fmt.Errorf("invalid WireGuard overlay prefix")
	}
	return overlay, nil
}

func orderedRuntimeIDs(left, right string) []string {
	if left == right {
		return []string{left}
	}
	values := []string{left, right}
	slices.Sort(values)
	return values
}

func orderedUniqueRuntimeIDs(values ...string) []string {
	slices.Sort(values)
	return slices.Compact(values)
}

func lockWireGuardMutations(tx *gorm.DB) error {
	// ponytail: Phase 4 serializes low-frequency WireGuard mutations globally; replace with gateway-scoped locks when measured throughput requires it.
	return tx.Exec(`SELECT pg_advisory_xact_lock(hashtext('network-wireguard-mutations'))`).Error
}

func credentialRotationReason(previous credentialRow, exists bool, enrollment enrollmentRow, consumption domainnetworkruntime.EnrollmentConsumption) string {
	if !exists {
		return ""
	}
	if previous.RuntimeKind != consumption.RuntimeKind || previous.DeviceID != enrollment.DeviceID || previous.SubjectID != enrollment.SubjectID {
		return "runtime_binding_changed"
	}
	previousWireGuard := previous.WireGuardPublicKey != "" && credentialHasCapability(previous, "wireguard")
	nextWireGuard := consumption.WireGuardPublicKey != "" && slices.Contains(consumption.Capabilities, "wireguard")
	if previousWireGuard && !nextWireGuard {
		return "wireguard_capability_removed"
	}
	if previous.WireGuardPublicKey != consumption.WireGuardPublicKey {
		return "wireguard_key_rotated"
	}
	return ""
}

func credentialHasCapability(credential credentialRow, capability string) bool {
	var capabilities []string
	return json.Unmarshal(credential.Capabilities, &capabilities) == nil && slices.Contains(capabilities, capability)
}

func relatedWireGuardRuntimeIDs(tx *gorm.DB, runtimeID, runtimeKind string) ([]string, error) {
	query := `SELECT DISTINCT g.runtime_id FROM network_wireguard_peers p JOIN network_access_gateways g ON g.id = p.gateway_id
		WHERE p.tenant_id = 'default' AND p.workspace_id = 'default' AND p.runtime_id = ? AND p.status = 'active'`
	if runtimeKind == "gateway" {
		query = `SELECT DISTINCT p.runtime_id FROM network_wireguard_peers p JOIN network_access_gateways g ON g.id = p.gateway_id
			WHERE p.tenant_id = 'default' AND p.workspace_id = 'default' AND g.runtime_id = ? AND p.status = 'active'
			UNION SELECT related.runtime_id FROM network_access_gateways current
			JOIN network_access_gateways related ON
				(current.hub_gateway_id IS NULL AND related.hub_gateway_id = current.id)
				OR (current.hub_gateway_id IS NOT NULL AND related.id = current.hub_gateway_id)
			WHERE current.tenant_id = 'default' AND current.workspace_id = 'default' AND current.runtime_id = ?
			AND related.tenant_id = 'default' AND related.workspace_id = 'default' AND related.administrative_status = 'active'`
	}
	args := []any{runtimeID}
	if runtimeKind == "gateway" {
		args = append(args, runtimeID)
	}
	rows, err := tx.Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	runtimeIDs := []string{}
	for rows.Next() {
		var relatedRuntimeID string
		if err := rows.Scan(&relatedRuntimeID); err != nil {
			return nil, err
		}
		runtimeIDs = append(runtimeIDs, relatedRuntimeID)
	}
	return runtimeIDs, rows.Err()
}

func vpnGatewayRuntimeForSession(tx *gorm.DB, runtimeID, sessionID string) (string, error) {
	var gatewayRuntimeID string
	err := tx.Raw(`SELECT g.runtime_id FROM network_wireguard_peers p
		JOIN network_access_gateways g ON g.id = p.gateway_id
		WHERE p.tenant_id = 'default' AND p.workspace_id = 'default' AND p.runtime_id = ? AND p.session_id = ? AND p.status = 'active' LIMIT 1`, runtimeID, sessionID).Row().Scan(&gatewayRuntimeID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return gatewayRuntimeID, err
}

func lockVPNInputs(tx *gorm.DB, connection domainnetworkruntime.VPNConnection) (credentialRow, gatewayRuntimeRow, credentialRow, domainnetworkaccess.Space, error) {
	var endpointCredential, gatewayCredential credentialRow
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND runtime_id = ? AND runtime_kind = 'endpoint' AND status = 'active' AND expires_at >= ?", connection.CredentialID, connection.RuntimeID, connection.ValidUntil).First(&endpointCredential).Error; err != nil {
		return endpointCredential, gatewayRuntimeRow{}, gatewayCredential, domainnetworkaccess.Space{}, normalizeVPNInputError(err, "endpoint credential")
	}
	if endpointCredential.SubjectID != connection.SubjectID || endpointCredential.DeviceID != connection.DeviceID || endpointCredential.WireGuardPublicKey != connection.EndpointPublicKey {
		return endpointCredential, gatewayRuntimeRow{}, gatewayCredential, domainnetworkaccess.Space{}, apperrors.NewBusiness(apperrors.ErrConflict, "stale_vpn_decision", "The endpoint credential changed.", "端点凭据已变化。")
	}
	var postureVersion int
	err := tx.Raw(`SELECT posture_version FROM network_access_devices
		WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND owner_user_id::text = ?
		AND (? NOT IN (?, ?) OR COALESCE(site_id, '') = ?) FOR UPDATE`, connection.DeviceID, connection.SubjectID,
		connection.Mode, domainnetworkaccess.ModeInternalDirect, domainnetworkaccess.ModeInternalZTNA, connection.SiteID).Row().Scan(&postureVersion)
	if err != nil {
		return endpointCredential, gatewayRuntimeRow{}, gatewayCredential, domainnetworkaccess.Space{}, normalizeVPNInputError(err, "endpoint device")
	}
	if postureVersion != connection.PostureVersion {
		return endpointCredential, gatewayRuntimeRow{}, gatewayCredential, domainnetworkaccess.Space{}, apperrors.NewBusiness(apperrors.ErrConflict, "stale_vpn_decision", "The endpoint posture changed.", "端点设备状态已变化。")
	}
	if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext('network-gateway-topology:default:default'))`).Error; err != nil {
		return endpointCredential, gatewayRuntimeRow{}, gatewayCredential, domainnetworkaccess.Space{}, err
	}
	gateway, err := scanGatewayRuntime(tx.Raw(`SELECT id, runtime_id, site_id, COALESCE(hub_gateway_id, ''), public_endpoint_host, public_endpoint_port, overlay_cidr::text,
		routing_mode, advertised_cidrs, mtu, persistent_keepalive_seconds, dns_servers
		FROM network_access_gateways WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ?
		AND administrative_status = 'active' FOR UPDATE`, connection.GatewayID).Row())
	if err != nil {
		return endpointCredential, gateway, gatewayCredential, domainnetworkaccess.Space{}, normalizeVPNInputError(err, "gateway")
	}
	if reachable, err := gatewayReachesSite(tx, gateway, connection.SiteID); err != nil {
		return endpointCredential, gateway, gatewayCredential, domainnetworkaccess.Space{}, err
	} else if !reachable {
		return endpointCredential, gateway, gatewayCredential, domainnetworkaccess.Space{}, apperrors.NewBusiness(apperrors.ErrConflict, "gateway_topology_changed", "The VPN gateway topology changed.", "VPN 网关拓扑已变化。")
	}
	if gateway.RuntimeID != connection.GatewayRuntimeID {
		return endpointCredential, gateway, gatewayCredential, domainnetworkaccess.Space{}, apperrors.NewBusiness(apperrors.ErrConflict, "stale_vpn_decision", "The gateway runtime changed.", "网关运行时已变化。")
	}
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND runtime_id = ? AND runtime_kind = 'gateway' AND status = 'active' AND expires_at >= ?", connection.GatewayCredentialID, gateway.RuntimeID, connection.ValidUntil).First(&gatewayCredential).Error; err != nil {
		return endpointCredential, gateway, gatewayCredential, domainnetworkaccess.Space{}, normalizeVPNInputError(err, "gateway credential")
	}
	if gatewayCredential.WireGuardPublicKey == "" {
		return endpointCredential, gateway, gatewayCredential, domainnetworkaccess.Space{}, apperrors.NewBusiness(apperrors.ErrConflict, "gateway_configuration_invalid", "The gateway WireGuard key is unavailable.", "网关 WireGuard 公钥不可用。")
	}
	space, err := lockVPNSpace(tx, connection, gateway, gatewayCredential)
	return endpointCredential, gateway, gatewayCredential, space, err
}

func lockVPNSpace(tx *gorm.DB, connection domainnetworkruntime.VPNConnection, gateway gatewayRuntimeRow, gatewayCredential credentialRow) (domainnetworkaccess.Space, error) {
	var space domainnetworkaccess.Space
	var cidrs []byte
	err := tx.Raw(`SELECT id, site_id, cidrs, status FROM network_access_spaces
		WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND site_id = ? AND status = 'active' FOR UPDATE`, connection.NetworkSpaceID, connection.SiteID).Row().Scan(&space.ID, &space.SiteID, &cidrs, &space.Status)
	if err != nil {
		return space, normalizeVPNInputError(err, "network space")
	}
	if err := json.Unmarshal(cidrs, &space.CIDRs); err != nil {
		return space, fmt.Errorf("decode network space CIDRs: %w", err)
	}
	for _, cidr := range space.CIDRs {
		prefix, parseErr := netip.ParsePrefix(cidr)
		if parseErr != nil || !prefix.Addr().Is4() || prefix != prefix.Masked() || prefix.Bits() == 0 {
			return space, apperrors.NewBusiness(apperrors.ErrConflict, "network_scope_invalid", "The network space is not a valid split-tunnel scope.", "网络空间不是有效的分流范围。")
		}
	}
	if gateway.SiteID != connection.SiteID {
		members, err := gatewayTopologyMembers(tx, gateway, gatewayCredential, connection.CreatedAt)
		if err != nil {
			return space, err
		}
		plan, err := compileGatewaySitePlan(gateway, members, nil, connection.ValidUntil)
		if err != nil {
			return space, err
		}
		if !routesCoverAll(plan.routes, space.CIDRs) || plan.validUntil.Before(connection.ValidUntil) {
			return space, apperrors.NewBusiness(apperrors.ErrConflict, "gateway_topology_unavailable", "The selected VPN gateway cannot currently reach the target network.", "所选 VPN 网关当前无法到达目标网络。")
		}
	}
	return space, nil
}

func gatewayReachesSite(tx *gorm.DB, gateway gatewayRuntimeRow, siteID string) (bool, error) {
	var targetRoot string
	err := tx.Raw(`SELECT COALESCE(hub_gateway_id, id) FROM network_access_gateways
		WHERE tenant_id = 'default' AND workspace_id = 'default' AND site_id = ? AND administrative_status = 'active' LIMIT 1`, siteID).
		Row().Scan(&targetRoot)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	selectedRoot := gateway.ID
	if gateway.HubGatewayID != "" {
		selectedRoot = gateway.HubGatewayID
	}
	return selectedRoot == targetRoot, nil
}

func normalizeVPNInputError(err error, name string) error {
	if errors.Is(err, gorm.ErrRecordNotFound) || errors.Is(err, sql.ErrNoRows) {
		return apperrors.NewBusiness(apperrors.ErrConflict, "stale_vpn_decision", "The "+name+" changed before the VPN session was saved.", "VPN 会话保存前依赖状态已变化。")
	}
	return err
}

func expireWireGuardPeers(tx *gorm.DB, now time.Time) error {
	if err := tx.Exec(`UPDATE network_wireguard_peers SET status = 'expired', updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND status = 'active' AND expires_at <= ?`, now, now).Error; err != nil {
		return err
	}
	if err := tx.Exec(`UPDATE network_runtime_leases SET status = 'expired', updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND status IN ('issued', 'active', 'revoking') AND expires_at <= ?`, now, now).Error; err != nil {
		return err
	}
	return tx.Exec(`UPDATE network_runtime_sessions SET status = 'expired', updated_at = ? WHERE tenant_id = 'default' AND workspace_id = 'default' AND status IN ('pending', 'active', 'restricted', 'quarantine') AND valid_until <= ?`, now, now).Error
}

func revokeRuntimePeers(tx *gorm.DB, runtimeID string, now time.Time, reason string) error {
	if err := tx.Exec(`UPDATE network_runtime_leases SET status = 'revoked', revoked_at = ?, revoke_reason = ?, updated_at = ?
		WHERE tenant_id = 'default' AND workspace_id = 'default' AND session_id IN
		(SELECT session_id FROM network_wireguard_peers WHERE tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND status = 'active')
		AND status IN ('issued', 'active', 'revoking')`, now, reason, now, runtimeID).Error; err != nil {
		return err
	}
	if err := tx.Exec(`UPDATE network_runtime_sessions SET status = 'revoked', revoked_at = ?, revoke_reason = ?, updated_at = ?
		WHERE tenant_id = 'default' AND workspace_id = 'default' AND id IN
		(SELECT session_id FROM network_wireguard_peers WHERE tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND status = 'active')`, now, reason, now, runtimeID).Error; err != nil {
		return err
	}
	return tx.Exec(`UPDATE network_wireguard_peers SET status = 'revoked', revoked_at = ?, revoke_reason = ?, updated_at = ?
		WHERE tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND status = 'active'`, now, reason, now, runtimeID).Error
}

func usedOverlayAddresses(tx *gorm.DB, gatewayID string) (map[netip.Addr]struct{}, error) {
	rows, err := tx.Raw(`SELECT overlay_address::text FROM network_wireguard_peers WHERE tenant_id = 'default' AND workspace_id = 'default' AND gateway_id = ? AND status = 'active' ORDER BY overlay_address`, gatewayID).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	used := map[netip.Addr]struct{}{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		prefix, err := netip.ParsePrefix(raw)
		if err != nil {
			return nil, fmt.Errorf("parse allocated overlay address: %w", err)
		}
		used[prefix.Addr()] = struct{}{}
	}
	return used, rows.Err()
}

func allocateOverlayAddress(prefix netip.Prefix, used map[netip.Addr]struct{}) (netip.Addr, error) {
	if !prefix.Addr().Is4() || prefix != prefix.Masked() || prefix.Bits() < 16 || prefix.Bits() > 30 {
		return netip.Addr{}, fmt.Errorf("invalid WireGuard overlay prefix")
	}
	// The first usable address belongs to the gateway and the final address is the IPv4 broadcast address.
	for candidate := prefix.Addr().Next().Next(); prefix.Contains(candidate) && prefix.Contains(candidate.Next()); candidate = candidate.Next() {
		if _, exists := used[candidate]; !exists {
			return candidate, nil
		}
	}
	return netip.Addr{}, fmt.Errorf("WireGuard overlay address pool exhausted")
}

func insertVPNSession(tx *gorm.DB, connection domainnetworkruntime.VPNConnection) error {
	return tx.Exec(`INSERT INTO network_runtime_sessions
		(id, runtime_id, subject_id, device_id, site_id, gateway_id, mode, access_profile, status, policy_version,
		 configuration_version, posture_version, valid_until, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?)`, connection.SessionID, connection.RuntimeID,
		connection.SubjectID, connection.DeviceID, connection.SiteID, connection.GatewayID, connection.Mode,
		connection.AccessProfile, nasSessionStatus(connection.AccessProfile), connection.PolicyVersion,
		connection.PostureVersion, connection.ValidUntil, connection.CreatedAt, connection.CreatedAt).Error
}

func insertNetworkLease(tx *gorm.DB, lease networkprotocol.NetworkLease) error {
	cidrs, err := json.Marshal(lease.CIDRs)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO network_runtime_leases
		(id, session_id, lease_kind, subject_id, device_id, network_space_id, cidrs, resource_ids, policy_version,
		 status, issued_at, expires_at, created_at, updated_at)
		VALUES (?, ?, 'network', ?, ?, ?, ?::jsonb, '[]'::jsonb, ?, 'issued', ?, ?, ?, ?)`, lease.ID, lease.SessionID,
		lease.SubjectID, lease.DeviceID, lease.NetworkSpaceID, string(cidrs), lease.PolicyVersion,
		lease.IssuedAt, lease.ExpiresAt, lease.IssuedAt, lease.IssuedAt).Error
}

func insertResourceLease(tx *gorm.DB, lease networkprotocol.ResourceLease) error {
	resourceIDs, err := json.Marshal(lease.ResourceIDs)
	if err != nil {
		return err
	}
	return tx.Exec(`INSERT INTO network_runtime_leases
		(id, session_id, lease_kind, subject_id, device_id, network_space_id, cidrs, resource_ids, policy_version,
		 status, issued_at, expires_at, created_at, updated_at)
		VALUES (?, ?, 'resource', ?, ?, NULL, '[]'::jsonb, ?::jsonb, ?, 'issued', ?, ?, ?, ?)`, lease.ID, lease.SessionID,
		lease.SubjectID, lease.DeviceID, string(resourceIDs), lease.PolicyVersion,
		lease.IssuedAt, lease.ExpiresAt, lease.IssuedAt, lease.IssuedAt).Error
}

func vpnLeaseID(runtimeID, requestID, kind string) string {
	digest := sha256.Sum256([]byte(runtimeID + "\n" + requestID + "\n" + kind))
	return fmt.Sprintf("lease-%x", digest)
}

func vpnConnectResult(connection domainnetworkruntime.VPNConnection, networkLeases []networkprotocol.NetworkLease, resourceLeases []networkprotocol.ResourceLease, configurationVersion int) networkprotocol.VPNConnectResult {
	if networkLeases == nil {
		networkLeases = []networkprotocol.NetworkLease{}
	}
	if resourceLeases == nil {
		resourceLeases = []networkprotocol.ResourceLease{}
	}
	result := networkprotocol.VPNConnectResult{RequestID: connection.RequestID, Decision: connection.Decision, ReasonCode: connection.ReasonCode, PolicyVersion: connection.PolicyVersion, ValidUntil: connection.ValidUntil, NetworkLeases: networkLeases, ResourceLeases: resourceLeases}
	if connection.Decision == domainnetworkaccess.DecisionAllow {
		result.SessionID, result.GatewayID, result.ConfigurationVersion = connection.SessionID, connection.GatewayID, configurationVersion
	}
	return result
}

type vpnRequestRow struct {
	TenantID      string       `gorm:"primaryKey;column:tenant_id"`
	WorkspaceID   string       `gorm:"primaryKey;column:workspace_id"`
	RuntimeID     string       `gorm:"primaryKey;column:runtime_id"`
	RequestID     string       `gorm:"primaryKey;column:request_id"`
	RequestHash   string       `gorm:"column:request_hash"`
	Decision      string       `gorm:"column:decision"`
	SessionID     *string      `gorm:"column:session_id"`
	GatewayID     *string      `gorm:"column:gateway_id"`
	ResultPayload jsonDocument `gorm:"column:result_payload;type:jsonb"`
	ValidUntil    time.Time    `gorm:"column:valid_until"`
	CreatedAt     time.Time    `gorm:"column:created_at"`
}

func (vpnRequestRow) TableName() string { return "network_vpn_connection_requests" }

func (row vpnRequestRow) domain() (networkprotocol.VPNConnectResult, error) {
	var result networkprotocol.VPNConnectResult
	err := json.Unmarshal(row.ResultPayload, &result)
	return result, err
}

func insertVPNRequest(tx *gorm.DB, connection domainnetworkruntime.VPNConnection, result networkprotocol.VPNConnectResult) error {
	payload, err := json.Marshal(result)
	if err != nil {
		return err
	}
	row := vpnRequestRow{TenantID: "default", WorkspaceID: "default", RuntimeID: connection.RuntimeID, RequestID: connection.RequestID,
		RequestHash: connection.RequestHash, Decision: connection.Decision, ResultPayload: jsonDocument(payload), ValidUntil: connection.ValidUntil, CreatedAt: connection.CreatedAt}
	if connection.Decision == domainnetworkaccess.DecisionAllow {
		row.SessionID, row.GatewayID = &connection.SessionID, &connection.GatewayID
	}
	return tx.Create(&row).Error
}

type vpnPeerState struct {
	SessionID     string
	RuntimeID     string
	DeviceID      string
	PublicKey     string
	Address       string
	AccessProfile string
}

type leasedResourceTarget struct {
	ResourceID      string
	DestinationCIDR string
	Protocol        string
	Ports           []int
	LeaseID         string
	ExpiresAt       time.Time
}

func endpointVPNDesired(tx *gorm.DB, connection domainnetworkruntime.VPNConnection, gateway gatewayRuntimeRow, credential, gatewayCredential credentialRow, networkLeases []networkprotocol.NetworkLease, resourceLeases []networkprotocol.ResourceLease, address string, snapshot domainnetworkruntime.PolicySnapshot, mihomoProfile *domainnetworkaccess.MihomoProfile) (networkprotocol.ConfigurationDesired, error) {
	space, routes, resourceTargets, err := compileLeaseAccess(tx, networkLeases, resourceLeases)
	if err != nil {
		return networkprotocol.ConfigurationDesired{}, err
	}
	denied, err := protectedDestinations(tx, space.ID, snapshot.ProtectedResourceIDs, space.CIDRs)
	if err != nil {
		return networkprotocol.ConfigurationDesired{}, err
	}
	validUntil := connection.ValidUntil
	for _, lease := range networkLeases {
		validUntil = minTime(validUntil, lease.ExpiresAt)
	}
	for _, lease := range resourceLeases {
		validUntil = minTime(validUntil, lease.ExpiresAt)
	}
	desired := networkprotocol.ConfigurationDesired{
		PolicyVersion: snapshot.PolicyVersion, ValidUntil: validUntil, AccessProfile: connection.AccessProfile,
		ProtectedResourceIDs: slices.Clone(snapshot.ProtectedResourceIDs), NetworkLeases: slices.Clone(networkLeases), ResourceLeases: slices.Clone(resourceLeases),
		WireGuard: &networkprotocol.WireGuardConfiguration{
			Role: "endpoint", InterfaceName: "soha0", PublicKey: credential.WireGuardPublicKey, Addresses: []string{address},
			MTU: gateway.MTU, RoutingMode: gateway.RoutingMode, FirewallDefault: "deny",
			Peers:  []networkprotocol.WireGuardPeer{{RuntimeID: gateway.RuntimeID, PublicKey: gatewayCredential.WireGuardPublicKey, EndpointHost: gateway.PublicEndpointHost, EndpointPort: gateway.PublicEndpointPort, AllowedIPs: slices.Clone(routes), PersistentKeepaliveSeconds: gateway.PersistentKeepaliveSeconds}},
			Routes: slices.Clone(routes), DNSServers: dnsWithinRoutes(gateway.DNSServers, routes), FirewallRules: leaseFirewallRules(address, networkLeases, resourceTargets, denied),
		},
	}
	desired.Mihomo, err = mihomoDesired(mihomoProfile, desired.WireGuard, gateway.PublicEndpointHost)
	return desired, err
}

func activeEndpointMihomoProfile(tx *gorm.DB, credential credentialRow) (*domainnetworkaccess.MihomoProfile, error) {
	if credential.RuntimeKind != "endpoint" || !credentialHasCapability(credential, "mihomo") {
		return nil, nil
	}
	profiles, err := networkaccessrepo.New(tx).ListMihomoProfiles(tx.Statement.Context, domainnetworkaccess.MihomoProfileFilter{
		DeviceID: credential.DeviceID, Status: domainnetworkaccess.StatusActive, Limit: 2,
	})
	if err != nil {
		return nil, err
	}
	if len(profiles) == 0 {
		return nil, nil
	}
	if len(profiles) != 1 {
		return nil, apperrors.NewBusiness(apperrors.ErrConflict, "mihomo_profile_ambiguous", "The endpoint has multiple active mihomo profiles.", "端点存在多个活动 mihomo 配置。")
	}
	return &profiles[0], nil
}

func mihomoDesired(profile *domainnetworkaccess.MihomoProfile, wireGuard *networkprotocol.WireGuardConfiguration, gatewayHost string) (*networkprotocol.MihomoConfiguration, error) {
	if profile == nil {
		return nil, nil
	}
	desired := &networkprotocol.MihomoConfiguration{
		Mode: profile.Mode, SourceType: profile.SourceType, ProfileID: profile.ID, ProfileRevision: profile.Revision,
		MixedPort: profile.MixedPort, ControllerPort: profile.ControllerPort, DNSMode: profile.DNSMode,
		FakeIPRange: profile.FakeIPRange, SelectorGroup: profile.SelectorGroup, SelectedProxy: profile.SelectedProxy,
		BypassCIDRs: slices.Clone(profile.BypassCIDRs), BypassHosts: slices.Clone(profile.BypassHosts), FailClosed: profile.FailClosed,
	}
	if wireGuard != nil {
		desired.BypassCIDRs = append(desired.BypassCIDRs, wireGuard.Routes...)
		if gatewayHost != "" {
			desired.BypassHosts = append(desired.BypassHosts, gatewayHost)
		}
	}

	fakeIP, err := mihomoFakeIP(desired.FakeIPRange)
	if err != nil {
		return nil, err
	}
	if err := validateMihomoBypass(desired.BypassCIDRs, fakeIP); err != nil {
		return nil, err
	}
	if fakeIP.IsValid() && wireGuard != nil && len(wireGuard.DNSServers) != 0 {
		return nil, apperrors.NewBusiness(apperrors.ErrConflict, "mihomo_wireguard_dns_conflict", "mihomo fake-IP DNS cannot run while WireGuard owns endpoint DNS.", "WireGuard 管理端点 DNS 时不能启用 mihomo fake-IP DNS。")
	}
	desired.BypassCIDRs = collapseRoutes(desired.BypassCIDRs)
	slices.Sort(desired.BypassHosts)
	desired.BypassHosts = slices.Compact(desired.BypassHosts)
	if len(desired.BypassCIDRs) > 256 || len(desired.BypassHosts) > 128 {
		return nil, apperrors.NewBusiness(apperrors.ErrConflict, "mihomo_configuration_limit", "The merged mihomo bypass list exceeds its limit.", "合并后的 mihomo 绕过列表超过限制。")
	}
	return desired, nil
}

func mihomoFakeIP(raw string) (netip.Prefix, error) {
	if raw == "" {
		return netip.Prefix{}, nil
	}
	prefix, err := netip.ParsePrefix(raw)
	if err != nil || !prefix.Addr().Is4() || prefix != prefix.Masked() || prefix.Bits() == 0 {
		return netip.Prefix{}, invalidMihomoRuntimeConfiguration()
	}
	return prefix, nil
}

func validateMihomoBypass(cidrs []string, fakeIP netip.Prefix) error {
	for _, raw := range cidrs {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil || !prefix.Addr().Is4() || prefix != prefix.Masked() || prefix.Bits() == 0 {
			return invalidMihomoRuntimeConfiguration()
		}
		if fakeIP.IsValid() && fakeIP.Overlaps(prefix) {
			return apperrors.NewBusiness(apperrors.ErrConflict, "mihomo_wireguard_overlap", "The mihomo fake-IP range overlaps an authorized network route.", "mihomo fake-IP 地址段与授权网络路由重叠。")
		}
	}
	return nil
}

func invalidMihomoRuntimeConfiguration() error {
	return apperrors.NewBusiness(apperrors.ErrConflict, "mihomo_configuration_invalid", "The mihomo profile contains an invalid network prefix.", "mihomo 配置包含无效网络地址段。")
}

type gatewayTopologyMember struct {
	gateway    gatewayRuntimeRow
	credential credentialRow
}

type gatewaySitePlan struct {
	peers         []networkprotocol.WireGuardPeer
	routes        []string
	firewallRules []networkprotocol.WireGuardFirewallRule
	validUntil    time.Time
}

func gatewayTopologyMembers(tx *gorm.DB, local gatewayRuntimeRow, localCredential credentialRow, now time.Time) ([]gatewayTopologyMember, error) {
	rootID := local.ID
	if local.HubGatewayID != "" {
		rootID = local.HubGatewayID
	}
	rows, err := tx.Raw(`SELECT id, runtime_id, site_id, COALESCE(hub_gateway_id, ''), public_endpoint_host, public_endpoint_port,
		overlay_cidr::text, routing_mode, advertised_cidrs, mtu, persistent_keepalive_seconds, dns_servers
		FROM network_access_gateways WHERE tenant_id = 'default' AND workspace_id = 'default' AND administrative_status = 'active'
		AND (id = ? OR hub_gateway_id = ?) ORDER BY id LIMIT 66`, rootID, rootID).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	gateways := []gatewayRuntimeRow{}
	for rows.Next() {
		gateway, err := scanGatewayRuntime(rows)
		if err != nil {
			return nil, err
		}
		gateways = append(gateways, gateway)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if len(gateways) > 65 {
		return nil, apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "gateway_topology_limit", "The gateway topology limit was reached.", "网关拓扑数量已达上限。")
	}
	members := []gatewayTopologyMember{}
	for _, gateway := range gateways {
		credential := localCredential
		if gateway.ID != local.ID {
			credential, err = activeRuntimeCredential(tx, gateway.RuntimeID, now)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
		}
		if credential.WireGuardPublicKey == "" || !credentialHasCapability(credential, "wireguard") {
			continue
		}
		members = append(members, gatewayTopologyMember{gateway: gateway, credential: credential})
	}
	return members, nil
}

func compileGatewaySitePlan(local gatewayRuntimeRow, members []gatewayTopologyMember, protected []string, validUntil time.Time) (gatewaySitePlan, error) {
	plan := gatewaySitePlan{peers: []networkprotocol.WireGuardPeer{}, routes: []string{}, firewallRules: []networkprotocol.WireGuardFirewallRule{}, validUntil: validUntil}
	if local.RoutingMode != "" && local.RoutingMode != domainnetworkaccess.GatewayRoutingRouted {
		return plan, nil
	}
	direct := []gatewayTopologyMember{}
	if local.HubGatewayID == "" {
		for _, member := range members {
			if member.gateway.HubGatewayID == local.ID {
				direct = append(direct, member)
			}
		}
	} else {
		for _, member := range members {
			if member.gateway.ID == local.HubGatewayID {
				direct = append(direct, member)
				break
			}
		}
	}
	for _, peer := range direct {
		allowed := []string{}
		if local.HubGatewayID == "" {
			allowed = gatewaySitePrefixes(peer.gateway)
		} else {
			for _, member := range members {
				if member.gateway.ID != local.ID {
					allowed = append(allowed, gatewaySitePrefixes(member.gateway)...)
				}
			}
		}
		allowed = sortedUniqueStrings(allowed)
		plan.peers = append(plan.peers, networkprotocol.WireGuardPeer{
			RuntimeID: peer.gateway.RuntimeID, PublicKey: peer.credential.WireGuardPublicKey,
			EndpointHost: peer.gateway.PublicEndpointHost, EndpointPort: peer.gateway.PublicEndpointPort,
			AllowedIPs: allowed, PersistentKeepaliveSeconds: peer.gateway.PersistentKeepaliveSeconds,
		})
		plan.routes = append(plan.routes, allowed...)
		plan.validUntil = minTime(plan.validUntil, peer.credential.ExpiresAt)
		plan.firewallRules = append(plan.firewallRules, siteLinkFirewallRules(local, peer.gateway, members, protected, plan.validUntil)...)
	}
	plan.routes = sortedUniqueStrings(plan.routes)
	return plan, nil
}

func gatewaySitePrefixes(gateway gatewayRuntimeRow) []string {
	return append([]string{gateway.OverlayCIDR}, gateway.AdvertisedCIDRs...)
}

func siteLinkFirewallRules(local, directPeer gatewayRuntimeRow, members []gatewayTopologyMember, protected []string, expiresAt time.Time) []networkprotocol.WireGuardFirewallRule {
	remote := []gatewayRuntimeRow{}
	if local.HubGatewayID == "" {
		remote = append(remote, directPeer)
	} else {
		for _, member := range members {
			if member.gateway.ID != local.ID {
				remote = append(remote, member.gateway)
			}
		}
	}
	denies, allows := []networkprotocol.WireGuardFirewallRule{}, []networkprotocol.WireGuardFirewallRule{}
	seen := map[string]struct{}{}
	appendRule := func(target *[]networkprotocol.WireGuardFirewallRule, effect, direction, source, destination string) {
		key := effect + "\n" + direction + "\n" + source + "\n" + destination
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		expiry := expiresAt
		*target = append(*target, networkprotocol.WireGuardFirewallRule{
			ID: siteFirewallRuleID(effect, direction, directPeer.RuntimeID, source, destination), Effect: effect,
			SiteLinkRuntimeID: directPeer.RuntimeID, Direction: direction, SourceCIDR: source,
			DestinationCIDR: destination, Protocol: "any", ExpiresAt: &expiry,
		})
	}
	for _, sourceGateway := range remote {
		destinations := local.AdvertisedCIDRs
		if local.HubGatewayID == "" {
			destinations = []string{}
			for _, member := range members {
				if member.gateway.ID != sourceGateway.ID {
					destinations = append(destinations, member.gateway.AdvertisedCIDRs...)
				}
			}
		}
		for _, source := range gatewaySitePrefixes(sourceGateway) {
			for _, destination := range destinations {
				if source != sourceGateway.OverlayCIDR {
					for _, denied := range protectedWithin(protected, destination) {
						appendRule(&denies, "deny", "from_wireguard", source, denied)
					}
				}
				appendRule(&allows, "allow", "from_wireguard", source, destination)
			}
		}
	}
	for _, source := range local.AdvertisedCIDRs {
		for _, destinationGateway := range remote {
			for _, destination := range destinationGateway.AdvertisedCIDRs {
				for _, denied := range protectedWithin(protected, destination) {
					appendRule(&denies, "deny", "to_wireguard", source, denied)
				}
				appendRule(&allows, "allow", "to_wireguard", source, destination)
			}
		}
	}
	return append(denies, allows...)
}

func protectedWithin(protected []string, destination string) []string {
	target, err := netip.ParsePrefix(destination)
	if err != nil {
		return nil
	}
	result := []string{}
	for _, raw := range protected {
		prefix, err := netip.ParsePrefix(raw)
		if err == nil && prefix.Overlaps(target) {
			result = append(result, raw)
		}
	}
	return result
}

func siteFirewallRuleID(effect, direction, runtimeID, source, destination string) string {
	digest := sha256.Sum256([]byte(effect + "\n" + direction + "\n" + runtimeID + "\n" + source + "\n" + destination))
	return fmt.Sprintf("wg-site-%s-%x", effect, digest[:12])
}

func sortedUniqueStrings(values []string) []string {
	values = slices.Clone(values)
	slices.Sort(values)
	return slices.Compact(values)
}

func gatewayVPNDesired(tx *gorm.DB, gateway gatewayRuntimeRow, credential credentialRow, snapshot domainnetworkruntime.PolicySnapshot, validUntil, now time.Time) (networkprotocol.ConfigurationDesired, error) {
	peers, err := activeVPNPeers(tx, gateway.ID, now)
	if err != nil {
		return networkprotocol.ConfigurationDesired{}, err
	}
	if len(peers) > 1024 {
		return networkprotocol.ConfigurationDesired{}, apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "gateway_peer_limit", "The gateway peer limit was reached.", "网关 peer 数量已达上限。")
	}
	topology, err := gatewayTopologyMembers(tx, gateway, credential, now)
	if err != nil {
		return networkprotocol.ConfigurationDesired{}, err
	}
	protected, err := topologyProtectedDestinations(tx, topology, snapshot.ProtectedResourceIDs)
	if err != nil {
		return networkprotocol.ConfigurationDesired{}, err
	}
	sitePlan, err := compileGatewaySitePlan(gateway, topology, protected, minTime(validUntil, credential.ExpiresAt))
	if err != nil {
		return networkprotocol.ConfigurationDesired{}, err
	}
	desired := networkprotocol.ConfigurationDesired{PolicyVersion: snapshot.PolicyVersion, ValidUntil: sitePlan.validUntil, AccessProfile: domainnetworkaccess.ProfileFull, ProtectedResourceIDs: slices.Clone(snapshot.ProtectedResourceIDs), NetworkLeases: []networkprotocol.NetworkLease{}, ResourceLeases: []networkprotocol.ResourceLease{}}
	wireGuard := &networkprotocol.WireGuardConfiguration{Role: "gateway", InterfaceName: "soha0", PublicKey: credential.WireGuardPublicKey,
		Addresses: []string{netip.PrefixFrom(netip.MustParsePrefix(gateway.OverlayCIDR).Addr().Next(), 32).String()}, ListenPort: gateway.PublicEndpointPort,
		MTU: gateway.MTU, RoutingMode: gateway.RoutingMode, FirewallDefault: "deny", Peers: slices.Clone(sitePlan.peers), Routes: slices.Clone(sitePlan.routes), DNSServers: slices.Clone(gateway.DNSServers), FirewallRules: []networkprotocol.WireGuardFirewallRule{}}
	for index := range peers {
		peer := &peers[index]
		peerNetworkLeases, peerResourceLeases, err := activeSessionLeases(tx, peer.SessionID, now)
		if err != nil {
			return networkprotocol.ConfigurationDesired{}, err
		}
		if len(peerNetworkLeases) == 0 && len(peerResourceLeases) == 0 {
			continue
		}
		space, _, resourceTargets, err := compileLeaseAccess(tx, peerNetworkLeases, peerResourceLeases)
		if err != nil {
			return networkprotocol.ConfigurationDesired{}, err
		}
		denied, err := protectedDestinations(tx, space.ID, snapshot.ProtectedResourceIDs, space.CIDRs)
		if err != nil {
			return networkprotocol.ConfigurationDesired{}, err
		}
		wireGuard.Peers = append(wireGuard.Peers, networkprotocol.WireGuardPeer{RuntimeID: peer.RuntimeID, DeviceID: peer.DeviceID, PublicKey: peer.PublicKey, AllowedIPs: []string{peer.Address}, PersistentKeepaliveSeconds: 0})
		wireGuard.Routes = append(wireGuard.Routes, peer.Address)
		desired.NetworkLeases = append(desired.NetworkLeases, peerNetworkLeases...)
		desired.ResourceLeases = append(desired.ResourceLeases, peerResourceLeases...)
		for _, lease := range peerNetworkLeases {
			desired.ValidUntil = minTime(desired.ValidUntil, lease.ExpiresAt)
		}
		for _, lease := range peerResourceLeases {
			desired.ValidUntil = minTime(desired.ValidUntil, lease.ExpiresAt)
		}
		wireGuard.FirewallRules = append(wireGuard.FirewallRules, leaseFirewallRules(peer.Address, peerNetworkLeases, resourceTargets, denied)...)
	}
	wireGuard.FirewallRules = append(wireGuard.FirewallRules, sitePlan.firewallRules...)
	wireGuard.Routes = sortedUniqueStrings(wireGuard.Routes)
	if len(wireGuard.Peers) > 1024 || len(wireGuard.Routes) > 256 || len(desired.NetworkLeases) > 256 || len(desired.ResourceLeases) > 512 || len(wireGuard.FirewallRules) > 4096 {
		return networkprotocol.ConfigurationDesired{}, apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "gateway_configuration_limit", "The gateway configuration limit was reached.", "网关配置数量已达上限。")
	}
	desired.WireGuard = wireGuard
	return desired, nil
}

func topologyProtectedDestinations(tx *gorm.DB, members []gatewayTopologyMember, protectedIDs []string) ([]string, error) {
	if len(protectedIDs) == 0 {
		return []string{}, nil
	}
	advertised := []string{}
	for _, member := range members {
		advertised = append(advertised, member.gateway.AdvertisedCIDRs...)
	}
	rows, err := tx.Raw(`SELECT kind, target FROM network_access_resources
		WHERE tenant_id = 'default' AND workspace_id = 'default' AND protected = true AND id IN ? ORDER BY id`, protectedIDs).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []string{}
	for rows.Next() {
		var kind, target string
		if err := rows.Scan(&kind, &target); err != nil {
			return nil, err
		}
		switch kind {
		case "ip":
			target += "/32"
			if !prefixOverlapsAny(target, advertised) {
				continue
			}
			result = append(result, target)
		case "cidr":
			if target == "0.0.0.0/0" {
				result = append(result, advertised...)
			} else if prefixOverlapsAny(target, advertised) {
				result = append(result, target)
			}
		case "fqdn":
			result = append(result, advertised...)
		}
	}
	return sortedUniqueStrings(result), rows.Err()
}

func prefixOverlapsAny(raw string, candidates []string) bool {
	prefix, err := netip.ParsePrefix(raw)
	if err != nil {
		return false
	}
	for _, candidate := range candidates {
		other, err := netip.ParsePrefix(candidate)
		if err == nil && prefix.Overlaps(other) {
			return true
		}
	}
	return false
}

func routesCoverAll(routes, destinations []string) bool {
	for _, destination := range destinations {
		target, err := netip.ParsePrefix(destination)
		if err != nil {
			return false
		}
		covered := false
		for _, raw := range routes {
			route, err := netip.ParsePrefix(raw)
			if err == nil && route.Bits() <= target.Bits() && route.Contains(target.Addr()) {
				covered = true
				break
			}
		}
		if !covered {
			return false
		}
	}
	return len(destinations) != 0
}

func activeVPNPeers(tx *gorm.DB, gatewayID string, now time.Time) ([]vpnPeerState, error) {
	rows, err := tx.Raw(`SELECT p.session_id, p.runtime_id, p.device_id, p.public_key, p.overlay_address::text, s.access_profile
		FROM network_wireguard_peers p JOIN network_runtime_sessions s ON s.id = p.session_id
		WHERE p.tenant_id = 'default' AND p.workspace_id = 'default' AND p.gateway_id = ? AND p.status = 'active' AND p.expires_at > ?
		AND s.status IN ('pending', 'active', 'restricted', 'quarantine') AND s.valid_until > ?
		ORDER BY p.runtime_id, p.id LIMIT 1025`, gatewayID, now, now).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	peers := []vpnPeerState{}
	for rows.Next() {
		var peer vpnPeerState
		if err := rows.Scan(&peer.SessionID, &peer.RuntimeID, &peer.DeviceID, &peer.PublicKey, &peer.Address, &peer.AccessProfile); err != nil {
			return nil, err
		}
		peers = append(peers, peer)
	}
	return peers, rows.Err()
}

func compileLeaseAccess(tx *gorm.DB, networkLeases []networkprotocol.NetworkLease, resourceLeases []networkprotocol.ResourceLease) (domainnetworkaccess.Space, []string, []leasedResourceTarget, error) {
	var space domainnetworkaccess.Space
	routes := []string{}
	resourceTargets := []leasedResourceTarget{}
	for _, lease := range networkLeases {
		if lease.NetworkSpaceID == "" || (space.ID != "" && space.ID != lease.NetworkSpaceID) {
			return space, nil, nil, staleVPNResourceScope()
		}
		if space.ID == "" {
			var err error
			space, err = runtimeSpace(tx, lease.NetworkSpaceID)
			if err != nil {
				return space, nil, nil, err
			}
		}
		for _, route := range lease.CIDRs {
			if !routeWithinSpace(route, space) {
				return space, nil, nil, staleVPNResourceScope()
			}
			routes = append(routes, route)
		}
	}
	for _, lease := range resourceLeases {
		leaseSpace, targets, err := runtimeResourceTargets(tx, lease.ResourceIDs, space.ID)
		if err != nil {
			return space, nil, nil, err
		}
		if space.ID == "" {
			space = leaseSpace
		}
		for _, target := range targets {
			routes = append(routes, target.DestinationCIDR)
			target.LeaseID, target.ExpiresAt = lease.ID, lease.ExpiresAt
			resourceTargets = append(resourceTargets, target)
		}
	}
	if space.ID == "" || len(routes) == 0 {
		return space, nil, nil, staleVPNResourceScope()
	}
	routes = collapseRoutes(routes)
	return space, routes, resourceTargets, nil
}

func collapseRoutes(routes []string) []string {
	slices.SortFunc(routes, func(left, right string) int {
		leftPrefix, rightPrefix := netip.MustParsePrefix(left), netip.MustParsePrefix(right)
		if leftPrefix.Bits() != rightPrefix.Bits() {
			return leftPrefix.Bits() - rightPrefix.Bits()
		}
		return strings.Compare(left, right)
	})
	result := make([]string, 0, len(routes))
	for _, route := range routes {
		prefix := netip.MustParsePrefix(route)
		covered := false
		for _, existing := range result {
			cover := netip.MustParsePrefix(existing)
			if cover.Bits() <= prefix.Bits() && cover.Contains(prefix.Addr()) {
				covered = true
				break
			}
		}
		if !covered {
			result = append(result, route)
		}
	}
	slices.Sort(result)
	return result
}

func dnsWithinRoutes(servers, routes []string) []string {
	result := []string{}
	for _, raw := range servers {
		address, err := netip.ParseAddr(raw)
		if err != nil || !address.Is4() {
			continue
		}
		for _, route := range routes {
			if netip.MustParsePrefix(route).Contains(address) {
				result = append(result, raw)
				break
			}
		}
	}
	return result
}

func runtimeSpace(tx *gorm.DB, spaceID string) (domainnetworkaccess.Space, error) {
	var space domainnetworkaccess.Space
	var cidrs []byte
	err := tx.Raw(`SELECT id, site_id, cidrs, status FROM network_access_spaces
		WHERE tenant_id = 'default' AND workspace_id = 'default' AND id = ? AND status = 'active'`, spaceID).Row().Scan(&space.ID, &space.SiteID, &cidrs, &space.Status)
	if err != nil {
		return space, staleVPNResourceScope()
	}
	if err := json.Unmarshal(cidrs, &space.CIDRs); err != nil {
		return space, fmt.Errorf("decode network space CIDRs: %w", err)
	}
	return space, nil
}

func runtimeResourceTargets(tx *gorm.DB, resourceIDs []string, expectedSpaceID string) (domainnetworkaccess.Space, []leasedResourceTarget, error) {
	ids := slices.Clone(resourceIDs)
	slices.Sort(ids)
	if len(ids) == 0 || len(slices.Compact(ids)) != len(ids) {
		return domainnetworkaccess.Space{}, nil, staleVPNResourceScope()
	}
	rows, err := tx.Raw(`SELECT r.id, r.space_id, r.kind, r.target, r.protocol, r.ports, s.id, s.site_id, s.cidrs, s.status
		FROM network_access_resources r JOIN network_access_spaces s ON s.id = r.space_id
		WHERE r.tenant_id = 'default' AND r.workspace_id = 'default' AND s.tenant_id = 'default' AND s.workspace_id = 'default'
		AND s.status = 'active' AND r.id IN ? ORDER BY r.id`, ids).Rows()
	if err != nil {
		return domainnetworkaccess.Space{}, nil, err
	}
	defer func() { _ = rows.Close() }()
	var space domainnetworkaccess.Space
	targets := []leasedResourceTarget{}
	for rows.Next() {
		var resource domainnetworkaccess.Resource
		var ports, cidrs []byte
		var rowSpace domainnetworkaccess.Space
		if err := rows.Scan(&resource.ID, &resource.SpaceID, &resource.Kind, &resource.Target, &resource.Protocol, &ports,
			&rowSpace.ID, &rowSpace.SiteID, &cidrs, &rowSpace.Status); err != nil {
			return space, nil, err
		}
		if err := json.Unmarshal(ports, &resource.Ports); err != nil {
			return space, nil, fmt.Errorf("decode network resource ports: %w", err)
		}
		if err := json.Unmarshal(cidrs, &rowSpace.CIDRs); err != nil {
			return space, nil, fmt.Errorf("decode network space CIDRs: %w", err)
		}
		if (expectedSpaceID != "" && rowSpace.ID != expectedSpaceID) || (space.ID != "" && rowSpace.ID != space.ID) {
			return space, nil, staleVPNResourceScope()
		}
		space = rowSpace
		destination, ok := domainnetworkaccess.WireGuardResourceTarget(resource, rowSpace)
		if !ok {
			return space, nil, staleVPNResourceScope()
		}
		targets = append(targets, leasedResourceTarget{ResourceID: resource.ID, DestinationCIDR: destination, Protocol: resource.Protocol, Ports: slices.Clone(resource.Ports)})
	}
	if err := rows.Err(); err != nil {
		return space, nil, err
	}
	if len(targets) != len(ids) {
		return space, nil, staleVPNResourceScope()
	}
	return space, targets, nil
}

func routeWithinSpace(raw string, space domainnetworkaccess.Space) bool {
	route, err := netip.ParsePrefix(raw)
	if err != nil || !route.Addr().Is4() || route != route.Masked() || route.Bits() == 0 {
		return false
	}
	for _, spaceCIDR := range space.CIDRs {
		prefix, err := netip.ParsePrefix(spaceCIDR)
		if err == nil && prefix.Addr().Is4() && prefix == prefix.Masked() && prefix.Bits() <= route.Bits() && prefix.Contains(route.Addr()) {
			return true
		}
	}
	return false
}

func staleVPNResourceScope() error {
	return apperrors.NewBusiness(apperrors.ErrConflict, "stale_vpn_resource_scope", "The VPN resource scope changed or cannot be enforced.", "VPN 资源范围已变化或无法执行。")
}

func protectedDestinations(tx *gorm.DB, spaceID string, protectedIDs, spaceCIDRs []string) ([]string, error) {
	if len(protectedIDs) == 0 {
		return []string{}, nil
	}
	rows, err := tx.Raw(`SELECT kind, target FROM network_access_resources WHERE tenant_id = 'default' AND workspace_id = 'default' AND space_id = ? AND protected = true AND id IN ? ORDER BY id`, spaceID, protectedIDs).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	unique := map[string]struct{}{}
	for rows.Next() {
		var kind, target string
		if err := rows.Scan(&kind, &target); err != nil {
			return nil, err
		}
		switch kind {
		case "ip":
			unique[target+"/32"] = struct{}{}
		case "cidr":
			if target == "0.0.0.0/0" {
				for _, cidr := range spaceCIDRs {
					unique[cidr] = struct{}{}
				}
			} else {
				unique[target] = struct{}{}
			}
		case "fqdn":
			// FQDN has no stable L3 target in Phase 4; deny its entire containing space until the Phase 5 mapper exists.
			for _, cidr := range spaceCIDRs {
				unique[cidr] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(unique))
	for cidr := range unique {
		result = append(result, cidr)
	}
	slices.Sort(result)
	return result, rows.Err()
}

func leaseFirewallRules(source string, networkLeases []networkprotocol.NetworkLease, resourceTargets []leasedResourceTarget, denied []string) []networkprotocol.WireGuardFirewallRule {
	rules := make([]networkprotocol.WireGuardFirewallRule, 0, len(resourceTargets)+len(denied)+len(networkLeases))
	for _, target := range resourceTargets {
		expiresAt := target.ExpiresAt
		rules = append(rules, networkprotocol.WireGuardFirewallRule{
			ID: firewallRuleID("allow", source, target.DestinationCIDR, target.LeaseID), Effect: "allow", LeaseID: target.LeaseID,
			SourceCIDR: source, DestinationCIDR: target.DestinationCIDR, Protocol: target.Protocol, Ports: slices.Clone(target.Ports), ExpiresAt: &expiresAt,
		})
	}
	for _, destination := range denied {
		rules = append(rules, networkprotocol.WireGuardFirewallRule{ID: firewallRuleID("deny", source, destination, ""), Effect: "deny", SourceCIDR: source, DestinationCIDR: destination, Protocol: "any"})
	}
	for _, lease := range networkLeases {
		for _, destination := range lease.CIDRs {
			expiresAt := lease.ExpiresAt
			rules = append(rules, networkprotocol.WireGuardFirewallRule{ID: firewallRuleID("allow", source, destination, lease.ID), Effect: "allow", LeaseID: lease.ID, SourceCIDR: source, DestinationCIDR: destination, Protocol: "any", ExpiresAt: &expiresAt})
		}
	}
	return rules
}

func firewallRuleID(effect, source, destination, leaseID string) string {
	digest := sha256.Sum256([]byte(effect + "\n" + source + "\n" + destination + "\n" + leaseID))
	return fmt.Sprintf("wg-%s-%x", effect, digest[:12])
}
