package networkproxyruntime

import (
	"bufio"
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

func deadlineRunner(now time.Time) *Runner {
	pub := api.NetworkProxyServerPublication{BundleID: "bundle", Revision: 1, CredentialEpoch: 1, ServerHash: "hash", ValidUntil: now.Add(time.Minute)}
	e := newEngine(Config{Engine: domain.EngineMihomo}, "")
	e.processEpoch = "epoch"
	e.desired = domain.Configuration{Revision: 1, ContentHash: "hash", Publication: &pub}
	return &Runner{engine: e, enabled: true, appliedRevision: 1, validUntil: now.Add(time.Minute), ingress: &publicationIngress{epoch: "epoch", publication: pub, until: now.Add(time.Second), accepting: true}, config: Config{ManagedDeployment: true}, deployment: deploymentState{grant: &api.NetworkProxyDeploymentRuntimeControl{ExpiresAt: now.Add(time.Minute), AcceptingRequests: true}}}
}

func TestExistingIngressDeadlineRequiresCurrentOwnedIdentity(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name   string
		change func(*Runner)
	}{
		{"disabled", func(r *Runner) { r.enabled = false }},
		{"suspended", func(r *Runner) { r.suspended = true }},
		{"closed", func(r *Runner) { r.ingress.closed = true }},
		{"expired ingress", func(r *Runner) { r.ingress.until = now }},
		{"old engine epoch", func(r *Runner) { r.ingress.epoch = "old" }},
		{"unknown engine epoch", func(r *Runner) { r.engine.processEpoch = "" }},
		{"old bundle", func(r *Runner) { r.ingress.publication.BundleID = "old" }},
		{"old publication revision", func(r *Runner) { r.ingress.publication.Revision++ }},
		{"old credential epoch", func(r *Runner) { r.ingress.publication.CredentialEpoch++ }},
		{"old hash", func(r *Runner) { r.ingress.publication.ServerHash = "old" }},
		{"desired hash mismatch", func(r *Runner) { r.engine.desired.ContentHash = "changed" }},
		{"new configuration revision", func(r *Runner) { r.engine.desired.Revision++ }},
		{"missing publication", func(r *Runner) { r.engine.desired.Publication = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := deadlineRunner(now)
			tc.change(r)
			before := r.ingress.until
			r.refreshExistingIngressDeadline(now)
			if !r.ingress.until.Equal(before) {
				t.Fatal("unproven/expired ingress was extended")
			}
		})
	}
	r := deadlineRunner(now)
	r.ingress = nil
	r.refreshExistingIngressDeadline(now)
	if r.ingress != nil {
		t.Fatal("local deadline maintenance created ingress")
	}
}

func TestExistingIngressDeadlinePreservesEveryHardBound(t *testing.T) {
	now := time.Now()
	for _, kind := range []string{"configuration", "publication", "control", "certificate", "drain"} {
		for _, expired := range []bool{false, true} {
			t.Run(kind+"/expired="+map[bool]string{false: "false", true: "true"}[expired], func(t *testing.T) {
				r := deadlineRunner(now)
				bound := now.Add(2 * time.Second)
				if expired {
					bound = now
				}
				switch kind {
				case "configuration":
					r.validUntil = bound
				case "publication":
					r.engine.desired.Publication.ValidUntil = bound
				case "control":
					r.deployment.grant.ExpiresAt = bound
				case "certificate":
					r.ingress.certificateUntil = bound
				case "drain":
					r.deployment.drainLease = bound
				}
				before := r.ingress.until
				r.ingress.accepting = false
				r.refreshExistingIngressDeadline(now)
				want := bound
				if expired {
					want = before
				}
				if !r.ingress.until.Equal(want) {
					t.Fatalf("deadline=%v want=%v", r.ingress.until, want)
				}
				if r.ingress.accepting || r.health.snapshot.Load() != nil {
					t.Fatal("local maintenance changed accepting/readiness")
				}
			})
		}
	}
}

