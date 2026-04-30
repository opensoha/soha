package access

import (
	"slices"
	"sort"
	"strings"
	"sync"
)

const (
	PermOverviewView                    = "overview.view"
	PermPlatformNodesView               = "platform.nodes.view"
	PermPlatformNamespacesView          = "platform.namespaces.view"
	PermPlatformWorkloadsView           = "platform.workloads.view"
	PermPlatformConfigurationView       = "platform.configuration.view"
	PermPlatformNetworkView             = "platform.network.view"
	PermPlatformStorageView             = "platform.storage.view"
	PermPlatformExtensionsView          = "platform.extensions.view"
	PermPlatformHelmView                = "platform.helm.view"
	PermPlatformClustersView            = "platform.clusters.view"
	PermPlatformDeploymentRestart       = "platform.deployment.restart"
	PermPlatformDeploymentScale         = "platform.deployment.scale"
	PermPlatformDeploymentRollback      = "platform.deployment.rollback"
	PermDeliveryApplicationsView        = "delivery.applications.view"
	PermDeliveryApplicationsCreate      = "delivery.application.create"
	PermDeliveryApplicationsUpdate      = "delivery.application.update"
	PermDeliveryApplicationsDelete      = "delivery.application.delete"
	PermDeliveryBusinessLinesView       = "delivery.business-lines.view"
	PermDeliveryBusinessLinesManage     = "delivery.business-lines.manage"
	PermDeliveryEnvironmentsView        = "delivery.environments.view"
	PermDeliveryEnvironmentsManage      = "delivery.environments.manage"
	PermDeliveryApplicationEnvView      = "delivery.application-environments.view"
	PermDeliveryApplicationEnvManage    = "delivery.application-environments.manage"
	PermDeliveryWorkflowTemplatesView   = "delivery.workflow-templates.view"
	PermDeliveryWorkflowTemplatesManage = "delivery.workflow-templates.manage"
	PermDeliveryReleaseBoardView        = "delivery.release-board.view"
	PermDeliveryWorkflowsView           = "delivery.workflows.view"
	PermDeliveryWorkflowsTrigger        = "delivery.workflows.trigger"
	PermDeliveryReleasesView            = "delivery.releases.view"
	PermDeliveryReleasesTrigger         = "delivery.releases.trigger"
	PermDeliveryRegistriesView          = "delivery.registries.view"
	PermDeliveryRegistriesManage        = "delivery.registries.manage"
	PermObserveMonitoringView           = "observe.monitoring.view"
	PermObserveAlertsView               = "observe.alerts.view"
	PermObserveAlertsAcknowledge        = "observe.alerts.ack"
	PermObserveAlertsAssign             = "observe.alerts.assign"
	PermObserveNotificationsView        = "observe.notifications.view"
	PermObserveNotificationsManage      = "observe.notifications.manage"
	PermObserveOncallView               = "observe.oncall.view"
	PermObserveEventsView               = "observe.events.view"
	PermObserveAIView                   = "observe.ai.view"
	PermObserveAIChatUse                = "observe.ai.chat"
	PermObserveAIRootCauseRun           = "observe.ai.root-cause.run"
	PermObserveAIInspectionManage       = "observe.ai.inspection.manage"
	PermObserveAIInspectionRun          = "observe.ai.inspection.run"
	PermAccessUsersView                 = "access.users.view"
	PermAccessUsersManage               = "access.users.manage"
	PermAccessRolesView                 = "access.roles.view"
	PermAccessRolesManage               = "access.roles.manage"
	PermAccessGroupsView                = "access.groups.view"
	PermAccessGroupsManage              = "access.groups.manage"
	PermAccessPoliciesView              = "access.policies.view"
	PermAccessPoliciesManage            = "access.policies.manage"
	PermAccessScopeGrantsView           = "access.scope-grants.view"
	PermAccessScopeGrantsManage         = "access.scope-grants.manage"
	PermSystemOnlineUsersView           = "system.online-users.view"
	PermSystemOnlineUsersManage         = "system.online-users.manage"
	PermSystemAnnouncementsView         = "system.announcements.view"
	PermSystemAnnouncementsManage       = "system.announcements.manage"
	PermSystemMenusView                 = "system.menus.view"
	PermSystemMenusManage               = "system.menus.manage"
	PermSystemAuditView                 = "system.audit.view"
	PermSystemOperationsView            = "system.operations.view"
	PermSettingsIdentityView            = "settings.identity.view"
	PermSettingsIdentityManage          = "settings.identity.manage"
	PermSettingsMonitoringView          = "settings.monitoring.view"
	PermSettingsMonitoringManage        = "settings.monitoring.manage"
	PermSettingsAIView                  = "settings.ai.view"
	PermSettingsAIManage                = "settings.ai.manage"
	PermSettingsBrandingView            = "settings.branding.view"
	PermSettingsBrandingManage          = "settings.branding.manage"
)

