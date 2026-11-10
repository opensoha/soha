package db

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestMigrateFromFileKeepsDistinctFilenamesAtSameVersion(t *testing.T) {
	for _, tc := range []struct {
		name    string
		applied string
	}{
		{name: "fresh"},
		{name: "main already applied", applied: "0120_endpoint_response.sql"},
		{name: "delivery already applied", applied: "0120_template_adoption.sql"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			main := filepath.Join(dir, "0120_endpoint_response.sql")
			delivery := filepath.Join(dir, "0120_template_adoption.sql")
			if err := os.WriteFile(delivery, []byte("SELECT 2"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(main, []byte("SELECT 1"), 0o600); err != nil {
				t.Fatal(err)
			}
			sqlDB, mock, err := sqlmock.New()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sqlDB.Close() })
			mock.ExpectExec(`CREATE TABLE IF NOT EXISTS public.schema_migrations`).WillReturnResult(sqlmock.NewResult(0, 0))
			for _, migration := range []struct{ filename, statement string }{{main, "SELECT 1"}, {delivery, "SELECT 2"}} {
				lookup := mock.ExpectQuery(`SELECT COUNT\(1\) FROM public.schema_migrations WHERE filename`).WithArgs(migration.filename)
				if filepath.Base(migration.filename) == tc.applied {
					lookup.WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(1))
					continue
				}
				lookup.WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
				mock.ExpectExec(migration.statement).WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec(`RESET ALL`).WillReturnResult(sqlmock.NewResult(0, 0))
				mock.ExpectExec(`INSERT INTO public.schema_migrations`).WithArgs(migration.filename, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
			}

			store := &Store{sqlDB: sqlDB}
			if err := store.MigrateFromFile(t.Context(), dir); err != nil {
				t.Fatal(err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestExecuteMigrationStatementResetsSession(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	store := &Store{sqlDB: sqlDB}
	mock.ExpectExec(`SET search_path = ''`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`RESET ALL`).WillReturnResult(sqlmock.NewResult(0, 0))

	if err := store.executeMigrationStatement(context.Background(), `SET search_path = ''`); err != nil {
		t.Fatalf("executeMigrationStatement() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestExecuteMigrationStatementResetsSessionOnFailure(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()

	store := &Store{sqlDB: sqlDB}
	mock.ExpectExec(`bad migration`).WillReturnError(errors.New("boom"))
	mock.ExpectExec(`ROLLBACK`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectExec(`RESET ALL`).WillReturnResult(sqlmock.NewResult(0, 0))

	if err := store.executeMigrationStatement(context.Background(), `bad migration`); err == nil {
		t.Fatal("executeMigrationStatement() error = nil, want error")
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}
