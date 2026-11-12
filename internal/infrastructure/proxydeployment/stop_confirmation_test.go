package proxydeployment

import (
	"context"
	"encoding/json"
	"github.com/opensoha/soha/internal/domain/delivery"
	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
	"testing"
	"time"
)

type confirmExecution struct {
	fakeExecution
	calls  int
	stages []domain.DrainStatus
}

func (f *confirmExecution) Status(context.Context, string) (delivery.ExecutionTask, error) {
	f.calls++
	i := min(f.calls-1, len(f.stages)-1)
	task := f.task
	task.Result = map[string]any{"proxyDeploymentDrain": f.stages[i]}
	return task, f.statusErr
}
func confirmTask(now time.Time) (domain.Slot, domain.Command, delivery.ExecutionTask) {
	slot := domain.Slot{ID: "slot", ProxyID: "master", ExecutorID: "exec", Namespace: "soha", Name: "pool", UID: "uid", TemplateDigest: "digest", WorkloadKind: "StatefulSet", MaxReplicas: 2, Members: []domain.Member{{InstanceID: "pool-0", RuntimeID: "p0", Ordinal: 0}, {InstanceID: "pool-1", RuntimeID: "p1", Ordinal: 1}}}
	command := domain.Command{ProxyID: "master", Slot: slot, Generation: 2, OwnerEpoch: 1, Action: "stop", DrainTimeoutSeconds: 120, DrainUntil: now.Add(-time.Millisecond), Deadline: now.Add(6 * time.Second)}
	task := delivery.ExecutionTask{ID: "task", TaskKind: domain.TaskKind, ProviderKind: slot.ProviderKind(), ClaimedByAgentID: "exec", Status: "dispatching", CallbackToken: "token", Payload: map[string]any{"proxyDeployment": command}}
	return slot, command, task
}
func TestStopConfirmationAfterActiveDrainScalesOnceOnlyWithNewProof(t *testing.T) {
	now := time.Now()
	slot, command, task := confirmTask(now)
	confirmUntil := now.Add(5 * time.Second)
	client := &confirmExecution{fakeExecution: fakeExecution{task: task}, stages: []domain.DrainStatus{{Instances: 2, CountKnown: true, ActiveChecks: 3}, {Instances: 2, CountKnown: true, StopRequired: true, ConfirmUntil: &confirmUntil}}}
	scale := &fakeScale{initial: domain.Observation{Replicas: 2, DesiredReplicas: 2}, after: domain.Observation{Replicas: 0, DesiredReplicas: 0}}
	worker, err := NewWorker(client, scale, "exec", []domain.Slot{slot})
	if err != nil {
		t.Fatal(err)
	}
	worker.Execute(context.Background(), task)
	last := client.callbacks[len(client.callbacks)-1]
	var receipt domain.Receipt
	raw, _ := json.Marshal(last.Payload["proxyDeployment"])
	_ = json.Unmarshal(raw, &receipt)
	if client.calls < 2 || scale.writes != 1 || last.Status != "completed" || !receipt.Drained {
		t.Fatalf("active flow did not reach proof->single scale: calls=%d writes=%d receipt=%+v", client.calls, scale.writes, receipt)
	}
	if !scale.deadline.Equal(command.Deadline) {
		t.Fatal("confirmation extended original task deadline")
	}
}
func TestStopConfirmationUnknownOldACKAndExpiredProofNeverScale(t *testing.T) {
	for _, name := range []string{"unknown count", "old ordinary ACK", "expired confirmation", "missing member", "total deadline"} {
		t.Run(name, func(t *testing.T) {
			now := time.Now()
			slot, command, task := confirmTask(now)
			command.Deadline = now.Add(40 * time.Millisecond)
			task.Payload = map[string]any{"proxyDeployment": command}
			until := now.Add(time.Hour)
			proof := domain.DrainStatus{Instances: 2, CountKnown: true, StopRequired: true, ConfirmUntil: &until}
			switch name {
			case "unknown count", "total deadline":
				proof.CountKnown = false
			case "old ordinary ACK":
				proof.StopRequired = false
				proof.ConfirmUntil = nil
			case "expired confirmation":
				past := now.Add(-time.Millisecond)
				proof.ConfirmUntil = &past
			case "missing member":
				proof.Instances = 1
			}
			client := &confirmExecution{fakeExecution: fakeExecution{task: task}, stages: []domain.DrainStatus{proof}}
			scale := &fakeScale{initial: domain.Observation{Replicas: 2, DesiredReplicas: 2}}
			worker, err := NewWorker(client, scale, "exec", []domain.Slot{slot})
			if err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			worker.Execute(context.Background(), task)
			if scale.writes != 0 || time.Since(started) > time.Second {
				t.Fatal("unknown confirmation wrote scale or escaped original deadline")
			}
			last := client.callbacks[len(client.callbacks)-1]
			var receipt domain.Receipt
			raw, _ := json.Marshal(last.Payload["proxyDeployment"])
			_ = json.Unmarshal(raw, &receipt)
			if receipt.Drained || last.Status == "completed" {
				t.Fatal("missing stop proof treated as stopped")
			}
		})
	}
}
