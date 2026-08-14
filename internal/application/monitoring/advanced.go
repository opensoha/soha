package monitoring

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/telemetry"
)

type DataSourceRepository interface {
	ListDataSources(context.Context) ([]domaincopilot.DataSource, error)
	GetDataSource(context.Context, string) (domaincopilot.DataSource, error)
}

func (s *Service) ListRules(ctx context.Context, principal domainidentity.Principal) ([]domainalert.AlertRule, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertRulesView); err != nil {
		return nil, err
	}
	if s.rules == nil {
		return []domainalert.AlertRule{}, nil
	}
	return s.rules.ListRules(ctx)
}

func (s *Service) GetRule(ctx context.Context, principal domainidentity.Principal, ruleID string) (domainalert.AlertRule, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertRulesView); err != nil {
		return domainalert.AlertRule{}, err
	}
	if s.rules == nil {
		return domainalert.AlertRule{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	return s.rules.GetRule(ctx, strings.TrimSpace(ruleID))
}

func (s *Service) CreateRule(ctx context.Context, principal domainidentity.Principal, input domainalert.AlertRuleInput) (domainalert.AlertRule, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveAlertRulesManage, "create")); err != nil {
		return domainalert.AlertRule{}, err
	}
	if s.rules == nil {
		return domainalert.AlertRule{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if err := validateRuleInput(input); err != nil {
		return domainalert.AlertRule{}, err
	}
	item, err := s.rules.CreateRule(ctx, input)
	if err == nil {
		s.recordMonitoringMutation(ctx, principal, "AlertRule", item.ID, "observability.alert_rule.create", "created alert rule")
	}
	return item, err
}

func (s *Service) UpdateRule(ctx context.Context, principal domainidentity.Principal, ruleID string, input domainalert.AlertRuleInput) (domainalert.AlertRule, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveAlertRulesManage, "update")); err != nil {
		return domainalert.AlertRule{}, err
	}
	if s.rules == nil {
		return domainalert.AlertRule{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if err := validateRuleInput(input); err != nil {
		return domainalert.AlertRule{}, err
	}
	item, err := s.rules.UpdateRule(ctx, strings.TrimSpace(ruleID), input)
	if err == nil {
		s.recordMonitoringMutation(ctx, principal, "AlertRule", item.ID, "observability.alert_rule.update", "updated alert rule")
	}
	return item, err
}

func (s *Service) TestRule(ctx context.Context, principal domainidentity.Principal, input domainalert.AlertRuleInput) (domainalert.RuleTestResult, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertRulesView); err != nil {
		return domainalert.RuleTestResult{}, err
	}
	rule, err := s.normalizeRuleInput(input)
	if err != nil {
		return domainalert.RuleTestResult{}, err
	}
	result, err := s.evaluateRule(ctx, rule)
	if err != nil {
		return domainalert.RuleTestResult{}, err
	}
	if result.Matched && strings.TrimSpace(rule.NotificationPolicyID) != "" {
		policy, policyErr := s.findNotificationPolicy(ctx, rule.NotificationPolicyID)
		if policyErr == nil {
			event := s.previewEventFromRule(rule, result)
			result.NotificationPreview = s.buildNotificationOutputs(ctx, policy, event)
		}
	}
	return result, nil
}

func (s *Service) ListEvents(ctx context.Context, principal domainidentity.Principal, filter domainalert.AlertEventFilter) ([]domainalert.AlertEvent, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertsView); err != nil {
		return nil, err
	}
	if s.alertEvents == nil {
		return []domainalert.AlertEvent{}, nil
	}
	return s.alertEvents.ListEvents(ctx, filter)
}

func (s *Service) GetEvent(ctx context.Context, principal domainidentity.Principal, eventID string) (domainalert.AlertEvent, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertsView); err != nil {
		return domainalert.AlertEvent{}, err
	}
	return s.alertEvents.GetEvent(ctx, strings.TrimSpace(eventID))
}

func (s *Service) AcknowledgeEvent(ctx context.Context, principal domainidentity.Principal, eventID string) (domainalert.AlertEvent, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertsAcknowledge); err != nil {
		return domainalert.AlertEvent{}, err
	}
	item, err := s.alertEvents.GetEvent(ctx, strings.TrimSpace(eventID))
	if err != nil {
		return domainalert.AlertEvent{}, err
	}
	item.CurrentState = "acknowledged"
	item.UpdatedAt = time.Now().UTC()
	updated, err := s.alertEvents.UpdateEvent(ctx, eventID, toAlertEventInput(item))
	if err == nil {
		s.recordMonitoringMutation(ctx, principal, "AlertEvent", updated.ID, "observability.alert_event.acknowledge", "acknowledged alert event")
	}
	return updated, err
}

func (s *Service) ResolveEvent(ctx context.Context, principal domainidentity.Principal, eventID string) (domainalert.AlertEvent, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveAlertsManage, "update")); err != nil {
		return domainalert.AlertEvent{}, err
	}
	item, err := s.alertEvents.GetEvent(ctx, strings.TrimSpace(eventID))
	if err != nil {
		return domainalert.AlertEvent{}, err
	}
	item.Status = "resolved"
	item.CurrentState = "resolved"
	item.EndsAt = time.Now().UTC()
	item.UpdatedAt = time.Now().UTC()
	updated, err := s.alertEvents.UpdateEvent(ctx, eventID, toAlertEventInput(item))
	if err == nil {
		s.recordMonitoringMutation(ctx, principal, "AlertEvent", updated.ID, "observability.alert_event.resolve", "resolved alert event")
	}
	return updated, err
}

func (s *Service) HealEvent(ctx context.Context, principal domainidentity.Principal, eventID string, policyID string) (domainalert.HealingRun, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveHealingManage, "heal")); err != nil {
		return domainalert.HealingRun{}, err
	}
	event, err := s.alertEvents.GetEvent(ctx, strings.TrimSpace(eventID))
	if err != nil {
		return domainalert.HealingRun{}, err
	}
	policy, err := s.healingPolicies.GetHealingPolicy(ctx, strings.TrimSpace(policyID))
	if err != nil {
		return domainalert.HealingRun{}, err
	}
	result := map[string]any{
		"eventId": event.ID,
		"ruleId":  event.RuleID,
		"status":  event.Status,
		"policy":  policy.Name,
	}
	if strings.TrimSpace(event.RuleID) != "" {
		if rule, ruleErr := s.rules.GetRule(ctx, event.RuleID); ruleErr == nil && strings.TrimSpace(rule.NotificationPolicyID) != "" {
			if notificationPolicy, notifyErr := s.findNotificationPolicy(ctx, rule.NotificationPolicyID); notifyErr == nil {
				currentOnCall := s.resolveEventOnCall(ctx, notificationPolicy, event)
				if len(currentOnCall) > 0 {
					result["currentOnCall"] = currentOnCall
				}
				if participant := stringValue(currentOnCall["currentParticipant"], ""); participant != "" {
					result["approvalCandidates"] = []string{participant}
				}
			}
		}
	}
	run := domainalert.HealingRunInput{
		PolicyID:       policy.ID,
		EventID:        event.ID,
		Status:         "pending_approval",
		ApprovalStatus: "pending",
		RequestedBy:    principal.UserID,
		Result:         result,
	}
	created, err := s.healingRuns.CreateHealingRun(ctx, run)
	if err == nil {
		s.recordMonitoringMutation(ctx, principal, "HealingRun", created.ID, "observability.healing_run.create", "requested healing run")
	}
	return created, err
}

