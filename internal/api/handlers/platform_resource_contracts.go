package handlers

import (
	"context"
	"io"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

type PodReader interface {
	ListPods(context.Context, domainidentity.Principal, string, string) ([]domainresource.PodView, error)
	GetWorkloadOverview(context.Context, domainidentity.Principal, string, string) (domainresource.WorkloadOverviewView, error)
	GetPodDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.PodDetailView, error)
	GetPodLogs(context.Context, domainidentity.Principal, string, string, string, string, int64, int64, bool) (domainresource.PodLogsView, error)
}

type PodResourceEditor interface {
	DeletePod(context.Context, domainidentity.Principal, string, string, string) error
	GetPodYAML(context.Context, domainidentity.Principal, string, string, string) (domainresource.ResourceYAMLView, error)
	ApplyPodYAML(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.ResourceYAMLView, error)
}

type PodDiagnostics interface {
	GetPodMetrics(context.Context, domainidentity.Principal, string, string, string, int, int) (domainresource.PodMetricsView, error)
	ExecPod(context.Context, domainidentity.Principal, string, string, string, string, string, int64) (domainresource.PodExecView, error)
}

type DeploymentReader interface {
	ListDeployments(context.Context, domainidentity.Principal, string, string) ([]domainresource.DeploymentView, error)
	GetDeploymentDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.DeploymentDetailView, error)
	GetDeploymentYAML(context.Context, domainidentity.Principal, string, string, string) (domainresource.ResourceYAMLView, error)
	GetDeploymentMetrics(context.Context, domainidentity.Principal, string, string, string, int, int) (domainresource.ResourceMetricsView, error)
	GetDeploymentRolloutStatus(context.Context, domainidentity.Principal, string, string, string) (domainresource.DeploymentRolloutStatusView, error)
}

type DeploymentEditor interface {
	ApplyDeploymentYAML(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.ResourceYAMLView, error)
	ListDeploymentRolloutHistory(context.Context, domainidentity.Principal, string, string, string) ([]domainresource.RolloutHistoryView, error)
	RestartDeployment(context.Context, domainidentity.Principal, string, string, string) error
	RollbackDeployment(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.DeploymentRollbackView, error)
	ScaleDeployment(context.Context, domainidentity.Principal, string, string, string, int32) error
}

type StatefulSetReader interface {
	ListStatefulSets(context.Context, domainidentity.Principal, string, string) ([]domainresource.StatefulSetView, error)
	GetStatefulSetDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.StatefulSetDetailView, error)
	GetStatefulSetYAML(context.Context, domainidentity.Principal, string, string, string) (domainresource.ResourceYAMLView, error)
	GetStatefulSetMetrics(context.Context, domainidentity.Principal, string, string, string, int, int) (domainresource.ResourceMetricsView, error)
}

type StatefulSetEditor interface {
	ApplyStatefulSetYAML(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.ResourceYAMLView, error)
	RestartStatefulSet(context.Context, domainidentity.Principal, string, string, string) error
	ScaleStatefulSet(context.Context, domainidentity.Principal, string, string, string, int32) error
}

type DaemonSetReader interface {
	ListDaemonSets(context.Context, domainidentity.Principal, string, string) ([]domainresource.DaemonSetView, error)
	GetDaemonSetDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.DaemonSetDetailView, error)
	GetDaemonSetYAML(context.Context, domainidentity.Principal, string, string, string) (domainresource.ResourceYAMLView, error)
	GetDaemonSetMetrics(context.Context, domainidentity.Principal, string, string, string, int, int) (domainresource.ResourceMetricsView, error)
}

type DaemonSetEditor interface {
	ApplyDaemonSetYAML(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.ResourceYAMLView, error)
	RestartDaemonSet(context.Context, domainidentity.Principal, string, string, string) error
}

type JobService interface {
	ListJobs(context.Context, domainidentity.Principal, string, string) ([]domainresource.JobView, error)
	GetJobDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.JobDetailView, error)
	GetJobYAML(context.Context, domainidentity.Principal, string, string, string) (domainresource.ResourceYAMLView, error)
	ApplyJobYAML(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.ResourceYAMLView, error)
}

