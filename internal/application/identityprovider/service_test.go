package identityprovider

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	domainportal "github.com/opensoha/soha/internal/domain/providerportal"
	"github.com/opensoha/soha/internal/platform/apperrors"
	userrepo "github.com/opensoha/soha/internal/repository/user"
	"golang.org/x/crypto/bcrypt"
)

func TestServiceOIDCAuthorizationCodeFlow(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	users := &memoryUsers{}
	service := New(repo, users, nil, nil, "test-encryption-key-32-bytes-long")

	discovery := service.Discovery("https://soha.example/")
	expectOIDC(t, discovery.IntrospectionEndpoint == "https://soha.example/oauth2/introspect", "introspection endpoint = %q", discovery.IntrospectionEndpoint)
	expectOIDC(t, discovery.RevocationEndpoint == "https://soha.example/oauth2/revoke", "revocation endpoint = %q", discovery.RevocationEndpoint)

	verifier := "test-verifier-value"
	challenge := pkceChallenge(verifier)
	authorize, err := service.Authorize(ctx, "https://soha.example", users.principal(), domainprovider.AuthorizeInput{
		ResponseType:        "code",
		ClientID:            "client-1",
		RedirectURI:         "https://app.example/callback",
		Scope:               "openid profile email roles",
		State:               "state-1",
		Nonce:               "nonce-1",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
	})
	expectOIDC(t, err == nil, "Authorize returned error: %v", err)
	expectOIDC(t, authorize.Code != "", "Authorize code is empty")
	expectOIDC(t, authorize.State == "state-1", "Authorize state = %q", authorize.State)
	expectOIDC(t, authorize.RedirectURI == "https://app.example/callback", "Authorize redirect = %q", authorize.RedirectURI)

	token, err := service.Token(ctx, "https://soha.example", domainprovider.TokenInput{
		GrantType:    "authorization_code",
		Code:         authorize.Code,
		RedirectURI:  "https://app.example/callback",
		ClientID:     "client-1",
		ClientSecret: "secret-1",
		CodeVerifier: verifier,
	})
	expectOIDC(t, err == nil, "Token returned error: %v", err)
	expectOIDC(t, token.AccessToken != "", "access token is empty")
	expectOIDC(t, token.IDToken != "", "ID token is empty")
	expectOIDC(t, token.TokenType == "Bearer", "token type = %q", token.TokenType)

	jwks, err := service.JWKS(ctx)
	expectOIDC(t, err == nil, "JWKS returned error: %v", err)
	expectOIDC(t, len(jwks.Keys) == 1, "JWKS keys = %#v", jwks.Keys)
	expectOIDC(t, jwks.Keys[0]["kid"] != "", "JWKS kid is empty")
	expectOIDC(t, jwks.Keys[0]["alg"] == "ES256", "JWKS alg = %#v", jwks.Keys[0]["alg"])

	userInfo, err := service.UserInfo(ctx, "https://soha.example", "Bearer "+token.AccessToken)
	expectOIDC(t, err == nil, "UserInfo returned error: %v", err)
	expectOIDC(t, userInfo.Subject == "user-1", "UserInfo subject = %q", userInfo.Subject)
	expectOIDC(t, userInfo.Email == "ada@example.com", "UserInfo email = %q", userInfo.Email)
	expectOIDC(t, len(userInfo.Roles) == 1, "UserInfo roles = %#v", userInfo.Roles)
	expectOIDC(t, userInfo.Roles[0] == "admin", "UserInfo roles = %#v", userInfo.Roles)

	introspection, err := service.Introspect(ctx, "https://soha.example", token.AccessToken, domainprovider.ClientAuthInput{
		ClientID:     "client-1",
		ClientSecret: "secret-1",
	})
	expectOIDC(t, err == nil, "Introspect returned error: %v", err)
	expectOIDC(t, introspection.Active, "Introspect is inactive")
	expectOIDC(t, introspection.Subject == "user-1", "Introspect subject = %q", introspection.Subject)
	expectOIDC(t, introspection.ClientID == "client-1", "Introspect client = %q", introspection.ClientID)
	expectOIDC(t, introspection.TokenType == "Bearer", "Introspect token type = %q", introspection.TokenType)
	inactive, err := service.Introspect(ctx, "https://soha.example", "invalid-token", domainprovider.ClientAuthInput{
		ClientID:     "client-1",
		ClientSecret: "secret-1",
	})
	expectOIDC(t, err == nil, "inactive Introspect returned error: %v", err)
	expectOIDC(t, !inactive.Active, "inactive token reported active")

	err = service.Revoke(ctx, "https://soha.example", token.AccessToken, domainprovider.ClientAuthInput{
		ClientID:     "client-1",
		ClientSecret: "secret-1",
	})
	expectOIDC(t, err == nil, "Revoke returned error: %v", err)

	_, err = service.Token(ctx, "https://soha.example", domainprovider.TokenInput{
		GrantType:    "authorization_code",
		Code:         authorize.Code,
		RedirectURI:  "https://app.example/callback",
		ClientID:     "client-1",
		ClientSecret: "secret-1",
		CodeVerifier: verifier,
	})
	expectOIDC(t, errors.Is(err, apperrors.ErrUnauthorized), "second Token error = %v, want unauthorized", err)
}

func expectOIDC(t *testing.T, condition bool, format string, args ...any) {
	t.Helper()
	if !condition {
		t.Fatalf(format, args...)
	}
}

func TestServiceOIDCIntrospectRequiresClientAuthentication(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	users := &memoryUsers{}
	service := New(repo, users, nil, nil, "test-encryption-key-32-bytes-long")

	authorize, err := service.Authorize(ctx, "https://soha.example", users.principal(), domainprovider.AuthorizeInput{
		ResponseType:        "code",
		ClientID:            "client-1",
		RedirectURI:         "https://app.example/callback",
		Scope:               "openid",
		CodeChallenge:       pkceChallenge("verifier"),
		CodeChallengeMethod: "S256",
	})
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	token, err := service.Token(ctx, "https://soha.example", domainprovider.TokenInput{
		GrantType:    "authorization_code",
		Code:         authorize.Code,
		RedirectURI:  "https://app.example/callback",
		ClientID:     "client-1",
		ClientSecret: "secret-1",
		CodeVerifier: "verifier",
	})
	if err != nil {
		t.Fatalf("Token returned error: %v", err)
	}

	if _, err := service.Introspect(ctx, "https://soha.example", token.AccessToken, domainprovider.ClientAuthInput{
		ClientID:     "client-1",
		ClientSecret: "wrong-secret",
	}); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("Introspect error = %v, want unauthorized", err)
	}

	if err := service.Revoke(ctx, "https://soha.example", token.AccessToken, domainprovider.ClientAuthInput{
		ClientID:     "client-1",
		ClientSecret: "wrong-secret",
	}); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("Revoke error = %v, want unauthorized", err)
	}
}

