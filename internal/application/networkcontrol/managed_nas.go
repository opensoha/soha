package networkcontrol

import (
	"context"
	"strings"

	access "github.com/opensoha/soha/internal/domain/networkaccess"
	runtime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
)

type ManagedNASManager interface {
	ManagedNAS(context.Context, networkidentity.Identity, string, *networkprotocol.ManagedNASProof, bool) (access.NASBinding, bool, string, error)
	ManagedEnrollment(context.Context, networkidentity.Identity, networkprotocol.EnrollmentRequest) (*runtime.ManagedNASMemberScope, error)
	ManagedConfiguration(context.Context, networkidentity.Identity) error
}

func reservedNAS(id string) bool {
	return strings.HasPrefix(id, "radius-member:") || strings.HasPrefix(id, "radius-pool:")
}
func (s *Service) authenticateNASWithProof(ctx context.Context, id networkidentity.Identity, nas string, proof *networkprotocol.ManagedNASProof, required bool) (access.NASBinding, string, error) {
	if id.Kind != "nas" {
		return access.NASBinding{}, "", unauthorizedRuntime()
	}
	if _, err := s.authenticate(ctx, id); err != nil {
		return access.NASBinding{}, "", err
	}
	if s.options.ManagedNAS != nil {
		binding, managed, pin, err := s.options.ManagedNAS.ManagedNAS(ctx, id, nas, proof, required)
		if managed || err != nil {
			return binding, pin, err
		}
	} else if reservedNAS(id.ID) || proof != nil {
		return access.NASBinding{}, "", unauthorizedRuntime()
	}
	binding, err := s.store.NASBinding(ctx, id.ID, nas)
	if err != nil {
		return access.NASBinding{}, "", err
	}
	if binding.Status != access.StatusActive || binding.RuntimeID != id.ID || binding.NASID != nas {
		return access.NASBinding{}, "", unauthorizedRuntime()
	}
	return binding, "", nil
}

func attachManagedNAS(a *runtime.NASAuthorization, proof *networkprotocol.ManagedNASProof, pin, binding, fingerprint string) {
	if pin != "" {
		a.ManagedRuntimeProof = proof
		a.ManagedSlotHash = pin
		a.ManagedBindingID = binding
		a.ManagedRuntimeFingerprint = fingerprint
	}
}
