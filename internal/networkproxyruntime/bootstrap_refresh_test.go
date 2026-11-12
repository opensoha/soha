package networkproxyruntime

import (
	"bufio"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/proxytransport"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/networkprotocol"
	"golang.org/x/net/dns/dnsmessage"
	"os"
)

type bootstrapCountAdapter struct {
	engineAdapter
	starts *int
}

func (a bootstrapCountAdapter) commandArgs(path string, check bool) []string {
	if !check {
		*a.starts++
	}
	return a.engineAdapter.commandArgs(path, check)
}

func bootstrapRefreshFixture(t *testing.T, source string) (*Runner, net.Conn, net.Conn, *int) {
	t.Helper()
	config, _ := lifecycleFixture(t, "")
	config.RuntimeID = "runtime"
	config.InstanceID = "pod"
	config.ManagedDeployment = true
	config.IngressListen = "already-owned"
	e := newEngine(config, "controller-fixture-secret")
	t.Cleanup(e.stop)
	starts := new(int)
	e.adapter = bootstrapCountAdapter{engineAdapter: continuityAdapter{e.adapter}, starts: starts}
	desired := lifecycleDesired(1, 7890)
	desired.Content = fmt.Sprintf("port: 7890\nproxy-providers:\n  origin: {type: inline, payload: [{name: node, type: http, server: %s, port: 443}]}\n  other: {type: inline, payload: [{name: node-a, type: http, server: 1.0.0.1, port: 443}]}\nproxy-groups: [{name: choice, type: select, use: [origin, other]}]\nrule-providers: {fixture: {type: inline, behavior: domain, payload: [example.net]}}\nrules: [\"RULE-SET,fixture,choice\"]\n", source)
	desired.ContentHash = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(desired.Content)))
	pub := &api.NetworkProxyServerPublication{BundleID: "bundle", Revision: 1, SourceRevision: 1, CredentialEpoch: 1, ServerHash: desired.ContentHash, ValidUntil: desired.ValidUntil, DeliveryMode: "mapped_nodes", ConverterVersion: "fixture", MemberSetHash: desired.ContentHash, Nodes: []api.NetworkProxyServerPublicationNode{{NodeID: "node", OutboundID: "node"}, {NodeID: "node-a", OutboundID: "node-a"}}, Groups: []api.NetworkProxyGroup{}, Rules: []api.NetworkProxyRule{}}
	desired.Publication = pub
	applied := e.apply(context.Background(), desired)
	if applied.Status != "applied" {
		t.Fatalf("initial apply: %+v", applied)
	}
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		switch {
		case strings.HasSuffix(req.URL.Path, "/deployment/control"):
			g := confirmationGrant()
			g.StopRequired = false
			g.AcceptingRequests = true
			g.ControlRevision = 1
			g.ExpiresAt = time.Now().Add(30 * time.Second)
			_ = json.NewEncoder(w).Encode(g)
		case strings.HasSuffix(req.URL.Path, "/configuration"):
			d := desired
			d.ValidUntil = time.Now().Add(time.Minute)
			p := *pub
			p.ValidUntil = d.ValidUntil
			d.Publication = &p
			raw, _ := json.Marshal(d)
			_ = json.NewEncoder(w).Encode(proxyMessage{SchemaVersion: "proxy-runtime/v1alpha1", RuntimeID: "runtime", MessageType: "configuration.desired", OccurredAt: time.Now().UTC(), Payload: raw})
		case strings.HasSuffix(req.URL.Path, "/ingress/check"):
			var in api.NetworkProxyIngressCheckInput
			_ = json.NewDecoder(req.Body).Decode(&in)
			_ = json.NewEncoder(w).Encode(api.NetworkProxyIngressCheckResult{Allowed: true, BundleID: in.BundleID, NodeID: in.NodeID, Revision: in.Revision, CredentialEpoch: in.CredentialEpoch, ExpiresAt: time.Now().Add(5 * time.Second)})
		default:
			w.WriteHeader(204)
		}
	}))
	t.Cleanup(control.Close)
	client := NewClient(control.URL, "runtime", nil)
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	client.SetSchemas(schemas)
	backends := map[string]*egressBackend{"node": continuityBackend(t), "node-a": udpBackendFixture(t, "node-a")}
	scope, _ := proxytransport.NewScope(desired.Protection.CIDRs, desired.Protection.Hosts)
	p := &publicationIngress{client: client, epoch: e.processEpoch, publication: *pub, until: desired.ValidUntil, accepting: true, scope: scope, backends: backends, handlers: map[*proxytransport.Handler]struct{}{}, packetEnabled: true}
	p.udpPolicy.Store(copyUDPPolicy(desired))
	p.udpGuard = packetGuardFixture(t, desired, backends)
	server := httptest.NewTLSServer(p)
	p.server = server.Config
	p.listener = server.Listener
	t.Cleanup(func() { p.close(); server.Close() })
	dial := func(node string) net.Conn {
		c, err := tls.Dial("tcp", server.Listener.Addr().String(), fixtureValue[*http.Transport](t, server.Client().Transport).TLSClientConfig.Clone())
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
	c := dial("node")
	user, _ := proxytransport.EncodeMappedUsername("credential", "node")
	_, _ = fmt.Fprintf(c, "CONNECT 1.1.1.1:443 HTTP/1.1\r\nHost: 1.1.1.1:443\r\nProxy-Authorization: Basic %s\r\n\r\n", base64.StdEncoding.EncodeToString([]byte(user+":"+strings.Repeat("secret", 8))))
	reader := bufio.NewReader(c)
	response, err := readFixtureConnectStatus(t, c, reader)
	if err != nil || response != 200 {
		t.Fatalf("CONNECT: %v %v", response, err)
	}
	if _, err := io.ReadFull(reader, make([]byte, 1)); err != nil {
		t.Fatal(err)
	}
	user, _ = proxytransport.EncodeMappedUsername("credential", "node-a")
	udp, err := proxytransport.DialPacketTunnel(context.Background(), dial("node-a"), "1.1.1.1:443", user, strings.Repeat("secret", 8), time.Now().Add(5*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = udp.Close() })
	r := &Runner{config: config, client: client, engine: e, ingress: p, hostBootEpoch: "boot", enabled: true, validUntil: desired.ValidUntil, appliedRevision: 1, seenRevision: 1, seenHash: desired.Digest(), seenEnabled: true, lastAttempt: &applied, attemptReported: true, deployment: deploymentState{floor: 1, grant: &api.NetworkProxyDeploymentRuntimeControl{ControlRevision: 1, ExpiresAt: time.Now().Add(30 * time.Second), AcceptingRequests: true}}}
	return r, &bufferedFixtureConn{Conn: c, reader: reader}, udp, starts
}

