package proxydeployment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/domain/delivery"
	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type fakeExecution struct {
	task        delivery.ExecutionTask
	callbacks   []delivery.ExecutionCallbackInput
	statusErr   error
	statusCalls int
	callbackErr error
	drain       domain.DrainStatus
}

func (f *fakeExecution) Claim(context.Context) (delivery.ExecutionTask, error) { return f.task, nil }
func (f *fakeExecution) Status(context.Context, string) (delivery.ExecutionTask, error) {
	f.statusCalls++
	task := f.task
	task.Result = map[string]any{"proxyDeploymentDrain": f.drain}
	return task, f.statusErr
}
func (f *fakeExecution) Callback(_ context.Context, input delivery.ExecutionCallbackInput) (delivery.ExecutionTask, error) {
	f.callbacks = append(f.callbacks, input)
	f.task.Status = input.Status
	return f.task, f.callbackErr
}

type fakeScale struct {
	observes, writes      int
	initial, after        domain.Observation
	writeErr, readbackErr error
	initialErr            error
	deadline              time.Time
}

func (f *fakeScale) Observe(ctx context.Context, slot domain.Slot) (domain.Observation, error) {
	f.deadline, _ = ctx.Deadline()
	f.observes++
	// Observe returns the pre-write state until Scale actually runs, including
	// the refresh after drain; only a write can expose the configured readback.
	observation := f.initial
	if f.writes > 0 {
		observation = f.after
	}
	if observation.UID == "" {
		observation.UID = slot.UID
	}
	if observation.TemplateDigest == "" {
		observation.TemplateDigest = slot.TemplateDigest
	}
	if observation.ResourceVersion == "" {
		observation.ResourceVersion = "fixture-rv"
	}
	if f.observes == 1 {
		return observation, f.initialErr
	}
	if f.writes > 0 {
		return observation, f.readbackErr
	}
	return observation, nil
}

func TestWorkerPropagatesDurableCommandDeadlineToKubernetes(t *testing.T) {
	slot := domain.Slot{ID: "slot", ProxyID: "edge", ExecutorID: "executor", Namespace: "soha", Name: "edge", UID: "uid", TemplateDigest: "digest", WorkloadKind: "StatefulSet", Members: []domain.Member{{InstanceID: "edge-0", RuntimeID: "p0", Ordinal: 0}, {InstanceID: "edge-1", RuntimeID: "p1", Ordinal: 1}, {InstanceID: "edge-2", RuntimeID: "p2", Ordinal: 2}}, MaxReplicas: 3}
	deadline := time.Now().Add(5 * time.Second)
	command := domain.Command{ProxyID: "edge", Slot: slot, Generation: 2, OwnerEpoch: 1, Action: "start", Replicas: 1, Deadline: deadline}
	task := delivery.ExecutionTask{ID: "task", TaskKind: domain.TaskKind, ProviderKind: slot.ProviderKind(), Status: "dispatching", ClaimedByAgentID: "executor", CallbackToken: "callback", Payload: map[string]any{"proxyDeployment": command}}
	client := &fakeExecution{task: task, drain: domain.DrainStatus{Instances: 3, CountKnown: true, Ready: true}}
	runtime := &fakeScale{initial: domain.Observation{UID: "uid", TemplateDigest: "digest", Replicas: 1, ReadyReplicas: 1, DesiredReplicas: 1}}
	runtime.after = runtime.initial
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
			slot := domain.Slot{ID: "slot", ProxyID: "edge", ExecutorID: "executor", Namespace: "soha", Name: "edge", UID: "uid", TemplateDigest: "sha256:template", WorkloadKind: "StatefulSet", Members: []domain.Member{{InstanceID: "edge-0", RuntimeID: "p0", Ordinal: 0}, {InstanceID: "edge-1", RuntimeID: "p1", Ordinal: 1}, {InstanceID: "edge-2", RuntimeID: "p2", Ordinal: 2}}, MaxReplicas: 3}
			command := domain.Command{ProxyID: "edge", Slot: slot, Generation: 2, OwnerEpoch: 1, Action: "start", Replicas: 2, Deadline: time.Now().Add(time.Minute)}
			task := delivery.ExecutionTask{ID: "task", TaskKind: domain.TaskKind, ProviderKind: slot.ProviderKind(), Status: "dispatching", ClaimedByAgentID: "executor", CallbackToken: "callback", Payload: map[string]any{"proxyDeployment": command}}
			client := &fakeExecution{task: task, drain: domain.DrainStatus{Instances: 3, CountKnown: true, Ready: true}}
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
				encoded, _ := json.Marshal(last.Payload["proxyDeployment"])
				_ = json.Unmarshal(encoded, &receipt)
				if !receipt.Uncertain {
					t.Fatal("unknown outcome hidden")
				}
			}
		})
	}
}

