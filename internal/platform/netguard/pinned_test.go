package netguard

import (
	"context"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"sync/atomic"
	"testing"
)

func TestAllowedSourceAddress(t *testing.T) {
	for _, tc := range []struct {
		ip, allow string
		want      bool
	}{
		{"8.8.8.8", "", true}, {"10.2.3.4", "", false}, {"10.2.3.4", "10.2.0.0/16", true},
		{"10.3.3.4", "10.2.0.0/16", false}, {"::ffff:10.2.3.4", "10.2.0.0/16", true},
		{"fd12::1", "fd12::/48", true}, {"127.0.0.1", "127.0.0.0/8", false}, {"127.0.0.1", "127.0.0.1/32", true},
		{"169.254.169.254", "0.0.0.0/0", false}, {"0.0.0.0", "0.0.0.0/0", false},
		{"224.1.2.3", "0.0.0.0/0", false}, {"192.0.2.1", "0.0.0.0/0", false}, {"fe80::1", "::/0", false},
	} {
		var allowed []netip.Prefix
		if tc.allow != "" {
			allowed = []netip.Prefix{netip.MustParsePrefix(tc.allow)}
		}
		if got := AllowedSourceAddress(netip.MustParseAddr(tc.ip), allowed); got != tc.want {
			t.Errorf("%s in %s: got %v", tc.ip, tc.allow, got)
		}
	}
}

func TestPinnedHTTPSClientRejectsRedirectAndUntrustedTLS(t *testing.T) {
	var redirected atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { redirected.Add(1) }))
	defer target.Close()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	allowed := []netip.Prefix{netip.MustParsePrefix("127.0.0.1/32")}
	certificate := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw}))
	if _, err := PinnedHTTPSClient(t.Context(), endpoint.Hostname(), endpoint.Port(), nil, certificate); err == nil {
		t.Fatal("private address accepted without a connection grant")
	}
	client, err := PinnedHTTPSClient(t.Context(), endpoint.Hostname(), endpoint.Port(), allowed, certificate)
	if err != nil {
		t.Fatal(err)
	}
	defer client.CloseIdleConnections()
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusTemporaryRedirect || redirected.Load() != 0 {
		t.Fatal("redirect was followed")
	}
	redirectedResponse, err := client.Get(target.URL)
	if err == nil {
		_ = redirectedResponse.Body.Close()
		t.Fatal("pinned client dialed another endpoint")
	}
	untrusted, err := PinnedHTTPSClient(t.Context(), endpoint.Hostname(), endpoint.Port(), allowed, "")
	if err != nil {
		t.Fatal(err)
	}
	defer untrusted.CloseIdleConnections()
	untrustedResponse, err := untrusted.Get(server.URL)
	if err == nil {
		_ = untrustedResponse.Body.Close()
		t.Fatal("untrusted TLS certificate accepted")
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := ResolveAllowedAddress(canceled, "invalid.example", allowed); err == nil {
		t.Fatal("canceled DNS resolution succeeded")
	}
}
