package middleware

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	"github.com/opensoha/soha/internal/platform/redaction"
	"go.uber.org/zap"
)

func RequestLogger(logger *zap.Logger) gin.HandlerFunc {
	if logger == nil {
		logger = zap.NewNop()
	}
	return func(c *gin.Context) {
		startedAt := time.Now()
		c.Next()

		fields := []zap.Field{
			zap.String("request_id", c.GetString("request_id")),
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.String("peer_ip", requestPeerIP(c.Request)),
			zap.String("client_ip", c.ClientIP()),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("latency", time.Since(startedAt)),
		}
		if len(c.Errors) > 0 {
			fields = append(fields, zap.String("error", safeRequestError(c.Errors.Last().Err)))
		}
		if code := c.GetString(apiresponse.ErrorCodeContextKey); code != "" {
			fields = append(fields, zap.String("error_code", code))
		}

		switch status := c.Writer.Status(); {
		case status >= http.StatusInternalServerError:
			logger.Error("http request failed", fields...)
		case status >= http.StatusBadRequest:
			logger.Warn("http request rejected", fields...)
		default:
			logger.Info("http request completed", fields...)
		}
	}
}

func safeRequestError(err error) string {
	if err == nil {
		return ""
	}
	const maxCauseBytes = 2048
	cause := redaction.Text(err.Error())
	cause = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, cause)
	if len(cause) > maxCauseBytes {
		cause = cause[:maxCauseBytes] + "..."
	}
	return cause
}

func requestPeerIP(request *http.Request) string {
	if request == nil {
		return ""
	}
	remoteAddr := strings.TrimSpace(request.RemoteAddr)
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return host
	}
	return remoteAddr
}
