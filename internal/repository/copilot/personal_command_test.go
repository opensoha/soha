package copilot

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// The provider is a bounded stub; reservations, races and fences use PostgreSQL.
func TestPersonalCommandWithPostgres(t *testing.T) {
	f := newPersonalEnvironmentFixture(t)
	for _, test := range []struct {
		name string
		run  func(*testing.T, *personalEnvironmentFixture)
	}{
		{"once_only_and_request_identity", testPersonalCommandOnceOnly},
		{"current_owner_permission", testPersonalCommandOwnerPermission},
		{"whole_task_slot", testPersonalCommandTaskSlot},
		{"unknown_no_replay_or_new_command", testPersonalCommandUnknown},
		{"local_failure_restores_slot", testPersonalCommandNotDispatched},
		{"reservation_race", testPersonalCommandReservationRace},
		{"cancellation_fences_late_receipt", testPersonalCommandCancellation},
		{"cancellation_unknown_keeps_original_lease", testPersonalCommandCancellationUnknown},
		{"deadline_and_arguments", testPersonalCommandValidation},
	} {
		t.Run(test.name, func(t *testing.T) { f.t = t; test.run(t, f) })
	}
}

func commandInput() domain.PersonalCommandInput {
	return domain.PersonalCommandInput{TaskID: uuid.NewString(), IdempotencyKey: "command-1", Argv: []string{"/bin/echo", "fixture"}, TaskDeadline: time.Now().Add(time.Minute)}
}

func testPersonalCommandOnceOnly(t *testing.T, f *personalEnvironmentFixture) {
	f.wake(t, "command-once")
	input := commandInput()
	f.fileOutput = "fixture\n"
	before := len(f.calls)
	first, err := f.service.ExecutePersonalCommand(f.ctx, f.actor("command-once"), input)
	if err != nil || first.State != "succeeded" || first.Output != f.fileOutput || !strings.HasPrefix(first.OutputDigest, "sha256:") || first.ObservedAt == nil || len(f.calls) != before+1 || f.calls[before].TaskID != input.TaskID {
		t.Fatalf("command receipt: %+v %v", first, err)
	}
	repeated, err := f.service.ExecutePersonalCommand(f.ctx, f.actor("command-once"), input)
	if err != nil || repeated.ID != first.ID || repeated.Version != first.Version || len(f.calls) != before+1 {
		t.Fatal("repeated command dispatched again")
	}
	input.Argv[1] = "changed"
	if _, err := f.service.ExecutePersonalCommand(f.ctx, f.actor("command-once"), input); !errors.Is(err, apperrors.ErrConflict) || len(f.calls) != before+1 {
		t.Fatal("idempotency key accepted changed argv")
	}
}

func testPersonalCommandOwnerPermission(t *testing.T, f *personalEnvironmentFixture) {
	f.wake(t, "command-owner")
	command, err := f.service.ExecutePersonalCommand(f.ctx, f.actor("command-owner"), commandInput())
	if err != nil {
		t.Fatal(err)
	}
	f.wake(t, "command-foreign")
	before := len(f.calls)
	if _, err := f.service.GetPersonalCommand(f.ctx, f.actor("command-foreign"), command.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("foreign owner read command")
	}
	if _, err := f.service.CancelPersonalCommand(f.ctx, f.actor("command-foreign"), command.ID); !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatal("foreign owner canceled command")
	}
	f.scopeError = apperrors.ErrAccessDenied
	_, readErr := f.service.GetPersonalCommand(f.ctx, f.actor("command-owner"), command.ID)
	_, executeErr := f.service.ExecutePersonalCommand(f.ctx, f.actor("command-owner"), commandInput())
	f.scopeError = nil
	if !errors.Is(readErr, apperrors.ErrAccessDenied) || !errors.Is(executeErr, apperrors.ErrAccessDenied) || len(f.calls) != before {
		t.Fatal("revoked scope borrowed an old authorization")
	}
}

