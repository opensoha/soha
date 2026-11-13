package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

const personalTestImage = "busybox@sha256:9532d8c39891ca2ecde4d30d7710e01fb739c87a8b9299685c63704296b16028"
const personalTestHelper = "docker@sha256:a6dd5322747a95cd8e3207bd8d415a8fd20ec34e9c00f06dc019cbd912013489"

func personalTestRequest(action string) contractenv.Request {
	now := time.Now().UTC().Truncate(time.Microsecond)
	return contractenv.Request{SchemaVersion: contractenv.RequestVersion, Action: action, Lease: contractenv.Lease{
		SchemaVersion: contractenv.LeaseVersion, ID: "lease-fixture", RunID: "run-fixture", OwnerID: "owner-fixture", ScopeHash: "sha256:" + strings.Repeat("a", 64),
		Mode: "restricted-write", IssuedAt: now, ExpiresAt: now.Add(time.Hour), Quota: contractenv.Quota{DiskBytes: 32 << 20, MemoryBytes: 64 << 20, CPUUnits: 250, MaxProcesses: 16}}}
}

func personalTestConfig() cfgpkg.Config {
	return cfgpkg.Config{HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1", WriteTimeout: 35 * time.Second},
		Auth:                cfgpkg.AuthConfig{BearerToken: "personal-fixture-token"},
		PersonalEnvironment: cfgpkg.PersonalEnvironmentConfig{Enabled: true, Image: personalTestImage, QuotaHelperImage: personalTestHelper}}
}

func TestPersonalEnvironmentRoutesFailClosedAndAuditActions(t *testing.T) {
	// Successful authorization reaches a missing executable, without requiring a
	// real daemon in ordinary API tests. Actual Docker acceptance is opt-in.
	t.Setenv("PATH", t.TempDir())
	for _, action := range []string{"acquire", "inspect", "execute", "release", "delete_data"} {
		t.Run(action, func(t *testing.T) {
			request := personalTestRequest(action)
			if action == "execute" {
				request.Argv = []string{"/bin/true"}
			}
			data, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			for _, test := range []struct {
				name, token      string
				enabled, allowed bool
				status           int
			}{
				{"disabled", "personal-fixture-token", false, true, http.StatusNotFound},
				{"unauthenticated", "", true, true, http.StatusUnauthorized},
				{"denied", "personal-fixture-token", true, false, http.StatusForbidden},
				{"authorized-unavailable", "personal-fixture-token", true, true, http.StatusBadGateway},
			} {
				t.Run(test.name, func(t *testing.T) {
					cfg := personalTestConfig()
					cfg.PersonalEnvironment.Enabled = test.enabled
					if test.allowed {
						cfg.Security.AllowedActions = []string{"runtime.personal_environments." + action}
					}
					core, logs := observer.New(zap.InfoLevel)
					server := New(cfg, zap.New(core), nil, nil)
					req := httptest.NewRequest(http.MethodPost, "/api/v1/runtime/personal-environments", strings.NewReader(string(data)))
					if test.token != "" {
						req.Header.Set("Authorization", "Bearer "+test.token)
					}
					response := httptest.NewRecorder()
					server.httpServer.Handler.ServeHTTP(response, req)
					if response.Code != test.status {
						t.Fatalf("status=%d want=%d", response.Code, test.status)
					}
					if strings.Contains(response.Body.String(), "executable") || strings.Contains(response.Body.String(), "PATH") {
						t.Fatal("provider error exposed")
					}
					if test.status == http.StatusForbidden || test.status == http.StatusBadGateway {
						records := logs.FilterMessage("agent action audit").All()
						if len(records) != 1 || records[0].ContextMap()["action"] != "runtime.personal_environments."+action || records[0].ContextMap()["allowed"] != test.allowed {
							t.Fatal("missing action decision audit")
						}
					}
				})
			}
		})
	}
}

func TestPersonalEnvironmentRoutesRejectInvalidProtocolBeforeDocker(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	cfg := personalTestConfig()
	cfg.Security.AllowedActions = []string{actionPersonalEnvironmentAcquire}
	server := New(cfg, zap.NewNop(), nil, nil)
	base, err := json.Marshal(personalTestRequest("acquire"))
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name  string
		input func(map[string]any) string
	}{
		{"unknown field", func(p map[string]any) string { p["image"] = "caller"; b, _ := json.Marshal(p); return string(b) }},
		{"host path", func(p map[string]any) string {
			lease, ok := p["lease"].(map[string]any)
			if !ok {
				t.Fatal("invalid lease fixture")
			}
			lease["workspacePath"] = "/host"
			b, _ := json.Marshal(p)
			return string(b)
		}},
		{"null command", func(p map[string]any) string { p["argv"] = nil; b, _ := json.Marshal(p); return string(b) }},
		{"trailing object", func(p map[string]any) string { return string(base) + "{}" }},
		{"oversized", func(p map[string]any) string { return strings.Repeat(" ", 513<<10) + string(base) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			var properties map[string]any
			if err := json.Unmarshal(base, &properties); err != nil {
				t.Fatal(err)
			}
			req := httptest.NewRequest(http.MethodPost, "/api/v1/runtime/personal-environments", strings.NewReader(test.input(properties)))
			req.Header.Set("Authorization", "Bearer personal-fixture-token")
			response := httptest.NewRecorder()
			server.httpServer.Handler.ServeHTTP(response, req)
			if response.Code != http.StatusBadRequest {
				t.Fatalf("invalid request status=%d", response.Code)
			}
		})
	}
}
