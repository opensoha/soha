package execution

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"strings"
	"testing"
	"time"

	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

type deliveryDispatchRepoFake struct{ *executionRepoFake }

func (r *deliveryDispatchRepoFake) PrepareDeliveryJob(ctx context.Context, id string, reference map[string]any) (domaindelivery.ExecutionTask, error) {
	task, _ := r.GetExecutionTask(ctx, id)
	if task.Status == "queued" {
		task.Status, task.AttemptCount, task.Result = "dispatching", 1, maps.Clone(reference)
		r.tasks[id] = task
	}
	return task, nil
}

func (r *deliveryDispatchRepoFake) RequestDeliveryTaskStop(ctx context.Context, id, reason string) (domaindelivery.ExecutionTask, error) {
	task, _ := r.GetExecutionTask(ctx, id)
	if !isStrictTerminalTaskStatus(task.Status) && task.Status != "canceling" {
		task.Status = "canceling"
		task.Result = mergeMaps(task.Result, map[string]any{"cancelReason": reason})
		r.tasks[id] = task
	}
	return task, nil
}

type deliveryJobRuntimeFake struct {
	request      ExecutionJobRequest
	createCalls  int
	createErr    error
	inspection   ExecutionJobInspection
	stopState    ExecutionJobState
	beforeCreate func()
}

func (r *deliveryJobRuntimeFake) ClusterIDs() []string { return []string{"cluster"} }
func (r *deliveryJobRuntimeFake) CreateExecutionJob(_ context.Context, _ string, request ExecutionJobRequest) (ExecutionJobRef, error) {
	r.beforeCreate()
	r.createCalls++
	r.request = request
	return ExecutionJobRef{Name: request.Name, Namespace: request.Namespace}, r.createErr
}
func (r *deliveryJobRuntimeFake) InspectExecutionJob(context.Context, ExecutionJobRef) (ExecutionJobInspection, error) {
	return r.inspection, nil
}
func (r *deliveryJobRuntimeFake) DeleteExecutionJob(context.Context, ExecutionJobRef) error {
	panic("delivery cancellation must confirm stop")
}
func (r *deliveryJobRuntimeFake) StopDeliveryJob(context.Context, string, ExecutionJobRequest) (ExecutionJobInspection, error) {
	return ExecutionJobInspection{State: r.stopState}, nil
}

func TestDeliveryJobRecoversAmbiguousCreateWithoutRedispatch(t *testing.T) {
	ctx := context.Background()
	repo := &deliveryDispatchRepoFake{newExecutionRepoFake()}
	task := domaindelivery.ExecutionTask{ID: "task:durable", TaskKind: "build", ProviderKind: "k8s_job_runner", Status: "queued", CallbackToken: "token", Payload: map[string]any{"workflowScope": domainworkflow.ScopeDeliveryBatch, "commands": []string{"build"}, "image": "registry/app:v1"}}
	repo.tasks[task.ID] = task
	runtime := &deliveryJobRuntimeFake{createErr: context.DeadlineExceeded, inspection: ExecutionJobInspection{State: ExecutionJobSucceeded, ImageDigest: "sha256:" + strings.Repeat("a", 64)}}
	runtime.beforeCreate = func() {
		stored := repo.tasks[task.ID]
		if stored.Status != "dispatching" || stored.Result["k8sJobName"] != DeliveryJobName(task.ID) || stored.AttemptCount != 1 {
			t.Fatal("external creation preceded durable identity")
		}
	}
	service := New(repo, nil, nil, runtime, "cluster", "jobs", "alpine", "git", 30, "", nil)
	_, err := service.dispatchExecutionTask(ctx, task)
	if !errors.Is(err, context.DeadlineExceeded) || repo.tasks[task.ID].Status != "dispatching" {
		t.Fatalf("uncertain API write lost recoverable state: %v", err)
	}
	if handled, err := service.reconcileK8sJobExecution(ctx, repo.tasks[task.ID], time.Now()); !handled || err != nil {
		t.Fatalf("recover successful external write: %t %v", handled, err)
	}
	stored := repo.tasks[task.ID]
	if stored.Status != "completed" || stored.Result["imageDigest"] != runtime.inspection.ImageDigest || runtime.createCalls != 1 || !runtime.request.Retain || runtime.request.TimeoutSeconds != 300 {
		t.Fatalf("recovery replayed or lost artifact: %+v calls=%d", stored, runtime.createCalls)
	}
	if _, err := service.dispatchExecutionTask(ctx, task); err != nil || runtime.createCalls != 1 {
		t.Fatalf("stale caller dispatched completed Job: %v", err)
	}
}

