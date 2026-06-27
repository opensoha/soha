package aigateway

import (
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
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
			InputSchema: gatewayObjectSchema(nil, map[string]any{
				"search": gatewayStringSchema("Optional application name, key, or repository search text."),
				"limit":  gatewayIntegerSchema("Maximum applications to return."),
			}),
		},
		{
			Name:           "delivery.applications.detail",
			Title:          "Get Application Detail",
			Description:    "Read one delivery application detail with bindings, targets, latest release bundle, and execution summary.",
			Domain:         "delivery",
			Action:         "read",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationsView},
			RequiredScopes: []string{"businessLine", "application"},
			MCPAdapterID:   "delivery.v1",
			MCPToolName:    "delivery.applications.detail",
			InputSchema:    gatewayObjectSchema([]string{"applicationId"}, gatewayApplicationIDProperties()),
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
			MCPToolName:      "delivery.applications.create",
			RequiresApproval: false,
			InputSchema: gatewayObjectSchema([]string{"name", "key"}, map[string]any{
				"id":                  gatewayStringSchema("Optional caller-provided application id."),
				"name":                gatewayStringSchema("Application display name."),
				"key":                 gatewayStringSchema("Stable application key."),
				"group":               gatewayStringSchema("Optional application group."),
				"businessLineId":      gatewayStringSchema("Business line id."),
				"language":            gatewayStringSchema("Primary language or runtime."),
				"description":         gatewayStringSchema("Application description."),
				"ownerTeam":           gatewayStringSchema("Owning team."),
				"repositoryProvider":  gatewayStringSchema("Repository provider key."),
				"repositoryProjectId": gatewayStringSchema("Repository project id."),
				"repositoryPath":      gatewayStringSchema("Repository path."),
				"defaultBranch":       gatewayStringSchema("Default source branch."),
				"defaultTag":          gatewayStringSchema("Default image or release tag."),
				"buildImage":          gatewayStringSchema("Default build image."),
				"buildContextDir":     gatewayStringSchema("Build context directory."),
				"dockerfilePath":      gatewayStringSchema("Dockerfile path."),
				"enabled":             gatewayBooleanSchema("Whether the application is enabled."),
				"metadata":            gatewayFreeformObjectSchema("Application metadata."),
				"buildSources":        gatewayArraySchema("Optional build source definitions."),
			}),
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
			MCPToolName:    "delivery.application_environments.list",
			InputSchema:    gatewayObjectSchema(nil, gatewayApplicationBindingProperties()),
		},
		{
			Name:           "delivery.application_services.list",
			Title:          "List Application Services",
			Description:    "List service components and container configuration summaries for an application.",
			Domain:         "delivery",
			Action:         "list",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationServicesView},
			RequiredScopes: []string{"application"},
			MCPAdapterID:   "delivery.v1",
			MCPToolName:    "delivery.application_services.list",
			InputSchema:    gatewayObjectSchema([]string{"applicationId"}, gatewayApplicationIDProperties()),
		},
		{
			Name:           "delivery.build_sources.list",
			Title:          "List Build Sources",
			Description:    "List build sources for a delivery application and optionally show binding usage.",
			Domain:         "delivery",
			Action:         "list",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationsView},
			RequiredScopes: []string{"application"},
			MCPAdapterID:   "delivery.v1",
			MCPToolName:    "delivery.build_sources.list",
			InputSchema: gatewayObjectSchema([]string{"applicationId"}, map[string]any{
				"applicationId": gatewayStringSchema("Delivery application id."),
				"withBindings":  gatewayBooleanSchema("Include application environment binding usage for each build source."),
			}),
		},
		{
			Name:           "delivery.release_targets.list",
			Title:          "List Release Targets",
			Description:    "List release targets from application environment bindings.",
			Domain:         "delivery",
			Action:         "list",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationEnvView},
			RequiredScopes: []string{"application", "environment"},
			MCPAdapterID:   "delivery.v1",
			MCPToolName:    "delivery.release_targets.list",
			InputSchema:    gatewayObjectSchema(nil, gatewayApplicationBindingProperties()),
		},
		{
			Name:             "delivery.actions.trigger",
			Title:            "Trigger Delivery Action",
			Description:      "Trigger build, deploy, build_deploy, workflow, verify, or rollback actions through the delivery service.",
			Domain:           "delivery",
			Action:           "execute",
			RiskLevel:        domainaigateway.RiskLevelExecute,
			PermissionKeys:   []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryBuildsTrigger, appaccess.PermDeliveryReleasesTrigger},
			RequiredScopes:   []string{"application", "environment"},
			MCPAdapterID:     "delivery.v1",
			MCPToolName:      "delivery.actions.trigger",
			RequiresApproval: true,
			InputSchema: gatewayObjectSchema([]string{"applicationId", "action"}, map[string]any{
				"applicationId":            gatewayStringSchema("Delivery application id."),
				"applicationEnvironmentId": gatewayStringSchema("Target application environment binding id."),
				"action": map[string]any{
					"type":        "string",
					"description": "Delivery action to trigger.",
					"enum":        []any{"build", "deploy", "build_deploy", "workflow", "verify", "rollback"},
				},
				"buildSourceId":   gatewayStringSchema("Optional build source id for build-oriented actions."),
				"workflowId":      gatewayStringSchema("Optional workflow template id for workflow actions."),
				"releaseBundleId": gatewayStringSchema("Release bundle id, required for rollback and often used for deploy."),
				"reason":          gatewayStringSchema("Human-readable reason for audit and approval context."),
				"variables":       gatewayFreeformObjectSchema("Workflow or action variables."),
			}),
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
			InputSchema:    gatewayObjectSchema(nil, gatewayReleaseBundleListProperties()),
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
			InputSchema:    gatewayObjectSchema(nil, gatewayExecutionTaskListProperties()),
		},
		{
			Name:           "delivery.execution_logs.list",
			Title:          "List Execution Logs",
			Description:    "Read redacted logs for a durable execution task.",
			Domain:         "delivery",
			Action:         "logs",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryExecutionTasksView},
			RequiredScopes: []string{"application", "environment", "executionTask"},
			MCPAdapterID:   "delivery.v1",
			MCPToolName:    "delivery.execution_logs.list",
			InputSchema: gatewayObjectSchema([]string{"taskId"}, map[string]any{
				"taskId": gatewayStringSchema("Execution task id."),
				"limit":  gatewayIntegerSchema("Maximum log records to return."),
			}),
		},
		{
			Name:           "delivery.workflow_templates.list",
			Title:          "List Workflow Templates",
			Description:    "List delivery workflow templates for build, release, verification, and approval planning.",
			Domain:         "delivery",
			Action:         "list",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryWorkflowTemplatesView},
			RequiredScopes: []string{"application", "environment"},
			MCPAdapterID:   "delivery.v1",
			MCPToolName:    "delivery.workflow_templates.list",
			InputSchema:    gatewayObjectSchema(nil, map[string]any{}),
		},
		{
			Name:           "delivery.onboarding.analyze_repo",
			Title:          "Analyze Repository For Onboarding",
			Description:    "Analyze credential-free repository metadata and return a DeliveryDraft-shaped onboarding suggestion without creating platform objects.",
			Domain:         "delivery",
			Action:         "analyze",
			RiskLevel:      domainaigateway.RiskLevelAnalyze,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationsView},
			RequiredScopes: []string{"businessLine", "repository"},
			MCPAdapterID:   "delivery.v1",
			MCPToolName:    "delivery.onboarding.analyze_repo",
			InputSchema:    gatewayObjectSchema([]string{"repositoryPath"}, gatewayOnboardingAnalyzeRepoProperties()),
		},
		{
			Name:           "delivery.standards.dockerfile.generate",
			Title:          "Generate Dockerfile Draft",
			Description:    "Generate a platform-standard Dockerfile draft from credential-free runtime and build metadata.",
			Domain:         "delivery",
			Action:         "generate",
			RiskLevel:      domainaigateway.RiskLevelAnalyze,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationsView},
			RequiredScopes: []string{"businessLine", "repository"},
			MCPAdapterID:   "delivery.v1",
			MCPToolName:    "delivery.standards.dockerfile.generate",
			InputSchema:    gatewayObjectSchema([]string{"language"}, gatewayDockerfileGenerateProperties()),
		},
		{
			Name:           "delivery.standards.dockerfile.validate",
			Title:          "Validate Dockerfile Draft",
			Description:    "Validate Dockerfile content against platform baseline checks and return findings only.",
			Domain:         "delivery",
			Action:         "validate",
			RiskLevel:      domainaigateway.RiskLevelAnalyze,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationsView},
			RequiredScopes: []string{"businessLine", "repository"},
			MCPAdapterID:   "delivery.v1",
			MCPToolName:    "delivery.standards.dockerfile.validate",
			InputSchema:    gatewayObjectSchema([]string{"content"}, gatewayDockerfileValidateProperties()),
		},
		{
			Name:           "delivery.standards.helm.generate",
			Title:          "Generate Helm Draft",
			Description:    "Generate Helm chart and values drafts for preview, without writing repository files.",
			Domain:         "delivery",
			Action:         "generate",
			RiskLevel:      domainaigateway.RiskLevelAnalyze,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationsView},
			RequiredScopes: []string{"businessLine", "repository", "environment"},
			MCPAdapterID:   "delivery.v1",
			MCPToolName:    "delivery.standards.helm.generate",
			InputSchema:    gatewayObjectSchema([]string{"serviceName", "imageRepository"}, gatewayHelmGenerateProperties()),
		},
		{
			Name:           "delivery.standards.k8s.validate",
			Title:          "Validate Kubernetes Manifests",
			Description:    "Validate Kubernetes manifest drafts for probes, resource limits, selectors, and security context.",
			Domain:         "delivery",
			Action:         "validate",
			RiskLevel:      domainaigateway.RiskLevelAnalyze,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationsView},
			RequiredScopes: []string{"application", "environment"},
			MCPAdapterID:   "delivery.v1",
			MCPToolName:    "delivery.standards.k8s.validate",
			InputSchema:    gatewayObjectSchema([]string{"manifests"}, gatewayKubernetesValidateProperties()),
		},
		{
			Name:           "delivery.spec.render",
			Title:          "Render Delivery Spec",
			Description:    "Render user input and AI suggestions into a DeliveryDraft-compatible delivery spec for preview.",
			Domain:         "delivery",
			Action:         "render",
			RiskLevel:      domainaigateway.RiskLevelAnalyze,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationsView},
			RequiredScopes: []string{"businessLine", "application", "environment"},
			MCPAdapterID:   "delivery.v1",
			MCPToolName:    "delivery.spec.render",
			InputSchema:    gatewayObjectSchema([]string{"applicationDraft"}, gatewayDeliverySpecRenderProperties()),
		},
		{
			Name:           "delivery.application.bootstrap",
			Title:          "Prepare Application Bootstrap",
			Description:    "Prepare a DeliveryDraft bootstrap payload and confirm API handoff; this tool never creates or updates platform objects directly.",
			Domain:         "delivery",
			Action:         "plan",
			RiskLevel:      domainaigateway.RiskLevelAnalyze,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationsView},
			RequiredScopes: []string{"businessLine", "application", "environment"},
			MCPAdapterID:   "delivery.v1",
			MCPToolName:    "delivery.application.bootstrap",
			InputSchema:    gatewayObjectSchema(nil, gatewayDeliveryApplicationBootstrapProperties()),
		},
		{
			Name:           "delivery.release.plan",
			Title:          "Plan Delivery Release",
			Description:    "Build a DeliveryPlan-compatible release plan from user intent and delivery context without triggering execution.",
			Domain:         "delivery",
			Action:         "plan",
			RiskLevel:      domainaigateway.RiskLevelAnalyze,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationsView, appaccess.PermDeliveryApplicationEnvView},
			RequiredScopes: []string{"application", "environment"},
			MCPAdapterID:   "delivery.v1",
			MCPToolName:    "delivery.release.plan",
			InputSchema:    gatewayObjectSchema([]string{"applicationId", "applicationEnvironmentId", "action"}, gatewayDeliveryReleasePlanProperties()),
		},
		{
			Name:           "delivery.release_context.diff",
			Title:          "Build Release Diff Context",
			Description:    "Collect read-only candidate promotion context and compare release bundles, bindings, targets, and recent execution state.",
			Domain:         "delivery",
			Action:         "analyze",
			RiskLevel:      domainaigateway.RiskLevelAnalyze,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationsView, appaccess.PermDeliveryReleaseBundlesView, appaccess.PermDeliveryExecutionTasksView},
			RequiredScopes: []string{"application", "environment", "releaseBundle"},
			MCPAdapterID:   "delivery.v1",
			MCPToolName:    "delivery.release_context.diff",
			InputSchema: gatewayObjectSchema([]string{"applicationId"}, map[string]any{
				"applicationId":            gatewayStringSchema("Delivery application id."),
				"applicationEnvironmentId": gatewayStringSchema("Optional application environment binding id."),
				"sourceBundleId":           gatewayStringSchema("Optional source release bundle id."),
				"targetBundleId":           gatewayStringSchema("Optional target release bundle id."),
				"releaseBundleId":          gatewayStringSchema("Alias for targetBundleId."),
				"limit":                    gatewayIntegerSchema("Maximum release bundle and task records to inspect."),
			}),
		},
		{
			Name:           "delivery.rollback.context",
			Title:          "Build Rollback Context",
			Description:    "Collect read-only rollback suggestion context from release bundles, execution tasks, logs, and environment targets.",
			Domain:         "delivery",
			Action:         "analyze",
			RiskLevel:      domainaigateway.RiskLevelAnalyze,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationsView, appaccess.PermDeliveryReleaseBundlesView, appaccess.PermDeliveryExecutionTasksView},
			RequiredScopes: []string{"application", "environment", "releaseBundle", "executionTask"},
			MCPAdapterID:   "delivery.v1",
			MCPToolName:    "delivery.rollback.context",
			InputSchema: gatewayObjectSchema([]string{"applicationId"}, map[string]any{
				"applicationId":            gatewayStringSchema("Delivery application id."),
				"applicationEnvironmentId": gatewayStringSchema("Optional application environment binding id."),
				"releaseBundleId":          gatewayStringSchema("Optional release bundle id to inspect."),
				"executionTaskId":          gatewayStringSchema("Optional execution task id to anchor rollback evidence."),
				"limit":                    gatewayIntegerSchema("Maximum release bundle and task records to inspect."),
				"logLimit":                 gatewayIntegerSchema("Maximum execution log records to include."),
			}),
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
			InputSchema:    gatewayObjectSchema([]string{"clusterId"}, gatewayClusterNamespaceProperties("Pod namespace. Empty means all namespaces when allowed.")),
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
			InputSchema: gatewayObjectSchema([]string{"clusterId", "namespace", "podName"}, map[string]any{
				"clusterId":    gatewayStringSchema("Cluster id."),
				"namespace":    gatewayStringSchema("Pod namespace."),
				"podName":      gatewayStringSchema("Pod name."),
				"container":    gatewayStringSchema("Optional container name."),
				"tailLines":    gatewayIntegerSchema("Number of recent log lines to read."),
				"sinceSeconds": gatewayIntegerSchema("Only return logs newer than this many seconds."),
				"previous":     gatewayBooleanSchema("Read previous terminated container logs."),
			}),
		},
		{
			Name:           "k8s.pods.describe",
			Title:          "Describe Pod",
			Description:    "Read a describe-style pod summary with containers, conditions, volumes, and related resources.",
			Domain:         "k8s",
			Action:         "read",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermWorkspaceResourceView, appaccess.PermPlatformWorkloadsView},
			RequiredScopes: []string{"cluster", "namespace", "pod"},
			MCPAdapterID:   "platform-native.v1",
			MCPToolName:    "k8s.pods.describe",
			InputSchema: gatewayObjectSchema([]string{"clusterId", "namespace", "podName"}, map[string]any{
				"clusterId": gatewayStringSchema("Cluster id."),
				"namespace": gatewayStringSchema("Pod namespace."),
				"podName":   gatewayStringSchema("Pod name."),
			}),
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
			InputSchema:    gatewayObjectSchema([]string{"clusterId"}, gatewayClusterNamespaceProperties("Deployment namespace. Empty means all namespaces when allowed.")),
		},
		{
			Name:           "k8s.deployments.rollout_status",
			Title:          "Read Deployment Rollout Status",
			Description:    "Read rollout status, replica progress, revision, and conditions for one deployment.",
			Domain:         "k8s",
			Action:         "read",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermWorkspaceResourceView, appaccess.PermPlatformWorkloadsView},
			RequiredScopes: []string{"cluster", "namespace", "deployment"},
			MCPAdapterID:   "platform-native.v1",
			MCPToolName:    "k8s.deployments.rollout_status",
			InputSchema: gatewayObjectSchema([]string{"clusterId", "namespace", "deploymentName"}, map[string]any{
				"clusterId":      gatewayStringSchema("Cluster id."),
				"namespace":      gatewayStringSchema("Deployment namespace."),
				"deploymentName": gatewayStringSchema("Deployment name."),
			}),
		},
		{
			Name:           "k8s.deployments.events",
			Title:          "List Deployment Events",
			Description:    "List cluster events related to one deployment in the selected namespace.",
			Domain:         "k8s",
			Action:         "list",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermWorkspaceResourceView, appaccess.PermObserveEventsView},
			RequiredScopes: []string{"cluster", "namespace", "deployment", "timeRange"},
			MCPAdapterID:   "platform-native.v1",
			MCPToolName:    "k8s.deployments.events",
			InputSchema: gatewayObjectSchema([]string{"clusterId", "namespace", "deploymentName"}, map[string]any{
				"clusterId":      gatewayStringSchema("Cluster id."),
				"namespace":      gatewayStringSchema("Deployment namespace."),
				"deploymentName": gatewayStringSchema("Deployment name."),
				"limit":          gatewayIntegerSchema("Maximum events to inspect."),
			}),
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
			InputSchema:    gatewayObjectSchema([]string{"clusterId"}, gatewayClusterNamespaceProperties("Service namespace. Empty means all namespaces when allowed.")),
		},
		{
			Name:           "k8s.services.backends",
			Title:          "Read Service Backends",
			Description:    "Read service selector, matching pods, and related ingress route hints for one service.",
			Domain:         "k8s",
			Action:         "read",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermWorkspaceResourceView, appaccess.PermPlatformNetworkView, appaccess.PermPlatformWorkloadsView},
			RequiredScopes: []string{"cluster", "namespace", "service"},
			MCPAdapterID:   "platform-native.v1",
			MCPToolName:    "k8s.services.backends",
			InputSchema: gatewayObjectSchema([]string{"clusterId", "namespace", "serviceName"}, map[string]any{
				"clusterId":   gatewayStringSchema("Cluster id."),
				"namespace":   gatewayStringSchema("Service namespace."),
				"serviceName": gatewayStringSchema("Service name."),
			}),
		},
		{
			Name:           "k8s.routes.context",
			Title:          "Read Route Context",
			Description:    "Read ingress and Gateway API route context for a namespace or service without returning raw Kubernetes objects.",
			Domain:         "k8s",
			Action:         "read",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermWorkspaceResourceView, appaccess.PermPlatformNetworkView},
			RequiredScopes: []string{"cluster", "namespace", "service"},
			MCPAdapterID:   "platform-native.v1",
			MCPToolName:    "k8s.routes.context",
			InputSchema: gatewayObjectSchema([]string{"clusterId"}, map[string]any{
				"clusterId":   gatewayStringSchema("Cluster id."),
				"namespace":   gatewayStringSchema("Namespace for route context. Empty means all namespaces when allowed."),
				"serviceName": gatewayStringSchema("Optional service name to focus route context."),
			}),
		},
		{
			Name:           "k8s.storage.context",
			Title:          "Read Storage Context",
			Description:    "Read PVC, PV, and storage class summaries for storage diagnosis.",
			Domain:         "k8s",
			Action:         "read",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermWorkspaceResourceView, appaccess.PermPlatformStorageView},
			RequiredScopes: []string{"cluster", "namespace", "storage"},
			MCPAdapterID:   "platform-native.v1",
			MCPToolName:    "k8s.storage.context",
			InputSchema:    gatewayObjectSchema([]string{"clusterId"}, gatewayClusterNamespaceProperties("Namespace for PVC context. Empty includes cluster storage summary when allowed.")),
		},
		{
			Name:           "k8s.nodes.detail",
			Title:          "Read Node Detail",
			Description:    "Read node conditions, resource summary, taints, and scheduled pod context.",
			Domain:         "k8s",
			Action:         "read",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermWorkspaceResourceView, appaccess.PermPlatformNodesView},
			RequiredScopes: []string{"cluster", "node"},
			MCPAdapterID:   "platform-native.v1",
			MCPToolName:    "k8s.nodes.detail",
			InputSchema: gatewayObjectSchema([]string{"clusterId", "nodeName"}, map[string]any{
				"clusterId": gatewayStringSchema("Cluster id."),
				"nodeName":  gatewayStringSchema("Node name."),
			}),
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
			MCPToolName:    "k8s.events.list",
			InputSchema: gatewayObjectSchema([]string{"clusterId"}, map[string]any{
				"clusterId": gatewayStringSchema("Cluster id."),
				"namespace": gatewayStringSchema("Event namespace. Empty means all namespaces when allowed."),
				"limit":     gatewayIntegerSchema("Maximum events to return."),
			}),
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
			InputSchema: gatewayObjectSchema([]string{"applicationId"}, map[string]any{
				"applicationId":            gatewayStringSchema("Delivery application id."),
				"applicationEnvironmentId": gatewayStringSchema("Optional application environment binding id."),
				"releaseBundleId":          gatewayStringSchema("Optional release bundle id."),
				"executionTaskId":          gatewayStringSchema("Optional execution task id."),
				"clusterId":                gatewayStringSchema("Optional cluster id for runtime context."),
				"namespace":                gatewayStringSchema("Optional namespace for runtime context."),
				"workloadKind":             gatewayStringSchema("Optional workload kind, such as Deployment or Pod."),
				"workloadName":             gatewayStringSchema("Optional workload name."),
				"podName":                  gatewayStringSchema("Optional pod name for log evidence."),
				"container":                gatewayStringSchema("Optional container name."),
				"logLimit":                 gatewayIntegerSchema("Maximum log records to include."),
				"eventLimit":               gatewayIntegerSchema("Maximum event records to include."),
				"agentProviderId":          gatewayStringSchema("Optional external Agent Runtime provider id for deep analysis, such as hermes."),
				"providerId":               gatewayStringSchema("Alias for agentProviderId."),
				"deepAnalysis":             gatewayBooleanSchema("Queue external Agent Runtime deep analysis instead of only recording an internal artifact."),
				"externalAnalysis":         gatewayBooleanSchema("Alias for deepAnalysis."),
				"timeoutSeconds":           gatewayIntegerSchema("Agent Runtime timeout for external deep analysis."),
			}),
		},
		{
			Name:           "gateway.manifest.read",
			Title:          "Read Gateway Manifest",
			Description:    "Read the caller-filtered AI Gateway manifest for governance and client compatibility review.",
			Domain:         "gateway",
			Action:         "read",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermAIGatewayView},
			RequiredScopes: []string{"aiClient", "skill"},
			MCPAdapterID:   "gateway-governance.v1",
			MCPToolName:    "gateway.manifest.read",
			InputSchema:    gatewayObjectSchema(nil, gatewayManifestReadProperties()),
		},
		{
			Name:           "gateway.clients.list",
			Title:          "List AI Gateway Clients",
			Description:    "List AI Gateway clients and registration posture for governance review.",
			Domain:         "gateway",
			Action:         "list",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermAIGatewayManage},
			RequiredScopes: []string{"aiClient"},
			MCPAdapterID:   "gateway-governance.v1",
			MCPToolName:    "gateway.clients.list",
			InputSchema:    gatewayObjectSchema(nil, gatewayAIClientListProperties()),
		},
		{
			Name:           "gateway.tokens.list",
			Title:          "List AI Gateway Tokens",
			Description:    "List redacted personal and service account token metadata for Gateway governance review.",
			Domain:         "gateway",
			Action:         "list",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermAIGatewayManage},
			RequiredScopes: []string{"token"},
			MCPAdapterID:   "gateway-governance.v1",
			MCPToolName:    "gateway.tokens.list",
			InputSchema:    gatewayObjectSchema(nil, gatewayTokenListProperties()),
		},
		{
			Name:           "gateway.service_accounts.list",
			Title:          "List AI Gateway Service Accounts",
			Description:    "List AI Gateway service accounts without token values for governance review.",
			Domain:         "gateway",
			Action:         "list",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermAIGatewayManage},
			RequiredScopes: []string{"serviceAccount"},
			MCPAdapterID:   "gateway-governance.v1",
			MCPToolName:    "gateway.service_accounts.list",
			InputSchema:    gatewayObjectSchema(nil, gatewayServiceAccountListProperties()),
		},
		{
			Name:           "gateway.tool_grants.list",
			Title:          "List Tool Grants",
			Description:    "List AI Gateway tool grants by subject, client, and tool.",
			Domain:         "gateway",
			Action:         "list",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermAIGatewayManage},
			RequiredScopes: []string{"subject", "aiClient", "tool"},
			MCPAdapterID:   "gateway-governance.v1",
			MCPToolName:    "gateway.tool_grants.list",
			InputSchema:    gatewayObjectSchema(nil, gatewayToolGrantListProperties()),
		},
		{
			Name:           "gateway.access_policies.list",
			Title:          "List Gateway Access Policies",
			Description:    "List AI Gateway access policies by subject, client, and effect.",
			Domain:         "gateway",
			Action:         "list",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermAIGatewayManage},
			RequiredScopes: []string{"subject", "aiClient", "policy"},
			MCPAdapterID:   "gateway-governance.v1",
			MCPToolName:    "gateway.access_policies.list",
			InputSchema:    gatewayObjectSchema(nil, gatewayAccessPolicyListProperties()),
		},
		{
			Name:           "gateway.skill_bindings.list",
			Title:          "List Skill Bindings",
			Description:    "List AI Gateway skill bindings by subject, client, and skill.",
			Domain:         "gateway",
			Action:         "list",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermAIGatewayManage},
			RequiredScopes: []string{"aiClient", "skill"},
			MCPAdapterID:   "gateway-governance.v1",
			MCPToolName:    "gateway.skill_bindings.list",
			InputSchema:    gatewayObjectSchema(nil, gatewaySkillBindingListProperties()),
		},
		{
			Name:           "gateway.approvals.list",
			Title:          "List Gateway Approvals",
			Description:    "List redacted AI Gateway approval requests and decision metadata.",
			Domain:         "gateway",
			Action:         "list",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermAIGatewayManage},
			RequiredScopes: []string{"approval"},
			MCPAdapterID:   "gateway-governance.v1",
			MCPToolName:    "gateway.approvals.list",
			InputSchema:    gatewayObjectSchema(nil, gatewayApprovalListProperties()),
		},
		{
			Name:             "gateway.approvals.decide",
			Title:            "Decide Gateway Approval",
			Description:      "Approve, reject, or cancel a pending AI Gateway approval request.",
			Domain:           "gateway",
			Action:           "execute",
			RiskLevel:        domainaigateway.RiskLevelExecute,
			PermissionKeys:   []string{appaccess.PermAIGatewayInvoke, appaccess.PermAIGatewayManage},
			RequiredScopes:   []string{"approval"},
			MCPAdapterID:     "gateway-governance.v1",
			MCPToolName:      "gateway.approvals.decide",
			InputSchema:      gatewayObjectSchema([]string{"approvalRequestId", "decision"}, gatewayApprovalDecideProperties()),
		},
		{
			Name:           "gateway.audit_logs.list",
			Title:          "List Gateway Audit Logs",
			Description:    "List redacted AI Gateway audit logs by actor, client, skill, tool, approval, and result.",
			Domain:         "gateway",
			Action:         "list",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermAIGatewayManage},
			RequiredScopes: []string{"audit"},
			MCPAdapterID:   "gateway-governance.v1",
			MCPToolName:    "gateway.audit_logs.list",
			InputSchema:    gatewayObjectSchema(nil, gatewayAuditLogListProperties()),
		},
		{
			Name:           "gateway.governance.status",
			Title:          "Read Gateway Governance Status",
			Description:    "Read redacted AI Gateway governance health, policy coverage, approval, audit, token, client, and relay summaries.",
			Domain:         "gateway",
			Action:         "read",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermAIGatewayManage},
			RequiredScopes: []string{"aiClient", "policy", "tool"},
			MCPAdapterID:   "gateway-governance.v1",
			MCPToolName:    "gateway.governance.status",
			InputSchema: gatewayObjectSchema(nil, map[string]any{
				"windowHours": gatewayIntegerSchema("Governance lookback window in hours. Defaults to 24 and is capped at 168."),
			}),
		},
		{
			Name:           "gateway.relay.upstreams.list",
			Title:          "List Relay Upstreams",
			Description:    "List redacted AI Gateway relay upstream metadata for routing and health governance.",
			Domain:         "gateway",
			Action:         "list",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermAIGatewayRelayView},
			RequiredScopes: []string{"relayUpstream"},
			MCPAdapterID:   "gateway-governance.v1",
			MCPToolName:    "gateway.relay.upstreams.list",
			InputSchema:    gatewayObjectSchema(nil, gatewayRelayUpstreamListProperties()),
		},
		{
			Name:           "gateway.relay.model_routes.list",
			Title:          "List Relay Model Routes",
			Description:    "List AI Gateway relay model routes by public model, provider, upstream, and route group.",
			Domain:         "gateway",
			Action:         "list",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermAIGatewayRelayView},
			RequiredScopes: []string{"relayRoute"},
			MCPAdapterID:   "gateway-governance.v1",
			MCPToolName:    "gateway.relay.model_routes.list",
			InputSchema:    gatewayObjectSchema(nil, gatewayRelayModelRouteListProperties()),
		},
		{
			Name:           "gateway.relay.model_calls.list",
			Title:          "List Relay Model Calls",
			Description:    "List redacted AI Gateway relay model call logs by actor, client, model, upstream, status, cache status, and time window.",
			Domain:         "gateway",
			Action:         "list",
			RiskLevel:      domainaigateway.RiskLevelRead,
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermAIGatewayRelayView},
			RequiredScopes: []string{"relayCall"},
			MCPAdapterID:   "gateway-governance.v1",
			MCPToolName:    "gateway.relay.model_calls.list",
			InputSchema:    gatewayObjectSchema(nil, gatewayRelayModelCallListProperties()),
		},
		{
			Name:             "gateway.relay.cache.purge",
			Title:            "Purge Relay Cache",
			Description:      "Purge or dry-run purge AI Gateway relay response cache entries by model, upstream, route group, and age.",
			Domain:           "gateway",
			Action:           "execute",
			RiskLevel:        domainaigateway.RiskLevelExecute,
			PermissionKeys:   []string{appaccess.PermAIGatewayInvoke, appaccess.PermAIGatewayRelayManage},
			RequiredScopes:   []string{"relayCache"},
			RequiresApproval: true,
			MCPAdapterID:     "gateway-governance.v1",
			MCPToolName:      "gateway.relay.cache.purge",
			InputSchema:      gatewayObjectSchema(nil, gatewayRelayCachePurgeProperties()),
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
			ContextSchema:  gatewayObjectSchema(nil, gatewayDeliveryContextProperties()),
		},
		{
			Name:           "soha://delivery/execution-tasks",
			Description:    "Durable delivery execution tasks, callbacks, artifacts, and logs.",
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryExecutionTasksView},
			RequiredScopes: []string{"application", "environment"},
			ContextSchema:  gatewayObjectSchema(nil, gatewayDeliveryExecutionContextProperties()),
		},
		{
			Name:           "soha://k8s/runtime",
			Description:    "Scoped Kubernetes runtime inventory, events, logs, route, storage, and workload status.",
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermWorkspaceResourceView},
			RequiredScopes: []string{"cluster", "namespace"},
			ContextSchema:  gatewayObjectSchema([]string{"clusterId"}, gatewayKubernetesRuntimeContextProperties()),
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
			ArgumentSchema: gatewayObjectSchema([]string{"applicationId"}, gatewayDeliveryPromptArgumentProperties()),
			ContextSchema:  gatewayObjectSchema(nil, gatewayDeliveryContextProperties()),
		},
		{
			Name:           "soha.k8s.diagnose_workload",
			Description:    "Collect scoped workload, network route, storage, node, and event evidence without mutating cluster state.",
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermWorkspaceResourceView},
			RequiredScopes: []string{"cluster", "namespace", "workload"},
			ArgumentSchema: gatewayObjectSchema([]string{"clusterId"}, gatewayKubernetesPromptArgumentProperties()),
			ContextSchema:  gatewayObjectSchema([]string{"clusterId"}, gatewayKubernetesRuntimeContextProperties()),
		},
	}
}

