package networkcontrol

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net/url"
	"testing"
	"time"

	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	domainnetworkruntime "github.com/opensoha/soha/internal/domain/networkruntime"
	domainruntimeconfig "github.com/opensoha/soha/internal/domain/runtimeconfig"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

type controlStoreStub struct {
	snapshot           domainnetworkruntime.PolicySnapshot
	consumption        domainnetworkruntime.EnrollmentConsumption
	credential         domainnetworkruntime.Credential
	config             domainnetworkruntime.Configuration
	revoke             networkprotocol.LeaseRevoke
	nasBinding         domainnetworkaccess.NASBinding
	subject            domainnetworkaccess.Subject
	device             domainnetworkaccess.Device
	site               domainnetworkaccess.Site
	profile            domainnetworkaccess.SiteProfileBinding
	authorized         domainnetworkruntime.NASAuthorization
	subjectErr         error
	deviceErr          error
	nextCommand        domainnetworkaccess.SessionCommand
	commandResult      networkprotocol.NASSessionCommandResult
	credentials        map[string]domainnetworkruntime.Credential
	credentialErr      map[string]error
	space              domainnetworkaccess.Space
	resource           domainnetworkaccess.Resource
	gateway            domainnetworkaccess.Gateway
	gatewayCred        domainnetworkruntime.Credential
	vpnSaved           domainnetworkruntime.VPNConnection
	vpnResult          networkprotocol.VPNConnectResult
	vpnGatewayErr      error
	gatewaySiteID      string
	requestedGatewayID string
	mihomoCipher       string
	mihomoSourceType   string
	mihomoRev          int
	mihomoCredID       string
	mihomoID           string
	mihomoErr          error
}

func (store *controlStoreStub) LatestSnapshot(context.Context) (domainnetworkruntime.PolicySnapshot, error) {
	return store.snapshot, nil
}

func (store *controlStoreStub) ConsumeEnrollment(_ context.Context, consumption domainnetworkruntime.EnrollmentConsumption, _ domainnetworkruntime.PolicySnapshot, _ time.Time) (domainnetworkruntime.Credential, domainnetworkruntime.Configuration, error) {
	store.consumption = consumption
	return store.credential, store.config, nil
}

func (store *controlStoreStub) ActiveCredential(_ context.Context, fingerprint string, _ time.Time) (domainnetworkruntime.Credential, error) {
	if err, ok := store.credentialErr[fingerprint]; ok {
		return domainnetworkruntime.Credential{}, err
	}
	if credential, ok := store.credentials[fingerprint]; ok {
		return credential, nil
	}
	return store.credential, nil
}

func (store *controlStoreStub) ActiveEndpointCredential(_ context.Context, serial, authorityKeyID string, _ time.Time) (domainnetworkruntime.Credential, error) {
	key := serial + "\n" + authorityKeyID
	if err, ok := store.credentialErr[key]; ok {
		return domainnetworkruntime.Credential{}, err
	}
	if credential, ok := store.credentials[key]; ok {
		return credential, nil
	}
	return domainnetworkruntime.Credential{}, apperrors.ErrUnauthorized
}

func (store *controlStoreStub) TouchCredential(context.Context, string, time.Time) error { return nil }

func (store *controlStoreStub) EnsureConfiguration(context.Context, string, domainnetworkruntime.PolicySnapshot, time.Time, time.Time) (domainnetworkruntime.Configuration, error) {
	return store.config, nil
}

func (store *controlStoreStub) ApplyConfiguration(context.Context, string, networkprotocol.ConfigurationApplied, time.Time) error {
	return nil
}

func (store *controlStoreStub) RenewLeases(context.Context, string, networkprotocol.LeaseRenewRequest, domainnetworkruntime.PolicySnapshot, time.Time, time.Duration) (networkprotocol.LeaseRenewResult, error) {
	return networkprotocol.LeaseRenewResult{}, nil
}

func (store *controlStoreStub) RevokeLeases(_ context.Context, _ string, revoke networkprotocol.LeaseRevoke, _ domainnetworkruntime.PolicySnapshot, _, _ time.Time) (int64, error) {
	store.revoke = revoke
	return 1, nil
}

func (store *controlStoreStub) NASBinding(context.Context, string, string) (domainnetworkaccess.NASBinding, error) {
	return store.nasBinding, nil
}

func (store *controlStoreStub) NetworkSubject(context.Context, string) (domainnetworkaccess.Subject, error) {
	return store.subject, store.subjectErr
}

func (store *controlStoreStub) NetworkDevice(context.Context, string) (domainnetworkaccess.Device, error) {
	return store.device, store.deviceErr
}

func (store *controlStoreStub) NetworkSite(context.Context, string) (domainnetworkaccess.Site, error) {
	return store.site, nil
}

func (store *controlStoreStub) SiteProfileBinding(context.Context, string, string) (domainnetworkaccess.SiteProfileBinding, error) {
	return store.profile, nil
}

func (store *controlStoreStub) SaveNASAuthorization(_ context.Context, authorization domainnetworkruntime.NASAuthorization) (domainnetworkruntime.NASAuthorization, error) {
	store.authorized = authorization
	return authorization, nil
}

func (store *controlStoreStub) ClaimNASSessionCommand(context.Context, string, time.Time) (domainnetworkaccess.SessionCommand, error) {
	return store.nextCommand, nil
}

func (store *controlStoreStub) CompleteNASSessionCommand(_ context.Context, _ string, result networkprotocol.NASSessionCommandResult, _ time.Time) error {
	store.commandResult = result
	return nil
}

func (store *controlStoreStub) NetworkSpace(context.Context, string) (domainnetworkaccess.Space, error) {
	return store.space, nil
}

func (store *controlStoreStub) NetworkResource(_ context.Context, id string) (domainnetworkaccess.Resource, error) {
	if store.resource.ID != id {
		return domainnetworkaccess.Resource{}, apperrors.ErrNotFound
	}
	return store.resource, nil
}

func (store *controlStoreStub) ActiveVPNGateway(_ context.Context, siteID, gatewayID string, _ time.Time) (domainnetworkaccess.Gateway, domainnetworkruntime.Credential, error) {
	store.gatewaySiteID, store.requestedGatewayID = siteID, gatewayID
	return store.gateway, store.gatewayCred, store.vpnGatewayErr
}