func testPersonalCommandTaskSlot(t *testing.T, f *personalEnvironmentFixture) {
	view := f.wake(t, "command-slot")
	input := commandInput()
	if _, err := f.service.ExecutePersonalCommand(f.ctx, f.actor("command-slot"), input); err != nil {
		t.Fatal(err)
	}
	before := len(f.calls)
	other := commandInput()
	other.IdempotencyKey = "other-task"
	if _, err := f.service.ExecutePersonalCommand(f.ctx, f.actor("command-slot"), other); !errors.Is(err, apperrors.ErrConflict) || len(f.calls) != before {
		t.Fatal("another task stole the sandbox")
	}
	input.IdempotencyKey = "same-task-next-command"
	if _, err := f.service.ExecutePersonalCommand(f.ctx, f.actor("command-slot"), input); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor("command-slot"), "sleep", "", "en-US"); err != nil {
		t.Fatal(err)
	}
	item, err := f.repo.GetPersonalEnvironment(f.ctx, "command-slot", view.AssistantID)
	if err != nil || item.ActiveTaskID != "" || !item.DataInitialized {
		t.Fatalf("sleep retained task slot or lost data: %+v %v", item, err)
	}
	f.wake(t, "command-slot")
	if _, err := f.service.ExecutePersonalCommand(f.ctx, f.actor("command-slot"), other); err != nil {
		t.Fatal(err)
	}
}

func testPersonalCommandUnknown(t *testing.T, f *personalEnvironmentFixture) {
	view := f.wake(t, "command-unknown")
	input := commandInput()
	f.providerError = errors.New("private transport detail")
	command, err := f.service.ExecutePersonalCommand(f.ctx, f.actor("command-unknown"), input)
	f.providerError = nil
	if err != nil || command.State != "unknown" || command.Output != "" {
		t.Fatalf("unknown receipt: %+v %v", command, err)
	}
	before := len(f.calls)
	if repeated, err := f.service.ExecutePersonalCommand(f.ctx, f.actor("command-unknown"), input); err != nil || repeated.ID != command.ID || len(f.calls) != before {
		t.Fatal("unknown command replayed")
	}
	f.allowReconcile(t, "command-unknown", view.AssistantID)
	if _, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor("command-unknown"), "reconcile", "", "en-US"); err != nil {
		t.Fatal(err)
	}
	input.IdempotencyKey = "new-command"
	before = len(f.calls)
	if _, err := f.service.ExecutePersonalCommand(f.ctx, f.actor("command-unknown"), input); !errors.Is(err, apperrors.ErrConflict) || len(f.calls) != before {
		t.Fatal("ready inspection was used as command completion")
	}
	if _, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor("command-unknown"), "sleep", "", "en-US"); err != nil {
		t.Fatal(err)
	}
	f.wake(t, "command-unknown")
	input.IdempotencyKey = "command-1"
	before = len(f.calls)
	if repeated, err := f.service.ExecutePersonalCommand(f.ctx, f.actor("command-unknown"), input); err != nil || repeated.State != "unknown" || len(f.calls) != before {
		t.Fatal("new lease replayed the old logical command")
	}
	input.IdempotencyKey = "new-lease-command"
	if _, err := f.service.ExecutePersonalCommand(f.ctx, f.actor("command-unknown"), input); err != nil {
		t.Fatal(err)
	}
}

func testPersonalCommandNotDispatched(t *testing.T, f *personalEnvironmentFixture) {
	view := f.wake(t, "command-local")
	input := commandInput()
	f.providerError = domain.ErrPersonalEnvironmentNotDispatched
	command, err := f.service.ExecutePersonalCommand(f.ctx, f.actor("command-local"), input)
	f.providerError = nil
	item, readErr := f.repo.GetPersonalEnvironment(f.ctx, "command-local", view.AssistantID)
	if err != nil || readErr != nil || command.State != "not_dispatched" || item.State != "ready" || item.ActiveTaskID != "" || item.OperationID != "" || !item.DataInitialized {
		t.Fatalf("local failure consumed sandbox: %+v %+v %v %v", command, item, err, readErr)
	}
	before := len(f.calls)
	if _, err := f.service.ExecutePersonalCommand(f.ctx, f.actor("command-local"), input); err != nil || len(f.calls) != before {
		t.Fatal("known unissued command replayed without a new identity")
	}
	input = commandInput()
	input.IdempotencyKey = "new-command"
	if _, err := f.service.ExecutePersonalCommand(f.ctx, f.actor("command-local"), input); err != nil {
		t.Fatal(err)
	}
}

func reserveCommand(t *testing.T, f *personalEnvironmentFixture, owner string) (domain.PersonalEnvironmentRecord, domain.PersonalEnvironmentRecord, domain.PersonalCommandRecord) {
	t.Helper()
	view := f.wake(t, owner)
	previous, err := f.repo.GetPersonalEnvironment(f.ctx, owner, view.AssistantID)
	if err != nil {
		t.Fatal(err)
	}
	candidate := domain.PersonalCommandRecord{PersonalCommand: domain.PersonalCommand{ID: uuid.NewString(), AssistantID: view.AssistantID, TaskID: uuid.NewString()}, CreatedBy: owner, RequestHash: "sha256:" + strings.Repeat("1", 64), Lease: previous.Lease, EnvOperationID: uuid.NewString()}
	reserved, command, fresh, err := f.repo.ReservePersonalCommand(f.ctx, previous, candidate)
	if err != nil || !fresh {
		t.Fatalf("reserve: %v", err)
	}
	return previous, reserved, command
}

