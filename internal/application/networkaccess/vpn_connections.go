package networkaccess

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/netip"
	"slices"
	"time"

	"github.com/google/uuid"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	runtime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *VPNService) ownedDevice(ctx context.Context, p identity.Principal, deviceID string) (domain.Subject, domain.Device, error) {
	if p.UserID == "" {
		return domain.Subject{}, domain.Device{}, apperrors.ErrUnauthorized
	}
	if !runtimeIdentifierPattern.MatchString(deviceID) {
		return domain.Subject{}, domain.Device{}, invalid("deviceId is invalid")
	}
	subject, err := s.base.store.GetSubject(ctx, p.UserID)
	if err != nil {
		return subject, domain.Device{}, err
	}
	device, err := s.base.store.GetDevice(ctx, deviceID)
	if err != nil {
		return subject, device, err
	}
	if subject.Status != domain.StatusActive || device.OwnerUserID != p.UserID {
		return subject, device, apperrors.ErrAccessDenied
	}
	return subject, device, nil
}

func (s *VPNService) ListConnectionOptions(ctx context.Context, p identity.Principal, deviceID string) ([]domain.VPNConnectionOption, error) {
	subject, device, err := s.ownedDevice(ctx, p, deviceID)
	if err != nil {
		return nil, err
	}
	items := make([]domain.VPNConnectionOption, 0)
	filter := domain.VPNDocumentFilter{Limit: 200}
	for {
		profiles, err := s.profiles.ListVPNProfiles(ctx, filter)
		if err != nil {
			return nil, err
		}
		for _, profile := range profiles {
			if profile.PublishedConfiguration == nil || !profile.PublishedConfiguration.Enabled || !profile.PublishedConfiguration.Assignments.Matches(subject, device) {
				continue
			}
			item, err := s.connectionOption(ctx, profile, subject, device)
			if errors.Is(err, apperrors.ErrAccessDenied) || errors.Is(err, apperrors.ErrNotFound) {
				continue
			}
			if err != nil {
				return nil, err
			}
			items = append(items, item)
			if len(items) == 100 {
				return items, nil
			}
		}
		if len(profiles) < 200 {
			return items, nil
		}
		filter.AfterID = profiles[len(profiles)-1].ID
	}
}

func (s *VPNService) connectionOption(ctx context.Context, profile domain.VPNProfile, subject domain.Subject, device domain.Device) (domain.VPNConnectionOption, error) {
	config := *profile.PublishedConfiguration
	if _, err := s.authorizeConnection(ctx, subject, device, config); err != nil {
		return domain.VPNConnectionOption{}, err
	}
	policy, err := s.policies.GetVPNSelectionPolicy(ctx, config.SelectionPolicyID)
	if err != nil {
		return domain.VPNConnectionOption{}, err
	}
	item := domain.VPNConnectionOption{ProfileID: profile.ID, ProfileRevision: profile.PublishedRevision, Name: config.Name, SiteID: config.SiteID, NetworkSpaceID: config.NetworkSpaceID, Mode: config.Mode, AllowManualSelection: config.AllowManualSelection, SelectionPolicyID: policy.ID, SelectionPolicyRevision: policy.PublishedRevision, Candidates: []domain.VPNCandidate{}}
	if policy.PublishedConfiguration == nil || policy.PublishedRevision < 1 {
		return item, apperrors.ErrNotFound
	}
	item.SelectionStrategy = policy.PublishedConfiguration.Strategy
	facts, err := s.connections.VPNGatewayCandidates(ctx, config, s.now().UTC())
	if err != nil {
		return item, err
	}
	item.Candidates = VPNGatewayViews(facts, s.now().UTC(), 2*time.Minute)
	item.Available = slices.ContainsFunc(item.Candidates, func(candidate domain.VPNCandidate) bool { return candidate.Available })
	if !item.Available {
		item.ReasonCode = "no_eligible_gateway"
	}
	credential, err := s.connections.VPNEndpointCredential(ctx, device.ID, s.now().UTC())
	if errors.Is(err, apperrors.ErrNotFound) {
		item.Available = false
		item.ReasonCode = "endpoint_not_enrolled"
		return item, nil
	}
	if err != nil {
		return item, err
	}
	if credential.SubjectID != subject.UserID || !slices.Contains(credential.Capabilities, networkprotocol.CapabilityManagedVPN) {
		item.Available = false
		item.ReasonCode = "managed_vpn_runtime_required"
	}
	return item, nil
}

