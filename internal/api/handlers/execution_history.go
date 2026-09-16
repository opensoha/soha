package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type executionHistoryService interface {
	ListExecutionHistory(context.Context, domainidentity.Principal, domainworkflow.ExecutionHistoryFilter) (domainworkflow.ExecutionHistoryPage, error)
}

func (h *WorkflowHandler) ListExecutionHistory(c *gin.Context) {
	f := domainworkflow.ExecutionHistoryFilter{ApplicationID: c.Query("applicationId"), ServiceID: c.Query("serviceId"), ApplicationEnvironmentID: c.Query("applicationEnvironmentId"), WorkflowID: c.Query("workflowId"), BuildSourceID: c.Query("buildSourceId"), Status: c.Query("status"), Search: c.Query("search"), Cursor: c.Query("cursor")}
	if value, exists := c.GetQuery("limit"); exists {
		limit, err := strconv.Atoi(value)
		if err != nil || limit < 1 || limit > 100 {
			writeError(c, apperrors.ErrInvalidArgument)
			return
		}
		f.Limit = limit
	}
	service, ok := h.service.(executionHistoryService)
	if !ok {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	page, err := service.ListExecutionHistory(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), f)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, page)
}
