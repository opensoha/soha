package outpostruntime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha/internal/application/access"
	app "github.com/opensoha/soha/internal/application/outpostruntime"
	"github.com/opensoha/soha/internal/domain/delivery"
	"github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/outpostruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestUncertainReceiptWithObservationCannotBecomeKnownFailed(t *testing.T) {
	actual := int32(2)
	state := domain.State{Owner: "soha", UID: "uid", Generation: 2, ObservedGeneration: 1, DesiredReplicas: 1, ActualReplicas: &actual, LatestTaskID: "task", Phase: "queued"}
	command := domain.Command{Generation: 2, OwnerEpoch: 1, Action: "scale", Slot: domain.Slot{TemplateDigest: "digest"}}
	applyResult(&state, delivery.ExecutionTask{Status: "failed"}, command, &domain.Receipt{Uncertain: true, Reason: "scale_result_unknown", Observation: &domain.Observation{UID: "uid", TemplateDigest: "digest", DesiredReplicas: 1, Replicas: 1, ReadyReplicas: 1, ObservedAt: time.Now()}})
	if state.Phase != "unknown" || state.ObservedGeneration != 1 || !state.DrainHeld || !state.RequiresDrain() {
		t.Fatalf("uncertain result lost its fence: %+v", state)
	}
}

func TestReconcileRecordsKnownMismatchAndReadFailuresRemainUnknown(t *testing.T) {
	for _, test := range []struct {
		name            string
		goal, actual    int32
		held, uncertain bool
		phase, reason   string
		observed        int64
	}{
		{"stopped observation", 0, 0, true, false, "converged", "", 3},
		{"nonzero match retains held drain", 1, 1, true, false, "degraded", "drain_held", 3},
		{"known mismatch", 1, 2, false, false, "degraded", "observed_state_mismatch", 3},
		{"read failure remains unknown", 1, 0, true, true, "unknown", "observation_failed", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := domain.State{Owner: "soha", UID: "uid", Generation: 3, ObservedGeneration: 1, DesiredReplicas: test.goal, DrainHeld: test.held, LatestTaskID: "task", Phase: "queued"}
			command := domain.Command{Generation: 3, Action: "reconcile", Slot: domain.Slot{TemplateDigest: "digest"}}
			receipt := &domain.Receipt{Uncertain: test.uncertain}
			if test.uncertain {
				receipt.Reason = "observation_failed"
			} else {
				receipt.Observation = &domain.Observation{UID: "uid", TemplateDigest: "digest", DesiredReplicas: test.actual, Replicas: test.actual, ReadyReplicas: test.actual, ObservedAt: time.Now()}
			}
			applyResult(&state, delivery.ExecutionTask{Status: "failed"}, command, receipt)
			if state.Phase != test.phase || state.Reason != test.reason || state.ObservedGeneration != test.observed || state.DesiredReplicas != test.goal || state.DrainHeld != test.held {
				t.Fatalf("unexpected reconciliation: %+v", state)
			}
		})
	}
}

func TestUnknownOutcomeRejectsNewWritesAndBinding(t *testing.T) {
	for _, action := range []string{"start", "scale", "stop", "bind"} {
		t.Run(action, func(t *testing.T) {
			mutation := app.Mutation{State: domain.State{OutpostID: "edge"}}
			if action != "bind" {
				command := domain.Command{OutpostID: "edge", Slot: domain.Slot{UID: "uid", ExecutorID: "executor"}, Generation: 2, Action: action}
				mutation.Task = &delivery.ExecutionTask{TaskKind: domain.TaskKind, ProviderKind: command.Slot.ProviderKind(), Payload: map[string]any{"outpostRuntime": command}}
			}
			if err := validateNextMutation(domain.State{Phase: "unknown"}, mutation); !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("unknown %s accepted: %v", action, err)
			}
		})
	}
}

func TestReconcilePreservesTransactionCurrentGoalAndDrain(t *testing.T) {
	actual := int32(3)
	current := domain.State{OutpostID: "edge", Owner: "soha", UID: "uid", Generation: 1, DesiredReplicas: 2, ActualReplicas: &actual, LatestTaskID: "old", Phase: "unknown"}
	command := domain.Command{OutpostID: "edge", Generation: 2, Action: "reconcile", Replicas: 1, Slot: domain.Slot{UID: "uid", ExecutorID: "executor"}}
	mutation := app.Mutation{State: domain.State{Generation: 2, DesiredReplicas: 1}, Task: &delivery.ExecutionTask{ID: "new-read", TaskKind: domain.TaskKind, ProviderKind: command.Slot.ProviderKind(), Payload: map[string]any{"outpostRuntime": command}}}
	prepareReconciliation(current, &mutation)
	frozen, err := app.DecodeCommand(*mutation.Task)
	if err != nil || mutation.State.DesiredReplicas != 2 || !mutation.State.DrainHeld || mutation.State.ActualReplicas == nil || *mutation.State.ActualReplicas != 3 || frozen.Replicas != 2 {
		t.Fatalf("stale pre-read changed locked intent: %+v %+v %v", mutation.State, frozen, err)
	}
}

