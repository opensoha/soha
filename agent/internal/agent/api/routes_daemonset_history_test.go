package api

import (
	cfgpkg "github.com/opensoha/soha/agent/internal/agent/config"
	k8sagent "github.com/opensoha/soha/agent/internal/agent/kubernetes"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDaemonSetHistoryRequiresAgentAuthentication(t *testing.T) {
	server := New(cfgpkg.Config{HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"}, Auth: cfgpkg.AuthConfig{BearerToken: "history-test-token-which-is-never-a-real-credential"}}, nil, &k8sagent.Client{}, &fakeRuntimeController{})
	for _, prefix := range []string{"/api/v1/platform", "/api/v1/platform/ownership-v1", "/api/v1/platform/ownership-v2"} {
		for _, suffix := range []string{"/workloads/daemonsets/db/rollouts?namespace=team", "/workloads/daemonsets/db/rollouts/db-1/yaml?namespace=team"} {
			path := prefix + suffix
			response := httptest.NewRecorder()
			server.httpServer.Handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			if response.Code != http.StatusUnauthorized {
				t.Fatalf("unauthenticated history route %s returned %d", path, response.Code)
			}
		}
	}
}
