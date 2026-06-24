package announcement

import (
	"fmt"
	"strings"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func announcementNotFound(id string) error {
	return fmt.Errorf("%w: announcement not found: %s", apperrors.ErrNotFound, strings.TrimSpace(id))
}
