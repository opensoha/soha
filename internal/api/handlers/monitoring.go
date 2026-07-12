package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/internal/api/dto"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

type AlertService interface {
	Summary(context.Context, domainidentity.Principal) (domainalert.Summary, error)
	ListAlerts(context.Context, domainidentity.Principal, domainalert.Filter) ([]domainalert.Instance, error)
	GetAlert(context.Context, domainidentity.Principal, string) (domainalert.Instance, error)
	UpdateOwnership(context.Context, domainidentity.Principal, string, domainalert.OwnershipInput) (domainalert.Instance, error)
	Acknowledge(context.Context, domainidentity.Principal, string, string, string) (domainalert.Instance, error)
}

type ChannelService interface {
	ListChannels(context.Context, domainidentity.Principal) ([]domainalert.NotificationChannel, error)
	CreateChannel(context.Context, domainidentity.Principal, domainalert.ChannelInput) (domainalert.NotificationChannel, error)
	UpdateChannel(context.Context, domainidentity.Principal, string, domainalert.ChannelInput) (domainalert.NotificationChannel, error)
}

type AlertRouteService interface {
	ListRoutes(context.Context, domainidentity.Principal) ([]domainalert.AlertRoute, error)
	CreateRoute(context.Context, domainidentity.Principal, domainalert.RouteInput) (domainalert.AlertRoute, error)
	UpdateRoute(context.Context, domainidentity.Principal, string, domainalert.RouteInput) (domainalert.AlertRoute, error)
}

type SilenceService interface {
	ListSilences(context.Context, domainidentity.Principal) ([]domainalert.AlertSilence, error)
	CreateSilence(context.Context, domainidentity.Principal, domainalert.SilenceInput) (domainalert.AlertSilence, error)
	UpdateSilence(context.Context, domainidentity.Principal, string, domainalert.SilenceInput) (domainalert.AlertSilence, error)
}

type DeliveryLogService interface {
	ListDeliveryLogs(context.Context, domainidentity.Principal, domainalert.DeliveryFilter) ([]domainalert.DeliveryLog, error)
}

type WebhookService interface {
	ValidateWebhookToken(string) error
	Ingest(context.Context, domainalert.IngestRequest) (int, error)
}

type AlertIntegrationService interface {
	ListAlertIntegrations(context.Context, domainidentity.Principal) ([]domainalert.AlertIntegration, error)
	GetAlertIntegration(context.Context, domainidentity.Principal, string) (domainalert.AlertIntegration, error)
	CreateAlertIntegration(context.Context, domainidentity.Principal, domainalert.AlertIntegrationInput) (domainalert.AlertIntegration, error)
	UpdateAlertIntegration(context.Context, domainidentity.Principal, string, domainalert.AlertIntegrationInput) (domainalert.AlertIntegration, error)
	TestAlertIntegration(context.Context, domainidentity.Principal, domainalert.AlertIntegrationTestInput) (domainalert.AlertIntegrationTestResult, error)
	IngestAlertIntegration(context.Context, string, string, map[string]any) (int, error)
}

type AlertRuleService interface {
	ListRules(context.Context, domainidentity.Principal) ([]domainalert.AlertRule, error)
	GetRule(context.Context, domainidentity.Principal, string) (domainalert.AlertRule, error)
	CreateRule(context.Context, domainidentity.Principal, domainalert.AlertRuleInput) (domainalert.AlertRule, error)
	UpdateRule(context.Context, domainidentity.Principal, string, domainalert.AlertRuleInput) (domainalert.AlertRule, error)
	TestRule(context.Context, domainidentity.Principal, domainalert.AlertRuleInput) (domainalert.RuleTestResult, error)
	ListRuleRuns(context.Context, domainidentity.Principal, domainalert.AlertRuleRunFilter) ([]domainalert.AlertRuleRun, error)
}

