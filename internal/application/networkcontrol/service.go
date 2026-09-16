package networkcontrol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	appnetworkaccess "github.com/opensoha/soha/internal/application/networkaccess"
	appruntimeconfig "github.com/opensoha/soha/internal/application/runtimeconfig"
	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	domainnetworkruntime "github.com/opensoha/soha/internal/domain/networkruntime"
	domainruntimeconfig "github.com/opensoha/soha/internal/domain/runtimeconfig"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

var networkIdentifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

type Store interface {
	LatestSnapshot(context.Context) (domainnetworkruntime.PolicySnapshot, error)
	ConsumeEnrollment(context.Context, domainnetworkruntime.EnrollmentConsumption, domainnetworkruntime.PolicySnapshot, time.Time) (domainnetworkruntime.Credential, domainnetworkruntime.Configuration, error)
	ActiveCredential(context.Context, string, time.Time) (domainnetworkruntime.Credential, error)
	ActiveEndpointCredential(context.Context, string, string, time.Time) (domainnetworkruntime.Credential, error)
	TouchCredential(context.Context, string, time.Time) error
	EnsureConfiguration(context.Context, string, domainnetworkruntime.PolicySnapshot, time.Time, time.Time) (domainnetworkruntime.Configuration, error)
	ApplyConfiguration(context.Context, string, networkprotocol.ConfigurationApplied, time.Time) error
	RenewLeases(context.Context, string, networkprotocol.LeaseRenewRequest, domainnetworkruntime.PolicySnapshot, time.Time, time.Duration) (networkprotocol.LeaseRenewResult, error)
	RevokeLeases(context.Context, string, networkprotocol.LeaseRevoke, domainnetworkruntime.PolicySnapshot, time.Time, time.Time) (int64, error)
	NASBinding(context.Context, string, string) (domainnetworkaccess.NASBinding, error)
	NetworkSubject(context.Context, string) (domainnetworkaccess.Subject, error)
	NetworkDevice(context.Context, string) (domainnetworkaccess.Device, error)
	NetworkSite(context.Context, string) (domainnetworkaccess.Site, error)
	NetworkSpace(context.Context, string) (domainnetworkaccess.Space, error)
	NetworkResource(context.Context, string) (domainnetworkaccess.Resource, error)
	ActiveVPNGateway(context.Context, string, string, time.Time) (domainnetworkaccess.Gateway, domainnetworkruntime.Credential, error)
	SaveVPNConnection(context.Context, domainnetworkruntime.VPNConnection, domainnetworkruntime.PolicySnapshot) (networkprotocol.VPNConnectResult, error)
	SiteProfileBinding(context.Context, string, string) (domainnetworkaccess.SiteProfileBinding, error)
	SaveNASAuthorization(context.Context, domainnetworkruntime.NASAuthorization) (domainnetworkruntime.NASAuthorization, error)
	ClaimNASSessionCommand(context.Context, string, time.Time) (domainnetworkaccess.SessionCommand, error)
	CompleteNASSessionCommand(context.Context, string, networkprotocol.NASSessionCommandResult, time.Time) error
	MihomoSource(context.Context, string, string, time.Time) (string, string, int, error)
}

type Options struct {
	VPNProbes                VPNProbeReader
	MaxClockSkew             time.Duration
	ConfigurationTTL         time.Duration
	LeaseTTL                 time.Duration
	CredentialEncryptionKeys keyring.Ring
	LoadRuntimeConfig        func(context.Context) (domainruntimeconfig.State, error)
}

type Service struct {
	store    Store
	schemas  *networkprotocol.Schemas
	options  Options
	now      func() time.Time
	snapshot struct {
		sync.RWMutex
		value domainnetworkruntime.PolicySnapshot
		ready bool
	}
}

func New(store Store, schemas *networkprotocol.Schemas, options Options) (*Service, error) {
	if store == nil || schemas == nil {
		return nil, fmt.Errorf("network control store and schemas are required")
	}
	if options.MaxClockSkew <= 0 || options.ConfigurationTTL <= 0 || options.ConfigurationTTL > 5*time.Minute || options.LeaseTTL <= 0 || options.LeaseTTL > 5*time.Minute {
		return nil, fmt.Errorf("network control options are invalid")
	}
	return &Service{store: store, schemas: schemas, options: options, now: time.Now}, nil
}

func (s *Service) RefreshSnapshot(ctx context.Context) error {
	snapshot, err := s.store.LatestSnapshot(ctx)
	if err != nil {
		return err
	}
	snapshot.ProtectedResourceIDs = slices.Clone(snapshot.ProtectedResourceIDs)
	snapshot.Policies = slices.Clone(snapshot.Policies)
	s.snapshot.Lock()
	s.snapshot.value, s.snapshot.ready = snapshot, true
	s.snapshot.Unlock()
	return nil
}

func (s *Service) Ready() bool {
	s.snapshot.RLock()
	defer s.snapshot.RUnlock()
	return s.snapshot.ready
}

func (s *Service) CurrentSnapshot() (domainnetworkruntime.PolicySnapshot, error) {
	s.snapshot.RLock()
	defer s.snapshot.RUnlock()
	if !s.snapshot.ready {
		return domainnetworkruntime.PolicySnapshot{}, apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "policy_snapshot_unavailable", "No network policy snapshot is loaded.", "尚未加载网络策略快照。")
	}
	snapshot := s.snapshot.value
	snapshot.ProtectedResourceIDs = slices.Clone(snapshot.ProtectedResourceIDs)
	snapshot.Policies = slices.Clone(snapshot.Policies)
	return snapshot, nil
}

