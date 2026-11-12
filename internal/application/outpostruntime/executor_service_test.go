package outpostruntime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/domain/delivery"
	domain "github.com/opensoha/soha/internal/domain/outpostruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type privateTasks struct {
	task      delivery.ExecutionTask
	claimed   string
	callbacks int
}

func (f *privateTasks) ClaimOutpostRuntimeExecutionTask(_ context.Context, id string) (delivery.ExecutionTask, error) {
	f.claimed = id
	return f.task, nil
}
func (f *privateTasks) GetOutpostRuntimeExecutionTask(context.Context, string, string) (delivery.ExecutionTask, error) {
	return f.task, nil
}
func (f *privateTasks) RecordOutpostRuntimeCallback(context.Context, string, delivery.ExecutionCallbackInput) (delivery.ExecutionTask, error) {
	f.callbacks++
	return f.task, nil
}

func TestExecutorCredentialAndSlotFencing(t *testing.T) {
	slot := testSlot()
	command := domain.Command{OutpostID: "edge", Slot: slot, Generation: 2, OwnerEpoch: 1, Deadline: time.Now().Add(time.Minute)}
	task := delivery.ExecutionTask{ID: "task", TaskKind: domain.TaskKind, ProviderKind: slot.ProviderKind(), ClaimedByAgentID: "executor", CallbackToken: "callback", Payload: map[string]any{"outpostRuntime": command}}
	repo := &memoryRepository{state: domain.State{OutpostID: "edge", SlotID: "slot", Owner: "soha", UID: "uid", Generation: 2, OwnerEpoch: 1, LatestTaskID: "task"}, task: task}
	service, _ := New(repo, testPermissions(), []domain.Slot{slot}, nil)
	tasks := &privateTasks{task: task}
	token := strings.Repeat("x", 32)
	executor, err := NewExecutorService(service, tasks, []ExecutorCredential{{ExecutorID: "executor", Token: token}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = executor.Claim(context.Background(), "wrong", "executor"); !errors.Is(err, apperrors.ErrUnauthorized) || tasks.claimed != "" {
		t.Fatalf("wrong token reached claim: %v", err)
	}
	if _, err = executor.Claim(context.Background(), token, "other"); !errors.Is(err, apperrors.ErrAccessDenied) || tasks.claimed != "" {
		t.Fatalf("identity spoof reached claim: %v", err)
	}
	if _, err = executor.Claim(context.Background(), token, "executor"); err != nil || tasks.claimed != "executor" {
		t.Fatal(err)
	}
	repo.state.Generation++
	if _, err = executor.Status(context.Background(), token, "task"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("stale task read: %v", err)
	}
	if _, err = executor.Callback(context.Background(), token, delivery.ExecutionCallbackInput{CallbackToken: "callback", Status: "completed"}); !errors.Is(err, apperrors.ErrConflict) || tasks.callbacks != 0 {
		t.Fatalf("stale receipt wrote: %v", err)
	}
	repo.state.Generation = command.Generation
	input := delivery.ExecutionCallbackInput{CallbackToken: "callback", Status: "completed"}
	if _, err = executor.Callback(context.Background(), token, input); !errors.Is(err, apperrors.ErrInvalidArgument) || tasks.callbacks != 0 {
		t.Fatalf("terminal callback reused an earlier receipt: %v", err)
	}
	input.Payload = map[string]any{"outpostRuntime": domain.Receipt{Generation: command.Generation, OwnerEpoch: command.OwnerEpoch}}
	if _, err = executor.Callback(context.Background(), token, input); err != nil || tasks.callbacks != 1 {
		t.Fatalf("matching terminal callback did not reach ledger: %v", err)
	}
}
