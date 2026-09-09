package networkruntime_test

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	domainnetworkruntime "github.com/opensoha/soha/internal/domain/networkruntime"
	config "github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
	networkruntimerepo "github.com/opensoha/soha/internal/repository/networkruntime"
	"go.uber.org/zap"
)

func TestRepositoryWithPostgres(t *testing.T) {
	portText := os.Getenv("SOHA_NETWORK_RUNTIME_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_NETWORK_RUNTIME_TEST_POSTGRES_PORT to run the PostgreSQL integration test")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("invalid PostgreSQL test port: %v", err)
	}
	store, err := dbstore.New(config.DatabaseConfig{
		Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "soha", User: "pgsql", Password: "test-only", SSLMode: "disable",
		MaxOpenConns: 4, MaxIdleConns: 2, ConnMaxLifetime: time.Minute,
	}, zap.NewNop())
	if err != nil {
		t.Fatalf("open PostgreSQL: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	migrationSource := filepath.Join("..", "..", "..", "migrations", "postgres")
	migrationDir := filepath.Join(os.TempDir(), fmt.Sprintf("opensoha-network-runtime-migrations-%d", port))
	if err := os.MkdirAll(migrationDir, 0o700); err != nil {
		t.Fatalf("create migration staging directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(migrationDir) })
	for _, name := range []string{"0001_init.sql", "0060_network_access.sql", "0061_network_access_policy.sql", "0062_network_runtime.sql", "0063_network_nac.sql", "0064_network_certificate_binding.sql", "0065_network_wireguard.sql", "0066_network_gateway_management.sql", "0067_network_vpn.sql", "0068_network_access_grants.sql", "0069_network_device_posture_version.sql", "0070_network_mihomo_profiles.sql", "0071_network_nac_session_compat.sql", "0072_network_gateway_sites.sql", "0073_endpoint_device_inventory.sql", "0075_network_access_devices.sql", "0076_network_mihomo_sources.sql"} {
		contents, err := os.ReadFile(filepath.Join(migrationSource, name)) // #nosec G304 -- fixed migration names from the repository fixture list.
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(migrationDir, name), contents, 0o600); err != nil {
			t.Fatalf("stage %s: %v", name, err)
		}
	}
	if err := store.MigrateFromFile(ctx, migrationDir); err != nil {
		t.Fatalf("migrate PostgreSQL: %v", err)
	}

	scenario := &runtimeRepositoryScenario{ctx: ctx, store: store}
	scenario.seedPolicy(t)
	scenario.enrollFirstEndpoint(t)
	scenario.rotateInitialEndpoint(t)
	scenario.seedNetwork(t)
	scenario.checkGatewayEnrollment(t)
	scenario.checkMihomoSource(t)
	scenario.connectVPN(t)
	scenario.checkEndpointConfiguration(t)
	scenario.checkGatewayConfiguration(t)
	scenario.checkConfigurationContracts(t)
	scenario.checkSavedVPNState(t)
	scenario.applyConfiguration(t)
	scenario.checkMihomoRevision(t)
	scenario.seedPostureSession(t)
	scenario.checkPostureRenewal(t)
	scenario.revokePostureLease(t)
	scenario.authorizeNAS(t)
	scenario.claimCoA(t)
	scenario.completeCoA(t)
	scenario.disconnectNAS(t)
	scenario.expireNASCommand(t)
	scenario.renewVPN(t)
	scenario.revokeVPN(t)
	scenario.prepareVPNZTNA(t)
	scenario.consumeVPNZTNA(t)
	scenario.checkAndRevokeVPNZTNA(t)
	scenario.connectDirectZTNA(t)
	scenario.checkDirectZTNA(t)
	scenario.rotateEndpoint(t)
	scenario.rotateGateway(t)
	scenario.checkPendingAndProxyOnly(t)
}

type runtimeRepositoryScenario struct {
	ctx                        context.Context
	store                      *dbstore.Store
	repository                 *networkruntimerepo.Repository
	err                        error
	suffix                     string
	runtimeID                  string
	deviceID                   string
	subjectID                  string
	protectedID                string
	siteID                     string
	spaceID                    string
	gatewayID                  string
	gatewayRuntimeID           string
	mihomoProfileID            string
	sessionID                  string
	networkLeaseID             string
	resourceLeaseID            string
	postureDeviceID            string
	nasID                      string
	status                     string
	commandID                  string
	now                        time.Time
	secondNow                  time.Time
	connectedAt                time.Time
	sessionValidUntil          time.Time
	vpnRenewAt                 time.Time
	revokeNow                  time.Time
	ztnaRevokeAt               time.Time
	endpointRotationAt         time.Time
	policyVersion              int
	snapshot                   domainnetworkruntime.PolicySnapshot
	credential1                domainnetworkruntime.Credential
	credential2                domainnetworkruntime.Credential
	gatewayEnrollment          domainnetworkruntime.EnrollmentChallenge
	gatewayCredential          domainnetworkruntime.Credential
	connection                 domainnetworkruntime.VPNConnection
	vpnResult                  networkprotocol.VPNConnectResult
	endpointConfiguration      domainnetworkruntime.Configuration
	gatewayConfiguration       domainnetworkruntime.Configuration
	current                    domainnetworkruntime.Configuration
	authorization              domainnetworkruntime.NASAuthorization
	ztnaConnection             domainnetworkruntime.VPNConnection
	ztnaResult                 networkprotocol.VPNConnectResult
	ztnaGrant                  domainnetworkruntime.AccessGrant
	directConnection           domainnetworkruntime.VPNConnection
	endpointRotationConnection domainnetworkruntime.VPNConnection
	rotatedEndpointCredential  domainnetworkruntime.Credential
}

func (s *runtimeRepositoryScenario) seedPolicy(t *testing.T) {
	t.Helper()
	repository := networkruntimerepo.New(s.store.DB())
	s.repository = repository
	suffix := uuid.NewString()
	s.suffix = suffix
	runtimeID, deviceID, subjectID := "endpoint-"+s.suffix, "device-"+s.suffix, uuid.NewString()
	s.runtimeID = runtimeID
	s.deviceID = deviceID
	s.subjectID = subjectID
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	s.now = now
	if err := s.store.Exec(s.ctx, `INSERT INTO public.users (id, username, email) VALUES ($1::uuid, $2, $3)`, s.subjectID, "network-runtime-"+s.suffix, s.suffix+"@runtime.example.test"); err != nil {
		t.Fatalf("insert runtime user: %v", err)
	}
	protectedID := "resource-" + s.suffix
	s.protectedID = protectedID

	if err := s.store.SQLDB().QueryRowContext(s.ctx, `
		INSERT INTO public.network_access_policy_snapshots
			(content_hash, policies, protected_resource_ids, policy_count, protected_resource_count, published_at)
		VALUES ($1, '[]'::jsonb, $2::jsonb, 0, 1, $3)
		RETURNING policy_version`, digest("snapshot-"+s.suffix), `["`+s.protectedID+`"]`, s.now).Scan(&s.policyVersion); err != nil {
		t.Fatalf("insert policy snapshot: %v", err)
	}
	snapshot, err := s.repository.LatestSnapshot(s.ctx)
	s.snapshot = snapshot
	s.err = err
	if s.err != nil || s.snapshot.PolicyVersion != s.policyVersion || fmt.Sprint(s.snapshot.ProtectedResourceIDs) != "["+s.protectedID+"]" {
		t.Fatalf("LatestSnapshot() = %#v, %v", s.snapshot, s.err)
	}
}

func (s *runtimeRepositoryScenario) enrollFirstEndpoint(t *testing.T) {
	t.Helper()
	first := enrollment(s.suffix+"-1", s.runtimeID, s.deviceID, s.subjectID, s.now)
	if err := s.repository.CreateEnrollment(s.ctx, first); err != nil {
		t.Fatalf("CreateEnrollment(first): %v", err)
	}
	loaded, err := s.repository.GetEnrollment(s.ctx, first.ID)
	s.err = err
	if s.err != nil || loaded.ChallengeHash != first.ChallengeHash {
		t.Fatalf("GetEnrollment(first) = %#v, %v", loaded, s.err)
	}
	items, err := s.repository.ListEnrollments(s.ctx, 10)
	s.err = err
	if s.err != nil || len(items) == 0 {
		t.Fatalf("ListEnrollments() = %#v, %v", items, s.err)
	}
	credential1, configuration1, err := s.repository.ConsumeEnrollment(s.ctx, consumption(first, s.suffix+"-certificate-1", s.now), s.snapshot, s.now.Add(5*time.Minute))
	s.credential1 = credential1
	s.err = err
	if s.err != nil || s.credential1.Generation != 1 || s.credential1.WireGuardPublicKey == "" || configuration1.ConfigurationVersion != 1 || configuration1.Desired.AccessProfile != "onboarding" || len(configuration1.Desired.NetworkLeases) != 0 {
		t.Fatalf("ConsumeEnrollment(first) = credential %#v, configuration %#v, %v", s.credential1, configuration1, s.err)
	}
}

func (s *runtimeRepositoryScenario) rotateInitialEndpoint(t *testing.T) {
	t.Helper()
	secondNow := s.now.Add(time.Minute)
	s.secondNow = secondNow
	second := enrollment(s.suffix+"-2", s.runtimeID, s.deviceID, s.subjectID, s.secondNow)
	if err := s.repository.CreateEnrollment(s.ctx, second); err != nil {
		t.Fatalf("CreateEnrollment(second): %v", err)
	}
	credential2, configuration2, err := s.repository.ConsumeEnrollment(s.ctx, consumption(second, s.suffix+"-certificate-2", s.secondNow), s.snapshot, s.secondNow.Add(5*time.Minute))
	s.credential2 = credential2
	s.err = err
	if s.err != nil || s.credential2.Generation != 2 || configuration2.ConfigurationVersion != 2 {
		t.Fatalf("ConsumeEnrollment(second) = credential %#v, configuration %#v, %v", s.credential2, configuration2, s.err)
	}
	if _, err := s.repository.ActiveCredential(s.ctx, s.credential1.CertificateFingerprint, s.secondNow); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("old ActiveCredential() error = %v, want unauthorized", err)
	}
	if active, err := s.repository.ActiveCredential(s.ctx, s.credential2.CertificateFingerprint, s.secondNow); err != nil || active.ID != s.credential2.ID {
		t.Fatalf("new ActiveCredential() = %#v, %v", active, err)
	}
	if active, err := s.repository.ActiveEndpointCredential(s.ctx, s.credential2.CertificateSerial, s.credential2.CertificateAuthorityKeyID, s.secondNow); err != nil || active.ID != s.credential2.ID {
		t.Fatalf("ActiveEndpointCredential() = %#v, %v", active, err)
	}
	if _, err := s.repository.ActiveEndpointCredential(s.ctx, s.credential2.CertificateSerial, "ffffffffffffffff", s.secondNow); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("ActiveEndpointCredential(wrong issuer) error = %v, want unauthorized", err)
	}
	if err := s.repository.TouchCredential(s.ctx, s.credential2.ID, s.secondNow); err != nil {
		t.Fatalf("TouchCredential(): %v", err)
	}
}

