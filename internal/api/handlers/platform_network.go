package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/soha/soha/internal/api/middleware"
	apiresponse "github.com/soha/soha/internal/api/response"
)

func (h *PlatformHandler) ListServices(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListServices(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) GetNetworkTopology(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetNetworkTopology(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) GetServiceMetrics(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	rangeMinutes := parseLimit(c.Query("rangeMinutes"), 60)
	stepSeconds := parseLimit(c.Query("stepSeconds"), 60)
	item, err := h.resources.GetServiceMetrics(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("serviceName"), rangeMinutes, stepSeconds)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) ListIngresses(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListIngresses(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) ListEndpointSlices(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListEndpointSlices(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) ListNetworkPolicies(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListNetworkPolicies(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) ListGatewayClasses(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.resources.ListGatewayClasses(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) ListGateways(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListGateways(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
