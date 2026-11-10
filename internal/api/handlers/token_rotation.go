package handlers

import (
	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	"net/http"
)

func (h *aiGatewayPersonalTokenHandler) GetTokenRotation(c *gin.Context) {
	cliAuthHeaders(c)
	record, err := h.service.GetTokenRotation(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("rotationRequestId"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, record)
}