func (s *runtimeRepositoryScenario) seedNetwork(t *testing.T) {
	t.Helper()
	siteID, spaceID, gatewayID, gatewayRuntimeID := "site-"+s.suffix, "vpn-space-"+s.suffix, "gateway-"+s.suffix, "gateway-runtime-"+s.suffix
	s.siteID = siteID
	s.spaceID = spaceID
	s.gatewayID = gatewayID
	s.gatewayRuntimeID = gatewayRuntimeID
	if err := s.store.Exec(s.ctx, `INSERT INTO public.network_access_sites (id, name, status, created_at, updated_at) VALUES ($1, $2, 'active', $3, $3)`, s.siteID, "VPN site "+s.suffix, s.secondNow); err != nil {
		t.Fatalf("insert VPN site: %v", err)
	}
	if err := s.store.Exec(s.ctx, `INSERT INTO public.network_access_devices (id, owner_user_id, name, hostname, platform, status, posture_status, created_at, updated_at) VALUES ($1, $2::uuid, $3, $4, 'linux', 'active', 'compliant', $5, $5)`, s.deviceID, s.subjectID, "Runtime device "+s.suffix, s.runtimeID, s.secondNow); err != nil {
		t.Fatalf("insert runtime device: %v", err)
	}
	if err := s.store.Exec(s.ctx, `INSERT INTO public.network_access_spaces (id, site_id, name, cidrs, status, created_at, updated_at) VALUES ($1, $2, $3, '["10.77.0.0/24"]'::jsonb, 'active', $4, $4)`, s.spaceID, s.siteID, "VPN space "+s.suffix, s.secondNow); err != nil {
		t.Fatalf("insert VPN space: %v", err)
	}
	if err := s.store.Exec(s.ctx, `INSERT INTO public.network_access_resources (id, space_id, name, kind, target, protocol, ports, protected, path_mode, created_at, updated_at) VALUES ($1, $2, $3, 'ip', '10.77.0.10', 'tcp', '[443]'::jsonb, true, 'automatic', $4, $4)`, s.protectedID, s.spaceID, "Protected app "+s.suffix, s.secondNow); err != nil {
		t.Fatalf("insert protected VPN resource: %v", err)
	}
	if err := s.store.Exec(s.ctx, `INSERT INTO public.network_access_gateways
		(id, runtime_id, site_id, name, status, administrative_status, public_endpoint_host, public_endpoint_port,
		 overlay_cidr, routing_mode, mtu, persistent_keepalive_seconds, dns_servers, created_at, updated_at)
		VALUES ($1, $2, $3, $4, 'offline', 'active', 'vpn.example.test', 51820, '100.96.0.0/29'::cidr, 'routed', 1420, 25, '["10.77.0.53"]'::jsonb, $5, $5)`, s.gatewayID, s.gatewayRuntimeID, s.siteID, "VPN gateway "+s.suffix, s.secondNow); err != nil {
		t.Fatalf("insert VPN gateway: %v", err)
	}
}

func (s *runtimeRepositoryScenario) checkGatewayEnrollment(t *testing.T) {
	t.Helper()
	gatewayEnrollment := enrollment(s.suffix+"-gateway", s.gatewayRuntimeID, "gateway-device-"+s.suffix, "gateway-subject-"+s.suffix, s.secondNow)
	s.gatewayEnrollment = gatewayEnrollment
	s.gatewayEnrollment.RuntimeKind = "gateway"
	if err := s.repository.CreateEnrollment(s.ctx, s.gatewayEnrollment); err != nil {
		t.Fatalf("CreateEnrollment(gateway): %v", err)
	}
	gatewayCredential, gatewayBootstrap, err := s.repository.ConsumeEnrollment(s.ctx, consumption(s.gatewayEnrollment, s.suffix+"-gateway-certificate", s.secondNow), s.snapshot, s.secondNow.Add(5*time.Minute))
	s.gatewayCredential = gatewayCredential
	s.err = err
	if s.err != nil || gatewayBootstrap.Desired.WireGuard == nil || gatewayBootstrap.Desired.WireGuard.Role != "gateway" || len(gatewayBootstrap.Desired.WireGuard.Peers) != 0 {
		t.Fatalf("ConsumeEnrollment(gateway) = credential %#v, configuration %#v, %v", s.gatewayCredential, gatewayBootstrap, s.err)
	}
	if err := s.repository.TouchCredential(s.ctx, s.gatewayCredential.ID, s.secondNow); err != nil {
		t.Fatalf("TouchCredential(gateway): %v", err)
	}
	activeGateway, activeGatewayCredential, err := s.repository.ActiveVPNGateway(s.ctx, s.siteID, "", s.secondNow)
	s.err = err
	if s.err != nil || activeGateway.ID != s.gatewayID || activeGatewayCredential.ID != s.gatewayCredential.ID || activeGateway.WireGuardPublicKey == "" {
		t.Fatalf("ActiveVPNGateway() = %#v / %#v / %v", activeGateway, activeGatewayCredential, s.err)
	}
}

func (s *runtimeRepositoryScenario) checkMihomoSource(t *testing.T) {
	t.Helper()
	mihomoProfileID := "mihomo-" + s.suffix
	s.mihomoProfileID = mihomoProfileID
	if err := s.store.Exec(s.ctx, `INSERT INTO public.network_mihomo_profiles
		(id, device_id, name, mode, source_type, status, subscription_url_ciphertext, mixed_port, controller_port, dns_mode,
		 selector_group, selected_proxy, bypass_cidrs, bypass_hosts, fail_closed, created_at, updated_at)
		VALUES ($1, $2, $3, 'managed_follow', 'managed_subscription', 'active', 'test-ciphertext', 7890, 9090, 'disabled',
		 'Soha', 'edge-a', '["172.16.0.0/16"]'::jsonb, '["control.example.test"]'::jsonb, true, $4, $4)`,
		s.mihomoProfileID, s.deviceID, "Managed mihomo "+s.suffix, s.secondNow); err != nil {
		t.Fatalf("insert mihomo profile: %v", err)
	}
	if ciphertext, revision, err := s.repository.MihomoSubscription(s.ctx, s.credential2.ID, s.mihomoProfileID, s.secondNow); err != nil || ciphertext != "test-ciphertext" || revision != 1 {
		t.Fatalf("MihomoSubscription() = %q/%d, %v", ciphertext, revision, err)
	}
	if sourceType, ciphertext, revision, err := s.repository.MihomoSource(s.ctx, s.credential2.ID, s.mihomoProfileID, s.secondNow); err != nil || sourceType != domainnetworkaccess.MihomoSourceManagedSubscription || ciphertext != "test-ciphertext" || revision != 1 {
		t.Fatalf("MihomoSource() = %q/%q/%d, %v", sourceType, ciphertext, revision, err)
	}
	if _, _, err := s.repository.MihomoSubscription(s.ctx, s.gatewayCredential.ID, s.mihomoProfileID, s.secondNow); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("cross-device MihomoSubscription() error = %v, want not found", err)
	}
}

