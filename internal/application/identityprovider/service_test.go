package identityprovider

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	domainportal "github.com/opensoha/soha/internal/domain/providerportal"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/keyring"
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
	introspection, err = service.Introspect(ctx, "https://soha.example", token.AccessToken, domainprovider.ClientAuthInput{
		ClientID: "client-1", ClientSecret: "secret-1",
	})
	expectOIDC(t, err == nil && !introspection.Active, "revoked access token = %#v, error=%v", introspection, err)

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

func TestSAMLAttributeMappingsAcceptContractArray(t *testing.T) {
	mappings := samlAttributeMappings([]any{
		map[string]any{"source": "email", "target": "email", "required": true},
		map[string]any{"source": "roles", "target": "role"},
		"invalid",
	})
	if mappings["email"] != "email" || mappings["roles"] != "role" || len(mappings) != 2 {
		t.Fatalf("unexpected SAML attribute mappings: %#v", mappings)
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

func TestServiceOIDCPublicClientRequiresPKCEAndNoSecret(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.client.ClientType = domainprovider.OIDCClientTypePublic
	repo.client.ClientSecretHash = ""
	users := &memoryUsers{}
	service := New(repo, users, nil, nil, "test-encryption-key-32-bytes-long")

	if _, err := service.Authorize(ctx, "https://soha.example", users.principal(), domainprovider.AuthorizeInput{
		ResponseType: "code", ClientID: "client-1", RedirectURI: "https://app.example/callback", Scope: "openid",
	}); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("Authorize without PKCE error = %v, want invalid argument", err)
	}
	authorized, err := service.Authorize(ctx, "https://soha.example", users.principal(), domainprovider.AuthorizeInput{
		ResponseType: "code", ClientID: "client-1", RedirectURI: "https://app.example/callback", Scope: "openid",
		CodeChallenge: pkceChallenge("verifier"), CodeChallengeMethod: "S256",
	})
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	tokens, err := service.Token(ctx, "https://soha.example", domainprovider.TokenInput{
		GrantType: "authorization_code", Code: authorized.Code, RedirectURI: "https://app.example/callback",
		ClientID: "client-1", CodeVerifier: "verifier",
	})
	if err != nil || tokens.AccessToken == "" {
		t.Fatalf("public client Token = %#v, error=%v", tokens, err)
	}
}

func TestServiceOIDCConfidentialClientWithoutSecretHashFailsClosed(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.client.ClientSecretHash = ""
	users := &memoryUsers{}
	service := New(repo, users, nil, nil, "test-encryption-key-32-bytes-long")
	authorized, err := service.Authorize(ctx, "https://soha.example", users.principal(), domainprovider.AuthorizeInput{
		ResponseType: "code", ClientID: "client-1", RedirectURI: "https://app.example/callback", Scope: "openid",
		CodeChallenge: pkceChallenge("verifier"), CodeChallengeMethod: "S256",
	})
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	if _, err := service.Token(ctx, "https://soha.example", domainprovider.TokenInput{
		GrantType: "authorization_code", Code: authorized.Code, RedirectURI: "https://app.example/callback",
		ClientID: "client-1", CodeVerifier: "verifier",
	}); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("Token error = %v, want unauthorized", err)
	}
}

