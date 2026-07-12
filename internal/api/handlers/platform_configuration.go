package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
)

func (h *configMapResourceHandler) ListConfigMaps(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.service.ListConfigMaps(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *configMapResourceHandler) GetConfigMapDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.service.GetConfigMapDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *configMapResourceHandler) UpdateConfigMapData(c *gin.Context) {
	var payload struct {
		Data       map[string]string `json:"data"`
		BinaryData map[string]string `json:"binaryData"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid configmap data payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.service.UpdateConfigMapData(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("name"), payload.Data, payload.BinaryData)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *configMapResourceHandler) ListConfigMapReferences(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.service.ListConfigMapReferences(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *secretResourceHandler) GetSecretDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.service.GetSecretDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *secretResourceHandler) UpdateSecretData(c *gin.Context) {
	var payload struct {
		Data map[string]string `json:"data"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid secret data payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.service.UpdateSecretData(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("name"), payload.Data)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *secretResourceHandler) ListSecretReferences(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.service.ListSecretReferences(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *configMapResourceHandler) CreateConfigMap(c *gin.Context) {
	createResourceFromYAML(c, h.creator, "ConfigMap")
}
func (h *secretResourceHandler) CreateSecret(c *gin.Context) {
	createResourceFromYAML(c, h.creator, "Secret")
}
func createResourceFromYAML(c *gin.Context, service ResourceCreator, kind string) {
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
	item, err := service.CreateResourceFromYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, kind, payload.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}
func (h *secretResourceHandler) ListSecrets(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.service.ListSecrets(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *configurationInventoryResourceHandler) ListPriorityClasses(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListPriorityClasses(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *configurationInventoryResourceHandler) ListRuntimeClasses(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListRuntimeClasses(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *configurationInventoryResourceHandler) ListMutatingWebhookConfigurations(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListMutatingWebhookConfigurations(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *configurationInventoryResourceHandler) ListValidatingWebhookConfigurations(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListValidatingWebhookConfigurations(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *configurationInventoryResourceHandler) ListResourceQuotas(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.service.ListResourceQuotas(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *configurationInventoryResourceHandler) ListLimitRanges(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.service.ListLimitRanges(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *configurationInventoryResourceHandler) ListLeases(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.service.ListLeases(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
