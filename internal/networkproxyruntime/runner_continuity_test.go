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
	"strings"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/proxytransport"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/networkprotocol"
)

type continuityAdapter struct{ engineAdapter }

func (a continuityAdapter) observe(context.Context) (domain.Observation, []domain.Connection) {
	zero := 0
	return domain.Observation{Health: "healthy", EngineVersion: "fixture", Capabilities: []string{}, ActiveConnections: &zero}, nil
}

// Unlike mappedBackendFixture's short scenario, this backend permits a five-minute hold.
func continuityBackend(t *testing.T) *egressBackend {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = l.Close() })
	b := &egressBackend{name: "node", user: "backend", secret: strings.Repeat("s", 40), port: fixtureValue[*net.TCPAddr](t, l.Addr()).Port}
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }()
				_ = c.SetDeadline(time.Now().Add(5 * time.Minute))
				if !fixtureSOCKSAuth(c, fixtureListener{Users: []struct{ Username, Password string }{{b.user, b.secret}}}) {
					return
				}
				header := make([]byte, 4)
				if _, err := io.ReadFull(c, header); err != nil || header[3] != 1 {
					return
				}
				target := make([]byte, 6)
				if _, err := io.ReadFull(c, target); err != nil {
					return
				}
				_, _ = c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
				_, _ = io.WriteString(c, "A")
				_, _ = io.Copy(c, c)
			}()
		}
	}()
	return b
}

func TestRunnerNoActionContinuityFortySeconds(t *testing.T) {
	for _, failure := range []string{"healthy", "single-observe-400", "repeated-observe-400"} {
		t.Run(failure, func(t *testing.T) { runnerContinuity(t, failure) })
	}
}

func TestRunnerFirstObservationFailureDoesNotPublishIngress(t *testing.T) {
	runnerContinuity(t, "first-observe-400")
}

func runnerContinuity(t *testing.T, failure string) {
	config, _ := lifecycleFixture(t, "")
	config.RuntimeID, config.InstanceID, config.ManagedDeployment = "runtime", "pod", true
	config.PollInterval, config.IngressListen = 10*time.Second, "fixture-already-listening"
	e := newEngine(config, "controller-fixture-secret")
	defer e.stop()
	desired := lifecycleDesired(1, 7890)
	desired.ContentHash = fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(desired.Content)))
	applied := e.apply(context.Background(), desired)
	if applied.Status != "applied" {
		t.Fatalf("initial engine: %+v", applied)
	}
	e.adapter = continuityAdapter{e.adapter}
	publication := &api.NetworkProxyServerPublication{BundleID: "bundle", Revision: 1, CredentialEpoch: 1, ServerHash: desired.ContentHash, ValidUntil: desired.ValidUntil, Nodes: []api.NetworkProxyServerPublicationNode{{NodeID: "node", OutboundID: "node"}}}
	publication.ConverterVersion, publication.MemberSetHash, publication.SourceRevision = "fixture", desired.ContentHash, 1
	publication.DeliveryMode, publication.Groups, publication.Rules = "mapped_nodes", []api.NetworkProxyGroup{}, []api.NetworkProxyRule{}
	desired.Publication, e.desired.Publication = publication, publication
	started := time.Now()
	fixture := &continuityControlFixture{t: t, failure: failure, desired: desired, publication: publication, started: started}
	control := httptest.NewTLSServer(fixture)
	defer control.Close()
	client := NewClient(control.URL, "runtime", fixtureValue[*http.Transport](t, control.Client().Transport).TLSClientConfig.Clone())
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	client.SetSchemas(schemas)
	scope, _ := proxytransport.NewScope([]string{}, []string{})
	fixture.ingress = &publicationIngress{client: client, epoch: e.processEpoch, until: time.Now().Add(30 * time.Second), accepting: true, scope: scope, handlers: map[*proxytransport.Handler]struct{}{}, backends: map[string]*egressBackend{"node": continuityBackend(t)}, publication: *publication}
	server := httptest.NewTLSServer(fixture.ingress)
	defer server.Close()
	defer fixture.ingress.close()
	r := &Runner{config: config, client: client, engine: e, ingress: fixture.ingress, hostBootEpoch: "boot", enabled: true, appliedRevision: 1, validUntil: time.Now().Add(30 * time.Second), seenRevision: 1, seenHash: desired.Digest(), seenEnabled: true, seenPolicyVersion: 1, lastAttempt: &applied, attemptReported: true, deployment: deploymentState{floor: 1, grant: &api.NetworkProxyDeploymentRuntimeControl{ControlRevision: 1, ExpiresAt: time.Now().Add(30 * time.Second), AcceptingRequests: true}}}
	cycle := func() {
		err := r.cycle(context.Background())
		fixture.ingress.mu.Lock()
		until := fixture.ingress.until
		fixture.ingress.mu.Unlock()
		t.Logf("t=%v cycle err=%v control=%v config=%v publication=%v epoch=%s enabled=%v", time.Since(started).Round(time.Millisecond), err, r.deployment.grant.ExpiresAt.Sub(started).Round(time.Millisecond), r.validUntil.Sub(started).Round(time.Millisecond), until.Sub(started).Round(time.Millisecond), e.processEpoch, r.enabled)
		if err != nil && !strings.Contains(err.Error(), "HTTP 400") {
			t.Fatalf("unexpected cycle failure: %v", err)
		}
	}
	if failure == "first-observe-400" {
		r.ingress = nil
		cycle()
		if r.ingress != nil || fixture.readinessCalls.Load() != 0 {
			t.Fatal("ingress creation/readiness preceded accepted observation")
		}
		return
	}
	cycle()
	conn, reader, one := openContinuityStream(t, server)
	defer func() { _ = conn.Close() }()
	assertRunnerContinuityEcho(t, conn, reader, one, started, config.PollInterval, cycle)
	if fixture.checks.Load() < 20 {
		t.Fatalf("insufficient real rechecks: %d", fixture.checks.Load())
	}
	t.Logf("40s continuous flow, checks=%d observe=%d epoch=%s", fixture.checks.Load(), fixture.observeCalls.Load(), e.processEpoch)
}

