package networkproxyruntime

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/proxytransport"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
)

// The backend records the pinned literal destination and echoes a fixed node
// identity. It never dials the internet or the fixture request's destination.
func mappedBackendFixture(t *testing.T, name string) *egressBackend {
	t.Helper()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	b := &egressBackend{name: name, user: "backend-user", secret: strings.Repeat("s", 40), port: listener.Addr().(*net.TCPAddr).Port}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
				if !fixtureSOCKSAuth(conn, fixtureListener{Users: []struct{ Username, Password string }{{b.user, b.secret}}}) {
					return
				}
				header := make([]byte, 4)
				if _, err := io.ReadFull(conn, header); err != nil {
					return
				}
				var host string
				switch header[3] {
				case 1:
					raw := make([]byte, 4)
					if _, err := io.ReadFull(conn, raw); err != nil {
						return
					}
					host = net.IP(raw).String()
				case 4:
					raw := make([]byte, 16)
					if _, err := io.ReadFull(conn, raw); err != nil {
						return
					}
					host = net.IP(raw).String()
				default:
					return
				}
				port := make([]byte, 2)
				if _, err := io.ReadFull(conn, port); err != nil {
					return
				}
				if host != "1.1.1.1" {
					t.Errorf("backend received unpinned or unexpected destination %s", host)
					return
				}
				_, _ = conn.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
				_, _ = io.WriteString(conn, name)
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	return b
}

func TestTLSMappedIngressRoutesAndRevokes(t *testing.T) {
	var revoked atomic.Bool
	var checks atomic.Int32
	secret := strings.Repeat("employee-secret", 3)
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/ingress/check") {
			t.Error("unexpected control path")
		}
		var input api.NetworkProxyIngressCheckInput
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			w.WriteHeader(400)
			return
		}
		checks.Add(1)
		allowed := !revoked.Load() && input.CredentialSecret == secret && input.ProcessEpoch == "engine-epoch" && input.CredentialID == "credential"
		_ = json.NewEncoder(w).Encode(api.NetworkProxyIngressCheckResult{Allowed: allowed, BundleID: input.BundleID, Revision: input.Revision, NodeID: input.NodeID, CredentialEpoch: input.CredentialEpoch, ExpiresAt: time.Now().Add(time.Minute)})
	}))
	defer control.Close()
	scope, err := proxytransport.NewScope([]string{"10.0.0.0/8"}, []string{"protected.example"})
	if err != nil {
		t.Fatal(err)
	}
	p := &publicationIngress{client: NewClient(control.URL, "runtime", nil), epoch: "engine-epoch", until: time.Now().Add(time.Minute), accepting: true, scope: scope, handlers: map[*proxytransport.Handler]struct{}{}, backends: map[string]*egressBackend{"out-a": mappedBackendFixture(t, "A"), "out-b": mappedBackendFixture(t, "B")}, publication: api.NetworkProxyServerPublication{BundleID: "bundle", Revision: 1, CredentialEpoch: 1, Nodes: []api.NetworkProxyServerPublicationNode{{NodeID: "node-a", OutboundID: "out-a"}, {NodeID: "node-b", OutboundID: "out-b"}}}}
	server := httptest.NewTLSServer(p)
	defer server.Close()
	defer p.close()
	connect := func(node, destination, password string) (net.Conn, *http.Response) {
		t.Helper()
		cfg := server.Client().Transport.(*http.Transport).TLSClientConfig.Clone()
		conn, err := tls.Dial("tcp", server.Listener.Addr().String(), cfg)
		if err != nil {
			t.Fatal(err)
		}
		user, err := proxytransport.EncodeMappedUsername("credential", node)
		if err != nil {
			t.Fatal(err)
		}
		auth := base64.StdEncoding.EncodeToString([]byte(user + ":" + password))
		_, err = fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\nProxy-Authorization: Basic %s\r\n\r\n", destination, destination, auth)
		if err != nil {
			t.Fatal(err)
		}
		reader := bufio.NewReader(conn)
		response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
		if err != nil {
			conn.Close()
			t.Fatal(err)
		}
		return &bufferedFixtureConn{Conn: conn, reader: reader}, response
	}
	for _, test := range []struct{ node, identity string }{{"node-a", "A"}, {"node-b", "B"}} {
		conn, response := connect(test.node, "1.1.1.1:443", secret)
		if response.StatusCode != 200 {
			t.Fatalf("mapping %s: HTTP %d", test.node, response.StatusCode)
		}
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		raw := make([]byte, 1)
		if _, err := io.ReadFull(conn, raw); err != nil || string(raw) != test.identity {
			t.Fatalf("mapping identity %q, %v", raw, err)
		}
		conn.Close()
	}
	for _, test := range []struct {
		node, destination, password string
		status                      int
	}{{"node-a", "10.0.0.1:443", secret, 502}, {"node-a", "protected.example:443", secret, 502}, {"node-a", "1.1.1.1:443", "wrong", 403}, {"node-unknown", "1.1.1.1:443", secret, 403}} {
		conn, response := connect(test.node, test.destination, test.password)
		if response.StatusCode != test.status {
			t.Errorf("rejection HTTP %d; want %d", response.StatusCode, test.status)
		}
		conn.Close()
	}
	conn, response := connect("node-a", "1.1.1.1:443", secret)
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	_ = conn.SetReadDeadline(time.Now().Add(time.Second))
	one := make([]byte, 1)
	_, _ = io.ReadFull(conn, one)
	revoked.Store(true)
	start := time.Now()
	_ = conn.SetReadDeadline(time.Now().Add(9 * time.Second))
	_, err = conn.Read(one)
	conn.Close()
	if err == nil {
		t.Fatal("revoked stream remained open")
	}
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("revoked stream not closed within fixture budget")
	}
	if time.Since(start) > 9*time.Second {
		t.Fatal("revocation exceeded fixture budget")
	}
	if checks.Load() < 6 {
		t.Fatal("missing online or active-flow checks")
	}
	p.setAccepting(false)
	conn, response = connect("node-b", "1.1.1.1:443", secret)
	if response.StatusCode != 503 {
		t.Fatal("draining ingress admitted new request")
	}
	conn.Close()
}

