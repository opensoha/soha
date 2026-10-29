package contentdlp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"reflect"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

type StepUpVerifier interface {
	RequireRecentStepUp(context.Context, string, string) error
}

func WithStepUp(verifier StepUpVerifier) Option {
	return func(s *Service) { s.stepUp = verifier }
}

// RequireReleaseMFA does not accept client-supplied authentication timestamps.
func RequireReleaseMFA(ctx context.Context, verifier StepUpVerifier, userID, sessionID string) error {
	if userID == "" || sessionID == "" {
		return apperrors.ErrMFARequired
	}
	if verifier == nil || reflect.ValueOf(verifier).Kind() == reflect.Pointer && reflect.ValueOf(verifier).IsNil() {
		return apperrors.ErrServiceUnavailable
	}
	return verifier.RequireRecentStepUp(ctx, userID, sessionID)
}

// ReleaseTokenHash preserves old policy tokens; MFA tokens are session-bound.
func ReleaseTokenHash(token, sessionID string, stepUp bool) string {
	material := token
	if stepUp {
		material = sessionID + "\x00" + token
	}
	sum := sha256.Sum256([]byte(material))
	return hex.EncodeToString(sum[:])
}