func gatewayDeliveryContextProperties() map[string]any {
	return map[string]any{
		"businessLineId":            gatewayStringSchema("Optional business line id for delivery scope."),
		"applicationId":             gatewayStringSchema("Optional delivery application id."),
		"applicationEnvironmentId":  gatewayStringSchema("Optional application environment binding id."),
		"environmentId":             gatewayStringSchema("Optional delivery environment id."),
		"releaseBundleId":           gatewayStringSchema("Optional release bundle id."),
		"executionTaskId":           gatewayStringSchema("Optional execution task id."),
		"workflowRunId":             gatewayStringSchema("Optional workflow run id."),
		"gatewayApprovalRequestId":  gatewayStringSchema("Optional AI Gateway approval request id."),
		"workflowApprovalRequestId": gatewayStringSchema("Optional workflow approval request id."),
	}
}

func gatewayDeliveryExecutionContextProperties() map[string]any {
	out := gatewayDeliveryContextProperties()
	out["taskId"] = gatewayStringSchema("Alias for executionTaskId.")
	out["status"] = gatewayStringSchema("Optional execution task status.")
	return out
}

func gatewayDeliveryPromptArgumentProperties() map[string]any {
	return map[string]any{
		"applicationId":            gatewayStringSchema("Delivery application id."),
		"applicationEnvironmentId": gatewayStringSchema("Optional application environment binding id."),
		"environmentId":            gatewayStringSchema("Optional delivery environment id."),
		"releaseBundleId":          gatewayStringSchema("Optional candidate release bundle id."),
		"executionTaskId":          gatewayStringSchema("Optional execution task id for recent evidence."),
		"targetEnvironmentId":      gatewayStringSchema("Optional target environment id for promotion planning."),
	}
}

