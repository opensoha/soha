package monitoring

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/idempotency"
)

type healingRequestRepository interface {
	ClaimHealingRequest(context.Context, domainalert.HealingRequest, domainalert.HealingRunInput) (domainalert.HealingRun, error)
	GetHealingRequest(context.Context, string, string) (domainalert.HealingRequest, error)
}

type healingDecisionRepository interface {
	CompareAndSwapHealingRun(context.Context, domainalert.HealingRun, domainalert.HealingRun) (domainalert.HealingRun, bool, error)
}

func healingCredential(principal domainidentity.Principal, access domainidentity.AccessContext) (string, error) {
	credential := ""
	if access.SessionID != "" {
		credential = "session:" + access.SessionID
	} else if principal.AccessTokenID != "" {
		credential = "token:" + principal.AccessTokenID
	}
	if principal.UserID == "" || credential == "" {
		return "", apperrors.ErrAccessDenied
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(credential))), nil
}

func healingUUID(key string) bool {
	id, err := uuid.Parse(key)
	return err == nil && id != uuid.Nil && id.String() == key
}

func (s *Service) CreateOriginalHealingRequest(ctx context.Context, principal domainidentity.Principal, access domainidentity.AccessContext, eventID, policyID, key string) (domainalert.HealingOriginalReceipt, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermObserveHealingManage, "heal")); err != nil {
		return domainalert.HealingOriginalReceipt{}, err
	}
	credential, err := healingCredential(principal, access)
	if err != nil || !healingUUID(key) || !healingIdentifier(eventID) || !healingIdentifier(policyID) {
		if err == nil {
			err = apperrors.ErrInvalidArgument
		}
		return domainalert.HealingOriginalReceipt{}, err
	}
	repo, ok := s.healingRuns.(healingRequestRepository)
	if !ok {
		return domainalert.HealingOriginalReceipt{}, apperrors.ErrUnsupportedOperation
	}
	// The request input binds IDs, while the run intent captures effective
	// policy/target. A repeated UUID can observe its original after edits.
	id, digest, err := idempotency.Derive("healing-request-v1", principal.UserID, key, []string{eventID, policyID})
	if err != nil {
		return domainalert.HealingOriginalReceipt{}, apperrors.ErrInvalidArgument
	}
	if existing, getErr := repo.GetHealingRequest(ctx, principal.UserID, key); getErr == nil {
		if existing.CredentialHash != credential {
			return domainalert.HealingOriginalReceipt{}, apperrors.ErrAccessDenied
		}
		if existing.InputDigest != digest || existing.EventID != eventID || existing.PolicyID != policyID {
			return domainalert.HealingOriginalReceipt{}, apperrors.ErrConflict
		}
		return s.originalHealingReceipt(ctx, key, existing.RunID)
	} else if !errors.Is(getErr, apperrors.ErrNotFound) {
		return domainalert.HealingOriginalReceipt{}, getErr
	}
	input, err := s.prepareHealingRun(ctx, principal, eventID, policyID)
	if err != nil {
		return domainalert.HealingOriginalReceipt{}, err
	}
	input.ID = "healing-run:" + id
	request := domainalert.HealingRequest{ID: id, ActorID: principal.UserID, CredentialHash: credential, Key: key, EventID: eventID, PolicyID: policyID, InputDigest: digest, RunID: input.ID, CreatedAt: time.Now().UTC()}
	run, err := repo.ClaimHealingRequest(ctx, request, input)
	if err != nil {
		return domainalert.HealingOriginalReceipt{}, err
	}
	s.recordMonitoringMutation(ctx, principal, "HealingRun", run.ID, "observability.healing_run.request", "observed original healing request")
	return projectOriginalHealingRun(key, s.enrichHealingRun(ctx, run)), nil
}

func (s *Service) GetOriginalHealingRequest(ctx context.Context, principal domainidentity.Principal, access domainidentity.AccessContext, eventID, key string) (domainalert.HealingOriginalReceipt, error) {
	for _, permission := range []string{appaccess.PermObserveHealingView, appaccess.ManagedActionPermission(appaccess.PermObserveHealingManage, "heal")} {
		if err := s.authorize(ctx, principal, permission); err != nil {
			return domainalert.HealingOriginalReceipt{}, err
		}
	}
	credential, err := healingCredential(principal, access)
	if err != nil {
		return domainalert.HealingOriginalReceipt{}, err
	}
	if !healingUUID(key) || !healingIdentifier(eventID) {
		return domainalert.HealingOriginalReceipt{}, apperrors.ErrInvalidArgument
	}
	repo, ok := s.healingRuns.(healingRequestRepository)
	if !ok {
		return domainalert.HealingOriginalReceipt{}, apperrors.ErrUnsupportedOperation
	}
	request, err := repo.GetHealingRequest(ctx, principal.UserID, key)
	if err != nil {
		return domainalert.HealingOriginalReceipt{}, err
	}
	if request.EventID != eventID {
		return domainalert.HealingOriginalReceipt{}, apperrors.ErrNotFound
	}
	if request.CredentialHash != credential {
		return domainalert.HealingOriginalReceipt{}, apperrors.ErrAccessDenied
	}
	return s.originalHealingReceipt(ctx, key, request.RunID)
}

