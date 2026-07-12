package workflow

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

func TestDAGExecutor_BoundsParallelHandlers(t *testing.T) {
	var active atomic.Int64
	var maximum atomic.Int64
	handler := dagNodeHandlerFunc(func(context.Context, dagNodeExecutionInput) dagNodeHandlerResult {
		current := active.Add(1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		time.Sleep(15 * time.Millisecond)
		active.Add(-1)
		return completedDAGNode("done")
	})
	registry := &dagNodeHandlerRegistry{handlers: map[string]dagNodeHandler{"test": handler}, fallback: unsupportedDAGNodeHandler{}}
	executor := &dagExecutor{parallelism: 2, dispatcher: &dagNodeDispatcher{registry: registry}}
	ready := make([]dagWorkflowNode, 0, 6)
	for i := 0; i < 6; i++ {
		ready = append(ready, dagWorkflowNode{ID: string(rune('a' + i)), Name: "test", Type: "test", TimeoutSeconds: 1})
	}
	results := executor.executeReady(context.Background(), domainidentity.Principal{}, domainapp.App{}, domainworkflow.Input{}, domaincatalog.ApplicationEnvironment{}, ready, domainworkflow.Run{}, nil)
	if len(results) != len(ready) {
		t.Fatalf("results len = %d, want %d", len(results), len(ready))
	}
	if got := maximum.Load(); got != 2 {
		t.Fatalf("maximum parallel handlers = %d, want 2", got)
	}
}

func TestDAGExecutor_CancellationReturnsStableResults(t *testing.T) {
	started := make(chan struct{}, 1)
	handler := dagNodeHandlerFunc(func(ctx context.Context, _ dagNodeExecutionInput) dagNodeHandlerResult {
		started <- struct{}{}
		<-ctx.Done()
		return failedDAGNode(ctx.Err())
	})
	registry := &dagNodeHandlerRegistry{handlers: map[string]dagNodeHandler{"test": handler}, fallback: unsupportedDAGNodeHandler{}}
	executor := &dagExecutor{parallelism: 1, dispatcher: &dagNodeDispatcher{registry: registry}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan []dagExecutionResult, 1)
	go func() {
		done <- executor.executeReady(ctx, domainidentity.Principal{}, domainapp.App{}, domainworkflow.Input{}, domaincatalog.ApplicationEnvironment{}, []dagWorkflowNode{
			{ID: "one", Name: "one", Type: "test", TimeoutSeconds: 1},
			{ID: "two", Name: "two", Type: "test", TimeoutSeconds: 1},
		}, domainworkflow.Run{}, nil)
	}()
	<-started
	cancel()
	results := <-done
	if len(results) != 2 {
		t.Fatalf("results len = %d, want 2", len(results))
	}
	for _, result := range results {
		if result.status != "failed" || result.summary != context.Canceled.Error() {
			t.Fatalf("canceled result = %#v", result)
		}
	}
}

func TestRunStateStore_PublishesCloneAndObservesCancellation(t *testing.T) {
	var store runStateStore
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan domainworkflow.Run, 1)
	go func() {
		run, _ := store.wait(ctx, "run-1", "completed")
		done <- run
	}()
	run := domainworkflow.Run{ID: "run-1", Status: "completed", Metadata: map[string]any{"value": "original"}}
	store.publish(run)
	run.Metadata["value"] = "changed"
	got := <-done
	if got.Status != "completed" || got.Metadata["value"] != "original" {
		t.Fatalf("wait result = %#v", got)
	}

	canceled, stop := context.WithCancel(context.Background())
	stop()
	if _, err := store.wait(canceled, "missing"); err != context.Canceled {
		t.Fatalf("wait cancellation error = %v, want context canceled", err)
	}
}

func TestDAGScheduler_BoundsQueueAndShutsDown(t *testing.T) {
	var scheduler dagScheduler
	scheduler.configure(1, 1)
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	scheduler.start(context.Background(), func(context.Context, dagRunTask) {
		started <- struct{}{}
		<-release
	})
	if _, err := scheduler.enqueue(context.Background(), dagRunTask{}); err != nil {
		t.Fatalf("enqueue first task: %v", err)
	}
	<-started
	if _, err := scheduler.enqueue(context.Background(), dagRunTask{}); err != nil {
		t.Fatalf("enqueue queued task: %v", err)
	}
	enqueueCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := scheduler.enqueue(enqueueCtx, dagRunTask{}); err != context.DeadlineExceeded {
		t.Fatalf("enqueue full queue error = %v, want deadline exceeded", err)
	}
	close(release)
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), time.Second)
	defer shutdownCancel()
	if _, err := scheduler.shutdown(shutdownCtx); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}
