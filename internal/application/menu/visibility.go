package menu

import (
	"slices"
	"strings"

	appaccess "github.com/kubecrux/kubecrux/internal/application/access"
	domainmenu "github.com/kubecrux/kubecrux/internal/domain/menu"
)

type visibilityRule struct {
	permissions []string
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
	case item.ID == "build-templates":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryBuildTemplatesView}}, true
	case item.ID == "workflow-templates":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryWorkflowTemplatesView}}, true
	case item.ID == "release-board":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryReleaseBoardView}}, true
	case item.ID == "application-environments":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryApplicationEnvView}}, true
	case item.ID == "delivery-environments":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryEnvironmentsView}}, true
	case item.ID == "business-lines":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryBusinessLinesView}}, true
	case item.ID == "workflows":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryWorkflowsView}}, true
	case item.ID == "releases":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryReleasesView}}, true
	case item.ID == "registries":
		return visibilityRule{permissions: []string{appaccess.PermDeliveryRegistriesView}}, true
	case item.ID == "observability":
		return visibilityRule{permissions: []string{
			appaccess.PermObserveMonitoringView,
			appaccess.PermObserveAlertsView,
			appaccess.PermObserveNotificationsView,
			appaccess.PermObserveOncallView,
			appaccess.PermObserveEventsView,
		}}, true
	case item.ID == "monitoring":
		return visibilityRule{permissions: []string{appaccess.PermObserveMonitoringView}}, true
	case item.ID == "alerts":
		return visibilityRule{permissions: []string{appaccess.PermObserveAlertsView}}, true
	case item.ID == "notifications":
		return visibilityRule{permissions: []string{appaccess.PermObserveNotificationsView}}, true
	case item.ID == "oncall":
		return visibilityRule{permissions: []string{appaccess.PermObserveOncallView}}, true
	case item.ID == "events":
		return visibilityRule{permissions: []string{appaccess.PermObserveEventsView}}, true
	case item.ID == "assistant":
		return visibilityRule{permissions: []string{appaccess.PermObserveAIView, appaccess.PermObserveAIChatUse}}, true
	case item.ID == "assistant-workbench":
		return visibilityRule{permissions: []string{appaccess.PermObserveAIChatUse}}, true
	case item.ID == "assistant-operations", item.ID == "assistant-tools":
		return visibilityRule{permissions: []string{appaccess.PermObserveAIView}}, true
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
