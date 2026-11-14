package networkruntime

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domain "github.com/opensoha/soha/internal/domain/networkruntime"
)

func TestPasswordBudgetPersistentAdmission(t *testing.T) {
	for _, scenario := range []string{"allowed", "locked", "inflight", "abandoned-fifth-failure", "source-rate"} {
		t.Run(scenario, func(t *testing.T) {
			db, mock := passwordTestDB(t)
			r := &Repository{db: db}
			now := time.Now().UTC().Truncate(time.Microsecond)
			attempt := domain.PasswordAttempt{ID: "attempt-1", RuntimeID: "runtime-1", NASID: "nas-1", Source: "192.0.2.1", SubjectID: "subject-1", Now: now}
			mock.ExpectBegin()
			mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectExec(`DELETE FROM network_password_budgets`).WillReturnResult(sqlmock.NewResult(0, 0))
			for _, kind := range []string{"nas", "source"} {
				mock.ExpectExec(`INSERT INTO network_password_budgets`).WillReturnResult(sqlmock.NewResult(0, 0))
				attempts := 0
				if kind == "source" && scenario == "source-rate" {
					attempts = 120
				}
				mock.ExpectQuery(`SELECT .*network_password_budgets`).WillReturnRows(sqlmock.NewRows([]string{"bucket_hash", "window_start", "attempts"}).AddRow("budget", now, attempts))
				if attempts < 120 {
					mock.ExpectExec(`UPDATE "network_password_budgets" SET`).WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			if scenario != "source-rate" {
				mock.ExpectExec(`INSERT INTO network_password_budgets`).WillReturnResult(sqlmock.NewResult(0, 0))
				var locked, active, activeID any
				failures := 0
				switch scenario {
				case "locked":
					locked = now.Add(time.Minute)
				case "inflight":
					active = now.Add(time.Second)
					activeID = "other"
				case "abandoned-fifth-failure":
					active = now.Add(-time.Second)
					activeID = "lost-worker"
					failures = 4
				}
				mock.ExpectQuery(`SELECT .*network_password_budgets`).WillReturnRows(sqlmock.NewRows([]string{"bucket_hash", "window_start", "failures", "locked_until", "active_until", "active_attempt_id"}).AddRow("subject", now, failures, locked, active, activeID))
				if scenario == "allowed" || scenario == "abandoned-fifth-failure" {
					mock.ExpectExec(`UPDATE "network_password_budgets" SET`).WillReturnResult(sqlmock.NewResult(0, 1))
				}
			}
			mock.ExpectCommit()
			allowed, err := r.BeginPasswordAttempt(context.Background(), attempt)
			if err != nil || allowed != (scenario == "allowed") {
				t.Fatalf("allowed=%v err=%v", allowed, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPasswordFailureLocksAfterFive(t *testing.T) {
	db, mock := passwordTestDB(t)
	r := &Repository{db: db}
	now := time.Now().UTC().Truncate(time.Microsecond)
	attempt := domain.PasswordAttempt{ID: "attempt-5", SubjectID: "subject-1"}
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT .*network_password_budgets`).WillReturnRows(sqlmock.NewRows([]string{"bucket_hash", "failures"}).AddRow(passwordBudgetHash("subject", attempt.SubjectID), 4))
	mock.ExpectExec(`UPDATE "network_password_budgets" SET`).WithArgs(nil, nil, now.Add(24*time.Hour), 5, now.Add(5*time.Minute), passwordBudgetHash("subject", attempt.SubjectID), attempt.ID).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	if err := r.FinishPasswordAttempt(context.Background(), attempt, false, now); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
