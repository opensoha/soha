package networkaccess_test

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	config "github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	networkaccessrepo "github.com/opensoha/soha/internal/repository/networkaccess"
	"go.uber.org/zap"
)

func TestRepositoryWithPostgres(t *testing.T) {
	portText := os.Getenv("SOHA_NETWORK_ACCESS_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_NETWORK_ACCESS_TEST_POSTGRES_PORT to run the PostgreSQL integration test")
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
	migrationDir := filepath.Join(os.TempDir(), fmt.Sprintf("opensoha-network-access-migrations-%d", port))
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

	scenario := &accessRepositoryScenario{ctx: ctx, store: store}
	scenario.prepare(t)
	scenario.checkSiteAndGateway(t)
	scenario.checkNASBinding(t)
	scenario.checkProfileAndSession(t)
	scenario.checkSessionCommandAndSpaces(t)
	scenario.checkResourceAndSubject(t)
	scenario.checkDeviceRegistration(t)
	scenario.checkDevicePostureAndMihomo(t)
	scenario.checkPolicy(t)
	scenario.checkSnapshot(t)
	scenario.checkGatewayStatusAndDuplicateSite(t)
}

type accessRepositoryScenario struct {
	ctx              context.Context
	store            *dbstore.Store
	repository       *networkaccessrepo.Repository
	err              error
	suffix           string
	spaceCIDR        string
	overlayCIDR      string
	overlapHighCIDR  string
	overlapLowCIDR   string
	adjacentCIDR     string
	runtimeID        string
	sessionID        string
	userID           string
	teamID           string
	now              time.Time
	site             domainnetworkaccess.Site
	gateway          domainnetworkaccess.Gateway
	gateways         []domainnetworkaccess.Gateway
	nasBinding       domainnetworkaccess.NASBinding
	space            domainnetworkaccess.Space
	resource         domainnetworkaccess.Resource
	registeredDevice domainnetworkaccess.Device
	updatedDevice    domainnetworkaccess.Device
	policy           domainnetworkaccess.Policy
	compiledPolicies []domainnetworkaccess.Policy
	protectedIDs     []string
	snapshot         domainnetworkaccess.PolicySnapshot
}

func (s *accessRepositoryScenario) prepare(t *testing.T) {
	t.Helper()
	repository := networkaccessrepo.New(s.store.DB())
	s.repository = repository
	suffix := uuid.NewString()
	s.suffix = suffix
	networkSeed := sha256.Sum256([]byte(s.suffix))
	spaceCIDR := fmt.Sprintf("10.%d.%d.0/24", networkSeed[0], networkSeed[1])
	s.spaceCIDR = spaceCIDR
	overlayCIDR := fmt.Sprintf("100.%d.%d.0/24", 64+networkSeed[4]%64, networkSeed[5])
	s.overlayCIDR = overlayCIDR
	overlapHighCIDR := fmt.Sprintf("10.%d.%d.128/25", networkSeed[0], networkSeed[1])
	s.overlapHighCIDR = overlapHighCIDR
	overlapLowCIDR := fmt.Sprintf("10.%d.%d.0/25", networkSeed[0], networkSeed[1])
	s.overlapLowCIDR = overlapLowCIDR
	adjacentCIDR := fmt.Sprintf("172.%d.%d.0/24", 16+networkSeed[2]%16, networkSeed[3])
	s.adjacentCIDR = adjacentCIDR
	now := time.Now().UTC()
	s.now = now
}

