package monitoring

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	appaccess "github.com/soha/soha/internal/application/access"
	domainalert "github.com/soha/soha/internal/domain/alert"
	domaincopilot "github.com/soha/soha/internal/domain/copilot"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainworkflow "github.com/soha/soha/internal/domain/workflow"
	mcplogs "github.com/soha/soha/internal/infrastructure/mcp/logs"
	mcpmetrics "github.com/soha/soha/internal/infrastructure/mcp/metrics"
	mcptraces "github.com/soha/soha/internal/infrastructure/mcp/traces"
	"github.com/soha/soha/internal/platform/apperrors"
)

type DataSourceRepository interface {
	ListDataSources(context.Context) ([]domaincopilot.DataSource, error)
	GetDataSource(context.Context, string) (domaincopilot.DataSource, error)
}

func (s *Service) ListRules(ctx context.Context, principal domainidentity.Principal) ([]domainalert.AlertRule, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertRulesView); err != nil {
		return nil, err
	}
	if s.repo == nil {
		return []domainalert.AlertRule{}, nil
	}
	return s.repo.ListRules(ctx)
}

func (s *Service) GetRule(ctx context.Context, principal domainidentity.Principal, ruleID string) (domainalert.AlertRule, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertRulesView); err != nil {
		return domainalert.AlertRule{}, err
	}
	if s.repo == nil {
		return domainalert.AlertRule{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	return s.repo.GetRule(ctx, strings.TrimSpace(ruleID))
}

func (s *Service) CreateRule(ctx context.Context, principal domainidentity.Principal, input domainalert.AlertRuleInput) (domainalert.AlertRule, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertRulesManage); err != nil {
		return domainalert.AlertRule{}, err
	}
	if s.repo == nil {
		return domainalert.AlertRule{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if err := validateRuleInput(input); err != nil {
		return domainalert.AlertRule{}, err
	}
	return s.repo.CreateRule(ctx, input)
}

func (s *Service) UpdateRule(ctx context.Context, principal domainidentity.Principal, ruleID string, input domainalert.AlertRuleInput) (domainalert.AlertRule, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertRulesManage); err != nil {
		return domainalert.AlertRule{}, err
	}
	if s.repo == nil {
		return domainalert.AlertRule{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if err := validateRuleInput(input); err != nil {
		return domainalert.AlertRule{}, err
	}
	return s.repo.UpdateRule(ctx, strings.TrimSpace(ruleID), input)
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
	if s.repo == nil {
		return []domainalert.AlertEvent{}, nil
	}
	return s.repo.ListEvents(ctx, filter)
}

func (s *Service) GetEvent(ctx context.Context, principal domainidentity.Principal, eventID string) (domainalert.AlertEvent, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertsView); err != nil {
		return domainalert.AlertEvent{}, err
	}
	return s.repo.GetEvent(ctx, strings.TrimSpace(eventID))
}

func (s *Service) AcknowledgeEvent(ctx context.Context, principal domainidentity.Principal, eventID string) (domainalert.AlertEvent, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertsAcknowledge); err != nil {
		return domainalert.AlertEvent{}, err
	}
	item, err := s.repo.GetEvent(ctx, strings.TrimSpace(eventID))
	if err != nil {
		return domainalert.AlertEvent{}, err
	}
	item.CurrentState = "acknowledged"
	item.UpdatedAt = time.Now().UTC()
	return s.repo.UpdateEvent(ctx, eventID, toAlertEventInput(item))
}

func (s *Service) ResolveEvent(ctx context.Context, principal domainidentity.Principal, eventID string) (domainalert.AlertEvent, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveAlertsManage); err != nil {
		return domainalert.AlertEvent{}, err
	}
	item, err := s.repo.GetEvent(ctx, strings.TrimSpace(eventID))
	if err != nil {
		return domainalert.AlertEvent{}, err
	}
	item.Status = "resolved"
	item.CurrentState = "resolved"
	item.EndsAt = time.Now().UTC()
	item.UpdatedAt = time.Now().UTC()
	return s.repo.UpdateEvent(ctx, eventID, toAlertEventInput(item))
}