func gatewayKubernetesRuntimeContextProperties() map[string]any {
	return map[string]any{
		"clusterId":      gatewayStringSchema("Cluster id."),
		"namespace":      gatewayStringSchema("Namespace. Empty means all namespaces when allowed."),
		"workloadKind":   gatewayStringSchema("Optional workload kind, such as Deployment, StatefulSet, DaemonSet, Job, or Pod."),
		"workloadName":   gatewayStringSchema("Optional workload name."),
		"podName":        gatewayStringSchema("Optional pod name."),
		"container":      gatewayStringSchema("Optional container name."),
		"deploymentName": gatewayStringSchema("Optional deployment name."),
		"serviceName":    gatewayStringSchema("Optional service name."),
		"nodeName":       gatewayStringSchema("Optional node name."),
		"tailLines":      gatewayIntegerSchema("Optional log tail line count."),
		"eventLimit":     gatewayIntegerSchema("Optional event limit."),
	}
}

func gatewayKubernetesPromptArgumentProperties() map[string]any {
	out := gatewayKubernetesRuntimeContextProperties()
	out["symptom"] = gatewayStringSchema("Optional short problem statement or observed symptom.")
	out["sinceSeconds"] = gatewayIntegerSchema("Optional event or log lookback in seconds.")
	return out
}

