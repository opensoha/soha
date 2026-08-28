package identity

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/platform/apperrors"
	userrepo "github.com/opensoha/soha/internal/repository/user"
)

func TestNormalizeDesktopRedirectURI(t *testing.T) {
	for _, test := range []struct {
		name string
		uri  string
		ok   bool
	}{
		{name: "exact loopback callback", uri: "http://127.0.0.1:49152/callback/abcdefghijklmnop", ok: true},
		{name: "localhost hostname", uri: "http://localhost:49152/callback/abcdefghijklmnop"},
		{name: "all interfaces", uri: "http://0.0.0.0:49152/callback/abcdefghijklmnop"},
		{name: "https", uri: "https://127.0.0.1:49152/callback/abcdefghijklmnop"},
		{name: "privileged port", uri: "http://127.0.0.1:80/callback/abcdefghijklmnop"},
		{name: "missing port", uri: "http://127.0.0.1/callback/abcdefghijklmnop"},
		{name: "short random path", uri: "http://127.0.0.1:49152/callback/short"},
		{name: "query", uri: "http://127.0.0.1:49152/callback/abcdefghijklmnop?code=1"},
		{name: "fragment", uri: "http://127.0.0.1:49152/callback/abcdefghijklmnop#code"},
		{name: "userinfo", uri: "http://user@127.0.0.1:49152/callback/abcdefghijklmnop"},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := normalizeDesktopRedirectURI(test.uri)
			if (err == nil) != test.ok {
				t.Fatalf("normalizeDesktopRedirectURI() error = %v, want ok=%v", err, test.ok)
			}
		})
	}
}

func TestDesktopAuthAttemptBindsProviderCallbackAndStartsOnce(t *testing.T) {
	repo := newLoginMappingUserRepo()
	service := newTestServiceWithUserStore(repo)
	provider := desktopOAuthProvider()
	service.settings = loginProviderSettingsStub{providers: map[string]domainsettings.LoginProviderSettings{provider.ID: provider}}
	verifier := strings.Repeat("a", 43)
	challenge := desktopCodeChallenge(verifier)

	attempt, err := service.CreateDesktopAuthAttempt(context.Background(), domainidentity.DesktopAuthAttemptCreate{
		ProviderID: provider.ID, RedirectURI: "http://127.0.0.1:49152/callback/abcdefghijklmnop",
		CodeChallenge: challenge, CodeChallengeMethod: "S256",
	})
	if err != nil {
		t.Fatalf("CreateDesktopAuthAttempt() error = %v", err)
	}
	stored, ok := repo.ephemeral[desktopAuthAttemptKind+"|"+attempt.AttemptID]
	if !ok || stored.Payload["providerId"] != provider.ID || stored.Payload["codeChallenge"] != challenge {
		t.Fatalf("stored desktop attempt = %#v", stored)
	}
	if remaining := time.Until(attempt.ExpiresAt); remaining < 9*time.Minute || remaining > 10*time.Minute+time.Second {
		t.Fatalf("attempt TTL = %s", remaining)
	}

	loginURL, err := service.BeginDesktopAuthAttempt(context.Background(), attempt.AttemptID)
	if err != nil {
		t.Fatalf("BeginDesktopAuthAttempt() error = %v", err)
	}
	parsed, err := url.Parse(loginURL)
	if err != nil {
		t.Fatal(err)
	}
	state := parsed.Query().Get("state")
	stateToken, ok := repo.ephemeral[oauthStateKind+"|"+state]
	if !ok || stateToken.Payload["desktopAttemptId"] != attempt.AttemptID ||
		stateToken.Payload["desktopRedirectUri"] != "http://127.0.0.1:49152/callback/abcdefghijklmnop" ||
		stateToken.Payload["desktopCodeChallenge"] != challenge {
		t.Fatalf("stored provider state = %#v", stateToken)
	}
	if _, err := service.BeginDesktopAuthAttempt(context.Background(), attempt.AttemptID); desktopBusinessCode(err) != "desktop_auth_attempt_expired" {
		t.Fatalf("second BeginDesktopAuthAttempt() error = %v", err)
	}
	if len(repo.sessionsByID) != 0 {
		t.Fatal("starting desktop authentication created a session")
	}
}

