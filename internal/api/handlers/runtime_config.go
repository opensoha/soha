package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type RuntimeConfigService interface {
	Get(context.Context, domainidentity.Principal) (sohaapi.RuntimeConfigSnapshot, error)
	Validate(context.Context, domainidentity.Principal, sohaapi.RuntimeConfigChangeRequest) (sohaapi.RuntimeConfigValidationResult, error)
	Apply(context.Context, domainidentity.Principal, sohaapi.RuntimeConfigChangeRequest) (sohaapi.RuntimeConfigApplyResult, error)
	History(context.Context, domainidentity.Principal, int) ([]sohaapi.RuntimeConfigRevision, error)
	Rollback(context.Context, domainidentity.Principal, sohaapi.RuntimeConfigRollbackRequest) (sohaapi.RuntimeConfigApplyResult, error)
	Application(context.Context, domainidentity.Principal, string) (sohaapi.RuntimeConfigApplication, error)
}

type RuntimeResourceProvider interface {
	Snapshot() sohaapi.RuntimeResourceSnapshot
}

type RuntimeConfigHandler struct {
	service   RuntimeConfigService
	resources RuntimeResourceProvider
}

func NewRuntimeConfigHandler(service RuntimeConfigService, resources ...RuntimeResourceProvider) *RuntimeConfigHandler {
	handler := &RuntimeConfigHandler{service: service}
	if len(resources) > 0 {
		handler.resources = resources[0]
	}
	return handler
}

func (h *RuntimeConfigHandler) Get(c *gin.Context) {
	item, err := h.service.Get(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *RuntimeConfigHandler) Resources(c *gin.Context) {
	if h.resources == nil {
		apiresponse.Item(c, http.StatusOK, sohaapi.RuntimeResourceSnapshot{})
		return
	}
	apiresponse.Item(c, http.StatusOK, h.resources.Snapshot())
}

func (h *RuntimeConfigHandler) Validate(c *gin.Context) {
	var request sohaapi.RuntimeConfigChangeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid runtime configuration payload")
		return
	}
	item, err := h.service.Validate(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), request)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *RuntimeConfigHandler) Apply(c *gin.Context) {
	var request sohaapi.RuntimeConfigChangeRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid runtime configuration payload")
		return
	}
	item, err := h.service.Apply(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), request)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *RuntimeConfigHandler) History(c *gin.Context) {
	items, err := h.service.History(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), parseLimit(c.Query("limit"), 50))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *RuntimeConfigHandler) Rollback(c *gin.Context) {
	var request sohaapi.RuntimeConfigRollbackRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid runtime configuration rollback payload")
		return
	}
	item, err := h.service.Rollback(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), request)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *RuntimeConfigHandler) Application(c *gin.Context) {
	item, err := h.service.Application(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("runtimeConfigApplicationID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