func (store *controlStoreStub) SaveVPNConnection(_ context.Context, connection domainnetworkruntime.VPNConnection, _ domainnetworkruntime.PolicySnapshot) (networkprotocol.VPNConnectResult, error) {
	store.vpnSaved = connection
	return store.vpnResult, nil
}

func (store *controlStoreStub) MihomoSource(_ context.Context, credentialID, profileID string, _ time.Time) (string, string, int, error) {
	store.mihomoCredID, store.mihomoID = credentialID, profileID
	sourceType := store.mihomoSourceType
	if sourceType == "" {
		sourceType = domainnetworkaccess.MihomoSourceManagedSubscription
	}
	return sourceType, store.mihomoCipher, store.mihomoRev, store.mihomoErr
}

func TestServiceMihomoSubscriptionIsCredentialBoundAndEncrypted(t *testing.T) {
	now := time.Date(2026, 9, 3, 10, 0, 0, 0, time.UTC)
	identity, _ := testIdentity(t, now)
	key, err := keyring.NewKey("test-key", "test-secret-with-more-than-32-characters", time.Unix(0, 0), nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	ciphertext, err := secretcrypto.EncryptStringWithKeyring(keys, "https://subscriptions.example.test/mihomo?id=endpoint-1")
	if err != nil {
		t.Fatal(err)
	}
	store := &controlStoreStub{
		credential:       domainnetworkruntime.Credential{ID: "credential-1", RuntimeID: identity.ID, RuntimeKind: identity.Kind, DeviceID: "device-1", Capabilities: []string{"mihomo"}, ExpiresAt: now.Add(time.Hour)},
		mihomoSourceType: domainnetworkaccess.MihomoSourceManagedSubscription, mihomoCipher: ciphertext, mihomoRev: 4,
	}
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(store, schemas, Options{MaxClockSkew: 5 * time.Minute, ConfigurationTTL: 5 * time.Minute, LeaseTTL: 5 * time.Minute, CredentialEncryptionKeys: keys})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }

	got, err := service.MihomoSubscription(context.Background(), identity, "mihomo-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ProfileID != "mihomo-1" || got.ProfileRevision != 4 || got.SubscriptionURL != "https://subscriptions.example.test/mihomo?id=endpoint-1" || store.mihomoCredID != "credential-1" || store.mihomoID != "mihomo-1" {
		t.Fatalf("MihomoSubscription() = %#v, store credential/profile = %q/%q", got, store.mihomoCredID, store.mihomoID)
	}
	source, err := service.MihomoSource(context.Background(), identity, "mihomo-1")
	if err != nil || source.SourceType != domainnetworkaccess.MihomoSourceManagedSubscription || source.SubscriptionURL != got.SubscriptionURL {
		t.Fatalf("MihomoSource() = %#v, %v", source, err)
	}
	checkManualAndInvalidMihomoSource(t, service, store, identity, keys)
}

func TestServiceEnrollBindsChallengeCertificateAndSnapshot(t *testing.T) {
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	identity, publicKey := testIdentity(t, now)
	desired := networkprotocol.ConfigurationDesired{
		ConfigurationVersion: 1, PolicyVersion: 7, ValidUntil: now.Add(5 * time.Minute), AccessProfile: "onboarding",
		ProtectedResourceIDs: []string{"resource-1"}, NetworkLeases: []networkprotocol.NetworkLease{}, ResourceLeases: []networkprotocol.ResourceLease{},
	}
	store := &controlStoreStub{
		snapshot:   domainnetworkruntime.PolicySnapshot{PolicyVersion: 7, ContentHash: "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ProtectedResourceIDs: []string{"resource-1"}},
		credential: domainnetworkruntime.Credential{ID: "credential-1", RuntimeID: "endpoint-1", RuntimeKind: "endpoint", ExpiresAt: now.Add(time.Hour)},
		config:     domainnetworkruntime.Configuration{RuntimeID: "endpoint-1", ConfigurationVersion: 1, PolicyVersion: 7, Desired: desired, ValidUntil: desired.ValidUntil},
	}
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatalf("CompileSchemas() error = %v", err)
	}
	service, err := New(store, schemas, Options{MaxClockSkew: 5 * time.Minute, ConfigurationTTL: 5 * time.Minute, LeaseTTL: 5 * time.Minute})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	service.now = func() time.Time { return now }
	if err := service.RefreshSnapshot(context.Background()); err != nil {
		t.Fatalf("RefreshSnapshot() error = %v", err)
	}
	if !service.Ready() {
		t.Fatal("service not ready after snapshot refresh")
	}

	payload := networkprotocol.EnrollmentRequest{
		EnrollmentID: "enrollment-1", ChallengeID: "challenge-1", DeviceID: "device-1",
		DevicePublicKey: publicKey, WireGuardPublicKey: "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=", Platform: "windows", ClientVersion: "1.0.0", Capabilities: []string{"wireguard"},
	}
	raw := runtimeMessageJSON(t, now, identity, networkprotocol.MessageEnrollmentRequest, payload)
	token := "one-time-token-12345678901234567890"
	result, err := service.Enroll(context.Background(), identity, token, raw)
	if err != nil {
		t.Fatalf("Enroll() error = %v", err)
	}
	if result.MessageType != networkprotocol.MessageEnrollmentResult {
		t.Fatalf("result message type = %q", result.MessageType)
	}
	if store.consumption.RuntimeID != identity.ID || store.consumption.TokenHash == "" || store.consumption.CertificateFingerprint != identity.CertificateFingerprint || store.consumption.WireGuardPublicKey != payload.WireGuardPublicKey || store.consumption.CertificateSerial != "1" || store.consumption.CertificateAuthorityKeyID != "0102030405060708" {
		t.Fatalf("consumption = %#v", store.consumption)
	}

	mismatched := identity
	mismatched.ID = "endpoint-2"
	_, err = service.Enroll(context.Background(), mismatched, token, raw)
	if !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("mismatched enrollment error = %v, want unauthorized", err)
	}
}

