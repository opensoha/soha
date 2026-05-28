package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	"github.com/soha/soha/internal/api/dto"
	aperrors "github.com/soha/soha/internal/api/errors"
	apiMiddleware "github.com/soha/soha/internal/api/middleware"
	apiresponse "github.com/soha/soha/internal/api/response"
	domainaudit "github.com/soha/soha/internal/domain/audit"
	domaincluster "github.com/soha/soha/internal/domain/cluster"
	domainevent "github.com/soha/soha/internal/domain/event"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainmcp "github.com/soha/soha/internal/domain/mcp"
	domainoperation "github.com/soha/soha/internal/domain/operation"
	domainresource "github.com/soha/soha/internal/domain/resource"
	"github.com/soha/soha/internal/platform/apperrors"
	"github.com/soha/soha/internal/platform/runtimeobs"
	"k8s.io/client-go/tools/remotecommand"
)

type ClusterService interface {
	List(context.Context) ([]domaincluster.Summary, error)
	ListAccessible(context.Context, domainidentity.Principal) ([]domaincluster.Summary, error)
	Describe(context.Context, domainidentity.Principal, string) (domaincluster.Detail, error)
	Register(context.Context, domainidentity.Principal, domaincluster.RegisterInput) (domaincluster.Summary, error)
	Update(context.Context, domainidentity.Principal, string, domaincluster.UpdateInput) (domaincluster.Summary, error)
	Delete(context.Context, domainidentity.Principal, string) error
}