func (s *accessRepositoryScenario) checkSiteAndGateway(t *testing.T) {
	t.Helper()
	site, err := s.repository.CreateSite(s.ctx, domainnetworkaccess.Site{
		ID: "site-" + s.suffix, Name: "HQ-" + s.suffix, Description: "headquarters", Location: "Shanghai",
		Status: domainnetworkaccess.StatusActive, CreatedAt: s.now, UpdatedAt: s.now,
	})
	s.site = site
	s.err = err
	if s.err != nil || s.site.Status != domainnetworkaccess.StatusActive {
		t.Fatalf("create site = %#v, %v", s.site, s.err)
	}
	gateway, err := s.repository.CreateGateway(s.ctx, domainnetworkaccess.Gateway{
		ID: "gateway-" + s.suffix, RuntimeID: "gateway-runtime-" + s.suffix, SiteID: s.site.ID, Name: "HQ VPN",
		AdministrativeStatus: domainnetworkaccess.StatusActive, Status: domainnetworkaccess.GatewayOffline,
		PublicEndpointHost: "vpn.example.test", PublicEndpointPort: 51820, OverlayCIDR: s.overlayCIDR,
		RoutingMode: domainnetworkaccess.GatewayRoutingRouted, MTU: 1420, PersistentKeepaliveSeconds: 25,
		DNSServers: []string{"10.0.0.53"}, Capabilities: []string{}, CreatedAt: s.now, UpdatedAt: s.now,
	})
	s.gateway = gateway
	s.err = err
	if s.err != nil || s.gateway.RuntimeID == "" || s.gateway.OverlayCIDR != s.overlayCIDR || s.gateway.AdministrativeStatus != domainnetworkaccess.StatusActive || len(s.gateway.DNSServers) != 1 {
		t.Fatalf("create gateway = %#v, %v", s.gateway, s.err)
	}
	s.gateway, s.err = s.repository.UpdateGateway(s.ctx, s.gateway.ID, domainnetworkaccess.GatewayInput{
		RuntimeID: s.gateway.RuntimeID, SiteID: s.site.ID, Name: "HQ VPN updated", AdministrativeStatus: domainnetworkaccess.StatusActive,
		PublicEndpointHost: "vpn2.example.test", PublicEndpointPort: 51821, OverlayCIDR: s.overlayCIDR,
		RoutingMode: domainnetworkaccess.GatewayRoutingSNAT, MTU: 1380, PersistentKeepaliveSeconds: 30,
		DNSServers: []string{"10.0.0.53", "10.0.0.54"},
	}, s.now.Add(time.Second))
	if s.err != nil || s.gateway.RoutingMode != domainnetworkaccess.GatewayRoutingSNAT || s.gateway.PublicEndpointPort != 51821 || len(s.gateway.DNSServers) != 2 {
		t.Fatalf("update gateway = %#v, %v", s.gateway, s.err)
	}
	gateways, err := s.repository.ListGateways(s.ctx, domainnetworkaccess.GatewayFilter{SiteID: s.site.ID, Limit: 10})
	s.gateways = gateways
	s.err = err
	if s.err != nil || len(s.gateways) != 1 || s.gateways[0].ID != s.gateway.ID {
		t.Fatalf("list gateways = %#v, %v", s.gateways, s.err)
	}
}

func (s *accessRepositoryScenario) checkNASBinding(t *testing.T) {
	t.Helper()
	runtimeID := "freeradius-" + s.suffix
	s.runtimeID = runtimeID
	fingerprint := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("certificate-"+s.suffix)))
	publicKeyFingerprint := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("public-key-"+s.suffix)))
	challengeHash := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("challenge-"+s.suffix)))
	if err := s.store.Exec(s.ctx, `INSERT INTO public.network_runtime_enrollments (id, challenge_id, challenge_hash, runtime_id, runtime_kind, device_id, subject_id, status, expires_at, consumed_at, created_by, created_at) VALUES ($1, $2, $3, $4, 'nas', $5, $6, 'consumed', $7, $8, $9, $8)`, "enrollment-"+s.suffix, "challenge-"+s.suffix, challengeHash, s.runtimeID, "nas-device-"+s.suffix, "nas-subject-"+s.suffix, s.now.Add(time.Hour), s.now, "operator-"+s.suffix); err != nil {
		t.Fatalf("insert NAS enrollment: %v", err)
	}
	if err := s.store.Exec(s.ctx, `INSERT INTO public.network_runtime_credentials (id, enrollment_id, runtime_id, runtime_kind, device_id, subject_id, certificate_fingerprint, public_key_fingerprint, certificate_serial, generation, capabilities, status, not_before, expires_at, created_at) VALUES ($1, $2, $3, 'nas', $4, $5, $6, $7, $8, 1, '["radius"]'::jsonb, 'active', $9, $10, $9)`, "credential-"+s.suffix, "enrollment-"+s.suffix, s.runtimeID, "nas-device-"+s.suffix, "nas-subject-"+s.suffix, fingerprint, publicKeyFingerprint, s.suffix, s.now.Add(-time.Minute), s.now.Add(time.Hour)); err != nil {
		t.Fatalf("insert NAS credential: %v", err)
	}
	nasBinding, err := s.repository.CreateNASBinding(s.ctx, domainnetworkaccess.NASBinding{ID: "nas-binding-" + s.suffix, NASID: "nas-" + s.suffix, RuntimeID: s.runtimeID, SiteID: s.site.ID, Name: "HQ Wi-Fi", AccessMedium: domainnetworkaccess.AccessMediumWiFi, DeviceType: domainnetworkaccess.AccessDeviceTypeWirelessController, SSID: "Soha-Staff", ManagementAddress: "10.0.10.2", Status: domainnetworkaccess.StatusActive, CoASupported: true, DisconnectSupported: true, CreatedAt: s.now, UpdatedAt: s.now})
	s.nasBinding = nasBinding
	s.err = err
	if s.err != nil || s.nasBinding.RuntimeID != s.runtimeID || s.nasBinding.AccessMedium != domainnetworkaccess.AccessMediumWiFi || s.nasBinding.DeviceType != domainnetworkaccess.AccessDeviceTypeWirelessController || s.nasBinding.SSID != "Soha-Staff" || s.nasBinding.ManagementAddress != "10.0.10.2" || !s.nasBinding.CoASupported {
		t.Fatalf("create NAS binding = %#v, %v", s.nasBinding, s.err)
	}
	bindings, err := s.repository.ListNASBindings(s.ctx, domainnetworkaccess.NASBindingFilter{RuntimeID: s.runtimeID, Limit: 10})
	s.err = err
	if s.err != nil || len(bindings) != 1 || bindings[0].NASID != s.nasBinding.NASID {
		t.Fatalf("list NAS bindings = %#v, %v", bindings, s.err)
	}
}

