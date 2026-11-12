package networkproxyruntime

import (
	"bufio"
	"context"
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

func TestPollingDelayIgnoresExpiredBoundaries(t *testing.T) {
	now := time.Now()
	for _, tc := range []struct {
		name                   string
		control, config, drain time.Time
		want                   time.Duration
	}{
		{"expired control", now.Add(-time.Second), time.Time{}, time.Time{}, time.Second},
		{"expired configuration", time.Time{}, now.Add(-time.Second), time.Time{}, time.Second},
		{"at control boundary", now, time.Time{}, time.Time{}, time.Second},
		{"future control", now.Add(100 * time.Millisecond), time.Time{}, time.Time{}, 100 * time.Millisecond},
		{"future configuration", time.Time{}, now.Add(50 * time.Millisecond), time.Time{}, 50 * time.Millisecond},
		{"captured drain boundary", now.Add(time.Minute), time.Time{}, now.Add(30 * time.Millisecond), 30 * time.Millisecond},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &Runner{config: Config{PollInterval: time.Second, ManagedDeployment: !tc.control.IsZero()}, enabled: !tc.config.IsZero(), validUntil: tc.config}
			if r.config.ManagedDeployment {
				r.deployment = deploymentState{grant: &api.NetworkProxyDeploymentRuntimeControl{ExpiresAt: tc.control}, drainLease: tc.drain}
			}
			if got := r.nextCycleDelay(now); got != tc.want {
				t.Fatalf("delay = %v, want %v", got, tc.want)
			}
		})
	}
}

func pollingRunner(server *httptest.Server, managed bool) *Runner {
	return &Runner{config: Config{RuntimeID: "runtime", InstanceID: "pod", ManagedDeployment: managed, PollInterval: 80 * time.Millisecond, Engine: domain.EngineMihomo}, hostBootEpoch: "boot", client: NewClient(server.URL, "runtime", nil), engine: newEngine(Config{Engine: domain.EngineMihomo}, "")}
}

func TestRunPollingRequestCountRemainsBounded(t *testing.T) {
	for _, scenario := range []string{"expired-control-429", "expired-control-200", "expired-config-429", "near-boundary-success", "near-boundary-receipt-429"} {
		t.Run(scenario, func(t *testing.T) {
			var requests, controls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				requests.Add(1)
				if strings.HasSuffix(req.URL.Path, "/deployment/control") {
					n := controls.Add(1)
					if strings.HasPrefix(scenario, "near-boundary") {
						g := confirmationGrant()
						g.ControlRevision = int64(n + 1)
						g.ExpiresAt = time.Now().Add(8 * time.Millisecond)
						_ = json.NewEncoder(w).Encode(g)
						return
					}
					if scenario == "expired-control-200" {
						g := confirmationGrant()
						g.ExpiresAt = time.Now().Add(-time.Second)
						_ = json.NewEncoder(w).Encode(g)
						return
					}
				}
				if scenario == "near-boundary-success" && strings.HasSuffix(req.URL.Path, "/control/receipt") {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				w.WriteHeader(http.StatusTooManyRequests)
			}))
			defer server.Close()
			r := pollingRunner(server, scenario != "expired-config-429")
			r.enabled, r.validUntil = true, time.Now().Add(-time.Second)
			r.deployment.grant = &api.NetworkProxyDeploymentRuntimeControl{ExpiresAt: time.Now().Add(-time.Second)}
			ctx, cancel := context.WithTimeout(context.Background(), 220*time.Millisecond)
			defer cancel()
			if err := r.Run(ctx); err != nil {
				t.Fatal(err)
			}
			// Three normal cycles fit; allow one scheduling boundary cycle.
			limit := int32(4)
			if strings.HasPrefix(scenario, "near-boundary") {
				limit = 8
			}
			if n := requests.Load(); n < 2 || n > limit {
				t.Fatalf("HTTP requests = %d, limit %d", n, limit)
			}
			t.Logf("HTTP requests=%d over 220ms, poll=80ms, bound=%d", requests.Load(), limit)
			if r.enabled || !r.suspended {
				t.Fatal("expired lease retained serving state")
			}
		})
	}
}

