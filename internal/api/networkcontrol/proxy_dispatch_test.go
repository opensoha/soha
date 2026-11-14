package networkcontrol

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/opensoha/soha/internal/networkprotocol"
)

func TestProxyDispatchUsesDedicatedMTLSRoutes(t *testing.T) {
	schemas, err := networkprotocol.CompileSchemas()
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct{ method, path, kind string }{
		{http.MethodPost, "/api/network-control/v1/proxies/proxy-1/publication/ready", "proxy"},
		{http.MethodPost, "/api/network-control/v1/proxies/proxy-1/ingress/check", "proxy"},
		{http.MethodGet, "/api/network-control/v1/runtimes/endpoint-1/mihomo-profiles/profile-1/projection?processEpoch=boot-1", "endpoint"},
		{http.MethodGet, "/api/network-control/v1/runtimes/endpoint-1/mihomo-profiles/profile-1/direct-projection?processEpoch=boot-1", "endpoint"},
		{http.MethodPost, "/api/network-control/v1/runtimes/endpoint-1/mihomo-profiles/profile-1/projection/receipt", "endpoint"},
	} {
		t.Run(test.method+test.path, func(t *testing.T) {
			publication := &publicationStub{}
			router, err := NewRouter(&controlServiceStub{ready: true}, controlReadyStoreStub{}, Options{MaxBodyBytes: 1 << 20, RequestsPerMinute: 100, ProxySchemas: schemas, ProxyPublication: publication})
			if err != nil {
				t.Fatal(err)
			}
			id := "proxy-1"
			if test.kind == "endpoint" {
				id = "endpoint-1"
			}
			request := authenticatedRuntimeRequest(t, test.method, test.path, `{}`, test.kind, id)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusOK || !publication.called || response.Header().Get("Cache-Control") != "private, no-store" {
				t.Fatalf("route not wired: %d %s", response.Code, response.Body.String())
			}
			publication.called = false
			request = httptest.NewRequest(test.method, test.path, nil)
			response = httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized || publication.called {
				t.Fatalf("route allowed unauthenticated request: %d", response.Code)
			}
		})
	}
}