func TestServiceOIDCClaimsFollowGrantedScopes(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	users := &memoryUsers{}
	service := New(repo, users, nil, nil, "test-encryption-key-32-bytes-long")
	authorized, err := service.Authorize(ctx, "https://soha.example", users.principal(), domainprovider.AuthorizeInput{
		ResponseType: "code", ClientID: "client-1", RedirectURI: "https://app.example/callback", Scope: "openid",
		CodeChallenge: pkceChallenge("verifier"), CodeChallengeMethod: "S256",
	})
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	tokens, err := service.Token(ctx, "https://soha.example", domainprovider.TokenInput{
		GrantType: "authorization_code", Code: authorized.Code, RedirectURI: "https://app.example/callback",
		ClientID: "client-1", ClientSecret: "secret-1", CodeVerifier: "verifier",
	})
	if err != nil {
		t.Fatalf("Token returned error: %v", err)
	}
	claims, err := service.parseOIDCToken(ctx, "https://soha.example", tokens.IDToken, domainprovider.TokenTypeID, true)
	if err != nil {
		t.Fatalf("parse ID token: %v", err)
	}
	if claims.Email != "" || claims.UserName != "" || len(claims.Roles)+len(claims.Teams)+len(claims.Projects)+len(claims.Tags) != 0 {
		t.Fatalf("openid-only claims leaked profile data: %#v", claims)
	}
	userInfo, err := service.UserInfo(ctx, "https://soha.example", tokens.AccessToken)
	if err != nil {
		t.Fatalf("UserInfo returned error: %v", err)
	}
	if userInfo.Subject != "user-1" || userInfo.Email != "" || userInfo.Name != "" || len(userInfo.Roles) != 0 {
		t.Fatalf("openid-only UserInfo leaked profile data: %#v", userInfo)
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

func TestServiceOIDCRefreshTokenRotatesAndRevokesOnReplay(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.client.AllowedGrantTypes = []string{"authorization_code", "refresh_token"}
	repo.client.AllowedScopes = append(repo.client.AllowedScopes, "offline_access")
	repo.client.RefreshTokenTTLSeconds = 3600
	users := &memoryUsers{}
	service := New(repo, users, nil, nil, "test-encryption-key-32-bytes-long")

	authorized, err := service.Authorize(ctx, "https://soha.example", users.principal(), domainprovider.AuthorizeInput{
		ResponseType: "code", ClientID: "client-1", RedirectURI: "https://app.example/callback",
		Scope: "openid offline_access", CodeChallenge: pkceChallenge("verifier"), CodeChallengeMethod: "S256",
		PlatformSessionID: "platform-1",
	})
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	initial, err := service.Token(ctx, "https://soha.example", domainprovider.TokenInput{
		GrantType: "authorization_code", Code: authorized.Code, RedirectURI: "https://app.example/callback",
		ClientID: "client-1", ClientSecret: "secret-1", CodeVerifier: "verifier",
	})
	if err != nil || initial.RefreshToken == "" {
		t.Fatalf("initial Token = %#v, error=%v", initial, err)
	}
	rotated, err := service.Token(ctx, "https://soha.example", domainprovider.TokenInput{
		GrantType: "refresh_token", RefreshToken: initial.RefreshToken, ClientID: "client-1", ClientSecret: "secret-1",
	})
	if err != nil || rotated.RefreshToken == "" || rotated.RefreshToken == initial.RefreshToken {
		t.Fatalf("rotated Token = %#v, error=%v", rotated, err)
	}
	if _, err := service.Token(ctx, "https://soha.example", domainprovider.TokenInput{
		GrantType: "refresh_token", RefreshToken: initial.RefreshToken, ClientID: "client-1", ClientSecret: "secret-1",
	}); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("replayed refresh error = %v, want unauthorized", err)
	}
	introspection, err := service.Introspect(ctx, "https://soha.example", rotated.AccessToken, domainprovider.ClientAuthInput{ClientID: "client-1", ClientSecret: "secret-1"})
	if err != nil || introspection.Active {
		t.Fatalf("introspection after replay = %#v, error=%v, want inactive", introspection, err)
	}
}

func TestServiceOIDCEndSessionRevokesBoundSessions(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	users := &memoryUsers{}
	service := New(repo, users, nil, nil, "test-encryption-key-32-bytes-long")
	authorized, err := service.Authorize(ctx, "https://soha.example", users.principal(), domainprovider.AuthorizeInput{
		ResponseType: "code", ClientID: "client-1", RedirectURI: "https://app.example/callback", Scope: "openid",
		CodeChallenge: pkceChallenge("verifier"), CodeChallengeMethod: "S256", PlatformSessionID: "platform-1",
	})
	if err != nil {
		t.Fatalf("Authorize returned error: %v", err)
	}
	tokens, err := service.Token(ctx, "https://soha.example", domainprovider.TokenInput{
		GrantType: "authorization_code", Code: authorized.Code, RedirectURI: "https://app.example/callback",
		ClientID: "client-1", ClientSecret: "secret-1", CodeVerifier: "verifier",
	})
	if err != nil {
		t.Fatalf("Token returned error: %v", err)
	}
	if _, err := service.EndSession(ctx, "https://soha.example", domainprovider.EndSessionInput{
		IDTokenHint: tokens.IDToken, PostLogoutRedirectURI: "https://unregistered.example/logout",
	}); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("unregistered redirect error = %v, want invalid argument", err)
	}
	if _, err := service.EndSession(ctx, "https://soha.example", domainprovider.EndSessionInput{
		IDTokenHint: tokens.IDToken, PostLogoutRedirectURI: "https://app.example/callback",
	}); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("authorization redirect used for logout error = %v, want invalid argument", err)
	}
	result, err := service.EndSession(ctx, "https://soha.example", domainprovider.EndSessionInput{
		IDTokenHint: tokens.IDToken, PostLogoutRedirectURI: "https://app.example/logout", State: "state-1",
	})
	if err != nil || result.RedirectURI != "https://app.example/logout" || result.State != "state-1" {
		t.Fatalf("EndSession = %#v, error=%v", result, err)
	}
	if users.revokedSessionID != "platform-1" {
		t.Fatalf("revoked platform session = %q, want platform-1", users.revokedSessionID)
	}
	introspection, err := service.Introspect(ctx, "https://soha.example", tokens.AccessToken, domainprovider.ClientAuthInput{ClientID: "client-1", ClientSecret: "secret-1"})
	if err != nil || introspection.Active {
		t.Fatalf("introspection after logout = %#v, error=%v", introspection, err)
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

func TestServiceOIDCAuthorizeValidatesMaxAge(t *testing.T) {
	baseInput := domainprovider.AuthorizeInput{
		ResponseType: "code", ClientID: "client-1", RedirectURI: "https://app.example/callback",
		Scope: "openid", State: "state-1", CodeChallenge: pkceChallenge("verifier"),
		CodeChallengeMethod: "S256", PlatformSessionID: "session-1",
	}
	for _, test := range []struct {
		name, maxAge, wantCode string
		lastSeen               time.Time
		wantErr                error
	}{
		{name: "fresh session", maxAge: "300", lastSeen: time.Now().UTC().Add(-time.Minute)},
		{name: "stale session", maxAge: "30", lastSeen: time.Now().UTC().Add(-time.Minute), wantCode: "login_required", wantErr: apperrors.ErrUnauthorized},
		{name: "malformed value", maxAge: "soon", wantCode: "invalid_request", wantErr: apperrors.ErrInvalidArgument},
	} {
		t.Run(test.name, func(t *testing.T) {
			users := &memoryUsers{sessionLastSeen: test.lastSeen}
			service := New(newMemoryRepo(t), users, nil, nil, "test-encryption-key-32-bytes-long")
			input := baseInput
			input.MaxAge = test.maxAge
			_, err := service.Authorize(context.Background(), "https://soha.example", users.principal(), input)
			if test.wantErr == nil {
				if err != nil {
					t.Fatalf("Authorize returned error: %v", err)
				}
				return
			}
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Authorize error = %v, want %v", err, test.wantErr)
			}
			var redirectErr *domainprovider.AuthorizeRedirectError
			if !errors.As(err, &redirectErr) || redirectErr.Code != test.wantCode {
				t.Fatalf("Authorize redirect error = %#v, want code %q", redirectErr, test.wantCode)
			}
		})
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
			name: "prompt none without session",
			input: domainprovider.AuthorizeInput{
				ResponseType:        "code",
				ClientID:            "client-1",
				RedirectURI:         "https://app.example/callback",
				Scope:               "openid",
				State:               "state-1",
				Prompt:              "none",
				CodeChallenge:       pkceChallenge("verifier"),
				CodeChallengeMethod: "S256",
			},
			wantCode: "login_required",
			wantIs:   apperrors.ErrUnauthorized,
		},
		{
			name: "prompt login requires reauthentication",
			input: domainprovider.AuthorizeInput{
				ResponseType:        "code",
				ClientID:            "client-1",
				RedirectURI:         "https://app.example/callback",
				Scope:               "openid",
				State:               "state-1",
				Prompt:              "login",
				CodeChallenge:       pkceChallenge("verifier"),
				CodeChallengeMethod: "S256",
			},
			wantCode: "login_required",
			wantIs:   apperrors.ErrUnauthorized,
		},
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

			principal := (&memoryUsers{}).principal()
			if tt.name == "prompt none without session" {
				principal = domainidentity.Principal{}
			}
			_, err := service.Authorize(ctx, "https://soha.example", principal, tt.input)
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
			if redirectErr.Description != "" {
				t.Fatalf("AuthorizeRedirectError description = %q, want HTTP boundary to derive it", redirectErr.Description)
			}
			if len(repo.codes) != 0 {
				t.Fatalf("authorization codes = %d, want 0", len(repo.codes))
			}
		})
	}
}