type ResourceService interface {
	ListNamespaces(context.Context, domainidentity.Principal, string) ([]domainresource.NamespaceView, error)
	CreateNamespace(context.Context, domainidentity.Principal, string, domainresource.NamespaceUpsertInput) (domainresource.NamespaceView, error)
	UpdateNamespace(context.Context, domainidentity.Principal, string, string, domainresource.NamespaceUpsertInput) (domainresource.NamespaceView, error)
	DeleteNamespace(context.Context, domainidentity.Principal, string, string) error
	ListNodes(context.Context, domainidentity.Principal, string) ([]domainresource.NodeView, error)
	GetNodeDetail(context.Context, domainidentity.Principal, string, string) (domainresource.NodeDetailView, error)
	GetNodeYAML(context.Context, domainidentity.Principal, string, string) (domainresource.ResourceYAMLView, error)
	ApplyNodeYAML(context.Context, domainidentity.Principal, string, string, string) (domainresource.ResourceYAMLView, error)
	UpdateNode(context.Context, domainidentity.Principal, string, string, domainresource.NodeUpdateInput) (domainresource.NodeDetailView, error)
	DeleteNode(context.Context, domainidentity.Principal, string, string) error
	ListPods(context.Context, domainidentity.Principal, string, string) ([]domainresource.PodView, error)
	GetWorkloadOverview(context.Context, domainidentity.Principal, string, string) (domainresource.WorkloadOverviewView, error)
	GetPodDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.PodDetailView, error)
	DeletePod(context.Context, domainidentity.Principal, string, string, string) error
	GetPodLogs(context.Context, domainidentity.Principal, string, string, string, string, int64, int64, bool) (domainresource.PodLogsView, error)
	StreamPodLogs(context.Context, domainidentity.Principal, string, string, string, string, int64, int64, io.Writer) error
	GetPodYAML(context.Context, domainidentity.Principal, string, string, string) (domainresource.ResourceYAMLView, error)
	ApplyPodYAML(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.ResourceYAMLView, error)
	GetPodMetrics(context.Context, domainidentity.Principal, string, string, string, int, int) (domainresource.PodMetricsView, error)
	ExecPod(context.Context, domainidentity.Principal, string, string, string, string, string, int64) (domainresource.PodExecView, error)
	StreamPodTerminal(context.Context, domainidentity.Principal, string, string, string, string, string, io.Reader, io.Writer, io.Writer, remotecommand.TerminalSizeQueue) error
	ListDeployments(context.Context, domainidentity.Principal, string, string) ([]domainresource.DeploymentView, error)
	GetDeploymentDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.DeploymentDetailView, error)
	GetDeploymentYAML(context.Context, domainidentity.Principal, string, string, string) (domainresource.ResourceYAMLView, error)
	ApplyDeploymentYAML(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.ResourceYAMLView, error)
	GetDeploymentMetrics(context.Context, domainidentity.Principal, string, string, string, int, int) (domainresource.ResourceMetricsView, error)
	GetDeploymentRolloutStatus(context.Context, domainidentity.Principal, string, string, string) (domainresource.DeploymentRolloutStatusView, error)
	ListDeploymentRolloutHistory(context.Context, domainidentity.Principal, string, string, string) ([]domainresource.RolloutHistoryView, error)
	ListStatefulSets(context.Context, domainidentity.Principal, string, string) ([]domainresource.StatefulSetView, error)
	GetStatefulSetDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.StatefulSetDetailView, error)
	GetStatefulSetYAML(context.Context, domainidentity.Principal, string, string, string) (domainresource.ResourceYAMLView, error)
	ApplyStatefulSetYAML(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.ResourceYAMLView, error)
	ListDaemonSets(context.Context, domainidentity.Principal, string, string) ([]domainresource.DaemonSetView, error)
	GetDaemonSetDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.DaemonSetDetailView, error)
	GetDaemonSetYAML(context.Context, domainidentity.Principal, string, string, string) (domainresource.ResourceYAMLView, error)
	ApplyDaemonSetYAML(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.ResourceYAMLView, error)
	ListJobs(context.Context, domainidentity.Principal, string, string) ([]domainresource.JobView, error)
	GetJobDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.JobDetailView, error)
	GetJobYAML(context.Context, domainidentity.Principal, string, string, string) (domainresource.ResourceYAMLView, error)
	ApplyJobYAML(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.ResourceYAMLView, error)
	ListCronJobs(context.Context, domainidentity.Principal, string, string) ([]domainresource.CronJobView, error)
	ListReplicaSets(context.Context, domainidentity.Principal, string, string) ([]domainresource.ReplicaSetView, error)
	ListConfigMaps(context.Context, domainidentity.Principal, string, string) ([]domainresource.ConfigMapView, error)
	GetConfigMapDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.ConfigMapDetailView, error)
	ListSecrets(context.Context, domainidentity.Principal, string, string) ([]domainresource.SecretView, error)
	GetSecretDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.SecretDetailView, error)
	CreateResourceFromYAML(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.ResourceYAMLView, error)
	ListServiceAccounts(context.Context, domainidentity.Principal, string, string) ([]domainresource.ServiceAccountView, error)
	GetServiceAccountDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.ServiceAccountDetailView, error)
	ListRoles(context.Context, domainidentity.Principal, string, string) ([]domainresource.RoleView, error)
	GetRoleDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.RoleDetailView, error)
	ListRoleBindings(context.Context, domainidentity.Principal, string, string) ([]domainresource.RoleBindingView, error)
	GetRoleBindingDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.RoleBindingDetailView, error)
	ListHorizontalPodAutoscalers(context.Context, domainidentity.Principal, string, string) ([]domainresource.HorizontalPodAutoscalerView, error)
	ListPodDisruptionBudgets(context.Context, domainidentity.Principal, string, string) ([]domainresource.PodDisruptionBudgetView, error)
	GetCronJobDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.CronJobDetailView, error)
	GetCronJobYAML(context.Context, domainidentity.Principal, string, string, string) (domainresource.ResourceYAMLView, error)
	ApplyCronJobYAML(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.ResourceYAMLView, error)
	ListServices(context.Context, domainidentity.Principal, string, string) ([]domainresource.ServiceView, error)
	GetServiceMetrics(context.Context, domainidentity.Principal, string, string, string, int, int) (domainresource.ResourceMetricsView, error)
	ListIngresses(context.Context, domainidentity.Principal, string, string) ([]domainresource.IngressView, error)
	ListEndpointSlices(context.Context, domainidentity.Principal, string, string) ([]domainresource.EndpointSliceView, error)
	ListNetworkPolicies(context.Context, domainidentity.Principal, string, string) ([]domainresource.NetworkPolicyView, error)
	ListGatewayClasses(context.Context, domainidentity.Principal, string) ([]domainresource.GatewayClassView, error)
	ListGateways(context.Context, domainidentity.Principal, string, string) ([]domainresource.GatewayView, error)
	ListHTTPRoutes(context.Context, domainidentity.Principal, string, string) ([]domainresource.HTTPRouteView, error)
	ListBackendTLSPolicies(context.Context, domainidentity.Principal, string, string) ([]domainresource.BackendTLSPolicyView, error)
	ListGRPCRoutes(context.Context, domainidentity.Principal, string, string) ([]domainresource.GRPCRouteView, error)
	ListReferenceGrants(context.Context, domainidentity.Principal, string, string) ([]domainresource.ReferenceGrantView, error)
	ListPersistentVolumeClaims(context.Context, domainidentity.Principal, string, string) ([]domainresource.PersistentVolumeClaimView, error)
	GetPersistentVolumeClaimDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.PersistentVolumeClaimDetailView, error)
	ListPersistentVolumes(context.Context, domainidentity.Principal, string) ([]domainresource.PersistentVolumeView, error)
	GetPersistentVolumeDetail(context.Context, domainidentity.Principal, string, string) (domainresource.PersistentVolumeDetailView, error)
	ListStorageClasses(context.Context, domainidentity.Principal, string) ([]domainresource.StorageClassView, error)
	GetStorageClassDetail(context.Context, domainidentity.Principal, string, string) (domainresource.StorageClassDetailView, error)
	ListIngressClasses(context.Context, domainidentity.Principal, string) ([]domainresource.IngressClassView, error)
	ListPriorityClasses(context.Context, domainidentity.Principal, string) ([]domainresource.PriorityClassView, error)
	ListRuntimeClasses(context.Context, domainidentity.Principal, string) ([]domainresource.RuntimeClassView, error)
	ListClusterRoles(context.Context, domainidentity.Principal, string) ([]domainresource.ClusterRoleView, error)
	GetClusterRoleDetail(context.Context, domainidentity.Principal, string, string) (domainresource.ClusterRoleDetailView, error)
	ListClusterRoleBindings(context.Context, domainidentity.Principal, string) ([]domainresource.ClusterRoleBindingView, error)
	GetClusterRoleBindingDetail(context.Context, domainidentity.Principal, string, string) (domainresource.ClusterRoleBindingDetailView, error)
	ListMutatingWebhookConfigurations(context.Context, domainidentity.Principal, string) ([]domainresource.MutatingWebhookConfigurationView, error)
	ListValidatingWebhookConfigurations(context.Context, domainidentity.Principal, string) ([]domainresource.ValidatingWebhookConfigurationView, error)
	ListResourceQuotas(context.Context, domainidentity.Principal, string, string) ([]domainresource.ResourceQuotaView, error)
	ListLimitRanges(context.Context, domainidentity.Principal, string, string) ([]domainresource.LimitRangeView, error)
	ListLeases(context.Context, domainidentity.Principal, string, string) ([]domainresource.LeaseView, error)
	ListReplicationControllers(context.Context, domainidentity.Principal, string, string) ([]domainresource.ReplicationControllerView, error)
	GetResourceYAML(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.ResourceYAMLView, error)
	ApplyResourceYAMLByKind(context.Context, domainidentity.Principal, string, string, string, string, string) (domainresource.ResourceYAMLView, error)
	DeleteResourceByKind(context.Context, domainidentity.Principal, string, string, string, string) error
	ListPortForwards(context.Context, domainidentity.Principal, string) ([]domainresource.PortForwardSessionView, error)
	RegisterPortForward(context.Context, domainidentity.Principal, string, domainresource.PortForwardRegisterInput) (domainresource.PortForwardSessionView, error)
	StopPortForward(context.Context, domainidentity.Principal, string, string) error
	ListCRDs(context.Context, domainidentity.Principal, string) ([]domainresource.CRDView, error)
	ListCRDResources(context.Context, domainidentity.Principal, string, string, string) ([]domainresource.CustomResourceView, error)
	CreateCRDResourceFromYAML(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.ResourceYAMLView, error)
	GetCRDResourceYAML(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.ResourceYAMLView, error)
	ApplyCRDResourceYAML(context.Context, domainidentity.Principal, string, string, string, string, string) (domainresource.ResourceYAMLView, error)
	DeleteCRDResource(context.Context, domainidentity.Principal, string, string, string, string) error
	ListHelmReleases(context.Context, domainidentity.Principal, string, string) ([]domainresource.HelmReleaseView, error)
	GetHelmReleaseDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.HelmReleaseDetailView, error)
	ListHelmReleaseHistory(context.Context, domainidentity.Principal, string, string, string) ([]domainresource.HelmReleaseHistoryView, error)
	GetHelmReleaseValues(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.HelmValuesView, error)
	ListClusterEvents(context.Context, domainidentity.Principal, string, string, int) ([]domainresource.ClusterEventView, error)
	RestartDeployment(context.Context, domainidentity.Principal, string, string, string) error
	RollbackDeployment(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.DeploymentRollbackView, error)
	ScaleDeployment(context.Context, domainidentity.Principal, string, string, string, int32) error
}

