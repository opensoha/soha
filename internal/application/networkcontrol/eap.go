package networkcontrol

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/networkruntime"
	"github.com/opensoha/soha/internal/networkidentity"
	"github.com/opensoha/soha/internal/networkprotocol"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type EAPCertificateIssuer interface {
	Issue(context.Context, string, string, string, time.Time, time.Time) (string, string, error)
}

func (s *Service) eapCredential(ctx context.Context, identity networkidentity.Identity) (domain.Credential, error) {
	if identity.Kind != "endpoint" {
		return domain.Credential{}, unauthorizedRuntime()
	}
	credential, err := s.authenticate(ctx, identity)
	if err != nil {
		return domain.Credential{}, err
	}
	if !slices.Contains(credential.Capabilities, "eap-profile-v1") || !slices.Contains(credential.Capabilities, "radius-eap-tls-v1") {
		return domain.Credential{}, unauthorizedRuntime()
	}
	return credential, nil
}

func (s *Service) IssueEAPCertificate(ctx context.Context, identity networkidentity.Identity, raw []byte) (networkprotocol.RuntimeMessage, error) {
	message, err := s.validateInbound(identity, networkprotocol.MessageEAPCertificateRequest, raw)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	credential, err := s.eapCredential(ctx, identity)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	if s.options.EAPIssuer == nil {
		return networkprotocol.RuntimeMessage{}, apperrors.NewBusiness(apperrors.ErrServiceUnavailable, "eap_issuer_unavailable", "A dedicated EAP issuer is not configured.", "尚未配置独立 EAP 证书签发者。")
	}
	request, err := networkprotocol.DecodePayload[networkprotocol.EAPCertificateRequest](message.Payload)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, invalidRuntimeMessage()
	}
	now := s.now().UTC()
	certificatePEM, caPEM, err := s.options.EAPIssuer.Issue(ctx, credential.DeviceID, credential.SubjectID, request.CSRPEM, now, credential.ExpiresAt)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "eap_certificate_request_invalid", "The EAP certificate request cannot be issued.", "无法签发该 EAP 证书请求。")
	}
	block, _ := pem.Decode([]byte(certificatePEM))
	if block == nil {
		return networkprotocol.RuntimeMessage{}, fmt.Errorf("EAP issuer returned an invalid certificate")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, fmt.Errorf("parse issued EAP certificate: %w", err)
	}
	digest, csrDigest := sha256.Sum256(certificate.Raw), sha256.Sum256([]byte(request.CSRPEM))
	saved, err := s.store.SaveEAPCertificate(ctx, domain.EAPCredential{EAPCertificateResult: networkprotocol.EAPCertificateResult{
		CredentialID: uuid.NewString(), CertificatePEM: certificatePEM, CACertificatePEM: caPEM, Fingerprint: fmt.Sprintf("sha256:%x", digest), ExpiresAt: certificate.NotAfter, Status: "pending"},
		ParentCredentialID: credential.ID, RuntimeID: credential.RuntimeID, DeviceID: credential.DeviceID, SubjectID: credential.SubjectID,
		RequestID: request.RequestID, CSRHash: fmt.Sprintf("sha256:%x", csrDigest), CertificateSerial: certificate.SerialNumber.Text(16), CertificateAuthorityKeyID: fmt.Sprintf("%x", certificate.AuthorityKeyId), NotBefore: certificate.NotBefore, CreatedAt: now}, now)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	return s.outbound(identity, networkprotocol.MessageEAPCertificateResult, saved.EAPCertificateResult, minTime(now.Add(s.options.MaxClockSkew), saved.ExpiresAt))
}

func (s *Service) ActivateEAPCertificate(ctx context.Context, identity networkidentity.Identity, raw []byte) (networkprotocol.RuntimeMessage, error) {
	message, err := s.validateInbound(identity, networkprotocol.MessageEAPCertificateActivateRequest, raw)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	credential, err := s.eapCredential(ctx, identity)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	request, err := networkprotocol.DecodePayload[networkprotocol.EAPCertificateActivateRequest](message.Payload)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, invalidRuntimeMessage()
	}
	now := s.now().UTC()
	result, err := s.store.ActivateEAPCertificate(ctx, credential, request, now)
	if err != nil {
		return networkprotocol.RuntimeMessage{}, err
	}
	return s.outbound(identity, networkprotocol.MessageEAPCertificateActivateResult, result, minTime(now.Add(s.options.MaxClockSkew), result.ExpiresAt))
}
