package aigateway

import (
	appaccess "github.com/soha/soha/internal/application/access"
	domainaigateway "github.com/soha/soha/internal/domain/aigateway"
)

func defaultTools() []domainaigateway.ToolCapability {
	return []domainaigateway.ToolCapability{
		{
			Name:           "delivery.applications.list",
			Title:          "List Applications",
			Description:    "List soha delivery applications available to the caller.",
			Domain:         "delivery",
			Action:         "list",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationsView},
			RequiredScopes: []string{"businessLine", "application"},
			MCPAdapterID:   "delivery.v1",
			MCPToolName:    "delivery.applications.list",
		},
		{
			Name:             "delivery.applications.create",
			Title:            "Create Application",
			Description:      "Create a delivery application through the application service.",
			Domain:           "delivery",
			Action:           "create",
			RiskLevel:        domainaigateway.RiskLevelMutate,
			PermissionKeys:   []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationsCreate},
			RequiredScopes:   []string{"businessLine"},
			MCPAdapterID:     "delivery.v1",
			MCPToolName:      "delivery.application.bootstrap",
			RequiresApproval: false,
		},
		{
			Name:           "delivery.application_environments.list",
			Title:          "List Application Environment Bindings",
			Description:    "List delivery environment bindings and release targets for an application.",
			Domain:         "delivery",
			Action:         "list",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationEnvView},
			RequiredScopes: []string{"application", "environment"},
			MCPAdapterID:   "delivery.v1",
			MCPToolName:    "delivery.targets.list",
		},
		{
			Name:             "delivery.actions.trigger",
			Title:            "Trigger Delivery Action",
			Description:      "Trigger build, deploy, build_deploy, workflow, or verify actions through the delivery service.",
			Domain:           "delivery",
			Action:           "execute",
			RiskLevel:        domainaigateway.RiskLevelExecute,
			PermissionKeys:   []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryBuildsTrigger, appaccess.PermDeliveryReleasesTrigger},
			RequiredScopes:   []string{"application", "environment"},
			MCPAdapterID:     "delivery.v1",
			MCPToolName:      "delivery.execution.start",
			RequiresApproval: true,
		},
		{
			Name:           "delivery.release_bundles.list",
			Title:          "List Release Bundles",
			Description:    "List immutable release bundles and artifacts for delivery analysis.",
			Domain:         "delivery",
			Action:         "list",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryReleaseBundlesView},
			RequiredScopes: []string{"application", "environment"},
			MCPAdapterID:   "delivery.v1",
			MCPToolName:    "delivery.release_bundles.list",
		},
		{
			Name:           "delivery.execution_tasks.list",
			Title:          "List Execution Tasks",
			Description:    "List durable execution tasks, status, callbacks, and artifacts.",
			Domain:         "delivery",
			Action:         "list",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryExecutionTasksView},
			RequiredScopes: []string{"application", "environment"},
			MCPAdapterID:   "delivery.v1",
			MCPToolName:    "delivery.execution_tasks.list",
		},
		{
			Name:           "k8s.pods.list",
			Title:          "List Pods",
			Description:    "List pods from the scoped Kubernetes workbench API.",
			Domain:         "k8s",
			Action:         "list",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermWorkspaceResourceView, appaccess.PermPlatformWorkloadsView},
			RequiredScopes: []string{"cluster", "namespace"},
			MCPAdapterID:   "platform-native.v1",
			MCPToolName:    "k8s.pods.list",
		},
		{
			Name:           "k8s.pods.logs",
			Title:          "Read Pod Logs",
			Description:    "Read recent pod logs through the platform resource service with audit boundaries.",
			Domain:         "k8s",
			Action:         "logs",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermWorkspaceResourceView, appaccess.PermPlatformWorkloadsView},
			RequiredScopes: []string{"cluster", "namespace", "pod"},
			MCPAdapterID:   "platform-native.v1",
			MCPToolName:    "k8s.pods.logs",
		},
		{
			Name:           "k8s.deployments.list",
			Title:          "List Deployments",
			Description:    "List deployments from the scoped Kubernetes workbench API.",
			Domain:         "k8s",
			Action:         "list",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermWorkspaceResourceView, appaccess.PermPlatformWorkloadsView},
			RequiredScopes: []string{"cluster", "namespace"},
			MCPAdapterID:   "platform-native.v1",
			MCPToolName:    "k8s.deployments.list",
		},
		{
			Name:           "k8s.services.list",
			Title:          "List Services",
			Description:    "List services and backend linkage from the network workbench API.",
			Domain:         "k8s",
			Action:         "list",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermWorkspaceResourceView, appaccess.PermPlatformNetworkView},
			RequiredScopes: []string{"cluster", "namespace"},
			MCPAdapterID:   "platform-native.v1",
			MCPToolName:    "k8s.services.list",
		},
		{
			Name:           "k8s.events.list",
			Title:          "List Events",
			Description:    "List Kubernetes and platform events for scoped diagnosis.",
			Domain:         "k8s",
			Action:         "list",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermWorkspaceResourceView, appaccess.PermObserveEventsView},
			RequiredScopes: []string{"cluster", "namespace", "timeRange"},
			MCPAdapterID:   "platform-native.v1",
			MCPToolName:    "k8s.events",
		},
		{
			Name:             "diagnosis.release_failure.analyze",
			Title:            "Analyze Release Failure",
			Description:      "Create a soha-owned delivery failure diagnosis using release, task, log, event, and runtime context.",
			Domain:           "ai",
			Action:           "analyze",
			RiskLevel:        domainaigateway.RiskLevelAnalyze,
			PermissionKeys:   []string{appaccess.PermAIGatewayInvoke, appaccess.PermObserveAIChatUse, appaccess.PermDeliveryExecutionTasksView},
			RequiredScopes:   []string{"application", "environment", "executionTask"},
			MCPAdapterID:     "platform-native.v1",
			MCPToolName:      "diagnosis.release_failure.analyze",
			RequiresApproval: false,
		},
	}
}

