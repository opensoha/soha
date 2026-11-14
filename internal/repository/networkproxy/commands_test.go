package networkproxy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func commandTestRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	g, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{DisableAutomaticPing: true})
	if err != nil {
		t.Fatal(err)
	}
	return New(g), mock
}
func TestLateCommandResultCannotOverwrite(t *testing.T) {
	for _, epoch := range []string{"epoch-1", "epoch-2"} {
		t.Run(epoch, func(t *testing.T) {
			r, m := commandTestRepository(t)
			m.ExpectBegin()
			m.ExpectQuery(`SELECT .*network_proxy_instances.*FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"id", "process_epoch"}).AddRow("p1", epoch))
			if epoch == "epoch-1" {
				m.ExpectQuery(`SELECT .*network_proxy_runtime_commands.*kind=.*FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"id"}))
			}
			m.ExpectRollback()
			err := r.CompleteCommand(context.Background(), "p1", domain.RuntimeCommandResult{CommandID: "r1", RequestID: "r1", ProcessEpoch: "epoch-1", Status: "succeeded"}, time.Now())
			if !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("error=%v", err)
			}
			if err = m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCommandCompletionCanReplayOnlyExactTerminalResult(t *testing.T) {
	now := time.Date(2026, 11, 9, 12, 0, 0, 1234, time.UTC)
	input := domain.RuntimeCommandInput{RequestID: "r1", ProcessEpoch: "epoch-1", Kind: "select", NodeID: "n1", ExpiresAt: now.Add(time.Second)}
	stored, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, status string
		shift        time.Duration
		elapsed      time.Duration
		wantConflict bool
	}{
		{name: "first completion", status: "running"},
		{name: "exact terminal replay", status: "succeeded"},
		{name: "mismatched submicrosecond expiry", status: "succeeded", shift: time.Nanosecond, wantConflict: true},
		{name: "expired terminal receipt", status: "succeeded", elapsed: time.Second, wantConflict: true},
		{name: "cancelled expired or different result excluded by query", wantConflict: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r, m := commandTestRepository(t)
			result := domain.RuntimeCommandResult{CommandID: "r1", RequestID: "r1", ProcessEpoch: "epoch-1", Kind: "select", Status: "succeeded", ExpiresAt: input.ExpiresAt.Add(tt.shift), Result: &domain.CommandDataResult{SelectedNodeID: "n1"}}
			m.ExpectBegin()
			m.ExpectQuery(`SELECT .*network_proxy_instances.*FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"id", "process_epoch"}).AddRow("p1", "epoch-1"))
			rows := sqlmock.NewRows([]string{"id", "instance_id", "status", "input"})
			if tt.status != "" {
				rows.AddRow("r1", "p1", tt.status, stored)
			}
			m.ExpectQuery(`SELECT .*network_proxy_runtime_commands.*kind=.*status='running' OR .*status NOT IN \('cancelled','expired'\).*reason_code=.*result=.*FOR UPDATE`).
				WithArgs("p1", "r1", "r1", "epoch-1", "select", "succeeded", "", `{"selectedNodeId":"n1"}`, 1).
				WillReturnRows(rows)
			if tt.status == "running" {
				m.ExpectExec(`UPDATE .*network_proxy_runtime_commands.*status='running'.*"id".*"instance_id"`).WillReturnResult(sqlmock.NewResult(0, 1))
			}
			if tt.wantConflict {
				m.ExpectRollback()
			} else {
				m.ExpectCommit()
			}
			if err := r.CompleteCommand(context.Background(), "p1", result, now.Add(tt.elapsed)); tt.wantConflict && !errors.Is(err, apperrors.ErrConflict) || !tt.wantConflict && err != nil {
				t.Fatalf("error = %v, want conflict = %v", err, tt.wantConflict)
			}
			if err := m.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
func TestEpochClaimDoesNotConsumeNewAttempt(t *testing.T) {
	r, m := commandTestRepository(t)
	m.ExpectBegin()
	m.ExpectQuery(`SELECT .*network_proxy_instances.*FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"id", "process_epoch"}).AddRow("p1", "new"))
	m.ExpectRollback()
	if _, err := r.ClaimCommand(context.Background(), "p1", "old", time.Now()); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal(err)
	}
	if err := m.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
func TestReceiptExpiresWithoutInventingSuccess(t *testing.T) {
	now := time.Now()
	r := commandRow{ID: "r", Kind: "select", Status: "running", ExpiresAt: now}
	if got := r.receipt(now); got.Status != "expired" || got.Result != nil {
		t.Fatalf("receipt=%+v", got)
	}
}
