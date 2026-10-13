package virtualization

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type diskActionAdmissionRepo struct {
	*memoryRepo
	demand   domain.CapacityDemand
	admitted bool
}

func (r *diskActionAdmissionRepo) CreateTaskWithCapacity(ctx context.Context, task domain.Task, _ domain.CapacitySnapshot, demand domain.CapacityDemand) (domain.Task, error) {
	r.demand, r.admitted = demand, true
	return r.CreateTask(ctx, task)
}

func TestVMActionExplicitAdmissionWithUnconfiguredPolicy(t *testing.T) {
	repo := newMemoryRepo()
	connection := repo.addConnection(domain.Connection{Provider: ProviderPVE, Enabled: true})
	vm := domain.VM{ID: "vm", ExternalID: "101", ConnectionID: connection.ID, Name: "worker", Provider: ProviderPVE, NodeName: "pve-a"}
	repo.vms[vm.ID] = vm
	adapter := &capacityReaderAdapter{observed: domain.CapacitySnapshot{Complete: true, ObservedAt: time.Now(), VMs: []domain.CapacityVMAllocation{{ProviderVMID: "101", Name: "worker", Node: "pve-a", Generation: "generation-1", Status: "running"}}}}
	service := newTestService(repo, &captureOperations{}, adapter)
	admission := &diskActionAdmissionRepo{memoryRepo: repo}
	service.tasks = admission
	input := VMActionInput{Action: "resize", RequireCapacity: true, Disks: []domain.AdapterDiskChange{{ID: "scsi1", Storage: "data", SizeGiB: 8, Serial: "personal-data", Add: true}}}
	task, err := service.VMAction(t.Context(), testPrincipal(), vm.ID, input)
	if err != nil || !admission.admitted || admission.demand.DiskGiB != 8 || admission.demand.CPU != 0 || admission.demand.MemoryMiB != 0 || !boolValue(task.Payload, "requireCapacity") {
		t.Fatalf("explicit disk admission bypassed: task=%#v demand=%#v err=%v", task, admission.demand, err)
	}
	adapter.observed.VMs = nil
	if _, err = service.VMAction(t.Context(), testPrincipal(), vm.ID, input); !errors.Is(err, domain.ErrCapacityUnknown) || len(repo.tasks) != 1 {
		t.Fatalf("unknown allocation enqueued another task: %v", err)
	}
}

func TestAdditionalDiskAdmissionRetainsOriginalAllocation(t *testing.T) {
	change := domain.AdapterDiskChange{ID: "scsi1", Storage: "data", SizeGiB: 8, Serial: "personal-data", Add: true}
	observed := domain.CapacityVMAllocation{Status: "running", Node: "pve-a", RootStorage: "root", RootDiskGiB: 40}
	task := domain.Task{Payload: map[string]any{"action": "resize", "disks": []domain.AdapterDiskChange{change}}}
	demand, err := observedActionDemand(domain.CapacitySnapshot{}, observed, task)
	if err != nil || demand.Storage != "data" || demand.DiskGiB != 8 || demand.CPU != 0 || demand.MemoryMiB != 0 {
		t.Fatalf("data-only reservation = %#v, %v", demand, err)
	}
	observed.Disks = []domain.CapacityDiskAllocation{{ID: change.ID, Storage: change.Storage, Serial: change.Serial, SizeGiB: 8}}
	demand, err = observedActionDemand(domain.CapacitySnapshot{}, observed, task)
	if err != nil || demand.DiskGiB != 0 {
		t.Fatalf("matching retry reserved another disk: %#v, %v", demand, err)
	}
	observed.Disks[0].Serial = "another-data"
	if _, err = observedActionDemand(domain.CapacitySnapshot{}, observed, task); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("occupied slot accepted: %v", err)
	}
}

func TestAdditionalDiskAdmissionRejectsAmbiguityAndMultiplePools(t *testing.T) {
	valid := domain.AdapterDiskChange{ID: "scsi1", Storage: "data", SizeGiB: 8, Serial: "personal-data", Add: true}
	for _, edit := range []func(*domain.AdapterDiskChange){
		func(d *domain.AdapterDiskChange) { d.ID = "" },
		func(d *domain.AdapterDiskChange) { d.Storage = "" },
		func(d *domain.AdapterDiskChange) { d.Serial = "" },
		func(d *domain.AdapterDiskChange) { d.Add = false },
		func(d *domain.AdapterDiskChange) { d.SizeGiB = 0 },
		func(d *domain.AdapterDiskChange) { d.Serial = "data,media=cdrom" },
	} {
		change := valid
		edit(&change)
		if _, err := additionalDiskDemand(domain.CapacityVMAllocation{}, []domain.AdapterDiskChange{change}, domain.CapacityDemand{}); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("ambiguous data allocation accepted: %#v, %v", change, err)
		}
	}
	for _, changes := range [][]domain.AdapterDiskChange{{valid, valid}, {valid, {ID: "scsi2", Storage: "data", SizeGiB: 8, Serial: valid.Serial, Add: true}}} {
		if _, err := additionalDiskDemand(domain.CapacityVMAllocation{}, changes, domain.CapacityDemand{}); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("duplicate allocation accepted: %v", err)
		}
	}
	if _, err := additionalDiskDemand(domain.CapacityVMAllocation{}, []domain.AdapterDiskChange{valid}, domain.CapacityDemand{Storage: "root", DiskGiB: 10}); !errors.Is(err, apperrors.ErrUnsupportedOperation) {
		t.Fatalf("multi-pool demand silently merged: %v", err)
	}
	demand, err := additionalDiskDemand(domain.CapacityVMAllocation{}, []domain.AdapterDiskChange{valid}, domain.CapacityDemand{Storage: "data", DiskGiB: 10})
	if err != nil || demand.DiskGiB != 18 {
		t.Fatalf("same-pool demand lost: %#v %v", demand, err)
	}
}
