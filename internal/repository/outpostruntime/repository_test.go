package outpostruntime

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	app "github.com/opensoha/soha/internal/application/outpostruntime"
	"github.com/opensoha/soha/internal/domain/delivery"
	domain "github.com/opensoha/soha/internal/domain/outpostruntime"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func sqlRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return New(db), mock
}

func expectLock(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id FROM identity_outposts.*FOR UPDATE`).WithArgs("edge").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("edge"))
}
func expectCurrent(mock sqlmock.Sqlmock, generation int64) {
	mock.ExpectQuery(`SELECT id FROM identity_outposts WHERE id =`).WithArgs("edge").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("edge"))
	encoded, _ := json.Marshal(domain.State{OutpostID: "edge", Owner: "soha", SlotID: "slot", UID: "uid", Generation: generation})
	mock.ExpectQuery(`SELECT state FROM identity_outpost_runtime_deployments`).WithArgs("edge").WillReturnRows(sqlmock.NewRows([]string{"state"}).AddRow(encoded))
}

func TestCommitCASAndPersistentReplay(t *testing.T) {
	for _, test := range []struct {
		name        string
		replay      bool
		fingerprint string
		current     int64
		wantErr     bool
	}{
		{"target and task commit", false, "same", 1, false},
		{"stale generation", false, "same", 2, true},
		{"same request replays original", true, "same", 0, false},
		{"request reuse conflicts", true, "different", 0, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo, mock := sqlRepository(t)
			state := domain.State{OutpostID: "edge", Owner: "soha", SlotID: "slot", UID: "uid", Generation: 2, LatestTaskID: "task"}
			mutation := app.Mutation{RequestID: "request", Fingerprint: "same", ExpectedGeneration: 1, State: state, Task: &delivery.ExecutionTask{ID: "task", TaskKind: domain.TaskKind, ProviderKind: "outpost_runtime.executor", Payload: map[string]any{}}}
			expectLock(mock)
			query := mock.ExpectQuery(`SELECT fingerprint,response FROM identity_outpost_runtime_requests`).WithArgs("edge", "request")
			if test.replay {
				encoded, _ := json.Marshal(state)
				query.WillReturnRows(sqlmock.NewRows([]string{"fingerprint", "response"}).AddRow(test.fingerprint, encoded))
			} else {
				query.WillReturnRows(sqlmock.NewRows([]string{"fingerprint", "response"}))
				expectCurrent(mock, test.current)
				if !test.wantErr {
					mock.ExpectExec(`INSERT INTO execution_tasks`).WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectExec(`INSERT INTO identity_outpost_runtime_deployments`).WillReturnResult(sqlmock.NewResult(0, 1))
					mock.ExpectExec(`INSERT INTO identity_outpost_runtime_requests`).WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			if test.wantErr {
				mock.ExpectRollback()
			} else {
				mock.ExpectCommit()
			}
			got, err := repo.Commit(context.Background(), mutation)
			if test.wantErr {
				if !errors.Is(err, apperrors.ErrConflict) {
					t.Fatalf("got %v", err)
				}
			} else if err != nil || got.LatestTaskID != "task" {
				t.Fatalf("got %+v %v", got, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCommitRollsBackTaskWhenTargetPersistenceFails(t *testing.T) {
	repo, mock := sqlRepository(t)
	expectLock(mock)
	mock.ExpectQuery(`SELECT fingerprint,response`).WillReturnRows(sqlmock.NewRows([]string{"fingerprint", "response"}))
	expectCurrent(mock, 1)
	mock.ExpectExec(`INSERT INTO execution_tasks`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO identity_outpost_runtime_deployments`).WillReturnError(errors.New("storage unavailable"))
	mock.ExpectRollback()
	_, err := repo.Commit(context.Background(), app.Mutation{RequestID: "r", Fingerprint: "f", ExpectedGeneration: 1, State: domain.State{OutpostID: "edge", SlotID: "slot", Generation: 2}, Task: &delivery.ExecutionTask{ID: "task", Payload: map[string]any{}}})
	if err == nil {
		t.Fatal("expected rollback")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestLateOrUncertainReceiptDoesNotInventConvergence(t *testing.T) {
	state := domain.State{UID: "uid", Generation: 2, OwnerEpoch: 1, DesiredReplicas: 0}
	command := domain.Command{Generation: 2, OwnerEpoch: 1, Slot: domain.Slot{TemplateDigest: "digest"}}
	applyResult(&state, delivery.ExecutionTask{Status: "failed"}, command, &domain.Receipt{Generation: 2, OwnerEpoch: 1, Uncertain: true, Reason: "scale_result_unknown"})
	if state.ActualReplicas != nil || state.Phase != "unknown" || state.ObservedGeneration != 0 {
		t.Fatalf("invented observation: %+v", state)
	}
	applyResult(&state, delivery.ExecutionTask{Status: "completed"}, command, &domain.Receipt{Observation: &domain.Observation{UID: "replacement", TemplateDigest: "digest"}, Drained: true})
	if state.Phase != "conflict" || state.ActualReplicas != nil {
		t.Fatalf("accepted replaced workload: %+v", state)
	}
}

type projectedUnknown struct{}

func (projectedUnknown) Match(value driver.Value) bool {
	encoded, ok := value.(string)
	var state domain.State
	return ok && json.Unmarshal([]byte(encoded), &state) == nil &&
		state.Phase == "unknown" && state.Reason == "scale_result_unknown" &&
		state.ProjectedTaskID == "previous" && state.DrainHeld && state.Generation == 2
}

func expectUncertainTerminal(mock sqlmock.Sqlmock) domain.Command {
	command := domain.Command{OutpostID: "edge", Generation: 2, OwnerEpoch: 1, Action: "stop", Slot: domain.Slot{ID: "slot", UID: "uid", ExecutorID: "executor"}}
	payload, _ := json.Marshal(map[string]any{"outpostRuntime": command})
	result, _ := json.Marshal(map[string]any{"outpostRuntime": domain.Receipt{Generation: 2, OwnerEpoch: 1, Uncertain: true, Reason: "scale_result_unknown"}})
	mock.ExpectQuery(`SELECT id, task_kind, provider_kind, status, payload, result FROM execution_tasks`).WithArgs("previous").
		WillReturnRows(sqlmock.NewRows([]string{"id", "task_kind", "provider_kind", "status", "payload", "result"}).
			AddRow("previous", domain.TaskKind, command.Slot.ProviderKind(), "failed", payload, result))
	return command
}

func expectPendingState(mock sqlmock.Sqlmock, phase string) {
	state := domain.State{OutpostID: "edge", SlotID: "slot", Owner: "soha", UID: "uid", Generation: 2, OwnerEpoch: 1, LatestTaskID: "previous", Phase: phase}
	if phase != "queued" {
		state.ProjectedTaskID = "previous"
	}
	encoded, _ := json.Marshal(state)
	mock.ExpectQuery(`SELECT id FROM identity_outposts WHERE id =`).WithArgs("edge").WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow("edge"))
	mock.ExpectQuery(`SELECT state FROM identity_outpost_runtime_deployments`).WithArgs("edge").WillReturnRows(sqlmock.NewRows([]string{"state"}).AddRow(encoded))
}

