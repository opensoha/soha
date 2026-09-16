package virtualization

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func testWorkerPoolAdmission(t *testing.T, repo *Repository, ctx context.Context) {
	t.Helper()
	connection, err := repo.CreateConnection(ctx, domain.ConnectionInput{Provider: "pve", Name: "worker-pool-test", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	clusterID, poolID, sourceID := uuid.NewString(), uuid.New(), uuid.NewString()
	ids := make([]string, 9)
	for i := range ids {
		ids[i] = uuid.NewString()
	}
	t.Cleanup(func() {
		_ = repo.db.Exec(`DELETE FROM virtualization_capacity_reservations WHERE source_id=?`, sourceID).Error
		_ = repo.db.Exec(`DELETE FROM virtualization_capacity_observations WHERE source_id=?`, sourceID).Error
		_ = repo.db.Exec(`DELETE FROM virtualization_tasks WHERE id IN ?`, ids).Error
		_ = repo.db.Exec(`DELETE FROM virtualization_worker_pools WHERE id=?`, poolID).Error
		_ = repo.db.Exec(`DELETE FROM virtualization_connections WHERE id=?`, connection.ID).Error
		_ = repo.db.Exec(`DELETE FROM clusters WHERE id=?`, clusterID).Error
	})
	if err := repo.db.Exec(`INSERT INTO clusters(id,name) VALUES(?,?)`, clusterID, "worker-target").Error; err != nil {
		t.Fatal(err)
	}
	pool := domain.WorkerPool{VirtualizationWorkerPool: sohaapi.VirtualizationWorkerPool{ID: poolID, Spec: sohaapi.VirtualizationWorkerPoolSpec{Name: "workers", ConnectionID: connection.ID, ClusterID: clusterID, Owner: "soha-kubeadm", ImageID: "image", ProviderNode: "pve-a", Storage: "disk", Bridge: "vmbr0", SnippetStorage: "local", OsProfile: "ubuntu-24.04-amd64-containerd", KubernetesVersion: "v1.35.2", CPU: 4, MemoryMiB: 8192, DiskGiB: 80, MaxNodes: 2, Enabled: true, RequiredDaemonSets: []sohaapi.VirtualizationWorkerDaemonSet{{Namespace: "kube-system", Name: "network"}}}}}
	pool, err = repo.SaveWorkerPool(ctx, pool, 0)
	if err != nil || pool.Revision != 1 {
		t.Fatalf("pool creation: %+v %v", pool, err)
	}
	if _, err := repo.SaveWorkerPool(ctx, pool, 0); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("duplicate pool create overwrote owner: %v", err)
	}
	if err := repo.DeleteWorkerPool(ctx, poolID.String(), 2); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale deletion accepted: %v", err)
	}
	if err := repo.DeleteWorkerPool(ctx, poolID.String(), 1); err != nil {
		t.Fatalf("unused pool deletion: %v", err)
	}
	if _, err := repo.GetWorkerPool(ctx, poolID.String()); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("deleted configuration still visible: %v", err)
	}
	pool, err = repo.SaveWorkerPool(ctx, pool, 0)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := domain.CapacitySnapshot{SourceID: sourceID, Complete: true, ObservedAt: time.Now(), Nodes: []domain.CapacityNode{{Name: "pve-a", CPU: 64, MemoryMiB: 128000}}, Storage: []domain.CapacityStorage{{Key: "disk", Name: "disk", Nodes: []string{"pve-a"}, AvailableGiB: 1000}}}
	demand := domain.CapacityDemand{CPU: 4, MemoryMiB: 8192, DiskGiB: 80}
	create := func(ctx context.Context, id string) error {
		_, err := repo.CreateTaskWithCapacity(ctx, domain.Task{ID: id, Provider: "pve", ConnectionID: connection.ID, TaskKind: "vm_create", Status: "queued", Payload: map[string]any{"workerPoolId": poolID.String(), "requireCapacity": true, "creationVersion": 2}}, snapshot, demand)
		return err
	}
	first, second := admitWorkersConcurrently(t, ctx, repo, poolID.String(), ids, create)
	verifyUnknownWorkerBudget(t, ctx, repo, poolID.String(), ids[8], first, create)
	if err := repo.db.Exec(`UPDATE virtualization_tasks SET status='failed',result='{"providerEffect":"not_started"}'::jsonb WHERE id=?`, second).Error; err != nil {
		t.Fatal(err)
	}
	injected := errors.New("after task and capacity writes")
	err = repo.WithWorkerPoolAdmission(ctx, poolID.String(), 1, ids[8], func(ctx context.Context) error {
		if err := create(ctx, ids[8]); err != nil {
			return err
		}
		return injected
	})
	if !errors.Is(err, injected) {
		t.Fatalf("did not reach injected rollback: %v", err)
	}
	if _, err := repo.GetTask(ctx, ids[8]); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("VM task survived pool transaction rollback: %v", err)
	}
	var count int64
	if err := repo.db.Raw(`SELECT count(*) FROM virtualization_capacity_reservations WHERE task_id=?`, ids[8]).Scan(&count).Error; err != nil || count != 0 {
		t.Fatalf("capacity survived pool transaction rollback: %d %v", count, err)
	}
	verifyWorkerPoolRevision(t, ctx, repo, pool, ids[8])
}

