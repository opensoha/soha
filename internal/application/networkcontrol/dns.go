package networkcontrol

import (
	"context"
	"slices"
	"time"

	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type siteDNSStore interface {
	SaveDNSObservations(context.Context, string, networkprotocol.DNSObservationBatch, time.Time) error
}

func (s *Service) DNSObservations(ctx context.Context, identity networkidentity.Identity, raw []byte) error {
	message, err := s.validateInbound(identity, networkprotocol.MessageDNSObservations, raw)
	if err != nil {
		return err
	}
	credential, err := s.authenticate(ctx, identity)
	if err != nil {
		return err
	}
	if identity.Kind != "gateway" || !slices.Contains(credential.Capabilities, networkprotocol.CapabilitySiteDNS) {
		return unauthorizedRuntime()
	}
	payload, err := networkprotocol.DecodePayload[networkprotocol.DNSObservationBatch](message.Payload)
	if err != nil {
		return invalidRuntimeMessage()
	}
	store, ok := s.store.(siteDNSStore)
	if !ok {
		return apperrors.ErrServiceUnavailable
	}
	return store.SaveDNSObservations(ctx, identity.ID, payload, s.now().UTC())
}
