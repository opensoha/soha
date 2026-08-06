package settings

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) ListSAMLLoginSources(ctx context.Context, principal domainidentity.Principal) ([]sohaapi.SAMLLoginSource, error) {
	settings, err := s.GetIdentitySettings(ctx, principal)
	if err != nil {
		return nil, err
	}
	items := make([]sohaapi.SAMLLoginSource, 0)
	for _, provider := range settings.Providers {
		if provider.Type == "saml" {
			item, mapErr := s.samlSource(ctx, provider)
			if mapErr != nil {
				return nil, mapErr
			}
			items = append(items, item)
		}
	}
	return items, nil
}

func (s *Service) GetSAMLLoginSource(ctx context.Context, principal domainidentity.Principal, id string) (sohaapi.SAMLLoginSource, error) {
	provider, err := s.samlProvider(ctx, principal, id)
	if err != nil {
		return sohaapi.SAMLLoginSource{}, err
	}
	return s.samlSource(ctx, provider)
}

func (s *Service) CreateSAMLLoginSource(ctx context.Context, principal domainidentity.Principal, input sohaapi.SAMLLoginSourceInput) (sohaapi.SAMLLoginSource, error) {
	return s.saveSAMLLoginSource(ctx, principal, "", input)
}

func (s *Service) UpdateSAMLLoginSource(ctx context.Context, principal domainidentity.Principal, id string, input sohaapi.SAMLLoginSourceInput) (sohaapi.SAMLLoginSource, error) {
	return s.saveSAMLLoginSource(ctx, principal, strings.TrimSpace(id), input)
}

func (s *Service) DeleteSAMLLoginSource(ctx context.Context, principal domainidentity.Principal, id string) error {
	settings, err := s.GetIdentitySettings(ctx, principal)
	if err != nil {
		return err
	}
	providers := make([]domainsettings.LoginProviderSettings, 0, len(settings.Providers))
	found := false
	for _, provider := range settings.Providers {
		if provider.ID == strings.TrimSpace(id) && provider.Type == "saml" {
			found = true
			continue
		}
		providers = append(providers, provider)
	}
	if !found {
		return fmt.Errorf("%w: SAML login source not found", apperrors.ErrNotFound)
	}
	defaultID := settings.DefaultProviderID
	if defaultID == id {
		defaultID = ""
	}
	_, err = s.UpdateLoginProvidersSettings(ctx, principal, providers, defaultID, settings.LocalPasswordLoginEnabled)
	return err
}

func (s *Service) ValidateSAMLMetadata(ctx context.Context, principal domainidentity.Principal, input sohaapi.SAMLMetadataInput) (sohaapi.SAMLMetadataValidation, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermSettingsIdentityManage, "validate")); err != nil {
		return sohaapi.SAMLMetadataValidation{}, err
	}
	if s.saml == nil {
		return sohaapi.SAMLMetadataValidation{}, fmt.Errorf("%w: SAML metadata importer is not configured", apperrors.ErrUnsupportedOperation)
	}
	result, _, err := s.saml.ValidateMetadata(ctx, input)
	return result, err
}

func (s *Service) ImportSAMLLoginSourceMetadata(ctx context.Context, principal domainidentity.Principal, request sohaapi.SAMLMetadataImportRequest) (sohaapi.SAMLLoginSource, error) {
	if err := s.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermSettingsIdentityManage, "create")); err != nil {
		return sohaapi.SAMLLoginSource{}, err
	}
	if s.saml == nil {
		return sohaapi.SAMLLoginSource{}, fmt.Errorf("%w: SAML metadata importer is not configured", apperrors.ErrUnsupportedOperation)
	}
	validation, raw, err := s.saml.ValidateMetadata(ctx, request.Metadata)
	if err != nil {
		return sohaapi.SAMLLoginSource{}, err
	}
	if len(validation.SingleSignOnUrls) == 0 {
		return sohaapi.SAMLLoginSource{}, fmt.Errorf("%w: metadata has no SSO URL", apperrors.ErrInvalidArgument)
	}
	if request.Status == sohaapi.IdentityResourceStatusActive {
		return sohaapi.SAMLLoginSource{}, fmt.Errorf("%w: imported SAML sources must be saved disabled and configured with an entity ID and ACS URL before activation", apperrors.ErrInvalidArgument)
	}
	provider := domainsettings.LoginProviderSettings{
		ID: uuid.NewString(), Name: strings.TrimSpace(request.Name), Type: "saml",
		Enabled:     false,
		MetadataURL: request.Metadata.URL, MetadataXML: raw,
		FrontendRedirectURL: "/auth/callback",
	}
	applySAMLAttributeMappings(&provider, request.AttributeMappings)
	settings, err := s.GetIdentitySettings(ctx, principal)
	if err != nil {
		return sohaapi.SAMLLoginSource{}, err
	}
	settings.Providers = append(settings.Providers, provider)
	if _, err := s.UpdateLoginProvidersSettings(ctx, principal, settings.Providers, settings.DefaultProviderID, settings.LocalPasswordLoginEnabled); err != nil {
		return sohaapi.SAMLLoginSource{}, err
	}
	return s.samlSource(ctx, provider)
}