type AuditService interface {
	ListAuthorized(context.Context, domainidentity.Principal, domainaudit.Filter) ([]domainaudit.Entry, error)
}

type EventService interface {
	List(context.Context, int) ([]domainevent.Envelope, error)
	Get(context.Context, string) (domainevent.Envelope, error)
}

type OperationService interface {
	ListAuthorized(context.Context, domainidentity.Principal, domainoperation.Filter) ([]domainoperation.Entry, error)
}

type IntegrationService interface {
	ListCapabilities(context.Context) ([]domainmcp.Capability, error)
}

type PlatformHandler struct {
	clusters    ClusterService
	resources   ResourceService
	audit       AuditService
	events      EventService
	operations  OperationService
	integration IntegrationService
}

var podTerminalUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

const (
	podLogPingInterval   = 20 * time.Second
	podLogPongWait       = 45 * time.Second
	podLogReconnectDelay = 1200 * time.Millisecond
)

func NewPlatformHandler(clusters ClusterService, resources ResourceService, audit AuditService, events EventService, operations OperationService, integration IntegrationService) *PlatformHandler {
	return &PlatformHandler{
		clusters:    clusters,
		resources:   resources,
		audit:       audit,
		events:      events,
		operations:  operations,
		integration: integration,
	}
}

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

