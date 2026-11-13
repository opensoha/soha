package docker

import (
	"context"
	"fmt"
	"slices"

	dockerruntime "github.com/opensoha/soha/contracts/docker/runtime"
	domain "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type runtimeInventoryRepository interface {
	ReconcileRuntimeServices(context.Context, string, string, []domain.ServiceInput) error
}

func (s *Service) checkOriginalRuntimeContainers(ctx context.Context, item domain.Operation, binding dockerruntime.ProjectBinding) error {
	frozen, err := dockerruntime.ContainerSetFromValue(item.Payload["runtimeContainers"], binding)
	if err != nil {
		return apperrors.ErrConflict
	}
	services, err := s.repo.ListServices(ctx, domain.ServiceFilter{HostID: item.HostID, ProjectID: item.ProjectID, Limit: 257})
	if err != nil {
		return err
	}
	current, err := domain.RuntimeContainerSnapshot(binding, item.HostID, item.ProjectID, services)
	if err != nil || !dockerruntime.SameContainerSet(frozen, current) {
		return apperrors.ErrConflict
	}
	return nil
}

func (s *Service) reconcileBoundInventory(ctx context.Context, item domain.Operation, status string, payload map[string]any, binding dockerruntime.ProjectBinding) error {
	confirmed, _ := payload["runtimeInventoryConfirmed"].(bool)
	if status != OperationStatusCompleted || stringValue(item.Payload, "action") == "validate" || !confirmed {
		return nil
	}
	services, err := confirmedRuntimeServices(item, payload, binding)
	if err != nil {
		return err
	}
	repository, ok := s.repo.(runtimeInventoryRepository)
	if !ok {
		return apperrors.ErrUnsupportedOperation
	}
	return repository.ReconcileRuntimeServices(ctx, item.HostID, item.ProjectID, services)
}

func confirmedRuntimeServices(item domain.Operation, payload map[string]any, binding dockerruntime.ProjectBinding) ([]domain.ServiceInput, error) {
	records, ok := payload["services"].([]any)
	if !ok || records == nil || len(records) > 256 {
		return nil, fmt.Errorf("%w: Docker confirmed inventory requires a complete bounded array", apperrors.ErrInvalidArgument)
	}
	services := serviceInputsFromPayload(item, payload)
	if len(services) != len(records) {
		return nil, fmt.Errorf("%w: Docker confirmed inventory contains an invalid record", apperrors.ErrInvalidArgument)
	}
	names, ids := map[string]bool{}, map[string]bool{}
	for index, service := range services {
		record, ok := records[index].(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: Docker confirmed inventory contains an invalid record", apperrors.ErrInvalidArgument)
		}
		state, validState := record["status"].(string)
		target := dockerruntime.ContainerBinding{ProjectBinding: binding, ServiceName: service.Name, ContainerID: service.ContainerID}
		if target.Validate() != nil || names[service.Name] || ids[service.ContainerID] || !validState || !slices.Contains([]string{"created", "running", "paused", "restarting", "removing", "exited", "dead"}, state) {
			return nil, fmt.Errorf("%w: Docker confirmed inventory identity is ambiguous", apperrors.ErrInvalidArgument)
		}
		names[service.Name], ids[service.ContainerID] = true, true
	}
	return services, nil
}
