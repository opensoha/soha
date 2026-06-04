package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/soha/soha/internal/api/middleware"
	apiresponse "github.com/soha/soha/internal/api/response"
	domainresource "github.com/soha/soha/internal/domain/resource"
)

func (h *PlatformHandler) ListPortForwards(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.resources.ListPortForwards(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) RegisterPortForward(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	var payload domainresource.PortForwardRegisterInput
	if err := c.ShouldBindJSON(&payload); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid port forward payload")
		return
	}
	session, err := h.resources.RegisterPortForward(c.Request.Context(), principal, c.Param("clusterID"), payload)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, session)
}

func (h *PlatformHandler) StopPortForward(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.resources.StopPortForward(c.Request.Context(), principal, c.Param("clusterID"), c.Param("sessionID")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
