package networkcontrol

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	appnetworkaccess "github.com/opensoha/soha/internal/application/networkaccess"
	domain "github.com/opensoha/soha/internal/domain/networkaccess"
	runtime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprobe"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type ManagedVPNStore interface {
	GetVPNIntent(context.Context, string) (domain.VPNIntent, error)
	VPNAuthSessionActive(context.Context, string, string, time.Time) error
	GetVPNProfile(context.Context, string) (domain.VPNProfile, error)
	GetVPNSelectionPolicy(context.Context, string) (domain.VPNSelectionPolicy, error)
	VPNGatewayCandidates(context.Context, domain.VPNProfileConfig, time.Time) ([]runtime.VPNGatewayCandidate, error)
	ManagedVPNResult(context.Context, string, string, string, string) (networkprotocol.VPNManagedConnectResult, bool, error)
	SaveVPNDecision(context.Context, domain.VPNDecision) error
	FailManagedVPN(context.Context, runtime.VPNConnection, networkprotocol.VPNConnectResult) error
}

type VPNProbeReader interface {
	VPNProbe(context.Context, string, string, string) (*networkprotocol.VPNProbeBatch, error)
}

type managedVPNContext struct {
	store         ManagedVPNStore
	credential    runtime.Credential
	intent        domain.VPNIntent
	profile       domain.VPNProfileConfig
	policy        domain.VPNSelectionPolicyConfig
	snapshot      runtime.PolicySnapshot
	policyContext vpnPolicyContext
	accessProfile string
}

func (s *Service) decodeManagedVPN(ctx context.Context, identity networkidentity.Identity, kind string, raw []byte) (networkprotocol.VPNManagedConnectRequest, runtime.Credential, error) {
	message, err := s.validateInbound(identity, kind, raw)
	if err != nil {
		return networkprotocol.VPNManagedConnectRequest{}, runtime.Credential{}, err
	}
	if identity.Kind != "endpoint" {
		return networkprotocol.VPNManagedConnectRequest{}, runtime.Credential{}, unauthorizedRuntime()
	}
	credential, err := s.authenticate(ctx, identity)
	if err != nil {
		return networkprotocol.VPNManagedConnectRequest{}, credential, err
	}
	if !slices.Contains(credential.Capabilities, networkprotocol.CapabilityManagedVPN) || !slices.Contains(credential.Capabilities, "wireguard") || credential.WireGuardPublicKey == "" {
		return networkprotocol.VPNManagedConnectRequest{}, credential, managedVPNUnavailable()
	}
	request, err := networkprotocol.DecodePayload[networkprotocol.VPNManagedConnectRequest](message.Payload)
	return request, credential, err
}

func (s *Service) resolveManagedVPN(ctx context.Context, credential runtime.Credential, request networkprotocol.VPNManagedConnectRequest) (managedVPNContext, error) {
	store, ok := s.store.(ManagedVPNStore)
	if !ok {
		return managedVPNContext{}, managedVPNUnavailable()
	}
	intent, err := store.GetVPNIntent(ctx, request.IntentID)
	if err != nil {
		return managedVPNContext{}, err
	}
	state := managedVPNContext{store: store, credential: credential, intent: intent}
	if !managedIntentMatches(intent, credential, request.IntentToken, s.now().UTC()) {
		return state, managedVPNIntentInvalid()
	}
	return s.resolveManagedVPNContext(ctx, state, request)
}

