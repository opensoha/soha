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
	if path == "/" || hasMenuPathPrefix(path, workspaceResourceMenuPrefixes) {
		return appaccess.PermWorkspaceResourceView
	}
	if hasMenuPathPrefix(path, workspaceApplicationMenuPrefixes) {
		return appaccess.PermWorkspaceApplicationView
	}
	return ""
}

var workspaceResourceMenuPrefixes = []string{
	"/cluster-resources", "/workloads", "/configuration", "/network", "/storage",
	"/platform-access-control", "/helm", "/extensions", "/clusters", "/monitoring-workbench",
	"/ai-gateway", "/ai-workbench", "/virtualization", "/observability", "/ai-observe", "/chat",
	"/compute",
}

var workspaceApplicationMenuPrefixes = []string{
	"/applications", "/application-environments", "/build-templates", "/delivery/onboarding",
	"/delivery/testing", "/delivery/analysis", "/delivery/blueprints", "/delivery/release-bundles",
	"/delivery/execution-tasks", "/workflow-templates", "/release-board", "/workflows", "/releases", "/registries",
}

func hasMenuPathPrefix(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func permissionRuleForMenu(item domainmenu.Record) (visibilityRule, bool) {
	resolvers := []func(string) (visibilityRule, bool){
		coreDeliveryMenuRule,
		observabilityAIMenuRule,
		virtualizationAccessMenuRule,
		identitySystemMenuRule,
		platformFamilyMenuRule,
	}
	for _, resolve := range resolvers {
		if rule, ok := resolve(item.ID); ok {
			return rule, true
		}
	}
	return visibilityRule{}, false
}

func coreDeliveryMenuRule(id string) (visibilityRule, bool) {
	switch id {
	case "dashboard":
		return visibilityRule{permissions: []string{appaccess.PermOverviewView}}, true
	case "cluster-resources-nodes":
		return visibilityRule{permissions: []string{appaccess.PermPlatformNodesView}}, true
	case "cluster-resources-namespaces":
		return visibilityRule{permissions: []string{appaccess.PermPlatformNamespacesView}}, true
	case "extensions":
		return visibilityRule{permissions: []string{appaccess.PermPlatformExtensionsView}}, true
	case "clusters":
		return visibilityRule{permissions: []string{appaccess.PermPlatformClustersView}}, true
	case "builds", "delivery-blueprints", "delivery-onboarding":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryApplicationsView}}, true
	case "delivery-testing":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryReleaseBundlesView, appaccess.PermDeliveryExecutionTasksView, appaccess.PermDeliveryReleaseBoardView}}, true
	case "delivery-analysis":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryExecutionTasksView, appaccess.PermDeliveryReleaseBoardView, appaccess.PermDeliveryReleaseBundlesView}}, true
	case "build-templates":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryBuildTemplatesView}}, true
	case "release-bundles":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryReleaseBundlesView}}, true
	case "execution-tasks":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryExecutionTasksView}}, true
	case "workflow-templates":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryWorkflowTemplatesView}}, true
	case "release-board":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryReleaseBoardView}}, true
	case "application-environments":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryApplicationEnvView}}, true
	case "workflows":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryWorkflowsView}}, true
	case "releases":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryReleasesView}}, true
	case "registries":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryRegistriesView}}, true
	default:
		return visibilityRule{}, false
	}
}

