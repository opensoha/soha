package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	k8sagent "github.com/opensoha/soha-agent/internal/agent/kubernetes"
	"go.uber.org/zap"
)

func TestWebSocketOriginPolicy(t *testing.T) {
	policy := newWebSocketOriginPolicy(
		[]string{"https://console.example", "not-an-origin", "https://ignored.example/path"},
		"agent-token",
	)
	tests := []struct {
		name          string
		host          string
		origin        string
		authorization string
		want          bool
	}{
		{name: "configured origin", host: "agent.internal", origin: "https://console.example", want: true},
		{name: "same origin", host: "agent.example", origin: "https://agent.example", want: true},
		{name: "reverse proxy host with port", host: "agent.example:443", origin: "https://agent.example:443", want: true},
		{name: "local development origins", host: "127.0.0.1:18080", origin: "http://localhost:5173", want: true},
		{name: "IPv6 local development origins", host: "[::1]:18080", origin: "http://[::1]:5173", want: true},
		{
			name:          "missing origin with authenticated caller",
			host:          "agent.internal",
			authorization: "Bearer agent-token",
			want:          true,
		},
		{name: "missing origin without identity", host: "agent.internal"},
		{name: "missing origin with invalid token", host: "agent.internal", authorization: "Bearer wrong-token"},
		{name: "untrusted cross origin", host: "agent.internal", origin: "https://evil.example"},
		{name: "malformed origin", host: "agent.internal", origin: "://invalid"},
		{name: "origin with path", host: "agent.internal", origin: "https://console.example/path"},
		{name: "opaque origin", host: "agent.internal", origin: "null"},
		{name: "non HTTP origin", host: "agent.internal", origin: "file://console.example"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://"+tt.host+"/stream", nil)
			req.Host = tt.host
			if tt.origin != "" {
				req.Header.Set("Origin", tt.origin)
			}
			if tt.authorization != "" {
				req.Header.Set("Authorization", tt.authorization)
			}
			if got := policy.Check(req); got != tt.want {
				t.Fatalf("Check() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestWebSocketOriginPolicyRejectsMultipleOrigins(t *testing.T) {
	policy := newWebSocketOriginPolicy([]string{"https://console.example"}, "agent-token")
	req := httptest.NewRequest(http.MethodGet, "http://agent.internal/stream", nil)
	req.Header.Add("Origin", "https://console.example")
	req.Header.Add("Origin", "https://evil.example")
	if policy.Check(req) {
		t.Fatal("Check() accepted multiple Origin headers")
	}
}

func TestPodTerminalRejectsUntrustedOrigin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	platform := router.Group("/api/v1/platform")
	platform.Use(authMiddleware("agent-token"))
	actions := newActionPolicy(
		cfgSecurityWithActions(actionPlatformPodsExec),
		zap.NewNop(),
		nil,
	)
	registerPodTerminalRoutes(
		platform,
		&k8sagent.Client{},
		actions,
		newWebSocketOriginPolicy(nil, "agent-token"),
	)
	server := httptest.NewServer(router)
	defer server.Close()

	headers := http.Header{}
	headers.Set("Authorization", "Bearer agent-token")
	headers.Set("Origin", "https://evil.example")
	conn, resp, err := websocket.DefaultDialer.Dial(
		strings.Replace(server.URL, "http://", "ws://", 1)+"/api/v1/platform/workloads/pods/example/terminal",
		headers,
	)
	if conn != nil {
		_ = conn.Close()
	}
	if resp != nil {
		defer func() {
			_ = resp.Body.Close()
		}()
	}
	if err == nil {
		t.Fatal("Dial() succeeded, want origin rejection")
	}
	if resp == nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("status = %v, want %d", responseStatus(resp), http.StatusForbidden)
	}
}

func cfgSecurityWithActions(actions ...string) cfgpkg.SecurityConfig {
	return cfgpkg.SecurityConfig{AllowedActions: actions}
}