type recoveryRepository struct {
	app.Repository
	state domain.State
	task  *delivery.ExecutionTask
}

func (r *recoveryRepository) Get(context.Context, string) (domain.State, error) { return r.state, nil }
func (r *recoveryRepository) Commit(_ context.Context, m app.Mutation) (domain.State, error) {
	if r.state.Generation != m.ExpectedGeneration {
		return domain.State{}, apperrors.ErrConflict
	}
	if err := validateNextMutation(r.state, m); err != nil {
		return domain.State{}, err
	}
	prepareReconciliation(r.state, &m)
	r.state, r.task = m.State, m.Task
	return r.state, nil
}
func (r *recoveryRepository) RecordResult(_ context.Context, task delivery.ExecutionTask, _ domain.Command) error {
	projectTerminalTask(&r.state, task)
	return nil
}

type recoveryPermissionReader struct{}

func (recoveryPermissionReader) ListRolePermissions(context.Context) (map[string][]string, error) {
	return map[string][]string{"recovery-operator": {"identity.outposts.manage"}}, nil
}

type recoveryTasks struct{ app.ExecutionTasks }

func TestVerifiedReconcileThenExplicitStartReleasesHeldDrain(t *testing.T) {
	actual := int32(1)
	slot := domain.Slot{ID: "slot", OutpostID: "edge", ExecutorID: "executor", Namespace: "soha", Name: "edge", UID: "uid", TemplateDigest: "digest", MaxReplicas: 3}
	repo := &recoveryRepository{state: domain.State{OutpostID: "edge", SlotID: "slot", Owner: "soha", OwnerEpoch: 1, UID: "uid", Generation: 1, DesiredReplicas: 1, ActualReplicas: &actual, LatestTaskID: "old-task", Phase: "unknown", DrainHeld: true}}
	service, err := app.New(repo, access.NewPermissionResolver(recoveryPermissionReader{}), []domain.Slot{slot}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = app.NewExecutorService(service, recoveryTasks{}, []app.ExecutorCredential{{ExecutorID: "executor", Token: strings.Repeat("x", 32)}})
	if err != nil {
		t.Fatal(err)
	}
	principal := identity.Principal{UserID: "operator", Roles: []string{"recovery-operator"}}
	if _, err = service.Control(context.Background(), principal, "edge", domain.ControlInput{Action: "start", ExpectedGeneration: 1, RequestID: "blind-retry"}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("blind write accepted: %v", err)
	}
	state, err := service.Control(context.Background(), principal, "edge", domain.ControlInput{Action: "reconcile", ExpectedGeneration: 1, RequestID: "read"})
	if err != nil || state.DesiredReplicas != 1 || !state.DrainHeld {
		t.Fatalf("reconcile changed intent: %+v %v", state, err)
	}
	readTask := *repo.task
	readTask.Status = "completed"
	readTask.Result = map[string]any{"outpostRuntime": domain.Receipt{Generation: 2, OwnerEpoch: 1, Observation: &domain.Observation{UID: "uid", TemplateDigest: "digest", DesiredReplicas: 1, Replicas: 1, ReadyReplicas: 1, ObservedAt: time.Now()}}}
	if err = service.RecordExecutionTaskResult(context.Background(), readTask); err != nil {
		t.Fatal(err)
	}
	if repo.state.Phase != "degraded" || repo.state.Reason != "drain_held" || repo.state.ObservedGeneration != 2 || !repo.state.RequiresDrain() {
		t.Fatalf("read released the gate: %+v", repo.state)
	}
	state, err = service.Control(context.Background(), principal, "edge", domain.ControlInput{Action: "start", ExpectedGeneration: 2, RequestID: "confirmed-start"})
	if err != nil || state.DrainHeld || state.RequiresDrain() || state.LatestTaskID == readTask.ID {
		t.Fatalf("explicit start could not resume: %+v %v", state, err)
	}
}
