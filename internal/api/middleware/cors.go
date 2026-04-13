package middleware

import (
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"
)

func matchesOrigin(allowedOrigins []string, origin string) bool {
	if len(allowedOrigins) == 0 || slices.Contains(allowedOrigins, "*") {
		return true
	}

	for _, allowedOrigin := range allowedOrigins {
		if allowedOrigin == origin {
			return true
		}
		if strings.HasSuffix(allowedOrigin, "*") && strings.HasPrefix(origin, strings.TrimSuffix(allowedOrigin, "*")) {
			return true
		}
	}

	return false
}

func CORS(allowedOrigins []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		allowOrigin := ""
		if origin != "" {
			if matchesOrigin(allowedOrigins, origin) {
				allowOrigin = origin
			}
		}
		if allowOrigin != "" {
			c.Writer.Header().Set("Access-Control-Allow-Origin", allowOrigin)
			c.Writer.Header().Set("Vary", "Origin")
			c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
			c.Writer.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Source")
			c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		}
		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	}
}
