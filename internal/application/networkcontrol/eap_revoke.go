package networkcontrol

import (
	"context"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type EAPRevocationStore interface {
	RevokeEAPCertificate(context.Context, domain.Credential, networkprotocol.EAPCertificateRevokeRequest, time.Time) (networkprotocol.EAPCertificateRevokeResult, error)
}

func (s *Service) RevokeEAPCertificate(ctx context.Context, identity networkidentity.Identity, raw []byte) (networkprotocol.RuntimeMessage, error) {
	message, err := s.validateInbound(identity, "eap.certificate.revoke.request", raw)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	credential, err := s.eapCredential(ctx, identity)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	if s.options.EAPRevocations == nil {
		return networkprotocol.RuntimeMessage{}, apperrors.ErrServiceUnavailable
	}
	request, err := networkprotocol.DecodePayload[networkprotocol.EAPCertificateRevokeRequest](message.Payload)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, invalidRuntimeMessage()
	}
	now := s.now().UTC()
	result, err := s.options.EAPRevocations.RevokeEAPCertificate(ctx, credential, request, now)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	return s.outbound(identity, "eap.certificate.revoke.result", result, now.Add(s.options.MaxClockSkew))
}