func (s *Service) GetHealingRun(ctx context.Context, principal domainidentity.Principal, runID string) (domainalert.HealingRun, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveHealingView); err != nil {
		return domainalert.HealingRun{}, err
	}
	item, err := s.healingRuns.GetHealingRun(ctx, strings.TrimSpace(runID))
	if err != nil {
		return domainalert.HealingRun{}, err
	}
	return s.enrichHealingRun(ctx, item), nil
}

func (s *Service) ApproveHealingRun(ctx context.Context, principal domainidentity.Principal, runID, comment string) (domainalert.HealingRun, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveHealingManage, "approve")); err != nil {
		return domainalert.HealingRun{}, err
	}
	run, err := s.healingRuns.GetHealingRun(ctx, strings.TrimSpace(runID))
	if err != nil {
		return domainalert.HealingRun{}, err
	}
	policy, err := s.healingPolicies.GetHealingPolicy(ctx, run.PolicyID)
	if err != nil {
		return domainalert.HealingRun{}, err
	}
	if strings.TrimSpace(policy.ApprovalPolicyRef) != "" {
		candidates := stringSliceFromAny(run.Result["approvalCandidates"])
		if len(candidates) > 0 && !containsString(candidates, principal.UserID) && !containsString(candidates, principal.UserName) {
			return domainalert.HealingRun{}, fmt.Errorf("%w: current approver is not part of oncall approval candidates", apperrors.ErrAccessDenied)
		}
	}
	event, err := s.alertEvents.GetEvent(ctx, run.EventID)
	if err != nil {
		return domainalert.HealingRun{}, err
	}
	run.Status = "approved"
	run.ApprovalStatus = "approved"
	run.ApprovalComment = strings.TrimSpace(comment)
	run.ApprovedBy = principal.UserID
	run.Result = map[string]any{
		"decision":       "approved",
		"comment":        strings.TrimSpace(comment),
		"approvedBy":     principal.UserName,
		"executionState": "queued",
	}
	if s.workflow != nil && len(policy.Definition) > 0 {
		workflowRun, execErr := s.workflow.ExecuteSystemDAG(ctx, monitoringSystemPrincipal(), "healing:"+policy.ID, firstNonEmpty(policy.Name, policy.WorkflowTemplateID), policy.WorkflowTemplateID, policy.Definition, domainworkflow.Input{
			ApplicationID:  "healing:" + policy.ID,
			WorkflowName:   firstNonEmpty(policy.Name, policy.WorkflowTemplateID),
			ClusterID:      firstNonEmpty(event.ClusterID, stringValue(policy.Definition["clusterId"], "")),
			Namespace:      firstNonEmpty(event.Namespace, stringValue(policy.Definition["namespace"], "")),
			DeploymentName: firstNonEmpty(event.Labels["workload"], event.Labels["deployment"], event.Labels["app"], stringValue(policy.Definition["workloadName"], "")),
		}, map[string]any{
			"healingRunId":    run.ID,
			"healingPolicyId": policy.ID,
			"eventId":         event.ID,
			"healingContext": map[string]any{
				"event":   event,
				"policy":  policy,
				"comment": comment,
			},
		})
		if execErr != nil {
			run.Status = "failed"
			run.Result["executionError"] = execErr.Error()
			run.CompletedAt = time.Now().UTC()
			updated, updateErr := s.healingRuns.UpdateHealingRun(ctx, runID, toHealingRunInput(run))
			if updateErr == nil {
				s.recordMonitoringMutation(ctx, principal, "HealingRun", updated.ID, "observability.healing_run.approve", "approved healing run")
			}
			return updated, updateErr
		}
		run.WorkflowRunID = workflowRun.ID
		run.WorkflowStatus = workflowRun.Status
		run.WorkflowSummary = summarizeWorkflowRun(workflowRun)
		run.Status = workflowRun.Status
		run.Result["workflowRunId"] = workflowRun.ID
		run.Result["workflowStatus"] = workflowRun.Status
		run.Result["workflowSummary"] = run.WorkflowSummary
	}
	updated, err := s.healingRuns.UpdateHealingRun(ctx, runID, toHealingRunInput(run))
	if err != nil {
		return domainalert.HealingRun{}, err
	}
	s.recordMonitoringMutation(ctx, principal, "HealingRun", updated.ID, "observability.healing_run.approve", "approved healing run")
	return s.enrichHealingRun(ctx, updated), nil
}

func (s *Service) RejectHealingRun(ctx context.Context, principal domainidentity.Principal, runID, comment string) (domainalert.HealingRun, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveHealingManage, "reject")); err != nil {
		return domainalert.HealingRun{}, err
	}
	run, err := s.healingRuns.GetHealingRun(ctx, strings.TrimSpace(runID))
	if err != nil {
		return domainalert.HealingRun{}, err
	}
	run.Status = "rejected"
	run.ApprovalStatus = "rejected"
	run.ApprovalComment = strings.TrimSpace(comment)
	run.ApprovedBy = principal.UserID
	run.CompletedAt = time.Now().UTC()
	run.Result = map[string]any{
		"decision": "rejected",
		"comment":  strings.TrimSpace(comment),
	}
	updated, err := s.healingRuns.UpdateHealingRun(ctx, runID, toHealingRunInput(run))
	if err == nil {
		s.recordMonitoringMutation(ctx, principal, "HealingRun", updated.ID, "observability.healing_run.reject", "rejected healing run")
	}
	return updated, err
}

