package monitoring

import (
	"context"
	"testing"

	appaccess "github.com/kubecrux/kubecrux/internal/application/access"
	domainalert "github.com/kubecrux/kubecrux/internal/domain/alert"
	domainevent "github.com/kubecrux/kubecrux/internal/domain/event"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
)

type stubMonitoringRolePermissionReader struct {
	matrix map[string][]string
}

func (s stubMonitoringRolePermissionReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return s.matrix, nil
}

type stubMonitoringCompatRepository struct {
	listNotificationPoliciesFn func(context.Context) ([]domainalert.NotificationPolicy, error)
	createNotificationPolicyFn func(context.Context, domainalert.NotificationPolicyInput) (domainalert.NotificationPolicy, error)
	updateNotificationPolicyFn func(context.Context, string, domainalert.NotificationPolicyInput) (domainalert.NotificationPolicy, error)
}

func (s *stubMonitoringCompatRepository) Upsert(context.Context, string, []domainalert.IngestAlert) ([]domainalert.Instance, error) {
	return nil, nil
}

func (s *stubMonitoringCompatRepository) List(context.Context, domainalert.Filter) ([]domainalert.Instance, error) {
	return nil, nil
}

func (s *stubMonitoringCompatRepository) Get(context.Context, string) (domainalert.Instance, error) {
	return domainalert.Instance{}, nil
}

func (s *stubMonitoringCompatRepository) UpdateOwnership(context.Context, string, domainalert.OwnershipInput) (domainalert.Instance, error) {
	return domainalert.Instance{}, nil
}

func (s *stubMonitoringCompatRepository) Acknowledge(context.Context, string, string, string) (domainalert.Instance, error) {
	return domainalert.Instance{}, nil
}

func (s *stubMonitoringCompatRepository) Summary(context.Context) (domainalert.Summary, error) {
	return domainalert.Summary{}, nil
}

func (s *stubMonitoringCompatRepository) ListChannels(context.Context) ([]domainalert.NotificationChannel, error) {
	return nil, nil
}

func (s *stubMonitoringCompatRepository) CreateChannel(context.Context, domainalert.ChannelInput) (domainalert.NotificationChannel, error) {
	return domainalert.NotificationChannel{}, nil
}

func (s *stubMonitoringCompatRepository) UpdateChannel(context.Context, string, domainalert.ChannelInput) (domainalert.NotificationChannel, error) {
	return domainalert.NotificationChannel{}, nil
}

func (s *stubMonitoringCompatRepository) ListRoutes(context.Context) ([]domainalert.AlertRoute, error) {
	return nil, nil
}

func (s *stubMonitoringCompatRepository) CreateRoute(context.Context, domainalert.RouteInput) (domainalert.AlertRoute, error) {
	return domainalert.AlertRoute{}, nil
}

func (s *stubMonitoringCompatRepository) UpdateRoute(context.Context, string, domainalert.RouteInput) (domainalert.AlertRoute, error) {
	return domainalert.AlertRoute{}, nil
}

func (s *stubMonitoringCompatRepository) ListSilences(context.Context) ([]domainalert.AlertSilence, error) {
	return nil, nil
}

func (s *stubMonitoringCompatRepository) CreateSilence(context.Context, domainalert.SilenceInput) (domainalert.AlertSilence, error) {
	return domainalert.AlertSilence{}, nil
}

func (s *stubMonitoringCompatRepository) UpdateSilence(context.Context, string, domainalert.SilenceInput) (domainalert.AlertSilence, error) {
	return domainalert.AlertSilence{}, nil
}

func (s *stubMonitoringCompatRepository) ListDeliveryLogs(context.Context, domainalert.DeliveryFilter) ([]domainalert.DeliveryLog, error) {
	return nil, nil
}

func (s *stubMonitoringCompatRepository) CreateDeliveryLog(context.Context, domainalert.DeliveryLog) error {
	return nil
}

