package routes

import (
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	docsembed "github.com/kubecrux/kubecrux/docs"
	apiHandlers "github.com/kubecrux/kubecrux/internal/api/handlers"
	apiMiddleware "github.com/kubecrux/kubecrux/internal/api/middleware"
	cfgpkg "github.com/kubecrux/kubecrux/internal/infrastructure/config"
	swaggerinfra "github.com/kubecrux/kubecrux/internal/infrastructure/swagger"
	webembed "github.com/kubecrux/kubecrux/web"
	"go.uber.org/zap"
)

type Dependencies struct {
	System         *apiHandlers.SystemHandler
	Platform       *apiHandlers.PlatformHandler
	Announcements  *apiHandlers.AnnouncementHandler
	Module         *apiHandlers.ModuleHandler
	Monitoring     *apiHandlers.MonitoringHandler
	Catalog        *apiHandlers.CatalogHandler
	Delivery       *apiHandlers.DeliveryHandler
	Applications   *apiHandlers.ApplicationHandler
	Builds         *apiHandlers.BuildHandler
	Workflows      *apiHandlers.WorkflowHandler
	Registries     *apiHandlers.RegistryHandler
	Releases       *apiHandlers.ReleaseHandler
	Copilot        *apiHandlers.CopilotHandler
	Virtualization *apiHandlers.VirtualizationHandler
	Docker         *apiHandlers.DockerHandler
	Access         *apiHandlers.AccessHandler
	ScopeGrants    *apiHandlers.ScopeGrantHandler
	Menu           *apiHandlers.MenuHandler
	Settings       *apiHandlers.SettingsHandler
	Auth           *apiHandlers.AuthHandler
	Authn          apiMiddleware.AccessTokenParser
}

