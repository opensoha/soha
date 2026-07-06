package menu

import (
	"slices"
	"strings"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainmenu "github.com/opensoha/soha/internal/domain/menu"
)

type visibilityRule struct {
	permissions []string
}

func workspacePermissionForMenu(item domainmenu.Record) string {
	path := strings.TrimSpace(item.Path)
	switch {
	case path == "/" ||
		strings.HasPrefix(path, "/cluster-resources") ||
		strings.HasPrefix(path, "/workloads") ||
		strings.HasPrefix(path, "/configuration") ||
		strings.HasPrefix(path, "/network") ||
		strings.HasPrefix(path, "/storage") ||
		strings.HasPrefix(path, "/platform-access-control") ||
		strings.HasPrefix(path, "/helm") ||
		strings.HasPrefix(path, "/extensions") ||
		strings.HasPrefix(path, "/clusters") ||
		strings.HasPrefix(path, "/monitoring-workbench") ||
		strings.HasPrefix(path, "/ai-gateway") ||
		strings.HasPrefix(path, "/plugins") ||
		strings.HasPrefix(path, "/ai-workbench") ||
		strings.HasPrefix(path, "/virtualization") ||
		strings.HasPrefix(path, "/observability") ||
		strings.HasPrefix(path, "/ai-observe") ||
		strings.HasPrefix(path, "/chat"):
		return appaccess.PermWorkspaceResourceView
	case strings.HasPrefix(path, "/applications") ||
		strings.HasPrefix(path, "/application-environments") ||
		strings.HasPrefix(path, "/build-templates") ||
		strings.HasPrefix(path, "/delivery/onboarding") ||
		strings.HasPrefix(path, "/delivery/testing") ||
		strings.HasPrefix(path, "/delivery/analysis") ||
		strings.HasPrefix(path, "/delivery/blueprints") ||
		strings.HasPrefix(path, "/delivery/release-bundles") ||
		strings.HasPrefix(path, "/delivery/execution-tasks") ||
		strings.HasPrefix(path, "/workflow-templates") ||
		strings.HasPrefix(path, "/release-board") ||
		strings.HasPrefix(path, "/workflows") ||
		strings.HasPrefix(path, "/releases") ||
		strings.HasPrefix(path, "/registries"):
		return appaccess.PermWorkspaceApplicationView
	default:
		return ""
	}
}

