package db

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestExecuteMigrationStatementResetsSession(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sql mock: %v", err)
	}
	defer sqlDB.Close()

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
	defer sqlDB.Close()

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
