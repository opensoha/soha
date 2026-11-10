package workflow

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainrelease "github.com/opensoha/soha/internal/domain/release"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

func TestConcurrentExternalExecutionCallbacks(t *testing.T) {
	for _, tc := range []struct {
		name  string
		nodes []string
	}{
		{name: "duplicate", nodes: []string{"external-a"}},
		{name: "distinct", nodes: []string{"external-a", "external-b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			proceed := make(chan struct{})
			repo := &gatedPausedRunRepository{stubWorkflowRepository: &stubWorkflowRepository{}, contenders: make(chan struct{}, 2), proceed: proceed}
			taskStore := &recordingWorkflowArtifactStore{}
			run, tasks := waitingExternalRun(t, tc.nodes)
			repo.setItems([]domainworkflow.Run{run})
			for _, task := range tasks {
				if _, err := taskStore.CreateExecutionTask(ctx, task); err != nil {
					t.Fatal(err)
				}
			}
			var releases atomic.Int64
			services := []*Service{externalCallbackService(t, repo, taskStore, &releases), externalCallbackService(t, repo, taskStore, &releases)}
			stale := tasks[0]
			stale.ID = "other-attempt"
			if err := services[0].RecordExecutionTaskResult(ctx, stale); err != nil {
				t.Fatalf("stale callback: %v", err)
			}
			errs := make(chan error, len(services))
			for i, service := range services {
				go func() { errs <- service.RecordExecutionTaskResult(ctx, tasks[i%len(tasks)]) }()
			}
			for range services {
				select {
				case <-repo.contenders:
				case <-ctx.Done():
					t.Fatal("both callbacks did not reach the paused run transition")
				}
			}
			close(proceed)
			for range services {
				select {
				case err := <-errs:
					if err != nil {
						t.Fatal(err)
					}
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			final := waitForStoredCallbackRun(t, ctx, repo, run.ID)
			late := tasks[0]
			late.Status = "failed"
			if err := services[0].RecordExecutionTaskResult(ctx, late); err != nil {
				t.Fatalf("late conflicting callback: %v", err)
			}
			if got, err := repo.Get(ctx, run.ID); err != nil || got.Version != final.Version {
				t.Fatalf("late callback rewrote terminal run: %+v, %v", got, err)
			}
			assertCallbackDispatch(t, final, len(tasks), releases.Load())
		})
	}
}

func TestExternalExecutionCallbackBeforeRunPauses(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	proceed := make(chan struct{})
	releasePause := sync.OnceFunc(func() { close(proceed) })
	defer releasePause()
	repo := &gatedPauseRepository{stubWorkflowRepository: &stubWorkflowRepository{}, pausing: make(chan struct{}), proceed: proceed}
	taskStore := &recordingWorkflowArtifactStore{}
	var releases atomic.Int64
	service := externalCallbackService(t, repo, taskStore, &releases)
	dispatches := make(chan callbackReleaseDispatch, 2)
	service.releases = callbackReleaseExecutor{countingWorkflowReleaseExecutor{count: &releases}, dispatches}
	run, tasks := waitingExternalRun(t, []string{"external-a"})
	run.Status = "queued"
	repo.setItems([]domainworkflow.Run{run})
	pending := tasks[0]
	pending.Status = "queued"
	if _, err := taskStore.CreateExecutionTask(ctx, pending); err != nil {
		t.Fatal(err)
	}
	definition, ok := definitionFromRunMetadata(run)
	if !ok {
		t.Fatal("workflow definition is missing")
	}
	continuation, err := service.executionCallbackContinuation(ctx, run, definition, pending)
	if err != nil {
		t.Fatal(err)
	}
	continuation.run = run
	continuation.principal = domainidentity.Principal{UserID: "trigger-only-user", UserName: "trigger-only-user"}
	continuation.input.ImageTag = "request-only-tag"
	if err := service.enqueueDAGRun(ctx, continuation); err != nil {
		t.Fatal(err)
	}
	select {
	case <-repo.pausing:
	case <-ctx.Done():
		t.Fatal("worker did not reach the pause transition")
	}
	taskStore.mu.Lock()
	taskStore.tasks[0] = tasks[0]
	taskStore.mu.Unlock()
	if err := service.RecordExecutionTaskResult(ctx, tasks[0]); err != nil {
		t.Fatal(err)
	}
	if active, err := repo.Get(ctx, run.ID); err != nil || active.Status != "running" || releases.Load() != 0 {
		t.Fatalf("callback overwrote the active worker: %+v, %v", active, err)
	}
	releasePause()
	assertCallbackDispatch(t, waitForStoredCallbackRun(t, ctx, repo, run.ID), 1, releases.Load())
	if got := repo.pauseWrites.Load(); got != 2 {
		t.Fatalf("pause writes = %d, want initial failure and successful retry", got)
	}
	select {
	case dispatch := <-dispatches:
		if dispatch.principal.UserID != workflowSystemPrincipal().UserID || dispatch.input.ImageTag != "" || dispatch.input.ApplicationEnvironmentID != pending.ApplicationEnvironmentID {
			t.Fatalf("early callback dispatch used trigger context: %+v", dispatch)
		}
	default:
		t.Fatal("no release dispatch captured")
	}
}

type callbackReleaseDispatch struct {
	principal domainidentity.Principal
	input     domainrelease.TriggerInput
}

type callbackReleaseExecutor struct {
	countingWorkflowReleaseExecutor
	dispatches chan<- callbackReleaseDispatch
}

func (e callbackReleaseExecutor) Trigger(ctx context.Context, principal domainidentity.Principal, input domainrelease.TriggerInput) (domainrelease.Record, error) {
	e.dispatches <- callbackReleaseDispatch{principal: principal, input: input}
	return e.countingWorkflowReleaseExecutor.Trigger(ctx, principal, input)
}

type gatedPauseRepository struct {
	*stubWorkflowRepository
	pausing     chan struct{}
	proceed     <-chan struct{}
	pauseWrites atomic.Int64
}

func (r *gatedPauseRepository) UpdateApplicationRunIfCurrent(ctx context.Context, item domainworkflow.Run, status string, approval *domainworkflow.Approval) (domainworkflow.Run, error) {
	if item.Status == workflowStatusWaitingExecution && r.pauseWrites.Add(1) == 1 {
		close(r.pausing)
		select {
		case <-r.proceed:
			return domainworkflow.Run{}, errors.New("transient pause write error")
		case <-ctx.Done():
			return domainworkflow.Run{}, ctx.Err()
		}
	}
	return r.stubWorkflowRepository.UpdateApplicationRunIfCurrent(ctx, item, status, approval)
}

type gatedPausedRunRepository struct {
	*stubWorkflowRepository
	contenders chan struct{}
	proceed    <-chan struct{}
}

func (r *gatedPausedRunRepository) UpdateApplicationRunIfCurrent(ctx context.Context, item domainworkflow.Run, status string, approval *domainworkflow.Approval) (domainworkflow.Run, error) {
	if item.Status == "queued" {
		select {
		case r.contenders <- struct{}{}:
		case <-ctx.Done():
			return domainworkflow.Run{}, ctx.Err()
		}
		select {
		case <-r.proceed:
		case <-ctx.Done():
			return domainworkflow.Run{}, ctx.Err()
		}
	}
	return r.stubWorkflowRepository.UpdateApplicationRunIfCurrent(ctx, item, status, approval)
}

func externalCallbackService(t *testing.T, repo Repository, store *recordingWorkflowArtifactStore, releases *atomic.Int64) *Service {
	t.Helper()
	binding := domaincatalog.ApplicationEnvironment{
		ID: "binding-1", ApplicationID: "app-1",
		Targets: []domaincatalog.ReleaseTarget{{ID: "target-1", ClusterID: "cluster-1", Namespace: "default", WorkloadKind: "Deployment", WorkloadName: "demo", Enabled: true}},
	}
	service := &Service{repo: repo, apps: &stubWorkflowApps{}, catalog: &stubWorkflowCatalog{items: []domaincatalog.ApplicationEnvironment{binding}}, releases: countingWorkflowReleaseExecutor{count: releases}}
	service.SetArtifactStore(store)
	service.SetRuntimeOptions(1, 8, 1)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := service.Shutdown(ctx); err != nil {
			t.Error(err)
		}
	})
	return service
}