var (
	rolePermissionMatrixMu sync.RWMutex
	rolePermissionMatrix   map[string][]string
)

func allPermissionKeys() []string {
	return []string{
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
		PermPlatformDeploymentRestart,
		PermPlatformDeploymentScale,
		PermPlatformDeploymentRollback,
		PermDeliveryApplicationsView,
		PermDeliveryApplicationsCreate,
		PermDeliveryApplicationsUpdate,
		PermDeliveryApplicationsDelete,
		PermDeliveryBusinessLinesView,
		PermDeliveryBusinessLinesManage,
		PermDeliveryEnvironmentsView,
		PermDeliveryEnvironmentsManage,
		PermDeliveryApplicationEnvView,
		PermDeliveryApplicationEnvManage,
		PermDeliveryWorkflowTemplatesView,
		PermDeliveryWorkflowTemplatesManage,
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
		PermObserveNotificationsView,
		PermObserveNotificationsManage,
		PermObserveOncallView,
		PermObserveEventsView,
		PermObserveAIView,
		PermObserveAIChatUse,
		PermObserveAIRootCauseRun,
		PermObserveAIInspectionManage,
		PermObserveAIInspectionRun,
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
}

func defaultRolePermissions() map[string][]string {
	admin := allPermissionKeys()
	ops := []string{
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
		PermPlatformDeploymentRestart,
		PermPlatformDeploymentScale,
		PermPlatformDeploymentRollback,
		PermDeliveryApplicationsView,
		PermDeliveryApplicationsCreate,
		PermDeliveryApplicationsUpdate,
		PermDeliveryBusinessLinesView,
		PermDeliveryBusinessLinesManage,
		PermDeliveryEnvironmentsView,
		PermDeliveryEnvironmentsManage,
		PermDeliveryApplicationEnvView,
		PermDeliveryApplicationEnvManage,
		PermDeliveryWorkflowTemplatesView,
		PermDeliveryWorkflowTemplatesManage,
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
		PermObserveNotificationsView,
		PermObserveNotificationsManage,
		PermObserveOncallView,
		PermObserveEventsView,
		PermObserveAIView,
		PermObserveAIChatUse,
		PermObserveAIRootCauseRun,
		PermObserveAIInspectionManage,
		PermObserveAIInspectionRun,
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
		PermSettingsAIView,
		PermSettingsAIManage,
		PermSettingsBrandingView,
		PermSettingsBrandingManage,
	}
	developer := []string{
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
		PermObserveEventsView,
		PermObserveAIView,
		PermObserveAIChatUse,
		PermObserveAIRootCauseRun,
		PermObserveAIInspectionRun,
		PermDeliveryApplicationsView,
		PermDeliveryReleaseBoardView,
		PermDeliveryWorkflowsView,
		PermDeliveryWorkflowsTrigger,
		PermDeliveryReleasesView,
		PermDeliveryReleasesTrigger,
	}
	readonly := []string{
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
		PermDeliveryReleaseBoardView,
		PermDeliveryWorkflowsView,
		PermDeliveryReleasesView,
		PermObserveMonitoringView,
		PermObserveAlertsView,
		PermObserveEventsView,
		PermObserveAIView,
	}
	auditor := []string{
		PermOverviewView,
		PermObserveMonitoringView,
		PermObserveAlertsView,
		PermObserveNotificationsView,
		PermObserveEventsView,
		PermSystemAuditView,
		PermSystemOperationsView,
	}
	return map[string][]string{
		"admin":     admin,
		"ops":       ops,
		"developer": developer,
		"readonly":  readonly,
		"auditor":   auditor,
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
