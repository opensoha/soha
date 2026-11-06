package workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
)

type dagApprovalState struct {
	run        domainworkflow.Run
	app        domainapp.App
	definition dagWorkflowDefinition
	nodeID     string
}

func (s *Service) loadDAGApprovalState(ctx context.Context, principal domainidentity.Principal, workflowRunID string) (dagApprovalState, error) {
	run, err := s.repo.Get(ctx, strings.TrimSpace(workflowRunID))
	if err != nil {
		return dagApprovalState{}, err
	}
	if run.Scope != "" && run.Scope != "application" {
		return dagApprovalState{}, fmt.Errorf("%w: approve the delivery target and its final plan", apperrors.ErrInvalidArgument)
	}
	app, err := s.apps.Get(ctx, run.ApplicationID)
	if err != nil {
		return dagApprovalState{}, err
	}
	if err := s.authorize(ctx, principal, domainaccess.ActionApprove, app, run.ApplicationID, configString(run.Metadata, "bindingId"), appaccess.PermDeliveryApplicationEnvApprove); err != nil {
		return dagApprovalState{}, err
	}
	if run.Status != workflowStatusWaitingApproval {
		return dagApprovalState{}, fmt.Errorf("%w: workflow is not waiting for approval", apperrors.ErrInvalidArgument)
	}
	definition, ok := definitionFromRunMetadata(run)
	if !ok {
		return dagApprovalState{}, fmt.Errorf("%w: workflow definition is missing", apperrors.ErrInvalidArgument)
	}
	if len(run.NodeRuns) == 0 {
		return dagApprovalState{}, fmt.Errorf("%w: workflow has no node runs", apperrors.ErrInvalidArgument)
	}
	nodeID := pendingDAGApprovalNodeID(run.NodeRuns)
	if nodeID == "" {
		return dagApprovalState{}, fmt.Errorf("%w: approval node not found", apperrors.ErrInvalidArgument)
	}
	return dagApprovalState{run: run, app: app, definition: definition, nodeID: nodeID}, nil
}

func pendingDAGApprovalNodeID(nodeRuns []domainworkflow.NodeRun) string {
	for _, item := range nodeRuns {
		if item.Type == "manual_approval" && item.Status == workflowStatusWaitingApproval {
			return item.NodeID
		}
	}
	return ""
}

func newDAGApproval(principal domainidentity.Principal, state dagApprovalState, action, comment string) domainworkflow.Approval {
	return domainworkflow.Approval{
		ID:            uuid.NewString(),
		WorkflowRunID: state.run.ID,
		NodeID:        state.nodeID,
		Action:        action,
		Comment:       strings.TrimSpace(comment),
		ActorID:       principal.UserID,
		ActorName:     principal.UserName,
		Metadata:      workflowApprovalGatewayMetadata(state.run, state.nodeID),
		CreatedAt:     time.Now().UTC(),
	}
}

func (s *Service) applyDAGApprovalDecision(ctx context.Context, principal domainidentity.Principal, state dagApprovalState, action, comment string) (domainworkflow.Run, error) {
	var continuation dagRunTask
	if action == "approved" {
		var err error
		continuation, err = s.approvalContinuation(ctx, principal, state)
		if err != nil {
			return domainworkflow.Run{}, err
		}
	}
	approval := newDAGApproval(principal, state, action, comment)
	run := dagRunWithApprovalDecision(state, action, comment, principal.UserName)
	updated, err := s.repo.UpdateApplicationRunIfCurrent(ctx, run, workflowStatusWaitingApproval, &approval)
	if err != nil {
		return domainworkflow.Run{}, err
	}
	s.publishRunUpdate(updated)
	if action == "approved" {
		continuation.run = updated
		if err := s.enqueueDAGRun(ctx, continuation); err != nil {
			return s.failRun(context.Background(), updated, state.definition, fmt.Sprintf("workflow runner enqueue failed after approval: %v", err)), nil
		}
	}
	return updated, nil
}

func dagRunWithApprovalDecision(state dagApprovalState, action, comment, actorName string) domainworkflow.Run {
	nodeStatus, nodeSummary, runStatus := dagApprovalDecision(action, actorName)
	for index := range state.run.NodeRuns {
		if state.run.NodeRuns[index].NodeID != state.nodeID {
			continue
		}
		state.run.NodeRuns[index].Status = nodeStatus
		state.run.NodeRuns[index].Summary = nodeSummary
		state.run.NodeRuns[index].FinishedAt = time.Now().UTC().Format(time.RFC3339)
	}
	state.run.Status = runStatus
	state.run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if state.run.Metadata == nil {
		state.run.Metadata = map[string]any{}
	}
	state.run.Metadata["approvalDecision"] = action
	state.run.Metadata["approvalComment"] = strings.TrimSpace(comment)
	return syncRunNodeState(state.run, state.definition, restoreNodeRuns(state.definition, state.run.NodeRuns))
}

func dagApprovalDecision(action, actorName string) (string, string, string) {
	if action == "rejected" {
		return "failed", fmt.Sprintf("rejected by %s", actorName), "failed"
	}
	return "completed", fmt.Sprintf("approved by %s", actorName), "queued"
}

func (s *Service) approvalContinuation(ctx context.Context, principal domainidentity.Principal, state dagApprovalState) (dagRunTask, error) {
	input := domainworkflow.Input{
		ApplicationID:            state.run.ApplicationID,
		ApplicationEnvironmentID: configString(state.run.Metadata, "bindingId"),
		WorkflowName:             state.run.WorkflowName,
		ClusterID:                state.run.ClusterID,
		Namespace:                state.run.Namespace,
		DeploymentName:           state.run.DeploymentName,
	}
	binding, err := s.findApplicationEnvironmentBinding(ctx, input)
	if err != nil {
		return dagRunTask{}, err
	}
	if binding == nil {
		return dagRunTask{}, fmt.Errorf("%w: application environment binding is missing", apperrors.ErrInvalidArgument)
	}
	return dagRunTask{
		principal:   principal,
		app:         state.app,
		input:       input,
		binding:     *binding,
		definition:  state.definition,
		requestMeta: requestctx.FromContext(ctx),
	}, nil
}
