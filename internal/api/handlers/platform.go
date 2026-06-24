package handlers

import (
	"context"
	"io"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainevent "github.com/opensoha/soha/internal/domain/event"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmcp "github.com/opensoha/soha/internal/domain/mcp"
	domainoperation "github.com/opensoha/soha/internal/domain/operation"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"k8s.io/client-go/tools/remotecommand"
)

type ClusterService interface {
	List(context.Context) ([]domaincluster.Summary, error)
	ListAccessible(context.Context, domainidentity.Principal) ([]domaincluster.Summary, error)
	Describe(context.Context, domainidentity.Principal, string) (domaincluster.Detail, error)
	CapabilityMatrix(context.Context, domainidentity.Principal) ([]domaincluster.CapabilityMatrixEntry, error)
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
	GetNetworkTopology(context.Context, domainidentity.Principal, string, string) (domainresource.NetworkTopologyView, error)
	GetServiceMetrics(context.Context, domainidentity.Principal, string, string, string, int, int) (domainresource.ResourceMetricsView, error)
	ListIngresses(context.Context, domainidentity.Principal, string, string) ([]domainresource.IngressView, error)
	ListEndpointSlices(context.Context, domainidentity.Principal, string, string) ([]domainresource.EndpointSliceView, error)
	ListNetworkPolicies(context.Context, domainidentity.Principal, string, string) ([]domainresource.NetworkPolicyView, error)
	ListGatewayClasses(context.Context, domainidentity.Principal, string) ([]domainresource.GatewayClassView, error)
	ListGateways(context.Context, domainidentity.Principal, string, string) ([]domainresource.GatewayView, error)
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
	ListHelmCharts(context.Context, domainidentity.Principal, string, string, int, int) (domainresource.HelmChartCatalogView, error)
	GetHelmChartDetail(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.HelmChartDetailView, error)
	GetHelmChartValuesTemplate(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.HelmChartValuesTemplateView, error)
	InstallHelmChart(context.Context, domainidentity.Principal, string, domainresource.HelmChartInstallInput) (domainresource.HelmChartInstallResult, error)
	ListHelmReleases(context.Context, domainidentity.Principal, string, string) ([]domainresource.HelmReleaseView, error)
	GetHelmReleaseDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.HelmReleaseDetailView, error)
	ListHelmReleaseHistory(context.Context, domainidentity.Principal, string, string, string) ([]domainresource.HelmReleaseHistoryView, error)
	GetHelmReleaseValues(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.HelmValuesView, error)
	UpdateHelmReleaseValues(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.HelmValuesView, error)
	DeleteHelmRelease(context.Context, domainidentity.Principal, string, string, string) error
	ListClusterEvents(context.Context, domainidentity.Principal, string, string, int) ([]domainresource.ClusterEventView, error)
	RestartDeployment(context.Context, domainidentity.Principal, string, string, string) error
	RollbackDeployment(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.DeploymentRollbackView, error)
	ScaleDeployment(context.Context, domainidentity.Principal, string, string, string, int32) error
	RestartStatefulSet(context.Context, domainidentity.Principal, string, string, string) error
	ScaleStatefulSet(context.Context, domainidentity.Principal, string, string, string, int32) error
	RestartDaemonSet(context.Context, domainidentity.Principal, string, string, string) error
}

type AuditService interface {
	ListAuthorized(context.Context, domainidentity.Principal, domainaudit.Filter) ([]domainaudit.Entry, error)
	SummaryAuthorized(context.Context, domainidentity.Principal, domainaudit.Filter) (domainaudit.Summary, error)
	ExportCSVAuthorized(context.Context, domainidentity.Principal, domainaudit.Filter) (domainaudit.Export, error)
}

type EventService interface {
	List(context.Context, int) ([]domainevent.Envelope, error)
	Get(context.Context, string) (domainevent.Envelope, error)
	ValidateConnectorEventSinkToken(string) error
	IngestConnectorEvents(context.Context, domainevent.ConnectorEventIngestInput) (int, error)
}

type OperationService interface {
	ListAuthorized(context.Context, domainidentity.Principal, domainoperation.Filter) ([]domainoperation.Entry, error)
	SummaryAuthorized(context.Context, domainidentity.Principal, domainoperation.Filter) (domainoperation.Summary, error)
	ExportCSVAuthorized(context.Context, domainidentity.Principal, domainoperation.Filter) (domainoperation.Export, error)
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