func (s *Service) Enroll(ctx context.Context, identity networkidentity.Identity, token string, raw []byte) (networkprotocol.RuntimeMessage, error) {
	message, err := s.validateInbound(identity, networkprotocol.MessageEnrollmentRequest, raw)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	if len(strings.TrimSpace(token)) < 32 || identity.Certificate == nil || identity.Certificate.SerialNumber == nil {
		return networkprotocol.RuntimeMessage{}, apperrors.NewBusiness(apperrors.ErrUnauthorized, "invalid_enrollment_credential", "The enrollment credential is invalid.", "注册凭据无效。")
	}
	payload, err := networkprotocol.DecodePayload[networkprotocol.EnrollmentRequest](message.Payload)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, invalidRuntimeMessage()
	}
	if err := networkidentity.MatchPublicKey(identity.Certificate, payload.DevicePublicKey); err != nil {
		return networkprotocol.RuntimeMessage{}, apperrors.NewBusiness(apperrors.ErrUnauthorized, "enrollment_public_key_mismatch", "The enrollment public key does not match the client certificate.", "注册公钥与客户端证书不匹配。")
	}
	snapshot, err := s.CurrentSnapshot()
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	now := s.now().UTC()
	tokenDigest := sha256.Sum256([]byte(strings.TrimSpace(token)))
	consumption := domainnetworkruntime.EnrollmentConsumption{
		EnrollmentID: payload.EnrollmentID, ChallengeID: payload.ChallengeID,
		TokenHash: fmt.Sprintf("sha256:%x", tokenDigest), RuntimeID: identity.ID, RuntimeKind: identity.Kind,
		DeviceID: payload.DeviceID, CertificateFingerprint: identity.CertificateFingerprint,
		PublicKeyFingerprint: identity.PublicKeyFingerprint, WireGuardPublicKey: payload.WireGuardPublicKey,
		CertificateSerial:         identity.Certificate.SerialNumber.Text(16),
		CertificateAuthorityKeyID: fmt.Sprintf("%x", identity.Certificate.AuthorityKeyId),
		Capabilities:              slices.Clone(payload.Capabilities), NotBefore: identity.Certificate.NotBefore.UTC(),
		ExpiresAt: identity.Certificate.NotAfter.UTC(), ConsumedAt: now,
	}
	credential, _, err := s.store.ConsumeEnrollment(ctx, consumption, snapshot, now.Add(s.options.ConfigurationTTL))
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	result := networkprotocol.EnrollmentResult{
		Accepted: true, ReasonCode: "enrolled", CredentialReference: "secretref:runtime-credential/" + credential.ID,
		CredentialExpiresAt: &credential.ExpiresAt,
	}
	return s.outbound(identity, networkprotocol.MessageEnrollmentResult, result, minTime(now.Add(s.options.MaxClockSkew), credential.ExpiresAt))
}

func (s *Service) Configuration(ctx context.Context, identity networkidentity.Identity) (networkprotocol.RuntimeMessage, error) {
	credential, err := s.authenticate(ctx, identity)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	snapshot, err := s.CurrentSnapshot()
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	now := s.now().UTC()
	configuration, err := s.store.EnsureConfiguration(ctx, identity.ID, snapshot, now, now.Add(s.options.ConfigurationTTL))
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	desired := configuration.Desired
	if err := s.configureVPNProbe(ctx, credential, &desired); err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	desired.RuntimeIntervals, err = s.runtimeIntervals(ctx)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	return s.outbound(identity, networkprotocol.MessageConfiguration, desired, configuration.ValidUntil)
}

func (s *Service) runtimeIntervals(ctx context.Context) (*networkprotocol.RuntimeIntervals, error) {
	intervals := &networkprotocol.RuntimeIntervals{
		HeartbeatIntervalSeconds:         networkprotocol.DefaultRuntimeIntervalSeconds,
		ConfigurationPollIntervalSeconds: networkprotocol.DefaultRuntimeIntervalSeconds,
	}
	if s.options.LoadRuntimeConfig == nil {
		return intervals, nil
	}
	state, err := s.options.LoadRuntimeConfig(ctx)
	if err != nil {
		return nil, fmt.Errorf("load network runtime intervals: %w", err)
	}
	intervals.HeartbeatIntervalSeconds = runtimeInterval(state.Overrides[appruntimeconfig.KeyNetworkHeartbeatInterval])
	intervals.ConfigurationPollIntervalSeconds = runtimeInterval(state.Overrides[appruntimeconfig.KeyNetworkConfigPollInterval])
	return intervals, nil
}

func runtimeInterval(value any) int {
	seconds := networkprotocol.DefaultRuntimeIntervalSeconds
	switch typed := value.(type) {
	case int:
		seconds = typed
	case int64:
		seconds = int(typed)
	case float64:
		seconds = int(typed)
	}
	if seconds < networkprotocol.MinRuntimeIntervalSeconds || seconds > networkprotocol.MaxRuntimeIntervalSeconds {
		return networkprotocol.DefaultRuntimeIntervalSeconds
	}
	return seconds
}

