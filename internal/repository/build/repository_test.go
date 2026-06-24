package build

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRepositoryGetWrapsErrNotFound(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	db, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}

	mock.ExpectQuery(`(?s)SELECT id, project_id, source_system, status, metadata, started_at, finished_at, created_at\s+FROM build_records\s+WHERE id = \$1\s+LIMIT 1`).
		WithArgs("missing-build").
		WillReturnError(sql.ErrNoRows)

	_, err = New(db).Get(context.Background(), "missing-build")
	if err == nil {
		t.Fatal("Get() error = nil, want ErrNotFound")
	}
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("Get() error = %v, want ErrNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRepositoryGetByExecutionTaskIDWrapsErrNotFound(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	db, err := gorm.Open(postgres.New(postgres.Config{
		Conn:                 sqlDB,
		PreferSimpleProtocol: true,
	}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm: %v", err)
	}

	mock.ExpectQuery(`(?s)SELECT id, project_id, source_system, status, metadata, started_at, finished_at, created_at\s+FROM build_records\s+WHERE metadata ->> 'executionTaskId' = \$1\s+ORDER BY created_at DESC\s+LIMIT 1`).
		WithArgs("task-1").
		WillReturnError(sql.ErrNoRows)

	_, err = New(db).GetByExecutionTaskID(context.Background(), "task-1")
	if err == nil {
		t.Fatal("GetByExecutionTaskID() error = nil, want ErrNotFound")
	}
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("GetByExecutionTaskID() error = %v, want ErrNotFound", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