func gatewayObjectSchema(required []string, properties map[string]any) map[string]any {
	out := map[string]any{
		"type":                 "object",
		"additionalProperties": true,
		"properties":           properties,
	}
	if len(required) > 0 {
		items := make([]any, 0, len(required))
		for _, item := range required {
			items = append(items, item)
		}
		out["required"] = items
	}
	return out
}

func gatewayStringSchema(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}

func gatewayIntegerSchema(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}

func gatewayBooleanSchema(description string) map[string]any {
	return map[string]any{"type": "boolean", "description": description}
}

func gatewayArraySchema(description string) map[string]any {
	return map[string]any{"type": "array", "description": description}
}

func gatewayFreeformObjectSchema(description string) map[string]any {
	return map[string]any{"type": "object", "description": description, "additionalProperties": true}
}

func gatewayApplicationIDProperties() map[string]any {
	return map[string]any{
		"applicationId": gatewayStringSchema("Delivery application id."),
	}
}

func gatewayApplicationBindingProperties() map[string]any {
	return map[string]any{
		"applicationId":            gatewayStringSchema("Delivery application id."),
		"applicationEnvironmentId": gatewayStringSchema("Optional application environment binding id."),
		"bindingId":                gatewayStringSchema("Alias for applicationEnvironmentId."),
	}
}

