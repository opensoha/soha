package release

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

func TestRepositoryQueriesWrapErrNotFound(t *testing.T) {
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

	repository := New(db)
	tests := []struct {
		name  string
		query string
		arg   string
		get   func() error
	}{
		{"by id", `(?s)SELECT id, project_id, cluster_id, namespace, release_name, status, metadata, deployed_at, created_at\s+FROM deploy_records\s+WHERE id = \$1\s+LIMIT 1`, "missing-release", func() error { _, err := repository.Get(context.Background(), "missing-release"); return err }},
		{"by execution task", `(?s)SELECT id, project_id, cluster_id, namespace, release_name, status, metadata, deployed_at, created_at\s+FROM deploy_records\s+WHERE metadata ->> 'executionTaskId' = \$1\s+ORDER BY created_at DESC\s+LIMIT 1`, "task-1", func() error { _, err := repository.GetByExecutionTaskID(context.Background(), "task-1"); return err }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mock.ExpectQuery(test.query).WithArgs(test.arg).WillReturnError(sql.ErrNoRows)
			err := test.get()
			if !errors.Is(err, apperrors.ErrNotFound) {
				t.Fatalf("query error = %v, want ErrNotFound", err)
			}
		})
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