func defaultResources() []domainaigateway.ResourceCapability {
	return []domainaigateway.ResourceCapability{
		{
			Name:           "soha://delivery/applications",
			Description:    "Delivery applications, build sources, service components, and environment bindings.",
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationsView},
			RequiredScopes: []string{"businessLine", "application"},
		},
		{
			Name:           "soha://delivery/execution-tasks",
			Description:    "Durable delivery execution tasks, callbacks, artifacts, and logs.",
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryExecutionTasksView},
			RequiredScopes: []string{"application", "environment"},
		},
		{
			Name:           "soha://k8s/runtime",
			Description:    "Scoped Kubernetes runtime inventory, events, logs, and workload status.",
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermWorkspaceResourceView},
			RequiredScopes: []string{"cluster", "namespace"},
		},
	}
}

func defaultPrompts() []domainaigateway.PromptCapability {
	return []domainaigateway.PromptCapability{
		{
			Name:           "soha.delivery.plan_release",
			Description:    "Guide an AI client through application environment checks before triggering a release.",
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationsView},
			RequiredScopes: []string{"application", "environment"},
		},
		{
			Name:           "soha.k8s.diagnose_workload",
			Description:    "Collect scoped workload evidence and generate a diagnosis plan without mutating cluster state.",
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermWorkspaceResourceView},
			RequiredScopes: []string{"cluster", "namespace", "workload"},
		},
	}
}

func defaultSkills() []domainaigateway.SkillCapability {
	return []domainaigateway.SkillCapability{
		{
			ID:             "delivery-developer",
			Name:           "Delivery Developer",
			Category:       "delivery",
			Description:    "Application onboarding and self-service build/deploy workflow for AI coding tools.",
			CapabilityRefs: []string{"delivery.applications.list", "delivery.applications.create", "delivery.actions.trigger"},
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationsView},
			RequiredScopes: []string{"businessLine", "application", "environment"},
		},
		{
			ID:             "delivery-tester",
			Name:           "Delivery Tester",
			Category:       "delivery",
			Description:    "Test environment release verification, execution task review, and promotion evidence collection.",
			CapabilityRefs: []string{"delivery.application_environments.list", "delivery.execution_tasks.list", "diagnosis.release_failure.analyze"},
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryExecutionTasksView},
			RequiredScopes: []string{"application", "environment"},
		},
		{
			ID:             "k8s-sre",
			Name:           "K8s SRE",
			Category:       "platform",
			Description:    "Read-only Kubernetes runtime diagnosis for pods, services, events, and deployment status.",
			CapabilityRefs: []string{"k8s.pods.list", "k8s.pods.logs", "k8s.deployments.list", "k8s.events.list"},
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermWorkspaceResourceView},
			RequiredScopes: []string{"cluster", "namespace"},
		},
		{
			ID:             "security-change",
			Name:           "Security Change",
			Category:       "security",
			Description:    "Security-sensitive change planning, approval handoff, rollback criteria, and evidence collection.",
			CapabilityRefs: []string{"delivery.actions.trigger", "delivery.execution_tasks.list", "k8s.events.list"},
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke},
			RequiredScopes: []string{"application", "environment", "cluster", "namespace"},
		},
	}
}