func New(cfg cfgpkg.Config, logger *zap.Logger, deps Dependencies) *http.Server {
	if cfg.App.Env == "production" {
		gin.SetMode(gin.ReleaseMode)
	}

	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(apiMiddleware.RequestID())
	router.Use(apiMiddleware.CORS(cfg.HTTP.CORSAllowedOrigins))
	router.Use(apiMiddleware.BuildPrincipalMiddleware(cfg.Auth, deps.Authn))

	router.GET("/healthz", deps.System.Healthz)
	router.GET("/readyz", deps.System.Readyz)
	swaggerinfra.Register(router, cfg.Swagger.Enabled, cfg.Swagger.Path)
	apiCompat := router.Group("/api")
	apiCompat.Use(apiMiddleware.RequireAuth())
	{
		apiCompat.GET("/currentUser", deps.Auth.ProCurrentUser)
		apiCompat.GET("/currentUserDetail", deps.Auth.ProCurrentUser)
		apiCompat.GET("/accountSettingCurrentUser", deps.Auth.ProCurrentUser)
		apiCompat.POST("/login/outLogin", deps.Auth.ProLogout)
	}
	router.POST("/api/login/account", deps.Auth.ProLogin)

	v1 := router.Group(cfg.HTTP.BasePath)
	{
		v1.GET("/healthz", deps.System.Healthz)
		v1.GET("/readyz", deps.System.Readyz)
		v1.GET("/auth/providers", deps.Auth.ListProviders)
		v1.POST("/auth/login", deps.Auth.Login)
		v1.POST("/auth/refresh", deps.Auth.Refresh)
		v1.GET("/auth/oidc/login", deps.Auth.OIDCLogin)
		v1.GET("/auth/oidc/callback", deps.Auth.OIDCCallback)
		v1.GET("/auth/login/:providerID/start", deps.Auth.ProviderLogin)
		v1.GET("/auth/login/:providerID/callback", deps.Auth.ProviderCallback)
		v1.POST("/auth/oidc/exchange", deps.Auth.OIDCExchange)
		if cfg.Modules.Monitoring.Enabled {
			v1.POST("/integrations/alerts/webhook", deps.Monitoring.IngestWebhook)
		}
		if cfg.Modules.Delivery.Enabled {
			v1.GET("/delivery/execution-tasks/:taskID/runner-status", deps.Delivery.GetExecutionTaskRunnerStatus)
			v1.POST("/delivery/execution-callbacks", deps.Delivery.RecordExecutionCallback)
			v1.POST("/delivery/execution-tasks/claim", deps.Delivery.ClaimExecutionTask)
		}
		if cfg.Modules.Docker.Enabled {
			v1.POST("/docker/operations/claim", deps.Docker.ClaimOperation)
			v1.GET("/docker/operations/:id/runner-status", deps.Docker.GetOperationRunnerStatus)
			v1.POST("/docker/operation-callbacks", deps.Docker.RecordOperationCallback)
		}
		if cfg.Modules.AI.Enabled {
			v1.POST("/copilot/agent-runs/claim", deps.Copilot.ClaimAgentRun)
			v1.POST("/copilot/agent-runs/callback", deps.Copilot.RecordAgentRunCallback)
			v1.POST("/copilot/agent-runs/tool-call", deps.Copilot.RecordAgentToolCall)
		}
	}

	protected := router.Group(cfg.HTTP.BasePath)
	protected.Use(apiMiddleware.RequireAuth())
	{
		protected.GET("/auth/me", deps.Auth.Me)
		protected.GET("/auth/bootstrap", deps.Auth.Bootstrap)
		protected.POST("/auth/logout", deps.Auth.Logout)
		protected.GET("/auth/sessions", deps.Auth.ListSessions)
		protected.POST("/auth/sessions/:sessionID/revoke", deps.Auth.RevokeSession)
		protected.GET("/system/runtime-metrics", deps.System.RuntimeMetrics)
		protected.GET("/modules", deps.Module.List)
		protected.GET("/clusters", deps.Platform.ListClusters)
		protected.GET("/menus", deps.Menu.ListAll)
		protected.GET("/menus/:menuID", deps.Menu.Get)
		protected.GET("/menus/visible", deps.Menu.ListVisible)
		protected.POST("/menus", deps.Menu.Create)
		protected.PUT("/menus/:menuID", deps.Menu.Update)
		protected.DELETE("/menus/:menuID", deps.Menu.Delete)
		protected.GET("/announcements", deps.Announcements.List)
		protected.GET("/announcements/inbox", deps.Announcements.Inbox)
		protected.GET("/announcements/:announcementID", deps.Announcements.Get)
		protected.POST("/announcements/:announcementID/read", deps.Announcements.MarkRead)
		protected.POST("/announcements/:announcementID/publish", deps.Announcements.Publish)
		protected.POST("/announcements/:announcementID/withdraw", deps.Announcements.Withdraw)
		protected.POST("/announcements", deps.Announcements.Create)
		protected.PUT("/announcements/:announcementID", deps.Announcements.Update)
		protected.DELETE("/announcements/:announcementID", deps.Announcements.Delete)
		protected.POST("/clusters", deps.Platform.CreateCluster)
		protected.PUT("/clusters/:clusterID", deps.Platform.UpdateCluster)
		protected.DELETE("/clusters/:clusterID", deps.Platform.DeleteCluster)
		protected.GET("/clusters/:clusterID/detail", deps.Platform.DescribeCluster)
		protected.GET("/clusters/:clusterID/namespaces", deps.Platform.ListNamespaces)
		protected.POST("/clusters/:clusterID/namespaces", deps.Platform.CreateNamespace)
		protected.PUT("/clusters/:clusterID/namespaces/:namespaceName", deps.Platform.UpdateNamespace)
		protected.DELETE("/clusters/:clusterID/namespaces/:namespaceName", deps.Platform.DeleteNamespace)
		protected.GET("/clusters/:clusterID/infrastructure/nodes", deps.Platform.ListNodes)
		protected.GET("/clusters/:clusterID/infrastructure/nodes/:nodeName/detail", deps.Platform.GetNodeDetail)
		protected.GET("/clusters/:clusterID/infrastructure/nodes/:nodeName/yaml", deps.Platform.GetNodeYAML)
		protected.PUT("/clusters/:clusterID/infrastructure/nodes/:nodeName/yaml", deps.Platform.ApplyNodeYAML)
		protected.PUT("/clusters/:clusterID/infrastructure/nodes/:nodeName", deps.Platform.UpdateNode)
		protected.DELETE("/clusters/:clusterID/infrastructure/nodes/:nodeName", deps.Platform.DeleteNode)
		protected.GET("/clusters/:clusterID/workloads/overview", deps.Platform.GetWorkloadOverview)
		protected.GET("/clusters/:clusterID/workloads/pods", deps.Platform.ListPods)
		protected.GET("/clusters/:clusterID/workloads/pods/:podName/detail", deps.Platform.GetPodDetail)
		protected.DELETE("/clusters/:clusterID/workloads/pods/:podName", deps.Platform.DeletePod)
		protected.GET("/clusters/:clusterID/workloads/pods/:podName/logs", deps.Platform.GetPodLogs)
		protected.GET("/clusters/:clusterID/workloads/pods/:podName/logs/stream", deps.Platform.StreamPodLogs)
		protected.GET("/clusters/:clusterID/workloads/pods/:podName/yaml", deps.Platform.GetPodYAML)
		protected.PUT("/clusters/:clusterID/workloads/pods/:podName/yaml", deps.Platform.ApplyPodYAML)
		protected.GET("/clusters/:clusterID/workloads/pods/:podName/metrics", deps.Platform.GetPodMetrics)
		protected.POST("/clusters/:clusterID/workloads/pods/:podName/exec", deps.Platform.ExecPod)
		protected.GET("/clusters/:clusterID/workloads/pods/:podName/terminal", deps.Platform.StreamPodTerminal)
		protected.GET("/clusters/:clusterID/workloads/deployments", deps.Platform.ListDeployments)
		protected.GET("/clusters/:clusterID/workloads/deployments/:deploymentName/detail", deps.Platform.GetDeploymentDetail)
		protected.GET("/clusters/:clusterID/workloads/deployments/:deploymentName/yaml", deps.Platform.GetDeploymentYAML)
		protected.PUT("/clusters/:clusterID/workloads/deployments/:deploymentName/yaml", deps.Platform.ApplyDeploymentYAML)
		protected.GET("/clusters/:clusterID/workloads/deployments/:deploymentName/metrics", deps.Platform.GetDeploymentMetrics)
		protected.GET("/clusters/:clusterID/workloads/deployments/:deploymentName/rollout-status", deps.Platform.GetDeploymentRolloutStatus)
		protected.GET("/clusters/:clusterID/workloads/deployments/:deploymentName/rollouts", deps.Platform.ListDeploymentRollouts)
		protected.GET("/clusters/:clusterID/workloads/statefulsets", deps.Platform.ListStatefulSets)
		protected.GET("/clusters/:clusterID/workloads/statefulsets/:statefulSetName/detail", deps.Platform.GetStatefulSetDetail)
		protected.GET("/clusters/:clusterID/workloads/statefulsets/:statefulSetName/yaml", deps.Platform.GetStatefulSetYAML)
		protected.PUT("/clusters/:clusterID/workloads/statefulsets/:statefulSetName/yaml", deps.Platform.ApplyStatefulSetYAML)
		protected.GET("/clusters/:clusterID/workloads/daemonsets", deps.Platform.ListDaemonSets)
		protected.GET("/clusters/:clusterID/workloads/daemonsets/:daemonSetName/detail", deps.Platform.GetDaemonSetDetail)
		protected.GET("/clusters/:clusterID/workloads/daemonsets/:daemonSetName/yaml", deps.Platform.GetDaemonSetYAML)
		protected.PUT("/clusters/:clusterID/workloads/daemonsets/:daemonSetName/yaml", deps.Platform.ApplyDaemonSetYAML)
		protected.GET("/clusters/:clusterID/workloads/jobs", deps.Platform.ListJobs)
		protected.GET("/clusters/:clusterID/workloads/jobs/:jobName/detail", deps.Platform.GetJobDetail)
		protected.GET("/clusters/:clusterID/workloads/jobs/:jobName/yaml", deps.Platform.GetJobYAML)
		protected.PUT("/clusters/:clusterID/workloads/jobs/:jobName/yaml", deps.Platform.ApplyJobYAML)
		protected.GET("/clusters/:clusterID/workloads/cronjobs", deps.Platform.ListCronJobs)
		protected.GET("/clusters/:clusterID/workloads/replicasets", deps.Platform.ListReplicaSets)
		protected.GET("/clusters/:clusterID/workloads/cronjobs/:cronJobName/detail", deps.Platform.GetCronJobDetail)
		protected.GET("/clusters/:clusterID/workloads/cronjobs/:cronJobName/yaml", deps.Platform.GetCronJobYAML)
		protected.PUT("/clusters/:clusterID/workloads/cronjobs/:cronJobName/yaml", deps.Platform.ApplyCronJobYAML)
		protected.GET("/clusters/:clusterID/configuration/configmaps", deps.Platform.ListConfigMaps)
		protected.POST("/clusters/:clusterID/configuration/configmaps", deps.Platform.CreateConfigMap)
		protected.GET("/clusters/:clusterID/configuration/configmaps/:name/detail", deps.Platform.GetConfigMapDetail)
		protected.GET("/clusters/:clusterID/configuration/secrets", deps.Platform.ListSecrets)
		protected.POST("/clusters/:clusterID/configuration/secrets", deps.Platform.CreateSecret)
		protected.GET("/clusters/:clusterID/configuration/secrets/:name/detail", deps.Platform.GetSecretDetail)
		protected.GET("/clusters/:clusterID/configuration/hpas", deps.Platform.ListHorizontalPodAutoscalers)
		protected.GET("/clusters/:clusterID/configuration/poddisruptionbudgets", deps.Platform.ListPodDisruptionBudgets)
		protected.GET("/clusters/:clusterID/access-control/serviceaccounts", deps.Platform.ListServiceAccounts)
		protected.POST("/clusters/:clusterID/access-control/serviceaccounts", deps.Platform.CreateServiceAccount)
		protected.GET("/clusters/:clusterID/access-control/serviceaccounts/:name/detail", deps.Platform.GetServiceAccountDetail)
		protected.GET("/clusters/:clusterID/access-control/roles", deps.Platform.ListRoles)
		protected.POST("/clusters/:clusterID/access-control/roles", deps.Platform.CreateRole)
		protected.GET("/clusters/:clusterID/access-control/roles/:name/detail", deps.Platform.GetRoleDetail)
		protected.GET("/clusters/:clusterID/access-control/rolebindings", deps.Platform.ListRoleBindings)
		protected.POST("/clusters/:clusterID/access-control/rolebindings", deps.Platform.CreateRoleBinding)
		protected.GET("/clusters/:clusterID/access-control/rolebindings/:name/detail", deps.Platform.GetRoleBindingDetail)
		protected.GET("/clusters/:clusterID/network/services", deps.Platform.ListServices)
		protected.GET("/clusters/:clusterID/network/services/:serviceName/metrics", deps.Platform.GetServiceMetrics)
		protected.GET("/clusters/:clusterID/network/ingresses", deps.Platform.ListIngresses)
		protected.GET("/clusters/:clusterID/network/endpointslices", deps.Platform.ListEndpointSlices)
		protected.GET("/clusters/:clusterID/network/networkpolicies", deps.Platform.ListNetworkPolicies)
		protected.GET("/clusters/:clusterID/network/gateways", deps.Platform.ListGateways)
		protected.GET("/clusters/:clusterID/network/httproutes", deps.Platform.ListHTTPRoutes)
		protected.GET("/clusters/:clusterID/storage/persistentvolumeclaims", deps.Platform.ListPersistentVolumeClaims)
		protected.POST("/clusters/:clusterID/storage/persistentvolumeclaims", deps.Platform.CreatePersistentVolumeClaim)
		protected.GET("/clusters/:clusterID/storage/persistentvolumeclaims/:name/detail", deps.Platform.GetPersistentVolumeClaimDetail)
		protected.GET("/clusters/:clusterID/storage/persistentvolumes", deps.Platform.ListPersistentVolumes)
		protected.POST("/clusters/:clusterID/storage/persistentvolumes", deps.Platform.CreatePersistentVolume)
		protected.GET("/clusters/:clusterID/storage/persistentvolumes/:name/detail", deps.Platform.GetPersistentVolumeDetail)
		protected.GET("/clusters/:clusterID/storage/storageclasses", deps.Platform.ListStorageClasses)
		protected.POST("/clusters/:clusterID/storage/storageclasses", deps.Platform.CreateStorageClass)
		protected.GET("/clusters/:clusterID/storage/storageclasses/:name/detail", deps.Platform.GetStorageClassDetail)
		protected.GET("/clusters/:clusterID/network/ingressclasses", deps.Platform.ListIngressClasses)
		protected.GET("/clusters/:clusterID/configuration/priorityclasses", deps.Platform.ListPriorityClasses)
		protected.GET("/clusters/:clusterID/configuration/runtimeclasses", deps.Platform.ListRuntimeClasses)
		protected.GET("/clusters/:clusterID/access-control/clusterroles", deps.Platform.ListClusterRoles)
		protected.POST("/clusters/:clusterID/access-control/clusterroles", deps.Platform.CreateClusterRole)
		protected.GET("/clusters/:clusterID/access-control/clusterroles/:name/detail", deps.Platform.GetClusterRoleDetail)
		protected.GET("/clusters/:clusterID/access-control/clusterrolebindings", deps.Platform.ListClusterRoleBindings)
		protected.POST("/clusters/:clusterID/access-control/clusterrolebindings", deps.Platform.CreateClusterRoleBinding)
		protected.GET("/clusters/:clusterID/access-control/clusterrolebindings/:name/detail", deps.Platform.GetClusterRoleBindingDetail)
		protected.GET("/clusters/:clusterID/configuration/mutatingwebhookconfigurations", deps.Platform.ListMutatingWebhookConfigurations)
		protected.GET("/clusters/:clusterID/configuration/validatingwebhookconfigurations", deps.Platform.ListValidatingWebhookConfigurations)
		protected.GET("/clusters/:clusterID/configuration/resourcequotas", deps.Platform.ListResourceQuotas)
		protected.GET("/clusters/:clusterID/configuration/limitranges", deps.Platform.ListLimitRanges)
		protected.GET("/clusters/:clusterID/configuration/leases", deps.Platform.ListLeases)
		protected.GET("/clusters/:clusterID/workloads/replicationcontrollers", deps.Platform.ListReplicationControllers)
		protected.GET("/clusters/:clusterID/network/port-forwards", deps.Platform.ListPortForwards)
		protected.POST("/clusters/:clusterID/network/port-forwards", deps.Platform.RegisterPortForward)
		protected.DELETE("/clusters/:clusterID/network/port-forwards/:sessionID", deps.Platform.StopPortForward)
		deps.Platform.RegisterGenericResourceRoutes(protected)
		deps.Platform.RegisterWorkloadDeleteRoutes(protected)
		protected.GET("/clusters/:clusterID/extensions/crds", deps.Platform.ListCRDs)
		protected.GET("/clusters/:clusterID/extensions/crds/:crdName/resources", deps.Platform.ListCRDResources)
		protected.POST("/clusters/:clusterID/extensions/crds/:crdName/resources", deps.Platform.CreateCRDResource)
		protected.GET("/clusters/:clusterID/extensions/crds/:crdName/resources/:name/yaml", deps.Platform.GetCRDResourceYAML)
		protected.PUT("/clusters/:clusterID/extensions/crds/:crdName/resources/:name/yaml", deps.Platform.ApplyCRDResourceYAML)
		protected.DELETE("/clusters/:clusterID/extensions/crds/:crdName/resources/:name", deps.Platform.DeleteCRDResource)
		protected.GET("/clusters/:clusterID/helm/releases", deps.Platform.ListHelmReleases)
		protected.GET("/clusters/:clusterID/helm/releases/:releaseName/detail", deps.Platform.GetHelmReleaseDetail)
		protected.GET("/clusters/:clusterID/helm/releases/:releaseName/history", deps.Platform.ListHelmReleaseHistory)
		protected.GET("/clusters/:clusterID/helm/releases/:releaseName/values", deps.Platform.GetHelmReleaseValues)
		protected.GET("/clusters/:clusterID/events", deps.Platform.ListClusterEvents)
		protected.POST("/clusters/:clusterID/workloads/deployments/restart", deps.Platform.RestartDeployment)
		protected.POST("/clusters/:clusterID/workloads/deployments/rollback", deps.Platform.RollbackDeployment)
		protected.POST("/clusters/:clusterID/workloads/deployments/scale", deps.Platform.ScaleDeployment)
		if cfg.Modules.Monitoring.Enabled {
			protected.GET("/monitoring/summary", deps.Monitoring.Summary)
			protected.GET("/alerts", deps.Monitoring.ListAlerts)
			protected.GET("/alerts/:alertID", deps.Monitoring.GetAlert)
			protected.PUT("/alerts/:alertID/ownership", deps.Monitoring.UpdateAlertOwnership)
			protected.POST("/alerts/:alertID/acknowledge", deps.Monitoring.AcknowledgeAlert)
			protected.GET("/alert-rules", deps.Monitoring.ListRules)
			protected.POST("/alert-rules", deps.Monitoring.CreateRule)
			protected.PUT("/alert-rules/:ruleID", deps.Monitoring.UpdateRule)
			protected.POST("/alert-rules/:ruleID/validate", deps.Monitoring.TestRule)
			protected.POST("/alert-rules/:ruleID/test", deps.Monitoring.TestRule)
			protected.GET("/alert-rule-runs", deps.Monitoring.ListRuleRuns)
			protected.GET("/alert-events", deps.Monitoring.ListEvents)
			protected.GET("/alert-events/:eventID", deps.Monitoring.GetEvent)
			protected.POST("/alert-events/:eventID/acknowledge", deps.Monitoring.AcknowledgeEvent)
			protected.POST("/alert-events/:eventID/resolve", deps.Monitoring.ResolveEvent)
			protected.POST("/alert-events/:eventID/heal", deps.Monitoring.HealEvent)
			protected.POST("/healing-runs/:runID/approve", deps.Monitoring.ApproveHealingRun)
			protected.POST("/healing-runs/:runID/reject", deps.Monitoring.RejectHealingRun)
			protected.POST("/healing-runs/:runID/retry", deps.Monitoring.RetryHealingRun)
			protected.GET("/notification-policies", deps.Monitoring.ListNotificationPolicies)
			protected.POST("/notification-policies", deps.Monitoring.CreateNotificationPolicy)
			protected.PUT("/notification-policies/:policyID", deps.Monitoring.UpdateNotificationPolicy)
			protected.GET("/notification-policies/:policyID/preview", deps.Monitoring.PreviewNotificationPolicy)
			protected.GET("/notification-templates", deps.Monitoring.ListNotificationTemplates)
			protected.POST("/notification-templates", deps.Monitoring.CreateNotificationTemplate)
			protected.PUT("/notification-templates/:templateID", deps.Monitoring.UpdateNotificationTemplate)
			protected.GET("/healing-policies", deps.Monitoring.ListHealingPolicies)
			protected.POST("/healing-policies", deps.Monitoring.CreateHealingPolicy)
			protected.PUT("/healing-policies/:policyID", deps.Monitoring.UpdateHealingPolicy)
			protected.GET("/healing-runs", deps.Monitoring.ListHealingRuns)
			protected.GET("/healing-runs/:runID", deps.Monitoring.GetHealingRun)
			protected.GET("/oncall/schedules", deps.Monitoring.ListOnCallSchedules)
			protected.POST("/oncall/schedules", deps.Monitoring.CreateOnCallSchedule)
			protected.PUT("/oncall/schedules/:scheduleID", deps.Monitoring.UpdateOnCallSchedule)
			protected.GET("/oncall/rotations", deps.Monitoring.ListOnCallRotations)
			protected.POST("/oncall/rotations", deps.Monitoring.CreateOnCallRotation)
			protected.PUT("/oncall/rotations/:rotationID", deps.Monitoring.UpdateOnCallRotation)
			protected.GET("/oncall/escalation-policies", deps.Monitoring.ListOnCallEscalationPolicies)
			protected.POST("/oncall/escalation-policies", deps.Monitoring.CreateOnCallEscalationPolicy)
			protected.PUT("/oncall/escalation-policies/:policyID", deps.Monitoring.UpdateOnCallEscalationPolicy)
			protected.GET("/oncall/assignment-rules", deps.Monitoring.ListOnCallAssignmentRules)
			protected.POST("/oncall/assignment-rules", deps.Monitoring.CreateOnCallAssignmentRule)
			protected.PUT("/oncall/assignment-rules/:ruleID", deps.Monitoring.UpdateOnCallAssignmentRule)
			protected.GET("/oncall/routes", deps.Monitoring.ListOnCallAssignmentRules)
			protected.POST("/oncall/routes", deps.Monitoring.CreateOnCallAssignmentRule)
			protected.PUT("/oncall/routes/:routeID", deps.Monitoring.UpdateOnCallAssignmentRule)
			protected.GET("/oncall/current", deps.Monitoring.GetCurrentOnCall)
			protected.GET("/oncall/resolve", deps.Monitoring.ResolveOnCall)
			protected.GET("/oncall/tasks", deps.Monitoring.ListOnCallTasks)
			protected.GET("/alert-delivery-logs", deps.Monitoring.ListDeliveryLogs)
			protected.GET("/alert-silences", deps.Monitoring.ListSilences)
			protected.POST("/alert-silences", deps.Monitoring.CreateSilence)
			protected.PUT("/alert-silences/:silenceID", deps.Monitoring.UpdateSilence)
			protected.GET("/notification-channels", deps.Monitoring.ListChannels)
			protected.POST("/notification-channels", deps.Monitoring.CreateChannel)
			protected.PUT("/notification-channels/:channelID", deps.Monitoring.UpdateChannel)
			protected.GET("/alert-routes", deps.Monitoring.ListRoutes)
			protected.POST("/alert-routes", deps.Monitoring.CreateRoute)
			protected.PUT("/alert-routes/:routeID", deps.Monitoring.UpdateRoute)
		}
		if cfg.Modules.Delivery.Enabled {
			protected.GET("/business-lines", deps.Catalog.ListBusinessLines)
			protected.POST("/business-lines", deps.Catalog.CreateBusinessLine)
			protected.PUT("/business-lines/:businessLineID", deps.Catalog.UpdateBusinessLine)
			protected.DELETE("/business-lines/:businessLineID", deps.Catalog.DeleteBusinessLine)
			protected.GET("/delivery-environments", deps.Catalog.ListEnvironments)
			protected.POST("/delivery-environments", deps.Catalog.CreateEnvironment)
			protected.PUT("/delivery-environments/:environmentID", deps.Catalog.UpdateEnvironment)
			protected.DELETE("/delivery-environments/:environmentID", deps.Catalog.DeleteEnvironment)
			protected.GET("/application-environments", deps.Catalog.ListApplicationEnvironments)
			protected.GET("/application-environments/:applicationEnvironmentID", deps.Catalog.GetApplicationEnvironment)
			protected.GET("/application-environments/:applicationEnvironmentID/detail", deps.Delivery.GetApplicationEnvironmentDetail)
			protected.GET("/application-environments/target-candidates", deps.Delivery.ListTargetCandidates)
			protected.GET("/delivery/release-bundles", deps.Delivery.ListReleaseBundles)
			protected.GET("/delivery/release-bundles/:bundleID", deps.Delivery.GetReleaseBundle)
			protected.GET("/delivery/release-bundles/:bundleID/artifacts", deps.Delivery.ListReleaseBundleArtifacts)
			protected.GET("/delivery/execution-tasks", deps.Delivery.ListExecutionTasks)
			protected.GET("/delivery/execution-tasks/:taskID", deps.Delivery.GetExecutionTask)
			protected.GET("/delivery/execution-tasks/:taskID/logs", deps.Delivery.ListExecutionLogs)
			protected.GET("/delivery/execution-tasks/:taskID/artifacts", deps.Delivery.ListExecutionArtifacts)
			protected.POST("/delivery/execution-tasks/:taskID/cancel", deps.Delivery.CancelExecutionTask)
			protected.POST("/delivery/execution-tasks/:taskID/retry", deps.Delivery.RetryExecutionTask)
			protected.GET("/delivery/approval-policies", deps.Delivery.ListApprovalPolicies)
			protected.POST("/delivery/approval-policies", deps.Delivery.CreateApprovalPolicy)
			protected.PUT("/delivery/approval-policies/:approvalPolicyID", deps.Delivery.UpdateApprovalPolicy)
			protected.DELETE("/delivery/approval-policies/:approvalPolicyID", deps.Delivery.DeleteApprovalPolicy)
			protected.GET("/delivery/blueprints", deps.Delivery.ListDeliveryBlueprints)
			protected.POST("/delivery/blueprints", deps.Delivery.CreateDeliveryBlueprint)
			protected.PUT("/delivery/blueprints/:blueprintID", deps.Delivery.UpdateDeliveryBlueprint)
			protected.POST("/delivery/blueprints/:blueprintID/render-spec", deps.Delivery.RenderDeliveryBlueprintSpec)
			protected.POST("/delivery/blueprints/:blueprintID/bootstrap-application", deps.Delivery.BootstrapApplicationFromBlueprint)
			protected.POST("/application-environments", deps.Catalog.CreateApplicationEnvironment)
			protected.PUT("/application-environments/:applicationEnvironmentID", deps.Catalog.UpdateApplicationEnvironment)
			protected.DELETE("/application-environments/:applicationEnvironmentID", deps.Catalog.DeleteApplicationEnvironment)
			protected.GET("/build-templates", deps.Catalog.ListBuildTemplates)
			protected.POST("/build-templates", deps.Catalog.CreateBuildTemplate)
			protected.PUT("/build-templates/:buildTemplateID", deps.Catalog.UpdateBuildTemplate)
			protected.DELETE("/build-templates/:buildTemplateID", deps.Catalog.DeleteBuildTemplate)
			protected.GET("/workflow-templates", deps.Catalog.ListWorkflowTemplates)
			protected.POST("/workflow-templates", deps.Catalog.CreateWorkflowTemplate)
			protected.PUT("/workflow-templates/:workflowTemplateID", deps.Catalog.UpdateWorkflowTemplate)
			protected.DELETE("/workflow-templates/:workflowTemplateID", deps.Catalog.DeleteWorkflowTemplate)
			protected.GET("/applications", deps.Applications.ListApplications)
			protected.POST("/applications", deps.Applications.CreateApplication)
			protected.GET("/applications/:applicationID", deps.Applications.GetApplication)
			protected.GET("/applications/:applicationID/detail", deps.Delivery.GetApplicationDetail)
			protected.GET("/applications/:applicationID/runtime", deps.Delivery.GetApplicationRuntimeDetail)
			protected.POST("/applications/:applicationID/delivery-actions", deps.Delivery.TriggerApplicationDeliveryAction)
			protected.GET("/applications/:applicationID/application-environments/:applicationEnvironmentID/workloads/:workloadName/runtime", deps.Delivery.GetApplicationWorkloadRuntimeDetail)
			protected.GET("/applications/:applicationID/services", deps.Applications.ListApplicationServices)
			protected.POST("/applications/:applicationID/services", deps.Applications.CreateApplicationService)
			protected.GET("/applications/:applicationID/services/:serviceID", deps.Applications.GetApplicationService)
			protected.PUT("/applications/:applicationID/services/:serviceID", deps.Applications.UpdateApplicationService)
			protected.DELETE("/applications/:applicationID/services/:serviceID", deps.Applications.DeleteApplicationService)
			protected.PUT("/applications/:applicationID", deps.Applications.UpdateApplication)
			protected.DELETE("/applications/:applicationID", deps.Applications.DeleteApplication)
			protected.GET("/builds", deps.Builds.ListBuilds)
			protected.POST("/builds/trigger", deps.Builds.TriggerBuild)
			protected.GET("/workflows", deps.Workflows.List)
			protected.POST("/workflows/trigger", deps.Workflows.Trigger)
			protected.POST("/workflows/:workflowRunID/approve", deps.Workflows.Approve)
			protected.POST("/workflows/:workflowRunID/reject", deps.Workflows.Reject)
			protected.GET("/registries", deps.Registries.List)
			protected.POST("/registries", deps.Registries.Create)
			protected.PUT("/registries/:connectionID", deps.Registries.Update)
			protected.DELETE("/registries/:connectionID", deps.Registries.Delete)
			protected.GET("/releases", deps.Releases.ListReleases)
			protected.POST("/releases/trigger", deps.Releases.TriggerRelease)
			protected.GET("/delivery/release-board", deps.Delivery.ListReleaseBoard)
			protected.GET("/integrations/gitlab/projects", deps.Applications.ListGitRepositories)
			protected.GET("/integrations/gitlab/branches", deps.Applications.ListGitBranches)
			protected.GET("/integrations/gitlab/tags", deps.Applications.ListGitTags)
		}
		if cfg.Modules.Virtualization.Enabled {
			protected.GET("/virtualization/overview", deps.Virtualization.Overview)
			protected.GET("/virtualization/clusters", deps.Virtualization.ListConnections)
			protected.POST("/virtualization/clusters", deps.Virtualization.CreateConnection)
			protected.PUT("/virtualization/clusters/:id", deps.Virtualization.UpdateConnection)
			protected.DELETE("/virtualization/clusters/:id", deps.Virtualization.DeleteConnection)
			protected.POST("/virtualization/clusters/:id/test", deps.Virtualization.TestConnection)
			protected.POST("/virtualization/clusters/:id/sync", deps.Virtualization.SyncConnection)
			protected.GET("/virtualization/vms", deps.Virtualization.ListVMs)
			protected.POST("/virtualization/vms", deps.Virtualization.CreateVM)
			protected.GET("/virtualization/vms/:id/detail", deps.Virtualization.GetVMDetail)
			protected.GET("/virtualization/vms/:id", deps.Virtualization.GetVM)
			protected.POST("/virtualization/vms/:id/actions", deps.Virtualization.VMAction)
			protected.POST("/virtualization/vms/:id/power", deps.Virtualization.VMAction)
			protected.GET("/virtualization/vms/:id/metrics", deps.Virtualization.GetVMMetrics)
			protected.GET("/virtualization/vms/:id/console", deps.Virtualization.GetConsoleURL)
			protected.GET("/virtualization/vms/:id/console/vnc", deps.Virtualization.StreamVMConsole)
			protected.GET("/virtualization/vms/:id/console/novnc", deps.Virtualization.StreamVMConsole)
			protected.GET("/virtualization/images", deps.Virtualization.ListImages)
			protected.POST("/virtualization/images", deps.Virtualization.CreateImage)
			protected.PUT("/virtualization/images/:id", deps.Virtualization.UpdateImage)
			protected.DELETE("/virtualization/images/:id", deps.Virtualization.DeleteImage)
			protected.GET("/virtualization/flavors", deps.Virtualization.ListFlavors)
			protected.POST("/virtualization/flavors", deps.Virtualization.CreateFlavor)
			protected.PUT("/virtualization/flavors/:id", deps.Virtualization.UpdateFlavor)
			protected.DELETE("/virtualization/flavors/:id", deps.Virtualization.DeleteFlavor)
			protected.GET("/virtualization/operations", deps.Virtualization.ListOperations)
			protected.GET("/virtualization/operations/:taskID", deps.Virtualization.GetOperation)
			protected.GET("/virtualization/operations/:taskID/logs", deps.Virtualization.ListOperationLogs)
			protected.GET("/virtualization/operations/:taskID/stream", deps.Virtualization.StreamTaskUpdates)
			protected.POST("/virtualization/operations/:taskID/cancel", deps.Virtualization.CancelOperation)
			protected.POST("/virtualization/operations/:taskID/retry", deps.Virtualization.RetryOperation)
			protected.POST("/virtualization/sync", deps.Virtualization.SyncAll)
		}
		if cfg.Modules.Docker.Enabled {
			protected.GET("/docker/overview", deps.Docker.Overview)
			protected.GET("/docker/hosts", deps.Docker.ListHosts)
			protected.POST("/docker/hosts", deps.Docker.CreateHost)
			protected.POST("/docker/hosts/quick-create", deps.Docker.QuickCreateHost)
			protected.GET("/docker/hosts/:id", deps.Docker.GetHost)
			protected.PUT("/docker/hosts/:id", deps.Docker.UpdateHost)
			protected.DELETE("/docker/hosts/:id", deps.Docker.DeleteHost)
			protected.GET("/docker/projects", deps.Docker.ListProjects)
			protected.POST("/docker/projects", deps.Docker.CreateProject)
			protected.GET("/docker/projects/:id", deps.Docker.GetProject)
			protected.PUT("/docker/projects/:id", deps.Docker.UpdateProject)
			protected.DELETE("/docker/projects/:id", deps.Docker.DeleteProject)
			protected.POST("/docker/projects/:id/deploy", deps.Docker.DeployProject)
			protected.POST("/docker/containers/start", deps.Docker.StartContainer)
			protected.GET("/docker/services", deps.Docker.ListServices)
			protected.POST("/docker/services/:id/actions", deps.Docker.ServiceAction)
			protected.GET("/docker/ports", deps.Docker.ListPortMappings)
			protected.POST("/docker/ports", deps.Docker.CreatePortMapping)
			protected.PUT("/docker/ports/:id", deps.Docker.UpdatePortMapping)
			protected.DELETE("/docker/ports/:id", deps.Docker.DeletePortMapping)
			protected.GET("/docker/templates", deps.Docker.ListTemplates)
			protected.POST("/docker/templates", deps.Docker.CreateTemplate)
			protected.PUT("/docker/templates/:id", deps.Docker.UpdateTemplate)
			protected.DELETE("/docker/templates/:id", deps.Docker.DeleteTemplate)
			protected.GET("/docker/operations", deps.Docker.ListOperations)
			protected.GET("/docker/operations/:id", deps.Docker.GetOperation)
			protected.GET("/docker/operations/:id/logs", deps.Docker.ListOperationLogs)
			protected.POST("/docker/operations/:id/cancel", deps.Docker.CancelOperation)
			protected.POST("/docker/operations/:id/retry", deps.Docker.RetryOperation)
		}
		if cfg.Modules.AI.Enabled {
			protected.GET("/copilot/insights", deps.Copilot.ListInsights)
			protected.GET("/copilot/workbench/catalog", deps.Copilot.GetWorkbenchCatalog)
			protected.GET("/copilot/agent-providers", deps.Copilot.ListAgentProviders)
			protected.GET("/copilot/agent-runs", deps.Copilot.ListAgentRuns)
			protected.GET("/copilot/data-source-capabilities", deps.Copilot.ListDataSourceCapabilities)
			protected.GET("/copilot/data-sources", deps.Copilot.ListDataSources)
			protected.POST("/copilot/data-sources", deps.Copilot.CreateDataSource)
			protected.PUT("/copilot/data-sources/:dataSourceID", deps.Copilot.UpdateDataSource)
			protected.POST("/copilot/data-sources/:dataSourceID/validate", deps.Copilot.ValidateDataSource)
			protected.GET("/copilot/analysis-profiles", deps.Copilot.ListAnalysisProfiles)
			protected.POST("/copilot/analysis-profiles", deps.Copilot.CreateAnalysisProfile)
			protected.PUT("/copilot/analysis-profiles/:profileID", deps.Copilot.UpdateAnalysisProfile)
			protected.GET("/copilot/automation-policies", deps.Copilot.ListAutomationPolicies)
			protected.POST("/copilot/automation-policies", deps.Copilot.CreateAutomationPolicy)
			protected.PUT("/copilot/automation-policies/:policyID", deps.Copilot.UpdateAutomationPolicy)
			protected.DELETE("/copilot/automation-policies/:policyID", deps.Copilot.DeleteAutomationPolicy)
			protected.GET("/copilot/root-cause/runs", deps.Copilot.ListRootCauseRuns)
			protected.GET("/copilot/analysis/runs", deps.Copilot.ListAnalysisRuns)
			protected.POST("/copilot/root-cause/runs", deps.Copilot.CreateRootCauseRun)
			protected.GET("/copilot/root-cause/runs/:runID", deps.Copilot.GetRootCauseRun)
			protected.GET("/copilot/sessions", deps.Copilot.ListSessions)
			protected.GET("/copilot/sessions/:sessionID", deps.Copilot.GetSession)
			protected.POST("/copilot/sessions", deps.Copilot.CreateSession)
			protected.PATCH("/copilot/sessions/:sessionID", deps.Copilot.UpdateSession)
			protected.DELETE("/copilot/sessions/:sessionID", deps.Copilot.DeleteSession)
			protected.GET("/copilot/sessions/:sessionID/messages", deps.Copilot.ListMessages)
			protected.POST("/copilot/sessions/:sessionID/messages", deps.Copilot.SendMessage)
			protected.POST("/copilot/sessions/:sessionID/analyze", deps.Copilot.AnalyzeSession)
			protected.GET("/copilot/inspection-tasks", deps.Copilot.ListInspectionTasks)
			protected.POST("/copilot/inspection-tasks", deps.Copilot.CreateInspectionTask)
			protected.PUT("/copilot/inspection-tasks/:taskID", deps.Copilot.UpdateInspectionTask)
			protected.DELETE("/copilot/inspection-tasks/:taskID", deps.Copilot.DeleteInspectionTask)
			protected.GET("/copilot/inspection-runs", deps.Copilot.ListInspectionRuns)
			protected.POST("/copilot/inspection-tasks/:taskID/execute", deps.Copilot.ExecuteInspectionTask)
			protected.POST("/copilot/inspection-runs/:runID/session", deps.Copilot.CreateSessionFromInspectionRun)
			protected.POST("/copilot/sessions/:sessionID/inspection-task", deps.Copilot.CreateInspectionTaskFromSession)
		}
		protected.GET("/audit/logs", deps.Platform.ListAuditLogs)
		protected.GET("/operations/logs", deps.Platform.ListOperationLogs)
		protected.GET("/events", deps.Platform.ListEvents)
		protected.GET("/events/:eventID", deps.Platform.GetEvent)
		protected.GET("/access/users", deps.Access.ListUsers)
		protected.POST("/access/users", deps.Access.CreateUser)
		protected.PUT("/access/users/:userID", deps.Access.UpdateUser)
		protected.DELETE("/access/users/:userID", deps.Access.DeleteUser)
		protected.POST("/access/users/:userID/revoke-sessions", deps.Access.RevokeUserSessions)
		protected.GET("/access/roles", deps.Access.ListRoles)
		protected.POST("/access/roles", deps.Access.CreateRole)
		protected.PUT("/access/roles/:roleID", deps.Access.UpdateRole)
		protected.DELETE("/access/roles/:roleID", deps.Access.DeleteRole)
		protected.GET("/access/teams", deps.Access.ListTeams)
		protected.GET("/access/permission-snapshot", deps.Access.PermissionSnapshot)
		protected.POST("/access/teams", deps.Access.CreateTeam)
		protected.PUT("/access/teams/:teamID", deps.Access.UpdateTeam)
		protected.DELETE("/access/teams/:teamID", deps.Access.DeleteTeam)
		protected.GET("/access/policies", deps.Access.ListPolicies)
		protected.POST("/access/policies", deps.Access.CreatePolicy)
		protected.PUT("/access/policies/:policyID", deps.Access.UpdatePolicy)
		protected.DELETE("/access/policies/:policyID", deps.Access.DeletePolicy)
		protected.GET("/access/scope-grants", deps.ScopeGrants.List)
		protected.POST("/access/scope-grants", deps.ScopeGrants.Create)
		protected.PUT("/access/scope-grants/:scopeGrantID", deps.ScopeGrants.Update)
		protected.DELETE("/access/scope-grants/:scopeGrantID", deps.ScopeGrants.Delete)
		protected.PUT("/access/users/:userID/roles", deps.Access.ReplaceUserRoles)
		protected.PUT("/access/users/:userID/teams", deps.Access.ReplaceUserTeams)
		protected.GET("/mcp/capabilities", deps.Platform.ListMCPCapabilities)
		protected.GET("/settings/identity", deps.Settings.GetIdentitySettings)
		protected.PUT("/settings/identity/oidc", deps.Settings.UpdateOIDCSettings)
		protected.PUT("/settings/identity/providers", deps.Settings.UpdateLoginProvidersSettings)
		protected.GET("/settings/monitoring", deps.Settings.GetMonitoringSettings)
		protected.PUT("/settings/monitoring/prometheus", deps.Settings.UpdatePrometheusSettings)
		protected.GET("/settings/ai", deps.Settings.GetAISettings)
		protected.PUT("/settings/ai/provider", deps.Settings.UpdateAISettings)
		protected.PUT("/settings/ai/providers", deps.Settings.UpdateAIProviderConnections)
		protected.POST("/settings/ai/provider/models", deps.Settings.ListAIProviderModels)
		protected.POST("/settings/ai/provider/test", deps.Settings.TestAIProviderConnectivity)
		protected.GET("/settings/branding", deps.Settings.GetBrandingSettings)
		protected.PUT("/settings/branding", deps.Settings.UpdateBrandingSettings)
		protected.POST("/settings/branding/upload", deps.Settings.UploadBrandingAsset)
	}

	// Serve uploaded branding assets
	router.Static("/branding-assets", "data/branding")

	// Serve embedded documentation site
	registerDocs(router, logger)

	// Serve embedded frontend SPA assets
	registerSPA(router, logger)

	logger.Info("http server configured",
		zap.String("addr", cfg.HTTP.Addr),
		zap.String("base_path", cfg.HTTP.BasePath),
	)

	return &http.Server{
		Addr:              cfg.HTTP.Addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       cfg.HTTP.ReadTimeout,
		WriteTimeout:      cfg.HTTP.WriteTimeout,
	}
}

