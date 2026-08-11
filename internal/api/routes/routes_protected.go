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
	registerComputeRoutes(protected.Group("", apiMiddleware.RequireModule(deps.ModuleState, "compute")), deps)
	registerVirtualizationRoutes(protected.Group("", apiMiddleware.RequireModule(deps.ModuleState, "virtualization")), deps)
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
	registerSoftwareRoutes(protected, deps)
	registerCompanionRoutes(protected, deps)
	registerSettingsRoutes(protected, deps)
	registerSystemIntegrationRoutes(protected, deps)
	registerSecretRoutes(protected, deps)
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
	if deps.MFA != nil {
		protected.GET("/identity/mfa/credentials", deps.MFA.ListCredentials)
		protected.DELETE("/identity/mfa/credentials/:mfaCredentialID", deps.MFA.RevokeCredential)
		protected.POST("/identity/mfa/totp/enroll", deps.MFA.BeginTOTPEnrollment)
		protected.POST("/identity/mfa/challenges/:mfaChallengeID/verify", deps.MFA.VerifyChallenge)
		protected.POST("/identity/mfa/recovery-codes/challenge", deps.MFA.BeginRecoveryChallenge)
		protected.POST("/identity/mfa/webauthn/enroll", deps.MFA.BeginWebAuthnEnrollment)
		protected.POST("/identity/mfa/webauthn/authenticate", deps.MFA.BeginWebAuthnAuthentication)
		protected.POST("/identity/mfa/webauthn/challenges/:mfaChallengeID/verify", deps.MFA.VerifyWebAuthnChallenge)
		protected.POST("/identity/mfa/recovery-codes/regenerate", deps.MFA.RegenerateRecoveryCodes)
		protected.POST("/identity/users/:identityUserID/mfa/credentials/:mfaCredentialID/revoke", deps.MFA.AdminRevokeCredential)
		protected.POST("/identity/users/:identityUserID/mfa/reset", deps.MFA.AdminResetUserMFA)
	}
}
