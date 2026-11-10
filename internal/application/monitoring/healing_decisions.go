package monitoring

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) ApproveHealingRun(ctx context.Context, principal domainidentity.Principal, runID, comment string) (domainalert.HealingRun, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveHealingManage, "approve")); err != nil {
		return domainalert.HealingRun{}, err
	}
	run, policy, event, err := s.loadHealingDecision(ctx, principal, runID, comment)
	if err != nil {
		return domainalert.HealingRun{}, err
	}
	if run.ApprovalStatus != "pending" || run.SubmissionStatus != "not_submitted" {
		return s.enrichHealingRun(ctx, run), nil // Duplicate observes; no executor access.
	}
	if s.workflow == nil || len(policy.Definition) == 0 {
		return domainalert.HealingRun{}, apperrors.ErrUnsupportedOperation
	}
	intent, err := healingIntent(policy, event)
	if err != nil {
		return domainalert.HealingRun{}, err
	}
	if !policy.Enabled || (run.IntentDigest != "" && run.IntentDigest != intent) {
		return domainalert.HealingRun{}, apperrors.ErrConflict
	}
	claimed := run
	claimed.Status, claimed.ApprovalStatus, claimed.SubmissionStatus = "unknown", "approved", "unknown"
	claimed.IntentDigest = intent
	claimed.ApprovalComment, claimed.ApprovedBy = strings.TrimSpace(comment), principal.UserID
	claimed.WorkflowRunID = "workflow:" + uuid.NewSHA1(uuid.NameSpaceURL, []byte(fmt.Sprintf("healing-execution-v1\x00%s\x00%d", run.ID, run.ExecutionGeneration))).String()
	claimed.Result = map[string]any{"decision": "approved", "executionState": "unknown"}
	stored, won, err := s.casHealingRun(ctx, run, claimed)
	if err != nil {
		return domainalert.HealingRun{}, err
	}
	if !won {
		return s.enrichHealingRun(ctx, stored), nil
	}
	s.recordMonitoringMutation(ctx, principal, "HealingRun", run.ID, "observability.healing_run.approve", "approved original healing execution")
	return s.dispatchHealingRun(ctx, stored, policy, event), nil
}

func (s *Service) loadHealingDecision(ctx context.Context, principal domainidentity.Principal, runID, comment string) (domainalert.HealingRun, domainalert.HealingPolicy, domainalert.AlertEvent, error) {
	var policy domainalert.HealingPolicy
	var event domainalert.AlertEvent
	if !healingIdentifier(runID) || len(comment) > 4096 {
		return domainalert.HealingRun{}, policy, event, apperrors.ErrInvalidArgument
	}
	run, err := s.healingRuns.GetHealingRun(ctx, runID)
	if err != nil {
		return run, policy, event, err
	}
	policy, err = s.healingPolicies.GetHealingPolicy(ctx, run.PolicyID)
	if err != nil {
		return run, policy, event, err
	}
	event, err = s.alertEvents.GetEvent(ctx, run.EventID)
	if err != nil {
		return run, policy, event, err
	}
	err = s.authorizeHealingApprover(ctx, principal, policy, event)
	return run, policy, event, err
}

func (s *Service) dispatchHealingRun(ctx context.Context, claimed domainalert.HealingRun, policy domainalert.HealingPolicy, event domainalert.AlertEvent) domainalert.HealingRun {
	input := healingWorkflowInput(policy, event)
	input.RequestedRunID = claimed.WorkflowRunID
	workflow, err := s.workflow.ExecuteSystemDAG(ctx, monitoringSystemPrincipal(), input.ApplicationID, input.WorkflowName, policy.WorkflowTemplateID, policy.Definition, input, map[string]any{
		"healingRunId": claimed.ID, "healingPolicyId": policy.ID, "eventId": event.ID, "healingExecutionGeneration": claimed.ExecutionGeneration,
		"healingContext": map[string]any{"event": event, "policy": policy, "comment": claimed.ApprovalComment},
	})
	if err != nil || workflow.ID != claimed.WorkflowRunID {
		return s.enrichHealingRun(ctx, claimed)
	}
	finished := claimed
	finished.SubmissionStatus = "accepted"
	finished.Status = workflow.Status
	finished.Result = map[string]any{"decision": "approved", "executionState": "accepted", "workflowRunId": workflow.ID}
	// Cancellation after dispatch must not clear the fence. Persisting the
	// receipt is bounded independently; failure remains unknown and queryable.
	persist, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	stored, _, finishErr := s.casHealingRun(persist, claimed, finished)
	if finishErr != nil {
		return s.enrichHealingRun(ctx, claimed)
	}
	return s.enrichHealingRun(ctx, stored)
}

