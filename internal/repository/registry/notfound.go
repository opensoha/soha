package registry

import (
	"fmt"
	"strings"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func registryNotFound(id string) error {
	return fmt.Errorf("%w: registry connection not found: %s", apperrors.ErrNotFound, strings.TrimSpace(id))
}
