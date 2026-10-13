package personalguest

import (
	"context"
	"github.com/gin-gonic/gin"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"net/http"
)

type dataService interface {
	Artifact(context.Context, networkidentity.Identity) ([]byte, error)
	DataOperation(context.Context, networkidentity.Identity, string) (guest.DataPlan, error)
	DataLease(context.Context, networkidentity.Identity) (guest.DataPlan, error)
}

func (h *handler) dataLease(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	s, ok := h.service.(dataService)
	if !ok {
		respondError(c, apperrors.ErrServiceUnavailable)
		return
	}
	result, err := s.DataLease(c.Request.Context(), identity)
	if err != nil {
		respondError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, result)
}

func (h *handler) artifact(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	s, ok := h.service.(dataService)
	if !ok {
		respondError(c, apperrors.ErrServiceUnavailable)
		return
	}
	raw, err := s.Artifact(c.Request.Context(), identity)
	if err != nil {
		respondError(c, err)
		return
	}
	c.Data(http.StatusOK, "application/octet-stream", raw)
}

func (h *handler) dataOperation(c *gin.Context) {
	identity, ok := h.identity(c)
	if !ok {
		return
	}
	s, ok := h.service.(dataService)
	if !ok {
		respondError(c, apperrors.ErrServiceUnavailable)
		return
	}
	result, err := s.DataOperation(c.Request.Context(), identity, c.Param("operationID"))
	if err != nil {
		respondError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, result)
}
