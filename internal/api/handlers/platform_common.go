package handlers

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	aperrors "github.com/soha/soha/internal/api/errors"
	apiresponse "github.com/soha/soha/internal/api/response"
)

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
	status := aperrors.StatusCode(err)
	code := aperrors.Code(err)
	message := err.Error()
	if status == http.StatusInternalServerError {
		message = fmt.Sprintf("request failed: %v", err)
	}
	apiresponse.Error(c, status, code, message)
}
