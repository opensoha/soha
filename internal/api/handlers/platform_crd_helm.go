package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func (h *crdResourceHandler) ListCRDs(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.reader.ListCRDs(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *crdResourceHandler) ListCRDResources(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.reader.ListCRDResources(c.Request.Context(), principal, c.Param("clusterID"), c.Param("crdName"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *crdResourceHandler) CreateCRDResource(c *gin.Context) {
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
	item, err := h.editor.CreateCRDResourceFromYAML(c.Request.Context(), principal, c.Param("clusterID"), c.Param("crdName"), namespace, payload.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}
func (h *crdResourceHandler) GetCRDResourceYAML(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.reader.GetCRDResourceYAML(c.Request.Context(), principal, c.Param("clusterID"), c.Param("crdName"), namespace, c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *crdResourceHandler) ApplyCRDResourceYAML(c *gin.Context) {
	var payload struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid yaml payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.editor.ApplyCRDResourceYAML(c.Request.Context(), principal, c.Param("clusterID"), c.Param("crdName"), namespace, c.Param("name"), payload.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *crdResourceHandler) DeleteCRDResource(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	var query struct {
		Namespace   string `form:"namespace"`
		ExpectedUID string `form:"expectedUid" binding:"omitempty,max=128"`
	}
	if err := c.ShouldBindQuery(&query); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid deletion identity")
		return
	}
	if err := h.editor.DeleteCRDResource(c.Request.Context(), principal, c.Param("clusterID"), c.Param("crdName"), query.Namespace, c.Param("name"), query.ExpectedUID); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
func (h *helmCatalogResourceHandler) ListHelmCharts(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.ListHelmCharts(c.Request.Context(), principal, c.Param("clusterID"), c.Query("keyword"), parseLimit(c.Query("limit"), 100), parseOffset(c.Query("offset")))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *helmCatalogResourceHandler) GetHelmChartDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetHelmChartDetail(c.Request.Context(), principal, c.Param("clusterID"), c.Param("repositoryName"), c.Param("chartName"), c.Query("version"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *helmCatalogResourceHandler) GetHelmChartValuesTemplate(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetHelmChartValuesTemplate(c.Request.Context(), principal, c.Param("clusterID"), c.Query("packageId"), c.Query("name"), c.Query("version"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *helmCatalogResourceHandler) InstallHelmChart(c *gin.Context) {
	var payload domainresource.HelmChartInstallInput
	if err := c.ShouldBindJSON(&payload); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid helm chart install payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.InstallHelmChart(c.Request.Context(), principal, c.Param("clusterID"), payload)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}
func (h *helmReleaseResourceHandler) ListHelmReleases(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.reader.ListHelmReleases(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *helmReleaseResourceHandler) GetHelmReleaseDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.reader.GetHelmReleaseDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("releaseName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *helmReleaseResourceHandler) ListHelmReleaseHistory(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.reader.ListHelmReleaseHistory(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("releaseName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *helmReleaseResourceHandler) GetHelmReleaseValues(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	revision := c.Query("revision")
	item, err := h.reader.GetHelmReleaseValues(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("releaseName"), revision)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *helmReleaseResourceHandler) GetHelmReleaseManifest(c *gin.Context) {
	item, err := h.reader.GetHelmReleaseManifest(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"), c.Query("namespace"), c.Param("releaseName"), c.Query("revision"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *helmReleaseResourceHandler) PlanHelmReleaseRollback(c *gin.Context) {
	var input domainresource.HelmReleaseRollbackInput
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid helm release rollback payload")
		return
	}
	plan, err := h.editor.PlanHelmReleaseRollback(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"), c.Query("namespace"), c.Param("releaseName"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, plan)
}
func (h *helmReleaseResourceHandler) RollbackHelmRelease(c *gin.Context) {
	var input domainresource.HelmReleaseRollbackInput
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid helm release rollback payload")
		return
	}
	item, err := h.editor.RollbackHelmRelease(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"), c.Query("namespace"), c.Param("releaseName"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *helmReleaseResourceHandler) UpdateHelmReleaseValues(c *gin.Context) {
	var payload struct {
		Content string `json:"content"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid helm release values payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.editor.UpdateHelmReleaseValues(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("releaseName"), payload.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *helmReleaseResourceHandler) DeleteHelmRelease(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	if err := h.editor.DeleteHelmRelease(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("releaseName")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
