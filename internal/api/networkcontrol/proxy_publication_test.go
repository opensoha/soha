package networkcontrol

import (
	"context"
	"github.com/gin-gonic/gin"
	api "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/networkidentity"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type publicationStub struct{ called bool }

func (s *publicationStub) ServerReady(context.Context, networkidentity.Identity, string, api.NetworkProxyBundleReadback) error {
	s.called = true
	return nil
}
func (s *publicationStub) IngressCheck(context.Context, networkidentity.Identity, string, api.NetworkProxyIngressCheckInput) (api.NetworkProxyIngressCheckResult, error) {
	s.called = true
	return api.NetworkProxyIngressCheckResult{Allowed: false, ReasonCode: "access_denied"}, nil
}
func (s *publicationStub) Projection(context.Context, networkidentity.Identity, string, string, string) (api.NetworkProxyEmployeeProjection, error) {
	s.called = true
	return api.NetworkProxyEmployeeProjection{}, nil
}
func (s *publicationStub) ProjectionApplied(context.Context, networkidentity.Identity, string, api.NetworkProxyProjectionApplied) error {
	s.called = true
	return nil
}
func TestPublicationRequiresRealMTLSAndRejectsTrailingJSON(t *testing.T) {
	for _, test := range []struct {
		name, body, kind, id string
		authenticated        bool
	}{
		{name: "no_mtls", body: `{}`, kind: "proxy", id: "proxy-1"},
		{name: "path_identity", body: `{}`, kind: "proxy", id: "another-proxy", authenticated: true},
		{name: "endpoint_is_not_proxy", body: `{}`, kind: "endpoint", id: "proxy-1", authenticated: true},
		{name: "unknown_field", body: `{"credentialSecret":"synthetic-secret","raw":"forbidden"}`, kind: "proxy", id: "proxy-1", authenticated: true},
		{name: "trailing_json", body: `{} {"credentialSecret":"synthetic-secret"}`, kind: "proxy", id: "proxy-1", authenticated: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			s := &publicationStub{}
			h, err := NewPublicationHandler(s, Options{MaxBodyBytes: 1 << 20, RequestsPerMinute: 100})
			if err != nil {
				t.Fatal(err)
			}
			router := gin.New()
			router.POST("/api/network-control/v1/proxies/:runtimeID/ingress/check", h.IngressCheck)
			path := "/api/network-control/v1/proxies/proxy-1/ingress/check"
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(test.body))
			request.Header.Set("Content-Type", "application/json")
			if test.authenticated {
				request = authenticatedRuntimeRequest(t, http.MethodPost, path, test.body, test.kind, test.id)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			if response.Code < 400 || s.called {
				t.Fatalf("invalid request passed: %d", response.Code)
			}
			if strings.Contains(response.Body.String(), "synthetic-secret") {
				t.Fatal("secret in error")
			}
		})
	}
}
func TestIngressDecisionIsNoStoreAndDoesNotEchoSecret(t *testing.T) {
	s := &publicationStub{}
	h, _ := NewPublicationHandler(s, Options{MaxBodyBytes: 1 << 20, RequestsPerMinute: 100})
	router := gin.New()
	router.POST("/api/network-control/v1/proxies/:runtimeID/ingress/check", h.IngressCheck)
	path := "/api/network-control/v1/proxies/proxy-1/ingress/check"
	request := authenticatedRuntimeRequest(t, http.MethodPost, path, `{"credentialSecret":"synthetic-secret"}`, "proxy", "proxy-1")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 200 || !s.called || response.Header().Get("Cache-Control") != "private, no-store" || strings.Contains(response.Body.String(), "synthetic-secret") {
		t.Fatal("unsafe response", response.Code)
	}
}

func (s *publicationStub) DirectProjection(context.Context, networkidentity.Identity, string, string) (api.NetworkProxyDirectProjection, error) {
	s.called = true
	return api.NetworkProxyDirectProjection{}, nil
}
