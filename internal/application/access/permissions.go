package access

import (
	"slices"
	"sort"
	"strings"
	"sync"
)

const (
	PermWorkspaceApplicationView          = "workspace.application.view"
	PermWorkspaceResourceView             = "workspace.resource.view"
	PermOverviewView                      = "overview.view"
	PermPlatformNodesView                 = "platform.nodes.view"
	PermPlatformNamespacesView            = "platform.namespaces.view"
	PermPlatformWorkloadsView             = "platform.workloads.view"
	PermPlatformConfigurationView         = "platform.configuration.view"
	PermPlatformNetworkView               = "platform.network.view"
	PermPlatformStorageView               = "platform.storage.view"
	PermPlatformExtensionsView            = "platform.extensions.view"
	PermPlatformHelmView                  = "platform.helm.view"
	PermPlatformClustersView              = "platform.clusters.view"
	PermPlatformResourceCreate            = "platform.resource.create"
	PermPlatformDeploymentRestart         = "platform.deployment.restart"
	PermPlatformDeploymentScale           = "platform.deployment.scale"
	PermPlatformDeploymentRollback        = "platform.deployment.rollback"
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
	PermObserveMonitoringView             = "observe.monitoring.view"
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
	PermAIGatewayRelayView                = "ai.gateway.relay.view"
	PermAIGatewayRelayInvoke              = "ai.gateway.relay.invoke"
	PermAIGatewayRelayManage              = "ai.gateway.relay.manage"
	PermPluginView                        = "plugin.view"
	PermPluginInstall                     = "plugin.install"
	PermPluginManage                      = "plugin.manage"
	PermPluginConfigureSecrets            = "plugin.configure_secrets"
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
	PermVirtualizationClustersView        = "virtualization.clusters.view"
	PermVirtualizationClustersManage      = "virtualization.clusters.manage"
	PermVirtualizationImagesView          = "virtualization.images.view"
	PermVirtualizationImagesManage        = "virtualization.images.manage"
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
	PermSettingsMonitoringView            = "settings.monitoring.view"
	PermSettingsMonitoringManage          = "settings.monitoring.manage"
	PermSettingsAIView                    = "settings.ai.view"
	PermSettingsAIManage                  = "settings.ai.manage"
	PermSettingsBrandingView              = "settings.branding.view"
	PermSettingsBrandingManage            = "settings.branding.manage"
)

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
	PermPlatformConfigurationView,
	PermPlatformNetworkView,
	PermPlatformStorageView,
	PermPlatformExtensionsView,
	PermPlatformHelmView,
	PermPlatformClustersView,
	PermPlatformResourceCreate,
	PermPlatformDeploymentRestart,
	PermPlatformDeploymentScale,
	PermPlatformDeploymentRollback,
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
	PermObserveMonitoringView,
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
	PermAIGatewayRelayView,
	PermAIGatewayRelayInvoke,
	PermAIGatewayRelayManage,
	PermPluginView,
	PermPluginInstall,
	PermPluginManage,
	PermPluginConfigureSecrets,
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
	PermVirtualizationClustersView,
	PermVirtualizationClustersManage,
	PermVirtualizationImagesView,
	PermVirtualizationImagesManage,
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
	PermSettingsMonitoringView,
	PermSettingsMonitoringManage,
	PermSettingsAIView,
	PermSettingsAIManage,
	PermSettingsBrandingView,
	PermSettingsBrandingManage,
}

func allPermissionKeys() []string {
	return append([]string(nil), allPermissionKeySet...)
}

var opsRolePermissionKeys = []string{
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
	PermPlatformResourceCreate,
	PermPlatformDeploymentRestart,
	PermPlatformDeploymentScale,
	PermPlatformDeploymentRollback,
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
	PermObserveMonitoringView,
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
	PermIdentityPortalView,
	PermIdentityAuditView,
	PermVirtualizationOverviewView,
	PermVirtualizationVMsView,
	PermVirtualizationVMsManage,
	PermVirtualizationClustersView,
	PermVirtualizationImagesView,
	PermVirtualizationImagesManage,
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
}
var developerRolePermissionKeys = []string{
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