func TestStopRequiresFreshDrainEvidenceBeforeScale(t *testing.T) {
	slot := domain.Slot{ID: "slot", ProxyID: "edge", ExecutorID: "executor", Namespace: "soha", Name: "edge", UID: "uid", TemplateDigest: "digest", WorkloadKind: "StatefulSet", Members: []domain.Member{{InstanceID: "edge-0", RuntimeID: "p0", Ordinal: 0}, {InstanceID: "edge-1", RuntimeID: "p1", Ordinal: 1}, {InstanceID: "edge-2", RuntimeID: "p2", Ordinal: 2}}, MaxReplicas: 3}
	command := domain.Command{ProxyID: "edge", Slot: slot, Generation: 2, OwnerEpoch: 1, Action: "stop", DrainTimeoutSeconds: 1, DrainUntil: time.Now().Add(time.Second), Deadline: time.Now().Add(60 * time.Millisecond)}
	task := delivery.ExecutionTask{ID: "task", TaskKind: domain.TaskKind, ProviderKind: slot.ProviderKind(), Status: "dispatching", ClaimedByAgentID: "executor", CallbackToken: "callback", Payload: map[string]any{"proxyDeployment": command}}
	for _, ready := range []bool{true, false} {
		t.Run(map[bool]string{true: "drained", false: "still accepting"}[ready], func(t *testing.T) {
			client := &fakeExecution{task: task, drain: domain.DrainStatus{CountKnown: true, Instances: 2, AcceptingRequests: !ready}}
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

func TestProxyDeploymentUnknownCountOrMissingMemberNeverScales(t *testing.T) {
	for _, status := range []domain.DrainStatus{{Instances: 2, CountKnown: false}, {Instances: 1, CountKnown: true}} {
		slot := domain.Slot{ID: "slot", ProxyID: "master", ExecutorID: "exec", Namespace: "soha", Name: "pool", UID: "uid", TemplateDigest: "digest", WorkloadKind: "StatefulSet", MaxReplicas: 2, Members: []domain.Member{{InstanceID: "pool-0", RuntimeID: "p0", Ordinal: 0}, {InstanceID: "pool-1", RuntimeID: "p1", Ordinal: 1}}}
		command := domain.Command{ProxyID: "master", Slot: slot, Generation: 2, OwnerEpoch: 1, Action: "stop", DrainTimeoutSeconds: 120, Deadline: time.Now().Add(20 * time.Millisecond)}
		task := delivery.ExecutionTask{ID: "task", TaskKind: domain.TaskKind, ProviderKind: slot.ProviderKind(), ClaimedByAgentID: "exec", Status: "dispatching", CallbackToken: "callback", Payload: map[string]any{"proxyDeployment": command}}
		client := &fakeExecution{task: task, drain: status}
		scale := &fakeScale{initial: domain.Observation{Replicas: 2, DesiredReplicas: 2}}
		worker, err := NewWorker(client, scale, "exec", []domain.Slot{slot})
		if err != nil {
			t.Fatal(err)
		}
		worker.Execute(context.Background(), task)
		if scale.writes != 0 {
			t.Fatal("unknown/partial membership treated as zero active")
		}
	}
}
func TestProxyDeploymentShrinkDrainsThenResumesSurvivors(t *testing.T) {
	slot := domain.Slot{ID: "slot", ProxyID: "master", ExecutorID: "exec", Namespace: "soha", Name: "pool", UID: "uid", TemplateDigest: "digest", WorkloadKind: "StatefulSet", MaxReplicas: 2, Members: []domain.Member{{InstanceID: "pool-0", RuntimeID: "p0", Ordinal: 0}, {InstanceID: "pool-1", RuntimeID: "p1", Ordinal: 1}}}
	command := domain.Command{ProxyID: "master", Slot: slot, Generation: 2, OwnerEpoch: 1, Action: "scale", Replicas: 1, DrainTimeoutSeconds: 120, DrainUntil: time.Now().Add(120 * time.Second), Deadline: time.Now().Add(time.Second)}
	task := delivery.ExecutionTask{ID: "task", TaskKind: domain.TaskKind, ProviderKind: slot.ProviderKind(), ClaimedByAgentID: "exec", Status: "dispatching", CallbackToken: "callback", Payload: map[string]any{"proxyDeployment": command}}
	client := &fakeExecution{task: task, drain: domain.DrainStatus{Instances: 1, CountKnown: true, Ready: true}}
	scale := &fakeScale{initial: domain.Observation{Replicas: 2, DesiredReplicas: 2}, after: domain.Observation{Replicas: 1, DesiredReplicas: 1, ReadyReplicas: 1}}
	worker, err := NewWorker(client, scale, "exec", []domain.Slot{slot})
	if err != nil {
		t.Fatal(err)
	}
	worker.Execute(context.Background(), task)
	resumed := false
	for _, callback := range client.callbacks {
		raw, _ := json.Marshal(callback.Payload["proxyDeployment"])
		var receipt domain.Receipt
		_ = json.Unmarshal(raw, &receipt)
		resumed = resumed || receipt.Resume
	}
	if scale.writes != 1 || !resumed || client.callbacks[len(client.callbacks)-1].Status != "completed" {
		t.Fatal("shrink did not explicitly resume survivors")
	}
}

func TestWaitingReadyUsesSingleStatusAndStaysBelowMinuteBudget(t *testing.T) {
	for _, replicasMatch := range []bool{true, false} {
		t.Run(fmt.Sprint(replicasMatch), func(t *testing.T) {
			client := &fakeExecution{task: delivery.ExecutionTask{ID: "task", CallbackToken: "callback", Status: "running"}}
			runtime := &fakeScale{}
			elapsed := time.Duration(0)
			worker := &Worker{client: client, runtime: runtime, pollWait: func(_ context.Context, delay time.Duration) bool {
				if delay != 2*time.Second {
					t.Fatalf("poll delay=%v", delay)
				}
				elapsed += delay
				return elapsed < time.Minute
			}}
			observation := domain.Observation{}
			if replicasMatch {
				observation.Replicas, observation.DesiredReplicas = 1, 1
			}
			runtime.after = observation
			receipt := domain.Receipt{}
			status := worker.waitForConvergence(context.Background(), client.task, domain.Command{Replicas: 1, Deadline: time.Now().Add(2 * time.Minute)}, observation, &receipt)
			requests := len(client.callbacks) + client.statusCalls
			if elapsed != time.Minute || status != "failed" || client.statusCalls != 30 || len(client.callbacks) != 30 || requests >= 120 {
				t.Fatalf("elapsed=%v status=%s heartbeats=%d statuses=%d requests=%d", elapsed, status, len(client.callbacks), client.statusCalls, requests)
			}
			if runtime.writes != 0 {
				t.Fatal("waitingReady wrote scale")
			}
		})
	}
}

func TestWorkerRejectedHeartbeatOrExpiredDeadlineNeverScales(t *testing.T) {
	for _, test := range []struct {
		name        string
		callbackErr error
		expired     bool
	}{
		{"heartbeat rejected", apperrors.ErrAccessDenied, false},
		{"deadline expired", nil, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			slot := domain.Slot{ID: "slot", ProxyID: "edge", ExecutorID: "exec", Namespace: "soha", Name: "edge", UID: "uid", TemplateDigest: "digest", WorkloadKind: "StatefulSet", MaxReplicas: 1, Members: []domain.Member{{InstanceID: "edge-0", RuntimeID: "p0", Ordinal: 0}}}
			deadline := time.Now().Add(time.Minute)
			if test.expired {
				deadline = time.Now().Add(-time.Second)
			}
			command := domain.Command{ProxyID: "edge", Slot: slot, Generation: 1, OwnerEpoch: 1, Action: "start", Replicas: 1, Deadline: deadline}
			task := delivery.ExecutionTask{ID: "task", TaskKind: domain.TaskKind, ProviderKind: slot.ProviderKind(), ClaimedByAgentID: "exec", Status: "dispatching", CallbackToken: "callback", Payload: map[string]any{"proxyDeployment": command}}
			client := &fakeExecution{task: task, callbackErr: test.callbackErr}
			runtime := &fakeScale{}
			worker, err := NewWorker(client, runtime, "exec", []domain.Slot{slot})
			if err != nil {
				t.Fatal(err)
			}
			worker.Execute(context.Background(), task)
			if runtime.writes != 0 {
				t.Fatal("write after lost heartbeat/deadline")
			}
		})
	}
}
