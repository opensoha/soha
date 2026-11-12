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
)

func TestIngressRenewsAcrossShortGrantWindowsAndClosesAtFailedLease(t *testing.T) {
	const window = 5 * time.Second
	var failed atomic.Bool
	var checks atomic.Int32
	var lastExpiry atomic.Pointer[time.Time]
	failingCall := make(chan struct{}, 1)
	control := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input api.NetworkProxyIngressCheckInput
		if json.NewDecoder(r.Body).Decode(&input) != nil {
			w.WriteHeader(400)
			return
		}
		checks.Add(1)
		if failed.Load() {
			select {
			case failingCall <- struct{}{}:
			default:
			}
			// A stalled API must be cancelled within the old authorization lease.
			<-r.Context().Done()
			return
		}
		until := time.Now().Add(window)
		lastExpiry.Store(&until)
		_ = json.NewEncoder(w).Encode(api.NetworkProxyIngressCheckResult{Allowed: true, BundleID: input.BundleID, NodeID: input.NodeID, Revision: input.Revision, CredentialEpoch: input.CredentialEpoch, ExpiresAt: until})
	}))
	defer control.Close()
	scope, err := proxytransport.NewScope([]string{}, []string{})
	if err != nil {
		t.Fatal(err)
	}
	p := &publicationIngress{client: NewClient(control.URL, "runtime", nil), epoch: "engine", until: time.Now().Add(time.Minute), accepting: true, scope: scope, handlers: map[*proxytransport.Handler]struct{}{}, backends: map[string]*egressBackend{"node": mappedBackendFixture(t, "A")}, publication: api.NetworkProxyServerPublication{BundleID: "bundle", Revision: 1, CredentialEpoch: 1, Nodes: []api.NetworkProxyServerPublicationNode{{NodeID: "node", OutboundID: "node"}}}}
	server := httptest.NewTLSServer(p)
	defer server.Close()
	defer p.close()
	config := fixtureValue[*http.Transport](t, server.Client().Transport).TLSClientConfig.Clone()
	conn, err := tls.Dial("tcp", server.Listener.Addr().String(), config)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	username, err := proxytransport.EncodeMappedUsername("credential", "node")
	if err != nil {
		t.Fatal(err)
	}
	auth := base64.StdEncoding.EncodeToString([]byte(username + ":" + strings.Repeat("secret", 8)))
	_, err = fmt.Fprintf(conn, "CONNECT 1.1.1.1:443 HTTP/1.1\r\nHost: 1.1.1.1:443\r\nProxy-Authorization: Basic %s\r\n\r\n", auth)
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	response, err := readFixtureConnectStatus(t, conn, reader)
	if err != nil || response != 200 {
		t.Fatalf("CONNECT response %v, %v", response, err)
	}
	identity := make([]byte, 1)
	if _, err := io.ReadFull(reader, identity); err != nil || string(identity) != "A" {
		t.Fatalf("backend identity %q %v", identity, err)
	}
	assertIngressRenewalContinuity(t, conn, reader, window)
	if checks.Load() < 5 {
		t.Fatalf("too few genuine lease renewals: %d", checks.Load())
	}
	failed.Store(true)
	select {
	case <-failingCall:
	case <-time.After(3 * time.Second):
		t.Fatal("next authorization renewal was not attempted")
	}
	expiry := *lastExpiry.Load()
	_ = conn.SetReadDeadline(expiry.Add(time.Second))
	_, err = reader.Read(identity)
	if err == nil {
		t.Fatal("stalled authorization retained the stream")
	}
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("stream was not closed by its old authorization lease")
	}
	if time.Now().After(expiry.Add(time.Second)) {
		t.Fatal("API retry extended the previous lease")
	}
}

func TestIngressRenewalScheduleLeavesControlBudget(t *testing.T) {
	now := time.Now()
	for _, lease := range []time.Duration{5 * time.Second, time.Second, 100 * time.Millisecond} {
		delay := ingressRenewDelay(now.Add(lease), now)
		if delay <= 0 || delay > 2*time.Second || delay >= lease {
			t.Fatalf("invalid renewal delay %s for %s lease", delay, lease)
		}
	}
}

func assertIngressRenewalContinuity(t *testing.T, conn net.Conn, reader *bufio.Reader, window time.Duration) {
	t.Helper()
	identity := make([]byte, 1)
	started := time.Now()
	for time.Since(started) < 2*window+500*time.Millisecond {
		_ = conn.SetDeadline(time.Now().Add(time.Second))
		if _, err := conn.Write([]byte("x")); err != nil {
			t.Fatalf("stream closed while authorization renews: %v", err)
		}
		if _, err := io.ReadFull(reader, identity); err != nil || string(identity) != "x" {
			t.Fatalf("stream failed before two grant windows: %q %v", identity, err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
