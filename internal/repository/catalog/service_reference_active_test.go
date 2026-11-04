package catalog

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestServiceReferenceLifecycleMapsRestrictConflict(t *testing.T) {
	for _, code := range []string{"23001", "23503", "23505", "23514", "40001", "40P01"} {
		t.Run(code, func(t *testing.T) {
			err := fmt.Errorf("wrapped database failure: %w", &pgconn.PgError{Code: code})
			if !errors.Is(referenceLifecycleError(err), apperrors.ErrConflict) {
				t.Fatalf("SQLSTATE %s was not a conflict", code)
			}
		})
	}
	unrelated := errors.New("database unavailable")
	if referenceLifecycleError(unrelated) != unrelated || referenceLifecycleError(nil) != nil {
		t.Fatal("unrelated error changed")
	}
	invalid := &pgconn.PgError{Code: "42601"}
	if referenceLifecycleError(invalid) != invalid {
		t.Fatal("unrelated SQLSTATE became conflict")
	}
}
