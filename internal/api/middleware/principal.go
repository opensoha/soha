package middleware

import (
	"context"
	"errors"
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
		token, validHeader := accessTokenFromRequest(c)
		if !validHeader {
			_ = c.Error(errors.New("request contains multiple authorization headers"))
			apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "invalid authentication token")
			c.Abort()
			return
		}
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
				_ = c.Error(err)
				apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "invalid authentication token")
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
				_ = c.Error(err)
				apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "invalid stream ticket")
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

func accessTokenFromRequest(c *gin.Context) (requestAccessToken, bool) {
	authorization, valid := SingleAuthorizationHeader(c.Request)
	if !valid {
		return requestAccessToken{}, false
	}
	token := bearerToken(authorization)
	if token != "" {
		return requestAccessToken{value: token, source: accessTokenSourceHeader}, true
	}
	if allowsProtocolAccessCookie(c.Request.URL.Path) {
		if value, err := c.Cookie(ProtocolAccessCookieName); err == nil {
			if token = strings.TrimSpace(value); token != "" {
				return requestAccessToken{value: token, source: accessTokenSourceProtocolCookie}, true
			}
		}
	}
	if isAIGatewayLLMRelayPath(c.Request.URL.Path) {
		apiKey, valid := singleHeaderValue(c.Request, "x-api-key")
		if !valid {
			return requestAccessToken{}, false
		}
		if token = apiKey; token != "" {
			return requestAccessToken{value: token, source: accessTokenSourceAPIKey}, true
		}
	}
	return requestAccessToken{}, true
}

func SingleAuthorizationHeader(request *http.Request) (string, bool) {
	return singleHeaderValue(request, "Authorization")
}

func singleHeaderValue(request *http.Request, name string) (string, bool) {
	if request == nil {
		return "", true
	}
	values := request.Header.Values(name)
	if len(values) > 1 {
		return "", false
	}
	if len(values) == 0 {
		return "", true
	}
	return strings.TrimSpace(values[0]), true
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
	case strings.HasSuffix(path, "/ai/agent-providers/registry-snapshot"):
		return true
	case strings.HasSuffix(path, "/ai/agent-providers/registry-acks"):
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
