package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainregistry "github.com/opensoha/soha/internal/domain/registry"
)

type RegistryService interface {
	List(context.Context, domainidentity.Principal, int) ([]domainregistry.Connection, error)
	Create(context.Context, domainidentity.Principal, domainregistry.Input) (domainregistry.Connection, error)
	Update(context.Context, domainidentity.Principal, string, domainregistry.Input) (domainregistry.Connection, error)
	Delete(context.Context, domainidentity.Principal, string) error
}

type RegistryHandler struct {
	service RegistryService
}

func NewRegistryHandler(service RegistryService) *RegistryHandler {
	return &RegistryHandler{service: service}
}

func (h *RegistryHandler) List(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.List(c.Request.Context(), principal, parseLimit(c.Query("limit"), 50))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *RegistryHandler) Create(c *gin.Context) {
	var req dto.UpsertRegistryConnectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid registry payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.Create(c.Request.Context(), principal, domainregistry.Input{
		ID:           req.ID,
		Name:         req.Name,
		RegistryType: req.RegistryType,
		Endpoint:     req.Endpoint,
		Namespace:    req.Namespace,
		Username:     req.Username,
		Secret:       req.Secret,
		Insecure:     req.Insecure,
		Metadata:     req.Metadata,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *RegistryHandler) Update(c *gin.Context) {
	var req dto.UpsertRegistryConnectionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid registry payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.Update(c.Request.Context(), principal, c.Param("connectionID"), domainregistry.Input{
		ID:           req.ID,
		Name:         req.Name,
		RegistryType: req.RegistryType,
		Endpoint:     req.Endpoint,
		Namespace:    req.Namespace,
		Username:     req.Username,
		Secret:       req.Secret,
		Insecure:     req.Insecure,
		Metadata:     req.Metadata,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *RegistryHandler) Delete(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.Delete(c.Request.Context(), principal, c.Param("connectionID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}
