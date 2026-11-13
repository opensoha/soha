package docker

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domain "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type projectDeployPlanTransaction interface {
	WithinProjectDeployPlan(context.Context, string, domain.ProjectDeployPreconditions, func(context.Context) error) error
}

func (s *Service) projectDeployPlanBinding(ctx context.Context, project domain.Project) (*domain.ProjectDeployPreconditions, error) {
	host, err := s.repo.GetHost(ctx, project.HostID)
	if err != nil {
		return nil, err
	}
	projectUID, e1 := uuid.Parse(project.ResourceUID)
	projectRevision, e2 := uuid.Parse(project.ConfigurationRevision)
	hostUID, e3 := uuid.Parse(host.ResourceUID)
	hostRevision, e4 := uuid.Parse(host.ConfigurationRevision)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil {
		return nil, fmt.Errorf("%w: Docker deployment configuration identity is unavailable", apperrors.ErrConflict)
	}
	value := &domain.ProjectDeployPreconditions{HostID: host.ID,
		Project: sohaapi.DockerConfigurationPreconditions{ExpectedUID: projectUID, ExpectedConfigurationRevision: projectRevision},
		Host:    sohaapi.DockerConfigurationPreconditions{ExpectedUID: hostUID, ExpectedConfigurationRevision: hostRevision}}
	if err := validateProjectDeployPlan(*value); err != nil {
		return nil, err
	}
	return value, nil
}

func validateProjectDeployPlan(value domain.ProjectDeployPreconditions) error {
	if value.HostID == "" || len(value.HostID) > 128 || strings.TrimSpace(value.HostID) != value.HostID ||
		value.Project.ExpectedUID == uuid.Nil || value.Project.ExpectedConfigurationRevision == uuid.Nil ||
		value.Host.ExpectedUID == uuid.Nil || value.Host.ExpectedConfigurationRevision == uuid.Nil {
		return fmt.Errorf("%w: complete Docker deployment preconditions are required", apperrors.ErrInvalidArgument)
	}
	return nil
}

func (s *Service) deployReviewedProject(ctx context.Context, principal domainidentity.Principal, id string, input domain.ProjectDeployInput) (domain.Operation, error) {
	conditions := *input.Preconditions
	input.Preconditions = &conditions
	if err := validateProjectDeployPlan(conditions); err != nil {
		return domain.Operation{}, err
	}
	if strings.TrimSpace(input.IdempotencyKey) == "" {
		return domain.Operation{}, fmt.Errorf("%w: reviewed deployment requires an original idempotency key", apperrors.ErrInvalidArgument)
	}
	claim, err := operationClaimFor("docker.project.deploy:"+id, principal, input.IdempotencyKey, input)
	if err != nil {
		return domain.Operation{}, err
	}
	if original, found, err := s.findClaimedOperation(ctx, claim); err != nil || found {
		return original, err
	}
	repository, ok := s.repo.(projectDeployPlanTransaction)
	if !ok {
		return domain.Operation{}, apperrors.ErrUnsupportedOperation
	}
	var operation domain.Operation
	err = repository.WithinProjectDeployPlan(ctx, id, conditions, func(txCtx context.Context) error {
		project, action, err := s.prepareProjectDeploy(txCtx, id, input.Action)
		if err != nil {
			return err
		}
		operation, err = s.enqueueIdempotentOperation(txCtx, "docker.project.deploy:"+id, principal, input.IdempotencyKey, input, OperationKindProjectDeploy, project.HostID, project.ID, "", buildProjectDeployPayload(project, action))
		return err
	})
	if err != nil {
		// A concurrent identical request may have committed while this request
		// waited for the configuration locks. Recover only that original claim.
		if original, found, lookupErr := s.findClaimedOperation(ctx, claim); found || lookupErr != nil {
			return original, lookupErr
		}
		return domain.Operation{}, err
	}
	s.recordOperation(ctx, principal, "docker.project.deploy.enqueue", id, id, "success", "enqueued reviewed Docker compose action", map[string]any{"operationId": operation.ID, "action": input.Action})
	return domain.WithOperationState(operation, time.Now().UTC()), nil
}

// Original-request recovery is read-only. It must not obtain a fresh plan or
// depend on mutable configuration after the original response was lost.
func (s *Service) GetProjectDeploymentRequest(ctx context.Context, principal domainidentity.Principal, id, key, action string) (domain.Operation, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerOperationsView); err != nil {
		return domain.Operation{}, err
	}
	if strings.TrimSpace(key) == "" || strings.TrimSpace(id) == "" {
		return domain.Operation{}, apperrors.ErrInvalidArgument
	}
	claim, err := operationClaimFor("docker.project.deploy:"+id, principal, key, nil)
	if err != nil {
		return domain.Operation{}, err
	}
	item, err := s.repo.GetOperation(ctx, claim.id)
	if err != nil {
		return domain.Operation{}, err
	}
	if item.ProjectID != id || item.OperationKind != OperationKindProjectDeploy || item.RequestedBy != firstNonEmpty(principal.UserID, principal.UserName) {
		return domain.Operation{}, apperrors.ErrNotFound
	}
	if err := domain.CheckScope(ctx, operationScope(item)); err != nil {
		return domain.Operation{}, err
	}
	if action != "" && action != fmt.Sprint(item.Payload["action"]) {
		return domain.Operation{}, fmt.Errorf("%w: original Docker deployment used a different action", apperrors.ErrConflict)
	}
	item.Payload, item.Result = nil, nil
	return domain.WithOperationState(item, time.Now().UTC()), nil
}
