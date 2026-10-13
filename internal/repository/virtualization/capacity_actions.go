package virtualization

import (
	"encoding/json"
	"time"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"gorm.io/gorm"
)

func capacityActionsObserved(tx *gorm.DB, snapshot domain.CapacitySnapshot) ([]string, []string, error) {
	budget, runtime := []string{}, []string{}
	var rows []struct {
		ID         string
		Payload    []byte
		FinishedAt time.Time
	}
	if err := tx.Raw(`SELECT t.id,t.payload,t.finished_at FROM virtualization_tasks t JOIN virtualization_capacity_reservations r ON r.task_id=t.id WHERE r.source_id=? AND r.state='reserved' AND t.task_kind='vm_action' AND t.status='completed' AND t.result->>'accepted'='true' AND t.finished_at<=?`, snapshot.SourceID, snapshot.ObservedAt).Scan(&rows).Error; err != nil {
		return nil, nil, err
	}
	for _, row := range rows {
		var input struct {
			Action string                      `json:"action"`
			CPU    int64                       `json:"cpu"`
			Memory int64                       `json:"memoryMiB"`
			Disk   int64                       `json:"diskGiB"`
			Disks  []domain.AdapterDiskChange  `json:"disks"`
			Target domain.CapacityVMAllocation `json:"capacityTarget"`
		}
		if json.Unmarshal(row.Payload, &input) != nil {
			continue
		}
		for _, vm := range snapshot.VMs {
			if !capacityActionObserved(vm, input.Target, input.Action, input.CPU, input.Memory, input.Disk, input.Disks) {
				continue
			}
			budget = append(budget, row.ID)
			if (vm.RuntimeObserved || vm.Status == "stopped") && !row.FinishedAt.Add(30*time.Second).After(snapshot.ObservedAt) {
				runtime = append(runtime, row.ID)
			}
		}
	}
	return budget, runtime, nil
}

func capacityActionObserved(vm, target domain.CapacityVMAllocation, action string, cpu, memory, disk int64, changes []domain.AdapterDiskChange) bool {
	if vm.ProviderVMID != target.ProviderVMID || vm.Node != target.Node || vm.Name != target.Name || vm.Generation != target.Generation || vm.Generation == "" || vm.Generation == "/" {
		return false
	}
	if action == "start" || action == "restart" {
		return vm.Status == "running" || vm.Status == "paused" || vm.Status == "stopped"
	}
	return action == "resize" && (cpu <= 0 || vm.CPU == cpu) && (memory <= 0 || vm.MemoryMiB == memory) && (disk <= 0 || vm.RootStorage == target.RootStorage && vm.RootDiskGiB >= disk) && capacityDisksObserved(vm.Disks, changes)
}

func capacityDisksObserved(disks []domain.CapacityDiskAllocation, changes []domain.AdapterDiskChange) bool {
	for _, change := range changes {
		matched := false
		for _, observed := range disks {
			if observed.MatchesNewDisk(change) {
				matched = true
			}
		}
		if !matched {
			return false
		}
	}
	return true
}
