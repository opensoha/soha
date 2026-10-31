package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	protocol "github.com/opensoha/soha-contracts/docker/runtime"
)

func TestVersionedDockerReadsRequireAllowlistAndExactResponseIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	binding, _ := protocol.NewProjectBinding("11111111-1111-4111-8111-111111111111", "22222222-2222-4222-8222-222222222222")
	req := protocol.ReadRequest{Protocol: protocol.ReadProtocol, Action: "logs", ProjectID: "project", Container: protocol.ContainerBinding{ProjectBinding: binding, ContainerID: strings.Repeat("a", 64), ServiceName: "web"}}
	raw, _ := json.Marshal(req)
	for _, tc := range []struct {
		name   string
		allow  bool
		mutate bool
		body   string
		status int
		calls  int
	}{
		{"deny", false, false, string(raw), http.StatusForbidden, 0},
		{"valid", true, false, string(raw), http.StatusOK, 1},
		{"replacement", true, true, string(raw), http.StatusBadGateway, 1},
		{"extra request", true, false, string(raw) + " {}", http.StatusBadRequest, 0},
		{"unknown field", true, false, strings.TrimSuffix(string(raw), "}") + `,"composeContent":"private"}`, http.StatusBadRequest, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			cfg := cfgpkg.SecurityConfig{}
			if tc.allow {
				cfg.AllowedActions = []string{actionDockerVersionedLogs}
			}
			policy := newActionPolicy(cfg, nil, nil)
			router := gin.New()
			router.POST("/read", policy.Require(actionDockerVersionedLogs), dockerVersionedRead("logs", func(_ context.Context, r protocol.ReadRequest) (protocol.ReadResult, error) {
				calls++
				result := protocol.ReadResult{Protocol: r.Protocol, Action: r.Action, ProjectID: r.ProjectID, Container: r.Container, Encoding: "utf-8", Complete: true, Volumes: []protocol.Volume{}, Files: []protocol.FileEntry{}}
				if tc.mutate {
					result.Container.ContainerID = strings.Repeat("b", 64)
				}
				return result, nil
			}))
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/read", strings.NewReader(tc.body)))
			if response.Code != tc.status || calls != tc.calls {
				t.Fatalf("status=%d calls=%d, want %d/%d", response.Code, calls, tc.status, tc.calls)
			}
		})
	}
}

func TestDockerReadActionsSeparateVersionedAndLegacyRoutes(t *testing.T) {
	for route, actions := range map[string]struct{ required, otherProtocol string }{
		"logs":          {actionDockerRuntimeLogs, actionDockerVersionedLogs},
		"logs/stream":   {actionDockerRuntimeLogs, actionDockerVersionedLogs},
		"v1/logs":       {actionDockerVersionedLogs, actionDockerRuntimeLogs},
		"volumes":       {actionDockerRuntimeFiles, actionDockerVersionedFiles},
		"volume-files":  {actionDockerRuntimeFiles, actionDockerVersionedFiles},
		"volume-file":   {actionDockerRuntimeFiles, actionDockerVersionedFiles},
		"v1/volumes":    {actionDockerVersionedFiles, actionDockerRuntimeFiles},
		"v1/files/list": {actionDockerVersionedFiles, actionDockerRuntimeFiles},
		"v1/files/read": {actionDockerVersionedFiles, actionDockerRuntimeFiles},
	} {
		t.Run(route, func(t *testing.T) {
			for _, allowed := range []string{"", actions.required, actions.otherProtocol} {
				cfg := cfgpkg.Config{HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"}, Auth: cfgpkg.AuthConfig{BearerToken: "fixture-agent-token"}}
				if allowed != "" {
					cfg.Security.AllowedActions = []string{allowed}
				}
				router := dockerRuntimeTestRouter(cfg)
				request := httptest.NewRequest(http.MethodPost, "/api/v1/docker/runtime/"+route, strings.NewReader(`{}`))
				request.Header.Set("Authorization", "Bearer fixture-agent-token")
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				want := http.StatusForbidden
				if allowed == actions.required {
					want = http.StatusBadRequest
				}
				if response.Code != want {
					t.Fatalf("allow=%q status=%d want=%d", allowed, response.Code, want)
				}
			}
		})
	}
}
