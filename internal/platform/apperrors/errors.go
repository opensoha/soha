package apperrors

import (
	"errors"
	"strings"
)

var (
	ErrUnauthorized         = errors.New("unauthorized")
	ErrAccessDenied         = errors.New("access denied")
	ErrMFARequired          = errors.New("mfa required")
	ErrConflict             = errors.New("conflict")
	ErrNotFound             = errors.New("not found")
	ErrClusterUnready       = errors.New("cluster unavailable")
	ErrServiceUnavailable   = errors.New("service unavailable")
	ErrInvalidArgument      = errors.New("invalid argument")
	ErrUnsupportedOperation = errors.New("unsupported operation")
)

// BusinessError keeps an internal error category while exposing a stable code
// and a user-facing message at the HTTP boundary.
type BusinessError struct {
	kind           error
	code           string
	englishMessage string
	chineseMessage string
}

func NewBusiness(kind error, code, englishMessage, chineseMessage string) *BusinessError {
	return &BusinessError{
		kind:           kind,
		code:           strings.TrimSpace(code),
		englishMessage: strings.TrimSpace(englishMessage),
		chineseMessage: strings.TrimSpace(chineseMessage),
	}
}

func (e *BusinessError) Error() string {
	if e.code == "" {
		return e.englishMessage
	}
	return e.code + ": " + e.englishMessage
}

func (e *BusinessError) Unwrap() error { return e.kind }

func (e *BusinessError) Code() string { return e.code }

func (e *BusinessError) Message(language string) string {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(language)), "zh") && e.chineseMessage != "" {
		return e.chineseMessage
	}
	return e.englishMessage
}