func TestServiceOIDCIntrospectReturnsInactiveForOtherClientToken(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	users := &memoryUsers{}
	service := New(repo, users, nil, nil, "test-encryption-key-32-bytes-long")

	authorize, err := service.Authorize(ctx, "https://soha.example", users.principal(), domainprovider.AuthorizeInput{
		ResponseType:        "code",
		ClientID:            "client-1",
		RedirectURI:         "https://app.example/callback",
		Scope:               "openid",
		CodeChallenge:       pkceChallenge("verifier"),
		CodeChallengeMethod: "S256",
	})
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	token, err := service.Token(ctx, "https://soha.example", domainprovider.TokenInput{
		GrantType:    "authorization_code",
		Code:         authorize.Code,
		RedirectURI:  "https://app.example/callback",
		ClientID:     "client-1",
		ClientSecret: "secret-1",
		CodeVerifier: "verifier",
	})
	if err != nil {
		t.Fatalf("Token returned error: %v", err)
	}

	otherSecretHash, err := bcrypt.GenerateFromPassword([]byte("secret-2"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash other secret: %v", err)
	}
	repo.client.ID = "oidc-client-2"
	repo.client.ClientID = "client-2"
	repo.client.ClientSecretHash = string(otherSecretHash)

	introspection, err := service.Introspect(ctx, "https://soha.example", token.AccessToken, domainprovider.ClientAuthInput{
		ClientID:     "client-2",
		ClientSecret: "secret-2",
	})
	if err != nil {
		t.Fatalf("Introspect returned error: %v", err)
	}
	if introspection.Active {
		t.Fatalf("Introspect = %#v, want inactive for token issued to another client", introspection)
	}
}

func TestServiceOIDCPKCERejectsInvalidVerifier(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	users := &memoryUsers{}
	service := New(repo, users, nil, nil, "test-encryption-key-32-bytes-long")

	authorize, err := service.Authorize(ctx, "https://soha.example", users.principal(), domainprovider.AuthorizeInput{
		ResponseType:        "code",
		ClientID:            "client-1",
		RedirectURI:         "https://app.example/callback",
		Scope:               "openid",
		CodeChallenge:       pkceChallenge("correct-verifier"),
		CodeChallengeMethod: "S256",
	})
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}

	if _, err := service.Token(ctx, "https://soha.example", domainprovider.TokenInput{
		GrantType:    "authorization_code",
		Code:         authorize.Code,
		RedirectURI:  "https://app.example/callback",
		ClientID:     "client-1",
		ClientSecret: "secret-1",
		CodeVerifier: "wrong-verifier",
	}); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("Token error = %v, want unauthorized", err)
	}

	token, err := service.Token(ctx, "https://soha.example", domainprovider.TokenInput{
		GrantType:    "authorization_code",
		Code:         authorize.Code,
		RedirectURI:  "https://app.example/callback",
		ClientID:     "client-1",
		ClientSecret: "secret-1",
		CodeVerifier: "correct-verifier",
	})
	if err != nil {
		t.Fatalf("Token after rejected verifier returned error: %v", err)
	}
	if token.AccessToken == "" {
		t.Fatalf("Token after rejected verifier = %#v", token)
	}
}

func TestServiceOIDCClientSecretRejectsInvalidWithoutConsumingCode(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	users := &memoryUsers{}
	service := New(repo, users, nil, nil, "test-encryption-key-32-bytes-long")

	verifier := "correct-verifier"
	authorize, err := service.Authorize(ctx, "https://soha.example", users.principal(), domainprovider.AuthorizeInput{
		ResponseType:        "code",
		ClientID:            "client-1",
		RedirectURI:         "https://app.example/callback",
		Scope:               "openid",
		CodeChallenge:       pkceChallenge(verifier),
		CodeChallengeMethod: "S256",
	})
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}

	if _, err := service.Token(ctx, "https://soha.example", domainprovider.TokenInput{
		GrantType:    "authorization_code",
		Code:         authorize.Code,
		RedirectURI:  "https://app.example/callback",
		ClientID:     "client-1",
		ClientSecret: "wrong-secret",
		CodeVerifier: verifier,
	}); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("Token error = %v, want unauthorized", err)
	}

	token, err := service.Token(ctx, "https://soha.example", domainprovider.TokenInput{
		GrantType:    "authorization_code",
		Code:         authorize.Code,
		RedirectURI:  "https://app.example/callback",
		ClientID:     "client-1",
		ClientSecret: "secret-1",
		CodeVerifier: verifier,
	})
	if err != nil {
		t.Fatalf("Token after rejected secret returned error: %v", err)
	}
	if token.AccessToken == "" {
		t.Fatalf("Token after rejected secret = %#v", token)
	}
}

func TestServiceOIDCAuthorizeRejectsUnregisteredRedirectURI(t *testing.T) {
	ctx := context.Background()
	service := New(newMemoryRepo(t), &memoryUsers{}, nil, nil, "test-encryption-key-32-bytes-long")
	_, err := service.Authorize(ctx, "https://soha.example", (&memoryUsers{}).principal(), domainprovider.AuthorizeInput{
		ResponseType:  "code",
		ClientID:      "client-1",
		RedirectURI:   "https://evil.example/callback",
		Scope:         "openid",
		CodeChallenge: pkceChallenge("verifier"),
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("Authorize error = %v, want invalid argument", err)
	}
	var redirectErr *domainprovider.AuthorizeRedirectError
	if errors.As(err, &redirectErr) {
		t.Fatalf("Authorize error = %#v, want non-redirect error", redirectErr)
	}
}

func TestServiceOIDCAuthorizeRejectsInvalidClientWithoutRedirectError(t *testing.T) {
	ctx := context.Background()
	service := New(newMemoryRepo(t), &memoryUsers{}, nil, nil, "test-encryption-key-32-bytes-long")

	_, err := service.Authorize(ctx, "https://soha.example", (&memoryUsers{}).principal(), domainprovider.AuthorizeInput{
		ResponseType:  "token",
		ClientID:      "missing-client",
		RedirectURI:   "https://app.example/callback",
		Scope:         "openid",
		CodeChallenge: pkceChallenge("verifier"),
	})
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("Authorize error = %v, want not found", err)
	}
	var redirectErr *domainprovider.AuthorizeRedirectError
	if errors.As(err, &redirectErr) {
		t.Fatalf("Authorize error = %#v, want non-redirect error", redirectErr)
	}
}