func TestBootstrapRefreshCyclePreservesOriginalTCPUDPAndProcess(t *testing.T) {
	_, calls := bootstrapDNS(t)
	r, tcp, udp, starts := bootstrapRefreshFixture(t, "source.bootstrap.example")
	lookups := calls.Load()
	packetEcho(t, udp, []byte("before"))
	epoch, pid, gate, ingress := r.engine.processEpoch, r.engine.process.Process.Pid, r.engine.egress, r.ingress
	r.engine.bootstrapAt = time.Now().Add(-5 * time.Minute)
	before := time.Now()
	err := r.cycle(context.Background())
	if r.engine.processEpoch != epoch || r.engine.process.Process.Pid != pid || *starts != 1 || r.engine.egress != gate || r.ingress != ingress {
		t.Fatalf("unchanged bootstrap replaced process/guard/ingress: epochChanged=%v starts=%d cycleErr=%v", r.engine.processEpoch != epoch, *starts, err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if r.engine.bootstrapAt.Before(before) {
		t.Fatal("bootstrap revalidation time was not advanced")
	}
	if calls.Load()-lookups != 1 {
		t.Fatal("unchanged bootstrap skipped source DNS/pin revalidation")
	}
	if len(r.logs) != 1 || r.logs[0].Message != "bootstrap_refreshed_unchanged" {
		t.Fatal("missing bounded static bootstrap refresh event")
	}
	_ = tcp.SetDeadline(time.Now().Add(time.Second))
	_, err = tcp.Write([]byte("x"))
	if err != nil {
		t.Fatal(err)
	}
	one := make([]byte, 1)
	if _, err := io.ReadFull(tcp, one); err != nil || string(one) != "x" {
		t.Fatalf("original TCP lost: %q %v", one, err)
	}
	packetEcho(t, udp, []byte("after"))
}

func bootstrapDNS(t *testing.T) (*atomic.Uint32, *atomic.Int32) {
	t.Helper()
	listener, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ip := &atomic.Uint32{}
	ip.Store(1)
	calls := &atomic.Int32{}
	go func() {
		buf := make([]byte, 4096)
		for {
			n, from, err := listener.ReadFrom(buf)
			if err != nil {
				return
			}
			var parser dnsmessage.Parser
			header, err := parser.Start(buf[:n])
			if err != nil {
				continue
			}
			q, err := parser.Question()
			if err != nil {
				continue
			}
			h := dnsmessage.Header{ID: header.ID, Response: true, RecursionDesired: true, RecursionAvailable: true}
			if ip.Load() == 0 {
				h.RCode = dnsmessage.RCodeNameError
			}
			builder := dnsmessage.NewBuilder(nil, h)
			_ = builder.StartQuestions()
			_ = builder.Question(q)
			_ = builder.StartAnswers()
			if q.Type == dnsmessage.TypeA && ip.Load() != 0 {
				calls.Add(1)
				_ = builder.AResource(dnsmessage.ResourceHeader{Name: q.Name, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET}, dnsmessage.AResource{A: [4]byte{1, 1, 1, byte(ip.Load() & 0xff)}})
			}
			raw, err := builder.Finish()
			if err == nil {
				_, _ = listener.WriteTo(raw, from)
			}
		}
	}()
	previous := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{PreferGo: true, Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "udp", listener.LocalAddr().String())
	}}
	t.Cleanup(func() { net.DefaultResolver = previous; _ = listener.Close() })
	return ip, calls
}

