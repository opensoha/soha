package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"maps"
	"reflect"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type CapabilityApprovalGuard interface {
	WithCapabilityApproval(context.Context, string, bool, func(domainworkflow.Run) error) error
}

func (s *Service) SetCapabilityApprovalGuard(guard CapabilityApprovalGuard) {
	s.capabilityGuard = guard
}

func (s *Service) PrepareCapabilityNode(ctx context.Context, principal domainidentity.Principal, run domainworkflow.Run, node domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
	intent, err := domainworkflow.CapabilityIntentFrom(run)
	if err != nil {
		return node, err
	}
	completed := map[string]domainaigateway.ToolInvocationResult{}
	for _, entry := range run.NodeRuns {
		if entry.Status == "completed" && entry.Invocation != nil {
			completed[entry.NodeID] = *entry.Invocation
		}
	}
	for _, step := range intent.Input.Plan.Steps {
		if step.ID != node.NodeID {
			continue
		}
		call, err := s.ResolveCapabilityStep(step, intent.Input.Plan, completed)
		if err != nil {
			return node, err
		}
		if summary := gatewayRedactionAuditSummaryForValue(call.Input, gatewayRedactionRule{SecretTypes: []string{"default"}}, "input"); !summary.empty() {
			return node, fmt.Errorf("%w: bound input contains sensitive literal data", apperrors.ErrInvalidArgument)
		}
		tool, _ := s.toolByName(call.ToolName)
		call.AIClientID, call.SkillID, call.SessionID = intent.Input.AIClientID, intent.Input.SkillID, intent.ActorSessionID
		execution, _ := capabilityExecutionFrom(withCapabilityExecution(ctx, run, node))
		call.RequestID = execution.ResourceID("capability-invocation")
		ctx, err = s.authorizeCapabilityCall(ctx, principal, call)
		if err != nil {
			return node, err
		}
		if err := s.pinToolSecretRefs(ctx, principal, tool, &call); err != nil {
			return node, err
		}
		node.PreparedCall = &call
		return node, nil
	}
	return node, apperrors.ErrInvalidArgument
}

// Preparing and displaying a call may not consume a mutation. Execution still
// passes through InvokeTool, including current policy, input and SecretRef checks.
func (s *Service) authorizeCapabilityCall(ctx context.Context, principal domainidentity.Principal, call domainaigateway.ToolInvocationRequest) (context.Context, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayInvoke); err != nil {
		return ctx, err
	}
	tool, ok := s.toolByName(call.ToolName)
	if !ok || validateCapabilityVersion(tool, call.CapabilityVersion) != nil {
		return ctx, apperrors.ErrConflict
	}
	if err := s.authorizeTool(ctx, principal, tool); err != nil {
		return ctx, err
	}
	ctx, err := s.resolveCapabilityScopeContext(ctx, principal, tool, call.Input)
	if err != nil {
		return ctx, err
	}
	if _, err := s.authorizeToolGrant(ctx, principal, call.AIClientID, tool, capabilityGatewayScope(tool, call.Input)); err != nil {
		return ctx, err
	}
	return ctx, s.authorizeSkillBinding(ctx, principal, call.AIClientID, call.SkillID, tool)
}

func (s *Service) AdvanceCapabilityNode(ctx context.Context, principal domainidentity.Principal, run domainworkflow.Run, node domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
	if node.PreparedCall == nil {
		return node, apperrors.ErrInvalidArgument
	}
	ctx = withCapabilityExecution(ctx, run, node)
	call := *node.PreparedCall
	ctx, err := s.authorizeCapabilityCall(ctx, principal, call)
	if err != nil {
		node.Status, node.Summary = "blocked", "capability or authorization changed; child state is unconfirmed"
		return node, nil
	}
	if run.StopReason != "" {
		return s.cancelCapabilityNode(ctx, principal, run, node)
	}
	if node.Invocation != nil && node.Invocation.Task != nil {
		if !s.validCapabilityTaskResult(*node.PreparedCall, *node.Invocation) {
			node.Status, node.Summary = "blocked", "owning domain returned an invalid task reference"
			return node, nil
		}
		call = capabilityInvocationCall(node.Invocation.Task.StatusCall, call)
		ctx = context.WithValue(ctx, capabilityObservationKey{}, true)
	} else {
		// A deterministic approval is recovered even if the worker died before
		// persisting its reference. Its creation is confined to server context.
		approval, found, err := s.capabilityNodeApproval(ctx, call)
		if err != nil {
			return node, err
		}
		if found {
			return s.observeCapabilityApproval(ctx, principal, node, approval)
		}
	}
	result, err := s.InvokeTool(ctx, principal, call)
	if err != nil {
		if errors.Is(err, apperrors.ErrAccessDenied) || errors.Is(err, apperrors.ErrConflict) || errors.Is(err, apperrors.ErrInvalidArgument) {
			node.Status, node.Summary = "blocked", "capability execution requires a revised plan or authorization"
			return node, nil
		}
		return node, err
	}
	return s.capabilityNodeResult(node, result), nil
}