func (s *Service) HealEvent(ctx context.Context, principal domainidentity.Principal, eventID string, policyID string) (domainalert.HealingRun, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveHealingManage); err != nil {
		return domainalert.HealingRun{}, err
	}
	event, err := s.repo.GetEvent(ctx, strings.TrimSpace(eventID))
	if err != nil {
		return domainalert.HealingRun{}, err
	}
	policy, err := s.repo.GetHealingPolicy(ctx, strings.TrimSpace(policyID))
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
		if rule, ruleErr := s.repo.GetRule(ctx, event.RuleID); ruleErr == nil && strings.TrimSpace(rule.NotificationPolicyID) != "" {
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
	return s.repo.CreateHealingRun(ctx, run)
}

func (s *Service) GetHealingRun(ctx context.Context, principal domainidentity.Principal, runID string) (domainalert.HealingRun, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveHealingView); err != nil {
		return domainalert.HealingRun{}, err
	}
	item, err := s.repo.GetHealingRun(ctx, strings.TrimSpace(runID))
	if err != nil {
		return domainalert.HealingRun{}, err
	}
	return s.enrichHealingRun(ctx, item), nil
}

func (s *Service) ApproveHealingRun(ctx context.Context, principal domainidentity.Principal, runID, comment string) (domainalert.HealingRun, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveHealingManage); err != nil {
		return domainalert.HealingRun{}, err
	}
	run, err := s.repo.GetHealingRun(ctx, strings.TrimSpace(runID))
	if err != nil {
		return domainalert.HealingRun{}, err
	}
	policy, err := s.repo.GetHealingPolicy(ctx, run.PolicyID)
	if err != nil {
		return domainalert.HealingRun{}, err
	}
	if strings.TrimSpace(policy.ApprovalPolicyRef) != "" {
		candidates := stringSliceFromAny(run.Result["approvalCandidates"])
		if len(candidates) > 0 && !containsString(candidates, principal.UserID) && !containsString(candidates, principal.UserName) {
			return domainalert.HealingRun{}, fmt.Errorf("%w: current approver is not part of oncall approval candidates", apperrors.ErrAccessDenied)
		}
	}
	event, err := s.repo.GetEvent(ctx, run.EventID)
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
			return s.repo.UpdateHealingRun(ctx, runID, toHealingRunInput(run))
		}
		run.WorkflowRunID = workflowRun.ID
		run.WorkflowStatus = workflowRun.Status
		run.WorkflowSummary = summarizeWorkflowRun(workflowRun)
		run.Status = workflowRun.Status
		run.Result["workflowRunId"] = workflowRun.ID
		run.Result["workflowStatus"] = workflowRun.Status
		run.Result["workflowSummary"] = run.WorkflowSummary
	}
	updated, err := s.repo.UpdateHealingRun(ctx, runID, toHealingRunInput(run))
	if err != nil {
		return domainalert.HealingRun{}, err
	}
	return s.enrichHealingRun(ctx, updated), nil
}

func (s *Service) RejectHealingRun(ctx context.Context, principal domainidentity.Principal, runID, comment string) (domainalert.HealingRun, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveHealingManage); err != nil {
		return domainalert.HealingRun{}, err
	}
	run, err := s.repo.GetHealingRun(ctx, strings.TrimSpace(runID))
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
	return s.repo.UpdateHealingRun(ctx, runID, toHealingRunInput(run))
}

func (s *Service) RetryHealingRun(ctx context.Context, principal domainidentity.Principal, runID string) (domainalert.HealingRun, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveHealingManage); err != nil {
		return domainalert.HealingRun{}, err
	}
	run, err := s.repo.GetHealingRun(ctx, strings.TrimSpace(runID))
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
	return s.repo.UpdateHealingRun(ctx, runID, toHealingRunInput(run))
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
	return s.repo.ListNotificationPolicies(ctx)
}

