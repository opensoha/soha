package workflow

import (
	"context"
	"fmt"
	"time"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/runtimeobs"
	"go.uber.org/zap"
)

type dagRunExecutor struct {
	nodes                  *dagExecutor
	metrics                *runtimeobs.Registry
	updateRunFunc          func(context.Context, domainworkflow.Run) domainworkflow.Run
	finalizeCancellationFn func(context.Context, domainworkflow.Run, dagWorkflowDefinition, map[string]dagNodeRun, map[string]string, error)
	queueDepthFunc         func() int
	logWarnFunc            func(context.Context, string, ...zap.Field)
	logDebugFunc           func(context.Context, string, ...zap.Field)
}

func newDAGRunExecutor(service *Service) *dagRunExecutor {
	return &dagRunExecutor{
		nodes:                  newDAGExecutor(service),
		metrics:                service.metrics,
		updateRunFunc:          service.updateRun,
		finalizeCancellationFn: service.finalizeRunCancellation,
		queueDepthFunc:         service.queueDepth,
		logWarnFunc:            service.logWarnCtx,
		logDebugFunc:           service.logDebugCtx,
	}
}

func (e *dagRunExecutor) updateRun(ctx context.Context, run domainworkflow.Run) domainworkflow.Run {
	return e.updateRunFunc(ctx, run)
}

func (e *dagRunExecutor) finalizeRunCancellation(ctx context.Context, run domainworkflow.Run, definition dagWorkflowDefinition, nodeRuns map[string]dagNodeRun, statuses map[string]string, err error) {
	e.finalizeCancellationFn(ctx, run, definition, nodeRuns, statuses, err)
}

func (e *dagRunExecutor) executeReadyDAGNodes(ctx context.Context, principal domainidentity.Principal, app domainapp.App, input domainworkflow.Input, binding domaincatalog.ApplicationEnvironment, ready []dagWorkflowNode, run domainworkflow.Run, artifactState map[string]any) []dagExecutionResult {
	return e.nodes.executeReady(ctx, principal, app, input, binding, ready, run, artifactState)
}

func (e *dagRunExecutor) queueDepth() int {
	return e.queueDepthFunc()
}

func (e *dagRunExecutor) logWarnCtx(ctx context.Context, message string, fields ...zap.Field) {
	e.logWarnFunc(ctx, message, fields...)
}

func (e *dagRunExecutor) logDebugCtx(ctx context.Context, message string, fields ...zap.Field) {
	e.logDebugFunc(ctx, message, fields...)
}

type dagRunState struct {
	principal          domainidentity.Principal
	app                domainapp.App
	input              domainworkflow.Input
	binding            domaincatalog.ApplicationEnvironment
	definition         dagWorkflowDefinition
	run                domainworkflow.Run
	nodeRuns           map[string]dagNodeRun
	statuses           map[string]string
	artifactState      map[string]any
	stopFailureSources map[string]struct{}
	startedAt          time.Time
}

func (e *dagRunExecutor) run(ctx context.Context, principal domainidentity.Principal, app domainapp.App, input domainworkflow.Input, binding domaincatalog.ApplicationEnvironment, definition dagWorkflowDefinition, run domainworkflow.Run) {
	state := newDAGRunState(principal, app, input, binding, definition, run)
	e.start(ctx, state)

	for len(state.statuses) < len(state.definition.Nodes) {
		if e.cancelIfRequested(ctx, state) {
			return
		}
		ready, progressed := state.collectReadyNodes()
		if progressed {
			e.persist(ctx, state)
		}
		if len(ready) == 0 {
			paused, blocked := e.handleNoReadyNodes(ctx, state, progressed)
			if paused {
				return
			}
			if blocked {
				break
			}
			continue
		}

		state.markNodesRunning(ready)
		e.persist(ctx, state)
		results := e.executeReadyDAGNodes(
			ctx,
			state.principal,
			state.app,
			state.input,
			state.binding,
			ready,
			state.run,
			state.artifactState,
		)
		state.applyResults(results)
		e.persist(ctx, state)
	}

	e.finish(ctx, state)
}

func newDAGRunState(principal domainidentity.Principal, app domainapp.App, input domainworkflow.Input, binding domaincatalog.ApplicationEnvironment, definition dagWorkflowDefinition, run domainworkflow.Run) *dagRunState {
	run = cloneRunForAsyncWorker(run)
	nodeRuns := restoreNodeRuns(definition, run.NodeRuns)
	return &dagRunState{
		principal:          principal,
		app:                app,
		input:              input,
		binding:            binding,
		definition:         definition,
		run:                run,
		nodeRuns:           nodeRuns,
		statuses:           collectNodeStatuses(nodeRuns),
		artifactState:      restoredDAGArtifactState(run.Metadata),
		stopFailureSources: dagStopFailureSourcesFromNodeRuns(definition, nodeRuns),
		startedAt:          time.Now(),
	}
}