func TestExistingIngressDeadlineStillClosesRealFlowOnCoreDeny(t *testing.T) {
	var deny atomic.Bool
	control := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var input api.NetworkProxyIngressCheckInput
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		_ = json.NewEncoder(w).Encode(api.NetworkProxyIngressCheckResult{Allowed: !deny.Load(), BundleID: input.BundleID, NodeID: input.NodeID, Revision: input.Revision, CredentialEpoch: input.CredentialEpoch, ExpiresAt: time.Now().Add(5 * time.Second)})
	}))
	defer control.Close()
	client := NewClient(control.URL, "runtime", fixtureValue[*http.Transport](t, control.Client().Transport).TLSClientConfig.Clone())
	p, stream := stopStreamFixture(t, client)
	defer p.close()
	r := deadlineRunner(time.Now())
	r.ingress = p
	r.engine.processEpoch = p.epoch
	p.publication.ServerHash = "hash"
	r.refreshExistingIngressDeadline(time.Now())
	deny.Store(true)
	_ = stream.SetReadDeadline(time.Now().Add(3 * time.Second))
	_, err := stream.Read(make([]byte, 1))
	if err == nil {
		t.Fatal("Core denial retained real flow")
	}
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("Core denial was hidden by local deadline renewal")
	}
}

func TestExistingIngressDeadlineHardExpiryClosesRealFlow(t *testing.T) {
	control := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var input api.NetworkProxyIngressCheckInput
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			t.Error(err)
			w.WriteHeader(400)
			return
		}
		_ = json.NewEncoder(w).Encode(api.NetworkProxyIngressCheckResult{Allowed: true, BundleID: input.BundleID, NodeID: input.NodeID, Revision: input.Revision, CredentialEpoch: input.CredentialEpoch, ExpiresAt: time.Now().Add(5 * time.Second)})
	}))
	defer control.Close()
	for _, kind := range []string{"configuration", "publication", "control", "certificate", "drain"} {
		t.Run(kind, func(t *testing.T) {
			now := time.Now()
			r := deadlineRunner(now)
			p := r.ingress
			boundary := now.Add(100 * time.Millisecond)
			p.until = boundary
			switch kind {
			case "configuration":
				r.validUntil = boundary
			case "publication":
				r.engine.desired.Publication.ValidUntil = boundary
			case "control":
				r.deployment.grant.ExpiresAt = boundary
			case "certificate":
				p.certificateUntil = boundary
			case "drain":
				r.deployment.drainLease = boundary
			}
			p.client = NewClient(control.URL, "runtime", fixtureValue[*http.Transport](t, control.Client().Transport).TLSClientConfig.Clone())
			p.scope, _ = proxytransport.NewScope([]string{}, []string{})
			p.backends = map[string]*egressBackend{"node": continuityBackend(t)}
			p.handlers = map[*proxytransport.Handler]struct{}{}
			p.publication.Nodes = []api.NetworkProxyServerPublicationNode{{NodeID: "node", OutboundID: "node"}}
			server := httptest.NewTLSServer(p)
			defer server.Close()
			defer p.close()
			conn, err := tls.Dial("tcp", server.Listener.Addr().String(), fixtureValue[*http.Transport](t, server.Client().Transport).TLSClientConfig.Clone())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			_ = conn.SetDeadline(boundary.Add(200 * time.Millisecond))
			user, _ := proxytransport.EncodeMappedUsername("credential", "node")
			_, err = fmt.Fprintf(conn, "CONNECT 1.1.1.1:443 HTTP/1.1\r\nHost: 1.1.1.1:443\r\nProxy-Authorization: Basic %s\r\n\r\n", base64.StdEncoding.EncodeToString([]byte(user+":"+strings.Repeat("secret", 8))))
			if err != nil {
				t.Fatal(err)
			}
			reader := bufio.NewReader(conn)
			response, err := readFixtureConnectStatus(t, conn, reader)
			if err != nil || response != 200 {
				t.Fatalf("CONNECT: %v %v", response, err)
			}
			one := make([]byte, 1)
			if _, err := io.ReadFull(reader, one); err != nil || string(one) != "A" {
				t.Fatalf("backend: %q %v", one, err)
			}
			r.refreshExistingIngressDeadline(time.Now())
			if !p.until.Equal(boundary) {
				t.Fatal("local refresh widened hard boundary")
			}
			_, err = reader.Read(one)
			if err == nil {
				t.Fatal("expired flow survived")
			}
			if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				t.Fatal("flow did not close at original hard boundary")
			}
			if time.Now().Before(boundary) {
				t.Fatal("flow closed before hard boundary")
			}
		})
	}
}