func TestBootstrapRefreshChangedPinUsesOnePreparedSnapshot(t *testing.T) {
	ip, calls := bootstrapDNS(t)
	r, tcp, udp, starts := bootstrapRefreshFixture(t, "source.bootstrap.example")
	oldEpoch := r.engine.processEpoch
	r.engine.bootstrapAt = time.Now().Add(-5 * time.Minute)
	before := calls.Load()
	ip.Store(2)
	r.applyConfiguration(context.Background(), r.engine.desired)
	if r.suspended || *starts != 2 || r.engine.processEpoch == oldEpoch {
		t.Fatalf("changed pin did not safely replace: starts=%d reason=%s", *starts, r.reasonCode)
	}
	if calls.Load()-before != 1 {
		t.Fatalf("candidate resolved %d times; changed apply must reuse prepared pins", calls.Load()-before)
	}
	assertPacketClosed(t, tcp, time.Now().Add(time.Second))
	assertPacketClosed(t, udp, time.Now().Add(time.Second))
}

func TestBootstrapRefreshFailureAndReadbackDriftCloseOldFlows(t *testing.T) {
	for _, kind := range []string{"DNS failure", "new protection", "file drift", "guard drift", "expired configuration"} {
		t.Run(kind, func(t *testing.T) {
			ip, _ := bootstrapDNS(t)
			r, tcp, udp, starts := bootstrapRefreshFixture(t, "source.bootstrap.example")
			r.engine.bootstrapAt = time.Now().Add(-5 * time.Minute)
			desired := r.engine.desired
			switch kind {
			case "DNS failure":
				ip.Store(0)
			case "new protection":
				p := *desired.Protection
				p.PolicyVersion++
				p.CIDRs = []string{"1.1.1.0/24"}
				desired.Protection = &p
				desired.Revision++
			case "file drift":
				if err := os.WriteFile(r.config.ConfigFile, []byte("tampered"), 0600); err != nil {
					t.Fatal(err)
				}
			case "guard drift":
				r.engine.egress.close()
			case "expired configuration":
				desired.ValidUntil = time.Now().Add(-time.Millisecond)
			}
			r.applyConfiguration(context.Background(), desired)
			if !r.suspended || r.enabled || r.engine.process != nil || *starts != 1 {
				t.Fatalf("refresh failure retained serving state: reason=%s starts=%d", r.reasonCode, *starts)
			}
			assertPacketClosed(t, tcp, time.Now().Add(time.Second))
			assertPacketClosed(t, udp, time.Now().Add(time.Second))
		})
	}
}

func TestBootstrapFingerprintBindsMaterialAndIgnoresOnlyRenewableLeases(t *testing.T) {
	makeDesired := func() domain.Configuration {
		d := lifecycleDesired(1, 7890)
		d.Publication = &api.NetworkProxyServerPublication{BundleID: "bundle", Revision: 1, CredentialEpoch: 1, ServerHash: "hash", ValidUntil: d.ValidUntil}
		return d
	}
	for _, kind := range []string{"lease only", "observability only", "publication identity", "publication nodes", "publication groups", "publication hash", "binding internal", "protection", "source credential", "explicit UDP", "controller identity"} {
		t.Run(kind, func(t *testing.T) {
			e := newEngine(Config{Engine: domain.EngineMihomo, ControllerURL: "http://127.0.0.1:9090"}, "controller-secret")
			d := makeDesired()
			original, _, err := e.prepareBootstrap(context.Background(), d)
			if err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "lease only":
				d.ValidUntil = d.ValidUntil.Add(time.Minute)
				d.Publication.ValidUntil = d.ValidUntil
			case "observability only":
				e.config.HealthListen = "127.0.0.1:0"
				e.config.PollInterval = time.Second
			case "publication identity":
				d.Publication.CredentialEpoch++
			case "publication nodes":
				d.Publication.Nodes = []api.NetworkProxyServerPublicationNode{{NodeID: "node", OutboundID: "changed"}}
			case "publication groups":
				d.Publication.Groups = []api.NetworkProxyGroup{{GroupID: "changed"}}
			case "publication hash":
				d.Publication.ServerHash = "changed"
			case "binding internal":
				d.Binding.AuthorizationHash = "changed"
				d.Binding.OwnerUserID = "new-owner"
				d.Binding.ResourceSpaceIDs = map[string]string{"resource": "space"}
			case "protection":
				d.Protection.PolicyVersion++
			case "source credential":
				d.Content += "proxies: [{name: origin, type: http, server: 1.1.1.1, port: 443, password: changed}]\n"
			case "controller identity":
				e.config.ControllerURL = "http://127.0.0.1:9091"
			}
			next, _, err := e.prepareBootstrap(context.Background(), d)
			if err != nil {
				t.Fatal(err)
			}
			if kind == "explicit UDP" {
				next.explicitUDP["node"] = true
				next.fingerprint, err = e.bootstrapMaterialFingerprint(d, next)
				if err != nil {
					t.Fatal(err)
				}
			}
			if (original.fingerprint == next.fingerprint) != (kind == "lease only" || kind == "observability only") {
				t.Fatal("fingerprint missed material or included lease")
			}
			if e.egress != nil || e.process != nil {
				t.Fatal("preparation allocated an enforcement runtime")
			}
		})
	}
}

