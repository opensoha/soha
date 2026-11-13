package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	"go.uber.org/zap"
)

func TestDockerRuntimeLogArgsIncludeTimestampAndSince(t *testing.T) {
	args := dockerRuntimeLogArgs(dockerRuntimeWorkspace{ProjectName: "demo"}, dockerRuntimeRequest{ServiceName: "api", TailLines: 50, SinceSeconds: 90}, true)
	joined := strings.Join(args, " ")
	for _, expected := range []string{"--timestamps", "--no-color", "--tail 50", "--follow", "--since 90s", "api"} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("args = %q, missing %q", joined, expected)
		}
	}
}

func TestDockerRuntimeRoutesRequireConfiguredToken(t *testing.T) {
	router := dockerRuntimeTestRouter(cfgpkg.Config{
		HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"},
	})

	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/v1/docker/runtime/logs", bytes.NewBufferString(`{}`)))

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestDockerRuntimeRoutesRejectInvalidToken(t *testing.T) {
	router := dockerRuntimeTestRouter(cfgpkg.Config{
		HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"},
		Auth: cfgpkg.AuthConfig{BearerToken: "agent-token"},
	})

	req := httptest.NewRequest(http.MethodPost, "/api/v1/docker/runtime/logs", bytes.NewBufferString(`{}`))
	req.Header.Set("Authorization", "Bearer wrong-token")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, req)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestDockerRuntimeRoutesAcceptAgentOrControlPlaneToken(t *testing.T) {
	router := dockerRuntimeTestRouter(cfgpkg.Config{
		HTTP:         cfgpkg.HTTPConfig{BasePath: "/api/v1"},
		Auth:         cfgpkg.AuthConfig{BearerToken: "agent-token"},
		ControlPlane: cfgpkg.ControlPlaneConfig{BearerToken: "control-token"},
	})

	for _, token := range []string{"agent-token", "control-token"} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/docker/runtime/logs", bytes.NewBufferString(`{}`))
		req.Header.Set("Authorization", "Bearer "+token)
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, req)

		if recorder.Code == http.StatusUnauthorized {
			t.Fatalf("token %q was rejected with status %d", token, recorder.Code)
		}
	}
}

func TestDockerRuntimeTerminalRejectsUntrustedOrigin(t *testing.T) {
	router := dockerRuntimeTestRouter(cfgpkg.Config{
		HTTP:     cfgpkg.HTTPConfig{BasePath: "/api/v1"},
		Auth:     cfgpkg.AuthConfig{BearerToken: "agent-token"},
		Security: cfgpkg.SecurityConfig{AllowedActions: []string{actionDockerRuntimeTerminal}},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	headers := http.Header{}
	headers.Set("Authorization", "Bearer agent-token")
	headers.Set("Origin", "https://evil.example")
	conn, resp, err := websocket.DefaultDialer.Dial(strings.Replace(server.URL, "http://", "ws://", 1)+"/api/v1/docker/runtime/terminal", headers)
	if conn != nil {
		conn.Close()
	}
	if err == nil {
		t.Fatal("Dial() succeeded, want origin rejection")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %v, want %d", responseStatus(resp), http.StatusForbidden)
	}
}

func TestDockerRuntimeTerminalAcceptsConfiguredOrigin(t *testing.T) {
	router := dockerRuntimeTestRouter(cfgpkg.Config{
		HTTP: cfgpkg.HTTPConfig{
			BasePath:       "/api/v1",
			AllowedOrigins: []string{"https://console.example"},
		},
		Auth:     cfgpkg.AuthConfig{BearerToken: "agent-token"},
		Security: cfgpkg.SecurityConfig{AllowedActions: []string{actionDockerRuntimeTerminal}},
	})
	server := httptest.NewServer(router)
	defer server.Close()

	headers := http.Header{}
	headers.Set("Authorization", "Bearer agent-token")
	headers.Set("Origin", "https://console.example")
	conn, resp, err := websocket.DefaultDialer.Dial(strings.Replace(server.URL, "http://", "ws://", 1)+"/api/v1/docker/runtime/terminal", headers)
	if resp != nil && resp.Body != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		t.Fatalf("Dial() error = %v status=%v", err, responseStatus(resp))
	}
	conn.Close()
}

func dockerRuntimeTestRouter(cfg cfgpkg.Config) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerDockerRuntimeRoutes(router, cfg, zap.NewNop(), newActionPolicy(cfg.Security, zap.NewNop(), nil))
	return router
}

func responseStatus(resp *http.Response) int {
	if resp == nil {
		return 0
	}
	return resp.StatusCode
}