func (h *PlatformHandler) ListPods(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListPods(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) GetWorkloadOverview(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetWorkloadOverview(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) GetPodDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.resources.GetPodDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("podName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) DeletePod(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	if err := h.resources.DeletePod(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("podName")); err != nil {
		writeError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *PlatformHandler) GetPodLogs(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	tailLines := int64(parseLimit(c.Query("tailLines"), 200))
	sinceSeconds := int64(parseLimit(c.Query("sinceSeconds"), 0))
	previous := strings.EqualFold(c.Query("previous"), "true")
	item, err := h.resources.GetPodLogs(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("podName"), c.Query("container"), tailLines, sinceSeconds, previous)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) StreamPodLogs(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	container := c.Query("container")
	tailLines := int64(parseLimit(c.Query("tailLines"), 200))
	sinceSeconds := int64(parseLimit(c.Query("sinceSeconds"), 0))

	conn, err := podTerminalUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	_ = conn.SetReadDeadline(time.Now().Add(podLogPongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(podLogPongWait))
	})

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	var writeMu sync.Mutex
	go func() {
		ticker := time.NewTicker(podLogPingInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := writeControlMessage(conn, &writeMu, websocket.PingMessage, nil); err != nil {
					cancel()
					return
				}
			}
		}
	}()

	streamErrCh := make(chan error, 1)
	go func() {
		streamErrCh <- h.streamPodLogsWithReconnect(
			ctx,
			principal,
			c.Param("clusterID"),
			namespace,
			c.Param("podName"),
			container,
			tailLines,
			sinceSeconds,
			&logStreamWriter{conn: conn, writeMu: &writeMu},
		)
	}()

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		for {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}
			var message terminalMessage
			if err := json.Unmarshal(payload, &message); err != nil {
				continue
			}
			if message.Type == "close" {
				cancel()
				return
			}
		}
	}()

	select {
	case err := <-streamErrCh:
		exitMessage := terminalMessage{Type: "exit", Message: "log stream closed"}
		if err != nil && !errors.Is(err, context.Canceled) {
			exitMessage.Message = err.Error()
		}
		_ = writeTerminalMessage(conn, &writeMu, exitMessage)
	case <-readDone:
		_ = writeTerminalMessage(conn, &writeMu, terminalMessage{Type: "exit", Message: "log stream closed"})
	}
}

func (h *PlatformHandler) streamPodLogsWithReconnect(
	ctx context.Context,
	principal domainidentity.Principal,
	clusterID, namespace, podName, container string,
	tailLines, sinceSeconds int64,
	writer *logStreamWriter,
) error {
	currentTailLines := tailLines
	connectedOnce := false
	for {
		err := h.resources.StreamPodLogs(
			ctx,
			principal,
			clusterID,
			namespace,
			podName,
			container,
			currentTailLines,
			sinceSeconds,
			writer,
		)
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return err
		}
		if !connectedOnce && err != nil {
			return err
		}
		connectedOnce = true
		currentTailLines = 0
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(podLogReconnectDelay):
		}
	}
}

func (h *PlatformHandler) GetPodYAML(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.resources.GetPodYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("podName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) ApplyPodYAML(c *gin.Context) {
	var req dto.ApplyResourceYAMLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid pod yaml payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.resources.ApplyPodYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("podName"), req.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) GetPodMetrics(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	rangeMinutes := parseLimit(c.Query("rangeMinutes"), 60)
	stepSeconds := parseLimit(c.Query("stepSeconds"), 60)
	item, err := h.resources.GetPodMetrics(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("podName"), rangeMinutes, stepSeconds)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) ExecPod(c *gin.Context) {
	var req dto.ExecPodRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Command == "" {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "command is required")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.resources.ExecPod(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("podName"), req.Container, req.Command, req.TimeoutSeconds)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) StreamPodTerminal(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	container := c.Query("container")
	shell := c.DefaultQuery("shell", "/bin/sh")

	conn, err := podTerminalUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()

	stdinReader, stdinWriter := io.Pipe()
	defer stdinWriter.Close()
	sizeQueue := newTerminalSizeQueue()

	var writeMu sync.Mutex
	_ = writeTerminalMessage(conn, &writeMu, terminalMessage{
		Type:    "status",
		Message: "terminal session connected",
	})

	streamErrCh := make(chan error, 1)
	go func() {
		streamErrCh <- h.resources.StreamPodTerminal(
			ctx,
			principal,
			c.Param("clusterID"),
			namespace,
			c.Param("podName"),
			container,
			shell,
			stdinReader,
			terminalStreamWriter{conn: conn, writeMu: &writeMu, channel: "stdout"},
			terminalStreamWriter{conn: conn, writeMu: &writeMu, channel: "stderr"},
			sizeQueue,
		)
	}()

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		defer stdinWriter.Close()
		for {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				cancel()
				return
			}
			var message terminalMessage
			if err := json.Unmarshal(payload, &message); err != nil {
				_ = writeTerminalMessage(conn, &writeMu, terminalMessage{Type: "status", Message: "ignored invalid terminal message"})
				continue
			}
			switch message.Type {
			case "input":
				if _, err := io.WriteString(stdinWriter, message.Data); err != nil {
					cancel()
					return
				}
			case "resize":
				sizeQueue.Push(message.Cols, message.Rows)
			case "close":
				cancel()
				return
			}
		}
	}()

	streamErr := <-streamErrCh
	cancel()
	<-readDone

	exitMessage := terminalMessage{Type: "exit", Message: "terminal session closed"}
	if streamErr != nil && streamErr != context.Canceled {
		exitMessage.Message = streamErr.Error()
	}
	_ = writeTerminalMessage(conn, &writeMu, exitMessage)
}