func TestServiceOIDCAuthorizeReturnsRedirectErrorsAfterRegisteredRedirectURI(t *testing.T) {
	tests := []struct {
		name       string
		input      domainprovider.AuthorizeInput
		mutateRepo func(*memoryRepo)
		wantCode   string
		wantIs     error
	}{
		{
			name: "unsupported response type",
			input: domainprovider.AuthorizeInput{
				ResponseType:        "token",
				ClientID:            "client-1",
				RedirectURI:         "https://app.example/callback",
				Scope:               "openid",
				State:               "state-1",
				CodeChallenge:       pkceChallenge("verifier"),
				CodeChallengeMethod: "S256",
			},
			wantCode: "unsupported_response_type",
			wantIs:   apperrors.ErrInvalidArgument,
		},
		{
			name: "invalid scope",
			input: domainprovider.AuthorizeInput{
				ResponseType:        "code",
				ClientID:            "client-1",
				RedirectURI:         "https://app.example/callback",
				Scope:               "openid projects",
				State:               "state-1",
				CodeChallenge:       pkceChallenge("verifier"),
				CodeChallengeMethod: "S256",
			},
			wantCode: "invalid_scope",
			wantIs:   apperrors.ErrInvalidArgument,
		},
		{
			name: "missing pkce challenge",
			input: domainprovider.AuthorizeInput{
				ResponseType: "code",
				ClientID:     "client-1",
				RedirectURI:  "https://app.example/callback",
				Scope:        "openid",
				State:        "state-1",
			},
			wantCode: "invalid_request",
			wantIs:   apperrors.ErrInvalidArgument,
		},
		{
			name: "access denied",
			input: domainprovider.AuthorizeInput{
				ResponseType:        "code",
				ClientID:            "client-1",
				RedirectURI:         "https://app.example/callback",
				Scope:               "openid",
				State:               "state-1",
				CodeChallenge:       pkceChallenge("verifier"),
				CodeChallengeMethod: "S256",
			},
			mutateRepo: func(repo *memoryRepo) {
				repo.app.Assignments = []domainportal.ApplicationAssignment{
					{
						SubjectType: domainportal.AssignmentSubjectUser,
						SubjectID:   "other-user",
						Effect:      domainportal.AssignmentEffectAllow,
					},
				}
			},
			wantCode: "access_denied",
			wantIs:   apperrors.ErrAccessDenied,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			repo := newMemoryRepo(t)
			if tt.mutateRepo != nil {
				tt.mutateRepo(repo)
			}
			service := New(repo, &memoryUsers{}, nil, nil, "test-encryption-key-32-bytes-long")

			_, err := service.Authorize(ctx, "https://soha.example", (&memoryUsers{}).principal(), tt.input)
			if !errors.Is(err, tt.wantIs) {
				t.Fatalf("Authorize error = %v, want %v", err, tt.wantIs)
			}
			var redirectErr *domainprovider.AuthorizeRedirectError
			if !errors.As(err, &redirectErr) {
				t.Fatalf("Authorize error = %v, want AuthorizeRedirectError", err)
			}
			if redirectErr.RedirectURI != "https://app.example/callback" || redirectErr.State != "state-1" || redirectErr.Code != tt.wantCode {
				t.Fatalf("AuthorizeRedirectError = %#v", redirectErr)
			}
			if redirectErr.Description == "" {
				t.Fatalf("AuthorizeRedirectError description is empty")
			}
			if len(repo.codes) != 0 {
				t.Fatalf("authorization codes = %d, want 0", len(repo.codes))
			}
		})
	}
}

func TestServiceOIDCLaunchURLBuildsAuthorizeURL(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.client.RequirePKCE = false
	repo.app.LaunchURL = ""
	repo.app.Metadata = map[string]any{
		"oidc": map[string]any{
			"scopes": []any{"openid", "email"},
		},
	}
	service := New(repo, &memoryUsers{}, nil, nil, "test-encryption-key-32-bytes-long")

	launchURL, err := service.OIDCLaunchURL(ctx, repo.app)
	if err != nil {
		t.Fatalf("OIDCLaunchURL returned error: %v", err)
	}
	parsed, err := url.Parse(launchURL)
	if err != nil {
		t.Fatalf("parse launch URL: %v", err)
	}
	if parsed.Path != "/oauth2/authorize" {
		t.Fatalf("launch path = %q, want /oauth2/authorize", parsed.Path)
	}
	query := parsed.Query()
	if query.Get("response_type") != "code" || query.Get("client_id") != "client-1" {
		t.Fatalf("launch query = %s", parsed.RawQuery)
	}
	if query.Get("redirect_uri") != "https://app.example/callback" {
		t.Fatalf("redirect_uri = %q, want registered callback", query.Get("redirect_uri"))
	}
	if query.Get("scope") != "openid email" {
		t.Fatalf("scope = %q, want metadata scopes", query.Get("scope"))
	}
}

func TestServiceOIDCLaunchURLRejectsPKCERequiredClient(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.app.LaunchURL = ""
	service := New(repo, &memoryUsers{}, nil, nil, "test-encryption-key-32-bytes-long")

	_, err := service.OIDCLaunchURL(ctx, repo.app)
	if !errors.Is(err, apperrors.ErrNotFound) {
		t.Fatalf("OIDCLaunchURL error = %v, want not found for portal launch without non-PKCE client", err)
	}
}

func TestServiceProxyAuthAllowsAndInjectsHeaders(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.provider.Type = domainprovider.ProviderTypeProxy
	repo.provider.Config = map[string]any{
		"externalHost": "grafana.example.com",
		"cookieDomain": ".example.com",
		"headerMappings": map[string]any{
			"userId": "X-Auth-Request-User",
		},
	}
	repo.app.ProviderType = domainportal.ProviderTypeProxy
	repo.app.Status = domainportal.ApplicationStatusEnabled
	repo.app.Assignments = []domainportal.ApplicationAssignment{{
		SubjectType: domainportal.AssignmentSubjectRole,
		SubjectID:   "admin",
		Effect:      domainportal.AssignmentEffectAllow,
	}}
	service := New(repo, &memoryUsers{}, nil, nil, "test-encryption-key-32-bytes-long")

	result, err := service.ProxyAuth(ctx, (&memoryUsers{}).principal(), domainprovider.ProxyAuthInput{
		ForwardedHost:  "grafana.example.com",
		ForwardedProto: "https",
		ForwardedURI:   "/dashboards/db/main",
	})
	if err != nil {
		t.Fatalf("ProxyAuth returned error: %v", err)
	}
	if result.Decision != domainprovider.ProxyDecisionAllow {
		t.Fatalf("ProxyAuth decision = %q, want allow", result.Decision)
	}
	if result.CookieDomain != "example.com" {
		t.Fatalf("ProxyAuth cookie domain = %q, want example.com", result.CookieDomain)
	}
	if result.Headers["X-Auth-Request-User"] != "user-1" {
		t.Fatalf("custom user id header = %q", result.Headers["X-Auth-Request-User"])
	}
	if result.Headers["X-Soha-Roles"] != "admin" {
		t.Fatalf("roles header = %q", result.Headers["X-Soha-Roles"])
	}
}