func TestServiceConfigurationIncludesRuntimeIntervals(t *testing.T) {
	now := time.Date(2026, 9, 4, 8, 0, 0, 0, time.UTC)
	identity, _ := testIdentity(t, now)
	desired := networkprotocol.ConfigurationDesired{
		ConfigurationVersion: 1, PolicyVersion: 7, ValidUntil: now.Add(5 * time.Minute), AccessProfile: "full",
		ProtectedResourceIDs: []string{}, NetworkLeases: []networkprotocol.NetworkLease{}, ResourceLeases: []networkprotocol.ResourceLease{},
	}
	for _, test := range []struct {
		name      string
		overrides map[string]any
		want      networkprotocol.RuntimeIntervals
	}{
		{name: "defaults", overrides: map[string]any{}, want: networkprotocol.RuntimeIntervals{HeartbeatIntervalSeconds: 60, ConfigurationPollIntervalSeconds: 60}},
		{name: "configured", overrides: map[string]any{"network.runtime.heartbeat_interval_seconds": float64(90), "network.runtime.configuration_poll_interval_seconds": int64(120)}, want: networkprotocol.RuntimeIntervals{HeartbeatIntervalSeconds: 90, ConfigurationPollIntervalSeconds: 120}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &controlStoreStub{
				snapshot:   domainnetworkruntime.PolicySnapshot{PolicyVersion: 7},
				credential: domainnetworkruntime.Credential{ID: "credential-1", RuntimeID: identity.ID, RuntimeKind: identity.Kind, ExpiresAt: now.Add(time.Hour)},
				config:     domainnetworkruntime.Configuration{RuntimeID: identity.ID, ConfigurationVersion: 1, PolicyVersion: 7, Desired: desired, ValidUntil: desired.ValidUntil},
			}
			schemas, err := networkprotocol.CompileSchemas()
			if err != nil {
				t.Fatal(err)
			}
			service, err := New(store, schemas, Options{
				MaxClockSkew: 5 * time.Minute, ConfigurationTTL: 5 * time.Minute, LeaseTTL: 5 * time.Minute,
				LoadRuntimeConfig: func(context.Context) (domainruntimeconfig.State, error) {
					return domainruntimeconfig.State{Overrides: test.overrides}, nil
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			service.now = func() time.Time { return now }
			if err := service.RefreshSnapshot(context.Background()); err != nil {
				t.Fatal(err)
			}
			message, err := service.Configuration(context.Background(), identity)
			if err != nil {
				t.Fatal(err)
			}
			got, err := networkprotocol.DecodePayload[networkprotocol.ConfigurationDesired](message.Payload)
			if err != nil {
				t.Fatal(err)
			}
			if got.RuntimeIntervals == nil || *got.RuntimeIntervals != test.want {
				t.Fatalf("runtime intervals = %#v, want %#v", got.RuntimeIntervals, test.want)
			}
		})
	}
}

func TestServiceRejectsRevokeBeyondAuthenticatedMessageWindow(t *testing.T) {
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	identity, _ := testIdentity(t, now)
	store := &controlStoreStub{credential: domainnetworkruntime.Credential{
		ID: "credential-1", RuntimeID: identity.ID, RuntimeKind: identity.Kind, ExpiresAt: now.Add(time.Hour),
	}}
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatalf("CompileSchemas() error = %v", err)
	}
	service, err := New(store, schemas, Options{MaxClockSkew: 5 * time.Minute, ConfigurationTTL: 5 * time.Minute, LeaseTTL: 5 * time.Minute})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	service.now = func() time.Time { return now }

	payload := networkprotocol.LeaseRevoke{SessionID: "session-1", LeaseIDs: []string{"lease-1"}, ReasonCode: "policy_changed", EffectiveAt: now.Add(6 * time.Minute)}
	_, err = service.Revoke(context.Background(), identity, runtimeMessageJSON(t, now, identity, networkprotocol.MessageLeaseRevoke, payload))
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("Revoke() error = %v, want invalid argument", err)
	}
	if store.revoke.SessionID != "" {
		t.Fatalf("future revoke reached store: %#v", store.revoke)
	}
}