func (s *stubMonitoringCompatRepository) ListRules(context.Context) ([]domainalert.AlertRule, error) {
	return nil, nil
}

func (s *stubMonitoringCompatRepository) GetRule(context.Context, string) (domainalert.AlertRule, error) {
	return domainalert.AlertRule{}, nil
}

func (s *stubMonitoringCompatRepository) CreateRule(context.Context, domainalert.AlertRuleInput) (domainalert.AlertRule, error) {
	return domainalert.AlertRule{}, nil
}

func (s *stubMonitoringCompatRepository) UpdateRule(context.Context, string, domainalert.AlertRuleInput) (domainalert.AlertRule, error) {
	return domainalert.AlertRule{}, nil
}

func (s *stubMonitoringCompatRepository) ListRuleRuns(context.Context, domainalert.AlertRuleRunFilter) ([]domainalert.AlertRuleRun, error) {
	return nil, nil
}

func (s *stubMonitoringCompatRepository) CreateRuleRun(context.Context, domainalert.AlertRuleRunInput) (domainalert.AlertRuleRun, error) {
	return domainalert.AlertRuleRun{}, nil
}

func (s *stubMonitoringCompatRepository) ListEvents(context.Context, domainalert.AlertEventFilter) ([]domainalert.AlertEvent, error) {
	return nil, nil
}

func (s *stubMonitoringCompatRepository) GetEvent(context.Context, string) (domainalert.AlertEvent, error) {
	return domainalert.AlertEvent{}, nil
}

func (s *stubMonitoringCompatRepository) CreateEvent(context.Context, domainalert.AlertEventInput) (domainalert.AlertEvent, error) {
	return domainalert.AlertEvent{}, nil
}

func (s *stubMonitoringCompatRepository) UpdateEvent(context.Context, string, domainalert.AlertEventInput) (domainalert.AlertEvent, error) {
	return domainalert.AlertEvent{}, nil
}

func (s *stubMonitoringCompatRepository) ListNotificationPolicies(ctx context.Context) ([]domainalert.NotificationPolicy, error) {
	if s.listNotificationPoliciesFn == nil {
		return nil, nil
	}
	return s.listNotificationPoliciesFn(ctx)
}

func (s *stubMonitoringCompatRepository) CreateNotificationPolicy(ctx context.Context, input domainalert.NotificationPolicyInput) (domainalert.NotificationPolicy, error) {
	if s.createNotificationPolicyFn == nil {
		return domainalert.NotificationPolicy{}, nil
	}
	return s.createNotificationPolicyFn(ctx, input)
}

func (s *stubMonitoringCompatRepository) UpdateNotificationPolicy(ctx context.Context, policyID string, input domainalert.NotificationPolicyInput) (domainalert.NotificationPolicy, error) {
	if s.updateNotificationPolicyFn == nil {
		return domainalert.NotificationPolicy{}, nil
	}
	return s.updateNotificationPolicyFn(ctx, policyID, input)
}

func (s *stubMonitoringCompatRepository) ListNotificationTemplates(context.Context) ([]domainalert.NotificationTemplate, error) {
	return nil, nil
}

func (s *stubMonitoringCompatRepository) CreateNotificationTemplate(context.Context, domainalert.NotificationTemplateInput) (domainalert.NotificationTemplate, error) {
	return domainalert.NotificationTemplate{}, nil
}

func (s *stubMonitoringCompatRepository) UpdateNotificationTemplate(context.Context, string, domainalert.NotificationTemplateInput) (domainalert.NotificationTemplate, error) {
	return domainalert.NotificationTemplate{}, nil
}

func (s *stubMonitoringCompatRepository) ListHealingPolicies(context.Context) ([]domainalert.HealingPolicy, error) {
	return nil, nil
}

func (s *stubMonitoringCompatRepository) GetHealingPolicy(context.Context, string) (domainalert.HealingPolicy, error) {
	return domainalert.HealingPolicy{}, nil
}

