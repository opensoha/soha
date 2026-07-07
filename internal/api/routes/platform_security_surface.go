package routes

import (
	"strings"

	appaccess "github.com/opensoha/soha/internal/application/access"
)

type platformMutationSecuritySurfaceEntry struct {
	ResourceKind      string
	Action            string
	CapabilityKey     string
	AuditRequired     bool
	OperationRequired bool
}

func platformMutationSecuritySurface(method, path string) (platformMutationSecuritySurfaceEntry, bool) {
	method = strings.ToUpper(strings.TrimSpace(method))
	path = strings.TrimSpace(path)
	if !isMutationMethod(method) || !strings.HasPrefix(path, "/api/v1/clusters") {
		return platformMutationSecuritySurfaceEntry{}, false
	}

	resourceKind := platformMutationResourceKind(path)
	if resourceKind == "" {
		return platformMutationSecuritySurfaceEntry{}, false
	}

	return platformMutationSecuritySurfaceEntry{
		ResourceKind:      resourceKind,
		Action:            platformMutationAction(method, path),
		CapabilityKey:     platformMutationCapabilityKey(path),
		AuditRequired:     true,
		OperationRequired: true,
	}, true
}

func isMutationMethod(method string) bool {
	switch method {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	default:
		return false
	}
}

type nonPlatformMutationSecuritySurfaceEntry struct {
	ResourceKind      string
	Action            string
	PermissionKey     string
	ScopeRequired     bool
	AuditRequired     bool
	OperationRequired bool
}

func nonPlatformMutationSecuritySurface(method, path string) (nonPlatformMutationSecuritySurfaceEntry, bool) {
	method = strings.ToUpper(strings.TrimSpace(method))
	path = strings.TrimSpace(path)
	if !isMutationMethod(method) || strings.HasPrefix(path, "/api/v1/clusters") || strings.HasPrefix(path, "/api/v1/auth/") {
		return nonPlatformMutationSecuritySurfaceEntry{}, false
	}

	for _, classifier := range nonPlatformMutationSecurityClassifiers {
		if entry, ok := classifier(method, path); ok {
			entry.AuditRequired = true
			entry.OperationRequired = true
			return entry, true
		}
	}
	return nonPlatformMutationSecuritySurfaceEntry{}, false
}

var nonPlatformMutationSecurityClassifiers = []func(string, string) (nonPlatformMutationSecuritySurfaceEntry, bool){
	deliveryMutationSecuritySurface,
	monitoringMutationSecuritySurface,
	runtimeMutationSecuritySurface,
	copilotMutationSecuritySurface,
	systemMutationSecuritySurface,
	accessMutationSecuritySurface,
	aiGatewayMutationSecuritySurface,
	pluginMutationSecuritySurface,
	identityMutationSecuritySurface,
	settingsMutationSecuritySurface,
}