func capabilityInvocationCall(call domainaigateway.CapabilityCall, original domainaigateway.ToolInvocationRequest) domainaigateway.ToolInvocationRequest {
	original.ToolName, original.CapabilityVersion, original.Input, original.SecretRefs = call.ToolName, call.CapabilityVersion, call.Input, call.SecretRefs
	return original
}

func (s *Service) capabilityNodeResult(node domainworkflow.NodeRun, result domainaigateway.ToolInvocationResult) domainworkflow.NodeRun {
	if node.Invocation != nil && node.Invocation.Task != nil && result.Result != "success" {
		node.Status, node.Summary = "blocked", "domain task observation is held; child reference retained"
		return node
	}
	node.Invocation = &result
	if node.PreparedCall == nil || !s.validCapabilityTaskResult(*node.PreparedCall, result) {
		node.Status, node.Summary = "blocked", "owning domain task reference is missing or changed"
		return node
	}
	switch {
	case result.RequiresApproval && result.Result != "success":
		node.Status, node.Summary = "waiting_approval", "waiting for Gateway approval"
	case result.Result != "success":
		node.Status, node.Summary = "blocked", "Gateway policy held this step"
	case result.Task != nil && !result.Task.Terminal:
		node.Status, node.Summary = "waiting_execution", "waiting for the owning domain task"
	case result.Task != nil && result.Task.Outcome != "succeeded":
		node.Status, node.Summary = "failed", "owning domain task did not succeed"
		if result.Task.Outcome == "unknown" || result.Task.Outcome == "" {
			node.Status, node.Summary = "blocked", "owning domain task outcome is unknown"
		}
	default:
		node.Status, node.Summary = "completed", "capability step completed"
	}
	if node.Status == "completed" || node.Status == "failed" {
		node.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	}
	return node
}

func (s *Service) capabilityNodeApproval(ctx context.Context, call domainaigateway.ToolInvocationRequest) (domainaigateway.ApprovalRequest, bool, error) {
	repo := s.approvalRepository()
	if repo == nil {
		return domainaigateway.ApprovalRequest{}, false, nil
	}
	request, err := repo.GetApprovalRequest(ctx, capabilityApprovalID(call.RequestID))
	if errors.Is(err, apperrors.ErrNotFound) {
		return request, false, nil
	}
	if err != nil {
		return request, false, err
	}
	if request.RequestID != call.RequestID || request.ToolName != call.ToolName || !reflect.DeepEqual(request.ToolInput, call.Input) || !maps.Equal(request.SecretRefs, call.SecretRefs) {
		return request, false, fmt.Errorf("%w: frozen capability approval changed", apperrors.ErrConflict)
	}
	return request, true, nil
}

