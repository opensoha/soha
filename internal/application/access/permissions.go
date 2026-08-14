package access

import (
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode"
)

const (
	PermPlatformConfigurationSecretDataView = "platform.configuration.secret-data.view"
	PermPlatformHelmValuesView              = "platform.helm.values.view"
)

const (
	PermWorkspaceApplicationView          = "workspace.application.view"
	PermWorkspaceResourceView             = "workspace.resource.view"
	PermOverviewView                      = "overview.view"
	PermPlatformNodesView                 = "platform.nodes.view"
	PermPlatformNamespacesView            = "platform.namespaces.view"
	PermPlatformWorkloadsView             = "platform.workloads.view"
	PermPlatformWorkloadsOverviewView     = "platform.workloads.overview.view"
	PermPlatformConfigurationView         = "platform.configuration.view"
	PermPlatformNetworkView               = "platform.network.view"
	PermPlatformNetworkTopologyView       = "platform.network.topology.view"
	PermPlatformStorageView               = "platform.storage.view"
	PermPlatformExtensionsView            = "platform.extensions.view"
	PermPlatformHelmView                  = "platform.helm.view"
	PermPlatformClustersView              = "platform.clusters.view"
	PermPlatformResourceCreationUse       = "platform.resource-creation.use"
	PermPlatformDeploymentCreate          = "platform.deployment.create"
	PermPlatformDeploymentDelete          = "platform.deployment.delete"
	PermPlatformDeploymentRestart         = "platform.deployment.restart"
	PermPlatformDeploymentScale           = "platform.deployment.scale"
	PermPlatformDeploymentRollback        = "platform.deployment.rollback"
	PermPlatformDeploymentUpdate          = "platform.deployment.update"
	PermPlatformDeploymentView            = "platform.deployment.view"
	PermPlatformPodsDelete                = "platform.pods.delete"
	PermPlatformPodsExec                  = "platform.pods.exec"
	PermPlatformPodsLogs                  = "platform.pods.logs"
	PermPlatformPodsUpdate                = "platform.pods.update"
	PermPlatformPodsView                  = "platform.pods.view"
	PermPlatformAccessControlView         = "platform.access-control.view"
	PermPlatformRBACManage                = "platform.rbac.manage"
	PermPlatformRBACEscalate              = "platform.rbac.escalate"
	PermPlatformRBACBind                  = "platform.rbac.bind"
	PermPlatformNamespacesManage          = "platform.namespaces.manage"
	PermPlatformCRDsManage                = "platform.crds.manage"
	PermPlatformAdmissionManage           = "platform.admission.manage"
	PermPlatformClusterResourcesManage    = "platform.cluster-resources.manage"
	PermDeliveryApplicationsView          = "delivery.applications.view"
	PermDeliveryApplicationsCreate        = "delivery.application.create"
	PermDeliveryApplicationsUpdate        = "delivery.application.update"
	PermDeliveryApplicationsDelete        = "delivery.application.delete"
	PermDeliveryApplicationServicesView   = "delivery.application-services.view"
	PermDeliveryApplicationServicesManage = "delivery.application-services.manage"
	PermDeliveryApplicationEnvView        = "delivery.application-environments.view"
	PermDeliveryApplicationEnvManage      = "delivery.application-environments.manage"
	PermDeliveryWorkflowTemplatesView     = "delivery.workflow-templates.view"
	PermDeliveryWorkflowTemplatesManage   = "delivery.workflow-templates.manage"
	PermDeliveryBuildTemplatesView        = "delivery.build-templates.view"
	PermDeliveryBuildTemplatesManage      = "delivery.build-templates.manage"
	PermDeliveryBuildsTrigger             = "delivery.builds.trigger"
	PermDeliveryReleaseBundlesView        = "delivery.release-bundles.view"
	PermDeliveryExecutionTasksView        = "delivery.execution-tasks.view"
	PermDeliveryExecutionTasksManage      = "delivery.execution-tasks.manage"
	PermDeliveryReleaseBoardView          = "delivery.release-board.view"
	PermDeliveryWorkflowsView             = "delivery.workflows.view"
	PermDeliveryWorkflowsTrigger          = "delivery.workflows.trigger"
	PermDeliveryReleasesView              = "delivery.releases.view"
	PermDeliveryReleasesTrigger           = "delivery.releases.trigger"
	PermDeliveryRegistriesView            = "delivery.registries.view"
	PermDeliveryRegistriesManage          = "delivery.registries.manage"
	PermDeliveryManifestSourcesManage     = "delivery.manifest-sources.manage"
	PermDeliveryManifestDeploymentsManage = "delivery.manifest-deployments.manage"
	PermDeliveryManifestDriftRepair       = "delivery.manifest-drift.repair"
	PermDeliveryManifestDriftAdopt        = "delivery.manifest-drift.adopt"
	PermDeliveryManifestFieldsForce       = "delivery.manifest-fields.force"
	PermObserveMonitoringView             = "observe.monitoring.view"
	PermObserveDashboardsManage           = "observe.dashboards.manage"
	PermObserveLogDataSourcesView         = "observe.log-data-sources.view"
	PermObserveLogDataSourcesManage       = "observe.log-data-sources.manage"
	PermObserveLogCollectionManage        = "observe.log-collection.manage"
	PermObserveAlertsView                 = "observe.alerts.view"
	PermObserveAlertsAcknowledge          = "observe.alerts.ack"
	PermObserveAlertsAssign               = "observe.alerts.assign"
	PermObserveAlertsManage               = "observe.alerts.manage"
	PermObserveAlertRulesView             = "observe.alert-rules.view"
	PermObserveAlertRulesManage           = "observe.alert-rules.manage"
	PermObserveAlertIntegrationsView      = "observe.alert-integrations.view"
	PermObserveAlertIntegrationsManage    = "observe.alert-integrations.manage"
	PermObserveNotificationsView          = "observe.notifications.view"
	PermObserveNotificationsManage        = "observe.notifications.manage"
	PermObserveOncallView                 = "observe.oncall.view"
	PermObserveOncallManage               = "observe.oncall.manage"
	PermObserveHealingView                = "observe.healing.view"
	PermObserveHealingManage              = "observe.healing.manage"
	PermObserveEventsView                 = "observe.events.view"
	PermObserveAIView                     = "observe.ai.view"
	PermObserveAIChatUse                  = "observe.ai.chat"
	PermObserveAIRootCauseRun             = "observe.ai.root-cause.run"
	PermObserveAIInspectionManage         = "observe.ai.inspection.manage"
	PermObserveAIInspectionRun            = "observe.ai.inspection.run"
	PermAIKnowledgeView                   = "ai.knowledge.view"
	PermAIKnowledgeManage                 = "ai.knowledge.manage"
	PermAIKnowledgeConnectorsView         = "ai.knowledge.connectors.view"
	PermAIKnowledgeConnectorsManage       = "ai.knowledge.connectors.manage"
	PermAIKnowledgeIngestionOperate       = "ai.knowledge.ingestion.operate"
	PermAIKnowledgeRebuild                = "ai.knowledge.rebuild"
	PermAIKnowledgeGraphManage            = "ai.knowledge.graph.manage"
	PermAIContextInspect                  = "ai.context.inspect"
	PermAIEvaluationsView                 = "ai.evaluations.view"
	PermAIEvaluationsManage               = "ai.evaluations.manage"
	PermAIEvaluationsExecute              = "ai.evaluations.execute"
	PermAIEvaluationsGatesManage          = "ai.evaluations.gates.manage"
	PermAIEvaluationsFeedbackCurate       = "ai.evaluations.feedback.curate"
	PermAIAgentProvidersView              = "ai.agent-providers.view"
	PermAIAgentProvidersManage            = "ai.agent-providers.manage"
	PermAIAgentFleetView                  = "ai.agent-fleet.view"
	PermAIAgentFleetManage                = "ai.agent-fleet.manage"
	PermAIEnvironmentsView                = "ai.environments.view"
	PermAIEnvironmentsManage              = "ai.environments.manage"
	PermAIMemoryView                      = "ai.memory.view"
	PermAIMemoryManage                    = "ai.memory.manage"
	PermAIMultiAgentRun                   = "ai.multi-agent.run"
	PermAIOperationsView                  = "ai.operations.view"
	PermAIOperationsManage                = "ai.operations.manage"
	PermAIGatewayView                     = "ai.gateway.view"
	PermAIGatewayInvoke                   = "ai.gateway.invoke"
	PermAIGatewayManage                   = "ai.gateway.manage"
	PermAIGatewayApprovalsManage          = "ai.gateway.approvals.manage"
	PermAIGatewayClientsManage            = "ai.gateway.clients.manage"
	PermAIGatewayGrantsManage             = "ai.gateway.grants.manage"
	PermAIGatewayPoliciesManage           = "ai.gateway.policies.manage"
	PermAIGatewaySkillsManage             = "ai.gateway.skills.manage"
	PermAIGatewayTokensManage             = "ai.gateway.tokens.manage"
	PermAIGatewayRelayView                = "ai.gateway.relay.view"
	PermAIGatewayRelayInvoke              = "ai.gateway.relay.invoke"
	PermAIGatewayRelayManage              = "ai.gateway.relay.manage"
	PermPluginView                        = "plugin.view"
	PermPluginInstall                     = "plugin.install"
	PermPluginManage                      = "plugin.manage"
	PermPluginConfigure                   = "plugin.configure"
	PermPluginConfigureSecrets            = "plugin.configure_secrets"
	PermPluginLifecycle                   = "plugin.lifecycle"
	PermPluginRemove                      = "plugin.remove"
	PermPluginUpgrade                     = "plugin.upgrade"
	PermSecretView                        = "secret.view"
	PermSecretManage                      = "secret.manage"
	PermSecretUse                         = "secret.use"
	PermSecretCreate                      = "secret.create"
	PermSecretRevoke                      = "secret.revoke"
	PermSecretRotate                      = "secret.rotate"
	PermSecretUpdate                      = "secret.update"
	PermIdentityPortalView                = "identity.portal.view"
	PermIdentityApplicationsView          = "identity.applications.view"
	PermIdentityApplicationsManage        = "identity.applications.manage"
	PermIdentityProvidersView             = "identity.providers.view"
	PermIdentityProvidersManage           = "identity.providers.manage"
	PermIdentityOutpostsView              = "identity.outposts.view"
	PermIdentityOutpostsManage            = "identity.outposts.manage"
	PermIdentityPoliciesView              = "identity.policies.view"
	PermIdentityPoliciesManage            = "identity.policies.manage"
	PermIdentityAuditView                 = "identity.audit.view"
	PermVirtualizationOverviewView        = "virtualization.overview.view"
	PermVirtualizationVMsView             = "virtualization.vms.view"
	PermVirtualizationVMsManage           = "virtualization.vms.manage"
	PermVirtualizationVMsCreate           = "virtualization.vms.create"
	PermVirtualizationVMsPower            = "virtualization.vms.power"
	PermVirtualizationVMsResize           = "virtualization.vms.resize"
	PermVirtualizationVMsDelete           = "virtualization.vms.delete"
	PermVirtualizationClustersView        = "virtualization.clusters.view"
	PermVirtualizationClustersManage      = "virtualization.clusters.manage"
	PermVirtualizationImagesView          = "virtualization.images.view"
	PermVirtualizationImagesManage        = "virtualization.images.manage"
	PermVirtualizationStorageView         = "virtualization.storage.view"
	PermVirtualizationFlavorsView         = "virtualization.flavors.view"
	PermVirtualizationFlavorsManage       = "virtualization.flavors.manage"
	PermVirtualizationOperationsView      = "virtualization.operations.view"
	PermVirtualizationOperationsManage    = "virtualization.operations.manage"
	PermVirtualizationSyncView            = "virtualization.sync.view"
	PermVirtualizationSyncManage          = "virtualization.sync.manage"
	PermVirtualizationVMsMetrics          = "virtualization.vms.metrics"
	PermVirtualizationVMsConsole          = "virtualization.vms.console"
	PermDockerOverviewView                = "docker.overview.view"
	PermDockerHostsView                   = "docker.hosts.view"
	PermDockerHostsManage                 = "docker.hosts.manage"
	PermDockerProjectsView                = "docker.projects.view"
	PermDockerProjectsManage              = "docker.projects.manage"
	PermDockerProjectsDeploy              = "docker.projects.deploy"
	PermDockerServicesView                = "docker.services.view"
	PermDockerServicesManage              = "docker.services.manage"
	PermDockerPortsView                   = "docker.ports.view"
	PermDockerPortsManage                 = "docker.ports.manage"
	PermDockerTemplatesView               = "docker.templates.view"
	PermDockerTemplatesManage             = "docker.templates.manage"
	PermDockerOperationsView              = "docker.operations.view"
	PermDockerOperationsManage            = "docker.operations.manage"
	PermAccessUsersView                   = "access.users.view"
	PermAccessUsersManage                 = "access.users.manage"
	PermAccessRolesView                   = "access.roles.view"
	PermAccessRolesManage                 = "access.roles.manage"
	PermAccessGroupsView                  = "access.groups.view"
	PermAccessGroupsManage                = "access.groups.manage"
	PermAccessPoliciesView                = "access.policies.view"
	PermAccessPoliciesManage              = "access.policies.manage"
	PermAccessScopeGrantsView             = "access.scope-grants.view"
	PermAccessScopeGrantsManage           = "access.scope-grants.manage"
	PermAccessDirectoryView               = "access.directory.view"
	PermAccessDirectoryManage             = "access.directory.manage"
	PermAccessDirectorySync               = "access.directory.sync"
	PermAccessDirectoryPeopleManage       = "access.directory.people.manage"
	PermAccessIdentityLinkManage          = "access.identity.link.manage"
	PermSystemOnlineUsersView             = "system.online-users.view"
	PermSystemOnlineUsersManage           = "system.online-users.manage"
	PermSystemAnnouncementsView           = "system.announcements.view"
	PermSystemAnnouncementsManage         = "system.announcements.manage"
	PermSystemMenusView                   = "system.menus.view"
	PermSystemMenusManage                 = "system.menus.manage"
	PermSystemAuditView                   = "system.audit.view"
	PermSystemOperationsView              = "system.operations.view"
	PermSettingsIdentityView              = "settings.identity.view"
	PermSettingsIdentityManage            = "settings.identity.manage"
	PermSettingsAIView                    = "settings.ai.view"
	PermSettingsAIManage                  = "settings.ai.manage"
	PermSettingsBrandingView              = "settings.branding.view"
	PermSettingsBrandingManage            = "settings.branding.manage"
	PermSettingsRuntimeConfigView         = "settings.runtime-config.view"
	PermSettingsRuntimeConfigManage       = "settings.runtime-config.manage"
	PermSettingsSystemIntegrationsView    = "settings.system-integrations.view"
	PermSettingsSystemIntegrationsManage  = "settings.system-integrations.manage"
	PermSoftwarePackageCreate             = "software.package.create"
	PermSoftwarePackageDelete             = "software.package.delete"
	PermSoftwarePackageView               = "software.package.view"
)