func deliveryMutationSecuritySurface(method, path string) (nonPlatformMutationSecuritySurfaceEntry, bool) {
	scopeRequired := true
	switch {
	case strings.HasPrefix(path, "/api/v1/applications/") && strings.Contains(path, "/services"):
		return nonPlatformMutationEntry("ApplicationService", nonPlatformMutationAction(method, path), appaccess.PermDeliveryApplicationServicesManage, scopeRequired), true
	case strings.HasPrefix(path, "/api/v1/applications"):
		return nonPlatformMutationEntry("Application", nonPlatformMutationAction(method, path), deliveryApplicationPermission(method), scopeRequired), true
	case strings.HasPrefix(path, "/api/v1/application-environments"):
		return nonPlatformMutationEntry("ApplicationEnvironment", nonPlatformMutationAction(method, path), appaccess.PermDeliveryApplicationEnvManage, scopeRequired), true
	case strings.HasPrefix(path, "/api/v1/build-templates"):
		return nonPlatformMutationEntry("BuildTemplate", nonPlatformMutationAction(method, path), appaccess.PermDeliveryBuildTemplatesManage, scopeRequired), true
	case strings.HasPrefix(path, "/api/v1/workflow-templates"):
		return nonPlatformMutationEntry("WorkflowTemplate", nonPlatformMutationAction(method, path), appaccess.PermDeliveryWorkflowTemplatesManage, scopeRequired), true
	case strings.HasPrefix(path, "/api/v1/builds/trigger"):
		return nonPlatformMutationEntry("Build", "trigger", appaccess.PermDeliveryBuildsTrigger, scopeRequired), true
	case strings.HasPrefix(path, "/api/v1/workflows/trigger"):
		return nonPlatformMutationEntry("Workflow", "trigger", appaccess.PermDeliveryWorkflowsTrigger, scopeRequired), true
	case strings.HasPrefix(path, "/api/v1/workflows/") && strings.HasSuffix(path, "/approve"):
		return nonPlatformMutationEntry("WorkflowApproval", "approve", appaccess.PermDeliveryWorkflowsTrigger, scopeRequired), true
	case strings.HasPrefix(path, "/api/v1/workflows/") && strings.HasSuffix(path, "/reject"):
		return nonPlatformMutationEntry("WorkflowApproval", "reject", appaccess.PermDeliveryWorkflowsTrigger, scopeRequired), true
	case strings.HasPrefix(path, "/api/v1/registries"):
		return nonPlatformMutationEntry("RegistryConnection", nonPlatformMutationAction(method, path), appaccess.PermDeliveryRegistriesManage, false), true
	case strings.HasPrefix(path, "/api/v1/releases/trigger"):
		return nonPlatformMutationEntry("Release", "trigger", appaccess.PermDeliveryReleasesTrigger, scopeRequired), true
	case strings.HasPrefix(path, "/api/v1/delivery/execution-tasks/") && strings.HasSuffix(path, "/cancel"):
		return nonPlatformMutationEntry("ExecutionTask", "cancel", appaccess.PermDeliveryExecutionTasksManage, scopeRequired), true
	case strings.HasPrefix(path, "/api/v1/delivery/execution-tasks/") && strings.HasSuffix(path, "/retry"):
		return nonPlatformMutationEntry("ExecutionTask", "retry", appaccess.PermDeliveryExecutionTasksManage, scopeRequired), true
	case strings.HasPrefix(path, "/api/v1/delivery/blueprints") && strings.Contains(path, "/render-spec"):
		return nonPlatformMutationEntry("DeliveryBlueprint", "render", appaccess.PermDeliveryApplicationsCreate, scopeRequired), true
	case strings.HasPrefix(path, "/api/v1/delivery/blueprints") && strings.Contains(path, "/bootstrap-application"):
		return nonPlatformMutationEntry("DeliveryBlueprint", "bootstrap", appaccess.PermDeliveryApplicationsCreate, scopeRequired), true
	case strings.HasPrefix(path, "/api/v1/delivery/blueprints"):
		return nonPlatformMutationEntry("DeliveryBlueprint", nonPlatformMutationAction(method, path), appaccess.PermDeliveryApplicationsCreate, scopeRequired), true
	case strings.HasPrefix(path, "/api/v1/delivery/drafts") && strings.HasSuffix(path, "/confirm"):
		return nonPlatformMutationEntry("DeliveryDraft", "confirm", appaccess.PermDeliveryApplicationsUpdate, scopeRequired), true
	case strings.HasPrefix(path, "/api/v1/delivery/drafts"):
		return nonPlatformMutationEntry("DeliveryDraft", nonPlatformMutationAction(method, path), appaccess.PermDeliveryApplicationsUpdate, scopeRequired), true
	case strings.HasPrefix(path, "/api/v1/delivery/plans") && strings.HasSuffix(path, "/confirm"):
		return nonPlatformMutationEntry("DeliveryPlan", "confirm", appaccess.PermDeliveryWorkflowsTrigger, scopeRequired), true
	case strings.HasPrefix(path, "/api/v1/delivery/plans"):
		return nonPlatformMutationEntry("DeliveryPlan", nonPlatformMutationAction(method, path), appaccess.PermDeliveryApplicationsView, scopeRequired), true
	}
	return nonPlatformMutationSecuritySurfaceEntry{}, false
}

