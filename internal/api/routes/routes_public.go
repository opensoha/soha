package routes

import (
	"github.com/gin-gonic/gin"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
)

func registerPublicRoutes(v1 gin.IRoutes, cfg cfgpkg.Config, deps Dependencies) {
	v1.GET("/healthz", deps.System.Healthz)
	v1.GET("/readyz", deps.System.Readyz)
	v1.GET("/auth/providers", deps.Auth.ListProviders)
	v1.GET("/auth/login-options", deps.Auth.LoginOptions)
	v1.POST("/auth/login", deps.Auth.Login)
	v1.POST("/auth/refresh", deps.Auth.Refresh)
	v1.GET("/auth/oidc/login", deps.Auth.OIDCLogin)
	v1.GET("/auth/oidc/callback", deps.Auth.OIDCCallback)
	v1.GET("/auth/providers/:providerID/login", deps.Auth.ProviderLogin)
	v1.GET("/auth/login/:providerID/start", deps.Auth.ProviderLogin)
	v1.GET("/auth/login/:providerID/callback", deps.Auth.ProviderCallback)
	v1.POST("/auth/oidc/exchange", deps.Auth.OIDCExchange)
	registerProviderProtocolRoutes(v1, deps)

	if cfg.Modules.Monitoring.Enabled {
		v1.POST("/integrations/alerts/webhook", deps.Monitoring.IngestWebhook)
		v1.POST("/integrations/alerts/:integrationID/webhook", deps.Monitoring.IngestIntegrationWebhook)
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
	if cfg.Modules.AIGateway.Enabled && deps.Platform != nil {
		v1.POST("/connectors/events", deps.Platform.IngestConnectorEvents)
	}
}
