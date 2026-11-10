package monitoring

import (
	"context"
	"fmt"
	"strings"

	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// approvalCandidates on a run is a historical display snapshot. It cannot
// authorize an approver after an on-call rotation or configuration change.
func (s *Service) authorizeHealingApprover(ctx context.Context, principal domainidentity.Principal, policy domainalert.HealingPolicy, event domainalert.AlertEvent) error {
	if strings.TrimSpace(policy.ApprovalPolicyRef) == "" {
		return nil
	}
	participant, err := s.currentHealingApprovalParticipant(ctx, event)
	if err != nil || participant == "" || (participant != principal.UserID && participant != principal.UserName) {
		return fmt.Errorf("%w: current on-call approval is required", apperrors.ErrAccessDenied)
	}
	return nil
}

func (s *Service) currentHealingApprovalParticipant(ctx context.Context, event domainalert.AlertEvent) (string, error) {
	if strings.TrimSpace(event.RuleID) != "" {
		rule, err := s.rules.GetRule(ctx, event.RuleID)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(rule.NotificationPolicyID) != "" {
			policy, err := s.findNotificationPolicy(ctx, rule.NotificationPolicyID)
			if err != nil {
				return "", err
			}
			if strings.TrimSpace(policy.OnCallRef) != "" {
				current, err := s.resolveCurrentOnCall(ctx, policy.OnCallRef)
				if err != nil {
					return "", err
				} // An explicit broken reference never falls back to another schedule.
				return strings.TrimSpace(stringValue(current["currentParticipant"], "")), nil
			}
		}
	}
	current, err := s.resolveOnCallAssignment(ctx, onCallResolveInputFromEvent(event))
	if err != nil {
		return "", err
	}
	if stringValue(current["resolutionStatus"], "") != "matched" {
		return "", apperrors.ErrAccessDenied
	}
	return strings.TrimSpace(stringValue(current["currentParticipant"], "")), nil
}
