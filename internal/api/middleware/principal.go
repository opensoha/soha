package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	apiresponse "github.com/kubecrux/kubecrux/internal/api/response"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	cfgpkg "github.com/kubecrux/kubecrux/internal/infrastructure/config"
)

const (
	principalKey   = "principal"
	accessTokenKey = "access_token"
)

type AccessTokenParser interface {
	ParseAccessToken(context.Context, string) (domainidentity.Principal, domainidentity.AccessContext, error)
}

func BuildPrincipalMiddleware(cfg cfgpkg.AuthConfig, parser AccessTokenParser) gin.HandlerFunc {
	return func(c *gin.Context) {
		bearer := strings.TrimSpace(c.GetHeader("Authorization"))
		if bearer == "" {
			bearer = strings.TrimSpace(c.Query("access_token"))
		}
		if bearer != "" {
			principal, _, err := parser.ParseAccessToken(c.Request.Context(), bearer)
			if err != nil {
				apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", err.Error())
				c.Abort()
				return
			}
			c.Set(principalKey, principal)
			c.Set(accessTokenKey, bearer)
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
