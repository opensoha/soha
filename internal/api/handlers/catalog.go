package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type ApplicationEnvironmentService interface {
	ListEnvironments(context.Context, domainidentity.Principal) ([]domaincatalog.Environment, error)
	ListApplicationEnvironments(context.Context, domainidentity.Principal) ([]domaincatalog.ApplicationEnvironment, error)
	GetApplicationEnvironment(context.Context, domainidentity.Principal, string) (domaincatalog.ApplicationEnvironment, error)
	CreateApplicationEnvironment(context.Context, domainidentity.Principal, domaincatalog.ApplicationEnvironmentInput) (domaincatalog.ApplicationEnvironment, error)
	UpdateApplicationEnvironment(context.Context, domainidentity.Principal, string, domaincatalog.ApplicationEnvironmentInput) (domaincatalog.ApplicationEnvironment, error)
	DeleteApplicationEnvironment(context.Context, domainidentity.Principal, string) error
	SaveApplicationWorkflow(context.Context, domainidentity.Principal, string, string, domaincatalog.ApplicationWorkflowInput) (domaincatalog.ApplicationEnvironment, error)
}

type BuildTemplateService interface {
	ListBuildTemplates(context.Context, domainidentity.Principal) ([]domaincatalog.BuildTemplate, error)
	GetBuildTemplateUsage(context.Context, domainidentity.Principal, string) (domaincatalog.TemplateUsageSummary, error)
	CreateBuildTemplate(context.Context, domainidentity.Principal, domaincatalog.BuildTemplateInput) (domaincatalog.BuildTemplate, error)
	UpdateBuildTemplate(context.Context, domainidentity.Principal, string, domaincatalog.BuildTemplateInput) (domaincatalog.BuildTemplate, error)
	DeleteBuildTemplate(context.Context, domainidentity.Principal, string) error
}

type WorkflowTemplateService interface {
	ListWorkflowTemplates(context.Context, domainidentity.Principal) ([]domaincatalog.WorkflowTemplate, error)
	GetWorkflowTemplateUsage(context.Context, domainidentity.Principal, string) (domaincatalog.TemplateUsageSummary, error)
	CreateWorkflowTemplate(context.Context, domainidentity.Principal, domaincatalog.WorkflowTemplateInput) (domaincatalog.WorkflowTemplate, error)
	UpdateWorkflowTemplate(context.Context, domainidentity.Principal, string, domaincatalog.WorkflowTemplateInput) (domaincatalog.WorkflowTemplate, error)
	DeleteWorkflowTemplate(context.Context, domainidentity.Principal, string) error
}

type CatalogService interface {
	ApplicationEnvironmentService
	BuildTemplateService
	WorkflowTemplateService
}

type WorkflowCatalogService interface {
	ListWorkflowCatalog(context.Context, domainidentity.Principal, domaincatalog.WorkflowCatalogFilter) (domaincatalog.WorkflowCatalogPage, error)
}

func (h *CatalogHandler) ListWorkflowCatalog(c *gin.Context) {
	filter := domaincatalog.WorkflowCatalogFilter{Kind: c.Query("kind"), ApplicationID: c.Query("applicationId"), EnvironmentID: c.Query("environmentId"), Search: c.Query("search"), Limit: 12}
	for key, value := range map[string]*int{"offset": &filter.Offset, "limit": &filter.Limit} {
		if raw, present := c.GetQuery(key); present {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 0 || (key == "limit" && (parsed < 1 || parsed > 200)) {
				apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid workflow catalog pagination")
				return
			}
			*value = parsed
		}
	}
	service, ok := h.environments.(WorkflowCatalogService)
	if !ok {
		apiresponse.Error(c, http.StatusServiceUnavailable, "unavailable", "workflow catalog is unavailable")
		return
	}
	page, err := service.ListWorkflowCatalog(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), filter)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, page)
}

type CatalogHandler struct {
	environments ApplicationEnvironmentService
	builds       BuildTemplateService
	workflows    WorkflowTemplateService
}

func NewCatalogHandler(service CatalogService) *CatalogHandler {
	return NewCatalogHandlerWithServices(service, service, service)
}

func NewCatalogHandlerWithServices(environments ApplicationEnvironmentService, builds BuildTemplateService, workflows WorkflowTemplateService) *CatalogHandler {
	return &CatalogHandler{environments: environments, builds: builds, workflows: workflows}
}

