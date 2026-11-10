package aigatewayrelay

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/application/aigateway/relayport"
)

func TestRelayHTTPRedirectDoesNotReplayProviderKeyOrBody(t *testing.T) {
	for _, status := range []int{http.StatusTemporaryRedirect, http.StatusPermanentRedirect} {
		for _, crossHost := range []bool{false, true} {
			t.Run(fmt.Sprintf("%d/crossHost=%v", status, crossHost), func(t *testing.T) {
				var firstHits, targetHits atomic.Int32
				adapter := New(Config{})
				server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/redirect-target" {
						targetHits.Add(1)
						t.Errorf("received redirected request with provider key %v", r.Header.Get("x-goog-api-key") != "")
						return
					}
					firstHits.Add(1)
					body, _ := io.ReadAll(r.Body)
					if r.Method != http.MethodPost || r.Header.Get("x-goog-api-key") != "synthetic-key" || !strings.Contains(string(body), "synthetic-input") {
						t.Error("first hop did not receive the original Gemini request")
					}
					if r.TLS.ServerName != "first.example.com" || !strings.HasPrefix(r.Host, "first.example.com:") {
						t.Errorf("TLS server name = %q, HTTP host = %q", r.TLS.ServerName, r.Host)
					}
					location := "/redirect-target"
					if crossHost {
						location = "https://private.example.com" + location
					}
					http.Redirect(w, r, location, status)
				}))
				defer server.Close()
				trustRelayTestServer(t, adapter, server)
				var resolves, dials atomic.Int32
				target := namedRelayTestTarget(t, server, adapter, "gemini", &resolves, &dials)
				result, err := executeRelayTestRequest(context.Background(), adapter, target, relayport.HTTPOptions{
					Timeout: time.Second, StartStream: func(*http.Response) io.Writer {
						t.Error("redirect committed downstream stream")
						return io.Discard
					},
				})
				if err != nil || result.Failure != nil || result.Response == nil || result.Response.StatusCode != status || result.Wrote {
					t.Fatalf("execute result = %+v, err = %v", result, err)
				}
				if firstHits.Load() != 1 || targetHits.Load() != 0 || resolves.Load() != 1 || dials.Load() != 1 {
					t.Fatalf("first/target hits %d/%d, resolver/dial calls %d/%d", firstHits.Load(), targetHits.Load(), resolves.Load(), dials.Load())
				}
			})
		}
	}
}

func TestRelayProbeAndWebSocketRejectRedirect(t *testing.T) {
	var hits, targetHits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect-target" {
			targetHits.Add(1)
			return
		}
		hits.Add(1)
		if strings.Contains(r.Host, "unapproved.example.com") {
			t.Error("default header replaced the verified upstream host")
		}
		if r.Header.Get("Authorization") != "Bearer synthetic-key" || r.Method != http.MethodGet {
			t.Error("first hop did not receive authenticated GET")
		}
		http.Redirect(w, r, "/redirect-target", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	target := relayport.Target{
		ProviderKind: "openai", BaseURL: server.URL,
		DefaultHeaders: map[string]any{"Host": "unapproved.example.com"},
	}
	adapter := New(Config{AllowInsecureUpstreamHTTP: true, AllowPrivateUpstreamHosts: true})
	probe, err := adapter.Probe(context.Background(), target, "synthetic-key", time.Second)
	if err != nil || probe.Status != "failure" || probe.HTTPStatus != http.StatusTemporaryRedirect || probe.ModelCount != 0 {
		t.Fatalf("probe = %+v, err = %v", probe, err)
	}
	ws, err := adapter.WebSocket(context.Background(), relayport.Request{}, target, relayport.WebSocketOptions{
		APIKey: "synthetic-key", DialTimeout: time.Second, Upgrade: unexpectedUpgrade(t),
	})
	if err != nil || ws.DialError == nil || ws.HTTPStatus != http.StatusTemporaryRedirect {
		t.Fatalf("websocket = %+v, err = %v", ws, err)
	}
	if hits.Load() != 2 || targetHits.Load() != 0 {
		t.Fatalf("first hop / target hits = %d / %d", hits.Load(), targetHits.Load())
	}
}