func registerDocs(router *gin.Engine, logger *zap.Logger) {
	buildFS, err := docsembed.StaticFS()
	if err != nil {
		logger.Warn("docs assets not available, docs serving disabled", zap.Error(err))
		return
	}

	docsServer := http.FileServer(http.FS(buildFS))

	router.GET("/docs", func(c *gin.Context) {
		c.Redirect(http.StatusPermanentRedirect, "/docs/")
	})

	router.GET("/docs/*filepath", func(c *gin.Context) {
		requestPath := strings.TrimPrefix(c.Param("filepath"), "/")
		if requestPath == "" {
			requestPath = "index.html"
		}

		candidates := []string{requestPath}
		if !strings.Contains(path.Base(requestPath), ".") {
			candidates = append(candidates, path.Join(requestPath, "index.html"))
		}

		for _, candidate := range candidates {
			if info, err := fs.Stat(buildFS, candidate); err == nil && !info.IsDir() {
				c.Request.URL.Path = "/" + candidate
				docsServer.ServeHTTP(c.Writer, c.Request)
				return
			}
		}

		c.Request.URL.Path = "/404.html"
		docsServer.ServeHTTP(c.Writer, c.Request)
	})
}

func registerSPA(router *gin.Engine, logger *zap.Logger) {
	distFS, err := webembed.StaticFS()
	if err != nil {
		logger.Warn("web assets not available, SPA serving disabled", zap.Error(err))
		return
	}

	fileServer := http.FileServer(http.FS(distFS))

	router.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path

		// Let API and health routes fall through to 404
		if strings.HasPrefix(path, "/api/") || strings.HasPrefix(path, "/docs/") || path == "/docs" || path == "/healthz" || path == "/readyz" {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}

		// Try to serve the exact file first
		f, err := fs.Stat(distFS, strings.TrimPrefix(path, "/"))
		if err == nil && !f.IsDir() {
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}

		// SPA fallback: serve index.html for all other routes
		c.Request.URL.Path = "/"
		fileServer.ServeHTTP(c.Writer, c.Request)
	})
}