func (s *VPNService) authorizeConnection(ctx context.Context, subject domain.Subject, device domain.Device, config domain.VPNProfileConfig) (domain.PolicySnapshot, error) {
	if !config.Enabled || !config.Assignments.Matches(subject, device) {
		return domain.PolicySnapshot{}, apperrors.ErrAccessDenied
	}
	if config.Mode != domain.ModeExternalVPN {
		return authorizeNetworkResources(ctx, s.base.store, subject.UserID, AccessGrantInput{DeviceID: device.ID, SiteID: config.SiteID, NetworkSpaceID: config.NetworkSpaceID, Mode: config.Mode, ResourceIDs: config.ResourceIDs})
	}
	site, err := s.base.store.GetSite(ctx, config.SiteID)
	if err != nil {
		return domain.PolicySnapshot{}, err
	}
	space, err := s.base.store.GetSpace(ctx, config.NetworkSpaceID)
	if err != nil {
		return domain.PolicySnapshot{}, err
	}
	if space.SiteID != site.ID || space.Status != domain.StatusActive || len(space.CIDRs) == 0 {
		return domain.PolicySnapshot{}, apperrors.ErrAccessDenied
	}
	for _, raw := range space.CIDRs {
		prefix, err := netip.ParsePrefix(raw)
		if err != nil || !prefix.Addr().Is4() || prefix.Bits() == 0 || prefix != prefix.Masked() {
			return domain.PolicySnapshot{}, apperrors.ErrAccessDenied
		}
	}
	snapshot, err := s.base.store.GetPolicySnapshot(ctx)
	if err != nil {
		return snapshot, err
	}
	preview := EvaluateAdmission(AdmissionInput{SubjectUserID: subject.UserID, DeviceID: device.ID, SiteID: site.ID, Mode: config.Mode}, subject, device, site, snapshot)
	if preview.Decision != domain.DecisionAllow || !preview.NetworkLeaseRequired || preview.ResourceLeaseRequired {
		return snapshot, apperrors.ErrAccessDenied
	}
	return snapshot, nil
}

func (s *VPNService) CreateConnectionIntent(ctx context.Context, p identity.Principal, authSessionID string, input domain.VPNIntentInput) (domain.VPNIntentSecret, error) {
	subject, device, err := s.ownedDevice(ctx, p, input.DeviceID)
	if err != nil {
		return domain.VPNIntentSecret{}, err
	}
	profile, err := s.profiles.GetVPNProfile(ctx, input.ProfileID)
	if err != nil {
		return domain.VPNIntentSecret{}, err
	}
	if profile.PublishedConfiguration == nil {
		return domain.VPNIntentSecret{}, apperrors.ErrAccessDenied
	}
	config := *profile.PublishedConfiguration
	if err := validateVPNIntentSelection(config, input); err != nil {
		return domain.VPNIntentSecret{}, err
	}
	if _, err := s.authorizeConnection(ctx, subject, device, config); err != nil {
		return domain.VPNIntentSecret{}, err
	}
	if config.Mode != domain.ModeExternalVPN {
		if err := s.stepUp.RequireRecentStepUp(ctx, p.UserID, authSessionID); err != nil {
			return domain.VPNIntentSecret{}, err
		}
	}
	policy, err := s.policies.GetVPNSelectionPolicy(ctx, config.SelectionPolicyID)
	if err != nil {
		return domain.VPNIntentSecret{}, err
	}
	if policy.PublishedConfiguration == nil {
		return domain.VPNIntentSecret{}, apperrors.ErrAccessDenied
	}
	now := s.now().UTC()
	credential, err := s.connections.VPNEndpointCredential(ctx, device.ID, now)
	if err != nil {
		return domain.VPNIntentSecret{}, err
	}
	if credential.SubjectID != p.UserID || !slices.Contains(credential.Capabilities, networkprotocol.CapabilityManagedVPN) {
		return domain.VPNIntentSecret{}, apperrors.NewBusiness(apperrors.ErrConflict, "managed_vpn_runtime_required", "Upgrade and enroll the managed VPN runtime first.", "请先升级并登记支持受管 VPN 的终端运行时。")
	}
	token, hash, err := newVPNIntentToken()
	if err != nil {
		return domain.VPNIntentSecret{}, err
	}
	expires := now.Add(2 * time.Minute)
	if credential.ExpiresAt.Before(expires) {
		expires = credential.ExpiresAt
	}
	intent := domain.VPNIntent{ID: uuid.NewString(), RuntimeID: credential.RuntimeID, CredentialID: credential.ID, SubjectID: p.UserID, DeviceID: device.ID, AuthSessionID: authSessionID, ProfileID: profile.ID, ProfileRevision: profile.PublishedRevision, SelectionPolicyID: policy.ID, SelectionPolicyRevision: policy.PublishedRevision, Selection: input.Selection, RequestedGatewayID: input.GatewayID, TokenHash: hash, Status: "issued", CreatedAt: now, ExpiresAt: expires}
	if err := s.connections.CreateVPNIntent(ctx, intent); err != nil {
		return domain.VPNIntentSecret{}, err
	}
	s.base.recordMutation(ctx, p, "network_access.vpn.connection_intent", "NetworkVPNConnectionIntent", intent.ID, config.Name)
	return domain.VPNIntentSecret{IntentID: intent.ID, Token: token, ExpiresAt: expires}, nil
}