// PermPlatformResourceCreate is kept as a source-compatibility alias.
// Deprecated: use PermPlatformResourceCreationUse.
const PermPlatformResourceCreate = PermPlatformResourceCreationUse

var (
	rolePermissionMatrixMu sync.RWMutex
	rolePermissionMatrix   map[string][]string
)

var allPermissionKeySet = []string{
	PermWorkspaceApplicationView,
	PermWorkspaceResourceView,
	PermOverviewView,
	PermPlatformNodesView,
	PermPlatformNamespacesView,
	PermPlatformWorkloadsView,
	PermPlatformWorkloadsOverviewView,
	PermPlatformConfigurationView,
	PermPlatformConfigurationSecretDataView,
	PermPlatformNetworkView,
	PermPlatformNetworkTopologyView,
	PermPlatformStorageView,
	PermPlatformExtensionsView,
	PermPlatformHelmView,
	PermPlatformHelmValuesView,
	PermPlatformClustersView,
	PermPlatformResourceCreationUse,
	PermPlatformDeploymentCreate,
	PermPlatformDeploymentDelete,
	PermPlatformDeploymentRestart,
	PermPlatformDeploymentScale,
	PermPlatformDeploymentRollback,
	PermPlatformDeploymentUpdate,
	PermPlatformDeploymentView,
	PermPlatformPodsDelete,
	PermPlatformPodsExec,
	PermPlatformPodsLogs,
	PermPlatformPodsUpdate,
	PermPlatformPodsView,
	PermPlatformRBACManage,
	PermPlatformRBACEscalate,
	PermPlatformRBACBind,
	PermPlatformNamespacesManage,
	PermPlatformCRDsManage,
	PermPlatformAdmissionManage,
	PermPlatformClusterResourcesManage,
	PermDeliveryApplicationsView,
	PermDeliveryApplicationsCreate,
	PermDeliveryApplicationsUpdate,
	PermDeliveryApplicationsDelete,
	PermDeliveryApplicationServicesView,
	PermDeliveryApplicationServicesManage,
	PermDeliveryApplicationEnvView,
	PermDeliveryApplicationEnvManage,
	PermDeliveryWorkflowTemplatesView,
	PermDeliveryWorkflowTemplatesManage,
	PermDeliveryBuildTemplatesView,
	PermDeliveryBuildTemplatesManage,
	PermDeliveryBuildsTrigger,
	PermDeliveryReleaseBundlesView,
	PermDeliveryExecutionTasksView,
	PermDeliveryExecutionTasksManage,
	PermDeliveryReleaseBoardView,
	PermDeliveryWorkflowsView,
	PermDeliveryWorkflowsTrigger,
	PermDeliveryReleasesView,
	PermDeliveryReleasesTrigger,
	PermDeliveryRegistriesView,
	PermDeliveryRegistriesManage,
	PermDeliveryManifestSourcesManage,
	PermDeliveryManifestDeploymentsManage,
	PermDeliveryManifestDriftRepair,
	PermDeliveryManifestDriftAdopt,
	PermDeliveryManifestFieldsForce,
	PermObserveMonitoringView,
	PermObserveDashboardsManage,
	PermObserveLogDataSourcesView,
	PermObserveLogDataSourcesManage,
	PermObserveLogCollectionManage,
	PermObserveAlertsView,
	PermObserveAlertsAcknowledge,
	PermObserveAlertsAssign,
	PermObserveAlertsManage,
	PermObserveAlertRulesView,
	PermObserveAlertRulesManage,
	PermObserveAlertIntegrationsView,
	PermObserveAlertIntegrationsManage,
	PermObserveNotificationsView,
	PermObserveNotificationsManage,
	PermObserveOncallView,
	PermObserveOncallManage,
	PermObserveHealingView,
	PermObserveHealingManage,
	PermObserveEventsView,
	PermObserveAIView,
	PermObserveAIChatUse,
	PermObserveAIRootCauseRun,
	PermObserveAIInspectionManage,
	PermObserveAIInspectionRun,
	PermAIKnowledgeView,
	PermAIKnowledgeManage,
	PermAIKnowledgeConnectorsView,
	PermAIKnowledgeConnectorsManage,
	PermAIKnowledgeIngestionOperate,
	PermAIKnowledgeRebuild,
	PermAIKnowledgeGraphManage,
	PermAIContextInspect,
	PermAIEvaluationsView,
	PermAIEvaluationsManage,
	PermAIEvaluationsExecute,
	PermAIEvaluationsGatesManage,
	PermAIEvaluationsFeedbackCurate,
	PermAIAgentProvidersView,
	PermAIAgentProvidersManage,
	PermAIAgentFleetView,
	PermAIAgentFleetManage,
	PermAIEnvironmentsView,
	PermAIEnvironmentsManage,
	PermAIMemoryView,
	PermAIMemoryManage,
	PermAIMultiAgentRun,
	PermAIOperationsView,
	PermAIOperationsManage,
	PermAIGatewayView,
	PermAIGatewayInvoke,
	PermAIGatewayManage,
	PermAIGatewayApprovalsManage,
	PermAIGatewayClientsManage,
	PermAIGatewayGrantsManage,
	PermAIGatewayPoliciesManage,
	PermAIGatewaySkillsManage,
	PermAIGatewayTokensManage,
	PermAIGatewayRelayView,
	PermAIGatewayRelayInvoke,
	PermAIGatewayRelayManage,
	PermPluginView,
	PermPluginInstall,
	PermPluginManage,
	PermPluginConfigure,
	PermPluginConfigureSecrets,
	PermPluginLifecycle,
	PermPluginRemove,
	PermPluginUpgrade,
	PermSecretView,
	PermSecretManage,
	PermSecretUse,
	PermSecretCreate,
	PermSecretRevoke,
	PermSecretRotate,
	PermSecretUpdate,
	PermIdentityPortalView,
	PermIdentityApplicationsView,
	PermIdentityApplicationsManage,
	PermIdentityProvidersView,
	PermIdentityProvidersManage,
	PermIdentityOutpostsView,
	PermIdentityOutpostsManage,
	PermIdentityPoliciesView,
	PermIdentityPoliciesManage,
	PermIdentityAuditView,
	PermVirtualizationOverviewView,
	PermVirtualizationVMsView,
	PermVirtualizationVMsManage,
	PermVirtualizationVMsCreate,
	PermVirtualizationVMsPower,
	PermVirtualizationVMsResize,
	PermVirtualizationVMsDelete,
	PermVirtualizationClustersView,
	PermVirtualizationClustersManage,
	PermVirtualizationImagesView,
	PermVirtualizationImagesManage,
	PermVirtualizationStorageView,
	PermVirtualizationFlavorsView,
	PermVirtualizationFlavorsManage,
	PermVirtualizationOperationsView,
	PermVirtualizationOperationsManage,
	PermVirtualizationSyncView,
	PermVirtualizationSyncManage,
	PermVirtualizationVMsMetrics,
	PermVirtualizationVMsConsole,
	PermDockerOverviewView,
	PermDockerHostsView,
	PermDockerHostsManage,
	PermDockerProjectsView,
	PermDockerProjectsManage,
	PermDockerProjectsDeploy,
	PermDockerServicesView,
	PermDockerServicesManage,
	PermDockerPortsView,
	PermDockerPortsManage,
	PermDockerTemplatesView,
	PermDockerTemplatesManage,
	PermDockerOperationsView,
	PermDockerOperationsManage,
	PermAccessUsersView,
	PermAccessUsersManage,
	PermAccessRolesView,
	PermAccessRolesManage,
	PermAccessGroupsView,
	PermAccessGroupsManage,
	PermAccessPoliciesView,
	PermAccessPoliciesManage,
	PermAccessScopeGrantsView,
	PermAccessScopeGrantsManage,
	PermAccessDirectoryView,
	PermAccessDirectoryManage,
	PermAccessDirectorySync,
	PermAccessDirectoryPeopleManage,
	PermAccessIdentityLinkManage,
	PermSystemOnlineUsersView,
	PermSystemOnlineUsersManage,
	PermSystemAnnouncementsView,
	PermSystemAnnouncementsManage,
	PermSystemMenusView,
	PermSystemMenusManage,
	PermSystemAuditView,
	PermSystemOperationsView,
	PermSettingsIdentityView,
	PermSettingsIdentityManage,
	PermSettingsAIView,
	PermSettingsAIManage,
	PermSettingsBrandingView,
	PermSettingsBrandingManage,
	PermSettingsRuntimeConfigView,
	PermSettingsRuntimeConfigManage,
	PermSettingsSystemIntegrationsView,
	PermSettingsSystemIntegrationsManage,
	PermSoftwarePackageCreate,
	PermSoftwarePackageDelete,
	PermSoftwarePackageView,
}