type CronJobService interface {
	ListCronJobs(context.Context, domainidentity.Principal, string, string) ([]domainresource.CronJobView, error)
	GetCronJobDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.CronJobDetailView, error)
	GetCronJobYAML(context.Context, domainidentity.Principal, string, string, string) (domainresource.ResourceYAMLView, error)
	ApplyCronJobYAML(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.ResourceYAMLView, error)
	SetCronJobSuspend(context.Context, domainidentity.Principal, string, string, string, bool) (domainresource.CronJobDetailView, error)
}

type WorkloadInventoryService interface {
	ListReplicaSets(context.Context, domainidentity.Principal, string, string) ([]domainresource.ReplicaSetView, error)
	ListHorizontalPodAutoscalers(context.Context, domainidentity.Principal, string, string) ([]domainresource.HorizontalPodAutoscalerView, error)
	ListPodDisruptionBudgets(context.Context, domainidentity.Principal, string, string) ([]domainresource.PodDisruptionBudgetView, error)
	ListReplicationControllers(context.Context, domainidentity.Principal, string, string) ([]domainresource.ReplicationControllerView, error)
}

type PodStreamService interface {
	StreamPodLogs(context.Context, domainidentity.Principal, string, string, string, string, int64, int64, io.Writer) error
	StreamPodTerminal(context.Context, domainidentity.Principal, string, string, string, string, string, io.Reader, io.Writer, io.Writer, domainresource.TerminalSizeQueue) error
}

type ResourceCreator interface {
	CreateResourceFromYAML(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.ResourceYAMLView, error)
}

type ConfigMapService interface {
	ListConfigMaps(context.Context, domainidentity.Principal, string, string) ([]domainresource.ConfigMapView, error)
	GetConfigMapDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.ConfigMapDetailView, error)
	UpdateConfigMapData(context.Context, domainidentity.Principal, string, string, string, map[string]string, map[string]string) (domainresource.ConfigMapDetailView, error)
	ListConfigMapReferences(context.Context, domainidentity.Principal, string, string, string) ([]domainresource.ConfigReferenceView, error)
}

type SecretService interface {
	ListSecrets(context.Context, domainidentity.Principal, string, string) ([]domainresource.SecretView, error)
	GetSecretDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.SecretDetailView, error)
	UpdateSecretData(context.Context, domainidentity.Principal, string, string, string, map[string]string) (domainresource.SecretDetailView, error)
	ListSecretReferences(context.Context, domainidentity.Principal, string, string, string) ([]domainresource.ConfigReferenceView, error)
}

type ClusterConfigurationInventory interface {
	ListPriorityClasses(context.Context, domainidentity.Principal, string) ([]domainresource.PriorityClassView, error)
	ListRuntimeClasses(context.Context, domainidentity.Principal, string) ([]domainresource.RuntimeClassView, error)
	ListMutatingWebhookConfigurations(context.Context, domainidentity.Principal, string) ([]domainresource.MutatingWebhookConfigurationView, error)
	ListValidatingWebhookConfigurations(context.Context, domainidentity.Principal, string) ([]domainresource.ValidatingWebhookConfigurationView, error)
}

type NamespaceConfigurationInventory interface {
	ListResourceQuotas(context.Context, domainidentity.Principal, string, string) ([]domainresource.ResourceQuotaView, error)
	ListLimitRanges(context.Context, domainidentity.Principal, string, string) ([]domainresource.LimitRangeView, error)
	ListLeases(context.Context, domainidentity.Principal, string, string) ([]domainresource.LeaseView, error)
}

type ConfigurationInventoryService interface {
	ClusterConfigurationInventory
	NamespaceConfigurationInventory
}

type NetworkOverviewService interface {
	ListServices(context.Context, domainidentity.Principal, string, string) ([]domainresource.ServiceView, error)
	GetNetworkTopology(context.Context, domainidentity.Principal, string, string) (domainresource.NetworkTopologyView, error)
	GetServiceMetrics(context.Context, domainidentity.Principal, string, string, string, int, int) (domainresource.ResourceMetricsView, error)
}

type NetworkInventoryService interface {
	ListIngresses(context.Context, domainidentity.Principal, string, string) ([]domainresource.IngressView, error)
	ListEndpointSlices(context.Context, domainidentity.Principal, string, string) ([]domainresource.EndpointSliceView, error)
	ListNetworkPolicies(context.Context, domainidentity.Principal, string, string) ([]domainresource.NetworkPolicyView, error)
	ListIngressClasses(context.Context, domainidentity.Principal, string) ([]domainresource.IngressClassView, error)
}