func (s *Service) MihomoSource(ctx context.Context, identity networkidentity.Identity, profileID string) (networkprotocol.MihomoSource, error) {
	if identity.Kind != "endpoint" {
		return networkprotocol.MihomoSource{}, mihomoSubscriptionDenied()
	}
	profileID = strings.TrimSpace(profileID)
	if !networkIdentifierPattern.MatchString(profileID) {
		return networkprotocol.MihomoSource{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "mihomo_profile_id_invalid", "The mihomo profile ID is invalid.", "mihomo 配置 ID 无效。")
	}
	credential, err := s.authenticate(ctx, identity)
	if err != nil {
		return networkprotocol.MihomoSource{}, err
	}
	if !slices.Contains(credential.Capabilities, "mihomo") {
		return networkprotocol.MihomoSource{}, mihomoSubscriptionDenied()
	}
	sourceType, ciphertext, revision, err := s.store.MihomoSource(ctx, credential.ID, profileID, s.now().UTC())
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) || errors.Is(err, apperrors.ErrAccessDenied) || errors.Is(err, apperrors.ErrUnauthorized) {
			return networkprotocol.MihomoSource{}, mihomoSubscriptionDenied()
		}
		return networkprotocol.MihomoSource{}, err
	}
	if revision < 1 || !strings.HasPrefix(ciphertext, secretcrypto.PrefixV2) {
		return networkprotocol.MihomoSource{}, mihomoSubscriptionUnavailable()
	}
	plaintext, err := secretcrypto.DecryptStringWithKeyring(s.options.CredentialEncryptionKeys, ciphertext)
	if err != nil {
		return networkprotocol.MihomoSource{}, mihomoSubscriptionUnavailable()
	}
	result := networkprotocol.MihomoSource{ProfileID: profileID, ProfileRevision: revision, SourceType: sourceType}
	switch sourceType {
	case domainnetworkaccess.MihomoSourceManagedSubscription:
		if appnetworkaccess.ValidateMihomoSubscriptionURL(plaintext) != nil {
			return networkprotocol.MihomoSource{}, mihomoSubscriptionUnavailable()
		}
		result.SubscriptionURL = plaintext
	case domainnetworkaccess.MihomoSourceManualNode:
		var node domainnetworkaccess.MihomoManualNode
		if json.Unmarshal([]byte(plaintext), &node) != nil || appnetworkaccess.ValidateMihomoManualNode(node) != nil {
			return networkprotocol.MihomoSource{}, mihomoSubscriptionUnavailable()
		}
		result.ManualNode = &networkprotocol.MihomoManualNode{Protocol: node.Protocol, Server: node.Server, Port: node.Port, Username: node.Username, Password: node.Password}
	default:
		return networkprotocol.MihomoSource{}, mihomoSubscriptionUnavailable()
	}
	return result, nil
}

func (s *Service) MihomoSubscription(ctx context.Context, identity networkidentity.Identity, profileID string) (networkprotocol.MihomoSubscription, error) {
	source, err := s.MihomoSource(ctx, identity, profileID)
	if err != nil || source.SourceType != domainnetworkaccess.MihomoSourceManagedSubscription {
		if err != nil {
			return networkprotocol.MihomoSubscription{}, err
		}
		return networkprotocol.MihomoSubscription{}, mihomoSubscriptionDenied()
	}
	return networkprotocol.MihomoSubscription{ProfileID: source.ProfileID, ProfileRevision: source.ProfileRevision, SubscriptionURL: source.SubscriptionURL}, nil
}

func mihomoSubscriptionDenied() error {
	return apperrors.NewBusiness(apperrors.ErrAccessDenied, "mihomo_subscription_unavailable", "The mihomo subscription is not available to this endpoint.", "当前端点无法访问该 mihomo 订阅。")
}

func mihomoSubscriptionUnavailable() error {
	return apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "mihomo_subscription_decryption_failed", "The mihomo subscription could not be decrypted.", "mihomo 订阅无法解密。")
}

func (s *Service) Snapshot(ctx context.Context, identity networkidentity.Identity) (domainnetworkruntime.PolicySnapshot, error) {
	if _, err := s.authenticate(ctx, identity); err != nil {
		return domainnetworkruntime.PolicySnapshot{}, err
	}
	return s.CurrentSnapshot()
}

func (s *Service) Apply(ctx context.Context, identity networkidentity.Identity, raw []byte) error {
	message, err := s.validateInbound(identity, networkprotocol.MessageConfigurationApply, raw)
	if err != nil {
		return err
	}
	if _, err := s.authenticate(ctx, identity); err != nil {
		return err
	}
	payload, err := networkprotocol.DecodePayload[networkprotocol.ConfigurationApplied](message.Payload)
	if err != nil {
		return invalidRuntimeMessage()
	}
	return s.store.ApplyConfiguration(ctx, identity.ID, payload, s.now().UTC())
}

func (s *Service) Renew(ctx context.Context, identity networkidentity.Identity, raw []byte) (networkprotocol.RuntimeMessage, error) {
	message, err := s.validateInbound(identity, networkprotocol.MessageLeaseRenewRequest, raw)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	if _, err := s.authenticate(ctx, identity); err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	payload, err := networkprotocol.DecodePayload[networkprotocol.LeaseRenewRequest](message.Payload)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, invalidRuntimeMessage()
	}
	snapshot, err := s.CurrentSnapshot()
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	now := s.now().UTC()
	result, err := s.store.RenewLeases(ctx, identity.ID, payload, snapshot, now, s.options.LeaseTTL)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	return s.outbound(identity, networkprotocol.MessageLeaseRenewResult, result, result.ValidUntil)
}