func (e *dagRunExecutor) start(ctx context.Context, state *dagRunState) {
	if e.metrics != nil {
		e.metrics.RecordStart(runtimeobs.ComponentWorkflowRunner, state.run.ID, e.queueDepth(), len(state.definition.Nodes))
	}
	e.logDebugCtx(ctx, "workflow execution started", zap.String("runID", state.run.ID), zap.String("applicationID", state.run.ApplicationID))
	state.run.Status = "running"
	ensureDAGExecutionMetadata(&state.run)
	appendDAGRunEvent(&state.run, map[string]any{
		"type":   "workflow_started",
		"status": state.run.Status,
		"mode":   state.definition.Mode,
	})
	e.persist(ctx, state)
}

func (e *dagRunExecutor) persist(ctx context.Context, state *dagRunState) {
	state.run.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	state.run = syncRunNodeState(state.run, state.definition, state.nodeRuns)
	state.run = e.updateRun(ctx, state.run)
}

func (e *dagRunExecutor) cancelIfRequested(ctx context.Context, state *dagRunState) bool {
	err := ctx.Err()
	if err == nil {
		return false
	}
	e.finalizeRunCancellation(ctx, state.run, state.definition, state.nodeRuns, state.statuses, err)
	if e.metrics != nil {
		e.metrics.RecordFinish(runtimeobs.ComponentWorkflowRunner, state.run.ID, time.Since(state.startedAt), e.queueDepth(), len(state.definition.Nodes), runtimeobs.OutcomeCanceled, err)
	}
	e.logWarnCtx(ctx, "workflow execution canceled", zap.String("runID", state.run.ID), zap.Error(err))
	return true
}

func (s *dagRunState) collectReadyNodes() ([]dagWorkflowNode, bool) {
	ready := make([]dagWorkflowNode, 0)
	progressed := false
	for _, node := range s.definition.Nodes {
		if s.statuses[node.ID] != "" {
			continue
		}
		isReady, skipped := resolveDAGNodeReadiness(s.definition, node, incomingEdgesForNode(s.definition, node.ID), s.statuses)
		if skipped {
			s.skipNode(node, "conditions not met", map[string]any{"reason": "dependency_condition"})
			progressed = true
			continue
		}
		if !isReady {
			continue
		}
		if shouldStopDAGNodeAfterFailure(s.definition, node.ID, s.stopFailureSources) {
			s.skipNode(node, "stopped after failure policy", map[string]any{"reason": "failure_policy_stop"})
			progressed = true
			continue
		}
		matched, reason := evaluateDAGRunCondition(node.RunCondition, s.app, s.input, s.binding, s.artifactState)
		if !matched {
			s.skipRunCondition(node, reason)
			progressed = true
			continue
		}
		ready = append(ready, node)
	}
	return ready, progressed
}

func (s *dagRunState) skipNode(node dagWorkflowNode, summary string, metadata map[string]any) {
	entry := s.nodeRuns[node.ID]
	entry.Status = "skipped"
	entry.Summary = summary
	entry.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	s.nodeRuns[node.ID] = entry
	s.statuses[node.ID] = entry.Status
	appendDAGNodeEvent(&s.run, node.ID, "node_skipped", entry.Status, entry.Summary, metadata)
}

func (s *dagRunState) skipRunCondition(node dagWorkflowNode, reason string) {
	s.skipNode(node, reason, map[string]any{"reason": "run_condition", "runCondition": node.RunCondition})
	recordDAGNodeOutputs(&s.run, node.ID, map[string]any{
		"runCondition": map[string]any{
			"expression": node.RunCondition,
			"matched":    false,
			"reason":     reason,
		},
	})
}

func (e *dagRunExecutor) handleNoReadyNodes(ctx context.Context, state *dagRunState, progressed bool) (bool, bool) {
	switch {
	case hasWaitingApproval(state.nodeRuns):
		state.run.Status = workflowStatusWaitingApproval
		e.persist(ctx, state)
		return true, false
	case hasWaitingExecution(state.nodeRuns):
		state.run.Status = workflowStatusWaitingExecution
		e.persist(ctx, state)
		return true, false
	default:
		return false, !progressed
	}
}

