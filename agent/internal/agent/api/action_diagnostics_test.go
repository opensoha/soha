package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	contractresource "github.com/opensoha/soha-contracts/resource"
)

func TestActionDiagnosticsRequireAuthenticationAndReportActualPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	registerActionDiagnostics(router, cfgpkg.Config{HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"}, Auth: cfgpkg.AuthConfig{BearerToken: "test-agent-token"}, Security: cfgpkg.SecurityConfig{AllowedActions: []string{actionPlatformNodesDrain}}}, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/actions", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated diagnostics: %d", response.Code)
	}
	request = httptest.NewRequest(http.MethodGet, "/api/v1/diagnostics/actions", nil)
	request.Header.Set("Authorization", "Bearer test-agent-token")
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	var payload struct {
		Items []contractresource.AgentActionCheck `json:"items"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &payload) != nil || len(payload.Items) == 0 {
		t.Fatalf("diagnostics: %d %s", response.Code, response.Body.String())
	}
	for _, check := range payload.Items {
		want := "denied"
		if check.Action == actionPlatformNodesDrain {
			want = "allowed"
		}
		if check.Configured != want || check.KubernetesRBAC == "allowed" || check.CheckedAt == "" {
			t.Fatalf("unproven policy/RBAC claim: %+v", check)
		}
	}
}
