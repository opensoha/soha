package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	contractresource "github.com/opensoha/soha-contracts/resource"
)

type presenceRouteSpy struct{ conditionalResourceSpy }

func (s *presenceRouteSpy) ObserveResourcePresence(_ context.Context, namespace, kind, name string) (contractresource.ResourcePresence, error) {
	s.calls++
	return contractresource.ResourcePresence{Protocol: contractresource.ResourcePresenceProtocol, APIVersion: "v1", Resource: "configmaps", Kind: kind, Namespace: namespace, Name: name, CollectionResourceVersion: "opaque-collection", State: "absent"}, nil
}

func TestPresenceRouteAuthenticatesAndReturnsOnlyVersionedObservation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := New(cfgpkg.Config{HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"}, Auth: cfgpkg.AuthConfig{BearerToken: "test-only-agent-token"}}, nil, &k8sagent.Client{}, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/platform/resource-presence-v1/resources?namespace=team-a&kind=ConfigMap&name=app", nil)
	response := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatal("presence bypassed Agent authentication")
	}
	spy := &presenceRouteSpy{}
	router := gin.New()
	registerResourcePresenceRoute(router.Group("/api/v1/platform"), spy)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	var output struct {
		Data contractresource.ResourcePresence `json:"data"`
	}
	if response.Code != 200 || response.Header().Get("Cache-Control") != "no-store" || json.Unmarshal(response.Body.Bytes(), &output) != nil || output.Data.ValidateFor("ConfigMap", "team-a", "app") != nil || spy.calls != 1 {
		t.Fatal("presence route lost its versioned metadata-only target")
	}
}