func (s *Service) originalHealingReceipt(ctx context.Context, key, runID string) (domainalert.HealingOriginalReceipt, error) {
	run, err := s.healingRuns.GetHealingRun(ctx, runID)
	if err != nil {
		return domainalert.HealingOriginalReceipt{}, err
	}
	return projectOriginalHealingRun(key, s.enrichHealingRun(ctx, run)), nil
}

func projectOriginalHealingRun(key string, run domainalert.HealingRun) domainalert.HealingOriginalReceipt {
	return domainalert.HealingOriginalReceipt{Protocol: "healing-request-v1", IdempotencyKey: key, Run: domainalert.HealingOriginalRun{
		ID: run.ID, EventID: run.EventID, PolicyID: run.PolicyID, Status: run.Status, ApprovalStatus: run.ApprovalStatus,
		ExecutionGeneration: run.ExecutionGeneration, SubmissionStatus: run.SubmissionStatus, WorkflowRunID: run.WorkflowRunID,
		IntentDigest: run.IntentDigest, CreatedAt: run.CreatedAt, UpdatedAt: run.UpdatedAt,
	}}
}

func healingIdentifier(id string) bool {
	if strings.TrimSpace(id) != id || len(id) < 1 || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if c < 33 || c > 126 || strings.ContainsRune("/\\?#", c) {
			return false
		}
	}
	return true
}

// Only effective execution input and approval configuration form the intent;
// timestamps, alert delivery updates and historical candidates cannot change it.
func healingIntent(policy domainalert.HealingPolicy, event domainalert.AlertEvent) (string, error) {
	input := healingWorkflowInput(policy, event)
	payload, err := json.Marshal(struct {
		PolicyID, TemplateID, ApprovalPolicyRef string
		Definition                              map[string]any
		Input                                   domainworkflow.Input
	}{policy.ID, policy.WorkflowTemplateID, policy.ApprovalPolicyRef, policy.Definition, input})
	if err != nil {
		return "", apperrors.ErrInvalidArgument
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(payload)), nil
}

func healingWorkflowInput(policy domainalert.HealingPolicy, event domainalert.AlertEvent) domainworkflow.Input {
	return domainworkflow.Input{
		ApplicationID: "healing:" + policy.ID, WorkflowName: firstNonEmpty(policy.Name, policy.WorkflowTemplateID),
		ClusterID:      firstNonEmpty(event.ClusterID, stringValue(policy.Definition["clusterId"], "")),
		Namespace:      firstNonEmpty(event.Namespace, stringValue(policy.Definition["namespace"], "")),
		DeploymentName: firstNonEmpty(event.Labels["workload"], event.Labels["deployment"], event.Labels["app"], stringValue(policy.Definition["workloadName"], "")),
	}
}

func (s *Service) prepareHealingRun(ctx context.Context, principal domainidentity.Principal, eventID, policyID string) (domainalert.HealingRunInput, error) {
	if !healingIdentifier(eventID) || !healingIdentifier(policyID) {
		return domainalert.HealingRunInput{}, apperrors.ErrInvalidArgument
	}
	event, err := s.alertEvents.GetEvent(ctx, eventID)
	if err != nil {
		return domainalert.HealingRunInput{}, err
	}
	policy, err := s.healingPolicies.GetHealingPolicy(ctx, policyID)
	if err != nil {
		return domainalert.HealingRunInput{}, err
	}
	if !policy.Enabled {
		return domainalert.HealingRunInput{}, apperrors.ErrConflict
	}
	digest, err := healingIntent(policy, event)
	if err != nil {
		return domainalert.HealingRunInput{}, err
	}
	result := map[string]any{"eventId": event.ID, "ruleId": event.RuleID, "status": event.Status, "policy": policy.Name}
	s.addHealingDisplayContext(ctx, event, result)
	return domainalert.HealingRunInput{
		PolicyID: policy.ID, EventID: event.ID, Status: "pending_approval", ApprovalStatus: "pending", RequestedBy: principal.UserID,
		ExecutionGeneration: 1, SubmissionStatus: "not_submitted", IntentDigest: digest,
		Result: result,
	}, nil
}

// Keep the legacy UI's historical routing display. Authorization deliberately
// resolves current configuration again instead of trusting this snapshot.
func (s *Service) addHealingDisplayContext(ctx context.Context, event domainalert.AlertEvent, result map[string]any) {
	if strings.TrimSpace(event.RuleID) == "" {
		return
	}
	rule, err := s.rules.GetRule(ctx, event.RuleID)
	if err != nil || strings.TrimSpace(rule.NotificationPolicyID) == "" {
		return
	}
	policy, err := s.findNotificationPolicy(ctx, rule.NotificationPolicyID)
	if err != nil {
		return
	}
	current := s.resolveEventOnCall(ctx, policy, event)
	if len(current) > 0 {
		result["currentOnCall"] = current
	}
	if participant := stringValue(current["currentParticipant"], ""); participant != "" {
		result["approvalCandidates"] = []string{participant}
	}
}