func (s *Service) Revoke(ctx context.Context, identity networkidentity.Identity, raw []byte) (int64, error) {
	message, err := s.validateInbound(identity, networkprotocol.MessageLeaseRevoke, raw)
	if err != nil {
		return 0, err
	}
	if _, err := s.authenticate(ctx, identity); err != nil {
		return 0, err
	}
	payload, err := networkprotocol.DecodePayload[networkprotocol.LeaseRevoke](message.Payload)
	if err != nil {
		return 0, invalidRuntimeMessage()
	}
	if payload.EffectiveAt.After(message.ExpiresAt) {
		return 0, invalidRuntimeMessage()
	}
	snapshot, err := s.CurrentSnapshot()
	if err != nil {
		return 0, err
	}
	now := s.now().UTC()
	return s.store.RevokeLeases(ctx, identity.ID, payload, snapshot, now, now.Add(s.options.ConfigurationTTL))
}

func (s *Service) ConnectVPN(ctx context.Context, identity networkidentity.Identity, raw []byte) (networkprotocol.RuntimeMessage, error) {
	message, err := s.validateInbound(identity, networkprotocol.MessageVPNConnectRequest, raw)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	if identity.Kind != "endpoint" {
		return networkprotocol.RuntimeMessage{}, unauthorizedRuntime()
	}
	credential, err := s.authenticate(ctx, identity)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	request, err := networkprotocol.DecodePayload[networkprotocol.VPNConnectRequest](message.Payload)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, invalidRuntimeMessage()
	}
	snapshot, err := s.CurrentSnapshot()
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	now := s.now().UTC()
	connection := domainnetworkruntime.VPNConnection{
		RequestID: request.RequestID, RequestHash: vpnRequestHash(identity.ID, request), RuntimeID: identity.ID,
		CredentialID: credential.ID, SubjectID: credential.SubjectID, DeviceID: credential.DeviceID,
		SiteID: request.SiteID, NetworkSpaceID: request.NetworkSpaceID, ResourceIDs: slices.Clone(request.ResourceIDs),
		AccessGrantID: request.AccessGrantID, AccessGrantTokenHash: accessGrantTokenHash(request.AccessGrantToken), Mode: request.Mode,
		Decision: domainnetworkaccess.DecisionDeny, AccessProfile: domainnetworkaccess.ProfileDeny,
		PolicyVersion: snapshot.PolicyVersion, SessionID: vpnSessionID(identity.ID, request.RequestID),
		EndpointPublicKey: credential.WireGuardPublicKey, ValidUntil: minTime(now.Add(s.options.MaxClockSkew), credential.ExpiresAt), CreatedAt: now,
	}
	finish := func(reason string) (networkprotocol.RuntimeMessage, error) {
		connection.ReasonCode = reason
		result, saveErr := s.store.SaveVPNConnection(ctx, connection, snapshot)
		if saveErr != nil {
			return networkprotocol.RuntimeMessage{}, saveErr
		}
		return s.outbound(identity, networkprotocol.MessageVPNConnectResult, result, minTime(now.Add(s.options.MaxClockSkew), result.ValidUntil))
	}
	if reason := vpnRequestDenial(request, credential); reason != "" {
		return finish(reason)
	}
	policyContext, reason, err := s.loadVPNPolicyContext(ctx, credential, request, snapshot)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	if reason != "" {
		return finish(reason)
	}
	connection.PostureVersion = policyContext.principal.device.PostureVersion
	accessProfile, reason, err := s.evaluateVPNAccess(ctx, credential, request, policyContext)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	if reason != "" {
		return finish(reason)
	}
	gateway, gatewayCredential, err := s.store.ActiveVPNGateway(ctx, request.SiteID, request.GatewayID, now)
	if errors.Is(err, apperrors.ErrNotFound) {
		return finish("gateway_unavailable")
	} else if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	if !s.validVPNGateway(gateway, gatewayCredential, now) {
		return finish("gateway_unavailable")
	}
	connection.Decision, connection.AccessProfile, connection.ReasonCode = domainnetworkaccess.DecisionAllow, accessProfile, "policy_allowed"
	connection.GatewayID, connection.GatewayRuntimeID, connection.GatewayCredentialID = gateway.ID, gateway.RuntimeID, gatewayCredential.ID
	connection.ValidUntil = minTime(now.Add(s.options.LeaseTTL), minTime(credential.ExpiresAt, gatewayCredential.ExpiresAt))
	return finish(connection.ReasonCode)
}

type policyPrincipal struct {
	subject  domainnetworkaccess.Subject
	device   domainnetworkaccess.Device
	snapshot domainnetworkaccess.PolicySnapshot
}

type vpnPolicyContext struct {
	principal policyPrincipal
	site      domainnetworkaccess.Site
	space     domainnetworkaccess.Space
}

func vpnRequestDenial(request networkprotocol.VPNConnectRequest, credential domainnetworkruntime.Credential) string {
	ztna := isZTNAMode(request.Mode)
	if request.Mode != domainnetworkaccess.ModeExternalVPN && !ztna {
		return "access_mode_not_available"
	}
	if request.Mode == domainnetworkaccess.ModeExternalVPN && len(request.ResourceIDs) != 0 {
		return "resource_scope_requires_ztna"
	}
	if ztna && len(request.ResourceIDs) == 0 {
		return "resource_scope_required"
	}
	if credential.WireGuardPublicKey == "" || !slices.Contains(credential.Capabilities, "wireguard") {
		return "wireguard_key_unavailable"
	}
	return ""
}

