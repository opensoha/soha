package middleware

import (
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

func RequestLogger(logger *zap.Logger) gin.HandlerFunc {
	if logger == nil {
		logger = zap.NewNop()
	}
	return func(c *gin.Context) {
		startedAt := time.Now()
		c.Next()

		status := c.Writer.Status()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		if status < http.StatusBadRequest && (route == "/healthz" || route == "/readyz") {
			return
		}

		fields := []zap.Field{
			zap.String("event", requestLogEvent(status)),
			zap.String("request_id", c.GetString("request_id")),
			zap.String("method", c.Request.Method),
			zap.String("route", route),
			zap.String("peer_ip", requestPeerIP(c.Request)),
			zap.String("client_ip", c.ClientIP()),
			zap.Int("status", status),
			zap.Float64("latency_ms", float64(time.Since(startedAt))/float64(time.Millisecond)),
		}

		switch {
		case status >= http.StatusInternalServerError:
			logger.Error("http request failed", fields...)
		case status == http.StatusUnauthorized || status == http.StatusForbidden || status == http.StatusTooManyRequests:
			logger.Warn("http request rejected", fields...)
		case status >= http.StatusBadRequest:
			logger.Info("http request rejected", fields...)
		default:
			logger.Info("http request completed", fields...)
		}
	}
}

func requestLogEvent(status int) string {
	if status >= http.StatusInternalServerError {
		return "http.request.failed"
	}
	if status >= http.StatusBadRequest {
		return "http.request.rejected"
	}
	return "http.request.completed"
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