func monitoringMutationSecuritySurface(method, path string) (nonPlatformMutationSecuritySurfaceEntry, bool) {
	switch {
	case strings.HasPrefix(path, "/api/v1/alerts/"):
		return nonPlatformMutationEntry("Alert", monitoringAlertAction(path), monitoringAlertPermission(path), false), true
	case strings.HasPrefix(path, "/api/v1/alert-events/") && strings.HasSuffix(path, "/heal"):
		return nonPlatformMutationEntry("AlertEvent", "heal", appaccess.PermObserveHealingManage, false), true
	case strings.HasPrefix(path, "/api/v1/alert-events/"):
		return nonPlatformMutationEntry("AlertEvent", monitoringAlertAction(path), appaccess.PermObserveAlertsAcknowledge, false), true
	case strings.HasPrefix(path, "/api/v1/healing-runs/"):
		return nonPlatformMutationEntry("HealingRun", nonPlatformMutationAction(method, path), appaccess.PermObserveHealingManage, false), true
	case strings.HasPrefix(path, "/api/v1/alert-integrations"):
		return nonPlatformMutationEntry("AlertIntegration", nonPlatformMutationAction(method, path), appaccess.PermObserveAlertIntegrationsManage, false), true
	case strings.HasPrefix(path, "/api/v1/alert-rules"):
		return nonPlatformMutationEntry("AlertRule", nonPlatformMutationAction(method, path), appaccess.PermObserveAlertRulesManage, false), true
	case strings.HasPrefix(path, "/api/v1/notification-policies"):
		return nonPlatformMutationEntry("NotificationPolicy", nonPlatformMutationAction(method, path), appaccess.PermObserveNotificationsManage, false), true
	case strings.HasPrefix(path, "/api/v1/notification-templates"):
		return nonPlatformMutationEntry("NotificationTemplate", nonPlatformMutationAction(method, path), appaccess.PermObserveNotificationsManage, false), true
	case strings.HasPrefix(path, "/api/v1/healing-policies"):
		return nonPlatformMutationEntry("HealingPolicy", nonPlatformMutationAction(method, path), appaccess.PermObserveHealingManage, false), true
	case strings.HasPrefix(path, "/api/v1/oncall/"):
		return nonPlatformMutationEntry("OnCallConfig", nonPlatformMutationAction(method, path), appaccess.PermObserveOncallManage, false), true
	case strings.HasPrefix(path, "/api/v1/alert-silences"):
		return nonPlatformMutationEntry("AlertSilence", nonPlatformMutationAction(method, path), appaccess.PermObserveAlertsManage, false), true
	case strings.HasPrefix(path, "/api/v1/notification-channels"):
		return nonPlatformMutationEntry("NotificationChannel", nonPlatformMutationAction(method, path), appaccess.PermObserveNotificationsManage, false), true
	case strings.HasPrefix(path, "/api/v1/alert-routes"):
		return nonPlatformMutationEntry("AlertRoute", nonPlatformMutationAction(method, path), appaccess.PermObserveNotificationsManage, false), true
	}
	return nonPlatformMutationSecuritySurfaceEntry{}, false
}

func runtimeMutationSecuritySurface(method, path string) (nonPlatformMutationSecuritySurfaceEntry, bool) {
	switch {
	case strings.HasPrefix(path, "/api/v1/virtualization/clusters"):
		return nonPlatformMutationEntry("VirtualizationCluster", nonPlatformMutationAction(method, path), appaccess.PermVirtualizationClustersManage, false), true
	case strings.HasPrefix(path, "/api/v1/virtualization/vms"):
		return nonPlatformMutationEntry("VirtualMachine", nonPlatformMutationAction(method, path), appaccess.PermVirtualizationVMsManage, false), true
	case strings.HasPrefix(path, "/api/v1/virtualization/images"):
		return nonPlatformMutationEntry("VirtualizationImage", nonPlatformMutationAction(method, path), appaccess.PermVirtualizationImagesManage, false), true
	case strings.HasPrefix(path, "/api/v1/virtualization/flavors"):
		return nonPlatformMutationEntry("VirtualizationFlavor", nonPlatformMutationAction(method, path), appaccess.PermVirtualizationFlavorsManage, false), true
	case strings.HasPrefix(path, "/api/v1/virtualization/operations/"):
		return nonPlatformMutationEntry("VirtualizationOperation", nonPlatformMutationAction(method, path), appaccess.PermVirtualizationOperationsManage, false), true
	case path == "/api/v1/virtualization/sync":
		return nonPlatformMutationEntry("VirtualizationSync", "sync", appaccess.PermVirtualizationSyncManage, false), true
	case strings.HasPrefix(path, "/api/v1/docker/hosts"):
		return nonPlatformMutationEntry("DockerHost", nonPlatformMutationAction(method, path), appaccess.PermDockerHostsManage, false), true
	case strings.HasPrefix(path, "/api/v1/docker/projects") && strings.HasSuffix(path, "/deploy"):
		return nonPlatformMutationEntry("DockerProject", "deploy", appaccess.PermDockerProjectsDeploy, false), true
	case strings.HasPrefix(path, "/api/v1/docker/projects"):
		return nonPlatformMutationEntry("DockerProject", nonPlatformMutationAction(method, path), appaccess.PermDockerProjectsManage, false), true
	case strings.HasPrefix(path, "/api/v1/docker/containers/start"):
		return nonPlatformMutationEntry("DockerContainer", "start", appaccess.PermDockerServicesManage, false), true
	case strings.HasPrefix(path, "/api/v1/docker/services/"):
		return nonPlatformMutationEntry("DockerService", nonPlatformMutationAction(method, path), appaccess.PermDockerServicesManage, false), true
	case strings.HasPrefix(path, "/api/v1/docker/ports"):
		return nonPlatformMutationEntry("DockerPortMapping", nonPlatformMutationAction(method, path), appaccess.PermDockerPortsManage, false), true
	case strings.HasPrefix(path, "/api/v1/docker/templates"):
		return nonPlatformMutationEntry("DockerTemplate", nonPlatformMutationAction(method, path), appaccess.PermDockerTemplatesManage, false), true
	case strings.HasPrefix(path, "/api/v1/docker/operations/"):
		return nonPlatformMutationEntry("DockerOperation", nonPlatformMutationAction(method, path), appaccess.PermDockerOperationsManage, false), true
	}
	return nonPlatformMutationSecuritySurfaceEntry{}, false
}