func TestServiceRotateSigningKeyRetainsPreviousJWKSKey(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	service := New(repo, &memoryUsers{}, identityProviderTestPermissions(), nil, "test-encryption-key-32-bytes-long")
	if _, _, err := service.ensureSigningKey(ctx, repo.provider.ID); err != nil {
		t.Fatalf("ensureSigningKey returned error: %v", err)
	}
	firstKid := repo.key.KeyID
	rotated, err := service.RotateSigningKey(ctx, (&memoryUsers{}).principal(), repo.provider.ID)
	if err != nil {
		t.Fatalf("RotateSigningKey returned error: %v", err)
	}
	if rotated.KeyID == "" || rotated.KeyID == firstKid {
		t.Fatalf("rotated key id = %q, previous = %q", rotated.KeyID, firstKid)
	}
	jwks, err := service.JWKS(ctx)
	if err != nil {
		t.Fatalf("JWKS returned error: %v", err)
	}
	if len(jwks.Keys) != 2 {
		t.Fatalf("JWKS keys = %#v, want retained previous and active keys", jwks.Keys)
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

	session, err := service.IssueProxySession(ctx, users.principal(), domainidentity.AccessContext{SessionID: "session-1"})
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

func TestServiceProxySessionRequiresPlatformSession(t *testing.T) {
	service := New(newMemoryRepo(t), &memoryUsers{}, nil, nil, "test-encryption-key-32-bytes-long")
	_, err := service.IssueProxySession(context.Background(), (&memoryUsers{}).principal(), domainidentity.AccessContext{})
	if !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("IssueProxySession error = %v, want unauthorized", err)
	}
}

func TestServiceProxySessionUsesKeyringAndAcceptsPreviousKey(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	users := &memoryUsers{}
	now := time.Now().UTC()
	oldKey, err := keyring.NewKey("old", "old-secret", now.Add(-time.Hour), nil)
	if err != nil {
		t.Fatalf("old key: %v", err)
	}
	newKey, err := keyring.NewKey("new", "new-secret", now, nil)
	if err != nil {
		t.Fatalf("new key: %v", err)
	}
	oldRing, err := keyring.New(oldKey, nil)
	if err != nil {
		t.Fatalf("old ring: %v", err)
	}
	newRing, err := keyring.New(newKey, []keyring.Key{oldKey})
	if err != nil {
		t.Fatalf("new ring: %v", err)
	}
	oldService := NewWithEncryptionKeys(repo, users, nil, nil, oldRing)
	newService := NewWithEncryptionKeys(repo, users, nil, nil, newRing)
	session, err := oldService.IssueProxySession(ctx, users.principal(), domainidentity.AccessContext{SessionID: "session-1"})
	if err != nil {
		t.Fatalf("IssueProxySession returned error: %v", err)
	}
	principal, err := newService.parseProxySession(ctx, session.Token)
	if err != nil || principal.UserID != "user-1" {
		t.Fatalf("parse previous-key session = %#v, error=%v", principal, err)
	}
	otherKey, _ := keyring.NewKey("other", "other-secret", now, nil)
	otherRing, _ := keyring.New(otherKey, nil)
	if _, err := NewWithEncryptionKeys(repo, users, nil, nil, otherRing).parseProxySession(ctx, session.Token); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("cross-key session error = %v, want unauthorized", err)
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
		ProviderID:    "provider-1",
		OriginalURL:   "https://evil.example/dashboards",
		ForwardedHost: "grafana.example.com",
		ForwardedURI:  "/dashboards",
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

func TestServiceProxyAuthRejectsSkipPathWhenProviderDisabled(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.provider.Type = domainprovider.ProviderTypeProxy
	repo.provider.Enabled = false
	repo.provider.Status = domainprovider.ProviderStatusDisabled
	repo.provider.Config = map[string]any{"externalHost": "grafana.example.com", "skipAuthPaths": []any{"/healthz"}}
	repo.app.ProviderType = domainportal.ProviderTypeProxy
	repo.app.Status = domainportal.ApplicationStatusEnabled
	service := New(repo, &memoryUsers{}, nil, nil, "test-encryption-key-32-bytes-long")
	_, err := service.ProxyAuth(ctx, domainidentity.Principal{}, domainprovider.ProxyAuthInput{
		ProviderID: "provider-1", OriginalURL: "https://grafana.example.com/healthz",
	})
	if !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("ProxyAuth error = %v, want unauthorized", err)
	}
}

func TestServiceReverseProxyAuthorizesConfiguredUpstream(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.provider.Type = domainprovider.ProviderTypeProxy
	repo.provider.Config = map[string]any{
		"externalHost":         "grafana.example.com",
		"mode":                 domainprovider.ProxyModeReverseProxy,
		"upstreamUrl":          "http://grafana.internal:3000/base",
		"allowPrivateUpstream": true,
		"websocket_enabled":    true,
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
	if !result.AllowPrivateUpstream {
		t.Fatal("ReverseProxy private-upstream flag = false, want true")
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

func TestServiceProviderResponsesRedactSecretReferences(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.provider.SecretRefs = map[string]any{
		"CLIENT_SECRET": "soha://secrets/oidc-client",
	}
	service := New(repo, &memoryUsers{}, identityProviderTestPermissions(), nil, "test-encryption-key-32-bytes-long")

	items, err := service.ListProviders(ctx, (&memoryUsers{}).principal(), domainprovider.ProviderFilter{})
	if err != nil {
		t.Fatalf("ListProviders returned error: %v", err)
	}
	if len(items) != 1 || items[0].SecretRefs != nil {
		t.Fatalf("providers leaked secret references: %#v", items)
	}
	if len(items[0].ConfiguredSecretAliases) != 1 || items[0].ConfiguredSecretAliases[0] != "CLIENT_SECRET" {
		t.Fatalf("configured aliases = %#v", items[0].ConfiguredSecretAliases)
	}
	item, err := service.GetProvider(ctx, (&memoryUsers{}).principal(), repo.provider.ID)
	if err != nil || item.SecretRefs != nil {
		t.Fatalf("GetProvider result = %#v, error = %v", item, err)
	}

	repo.provider.Type = domainprovider.ProviderTypeProxy
	repo.provider.Config = map[string]any{"externalHost": "grafana.example.com"}
	repo.app.ProviderType = domainportal.ProviderTypeProxy
	repo.app.Status = domainportal.ApplicationStatusEnabled
	result, err := service.ProxyAuth(ctx, (&memoryUsers{}).principal(), domainprovider.ProxyAuthInput{
		OriginalURL: "https://grafana.example.com/",
	})
	if err != nil || result.Provider.SecretRefs != nil {
		t.Fatalf("ProxyAuth provider = %#v, error = %v", result.Provider, err)
	}
}

func TestProviderFromInputValidatesSecretReferences(t *testing.T) {
	input := domainprovider.ProviderInput{
		ApplicationID: "app-1",
		Name:          "OIDC",
		Type:          domainprovider.ProviderTypeOIDC,
		Status:        domainprovider.ProviderStatusEnabled,
		SecretRefs:    map[string]any{"clientSecret": "plain-text-secret"},
	}

	_, err := providerFromInput("provider-1", input, domainidentity.Principal{}, time.Now())
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("providerFromInput error = %v, want invalid secret reference", err)
	}
}

func TestProviderFromInputValidatesSAMLServiceProvider(t *testing.T) {
	input := domainprovider.ProviderInput{
		ApplicationID: "app-1",
		Name:          "Example SAML",
		Type:          domainprovider.ProviderTypeSAML,
		Enabled:       true,
		Config: map[string]any{
			"entityId":                     "https://sp.example/saml/metadata",
			"assertionConsumerServiceUrls": []any{"http://sp.example/saml/acs"},
		},
	}
	provider, err := providerFromInput("provider-1", input, domainidentity.Principal{}, time.Now())
	if err != nil {
		t.Fatalf("providerFromInput HTTP SAML ACS: %v", err)
	}

	input.Config["assertionConsumerServiceUrls"] = []any{"ftp://sp.example/saml/acs"}
	if _, err := providerFromInput("provider-1", input, domainidentity.Principal{}, time.Now()); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("providerFromInput unsupported SAML ACS error = %v, want invalid argument", err)
	}
	if provider.Type != domainprovider.ProviderTypeSAML {
		t.Fatalf("provider type = %q", provider.Type)
	}
}

func TestNormalizeRedirectURIsSupportsHTTP(t *testing.T) {
	values, err := normalizeRedirectURIs([]string{"http://app.example/callback", "https://app.example/logout"})
	if err != nil || len(values) != 2 {
		t.Fatalf("normalizeRedirectURIs() = %#v, %v", values, err)
	}
	for _, value := range []string{"ftp://app.example/callback", "http://app.example/callback#fragment", "/callback"} {
		if _, err := normalizeRedirectURIs([]string{value}); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatalf("normalizeRedirectURIs(%q) error = %v, want invalid argument", value, err)
		}
	}
}

func TestRedirectURIAllowedSupportsStrictAndRegexRules(t *testing.T) {
	client := domainprovider.OIDCClient{
		RedirectURIs:       []string{"http://app.example/callback"},
		RedirectURIRegexes: []string{`https?://[a-z0-9-]+\.example\.com/callback`},
	}
	tests := map[string]bool{
		"http://app.example/callback":                                  true,
		"https://tenant.example.com/callback":                          true,
		"http://tenant.example.com/callback":                           true,
		"https://tenant.example.com/callback/extra":                    false,
		"https://evil.example.net/https://tenant.example.com/callback": false,
		"ftp://tenant.example.com/callback":                            false,
		"https://tenant.example.com/callback#token":                    false,
	}
	for candidate, want := range tests {
		if got := redirectURIAllowed(client, candidate); got != want {
			t.Errorf("redirectURIAllowed(%q) = %v, want %v", candidate, got, want)
		}
	}
}

func TestOIDCClientFromInputAllowsRegexOnlyRedirect(t *testing.T) {
	client, err := oidcClientFromInput("client-record-1", domainprovider.OIDCClientInput{
		ProviderID:         "provider-1",
		ClientID:           "client-1",
		RedirectURIRegexes: []string{`https?://[a-z0-9-]+\.example\.com/callback`},
	}, "")
	if err != nil {
		t.Fatalf("oidcClientFromInput() error = %v", err)
	}
	if len(client.RedirectURIs) != 0 || len(client.RedirectURIRegexes) != 1 {
		t.Fatalf("oidcClientFromInput() redirects = %#v, regexes = %#v", client.RedirectURIs, client.RedirectURIRegexes)
	}

	for _, pattern := range []string{"(", strings.Repeat("a", maxOIDCRedirectURIRegexLength+1)} {
		_, err := oidcClientFromInput("client-record-1", domainprovider.OIDCClientInput{
			ProviderID:         "provider-1",
			ClientID:           "client-1",
			RedirectURIRegexes: []string{pattern},
		}, "")
		if !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Errorf("oidcClientFromInput(%q) error = %v, want invalid argument", pattern, err)
		}
	}
}

