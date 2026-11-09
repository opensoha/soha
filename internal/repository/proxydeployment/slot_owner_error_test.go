package proxydeployment

import (
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"testing"
)

func TestRuntimeSlotOwnerConflictMapping(t *testing.T) {
	target := fmt.Errorf("save parent: %w", &pgconn.PgError{Code: "23505", ConstraintName: "runtime_slot_owner_conflict", Message: "runtime_slot_owner_conflict"})
	if !errors.Is(runtimeSlotOwnerError(target), apperrors.ErrConflict) {
		t.Fatal("parent UID conflict not mapped")
	}
	for _, e := range []error{&pgconn.PgError{Code: "23505", ConstraintName: "other_unique"}, &pgconn.PgError{Code: "23503", ConstraintName: "runtime_slot_owner_conflict"}, errors.New("unknown")} {
		if runtimeSlotOwnerError(e) != e {
			t.Fatal("unrelated SQL error rewritten")
		}
	}
}
