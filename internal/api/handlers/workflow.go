package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kubecrux/kubecrux/internal/api/dto"
	apiMiddleware "github.com/kubecrux/kubecrux/internal/api/middleware"
	apiresponse "github.com/kubecrux/kubecrux/internal/api/response"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainworkflow "github.com/kubecrux/kubecrux/internal/domain/workflow"
)

type WorkflowService interface {
	List(context.Context, domainidentity.Principal, string, int) ([]domainworkflow.Run, error)
	Trigger(context.Context, domainidentity.Principal, domainworkflow.Input) (domainworkflow.Run, error)
}

type WorkflowHandler struct {
	service WorkflowService
}

func NewWorkflowHandler(service WorkflowService) *WorkflowHandler {
	return &WorkflowHandler{service: service}
}

func (h *WorkflowHandler) List(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.List(c.Request.Context(), principal, c.Query("applicationId"), parseLimit(c.Query("limit"), 50))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *WorkflowHandler) Trigger(c *gin.Context) {
	var req dto.TriggerWorkflowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid workflow trigger payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.Trigger(c.Request.Context(), principal, domainworkflow.Input{
		ApplicationID:  req.ApplicationID,
		WorkflowName:   req.WorkflowName,
		ClusterID:      req.ClusterID,
		Namespace:      req.Namespace,
		DeploymentName: req.DeploymentName,
		TriggerBuild:   req.TriggerBuild,
		TriggerRelease: req.TriggerRelease,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}