func gatewayReleaseBundleListProperties() map[string]any {
	return map[string]any{
		"applicationId":            gatewayStringSchema("Optional delivery application id filter."),
		"applicationEnvironmentId": gatewayStringSchema("Optional application environment binding id filter."),
		"bundleId":                 gatewayStringSchema("Release bundle id to inspect artifacts when artifacts=true."),
		"artifacts":                gatewayBooleanSchema("Return artifacts for bundleId instead of listing bundles."),
		"limit":                    gatewayIntegerSchema("Maximum release bundle records to return."),
	}
}

func gatewayExecutionTaskListProperties() map[string]any {
	return map[string]any{
		"applicationId":            gatewayStringSchema("Optional delivery application id filter."),
		"applicationEnvironmentId": gatewayStringSchema("Optional application environment binding id filter."),
		"releaseBundleId":          gatewayStringSchema("Optional release bundle id filter."),
		"status":                   gatewayStringSchema("Optional execution task status filter."),
		"providerKind":             gatewayStringSchema("Optional execution provider kind filter."),
		"taskId":                   gatewayStringSchema("Execution task id to inspect logs when logs=true."),
		"logs":                     gatewayBooleanSchema("Return logs for taskId instead of listing tasks."),
		"limit":                    gatewayIntegerSchema("Maximum execution task records to return."),
		"logLimit":                 gatewayIntegerSchema("Maximum log records to return when logs=true."),
	}
}

