package application

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestDeleteServiceMapsActiveReferenceDependency(t *testing.T) {
	for _, code := range []string{"23001", "23503"} {
		t.Run(code, func(t *testing.T) {
			repository, mock := newApplicationRepository(t)
			original := &pgconn.PgError{Code: code, ConstraintName: "environment_service_references_service_id_fkey"}
			mock.ExpectExec(`DELETE FROM application_services`).WithArgs("app", "service").WillReturnError(original)
			err := repository.DeleteService(context.Background(), "app", "service")
			if !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("active reference deletion became non-conflict: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDeleteServicePreservesUnrelatedErrorsAndSuccessfulCleanup(t *testing.T) {
	t.Run("unrelated", func(t *testing.T) {
		repository, mock := newApplicationRepository(t)
		original := &pgconn.PgError{Code: "23503", ConstraintName: "unrelated_dependency"}
		mock.ExpectExec(`DELETE FROM application_services`).WithArgs("app", "service").WillReturnError(original)
		err := repository.DeleteService(context.Background(), "app", "service")
		if !errors.Is(err, original) || errors.Is(err, apperrors.ErrConflict) {
			t.Fatalf("unrelated semantics changed: %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("detached", func(t *testing.T) {
		repository, mock := newApplicationRepository(t)
		mock.ExpectExec(`DELETE FROM application_services`).WithArgs("app", "service").WillReturnResult(sqlmock.NewResult(0, 1))
		if err := repository.DeleteService(context.Background(), "app", "service"); err != nil {
			t.Fatal(err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatal(err)
		}
	})
}