func TestServiceConnectVPNUsesCredentialIdentityAndLiveGateway(t *testing.T) {
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	identity, _ := testIdentity(t, now)
	lastGatewayPoll := now.Add(-time.Minute)
	policies, err := json.Marshal([]domainnetworkaccess.Policy{
		{
			ID: "vpn-wide", Enabled: true, Priority: 100, Effect: domainnetworkaccess.PolicyEffectAllow,
			Subjects: domainnetworkaccess.PolicySubjects{Teams: []string{"engineering"}}, SiteIDs: []string{"site-hq"},
			Modes: []string{domainnetworkaccess.ModeExternalVPN}, DeviceStatuses: []string{domainnetworkaccess.DeviceStatusActive},
			PostureStatuses: []string{domainnetworkaccess.PostureCompliant}, AccessProfile: domainnetworkaccess.ProfileFull,
		},
		{
			ID: "resource-db", Enabled: true, Priority: 100, Effect: domainnetworkaccess.PolicyEffectAllow,
			Subjects: domainnetworkaccess.PolicySubjects{Teams: []string{"engineering"}}, SiteIDs: []string{"site-hq"}, ResourceIDs: []string{"resource-db"},
			Modes: []string{domainnetworkaccess.ModeInternalZTNA, domainnetworkaccess.ModeExternalVPNZTNA, domainnetworkaccess.ModeExternalDirectZTNA}, DeviceStatuses: []string{domainnetworkaccess.DeviceStatusActive},
			PostureStatuses: []string{domainnetworkaccess.PostureCompliant}, AccessProfile: domainnetworkaccess.ProfileFull,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	store := &controlStoreStub{
		snapshot:    domainnetworkruntime.PolicySnapshot{PolicyVersion: 7, Policies: policies},
		credential:  domainnetworkruntime.Credential{ID: "endpoint-credential", RuntimeID: identity.ID, RuntimeKind: "endpoint", DeviceID: "device-1", SubjectID: "user-1", WireGuardPublicKey: "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=", Capabilities: []string{"wireguard"}, ExpiresAt: now.Add(time.Hour)},
		subject:     domainnetworkaccess.Subject{UserID: "user-1", Status: domainnetworkaccess.StatusActive, Teams: []string{"engineering"}},
		device:      domainnetworkaccess.Device{ID: "device-1", OwnerUserID: "user-1", SiteID: "site-hq", Status: domainnetworkaccess.DeviceStatusActive, PostureStatus: domainnetworkaccess.PostureCompliant, PostureVersion: 5},
		site:        domainnetworkaccess.Site{ID: "site-hq", Status: domainnetworkaccess.StatusActive},
		space:       domainnetworkaccess.Space{ID: "space-hq", SiteID: "site-hq", CIDRs: []string{"10.20.0.0/16"}, Status: domainnetworkaccess.StatusActive},
		resource:    domainnetworkaccess.Resource{ID: "resource-db", SpaceID: "space-hq", Kind: "ip", Target: "10.20.8.10", Protocol: "tcp", Ports: []int{5432}, Protected: true, PathMode: domainnetworkaccess.PathAutomatic},
		gateway:     domainnetworkaccess.Gateway{ID: "gateway-branch", RuntimeID: "gateway-runtime", SiteID: "site-branch", HubGatewayID: "gateway-hq", AdministrativeStatus: domainnetworkaccess.StatusActive, WireGuardPublicKey: "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC="},
		gatewayCred: domainnetworkruntime.Credential{ID: "gateway-credential", RuntimeID: "gateway-runtime", RuntimeKind: "gateway", WireGuardPublicKey: "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC=", Capabilities: []string{"wireguard"}, LastControlSeenAt: &lastGatewayPoll, ExpiresAt: now.Add(time.Hour)},
		vpnResult:   networkprotocol.VPNConnectResult{RequestID: "vpn-request-1", Decision: domainnetworkaccess.DecisionAllow, ReasonCode: "policy_allowed", SessionID: "vpn-session-1", GatewayID: "gateway-branch", ConfigurationVersion: 2, PolicyVersion: 7, ValidUntil: now.Add(5 * time.Minute), NetworkLeases: []networkprotocol.NetworkLease{{ID: "lease-1", SessionID: "vpn-session-1", SubjectID: "user-1", DeviceID: "device-1", NetworkSpaceID: "space-hq", CIDRs: []string{"10.20.0.0/16"}, PolicyVersion: 7, IssuedAt: now, ExpiresAt: now.Add(5 * time.Minute)}}, ResourceLeases: []networkprotocol.ResourceLease{}},
	}
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(store, schemas, Options{MaxClockSkew: 5 * time.Minute, ConfigurationTTL: 5 * time.Minute, LeaseTTL: 5 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	service.now = func() time.Time { return now }
	if err := service.RefreshSnapshot(context.Background()); err != nil {
		t.Fatal(err)
	}
	request := networkprotocol.VPNConnectRequest{RequestID: "vpn-request-1", SiteID: "site-hq", NetworkSpaceID: "space-hq", GatewayID: "gateway-branch", Mode: domainnetworkaccess.ModeExternalVPN}
	message, err := service.ConnectVPN(context.Background(), identity, runtimeMessageJSON(t, now, identity, networkprotocol.MessageVPNConnectRequest, request))
	if err != nil {
		t.Fatalf("ConnectVPN() error = %v", err)
	}
	result, err := networkprotocol.DecodePayload[networkprotocol.VPNConnectResult](message.Payload)
	if err != nil || result.Decision != domainnetworkaccess.DecisionAllow || result.ConfigurationVersion != 2 {
		t.Fatalf("ConnectVPN() result = %#v, %v", result, err)
	}
	checkSavedVPNIdentity(t, store)

	stale := now.Add(-11 * time.Minute)
	store.gatewayCred.LastControlSeenAt = &stale
	store.vpnResult = networkprotocol.VPNConnectResult{RequestID: "vpn-request-2", Decision: domainnetworkaccess.DecisionDeny, ReasonCode: "gateway_unavailable", PolicyVersion: 7, ValidUntil: now.Add(5 * time.Minute), NetworkLeases: []networkprotocol.NetworkLease{}, ResourceLeases: []networkprotocol.ResourceLease{}}
	request.RequestID = "vpn-request-2"
	message, err = service.ConnectVPN(context.Background(), identity, runtimeMessageJSON(t, now, identity, networkprotocol.MessageVPNConnectRequest, request))
	if err != nil {
		t.Fatalf("ConnectVPN(stale gateway) error = %v", err)
	}
	result, err = networkprotocol.DecodePayload[networkprotocol.VPNConnectResult](message.Payload)
	if err != nil || result.Decision != domainnetworkaccess.DecisionDeny || store.vpnSaved.ReasonCode != "gateway_unavailable" {
		t.Fatalf("stale gateway result/saved = %#v / %#v / %v", result, store.vpnSaved, err)
	}

	store.gatewayCred.LastControlSeenAt = &lastGatewayPoll
	checkZTNAConnectionModes(t, service, store, identity, now)
}

func TestServiceSnapshotRequiresMatchingActiveCredential(t *testing.T) {
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	identity, _ := testIdentity(t, now)
	store := &controlStoreStub{
		snapshot:   domainnetworkruntime.PolicySnapshot{PolicyVersion: 7},
		credential: domainnetworkruntime.Credential{ID: "credential-1", RuntimeID: identity.ID, RuntimeKind: identity.Kind, ExpiresAt: now.Add(time.Hour)},
	}
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatalf("CompileSchemas() error = %v", err)
	}
	service, err := New(store, schemas, Options{MaxClockSkew: 5 * time.Minute, ConfigurationTTL: 5 * time.Minute, LeaseTTL: 5 * time.Minute})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	service.now = func() time.Time { return now }
	if err := service.RefreshSnapshot(context.Background()); err != nil {
		t.Fatalf("RefreshSnapshot() error = %v", err)
	}
	if snapshot, err := service.Snapshot(context.Background(), identity); err != nil || snapshot.PolicyVersion != 7 {
		t.Fatalf("Snapshot() = %#v, %v", snapshot, err)
	}

	mismatched := identity
	mismatched.ID = "endpoint-2"
	if _, err := service.Snapshot(context.Background(), mismatched); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("mismatched Snapshot() error = %v, want unauthorized", err)
	}
}

func TestServiceAuthorizesNASFromPublishedNetworkPolicy(t *testing.T) {
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	identity := testRuntimeIdentity(t, now, "nas", "freeradius-hq")
	policies, err := json.Marshal([]domainnetworkaccess.Policy{{ID: "network-wide", Enabled: true, Priority: 100, Effect: domainnetworkaccess.PolicyEffectAllow, Subjects: domainnetworkaccess.PolicySubjects{Teams: []string{"engineering"}}, SiteIDs: []string{"site-hq"}, Modes: []string{domainnetworkaccess.ModeInternalDirect}, DeviceStatuses: []string{domainnetworkaccess.DeviceStatusActive}, PostureStatuses: []string{domainnetworkaccess.PostureCompliant}, AccessProfile: domainnetworkaccess.ProfileFull}})
	if err != nil {
		t.Fatalf("marshal policies: %v", err)
	}
	clientSerial := "753484ff6e444ae9d86814989e85cb2fdd462193"
	clientAuthorityKeyID := "e9000cae780f994507ff4db049e42084c75a7aba"
	clientBindingKey := clientSerial + "\n" + clientAuthorityKeyID
	store := &controlStoreStub{
		snapshot:   domainnetworkruntime.PolicySnapshot{PolicyVersion: 7, Policies: policies},
		credential: domainnetworkruntime.Credential{ID: "credential-1", RuntimeID: identity.ID, RuntimeKind: identity.Kind, ExpiresAt: now.Add(time.Hour)},
		nasBinding: domainnetworkaccess.NASBinding{NASID: "nas-hq-wifi", RuntimeID: identity.ID, SiteID: "site-hq", Status: domainnetworkaccess.StatusActive},
		subject:    domainnetworkaccess.Subject{UserID: "user-1", Status: domainnetworkaccess.StatusActive, Teams: []string{"engineering"}},
		device:     domainnetworkaccess.Device{ID: "device-1", OwnerUserID: "user-1", SiteID: "site-hq", Status: domainnetworkaccess.DeviceStatusActive, PostureStatus: domainnetworkaccess.PostureCompliant},
		site:       domainnetworkaccess.Site{ID: "site-hq", Status: domainnetworkaccess.StatusActive},
		profile:    domainnetworkaccess.SiteProfileBinding{SiteID: "site-hq", AccessProfile: domainnetworkaccess.ProfileFull, VLANID: 20, FilterID: "soha-full", SessionTimeoutSeconds: 3600},
		credentials: map[string]domainnetworkruntime.Credential{
			clientBindingKey: {RuntimeKind: "endpoint", SubjectID: "user-1", DeviceID: "device-1", CertificateSerial: clientSerial, CertificateAuthorityKeyID: clientAuthorityKeyID},
		},
		credentialErr: map[string]error{},
	}
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatalf("CompileSchemas() error = %v", err)
	}
	service, err := New(store, schemas, Options{MaxClockSkew: 5 * time.Minute, ConfigurationTTL: 5 * time.Minute, LeaseTTL: 5 * time.Minute})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	service.now = func() time.Time { return now }
	if err := service.RefreshSnapshot(context.Background()); err != nil {
		t.Fatalf("RefreshSnapshot() error = %v", err)
	}
	request := networkprotocol.NASAuthorizationRequest{RequestID: "radius-request-1", NASID: "nas-hq-wifi", SubjectID: "user-1", StationID: "AA-BB-CC-DD-EE-FF", AuthenticationMethod: "eap-tls", ClientCertificateSerial: clientSerial, ClientCertificateAuthorityKeyID: "E9:00:0C:AE:78:0F:99:45:07:FF:4D:B0:49:E4:20:84:C7:5A:7A:BA"}
	raw := runtimeMessageJSON(t, now, identity, networkprotocol.MessageNASAuthorizationRequest, request)
	message, err := service.AuthorizeNAS(context.Background(), identity, raw)
	if err != nil {
		t.Fatalf("AuthorizeNAS() error = %v", err)
	}
	result, err := networkprotocol.DecodePayload[networkprotocol.NASAuthorizationResult](message.Payload)
	if err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Decision != domainnetworkaccess.DecisionAllow || result.AccessProfile != domainnetworkaccess.ProfileFull || result.RadiusAttributes == nil || result.RadiusAttributes.VLANID != 20 || result.RadiusAttributes.FilterID != "soha-full" || result.RadiusAttributes.SessionTimeoutSeconds != 3600 {
		t.Fatalf("authorization result = %#v", result)
	}
	if store.authorized.SessionID == "" || store.authorized.RuntimeID != identity.ID || store.authorized.PolicyVersion != 7 {
		t.Fatalf("persisted authorization = %#v", store.authorized)
	}

	request.RequestID = "radius-request-mismatched-subject"
	request.SubjectID = "user-2"
	if _, err := service.AuthorizeNAS(context.Background(), identity, runtimeMessageJSON(t, now, identity, networkprotocol.MessageNASAuthorizationRequest, request)); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("AuthorizeNAS(mismatched EAP-TLS credential) error = %v, want unauthorized", err)
	}
	request.SubjectID = "user-1"
	store.credentialErr[clientBindingKey] = apperrors.ErrUnauthorized
	request.RequestID = "radius-request-revoked-certificate"
	if _, err := service.AuthorizeNAS(context.Background(), identity, runtimeMessageJSON(t, now, identity, networkprotocol.MessageNASAuthorizationRequest, request)); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("AuthorizeNAS(inactive EAP-TLS credential) error = %v, want unauthorized", err)
	}
	delete(store.credentialErr, clientBindingKey)

	checkNASPasswordAndDenials(t, service, store, identity, now, request)
}

