package networkproxyruntime

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/contracts/proxytransport"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
)

func udpBackendFixture(t *testing.T, name string) *egressBackend {
	t.Helper()
	listener, port, err := listenEgressPort()
	if err != nil {
		t.Fatal(err)
	}
	b := &egressBackend{name: name, user: "backend-" + name, secret: "012345678901234567890123456789012345", port: port, udp: true}
	until := time.Now().Add(time.Minute)
	dial := func(ctx context.Context, _ string, target string) (net.Conn, error) {
		if target != "1.1.1.1:443" {
			t.Errorf("unpinned or forbidden backend target %s", target)
			return nil, errUnsafeConfiguration
		}
		return udpProbeConn(until, b), nil
	}
	gate, err := proxytransport.NewSOCKS5Gate(map[string]string{b.user: b.secret}, func(context.Context, string, string) (net.Conn, error) { return nil, errors.New("TCP unused") }, dial, func(context.Context, string, string) error { return nil }, until)
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = gate.Serve(listener) }()
	t.Cleanup(func() { _ = gate.Close() })
	return b
}

type udpIngressFixture struct {
	p              *publicationIngress
	server         *httptest.Server
	revokedA       atomic.Bool
	revokedGateway atomic.Bool
	checks         atomic.Int32
	policy         domain.Configuration
}

func newUDPIngressFixture(t *testing.T) *udpIngressFixture {
	t.Helper()
	f := &udpIngressFixture{}
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input api.NetworkProxyIngressCheckInput
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			w.WriteHeader(400)
			return
		}
		f.checks.Add(1)
		if input.Network != "udp" {
			t.Error("UDP authorization mislabeled TCP")
		}
		_ = json.NewEncoder(w).Encode(api.NetworkProxyIngressCheckResult{Allowed: (input.NodeID != "node-a" || !f.revokedA.Load()) && (input.NodeID != "gateway" || !f.revokedGateway.Load()), BundleID: input.BundleID, NodeID: input.NodeID, Revision: input.Revision, CredentialEpoch: input.CredentialEpoch, ExpiresAt: time.Now().Add(5 * time.Second)})
	}))
	t.Cleanup(control.Close)
	pub := &api.NetworkProxyServerPublication{BundleID: "bundle", Revision: 1, CredentialEpoch: 1, Nodes: []api.NetworkProxyServerPublicationNode{{NodeID: "node-a", OutboundID: "node-a"}, {NodeID: "node-b", OutboundID: "node-b"}, {NodeID: "http", OutboundID: "http"}}}
	f.policy = domain.Configuration{Protection: &domain.Protection{PolicyVersion: 1, CIDRs: []string{"10.0.0.0/8"}, Hosts: []string{"protected.example"}}, Publication: pub}
	f.p = &publicationIngress{packetEnabled: true, accepting: true, until: time.Now().Add(time.Minute), publication: *pub, epoch: "engine", client: NewClient(control.URL, "runtime", nil), backends: map[string]*egressBackend{"node-a": udpBackendFixture(t, "node-a"), "node-b": udpBackendFixture(t, "node-b"), "http": {name: "http", udp: false, user: "http-user", secret: "012345678901234567890123456789012345"}}, handlers: map[*proxytransport.Handler]struct{}{}}
	f.p.udpPolicy.Store(copyUDPPolicy(f.policy))
	f.p.udpGuard = packetGuardFixture(t, f.policy, f.p.backends)
	f.server = httptest.NewTLSServer(f.p)
	f.p.server, f.p.listener = f.server.Config, f.server.Listener
	t.Cleanup(func() { f.p.close(); f.server.Close() })
	return f
}

func (f *udpIngressFixture) dial(t *testing.T, node, target string) (net.Conn, error) {
	t.Helper()
	conn, err := tls.Dial("tcp", f.server.Listener.Addr().String(), fixtureValue[*http.Transport](t, f.server.Client().Transport).TLSClientConfig.Clone())
	if err != nil {
		return nil, err
	}
	user, _ := proxytransport.EncodeMappedUsername("credential", node)
	return proxytransport.DialPacketTunnel(context.Background(), conn, target, user, "012345678901234567890123456789012345", time.Now().Add(5*time.Second))
}

func packetEcho(t *testing.T, c net.Conn, payload []byte) {
	t.Helper()
	_ = c.SetDeadline(time.Now().Add(time.Second))
	if _, err := c.Write(payload); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 65507)
	n, err := c.Read(buffer)
	if err != nil || n != len(payload) || string(buffer[:n]) != string(payload) {
		t.Fatalf("datagram boundary changed %d %v", n, err)
	}
}

