package middleware

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func RequestOrigin(c *gin.Context) (string, error) {
	scheme := "http"
	if c.Request.TLS != nil || strings.EqualFold(strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")), "https") {
		scheme = "https"
	}
	origin, err := url.Parse(scheme + "://" + strings.TrimSpace(c.Request.Host))
	if err != nil || origin.Hostname() == "" || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return "", fmt.Errorf("%w: request origin is invalid", apperrors.ErrInvalidArgument)
	}
	return strings.ToLower(origin.Scheme) + "://" + strings.ToLower(origin.Host), nil
}