func (s *Service) RetryHealingRun(ctx context.Context, principal domainidentity.Principal, runID string) (domainalert.HealingRun, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveHealingManage, "retry")); err != nil {
		return domainalert.HealingRun{}, err
	}
	run, err := s.healingRuns.GetHealingRun(ctx, strings.TrimSpace(runID))
	if err != nil {
		return domainalert.HealingRun{}, err
	}
	run.Status = "pending_approval"
	run.ApprovalStatus = "pending"
	run.ApprovalComment = ""
	run.ApprovedBy = ""
	run.WorkflowRunID = ""
	run.WorkflowStatus = ""
	run.WorkflowSummary = ""
	run.CompletedAt = time.Time{}
	run.Result = map[string]any{"retryOf": run.ID}
	updated, err := s.healingRuns.UpdateHealingRun(ctx, runID, toHealingRunInput(run))
	if err == nil {
		s.recordMonitoringMutation(ctx, principal, "HealingRun", updated.ID, "observability.healing_run.retry", "retried healing run")
	}
	return updated, err
}

func (s *Service) ListDataSources(ctx context.Context, principal domainidentity.Principal) ([]domaincopilot.DataSource, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertRulesView); err != nil {
		return nil, err
	}
	if s.dataSources == nil {
		return []domaincopilot.DataSource{}, nil
	}
	return s.dataSources.ListDataSources(ctx)
}

func (s *Service) GetDataSource(ctx context.Context, principal domainidentity.Principal, dataSourceID string) (domaincopilot.DataSource, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertRulesView); err != nil {
		return domaincopilot.DataSource{}, err
	}
	if s.dataSources == nil {
		return domaincopilot.DataSource{}, fmt.Errorf("%w: datasource repository is not configured", apperrors.ErrInvalidArgument)
	}
	return s.dataSources.GetDataSource(ctx, strings.TrimSpace(dataSourceID))
}

func (s *Service) ListNotificationPolicies(ctx context.Context, principal domainidentity.Principal) ([]domainalert.NotificationPolicy, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsView); err != nil {
		return nil, err
	}
	return s.notificationPolicies.ListNotificationPolicies(ctx)
}

func (s *Service) CreateNotificationPolicy(ctx context.Context, principal domainidentity.Principal, input domainalert.NotificationPolicyInput) (domainalert.NotificationPolicy, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveNotificationsManage, "create")); err != nil {
		return domainalert.NotificationPolicy{}, err
	}
	if err := validateNotificationPolicyInput(input); err != nil {
		return domainalert.NotificationPolicy{}, err
	}
	item, err := s.notificationPolicies.CreateNotificationPolicy(ctx, input)
	if err == nil {
		s.recordMonitoringMutation(ctx, principal, "NotificationPolicy", item.ID, "observability.notification_policy.create", "created notification policy")
	}
	return item, err
}

func (s *Service) UpdateNotificationPolicy(ctx context.Context, principal domainidentity.Principal, policyID string, input domainalert.NotificationPolicyInput) (domainalert.NotificationPolicy, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveNotificationsManage, "update")); err != nil {
		return domainalert.NotificationPolicy{}, err
	}
	if err := validateNotificationPolicyInput(input); err != nil {
		return domainalert.NotificationPolicy{}, err
	}
	item, err := s.notificationPolicies.UpdateNotificationPolicy(ctx, policyID, input)
	if err == nil {
		s.recordMonitoringMutation(ctx, principal, "NotificationPolicy", item.ID, "observability.notification_policy.update", "updated notification policy")
	}
	return item, err
}

func (s *Service) ListNotificationTemplates(ctx context.Context, principal domainidentity.Principal) ([]domainalert.NotificationTemplate, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsView); err != nil {
		return nil, err
	}
	return s.notificationTemplates.ListNotificationTemplates(ctx)
}

func (s *Service) CreateNotificationTemplate(ctx context.Context, principal domainidentity.Principal, input domainalert.NotificationTemplateInput) (domainalert.NotificationTemplate, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveNotificationsManage, "create")); err != nil {
		return domainalert.NotificationTemplate{}, err
	}
	if err := validateNotificationTemplateInput(input); err != nil {
		return domainalert.NotificationTemplate{}, err
	}
	item, err := s.notificationTemplates.CreateNotificationTemplate(ctx, input)
	if err == nil {
		s.recordMonitoringMutation(ctx, principal, "NotificationTemplate", item.ID, "observability.notification_template.create", "created notification template")
	}
	return item, err
}

func (s *Service) UpdateNotificationTemplate(ctx context.Context, principal domainidentity.Principal, templateID string, input domainalert.NotificationTemplateInput) (domainalert.NotificationTemplate, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveNotificationsManage, "update")); err != nil {
		return domainalert.NotificationTemplate{}, err
	}
	if err := validateNotificationTemplateInput(input); err != nil {
		return domainalert.NotificationTemplate{}, err
	}
	item, err := s.notificationTemplates.UpdateNotificationTemplate(ctx, templateID, input)
	if err == nil {
		s.recordMonitoringMutation(ctx, principal, "NotificationTemplate", item.ID, "observability.notification_template.update", "updated notification template")
	}
	return item, err
}

func (s *Service) ListHealingPolicies(ctx context.Context, principal domainidentity.Principal) ([]domainalert.HealingPolicy, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveHealingView); err != nil {
		return nil, err
	}
	return s.healingPolicies.ListHealingPolicies(ctx)
}

func (s *Service) CreateHealingPolicy(ctx context.Context, principal domainidentity.Principal, input domainalert.HealingPolicyInput) (domainalert.HealingPolicy, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveHealingManage, "create")); err != nil {
		return domainalert.HealingPolicy{}, err
	}
	if err := validateHealingPolicyInput(input); err != nil {
		return domainalert.HealingPolicy{}, err
	}
	item, err := s.healingPolicies.CreateHealingPolicy(ctx, input)
	if err == nil {
		s.recordMonitoringMutation(ctx, principal, "HealingPolicy", item.ID, "observability.healing_policy.create", "created healing policy")
	}
	return item, err
}

func (s *Service) UpdateHealingPolicy(ctx context.Context, principal domainidentity.Principal, policyID string, input domainalert.HealingPolicyInput) (domainalert.HealingPolicy, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveHealingManage, "update")); err != nil {
		return domainalert.HealingPolicy{}, err
	}
	if err := validateHealingPolicyInput(input); err != nil {
		return domainalert.HealingPolicy{}, err
	}
	item, err := s.healingPolicies.UpdateHealingPolicy(ctx, policyID, input)
	if err == nil {
		s.recordMonitoringMutation(ctx, principal, "HealingPolicy", item.ID, "observability.healing_policy.update", "updated healing policy")
	}
	return item, err
}

func (s *Service) ListHealingRuns(ctx context.Context, principal domainidentity.Principal, filter domainalert.HealingRunFilter) ([]domainalert.HealingRun, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveHealingView); err != nil {
		return nil, err
	}
	items, err := s.healingRuns.ListHealingRuns(ctx, filter)
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index] = s.enrichHealingRun(ctx, items[index])
	}
	return items, nil
}

