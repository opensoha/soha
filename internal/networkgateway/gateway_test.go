package networkgateway

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/networkprotocol"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

func TestLoadOrCreatePrivateKeyPersistsOneProtectedKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wireguard.key")
	first, err := LoadOrCreatePrivateKey(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := LoadOrCreatePrivateKey(path)
	if err != nil || first != second {
		t.Fatalf("second key = %v, %v; want original", second.PublicKey(), err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("key mode = %v, %v; want 0600", info.Mode().Perm(), err)
	}
}

func TestLoadOrCreatePrivateKeyRejectsUnsafeStateDirectory(t *testing.T) {
	directory := t.TempDir()
	// #nosec G302 -- verifies rejection of an unsafe temporary directory.
	if err := os.Chmod(directory, 0o777); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreatePrivateKey(filepath.Join(directory, "wireguard.key")); err == nil {
		t.Fatal("world-writable state directory must fail")
	}
}

func TestGatewayDesiredValidationAndFirewallPlanFailClosed(t *testing.T) {
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	desired := testGatewayDesired(privateKey.PublicKey().String(), now)
	if err := ValidateGatewayDesired(desired, privateKey.PublicKey(), now, ""); err != nil {
		t.Fatalf("valid desired rejected: %v", err)
	}
	program, err := BuildNftProgram(*desired.WireGuard, false, "")
	if err != nil {
		t.Fatal(err)
	}
	deny := strings.Index(program, `comment "deny-protected"`)
	allow := strings.Index(program, `comment "allow-network"`)
	if deny < 0 || allow < 0 || deny > allow || strings.Contains(program, "masquerade") {
		t.Fatalf("routed nft program is not deny-first: %s", program)
	}

	snat := desired
	snatWireGuard := *desired.WireGuard
	snat.WireGuard = &snatWireGuard
	snat.WireGuard.RoutingMode = "snat"
	if err := ValidateGatewayDesired(snat, privateKey.PublicKey(), now, ""); err == nil {
		t.Fatal("SNAT without an egress interface must fail closed")
	}
	snatProgram, err := BuildNftProgram(*snat.WireGuard, false, "eth0")
	if err != nil || !strings.Contains(snatProgram, `oifname "eth0" masquerade`) {
		t.Fatalf("SNAT program = %q, %v", snatProgram, err)
	}

	defaultRoute := desired
	defaultRouteWireGuard := *desired.WireGuard
	defaultRoute.WireGuard = &defaultRouteWireGuard
	defaultRoute.WireGuard.Routes = []string{"0.0.0.0/0"}
	if err := ValidateGatewayDesired(defaultRoute, privateKey.PublicKey(), now, ""); err == nil {
		t.Fatal("default route must fail closed")
	}
	lateDeny := desired
	lateDenyWireGuard := *desired.WireGuard
	lateDeny.WireGuard = &lateDenyWireGuard
	lateDeny.WireGuard.FirewallRules = []networkprotocol.WireGuardFirewallRule{desired.WireGuard.FirewallRules[1], desired.WireGuard.FirewallRules[0]}
	if err := ValidateGatewayDesired(lateDeny, privateKey.PublicKey(), now, ""); err == nil {
		t.Fatal("a protected deny after its covering allow must fail closed")
	}
	tcpAll := *desired.WireGuard
	tcpAll.FirewallRules = append([]networkprotocol.WireGuardFirewallRule(nil), desired.WireGuard.FirewallRules...)
	tcpAll.FirewallRules[1].Protocol = "tcp"
	if program, err := BuildNftProgram(tcpAll, false, ""); err != nil || !strings.Contains(program, "ip protocol tcp accept") {
		t.Fatalf("all-port TCP rule = %q, %v", program, err)
	}
	foreignInterface := desired
	foreignInterfaceWireGuard := *desired.WireGuard
	foreignInterface.WireGuard = &foreignInterfaceWireGuard
	foreignInterface.WireGuard.InterfaceName = "wg0"
	if err := ValidateGatewayDesired(foreignInterface, privateKey.PublicKey(), now, ""); err == nil {
		t.Fatal("gateway configuration must not retarget another interface")
	}
	injected := *desired.WireGuard
	injected.FirewallRules = []networkprotocol.WireGuardFirewallRule{{ID: "rule-1", Effect: "allow", SourceCIDR: "100.96.0.2/32 counter accept", DestinationCIDR: "10.77.0.0/24", Protocol: "any"}}
	if _, err := BuildNftProgram(injected, false, ""); err == nil {
		t.Fatal("nft program builder must reject unvalidated rule input")
	}
}

func TestGatewayDesiredAllowsResourceLeaseBeforeProtectedDenyAndSiteLinks(t *testing.T) {
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	desired := testGatewayDesired(privateKey.PublicKey().String(), now)
	desired.ResourceLeases = []networkprotocol.ResourceLease{{
		ID: "resource-lease-1", SessionID: "session-1", SubjectID: "user-1", DeviceID: "device-1",
		ResourceIDs: []string{"resource-db"}, PolicyVersion: 1, IssuedAt: now, ExpiresAt: desired.ValidUntil,
	}}
	resourceExpiry := desired.ValidUntil
	desired.WireGuard.FirewallRules = append([]networkprotocol.WireGuardFirewallRule{{
		ID: "allow-resource", Effect: "allow", LeaseID: "resource-lease-1", SourceCIDR: "100.96.0.2/32",
		DestinationCIDR: "10.77.0.10/32", Protocol: "tcp", Ports: []int{5432}, ExpiresAt: &resourceExpiry,
	}}, desired.WireGuard.FirewallRules...)
	if err := ValidateGatewayDesired(desired, privateKey.PublicKey(), now, ""); err != nil {
		t.Fatalf("resource allow before protected deny rejected: %v", err)
	}

	site := networkprotocol.ConfigurationDesired{
		ConfigurationVersion: 2, PolicyVersion: 1, ValidUntil: now.Add(5 * time.Minute), AccessProfile: "full",
		NetworkLeases: []networkprotocol.NetworkLease{}, ResourceLeases: []networkprotocol.ResourceLease{},
		WireGuard: &networkprotocol.WireGuardConfiguration{
			Role: "gateway", InterfaceName: "soha0", PublicKey: privateKey.PublicKey().String(), Addresses: []string{"100.97.0.1/32"},
			ListenPort: 51821, MTU: 1420, RoutingMode: "routed", FirewallDefault: "deny",
			Peers: []networkprotocol.WireGuardPeer{{
				RuntimeID: "gateway-hq", PublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", EndpointHost: "192.0.2.10", EndpointPort: 51820,
				AllowedIPs: []string{"100.96.0.0/24", "10.20.0.0/16"}, PersistentKeepaliveSeconds: 25,
			}},
			Routes: []string{"100.96.0.0/24", "10.20.0.0/16"},
			FirewallRules: []networkprotocol.WireGuardFirewallRule{
				{ID: "from-hq", Effect: "allow", SiteLinkRuntimeID: "gateway-hq", Direction: "from_wireguard", SourceCIDR: "10.20.0.0/16", DestinationCIDR: "10.40.0.0/16", Protocol: "any", ExpiresAt: timePointer(now.Add(5 * time.Minute))},
				{ID: "to-hq", Effect: "allow", SiteLinkRuntimeID: "gateway-hq", Direction: "to_wireguard", SourceCIDR: "10.40.0.0/16", DestinationCIDR: "10.20.0.0/16", Protocol: "any", ExpiresAt: timePointer(now.Add(5 * time.Minute))},
			},
		},
	}
	if err := ValidateGatewayDesired(site, privateKey.PublicKey(), now, ""); err != nil {
		t.Fatalf("site link desired rejected: %v", err)
	}
	program, err := BuildNftProgram(*site.WireGuard, false, "")
	if err != nil || !strings.Contains(program, `oifname "soha0" ip saddr 10.40.0.0/16`) {
		t.Fatalf("site link nft program = %q, %v", program, err)
	}
}

func TestExecutorRollsBackToLastAppliedConfiguration(t *testing.T) {
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	system := &fakeSystem{readbackHash: "sha256:" + strings.Repeat("a", 64)}
	executor, err := NewExecutor(privateKey, system, "")
	if err != nil {
		t.Fatal(err)
	}
	first := testGatewayDesired(privateKey.PublicKey().String(), now)
	if outcome := executor.Apply(context.Background(), first, now); outcome.Status != "applied" || outcome.ReadbackHash == "" {
		t.Fatalf("first apply = %#v", outcome)
	}
	second := first
	second.ConfigurationVersion++
	system.failNextApply = true
	if outcome := executor.Apply(context.Background(), second, now); outcome.Status != "rolled-back" || outcome.ReasonCode != "system_apply_failed" {
		t.Fatalf("failed apply = %#v", outcome)
	}
	want := []string{"baseline:soha0", "apply:1", "readback:1", "baseline:soha0", "apply:2", "baseline:soha0", "apply:1", "readback:1"}
	if strings.Join(system.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("calls = %#v, want %#v", system.calls, want)
	}
}

func TestExecutorReportsRollbackFailureAndFailsClosed(t *testing.T) {
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	system := &fakeSystem{readbackHash: "sha256:" + strings.Repeat("a", 64)}
	executor, err := NewExecutor(privateKey, system, "")
	if err != nil {
		t.Fatal(err)
	}
	first := testGatewayDesired(privateKey.PublicKey().String(), now)
	if outcome := executor.Apply(context.Background(), first, now); outcome.Status != "applied" {
		t.Fatalf("first apply = %#v", outcome)
	}
	second := first
	second.ConfigurationVersion++
	system.failApply = true
	if outcome := executor.Apply(context.Background(), second, now); outcome.Status != "rejected" || outcome.ReasonCode != "rollback_failed" {
		t.Fatalf("failed rollback = %#v", outcome)
	}
	if got := system.calls[len(system.calls)-1]; got != "disable:soha0" {
		t.Fatalf("last rollback call = %q, want fail-closed disable", got)
	}
}

func TestExecutorRejectsFailedFirstApplyAfterDisabling(t *testing.T) {
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	system := &fakeSystem{failNextApply: true, readbackHash: "sha256:" + strings.Repeat("a", 64)}
	executor, err := NewExecutor(privateKey, system, "")
	if err != nil {
		t.Fatal(err)
	}
	outcome := executor.Apply(context.Background(), testGatewayDesired(privateKey.PublicKey().String(), now), now)
	if outcome.Status != "rejected" || outcome.ReasonCode != "system_apply_failed" {
		t.Fatalf("first failed apply = %#v", outcome)
	}
	if got := system.calls[len(system.calls)-1]; got != "disable:soha0" {
		t.Fatalf("last recovery call = %q, want disable", got)
	}
}

func TestExecutorDisableStillRemovesInterfaceWhenBaselineFails(t *testing.T) {
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	system := &fakeSystem{failBaseline: true}
	executor, err := NewExecutor(privateKey, system, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.Disable(context.Background()); err == nil {
		t.Fatal("baseline failure must be returned")
	}
	want := []string{"baseline:soha0", "disable:soha0"}
	if strings.Join(system.calls, ",") != strings.Join(want, ",") {
		t.Fatalf("disable calls = %#v, want %#v", system.calls, want)
	}
}

func TestLinuxSystemUsesReplacePeersAndScopedCommands(t *testing.T) {
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	desired := testGatewayDesired(privateKey.PublicKey().String(), now)
	wg := &fakeWireGuard{}
	commands := &fakeCommands{missingLink: true}
	system, err := newLinuxSystem(wg, commands, "/sbin/ip", "/usr/sbin/nft", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := system.Apply(context.Background(), desired, privateKey); err != nil {
		t.Fatal(err)
	}
	if !wg.config.ReplacePeers || wg.config.PrivateKey == nil || *wg.config.PrivateKey != privateKey || len(wg.config.Peers) != 1 || !wg.config.Peers[0].ReplaceAllowedIPs || len(wg.config.Peers[0].AllowedIPs) != 1 {
		t.Fatalf("WireGuard config = %#v", wg.config)
	}
	for _, call := range commands.calls {
		if strings.Contains(call, "/bin/sh") || strings.Contains(call, " sh ") {
			t.Fatalf("system command used a shell: %s", call)
		}
	}
	joined := strings.Join(commands.calls, "\n")
	for _, expected := range []string{"/sbin/ip link add dev soha0 type wireguard", "/sbin/ip -4 address flush dev soha0", "/sbin/ip link set dev soha0 mtu 1420 up", "/usr/sbin/nft -f -"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("commands missing %q: %s", expected, joined)
		}
	}
}

func TestLinuxSystemOnlyDeletesOwnedWireGuardInterface(t *testing.T) {
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	foreignKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	commands := &fakeCommands{linkJSON: []byte(`[{"ifindex":3,"ifname":"soha0","linkinfo":{"info_kind":"veth"}}]`)}
	system, err := newLinuxSystem(&fakeWireGuard{}, commands, "/sbin/ip", "/usr/sbin/nft", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := system.Disable(context.Background(), "soha0", privateKey.PublicKey()); err == nil {
		t.Fatal("non-WireGuard name collision must not be deleted")
	}
	if strings.Contains(strings.Join(commands.calls, "\n"), "link delete") {
		t.Fatal("non-WireGuard interface was deleted")
	}

	commands.calls = nil
	commands.linkJSON = []byte(`[{"ifindex":3,"ifname":"soha0","linkinfo":{"info_kind":"wireguard"}}]`)
	system.wg = &fakeWireGuard{device: wgtypes.Device{PublicKey: foreignKey.PublicKey()}}
	if err := system.Disable(context.Background(), "soha0", privateKey.PublicKey()); err == nil {
		t.Fatal("WireGuard interface owned by another key must not be deleted")
	}
	if strings.Contains(strings.Join(commands.calls, "\n"), "link delete") {
		t.Fatal("foreign WireGuard interface was deleted")
	}
}

func TestDisabledReadbackDoesNotTreatIPFailureAsAbsence(t *testing.T) {
	system, err := newLinuxSystem(&fakeWireGuard{}, &fakeCommands{linkError: os.ErrPermission}, "/sbin/ip", "/usr/sbin/nft", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := system.Readback(context.Background(), networkprotocol.ConfigurationDesired{}); err == nil {
		t.Fatal("failed interface inspection must not verify disabled state")
	}
}

func TestNftReadbackRejectsWrongPolicyAndRuleAction(t *testing.T) {
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	desired := testGatewayDesired(privateKey.PublicKey().String(), time.Now().UTC())
	raw := []byte(`{"nftables":[
		{"table":{"family":"inet","name":"soha_network_gateway"}},
		{"chain":{"family":"inet","table":"soha_network_gateway","name":"forward","type":"filter","hook":"forward","policy":"accept"}},
		{"rule":{"family":"inet","table":"soha_network_gateway","chain":"forward","expr":[{"match":{"op":"==","left":{"meta":{"key":"oifname"}},"right":"soha0"}},{"match":{"op":"in","left":{"ct":{"key":"state"}},"right":["established","related"]}},{"accept":null}],"comment":"return-established"}},
		{"rule":{"family":"inet","table":"soha_network_gateway","chain":"forward","expr":[{"match":{"op":"==","left":{"meta":{"key":"iifname"}},"right":"soha0"}},{"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"saddr"}},"right":"100.96.0.2"}},{"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"daddr"}},"right":"10.77.0.10"}},{"drop":null}],"comment":"deny-protected"}},
		{"rule":{"family":"inet","table":"soha_network_gateway","chain":"forward","expr":[{"match":{"op":"==","left":{"meta":{"key":"iifname"}},"right":"soha0"}},{"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"saddr"}},"right":"100.96.0.2"}},{"match":{"op":"==","left":{"payload":{"protocol":"ip","field":"daddr"}},"right":{"prefix":{"addr":"10.77.0.0","len":24}}}},{"accept":null}],"comment":"allow-network"}},
		{"rule":{"family":"inet","table":"soha_network_gateway","chain":"forward","expr":[{"match":{"op":"==","left":{"meta":{"key":"iifname"}},"right":"soha0"}},{"drop":null}],"comment":"default-deny-from-wireguard"}},
		{"rule":{"family":"inet","table":"soha_network_gateway","chain":"forward","expr":[{"match":{"op":"==","left":{"meta":{"key":"oifname"}},"right":"soha0"}},{"drop":null}],"comment":"default-deny-to-wireguard"}}
	]}`)
	if _, err := verifyNftReadback(raw, *desired.WireGuard, ""); err != nil {
		t.Fatalf("valid nft readback rejected: %v", err)
	}
	wrongAction := strings.Replace(string(raw), `{"drop":null}],"comment":"deny-protected"`, `{"accept":null}],"comment":"deny-protected"`, 1)
	if _, err := verifyNftReadback([]byte(wrongAction), *desired.WireGuard, ""); err == nil {
		t.Fatal("wrong firewall action must fail readback")
	}
	wrongPolicy := strings.Replace(string(raw), `"policy":"accept"`, `"policy":"drop"`, 1)
	if _, err := verifyNftReadback([]byte(wrongPolicy), *desired.WireGuard, ""); err == nil {
		t.Fatal("wrong chain policy must fail readback")
	}
	wrongField := strings.Replace(string(raw), `"field":"saddr"`, `"field":"daddr"`, 1)
	if _, err := verifyNftReadback([]byte(wrongField), *desired.WireGuard, ""); err == nil {
		t.Fatal("source address bound to the wrong nft field must fail readback")
	}
}

func TestLinuxSystemIntegration(t *testing.T) {
	if os.Getenv("SOHA_NETWORK_GATEWAY_LINUX_INTEGRATION") != "1" {
		t.Skip("set SOHA_NETWORK_GATEWAY_LINUX_INTEGRATION=1 in an isolated Linux network namespace")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	peerKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	system, err := NewLinuxSystem("/sbin/ip", "/usr/sbin/nft", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = system.Close() }()
	defer func() {
		_ = exec.CommandContext(context.Background(), "/usr/sbin/nft", "delete", "table", "inet", firewallTable).Run()
	}()
	desired := testGatewayDesired(privateKey.PublicKey().String(), time.Now().UTC())
	desired.WireGuard.Peers[0].PublicKey = peerKey.PublicKey().String()
	desired.WireGuard.FirewallRules[1].Protocol = "tcp"
	desired.WireGuard.FirewallRules[1].Ports = []int{443, 80}
	executor, err := NewExecutor(privateKey, system, "")
	if err != nil {
		t.Fatal(err)
	}
	if outcome := executor.Apply(ctx, desired, time.Now().UTC()); outcome.Status != "applied" {
		t.Fatalf("real Linux apply = %#v", outcome)
	}
	if err := executor.Disable(ctx); err != nil {
		t.Fatal(err)
	}
	if err := exec.CommandContext(ctx, "/sbin/ip", "link", "show", "dev", defaultInterfaceName).Run(); err == nil {
		t.Fatal("WireGuard interface still exists after disable")
	}
}

func TestRuntimeAppliesReportsAndExpiresFailClosed(t *testing.T) {
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	desired := testGatewayDesired(privateKey.PublicKey().String(), now)
	payload, _ := json.Marshal(desired)
	control := &fakeControl{message: networkprotocol.RuntimeMessage{
		SchemaVersion: networkprotocol.RuntimeSchemaVersion, MessageID: "configuration-1", MessageType: networkprotocol.MessageConfiguration,
		ProducerID: "network-control", RuntimeID: "gateway-1", RuntimeKind: "gateway", OccurredAt: now, ExpiresAt: desired.ValidUntil, Payload: payload,
	}}
	applier := &fakeApplier{outcome: ApplyOutcome{Status: "applied", ReadbackHash: "sha256:" + strings.Repeat("b", 64)}}
	runtime, err := NewRuntime("gateway-1", control, applier, nil, time.Second, time.Minute, 5*time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	runtime.now = func() time.Time { return now }
	if err := runtime.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if applier.applied != 1 || len(control.reports) != 1 || control.reports[0].ConfigurationVersion != 1 || !runtime.Ready() {
		t.Fatalf("runtime state: applies=%d reports=%#v ready=%v", applier.applied, control.reports, runtime.Ready())
	}
	request := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	recorder := httptest.NewRecorder()
	runtime.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("ready response = %d %s", recorder.Code, recorder.Body.String())
	}

	now = desired.ValidUntil.Add(time.Second)
	control.err = errors.New("control unavailable")
	if err := runtime.Cycle(context.Background()); err == nil || applier.disabled != 1 || runtime.Ready() {
		t.Fatalf("expired cycle: err=%v disabled=%d ready=%v", err, applier.disabled, runtime.Ready())
	}
	recorder = httptest.NewRecorder()
	runtime.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expired ready response = %d %s", recorder.Code, recorder.Body.String())
	}
}

func TestRuntimeDoesNotReapplyATerminalFailedVersion(t *testing.T) {
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	desired := testGatewayDesired(privateKey.PublicKey().String(), now)
	payload, _ := json.Marshal(desired)
	control := &fakeControl{message: networkprotocol.RuntimeMessage{
		SchemaVersion: networkprotocol.RuntimeSchemaVersion, MessageID: "configuration-1", MessageType: networkprotocol.MessageConfiguration,
		ProducerID: "network-control", RuntimeID: "gateway-1", RuntimeKind: "gateway", OccurredAt: now, ExpiresAt: desired.ValidUntil, Payload: payload,
	}}
	applier := &fakeApplier{outcome: ApplyOutcome{Status: "rolled-back", ReadbackHash: "sha256:" + strings.Repeat("d", 64), ReasonCode: "system_apply_failed"}}
	runtime, err := NewRuntime("gateway-1", control, applier, nil, 5*time.Second, time.Minute, 5*time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	runtime.now = func() time.Time { return now }
	if err := runtime.Cycle(context.Background()); err == nil {
		t.Fatal("failed application must surface an error")
	}
	if err := runtime.Cycle(context.Background()); err != nil {
		t.Fatalf("terminal failed version should be ignored after its report is accepted: %v", err)
	}
	if applier.applied != 1 || len(control.reports) != 1 {
		t.Fatalf("same failed version reapplied: applies=%d reports=%d", applier.applied, len(control.reports))
	}

	desired.ConfigurationVersion = 2
	desired.ValidUntil = desired.ValidUntil.Add(time.Minute)
	payload, _ = json.Marshal(desired)
	control.message.MessageID = "configuration-2"
	control.message.ExpiresAt = desired.ValidUntil
	control.message.Payload = payload
	applier.outcome = ApplyOutcome{Status: "applied", ReadbackHash: "sha256:" + strings.Repeat("e", 64)}
	if err := runtime.Cycle(context.Background()); err != nil || applier.applied != 2 || !runtime.Ready() {
		t.Fatalf("higher version was not applied: err=%v applies=%d ready=%v", err, applier.applied, runtime.Ready())
	}
}

func TestRuntimeSchedulesTheNextCycleAtConfigurationExpiry(t *testing.T) {
	now := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	runtime, err := NewRuntime("gateway-1", &fakeControl{}, &fakeApplier{}, nil, 5*time.Second, time.Minute, time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	runtime.validUntil = now.Add(1500 * time.Millisecond)
	runtime.disabled = false
	runtime.intervalJitter = func(delay time.Duration) time.Duration { return delay }
	if got := runtime.nextCycleDelay(now); got != 1500*time.Millisecond {
		t.Fatalf("next cycle delay = %s, want exact expiry delay", got)
	}
}

func TestRuntimeUpdatesUnifiedIntervalsWithoutReapplyingConfiguration(t *testing.T) {
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 4, 8, 0, 0, 0, time.UTC)
	desired := testGatewayDesired(privateKey.PublicKey().String(), now)
	desired.RuntimeIntervals = &networkprotocol.RuntimeIntervals{HeartbeatIntervalSeconds: 90, ConfigurationPollIntervalSeconds: 120}
	payload, _ := json.Marshal(desired)
	control := &fakeControl{message: networkprotocol.RuntimeMessage{
		SchemaVersion: networkprotocol.RuntimeSchemaVersion, MessageID: "configuration-1", MessageType: networkprotocol.MessageConfiguration,
		ProducerID: "network-control", RuntimeID: "gateway-1", RuntimeKind: "gateway", OccurredAt: now, ExpiresAt: desired.ValidUntil, Payload: payload,
	}}
	applier := &fakeApplier{outcome: ApplyOutcome{Status: "applied", ReadbackHash: "sha256:" + strings.Repeat("f", 64)}}
	runtime, err := NewRuntime("gateway-1", control, applier, nil, time.Minute, time.Minute, 5*time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	runtime.now = func() time.Time { return now }
	if err := runtime.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if poll, heartbeat := runtime.intervals(); poll != 120*time.Second || heartbeat != 90*time.Second {
		t.Fatalf("intervals = %s/%s", poll, heartbeat)
	}

	desired.RuntimeIntervals = &networkprotocol.RuntimeIntervals{HeartbeatIntervalSeconds: 75, ConfigurationPollIntervalSeconds: 80}
	control.message.Payload, _ = json.Marshal(desired)
	if err := runtime.Cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if poll, heartbeat := runtime.intervals(); poll != 80*time.Second || heartbeat != 75*time.Second || applier.applied != 1 {
		t.Fatalf("updated intervals = %s/%s, applies = %d", poll, heartbeat, applier.applied)
	}
}

func TestRuntimeHeartbeatRetriesImmediatelyWithBoundedBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	telemetry := telemetryFunc(func(context.Context, RuntimeStatus) error {
		calls++
		if calls < 8 {
			return errors.New("offline")
		}
		cancel()
		return nil
	})
	runtime, err := NewRuntime("gateway-1", &fakeControl{}, &fakeApplier{}, telemetry, time.Minute, time.Minute, time.Minute, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	runtime.intervalJitter = func(delay time.Duration) time.Duration { return delay }
	var delays []time.Duration
	runtime.wait = func(ctx context.Context, delay time.Duration) bool {
		if ctx.Err() != nil {
			return false
		}
		delays = append(delays, delay)
		return true
	}
	runtime.runHeartbeats(ctx)
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second, 30 * time.Second, 30 * time.Second}
	if calls != 8 || !slices.Equal(delays, want) {
		t.Fatalf("heartbeat calls/delays = %d/%v, want 8/%v", calls, delays, want)
	}
}

func TestRuntimeIntervalJitterStaysWithinTwentyPercent(t *testing.T) {
	for range 100 {
		delay := runtimeIntervalJitter(time.Minute)
		if delay < 48*time.Second || delay > 72*time.Second {
			t.Fatalf("runtime interval jitter = %s", delay)
		}
	}
}

func TestControlAndTelemetryClientsUseStrictRuntimeContracts(t *testing.T) {
	now := time.Date(2026, 9, 3, 8, 0, 0, 0, time.UTC)
	privateKey, err := wgtypes.GeneratePrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	desired := testGatewayDesired(privateKey.PublicKey().String(), now)
	desiredPayload, _ := json.Marshal(desired)
	configuration := networkprotocol.RuntimeMessage{
		SchemaVersion: networkprotocol.RuntimeSchemaVersion, MessageID: "configuration-1", MessageType: networkprotocol.MessageConfiguration,
		ProducerID: "network-control", RuntimeID: "gateway-1", RuntimeKind: "gateway", OccurredAt: now, ExpiresAt: desired.ValidUntil, Payload: desiredPayload,
	}
	var applied, enrolled, heartbeat bool
	server := httptest.NewTLSServer(gatewayContractHandler(now, configuration, &applied, &enrolled, &heartbeat))
	defer server.Close()
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	control, err := NewControlClient(server.URL, "gateway-1", server.Client(), schemas, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	control.now = func() time.Time { return now }
	if message, err := control.Configuration(context.Background()); err != nil || message.MessageID != configuration.MessageID {
		t.Fatalf("configuration = %#v, %v", message, err)
	}
	if err := control.Report(context.Background(), networkprotocol.ConfigurationApplied{ConfigurationVersion: 1, PolicyVersion: 1, Status: "applied", ReadbackHash: "sha256:" + strings.Repeat("c", 64)}); err != nil {
		t.Fatal(err)
	}
	if _, err := control.Enroll(context.Background(), Enrollment{EnrollmentID: "enrollment-1", ChallengeID: "challenge-1", DeviceID: "device-1", DevicePublicKey: testPublicKeyPEM(t), WireGuardPublicKey: privateKey.PublicKey().String(), ClientVersion: "test", Token: strings.Repeat("t", 32)}); err != nil {
		t.Fatal(err)
	}
	telemetry, err := NewTelemetryClient(server.URL, "gateway-1", server.Client(), schemas)
	if err != nil {
		t.Fatal(err)
	}
	telemetry.now = func() time.Time { return now }
	if err := telemetry.Heartbeat(context.Background(), RuntimeStatus{Status: "healthy", ConfigurationVersion: 1, PolicyVersion: 1, UptimeSeconds: 10}); err != nil {
		t.Fatal(err)
	}
	if !applied || !enrolled || !heartbeat {
		t.Fatalf("calls: applied=%v enrolled=%v heartbeat=%v", applied, enrolled, heartbeat)
	}
}

func TestGatewayConfigKeepsControlAndIngestIdentitiesSeparate(t *testing.T) {
	directory := t.TempDir()
	file := func(name string) string {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte("test"), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	config := Config{
		RuntimeID: "gateway-1", DeviceID: "device-1", ControlURL: "https://control.example.test",
		ControlCAFile: file("control-ca.pem"), ControlCertFile: file("control-cert.pem"), ControlKeyFile: file("control-key.pem"),
		WireGuardPrivateKeyFile: filepath.Join(directory, "wireguard.key"), IPPath: "/sbin/ip", NFTPath: "/usr/sbin/nft", HealthAddress: ":8084",
		PollInterval: time.Second, HeartbeatInterval: time.Minute, RequestTimeout: 5 * time.Second, MaxClockSkew: 5 * time.Minute,
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	partialIngest := config
	partialIngest.IngestURL = "https://ingest.example.test"
	if err := partialIngest.Validate(); err == nil {
		t.Fatal("ingest URL without its separately scoped certificate must fail")
	}
	insecure := config
	insecure.ControlURL = "http://control.example.test"
	if err := insecure.Validate(); err == nil {
		t.Fatal("plaintext control URL must fail")
	}
}

func TestGatewayHTTPClientsRejectRedirects(t *testing.T) {
	if err := rejectRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Fatalf("rejectRedirect() error = %v", err)
	}
}

type fakeControl struct {
	message networkprotocol.RuntimeMessage
	err     error
	reports []networkprotocol.ConfigurationApplied
}

func (c *fakeControl) Configuration(context.Context) (networkprotocol.RuntimeMessage, error) {
	return c.message, c.err
}

func (c *fakeControl) Report(_ context.Context, report networkprotocol.ConfigurationApplied) error {
	c.reports = append(c.reports, report)
	return nil
}

type fakeApplier struct {
	outcome  ApplyOutcome
	applied  int
	disabled int
}

type telemetryFunc func(context.Context, RuntimeStatus) error

func (fn telemetryFunc) Heartbeat(ctx context.Context, status RuntimeStatus) error {
	return fn(ctx, status)
}

func (a *fakeApplier) Apply(context.Context, networkprotocol.ConfigurationDesired, time.Time) ApplyOutcome {
	a.applied++
	return a.outcome
}

func (a *fakeApplier) Disable(context.Context) error {
	a.disabled++
	return nil
}

type fakeWireGuard struct {
	config wgtypes.Config
	device wgtypes.Device
}

func (w *fakeWireGuard) Device(string) (*wgtypes.Device, error) {
	return &w.device, nil
}

func (w *fakeWireGuard) ConfigureDevice(_ string, config wgtypes.Config) error {
	w.config = config
	return nil
}

func (w *fakeWireGuard) Close() error { return nil }

type fakeCommands struct {
	calls       []string
	missingLink bool
	linkJSON    []byte
	linkError   error
}

func (c *fakeCommands) Run(_ context.Context, stdin []byte, path string, args ...string) ([]byte, error) {
	c.calls = append(c.calls, strings.Join(append([]string{path}, args...), " ")+string(stdin))
	if path == "/sbin/ip" && strings.Join(args, " ") == "-j -d link show" {
		if c.linkError != nil {
			return nil, c.linkError
		}
		if c.missingLink {
			return []byte(`[]`), nil
		}
		return c.linkJSON, nil
	}
	if path == "/usr/sbin/nft" && strings.Join(args, " ") == "-j list tables" {
		return []byte(`{"nftables":[{"metainfo":{"json_schema_version":1}}]}`), nil
	}
	return nil, nil
}

type fakeSystem struct {
	calls         []string
	failNextApply bool
	failApply     bool
	failBaseline  bool
	readbackHash  string
}

func (s *fakeSystem) Baseline(_ context.Context, interfaceName string) error {
	s.calls = append(s.calls, "baseline:"+interfaceName)
	if s.failBaseline {
		return os.ErrPermission
	}
	return nil
}

func (s *fakeSystem) Apply(_ context.Context, desired networkprotocol.ConfigurationDesired, _ wgtypes.Key) error {
	s.calls = append(s.calls, "apply:"+string(rune('0'+desired.ConfigurationVersion)))
	if s.failNextApply || s.failApply {
		s.failNextApply = false
		return os.ErrPermission
	}
	return nil
}

func (s *fakeSystem) Readback(_ context.Context, desired networkprotocol.ConfigurationDesired) (string, error) {
	s.calls = append(s.calls, "readback:"+string(rune('0'+desired.ConfigurationVersion)))
	return s.readbackHash, nil
}

func (s *fakeSystem) Disable(_ context.Context, interfaceName string, _ wgtypes.Key) error {
	s.calls = append(s.calls, "disable:"+interfaceName)
	return nil
}

func testGatewayDesired(publicKey string, now time.Time) networkprotocol.ConfigurationDesired {
	return networkprotocol.ConfigurationDesired{
		ConfigurationVersion: 1,
		PolicyVersion:        1,
		ValidUntil:           now.Add(5 * time.Minute),
		AccessProfile:        "full",
		ProtectedResourceIDs: []string{"protected-app"},
		NetworkLeases: []networkprotocol.NetworkLease{{
			ID: "lease-1", SessionID: "session-1", SubjectID: "user-1", DeviceID: "device-1", NetworkSpaceID: "space-1",
			CIDRs: []string{"10.77.0.0/24"}, PolicyVersion: 1, IssuedAt: now, ExpiresAt: now.Add(5 * time.Minute),
		}},
		ResourceLeases: []networkprotocol.ResourceLease{},
		WireGuard: &networkprotocol.WireGuardConfiguration{
			Role: "gateway", InterfaceName: "soha0", PublicKey: publicKey, Addresses: []string{"100.96.0.1/32"},
			ListenPort: 51820, MTU: 1420, RoutingMode: "routed", FirewallDefault: "deny",
			Peers:  []networkprotocol.WireGuardPeer{{RuntimeID: "endpoint-1", DeviceID: "device-1", PublicKey: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=", AllowedIPs: []string{"100.96.0.2/32"}}},
			Routes: []string{"100.96.0.2/32"}, FirewallRules: []networkprotocol.WireGuardFirewallRule{
				{ID: "deny-protected", Effect: "deny", SourceCIDR: "100.96.0.2/32", DestinationCIDR: "10.77.0.10/32", Protocol: "any"},
				{ID: "allow-network", Effect: "allow", LeaseID: "lease-1", SourceCIDR: "100.96.0.2/32", DestinationCIDR: "10.77.0.0/24", Protocol: "any", ExpiresAt: timePointer(now.Add(5 * time.Minute))},
			},
		},
	}
}

func timePointer(value time.Time) *time.Time { return &value }

func testPublicKeyPEM(t *testing.T) string {
	t.Helper()
	publicKey, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: encoded}))
}

func gatewayContractHandler(now time.Time, configuration networkprotocol.RuntimeMessage, applied, enrolled, heartbeat *bool) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		switch request.Method + " " + request.URL.Path {
		case "GET /api/network-control/v1/runtimes/gateway-1/configuration":
			_ = json.NewEncoder(response).Encode(configuration)
		case "POST /api/network-control/v1/runtimes/gateway-1/configuration:applied":
			var message networkprotocol.RuntimeMessage
			_ = json.NewDecoder(request.Body).Decode(&message)
			payload, _ := networkprotocol.DecodePayload[networkprotocol.ConfigurationApplied](message.Payload)
			*applied = message.MessageType == networkprotocol.MessageConfigurationApply && payload.ConfigurationVersion == 1
			response.WriteHeader(http.StatusNoContent)
		case "POST /api/network-control/v1/runtimes/gateway-1/enroll":
			*enrolled = request.Header.Get("Authorization") == "Bearer "+strings.Repeat("t", 32)
			payload, _ := json.Marshal(networkprotocol.EnrollmentResult{Accepted: true, ReasonCode: "enrolled"})
			message := networkprotocol.RuntimeMessage{SchemaVersion: networkprotocol.RuntimeSchemaVersion, MessageID: "enrollment-result", MessageType: networkprotocol.MessageEnrollmentResult, ProducerID: "network-control", RuntimeID: "gateway-1", RuntimeKind: "gateway", OccurredAt: now, ExpiresAt: now.Add(time.Minute), Payload: payload}
			response.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(response).Encode(message)
		case "POST /api/ingest/v1/events:batch":
			var batch networkprotocol.IngestBatch
			_ = json.NewDecoder(request.Body).Decode(&batch)
			*heartbeat = batch.ProducerID == "gateway-1" && batch.ProducerKind == "gateway" && len(batch.Events) == 1 && batch.Events[0].Type == networkprotocol.EventHeartbeat
			response.WriteHeader(http.StatusAccepted)
			_, _ = response.Write([]byte(`{"batchId":"accepted","accepted":1,"duplicates":0}`))
		default:
			http.NotFound(response, request)
		}
	})
}