func isZTNAMode(mode string) bool {
	return mode == domainnetworkaccess.ModeInternalZTNA || mode == domainnetworkaccess.ModeExternalVPNZTNA || mode == domainnetworkaccess.ModeExternalDirectZTNA
}

func (s *Service) loadPolicyPrincipal(ctx context.Context, subjectID, deviceID string, snapshot domainnetworkruntime.PolicySnapshot) (policyPrincipal, error) {
	var policies []domainnetworkaccess.Policy
	if err := json.Unmarshal(snapshot.Policies, &policies); err != nil {
		return policyPrincipal{}, fmt.Errorf("decode network policy snapshot: %w", err)
	}
	subject, err := s.store.NetworkSubject(ctx, subjectID)
	if errors.Is(err, apperrors.ErrNotFound) {
		subject = domainnetworkaccess.Subject{UserID: subjectID, Status: domainnetworkaccess.StatusDisabled}
	} else if err != nil {
		return policyPrincipal{}, err
	}
	device, err := s.store.NetworkDevice(ctx, deviceID)
	if errors.Is(err, apperrors.ErrNotFound) {
		device = domainnetworkaccess.Device{ID: deviceID, Status: domainnetworkaccess.DeviceStatusRevoked, PostureStatus: domainnetworkaccess.PostureUnknown}
	} else if err != nil {
		return policyPrincipal{}, err
	}
	return policyPrincipal{subject: subject, device: device, snapshot: domainnetworkaccess.PolicySnapshot{PolicyVersion: snapshot.PolicyVersion, Policies: policies}}, nil
}

func (s *Service) loadVPNPolicyContext(ctx context.Context, credential domainnetworkruntime.Credential, request networkprotocol.VPNConnectRequest, snapshot domainnetworkruntime.PolicySnapshot) (vpnPolicyContext, string, error) {
	principal, err := s.loadPolicyPrincipal(ctx, credential.SubjectID, credential.DeviceID, snapshot)
	if err != nil {
		return vpnPolicyContext{}, "", err
	}
	site, err := s.store.NetworkSite(ctx, request.SiteID)
	if errors.Is(err, apperrors.ErrNotFound) {
		return vpnPolicyContext{}, "network_scope_not_active", nil
	}
	if err != nil {
		return vpnPolicyContext{}, "", err
	}
	space, err := s.store.NetworkSpace(ctx, request.NetworkSpaceID)
	if errors.Is(err, apperrors.ErrNotFound) {
		return vpnPolicyContext{}, "network_scope_not_active", nil
	}
	if err != nil {
		return vpnPolicyContext{}, "", err
	}
	if space.Status != domainnetworkaccess.StatusActive || space.SiteID != site.ID || hasDefaultRoute(space.CIDRs) {
		return vpnPolicyContext{}, "network_scope_not_active", nil
	}
	return vpnPolicyContext{principal: principal, site: site, space: space}, "", nil
}

func (s *Service) evaluateVPNAccess(ctx context.Context, credential domainnetworkruntime.Credential, request networkprotocol.VPNConnectRequest, policyContext vpnPolicyContext) (string, string, error) {
	if !isZTNAMode(request.Mode) {
		preview := appnetworkaccess.EvaluateAdmission(appnetworkaccess.AdmissionInput{SubjectUserID: credential.SubjectID, DeviceID: credential.DeviceID, SiteID: request.SiteID, Mode: request.Mode}, policyContext.principal.subject, policyContext.principal.device, policyContext.site, policyContext.principal.snapshot)
		if preview.Decision != domainnetworkaccess.DecisionAllow || !preview.NetworkLeaseRequired || preview.ResourceLeaseRequired {
			return "", firstReason(preview.Reasons, "authorization_denied"), nil
		}
		return preview.NetworkProfile, "", nil
	}
	return s.evaluateZTNAResources(ctx, credential, request, policyContext)
}

func (s *Service) evaluateZTNAResources(ctx context.Context, credential domainnetworkruntime.Credential, request networkprotocol.VPNConnectRequest, policyContext vpnPolicyContext) (string, string, error) {
	accessProfile := ""
	for _, resourceID := range request.ResourceIDs {
		resource, err := s.store.NetworkResource(ctx, resourceID)
		if errors.Is(err, apperrors.ErrNotFound) {
			return "", "resource_scope_not_active", nil
		}
		if err != nil {
			return "", "", err
		}
		if _, supported := domainnetworkaccess.WireGuardResourceTarget(resource, policyContext.space); !supported {
			return "", "resource_transport_not_supported", nil
		}
		preview := appnetworkaccess.EvaluatePolicy(appnetworkaccess.PreviewInput{SubjectUserID: credential.SubjectID, DeviceID: credential.DeviceID, ResourceID: resourceID, SiteID: request.SiteID, Mode: request.Mode}, policyContext.principal.subject, policyContext.principal.device, policyContext.site, policyContext.space, resource, policyContext.principal.snapshot)
		if preview.Decision != domainnetworkaccess.DecisionAllow || !preview.ResourceLeaseRequired || preview.NetworkLeaseRequired != (request.Mode == domainnetworkaccess.ModeExternalVPNZTNA) {
			return "", firstReason(preview.Reasons, "authorization_denied"), nil
		}
		if accessProfile != "" && accessProfile != preview.NetworkProfile {
			return "", "resource_access_profile_conflict", nil
		}
		accessProfile = preview.NetworkProfile
	}
	return accessProfile, "", nil
}

