package routes

import (
	"github.com/gin-gonic/gin"
	apiHandlers "github.com/opensoha/soha/internal/api/handlers"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
)

func registerProtectedRoutes(protected *gin.RouterGroup, cfg cfgpkg.Config, deps Dependencies) {
	registerProtectedAuthRoutes(protected, deps)
	registerSystemRoutes(protected, deps)
	registerPlatformRoutes(protected, deps)
	registerMonitoringRoutes(protected.Group("", apiMiddleware.RequireModule(deps.ModuleState, "monitoring")), cfg, deps)
	registerDeliveryRoutes(protected.Group("", apiMiddleware.RequireModule(deps.ModuleState, "delivery")), cfg, deps)
	registerComputeRoutes(protected.Group("", apiMiddleware.RequireModule(deps.ModuleState, "compute")), cfg, deps)
	registerVirtualizationRoutes(protected.Group("", apiMiddleware.RequireModule(deps.ModuleState, "virtualization")), cfg, deps)
	registerDockerRoutes(protected.Group("", apiMiddleware.RequireModule(deps.ModuleState, "docker")), cfg, deps)
	aiRoutes := protected.Group("", apiMiddleware.RequireModule(deps.ModuleState, "ai"))
	registerCopilotRoutes(aiRoutes, cfg, deps)
	if deps.Knowledge != nil {
		apiHandlers.RegisterKnowledgeRoutes(aiRoutes, deps.Knowledge)
	}
	if deps.AgentProviders != nil {
		apiHandlers.RegisterProtectedAgentProviderRoutes(aiRoutes, deps.AgentProviders)
	}
	if deps.Evaluation != nil {
		apiHandlers.RegisterEvaluationRoutes(aiRoutes, deps.Evaluation)
	}
	if deps.AIAdvanced != nil {
		apiHandlers.RegisterAIAdvancedRoutes(aiRoutes, deps.AIAdvanced)
	}
	if deps.AIProduction != nil {
		apiHandlers.RegisterAIProductionRoutes(aiRoutes, deps.AIProduction)
	}
	registerOperationalAuditRoutes(protected, deps)
	registerAccessRoutes(protected, deps)
	registerDirectorySyncRoutes(protected, deps)
	registerProviderPortalRoutes(protected, deps)
	registerAIGatewayRoutes(protected.Group("", apiMiddleware.RequireModule(deps.ModuleState, "aiGateway")), deps)
	registerPluginRoutes(protected, deps)
	registerSettingsRoutes(protected, deps)
	registerSystemIntegrationRoutes(protected, deps)
}

func registerProtectedAuthRoutes(protected gin.IRoutes, deps Dependencies) {
	protected.GET("/auth/me", deps.Auth.Me)
	protected.GET("/auth/profile", deps.Auth.Profile)
	protected.PATCH("/auth/profile", deps.Auth.UpdateProfile)
	protected.POST("/auth/profile/password", deps.Auth.ChangePassword)
	protected.POST("/auth/profile/identities/:providerID/link", deps.Auth.ProviderLink)
	protected.GET("/auth/bootstrap", deps.Auth.Bootstrap)
	protected.POST("/auth/logout", deps.Auth.Logout)
	protected.POST("/auth/stream-ticket", deps.Auth.IssueStreamTicket)
	protected.GET("/auth/sessions", deps.Auth.ListSessions)
	protected.POST("/auth/sessions/:sessionID/revoke", deps.Auth.RevokeSession)
}
