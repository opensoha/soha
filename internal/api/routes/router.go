package routes

import (
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	apiHandlers "github.com/kubecrux/kubecrux/internal/api/handlers"
	apiMiddleware "github.com/kubecrux/kubecrux/internal/api/middleware"
	cfgpkg "github.com/kubecrux/kubecrux/internal/infrastructure/config"
	swaggerinfra "github.com/kubecrux/kubecrux/internal/infrastructure/swagger"
	webembed "github.com/kubecrux/kubecrux/web"
	"go.uber.org/zap"
)

type Dependencies struct {
	System        *apiHandlers.SystemHandler
	Platform      *apiHandlers.PlatformHandler
	Announcements *apiHandlers.AnnouncementHandler
	Monitoring    *apiHandlers.MonitoringHandler
	Catalog       *apiHandlers.CatalogHandler
	Applications  *apiHandlers.ApplicationHandler
	Builds        *apiHandlers.BuildHandler
	Workflows     *apiHandlers.WorkflowHandler
	Registries    *apiHandlers.RegistryHandler
	Releases      *apiHandlers.ReleaseHandler
	Copilot       *apiHandlers.CopilotHandler
	Access        *apiHandlers.AccessHandler
	ScopeGrants   *apiHandlers.ScopeGrantHandler
	Menu          *apiHandlers.MenuHandler
	Settings      *apiHandlers.SettingsHandler
	Auth          *apiHandlers.AuthHandler
	Authn         apiMiddleware.AccessTokenParser
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

	v1 := router.Group(cfg.HTTP.BasePath)
	{
		v1.GET("/healthz", deps.System.Healthz)
		v1.GET("/readyz", deps.System.Readyz)
		v1.GET("/auth/providers", deps.Auth.ListProviders)
		v1.POST("/auth/login", deps.Auth.Login)
		v1.POST("/auth/refresh", deps.Auth.Refresh)
		v1.GET("/auth/oidc/login", deps.Auth.OIDCLogin)
		v1.GET("/auth/oidc/callback", deps.Auth.OIDCCallback)
		v1.POST("/auth/oidc/exchange", deps.Auth.OIDCExchange)
		v1.POST("/integrations/alerts/webhook", deps.Monitoring.IngestWebhook)
	}

	protected := router.Group(cfg.HTTP.BasePath)
	protected.Use(apiMiddleware.RequireAuth())
	{
		protected.GET("/auth/me", deps.Auth.Me)
		protected.POST("/auth/logout", deps.Auth.Logout)
		protected.GET("/auth/sessions", deps.Auth.ListSessions)
		protected.POST("/auth/sessions/:sessionID/revoke", deps.Auth.RevokeSession)
		protected.GET("/system/runtime-metrics", deps.System.RuntimeMetrics)
		protected.GET("/clusters", deps.Platform.ListClusters)
		protected.GET("/menus", deps.Menu.ListAll)
		protected.GET("/menus/:menuID", deps.Menu.Get)
		protected.GET("/menus/visible", deps.Menu.ListVisible)
		protected.POST("/menus", deps.Menu.Create)
		protected.PUT("/menus/:menuID", deps.Menu.Update)
		protected.DELETE("/menus/:menuID", deps.Menu.Delete)
		protected.GET("/announcements", deps.Announcements.List)
		protected.GET("/announcements/:announcementID", deps.Announcements.Get)
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
		protected.GET("/clusters/:clusterID/configuration/secrets", deps.Platform.ListSecrets)
		protected.GET("/clusters/:clusterID/configuration/hpas", deps.Platform.ListHorizontalPodAutoscalers)
		protected.GET("/clusters/:clusterID/configuration/poddisruptionbudgets", deps.Platform.ListPodDisruptionBudgets)
		protected.GET("/clusters/:clusterID/access-control/serviceaccounts", deps.Platform.ListServiceAccounts)
		protected.GET("/clusters/:clusterID/access-control/roles", deps.Platform.ListRoles)
		protected.GET("/clusters/:clusterID/access-control/rolebindings", deps.Platform.ListRoleBindings)
		protected.GET("/clusters/:clusterID/network/services", deps.Platform.ListServices)
		protected.GET("/clusters/:clusterID/network/services/:serviceName/metrics", deps.Platform.GetServiceMetrics)
		protected.GET("/clusters/:clusterID/network/ingresses", deps.Platform.ListIngresses)
		protected.GET("/clusters/:clusterID/network/endpointslices", deps.Platform.ListEndpointSlices)
		protected.GET("/clusters/:clusterID/network/networkpolicies", deps.Platform.ListNetworkPolicies)
		protected.GET("/clusters/:clusterID/network/gateways", deps.Platform.ListGateways)
		protected.GET("/clusters/:clusterID/network/httproutes", deps.Platform.ListHTTPRoutes)
		protected.GET("/clusters/:clusterID/storage/persistentvolumeclaims", deps.Platform.ListPersistentVolumeClaims)
		protected.GET("/clusters/:clusterID/storage/persistentvolumes", deps.Platform.ListPersistentVolumes)
		protected.GET("/clusters/:clusterID/storage/storageclasses", deps.Platform.ListStorageClasses)
		protected.GET("/clusters/:clusterID/extensions/crds", deps.Platform.ListCRDs)
		protected.GET("/clusters/:clusterID/helm/releases", deps.Platform.ListHelmReleases)
		protected.GET("/clusters/:clusterID/events", deps.Platform.ListClusterEvents)
		protected.POST("/clusters/:clusterID/workloads/deployments/restart", deps.Platform.RestartDeployment)
		protected.POST("/clusters/:clusterID/workloads/deployments/rollback", deps.Platform.RollbackDeployment)
		protected.POST("/clusters/:clusterID/workloads/deployments/scale", deps.Platform.ScaleDeployment)
		protected.GET("/monitoring/summary", deps.Monitoring.Summary)
		protected.GET("/alerts", deps.Monitoring.ListAlerts)
		protected.GET("/alerts/:alertID", deps.Monitoring.GetAlert)
		protected.PUT("/alerts/:alertID/ownership", deps.Monitoring.UpdateAlertOwnership)
		protected.POST("/alerts/:alertID/acknowledge", deps.Monitoring.AcknowledgeAlert)
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
		protected.POST("/application-environments", deps.Catalog.CreateApplicationEnvironment)
		protected.PUT("/application-environments/:applicationEnvironmentID", deps.Catalog.UpdateApplicationEnvironment)
		protected.DELETE("/application-environments/:applicationEnvironmentID", deps.Catalog.DeleteApplicationEnvironment)
		protected.GET("/workflow-templates", deps.Catalog.ListWorkflowTemplates)
		protected.POST("/workflow-templates", deps.Catalog.CreateWorkflowTemplate)
		protected.PUT("/workflow-templates/:workflowTemplateID", deps.Catalog.UpdateWorkflowTemplate)
		protected.DELETE("/workflow-templates/:workflowTemplateID", deps.Catalog.DeleteWorkflowTemplate)
		protected.GET("/applications", deps.Applications.ListApplications)
		protected.POST("/applications", deps.Applications.CreateApplication)
		protected.GET("/applications/:applicationID", deps.Applications.GetApplication)
		protected.PUT("/applications/:applicationID", deps.Applications.UpdateApplication)
		protected.DELETE("/applications/:applicationID", deps.Applications.DeleteApplication)
		protected.GET("/builds", deps.Builds.ListBuilds)
		protected.POST("/builds/trigger", deps.Builds.TriggerBuild)
		protected.GET("/workflows", deps.Workflows.List)
		protected.POST("/workflows/trigger", deps.Workflows.Trigger)
		protected.GET("/registries", deps.Registries.List)
		protected.POST("/registries", deps.Registries.Create)
		protected.PUT("/registries/:connectionID", deps.Registries.Update)
		protected.DELETE("/registries/:connectionID", deps.Registries.Delete)
		protected.GET("/releases", deps.Releases.ListReleases)
		protected.POST("/releases/trigger", deps.Releases.TriggerRelease)
		protected.GET("/integrations/gitlab/projects", deps.Applications.ListGitRepositories)
		protected.GET("/integrations/gitlab/branches", deps.Applications.ListGitBranches)
		protected.GET("/integrations/gitlab/tags", deps.Applications.ListGitTags)
		protected.GET("/copilot/insights", deps.Copilot.ListInsights)
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
		protected.GET("/copilot/root-cause/runs", deps.Copilot.ListRootCauseRuns)
		protected.POST("/copilot/root-cause/runs", deps.Copilot.CreateRootCauseRun)
		protected.GET("/copilot/root-cause/runs/:runID", deps.Copilot.GetRootCauseRun)
		protected.GET("/copilot/sessions", deps.Copilot.ListSessions)
		protected.POST("/copilot/sessions", deps.Copilot.CreateSession)
		protected.GET("/copilot/sessions/:sessionID/messages", deps.Copilot.ListMessages)
		protected.POST("/copilot/sessions/:sessionID/messages", deps.Copilot.SendMessage)
		protected.GET("/copilot/inspection-tasks", deps.Copilot.ListInspectionTasks)
		protected.POST("/copilot/inspection-tasks", deps.Copilot.CreateInspectionTask)
		protected.PUT("/copilot/inspection-tasks/:taskID", deps.Copilot.UpdateInspectionTask)
		protected.GET("/copilot/inspection-runs", deps.Copilot.ListInspectionRuns)
		protected.POST("/copilot/inspection-tasks/:taskID/execute", deps.Copilot.ExecuteInspectionTask)
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
		protected.GET("/settings/monitoring", deps.Settings.GetMonitoringSettings)
		protected.PUT("/settings/monitoring/prometheus", deps.Settings.UpdatePrometheusSettings)
		protected.GET("/settings/ai", deps.Settings.GetAISettings)
		protected.PUT("/settings/ai/provider", deps.Settings.UpdateAISettings)
		protected.GET("/settings/branding", deps.Settings.GetBrandingSettings)
		protected.PUT("/settings/branding", deps.Settings.UpdateBrandingSettings)
		protected.POST("/settings/branding/upload", deps.Settings.UploadBrandingAsset)
	}

	// Serve uploaded branding assets
	router.Static("/branding-assets", "data/branding")

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

func registerSPA(router *gin.Engine, logger *zap.Logger) {
	distFS, err := fs.Sub(webembed.Assets, "dist")
	if err != nil {
		logger.Warn("web assets not available, SPA serving disabled", zap.Error(err))
		return
	}

	fileServer := http.FileServer(http.FS(distFS))

	router.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path

		// Let API and health routes fall through to 404
		if strings.HasPrefix(path, "/api/") || path == "/healthz" || path == "/readyz" {
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
