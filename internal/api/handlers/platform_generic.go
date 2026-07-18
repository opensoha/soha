package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
)

func (h *genericResourceHandler) genericResourceYAMLGetWithParam(kind, nameParam string) gin.HandlerFunc {
	return func(c *gin.Context) {
		principal := apiMiddleware.PrincipalFromContext(c)
		namespace := c.Query("namespace")
		item, err := h.service.GetResourceYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, kind, c.Param(nameParam))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	}
}
func (h *genericResourceHandler) genericResourceYAMLApplyWithParam(kind, nameParam string) gin.HandlerFunc {
	return func(c *gin.Context) {
		principal := apiMiddleware.PrincipalFromContext(c)
		namespace := c.Query("namespace")
		var payload struct {
			Content string `json:"content"`
		}
		if err := c.ShouldBindJSON(&payload); err != nil {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid yaml payload")
			return
		}
		item, err := h.service.ApplyResourceYAMLByKind(c.Request.Context(), principal, c.Param("clusterID"), namespace, kind, c.Param(nameParam), payload.Content)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	}
}
func (h *genericResourceHandler) genericResourceDeleteWithParam(kind, nameParam string) gin.HandlerFunc {
	return func(c *gin.Context) {
		principal := apiMiddleware.PrincipalFromContext(c)
		namespace := c.Query("namespace")
		if err := h.service.DeleteResourceByKind(c.Request.Context(), principal, c.Param("clusterID"), namespace, kind, c.Param(nameParam)); err != nil {
			writeError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// RegisterGenericResourceRoutes wires delete + yaml view/apply endpoints for
// platform resources backed by the generic dynamic-client path.
func (h *genericResourceHandler) RegisterGenericResourceRoutes(group gin.IRoutes) {
	kinds := []struct {
		path      string
		kind      string
		nameParam string
	}{
		{path: "/clusters/:clusterID/access-control/serviceaccounts/:name", kind: "ServiceAccount"},
		{path: "/clusters/:clusterID/access-control/roles/:name", kind: "Role"},
		{path: "/clusters/:clusterID/access-control/rolebindings/:name", kind: "RoleBinding"},
		{path: "/clusters/:clusterID/network/services/:serviceName", kind: "Service", nameParam: "serviceName"},
		{path: "/clusters/:clusterID/network/ingresses/:name", kind: "Ingress"},
		{path: "/clusters/:clusterID/network/endpointslices/:name", kind: "EndpointSlice"},
		{path: "/clusters/:clusterID/network/networkpolicies/:name", kind: "NetworkPolicy"},
		{path: "/clusters/:clusterID/network/ingressclasses/:name", kind: "IngressClass"},
		{path: "/clusters/:clusterID/network/gatewayclasses/:name", kind: "GatewayClass"},
		{path: "/clusters/:clusterID/network/gateways/:name", kind: "Gateway"},
		{path: "/clusters/:clusterID/configuration/priorityclasses/:name", kind: "PriorityClass"},
		{path: "/clusters/:clusterID/configuration/runtimeclasses/:name", kind: "RuntimeClass"},
		{path: "/clusters/:clusterID/access-control/clusterroles/:name", kind: "ClusterRole"},
		{path: "/clusters/:clusterID/access-control/clusterrolebindings/:name", kind: "ClusterRoleBinding"},
		{path: "/clusters/:clusterID/configuration/mutatingwebhookconfigurations/:name", kind: "MutatingWebhookConfiguration"},
		{path: "/clusters/:clusterID/configuration/validatingwebhookconfigurations/:name", kind: "ValidatingWebhookConfiguration"},
		{path: "/clusters/:clusterID/configuration/resourcequotas/:name", kind: "ResourceQuota"},
		{path: "/clusters/:clusterID/configuration/limitranges/:name", kind: "LimitRange"},
		{path: "/clusters/:clusterID/configuration/leases/:name", kind: "Lease"},
		{path: "/clusters/:clusterID/configuration/hpas/:name", kind: "HorizontalPodAutoscaler"},
		{path: "/clusters/:clusterID/configuration/poddisruptionbudgets/:name", kind: "PodDisruptionBudget"},
		{path: "/clusters/:clusterID/configuration/configmaps/:name", kind: "ConfigMap"},
		{path: "/clusters/:clusterID/configuration/secrets/:name", kind: "Secret"},
		{path: "/clusters/:clusterID/storage/persistentvolumeclaims/:name", kind: "PersistentVolumeClaim"},
		{path: "/clusters/:clusterID/storage/persistentvolumes/:name", kind: "PersistentVolume"},
		{path: "/clusters/:clusterID/storage/storageclasses/:name", kind: "StorageClass"},
	}
	for _, entry := range kinds {
		nameParam := entry.nameParam
		if nameParam == "" {
			nameParam = "name"
		}
		group.GET(entry.path+"/yaml", h.genericResourceYAMLGetWithParam(entry.kind, nameParam))
		group.PUT(entry.path+"/yaml", h.genericResourceYAMLApplyWithParam(entry.kind, nameParam))
		group.DELETE(entry.path, h.genericResourceDeleteWithParam(entry.kind, nameParam))
	}
}