func (s *accessRepositoryScenario) checkProfileAndSession(t *testing.T) {
	t.Helper()
	profileBinding, err := s.repository.CreateSiteProfileBinding(s.ctx, domainnetworkaccess.SiteProfileBinding{ID: "profile-binding-" + s.suffix, SiteID: s.site.ID, AccessProfile: domainnetworkaccess.ProfileRestricted, VLANID: 30, FilterID: "soha-restricted", SessionTimeoutSeconds: 3600, CreatedAt: s.now, UpdatedAt: s.now})
	s.err = err
	if s.err != nil || profileBinding.VLANID != 30 || profileBinding.FilterID != "soha-restricted" {
		t.Fatalf("create site profile binding = %#v, %v", profileBinding, s.err)
	}
	profileBinding, s.err = s.repository.UpdateSiteProfileBinding(s.ctx, profileBinding.ID, domainnetworkaccess.SiteProfileBindingInput{SiteID: s.site.ID, AccessProfile: domainnetworkaccess.ProfileRestricted, SessionTimeoutSeconds: 600}, s.now.Add(time.Second))
	if s.err != nil || profileBinding.VLANID != 0 || profileBinding.FilterID != "" || profileBinding.SessionTimeoutSeconds != 600 {
		t.Fatalf("update site profile binding = %#v, %v", profileBinding, s.err)
	}
	sessionID := "nac-session-" + s.suffix
	s.sessionID = sessionID
	if err := s.store.Exec(s.ctx, `INSERT INTO public.network_runtime_sessions (id, runtime_id, subject_id, device_id, site_id, mode, access_profile, status, policy_version, configuration_version, posture_version, valid_until, nas_id, authentication_method, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, 'internal_direct', 'restricted', 'restricted', 1, 0, 0, $6, $7, 'password-compatible', $8, $8)`, s.sessionID, s.runtimeID, "subject-"+s.suffix, "device-"+s.suffix, s.site.ID, s.now.Add(time.Hour), s.nasBinding.NASID, s.now); err != nil {
		t.Fatalf("insert NAC session: %v", err)
	}
	sessions, err := s.repository.ListSessions(s.ctx, domainnetworkaccess.SessionFilter{RuntimeID: s.runtimeID, Status: "restricted", Limit: 10})
	s.err = err
	if s.err != nil || len(sessions) != 1 || sessions[0].ID != s.sessionID || sessions[0].Path != domainnetworkaccess.PathSiteDirect {
		t.Fatalf("list NAC sessions = %#v, %v", sessions, s.err)
	}
	for _, mode := range []string{domainnetworkaccess.ModeInternalZTNA, domainnetworkaccess.ModeExternalDirectZTNA} {
		if err := s.store.Exec(s.ctx, `UPDATE public.network_runtime_sessions SET mode = $1 WHERE id = $2`, mode, s.sessionID); err != nil {
			t.Fatalf("set session mode %s: %v", mode, err)
		}
		session, err := s.repository.GetSession(s.ctx, s.sessionID)
		if err != nil || session.Path != domainnetworkaccess.PathWireGuardZTNA {
			t.Fatalf("session mode %s path = %q, %v", mode, session.Path, err)
		}
	}
}

