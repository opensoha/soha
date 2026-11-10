package monitoring

import (
	"context"
	"errors"
	"testing"
	"time"

	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type healingApprovalRuleReader struct {
	RuleRepository
	rule domainalert.AlertRule
	err  error
}

func (r healingApprovalRuleReader) GetRule(context.Context, string) (domainalert.AlertRule, error) {
	return r.rule, r.err
}

func TestHealingApprovalRequiresCurrentOnCallAndFailsClosed(t *testing.T) {
	now := time.Now().UTC().Add(-time.Hour)
	repo := &stubMonitoringCompatRepository{
		onCallSchedules:       []domainalert.OnCallSchedule{{ID: "schedule:owned", Name: "Owned", TimeZone: "UTC", Enabled: true, CreatedAt: now, UpdatedAt: now}},
		onCallRotations:       []domainalert.OnCallRotation{{ID: "rotation:owned", ScheduleID: "schedule:owned", Participants: []string{"current-user"}, RotationConfig: map[string]any{"shiftHours": 12}, Enabled: true}},
		onCallAssignmentRules: []domainalert.OnCallAssignmentRule{{ID: "assignment:owned", Name: "Owned", Role: "ops", TargetType: "schedule", TargetRef: "schedule:owned", Priority: 100, Enabled: true}},
	}
	service := serviceWithCompatRepository(repo)
	policy := domainalert.HealingPolicy{ApprovalPolicyRef: "required"}
	event := domainalert.AlertEvent{ID: "event:owned", Severity: "critical"}
	repo.alertEvents = map[string]domainalert.AlertEvent{event.ID: event}
	principal := domainidentity.Principal{UserID: "current-user", UserName: "current-name"}
	if err := service.authorizeHealingApprover(t.Context(), principal, policy, event); err != nil {
		t.Fatal(err)
	}
	repo.onCallRotations[0].Participants = []string{"next-user"}
	if err := service.authorizeHealingApprover(t.Context(), principal, policy, event); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("historical candidate retained approval: %v", err)
	}
	principal.UserID = "next-user"
	if err := service.authorizeHealingApprover(t.Context(), principal, policy, event); err != nil {
		t.Fatal(err)
	}
	repo.onCallRotations = nil
	if err := service.authorizeHealingApprover(t.Context(), principal, policy, event); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("empty candidates authorized: %v", err)
	}
	policy.ApprovalPolicyRef = ""
	if err := service.authorizeHealingApprover(t.Context(), principal, policy, event); err != nil {
		t.Fatalf("unrestricted legacy policy changed: %v", err)
	}
}

func TestHealingExplicitBrokenOnCallCannotFallBackToAssignment(t *testing.T) {
	repo := &stubMonitoringCompatRepository{
		onCallSchedules:       []domainalert.OnCallSchedule{{ID: "fallback", Enabled: true, TimeZone: "UTC", CreatedAt: time.Now().Add(-time.Hour)}},
		onCallRotations:       []domainalert.OnCallRotation{{ID: "rotation:fallback", ScheduleID: "fallback", Participants: []string{"actor"}, Enabled: true}},
		onCallAssignmentRules: []domainalert.OnCallAssignmentRule{{ID: "assignment:fallback", Role: "ops", TargetType: "schedule", TargetRef: "fallback", Priority: 100, Enabled: true}},
		listNotificationPoliciesFn: func(context.Context) ([]domainalert.NotificationPolicy, error) {
			return []domainalert.NotificationPolicy{{ID: "notification:owned", OnCallRef: "missing-explicit-schedule"}}, nil
		},
	}
	service := serviceWithCompatRepository(repo)
	service.rules = healingApprovalRuleReader{rule: domainalert.AlertRule{ID: "rule:owned", NotificationPolicyID: "notification:owned"}}
	err := service.authorizeHealingApprover(t.Context(), domainidentity.Principal{UserID: "actor"}, domainalert.HealingPolicy{ApprovalPolicyRef: "required"}, domainalert.AlertEvent{ID: "event", RuleID: "rule:owned", Severity: "critical"})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("explicit broken reference authorized fallback: %v", err)
	}
}
