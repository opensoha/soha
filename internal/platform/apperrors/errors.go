package apperrors

import "errors"

var (
	ErrUnauthorized         = errors.New("unauthorized")
	ErrAccessDenied         = errors.New("access denied")
	ErrMFARequired          = errors.New("mfa required")
	ErrConflict             = errors.New("conflict")
	ErrNotFound             = errors.New("not found")
	ErrClusterUnready       = errors.New("cluster unavailable")
	ErrInvalidArgument      = errors.New("invalid argument")
	ErrUnsupportedOperation = errors.New("unsupported operation")
)
