package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kubecrux/kubecrux/internal/api/dto"
	apiMiddleware "github.com/kubecrux/kubecrux/internal/api/middleware"
	apiresponse "github.com/kubecrux/kubecrux/internal/api/response"
	domaincatalog "github.com/kubecrux/kubecrux/internal/domain/catalog"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
)

type CatalogService interface {
	ListBusinessLines(context.Context, domainidentity.Principal) ([]domaincatalog.BusinessLine, error)
	CreateBusinessLine(context.Context, domainidentity.Principal, domaincatalog.BusinessLineInput) (domaincatalog.BusinessLine, error)
	UpdateBusinessLine(context.Context, domainidentity.Principal, string, domaincatalog.BusinessLineInput) (domaincatalog.BusinessLine, error)
	DeleteBusinessLine(context.Context, domainidentity.Principal, string) error

	ListEnvironments(context.Context, domainidentity.Principal) ([]domaincatalog.Environment, error)
	CreateEnvironment(context.Context, domainidentity.Principal, domaincatalog.EnvironmentInput) (domaincatalog.Environment, error)
	UpdateEnvironment(context.Context, domainidentity.Principal, string, domaincatalog.EnvironmentInput) (domaincatalog.Environment, error)
	DeleteEnvironment(context.Context, domainidentity.Principal, string) error

	ListApplicationEnvironments(context.Context, domainidentity.Principal) ([]domaincatalog.ApplicationEnvironment, error)
	GetApplicationEnvironment(context.Context, domainidentity.Principal, string) (domaincatalog.ApplicationEnvironment, error)
	CreateApplicationEnvironment(context.Context, domainidentity.Principal, domaincatalog.ApplicationEnvironmentInput) (domaincatalog.ApplicationEnvironment, error)
	UpdateApplicationEnvironment(context.Context, domainidentity.Principal, string, domaincatalog.ApplicationEnvironmentInput) (domaincatalog.ApplicationEnvironment, error)
	DeleteApplicationEnvironment(context.Context, domainidentity.Principal, string) error

	ListWorkflowTemplates(context.Context, domainidentity.Principal) ([]domaincatalog.WorkflowTemplate, error)
	CreateWorkflowTemplate(context.Context, domainidentity.Principal, domaincatalog.WorkflowTemplateInput) (domaincatalog.WorkflowTemplate, error)
	UpdateWorkflowTemplate(context.Context, domainidentity.Principal, string, domaincatalog.WorkflowTemplateInput) (domaincatalog.WorkflowTemplate, error)
	DeleteWorkflowTemplate(context.Context, domainidentity.Principal, string) error
}

type CatalogHandler struct {
	service CatalogService
}

func NewCatalogHandler(service CatalogService) *CatalogHandler {
	return &CatalogHandler{service: service}
}

