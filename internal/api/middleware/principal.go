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
	ProtocolAccessCookieName = "soha_protocol_access_token"

	principalKey     = "principal"
	accessTokenKey   = "access_token"
	accessContextKey = "access_context"
)

type accessTokenSource string

const (
	accessTokenSourceHeader         accessTokenSource = "header"
	accessTokenSourceQuery          accessTokenSource = "query"
	accessTokenSourceProtocolCookie accessTokenSource = "protocol_cookie"
	accessTokenSourceAPIKey         accessTokenSource = "api_key"
)

type requestAccessToken struct {
	value  string
	source accessTokenSource
}

type AccessTokenParser interface {
	ParseAccessToken(context.Context, string) (domainidentity.Principal, domainidentity.AccessContext, error)
}

type StreamTicketParser interface {
	ParseStreamTicket(context.Context, string, string) (domainidentity.Principal, domainidentity.AccessContext, error)
}

func BuildPrincipalMiddleware(cfg cfgpkg.AuthConfig, parser AccessTokenParser) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := accessTokenFromRequest(c)
		if token.value != "" {
			principal, accessCtx, err := parser.ParseAccessToken(c.Request.Context(), token.value)
			if err != nil {
				if token.source == accessTokenSourceProtocolCookie {
					c.Next()
					return
				}
				if allowsExternalBearerToken(c.Request.URL.Path) {
					c.Next()
					return
				}
				apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", err.Error())
				c.Abort()
				return
			}
			c.Set(principalKey, principal)
			c.Set(accessTokenKey, token.value)
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

func accessTokenFromRequest(c *gin.Context) requestAccessToken {
	token := bearerToken(c.GetHeader("Authorization"))
	if token != "" {
		return requestAccessToken{value: token, source: accessTokenSourceHeader}
	}
	token = strings.TrimSpace(c.Query("access_token"))
	if token != "" {
		return requestAccessToken{value: token, source: accessTokenSourceQuery}
	}
	if allowsProtocolAccessCookie(c.Request.URL.Path) {
		if value, err := c.Cookie(ProtocolAccessCookieName); err == nil {
			if token = strings.TrimSpace(value); token != "" {
				return requestAccessToken{value: token, source: accessTokenSourceProtocolCookie}
			}
		}
	}
	if isAIGatewayLLMRelayPath(c.Request.URL.Path) {
		if token = strings.TrimSpace(c.GetHeader("x-api-key")); token != "" {
			return requestAccessToken{value: token, source: accessTokenSourceAPIKey}
		}
	}
	return requestAccessToken{}
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

func bearerToken(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 7 && strings.EqualFold(value[:7], "Bearer ") {
		return strings.TrimSpace(value[7:])
	}
	return ""
}

func isAIGatewayLLMRelayPath(path string) bool {
	path = strings.TrimSpace(path)
	return strings.HasPrefix(path, "/api/v1/ai-gateway/llm/")
}

func allowsProtocolAccessCookie(path string) bool {
	path = strings.TrimSpace(path)
	switch path {
	case "/oauth2/authorize", "/api/v1/provider/oidc/authorize", "/api/v1/provider/proxy/callback":
		return true
	default:
		return false
	}
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
	case strings.HasPrefix(path, "/api/v1/provider/outposts"):
		return true
	case path == "/oauth2/userinfo" || path == "/api/v1/provider/oidc/userinfo":
		return true
	default:
		return false
	}
}
