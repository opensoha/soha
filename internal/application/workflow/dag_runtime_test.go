package workflow

import (
	"context"
	"errors"
	"reflect"
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

type workflowPersistenceContextKey struct{}

type workflowPersistenceRepository struct {
	Repository
	result     domainworkflow.Run
	err        error
	contextErr error
	value      any
}

func (r *workflowPersistenceRepository) Update(ctx context.Context, _ domainworkflow.Run) (domainworkflow.Run, error) {
	r.contextErr, r.value = ctx.Err(), ctx.Value(workflowPersistenceContextKey{})
	return r.result, r.err
}

func TestUpdateRunPreservesPersistenceResultAndContext(t *testing.T) {
	for _, name := range []string{"success", "failure", "cancelled context", "nil context"} {
		t.Run(name, func(t *testing.T) {
			input := domainworkflow.Run{ID: "run", Status: "running", Version: 1}
			stored := domainworkflow.Run{ID: "run", Status: "completed", Version: 2}
			repo := &workflowPersistenceRepository{result: stored}
			service := &Service{repo: repo}
			ctx := context.WithValue(t.Context(), workflowPersistenceContextKey{}, "request")
			want := stored
			if name == "failure" {
				repo.err = errors.New("database unavailable")
				want = input
			}
			if name == "cancelled context" {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelled
			}
			if name == "nil context" {
				ctx = nil
			}
			if got := service.updateRun(ctx, input); !reflect.DeepEqual(got, want) {
				t.Fatalf("persisted run = %+v, want %+v", got, want)
			}
			if repo.contextErr != nil || (name != "nil context" && repo.value != "request") {
				t.Fatalf("persistence context lost: error = %v, value = %v", repo.contextErr, repo.value)
			}
		})
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
