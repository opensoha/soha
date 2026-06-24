package menu

import (
	"fmt"
	"strings"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func menuNotFound(id string) error {
	return fmt.Errorf("%w: menu not found: %s", apperrors.ErrNotFound, strings.TrimSpace(id))
}
