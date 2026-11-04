package manifest

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestManifestSourceLifecycleMapsReferenceGuardConflicts(t *testing.T) {
	for _, state := range []string{"23001", "23503", "23514", "40001", "40P01"} {
		t.Run(state, func(t *testing.T) {
			repository, mock := newManifestRepository(t)
			mock.ExpectBegin()
			mock.ExpectExec(`DELETE FROM manifest_packages`).WithArgs("package").WillReturnError(&pgconn.PgError{Code: state})
			mock.ExpectRollback()
			if err := repository.Delete(context.Background(), "package"); !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("guard became non-conflict: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
	expected := errors.New("unrelated database failure")
	if referenceLifecycleError(expected) != expected || referenceLifecycleError(nil) != nil {
		t.Fatal("unrelated errors changed")
	}
}

func TestManifestSourceUpdateMapsReferenceConflicts(t *testing.T) {
	for _, state := range []string{"23514", "40001", "40P01"} {
		t.Run(state, func(t *testing.T) {
			repository, mock := newManifestRepository(t)
			mock.ExpectExec(`UPDATE manifest_sources`).WillReturnError(&pgconn.PgError{Code: state})
			_, err := repository.UpdateSource(context.Background(), "package", domainmanifest.SourceInput{})
			if !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("guard became non-conflict: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestManifestPackageUpdateMapsReferenceConflicts(t *testing.T) {
	for _, state := range []string{"23514", "40001", "40P01"} {
		t.Run(state, func(t *testing.T) {
			repository, mock := newManifestRepository(t)
			mock.ExpectBegin()
			mock.ExpectExec(`UPDATE manifest_packages`).WillReturnError(&pgconn.PgError{Code: state})
			mock.ExpectRollback()
			_, err := repository.Update(context.Background(), "package", domainmanifest.Package{})
			if !errors.Is(err, apperrors.ErrConflict) {
				t.Fatalf("guard became non-conflict: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