func (s *runtimeRepositoryScenario) connectVPN(t *testing.T) {
	t.Helper()
	connectedAt := s.secondNow.Add(20 * time.Second)
	s.connectedAt = connectedAt
	connection := domainnetworkruntime.VPNConnection{
		RequestID: "vpn-connect-" + s.suffix, RequestHash: digest("vpn-connect-request-" + s.suffix), RuntimeID: s.runtimeID,
		CredentialID: s.credential2.ID, SubjectID: s.subjectID, DeviceID: s.deviceID, SiteID: s.siteID, NetworkSpaceID: s.spaceID,
		Mode: domainnetworkaccess.ModeExternalVPN, Decision: domainnetworkaccess.DecisionAllow, AccessProfile: domainnetworkaccess.ProfileFull,
		PolicyVersion: s.policyVersion, PostureVersion: 1, ReasonCode: "policy_allowed", SessionID: "vpn-session-" + s.suffix,
		GatewayID: s.gatewayID, GatewayRuntimeID: s.gatewayRuntimeID, GatewayCredentialID: s.gatewayCredential.ID,
		EndpointPublicKey: s.credential2.WireGuardPublicKey, ValidUntil: s.connectedAt.Add(4 * time.Minute), CreatedAt: s.connectedAt,
	}
	s.connection = connection
	vpnResult, err := s.repository.SaveVPNConnection(s.ctx, s.connection, s.snapshot)
	s.vpnResult = vpnResult
	s.err = err
	if s.err != nil || s.vpnResult.Decision != domainnetworkaccess.DecisionAllow || s.vpnResult.GatewayID != s.gatewayID || len(s.vpnResult.NetworkLeases) != 1 || s.vpnResult.ConfigurationVersion < 3 {
		t.Fatalf("SaveVPNConnection() = %#v, %v", s.vpnResult, s.err)
	}
	replayed, err := s.repository.SaveVPNConnection(s.ctx, s.connection, s.snapshot)
	s.err = err
	if s.err != nil || fmt.Sprint(replayed) != fmt.Sprint(s.vpnResult) {
		t.Fatalf("idempotent SaveVPNConnection() = %#v, %v", replayed, s.err)
	}
	conflict := s.connection
	conflict.RequestHash = digest("different-vpn-request-" + s.suffix)
	if _, err := s.repository.SaveVPNConnection(s.ctx, conflict, s.snapshot); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("reused VPN request error = %v, want conflict", err)
	}
}

func (s *runtimeRepositoryScenario) checkEndpointConfiguration(t *testing.T) {
	t.Helper()
	endpointConfiguration, err := s.repository.EnsureConfiguration(s.ctx, s.runtimeID, s.snapshot, s.connectedAt, s.connectedAt.Add(5*time.Minute))
	s.endpointConfiguration = endpointConfiguration
	s.err = err
	if s.err != nil || s.endpointConfiguration.Desired.WireGuard == nil || fmt.Sprint(s.endpointConfiguration.Desired.WireGuard.Addresses) != "[100.96.0.2/32]" || fmt.Sprint(s.endpointConfiguration.Desired.WireGuard.Routes) != "[10.77.0.0/24]" || len(s.endpointConfiguration.Desired.WireGuard.FirewallRules) != 2 || s.endpointConfiguration.Desired.WireGuard.FirewallRules[0].Effect != "deny" || s.endpointConfiguration.Desired.WireGuard.FirewallRules[0].DestinationCIDR != "10.77.0.10/32" || s.endpointConfiguration.Desired.Mihomo == nil || s.endpointConfiguration.Desired.Mihomo.ProfileID != s.mihomoProfileID || fmt.Sprint(s.endpointConfiguration.Desired.Mihomo.BypassCIDRs) != "[10.77.0.0/24 172.16.0.0/16]" || fmt.Sprint(s.endpointConfiguration.Desired.Mihomo.BypassHosts) != "[control.example.test vpn.example.test]" {
		t.Fatalf("endpoint VPN configuration = %#v, %v", s.endpointConfiguration, s.err)
	}
}

func (s *runtimeRepositoryScenario) checkGatewayConfiguration(t *testing.T) {
	t.Helper()
	gatewayConfiguration, err := s.repository.EnsureConfiguration(s.ctx, s.gatewayRuntimeID, s.snapshot, s.connectedAt, s.connectedAt.Add(5*time.Minute))
	s.gatewayConfiguration = gatewayConfiguration
	s.err = err
	if s.err != nil || s.gatewayConfiguration.Desired.WireGuard == nil || fmt.Sprint(s.gatewayConfiguration.Desired.WireGuard.Addresses) != "[100.96.0.1/32]" || len(s.gatewayConfiguration.Desired.WireGuard.Peers) != 1 || fmt.Sprint(s.gatewayConfiguration.Desired.WireGuard.Peers[0].AllowedIPs) != "[100.96.0.2/32]" || fmt.Sprint(s.gatewayConfiguration.Desired.WireGuard.Routes) != "[100.96.0.2/32]" || s.gatewayConfiguration.Desired.WireGuard.FirewallDefault != "deny" {
		t.Fatalf("gateway VPN configuration = %#v, %v", s.gatewayConfiguration, s.err)
	}
}

func (s *runtimeRepositoryScenario) checkConfigurationContracts(t *testing.T) {
	t.Helper()
	for runtime, configuration := range map[string]domainnetworkruntime.Configuration{s.runtimeID: s.endpointConfiguration, s.gatewayRuntimeID: s.gatewayConfiguration} {
		payload, err := json.Marshal(configuration.Desired)
		if err != nil {
			t.Fatal(err)
		}
		encoded, err := json.Marshal(networkprotocol.RuntimeMessage{SchemaVersion: networkprotocol.RuntimeSchemaVersion, MessageID: "configuration-" + runtime, MessageType: networkprotocol.MessageConfiguration, ProducerID: "network-control", RuntimeID: runtime, RuntimeKind: map[bool]string{true: "gateway", false: "endpoint"}[runtime == s.gatewayRuntimeID], OccurredAt: s.connectedAt, ExpiresAt: configuration.ValidUntil, Payload: payload})
		if err != nil {
			t.Fatal(err)
		}
		schemas, err := networkprotocol.CompileSchemas()
		if err != nil {
			t.Fatal(err)
		}
		if err := schemas.ValidateRuntime(encoded); err != nil {
			t.Fatalf("runtime configuration %s violates contract: %v\n%s", runtime, err, encoded)
		}
		if string(payload) == "" || containsPrivateKey(string(payload)) {
			t.Fatalf("runtime configuration %s exposed a private key: %s", runtime, payload)
		}
	}
}

func (s *runtimeRepositoryScenario) checkSavedVPNState(t *testing.T) {
	t.Helper()
	var savedGatewayID, peerAddress string
	if err := s.store.SQLDB().QueryRowContext(s.ctx, `SELECT gateway_id FROM public.network_runtime_sessions WHERE id = $1`, s.connection.SessionID).Scan(&savedGatewayID); err != nil || savedGatewayID != s.gatewayID {
		t.Fatalf("saved VPN session gateway = %q, %v", savedGatewayID, err)
	}
	if err := s.store.SQLDB().QueryRowContext(s.ctx, `SELECT overlay_address::text FROM public.network_wireguard_peers WHERE session_id = $1 AND status = 'active'`, s.connection.SessionID).Scan(&peerAddress); err != nil || peerAddress != "100.96.0.2/32" {
		t.Fatalf("saved WireGuard peer address = %q, %v", peerAddress, err)
	}
}

func (s *runtimeRepositoryScenario) applyConfiguration(t *testing.T) {
	t.Helper()
	readbackHash := digest("readback-" + s.suffix)
	ack := networkprotocol.ConfigurationApplied{ConfigurationVersion: s.vpnResult.ConfigurationVersion, PolicyVersion: s.policyVersion, Status: "applied", ReadbackHash: readbackHash}
	staleAck := ack
	staleAck.ConfigurationVersion--
	if err := s.repository.ApplyConfiguration(s.ctx, s.runtimeID, staleAck, s.secondNow); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale ApplyConfiguration() error = %v, want conflict", err)
	}
	if err := s.repository.ApplyConfiguration(s.ctx, s.runtimeID, ack, s.secondNow); err != nil {
		t.Fatalf("ApplyConfiguration(): %v", err)
	}
	if err := s.repository.ApplyConfiguration(s.ctx, s.runtimeID, ack, s.secondNow); err != nil {
		t.Fatalf("idempotent ApplyConfiguration(): %v", err)
	}
	conflictingAck := ack
	conflictingAck.ReadbackHash = digest("different-" + s.suffix)
	if err := s.repository.ApplyConfiguration(s.ctx, s.runtimeID, conflictingAck, s.secondNow); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("conflicting ApplyConfiguration() error = %v, want conflict", err)
	}
	current, err := s.repository.EnsureConfiguration(s.ctx, s.runtimeID, s.snapshot, s.secondNow, s.secondNow.Add(5*time.Minute))
	s.current = current
	s.err = err
	if s.err != nil || s.current.ConfigurationVersion != s.vpnResult.ConfigurationVersion || s.current.ReadbackHash != readbackHash {
		t.Fatalf("EnsureConfiguration() = %#v, %v", s.current, s.err)
	}
}