func (s *Service) validVPNGateway(gateway domainnetworkaccess.Gateway, credential domainnetworkruntime.Credential, now time.Time) bool {
	return gateway.ID != "" && gateway.RuntimeID == credential.RuntimeID && gateway.AdministrativeStatus == domainnetworkaccess.StatusActive && gateway.WireGuardPublicKey != "" && gateway.WireGuardPublicKey == credential.WireGuardPublicKey && slices.Contains(credential.Capabilities, "wireguard") && credential.LastControlSeenAt != nil && !credential.LastControlSeenAt.Before(now.Add(-2*s.options.ConfigurationTTL))
}

func (s *Service) AuthorizeNAS(ctx context.Context, identity networkidentity.Identity, raw []byte) (networkprotocol.RuntimeMessage, error) {
	message, err := s.validateInbound(identity, networkprotocol.MessageNASAuthorizationRequest, raw)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	payload, err := networkprotocol.DecodePayload[networkprotocol.NASAuthorizationRequest](message.Payload)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, invalidRuntimeMessage()
	}
	result, err := s.authorizeNAS(ctx, identity, payload)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	return s.outbound(identity, networkprotocol.MessageNASAuthorizationResult, result, minTime(s.now().UTC().Add(s.options.MaxClockSkew), result.ValidUntil))
}

func (s *Service) AuthorizeRADIUS(ctx context.Context, identity networkidentity.Identity, raw []byte) (networkprotocol.NASAuthorizationResult, error) {
	if identity.Kind != "nas" {
		return networkprotocol.NASAuthorizationResult{}, unauthorizedRuntime()
	}
	now := s.now().UTC()
	encoded, err := json.Marshal(networkprotocol.RuntimeMessage{
		SchemaVersion: networkprotocol.RuntimeSchemaVersion,
		MessageID:     uuid.NewString(),
		MessageType:   networkprotocol.MessageNASAuthorizationRequest,
		ProducerID:    identity.ID,
		RuntimeID:     identity.ID,
		RuntimeKind:   identity.Kind,
		OccurredAt:    now,
		ExpiresAt:     now.Add(s.options.MaxClockSkew),
		Payload:       json.RawMessage(raw),
	})
	if err != nil {
		return networkprotocol.NASAuthorizationResult{}, invalidRuntimeMessage()
	}
	message, err := s.AuthorizeNAS(ctx, identity, encoded)
	if err != nil {
		return networkprotocol.NASAuthorizationResult{}, err
	}
	result, err := networkprotocol.DecodePayload[networkprotocol.NASAuthorizationResult](message.Payload)
	if err != nil {
		return networkprotocol.NASAuthorizationResult{}, fmt.Errorf("decode NAS authorization result: %w", err)
	}
	return result, nil
}

func (s *Service) NextNASSessionCommand(ctx context.Context, identity networkidentity.Identity) (networkprotocol.RuntimeMessage, error) {
	if identity.Kind != "nas" {
		return networkprotocol.RuntimeMessage{}, unauthorizedRuntime()
	}
	if _, err := s.authenticate(ctx, identity); err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	now := s.now().UTC()
	command, err := s.store.ClaimNASSessionCommand(ctx, identity.ID, now)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	if command.RuntimeID != identity.ID || command.ID == "" || command.SessionID == "" || command.NASID == "" {
		return networkprotocol.RuntimeMessage{}, fmt.Errorf("claimed NAS session command does not match runtime")
	}
	payload := networkprotocol.NASSessionCommand{
		CommandID: command.ID, SessionID: command.SessionID, NASID: command.NASID,
		SubjectID: command.SubjectID, DeviceID: command.DeviceID,
		Action: command.Action, TargetAccessProfile: command.TargetAccessProfile, PolicyVersion: command.PolicyVersion,
		EffectiveAt: command.EffectiveAt, ReasonCode: command.ReasonCode, RadiusAttributes: command.RadiusAttributes,
	}
	return s.outbound(identity, networkprotocol.MessageNASSessionCommand, payload, minTime(now.Add(s.options.MaxClockSkew), command.ExpiresAt))
}

func (s *Service) CompleteNASSessionCommand(ctx context.Context, identity networkidentity.Identity, raw []byte) error {
	if identity.Kind != "nas" {
		return unauthorizedRuntime()
	}
	message, err := s.validateInbound(identity, networkprotocol.MessageNASSessionCommandResult, raw)
	if err != nil {
		return err
	}
	if _, err := s.authenticate(ctx, identity); err != nil {
		return err
	}
	result, err := networkprotocol.DecodePayload[networkprotocol.NASSessionCommandResult](message.Payload)
	if err != nil || result.CompletedAt.After(message.ExpiresAt) {
		return invalidRuntimeMessage()
	}
	return s.store.CompleteNASSessionCommand(ctx, identity.ID, result, s.now().UTC())
}

