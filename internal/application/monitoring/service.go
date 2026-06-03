package monitoring

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	appaccess "github.com/soha/soha/internal/application/access"
	domainalert "github.com/soha/soha/internal/domain/alert"
	domainevent "github.com/soha/soha/internal/domain/event"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	"github.com/soha/soha/internal/platform/apperrors"
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
	ListRules(context.Context) ([]domainalert.AlertRule, error)
	GetRule(context.Context, string) (domainalert.AlertRule, error)
	CreateRule(context.Context, domainalert.AlertRuleInput) (domainalert.AlertRule, error)
	UpdateRule(context.Context, string, domainalert.AlertRuleInput) (domainalert.AlertRule, error)
	ListRuleRuns(context.Context, domainalert.AlertRuleRunFilter) ([]domainalert.AlertRuleRun, error)
	CreateRuleRun(context.Context, domainalert.AlertRuleRunInput) (domainalert.AlertRuleRun, error)
	ListEvents(context.Context, domainalert.AlertEventFilter) ([]domainalert.AlertEvent, error)
	GetEvent(context.Context, string) (domainalert.AlertEvent, error)
	CreateEvent(context.Context, domainalert.AlertEventInput) (domainalert.AlertEvent, error)
	UpdateEvent(context.Context, string, domainalert.AlertEventInput) (domainalert.AlertEvent, error)
	ListNotificationPolicies(context.Context) ([]domainalert.NotificationPolicy, error)
	CreateNotificationPolicy(context.Context, domainalert.NotificationPolicyInput) (domainalert.NotificationPolicy, error)
	UpdateNotificationPolicy(context.Context, string, domainalert.NotificationPolicyInput) (domainalert.NotificationPolicy, error)
	ListNotificationTemplates(context.Context) ([]domainalert.NotificationTemplate, error)
	CreateNotificationTemplate(context.Context, domainalert.NotificationTemplateInput) (domainalert.NotificationTemplate, error)
	UpdateNotificationTemplate(context.Context, string, domainalert.NotificationTemplateInput) (domainalert.NotificationTemplate, error)
	ListHealingPolicies(context.Context) ([]domainalert.HealingPolicy, error)
	GetHealingPolicy(context.Context, string) (domainalert.HealingPolicy, error)
	CreateHealingPolicy(context.Context, domainalert.HealingPolicyInput) (domainalert.HealingPolicy, error)
	UpdateHealingPolicy(context.Context, string, domainalert.HealingPolicyInput) (domainalert.HealingPolicy, error)
	ListHealingRuns(context.Context, domainalert.HealingRunFilter) ([]domainalert.HealingRun, error)
	GetHealingRun(context.Context, string) (domainalert.HealingRun, error)
	CreateHealingRun(context.Context, domainalert.HealingRunInput) (domainalert.HealingRun, error)
	UpdateHealingRun(context.Context, string, domainalert.HealingRunInput) (domainalert.HealingRun, error)
	ListOnCallSchedules(context.Context) ([]domainalert.OnCallSchedule, error)
	CreateOnCallSchedule(context.Context, domainalert.OnCallScheduleInput) (domainalert.OnCallSchedule, error)
	UpdateOnCallSchedule(context.Context, string, domainalert.OnCallScheduleInput) (domainalert.OnCallSchedule, error)
	ListOnCallRotations(context.Context) ([]domainalert.OnCallRotation, error)
	CreateOnCallRotation(context.Context, domainalert.OnCallRotationInput) (domainalert.OnCallRotation, error)
	UpdateOnCallRotation(context.Context, string, domainalert.OnCallRotationInput) (domainalert.OnCallRotation, error)
	ListOnCallEscalationPolicies(context.Context) ([]domainalert.OnCallEscalationPolicy, error)
	CreateOnCallEscalationPolicy(context.Context, domainalert.OnCallEscalationPolicyInput) (domainalert.OnCallEscalationPolicy, error)
	UpdateOnCallEscalationPolicy(context.Context, string, domainalert.OnCallEscalationPolicyInput) (domainalert.OnCallEscalationPolicy, error)
	ListOnCallAssignmentRules(context.Context) ([]domainalert.OnCallAssignmentRule, error)
	CreateOnCallAssignmentRule(context.Context, domainalert.OnCallAssignmentRuleInput) (domainalert.OnCallAssignmentRule, error)
	UpdateOnCallAssignmentRule(context.Context, string, domainalert.OnCallAssignmentRuleInput) (domainalert.OnCallAssignmentRule, error)
	ListAlertIntegrations(context.Context) ([]domainalert.AlertIntegration, error)
	GetAlertIntegration(context.Context, string) (domainalert.AlertIntegration, error)
	CreateAlertIntegration(context.Context, domainalert.AlertIntegrationInput) (domainalert.AlertIntegration, error)
	UpdateAlertIntegration(context.Context, string, domainalert.AlertIntegrationInput) (domainalert.AlertIntegration, error)
	UpdateAlertIntegrationStatus(context.Context, string, domainalert.AlertIntegrationStatusInput) (domainalert.AlertIntegration, error)
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
	permissions  *appaccess.PermissionResolver
	webhookToken string
	enabled      bool
	httpClient   *http.Client
	automation   AlertAutomationHandler
	dataSources  DataSourceRepository
	workflow     WorkflowExecutor
	ruleInterval time.Duration
	startMu      sync.Mutex
	started      bool
}

