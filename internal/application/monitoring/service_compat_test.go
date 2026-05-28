package monitoring

import (
	"context"
	"testing"
	"time"

	appaccess "github.com/soha/soha/internal/application/access"
	domainalert "github.com/soha/soha/internal/domain/alert"
	domainevent "github.com/soha/soha/internal/domain/event"
	domainidentity "github.com/soha/soha/internal/domain/identity"
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
	onCallSchedules            []domainalert.OnCallSchedule
	onCallRotations            []domainalert.OnCallRotation
	onCallAssignmentRules      []domainalert.OnCallAssignmentRule
	alertEvents                map[string]domainalert.AlertEvent
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
	items := make([]domainalert.AlertEvent, 0, len(s.alertEvents))
	for _, item := range s.alertEvents {
		items = append(items, item)
	}
	return items, nil
}

func (s *stubMonitoringCompatRepository) GetEvent(_ context.Context, eventID string) (domainalert.AlertEvent, error) {
	if s.alertEvents != nil {
		if item, ok := s.alertEvents[eventID]; ok {
			return item, nil
		}
	}
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
	return s.onCallSchedules, nil
}

func (s *stubMonitoringCompatRepository) CreateOnCallSchedule(context.Context, domainalert.OnCallScheduleInput) (domainalert.OnCallSchedule, error) {
	return domainalert.OnCallSchedule{}, nil
}

func (s *stubMonitoringCompatRepository) UpdateOnCallSchedule(context.Context, string, domainalert.OnCallScheduleInput) (domainalert.OnCallSchedule, error) {
	return domainalert.OnCallSchedule{}, nil
}