func (s *Service) CreateNotificationPolicy(ctx context.Context, principal domainidentity.Principal, input domainalert.NotificationPolicyInput) (domainalert.NotificationPolicy, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsManage); err != nil {
		return domainalert.NotificationPolicy{}, err
	}
	if err := validateNotificationPolicyInput(input); err != nil {
		return domainalert.NotificationPolicy{}, err
	}
	return s.repo.CreateNotificationPolicy(ctx, input)
}

func (s *Service) UpdateNotificationPolicy(ctx context.Context, principal domainidentity.Principal, policyID string, input domainalert.NotificationPolicyInput) (domainalert.NotificationPolicy, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsManage); err != nil {
		return domainalert.NotificationPolicy{}, err
	}
	if err := validateNotificationPolicyInput(input); err != nil {
		return domainalert.NotificationPolicy{}, err
	}
	return s.repo.UpdateNotificationPolicy(ctx, policyID, input)
}

func (s *Service) ListNotificationTemplates(ctx context.Context, principal domainidentity.Principal) ([]domainalert.NotificationTemplate, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsView); err != nil {
		return nil, err
	}
	return s.repo.ListNotificationTemplates(ctx)
}

func (s *Service) CreateNotificationTemplate(ctx context.Context, principal domainidentity.Principal, input domainalert.NotificationTemplateInput) (domainalert.NotificationTemplate, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsManage); err != nil {
		return domainalert.NotificationTemplate{}, err
	}
	if err := validateNotificationTemplateInput(input); err != nil {
		return domainalert.NotificationTemplate{}, err
	}
	return s.repo.CreateNotificationTemplate(ctx, input)
}

func (s *Service) UpdateNotificationTemplate(ctx context.Context, principal domainidentity.Principal, templateID string, input domainalert.NotificationTemplateInput) (domainalert.NotificationTemplate, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveNotificationsManage); err != nil {
		return domainalert.NotificationTemplate{}, err
	}
	if err := validateNotificationTemplateInput(input); err != nil {
		return domainalert.NotificationTemplate{}, err
	}
	return s.repo.UpdateNotificationTemplate(ctx, templateID, input)
}

func (s *Service) ListHealingPolicies(ctx context.Context, principal domainidentity.Principal) ([]domainalert.HealingPolicy, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveHealingView); err != nil {
		return nil, err
	}
	return s.repo.ListHealingPolicies(ctx)
}

func (s *Service) CreateHealingPolicy(ctx context.Context, principal domainidentity.Principal, input domainalert.HealingPolicyInput) (domainalert.HealingPolicy, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveHealingManage); err != nil {
		return domainalert.HealingPolicy{}, err
	}
	if err := validateHealingPolicyInput(input); err != nil {
		return domainalert.HealingPolicy{}, err
	}
	return s.repo.CreateHealingPolicy(ctx, input)
}

func (s *Service) UpdateHealingPolicy(ctx context.Context, principal domainidentity.Principal, policyID string, input domainalert.HealingPolicyInput) (domainalert.HealingPolicy, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveHealingManage); err != nil {
		return domainalert.HealingPolicy{}, err
	}
	if err := validateHealingPolicyInput(input); err != nil {
		return domainalert.HealingPolicy{}, err
	}
	return s.repo.UpdateHealingPolicy(ctx, policyID, input)
}

func (s *Service) ListHealingRuns(ctx context.Context, principal domainidentity.Principal, filter domainalert.HealingRunFilter) ([]domainalert.HealingRun, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveHealingView); err != nil {
		return nil, err
	}
	items, err := s.repo.ListHealingRuns(ctx, filter)
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
	return s.repo.ListOnCallSchedules(ctx)
}

func (s *Service) CreateOnCallSchedule(ctx context.Context, principal domainidentity.Principal, input domainalert.OnCallScheduleInput) (domainalert.OnCallSchedule, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveOncallManage); err != nil {
		return domainalert.OnCallSchedule{}, err
	}
	return s.repo.CreateOnCallSchedule(ctx, input)
}

