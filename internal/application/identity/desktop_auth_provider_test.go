package identity

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestDesktopOAuthCallbackCompletesThroughExchange(t *testing.T) {
	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/token":
			_ = json.NewEncoder(writer).Encode(map[string]any{"access_token": "provider-access", "token_type": "Bearer"})
		case "/userinfo":
			if request.Header.Get("Authorization") != "Bearer provider-access" {
				http.Error(writer, "missing bearer token", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"sub": "oauth-user-1", "name": "Ada", "email": "ada@example.com"})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer providerServer.Close()

	provider := desktopOAuthProvider()
	provider.TokenURL = providerServer.URL + "/token"
	provider.UserInfoURL = providerServer.URL + "/userinfo"
	provider.UserIDField = "sub"
	provider.UserNameField = "name"
	provider.EmailField = "email"
	repo := newLoginMappingUserRepo()
	service := newDesktopProviderTestService(repo, provider, nil)
	attempt, verifier, authorizationURL := beginDesktopProviderTestAttempt(t, service, provider.ID)
	state := queryValue(t, authorizationURL, "state")

	redirectURL, err := service.HandleProviderCallback(context.Background(), provider.ID, state, "provider-code")
	if err != nil {
		t.Fatalf("HandleProviderCallback() error = %v", err)
	}
	if _, err := service.HandleProviderCallback(context.Background(), provider.ID, state, "provider-code"); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("replayed OAuth state error = %v, want unauthorized", err)
	}
	result := exchangeDesktopProviderCallback(t, service, attempt.AttemptID, redirectURL, verifier)
	if result.User.Email != "ada@example.com" || len(repo.sessionsByID) != 1 {
		t.Fatalf("desktop OAuth result = %#v, sessions = %d", result, len(repo.sessionsByID))
	}
	if _, ok := repo.identities["oauth2|"+provider.ID+"|oauth-user-1"]; !ok {
		t.Fatal("desktop OAuth callback did not persist the external identity")
	}
}

func TestDesktopOIDCCallbackCompletesThroughExchange(t *testing.T) {
	signingKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	const keyID = "desktop-oidc-test-key"
	var issuer, callbackNonce string
	providerServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/.well-known/openid-configuration":
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"issuer": issuer, "authorization_endpoint": issuer + "/authorize", "token_endpoint": issuer + "/token",
				"jwks_uri": issuer + "/keys", "userinfo_endpoint": issuer + "/userinfo",
				"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
				"id_token_signing_alg_values_supported": []string{"RS256"},
			})
		case "/keys":
			_ = json.NewEncoder(writer).Encode(map[string]any{"keys": []map[string]any{{
				"kty": "RSA", "use": "sig", "alg": "RS256", "kid": keyID,
				"n": base64.RawURLEncoding.EncodeToString(signingKey.PublicKey.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(signingKey.PublicKey.E)).Bytes()),
			}}})
		case "/token":
			now := time.Now().UTC()
			idToken := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.MapClaims{
				"iss": issuer, "sub": "oidc-user-1", "aud": "desktop-client", "nonce": callbackNonce,
				"email": "oidc@example.com", "name": "OIDC User", "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
			})
			idToken.Header["kid"] = keyID
			signed, signErr := idToken.SignedString(signingKey)
			if signErr != nil {
				http.Error(writer, signErr.Error(), http.StatusInternalServerError)
				return
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{
				"access_token": "provider-access", "token_type": "Bearer", "expires_in": 300, "id_token": signed,
			})
		default:
			http.NotFound(writer, request)
		}
	}))
	defer providerServer.Close()
	issuer = providerServer.URL

	provider := domainsettings.LoginProviderSettings{
		ID: "oidc-main", Name: "OIDC Main", Type: "oidc", Enabled: true,
		ClientID: "desktop-client", ClientSecret: "desktop-secret", Issuer: issuer,
		RedirectURL: issuer + "/callback", FrontendRedirectURL: "https://soha.example/auth/callback",
		Scopes: []string{"openid", "profile", "email"},
	}
	repo := newLoginMappingUserRepo()
	service := newDesktopProviderTestService(repo, provider, nil)
	attempt, verifier, authorizationURL := beginDesktopProviderTestAttempt(t, service, provider.ID)
	state := queryValue(t, authorizationURL, "state")
	callbackNonce = queryValue(t, authorizationURL, "nonce")

	redirectURL, err := service.HandleProviderCallback(context.Background(), provider.ID, state, "provider-code")
	if err != nil {
		t.Fatalf("HandleProviderCallback() error = %v", err)
	}
	if _, err := service.HandleProviderCallback(context.Background(), provider.ID, state, "provider-code"); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("replayed OIDC state error = %v, want unauthorized", err)
	}
	result := exchangeDesktopProviderCallback(t, service, attempt.AttemptID, redirectURL, verifier)
	if result.User.Email != "oidc@example.com" || len(repo.sessionsByID) != 1 {
		t.Fatalf("desktop OIDC result = %#v, sessions = %d", result, len(repo.sessionsByID))
	}
	if _, ok := repo.identities["oidc|"+provider.ID+"|oidc-user-1"]; !ok {
		t.Fatal("desktop OIDC callback did not persist the external identity")
	}
}

