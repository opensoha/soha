package proxydeployment

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/domain/delivery"
	domain "github.com/opensoha/soha/internal/domain/proxydeployment"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// Like the shared ScaleClient, this fixture rejects a stale resourceVersion
// before any write. Drain changes only Ready/RV unless a test injects drift.
type revisionScaleFixture struct {
	current              domain.Observation
	reads, calls, writes int
	fault                string
}

func (s *revisionScaleFixture) Observe(ctx context.Context, _ domain.Slot) (domain.Observation, error) {
	s.reads++
	if s.reads == 2 && s.fault == "deadline" {
		<-ctx.Done()
		return domain.Observation{}, ctx.Err()
	}
	if s.reads == 2 && s.fault == "observe failed" {
		return domain.Observation{}, apperrors.ErrServiceUnavailable
	}
	return s.current, nil
}
func (s *revisionScaleFixture) Scale(_ context.Context, slot domain.Slot, observed domain.Observation, target int32) error {
	s.calls++
	if observed.ResourceVersion != s.current.ResourceVersion || observed.UID != slot.UID || observed.TemplateDigest != slot.TemplateDigest {
		return apperrors.ErrConflict
	}
	s.writes++
	s.current.DesiredReplicas, s.current.Replicas, s.current.ReadyReplicas = target, target, target
	s.current.ResourceVersion = "written-rv"
	return nil
}

type revisionExecutionFixture struct {
	fakeExecution
	scale *revisionScaleFixture
	fault string
}

func (f *revisionExecutionFixture) Status(ctx context.Context, id string) (delivery.ExecutionTask, error) {
	if f.statusCalls == 0 {
		f.scale.current.ResourceVersion = "drained-rv"
		f.scale.current.ReadyReplicas = 0
		switch f.fault {
		case "spec drift":
			f.scale.current.DesiredReplicas = 1
		case "actual drift":
			f.scale.current.Replicas = 1
		case "UID drift":
			f.scale.current.UID = "other-uid"
		case "template drift":
			f.scale.current.TemplateDigest = "other-template"
		case "missing RV":
			f.scale.current.ResourceVersion = ""
		}
	}
	if f.scale.reads >= 2 && f.scale.writes == 0 {
		switch f.fault {
		case "proof lost":
			f.drain.CountKnown = false
		case "task lost":
			f.statusErr = apperrors.ErrConflict
		case "confirmation expired":
			past := time.Now().Add(-time.Second)
			f.drain.StopRequired = true
			f.drain.ConfirmUntil = &past
		case "RV changed again":
			f.scale.current.ResourceVersion = "raced-rv"
		}
	}
	return f.fakeExecution.Status(ctx, id)
}

func executeRevisionFixture(t *testing.T, target int32, fault string) (*revisionScaleFixture, *revisionExecutionFixture, domain.Receipt) {
	t.Helper()
	now := time.Now()
	slot, command, task := confirmTask(now)
	command.Action, command.Replicas, command.DrainUntil = "scale", target, now.Add(time.Minute)
	if target == 0 {
		command.Action = "stop"
	}
	if fault == "deadline" {
		command.Deadline = now.Add(40 * time.Millisecond)
	}
	task.Payload = map[string]any{"proxyDeployment": command}
	scale := &revisionScaleFixture{fault: fault, current: domain.Observation{UID: slot.UID, TemplateDigest: slot.TemplateDigest, ResourceVersion: "initial-rv", Replicas: 2, DesiredReplicas: 2, ReadyReplicas: 2, ObservedAt: now}}
	client := &revisionExecutionFixture{fakeExecution: fakeExecution{task: task, drain: domain.DrainStatus{Instances: int(2 - target), CountKnown: true, Ready: true}}, scale: scale, fault: fault}
	w, err := NewWorker(client, scale, "exec", []domain.Slot{slot})
	if err != nil {
		t.Fatal(err)
	}
	w.Execute(context.Background(), task)
	var receipt domain.Receipt
	raw, _ := json.Marshal(client.callbacks[len(client.callbacks)-1].Payload["proxyDeployment"])
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	return scale, client, receipt
}

func TestScaleRefreshesResourceVersionAfterDrain(t *testing.T) {
	for _, target := range []int32{0, 1} {
		t.Run(map[int32]string{0: "stop", 1: "shrink"}[target], func(t *testing.T) {
			scale, client, receipt := executeRevisionFixture(t, target, "")
			if scale.writes != 1 || client.callbacks[len(client.callbacks)-1].Status != "completed" || receipt.Uncertain {
				t.Fatalf("Ready/RV-only drain prevented scale: writes=%d calls=%d receipt=%+v", scale.writes, scale.calls, receipt)
			}
		})
	}
}

func TestRefreshedScaleStillRejectsDriftAndInvalidProof(t *testing.T) {
	for _, fault := range []string{"spec drift", "actual drift", "UID drift", "template drift", "missing RV", "proof lost", "task lost", "confirmation expired", "deadline", "observe failed", "RV changed again"} {
		t.Run(fault, func(t *testing.T) {
			scale, client, receipt := executeRevisionFixture(t, 0, fault)
			if scale.writes != 0 || client.callbacks[len(client.callbacks)-1].Status == "completed" {
				t.Fatalf("unsafe scale: calls=%d writes=%d receipt=%+v", scale.calls, scale.writes, receipt)
			}
			if fault != "RV changed again" && scale.calls != 0 {
				t.Fatal("precondition rejection reached Scale")
			}
			if fault == "RV changed again" && (!receipt.Uncertain || receipt.Reason != "scale_result_unknown" || scale.calls != 1) {
				t.Fatal("strict post-refresh CAS failure was hidden or retried")
			}
			if (fault == "deadline" || fault == "observe failed") && !receipt.Uncertain {
				t.Fatal("failed observation was presented as certain")
			}
		})
	}
}
