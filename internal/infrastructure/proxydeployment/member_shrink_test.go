package proxydeployment

import (
	"context"
	"testing"
	"time"

	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
)

func TestShrinkWaitsOnlyForRemovedMembersBeforeScaling(t *testing.T) {
	for _, old := range []int32{2, 3} {
		t.Run(string(rune('0'+old))+" to 1", func(t *testing.T) {
			now := time.Now()
			slot, command, task := confirmTask(now)
			if old == 3 {
				slot.MaxReplicas = 3
				slot.Members = append(slot.Members, domain.Member{RuntimeID: "p2", InstanceID: "pool-2", Ordinal: 2})
			}
			command.Slot, command.Action, command.Replicas, command.DrainUntil, command.Deadline = slot, "scale", 1, now.Add(time.Minute), now.Add(150*time.Millisecond)
			task.Payload = map[string]any{"proxyDeployment": command}
			client := &fakeExecution{task: task, drain: domain.DrainStatus{Instances: int(old - 1), CountKnown: true, Ready: true}}
			scale := &fakeScale{initial: domain.Observation{Replicas: old, DesiredReplicas: old}, after: domain.Observation{Replicas: 1, DesiredReplicas: 1, ReadyReplicas: 1}}
			worker, err := NewWorker(client, scale, "exec", []domain.Slot{slot})
			if err != nil {
				t.Fatal(err)
			}
			worker.Execute(context.Background(), task)
			if scale.writes != 1 || client.callbacks[len(client.callbacks)-1].Status != "completed" {
				t.Fatalf("removed-member proof could not scale: writes=%d callbacks=%+v", scale.writes, client.callbacks)
			}
		})
	}
}

func TestShrinkInvalidRemovedProofNeverScales(t *testing.T) {
	for _, name := range []string{"unknown", "missing", "whole pool proof", "expired", "ordinary old ACK", "active", "accepting", "lost writer fence"} {
		t.Run(name, func(t *testing.T) {
			now := time.Now()
			slot, command, task := confirmTask(now)
			command.Action = "scale"
			command.Replicas = 1
			command.Deadline = now.Add(40 * time.Millisecond)
			task.Payload = map[string]any{"proxyDeployment": command}
			until := now.Add(time.Second)
			proof := domain.DrainStatus{Instances: 1, CountKnown: true, StopRequired: true, ConfirmUntil: &until}
			switch name {
			case "unknown":
				proof.CountKnown = false
			case "missing":
				proof.Instances = 0
			case "whole pool proof":
				proof.Instances = 2
			case "expired":
				until = now
			case "ordinary old ACK":
				proof.StopRequired = false
				proof.ConfirmUntil = nil
			case "active":
				proof.ActiveChecks = 1
			case "accepting":
				proof.AcceptingRequests = true
			}
			client := &fakeExecution{task: task, drain: proof}
			if name == "lost writer fence" {
				client.statusErr = context.Canceled
			}
			scale := &fakeScale{initial: domain.Observation{Replicas: 2, DesiredReplicas: 2}}
			worker, err := NewWorker(client, scale, "exec", []domain.Slot{slot})
			if err != nil {
				t.Fatal(err)
			}
			worker.Execute(context.Background(), task)
			if scale.writes != 0 {
				t.Fatal("invalid member proof wrote scale")
			}
		})
	}
}

func TestShrinkRechecksRemovedProofImmediatelyBeforeScale(t *testing.T) {
	now := time.Now()
	slot, command, task := confirmTask(now)
	command.Action = "scale"
	command.Replicas = 1
	command.DrainUntil = now.Add(time.Minute)
	task.Payload = map[string]any{"proxyDeployment": command}
	client := &confirmExecution{fakeExecution: fakeExecution{task: task}, stages: []domain.DrainStatus{{Instances: 1, CountKnown: true}, {Instances: 0, CountKnown: false}}}
	scale := &fakeScale{initial: domain.Observation{Replicas: 2, DesiredReplicas: 2}}
	worker, err := NewWorker(client, scale, "exec", []domain.Slot{slot})
	if err != nil {
		t.Fatal(err)
	}
	worker.Execute(context.Background(), task)
	if scale.writes != 0 || client.calls != 2 {
		t.Fatalf("stale proof scaled: writes=%d calls=%d", scale.writes, client.calls)
	}
}