func observabilityAIMenuRule(id string) (visibilityRule, bool) {
	switch id {
	case "monitoring-workbench":
		return visibilityRule{permissions: []string{
			appaccess.PermObserveMonitoringView,
			appaccess.PermObserveAlertRulesView,
			appaccess.PermObserveAlertsView,
			appaccess.PermObserveNotificationsView,
			appaccess.PermObserveOncallView,
			appaccess.PermObserveHealingView,
			appaccess.PermObserveEventsView,
		}}, true
	case "monitoring-workbench-overview", "monitoring":
		return visibilityRule{permissions: []string{appaccess.PermObserveMonitoringView}}, true
	case "monitoring-workbench-alerts", "alerts":
		return visibilityRule{permissions: []string{appaccess.PermObserveAlertsView}}, true
	case "monitoring-workbench-rules", "rules":
		return visibilityRule{permissions: []string{appaccess.PermObserveAlertRulesView}}, true
	case "monitoring-workbench-notifications", "notifications":
		return visibilityRule{permissions: []string{appaccess.PermObserveNotificationsView}}, true
	case "monitoring-workbench-oncall", "oncall":
		return visibilityRule{permissions: []string{appaccess.PermObserveOncallView}}, true
	case "monitoring-workbench-healing", "healing":
		return visibilityRule{permissions: []string{appaccess.PermObserveHealingView}}, true
	case "monitoring-workbench-events", "events":
		return visibilityRule{permissions: []string{appaccess.PermObserveEventsView}}, true
	case "ai-workbench":
		return visibilityRule{permissions: []string{appaccess.PermObserveAIView, appaccess.PermObserveAIChatUse}}, true
	case "ai-workbench-chat", "ai-workbench-investigation", "assistant-workbench":
		return visibilityRule{permissions: []string{appaccess.PermObserveAIChatUse}}, true
	case "ai-workbench-inspection", "ai-workbench-tool-settings", "ai-workbench-operations", "ai-workbench-tools", "assistant-operations", "assistant-tools":
		return visibilityRule{permissions: []string{appaccess.PermObserveAIView}}, true
	case "ai-workbench-model-settings":
		return visibilityRule{permissions: []string{appaccess.PermSettingsAIView}}, true
	case "ai-gateway", "ai-gateway-tokens":
		return visibilityRule{permissions: []string{appaccess.PermAIGatewayView, appaccess.PermAIGatewayInvoke, appaccess.PermAIGatewayManage}}, true
	case "ai-gateway-overview", "ai-gateway-manifest":
		return visibilityRule{permissions: []string{appaccess.PermAIGatewayView}}, true
	case "ai-gateway-clients", "ai-gateway-governance", "ai-gateway-call-logs":
		return visibilityRule{permissions: []string{appaccess.PermAIGatewayManage}}, true
	case "settings-extensions", "extension-center":
		return visibilityRule{permissions: []string{appaccess.PermPluginView}}, true
	case "settings-extensions-marketplace", "extensions-marketplace", "extensions-installed", "plugins", "plugins-marketplace", "plugins-installed":
		return visibilityRule{permissions: []string{appaccess.PermPluginView}}, true
	case "settings-extensions-capabilities", "extensions-capabilities":
		return visibilityRule{permissions: []string{appaccess.PermPlatformExtensionsView}}, true
	default:
		return visibilityRule{}, false
	}
}

func virtualizationAccessMenuRule(id string) (visibilityRule, bool) {
	if rule, ok := computeMenuRule(id); ok {
		return rule, true
	}
	switch id {
	case "virtualization-workbench":
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
	case "virtualization-workbench-overview":
		return visibilityRule{permissions: []string{appaccess.PermVirtualizationOverviewView}}, true
	case "virtualization-workbench-vms":
		return visibilityRule{permissions: []string{appaccess.PermVirtualizationVMsView}}, true
	case "virtualization-workbench-clusters":
		return visibilityRule{permissions: []string{appaccess.PermVirtualizationClustersView}}, true
	case "virtualization-workbench-images":
		return visibilityRule{permissions: []string{appaccess.PermVirtualizationImagesView}}, true
	case "virtualization-workbench-flavors":
		return visibilityRule{permissions: []string{appaccess.PermVirtualizationFlavorsView}}, true
	case "virtualization-workbench-operations":
		return visibilityRule{permissions: []string{appaccess.PermVirtualizationOperationsView}}, true
	case "virtualization-workbench-sync":
		return visibilityRule{permissions: []string{appaccess.PermVirtualizationSyncView, appaccess.PermVirtualizationSyncManage}}, true
	case "access":
		return visibilityRule{permissions: []string{
			appaccess.PermAccessUsersView,
			appaccess.PermAccessRolesView,
			appaccess.PermAccessGroupsView,
			appaccess.PermAccessPoliciesView,
			appaccess.PermAccessScopeGrantsView,
		}}, true
	case "access-users":
		return visibilityRule{permissions: []string{appaccess.PermAccessUsersView}}, true
	case "access-roles":
		return visibilityRule{permissions: []string{appaccess.PermAccessRolesView}}, true
	case "access-teams":
		return visibilityRule{permissions: []string{appaccess.PermAccessGroupsView}}, true
	case "access-policies":
		return visibilityRule{permissions: []string{appaccess.PermAccessPoliciesView}}, true
	default:
		return visibilityRule{}, false
	}
}

