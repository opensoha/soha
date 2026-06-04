package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/soha/soha/internal/api/dto"
	apiMiddleware "github.com/soha/soha/internal/api/middleware"
	apiresponse "github.com/soha/soha/internal/api/response"
	domaincluster "github.com/soha/soha/internal/domain/cluster"
	domainresource "github.com/soha/soha/internal/domain/resource"
)

func (h *PlatformHandler) ListClusters(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.clusters.ListAccessible(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) CreateCluster(c *gin.Context) {
	var req dto.CreateClusterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid cluster payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.clusters.Register(c.Request.Context(), principal, domaincluster.RegisterInput{
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
func (h *PlatformHandler) DescribeCluster(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.clusters.Describe(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) UpdateCluster(c *gin.Context) {
	var req dto.UpdateClusterRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid cluster payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.clusters.Update(c.Request.Context(), principal, c.Param("clusterID"), domaincluster.UpdateInput{
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
func (h *PlatformHandler) DeleteCluster(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.clusters.Delete(c.Request.Context(), principal, c.Param("clusterID")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
func (h *PlatformHandler) ListNamespaces(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.resources.ListNamespaces(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) CreateNamespace(c *gin.Context) {
	var req dto.NamespaceUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid namespace payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.resources.CreateNamespace(c.Request.Context(), principal, c.Param("clusterID"), domainresource.NamespaceUpsertInput{
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
func (h *PlatformHandler) UpdateNamespace(c *gin.Context) {
	var req dto.NamespaceUpsertRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid namespace payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.resources.UpdateNamespace(c.Request.Context(), principal, c.Param("clusterID"), c.Param("namespaceName"), domainresource.NamespaceUpsertInput{
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
func (h *PlatformHandler) DeleteNamespace(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.resources.DeleteNamespace(c.Request.Context(), principal, c.Param("clusterID"), c.Param("namespaceName")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
func (h *PlatformHandler) ListNodes(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.resources.ListNodes(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}
func (h *PlatformHandler) GetNodeDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.resources.GetNodeDetail(c.Request.Context(), principal, c.Param("clusterID"), c.Param("nodeName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) GetNodeYAML(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.resources.GetNodeYAML(c.Request.Context(), principal, c.Param("clusterID"), c.Param("nodeName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) ApplyNodeYAML(c *gin.Context) {
	var req dto.ApplyResourceYAMLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid node yaml payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.resources.ApplyNodeYAML(c.Request.Context(), principal, c.Param("clusterID"), c.Param("nodeName"), req.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) UpdateNode(c *gin.Context) {
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
	item, err := h.resources.UpdateNode(c.Request.Context(), principal, c.Param("clusterID"), c.Param("nodeName"), domainresource.NodeUpdateInput{
		Labels: req.Labels,
		Taints: taints,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}
func (h *PlatformHandler) DeleteNode(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.resources.DeleteNode(c.Request.Context(), principal, c.Param("clusterID"), c.Param("nodeName")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}