func gatewayAIClientListProperties() map[string]any {
	return map[string]any{
		"status": gatewayStringSchema("Optional AI client status filter."),
		"kind":   gatewayStringSchema("Optional AI client kind filter."),
	}
}

func gatewayManifestReadProperties() map[string]any {
	return map[string]any{
		"aiClientId":   gatewayStringSchema("Optional AI client id used for grant and policy filtering."),
		"aiClientName": gatewayStringSchema("Optional AI client display name for caller context."),
		"skillId":      gatewayStringSchema("Optional skill id used for skill binding filtering."),
		"tokenId":      gatewayStringSchema("Optional token id for caller context."),
		"tokenKind":    gatewayStringSchema("Optional token kind for caller context."),
		"sessionId":    gatewayStringSchema("Optional session id for caller context."),
		"subjectType":  gatewayStringSchema("Optional subject type for caller context."),
		"subjectId":    gatewayStringSchema("Optional subject id for caller context."),
		"source":       gatewayStringSchema("Optional source label for caller context."),
	}
}

func gatewayTokenListProperties() map[string]any {
	return map[string]any{
		"userId":                 gatewayStringSchema("Optional personal access token owner user id filter."),
		"includeServiceAccounts": gatewayBooleanSchema("Include service account token metadata. Defaults to true."),
	}
}

func gatewayServiceAccountListProperties() map[string]any {
	return map[string]any{
		"status": gatewayStringSchema("Optional service account status filter."),
	}
}

func gatewayToolGrantListProperties() map[string]any {
	return map[string]any{
		"subjectType":    gatewayStringSchema("Optional subject type filter."),
		"subjectId":      gatewayStringSchema("Optional subject id filter."),
		"aiClientId":     gatewayStringSchema("Optional AI client id filter."),
		"toolName":       gatewayStringSchema("Optional Gateway tool name filter."),
		"includeExpired": gatewayBooleanSchema("Include expired grants."),
	}
}

func gatewayAccessPolicyListProperties() map[string]any {
	return map[string]any{
		"subjectType":     gatewayStringSchema("Optional subject type filter."),
		"subjectId":       gatewayStringSchema("Optional subject id filter."),
		"aiClientId":      gatewayStringSchema("Optional AI client id filter."),
		"effect":          gatewayStringSchema("Optional policy effect filter, allow or deny."),
		"includeDisabled": gatewayBooleanSchema("Include disabled policies."),
	}
}

func gatewaySkillBindingListProperties() map[string]any {
	return map[string]any{
		"subjectType":     gatewayStringSchema("Optional subject type filter."),
		"subjectId":       gatewayStringSchema("Optional subject id filter."),
		"aiClientId":      gatewayStringSchema("Optional AI client id filter."),
		"skillId":         gatewayStringSchema("Optional skill id filter."),
		"includeDisabled": gatewayBooleanSchema("Include disabled bindings."),
	}
}

func gatewayApprovalListProperties() map[string]any {
	return map[string]any{
		"id":         gatewayStringSchema("Optional approval request id filter."),
		"status":     gatewayStringSchema("Optional approval request status filter."),
		"actorType":  gatewayStringSchema("Optional actor type filter."),
		"actorId":    gatewayStringSchema("Optional actor id filter."),
		"aiClientId": gatewayStringSchema("Optional AI client id filter."),
		"skillId":    gatewayStringSchema("Optional skill id filter."),
		"toolName":   gatewayStringSchema("Optional Gateway tool name filter."),
		"riskLevel":  gatewayStringSchema("Optional risk level filter."),
		"strategy":   gatewayStringSchema("Optional approval strategy filter."),
		"limit":      gatewayIntegerSchema("Maximum approval requests to return."),
	}
}

func gatewayApprovalDecideProperties() map[string]any {
	return map[string]any{
		"approvalRequestId": gatewayStringSchema("Approval request id."),
		"id":                gatewayStringSchema("Alias for approvalRequestId."),
		"decision":          gatewayStringSchema("Decision action: approve, reject, or cancel."),
		"action":            gatewayStringSchema("Alias for decision."),
		"comment":           gatewayStringSchema("Human decision comment. Sensitive text is redacted in returned traces."),
	}
}

