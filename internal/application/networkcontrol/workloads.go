package networkcontrol

import (
	"context"
	"encoding/json"
	"slices"
	"time"

	app "github.com/opensoha/soha/internal/application/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	runtime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type workloadControlStore interface {
	WorkloadContext(context.Context, string, string, time.Time) (domain.WorkloadBinding, domain.Subject, error)
}

func (s *Service) connectWorkload(ctx context.Context, identity networkidentity.Identity, credential runtime.Credential, request networkprotocol.VPNConnectRequest) (networkprotocol.RuntimeMessage, error) {
	store, ok := s.store.(workloadControlStore)
	if !ok {
		return networkprotocol.RuntimeMessage{}, apperrors.ErrServiceUnavailable
	}
	now := s.now().UTC()
	binding, subject, err := store.WorkloadContext(ctx, identity.ID, credential.SubjectID, now)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	if request.WorkloadID != binding.ID || request.Mode != domain.ModeExternalDirectZTNA || request.SiteID != binding.SiteID || request.NetworkSpaceID != binding.NetworkSpaceID || len(request.ResourceIDs) == 0 || len(request.ResourceIDs) > 256 || request.AccessGrantID != "" || request.AccessGrantToken != "" {
		return networkprotocol.RuntimeMessage{}, apperrors.ErrAccessDenied
	}
	device, err := s.store.NetworkDevice(ctx, binding.DeviceID)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	site, err := s.store.NetworkSite(ctx, binding.SiteID)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	space, err := s.store.NetworkSpace(ctx, binding.NetworkSpaceID)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	snapshot, err := s.CurrentSnapshot()
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	profile, err := s.workloadResourceProfile(ctx, request, binding, subject, device, site, space, snapshot)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}

	gateway, gatewayCredential, err := s.store.ActiveVPNGateway(ctx, binding.SiteID, request.GatewayID, now)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	if gateway.SiteID != binding.SiteID || !s.validVPNGateway(gateway, gatewayCredential, now) {
		return networkprotocol.RuntimeMessage{}, apperrors.ErrAccessDenied
	}
	connection := runtime.VPNConnection{WorkloadID: binding.ID, RequestID: request.RequestID, RequestHash: vpnRequestHash(identity.ID, request), RuntimeID: identity.ID, CredentialID: credential.ID, SubjectID: subject.UserID, DeviceID: binding.DeviceID, SiteID: binding.SiteID, NetworkSpaceID: binding.NetworkSpaceID, ResourceIDs: slices.Clone(request.ResourceIDs), Mode: request.Mode, Decision: domain.DecisionAllow, AccessProfile: profile, PolicyVersion: snapshot.PolicyVersion, PostureVersion: device.PostureVersion, ReasonCode: "workload_policy_allowed", SessionID: vpnSessionID(identity.ID, request.RequestID), GatewayID: gateway.ID, GatewayRuntimeID: gateway.RuntimeID, GatewayCredentialID: gatewayCredential.ID, EndpointPublicKey: credential.WireGuardPublicKey, ValidUntil: minTime(now.Add(s.options.LeaseTTL), minTime(binding.ExpiresAt, minTime(credential.ExpiresAt, gatewayCredential.ExpiresAt))), CreatedAt: now}
	result, err := s.store.SaveVPNConnection(ctx, connection, snapshot)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	return s.outbound(identity, networkprotocol.MessageVPNConnectResult, result, result.ValidUntil)
}

func (s *Service) workloadResourceProfile(ctx context.Context, request networkprotocol.VPNConnectRequest, binding domain.WorkloadBinding, subject domain.Subject, device domain.Device, site domain.Site, space domain.Space, snapshot runtime.PolicySnapshot) (string, error) {
	var policies []domain.Policy
	if err := json.Unmarshal(snapshot.Policies, &policies); err != nil {
		return "", err
	}
	policySnapshot := domain.PolicySnapshot{PolicyVersion: snapshot.PolicyVersion, Policies: policies, ProtectedResourceIDs: snapshot.ProtectedResourceIDs}
	profile := ""
	for _, id := range request.ResourceIDs {
		if !slices.Contains(binding.ResourceIDs, id) {
			return "", apperrors.ErrAccessDenied
		}
		resource, err := s.store.NetworkResource(ctx, id)
		if err != nil {
			return "", err
		}
		selected := app.EvaluateWorkloadPolicy(app.PreviewInput{SubjectUserID: subject.UserID, DeviceID: device.ID, SiteID: site.ID, ResourceID: id, Mode: request.Mode}, subject, device, site, space, resource, policySnapshot)
		if selected == "" || profile != "" && selected != profile {
			return "", apperrors.ErrAccessDenied
		}
		profile = selected
	}
	return profile, nil
}
