package monitoring

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/kubecrux/kubecrux/internal/application/access"
	domainalert "github.com/kubecrux/kubecrux/internal/domain/alert"
	domainevent "github.com/kubecrux/kubecrux/internal/domain/event"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	"github.com/kubecrux/kubecrux/internal/platform/apperrors"
)

type Repository interface {
	Upsert(context.Context, string, []domainalert.IngestAlert) ([]domainalert.Instance, error)
	List(context.Context, domainalert.Filter) ([]domainalert.Instance, error)
	Get(context.Context, string) (domainalert.Instance, error)
	UpdateOwnership(context.Context, string, domainalert.OwnershipInput) (domainalert.Instance, error)
	Acknowledge(context.Context, string, string, string) (domainalert.Instance, error)
	Summary(context.Context) (domainalert.Summary, error)
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
	CreateDeliveryLog(context.Context, domainalert.DeliveryLog) error
}

type EventWriter interface {
	Create(context.Context, domainevent.Envelope) error
}

type AlertAutomationHandler interface {
	HandleAlertAutomation(context.Context, domainalert.Instance) error
}

type Service struct {
	repo         Repository
	events       EventWriter
	webhookToken string
	enabled      bool
	httpClient   *http.Client
	automation   AlertAutomationHandler
}

func New(repo Repository, events EventWriter, enabled bool, webhookToken string) *Service {
	return &Service{repo: repo, events: events, enabled: enabled, webhookToken: webhookToken, httpClient: &http.Client{Timeout: 8 * time.Second}}
}

func (s *Service) SetAutomation(handler AlertAutomationHandler) {
	s.automation = handler
}

func (s *Service) Summary(ctx context.Context, principal domainidentity.Principal) (domainalert.Summary, error) {
	if err := authorize(principal, appaccess.PermObserveMonitoringView); err != nil {
		return domainalert.Summary{}, err
	}
	if s.repo == nil {
		return domainalert.Summary{}, nil
	}
	return s.repo.Summary(ctx)
}

func (s *Service) ListAlerts(ctx context.Context, principal domainidentity.Principal, filter domainalert.Filter) ([]domainalert.Instance, error) {
	if err := authorize(principal, appaccess.PermObserveAlertsView); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return []domainalert.Instance{}, nil
	}
	return s.repo.List(ctx, filter)
}

func (s *Service) GetAlert(ctx context.Context, principal domainidentity.Principal, alertID string) (domainalert.Instance, error) {
	if err := authorize(principal, appaccess.PermObserveAlertsView); err != nil {
		return domainalert.Instance{}, err
	}
	if s.repo == nil {
		return domainalert.Instance{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	item, err := s.repo.Get(ctx, strings.TrimSpace(alertID))
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return domainalert.Instance{}, fmt.Errorf("%w: %s", apperrors.ErrNotFound, strings.TrimSpace(alertID))
		}
		return domainalert.Instance{}, err
	}
	return item, nil
}

func (s *Service) UpdateOwnership(ctx context.Context, principal domainidentity.Principal, alertID string, input domainalert.OwnershipInput) (domainalert.Instance, error) {
	if err := authorize(principal, appaccess.PermObserveAlertsAssign); err != nil {
		return domainalert.Instance{}, err
	}
	if s.repo == nil {
		return domainalert.Instance{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(alertID) == "" {
		return domainalert.Instance{}, fmt.Errorf("%w: alert id is required", apperrors.ErrInvalidArgument)
	}
	item, err := s.repo.UpdateOwnership(ctx, alertID, input)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return domainalert.Instance{}, fmt.Errorf("%w: %s", apperrors.ErrNotFound, strings.TrimSpace(alertID))
		}
		return domainalert.Instance{}, err
	}
	return item, nil
}