func gatewayAuditLogListProperties() map[string]any {
	return map[string]any{
		"actorType":         gatewayStringSchema("Optional actor type filter."),
		"actorId":           gatewayStringSchema("Optional actor id filter."),
		"aiClientId":        gatewayStringSchema("Optional AI client id filter."),
		"skillId":           gatewayStringSchema("Optional skill id filter."),
		"toolName":          gatewayStringSchema("Optional Gateway tool name filter."),
		"approvalRequestId": gatewayStringSchema("Optional approval request id filter."),
		"riskLevel":         gatewayStringSchema("Optional risk level filter."),
		"result":            gatewayStringSchema("Optional audit result filter."),
		"action":            gatewayStringSchema("Optional audit action filter."),
		"limit":             gatewayIntegerSchema("Maximum audit logs to return."),
	}
}

func gatewayRelayUpstreamListProperties() map[string]any {
	return map[string]any{
		"providerKind": gatewayStringSchema("Optional relay provider kind filter."),
		"status":       gatewayStringSchema("Optional relay upstream status filter."),
		"includeAll":   gatewayBooleanSchema("Include inactive upstreams."),
	}
}

func gatewayRelayModelRouteListProperties() map[string]any {
	return map[string]any{
		"publicModel":     gatewayStringSchema("Optional public model filter."),
		"providerKind":    gatewayStringSchema("Optional provider kind filter."),
		"upstreamId":      gatewayStringSchema("Optional upstream id filter."),
		"routeGroup":      gatewayStringSchema("Optional route group filter."),
		"includeDisabled": gatewayBooleanSchema("Include disabled routes."),
	}
}

func gatewayRelayModelCallListProperties() map[string]any {
	return map[string]any{
		"actorType":    gatewayStringSchema("Optional actor type filter."),
		"actorId":      gatewayStringSchema("Optional actor id filter."),
		"tokenId":      gatewayStringSchema("Optional token id filter."),
		"tokenPrefix":  gatewayStringSchema("Optional token prefix filter."),
		"tokenKind":    gatewayStringSchema("Optional token kind filter."),
		"aiClientId":   gatewayStringSchema("Optional AI client id filter."),
		"publicModel":  gatewayStringSchema("Optional public model filter."),
		"upstreamId":   gatewayStringSchema("Optional upstream id filter."),
		"providerKind": gatewayStringSchema("Optional provider kind filter."),
		"status":       gatewayStringSchema("Optional relay call status filter."),
		"endpoint":     gatewayStringSchema("Optional relay endpoint filter."),
		"cacheStatus":  gatewayStringSchema("Optional relay cache status filter."),
		"from":         gatewayStringSchema("Optional RFC3339 lower time bound."),
		"to":           gatewayStringSchema("Optional RFC3339 upper time bound."),
		"limit":        gatewayIntegerSchema("Maximum relay call logs to return."),
	}
}

func gatewayRelayCachePurgeProperties() map[string]any {
	return map[string]any{
		"publicModel": gatewayStringSchema("Optional public model filter."),
		"upstreamId":  gatewayStringSchema("Optional upstream id filter."),
		"routeGroup":  gatewayStringSchema("Optional route group filter. Expands to matching route model/upstream pairs."),
		"olderThan":   gatewayStringSchema("Optional RFC3339 timestamp. Only cache entries updated before this time are purged."),
		"dryRun":      gatewayBooleanSchema("Count matching cache entries without deleting them."),
	}
}

func gatewayOnboardingAnalyzeRepoProperties() map[string]any {
	return map[string]any{
		"repositoryPath":  gatewayStringSchema("Repository path, such as org/service."),
		"repositoryUrl":   gatewayStringSchema("Optional repository URL without credentials."),
		"businessLineId":  gatewayStringSchema("Optional business line id for the draft."),
		"applicationName": gatewayStringSchema("Optional application display name override."),
		"applicationKey":  gatewayStringSchema("Optional stable application key override."),
		"ownerTeam":       gatewayStringSchema("Optional owning team."),
		"language":        gatewayStringSchema("Optional detected or requested language."),
		"framework":       gatewayStringSchema("Optional framework, such as React, Gin, Spring Boot, or FastAPI."),
		"entrypoint":      gatewayStringSchema("Optional process entrypoint or main module."),
		"packageManager":  gatewayStringSchema("Optional package manager or build tool."),
		"buildCommand":    gatewayStringSchema("Optional credential-free build command."),
		"startCommand":    gatewayStringSchema("Optional credential-free start command."),
		"defaultBranch":   gatewayStringSchema("Default branch for onboarding."),
		"dockerfilePath":  gatewayStringSchema("Dockerfile path."),
		"buildContextDir": gatewayStringSchema("Build context directory."),
		"serviceKey":      gatewayStringSchema("Optional service key override."),
		"serviceName":     gatewayStringSchema("Optional service display name override."),
		"environmentKey":  gatewayStringSchema("Optional environment key for a suggested binding."),
		"environmentId":   gatewayStringSchema("Optional environment id for a suggested binding."),
		"clusterId":       gatewayStringSchema("Optional cluster id for a suggested release target."),
		"namespace":       gatewayStringSchema("Optional namespace for a suggested release target."),
		"workloadName":    gatewayStringSchema("Optional workload name for a suggested release target."),
		"containerName":   gatewayStringSchema("Optional container name."),
		"port":            gatewayIntegerSchema("Primary service port."),
		"files":           gatewayArraySchema("Optional repository file list used as sanitized evidence."),
		"hints":           gatewayFreeformObjectSchema("Optional credential-free analysis hints."),
	}
}

func gatewayDockerfileGenerateProperties() map[string]any {
	return map[string]any{
		"language":        gatewayStringSchema("Application language or runtime."),
		"framework":       gatewayStringSchema("Optional framework hint."),
		"packageManager":  gatewayStringSchema("Optional package manager or build tool."),
		"buildCommand":    gatewayStringSchema("Optional build command."),
		"startCommand":    gatewayStringSchema("Optional start command."),
		"entrypoint":      gatewayStringSchema("Optional entrypoint or binary path."),
		"port":            gatewayIntegerSchema("Optional exposed port."),
		"contextDir":      gatewayStringSchema("Build context directory."),
		"dockerfilePath":  gatewayStringSchema("Output Dockerfile path."),
		"runtimeImage":    gatewayStringSchema("Optional runtime base image."),
		"builderImage":    gatewayStringSchema("Optional builder base image."),
		"nonRootUser":     gatewayStringSchema("Optional non-root user name."),
		"includeHealth":   gatewayBooleanSchema("Include a HEALTHCHECK instruction when true."),
		"healthcheckPath": gatewayStringSchema("HTTP path for generated healthcheck."),
	}
}

func gatewayDockerfileValidateProperties() map[string]any {
	return map[string]any{
		"content":         gatewayStringSchema("Dockerfile content to validate."),
		"path":            gatewayStringSchema("Optional file path for reporting."),
		"language":        gatewayStringSchema("Optional language hint."),
		"expectedPort":    gatewayIntegerSchema("Optional expected exposed port."),
		"buildContextDir": gatewayStringSchema("Optional build context directory."),
	}
}

