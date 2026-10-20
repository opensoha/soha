package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	cr "github.com/opensoha/soha-contracts/resource"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (h *nodeResourceHandler) GetResourceMonitoring(c *gin.Context) {
	service, ok := h.reader.(ResourceMonitoringService)
	if !ok {
		writeError(c, apperrors.ErrUnsupportedOperation)
		return
	}
	input, err := parseResourceMonitoringQuery(c)
	if err != nil {
		writeError(c, err)
		return
	}
	view, err := service.GetResourceMonitoring(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, view)
}
func parseResourceMonitoringQuery(c *gin.Context) (cr.ResourceMonitoringQuery, error) {
	input := cr.ResourceMonitoringQuery{View: c.Query("view"), Node: c.Query("node"), From: c.Query("from"), To: c.Query("to")}
	allowed := map[string]bool{"view": true, "node": true, "from": true, "to": true, "rangeMinutes": true, "stepSeconds": true}
	for key, values := range c.Request.URL.Query() {
		if !allowed[key] || len(values) != 1 {
			return input, apperrors.ErrInvalidArgument
		}
	}
	for key, target := range map[string]*int{"rangeMinutes": &input.RangeMinutes, "stepSeconds": &input.StepSeconds} {
		if value, provided := c.GetQuery(key); provided {
			parsed, err := strconv.Atoi(value)
			if err != nil {
				return input, apperrors.ErrInvalidArgument
			}
			*target = parsed
		}
	}
	return input, nil
}
