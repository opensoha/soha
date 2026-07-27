package identityprovider

import (
	"context"
	"encoding/base64"
	"fmt"
	"html"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	domainportal "github.com/opensoha/soha/internal/domain/providerportal"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
	"github.com/opensoha/soha/internal/platform/requestctx"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

type SAMLRequestInput struct {
	Method          string
	Encoded         string
	RelayState      string
	RawQuery        string
	MetadataURL     string
	SSOURL          string
	ServiceProvider domainprovider.SAMLServiceProvider
}

type SAMLValidatedRequest struct {
	ID         string
	Issuer     string
	ACSURL     string
	RelayState string
}

type SAMLSigningMaterial struct {
	PrivateKeyPEM             string
	CertificatePEM            string
	AdditionalCertificatesPEM []string
	MetadataURL               string
	SSOURL                    string
}

type SAMLResponseInput struct {
	RequestID    string
	SPEntityID   string
	ACSURL       string
	NameID       string
	NameIDFormat string
	SessionIndex string
	AuthnInstant time.Time
	Attributes   map[string][]string
}

type SAMLIdentityProviderRuntime interface {
	Metadata(SAMLSigningMaterial) ([]byte, error)
	ValidateRequest(SAMLSigningMaterial, SAMLRequestInput) (SAMLValidatedRequest, error)
	SignResponse(SAMLSigningMaterial, SAMLResponseInput) ([]byte, error)
}

type samlPendingRequestRepository interface {
	CreateSAMLPendingRequest(context.Context, domainprovider.SAMLPendingRequest) error
	ConsumeSAMLPendingRequest(context.Context, string, string, time.Time) (domainprovider.SAMLPendingRequest, error)
}

type SAMLSSOResult struct {
	ACSURL string
	HTML   []byte
}

func NewWithEncryptionKeysAndSAML(repo domainprovider.Repository, users UserRepository, permissions *appaccess.PermissionResolver, audit AuditRecorder, encryptionKeys keyring.Ring, runtime SAMLIdentityProviderRuntime) *Service {
	service := NewWithEncryptionKeys(repo, users, permissions, audit, encryptionKeys)
	service.saml = runtime
	return service
}

func (s *Service) SAMLProviderMetadata(ctx context.Context, issuer, providerID string) ([]byte, error) {
	provider, material, _, err := s.samlRuntime(ctx, issuer, providerID)
	if err != nil {
		return nil, err
	}
	if !providerEnabled(provider) {
		return nil, fmt.Errorf("%w: SAML provider is disabled", apperrors.ErrNotFound)
	}
	return s.saml.Metadata(material)
}

func (s *Service) PrepareSAMLSSOLogin(ctx context.Context, issuer, providerID string, input SAMLRequestInput) (string, error) {
	provider, material, serviceProvider, err := s.samlRuntime(ctx, issuer, providerID)
	if err != nil {
		return "", err
	}
	if !providerEnabled(provider) {
		return "", fmt.Errorf("%w: SAML provider is disabled", apperrors.ErrUnauthorized)
	}
	input.MetadataURL, input.SSOURL, input.ServiceProvider = material.MetadataURL, material.SSOURL, serviceProvider
	if _, err := s.saml.ValidateRequest(material, input); err != nil {
		return "", fmt.Errorf("%w: invalid SAML AuthnRequest", apperrors.ErrInvalidArgument)
	}
	repository, ok := s.repo.(samlPendingRequestRepository)
	if !ok {
		return "", fmt.Errorf("%w: SAML pending request repository is not configured", apperrors.ErrUnsupportedOperation)
	}
	now := time.Now().UTC()
	token := uuid.NewString()
	err = repository.CreateSAMLPendingRequest(ctx, domainprovider.SAMLPendingRequest{
		Token: token, ProviderID: provider.ID, Method: input.Method, Encoded: input.Encoded,
		RelayState: input.RelayState, RawQuery: input.RawQuery, ExpiresAt: now.Add(10 * time.Minute), CreatedAt: now,
	})
	return token, err
}

func (s *Service) ResumeSAMLSSO(ctx context.Context, providerID, token string) (SAMLRequestInput, error) {
	repository, ok := s.repo.(samlPendingRequestRepository)
	if !ok {
		return SAMLRequestInput{}, fmt.Errorf("%w: SAML pending request repository is not configured", apperrors.ErrUnsupportedOperation)
	}
	item, err := repository.ConsumeSAMLPendingRequest(ctx, strings.TrimSpace(token), strings.TrimSpace(providerID), time.Now().UTC())
	if err != nil {
		return SAMLRequestInput{}, err
	}
	return SAMLRequestInput{Method: item.Method, Encoded: item.Encoded, RelayState: item.RelayState, RawQuery: item.RawQuery}, nil
}

func (s *Service) SAMLSSO(ctx context.Context, issuer, providerID, sessionID string, principal domainidentity.Principal, input SAMLRequestInput) (SAMLSSOResult, error) {
	provider, material, serviceProvider, err := s.samlRuntime(ctx, issuer, providerID)
	if err != nil {
		return SAMLSSOResult{}, err
	}
	if !providerEnabled(provider) {
		return SAMLSSOResult{}, fmt.Errorf("%w: SAML provider is disabled", apperrors.ErrUnauthorized)
	}
	application, err := s.repo.GetProviderApplication(ctx, provider.ID)
	if err != nil {
		return SAMLSSOResult{}, err
	}
	if err := s.validateSAMLProviderAccess(ctx, principal, application, sessionID); err != nil {
		return SAMLSSOResult{}, err
	}
	input.MetadataURL, input.SSOURL, input.ServiceProvider = material.MetadataURL, material.SSOURL, serviceProvider
	request, err := s.saml.ValidateRequest(material, input)
	if err != nil {
		return SAMLSSOResult{}, fmt.Errorf("%w: invalid SAML AuthnRequest", apperrors.ErrInvalidArgument)
	}
	repository := s.repo.(samlProviderRepository)
	if err := repository.ConsumeSAMLReplayKey(ctx, provider.ID, "request", request.ID, time.Now().UTC().Add(10*time.Minute)); err != nil {
		return SAMLSSOResult{}, fmt.Errorf("%w: SAML AuthnRequest was already consumed", apperrors.ErrUnauthorized)
	}
	response, err := s.saml.SignResponse(material, SAMLResponseInput{
		RequestID: request.ID, SPEntityID: request.Issuer, ACSURL: request.ACSURL,
		NameID: samlNameID(principal, serviceProvider.NameIDFormat), NameIDFormat: serviceProvider.NameIDFormat,
		SessionIndex: firstNonEmpty(sessionID, uuid.NewString()), AuthnInstant: time.Now().UTC(),
		Attributes: samlAttributes(principal, serviceProvider.AttributeMappings),
	})
	if err != nil {
		return SAMLSSOResult{}, err
	}
	encoded := base64.StdEncoding.EncodeToString(response)
	form := []byte(`<!doctype html><html><body><form method="post" action="` + html.EscapeString(request.ACSURL) + `"><input type="hidden" name="SAMLResponse" value="` + html.EscapeString(encoded) + `"><input type="hidden" name="RelayState" value="` + html.EscapeString(request.RelayState) + `"></form><script>document.forms[0].submit()</script></body></html>`)
	s.recordAudit(ctx, principal, "identity.saml.sso", "success", provider, domainprovider.OIDCClient{}, map[string]any{"applicationId": application.ID, "spEntityId": request.Issuer})
	return SAMLSSOResult{ACSURL: request.ACSURL, HTML: form}, nil
}

func (s *Service) samlRuntime(ctx context.Context, issuer, providerID string) (domainprovider.Provider, SAMLSigningMaterial, domainprovider.SAMLServiceProvider, error) {
	if s.saml == nil {
		return domainprovider.Provider{}, SAMLSigningMaterial{}, domainprovider.SAMLServiceProvider{}, fmt.Errorf("%w: SAML IdP runtime is not configured", apperrors.ErrUnsupportedOperation)
	}
	repository, ok := s.repo.(samlProviderRepository)
	if !ok {
		return domainprovider.Provider{}, SAMLSigningMaterial{}, domainprovider.SAMLServiceProvider{}, fmt.Errorf("%w: SAML provider repository is not configured", apperrors.ErrUnsupportedOperation)
	}
	provider, err := s.repo.GetProvider(ctx, providerID)
	if err != nil || provider.Type != domainprovider.ProviderTypeSAML {
		return domainprovider.Provider{}, SAMLSigningMaterial{}, domainprovider.SAMLServiceProvider{}, fmt.Errorf("%w: SAML provider not found", apperrors.ErrNotFound)
	}
	serviceProvider, err := repository.GetSAMLServiceProvider(ctx, provider.ID)
	if err != nil {
		return provider, SAMLSigningMaterial{}, serviceProvider, err
	}
	key, err := repository.GetActiveSAMLSigningKey(ctx, provider.ID)
	if err != nil {
		return provider, SAMLSigningMaterial{}, serviceProvider, err
	}
	metadataKeys, err := repository.ListSAMLMetadataSigningKeys(ctx, provider.ID, time.Now().UTC())
	if err != nil {
		return provider, SAMLSigningMaterial{}, serviceProvider, err
	}
	additional := make([]string, 0, len(metadataKeys))
	for _, candidate := range metadataKeys {
		if candidate.ID != key.ID {
			additional = append(additional, candidate.CertificatePEM)
		}
	}
	privateKey, err := secretcrypto.DecryptStringWithKeyring(s.encryptionKeys, key.EncryptedPrivateKey)
	if err != nil {
		return provider, SAMLSigningMaterial{}, serviceProvider, fmt.Errorf("decrypt SAML signing key: %w", err)
	}
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	return provider, SAMLSigningMaterial{
		PrivateKeyPEM: privateKey, CertificatePEM: key.CertificatePEM,
		AdditionalCertificatesPEM: additional,
		MetadataURL:               issuer + "/saml2/idp/" + provider.ID + "/metadata",
		SSOURL:                    issuer + "/saml2/idp/" + provider.ID + "/sso",
	}, serviceProvider, nil
}

func (s *Service) validateSAMLProviderAccess(ctx context.Context, principal domainidentity.Principal, application domainportal.Application, sessionID string) error {
	if application.Status != domainportal.ApplicationStatusEnabled {
		return fmt.Errorf("%w: application is disabled", apperrors.ErrAccessDenied)
	}
	access := domainportal.AccessPolicyContext{SourceIP: requestctx.FromContext(ctx).SourceIP, Now: time.Now().UTC()}
	if sessionID != "" && s.users != nil {
		if session, err := s.users.GetAuthSessionByID(ctx, sessionID); err == nil {
			access.MFAAuthenticated = metadataMFA(session.Metadata)
		}
	}
	return applicationPolicyAccessError(principal, application, access)
}

func samlNameID(principal domainidentity.Principal, format string) string {
	if strings.Contains(strings.ToLower(format), "email") && principal.Email != "" {
		return principal.Email
	}
	return principal.UserID
}

func samlAttributes(principal domainidentity.Principal, mappings map[string]string) map[string][]string {
	claims := map[string][]string{
		"email": {principal.Email}, "name": {principal.UserName}, "roles": principal.Roles,
		"teams": principal.Teams, "projects": principal.Projects, "tags": principal.Tags,
	}
	if len(mappings) == 0 {
		mappings = map[string]string{"email": "email", "name": "name", "roles": "roles"}
	}
	keys := make([]string, 0, len(mappings))
	for claim := range mappings {
		keys = append(keys, claim)
	}
	sort.Strings(keys)
	result := map[string][]string{}
	for _, claim := range keys {
		attribute := strings.TrimSpace(mappings[claim])
		if values, ok := claims[claim]; ok && attribute != "" {
			result[attribute] = append([]string(nil), values...)
		}
	}
	return result
}
