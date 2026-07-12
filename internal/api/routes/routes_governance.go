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
	registerAccessUserRoleRoutes(protected, deps)
	registerAccessTeamPolicyRoutes(protected, deps)
}

func registerAccessUserRoleRoutes(protected gin.IRoutes, deps Dependencies) {
	protected.GET("/access/users", deps.Access.ListUsers)
	protected.POST("/access/users", deps.Access.CreateUser)
	protected.PUT("/access/users/:userID", deps.Access.UpdateUser)
	protected.DELETE("/access/users/:userID", deps.Access.DeleteUser)
	protected.POST("/access/users/:userID/revoke-sessions", deps.Access.RevokeUserSessions)
	protected.GET("/access/roles", deps.Access.ListRoles)
	protected.POST("/access/roles", deps.Access.CreateRole)
	protected.PUT("/access/roles/:roleID", deps.Access.UpdateRole)
	protected.DELETE("/access/roles/:roleID", deps.Access.DeleteRole)
	protected.PUT("/access/users/:userID/roles", deps.Access.ReplaceUserRoles)
	protected.PUT("/access/users/:userID/teams", deps.Access.ReplaceUserTeams)
}

func registerAccessTeamPolicyRoutes(protected gin.IRoutes, deps Dependencies) {
	registerAccessTeamRoutes(protected, deps)
	registerAccessPolicyScopeRoutes(protected, deps)
}

func registerAccessTeamRoutes(protected gin.IRoutes, deps Dependencies) {
	protected.GET("/access/teams", deps.Access.ListTeams)
	protected.GET("/access/permission-snapshot", deps.Access.PermissionSnapshot)
	protected.POST("/access/teams", deps.Access.CreateTeam)
	protected.PUT("/access/teams/:teamID", deps.Access.UpdateTeam)
	protected.DELETE("/access/teams/:teamID", deps.Access.DeleteTeam)
}

func registerAccessPolicyScopeRoutes(protected gin.IRoutes, deps Dependencies) {
	protected.GET("/access/policies", deps.Access.ListPolicies)
	protected.POST("/access/policies", deps.Access.CreatePolicy)
	protected.PUT("/access/policies/:policyID", deps.Access.UpdatePolicy)
	protected.DELETE("/access/policies/:policyID", deps.Access.DeletePolicy)
	protected.GET("/access/scope-grants", deps.ScopeGrants.List)
	protected.POST("/access/scope-grants", deps.ScopeGrants.Create)
	protected.PUT("/access/scope-grants/:scopeGrantID", deps.ScopeGrants.Update)
	protected.DELETE("/access/scope-grants/:scopeGrantID", deps.ScopeGrants.Delete)
}

func registerAIGatewayRoutes(protected gin.IRoutes, deps Dependencies) {
	registerAIGatewayManagementRoutes(protected, deps)
	registerAIGatewayRelayRoutes(protected, deps)
}

func registerAIGatewayManagementRoutes(protected gin.IRoutes, deps Dependencies) {
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
	protected.GET("/ai-gateway/relay/upstreams", deps.AIGateway.ListLLMUpstreams)
	protected.POST("/ai-gateway/relay/upstreams", deps.AIGateway.CreateLLMUpstream)
	protected.PUT("/ai-gateway/relay/upstreams/:upstreamID", deps.AIGateway.UpdateLLMUpstream)
	protected.POST("/ai-gateway/relay/upstreams/health-checks/run", deps.AIGateway.RunLLMRelayHealthChecks)
	protected.POST("/ai-gateway/relay/upstreams/:upstreamID/test", deps.AIGateway.TestLLMUpstream)
	protected.GET("/ai-gateway/relay/model-routes", deps.AIGateway.ListLLMModelRoutes)
	protected.POST("/ai-gateway/relay/model-routes", deps.AIGateway.CreateLLMModelRoute)
	protected.PUT("/ai-gateway/relay/model-routes/:routeID", deps.AIGateway.UpdateLLMModelRoute)
	protected.DELETE("/ai-gateway/relay/model-routes/:routeID", deps.AIGateway.DeleteLLMModelRoute)
	protected.GET("/ai-gateway/relay/model-calls", deps.AIGateway.ListLLMCallLogs)
	protected.GET("/ai-gateway/relay/metrics", deps.AIGateway.LLMRelayMetrics)
	protected.GET("/ai-gateway/relay/cache/stats", deps.AIGateway.LLMRelayCacheStats)
	protected.POST("/ai-gateway/relay/cache/purge", deps.AIGateway.PurgeLLMRelayCache)
}

