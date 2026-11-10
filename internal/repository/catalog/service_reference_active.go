package catalog

import (
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func referenceLifecycleError(err error) error {
	var conflict *pgconn.PgError
	if errors.As(err, &conflict) {
		switch conflict.Code {
		case "23001", "23503", "23505", "23514", "40001", "40P01":
			return fmt.Errorf("%w: environment or target configuration conflicts with existing dependencies", apperrors.ErrConflict)
		}
	}
	return err
}