func gatewayHelmGenerateProperties() map[string]any {
	return map[string]any{
		"applicationName": gatewayStringSchema("Optional application display name."),
		"serviceName":     gatewayStringSchema("Service name."),
		"chartName":       gatewayStringSchema("Optional Helm chart name override."),
		"imageRepository": gatewayStringSchema("Container image repository without credentials."),
		"imageTag":        gatewayStringSchema("Container image tag template."),
		"namespace":       gatewayStringSchema("Optional target namespace."),
		"port":            gatewayIntegerSchema("Container and service port."),
		"replicas":        gatewayIntegerSchema("Desired replica count."),
		"serviceAccount":  gatewayStringSchema("Optional service account name."),
		"healthcheckPath": gatewayStringSchema("HTTP probe path."),
		"resourceProfile": gatewayFreeformObjectSchema("Optional credential-free resource requests and limits."),
		"labels":          gatewayFreeformObjectSchema("Optional workload labels."),
	}
}

func gatewayKubernetesValidateProperties() map[string]any {
	return map[string]any{
		"manifests":    gatewayArraySchema("Kubernetes manifest contents to validate."),
		"content":      gatewayStringSchema("Single Kubernetes manifest content to validate."),
		"expectedKind": gatewayStringSchema("Optional expected workload kind."),
		"namespace":    gatewayStringSchema("Optional expected namespace."),
	}
}

func gatewayDeliverySpecRenderProperties() map[string]any {
	return map[string]any{
		"source":              gatewayStringSchema("Draft source, normally manual, ai, or blueprint."),
		"applicationDraft":    gatewayFreeformObjectSchema("Delivery draft application profile."),
		"services":            gatewayArraySchema("Service component drafts."),
		"buildSources":        gatewayArraySchema("Build source drafts."),
		"environmentBindings": gatewayArraySchema("Environment binding drafts."),
		"files":               gatewayArraySchema("Specification file drafts."),
		"executionHints":      gatewayFreeformObjectSchema("Credential-free workflow and approval hints."),
		"postCreateActions":   gatewayArraySchema("Suggested post-create actions."),
	}
}

func gatewayDeliveryApplicationBootstrapProperties() map[string]any {
	props := gatewayDeliverySpecRenderProperties()
	props["draftId"] = gatewayStringSchema("Existing DeliveryDraft id to hand off for confirmation.")
	props["spec"] = gatewayFreeformObjectSchema("Rendered delivery spec to convert into a DeliveryDraft payload.")
	return props
}

func gatewayDeliveryReleasePlanProperties() map[string]any {
	return map[string]any{
		"source":                   gatewayStringSchema("Plan source, normally manual or ai."),
		"applicationId":            gatewayStringSchema("Delivery application id."),
		"applicationEnvironmentId": gatewayStringSchema("Application environment binding id."),
		"environmentKey":           gatewayStringSchema("Optional environment key."),
		"action":                   gatewayStringSchema("Delivery action: build, deploy, build_deploy, workflow, verify, or rollback."),
		"targetId":                 gatewayStringSchema("Optional release target id."),
		"buildSourceId":            gatewayStringSchema("Optional build source id."),
		"releaseBundleId":          gatewayStringSchema("Release bundle id for deploy or rollback."),
		"refType":                  gatewayStringSchema("Source ref type, such as branch, tag, or commit."),
		"refName":                  gatewayStringSchema("Source branch, tag, or commit."),
		"imageTag":                 gatewayStringSchema("Optional image tag."),
		"releaseName":              gatewayStringSchema("Optional release name."),
		"containerName":            gatewayStringSchema("Optional target container."),
		"reason":                   gatewayStringSchema("Human-readable release reason."),
		"variables":                gatewayFreeformObjectSchema("Credential-free workflow variables."),
		"buildArgs":                gatewayFreeformObjectSchema("Credential-free build arguments."),
		"intent":                   gatewayStringSchema("Natural-language user intent used as planning context."),
	}
}

func gatewayClusterNamespaceProperties(namespaceDescription string) map[string]any {
	return map[string]any{
		"clusterId": gatewayStringSchema("Cluster id."),
		"namespace": gatewayStringSchema(namespaceDescription),
	}
}

func defaultSkills() []domainaigateway.SkillCapability {
	return []domainaigateway.SkillCapability{
		{
			ID:             "delivery-developer",
			Name:           "Delivery Developer",
			Category:       "delivery",
			Description:    "Application onboarding, delivery context review, and self-service build/deploy/rollback workflow for AI coding tools.",
			CapabilityRefs: []string{"delivery.applications.list", "delivery.applications.detail", "delivery.applications.create", "delivery.onboarding.analyze_repo", "delivery.standards.dockerfile.generate", "delivery.standards.dockerfile.validate", "delivery.standards.helm.generate", "delivery.standards.k8s.validate", "delivery.spec.render", "delivery.application.bootstrap", "delivery.application_environments.list", "delivery.application_services.list", "delivery.build_sources.list", "delivery.release_targets.list", "delivery.release_bundles.list", "delivery.execution_tasks.list", "delivery.execution_logs.list", "delivery.release.plan", "delivery.release_context.diff", "delivery.rollback.context", "delivery.actions.trigger"},
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryApplicationsView},
			RequiredScopes: []string{"businessLine", "application", "environment"},
		},
		{
			ID:             "delivery-tester",
			Name:           "Delivery Tester",
			Category:       "delivery",
			Description:    "Test environment release verification, execution task review, and promotion evidence collection.",
			CapabilityRefs: []string{"delivery.application_environments.list", "delivery.release_targets.list", "delivery.release_bundles.list", "delivery.execution_tasks.list", "delivery.execution_logs.list", "delivery.release.plan", "delivery.release_context.diff", "diagnosis.release_failure.analyze"},
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermDeliveryExecutionTasksView},
			RequiredScopes: []string{"application", "environment"},
		},
		{
			ID:             "k8s-sre",
			Name:           "K8s SRE",
			Category:       "platform",
			Description:    "Read-only Kubernetes runtime diagnosis for pods, services, events, and deployment status.",
			CapabilityRefs: []string{"k8s.pods.list", "k8s.pods.logs", "k8s.pods.describe", "k8s.deployments.list", "k8s.deployments.rollout_status", "k8s.deployments.events", "k8s.services.list", "k8s.services.backends", "k8s.routes.context", "k8s.storage.context", "k8s.nodes.detail", "k8s.events.list"},
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke, appaccess.PermWorkspaceResourceView},
			RequiredScopes: []string{"cluster", "namespace"},
		},
		{
			ID:             "security-change",
			Name:           "Security Change",
			Category:       "security",
			Description:    "Security-sensitive change planning, approval handoff, rollback criteria, and evidence collection.",
			CapabilityRefs: []string{"delivery.actions.trigger", "delivery.workflow_templates.list", "delivery.rollback.context", "delivery.execution_tasks.list", "k8s.events.list"},
			PermissionKeys: []string{appaccess.PermAIGatewayInvoke},
			RequiredScopes: []string{"application", "environment", "cluster", "namespace"},
		},
	}
}
