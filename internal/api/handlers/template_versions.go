package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type BuildTemplateVersionService interface {
	GetBuildTemplate(context.Context, domainidentity.Principal, string) (domaincatalog.BuildTemplate, error)
	ListBuildTemplateVersions(context.Context, domainidentity.Principal, string) ([]domaincatalog.BuildTemplate, error)
	GetBuildTemplateVersion(context.Context, domainidentity.Principal, string, int64) (domaincatalog.BuildTemplate, error)
	PublishBuildTemplate(context.Context, domainidentity.Principal, string, int64) (domaincatalog.BuildTemplate, error)
}

type WorkflowTemplateVersionService interface {
	GetWorkflowTemplate(context.Context, domainidentity.Principal, string) (domaincatalog.WorkflowTemplate, error)
	ListWorkflowTemplateVersions(context.Context, domainidentity.Principal, string) ([]domaincatalog.WorkflowTemplate, error)
	GetWorkflowTemplateVersion(context.Context, domainidentity.Principal, string, int64) (domaincatalog.WorkflowTemplate, error)
	PublishWorkflowTemplate(context.Context, domainidentity.Principal, string, int64) (domaincatalog.WorkflowTemplate, error)
}

type TemplateVersionHandler struct {
	builds    BuildTemplateVersionService
	workflows WorkflowTemplateVersionService
}

func NewTemplateVersionHandler(builds BuildTemplateVersionService, workflows WorkflowTemplateVersionService) *TemplateVersionHandler {
	return &TemplateVersionHandler{builds: builds, workflows: workflows}
}

func templateVersionParam(c *gin.Context) (int64, bool) {
	version, err := strconv.ParseInt(c.Param("version"), 10, 64)
	if err != nil || version < 1 {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "version must be a positive integer")
		return 0, false
	}
	return version, true
}

func templatePublishRevision(c *gin.Context) (int64, bool) {
	var req struct {
		ExpectedRevision int64 `json:"expectedRevision"`
	}
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil || req.ExpectedRevision < 1 || decoder.Decode(new(any)) != io.EOF {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "expectedRevision must be a positive integer")
		return 0, false
	}
	return req.ExpectedRevision, true
}

func (h *TemplateVersionHandler) GetBuildTemplate(c *gin.Context) {
	item, err := h.builds.GetBuildTemplate(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("buildTemplateID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *TemplateVersionHandler) ListBuildTemplateVersions(c *gin.Context) {
	item, err := h.builds.ListBuildTemplateVersions(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("buildTemplateID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, item)
}

func (h *TemplateVersionHandler) GetBuildTemplateVersion(c *gin.Context) {
	version, ok := templateVersionParam(c)
	if !ok {
		return
	}
	item, err := h.builds.GetBuildTemplateVersion(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("buildTemplateID"), version)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *TemplateVersionHandler) PublishBuildTemplate(c *gin.Context) {
	revision, ok := templatePublishRevision(c)
	if !ok {
		return
	}
	item, err := h.builds.PublishBuildTemplate(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("buildTemplateID"), revision)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *TemplateVersionHandler) GetWorkflowTemplate(c *gin.Context) {
	item, err := h.workflows.GetWorkflowTemplate(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("workflowTemplateID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *TemplateVersionHandler) ListWorkflowTemplateVersions(c *gin.Context) {
	item, err := h.workflows.ListWorkflowTemplateVersions(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("workflowTemplateID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, item)
}

func (h *TemplateVersionHandler) GetWorkflowTemplateVersion(c *gin.Context) {
	version, ok := templateVersionParam(c)
	if !ok {
		return
	}
	item, err := h.workflows.GetWorkflowTemplateVersion(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("workflowTemplateID"), version)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *TemplateVersionHandler) PublishWorkflowTemplate(c *gin.Context) {
	revision, ok := templatePublishRevision(c)
	if !ok {
		return
	}
	item, err := h.workflows.PublishWorkflowTemplate(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("workflowTemplateID"), revision)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
