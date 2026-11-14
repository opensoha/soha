package execution

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type privateTaskReader func(*Service, context.Context, string, string) (delivery.ExecutionTask, error)
type privateTaskCallback func(*Service, context.Context, string, delivery.ExecutionCallbackInput) (delivery.ExecutionTask, error)

func assertPrivateCallbackDenied(t *testing.T, callback func(context.Context, delivery.ExecutionCallbackInput) (delivery.ExecutionTask, error), input delivery.ExecutionCallbackInput) {
	t.Helper()
	if _, err := callback(context.Background(), input); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("generic callback accepted: %v", err)
	}
}
func assertWrongPrivateIdentity(t *testing.T, service *Service, read privateTaskReader, callback privateTaskCallback, input delivery.ExecutionCallbackInput) {
	t.Helper()
	if _, err := read(service, context.Background(), "other", "private"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("wrong identity got task: %v", err)
	}
	if _, err := callback(service, context.Background(), "other", input); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("wrong identity wrote task: %v", err)
	}
}
func assertDedicatedPrivateIdentity(t *testing.T, service *Service, read privateTaskReader, callback privateTaskCallback, input delivery.ExecutionCallbackInput) {
	t.Helper()
	if task, err := read(service, context.Background(), "edge", "private"); err != nil || task.CallbackToken != "secret" {
		t.Fatalf("dedicated status failed: %v", err)
	}
	if _, err := callback(service, context.Background(), "edge", input); err != nil {
		t.Fatal(err)
	}
}
func privateMaintenanceFixture(kind, prefix string, payload map[string]any, now time.Time) (*executionRepoFake, *Service, *executionTaskSinkSpy) {
	oldHeartbeat := now.Add(-241 * time.Second)
	repo := newExecutionRepoFake()
	for _, input := range []struct {
		id, kind, provider, status string
		payload                    map[string]any
	}{
		{"queued", kind, prefix + "edge", "queued", payload},
		{"claimed", kind, prefix + "edge", "running", map[string]any{}},
		{"ordinary", "build", "ci_agent_runner", "running", nil},
		{"other-slot", kind, prefix + "other", "running", nil},
	} {
		task := delivery.ExecutionTask{ID: input.id, TaskKind: input.kind, ProviderKind: input.provider, Status: input.status, TimeoutSeconds: 240, Payload: input.payload}
		if input.id == "queued" {
			task.CreatedAt = now.Add(-91 * time.Second)
		} else {
			task.LastHeartbeatAt = &oldHeartbeat
		}
		repo.tasks[input.id] = task
	}
	service := New(repo, nil, nil, nil, "", "", "", "", 0, "", nil)
	sink := &executionTaskSinkSpy{}
	service.AddExecutionTaskSink(sink)
	return repo, service, sink
}
func assertPrivateMaintenanceResults(t *testing.T, repo *executionRepoFake, sink *executionTaskSinkSpy) {
	t.Helper()
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

func assertPrivateDeadline(t *testing.T, task delivery.ExecutionTask, expired func(delivery.ExecutionTask, time.Time) bool, deadline time.Time) {
	t.Helper()
	if expired(task, deadline.Add(-time.Nanosecond)) {
		t.Fatal("expired before command deadline")
	}
	if !expired(task, deadline) {
		t.Fatal("did not expire at deadline")
	}
	if executionMonitorInterval != 15*time.Second {
		t.Fatal("maintenance polling bound changed")
	}
}
func assertPrivateLifecycleDenied(t *testing.T, service *Service, repo *executionRepoFake) {
	t.Helper()
	if _, err := service.RetryExecutionTask(context.Background(), "private", delivery.ExecutionTaskActionInput{}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("generic retry accepted: %v", err)
	}
	task := repo.tasks["private"]
	task.Status = "running"
	repo.tasks["private"] = task
	if _, err := service.CancelExecutionTask(context.Background(), "private", delivery.ExecutionTaskActionInput{}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("generic cancel accepted: %v", err)
	}
	if len(repo.updates) != 0 {
		t.Fatal("generic lifecycle action wrote private task")
	}
}

func assertPrivateStatusAndCallbacks(t *testing.T, kind, provider string, read privateTaskReader, callback privateTaskCallback) {
	t.Helper()
	repo := newExecutionRepoFake()
	repo.tasks["private"] = delivery.ExecutionTask{ID: "private", ProviderKind: provider, TaskKind: kind, ClaimedByAgentID: "edge", Status: "dispatching", CallbackToken: "secret"}
	service := New(repo, nil, nil, nil, "", "", "", "", 0, "", nil)
	input := delivery.ExecutionCallbackInput{CallbackToken: "secret", Status: "running", Payload: map[string]any{}}
	assertPrivateCallbackDenied(t, service.RecordCallback, input)
	if len(repo.callbacks) != 0 || len(repo.updates) != 0 {
		t.Fatal("generic callback wrote state")
	}
	assertWrongPrivateIdentity(t, service, read, callback, input)
	assertDedicatedPrivateIdentity(t, service, read, callback, input)
	repo.tasks["normal"] = delivery.ExecutionTask{ID: "normal", TaskKind: "build", ProviderKind: "ci_agent_runner", CallbackToken: "normal"}
	if _, err := callback(service, context.Background(), "edge", delivery.ExecutionCallbackInput{CallbackToken: "normal", Status: "running"}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("private executor wrote unrelated task: %v", err)
	}
}
func assertPrivateMaintenance(t *testing.T, kind, prefix string, payload map[string]any, now time.Time, maintain func(*Service, context.Context, []string, time.Time) error) {
	t.Helper()
	repo, service, sink := privateMaintenanceFixture(kind, prefix, payload, now)
	if err := maintain(service, context.Background(), []string{"edge"}, now); err != nil {
		t.Fatal(err)
	}
	assertPrivateMaintenanceResults(t, repo, sink)
}
func assertPrivateCommandDeadline(t *testing.T, payloadKey string, command any, expired func(delivery.ExecutionTask, time.Time) bool, deadline time.Time) {
	t.Helper()
	task := delivery.ExecutionTask{Status: "queued", CreatedAt: deadline.Add(-90 * time.Second), TimeoutSeconds: 240, Payload: map[string]any{payloadKey: command}}
	assertPrivateDeadline(t, task, expired, deadline)
}
func assertPrivateLifecycle(t *testing.T, kind, provider string) {
	t.Helper()
	repo := newExecutionRepoFake()
	repo.tasks["private"] = delivery.ExecutionTask{ID: "private", TaskKind: kind, ProviderKind: provider, Status: "failed"}
	service := New(repo, nil, nil, nil, "", "", "", "", 0, "", nil)
	assertPrivateLifecycleDenied(t, service, repo)
	if _, err := service.GetOutpostRuntimeExecutionTask(context.Background(), "edge", "private"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatal("Outpost read Proxy task")
	}
}

func TestPrivateExpiryDecodeKeepsTimeoutFallback(t *testing.T) {
	now := time.Now().UTC()
	for _, runtime := range []struct {
		name, key string
		expired   func(delivery.ExecutionTask, time.Time) bool
	}{
		{"outpost", "outpostRuntime", outpostRuntimeExpired},
		{"proxy", "proxyDeployment", proxyDeploymentExpired},
		{"radius", "radiusDeployment", radiusDeploymentExpired},
	} {
		t.Run(runtime.name, func(t *testing.T) {
			for _, payload := range []any{func() {}, map[string]any{"deadline": now.Add(-time.Second), "replicas": "invalid"}} {
				task := delivery.ExecutionTask{Status: "queued", CreatedAt: now.Add(-time.Second), TimeoutSeconds: 240, Payload: map[string]any{runtime.key: payload}}
				if runtime.expired(task, now) {
					t.Fatal("invalid typed command bypassed queued timeout")
				}
				task.CreatedAt = now.Add(-241 * time.Second)
				if !runtime.expired(task, now) {
					t.Fatal("invalid typed command disabled queued timeout")
				}
			}
		})
	}
}
