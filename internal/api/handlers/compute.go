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
	result, err := h.service.ListTasks(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), appcompute.TaskFilter{Domain: c.Query("domain"), ProviderKey: c.Query("providerKey"), Status: c.Query("status"), Category: c.Query("category"), Cursor: c.Query("cursor"), Limit: queryLimit(c, 50)})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, result)
}

func invalidComputeFilter(name string) error { return &computeInputError{name: name} }

type computeInputError struct{ name string }

func (e *computeInputError) Error() string { return "invalid compute " + e.name }
func (e *computeInputError) Unwrap() error { return apperrors.ErrInvalidArgument }
