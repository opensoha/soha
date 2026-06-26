package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
)

const (
	principalKey     = "principal"
	accessTokenKey   = "access_token"
	accessContextKey = "access_context"
)

type AccessTokenParser interface {
	ParseAccessToken(context.Context, string) (domainidentity.Principal, domainidentity.AccessContext, error)
}

type StreamTicketParser interface {
	ParseStreamTicket(context.Context, string, string) (domainidentity.Principal, domainidentity.AccessContext, error)
}

func BuildPrincipalMiddleware(cfg cfgpkg.AuthConfig, parser AccessTokenParser) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := accessTokenFromRequest(c)
		if token != "" {
			principal, accessCtx, err := parser.ParseAccessToken(c.Request.Context(), token)
			if err != nil {
				if allowsExternalBearerToken(c.Request.URL.Path) {
					c.Next()
					return
				}
				apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", err.Error())
				c.Abort()
				return
			}
			c.Set(principalKey, principal)
			c.Set(accessTokenKey, token)
			c.Set(accessContextKey, accessCtx)
			c.Next()
			return
		}
		if ticket := strings.TrimSpace(c.Query("stream_ticket")); ticket != "" {
			streamParser, ok := parser.(StreamTicketParser)
			if !ok {
				apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "stream ticket parser is not configured")
				c.Abort()
				return
			}
			principal, accessCtx, err := streamParser.ParseStreamTicket(c.Request.Context(), ticket, c.Request.URL.Path)
			if err != nil {
				apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", err.Error())
				c.Abort()
				return
			}
			c.Set(principalKey, principal)
			c.Set(accessTokenKey, ticket)
			c.Set(accessContextKey, accessCtx)
			c.Next()
			return
		}

		if cfg.EnableDevAuth {
			principal := domainidentity.Principal{
				UserID:   cfg.DevPrincipal.UserID,
				UserName: cfg.DevPrincipal.Name,
				Email:    cfg.DevPrincipal.Email,
				Roles:    append([]string(nil), cfg.DevPrincipal.Roles...),
			}
			c.Set(principalKey, principal)
		}
		c.Next()
	}
}

func accessTokenFromRequest(c *gin.Context) string {
	token := normalizeBearerToken(c.GetHeader("Authorization"))
	if token != "" {
		return token
	}
	token = strings.TrimSpace(c.Query("access_token"))
	if token != "" {
		return token
	}
	if isAIGatewayLLMRelayPath(c.Request.URL.Path) {
		return strings.TrimSpace(c.GetHeader("x-api-key"))
	}
	return ""
}

func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		principal := PrincipalFromContext(c)
		if principal.UserID == "" {
			apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "authentication required")
			c.Abort()
			return
		}
		c.Next()
	}
}

func PrincipalFromContext(c *gin.Context) domainidentity.Principal {
	principal, _ := c.Get(principalKey)
	value, _ := principal.(domainidentity.Principal)
	return value
}

func BearerTokenFromContext(c *gin.Context) string {
	token, _ := c.Get(accessTokenKey)
	value, _ := token.(string)
	return value
}

func AccessContextFromContext(c *gin.Context) domainidentity.AccessContext {
	accessCtx, _ := c.Get(accessContextKey)
	value, _ := accessCtx.(domainidentity.AccessContext)
	return value
}

func normalizeBearerToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) >= 7 && strings.EqualFold(value[:7], "Bearer ") {
		return strings.TrimSpace(value[7:])
	}
	return value
}

func isAIGatewayLLMRelayPath(path string) bool {
	path = strings.TrimSpace(path)
	return strings.HasPrefix(path, "/api/v1/ai-gateway/llm/")
}

func allowsExternalBearerToken(path string) bool {
	path = strings.TrimSpace(path)
	switch {
	case strings.HasSuffix(path, "/integrations/alerts/webhook"):
		return true
	case strings.HasSuffix(path, "/delivery/execution-callbacks"):
		return true
	case strings.HasSuffix(path, "/delivery/execution-tasks/claim"):
		return true
	case strings.Contains(path, "/delivery/execution-tasks/") && strings.HasSuffix(path, "/runner-status"):
		return true
	case strings.HasSuffix(path, "/docker/operations/claim"):
		return true
	case strings.Contains(path, "/docker/operations/") && strings.HasSuffix(path, "/runner-status"):
		return true
	case strings.HasSuffix(path, "/docker/operation-callbacks"):
		return true
	case strings.HasSuffix(path, "/copilot/agent-runs/claim"):
		return true
	case strings.HasSuffix(path, "/copilot/agent-runs/callback"):
		return true
	case strings.HasSuffix(path, "/copilot/agent-runs/tool-call"):
		return true
	case strings.HasSuffix(path, "/connectors/events"):
		return true
	default:
		return false
	}
}