func allPermissionKeys() []string {
	keys := append([]string(nil), allPermissionKeySet...)
	if catalog, err := loadPermissionCatalog(); err == nil {
		for _, permission := range catalog.Permissions {
			keys = append(keys, permission.Key)
		}
	}
	return normalizePermissionKeys(keys)
}

func ManagedActionPermission(managePermissionKey, action string) string {
	managePermissionKey = strings.TrimSpace(managePermissionKey)
	action = strings.TrimSpace(action)
	if !strings.HasSuffix(managePermissionKey, ".manage") || action == "" {
		panic("managed action permission requires a .manage key and non-empty action")
	}
	return strings.TrimSuffix(managePermissionKey, ".manage") + "." + action
}

func PlatformActionPermission(resourceGroup, kind, action string) string {
	resourceGroup = strings.TrimSpace(resourceGroup)
	kind = strings.TrimSpace(kind)
	action = strings.TrimSpace(action)
	if kind == "" || action == "" {
		panic("platform action permission requires non-empty kind and action")
	}

	var resource string
	switch strings.ToLower(kind) {
	case "deployment":
		resource = "deployment"
	case "pod":
		resource = "pods"
	case "cluster":
		resource = "clusters"
	case "namespace":
		resource = "namespaces"
	case "node":
		resource = "nodes"
	case "helmrelease":
		resource = "helm.releases"
	case "logcollection":
		resource = "observability.logging"
	case "crd", "customresourcedefinition":
		resource = "extensions.crds"
	default:
		if resourceGroup == "extensions" {
			resource = "extensions.custom-resources"
		} else {
			resource = resourceGroup + "." + pluralPermissionResource(kind)
		}
	}
	return "platform." + strings.Trim(resource, ".") + "." + action
}

