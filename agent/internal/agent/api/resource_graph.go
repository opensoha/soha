package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	apiresponse "github.com/opensoha/soha/agent/internal/api/response"
	contractruntime "github.com/opensoha/soha/contracts/resource/runtime"
)

func writeResourceGraphError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, contractruntime.ErrInvalidResourceGraphRoot):
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid resource graph scope or root")
	case errors.Is(err, contractruntime.ErrUnsupportedResourceGraphRoot):
		apiresponse.Error(c, http.StatusNotImplemented, "unsupported_operation", "resource graph root is unsupported")
	default:
		writeError(c, err)
	}
}
