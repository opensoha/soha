package virtualization

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/infrastructure/config"
	dbstore "github.com/opensoha/soha/internal/infrastructure/db"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"go.uber.org/zap"
)

func TestCapacityAdmissionWithPostgres(t *testing.T) {
	portText := os.Getenv("SOHA_CATALOG_TEST_POSTGRES_PORT")
	if portText == "" {
		t.Skip("set SOHA_CATALOG_TEST_POSTGRES_PORT to an isolated PostgreSQL instance")
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	store, err := dbstore.New(config.DatabaseConfig{Driver: "postgres", Host: "127.0.0.1", Port: port, Name: "soha", User: "pgsql", Password: "test-only", SSLMode: "disable", MaxOpenConns: 8, MaxIdleConns: 4}, zap.NewNop())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := store.MigrateFromFile(ctx, filepath.Join("..", "..", "..", "migrations", "postgres")); err != nil {
		t.Fatal(err)
	}
	repo := New(store.DB())
	source := "capacity-test/" + uuid.NewString()
	snapshot := domain.CapacitySnapshot{SourceID: source, ObservedAt: time.Now(), Complete: true, Nodes: []domain.CapacityNode{{Name: "a", CPU: 8, MemoryMiB: 16000}, {Name: "b", CPU: 8, MemoryMiB: 16000}}, Storage: []domain.CapacityStorage{{Key: "shared", Name: "disk", Nodes: []string{"a", "b"}, AvailableGiB: 40}}}
	demand := domain.CapacityDemand{CPU: 2, MemoryMiB: 2048, DiskGiB: 40}
	ids := make([]string, 8)
	for index := range ids {
		ids[index] = uuid.NewString()
	}
	t.Cleanup(func() {
		_ = store.DB().Exec(`DELETE FROM virtualization_capacity_reservations WHERE source_id=?`, source).Error
		_ = store.DB().Exec(`DELETE FROM virtualization_capacity_observations WHERE source_id=?`, source).Error
		_ = store.DB().Exec(`DELETE FROM virtualization_tasks WHERE id IN ?`, ids).Error
	})
	// Capacity discovery must not create a watermark or a reservation.
	if node, _, err := repo.SelectCapacity(ctx, snapshot, demand); err != nil || node.Name != "a" {
		t.Fatalf("read capacity: %+v %v", node, err)
	}
	var readWrites int64
	if err := store.DB().Raw(`SELECT (SELECT count(*) FROM virtualization_capacity_observations WHERE source_id=?) + (SELECT count(*) FROM virtualization_capacity_reservations WHERE source_id=?)`, source, source).Scan(&readWrites).Error; err != nil || readWrites != 0 {
		t.Fatalf("read wrote ledger: %d %v", readWrites, err)
	}
	staleRead := snapshot
	staleRead.ObservedAt = time.Now().Add(-time.Minute)
	if _, _, err := repo.SelectCapacity(ctx, staleRead, demand); !errors.Is(err, domain.ErrCapacityUnknown) {
		t.Fatalf("stale read: %v", err)
	}
	winner := admitCapacityConcurrently(t, ctx, store, repo, snapshot, demand, ids)
	loserID, snapshot := verifyCapacityObservation(t, ctx, store, repo, snapshot, demand, ids, winner)
	verifyCapacityRetry(t, ctx, store, repo, snapshot, demand, ids, winner.ID, loserID)
	t.Run("provider identity claim", func(t *testing.T) { testProviderIdentityClaims(t, repo, ctx) })
	t.Run("worker pool admission", func(t *testing.T) { testWorkerPoolAdmission(t, repo, ctx) })
}

func testProviderIdentityClaims(t *testing.T, repo *Repository, ctx context.Context) {
	t.Helper()
	source := "pve-identity/" + uuid.NewString()
	tasks := make([]domain.Task, 8)
	ids := make([]string, len(tasks))
	connections := make([]string, len(tasks))
	t.Cleanup(func() {
		_ = repo.db.Exec(`DELETE FROM virtualization_tasks WHERE id IN ?`, ids).Error
		_ = repo.db.Exec(`DELETE FROM virtualization_connections WHERE id IN ?`, connections).Error
	})
	for index := range tasks {
		connection, err := repo.CreateConnection(ctx, domain.ConnectionInput{Provider: "pve", Name: "identity-test", Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		connections[index] = connection.ID
		created, err := repo.CreateTask(ctx, domain.Task{Provider: "pve", ConnectionID: connection.ID, TaskKind: "vm_create", Status: "running", ClaimedByWorkerID: uuid.NewString(), AttemptCount: 1, Payload: map[string]any{"providerSourceId": source}})
		if err != nil {
			t.Fatal(err)
		}
		ids[index] = created.ID
		tasks[index], err = repo.GetTask(ctx, created.ID)
		if err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(tasks))
	for _, task := range tasks {
		wg.Add(1)
		go func(task domain.Task) {
			defer wg.Done()
			task.Payload["providerIdentityPrepared"] = true
			task.Payload["providerDispatchStarted"] = true
			task.Payload["providerParams"] = map[string]any{"vmid": "701"}
			_, err := repo.UpdateTask(ctx, task)
			errs <- err
		}(task)
	}
	wg.Wait()
	close(errs)
	successes, conflicts := 0, 0
	for err := range errs {
		if err == nil {
			successes++
		} else if errors.Is(err, domain.ErrProviderIdentityClaimed) {
			conflicts++
		} else {
			t.Fatal(err)
		}
	}
	if successes != 1 || conflicts != 7 {
		t.Fatalf("same source through connection aliases: successes=%d conflicts=%d", successes, conflicts)
	}
	prepared := 0
	for _, id := range ids {
		task, err := repo.GetTask(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if task.Payload["providerIdentityPrepared"] == true {
			prepared++
		} else if task.Payload["providerDispatchStarted"] != nil || task.Payload["providerParams"] != nil {
			t.Fatalf("failed claim saved a partial checkpoint: %+v", task.Payload)
		}
	}
	if prepared != 1 {
		t.Fatalf("persisted %d provider identities", prepared)
	}
}

func mustCapacityTask(t *testing.T, repo *Repository, id string) domain.Task {
	t.Helper()
	task, err := repo.GetTask(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	task.Status = "queued"
	return task
}

func TestCapacityQuotaSpansNodes(t *testing.T) {
	limit := int64(4)
	snapshot := domain.CapacitySnapshot{Namespace: "apps", QuotaCPU: &limit, Nodes: []domain.CapacityNode{{Name: "b", CPU: 8, MemoryMiB: 16000}}, Storage: []domain.CapacityStorage{{Key: "shared", Name: "disk", Nodes: []string{"b"}, AvailableGiB: 200}}}
	pending := []pendingCapacity{{Namespace: "apps", NodeName: "a", StorageKey: "shared", CPU: 3, MemoryMiB: 2048, DiskGiB: 20}}
	_, _, err := selectCapacity(snapshot, domain.CapacityDemand{CPU: 2, MemoryMiB: 2048, DiskGiB: 20}, pending)
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("quota treated as per-node capacity: %v", err)
	}
}

func admitCapacityConcurrently(t *testing.T, ctx context.Context, store *dbstore.Store, repo *Repository, snapshot domain.CapacitySnapshot, demand domain.CapacityDemand, ids []string) domain.Task {
	t.Helper()
	var wg sync.WaitGroup
	results := make(chan domain.Task, 8)
	errorsFound := make(chan error, 8)
	for index := range ids {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			created, err := repo.CreateTaskWithCapacity(ctx, domain.Task{ID: ids[index], Provider: "pve", TaskKind: "vm_create", Status: "queued", Payload: map[string]any{"creationVersion": 2, "requireCapacity": true}}, snapshot, demand)
			if err == nil {
				results <- created
			} else {
				errorsFound <- err
			}
		}(index)
	}
	wg.Wait()
	close(results)
	close(errorsFound)
	if len(results) != 1 {
		t.Fatalf("shared pool admitted %d tasks, want one", len(results))
	}
	for err := range errorsFound {
		if !errors.Is(err, apperrors.ErrConflict) {
			t.Fatal(err)
		}
	}
	winner := <-results
	var taskCount, reservationCount int64
	store.DB().Raw(`SELECT count(*) FROM virtualization_tasks WHERE id IN ?`, ids).Scan(&taskCount)
	store.DB().Raw(`SELECT count(*) FROM virtualization_capacity_reservations WHERE source_id=?`, snapshot.SourceID).Scan(&reservationCount)
	if taskCount != 1 || reservationCount != 1 || winner.Payload["node"] != "a" || winner.Payload["capacityReserved"] != true {
		t.Fatalf("non-atomic admission: tasks=%d reservations=%d %+v", taskCount, reservationCount, winner)
	}
	return winner
}

func verifyCapacityObservation(t *testing.T, ctx context.Context, store *dbstore.Store, repo *Repository, snapshot domain.CapacitySnapshot, demand domain.CapacityDemand, ids []string, winner domain.Task) (string, domain.CapacitySnapshot) {
	t.Helper()
	// Unknown provider outcomes must retain the only disk reservation.
	if err := store.DB().Exec(`UPDATE virtualization_tasks SET status='canceling',attempt_count=1,payload=payload || '{"providerDispatchStarted":true}'::jsonb WHERE id=?`, winner.ID).Error; err != nil {
		t.Fatal(err)
	}
	loserID := ids[0]
	if loserID == winner.ID {
		loserID = ids[1]
	}
	_, err := repo.CreateTaskWithCapacity(ctx, domain.Task{ID: loserID, Provider: "pve", TaskKind: "vm_create", Status: "queued"}, snapshot, demand)
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("unknown effect freed capacity: %v", err)
	}
	if _, _, err := repo.SelectCapacity(ctx, snapshot, demand); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("read ignored unknown effect or NULL result reservation: %v", err)
	}
	// Only a provider observation that includes the original VM can account it.
	if err := store.DB().Exec(`UPDATE virtualization_tasks SET status='completed',result='{"providerEffect":"created"}'::jsonb,finished_at=? WHERE id=?`, time.Now().Add(-time.Second), winner.ID).Error; err != nil {
		t.Fatal(err)
	}
	snapshot.ObservedAt = time.Now()
	snapshot.ObservedOperations = []string{winner.ID}
	snapshot.Storage[0].AvailableGiB = 0
	if _, err := repo.CreateTaskWithCapacity(ctx, domain.Task{ID: loserID, Provider: "pve", TaskKind: "vm_create", Status: "queued"}, snapshot, demand); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("exhausted observed pool admitted: %v", err)
	}
	// Transaction failure above also rolls accounting back. Fresh real free
	// capacity can admit a task and account the already observed allocation.
	snapshot.Storage[0].AvailableGiB = 40
	if _, err := repo.CreateTaskWithCapacity(ctx, domain.Task{ID: loserID, Provider: "pve", TaskKind: "vm_create", Status: "queued"}, snapshot, demand); err != nil {
		t.Fatal(err)
	}
	var state string
	store.DB().Raw(`SELECT state FROM virtualization_capacity_reservations WHERE task_id=?`, winner.ID).Scan(&state)
	if state != "accounted" {
		t.Fatalf("observation not accounted: %s", state)
	}
	older := snapshot
	older.ObservedAt = snapshot.ObservedAt.Add(-time.Second)
	older.Storage = []domain.CapacityStorage{{Key: "shared", Name: "disk", Nodes: []string{"a", "b"}, AvailableGiB: 1000}}
	if _, err := repo.CreateTaskWithCapacity(ctx, domain.Task{ID: uuid.NewString(), Provider: "pve", TaskKind: "vm_create", Status: "queued"}, older, demand); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("older concurrent inventory reused accounted capacity: %v", err)
	}
	if _, _, err := repo.SelectCapacity(ctx, older, demand); !errors.Is(err, domain.ErrCapacityUnknown) {
		t.Fatalf("old inventory read bypassed watermark: %v", err)
	}
	return loserID, snapshot
}

