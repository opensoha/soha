package workflow

import (
	"context"
	"errors"
	"time"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"go.uber.org/zap"
)

func capabilityDAG(plan domainaigateway.CapabilityPlan) dagWorkflowDefinition {
	dag := dagWorkflowDefinition{SchemaVersion: 3, Mode: domainworkflow.ScopeCapabilityTask}
	for _, step := range plan.Steps {
		dag.Nodes = append(dag.Nodes, dagWorkflowNode{ID: step.ID, Name: step.Call.ToolName, Type: "capability", CapabilityRef: step.Call.ToolName})
		for _, dependency := range step.DependsOn {
			dag.Edges = append(dag.Edges, dagWorkflowEdge{ID: dependency + ":" + step.ID, Source: dependency, Target: step.ID, Condition: "success"})
		}
	}
	return dag
}

func (s *Service) runCapabilityTick(parent context.Context, run domainworkflow.Run) {
	ctx, cancel := context.WithTimeout(parent, deliveryRunLease*3/4)
	defer cancel()
	repo, err := s.capabilityRepository()
	if err != nil {
		return
	}
	intent, err := domainworkflow.CapabilityIntentFrom(run)
	if err != nil {
		run.Status = "blocked"
		_, _ = repo.SaveManagedRun(ctx, run, true)
		return
	}
	deadline := intent.Deadline
	for _, node := range run.NodeRuns {
		if node.DispatchAttempted && !capabilityNodeTerminal(node.Status) {
			if candidate := domainworkflow.CapabilityNodeDeadline(intent, node); candidate.Before(deadline) {
				deadline = candidate
			}
		}
	}
	if run.StopReason == "" && time.Now().After(deadline) {
		run, err = repo.StopManagedRun(ctx, run.ID, "failure", "capability task deadline exceeded")
		if err != nil {
			return
		}
	}
	principal, err := s.deliveryPrincipals.CurrentExecutionPrincipal(ctx, intent.ActorID, intent.ActorTokenID)
	if err != nil {
		// No roles cached in a task may authorize a resumed operation.
		run.Status = "blocked"
		run.StopSummary = "execution identity is no longer authorized; child state is unconfirmed"
		_, _ = repo.SaveManagedRun(ctx, run, true)
		return
	}
	run, err = s.advanceCapabilityRun(ctx, repo, principal, intent, run)
	if err != nil {
		if !errors.Is(err, apperrors.ErrConflict) && ctx.Err() == nil {
			s.logWarnCtx(ctx, "capability task reconciliation deferred", zap.String("run_id", run.ID), zap.Error(err))
		}
		return
	}
	run.Status = capabilityRunStatus(run, intent.Input.Plan)
	run.Metadata["capabilityNextPollAt"] = time.Now().UTC().Add(2 * time.Second).Format(time.RFC3339Nano)
	updated, err := repo.SaveManagedRun(ctx, run, true)
	if err == nil {
		s.publishRunUpdate(updated)
	}
}

func (s *Service) advanceCapabilityRun(ctx context.Context, repo CapabilityRepository, principal domainidentity.Principal, intent domainworkflow.CapabilityIntent, run domainworkflow.Run) (domainworkflow.Run, error) {
	for _, node := range run.NodeRuns {
		if node.Status == "failed" && run.StopReason == "" {
			var err error
			run, err = repo.StopManagedRun(ctx, run.ID, "failure", "capability step failed")
			if err != nil {
				return run, err
			}
		}
	}
	if run.StopReason != "" {
		for _, node := range run.NodeRuns {
			if capabilityNodeTerminal(node.Status) {
				continue
			}
			var err error
			run, err = repo.UpdateCapabilityNode(ctx, run, node.NodeID, func(current domainworkflow.Run, entry domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
				if !entry.DispatchAttempted {
					entry.Status, entry.Summary = "canceled", "not dispatched because the task stopped"
					return entry, nil
				}
				return s.capabilityRuntime.AdvanceCapabilityNode(ctx, principal, current, entry)
			})
			if err != nil {
				return run, err
			}
		}
		return run, nil
	}
	// ponytail: one active node per goal; reuse DAG readiness and the shared worker
	// pool. Add resource-scoped parallelism only when providers expose safe locks.
	for _, node := range run.NodeRuns {
		if node.Status != "pending" && !capabilityNodeTerminal(node.Status) {
			return s.advanceCapabilityNode(ctx, repo, principal, run, node)
		}
	}
	state := newDAGRunState(principal, domainapp.App{}, domainworkflow.Input{}, domaincatalog.ApplicationEnvironment{}, capabilityDAG(intent.Input.Plan), run)
	ready, _ := state.collectReadyNodes()
	if len(ready) == 0 {
		return run, nil
	}
	return s.advanceCapabilityNode(ctx, repo, principal, run, state.nodeRuns[ready[0].ID])
}

func (s *Service) advanceCapabilityNode(ctx context.Context, repo CapabilityRepository, principal domainidentity.Principal, run domainworkflow.Run, node domainworkflow.NodeRun) (domainworkflow.Run, error) {
	if node.PreparedCall == nil {
		var err error
		run, err = repo.UpdateCapabilityNode(ctx, run, node.NodeID, func(current domainworkflow.Run, entry domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
			if current.StopReason != "" {
				return entry, apperrors.ErrConflict
			}
			prepared, err := s.capabilityRuntime.PrepareCapabilityNode(ctx, principal, current, entry)
			if err != nil {
				entry.Status, entry.Summary = "blocked", "input, capability or permission changed; validate a revised plan"
				return entry, nil
			}
			prepared.Status, prepared.StartedAt = "running", time.Now().UTC().Format(time.RFC3339)
			// Commit this marker before any external mutation. On restart, only
			// the same frozen, explicitly idempotent call may be dispatched.
			prepared.DispatchAttempted = true
			return prepared, nil
		})
		if err != nil {
			return run, err
		}
	}
	return repo.UpdateCapabilityNode(ctx, run, node.NodeID, func(current domainworkflow.Run, entry domainworkflow.NodeRun) (domainworkflow.NodeRun, error) {
		if capabilityNodeTerminal(entry.Status) {
			return entry, nil
		}
		return s.capabilityRuntime.AdvanceCapabilityNode(ctx, principal, current, entry)
	})
}

func capabilityNodeTerminal(status string) bool {
	switch status {
	case "completed", "failed", "canceled", "blocked", "inconclusive":
		return true
	default:
		return false
	}
}

func capabilityRunStatus(run domainworkflow.Run, plan domainaigateway.CapabilityPlan) string {
	allDone, waitingApproval, failed := true, false, false
	for _, node := range run.NodeRuns {
		if node.Status == "blocked" {
			return "blocked"
		}
		failed = failed || node.Status == "failed"
		allDone = allDone && capabilityNodeTerminal(node.Status)
		waitingApproval = waitingApproval || node.Status == "waiting_approval"
	}
	if !allDone {
		if run.StopReason != "" {
			return "canceling"
		}
		if waitingApproval {
			return "waiting_approval"
		}
		return "running"
	}
	if failed {
		return "failed"
	}
	if run.StopReason != "" {
		if run.StopReason == "user" {
			return "canceled"
		}
		return "failed"
	}
	switch domainworkflow.AssessCapabilityRun(run, plan).Verdict {
	case "satisfied":
		return "completed"
	case "unsatisfied":
		return "failed"
	default:
		return "inconclusive"
	}
}
