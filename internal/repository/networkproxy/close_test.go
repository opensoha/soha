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
)

func TestCloseRequestPersistsExactIdentityAndWindow(t *testing.T) {
	r, mock := commandTestRepository(t)
	ctx := context.Background()
	now := time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)
	input := domain.CloseInput{RequestID: "caller-id", ProcessEpoch: "epoch-1", IssuedAt: now.Add(-time.Nanosecond), ExpiresAt: now.Add(20*time.Second + time.Nanosecond)}
	caller, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	lock := func() {
		mock.ExpectBegin()
		mock.ExpectQuery(`SELECT .*network_proxy_instances.*FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"id", "process_epoch"}).AddRow("p1", "epoch-1"))
	}
	saved := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{"id", "instance_id", "connection_id", "created_by", "process_epoch", "status", "expires_at", "caller_input"}).
			AddRow(input.RequestID, "p1", "connection-1", "actor-1", "epoch-1", "pending", input.ExpiresAt.Truncate(time.Microsecond), caller)
	}
	lock()
	mock.ExpectQuery(`SELECT .*network_proxy_close_commands.*WHERE id=`).WithArgs(input.RequestID, 1).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectQuery(`SELECT .*network_proxy_connections.*process_epoch=.*observed_at>=`).
		WithArgs("p1", "epoch-1", now.Add(-time.Minute), 1).WillReturnRows(sqlmock.NewRows([]string{"connections"}).AddRow([]byte(`[{"id":"connection-1"}]`)))
	mock.ExpectExec(`INSERT INTO "network_proxy_close_commands".*"caller_input".*ON CONFLICT DO NOTHING`).
		WithArgs("epoch-1", input.RequestID, "p1", "connection-1", "pending", "", input.ExpiresAt, "actor-1", now, nil, string(caller)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	command, err := r.QueueCloseRequest(ctx, "p1", "connection-1", "actor-1", input, now)
	if err != nil || command.ID != input.RequestID || !command.ExpiresAt.Equal(input.ExpiresAt) {
		t.Fatalf("queued = %+v, error = %v", command, err)
	}
	lock()
	mock.ExpectQuery(`SELECT .*network_proxy_close_commands.*WHERE id=`).WithArgs(input.RequestID, 1).WillReturnRows(saved())
	mock.ExpectCommit()
	replayed, err := r.QueueCloseRequest(ctx, "p1", "connection-1", "actor-1", input, now)
	if err != nil || replayed != command {
		t.Fatalf("replayed = %+v, error = %v", replayed, err)
	}
	mock.ExpectQuery(`SELECT .*network_proxy_close_commands.*instance_id = .* AND id =`).WithArgs("p1", input.RequestID, 1).WillReturnRows(saved())
	read, err := r.GetClose(ctx, "p1", input.RequestID, now)
	if err != nil || read != command {
		t.Fatalf("receipt = %+v, error = %v", read, err)
	}
	for _, tt := range []struct {
		name, target, connection, actor string
		change                          func(*domain.CloseInput)
	}{
		{name: "exact expired replay", target: "p1", connection: "connection-1", actor: "actor-1"},
		{name: "changed actor", target: "p1", connection: "connection-1", actor: "actor-2"},
		{name: "changed target", target: "p2", connection: "connection-1", actor: "actor-1"},
		{name: "changed connection", target: "p1", connection: "connection-2", actor: "actor-1"},
		{name: "changed epoch", target: "p1", connection: "connection-1", actor: "actor-1", change: func(in *domain.CloseInput) { in.ProcessEpoch = "epoch-2" }},
		{name: "changed issue time", target: "p1", connection: "connection-1", actor: "actor-1", change: func(in *domain.CloseInput) { in.IssuedAt = in.IssuedAt.Add(time.Nanosecond) }},
		{name: "changed expiry", target: "p1", connection: "connection-1", actor: "actor-1", change: func(in *domain.CloseInput) { in.ExpiresAt = in.ExpiresAt.Add(time.Nanosecond) }},
	} {
		t.Run(tt.name, func(t *testing.T) {
			changed := input
			if tt.change != nil {
				tt.change(&changed)
			}
			mock.ExpectQuery(`SELECT .*network_proxy_close_commands.*WHERE id=`).WithArgs(input.RequestID, 1).WillReturnRows(saved())
			receipt, found, err := r.ReplayClose(ctx, tt.target, tt.connection, tt.actor, changed, now.Add(time.Minute))
			if !found || tt.name == "exact expired replay" && (err != nil || receipt.Status != "expired" || receipt.ID != input.RequestID) ||
				tt.name != "exact expired replay" && !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("receipt = %+v, found = %v, error = %v", receipt, found, err)
			}
		})
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
