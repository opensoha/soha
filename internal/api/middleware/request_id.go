package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/soha/soha/internal/platform/requestctx"
)

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := uuid.NewString()
		c.Set("request_id", requestID)
		c.Writer.Header().Set("X-Request-Id", requestID)
		meta := requestctx.Metadata{
			RequestID: requestID,
			Path:      c.Request.URL.Path,
			Method:    c.Request.Method,
			SourceIP:  c.ClientIP(),
			Source:    c.GetHeader("X-Source"),
			UserAgent: c.Request.UserAgent(),
		}
		if meta.Source == "" {
			meta.Source = "console"
		}
		c.Request = c.Request.WithContext(requestctx.WithMetadata(c.Request.Context(), meta))
		c.Next()
	}
}
