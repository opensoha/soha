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
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/proxytransport"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
)

func confirmationGrant() api.NetworkProxyDeploymentRuntimeControl {
	return api.NetworkProxyDeploymentRuntimeControl{RuntimeID: "runtime", InstanceID: "pod", ProcessEpoch: "boot", CredentialID: "credential", CredentialEpoch: 1, OwnerEpoch: 1, ControlRevision: 2, IssuedAt: time.Now().Add(-time.Millisecond), ExpiresAt: time.Now().Add(5 * time.Second), StopRequired: true}
}

func stopStreamFixture(t *testing.T, client *Client) (*publicationIngress, net.Conn) {
	t.Helper()
	scope, _ := proxytransport.NewScope([]string{}, []string{})
	p := &publicationIngress{client: client, epoch: "engine", until: time.Now().Add(time.Minute), accepting: true, scope: scope, handlers: map[*proxytransport.Handler]struct{}{}, backends: map[string]*egressBackend{"node": mappedBackendFixture(t, "A")}, publication: api.NetworkProxyServerPublication{BundleID: "bundle", Revision: 1, CredentialEpoch: 1, Nodes: []api.NetworkProxyServerPublicationNode{{NodeID: "node", OutboundID: "node"}}}}
	server := httptest.NewTLSServer(p)
	p.server, p.listener = server.Config, server.Listener
	t.Cleanup(server.Close)
	conn, err := tls.Dial("tcp", server.Listener.Addr().String(), fixtureValue[*http.Transport](t, server.Client().Transport).TLSClientConfig.Clone())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	user, _ := proxytransport.EncodeMappedUsername("credential", "node")
	auth := base64.StdEncoding.EncodeToString([]byte(user + ":" + strings.Repeat("secret", 8)))
	_, err = fmt.Fprintf(conn, "CONNECT 1.1.1.1:443 HTTP/1.1\r\nHost: 1.1.1.1:443\r\nProxy-Authorization: Basic %s\r\n\r\n", auth)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	response, err := readFixtureConnectStatus(t, conn, reader)
	if err != nil || response != 200 {
		t.Fatalf("CONNECT %v %v", response, err)
	}
	identity := make([]byte, 1)
	if _, err := io.ReadFull(reader, identity); err != nil || string(identity) != "A" {
		t.Fatalf("identity %q %v", identity, err)
	}
	return p, &bufferedFixtureConn{Conn: conn, reader: reader}
}

func TestStopConfirmationReapsOwnedProcessAndPersistentIngress(t *testing.T) {
	grant := confirmationGrant()
	var received atomic.Pointer[api.NetworkProxyDeploymentRuntimeAck]
	var configReads atomic.Int32
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch {
		case strings.HasSuffix(request.URL.Path, "/ingress/check"):
			var input api.NetworkProxyIngressCheckInput
			_ = json.NewDecoder(request.Body).Decode(&input)
			_ = json.NewEncoder(w).Encode(api.NetworkProxyIngressCheckResult{Allowed: true, BundleID: input.BundleID, Revision: input.Revision, NodeID: input.NodeID, CredentialEpoch: input.CredentialEpoch, ExpiresAt: time.Now().Add(5 * time.Second)})
		case strings.HasSuffix(request.URL.Path, "/deployment/control"):
			if request.URL.Query().Get("processEpoch") != "boot" || request.URL.Query().Get("instanceId") != "pod" {
				t.Error("control identity mismatch")
			}
			_ = json.NewEncoder(w).Encode(grant)
		case strings.HasSuffix(request.URL.Path, "/control/receipt"):
			var ack api.NetworkProxyDeploymentRuntimeAck
			_ = json.NewDecoder(request.Body).Decode(&ack)
			received.Store(&ack)
			_ = json.NewEncoder(w).Encode(ack)
		default:
			configReads.Add(1)
			w.WriteHeader(409)
		}
	}))
	defer control.Close()
	client := NewClient(control.URL, "runtime", nil)
	config, _ := lifecycleFixture(t, "")
	e := newEngine(config, "controller-fixture-secret")
	defer e.stop()
	result := e.apply(context.Background(), lifecycleDesired(1, 7890))
	if result.Status != "applied" {
		t.Fatalf("apply %+v", result)
	}
	child := e.process
	p, stream := stopStreamFixture(t, client)
	drainBoundary := time.Now().Add(200 * time.Millisecond)
	r := &Runner{config: Config{RuntimeID: "runtime", InstanceID: "pod", ManagedDeployment: true}, hostBootEpoch: "boot", engine: e, client: client, ingress: p, enabled: true, validUntil: drainBoundary, deployment: deploymentState{floor: 1, grant: &api.NetworkProxyDeploymentRuntimeControl{ControlRevision: 1, ExpiresAt: drainBoundary}}}
	// Keep a real TCP stream alive until the fixed drain boundary.
	for time.Now().Before(drainBoundary) {
		_ = stream.SetDeadline(time.Now().Add(time.Second))
		_, err := stream.Write([]byte("x"))
		if err != nil {
			t.Fatal(err)
		}
		one := make([]byte, 1)
		if _, err := io.ReadFull(stream, one); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := r.cycle(context.Background()); err != nil {
		t.Fatal(err)
	}
	if configReads.Load() != 0 {
		t.Fatal("confirmation attempted expired configuration/publication reads")
	}
	proof := received.Load()
	assertConfirmedOwnedStopProof(t, proof)
	if e.process != nil || child.ProcessState == nil {
		t.Fatal("owned child was not reaped")
	}
	if p.active.Load() != 0 {
		t.Fatal("owned ingress stream remains active")
	}
	_ = stream.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := stream.Read(make([]byte, 1)); err == nil {
		t.Fatal("old TCP stream survived stop proof")
	}
	if conn, err := net.DialTimeout("tcp", p.listener.Addr().String(), time.Second); err == nil {
		_ = conn.Close()
		t.Fatal("owned TLS listener survived stop proof")
	}
	late := grant
	late.ControlRevision = 1
	late.StopRequired = false
	late.AcceptingRequests = true
	if r.acceptDeployment(late, time.Now()) == nil || e.process != nil {
		t.Fatal("late serving revision resurrected the process")
	}
}

