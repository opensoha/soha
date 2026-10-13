package secret

import (
	"context"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/secret"
)

func (s *Service) PinSSHCredential(ctx context.Context, principal domainidentity.Principal, uri string, target domain.Target) (domain.Reference, domain.Kind, error) {
	pinned, err := s.PinReferences(ctx, principal, map[string]string{"SSH_AUTH": uri}, target)
	if err != nil {
		return domain.Reference{}, "", err
	}
	_, version, err := s.availableReference(ctx, principal, pinned[0], target)
	if err != nil {
		return domain.Reference{}, "", err
	}
	if version.Kind != domain.KindSSHKeypair && version.Kind != domain.KindSSHPassword {
		return domain.Reference{}, "", unavailableReference()
	}
	return pinned[0], version.Kind, nil
}