func TestUDPTLSMappedPinsNodesPreservesPacketsAndRejectsHTTP(t *testing.T) {
	f := newUDPIngressFixture(t)
	for _, node := range []string{"node-a", "node-b"} {
		c, err := f.dial(t, node, "1.1.1.1:443")
		if err != nil {
			t.Fatal(err)
		}
		for _, payload := range [][]byte{{}, []byte("one"), {0, 1, 0, 2}} {
			packetEcho(t, c, payload)
		}
		_ = c.Close()
		if f.p.backends[node].udpProbes.Load() != 3 {
			t.Fatal("selected node did not handle its packets")
		}
	}
	for _, target := range []string{"10.0.0.1:443", "protected.example:443"} {
		if c, err := f.dial(t, "node-a", target); err == nil {
			_ = c.Close()
			t.Fatal("protected UDP destination accepted")
		}
	}
	if c, err := f.dial(t, "http", "1.1.1.1:443"); !errors.Is(err, proxytransport.ErrPacketUnsupported) {
		if c != nil {
			_ = c.Close()
		}
		t.Fatalf("TCP-only node was not typed unsupported: %v", err)
	}
	for _, values := range [][]string{{"udp/2"}, {"udp/1", "udp/1"}, {""}} {
		r := httptest.NewRequest(http.MethodConnect, "https://1.1.1.1:443", nil)
		r.TLS = &tls.ConnectionState{}
		r.Header[proxytransport.PacketTransportHeader] = values
		w := httptest.NewRecorder()
		f.p.ServeHTTP(w, r)
		if w.Code != 400 {
			t.Fatal("invalid UDP opt-in fell through to TCP")
		}
	}
}

func TestUDPNodeRevocationOnlyClosesAAndDrainUsesFixedBound(t *testing.T) {
	f := newUDPIngressFixture(t)
	a, err := f.dial(t, "node-a", "1.1.1.1:443")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()
	b, err := f.dial(t, "node-b", "1.1.1.1:443")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	packetEcho(t, a, []byte("A"))
	packetEcho(t, b, []byte("B"))
	f.revokedA.Store(true)
	latest := copyUDPPolicy(f.policy)
	latest.publication.Nodes = latest.publication.Nodes[1:]
	f.p.udpPolicy.Store(latest)
	_ = a.SetDeadline(time.Now().Add(time.Second))
	_, _ = a.Write([]byte("revoked"))
	if _, err := a.Read(make([]byte, 100)); err == nil {
		t.Fatal("A remained authorized")
	}
	packetEcho(t, b, []byte("B survives"))
	f.p.setAccepting(false)
	bound := time.Now().Add(250 * time.Millisecond)
	f.p.mu.Lock()
	f.p.until = bound
	f.p.mu.Unlock()
	f.p.capPacketLeases()
	packetEcho(t, b, []byte("draining"))
	if c, err := f.dial(t, "node-b", "1.1.1.1:443"); err == nil {
		_ = c.Close()
		t.Fatal("drain admitted association")
	}
	_ = b.SetReadDeadline(bound.Add(time.Second))
	if _, err := b.Read(make([]byte, 100)); err == nil {
		t.Fatal("drain retained stream")
	}
	deadline := time.Now().Add(time.Second)
	for f.p.active.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if f.p.active.Load() != 0 {
		t.Fatal("UDP count falsely retained past close")
	}
}

func TestUDPStopConfirmationClosesAssociationsBeforeZeroProof(t *testing.T) {
	f := newUDPIngressFixture(t)
	c, err := f.dial(t, "node-b", "1.1.1.1:443")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	packetEcho(t, c, []byte("active"))
	config, _ := lifecycleFixture(t, "")
	e := newEngine(config, "fixture-controller-secret")
	defer e.stop()
	if result := e.apply(context.Background(), lifecycleDesired(1, 7890)); result.Status != "applied" {
		t.Fatalf("apply %+v", result)
	}
	var proof atomic.Pointer[api.NetworkProxyDeploymentRuntimeAck]
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ack api.NetworkProxyDeploymentRuntimeAck
		_ = json.NewDecoder(r.Body).Decode(&ack)
		proof.Store(&ack)
		_ = json.NewEncoder(w).Encode(ack)
	}))
	defer server.Close()
	r := &Runner{config: Config{RuntimeID: "runtime", ManagedDeployment: true}, hostBootEpoch: "boot", client: NewClient(server.URL, "runtime", nil), engine: e, ingress: f.p, deployment: deploymentState{grant: ptrConfirmationGrant()}}
	if err := r.confirmOwnedStop(context.Background()); err != nil {
		t.Fatal(err)
	}
	ack := proof.Load()
	if ack == nil || !ack.ProcessStopped || !ack.ActiveCountKnown || ack.ActiveConnections != 0 || f.p.active.Load() != 0 || f.p.udpOwned.activeCount() != 0 {
		t.Fatalf("invalid UDP stop proof %+v", ack)
	}
}

