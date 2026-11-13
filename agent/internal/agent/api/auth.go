package api

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
)

func authMiddleware(token string) gin.HandlerFunc {
	allowed := allowedAuthTokens(token)
	return func(c *gin.Context) {
		if len(allowed) == 0 {
			c.Next()
			return
		}
		if requestHasAnyBearerToken(c.Request, allowed) {
			c.Next()
			return
		}
		apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "invalid agent token")
		c.Abort()
	}
}

func authAnyMiddleware(tokens ...string) gin.HandlerFunc {
	allowed := allowedAuthTokens(tokens...)
	return func(c *gin.Context) {
		if len(allowed) == 0 {
			c.Next()
			return
		}
		if requestHasAnyBearerToken(c.Request, allowed) {
			c.Next()
			return
		}
		apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "invalid agent token")
		c.Abort()
	}
}

func authRequiredAnyMiddleware(tokens ...string) gin.HandlerFunc {
	allowed := allowedAuthTokens(tokens...)
	return func(c *gin.Context) {
		if len(allowed) == 0 {
			apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "agent token is required")
			c.Abort()
			return
		}
		if requestHasAnyBearerToken(c.Request, allowed) {
			c.Next()
			return
		}
		apiresponse.Error(c, http.StatusUnauthorized, "unauthorized", "invalid agent token")
		c.Abort()
	}
}

func allowedAuthTokens(tokens ...string) []string {
	allowed := make([]string, 0, len(tokens))
	for _, token := range tokens {
		if trimmed := strings.TrimSpace(token); trimmed != "" {
			allowed = append(allowed, trimmed)
		}
	}
	return allowed
}

func requestHasAnyBearerToken(r *http.Request, allowed []string) bool {
	if r == nil {
		return false
	}
	if len(r.Header.Values("Authorization")) != 1 {
		return false
	}
	presented, ok := parseBearerToken(r.Header.Get("Authorization"))
	if !ok {
		return false
	}
	matched := 0
	for _, expected := range allowed {
		matched |= subtle.ConstantTimeCompare([]byte(presented), []byte(expected))
	}
	return matched == 1
}

func parseBearerToken(header string) (string, bool) {
	header = strings.TrimSpace(header)
	scheme, token, ok := strings.Cut(header, " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	if token == "" || strings.ContainsAny(token, " \t\r\n") {
		return "", false
	}
	return token, true
}

func secureTokenEqual(presented, expected string) bool {
	return subtle.ConstantTimeCompare([]byte(presented), []byte(expected)) == 1
}
