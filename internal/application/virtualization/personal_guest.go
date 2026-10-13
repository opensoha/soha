package virtualization

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"

	guest "github.com/opensoha/soha-contracts/agent/guest"
	appaccess "github.com/opensoha/soha/internal/application/access"
	identity "github.com/opensoha/soha/internal/domain/identity"
	domain "github.com/opensoha/soha/internal/domain/virtualization"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

// PersonalGuestVMProof is a provider observation, not a data or tool readiness grant.
type PersonalGuestVMProof struct {
	Provider             string
	ProviderIdentityHash string
	DataDiskBytes        int64
	DataIdentityHash     string
}

func (s *Service) AuthorizePersonalVMAssignment(ctx context.Context, principal identity.Principal, id string) error {
	_, _, err := s.accessVM(ctx, principal, id, vmAccessPermission)
	return err
}

func (s *Service) ObservePersonalGuestVM(ctx context.Context, principal identity.Principal, id, serial string) (PersonalGuestVMProof, error) {
	if serial == "" || !(domain.AdapterDiskChange{Add: true, Serial: serial}).ValidSerial() {
		return PersonalGuestVMProof{}, apperrors.ErrInvalidArgument
	}
	vm, connection, err := s.accessVM(ctx, principal, id, appaccess.PermVirtualizationVMsView)
	if err != nil {
		return PersonalGuestVMProof{}, err
	}
	if normalizeProvider(vm.Provider) != ProviderPVE || normalizeProvider(connection.Provider) != ProviderPVE {
		return PersonalGuestVMProof{}, apperrors.ErrUnsupportedOperation
	}
	observed, source, err := s.observeVMIdentity(ctx, vm, connection)
	if err != nil {
		return PersonalGuestVMProof{}, err
	}
	if !strings.EqualFold(observed.Status, "running") {
		return PersonalGuestVMProof{}, apperrors.ErrConflict
	}
	var matches int
	var bytes int64
	var dataRef string
	for _, disk := range observed.DataDisks {
		if disk.Serial == serial {
			matches++
			if disk.SizeGiB < 1 || disk.SizeGiB > 1024 {
				return PersonalGuestVMProof{}, apperrors.ErrConflict
			}
			bytes = disk.SizeGiB << 30
			dataRef = disk.ProviderRef
		}
	}
	if matches != 1 {
		return PersonalGuestVMProof{}, apperrors.ErrConflict
	}
	digest, err := personalGuestProviderIdentityHash(ProviderPVE, source, observed.ProviderVMID, observed.Generation, dataRef)
	if err != nil {
		return PersonalGuestVMProof{}, err
	}
	dataHash, err := personalGuestProviderIdentityHash(ProviderPVE, source, dataRef)
	if err != nil {
		return PersonalGuestVMProof{}, err
	}
	return PersonalGuestVMProof{Provider: ProviderPVE, ProviderIdentityHash: digest, DataIdentityHash: dataHash, DataDiskBytes: bytes}, nil
}

func (s *Service) ValidatePersonalGuestVM(ctx context.Context, principal identity.Principal, binding guest.Binding) error {
	if binding.Validate() != nil || binding.OwnerID != principal.UserID {
		return apperrors.ErrAccessDenied
	}
	proof, err := s.ObservePersonalGuestVM(ctx, principal, binding.ComputeRef, binding.DataDiskSerial)
	if err != nil {
		return err
	}
	if proof.Provider != binding.Provider || proof.ProviderIdentityHash != binding.ProviderIdentityHash || proof.DataDiskBytes != binding.DataDiskBytes {
		return apperrors.ErrConflict
	}
	return nil
}

func personalGuestProviderIdentityHash(parts ...string) (string, error) {
	for _, part := range parts {
		if part == "" || len(part) > 512 || strings.ContainsRune(part, '\x00') || strings.TrimSpace(part) != part {
			return "", apperrors.ErrConflict
		}
	}
	digest := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return fmt.Sprintf("sha256:%x", digest), nil
}
