package bootstrap

import (
	"context"
	"time"

	appvirtualization "github.com/opensoha/soha/internal/application/virtualization"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func observePersonalVM(ctx context.Context, core *coreServices, vm *appvirtualization.Service, principal identity.Principal, policy domain.PersonalVMPolicy) (domain.PersonalVMIdentity, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if policy.Validate() != nil || policy.OwnerID != principal.UserID {
		return domain.PersonalVMIdentity{}, apperrors.ErrAccessDenied
	}
	current, err := core.identityService.CurrentPrincipal(ctx, principal.UserID)
	if err != nil {
		return domain.PersonalVMIdentity{}, err
	}
	if current.UserID != policy.OwnerID {
		return domain.PersonalVMIdentity{}, apperrors.ErrAccessDenied
	}
	proof, err := vm.ObservePersonalGuestVM(ctx, current, policy.ComputeRef, policy.DataDiskSerial)
	if err != nil {
		return domain.PersonalVMIdentity{}, err
	}
	if proof.Provider != policy.Provider || proof.DataDiskBytes != policy.DataDiskBytes {
		return domain.PersonalVMIdentity{}, apperrors.ErrConflict
	}
	return domain.PersonalVMIdentity{ProviderIdentityHash: proof.ProviderIdentityHash, DataIdentityHash: proof.DataIdentityHash, DataDiskSerial: policy.DataDiskSerial, DataDiskBytes: proof.DataDiskBytes}, nil
}

func validatePersonalVMAssignment(ctx context.Context, core *coreServices, repos *repositories, vm *appvirtualization.Service, admin identity.Principal, policy domain.PersonalVMPolicy) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := vm.AuthorizePersonalVMAssignment(ctx, admin, policy.ComputeRef); err != nil {
		return err
	}
	access, err := repos.copilotRepository.GetPersonalAgentAccess(ctx, policy.OwnerID)
	if err != nil {
		return err
	}
	if access.Status != "approved" || access.UserID != policy.OwnerID {
		return apperrors.ErrAccessDenied
	}
	_, err = observePersonalVM(ctx, core, vm, identity.Principal{UserID: policy.OwnerID}, policy)
	return err
}
