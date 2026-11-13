package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	cfgpkg "github.com/opensoha/soha/agent/internal/agent/config"
	k8sagent "github.com/opensoha/soha/agent/internal/agent/kubernetes"
	contractresource "github.com/opensoha/soha/contracts/resource"
	"go.uber.org/zap"
)

type podExecutionSpy struct {
	calls int
	input contractresource.PodArgvInput
}

func (s *podExecutionSpy) InspectPodExecutionTarget(context.Context, string, string, string, string) (contractresource.PodExecutionTarget, error) {
	s.calls++
	return contractresource.PodExecutionTarget{}, nil
}
func (s *podExecutionSpy) ExecutePodArgv(_ context.Context, _, _ string, input contractresource.PodArgvInput) (contractresource.PodArgvResult, error) {
	s.calls++
	s.input = input
	return contractresource.PodArgvResult{}, nil
}

func TestPodExecutionTransportRetainsAuthenticationAndExecAllowlist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := New(cfgpkg.Config{HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"}, Auth: cfgpkg.AuthConfig{BearerToken: "test-only-agent-token"}}, zap.NewNop(), &k8sagent.Client{}, nil)
	for _, entry := range []struct{ method, path string }{{"POST", "/pods/app/argv"}, {"GET", "/pods/app/target"}, {"GET", "/pods/app/terminal"}} {
		for _, authenticated := range []bool{false, true} {
			request := httptest.NewRequest(entry.method, "/api/v1/platform/pod-execution-v1"+entry.path, strings.NewReader(`{}`))
			if authenticated {
				request.Header.Set("Authorization", "Bearer test-only-agent-token")
			}
			response := httptest.NewRecorder()
			server.httpServer.Handler.ServeHTTP(response, request)
			want := http.StatusUnauthorized
			if authenticated {
				want = http.StatusForbidden
			}
			if response.Code != want {
				t.Fatalf("exec allowlist bypass: %d", response.Code)
			}
		}
	}
}
func TestPodExecutionTransportStrictlyBoundsLiteralArgv(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []string{"literal", "unknown-field", "trailing", "missing-uid", "stdin-limit", "argv-limit"} {
		t.Run(scenario, func(t *testing.T) {
			spy := &podExecutionSpy{}
			router := gin.New()
			registerPodExecutionRoutes(router.Group("/api/v1/platform"), spy, newActionPolicy(cfgpkg.SecurityConfig{AllowedActions: []string{actionPlatformPodsExec}}, zap.NewNop(), nil))
			input := contractresource.PodArgvInput{ExpectedUID: "reviewed", Argv: []string{"printf", "$(literal)"}}
			if scenario == "missing-uid" {
				input.ExpectedUID = ""
			}
			if scenario == "stdin-limit" {
				input.StdinBase64 = strings.Repeat("A", 90000)
			}
			if scenario == "argv-limit" {
				input.Argv = []string{strings.Repeat("x", 8193)}
			}
			body, _ := json.Marshal(input)
			if scenario == "unknown-field" {
				body = []byte(strings.TrimSuffix(string(body), "}") + `,"command":"legacy-shell"}`)
			}
			if scenario == "trailing" {
				body = append(body, []byte(` {}`)...)
			}
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest("POST", "/api/v1/platform/pod-execution-v1/pods/app/argv?namespace=test", strings.NewReader(string(body))))
			if scenario == "literal" {
				if response.Code != 200 || spy.calls != 1 || spy.input.Argv[1] != "$(literal)" || spy.input.TimeoutSeconds != 10 {
					t.Fatalf("argv changed: %d %+v", response.Code, spy.input)
				}
			} else if response.Code != 400 || spy.calls != 0 {
				t.Fatalf("invalid exec reached adapter: %d %d", response.Code, spy.calls)
			}
		})
	}
}