func TestServiceMapsEveryNACAccessProfile(t *testing.T) {
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	identity := testRuntimeIdentity(t, now, "nas", "freeradius-hq")
	serial := "753484ff6e444ae9d86814989e85cb2fdd462193"
	authorityKeyID := "e9000cae780f994507ff4db049e42084c75a7aba"
	for index, profile := range []string{
		domainnetworkaccess.ProfileOnboarding,
		domainnetworkaccess.ProfileFull,
		domainnetworkaccess.ProfileRestricted,
		domainnetworkaccess.ProfileQuarantine,
		domainnetworkaccess.ProfileDeny,
	} {
		t.Run(profile, func(t *testing.T) {
			effect := domainnetworkaccess.PolicyEffectAllow
			if profile == domainnetworkaccess.ProfileDeny {
				effect = domainnetworkaccess.PolicyEffectDeny
			}
			policies, err := json.Marshal([]domainnetworkaccess.Policy{{
				ID: "profile-" + profile, Enabled: true, Priority: 100, Effect: effect,
				Subjects: domainnetworkaccess.PolicySubjects{Teams: []string{"engineering"}},
				SiteIDs:  []string{"site-hq"}, Modes: []string{domainnetworkaccess.ModeInternalDirect},
				DeviceStatuses: []string{domainnetworkaccess.DeviceStatusActive}, PostureStatuses: []string{domainnetworkaccess.PostureCompliant},
				AccessProfile: profile,
			}})
			if err != nil {
				t.Fatal(err)
			}
			store := &controlStoreStub{
				snapshot:   domainnetworkruntime.PolicySnapshot{PolicyVersion: 7, Policies: policies},
				credential: domainnetworkruntime.Credential{ID: "credential-1", RuntimeID: identity.ID, RuntimeKind: identity.Kind, ExpiresAt: now.Add(time.Hour)},
				nasBinding: domainnetworkaccess.NASBinding{NASID: "nas-hq", RuntimeID: identity.ID, SiteID: "site-hq", Status: domainnetworkaccess.StatusActive},
				subject:    domainnetworkaccess.Subject{UserID: "user-1", Status: domainnetworkaccess.StatusActive, Teams: []string{"engineering"}},
				device:     domainnetworkaccess.Device{ID: "device-1", OwnerUserID: "user-1", SiteID: "site-hq", Status: domainnetworkaccess.DeviceStatusActive, PostureStatus: domainnetworkaccess.PostureCompliant},
				site:       domainnetworkaccess.Site{ID: "site-hq", Status: domainnetworkaccess.StatusActive},
				profile: domainnetworkaccess.SiteProfileBinding{
					SiteID: "site-hq", AccessProfile: profile, VLANID: 10 + index*10,
					FilterID: "soha-" + profile, SessionTimeoutSeconds: 900,
				},
				credentials: map[string]domainnetworkruntime.Credential{
					serial + "\n" + authorityKeyID: {RuntimeKind: "endpoint", SubjectID: "user-1", DeviceID: "device-1"},
				},
				credentialErr: map[string]error{},
			}
			schemas, err := networkprotocol.CompileSchemas()
			if err != nil {
				t.Fatal(err)
			}
			service, err := New(store, schemas, Options{MaxClockSkew: 5 * time.Minute, ConfigurationTTL: 5 * time.Minute, LeaseTTL: 5 * time.Minute})
			if err != nil {
				t.Fatal(err)
			}
			service.now = func() time.Time { return now }
			if err := service.RefreshSnapshot(context.Background()); err != nil {
				t.Fatal(err)
			}
			request := networkprotocol.NASAuthorizationRequest{
				RequestID: "request-" + profile, NASID: "nas-hq", SubjectID: "user-1", StationID: "aa:bb:cc:dd:ee:ff",
				AuthenticationMethod:    networkprotocol.NASAuthenticationEAPTLS,
				ClientCertificateSerial: serial, ClientCertificateAuthorityKeyID: authorityKeyID,
			}
			message, err := service.AuthorizeNAS(context.Background(), identity, runtimeMessageJSON(t, now, identity, networkprotocol.MessageNASAuthorizationRequest, request))
			if err != nil {
				t.Fatal(err)
			}
			result, err := networkprotocol.DecodePayload[networkprotocol.NASAuthorizationResult](message.Payload)
			if err != nil {
				t.Fatal(err)
			}
			if profile == domainnetworkaccess.ProfileDeny {
				if result.Decision != domainnetworkaccess.DecisionDeny || result.AccessProfile != profile || result.RadiusAttributes != nil {
					t.Fatalf("deny result = %#v", result)
				}
				return
			}
			if result.Decision != domainnetworkaccess.DecisionAllow || result.AccessProfile != profile || result.RadiusAttributes == nil || result.RadiusAttributes.VLANID != 10+index*10 || result.RadiusAttributes.FilterID != "soha-"+profile {
				t.Fatalf("%s result = %#v", profile, result)
			}
		})
	}
}