func (s *Service) saveSAMLLoginSource(ctx context.Context, principal domainidentity.Principal, id string, input sohaapi.SAMLLoginSourceInput) (sohaapi.SAMLLoginSource, error) {
	if input.WantAuthnRequestsSigned {
		return sohaapi.SAMLLoginSource{}, fmt.Errorf("%w: signed SAML AuthnRequests require a configured signing-key runtime", apperrors.ErrUnsupportedOperation)
	}
	settings, err := s.GetIdentitySettings(ctx, principal)
	if err != nil {
		return sohaapi.SAMLLoginSource{}, err
	}
	provider := samlProviderFromContract(id, input)
	if provider.ID == "" {
		provider.ID = uuid.NewString()
	}
	found := id == ""
	for index := range settings.Providers {
		if settings.Providers[index].ID == id && settings.Providers[index].Type == "saml" {
			if settings.Providers[index].MetadataURL == provider.MetadataURL {
				provider.MetadataXML = settings.Providers[index].MetadataXML
			}
			settings.Providers[index] = provider
			found = true
			break
		}
	}
	if !found {
		return sohaapi.SAMLLoginSource{}, fmt.Errorf("%w: SAML login source not found", apperrors.ErrNotFound)
	}
	if id == "" {
		settings.Providers = append(settings.Providers, provider)
	}
	updated, err := s.UpdateLoginProvidersSettings(ctx, principal, settings.Providers, settings.DefaultProviderID, settings.LocalPasswordLoginEnabled)
	if err != nil {
		return sohaapi.SAMLLoginSource{}, err
	}
	for _, candidate := range updated.Providers {
		if candidate.ID == provider.ID {
			return s.samlSource(ctx, candidate)
		}
	}
	return sohaapi.SAMLLoginSource{}, fmt.Errorf("%w: SAML login source not found after update", apperrors.ErrNotFound)
}

func samlProviderFromContract(id string, input sohaapi.SAMLLoginSourceInput) domainsettings.LoginProviderSettings {
	provider := domainsettings.LoginProviderSettings{
		ID: id, Name: input.Name, Type: "saml", Enabled: input.Status == sohaapi.IdentityResourceStatusActive,
		MetadataURL: input.IdpMetadataURL, EntityID: input.EntityID, RedirectURL: input.AcsURL,
		FrontendRedirectURL: "/auth/callback",
	}
	applySAMLAttributeMappings(&provider, input.AttributeMappings)
	return provider
}

func applySAMLAttributeMappings(provider *domainsettings.LoginProviderSettings, mappings []sohaapi.SAMLAttributeMapping) {
	for _, mapping := range mappings {
		switch mapping.Target {
		case sohaapi.Email:
			provider.EmailField = mapping.Source
		case sohaapi.Username, sohaapi.DisplayName:
			provider.UserNameField = mapping.Source
		case sohaapi.Role:
			provider.RoleField = mapping.Source
		case sohaapi.Organization:
			provider.OrganizationField = mapping.Source
		case sohaapi.Subject:
			provider.UserIDField = mapping.Source
		}
	}
}

func (s *Service) samlProvider(ctx context.Context, principal domainidentity.Principal, id string) (domainsettings.LoginProviderSettings, error) {
	settings, err := s.GetIdentitySettings(ctx, principal)
	if err != nil {
		return domainsettings.LoginProviderSettings{}, err
	}
	for _, provider := range settings.Providers {
		if provider.ID == strings.TrimSpace(id) && provider.Type == "saml" {
			return provider, nil
		}
	}
	return domainsettings.LoginProviderSettings{}, fmt.Errorf("%w: SAML login source not found", apperrors.ErrNotFound)
}

func (s *Service) samlSource(ctx context.Context, provider domainsettings.LoginProviderSettings) (sohaapi.SAMLLoginSource, error) {
	if s.saml == nil {
		return sohaapi.SAMLLoginSource{}, fmt.Errorf("%w: SAML metadata importer is not configured", apperrors.ErrUnsupportedOperation)
	}
	validation, _, err := s.saml.ValidateMetadata(ctx, sohaapi.SAMLMetadataInput{Source: sohaapi.XML, XML: provider.MetadataXML})
	if err != nil {
		return sohaapi.SAMLLoginSource{}, err
	}
	ssoURL := ""
	if len(validation.SingleSignOnUrls) > 0 {
		ssoURL = validation.SingleSignOnUrls[0]
	}
	now := time.Now().UTC()
	return sohaapi.SAMLLoginSource{
		ID: provider.ID, Name: provider.Name, Status: samlStatus(provider.Enabled),
		EntityID: provider.EntityID, AcsURL: provider.RedirectURL, IdpMetadataURL: provider.MetadataURL,
		SingleSignOnURL: ssoURL, NameIDFormat: sohaapi.Unspecified,
		AllowedClockSkewSeconds: 120, Certificates: validation.Certificates,
		AttributeMappings: samlMappings(provider), CreatedAt: now, UpdatedAt: now,
	}, nil
}

func samlStatus(enabled bool) sohaapi.IdentityResourceStatus {
	if enabled {
		return sohaapi.IdentityResourceStatusActive
	}
	return sohaapi.IdentityResourceStatusDisabled
}

func samlMappings(provider domainsettings.LoginProviderSettings) []sohaapi.SAMLAttributeMapping {
	items := make([]sohaapi.SAMLAttributeMapping, 0, 5)
	for _, item := range []struct {
		source string
		target sohaapi.SAMLAttributeMappingTarget
	}{{provider.UserIDField, sohaapi.Subject}, {provider.EmailField, sohaapi.Email}, {provider.UserNameField, sohaapi.Username}, {provider.RoleField, sohaapi.Role}, {provider.OrganizationField, sohaapi.Organization}} {
		if item.source != "" {
			items = append(items, sohaapi.SAMLAttributeMapping{Source: item.source, Target: item.target})
		}
	}
	return items
}
