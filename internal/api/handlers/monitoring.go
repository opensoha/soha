package handlers

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/kubecrux/kubecrux/internal/api/middleware"
	"github.com/kubecrux/kubecrux/internal/api/dto"
	apiresponse "github.com/kubecrux/kubecrux/internal/api/response"
	domainalert "github.com/kubecrux/kubecrux/internal/domain/alert"
)

type MonitoringService interface {
	Summary(context.Context) (domainalert.Summary, error)
	ListAlerts(context.Context, domainalert.Filter) ([]domainalert.Instance, error)
	GetAlert(context.Context, string) (domainalert.Instance, error)
	UpdateOwnership(context.Context, string, domainalert.OwnershipInput) (domainalert.Instance, error)
	Acknowledge(context.Context, string, string, string) (domainalert.Instance, error)
	ListChannels(context.Context) ([]domainalert.NotificationChannel, error)
	CreateChannel(context.Context, domainalert.ChannelInput) (domainalert.NotificationChannel, error)
	UpdateChannel(context.Context, string, domainalert.ChannelInput) (domainalert.NotificationChannel, error)
	ListRoutes(context.Context) ([]domainalert.AlertRoute, error)
	CreateRoute(context.Context, domainalert.RouteInput) (domainalert.AlertRoute, error)
	UpdateRoute(context.Context, string, domainalert.RouteInput) (domainalert.AlertRoute, error)
	ListSilences(context.Context) ([]domainalert.AlertSilence, error)
	CreateSilence(context.Context, domainalert.SilenceInput) (domainalert.AlertSilence, error)
	UpdateSilence(context.Context, string, domainalert.SilenceInput) (domainalert.AlertSilence, error)
	ListDeliveryLogs(context.Context, domainalert.DeliveryFilter) ([]domainalert.DeliveryLog, error)
	ValidateWebhookToken(string) error
	Ingest(context.Context, domainalert.IngestRequest) (int, error)
}

type MonitoringHandler struct {
	service MonitoringService
}

func NewMonitoringHandler(service MonitoringService) *MonitoringHandler {
	return &MonitoringHandler{service: service}
}

func (h *MonitoringHandler) Summary(c *gin.Context) {
	item, err := h.service.Summary(c.Request.Context())
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MonitoringHandler) ListAlerts(c *gin.Context) {
	items, err := h.service.ListAlerts(c.Request.Context(), domainalert.Filter{
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
	item, err := h.service.GetAlert(c.Request.Context(), c.Param("alertID"))
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
	item, err := h.service.UpdateOwnership(c.Request.Context(), c.Param("alertID"), domainalert.OwnershipInput{
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
	item, err := h.service.Acknowledge(c.Request.Context(), c.Param("alertID"), principal.UserID, principal.UserName)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *MonitoringHandler) ListChannels(c *gin.Context) {
	items, err := h.service.ListChannels(c.Request.Context())
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
	item, err := h.service.CreateChannel(c.Request.Context(), domainalert.ChannelInput{
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
	item, err := h.service.UpdateChannel(c.Request.Context(), c.Param("channelID"), domainalert.ChannelInput{
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
	items, err := h.service.ListRoutes(c.Request.Context())
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
	item, err := h.service.CreateRoute(c.Request.Context(), domainalert.RouteInput{
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
	item, err := h.service.UpdateRoute(c.Request.Context(), c.Param("routeID"), domainalert.RouteInput{
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
	items, err := h.service.ListSilences(c.Request.Context())
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
	item, err := h.service.CreateSilence(c.Request.Context(), domainalert.SilenceInput{
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
	item, err := h.service.UpdateSilence(c.Request.Context(), c.Param("silenceID"), domainalert.SilenceInput{
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
	items, err := h.service.ListDeliveryLogs(c.Request.Context(), domainalert.DeliveryFilter{
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