func copilotMutationSecuritySurface(method, path string) (nonPlatformMutationSecuritySurfaceEntry, bool) {
	switch {
	case strings.HasPrefix(path, "/api/v1/copilot/agent-runs/"):
		return nonPlatformMutationEntry("AgentRun", "cancel", appaccess.PermObserveAIInspectionManage, false), true
	case strings.HasPrefix(path, "/api/v1/copilot/data-sources"):
		return nonPlatformMutationEntry("CopilotDataSource", nonPlatformMutationAction(method, path), appaccess.PermObserveAIInspectionManage, false), true
	case strings.HasPrefix(path, "/api/v1/copilot/analysis-profiles"):
		return nonPlatformMutationEntry("CopilotAnalysisProfile", nonPlatformMutationAction(method, path), appaccess.PermObserveAIInspectionManage, false), true
	case strings.HasPrefix(path, "/api/v1/copilot/automation-policies"):
		return nonPlatformMutationEntry("CopilotAutomationPolicy", nonPlatformMutationAction(method, path), appaccess.PermObserveAIInspectionManage, false), true
	case strings.HasPrefix(path, "/api/v1/copilot/root-cause/runs"):
		return nonPlatformMutationEntry("RootCauseRun", "run", appaccess.PermObserveAIRootCauseRun, false), true
	case strings.HasPrefix(path, "/api/v1/copilot/global-assistant/events"):
		return nonPlatformMutationEntry("AIWorkbenchGlobalAssistant", "record-event", appaccess.PermObserveAIChatUse, false), true
	case strings.HasPrefix(path, "/api/v1/copilot/sessions") && strings.Contains(path, "/inspection-task"):
		return nonPlatformMutationEntry("InspectionTask", "create", appaccess.PermObserveAIInspectionManage, false), true
	case strings.HasPrefix(path, "/api/v1/copilot/sessions") && strings.Contains(path, "/analyze"):
		return nonPlatformMutationEntry("CopilotSession", "analyze", appaccess.PermObserveAIChatUse, false), true
	case strings.HasPrefix(path, "/api/v1/copilot/sessions") && strings.Contains(path, "/messages"):
		return nonPlatformMutationEntry("CopilotMessage", "create", appaccess.PermObserveAIChatUse, false), true
	case strings.HasPrefix(path, "/api/v1/copilot/sessions"):
		return nonPlatformMutationEntry("CopilotSession", nonPlatformMutationAction(method, path), appaccess.PermObserveAIChatUse, false), true
	case strings.HasPrefix(path, "/api/v1/copilot/inspection-tasks") && strings.Contains(path, "/execute"):
		return nonPlatformMutationEntry("InspectionTask", "execute", appaccess.PermObserveAIInspectionRun, false), true
	case strings.HasPrefix(path, "/api/v1/copilot/inspection-runs/") && strings.Contains(path, "/session"):
		return nonPlatformMutationEntry("InspectionRun", "create-session", appaccess.PermObserveAIChatUse, false), true
	case strings.HasPrefix(path, "/api/v1/copilot/inspection-tasks"):
		return nonPlatformMutationEntry("InspectionTask", nonPlatformMutationAction(method, path), appaccess.PermObserveAIInspectionManage, false), true
	}
	return nonPlatformMutationSecuritySurfaceEntry{}, false
}