func TestCommitRepairsUncertainTerminalBeforeRejectingNewWrite(t *testing.T) {
	repo, mock := sqlRepository(t)
	expectLock(mock)
	mock.ExpectQuery(`SELECT fingerprint,response`).WillReturnRows(sqlmock.NewRows([]string{"fingerprint", "response"}))
	expectPendingState(mock, "queued")
	command := expectUncertainTerminal(mock)
	mock.ExpectExec(`INSERT INTO identity_outpost_runtime_deployments`).WithArgs("edge", "slot", int64(2), projectedUnknown{}).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	command.Action, command.Generation = "start", 3
	task := &delivery.ExecutionTask{ID: "new", TaskKind: domain.TaskKind, ProviderKind: command.Slot.ProviderKind(), Payload: map[string]any{"outpostRuntime": command}}
	_, err := repo.Commit(context.Background(), app.Mutation{State: domain.State{OutpostID: "edge", Generation: 3}, ExpectedGeneration: 2, RequestID: "new", Task: task})
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("new mutation crossed the uncertain fence: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestDelayedOutpostSinkReadsAuthoritativeTerminalResult(t *testing.T) {
	repo, mock := sqlRepository(t)
	expectLock(mock)
	expectPendingState(mock, "queued")
	command := expectUncertainTerminal(mock)
	mock.ExpectExec(`INSERT INTO identity_outpost_runtime_deployments`).WithArgs("edge", "slot", int64(2), projectedUnknown{}).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	expectLock(mock)
	expectPendingState(mock, "unknown")
	mock.ExpectCommit()

	for range 2 {
		err := repo.RecordResult(context.Background(), delivery.ExecutionTask{ID: "previous", Status: "running"}, command)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestOutpostBindingCannotOverwriteAlreadyProjectedResult(t *testing.T) {
	repo, mock := sqlRepository(t)
	expectLock(mock)
	mock.ExpectQuery(`SELECT fingerprint,response`).WillReturnRows(sqlmock.NewRows([]string{"fingerprint", "response"}))
	// The binding was built from a queued pre-read. Before the transaction
	// takes the owner lock, the sink projects a known result at the same generation.
	expectPendingState(mock, "converged")
	mock.ExpectRollback()

	binding := domain.State{OutpostID: "edge", SlotID: "slot", Owner: "soha", UID: "uid", Generation: 3, OwnerEpoch: 1, LatestTaskID: "previous", Phase: "unobserved"}
	_, err := repo.Commit(context.Background(), app.Mutation{State: binding, ExpectedGeneration: 2, RequestID: "rebind"})
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("binding overwrote the projected result: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
