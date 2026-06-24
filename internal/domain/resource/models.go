package resource

import contractsresource "github.com/opensoha/soha-contracts/resource"

const (
	PodLogsMaxContentBytes = contractsresource.PodLogsMaxContentBytes
	PodExecMaxOutputBytes  = contractsresource.PodExecMaxOutputBytes
)

type (
	CRDResourceDefinition = contractsresource.CRDResourceDefinition
	NamespaceView         = contractsresource.NamespaceView
	NamespaceUpsertInput  = contractsresource.NamespaceUpsertInput

	PodView                       = contractsresource.PodView
	PodDetailView                 = contractsresource.PodDetailView
	PodLogsView                   = contractsresource.PodLogsView
	PodExecView                   = contractsresource.PodExecView
	PodVolumeMountView            = contractsresource.PodVolumeMountView
	PodVolumeView                 = contractsresource.PodVolumeView
	PodRelatedResourceView        = contractsresource.PodRelatedResourceView
	WorkloadOverviewNamespaceView = contractsresource.WorkloadOverviewNamespaceView
	WorkloadOverviewPodView       = contractsresource.WorkloadOverviewPodView
	WorkloadOverviewView          = contractsresource.WorkloadOverviewView
	WorkloadConditionView         = contractsresource.WorkloadConditionView
	WorkloadContainerView         = contractsresource.WorkloadContainerView

	ResourceQuantityView   = contractsresource.ResourceQuantityView
	ResourcePercentageView = contractsresource.ResourcePercentageView
	ResourceMetricsView    = contractsresource.ResourceMetricsView
	ResourceYAMLView       = contractsresource.ResourceYAMLView

	MetricPointView  = contractsresource.MetricPointView
	MetricSeriesView = contractsresource.MetricSeriesView
	PodMetricsView   = contractsresource.PodMetricsView

	DeploymentView              = contractsresource.DeploymentView
	DeploymentDetailView        = contractsresource.DeploymentDetailView
	DeploymentRolloutStatusView = contractsresource.DeploymentRolloutStatusView
	DeploymentRollbackView      = contractsresource.DeploymentRollbackView
	RolloutHistoryView          = contractsresource.RolloutHistoryView
	StatefulSetView             = contractsresource.StatefulSetView
	StatefulSetDetailView       = contractsresource.StatefulSetDetailView
	DaemonSetView               = contractsresource.DaemonSetView
	DaemonSetDetailView         = contractsresource.DaemonSetDetailView
	JobView                     = contractsresource.JobView
	JobDetailView               = contractsresource.JobDetailView
	CronJobView                 = contractsresource.CronJobView
	CronJobDetailView           = contractsresource.CronJobDetailView
	ReplicaSetView              = contractsresource.ReplicaSetView
	ReplicationControllerView   = contractsresource.ReplicationControllerView

	ServiceView                = contractsresource.ServiceView
	IngressView                = contractsresource.IngressView
	GatewayView                = contractsresource.GatewayView
	GatewayClassView           = contractsresource.GatewayClassView
	HTTPRouteView              = contractsresource.HTTPRouteView
	BackendTLSPolicyView       = contractsresource.BackendTLSPolicyView
	GRPCRouteView              = contractsresource.GRPCRouteView
	ReferenceGrantView         = contractsresource.ReferenceGrantView
	EndpointSliceView          = contractsresource.EndpointSliceView
	NetworkPolicyView          = contractsresource.NetworkPolicyView
	IngressClassView           = contractsresource.IngressClassView
	NetworkTopologyView        = contractsresource.NetworkTopologyView
	NetworkTopologyNodeView    = contractsresource.NetworkTopologyNodeView
	NetworkTopologyTraceView   = contractsresource.NetworkTopologyTraceView
	NetworkTopologySummaryView = contractsresource.NetworkTopologySummaryView

	NodeView                = contractsresource.NodeView
	NodePodView             = contractsresource.NodePodView
	NodeDetailView          = contractsresource.NodeDetailView
	NodeResourceSummaryView = contractsresource.NodeResourceSummaryView
	NodeTaintView           = contractsresource.NodeTaintView
	NodeUpdateInput         = contractsresource.NodeUpdateInput
	ClusterEventView        = contractsresource.ClusterEventView

	PersistentVolumeClaimView       = contractsresource.PersistentVolumeClaimView
	PersistentVolumeView            = contractsresource.PersistentVolumeView
	StorageClassView                = contractsresource.StorageClassView
	PersistentVolumeClaimDetailView = contractsresource.PersistentVolumeClaimDetailView
	PersistentVolumeDetailView      = contractsresource.PersistentVolumeDetailView
	StorageClassDetailView          = contractsresource.StorageClassDetailView

	CRDView                            = contractsresource.CRDView
	CustomResourceView                 = contractsresource.CustomResourceView
	ConfigMapView                      = contractsresource.ConfigMapView
	ConfigMapDetailView                = contractsresource.ConfigMapDetailView
	SecretView                         = contractsresource.SecretView
	SecretDetailView                   = contractsresource.SecretDetailView
	ServiceAccountView                 = contractsresource.ServiceAccountView
	ServiceAccountDetailView           = contractsresource.ServiceAccountDetailView
	RoleView                           = contractsresource.RoleView
	RoleDetailView                     = contractsresource.RoleDetailView
	RoleBindingView                    = contractsresource.RoleBindingView
	RoleBindingDetailView              = contractsresource.RoleBindingDetailView
	ClusterRoleView                    = contractsresource.ClusterRoleView
	ClusterRoleDetailView              = contractsresource.ClusterRoleDetailView
	ClusterRoleBindingView             = contractsresource.ClusterRoleBindingView
	ClusterRoleBindingDetailView       = contractsresource.ClusterRoleBindingDetailView
	MutatingWebhookConfigurationView   = contractsresource.MutatingWebhookConfigurationView
	ValidatingWebhookConfigurationView = contractsresource.ValidatingWebhookConfigurationView
	ResourceQuotaView                  = contractsresource.ResourceQuotaView
	LimitRangeView                     = contractsresource.LimitRangeView
	LeaseView                          = contractsresource.LeaseView
	HorizontalPodAutoscalerView        = contractsresource.HorizontalPodAutoscalerView
	PodDisruptionBudgetView            = contractsresource.PodDisruptionBudgetView
	PriorityClassView                  = contractsresource.PriorityClassView
	RuntimeClassView                   = contractsresource.RuntimeClassView

	HelmReleaseView              = contractsresource.HelmReleaseView
	HelmReleaseDetailView        = contractsresource.HelmReleaseDetailView
	HelmReleaseHistoryView       = contractsresource.HelmReleaseHistoryView
	HelmValuesView               = contractsresource.HelmValuesView
	HelmChartRepositoryView      = contractsresource.HelmChartRepositoryView
	HelmChartMaintainerView      = contractsresource.HelmChartMaintainerView
	HelmChartView                = contractsresource.HelmChartView
	HelmChartLinkView            = contractsresource.HelmChartLinkView
	HelmChartVersionView         = contractsresource.HelmChartVersionView
	HelmChartDetailView          = contractsresource.HelmChartDetailView
	HelmChartValuesTemplateView  = contractsresource.HelmChartValuesTemplateView
	HelmChartInstallInput        = contractsresource.HelmChartInstallInput
	HelmChartInstallResourceView = contractsresource.HelmChartInstallResourceView
	HelmChartInstallResult       = contractsresource.HelmChartInstallResult
	HelmChartCatalogView         = contractsresource.HelmChartCatalogView

	PortForwardSessionView   = contractsresource.PortForwardSessionView
	PortForwardRegisterInput = contractsresource.PortForwardRegisterInput
)
