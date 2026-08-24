package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (h *resourceGraphHandler) GetResourceGraph(c *gin.Context) {
	kind, name := c.Query("kind"), c.Query("name")
	if kind == "" || name == "" {
		writeError(c, apperrors.ErrInvalidArgument)
		return
	}
	item, err := h.service.GetResourceGraph(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"), c.Query("namespace"), kind, name)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *securityPostureHandler) GetSecurityPosture(c *gin.Context) {
	item, err := h.service.GetSecurityPosture(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"), c.Query("namespace"), parseLimit(c.Query("limit"), 100))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
