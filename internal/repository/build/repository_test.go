package build

import (
	"context"
	"database/sql"
	"errors"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	"regexp"
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
		{"by id", `(?s)SELECT id, project_id, source_system, status, metadata, started_at, finished_at, created_at\s+FROM build_records\s+WHERE id = \$1\s+LIMIT 1`, "missing-build", func() error { _, err := repository.Get(context.Background(), "missing-build"); return err }},
		{"by execution task", `(?s)SELECT id, project_id, source_system, status, metadata, started_at, finished_at, created_at\s+FROM build_records\s+WHERE metadata ->> 'executionTaskId' = \$1\s+ORDER BY created_at DESC\s+LIMIT 1`, "task-1", func() error { _, err := repository.GetByExecutionTaskID(context.Background(), "task-1"); return err }},
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

func TestDefinitionFilterPrecedesBuildLimit(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = sqlDB.Close() }()
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB, PreferSimpleProtocol: true}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	query := `SELECT id, project_id, source_system, status, metadata, started_at, finished_at, created_at FROM build_records WHERE TRUE AND project_id = $1 AND COALESCE(NULLIF(metadata->>'buildSourceId', ''), 'default:' || project_id) = $2 ORDER BY created_at DESC, id DESC LIMIT $3`
	mock.ExpectQuery(regexp.QuoteMeta(query)).WithArgs("app", "default:app", 10).WillReturnRows(sqlmock.NewRows([]string{"id"}))
	if _, err := New(db).List(context.Background(), domainbuild.Filter{ApplicationID: "app", BuildSourceID: "default:app", Limit: 10}); err != nil {
		t.Fatal(err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
