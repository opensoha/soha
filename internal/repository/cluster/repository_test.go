package cluster

import (
	"errors"
	"testing"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestErrNotFoundWrapsAppErrorSentinel(t *testing.T) {
	if !errors.Is(ErrNotFound, apperrors.ErrNotFound) {
		t.Fatalf("ErrNotFound should wrap apperrors.ErrNotFound")
	}
}