func TestServiceProxyAuthIgnoresUnsafeCookieDomain(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.provider.Type = domainprovider.ProviderTypeProxy
	repo.provider.Config = map[string]any{
		"externalHost": "grafana.example.com",
		"cookieDomain": "evil.example.net",
	}
	repo.app.ProviderType = domainportal.ProviderTypeProxy
	repo.app.Status = domainportal.ApplicationStatusEnabled
	service := New(repo, &memoryUsers{}, nil, nil, "test-encryption-key-32-bytes-long")

	result, err := service.ProxyAuth(ctx, (&memoryUsers{}).principal(), domainprovider.ProxyAuthInput{
		ForwardedHost: "grafana.example.com",
		ForwardedURI:  "/dashboards",
	})
	if err != nil {
		t.Fatalf("ProxyAuth returned error: %v", err)
	}
	if result.CookieDomain != "" {
		t.Fatalf("ProxyAuth cookie domain = %q, want empty for mismatched domain", result.CookieDomain)
	}
}

func TestServiceProxyAuthRequiresLoginWhenUnauthenticated(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.provider.Type = domainprovider.ProviderTypeProxy
	repo.provider.Config = map[string]any{"externalHost": "grafana.example.com"}
	repo.app.ProviderType = domainportal.ProviderTypeProxy
	repo.app.Status = domainportal.ApplicationStatusEnabled
	service := New(repo, &memoryUsers{}, nil, nil, "test-encryption-key-32-bytes-long")

	result, err := service.ProxyAuth(ctx, domainidentity.Principal{}, domainprovider.ProxyAuthInput{
		ForwardedHost:  "grafana.example.com",
		ForwardedProto: "https",
		ForwardedURI:   "/login",
	})
	if err != nil {
		t.Fatalf("ProxyAuth returned error: %v", err)
	}
	if result.Decision != domainprovider.ProxyDecisionLogin || result.LoginURL == "" {
		t.Fatalf("ProxyAuth result = %#v, want login with URL", result)
	}
}

func TestServiceProxyAuthUsesProxySessionToken(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.provider.Type = domainprovider.ProviderTypeProxy
	repo.provider.Config = map[string]any{"externalHost": "grafana.example.com"}
	repo.app.ProviderType = domainportal.ProviderTypeProxy
	repo.app.Status = domainportal.ApplicationStatusEnabled
	users := &memoryUsers{}
	service := New(repo, users, nil, nil, "test-encryption-key-32-bytes-long")

	session, err := service.IssueProxySession(ctx, users.principal())
	if err != nil {
		t.Fatalf("IssueProxySession returned error: %v", err)
	}
	if session.Token == "" || session.ExpiresAt.IsZero() {
		t.Fatalf("IssueProxySession = %#v", session)
	}

	result, err := service.ProxyAuth(ctx, domainidentity.Principal{}, domainprovider.ProxyAuthInput{
		ForwardedHost:  "grafana.example.com",
		ForwardedProto: "https",
		ForwardedURI:   "/dashboards/db/main",
		SessionToken:   session.Token,
	})
	if err != nil {
		t.Fatalf("ProxyAuth returned error: %v", err)
	}
	if result.Decision != domainprovider.ProxyDecisionAllow {
		t.Fatalf("ProxyAuth decision = %q, want allow", result.Decision)
	}
	if result.Headers["X-Soha-User-Id"] != "user-1" {
		t.Fatalf("proxy session user id header = %q", result.Headers["X-Soha-User-Id"])
	}
}

func TestServiceProxyAuthIgnoresInvalidProxySessionToken(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.provider.Type = domainprovider.ProviderTypeProxy
	repo.provider.Config = map[string]any{"externalHost": "grafana.example.com"}
	repo.app.ProviderType = domainportal.ProviderTypeProxy
	repo.app.Status = domainportal.ApplicationStatusEnabled
	service := New(repo, &memoryUsers{}, nil, nil, "test-encryption-key-32-bytes-long")

	result, err := service.ProxyAuth(ctx, domainidentity.Principal{}, domainprovider.ProxyAuthInput{
		ForwardedHost: "grafana.example.com",
		ForwardedURI:  "/dashboards",
		SessionToken:  "invalid-token",
	})
	if err != nil {
		t.Fatalf("ProxyAuth returned error: %v", err)
	}
	if result.Decision != domainprovider.ProxyDecisionLogin {
		t.Fatalf("ProxyAuth decision = %q, want login", result.Decision)
	}
}

func TestServiceProxyAuthDeniesUnauthorizedPrincipal(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.provider.Type = domainprovider.ProviderTypeProxy
	repo.provider.Config = map[string]any{"externalHost": "grafana.example.com"}
	repo.app.ProviderType = domainportal.ProviderTypeProxy
	repo.app.Status = domainportal.ApplicationStatusEnabled
	repo.app.Assignments = []domainportal.ApplicationAssignment{{
		SubjectType: domainportal.AssignmentSubjectRole,
		SubjectID:   "admin",
		Effect:      domainportal.AssignmentEffectAllow,
	}}
	service := New(repo, &memoryUsers{}, nil, nil, "test-encryption-key-32-bytes-long")

	result, err := service.ProxyAuth(ctx, domainidentity.Principal{
		UserID:   "user-2",
		UserName: "Grace",
		Roles:    []string{"viewer"},
	}, domainprovider.ProxyAuthInput{
		ForwardedHost: "grafana.example.com",
		ForwardedURI:  "/dashboards",
	})
	if err != nil {
		t.Fatalf("ProxyAuth returned error: %v", err)
	}
	if result.Decision != domainprovider.ProxyDecisionDeny {
		t.Fatalf("ProxyAuth decision = %q, want deny", result.Decision)
	}
}

func TestServiceProxyAuthRejectsProviderIDHostMismatch(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.provider.Type = domainprovider.ProviderTypeProxy
	repo.provider.Config = map[string]any{"externalHost": "grafana.example.com"}
	repo.app.ProviderType = domainportal.ProviderTypeProxy
	repo.app.Status = domainportal.ApplicationStatusEnabled
	service := New(repo, &memoryUsers{}, nil, nil, "test-encryption-key-32-bytes-long")

	_, err := service.ProxyAuth(ctx, (&memoryUsers{}).principal(), domainprovider.ProxyAuthInput{
		ProviderID:  "provider-1",
		OriginalURL: "https://evil.example/dashboards",
	})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("ProxyAuth error = %v, want access denied", err)
	}
}