func (s *stubMonitoringCompatRepository) CreateHealingPolicy(context.Context, domainalert.HealingPolicyInput) (domainalert.HealingPolicy, error) {
	return domainalert.HealingPolicy{}, nil
}

func (s *stubMonitoringCompatRepository) UpdateHealingPolicy(context.Context, string, domainalert.HealingPolicyInput) (domainalert.HealingPolicy, error) {
	return domainalert.HealingPolicy{}, nil
}

func (s *stubMonitoringCompatRepository) ListHealingRuns(context.Context, domainalert.HealingRunFilter) ([]domainalert.HealingRun, error) {
	return nil, nil
}

func (s *stubMonitoringCompatRepository) GetHealingRun(context.Context, string) (domainalert.HealingRun, error) {
	return domainalert.HealingRun{}, nil
}

func (s *stubMonitoringCompatRepository) CreateHealingRun(context.Context, domainalert.HealingRunInput) (domainalert.HealingRun, error) {
	return domainalert.HealingRun{}, nil
}

func (s *stubMonitoringCompatRepository) UpdateHealingRun(context.Context, string, domainalert.HealingRunInput) (domainalert.HealingRun, error) {
	return domainalert.HealingRun{}, nil
}

func (s *stubMonitoringCompatRepository) ListOnCallSchedules(context.Context) ([]domainalert.OnCallSchedule, error) {
	return nil, nil
}

func (s *stubMonitoringCompatRepository) CreateOnCallSchedule(context.Context, domainalert.OnCallScheduleInput) (domainalert.OnCallSchedule, error) {
	return domainalert.OnCallSchedule{}, nil
}

func (s *stubMonitoringCompatRepository) UpdateOnCallSchedule(context.Context, string, domainalert.OnCallScheduleInput) (domainalert.OnCallSchedule, error) {
	return domainalert.OnCallSchedule{}, nil
}

func (s *stubMonitoringCompatRepository) ListOnCallRotations(context.Context) ([]domainalert.OnCallRotation, error) {
	return nil, nil
}

func (s *stubMonitoringCompatRepository) CreateOnCallRotation(context.Context, domainalert.OnCallRotationInput) (domainalert.OnCallRotation, error) {
	return domainalert.OnCallRotation{}, nil
}

func (s *stubMonitoringCompatRepository) UpdateOnCallRotation(context.Context, string, domainalert.OnCallRotationInput) (domainalert.OnCallRotation, error) {
	return domainalert.OnCallRotation{}, nil
}

func (s *stubMonitoringCompatRepository) ListOnCallEscalationPolicies(context.Context) ([]domainalert.OnCallEscalationPolicy, error) {
	return nil, nil
}

func (s *stubMonitoringCompatRepository) CreateOnCallEscalationPolicy(context.Context, domainalert.OnCallEscalationPolicyInput) (domainalert.OnCallEscalationPolicy, error) {
	return domainalert.OnCallEscalationPolicy{}, nil
}

func (s *stubMonitoringCompatRepository) UpdateOnCallEscalationPolicy(context.Context, string, domainalert.OnCallEscalationPolicyInput) (domainalert.OnCallEscalationPolicy, error) {
	return domainalert.OnCallEscalationPolicy{}, nil
}

var _ Repository = (*stubMonitoringCompatRepository)(nil)
var _ EventWriter = (*stubMonitoringEventWriter)(nil)

type stubMonitoringEventWriter struct{}

func (s *stubMonitoringEventWriter) Create(context.Context, domainevent.Envelope) error {
	return nil
}

func monitoringCompatPermissions(keys ...string) *appaccess.PermissionResolver {
	return appaccess.NewPermissionResolver(stubMonitoringRolePermissionReader{
		matrix: map[string][]string{"ops": keys},
	})
}

func monitoringCompatPrincipal() domainidentity.Principal {
	return domainidentity.Principal{
		UserID: "user-1",
		Roles:  []string{"ops"},
	}
}

