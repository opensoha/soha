package proxydeployment

import (
	"context"
	"github.com/opensoha/soha/internal/domain/delivery"
	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
	"testing"
	"time"
)

func TestWorkerCanonicalPinIgnoresTakeoverEvidenceButRejectsChangedTemplate(t *testing.T) {
	for _, changed := range []bool{false, true} {
		slot := domain.Slot{ID: "slot", ProxyID: "edge", ExecutorID: "exec", Namespace: "soha", Name: "edge", UID: "uid", TemplateDigest: "digest", MaxReplicas: 1, Members: []domain.Member{{InstanceID: "edge-0", RuntimeID: "edge"}}}
		commandSlot := slot
		commandSlot.AllowAdoption = true
		commandSlot.PreviousOwnerStopped = true
		if changed {
			commandSlot.TemplateDigest = "new-template"
		}
		task := delivery.ExecutionTask{ID: "task", TaskKind: domain.TaskKind, ProviderKind: slot.ProviderKind(), Status: "dispatching", ClaimedByAgentID: "exec", CallbackToken: "token", Payload: map[string]any{"proxyDeployment": domain.Command{ProxyID: "edge", Slot: commandSlot, Generation: 2, Action: "start", Replicas: 1, Deadline: time.Now().Add(time.Second)}}}
		client := &fakeExecution{task: task, drain: domain.DrainStatus{Instances: 1, CountKnown: true, Ready: true}}
		scale := &fakeScale{initial: domain.Observation{DesiredReplicas: 1, Replicas: 1, ReadyReplicas: 1}}
		worker, err := NewWorker(client, scale, "exec", []domain.Slot{slot})
		if err != nil {
			t.Fatal(err)
		}
		worker.Execute(context.Background(), task)
		if changed && scale.observes != 0 || !changed && scale.observes != 1 {
			t.Fatalf("wrong canonical pin fence: changed=%t observes=%d", changed, scale.observes)
		}
	}
}
