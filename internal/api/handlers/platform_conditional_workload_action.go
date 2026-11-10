package handlers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"
	contractresource "github.com/opensoha/soha-contracts/resource"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type conditionalWorkloadActionService interface {
	ExecuteWorkloadActionConditional(context.Context, domainidentity.Principal, string, string, string, string, string, contractresource.ConditionalWorkloadActionInput) (contractresource.ConditionalWorkloadActionReceipt, error)
}

func (h *genericResourceHandler) registerConditionalWorkloadActionRoute(group gin.IRoutes) {
	group.POST("/clusters/:clusterID/resources/:kind/:name/conditional/actions/:action", func(c *gin.Context) {
		service, ok := h.service.(conditionalWorkloadActionService)
		if !ok {
			writeError(c, apperrors.ErrUnsupportedOperation)
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4096)
		decoder := json.NewDecoder(c.Request.Body)
		decoder.DisallowUnknownFields()
		var input contractresource.ConditionalWorkloadActionInput
		if err := decoder.Decode(&input); err != nil {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid conditional workload action payload")
			return
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "one conditional workload action payload is required")
			return
		}
		item, err := service.ExecuteWorkloadActionConditional(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"), c.Query("namespace"), c.Param("kind"), c.Param("name"), c.Param("action"), input)
		if err != nil {
			writeError(c, err)
			return
		}
		c.Header("Cache-Control", "no-store")
		apiresponse.Item(c, http.StatusOK, item)
	})
}
