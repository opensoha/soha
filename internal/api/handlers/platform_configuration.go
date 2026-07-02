package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
)

func (h *PlatformHandler) ListConfigMaps(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListConfigMaps(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) GetConfigMapDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetConfigMapDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) UpdateConfigMapData(c *gin.Context) {
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
	item, err := h.resources.UpdateConfigMapData(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("name"), payload.Data, payload.BinaryData)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) ListConfigMapReferences(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListConfigMapReferences(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) GetSecretDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetSecretDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) UpdateSecretData(c *gin.Context) {
	var payload struct {
		Data map[string]string `json:"data"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid secret data payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.UpdateSecretData(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("name"), payload.Data)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) ListSecretReferences(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListSecretReferences(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) CreateConfigMap(c *gin.Context) {
	h.createResourceFromYAML(c, "ConfigMap")
}
func (h *PlatformHandler) CreateSecret(c *gin.Context) {
	h.createResourceFromYAML(c, "Secret")
}
func (h *PlatformHandler) createResourceFromYAML(c *gin.Context, kind string) {
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
	item, err := h.resources.CreateResourceFromYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, kind, payload.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}
func (h *PlatformHandler) ListSecrets(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListSecrets(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) ListIngressClasses(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.resources.ListIngressClasses(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) ListPriorityClasses(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.resources.ListPriorityClasses(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) ListRuntimeClasses(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.resources.ListRuntimeClasses(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) ListMutatingWebhookConfigurations(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.resources.ListMutatingWebhookConfigurations(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) ListValidatingWebhookConfigurations(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.resources.ListValidatingWebhookConfigurations(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) ListResourceQuotas(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListResourceQuotas(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) ListLimitRanges(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListLimitRanges(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) ListLeases(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListLeases(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) ListReplicationControllers(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListReplicationControllers(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
