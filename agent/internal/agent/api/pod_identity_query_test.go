package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	cfgpkg "github.com/opensoha/soha/agent/internal/agent/config"
	k8sagent "github.com/opensoha/soha/agent/internal/agent/kubernetes"
)

func TestPodExpectedUIDDoesNotDowngradeMalformedInput(t *testing.T) {
	for _, test := range []struct {
		query  string
		status int
		uid    string
	}{
		{"", 200, ""}, {"?expectedUid=original", 200, "original"},
		{"?expectedUid=", 400, ""}, {"?expectedUid=%20", 400, ""}, {"?expectedUid=%XX", 400, ""},
		{"?expectedUid=&expectedUid=old", 400, ""}, {"?expectedUid=old&expectedUid=other", 400, ""},
	} {
		t.Run(test.query, func(t *testing.T) {
			router := gin.New()
			router.GET("/session", func(c *gin.Context) {
				uid, valid := podExpectedUID(c, false)
				if !valid {
					return
				}
				c.String(http.StatusOK, uid)
			})
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/session"+test.query, nil))
			if response.Code != test.status || (test.status == 200 && response.Body.String() != test.uid) {
				t.Fatalf("response = %d %q", response.Code, response.Body.String())
			}
		})
	}
}

func TestUIDBoundPodRoutesRequireAuthenticationAndValidIdentity(t *testing.T) {
	server := New(cfgpkg.Config{
		HTTP:     cfgpkg.HTTPConfig{BasePath: "/api/v1"},
		Auth:     cfgpkg.AuthConfig{BearerToken: "agent-token"},
		Security: cfgpkg.SecurityConfig{AllowedActions: []string{actionPlatformPodsExec}},
	}, nil, &k8sagent.Client{}, nil)
	for _, suffix := range []string{"logs", "logs/stream", "terminal"} {
		t.Run(suffix, func(t *testing.T) {
			path := "/api/v1/platform/workloads/pods/api-0/uid-bound/" + suffix
			recorder := httptest.NewRecorder()
			server.httpServer.Handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path+"?expectedUid=uid-a", nil))
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("unauthenticated response = %d %s", recorder.Code, recorder.Body.String())
			}
			for _, query := range []string{"", "?expectedUid=", "?expectedUid=%20", "?expectedUid=uid-a%20", "?expectedUid=uid-a&expectedUid=uid-b", "?expectedUid=%XX", "?expectedUid=" + strings.Repeat("a", 129)} {
				req := httptest.NewRequest(http.MethodGet, path+query, nil)
				req.Header.Set("Authorization", "Bearer agent-token")
				recorder := httptest.NewRecorder()
				server.httpServer.Handler.ServeHTTP(recorder, req)
				if recorder.Code != http.StatusBadRequest || !strings.Contains(recorder.Body.String(), "invalid_argument") {
					t.Fatalf("query %q response = %d %s", query, recorder.Code, recorder.Body.String())
				}
			}
		})
	}
}

func TestUIDBoundPodTerminalRequiresExecAction(t *testing.T) {
	server := New(cfgpkg.Config{
		HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"},
		Auth: cfgpkg.AuthConfig{BearerToken: "agent-token"},
	}, nil, &k8sagent.Client{}, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/platform/workloads/pods/api-0/uid-bound/terminal?expectedUid=uid-a", nil)
	req.Header.Set("Authorization", "Bearer agent-token")
	recorder := httptest.NewRecorder()
	server.httpServer.Handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("terminal without exec allowlist = %d %s", recorder.Code, recorder.Body.String())
	}
}