type AlertEventService interface {
	ListEvents(context.Context, domainidentity.Principal, domainalert.AlertEventFilter) ([]domainalert.AlertEvent, error)
	GetEvent(context.Context, domainidentity.Principal, string) (domainalert.AlertEvent, error)
	AcknowledgeEvent(context.Context, domainidentity.Principal, string) (domainalert.AlertEvent, error)
	ResolveEvent(context.Context, domainidentity.Principal, string) (domainalert.AlertEvent, error)
	HealEvent(context.Context, domainidentity.Principal, string, string) (domainalert.HealingRun, error)
}

type HealingRunService interface {
	GetHealingRun(context.Context, domainidentity.Principal, string) (domainalert.HealingRun, error)
	ApproveHealingRun(context.Context, domainidentity.Principal, string, string) (domainalert.HealingRun, error)
	RejectHealingRun(context.Context, domainidentity.Principal, string, string) (domainalert.HealingRun, error)
	RetryHealingRun(context.Context, domainidentity.Principal, string) (domainalert.HealingRun, error)
	ListHealingRuns(context.Context, domainidentity.Principal, domainalert.HealingRunFilter) ([]domainalert.HealingRun, error)
}

type NotificationPolicyService interface {
	ListNotificationPolicies(context.Context, domainidentity.Principal) ([]domainalert.NotificationPolicy, error)
	CreateNotificationPolicy(context.Context, domainidentity.Principal, domainalert.NotificationPolicyInput) (domainalert.NotificationPolicy, error)
	UpdateNotificationPolicy(context.Context, domainidentity.Principal, string, domainalert.NotificationPolicyInput) (domainalert.NotificationPolicy, error)
	PreviewNotificationPolicy(context.Context, domainidentity.Principal, string, string) ([]map[string]any, error)
}

type NotificationTemplateService interface {
	ListNotificationTemplates(context.Context, domainidentity.Principal) ([]domainalert.NotificationTemplate, error)
	CreateNotificationTemplate(context.Context, domainidentity.Principal, domainalert.NotificationTemplateInput) (domainalert.NotificationTemplate, error)
	UpdateNotificationTemplate(context.Context, domainidentity.Principal, string, domainalert.NotificationTemplateInput) (domainalert.NotificationTemplate, error)
}

type HealingPolicyService interface {
	ListHealingPolicies(context.Context, domainidentity.Principal) ([]domainalert.HealingPolicy, error)
	CreateHealingPolicy(context.Context, domainidentity.Principal, domainalert.HealingPolicyInput) (domainalert.HealingPolicy, error)
	UpdateHealingPolicy(context.Context, domainidentity.Principal, string, domainalert.HealingPolicyInput) (domainalert.HealingPolicy, error)
}

type OnCallScheduleService interface {
	ListOnCallSchedules(context.Context, domainidentity.Principal) ([]domainalert.OnCallSchedule, error)
	CreateOnCallSchedule(context.Context, domainidentity.Principal, domainalert.OnCallScheduleInput) (domainalert.OnCallSchedule, error)
	UpdateOnCallSchedule(context.Context, domainidentity.Principal, string, domainalert.OnCallScheduleInput) (domainalert.OnCallSchedule, error)
}

type OnCallRotationService interface {
	ListOnCallRotations(context.Context, domainidentity.Principal) ([]domainalert.OnCallRotation, error)
	CreateOnCallRotation(context.Context, domainidentity.Principal, domainalert.OnCallRotationInput) (domainalert.OnCallRotation, error)
	UpdateOnCallRotation(context.Context, domainidentity.Principal, string, domainalert.OnCallRotationInput) (domainalert.OnCallRotation, error)
}

type OnCallEscalationService interface {
	ListOnCallEscalationPolicies(context.Context, domainidentity.Principal) ([]domainalert.OnCallEscalationPolicy, error)
	CreateOnCallEscalationPolicy(context.Context, domainidentity.Principal, domainalert.OnCallEscalationPolicyInput) (domainalert.OnCallEscalationPolicy, error)
	UpdateOnCallEscalationPolicy(context.Context, domainidentity.Principal, string, domainalert.OnCallEscalationPolicyInput) (domainalert.OnCallEscalationPolicy, error)
}