func TestCreateDesktopAuthAttemptRejectsUnavailableAndUnsupportedProviders(t *testing.T) {
	for _, test := range []struct {
		name     string
		provider *domainsettings.LoginProviderSettings
		wantCode string
	}{
		{name: "missing", wantCode: "desktop_auth_provider_unavailable"},
		{name: "disabled", provider: &domainsettings.LoginProviderSettings{ID: "disabled", Type: "oidc"}, wantCode: "desktop_auth_provider_unavailable"},
		{name: "password", provider: &domainsettings.LoginProviderSettings{ID: "password", Type: "password", Enabled: true}, wantCode: "desktop_auth_provider_unavailable"},
		{name: "SAML runtime unavailable", provider: &domainsettings.LoginProviderSettings{ID: "saml", Type: "saml", Enabled: true}, wantCode: "desktop_auth_provider_unavailable"},
		{name: "unsupported", provider: &domainsettings.LoginProviderSettings{ID: "ldap", Type: "ldap", Enabled: true}, wantCode: "desktop_auth_provider_unsupported"},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := newLoginMappingUserRepo()
			service := newTestServiceWithUserStore(repo)
			providers := map[string]domainsettings.LoginProviderSettings{}
			providerID := "missing"
			if test.provider != nil {
				providers[test.provider.ID] = *test.provider
				providerID = test.provider.ID
			}
			service.settings = loginProviderSettingsStub{providers: providers}
			_, err := service.CreateDesktopAuthAttempt(context.Background(), domainidentity.DesktopAuthAttemptCreate{
				ProviderID: providerID, RedirectURI: "http://127.0.0.1:49152/callback/abcdefghijklmnop",
				CodeChallenge: desktopCodeChallenge(strings.Repeat("a", 43)), CodeChallengeMethod: "S256",
			})
			if code := desktopBusinessCode(err); code != test.wantCode {
				t.Fatalf("CreateDesktopAuthAttempt() code = %q, want %q (error=%v)", code, test.wantCode, err)
			}
		})
	}
}

func TestDesktopAuthRejectsOversizedPublicIdentifiers(t *testing.T) {
	service := newTestServiceWithUserStore(newLoginMappingUserRepo())
	verifier := strings.Repeat("a", 43)

	if _, err := service.CreateDesktopAuthAttempt(context.Background(), domainidentity.DesktopAuthAttemptCreate{
		ProviderID: strings.Repeat("a", 129), RedirectURI: "http://127.0.0.1:49152/callback/abcdefghijklmnop",
		CodeChallenge: desktopCodeChallenge(verifier), CodeChallengeMethod: "S256",
	}); desktopBusinessCode(err) != "desktop_auth_invalid_provider" {
		t.Fatalf("oversized provider error = %v", err)
	}
	if _, err := service.BeginDesktopAuthAttempt(context.Background(), strings.Repeat("a", 129)); desktopBusinessCode(err) != "desktop_auth_invalid_attempt" {
		t.Fatalf("oversized attempt error = %v", err)
	}
	if _, err := service.ConsumeDesktopAuthAttempt(context.Background(), "attempt-1", strings.Repeat("a", 513), verifier); desktopBusinessCode(err) != "desktop_auth_invalid_exchange" {
		t.Fatalf("oversized exchange code error = %v", err)
	}
}

func TestDesktopAuthExchangeConsumesFailuresAndIssuesOneSession(t *testing.T) {
	repo := newLoginMappingUserRepo()
	repo.usersByID["user-1"] = userrepo.User{
		ID: "user-1", Username: "ada", DisplayName: "Ada", Email: "ada@example.com", Status: "active", AuthzVersion: 1,
	}
	service := newTestServiceWithUserStore(repo)
	service.cfg = cfgpkg.AuthConfig{JWT: cfgpkg.JWTConfig{
		Secret: "desktop-auth-test-secret", Issuer: "soha-test", AccessTTL: time.Minute, RefreshTTL: time.Hour,
	}}
	provider := desktopOAuthProvider()
	principal := domainidentity.Principal{UserID: "user-1", UserName: "ada", Email: "ada@example.com"}
	verifier := strings.Repeat("a", 43)
	completion := desktopAuthCompletion{
		AttemptID: "attempt-1", RedirectURI: "http://127.0.0.1:49152/callback/abcdefghijklmnop",
		CodeChallenge: desktopCodeChallenge(verifier),
	}

	redirectURL, err := service.storeDesktopAuthCallback(context.Background(), principal, provider, completion)
	if err != nil {
		t.Fatal(err)
	}
	callbackCode := callbackCodeFromRedirect(t, redirectURL)
	if strings.Contains(redirectURL, "accessToken") || strings.Contains(redirectURL, "refreshToken") || len(repo.sessionsByID) != 0 {
		t.Fatalf("callback leaked credentials or created a session: %s", redirectURL)
	}
	if _, err := service.ConsumeDesktopAuthAttempt(context.Background(), completion.AttemptID, callbackCode, strings.Repeat("b", 43)); desktopBusinessCode(err) != "desktop_auth_verification_failed" {
		t.Fatalf("wrong verifier error = %v", err)
	}
	if len(repo.sessionsByID) != 0 {
		t.Fatal("wrong verifier created a session")
	}
	if _, err := service.ConsumeDesktopAuthAttempt(context.Background(), completion.AttemptID, callbackCode, verifier); desktopBusinessCode(err) != "desktop_auth_code_expired" {
		t.Fatalf("failed callback code replay error = %v", err)
	}

	redirectURL, err = service.storeDesktopAuthCallback(context.Background(), principal, provider, completion)
	if err != nil {
		t.Fatal(err)
	}
	callbackCode = callbackCodeFromRedirect(t, redirectURL)
	result, err := service.ConsumeDesktopAuthAttempt(context.Background(), completion.AttemptID, callbackCode, verifier)
	if err != nil || result.Tokens.AccessToken == "" || result.Tokens.RefreshToken == "" || len(repo.sessionsByID) != 1 {
		t.Fatalf("successful exchange result=%#v error=%v sessions=%d", result, err, len(repo.sessionsByID))
	}
	if _, err := service.ConsumeDesktopAuthAttempt(context.Background(), completion.AttemptID, callbackCode, verifier); desktopBusinessCode(err) != "desktop_auth_code_expired" {
		t.Fatalf("successful callback code replay error = %v", err)
	}
	if len(repo.sessionsByID) != 1 {
		t.Fatalf("replay created another session: %d", len(repo.sessionsByID))
	}
}