func systemMutationSecuritySurface(method, path string) (nonPlatformMutationSecuritySurfaceEntry, bool) {
	switch {
	case strings.HasPrefix(path, "/api/v1/menus"):
		return nonPlatformMutationEntry("Menu", nonPlatformMutationAction(method, path), appaccess.PermSystemMenusManage, false), true
	case strings.HasPrefix(path, "/api/v1/announcements/") && strings.HasSuffix(path, "/read"):
		return nonPlatformMutationEntry("Announcement", "read", appaccess.PermSystemAnnouncementsView, false), true
	case strings.HasPrefix(path, "/api/v1/announcements"):
		return nonPlatformMutationEntry("Announcement", nonPlatformMutationAction(method, path), appaccess.PermSystemAnnouncementsManage, false), true
	}
	return nonPlatformMutationSecuritySurfaceEntry{}, false
}

func accessMutationSecuritySurface(method, path string) (nonPlatformMutationSecuritySurfaceEntry, bool) {
	switch {
	case strings.HasPrefix(path, "/api/v1/access/users"):
		return nonPlatformMutationEntry("AccessUser", accessRouteAction(method, path), appaccess.PermAccessUsersManage, false), true
	case strings.HasPrefix(path, "/api/v1/access/roles"):
		return nonPlatformMutationEntry("AccessRole", nonPlatformMutationAction(method, path), appaccess.PermAccessRolesManage, false), true
	case strings.HasPrefix(path, "/api/v1/access/teams"):
		return nonPlatformMutationEntry("AccessTeam", nonPlatformMutationAction(method, path), appaccess.PermAccessGroupsManage, false), true
	case strings.HasPrefix(path, "/api/v1/access/policies"):
		return nonPlatformMutationEntry("AccessPolicy", nonPlatformMutationAction(method, path), appaccess.PermAccessPoliciesManage, false), true
	case strings.HasPrefix(path, "/api/v1/access/scope-grants"):
		return nonPlatformMutationEntry("ScopeGrant", nonPlatformMutationAction(method, path), appaccess.PermAccessScopeGrantsManage, false), true
	}
	return nonPlatformMutationSecuritySurfaceEntry{}, false
}

func aiGatewayMutationSecuritySurface(method, path string) (nonPlatformMutationSecuritySurfaceEntry, bool) {
	switch {
	case strings.HasPrefix(path, "/api/v1/ai-gateway/llm/"):
		return nonPlatformMutationEntry("AIGatewayLLMRelayInvocation", "invoke", appaccess.PermAIGatewayRelayInvoke, true), true
	case strings.HasPrefix(path, "/api/v1/ai-gateway/relay/"):
		return nonPlatformMutationEntry("AIGatewayLLMRelay", nonPlatformMutationAction(method, path), appaccess.PermAIGatewayRelayManage, false), true
	case strings.HasPrefix(path, "/api/v1/ai-gateway/tools/"):
		return nonPlatformMutationEntry("AIGatewayToolInvocation", "invoke", appaccess.PermAIGatewayInvoke, true), true
	case strings.HasPrefix(path, "/api/v1/ai-gateway/resources/read"):
		return nonPlatformMutationEntry("AIGatewayResourceRead", "read", appaccess.PermAIGatewayInvoke, true), true
	case strings.HasPrefix(path, "/api/v1/ai-gateway/prompts/get"):
		return nonPlatformMutationEntry("AIGatewayPrompt", "get", appaccess.PermAIGatewayInvoke, true), true
	case strings.HasPrefix(path, "/api/v1/ai-gateway/approval-requests/"):
		return nonPlatformMutationEntry("AIGatewayApprovalRequest", nonPlatformMutationAction(method, path), appaccess.PermAIGatewayInvoke, true), true
	case strings.HasPrefix(path, "/api/v1/ai-gateway/personal-access-tokens"):
		return nonPlatformMutationEntry("AIGatewayPersonalAccessToken", nonPlatformMutationAction(method, path), appaccess.PermAIGatewayManage, false), true
	case strings.HasPrefix(path, "/api/v1/ai-gateway/service-accounts"):
		return nonPlatformMutationEntry("AIGatewayServiceAccount", nonPlatformMutationAction(method, path), appaccess.PermAIGatewayManage, false), true
	case strings.HasPrefix(path, "/api/v1/ai-gateway/service-account-tokens"):
		return nonPlatformMutationEntry("AIGatewayServiceAccountToken", nonPlatformMutationAction(method, path), appaccess.PermAIGatewayManage, false), true
	case strings.HasPrefix(path, "/api/v1/ai-gateway/ai-clients"):
		return nonPlatformMutationEntry("AIGatewayAIClient", nonPlatformMutationAction(method, path), appaccess.PermAIGatewayManage, false), true
	case strings.HasPrefix(path, "/api/v1/ai-gateway/tool-grants"):
		return nonPlatformMutationEntry("AIGatewayToolGrant", nonPlatformMutationAction(method, path), appaccess.PermAIGatewayManage, false), true
	case strings.HasPrefix(path, "/api/v1/ai-gateway/access-policies"):
		return nonPlatformMutationEntry("AIGatewayAccessPolicy", nonPlatformMutationAction(method, path), appaccess.PermAIGatewayManage, false), true
	case strings.HasPrefix(path, "/api/v1/ai-gateway/skill-bindings"):
		return nonPlatformMutationEntry("AIGatewaySkillBinding", nonPlatformMutationAction(method, path), appaccess.PermAIGatewayManage, false), true
	}
	return nonPlatformMutationSecuritySurfaceEntry{}, false
}

