package networkgateway

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/networkprobe"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/keyring"
)

func TestVPNProbeRequiresBoundIdentityAndFreshConfiguration(t *testing.T) {
	now := time.Now().UTC()
	key, err := keyring.NewKey("test", strings.Repeat("a", 64), now, nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := keyring.New(key, nil)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := networkprobe.NewSigner(keys)
	if err != nil {
		t.Fatal(err)
	}
	token, err := signer.Sign(networkprobe.Claims{Version: 1, GatewayRuntimeID: "gateway-1", EndpointRuntimeID: "endpoint-1", IntentID: "intent-1", IssuedAt: now.Unix(), ExpiresAt: now.Add(time.Minute).Unix()})
	if err != nil {
		t.Fatal(err)
	}
	r := &Runtime{runtimeID: "gateway-1", now: func() time.Time { return now }, validUntil: now.Add(time.Minute), probe: &networkprotocol.VPNProbeConfiguration{GatewayID: "gw-1", VerificationKey: signer.PublicKey()}}
	handler := r.ProbeHandler()
	for _, tt := range []struct {
		name, identity, token, target string
		expired                       bool
		status                        int
	}{
		{"accepted", "endpoint-1", token, "/vpn/probe", false, http.StatusNoContent},
		{"other endpoint", "endpoint-2", token, "/vpn/probe", false, http.StatusForbidden},
		{"missing certificate", "", token, "/vpn/probe", false, http.StatusUnauthorized},
		{"missing token", "endpoint-1", "", "/vpn/probe", false, http.StatusForbidden},
		{"expired configuration", "endpoint-1", token, "/vpn/probe", true, http.StatusServiceUnavailable},
		{"arbitrary target", "endpoint-1", token, "/vpn/probe?url=other", false, http.StatusBadRequest},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r.disabled = tt.expired
			request := httptest.NewRequest(http.MethodGet, "https://gateway.example"+tt.target, nil)
			request.Header.Set("Authorization", "Bearer "+tt.token)
			if tt.identity != "" {
				uri, err := url.Parse("spiffe://opensoha.local/network-control/endpoint/" + tt.identity)
				if err != nil {
					t.Fatal(err)
				}
				request.TLS = &tls.ConnectionState{VerifiedChains: [][]*x509.Certificate{{{URIs: []*url.URL{uri}}}}}
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tt.status || response.Body.Len() != 0 {
				t.Fatalf("status/body=%d/%q, want %d/empty", response.Code, response.Body.String(), tt.status)
			}
		})
	}
}