func (h *CatalogHandler) ListBusinessLines(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListBusinessLines(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *CatalogHandler) CreateBusinessLine(c *gin.Context) {
	var req dto.BusinessLineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid business line payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateBusinessLine(c.Request.Context(), principal, domaincatalog.BusinessLineInput{
		ID:          req.ID,
		Key:         req.Key,
		Name:        req.Name,
		Description: req.Description,
		Owners:      req.Owners,
		SortOrder:   req.SortOrder,
		Enabled:     req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *CatalogHandler) UpdateBusinessLine(c *gin.Context) {
	var req dto.BusinessLineRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid business line payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateBusinessLine(c.Request.Context(), principal, c.Param("businessLineID"), domaincatalog.BusinessLineInput{
		ID:          req.ID,
		Key:         req.Key,
		Name:        req.Name,
		Description: req.Description,
		Owners:      req.Owners,
		SortOrder:   req.SortOrder,
		Enabled:     req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *CatalogHandler) DeleteBusinessLine(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteBusinessLine(c.Request.Context(), principal, c.Param("businessLineID")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *CatalogHandler) ListEnvironments(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListEnvironments(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *CatalogHandler) CreateEnvironment(c *gin.Context) {
	var req dto.DeliveryEnvironmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid environment payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateEnvironment(c.Request.Context(), principal, domaincatalog.EnvironmentInput{
		ID:               req.ID,
		Key:              req.Key,
		Name:             req.Name,
		Tier:             req.Tier,
		StageLevel:       req.StageLevel,
		SortOrder:        req.SortOrder,
		IsProduction:     req.IsProduction,
		RequiresApproval: req.RequiresApproval,
		Enabled:          req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *CatalogHandler) UpdateEnvironment(c *gin.Context) {
	var req dto.DeliveryEnvironmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid environment payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateEnvironment(c.Request.Context(), principal, c.Param("environmentID"), domaincatalog.EnvironmentInput{
		ID:               req.ID,
		Key:              req.Key,
		Name:             req.Name,
		Tier:             req.Tier,
		StageLevel:       req.StageLevel,
		SortOrder:        req.SortOrder,
		IsProduction:     req.IsProduction,
		RequiresApproval: req.RequiresApproval,
		Enabled:          req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *CatalogHandler) DeleteEnvironment(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteEnvironment(c.Request.Context(), principal, c.Param("environmentID")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *CatalogHandler) ListApplicationEnvironments(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListApplicationEnvironments(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *CatalogHandler) GetApplicationEnvironment(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetApplicationEnvironment(c.Request.Context(), principal, c.Param("applicationEnvironmentID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *CatalogHandler) CreateApplicationEnvironment(c *gin.Context) {
	var req dto.ApplicationEnvironmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid application environment payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateApplicationEnvironment(c.Request.Context(), principal, mapApplicationEnvironmentInput(req))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *CatalogHandler) UpdateApplicationEnvironment(c *gin.Context) {
	var req dto.ApplicationEnvironmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid application environment payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateApplicationEnvironment(c.Request.Context(), principal, c.Param("applicationEnvironmentID"), mapApplicationEnvironmentInput(req))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *CatalogHandler) DeleteApplicationEnvironment(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteApplicationEnvironment(c.Request.Context(), principal, c.Param("applicationEnvironmentID")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *CatalogHandler) ListWorkflowTemplates(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListWorkflowTemplates(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *CatalogHandler) CreateWorkflowTemplate(c *gin.Context) {
	var req dto.WorkflowTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid workflow template payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateWorkflowTemplate(c.Request.Context(), principal, domaincatalog.WorkflowTemplateInput{
		ID:          req.ID,
		Key:         req.Key,
		Name:        req.Name,
		Description: req.Description,
		Category:    req.Category,
		Definition:  req.Definition,
		Enabled:     req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *CatalogHandler) UpdateWorkflowTemplate(c *gin.Context) {
	var req dto.WorkflowTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid workflow template payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateWorkflowTemplate(c.Request.Context(), principal, c.Param("workflowTemplateID"), domaincatalog.WorkflowTemplateInput{
		ID:          req.ID,
		Key:         req.Key,
		Name:        req.Name,
		Description: req.Description,
		Category:    req.Category,
		Definition:  req.Definition,
		Enabled:     req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *CatalogHandler) DeleteWorkflowTemplate(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteWorkflowTemplate(c.Request.Context(), principal, c.Param("workflowTemplateID")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func mapApplicationEnvironmentInput(req dto.ApplicationEnvironmentRequest) domaincatalog.ApplicationEnvironmentInput {
	targets := make([]domaincatalog.ReleaseTargetInput, 0, len(req.Targets))
	for _, item := range req.Targets {
		targets = append(targets, domaincatalog.ReleaseTargetInput{
			ID:            item.ID,
			ClusterID:     item.ClusterID,
			Namespace:     item.Namespace,
			WorkloadKind:  item.WorkloadKind,
			WorkloadName:  item.WorkloadName,
			ContainerName: item.ContainerName,
			Enabled:       item.Enabled,
		})
	}
	return domaincatalog.ApplicationEnvironmentInput{
		ID:                 req.ID,
		ApplicationID:      req.ApplicationID,
		EnvironmentID:      req.EnvironmentID,
		WorkflowTemplateID: req.WorkflowTemplateID,
		BuildPolicy:        req.BuildPolicy,
		ReleasePolicy:      req.ReleasePolicy,
		Targets:            targets,
	}
}
