package identityprovider

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"time"

	contractauth "github.com/opensoha/soha-contracts/auth"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) GetProviderSetup(ctx context.Context, principal domainidentity.Principal, providerID, publicURL string) (sohaapi.IdentityProviderSetup, error) {
	provider, err := s.GetProvider(ctx, principal, providerID)
	if err != nil {
		return sohaapi.IdentityProviderSetup{}, err
	}
	setup := sohaapi.IdentityProviderSetup{ProviderID: provider.ID, ConfigurationStatus: "complete", Issues: []string{}}
	base, err := normalizeOutpostForwardAuthURL(publicURL)
	if err != nil || base == "" {
		setup.Issues = append(setup.Issues, "public_url_unconfigured")
		base = ""
	}
	base = strings.TrimRight(base, "/")
	switch provider.Type {
	case domainprovider.ProviderTypeOIDC:
		err = s.oidcProviderSetup(ctx, provider, base, &setup)
	case domainprovider.ProviderTypeSAML:
		if validateSAMLProviderConfig(provider.Config) != nil {
			setup.Issues = append(setup.Issues, "saml_sp_configuration_incomplete")
		}
		if base != "" {
			setup.Endpoints.SamlMetadataURL = base + "/saml2/idp/" + url.PathEscape(provider.ID) + "/metadata"
			setup.Endpoints.SamlSSOUrl = base + "/saml2/idp/" + url.PathEscape(provider.ID) + "/sso"
		}
	case domainprovider.ProviderTypeProxy:
		err = s.proxyProviderSetup(ctx, provider, base, &setup)
	}
	if err != nil {
		return sohaapi.IdentityProviderSetup{}, err
	}
	if len(setup.Issues) > 0 {
		setup.ConfigurationStatus = "incomplete"
	}
	return setup, nil
}

func (s *Service) oidcProviderSetup(ctx context.Context, provider domainprovider.Provider, base string, setup *sohaapi.IdentityProviderSetup) error {
	clients, err := s.repo.ListOIDCClients(ctx, provider.ID)
	if err != nil {
		return err
	}
	if len(clients) == 0 {
		setup.Issues = append(setup.Issues, "oidc_client_missing")
	}
	if base == "" {
		return nil
	}
	discovery := s.Discovery(base)
	setup.Endpoints = sohaapi.IdentityProviderSetupEndpoints{
		Issuer: discovery.Issuer, DiscoveryURL: base + "/.well-known/openid-configuration",
		AuthorizationURL: discovery.AuthorizationEndpoint, TokenURL: discovery.TokenEndpoint,
		UserInfoURL: discovery.UserInfoEndpoint, JwksURL: discovery.JWKSURI, LogoutURL: discovery.EndSessionEndpoint,
	}
	return nil
}

func (s *Service) proxyProviderSetup(ctx context.Context, provider domainprovider.Provider, base string, setup *sohaapi.IdentityProviderSetup) error {
	if len(configStringSlice(provider.Config, "externalHosts", "external_hosts", "hosts")) == 0 {
		setup.Issues = append(setup.Issues, "proxy_external_host_missing")
	}
	if base != "" {
		setup.Endpoints.LoginURL = base + "/api/v1/provider/proxy/start?provider_id=" + url.QueryEscape(provider.ID)
	}
	setup.MigrationRequired = configString(provider.Config, "mode") == domainprovider.ProxyModeReverseProxy
	if setup.MigrationRequired && base != "" {
		setup.Endpoints.LegacyReverseProxyURL = base + "/api/v1/provider/proxy/reverse/" + url.PathEscape(provider.ID)
	}
	outpostID := configString(provider.Config, "outpostId", "outpost_id")
	if outpostID == "" {
		if base != "" {
			setup.Endpoints.ForwardAuthURL = base + "/api/v1/provider/proxy/auth?provider_id=" + url.QueryEscape(provider.ID)
		}
		return nil
	}
	outpost, err := s.repo.GetOutpost(ctx, outpostID)
	if errors.Is(err, apperrors.ErrNotFound) {
		setup.Issues = append(setup.Issues, "outpost_missing")
		return nil
	}
	if err != nil {
		return err
	}
	outpost = s.outpostManagementView(outpost, time.Now().UTC())
	setup.OutpostID, setup.OutpostName = outpost.ID, outpost.Name
	setup.OutpostRuntimeStatus = sohaapi.IdentityProviderSetupOutpostRuntimeStatus(outpost.RuntimeStatus)
	setup.OutpostRuntimeReason = outpost.RuntimeReason
	if outpost.Mode == domainprovider.OutpostModeEmbedded {
		if base != "" {
			setup.Endpoints.ForwardAuthURL = base + "/api/v1/provider/proxy/auth?provider_id=" + url.QueryEscape(provider.ID)
		}
		return nil
	}
	setup.RequiresOutpostToken = true
	for _, header := range configStringMap(provider.Config, "headerMappings", "header_mappings") {
		if header != "" && !contractauth.IsOutpostIdentityHeader(header) {
			setup.Issues = append(setup.Issues, "outpost_identity_header_unsupported")
			break
		}
	}
	if base != "" && !strings.HasPrefix(base, "https://") {
		setup.Issues = append(setup.Issues, "remote_public_url_requires_https")
	}
	setup.Endpoints.ForwardAuthURL, err = normalizeOutpostForwardAuthURL(outpost.ForwardAuthURL)
	if err != nil || setup.Endpoints.ForwardAuthURL == "" {
		setup.Endpoints.ForwardAuthURL = ""
		setup.Issues = append(setup.Issues, "outpost_forward_auth_url_missing")
	}
	if s.requireOutpostRuntimeSigner() != nil {
		setup.Issues = append(setup.Issues, "signing_key_unconfigured")
	}
	return nil
}
