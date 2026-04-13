package handlers

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/kubecrux/kubecrux/internal/api/dto"
	apiMiddleware "github.com/kubecrux/kubecrux/internal/api/middleware"
	apiresponse "github.com/kubecrux/kubecrux/internal/api/response"
	domainapp "github.com/kubecrux/kubecrux/internal/domain/application"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
)

type ApplicationService interface {
	List(context.Context, domainidentity.Principal, domainapp.Filter) ([]domainapp.App, error)
	Get(context.Context, domainidentity.Principal, string) (domainapp.App, error)
	Create(context.Context, domainidentity.Principal, domainapp.UpsertInput) (domainapp.App, error)
	Update(context.Context, domainidentity.Principal, string, domainapp.UpsertInput) (domainapp.App, error)
	Delete(context.Context, domainidentity.Principal, string) error
	ListGitRepositories(context.Context, domainidentity.Principal, string, int) ([]domainapp.GitRepository, error)
	ListGitBranches(context.Context, domainidentity.Principal, string, string, int) ([]domainapp.GitReference, error)
	ListGitTags(context.Context, domainidentity.Principal, string, string, int) ([]domainapp.GitReference, error)
}

type ApplicationHandler struct {
	service ApplicationService
}

func NewApplicationHandler(service ApplicationService) *ApplicationHandler {
	return &ApplicationHandler{service: service}
}

func (h *ApplicationHandler) ListApplications(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.List(c.Request.Context(), principal, domainapp.Filter{
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
	item, err := h.service.Get(c.Request.Context(), principal, c.Param("applicationID"))
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
	item, err := h.service.Create(c.Request.Context(), principal, mapApplicationInput(req))
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
	item, err := h.service.Update(c.Request.Context(), principal, c.Param("applicationID"), mapApplicationInput(req))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ApplicationHandler) DeleteApplication(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.Delete(c.Request.Context(), principal, c.Param("applicationID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *ApplicationHandler) ListGitRepositories(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListGitRepositories(c.Request.Context(), principal, c.Query("search"), parseLimit(c.Query("limit"), 50))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *ApplicationHandler) ListGitBranches(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListGitBranches(c.Request.Context(), principal, c.Query("projectId"), c.Query("search"), parseLimit(c.Query("limit"), 50))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *ApplicationHandler) ListGitTags(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListGitTags(c.Request.Context(), principal, c.Query("projectId"), c.Query("search"), parseLimit(c.Query("limit"), 50))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func mapApplicationInput(req dto.UpsertApplicationRequest) domainapp.UpsertInput {
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
	}
}
