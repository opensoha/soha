package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/kubecrux/kubecrux/internal/api/dto"
	apiMiddleware "github.com/kubecrux/kubecrux/internal/api/middleware"
	apiresponse "github.com/kubecrux/kubecrux/internal/api/response"
	domainalert "github.com/kubecrux/kubecrux/internal/domain/alert"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
)

type MonitoringService interface {
	Summary(context.Context, domainidentity.Principal) (domainalert.Summary, error)
	ListAlerts(context.Context, domainidentity.Principal, domainalert.Filter) ([]domainalert.Instance, error)
	GetAlert(context.Context, domainidentity.Principal, string) (domainalert.Instance, error)
	UpdateOwnership(context.Context, domainidentity.Principal, string, domainalert.OwnershipInput) (domainalert.Instance, error)
	Acknowledge(context.Context, domainidentity.Principal, string, string, string) (domainalert.Instance, error)
	ListChannels(context.Context, domainidentity.Principal) ([]domainalert.NotificationChannel, error)
	CreateChannel(context.Context, domainidentity.Principal, domainalert.ChannelInput) (domainalert.NotificationChannel, error)
	UpdateChannel(context.Context, domainidentity.Principal, string, domainalert.ChannelInput) (domainalert.NotificationChannel, error)
	ListRoutes(context.Context, domainidentity.Principal) ([]domainalert.AlertRoute, error)
	CreateRoute(context.Context, domainidentity.Principal, domainalert.RouteInput) (domainalert.AlertRoute, error)
	UpdateRoute(context.Context, domainidentity.Principal, string, domainalert.RouteInput) (domainalert.AlertRoute, error)
	ListSilences(context.Context, domainidentity.Principal) ([]domainalert.AlertSilence, error)
	CreateSilence(context.Context, domainidentity.Principal, domainalert.SilenceInput) (domainalert.AlertSilence, error)
	UpdateSilence(context.Context, domainidentity.Principal, string, domainalert.SilenceInput) (domainalert.AlertSilence, error)
	ListDeliveryLogs(context.Context, domainidentity.Principal, domainalert.DeliveryFilter) ([]domainalert.DeliveryLog, error)
	ValidateWebhookToken(string) error
	Ingest(context.Context, domainalert.IngestRequest) (int, error)
	ListRules(context.Context, domainidentity.Principal) ([]domainalert.AlertRule, error)
	GetRule(context.Context, domainidentity.Principal, string) (domainalert.AlertRule, error)
	CreateRule(context.Context, domainidentity.Principal, domainalert.AlertRuleInput) (domainalert.AlertRule, error)
	UpdateRule(context.Context, domainidentity.Principal, string, domainalert.AlertRuleInput) (domainalert.AlertRule, error)
	TestRule(context.Context, domainidentity.Principal, domainalert.AlertRuleInput) (domainalert.RuleTestResult, error)
	ListRuleRuns(context.Context, domainidentity.Principal, domainalert.AlertRuleRunFilter) ([]domainalert.AlertRuleRun, error)
	ListEvents(context.Context, domainidentity.Principal, domainalert.AlertEventFilter) ([]domainalert.AlertEvent, error)
	GetEvent(context.Context, domainidentity.Principal, string) (domainalert.AlertEvent, error)
	AcknowledgeEvent(context.Context, domainidentity.Principal, string) (domainalert.AlertEvent, error)
	ResolveEvent(context.Context, domainidentity.Principal, string) (domainalert.AlertEvent, error)
	HealEvent(context.Context, domainidentity.Principal, string, string) (domainalert.HealingRun, error)
	GetHealingRun(context.Context, domainidentity.Principal, string) (domainalert.HealingRun, error)
	ApproveHealingRun(context.Context, domainidentity.Principal, string, string) (domainalert.HealingRun, error)
	RejectHealingRun(context.Context, domainidentity.Principal, string, string) (domainalert.HealingRun, error)
	RetryHealingRun(context.Context, domainidentity.Principal, string) (domainalert.HealingRun, error)
	ListNotificationPolicies(context.Context, domainidentity.Principal) ([]domainalert.NotificationPolicy, error)
	CreateNotificationPolicy(context.Context, domainidentity.Principal, domainalert.NotificationPolicyInput) (domainalert.NotificationPolicy, error)
	UpdateNotificationPolicy(context.Context, domainidentity.Principal, string, domainalert.NotificationPolicyInput) (domainalert.NotificationPolicy, error)
	PreviewNotificationPolicy(context.Context, domainidentity.Principal, string, string) ([]map[string]any, error)
	ListNotificationTemplates(context.Context, domainidentity.Principal) ([]domainalert.NotificationTemplate, error)
	CreateNotificationTemplate(context.Context, domainidentity.Principal, domainalert.NotificationTemplateInput) (domainalert.NotificationTemplate, error)
	UpdateNotificationTemplate(context.Context, domainidentity.Principal, string, domainalert.NotificationTemplateInput) (domainalert.NotificationTemplate, error)
	ListHealingPolicies(context.Context, domainidentity.Principal) ([]domainalert.HealingPolicy, error)
	CreateHealingPolicy(context.Context, domainidentity.Principal, domainalert.HealingPolicyInput) (domainalert.HealingPolicy, error)
	UpdateHealingPolicy(context.Context, domainidentity.Principal, string, domainalert.HealingPolicyInput) (domainalert.HealingPolicy, error)
	ListHealingRuns(context.Context, domainidentity.Principal, domainalert.HealingRunFilter) ([]domainalert.HealingRun, error)
	ListOnCallSchedules(context.Context, domainidentity.Principal) ([]domainalert.OnCallSchedule, error)
	CreateOnCallSchedule(context.Context, domainidentity.Principal, domainalert.OnCallScheduleInput) (domainalert.OnCallSchedule, error)
	UpdateOnCallSchedule(context.Context, domainidentity.Principal, string, domainalert.OnCallScheduleInput) (domainalert.OnCallSchedule, error)
	ListOnCallRotations(context.Context, domainidentity.Principal) ([]domainalert.OnCallRotation, error)
	CreateOnCallRotation(context.Context, domainidentity.Principal, domainalert.OnCallRotationInput) (domainalert.OnCallRotation, error)
	UpdateOnCallRotation(context.Context, domainidentity.Principal, string, domainalert.OnCallRotationInput) (domainalert.OnCallRotation, error)
	ListOnCallEscalationPolicies(context.Context, domainidentity.Principal) ([]domainalert.OnCallEscalationPolicy, error)
	CreateOnCallEscalationPolicy(context.Context, domainidentity.Principal, domainalert.OnCallEscalationPolicyInput) (domainalert.OnCallEscalationPolicy, error)
	UpdateOnCallEscalationPolicy(context.Context, domainidentity.Principal, string, domainalert.OnCallEscalationPolicyInput) (domainalert.OnCallEscalationPolicy, error)
	ListOnCallAssignmentRules(context.Context, domainidentity.Principal) ([]domainalert.OnCallAssignmentRule, error)
	CreateOnCallAssignmentRule(context.Context, domainidentity.Principal, domainalert.OnCallAssignmentRuleInput) (domainalert.OnCallAssignmentRule, error)
	UpdateOnCallAssignmentRule(context.Context, domainidentity.Principal, string, domainalert.OnCallAssignmentRuleInput) (domainalert.OnCallAssignmentRule, error)
	GetCurrentOnCall(context.Context, domainidentity.Principal, string) (map[string]any, error)
	ResolveOnCall(context.Context, domainidentity.Principal, domainalert.OnCallResolveInput) (map[string]any, error)
	ListOnCallTasks(context.Context, domainidentity.Principal, int) ([]domainalert.OnCallTask, error)
}