func pluralPermissionResource(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "httproute":
		return "http-routes"
	case "grpcroute":
		return "grpc-routes"
	case "backendtlspolicy":
		return "backend-tls-policies"
	}
	runes := []rune(strings.TrimSpace(kind))
	var name strings.Builder
	for index, current := range runes {
		if index > 0 && unicode.IsUpper(current) && (unicode.IsLower(runes[index-1]) || unicode.IsDigit(runes[index-1])) {
			name.WriteByte('-')
		}
		name.WriteRune(unicode.ToLower(current))
	}
	value := name.String()
	switch {
	case strings.HasSuffix(value, "y") && len(value) > 1 && !strings.ContainsRune("aeiou", rune(value[len(value)-2])):
		return strings.TrimSuffix(value, "y") + "ies"
	case strings.HasSuffix(value, "s"), strings.HasSuffix(value, "x"), strings.HasSuffix(value, "z"), strings.HasSuffix(value, "ch"), strings.HasSuffix(value, "sh"):
		return value + "es"
	default:
		return value + "s"
	}
}

var opsRolePermissionKeys = []string{
	PermSoftwarePackageView,
	PermWorkspaceApplicationView,
	PermWorkspaceResourceView,
	PermOverviewView,
	PermPlatformNodesView,
	PermPlatformNamespacesView,
	PermPlatformWorkloadsView,
	PermPlatformConfigurationView,
	PermPlatformConfigurationSecretDataView,
	PermPlatformNetworkView,
	PermPlatformStorageView,
	PermPlatformExtensionsView,
	PermPlatformHelmView,
	PermPlatformHelmValuesView,
	PermPlatformClustersView,
	PermPlatformResourceCreationUse,
	PermPlatformDeploymentCreate,
	PermPlatformDeploymentRestart,
	PermPlatformDeploymentScale,
	PermPlatformDeploymentRollback,
	PermPlatformDeploymentUpdate,
	PermPlatformDeploymentView,
	PermPlatformPodsLogs,
	PermPlatformPodsView,
	PermDeliveryApplicationsView,
	PermDeliveryApplicationsCreate,
	PermDeliveryApplicationsUpdate,
	PermDeliveryApplicationServicesView,
	PermDeliveryApplicationServicesManage,
	PermDeliveryApplicationEnvView,
	PermDeliveryApplicationEnvManage,
	PermDeliveryWorkflowTemplatesView,
	PermDeliveryWorkflowTemplatesManage,
	PermDeliveryBuildTemplatesView,
	PermDeliveryBuildTemplatesManage,
	PermDeliveryBuildsTrigger,
	PermDeliveryReleaseBundlesView,
	PermDeliveryExecutionTasksView,
	PermDeliveryExecutionTasksManage,
	PermDeliveryReleaseBoardView,
	PermDeliveryWorkflowsView,
	PermDeliveryWorkflowsTrigger,
	PermDeliveryReleasesView,
	PermDeliveryReleasesTrigger,
	PermDeliveryRegistriesView,
	PermDeliveryRegistriesManage,
	PermDeliveryManifestSourcesManage,
	PermDeliveryManifestDeploymentsManage,
	PermDeliveryManifestDriftRepair,
	PermDeliveryManifestDriftAdopt,
	PermObserveMonitoringView,
	PermObserveLogDataSourcesView,
	PermObserveLogDataSourcesManage,
	PermObserveAlertsView,
	PermObserveAlertsAcknowledge,
	PermObserveAlertsAssign,
	PermObserveAlertsManage,
	PermObserveAlertRulesView,
	PermObserveAlertRulesManage,
	PermObserveAlertIntegrationsView,
	PermObserveAlertIntegrationsManage,
	PermObserveNotificationsView,
	PermObserveNotificationsManage,
	PermObserveOncallView,
	PermObserveOncallManage,
	PermObserveHealingView,
	PermObserveHealingManage,
	PermObserveEventsView,
	PermObserveAIView,
	PermObserveAIChatUse,
	PermObserveAIRootCauseRun,
	PermObserveAIInspectionManage,
	PermObserveAIInspectionRun,
	PermAIKnowledgeView,
	PermAIKnowledgeManage,
	PermAIKnowledgeConnectorsView,
	PermAIKnowledgeConnectorsManage,
	PermAIKnowledgeIngestionOperate,
	PermAIContextInspect,
	PermAIEvaluationsView,
	PermAIEvaluationsManage,
	PermAIAgentProvidersView,
	PermAIAgentProvidersManage,
	PermAIGatewayView,
	PermAIGatewayInvoke,
	PermAIGatewayManage,
	PermAIGatewayRelayView,
	PermAIGatewayRelayInvoke,
	PermAIGatewayRelayManage,
	PermPluginView,
	PermPluginInstall,
	PermPluginManage,
	PermPluginConfigureSecrets,
	PermSecretView,
	PermSecretManage,
	PermSecretUse,
	PermIdentityPortalView,
	PermIdentityAuditView,
	PermVirtualizationOverviewView,
	PermVirtualizationVMsView,
	PermVirtualizationVMsManage,
	PermVirtualizationVMsCreate,
	PermVirtualizationVMsPower,
	PermVirtualizationVMsResize,
	PermVirtualizationVMsDelete,
	PermVirtualizationClustersView,
	PermVirtualizationImagesView,
	PermVirtualizationImagesManage,
	PermVirtualizationStorageView,
	PermVirtualizationFlavorsView,
	PermVirtualizationOperationsView,
	PermVirtualizationSyncView,
	PermVirtualizationSyncManage,
	PermVirtualizationVMsMetrics,
	PermVirtualizationVMsConsole,
	PermDockerOverviewView,
	PermDockerHostsView,
	PermDockerHostsManage,
	PermDockerProjectsView,
	PermDockerProjectsManage,
	PermDockerProjectsDeploy,
	PermDockerServicesView,
	PermDockerServicesManage,
	PermDockerPortsView,
	PermDockerPortsManage,
	PermDockerTemplatesView,
	PermDockerTemplatesManage,
	PermDockerOperationsView,
	PermDockerOperationsManage,
	PermSystemAuditView,
	PermSystemOperationsView,
	PermAccessUsersView,
	PermAccessUsersManage,
	PermAccessRolesView,
	PermAccessRolesManage,
	PermAccessGroupsView,
	PermAccessGroupsManage,
	PermAccessPoliciesView,
	PermAccessPoliciesManage,
	PermAccessScopeGrantsView,
	PermAccessScopeGrantsManage,
	PermAccessDirectoryView,
	PermAccessDirectoryManage,
	PermAccessDirectorySync,
	PermAccessDirectoryPeopleManage,
	PermAccessIdentityLinkManage,
	PermSettingsAIView,
	PermSettingsAIManage,
	PermSettingsBrandingView,
	PermSettingsBrandingManage,
	PermSettingsRuntimeConfigView,
}
var developerRolePermissionKeys = []string{
	PermSoftwarePackageView,
	PermWorkspaceApplicationView,
	PermWorkspaceResourceView,
	PermOverviewView,
	PermPlatformNodesView,
	PermPlatformNamespacesView,
	PermPlatformWorkloadsView,
	PermPlatformConfigurationView,
	PermPlatformNetworkView,
	PermPlatformStorageView,
	PermPlatformExtensionsView,
	PermPlatformHelmView,
	PermPlatformDeploymentRestart,
	PermPlatformDeploymentScale,
	PermPlatformDeploymentView,
	PermPlatformPodsLogs,
	PermPlatformPodsView,
	PermObserveMonitoringView,
	PermObserveAlertsView,
	PermObserveAlertsAcknowledge,
	PermObserveAlertIntegrationsView,
	PermObserveEventsView,
	PermObserveAIView,
	PermObserveAIChatUse,
	PermObserveAIRootCauseRun,
	PermObserveAIInspectionRun,
	PermAIKnowledgeView,
	PermAIKnowledgeConnectorsView,
	PermAIContextInspect,
	PermAIEvaluationsView,
	PermAIEvaluationsManage,
	PermAIAgentProvidersView,
	PermAIGatewayView,
	PermAIGatewayInvoke,
	PermAIGatewayRelayView,
	PermAIGatewayRelayInvoke,
	PermSecretView,
	PermSecretUse,
	PermPluginView,
	PermIdentityPortalView,
	PermDeliveryApplicationsView,
	PermDeliveryApplicationServicesView,
	PermDeliveryApplicationEnvView,
	PermDeliveryWorkflowTemplatesView,
	PermDeliveryBuildTemplatesView,
	PermDeliveryBuildsTrigger,
	PermDeliveryReleaseBundlesView,
	PermDeliveryExecutionTasksView,
	PermDeliveryReleaseBoardView,
	PermDeliveryWorkflowsView,
	PermDeliveryWorkflowsTrigger,
	PermDeliveryReleasesView,
	PermDeliveryReleasesTrigger,
	PermDeliveryManifestSourcesManage,
	PermDeliveryManifestDeploymentsManage,
	PermDockerOverviewView,
	PermDockerHostsView,
	PermDockerProjectsView,
	PermDockerProjectsManage,
	PermDockerProjectsDeploy,
	PermDockerServicesView,
	PermDockerServicesManage,
	PermDockerPortsView,
	PermDockerPortsManage,
	PermDockerTemplatesView,
	PermDockerOperationsView,
}
var testerRolePermissionKeys = []string{
	PermSoftwarePackageView,
	PermWorkspaceApplicationView,
	PermOverviewView,
	PermDeliveryApplicationsView,
	PermDeliveryApplicationServicesView,
	PermDeliveryApplicationEnvView,
	PermDeliveryReleaseBundlesView,
	PermDeliveryExecutionTasksView,
	PermIdentityPortalView,
}
var readonlyRolePermissionKeys = []string{
	PermSoftwarePackageView,
	PermWorkspaceApplicationView,
	PermWorkspaceResourceView,
	PermOverviewView,
	PermPlatformNodesView,
	PermPlatformNamespacesView,
	PermPlatformWorkloadsView,
	PermPlatformConfigurationView,
	PermPlatformNetworkView,
	PermPlatformStorageView,
	PermPlatformExtensionsView,
	PermPlatformHelmView,
	PermPlatformClustersView,
	PermPlatformDeploymentView,
	PermPlatformPodsLogs,
	PermPlatformPodsView,
	PermDeliveryApplicationsView,
	PermDeliveryApplicationServicesView,
	PermDeliveryApplicationEnvView,
	PermDeliveryReleaseBundlesView,
	PermDeliveryExecutionTasksView,
	PermDeliveryReleaseBoardView,
	PermDeliveryWorkflowsView,
	PermDeliveryReleasesView,
	PermObserveMonitoringView,
	PermObserveAlertsView,
	PermObserveAlertIntegrationsView,
	PermObserveEventsView,
	PermObserveAIView,
	PermAIKnowledgeView,
	PermAIKnowledgeConnectorsView,
	PermAIContextInspect,
	PermAIEvaluationsView,
	PermAIAgentProvidersView,
	PermAIGatewayView,
	PermAIGatewayRelayView,
	PermSecretView,
	PermPluginView,
	PermIdentityPortalView,
	PermDockerOverviewView,
	PermDockerHostsView,
	PermDockerProjectsView,
	PermDockerServicesView,
	PermDockerPortsView,
	PermDockerTemplatesView,
	PermDockerOperationsView,
}
var auditorRolePermissionKeys = []string{
	PermSoftwarePackageView,
	PermWorkspaceResourceView,
	PermOverviewView,
	PermObserveMonitoringView,
	PermObserveAlertsView,
	PermObserveAlertIntegrationsView,
	PermObserveNotificationsView,
	PermObserveEventsView,
	PermSystemAuditView,
	PermSystemOperationsView,
	PermPluginView,
	PermIdentityPortalView,
	PermIdentityAuditView,
}