func (s *Service) Acknowledge(ctx context.Context, principal domainidentity.Principal, alertID, userID, userName string) (domainalert.Instance, error) {
	if err := authorize(principal, appaccess.PermObserveAlertsAcknowledge); err != nil {
		return domainalert.Instance{}, err
	}
	if s.repo == nil {
		return domainalert.Instance{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(alertID) == "" {
		return domainalert.Instance{}, fmt.Errorf("%w: alert id is required", apperrors.ErrInvalidArgument)
	}
	item, err := s.repo.Acknowledge(ctx, alertID, userID, userName)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return domainalert.Instance{}, fmt.Errorf("%w: %s", apperrors.ErrNotFound, strings.TrimSpace(alertID))
		}
		return domainalert.Instance{}, err
	}
	return item, nil
}

func (s *Service) ListChannels(ctx context.Context, principal domainidentity.Principal) ([]domainalert.NotificationChannel, error) {
	if err := authorize(principal, appaccess.PermObserveNotificationsView); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return []domainalert.NotificationChannel{}, nil
	}
	return s.repo.ListChannels(ctx)
}

func (s *Service) CreateChannel(ctx context.Context, principal domainidentity.Principal, input domainalert.ChannelInput) (domainalert.NotificationChannel, error) {
	if err := authorize(principal, appaccess.PermObserveNotificationsManage); err != nil {
		return domainalert.NotificationChannel{}, err
	}
	if s.repo == nil {
		return domainalert.NotificationChannel{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if err := validateChannelInput(input); err != nil {
		return domainalert.NotificationChannel{}, err
	}
	return s.repo.CreateChannel(ctx, input)
}

func (s *Service) UpdateChannel(ctx context.Context, principal domainidentity.Principal, channelID string, input domainalert.ChannelInput) (domainalert.NotificationChannel, error) {
	if err := authorize(principal, appaccess.PermObserveNotificationsManage); err != nil {
		return domainalert.NotificationChannel{}, err
	}
	if s.repo == nil {
		return domainalert.NotificationChannel{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(channelID) == "" {
		return domainalert.NotificationChannel{}, fmt.Errorf("%w: notification channel id is required", apperrors.ErrInvalidArgument)
	}
	if err := validateChannelInput(input); err != nil {
		return domainalert.NotificationChannel{}, err
	}
	item, err := s.repo.UpdateChannel(ctx, channelID, input)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return domainalert.NotificationChannel{}, fmt.Errorf("%w: %s", apperrors.ErrNotFound, strings.TrimSpace(channelID))
		}
		return domainalert.NotificationChannel{}, err
	}
	return item, nil
}

func (s *Service) ListRoutes(ctx context.Context, principal domainidentity.Principal) ([]domainalert.AlertRoute, error) {
	if err := authorize(principal, appaccess.PermObserveNotificationsView); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return []domainalert.AlertRoute{}, nil
	}
	return s.repo.ListRoutes(ctx)
}

func (s *Service) ListSilences(ctx context.Context, principal domainidentity.Principal) ([]domainalert.AlertSilence, error) {
	if err := authorize(principal, appaccess.PermObserveNotificationsView); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return []domainalert.AlertSilence{}, nil
	}
	return s.repo.ListSilences(ctx)
}

