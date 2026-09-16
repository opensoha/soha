package virtualization

import (
	"context"
	"errors"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type recoveryAdapter struct {
	fakeAdapter
	prepares  int
	onPrepare func()
	create    func(domain.AdapterCreateVMInput) (domain.AdapterVM, error)
}

type observingAdapter struct {
	recoveryAdapter
	found bool
	reads int
}

func (a *observingAdapter) ObserveVMCreation(_ context.Context, _ domain.AdapterConnection, input domain.AdapterCreateVMInput) (domain.AdapterVM, bool, error) {
	a.reads++
	return domain.AdapterVM{ID: "701", Name: input.Name, Status: "stopped"}, a.found, nil
}

func TestVMCreateCanceledUnknownReconcilesWithoutAnotherWrite(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo()
	connection := repo.addConnection(domain.Connection{Provider: ProviderPVE, Enabled: true})
	adapter := &observingAdapter{}
	service := newTestService(repo, &captureOperations{}, adapter)
	created, err := service.CreateVM(ctx, testPrincipal(), CreateVMInput{ConnectionID: connection.ID, Name: "cancel-unknown", CPU: 2, MemoryMiB: 2048})
	if err != nil {
		t.Fatal(err)
	}
	writes := 0
	adapter.create = func(input domain.AdapterCreateVMInput) (domain.AdapterVM, error) {
		writes++
		_, err := service.CancelOperation(ctx, testPrincipal(), input.OperationID)
		if err != nil {
			t.Fatal(err)
		}
		pending, _ := repo.GetTask(ctx, input.OperationID)
		service.observeCanceledCreation(ctx, pending)
		if adapter.reads != 0 {
			t.Fatal("observed completion while provider caller is still active")
		}
		return domain.AdapterVM{}, errors.New("response lost")
	}
	claimed, _ := repo.ClaimTask(ctx, "worker", time.Now())
	service.executeTask(ctx, claimed)
	pending, _ := repo.GetTask(ctx, created.ID)
	if !boolValue(pending.Result, "providerAttemptFinished") {
		t.Fatal("missing durable caller exit receipt")
	}
	rebuilt := newTestService(repo, &captureOperations{}, adapter)
	rebuilt.observeCanceledCreation(ctx, pending)
	pending, _ = repo.GetTask(ctx, created.ID)
	if pending.Status != TaskStatusCanceling || boolValue(pending.Result, "cancellationConfirmed") {
		t.Fatal("absence incorrectly proved provider cancellation")
	}
	adapter.found = true
	rebuilt.observeCanceledCreation(ctx, pending)
	completed, _ := repo.GetTask(ctx, created.ID)
	if completed.Status != TaskStatusCanceled || completed.VMID == "" || completed.Result["providerEffect"] != "created" || writes != 1 || adapter.reads != 2 {
		t.Fatalf("observation lost original receipt or wrote again: task=%+v writes=%d reads=%d", completed, writes, adapter.reads)
	}
	if _, err := rebuilt.RetryOperation(ctx, testPrincipal(), created.ID); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("retried already created VM: %v", err)
	}
}

func TestVMCreateRetryBeforeDispatchAndLegacyCancellation(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo()
	connection := repo.addConnection(domain.Connection{Provider: ProviderPVE, Enabled: true})
	adapter := &recoveryAdapter{}
	service := newTestService(repo, &captureOperations{}, adapter)
	created, err := service.CreateVM(ctx, testPrincipal(), CreateVMInput{ConnectionID: connection.ID, Name: "retry", CPU: 2, MemoryMiB: 2048})
	if err != nil {
		t.Fatal(err)
	}
	claimed, _ := repo.ClaimTask(ctx, "first", time.Now())
	service.failTask(ctx, claimed, errors.New("connection temporarily unavailable"))
	if _, err := service.RetryOperation(ctx, testPrincipal(), created.ID); err != nil {
		t.Fatal(err)
	}
	adapter.create = func(input domain.AdapterCreateVMInput) (domain.AdapterVM, error) {
		return domain.AdapterVM{ID: "701", Name: input.Name, Status: "running"}, nil
	}
	claimed, _ = repo.ClaimTask(ctx, "second", time.Now())
	service.executeTask(ctx, claimed)
	completed, _ := repo.GetTask(ctx, created.ID)
	if completed.Status != TaskStatusSucceeded {
		t.Fatalf("known pre-dispatch failure cannot retry: %+v", completed)
	}
	legacy := claimed
	legacy.ID, legacy.Status, legacy.Payload = "legacy", TaskStatusRunning, nil
	repo.tasks[legacy.ID] = legacy
	canceled, err := service.CancelOperation(ctx, testPrincipal(), legacy.ID)
	if err != nil || canceled.Status != TaskStatusCanceling || boolValue(canceled.Result, "cancellationConfirmed") {
		t.Fatalf("legacy provider outcome incorrectly confirmed: %+v %v", canceled, err)
	}
}

