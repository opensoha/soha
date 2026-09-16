package docker

import (
	"context"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Lookup uses exactly the same normalized inputs and existing idempotency record
// as dispatch. It never creates work, including after a canceled parent goal.
func (s *Service) FindQuickCreateOperation(ctx context.Context, principal domainidentity.Principal, input domaindocker.QuickCreateHostInput) (domaindocker.Operation, error) {
	input, _, err := s.prepareQuickCreateHost(input)
	if err != nil {
		return domaindocker.Operation{}, err
	}
	return s.findCapabilityOperation(ctx, principal, "docker.host.quick_create", input.IdempotencyKey, input)
}

func (s *Service) FindProjectDeployOperation(ctx context.Context, principal domainidentity.Principal, id string, input domaindocker.ProjectDeployInput) (domaindocker.Operation, error) {
	return s.findCapabilityOperation(ctx, principal, "docker.project.deploy:"+id, input.IdempotencyKey, input)
}

func (s *Service) FindServiceActionOperation(ctx context.Context, principal domainidentity.Principal, id string, input domaindocker.ServiceActionInput) (domaindocker.Operation, error) {
	return s.findCapabilityOperation(ctx, principal, "docker.service.action:"+id, input.IdempotencyKey, input)
}

func (s *Service) findCapabilityOperation(ctx context.Context, principal domainidentity.Principal, scope, key string, input any) (domaindocker.Operation, error) {
	if err := s.authorize(ctx, principal, appaccess.PermDockerOperationsView); err != nil {
		return domaindocker.Operation{}, err
	}
	claim, err := operationClaimFor(scope, principal, key, input)
	if err != nil {
		return domaindocker.Operation{}, err
	}
	operation, found, err := s.findClaimedOperation(ctx, claim)
	if err != nil {
		return operation, err
	}
	if !found {
		return operation, apperrors.ErrNotFound
	}
	return domaindocker.WithOperationState(operation, time.Now().UTC()), nil
}