func TestServiceDeliversAndCompletesNASSessionCommand(t *testing.T) {
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	identity := testRuntimeIdentity(t, now, "nas", "freeradius-hq")
	store := &controlStoreStub{
		credential: domainnetworkruntime.Credential{ID: "credential-1", RuntimeID: identity.ID, RuntimeKind: identity.Kind, ExpiresAt: now.Add(time.Hour)},
		nextCommand: domainnetworkaccess.SessionCommand{
			ID: "command-1", SessionID: "session-1", RuntimeID: identity.ID, NASID: "nas-hq", Action: domainnetworkaccess.SessionActionCoA,
			SubjectID: "user-1", DeviceID: "device-1",
			TargetAccessProfile: domainnetworkaccess.ProfileRestricted, PolicyVersion: 7, Status: domainnetworkaccess.SessionCommandDelivered,
			ReasonCode: "risk_changed", RadiusAttributes: &networkprotocol.RadiusAttributes{VLANID: 30, FilterID: "soha-restricted", SessionTimeoutSeconds: 900},
			EffectiveAt: now, ExpiresAt: now.Add(5 * time.Minute), CreatedAt: now,
		},
	}
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatalf("CompileSchemas() error = %v", err)
	}
	service, err := New(store, schemas, Options{MaxClockSkew: 5 * time.Minute, ConfigurationTTL: 5 * time.Minute, LeaseTTL: 5 * time.Minute})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	service.now = func() time.Time { return now }

	message, err := service.NextNASSessionCommand(context.Background(), identity)
	if err != nil {
		t.Fatalf("NextNASSessionCommand() error = %v", err)
	}
	payload, err := networkprotocol.DecodePayload[networkprotocol.NASSessionCommand](message.Payload)
	if err != nil || message.MessageType != networkprotocol.MessageNASSessionCommand || payload.CommandID != "command-1" || payload.SubjectID != "user-1" || payload.DeviceID != "device-1" || payload.RadiusAttributes == nil || payload.RadiusAttributes.VLANID != 30 {
		t.Fatalf("NextNASSessionCommand() = %#v, payload %#v, %v", message, payload, err)
	}

	result := networkprotocol.NASSessionCommandResult{CommandID: "command-1", SessionID: "session-1", Status: domainnetworkaccess.SessionCommandApplied, ReasonCode: "coa_applied", CompletedAt: now.Add(time.Second)}
	raw := runtimeMessageJSON(t, now, identity, networkprotocol.MessageNASSessionCommandResult, result)
	if err := service.CompleteNASSessionCommand(context.Background(), identity, raw); err != nil {
		t.Fatalf("CompleteNASSessionCommand() error = %v", err)
	}
	if store.commandResult != result {
		t.Fatalf("persisted command result = %#v", store.commandResult)
	}

	endpoint := testRuntimeIdentity(t, now, "endpoint", "endpoint-1")
	if _, err := service.NextNASSessionCommand(context.Background(), endpoint); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("endpoint NextNASSessionCommand() error = %v, want unauthorized", err)
	}
}

