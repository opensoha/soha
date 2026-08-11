package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appcompute "github.com/opensoha/soha/internal/application/compute"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type ComputeService interface {
	Capabilities(context.Context, domainidentity.Principal) (sohaapi.ComputeCapabilityManifest, error)
	Overview(context.Context, domainidentity.Principal) (sohaapi.ComputeOverview, error)
	ListAccessSources(context.Context, domainidentity.Principal, appcompute.AccessSourceFilter) (sohaapi.ComputeAccessSourceListEnvelope, error)
	ListProviders(context.Context, domainidentity.Principal, appcompute.ProviderFilter) (sohaapi.ComputeProviderListEnvelope, error)
	ListProviderInstances(context.Context, domainidentity.Principal, appcompute.ProviderInstanceFilter) (sohaapi.ComputeProviderInstanceListEnvelope, error)
	GetProviderInstance(context.Context, domainidentity.Principal, string, string, string) (sohaapi.ComputeProviderInstance, error)
	CheckProviderInstanceHealth(context.Context, domainidentity.Principal, string, string, string, string, sohaapi.ComputeProviderReadRequest) (sohaapi.ComputeTaskView, error)
	DiscoverProviderInstance(context.Context, domainidentity.Principal, string, string, string, string, sohaapi.ComputeProviderDiscoverRequest) (sohaapi.ComputeTaskView, error)
	GetResource(context.Context, domainidentity.Principal, string, string, string) (map[string]any, error)
	ListResourceRelations(context.Context, domainidentity.Principal, string, string, string, string, int) (sohaapi.ComputeResourceRelations, error)
	ExecuteResourceAction(context.Context, domainidentity.Principal, string, string, string, string, string, sohaapi.ComputeResourceActionRequest) (sohaapi.ComputeTaskView, error)
	ListTasks(context.Context, domainidentity.Principal, appcompute.TaskFilter) (sohaapi.ComputeTaskListEnvelope, error)
	GetTask(context.Context, domainidentity.Principal, string, string) (sohaapi.ComputeTaskView, error)
	ListTaskLogs(context.Context, domainidentity.Principal, string, string) (sohaapi.ComputeTaskLogListEnvelope, error)
	CancelTask(context.Context, domainidentity.Principal, string, string) (sohaapi.ComputeTaskView, error)
	RetryTask(context.Context, domainidentity.Principal, string, string) (sohaapi.ComputeTaskView, error)
}

type ComputeHandler struct{ service ComputeService }

func NewComputeHandler(service ComputeService) *ComputeHandler {
	return &ComputeHandler{service: service}
}

func (h *ComputeHandler) Capabilities(c *gin.Context) {
	item, err := h.service.Capabilities(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ComputeHandler) Overview(c *gin.Context) {
	item, err := h.service.Overview(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ComputeHandler) ListAccessSources(c *gin.Context) {
	if value := strings.TrimSpace(c.Query("sourceType")); value != "" && !sohaapi.ComputeAccessSourceType(value).Valid() {
		writeError(c, invalidComputeFilter("sourceType"))
		return
	}
	result, err := h.service.ListAccessSources(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), appcompute.AccessSourceFilter{SourceType: c.Query("sourceType"), ProviderKey: c.Query("providerKey"), Cursor: c.Query("cursor"), Limit: queryLimit(c, 50)})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, result)
}

func (h *ComputeHandler) ListProviders(c *gin.Context) {
	if value := strings.TrimSpace(c.Query("domain")); value != "" && !sohaapi.ComputeProviderDomain(value).Valid() {
		writeError(c, invalidComputeFilter("domain"))
		return
	}
	if value := strings.TrimSpace(c.Query("source")); value != "" && !sohaapi.ComputeProviderSource(value).Valid() {
		writeError(c, invalidComputeFilter("source"))
		return
	}
	result, err := h.service.ListProviders(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), appcompute.ProviderFilter{Domain: c.Query("domain"), Source: c.Query("source"), Cursor: c.Query("cursor"), Limit: queryLimit(c, 50)})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, result)
}

func (h *ComputeHandler) ListProviderInstances(c *gin.Context) {
	if value := strings.TrimSpace(c.Query("domain")); value != "" && !sohaapi.ComputeProviderDomain(value).Valid() {
		writeError(c, invalidComputeFilter("domain"))
		return
	}
	result, err := h.service.ListProviderInstances(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), appcompute.ProviderInstanceFilter{Domain: c.Query("domain"), ProviderKey: c.Query("providerKey"), Cursor: c.Query("cursor"), Limit: queryLimit(c, 50)})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, result)
}

