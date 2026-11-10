package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/requestctx"
	"go.uber.org/zap"
)

func (s *Service) RecordExecutionTaskResult(ctx context.Context, task domaindelivery.ExecutionTask) error {
	runID := workflowMetadataString(task.Payload, "workflowRunId")
	nodeID := workflowMetadataString(task.Payload, "workflowNodeId")
	if runID == "" || nodeID == "" {
		return nil
	}
	if _, terminal := workflowStatusFromExecutionTask(task.Status); !terminal {
		return nil
	}
	run, err := s.repo.Get(ctx, runID)
	if err != nil {
		return err
	}
	if !isPausedApplicationRun(run) {
		// Managed runs reconcile under their lease. An active application runner
		// reconciles the persisted task before yielding the run.
		return nil
	}
	definition, ok := definitionFromRunMetadata(run)
	if !ok {
		return fmt.Errorf("%w: workflow definition is missing", apperrors.ErrInvalidArgument)
	}
	nodeRuns := restoreNodeRuns(definition, run.NodeRuns)
	if !applyDAGExecutionTaskResult(&run, nodeRuns, nodeID, task) {
		return nil
	}
	continuation, err := s.executionCallbackContinuation(ctx, run, definition, task)
	if err != nil {
		return err
	}
	pausedStatus := run.Status
	run.Status = "queued"
	run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	updated, err := s.repo.UpdateApplicationRunIfCurrent(ctx, syncRunNodeState(run, definition, nodeRuns), pausedStatus, nil)
	if errors.Is(err, apperrors.ErrConflict) {
		return nil
	}
	if err != nil {
		return err
	}
	s.publishRunUpdate(updated)
	continuation.run = updated
	if err := s.enqueueDAGRun(ctx, continuation); err != nil {
		s.failRun(context.Background(), updated, definition, fmt.Sprintf("workflow runner enqueue failed after execution task callback: %v", err))
		return err
	}
	return nil
}

func isPausedApplicationRun(run domainworkflow.Run) bool {
	return (run.Scope == "" || run.Scope == domainworkflow.ScopeApplication) &&
		(run.Status == workflowStatusWaitingApproval || run.Status == workflowStatusWaitingExecution)
}

func (s *Service) executionCallbackContinuation(ctx context.Context, run domainworkflow.Run, definition dagWorkflowDefinition, task domaindelivery.ExecutionTask) (dagRunTask, error) {
	app, err := s.apps.Get(ctx, run.ApplicationID)
	if err != nil {
		return dagRunTask{}, err
	}
	input := domainworkflow.Input{
		ApplicationID:            run.ApplicationID,
		ApplicationEnvironmentID: strings.TrimSpace(task.ApplicationEnvironmentID),
		WorkflowName:             run.WorkflowName,
		ClusterID:                run.ClusterID,
		Namespace:                run.Namespace,
		DeploymentName:           run.DeploymentName,
		Variables:                map[string]any{},
	}
	if bundleID := firstNonEmpty(task.ReleaseBundleID, workflowMetadataString(run.Metadata, "releaseBundleId")); bundleID != "" {
		input.Variables["releaseBundleId"] = bundleID
	}
	binding, err := s.findApplicationEnvironmentBinding(ctx, input)
	if err != nil {
		return dagRunTask{}, err
	}
	if binding == nil {
		return dagRunTask{}, fmt.Errorf("%w: application environment binding is missing", apperrors.ErrInvalidArgument)
	}
	return dagRunTask{
		principal: workflowSystemPrincipal(), app: app, input: input, binding: *binding,
		definition: definition, requestMeta: requestctx.FromContext(ctx),
	}, nil
}

