package virtualization

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/runtimeobs"
)

// Model the production repository's optimistic lock, including heartbeat writes.
type completionTasks struct {
	*memoryRepo
	writeErr error
}

func (r *completionTasks) UpdateTask(ctx context.Context, task domain.Task) (domain.Task, error) {
	current, err := r.GetTask(ctx, task.ID)
	if err != nil {
		return domain.Task{}, err
	}
	if r.writeErr != nil {
		return domain.Task{}, r.writeErr
	}
	if !current.UpdatedAt.Equal(task.UpdatedAt) {
		return domain.Task{}, apperrors.ErrConflict
	}
	return r.memoryRepo.UpdateTask(ctx, task)
}

func (r *completionTasks) HeartbeatTask(ctx context.Context, id, worker string, now time.Time) error {
	if err := r.memoryRepo.HeartbeatTask(ctx, id, worker, now); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	task := r.tasks[id]
	task.UpdatedAt = now
	r.tasks[id] = task
	return nil
}

func TestAssetSyncCompletionAfterHeartbeat(t *testing.T) {
	for _, failedWrite := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "write failure"}[failedWrite], func(t *testing.T) {
			repo := newMemoryRepo()
			connection := repo.addConnection(domain.Connection{Provider: ProviderPVE, Enabled: true})
			_, _ = repo.CreateTask(context.Background(), domain.Task{ID: "sync", ConnectionID: connection.ID, Provider: ProviderPVE, TaskKind: TaskKindAssetSync, Status: TaskStatusQueued})
			operations := &captureOperations{}
			service := newTestService(repo, operations, fakeAdapter{syncResult: domain.AssetSyncResult{Assets: []domain.Asset{{Type: "vm", Name: "vm-1"}}}})
			tasks := &completionTasks{memoryRepo: repo}
			if failedWrite {
				tasks.writeErr = errors.New("database unavailable")
			}
			service.tasks, service.taskQueue = tasks, tasks
			service.runOnce(context.Background())
			stored, _ := repo.GetTask(context.Background(), "sync")
			if failedWrite {
				if stored.Status == TaskStatusSucceeded || operations.has("virtualization.worker.asset_sync") {
					t.Fatal("failed completion was reported as success")
				}
			} else if stored.Status != TaskStatusSucceeded || stored.FinishedAt == nil || stored.LastHeartbeatAt == nil || stored.Result["assetCount"] != 1 {
				t.Fatalf("completion after heartbeat = %#v", stored)
			}
			logs, _ := repo.ListTaskLogs(context.Background(), "sync", 100)
			var hasCompleted, hasFailure bool
			for _, log := range logs {
				hasCompleted = hasCompleted || strings.Contains(log.Message, "asset sync completed")
				hasFailure = hasFailure || strings.Contains(log.Message, "persist task completion")
			}
			if hasCompleted == failedWrite || hasFailure != failedWrite {
				t.Fatalf("completed=%v failure=%v", hasCompleted, hasFailure)
			}
		})
	}
}

func TestCompleteTaskRejectsStaleAttemptsAndTerminalStates(t *testing.T) {
	for _, change := range []string{"canceled", "canceling", "timeout", "new worker", "new attempt", "missing"} {
		t.Run(change, func(t *testing.T) {
			repo := newMemoryRepo()
			task := domain.Task{ID: "task", Status: TaskStatusRunning, ClaimedByWorkerID: "worker-1", AttemptCount: 1}
			current := task
			switch change {
			case "canceled":
				current.Status = TaskStatusCanceled
			case "canceling":
				current.Status = TaskStatusCanceling
			case "timeout":
				current.Status = TaskStatusTimeout
			case "new worker":
				current.ClaimedByWorkerID = "worker-2"
			case "new attempt":
				current.AttemptCount++
			}
			if change != "missing" {
				repo.tasks[task.ID] = current
			}
			service := newTestService(repo, &captureOperations{}, fakeAdapter{})
			outcome, err := service.completeTask(context.Background(), task)
			if change == "canceled" {
				if outcome != runtimeobs.OutcomeCanceled || err != nil {
					t.Fatalf("canceled completion = %s, %v", outcome, err)
				}
			} else if outcome != runtimeobs.OutcomeFailed || err == nil {
				t.Fatalf("stale completion = %s, %v", outcome, err)
			}
			stored, _ := repo.GetTask(context.Background(), task.ID)
			if stored.Status == TaskStatusSucceeded || stored.FinishedAt != nil {
				t.Fatalf("stale completion overwrote task: %#v", stored)
			}
		})
	}
}

func TestAssetSyncPartialScanFailsWithoutInvalidatingInventory(t *testing.T) {
	repo := newMemoryRepo()
	lastSync := time.Now().Add(-time.Hour)
	connection := repo.addConnection(domain.Connection{Provider: ProviderPVE, Enabled: true, LastSyncedAt: &lastSync})
	repo.vms["existing"] = domain.VM{ID: "existing", ConnectionID: connection.ID, Provider: ProviderPVE, Status: "running", LastSeenAt: &lastSync}
	_, _ = repo.CreateTask(context.Background(), domain.Task{ID: "sync", ConnectionID: connection.ID, Provider: ProviderPVE, TaskKind: TaskKindAssetSync, Status: TaskStatusQueued})
	operations := &captureOperations{}
	service := newTestService(repo, operations, fakeAdapter{syncResult: domain.AssetSyncResult{
		Health: domain.AssetHealth{Status: "degraded", Message: "node inventory timed out", Reason: "api_unavailable"},
		Assets: []domain.Asset{{Type: "vm", Name: "partial-vm"}},
	}})
	service.runOnce(context.Background())
	task, _ := repo.GetTask(context.Background(), "sync")
	if task.Status != TaskStatusFailed || task.FinishedAt == nil || task.Result["error"] != "node inventory timed out" {
		t.Fatalf("partial scan task = %#v", task)
	}
	stored, _ := repo.GetConnection(context.Background(), connection.ID)
	if stored.Health["status"] != "degraded" || !stored.LastSyncedAt.Equal(lastSync) || len(repo.vms) != 1 || repo.vms["existing"].Status != "running" {
		t.Fatalf("partial scan invalidated inventory: connection=%#v vms=%#v", stored, repo.vms)
	}
	logs, _ := repo.ListTaskLogs(context.Background(), "sync", 100)
	for _, log := range logs {
		if strings.Contains(log.Message, "asset sync completed") {
			t.Fatal("partial scan logged successful completion")
		}
	}
}
