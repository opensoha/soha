package monitoring

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"sync"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domainevent "github.com/opensoha/soha/internal/domain/event"
	domaingovernance "github.com/opensoha/soha/internal/domain/governance"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/telemetry"
)

type EventWriter interface {
	Create(context.Context, domainevent.Envelope) error
}

type AlertAutomationHandler interface {
	HandleAlertAutomation(context.Context, domainalert.Instance) error
}

type LogTelemetry interface {
	Correlate(context.Context, string, string, map[string]any, telemetry.LogCorrelationQuery) (telemetry.LogCorrelationResult, error)
}

type MetricTelemetry interface {
	Analyze(context.Context, string, string, map[string]any, telemetry.MetricRangeQuery) (telemetry.MetricAnomalySummary, error)
}

type TraceTelemetry interface {
	FindSlowSpans(context.Context, string, string, map[string]any, telemetry.TraceQuery) (telemetry.TraceResult, error)
}

type Service struct {
	alertReader           AlertReader
	alertWriter           AlertWriter
	channels              ChannelRepository
	silences              SilenceRepository
	deliveryLogs          DeliveryLogRepository
	rules                 RuleRepository
	ruleRuns              RuleRunRepository
	alertEvents           AlertEventRepository
	notificationPolicies  NotificationPolicyRepository
	notificationTemplates NotificationTemplateRepository
	healingPolicies       HealingPolicyRepository
	healingRuns           HealingRunRepository
	onCallSchedules       OnCallScheduleRepository
	onCallRotations       OnCallRotationRepository
	onCallEscalations     OnCallEscalationRepository
	onCallAssignments     OnCallAssignmentRepository
	integrations          AlertIntegrationRepository
	events                EventWriter
	permissions           *appaccess.PermissionResolver
	webhookToken          string
	webhookKeys           keyring.Ring
	enabled               bool
	httpClient            *http.Client
	automation            AlertAutomationHandler
	dataSources           DataSourceRepository
	workflow              WorkflowExecutor
	ruleInterval          time.Duration
	logs                  LogTelemetry
	metrics               MetricTelemetry
	traces                TraceTelemetry
	lifecycleMu           sync.Mutex
	lifecycleCancel       context.CancelFunc
	lifecycleDone         chan struct{}
	running               bool
}

type Dependencies struct {
	AlertReader           AlertReader
	AlertWriter           AlertWriter
	Channels              ChannelRepository
	Silences              SilenceRepository
	DeliveryLogs          DeliveryLogRepository
	Rules                 RuleRepository
	RuleRuns              RuleRunRepository
	AlertEvents           AlertEventRepository
	NotificationPolicies  NotificationPolicyRepository
	NotificationTemplates NotificationTemplateRepository
	HealingPolicies       HealingPolicyRepository
	HealingRuns           HealingRunRepository
	OnCallSchedules       OnCallScheduleRepository
	OnCallRotations       OnCallRotationRepository
	OnCallEscalations     OnCallEscalationRepository
	OnCallAssignments     OnCallAssignmentRepository
	Integrations          AlertIntegrationRepository
	Events                EventWriter
	DataSources           DataSourceRepository
	Permissions           *appaccess.PermissionResolver
	Enabled               bool
	WebhookKeys           keyring.Ring
}

type Option func(*Service)

func WithTelemetryBackends(logs LogTelemetry, metrics MetricTelemetry, traces TraceTelemetry) Option {
	return func(service *Service) {
		if logs != nil {
			service.logs = logs
		}
		if metrics != nil {
			service.metrics = metrics
		}
		if traces != nil {
			service.traces = traces
		}
	}
}

