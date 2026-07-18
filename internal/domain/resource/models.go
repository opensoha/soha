package resource

import contractsresource "github.com/opensoha/soha-contracts/resource"

const (
	PodLogsMaxContentBytes = contractsresource.PodLogsMaxContentBytes
	PodExecMaxOutputBytes  = contractsresource.PodExecMaxOutputBytes
	StorageRelationLimit   = contractsresource.StorageRelationLimit
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

	DeploymentView                  = contractsresource.DeploymentView
	DeploymentDetailView            = contractsresource.DeploymentDetailView
	DeploymentRolloutStatusView     = contractsresource.DeploymentRolloutStatusView
	DeploymentRollbackView          = contractsresource.DeploymentRollbackView
	RolloutHistoryView              = contractsresource.RolloutHistoryView
	StatefulSetView                 = contractsresource.StatefulSetView
	StatefulSetDetailView           = contractsresource.StatefulSetDetailView
	DaemonSetView                   = contractsresource.DaemonSetView
	DaemonSetDetailView             = contractsresource.DaemonSetDetailView
	JobView                         = contractsresource.JobView
	JobDetailView                   = contractsresource.JobDetailView
	CronJobView                     = contractsresource.CronJobView
	CronJobDetailView               = contractsresource.CronJobDetailView
	ReplicaSetView                  = contractsresource.ReplicaSetView
	ReplicaSetDetailView            = contractsresource.ReplicaSetDetailView
	ReplicationControllerView       = contractsresource.ReplicationControllerView
	ReplicationControllerDetailView = contractsresource.ReplicationControllerDetailView
	WorkloadRelationView            = contractsresource.WorkloadRelationView

	ServiceView                  = contractsresource.ServiceView
	ServiceDetailView            = contractsresource.ServiceDetailView
	ServiceEndpointView          = contractsresource.ServiceEndpointView
	IngressView                  = contractsresource.IngressView
	IngressDetailView            = contractsresource.IngressDetailView
	IngressRouteView             = contractsresource.IngressRouteView
	IngressBackendView           = contractsresource.IngressBackendView
	NetworkRelatedPodView        = contractsresource.NetworkRelatedPodView
	GatewayView                  = contractsresource.GatewayView
	GatewayDetailView            = contractsresource.GatewayDetailView
	GatewayClassView             = contractsresource.GatewayClassView
	GatewayClassDetailView       = contractsresource.GatewayClassDetailView
	GatewayListenerView          = contractsresource.GatewayListenerView
	GatewayRouteReferenceView    = contractsresource.GatewayRouteReferenceView
	GatewayRouteParentStatusView = contractsresource.GatewayRouteParentStatusView
	HTTPRouteView                = contractsresource.HTTPRouteView
	HTTPRouteDetailView          = contractsresource.HTTPRouteDetailView
	GatewayRouteRuleView         = contractsresource.GatewayRouteRuleView
	GatewayRouteBackendView      = contractsresource.GatewayRouteBackendView
	BackendTLSPolicyView         = contractsresource.BackendTLSPolicyView
	BackendTLSPolicyDetailView   = contractsresource.BackendTLSPolicyDetailView
	GRPCRouteView                = contractsresource.GRPCRouteView
	GRPCRouteDetailView          = contractsresource.GRPCRouteDetailView
	ReferenceGrantView           = contractsresource.ReferenceGrantView
	ReferenceGrantDetailView     = contractsresource.ReferenceGrantDetailView
	ReferenceGrantFromView       = contractsresource.ReferenceGrantFromView
	ReferenceGrantToView         = contractsresource.ReferenceGrantToView
	EndpointSliceView            = contractsresource.EndpointSliceView
	EndpointSliceDetailView      = contractsresource.EndpointSliceDetailView
	NetworkPolicyView            = contractsresource.NetworkPolicyView
	NetworkPolicyDetailView      = contractsresource.NetworkPolicyDetailView
	NetworkPolicyPeerView        = contractsresource.NetworkPolicyPeerView
	NetworkPolicyPortView        = contractsresource.NetworkPolicyPortView
	NetworkPolicyRuleView        = contractsresource.NetworkPolicyRuleView
	IngressClassView             = contractsresource.IngressClassView
	IngressClassDetailView       = contractsresource.IngressClassDetailView
	NetworkTopologyView          = contractsresource.NetworkTopologyView
	NetworkTopologyNodeView      = contractsresource.NetworkTopologyNodeView
	NetworkTopologyTraceView     = contractsresource.NetworkTopologyTraceView
	NetworkTopologySummaryView   = contractsresource.NetworkTopologySummaryView

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
	StoragePodReferenceView         = contractsresource.StoragePodReferenceView
	PersistentVolumeClaimDetailView = contractsresource.PersistentVolumeClaimDetailView
	PersistentVolumeDetailView      = contractsresource.PersistentVolumeDetailView
	StorageClassDetailView          = contractsresource.StorageClassDetailView

	CRDView                                 = contractsresource.CRDView
	CustomResourceView                      = contractsresource.CustomResourceView
	ConfigMapView                           = contractsresource.ConfigMapView
	ConfigMapDetailView                     = contractsresource.ConfigMapDetailView
	SecretView                              = contractsresource.SecretView
	SecretDetailView                        = contractsresource.SecretDetailView
	ServiceAccountView                      = contractsresource.ServiceAccountView
	ServiceAccountDetailView                = contractsresource.ServiceAccountDetailView
	RoleView                                = contractsresource.RoleView
	RoleDetailView                          = contractsresource.RoleDetailView
	RoleBindingView                         = contractsresource.RoleBindingView
	RoleBindingDetailView                   = contractsresource.RoleBindingDetailView
	ClusterRoleView                         = contractsresource.ClusterRoleView
	ClusterRoleDetailView                   = contractsresource.ClusterRoleDetailView
	ClusterRoleBindingView                  = contractsresource.ClusterRoleBindingView
	ClusterRoleBindingDetailView            = contractsresource.ClusterRoleBindingDetailView
	MutatingWebhookConfigurationView        = contractsresource.MutatingWebhookConfigurationView
	ValidatingWebhookConfigurationView      = contractsresource.ValidatingWebhookConfigurationView
	AdmissionWebhookRuleView                = contractsresource.AdmissionWebhookRuleView
	AdmissionWebhookView                    = contractsresource.AdmissionWebhookView
	AdmissionWebhookConfigurationDetailView = contractsresource.AdmissionWebhookConfigurationDetailView
	ResourceQuotaView                       = contractsresource.ResourceQuotaView
	ResourceQuotaDetailView                 = contractsresource.ResourceQuotaDetailView
	LimitRangeView                          = contractsresource.LimitRangeView
	LimitRangeRuleView                      = contractsresource.LimitRangeRuleView
	LimitRangeDetailView                    = contractsresource.LimitRangeDetailView
	LeaseView                               = contractsresource.LeaseView
	HorizontalPodAutoscalerView             = contractsresource.HorizontalPodAutoscalerView
	HorizontalPodAutoscalerMetricView       = contractsresource.HorizontalPodAutoscalerMetricView
	HorizontalPodAutoscalerDetailView       = contractsresource.HorizontalPodAutoscalerDetailView
	PodDisruptionBudgetView                 = contractsresource.PodDisruptionBudgetView
	PodDisruptionBudgetDetailView           = contractsresource.PodDisruptionBudgetDetailView
	PriorityClassView                       = contractsresource.PriorityClassView
	RuntimeClassView                        = contractsresource.RuntimeClassView

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

type ConfigReferenceView struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Path      string `json:"path"`
}