func (h *PlatformHandler) ListDeployments(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListDeployments(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) GetDeploymentDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.resources.GetDeploymentDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("deploymentName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) GetDeploymentYAML(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.resources.GetDeploymentYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("deploymentName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) ApplyDeploymentYAML(c *gin.Context) {
	var req dto.ApplyResourceYAMLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid deployment yaml payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.resources.ApplyDeploymentYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("deploymentName"), req.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) GetDeploymentMetrics(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	rangeMinutes := parseLimit(c.Query("rangeMinutes"), 60)
	stepSeconds := parseLimit(c.Query("stepSeconds"), 60)
	item, err := h.resources.GetDeploymentMetrics(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("deploymentName"), rangeMinutes, stepSeconds)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) GetDeploymentRolloutStatus(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	item, err := h.resources.GetDeploymentRolloutStatus(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("deploymentName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) ListDeploymentRollouts(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.DefaultQuery("namespace", "default")
	items, err := h.resources.ListDeploymentRolloutHistory(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("deploymentName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) ListStatefulSets(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListStatefulSets(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) GetStatefulSetDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetStatefulSetDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("statefulSetName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) GetStatefulSetYAML(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetStatefulSetYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("statefulSetName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) ApplyStatefulSetYAML(c *gin.Context) {
	var req dto.ApplyResourceYAMLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid statefulset yaml payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.ApplyStatefulSetYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("statefulSetName"), req.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) ListDaemonSets(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListDaemonSets(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) GetDaemonSetDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetDaemonSetDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("daemonSetName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) GetDaemonSetYAML(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetDaemonSetYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("daemonSetName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) ApplyDaemonSetYAML(c *gin.Context) {
	var req dto.ApplyResourceYAMLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid daemonset yaml payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.ApplyDaemonSetYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("daemonSetName"), req.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) ListJobs(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListJobs(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) GetJobDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetJobDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("jobName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) GetJobYAML(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetJobYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("jobName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) ApplyJobYAML(c *gin.Context) {
	var req dto.ApplyResourceYAMLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid job yaml payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.ApplyJobYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("jobName"), req.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) ListCronJobs(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListCronJobs(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) GetCronJobDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetCronJobDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("cronJobName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) GetCronJobYAML(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetCronJobYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("cronJobName"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) ApplyCronJobYAML(c *gin.Context) {
	var req dto.ApplyResourceYAMLRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid cronjob yaml payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.ApplyCronJobYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("cronJobName"), req.Content)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) ListReplicaSets(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListReplicaSets(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

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

func (h *PlatformHandler) ListServiceAccounts(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListServiceAccounts(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) GetServiceAccountDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetServiceAccountDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) CreateServiceAccount(c *gin.Context) {
	h.createResourceFromYAML(c, "ServiceAccount")
}

func (h *PlatformHandler) ListRoles(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListRoles(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) GetRoleDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetRoleDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) CreateRole(c *gin.Context) {
	h.createResourceFromYAML(c, "Role")
}

func (h *PlatformHandler) ListRoleBindings(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListRoleBindings(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) GetRoleBindingDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetRoleBindingDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) CreateRoleBinding(c *gin.Context) {
	h.createResourceFromYAML(c, "RoleBinding")
}

func (h *PlatformHandler) ListHorizontalPodAutoscalers(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListHorizontalPodAutoscalers(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) ListPodDisruptionBudgets(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListPodDisruptionBudgets(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

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

func (h *PlatformHandler) ListHTTPRoutes(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListHTTPRoutes(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) ListBackendTLSPolicies(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListBackendTLSPolicies(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) ListGRPCRoutes(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListGRPCRoutes(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) ListReferenceGrants(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListReferenceGrants(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) ListPersistentVolumeClaims(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	items, err := h.resources.ListPersistentVolumeClaims(c.Request.Context(), principal, c.Param("clusterID"), namespace)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) GetPersistentVolumeClaimDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	item, err := h.resources.GetPersistentVolumeClaimDetail(c.Request.Context(), principal, c.Param("clusterID"), namespace, c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) CreatePersistentVolumeClaim(c *gin.Context) {
	h.createResourceFromYAML(c, "PersistentVolumeClaim")
}

func (h *PlatformHandler) ListPersistentVolumes(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.resources.ListPersistentVolumes(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) GetPersistentVolumeDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.resources.GetPersistentVolumeDetail(c.Request.Context(), principal, c.Param("clusterID"), c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) CreatePersistentVolume(c *gin.Context) {
	h.createResourceFromYAML(c, "PersistentVolume")
}

func (h *PlatformHandler) ListStorageClasses(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.resources.ListStorageClasses(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) GetStorageClassDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.resources.GetStorageClassDetail(c.Request.Context(), principal, c.Param("clusterID"), c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) CreateStorageClass(c *gin.Context) {
	h.createResourceFromYAML(c, "StorageClass")
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

func (h *PlatformHandler) ListClusterRoles(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.resources.ListClusterRoles(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) GetClusterRoleDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.resources.GetClusterRoleDetail(c.Request.Context(), principal, c.Param("clusterID"), c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) CreateClusterRole(c *gin.Context) {
	h.createResourceFromYAML(c, "ClusterRole")
}

func (h *PlatformHandler) ListClusterRoleBindings(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.resources.ListClusterRoleBindings(c.Request.Context(), principal, c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) GetClusterRoleBindingDetail(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.resources.GetClusterRoleBindingDetail(c.Request.Context(), principal, c.Param("clusterID"), c.Param("name"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) CreateClusterRoleBinding(c *gin.Context) {
	h.createResourceFromYAML(c, "ClusterRoleBinding")
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

func (h *PlatformHandler) genericResourceYAMLGet(kind string) gin.HandlerFunc {
	return h.genericResourceYAMLGetWithParam(kind, "name")
}

func (h *PlatformHandler) genericResourceYAMLGetWithParam(kind, nameParam string) gin.HandlerFunc {
	return func(c *gin.Context) {
		principal := apiMiddleware.PrincipalFromContext(c)
		namespace := c.Query("namespace")
		item, err := h.resources.GetResourceYAML(c.Request.Context(), principal, c.Param("clusterID"), namespace, kind, c.Param(nameParam))
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	}
}

func (h *PlatformHandler) genericResourceYAMLApply(kind string) gin.HandlerFunc {
	return h.genericResourceYAMLApplyWithParam(kind, "name")
}

func (h *PlatformHandler) genericResourceYAMLApplyWithParam(kind, nameParam string) gin.HandlerFunc {
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
		item, err := h.resources.ApplyResourceYAMLByKind(c.Request.Context(), principal, c.Param("clusterID"), namespace, kind, c.Param(nameParam), payload.Content)
		if err != nil {
			writeError(c, err)
			return
		}
		apiresponse.Item(c, http.StatusOK, item)
	}
}

func (h *PlatformHandler) genericResourceDelete(kind string) gin.HandlerFunc {
	return h.genericResourceDeleteWithParam(kind, "name")
}

func (h *PlatformHandler) genericResourceDeleteWithParam(kind, nameParam string) gin.HandlerFunc {
	return func(c *gin.Context) {
		principal := apiMiddleware.PrincipalFromContext(c)
		namespace := c.Query("namespace")
		if err := h.resources.DeleteResourceByKind(c.Request.Context(), principal, c.Param("clusterID"), namespace, kind, c.Param(nameParam)); err != nil {
			writeError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	}
}

// RegisterGenericResourceRoutes wires delete + yaml view/apply endpoints for
// platform resources backed by the generic dynamic-client path.
func (h *PlatformHandler) RegisterGenericResourceRoutes(group gin.IRoutes) {
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
		{path: "/clusters/:clusterID/network/httproutes/:name", kind: "HTTPRoute"},
		{path: "/clusters/:clusterID/network/backendtlspolicies/:name", kind: "BackendTLSPolicy"},
		{path: "/clusters/:clusterID/network/grpcroutes/:name", kind: "GRPCRoute"},
		{path: "/clusters/:clusterID/network/referencegrants/:name", kind: "ReferenceGrant"},
		{path: "/clusters/:clusterID/configuration/priorityclasses/:name", kind: "PriorityClass"},
		{path: "/clusters/:clusterID/configuration/runtimeclasses/:name", kind: "RuntimeClass"},
		{path: "/clusters/:clusterID/access-control/clusterroles/:name", kind: "ClusterRole"},
		{path: "/clusters/:clusterID/access-control/clusterrolebindings/:name", kind: "ClusterRoleBinding"},
		{path: "/clusters/:clusterID/configuration/mutatingwebhookconfigurations/:name", kind: "MutatingWebhookConfiguration"},
		{path: "/clusters/:clusterID/configuration/validatingwebhookconfigurations/:name", kind: "ValidatingWebhookConfiguration"},
		{path: "/clusters/:clusterID/configuration/resourcequotas/:name", kind: "ResourceQuota"},
		{path: "/clusters/:clusterID/configuration/limitranges/:name", kind: "LimitRange"},
		{path: "/clusters/:clusterID/configuration/leases/:name", kind: "Lease"},
		{path: "/clusters/:clusterID/workloads/replicationcontrollers/:name", kind: "ReplicationController"},
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

// RegisterWorkloadDeleteRoutes wires DELETE endpoints for built-in workloads using
// the existing route param names (`:deploymentName`, etc.) to avoid route conflicts
// with other GET/PUT entries already registered in router.go.
func (h *PlatformHandler) RegisterWorkloadDeleteRoutes(group gin.IRoutes) {
	entries := []struct {
		path  string
		param string
		kind  string
	}{
		{"/clusters/:clusterID/workloads/deployments/:deploymentName", "deploymentName", "Deployment"},
		{"/clusters/:clusterID/workloads/statefulsets/:statefulSetName", "statefulSetName", "StatefulSet"},
		{"/clusters/:clusterID/workloads/daemonsets/:daemonSetName", "daemonSetName", "DaemonSet"},
		{"/clusters/:clusterID/workloads/jobs/:jobName", "jobName", "Job"},
		{"/clusters/:clusterID/workloads/cronjobs/:cronJobName", "cronJobName", "CronJob"},
	}
	for _, entry := range entries {
		kind := entry.kind
		paramName := entry.param
		group.DELETE(entry.path, func(c *gin.Context) {
			principal := apiMiddleware.PrincipalFromContext(c)
			namespace := c.Query("namespace")
			if err := h.resources.DeleteResourceByKind(c.Request.Context(), principal, c.Param("clusterID"), namespace, kind, c.Param(paramName)); err != nil {
				writeError(c, err)
				return
			}
			c.Status(http.StatusNoContent)
		})
	}
}

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

func (h *PlatformHandler) ListClusterEvents(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	namespace := c.Query("namespace")
	limit := parseLimit(c.Query("limit"), 20)
	items, err := h.resources.ListClusterEvents(c.Request.Context(), principal, c.Param("clusterID"), namespace, limit)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) RestartDeployment(c *gin.Context) {
	var req dto.RestartDeploymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid restart deployment payload")
		return
	}
	if req.Namespace == "" || req.Name == "" {
		writeError(c, fmt.Errorf("%w: namespace and name are required", apperrors.ErrInvalidArgument))
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.resources.RestartDeployment(c.Request.Context(), principal, c.Param("clusterID"), req.Namespace, req.Name); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *PlatformHandler) ScaleDeployment(c *gin.Context) {
	var req dto.ScaleDeploymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid scale deployment payload")
		return
	}
	if req.Namespace == "" || req.Name == "" {
		writeError(c, fmt.Errorf("%w: namespace and name are required", apperrors.ErrInvalidArgument))
		return
	}
	if req.Replicas < 0 {
		writeError(c, fmt.Errorf("%w: replicas must be greater than or equal to zero", apperrors.ErrInvalidArgument))
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	if err := h.resources.ScaleDeployment(c.Request.Context(), principal, c.Param("clusterID"), req.Namespace, req.Name, req.Replicas); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ok"})
}

func (h *PlatformHandler) RollbackDeployment(c *gin.Context) {
	var req dto.RollbackDeploymentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid rollback deployment payload")
		return
	}
	if req.Namespace == "" || req.Name == "" || req.Revision == "" {
		writeError(c, fmt.Errorf("%w: namespace, name, and revision are required", apperrors.ErrInvalidArgument))
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.resources.RollbackDeployment(c.Request.Context(), principal, c.Param("clusterID"), req.Namespace, req.Name, req.Revision)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) ListAuditLogs(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	limit := parseLimit(c.Query("limit"), 50)
	items, err := h.audit.ListAuthorized(c.Request.Context(), principal, domainaudit.Filter{
		Action: c.Query("action"),
		Result: c.Query("result"),
		Limit:  limit,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

type terminalMessage struct {
	Type    string `json:"type"`
	Data    string `json:"data,omitempty"`
	Message string `json:"message,omitempty"`
	Cols    int    `json:"cols,omitempty"`
	Rows    int    `json:"rows,omitempty"`
}

type terminalStreamWriter struct {
	conn    *websocket.Conn
	writeMu *sync.Mutex
	channel string
}

type logStreamWriter struct {
	conn          *websocket.Conn
	writeMu       *sync.Mutex
	pendingBuffer string
}

func (w terminalStreamWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if err := writeTerminalMessage(w.conn, w.writeMu, terminalMessage{
		Type: w.channel,
		Data: string(p),
	}); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (w *logStreamWriter) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	data := w.pendingBuffer + string(p)
	lines := strings.Split(data, "\n")
	for _, line := range lines[:len(lines)-1] {
		if err := writeTerminalMessage(w.conn, w.writeMu, terminalMessage{
			Type: "log",
			Data: line,
		}); err != nil {
			return 0, err
		}
	}
	last := lines[len(lines)-1]
	if strings.HasSuffix(data, "\n") {
		if last != "" {
			if err := writeTerminalMessage(w.conn, w.writeMu, terminalMessage{Type: "log", Data: last}); err != nil {
				return 0, err
			}
		}
		w.pendingBuffer = ""
	} else {
		w.pendingBuffer = last
	}
	return len(p), nil
}

type terminalSizeQueue struct {
	ch chan remotecommand.TerminalSize
}

func newTerminalSizeQueue() *terminalSizeQueue {
	return &terminalSizeQueue{ch: make(chan remotecommand.TerminalSize, 1)}
}

func (q *terminalSizeQueue) Next() *remotecommand.TerminalSize {
	size, ok := <-q.ch
	if !ok {
		return nil
	}
	return &size
}

func (q *terminalSizeQueue) Push(cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	size := remotecommand.TerminalSize{Width: uint16(cols), Height: uint16(rows)}
	select {
	case q.ch <- size:
	default:
		select {
		case <-q.ch:
		default:
		}
		q.ch <- size
	}
}

func writeTerminalMessage(conn *websocket.Conn, writeMu *sync.Mutex, message terminalMessage) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	return conn.WriteJSON(message)
}

func writeControlMessage(conn *websocket.Conn, writeMu *sync.Mutex, messageType int, data []byte) error {
	writeMu.Lock()
	defer writeMu.Unlock()
	return conn.WriteControl(messageType, data, time.Now().Add(5*time.Second))
}

func (h *PlatformHandler) ListEvents(c *gin.Context) {
	limit := parseLimit(c.Query("limit"), 50)
	items, err := h.events.List(c.Request.Context(), limit)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) GetEvent(c *gin.Context) {
	item, err := h.events.Get(c.Request.Context(), c.Param("eventID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *PlatformHandler) ListOperationLogs(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	limit := parseLimit(c.Query("limit"), 50)
	items, err := h.operations.ListAuthorized(c.Request.Context(), principal, domainoperation.Filter{
		OperationType: c.Query("operationType"),
		Result:        c.Query("result"),
		Limit:         limit,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *PlatformHandler) ListMCPCapabilities(c *gin.Context) {
	items, err := h.integration.ListCapabilities(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

type ReadinessProbe interface {
	Ping(context.Context) error
}

type RuntimeMetricsProvider interface {
	Snapshot() runtimeobs.Snapshot
}

type SystemHandler struct {
	postgres ReadinessProbe
	metrics  RuntimeMetricsProvider
}

func NewSystemHandler(postgres ReadinessProbe, metrics RuntimeMetricsProvider) *SystemHandler {
	return &SystemHandler{postgres: postgres, metrics: metrics}
}

func (h *SystemHandler) Healthz(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	status := map[string]string{"status": "ok", "postgres": "ok"}
	httpStatus := http.StatusOK
	if err := h.postgres.Ping(ctx); err != nil {
		status["status"] = "degraded"
		status["postgres"] = err.Error()
		httpStatus = http.StatusServiceUnavailable
	}
	apiresponse.JSON(c, httpStatus, status)
}

func (h *SystemHandler) Readyz(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()
	if err := h.postgres.Ping(ctx); err != nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "postgres_unavailable", fmt.Sprintf("postgres not ready: %v", err))
		return
	}
	apiresponse.JSON(c, http.StatusOK, gin.H{"status": "ready"})
}

func (h *SystemHandler) RuntimeMetrics(c *gin.Context) {
	if h.metrics == nil {
		apiresponse.JSON(c, http.StatusOK, runtimeobs.Snapshot{})
		return
	}
	apiresponse.JSON(c, http.StatusOK, h.metrics.Snapshot())
}

func parseLimit(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 {
		return fallback
	}
	return limit
}

func writeError(c *gin.Context, err error) {
	status := aperrors.StatusCode(err)
	code := aperrors.Code(err)
	message := err.Error()
	if status == http.StatusInternalServerError {
		message = fmt.Sprintf("request failed: %v", err)
	}
	apiresponse.Error(c, status, code, message)
}