func New(repo Repository, events EventWriter, dataSources DataSourceRepository, permissions *appaccess.PermissionResolver, enabled bool, webhookToken string) *Service {
	return &Service{
		repo:         repo,
		events:       events,
		dataSources:  dataSources,
		permissions:  permissions,
		enabled:      enabled,
		webhookToken: webhookToken,
		httpClient:   &http.Client{Timeout: 8 * time.Second},
		ruleInterval: 1 * time.Minute,
	}
}

func (s *Service) SetAutomation(handler AlertAutomationHandler) {
	s.automation = handler
}

func (s *Service) SetDataSourceRepository(dataSources DataSourceRepository) {
	s.dataSources = dataSources
}

func (s *Service) SetWorkflowExecutor(workflow WorkflowExecutor) {
	s.workflow = workflow
}

func (s *Service) Summary(ctx context.Context, principal domainidentity.Principal) (domainalert.Summary, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveMonitoringView); err != nil {
		return domainalert.Summary{}, err
	}
	if s.repo == nil {
		return domainalert.Summary{}, nil
	}
	return s.repo.Summary(ctx)
}

func (s *Service) ListAlerts(ctx context.Context, principal domainidentity.Principal, filter domainalert.Filter) ([]domainalert.Instance, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertsView); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return []domainalert.Instance{}, nil
	}
	return s.repo.List(ctx, filter)
}

func (s *Service) GetAlert(ctx context.Context, principal domainidentity.Principal, alertID string) (domainalert.Instance, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertsView); err != nil {
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
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertsAssign); err != nil {
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
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertsAcknowledge); err != nil {
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
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsView); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return []domainalert.NotificationChannel{}, nil
	}
	return s.repo.ListChannels(ctx)
}

func (s *Service) CreateChannel(ctx context.Context, principal domainidentity.Principal, input domainalert.ChannelInput) (domainalert.NotificationChannel, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsManage); err != nil {
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
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsManage); err != nil {
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
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsView); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return []domainalert.AlertRoute{}, nil
	}
	items, err := s.repo.ListNotificationPolicies(ctx)
	if err != nil {
		return nil, err
	}
	return compatAlertRoutes(items), nil
}

func (s *Service) ListSilences(ctx context.Context, principal domainidentity.Principal) ([]domainalert.AlertSilence, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsView); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return []domainalert.AlertSilence{}, nil
	}
	return s.repo.ListSilences(ctx)
}

func (s *Service) CreateSilence(ctx context.Context, principal domainidentity.Principal, input domainalert.SilenceInput) (domainalert.AlertSilence, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsManage); err != nil {
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
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsManage); err != nil {
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
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertsView); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return []domainalert.DeliveryLog{}, nil
	}
	return s.repo.ListDeliveryLogs(ctx, filter)
}