func (s *runtimeRepositoryScenario) checkMihomoRevision(t *testing.T) {
	t.Helper()
	if err := s.store.Exec(s.ctx, `UPDATE public.network_mihomo_profiles SET selected_proxy = 'edge-b', revision = revision + 1, updated_at = $2 WHERE id = $1`, s.mihomoProfileID, s.secondNow.Add(time.Second)); err != nil {
		t.Fatalf("update mihomo profile revision: %v", err)
	}
	mihomoUpdated, err := s.repository.EnsureConfiguration(s.ctx, s.runtimeID, s.snapshot, s.secondNow.Add(time.Second), s.secondNow.Add(5*time.Minute))
	s.err = err
	if s.err != nil || mihomoUpdated.ConfigurationVersion <= s.current.ConfigurationVersion || mihomoUpdated.Desired.Mihomo == nil || mihomoUpdated.Desired.Mihomo.ProfileRevision != 2 || mihomoUpdated.Desired.Mihomo.SelectedProxy != "edge-b" {
		t.Fatalf("mihomo revision configuration = %#v, %v", mihomoUpdated, s.err)
	}
	mihomoStable, err := s.repository.EnsureConfiguration(s.ctx, s.runtimeID, s.snapshot, s.secondNow.Add(2*time.Second), s.secondNow.Add(5*time.Minute))
	s.err = err
	if s.err != nil || mihomoStable.ConfigurationVersion != mihomoUpdated.ConfigurationVersion {
		t.Fatalf("stable mihomo configuration = %#v, %v", mihomoStable, s.err)
	}
}

func (s *runtimeRepositoryScenario) seedPostureSession(t *testing.T) {
	t.Helper()
	sessionID, networkLeaseID, resourceLeaseID := "session-"+s.suffix, "network-lease-"+s.suffix, "resource-lease-"+s.suffix
	s.sessionID = sessionID
	s.networkLeaseID = networkLeaseID
	s.resourceLeaseID = resourceLeaseID
	postureDeviceID := "posture-device-" + s.suffix
	s.postureDeviceID = postureDeviceID
	if err := s.store.Exec(s.ctx, `INSERT INTO public.network_access_devices (id, owner_user_id, name, hostname, platform, site_id, status, posture_status, created_at, updated_at) VALUES ($1, $2::uuid, $3, $4, 'linux', $5, 'active', 'compliant', $6, $6)`, s.postureDeviceID, s.subjectID, "Posture device "+s.suffix, "posture-"+s.runtimeID, s.siteID, s.secondNow); err != nil {
		t.Fatalf("insert posture device: %v", err)
	}
	sessionValidUntil := s.secondNow.Add(4 * time.Minute)
	s.sessionValidUntil = sessionValidUntil
	if err := s.store.Exec(s.ctx, `
		INSERT INTO public.network_runtime_sessions
			(id, runtime_id, subject_id, device_id, site_id, mode, access_profile, status, policy_version, configuration_version, posture_version, valid_until)
		VALUES ($1, $2, $3, $4, $5, 'external_vpn_ztna', 'full', 'active', $6, $7, 1, $8)`,
		s.sessionID, s.runtimeID, s.subjectID, s.postureDeviceID, s.siteID, s.policyVersion, s.vpnResult.ConfigurationVersion, s.sessionValidUntil); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	if err := s.store.Exec(s.ctx, `
		INSERT INTO public.network_runtime_leases
			(id, session_id, lease_kind, subject_id, device_id, network_space_id, cidrs, resource_ids, policy_version, status, issued_at, expires_at)
		VALUES
			($1, $3, 'network', $4, $5, $6, '["10.200.0.0/16"]'::jsonb, '[]'::jsonb, $7, 'issued', $8, $9),
			($2, $3, 'resource', $4, $5, NULL, '[]'::jsonb, $10::jsonb, $7, 'active', $8, $9)`,
		s.networkLeaseID, s.resourceLeaseID, s.sessionID, s.subjectID, s.postureDeviceID, "space-"+s.suffix, s.policyVersion, s.secondNow.Add(-time.Minute), s.secondNow.Add(2*time.Minute), `["`+s.protectedID+`"]`); err != nil {
		t.Fatalf("insert leases: %v", err)
	}
}

func (s *runtimeRepositoryScenario) checkPostureRenewal(t *testing.T) {
	t.Helper()
	postureVersion := 1
	renewed, err := s.repository.RenewLeases(s.ctx, s.runtimeID, networkprotocol.LeaseRenewRequest{
		SessionID: s.sessionID, LeaseIDs: []string{s.networkLeaseID, s.resourceLeaseID}, ObservedConfigurationVersion: s.vpnResult.ConfigurationVersion, PostureVersion: &postureVersion,
	}, s.snapshot, s.secondNow, 5*time.Minute)
	s.err = err
	if s.err != nil || !renewed.ValidUntil.Equal(s.sessionValidUntil) || len(renewed.NetworkLeases) != 1 || len(renewed.ResourceLeases) != 1 || len(renewed.RevokedLeaseIDs) != 0 {
		t.Fatalf("RenewLeases() = %#v, %v", renewed, s.err)
	}
	if err := s.store.Exec(s.ctx, `UPDATE public.network_access_devices SET posture_status = 'non_compliant' WHERE id = $1`, s.postureDeviceID); err != nil {
		t.Fatalf("downgrade device posture: %v", err)
	}
	var renewedConfigurationVersion int
	if err := s.store.SQLDB().QueryRowContext(s.ctx, `SELECT configuration_version FROM public.network_runtime_sessions WHERE id = $1`, s.sessionID).Scan(&renewedConfigurationVersion); err != nil {
		t.Fatalf("read renewed configuration version: %v", err)
	}
	if _, err := s.repository.RenewLeases(s.ctx, s.runtimeID, networkprotocol.LeaseRenewRequest{
		SessionID: s.sessionID, LeaseIDs: []string{s.networkLeaseID}, ObservedConfigurationVersion: renewedConfigurationVersion,
	}, s.snapshot, s.secondNow, 5*time.Minute); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("posture downgrade RenewLeases() error = %v, want conflict", err)
	}
	if _, err := s.repository.RenewLeases(s.ctx, s.runtimeID, networkprotocol.LeaseRenewRequest{
		SessionID: s.sessionID, LeaseIDs: []string{s.networkLeaseID}, ObservedConfigurationVersion: s.vpnResult.ConfigurationVersion - 1,
	}, s.snapshot, s.secondNow, 5*time.Minute); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale RenewLeases() error = %v, want conflict", err)
	}
}

func (s *runtimeRepositoryScenario) revokePostureLease(t *testing.T) {
	t.Helper()
	effectiveAt := s.secondNow.Add(time.Minute)
	affected, err := s.repository.RevokeLeases(s.ctx, s.runtimeID, networkprotocol.LeaseRevoke{
		SessionID: s.sessionID, LeaseIDs: []string{s.networkLeaseID}, ReasonCode: "policy_changed", EffectiveAt: effectiveAt,
	}, s.snapshot, s.secondNow, s.secondNow.Add(5*time.Minute))
	s.err = err
	if s.err != nil || affected != 1 {
		t.Fatalf("RevokeLeases() = %d, %v", affected, s.err)
	}

	var revokedAt time.Time
	if err := s.store.SQLDB().QueryRowContext(s.ctx, `SELECT status, revoked_at FROM public.network_runtime_leases WHERE id = $1`, s.networkLeaseID).Scan(&s.status, &revokedAt); err != nil || s.status != "revoking" || !revokedAt.Equal(effectiveAt) {
		t.Fatalf("revoked lease = status:%q revokedAt:%v error:%v", s.status, revokedAt, err)
	}
}

