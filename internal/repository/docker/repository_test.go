package docker

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestSafeTableNameAllowsKnownDockerTables(t *testing.T) {
	for _, tableName := range []string{
		"docker_hosts",
		"docker_projects",
		"docker_services",
		"docker_port_mappings",
		"docker_templates",
		"docker_operations",
	} {
		got, err := safeTableName(tableName)
		if err != nil {
			t.Fatalf("safeTableName(%q) error = %v", tableName, err)
		}
		if got != tableName {
			t.Fatalf("safeTableName(%q) = %q", tableName, got)
		}
	}
}

func TestSafeTableNameRejectsUnknownDockerTables(t *testing.T) {
	if _, err := safeTableName("docker_hosts; DROP TABLE docker_hosts"); err == nil {
		t.Fatal("safeTableName accepted unsafe table name")
	}
}

func TestErrNotFoundWrapsAppErrorSentinel(t *testing.T) {
	if !errors.Is(ErrNotFound, apperrors.ErrNotFound) {
		t.Fatal("ErrNotFound should wrap apperrors.ErrNotFound")
	}
}

func TestUpdateOperationRejectsStaleWrite(t *testing.T) {
	repository, mock := newDockerRepository(t)
	mock.ExpectExec(`UPDATE docker_operations[\s\S]+WHERE id = \$[0-9]+ AND updated_at = \$[0-9]+`).
		WillReturnResult(sqlmock.NewResult(0, 0))

	_, err := repository.UpdateOperation(context.Background(), domaindocker.Operation{
		ID:            "operation-1",
		OperationKind: "project_deploy",
		Status:        "running",
		UpdatedAt:     time.Date(2026, 8, 7, 8, 0, 0, 0, time.UTC),
	})
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("UpdateOperation() error = %v, want conflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestClaimOperationUpdatesSelectedOperation(t *testing.T) {
	repository, mock := newDockerRepository(t)
	now := time.Date(2026, 8, 8, 8, 0, 0, 0, time.UTC)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id,[\s\S]+FROM docker_operations[\s\S]+FOR UPDATE SKIP LOCKED`).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "host_id", "project_id", "service_id", "operation_kind", "status", "requested_by", "claimed_by_worker_id",
			"callback_token", "attempt_count", "max_retries", "timeout_seconds", "payload", "result", "started_at", "last_heartbeat_at", "finished_at", "created_at", "updated_at",
		}).AddRow("operation-1", "host-1", "project-1", "", "project_deploy", "queued", "user-1", "", "", 0, 1, 1800, []byte(`{}`), []byte(`{}`), nil, nil, nil, now, now))
	mock.ExpectExec(`UPDATE docker_operations[\s\S]+WHERE id = \$[0-9]+ AND`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	claimed, err := repository.ClaimOperation(context.Background(), "worker-1", "agent-1", []string{"host-1"}, []string{"project_deploy"}, "callback-token", now)
	if err != nil {
		t.Fatalf("ClaimOperation() error = %v", err)
	}
	if claimed.ID != "operation-1" || claimed.Status != "running" || claimed.ClaimedByWorkerID != "worker-1" || claimed.CallbackToken != "callback-token" {
		t.Fatalf("ClaimOperation() = %#v", claimed)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestConsumeHostAgentInstallTicketLocksAndConsumesOnce(t *testing.T) {
	repository, mock := newDockerRepository(t)
	now := time.Date(2026, 8, 15, 8, 0, 0, 0, time.UTC)
	expiresAt := now.Add(time.Minute)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT operation_id,[\s\S]+FROM docker_host_agent_installations WHERE operation_id = \$1 FOR UPDATE`).
		WithArgs("operation-1").
		WillReturnRows(sqlmock.NewRows([]string{
			"operation_id", "host_id", "download_token_hash", "download_expires_at", "downloaded_at",
			"enrollment_token_hash", "enrollment_expires_at", "enrolled_at", "agent_id",
			"agent_token_ciphertext", "runtime_token_hash", "revoked_at", "created_at", "updated_at",
		}).AddRow(
			"operation-1", "host-1", "download-hash", expiresAt, nil,
			"", nil, nil, "", "", "", nil, now, now,
		))
	mock.ExpectExec(`UPDATE docker_host_agent_installations[\s\S]+WHERE operation_id = \$[0-9]+ AND downloaded_at IS NULL`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	state, err := repository.ConsumeHostAgentInstallTicket(
		context.Background(), "operation-1", "download-hash", "enrollment-hash", now.Add(15*time.Minute), now,
	)
	if err != nil {
		t.Fatalf("ConsumeHostAgentInstallTicket() error = %v", err)
	}
	if state.DownloadedAt == nil || state.EnrollmentTokenHash != "enrollment-hash" || state.HostID != "host-1" {
		t.Fatalf("consumed installation = %#v", state)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func newDockerRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	return New(db), mock
}
