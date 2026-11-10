package resourcebackend

import (
	"fmt"

	"github.com/opensoha/soha/internal/platform/apperrors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func observedWorkloadDeleteError(err error) error {
	switch {
	case apierrors.IsNotFound(err):
		return fmt.Errorf("%w: %w", apperrors.ErrNotFound, err)
	case apierrors.IsForbidden(err), apierrors.IsUnauthorized(err):
		return fmt.Errorf("%w: %w", apperrors.ErrAccessDenied, err)
	case apierrors.IsBadRequest(err), apierrors.IsInvalid(err):
		return fmt.Errorf("%w: %w", apperrors.ErrInvalidArgument, err)
	default:
		return resourceMutationError(err)
	}
}
