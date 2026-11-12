package identity

import (
	"context"
	"fmt"

	domainmfa "github.com/opensoha/soha/internal/domain/identitymfa"
)

type NetworkPasswordSettings interface {
	LocalPasswordLoginEnabled(context.Context) (bool, error)
}

type NetworkPasswordMFAReader interface {
	ListCredentials(context.Context, string) ([]domainmfa.Credential, error)
}

// NetworkPasswordPolicy reuses the same local-password switch and MFA records
// as interactive identity, while requiring an explicit network-only opt-in.
type NetworkPasswordPolicy struct {
	Enabled                         bool
	InteractiveVerificationRequired bool
	Settings                        NetworkPasswordSettings
	MFA                             NetworkPasswordMFAReader
}

func (p NetworkPasswordPolicy) LocalPasswordLoginEnabled(ctx context.Context) (bool, error) {
	if !p.Enabled {
		return false, nil
	}
	if isNilDependency(p.Settings) {
		return false, fmt.Errorf("network password settings unavailable")
	}
	return p.Settings.LocalPasswordLoginEnabled(ctx)
}

func (p NetworkPasswordPolicy) NetworkPasswordRequiresInteraction(ctx context.Context, subjectID string) (bool, error) {
	if p.InteractiveVerificationRequired {
		return true, nil
	}
	if isNilDependency(p.MFA) {
		return true, fmt.Errorf("network MFA protection unavailable")
	}
	credentials, err := p.MFA.ListCredentials(ctx, subjectID)
	if err != nil {
		return true, err
	}
	return len(credentials) > 0, nil
}
