package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type DeploymentTemplateCatalogService interface {
	ListDeploymentTemplates(context.Context, domainidentity.Principal) ([]domaincatalog.DeploymentTemplate, error)
	GetDeploymentTemplate(context.Context, domainidentity.Principal, string) (domaincatalog.DeploymentTemplate, error)
	SaveDeploymentTemplate(context.Context, domainidentity.Principal, string, domaincatalog.DeploymentTemplateInput) (domaincatalog.DeploymentTemplate, error)
	DeprecateDeploymentTemplate(context.Context, domainidentity.Principal, string) error
}

type DeploymentTemplateVersionService interface {
	ListDeploymentTemplateVersions(context.Context, domainidentity.Principal, string) ([]domaincatalog.DeploymentTemplate, error)
	GetDeploymentTemplateVersion(context.Context, domainidentity.Principal, string, int64) (domaincatalog.DeploymentTemplate, error)
	PublishDeploymentTemplate(context.Context, domainidentity.Principal, string, int64) (domaincatalog.DeploymentTemplate, error)
	PreviewDeploymentTemplate(context.Context, domainidentity.Principal, string, domaincatalog.DeploymentTemplatePreviewInput) (domaincatalog.DeploymentTemplatePreview, error)
}

type DeploymentTemplateHandler struct {
	catalog  DeploymentTemplateCatalogService
	versions DeploymentTemplateVersionService
}

func NewDeploymentTemplateHandler(catalog DeploymentTemplateCatalogService, versions DeploymentTemplateVersionService) *DeploymentTemplateHandler {
	return &DeploymentTemplateHandler{catalog: catalog, versions: versions}
}

func (h *DeploymentTemplateHandler) List(c *gin.Context) {
	items, err := h.catalog.ListDeploymentTemplates(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *DeploymentTemplateHandler) Get(c *gin.Context) {
	item, err := h.catalog.GetDeploymentTemplate(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("deploymentTemplateID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeploymentTemplateHandler) Save(c *gin.Context) {
	var input domaincatalog.DeploymentTemplateInput
	if !decodeDeploymentTemplateRequest(c, &input, "key", "name", "source", "parameterSchema", "defaults", "health", "enabled") {
		return
	}
	item, err := h.catalog.SaveDeploymentTemplate(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("deploymentTemplateID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	status := http.StatusOK
	if c.Request.Method == http.MethodPost {
		status = http.StatusCreated
	}
	apiresponse.Item(c, status, item)
}

func (h *DeploymentTemplateHandler) Deprecate(c *gin.Context) {
	if err := h.catalog.DeprecateDeploymentTemplate(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("deploymentTemplateID")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *DeploymentTemplateHandler) Versions(c *gin.Context) {
	items, err := h.versions.ListDeploymentTemplateVersions(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("deploymentTemplateID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *DeploymentTemplateHandler) Version(c *gin.Context) {
	version, ok := templateVersionParam(c)
	if !ok {
		return
	}
	item, err := h.versions.GetDeploymentTemplateVersion(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("deploymentTemplateID"), version)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeploymentTemplateHandler) Publish(c *gin.Context) {
	revision, ok := templatePublishRevision(c)
	if !ok {
		return
	}
	item, err := h.versions.PublishDeploymentTemplate(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("deploymentTemplateID"), revision)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *DeploymentTemplateHandler) Preview(c *gin.Context) {
	var input domaincatalog.DeploymentTemplatePreviewInput
	if !decodeDeploymentTemplateRequest(c, &input, "templateId", "version", "serviceKey", "parameters") {
		return
	}
	item, err := h.versions.PreviewDeploymentTemplate(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("applicationID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func decodeDeploymentTemplateRequest(c *gin.Context, input any, required ...string) bool {
	data, err := io.ReadAll(io.LimitReader(c.Request.Body, (2<<20)+1))
	if err != nil || len(data) > 2<<20 {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "template request exceeds 2 MiB")
		return false
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(input); err != nil || decoder.Decode(new(any)) != io.EOF {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid template request fields")
		return false
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "template request must be an object")
		return false
	}
	for _, key := range required {
		if value := bytes.TrimSpace(fields[key]); len(value) == 0 || bytes.Equal(value, []byte("null")) {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", key+" is required")
			return false
		}
	}
	return true
}
