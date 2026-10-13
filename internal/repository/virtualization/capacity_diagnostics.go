package virtualization

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/gorm"
)

type capacityObservationPayload struct {
	Name           string `json:"name"`
	Node           string `json:"node"`
	ProviderParams struct {
		VMID string `json:"vmid"`
	} `json:"providerParams"`
}

func matchesCapacityObservation(vm domain.CapacityVMObservation, operationID string, payload capacityObservationPayload, revision int) bool {
	return vm.OperationID == operationID && vm.ProviderVMID == payload.ProviderParams.VMID && vm.Name == payload.Name && vm.Node == payload.Node && (revision == 0 || strings.HasPrefix(vm.Generation, operationID+"/"))
}

func capacityObserved(tx *gorm.DB, snapshot domain.CapacitySnapshot) ([]string, []string, error) {
	budget, runtime := []string{}, []string{}
	actionBudget, actionRuntime, err := capacityActionsObserved(tx, snapshot)
	if err != nil {
		return nil, nil, err
	}
	budget, runtime = append(budget, actionBudget...), append(runtime, actionRuntime...)
	if len(snapshot.ObservedVMs) == 0 && snapshot.Policy.Revision == 0 {
		return budget, append(runtime, snapshot.ObservedOperations...), nil
	}
	ids := make([]string, 0, len(snapshot.ObservedVMs))
	for _, vm := range snapshot.ObservedVMs {
		ids = append(ids, vm.OperationID)
	}
	if len(ids) == 0 {
		return budget, runtime, nil
	}
	var rows []struct {
		ID         string
		Payload    []byte
		Status     string
		FinishedAt *time.Time
		Result     []byte
	}
	err = tx.Raw(`SELECT t.id,t.payload,t.status,t.finished_at,t.result FROM virtualization_tasks t JOIN virtualization_capacity_reservations r ON r.task_id=t.id WHERE r.source_id=? AND t.id IN ? AND t.payload->>'providerIdentityPrepared'='true' AND t.payload->>'providerDispatchStarted'='true'`, snapshot.SourceID, ids).Scan(&rows).Error
	if err != nil {
		return nil, nil, err
	}
	for _, row := range rows {
		var payload capacityObservationPayload
		var result struct {
			ProviderEffect string `json:"providerEffect"`
		}
		if json.Unmarshal(row.Payload, &payload) != nil || json.Unmarshal(row.Result, &result) != nil {
			continue
		}
		for _, vm := range snapshot.ObservedVMs {
			if !matchesCapacityObservation(vm, row.ID, payload, snapshot.Policy.Revision) {
				continue
			}
			if vm.Resident {
				budget = append(budget, row.ID)
			}
			if (vm.RuntimeObserved || vm.Status == "stopped") && (row.Status == "completed" || row.Status == "canceled") && result.ProviderEffect == "created" && row.FinishedAt != nil && !row.FinishedAt.After(snapshot.ObservedAt) {
				runtime = append(runtime, row.ID)
			}
		}
	}
	return budget, runtime, nil
}

func capacityPending(tx *gorm.DB, snapshot domain.CapacitySnapshot, exclude string) ([]pendingCapacity, error) {
	budget, runtime, err := capacityObserved(tx, snapshot)
	if err != nil {
		return nil, err
	}
	var pending []pendingCapacity
	err = tx.Raw(`SELECT r.node_name,r.namespace,r.storage_key,
        CASE WHEN r.task_id IN ? THEN 0 ELSE r.cpu END AS budget_cpu,
        CASE WHEN r.task_id IN ? THEN 0 ELSE r.memory_mib END AS budget_memory_mib,
        r.cpu,r.memory_mib,r.disk_gib
        FROM virtualization_capacity_reservations r JOIN virtualization_tasks t ON t.id=r.task_id
        WHERE r.source_id=? AND r.state='reserved' AND r.task_id<>?
        AND NOT COALESCE((`+capacityReleaseCondition+`),FALSE)
        AND NOT COALESCE((`+capacityAccountCondition+`),FALSE)`, budget, budget, snapshot.SourceID, exclude, runtime, snapshot.ObservedAt).Scan(&pending).Error
	return pending, err
}

func (r *Repository) DiagnoseCapacity(ctx context.Context, snapshot domain.CapacitySnapshot, demand domain.CapacityDemand) ([]domain.CapacityNodeDiagnostic, error) {
	var diagnostics []domain.CapacityNodeDiagnostic
	err := dbtx.DB(ctx, r.db).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtextextended(?,0))`, "virtualization-capacity/"+snapshot.SourceID).Error; err != nil {
			return err
		}
		if !validCapacitySnapshot(snapshot) {
			return domain.ErrCapacityUnknown
		}
		if err := checkCapacityPolicyRevision(tx, snapshot); err != nil {
			return err
		}
		pending, err := capacityPending(tx, snapshot, "")
		if err != nil {
			return err
		}
		diagnostics = capacityDiagnostics(snapshot, demand, pending)
		return nil
	})
	return diagnostics, err
}

func capacityDiagnostics(snapshot domain.CapacitySnapshot, demand domain.CapacityDemand, pending []pendingCapacity) []domain.CapacityNodeDiagnostic {
	result := []domain.CapacityNodeDiagnostic{}
	for _, node := range snapshot.Nodes {
		if demand.Node != "" && demand.Node != node.Name {
			continue
		}
		cpu, memory, budgetCPU, budgetMemory := int64(0), int64(0), int64(0), int64(0)
		for _, item := range pending {
			if item.NodeName == node.Name {
				cpu += item.CPU
				memory += item.MemoryMiB
				budgetCPU += item.BudgetCPU
				budgetMemory += item.BudgetMemoryMiB
			}
		}
		entry := domain.CapacityNodeDiagnostic{Node: node.Name, Checks: []domain.CapacityCheckDiagnostic{}}
		if !demand.StorageOnly {
			if snapshot.Policy.Revision > 0 {
				for _, check := range []domain.CapacityCheckDiagnostic{domain.CapacityCheck("budget", "cpu", node.LogicalCPU*int64(snapshot.Policy.CPU.Percent)/100, node.ConfiguredCPU, budgetCPU, demand.CPU), domain.CapacityCheck("budget", "memory", max(int64(0), node.TotalMemoryMiB-node.HostReserveMiB)*int64(snapshot.Policy.Memory.Percent)/100, node.ConfiguredMemoryMiB, budgetMemory, demand.MemoryMiB)} {
					if !node.BudgetObserved {
						check.Status, check.Reason = "unknown", "configured capacity was not observed"
					}
					entry.Checks = append(entry.Checks, check)
				}
			}
			entry.Checks = append(entry.Checks, domain.CapacityCheck("runtime", "cpu", node.CPU, 0, cpu, demand.CPU), domain.CapacityCheck("runtime", "memory", node.MemoryMiB, 0, memory, demand.MemoryMiB))
		}
		if demand.DiskGiB == 0 {
			result = append(result, entry)
			continue
		}
		for _, storage := range snapshot.Storage {
			if demand.Storage != "" && demand.Storage != storage.Name || !slices.Contains(storage.Nodes, node.Name) {
				continue
			}
			disk := int64(0)
			for _, item := range pending {
				if item.StorageKey == storage.Key {
					disk += item.DiskGiB
				}
			}
			pool := domain.CapacityNodeDiagnostic{Node: entry.Node, Storage: storage.Name, Checks: append(slices.Clone(entry.Checks), domain.CapacityCheck("storage", "disk", storage.AvailableGiB, 0, disk, demand.DiskGiB))}
			result = append(result, pool)
		}
	}
	return result
}