func TestBootstrapFingerprintBindsBinaryBytesAtSamePath(t *testing.T) {
	binary := t.TempDir() + "/binary"
	if err := os.WriteFile(binary, []byte("first operator binary"), 0700); err != nil { // #nosec G306 -- owner-only executable fixture in t.TempDir; fingerprint binds its bytes.
		t.Fatal(err)
	}
	e := newEngine(Config{Engine: domain.EngineMihomo, EngineBinary: binary, ControllerURL: "http://127.0.0.1:9090"}, "secret")
	d := lifecycleDesired(1, 7890)
	before, _, err := e.prepareBootstrap(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, []byte("new operator binary"), 0700); err != nil { // #nosec G306 -- replace the owner-only executable fixture to test same-path drift.
		t.Fatal(err)
	}
	after, _, err := e.prepareBootstrap(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if before.fingerprint == after.fingerprint {
		t.Fatal("binary replacement at same path was ignored")
	}
}

func TestBootstrapRefreshDisabledExpiredControlAndDrainCannotReuse(t *testing.T) {
	for _, kind := range []string{"disabled", "expired control", "drain"} {
		t.Run(kind, func(t *testing.T) {
			r, tcp, udp, starts := bootstrapRefreshFixture(t, "1.1.1.1")
			r.engine.bootstrapAt = time.Now().Add(-5 * time.Minute)
			old := r.engine.bootstrapAt
			desired := r.engine.desired
			switch kind {
			case "disabled":
				desired.Enabled = false
				desired.Revision++
			case "expired control":
				r.deployment.grant.ExpiresAt = time.Now()
				r.expire(time.Now())
			case "drain":
				r.deployment.grant.AcceptingRequests = false
				r.deployment.drainLease = time.Now().Add(100 * time.Millisecond)
			}
			r.applyConfiguration(context.Background(), desired)
			if !r.engine.bootstrapAt.Equal(old) || *starts != 1 {
				t.Fatal("inactive/draining state reused bootstrap refresh")
			}
			if kind == "drain" {
				time.Sleep(time.Until(r.deployment.drainLease))
				r.expire(time.Now())
			}
			if r.enabled || r.engine.process != nil {
				t.Fatal("inactive/expired state retained engine")
			}
			assertPacketClosed(t, tcp, time.Now().Add(time.Second))
			assertPacketClosed(t, udp, time.Now().Add(time.Second))
		})
	}
}

func TestBootstrapUsesVerifiedInstalledBinaryAlias(t *testing.T) {
	config, _ := lifecycleFixture(t, "")
	link := filepath.Join(t.TempDir(), "installed-engine")
	if err := os.Symlink(config.EngineBinary, link); err != nil {
		t.Fatal(err)
	}
	// A configured relative path remains supported without changing process cwd.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	relative, err := filepath.Rel(cwd, link)
	if err != nil {
		t.Fatal(err)
	}
	config.EngineBinary = relative
	e := newEngine(config, "controller-secret")
	defer e.stop()
	result := e.apply(context.Background(), lifecycleDesired(1, 7890))
	if result.Status != "applied" || e.kernel.path == link || e.process.Path != e.kernel.path {
		t.Fatalf("installed path not applied via verified target: %+v", result)
	}
	if _, err := hashKernelArtifact(link); err == nil {
		t.Fatal("explicit preflight accepted symlink")
	}
}