func (s *accessRepositoryScenario) checkSessionCommandAndSpaces(t *testing.T) {
	t.Helper()
	command := domainnetworkaccess.SessionCommand{
		ID: "session-command-" + s.suffix, SessionID: s.sessionID, RuntimeID: s.runtimeID, NASID: s.nasBinding.NASID,
		Action: domainnetworkaccess.SessionActionDisconnect, TargetAccessProfile: domainnetworkaccess.ProfileDeny,
		PolicyVersion: 1, Status: domainnetworkaccess.SessionCommandPending, ReasonCode: "manual_revoke",
		PlanHash:    fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("session-command-"+s.suffix))),
		EffectiveAt: s.now.Add(time.Second), ExpiresAt: s.now.Add(5 * time.Minute), CreatedAt: s.now,
	}
	createdCommand, err := s.repository.CreateSessionCommand(s.ctx, command)
	s.err = err
	if s.err != nil || createdCommand.ID != command.ID || createdCommand.RadiusAttributes != nil {
		t.Fatalf("create disconnect command = %#v, %v", createdCommand, s.err)
	}
	retryCommand := command
	retryCommand.ID = "different-id-" + s.suffix
	retriedCommand, err := s.repository.CreateSessionCommand(s.ctx, retryCommand)
	s.err = err
	if s.err != nil || retriedCommand.ID != command.ID {
		t.Fatalf("idempotent disconnect command = %#v, %v", retriedCommand, s.err)
	}
	space, err := s.repository.CreateSpace(s.ctx, domainnetworkaccess.Space{
		ID: "space-" + s.suffix, SiteID: s.site.ID, Name: "corp", CIDRs: []string{s.spaceCIDR},
		Status: domainnetworkaccess.StatusActive, CreatedAt: s.now, UpdatedAt: s.now,
	})
	s.space = space
	s.err = err
	if s.err != nil || len(s.space.CIDRs) != 1 || s.space.CIDRs[0] != s.spaceCIDR {
		t.Fatalf("create space = %#v, %v", s.space, s.err)
	}
	_, s.err = s.repository.CreateSpace(s.ctx, domainnetworkaccess.Space{
		ID: "gateway-overlap-space-" + s.suffix, SiteID: s.site.ID, Name: "gateway-overlap", CIDRs: []string{s.overlayCIDR},
		Status: domainnetworkaccess.StatusActive, CreatedAt: s.now, UpdatedAt: s.now,
	})
	if !errors.Is(s.err, apperrors.ErrConflict) {
		t.Fatalf("gateway-overlapping space create error = %v, want conflict", s.err)
	}
	_, s.err = s.repository.CreateSpace(s.ctx, domainnetworkaccess.Space{
		ID: "overlap-space-" + s.suffix, SiteID: s.site.ID, Name: "overlap", CIDRs: []string{s.overlapHighCIDR},
		Status: domainnetworkaccess.StatusActive, CreatedAt: s.now, UpdatedAt: s.now,
	})
	if !errors.Is(s.err, apperrors.ErrConflict) {
		t.Fatalf("overlapping space create error = %v, want conflict", s.err)
	}
	adjacentSpace, err := s.repository.CreateSpace(s.ctx, domainnetworkaccess.Space{
		ID: "adjacent-space-" + s.suffix, SiteID: s.site.ID, Name: "adjacent", CIDRs: []string{s.adjacentCIDR},
		Status: domainnetworkaccess.StatusActive, CreatedAt: s.now, UpdatedAt: s.now,
	})
	s.err = err
	if s.err != nil {
		t.Fatalf("create adjacent space: %v", s.err)
	}
	_, s.err = s.repository.UpdateSpace(s.ctx, adjacentSpace.ID, domainnetworkaccess.SpaceInput{SiteID: s.site.ID, Name: adjacentSpace.Name, CIDRs: []string{s.overlapLowCIDR}, Status: domainnetworkaccess.StatusActive}, s.now.Add(time.Second))
	if !errors.Is(s.err, apperrors.ErrConflict) {
		t.Fatalf("overlapping space update error = %v, want conflict", s.err)
	}
}

