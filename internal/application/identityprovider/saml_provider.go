package identityprovider

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

type samlProviderRepository interface {
	CreateSAMLProvider(context.Context, domainprovider.Provider, domainprovider.SAMLServiceProvider, domainprovider.SAMLSigningKey) (domainprovider.Provider, error)
	UpdateSAMLProvider(context.Context, domainprovider.Provider, domainprovider.SAMLServiceProvider) (domainprovider.Provider, error)
	UpsertSAMLServiceProvider(context.Context, domainprovider.SAMLServiceProvider) error
	GetSAMLServiceProvider(context.Context, string) (domainprovider.SAMLServiceProvider, error)
	GetActiveSAMLSigningKey(context.Context, string) (domainprovider.SAMLSigningKey, error)
	GetSAMLSigningKey(context.Context, string) (domainprovider.SAMLSigningKey, error)
	ListSAMLMetadataSigningKeys(context.Context, string, time.Time) ([]domainprovider.SAMLSigningKey, error)
	RotateSAMLSigningKey(context.Context, string, domainprovider.SAMLSigningKey, time.Time) (domainprovider.SAMLSigningKey, domainprovider.SAMLSigningKey, error)
	ConsumeSAMLReplayKey(context.Context, string, string, string, time.Time) error
}

func (s *Service) RotateSAMLCertificate(ctx context.Context, principal domainidentity.Principal, certificateID string, request sohaapi.SAMLCertificateRotateRequest) (sohaapi.SAMLCertificateRotation, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermIdentityProvidersManage); err != nil {
		return sohaapi.SAMLCertificateRotation{}, err
	}
	if request.OverlapSeconds < 0 || request.OverlapSeconds > int((30*24*time.Hour)/time.Second) {
		return sohaapi.SAMLCertificateRotation{}, fmt.Errorf("%w: SAML certificate overlap must be between 0 and 2592000 seconds", apperrors.ErrInvalidArgument)
	}
	repository, ok := s.repo.(samlProviderRepository)
	if !ok {
		return sohaapi.SAMLCertificateRotation{}, fmt.Errorf("%w: SAML provider repository is not configured", apperrors.ErrUnsupportedOperation)
	}
	now := time.Now().UTC()
	current, err := repository.GetSAMLSigningKey(ctx, strings.TrimSpace(certificateID))
	if err != nil {
		return sohaapi.SAMLCertificateRotation{}, err
	}
	generated, err := s.generateSAMLSigningKey(current.ProviderID, now)
	if err != nil {
		return sohaapi.SAMLCertificateRotation{}, err
	}
	retiring, active, err := repository.RotateSAMLSigningKey(ctx, strings.TrimSpace(certificateID), generated, now.Add(time.Duration(request.OverlapSeconds)*time.Second))
	if err != nil {
		return sohaapi.SAMLCertificateRotation{}, err
	}
	s.recordAudit(ctx, principal, "identity.saml.certificate.rotate", "success", domainprovider.Provider{ID: retiring.ProviderID, Type: domainprovider.ProviderTypeSAML}, domainprovider.OIDCClient{}, map[string]any{"retiringCertificateId": retiring.ID, "activeCertificateId": active.ID, "overlapSeconds": request.OverlapSeconds})
	return sohaapi.SAMLCertificateRotation{Active: samlCertificateSummary(active, sohaapi.CertificateSummaryStatusActive), Retiring: samlCertificateSummary(retiring, sohaapi.CertificateSummaryStatusRetiring), OverlapEndsAt: *retiring.RetireAfter}, nil
}

func samlCertificateSummary(key domainprovider.SAMLSigningKey, status sohaapi.CertificateSummaryStatus) sohaapi.CertificateSummary {
	summary := sohaapi.CertificateSummary{ID: key.ID, FingerprintSHA256: key.FingerprintSHA256, NotBefore: key.NotBefore, NotAfter: key.NotAfter, Status: status}
	if block, _ := pem.Decode([]byte(key.CertificatePEM)); block != nil {
		if certificate, err := x509.ParseCertificate(block.Bytes); err == nil {
			summary.Subject, summary.Issuer = certificate.Subject.String(), certificate.Issuer.String()
		}
	}
	return summary
}

func (s *Service) createSAMLProvider(ctx context.Context, item domainprovider.Provider, now time.Time) (domainprovider.Provider, error) {
	repository, ok := s.repo.(samlProviderRepository)
	if !ok {
		return domainprovider.Provider{}, fmt.Errorf("%w: SAML provider repository is not configured", apperrors.ErrUnsupportedOperation)
	}
	serviceProvider, err := samlServiceProviderFromConfig(item, now)
	if err != nil {
		return domainprovider.Provider{}, err
	}
	key, err := s.generateSAMLSigningKey(item.ID, now)
	if err != nil {
		return domainprovider.Provider{}, err
	}
	return repository.CreateSAMLProvider(ctx, item, serviceProvider, key)
}