type OnCallAssignmentService interface {
	ListOnCallAssignmentRules(context.Context, domainidentity.Principal) ([]domainalert.OnCallAssignmentRule, error)
	CreateOnCallAssignmentRule(context.Context, domainidentity.Principal, domainalert.OnCallAssignmentRuleInput) (domainalert.OnCallAssignmentRule, error)
	UpdateOnCallAssignmentRule(context.Context, domainidentity.Principal, string, domainalert.OnCallAssignmentRuleInput) (domainalert.OnCallAssignmentRule, error)
}

type OnCallRuntimeService interface {
	GetCurrentOnCall(context.Context, domainidentity.Principal, string) (map[string]any, error)
	ResolveOnCall(context.Context, domainidentity.Principal, domainalert.OnCallResolveInput) (map[string]any, error)
	ListOnCallTasks(context.Context, domainidentity.Principal, int) ([]domainalert.OnCallTask, error)
}

type MonitoringHandler struct {
	alertHandler
	channelHandler
	alertRouteHandler
	silenceHandler
	deliveryLogHandler
	webhookHandler
	integrationHandler
	ruleHandler
	eventHandler
	healingRunHandler
	notificationPolicyHandler
	notificationTemplateHandler
	healingPolicyHandler
	onCallScheduleHandler
	onCallRotationHandler
	onCallEscalationHandler
	onCallAssignmentHandler
	onCallRuntimeHandler
}

type MonitoringDependencies struct {
	Alerts                AlertService
	Channels              ChannelService
	Routes                AlertRouteService
	Silences              SilenceService
	DeliveryLogs          DeliveryLogService
	Webhooks              WebhookService
	Integrations          AlertIntegrationService
	Rules                 AlertRuleService
	Events                AlertEventService
	HealingRuns           HealingRunService
	NotificationPolicies  NotificationPolicyService
	NotificationTemplates NotificationTemplateService
	HealingPolicies       HealingPolicyService
	OnCallSchedules       OnCallScheduleService
	OnCallRotations       OnCallRotationService
	OnCallEscalations     OnCallEscalationService
	OnCallAssignments     OnCallAssignmentService
	OnCallRuntime         OnCallRuntimeService
}

type alertHandler struct{ service AlertService }
type channelHandler struct{ service ChannelService }
type alertRouteHandler struct{ service AlertRouteService }
type silenceHandler struct{ service SilenceService }
type deliveryLogHandler struct{ service DeliveryLogService }
type webhookHandler struct{ service WebhookService }
type integrationHandler struct{ service AlertIntegrationService }
type ruleHandler struct{ service AlertRuleService }
type eventHandler struct{ service AlertEventService }
type healingRunHandler struct{ service HealingRunService }
type notificationPolicyHandler struct{ service NotificationPolicyService }
type notificationTemplateHandler struct{ service NotificationTemplateService }
type healingPolicyHandler struct{ service HealingPolicyService }
type onCallScheduleHandler struct{ service OnCallScheduleService }
type onCallRotationHandler struct{ service OnCallRotationService }
type onCallEscalationHandler struct{ service OnCallEscalationService }
type onCallAssignmentHandler struct{ service OnCallAssignmentService }
type onCallRuntimeHandler struct{ service OnCallRuntimeService }