func (h *CatalogHandler) ListEnvironments(c *gin.Context) {
	items, err := h.environments.ListEnvironments(
		c.Request.Context(),
		apiMiddleware.PrincipalFromContext(c),
	)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *CatalogHandler) ListApplicationEnvironments(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.environments.ListApplicationEnvironments(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *CatalogHandler) GetApplicationEnvironment(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.environments.GetApplicationEnvironment(c.Request.Context(), principal, c.Param("applicationEnvironmentID"))
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
	item, err := h.environments.CreateApplicationEnvironment(c.Request.Context(), principal, mapApplicationEnvironmentInput(req))
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
	item, err := h.environments.UpdateApplicationEnvironment(c.Request.Context(), principal, c.Param("applicationEnvironmentID"), mapApplicationEnvironmentInput(req))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *CatalogHandler) DeleteApplicationEnvironment(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.environments.DeleteApplicationEnvironment(c.Request.Context(), principal, c.Param("applicationEnvironmentID")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *CatalogHandler) SaveApplicationWorkflow(c *gin.Context) {
	var req dto.ApplicationWorkflowRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid application workflow payload")
		return
	}
	item, err := h.environments.SaveApplicationWorkflow(
		c.Request.Context(),
		apiMiddleware.PrincipalFromContext(c),
		c.Param("applicationID"),
		c.Param("applicationEnvironmentID"),
		domaincatalog.ApplicationWorkflowInput{
			ExpectedRevision: req.ExpectedRevision,
			Name:             req.Name,
			Description:      req.Description,
			Definition:       req.Definition,
			Enabled:          req.Enabled,
		},
	)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *CatalogHandler) ListBuildTemplates(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.builds.ListBuildTemplates(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *CatalogHandler) GetBuildTemplateUsage(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.builds.GetBuildTemplateUsage(c.Request.Context(), principal, c.Param("buildTemplateID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *CatalogHandler) CreateBuildTemplate(c *gin.Context) {
	var req dto.BuildTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid build template payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.builds.CreateBuildTemplate(c.Request.Context(), principal, buildTemplateInput(req))
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
	item, err := h.builds.UpdateBuildTemplate(c.Request.Context(), principal, c.Param("buildTemplateID"), buildTemplateInput(req))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func buildTemplateInput(req dto.BuildTemplateRequest) domaincatalog.BuildTemplateInput {
	return domaincatalog.BuildTemplateInput{
		CopiedFrom:       req.CopiedFrom,
		ExpectedRevision: req.ExpectedRevision, Publish: req.Publish,
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
	}
}

func (h *CatalogHandler) DeleteBuildTemplate(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.builds.DeleteBuildTemplate(c.Request.Context(), principal, c.Param("buildTemplateID")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *CatalogHandler) ListWorkflowTemplates(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.workflows.ListWorkflowTemplates(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *CatalogHandler) GetWorkflowTemplateUsage(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.workflows.GetWorkflowTemplateUsage(c.Request.Context(), principal, c.Param("workflowTemplateID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *CatalogHandler) CreateWorkflowTemplate(c *gin.Context) {
	var req dto.WorkflowTemplateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid workflow template payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.workflows.CreateWorkflowTemplate(c.Request.Context(), principal, domaincatalog.WorkflowTemplateInput{
		CopiedFrom:       req.CopiedFrom,
		ExpectedRevision: req.ExpectedRevision,
		Publish:          req.Publish,
		ID:               req.ID,
		Key:              req.Key,
		Name:             req.Name,
		Description:      req.Description,
		Category:         req.Category,
		Definition:       req.Definition,
		Enabled:          req.Enabled,
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
	item, err := h.workflows.UpdateWorkflowTemplate(c.Request.Context(), principal, c.Param("workflowTemplateID"), domaincatalog.WorkflowTemplateInput{
		CopiedFrom:       req.CopiedFrom,
		ExpectedRevision: req.ExpectedRevision,
		Publish:          req.Publish,
		ID:               req.ID,
		Key:              req.Key,
		Name:             req.Name,
		Description:      req.Description,
		Category:         req.Category,
		Definition:       req.Definition,
		Enabled:          req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *CatalogHandler) DeleteWorkflowTemplate(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.workflows.DeleteWorkflowTemplate(c.Request.Context(), principal, c.Param("workflowTemplateID")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func mapApplicationEnvironmentInput(req dto.ApplicationEnvironmentRequest) domaincatalog.ApplicationEnvironmentInput {
	targets := make([]domaincatalog.ReleaseTargetInput, 0, len(req.Targets))
	for _, item := range req.Targets {
		targets = append(targets, domaincatalog.ReleaseTargetInput{
			Helm:          item.Helm,
			Docker:        item.Docker,
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
		ExpectedUpdatedAt:       req.ExpectedUpdatedAt,
		ID:                      req.ID,
		ApplicationID:           req.ApplicationID,
		EnvironmentID:           req.EnvironmentID,
		Alias:                   req.Alias,
		ClusterID:               req.ClusterID,
		Namespace:               req.Namespace,
		RegistryID:              req.RegistryID,
		StrategyProfileID:       req.StrategyProfileID,
		PromotionPolicyID:       req.PromotionPolicyID,
		ArtifactPolicyID:        req.ArtifactPolicyID,
		WorkflowTemplateID:      req.WorkflowTemplateID,
		WorkflowTemplateVersion: req.WorkflowTemplateVersion,
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
