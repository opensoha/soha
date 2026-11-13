package api

import (
	"context"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	runnerpkg "github.com/opensoha/soha-agent/internal/agent/runner"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
)

type outpostRuntimeController interface {
	CheckOutpostAccess(ctx context.Context, request runnerpkg.ForwardAuthRequest) (runnerpkg.ForwardAuthResult, error)
	OutpostStatus() runnerpkg.OutpostStatus
}

const proxySessionCookieName = "soha_proxy_session"
const outpostAuthHeaderName = "X-Soha-Outpost-Token"

func registerOutpostRoutes(router *gin.Engine, cfg cfgpkg.Config, runtime RuntimeTaskController) {
	if !cfg.ControlPlane.Outpost.Enabled {
		return
	}
	controller, ok := runtime.(outpostRuntimeController)
	if !ok {
		return
	}
	route := router.Group(cfg.HTTP.BasePath + "/outpost")
	route.Use(outpostAuthMiddleware(cfg.Auth.BearerToken))
	route.GET("/forward-auth", func(c *gin.Context) {
		originalURL := firstHeader(c, "X-Forwarded-Uri", "X-Original-URL")
		parsed, err := url.ParseRequestURI(originalURL)
		if err != nil {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_request", "forward-auth request is invalid")
			return
		}
		host := firstHeader(c, "X-Forwarded-Host")
		if host == "" {
			host = c.Request.Host
		}
		if !parsed.IsAbs() {
			proto := firstHeader(c, "X-Forwarded-Proto")
			if proto == "" {
				proto = "https"
			}
			if proto != "http" && proto != "https" {
				apiresponse.Error(c, http.StatusBadRequest, "invalid_request", "forward-auth request is invalid")
				return
			}
			parsed.Scheme, parsed.Host = proto, host
			originalURL = parsed.String()
		}
		requestPath := parsed.Path
		if requestPath == "" {
			requestPath = "/"
		}
		method := firstHeader(c, "X-Forwarded-Method")
		if method == "" {
			method = c.Request.Method
		}
		sessionToken := strings.TrimSpace(c.GetHeader("X-Soha-Session-Token"))
		if sessionToken == "" {
			sessionToken, _ = c.Cookie(proxySessionCookieName)
			sessionToken = strings.TrimSpace(sessionToken)
		}
		result, checkErr := controller.CheckOutpostAccess(c.Request.Context(), runnerpkg.ForwardAuthRequest{
			Method: method, OriginalURL: originalURL,
			RequestHost: host, RequestPath: requestPath,
			SessionToken: sessionToken, SourceIP: c.ClientIP(),
		})
		for name, value := range result.Headers {
			c.Header(name, value)
		}
		if result.RedirectURL != "" && result.Decision == "redirect" {
			c.Header("Location", result.RedirectURL)
			if c.Query("mode") == "nginx" {
				c.Header("X-Soha-Login-URL", result.RedirectURL)
				result.StatusCode = http.StatusUnauthorized
			}
		}
		if checkErr != nil && result.StatusCode == 0 {
			result.StatusCode = http.StatusServiceUnavailable
		}
		if result.StatusCode == 0 {
			result.StatusCode = http.StatusForbidden
		}
		c.Status(result.StatusCode)
	})
}

func outpostAuthMiddleware(token string) gin.HandlerFunc {
	expected := strings.TrimSpace(token)
	bearer := authMiddleware(token)
	return func(c *gin.Context) {
		values := c.Request.Header.Values(outpostAuthHeaderName)
		if expected != "" && len(values) == 1 && secureTokenEqual(strings.TrimSpace(values[0]), expected) {
			c.Request.Header.Del(outpostAuthHeaderName)
			c.Next()
			return
		}
		bearer(c)
	}
}

func firstHeader(c *gin.Context, names ...string) string {
	for _, name := range names {
		if value := strings.TrimSpace(c.GetHeader(name)); value != "" {
			return value
		}
	}
	return ""
}
