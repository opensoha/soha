package routes

import (
	"github.com/gin-gonic/gin"
	apiHandlers "github.com/opensoha/soha/internal/api/handlers"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
)

func registerProtectedRoutes(protected gin.IRoutes, cfg cfgpkg.Config, deps Dependencies) {
	registerProtectedAuthRoutes(protected, deps)
	registerSystemRoutes(protected, deps)
	registerPlatformRoutes(protected, deps)
	registerMonitoringRoutes(protected, cfg, deps)
	registerDeliveryRoutes(protected, cfg, deps)
	registerVirtualizationRoutes(protected, cfg, deps)
	registerDockerRoutes(protected, cfg, deps)
	registerCopilotRoutes(protected, cfg, deps)
	if deps.Knowledge != nil {
		apiHandlers.RegisterKnowledgeRoutes(protected, deps.Knowledge)
	}
	if deps.AgentProviders != nil {
		apiHandlers.RegisterProtectedAgentProviderRoutes(protected, deps.AgentProviders)
	}
	if deps.Evaluation != nil {
		apiHandlers.RegisterEvaluationRoutes(protected, deps.Evaluation)
	}
	if deps.AIAdvanced != nil {
		apiHandlers.RegisterAIAdvancedRoutes(protected, deps.AIAdvanced)
	}
	if deps.AIProduction != nil {
		apiHandlers.RegisterAIProductionRoutes(protected, deps.AIProduction)
	}
	registerOperationalAuditRoutes(protected, deps)
	registerAccessRoutes(protected, deps)
	registerDirectorySyncRoutes(protected, deps)
	registerProviderPortalRoutes(protected, deps)
	registerAIGatewayRoutes(protected, deps)
	registerPluginRoutes(protected, deps)
	registerSettingsRoutes(protected, deps)
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