func (s *runtimeRepositoryScenario) authorizeNAS(t *testing.T) {
	t.Helper()
	nasID, nasRequestID := "nas-"+s.suffix, "radius-request-"+s.suffix
	s.nasID = nasID
	nasSiteID := "nas-site-" + s.suffix
	if err := s.store.Exec(s.ctx, `INSERT INTO public.network_access_sites (id, name, status) VALUES ($1, $2, 'active')`, nasSiteID, "NAS Site "+s.suffix); err != nil {
		t.Fatalf("insert NAS site: %v", err)
	}
	if err := s.store.Exec(s.ctx, `INSERT INTO public.network_access_nas_bindings (id, nas_id, runtime_id, site_id, name, status) VALUES ($1, $2, $3, $4, $5, 'active')`, "nas-binding-"+s.suffix, s.nasID, s.runtimeID, nasSiteID, "NAS "+s.suffix); err != nil {
		t.Fatalf("insert NAS binding: %v", err)
	}
	authorization := domainnetworkruntime.NASAuthorization{
		RequestID: nasRequestID, RequestHash: digest("nas-request-" + s.suffix), RuntimeID: s.runtimeID, NASID: s.nasID,
		SubjectID: s.subjectID, DeviceID: s.deviceID, AuthenticationMethod: networkprotocol.NASAuthenticationPasswordCompatible,
		SessionID: "nac-session-" + s.suffix, Decision: domainnetworkaccess.DecisionAllow, AccessProfile: domainnetworkaccess.ProfileRestricted,
		PolicyVersion: s.policyVersion, ReasonCode: "password_compatible_profile_capped",
		RadiusAttributes: &networkprotocol.RadiusAttributes{VLANID: 30, FilterID: "soha-restricted", SessionTimeoutSeconds: 600},
		ValidUntil:       s.secondNow.Add(10 * time.Minute), CreatedAt: s.secondNow,
	}
	s.authorization = authorization
	savedAuthorization, err := s.repository.SaveNASAuthorization(s.ctx, s.authorization)
	s.err = err
	if s.err != nil || savedAuthorization.SessionID != s.authorization.SessionID || savedAuthorization.RadiusAttributes == nil || savedAuthorization.RadiusAttributes.VLANID != 30 {
		t.Fatalf("SaveNASAuthorization() = %#v, %v", savedAuthorization, s.err)
	}
	replayedAuthorization, err := s.repository.SaveNASAuthorization(s.ctx, s.authorization)
	s.err = err
	if s.err != nil || replayedAuthorization.RequestHash != s.authorization.RequestHash || replayedAuthorization.RadiusAttributes == nil || replayedAuthorization.RadiusAttributes.FilterID != "soha-restricted" {
		t.Fatalf("idempotent SaveNASAuthorization() = %#v, %v", replayedAuthorization, s.err)
	}
	conflictingAuthorization := s.authorization
	conflictingAuthorization.RequestHash = digest("different-nas-request-" + s.suffix)
	if _, err := s.repository.SaveNASAuthorization(s.ctx, conflictingAuthorization); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("conflicting SaveNASAuthorization() error = %v, want conflict", err)
	}
	if err := s.store.SQLDB().QueryRowContext(s.ctx, `SELECT status FROM public.network_runtime_sessions WHERE id = $1`, s.authorization.SessionID).Scan(&s.status); err != nil || s.status != "restricted" {
		t.Fatalf("NAS session status = %q, %v", s.status, err)
	}
}

func (s *runtimeRepositoryScenario) claimCoA(t *testing.T) {
	t.Helper()
	commandID := "coa-command-" + s.suffix
	s.commandID = commandID
	commandHash := digest("coa-command-" + s.suffix)
	if err := s.store.Exec(s.ctx, `INSERT INTO public.network_nas_session_commands (id, session_id, runtime_id, nas_id, action, target_access_profile, policy_version, status, reason_code, plan_hash, radius_attributes, effective_at, expires_at, created_at, updated_at) VALUES ($1, $2, $3, $4, 'coa', 'full', $5, 'pending', 'risk_cleared', $6, '{"vlanId":20,"filterId":"soha-full","sessionTimeoutSeconds":600}'::jsonb, $7, $8, $7, $7)`, s.commandID, s.authorization.SessionID, s.runtimeID, s.nasID, s.policyVersion, commandHash, s.secondNow, s.secondNow.Add(4*time.Minute)); err != nil {
		t.Fatalf("insert CoA command: %v", err)
	}
	claimed, err := s.repository.ClaimNASSessionCommand(s.ctx, s.runtimeID, s.secondNow)
	s.err = err
	if s.err != nil || claimed.ID != s.commandID || claimed.Status != domainnetworkaccess.SessionCommandDelivered || claimed.RadiusAttributes == nil || claimed.RadiusAttributes.VLANID != 20 {
		t.Fatalf("ClaimNASSessionCommand() = %#v, %v", claimed, s.err)
	}
	if _, err := s.repository.ClaimNASSessionCommand(s.ctx, s.runtimeID, s.secondNow.Add(5*time.Second)); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("early duplicate ClaimNASSessionCommand() error = %v, want not found", err)
	}
	redelivered, err := s.repository.ClaimNASSessionCommand(s.ctx, s.runtimeID, s.secondNow.Add(11*time.Second))
	s.err = err
	if s.err != nil || redelivered.ID != s.commandID {
		t.Fatalf("redelivered ClaimNASSessionCommand() = %#v, %v", redelivered, s.err)
	}
}

func (s *runtimeRepositoryScenario) completeCoA(t *testing.T) {
	t.Helper()
	commandResult := networkprotocol.NASSessionCommandResult{CommandID: s.commandID, SessionID: s.authorization.SessionID, Status: domainnetworkaccess.SessionCommandApplied, ReasonCode: "coa_applied", CompletedAt: s.secondNow.Add(12 * time.Second)}
	if err := s.repository.CompleteNASSessionCommand(s.ctx, s.runtimeID, commandResult, s.secondNow.Add(12*time.Second)); err != nil {
		t.Fatalf("CompleteNASSessionCommand() error = %v", err)
	}
	if err := s.repository.CompleteNASSessionCommand(s.ctx, s.runtimeID, commandResult, s.secondNow.Add(13*time.Second)); err != nil {
		t.Fatalf("idempotent CompleteNASSessionCommand() error = %v", err)
	}
	conflictingResult := commandResult
	conflictingResult.ReasonCode = "different_result"
	if err := s.repository.CompleteNASSessionCommand(s.ctx, s.runtimeID, conflictingResult, s.secondNow.Add(13*time.Second)); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("conflicting CompleteNASSessionCommand() error = %v, want conflict", err)
	}
	var accessProfile string
	if err := s.store.SQLDB().QueryRowContext(s.ctx, `SELECT status, access_profile FROM public.network_runtime_sessions WHERE id = $1`, s.authorization.SessionID).Scan(&s.status, &accessProfile); err != nil || s.status != "active" || accessProfile != domainnetworkaccess.ProfileFull {
		t.Fatalf("CoA-updated NAS session = status:%q profile:%q error:%v", s.status, accessProfile, err)
	}
}

func (s *runtimeRepositoryScenario) disconnectNAS(t *testing.T) {
	t.Helper()
	disconnectID := "disconnect-command-" + s.suffix
	if err := s.store.Exec(s.ctx, `INSERT INTO public.network_nas_session_commands (id, session_id, runtime_id, nas_id, action, target_access_profile, policy_version, status, reason_code, plan_hash, effective_at, expires_at, created_at, updated_at) VALUES ($1, $2, $3, $4, 'disconnect', 'deny', $5, 'pending', 'access_revoked', $6, $7, $8, $7, $7)`, disconnectID, s.authorization.SessionID, s.runtimeID, s.nasID, s.policyVersion, digest("disconnect-command-"+s.suffix), s.secondNow.Add(14*time.Second), s.secondNow.Add(4*time.Minute)); err != nil {
		t.Fatalf("insert disconnect command: %v", err)
	}
	if claimed, err := s.repository.ClaimNASSessionCommand(s.ctx, s.runtimeID, s.secondNow.Add(14*time.Second)); err != nil || claimed.ID != disconnectID {
		t.Fatalf("claim disconnect command = %#v, %v", claimed, err)
	}
	disconnectResult := networkprotocol.NASSessionCommandResult{CommandID: disconnectID, SessionID: s.authorization.SessionID, Status: domainnetworkaccess.SessionCommandApplied, ReasonCode: "disconnected", CompletedAt: s.secondNow.Add(15 * time.Second)}
	if err := s.repository.CompleteNASSessionCommand(s.ctx, s.runtimeID, disconnectResult, s.secondNow.Add(15*time.Second)); err != nil {
		t.Fatalf("complete disconnect command: %v", err)
	}
	if err := s.store.SQLDB().QueryRowContext(s.ctx, `SELECT status FROM public.network_runtime_sessions WHERE id = $1`, s.authorization.SessionID).Scan(&s.status); err != nil || s.status != "revoked" {
		t.Fatalf("disconnected NAS session status = %q, %v", s.status, err)
	}
}

func (s *runtimeRepositoryScenario) expireNASCommand(t *testing.T) {
	t.Helper()
	expiredCommandID := "expired-command-" + s.suffix
	if err := s.store.Exec(s.ctx, `INSERT INTO public.network_nas_session_commands (id, session_id, runtime_id, nas_id, action, target_access_profile, policy_version, status, reason_code, plan_hash, effective_at, expires_at, created_at, updated_at) VALUES ($1, $2, $3, $4, 'disconnect', 'deny', $5, 'pending', 'expired_test', $6, $7, $8, $7, $7)`, expiredCommandID, s.authorization.SessionID, s.runtimeID, s.nasID, s.policyVersion, digest("expired-command-"+s.suffix), s.secondNow.Add(16*time.Second), s.secondNow.Add(17*time.Second)); err != nil {
		t.Fatalf("insert expired command: %v", err)
	}
	if _, err := s.repository.ClaimNASSessionCommand(s.ctx, s.runtimeID, s.secondNow.Add(18*time.Second)); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("claim after command expiry error = %v, want not found", err)
	}
	if err := s.store.SQLDB().QueryRowContext(s.ctx, `SELECT status FROM public.network_nas_session_commands WHERE id = $1`, expiredCommandID).Scan(&s.status); err != nil || s.status != domainnetworkaccess.SessionCommandExpired {
		t.Fatalf("expired command status = %q, %v", s.status, err)
	}
}

