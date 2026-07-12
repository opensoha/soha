package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type ApplicationCatalogService interface {
	List(context.Context, domainidentity.Principal, domainapp.Filter) ([]domainapp.App, error)
	Get(context.Context, domainidentity.Principal, string) (domainapp.App, error)
	Create(context.Context, domainidentity.Principal, domainapp.UpsertInput) (domainapp.App, error)
	Update(context.Context, domainidentity.Principal, string, domainapp.UpsertInput) (domainapp.App, error)
	Delete(context.Context, domainidentity.Principal, string) error
}

type ApplicationComponentService interface {
	ListServices(context.Context, domainidentity.Principal, string) ([]domainapp.Service, error)
	GetService(context.Context, domainidentity.Principal, string, string) (domainapp.Service, error)
	CreateService(context.Context, domainidentity.Principal, string, domainapp.ServiceInput) (domainapp.Service, error)
	UpdateService(context.Context, domainidentity.Principal, string, string, domainapp.ServiceInput) (domainapp.Service, error)
	DeleteService(context.Context, domainidentity.Principal, string, string) error
}

type ApplicationGitService interface {
	ListGitRepositories(context.Context, domainidentity.Principal, string, int) ([]domainapp.GitRepository, error)
	ListGitBranches(context.Context, domainidentity.Principal, string, string, int) ([]domainapp.GitReference, error)
	ListGitTags(context.Context, domainidentity.Principal, string, string, int) ([]domainapp.GitReference, error)
}

type ApplicationService interface {
	ApplicationCatalogService
	ApplicationComponentService
	ApplicationGitService
}

type ApplicationHandler struct {
	applications ApplicationCatalogService
	components   ApplicationComponentService
	git          ApplicationGitService
}

func NewApplicationHandler(service ApplicationService) *ApplicationHandler {
	return NewApplicationHandlerWithServices(service, service, service)
}

func NewApplicationHandlerWithServices(applications ApplicationCatalogService, components ApplicationComponentService, git ApplicationGitService) *ApplicationHandler {
	return &ApplicationHandler{applications: applications, components: components, git: git}
}

func (h *ApplicationHandler) ListApplications(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.applications.List(c.Request.Context(), principal, domainapp.Filter{
		Search: c.Query("search"),
		Limit:  parseLimit(c.Query("limit"), 100),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *ApplicationHandler) GetApplication(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.applications.Get(c.Request.Context(), principal, c.Param("applicationID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ApplicationHandler) CreateApplication(c *gin.Context) {
	var req dto.UpsertApplicationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid application payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.applications.Create(c.Request.Context(), principal, mapApplicationInput(req))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *ApplicationHandler) UpdateApplication(c *gin.Context) {
	var req dto.UpsertApplicationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid application payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.applications.Update(c.Request.Context(), principal, c.Param("applicationID"), mapApplicationInput(req))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ApplicationHandler) DeleteApplication(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.applications.Delete(c.Request.Context(), principal, c.Param("applicationID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *ApplicationHandler) ListApplicationServices(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.components.ListServices(c.Request.Context(), principal, c.Param("applicationID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *ApplicationHandler) GetApplicationService(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.components.GetService(c.Request.Context(), principal, c.Param("applicationID"), c.Param("serviceID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ApplicationHandler) CreateApplicationService(c *gin.Context) {
	var req dto.UpsertApplicationServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid application service payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.components.CreateService(c.Request.Context(), principal, c.Param("applicationID"), mapApplicationServiceInput(req))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *ApplicationHandler) UpdateApplicationService(c *gin.Context) {
	var req dto.UpsertApplicationServiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid application service payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.components.UpdateService(c.Request.Context(), principal, c.Param("applicationID"), c.Param("serviceID"), mapApplicationServiceInput(req))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ApplicationHandler) DeleteApplicationService(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.components.DeleteService(c.Request.Context(), principal, c.Param("applicationID"), c.Param("serviceID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *ApplicationHandler) ListGitRepositories(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.git.ListGitRepositories(c.Request.Context(), principal, c.Query("search"), parseLimit(c.Query("limit"), 50))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *ApplicationHandler) ListGitBranches(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.git.ListGitBranches(c.Request.Context(), principal, c.Query("projectId"), c.Query("search"), parseLimit(c.Query("limit"), 50))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *ApplicationHandler) ListGitTags(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.git.ListGitTags(c.Request.Context(), principal, c.Query("projectId"), c.Query("search"), parseLimit(c.Query("limit"), 50))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func mapApplicationInput(req dto.UpsertApplicationRequest) domainapp.UpsertInput {
	buildSources := make([]domainapp.BuildSourceInput, 0, len(req.BuildSources))
	for _, item := range req.BuildSources {
		buildSources = append(buildSources, domainapp.BuildSourceInput{
			ID:         item.ID,
			Name:       item.Name,
			Type:       domainapp.BuildSourceType(item.Type),
			Enabled:    item.Enabled,
			IsDefault:  item.IsDefault,
			BuildImage: item.BuildImage,
			DefaultTag: item.DefaultTag,
			Config:     item.Config,
		})
	}
	return domainapp.UpsertInput{
		ID:                  req.ID,
		Name:                req.Name,
		Key:                 req.Key,
		Group:               req.Group,
		BusinessLineID:      req.BusinessLineID,
		Language:            req.Language,
		Description:         req.Description,
		OwnerTeam:           req.OwnerTeam,
		RepositoryProvider:  req.RepositoryProvider,
		RepositoryProjectID: req.RepositoryProjectID,
		RepositoryPath:      req.RepositoryPath,
		DefaultBranch:       req.DefaultBranch,
		DefaultTag:          req.DefaultTag,
		BuildImage:          req.BuildImage,
		BuildContextDir:     req.BuildContextDir,
		DockerfilePath:      req.DockerfilePath,
		Enabled:             req.Enabled,
		Metadata:            req.Metadata,
		BuildSources:        buildSources,
	}
}

func mapApplicationServiceInput(req dto.UpsertApplicationServiceRequest) domainapp.ServiceInput {
	containers := make([]domainapp.ServiceContainerInput, 0, len(req.Containers))
	for _, item := range req.Containers {
		containers = append(containers, domainapp.ServiceContainerInput{
			ID:                 item.ID,
			Name:               item.Name,
			ImageRepository:    item.ImageRepository,
			DefaultTagTemplate: item.DefaultTagTemplate,
			DockerfilePath:     item.DockerfilePath,
			BuildContextDir:    item.BuildContextDir,
			RuntimePorts:       item.RuntimePorts,
			EnvSchema:          item.EnvSchema,
			ResourceProfile:    item.ResourceProfile,
			HealthCheck:        item.HealthCheck,
			Metadata:           item.Metadata,
		})
	}
	return domainapp.ServiceInput{
		ID:                  req.ID,
		Key:                 req.Key,
		Name:                req.Name,
		Description:         req.Description,
		ServiceKind:         domainapp.ServiceKind(req.ServiceKind),
		OwnerTeam:           req.OwnerTeam,
		RepositoryProvider:  req.RepositoryProvider,
		RepositoryProjectID: req.RepositoryProjectID,
		RepositoryPath:      req.RepositoryPath,
		DefaultBranch:       req.DefaultBranch,
		BuildSourceID:       req.BuildSourceID,
		Enabled:             req.Enabled,
		Metadata:            req.Metadata,
		Containers:          containers,
	}
}