func (s *Service) authorizeNAS(ctx context.Context, identity networkidentity.Identity, request networkprotocol.NASAuthorizationRequest) (networkprotocol.NASAuthorizationResult, error) {
	binding, err := s.authenticateNAS(ctx, identity, request.NASID)
	if err != nil {
		return networkprotocol.NASAuthorizationResult{}, err
	}
	request, err = s.resolveNASDevice(ctx, request)
	if err != nil {
		return networkprotocol.NASAuthorizationResult{}, err
	}
	snapshot, err := s.CurrentSnapshot()
	if err != nil {
		return networkprotocol.NASAuthorizationResult{}, err
	}
	principal, err := s.loadPolicyPrincipal(ctx, request.SubjectID, request.DeviceID, snapshot)
	if err != nil {
		return networkprotocol.NASAuthorizationResult{}, err
	}
	site, err := s.store.NetworkSite(ctx, binding.SiteID)
	if err != nil {
		return networkprotocol.NASAuthorizationResult{}, err
	}
	preview := appnetworkaccess.EvaluateAdmission(appnetworkaccess.AdmissionInput{SubjectUserID: request.SubjectID, DeviceID: request.DeviceID, SiteID: binding.SiteID, Mode: domainnetworkaccess.ModeInternalDirect}, principal.subject, principal.device, site, principal.snapshot)

	now := s.now().UTC()
	authorization := domainnetworkruntime.NASAuthorization{
		RequestID: request.RequestID, RequestHash: nasRequestHash(identity.ID, request), RuntimeID: identity.ID,
		NASID: request.NASID, SubjectID: request.SubjectID, DeviceID: request.DeviceID, AuthenticationMethod: request.AuthenticationMethod,
		SessionID: nasSessionID(identity.ID, request.RequestID), Decision: preview.Decision, AccessProfile: domainnetworkaccess.ProfileDeny,
		PolicyVersion: snapshot.PolicyVersion, ReasonCode: firstReason(preview.Reasons, "authorization_denied"),
		ValidUntil: now.Add(s.options.MaxClockSkew), CreatedAt: now,
	}
	if preview.Decision == domainnetworkaccess.DecisionAllow {
		authorization.AccessProfile, authorization.ReasonCode = preview.NetworkProfile, "policy_allowed"
		if request.AuthenticationMethod == networkprotocol.NASAuthenticationPasswordCompatible && authorization.AccessProfile == domainnetworkaccess.ProfileFull {
			authorization.AccessProfile, authorization.ReasonCode = domainnetworkaccess.ProfileRestricted, "password_compatible_profile_capped"
		}
		profile, profileErr := s.store.SiteProfileBinding(ctx, binding.SiteID, authorization.AccessProfile)
		if errors.Is(profileErr, apperrors.ErrNotFound) {
			authorization.Decision, authorization.AccessProfile, authorization.ReasonCode = domainnetworkaccess.DecisionDeny, domainnetworkaccess.ProfileDeny, "site_profile_binding_unavailable"
		} else if profileErr != nil {
			return networkprotocol.NASAuthorizationResult{}, profileErr
		} else if profile.SiteID != binding.SiteID || profile.AccessProfile != authorization.AccessProfile {
			return networkprotocol.NASAuthorizationResult{}, fmt.Errorf("site profile binding does not match authorization")
		} else {
			authorization.RadiusAttributes = &networkprotocol.RadiusAttributes{VLANID: profile.VLANID, FilterID: profile.FilterID, SessionTimeoutSeconds: profile.SessionTimeoutSeconds}
			authorization.ValidUntil = now.Add(time.Duration(profile.SessionTimeoutSeconds) * time.Second)
		}
	}
	saved, err := s.store.SaveNASAuthorization(ctx, authorization)
	if err != nil {
		return networkprotocol.NASAuthorizationResult{}, err
	}
	return nasAuthorizationResult(saved), nil
}

func (s *Service) authenticateNAS(ctx context.Context, identity networkidentity.Identity, nasID string) (domainnetworkaccess.NASBinding, error) {
	if identity.Kind != "nas" {
		return domainnetworkaccess.NASBinding{}, unauthorizedRuntime()
	}
	if _, err := s.authenticate(ctx, identity); err != nil {
		return domainnetworkaccess.NASBinding{}, err
	}
	binding, err := s.store.NASBinding(ctx, identity.ID, nasID)
	if err != nil {
		return domainnetworkaccess.NASBinding{}, err
	}
	if binding.Status != domainnetworkaccess.StatusActive || binding.RuntimeID != identity.ID || binding.NASID != nasID {
		return domainnetworkaccess.NASBinding{}, unauthorizedRuntime()
	}
	return binding, nil
}

func (s *Service) resolveNASDevice(ctx context.Context, request networkprotocol.NASAuthorizationRequest) (networkprotocol.NASAuthorizationRequest, error) {
	if request.AuthenticationMethod != networkprotocol.NASAuthenticationEAPTLS {
		return request, nil
	}
	serial, authorityKeyID, err := canonicalCertificateBinding(request.ClientCertificateSerial, request.ClientCertificateAuthorityKeyID)
	if err != nil {
		return networkprotocol.NASAuthorizationRequest{}, invalidRuntimeMessage()
	}
	credential, err := s.store.ActiveEndpointCredential(ctx, serial, authorityKeyID, s.now().UTC())
	if err != nil {
		return networkprotocol.NASAuthorizationRequest{}, err
	}
	if credential.RuntimeKind != "endpoint" || credential.SubjectID != request.SubjectID {
		return networkprotocol.NASAuthorizationRequest{}, unauthorizedRuntime()
	}
	request.DeviceID = credential.DeviceID
	return request, nil
}

