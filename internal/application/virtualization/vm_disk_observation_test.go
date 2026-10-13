package virtualization

import (
	"context"
	"errors"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestVMRootResizeUsesRootRatherThanTotal(t *testing.T) {
	for _, test := range []struct {
		name    string
		config  map[string]any
		size    int
		invalid bool
	}{
		{"grow root below total", map[string]any{"diskGiB": 48, "diskCount": 2, "rootDiskGiB": "40"}, 41, false},
		{"shrink root", map[string]any{"diskGiB": 48, "diskCount": 2, "rootDiskGiB": 40}, 39, true},
		{"legacy single disk", map[string]any{"diskGiB": 40}, 39, true},
		{"legacy ambiguous total", map[string]any{"diskGiB": 48, "diskCount": 2}, 50, true},
		{"legacy string count", map[string]any{"diskGiB": 48, "diskCount": "2"}, 50, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := newMemoryRepo()
			connection := repo.addConnection(domain.Connection{Provider: ProviderPVE, Enabled: true})
			vm := domain.VM{ID: "vm", Provider: ProviderPVE, ConnectionID: connection.ID, Config: test.config}
			repo.vms[vm.ID] = vm
			_, err := newTestService(repo, &captureOperations{}, fakeAdapter{}).VMAction(t.Context(), testPrincipal(), vm.ID, VMActionInput{Action: "resize", DiskGiB: test.size})
			if (err != nil) != test.invalid || (test.invalid && !errors.Is(err, apperrors.ErrInvalidArgument)) {
				t.Fatalf("root resize validation = %v", err)
			}
			if test.invalid && len(repo.tasks) != 0 {
				t.Fatal("invalid resize reached the queue")
			}
		})
	}
}

type diskObservationAdapter struct {
	fakeAdapter
	readErr error
}

func (diskObservationAdapter) ResizeVM(context.Context, domain.AdapterConnection, domain.AdapterVM, domain.AdapterResizeVMInput) (domain.PowerActionResult, error) {
	return domain.PowerActionResult{Accepted: true}, nil
}

func (a diskObservationAdapter) ListVMDevices(context.Context, domain.AdapterConnection, domain.AdapterVM) ([]domain.VMDevice, error) {
	if a.readErr != nil {
		return nil, a.readErr
	}
	return []domain.VMDevice{{ID: "scsi0", Kind: "disk", SizeGiB: 40}, {ID: "scsi1", Kind: "disk", SizeGiB: 8}, {ID: "net0", Kind: "network"}}, nil
}

func TestFailedDiskObservationKeepsLastInventory(t *testing.T) {
	repo := newMemoryRepo()
	vm := domain.VM{ID: "vm", Provider: ProviderPVE, Config: map[string]any{"diskGiB": 48, "diskCount": 2, "rootDiskGiB": 40}}
	repo.vms[vm.ID] = vm
	task := domain.Task{ID: "task", VMID: vm.ID, TaskKind: TaskKindVMAction, Status: TaskStatusRunning, Payload: map[string]any{"diskGiB": 41}}
	_, _ = repo.CreateTask(t.Context(), task)
	adapter := diskObservationAdapter{readErr: errors.New("inventory unavailable")}
	service := newTestService(repo, &captureOperations{}, adapter)
	if _, err := service.executeVMResize(t.Context(), task, vm, adapter, domain.AdapterConnection{}); err != nil {
		t.Fatal(err)
	}
	if repo.vms[vm.ID].Config["diskGiB"] != 48 || repo.vms[vm.ID].Config["rootDiskGiB"] != 40 || len(repo.logs[task.ID]) != 2 || repo.logs[task.ID][0].LogLevel != "warn" {
		t.Fatal("failed observation fabricated an inventory update or lost its warning")
	}
	if applyPVEVMDeviceObservation(vm.Config, []domain.VMDevice{{ID: "scsi0", Kind: "disk", SizeGiB: 41}, {ID: "scsi1", Kind: "disk"}}) || vm.Config["rootDiskGiB"] != 40 {
		t.Fatal("incomplete observation partially updated the inventory")
	}
}

func TestVMResizePersistsObservedAdditionalDisk(t *testing.T) {
	repo := newMemoryRepo()
	vm := domain.VM{ID: "vm", Provider: ProviderPVE, ExternalID: "51049", NodeName: "pve-a", Config: map[string]any{"diskGiB": 40, "diskCount": 1, "cpu": 2}}
	repo.vms[vm.ID] = vm
	task := domain.Task{ID: "task", VMID: vm.ID, TaskKind: TaskKindVMAction, Status: TaskStatusRunning, Payload: map[string]any{"disks": []domain.AdapterDiskChange{{ID: "scsi1", SizeGiB: 8, Add: true}}}}
	_, _ = repo.CreateTask(t.Context(), task)
	service := newTestService(repo, &captureOperations{}, diskObservationAdapter{})
	_, err := service.executeVMResize(t.Context(), task, vm, diskObservationAdapter{}, domain.AdapterConnection{})
	if err != nil {
		t.Fatal(err)
	}
	stored := repo.vms[vm.ID]
	if stored.Config["diskGiB"] != 48 || stored.Config["diskCount"] != 2 || stored.Config["rootDiskGiB"] != 40 || stored.Config["cpu"] != 2 || repo.tasks[task.ID].Status != TaskStatusSucceeded {
		t.Fatalf("successful action left stale disk inventory: %#v", stored.Config)
	}
}