func (s *dagRunState) markNodesRunning(nodes []dagWorkflowNode) {
	for _, node := range nodes {
		entry := s.nodeRuns[node.ID]
		entry.Status = "running"
		entry.StartedAt = time.Now().UTC().Format(time.RFC3339)
		s.nodeRuns[node.ID] = entry
		appendDAGNodeEvent(&s.run, node.ID, "node_started", entry.Status, "", nil)
	}
}

func (s *dagRunState) applyResults(results []dagExecutionResult) {
	for _, result := range results {
		entry := s.nodeRuns[result.nodeID]
		entry.Status = result.status
		entry.Summary = result.summary
		entry.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		s.nodeRuns[result.nodeID] = entry
		s.statuses[result.nodeID] = result.status
		recordDAGNodeOutputs(&s.run, result.nodeID, metadataDAGExecutionResult(result))
		s.appendResultEvents(result)
		s.mergeArtifacts(result)
		if result.status == "failed" {
			s.recordFailure(result)
		}
	}
}

func (s *dagRunState) appendResultEvents(result dagExecutionResult) {
	for _, event := range result.events {
		appendDAGNodeEvent(&s.run, result.nodeID, mapString(event, "type"), result.status, result.summary, event)
	}
}

func (s *dagRunState) mergeArtifacts(result dagExecutionResult) {
	for key, value := range result.outputs {
		s.artifactState[key] = value
	}
	for key, value := range result.artifacts {
		s.artifactState[key] = value
	}
	if len(result.outputs) == 0 && len(result.artifacts) == 0 {
		return
	}
	if s.run.Metadata == nil {
		s.run.Metadata = map[string]any{}
	}
	s.run.Metadata["artifacts"] = metadataDAGArtifactState(s.artifactState)
}

func (s *dagRunState) recordFailure(result dagExecutionResult) {
	policy := effectiveDAGFailurePolicy(s.definition, nodeByID(s.definition, result.nodeID))
	if policy == "stop" {
		s.stopFailureSources[result.nodeID] = struct{}{}
	}
	recordDAGFailurePolicy(&s.run, result.nodeID, policy, result.summary)
}

func (e *dagRunExecutor) finish(ctx context.Context, state *dagRunState) {
	finalStatus := state.finalStatus()
	state.run.Status = finalStatus
	e.persist(ctx, state)
	duration := time.Since(state.startedAt)
	if finalStatus == workflowStatusWaitingApproval || finalStatus == workflowStatusWaitingExecution {
		e.logDebugCtx(ctx, "workflow execution paused", zap.String("runID", state.run.ID), zap.String("applicationID", state.run.ApplicationID), zap.String("status", finalStatus), zap.Duration("duration", duration))
		return
	}
	e.recordFinishMetrics(state, finalStatus, duration)
	if finalStatus == "failed" {
		e.logWarnCtx(ctx, "workflow execution failed", zap.String("runID", state.run.ID), zap.String("applicationID", state.run.ApplicationID), zap.Duration("duration", duration))
		return
	}
	e.logDebugCtx(ctx, "workflow execution completed", zap.String("runID", state.run.ID), zap.String("applicationID", state.run.ApplicationID), zap.Duration("duration", duration))
}

func (s *dagRunState) finalStatus() string {
	for _, node := range s.definition.Nodes {
		status := s.statuses[node.ID]
		if status == workflowStatusWaitingApproval || status == workflowStatusWaitingExecution {
			return status
		}
		if status == "failed" && dagFailureCountsAsWorkflowFailure(s.definition, node) {
			return "failed"
		}
		if status == "" {
			entry := s.nodeRuns[node.ID]
			entry.Status = "skipped"
			entry.Summary = "unresolved DAG dependency"
			entry.FinishedAt = time.Now().UTC().Format(time.RFC3339)
			s.nodeRuns[node.ID] = entry
			s.statuses[node.ID] = entry.Status
		}
	}
	return "completed"
}

func (e *dagRunExecutor) recordFinishMetrics(state *dagRunState, finalStatus string, duration time.Duration) {
	if e.metrics == nil {
		return
	}
	outcome := runtimeobs.OutcomeSucceeded
	var err error
	if finalStatus == "failed" {
		outcome = runtimeobs.OutcomeFailed
		err = fmt.Errorf("workflow finished with status %s", finalStatus)
	}
	e.metrics.RecordFinish(runtimeobs.ComponentWorkflowRunner, state.run.ID, duration, e.queueDepth(), len(state.definition.Nodes), outcome, err)
}
