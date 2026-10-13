package copilot

import (
	"context"
	"crypto/sha256"
	"fmt"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *PersonalGuestService) ConfigureProvisioning(artifact []byte, grant func(context.Context, guest.Binding, string) (guest.DataPlan, error)) string {
	s.artifact, s.dataGrant = artifact, grant
	return fmt.Sprintf("%x", sha256.Sum256(artifact))
}

func (s *PersonalGuestService) ConfigureDataLease(lease func(context.Context, guest.Binding) (guest.DataPlan, error)) {
	s.dataLease = lease
}

func (s *PersonalGuestService) DataLease(ctx context.Context, identity networkidentity.Identity) (guest.DataPlan, error) {
	item, err := s.authenticatedGuest(ctx, identity)
	if err != nil {
		return guest.DataPlan{}, err
	}
	if item.Status != "registered" || s.dataLease == nil {
		return guest.DataPlan{}, apperrors.ErrAccessDenied
	}
	return s.dataLease(ctx, item.Binding)
}

func (s *PersonalGuestService) Artifact(ctx context.Context, identity networkidentity.Identity) ([]byte, error) {
	if len(s.artifact) == 0 {
		return nil, apperrors.ErrServiceUnavailable
	}
	if _, err := s.authenticatedGuest(ctx, identity); err != nil {
		return nil, err
	}
	return s.artifact, nil
}

func (s *PersonalGuestService) DataOperation(ctx context.Context, identity networkidentity.Identity, operationID string) (guest.DataPlan, error) {
	item, err := s.authenticatedGuest(ctx, identity)
	if err != nil {
		return guest.DataPlan{}, err
	}
	if item.Status != "registered" || s.dataGrant == nil {
		return guest.DataPlan{}, apperrors.ErrAccessDenied
	}
	return s.dataGrant(ctx, item.Binding, operationID)
}
