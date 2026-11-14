package docker

import (
	"context"
	"fmt"
	"slices"
	"time"

	dockerruntime "github.com/opensoha/soha/contracts/docker/runtime"
	domain "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type identityOperationClaimer interface {
	ClaimIdentityOperation(context.Context, string, string, []string, []string, string, time.Time) (domain.Operation, error)
}

func validateRuntimeClaimProtocols(protocols []string) error {
	if dockerruntime.ValidateRuntimeProtocols(protocols) != nil {
		return fmt.Errorf("%w: Docker runtime protocols are invalid", apperrors.ErrInvalidArgument)
	}
	return nil
}

type runtimeIdentityTransaction interface {
	WithinRuntimeIdentity(context.Context, string, string, string, string, func(context.Context) error) error
}

func (s *Service) applyIdentityCallback(ctx context.Context, item domain.Operation, status string, payload map[string]any) error {
	value, present := item.Payload["runtimeBinding"]
	if !present {
		s.applyCallbackRuntimeStateUnchecked(ctx, item, status, payload)
		return nil
	}
	binding, err := dockerruntime.ProjectBindingFromValue(value)
	if err != nil {
		return apperrors.ErrConflict
	}
	repository, ok := s.repo.(runtimeIdentityTransaction)
	if !ok {
		return apperrors.ErrUnsupportedOperation
	}
	return repository.WithinRuntimeIdentity(ctx, item.HostID, item.ProjectID, binding.HostUID, binding.ProjectUID, func(txCtx context.Context) error {
		s.applyCallbackRuntimeStateUnchecked(txCtx, item, status, payload)
		return s.reconcileBoundInventory(txCtx, item, status, payload, binding)
	})
}

func validateFrozenRuntimeIdentity(project domain.Project, host domain.Host, frozen *dockerruntime.ProjectBinding) error {
	if project.RuntimeIdentityVersion == 0 && frozen == nil {
		return nil
	}
	if project.RuntimeIdentityVersion != 1 || frozen == nil {
		return apperrors.ErrConflict
	}
	current, err := dockerruntime.NewProjectBinding(host.ResourceUID, project.ResourceUID)
	if err != nil || frozen.Validate() != nil || *frozen != current {
		return apperrors.ErrConflict
	}
	return nil
}

func (s *Service) bindProjectRuntimePayload(ctx context.Context, hostID, projectID string, payload map[string]any) (map[string]any, error) {
	if projectID == "" {
		return payload, nil
	}
	project, err := s.repo.GetProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if project.RuntimeIdentityVersion == 0 {
		return payload, nil
	}
	host, err := s.repo.GetHost(ctx, hostID)
	if err != nil {
		return nil, err
	}
	bound, err := domain.BindProjectRuntimePayload(project, host, payload)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", apperrors.ErrInvalidArgument, err)
	}
	return bound, nil
}

func (s *Service) checkRuntimeOperationIdentity(ctx context.Context, item domain.Operation) error {
	value, present := item.Payload["runtimeBinding"]
	if !present {
		return nil
	}
	binding, err := dockerruntime.ProjectBindingFromValue(value)
	if err != nil {
		return apperrors.ErrConflict
	}
	project, err := s.repo.GetProject(ctx, item.ProjectID)
	if err != nil {
		return err
	}
	host, err := s.repo.GetHost(ctx, item.HostID)
	if err != nil {
		return err
	}
	if project.RuntimeIdentityVersion != 1 || project.HostID != item.HostID || project.ResourceUID != binding.ProjectUID || host.ResourceUID != binding.HostUID {
		return apperrors.ErrConflict
	}
	if err := s.checkOriginalRuntimeContainers(ctx, item, binding); err != nil {
		return err
	}
	return s.checkRuntimeContainerOperation(ctx, item, binding)
}

func (s *Service) checkRuntimeContainerOperation(ctx context.Context, item domain.Operation, project dockerruntime.ProjectBinding) error {
	if item.OperationKind != OperationKindServiceAction {
		return nil
	}
	target, err := dockerruntime.ContainerBindingFromValue(item.Payload["runtimeContainer"])
	if err != nil || target.ProjectBinding != project {
		return apperrors.ErrConflict
	}
	service, err := s.repo.GetService(ctx, item.ServiceID)
	if err != nil {
		return err
	}
	if service.ProjectID != item.ProjectID || service.HostID != item.HostID || service.Name != target.ServiceName || service.ContainerID != target.ContainerID {
		return apperrors.ErrConflict
	}
	return nil
}

func (s *Service) claimRuntimeOperation(ctx context.Context, input domain.OperationClaimInput, workerID string, kinds []string, token string) (domain.Operation, error) {
	if token != "" && slices.Contains(input.RuntimeProtocols, dockerruntime.IdentityProtocol) {
		if repository, ok := s.repo.(identityOperationClaimer); ok {
			return repository.ClaimIdentityOperation(ctx, workerID, input.AgentID, input.HostIDs, kinds, token, time.Now().UTC())
		}
	}
	return s.repo.ClaimOperation(ctx, workerID, input.AgentID, input.HostIDs, kinds, token, time.Now().UTC())
}

func (s *Service) RecordOperationCallback(ctx context.Context, input domain.OperationCallbackInput) (_ domain.Operation, retErr error) {
	principal := runnerPrincipal(input.WorkerID)
	defer func() {
		s.recordMutationFailure(ctx, principal, "docker.operation.callback", input.OperationID, input.OperationID, retErr, map[string]any{"callbackStatus": input.Status})
	}()
	item, err := s.repo.GetOperation(ctx, input.OperationID)
	if err != nil {
		return domain.Operation{}, err
	}
	if operationTerminal(item.Status) {
		return s.recordOperationCallback(ctx, input)
	}
	value, present := item.Payload["runtimeBinding"]
	if !present {
		return s.recordOperationCallback(ctx, input)
	}
	binding, err := dockerruntime.ProjectBindingFromValue(value)
	if err != nil {
		return domain.Operation{}, apperrors.ErrConflict
	}
	repository, ok := s.repo.(runtimeIdentityTransaction)
	if !ok {
		return domain.Operation{}, apperrors.ErrUnsupportedOperation
	}
	var result domain.Operation
	err = repository.WithinRuntimeIdentity(ctx, item.HostID, item.ProjectID, binding.HostUID, binding.ProjectUID, func(txCtx context.Context) error {
		var callbackErr error
		result, callbackErr = s.recordOperationCallback(txCtx, input)
		return callbackErr
	})
	return result, err
}

func (s *Service) prepareRuntimeClaimInput(ctx context.Context, input domain.OperationClaimInput) (domain.OperationClaimInput, error) {
	if err := validateRuntimeClaimProtocols(input.RuntimeProtocols); err != nil {
		return domain.OperationClaimInput{}, err
	}
	if input.Authorization.HostBound() {
		if !runnerIdentityMatches(input.WorkerID, input.Authorization.HostID) || !runnerIdentityMatches(input.AgentID, input.Authorization.AgentID) || !runnerHostsMatch(input.HostIDs, input.Authorization.HostID) {
			return domain.OperationClaimInput{}, fmt.Errorf("%w: Docker runner credential is bound to another host", apperrors.ErrAccessDenied)
		}
		input.WorkerID = input.Authorization.HostID
		input.AgentID = input.Authorization.AgentID
		input.HostIDs = []string{input.Authorization.HostID}
		if err := s.repo.HeartbeatHost(ctx, input.Authorization.HostID, input.Authorization.AgentID); err != nil {
			return domain.OperationClaimInput{}, err
		}
	}
	return input, nil
}
