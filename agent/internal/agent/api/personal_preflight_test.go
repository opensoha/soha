package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.uber.org/zap"
)

func TestPersonalPreflightRequiresAuthAllowlistAndStrictQuotaInput(t *testing.T) {
	valid := `{"schemaVersion":"opensoha.dev/personal-environment-preflight-request/v1","id":"00000000-0000-4000-8000-000000000048","quota":{"diskBytes":33554432,"memoryBytes":67108864,"cpuUnits":250,"maxProcesses":16,"networkEgress":false}}`
	for _, test := range []struct {
		name, body    string
		auth, allowed bool
		status        int
	}{
		{"auth", valid, false, true, http.StatusUnauthorized},
		{"action", valid, true, false, http.StatusForbidden},
		{"missing", `{}`, true, true, http.StatusBadRequest},
		{"owner", strings.Replace(valid, `"id":`, `"ownerId":"other","id":`, 1), true, true, http.StatusBadRequest},
		{"command", strings.Replace(valid, `"id":`, `"command":"/bin/sh","id":`, 1), true, true, http.StatusBadRequest},
		{"network", strings.Replace(valid, `"networkEgress":false`, `"networkEgress":true`, 1), true, true, http.StatusBadRequest},
		{"trailing", valid + `{}`, true, true, http.StatusBadRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := personalTestConfig()
			cfg.Security.AllowedActions = []string{actionPersonalEnvironmentAcquire}
			if test.allowed {
				cfg.Security.AllowedActions = append(cfg.Security.AllowedActions, actionPersonalEnvironmentPreflight)
			}
			server := New(cfg, zap.NewNop(), nil, nil)
			r := httptest.NewRequest(http.MethodPost, "/api/v1/runtime/personal-environments/preflight", strings.NewReader(test.body))
			if test.auth {
				r.Header.Set("Authorization", "Bearer personal-fixture-token")
			}
			response := httptest.NewRecorder()
			server.httpServer.Handler.ServeHTTP(response, r)
			if response.Code != test.status {
				t.Fatalf("status=%d want=%d", response.Code, test.status)
			}
		})
	}
}