func pluginMutationSecuritySurface(method, path string) (nonPlatformMutationSecuritySurfaceEntry, bool) {
	switch {
	case strings.HasPrefix(path, "/api/v1/plugins/install"):
		return nonPlatformMutationEntry("Plugin", "install", appaccess.PermPluginInstall, false), true
	case strings.HasPrefix(path, "/api/v1/plugins/") && strings.HasSuffix(path, "/config"):
		return nonPlatformMutationEntry("PluginConfig", "update", appaccess.PermPluginConfigureSecrets, false), true
	case strings.HasPrefix(path, "/api/v1/plugins/"):
		return nonPlatformMutationEntry("Plugin", nonPlatformMutationAction(method, path), appaccess.PermPluginManage, false), true
	}
	return nonPlatformMutationSecuritySurfaceEntry{}, false
}

func identityMutationSecuritySurface(method, path string) (nonPlatformMutationSecuritySurfaceEntry, bool) {
	switch {
	case strings.HasPrefix(path, "/api/v1/portal/applications/") && strings.HasSuffix(path, "/launch"):
		return nonPlatformMutationEntry("IdentityApplicationLaunch", "launch", appaccess.PermIdentityPortalView, false), true
	case strings.HasPrefix(path, "/api/v1/portal/applications/") && strings.HasSuffix(path, "/favorite"):
		return nonPlatformMutationEntry("IdentityApplicationFavorite", nonPlatformMutationAction(method, path), appaccess.PermIdentityPortalView, false), true
	case strings.HasPrefix(path, "/api/v1/identity/applications"):
		return nonPlatformMutationEntry("IdentityApplication", nonPlatformMutationAction(method, path), appaccess.PermIdentityApplicationsManage, false), true
	case strings.HasPrefix(path, "/api/v1/identity/policies"):
		return nonPlatformMutationEntry("IdentityPolicy", nonPlatformMutationAction(method, path), appaccess.PermIdentityPoliciesManage, false), true
	case strings.HasPrefix(path, "/api/v1/identity/sessions/") && strings.HasSuffix(path, "/revoke"):
		return nonPlatformMutationEntry("IdentitySession", "revoke", appaccess.PermIdentitySessionsManage, false), true
	case strings.HasPrefix(path, "/api/v1/identity/outposts"):
		return nonPlatformMutationEntry("IdentityOutpost", nonPlatformMutationAction(method, path), appaccess.PermIdentityOutpostsManage, false), true
	case strings.HasPrefix(path, "/api/v1/identity/providers") && strings.Contains(path, "/oidc-clients"):
		return nonPlatformMutationEntry("IdentityOIDCClient", nonPlatformMutationAction(method, path), appaccess.PermIdentityProvidersManage, false), true
	case strings.HasPrefix(path, "/api/v1/identity/providers"):
		return nonPlatformMutationEntry("IdentityProvider", nonPlatformMutationAction(method, path), appaccess.PermIdentityProvidersManage, false), true
	case strings.HasPrefix(path, "/api/v1/identity/oidc-clients"):
		return nonPlatformMutationEntry("IdentityOIDCClient", nonPlatformMutationAction(method, path), appaccess.PermIdentityProvidersManage, false), true
	}
	return nonPlatformMutationSecuritySurfaceEntry{}, false
}

