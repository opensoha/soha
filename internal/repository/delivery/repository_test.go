package delivery

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestClaimExecutionTaskClosesSelectedRowAndUpdatesTask(t *testing.T) {
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	repository := New(db)
	now := time.Now().UTC()
	rows := sqlmock.NewRows([]string{
		"id", "release_bundle_id", "application_id", "application_environment_id", "task_kind", "provider_kind", "target_kind", "status", "queue_key", "lock_key",
		"max_retries", "attempt_count", "timeout_seconds", "callback_token", "claimed_by_agent_id", "runtime_endpoint", "runtime_cluster_id", "stop_transport", "secret_refs", "secret_principal", "secret_target", "payload", "result", "started_at", "last_heartbeat_at", "last_runtime_seen_at", "finished_at", "created_at", "updated_at",
	}).AddRow(
		"task-1", nil, "app-1", nil, "manifest.apply", "manifest.direct", "deployment", "queued", "queue-1", "lock-1",
		3, 0, 300, "callback-1", nil, nil, nil, nil, []byte(`[]`), []byte(`{}`), []byte(`{}`), []byte(`{"action":"apply"}`), []byte(`{}`), nil, nil, nil, nil, now, now,
	)

	mock.ExpectBegin()
	mock.ExpectQuery(`(?s)SELECT id, release_bundle_id.*FOR UPDATE SKIP LOCKED`).
		WithArgs("manifest.direct").
		WillReturnRows(rows).
		RowsWillBeClosed()
	mock.ExpectExec(`(?s)UPDATE execution_tasks.*WHERE id =`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	task, err := repository.ClaimExecutionTask(context.Background(), []string{"manifest.direct"}, "worker-1", "local")
	if err != nil {
		t.Fatalf("ClaimExecutionTask() error = %v", err)
	}
	if task.ID != "task-1" || task.Status != "dispatching" || task.AttemptCount != 1 {
		t.Fatalf("ClaimExecutionTask() task = %#v", task)
	}
	if task.ClaimedByAgentID != "worker-1" || task.RuntimeEndpoint != "local" {
		t.Fatalf("ClaimExecutionTask() claim = agent %q endpoint %q", task.ClaimedByAgentID, task.RuntimeEndpoint)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
