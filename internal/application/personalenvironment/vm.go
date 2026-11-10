package personalenvironment

import (
	"context"
	"fmt"
	"time"

	guest "github.com/opensoha/soha-contracts/agent/guest"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type AssignmentApprovals interface {
	GetPersonalAgentAccess(context.Context, string) (domain.PersonalAgentAccess, error)
}

type VMAssignmentAuthorizer interface {
	AuthorizePersonalVMAssignment(context.Context, identity.Principal, string) error
}

type VMObservation struct {
	Provider string
	Identity domain.PersonalVMIdentity
}

type VMRuntime interface {
	PersonalRuntimeArtifact() string
	InvokePersonalGuestRuntime(context.Context, identity.Principal, guest.RuntimeOperation) (guest.RuntimeResult, error)
}

type VMGuestStore interface {
	CurrentPersonalGuest(context.Context, string, string) (domain.PersonalGuestRecord, error)
	CheckPersonalGuestBinding(context.Context, domain.PersonalGuestRecord) error
	RecoverTerminalPersonalRuntime(context.Context, domain.PersonalEnvironmentRecord) (domain.PersonalEnvironmentRecord, error)
}

type VMDependencies struct {
	Identity      CurrentIdentity
	Approvals     AssignmentApprovals
	Assignment    VMAssignmentAuthorizer
	ObserveVM     func(context.Context, identity.Principal, string, string) (VMObservation, error)
	Runtime       VMRuntime
	FrozenRuntime func(context.Context, identity.Principal, string) (guest.RuntimeOperation, bool, error)
	Guests        VMGuestStore
}

type VM struct {
	deps VMDependencies
	now  func() time.Time
}

func NewVM(deps VMDependencies) (*VM, error) {
	if missingPort(deps.Identity) || missingPort(deps.Approvals) || missingPort(deps.Assignment) ||
		deps.ObserveVM == nil || missingPort(deps.Runtime) || deps.FrozenRuntime == nil || missingPort(deps.Guests) {
		return nil, fmt.Errorf("personal environment VM requires current identity, approvals, assignment, observation, runtime and guest storage")
	}
	return &VM{deps: deps, now: time.Now}, nil
}

func (s *VM) Observe(ctx context.Context, principal identity.Principal, policy domain.PersonalVMPolicy) (domain.PersonalVMIdentity, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if policy.Validate() != nil || policy.OwnerID != principal.UserID {
		return domain.PersonalVMIdentity{}, apperrors.ErrAccessDenied
	}
	current, err := s.deps.Identity.CurrentPrincipal(ctx, principal.UserID)
	if err != nil {
		return domain.PersonalVMIdentity{}, err
	}
	if current.UserID != policy.OwnerID {
		return domain.PersonalVMIdentity{}, apperrors.ErrAccessDenied
	}
	proof, err := s.deps.ObserveVM(ctx, current, policy.ComputeRef, policy.DataDiskSerial)
	if err != nil {
		return domain.PersonalVMIdentity{}, err
	}
	if proof.Provider != policy.Provider || proof.Identity.DataDiskBytes != policy.DataDiskBytes {
		return domain.PersonalVMIdentity{}, apperrors.ErrConflict
	}
	return proof.Identity, nil
}

func (s *VM) ValidateAssignment(ctx context.Context, admin identity.Principal, policy domain.PersonalVMPolicy) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := s.deps.Assignment.AuthorizePersonalVMAssignment(ctx, admin, policy.ComputeRef); err != nil {
		return err
	}
	access, err := s.deps.Approvals.GetPersonalAgentAccess(ctx, policy.OwnerID)
	if err != nil {
		return err
	}
	if access.Status != "approved" || access.UserID != policy.OwnerID {
		return apperrors.ErrAccessDenied
	}
	_, err = s.Observe(ctx, identity.Principal{UserID: policy.OwnerID}, policy)
	return err
}
