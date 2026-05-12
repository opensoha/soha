package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/kubecrux/kubecrux/internal/api/middleware"
	apiresponse "github.com/kubecrux/kubecrux/internal/api/response"
	domainmodule "github.com/kubecrux/kubecrux/internal/domain/module"
)

type ModuleService interface {
	List(context.Context) ([]domainmodule.Status, error)
}

type ModuleHandler struct {
	service ModuleService
}

func NewModuleHandler(service ModuleService) *ModuleHandler {
	return &ModuleHandler{service: service}
}

func (h *ModuleHandler) List(c *gin.Context) {
	if h.service == nil {
		apiresponse.Items(c, http.StatusOK, []domainmodule.Status{})
		return
	}
	_ = apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.List(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