func TestDeliveryJobTimeoutSurvivesHeartbeatAndRestart(t *testing.T) {
	ctx := context.Background()
	now := time.Now().UTC()
	started := now.Add(-time.Minute)
	repo := &deliveryDispatchRepoFake{newExecutionRepoFake()}
	task := domaindelivery.ExecutionTask{ID: "task:timeout", TaskKind: "build", ProviderKind: "k8s_job_runner", Status: "running", TimeoutSeconds: 60, StartedAt: &started, LastHeartbeatAt: &now, Payload: map[string]any{"workflowScope": domainworkflow.ScopeDeliveryBatch}, Result: map[string]any{"k8sJobName": "known", "k8sJobClusterId": "cluster", "k8sJobNamespace": "jobs"}}
	repo.tasks[task.ID] = task
	runtime := &deliveryJobRuntimeFake{inspection: ExecutionJobInspection{State: ExecutionJobRunning}, stopState: ExecutionJobRunning}
	service := New(repo, nil, nil, runtime, "cluster", "jobs", "alpine", "git", 30, "", nil)
	if err := service.recoverStaleTasks(ctx, now); err != nil {
		t.Fatal(err)
	}
	stopping := repo.tasks[task.ID]
	if stopping.Status != "canceling" || stopping.FinishedAt != nil || stopping.Result["cancelReason"] != executionTimeoutReason {
		t.Fatalf("timeout must await actual stop despite fresh heartbeat: %s %#v", stopping.Status, stopping.Result)
	}
	runtime.stopState = ExecutionJobCanceled
	restarted := New(repo, nil, nil, runtime, "cluster", "jobs", "alpine", "git", 30, "", nil)
	if err := restarted.recoverStaleTasks(ctx, now); err != nil {
		t.Fatal(err)
	}
	stopped := repo.tasks[task.ID]
	if stopped.Status != "failed" || stopped.FinishedAt == nil || stopped.Result["error"] != executionTimeoutMessage(task) {
		t.Fatalf("confirmed timeout lost failure cause: %s %#v", stopped.Status, stopped.Result)
	}
	legacyRequest, err := json.Marshal(deliveryJobRequest(task))
	if err != nil || strings.Contains(string(legacyRequest), "TimeoutSeconds") {
		t.Fatalf("legacy Job identity changed: %s %v", legacyRequest, err)
	}
}

func TestDeliveryJobCancellationNeedsRuntimeConfirmation(t *testing.T) {
	ctx := context.Background()
	repo := &deliveryDispatchRepoFake{newExecutionRepoFake()}
	task := domaindelivery.ExecutionTask{ID: "task:cancel", TaskKind: "release", ProviderKind: "k8s_job_runner", Status: "running", Payload: map[string]any{"workflowScope": domainworkflow.ScopeDeliveryBatch}, Result: map[string]any{"k8sJobName": "known", "k8sJobClusterId": "cluster", "k8sJobNamespace": "jobs"}}
	repo.tasks[task.ID] = task
	runtime := &deliveryJobRuntimeFake{stopState: ExecutionJobRunning}
	service := New(repo, nil, nil, runtime, "cluster", "jobs", "alpine", "git", 30, "", nil)
	stopping, err := service.CancelExecutionTask(ctx, task.ID, domaindelivery.ExecutionTaskActionInput{Reason: "user"})
	if err != nil || stopping.Status != "canceling" || stopping.FinishedAt != nil {
		t.Fatalf("unconfirmed stop became terminal: %+v %v", stopping, err)
	}
	runtime.stopState = ExecutionJobCanceled
	stopped, err := service.CancelExecutionTask(ctx, task.ID, domaindelivery.ExecutionTaskActionInput{Reason: "user"})
	if err != nil || stopped.Status != "canceled" || stopped.FinishedAt == nil {
		t.Fatalf("confirmed stop did not finish: %+v %v", stopped, err)
	}
}
