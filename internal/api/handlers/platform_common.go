package handlers

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	aperrors "github.com/opensoha/soha/internal/api/errors"
	apiresponse "github.com/opensoha/soha/internal/api/response"
)

func requiredIdempotencyKey(c *gin.Context) (string, bool) {
	key := strings.TrimSpace(c.GetHeader("Idempotency-Key"))
	if key == "" {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "Idempotency-Key header is required")
		return "", false
	}
	return key, true
}

func parseLimit(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 {
		return fallback
	}
	return limit
}
func parseOffset(value string) int {
	offset, err := strconv.Atoi(value)
	if err != nil || offset < 0 {
		return 0
	}
	return offset
}
func writeError(c *gin.Context, err error) {
	if err == nil {
		err = errors.New("handler returned a nil error")
	}
	_ = c.Error(err)
	status := aperrors.StatusCode(err)
	code := aperrors.Code(err)
	apiresponse.Error(c, status, code, aperrors.Message(err))
}
