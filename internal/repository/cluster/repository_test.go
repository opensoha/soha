package cluster

import (
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestErrNotFoundWrapsAppErrorSentinel(t *testing.T) {
	if !errors.Is(ErrNotFound, apperrors.ErrNotFound) {
		t.Fatalf("ErrNotFound should wrap apperrors.ErrNotFound")
	}
}

func TestCreateRegistrationConflictsBeforeWritingCredentials(t *testing.T) {
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	mock.ExpectBegin()
	mock.ExpectExec(`INSERT INTO clusters .* ON CONFLICT \(id\) DO NOTHING$`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	err = New(db).CreateRegistration(t.Context(), domaincluster.Connection{Summary: domaincluster.Summary{ID: "duplicate"}})
	if !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("duplicate create = %v, want conflict", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