func (s *Service) UpdateOnCallSchedule(ctx context.Context, principal domainidentity.Principal, scheduleID string, input domainalert.OnCallScheduleInput) (domainalert.OnCallSchedule, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveOncallManage); err != nil {
		return domainalert.OnCallSchedule{}, err
	}
	return s.repo.UpdateOnCallSchedule(ctx, scheduleID, input)
}

func (s *Service) ListOnCallRotations(ctx context.Context, principal domainidentity.Principal) ([]domainalert.OnCallRotation, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveOncallView); err != nil {
		return nil, err
	}
	return s.repo.ListOnCallRotations(ctx)
}

func (s *Service) CreateOnCallRotation(ctx context.Context, principal domainidentity.Principal, input domainalert.OnCallRotationInput) (domainalert.OnCallRotation, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveOncallManage); err != nil {
		return domainalert.OnCallRotation{}, err
	}
	return s.repo.CreateOnCallRotation(ctx, input)
}

func (s *Service) UpdateOnCallRotation(ctx context.Context, principal domainidentity.Principal, rotationID string, input domainalert.OnCallRotationInput) (domainalert.OnCallRotation, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveOncallManage); err != nil {
		return domainalert.OnCallRotation{}, err
	}
	return s.repo.UpdateOnCallRotation(ctx, rotationID, input)
}

func (s *Service) ListOnCallEscalationPolicies(ctx context.Context, principal domainidentity.Principal) ([]domainalert.OnCallEscalationPolicy, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveOncallView); err != nil {
		return nil, err
	}
	return s.repo.ListOnCallEscalationPolicies(ctx)
}

func (s *Service) CreateOnCallEscalationPolicy(ctx context.Context, principal domainidentity.Principal, input domainalert.OnCallEscalationPolicyInput) (domainalert.OnCallEscalationPolicy, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveOncallManage); err != nil {
		return domainalert.OnCallEscalationPolicy{}, err
	}
	return s.repo.CreateOnCallEscalationPolicy(ctx, input)
}

func (s *Service) UpdateOnCallEscalationPolicy(ctx context.Context, principal domainidentity.Principal, policyID string, input domainalert.OnCallEscalationPolicyInput) (domainalert.OnCallEscalationPolicy, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveOncallManage); err != nil {
		return domainalert.OnCallEscalationPolicy{}, err
	}
	return s.repo.UpdateOnCallEscalationPolicy(ctx, policyID, input)
}

func (s *Service) ListOnCallAssignmentRules(ctx context.Context, principal domainidentity.Principal) ([]domainalert.OnCallAssignmentRule, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveOncallView); err != nil {
		return nil, err
	}
	return s.repo.ListOnCallAssignmentRules(ctx)
}

func (s *Service) CreateOnCallAssignmentRule(ctx context.Context, principal domainidentity.Principal, input domainalert.OnCallAssignmentRuleInput) (domainalert.OnCallAssignmentRule, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveOncallManage); err != nil {
		return domainalert.OnCallAssignmentRule{}, err
	}
	if err := validateOnCallAssignmentRuleInput(input); err != nil {
		return domainalert.OnCallAssignmentRule{}, err
	}
	return s.repo.CreateOnCallAssignmentRule(ctx, input)
}

