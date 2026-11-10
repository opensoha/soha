package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

func allowGovernedAIRequest(c *gin.Context, principal domainidentity.Principal, basePath string) bool {
	if principal.GovernedAI == nil {
		return true
	}
	if principal.GovernedAI.Mode == "gateway_only" {
		for _, route := range []struct{ method, path string }{
			{http.MethodGet, "/auth/context"},
			{http.MethodGet, "/ai-gateway/capabilities"},
			{http.MethodPost, "/ai-gateway/tools/:toolName/invoke"},
			{http.MethodPost, "/ai-gateway/resources/read"},
			{http.MethodPost, "/ai-gateway/prompts/get"},
			{http.MethodPost, "/ai-gateway/plans/validate"},
			{http.MethodPost, "/ai-gateway/tasks"},
			{http.MethodGet, "/ai-gateway/tasks"},
			{http.MethodGet, "/ai-gateway/tasks/:taskId"},
			{http.MethodPost, "/ai-gateway/tasks/:taskId/cancel"},
			{http.MethodPost, "/ai-gateway/tasks/:taskId/resume"},
		} {
			if c.Request.Method == route.method && c.FullPath() == basePath+route.path {
				return true
			}
		}
	}
	apiresponse.Error(c, http.StatusForbidden, "governed_ai_required", "credential is restricted to governed AI Gateway operations")
	c.Abort()
	return false
}
