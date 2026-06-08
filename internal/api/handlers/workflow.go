package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

type WorkflowService interface {
	List(context.Context, domainidentity.Principal, string, int) ([]domainworkflow.Run, error)
	Trigger(context.Context, domainidentity.Principal, domainworkflow.Input) (domainworkflow.Run, error)
	Approve(context.Context, domainidentity.Principal, string, string) (domainworkflow.Run, error)
	Reject(context.Context, domainidentity.Principal, string, string) (domainworkflow.Run, error)
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
		ApplicationID:            req.ApplicationID,
		ApplicationEnvironmentID: req.ApplicationEnvironmentID,
		WorkflowName:             req.WorkflowName,
		ClusterID:                req.ClusterID,
		Namespace:                req.Namespace,
		DeploymentName:           req.DeploymentName,
		BuildSourceID:            req.BuildSourceID,
		RefType:                  req.RefType,
		RefName:                  req.RefName,
		ImageTag:                 req.ImageTag,
		ReleaseName:              req.ReleaseName,
		ContainerName:            req.ContainerName,
		Variables:                req.Variables,
		BuildArgs:                req.BuildArgs,
		TriggerBuild:             req.TriggerBuild,
		TriggerRelease:           req.TriggerRelease,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *WorkflowHandler) Approve(c *gin.Context) {
	var req dto.WorkflowApprovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid workflow approval payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.Approve(c.Request.Context(), principal, c.Param("workflowRunID"), req.Comment)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *WorkflowHandler) Reject(c *gin.Context) {
	var req dto.WorkflowApprovalRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid workflow approval payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.Reject(c.Request.Context(), principal, c.Param("workflowRunID"), req.Comment)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}