func validateVPNIntentSelection(config domain.VPNProfileConfig, input domain.VPNIntentInput) error {
	if input.Selection == domain.VPNSelectionAuto {
		if input.GatewayID != "" {
			return invalid("auto selection cannot include gatewayId")
		}
		return nil
	}
	if input.Selection != domain.VPNSelectionManual {
		return invalid("VPN selection is invalid")
	}
	if !config.AllowManualSelection || !slices.Contains(config.GatewayIDs, input.GatewayID) {
		return apperrors.NewBusiness(apperrors.ErrAccessDenied, "vpn_manual_selection_denied", "The selected VPN entrance is not allowed by this profile.", "此连接方案不允许选择该接入点。")
	}
	return nil
}

func newVPNIntentToken() (string, string, error) {
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", "", err
	}
	token := base64.RawURLEncoding.EncodeToString(random[:])
	digest := sha256.Sum256([]byte(token))
	return token, fmt.Sprintf("sha256:%x", digest), nil
}

func VPNGatewayViews(facts []runtime.VPNGatewayCandidate, now time.Time, freshness time.Duration) []domain.VPNCandidate {
	items := make([]domain.VPNCandidate, 0, len(facts))
	for _, fact := range facts {
		gateway := fact.Gateway
		reason := vpnGatewayUnavailableReason(fact, now, freshness)
		items = append(items, domain.VPNCandidate{GatewayID: gateway.ID, Name: gateway.Name, Region: gateway.Region, ProviderCode: gateway.ProviderCode, ProviderName: gateway.ProviderName, Available: reason == "", ReasonCode: reason, Priority: gateway.SelectionPriority})
	}
	return items
}

func vpnGatewayUnavailableReason(fact runtime.VPNGatewayCandidate, now time.Time, freshness time.Duration) string {
	g, c := fact.Gateway, fact.Credential
	if g.AdministrativeStatus != domain.StatusActive {
		return "gateway_disabled"
	}
	if !g.AcceptNewConnections {
		return "gateway_draining"
	}
	if !fact.Reachable {
		return "target_unreachable"
	}
	if c.ID == "" || c.RuntimeID != g.RuntimeID || !c.ExpiresAt.After(now) || c.NotBefore.After(now) || c.WireGuardPublicKey == "" || c.WireGuardPublicKey != g.WireGuardPublicKey || !slices.Contains(c.Capabilities, "wireguard") {
		return "gateway_credential_unavailable"
	}
	if !fact.ExecutionReady {
		return "gateway_configuration_not_ready"
	}
	if c.LastControlSeenAt == nil || c.LastControlSeenAt.Before(now.Add(-freshness)) {
		return "gateway_offline"
	}
	if fact.OverlayCapacity <= fact.ActiveSessions || (g.MaxSessions > 0 && fact.ActiveSessions >= g.MaxSessions) {
		return "gateway_capacity_exhausted"
	}
	return ""
}
