package routes

import (
	"github.com/gin-gonic/gin"
	cfgpkg "github.com/soha/soha/internal/infrastructure/config"
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
	registerOperationalAuditRoutes(protected, deps)
	registerAccessRoutes(protected, deps)
	registerAIGatewayRoutes(protected, deps)
	registerSettingsRoutes(protected, deps)
}

func registerProtectedAuthRoutes(protected gin.IRoutes, deps Dependencies) {
	protected.GET("/auth/me", deps.Auth.Me)
	protected.GET("/auth/profile", deps.Auth.Profile)
	protected.GET("/auth/bootstrap", deps.Auth.Bootstrap)
	protected.POST("/auth/logout", deps.Auth.Logout)
	protected.POST("/auth/stream-ticket", deps.Auth.IssueStreamTicket)
	protected.GET("/auth/sessions", deps.Auth.ListSessions)
	protected.POST("/auth/sessions/:sessionID/revoke", deps.Auth.RevokeSession)
}