func TestRunPollingExpiryClosesIngressWithoutExtraHTTP(t *testing.T) {
	for _, managed := range []bool{false, true} {
		t.Run(map[bool]string{false: "configuration", true: "control"}[managed], func(t *testing.T) {
			var requests atomic.Int32
			first := make(chan struct{}, 1)
			boundary := time.Now().Add(70 * time.Millisecond)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				requests.Add(1)
				if strings.HasSuffix(req.URL.Path, "/deployment/control") {
					g := confirmationGrant()
					g.StopRequired, g.AcceptingRequests, g.ExpiresAt = false, true, boundary
					_ = json.NewEncoder(w).Encode(g)
					return
				}
				select {
				case first <- struct{}{}:
				default:
				}
				w.WriteHeader(http.StatusTooManyRequests)
			}))
			defer server.Close()
			r := pollingRunner(server, managed)
			r.config.PollInterval = time.Second
			r.enabled, r.validUntil = true, boundary
			if managed {
				r.validUntil = boundary.Add(time.Minute)
				r.deployment.grant = &api.NetworkProxyDeploymentRuntimeControl{ControlRevision: 1, ExpiresAt: boundary, AcceptingRequests: true}
			}
			p := &publicationIngress{accepting: true}
			r.ingress = p
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- r.Run(ctx) }()
			defer func() { cancel(); <-done }()
			select {
			case <-first:
			case <-time.After(time.Second):
				t.Fatal("missing initial poll")
			}
			deadline := boundary.Add(300 * time.Millisecond)
			for {
				p.mu.Lock()
				closed := p.closed
				p.mu.Unlock()
				if closed {
					if time.Now().Before(boundary) {
						t.Fatal("ingress closed before authorized boundary")
					}
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("expiry did not close ingress before next poll")
				}
				time.Sleep(time.Millisecond)
			}
			want := int32(1)
			if managed {
				want = 2
			}
			if n := requests.Load(); n != want {
				t.Fatalf("expiry triggered extra HTTP: %d, want %d", n, want)
			}
		})
	}
}

func TestRunPollingRecoversExpiredControlAndCancelsWaiting(t *testing.T) {
	var reads atomic.Int32
	proof := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasSuffix(req.URL.Path, "/deployment/control") {
			if reads.Add(1) == 1 {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			_ = json.NewEncoder(w).Encode(confirmationGrant())
			return
		}
		if strings.HasSuffix(req.URL.Path, "/control/receipt") {
			proof <- struct{}{}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		t.Errorf("stop recovery made business request %s", req.URL.Path)
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	r := pollingRunner(server, true)
	r.deployment.grant = &api.NetworkProxyDeploymentRuntimeControl{ExpiresAt: time.Now().Add(-time.Second)}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	select {
	case <-proof:
	case <-time.After(2 * time.Second):
		cancel()
		<-done
		t.Fatal("new stop revision was not recovered")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("cancellation waited for poll timer")
	}
	if !r.deployment.drained || r.enabled {
		t.Fatal("stop recovery did not remain fail closed")
	}
}

func TestPollingCapturedDrainDeadlineCannotBeExtended(t *testing.T) {
	now := time.Now()
	r := &Runner{config: Config{ManagedDeployment: true, PollInterval: time.Second}, enabled: true, validUntil: now.Add(time.Minute), engine: newEngine(Config{Engine: domain.EngineMihomo}, ""), deployment: deploymentState{grant: &api.NetworkProxyDeploymentRuntimeControl{ExpiresAt: now.Add(time.Minute)}, drainLease: now}}
	ctx, cancel := r.cycleContext(context.Background())
	defer cancel()
	if ctx.Err() == nil {
		t.Fatal("cycle can continue past captured drain deadline")
	}
	r.expire(now)
	if r.enabled || !r.suspended {
		t.Fatal("draining continued past hard expiry")
	}
	if got := r.nextCycleDelay(now); got != time.Second {
		t.Fatalf("expired drain retries in %v", got)
	}
}

func TestRunPollingCancelsInFlightRequest(t *testing.T) {
	started := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		started <- struct{}{}
		<-req.Context().Done()
	}))
	defer server.Close()
	r := pollingRunner(server, true)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- r.Run(ctx) }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("request did not start")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("HTTP request ignored cancellation")
	}
}