func (s *Service) ListOnCallSchedules(ctx context.Context, principal domainidentity.Principal) ([]domainalert.OnCallSchedule, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveOncallView); err != nil {
		return nil, err
	}
	return s.onCallSchedules.ListOnCallSchedules(ctx)
}

func (s *Service) CreateOnCallSchedule(ctx context.Context, principal domainidentity.Principal, input domainalert.OnCallScheduleInput) (domainalert.OnCallSchedule, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveOncallManage, "create")); err != nil {
		return domainalert.OnCallSchedule{}, err
	}
	item, err := s.onCallSchedules.CreateOnCallSchedule(ctx, input)
	if err == nil {
		s.recordMonitoringMutation(ctx, principal, "OnCallSchedule", item.ID, "observability.oncall_schedule.create", "created on-call schedule")
	}
	return item, err
}

func (s *Service) UpdateOnCallSchedule(ctx context.Context, principal domainidentity.Principal, scheduleID string, input domainalert.OnCallScheduleInput) (domainalert.OnCallSchedule, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveOncallManage, "update")); err != nil {
		return domainalert.OnCallSchedule{}, err
	}
	item, err := s.onCallSchedules.UpdateOnCallSchedule(ctx, scheduleID, input)
	if err == nil {
		s.recordMonitoringMutation(ctx, principal, "OnCallSchedule", item.ID, "observability.oncall_schedule.update", "updated on-call schedule")
	}
	return item, err
}

func (s *Service) ListOnCallRotations(ctx context.Context, principal domainidentity.Principal) ([]domainalert.OnCallRotation, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveOncallView); err != nil {
		return nil, err
	}
	return s.onCallRotations.ListOnCallRotations(ctx)
}

func (s *Service) CreateOnCallRotation(ctx context.Context, principal domainidentity.Principal, input domainalert.OnCallRotationInput) (domainalert.OnCallRotation, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveOncallManage, "create")); err != nil {
		return domainalert.OnCallRotation{}, err
	}
	item, err := s.onCallRotations.CreateOnCallRotation(ctx, input)
	if err == nil {
		s.recordMonitoringMutation(ctx, principal, "OnCallRotation", item.ID, "observability.oncall_rotation.create", "created on-call rotation")
	}
	return item, err
}

func (s *Service) UpdateOnCallRotation(ctx context.Context, principal domainidentity.Principal, rotationID string, input domainalert.OnCallRotationInput) (domainalert.OnCallRotation, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveOncallManage, "update")); err != nil {
		return domainalert.OnCallRotation{}, err
	}
	item, err := s.onCallRotations.UpdateOnCallRotation(ctx, rotationID, input)
	if err == nil {
		s.recordMonitoringMutation(ctx, principal, "OnCallRotation", item.ID, "observability.oncall_rotation.update", "updated on-call rotation")
	}
	return item, err
}

func (s *Service) ListOnCallEscalationPolicies(ctx context.Context, principal domainidentity.Principal) ([]domainalert.OnCallEscalationPolicy, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveOncallView); err != nil {
		return nil, err
	}
	return s.onCallEscalations.ListOnCallEscalationPolicies(ctx)
}

func (s *Service) CreateOnCallEscalationPolicy(ctx context.Context, principal domainidentity.Principal, input domainalert.OnCallEscalationPolicyInput) (domainalert.OnCallEscalationPolicy, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveOncallManage, "create")); err != nil {
		return domainalert.OnCallEscalationPolicy{}, err
	}
	item, err := s.onCallEscalations.CreateOnCallEscalationPolicy(ctx, input)
	if err == nil {
		s.recordMonitoringMutation(ctx, principal, "OnCallEscalationPolicy", item.ID, "observability.oncall_escalation.create", "created on-call escalation policy")
	}
	return item, err
}

func (s *Service) UpdateOnCallEscalationPolicy(ctx context.Context, principal domainidentity.Principal, policyID string, input domainalert.OnCallEscalationPolicyInput) (domainalert.OnCallEscalationPolicy, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveOncallManage, "update")); err != nil {
		return domainalert.OnCallEscalationPolicy{}, err
	}
	item, err := s.onCallEscalations.UpdateOnCallEscalationPolicy(ctx, policyID, input)
	if err == nil {
		s.recordMonitoringMutation(ctx, principal, "OnCallEscalationPolicy", item.ID, "observability.oncall_escalation.update", "updated on-call escalation policy")
	}
	return item, err
}

func (s *Service) ListOnCallAssignmentRules(ctx context.Context, principal domainidentity.Principal) ([]domainalert.OnCallAssignmentRule, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveOncallView); err != nil {
		return nil, err
	}
	return s.onCallAssignments.ListOnCallAssignmentRules(ctx)
}

func (s *Service) CreateOnCallAssignmentRule(ctx context.Context, principal domainidentity.Principal, input domainalert.OnCallAssignmentRuleInput) (domainalert.OnCallAssignmentRule, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveOncallManage, "create")); err != nil {
		return domainalert.OnCallAssignmentRule{}, err
	}
	if err := validateOnCallAssignmentRuleInput(input); err != nil {
		return domainalert.OnCallAssignmentRule{}, err
	}
	item, err := s.onCallAssignments.CreateOnCallAssignmentRule(ctx, input)
	if err == nil {
		s.recordMonitoringMutation(ctx, principal, "OnCallAssignmentRule", item.ID, "observability.oncall_assignment.create", "created on-call assignment rule")
	}
	return item, err
}

func (s *Service) UpdateOnCallAssignmentRule(ctx context.Context, principal domainidentity.Principal, ruleID string, input domainalert.OnCallAssignmentRuleInput) (domainalert.OnCallAssignmentRule, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveOncallManage, "update")); err != nil {
		return domainalert.OnCallAssignmentRule{}, err
	}
	if err := validateOnCallAssignmentRuleInput(input); err != nil {
		return domainalert.OnCallAssignmentRule{}, err
	}
	item, err := s.onCallAssignments.UpdateOnCallAssignmentRule(ctx, ruleID, input)
	if err == nil {
		s.recordMonitoringMutation(ctx, principal, "OnCallAssignmentRule", item.ID, "observability.oncall_assignment.update", "updated on-call assignment rule")
	}
	return item, err
}

func (s *Service) ResolveOnCall(ctx context.Context, principal domainidentity.Principal, input domainalert.OnCallResolveInput) (map[string]any, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveOncallView); err != nil {
		return nil, err
	}
	return s.resolveOnCallAssignment(ctx, input)
}