func (s *Service) resolveManagedVPNContext(ctx context.Context, state managedVPNContext, request networkprotocol.VPNManagedConnectRequest) (managedVPNContext, error) {
	intent, credential := state.intent, state.credential
	var err error
	if err := state.store.VPNAuthSessionActive(ctx, intent.AuthSessionID, intent.SubjectID, s.now().UTC()); err != nil {
		return state, err
	}
	profile, err := state.store.GetVPNProfile(ctx, intent.ProfileID)
	if err != nil {
		return state, err
	}
	policy, err := state.store.GetVPNSelectionPolicy(ctx, intent.SelectionPolicyID)
	if err != nil {
		return state, err
	}
	if profile.PublishedConfiguration == nil || policy.PublishedConfiguration == nil || profile.PublishedRevision != intent.ProfileRevision || policy.PublishedRevision != intent.SelectionPolicyRevision || profile.PublishedConfiguration.SelectionPolicyID != policy.ID {
		return state, managedVPNIntentInvalid()
	}
	state.profile, state.policy = *profile.PublishedConfiguration, *policy.PublishedConfiguration
	state.snapshot, err = s.CurrentSnapshot()
	if err != nil {
		return state, err
	}
	legacy := managedScopeRequest(request, state)
	var reason string
	state.policyContext, reason, err = s.loadVPNPolicyContext(ctx, credential, legacy, state.snapshot)
	if err != nil {
		return state, err
	}
	if reason != "" {
		return state, apperrors.NewBusiness(apperrors.ErrAccessDenied, reason, "VPN access is not allowed.", "不允许此 VPN 访问。")
	}
	if !state.profile.Enabled || !state.profile.Assignments.Matches(state.policyContext.principal.subject, state.policyContext.principal.device) {
		return state, managedVPNIntentInvalid()
	}
	state.accessProfile, reason, err = s.evaluateVPNAccess(ctx, credential, legacy, state.policyContext)
	if err != nil {
		return state, err
	}
	if reason != "" {
		return state, apperrors.NewBusiness(apperrors.ErrAccessDenied, reason, "VPN access is no longer allowed by the current policy.", "当前策略已不再允许此 VPN 访问。")
	}
	return state, nil
}

func managedScopeRequest(request networkprotocol.VPNManagedConnectRequest, state managedVPNContext) networkprotocol.VPNConnectRequest {
	return networkprotocol.VPNConnectRequest{RequestID: request.RequestID, SiteID: state.profile.SiteID, NetworkSpaceID: state.profile.NetworkSpaceID, Mode: state.profile.Mode, ResourceIDs: slices.Clone(state.profile.ResourceIDs)}
}

func managedIntentMatches(intent domain.VPNIntent, credential runtime.Credential, token string, now time.Time) bool {
	hash := accessGrantTokenHash(token)
	return intent.Status == "issued" && intent.ExpiresAt.After(now) && intent.RuntimeID == credential.RuntimeID && intent.CredentialID == credential.ID && intent.SubjectID == credential.SubjectID && intent.DeviceID == credential.DeviceID && len(hash) == 71 && subtle.ConstantTimeCompare([]byte(hash), []byte(intent.TokenHash)) == 1
}

func (s *Service) PrepareManagedVPN(ctx context.Context, identity networkidentity.Identity, raw []byte) (networkprotocol.RuntimeMessage, error) {
	request, credential, err := s.decodeManagedVPN(ctx, identity, networkprotocol.MessageVPNManagedPrepareRequest, raw)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	if request.ProbeBatchID != "" {
		return networkprotocol.RuntimeMessage{}, invalidRuntimeMessage()
	}
	state, err := s.resolveManagedVPN(ctx, credential, request)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	facts, err := state.store.VPNGatewayCandidates(ctx, state.profile, s.now().UTC())
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	descriptors, err := s.managedProbeDescriptors(state, facts)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	result := networkprotocol.VPNManagedPrepareResult{RequestID: request.RequestID, IntentID: state.intent.ID, ProfileID: state.intent.ProfileID, ProfileRevision: state.intent.ProfileRevision, SelectionPolicyRevision: state.intent.SelectionPolicyRevision, ExpiresAt: state.intent.ExpiresAt, SamplesPerGateway: max(3, state.policy.MinSamples), TimeoutMillis: 1000, MaxConcurrency: 4, Descriptors: descriptors}
	return s.outbound(identity, networkprotocol.MessageVPNManagedPrepareResult, result, state.intent.ExpiresAt)
}

