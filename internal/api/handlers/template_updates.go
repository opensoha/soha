package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainupdate "github.com/opensoha/soha/internal/domain/templateupdate"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

type TemplateUpdateReader interface {
	ListTargets(context.Context, domainidentity.Principal, domainupdate.Filter) (sohaapi.TemplateUpdateTargetPage, error)
	CreatePreview(context.Context, domainidentity.Principal, sohaapi.TemplateUpdatePreviewInput) (sohaapi.TemplateUpdatePreview, error)
	GetPreview(context.Context, domainidentity.Principal, string) (sohaapi.TemplateUpdatePreview, error)
	CreateBatch(context.Context, domainidentity.Principal, string, sohaapi.TemplateUpdateBatchInput) (domainworkflow.DeliveryBatch, error)
	ListPolicies(context.Context, domainidentity.Principal, domainupdate.Filter) ([]sohaapi.TemplateUpdatePolicy, error)
	SavePolicy(context.Context, domainidentity.Principal, sohaapi.TemplateUpdatePolicyInput) (sohaapi.TemplateUpdatePolicy, error)
}
type TemplateUpdateHandler struct{ service TemplateUpdateReader }

func NewTemplateUpdateHandler(service TemplateUpdateReader) *TemplateUpdateHandler {
	return &TemplateUpdateHandler{service: service}
}

func updateFilter(c *gin.Context) (domainupdate.Filter, bool) {
	f := domainupdate.Filter{Kind: sohaapi.TemplateUpdateKind(c.Query("kind")), TemplateID: c.Query("templateId"), ApplicationID: c.Query("applicationId"), ServiceID: c.Query("serviceId"), ApplicationEnvironmentID: c.Query("applicationEnvironmentId"), Page: 1, PageSize: 20}
	for key, destination := range map[string]*int{"page": &f.Page, "pageSize": &f.PageSize} {
		if raw, ok := c.GetQuery(key); ok {
			value, err := strconv.Atoi(raw)
			if err != nil || value < 1 || key == "pageSize" && value > 100 {
				apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid pagination")
				return f, false
			}
			*destination = value
		}
	}
	return f, true
}
func (h *TemplateUpdateHandler) Targets(c *gin.Context) {
	f, ok := updateFilter(c)
	if !ok {
		return
	}
	value, err := h.service.ListTargets(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), f)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, value)
}
func (h *TemplateUpdateHandler) Preview(c *gin.Context) {
	var input sohaapi.TemplateUpdatePreviewInput
	if !decodeDeploymentTemplateRequest(c, &input, "kind", "templateId", "toVersion", "targets") {
		return
	}
	value, err := h.service.CreatePreview(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, value)
}
func (h *TemplateUpdateHandler) GetPreview(c *gin.Context) {
	value, err := h.service.GetPreview(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("previewID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, value)
}
func (h *TemplateUpdateHandler) Batch(c *gin.Context) {
	var input sohaapi.TemplateUpdateBatchInput
	if !decodeDeploymentTemplateRequest(c, &input, "previewDigest", "candidateIds", "waveSize", "stopOnFailure", "acknowledgeHistoryGaps", "idempotencyKey") {
		return
	}
	value, err := h.service.CreateBatch(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("previewID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, value)
}
func (h *TemplateUpdateHandler) Policies(c *gin.Context) {
	f, ok := updateFilter(c)
	if !ok {
		return
	}
	value, err := h.service.ListPolicies(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), f)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, value)
}
func (h *TemplateUpdateHandler) SavePolicy(c *gin.Context) {
	var input sohaapi.TemplateUpdatePolicyInput
	if !decodeDeploymentTemplateRequest(c, &input, "kind", "templateId", "targetId", "mode", "paused", "expectedVersion") {
		return
	}
	value, err := h.service.SavePolicy(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, value)
}