func admitWorkersConcurrently(t *testing.T, ctx context.Context, repo *Repository, poolID string, ids []string, create func(context.Context, string) error) (string, string) {
	t.Helper()
	var wg sync.WaitGroup
	successes, errs := make(chan string, 8), make(chan error, 8)
	for _, id := range ids[:8] {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			err := repo.WithWorkerPoolAdmission(ctx, poolID, 1, id, func(ctx context.Context) error { return create(ctx, id) })
			if err == nil {
				successes <- id
			} else {
				errs <- err
			}
		}(id)
	}
	wg.Wait()
	close(successes)
	close(errs)
	if len(successes) != 2 || len(errs) != 6 {
		t.Fatalf("pool budget oversold: success=%d errors=%d", len(successes), len(errs))
	}
	for err := range errs {
		if !errors.Is(err, apperrors.ErrConflict) {
			t.Fatal(err)
		}
	}
	return <-successes, <-successes
}

func verifyUnknownWorkerBudget(t *testing.T, ctx context.Context, repo *Repository, poolID, nextID, first string, create func(context.Context, string) error) {
	t.Helper()
	if err := repo.db.Exec(`UPDATE virtualization_tasks SET status='failed',result='{"providerEffect":"unknown"}'::jsonb,payload=payload||'{"providerDispatchStarted":true}'::jsonb WHERE id=?`, first).Error; err != nil {
		t.Fatal(err)
	}
	if err := repo.db.Exec(`UPDATE virtualization_tasks SET claimed_by_worker_id='worker-cleanup',attempt_count=1 WHERE id=?`, first).Error; err != nil {
		t.Fatal(err)
	}
	cleanup, err := repo.GetTask(ctx, first)
	if err != nil {
		t.Fatal(err)
	}
	staleCleanup := cleanup
	cleanup.Result["workerBootstrapRevoked"] = true
	cleaned, err := repo.UpdateTask(ctx, cleanup)
	if err != nil || cleaned.Status != "failed" || cleaned.Result["providerEffect"] != "unknown" || cleaned.Result["workerBootstrapRevoked"] != true {
		t.Fatalf("terminal token cleanup lost its receipt or provider effect: %+v %v", cleaned, err)
	}
	if _, err := repo.UpdateTask(ctx, staleCleanup); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale cleanup bypassed task fence: %v", err)
	}
	if err := repo.WithWorkerPoolAdmission(ctx, poolID, 1, nextID, func(ctx context.Context) error { return create(ctx, nextID) }); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("unknown provider outcome released pool budget: %v", err)
	}
}

func verifyWorkerPoolRevision(t *testing.T, ctx context.Context, repo *Repository, pool domain.WorkerPool, nextID string) {
	t.Helper()
	if err := repo.DeleteWorkerPool(ctx, pool.ID.String(), 1); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("deleted pool with retained operation history: %v", err)
	}
	pool.Spec.Enabled = false
	saved, err := repo.SaveWorkerPool(ctx, pool, 1)
	if err != nil || saved.Revision != 2 {
		t.Fatalf("pool revision update: %+v %v", saved, err)
	}
	if err := repo.WithWorkerPoolAdmission(ctx, pool.ID.String(), 2, nextID, func(context.Context) error { t.Fatal("disabled pool executed"); return nil }); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("disabled pool admitted: %v", err)
	}
	if _, err := repo.SaveWorkerPool(ctx, pool, 1); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale pool update overwrote owner: %v", err)
	}
}