func (s *Service) managedProbeDescriptors(state managedVPNContext, facts []runtime.VPNGatewayCandidate) ([]networkprotocol.VPNProbeDescriptor, error) {
	views := appnetworkaccess.VPNGatewayViews(facts, s.now().UTC(), 2*s.options.ConfigurationTTL)
	result := make([]networkprotocol.VPNProbeDescriptor, 0, len(facts))
	for index, fact := range facts {
		if !views[index].Available {
			continue
		}
		g := fact.Gateway
		descriptor := networkprotocol.VPNProbeDescriptor{GatewayID: g.ID, RuntimeID: g.RuntimeID, Name: g.Name, ProviderCode: g.ProviderCode}
		if slices.Contains(fact.Credential.Capabilities, networkprotocol.CapabilityVPNProbe) && appnetworkaccess.ValidVPNProbeURL(g.ProbeURL, g.PublicEndpointHost) {
			signer, err := networkprobe.NewSigner(s.options.CredentialEncryptionKeys)
			if err != nil {
				return nil, err
			}
			now := s.now().UTC()
			expiry := minTime(now.Add(time.Minute), state.intent.ExpiresAt)
			token, err := signer.Sign(networkprobe.Claims{Version: 1, GatewayRuntimeID: g.RuntimeID, EndpointRuntimeID: state.credential.RuntimeID, IntentID: state.intent.ID, IssuedAt: now.Unix(), ExpiresAt: expiry.Unix()})
			if err != nil {
				return nil, err
			}
			descriptor.URL, descriptor.Token, descriptor.ExpiresAt = g.ProbeURL, token, &expiry
		}
		result = append(result, descriptor)
	}
	return result, nil
}

func (s *Service) ConnectManagedVPN(ctx context.Context, identity networkidentity.Identity, raw []byte) (networkprotocol.RuntimeMessage, error) {
	request, credential, err := s.decodeManagedVPN(ctx, identity, networkprotocol.MessageVPNManagedConnectRequest, raw)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	store, ok := s.store.(ManagedVPNStore)
	if !ok {
		return networkprotocol.RuntimeMessage{}, managedVPNUnavailable()
	}
	hash := managedRequestHash(identity.ID, request)
	if previous, found, err := store.ManagedVPNResult(ctx, request.IntentID, identity.ID, request.RequestID, hash); err != nil {
		return networkprotocol.RuntimeMessage{}, err
	} else if found {
		if !previous.ValidUntil.After(s.now().UTC()) {
			return networkprotocol.RuntimeMessage{}, managedVPNIntentInvalid()
		}
		if err := s.revalidateManagedReplay(ctx, store, credential, request, previous); err != nil {
			return networkprotocol.RuntimeMessage{}, err
		}
		return s.outbound(identity, networkprotocol.MessageVPNManagedConnectResult, previous, previous.ValidUntil)
	}
	state, err := s.resolveManagedVPN(ctx, credential, request)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	facts, err := store.VPNGatewayCandidates(ctx, state.profile, s.now().UTC())
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	views := appnetworkaccess.VPNGatewayViews(facts, s.now().UTC(), 2*s.options.ConfigurationTTL)
	s.applyManagedMeasurements(ctx, state, request, views)
	ids, candidates, reason := appnetworkaccess.RankVPNCandidates(state.policy, state.intent.Selection, state.intent.RequestedGatewayID, identity.ID, views, s.now().UTC())
	connection := s.managedConnection(state, request, hash, candidates, reason)
	if len(ids) == 0 {
		return s.finishManagedFailure(ctx, identity, state, connection, "no_eligible_gateway")
	}
	result, err := s.tryManagedGateways(ctx, state, &connection, ids, facts)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	if result.Decision != domain.DecisionAllow {
		return s.finishManagedFailure(ctx, identity, state, connection, result.ReasonCode)
	}
	stored, found, err := store.ManagedVPNResult(ctx, request.IntentID, identity.ID, request.RequestID, hash)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	if !found {
		return networkprotocol.RuntimeMessage{}, managedVPNIntentInvalid()
	}
	return s.outbound(identity, networkprotocol.MessageVPNManagedConnectResult, stored, stored.ValidUntil)
}