func (s *accessRepositoryScenario) checkResourceAndSubject(t *testing.T) {
	t.Helper()
	resource, err := s.repository.CreateResource(s.ctx, domainnetworkaccess.Resource{
		ID: "resource-" + s.suffix, SpaceID: s.space.ID, Name: "git", Kind: "fqdn", Target: "git.corp.example",
		Protocol: "tcp", Ports: []int{443}, Protected: true, PathMode: domainnetworkaccess.PathAccessProxy,
		CreatedAt: s.now, UpdatedAt: s.now,
	})
	s.resource = resource
	s.err = err
	if s.err != nil || len(s.resource.Ports) != 1 || s.resource.Ports[0] != 443 || !s.resource.Protected {
		t.Fatalf("create resource = %#v, %v", s.resource, s.err)
	}

	userID := uuid.NewString()
	s.userID = userID
	if err := s.store.Exec(s.ctx, `INSERT INTO public.users (id, username, email, tags) VALUES ($1::uuid, $2, $3, $4::json)`, s.userID, "network-test-"+s.suffix, s.suffix+"@example.test", `["employee"]`); err != nil {
		t.Fatalf("insert test user: %v", err)
	}
	teamID := "team-" + s.suffix
	s.teamID = teamID
	if err := s.store.Exec(s.ctx, `INSERT INTO public.teams (id, name, slug) VALUES ($1, $2, $3)`, s.teamID, "Engineering-"+s.suffix, "engineering-"+s.suffix); err != nil {
		t.Fatalf("insert test team: %v", err)
	}
	if err := s.store.Exec(s.ctx, `INSERT INTO public.user_team_bindings (id, user_id, team_id) VALUES ($1, $2::uuid, $3)`, "binding-"+s.suffix, s.userID, s.teamID); err != nil {
		t.Fatalf("insert test team binding: %v", err)
	}
	subject, err := s.repository.GetSubject(s.ctx, s.userID)
	s.err = err
	if s.err != nil || subject.Status != domainnetworkaccess.StatusActive || fmt.Sprint(subject.Teams) != "["+s.teamID+"]" || fmt.Sprint(subject.Tags) != "[employee]" {
		t.Fatalf("get subject = %#v, %v", subject, s.err)
	}
}

func (s *accessRepositoryScenario) checkDeviceRegistration(t *testing.T) {
	t.Helper()
	registeredDevice, err := s.repository.RegisterDevice(s.ctx, domainnetworkaccess.Device{
		ID: "device-" + s.suffix, OwnerUserID: s.userID, Name: "Laptop", Hostname: "laptop-1", Platform: "windows",
		DeviceType: domainnetworkaccess.DeviceTypeUnknown, OwnershipType: domainnetworkaccess.DeviceOwnershipUnassigned,
		ReportedFacts: &domainnetworkaccess.DeviceReportedFacts{Architecture: "amd64", AgentVersion: "0.2.0", CollectedAt: s.now, NetworkInterfaces: []domainnetworkaccess.DeviceNetworkInterface{{Name: "Ethernet", Kind: domainnetworkaccess.NetworkInterfaceKindPhysical, Status: domainnetworkaccess.NetworkInterfaceStatusUp, IPv4Addresses: []string{"10.0.12.44"}}}},
		LastSeenAt:    &s.now, CreatedAt: s.now, UpdatedAt: s.now,
	})
	s.registeredDevice = registeredDevice
	s.err = err
	if s.err != nil || s.registeredDevice.Status != domainnetworkaccess.DeviceStatusPending || s.registeredDevice.PostureStatus != domainnetworkaccess.PostureUnknown || s.registeredDevice.DeviceType != domainnetworkaccess.DeviceTypeUnknown || s.registeredDevice.ReportedFacts == nil || len(s.registeredDevice.ReportedFacts.NetworkInterfaces) != 1 {
		t.Fatalf("register endpoint device = %#v, %v", s.registeredDevice, s.err)
	}
	s.registeredDevice.DeviceType, s.registeredDevice.UpdatedAt = domainnetworkaccess.DeviceTypeLaptop, s.now.Add(500*time.Millisecond)
	s.registeredDevice, s.err = s.repository.RegisterDevice(s.ctx, s.registeredDevice)
	if s.err != nil || s.registeredDevice.DeviceType != domainnetworkaccess.DeviceTypeLaptop {
		t.Fatalf("identify endpoint device type = %#v, %v", s.registeredDevice, s.err)
	}
	s.registeredDevice, s.err = s.repository.UpdateDevice(s.ctx, s.registeredDevice.ID, domainnetworkaccess.DeviceInput{Name: s.registeredDevice.Name, SiteID: s.site.ID, Status: domainnetworkaccess.DeviceStatusActive, PostureStatus: domainnetworkaccess.PostureNoncompliant, DeviceType: domainnetworkaccess.DeviceTypeDesktop, OwnershipType: domainnetworkaccess.DeviceOwnershipCompany}, s.now.Add(time.Second))
	if s.err != nil {
		t.Fatalf("admit endpoint device: %v", s.err)
	}
	s.registeredDevice.Name, s.registeredDevice.Hostname, s.registeredDevice.Platform, s.registeredDevice.UpdatedAt = "Renamed Laptop", "renamed-laptop", "darwin", s.now.Add(2*time.Second)
	s.registeredDevice.DeviceType = domainnetworkaccess.DeviceTypeLaptop
	s.registeredDevice.LastSeenAt = &s.registeredDevice.UpdatedAt
	s.registeredDevice, s.err = s.repository.RegisterDevice(s.ctx, s.registeredDevice)
	if s.err != nil || s.registeredDevice.Status != domainnetworkaccess.DeviceStatusActive || s.registeredDevice.PostureStatus != domainnetworkaccess.PostureNoncompliant || s.registeredDevice.Name != "Renamed Laptop" || s.registeredDevice.DeviceType != domainnetworkaccess.DeviceTypeDesktop || s.registeredDevice.OwnershipType != domainnetworkaccess.DeviceOwnershipCompany {
		t.Fatalf("re-register endpoint device = %#v, %v", s.registeredDevice, s.err)
	}
}