func testIdentity(t *testing.T, now time.Time) (networkidentity.Identity, string) {
	t.Helper()
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	encoded, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey() error = %v", err)
	}
	identityURI, _ := url.Parse("spiffe://opensoha.local/network-control/endpoint/endpoint-1")
	certificate := &x509.Certificate{
		Raw: []byte("certificate"), RawSubjectPublicKeyInfo: encoded, PublicKey: publicKey, URIs: []*url.URL{identityURI},
		SerialNumber: big.NewInt(1), AuthorityKeyId: []byte{1, 2, 3, 4, 5, 6, 7, 8},
		NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
	}
	identity, err := networkidentity.ParseCertificate(certificate, networkidentity.ScopeNetworkControl)
	if err != nil {
		t.Fatalf("ParseCertificate() error = %v", err)
	}
	return identity, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: encoded}))
}

func testRuntimeIdentity(t *testing.T, now time.Time, kind, id string) networkidentity.Identity {
	t.Helper()
	identityURI, err := url.Parse("spiffe://opensoha.local/network-control/" + kind + "/" + id)
	if err != nil {
		t.Fatalf("parse identity URI: %v", err)
	}
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	encoded, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey() error = %v", err)
	}
	certificate := &x509.Certificate{Raw: []byte("certificate-" + id), RawSubjectPublicKeyInfo: encoded, PublicKey: publicKey, URIs: []*url.URL{identityURI}, SerialNumber: big.NewInt(2), NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour)}
	identity, err := networkidentity.ParseCertificate(certificate, networkidentity.ScopeNetworkControl)
	if err != nil {
		t.Fatalf("ParseCertificate() error = %v", err)
	}
	return identity
}

func runtimeMessageJSON(t *testing.T, now time.Time, identity networkidentity.Identity, messageType string, payload any) []byte {
	t.Helper()
	encodedPayload, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	encoded, err := json.Marshal(networkprotocol.RuntimeMessage{
		SchemaVersion: networkprotocol.RuntimeSchemaVersion, MessageID: "message-1", MessageType: messageType,
		ProducerID: identity.ID, RuntimeID: identity.ID, RuntimeKind: identity.Kind,
		OccurredAt: now, ExpiresAt: now.Add(5 * time.Minute), Payload: encodedPayload,
	})
	if err != nil {
		t.Fatalf("marshal runtime message: %v", err)
	}
	return encoded
}

func checkManualAndInvalidMihomoSource(t *testing.T, service *Service, store *controlStoreStub, identity networkidentity.Identity, keys keyring.Ring) {
	t.Helper()
	var err error
	manualRaw, _ := json.Marshal(domainnetworkaccess.MihomoManualNode{Protocol: "socks5", Server: "proxy.example.test", Port: 1080, Username: "alice", Password: "secret"})
	store.mihomoSourceType = domainnetworkaccess.MihomoSourceManualNode
	store.mihomoCipher, err = secretcrypto.EncryptStringWithKeyring(keys, string(manualRaw))
	if err != nil {
		t.Fatal(err)
	}
	source, err := service.MihomoSource(context.Background(), identity, "mihomo-1")
	if err != nil || source.ManualNode == nil || source.ManualNode.Password != "secret" || source.SubscriptionURL != "" {
		t.Fatalf("manual MihomoSource() = %#v, %v", source, err)
	}

	store.mihomoCipher = "https://plaintext.example.test/leak"
	if _, err := service.MihomoSource(context.Background(), identity, "mihomo-1"); !errors.Is(err, apperrors.ErrServiceUnavailable) {
		t.Fatalf("plaintext MihomoSource() error = %v", err)
	}
}

func checkSavedVPNIdentity(t *testing.T, store *controlStoreStub) {
	t.Helper()
	if store.vpnSaved.SubjectID != "user-1" || store.vpnSaved.DeviceID != "device-1" || store.vpnSaved.GatewayID != "gateway-branch" || store.vpnSaved.AccessProfile != domainnetworkaccess.ProfileFull || store.vpnSaved.PostureVersion != 5 || store.vpnSaved.RequestHash == "" || store.gatewaySiteID != "site-hq" || store.requestedGatewayID != "gateway-branch" {
		t.Fatalf("saved VPN connection = %#v", store.vpnSaved)
	}
}

