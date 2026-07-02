package middleware

import (
	"encoding/hex"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := uuid.NewString()
		traceID, spanID := traceContextFromHeaders(c)
		c.Set("request_id", requestID)
		c.Writer.Header().Set("X-Request-Id", requestID)
		meta := requestctx.Metadata{
			RequestID: requestID,
			TraceID:   traceID,
			SpanID:    spanID,
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

func traceContextFromHeaders(c *gin.Context) (string, string) {
	traceparent := strings.TrimSpace(c.GetHeader("traceparent"))
	if traceparent == "" {
		return "", ""
	}
	parts := strings.Split(traceparent, "-")
	if len(parts) != 4 {
		return "", ""
	}
	traceID := strings.ToLower(strings.TrimSpace(parts[1]))
	spanID := strings.ToLower(strings.TrimSpace(parts[2]))
	if len(traceID) != 32 || len(spanID) != 16 {
		return "", ""
	}
	if _, err := hex.DecodeString(traceID); err != nil {
		return "", ""
	}
	if _, err := hex.DecodeString(spanID); err != nil {
		return "", ""
	}
	return traceID, spanID
}