func defaultRolePermissions() map[string][]string {
	return map[string][]string{
		"admin":     allPermissionKeys(),
		"ops":       append([]string(nil), opsRolePermissionKeys...),
		"developer": append([]string(nil), developerRolePermissionKeys...),
		"tester":    append([]string(nil), testerRolePermissionKeys...),
		"readonly":  append([]string(nil), readonlyRolePermissionKeys...),
		"auditor":   append([]string(nil), auditorRolePermissionKeys...),
	}
}

func normalizePermissionKeys(permissionKeys []string) []string {
	keys := make([]string, 0, len(permissionKeys))
	for _, permissionKey := range permissionKeys {
		value := strings.TrimSpace(permissionKey)
		if value == "" || slices.Contains(keys, value) {
			continue
		}
		keys = append(keys, value)
	}
	sort.Strings(keys)
	return keys
}

func SetRolePermissionMatrix(matrix map[string][]string) {
	rolePermissionMatrixMu.Lock()
	defer rolePermissionMatrixMu.Unlock()
	if len(matrix) == 0 {
		rolePermissionMatrix = nil
		return
	}
	rolePermissionMatrix = make(map[string][]string, len(matrix))
	for roleID, keys := range matrix {
		rolePermissionMatrix[strings.TrimSpace(roleID)] = normalizePermissionKeys(keys)
	}
}

