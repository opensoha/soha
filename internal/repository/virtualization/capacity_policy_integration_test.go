package virtualization

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func testCapacityPolicyDefaultAndAlias(t *testing.T, repo *Repository, ctx context.Context) (domain.Connection, domain.Connection, domain.CapacityPolicy) {
	t.Helper()
	id := uuid.NewString()
	connection, err := repo.CreateConnection(ctx, domain.ConnectionInput{Provider: "pve", Name: "task51-policy", Endpoint: "https://" + id + ".example"})
	if err != nil {
		t.Fatal(err)
	}
	policy, err := repo.GetCapacityPolicy(ctx, connection.ID, "legacy")
	if err != nil || policy.Mode != "enforced" || policy.SourceConfirmed || policy.Revision != 1 || policy.CPU.Percent != 100 || policy.Memory.Percent != 100 {
		t.Fatalf("strict new source default: %+v %v", policy, err)
	}
	physical := "pve/task51-" + id
	confirmed := domain.CapacityPolicy{SourceID: physical, Mode: "enforced", CPU: domain.OvercommitPolicy{Percent: 100}, Memory: domain.OvercommitPolicy{Percent: 100}, UpdatedBy: "test"}
	saved, err := repo.SaveCapacityPolicy(ctx, connection.ID, policy.SourceID, confirmed, 1)
	if err != nil || !saved.SourceConfirmed || saved.SourceID != physical {
		t.Fatalf("confirm source: %+v %v", saved, err)
	}
	if _, err := repo.UpdateConnection(ctx, connection.ID, domain.ConnectionInput{Provider: "pve", Name: connection.Name, Endpoint: "https://different-source.example"}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("confirmed source endpoint changed", err)
	}
	alias, err := repo.CreateConnection(ctx, domain.ConnectionInput{Provider: "pve", Name: "task51-alias", Endpoint: "https://alias-" + id + ".example"})
	if err != nil {
		t.Fatal(err)
	}
	aliasPolicy, err := repo.GetCapacityPolicy(ctx, alias.ID, "legacy")
	if err != nil {
		t.Fatal(err)
	}
	joined, err := repo.SaveCapacityPolicy(ctx, alias.ID, aliasPolicy.SourceID, confirmed, 1)
	if err != nil || joined.SourceID != saved.SourceID || joined.Revision != saved.Revision {
		t.Fatalf("alias forked policy: %+v %v", joined, err)
	}
	return connection, alias, saved
}

func testCapacityPolicyWithPostgres(t *testing.T, repo *Repository, ctx context.Context) {
	t.Helper()
	connection, alias, saved := testCapacityPolicyDefaultAndAlias(t, repo, ctx)
	physical, confirmed := saved.SourceID, saved
	snapshot := domain.CapacitySnapshot{ConnectionID: connection.ID, SourceID: physical, Policy: saved, ObservedAt: time.Now(), Complete: true, Nodes: []domain.CapacityNode{{Name: "n", CPU: 8, MemoryMiB: 8192, LogicalCPU: 8, TotalMemoryMiB: 8192, BudgetObserved: true}}, Storage: []domain.CapacityStorage{{Key: "disk", Name: "disk", Nodes: []string{"n"}, AvailableGiB: 40}}}
	task := domain.Task{Provider: "pve", ConnectionID: connection.ID, TaskKind: "vm_create", Status: "queued", Payload: map[string]any{"requireCapacity": true}}
	created, err := repo.CreateTaskWithCapacity(ctx, task, snapshot, domain.CapacityDemand{CPU: 4, MemoryMiB: 4096, DiskGiB: 30})
	if err != nil {
		t.Fatal(err)
	}
	aliasSnapshot := snapshot
	aliasSnapshot.ConnectionID = alias.ID
	if _, _, err := repo.SelectCapacity(ctx, aliasSnapshot, domain.CapacityDemand{CPU: 4, MemoryMiB: 4096, DiskGiB: 30}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("alias ignored pending shared storage", err)
	}
	confirmed.CPU = domain.OvercommitPolicy{Enabled: true, Percent: 200}
	updated, err := repo.SaveCapacityPolicy(ctx, connection.ID, physical, confirmed, saved.Revision)
	if err != nil || updated.Revision != saved.Revision+1 {
		t.Fatalf("revision: %+v %v", updated, err)
	}
	if _, _, err := repo.SelectCapacity(ctx, snapshot, domain.CapacityDemand{}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("stale policy preview passed", err)
	}
	task.ConnectionID = alias.ID
	if _, err := repo.CreateTaskWithCapacity(ctx, task, aliasSnapshot, domain.CapacityDemand{CPU: 1, MemoryMiB: 1024, DiskGiB: 1}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("stale policy admitted", err)
	}
	testCapacityDispatchFence(t, repo, ctx, created, snapshot, updated)
}

func testCapacityDispatchFence(t *testing.T, repo *Repository, ctx context.Context, created domain.Task, snapshot domain.CapacitySnapshot, updated domain.CapacityPolicy) {
	t.Helper()
	current, err := repo.GetTask(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Seed the claimed row; UpdateTask deliberately cannot acquire a worker claim.
	if err := repo.db.Exec(`UPDATE virtualization_tasks SET status='running',claimed_by_worker_id='task51-worker',attempt_count=1,updated_at=now() WHERE id=?`, current.ID).Error; err != nil {
		t.Fatal(err)
	}
	current, err = repo.GetTask(ctx, created.ID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.Policy, snapshot.ObservedAt = updated, time.Now()
	dispatched, err := repo.RecheckTaskWithCapacity(ctx, current, snapshot, domain.CapacityDemand{CPU: 4, MemoryMiB: 4096, DiskGiB: 30})
	if err != nil || dispatched.Payload["providerDispatchStarted"] != true || payloadRevision(dispatched.Payload) != updated.Revision {
		t.Fatalf("dispatch fence: %+v %v", dispatched, err)
	}
	if _, err := repo.RecheckTaskWithCapacity(ctx, current, snapshot, domain.CapacityDemand{CPU: 4, MemoryMiB: 4096, DiskGiB: 30}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("old worker checkpoint reused", err)
	}
}

func payloadRevision(payload map[string]any) int {
	value, _ := payload["capacityPolicyRevision"].(float64)
	return int(value)
}