type MonitoringHandler struct {
	service MonitoringService
}

func NewMonitoringHandler(service MonitoringService) *MonitoringHandler {
	return &MonitoringHandler{service: service}
}

func (h *MonitoringHandler) Summary(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.Summary(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MonitoringHandler) ListAlerts(c *gin.Context) {
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

func (h *MonitoringHandler) GetAlert(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.GetAlert(c.Request.Context(), principal, c.Param("alertID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MonitoringHandler) UpdateAlertOwnership(c *gin.Context) {
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

func (h *MonitoringHandler) AcknowledgeAlert(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	item, err := h.service.Acknowledge(c.Request.Context(), principal, c.Param("alertID"), principal.UserID, principal.UserName)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MonitoringHandler) ListChannels(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListChannels(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *MonitoringHandler) CreateChannel(c *gin.Context) {
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

func (h *MonitoringHandler) UpdateChannel(c *gin.Context) {
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

func (h *MonitoringHandler) ListRoutes(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListRoutes(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *MonitoringHandler) CreateRoute(c *gin.Context) {
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

func (h *MonitoringHandler) UpdateRoute(c *gin.Context) {
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

func (h *MonitoringHandler) ListSilences(c *gin.Context) {
	principal := apiMiddleware.PrincipalFromContext(c)
	items, err := h.service.ListSilences(c.Request.Context(), principal)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *MonitoringHandler) CreateSilence(c *gin.Context) {
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

func (h *MonitoringHandler) UpdateSilence(c *gin.Context) {
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

func (h *MonitoringHandler) ListDeliveryLogs(c *gin.Context) {
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

func (h *MonitoringHandler) IngestWebhook(c *gin.Context) {
	token := strings.TrimSpace(c.GetHeader("X-Kubecrux-Webhook-Token"))
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