func (h *ComputeHandler) GetProviderInstance(c *gin.Context) {
	if !validComputeProviderDomain(c) {
		return
	}
	item, err := h.service.GetProviderInstance(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("domain"), c.Param("providerKey"), c.Param("instanceRef"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ComputeHandler) CheckProviderInstanceHealth(c *gin.Context) {
	var input sohaapi.ComputeProviderReadRequest
	h.mutateProviderInstance(c, &input, func(key string) (sohaapi.ComputeTaskView, error) {
		return h.service.CheckProviderInstanceHealth(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("domain"), c.Param("providerKey"), c.Param("instanceRef"), key, input)
	})
}

func (h *ComputeHandler) DiscoverProviderInstance(c *gin.Context) {
	var input sohaapi.ComputeProviderDiscoverRequest
	h.mutateProviderInstance(c, &input, func(key string) (sohaapi.ComputeTaskView, error) {
		return h.service.DiscoverProviderInstance(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("domain"), c.Param("providerKey"), c.Param("instanceRef"), key, input)
	})
}

func (h *ComputeHandler) mutateProviderInstance(c *gin.Context, input any, invoke func(string) (sohaapi.ComputeTaskView, error)) {
	if !validComputeProviderDomain(c) {
		return
	}
	key, ok := requiredIdempotencyKey(c)
	if !ok {
		return
	}
	if err := c.ShouldBindJSON(input); err != nil {
		writeError(c, invalidComputeFilter("request body"))
		return
	}
	item, err := invoke(key)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *ComputeHandler) GetResource(c *gin.Context) {
	if !validComputeResourceIdentity(c) {
		return
	}
	item, err := h.service.GetResource(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("domain"), c.Param("kind"), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ComputeHandler) ListResourceRelations(c *gin.Context) {
	if !validComputeResourceIdentity(c) {
		return
	}
	item, err := h.service.ListResourceRelations(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("domain"), c.Param("kind"), c.Param("id"), c.Query("cursor"), queryLimit(c, 50))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ComputeHandler) ExecuteResourceAction(c *gin.Context) {
	if !validComputeResourceIdentity(c) {
		return
	}
	key, ok := requiredIdempotencyKey(c)
	if !ok {
		return
	}
	var input sohaapi.ComputeResourceActionRequest
	if err := c.ShouldBindJSON(&input); err != nil {
		writeError(c, invalidComputeFilter("request body"))
		return
	}
	item, err := h.service.ExecuteResourceAction(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("domain"), c.Param("kind"), c.Param("id"), c.Param("action"), key, input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func (h *ComputeHandler) ListTasks(c *gin.Context) {
	if value := strings.TrimSpace(c.Query("domain")); value != "" && !sohaapi.ComputeTaskDomain(value).Valid() {
		writeError(c, invalidComputeFilter("domain"))
		return
	}
	if value := strings.TrimSpace(c.Query("status")); value != "" && !sohaapi.ComputeTaskStatus(value).Valid() {
		writeError(c, invalidComputeFilter("status"))
		return
	}
	if value := strings.TrimSpace(c.Query("category")); value != "" && !sohaapi.ComputeTaskCategory(value).Valid() {
		writeError(c, invalidComputeFilter("category"))
		return
	}
	result, err := h.service.ListTasks(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), appcompute.TaskFilter{Domain: c.Query("domain"), ProviderKey: c.Query("providerKey"), Status: c.Query("status"), Category: c.Query("category"), ResourceKind: c.Query("resourceKind"), ResourceID: c.Query("resourceId"), Cursor: c.Query("cursor"), Limit: queryLimit(c, 50)})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, result)
}

func (h *ComputeHandler) GetTask(c *gin.Context) {
	if !validComputeTaskDomain(c) {
		return
	}
	item, err := h.service.GetTask(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("domain"), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ComputeHandler) ListTaskLogs(c *gin.Context) {
	if !validComputeTaskDomain(c) {
		return
	}
	result, err := h.service.ListTaskLogs(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("domain"), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, result)
}

func (h *ComputeHandler) CancelTask(c *gin.Context) {
	h.mutateTask(c, true)
}

func (h *ComputeHandler) RetryTask(c *gin.Context) {
	h.mutateTask(c, false)
}

func (h *ComputeHandler) mutateTask(c *gin.Context, cancel bool) {
	if !validComputeTaskDomain(c) {
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	var item sohaapi.ComputeTaskView
	var err error
	if cancel {
		item, err = h.service.CancelTask(c.Request.Context(), principal, c.Param("domain"), c.Param("id"))
	} else {
		item, err = h.service.RetryTask(c.Request.Context(), principal, c.Param("domain"), c.Param("id"))
	}
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusAccepted, item)
}

func validComputeTaskDomain(c *gin.Context) bool {
	if !sohaapi.ComputeTaskDomain(strings.TrimSpace(c.Param("domain"))).Valid() {
		writeError(c, invalidComputeFilter("domain"))
		return false
	}
	return true
}

func validComputeProviderDomain(c *gin.Context) bool {
	if !sohaapi.ComputeProviderDomain(strings.TrimSpace(c.Param("domain"))).Valid() {
		writeError(c, invalidComputeFilter("domain"))
		return false
	}
	return true
}

func validComputeResourceIdentity(c *gin.Context) bool {
	if !sohaapi.ComputeDomain(strings.TrimSpace(c.Param("domain"))).Valid() {
		writeError(c, invalidComputeFilter("domain"))
		return false
	}
	if !sohaapi.ComputeResourceKind(strings.TrimSpace(c.Param("kind"))).Valid() {
		writeError(c, invalidComputeFilter("kind"))
		return false
	}
	return true
}

func invalidComputeFilter(name string) error { return &computeInputError{name: name} }

type computeInputError struct{ name string }

func (e *computeInputError) Error() string { return "invalid compute " + e.name }
func (e *computeInputError) Unwrap() error { return apperrors.ErrInvalidArgument }