func waitingExternalRun(t *testing.T, externalNodes []string) (domainworkflow.Run, []domaindelivery.ExecutionTask) {
	t.Helper()
	run := domainworkflow.Run{
		ID: "workflow:external-callback", ApplicationID: "app-1", WorkflowName: "delivery-dag",
		ClusterID: "cluster-1", Namespace: "default", DeploymentName: "demo", Status: workflowStatusWaitingExecution,
	}
	definition := dagWorkflowDefinition{Mode: "delivery_dag", Nodes: []dagWorkflowNode{{ID: "release", Name: "release", Type: "release"}}}
	for _, id := range externalNodes {
		definition.Nodes = append(definition.Nodes, dagWorkflowNode{ID: id, Name: id, Type: "verify", ExecutorKind: "mcp"})
		definition.Edges = append(definition.Edges, dagWorkflowEdge{ID: "edge-" + id, Source: id, Target: "release", Condition: "success"})
	}
	run.Metadata = map[string]any{"mode": definition.Mode, "nodes": definition.Nodes, "edges": definition.Edges}
	nodeRuns := initializeNodeRuns(definition)
	tasks := make([]domaindelivery.ExecutionTask, 0, len(externalNodes))
	for _, id := range externalNodes {
		task := domaindelivery.ExecutionTask{
			ID: "task:" + id, ApplicationEnvironmentID: "binding-1", Status: "completed",
			Payload: map[string]any{"workflowRunId": run.ID, "workflowNodeId": id},
		}
		node := nodeRuns[id]
		node.Status = workflowStatusWaitingExecution
		nodeRuns[id] = node
		recordDAGNodeOutputs(&run, id, map[string]any{"outputs": map[string]any{"executionTaskId": task.ID}})
		tasks = append(tasks, task)
	}
	return reloadWorkflowRun(t, syncRunNodeState(run, definition, nodeRuns)), tasks
}

func waitForStoredCallbackRun(t *testing.T, ctx context.Context, repo Repository, runID string) domainworkflow.Run {
	t.Helper()
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	for {
		run, err := repo.Get(ctx, runID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status == "completed" || run.Status == "failed" {
			return run
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatalf("workflow remained %q: %v", run.Status, ctx.Err())
		}
	}
}

func assertCallbackDispatch(t *testing.T, run domainworkflow.Run, wantCallbacks int, releases int64) {
	t.Helper()
	if run.Status != "completed" || releases != 1 {
		t.Fatalf("workflow status = %q, release dispatches = %d; want completed with one dispatch", run.Status, releases)
	}
	callbacks := 0
	for _, event := range metadataMapSlice(run.Metadata["events"]) {
		if event["type"] == "execution_task_callback" {
			callbacks++
		}
	}
	if callbacks != wantCallbacks {
		t.Fatalf("recorded %d callback events, want %d", callbacks, wantCallbacks)
	}
}