func NewMonitoringHandler(deps MonitoringDependencies) *MonitoringHandler {
	return &MonitoringHandler{
		alertHandler:                alertHandler{service: deps.Alerts},
		channelHandler:              channelHandler{service: deps.Channels},
		alertRouteHandler:           alertRouteHandler{service: deps.Routes},
		silenceHandler:              silenceHandler{service: deps.Silences},
		deliveryLogHandler:          deliveryLogHandler{service: deps.DeliveryLogs},
		webhookHandler:              webhookHandler{service: deps.Webhooks},
		integrationHandler:          integrationHandler{service: deps.Integrations},
		ruleHandler:                 ruleHandler{service: deps.Rules},
		eventHandler:                eventHandler{service: deps.Events},
		healingRunHandler:           healingRunHandler{service: deps.HealingRuns},
		notificationPolicyHandler:   notificationPolicyHandler{service: deps.NotificationPolicies},
		notificationTemplateHandler: notificationTemplateHandler{service: deps.NotificationTemplates},
		healingPolicyHandler:        healingPolicyHandler{service: deps.HealingPolicies},
		onCallScheduleHandler:       onCallScheduleHandler{service: deps.OnCallSchedules},
		onCallRotationHandler:       onCallRotationHandler{service: deps.OnCallRotations},
		onCallEscalationHandler:     onCallEscalationHandler{service: deps.OnCallEscalations},
		onCallAssignmentHandler:     onCallAssignmentHandler{service: deps.OnCallAssignments},
		onCallRuntimeHandler:        onCallRuntimeHandler{service: deps.OnCallRuntime},
	}
}