func TestDesktopAuthExchangeConsumesCallbackCodeAtomically(t *testing.T) {
	repo := newLoginMappingUserRepo()
	repo.usersByID["user-1"] = userrepo.User{
		ID: "user-1", Username: "ada", DisplayName: "Ada", Email: "ada@example.com", Status: "active", AuthzVersion: 1,
	}
	service := newTestServiceWithUserStore(repo)
	service.cfg = cfgpkg.AuthConfig{JWT: cfgpkg.JWTConfig{
		Secret: "desktop-auth-test-secret", Issuer: "soha-test", AccessTTL: time.Minute, RefreshTTL: time.Hour,
	}}
	verifier := strings.Repeat("a", 43)
	completion := desktopAuthCompletion{
		AttemptID: "attempt-1", RedirectURI: "http://127.0.0.1:49152/callback/abcdefghijklmnop",
		CodeChallenge: desktopCodeChallenge(verifier),
	}
	redirectURL, err := service.storeDesktopAuthCallback(
		context.Background(),
		domainidentity.Principal{UserID: "user-1", UserName: "ada", Email: "ada@example.com"},
		desktopOAuthProvider(),
		completion,
	)
	if err != nil {
		t.Fatal(err)
	}
	callbackCode := callbackCodeFromRedirect(t, redirectURL)

	const workers = 16
	type outcome struct {
		result domainidentity.AuthResult
		err    error
	}
	start := make(chan struct{})
	outcomes := make(chan outcome, workers)
	var wait sync.WaitGroup
	for range workers {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			result, err := service.ConsumeDesktopAuthAttempt(context.Background(), completion.AttemptID, callbackCode, verifier)
			outcomes <- outcome{result: result, err: err}
		}()
	}
	close(start)
	wait.Wait()
	close(outcomes)

	successes := 0
	for result := range outcomes {
		if result.err == nil {
			successes++
			if result.result.Tokens.AccessToken == "" || result.result.Tokens.RefreshToken == "" {
				t.Fatal("successful exchange returned empty tokens")
			}
			continue
		}
		if code := desktopBusinessCode(result.err); code != "desktop_auth_code_expired" {
			t.Fatalf("concurrent exchange error code = %q, want desktop_auth_code_expired", code)
		}
	}
	if successes != 1 || len(repo.sessionsByID) != 1 {
		t.Fatalf("successful exchanges = %d, sessions = %d; want 1 and 1", successes, len(repo.sessionsByID))
	}
}

func desktopOAuthProvider() domainsettings.LoginProviderSettings {
	return domainsettings.LoginProviderSettings{
		ID: "oauth-main", Name: "OAuth Main", Type: "oauth2", Enabled: true,
		ClientID: "client-1", ClientSecret: "secret-1",
		AuthorizeURL: "https://idp.example/authorize", TokenURL: "https://idp.example/token",
		RedirectURL: "https://soha.example/api/v1/auth/login/oauth-main/callback",
	}
}

func callbackCodeFromRedirect(t *testing.T, rawURL string) string {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Query().Get("attempt") == "" || parsed.Query().Get("code") == "" {
		t.Fatalf("desktop callback query = %q", parsed.RawQuery)
	}
	return parsed.Query().Get("code")
}

func desktopBusinessCode(err error) string {
	var business *apperrors.BusinessError
	if !errors.As(err, &business) {
		return ""
	}
	return business.Code()
}
