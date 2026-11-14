package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	cfgpkg "github.com/opensoha/soha/agent/internal/agent/config"
)

func TestConditionalHelmValuesRequiresActionAndRevision(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		allowed    bool
		status     int
	}{
		{name: "action denied", body: `{"content":"{}","expectedRevision":3}`, status: 403},
		{name: "revision required", body: `{"content":"{}"}`, allowed: true, status: 400},
		{name: "revision overflow", body: `{"content":"{}","expectedRevision":2147483647}`, allowed: true, status: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			router := gin.New()
			policy := cfgpkg.SecurityConfig{}
			if tc.allowed {
				policy.AllowedActions = []string{actionPlatformHelmReleaseValuesUpdate}
			}
			registerHelmRoutes(router.Group("/api/v1/platform"), nil, newActionPolicy(policy, nil, nil))
			req := httptest.NewRequest(http.MethodPut, "/api/v1/platform/helm/releases/app/values/conditional?namespace=test", strings.NewReader(tc.body))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status=%d, want=%d, response=%s", rec.Code, tc.status, rec.Body)
			}
		})
	}
}
