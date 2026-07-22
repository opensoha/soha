package runtimeinfo

import (
	"testing"

	"github.com/opensoha/soha/internal/platform/runtimeobs"
)

type fixedMetrics struct {
	snapshot runtimeobs.Snapshot
}

func (m fixedMetrics) Snapshot() runtimeobs.Snapshot { return m.snapshot }

func TestSnapshotIncludesProcessAndServiceMetrics(t *testing.T) {
	collector := NewCollector(fixedMetrics{snapshot: runtimeobs.Snapshot{
		ClusterSync:       runtimeobs.ComponentSnapshot{Started: 3, Succeeded: 2, Failed: 1, QueueDepth: 4},
		WorkflowRunner:    runtimeobs.ComponentSnapshot{Started: 2, Succeeded: 1, Canceled: 1, QueueDepth: 2},
		DirectorySync:     runtimeobs.ComponentSnapshot{Started: 1, Succeeded: 1},
		CopilotInspection: runtimeobs.ComponentSnapshot{},
	}})

	snapshot := collector.Snapshot()
	if snapshot.GeneratedAt.IsZero() || snapshot.CPU.LogicalCores < 1 {
		t.Fatalf("invalid process snapshot: %#v", snapshot)
	}
	if snapshot.Memory.HeapSysBytes < snapshot.Memory.HeapAllocBytes || snapshot.Memory.GoReservedBytes == 0 {
		t.Fatalf("invalid memory snapshot: %#v", snapshot.Memory)
	}
	if snapshot.Services.Started != 6 || snapshot.Services.Succeeded != 4 || snapshot.Services.Failed != 1 || snapshot.Services.Canceled != 1 || snapshot.Services.QueueDepth != 6 {
		t.Fatalf("invalid service aggregate: %#v", snapshot.Services)
	}
}

func TestNormalizedCPUPercentUsesWholeMachineCapacity(t *testing.T) {
	value := normalizedCPUPercent(2, 1)
	if value <= 0 || value > 100 {
		t.Fatalf("normalized CPU percent = %f", value)
	}
	if normalizedCPUPercent(0, 1) != 0 || normalizedCPUPercent(1, 0) != 0 {
		t.Fatal("invalid CPU intervals should return zero")
	}
}