func New(deps Dependencies, options ...Option) (*Service, error) {
	required := []struct {
		name  string
		value any
	}{
		{"alert reader", deps.AlertReader},
		{"alert writer", deps.AlertWriter},
		{"channels", deps.Channels},
		{"silences", deps.Silences},
		{"delivery logs", deps.DeliveryLogs},
		{"rules", deps.Rules},
		{"rule runs", deps.RuleRuns},
		{"alert events", deps.AlertEvents},
		{"notification policies", deps.NotificationPolicies},
		{"notification templates", deps.NotificationTemplates},
		{"healing policies", deps.HealingPolicies},
		{"healing runs", deps.HealingRuns},
		{"on-call schedules", deps.OnCallSchedules},
		{"on-call rotations", deps.OnCallRotations},
		{"on-call escalations", deps.OnCallEscalations},
		{"on-call assignments", deps.OnCallAssignments},
		{"alert integrations", deps.Integrations},
		{"events", deps.Events},
		{"data sources", deps.DataSources},
		{"permissions", deps.Permissions},
	}
	for _, dependency := range required {
		if isNilMonitoringDependency(dependency.value) {
			return nil, fmt.Errorf("%w: monitoring service: %s dependency is required", apperrors.ErrInvalidArgument, dependency.name)
		}
	}
	service := &Service{
		alertReader:           deps.AlertReader,
		alertWriter:           deps.AlertWriter,
		channels:              deps.Channels,
		silences:              deps.Silences,
		deliveryLogs:          deps.DeliveryLogs,
		rules:                 deps.Rules,
		ruleRuns:              deps.RuleRuns,
		alertEvents:           deps.AlertEvents,
		notificationPolicies:  deps.NotificationPolicies,
		notificationTemplates: deps.NotificationTemplates,
		healingPolicies:       deps.HealingPolicies,
		healingRuns:           deps.HealingRuns,
		onCallSchedules:       deps.OnCallSchedules,
		onCallRotations:       deps.OnCallRotations,
		onCallEscalations:     deps.OnCallEscalations,
		onCallAssignments:     deps.OnCallAssignments,
		integrations:          deps.Integrations,
		events:                deps.Events,
		dataSources:           deps.DataSources,
		permissions:           deps.Permissions,
		enabled:               deps.Enabled,
		webhookKeys:           deps.WebhookKeys,
		httpClient:            &http.Client{Timeout: 8 * time.Second},
		ruleInterval:          1 * time.Minute,
		logs:                  unavailableTelemetry{},
		metrics:               unavailableTelemetry{},
		traces:                unavailableTelemetry{},
	}
	for _, option := range options {
		if option != nil {
			option(service)
		}
	}
	return service, nil
}

func isNilMonitoringDependency(dependency any) bool {
	if dependency == nil {
		return true
	}
	value := reflect.ValueOf(dependency)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Ptr, reflect.Slice:
		return value.IsNil()
	default:
		return false
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

func (s *Service) logBackend() LogTelemetry {
	if s.logs != nil {
		return s.logs
	}
	return unavailableTelemetry{}
}

func (s *Service) metricBackend() MetricTelemetry {
	if s.metrics != nil {
		return s.metrics
	}
	return unavailableTelemetry{}
}

func (s *Service) traceBackend() TraceTelemetry {
	if s.traces != nil {
		return s.traces
	}
	return unavailableTelemetry{}
}

type unavailableTelemetry struct{}

func (unavailableTelemetry) Correlate(context.Context, string, string, map[string]any, telemetry.LogCorrelationQuery) (telemetry.LogCorrelationResult, error) {
	return telemetry.LogCorrelationResult{}, errors.New("log telemetry backend is not configured")
}

func (unavailableTelemetry) Analyze(context.Context, string, string, map[string]any, telemetry.MetricRangeQuery) (telemetry.MetricAnomalySummary, error) {
	return telemetry.MetricAnomalySummary{}, errors.New("metric telemetry backend is not configured")
}

func (unavailableTelemetry) FindSlowSpans(context.Context, string, string, map[string]any, telemetry.TraceQuery) (telemetry.TraceResult, error) {
	return telemetry.TraceResult{}, errors.New("trace telemetry backend is not configured")
}

func (s *Service) RecordGovernanceAlert(ctx context.Context, input domaingovernance.AlertInput) error {
	if s.alertEvents == nil {
		return nil
	}
	event := governanceAlertEventInput(input)
	item, err := s.alertEvents.CreateEvent(ctx, event)
	if err != nil {
		return err
	}
	_, _ = s.fanOutEvent(ctx, item)
	return nil
}

func (s *Service) Summary(ctx context.Context, principal domainidentity.Principal) (domainalert.Summary, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveMonitoringView); err != nil {
		return domainalert.Summary{}, err
	}
	if s.alertReader == nil {
		return domainalert.Summary{}, nil
	}
	return s.alertReader.Summary(ctx)
}