func settingsMutationSecuritySurface(method, path string) (nonPlatformMutationSecuritySurfaceEntry, bool) {
	switch {
	case strings.HasPrefix(path, "/api/v1/settings/identity"):
		return nonPlatformMutationEntry("IdentitySettings", "update", appaccess.PermSettingsIdentityManage, false), true
	case strings.HasPrefix(path, "/api/v1/settings/monitoring"):
		return nonPlatformMutationEntry("MonitoringSettings", "update", appaccess.PermSettingsMonitoringManage, false), true
	case strings.HasPrefix(path, "/api/v1/settings/ai"):
		return nonPlatformMutationEntry("AISettings", nonPlatformMutationAction(method, path), appaccess.PermSettingsAIManage, false), true
	case strings.HasPrefix(path, "/api/v1/settings/branding"):
		return nonPlatformMutationEntry("BrandingSettings", nonPlatformMutationAction(method, path), appaccess.PermSettingsBrandingManage, false), true
	}
	return nonPlatformMutationSecuritySurfaceEntry{}, false
}

func nonPlatformMutationEntry(resourceKind, action, permissionKey string, scopeRequired bool) nonPlatformMutationSecuritySurfaceEntry {
	return nonPlatformMutationSecuritySurfaceEntry{
		ResourceKind:  resourceKind,
		Action:        action,
		PermissionKey: permissionKey,
		ScopeRequired: scopeRequired,
	}
}

func deliveryApplicationPermission(method string) string {
	switch method {
	case "POST":
		return appaccess.PermDeliveryApplicationsCreate
	case "DELETE":
		return appaccess.PermDeliveryApplicationsDelete
	default:
		return appaccess.PermDeliveryApplicationsUpdate
	}
}

func monitoringAlertAction(path string) string {
	switch {
	case strings.HasSuffix(path, "/ownership"):
		return "assign"
	case strings.HasSuffix(path, "/acknowledge"):
		return "acknowledge"
	default:
		return "update"
	}
}

func monitoringAlertPermission(path string) string {
	switch {
	case strings.HasSuffix(path, "/ownership"):
		return appaccess.PermObserveAlertsAssign
	case strings.HasSuffix(path, "/acknowledge"):
		return appaccess.PermObserveAlertsAcknowledge
	default:
		return appaccess.PermObserveAlertsManage
	}
}

func accessRouteAction(method, path string) string {
	switch {
	case strings.HasSuffix(path, "/revoke-sessions"):
		return "revoke-sessions"
	case strings.HasSuffix(path, "/roles"):
		return "replace-roles"
	case strings.HasSuffix(path, "/teams"):
		return "replace-teams"
	default:
		return nonPlatformMutationAction(method, path)
	}
}

func nonPlatformMutationAction(method, path string) string {
	for _, marker := range []string{
		"approve",
		"reject",
		"cancel",
		"retry",
		"test",
		"sync",
		"validate",
		"publish",
		"withdraw",
		"enable",
		"disable",
		"upgrade",
		"rotate",
		"revoke",
		"power",
		"actions",
	} {
		if strings.HasSuffix(path, "/"+marker) {
			return marker
		}
	}
	switch method {
	case "POST":
		return "create"
	case "PUT", "PATCH":
		return "update"
	case "DELETE":
		return "delete"
	default:
		return strings.ToLower(method)
	}
}

func platformMutationAction(method, path string) string {
	switch {
	case strings.Contains(path, "/exec"):
		return "exec"
	case strings.Contains(path, "/restart"):
		return "restart"
	case strings.Contains(path, "/rollback"):
		return "rollback"
	case strings.Contains(path, "/scale"):
		return "scale"
	case strings.Contains(path, "/yaml"):
		return "update"
	case strings.Contains(path, "/port-forwards") && method == "POST":
		return "create"
	case strings.Contains(path, "/port-forwards") && method == "DELETE":
		return "delete"
	case strings.Contains(path, "/helm/charts/install"):
		return "create"
	case strings.Contains(path, "/helm/releases") && strings.Contains(path, "/values"):
		return "update"
	}

	switch method {
	case "POST":
		return "create"
	case "PUT":
		return "update"
	case "DELETE":
		return "delete"
	default:
		return strings.ToLower(method)
	}
}