func (s *Service) observeCapabilityApproval(ctx context.Context, principal domainidentity.Principal, node domainworkflow.NodeRun, request domainaigateway.ApprovalRequest) (domainworkflow.NodeRun, error) {
	switch request.Status {
	case "pending":
		if request.ExpiresAt != nil && !request.ExpiresAt.After(time.Now()) {
			node.Status, node.Summary = "failed", "approval expired"
		} else {
			node.Status, node.Summary = "waiting_approval", "waiting for Gateway approval"
		}
		return node, nil
	case "approved":
		// The caller holds the Run row lock. The original vote is durable;
		// recheck identity/policy and replay only the frozen idempotent call.
		ctx = context.WithValue(ctx, capabilityPrincipalKey{}, principal)
		result, err := s.executeApprovedCapability(ctx, principal, request)
		if err != nil {
			return node, err
		}
		if result.Invocation != nil {
			return s.capabilityNodeResult(node, *result.Invocation), nil
		}
		return node, apperrors.ErrConflict
	case "executed":
		tool, ok := s.toolByName(request.ToolName)
		if !ok {
			return node, apperrors.ErrConflict
		}
		output, _, err := s.sanitizeToolOutputByAccessPolicy(ctx, principal, request.AIClientID, request.SkillID, tool, capabilityGatewayScope(tool, request.ToolInput), request.Output)
		if err != nil {
			return node, err
		}
		return s.capabilityNodeResult(node, domainaigateway.ToolInvocationResult{ToolName: tool.Name, CapabilityVersion: tool.Version, Result: "success", Output: output, RelatedIDs: request.RelatedIDs, Task: s.gatewayRegistry().TaskReference(tool, request.Output, output), Assessment: visibleCapabilityAssessment(tool, output)}), nil
	default:
		node.Status, node.Summary = "failed", "approval was rejected, canceled, expired or failed"
		return node, nil
	}
}

func (s *Service) cancelCapabilityNode(ctx context.Context, principal domainidentity.Principal, run domainworkflow.Run, node domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
	var err error
	node, err = s.recoverCapabilityNodeOnStop(ctx, principal, node)
	if err != nil || node.Status == "canceled" || node.Invocation == nil || node.Invocation.Task == nil {
		return node, err
	}
	if !s.validCapabilityTaskResult(*node.PreparedCall, *node.Invocation) {
		node.Status, node.Summary = "blocked", "owning domain returned an invalid task reference"
		return node, nil
	}
	task := node.Invocation.Task
	call := task.StatusCall
	if !task.Terminal && task.CancelCall != nil {
		return s.cancelCapabilityDomainTask(ctx, principal, node, *task.CancelCall)
	}
	result, err := s.InvokeTool(context.WithValue(ctx, capabilityObservationKey{}, true), principal, capabilityInvocationCall(call, *node.PreparedCall))
	if err != nil {
		return capabilityCancellationError(node, err)
	}
	return s.canceledCapabilityNodeResult(node, result), nil
}

func (s *Service) recoverCapabilityNodeOnStop(ctx context.Context, principal domainidentity.Principal, node domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
	if s.knownSynchronousCapabilityResult(node) {
		node.Status, node.Summary = "canceled", "task stopped; completed synchronous effect retained"
		return node, nil
	}
	if node.Invocation == nil || node.Invocation.Task == nil {
		approval, found, err := s.capabilityNodeApproval(ctx, *node.PreparedCall)
		if err != nil {
			return node, err
		}
		if found && approval.Status == "pending" {
			node.Status, node.Summary = "canceled", "stopped before approval; this approval can no longer execute"
			return node, nil
		}
		if found && approval.Status == "executed" {
			node, err = s.observeCapabilityApproval(ctx, principal, node, approval)
			if err != nil {
				return node, err
			}
		}
		if node.Invocation == nil || node.Invocation.Task == nil {
			recovered, err := s.recoverCapabilityCall(ctx, principal, *node.PreparedCall)
			if err != nil {
				return node, err
			}
			if recovered != nil {
				node.Invocation = recovered
			}
			if s.knownSynchronousCapabilityResult(node) {
				node.Status, node.Summary = "canceled", "task stopped; recovered synchronous effect retained"
				return node, nil
			}
		}
		if node.Invocation == nil || node.Invocation.Task == nil {
			// Never re-dispatch a mutation to discover its result after stop.
			node.Status, node.Summary = "blocked", "dispatch outcome is unknown; domain reconciliation is required"
			return node, nil
		}
	}
	return node, nil
}

