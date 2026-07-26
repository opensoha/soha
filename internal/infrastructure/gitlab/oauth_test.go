package gitlab

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	appsystemintegration "github.com/opensoha/soha/internal/application/systemintegration"
)

func TestOAuthProviderBuildsAuthorizationURLAndExchangesCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/gitlab/oauth/token" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") != "authorization_code" || r.Form.Get("code") != "oauth-code" || r.Form.Get("client_secret") != "secret" {
			t.Fatalf("unexpected token form: %#v", r.Form)
		}
		_, _ = fmt.Fprint(w, `{"access_token":"access","refresh_token":"refresh","expires_in":7200}`)
	}))
	defer server.Close()

	provider := NewOAuthProvider()
	config := appsystemintegration.OAuthProviderConfig{
		BaseURL:      server.URL + "/gitlab/api/v4",
		ClientID:     "client-id",
		ClientSecret: "secret",
		RedirectURI:  "https://soha.example/api/v1/system-integrations/oauth/gitlab/callback",
	}
	authorizationURL, err := provider.AuthorizationURL(config, "opaque-state")
	if err != nil {
		t.Fatalf("AuthorizationURL() error = %v", err)
	}
	parsed, _ := url.Parse(authorizationURL)
	if parsed.Path != "/gitlab/oauth/authorize" || parsed.Query().Get("state") != "opaque-state" || parsed.Query().Get("scope") != "api" {
		t.Fatalf("authorization URL = %q", authorizationURL)
	}
	token, err := provider.Exchange(context.Background(), config, "oauth-code")
	if err != nil {
		t.Fatalf("Exchange() error = %v", err)
	}
	if token.AccessToken != "access" || token.RefreshToken != "refresh" || token.ExpiresIn != 7200 {
		t.Fatalf("token = %#v", token)
	}
}