type continuityControlFixture struct {
	t                                    *testing.T
	failure                              string
	desired                              domain.Configuration
	publication                          *api.NetworkProxyServerPublication
	started                              time.Time
	observeCalls, checks, readinessCalls atomic.Int32
	ingress                              *publicationIngress
}

func (f *continuityControlFixture) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	switch {
	case strings.HasSuffix(req.URL.Path, "/deployment/control"):
		now := time.Now()
		_ = json.NewEncoder(w).Encode(api.NetworkProxyDeploymentRuntimeControl{RuntimeID: "runtime", InstanceID: "pod", ProcessEpoch: "boot", CredentialID: "credential", CredentialEpoch: 1, OwnerEpoch: 1, ControlRevision: 1, IssuedAt: now, ExpiresAt: now.Add(30 * time.Second), AcceptingRequests: true})
	case strings.HasSuffix(req.URL.Path, "/configuration"):
		d := f.desired
		d.ValidUntil = time.Now().Add(30 * time.Second)
		pub := *f.publication
		pub.ValidUntil = d.ValidUntil
		d.Publication = &pub
		raw, _ := json.Marshal(d)
		_ = json.NewEncoder(w).Encode(proxyMessage{SchemaVersion: "proxy-runtime/v1alpha1", RuntimeID: "runtime", MessageType: "configuration.desired", OccurredAt: time.Now().UTC(), Payload: raw})
	case strings.HasSuffix(req.URL.Path, "/observations"):
		n := f.observeCalls.Add(1)
		if f.failure == "first-observe-400" || (f.failure == "single-observe-400" && n == 2) || (f.failure == "repeated-observe-400" && n >= 2) {
			f.t.Logf("t=%v Observe call=%d HTTP400", time.Since(f.started).Round(time.Millisecond), n)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	case strings.HasSuffix(req.URL.Path, "/ingress/check"):
		var input api.NetworkProxyIngressCheckInput
		if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
			f.t.Error(err)
			w.WriteHeader(400)
			return
		}
		f.checks.Add(1)
		f.ingress.mu.Lock()
		until := f.ingress.until
		f.ingress.mu.Unlock()
		f.t.Logf("t=%v ingress check=%d epoch=%s p.until=%v", time.Since(f.started).Round(time.Millisecond), f.checks.Load(), input.ProcessEpoch, until.Sub(f.started).Round(time.Millisecond))
		_ = json.NewEncoder(w).Encode(api.NetworkProxyIngressCheckResult{Allowed: true, BundleID: input.BundleID, NodeID: input.NodeID, Revision: input.Revision, CredentialEpoch: input.CredentialEpoch, ExpiresAt: time.Now().Add(5 * time.Second)})
	case strings.HasSuffix(req.URL.Path, "/publication/ready"):
		f.readinessCalls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

func assertRunnerContinuityEcho(t *testing.T, conn net.Conn, reader *bufio.Reader, one []byte, started time.Time, interval time.Duration, cycle func()) {
	t.Helper()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for time.Since(started) < 40*time.Second {
		select {
		case <-ticker.C:
			cycle()
		default:
		}
		_ = conn.SetDeadline(time.Now().Add(time.Second))
		if _, err := conn.Write([]byte("x")); err != nil {
			t.Fatalf("t=%v flow write: %v", time.Since(started), err)
		}
		if _, err := io.ReadFull(reader, one); err != nil || string(one) != "x" {
			t.Fatalf("t=%v flow lost with fresh grants: %q %v", time.Since(started), one, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func openContinuityStream(t *testing.T, server *httptest.Server) (net.Conn, *bufio.Reader, []byte) {
	t.Helper()
	conn, err := tls.Dial("tcp", server.Listener.Addr().String(), fixtureValue[*http.Transport](t, server.Client().Transport).TLSClientConfig.Clone())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
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
	return conn, reader, one
}