func TestServiceProxyAuthAllowsSkipAuthPathWithoutPrincipal(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.provider.Type = domainprovider.ProviderTypeProxy
	repo.provider.Config = map[string]any{
		"externalHost":  "grafana.example.com",
		"skipAuthPaths": []any{"/healthz", "/public"},
	}
	repo.app.ProviderType = domainportal.ProviderTypeProxy
	repo.app.Status = domainportal.ApplicationStatusEnabled
	service := New(repo, &memoryUsers{}, nil, nil, "test-encryption-key-32-bytes-long")

	result, err := service.ProxyAuth(ctx, domainidentity.Principal{}, domainprovider.ProxyAuthInput{
		ForwardedHost: "grafana.example.com",
		ForwardedURI:  "/public/assets/logo.svg",
	})
	if err != nil {
		t.Fatalf("ProxyAuth returned error: %v", err)
	}
	if result.Decision != domainprovider.ProxyDecisionAllow || !result.Skipped {
		t.Fatalf("ProxyAuth result = %#v, want skipped allow", result)
	}
	if len(result.Headers) != 0 {
		t.Fatalf("skip auth headers = %#v, want none", result.Headers)
	}
}

func TestServiceReverseProxyAuthorizesConfiguredUpstream(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.provider.Type = domainprovider.ProviderTypeProxy
	repo.provider.Config = map[string]any{
		"externalHost":      "grafana.example.com",
		"mode":              domainprovider.ProxyModeReverseProxy,
		"upstreamUrl":       "http://grafana.internal:3000/base",
		"websocket_enabled": true,
	}
	repo.app.ProviderType = domainportal.ProviderTypeProxy
	repo.app.Status = domainportal.ApplicationStatusEnabled
	service := New(repo, &memoryUsers{}, nil, nil, "test-encryption-key-32-bytes-long")

	result, err := service.ReverseProxy(ctx, (&memoryUsers{}).principal(), domainprovider.ReverseProxyInput{
		ProviderID:  repo.provider.ID,
		Path:        "/dashboards/main",
		OriginalURL: "https://soha.example/api/v1/provider/proxy/reverse/provider-1/dashboards/main",
		Method:      http.MethodGet,
	})
	if err != nil {
		t.Fatalf("ReverseProxy returned error: %v", err)
	}
	if result.Auth.Decision != domainprovider.ProxyDecisionAllow {
		t.Fatalf("ReverseProxy decision = %q, want allow", result.Auth.Decision)
	}
	if result.UpstreamURL != "http://grafana.internal:3000/base" {
		t.Fatalf("ReverseProxy upstream = %q", result.UpstreamURL)
	}
	if !result.WebsocketEnabled {
		t.Fatal("ReverseProxy websocket flag = false, want true")
	}
}

func TestServiceReverseProxyRejectsForwardAuthAndUnsafeUpstream(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.provider.Type = domainprovider.ProviderTypeProxy
	repo.provider.Config = map[string]any{
		"externalHost": "grafana.example.com",
		"mode":         domainprovider.ProxyModeForwardAuth,
		"upstreamUrl":  "http://grafana.internal:3000",
	}
	service := New(repo, &memoryUsers{}, nil, nil, "test-encryption-key-32-bytes-long")

	_, err := service.ReverseProxy(ctx, (&memoryUsers{}).principal(), domainprovider.ReverseProxyInput{ProviderID: repo.provider.ID})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("ReverseProxy forward-auth error = %v, want invalid argument", err)
	}

	repo.provider.Config["mode"] = domainprovider.ProxyModeReverseProxy
	repo.provider.Config["upstreamUrl"] = "file:///etc/passwd"
	_, err = service.ReverseProxy(ctx, (&memoryUsers{}).principal(), domainprovider.ReverseProxyInput{ProviderID: repo.provider.ID})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("ReverseProxy unsafe upstream error = %v, want invalid argument", err)
	}
}

func TestProviderFromInputValidatesReverseProxyConfiguration(t *testing.T) {
	input := domainprovider.ProviderInput{
		ApplicationID: "app-1",
		Name:          "Grafana",
		Type:          domainprovider.ProviderTypeProxy,
		Enabled:       true,
		Config: map[string]any{
			"mode":        domainprovider.ProxyModeReverseProxy,
			"upstreamUrl": "file:///etc/passwd",
		},
	}
	_, err := providerFromInput("provider-1", input, domainidentity.Principal{}, time.Now())
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("providerFromInput error = %v, want invalid reverse proxy upstream", err)
	}

	input.Config["upstreamUrl"] = "https://grafana.internal:3000"
	provider, err := providerFromInput("provider-1", input, domainidentity.Principal{}, time.Now())
	if err != nil {
		t.Fatalf("providerFromInput valid reverse proxy: %v", err)
	}
	if provider.Config["mode"] != domainprovider.ProxyModeReverseProxy {
		t.Fatalf("provider mode = %#v", provider.Config["mode"])
	}
}

func TestServiceOutpostClaimAndHeartbeat(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	token := "outpost-token-1"
	repo.outposts["outpost-1"] = domainprovider.Outpost{
		ID:        "outpost-1",
		Name:      "Edge",
		Mode:      domainprovider.OutpostModeExternal,
		TokenHash: hashToken(token),
		Status:    domainprovider.OutpostStatusOffline,
	}
	repo.provider.Type = domainprovider.ProviderTypeProxy
	repo.provider.Config = map[string]any{
		"externalHost": "grafana.example.com",
		"outpostId":    "outpost-1",
	}
	service := New(repo, &memoryUsers{}, nil, nil, "test-encryption-key-32-bytes-long")

	claim, err := service.ClaimOutpost(ctx, domainprovider.OutpostClaimInput{
		OutpostID: "outpost-1",
		Token:     token,
		Version:   "0.1.0",
	})
	if err != nil {
		t.Fatalf("ClaimOutpost returned error: %v", err)
	}
	if claim.Outpost.Status != domainprovider.OutpostStatusOnline || claim.Outpost.LastSeenAt == nil {
		t.Fatalf("ClaimOutpost outpost = %#v, want online with lastSeenAt", claim.Outpost)
	}
	if len(claim.Providers) != 1 || claim.Providers[0].ID != "provider-1" {
		t.Fatalf("ClaimOutpost providers = %#v", claim.Providers)
	}

	heartbeat, err := service.HeartbeatOutpost(ctx, "outpost-1", domainprovider.OutpostHeartbeatInput{
		Token:   token,
		Status:  domainprovider.OutpostStatusDegraded,
		Version: "0.1.1",
	})
	if err != nil {
		t.Fatalf("HeartbeatOutpost returned error: %v", err)
	}
	if heartbeat.Outpost.Status != domainprovider.OutpostStatusDegraded || heartbeat.Outpost.Version != "0.1.1" {
		t.Fatalf("HeartbeatOutpost = %#v, want degraded 0.1.1", heartbeat.Outpost)
	}

	session, err := service.IssueProxySession(ctx, (&memoryUsers{}).principal())
	if err != nil {
		t.Fatalf("IssueProxySession returned error: %v", err)
	}
	check, err := service.CheckOutpost(ctx, "outpost-1", domainprovider.OutpostCheckInput{
		Token:        token,
		ProviderID:   "provider-1",
		OriginalURL:  "https://grafana.example.com/dashboards",
		SessionToken: session.Token,
	})
	if err != nil {
		t.Fatalf("CheckOutpost returned error: %v", err)
	}
	if check.Decision != domainprovider.ProxyDecisionAllow || check.Headers["X-Soha-User-Id"] != "user-1" {
		t.Fatalf("CheckOutpost = %#v, want allow for proxy session", check)
	}

	events, err := service.RecordOutpostEvents(ctx, "outpost-1", domainprovider.OutpostEventsInput{
		Token: token,
		Events: []domainprovider.OutpostEvent{{
			EventType:     "proxy_allow",
			ProviderID:    "provider-1",
			ApplicationID: "app-1",
			Result:        "success",
			OriginalURL:   "https://grafana.example.com/dashboards",
		}},
	})
	if err != nil {
		t.Fatalf("RecordOutpostEvents returned error: %v", err)
	}
	if events.Accepted != 1 {
		t.Fatalf("RecordOutpostEvents accepted = %d, want 1", events.Accepted)
	}
}