func permissionRuleForMenu(item domainmenu.Record) (visibilityRule, bool) {
	switch {
	case item.ID == "dashboard":
		return visibilityRule{permissions: []string{appaccess.PermOverviewView}}, true
	case item.ID == "cluster-resources-nodes":
		return visibilityRule{permissions: []string{appaccess.PermPlatformNodesView}}, true
	case item.ID == "cluster-resources-namespaces":
		return visibilityRule{permissions: []string{appaccess.PermPlatformNamespacesView}}, true
	case item.ID == "extensions":
		return visibilityRule{permissions: []string{appaccess.PermPlatformExtensionsView}}, true
	case item.ID == "clusters":
		return visibilityRule{permissions: []string{appaccess.PermPlatformClustersView}}, true
	case item.ID == "builds":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryApplicationsView}}, true
	case item.ID == "delivery-blueprints":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryApplicationsView}}, true
	case item.ID == "delivery-onboarding":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryApplicationsView}}, true
	case item.ID == "delivery-testing":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryReleaseBundlesView, appaccess.PermDeliveryExecutionTasksView, appaccess.PermDeliveryReleaseBoardView}}, true
	case item.ID == "delivery-analysis":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryExecutionTasksView, appaccess.PermDeliveryReleaseBoardView, appaccess.PermDeliveryReleaseBundlesView}}, true
	case item.ID == "build-templates":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryBuildTemplatesView}}, true
	case item.ID == "release-bundles":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryReleaseBundlesView}}, true
	case item.ID == "execution-tasks":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryExecutionTasksView}}, true
	case item.ID == "workflow-templates":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryWorkflowTemplatesView}}, true
	case item.ID == "release-board":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryReleaseBoardView}}, true
	case item.ID == "application-environments":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryApplicationEnvView}}, true
	case item.ID == "workflows":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryWorkflowsView}}, true
	case item.ID == "releases":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryReleasesView}}, true
	case item.ID == "registries":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryRegistriesView}}, true
	case item.ID == "monitoring-workbench":
		return visibilityRule{permissions: []string{
			appaccess.PermObserveMonitoringView,
			appaccess.PermObserveAlertRulesView,
			appaccess.PermObserveAlertsView,
			appaccess.PermObserveNotificationsView,
			appaccess.PermObserveOncallView,
			appaccess.PermObserveHealingView,
			appaccess.PermObserveEventsView,
		}}, true
	case item.ID == "monitoring-workbench-overview", item.ID == "monitoring":
		return visibilityRule{permissions: []string{appaccess.PermObserveMonitoringView}}, true
	case item.ID == "monitoring-workbench-alerts", item.ID == "alerts":
		return visibilityRule{permissions: []string{appaccess.PermObserveAlertsView}}, true
	case item.ID == "monitoring-workbench-rules", item.ID == "rules":
		return visibilityRule{permissions: []string{appaccess.PermObserveAlertRulesView}}, true
	case item.ID == "monitoring-workbench-notifications", item.ID == "notifications":
		return visibilityRule{permissions: []string{appaccess.PermObserveNotificationsView}}, true
	case item.ID == "monitoring-workbench-oncall", item.ID == "oncall":
		return visibilityRule{permissions: []string{appaccess.PermObserveOncallView}}, true
	case item.ID == "monitoring-workbench-healing", item.ID == "healing":
		return visibilityRule{permissions: []string{appaccess.PermObserveHealingView}}, true
	case item.ID == "monitoring-workbench-events", item.ID == "events":
		return visibilityRule{permissions: []string{appaccess.PermObserveEventsView}}, true
	case item.ID == "ai-workbench":
		return visibilityRule{permissions: []string{appaccess.PermObserveAIView, appaccess.PermObserveAIChatUse}}, true
	case item.ID == "ai-workbench-chat", item.ID == "ai-workbench-investigation", item.ID == "assistant-workbench":
		return visibilityRule{permissions: []string{appaccess.PermObserveAIChatUse}}, true
	case item.ID == "ai-workbench-inspection", item.ID == "ai-workbench-tool-settings", item.ID == "ai-workbench-operations", item.ID == "ai-workbench-tools", item.ID == "assistant-operations", item.ID == "assistant-tools":
		return visibilityRule{permissions: []string{appaccess.PermObserveAIView}}, true
	case item.ID == "ai-workbench-model-settings":
		return visibilityRule{permissions: []string{appaccess.PermSettingsAIView}}, true
	case item.ID == "ai-gateway":
		return visibilityRule{permissions: []string{appaccess.PermAIGatewayView, appaccess.PermAIGatewayInvoke, appaccess.PermAIGatewayManage}}, true
	case item.ID == "ai-gateway-overview", item.ID == "ai-gateway-manifest":
		return visibilityRule{permissions: []string{appaccess.PermAIGatewayView}}, true
	case item.ID == "ai-gateway-tokens":
		return visibilityRule{permissions: []string{appaccess.PermAIGatewayView, appaccess.PermAIGatewayInvoke, appaccess.PermAIGatewayManage}}, true
	case item.ID == "ai-gateway-clients", item.ID == "ai-gateway-governance", item.ID == "ai-gateway-call-logs":
		return visibilityRule{permissions: []string{appaccess.PermAIGatewayManage}}, true
	case item.ID == "plugins", item.ID == "plugins-marketplace", item.ID == "plugins-installed":
		return visibilityRule{permissions: []string{appaccess.PermPluginView}}, true
	case item.ID == "virtualization-workbench":
		return visibilityRule{permissions: []string{
			appaccess.PermVirtualizationOverviewView,
			appaccess.PermVirtualizationVMsView,
			appaccess.PermVirtualizationClustersView,
			appaccess.PermVirtualizationImagesView,
			appaccess.PermVirtualizationFlavorsView,
			appaccess.PermVirtualizationOperationsView,
			appaccess.PermVirtualizationSyncView,
			appaccess.PermVirtualizationSyncManage,
		}}, true
	case item.ID == "virtualization-workbench-overview":
		return visibilityRule{permissions: []string{appaccess.PermVirtualizationOverviewView}}, true
	case item.ID == "virtualization-workbench-vms":
		return visibilityRule{permissions: []string{appaccess.PermVirtualizationVMsView}}, true
	case item.ID == "virtualization-workbench-clusters":
		return visibilityRule{permissions: []string{appaccess.PermVirtualizationClustersView}}, true
	case item.ID == "virtualization-workbench-images":
		return visibilityRule{permissions: []string{appaccess.PermVirtualizationImagesView}}, true
	case item.ID == "virtualization-workbench-flavors":
		return visibilityRule{permissions: []string{appaccess.PermVirtualizationFlavorsView}}, true
	case item.ID == "virtualization-workbench-operations":
		return visibilityRule{permissions: []string{appaccess.PermVirtualizationOperationsView}}, true
	case item.ID == "virtualization-workbench-sync":
		return visibilityRule{permissions: []string{appaccess.PermVirtualizationSyncView, appaccess.PermVirtualizationSyncManage}}, true
	case item.ID == "access":
		return visibilityRule{permissions: []string{
			appaccess.PermAccessUsersView,
			appaccess.PermAccessRolesView,
			appaccess.PermAccessGroupsView,
			appaccess.PermAccessPoliciesView,
			appaccess.PermAccessScopeGrantsView,
		}}, true
	case item.ID == "access-users":
		return visibilityRule{permissions: []string{appaccess.PermAccessUsersView}}, true
	case item.ID == "access-roles":
		return visibilityRule{permissions: []string{appaccess.PermAccessRolesView}}, true
	case item.ID == "access-teams":
		return visibilityRule{permissions: []string{appaccess.PermAccessGroupsView}}, true
	case item.ID == "access-policies":
		return visibilityRule{permissions: []string{appaccess.PermAccessPoliciesView}}, true
	case item.ID == "system":
		return visibilityRule{permissions: []string{
			appaccess.PermSystemOnlineUsersView,
			appaccess.PermSystemAnnouncementsView,
			appaccess.PermSystemMenusView,
			appaccess.PermSystemAuditView,
			appaccess.PermSystemOperationsView,
		}}, true
	case item.ID == "identity":
		return visibilityRule{permissions: []string{
			appaccess.PermIdentityApplicationsView,
			appaccess.PermIdentityProvidersView,
			appaccess.PermIdentityOutpostsView,
			appaccess.PermIdentityPoliciesView,
			appaccess.PermIdentitySessionsView,
			appaccess.PermIdentityAuditView,
		}}, true
	case item.ID == "identity-overview":
		return visibilityRule{permissions: []string{
			appaccess.PermIdentityApplicationsView,
			appaccess.PermIdentityProvidersView,
			appaccess.PermIdentityOutpostsView,
			appaccess.PermIdentityPoliciesView,
			appaccess.PermIdentitySessionsView,
			appaccess.PermIdentityAuditView,
		}}, true
	case item.ID == "identity-applications":
		return visibilityRule{permissions: []string{appaccess.PermIdentityApplicationsView}}, true
	case item.ID == "identity-providers":
		return visibilityRule{permissions: []string{appaccess.PermIdentityProvidersView}}, true
	case item.ID == "identity-outposts":
		return visibilityRule{permissions: []string{appaccess.PermIdentityOutpostsView}}, true
	case item.ID == "identity-policies":
		return visibilityRule{permissions: []string{appaccess.PermIdentityPoliciesView}}, true
	case item.ID == "identity-sessions":
		return visibilityRule{permissions: []string{appaccess.PermIdentitySessionsView}}, true
	case item.ID == "identity-audit":
		return visibilityRule{permissions: []string{appaccess.PermIdentityAuditView}}, true
	case item.ID == "system-online-users":
		return visibilityRule{permissions: []string{appaccess.PermSystemOnlineUsersView}}, true
	case item.ID == "announcements":
		return visibilityRule{permissions: []string{appaccess.PermSystemAnnouncementsView}}, true
	case item.ID == "menus":
		return visibilityRule{permissions: []string{appaccess.PermSystemMenusView}}, true
	case item.ID == "audit":
		return visibilityRule{permissions: []string{appaccess.PermSystemAuditView}}, true
	case item.ID == "operations":
		return visibilityRule{permissions: []string{appaccess.PermSystemOperationsView}}, true
	case item.ID == "settings":
		return visibilityRule{permissions: []string{
			appaccess.PermSettingsIdentityView,
			appaccess.PermSettingsMonitoringView,
			appaccess.PermSettingsAIView,
			appaccess.PermSettingsBrandingView,
		}}, true
	case item.ID == "settings-login":
		return visibilityRule{permissions: []string{appaccess.PermSettingsIdentityView}}, true
	case item.ID == "settings-branding":
		return visibilityRule{permissions: []string{appaccess.PermSettingsBrandingView}}, true
	case item.ID == "helm" || strings.HasPrefix(item.ID, "helm-"):
		return visibilityRule{permissions: []string{appaccess.PermPlatformHelmView}}, true
	case item.ID == "workloads" || strings.HasPrefix(item.ID, "workloads-"):
		return visibilityRule{permissions: []string{appaccess.PermPlatformWorkloadsView}}, true
	case item.ID == "configuration" || strings.HasPrefix(item.ID, "configuration-"):
		return visibilityRule{permissions: []string{appaccess.PermPlatformConfigurationView}}, true
	case item.ID == "network" || strings.HasPrefix(item.ID, "network-"):
		return visibilityRule{permissions: []string{appaccess.PermPlatformNetworkView}}, true
	case item.ID == "storage" || strings.HasPrefix(item.ID, "storage-"):
		return visibilityRule{permissions: []string{appaccess.PermPlatformStorageView}}, true
	default:
		return visibilityRule{}, false
	}
}

func isVisibleByPermissions(item domainmenu.Record, permissionKeys []string) bool {
	if workspacePermission := workspacePermissionForMenu(item); workspacePermission != "" && !slices.Contains(permissionKeys, workspacePermission) {
		return false
	}
	rule, ok := permissionRuleForMenu(item)
	if !ok {
		return false
	}
	for _, permissionKey := range rule.permissions {
		if slices.Contains(permissionKeys, permissionKey) {
			return true
		}
	}
	return false
}