func checkZTNAConnectionModes(t *testing.T, service *Service, store *controlStoreStub, identity networkidentity.Identity, now time.Time) {
	t.Helper()
	for _, tc := range []struct {
		mode        string
		requestID   string
		wantNetwork bool
	}{
		{mode: domainnetworkaccess.ModeInternalZTNA, requestID: "vpn-request-internal-ztna"},
		{mode: domainnetworkaccess.ModeExternalVPNZTNA, requestID: "vpn-request-ztna", wantNetwork: true},
		{mode: domainnetworkaccess.ModeExternalDirectZTNA, requestID: "vpn-request-direct-ztna"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			networkLeases := []networkprotocol.NetworkLease{}
			if tc.wantNetwork {
				networkLeases = []networkprotocol.NetworkLease{{ID: "lease-network", SessionID: tc.requestID, SubjectID: "user-1", DeviceID: "device-1", NetworkSpaceID: "space-hq", CIDRs: []string{"10.20.0.0/16"}, PolicyVersion: 7, IssuedAt: now, ExpiresAt: now.Add(5 * time.Minute)}}
			}
			store.vpnResult = networkprotocol.VPNConnectResult{
				RequestID: tc.requestID, Decision: domainnetworkaccess.DecisionAllow, ReasonCode: "policy_allowed", SessionID: tc.requestID,
				GatewayID: "gateway-branch", ConfigurationVersion: 3, PolicyVersion: 7, ValidUntil: now.Add(5 * time.Minute), NetworkLeases: networkLeases,
				ResourceLeases: []networkprotocol.ResourceLease{{ID: "lease-resource", SessionID: tc.requestID, SubjectID: "user-1", DeviceID: "device-1", ResourceIDs: []string{"resource-db"}, PolicyVersion: 7, IssuedAt: now, ExpiresAt: now.Add(5 * time.Minute)}},
			}
			request := networkprotocol.VPNConnectRequest{
				RequestID: tc.requestID, SiteID: "site-hq", NetworkSpaceID: "space-hq", GatewayID: "gateway-branch", Mode: tc.mode,
				ResourceIDs: []string{"resource-db"}, AccessGrantID: "grant-1", AccessGrantToken: "0123456789abcdef0123456789abcdef",
			}
			message, err := service.ConnectVPN(context.Background(), identity, runtimeMessageJSON(t, now, identity, networkprotocol.MessageVPNConnectRequest, request))
			if err != nil {
				t.Fatalf("ConnectVPN(%s) error = %v", tc.mode, err)
			}
			result, err := networkprotocol.DecodePayload[networkprotocol.VPNConnectResult](message.Payload)
			if err != nil || result.Decision != domainnetworkaccess.DecisionAllow || len(result.ResourceLeases) != 1 || (len(result.NetworkLeases) == 1) != tc.wantNetwork {
				t.Fatalf("ConnectVPN(%s) result = %#v, %v", tc.mode, result, err)
			}
			if store.vpnSaved.Mode != tc.mode || len(store.vpnSaved.ResourceIDs) != 1 || store.vpnSaved.ResourceIDs[0] != "resource-db" || store.vpnSaved.GatewayID != "gateway-branch" || store.vpnSaved.AccessGrantID != "grant-1" || store.vpnSaved.AccessGrantTokenHash != accessGrantTokenHash(request.AccessGrantToken) || store.vpnSaved.AccessGrantTokenHash == request.AccessGrantToken {
				t.Fatalf("saved %s connection = %#v", tc.mode, store.vpnSaved)
			}
		})
	}
}

func checkNASPasswordAndDenials(t *testing.T, service *Service, store *controlStoreStub, identity networkidentity.Identity, now time.Time, request networkprotocol.NASAuthorizationRequest) {
	t.Helper()
	request.RequestID = "radius-request-2"
	request.AuthenticationMethod = networkprotocol.NASAuthenticationPasswordCompatible
	request.ClientCertificateSerial = ""
	request.ClientCertificateAuthorityKeyID = ""
	request.DeviceID = "device-1"
	store.profile = domainnetworkaccess.SiteProfileBinding{SiteID: "site-hq", AccessProfile: domainnetworkaccess.ProfileRestricted, VLANID: 30, FilterID: "soha-restricted", SessionTimeoutSeconds: 900}
	message, err := service.AuthorizeNAS(context.Background(), identity, runtimeMessageJSON(t, now, identity, networkprotocol.MessageNASAuthorizationRequest, request))
	if err != nil {
		t.Fatalf("AuthorizeNAS(password-compatible) error = %v", err)
	}
	result, err := networkprotocol.DecodePayload[networkprotocol.NASAuthorizationResult](message.Payload)
	if err != nil {
		t.Fatalf("decode password-compatible result: %v", err)
	}
	if result.AccessProfile != domainnetworkaccess.ProfileRestricted || result.ReasonCode != "password_compatible_profile_capped" || result.RadiusAttributes == nil || result.RadiusAttributes.VLANID != 30 {
		t.Fatalf("password-compatible authorization result = %#v", result)
	}

	request.RequestID = "radius-request-3"
	request.AuthenticationMethod = networkprotocol.NASAuthenticationEAPTLS
	if _, err := service.AuthorizeNAS(context.Background(), identity, runtimeMessageJSON(t, now, identity, networkprotocol.MessageNASAuthorizationRequest, request)); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("AuthorizeNAS(missing certificate binding) error = %v, want invalid argument", err)
	}

	request.RequestID = "radius-request-4"
	request.AuthenticationMethod = networkprotocol.NASAuthenticationPasswordCompatible
	store.subjectErr = apperrors.ErrNotFound
	message, err = service.AuthorizeNAS(context.Background(), identity, runtimeMessageJSON(t, now, identity, networkprotocol.MessageNASAuthorizationRequest, request))
	if err != nil {
		t.Fatalf("AuthorizeNAS(unknown subject) error = %v", err)
	}
	result, err = networkprotocol.DecodePayload[networkprotocol.NASAuthorizationResult](message.Payload)
	if err != nil {
		t.Fatalf("decode unknown-subject result: %v", err)
	}
	if result.Decision != domainnetworkaccess.DecisionDeny || result.AccessProfile != domainnetworkaccess.ProfileDeny || result.ReasonCode != "subject_not_active" || store.authorized.RadiusAttributes != nil {
		t.Fatalf("unknown-subject authorization result = %#v, persisted = %#v", result, store.authorized)
	}

	_, err = service.AuthorizeRADIUS(context.Background(), identity, []byte(`{"requestId":"radius-request-5","nasId":"nas-hq-wifi","subjectId":"user-1","deviceId":"device-1","authenticationMethod":"password-compatible","userPassword":"must-not-cross-boundary"}`))
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("AuthorizeRADIUS(secret field) error = %v, want invalid argument", err)
	}
}
