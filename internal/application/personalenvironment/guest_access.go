package personalenvironment

import (
	"context"
	"fmt"
	"time"

	guest "github.com/opensoha/soha/contracts/agent/guest"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type GuestApprovals interface {
	GetPersonalAgentAccess(context.Context, identity.Principal) (domain.PersonalAgentAccess, error)
}

type GuestVMAccess interface {
	ValidatePersonalGuestVM(context.Context, identity.Principal, guest.Binding) error
	AuthorizePersonalGuestMutation(context.Context, identity.Principal, guest.Binding) error
}

type GuestLeaseStore interface {
	PersonalGuestDataLease(context.Context, guest.Binding) (guest.DataPlan, error)
}

type GuestAccessDependencies struct {
	Identity  CurrentIdentity
	Approvals GuestApprovals
	VM        GuestVMAccess
	Leases    GuestLeaseStore
}

type GuestAccess struct {
	deps GuestAccessDependencies
}

func NewGuestAccess(deps GuestAccessDependencies) (*GuestAccess, error) {
	if missingPort(deps.Identity) || missingPort(deps.Approvals) || missingPort(deps.VM) || missingPort(deps.Leases) {
		return nil, fmt.Errorf("personal guest access requires current identity, approvals, VM access and lease storage")
	}
	return &GuestAccess{deps: deps}, nil
}

func (s *GuestAccess) Authorize(ctx context.Context, binding guest.Binding) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	principal, err := s.deps.Identity.CurrentPrincipal(ctx, binding.OwnerID)
	if err != nil {
		return err
	}
	if principal.UserID != binding.OwnerID {
		return apperrors.ErrAccessDenied
	}
	access, err := s.deps.Approvals.GetPersonalAgentAccess(ctx, principal)
	if err != nil {
		return err
	}
	if access.UserID != binding.OwnerID || access.Status != "approved" {
		return apperrors.ErrAccessDenied
	}
	return s.deps.VM.ValidatePersonalGuestVM(ctx, principal, binding)
}

func (s *GuestAccess) AuthorizeData(ctx context.Context, binding guest.Binding) error {
	if err := s.Authorize(ctx, binding); err != nil {
		return err
	}
	principal, err := s.deps.Identity.CurrentPrincipal(ctx, binding.OwnerID)
	if err != nil {
		return err
	}
	return s.deps.VM.AuthorizePersonalGuestMutation(ctx, principal, binding)
}

func (s *GuestAccess) DataLease(ctx context.Context, binding guest.Binding) (guest.DataPlan, error) {
	if err := s.AuthorizeData(ctx, binding); err != nil {
		return guest.DataPlan{}, err
	}
	return s.deps.Leases.PersonalGuestDataLease(ctx, binding)
}
