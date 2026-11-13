package environment

import (
	"errors"
	"testing"
	"time"
)

func TestManagerLeaseLifecycleAndGC(t *testing.T) {
	now := time.Date(2026, 7, 14, 8, 0, 0, 0, time.UTC)
	manager := NewManager()
	lease, err := manager.Acquire(Lease{
		ID: "lease-1", RunID: "run-1", OwnerID: "runner-1", ScopeHash: "sha256:scope",
		Mode: "observe-only", ExpiresAt: now.Add(time.Minute),
		Quota: Quota{DiskBytes: 1024, MemoryBytes: 2048, CPUUnits: 100, MaxProcesses: 2},
	}, now)
	if err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if lease.IssuedAt != now {
		t.Fatalf("IssuedAt = %v, want %v", lease.IssuedAt, now)
	}
	snapshot, err := manager.Snapshot(lease.ID, ResourceUsage{DiskBytes: 1025}, []string{"artifact:b", "artifact:a", "artifact:a"}, now)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if snapshot.State != "quota_exceeded" || len(snapshot.ArtifactRefs) != 2 || snapshot.ArtifactRefs[0] != "artifact:a" {
		t.Fatalf("Snapshot() = %#v", snapshot)
	}
	if got := manager.GCExpired(now.Add(2 * time.Minute)); len(got) != 1 || got[0].ID != lease.ID {
		t.Fatalf("GCExpired() = %#v", got)
	}
}

func TestManagerRejectsUnboundedOrExpiredLease(t *testing.T) {
	now := time.Now().UTC()
	manager := NewManager()
	_, err := manager.Acquire(Lease{ID: "lease", RunID: "run", OwnerID: "owner", ScopeHash: "hash", Mode: "observe-only", ExpiresAt: now}, now)
	if !errors.Is(err, ErrLeaseExpired) {
		t.Fatalf("Acquire() error = %v, want ErrLeaseExpired", err)
	}
}