func (s *runtimeRepositoryScenario) renewVPN(t *testing.T) {
	t.Helper()
	vpnRenewAt := s.connectedAt.Add(2 * time.Minute)
	s.vpnRenewAt = vpnRenewAt
	vpnRenewed, err := s.repository.RenewLeases(s.ctx, s.runtimeID, networkprotocol.LeaseRenewRequest{SessionID: s.connection.SessionID, LeaseIDs: []string{s.vpnResult.NetworkLeases[0].ID}, ObservedConfigurationVersion: s.vpnResult.ConfigurationVersion}, s.snapshot, s.vpnRenewAt, 5*time.Minute)
	s.err = err
	if s.err != nil || len(vpnRenewed.NetworkLeases) != 1 || !vpnRenewed.ValidUntil.Equal(s.vpnRenewAt.Add(5*time.Minute)) {
		t.Fatalf("renew VPN lease = %#v, %v", vpnRenewed, s.err)
	}
	var renewedVPNVersion int
	var renewedPeerExpiry time.Time
	if err := s.store.SQLDB().QueryRowContext(s.ctx, `SELECT s.configuration_version, p.expires_at FROM public.network_runtime_sessions s JOIN public.network_wireguard_peers p ON p.session_id = s.id WHERE s.id = $1`, s.connection.SessionID).Scan(&renewedVPNVersion, &renewedPeerExpiry); err != nil || renewedVPNVersion <= s.vpnResult.ConfigurationVersion || !renewedPeerExpiry.Equal(vpnRenewed.ValidUntil) {
		t.Fatalf("renewed VPN version/peer expiry = %d/%v, %v", renewedVPNVersion, renewedPeerExpiry, err)
	}
}

func (s *runtimeRepositoryScenario) revokeVPN(t *testing.T) {
	t.Helper()
	revokeNow := s.vpnRenewAt.Add(time.Minute)
	s.revokeNow = revokeNow
	if revoked, err := s.repository.RevokeLeases(s.ctx, s.runtimeID, networkprotocol.LeaseRevoke{SessionID: s.connection.SessionID, LeaseIDs: []string{s.vpnResult.NetworkLeases[0].ID}, ReasonCode: "user_disconnected", EffectiveAt: s.revokeNow}, s.snapshot, s.revokeNow, s.revokeNow.Add(5*time.Minute)); err != nil || revoked != 1 {
		t.Fatalf("revoke VPN lease = %d, %v", revoked, err)
	}
	var peerStatus string
	if err := s.store.SQLDB().QueryRowContext(s.ctx, `SELECT status FROM public.network_wireguard_peers WHERE session_id = $1`, s.connection.SessionID).Scan(&peerStatus); err != nil || peerStatus != "revoked" {
		t.Fatalf("revoked WireGuard peer status = %q, %v", peerStatus, err)
	}
	endpointAfterRevoke, err := s.repository.EnsureConfiguration(s.ctx, s.runtimeID, s.snapshot, s.revokeNow, s.revokeNow.Add(5*time.Minute))
	s.err = err
	if s.err != nil || endpointAfterRevoke.ConfigurationVersion <= s.vpnResult.ConfigurationVersion || endpointAfterRevoke.Desired.WireGuard != nil || endpointAfterRevoke.Desired.AccessProfile != domainnetworkaccess.ProfileOnboarding {
		t.Fatalf("endpoint configuration after revoke = %#v, %v", endpointAfterRevoke, s.err)
	}
	gatewayAfterRevoke, err := s.repository.EnsureConfiguration(s.ctx, s.gatewayRuntimeID, s.snapshot, s.revokeNow, s.revokeNow.Add(5*time.Minute))
	s.err = err
	if s.err != nil || gatewayAfterRevoke.Desired.WireGuard == nil || len(gatewayAfterRevoke.Desired.WireGuard.Peers) != 0 {
		t.Fatalf("gateway configuration after revoke = %#v, %v", gatewayAfterRevoke, s.err)
	}
}

func (s *runtimeRepositoryScenario) prepareVPNZTNA(t *testing.T) {
	t.Helper()
	ztnaConnection := s.connection
	s.ztnaConnection = ztnaConnection
	s.ztnaConnection.RequestID = "vpn-connect-ztna-" + s.suffix
	s.ztnaConnection.RequestHash = digest(s.ztnaConnection.RequestID)
	s.ztnaConnection.SessionID = "vpn-session-ztna-" + s.suffix
	s.ztnaConnection.Mode = domainnetworkaccess.ModeExternalVPNZTNA
	s.ztnaConnection.ResourceIDs = []string{s.protectedID}
	s.ztnaConnection.CreatedAt = s.revokeNow.Add(2 * time.Second)
	s.ztnaConnection.ValidUntil = s.ztnaConnection.CreatedAt.Add(4 * time.Minute)
	ztnaToken := "ztna-token-" + s.suffix
	s.ztnaConnection.AccessGrantID, s.ztnaConnection.AccessGrantTokenHash = "grant-ztna-"+s.suffix, digest(ztnaToken)
	ztnaGrant := accessGrantForConnection(s.ztnaConnection, s.ztnaConnection.AccessGrantID, ztnaToken, s.ztnaConnection.CreatedAt.Add(-time.Second), s.ztnaConnection.CreatedAt.Add(3*time.Minute))
	s.ztnaGrant = ztnaGrant
	if err := s.repository.CreateAccessGrant(s.ctx, s.ztnaGrant); err != nil {
		t.Fatalf("CreateAccessGrant(VPN+ZTNA): %v", err)
	}
	wrongToken := s.ztnaConnection
	wrongToken.AccessGrantTokenHash = digest("wrong-" + ztnaToken)
	if _, err := s.repository.SaveVPNConnection(s.ctx, wrongToken, s.snapshot); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("SaveVPNConnection(wrong grant token) error = %v, want access denied", err)
	}
}

func (s *runtimeRepositoryScenario) consumeVPNZTNA(t *testing.T) {
	t.Helper()
	ztnaResult, err := s.repository.SaveVPNConnection(s.ctx, s.ztnaConnection, s.snapshot)
	s.ztnaResult = ztnaResult
	s.err = err
	if s.err != nil || len(s.ztnaResult.NetworkLeases) != 1 || len(s.ztnaResult.ResourceLeases) != 1 || fmt.Sprint(s.ztnaResult.ResourceLeases[0].ResourceIDs) != "["+s.protectedID+"]" || !s.ztnaResult.ValidUntil.Equal(s.ztnaGrant.ExpiresAt) {
		t.Fatalf("SaveVPNConnection(VPN+ZTNA) = %#v, %v", s.ztnaResult, s.err)
	}
	if replayed, err := s.repository.SaveVPNConnection(s.ctx, s.ztnaConnection, s.snapshot); err != nil || fmt.Sprint(replayed) != fmt.Sprint(s.ztnaResult) {
		t.Fatalf("idempotent SaveVPNConnection(VPN+ZTNA) = %#v, %v", replayed, err)
	}
	consumedGrant, err := s.repository.GetAccessGrant(s.ctx, s.ztnaGrant.ID, s.ztnaConnection.CreatedAt)
	s.err = err
	if s.err != nil || consumedGrant.Status != domainnetworkruntime.AccessGrantConsumed || consumedGrant.TokenHash != "" || consumedGrant.SessionID != s.ztnaConnection.SessionID || fmt.Sprint(consumedGrant.ResourceLeaseIDs) != "["+s.ztnaResult.ResourceLeases[0].ID+"]" {
		t.Fatalf("consumed VPN+ZTNA grant = %#v, %v", consumedGrant, s.err)
	}
	reusedGrant := s.ztnaConnection
	reusedGrant.RequestID, reusedGrant.RequestHash, reusedGrant.SessionID = "vpn-connect-reused-grant-"+s.suffix, digest("vpn-connect-reused-grant-"+s.suffix), "vpn-session-reused-grant-"+s.suffix
	if _, err := s.repository.SaveVPNConnection(s.ctx, reusedGrant, s.snapshot); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("SaveVPNConnection(reused grant) error = %v, want access denied", err)
	}
}

func (s *runtimeRepositoryScenario) checkAndRevokeVPNZTNA(t *testing.T) {
	t.Helper()
	ztnaEndpoint, err := s.repository.EnsureConfiguration(s.ctx, s.runtimeID, s.snapshot, s.ztnaConnection.CreatedAt, s.ztnaConnection.CreatedAt.Add(5*time.Minute))
	s.err = err
	if s.err != nil || ztnaEndpoint.Desired.WireGuard == nil || fmt.Sprint(ztnaEndpoint.Desired.WireGuard.Routes) != "[10.77.0.0/24]" || len(ztnaEndpoint.Desired.ResourceLeases) != 1 || len(ztnaEndpoint.Desired.WireGuard.FirewallRules) != 3 || ztnaEndpoint.Desired.WireGuard.FirewallRules[0].Effect != "allow" || ztnaEndpoint.Desired.WireGuard.FirewallRules[0].LeaseID != s.ztnaResult.ResourceLeases[0].ID || ztnaEndpoint.Desired.WireGuard.FirewallRules[1].Effect != "deny" || ztnaEndpoint.Desired.WireGuard.FirewallRules[2].LeaseID != s.ztnaResult.NetworkLeases[0].ID {
		t.Fatalf("VPN+ZTNA endpoint configuration = %#v, %v", ztnaEndpoint, s.err)
	}
	ztnaRevokeAt := s.ztnaConnection.CreatedAt.Add(time.Second)
	s.ztnaRevokeAt = ztnaRevokeAt
	if revoked, err := s.repository.RevokeLeases(s.ctx, s.runtimeID, networkprotocol.LeaseRevoke{SessionID: s.ztnaConnection.SessionID, LeaseIDs: []string{s.ztnaResult.NetworkLeases[0].ID, s.ztnaResult.ResourceLeases[0].ID}, ReasonCode: "mode_changed", EffectiveAt: s.ztnaRevokeAt}, s.snapshot, s.ztnaRevokeAt, s.ztnaRevokeAt.Add(5*time.Minute)); err != nil || revoked != 2 {
		t.Fatalf("revoke VPN+ZTNA leases = %d, %v", revoked, err)
	}
}