type GatewayRoutingService interface {
	ListGatewayClasses(context.Context, domainidentity.Principal, string) ([]domainresource.GatewayClassView, error)
	ListGateways(context.Context, domainidentity.Principal, string, string) ([]domainresource.GatewayView, error)
	ListHTTPRoutes(context.Context, domainidentity.Principal, string, string) ([]domainresource.HTTPRouteView, error)
}

type GatewayPolicyService interface {
	ListBackendTLSPolicies(context.Context, domainidentity.Principal, string, string) ([]domainresource.BackendTLSPolicyView, error)
	ListGRPCRoutes(context.Context, domainidentity.Principal, string, string) ([]domainresource.GRPCRouteView, error)
	ListReferenceGrants(context.Context, domainidentity.Principal, string, string) ([]domainresource.ReferenceGrantView, error)
}

type PersistentVolumeClaimService interface {
	ListPersistentVolumeClaims(context.Context, domainidentity.Principal, string, string) ([]domainresource.PersistentVolumeClaimView, error)
	GetPersistentVolumeClaimDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.PersistentVolumeClaimDetailView, error)
}

type PersistentVolumeService interface {
	ListPersistentVolumes(context.Context, domainidentity.Principal, string) ([]domainresource.PersistentVolumeView, error)
	GetPersistentVolumeDetail(context.Context, domainidentity.Principal, string, string) (domainresource.PersistentVolumeDetailView, error)
}

type StorageClassService interface {
	ListStorageClasses(context.Context, domainidentity.Principal, string) ([]domainresource.StorageClassView, error)
	GetStorageClassDetail(context.Context, domainidentity.Principal, string, string) (domainresource.StorageClassDetailView, error)
}

type ServiceAccountService interface {
	ListServiceAccounts(context.Context, domainidentity.Principal, string, string) ([]domainresource.ServiceAccountView, error)
	GetServiceAccountDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.ServiceAccountDetailView, error)
}

type RoleService interface {
	ListRoles(context.Context, domainidentity.Principal, string, string) ([]domainresource.RoleView, error)
	GetRoleDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.RoleDetailView, error)
}

type RoleBindingService interface {
	ListRoleBindings(context.Context, domainidentity.Principal, string, string) ([]domainresource.RoleBindingView, error)
	GetRoleBindingDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.RoleBindingDetailView, error)
}

type NamespacedRBACService interface {
	ServiceAccountService
	RoleService
	RoleBindingService
}

type ClusterRBACService interface {
	ListClusterRoles(context.Context, domainidentity.Principal, string) ([]domainresource.ClusterRoleView, error)
	GetClusterRoleDetail(context.Context, domainidentity.Principal, string, string) (domainresource.ClusterRoleDetailView, error)
	ListClusterRoleBindings(context.Context, domainidentity.Principal, string) ([]domainresource.ClusterRoleBindingView, error)
	GetClusterRoleBindingDetail(context.Context, domainidentity.Principal, string, string) (domainresource.ClusterRoleBindingDetailView, error)
}

type CRDReader interface {
	ListCRDs(context.Context, domainidentity.Principal, string) ([]domainresource.CRDView, error)
	ListCRDResources(context.Context, domainidentity.Principal, string, string, string) ([]domainresource.CustomResourceView, error)
	GetCRDResourceYAML(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.ResourceYAMLView, error)
}

type CRDEditor interface {
	CreateCRDResourceFromYAML(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.ResourceYAMLView, error)
	ApplyCRDResourceYAML(context.Context, domainidentity.Principal, string, string, string, string, string) (domainresource.ResourceYAMLView, error)
	DeleteCRDResource(context.Context, domainidentity.Principal, string, string, string, string) error
}

type HelmCatalogService interface {
	ListHelmCharts(context.Context, domainidentity.Principal, string, string, int, int) (domainresource.HelmChartCatalogView, error)
	GetHelmChartDetail(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.HelmChartDetailView, error)
	GetHelmChartValuesTemplate(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.HelmChartValuesTemplateView, error)
	InstallHelmChart(context.Context, domainidentity.Principal, string, domainresource.HelmChartInstallInput) (domainresource.HelmChartInstallResult, error)
}

