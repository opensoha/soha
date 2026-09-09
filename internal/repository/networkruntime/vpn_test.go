package networkruntime

import (
	"encoding/json"
	"errors"
	"net/netip"
	"slices"
	"testing"
	"time"

	domainnetworkaccess "github.com/opensoha/soha/internal/domain/networkaccess"
	domainnetworkruntime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestAccessGrantMatchesEveryBoundDimension(t *testing.T) {
	now := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	tokenHash := "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	resources, _ := json.Marshal([]string{"resource-b", "resource-a"})
	row := accessGrantRow{
		SubjectID: "user-1", DeviceID: "device-1", SiteID: "site-1", NetworkSpaceID: "space-1",
		Mode: domainnetworkaccess.ModeExternalVPNZTNA, ResourceIDs: resources, PolicyVersion: 7,
		Status: domainnetworkruntime.AccessGrantIssued, TokenHash: &tokenHash, ExpiresAt: now.Add(time.Minute),
	}
	connection := domainnetworkruntime.VPNConnection{
		SubjectID: "user-1", DeviceID: "device-1", SiteID: "site-1", NetworkSpaceID: "space-1",
		Mode: domainnetworkaccess.ModeExternalVPNZTNA, ResourceIDs: []string{"resource-a", "resource-b"},
		PolicyVersion: 7, AccessGrantTokenHash: tokenHash, CreatedAt: now,
	}
	if !accessGrantMatches(row, connection) {
		t.Fatal("matching access grant was rejected")
	}

	for name, mutate := range map[string]func(*domainnetworkruntime.VPNConnection){
		"token": func(value *domainnetworkruntime.VPNConnection) {
			value.AccessGrantTokenHash = "sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		},
		"subject": func(value *domainnetworkruntime.VPNConnection) { value.SubjectID = "user-2" },
		"device":  func(value *domainnetworkruntime.VPNConnection) { value.DeviceID = "device-2" },
		"site":    func(value *domainnetworkruntime.VPNConnection) { value.SiteID = "site-2" },
		"space":   func(value *domainnetworkruntime.VPNConnection) { value.NetworkSpaceID = "space-2" },
		"mode": func(value *domainnetworkruntime.VPNConnection) {
			value.Mode = domainnetworkaccess.ModeExternalDirectZTNA
		},
		"resource": func(value *domainnetworkruntime.VPNConnection) { value.ResourceIDs = []string{"resource-a"} },
		"duplicate resource": func(value *domainnetworkruntime.VPNConnection) {
			value.ResourceIDs = []string{"resource-a", "resource-a"}
		},
		"policy": func(value *domainnetworkruntime.VPNConnection) { value.PolicyVersion = 8 },
		"expiry": func(value *domainnetworkruntime.VPNConnection) { value.CreatedAt = row.ExpiresAt },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := connection
			mutate(&candidate)
			if accessGrantMatches(row, candidate) {
				t.Fatal("mismatched access grant was accepted")
			}
		})
	}
}

func TestAllocateOverlayAddressReservesNetworkGatewayAndBroadcast(t *testing.T) {
	prefix := netip.MustParsePrefix("100.96.0.0/29")
	address, err := allocateOverlayAddress(prefix, map[netip.Addr]struct{}{
		netip.MustParseAddr("100.96.0.2"): {},
	})
	if err != nil || address.String() != "100.96.0.3" {
		t.Fatalf("allocateOverlayAddress() = %s, %v", address, err)
	}

	_, err = allocateOverlayAddress(netip.MustParsePrefix("100.96.1.0/30"), map[netip.Addr]struct{}{
		netip.MustParseAddr("100.96.1.2"): {},
	})
	if err == nil {
		t.Fatal("allocateOverlayAddress() unexpectedly allocated an exhausted /30")
	}
}