func applyDAGExecutionTaskResult(run *domainworkflow.Run, nodeRuns map[string]dagNodeRun, nodeID string, task domaindelivery.ExecutionTask) bool {
	entry := nodeRuns[nodeID]
	if entry.Status != workflowStatusWaitingExecution || task.ID == "" || task.ID != dagExternalTaskID(*run, nodeID) {
		return false
	}
	status, terminal := workflowStatusFromExecutionTask(task.Status)
	if !terminal {
		return false
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if entry.StartedAt == "" {
		entry.StartedAt = now
	}
	entry.Status = status
	entry.Summary = executionTaskWorkflowSummary(task)
	entry.FinishedAt = now
	nodeRuns[nodeID] = entry

	outputs := map[string]any{
		"executionTaskId":     task.ID,
		"executionTaskStatus": strings.TrimSpace(task.Status),
		"executionTaskResult": task.Result,
	}
	recordDAGNodeOutputs(run, nodeID, map[string]any{"outputs": outputs})
	artifactState := restoredDAGArtifactState(run.Metadata)
	for key, value := range outputs {
		artifactState[key] = value
	}
	run.Metadata["artifacts"] = metadataDAGArtifactState(artifactState)
	appendDAGNodeEvent(run, nodeID, "execution_task_callback", status, entry.Summary, map[string]any{
		"executionTaskId":     task.ID,
		"executionTaskStatus": strings.TrimSpace(task.Status),
	})
	return true
}

func dagExternalTaskID(run domainworkflow.Run, nodeID string) string {
	nodeOutputs := metadataMap(metadataMap(run.Metadata["nodeOutputs"])[nodeID])
	return workflowMetadataString(metadataMap(nodeOutputs["outputs"]), "executionTaskId")
}

// Read after persisting the pause so a concurrent completion is either visible
// here or handled by a callback that sees the paused status.
func (e *dagRunExecutor) resumeIfExternalTaskFinished(ctx context.Context, state *dagRunState) bool {
	pausedStatus := state.run.Status
	task := e.reconcileExternalTaskResults(ctx, state)
	if task.ID == "" {
		return false
	}
	continuation, err := e.delivery.executionCallbackContinuation(ctx, state.run, state.definition, task)
	if err != nil {
		e.logWarnCtx(ctx, "could not load workflow continuation for external task", zap.String("run_id", state.run.ID), zap.String("task_id", task.ID))
		return false
	}
	state.run.Status = "running"
	state.run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	run := syncRunNodeState(state.run, state.definition, state.nodeRuns)
	updated, err := e.delivery.repo.UpdateApplicationRunIfCurrent(ctx, run, pausedStatus, nil)
	if err != nil {
		if !errors.Is(err, apperrors.ErrConflict) {
			e.logWarnCtx(ctx, "could not resume workflow after external task completion", zap.String("run_id", run.ID))
		}
		return false
	}
	state.run = updated
	state.principal = continuation.principal
	state.app = continuation.app
	state.input = continuation.input
	state.binding = continuation.binding
	e.delivery.publishRunUpdate(updated)
	return true
}

func (e *dagRunExecutor) reconcileExternalTaskResults(ctx context.Context, state *dagRunState) domaindelivery.ExecutionTask {
	if e.delivery.taskStore == nil {
		return domaindelivery.ExecutionTask{}
	}
	var first domaindelivery.ExecutionTask
	for _, node := range state.definition.Nodes {
		if state.nodeRuns[node.ID].Status != workflowStatusWaitingExecution {
			continue
		}
		taskID := dagExternalTaskID(state.run, node.ID)
		if taskID == "" {
			continue
		}
		task, err := e.delivery.taskStore.GetExecutionTask(ctx, taskID)
		if err != nil {
			e.logWarnCtx(ctx, "could not read workflow execution task", zap.String("run_id", state.run.ID), zap.String("task_id", taskID))
			continue
		}
		if applyDAGExecutionTaskResult(&state.run, state.nodeRuns, node.ID, task) && first.ID == "" {
			first = task
		}
	}
	if first.ID != "" {
		state.statuses = collectNodeStatuses(state.nodeRuns)
		state.artifactState = restoredDAGArtifactState(state.run.Metadata)
		state.stopFailureSources = dagStopFailureSourcesFromNodeRuns(state.definition, state.nodeRuns)
	}
	return first
}