func (s *Service) updateSAMLProvider(ctx context.Context, item domainprovider.Provider, now time.Time) (domainprovider.Provider, error) {
	repository, ok := s.repo.(samlProviderRepository)
	if !ok {
		return domainprovider.Provider{}, fmt.Errorf("%w: SAML provider repository is not configured", apperrors.ErrUnsupportedOperation)
	}
	serviceProvider, err := samlServiceProviderFromConfig(item, now)
	if err != nil {
		return domainprovider.Provider{}, err
	}
	return repository.UpdateSAMLProvider(ctx, item, serviceProvider)
}

func samlServiceProviderFromConfig(provider domainprovider.Provider, now time.Time) (domainprovider.SAMLServiceProvider, error) {
	acs := configStringSlice(provider.Config, "assertionConsumerServiceUrls", "acsUrls", "acs_urls")
	mappings := samlAttributeMappings(provider.Config["attributeMappings"])
	return domainprovider.SAMLServiceProvider{
		ProviderID: provider.ID, EntityID: configString(provider.Config, "entityId", "entityID", "entity_id"),
		AssertionConsumerServiceURLs: acs,
		NameIDFormat:                 firstNonEmpty(configString(provider.Config, "nameIdFormat", "name_id_format"), "urn:oasis:names:tc:SAML:1.1:nameid-format:emailAddress"),
		WantAuthnRequestsSigned:      configBoolean(provider.Config, "wantAuthnRequestsSigned", "want_authn_requests_signed"),
		WantAssertionsSigned:         true,
		SigningCertificatePEM:        configString(provider.Config, "spCertificatePem", "signingCertificatePem", "signing_certificate_pem"),
		AttributeMappings:            mappings,
		CreatedAt:                    now,
		UpdatedAt:                    now,
	}, nil
}

func samlAttributeMappings(raw any) map[string]string {
	mappings := map[string]string{}
	switch value := raw.(type) {
	case map[string]any:
		for claim, attribute := range value {
			addSAMLAttributeMapping(mappings, claim, attribute)
		}
	case []any:
		for _, item := range value {
			mapping, ok := item.(map[string]any)
			if !ok {
				continue
			}
			addSAMLAttributeMapping(mappings, fmt.Sprint(mapping["source"]), mapping["target"])
		}
	}
	return mappings
}

func addSAMLAttributeMapping(mappings map[string]string, claim string, attribute any) {
	claim = strings.TrimSpace(claim)
	name := strings.TrimSpace(fmt.Sprint(attribute))
	if claim != "" && name != "" && name != "<nil>" {
		mappings[claim] = name
	}
}

func (s *Service) generateSAMLSigningKey(providerID string, now time.Time) (domainprovider.SAMLSigningKey, error) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return domainprovider.SAMLSigningKey{}, fmt.Errorf("generate SAML signing key: %w", err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(now.UnixNano()), Subject: pkix.Name{CommonName: "Soha SAML " + providerID},
		NotBefore: now.Add(-5 * time.Minute), NotAfter: now.AddDate(2, 0, 0),
		KeyUsage: x509.KeyUsageDigitalSignature, BasicConstraintsValid: true,
	}
	rawCertificate, err := x509.CreateCertificate(rand.Reader, template, template, &privateKey.PublicKey, privateKey)
	if err != nil {
		return domainprovider.SAMLSigningKey{}, fmt.Errorf("create SAML signing certificate: %w", err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return domainprovider.SAMLSigningKey{}, fmt.Errorf("marshal SAML signing key: %w", err)
	}
	privatePEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})
	encrypted, err := secretcrypto.EncryptStringWithKeyring(s.encryptionKeys, string(privatePEM))
	if err != nil {
		return domainprovider.SAMLSigningKey{}, fmt.Errorf("encrypt SAML signing key: %w", err)
	}
	fingerprint := sha256.Sum256(rawCertificate)
	return domainprovider.SAMLSigningKey{
		ID: uuid.NewString(), ProviderID: providerID, EncryptedPrivateKey: encrypted,
		CertificatePEM:    string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: rawCertificate})),
		FingerprintSHA256: hex.EncodeToString(fingerprint[:]), Active: true,
		NotBefore: template.NotBefore, NotAfter: template.NotAfter, CreatedAt: now,
	}, nil
}