func TestServiceListRoutesUsesNotificationPolicies(t *testing.T) {
	repo := &stubMonitoringCompatRepository{
		listNotificationPoliciesFn: func(context.Context) ([]domainalert.NotificationPolicy, error) {
			return []domainalert.NotificationPolicy{
				{
					ID:          "policy-empty",
					Name:        "Empty Policy",
					Matchers:    map[string]any{},
					ChannelRefs: []string{},
					Enabled:     true,
				},
				{
					ID:          "policy-critical",
					Name:        "Critical Route",
					Matchers:    map[string]any{"severity": "critical"},
					ChannelRefs: []string{"channel-a", "channel-b"},
					Enabled:     false,
				},
			}, nil
		},
	}
	service := &Service{
		repo:        repo,
		events:      &stubMonitoringEventWriter{},
		permissions: monitoringCompatPermissions(appaccess.PermObserveNotificationsView),
	}

	items, err := service.ListRoutes(context.Background(), monitoringCompatPrincipal())
	if err != nil {
		t.Fatalf("ListRoutes returned error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].ID != "policy-empty" || items[0].Name != "Empty Policy" {
		t.Fatalf("items[0] = %#v, want projected empty policy", items[0])
	}
	if len(items[0].Matchers) != 0 {
		t.Fatalf("items[0].Matchers = %#v, want empty matchers", items[0].Matchers)
	}
	if len(items[0].ChannelIDs) != 0 {
		t.Fatalf("items[0].ChannelIDs = %#v, want empty channels", items[0].ChannelIDs)
	}
	if items[1].ID != "policy-critical" || items[1].Name != "Critical Route" {
		t.Fatalf("items[1] = %#v, want projected critical policy", items[1])
	}
	if got := items[1].Matchers["severity"]; got != "critical" {
		t.Fatalf("items[1].Matchers[severity] = %v, want critical", got)
	}
	if len(items[1].ChannelIDs) != 2 || items[1].ChannelIDs[0] != "channel-a" || items[1].ChannelIDs[1] != "channel-b" {
		t.Fatalf("items[1].ChannelIDs = %#v, want projected channels", items[1].ChannelIDs)
	}
}

func TestServiceCreateRouteMapsToCompatibilityNotificationPolicy(t *testing.T) {
	var captured domainalert.NotificationPolicyInput
	repo := &stubMonitoringCompatRepository{
		createNotificationPolicyFn: func(_ context.Context, input domainalert.NotificationPolicyInput) (domainalert.NotificationPolicy, error) {
			captured = input
			return domainalert.NotificationPolicy{
				ID:              "route-primary",
				Name:            input.Name,
				Matchers:        input.Matchers,
				ProcessorChain:  input.ProcessorChain,
				ChannelRefs:     input.ChannelRefs,
				OnCallRef:       input.OnCallRef,
				SendResolved:    input.SendResolved,
				CooldownSeconds: input.CooldownSeconds,
				Enabled:         input.Enabled,
			}, nil
		},
	}
	service := &Service{
		repo:        repo,
		events:      &stubMonitoringEventWriter{},
		permissions: monitoringCompatPermissions(appaccess.PermObserveNotificationsManage),
	}

	item, err := service.CreateRoute(context.Background(), monitoringCompatPrincipal(), domainalert.RouteInput{
		ID:         "route-primary",
		Name:       "Primary Route",
		Matchers:   map[string]any{"severity": "critical"},
		ChannelIDs: []string{"channel-slack"},
		Enabled:    true,
	})
	if err != nil {
		t.Fatalf("CreateRoute returned error: %v", err)
	}
	if captured.ID != "route-primary" {
		t.Fatalf("captured.ID = %q, want route-primary", captured.ID)
	}
	if captured.Name != "Primary Route" {
		t.Fatalf("captured.Name = %q, want Primary Route", captured.Name)
	}
	if got := captured.Matchers["severity"]; got != "critical" {
		t.Fatalf("captured.Matchers[severity] = %v, want critical", got)
	}
	if len(captured.ChannelRefs) != 1 || captured.ChannelRefs[0] != "channel-slack" {
		t.Fatalf("captured.ChannelRefs = %#v, want [channel-slack]", captured.ChannelRefs)
	}
	if len(captured.ProcessorChain) != 1 || captured.ProcessorChain[0] != "webhook_update" {
		t.Fatalf("captured.ProcessorChain = %#v, want [webhook_update]", captured.ProcessorChain)
	}
	if captured.OnCallRef != "" || captured.SendResolved || captured.CooldownSeconds != 0 {
		t.Fatalf("captured compatibility defaults = %#v, want empty oncall + false sendResolved + zero cooldown", captured)
	}
	if item.ID != "route-primary" || item.Name != "Primary Route" {
		t.Fatalf("item = %#v, want projected route", item)
	}
	if len(item.ChannelIDs) != 1 || item.ChannelIDs[0] != "channel-slack" {
		t.Fatalf("item.ChannelIDs = %#v, want [channel-slack]", item.ChannelIDs)
	}
}

func TestServiceUpdateRouteMapsToCompatibilityNotificationPolicy(t *testing.T) {
	var capturedID string
	var captured domainalert.NotificationPolicyInput
	repo := &stubMonitoringCompatRepository{
		updateNotificationPolicyFn: func(_ context.Context, policyID string, input domainalert.NotificationPolicyInput) (domainalert.NotificationPolicy, error) {
			capturedID = policyID
			captured = input
			return domainalert.NotificationPolicy{
				ID:              policyID,
				Name:            input.Name,
				Matchers:        input.Matchers,
				ProcessorChain:  input.ProcessorChain,
				ChannelRefs:     input.ChannelRefs,
				OnCallRef:       input.OnCallRef,
				SendResolved:    input.SendResolved,
				CooldownSeconds: input.CooldownSeconds,
				Enabled:         input.Enabled,
			}, nil
		},
	}
	service := &Service{
		repo:        repo,
		events:      &stubMonitoringEventWriter{},
		permissions: monitoringCompatPermissions(appaccess.PermObserveNotificationsManage),
	}

	item, err := service.UpdateRoute(context.Background(), monitoringCompatPrincipal(), "route-legacy", domainalert.RouteInput{
		Name:       "Legacy Route",
		Matchers:   nil,
		ChannelIDs: []string{"channel-feishu"},
		Enabled:    false,
	})
	if err != nil {
		t.Fatalf("UpdateRoute returned error: %v", err)
	}
	if capturedID != "route-legacy" {
		t.Fatalf("capturedID = %q, want route-legacy", capturedID)
	}
	if captured.Name != "Legacy Route" {
		t.Fatalf("captured.Name = %q, want Legacy Route", captured.Name)
	}
	if captured.Matchers != nil {
		t.Fatalf("captured.Matchers = %#v, want nil for empty matcher compatibility", captured.Matchers)
	}
	if len(captured.ChannelRefs) != 1 || captured.ChannelRefs[0] != "channel-feishu" {
		t.Fatalf("captured.ChannelRefs = %#v, want [channel-feishu]", captured.ChannelRefs)
	}
	if len(captured.ProcessorChain) != 1 || captured.ProcessorChain[0] != "webhook_update" {
		t.Fatalf("captured.ProcessorChain = %#v, want [webhook_update]", captured.ProcessorChain)
	}
	if item.ID != "route-legacy" || item.Name != "Legacy Route" {
		t.Fatalf("item = %#v, want projected route", item)
	}
	if item.Enabled {
		t.Fatalf("item.Enabled = true, want false")
	}
	if len(item.ChannelIDs) != 1 || item.ChannelIDs[0] != "channel-feishu" {
		t.Fatalf("item.ChannelIDs = %#v, want [channel-feishu]", item.ChannelIDs)
	}
}
