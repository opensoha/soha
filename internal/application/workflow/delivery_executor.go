package workflow

import (
	"context"
	"errors"
	"time"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"go.uber.org/zap"
)

const deliveryRunLease = time.Minute

func (s *Service) claimDeliveryRun(ctx context.Context, owner string) (dagRunTask, bool) {
	repo, err := s.deliveryRepository()
	if err != nil {
		return dagRunTask{}, false
	}
	run, err := repo.ClaimManagedRun(ctx, owner, deliveryRunLease)
	if err != nil {
		if !errors.Is(err, apperrors.ErrNotFound) && ctx.Err() == nil {
			s.logWarnCtx(ctx, "delivery run claim failed", zap.Error(err))
		}
		return dagRunTask{}, false
	}
	return dagRunTask{run: run}, true
}

// One bounded pass uses the same DAG state, readiness and node executor as
// application runs. Waiting tasks remain durable; no goroutine owns their life.
func (e *dagRunExecutor) runDeliveryTick(ctx context.Context, run domainworkflow.Run) {
	ctx, cancel := context.WithTimeout(ctx, deliveryRunLease*3/4)
	defer cancel()
	repo, err := e.delivery.deliveryRepository()
	if err != nil {
		return
	}
	batch, current, err := repo.GetDeliveryBatch(ctx, run.DeliveryBatchID)
	if err != nil || !sameDeliveryLease(run, current) {
		return
	}
	definition, ok := definitionFromRunMetadata(run)
	if !ok || definition.Mode != domainworkflow.ScopeDeliveryBatch {
		e.logWarnCtx(ctx, "delivery run definition is missing", zap.String("run_id", run.ID))
		return
	}
	state := newDAGRunState(domainidentity.Principal{}, domainapp.App{}, domainworkflow.Input{}, domaincatalog.ApplicationEnvironment{}, definition, run)
	if err := e.advanceDeliveryRun(ctx, repo, batch, state); err != nil {
		if ctx.Err() == nil {
			e.logWarnCtx(ctx, "delivery run reconciliation paused", zap.String("run_id", run.ID), zap.Error(err))
		}
		return
	}
	state.run.Status = deliveryRunStatus(batch, state)
	if err := e.persistDeliveryRun(ctx, repo, state, true); err != nil && ctx.Err() == nil {
		e.logWarnCtx(ctx, "delivery run persistence deferred", zap.String("run_id", run.ID), zap.Error(err))
	}
}

func sameDeliveryLease(expected, actual domainworkflow.Run) bool {
	return expected.ID == actual.ID && expected.Version == actual.Version && expected.FencingToken == actual.FencingToken && expected.LeaseOwner != "" && expected.LeaseOwner == actual.LeaseOwner
}

func (e *dagRunExecutor) advanceDeliveryRun(ctx context.Context, repo DeliveryRepository, batch domainworkflow.DeliveryBatch, state *dagRunState) error {
	if err := e.stopFailedDeliveryRun(ctx, repo, batch, state); err != nil {
		return err
	}
	if state.run.StopReason != "" {
		return e.cancelDeliveryNodes(ctx, repo, state)
	}
	if err := e.executeDeliveryNodes(ctx, repo, state, activeDeliveryNodes(state)); err != nil {
		return err
	}
	if err := e.stopFailedDeliveryRun(ctx, repo, batch, state); err != nil {
		return err
	}
	if state.run.StopReason != "" {
		return e.cancelDeliveryNodes(ctx, repo, state)
	}
	ready, _ := state.collectReadyNodes()
	limit := min(batch.Definition.MaxConcurrency, e.nodes.parallelism) - len(activeDeliveryNodes(state))
	if limit < 0 {
		limit = 0
	}
	ready = ready[:min(len(ready), limit)]
	if len(ready) == 0 {
		return nil
	}
	state.markNodesRunning(ready)
	if err := e.persistDeliveryRun(ctx, repo, state, false); err != nil {
		return err
	}
	if err := e.executeDeliveryNodes(ctx, repo, state, ready); err != nil {
		return err
	}
	if err := e.stopFailedDeliveryRun(ctx, repo, batch, state); err != nil {
		return err
	}
	if state.run.StopReason != "" {
		return e.cancelDeliveryNodes(ctx, repo, state)
	}
	return nil
}