func (s *Service) ListAlerts(ctx context.Context, principal domainidentity.Principal, filter domainalert.Filter) ([]domainalert.Instance, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertsView); err != nil {
		return nil, err
	}
	if s.alertReader == nil {
		return []domainalert.Instance{}, nil
	}
	return s.alertReader.List(ctx, filter)
}

func (s *Service) GetAlert(ctx context.Context, principal domainidentity.Principal, alertID string) (domainalert.Instance, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertsView); err != nil {
		return domainalert.Instance{}, err
	}
	if s.alertReader == nil {
		return domainalert.Instance{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	item, err := s.alertReader.Get(ctx, strings.TrimSpace(alertID))
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
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
	if s.alertWriter == nil {
		return domainalert.Instance{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(alertID) == "" {
		return domainalert.Instance{}, fmt.Errorf("%w: alert id is required", apperrors.ErrInvalidArgument)
	}
	item, err := s.alertWriter.UpdateOwnership(ctx, alertID, input)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
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
	if s.alertWriter == nil {
		return domainalert.Instance{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(alertID) == "" {
		return domainalert.Instance{}, fmt.Errorf("%w: alert id is required", apperrors.ErrInvalidArgument)
	}
	item, err := s.alertWriter.Acknowledge(ctx, alertID, userID, userName)
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
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
	if s.channels == nil {
		return []domainalert.NotificationChannel{}, nil
	}
	return s.channels.ListChannels(ctx)
}

func (s *Service) CreateChannel(ctx context.Context, principal domainidentity.Principal, input domainalert.ChannelInput) (domainalert.NotificationChannel, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsManage); err != nil {
		return domainalert.NotificationChannel{}, err
	}
	if s.channels == nil {
		return domainalert.NotificationChannel{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if err := validateChannelInput(input); err != nil {
		return domainalert.NotificationChannel{}, err
	}
	return s.channels.CreateChannel(ctx, input)
}

func (s *Service) UpdateChannel(ctx context.Context, principal domainidentity.Principal, channelID string, input domainalert.ChannelInput) (domainalert.NotificationChannel, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsManage); err != nil {
		return domainalert.NotificationChannel{}, err
	}
	if s.channels == nil {
		return domainalert.NotificationChannel{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	return updateMonitoringResource(
		ctx, channelID, "notification channel", func() error { return validateChannelInput(input) },
		func(ctx context.Context, id string) (domainalert.NotificationChannel, error) {
			return s.channels.UpdateChannel(ctx, id, input)
		},
	)
}

func (s *Service) ListRoutes(ctx context.Context, principal domainidentity.Principal) ([]domainalert.AlertRoute, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsView); err != nil {
		return nil, err
	}
	if s.notificationPolicies == nil {
		return []domainalert.AlertRoute{}, nil
	}
	items, err := s.notificationPolicies.ListNotificationPolicies(ctx)
	if err != nil {
		return nil, err
	}
	return compatAlertRoutes(items), nil
}

func (s *Service) ListSilences(ctx context.Context, principal domainidentity.Principal) ([]domainalert.AlertSilence, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsView); err != nil {
		return nil, err
	}
	if s.silences == nil {
		return []domainalert.AlertSilence{}, nil
	}
	return s.silences.ListSilences(ctx)
}

func (s *Service) CreateSilence(ctx context.Context, principal domainidentity.Principal, input domainalert.SilenceInput) (domainalert.AlertSilence, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsManage); err != nil {
		return domainalert.AlertSilence{}, err
	}
	if s.silences == nil {
		return domainalert.AlertSilence{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if err := validateSilenceInput(input); err != nil {
		return domainalert.AlertSilence{}, err
	}
	return s.silences.CreateSilence(ctx, input)
}

func (s *Service) UpdateSilence(ctx context.Context, principal domainidentity.Principal, silenceID string, input domainalert.SilenceInput) (domainalert.AlertSilence, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsManage); err != nil {
		return domainalert.AlertSilence{}, err
	}
	if s.silences == nil {
		return domainalert.AlertSilence{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	return updateMonitoringResource(
		ctx, silenceID, "alert silence", func() error { return validateSilenceInput(input) },
		func(ctx context.Context, id string) (domainalert.AlertSilence, error) {
			return s.silences.UpdateSilence(ctx, id, input)
		},
	)
}

func updateMonitoringResource[T any](ctx context.Context, id, resourceName string, validate func() error, update func(context.Context, string) (T, error)) (T, error) {
	var zero T
	trimmedID := strings.TrimSpace(id)
	if trimmedID == "" {
		return zero, fmt.Errorf("%w: %s id is required", apperrors.ErrInvalidArgument, resourceName)
	}
	if err := validate(); err != nil {
		return zero, err
	}
	item, err := update(ctx, id)
	if err == nil {
		return item, nil
	}
	if errors.Is(err, apperrors.ErrNotFound) {
		return zero, fmt.Errorf("%w: %s", apperrors.ErrNotFound, trimmedID)
	}
	return zero, err
}

func (s *Service) ListDeliveryLogs(ctx context.Context, principal domainidentity.Principal, filter domainalert.DeliveryFilter) ([]domainalert.DeliveryLog, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertsView); err != nil {
		return nil, err
	}
	if s.deliveryLogs == nil {
		return []domainalert.DeliveryLog{}, nil
	}
	return s.deliveryLogs.ListDeliveryLogs(ctx, filter)
}

func (s *Service) CreateRoute(ctx context.Context, principal domainidentity.Principal, input domainalert.RouteInput) (domainalert.AlertRoute, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsManage); err != nil {
		return domainalert.AlertRoute{}, err
	}
	if s.notificationPolicies == nil {
		return domainalert.AlertRoute{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if err := validateRouteInput(input); err != nil {
		return domainalert.AlertRoute{}, err
	}
	item, err := s.notificationPolicies.CreateNotificationPolicy(ctx, compatNotificationPolicyInput(input))
	if err != nil {
		return domainalert.AlertRoute{}, err
	}
	return compatAlertRoute(item), nil
}

func (s *Service) UpdateRoute(ctx context.Context, principal domainidentity.Principal, routeID string, input domainalert.RouteInput) (domainalert.AlertRoute, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsManage); err != nil {
		return domainalert.AlertRoute{}, err
	}
	if s.notificationPolicies == nil {
		return domainalert.AlertRoute{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(routeID) == "" {
		return domainalert.AlertRoute{}, fmt.Errorf("%w: alert route id is required", apperrors.ErrInvalidArgument)
	}
	if err := validateRouteInput(input); err != nil {
		return domainalert.AlertRoute{}, err
	}
	item, err := s.notificationPolicies.UpdateNotificationPolicy(ctx, routeID, compatNotificationPolicyInput(input))
	if err != nil {
		if errors.Is(err, apperrors.ErrNotFound) {
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
	if s.webhookKeys.Active().ID() != "" {
		if !s.webhookKeys.Match(token, time.Now().UTC()) {
			return fmt.Errorf("%w: invalid monitoring webhook token", apperrors.ErrUnauthorized)
		}
		return nil
	}
	if strings.TrimSpace(s.webhookToken) == "" {
		return nil
	}
	if !alertIntegrationTokenMatches(s.webhookToken, token) {
		return fmt.Errorf("%w: invalid monitoring webhook token", apperrors.ErrUnauthorized)
	}
	return nil
}

func (s *Service) Ingest(ctx context.Context, req domainalert.IngestRequest) (int, error) {
	if s.alertWriter == nil {
		return 0, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if !s.enabled {
		return 0, fmt.Errorf("%w: monitoring integrations are disabled", apperrors.ErrAccessDenied)
	}
	if len(req.Alerts) == 0 {
		return 0, fmt.Errorf("%w: alerts payload cannot be empty", apperrors.ErrInvalidArgument)
	}
	instances, err := s.alertWriter.Upsert(ctx, req.Source, req.Alerts)
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
		event, _ := s.alertEvents.CreateEvent(ctx, domainalert.AlertEventInput{
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