func TestServiceOutpostClaimRejectsInvalidToken(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.outposts["outpost-1"] = domainprovider.Outpost{
		ID:        "outpost-1",
		Name:      "Edge",
		Mode:      domainprovider.OutpostModeExternal,
		TokenHash: hashToken("correct-token"),
		Status:    domainprovider.OutpostStatusOffline,
	}
	service := New(repo, &memoryUsers{}, nil, nil, "test-encryption-key-32-bytes-long")

	_, err := service.ClaimOutpost(ctx, domainprovider.OutpostClaimInput{
		OutpostID: "outpost-1",
		Token:     "wrong-token",
	})
	if !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("ClaimOutpost error = %v, want unauthorized", err)
	}
}

func TestServiceOutpostCheckRejectsUnassignedProvider(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	token := "outpost-token-1"
	repo.outposts["outpost-1"] = domainprovider.Outpost{
		ID:        "outpost-1",
		Name:      "Edge",
		Mode:      domainprovider.OutpostModeExternal,
		TokenHash: hashToken(token),
		Status:    domainprovider.OutpostStatusOnline,
	}
	repo.provider.Type = domainprovider.ProviderTypeProxy
	repo.provider.Config = map[string]any{
		"externalHost": "grafana.example.com",
		"outpostId":    "other-outpost",
	}
	service := New(repo, &memoryUsers{}, nil, nil, "test-encryption-key-32-bytes-long")

	_, err := service.CheckOutpost(ctx, "outpost-1", domainprovider.OutpostCheckInput{
		Token:       token,
		ProviderID:  "provider-1",
		OriginalURL: "https://grafana.example.com/dashboards",
	})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("CheckOutpost error = %v, want access denied", err)
	}

	_, err = service.RecordOutpostEvents(ctx, "outpost-1", domainprovider.OutpostEventsInput{
		Token: token,
		Events: []domainprovider.OutpostEvent{{
			EventType:  "proxy_allow",
			ProviderID: "provider-1",
			Result:     "success",
		}},
	})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("RecordOutpostEvents error = %v, want access denied", err)
	}
}

