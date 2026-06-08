package errors

import (
	"errors"
	"net/http"

	legacy "github.com/opensoha/soha/internal/platform/apperrors"
)

var (
	ErrUnauthorized    = legacy.ErrUnauthorized
	ErrAccessDenied    = legacy.ErrAccessDenied
	ErrNotFound        = legacy.ErrNotFound
	ErrClusterUnready  = legacy.ErrClusterUnready
	ErrInvalidArgument = legacy.ErrInvalidArgument
)

func StatusCode(err error) int {
	switch {
	case errors.Is(err, ErrInvalidArgument):
		return http.StatusBadRequest
	case errors.Is(err, ErrUnauthorized):
		return http.StatusUnauthorized
	case errors.Is(err, ErrAccessDenied):
		return http.StatusForbidden
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, ErrClusterUnready):
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}

func Code(err error) string {
	switch {
	case errors.Is(err, ErrInvalidArgument):
		return "invalid_argument"
	case errors.Is(err, ErrUnauthorized):
		return "unauthorized"
	case errors.Is(err, ErrAccessDenied):
		return "access_denied"
	case errors.Is(err, ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrClusterUnready):
		return "cluster_unavailable"
	default:
		return "internal_error"
	}
}