func (s *Service) CreateRoute(ctx context.Context, principal domainidentity.Principal, input domainalert.RouteInput) (domainalert.AlertRoute, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsManage); err != nil {
		return domainalert.AlertRoute{}, err
	}
	if s.repo == nil {
		return domainalert.AlertRoute{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if err := validateRouteInput(input); err != nil {
		return domainalert.AlertRoute{}, err
	}
	item, err := s.repo.CreateNotificationPolicy(ctx, compatNotificationPolicyInput(input))
	if err != nil {
		return domainalert.AlertRoute{}, err
	}
	return compatAlertRoute(item), nil
}

func (s *Service) UpdateRoute(ctx context.Context, principal domainidentity.Principal, routeID string, input domainalert.RouteInput) (domainalert.AlertRoute, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsManage); err != nil {
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
	item, err := s.repo.UpdateNotificationPolicy(ctx, routeID, compatNotificationPolicyInput(input))
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return domainalert.AlertRoute{}, fmt.Errorf("%w: %s", apperrors.ErrNotFound, strings.TrimSpace(routeID))
		}
		return domainalert.AlertRoute{}, err
	}
	return compatAlertRoute(item), nil
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
		event, _ := s.repo.CreateEvent(ctx, domainalert.AlertEventInput{
			ID:           instance.ID,
			RuleID:       "",
			SourceType:   "external_webhook",
			SourceSystem: instance.Source,
			Fingerprint:  instance.Fingerprint,
			Title:        instance.Title,
			Summary:      instance.Summary,
			Severity:     instance.Severity,
			Status:       instance.Status,
			ClusterID:    instance.ClusterID,
			Namespace:    instance.Namespace,
			Labels:       instance.Labels,
			Annotations:  instance.Annotations,
			Receiver:     instance.Receiver,
			GeneratorURL: instance.GeneratorURL,
			StartsAt:     instance.StartsAt,
			EndsAt:       instance.EndsAt,
			LastSeenAt:   instance.LastSeenAt,
			CurrentState: instance.Status,
		})
		_, _ = s.fanOutEvent(ctx, event)
		if s.automation != nil {
			_ = s.automation.HandleAlertAutomation(ctx, instance)
		}
	}
	return len(instances), nil
}

func (s *Service) authorize(ctx context.Context, principal domainidentity.Principal, permissionKey string) error {
	return appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, permissionKey)
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

func compatAlertRoutes(items []domainalert.NotificationPolicy) []domainalert.AlertRoute {
	routes := make([]domainalert.AlertRoute, 0, len(items))
	for _, item := range items {
		routes = append(routes, compatAlertRoute(item))
	}
	return routes
}

func compatAlertRoute(item domainalert.NotificationPolicy) domainalert.AlertRoute {
	return domainalert.AlertRoute{
		ID:         item.ID,
		Name:       item.Name,
		Matchers:   item.Matchers,
		ChannelIDs: append([]string{}, item.ChannelRefs...),
		Enabled:    item.Enabled,
		CreatedAt:  item.CreatedAt,
		UpdatedAt:  item.UpdatedAt,
	}
}

func compatNotificationPolicyInput(input domainalert.RouteInput) domainalert.NotificationPolicyInput {
	return domainalert.NotificationPolicyInput{
		ID:              input.ID,
		Name:            input.Name,
		Matchers:        input.Matchers,
		ProcessorChain:  []string{"webhook_update"},
		ChannelRefs:     append([]string{}, input.ChannelIDs...),
		OnCallRef:       "",
		SendResolved:    false,
		CooldownSeconds: 0,
		Enabled:         input.Enabled,
	}
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

func firstMatchingSilence(silences []domainalert.AlertSilence, instance domainalert.Instance, now time.Time) (domainalert.AlertSilence, bool) {
	for _, silence := range silences {
		if !silence.Enabled {
			continue
		}
		if now.Before(silence.StartsAt) || now.After(silence.EndsAt) {
			continue
		}
		if silenceMatches(silence.Matchers, instance) {
			return silence, true
		}
	}
	return domainalert.AlertSilence{}, false
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
