package routes

import (
	"github.com/gin-gonic/gin"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
)

func registerComputeRoutes(protected gin.IRoutes, cfg cfgpkg.Config, deps Dependencies) {
	if !cfg.Modules.Virtualization.Enabled && !cfg.Modules.Docker.Enabled {
		return
	}
	protected.GET("/compute/overview", deps.Compute.Overview)
	protected.GET("/compute/access-sources", deps.Compute.ListAccessSources)
	protected.GET("/compute/tasks", deps.Compute.ListTasks)
	protected.GET("/compute/tasks/:domain/:id", deps.Compute.GetTask)
	protected.GET("/compute/tasks/:domain/:id/logs", deps.Compute.ListTaskLogs)
	protected.POST("/compute/tasks/:domain/:id/cancel", deps.Compute.CancelTask)
	protected.POST("/compute/tasks/:domain/:id/retry", deps.Compute.RetryTask)
}

func registerVirtualizationRoutes(protected gin.IRoutes, cfg cfgpkg.Config, deps Dependencies) {
	if !cfg.Modules.Virtualization.Enabled {
		return
	}

	protected.GET("/virtualization/clusters", deps.Virtualization.ListConnections)
	protected.POST("/virtualization/clusters", deps.Virtualization.CreateConnection)
	protected.PUT("/virtualization/clusters/:id", deps.Virtualization.UpdateConnection)
	protected.GET("/virtualization/clusters/:id/delete-dependencies", deps.Virtualization.GetConnectionDeleteDependencies)
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

func registerDockerRoutes(protected gin.IRoutes, cfg cfgpkg.Config, deps Dependencies) {
	if !cfg.Modules.Docker.Enabled {
		return
	}

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
	protected.GET("/docker/projects/:id/runtime/logs", deps.Docker.GetProjectLogs)
	protected.GET("/docker/projects/:id/runtime/logs/stream", deps.Docker.StreamProjectLogs)
	protected.GET("/docker/projects/:id/runtime/terminal", deps.Docker.StreamProjectTerminal)
	protected.GET("/docker/projects/:id/runtime/volumes", deps.Docker.ListProjectVolumes)
	protected.GET("/docker/projects/:id/runtime/volume-files", deps.Docker.ListProjectVolumeFiles)
	protected.GET("/docker/projects/:id/runtime/volume-file", deps.Docker.ReadProjectVolumeFile)
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

func registerCopilotRoutes(protected gin.IRoutes, cfg cfgpkg.Config, deps Dependencies) {
	if !cfg.Modules.AI.Enabled {
		return
	}

	protected.GET("/copilot/insights", deps.Copilot.ListInsights)
	protected.GET("/copilot/workbench/catalog", deps.Copilot.GetWorkbenchCatalog)
	protected.GET("/copilot/agent-providers", deps.Copilot.ListAgentProviders)
	protected.GET("/copilot/agent-runs", deps.Copilot.ListAgentRuns)
	protected.POST("/copilot/agent-runs/:runID/cancel", deps.Copilot.CancelAgentRun)
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
	protected.POST("/copilot/sessions/:sessionID/messages/stream", deps.Copilot.StreamMessage)
	protected.POST("/copilot/sessions/:sessionID/analyze", deps.Copilot.AnalyzeSession)
	protected.POST("/copilot/global-assistant/events", deps.Copilot.RecordGlobalAssistantEvent)
	protected.GET("/copilot/inspection-tasks", deps.Copilot.ListInspectionTasks)
	protected.POST("/copilot/inspection-tasks", deps.Copilot.CreateInspectionTask)
	protected.PUT("/copilot/inspection-tasks/:taskID", deps.Copilot.UpdateInspectionTask)
	protected.DELETE("/copilot/inspection-tasks/:taskID", deps.Copilot.DeleteInspectionTask)
	protected.GET("/copilot/inspection-runs", deps.Copilot.ListInspectionRuns)
	protected.POST("/copilot/inspection-tasks/:taskID/execute", deps.Copilot.ExecuteInspectionTask)
	protected.POST("/copilot/inspection-runs/:runID/session", deps.Copilot.CreateSessionFromInspectionRun)
	protected.POST("/copilot/sessions/:sessionID/inspection-task", deps.Copilot.CreateInspectionTaskFromSession)
}
