package resource

import (
	"context"
	"io"
	"net"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

type ClusterMetadataProvider interface {
	Metadata(string) (domaincluster.Summary, error)
}

type PodReaderAgent interface {
	ListPods(context.Context, string) ([]domainresource.PodView, error)
	GetPodDetail(context.Context, string, string) (domainresource.PodDetailView, error)
	GetPodLogs(context.Context, string, string, string, int64, int64, bool) (domainresource.PodLogsView, error)
	GetPodYAML(context.Context, string, string) (domainresource.ResourceYAMLView, error)
}

type PodInteractiveAgent interface {
	StreamPodLogs(context.Context, string, string, string, int64, int64, io.Writer) error
	ExecPod(context.Context, string, string, string, string, int64) (domainresource.PodExecView, error)
	StreamPodTerminal(context.Context, string, string, string, string, io.Reader, io.Writer, io.Writer, domainresource.TerminalSizeQueue) error
}

type PodAgent interface {
	PodReaderAgent
	PodInteractiveAgent
}

type DirectPodReader interface {
	ListPods(context.Context, string, string) ([]domainresource.PodView, string, error)
	GetPodDetail(context.Context, string, string, string) (domainresource.PodDetailView, error)
	GetPodLogs(context.Context, string, string, string, string, int64, int64, bool) (domainresource.PodLogsView, error)
	GetPodYAML(context.Context, string, string, string) (domainresource.ResourceYAMLView, error)
}

type DirectPodMutator interface {
	DeletePod(context.Context, string, string, string) error
}

type DirectPodInteractive interface {
	StreamPodLogs(context.Context, string, string, string, string, int64, int64, io.Writer) error
	ExecPod(context.Context, string, string, string, string, string, int64) (domainresource.PodExecView, error)
	StreamPodTerminal(context.Context, string, string, string, string, string, io.Reader, io.Writer, io.Writer, domainresource.TerminalSizeQueue) error
}

type DirectPods interface {
	DirectPodReader
	DirectPodMutator
	DirectPodInteractive
}

type DeploymentReaderAgent interface {
	ListDeployments(context.Context, string) ([]domainresource.DeploymentView, error)
	GetDeploymentDetail(context.Context, string, string) (domainresource.DeploymentDetailView, error)
	GetDeploymentYAML(context.Context, string, string) (domainresource.ResourceYAMLView, error)
	GetDeploymentRolloutStatus(context.Context, string, string) (domainresource.DeploymentRolloutStatusView, error)
	ListDeploymentRolloutHistory(context.Context, string, string) ([]domainresource.RolloutHistoryView, error)
}

type DeploymentMutationAgent interface {
	RollbackDeployment(context.Context, string, string, string) error
	RestartDeployment(context.Context, string, string) error
	ScaleDeployment(context.Context, string, string, int32) error
}

type DeploymentAgent interface {
	DeploymentReaderAgent
	DeploymentMutationAgent
}

type StatefulSetAgent interface {
	ListStatefulSets(context.Context, string) ([]domainresource.StatefulSetView, error)
	GetStatefulSetDetail(context.Context, string, string) (domainresource.StatefulSetDetailView, error)
	GetStatefulSetYAML(context.Context, string, string) (domainresource.ResourceYAMLView, error)
	RestartStatefulSet(context.Context, string, string) error
	ScaleStatefulSet(context.Context, string, string, int32) error
}

type DaemonSetAgent interface {
	ListDaemonSets(context.Context, string) ([]domainresource.DaemonSetView, error)
	GetDaemonSetDetail(context.Context, string, string) (domainresource.DaemonSetDetailView, error)
	GetDaemonSetYAML(context.Context, string, string) (domainresource.ResourceYAMLView, error)
	RestartDaemonSet(context.Context, string, string) error
}

type JobAgent interface {
	ListJobs(context.Context, string) ([]domainresource.JobView, error)
	GetJobDetail(context.Context, string, string) (domainresource.JobDetailView, error)
	GetJobYAML(context.Context, string, string) (domainresource.ResourceYAMLView, error)
}

type CronJobAgent interface {
	ListCronJobs(context.Context, string) ([]domainresource.CronJobView, error)
	GetCronJobDetail(context.Context, string, string) (domainresource.CronJobDetailView, error)
	GetCronJobYAML(context.Context, string, string) (domainresource.ResourceYAMLView, error)
}

type BatchWorkloadAgent interface {
	JobAgent
	CronJobAgent
}

type WorkloadInventoryAgent interface {
	ListReplicaSets(context.Context, string) ([]domainresource.ReplicaSetView, error)
	ListHorizontalPodAutoscalers(context.Context, string) ([]domainresource.HorizontalPodAutoscalerView, error)
	ListPodDisruptionBudgets(context.Context, string) ([]domainresource.PodDisruptionBudgetView, error)
}

type WorkloadAgent interface {
	PodAgent
	DeploymentAgent
	StatefulSetAgent
	DaemonSetAgent
	BatchWorkloadAgent
	WorkloadInventoryAgent
}

type DirectDeploymentReader interface {
	ListDeployments(context.Context, string, string) ([]domainresource.DeploymentView, string, error)
	GetDeploymentDetail(context.Context, string, string, string) (domainresource.DeploymentDetailView, error)
	GetDeploymentYAML(context.Context, string, string, string) (domainresource.ResourceYAMLView, error)
	GetDeploymentRolloutStatus(context.Context, string, string, string) (domainresource.DeploymentRolloutStatusView, error)
	ListDeploymentRolloutHistory(context.Context, string, string, string) ([]domainresource.RolloutHistoryView, error)
}

type DirectDeploymentMutator interface {
	RollbackDeployment(context.Context, string, string, string, string) error
	RestartDeployment(context.Context, string, string, string) error
	ScaleDeployment(context.Context, string, string, string, int32) error
}

type DirectStatefulSet interface {
	ListStatefulSets(context.Context, string, string) ([]domainresource.StatefulSetView, string, error)
	GetStatefulSetDetail(context.Context, string, string, string) (domainresource.StatefulSetDetailView, error)
	GetStatefulSetYAML(context.Context, string, string, string) (domainresource.ResourceYAMLView, error)
	RestartStatefulSet(context.Context, string, string, string) error
	ScaleStatefulSet(context.Context, string, string, string, int32) error
}

type DirectDaemonSet interface {
	ListDaemonSets(context.Context, string, string) ([]domainresource.DaemonSetView, error)
	GetDaemonSetDetail(context.Context, string, string, string) (domainresource.DaemonSetDetailView, error)
	GetDaemonSetYAML(context.Context, string, string, string) (domainresource.ResourceYAMLView, error)
	RestartDaemonSet(context.Context, string, string, string) error
}

type DirectJobReader interface {
	ListJobs(context.Context, string, string) ([]domainresource.JobView, error)
	GetJobDetail(context.Context, string, string, string) (domainresource.JobDetailView, error)
	GetJobYAML(context.Context, string, string, string) (domainresource.ResourceYAMLView, error)
}

type DirectCronJob interface {
	ListCronJobs(context.Context, string, string) ([]domainresource.CronJobView, error)
	GetCronJobDetail(context.Context, string, string, string) (domainresource.CronJobDetailView, error)
	GetCronJobYAML(context.Context, string, string, string) (domainresource.ResourceYAMLView, error)
	SetCronJobSuspend(context.Context, string, string, string, bool) (domainresource.CronJobDetailView, error)
}

type DirectWorkloadInventoryReader interface {
	ListReplicaSets(context.Context, string, string) ([]domainresource.ReplicaSetView, error)
	ListHorizontalPodAutoscalers(context.Context, string, string) ([]domainresource.HorizontalPodAutoscalerView, error)
	ListPodDisruptionBudgets(context.Context, string, string) ([]domainresource.PodDisruptionBudgetView, error)
}

type DirectWorkloads interface {
	DirectDeploymentReader
	DirectDeploymentMutator
	DirectStatefulSet
	DirectDaemonSet
	DirectJobReader
	DirectCronJob
	DirectWorkloadInventoryReader
}

type ConfigurationObjectAgent interface {
	ListConfigMaps(context.Context, string) ([]domainresource.ConfigMapView, error)
	ListSecrets(context.Context, string) ([]domainresource.SecretView, error)
}

type ClusterClassAgent interface {
	ListIngressClasses(context.Context) ([]domainresource.IngressClassView, error)
	ListPriorityClasses(context.Context) ([]domainresource.PriorityClassView, error)
	ListRuntimeClasses(context.Context) ([]domainresource.RuntimeClassView, error)
}

type AdmissionConfigurationAgent interface {
	ListMutatingWebhookConfigurations(context.Context) ([]domainresource.MutatingWebhookConfigurationView, error)
	ListValidatingWebhookConfigurations(context.Context) ([]domainresource.ValidatingWebhookConfigurationView, error)
}

type NamespaceConfigurationAgent interface {
	ListResourceQuotas(context.Context, string) ([]domainresource.ResourceQuotaView, error)
	ListLimitRanges(context.Context, string) ([]domainresource.LimitRangeView, error)
	ListLeases(context.Context, string) ([]domainresource.LeaseView, error)
	ListReplicationControllers(context.Context, string) ([]domainresource.ReplicationControllerView, error)
}

type ConfigurationAgent interface {
	ConfigurationObjectAgent
	ClusterClassAgent
	AdmissionConfigurationAgent
	NamespaceConfigurationAgent
}

type DirectConfigurationObjectReader interface {
	ListConfigMaps(context.Context, string, string) ([]domainresource.ConfigMapView, error)
	ListSecrets(context.Context, string, string) ([]domainresource.SecretView, error)
	GetConfigMapDetail(context.Context, string, string, string) (domainresource.ConfigMapDetailView, error)
	GetSecretDetail(context.Context, string, string, string) (domainresource.SecretDetailView, error)
	ListConfigReferences(context.Context, string, string, string, bool) ([]domainresource.ConfigReferenceView, error)
}

type DirectConfigurationObjectWriter interface {
	UpdateConfigMapData(context.Context, string, string, string, map[string]string, map[string]string) (domainresource.ConfigMapDetailView, error)
	UpdateSecretData(context.Context, string, string, string, map[string]string) (domainresource.SecretDetailView, error)
}

type DirectClusterConfigurationReader interface {
	ListIngressClasses(context.Context, string) ([]domainresource.IngressClassView, error)
	ListPriorityClasses(context.Context, string) ([]domainresource.PriorityClassView, error)
	ListRuntimeClasses(context.Context, string) ([]domainresource.RuntimeClassView, error)
	ListMutatingWebhookConfigurations(context.Context, string) ([]domainresource.MutatingWebhookConfigurationView, error)
	ListValidatingWebhookConfigurations(context.Context, string) ([]domainresource.ValidatingWebhookConfigurationView, error)
}

type DirectNamespaceConfigurationReader interface {
	ListResourceQuotas(context.Context, string, string) ([]domainresource.ResourceQuotaView, error)
	ListLimitRanges(context.Context, string, string) ([]domainresource.LimitRangeView, error)
	ListLeases(context.Context, string, string) ([]domainresource.LeaseView, error)
	ListReplicationControllers(context.Context, string, string) ([]domainresource.ReplicationControllerView, error)
}

type DirectConfiguration interface {
	DirectConfigurationObjectReader
	DirectConfigurationObjectWriter
	DirectClusterConfigurationReader
	DirectNamespaceConfigurationReader
}

type CoreNetworkAgent interface {
	ListServices(context.Context, string) ([]domainresource.ServiceView, error)
	ListIngresses(context.Context, string) ([]domainresource.IngressView, error)
	ListEndpointSlices(context.Context, string) ([]domainresource.EndpointSliceView, error)
	ListNetworkPolicies(context.Context, string) ([]domainresource.NetworkPolicyView, error)
}

type GatewayAgent interface {
	ListGatewayClasses(context.Context) ([]domainresource.GatewayClassView, error)
	ListGateways(context.Context, string) ([]domainresource.GatewayView, error)
	ListHTTPRoutes(context.Context, string) ([]domainresource.HTTPRouteView, error)
}

type GatewayPolicyAgent interface {
	ListBackendTLSPolicies(context.Context, string) ([]domainresource.BackendTLSPolicyView, error)
	ListGRPCRoutes(context.Context, string) ([]domainresource.GRPCRouteView, error)
	ListReferenceGrants(context.Context, string) ([]domainresource.ReferenceGrantView, error)
}

type DirectGatewayReader interface {
	ListGatewayClasses(context.Context, string) ([]domainresource.GatewayClassView, error)
	ListGateways(context.Context, string, string) ([]domainresource.GatewayView, error)
	ListHTTPRoutes(context.Context, string, string) ([]domainresource.HTTPRouteView, error)
	ListBackendTLSPolicies(context.Context, string, string) ([]domainresource.BackendTLSPolicyView, error)
	ListGRPCRoutes(context.Context, string, string) ([]domainresource.GRPCRouteView, error)
	ListReferenceGrants(context.Context, string, string) ([]domainresource.ReferenceGrantView, error)
}

type NetworkAgent interface {
	CoreNetworkAgent
	GatewayAgent
	GatewayPolicyAgent
}

type DirectNetworkReader interface {
	ListServices(context.Context, string, string) ([]domainresource.ServiceView, string, error)
	ListIngresses(context.Context, string, string) ([]domainresource.IngressView, string, error)
	ListEndpointSlices(context.Context, string, string) ([]domainresource.EndpointSliceView, error)
	ListNetworkPolicies(context.Context, string, string) ([]domainresource.NetworkPolicyView, error)
}

type StorageAgent interface {
	ListPersistentVolumeClaims(context.Context, string) ([]domainresource.PersistentVolumeClaimView, error)
	ListPersistentVolumes(context.Context) ([]domainresource.PersistentVolumeView, error)
	ListStorageClasses(context.Context) ([]domainresource.StorageClassView, error)
}

type DirectStorageReader interface {
	ListPersistentVolumeClaims(context.Context, string, string) ([]domainresource.PersistentVolumeClaimView, error)
	GetPersistentVolumeClaimDetail(context.Context, string, string, string) (domainresource.PersistentVolumeClaimDetailView, error)
	ListPersistentVolumes(context.Context, string) ([]domainresource.PersistentVolumeView, error)
	GetPersistentVolumeDetail(context.Context, string, string) (domainresource.PersistentVolumeDetailView, error)
	ListStorageClasses(context.Context, string) ([]domainresource.StorageClassView, error)
	GetStorageClassDetail(context.Context, string, string) (domainresource.StorageClassDetailView, error)
}

type ServiceAccountAgent interface {
	ListServiceAccounts(context.Context, string) ([]domainresource.ServiceAccountView, error)
	GetServiceAccountDetail(context.Context, string, string) (domainresource.ServiceAccountDetailView, error)
}

type NamespacedRoleAgent interface {
	ListRoles(context.Context, string) ([]domainresource.RoleView, error)
	GetRoleDetail(context.Context, string, string) (domainresource.RoleDetailView, error)
	ListRoleBindings(context.Context, string) ([]domainresource.RoleBindingView, error)
	GetRoleBindingDetail(context.Context, string, string) (domainresource.RoleBindingDetailView, error)
}

type ClusterRoleAgent interface {
	ListClusterRoles(context.Context) ([]domainresource.ClusterRoleView, error)
	GetClusterRoleDetail(context.Context, string) (domainresource.ClusterRoleDetailView, error)
	ListClusterRoleBindings(context.Context) ([]domainresource.ClusterRoleBindingView, error)
	GetClusterRoleBindingDetail(context.Context, string) (domainresource.ClusterRoleBindingDetailView, error)
}

type RBACAgent interface {
	ServiceAccountAgent
	NamespacedRoleAgent
	ClusterRoleAgent
}

type DirectServiceAccountReader interface {
	ListServiceAccounts(context.Context, string, string) ([]domainresource.ServiceAccountView, error)
	GetServiceAccountDetail(context.Context, string, string, string) (domainresource.ServiceAccountDetailView, error)
}

type DirectNamespacedRoleReader interface {
	ListRoles(context.Context, string, string) ([]domainresource.RoleView, error)
	GetRoleDetail(context.Context, string, string, string) (domainresource.RoleDetailView, error)
	ListRoleBindings(context.Context, string, string) ([]domainresource.RoleBindingView, error)
	GetRoleBindingDetail(context.Context, string, string, string) (domainresource.RoleBindingDetailView, error)
}

type DirectClusterRoleReader interface {
	ListClusterRoles(context.Context, string) ([]domainresource.ClusterRoleView, error)
	GetClusterRoleDetail(context.Context, string, string) (domainresource.ClusterRoleDetailView, error)
	ListClusterRoleBindings(context.Context, string) ([]domainresource.ClusterRoleBindingView, error)
	GetClusterRoleBindingDetail(context.Context, string, string) (domainresource.ClusterRoleBindingDetailView, error)
}

type DirectRBACReader interface {
	DirectServiceAccountReader
	DirectNamespacedRoleReader
	DirectClusterRoleReader
}

type HelmReleaseReaderAgent interface {
	ListHelmReleases(context.Context, string) ([]domainresource.HelmReleaseView, error)
	GetHelmReleaseDetail(context.Context, string, string) (domainresource.HelmReleaseDetailView, error)
	ListHelmReleaseHistory(context.Context, string, string) ([]domainresource.HelmReleaseHistoryView, error)
	GetHelmReleaseValues(context.Context, string, string, string) (domainresource.HelmValuesView, error)
}

type HelmReleaseMutationAgent interface {
	InstallHelmChart(context.Context, domainresource.HelmChartInstallInput) (domainresource.HelmChartInstallResult, error)
	UpdateHelmReleaseValues(context.Context, string, string, string) (domainresource.HelmValuesView, error)
	DeleteHelmRelease(context.Context, string, string) error
}

type HelmAgent interface {
	HelmReleaseReaderAgent
	HelmReleaseMutationAgent
}

type InventoryAgent interface {
	ListNamespaces(context.Context) ([]domainresource.NamespaceView, error)
	ListNodes(context.Context) ([]domainresource.NodeView, error)
	GetNodeDetail(context.Context, string) (domainresource.NodeDetailView, error)
}

type DirectInventory interface {
	ListNamespaces(context.Context, string) ([]domainresource.NamespaceView, string, error)
	CreateNamespace(context.Context, string, domainresource.NamespaceUpsertInput) (domainresource.NamespaceView, error)
	UpdateNamespace(context.Context, string, string, domainresource.NamespaceUpsertInput) (domainresource.NamespaceView, error)
	DeleteNamespace(context.Context, string, string) error
	ListNodes(context.Context, string) ([]domainresource.NodeView, string, error)
	GetNodeDetail(context.Context, string, string) (domainresource.NodeDetailView, error)
	UpdateNode(context.Context, string, string, domainresource.NodeUpdateInput) (domainresource.NodeDetailView, error)
	GetNodeYAML(context.Context, string, string) (domainresource.ResourceYAMLView, error)
	DeleteNode(context.Context, string, string) error
}

type CustomResourceReaderAgent interface {
	ListCRDs(context.Context) ([]domainresource.CRDView, error)
	ListCustomResources(context.Context, domainresource.CRDResourceDefinition, string) ([]domainresource.CustomResourceView, error)
}

type CustomResourceMutationAgent interface {
	CreateCustomResourceYAML(context.Context, domainresource.CRDResourceDefinition, string, string) (domainresource.ResourceYAMLView, error)
	GetCustomResourceYAML(context.Context, domainresource.CRDResourceDefinition, string, string) (domainresource.ResourceYAMLView, error)
	ApplyCustomResourceYAML(context.Context, domainresource.CRDResourceDefinition, string, string, string) (domainresource.ResourceYAMLView, error)
	DeleteCustomResource(context.Context, domainresource.CRDResourceDefinition, string, string) error
}

type CustomResourceAgent interface {
	CustomResourceReaderAgent
	CustomResourceMutationAgent
}

type DirectCustomResource interface {
	ListCRDs(context.Context, string) ([]domainresource.CRDView, error)
	ResolveCRD(context.Context, string, string) (domainresource.CRDResourceDefinition, error)
	ListCustomResources(context.Context, string, domainresource.CRDResourceDefinition, string) ([]domainresource.CustomResourceView, error)
	CreateCustomResourceYAML(context.Context, string, domainresource.CRDResourceDefinition, string, string) (domainresource.ResourceYAMLView, error)
	GetCustomResourceYAML(context.Context, string, domainresource.CRDResourceDefinition, string, string) (domainresource.ResourceYAMLView, error)
	ApplyCustomResourceYAML(context.Context, string, domainresource.CRDResourceDefinition, string, string, string) (domainresource.ResourceYAMLView, error)
	DeleteCustomResource(context.Context, string, domainresource.CRDResourceDefinition, string, string) error
}

type GenericResourceAgent interface {
	GetResourceYAML(context.Context, string, string, string) (domainresource.ResourceYAMLView, error)
	ApplyResourceYAML(context.Context, string, string, string, string) (domainresource.ResourceYAMLView, error)
	DeleteResource(context.Context, string, string, string) error
}

type DirectGenericResource interface {
	CreateResourceYAML(context.Context, string, string, string, string) (domainresource.ResourceYAMLView, error)
	GetResourceYAML(context.Context, string, string, string, string) (domainresource.ResourceYAMLView, error)
	ApplyResourceYAML(context.Context, string, string, string, string, string) (domainresource.ResourceYAMLView, error)
	DeleteResource(context.Context, string, string, string, string) error
}

type EventAgent interface {
	ListClusterEvents(context.Context, string, int) ([]domainresource.ClusterEventView, error)
}

type DirectEventReader interface {
	ListClusterEvents(context.Context, string, string, int) ([]domainresource.ClusterEventView, string, error)
}

type DirectHelmReleaseReader interface {
	ListHelmReleases(context.Context, string, string) ([]domainresource.HelmReleaseView, error)
	GetHelmReleaseDetail(context.Context, string, string, string) (domainresource.HelmReleaseDetailView, error)
	ListHelmReleaseHistory(context.Context, string, string, string) ([]domainresource.HelmReleaseHistoryView, error)
	GetHelmReleaseValues(context.Context, string, string, string, string) (domainresource.HelmValuesView, error)
}

type DirectHelmReleaseMutator interface {
	InstallHelmChart(context.Context, string, domainresource.HelmChartInstallInput) (domainresource.HelmChartInstallResult, error)
	UpdateHelmReleaseValues(context.Context, string, string, string, string) (domainresource.HelmValuesView, error)
	DeleteHelmRelease(context.Context, string, string, string) error
}

type DirectHelm interface {
	DirectHelmReleaseReader
	DirectHelmReleaseMutator
}

type DirectPortForwardSession interface {
	Stop()
	LastError() string
}

type DirectPortForwardStarter interface {
	StartPortForward(context.Context, string, domainresource.PortForwardSessionView) (DirectPortForwardSession, error)
}

type PortForwardAgent interface {
	ListPortForwards(context.Context) ([]domainresource.PortForwardSessionView, error)
	RegisterPortForward(context.Context, domainresource.PortForwardRegisterInput) (domainresource.PortForwardSessionView, error)
	StopPortForward(context.Context, string) error
	StreamPortForward(context.Context, string, net.Conn) error
}

type AgentClientFactory[T any] func(domaincluster.Connection) (T, error)

// AgentClients keeps agent protocol dependencies aligned with resource
// capabilities instead of exposing the infrastructure client's full API.
type AgentClients struct {
	Workloads       AgentClientFactory[WorkloadAgent]
	Configuration   AgentClientFactory[ConfigurationAgent]
	Network         AgentClientFactory[NetworkAgent]
	Storage         AgentClientFactory[StorageAgent]
	RBAC            AgentClientFactory[RBACAgent]
	Helm            AgentClientFactory[HelmAgent]
	Inventory       AgentClientFactory[InventoryAgent]
	CustomResources AgentClientFactory[CustomResourceAgent]
	Generic         AgentClientFactory[GenericResourceAgent]
	Events          AgentClientFactory[EventAgent]
	PortForwards    AgentClientFactory[PortForwardAgent]
}

type PortForwardRecord = domainresource.PortForwardRecord

type PortForwardRepository interface {
	List(context.Context) ([]PortForwardRecord, error)
	Upsert(context.Context, PortForwardRecord) error
	Delete(context.Context, string) error
	MarkStatus(context.Context, string, string, string) error
}