func testPersonalCommandReservationRace(t *testing.T, f *personalEnvironmentFixture) {
	previous, reserved, command := reserveCommand(t, f, "command-race")
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, found, fresh, err := f.repo.ReservePersonalCommand(f.ctx, previous, command)
			if err == nil && (fresh || found.ID != command.ID) {
				err = errors.New("duplicate reservation dispatched")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	current, err := f.repo.GetPersonalEnvironment(f.ctx, "command-race", previous.AssistantID)
	if err != nil || current.Version != reserved.Version || current.OperationID != reserved.OperationID {
		t.Fatal("idempotent race mutated the reservation")
	}
}

func testPersonalCommandCancellation(t *testing.T, f *personalEnvironmentFixture) {
	previous, reserved, command := reserveCommand(t, f, "command-cancel")
	canceled, err := f.service.CancelPersonalCommand(f.ctx, f.actor("command-cancel"), command.ID)
	if err != nil || canceled.State != "canceled" || canceled.ObservedAt == nil || f.calls[len(f.calls)-1].Lease.ID != command.Lease.ID {
		t.Fatalf("original command cancellation: %+v %v", canceled, err)
	}
	reserved.State = "ready"
	command.State, command.Output = "succeeded", "late"
	if _, err := f.repo.CompletePersonalCommand(f.ctx, previous, reserved, command, false); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("late command receipt survived cancellation: %v", err)
	}
	item, err := f.repo.GetPersonalEnvironment(f.ctx, "command-cancel", previous.AssistantID)
	stored, readErr := f.repo.GetPersonalCommand(f.ctx, "command-cancel", command.ID)
	if err != nil || readErr != nil || item.State != "sleeping" || item.ActiveTaskID != "" || stored.State != "canceled" || stored.Output != "" {
		t.Fatal("late completion partially committed")
	}
}

func testPersonalCommandCancellationUnknown(t *testing.T, f *personalEnvironmentFixture) {
	_, reserved, command := reserveCommand(t, f, "command-cancel-unknown")
	f.providerError = errors.New("transport unavailable")
	canceled, err := f.service.CancelPersonalCommand(f.ctx, f.actor("command-cancel-unknown"), command.ID)
	f.providerError = nil
	if err != nil || canceled.State != "cancellation_unknown" || canceled.ObservedAt != nil {
		t.Fatalf("unproven cancellation marked complete: %+v %v", canceled, err)
	}
	f.allowReconcile(t, "command-cancel-unknown", reserved.AssistantID)
	if _, err := f.service.ChangePersonalEnvironment(f.ctx, f.actor("command-cancel-unknown"), "sleep", "", "en-US"); err != nil {
		t.Fatal(err)
	}
	f.wake(t, "command-cancel-unknown")
	before := len(f.calls)
	if _, err := f.service.CancelPersonalCommand(f.ctx, f.actor("command-cancel-unknown"), command.ID); !errors.Is(err, apperrors.ErrConflict) || len(f.calls) != before {
		t.Fatal("old command cancellation stopped a newer lease")
	}
}

func testPersonalCommandValidation(t *testing.T, f *personalEnvironmentFixture) {
	f.wake(t, "command-validation")
	before := len(f.calls)
	for _, mutate := range []func(*domain.PersonalCommandInput){
		func(i *domain.PersonalCommandInput) { i.TaskID = "../task" },
		func(i *domain.PersonalCommandInput) { i.IdempotencyKey = " key " },
		func(i *domain.PersonalCommandInput) { i.Argv = []string{"relative"} },
		func(i *domain.PersonalCommandInput) { i.TaskDeadline = time.Time{} },
		func(i *domain.PersonalCommandInput) { i.TaskDeadline = time.Now().Add(time.Hour) },
	} {
		input := commandInput()
		mutate(&input)
		if _, err := f.service.ExecutePersonalCommand(f.ctx, f.actor("command-validation"), input); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("invalid command: %v", err)
		}
	}
	input := commandInput()
	input.TaskDeadline = time.Now().Add(-time.Second)
	if _, err := f.service.ExecutePersonalCommand(f.ctx, f.actor("command-validation"), input); !errors.Is(err, apperrors.ErrConflict) || len(f.calls) != before {
		t.Fatal("expired task dispatched")
	}
}