func (a *recoveryAdapter) PrepareVMCreate(_ context.Context, _ domain.AdapterConnection, input domain.AdapterCreateVMInput) (domain.AdapterCreateVMInput, error) {
	a.prepares++
	input.Node, input.Namespace = "frozen-node", "frozen-ns"
	input.ProviderParams = map[string]any{"vmid": "701", "storage": "frozen-storage"}
	if a.onPrepare != nil {
		a.onPrepare()
	}
	return input, nil
}

func (a *recoveryAdapter) CreateVM(_ context.Context, _ domain.AdapterConnection, input domain.AdapterCreateVMInput) (domain.AdapterVM, error) {
	return a.create(input)
}

func TestVMCreateRetryKeepsPersistedProviderIdentity(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo()
	connection := repo.addConnection(domain.Connection{Provider: ProviderPVE, Enabled: true})
	adapter := &recoveryAdapter{}
	service := newTestService(repo, &captureOperations{}, adapter)
	created, err := service.CreateVM(ctx, testPrincipal(), CreateVMInput{ConnectionID: connection.ID, Name: "stable", CPU: 2, MemoryMiB: 2048})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	adapter.create = func(input domain.AdapterCreateVMInput) (domain.AdapterVM, error) {
		calls++
		stored, err := repo.GetTask(ctx, created.ID)
		if err != nil || !boolValue(stored.Payload, "providerDispatchStarted") || input.OperationID != created.ID || input.Node != "frozen-node" || input.ProviderParams["vmid"] != "701" {
			t.Fatalf("provider called without frozen identity: %+v %v", input, err)
		}
		if boolValue(stored.Result, "providerAttemptFinished") {
			t.Fatal("retry inherited the previous caller's exit acknowledgment")
		}
		if calls == 1 {
			return domain.AdapterVM{}, errors.New("provider response lost")
		}
		return domain.AdapterVM{ID: "701", Name: input.Name, Node: input.Node, Status: "running"}, nil
	}
	claimed, _ := repo.ClaimTask(ctx, "worker-1", time.Now())
	service.executeTask(ctx, claimed)
	failed, _ := repo.GetTask(ctx, created.ID)
	if failed.Status != TaskStatusFailed || failed.Result["providerEffect"] != "unknown" {
		t.Fatalf("lost effect classified: %+v", failed)
	}
	if _, err := service.RetryOperation(ctx, testPrincipal(), created.ID); err != nil {
		t.Fatal(err)
	}
	rebuilt := newTestService(repo, &captureOperations{}, adapter)
	claimed, _ = repo.ClaimTask(ctx, "worker-2", time.Now())
	rebuilt.executeTask(ctx, claimed)
	completed, _ := repo.GetTask(ctx, created.ID)
	if adapter.prepares != 1 || calls != 2 || completed.Status != TaskStatusSucceeded || completed.VMID == "" || completed.Result["providerVmId"] != "701" {
		t.Fatalf("retry lost provider identity: prepares=%d calls=%d task=%+v", adapter.prepares, calls, completed)
	}
}

