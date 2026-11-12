package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/domain/delivery"
	domain "github.com/opensoha/soha/internal/domain/radiusdeployment"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestRadiusDeploymentPrivateStatusAndCallbackRequireExecutorIdentity(t *testing.T) {
	repo := newExecutionRepoFake()
	repo.tasks["private"] = delivery.ExecutionTask{ID: "private", ProviderKind: "radius_deployment.edge", TaskKind: "radius_deployment_control", ClaimedByAgentID: "edge", Status: "dispatching", CallbackToken: "secret"}
	service := New(repo, nil, nil, nil, "", "", "", "", 0, "", nil)
	input := delivery.ExecutionCallbackInput{CallbackToken: "secret", Status: "running", Payload: map[string]any{}}
	if _, err := service.RecordCallback(context.Background(), input); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("generic callback accepted: %v", err)
	}
	if len(repo.callbacks) != 0 || len(repo.updates) != 0 {
		t.Fatal("generic callback wrote state")
	}
	if _, err := service.GetRadiusDeploymentExecutionTask(context.Background(), "other", "private"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("wrong identity got task: %v", err)
	}
	if _, err := service.RecordRadiusDeploymentCallback(context.Background(), "other", input); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("wrong identity wrote task: %v", err)
	}
	if task, err := service.GetRadiusDeploymentExecutionTask(context.Background(), "edge", "private"); err != nil || task.CallbackToken != "secret" {
		t.Fatalf("dedicated status failed: %v", err)
	}
	if _, err := service.RecordRadiusDeploymentCallback(context.Background(), "edge", input); err != nil {
		t.Fatal(err)
	}
	repo.tasks["normal"] = delivery.ExecutionTask{ID: "normal", TaskKind: "build", ProviderKind: "ci_agent_runner", CallbackToken: "normal"}
	if _, err := service.RecordRadiusDeploymentCallback(context.Background(), "edge", delivery.ExecutionCallbackInput{CallbackToken: "normal", Status: "running"}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("private executor wrote unrelated task: %v", err)
	}
}

func TestRadiusDeploymentPrivateMaintenanceExpiresQueuedAndClaimedWithoutDispatchingDelivery(t *testing.T) {
	now := time.Now().UTC()
	oldHeartbeat := now.Add(-241 * time.Second)
	repo := newExecutionRepoFake()
	repo.tasks["queued"] = delivery.ExecutionTask{ID: "queued", TaskKind: domain.TaskKind, ProviderKind: "radius_deployment.edge", Status: "queued", CreatedAt: now.Add(-91 * time.Second), TimeoutSeconds: 240, Payload: map[string]any{"radiusDeployment": domain.Command{Deadline: now.Add(-time.Second)}}}
	repo.tasks["claimed"] = delivery.ExecutionTask{ID: "claimed", TaskKind: domain.TaskKind, ProviderKind: "radius_deployment.edge", Status: "running", LastHeartbeatAt: &oldHeartbeat, TimeoutSeconds: 240, Payload: map[string]any{}}
	repo.tasks["ordinary"] = delivery.ExecutionTask{ID: "ordinary", TaskKind: "build", ProviderKind: "ci_agent_runner", Status: "running", LastHeartbeatAt: &oldHeartbeat, TimeoutSeconds: 240}
	repo.tasks["other-slot"] = delivery.ExecutionTask{ID: "other-slot", TaskKind: domain.TaskKind, ProviderKind: "radius_deployment.other", Status: "running", LastHeartbeatAt: &oldHeartbeat, TimeoutSeconds: 240}
	service := New(repo, nil, nil, nil, "", "", "", "", 0, "", nil)
	sink := &executionTaskSinkSpy{}
	service.AddExecutionTaskSink(sink)
	if err := service.maintainRadiusDeployment(context.Background(), []string{"edge"}, now); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"queued", "claimed"} {
		if repo.tasks[id].Status != "callback_timeout" {
			t.Fatalf("%s not expired: %s", id, repo.tasks[id].Status)
		}
	}
	for _, id := range []string{"ordinary", "other-slot"} {
		if repo.tasks[id].Status != "running" {
			t.Fatalf("unrelated task was changed: %s", id)
		}
	}
	if len(sink.tasks) != 2 {
		t.Fatalf("result sinks=%d", len(sink.tasks))
	}
}

func TestRadiusDeploymentPrivateMaintenanceDeadlineHasNoEarlyExpiry(t *testing.T) {
	deadline := time.Now().UTC()
	task := delivery.ExecutionTask{Status: "queued", CreatedAt: deadline.Add(-90 * time.Second), TimeoutSeconds: 240, Payload: map[string]any{"radiusDeployment": domain.Command{Deadline: deadline}}}
	if radiusDeploymentExpired(task, deadline.Add(-time.Nanosecond)) {
		t.Fatal("expired before command deadline")
	}
	if !radiusDeploymentExpired(task, deadline) {
		t.Fatal("did not expire at deadline")
	}
	if executionMonitorInterval != 15*time.Second {
		t.Fatal("maintenance polling bound changed")
	}
}

func TestRadiusDeploymentGenericActionsCannotReplayOrCancel(t *testing.T) {
	repo := newExecutionRepoFake()
	repo.tasks["private"] = delivery.ExecutionTask{ID: "private", TaskKind: domain.TaskKind, ProviderKind: "radius_deployment.edge", Status: "failed"}
	service := New(repo, nil, nil, nil, "", "", "", "", 0, "", nil)
	if _, err := service.RetryExecutionTask(context.Background(), "private", delivery.ExecutionTaskActionInput{}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("generic retry accepted: %v", err)
	}
	repo.tasks["private"] = delivery.ExecutionTask{ID: "private", TaskKind: domain.TaskKind, ProviderKind: "radius_deployment.edge", Status: "running"}
	if _, err := service.CancelExecutionTask(context.Background(), "private", delivery.ExecutionTaskActionInput{}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("generic cancel accepted: %v", err)
	}
	if len(repo.updates) != 0 {
		t.Fatal("generic lifecycle action wrote private task")
	}
	if _, err := service.GetOutpostRuntimeExecutionTask(context.Background(), "edge", "private"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("Outpost read Proxy task")
	}
}