func (s *Service) UpdateOnCallAssignmentRule(ctx context.Context, principal domainidentity.Principal, ruleID string, input domainalert.OnCallAssignmentRuleInput) (domainalert.OnCallAssignmentRule, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveOncallManage); err != nil {
		return domainalert.OnCallAssignmentRule{}, err
	}
	if err := validateOnCallAssignmentRuleInput(input); err != nil {
		return domainalert.OnCallAssignmentRule{}, err
	}
	return s.repo.UpdateOnCallAssignmentRule(ctx, ruleID, input)
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
	if s.repo == nil {
		return []domainalert.OnCallTask{}, nil
	}
	events, err := s.repo.ListEvents(ctx, domainalert.AlertEventFilter{Status: "firing", Limit: limit})
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
	if s.repo == nil {
		return domainalert.AlertSilence{}, fmt.Errorf("%w: alert repository is not configured", apperrors.ErrInvalidArgument)
	}
	if err := validateSilenceInput(input); err != nil {
		return domainalert.AlertSilence{}, err
	}
	return s.repo.CreateSilence(ctx, input)
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
		ExecutedAt: time.Now().UTC(),
	}
	if s.dataSources == nil {
		result.Summary = "no data source repository configured"
		return result, nil
	}
	dataSources, err := s.dataSources.ListDataSources(ctx)
	if err != nil {
		return result, err
	}
	selected := filterDataSources(dataSources, rule.DatasourceSelector)
	result.DataSources = make([]string, 0, len(selected))
	for _, source := range selected {
		result.DataSources = append(result.DataSources, source.ID)
	}
	switch rule.RuleType {
	case "metrics":
		return s.evaluateMetricRule(ctx, rule, selected)
	case "logs":
		return s.evaluateLogRule(ctx, rule, selected)
	case "traces":
		return s.evaluateTraceRule(ctx, rule, selected)
	default:
		result.Summary = "external passthrough rule validated only"
		return result, nil
	}
}

func (s *Service) evaluateMetricRule(ctx context.Context, rule domainalert.AlertRule, sources []domaincopilot.DataSource) (domainalert.RuleTestResult, error) {
	result := domainalert.RuleTestResult{
		RuleID:     rule.ID,
		RuleType:   rule.RuleType,
		ExecutedAt: time.Now().UTC(),
	}
	query := stringValue(rule.QuerySpec["metricKey"], "")
	if query == "" {
		query = stringValue(rule.QuerySpec["query"], "cpu_usage")
	}
	if len(sources) == 0 {
		result.Summary = "no matching metrics data source found"
		return result, nil
	}
	scope := mcpmetrics.Scope{
		ClusterID: stringValue(rule.DatasourceSelector["clusterId"], ""),
		Namespace: stringValue(rule.DatasourceSelector["namespace"], ""),
		Workload:  stringValue(rule.DatasourceSelector["workload"], ""),
	}
	if query == "" {
		query = "cpu_usage"
	}
	for _, source := range sources {
		if source.SourceKind != "metrics" {
			continue
		}
		summary, err := mcpmetrics.DefaultRegistry().Analyze(ctx, source.BackendType, source.ID, source.Config, mcpmetrics.RangeQuery{
			Scope:     scope,
			MetricKey: query,
			TimeFrom:  time.Now().UTC().Add(-time.Duration(intValue(rule.QuerySpec["windowMinutes"], 60)) * time.Minute),
			TimeTo:    time.Now().UTC(),
			Step:      time.Duration(intValue(rule.QuerySpec["stepSeconds"], 60)) * time.Second,
		})
		if err != nil {
			continue
		}
		samples := make([]map[string]any, 0, len(summary.Signals))
		matched := false
		for _, signal := range summary.Signals {
			sample := map[string]any{
				"dataSourceId": source.ID,
				"summary":      summary.Summary,
				"signals":      summary.Signals,
				"series":       summary.Series,
				"labels": map[string]any{
					"clusterId": scope.ClusterID,
					"namespace": scope.Namespace,
					"workload":  scope.Workload,
				},
			}
			for key, value := range signal {
				sample[key] = value
			}
			if trend := strings.TrimSpace(fmt.Sprint(signal["trend"])); trend != "" && trend != "stable" {
				matched = true
			}
			samples = append(samples, sample)
		}
		if len(samples) == 0 {
			samples = append(samples, map[string]any{
				"dataSourceId": source.ID,
				"summary":      summary.Summary,
				"signals":      summary.Signals,
				"series":       summary.Series,
				"labels": map[string]any{
					"clusterId": scope.ClusterID,
					"namespace": scope.Namespace,
					"workload":  scope.Workload,
				},
			})
		}
		result.Samples = append(result.Samples, samples...)
		result.Summary = summary.Summary
		result.Matched = matched
		return result, nil
	}
	result.Summary = "no metrics data source produced a match"
	return result, nil
}

