package proxydeployment

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/domain/delivery"
	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
)

func TestReadonlyReconcileNeverWritesAndCanBeRetriedAfterReadFailure(t *testing.T) {
	for _, test := range []struct {
		name           string
		goal, actual   int32
		readErr        error
		status, reason string
		uncertain      bool
	}{
		{"matching running state", 1, 1, nil, "completed", "", false},
		{"matching stopped state", 0, 0, nil, "completed", "", false},
		{"known mismatch", 1, 2, nil, "failed", "observed_state_mismatch", false},
		{"failed read remains unknown", 1, 1, errors.New("API unavailable"), "failed", "observation_failed", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			slot := domain.Slot{ID: "slot", ProxyID: "edge", ExecutorID: "executor", Namespace: "soha", Name: "edge", UID: "uid", TemplateDigest: "digest", WorkloadKind: "StatefulSet", Members: []domain.Member{{InstanceID: "edge-0", RuntimeID: "p0", Ordinal: 0}, {InstanceID: "edge-1", RuntimeID: "p1", Ordinal: 1}, {InstanceID: "edge-2", RuntimeID: "p2", Ordinal: 2}}, MaxReplicas: 3}
			command := domain.Command{ProxyID: "edge", Slot: slot, Generation: 3, OwnerEpoch: 1, Action: "reconcile", Replicas: test.goal, Deadline: time.Now().Add(time.Minute)}
			task := delivery.ExecutionTask{ID: "new-read-task", TaskKind: domain.TaskKind, ProviderKind: slot.ProviderKind(), Status: "dispatching", ClaimedByAgentID: "executor", CallbackToken: "new-read-token", Payload: map[string]any{"proxyDeployment": command}}
			client := &fakeExecution{task: task, drain: domain.DrainStatus{AcceptingRequests: true, ActiveChecks: 100}}
			scaler := &fakeScale{initial: domain.Observation{UID: "uid", TemplateDigest: "digest", DesiredReplicas: test.actual, Replicas: test.actual, ReadyReplicas: test.actual, ObservedAt: time.Now()}, initialErr: test.readErr}
			worker, _ := NewWorker(client, scaler, "executor", []domain.Slot{slot})
			worker.Execute(context.Background(), task)
			if scaler.writes != 0 || scaler.observes != 1 {
				t.Fatalf("readonly action performed writes=%d reads=%d", scaler.writes, scaler.observes)
			}
			last := client.callbacks[len(client.callbacks)-1]
			var receipt domain.Receipt
			encoded, _ := json.Marshal(last.Payload["proxyDeployment"])
			_ = json.Unmarshal(encoded, &receipt)
			if last.Status != test.status || receipt.Reason != test.reason || receipt.Uncertain != test.uncertain || receipt.Drained {
				t.Fatalf("unexpected receipt: %s %+v", last.Status, receipt)
			}
			if test.readErr != nil {
				retryTask := task
				retryTask.ID, retryTask.CallbackToken = "another-read-task", "another-read-token"
				retryCommand := command
				retryCommand.Generation++
				retryTask.Payload = map[string]any{"proxyDeployment": retryCommand}
				client.task = retryTask
				scaler.initialErr = nil
				scaler.observes = 0
				worker.Execute(context.Background(), retryTask)
				last = client.callbacks[len(client.callbacks)-1]
				if last.Status != "completed" || last.CallbackToken != retryTask.CallbackToken || scaler.writes != 0 {
					t.Fatalf("new readonly task failed: %+v writes=%d", last, scaler.writes)
				}
			}
		})
	}
}
