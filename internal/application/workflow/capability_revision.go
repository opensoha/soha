package workflow

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) ResumeCapabilityTask(ctx context.Context, principal domainidentity.Principal, id string, input domainaigateway.CapabilityTaskRevisionInput) (domainaigateway.CapabilityTask, error) {
	if err := s.authorizePermission(ctx, principal, appaccess.PermAIGatewayInvoke); err != nil {
		return domainaigateway.CapabilityTask{}, err
	}
	repo, err := s.capabilityRepository()
	if err != nil {
		return domainaigateway.CapabilityTask{}, err
	}
	run, err := s.repo.Get(ctx, id)
	if err != nil {
		return domainaigateway.CapabilityTask{}, err
	}
	intent, err := domainworkflow.CapabilityIntentFrom(run)
	if err != nil || intent.ActorID != principal.UserID {
		return domainaigateway.CapabilityTask{}, apperrors.ErrNotFound
	}
	if input.ExpectedVersion <= 0 || run.Version != input.ExpectedVersion || intent.Input.Plan.Goal != input.Plan.Goal {
		return domainaigateway.CapabilityTask{}, fmt.Errorf("%w: reload the task; a revision must preserve its goal", apperrors.ErrConflict)
	}
	// Public task views omit SecretRefs. A retained step can reuse its opaque
	// references without exposing them; an explicit empty map still clears them.
	refs := map[string]map[string]string{}
	for _, step := range intent.Input.Plan.Steps {
		refs[step.ID] = step.Call.SecretRefs
	}
	input.Plan.Steps = append([]domainaigateway.CapabilityPlanStep(nil), input.Plan.Steps...)
	for i := range input.Plan.Steps {
		if input.Plan.Steps[i].Call.SecretRefs == nil {
			input.Plan.Steps[i].Call.SecretRefs = maps.Clone(refs[input.Plan.Steps[i].ID])
		}
	}
	intent.Input.Plan = input.Plan
	validation, err := s.capabilityRuntime.ValidateCapabilityPlan(ctx, principal, intent.Input)
	if err != nil {
		return domainaigateway.CapabilityTask{}, err
	}
	if !validation.Valid {
		return domainaigateway.CapabilityTask{}, fmt.Errorf("%w: revised plan is invalid", apperrors.ErrInvalidArgument)
	}
	intent.Digest, intent.ActorTokenID = validation.Digest, principal.AccessTokenID
	timeout := input.Plan.TimeoutSeconds
	if timeout == 0 {
		timeout = 3600
	}
	intent.Deadline = time.Now().UTC().Add(time.Duration(timeout) * time.Second)
	run, err = repo.ReviseCapabilityRun(ctx, id, input.ExpectedVersion, func(current domainworkflow.Run) (domainworkflow.Run, error) {
		return reviseCapabilityRun(current, intent)
	})
	if err != nil {
		return domainaigateway.CapabilityTask{}, err
	}
	return s.capabilityRuntime.VisibleCapabilityTask(ctx, principal, run)
}

func reviseCapabilityRun(run domainworkflow.Run, intent domainworkflow.CapabilityIntent) (domainworkflow.Run, error) {
	previous, err := domainworkflow.CapabilityIntentFrom(run)
	if err != nil {
		return run, err
	}
	history, err := domainworkflow.CapabilityRevisionsFrom(run)
	if err != nil {
		return run, err
	}
	if previous.PlanVersion >= 20 {
		return run, fmt.Errorf("%w: task has reached 20 plan revisions", apperrors.ErrConflict)
	}
	history = append(history, domainworkflow.CapabilityRevision{Intent: previous, Nodes: run.NodeRuns, Status: run.Status, Version: run.Version, ArchivedAt: time.Now().UTC().Format(time.RFC3339)})
	if err := validateCapabilityRevision(intent.Input.Plan, history); err != nil {
		return run, err
	}
	nodes := make([]domainworkflow.NodeRun, 0, len(intent.Input.Plan.Steps))
	for _, step := range intent.Input.Plan.Steps {
		node := domainworkflow.NodeRun{NodeID: step.ID, Name: step.Call.ToolName, Type: "capability", Status: "pending"}
		for _, current := range run.NodeRuns {
			if current.NodeID == step.ID {
				node = current
				if node.Status == "blocked" {
					node.Status, node.Summary = "pending", ""
					if node.DispatchAttempted {
						node.Status = "running"
					}
				}
				break
			}
		}
		nodes = append(nodes, node)
	}
	intent.PlanVersion = previous.PlanVersion + 1
	run.Metadata = maps.Clone(run.Metadata)
	if run.Metadata["capabilityInitialDigest"] == nil {
		run.Metadata["capabilityInitialDigest"] = previous.Digest
	}
	run.Metadata["capabilityIntent"], run.Metadata["capabilityRevisions"] = intent, history
	delete(run.Metadata, "capabilityNextPollAt")
	run.NodeRuns = nodes
	return run, nil
}