func TestRunPollsStopConfirmationAfterExpiredGrant(t *testing.T) {
	grant := confirmationGrant()
	var configReads atomic.Int32
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if strings.HasSuffix(request.URL.Path, "/deployment/control") {
			_ = json.NewEncoder(w).Encode(grant)
			return
		}
		if strings.HasSuffix(request.URL.Path, "/control/receipt") {
			var ack api.NetworkProxyDeploymentRuntimeAck
			_ = json.NewDecoder(request.Body).Decode(&ack)
			if !ack.ProcessStopped {
				t.Error("missing stop proof")
			}
			_ = json.NewEncoder(w).Encode(ack)
			cancel()
			return
		}
		configReads.Add(1)
		w.WriteHeader(409)
	}))
	defer server.Close()
	r := &Runner{config: Config{RuntimeID: "runtime", InstanceID: "pod", ManagedDeployment: true, PollInterval: 10 * time.Millisecond}, hostBootEpoch: "boot", engine: newEngine(Config{Engine: domain.EngineMihomo}, ""), client: NewClient(server.URL, "runtime", nil), deployment: deploymentState{floor: 1, grant: &api.NetworkProxyDeploymentRuntimeControl{ControlRevision: 1, ExpiresAt: time.Now().Add(-time.Second)}}}
	if err := r.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if r.deployment.floor != 2 || configReads.Load() != 0 {
		t.Fatal("expired business lease blocked the confirmation-only control path")
	}
}

func TestStopConfirmationDoesNotInventZeroForUnknownOwnedHandle(t *testing.T) {
	var received atomic.Pointer[api.NetworkProxyDeploymentRuntimeAck]
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ack api.NetworkProxyDeploymentRuntimeAck
		_ = json.NewDecoder(r.Body).Decode(&ack)
		received.Store(&ack)
		_ = json.NewEncoder(w).Encode(ack)
	}))
	defer server.Close()
	e := newEngine(Config{Engine: domain.EngineMihomo}, "")
	e.process = &exec.Cmd{}
	r := &Runner{config: Config{RuntimeID: "runtime", ManagedDeployment: true}, hostBootEpoch: "boot", client: NewClient(server.URL, "runtime", nil), engine: e, deployment: deploymentState{grant: ptrConfirmationGrant()}}
	if err := r.confirmOwnedStop(context.Background()); err == nil {
		t.Fatal("unknown child handle reported success")
	}
	proof := received.Load()
	if proof == nil || proof.ProcessStopped || proof.ActiveCountKnown {
		t.Fatalf("unknown stop fabricated zero proof %+v", proof)
	}
}

func ptrConfirmationGrant() *api.NetworkProxyDeploymentRuntimeControl {
	g := confirmationGrant()
	return &g
}

func TestStopConfirmationGrantCannotRenewDeadlineOrServe(t *testing.T) {
	g := confirmationGrant()
	r := &Runner{config: Config{RuntimeID: "runtime", InstanceID: "pod", ManagedDeployment: true}, hostBootEpoch: "boot"}
	if err := r.acceptDeployment(g, time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*api.NetworkProxyDeploymentRuntimeControl){
		func(g *api.NetworkProxyDeploymentRuntimeControl) { g.ExpiresAt = g.ExpiresAt.Add(time.Second) },
		func(g *api.NetworkProxyDeploymentRuntimeControl) { g.AcceptingRequests = true },
		func(g *api.NetworkProxyDeploymentRuntimeControl) { g.ExpiresAt = g.IssuedAt.Add(31 * time.Second) },
		func(g *api.NetworkProxyDeploymentRuntimeControl) { g.ControlRevision-- },
	} {
		bad := g
		change(&bad)
		if r.acceptDeployment(bad, time.Now()) == nil {
			t.Fatal("invalid confirmation mutation accepted")
		}
	}
	if r.deployment.mayApply(time.Now()) {
		t.Fatal("stop-only grant allows configuration startup")
	}
}

func assertConfirmedOwnedStopProof(t *testing.T, proof *api.NetworkProxyDeploymentRuntimeAck) {
	t.Helper()
	if proof == nil || !proof.ProcessStopped || !proof.ActiveCountKnown || proof.ActiveConnections != 0 || proof.AcceptingRequests || proof.ControlRevision != 2 || proof.ProcessEpoch != "boot" {
		t.Fatalf("incorrect stop proof %+v", proof)
	}
}
