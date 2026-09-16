package identityprovider

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"errors"
	"net/url"
	"slices"
	"testing"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestProxyLoginUsesPublicSohaURLAndPreservesReturnQuery(t *testing.T) {
	service := &Service{}
	service.SetPublicAccessURL(func() string { return "https://sso.example.com/" })
	original := "https://app.example.com/dashboard?first=1&second=2"
	target, err := url.Parse(service.proxyLoginURL("provider-1", original))
	ssoNoError(t, err)
	ssoCheck(t, target.Host == "sso.example.com" && target.Path == "/api/v1/provider/proxy/start" && target.Query().Get("provider_id") == "provider-1" && target.Query().Get("return_to") == original, "login URL = %s", target)
}

func TestProviderSetupUsesConfiguredEndpoints(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	service := New(repo, &memoryUsers{}, identityProviderTestPermissions(), nil, "test-encryption-key-32-bytes-long")
	principal := (&memoryUsers{}).principal()
	setup, err := service.GetProviderSetup(ctx, principal, repo.provider.ID, "https://sso.example.com/")
	ssoNoError(t, err)
	ssoCheck(t, setup.Endpoints.Issuer == "https://sso.example.com" && setup.Endpoints.DiscoveryURL == "https://sso.example.com/.well-known/openid-configuration" && setup.Endpoints.JwksURL == "https://sso.example.com/oauth2/jwks", "OIDC endpoints = %#v", setup.Endpoints)
	setup, err = service.GetProviderSetup(ctx, principal, repo.provider.ID, "")
	ssoCheck(t, err == nil && setup.Endpoints.Issuer == "" && slices.Contains(setup.Issues, "public_url_unconfigured"), "unconfigured public URL = %#v, %v", setup, err)
	if _, err := service.GetProviderSetup(ctx, domainidentity.Principal{UserID: "viewer"}, repo.provider.ID, "https://sso.example.com"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("missing permission = %v", err)
	}
	repo.provider.Type = domainprovider.ProviderTypeSAML
	repo.provider.Config = map[string]any{"entityId": "https://sp.example.com", "assertionConsumerServiceUrls": []string{"https://sp.example.com/acs"}}
	setup, err = service.GetProviderSetup(ctx, principal, repo.provider.ID, "https://sso.example.com")
	ssoCheck(t, err == nil && setup.ConfigurationStatus == "complete" && setup.Endpoints.SamlMetadataURL == "https://sso.example.com/saml2/idp/"+repo.provider.ID+"/metadata", "SAML setup = %#v, %v", setup, err)
}

func TestProxySetupNeverFallsBackFromSelectedRemoteOutpost(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.provider.Type = domainprovider.ProviderTypeProxy
	repo.provider.Config = map[string]any{"mode": "forward_auth", "externalHosts": []string{"app.example.com"}, "outpostId": "edge"}
	repo.outposts["edge"] = domainprovider.Outpost{ID: "edge", Name: "Remote edge", Mode: "external", Endpoint: "https://legacy-management.example.com"}
	service := New(repo, &memoryUsers{}, identityProviderTestPermissions(), nil, "test-encryption-key-32-bytes-long")
	seed := sha256.Sum256([]byte("setup-test"))
	service.SetOutpostSigningKey("test-key", ed25519.NewKeyFromSeed(seed[:]))
	principal := (&memoryUsers{}).principal()
	setup, err := service.GetProviderSetup(ctx, principal, repo.provider.ID, "https://sso.example.com")
	ssoCheck(t, err == nil && setup.Endpoints.ForwardAuthURL == "" && setup.RequiresOutpostToken && slices.Contains(setup.Issues, "outpost_forward_auth_url_missing"), "missing remote address = %#v, %v", setup, err)
	outpost := repo.outposts["edge"]
	outpost.ForwardAuthURL = "https://auth.example.com/custom/outpost/forward-auth"
	repo.outposts["edge"] = outpost
	setup, err = service.GetProviderSetup(ctx, principal, repo.provider.ID, "https://sso.example.com")
	ssoCheck(t, err == nil && setup.ConfigurationStatus == "complete" && setup.Endpoints.ForwardAuthURL == outpost.ForwardAuthURL && setup.OutpostRuntimeStatus == "unavailable" && setup.OutpostRuntimeReason == "awaiting_registration", "configured remote = %#v, %v", setup, err)
	setup, err = service.GetProviderSetup(ctx, principal, repo.provider.ID, "http://sso.example.com")
	ssoCheck(t, err == nil && setup.ConfigurationStatus == "incomplete" && slices.Contains(setup.Issues, "remote_public_url_requires_https"), "remote HTTP login would be rejected by Agent: %#v, %v", setup, err)
	repo.provider.Config["headerMappings"] = map[string]string{"email": "X-Auth-Email"}
	setup, err = service.GetProviderSetup(ctx, principal, repo.provider.ID, "https://sso.example.com")
	ssoCheck(t, err == nil && slices.Contains(setup.Issues, "outpost_identity_header_unsupported"), "unsupported remote header not diagnosed: %#v, %v", setup, err)
	repo.provider.Config["headerMappings"] = map[string]string{"email": "X-Auth-Request-Email"}
	setup, err = service.GetProviderSetup(ctx, principal, repo.provider.ID, "https://sso.example.com")
	ssoCheck(t, err == nil && setup.ConfigurationStatus == "complete", "supported remote header rejected: %#v, %v", setup, err)
	outpost.Mode = "embedded"
	repo.outposts["edge"] = outpost
	setup, err = service.GetProviderSetup(ctx, principal, repo.provider.ID, "https://sso.example.com")
	ssoCheck(t, err == nil && !setup.RequiresOutpostToken && setup.Endpoints.ForwardAuthURL == "https://sso.example.com/api/v1/provider/proxy/auth?provider_id="+repo.provider.ID, "embedded setup = %#v, %v", setup, err)
	delete(repo.outposts, "edge")
	setup, err = service.GetProviderSetup(ctx, principal, repo.provider.ID, "https://sso.example.com")
	ssoCheck(t, err == nil && setup.Endpoints.ForwardAuthURL == "" && slices.Contains(setup.Issues, "outpost_missing"), "deleted outpost = %#v, %v", setup, err)
}
