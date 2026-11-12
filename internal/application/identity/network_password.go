package identity

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"golang.org/x/crypto/bcrypt"
)

// NetworkPasswordStore is identity-owned. Hashes never cross the verifier port.
type NetworkPasswordStore interface {
	GetByID(context.Context, string) (domainidentity.User, error)
	GetPasswordHash(context.Context, string) (string, error)
	PasswordUpdatedAt(context.Context, string) (time.Time, error)
}

type NetworkPasswordProtection interface {
	LocalPasswordLoginEnabled(context.Context) (bool, error)
	NetworkPasswordRequiresInteraction(context.Context, string) (bool, error)
}

type NetworkPasswordIdentity struct {
	SubjectID         string
	PasswordUpdatedAt time.Time
}

// NetworkPasswordVerifier authenticates an exact profile subject, without issuing
// login sessions or interpreting username/email aliases. Its caller owns budgets.
type NetworkPasswordVerifier struct {
	store      NetworkPasswordStore
	protection NetworkPasswordProtection
	dummyHash  string
}

var networkPasswordSubject = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func NewNetworkPasswordVerifier(store NetworkPasswordStore, protection NetworkPasswordProtection) (*NetworkPasswordVerifier, error) {
	if isNilDependency(store) || isNilDependency(protection) {
		return nil, fmt.Errorf("network password verifier requires identity store and protection policy")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("network-password-timing-placeholder"), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	return &NetworkPasswordVerifier{store: store, protection: protection, dummyHash: string(hash)}, nil
}

func comparePasswordHash(hash string, password []byte) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), password)
}

func networkPasswordDenied() error {
	return fmt.Errorf("%w: network password authentication failed", apperrors.ErrUnauthorized)
}

func (v *NetworkPasswordVerifier) allowed(ctx context.Context, subject string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	enabled, err := v.protection.LocalPasswordLoginEnabled(ctx)
	if err != nil {
		return fmt.Errorf("read network password protection: %w", err)
	}
	if !enabled {
		return networkPasswordDenied()
	}
	interactive, err := v.protection.NetworkPasswordRequiresInteraction(ctx, subject)
	if err != nil {
		return fmt.Errorf("read network authentication requirements: %w", err)
	}
	if interactive {
		return networkPasswordDenied()
	}
	user, err := v.store.GetByID(ctx, subject)
	if errors.Is(err, apperrors.ErrNotFound) {
		return networkPasswordDenied()
	}
	if err != nil {
		return fmt.Errorf("read network password account: %w", err)
	}
	if user.ID != subject || user.Status != "active" {
		return networkPasswordDenied()
	}
	return nil
}

func (v *NetworkPasswordVerifier) Verify(ctx context.Context, subject string, password []byte) (NetworkPasswordIdentity, error) {
	if !networkPasswordSubject.MatchString(subject) || len(password) == 0 || len(password) > 72 {
		return NetworkPasswordIdentity{}, networkPasswordDenied()
	}
	if err := v.allowed(ctx, subject); err != nil {
		if errors.Is(err, apperrors.ErrUnauthorized) {
			_ = comparePasswordHash(v.dummyHash, password)
		}
		return NetworkPasswordIdentity{}, err
	}
	version, err := v.store.PasswordUpdatedAt(ctx, subject)
	if err != nil {
		return NetworkPasswordIdentity{}, networkPasswordDenied()
	}
	hash, err := v.store.GetPasswordHash(ctx, subject)
	if err != nil {
		_ = comparePasswordHash(v.dummyHash, password)
		return NetworkPasswordIdentity{}, networkPasswordDenied()
	}
	if err := comparePasswordHash(hash, password); err != nil {
		return NetworkPasswordIdentity{}, networkPasswordDenied()
	}
	result := NetworkPasswordIdentity{SubjectID: subject, PasswordUpdatedAt: version.UTC()}
	if err := v.Current(ctx, result); err != nil {
		return NetworkPasswordIdentity{}, err
	}
	return result, nil
}

func (v *NetworkPasswordVerifier) Current(ctx context.Context, identity NetworkPasswordIdentity) error {
	if err := v.allowed(ctx, identity.SubjectID); err != nil {
		return err
	}
	current, err := v.store.PasswordUpdatedAt(ctx, identity.SubjectID)
	if err != nil || current.IsZero() || !current.Equal(identity.PasswordUpdatedAt) {
		return networkPasswordDenied()
	}
	return ctx.Err()
}
