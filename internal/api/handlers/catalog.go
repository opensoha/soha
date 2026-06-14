package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type CatalogService interface {
	ListApplicationEnvironments(context.Context, domainidentity.Principal) ([]domaincatalog.ApplicationEnvironment, error)
	GetApplicationEnvironment(context.Context, domainidentity.Principal, string) (domaincatalog.ApplicationEnvironment, error)
	CreateApplicationEnvironment(context.Context, domainidentity.Principal, domaincatalog.ApplicationEnvironmentInput) (domaincatalog.ApplicationEnvironment, error)
	UpdateApplicationEnvironment(context.Context, domainidentity.Principal, string, domaincatalog.ApplicationEnvironmentInput) (domaincatalog.ApplicationEnvironment, error)
	DeleteApplicationEnvironment(context.Context, domainidentity.Principal, string) error

	ListBuildTemplates(context.Context, domainidentity.Principal) ([]domaincatalog.BuildTemplate, error)
	CreateBuildTemplate(context.Context, domainidentity.Principal, domaincatalog.BuildTemplateInput) (domaincatalog.BuildTemplate, error)
	UpdateBuildTemplate(context.Context, domainidentity.Principal, string, domaincatalog.BuildTemplateInput) (domaincatalog.BuildTemplate, error)
	DeleteBuildTemplate(context.Context, domainidentity.Principal, string) error

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

func (h *CatalogHandler) ListBuildTemplates(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListBuildTemplates(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *CatalogHandler) CreateBuildTemplate(c *gin.Context) {
	var req dto.BuildTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid build template payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateBuildTemplate(c.Request.Context(), principal, domaincatalog.BuildTemplateInput{
		ID:                 req.ID,
		Key:                req.Key,
		Name:               req.Name,
		Description:        req.Description,
		BuilderKind:        req.BuilderKind,
		DockerfileTemplate: req.DockerfileTemplate,
		BuildCommands:      req.BuildCommands,
		VariableSchema:     req.VariableSchema,
		DefaultVariables:   req.DefaultVariables,
		Enabled:            req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *CatalogHandler) UpdateBuildTemplate(c *gin.Context) {
	var req dto.BuildTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid build template payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateBuildTemplate(c.Request.Context(), principal, c.Param("buildTemplateID"), domaincatalog.BuildTemplateInput{
		ID:                 req.ID,
		Key:                req.Key,
		Name:               req.Name,
		Description:        req.Description,
		BuilderKind:        req.BuilderKind,
		DockerfileTemplate: req.DockerfileTemplate,
		BuildCommands:      req.BuildCommands,
		VariableSchema:     req.VariableSchema,
		DefaultVariables:   req.DefaultVariables,
		Enabled:            req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *CatalogHandler) DeleteBuildTemplate(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteBuildTemplate(c.Request.Context(), principal, c.Param("buildTemplateID")); err != nil {
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
			TargetKind:    item.TargetKind,
			ExecutorKind:  item.ExecutorKind,
			GroupKey:      item.GroupKey,
			WaveKey:       item.WaveKey,
			RegionKey:     item.RegionKey,
			ConfigRef:     item.ConfigRef,
			WorkloadKind:  item.WorkloadKind,
			WorkloadName:  item.WorkloadName,
			ContainerName: item.ContainerName,
			Metadata:      item.Metadata,
			Enabled:       item.Enabled,
		})
	}
	return domaincatalog.ApplicationEnvironmentInput{
		ID:                 req.ID,
		ApplicationID:      req.ApplicationID,
		EnvironmentID:      req.EnvironmentID,
		StrategyProfileID:  req.StrategyProfileID,
		PromotionPolicyID:  req.PromotionPolicyID,
		ArtifactPolicyID:   req.ArtifactPolicyID,
		WorkflowTemplateID: req.WorkflowTemplateID,
		BuildPolicy: domaincatalog.BuildPolicy{
			SourceID:         req.BuildPolicy.SourceID,
			RefType:          req.BuildPolicy.RefType,
			RefValue:         req.BuildPolicy.RefValue,
			ImageTagMode:     req.BuildPolicy.ImageTagMode,
			ImageTagTemplate: req.BuildPolicy.ImageTagTemplate,
			Variables:        req.BuildPolicy.Variables,
			BuildArgs:        req.BuildPolicy.BuildArgs,
		},
		ReleasePolicy: domaincatalog.ReleasePolicy{
			ActionKind:            req.ReleasePolicy.ActionKind,
			RequiresApproval:      req.ReleasePolicy.RequiresApproval,
			ApproverRoles:         req.ReleasePolicy.ApproverRoles,
			AutoRollback:          req.ReleasePolicy.AutoRollback,
			RolloutTimeoutSeconds: req.ReleasePolicy.RolloutTimeoutSeconds,
			VerificationMode:      req.ReleasePolicy.VerificationMode,
		},
		ResourceSelector: domaincatalog.ResourceSelector{
			MatchLabels: req.ResourceSelector.MatchLabels,
		},
		Targets: targets,
	}
}