func (s *Service) RejectHealingRun(ctx context.Context, principal domainidentity.Principal, runID, comment string) (domainalert.HealingRun, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveHealingManage, "reject")); err != nil {
		return domainalert.HealingRun{}, err
	}
	run, _, _, err := s.loadHealingDecision(ctx, principal, runID, comment)
	if err != nil {
		return domainalert.HealingRun{}, err
	}
	if run.ApprovalStatus != "pending" || run.SubmissionStatus != "not_submitted" {
		return domainalert.HealingRun{}, apperrors.ErrConflict
	}
	next := run
	next.Status, next.ApprovalStatus = "rejected", "rejected"
	next.ApprovalComment, next.ApprovedBy = strings.TrimSpace(comment), principal.UserID
	next.CompletedAt = time.Now().UTC()
	next.Result = map[string]any{"decision": "rejected"}
	stored, won, err := s.casHealingRun(ctx, run, next)
	if err != nil {
		return domainalert.HealingRun{}, err
	}
	if !won {
		return domainalert.HealingRun{}, apperrors.ErrConflict
	}
	s.recordMonitoringMutation(ctx, principal, "HealingRun", run.ID, "observability.healing_run.reject", "rejected pending healing run")
	return stored, nil
}

func (s *Service) RetryHealingRun(ctx context.Context, principal domainidentity.Principal, runID string) (domainalert.HealingRun, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveHealingManage, "retry")); err != nil {
		return domainalert.HealingRun{}, err
	}
	run, err := s.healingRuns.GetHealingRun(ctx, strings.TrimSpace(runID))
	if err != nil {
		return domainalert.HealingRun{}, err
	}
	if !s.healingRetryable(ctx, run) {
		return domainalert.HealingRun{}, apperrors.ErrConflict
	}
	input, err := s.prepareHealingRun(ctx, principal, run.EventID, run.PolicyID)
	if err != nil {
		return domainalert.HealingRun{}, err
	}
	next := run
	next.Status, next.ApprovalStatus, next.SubmissionStatus = "pending_approval", "pending", "not_submitted"
	next.ExecutionGeneration++
	next.IntentDigest = input.IntentDigest
	next.ApprovalComment, next.ApprovedBy, next.WorkflowRunID = "", "", ""
	next.StartedAt, next.CompletedAt = time.Time{}, time.Time{}
	next.Result = map[string]any{"retryOf": run.ID, "previousWorkflowRunId": run.WorkflowRunID}
	stored, won, err := s.casHealingRun(ctx, run, next)
	if err != nil {
		return domainalert.HealingRun{}, err
	}
	if !won {
		return domainalert.HealingRun{}, apperrors.ErrConflict
	}
	s.recordMonitoringMutation(ctx, principal, "HealingRun", run.ID, "observability.healing_run.retry", "opened a new pending healing generation")
	return stored, nil
}

func (s *Service) healingRetryable(ctx context.Context, run domainalert.HealingRun) bool {
	if run.ApprovalStatus == "rejected" && run.SubmissionStatus == "not_submitted" && run.WorkflowRunID == "" {
		return true
	}
	if s.workflow == nil || run.WorkflowRunID == "" {
		return false
	}
	workflow, err := s.workflow.GetSystemRun(ctx, run.WorkflowRunID)
	if err != nil || workflow.ID != run.WorkflowRunID || workflow.ApplicationID != "healing:"+run.PolicyID {
		return false
	}
	switch workflow.Status {
	case "failed", "canceled", "timed_out":
		return true
	}
	return false
}

func (s *Service) casHealingRun(ctx context.Context, before, after domainalert.HealingRun) (domainalert.HealingRun, bool, error) {
	repo, ok := s.healingRuns.(healingDecisionRepository)
	if !ok {
		return domainalert.HealingRun{}, false, apperrors.ErrUnsupportedOperation
	}
	return repo.CompareAndSwapHealingRun(ctx, before, after)
}