func platformMutationCapabilityKey(path string) string {
	switch {
	case strings.Contains(path, "/namespaces"):
		return "namespace.lifecycle"
	case strings.Contains(path, "/workloads/pods") && strings.Contains(path, "/exec"):
		return "pod.exec"
	case strings.Contains(path, "/workloads/deployments/restart"),
		strings.Contains(path, "/workloads/deployments/rollback"),
		strings.Contains(path, "/workloads/deployments/scale"),
		strings.Contains(path, "/workloads/statefulsets/restart"),
		strings.Contains(path, "/workloads/statefulsets/scale"),
		strings.Contains(path, "/workloads/daemonsets/restart"):
		return "workload.mutations"
	case strings.Contains(path, "/workloads/") && strings.Contains(path, "/yaml"):
		return "resource.yaml.apply"
	case strings.Contains(path, "/workloads/"):
		return "workload.mutations"
	case strings.Contains(path, "/configuration/"):
		return "resource.yaml.apply"
	case strings.Contains(path, "/access-control/"):
		return "rbac.inventory"
	case strings.Contains(path, "/network/port-forwards"):
		return "port.forward"
	case strings.Contains(path, "/network/"):
		return "resource.yaml.apply"
	case strings.Contains(path, "/storage/"):
		return "storage.inventory"
	case strings.Contains(path, "/extensions/crds/"):
		return "custom.resources"
	case strings.Contains(path, "/helm/"):
		return "helm.releases"
	default:
		return "cluster.inventory"
	}
}

func platformMutationResourceKind(path string) string {
	switch {
	case path == "/api/v1/clusters" || strings.Contains(path, "/clusters/:clusterID") && !strings.Contains(path, "/clusters/:clusterID/"):
		return "Cluster"
	case strings.Contains(path, "/namespaces"):
		return "Namespace"
	case strings.Contains(path, "/infrastructure/nodes"):
		return "Node"
	case strings.Contains(path, "/workloads/pods"):
		return "Pod"
	case strings.Contains(path, "/workloads/deployments"):
		return "Deployment"
	case strings.Contains(path, "/workloads/statefulsets"):
		return "StatefulSet"
	case strings.Contains(path, "/workloads/daemonsets"):
		return "DaemonSet"
	case strings.Contains(path, "/workloads/replicasets"):
		return "ReplicaSet"
	case strings.Contains(path, "/workloads/jobs"):
		return "Job"
	case strings.Contains(path, "/workloads/cronjobs"):
		return "CronJob"
	case strings.Contains(path, "/workloads/replicationcontrollers"):
		return "ReplicationController"
	case strings.Contains(path, "/configuration/configmaps"):
		return "ConfigMap"
	case strings.Contains(path, "/configuration/secrets"):
		return "Secret"
	case strings.Contains(path, "/configuration/hpas"):
		return "HorizontalPodAutoscaler"
	case strings.Contains(path, "/configuration/poddisruptionbudgets"):
		return "PodDisruptionBudget"
	case strings.Contains(path, "/configuration/priorityclasses"):
		return "PriorityClass"
	case strings.Contains(path, "/configuration/runtimeclasses"):
		return "RuntimeClass"
	case strings.Contains(path, "/configuration/mutatingwebhookconfigurations"):
		return "MutatingWebhookConfiguration"
	case strings.Contains(path, "/configuration/validatingwebhookconfigurations"):
		return "ValidatingWebhookConfiguration"
	case strings.Contains(path, "/configuration/resourcequotas"):
		return "ResourceQuota"
	case strings.Contains(path, "/configuration/limitranges"):
		return "LimitRange"
	case strings.Contains(path, "/configuration/leases"):
		return "Lease"
	case strings.Contains(path, "/access-control/serviceaccounts"):
		return "ServiceAccount"
	case strings.Contains(path, "/access-control/roles"):
		return "Role"
	case strings.Contains(path, "/access-control/rolebindings"):
		return "RoleBinding"
	case strings.Contains(path, "/access-control/clusterroles"):
		return "ClusterRole"
	case strings.Contains(path, "/access-control/clusterrolebindings"):
		return "ClusterRoleBinding"
	case strings.Contains(path, "/network/services"):
		return "Service"
	case strings.Contains(path, "/network/ingresses"):
		return "Ingress"
	case strings.Contains(path, "/network/endpointslices"):
		return "EndpointSlice"
	case strings.Contains(path, "/network/networkpolicies"):
		return "NetworkPolicy"
	case strings.Contains(path, "/network/ingressclasses"):
		return "IngressClass"
	case strings.Contains(path, "/network/gatewayclasses"):
		return "GatewayClass"
	case strings.Contains(path, "/network/gateways"):
		return "Gateway"
	case strings.Contains(path, "/network/port-forwards"):
		return "PortForward"
	case strings.Contains(path, "/storage/persistentvolumeclaims"):
		return "PersistentVolumeClaim"
	case strings.Contains(path, "/storage/persistentvolumes"):
		return "PersistentVolume"
	case strings.Contains(path, "/storage/storageclasses"):
		return "StorageClass"
	case strings.Contains(path, "/extensions/crds/"):
		return "CustomResource"
	case strings.Contains(path, "/helm/"):
		return "HelmRelease"
	default:
		return ""
	}
}
