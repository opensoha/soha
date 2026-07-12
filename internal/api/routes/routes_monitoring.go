package routes

import (
	"github.com/gin-gonic/gin"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
)

func registerMonitoringRoutes(protected gin.IRoutes, cfg cfgpkg.Config, deps Dependencies) {
	if !cfg.Modules.Monitoring.Enabled {
		return
	}
	registerAlertRoutes(protected, deps)
	registerNotificationRoutes(protected, deps)
	registerOnCallRoutes(protected, deps)
	registerAlertDeliveryRoutes(protected, deps)
}

func registerAlertRoutes(protected gin.IRoutes, deps Dependencies) {
	registerAlertResourceRoutes(protected, deps)
	registerAlertEventRoutes(protected, deps)
}

func registerAlertResourceRoutes(protected gin.IRoutes, deps Dependencies) {
	registerAlertStateIntegrationRoutes(protected, deps)
	registerAlertRuleRoutes(protected, deps)
}

func registerAlertStateIntegrationRoutes(protected gin.IRoutes, deps Dependencies) {
	protected.GET("/monitoring/summary", deps.Monitoring.Summary)
	protected.GET("/alerts", deps.Monitoring.ListAlerts)
	protected.GET("/alerts/:alertID", deps.Monitoring.GetAlert)
	protected.PUT("/alerts/:alertID/ownership", deps.Monitoring.UpdateAlertOwnership)
	protected.POST("/alerts/:alertID/acknowledge", deps.Monitoring.AcknowledgeAlert)
	protected.GET("/alert-integrations", deps.Monitoring.ListAlertIntegrations)
	protected.POST("/alert-integrations", deps.Monitoring.CreateAlertIntegration)
	protected.POST("/alert-integrations/test", deps.Monitoring.TestAlertIntegration)
	protected.GET("/alert-integrations/:integrationID", deps.Monitoring.GetAlertIntegration)
	protected.PUT("/alert-integrations/:integrationID", deps.Monitoring.UpdateAlertIntegration)
}

func registerAlertRuleRoutes(protected gin.IRoutes, deps Dependencies) {
	protected.GET("/alert-rules", deps.Monitoring.ListRules)
	protected.POST("/alert-rules", deps.Monitoring.CreateRule)
	protected.PUT("/alert-rules/:ruleID", deps.Monitoring.UpdateRule)
	protected.POST("/alert-rules/:ruleID/validate", deps.Monitoring.TestRule)
	protected.POST("/alert-rules/:ruleID/test", deps.Monitoring.TestRule)
	protected.GET("/alert-rule-runs", deps.Monitoring.ListRuleRuns)
}

func registerAlertEventRoutes(protected gin.IRoutes, deps Dependencies) {
	protected.GET("/alert-events", deps.Monitoring.ListEvents)
	protected.GET("/alert-events/:eventID", deps.Monitoring.GetEvent)
	protected.POST("/alert-events/:eventID/acknowledge", deps.Monitoring.AcknowledgeEvent)
	protected.POST("/alert-events/:eventID/resolve", deps.Monitoring.ResolveEvent)
	protected.POST("/alert-events/:eventID/heal", deps.Monitoring.HealEvent)
	protected.POST("/healing-runs/:runID/approve", deps.Monitoring.ApproveHealingRun)
	protected.POST("/healing-runs/:runID/reject", deps.Monitoring.RejectHealingRun)
	protected.POST("/healing-runs/:runID/retry", deps.Monitoring.RetryHealingRun)
}

func registerNotificationRoutes(protected gin.IRoutes, deps Dependencies) {
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
}

func registerOnCallRoutes(protected gin.IRoutes, deps Dependencies) {
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
}

func registerAlertDeliveryRoutes(protected gin.IRoutes, deps Dependencies) {
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