func TestRelayAllDialPathsRejectPrivateDNSAnswers(t *testing.T) {
	for _, operation := range []string{"http", "probe", "websocket"} {
		t.Run(operation, func(t *testing.T) {
			adapter := New(Config{})
			var resolves, dials atomic.Int32
			adapter.resolve = func(context.Context, string, string) ([]netip.Addr, error) {
				resolves.Add(1)
				return []netip.Addr{netip.MustParseAddr("93.184.216.34"), netip.MustParseAddr("::ffff:169.254.169.254")}, nil
			}
			adapter.dial = func(context.Context, string, string) (net.Conn, error) {
				dials.Add(1)
				return nil, fmt.Errorf("unexpected dial")
			}
			ctx := context.Background()
			target := relayport.Target{BaseURL: "https://provider.example.com", ProviderKind: "openai"}
			switch operation {
			case "http":
				result, err := executeRelayTestRequest(ctx, adapter, target, relayport.HTTPOptions{Timeout: time.Second})
				if err != nil || result.Failure == nil || !strings.Contains(result.Failure.Message, "disallowed address") {
					t.Fatalf("execute result = %+v, err = %v", result, err)
				}
			case "probe":
				if _, err := adapter.Probe(ctx, target, "synthetic-key", time.Second); err == nil {
					t.Fatal("probe accepted a private DNS answer")
				}
			case "websocket":
				result, err := adapter.WebSocket(ctx, relayport.Request{}, target, relayport.WebSocketOptions{
					APIKey: "synthetic-key", DialTimeout: time.Second, Upgrade: unexpectedUpgrade(t),
				})
				if err != nil || result.DialError == nil || !strings.Contains(result.DialError.Error(), "disallowed address") {
					t.Fatalf("websocket result = %+v, err = %v", result, err)
				}
			}
			if resolves.Load() != 1 || dials.Load() != 0 {
				t.Fatalf("resolver/dial calls = %d/%d", resolves.Load(), dials.Load())
			}
		})
	}
}

func unexpectedUpgrade(t *testing.T) relayport.UpgradeWebSocket {
	t.Helper()
	return func(http.Header) (relayport.WebSocketConn, error) {
		t.Error("unexpected downstream upgrade")
		return nil, fmt.Errorf("unexpected downstream upgrade")
	}
}

func trustRelayTestServer(t *testing.T, adapter *Adapter, server *httptest.Server) {
	t.Helper()
	roots := x509.NewCertPool()
	roots.AddCert(server.Certificate())
	transport, ok := adapter.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected relay transport type %T", adapter.client.Transport)
	}
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
}

func namedRelayTestTarget(t *testing.T, server *httptest.Server, adapter *Adapter, provider string, resolves, dials *atomic.Int32) relayport.Target {
	t.Helper()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	adapter.resolve = func(_ context.Context, _, host string) ([]netip.Addr, error) {
		if resolves.Add(1) == 1 && host == "first.example.com" {
			return []netip.Addr{netip.MustParseAddr("93.184.216.34")}, nil
		}
		return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
	}
	adapter.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		dials.Add(1)
		if address != net.JoinHostPort("93.184.216.34", u.Port()) {
			return nil, fmt.Errorf("dial was not pinned to approved address %s", address)
		}
		// Map the approved fake IP to the test listener without DNS or external network.
		return (&net.Dialer{}).DialContext(ctx, network, u.Host)
	}
	return relayport.Target{BaseURL: "https://" + net.JoinHostPort("first.example.com", u.Port()), ProviderKind: provider, Model: "upstream-model"}
}

func executeRelayTestRequest(ctx context.Context, adapter *Adapter, target relayport.Target, opts relayport.HTTPOptions) (relayport.HTTPResult, error) {
	endpoint := "chat/completions"
	if target.ProviderKind == "gemini" {
		endpoint = "generateContent"
	}
	prepared, err := adapter.Prepare(relayport.Request{
		ProviderKind: target.ProviderKind, Endpoint: endpoint, Body: []byte(`{"model":"public-model","input":"synthetic-input"}`),
	}, target, relayport.RequestOptions{})
	if err != nil {
		return relayport.HTTPResult{}, err
	}
	opts.APIKey = "synthetic-key"
	return adapter.Execute(ctx, prepared, opts)
}

func TestRelayWebSocketHandshakeHonorsDialDeadline(t *testing.T) {
	closed := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			close(closed)
		case <-release:
		}
	}))
	defer server.Close()
	defer close(release)
	adapter := New(Config{AllowInsecureUpstreamHTTP: true, AllowPrivateUpstreamHosts: true})
	target := relayport.Target{BaseURL: server.URL, ProviderKind: "openai"}
	started := time.Now()
	result, err := adapter.WebSocket(context.Background(), relayport.Request{}, target, relayport.WebSocketOptions{
		APIKey: "synthetic-key", DialTimeout: 100 * time.Millisecond, Upgrade: unexpectedUpgrade(t),
	})
	if err != nil || result.DialError == nil || result.HTTPStatus != 0 {
		t.Fatalf("websocket result = %+v, err = %v", result, err)
	}
	if elapsed := time.Since(started); elapsed >= time.Second {
		t.Fatalf("handshake timeout elapsed = %v", elapsed)
	}
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("upstream did not observe websocket handshake timeout")
	}
}