func verifyCapacityRetry(t *testing.T, ctx context.Context, store *dbstore.Store, repo *Repository, snapshot domain.CapacitySnapshot, demand domain.CapacityDemand, ids []string, winnerID, loserID string) {
	t.Helper()
	var state string
	// A queued canceled request did not reach a provider and is reclaimable.
	if err := store.DB().Exec(`UPDATE virtualization_tasks SET status='canceled' WHERE id=?`, loserID).Error; err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.SelectCapacity(ctx, snapshot, demand); err != nil {
		t.Fatalf("read did not account known canceled request: %v", err)
	}
	if err := store.DB().Raw(`SELECT state FROM virtualization_capacity_reservations WHERE task_id=?`, loserID).Scan(&state).Error; err != nil || state != "reserved" {
		t.Fatalf("read changed reservation: %s %v", state, err)
	}
	thirdID := ids[2]
	if thirdID == winnerID || thirdID == loserID {
		thirdID = ids[3]
	}
	if thirdID == winnerID || thirdID == loserID {
		thirdID = ids[4]
	}
	if _, err := repo.CreateTaskWithCapacity(ctx, domain.Task{ID: thirdID, Provider: "pve", TaskKind: "vm_create", Status: "queued"}, snapshot, demand); err != nil {
		t.Fatal(err)
	}
	// Retrying an old canceled task cannot reuse capacity now held by thirdID.
	retry, err := repo.GetTask(ctx, loserID)
	if err != nil {
		t.Fatal(err)
	}
	retry.Status = "queued"
	retry.Payload["requireCapacity"] = true
	retry.Result = map[string]any{"providerEffect": "not_started"}
	if _, err := repo.UpdateTask(ctx, retry); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("retry bypassed admission: %v", err)
	}
	if _, err := repo.RetryTaskWithCapacity(ctx, retry, snapshot, demand); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("retry reused released capacity: %v", err)
	}
	if err := store.DB().Exec(`UPDATE virtualization_tasks SET status='canceled' WHERE id=?`, thirdID).Error; err != nil {
		t.Fatal(err)
	}
	retried, err := repo.RetryTaskWithCapacity(ctx, retry, snapshot, demand)
	if err != nil || retried.ID != loserID {
		t.Fatalf("retry did not reacquire original reservation: %+v %v", retried, err)
	}
	if _, err := repo.RetryTaskWithCapacity(ctx, retry, snapshot, demand); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale retry overwrote original task: %v", err)
	}
	// Its earlier not_started result does not release a task that is queued again.
	if _, err := repo.RetryTaskWithCapacity(ctx, mustCapacityTask(t, repo, thirdID), snapshot, demand); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("queued retry reservation was released: %v", err)
	}
	stale := snapshot
	stale.ObservedAt = time.Now().Add(-time.Minute)
	if _, err := repo.CreateTaskWithCapacity(ctx, domain.Task{}, stale, demand); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale inventory admitted: %v", err)
	}
}
