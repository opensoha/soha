package virtualization

import (
	"errors"
	"testing"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestTwoGateAdmission(t *testing.T) {
	snapshot := domain.CapacitySnapshot{Policy: domain.CapacityPolicy{Revision: 1, Mode: "enforced", CPU: domain.OvercommitPolicy{Enabled: true, Percent: 300}, Memory: domain.OvercommitPolicy{Enabled: true, Percent: 120}}, Nodes: []domain.CapacityNode{{Name: "node", CPU: 40, MemoryMiB: 32768, LogicalCPU: 72, TotalMemoryMiB: 378 * 1024, HostReserveMiB: 19 * 1024, ConfiguredCPU: 174, ConfiguredMemoryMiB: 388 * 1024, BudgetObserved: true}}, Storage: []domain.CapacityStorage{{Key: "disk", Name: "disk", Nodes: []string{"node"}, AvailableGiB: 100}}}
	demand := domain.CapacityDemand{CPU: 2, MemoryMiB: 4608, DiskGiB: 40}
	if _, _, err := selectCapacity(snapshot, demand, nil); err != nil {
		t.Fatal(err)
	}
	snapshot.Policy.Memory = domain.OvercommitPolicy{Percent: 100}
	if _, _, err := selectCapacity(snapshot, demand, nil); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("overcommitted configuration passed memory budget: %v", err)
	}
	snapshot.Policy.Memory = domain.OvercommitPolicy{Enabled: true, Percent: 150}
	snapshot.Nodes[0].MemoryMiB = 2048
	if _, _, err := selectCapacity(snapshot, demand, nil); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("memory multiplier expanded actual free memory: %v", err)
	}
	demand = domain.CapacityDemand{DiskGiB: 40, StorageOnly: true}
	if _, _, err := selectCapacity(snapshot, demand, nil); err != nil {
		t.Fatalf("create-only unexpectedly reserved runtime capacity: %v", err)
	}
	demand = domain.CapacityDemand{CPU: 0, MemoryMiB: 0}
	snapshot.Nodes[0].ConfiguredCPU = 1000
	if _, _, err := selectCapacity(snapshot, demand, nil); err != nil {
		t.Fatalf("non-increasing maintenance blocked on existing overcommit: %v", err)
	}
}

func TestBudgetAndRuntimePendingAreIndependent(t *testing.T) {
	snapshot := domain.CapacitySnapshot{Policy: domain.CapacityPolicy{Revision: 1, Mode: "enforced", CPU: domain.OvercommitPolicy{Percent: 100}, Memory: domain.OvercommitPolicy{Percent: 100}}, Nodes: []domain.CapacityNode{{Name: "n", CPU: 8, MemoryMiB: 8192, LogicalCPU: 8, TotalMemoryMiB: 8192, ConfiguredCPU: 4, ConfiguredMemoryMiB: 4096, BudgetObserved: true}}}
	demand := domain.CapacityDemand{CPU: 4, MemoryMiB: 4096}
	pending := []pendingCapacity{{NodeName: "n", CPU: 4, MemoryMiB: 4096, BudgetCPU: 0, BudgetMemoryMiB: 0}}
	if _, _, err := selectCapacity(snapshot, demand, pending); err != nil {
		t.Fatal("observed configuration counted twice:", err)
	}
	pending[0].BudgetCPU = 4
	if _, _, err := selectCapacity(snapshot, demand, pending); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("unobserved budget reservation ignored:", err)
	}
	checks := capacityDiagnostics(snapshot, demand, pending)[0].Checks
	if checks[0].Pending != 4 || checks[2].Pending != 4 || checks[0].Status != "unavailable" {
		t.Fatalf("diagnostics mismatch: %+v", checks)
	}
}
