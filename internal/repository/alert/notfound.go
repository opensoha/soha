package alert

import (
	"fmt"
	"strings"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func alertNotFound(kind, id string) error {
	return fmt.Errorf("%w: %s not found: %s", apperrors.ErrNotFound, strings.TrimSpace(kind), strings.TrimSpace(id))
}