func (s *Service) CreateSilence(ctx context.Context, principal domainidentity.Principal, input domainalert.SilenceInput) (domainalert.AlertSilence, error) {
	if err := authorize(principal, appaccess.PermObserveNotificationsManage); err != nil {
		return domainalert.AlertSilence{}, err
	}
	if s.repo == nil {
		return domainalert.AlertSilence{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if err := validateSilenceInput(input); err != nil {
		return domainalert.AlertSilence{}, err
	}
	return s.repo.CreateSilence(ctx, input)
}

func (s *Service) UpdateSilence(ctx context.Context, principal domainidentity.Principal, silenceID string, input domainalert.SilenceInput) (domainalert.AlertSilence, error) {
	if err := authorize(principal, appaccess.PermObserveNotificationsManage); err != nil {
		return domainalert.AlertSilence{}, err
	}
	if s.repo == nil {
		return domainalert.AlertSilence{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(silenceID) == "" {
		return domainalert.AlertSilence{}, fmt.Errorf("%w: alert silence id is required", apperrors.ErrInvalidArgument)
	}
	if err := validateSilenceInput(input); err != nil {
		return domainalert.AlertSilence{}, err
	}
	item, err := s.repo.UpdateSilence(ctx, silenceID, input)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return domainalert.AlertSilence{}, fmt.Errorf("%w: %s", apperrors.ErrNotFound, strings.TrimSpace(silenceID))
		}
		return domainalert.AlertSilence{}, err
	}
	return item, nil
}

func (s *Service) ListDeliveryLogs(ctx context.Context, principal domainidentity.Principal, filter domainalert.DeliveryFilter) ([]domainalert.DeliveryLog, error) {
	if err := authorize(principal, appaccess.PermObserveAlertsView); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return []domainalert.DeliveryLog{}, nil
	}
	return s.repo.ListDeliveryLogs(ctx, filter)
}

func (s *Service) CreateRoute(ctx context.Context, principal domainidentity.Principal, input domainalert.RouteInput) (domainalert.AlertRoute, error) {
	if err := authorize(principal, appaccess.PermObserveNotificationsManage); err != nil {
		return domainalert.AlertRoute{}, err
	}
	if s.repo == nil {
		return domainalert.AlertRoute{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if err := validateRouteInput(input); err != nil {
		return domainalert.AlertRoute{}, err
	}
	return s.repo.CreateRoute(ctx, input)
}

func (s *Service) UpdateRoute(ctx context.Context, principal domainidentity.Principal, routeID string, input domainalert.RouteInput) (domainalert.AlertRoute, error) {
	if err := authorize(principal, appaccess.PermObserveNotificationsManage); err != nil {
		return domainalert.AlertRoute{}, err
	}
	if s.repo == nil {
		return domainalert.AlertRoute{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(routeID) == "" {
		return domainalert.AlertRoute{}, fmt.Errorf("%w: alert route id is required", apperrors.ErrInvalidArgument)
	}
	if err := validateRouteInput(input); err != nil {
		return domainalert.AlertRoute{}, err
	}
	item, err := s.repo.UpdateRoute(ctx, routeID, input)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return domainalert.AlertRoute{}, fmt.Errorf("%w: %s", apperrors.ErrNotFound, strings.TrimSpace(routeID))
		}
		return domainalert.AlertRoute{}, err
	}
	return item, nil
}

func (s *Service) ValidateWebhookToken(token string) error {
	if !s.enabled {
		return fmt.Errorf("%w: monitoring integrations are disabled", apperrors.ErrAccessDenied)
	}
	if strings.TrimSpace(s.webhookToken) == "" {
		return nil
	}
	if strings.TrimSpace(token) != strings.TrimSpace(s.webhookToken) {
		return fmt.Errorf("%w: invalid monitoring webhook token", apperrors.ErrUnauthorized)
	}
	return nil
}

func (s *Service) Ingest(ctx context.Context, req domainalert.IngestRequest) (int, error) {
	if s.repo == nil {
		return 0, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if !s.enabled {
		return 0, fmt.Errorf("%w: monitoring integrations are disabled", apperrors.ErrAccessDenied)
	}
	if len(req.Alerts) == 0 {
		return 0, fmt.Errorf("%w: alerts payload cannot be empty", apperrors.ErrInvalidArgument)
	}
	instances, err := s.repo.Upsert(ctx, req.Source, req.Alerts)
	if err != nil {
		return 0, err
	}
	for _, instance := range instances {
		if s.events == nil {
			// continue fan-out even when event repository is disabled
		} else {
			severity := instance.Severity
			if strings.TrimSpace(severity) == "" {
				severity = "warning"
			}
			_ = s.events.Create(ctx, domainevent.Envelope{
				ID:        fmt.Sprintf("alert:%s:%d", instance.ID, time.Now().UTC().UnixNano()),
				Source:    instance.Source,
				Category:  "alert",
				Severity:  severity,
				ClusterID: instance.ClusterID,
				Namespace: instance.Namespace,
				Summary:   fmt.Sprintf("%s [%s]", instance.Title, instance.Status),
				Payload: map[string]any{
					"alertId":     instance.ID,
					"fingerprint": instance.Fingerprint,
					"status":      instance.Status,
					"receiver":    instance.Receiver,
					"labels":      instance.Labels,
					"annotations": instance.Annotations,
				},
			})
		}
		s.fanOutAlert(ctx, instance)
		if s.automation != nil {
			_ = s.automation.HandleAlertAutomation(ctx, instance)
		}
	}
	return len(instances), nil
}

func authorize(principal domainidentity.Principal, permissionKey string) error {
	if appaccess.HasPermission(principal.Roles, permissionKey) {
		return nil
	}
	return fmt.Errorf("%w: missing permission %s", apperrors.ErrAccessDenied, permissionKey)
}

func validateChannelInput(input domainalert.ChannelInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return fmt.Errorf("%w: notification channel name is required", apperrors.ErrInvalidArgument)
	}
	channelType := strings.ToLower(strings.TrimSpace(input.ChannelType))
	switch channelType {
	case "webhook", "slack", "email", "wecom", "dingtalk", "feishu":
	default:
		return fmt.Errorf("%w: unsupported notification channel type %q", apperrors.ErrInvalidArgument, input.ChannelType)
	}
	if input.Config == nil {
		return fmt.Errorf("%w: notification channel config is required", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validateRouteInput(input domainalert.RouteInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return fmt.Errorf("%w: alert route name is required", apperrors.ErrInvalidArgument)
	}
	if len(input.ChannelIDs) == 0 {
		return fmt.Errorf("%w: at least one channel id is required", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validateSilenceInput(input domainalert.SilenceInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return fmt.Errorf("%w: alert silence name is required", apperrors.ErrInvalidArgument)
	}
	if input.StartsAt.IsZero() || input.EndsAt.IsZero() {
		return fmt.Errorf("%w: silence start and end times are required", apperrors.ErrInvalidArgument)
	}
	if !input.EndsAt.After(input.StartsAt) {
		return fmt.Errorf("%w: silence end time must be after start time", apperrors.ErrInvalidArgument)
	}
	return nil
}

func (s *Service) fanOutAlert(ctx context.Context, instance domainalert.Instance) {
	if s.repo == nil {
		return
	}
	silences, err := s.repo.ListSilences(ctx)
	if err == nil {
		if silence, ok := firstMatchingSilence(silences, instance, time.Now().UTC()); ok {
			_ = s.repo.CreateDeliveryLog(ctx, domainalert.DeliveryLog{
				ID:        uuid.NewString(),
				AlertID:   instance.ID,
				Status:    "silenced",
				Summary:   silence.Reason,
				Metadata:  map[string]any{"silenceId": silence.ID, "silenceName": silence.Name},
				CreatedAt: time.Now().UTC(),
			})
			return
		}
	}
	routes, err := s.repo.ListRoutes(ctx)
	if err != nil || len(routes) == 0 {
		return
	}
	channels, err := s.repo.ListChannels(ctx)
	if err != nil || len(channels) == 0 {
		return
	}
	channelMap := make(map[string]domainalert.NotificationChannel, len(channels))
	for _, channel := range channels {
		channelMap[channel.ID] = channel
	}
	for _, route := range routes {
		if !route.Enabled || !matchesRoute(route, instance) {
			continue
		}
		for _, channelID := range route.ChannelIDs {
			channel, ok := channelMap[channelID]
			if !ok || !channel.Enabled {
				_ = s.repo.CreateDeliveryLog(ctx, domainalert.DeliveryLog{
					ID:        uuid.NewString(),
					AlertID:   instance.ID,
					ChannelID: channelID,
					Status:    "skipped",
					Summary:   "channel is missing or disabled",
					Metadata:  map[string]any{"routeId": route.ID},
					CreatedAt: time.Now().UTC(),
				})
				continue
			}
			status, summary, metadata := s.deliverToChannel(ctx, channel, instance, route)
			_ = s.repo.CreateDeliveryLog(ctx, domainalert.DeliveryLog{
				ID:        uuid.NewString(),
				AlertID:   instance.ID,
				ChannelID: channelID,
				Status:    status,
				Summary:   summary,
				Metadata:  metadata,
				CreatedAt: time.Now().UTC(),
			})
		}
	}
}

func firstMatchingSilence(silences []domainalert.AlertSilence, instance domainalert.Instance, now time.Time) (domainalert.AlertSilence, bool) {
	for _, silence := range silences {
		if !silence.Enabled {
			continue
		}
		if now.Before(silence.StartsAt) || now.After(silence.EndsAt) {
			continue
		}
		routeLike := domainalert.AlertRoute{Matchers: silence.Matchers, Enabled: silence.Enabled}
		if matchesRoute(routeLike, instance) {
			return silence, true
		}
	}
	return domainalert.AlertSilence{}, false
}

func matchesRoute(route domainalert.AlertRoute, instance domainalert.Instance) bool {
	if len(route.Matchers) == 0 {
		return true
	}
	for key, rawValue := range route.Matchers {
		values := matcherValues(rawValue)
		switch {
		case key == "severity":
			if !containsMatcher(values, instance.Severity) {
				return false
			}
		case key == "status":
			if !containsMatcher(values, instance.Status) {
				return false
			}
		case key == "clusterId":
			if !containsMatcher(values, instance.ClusterID) {
				return false
			}
		case key == "namespace":
			if !containsMatcher(values, instance.Namespace) {
				return false
			}
		case strings.HasPrefix(key, "label:"):
			labelKey := strings.TrimPrefix(key, "label:")
			if !containsMatcher(values, instance.Labels[labelKey]) {
				return false
			}
		}
	}
	return true
}

func matcherValues(value any) []string {
	switch typed := value.(type) {
	case string:
		return []string{typed}
	case []string:
		return typed
	case []any:
		items := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				items = append(items, text)
			}
		}
		return items
	default:
		return []string{}
	}
}

func containsMatcher(values []string, actual string) bool {
	if len(values) == 0 {
		return true
	}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(actual)) {
			return true
		}
	}
	return false
}

func (s *Service) deliverToChannel(ctx context.Context, channel domainalert.NotificationChannel, instance domainalert.Instance, route domainalert.AlertRoute) (string, string, map[string]any) {
	targetURL := resolveChannelURL(channel)
	if targetURL == "" {
		return "skipped", "channel does not expose a supported webhook url", map[string]any{"routeId": route.ID, "channelType": channel.ChannelType}
	}
	payload := map[string]any{
		"route":   route.Name,
		"routeId": route.ID,
		"alert": map[string]any{
			"id":          instance.ID,
			"title":       instance.Title,
			"summary":     instance.Summary,
			"severity":    instance.Severity,
			"status":      instance.Status,
			"clusterId":   instance.ClusterID,
			"namespace":   instance.Namespace,
			"fingerprint": instance.Fingerprint,
			"labels":      instance.Labels,
			"annotations": instance.Annotations,
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return "failed", err.Error(), map[string]any{"routeId": route.ID}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, strings.NewReader(string(encoded)))
	if err != nil {
		return "failed", err.Error(), map[string]any{"routeId": route.ID, "url": targetURL}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return "failed", err.Error(), map[string]any{"routeId": route.ID, "url": targetURL}
	}
	defer resp.Body.Close()
	if resp.StatusCode >= http.StatusBadRequest {
		return "failed", fmt.Sprintf("delivery failed with status %d", resp.StatusCode), map[string]any{"routeId": route.ID, "url": targetURL}
	}
	return "delivered", "alert delivered to channel", map[string]any{"routeId": route.ID, "url": targetURL}
}

func resolveChannelURL(channel domainalert.NotificationChannel) string {
	keys := []string{"url", "webhookUrl", "webhook_url"}
	for _, key := range keys {
		if value, ok := channel.Config[key]; ok {
			if text, ok := value.(string); ok {
				if parsed, err := url.Parse(strings.TrimSpace(text)); err == nil && parsed.Scheme != "" && parsed.Host != "" {
					return text
				}
			}
		}
	}
	return ""
}
