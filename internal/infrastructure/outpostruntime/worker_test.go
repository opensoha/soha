package outpostruntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/domain/delivery"
	domain "github.com/opensoha/soha/internal/domain/outpostruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type fakeExecution struct {
	task      delivery.ExecutionTask
	callbacks []delivery.ExecutionCallbackInput
	statusErr error
	drain     domain.DrainStatus
}

func (f *fakeExecution) Claim(context.Context) (delivery.ExecutionTask, error) { return f.task, nil }
func (f *fakeExecution) Status(context.Context, string) (delivery.ExecutionTask, error) {
	task := f.task
	task.Result = map[string]any{"outpostRuntimeDrain": f.drain}
	return task, f.statusErr
}
func (f *fakeExecution) Callback(_ context.Context, input delivery.ExecutionCallbackInput) (delivery.ExecutionTask, error) {
	f.callbacks = append(f.callbacks, input)
	f.task.Status = input.Status
	return f.task, nil
}

type fakeScale struct {
	observes, writes      int
	initial, after        domain.Observation
	writeErr, readbackErr error
	initialErr            error
	deadline              time.Time
}

func (f *fakeScale) Observe(ctx context.Context, _ domain.Slot) (domain.Observation, error) {
	f.deadline, _ = ctx.Deadline()
	f.observes++
	if f.observes == 1 {
		return f.initial, f.initialErr
	}
	return f.after, f.readbackErr
}

func TestWorkerPropagatesDurableCommandDeadlineToKubernetes(t *testing.T) {
	slot := domain.Slot{ID: "slot", OutpostID: "edge", ExecutorID: "executor", Namespace: "soha", Name: "edge", UID: "uid", TemplateDigest: "digest", MaxReplicas: 3}
	deadline := time.Now().Add(5 * time.Second)
	command := domain.Command{OutpostID: "edge", Slot: slot, Generation: 2, OwnerEpoch: 1, Action: "start", Replicas: 1, Deadline: deadline}
	task := delivery.ExecutionTask{ID: "task", TaskKind: domain.TaskKind, ProviderKind: slot.ProviderKind(), Status: "dispatching", ClaimedByAgentID: "executor", CallbackToken: "callback", Payload: map[string]any{"outpostRuntime": command}}
	client := &fakeExecution{task: task}
	runtime := &fakeScale{initial: domain.Observation{UID: "uid", TemplateDigest: "digest", Replicas: 1, ReadyReplicas: 1, DesiredReplicas: 1}}
	worker, _ := NewWorker(client, runtime, "executor", []domain.Slot{slot})
	worker.Execute(context.Background(), task)
	if !runtime.deadline.Equal(deadline) {
		t.Fatalf("Kubernetes deadline=%v want %v", runtime.deadline, deadline)
	}
}
func (f *fakeScale) Scale(context.Context, domain.Slot, domain.Observation, int32) error {
	f.writes++
	return f.writeErr
}

func TestWorkerReadsBackUnknownWriteWithoutRepeating(t *testing.T) {
	for _, test := range []struct {
		name        string
		readbackErr error
		fenced      bool
		wantStatus  string
		wantWrites  int
	}{
		{"lost write response but observed", nil, false, "completed", 1},
		{"unknown outcome retained", errors.New("API unavailable"), false, "failed", 1},
		{"stale generation", nil, true, "failed", 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			slot := domain.Slot{ID: "slot", OutpostID: "edge", ExecutorID: "executor", Namespace: "soha", Name: "edge", UID: "uid", TemplateDigest: "sha256:template", MaxReplicas: 3}
			command := domain.Command{OutpostID: "edge", Slot: slot, Generation: 2, OwnerEpoch: 1, Action: "start", Replicas: 2, Deadline: time.Now().Add(time.Minute)}
			task := delivery.ExecutionTask{ID: "task", TaskKind: domain.TaskKind, ProviderKind: slot.ProviderKind(), Status: "dispatching", ClaimedByAgentID: "executor", CallbackToken: "callback", Payload: map[string]any{"outpostRuntime": command}}
			client := &fakeExecution{task: task}
			if test.fenced {
				client.statusErr = apperrors.ErrConflict
			}
			runtime := &fakeScale{initial: domain.Observation{UID: "uid", TemplateDigest: slot.TemplateDigest, DesiredReplicas: 1, Replicas: 1, ReadyReplicas: 1}, after: domain.Observation{UID: "uid", TemplateDigest: slot.TemplateDigest, DesiredReplicas: 2, Replicas: 2, ReadyReplicas: 2}, writeErr: errors.New("response lost"), readbackErr: test.readbackErr}
			worker, _ := NewWorker(client, runtime, "executor", []domain.Slot{slot})
			worker.Execute(context.Background(), task)
			if runtime.writes != test.wantWrites {
				t.Fatalf("writes=%d", runtime.writes)
			}
			last := client.callbacks[len(client.callbacks)-1]
			if last.Status != test.wantStatus {
				t.Fatalf("status=%s", last.Status)
			}
			if test.readbackErr != nil {
				var receipt domain.Receipt
				encoded, _ := json.Marshal(last.Payload["outpostRuntime"])
				_ = json.Unmarshal(encoded, &receipt)
				if !receipt.Uncertain {
					t.Fatal("unknown outcome hidden")
				}
			}
		})
	}
}

func TestStopRequiresFreshDrainEvidenceBeforeScale(t *testing.T) {
	slot := domain.Slot{ID: "slot", OutpostID: "edge", ExecutorID: "executor", Namespace: "soha", Name: "edge", UID: "uid", TemplateDigest: "digest", MaxReplicas: 3}
	command := domain.Command{OutpostID: "edge", Slot: slot, Generation: 2, OwnerEpoch: 1, Action: "stop", DrainTimeoutSeconds: 1, Deadline: time.Now().Add(time.Minute)}
	task := delivery.ExecutionTask{ID: "task", TaskKind: domain.TaskKind, ProviderKind: slot.ProviderKind(), Status: "dispatching", ClaimedByAgentID: "executor", CallbackToken: "callback", Payload: map[string]any{"outpostRuntime": command}}
	for _, ready := range []bool{true, false} {
		t.Run(map[bool]string{true: "drained", false: "still accepting"}[ready], func(t *testing.T) {
			client := &fakeExecution{task: task, drain: domain.DrainStatus{Instances: 2, AcceptingRequests: !ready}}
			scaler := &fakeScale{initial: domain.Observation{UID: "uid", TemplateDigest: "digest", Replicas: 2, ReadyReplicas: 2, DesiredReplicas: 2}, after: domain.Observation{UID: "uid", TemplateDigest: "digest"}}
			worker, _ := NewWorker(client, scaler, "executor", []domain.Slot{slot})
			worker.Execute(context.Background(), task)
			if (scaler.writes == 1) != ready {
				t.Fatalf("writes=%d", scaler.writes)
			}
			last := client.callbacks[len(client.callbacks)-1]
			if ready && last.Status != "completed" {
				t.Fatalf("status=%s", last.Status)
			}
		})
	}
}
