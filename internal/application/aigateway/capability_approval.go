package aigateway

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"slices"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type capabilityPrincipalKey struct{}
type capabilityCancellationKey struct{}
type capabilityObservationKey struct{}
type capabilityExecutionKey struct{}

// The goal lease is checked by the Workflow repository. Do not propagate a
// delivery-stage authority into domains that own their own child Run/plan locks.
func withCapabilityExecution(ctx context.Context, run domainworkflow.Run, node domainworkflow.NodeRun) context.Context {
	execution := domainworkflow.NodeExecution{Scope: run.Scope, RunID: run.ID, NodeID: node.NodeID, Version: run.Version, FencingToken: run.FencingToken, LeaseOwner: run.LeaseOwner, Attempt: 1}
	bound := capabilityTaskScope{}
	if run.Scope == domainworkflow.ScopeCapabilityTask {
		intent, err := domainworkflow.CapabilityIntentFrom(run)
		bound.invalid = err != nil
		bound.constraints = slices.Clone(intent.Input.ScopeConstraints)
	}
	ctx = context.WithValue(ctx, capabilityTaskScopeKey{}, bound)
	return context.WithValue(ctx, capabilityExecutionKey{}, execution)
}

func capabilityExecutionFrom(ctx context.Context) (domainworkflow.NodeExecution, bool) {
	execution, ok := ctx.Value(capabilityExecutionKey{}).(domainworkflow.NodeExecution)
	return execution, ok
}

func capabilityApprovalID(requestID string) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte("capability-approval\x00"+requestID)).String()
}

func (s *Service) withCapabilityApproval(ctx context.Context, request domainaigateway.ApprovalRequest, execute func(context.Context) (domainaigateway.ApprovalDecisionResult, error)) (domainaigateway.ApprovalDecisionResult, error) {
	runID, _ := request.RelatedIDs["capabilityTaskId"].(string)
	if runID == "" {
		return execute(ctx)
	}
	if s.capabilityGuard == nil {
		return domainaigateway.ApprovalDecisionResult{}, apperrors.ErrAccessDenied
	}
	var result domainaigateway.ApprovalDecisionResult
	cancellation, _ := request.RelatedIDs["capabilityCancellation"].(bool)
	err := s.capabilityGuard.WithCapabilityApproval(ctx, runID, cancellation, func(run domainworkflow.Run) error {
		intent, err := domainworkflow.CapabilityIntentFrom(run)
		if err != nil {
			return fmt.Errorf("%w: capability task expired", apperrors.ErrConflict)
		}
		node, err := matchingCapabilityApprovalNode(run, intent, request, cancellation)
		if err != nil {
			return err
		}
		return s.withNativeCapabilityParent(ctx, run, cancellation, func(boundedCtx context.Context) error {
			if !cancellation {
				var cancel context.CancelFunc
				boundedCtx, cancel = context.WithDeadline(boundedCtx, domainworkflow.CapabilityNodeDeadline(intent, node))
				defer cancel()
				if err := boundedCtx.Err(); err != nil {
					return err
				}
			}
			reader, ok := s.identity.(interface {
				CurrentExecutionPrincipal(context.Context, string, string) (domainidentity.Principal, error)
			})
			if !ok {
				return apperrors.ErrAccessDenied
			}
			actor, err := reader.CurrentExecutionPrincipal(boundedCtx, intent.ActorID, intent.ActorTokenID)
			if err == nil && intent.PermissionCeiling != nil {
				actor, err = appaccess.RuntimePrincipalWithin(boundedCtx, s.permissions, actor, intent.PermissionCeiling)
			}
			if err != nil {
				return err
			}
			boundedCtx = withCapabilityExecution(boundedCtx, run, node)
			boundedCtx = context.WithValue(boundedCtx, capabilityPrincipalKey{}, actor)
			result, err = execute(boundedCtx)
			return err
		})
	})
	return result, err
}

func matchingCapabilityApprovalNode(run domainworkflow.Run, intent domainworkflow.CapabilityIntent, request domainaigateway.ApprovalRequest, cancellation bool) (domainworkflow.NodeRun, error) {
	for _, node := range run.NodeRuns {
		if node.NodeID != request.RelatedIDs["capabilityNodeId"] || node.PreparedCall == nil || !node.DispatchAttempted {
			continue
		}
		call := node.PreparedCall
		if cancellation {
			if node.ControlCall == nil || node.ControlCall.RequestID != request.RequestID {
				return domainworkflow.NodeRun{}, apperrors.ErrConflict
			}
			call = node.ControlCall
			if run.StopReason == "" || node.Invocation == nil || node.Invocation.Task == nil || node.Invocation.Task.CancelCall == nil || call.ToolName != node.Invocation.Task.CancelCall.ToolName {
				return domainworkflow.NodeRun{}, apperrors.ErrConflict
			}
		} else if run.StopReason != "" || run.Status == "blocked" || !domainworkflow.CapabilityNodeDeadline(intent, node).After(time.Now()) || (node.Status != "waiting_approval" && node.Status != "running") {
			return domainworkflow.NodeRun{}, apperrors.ErrConflict
		}
		if !capabilityApprovalMatchesCall(request, *call) {
			return domainworkflow.NodeRun{}, fmt.Errorf("%w: approval does not match the frozen step", apperrors.ErrConflict)
		}
		return node, nil
	}
	return domainworkflow.NodeRun{}, apperrors.ErrConflict
}

func capabilityApprovalMatchesCall(request domainaigateway.ApprovalRequest, call domainaigateway.ToolInvocationRequest) bool {
	return call.RequestID == request.RequestID && capabilityApprovalID(call.RequestID) == request.ID && call.ToolName == request.ToolName && reflect.DeepEqual(call.Input, request.ToolInput) && maps.Equal(call.SecretRefs, request.SecretRefs)
}