func (s *stubMonitoringCompatRepository) ListOnCallRotations(context.Context) ([]domainalert.OnCallRotation, error) {
	return s.onCallRotations, nil
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

func (s *stubMonitoringCompatRepository) ListOnCallAssignmentRules(context.Context) ([]domainalert.OnCallAssignmentRule, error) {
	return s.onCallAssignmentRules, nil
}

func (s *stubMonitoringCompatRepository) CreateOnCallAssignmentRule(context.Context, domainalert.OnCallAssignmentRuleInput) (domainalert.OnCallAssignmentRule, error) {
	return domainalert.OnCallAssignmentRule{}, nil
}

func (s *stubMonitoringCompatRepository) UpdateOnCallAssignmentRule(context.Context, string, domainalert.OnCallAssignmentRuleInput) (domainalert.OnCallAssignmentRule, error) {
	return domainalert.OnCallAssignmentRule{}, nil
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

func TestServiceResolveOnCallUsesBusinessLineAndRoleAssignment(t *testing.T) {
	now := time.Now().UTC().Add(-1 * time.Hour)
	repo := &stubMonitoringCompatRepository{
		onCallSchedules: []domainalert.OnCallSchedule{
			{ID: "schedule:dev-retail", Name: "Retail Dev", TimeZone: "UTC", Enabled: true, CreatedAt: now, UpdatedAt: now},
		},
		onCallRotations: []domainalert.OnCallRotation{
			{ID: "rotation:dev-retail", ScheduleID: "schedule:dev-retail", Name: "Retail Dev Rotation", Participants: []string{"dev-a", "dev-b"}, RotationConfig: map[string]any{"shiftHours": 12}, Enabled: true},
		},
		onCallAssignmentRules: []domainalert.OnCallAssignmentRule{
			{ID: "oncall-rule:retail-dev", Name: "Retail critical dev", BusinessLineID: "retail", Severity: "critical", Role: "dev", TargetType: "schedule", TargetRef: "schedule:dev-retail", Priority: 200, Enabled: true},
		},
	}
	service := &Service{
		repo:        repo,
		events:      &stubMonitoringEventWriter{},
		permissions: monitoringCompatPermissions(appaccess.PermObserveOncallView),
	}

	result, err := service.ResolveOnCall(context.Background(), monitoringCompatPrincipal(), domainalert.OnCallResolveInput{
		BusinessLineID: "retail",
		Severity:       "critical",
		Role:           "dev",
	})
	if err != nil {
		t.Fatalf("ResolveOnCall returned error: %v", err)
	}
	if got := result["resolutionStatus"]; got != "matched" {
		t.Fatalf("resolutionStatus = %v, want matched", got)
	}
	if got := result["assignmentRuleId"]; got != "oncall-rule:retail-dev" {
		t.Fatalf("assignmentRuleId = %v, want oncall-rule:retail-dev", got)
	}
	if got := result["currentParticipant"]; got != "dev-a" {
		t.Fatalf("currentParticipant = %v, want dev-a", got)
	}
}

func TestServiceResolveOnCallUsesRotationDateOverride(t *testing.T) {
	now := time.Now().UTC()
	dateKey := now.Format("2006-01-02")
	repo := &stubMonitoringCompatRepository{
		onCallSchedules: []domainalert.OnCallSchedule{
			{ID: "schedule:dev-retail", Name: "Retail Dev", TimeZone: "UTC", Enabled: true, CreatedAt: now.Add(-24 * time.Hour), UpdatedAt: now},
		},
		onCallRotations: []domainalert.OnCallRotation{
			{
				ID:           "rotation:dev-retail",
				ScheduleID:   "schedule:dev-retail",
				Name:         "Retail Dev Rotation",
				Participants: []string{"dev-a", "dev-b"},
				RotationConfig: map[string]any{
					"shiftHours": 24,
					"overrides": map[string]any{
						dateKey: []any{"override-a", "override-b"},
					},
				},
				Enabled: true,
			},
		},
		onCallAssignmentRules: []domainalert.OnCallAssignmentRule{
			{ID: "oncall-rule:retail-dev", Name: "Retail critical dev", BusinessLineID: "retail", Severity: "critical", Role: "dev", TargetType: "schedule", TargetRef: "schedule:dev-retail", Priority: 200, Enabled: true},
		},
	}
	service := &Service{
		repo:        repo,
		events:      &stubMonitoringEventWriter{},
		permissions: monitoringCompatPermissions(appaccess.PermObserveOncallView),
	}

	result, err := service.ResolveOnCall(context.Background(), monitoringCompatPrincipal(), domainalert.OnCallResolveInput{
		BusinessLineID: "retail",
		Severity:       "critical",
		Role:           "dev",
	})
	if err != nil {
		t.Fatalf("ResolveOnCall returned error: %v", err)
	}
	if got := result["currentParticipant"]; got != "override-a" {
		t.Fatalf("currentParticipant = %v, want override-a", got)
	}
	if got := result["override"]; got != true {
		t.Fatalf("override = %v, want true", got)
	}
	if got := result["overrideDate"]; got != dateKey {
		t.Fatalf("overrideDate = %v, want %s", got, dateKey)
	}
}

func TestServiceResolveOnCallUsesIRMRoutingOrderAndGrouping(t *testing.T) {
	now := time.Now().UTC().Add(-1 * time.Hour)
	repo := &stubMonitoringCompatRepository{
		onCallSchedules: []domainalert.OnCallSchedule{
			{ID: "schedule:platform", Name: "Platform Primary", TimeZone: "UTC", Enabled: true, CreatedAt: now, UpdatedAt: now},
			{ID: "schedule:payments", Name: "Payments Primary", TimeZone: "UTC", Enabled: true, CreatedAt: now, UpdatedAt: now},
		},
		onCallRotations: []domainalert.OnCallRotation{
			{ID: "rotation:platform", ScheduleID: "schedule:platform", Name: "Platform Rotation", Participants: []string{"platform-a"}, Enabled: true},
			{ID: "rotation:payments", ScheduleID: "schedule:payments", Name: "Payments Rotation", Participants: []string{"payments-a"}, Enabled: true},
		},
		onCallAssignmentRules: []domainalert.OnCallAssignmentRule{
			{ID: "oncall-route:fallback", Name: "Prometheus fallback", IntegrationType: "prometheus", TargetType: "schedule", TargetRef: "schedule:platform", RouteOrder: 50, GroupBy: []string{"alertName"}, Priority: 300, Enabled: true},
			{ID: "oncall-route:payments", Name: "Payments checkout", IntegrationType: "prometheus", Service: "checkout", Severity: "critical", TargetType: "schedule", TargetRef: "schedule:payments", RouteOrder: 10, GroupBy: []string{"alertName", "service", "clusterId"}, Priority: 100, Enabled: true},
		},
	}
	service := &Service{
		repo:        repo,
		events:      &stubMonitoringEventWriter{},
		permissions: monitoringCompatPermissions(appaccess.PermObserveOncallView),
	}

	result, err := service.ResolveOnCall(context.Background(), monitoringCompatPrincipal(), domainalert.OnCallResolveInput{
		IntegrationType: "prometheus",
		AlertName:       "HighLatency",
		Severity:        "critical",
		Service:         "checkout",
		ClusterID:       "prod-a",
	})
	if err != nil {
		t.Fatalf("ResolveOnCall returned error: %v", err)
	}
	if got := result["routeId"]; got != "oncall-route:payments" {
		t.Fatalf("routeId = %v, want oncall-route:payments", got)
	}
	if got := result["groupKey"]; got != "alertName=HighLatency|service=checkout|clusterId=prod-a" {
		t.Fatalf("groupKey = %v, want alertName=HighLatency|service=checkout|clusterId=prod-a", got)
	}
	if got := result["currentParticipant"]; got != "payments-a" {
		t.Fatalf("currentParticipant = %v, want payments-a", got)
	}
}

func TestServiceResolveOnCallCanDeriveContextFromAlertEvent(t *testing.T) {
	now := time.Now().UTC().Add(-1 * time.Hour)
	repo := &stubMonitoringCompatRepository{
		alertEvents: map[string]domainalert.AlertEvent{
			"evt-qa": {
				ID:         "evt-qa",
				Title:      "Checkout smoke test failed",
				Severity:   "warning",
				SourceType: "prometheus",
				Labels: map[string]string{
					"businessLineId": "retail",
					"alertCategory":  "business",
					"service":        "checkout",
					"role":           "qa",
				},
			},
		},
		onCallSchedules: []domainalert.OnCallSchedule{
			{ID: "schedule:qa-retail", Name: "Retail QA", TimeZone: "UTC", Enabled: true, CreatedAt: now, UpdatedAt: now},
		},
		onCallRotations: []domainalert.OnCallRotation{
			{ID: "rotation:qa-retail", ScheduleID: "schedule:qa-retail", Name: "Retail QA Rotation", Participants: []string{"qa-a", "qa-b"}, RotationConfig: map[string]any{"shiftHours": 12}, Enabled: true},
		},
		onCallAssignmentRules: []domainalert.OnCallAssignmentRule{
			{ID: "oncall-rule:retail-qa", Name: "Retail QA business alerts", BusinessLineID: "retail", AlertCategory: "business", Service: "checkout", Role: "qa", TargetType: "schedule", TargetRef: "schedule:qa-retail", Priority: 200, Enabled: true},
		},
	}
	service := &Service{
		repo:        repo,
		events:      &stubMonitoringEventWriter{},
		permissions: monitoringCompatPermissions(appaccess.PermObserveOncallView),
	}

	result, err := service.ResolveOnCall(context.Background(), monitoringCompatPrincipal(), domainalert.OnCallResolveInput{AlertID: "evt-qa"})
	if err != nil {
		t.Fatalf("ResolveOnCall returned error: %v", err)
	}
	if got := result["assignmentRuleId"]; got != "oncall-rule:retail-qa" {
		t.Fatalf("assignmentRuleId = %v, want oncall-rule:retail-qa", got)
	}
	if got := result["currentParticipant"]; got != "qa-a" {
		t.Fatalf("currentParticipant = %v, want qa-a", got)
	}
	if got := result["role"]; got != "qa" {
		t.Fatalf("role = %v, want qa", got)
	}
}

func TestServiceListOnCallTasksBuildsTasksFromFiringAlertEvents(t *testing.T) {
	now := time.Now().UTC().Add(-1 * time.Hour)
	repo := &stubMonitoringCompatRepository{
		alertEvents: map[string]domainalert.AlertEvent{
			"evt-payment": {
				ID:           "evt-payment",
				Title:        "Payment checkout latency",
				Summary:      "p95 latency exceeded threshold",
				Severity:     "critical",
				Status:       "firing",
				SourceType:   "prometheus",
				SourceSystem: "prom-main",
				ClusterID:    "prod-a",
				Namespace:    "checkout",
				Labels: map[string]string{
					"service":        "checkout",
					"businessLineId": "payment",
				},
				LastSeenAt: now,
				CreatedAt:  now,
				UpdatedAt:  now,
			},
		},
		onCallSchedules: []domainalert.OnCallSchedule{
			{ID: "schedule:payment-primary", Name: "Payment Primary", TimeZone: "UTC", Enabled: true, CreatedAt: now, UpdatedAt: now},
		},
		onCallRotations: []domainalert.OnCallRotation{
			{ID: "rotation:payment-primary", ScheduleID: "schedule:payment-primary", Name: "Payment Rotation", Participants: []string{"pay-a", "pay-b"}, Enabled: true},
		},
		onCallAssignmentRules: []domainalert.OnCallAssignmentRule{
			{ID: "oncall-route:payment", Name: "Payment route", IntegrationType: "prometheus", Service: "checkout", TargetType: "schedule", TargetRef: "schedule:payment-primary", RouteOrder: 10, GroupBy: []string{"alertName", "service"}, Enabled: true},
		},
	}
	service := &Service{
		repo:        repo,
		events:      &stubMonitoringEventWriter{},
		permissions: monitoringCompatPermissions(appaccess.PermObserveOncallView),
	}

	tasks, err := service.ListOnCallTasks(context.Background(), monitoringCompatPrincipal(), 20)
	if err != nil {
		t.Fatalf("ListOnCallTasks returned error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("len(tasks) = %d, want 1", len(tasks))
	}
	if got := tasks[0].EventID; got != "evt-payment" {
		t.Fatalf("EventID = %v, want evt-payment", got)
	}
	if got := tasks[0].RouteID; got != "oncall-route:payment" {
		t.Fatalf("RouteID = %v, want oncall-route:payment", got)
	}
	if got := tasks[0].CurrentParticipant; got != "pay-a" {
		t.Fatalf("CurrentParticipant = %v, want pay-a", got)
	}
	if got := tasks[0].GroupKey; got != "alertName=Payment checkout latency|service=checkout" {
		t.Fatalf("GroupKey = %v, want alertName=Payment checkout latency|service=checkout", got)
	}
}
