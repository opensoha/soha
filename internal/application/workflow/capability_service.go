package workflow

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Gateway owns discovery, binding and policy. Workflow owns the existing durable
// Run and lease; each domain still owns its child operation and side effects.
type CapabilityRuntime interface {
	ValidateCapabilityPlan(context.Context, domainidentity.Principal, domainaigateway.CapabilityTaskInput) (domainaigateway.CapabilityPlanValidation, error)
	PrepareCapabilityNode(context.Context, domainidentity.Principal, domainworkflow.Run, domainworkflow.NodeRun) (domainworkflow.NodeRun, error)
	AdvanceCapabilityNode(context.Context, domainidentity.Principal, domainworkflow.Run, domainworkflow.NodeRun) (domainworkflow.NodeRun, error)
	VisibleCapabilityTask(context.Context, domainidentity.Principal, domainworkflow.Run) (domainaigateway.CapabilityTask, error)
}

type CapabilityRepository interface {
	DeliveryRepository
	CreateCapabilityRun(context.Context, domainworkflow.Run) (domainworkflow.Run, error)
	ReviseCapabilityRun(context.Context, string, int64, func(domainworkflow.Run) (domainworkflow.Run, error)) (domainworkflow.Run, error)
	ListCapabilityRuns(context.Context, string, int) ([]domainworkflow.Run, error)
	UpdateCapabilityNode(context.Context, domainworkflow.Run, string, func(domainworkflow.Run, domainworkflow.NodeRun) (domainworkflow.NodeRun, error)) (domainworkflow.Run, error)
}

func (s *Service) SetCapabilityRuntime(runtime CapabilityRuntime, principals DeliveryPrincipalReader) {
	s.capabilityRuntime, s.deliveryPrincipals = runtime, principals
	s.configureManagedPoll()
}

func (s *Service) capabilityRepository() (CapabilityRepository, error) {
	repo, ok := s.repo.(CapabilityRepository)
	if !ok || s.capabilityRuntime == nil || s.deliveryPrincipals == nil {
		return nil, fmt.Errorf("%w: capability task runtime is unavailable", apperrors.ErrInvalidArgument)
	}
	return repo, nil
}

func (s *Service) CreateCapabilityTask(ctx context.Context, principal domainidentity.Principal, input domainaigateway.CapabilityTaskInput) (domainaigateway.CapabilityTask, error) {
	if err := s.authorizePermission(ctx, principal, appaccess.PermAIGatewayInvoke); err != nil {
		return domainaigateway.CapabilityTask{}, err
	}
	repo, err := s.capabilityRepository()
	if err != nil {
		return domainaigateway.CapabilityTask{}, err
	}
	validation, err := s.capabilityRuntime.ValidateCapabilityPlan(ctx, principal, input)
	if err != nil {
		return domainaigateway.CapabilityTask{}, err
	}
	if !validation.Valid {
		return domainaigateway.CapabilityTask{}, fmt.Errorf("%w: capability plan is invalid; validate the plan for details", apperrors.ErrInvalidArgument)
	}
	now := time.Now().UTC()
	timeout := input.Plan.TimeoutSeconds
	if timeout == 0 {
		timeout = 3600
	}
	intent := domainworkflow.CapabilityIntent{Input: input, ActorID: principal.UserID, ActorTokenID: principal.AccessTokenID, Digest: validation.Digest, PlanVersion: 1, Deadline: now.Add(time.Duration(timeout) * time.Second)}
	run := domainworkflow.Run{ID: uuid.NewString(), Scope: domainworkflow.ScopeCapabilityTask, WorkflowName: input.Plan.Goal, Status: "queued", Metadata: map[string]any{"capabilityIntent": intent}, Steps: []domainworkflow.Step{}, CreatedAt: now.Format(time.RFC3339), UpdatedAt: now.Format(time.RFC3339)}
	for _, step := range input.Plan.Steps {
		run.NodeRuns = append(run.NodeRuns, domainworkflow.NodeRun{NodeID: step.ID, Name: step.Call.ToolName, Type: "capability", Status: "pending"})
	}
	run, err = repo.CreateCapabilityRun(ctx, run)
	if err != nil {
		return domainaigateway.CapabilityTask{}, err
	}
	return s.capabilityRuntime.VisibleCapabilityTask(ctx, principal, run)
}

func (s *Service) GetCapabilityTask(ctx context.Context, principal domainidentity.Principal, id string) (domainaigateway.CapabilityTask, error) {
	if _, err := s.capabilityRepository(); err != nil {
		return domainaigateway.CapabilityTask{}, err
	}
	run, err := s.repo.Get(ctx, id)
	if err != nil {
		return domainaigateway.CapabilityTask{}, err
	}
	return s.capabilityRuntime.VisibleCapabilityTask(ctx, principal, run)
}

func (s *Service) ListCapabilityTasks(ctx context.Context, principal domainidentity.Principal, limit int) ([]domainaigateway.CapabilityTask, error) {
	if err := s.authorizePermission(ctx, principal, appaccess.PermAIGatewayInvoke); err != nil {
		return nil, err
	}
	repo, err := s.capabilityRepository()
	if err != nil {
		return nil, err
	}
	runs, err := repo.ListCapabilityRuns(ctx, principal.UserID, limit)
	if err != nil {
		return nil, err
	}
	items := []domainaigateway.CapabilityTask{}
	for _, run := range runs {
		item, err := s.capabilityRuntime.VisibleCapabilityTask(ctx, principal, run)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func (s *Service) CancelCapabilityTask(ctx context.Context, principal domainidentity.Principal, id string) (domainaigateway.CapabilityTask, error) {
	if _, err := s.GetCapabilityTask(ctx, principal, id); err != nil {
		return domainaigateway.CapabilityTask{}, err
	}
	repo, err := s.capabilityRepository()
	if err != nil {
		return domainaigateway.CapabilityTask{}, err
	}
	run, err := repo.StopManagedRun(ctx, id, "user", "capability task cancellation requested")
	if err != nil {
		return domainaigateway.CapabilityTask{}, err
	}
	return s.capabilityRuntime.VisibleCapabilityTask(ctx, principal, run)
}
