package routes

import "github.com/gin-gonic/gin"

func registerSystemRoutes(protected gin.IRoutes, deps Dependencies) {
	protected.GET("/system/runtime-metrics", deps.System.RuntimeMetrics)
	protected.GET("/modules", deps.Module.List)
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
}

func registerOperationalAuditRoutes(protected gin.IRoutes, deps Dependencies) {
	protected.GET("/audit/logs", deps.Platform.ListAuditLogs)
	protected.GET("/audit/logs/export", deps.Platform.ExportAuditLogs)
	protected.GET("/audit/summary", deps.Platform.AuditSummary)
	protected.GET("/operations/logs", deps.Platform.ListOperationLogs)
	protected.GET("/operations/logs/export", deps.Platform.ExportOperationLogs)
	protected.GET("/operations/summary", deps.Platform.OperationSummary)
	protected.GET("/events", deps.Platform.ListEvents)
	protected.GET("/events/:eventID", deps.Platform.GetEvent)
	protected.GET("/mcp/capabilities", deps.Platform.ListMCPCapabilities)
}

func registerAccessRoutes(protected gin.IRoutes, deps Dependencies) {
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
}

func registerAIGatewayRoutes(protected gin.IRoutes, deps Dependencies) {
	if deps.AIGateway == nil {
		return
	}

	protected.GET("/ai-gateway/capabilities", deps.AIGateway.Capabilities)
	protected.POST("/ai-gateway/tools/:toolName/invoke", deps.AIGateway.InvokeTool)
	protected.POST("/ai-gateway/resources/read", deps.AIGateway.ReadResource)
	protected.POST("/ai-gateway/prompts/get", deps.AIGateway.GetPrompt)
	protected.GET("/ai-gateway/personal-access-tokens", deps.AIGateway.ListPersonalAccessTokens)
	protected.POST("/ai-gateway/personal-access-tokens", deps.AIGateway.CreatePersonalAccessToken)
	protected.POST("/ai-gateway/personal-access-tokens/:tokenID/revoke", deps.AIGateway.RevokePersonalAccessToken)
	protected.POST("/ai-gateway/personal-access-tokens/:tokenID/rotate", deps.AIGateway.RotatePersonalAccessToken)
	protected.GET("/ai-gateway/service-accounts", deps.AIGateway.ListServiceAccounts)
	protected.POST("/ai-gateway/service-accounts", deps.AIGateway.CreateServiceAccount)
	protected.GET("/ai-gateway/service-account-tokens", deps.AIGateway.ListServiceAccountTokens)
	protected.POST("/ai-gateway/service-accounts/:serviceAccountID/tokens", deps.AIGateway.CreateServiceAccountToken)
	protected.POST("/ai-gateway/service-account-tokens/:tokenID/revoke", deps.AIGateway.RevokeServiceAccountToken)
	protected.POST("/ai-gateway/service-account-tokens/:tokenID/rotate", deps.AIGateway.RotateServiceAccountToken)
	protected.GET("/ai-gateway/ai-clients", deps.AIGateway.ListAIClients)
	protected.POST("/ai-gateway/ai-clients", deps.AIGateway.CreateAIClient)
	protected.PUT("/ai-gateway/ai-clients/:clientID", deps.AIGateway.UpdateAIClient)
	protected.GET("/ai-gateway/tool-grants", deps.AIGateway.ListToolGrants)
	protected.POST("/ai-gateway/tool-grants", deps.AIGateway.CreateToolGrant)
	protected.DELETE("/ai-gateway/tool-grants/:grantID", deps.AIGateway.DeleteToolGrant)
	protected.GET("/ai-gateway/access-policies", deps.AIGateway.ListAccessPolicies)
	protected.POST("/ai-gateway/access-policies", deps.AIGateway.CreateAccessPolicy)
	protected.PUT("/ai-gateway/access-policies/:policyID", deps.AIGateway.UpdateAccessPolicy)
	protected.DELETE("/ai-gateway/access-policies/:policyID", deps.AIGateway.DeleteAccessPolicy)
	protected.GET("/ai-gateway/governance/status", deps.AIGateway.GovernanceStatus)
	protected.GET("/ai-gateway/skill-bindings", deps.AIGateway.ListSkillBindings)
	protected.POST("/ai-gateway/skill-bindings", deps.AIGateway.CreateSkillBinding)
	protected.PUT("/ai-gateway/skill-bindings/:bindingID", deps.AIGateway.UpdateSkillBinding)
	protected.DELETE("/ai-gateway/skill-bindings/:bindingID", deps.AIGateway.DeleteSkillBinding)
	protected.GET("/ai-gateway/audit-logs", deps.AIGateway.ListAuditLogs)
	protected.GET("/ai-gateway/approval-requests", deps.AIGateway.ListApprovalRequests)
	protected.GET("/ai-gateway/approval-requests/:requestID/timeline", deps.AIGateway.GetApprovalTimeline)
	protected.POST("/ai-gateway/approval-requests/:requestID/approve", deps.AIGateway.ApproveApprovalRequest)
	protected.POST("/ai-gateway/approval-requests/:requestID/reject", deps.AIGateway.RejectApprovalRequest)
	protected.POST("/ai-gateway/approval-requests/:requestID/cancel", deps.AIGateway.CancelApprovalRequest)
}

func registerPluginRoutes(protected gin.IRoutes, deps Dependencies) {
	if deps.Plugins == nil {
		return
	}

	protected.GET("/plugins/marketplace", deps.Plugins.ListMarketplace)
	protected.GET("/plugins/marketplace/:pluginID", deps.Plugins.GetMarketplace)
	protected.GET("/plugins/installed", deps.Plugins.ListInstalled)
	protected.POST("/plugins/install", deps.Plugins.Install)
	protected.GET("/plugins/:pluginID", deps.Plugins.GetInstalled)
	protected.DELETE("/plugins/:pluginID", deps.Plugins.Remove)
	protected.GET("/plugins/:pluginID/manifest", deps.Plugins.GetManifest)
	protected.POST("/plugins/:pluginID/enable", deps.Plugins.Enable)
	protected.POST("/plugins/:pluginID/disable", deps.Plugins.Disable)
	protected.POST("/plugins/:pluginID/upgrade", deps.Plugins.Upgrade)
	protected.PUT("/plugins/:pluginID/config", deps.Plugins.Configure)
}

func registerSettingsRoutes(protected gin.IRoutes, deps Dependencies) {
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