func (s *runtimeRepositoryScenario) connectDirectZTNA(t *testing.T) {
	t.Helper()
	directConnection := s.connection
	s.directConnection = directConnection
	s.directConnection.RequestID = "direct-ztna-" + s.suffix
	s.directConnection.RequestHash = digest(s.directConnection.RequestID)
	s.directConnection.SessionID = "direct-ztna-session-" + s.suffix
	s.directConnection.Mode = domainnetworkaccess.ModeExternalDirectZTNA
	s.directConnection.ResourceIDs = []string{s.protectedID}
	s.directConnection.CreatedAt = s.ztnaRevokeAt.Add(2 * time.Second)
	s.directConnection.ValidUntil = s.directConnection.CreatedAt.Add(4 * time.Minute)
	directToken := "direct-token-" + s.suffix
	s.directConnection.AccessGrantID, s.directConnection.AccessGrantTokenHash = "grant-direct-"+s.suffix, digest(directToken)
	if err := s.repository.CreateAccessGrant(s.ctx, accessGrantForConnection(s.directConnection, s.directConnection.AccessGrantID, directToken, s.directConnection.CreatedAt.Add(-time.Second), s.directConnection.ValidUntil)); err != nil {
		t.Fatalf("CreateAccessGrant(direct ZTNA): %v", err)
	}
	directResult, err := s.repository.SaveVPNConnection(s.ctx, s.directConnection, s.snapshot)
	s.err = err
	if s.err != nil || len(directResult.NetworkLeases) != 0 || len(directResult.ResourceLeases) != 1 {
		t.Fatalf("SaveVPNConnection(direct ZTNA) = %#v, %v", directResult, s.err)
	}
}

func (s *runtimeRepositoryScenario) checkDirectZTNA(t *testing.T) {
	t.Helper()
	directEndpoint, err := s.repository.EnsureConfiguration(s.ctx, s.runtimeID, s.snapshot, s.directConnection.CreatedAt, s.directConnection.CreatedAt.Add(5*time.Minute))
	s.err = err
	if s.err != nil || directEndpoint.Desired.WireGuard == nil || fmt.Sprint(directEndpoint.Desired.WireGuard.Routes) != "[10.77.0.10/32]" || len(directEndpoint.Desired.WireGuard.DNSServers) != 0 || len(directEndpoint.Desired.NetworkLeases) != 0 || len(directEndpoint.Desired.ResourceLeases) != 1 || len(directEndpoint.Desired.WireGuard.FirewallRules) != 2 || directEndpoint.Desired.WireGuard.FirewallRules[0].Effect != "allow" || directEndpoint.Desired.WireGuard.FirewallRules[1].Effect != "deny" {
		t.Fatalf("direct ZTNA endpoint configuration = %#v, %v", directEndpoint, s.err)
	}
	directGateway, err := s.repository.EnsureConfiguration(s.ctx, s.gatewayRuntimeID, s.snapshot, s.directConnection.CreatedAt, s.directConnection.CreatedAt.Add(5*time.Minute))
	s.err = err
	if s.err != nil || directGateway.Desired.WireGuard == nil || len(directGateway.Desired.WireGuard.Peers) != 1 || len(directGateway.Desired.NetworkLeases) != 0 || len(directGateway.Desired.ResourceLeases) != 1 || len(directGateway.Desired.WireGuard.FirewallRules) != 2 || directGateway.Desired.WireGuard.FirewallRules[0].Effect != "allow" || directGateway.Desired.WireGuard.FirewallRules[1].Effect != "deny" {
		t.Fatalf("direct ZTNA gateway configuration = %#v, %v", directGateway, s.err)
	}
}

func (s *runtimeRepositoryScenario) rotateEndpoint(t *testing.T) {
	t.Helper()
	endpointRotationConnection := s.connection
	s.endpointRotationConnection = endpointRotationConnection
	s.endpointRotationConnection.RequestID = "vpn-connect-before-endpoint-rotation-" + s.suffix
	s.endpointRotationConnection.RequestHash = digest(s.endpointRotationConnection.RequestID)
	s.endpointRotationConnection.SessionID = "vpn-session-before-endpoint-rotation-" + s.suffix
	s.endpointRotationConnection.CreatedAt = s.revokeNow.Add(10 * time.Second)
	s.endpointRotationConnection.ValidUntil = s.endpointRotationConnection.CreatedAt.Add(4 * time.Minute)
	endpointRotationResult, err := s.repository.SaveVPNConnection(s.ctx, s.endpointRotationConnection, s.snapshot)
	s.err = err
	if s.err != nil || endpointRotationResult.Decision != domainnetworkaccess.DecisionAllow {
		t.Fatalf("connect before endpoint rotation = %#v, %v", endpointRotationResult, s.err)
	}
	endpointRotationAt := s.endpointRotationConnection.CreatedAt.Add(10 * time.Second)
	s.endpointRotationAt = endpointRotationAt
	endpointRotationEnrollment := enrollment(s.suffix+"-endpoint-rotation", s.runtimeID, s.deviceID, s.subjectID, s.endpointRotationAt)
	if err := s.repository.CreateEnrollment(s.ctx, endpointRotationEnrollment); err != nil {
		t.Fatalf("CreateEnrollment(endpoint rotation): %v", err)
	}
	rotatedEndpointCredential, rotatedEndpointConfiguration, err := s.repository.ConsumeEnrollment(s.ctx, consumption(endpointRotationEnrollment, s.suffix+"-endpoint-certificate-rotated", s.endpointRotationAt), s.snapshot, s.endpointRotationAt.Add(5*time.Minute))
	s.rotatedEndpointCredential = rotatedEndpointCredential
	s.err = err
	if s.err != nil || s.rotatedEndpointCredential.Generation != 3 || rotatedEndpointConfiguration.Desired.WireGuard != nil || rotatedEndpointConfiguration.Desired.AccessProfile != domainnetworkaccess.ProfileOnboarding {
		t.Fatalf("ConsumeEnrollment(endpoint rotation) = credential %#v, configuration %#v, %v", s.rotatedEndpointCredential, rotatedEndpointConfiguration, s.err)
	}
	assertRevokedVPNSession(t, s.store, s.ctx, s.endpointRotationConnection.SessionID)
	gatewayAfterEndpointRotation, err := s.repository.EnsureConfiguration(s.ctx, s.gatewayRuntimeID, s.snapshot, s.endpointRotationAt, s.endpointRotationAt.Add(5*time.Minute))
	s.err = err
	if s.err != nil || gatewayAfterEndpointRotation.Desired.WireGuard == nil || len(gatewayAfterEndpointRotation.Desired.WireGuard.Peers) != 0 {
		t.Fatalf("gateway configuration after endpoint rotation = %#v, %v", gatewayAfterEndpointRotation, s.err)
	}
}