func SetRolePermissionKeys(roleID string, permissionKeys []string) {
	rolePermissionMatrixMu.Lock()
	defer rolePermissionMatrixMu.Unlock()
	if rolePermissionMatrix == nil {
		rolePermissionMatrix = map[string][]string{}
	}
	rolePermissionMatrix[strings.TrimSpace(roleID)] = normalizePermissionKeys(permissionKeys)
}

func DeleteRolePermissionKeys(roleID string) {
	rolePermissionMatrixMu.Lock()
	defer rolePermissionMatrixMu.Unlock()
	delete(rolePermissionMatrix, strings.TrimSpace(roleID))
	if len(rolePermissionMatrix) == 0 {
		rolePermissionMatrix = nil
	}
}

func effectiveRolePermissionMatrix() map[string][]string {
	matrix := defaultRolePermissions()
	rolePermissionMatrixMu.RLock()
	defer rolePermissionMatrixMu.RUnlock()
	for roleID, keys := range rolePermissionMatrix {
		matrix[roleID] = append([]string(nil), keys...)
	}
	return matrix
}

func PermissionKeysForRoles(roles []string) []string {
	matrix := effectiveRolePermissionMatrix()
	keys := make([]string, 0)
	for _, role := range roles {
		for _, permission := range matrix[strings.TrimSpace(role)] {
			if !slices.Contains(keys, permission) {
				keys = append(keys, permission)
			}
		}
	}
	return normalizePermissionKeys(keys)
}

func HasPermission(roles []string, permissionKey string) bool {
	if strings.TrimSpace(permissionKey) == "" {
		return true
	}
	return slices.Contains(PermissionKeysForRoles(roles), strings.TrimSpace(permissionKey))
}
