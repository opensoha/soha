package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func (h *clusterHandler) ListClusters(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListAccessible(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *clusterHandler) CreateCluster(c *gin.Context) {
	var req dto.CreateClusterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid cluster payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.Register(c.Request.Context(), principal, domaincluster.RegisterInput{
		ID:                     req.ID,
		Name:                   req.Name,
		Region:                 req.Region,
		Environment:            req.Environment,
		Labels:                 req.Labels,
		ConnectionMode:         domaincluster.ConnectionMode(req.ConnectionMode),
		Kubeconfig:             req.Kubeconfig,
		Context:                req.Context,
		AgentEndpoint:          req.AgentEndpoint,
		AgentToken:             req.AgentToken,
		PrometheusBaseURL:      req.PrometheusBaseURL,
		PrometheusBearerToken:  req.PrometheusBearerToken,
		PrometheusClusterLabel: req.PrometheusClusterLabel,
		GrafanaBaseURL:         req.GrafanaBaseURL,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}
func (h *clusterHandler) DescribeCluster(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.Describe(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *clusterHandler) ClusterCapabilityMatrix(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.CapabilityMatrix(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *clusterHandler) UpdateCluster(c *gin.Context) {
	var req dto.UpdateClusterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid cluster payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.Update(c.Request.Context(), principal, c.Param("clusterID"), domaincluster.UpdateInput{
		Name:                   req.Name,
		Region:                 req.Region,
		Environment:            req.Environment,
		Labels:                 req.Labels,
		ConnectionMode:         domaincluster.ConnectionMode(req.ConnectionMode),
		Kubeconfig:             req.Kubeconfig,
		Context:                req.Context,
		AgentEndpoint:          req.AgentEndpoint,
		AgentToken:             req.AgentToken,
		PrometheusBaseURL:      req.PrometheusBaseURL,
		PrometheusBearerToken:  req.PrometheusBearerToken,
		PrometheusClusterLabel: req.PrometheusClusterLabel,
		GrafanaBaseURL:         req.GrafanaBaseURL,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *clusterHandler) DeleteCluster(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.Delete(c.Request.Context(), principal, c.Param("clusterID")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
func (h *namespaceResourceHandler) ListNamespaces(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListNamespaces(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *namespaceResourceHandler) CreateNamespace(c *gin.Context) {
	var req dto.NamespaceUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid namespace payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateNamespace(c.Request.Context(), principal, c.Param("clusterID"), domainresource.NamespaceUpsertInput{
		Name:        req.Name,
		Labels:      req.Labels,
		Annotations: req.Annotations,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}
func (h *namespaceResourceHandler) UpdateNamespace(c *gin.Context) {
	var req dto.NamespaceUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid namespace payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateNamespace(c.Request.Context(), principal, c.Param("clusterID"), c.Param("namespaceName"), domainresource.NamespaceUpsertInput{
		Name:        req.Name,
		Labels:      req.Labels,
		Annotations: req.Annotations,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *namespaceResourceHandler) DeleteNamespace(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.service.DeleteNamespace(c.Request.Context(), principal, c.Param("clusterID"), c.Param("namespaceName")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
func (h *nodeResourceHandler) ListNodes(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.reader.ListNodes(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *nodeResourceHandler) GetNodeDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.reader.GetNodeDetail(c.Request.Context(), principal, c.Param("clusterID"), c.Param("nodeName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *nodeResourceHandler) GetNodeYAML(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.reader.GetNodeYAML(c.Request.Context(), principal, c.Param("clusterID"), c.Param("nodeName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *nodeResourceHandler) ApplyNodeYAML(c *gin.Context) {
	var req dto.ApplyResourceYAMLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid node yaml payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.editor.ApplyNodeYAML(c.Request.Context(), principal, c.Param("clusterID"), c.Param("nodeName"), req.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *nodeResourceHandler) UpdateNode(c *gin.Context) {
	var req dto.NodeUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid node payload")
		return
	}
	taints := make([]domainresource.NodeTaintView, 0, len(req.Taints))
	for _, taint := range req.Taints {
		taints = append(taints, domainresource.NodeTaintView{
			Key:    taint.Key,
			Value:  taint.Value,
			Effect: taint.Effect,
		})
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.editor.UpdateNode(c.Request.Context(), principal, c.Param("clusterID"), c.Param("nodeName"), domainresource.NodeUpdateInput{
		Labels: req.Labels,
		Taints: taints,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *nodeResourceHandler) DeleteNode(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.editor.DeleteNode(c.Request.Context(), principal, c.Param("clusterID"), c.Param("nodeName")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