func TestVMCreateCancellationRetainsProviderReceipt(t *testing.T) {
	for _, outcome := range []string{"created", "unknown", "before-dispatch"} {
		t.Run(outcome, func(t *testing.T) {
			ctx := context.Background()
			repo := newMemoryRepo()
			connection := repo.addConnection(domain.Connection{Provider: ProviderPVE, Enabled: true})
			adapter := &recoveryAdapter{}
			service := newTestService(repo, &captureOperations{}, adapter)
			created, err := service.CreateVM(ctx, testPrincipal(), CreateVMInput{ConnectionID: connection.ID, Name: "cancel", CPU: 2, MemoryMiB: 2048})
			if err != nil {
				t.Fatal(err)
			}
			cancel := func() {
				item, err := service.CancelOperation(ctx, testPrincipal(), created.ID)
				if err != nil {
					t.Fatal(err)
				}
				if outcome != "before-dispatch" && (item.Status != TaskStatusCanceling || item.FinishedAt != nil) {
					t.Fatalf("premature cancellation: %+v", item)
				}
			}
			if outcome == "before-dispatch" {
				adapter.onPrepare = cancel
			}
			adapter.create = func(input domain.AdapterCreateVMInput) (domain.AdapterVM, error) {
				if outcome == "before-dispatch" {
					t.Fatal("provider ran after cancellation")
				}
				cancel()
				if outcome == "unknown" {
					return domain.AdapterVM{}, context.DeadlineExceeded
				}
				return domain.AdapterVM{ID: "701", Name: input.Name, Status: "running"}, nil
			}
			claimed, _ := repo.ClaimTask(ctx, "worker", time.Now())
			service.executeTask(ctx, claimed)
			item, _ := repo.GetTask(ctx, created.ID)
			if outcome == "unknown" {
				if item.Status != TaskStatusCanceling || item.Result["providerEffect"] != "unknown" || domain.WithOperationState(item, time.Now()).OperationState.Terminal {
					t.Fatalf("unknown effect released: %+v", item)
				}
			} else if item.Status != TaskStatusCanceled || !boolValue(item.Result, "cancellationConfirmed") {
				t.Fatalf("cancellation not confirmed: %+v", item)
			}
			if outcome == "created" && (item.VMID == "" || item.Result["providerEffect"] != "created") {
				t.Fatalf("created resource receipt lost: %+v", item)
			}
		})
	}
}

func TestVMCreateCheckpointRejectsStaleWorker(t *testing.T) {
	repo := newMemoryRepo()
	repo.tasks["task"] = domain.Task{ID: "task", Status: TaskStatusRunning, ClaimedByWorkerID: "new", AttemptCount: 2}
	service := newTestService(repo, &captureOperations{}, fakeAdapter{})
	_, err := service.checkpointVMCreate(context.Background(), domain.Task{ID: "task", ClaimedByWorkerID: "old", AttemptCount: 1}, domain.AdapterCreateVMInput{}, "connection-identity")
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal(err)
	}
}

func TestVMCreateLateReceiptAfterTimeoutIsFenced(t *testing.T) {
	repo := newMemoryRepo()
	repo.tasks["task"] = domain.Task{ID: "task", TaskKind: TaskKindVMCreate, Status: TaskStatusTimeout, ClaimedByWorkerID: "worker", AttemptCount: 1, Payload: map[string]any{"providerDispatchStarted": true}}
	service := newTestService(repo, &captureOperations{}, fakeAdapter{})
	receipt := domain.Task{ID: "task", VMID: "vm", ClaimedByWorkerID: "worker", AttemptCount: 1, Result: map[string]any{"providerVmId": "701"}}
	if err := service.finishVMCreate(context.Background(), receipt); err != nil {
		t.Fatal(err)
	}
	stored, _ := repo.GetTask(context.Background(), "task")
	if stored.Status != TaskStatusSucceeded || stored.VMID != "vm" {
		t.Fatalf("late receipt lost: %+v", stored)
	}
	stored.Status = TaskStatusRunning
	stored.ClaimedByWorkerID = "new-worker"
	stored.AttemptCount = 2
	_, _ = repo.UpdateTask(context.Background(), stored)
	if err := service.finishVMCreate(context.Background(), receipt); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("late receipt overwrote new claim: %v", err)
	}
}