func TestLeaseFirewallRulesAuthorizeResourceBeforeProtectedSet(t *testing.T) {
	expiresAt := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	rules := leaseFirewallRules(
		"100.96.0.2/32",
		[]networkprotocol.NetworkLease{{ID: "network-lease", CIDRs: []string{"10.20.0.0/16"}, ExpiresAt: expiresAt}},
		[]leasedResourceTarget{{ResourceID: "resource-db", DestinationCIDR: "10.20.8.10/32", Protocol: "tcp", Ports: []int{5432}, LeaseID: "resource-lease", ExpiresAt: expiresAt}},
		[]string{"10.20.8.10/32"},
	)
	if len(rules) != 3 || rules[0].Effect != "allow" || rules[0].LeaseID != "resource-lease" || rules[0].Protocol != "tcp" || len(rules[0].Ports) != 1 || rules[0].Ports[0] != 5432 || rules[1].Effect != "deny" || rules[2].Effect != "allow" || rules[2].LeaseID != "network-lease" {
		t.Fatalf("leaseFirewallRules() = %#v", rules)
	}

	direct := leaseFirewallRules("100.96.0.3/32", nil, []leasedResourceTarget{{DestinationCIDR: "10.20.8.10/32", Protocol: "tcp", Ports: []int{5432}, LeaseID: "resource-lease", ExpiresAt: expiresAt}}, []string{"10.20.8.10/32"})
	if len(direct) != 2 || direct[0].Effect != "allow" || direct[1].Effect != "deny" {
		t.Fatalf("direct ZTNA rules = %#v", direct)
	}
}