func (s *accessRepositoryScenario) checkDevicePostureAndMihomo(t *testing.T) {
	t.Helper()
	devices, err := s.repository.ListDevices(s.ctx, domainnetworkaccess.DeviceFilter{OwnerUserID: s.userID, Limit: 10})
	s.err = err
	if s.err != nil || len(devices) != 1 || devices[0].PostureStatus != domainnetworkaccess.PostureNoncompliant || devices[0].PostureVersion != 2 {
		t.Fatalf("list devices = %#v, %v", devices, s.err)
	}
	updatedDevice, err := s.repository.UpdateDevice(s.ctx, devices[0].ID, domainnetworkaccess.DeviceInput{Name: devices[0].Name, SiteID: s.site.ID, Status: devices[0].Status, PostureStatus: domainnetworkaccess.PostureCompliant}, s.now.Add(3*time.Second))
	s.updatedDevice = updatedDevice
	s.err = err
	if s.err != nil || s.updatedDevice.PostureStatus != domainnetworkaccess.PostureCompliant || s.updatedDevice.PostureVersion != 3 {
		t.Fatalf("update device posture = %#v, %v", s.updatedDevice, s.err)
	}
	mihomoProfile, err := s.repository.CreateMihomoProfile(s.ctx, domainnetworkaccess.MihomoProfile{
		ID: "mihomo-" + s.suffix, DeviceID: s.updatedDevice.ID, Name: "Managed proxy", Mode: domainnetworkaccess.MihomoModeManagedFollow,
		Status: domainnetworkaccess.StatusActive, SubscriptionURLCiphertext: "encrypted-subscription", Revision: 1,
		MixedPort: 7890, ControllerPort: 9090, DNSMode: domainnetworkaccess.MihomoDNSFakeIP,
		FakeIPRange: "198.18.0.0/16", SelectorGroup: "SOHA", SelectedProxy: "Hong Kong",
		BypassCIDRs: []string{"10.0.0.0/8"}, BypassHosts: []string{"control.example.test"}, FailClosed: true,
		CreatedAt: s.now, UpdatedAt: s.now,
	})
	s.err = err
	if s.err != nil || !mihomoProfile.SubscriptionConfigured || mihomoProfile.SubscriptionURLCiphertext != "encrypted-subscription" || mihomoProfile.Revision != 1 {
		t.Fatalf("create mihomo profile = %#v, %v", mihomoProfile, s.err)
	}
	mihomoProfile.Mode, mihomoProfile.SubscriptionURLCiphertext, mihomoProfile.SelectedProxy = domainnetworkaccess.MihomoModeAppSubscription, "", ""
	mihomoProfile.DNSMode, mihomoProfile.FakeIPRange, mihomoProfile.FailClosed = domainnetworkaccess.MihomoDNSDisabled, "", false
	mihomoProfile, s.err = s.repository.UpdateMihomoProfile(s.ctx, mihomoProfile.ID, mihomoProfile, s.now.Add(time.Second))
	if s.err != nil || mihomoProfile.SubscriptionConfigured || mihomoProfile.Revision != 2 || mihomoProfile.Mode != domainnetworkaccess.MihomoModeAppSubscription {
		t.Fatalf("update mihomo profile = %#v, %v", mihomoProfile, s.err)
	}
	mihomoProfiles, err := s.repository.ListMihomoProfiles(s.ctx, domainnetworkaccess.MihomoProfileFilter{DeviceID: s.updatedDevice.ID, Limit: 10})
	s.err = err
	if s.err != nil || len(mihomoProfiles) != 1 || mihomoProfiles[0].ID != mihomoProfile.ID {
		t.Fatalf("list mihomo profiles = %#v, %v", mihomoProfiles, s.err)
	}
}

