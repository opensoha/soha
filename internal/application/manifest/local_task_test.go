package manifest

import (
	"context"
	"errors"
	resourceruntime "github.com/opensoha/soha-contracts/resource/runtime"
	"sync"
	"testing"
	"time"

	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
)

type localTaskFixture struct {
	ManifestTaskRuntime
	mu        sync.Mutex
	task      domaindelivery.ExecutionTask
	readError error
	callbacks []string
}

func (f *localTaskFixture) RecordCallback(_ context.Context, input domaindelivery.ExecutionCallbackInput) (domaindelivery.ExecutionTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callbacks = append(f.callbacks, input.Status)
	if f.task.Status != "canceled" {
		f.task.Status = input.Status
	}
	return f.task, nil
}
func (f *localTaskFixture) GetExecutionTaskInternal(context.Context, string) (domaindelivery.ExecutionTask, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.task, f.readError
}

type blockingManifestRuntime struct {
	started   chan struct{}
	stopError error
}

func (r blockingManifestRuntime) Execute(ctx context.Context, _ domainmanifest.TaskPayload) (domainmanifest.TaskResult, error) {
	close(r.started)
	<-ctx.Done()
	if r.stopError != nil {
		return domainmanifest.TaskResult{}, r.stopError
	}
	return domainmanifest.TaskResult{}, ctx.Err()
}

func TestLocalManifestExecutionStopsOnCancelTimeoutAndLostOwnership(t *testing.T) {
	for _, reason := range []string{"cancel", "timeout", "new attempt", "state unavailable", "stop unconfirmed", "rollout stop unconfirmed"} {
		t.Run(reason, func(t *testing.T) {
			task := domaindelivery.ExecutionTask{ID: "task-1", CallbackToken: "attempt-1", Status: "dispatching", TimeoutSeconds: 5, Payload: map[string]any{"action": "apply", "packageId": "package-1", "generation": 1, "idempotencyKey": "key-1"}}
			fixture := &localTaskFixture{task: task}
			started, done := make(chan struct{}), make(chan struct{})
			runtime := blockingManifestRuntime{started: started}
			if reason == "stop unconfirmed" {
				runtime.stopError = resourceruntime.ErrArgoStopUnconfirmed
			}
			if reason == "rollout stop unconfirmed" {
				runtime.stopError = resourceruntime.ErrRolloutStopUnconfirmed
			}
			service := &DeclarativeService{tasks: fixture, direct: runtime}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			if reason == "timeout" {
				var shortCancel context.CancelFunc
				ctx, shortCancel = context.WithTimeout(ctx, 25*time.Millisecond)
				defer shortCancel()
			}
			go func() { defer close(done); service.executeLocalTask(ctx, task) }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("runtime did not start")
			}
			fixture.mu.Lock()
			switch reason {
			case "cancel", "stop unconfirmed", "rollout stop unconfirmed":
				fixture.task.Status = "canceled"
			case "new attempt":
				fixture.task.CallbackToken = "attempt-2"
			case "state unavailable":
				fixture.readError = errors.New("database unavailable")
			}
			fixture.mu.Unlock()
			select {
			case <-done:
			case <-time.After(2 * time.Second):
				t.Fatal("runtime continued after stop")
			}
			want := "canceled"
			if reason == "timeout" {
				want = "callback_timeout"
			}
			if reason == "state unavailable" || reason == "stop unconfirmed" || reason == "rollout stop unconfirmed" {
				want = "failed"
			}
			fixture.mu.Lock()
			defer fixture.mu.Unlock()
			if len(fixture.callbacks) != 2 || fixture.callbacks[1] != want {
				t.Fatalf("callbacks = %v, want running and %s", fixture.callbacks, want)
			}
		})
	}
}