// The guard owns a real CONNECT stream with a deliberately longer data lease.
// Only Runner's current authorization boundary can close it in this test.
func pollingOwnedStream(t *testing.T) (*egressGuard, net.Conn) {
	t.Helper()
	echo, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = echo.Close() })
	go func() {
		conn, err := echo.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(conn, conn)
	}()
	g, err := newEgressGuard(nil, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(g.close)
	secret := strings.Repeat("s", 32)
	g.handler, err = proxytransport.New(map[string]string{"user": secret}, func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "tcp", echo.Addr().String())
	}, time.Now().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	g.server.Handler = g.handler
	go func() { _ = g.server.Serve(g.listener) }()
	conn, err := net.DialTimeout("tcp", g.listener.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(time.Second))
	_, err = fmt.Fprintf(conn, "CONNECT 1.1.1.1:443 HTTP/1.1\r\nHost: 1.1.1.1:443\r\nProxy-Authorization: Basic %s\r\n\r\n", base64.StdEncoding.EncodeToString([]byte("user:"+secret)))
	if err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodConnect})
	if err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("CONNECT: %v %v", response, err)
	}
	if _, err := conn.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	one := make([]byte, 1)
	if _, err := io.ReadFull(reader, one); err != nil || string(one) != "x" {
		t.Fatalf("echo: %q %v", one, err)
	}
	return g, &bufferedFixtureConn{Conn: conn, reader: reader}
}

func TestRunPollingBlockedControlClosesOwnedFlowAtCurrentExpiry(t *testing.T) {
	for _, boundaryKind := range []string{"control", "configuration", "drain"} {
		for _, stopRequired := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stop=%v", boundaryKind, stopRequired), func(t *testing.T) {
				g, stream := pollingOwnedStream(t)
				var requests, controls atomic.Int32
				first := make(chan struct{}, 1)
				recovered := make(chan struct{}, 1)
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
					requests.Add(1)
					if strings.HasSuffix(req.URL.Path, "/deployment/control") {
						if controls.Add(1) == 1 {
							first <- struct{}{}
							<-req.Context().Done()
							return
						}
						grant := confirmationGrant()
						grant.StopRequired, grant.AcceptingRequests = stopRequired, !stopRequired
						_ = json.NewEncoder(w).Encode(grant)
						return
					}
					// A disabled desired configuration does not restart the closed engine.
					w.WriteHeader(http.StatusNoContent)
					select {
					case recovered <- struct{}{}:
					default:
					}
				}))
				defer server.Close()
				r := pollingRunner(server, true)
				r.config.PollInterval = 250 * time.Millisecond
				r.enabled, r.engine.egress = true, g
				r.validUntil = time.Now().Add(time.Minute)
				boundary := time.Now().Add(70 * time.Millisecond)
				r.deployment.grant = &api.NetworkProxyDeploymentRuntimeControl{ControlRevision: 1, ExpiresAt: boundary.Add(time.Minute), AcceptingRequests: true}
				switch boundaryKind {
				case "control":
					r.deployment.grant.ExpiresAt = boundary
				case "configuration":
					r.validUntil = boundary
				case "drain":
					r.deployment.drainLease = boundary
				}
				ctx, cancel := context.WithCancel(context.Background())
				done := make(chan error, 1)
				go func() { done <- r.Run(ctx) }()
				finished := false
				defer func() {
					cancel()
					if !finished {
						<-done
					}
				}()
				select {
				case <-first:
				case <-time.After(time.Second):
					t.Fatal("initial control request missing")
				}
				_ = stream.SetReadDeadline(boundary.Add(150 * time.Millisecond))
				_, err := stream.Read(make([]byte, 1))
				if err == nil {
					t.Fatal("flow survived hard expiry")
				}
				if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
					t.Fatal("blocked control GET delayed flow closure")
				}
				if time.Now().Before(boundary) {
					t.Fatal("flow closed before authorized boundary")
				}
				if g.accepting.Load() {
					t.Fatal("expired guard still accepts requests")
				}
				conn, err := net.DialTimeout("tcp", g.listener.Addr().String(), 20*time.Millisecond)
				if err == nil {
					conn.Close()
					t.Fatal("expired guard permits reconnect")
				}
				if n := requests.Load(); n != 1 {
					t.Fatalf("expiry caused extra HTTP: %d", n)
				}
				select {
				case <-recovered:
				case <-time.After(time.Second):
					t.Fatal("old deadline prevented fresh control recovery")
				}
				// State is read only after Runner has stopped.
				cancel()
				err = <-done
				finished = true
				if err != nil || r.deployment.floor != 2 || r.enabled {
					t.Fatalf("recovery state: err=%v revision=%d enabled=%v", err, r.deployment.floor, r.enabled)
				}
				if stopRequired && !r.deployment.drained {
					t.Fatal("fresh stop grant failed to confirm owned stop")
				}
			})
		}
	}
}
