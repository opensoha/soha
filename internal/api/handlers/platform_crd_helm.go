package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/soha/soha/internal/api/middleware"
	apiresponse "github.com/soha/soha/internal/api/response"
	domainresource "github.com/soha/soha/internal/domain/resource"
)

func (h *PlatformHandler) ListCRDs(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.resources.ListCRDs(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) ListCRDResources(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListCRDResources(c.Request.Context(), principal, c.Param("clusterID"), c.Param("crdName"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) CreateCRDResource(c *gin.Context) {
	var payload struct {
		Content   string `json:"content"`
		Namespace string `json:"namespace"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid create resource payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := payload.Namespace
	if namespace == "" {
		namespace = c.Query("namespace")
	}
	item, err := h.resources.CreateCRDResourceFromYAML(c.Request.Context(), principal, c.Param("clusterID"), c.Param("crdName"), namespace, payload.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}
func (h *PlatformHandler) GetCRDResourceYAML(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetCRDResourceYAML(c.Request.Context(), principal, c.Param("clusterID"), c.Param("crdName"), namespace, c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) ApplyCRDResourceYAML(c *gin.Context) {
	var payload struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid yaml payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.ApplyCRDResourceYAML(c.Request.Context(), principal, c.Param("clusterID"), c.Param("crdName"), namespace, c.Param("name"), payload.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) DeleteCRDResource(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	if err := h.resources.DeleteCRDResource(c.Request.Context(), principal, c.Param("clusterID"), c.Param("crdName"), namespace, c.Param("name")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
func (h *PlatformHandler) ListHelmCharts(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.resources.ListHelmCharts(c.Request.Context(), principal, c.Param("clusterID"), c.Query("keyword"), parseLimit(c.Query("limit"), 100), parseOffset(c.Query("offset")))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) GetHelmChartDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.resources.GetHelmChartDetail(c.Request.Context(), principal, c.Param("clusterID"), c.Param("repositoryName"), c.Param("chartName"), c.Query("version"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) GetHelmChartValuesTemplate(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.resources.GetHelmChartValuesTemplate(c.Request.Context(), principal, c.Param("clusterID"), c.Query("packageId"), c.Query("name"), c.Query("version"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) InstallHelmChart(c *gin.Context) {
	var payload domainresource.HelmChartInstallInput
	if err := c.ShouldBindJSON(&payload); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid helm chart install payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.resources.InstallHelmChart(c.Request.Context(), principal, c.Param("clusterID"), payload)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}
func (h *PlatformHandler) ListHelmReleases(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListHelmReleases(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) GetHelmReleaseDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetHelmReleaseDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("releaseName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) ListHelmReleaseHistory(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListHelmReleaseHistory(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("releaseName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) GetHelmReleaseValues(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	revision := c.Query("revision")
	item, err := h.resources.GetHelmReleaseValues(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("releaseName"), revision)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) UpdateHelmReleaseValues(c *gin.Context) {
	var payload struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid helm release values payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.UpdateHelmReleaseValues(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("releaseName"), payload.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) DeleteHelmRelease(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	if err := h.resources.DeleteHelmRelease(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("releaseName")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
