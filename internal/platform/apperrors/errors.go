package apperrors

import "errors"

var (
	ErrUnauthorized    = errors.New("unauthorized")
	ErrAccessDenied    = errors.New("access denied")
	ErrNotFound        = errors.New("not found")
	ErrClusterUnready  = errors.New("cluster unavailable")
	ErrInvalidArgument = errors.New("invalid argument")
)