func computeMenuRule(id string) (visibilityRule, bool) {
	switch id {
	case "compute-workbench":
		return visibilityRule{permissions: []string{
			appaccess.PermVirtualizationOverviewView, appaccess.PermVirtualizationVMsView,
			appaccess.PermVirtualizationClustersView, appaccess.PermVirtualizationImagesView,
			appaccess.PermVirtualizationFlavorsView, appaccess.PermVirtualizationOperationsView,
			appaccess.PermVirtualizationSyncView, appaccess.PermVirtualizationSyncManage,
			appaccess.PermDockerOverviewView, appaccess.PermDockerHostsView, appaccess.PermDockerProjectsView,
			appaccess.PermDockerServicesView, appaccess.PermDockerPortsView, appaccess.PermDockerTemplatesView,
			appaccess.PermDockerOperationsView,
		}}, true
	case "compute-workbench-overview":
		return visibilityRule{permissions: []string{appaccess.PermVirtualizationOverviewView, appaccess.PermDockerOverviewView}}, true
	case "compute-workbench-access":
		return visibilityRule{permissions: []string{appaccess.PermVirtualizationClustersView, appaccess.PermDockerHostsView}}, true
	case "compute-workbench-tasks-sync":
		return visibilityRule{permissions: []string{appaccess.PermVirtualizationSyncView, appaccess.PermVirtualizationSyncManage, appaccess.PermDockerOperationsView}}, true
	case "compute-workbench-tasks-build", "compute-workbench-tasks-operations":
		return visibilityRule{permissions: []string{appaccess.PermVirtualizationOperationsView, appaccess.PermDockerOperationsView}}, true
	default:
		return visibilityRule{}, false
	}
}

func identitySystemMenuRule(id string) (visibilityRule, bool) {
	switch id {
	case "system":
		return visibilityRule{permissions: []string{
			appaccess.PermSystemOnlineUsersView,
			appaccess.PermSystemAnnouncementsView,
			appaccess.PermSystemMenusView,
			appaccess.PermSystemAuditView,
			appaccess.PermSystemOperationsView,
		}}, true
	case "identity", "identity-overview":
		return visibilityRule{permissions: []string{
			appaccess.PermIdentityApplicationsView,
			appaccess.PermIdentityProvidersView,
			appaccess.PermIdentityOutpostsView,
			appaccess.PermIdentityPoliciesView,
			appaccess.PermIdentitySessionsView,
			appaccess.PermIdentityAuditView,
		}}, true
	case "identity-applications":
		return visibilityRule{permissions: []string{appaccess.PermIdentityApplicationsView}}, true
	case "identity-providers":
		return visibilityRule{permissions: []string{appaccess.PermIdentityProvidersView}}, true
	case "identity-outposts":
		return visibilityRule{permissions: []string{appaccess.PermIdentityOutpostsView}}, true
	case "identity-policies":
		return visibilityRule{permissions: []string{appaccess.PermIdentityPoliciesView}}, true
	case "identity-sessions":
		return visibilityRule{permissions: []string{appaccess.PermIdentitySessionsView}}, true
	case "identity-audit":
		return visibilityRule{permissions: []string{appaccess.PermIdentityAuditView}}, true
	case "system-online-users":
		return visibilityRule{permissions: []string{appaccess.PermSystemOnlineUsersView}}, true
	case "announcements":
		return visibilityRule{permissions: []string{appaccess.PermSystemAnnouncementsView}}, true
	case "menus":
		return visibilityRule{permissions: []string{appaccess.PermSystemMenusView}}, true
	case "audit":
		return visibilityRule{permissions: []string{appaccess.PermSystemAuditView}}, true
	case "operations":
		return visibilityRule{permissions: []string{appaccess.PermSystemOperationsView}}, true
	case "settings":
		return visibilityRule{permissions: []string{
			appaccess.PermSettingsIdentityView,
			appaccess.PermSettingsMonitoringView,
			appaccess.PermSettingsAIView,
			appaccess.PermSettingsBrandingView,
		}}, true
	case "settings-login":
		return visibilityRule{permissions: []string{appaccess.PermSettingsIdentityView}}, true
	case "settings-branding":
		return visibilityRule{permissions: []string{appaccess.PermSettingsBrandingView}}, true
	default:
		return visibilityRule{}, false
	}
}

func platformFamilyMenuRule(id string) (visibilityRule, bool) {
	switch {
	case id == "helm" || strings.HasPrefix(id, "helm-"):
		return visibilityRule{permissions: []string{appaccess.PermPlatformHelmView}}, true
	case id == "workloads" || strings.HasPrefix(id, "workloads-"):
		return visibilityRule{permissions: []string{appaccess.PermPlatformWorkloadsView}}, true
	case id == "configuration" || strings.HasPrefix(id, "configuration-"):
		return visibilityRule{permissions: []string{appaccess.PermPlatformConfigurationView}}, true
	case id == "network" || strings.HasPrefix(id, "network-"):
		return visibilityRule{permissions: []string{appaccess.PermPlatformNetworkView}}, true
	case id == "storage" || strings.HasPrefix(id, "storage-"):
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