func TestServiceCreateOIDCClientRequiresOIDCProvider(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.provider.Type = domainprovider.ProviderTypeProxy
	service := New(repo, &memoryUsers{}, identityProviderTestPermissions(), nil, "test-encryption-key-32-bytes-long")

	_, err := service.CreateOIDCClient(ctx, (&memoryUsers{}).principal(), "provider-1", domainprovider.OIDCClientInput{
		ClientID:          "client-2",
		RedirectURIs:      []string{"https://app.example/callback"},
		AllowedScopes:     []string{"openid"},
		AllowedGrantTypes: []string{"authorization_code"},
		Status:            domainprovider.OIDCClientStatusEnabled,
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("CreateOIDCClient error = %v, want invalid argument", err)
	}
}

func TestServiceCreateOIDCClientGeneratesSecretForOIDCProvider(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	service := New(repo, &memoryUsers{}, identityProviderTestPermissions(), nil, "test-encryption-key-32-bytes-long")

	created, err := service.CreateOIDCClient(ctx, (&memoryUsers{}).principal(), "provider-1", domainprovider.OIDCClientInput{
		ClientID:          "client-2",
		RedirectURIs:      []string{"https://app.example/callback"},
		AllowedScopes:     []string{"openid", "email"},
		AllowedGrantTypes: []string{"authorization_code"},
		Status:            domainprovider.OIDCClientStatusEnabled,
	})
	if err != nil {
		t.Fatalf("CreateOIDCClient returned error: %v", err)
	}
	if created.Client.ClientID != "client-2" || created.Client.ProviderID != "provider-1" {
		t.Fatalf("created client = %#v", created.Client)
	}
	if created.ClientSecret == "" || created.Client.ClientSecretHash == "" {
		t.Fatalf("created client secret/hash missing: %#v", created)
	}
}

func TestServiceUpdateOIDCClientRequiresOIDCProvider(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.provider.Type = domainprovider.ProviderTypeProxy
	service := New(repo, &memoryUsers{}, identityProviderTestPermissions(), nil, "test-encryption-key-32-bytes-long")

	_, err := service.UpdateOIDCClient(ctx, (&memoryUsers{}).principal(), "oidc-client-1", domainprovider.OIDCClientInput{
		ProviderID:        "provider-1",
		ClientID:          "client-1",
		RedirectURIs:      []string{"https://app.example/callback"},
		AllowedScopes:     []string{"openid"},
		AllowedGrantTypes: []string{"authorization_code"},
		RequirePKCE:       true,
		Status:            domainprovider.OIDCClientStatusEnabled,
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("UpdateOIDCClient error = %v, want invalid argument", err)
	}
}

func TestServiceListOIDCClientsRequiresOIDCProvider(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.provider.Type = domainprovider.ProviderTypeProxy
	service := New(repo, &memoryUsers{}, identityProviderTestPermissions(), nil, "test-encryption-key-32-bytes-long")

	_, err := service.ListOIDCClients(ctx, (&memoryUsers{}).principal(), "provider-1")
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("ListOIDCClients error = %v, want invalid argument", err)
	}
}

func TestServiceCreateProviderRejectsDuplicateApplicationProvider(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	service := New(repo, &memoryUsers{}, identityProviderTestPermissions(), nil, "test-encryption-key-32-bytes-long")

	_, err := service.CreateProvider(ctx, (&memoryUsers{}).principal(), domainprovider.ProviderInput{
		ApplicationID: "app-1",
		Name:          "Duplicate Provider",
		Type:          domainprovider.ProviderTypeOIDC,
		Enabled:       true,
		Status:        domainprovider.ProviderStatusEnabled,
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("CreateProvider error = %v, want invalid argument", err)
	}
}

func TestServiceUpdateProviderRejectsApplicationWithExistingProvider(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.extraProviders = map[string]domainprovider.Provider{
		"provider-2": {
			ID:            "provider-2",
			ApplicationID: "app-2",
			Name:          "Other Provider",
			Type:          domainprovider.ProviderTypeProxy,
			Enabled:       true,
			Status:        domainprovider.ProviderStatusEnabled,
		},
	}
	service := New(repo, &memoryUsers{}, identityProviderTestPermissions(), nil, "test-encryption-key-32-bytes-long")

	_, err := service.UpdateProvider(ctx, (&memoryUsers{}).principal(), "provider-1", domainprovider.ProviderInput{
		ApplicationID: "app-2",
		Name:          "Moved Provider",
		Type:          domainprovider.ProviderTypeOIDC,
		Enabled:       true,
		Status:        domainprovider.ProviderStatusEnabled,
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("UpdateProvider error = %v, want invalid argument", err)
	}
}

func TestServiceCreateOIDCClientRejectsUnsupportedGrantType(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	service := New(repo, &memoryUsers{}, identityProviderTestPermissions(), nil, "test-encryption-key-32-bytes-long")

	_, err := service.CreateOIDCClient(ctx, (&memoryUsers{}).principal(), "provider-1", domainprovider.OIDCClientInput{
		ClientID:          "client-2",
		RedirectURIs:      []string{"https://app.example/callback"},
		AllowedScopes:     []string{"openid"},
		AllowedGrantTypes: []string{"authorization_code", "refresh_token"},
		Status:            domainprovider.OIDCClientStatusEnabled,
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("CreateOIDCClient error = %v, want invalid argument", err)
	}
}

func TestServiceCreateOIDCClientNormalizesRefreshTokenTTL(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	service := New(repo, &memoryUsers{}, identityProviderTestPermissions(), nil, "test-encryption-key-32-bytes-long")

	created, err := service.CreateOIDCClient(ctx, (&memoryUsers{}).principal(), "provider-1", domainprovider.OIDCClientInput{
		ClientID:               "client-2",
		RedirectURIs:           []string{"https://app.example/callback"},
		AllowedScopes:          []string{"openid"},
		AllowedGrantTypes:      []string{"authorization_code"},
		RefreshTokenTTLSeconds: 86400,
		Status:                 domainprovider.OIDCClientStatusEnabled,
	})
	if err != nil {
		t.Fatalf("CreateOIDCClient returned error: %v", err)
	}
	if created.Client.RefreshTokenTTLSeconds != 0 {
		t.Fatalf("refresh token ttl = %d, want 0", created.Client.RefreshTokenTTLSeconds)
	}
	if got, want := created.Client.AllowedGrantTypes, []string{"authorization_code"}; fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("allowed grant types = %#v, want %#v", got, want)
	}
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type memoryRepo struct {
	provider       domainprovider.Provider
	extraProviders map[string]domainprovider.Provider
	client         domainprovider.OIDCClient
	app            domainportal.Application
	key            *domainprovider.SigningKey
	codes          map[string]domainprovider.AuthorizationCode
	outposts       map[string]domainprovider.Outpost
}

func newMemoryRepo(t *testing.T) *memoryRepo {
	t.Helper()
	secretHash, err := bcrypt.GenerateFromPassword([]byte("secret-1"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatalf("hash secret: %v", err)
	}
	return &memoryRepo{
		provider: domainprovider.Provider{
			ID:            "provider-1",
			ApplicationID: "app-1",
			Name:          "Provider",
			Type:          domainprovider.ProviderTypeOIDC,
			Enabled:       true,
			Status:        domainprovider.ProviderStatusEnabled,
		},
		client: domainprovider.OIDCClient{
			ID:                    "oidc-client-1",
			ProviderID:            "provider-1",
			ClientID:              "client-1",
			ClientSecretHash:      string(secretHash),
			RedirectURIs:          []string{"https://app.example/callback"},
			AllowedScopes:         []string{"openid", "profile", "email", "roles"},
			AllowedGrantTypes:     []string{"authorization_code"},
			RequirePKCE:           true,
			AccessTokenTTLSeconds: defaultOIDCAccessTokenTTLSeconds,
			IDTokenTTLSeconds:     defaultOIDCIDTokenTTLSeconds,
			Status:                domainprovider.OIDCClientStatusEnabled,
		},
		app: domainportal.Application{
			ID:           "app-1",
			ProviderID:   "provider-1",
			ProviderType: domainportal.ProviderTypeOIDC,
			Status:       domainportal.ApplicationStatusEnabled,
		},
		codes:    map[string]domainprovider.AuthorizationCode{},
		outposts: map[string]domainprovider.Outpost{},
	}
}

func (r *memoryRepo) ListProviders(_ context.Context, filter domainprovider.ProviderFilter) ([]domainprovider.Provider, error) {
	items := make([]domainprovider.Provider, 0, len(r.extraProviders)+1)
	for _, provider := range r.allProviders() {
		if filter.ApplicationID != "" && provider.ApplicationID != filter.ApplicationID {
			continue
		}
		if filter.Type != "" && provider.Type != filter.Type {
			continue
		}
		if filter.Status != "" && provider.Status != filter.Status {
			continue
		}
		items = append(items, provider)
	}
	return items, nil
}

func (r *memoryRepo) GetProvider(_ context.Context, providerID string) (domainprovider.Provider, error) {
	for _, provider := range r.allProviders() {
		if providerID == provider.ID {
			return provider, nil
		}
	}
	return domainprovider.Provider{}, apperrors.ErrNotFound
}

func (r *memoryRepo) CreateProvider(_ context.Context, item domainprovider.Provider) (domainprovider.Provider, error) {
	if item.ID == r.provider.ID {
		r.provider = item
		return item, nil
	}
	if r.extraProviders == nil {
		r.extraProviders = map[string]domainprovider.Provider{}
	}
	r.extraProviders[item.ID] = item
	return item, nil
}

func (r *memoryRepo) UpdateProvider(_ context.Context, item domainprovider.Provider) (domainprovider.Provider, error) {
	if item.ID == r.provider.ID {
		r.provider = item
		return item, nil
	}
	if _, ok := r.extraProviders[item.ID]; ok {
		r.extraProviders[item.ID] = item
		return item, nil
	}
	return domainprovider.Provider{}, apperrors.ErrNotFound
}

func (r *memoryRepo) DeleteProvider(context.Context, string) error {
	return nil
}

func (r *memoryRepo) allProviders() []domainprovider.Provider {
	items := make([]domainprovider.Provider, 0, len(r.extraProviders)+1)
	items = append(items, r.provider)
	for _, provider := range r.extraProviders {
		items = append(items, provider)
	}
	return items
}

func (r *memoryRepo) ListOutposts(context.Context, domainprovider.OutpostFilter) ([]domainprovider.Outpost, error) {
	items := make([]domainprovider.Outpost, 0, len(r.outposts))
	for _, item := range r.outposts {
		items = append(items, item)
	}
	return items, nil
}

func (r *memoryRepo) GetOutpost(_ context.Context, outpostID string) (domainprovider.Outpost, error) {
	item, ok := r.outposts[outpostID]
	if !ok {
		return domainprovider.Outpost{}, apperrors.ErrNotFound
	}
	return item, nil
}

func (r *memoryRepo) CreateOutpost(_ context.Context, item domainprovider.Outpost) (domainprovider.Outpost, error) {
	r.outposts[item.ID] = item
	return item, nil
}

func (r *memoryRepo) UpdateOutpost(_ context.Context, item domainprovider.Outpost) (domainprovider.Outpost, error) {
	if _, ok := r.outposts[item.ID]; !ok {
		return domainprovider.Outpost{}, apperrors.ErrNotFound
	}
	r.outposts[item.ID] = item
	return item, nil
}

func (r *memoryRepo) DeleteOutpost(_ context.Context, outpostID string) error {
	if _, ok := r.outposts[outpostID]; !ok {
		return apperrors.ErrNotFound
	}
	delete(r.outposts, outpostID)
	return nil
}

func (r *memoryRepo) GetProviderApplication(_ context.Context, providerID string) (domainportal.Application, error) {
	if providerID != r.provider.ID {
		return domainportal.Application{}, apperrors.ErrNotFound
	}
	return r.app, nil
}

func (r *memoryRepo) ListOIDCClients(_ context.Context, providerID string) ([]domainprovider.OIDCClient, error) {
	if providerID != r.client.ProviderID {
		return nil, nil
	}
	return []domainprovider.OIDCClient{r.client}, nil
}

func (r *memoryRepo) GetOIDCClient(_ context.Context, id string) (domainprovider.OIDCClient, error) {
	if id != r.client.ID {
		return domainprovider.OIDCClient{}, apperrors.ErrNotFound
	}
	return r.client, nil
}

func (r *memoryRepo) GetOIDCClientByClientID(_ context.Context, clientID string) (domainprovider.OIDCClient, error) {
	if clientID != r.client.ClientID {
		return domainprovider.OIDCClient{}, apperrors.ErrNotFound
	}
	return r.client, nil
}

func (r *memoryRepo) CreateOIDCClient(_ context.Context, item domainprovider.OIDCClient) (domainprovider.OIDCClient, error) {
	r.client = item
	return item, nil
}

func (r *memoryRepo) UpdateOIDCClient(_ context.Context, item domainprovider.OIDCClient) (domainprovider.OIDCClient, error) {
	if item.ID != r.client.ID {
		return domainprovider.OIDCClient{}, apperrors.ErrNotFound
	}
	r.client = item
	return item, nil
}

func (r *memoryRepo) DeleteOIDCClient(context.Context, string) error {
	return nil
}

func (r *memoryRepo) GetActiveSigningKey(context.Context, string) (domainprovider.SigningKey, error) {
	if r.key == nil {
		return domainprovider.SigningKey{}, apperrors.ErrNotFound
	}
	return *r.key, nil
}

func (r *memoryRepo) CreateSigningKey(_ context.Context, key domainprovider.SigningKey) (domainprovider.SigningKey, error) {
	r.key = &key
	return key, nil
}

func (r *memoryRepo) ListActivePublicKeys(context.Context) ([]domainprovider.SigningKey, error) {
	if r.key == nil {
		return nil, nil
	}
	return []domainprovider.SigningKey{*r.key}, nil
}

func (r *memoryRepo) CreateAuthorizationCode(_ context.Context, code domainprovider.AuthorizationCode) error {
	r.codes[code.CodeHash] = code
	return nil
}

func (r *memoryRepo) GetAuthorizationCode(_ context.Context, codeHash string, now time.Time) (domainprovider.AuthorizationCode, error) {
	code, ok := r.codes[codeHash]
	if !ok || code.ConsumedAt != nil || !code.ExpiresAt.After(now) {
		return domainprovider.AuthorizationCode{}, apperrors.ErrUnauthorized
	}
	return code, nil
}

func (r *memoryRepo) ConsumeAuthorizationCode(_ context.Context, codeHash string, now time.Time) (domainprovider.AuthorizationCode, error) {
	code, ok := r.codes[codeHash]
	if !ok || code.ConsumedAt != nil || !code.ExpiresAt.After(now) {
		return domainprovider.AuthorizationCode{}, apperrors.ErrUnauthorized
	}
	consumedAt := now
	code.ConsumedAt = &consumedAt
	r.codes[codeHash] = code
	return code, nil
}

type memoryUsers struct{}

func (m *memoryUsers) principal() domainidentity.Principal {
	return domainidentity.Principal{
		UserID:   "user-1",
		UserName: "Ada",
		Email:    "ada@example.com",
		Roles:    []string{"admin"},
	}
}

func (m *memoryUsers) GetByID(context.Context, string) (userrepo.User, error) {
	return userrepo.User{ID: "user-1", Username: "ada", DisplayName: "Ada", Email: "ada@example.com", Status: "active"}, nil
}

func (m *memoryUsers) GetAuthzState(context.Context, string) (userrepo.AuthzState, error) {
	return userrepo.AuthzState{UserID: "user-1", Status: "active", AuthzVersion: 1}, nil
}

func (m *memoryUsers) ListRoles(context.Context, string) ([]string, error) {
	return []string{"admin"}, nil
}

func (m *memoryUsers) ListTeams(context.Context, string) ([]string, error) {
	return []string{}, nil
}

func (m *memoryUsers) ListProjects(context.Context, string) ([]string, error) {
	return []string{}, nil
}

type identityProviderRolePermissions struct {
	matrix map[string][]string
}

func (r identityProviderRolePermissions) ListRolePermissions(context.Context) (map[string][]string, error) {
	return r.matrix, nil
}

func identityProviderTestPermissions() *appaccess.PermissionResolver {
	return appaccess.NewPermissionResolver(identityProviderRolePermissions{
		matrix: map[string][]string{
			"admin": {
				appaccess.PermIdentityProvidersView,
				appaccess.PermIdentityProvidersManage,
			},
		},
	})
}
