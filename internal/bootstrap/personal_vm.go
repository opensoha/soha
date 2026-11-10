package bootstrap

import (
	"context"

	guest "github.com/opensoha/soha-contracts/agent/guest"
	appidentity "github.com/opensoha/soha/internal/application/identity"
	"github.com/opensoha/soha/internal/application/personalenvironment"
	appvirtualization "github.com/opensoha/soha/internal/application/virtualization"
	domain "github.com/opensoha/soha/internal/domain/copilot"
	identity "github.com/opensoha/soha/internal/domain/identity"
	copilotrepo "github.com/opensoha/soha/internal/repository/copilot"
)

func newPersonalEnvironmentVM(identities *appidentity.Service, repo *copilotrepo.Repository, vm *appvirtualization.Service) (*personalenvironment.VM, error) {
	return personalenvironment.NewVM(personalenvironment.VMDependencies{
		Identity:   identities,
		Approvals:  repo,
		Assignment: vm,
		Runtime:    vm,
		Guests:     repo,
		ObserveVM: func(ctx context.Context, principal identity.Principal, computeRef, serial string) (personalenvironment.VMObservation, error) {
			proof, err := vm.ObservePersonalGuestVM(ctx, principal, computeRef, serial)
			return personalenvironment.VMObservation{Provider: proof.Provider, Identity: domain.PersonalVMIdentity{
				ProviderIdentityHash: proof.ProviderIdentityHash, DataIdentityHash: proof.DataIdentityHash,
				DataDiskSerial: serial, DataDiskBytes: proof.DataDiskBytes,
			}}, err
		},
		FrozenRuntime: func(ctx context.Context, principal identity.Principal, id string) (guest.RuntimeOperation, bool, error) {
			op, task, err := vm.FrozenPersonalGuestRuntime(ctx, principal, id)
			return op, task.Terminal, err
		},
	})
}
