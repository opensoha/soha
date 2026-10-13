package virtualization

import (
	"testing"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
)

func TestDataDiskReservationRequiresActualMatchingDisk(t *testing.T) {
	target := domain.CapacityVMAllocation{ProviderVMID: "101", Node: "pve-a", Name: "personal", Generation: "stable/creation", RootStorage: "disk", RootDiskGiB: 40}
	change := domain.AdapterDiskChange{ID: "scsi1", Storage: "disk", SizeGiB: 8, Serial: "personal-data", Add: true}
	check := func(vm domain.CapacityVMAllocation) bool {
		return capacityActionObserved(vm, target, "resize", 0, 0, 0, []domain.AdapterDiskChange{change})
	}
	if check(target) {
		t.Fatal("missing data disk released its reservation")
	}
	current := target
	current.Disks = []domain.CapacityDiskAllocation{{ID: change.ID, Storage: change.Storage, Serial: change.Serial, SizeGiB: 8}}
	if !check(current) {
		t.Fatal("confirmed data disk did not release its reservation")
	}
	for _, edit := range []func(*domain.CapacityVMAllocation){
		func(v *domain.CapacityVMAllocation) { v.Generation = "new/creation" },
		func(v *domain.CapacityVMAllocation) { v.Disks[0].Serial = "other-data" },
		func(v *domain.CapacityVMAllocation) { v.Disks[0].Storage = "other-pool" },
		func(v *domain.CapacityVMAllocation) { v.Disks[0].SizeGiB = 4 },
	} {
		changed := current
		changed.Disks = append([]domain.CapacityDiskAllocation{}, current.Disks...)
		edit(&changed)
		if check(changed) {
			t.Fatal("changed or incomplete data allocation released its reservation")
		}
	}
}
