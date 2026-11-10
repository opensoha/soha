package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
)

type governedTokenParser struct{ bound bool }

func (p governedTokenParser) ParseAccessToken(context.Context, string) (domainidentity.Principal, domainidentity.AccessContext, error) {
	principal := domainidentity.Principal{UserID: "user"}
	if p.bound {
		principal.GovernedAI = &domainidentity.GovernedAIBinding{Mode: "gateway_only", AIClientID: "codex", SkillID: "k8s-sre"}
	}
	return principal, domainidentity.AccessContext{TokenKind: "personal_access_token"}, nil
}

func TestGovernedAIRestrictsRoutesBeforeHandlers(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, prefix := range []string{"/api/v1", "/custom/api"} {
		for _, bound := range []bool{false, true} {
			router := gin.New()
			router.Use(BuildPrincipalMiddleware(cfgpkg.AuthConfig{}, governedTokenParser{bound}, prefix))
			for _, test := range []struct {
				method, pattern, path string
				allowed               bool
			}{
				{http.MethodGet, "/auth/context", "/auth/context", true},
				{http.MethodPost, "/ai-gateway/tools/:toolName/invoke", "/ai-gateway/tools/test/invoke", true},
				{http.MethodGet, "/ai-gateway/tasks/:taskId", "/ai-gateway/tasks/task", true},
				{http.MethodPost, "/ai-gateway/tasks/:taskId/resume", "/ai-gateway/tasks/task/resume", true},
				{http.MethodGet, "/compute/resources", "/compute/resources", false},
				{http.MethodPost, "/copilot/agent-runs/callback", "/copilot/agent-runs/callback", false},
				{http.MethodPost, "/ai-gateway/personal-access-tokens", "/ai-gateway/personal-access-tokens", false},
				{http.MethodGet, "/ai-gateway/llm/openai/v1/models", "/ai-gateway/llm/openai/v1/models", false},
			} {
				router.Handle(test.method, prefix+test.pattern, func(c *gin.Context) { c.Status(http.StatusNoContent) })
				request := httptest.NewRequest(test.method, prefix+test.path, nil)
				request.Header.Set("Authorization", "Bearer test-only")
				request.Header.Set("X-Soha-AI-Client-ID", "other-client")
				request.Header.Set("X-Soha-Skill-ID", "other-skill")
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				want := http.StatusNoContent
				if bound && !test.allowed {
					want = http.StatusForbidden
				}
				if response.Code != want {
					t.Fatalf("prefix=%s bound=%v %s: status %d, want %d", prefix, bound, test.path, response.Code, want)
				}
			}
		}
	}
}