func (s *accessRepositoryScenario) checkPolicy(t *testing.T) {
	t.Helper()
	policy, err := s.repository.CreatePolicy(s.ctx, domainnetworkaccess.Policy{
		ID: "policy-" + s.suffix, Name: "Engineering access " + s.suffix, Enabled: true, Priority: 100,
		Effect: domainnetworkaccess.PolicyEffectAllow, Subjects: domainnetworkaccess.PolicySubjects{Teams: []string{s.teamID}},
		SiteIDs: []string{s.site.ID}, ResourceIDs: []string{s.resource.ID}, Modes: []string{domainnetworkaccess.ModeInternalZTNA},
		DeviceStatuses: []string{domainnetworkaccess.DeviceStatusActive}, PostureStatuses: []string{domainnetworkaccess.PostureCompliant},
		AccessProfile: domainnetworkaccess.ProfileFull, Version: 1, CreatedAt: s.now, UpdatedAt: s.now,
	})
	s.policy = policy
	s.err = err
	if s.err != nil || s.policy.Version != 1 || fmt.Sprint(s.policy.Subjects.Teams) != "["+s.teamID+"]" {
		t.Fatalf("create policy = %#v, %v", s.policy, s.err)
	}
	s.policy, s.err = s.repository.UpdatePolicy(s.ctx, s.policy.ID, domainnetworkaccess.PolicyInput{
		Name: s.policy.Name, Enabled: true, Priority: 90, Effect: s.policy.Effect, Subjects: s.policy.Subjects,
		SiteIDs: s.policy.SiteIDs, ResourceIDs: s.policy.ResourceIDs, Modes: s.policy.Modes,
		DeviceStatuses: s.policy.DeviceStatuses, PostureStatuses: s.policy.PostureStatuses, AccessProfile: s.policy.AccessProfile,
	}, s.now.Add(time.Second))
	if s.err != nil || s.policy.Version != 2 || s.policy.Priority != 90 {
		t.Fatalf("update policy = %#v, %v", s.policy, s.err)
	}
	policies, err := s.repository.ListPolicies(s.ctx, domainnetworkaccess.PolicyFilter{Enabled: boolPointer(true), Limit: 10})
	s.err = err
	if s.err != nil || !containsPolicy(policies, s.policy.ID) {
		t.Fatalf("list policies = %#v, %v", policies, s.err)
	}
	compiledPolicies, err := s.repository.ListPoliciesForSnapshot(s.ctx)
	s.compiledPolicies = compiledPolicies
	s.err = err
	if s.err != nil || !containsPolicy(s.compiledPolicies, s.policy.ID) {
		t.Fatalf("list policies for snapshot = %#v, %v", s.compiledPolicies, s.err)
	}
	protectedIDs, err := s.repository.ListProtectedResourceIDs(s.ctx)
	s.protectedIDs = protectedIDs
	s.err = err
	if s.err != nil || !containsString(s.protectedIDs, s.resource.ID) {
		t.Fatalf("list protected resources = %#v, %v", s.protectedIDs, s.err)
	}
}