func registerAIGatewayRelayRoutes(protected gin.IRoutes, deps Dependencies) {
	protected.GET("/ai-gateway/llm/openai/v1/models", deps.AIGateway.RelayOpenAIModels)
	protected.POST("/ai-gateway/llm/openai/v1/chat/completions", deps.AIGateway.RelayOpenAIChatCompletions)
	protected.POST("/ai-gateway/llm/openai/v1/responses", deps.AIGateway.RelayOpenAIResponses)
	protected.POST("/ai-gateway/llm/openai/v1/embeddings", deps.AIGateway.RelayOpenAIEmbeddings)
	protected.POST("/ai-gateway/llm/openai/v1/images/generations", deps.AIGateway.RelayOpenAIImageGenerations)
	protected.POST("/ai-gateway/llm/openai/v1/images/edits", deps.AIGateway.RelayOpenAIImageEdits)
	protected.POST("/ai-gateway/llm/openai/v1/images/variations", deps.AIGateway.RelayOpenAIImageVariations)
	protected.POST("/ai-gateway/llm/openai/v1/audio/speech", deps.AIGateway.RelayOpenAIAudioSpeech)
	protected.POST("/ai-gateway/llm/openai/v1/audio/transcriptions", deps.AIGateway.RelayOpenAIAudioTranscriptions)
	protected.POST("/ai-gateway/llm/openai/v1/audio/translations", deps.AIGateway.RelayOpenAIAudioTranslations)
	protected.GET("/ai-gateway/llm/openai/v1/realtime", deps.AIGateway.RelayOpenAIRealtime)
	protected.GET("/ai-gateway/llm/deepseek/v1/models", deps.AIGateway.RelayDeepSeekModels)
	protected.POST("/ai-gateway/llm/deepseek/v1/chat/completions", deps.AIGateway.RelayDeepSeekChatCompletions)
	protected.POST("/ai-gateway/llm/deepseek/v1/responses", deps.AIGateway.RelayDeepSeekResponses)
	protected.POST("/ai-gateway/llm/deepseek/v1/embeddings", deps.AIGateway.RelayDeepSeekEmbeddings)
	protected.POST("/ai-gateway/llm/deepseek/v1/images/generations", deps.AIGateway.RelayDeepSeekImageGenerations)
	protected.POST("/ai-gateway/llm/deepseek/v1/images/edits", deps.AIGateway.RelayDeepSeekImageEdits)
	protected.POST("/ai-gateway/llm/deepseek/v1/images/variations", deps.AIGateway.RelayDeepSeekImageVariations)
	protected.POST("/ai-gateway/llm/deepseek/v1/audio/speech", deps.AIGateway.RelayDeepSeekAudioSpeech)
	protected.POST("/ai-gateway/llm/deepseek/v1/audio/transcriptions", deps.AIGateway.RelayDeepSeekAudioTranscriptions)
	protected.POST("/ai-gateway/llm/deepseek/v1/audio/translations", deps.AIGateway.RelayDeepSeekAudioTranslations)
	protected.GET("/ai-gateway/llm/qwen/v1/models", deps.AIGateway.RelayQwenModels)
	protected.POST("/ai-gateway/llm/qwen/v1/chat/completions", deps.AIGateway.RelayQwenChatCompletions)
	protected.POST("/ai-gateway/llm/qwen/v1/responses", deps.AIGateway.RelayQwenResponses)
	protected.POST("/ai-gateway/llm/qwen/v1/embeddings", deps.AIGateway.RelayQwenEmbeddings)
	protected.POST("/ai-gateway/llm/qwen/v1/images/generations", deps.AIGateway.RelayQwenImageGenerations)
	protected.POST("/ai-gateway/llm/qwen/v1/images/edits", deps.AIGateway.RelayQwenImageEdits)
	protected.POST("/ai-gateway/llm/qwen/v1/images/variations", deps.AIGateway.RelayQwenImageVariations)
	protected.POST("/ai-gateway/llm/qwen/v1/audio/speech", deps.AIGateway.RelayQwenAudioSpeech)
	protected.POST("/ai-gateway/llm/qwen/v1/audio/transcriptions", deps.AIGateway.RelayQwenAudioTranscriptions)
	protected.POST("/ai-gateway/llm/qwen/v1/audio/translations", deps.AIGateway.RelayQwenAudioTranslations)
	protected.GET("/ai-gateway/llm/openrouter/v1/models", deps.AIGateway.RelayOpenRouterModels)
	protected.POST("/ai-gateway/llm/openrouter/v1/chat/completions", deps.AIGateway.RelayOpenRouterChatCompletions)
	protected.POST("/ai-gateway/llm/openrouter/v1/responses", deps.AIGateway.RelayOpenRouterResponses)
	protected.POST("/ai-gateway/llm/openrouter/v1/embeddings", deps.AIGateway.RelayOpenRouterEmbeddings)
	protected.POST("/ai-gateway/llm/openrouter/v1/images/generations", deps.AIGateway.RelayOpenRouterImageGenerations)
	protected.POST("/ai-gateway/llm/openrouter/v1/images/edits", deps.AIGateway.RelayOpenRouterImageEdits)
	protected.POST("/ai-gateway/llm/openrouter/v1/images/variations", deps.AIGateway.RelayOpenRouterImageVariations)
	protected.POST("/ai-gateway/llm/openrouter/v1/audio/speech", deps.AIGateway.RelayOpenRouterAudioSpeech)
	protected.POST("/ai-gateway/llm/openrouter/v1/audio/transcriptions", deps.AIGateway.RelayOpenRouterAudioTranscriptions)
	protected.POST("/ai-gateway/llm/openrouter/v1/audio/translations", deps.AIGateway.RelayOpenRouterAudioTranslations)
	protected.GET("/ai-gateway/llm/azure-openai/v1/models", deps.AIGateway.RelayAzureOpenAIModels)
	protected.POST("/ai-gateway/llm/azure-openai/v1/chat/completions", deps.AIGateway.RelayAzureOpenAIChatCompletions)
	protected.POST("/ai-gateway/llm/azure-openai/v1/responses", deps.AIGateway.RelayAzureOpenAIResponses)
	protected.POST("/ai-gateway/llm/azure-openai/v1/embeddings", deps.AIGateway.RelayAzureOpenAIEmbeddings)
	protected.POST("/ai-gateway/llm/azure-openai/v1/images/generations", deps.AIGateway.RelayAzureOpenAIImageGenerations)
	protected.POST("/ai-gateway/llm/azure-openai/v1/images/edits", deps.AIGateway.RelayAzureOpenAIImageEdits)
	protected.POST("/ai-gateway/llm/azure-openai/v1/images/variations", deps.AIGateway.RelayAzureOpenAIImageVariations)
	protected.POST("/ai-gateway/llm/azure-openai/v1/audio/speech", deps.AIGateway.RelayAzureOpenAIAudioSpeech)
	protected.POST("/ai-gateway/llm/azure-openai/v1/audio/transcriptions", deps.AIGateway.RelayAzureOpenAIAudioTranscriptions)
	protected.POST("/ai-gateway/llm/azure-openai/v1/audio/translations", deps.AIGateway.RelayAzureOpenAIAudioTranslations)
	protected.GET("/ai-gateway/llm/gemini/v1beta/models", deps.AIGateway.RelayGeminiModels)
	protected.POST("/ai-gateway/llm/gemini/v1beta/interactions", deps.AIGateway.RelayGeminiInteractions)
	protected.POST("/ai-gateway/llm/gemini/v1beta/models/*modelAction", deps.AIGateway.RelayGeminiModelAction)
	protected.POST("/ai-gateway/llm/cohere/v2/rerank", deps.AIGateway.RelayCohereRerank)
	protected.GET("/ai-gateway/llm/anthropic/v1/models", deps.AIGateway.RelayAnthropicModels)
	protected.POST("/ai-gateway/llm/anthropic/v1/messages", deps.AIGateway.RelayAnthropicMessages)
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

	protected.GET("/extensions/runtime", deps.Plugins.ListRuntimeExtensions)
	protected.GET("/extensions/resource", deps.Plugins.ListResourceExtensions)
	protected.GET("/extensions/metrics", deps.Plugins.ListMetricExtensions)
	protected.GET("/extensions/alerts", deps.Plugins.ListAlertExtensions)
	protected.GET("/extensions/ai", deps.Plugins.ListAIExtensions)
	protected.GET("/extensions/auth", deps.Plugins.ListAuthExtensions)
	protected.GET("/extensions/identity", deps.Plugins.ListIdentityExtensions)
	protected.GET("/extensions/ui", deps.Plugins.ListUIExtensions)
}

func registerSettingsRoutes(protected gin.IRoutes, deps Dependencies) {
	protected.GET("/settings/identity", deps.Settings.GetIdentitySettings)
	protected.PUT("/settings/identity/providers", deps.Settings.UpdateLoginProvidersSettings)
	protected.GET("/settings/monitoring", deps.Settings.GetMonitoringSettings)
	protected.PUT("/settings/monitoring/prometheus", deps.Settings.UpdatePrometheusSettings)
	protected.GET("/settings/ai", deps.Settings.GetAISettings)
	protected.PUT("/settings/ai/workbench-model", deps.Settings.UpdateAIWorkbenchModelSettings)
	protected.PUT("/settings/ai/skills", deps.Settings.UpdateAISkills)
	protected.GET("/settings/branding", deps.Settings.GetBrandingSettings)
	protected.PUT("/settings/branding", deps.Settings.UpdateBrandingSettings)
	protected.POST("/settings/branding/upload", deps.Settings.UploadBrandingAsset)
}