func validateCapabilityRevision(plan domainaigateway.CapabilityPlan, history []domainworkflow.CapabilityRevision) error {
	steps := map[string]domainaigateway.CapabilityPlanStep{}
	for _, step := range plan.Steps {
		steps[step.ID] = step
	}
	for revisionIndex, revision := range history {
		for _, node := range revision.Nodes {
			if !node.DispatchAttempted {
				continue
			}
			step, retained := steps[node.NodeID]
			unresolved := capabilityChildUnresolved(node)
			if revisionIndex == len(history)-1 && unresolved && (!retained || node.ControlCall != nil) {
				return fmt.Errorf("%w: unresolved step %s must remain; finish pending cancellation before resuming", apperrors.ErrConflict, node.NodeID)
			}
			if !retained {
				continue
			}
			for _, oldStep := range revision.Intent.Input.Plan.Steps {
				if oldStep.ID != node.NodeID {
					continue
				}
				a, _ := json.Marshal(oldStep)
				b, _ := json.Marshal(step)
				if string(a) != string(b) {
					return fmt.Errorf("%w: dispatched step %s is frozen; use a new ID for a new call", apperrors.ErrConflict, node.NodeID)
				}
			}
			for _, verification := range plan.VerificationSteps {
				if verification == node.NodeID && !unresolved {
					return fmt.Errorf("%w: verification requires a new step ID and fresh evidence", apperrors.ErrConflict)
				}
			}
		}
	}
	return nil
}

func capabilityChildUnresolved(node domainworkflow.NodeRun) bool {
	if node.Invocation == nil {
		return true
	}
	if node.Invocation.Task != nil {
		return !node.Invocation.Task.Terminal
	}
	return node.Invocation.Result != "success"
}

func (s *Service) GetCapabilityTaskRevision(ctx context.Context, principal domainidentity.Principal, id string, version int) (domainaigateway.CapabilityTask, error) {
	if _, err := s.capabilityRepository(); err != nil {
		return domainaigateway.CapabilityTask{}, err
	}
	run, err := s.repo.Get(ctx, id)
	if err != nil {
		return domainaigateway.CapabilityTask{}, err
	}
	intent, err := domainworkflow.CapabilityIntentFrom(run)
	if err != nil || intent.ActorID != principal.UserID {
		return domainaigateway.CapabilityTask{}, apperrors.ErrNotFound
	}
	if version == intent.PlanVersion {
		return s.capabilityRuntime.VisibleCapabilityTask(ctx, principal, run)
	}
	history, err := domainworkflow.CapabilityRevisionsFrom(run)
	if err != nil {
		return domainaigateway.CapabilityTask{}, err
	}
	for _, revision := range history {
		if revision.Intent.PlanVersion != version {
			continue
		}
		run.Metadata = maps.Clone(run.Metadata)
		run.Metadata["capabilityIntent"] = revision.Intent
		run.NodeRuns, run.Status, run.Version, run.UpdatedAt = revision.Nodes, revision.Status, revision.Version, revision.ArchivedAt
		return s.capabilityRuntime.VisibleCapabilityTask(ctx, principal, run)
	}
	return domainaigateway.CapabilityTask{}, apperrors.ErrNotFound
}