func (s *runtimeRepositoryScenario) rotateGateway(t *testing.T) {
	t.Helper()
	gatewayRotationConnection := s.endpointRotationConnection
	gatewayRotationConnection.RequestID = "vpn-connect-before-gateway-rotation-" + s.suffix
	gatewayRotationConnection.RequestHash = digest(gatewayRotationConnection.RequestID)
	gatewayRotationConnection.SessionID = "vpn-session-before-gateway-rotation-" + s.suffix
	gatewayRotationConnection.CredentialID = s.rotatedEndpointCredential.ID
	gatewayRotationConnection.EndpointPublicKey = s.rotatedEndpointCredential.WireGuardPublicKey
	gatewayRotationConnection.CreatedAt = s.endpointRotationAt.Add(10 * time.Second)
	gatewayRotationConnection.ValidUntil = gatewayRotationConnection.CreatedAt.Add(4 * time.Minute)
	gatewayRotationResult, err := s.repository.SaveVPNConnection(s.ctx, gatewayRotationConnection, s.snapshot)
	s.err = err
	if s.err != nil || gatewayRotationResult.Decision != domainnetworkaccess.DecisionAllow {
		t.Fatalf("connect before gateway rotation = %#v, %v", gatewayRotationResult, s.err)
	}
	gatewayRotationAt := gatewayRotationConnection.CreatedAt.Add(10 * time.Second)
	gatewayRotationEnrollment := enrollment(s.suffix+"-gateway-rotation", s.gatewayRuntimeID, s.gatewayEnrollment.DeviceID, s.gatewayEnrollment.SubjectID, gatewayRotationAt)
	gatewayRotationEnrollment.RuntimeKind = "gateway"
	if err := s.repository.CreateEnrollment(s.ctx, gatewayRotationEnrollment); err != nil {
		t.Fatalf("CreateEnrollment(gateway rotation): %v", err)
	}
	rotatedGatewayCredential, rotatedGatewayConfiguration, err := s.repository.ConsumeEnrollment(s.ctx, consumption(gatewayRotationEnrollment, s.suffix+"-gateway-certificate-rotated", gatewayRotationAt), s.snapshot, gatewayRotationAt.Add(5*time.Minute))
	s.err = err
	if s.err != nil || rotatedGatewayCredential.Generation != 2 || rotatedGatewayConfiguration.Desired.WireGuard == nil || len(rotatedGatewayConfiguration.Desired.WireGuard.Peers) != 0 {
		t.Fatalf("ConsumeEnrollment(gateway rotation) = credential %#v, configuration %#v, %v", rotatedGatewayCredential, rotatedGatewayConfiguration, s.err)
	}
	assertRevokedVPNSession(t, s.store, s.ctx, gatewayRotationConnection.SessionID)
	endpointAfterGatewayRotation, err := s.repository.EnsureConfiguration(s.ctx, s.runtimeID, s.snapshot, gatewayRotationAt, gatewayRotationAt.Add(5*time.Minute))
	s.err = err
	if s.err != nil || endpointAfterGatewayRotation.Desired.WireGuard != nil || endpointAfterGatewayRotation.Desired.AccessProfile != domainnetworkaccess.ProfileOnboarding {
		t.Fatalf("endpoint configuration after gateway rotation = %#v, %v", endpointAfterGatewayRotation, s.err)
	}
}

func (s *runtimeRepositoryScenario) checkPendingAndProxyOnly(t *testing.T) {
	t.Helper()
	pending := enrollment(s.suffix+"-3", "endpoint-pending-"+s.suffix, s.deviceID, s.subjectID, s.secondNow)
	if err := s.repository.CreateEnrollment(s.ctx, pending); err != nil {
		t.Fatalf("CreateEnrollment(pending): %v", err)
	}
	if err := s.repository.RevokeEnrollment(s.ctx, pending.ID, s.secondNow); err != nil {
		t.Fatalf("RevokeEnrollment(): %v", err)
	}
	if revoked, err := s.repository.GetEnrollment(s.ctx, pending.ID); err != nil || revoked.Status != domainnetworkruntime.EnrollmentRevoked {
		t.Fatalf("GetEnrollment(revoked) = %#v, %v", revoked, err)
	}

	mihomoOnlyRuntimeID, mihomoOnlyDeviceID := "endpoint-mihomo-"+s.suffix, "device-mihomo-"+s.suffix
	if err := s.store.Exec(s.ctx, `INSERT INTO public.network_access_devices (id, owner_user_id, name, hostname, platform, site_id, status, posture_status, created_at, updated_at) VALUES ($1, $2::uuid, $3, $4, 'windows', $5, 'active', 'compliant', $6, $6)`, mihomoOnlyDeviceID, s.subjectID, "Mihomo-only device "+s.suffix, mihomoOnlyRuntimeID, s.siteID, s.secondNow); err != nil {
		t.Fatalf("insert mihomo-only device: %v", err)
	}
	mihomoOnlyEnrollment := enrollment(s.suffix+"-mihomo-only", mihomoOnlyRuntimeID, mihomoOnlyDeviceID, s.subjectID, s.secondNow)
	if err := s.repository.CreateEnrollment(s.ctx, mihomoOnlyEnrollment); err != nil {
		t.Fatalf("CreateEnrollment(mihomo-only): %v", err)
	}
	mihomoOnlyConsumption := consumption(mihomoOnlyEnrollment, s.suffix+"-mihomo-only-certificate", s.secondNow)
	mihomoOnlyConsumption.Capabilities = []string{"mihomo"}
	if _, _, err := s.repository.ConsumeEnrollment(s.ctx, mihomoOnlyConsumption, s.snapshot, s.secondNow.Add(5*time.Minute)); err != nil {
		t.Fatalf("ConsumeEnrollment(mihomo-only): %v", err)
	}
	if err := s.store.Exec(s.ctx, `INSERT INTO public.network_mihomo_profiles
		(id, device_id, name, mode, status, mixed_port, controller_port, dns_mode, selector_group,
		 bypass_cidrs, bypass_hosts, fail_closed, created_at, updated_at)
		VALUES ($1, $2, $3, 'app_subscription', 'active', 7891, 9091, 'disabled', 'Local',
		 '[]'::jsonb, '[]'::jsonb, false, $4, $4)`, "mihomo-only-"+s.suffix, mihomoOnlyDeviceID, "Local mihomo "+s.suffix, s.secondNow); err != nil {
		t.Fatalf("insert mihomo-only profile: %v", err)
	}
	mihomoOnlyConfiguration, err := s.repository.EnsureConfiguration(s.ctx, mihomoOnlyRuntimeID, s.snapshot, s.secondNow, s.secondNow.Add(5*time.Minute))
	s.err = err
	if s.err != nil || mihomoOnlyConfiguration.Desired.WireGuard != nil || mihomoOnlyConfiguration.Desired.Mihomo == nil || mihomoOnlyConfiguration.Desired.Mihomo.Mode != domainnetworkaccess.MihomoModeAppSubscription {
		t.Fatalf("mihomo-only configuration = %#v, %v", mihomoOnlyConfiguration, s.err)
	}
}

func assertRevokedVPNSession(t *testing.T, store *dbstore.Store, ctx context.Context, sessionID string) {
	t.Helper()
	var peerStatus, sessionStatus string
	if err := store.SQLDB().QueryRowContext(ctx, `SELECT p.status, s.status FROM public.network_wireguard_peers p JOIN public.network_runtime_sessions s ON s.id = p.session_id WHERE p.session_id = $1`, sessionID).Scan(&peerStatus, &sessionStatus); err != nil || peerStatus != "revoked" || sessionStatus != "revoked" {
		t.Fatalf("rotated VPN session %s status = peer %q / session %q, %v", sessionID, peerStatus, sessionStatus, err)
	}
}

func enrollment(suffix, runtimeID, deviceID, subjectID string, createdAt time.Time) domainnetworkruntime.EnrollmentChallenge {
	return domainnetworkruntime.EnrollmentChallenge{
		ID: "enrollment-" + suffix, ChallengeID: "challenge-" + suffix, ChallengeHash: digest("token-" + suffix),
		RuntimeID: runtimeID, RuntimeKind: "endpoint", DeviceID: deviceID, SubjectID: subjectID,
		Status: domainnetworkruntime.EnrollmentPending, ExpiresAt: createdAt.Add(10 * time.Minute), CreatedBy: "integration-test", CreatedAt: createdAt,
	}
}

func consumption(challenge domainnetworkruntime.EnrollmentChallenge, certificate string, consumedAt time.Time) domainnetworkruntime.EnrollmentConsumption {
	serial := fmt.Sprintf("%x", sha256.Sum256([]byte(certificate)))[:40]
	wireGuardKey := sha256.Sum256([]byte(certificate + "-wireguard"))
	return domainnetworkruntime.EnrollmentConsumption{
		EnrollmentID: challenge.ID, ChallengeID: challenge.ChallengeID, TokenHash: challenge.ChallengeHash,
		RuntimeID: challenge.RuntimeID, RuntimeKind: challenge.RuntimeKind, DeviceID: challenge.DeviceID,
		CertificateFingerprint: digest(certificate), PublicKeyFingerprint: digest(certificate + "-key"), WireGuardPublicKey: base64.StdEncoding.EncodeToString(wireGuardKey[:]), CertificateSerial: serial,
		CertificateAuthorityKeyID: "0123456789abcdef0123456789abcdef01234567",
		Capabilities:              []string{"wireguard", "mihomo"}, NotBefore: consumedAt.Add(-time.Minute), ExpiresAt: consumedAt.Add(time.Hour), ConsumedAt: consumedAt,
	}
}

func accessGrantForConnection(connection domainnetworkruntime.VPNConnection, id, token string, createdAt, expiresAt time.Time) domainnetworkruntime.AccessGrant {
	return domainnetworkruntime.AccessGrant{
		ID: id, SubjectID: connection.SubjectID, AuthSessionID: "auth-session-" + id, DeviceID: connection.DeviceID,
		SiteID: connection.SiteID, NetworkSpaceID: connection.NetworkSpaceID, Mode: connection.Mode,
		ResourceIDs: append([]string(nil), connection.ResourceIDs...), PolicyVersion: connection.PolicyVersion,
		Status: domainnetworkruntime.AccessGrantIssued, TokenHash: digest(token), ReasonCode: "policy_allowed",
		ExpiresAt: expiresAt, CreatedBy: connection.SubjectID, CreatedAt: createdAt, UpdatedAt: createdAt, ResourceLeaseIDs: []string{},
	}
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("sha256:%x", sum)
}

func containsPrivateKey(value string) bool {
	return strings.Contains(strings.ToLower(value), "privatekey")
}
