package copilot

import (
	"fmt"
	"strings"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func copilotNotFound(kind, id string) error {
	return fmt.Errorf("%w: %s not found: %s", apperrors.ErrNotFound, strings.TrimSpace(kind), strings.TrimSpace(id))
}
