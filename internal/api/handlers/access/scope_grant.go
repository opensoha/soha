package access

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainscopegrant "github.com/opensoha/soha/internal/domain/scopegrant"
)

type ScopeGrantService interface {
	List(context.Context, domainidentity.Principal, string, string) ([]domainscopegrant.Record, error)
	ListLegacy(context.Context, domainidentity.Principal) ([]domainscopegrant.Record, error)
	Create(context.Context, domainidentity.Principal, string, string, domainscopegrant.Input) (domainscopegrant.Record, error)
	CreateLegacy(context.Context, domainidentity.Principal, domainscopegrant.Input) (domainscopegrant.Record, error)
	Update(context.Context, domainidentity.Principal, string, string, string, domainscopegrant.Input) (domainscopegrant.Record, error)
	UpdateLegacy(context.Context, domainidentity.Principal, string, domainscopegrant.Input) (domainscopegrant.Record, error)
	Delete(context.Context, domainidentity.Principal, string, string, string) error
	DeleteLegacy(context.Context, domainidentity.Principal, string) error
}

type ScopeGrantHandler struct {
	service ScopeGrantService
}

func NewScopeGrantHandler(service ScopeGrantService) *ScopeGrantHandler {
	return &ScopeGrantHandler{service: service}
}

func (h *ScopeGrantHandler) ListLegacy(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListLegacy(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *ScopeGrantHandler) CreateLegacy(c *gin.Context) {
	var req dto.ScopeGrantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid scope grant payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateLegacy(c.Request.Context(), principal, mapScopeGrantInput(req))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *ScopeGrantHandler) UpdateLegacy(c *gin.Context) {
	var req dto.ScopeGrantRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid scope grant payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateLegacy(c.Request.Context(), principal, c.Param("scopeGrantID"), mapScopeGrantInput(req))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ScopeGrantHandler) DeleteLegacy(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteLegacy(c.Request.Context(), principal, c.Param("scopeGrantID")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *ScopeGrantHandler) List(subjectType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		principal := apiMiddleware.PrincipalFromContext(c)
		items, err := h.service.List(c.Request.Context(), principal, subjectType, c.Param(subjectType+"ID"))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Items(c, http.StatusOK, items)
	}
}

func (h *ScopeGrantHandler) Create(subjectType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req dto.ScopeGrantRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid scope grant payload")
			return
		}
		principal := apiMiddleware.PrincipalFromContext(c)
		item, err := h.service.Create(c.Request.Context(), principal, subjectType, c.Param(subjectType+"ID"), mapScopeGrantInput(req))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusCreated, item)
	}
}

func (h *ScopeGrantHandler) Update(subjectType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req dto.ScopeGrantRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid scope grant payload")
			return
		}
		principal := apiMiddleware.PrincipalFromContext(c)
		item, err := h.service.Update(c.Request.Context(), principal, subjectType, c.Param(subjectType+"ID"), c.Param("scopeGrantID"), mapScopeGrantInput(req))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	}
}

func (h *ScopeGrantHandler) Delete(subjectType string) gin.HandlerFunc {
	return func(c *gin.Context) {
		principal := apiMiddleware.PrincipalFromContext(c)
		if err := h.service.Delete(c.Request.Context(), principal, subjectType, c.Param(subjectType+"ID"), c.Param("scopeGrantID")); err != nil {
			writeError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

func mapScopeGrantInput(req dto.ScopeGrantRequest) domainscopegrant.Input {
	return domainscopegrant.Input{
		ID:                req.ID,
		SubjectType:       req.SubjectType,
		SubjectID:         req.SubjectID,
		BusinessLineID:    req.BusinessLineID,
		EnvironmentIDs:    req.EnvironmentIDs,
		ApplicationIDs:    req.ApplicationIDs,
		ScopeType:         req.ScopeType,
		ClusterIDs:        req.ClusterIDs,
		Namespaces:        req.Namespaces,
		NamespaceSelector: req.NamespaceSelector,
		ResourceGroups:    req.ResourceGroups,
		ResourceKinds:     req.ResourceKinds,
		Role:              req.Role,
		Effect:            req.Effect,
		Enabled:           req.Enabled,
	}
}
