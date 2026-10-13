package personalguest

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"

	"github.com/gin-gonic/gin"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (h *handler) browserNetwork(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	s, ok := h.service.(interface {
		BrowserNetwork(context.Context, networkidentity.Identity, string, guest.BrowserNetworkRequest) (guest.BrowserNetworkResponse, error)
	})
	if !ok {
		respondError(c, apperrors.ErrServiceUnavailable)
		return
	}
	typeName, _, err := mime.ParseMediaType(c.GetHeader("Content-Type"))
	if err != nil || typeName != "application/json" || c.GetHeader("Content-Encoding") != "" {
		respondError(c, apperrors.ErrInvalidArgument)
		return
	}
	raw, err := io.ReadAll(io.LimitReader(c.Request.Body, 128<<10+1))
	var input guest.BrowserNetworkRequest
	if err != nil || len(raw) > 128<<10 || json.Unmarshal(raw, &input) != nil {
		respondError(c, apperrors.ErrInvalidArgument)
		return
	}
	result, err := s.BrowserNetwork(c.Request.Context(), identity, c.Param("operationID"), input)
	if err != nil {
		respondError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, result)
}

func (h *handler) toolsOperation(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	s, ok := h.service.(interface {
		ToolsOperation(context.Context, networkidentity.Identity, string) (guest.ToolsPlan, error)
	})
	if !ok {
		respondError(c, apperrors.ErrServiceUnavailable)
		return
	}
	result, err := s.ToolsOperation(c.Request.Context(), identity, c.Param("operationID"))
	if err != nil {
		respondError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, result)
}

func (h *handler) runtimeOperation(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	s, ok := h.service.(interface {
		RuntimeOperation(context.Context, networkidentity.Identity, string) (guest.RuntimeOperation, error)
	})
	if !ok {
		respondError(c, apperrors.ErrServiceUnavailable)
		return
	}
	result, err := s.RuntimeOperation(c.Request.Context(), identity, c.Param("operationID"))
	if err != nil {
		respondError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, result)
}