func TestDesktopSAMLCallbackCompletesThroughExchange(t *testing.T) {
	provider := domainsettings.LoginProviderSettings{
		ID: "saml-main", Name: "SAML Main", Type: "saml", Enabled: true,
		EmailField: "email", UserNameField: "displayName",
	}
	runtime := samlRuntimeStub{
		request: SAMLAuthnRequest{ID: "request-1", RedirectURL: "https://idp.example/sso"},
		assertion: SAMLAssertion{
			ID: "assertion-1", Subject: "saml-user-1",
			Attributes: map[string][]string{"email": {"saml@example.com"}, "displayName": {"SAML User"}},
		},
	}
	repo := newLoginMappingUserRepo()
	service := newDesktopProviderTestService(repo, provider, runtime)
	attempt, verifier, _ := beginDesktopProviderTestAttempt(t, service, provider.ID)
	relayState := ephemeralTokenValue(t, repo, samlStateKind)

	redirectURL, err := service.HandleSAMLResponse(context.Background(), provider.ID, "encoded-response", relayState)
	if err != nil {
		t.Fatalf("HandleSAMLResponse() error = %v", err)
	}
	if _, err := service.HandleSAMLResponse(context.Background(), provider.ID, "encoded-response", relayState); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("replayed SAML state error = %v, want unauthorized", err)
	}
	result := exchangeDesktopProviderCallback(t, service, attempt.AttemptID, redirectURL, verifier)
	if result.User.Email != "saml@example.com" || len(repo.sessionsByID) != 1 {
		t.Fatalf("desktop SAML result = %#v, sessions = %d", result, len(repo.sessionsByID))
	}
	if _, ok := repo.identities["saml|"+provider.ID+"|saml-user-1"]; !ok {
		t.Fatal("desktop SAML callback did not persist the external identity")
	}
}

func newDesktopProviderTestService(repo *loginMappingUserRepo, provider domainsettings.LoginProviderSettings, samlRuntime SAMLLoginRuntime) *Service {
	dependencies := testDependenciesWithUserStore(repo)
	dependencies.Settings = loginProviderSettingsStub{providers: map[string]domainsettings.LoginProviderSettings{provider.ID: provider}}
	dependencies.SAML = samlRuntime
	service, err := New(dependencies)
	if err != nil {
		panic(err)
	}
	service.cfg = cfgpkg.AuthConfig{JWT: cfgpkg.JWTConfig{
		Secret: "desktop-provider-test-secret", Issuer: "soha-test", AccessTTL: time.Minute, RefreshTTL: time.Hour,
	}}
	return service
}

func beginDesktopProviderTestAttempt(t *testing.T, service *Service, providerID string) (domainidentity.DesktopAuthAttempt, string, string) {
	t.Helper()
	verifier := strings.Repeat("a", 43)
	attempt, err := service.CreateDesktopAuthAttempt(context.Background(), domainidentity.DesktopAuthAttemptCreate{
		ProviderID: providerID, RedirectURI: "http://127.0.0.1:49152/callback/abcdefghijklmnop",
		CodeChallenge: desktopCodeChallenge(verifier), CodeChallengeMethod: "S256",
	})
	if err != nil {
		t.Fatalf("CreateDesktopAuthAttempt() error = %v", err)
	}
	authorizationURL, err := service.BeginDesktopAuthAttempt(context.Background(), attempt.AttemptID)
	if err != nil {
		t.Fatalf("BeginDesktopAuthAttempt() error = %v", err)
	}
	return attempt, verifier, authorizationURL
}

func exchangeDesktopProviderCallback(t *testing.T, service *Service, attemptID, redirectURL, verifier string) domainidentity.AuthResult {
	t.Helper()
	result, err := service.ConsumeDesktopAuthAttempt(context.Background(), attemptID, callbackCodeFromRedirect(t, redirectURL), verifier)
	if err != nil || result.Tokens.AccessToken == "" || result.Tokens.RefreshToken == "" {
		t.Fatalf("ConsumeDesktopAuthAttempt() result = %#v, error = %v", result, err)
	}
	return result
}

func queryValue(t *testing.T, rawURL, key string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	value := parsed.Query().Get(key)
	if value == "" {
		t.Fatalf("URL %q has no %q query value", rawURL, key)
	}
	return value
}

func ephemeralTokenValue(t *testing.T, repo *loginMappingUserRepo, kind string) string {
	t.Helper()
	for key := range repo.ephemeral {
		if strings.HasPrefix(key, kind+"|") {
			return strings.TrimPrefix(key, kind+"|")
		}
	}
	t.Fatalf("no %s token was stored", kind)
	return ""
}