func (s *accessRepositoryScenario) checkSnapshot(t *testing.T) {
	t.Helper()
	contentHash := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(s.suffix)))
	snapshot, err := s.repository.PublishPolicySnapshot(s.ctx, domainnetworkaccess.PolicySnapshot{
		ContentHash: contentHash, PolicyCount: len(s.compiledPolicies), ProtectedResourceCount: len(s.protectedIDs),
		PublishedAt: s.now, Policies: s.compiledPolicies, ProtectedResourceIDs: s.protectedIDs,
	})
	s.snapshot = snapshot
	s.err = err
	if s.err != nil || s.snapshot.PolicyVersion < 1 || s.snapshot.ContentHash != contentHash {
		t.Fatalf("publish snapshot = %#v, %v", s.snapshot, s.err)
	}
	sameSnapshot, err := s.repository.PublishPolicySnapshot(s.ctx, s.snapshot)
	s.err = err
	if s.err != nil || sameSnapshot.PolicyVersion != s.snapshot.PolicyVersion {
		t.Fatalf("republish snapshot = %#v, %v", sameSnapshot, s.err)
	}
	changedHash := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(s.suffix+"-changed")))
	changedSnapshot := s.snapshot
	changedSnapshot.ContentHash = changedHash
	changedSnapshot.PublishedAt = s.now.Add(2 * time.Second)
	changedSnapshot, s.err = s.repository.PublishPolicySnapshot(s.ctx, changedSnapshot)
	if s.err != nil || changedSnapshot.PolicyVersion <= s.snapshot.PolicyVersion {
		t.Fatalf("publish changed snapshot = %#v, %v", changedSnapshot, s.err)
	}
	latestSnapshot, err := s.repository.GetPolicySnapshot(s.ctx)
	s.err = err
	if s.err != nil || latestSnapshot.PolicyVersion != changedSnapshot.PolicyVersion || !containsPolicy(latestSnapshot.Policies, s.policy.ID) {
		t.Fatalf("get latest snapshot = %#v, %v", latestSnapshot, s.err)
	}
	conflictRanges, err := s.repository.ListConflictRanges(s.ctx)
	s.err = err
	if s.err != nil || !containsCIDR(conflictRanges, s.space.ID, s.spaceCIDR) || !containsCIDR(conflictRanges, s.gateway.ID, s.overlayCIDR) {
		t.Fatalf("list conflict ranges = %#v, %v", conflictRanges, s.err)
	}
}

func (s *accessRepositoryScenario) checkGatewayStatusAndDuplicateSite(t *testing.T) {
	t.Helper()
	if err := s.store.Exec(s.ctx, `UPDATE public.network_access_gateways SET status = $1, version = $2, capabilities = $3::jsonb, policy_version = $4 WHERE id = $5`, domainnetworkaccess.GatewayOnline, "0.1.0", `["wireguard","ztna"]`, 7, s.gateway.ID); err != nil {
		t.Fatalf("update gateway runtime status: %v", err)
	}
	s.gateways, s.err = s.repository.ListGateways(s.ctx, domainnetworkaccess.GatewayFilter{SiteID: s.site.ID, Status: domainnetworkaccess.GatewayOnline, Limit: 10})
	if s.err != nil || len(s.gateways) != 1 || fmt.Sprint(s.gateways[0].Capabilities) != "[wireguard ztna]" || s.gateways[0].RuntimeID != s.gateway.RuntimeID {
		t.Fatalf("list gateways = %#v, %v", s.gateways, s.err)
	}

	_, s.err = s.repository.CreateSite(s.ctx, domainnetworkaccess.Site{ID: "duplicate-" + s.suffix, Name: s.site.Name, Status: domainnetworkaccess.StatusActive, CreatedAt: s.now, UpdatedAt: s.now})
	if !errors.Is(s.err, apperrors.ErrConflict) {
		t.Fatalf("duplicate site error = %v, want conflict", s.err)
	}
}

func boolPointer(value bool) *bool { return &value }

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func containsPolicy(values []domainnetworkaccess.Policy, id string) bool {
	for _, value := range values {
		if value.ID == id {
			return true
		}
	}
	return false
}

func containsCIDR(values []domainnetworkaccess.ConflictRange, sourceID, cidr string) bool {
	for _, value := range values {
		if value.SourceID == sourceID && value.CIDR == cidr {
			return true
		}
	}
	return false
}
