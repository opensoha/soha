package virtualization

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/redaction"
)

func (s *Service) CheckCapacity(ctx context.Context, principal domainidentity.Principal, input sohaapi.VirtualizationCapacityInput) (sohaapi.VirtualizationCapacityResult, error) {
	result := sohaapi.VirtualizationCapacityResult{ConnectionID: strings.TrimSpace(input.ConnectionID), Status: sohaapi.VirtualizationCapacityResultStatusUnknown}
	for _, permission := range []string{appaccess.PermVirtualizationClustersView, appaccess.PermVirtualizationVMsView, appaccess.PermVirtualizationStorageView} {
		if err := s.authorize(ctx, principal, permission); err != nil {
			return result, err
		}
	}
	if result.ConnectionID == "" || input.CPU < 1 || input.CPU > 65536 || input.MemoryMiB < 1 || input.MemoryMiB > 1<<40 || input.DiskGiB < 1 || input.DiskGiB > 1<<40 {
		return result, fmt.Errorf("%w: connection and bounded positive CPU, memory and root disk are required", apperrors.ErrInvalidArgument)
	}
	architecture, err := normalizeArchitecture(input.Architecture)
	if err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	connection, err := s.connections.GetConnection(ctx, result.ConnectionID)
	if err != nil {
		return result, mapNotFound(err)
	}
	if err := domain.CheckScope(ctx, connectionCapabilityScope(connection, strings.TrimSpace(input.Namespace))); err != nil {
		return result, err
	}
	result.Provider = connection.Provider
	if !connection.Enabled {
		result.Reason = "virtualization connection is disabled"
		return result, nil
	}
	adapterInput := domain.AdapterCreateVMInput{Architecture: architecture, Namespace: strings.TrimSpace(input.Namespace), Node: strings.TrimSpace(input.Node), CPU: input.CPU, Memory: memoryString(input.MemoryMiB), DiskSize: diskString(input.DiskGiB), SourceMode: "datasource_clone", StartAfterCreate: true, ProviderParams: map[string]any{"storage": strings.TrimSpace(input.Storage), "storageClass": strings.TrimSpace(input.Storage)}}
	snapshot, err := s.observeCapacity(ctx, connection, adapterInput)
	if err != nil {
		result.Reason = "provider capacity could not be verified; inspect the connection and supported capacity mode"
		return result, nil
	}
	overhead, err := domain.CapacityMemoryOverheadMiB(connection.Config)
	if err != nil {
		return result, fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	node, storage, err := s.tasks.SelectCapacity(ctx, snapshot, domain.CapacityDemand{Node: adapterInput.Node, Storage: strings.TrimSpace(input.Storage), CPU: int64(input.CPU), MemoryMiB: int64(input.MemoryMiB) + overhead, DiskGiB: capacityDiskDemand(connection.Provider, input.DiskGiB)})
	result.Namespace, result.ObservedAt = snapshot.Namespace, &snapshot.ObservedAt
	validUntil := snapshot.ObservedAt.Add(30 * time.Second)
	result.ValidUntil = &validUntil
	if err != nil {
		result.Reason = "capacity inventory or reservation ledger could not be verified"
		if errors.Is(err, apperrors.ErrConflict) {
			result.Status = sohaapi.VirtualizationCapacityResultStatusUnavailable
			result.Reason = redaction.LogText(err.Error(), 512)
		}
		return result, nil
	}
	result.Status, result.Node, result.Storage = sohaapi.VirtualizationCapacityResultStatusAvailable, node.Name, storage.Name
	result.Reason = "placement fits observed unreserved capacity; creation must perform fresh atomic admission"
	return result, nil
}

func (s *Service) observeCapacity(ctx context.Context, connection domain.Connection, input domain.AdapterCreateVMInput) (domain.CapacitySnapshot, error) {
	adapter, providerConnection, err := s.adapterForConnection(ctx, connection)
	if err != nil {
		return domain.CapacitySnapshot{}, err
	}
	provider, ok := adapter.(domain.CapacityProvider)
	if !ok {
		return domain.CapacitySnapshot{}, fmt.Errorf("%w: provider capacity is unavailable", apperrors.ErrConflict)
	}
	return provider.ObserveCapacity(ctx, providerConnection, input)
}

func (s *Service) createTaskWithAdmission(ctx context.Context, task domain.Task) (domain.Task, error) {
	if task.TaskKind != TaskKindVMCreate || !boolValue(task.Payload, "requireCapacity") {
		return s.tasks.CreateTask(ctx, task)
	}
	if task.ID != "" {
		if _, err := s.tasks.GetTask(ctx, task.ID); err == nil {
			return domain.Task{}, apperrors.ErrConflict // Caller validates and returns the immutable idempotent receipt.
		} else if !errors.Is(err, apperrors.ErrNotFound) {
			return domain.Task{}, err
		}
	}
	return s.admitVMTask(ctx, task, false)
}

func (s *Service) retryTaskWithAdmission(ctx context.Context, task domain.Task) (domain.Task, error) {
	if !isWorkerCreation(task) {
		return s.retryVMTaskWithAdmission(ctx, task)
	}
	pool, err := workerPoolFromTask(task)
	if err != nil || s.workerPools == nil {
		return domain.Task{}, apperrors.ErrConflict
	}
	var saved domain.Task
	err = s.workerPools.WithWorkerPoolAdmission(ctx, pool.ID.String(), pool.Revision, task.ID, func(ctx context.Context) error {
		var err error
		saved, err = s.retryVMTaskWithAdmission(ctx, task)
		return err
	})
	return saved, err
}

func (s *Service) retryVMTaskWithAdmission(ctx context.Context, task domain.Task) (domain.Task, error) {
	if task.TaskKind != TaskKindVMCreate || !boolValue(task.Payload, "requireCapacity") || boolValue(task.Payload, "providerDispatchStarted") {
		return s.tasks.UpdateTask(ctx, task)
	}
	return s.admitVMTask(ctx, task, true)
}

func (s *Service) admitVMTask(ctx context.Context, task domain.Task, retry bool) (domain.Task, error) {
	connection, err := s.connections.GetConnection(ctx, task.ConnectionID)
	if err != nil {
		return domain.Task{}, err
	}
	if err := checkVMCreateConnection(task, connection); err != nil {
		return domain.Task{}, err
	}
	input := adapterCreateVMInput(task.Payload)
	if input.CPU <= 0 || payloadInt(task.Payload, "memoryMiB") <= 0 || payloadInt(task.Payload, "diskGiB") <= 0 || len(input.Disks) != 0 {
		return domain.Task{}, fmt.Errorf("%w: capacity admission requires explicit positive CPU, memory and root disk; additional disks need separate storage demand", apperrors.ErrInvalidArgument)
	}
	snapshot, err := s.observeCapacity(ctx, connection, input)
	if err != nil {
		return domain.Task{}, err
	}
	storage := firstNonEmpty(stringValue(input.ProviderParams, "storage"), stringValue(input.ProviderParams, "storageClass"))
	memory := int64(payloadInt(task.Payload, "memoryMiB"))
	overhead, err := domain.CapacityMemoryOverheadMiB(connection.Config)
	if err != nil {
		return domain.Task{}, fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	memory += overhead
	task.Payload["providerConnectionIdentity"] = vmCreateConnectionIdentity(connection)
	demand := domain.CapacityDemand{Node: input.Node, Storage: storage, CPU: int64(input.CPU), MemoryMiB: memory, DiskGiB: capacityDiskDemand(connection.Provider, payloadInt(task.Payload, "diskGiB"))}
	if retry {
		return s.tasks.RetryTaskWithCapacity(ctx, task, snapshot, demand)
	}
	return s.tasks.CreateTaskWithCapacity(ctx, task, snapshot, demand)
}

func capacityDiskDemand(provider string, rootGiB int) int64 {
	demand := int64(rootGiB)
	if provider == ProviderPVE {
		// Conservatively reserve one GiB for the PVE cloud-init volume, whose
		// allocation is separate from the root disk even for a full clone.
		demand++
	}
	return demand
}
