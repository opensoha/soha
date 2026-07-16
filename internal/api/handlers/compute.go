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
	Overview(context.Context, domainidentity.Principal) (sohaapi.ComputeOverview, error)
	ListAccessSources(context.Context, domainidentity.Principal, appcompute.AccessSourceFilter) (sohaapi.ComputeAccessSourceListEnvelope, error)
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

func invalidComputeFilter(name string) error { return &computeInputError{name: name} }

type computeInputError struct{ name string }

func (e *computeInputError) Error() string { return "invalid compute " + e.name }
func (e *computeInputError) Unwrap() error { return apperrors.ErrInvalidArgument }