type HelmReleaseReader interface {
	ListHelmReleases(context.Context, domainidentity.Principal, string, string) ([]domainresource.HelmReleaseView, error)
	GetHelmReleaseDetail(context.Context, domainidentity.Principal, string, string, string) (domainresource.HelmReleaseDetailView, error)
	ListHelmReleaseHistory(context.Context, domainidentity.Principal, string, string, string) ([]domainresource.HelmReleaseHistoryView, error)
	GetHelmReleaseValues(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.HelmValuesView, error)
}

type HelmReleaseEditor interface {
	UpdateHelmReleaseValues(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.HelmValuesView, error)
	DeleteHelmRelease(context.Context, domainidentity.Principal, string, string, string) error
}

type NamespaceService interface {
	ListNamespaces(context.Context, domainidentity.Principal, string) ([]domainresource.NamespaceView, error)
	CreateNamespace(context.Context, domainidentity.Principal, string, domainresource.NamespaceUpsertInput) (domainresource.NamespaceView, error)
	UpdateNamespace(context.Context, domainidentity.Principal, string, string, domainresource.NamespaceUpsertInput) (domainresource.NamespaceView, error)
	DeleteNamespace(context.Context, domainidentity.Principal, string, string) error
}

type NodeReader interface {
	ListNodes(context.Context, domainidentity.Principal, string) ([]domainresource.NodeView, error)
	GetNodeDetail(context.Context, domainidentity.Principal, string, string) (domainresource.NodeDetailView, error)
	GetNodeYAML(context.Context, domainidentity.Principal, string, string) (domainresource.ResourceYAMLView, error)
}

type NodeEditor interface {
	ApplyNodeYAML(context.Context, domainidentity.Principal, string, string, string) (domainresource.ResourceYAMLView, error)
	UpdateNode(context.Context, domainidentity.Principal, string, string, domainresource.NodeUpdateInput) (domainresource.NodeDetailView, error)
	DeleteNode(context.Context, domainidentity.Principal, string, string) error
}

type GenericResourceService interface {
	GetResourceYAML(context.Context, domainidentity.Principal, string, string, string, string) (domainresource.ResourceYAMLView, error)
	ApplyResourceYAMLByKind(context.Context, domainidentity.Principal, string, string, string, string, string) (domainresource.ResourceYAMLView, error)
	DeleteResourceByKind(context.Context, domainidentity.Principal, string, string, string, string) error
}

type PortForwardService interface {
	ListPortForwards(context.Context, domainidentity.Principal, string) ([]domainresource.PortForwardSessionView, error)
	RegisterPortForward(context.Context, domainidentity.Principal, string, domainresource.PortForwardRegisterInput) (domainresource.PortForwardSessionView, error)
	StopPortForward(context.Context, domainidentity.Principal, string, string) error
}

type ClusterEventService interface {
	ListClusterEvents(context.Context, domainidentity.Principal, string, string, int) ([]domainresource.ClusterEventView, error)
}

// ResourceServices wires independently replaceable capability contracts.
type ResourceServices struct {
	PodReader              PodReader
	PodEditor              PodResourceEditor
	PodDiagnostics         PodDiagnostics
	PodStreams             PodStreamService
	DeploymentReader       DeploymentReader
	DeploymentEditor       DeploymentEditor
	StatefulSetReader      StatefulSetReader
	StatefulSetEditor      StatefulSetEditor
	DaemonSetReader        DaemonSetReader
	DaemonSetEditor        DaemonSetEditor
	Jobs                   JobService
	CronJobs               CronJobService
	WorkloadInventory      WorkloadInventoryService
	Creator                ResourceCreator
	ConfigMaps             ConfigMapService
	Secrets                SecretService
	ConfigurationInventory ConfigurationInventoryService
	NetworkOverview        NetworkOverviewService
	NetworkInventory       NetworkInventoryService
	GatewayRouting         GatewayRoutingService
	GatewayPolicy          GatewayPolicyService
	PersistentVolumeClaims PersistentVolumeClaimService
	PersistentVolumes      PersistentVolumeService
	StorageClasses         StorageClassService
	NamespacedRBAC         NamespacedRBACService
	ClusterRBAC            ClusterRBACService
	CRDReader              CRDReader
	CRDEditor              CRDEditor
	Helm                   HelmCatalogService
	HelmReleaseReader      HelmReleaseReader
	HelmReleaseEditor      HelmReleaseEditor
	Namespaces             NamespaceService
	NodeReader             NodeReader
	NodeEditor             NodeEditor
	Generic                GenericResourceService
	Events                 ClusterEventService
	PortForwards           PortForwardService
}
