package networkruntime

import (
	"context"
	"errors"
	"net/netip"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	runtime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
	networkrepo "github.com/opensoha/soha/internal/repository/networkaccess"
	"gorm.io/gorm"
)

func (r *Repository) VPNEndpointCredential(ctx context.Context, deviceID string, now time.Time) (runtime.Credential, error) {
	var row credentialRow
	err := r.db.WithContext(ctx).Where("tenant_id = 'default' AND workspace_id = 'default' AND device_id = ? AND runtime_kind = 'endpoint' AND status = 'active' AND not_before <= ? AND expires_at > ?", deviceID, now, now).Order("generation DESC").First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return runtime.Credential{}, apperrors.ErrNotFound
	}
	return row.domain(), err
}

func (r *Repository) VPNGatewayCandidates(ctx context.Context, profile domain.VPNProfileConfig, now time.Time) ([]runtime.VPNGatewayCandidate, error) {
	items := make([]runtime.VPNGatewayCandidate, 0, len(profile.GatewayIDs))
	for _, id := range profile.GatewayIDs {
		gateway, err := networkrepo.New(r.db).GetGateway(ctx, id)
		if errors.Is(err, apperrors.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		item := runtime.VPNGatewayCandidate{Gateway: gateway, ReasonCode: "gateway_unavailable"}
		_, credential, err := r.ActiveVPNGateway(ctx, profile.SiteID, id, now)
		if errors.Is(err, apperrors.ErrNotFound) {
			items = append(items, item)
			continue
		}
		if err != nil {
			return nil, err
		}
		item.Credential = credential
		item.ExecutionReady, err = vpnGatewayExecutionReady(r.db.WithContext(ctx), gateway.RuntimeID, credential.WireGuardPublicKey, now)
		if err != nil {
			return nil, err
		}
		item.Reachable, err = r.vpnCandidateReachesSpace(ctx, gateway, credential, profile.NetworkSpaceID, now)
		if err != nil {
			return nil, err
		}
		if !item.Reachable {
			item.ReasonCode = "target_unreachable"
		}
		if err := r.db.WithContext(ctx).Raw(`SELECT count(*) FROM network_wireguard_peers WHERE tenant_id = 'default' AND workspace_id = 'default' AND gateway_id = ? AND status = 'active' AND expires_at > ?`, id, now).Row().Scan(&item.ActiveSessions); err != nil {
			return nil, err
		}
		if prefix, err := netip.ParsePrefix(gateway.OverlayCIDR); err == nil && prefix.Addr().Is4() && prefix.Bits() >= 16 && prefix.Bits() <= 30 {
			item.OverlayCapacity = (1 << (32 - prefix.Bits())) - 3
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *Repository) vpnCandidateReachesSpace(ctx context.Context, gateway domain.Gateway, credential runtime.Credential, spaceID string, now time.Time) (bool, error) {
	space, err := networkrepo.New(r.db).GetSpace(ctx, spaceID)
	if err != nil {
		return false, err
	}
	if space.Status != domain.StatusActive {
		return false, nil
	}
	if gateway.SiteID == space.SiteID {
		return true, nil
	}
	db := r.db.WithContext(ctx)
	local, err := gatewayRuntimeByID(db, gateway.ID)
	if err != nil {
		return false, err
	}
	var row credentialRow
	if err := db.Where("id = ? AND tenant_id = 'default' AND workspace_id = 'default'", credential.ID).First(&row).Error; err != nil {
		return false, err
	}
	members, err := gatewayTopologyMembers(db, local, row, now)
	if err != nil {
		return false, err
	}
	plan, err := compileGatewaySitePlan(local, members, nil, credential.ExpiresAt)
	if err != nil {
		return false, nil
	}
	return routesCoverAll(plan.routes, space.CIDRs) && plan.validUntil.After(now), nil
}

func (r *Repository) VPNGatewayID(ctx context.Context, runtimeID string) (string, error) {
	var id string
	err := r.db.WithContext(ctx).Raw(`SELECT id FROM network_access_gateways WHERE tenant_id = 'default' AND workspace_id = 'default' AND runtime_id = ? AND administrative_status = 'active'`, runtimeID).Row().Scan(&id)
	return id, err
}

func vpnGatewayExecutionReady(tx *gorm.DB, runtimeID, publicKey string, now time.Time) (bool, error) {
	var ready bool
	err := tx.Raw(`SELECT EXISTS(SELECT 1 FROM network_runtime_configurations WHERE runtime_id=? AND tenant_id='default' AND workspace_id='default' AND apply_status='applied' AND valid_until>? AND desired_payload->'wireguard'->>'publicKey' IS NOT NULL AND (?='' OR desired_payload->'wireguard'->>'publicKey'=?))
 AND COALESCE((SELECT apply_status NOT IN ('rejected','rolled-back') FROM network_runtime_configurations WHERE runtime_id=? AND tenant_id='default' AND workspace_id='default' ORDER BY configuration_version DESC LIMIT 1),false)`, runtimeID, now, publicKey, publicKey, runtimeID).Row().Scan(&ready)
	return ready, err
}
