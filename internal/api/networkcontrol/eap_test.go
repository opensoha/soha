package networkcontrol

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opensoha/soha/internal/networkprotocol"
)

func TestRouterKeepsEAPCertificateActionsBoundToRuntime(t *testing.T) {
	service := &controlServiceStub{ready: true}
	router, err := NewRouter(service, controlReadyStoreStub{}, Options{MaxBodyBytes: 1024, RequestsPerMinute: 100})
	if err != nil {
		t.Fatal(err)
	}
	for action, kind := range map[string]string{"eap:certificate": networkprotocol.MessageEAPCertificateResult, "eap:activate": networkprotocol.MessageEAPCertificateActivateResult} {
		request := authenticatedControlRequest(t, http.MethodPost, "/api/network-control/v1/runtimes/endpoint-1/"+action, `{"messageType":"test"}`)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), kind) || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("EAP route %s: %d %s", action, response.Code, response.Body.String())
		}
		request = authenticatedControlRequest(t, http.MethodPost, "/api/network-control/v1/runtimes/other-endpoint/"+action, `{}`)
		response = httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized {
			t.Fatalf("cross runtime %s returned %d", action, response.Code)
		}
	}
}