func (e *dagRunExecutor) executeDeliveryNodes(ctx context.Context, repo DeliveryRepository, state *dagRunState, nodes []dagWorkflowNode) error {
	if len(nodes) == 0 {
		return nil
	}
	results := e.executeReadyDAGNodes(ctx, state.principal, state.app, state.input, state.binding, nodes, state.run, state.artifactState)
	var retryErr error
	for _, result := range results {
		if result.err != nil {
			retryErr = errors.Join(retryErr, result.err)
			continue
		}
		state.applyResults([]dagExecutionResult{result})
	}
	// Persist successful siblings even when a remote read failed. The failed
	// read keeps its old task reference and is reconciled on the next claim.
	if err := e.persistDeliveryRun(ctx, repo, state, false); err != nil {
		return err
	}
	if retryErr != nil {
		e.logWarnCtx(ctx, "delivery stage observation deferred", zap.String("run_id", state.run.ID), zap.Error(retryErr))
	}
	return ctx.Err()
}

func (e *dagRunExecutor) persistDeliveryRun(ctx context.Context, repo DeliveryRepository, state *dagRunState, release bool) error {
	state.run = syncRunNodeState(state.run, state.definition, state.nodeRuns)
	updated, err := repo.SaveManagedRun(ctx, state.run, release)
	if err != nil {
		return err
	}
	state.run = updated
	e.delivery.publishRunUpdate(updated)
	return nil
}

func (e *dagRunExecutor) stopFailedDeliveryRun(ctx context.Context, repo DeliveryRepository, batch domainworkflow.DeliveryBatch, state *dagRunState) error {
	if !batch.Definition.StopsOnFailure() || state.run.StopReason != "" {
		return nil
	}
	for _, node := range state.definition.Nodes {
		entry := state.nodeRuns[node.ID]
		if entry.Status != "failed" {
			continue
		}
		stopped, err := repo.StopManagedRun(ctx, state.run.ID, "failure", entry.Summary)
		if err != nil {
			return err
		}
		// Stop changes the version. Another owner must never be adopted here.
		if stopped.FencingToken != state.run.FencingToken || stopped.LeaseOwner != state.run.LeaseOwner {
			return apperrors.ErrConflict
		}
		state.run = stopped
		return nil
	}
	return nil
}

func (e *dagRunExecutor) cancelDeliveryNodes(ctx context.Context, repo DeliveryRepository, state *dagRunState) error {
	for _, node := range state.definition.Nodes {
		entry := state.nodeRuns[node.ID]
		if entry.Status != "pending" && entry.Status != "" {
			continue
		}
		entry.Status, entry.Summary = "canceled", "not dispatched because this delivery was stopped"
		entry.FinishedAt = time.Now().UTC().Format(time.RFC3339)
		state.nodeRuns[node.ID], state.statuses[node.ID] = entry, entry.Status
	}
	return e.executeDeliveryNodes(ctx, repo, state, activeDeliveryNodes(state))
}

func activeDeliveryNodes(state *dagRunState) []dagWorkflowNode {
	nodes := []dagWorkflowNode{}
	for _, node := range state.definition.Nodes {
		status := state.nodeRuns[node.ID].Status
		if status != "pending" && status != "" && !deliveryNodeTerminal(status) {
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func deliveryRunStatus(batch domainworkflow.DeliveryBatch, state *dagRunState) string {
	active := activeDeliveryNodes(state)
	if state.run.StopReason != "" {
		if len(active) > 0 {
			return "canceling"
		}
		if state.run.StopReason == "failure" {
			return "failed"
		}
		return "canceled"
	}
	if len(active) > 0 {
		if hasWaitingApproval(state.nodeRuns) {
			return workflowStatusWaitingApproval
		}
		return workflowStatusWaitingExecution
	}
	for _, node := range state.nodeRuns {
		if !deliveryNodeTerminal(node.Status) {
			return "running"
		}
	}
	completed := 0
	for _, target := range batch.Targets {
		last := target.Target.ID + ":health"
		if target.Target.Action == "build" {
			last = target.Target.ID + ":done"
		}
		if state.nodeRuns[last].Status == "completed" {
			completed++
		}
	}
	if completed == len(batch.Targets) {
		return "completed"
	}
	if completed > 0 {
		return "partially_completed"
	}
	return "failed"
}
