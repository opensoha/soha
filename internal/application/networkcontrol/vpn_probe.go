package networkcontrol

import (
	"context"
	"slices"

	runtime "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkprobe"
	"github.com/opensoha/soha/internal/networkprotocol"
)

func (s *Service) configureVPNProbe(ctx context.Context, credential runtime.Credential, desired *networkprotocol.ConfigurationDesired) error {
	if credential.RuntimeKind != "gateway" || (!slices.Contains(credential.Capabilities, networkprotocol.CapabilityVPNProbe) && !slices.Contains(credential.Capabilities, networkprotocol.CapabilityVPNMetrics)) {
		return nil
	}
	store, ok := s.store.(interface {
		VPNGatewayID(context.Context, string) (string, error)
	})
	if !ok {
		return managedVPNUnavailable()
	}
	id, err := store.VPNGatewayID(ctx, credential.RuntimeID)
	if err != nil {
		return err
	}
	signer, err := networkprobe.NewSigner(s.options.CredentialEncryptionKeys)
	if err != nil {
		return err
	}
	desired.VPNProbe = &networkprotocol.VPNProbeConfiguration{GatewayID: id, VerificationKey: signer.PublicKey()}
	return nil
}