func TestPublicationDisablesUnmanagedListenersAndRejectsWrongOutbound(t *testing.T) {
	g, err := newEgressGuard(nil, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	defer g.close()
	b, err := g.add("node-a")
	if err != nil {
		t.Fatal(err)
	}
	_ = b
	until := time.Now().Add(time.Minute)
	desired := domain.Configuration{ContentHash: "hash", ValidUntil: until, Publication: &api.NetworkProxyServerPublication{ServerHash: "hash", ValidUntil: until, DeliveryMode: "mapped_nodes", Nodes: []api.NetworkProxyServerPublicationNode{{NodeID: "node-a", OutboundID: "node-a"}}, Groups: []api.NetworkProxyGroup{}, Rules: []api.NetworkProxyRule{}}}
	doc := map[string]any{"port": 7890, "allow-lan": true, "proxy-groups": []any{}}
	if err := g.compilePublication(doc, desired, domain.EngineMihomo); err != nil {
		t.Fatal(err)
	}
	if doc["port"] != 0 || doc["allow-lan"] != false || doc["bind-address"] != "127.0.0.1" {
		t.Fatal("unmanaged ingress remains exposed")
	}
	desired.Publication.Nodes[0].OutboundID = "unapproved"
	if g.compilePublication(doc, desired, domain.EngineMihomo) == nil {
		t.Fatal("unapproved outbound accepted")
	}
}

type bufferedFixtureConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedFixtureConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

func TestGatewayPublicationPinsAggregateAndMandatoryReject(t *testing.T) {
	g, err := newEgressGuard(nil, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	defer g.close()
	for _, name := range []string{"node-a", "node-b"} {
		if _, err := g.add(name); err != nil {
			t.Fatal(err)
		}
	}
	until := time.Now().Add(time.Minute)
	p := &api.NetworkProxyServerPublication{ServerHash: "hash", ValidUntil: until, DeliveryMode: "gateway", Nodes: []api.NetworkProxyServerPublicationNode{{NodeID: "gateway", OutboundID: "aggregate"}}, Groups: []api.NetworkProxyGroup{{GroupID: "aggregate", Type: "select", NodeIDs: []string{"node-a", "node-b"}}}, Rules: []api.NetworkProxyRule{}}
	doc := map[string]any{"proxy-groups": []any{map[string]any{"name": "aggregate", "type": "select", "proxies": []any{"node-a", "node-b"}}}, "listeners": []any{}}
	if err := g.compilePublication(doc, domain.Configuration{ContentHash: "hash", ValidUntil: until, Publication: p}, domain.EngineMihomo); err != nil {
		t.Fatal(err)
	}
	listeners := doc["listeners"].([]any)
	if len(listeners) != 1 || listeners[0].(map[string]any)["proxy"] != "aggregate" || !g.backends["aggregate"].group {
		t.Fatal("gateway not pinned to actual aggregate")
	}
	scope, _ := proxytransport.NewScope([]string{}, []string{})
	p.Rules = []api.NetworkProxyRule{{RuleID: "mandatory", Source: "organization_required", Enabled: true, Matcher: api.NetworkProxyRuleMatcher{Type: "all"}, Action: api.NetworkProxyRuleAction{Type: "reject"}}}
	ingress := &publicationIngress{scope: scope, publication: *p, backends: g.backends}
	if conn, err := ingress.dial(context.Background(), "1.1.1.1:443", "gateway", "aggregate", g.backends["aggregate"]); err == nil {
		conn.Close()
		t.Fatal("mandatory rejection bypassed fixed outbound")
	}
}