func TestServiceOIDCRegexRedirectIsRecheckedAtTokenExchange(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.client.RedirectURIs = nil
	repo.client.RedirectURIRegexes = []string{`https?://[a-z0-9-]+\.example\.com/callback`}
	service := New(repo, &memoryUsers{}, nil, nil, "test-encryption-key-32-bytes-long")
	verifier := strings.Repeat("v", 43)
	redirectURI := "http://tenant.example.com/callback"

	authorized, err := service.Authorize(ctx, "https://soha.example", (&memoryUsers{}).principal(), domainprovider.AuthorizeInput{
		ResponseType:        "code",
		ClientID:            "client-1",
		RedirectURI:         redirectURI,
		Scope:               "openid",
		CodeChallenge:       pkceChallenge(verifier),
		CodeChallengeMethod: "S256",
	})
	if err != nil {
		t.Fatalf("Authorize() error = %v", err)
	}

	repo.client.RedirectURIRegexes = nil
	_, err = service.Token(ctx, "https://soha.example", domainprovider.TokenInput{
		GrantType:    "authorization_code",
		Code:         authorized.Code,
		RedirectURI:  redirectURI,
		ClientID:     "client-1",
		ClientSecret: "secret-1",
		CodeVerifier: verifier,
	})
	if !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("Token() error = %v, want unauthorized after redirect rule removal", err)
	}
}

