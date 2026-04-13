package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kubecrux/kubecrux/internal/api/dto"
	apiresponse "github.com/kubecrux/kubecrux/internal/api/response"
	domainscopegrant "github.com/kubecrux/kubecrux/internal/domain/scopegrant"
)

type ScopeGrantService interface {
	List(context.Context) ([]domainscopegrant.Record, error)
	Create(context.Context, domainscopegrant.Input) (domainscopegrant.Record, error)
	Update(context.Context, string, domainscopegrant.Input) (domainscopegrant.Record, error)
	Delete(context.Context, string) error
}

type ScopeGrantHandler struct {
	service ScopeGrantService
}

func NewScopeGrantHandler(service ScopeGrantService) *ScopeGrantHandler {
	return &ScopeGrantHandler{service: service}
}

func (h *ScopeGrantHandler) List(c *gin.Context) {
	items, err := h.service.List(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *ScopeGrantHandler) Create(c *gin.Context) {
	var req dto.ScopeGrantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid scope grant payload")
		return
	}
	item, err := h.service.Create(c.Request.Context(), mapScopeGrantInput(req))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *ScopeGrantHandler) Update(c *gin.Context) {
	var req dto.ScopeGrantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid scope grant payload")
		return
	}
	item, err := h.service.Update(c.Request.Context(), c.Param("scopeGrantID"), mapScopeGrantInput(req))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ScopeGrantHandler) Delete(c *gin.Context) {
	if err := h.service.Delete(c.Request.Context(), c.Param("scopeGrantID")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func mapScopeGrantInput(req dto.ScopeGrantRequest) domainscopegrant.Input {
	return domainscopegrant.Input{
		ID:             req.ID,
		SubjectType:    req.SubjectType,
		SubjectID:      req.SubjectID,
		BusinessLineID: req.BusinessLineID,
		EnvironmentIDs: req.EnvironmentIDs,
		ApplicationIDs: req.ApplicationIDs,
		Role:           req.Role,
		Effect:         req.Effect,
		Enabled:        req.Enabled,
	}
}