func (s *Service) evaluateLogRule(ctx context.Context, rule domainalert.AlertRule, sources []domaincopilot.DataSource) (domainalert.RuleTestResult, error) {
	result := domainalert.RuleTestResult{
		RuleID:     rule.ID,
		RuleType:   rule.RuleType,
		ExecutedAt: time.Now().UTC(),
	}
	if len(sources) == 0 {
		result.Summary = "no matching logs data source found"
		return result, nil
	}
	scope := mcplogs.Scope{
		ClusterID: stringValue(rule.DatasourceSelector["clusterId"], ""),
		Namespace: stringValue(rule.DatasourceSelector["namespace"], ""),
		Workload:  stringValue(rule.DatasourceSelector["workload"], ""),
		Service:   stringValue(rule.DatasourceSelector["service"], ""),
	}
	query := stringValue(rule.QuerySpec["query"], "")
	if query == "" {
		query = stringValue(rule.QuerySpec["pattern"], "")
	}
	for _, source := range sources {
		if source.SourceKind != "logs" {
			continue
		}
		correlation, err := mcplogs.DefaultRegistry().Correlate(ctx, source.BackendType, source.ID, source.Config, mcplogs.CorrelationQuery{
			Scope:    scope,
			Query:    query,
			TimeFrom: time.Now().UTC().Add(-time.Duration(intValue(rule.QuerySpec["windowMinutes"], 60)) * time.Minute),
			TimeTo:   time.Now().UTC(),
			Limit:    intValue(rule.ThresholdSpec["minCount"], 20),
		})
		if err != nil {
			continue
		}
		if len(correlation.Records) == 0 && len(correlation.Signatures) == 0 {
			continue
		}
		result.Matched = true
		result.Summary = correlation.Summary
		result.Samples = []map[string]any{
			{"dataSourceId": source.ID, "summary": correlation.Summary, "truncated": correlation.Truncated},
		}
		return result, nil
	}
	result.Summary = "no logs data source produced a match"
	return result, nil
}

func (s *Service) evaluateTraceRule(ctx context.Context, rule domainalert.AlertRule, sources []domaincopilot.DataSource) (domainalert.RuleTestResult, error) {
	result := domainalert.RuleTestResult{
		RuleID:     rule.ID,
		RuleType:   rule.RuleType,
		ExecutedAt: time.Now().UTC(),
	}
	if len(sources) == 0 {
		result.Summary = "no matching traces data source found"
		return result, nil
	}
	scope := mcptraces.Scope{
		ClusterID: stringValue(rule.DatasourceSelector["clusterId"], ""),
		Namespace: stringValue(rule.DatasourceSelector["namespace"], ""),
		Workload:  stringValue(rule.DatasourceSelector["workload"], ""),
		Service:   stringValue(rule.DatasourceSelector["service"], ""),
	}
	for _, source := range sources {
		if source.SourceKind != "traces" {
			continue
		}
		traceResult, err := mcptraces.DefaultRegistry().FindSlowSpans(ctx, source.BackendType, source.ID, source.Config, mcptraces.Query{
			Scope:       scope,
			TimeFrom:    time.Now().UTC().Add(-time.Duration(intValue(rule.QuerySpec["windowMinutes"], 60)) * time.Minute),
			TimeTo:      time.Now().UTC(),
			MinDuration: time.Duration(intValue(rule.ThresholdSpec["minDurationMs"], 250)) * time.Millisecond,
			Limit:       intValue(rule.ThresholdSpec["sampleLimit"], 20),
		})
		if err != nil {
			continue
		}
		if len(traceResult.Spans) == 0 {
			continue
		}
		result.Matched = true
		result.Summary = traceResult.Summary
		result.Samples = []map[string]any{
			{"dataSourceId": source.ID, "summary": traceResult.Summary, "spanCount": len(traceResult.Spans)},
		}
		return result, nil
	}
	result.Summary = "no traces data source produced a match"
	return result, nil
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
		CurrentState: "firing",
		LastSeenAt:   time.Now().UTC(),
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
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