func (s *Service) ListOnCallTasks(ctx context.Context, principal domainidentity.Principal, limit int) ([]domainalert.OnCallTask, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveOncallView); err != nil {
		return nil, err
	}
	if s.alertEvents == nil {
		return []domainalert.OnCallTask{}, nil
	}
	events, err := s.alertEvents.ListEvents(ctx, domainalert.AlertEventFilter{Status: "firing", Limit: limit})
	if err != nil {
		return nil, err
	}
	tasks := make([]domainalert.OnCallTask, 0, len(events))
	for _, event := range events {
		tasks = append(tasks, s.buildOnCallTask(ctx, event))
	}
	return tasks, nil
}

func (s *Service) CreateWorkflowSilence(ctx context.Context, principal domainidentity.Principal, input domainalert.SilenceInput) (domainalert.AlertSilence, error) {
	if s.silences == nil {
		return domainalert.AlertSilence{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if err := validateSilenceInput(input); err != nil {
		return domainalert.AlertSilence{}, err
	}
	item, err := s.silences.CreateSilence(ctx, input)
	if err == nil {
		s.recordMonitoringMutation(ctx, principal, "AlertSilence", item.ID, "observability.alert_silence.create", "created workflow silence")
	}
	return item, err
}

func (s *Service) normalizeRuleInput(input domainalert.AlertRuleInput) (domainalert.AlertRule, error) {
	if err := validateRuleInput(input); err != nil {
		return domainalert.AlertRule{}, err
	}
	return normalizeRulePreview(input), nil
}

func (s *Service) evaluateRule(ctx context.Context, rule domainalert.AlertRule) (domainalert.RuleTestResult, error) {
	result := domainalert.RuleTestResult{
		RuleID:     rule.ID,
		RuleType:   rule.RuleType,
		State:      "no_data",
		ExecutedAt: time.Now().UTC(),
	}
	if s.dataSources == nil {
		result.Summary = "no data source repository configured"
		result.QuerySnapshot = buildRuleQuerySnapshot(rule, result.ExecutedAt, nil)
		return result, nil
	}
	dataSources, err := s.dataSources.ListDataSources(ctx)
	if err != nil {
		result.QuerySnapshot = buildRuleQuerySnapshot(rule, result.ExecutedAt, nil)
		return result, err
	}
	selected := filterDataSources(dataSources, rule.DatasourceSelector)
	result.DataSources = make([]string, 0, len(selected))
	for _, source := range selected {
		result.DataSources = append(result.DataSources, source.ID)
	}
	var evaluated domainalert.RuleTestResult
	switch rule.RuleType {
	case "metrics":
		evaluated, err = s.evaluateMetricRule(ctx, rule, selected)
	case "logs":
		evaluated, err = s.evaluateLogRule(ctx, rule, selected)
	case "traces":
		evaluated, err = s.evaluateTraceRule(ctx, rule, selected)
	default:
		result.State = "clear"
		result.Summary = "external passthrough rule validated only"
		evaluated = result
	}
	evaluated.QuerySnapshot = buildRuleQuerySnapshot(rule, evaluated.ExecutedAt, selected)
	return evaluated, err
}

func buildRuleQuerySnapshot(rule domainalert.AlertRule, executedAt time.Time, sources []domaincopilot.DataSource) map[string]any {
	if rule.RuleType != "metrics" && rule.RuleType != "logs" && rule.RuleType != "traces" {
		return nil
	}
	if executedAt.IsZero() {
		executedAt = time.Now().UTC()
	}
	windowMinutes := intValue(rule.QuerySpec["windowMinutes"], 60)
	if windowMinutes <= 0 {
		windowMinutes = 60
	}
	scope := map[string]any{}
	for _, key := range []string{"workspaceId", "clusterId", "environment", "namespace", "workload", "service"} {
		if value := strings.TrimSpace(stringValue(rule.DatasourceSelector[key], "")); value != "" {
			scope[key] = value
		}
	}
	snapshot := map[string]any{
		"version": "v1",
		"signal":  rule.RuleType,
		"context": map[string]any{
			"version": "v1",
			"scope":   scope,
			"timeRange": map[string]any{
				"from": executedAt.Add(-time.Duration(windowMinutes) * time.Minute).Format(time.RFC3339Nano),
				"to":   executedAt.Format(time.RFC3339Nano),
			},
		},
		"createdAt": executedAt.Format(time.RFC3339Nano),
	}
	matching := make([]domaincopilot.DataSource, 0, len(sources))
	for _, source := range sources {
		if source.Enabled && source.SourceKind == rule.RuleType {
			matching = append(matching, source)
		}
	}
	if len(matching) == 1 {
		snapshot["dataSourceId"] = matching[0].ID
		snapshot["backendType"] = matching[0].BackendType
	}
	query := applyRuleQuerySnapshotDetails(snapshot, rule, matching)
	if query != "" {
		snapshot["query"] = query
	}
	return snapshot
}

func applyRuleQuerySnapshotDetails(snapshot map[string]any, rule domainalert.AlertRule, matching []domaincopilot.DataSource) string {
	query := strings.TrimSpace(stringValue(rule.QuerySpec["query"], ""))
	switch rule.RuleType {
	case "metrics":
		if metricKey := strings.TrimSpace(stringValue(rule.QuerySpec["metricKey"], "")); metricKey != "" {
			snapshot["metricKey"] = metricKey
			snapshot["queryLanguage"] = "metric_key"
		} else if query != "" {
			snapshot["queryLanguage"] = "promql"
		}
	case "logs":
		if query == "" {
			query = strings.TrimSpace(stringValue(rule.QuerySpec["pattern"], ""))
		}
		if len(matching) == 1 {
			switch matching[0].BackendType {
			case "loki":
				snapshot["queryLanguage"] = "logql"
			case "es", "elasticsearch":
				snapshot["queryLanguage"] = "elasticsearch"
			case "clickhouse":
				snapshot["queryLanguage"] = "clickhouse_sql"
			}
		}
	case "traces":
		if query != "" {
			snapshot["queryLanguage"] = "trace_filter"
		}
	}
	return query
}

func (s *Service) evaluateMetricRule(ctx context.Context, rule domainalert.AlertRule, sources []domaincopilot.DataSource) (domainalert.RuleTestResult, error) {
	result := newRuleTestResult(rule, sources, "metrics")
	metricKey := strings.TrimSpace(stringValue(rule.QuerySpec["metricKey"], ""))
	expression := strings.TrimSpace(stringValue(rule.QuerySpec["query"], ""))
	if metricKey == "" && expression == "" {
		metricKey = "cpu_usage"
	}
	if len(sources) == 0 {
		result.Summary = "no matching metrics data source found"
		return result, nil
	}
	threshold, err := parseMetricThreshold(rule.ThresholdSpec)
	if err != nil {
		return result, err
	}
	scope := telemetry.MetricScope{
		ClusterID: stringValue(rule.DatasourceSelector["clusterId"], ""),
		Namespace: stringValue(rule.DatasourceSelector["namespace"], ""),
		Workload:  stringValue(rule.DatasourceSelector["workload"], ""),
	}
	succeeded := 0
	hasData := false
	for _, source := range sources {
		if source.SourceKind != "metrics" {
			continue
		}
		summary, queryErr := s.metricBackend().Analyze(ctx, source.BackendType, source.ID, source.Config, telemetry.MetricRangeQuery{
			Scope:      scope,
			MetricKey:  metricKey,
			Expression: expression,
			TimeFrom:   time.Now().UTC().Add(-time.Duration(intValue(rule.QuerySpec["windowMinutes"], 60)) * time.Minute),
			TimeTo:     time.Now().UTC(),
			Step:       time.Duration(intValue(rule.QuerySpec["stepSeconds"], 60)) * time.Second,
		})
		if queryErr != nil {
			result.Errors = append(result.Errors, source.ID+": "+queryErr.Error())
			continue
		}
		succeeded++
		if len(summary.Signals) == 0 && len(summary.Series) == 0 {
			continue
		}
		samples, matched := metricRuleSamples(source.ID, summary, scope, threshold)
		result.Matched = result.Matched || matched
		if len(samples) == 0 {
			continue
		}
		hasData = true
		result.Samples = append(result.Samples, samples...)
		result.Summary = summary.Summary
	}
	result.State = ruleEvaluationState(result.Matched, succeeded, hasData, result.Errors)
	if threshold.configured || result.Summary == "" {
		result.Summary = ruleEvaluationSummary("metrics", result.State)
	}
	return result, nil
}

func metricRuleSamples(sourceID string, summary telemetry.MetricAnomalySummary, scope telemetry.MetricScope, threshold metricThreshold) ([]map[string]any, bool) {
	samples := make([]map[string]any, 0, len(summary.Signals))
	matchedAny := false
	for _, signal := range summary.Signals {
		matched, evaluated := metricSignalMatches(signal, threshold)
		if !evaluated {
			continue
		}
		sample := map[string]any{
			"dataSourceId": sourceID,
			"summary":      summary.Summary,
			"signals":      summary.Signals,
			"series":       summary.Series,
			"labels":       metricRuleScopeLabels(scope),
			"matched":      matched,
		}
		for key, value := range signal {
			sample[key] = value
		}
		matchedAny = matchedAny || matched
		samples = append(samples, sample)
	}
	if len(samples) > 0 {
		return samples, matchedAny
	}
	for _, series := range summary.Series {
		signal := map[string]any{"latest": series.Latest, "metricKey": series.Key, "label": series.Label}
		matched, evaluated := metricSignalMatches(signal, threshold)
		if !evaluated {
			continue
		}
		samples = append(samples, map[string]any{
			"dataSourceId": sourceID,
			"summary":      summary.Summary,
			"signals":      summary.Signals,
			"series":       []telemetry.MetricSeries{series},
			"metricKey":    series.Key,
			"label":        series.Label,
			"latest":       series.Latest,
			"matched":      matched,
			"labels":       metricRuleScopeLabels(scope),
		})
		matchedAny = matchedAny || matched
	}
	return samples, matchedAny
}

func metricRuleScopeLabels(scope telemetry.MetricScope) map[string]any {
	return map[string]any{
		"clusterId": scope.ClusterID,
		"namespace": scope.Namespace,
		"workload":  scope.Workload,
	}
}

func (s *Service) evaluateLogRule(ctx context.Context, rule domainalert.AlertRule, sources []domaincopilot.DataSource) (domainalert.RuleTestResult, error) {
	result := newRuleTestResult(rule, sources, "logs")
	if len(sources) == 0 {
		result.Summary = "no matching logs data source found"
		return result, nil
	}
	scope := telemetry.LogScope{
		ClusterID: stringValue(rule.DatasourceSelector["clusterId"], ""),
		Namespace: stringValue(rule.DatasourceSelector["namespace"], ""),
		Workload:  stringValue(rule.DatasourceSelector["workload"], ""),
		Service:   stringValue(rule.DatasourceSelector["service"], ""),
	}
	query := stringValue(rule.QuerySpec["query"], "")
	if query == "" {
		query = stringValue(rule.QuerySpec["pattern"], "")
	}
	succeeded := 0
	hasData := false
	for _, source := range sources {
		if source.SourceKind != "logs" {
			continue
		}
		correlation, queryErr := s.logBackend().Correlate(ctx, source.BackendType, source.ID, source.Config, telemetry.LogCorrelationQuery{
			Scope:    scope,
			Query:    query,
			TimeFrom: time.Now().UTC().Add(-time.Duration(intValue(rule.QuerySpec["windowMinutes"], 60)) * time.Minute),
			TimeTo:   time.Now().UTC(),
			Limit:    intValue(rule.ThresholdSpec["minCount"], 20),
		})
		if queryErr != nil {
			result.Errors = append(result.Errors, source.ID+": "+queryErr.Error())
			continue
		}
		succeeded++
		hasData = true
		if len(correlation.Records) == 0 && len(correlation.Signatures) == 0 {
			continue
		}
		result.Matched = true
		result.Summary = correlation.Summary
		result.Samples = append(result.Samples, map[string]any{
			"dataSourceId": source.ID, "summary": correlation.Summary, "truncated": correlation.Truncated,
		})
	}
	result.State = ruleEvaluationState(result.Matched, succeeded, hasData, result.Errors)
	if result.Summary == "" {
		result.Summary = ruleEvaluationSummary("logs", result.State)
	}
	return result, nil
}

func (s *Service) evaluateTraceRule(ctx context.Context, rule domainalert.AlertRule, sources []domaincopilot.DataSource) (domainalert.RuleTestResult, error) {
	result := newRuleTestResult(rule, sources, "traces")
	if len(sources) == 0 {
		result.Summary = "no matching traces data source found"
		return result, nil
	}
	scope := telemetry.TraceScope{
		ClusterID: stringValue(rule.DatasourceSelector["clusterId"], ""),
		Namespace: stringValue(rule.DatasourceSelector["namespace"], ""),
		Workload:  stringValue(rule.DatasourceSelector["workload"], ""),
		Service:   stringValue(rule.DatasourceSelector["service"], ""),
	}
	succeeded := 0
	hasData := false
	for _, source := range sources {
		if source.SourceKind != "traces" {
			continue
		}
		traceResult, queryErr := s.traceBackend().FindSlowSpans(ctx, source.BackendType, source.ID, source.Config, telemetry.TraceQuery{
			Scope:       scope,
			TimeFrom:    time.Now().UTC().Add(-time.Duration(intValue(rule.QuerySpec["windowMinutes"], 60)) * time.Minute),
			TimeTo:      time.Now().UTC(),
			MinDuration: time.Duration(intValue(rule.ThresholdSpec["minDurationMs"], 250)) * time.Millisecond,
			Limit:       intValue(rule.ThresholdSpec["sampleLimit"], 20),
		})
		if queryErr != nil {
			result.Errors = append(result.Errors, source.ID+": "+queryErr.Error())
			continue
		}
		succeeded++
		hasData = true
		if len(traceResult.Spans) == 0 {
			continue
		}
		result.Matched = true
		result.Summary = traceResult.Summary
		result.Samples = append(result.Samples, map[string]any{
			"dataSourceId": source.ID, "summary": traceResult.Summary, "spanCount": len(traceResult.Spans),
		})
	}
	result.State = ruleEvaluationState(result.Matched, succeeded, hasData, result.Errors)
	if result.Summary == "" {
		result.Summary = ruleEvaluationSummary("traces", result.State)
	}
	return result, nil
}

type metricThreshold struct {
	configured bool
	operator   string
	reducer    string
	value      float64
	lower      float64
	upper      float64
}

func parseMetricThreshold(spec map[string]any) (metricThreshold, error) {
	operator := strings.ToLower(strings.TrimSpace(stringValue(spec["operator"], "")))
	if operator == "" {
		return metricThreshold{}, nil
	}
	aliases := map[string]string{
		">": "gt", ">=": "gte", "<": "lt", "<=": "lte", "==": "eq", "=": "eq",
	}
	if normalized, ok := aliases[operator]; ok {
		operator = normalized
	}
	threshold := metricThreshold{configured: true, operator: operator, reducer: strings.ToLower(stringValue(spec["reducer"], "last"))}
	switch threshold.reducer {
	case "last":
		threshold.reducer = "latest"
	case "avg":
		threshold.reducer = "average"
	case "latest", "average", "max", "min", "sum", "count":
	default:
		return metricThreshold{}, fmt.Errorf("%w: unsupported metric threshold reducer %q", apperrors.ErrInvalidArgument, threshold.reducer)
	}
	if operator == "outside_range" {
		lower, lowerOK := numberValue(spec["lower"])
		upper, upperOK := numberValue(spec["upper"])
		if !lowerOK || !upperOK || lower > upper {
			return metricThreshold{}, fmt.Errorf("%w: outside_range requires lower <= upper", apperrors.ErrInvalidArgument)
		}
		threshold.lower, threshold.upper = lower, upper
		return threshold, nil
	}
	if operator != "gt" && operator != "gte" && operator != "lt" && operator != "lte" && operator != "eq" {
		return metricThreshold{}, fmt.Errorf("%w: unsupported metric threshold operator %q", apperrors.ErrInvalidArgument, operator)
	}
	value, ok := numberValue(spec["value"])
	if !ok {
		return metricThreshold{}, fmt.Errorf("%w: metric threshold value is required", apperrors.ErrInvalidArgument)
	}
	threshold.value = value
	return threshold, nil
}

func metricSignalMatches(signal map[string]any, threshold metricThreshold) (bool, bool) {
	if !threshold.configured {
		trend := strings.TrimSpace(fmt.Sprint(signal["trend"]))
		return trend != "" && trend != "stable", trend != ""
	}
	actual, ok := numberValue(signal[threshold.reducer])
	if !ok {
		return false, false
	}
	switch threshold.operator {
	case "gt":
		return actual > threshold.value, true
	case "gte":
		return actual >= threshold.value, true
	case "lt":
		return actual < threshold.value, true
	case "lte":
		return actual <= threshold.value, true
	case "eq":
		return actual == threshold.value, true
	case "outside_range":
		return actual < threshold.lower || actual > threshold.upper, true
	default:
		return false, false
	}
}

func numberValue(value any) (float64, bool) {
	switch current := value.(type) {
	case float64:
		return current, true
	case float32:
		return float64(current), true
	case int:
		return float64(current), true
	case int64:
		return float64(current), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(current), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func newRuleTestResult(rule domainalert.AlertRule, sources []domaincopilot.DataSource, sourceKind string) domainalert.RuleTestResult {
	result := domainalert.RuleTestResult{RuleID: rule.ID, RuleType: rule.RuleType, State: "no_data", ExecutedAt: time.Now().UTC()}
	for _, source := range sources {
		if source.Enabled && source.SourceKind == sourceKind {
			result.DataSources = append(result.DataSources, source.ID)
		}
	}
	return result
}

func ruleEvaluationState(matched bool, succeeded int, hasData bool, errors []string) string {
	if succeeded == 0 && len(errors) > 0 {
		return "error"
	}
	if len(errors) > 0 {
		return "partial"
	}
	if !hasData {
		return "no_data"
	}
	if matched {
		return "matched"
	}
	return "clear"
}

func ruleEvaluationSummary(signal, state string) string {
	switch state {
	case "error":
		return signal + " query failed"
	case "partial":
		return signal + " query returned partial data"
	case "no_data":
		return "no " + signal + " data available"
	case "clear":
		return signal + " threshold is clear"
	default:
		return signal + " threshold matched"
	}
}

func validateRuleInput(input domainalert.AlertRuleInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return fmt.Errorf("%w: alert rule name is required", apperrors.ErrInvalidArgument)
	}
	ruleType := strings.ToLower(strings.TrimSpace(input.RuleType))
	switch ruleType {
	case "metrics", "logs", "traces", "external_passthrough":
	default:
		return fmt.Errorf("%w: unsupported alert rule type %q", apperrors.ErrInvalidArgument, input.RuleType)
	}
	return nil
}

func validateNotificationPolicyInput(input domainalert.NotificationPolicyInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return fmt.Errorf("%w: notification policy name is required", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validateNotificationTemplateInput(input domainalert.NotificationTemplateInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return fmt.Errorf("%w: notification template name is required", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validateHealingPolicyInput(input domainalert.HealingPolicyInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return fmt.Errorf("%w: healing policy name is required", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(input.WorkflowTemplateID) == "" {
		return fmt.Errorf("%w: workflowTemplateId is required", apperrors.ErrInvalidArgument)
	}
	if len(input.Definition) == 0 {
		return fmt.Errorf("%w: healing workflow definition is required", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validateOnCallAssignmentRuleInput(input domainalert.OnCallAssignmentRuleInput) error {
	if strings.TrimSpace(input.Name) == "" {
		return fmt.Errorf("%w: oncall assignment rule name is required", apperrors.ErrInvalidArgument)
	}
	targetType := strings.ToLower(strings.TrimSpace(input.TargetType))
	if targetType == "" {
		targetType = "escalation"
	}
	switch targetType {
	case "schedule", "escalation":
	default:
		return fmt.Errorf("%w: targetType must be schedule or escalation chain", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(input.TargetRef) == "" {
		return fmt.Errorf("%w: targetRef is required", apperrors.ErrInvalidArgument)
	}
	return nil
}

func filterDataSources(items []domaincopilot.DataSource, selector map[string]any) []domaincopilot.DataSource {
	if len(items) == 0 {
		return items
	}
	if len(selector) == 0 {
		return items
	}
	filtered := make([]domaincopilot.DataSource, 0, len(items))
	wantedIDs := stringSliceValue(selector["datasourceIds"])
	wantedKind := strings.TrimSpace(stringValue(selector["sourceKind"], ""))
	wantedBackend := strings.TrimSpace(stringValue(selector["backendType"], ""))
	for _, item := range items {
		if !item.Enabled {
			continue
		}
		if len(wantedIDs) > 0 && !containsString(wantedIDs, item.ID) {
			continue
		}
		if wantedKind != "" && !strings.EqualFold(wantedKind, item.SourceKind) {
			continue
		}
		if wantedBackend != "" && !strings.EqualFold(wantedBackend, item.BackendType) {
			continue
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func normalizeRulePreview(input domainalert.AlertRuleInput) domainalert.AlertRule {
	return domainalert.AlertRule{
		ID:                   strings.TrimSpace(input.ID),
		Name:                 strings.TrimSpace(input.Name),
		RuleType:             strings.ToLower(strings.TrimSpace(input.RuleType)),
		DatasourceSelector:   input.DatasourceSelector,
		QuerySpec:            input.QuerySpec,
		ThresholdSpec:        input.ThresholdSpec,
		ForSeconds:           input.ForSeconds,
		GroupBy:              input.GroupBy,
		Labels:               input.Labels,
		Annotations:          input.Annotations,
		NotificationPolicyID: strings.TrimSpace(input.NotificationPolicyID),
		HealingPolicyIDs:     input.HealingPolicyIDs,
		Enabled:              input.Enabled,
	}
}

func (s *Service) previewEventFromRule(rule domainalert.AlertRule, result domainalert.RuleTestResult) domainalert.AlertEvent {
	fingerprint := internalRuleFingerprint(rule.ID, nil)
	return domainalert.AlertEvent{
		ID:           internalRuleEventID(rule, fingerprint),
		RuleID:       rule.ID,
		SourceType:   "internal_rule",
		SourceSystem: "soha",
		Fingerprint:  fingerprint,
		Title:        firstNonEmpty(strings.TrimSpace(rule.Name), "Alert Rule"),
		Summary:      firstNonEmpty(result.Summary, rule.Name),
		Severity:     firstNonEmpty(normalizeRuleSeverity(rule, result), "warning"),
		Status:       "firing",
		ClusterID:    stringValue(rule.DatasourceSelector["clusterId"], ""),
		Namespace:    stringValue(rule.DatasourceSelector["namespace"], ""),
		Labels: mergeLabelMaps(rule.Labels, map[string]string{
			"ruleId":   rule.ID,
			"ruleType": rule.RuleType,
		}),
		Annotations: mergeLabelMaps(rule.Annotations, map[string]string{
			"ruleSummary": result.Summary,
		}),
		QuerySnapshot: result.QuerySnapshot,
		CurrentState:  "firing",
		LastSeenAt:    time.Now().UTC(),
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}
}

func toAlertEventInput(item domainalert.AlertEvent) domainalert.AlertEventInput {
	return domainalert.AlertEventInput{
		ID:                 item.ID,
		RuleID:             item.RuleID,
		SourceType:         item.SourceType,
		SourceSystem:       item.SourceSystem,
		Fingerprint:        item.Fingerprint,
		Title:              item.Title,
		Summary:            item.Summary,
		Severity:           item.Severity,
		Status:             item.Status,
		ClusterID:          item.ClusterID,
		Namespace:          item.Namespace,
		Labels:             item.Labels,
		Annotations:        item.Annotations,
		QuerySnapshot:      item.QuerySnapshot,
		Receiver:           item.Receiver,
		GeneratorURL:       item.GeneratorURL,
		CurrentState:       item.CurrentState,
		LastNotificationAt: item.LastNotificationAt,
		StartsAt:           item.StartsAt,
		EndsAt:             item.EndsAt,
		LastSeenAt:         item.LastSeenAt,
	}
}

func toHealingRunInput(item domainalert.HealingRun) domainalert.HealingRunInput {
	return domainalert.HealingRunInput{
		ID:              item.ID,
		PolicyID:        item.PolicyID,
		EventID:         item.EventID,
		Status:          item.Status,
		ApprovalStatus:  item.ApprovalStatus,
		ApprovalComment: item.ApprovalComment,
		RequestedBy:     item.RequestedBy,
		ApprovedBy:      item.ApprovedBy,
		WorkflowRunID:   item.WorkflowRunID,
		WorkflowStatus:  item.WorkflowStatus,
		WorkflowSummary: item.WorkflowSummary,
		Result:          item.Result,
		StartedAt:       item.StartedAt,
		CompletedAt:     item.CompletedAt,
	}
}

func stringValue(value any, fallback string) string {
	if text, ok := value.(string); ok && strings.TrimSpace(text) != "" {
		return strings.TrimSpace(text)
	}
	return fallback
}

func intValue(value any, fallback int) int {
	switch current := value.(type) {
	case int:
		return current
	case int64:
		return int(current)
	case float64:
		return int(current)
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(current)); err == nil {
			return parsed
		}
	}
	return fallback
}

func stringSliceValue(value any) []string {
	switch current := value.(type) {
	case []string:
		return normalizeStrings(current)
	case []any:
		items := make([]string, 0, len(current))
		for _, item := range current {
			if text, ok := item.(string); ok {
				items = append(items, text)
			}
		}
		return normalizeStrings(items)
	default:
		return nil
	}
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(target)) {
			return true
		}
	}
	return false
}

func normalizeStrings(items []string) []string {
	if len(items) == 0 {
		return []string{}
	}
	normalized := make([]string, 0, len(items))
	for _, item := range items {
		if value := strings.TrimSpace(item); value != "" {
			normalized = append(normalized, value)
		}
	}
	return normalized
}

func stringSliceFromAny(value any) []string {
	switch current := value.(type) {
	case []string:
		return normalizeStrings(current)
	case []any:
		items := make([]string, 0, len(current))
		for _, item := range current {
			items = append(items, fmt.Sprint(item))
		}
		return normalizeStrings(items)
	default:
		return nil
	}
}