func (h *alertHandler) Summary(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.Summary(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *alertHandler) ListAlerts(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListAlerts(c.Request.Context(), principal, domainalert.Filter{
		Status:    c.Query("status"),
		ClusterID: c.Query("clusterId"),
		Limit:     parseLimit(c.Query("limit"), 50),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *alertHandler) GetAlert(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetAlert(c.Request.Context(), principal, c.Param("alertID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *alertHandler) UpdateAlertOwnership(c *gin.Context) {
	var req dto.AlertOwnershipRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid alert ownership payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateOwnership(c.Request.Context(), principal, c.Param("alertID"), domainalert.OwnershipInput{
		OwnerTeam: req.OwnerTeam,
		Assignee:  req.Assignee,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *alertHandler) AcknowledgeAlert(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.Acknowledge(c.Request.Context(), principal, c.Param("alertID"), principal.UserID, principal.UserName)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *channelHandler) ListChannels(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListChannels(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *channelHandler) CreateChannel(c *gin.Context) {
	var req dto.NotificationChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid notification channel payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateChannel(c.Request.Context(), principal, domainalert.ChannelInput{
		ID:          req.ID,
		Name:        req.Name,
		ChannelType: req.ChannelType,
		Enabled:     req.Enabled,
		Config:      req.Config,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *channelHandler) UpdateChannel(c *gin.Context) {
	var req dto.NotificationChannelRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid notification channel payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateChannel(c.Request.Context(), principal, c.Param("channelID"), domainalert.ChannelInput{
		ID:          req.ID,
		Name:        req.Name,
		ChannelType: req.ChannelType,
		Enabled:     req.Enabled,
		Config:      req.Config,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *alertRouteHandler) ListRoutes(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListRoutes(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *alertRouteHandler) CreateRoute(c *gin.Context) {
	var req dto.AlertRouteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid alert route payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateRoute(c.Request.Context(), principal, domainalert.RouteInput{
		ID:         req.ID,
		Name:       req.Name,
		Matchers:   req.Matchers,
		ChannelIDs: req.ChannelIDs,
		Enabled:    req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *alertRouteHandler) UpdateRoute(c *gin.Context) {
	var req dto.AlertRouteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid alert route payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateRoute(c.Request.Context(), principal, c.Param("routeID"), domainalert.RouteInput{
		ID:         req.ID,
		Name:       req.Name,
		Matchers:   req.Matchers,
		ChannelIDs: req.ChannelIDs,
		Enabled:    req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *silenceHandler) ListSilences(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListSilences(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *silenceHandler) CreateSilence(c *gin.Context) {
	var req dto.AlertSilenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid alert silence payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateSilence(c.Request.Context(), principal, domainalert.SilenceInput{
		ID:       req.ID,
		Name:     req.Name,
		Matchers: req.Matchers,
		Reason:   req.Reason,
		StartsAt: req.StartsAt,
		EndsAt:   req.EndsAt,
		Enabled:  req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *silenceHandler) UpdateSilence(c *gin.Context) {
	var req dto.AlertSilenceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid alert silence payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateSilence(c.Request.Context(), principal, c.Param("silenceID"), domainalert.SilenceInput{
		ID:       req.ID,
		Name:     req.Name,
		Matchers: req.Matchers,
		Reason:   req.Reason,
		StartsAt: req.StartsAt,
		EndsAt:   req.EndsAt,
		Enabled:  req.Enabled,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *deliveryLogHandler) ListDeliveryLogs(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListDeliveryLogs(c.Request.Context(), principal, domainalert.DeliveryFilter{
		AlertID: c.Query("alertId"),
		Status:  c.Query("status"),
		Limit:   parseLimit(c.Query("limit"), 100),
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *webhookHandler) IngestWebhook(c *gin.Context) {
	token := strings.TrimSpace(c.GetHeader("X-Soha-Webhook-Token"))
	if token == "" {
		token = strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
	}
	if err := h.service.ValidateWebhookToken(token); err != nil {
		writeError(c, err)
		return
	}

	var req dto.IngestAlertsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid alert webhook payload")
		return
	}
	alerts := make([]domainalert.IngestAlert, 0, len(req.Alerts))
	for _, item := range req.Alerts {
		alerts = append(alerts, domainalert.IngestAlert{
			Fingerprint:  item.Fingerprint,
			Title:        item.Title,
			Summary:      item.Summary,
			Severity:     item.Severity,
			Status:       item.Status,
			ClusterID:    item.ClusterID,
			Namespace:    item.Namespace,
			Labels:       item.Labels,
			Annotations:  item.Annotations,
			Receiver:     item.Receiver,
			GeneratorURL: item.GeneratorURL,
			StartsAt:     item.StartsAt,
			EndsAt:       item.EndsAt,
		})
	}
	count, err := h.service.Ingest(c.Request.Context(), domainalert.IngestRequest{Source: req.Source, Alerts: alerts})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusAccepted, gin.H{"accepted": count})
}

func (h *integrationHandler) ListAlertIntegrations(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListAlertIntegrations(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *integrationHandler) GetAlertIntegration(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetAlertIntegration(c.Request.Context(), principal, c.Param("integrationID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *integrationHandler) CreateAlertIntegration(c *gin.Context) {
	var req dto.AlertIntegrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid alert integration payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.CreateAlertIntegration(c.Request.Context(), principal, alertIntegrationInput(req))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *integrationHandler) UpdateAlertIntegration(c *gin.Context) {
	var req dto.AlertIntegrationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid alert integration payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.UpdateAlertIntegration(c.Request.Context(), principal, c.Param("integrationID"), alertIntegrationInput(req))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *integrationHandler) TestAlertIntegration(c *gin.Context) {
	var req dto.AlertIntegrationTestRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid alert integration test payload")
		return
	}
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.TestAlertIntegration(c.Request.Context(), principal, domainalert.AlertIntegrationTestInput{
		IntegrationType: req.IntegrationType,
		LabelMapping:    req.LabelMapping,
		DedupeConfig:    req.DedupeConfig,
		Payload:         req.Payload,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *integrationHandler) IngestIntegrationWebhook(c *gin.Context) {
	token := strings.TrimSpace(c.GetHeader("X-Soha-Webhook-Token"))
	if token == "" {
		token = strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer "))
	}
	if token == "" {
		token = strings.TrimSpace(c.Query("token"))
	}
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid alert integration webhook payload")
		return
	}
	count, err := h.service.IngestAlertIntegration(c.Request.Context(), c.Param("integrationID"), token, payload)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.JSON(c, http.StatusAccepted, gin.H{"accepted": count})
}

func alertIntegrationInput(req dto.AlertIntegrationRequest) domainalert.AlertIntegrationInput {
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}
	return domainalert.AlertIntegrationInput{
		ID:              req.ID,
		Name:            req.Name,
		IntegrationType: req.IntegrationType,
		Description:     req.Description,
		Token:           req.Token,
		LabelMapping:    req.LabelMapping,
		DedupeConfig:    req.DedupeConfig,
		Enabled:         enabled,
	}
}