func TestUDPGatewayPinsNativeSelectionAndAggregateRevocation(t *testing.T) {
	f := newUDPIngressFixture(t)
	group := api.NetworkProxyGroup{GroupID: "choice", Type: "select", NodeIDs: []string{"node-a", "node-b"}}
	f.policy.Publication.Nodes = []api.NetworkProxyServerPublicationNode{{NodeID: "gateway", OutboundID: "choice"}}
	f.policy.Publication.Groups = []api.NetworkProxyGroup{group}
	f.p.publication = *f.policy.Publication
	f.p.backends["choice"] = &egressBackend{name: "choice", group: true}
	f.p.udpPolicy.Store(copyUDPPolicy(f.policy))
	var selected atomic.Pointer[string]
	aName := "node-a"
	bName := "node-b"
	selected.Store(&aName)
	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"now": *selected.Load(), "all": group.NodeIDs})
	}))
	defer controller.Close()
	f.p.controller = newClashController(controller.URL, "controller-secret")
	a, err := f.dial(t, "gateway", "1.1.1.1:443")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()
	packetEcho(t, a, []byte("A"))
	selected.Store(&bName)
	b, err := f.dial(t, "gateway", "1.1.1.1:443")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = b.Close() }()
	packetEcho(t, b, []byte("B"))
	packetEcho(t, a, []byte("still-A"))
	if f.p.backends["node-a"].udpProbes.Load() != 2 || f.p.backends["node-b"].udpProbes.Load() != 1 {
		t.Fatal("new selection migrated an old UDP association")
	}
	f.revokedGateway.Store(true)
	for _, c := range []net.Conn{a, b} {
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, err := c.Read(make([]byte, 100))
		if err == nil {
			t.Fatal("revoked aggregate retained a UDP association")
		}
		if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
			t.Fatal("aggregate revocation not enforced by online lease")
		}
	}
}

func TestMihomoUDPBootstrapAndRenderedFlagsAreProtocolBound(t *testing.T) {
	scope, err := proxytransport.NewScope([]string{"10.0.0.0/8"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"socks5", "ss", "trojan", "vmess", "vless", "http", "snell", "ssr"} {
		node := map[string]any{"type": kind, "name": "node", "server": "1.1.1.1", "port": 1080}
		if err := pinMihomoTransport(context.Background(), node, scope); err != nil {
			t.Fatal(err)
		}
		want := mihomoUDPProtocol(kind)
		if node["udp"] != want {
			t.Fatal("bootstrap reset UDP eligibility")
		}
		g, err := newEgressGuard(nil, time.Now().Add(time.Minute))
		if err != nil {
			t.Fatal(err)
		}
		g.udpEnabled = true
		doc := map[string]any{"proxies": []any{node}, "rules": []any{"MATCH,node"}}
		if err := g.mihomo(doc); err != nil {
			g.close()
			t.Fatal(err)
		}
		b := g.backends["node"]
		front := g.front(b)
		if front["type"] != "socks5" || front["udp"] != want || mihomoBackend(b, b.realName())["udp"] != want {
			g.close()
			t.Fatal("render lost protocol-bound UDP flags")
		}
		g.close()
	}
}

func packetGuardFixture(t *testing.T, policy domain.Configuration, backends map[string]*egressBackend) *egressGuard {
	t.Helper()
	g, err := newEgressGuard(policy.Protection, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.close)
	g.udpEnabled = true
	g.backends = backends
	g.udpPolicy.Store(copyUDPPolicy(policy))
	if err := g.start(policy.Protection, time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	b := backends["node-a"]
	c, err := proxytransport.DialSOCKS5UDP(context.Background(), g.listener.Addr().String(), b.user, b.secret, udpGuardProbeTarget, time.Now().Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Write([]byte("proof")); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, 100)
	n, err := c.Read(buffer)
	_ = c.Close()
	if err != nil || string(buffer[:n]) != "proof" {
		t.Fatalf("guard fixture proof %q %v", buffer[:n], err)
	}
	for _, b := range backends {
		b.udpProbes.Store(0)
	}
	g.udpVerified.Store(true)
	return g
}

func TestUDPCertificateHardBoundAndMissingGuardFailClosed(t *testing.T) {
	f := newUDPIngressFixture(t)
	c, err := f.dial(t, "node-a", "1.1.1.1:443")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	packetEcho(t, c, []byte("certificate bound"))
	f.p.mu.Lock()
	f.p.certificateUntil = time.Now().Add(150 * time.Millisecond)
	f.p.until = f.p.capCertificate(time.Now().Add(time.Minute))
	until := f.p.until
	f.p.mu.Unlock()
	f.p.capPacketLeases()
	assertPacketClosed(t, c, until.Add(time.Second))
	missing := newUDPIngressFixture(t)
	missing.p.udpGuard = nil
	if c, err := missing.dial(t, "node-b", "1.1.1.1:443"); !errors.Is(err, proxytransport.ErrPacketUnsupported) {
		if c != nil {
			_ = c.Close()
		}
		t.Fatalf("missing verified guard accepted UDP: %v", err)
	}
}