func (s *Service) managedConnection(state managedVPNContext, request networkprotocol.VPNManagedConnectRequest, hash string, candidates []domain.VPNCandidateDecision, reason string) runtime.VPNConnection {
	now := s.now().UTC()
	decision := domain.VPNDecision{ID: state.intent.ID, ProfileID: state.intent.ProfileID, ProfileRevision: state.intent.ProfileRevision, SelectionPolicyID: state.intent.SelectionPolicyID, SelectionPolicyRevision: state.intent.SelectionPolicyRevision, SiteID: state.profile.SiteID, NetworkSpaceID: state.profile.NetworkSpaceID, SubjectID: state.credential.SubjectID, DeviceID: state.credential.DeviceID, Selection: state.intent.Selection, RequestedGatewayID: state.intent.RequestedGatewayID, Strategy: state.policy.Strategy, ReasonCode: reason, State: "selected", CreatedAt: now, UpdatedAt: now, Candidates: candidates}
	return runtime.VPNConnection{RuntimeID: state.credential.RuntimeID, CredentialID: state.credential.ID, SubjectID: state.credential.SubjectID, DeviceID: state.credential.DeviceID, SiteID: state.profile.SiteID, NetworkSpaceID: state.profile.NetworkSpaceID, Mode: state.profile.Mode, ResourceIDs: slices.Clone(state.profile.ResourceIDs), Decision: domain.DecisionAllow, AccessProfile: state.accessProfile, PolicyVersion: state.snapshot.PolicyVersion, PostureVersion: state.policyContext.principal.device.PostureVersion, EndpointPublicKey: state.credential.WireGuardPublicKey, ValidUntil: minTime(now.Add(s.options.LeaseTTL), state.credential.ExpiresAt), CreatedAt: now, Managed: &runtime.ManagedVPNConnection{Intent: state.intent, RequestID: request.RequestID, RequestHash: hash, Profile: state.profile, Policy: state.policy, Decision: decision}}
}

func (s *Service) tryManagedGateways(ctx context.Context, state managedVPNContext, connection *runtime.VPNConnection, ids []string, facts []runtime.VPNGatewayCandidate) (networkprotocol.VPNConnectResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := state.store.SaveVPNDecision(ctx, connection.Managed.Decision); err != nil {
		return networkprotocol.VPNConnectResult{}, err
	}
	lastReason := "gateway_unavailable"
	for attempt, id := range ids[:min(len(ids), state.policy.MaxAttempts)] {
		if err := ctx.Err(); err != nil {
			return networkprotocol.VPNConnectResult{}, err
		}
		index := slices.IndexFunc(facts, func(fact runtime.VPNGatewayCandidate) bool { return fact.Gateway.ID == id })
		if index < 0 {
			continue
		}
		fact := facts[index]
		connection.RequestID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(connection.Managed.RequestID+":"+id)).String()
		connection.RequestHash = connection.Managed.RequestHash
		connection.SessionID = vpnSessionID(connection.RuntimeID, connection.RequestID)
		connection.GatewayID, connection.GatewayRuntimeID, connection.GatewayCredentialID = id, fact.Gateway.RuntimeID, fact.Credential.ID
		connection.ValidUntil = minTime(connection.CreatedAt.Add(s.options.LeaseTTL), minTime(state.credential.ExpiresAt, fact.Credential.ExpiresAt))
		connection.ReasonCode = "policy_allowed"
		if attempt > 0 {
			if state.intent.Selection == domain.VPNSelectionManual {
				connection.Managed.Decision.ReasonCode = "manual_fallback_to_auto"
			} else {
				connection.Managed.Decision.ReasonCode = "auto_gateway_retry"
			}
		}
		result, err := s.store.SaveVPNConnection(ctx, *connection, state.snapshot)
		if err == nil && result.Decision == domain.DecisionAllow {
			return result, nil
		}
		if err != nil && !errors.Is(err, apperrors.ErrConflict) {
			return networkprotocol.VPNConnectResult{}, err
		}
		lastReason = "gateway_changed_before_connect"
		if err == nil && result.ReasonCode != "" {
			lastReason = result.ReasonCode
		}
		for i := range connection.Managed.Decision.Candidates {
			if connection.Managed.Decision.Candidates[i].GatewayID == id {
				connection.Managed.Decision.Candidates[i].Eligible = false
				connection.Managed.Decision.Candidates[i].ReasonCode = lastReason
			}
		}
	}
	return networkprotocol.VPNConnectResult{Decision: domain.DecisionDeny, ReasonCode: lastReason}, nil
}