func canonicalCertificateBinding(serial, authorityKeyID string) (string, string, error) {
	serialNumber, ok := new(big.Int).SetString(strings.TrimSpace(serial), 16)
	if !ok || serialNumber.Sign() <= 0 || serialNumber.BitLen() > 160 {
		return "", "", fmt.Errorf("invalid certificate serial")
	}
	keyID := strings.ReplaceAll(strings.TrimSpace(authorityKeyID), ":", "")
	decoded, err := hex.DecodeString(keyID)
	if err != nil || len(decoded) < 8 || len(decoded) > 64 {
		return "", "", fmt.Errorf("invalid certificate authority key identifier")
	}
	return serialNumber.Text(16), hex.EncodeToString(decoded), nil
}

func nasAuthorizationResult(value domainnetworkruntime.NASAuthorization) networkprotocol.NASAuthorizationResult {
	return networkprotocol.NASAuthorizationResult{RequestID: value.RequestID, SessionID: value.SessionID, Decision: value.Decision, AccessProfile: value.AccessProfile, PolicyVersion: value.PolicyVersion, ValidUntil: value.ValidUntil, ReasonCode: value.ReasonCode, RadiusAttributes: value.RadiusAttributes}
}

func nasRequestHash(runtimeID string, request networkprotocol.NASAuthorizationRequest) string {
	encoded, _ := json.Marshal(request)
	digest := sha256.Sum256(append(append([]byte(runtimeID), '\n'), encoded...))
	return fmt.Sprintf("sha256:%x", digest)
}

func vpnRequestHash(runtimeID string, request networkprotocol.VPNConnectRequest) string {
	encoded, _ := json.Marshal(request)
	digest := sha256.Sum256(append(append([]byte(runtimeID), '\n'), encoded...))
	return fmt.Sprintf("sha256:%x", digest)
}

func accessGrantTokenHash(token string) string {
	if token == "" {
		return ""
	}
	digest := sha256.Sum256([]byte(token))
	return fmt.Sprintf("sha256:%x", digest)
}

func vpnSessionID(runtimeID, requestID string) string {
	digest := sha256.Sum256([]byte(runtimeID + "\n" + requestID))
	return fmt.Sprintf("vpn-%x", digest)
}

func hasDefaultRoute(cidrs []string) bool {
	return slices.Contains(cidrs, "0.0.0.0/0")
}

func nasSessionID(runtimeID, requestID string) string {
	digest := sha256.Sum256([]byte(runtimeID + "\n" + requestID))
	return fmt.Sprintf("nac-%x", digest)
}

func firstReason(reasons []string, fallback string) string {
	if len(reasons) == 0 || reasons[0] == "" {
		return fallback
	}
	return reasons[0]
}

func (s *Service) authenticate(ctx context.Context, identity networkidentity.Identity) (domainnetworkruntime.Credential, error) {
	if identity.Scope != networkidentity.ScopeNetworkControl {
		return domainnetworkruntime.Credential{}, unauthorizedRuntime()
	}
	now := s.now().UTC()
	credential, err := s.store.ActiveCredential(ctx, identity.CertificateFingerprint, now)
	if err != nil {
		return domainnetworkruntime.Credential{}, err
	}
	if credential.RuntimeID != identity.ID || credential.RuntimeKind != identity.Kind {
		return domainnetworkruntime.Credential{}, unauthorizedRuntime()
	}
	if err := s.store.TouchCredential(ctx, credential.ID, now); err != nil {
		return domainnetworkruntime.Credential{}, err
	}
	return credential, nil
}

func (s *Service) validateInbound(identity networkidentity.Identity, messageType string, raw []byte) (networkprotocol.RuntimeMessage, error) {
	if err := s.schemas.ValidateRuntime(raw); err != nil {
		return networkprotocol.RuntimeMessage{}, invalidRuntimeMessage()
	}
	message, err := networkprotocol.DecodeRuntimeMessage(raw)
	if err != nil || message.MessageType != messageType {
		return networkprotocol.RuntimeMessage{}, invalidRuntimeMessage()
	}
	if identity.Scope != networkidentity.ScopeNetworkControl || message.ProducerID != identity.ID || message.RuntimeID != identity.ID || message.RuntimeKind != identity.Kind {
		return networkprotocol.RuntimeMessage{}, unauthorizedRuntime()
	}
	if err := networkprotocol.ValidateRuntimeMessageWindow(message, s.now().UTC(), s.options.MaxClockSkew); err != nil {
		return networkprotocol.RuntimeMessage{}, apperrors.NewBusiness(apperrors.ErrGone, "runtime_message_expired", "The runtime message is expired or outside the accepted clock window.", "运行时消息已过期或超出允许的时钟窗口。")
	}
	return message, nil
}

func (s *Service) outbound(identity networkidentity.Identity, messageType string, payload any, expiresAt time.Time) (networkprotocol.RuntimeMessage, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	return networkprotocol.RuntimeMessage{
		SchemaVersion: networkprotocol.RuntimeSchemaVersion, MessageID: uuid.NewString(), MessageType: messageType,
		ProducerID: "network-control", RuntimeID: identity.ID, RuntimeKind: identity.Kind,
		OccurredAt: s.now().UTC(), ExpiresAt: expiresAt.UTC(), Payload: encoded,
	}, nil
}

func invalidRuntimeMessage() error {
	return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "invalid_runtime_message", "The request does not match the network-runtime contract.", "请求不符合 network-runtime 契约。")
}

func unauthorizedRuntime() error {
	return apperrors.NewBusiness(apperrors.ErrUnauthorized, "runtime_identity_mismatch", "The authenticated runtime does not match the request.", "已认证的运行时与请求不匹配。")
}

func minTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}
