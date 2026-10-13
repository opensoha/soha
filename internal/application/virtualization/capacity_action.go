package virtualization

import (
	"context"
	"fmt"
	"strings"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) admitVMAction(ctx context.Context, task domain.Task, retry bool) (domain.Task, error) {
	action := payloadString(task.Payload, "action")
	if action != "start" && action != "restart" && action != "resize" {
		if retry {
			return s.tasks.UpdateTask(ctx, task)
		}
		return s.tasks.CreateTask(ctx, task)
	}
	vm, err := s.vms.GetVM(ctx, task.VMID)
	if err != nil {
		return domain.Task{}, err
	}
	connection, err := s.connections.GetConnection(ctx, task.ConnectionID)
	if err != nil {
		return domain.Task{}, err
	}
	if connection.Provider != ProviderPVE {
		return domain.Task{}, fmt.Errorf("%w: action capacity admission is not available for this provider", apperrors.ErrUnsupportedOperation)
	}
	snapshot, err := s.observeCapacity(ctx, connection, domain.AdapterCreateVMInput{Node: vm.NodeName})
	if err != nil {
		return domain.Task{}, err
	}
	demand, err := capacityActionDemand(snapshot, vm, task)
	if err != nil {
		return domain.Task{}, err
	}
	for _, allocation := range snapshot.VMs {
		if allocation.ProviderVMID == vm.ExternalID && allocation.Node == vm.NodeName && allocation.Name == vm.Name {
			task.Payload = cloneMap(task.Payload)
			task.Payload["capacityTarget"] = allocation
		}
	}
	if retry && task.Status == TaskStatusRunning {
		return s.tasks.RecheckTaskWithCapacity(ctx, task, snapshot, demand)
	}
	if retry {
		return s.tasks.RetryTaskWithCapacity(ctx, task, snapshot, demand)
	}
	return s.tasks.CreateTaskWithCapacity(ctx, task, snapshot, demand)
}

func capacityActionDemand(snapshot domain.CapacitySnapshot, vm domain.VM, task domain.Task) (domain.CapacityDemand, error) {
	for _, observed := range snapshot.VMs {
		if observed.ProviderVMID != vm.ExternalID || observed.Node != vm.NodeName || observed.Name != vm.Name {
			continue
		}
		var previous domain.CapacityVMAllocation
		decodePayloadValue(task.Payload["capacityTarget"], &previous)
		if previous.ProviderVMID != "" && previous.Generation != observed.Generation {
			return domain.CapacityDemand{}, fmt.Errorf("%w: provider VM generation changed while queued", apperrors.ErrConflict)
		}
		return observedActionDemand(snapshot, observed, task)

	}
	return domain.CapacityDemand{}, domain.ErrCapacityUnknown
}

func (s *Service) recheckVMActionCapacity(ctx context.Context, task domain.Task, connection domain.Connection) (domain.Task, error) {
	if boolValue(task.Payload, "providerDispatchStarted") {
		return task, nil
	}
	policy, err := s.capacityPolicy(ctx, connection)
	if err != nil {
		return task, err
	}
	if policy.Mode != "enforced" && !boolValue(task.Payload, "requireCapacity") {
		return task, nil
	}
	action := payloadString(task.Payload, "action")
	if action != "start" && action != "restart" && action != "resize" {
		return task, nil
	}
	task.Payload = cloneMap(task.Payload)
	task.Payload["requireCapacity"] = true
	rechecked, err := s.admitVMAction(ctx, task, true)
	if err != nil {
		return task, err
	}
	return rechecked, nil
}

func observedActionDemand(snapshot domain.CapacitySnapshot, observed domain.CapacityVMAllocation, task domain.Task) (domain.CapacityDemand, error) {
	resident := observed.Status == "running" || observed.Status == "paused"
	if observed.Status != "running" && observed.Status != "paused" && observed.Status != "stopped" {
		return domain.CapacityDemand{}, domain.ErrCapacityUnknown
	}
	demand := domain.CapacityDemand{Node: observed.Node}
	switch payloadString(task.Payload, "action") {
	case "start":
		if !resident {
			demand.CPU, demand.MemoryMiB = observed.CPU, observed.MemoryMiB+snapshot.HypervisorOverheadMiB
		}
	case "resize":
		if resident {
			cpu, memory := int64(payloadInt(task.Payload, "cpu")), int64(payloadInt(task.Payload, "memoryMiB"))
			if cpu > 0 {
				demand.CPU = max(int64(0), cpu-observed.CPU)
			}
			if memory > 0 {
				demand.MemoryMiB = max(int64(0), memory-observed.MemoryMiB)
			}
		}
		if disk := int64(payloadInt(task.Payload, "diskGiB")); disk > 0 {
			if observed.RootDiskGiB <= 0 || strings.TrimSpace(observed.RootStorage) == "" {
				return demand, domain.ErrCapacityUnknown
			}
			if disk < observed.RootDiskGiB {
				return demand, fmt.Errorf("%w: disks cannot be reduced", apperrors.ErrInvalidArgument)
			}
			demand.Storage, demand.DiskGiB = observed.RootStorage, max(int64(0), disk-observed.RootDiskGiB)
		}
		return additionalDiskDemand(observed, adapterCreateVMInput(task.Payload).Disks, demand)
	}
	return demand, nil
}

func additionalDiskDemand(observed domain.CapacityVMAllocation, changes []domain.AdapterDiskChange, demand domain.CapacityDemand) (domain.CapacityDemand, error) {
	seen := map[string]bool{}
	serials := map[string]bool{}
	for _, change := range changes {
		if !validNewDiskAdmission(change) || seen[change.ID] || serials[change.Serial] {
			return demand, fmt.Errorf("%w: disk admission requires distinct fixed slots, storage, serials and positive bounded new disks", apperrors.ErrInvalidArgument)
		}
		seen[change.ID], serials[change.Serial] = true, true
		alreadyObserved := false
		for _, disk := range observed.Disks {
			if disk.ID == change.ID {
				if !disk.MatchesNewDisk(change) {
					return demand, fmt.Errorf("%w: new disk slot is occupied by another allocation", apperrors.ErrConflict)
				}
				alreadyObserved = true
			} else if disk.Serial == change.Serial {
				return demand, fmt.Errorf("%w: disk serial belongs to another slot", apperrors.ErrConflict)
			}
		}
		if alreadyObserved {
			continue
		}
		// The current reservation ledger owns one pool per operation.
		if demand.DiskGiB > 0 && demand.Storage != change.Storage {
			return demand, fmt.Errorf("%w: multi-pool changes require separate operations", apperrors.ErrUnsupportedOperation)
		}
		demand.Storage, demand.DiskGiB = change.Storage, demand.DiskGiB+int64(change.SizeGiB)
	}
	return demand, nil
}

func validNewDiskAdmission(change domain.AdapterDiskChange) bool {
	return change.Add && change.ID != "" && change.Storage != "" && change.Serial != "" && change.ValidSerial() && change.SizeGiB > 0 && int64(change.SizeGiB) <= 1<<40
}