func (s *Service) cancelCapabilityDomainTask(ctx context.Context, principal domainidentity.Principal, node domainworkflow.NodeRun, cancelCall domainaigateway.CapabilityCall) (domainworkflow.NodeRun, error) {
	ctx = context.WithValue(ctx, capabilityCancellationKey{}, true)
	call := capabilityInvocationCall(cancelCall, *node.PreparedCall)
	call.RequestID = uuid.NewSHA1(uuid.NameSpaceOID, []byte(node.PreparedCall.RequestID+"\x00cancel\x00"+cancelCall.ToolName)).String()
	node.ControlCall = &call
	request, found, err := s.capabilityNodeApproval(ctx, call)
	if err != nil {
		return node, err
	}
	if found {
		switch request.Status {
		case "pending":
			node.Status, node.Summary = "waiting_approval", "domain cancellation requires approval"
			if request.ExpiresAt != nil && !request.ExpiresAt.After(time.Now()) {
				node.Status, node.Summary = "blocked", "domain cancellation approval expired; child state is unconfirmed"
			}
			return node, nil
		case "approved":
			ctx = context.WithValue(ctx, capabilityPrincipalKey{}, principal)
			decision, err := s.executeApprovedCapability(ctx, principal, request)
			if err != nil {
				return node, err
			}
			if decision.Invocation == nil {
				return node, apperrors.ErrConflict
			}
			return s.canceledCapabilityNodeResult(node, *decision.Invocation), nil
		case "executed":
			// Re-read the domain state instead of treating approval completion as
			// acknowledgment from the runner.
			call = capabilityInvocationCall(node.Invocation.Task.StatusCall, *node.PreparedCall)
			ctx = context.WithValue(ctx, capabilityObservationKey{}, true)
		default:
			node.Status, node.Summary = "blocked", "domain cancellation approval did not execute; child state is unconfirmed"
			return node, nil
		}
	}
	result, err := s.InvokeTool(ctx, principal, call)
	if err != nil {
		return capabilityCancellationError(node, err)
	}
	if result.RequiresApproval {
		// Keep the child task, so waiting for cancel approval cannot discard
		// the only reference to an operation which is still running.
		node.Status, node.Summary = "waiting_approval", "domain cancellation requires approval"
		return node, nil
	}
	return s.canceledCapabilityNodeResult(node, result), nil
}

func capabilityCancellationError(node domainworkflow.NodeRun, err error) (domainworkflow.NodeRun, error) {
	if errors.Is(err, apperrors.ErrAccessDenied) || errors.Is(err, apperrors.ErrConflict) || errors.Is(err, apperrors.ErrInvalidArgument) {
		node.Status, node.Summary = "blocked", "domain cancellation or observation is unavailable; child state is unconfirmed"
		return node, nil
	}
	return node, err
}

func (s *Service) canceledCapabilityNodeResult(node domainworkflow.NodeRun, result domainaigateway.ToolInvocationResult) domainworkflow.NodeRun {
	node = s.capabilityNodeResult(node, result)
	if result.Task != nil && result.Task.Terminal && (result.Task.Outcome == "succeeded" || result.Task.Outcome == "failed" || result.Task.Outcome == "canceled") && s.validCapabilityTaskResult(*node.PreparedCall, result) {
		node.Status, node.Summary = "canceled", "task stopped; inspect child outcome for effects already applied"
	}
	return node
}

func visibleCapabilityAssessment(tool domainaigateway.ToolCapability, output any) *domainaigateway.CapabilityAssessment {
	if !tool.ProducesAssessment {
		return nil
	}
	var assessment domainaigateway.CapabilityAssessment
	raw, err := json.Marshal(output)
	if err != nil || json.Unmarshal(raw, &assessment) != nil || assessment.Summary == "" {
		return nil
	}
	if assessment.Verdict != "satisfied" && assessment.Verdict != "unsatisfied" && assessment.Verdict != "inconclusive" {
		return nil
	}
	for _, evidence := range assessment.Evidence {
		if evidence.Kind == "" || evidence.Source == "" || evidence.ObservedAt.IsZero() || evidence.Summary == "" {
			return nil
		}
		if evidence.Incomplete && assessment.Verdict == "satisfied" {
			assessment.Verdict = "inconclusive"
		}
	}
	if len(assessment.Evidence) == 0 && assessment.Verdict == "satisfied" {
		assessment.Verdict = "inconclusive"
	}
	return &assessment
}

func (s *Service) knownSynchronousCapabilityResult(node domainworkflow.NodeRun) bool {
	if node.PreparedCall == nil || node.Invocation == nil || node.Invocation.Result != "success" {
		return false
	}
	tool, found := s.toolByName(node.PreparedCall.ToolName)
	return found && tool.Execution != nil && tool.Execution.Mode == "sync" && node.Invocation.Task == nil && s.validCapabilityTaskResult(*node.PreparedCall, *node.Invocation)
}
