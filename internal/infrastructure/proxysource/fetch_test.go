package proxysource

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"
)

func TestFetchRechecksProtectedAddressAfterPolicyChange(t *testing.T) {
	var protected atomic.Bool
	var requests atomic.Int32
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = fmt.Fprint(w, "source")
	}))
	defer server.Close()
	options := Options{
		Protected:     func(context.Context, netip.Addr) bool { return protected.Load() },
		ProtectedHost: func(context.Context, string) bool { return false },
	}
	fetcher, err := NewFetcher(options)
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := fetcher.client.Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected HTTP transport")
	}
	defer transport.CloseIdleConnections()
	trusted, ok := server.Client().Transport.(*http.Transport)
	if !ok {
		t.Fatal("expected trusted test transport")
	}
	transport.TLSClientConfig = trusted.TLSClientConfig.Clone()
	var dials atomic.Int32
	// Exercise connection reuse with a public peer policy and a local TLS fixture.
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		dials.Add(1)
		if !allowedAddress(ctx, netip.MustParseAddr("8.8.8.8"), options) {
			return nil, fmt.Errorf("protected peer")
		}
		var dialer net.Dialer
		return dialer.DialContext(ctx, network, address)
	}
	if source, fetchErr := fetcher.Fetch(t.Context(), server.URL); fetchErr != nil || source != "source" {
		t.Fatalf("first fetch = %q, %v", source, fetchErr)
	}
	protected.Store(true)
	if _, fetchErr := fetcher.Fetch(t.Context(), server.URL); fetchErr == nil {
		t.Fatal("fetch succeeded after its peer became protected")
	}
	if requests.Load() != 1 || dials.Load() != 2 {
		t.Fatalf("requests = %d, dials = %d; want 1, 2", requests.Load(), dials.Load())
	}
}