func TestSAMLSSORejectsReplayedAuthnRequest(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.provider.Type = domainprovider.ProviderTypeSAML
	repo.app.ProviderType = domainportal.ProviderTypeSAML
	repo.samlSP = domainprovider.SAMLServiceProvider{
		ProviderID: repo.provider.ID, EntityID: "https://sp.example.test/metadata",
		AssertionConsumerServiceURLs: []string{"https://sp.example.test/acs"},
	}
	now := time.Now().UTC()
	encryptionKey, err := keyring.NewKey("active", "test-encryption-key-32-bytes-long", now, nil)
	if err != nil {
		t.Fatal(err)
	}
	encryptionKeys, err := keyring.New(encryptionKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	service := NewWithEncryptionKeys(repo, &memoryUsers{}, nil, nil, encryptionKeys)
	key, err := service.generateSAMLSigningKey(repo.provider.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	repo.samlKey = key
	service.saml = fakeSAMLProviderRuntime{}
	principal := (&memoryUsers{}).principal()
	input := SAMLRequestInput{Method: http.MethodPost, Encoded: "request"}

	result, err := service.SAMLSSO(ctx, "https://soha.example", repo.provider.ID, "session-1", principal, input)
	if err != nil || result.ACSURL != "https://sp.example.test/acs" || len(result.HTML) == 0 {
		t.Fatalf("first SAML SSO = %#v, error=%v", result, err)
	}
	if _, err := service.SAMLSSO(ctx, "https://soha.example", repo.provider.ID, "session-1", principal, input); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("replayed SAML SSO error = %v, want unauthorized", err)
	}
}

func TestSAMLSSOLoginPreservesPOSTRequest(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	repo.provider.Type = domainprovider.ProviderTypeSAML
	repo.samlSP = domainprovider.SAMLServiceProvider{
		ProviderID: repo.provider.ID, EntityID: "https://sp.example.test/metadata",
		AssertionConsumerServiceURLs: []string{"https://sp.example.test/acs"},
	}
	now := time.Now().UTC()
	encryptionKey, err := keyring.NewKey("active", "test-encryption-key-32-bytes-long", now, nil)
	if err != nil {
		t.Fatal(err)
	}
	encryptionKeys, err := keyring.New(encryptionKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	service := NewWithEncryptionKeys(repo, &memoryUsers{}, nil, nil, encryptionKeys)
	repo.samlKey, err = service.generateSAMLSigningKey(repo.provider.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	service.saml = fakeSAMLProviderRuntime{}

	token, err := service.PrepareSAMLSSOLogin(ctx, "https://soha.example", repo.provider.ID, SAMLRequestInput{
		Method: http.MethodPost, Encoded: "request", RelayState: "relay",
	})
	if err != nil {
		t.Fatal(err)
	}
	input, err := service.ResumeSAMLSSO(ctx, repo.provider.ID, token)
	if err != nil || input.Method != http.MethodPost || input.Encoded != "request" || input.RelayState != "relay" {
		t.Fatalf("resumed SAML input = %#v, error=%v", input, err)
	}
	if _, err := service.ResumeSAMLSSO(ctx, repo.provider.ID, token); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("reused SAML login token error = %v, want unauthorized", err)
	}
}

func TestApplicationPolicyAccessReturnsMFARequired(t *testing.T) {
	application := domainportal.Application{
		Status:   domainportal.ApplicationStatusEnabled,
		Metadata: map[string]any{"accessPolicy": map[string]any{"requireMfa": true}},
	}
	principal := (&memoryUsers{}).principal()
	err := applicationPolicyAccessError(principal, application, domainportal.AccessPolicyContext{Now: time.Now().UTC()})
	if !errors.Is(err, apperrors.ErrMFARequired) {
		t.Fatalf("policy error = %v, want MFA required", err)
	}
	if err := applicationPolicyAccessError(principal, application, domainportal.AccessPolicyContext{MFAAuthenticated: true, Now: time.Now().UTC()}); err != nil {
		t.Fatalf("policy rejected stepped-up session: %v", err)
	}
}

type fakeSAMLProviderRuntime struct{}

func (fakeSAMLProviderRuntime) Metadata(SAMLSigningMaterial) ([]byte, error) {
	return []byte("<metadata/>"), nil
}

func (fakeSAMLProviderRuntime) ValidateRequest(_ SAMLSigningMaterial, input SAMLRequestInput) (SAMLValidatedRequest, error) {
	return SAMLValidatedRequest{ID: "request-1", Issuer: input.ServiceProvider.EntityID, ACSURL: input.ServiceProvider.AssertionConsumerServiceURLs[0], RelayState: input.RelayState}, nil
}

func (fakeSAMLProviderRuntime) SignResponse(SAMLSigningMaterial, SAMLResponseInput) ([]byte, error) {
	return []byte("<Response/>"), nil
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
	if claim.ConfigVersion == "" {
		t.Fatal("ClaimOutpost configVersion is empty")
	}

	heartbeat, err := service.HeartbeatOutpost(ctx, "outpost-1", domainprovider.OutpostHeartbeatInput{
		Token:         token,
		Status:        domainprovider.OutpostStatusDegraded,
		Version:       "0.1.1",
		ConfigVersion: claim.ConfigVersion,
	})
	if err != nil {
		t.Fatalf("HeartbeatOutpost returned error: %v", err)
	}
	if heartbeat.Outpost.Status != domainprovider.OutpostStatusDegraded || heartbeat.Outpost.Version != "0.1.1" {
		t.Fatalf("HeartbeatOutpost = %#v, want degraded 0.1.1", heartbeat.Outpost)
	}
	if heartbeat.ConfigVersion != claim.ConfigVersion || len(heartbeat.Providers) != 0 {
		t.Fatalf("unchanged heartbeat = %#v", heartbeat)
	}
	repo.provider.Config["websocketEnabled"] = true
	changed, err := service.HeartbeatOutpost(ctx, "outpost-1", domainprovider.OutpostHeartbeatInput{
		Token: token, ConfigVersion: claim.ConfigVersion,
	})
	if err != nil || changed.ConfigVersion == claim.ConfigVersion || len(changed.Providers) != 1 {
		t.Fatalf("changed heartbeat = %#v, error=%v", changed, err)
	}

	session, err := service.IssueProxySession(ctx, (&memoryUsers{}).principal(), domainidentity.AccessContext{SessionID: "session-1"})
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

func TestServiceRotateOutpostTokenInvalidatesPreviousToken(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	oldToken := "outpost-token-1"
	repo.outposts["outpost-1"] = domainprovider.Outpost{
		ID: "outpost-1", Name: "Edge", Mode: domainprovider.OutpostModeExternal,
		TokenHash: hashToken(oldToken), Status: domainprovider.OutpostStatusOffline,
	}
	service := New(repo, &memoryUsers{}, identityProviderTestPermissions(), nil, "test-encryption-key-32-bytes-long")
	rotated, err := service.RotateOutpostToken(ctx, (&memoryUsers{}).principal(), "outpost-1")
	if err != nil || rotated.Token == "" || rotated.Token == oldToken {
		t.Fatalf("RotateOutpostToken = %#v, error=%v", rotated, err)
	}
	if _, err := service.ClaimOutpost(ctx, domainprovider.OutpostClaimInput{OutpostID: "outpost-1", Token: oldToken}); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("old token claim error = %v, want unauthorized", err)
	}
	if _, err := service.ClaimOutpost(ctx, domainprovider.OutpostClaimInput{OutpostID: "outpost-1", Token: rotated.Token}); err != nil {
		t.Fatalf("new token claim error = %v", err)
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

func TestServiceListOIDCClientsWithoutProviderListsAll(t *testing.T) {
	repo := newMemoryRepo(t)
	service := New(repo, &memoryUsers{}, identityProviderTestPermissions(), nil, "test-encryption-key-32-bytes-long")

	items, err := service.ListOIDCClients(context.Background(), (&memoryUsers{}).principal(), "")
	if err != nil {
		t.Fatalf("ListOIDCClients returned error: %v", err)
	}
	if len(items) != 1 || items[0].ID != repo.client.ID {
		t.Fatalf("clients = %#v, want all clients", items)
	}
}

func TestServiceCreateOIDCClientUsesInputProviderWithoutPathProvider(t *testing.T) {
	repo := newMemoryRepo(t)
	service := New(repo, &memoryUsers{}, identityProviderTestPermissions(), nil, "test-encryption-key-32-bytes-long")

	created, err := service.CreateOIDCClient(context.Background(), (&memoryUsers{}).principal(), "", domainprovider.OIDCClientInput{
		ProviderID:        "provider-1",
		ClientID:          "legacy-client",
		ClientType:        domainprovider.OIDCClientTypePublic,
		RedirectURIs:      []string{"https://app.example/callback"},
		AllowedScopes:     []string{"openid"},
		AllowedGrantTypes: []string{"authorization_code"},
		RequirePKCE:       true,
		Status:            domainprovider.OIDCClientStatusEnabled,
	})
	if err != nil {
		t.Fatalf("CreateOIDCClient returned error: %v", err)
	}
	if created.Client.ProviderID != "provider-1" || created.Client.ClientID != "legacy-client" {
		t.Fatalf("created client = %#v", created.Client)
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
		AllowedGrantTypes: []string{"authorization_code", "client_credentials"},
		Status:            domainprovider.OIDCClientStatusEnabled,
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("CreateOIDCClient error = %v, want invalid argument", err)
	}
}

func TestServiceCreateOIDCClientRejectsShortExplicitSecret(t *testing.T) {
	ctx := context.Background()
	repo := newMemoryRepo(t)
	service := New(repo, &memoryUsers{}, identityProviderTestPermissions(), nil, "test-encryption-key-32-bytes-long")

	_, err := service.CreateOIDCClient(ctx, (&memoryUsers{}).principal(), "provider-1", domainprovider.OIDCClientInput{
		ClientID:      "client-2",
		ClientSecret:  "too-short",
		RedirectURIs:  []string{"https://app.example/callback"},
		AllowedScopes: []string{"openid"},
		Status:        domainprovider.OIDCClientStatusEnabled,
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
	if created.Client.RefreshTokenTTLSeconds != 86400 {
		t.Fatalf("refresh token ttl = %d, want 86400", created.Client.RefreshTokenTTLSeconds)
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
	provider        domainprovider.Provider
	extraProviders  map[string]domainprovider.Provider
	client          domainprovider.OIDCClient
	app             domainportal.Application
	key             *domainprovider.SigningKey
	keys            []domainprovider.SigningKey
	codes           map[string]domainprovider.AuthorizationCode
	outposts        map[string]domainprovider.Outpost
	sessions        map[string]domainprovider.OIDCSession
	refreshTokens   map[string]domainprovider.OIDCRefreshToken
	samlSP          domainprovider.SAMLServiceProvider
	samlKey         domainprovider.SAMLSigningKey
	samlReplay      map[string]struct{}
	samlPending     map[string]domainprovider.SAMLPendingRequest
	outpostDigests  map[string]string
	outpostVersions map[string]int64
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
			ID:                     "oidc-client-1",
			ProviderID:             "provider-1",
			ClientID:               "client-1",
			ClientType:             domainprovider.OIDCClientTypeConfidential,
			ClientSecretHash:       string(secretHash),
			RedirectURIs:           []string{"https://app.example/callback"},
			PostLogoutRedirectURIs: []string{"https://app.example/logout"},
			AllowedScopes:          []string{"openid", "profile", "email", "roles"},
			AllowedGrantTypes:      []string{"authorization_code"},
			RequirePKCE:            true,
			AccessTokenTTLSeconds:  defaultOIDCAccessTokenTTLSeconds,
			IDTokenTTLSeconds:      defaultOIDCIDTokenTTLSeconds,
			Status:                 domainprovider.OIDCClientStatusEnabled,
		},
		app: domainportal.Application{
			ID:           "app-1",
			ProviderID:   "provider-1",
			ProviderType: domainportal.ProviderTypeOIDC,
			Status:       domainportal.ApplicationStatusEnabled,
		},
		codes:           map[string]domainprovider.AuthorizationCode{},
		outposts:        map[string]domainprovider.Outpost{},
		sessions:        map[string]domainprovider.OIDCSession{},
		refreshTokens:   map[string]domainprovider.OIDCRefreshToken{},
		samlReplay:      map[string]struct{}{},
		samlPending:     map[string]domainprovider.SAMLPendingRequest{},
		outpostDigests:  map[string]string{},
		outpostVersions: map[string]int64{},
	}
}

func TestRotateSAMLCertificateReplacesActiveKeyAndRetainsOverlap(t *testing.T) {
	repo := newMemoryRepo(t)
	repo.provider.Type = domainprovider.ProviderTypeSAML
	now := time.Now().UTC()
	activeKey, err := keyring.NewKey("test", "test-encryption-key-32-bytes-long", now.Add(-time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := keyring.New(activeKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	service := NewWithEncryptionKeys(repo, &memoryUsers{}, identityProviderTestPermissions(), nil, keys)
	current, err := service.generateSAMLSigningKey(repo.provider.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	repo.samlKey = current

	rotation, err := service.RotateSAMLCertificate(t.Context(), (&memoryUsers{}).principal(), current.ID, sohaapi.SAMLCertificateRotateRequest{OverlapSeconds: 600})
	if err != nil {
		t.Fatal(err)
	}
	if rotation.Active.ID == current.ID || rotation.Retiring.ID != current.ID || repo.samlKey.ID != rotation.Active.ID || !repo.samlKey.Active {
		t.Fatalf("unexpected rotation: result=%#v stored=%#v", rotation, repo.samlKey)
	}
	if rotation.OverlapEndsAt.Before(time.Now().UTC().Add(9*time.Minute)) || rotation.Retiring.Status != sohaapi.CertificateSummaryStatusRetiring {
		t.Fatalf("overlap was not retained: %#v", rotation)
	}
}

func TestRotateSAMLProviderCertificateSelectsActiveKey(t *testing.T) {
	repo := newMemoryRepo(t)
	repo.provider.Type = domainprovider.ProviderTypeSAML
	now := time.Now().UTC()
	activeKey, err := keyring.NewKey("test", "test-encryption-key-32-bytes-long", now.Add(-time.Hour), nil)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := keyring.New(activeKey, nil)
	if err != nil {
		t.Fatal(err)
	}
	service := NewWithEncryptionKeys(repo, &memoryUsers{}, identityProviderTestPermissions(), nil, keys)
	current, err := service.generateSAMLSigningKey(repo.provider.ID, now)
	if err != nil {
		t.Fatal(err)
	}
	repo.samlKey = current

	rotation, err := service.RotateSAMLProviderCertificate(t.Context(), (&memoryUsers{}).principal(), repo.provider.ID, sohaapi.SAMLCertificateRotateRequest{OverlapSeconds: 60})
	if err != nil {
		t.Fatal(err)
	}
	if rotation.Retiring.ID != current.ID || rotation.Active.ID == current.ID {
		t.Fatalf("unexpected provider certificate rotation: %#v", rotation)
	}

	repo.provider.Type = domainprovider.ProviderTypeOIDC
	if _, err := service.RotateSAMLProviderCertificate(t.Context(), (&memoryUsers{}).principal(), repo.provider.ID, sohaapi.SAMLCertificateRotateRequest{}); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("non-SAML provider rotation error = %v, want invalid argument", err)
	}
}

func (r *memoryRepo) CreateSAMLProvider(_ context.Context, provider domainprovider.Provider, serviceProvider domainprovider.SAMLServiceProvider, key domainprovider.SAMLSigningKey) (domainprovider.Provider, error) {
	r.provider, r.samlSP, r.samlKey = provider, serviceProvider, key
	return provider, nil
}

func (r *memoryRepo) UpsertSAMLServiceProvider(_ context.Context, item domainprovider.SAMLServiceProvider) error {
	r.samlSP = item
	return nil
}

func (r *memoryRepo) UpdateSAMLProvider(_ context.Context, provider domainprovider.Provider, serviceProvider domainprovider.SAMLServiceProvider) (domainprovider.Provider, error) {
	r.provider, r.samlSP = provider, serviceProvider
	return provider, nil
}

func (r *memoryRepo) GetSAMLServiceProvider(_ context.Context, providerID string) (domainprovider.SAMLServiceProvider, error) {
	if providerID != r.samlSP.ProviderID {
		return domainprovider.SAMLServiceProvider{}, apperrors.ErrNotFound
	}
	return r.samlSP, nil
}

func (r *memoryRepo) GetActiveSAMLSigningKey(_ context.Context, providerID string) (domainprovider.SAMLSigningKey, error) {
	if providerID != r.samlKey.ProviderID {
		return domainprovider.SAMLSigningKey{}, apperrors.ErrNotFound
	}
	return r.samlKey, nil
}

func (r *memoryRepo) GetSAMLSigningKey(_ context.Context, certificateID string) (domainprovider.SAMLSigningKey, error) {
	if certificateID != r.samlKey.ID {
		return domainprovider.SAMLSigningKey{}, apperrors.ErrNotFound
	}
	return r.samlKey, nil
}

func (r *memoryRepo) ListSAMLMetadataSigningKeys(_ context.Context, providerID string, now time.Time) ([]domainprovider.SAMLSigningKey, error) {
	if providerID != r.samlKey.ProviderID || !r.samlKey.NotAfter.After(now) {
		return nil, nil
	}
	return []domainprovider.SAMLSigningKey{r.samlKey}, nil
}

func (r *memoryRepo) RotateSAMLSigningKey(_ context.Context, certificateID string, next domainprovider.SAMLSigningKey, retireAfter time.Time) (domainprovider.SAMLSigningKey, domainprovider.SAMLSigningKey, error) {
	if certificateID != r.samlKey.ID || !r.samlKey.Active {
		return domainprovider.SAMLSigningKey{}, domainprovider.SAMLSigningKey{}, apperrors.ErrConflict
	}
	retiring := r.samlKey
	retiring.Active, retiring.RetireAfter = false, &retireAfter
	r.samlKey = next
	return retiring, next, nil
}

func (r *memoryRepo) ResolveOutpostRuntimeVersion(_ context.Context, outpostID, digest string) (int64, error) {
	if r.outpostVersions[outpostID] == 0 {
		r.outpostVersions[outpostID] = 1
		r.outpostDigests[outpostID] = digest
		return 1, nil
	}
	if r.outpostDigests[outpostID] != digest {
		r.outpostVersions[outpostID]++
		r.outpostDigests[outpostID] = digest
	}
	return r.outpostVersions[outpostID], nil
}

func (r *memoryRepo) ConsumeSAMLReplayKey(_ context.Context, providerID, kind, replayKey string, _ time.Time) error {
	key := providerID + ":" + kind + ":" + replayKey
	if _, exists := r.samlReplay[key]; exists {
		return apperrors.ErrConflict
	}
	r.samlReplay[key] = struct{}{}
	return nil
}

func (r *memoryRepo) CreateSAMLPendingRequest(_ context.Context, item domainprovider.SAMLPendingRequest) error {
	r.samlPending[item.Token] = item
	return nil
}

func (r *memoryRepo) ConsumeSAMLPendingRequest(_ context.Context, token, providerID string, now time.Time) (domainprovider.SAMLPendingRequest, error) {
	item, ok := r.samlPending[token]
	if !ok || item.ProviderID != providerID || !item.ExpiresAt.After(now) {
		return domainprovider.SAMLPendingRequest{}, apperrors.ErrUnauthorized
	}
	delete(r.samlPending, token)
	return item, nil
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
	if providerID != "" && providerID != r.client.ProviderID {
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
	r.keys = append(r.keys, key)
	return key, nil
}

func (r *memoryRepo) RotateSigningKey(_ context.Context, _ string, key domainprovider.SigningKey, now time.Time) (domainprovider.SigningKey, error) {
	for index := range r.keys {
		if r.keys[index].Active {
			r.keys[index].Active = false
			r.keys[index].RotatedAt = &now
		}
	}
	r.key = &key
	r.keys = append(r.keys, key)
	return key, nil
}

func (r *memoryRepo) ListActivePublicKeys(context.Context) ([]domainprovider.SigningKey, error) {
	return append([]domainprovider.SigningKey(nil), r.keys...), nil
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

func (r *memoryRepo) CreateOIDCSession(_ context.Context, session domainprovider.OIDCSession, refresh *domainprovider.OIDCRefreshToken) error {
	r.sessions[session.ID] = session
	if refresh != nil {
		r.refreshTokens[refresh.TokenHash] = *refresh
	}
	return nil
}

func (r *memoryRepo) GetOIDCSession(_ context.Context, sessionID string, now time.Time) (domainprovider.OIDCSession, error) {
	session, ok := r.sessions[sessionID]
	if !ok || session.RevokedAt != nil || !session.ExpiresAt.After(now) {
		return domainprovider.OIDCSession{}, apperrors.ErrUnauthorized
	}
	return session, nil
}

func (r *memoryRepo) RotateOIDCRefreshToken(_ context.Context, tokenHash string, next domainprovider.OIDCRefreshToken, now time.Time) (domainprovider.OIDCSession, error) {
	current, ok := r.refreshTokens[tokenHash]
	if !ok {
		return domainprovider.OIDCSession{}, apperrors.ErrUnauthorized
	}
	if current.ConsumedAt != nil {
		when := now
		session := r.sessions[current.SessionID]
		session.RevokedAt = &when
		r.sessions[current.SessionID] = session
		for hash, item := range r.refreshTokens {
			if item.FamilyID == current.FamilyID {
				item.RevokedAt = &when
				r.refreshTokens[hash] = item
			}
		}
		return domainprovider.OIDCSession{}, apperrors.ErrUnauthorized
	}
	session, err := r.GetOIDCSession(context.Background(), current.SessionID, now)
	if err != nil || current.RevokedAt != nil || !current.ExpiresAt.After(now) {
		return domainprovider.OIDCSession{}, apperrors.ErrUnauthorized
	}
	when := now
	current.ConsumedAt = &when
	r.refreshTokens[tokenHash] = current
	next.SessionID, next.FamilyID, next.ParentID, next.ExpiresAt = current.SessionID, current.FamilyID, current.ID, current.ExpiresAt
	r.refreshTokens[next.TokenHash] = next
	session.LastSeenAt = now
	r.sessions[session.ID] = session
	return session, nil
}

func (r *memoryRepo) RevokeOIDCSession(_ context.Context, sessionID, clientID string, now time.Time) error {
	session, ok := r.sessions[sessionID]
	if !ok || session.ClientID != clientID {
		return nil
	}
	session.RevokedAt = &now
	r.sessions[sessionID] = session
	for hash, item := range r.refreshTokens {
		if item.SessionID == sessionID {
			item.RevokedAt = &now
			r.refreshTokens[hash] = item
		}
	}
	return nil
}

func (r *memoryRepo) RevokeOIDCSessionByRefreshToken(_ context.Context, tokenHash, clientID string, now time.Time) error {
	item, ok := r.refreshTokens[tokenHash]
	if !ok {
		return nil
	}
	return r.RevokeOIDCSession(context.Background(), item.SessionID, clientID, now)
}

type memoryUsers struct {
	revokedSessionID string
	sessionLastSeen  time.Time
}

func (m *memoryUsers) GetAuthSessionByID(_ context.Context, sessionID string) (domainidentity.Session, error) {
	status := "active"
	if sessionID == m.revokedSessionID {
		status = "revoked"
	}
	lastSeen := m.sessionLastSeen
	if lastSeen.IsZero() {
		lastSeen = time.Now().UTC()
	}
	return domainidentity.Session{ID: sessionID, UserID: "user-1", Status: status, ExpiresAt: time.Now().UTC().Add(time.Hour), LastSeenAt: lastSeen}, nil
}

func (m *memoryUsers) RevokeSessionByID(_ context.Context, sessionID string) error {
	m.revokedSessionID = sessionID
	return nil
}

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
				appaccess.PermIdentityOutpostsView,
				appaccess.PermIdentityOutpostsManage,
			},
		},
	})
}