func TestCompileGatewaySitePlanBuildsHubAndSpokeWithoutDirectSpokePeer(t *testing.T) {
	now := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	members := []gatewayTopologyMember{
		{gateway: gatewayRuntimeRow{ID: "gateway-a", RuntimeID: "runtime-a", SiteID: "site-a", OverlayCIDR: "100.96.0.0/24", AdvertisedCIDRs: []string{"10.10.0.0/16"}}, credential: credentialRow{WireGuardPublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", ExpiresAt: now.Add(time.Hour)}},
		{gateway: gatewayRuntimeRow{ID: "gateway-b", RuntimeID: "runtime-b", SiteID: "site-b", HubGatewayID: "gateway-a", OverlayCIDR: "100.97.0.0/24", AdvertisedCIDRs: []string{"10.20.0.0/16"}, PersistentKeepaliveSeconds: 25, PublicEndpointHost: "b.example.test", PublicEndpointPort: 51821}, credential: credentialRow{WireGuardPublicKey: "BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB=", ExpiresAt: now.Add(time.Hour)}},
		{gateway: gatewayRuntimeRow{ID: "gateway-c", RuntimeID: "runtime-c", SiteID: "site-c", HubGatewayID: "gateway-a", OverlayCIDR: "100.98.0.0/24", AdvertisedCIDRs: []string{"10.30.0.0/16"}, PersistentKeepaliveSeconds: 25, PublicEndpointHost: "c.example.test", PublicEndpointPort: 51822}, credential: credentialRow{WireGuardPublicKey: "CCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCCC=", ExpiresAt: now.Add(time.Hour)}},
	}

	hub, err := compileGatewaySitePlan(members[0].gateway, members, []string{"10.30.8.10/32"}, now.Add(5*time.Minute))
	if err != nil || len(hub.peers) != 2 || hub.peers[0].RuntimeID != "runtime-b" || hub.peers[1].RuntimeID != "runtime-c" {
		t.Fatalf("hub plan = %#v, %v", hub, err)
	}
	for _, peer := range hub.peers {
		if peer.RuntimeID == "runtime-b" && slices.Contains(peer.AllowedIPs, "10.30.0.0/16") {
			t.Fatal("hub-to-B peer must not own C routes")
		}
	}

	spoke, err := compileGatewaySitePlan(members[1].gateway, members, []string{"10.30.8.10/32"}, now.Add(5*time.Minute))
	if err != nil || len(spoke.peers) != 1 || spoke.peers[0].RuntimeID != "runtime-a" || !slices.Contains(spoke.peers[0].AllowedIPs, "10.30.0.0/16") || slices.Contains(spoke.peers[0].AllowedIPs, "10.20.0.0/16") {
		t.Fatalf("spoke plan = %#v, %v", spoke, err)
	}
	assertSpokeFirewallRules(t, spoke.firewallRules)
}

func TestRoutesCoverAllRequiresEveryTargetPrefix(t *testing.T) {
	if !routesCoverAll([]string{"10.10.0.0/16", "10.20.8.0/24"}, []string{"10.10.4.0/24", "10.20.8.10/32"}) {
		t.Fatal("covered target prefixes were rejected")
	}
	if routesCoverAll([]string{"10.10.0.0/16"}, []string{"10.10.4.0/24", "10.20.8.10/32"}) {
		t.Fatal("partially covered target prefixes were accepted")
	}
}

func TestCollapseRoutesDropsResourceRouteCoveredByNetworkLease(t *testing.T) {
	got := collapseRoutes([]string{"10.20.8.10/32", "10.20.0.0/16", "10.30.1.0/24", "10.30.1.0/24"})
	if len(got) != 2 || got[0] != "10.20.0.0/16" || got[1] != "10.30.1.0/24" {
		t.Fatalf("collapseRoutes() = %v", got)
	}
}

func TestMihomoDesiredMergesWireGuardBypassWithoutOwningRoutes(t *testing.T) {
	profile := &domainnetworkaccess.MihomoProfile{
		ID: "mihomo-1", Mode: domainnetworkaccess.MihomoModeManagedFollow, Revision: 3,
		MixedPort: 7890, ControllerPort: 9090, DNSMode: domainnetworkaccess.MihomoDNSDisabled,
		SelectorGroup: "Soha", SelectedProxy: "edge-a", BypassCIDRs: []string{"192.168.0.0/16"},
		BypassHosts: []string{"control.example.com"}, FailClosed: true,
	}
	wireGuard := &networkprotocol.WireGuardConfiguration{Routes: []string{"10.20.0.0/16"}}

	got, err := mihomoDesired(profile, wireGuard, "vpn.example.com")
	if err != nil {
		t.Fatal(err)
	}
	if got.ProfileID != profile.ID || got.ProfileRevision != 3 || !slices.Equal(got.BypassCIDRs, []string{"10.20.0.0/16", "192.168.0.0/16"}) || !slices.Equal(got.BypassHosts, []string{"control.example.com", "vpn.example.com"}) {
		t.Fatalf("mihomoDesired() = %#v", got)
	}
	if !slices.Equal(wireGuard.Routes, []string{"10.20.0.0/16"}) {
		t.Fatalf("mihomo mutated WireGuard routes: %v", wireGuard.Routes)
	}
}

func TestMihomoDesiredRejectsWireGuardDNSAndFakeIPOverlap(t *testing.T) {
	profile := &domainnetworkaccess.MihomoProfile{
		ID: "mihomo-1", Mode: domainnetworkaccess.MihomoModeManagedFollow, Revision: 1,
		MixedPort: 7890, ControllerPort: 9090, DNSMode: domainnetworkaccess.MihomoDNSFakeIP,
		FakeIPRange: "198.18.0.0/16", SelectorGroup: "Soha", SelectedProxy: "edge-a", FailClosed: true,
	}

	for name, wireGuard := range map[string]*networkprotocol.WireGuardConfiguration{
		"dns":     {Routes: []string{"10.20.0.0/16"}, DNSServers: []string{"10.20.0.53"}},
		"overlap": {Routes: []string{"198.18.10.0/24"}},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := mihomoDesired(profile, wireGuard, ""); !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("mihomoDesired() error = %v", err)
			}
		})
	}
}

func assertSpokeFirewallRules(t *testing.T, rules []networkprotocol.WireGuardFirewallRule) {
	t.Helper()
	hasFrom, hasTo, hasProtectedDeny := false, false, false
	for _, rule := range rules {
		hasFrom = hasFrom || rule.Direction == "from_wireguard"
		hasTo = hasTo || rule.Direction == "to_wireguard"
		hasProtectedDeny = hasProtectedDeny || rule.Effect == "deny" && rule.DestinationCIDR == "10.30.8.10/32"
	}
	if !hasFrom || !hasTo || !hasProtectedDeny {
		t.Fatalf("spoke firewall rules = %#v", rules)
	}
}