func (s *Service) finishManagedFailure(ctx context.Context, identity networkidentity.Identity, state managedVPNContext, connection runtime.VPNConnection, reason string) (networkprotocol.RuntimeMessage, error) {
	connection.Decision, connection.ReasonCode = domain.DecisionDeny, reason
	connection.Managed.Decision.ReasonCode = reason
	result := networkprotocol.VPNConnectResult{RequestID: connection.Managed.RequestID, Decision: domain.DecisionDeny, ReasonCode: reason, PolicyVersion: state.snapshot.PolicyVersion, ValidUntil: state.intent.ExpiresAt, NetworkLeases: []networkprotocol.NetworkLease{}, ResourceLeases: []networkprotocol.ResourceLease{}}
	if err := state.store.FailManagedVPN(ctx, connection, result); err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	stored, _, err := state.store.ManagedVPNResult(ctx, state.intent.ID, identity.ID, connection.Managed.RequestID, connection.Managed.RequestHash)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	return s.outbound(identity, networkprotocol.MessageVPNManagedConnectResult, stored, stored.ValidUntil)
}

func (s *Service) applyManagedMeasurements(ctx context.Context, state managedVPNContext, request networkprotocol.VPNManagedConnectRequest, views []domain.VPNCandidate) {
	if s.options.VPNProbes == nil || request.ProbeBatchID == "" {
		return
	}
	batch, err := s.options.VPNProbes.VPNProbe(ctx, state.credential.RuntimeID, state.intent.ID, request.ProbeBatchID)
	if err != nil || batch == nil || batch.BatchID != request.ProbeBatchID || networkprotocol.ValidateVPNProbeBatch(*batch, s.now().UTC()) != nil || batch.WindowStartedAt.Before(state.intent.CreatedAt) || batch.IntentID != state.intent.ID || batch.ProfileID != state.intent.ProfileID || batch.ProfileRevision != state.intent.ProfileRevision || batch.SelectionPolicyRevision != state.intent.SelectionPolicyRevision {
		return
	}
	appnetworkaccess.ApplyVPNProbeViews(views, *batch)
}

func managedRequestHash(runtimeID string, request networkprotocol.VPNManagedConnectRequest) string {
	raw, _ := json.Marshal(request)
	digest := sha256.Sum256(append([]byte(runtimeID+"\x00managed-vpn\x00"), raw...))
	return fmt.Sprintf("sha256:%x", digest)
}
func managedVPNUnavailable() error {
	return apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "managed_vpn_unavailable", "Managed VPN is not supported by this runtime.", "当前运行时不支持受管 VPN。")
}
func managedVPNIntentInvalid() error {
	return apperrors.NewBusiness(apperrors.ErrAccessDenied, "vpn_intent_invalid", "The VPN request is expired, changed or unauthorized.", "VPN 请求已过期、发生变化或未获授权。")
}

func (s *Service) revalidateManagedReplay(ctx context.Context, store ManagedVPNStore, credential runtime.Credential, request networkprotocol.VPNManagedConnectRequest, previous networkprotocol.VPNManagedConnectResult) error {
	intent, err := store.GetVPNIntent(ctx, request.IntentID)
	if err != nil {
		return err
	}
	if intent.CredentialID != credential.ID || intent.RuntimeID != credential.RuntimeID || intent.SubjectID != credential.SubjectID || intent.DeviceID != credential.DeviceID {
		return managedVPNIntentInvalid()
	}
	if previous.Decision != domain.DecisionAllow {
		return nil
	}
	state, err := s.resolveManagedVPNContext(ctx, managedVPNContext{store: store, credential: credential, intent: intent}, request)
	if err != nil {
		return err
	}
	now := s.now().UTC()
	configuration, err := s.store.EnsureConfiguration(ctx, credential.RuntimeID, state.snapshot, now, now.Add(s.options.ConfigurationTTL))
	if err != nil {
		return err
	}
	for _, lease := range configuration.Desired.NetworkLeases {
		if lease.SessionID == previous.SessionID {
			return nil
		}
	}
	for _, lease := range configuration.Desired.ResourceLeases {
		if lease.SessionID == previous.SessionID {
			return nil
		}
	}
	return managedVPNIntentInvalid()
}
