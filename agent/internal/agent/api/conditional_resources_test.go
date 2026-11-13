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
	domainresource "github.com/opensoha/soha/agent/internal/domain/resource"
	contractresource "github.com/opensoha/soha/contracts/resource"
	"go.uber.org/zap"
)

type conditionalResourceSpy struct {
	calls    int
	expected contractresource.ResourceMutationPreconditions
}

func (s *conditionalResourceSpy) GetResourceIdentity(context.Context, string, string, string) (contractresource.ResourceIdentity, error) {
	s.calls++
	return contractresource.ResourceIdentity{APIVersion: "v1", Kind: "ConfigMap", Name: "sample", Namespace: "test", UID: "reviewed", ResourceVersion: "1"}, nil
}
func (s *conditionalResourceSpy) ApplyResourceYAMLConditional(_ context.Context, _, _, _, _ string, expected contractresource.ResourceMutationPreconditions) (domainresource.ResourceYAMLView, error) {
	s.calls++
	s.expected = expected
	return domainresource.ResourceYAMLView{Kind: "ConfigMap", Name: "sample", Content: "apiVersion: v1"}, nil
}
func (s *conditionalResourceSpy) DryRunResourceYAMLConditional(_ context.Context, _, _, _, _ string, expected contractresource.ResourceMutationPreconditions) (domainresource.ResourceUpdateAnalysis, error) {
	s.calls++
	s.expected = expected
	return domainresource.ResourceUpdateAnalysis{}, nil
}
func (s *conditionalResourceSpy) DeleteResourceConditional(_ context.Context, _, _, _ string, expected contractresource.ResourceMutationPreconditions) error {
	s.calls++
	s.expected = expected
	return nil
}

func TestConditionalResourceMutationsRetainActionAllowlistAndAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	server := New(cfgpkg.Config{HTTP: cfgpkg.HTTPConfig{BasePath: "/api/v1"}, Auth: cfgpkg.AuthConfig{BearerToken: "test-only-agent-token"}}, zap.NewNop(), &k8sagent.Client{}, nil)
	for _, entry := range []struct{ method, path string }{{http.MethodPut, "/resources/yaml"}, {http.MethodPost, "/resources/yaml/preflight"}, {http.MethodDelete, "/resources"}} {
		for _, authenticated := range []bool{false, true} {
			request := httptest.NewRequest(entry.method, "/api/v1/platform/preconditions-v1"+entry.path, strings.NewReader(`{}`))
			if authenticated {
				request.Header.Set("Authorization", "Bearer test-only-agent-token")
			}
			recorder := httptest.NewRecorder()
			server.httpServer.Handler.ServeHTTP(recorder, request)
			want := http.StatusUnauthorized
			if authenticated {
				want = http.StatusForbidden
			}
			if recorder.Code != want {
				t.Fatalf("conditional mutation escaped authentication or allowlist: %d", recorder.Code)
			}
		}
	}
}

func TestConditionalResourceTransportRequiresGuardAndDoesNotClaimDeletionComplete(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []string{"valid-delete", "missing-guard", "unknown-field", "trailing-body", "valid-preview", "valid-edit"} {
		t.Run(scenario, func(t *testing.T) {
			spy := &conditionalResourceSpy{}
			router := gin.New()
			registerConditionalResourceRoutes(router.Group("/api/v1/platform"), spy, newActionPolicy(cfgpkg.SecurityConfig{AllowedActions: []string{actionPlatformResourcesApply, actionPlatformResourcesDelete}}, zap.NewNop(), nil))
			method, path := http.MethodDelete, "/resources"
			body := `{"namespace":"test","kind":"ConfigMap","name":"sample","preconditions":{"expectedUid":"reviewed","expectedResourceVersion":"1"}}`
			switch scenario {
			case "missing-guard":
				body = `{"kind":"ConfigMap","name":"sample"}`
			case "unknown-field":
				body = strings.TrimSuffix(body, "}") + `,"unsafe":true}`
			case "trailing-body":
				body += ` {}`
			case "valid-preview":
				method, path = http.MethodPost, "/resources/yaml/preflight"
				body = strings.TrimSuffix(body, "}") + `,"content":"apiVersion: v1"}`
			case "valid-edit":
				method, path = http.MethodPut, "/resources/yaml"
				body = strings.TrimSuffix(body, "}") + `,"content":"apiVersion: v1"}`
			}
			request := httptest.NewRequest(method, "/api/v1/platform/preconditions-v1"+path, strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			valid := strings.HasPrefix(scenario, "valid-")
			if !valid {
				if recorder.Code != 400 || spy.calls != 0 {
					t.Fatal("invalid guard reached Kubernetes client")
				}
				return
			}
			if spy.calls != 1 || spy.expected.ExpectedUID != "reviewed" || spy.expected.ExpectedResourceVersion != "1" {
				t.Fatal("caller preconditions were lost")
			}
			want := 200
			if scenario == "valid-delete" {
				want = 202
			}
			if recorder.Code != want {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			if scenario == "valid-delete" {
				var out struct {
					Data contractresource.ResourceDeletionReceipt `json:"data"`
				}
				if json.Unmarshal(recorder.Body.Bytes(), &out) != nil || !out.Data.Accepted || out.Data.State != "deletion_requested" {
					t.Fatal("deletion receipt claimed an unobserved outcome")
				}
			}
		})
	}
}
