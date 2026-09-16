package providerportal

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/golang-jwt/jwt/v5"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
)

func TestOIDCProtocolHTTPWithPostgres(t *testing.T) {
	f := newSSOProtocolFixture(t)
	callback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(r.URL.Query())
	}))
	defer callback.Close()
	request, _ := http.NewRequest("GET", f.server.URL+"/.well-known/openid-configuration", nil)
	body, _ := ssoHTTP(t, f.client, request, http.StatusOK)
	var discovery domainprovider.DiscoveryDocument
	if err := json.Unmarshal(body, &discovery); err != nil || discovery.Issuer != f.server.URL {
		t.Fatalf("discovery: %v", err)
	}
	for _, kind := range []string{"confidential", "public"} {
		t.Run(kind, func(t *testing.T) {
			verifyOIDCClientFlow(t, f, callback.URL, discovery, kind)
		})
	}
}

func verifyOIDCClientIDToken(t *testing.T, f *ssoProtocolFixture, discovery domainprovider.DiscoveryDocument, audience, raw string) {
	t.Helper()
	request, _ := http.NewRequest("GET", discovery.JWKSURI, nil)
	body, _ := ssoHTTP(t, f.client, request, http.StatusOK)
	var jwks struct {
		Keys []struct {
			Kid string `json:"kid"`
			X   string `json:"x"`
			Y   string `json:"y"`
		} `json:"keys"`
	}
	ssoNoError(t, json.Unmarshal(body, &jwks))
	claims := jwt.MapClaims{}
	_, err := jwt.ParseWithClaims(raw, claims, func(token *jwt.Token) (any, error) {
		for _, key := range jwks.Keys {
			if key.Kid != token.Header["kid"] {
				continue
			}
			x, err := base64.RawURLEncoding.DecodeString(key.X)
			if err != nil {
				return nil, err
			}
			y, err := base64.RawURLEncoding.DecodeString(key.Y)
			if err != nil {
				return nil, err
			}
			return &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}, nil
		}
		return nil, fmt.Errorf("JWKS signing key missing")
	}, jwt.WithValidMethods([]string{"ES256"}), jwt.WithIssuer(discovery.Issuer), jwt.WithAudience(audience), jwt.WithExpirationRequired())
	ssoCheck(t, err == nil && claims["nonce"] == "opaque-nonce" && claims["sub"] == f.principal.UserID, "client ID token verification: %v", err)
}

func decodeOIDCTestTokens(t *testing.T, data []byte) domainprovider.TokenResponse {
	t.Helper()
	var tokens domainprovider.TokenResponse
	if err := json.Unmarshal(data, &tokens); err != nil || tokens.AccessToken == "" || tokens.IDToken == "" || tokens.RefreshToken == "" {
		t.Fatalf("missing tokens: %v", err)
	}
	return tokens
}

//nolint:funlen // One client lifecycle keeps code replay, refresh rotation, revocation and logout assertions in order.
func verifyOIDCClientFlow(t *testing.T, f *ssoProtocolFixture, callbackURL string, discovery domainprovider.DiscoveryDocument, kind string) {
	t.Helper()
	input := onboardingTestInput("oidc")
	input.OIDCClient.ClientType = kind
	input.OIDCClient.RedirectURIs = []string{callbackURL + "/callback"}
	input.OIDCClient.PostLogoutRedirectURIs = []string{callbackURL + "/logout"}
	input.OIDCClient.AllowedScopes = []string{"openid", "profile", "offline_access"}
	input.OIDCClient.AllowedGrantTypes = []string{"authorization_code", "refresh_token"}
	result := f.onboard(t, input)
	clientID, secret := result.OIDCClient.Client.ClientID, result.OIDCClient.ClientSecret
	ssoCheck(t, kind != "public" || secret == "", "public client received secret")
	const verifier = "0123456789abcdefghijklmnopqrstuvwxyz-._~ABCDEFGHIJKLMNOPQRSTUVWXYZ"
	sum := sha256.Sum256([]byte(verifier))
	values := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {callbackURL + "/callback"}, "scope": {"openid profile offline_access"}, "state": {"opaque-state"}, "nonce": {"opaque-nonce"}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}}
	authorize := func(session string) string {
		t.Helper()
		req, _ := http.NewRequest("GET", discovery.AuthorizationEndpoint+"?"+values.Encode(), nil)
		req.AddCookie(&http.Cookie{Name: "test-platform-session", Value: f.login(t, session)})
		_, headers := ssoHTTP(t, f.client, req, http.StatusFound)
		target, err := url.Parse(headers.Get("Location"))
		ssoCheck(t, err == nil && strings.HasPrefix(target.String(), callbackURL+"/callback?") && target.Query().Get("error") == "", "authorization callback rejected: %v", err)
		callbackRequest, _ := http.NewRequest("GET", target.String(), nil)
		callbackBody, _ := ssoHTTP(t, f.client, callbackRequest, http.StatusOK)
		var received url.Values
		if err := json.Unmarshal(callbackBody, &received); err != nil || received.Get("state") != "opaque-state" || received.Get("code") == "" {
			t.Fatalf("callback code/state: %v", err)
		}
		return received.Get("code")
	}
	post := func(endpoint string, form url.Values, status int) []byte {
		t.Helper()
		if kind == "public" {
			form.Set("client_id", clientID)
		}
		req, _ := http.NewRequest("POST", endpoint, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if kind == "confidential" {
			req.SetBasicAuth(clientID, secret)
		}
		data, _ := ssoHTTP(t, f.client, req, status)
		return data
	}
	exchangeForm := func(code, proof string) url.Values {
		return url.Values{"grant_type": {"authorization_code"}, "code": {code}, "redirect_uri": {callbackURL + "/callback"}, "code_verifier": {proof}}
	}

	code := authorize(kind + "-initial")
	tokens := decodeOIDCTestTokens(t, post(discovery.TokenEndpoint, exchangeForm(code, verifier), http.StatusOK))
	verifyOIDCClientIDToken(t, f, discovery, clientID, tokens.IDToken)
	post(discovery.TokenEndpoint, exchangeForm(code, verifier), http.StatusUnauthorized)
	infoRequest, _ := http.NewRequest("GET", discovery.UserInfoEndpoint, nil)
	infoRequest.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	info, _ := ssoHTTP(t, f.client, infoRequest, http.StatusOK)
	var user map[string]any
	if err := json.Unmarshal(info, &user); err != nil || user["sub"] != f.principal.UserID {
		t.Fatalf("userinfo: %v", err)
	}
	for _, proof := range []string{"", "wrong-verifier"} {
		post(discovery.TokenEndpoint, exchangeForm(authorize(kind+"-pkce"), proof), http.StatusUnauthorized)
	}
	wrongRedirect := exchangeForm(authorize(kind+"-redirect"), verifier)
	wrongRedirect.Set("redirect_uri", callbackURL+"/unregistered")
	post(discovery.TokenEndpoint, wrongRedirect, http.StatusUnauthorized)
	values.Set("redirect_uri", "https://unregistered.invalid/callback")
	badRequest, _ := http.NewRequest("GET", discovery.AuthorizationEndpoint+"?"+values.Encode(), nil)
	ssoHTTP(t, f.client, badRequest, http.StatusBadRequest)
	values.Set("redirect_uri", callbackURL+"/callback")
	challenge := values.Get("code_challenge")
	values.Del("code_challenge")
	badRequest, _ = http.NewRequest("GET", discovery.AuthorizationEndpoint+"?"+values.Encode(), nil)
	badRequest.AddCookie(&http.Cookie{Name: "test-platform-session", Value: f.login(t, kind+"-pkce")})
	_, missingPKCEHeaders := ssoHTTP(t, f.client, badRequest, http.StatusFound)
	missingPKCE, err := url.Parse(missingPKCEHeaders.Get("Location"))
	ssoCheck(t, err == nil && missingPKCE.Query().Get("error") == "invalid_request" && missingPKCE.Query().Get("code") == "", "missing PKCE did not return a registered error callback: %v", err)
	values.Set("code_challenge", challenge)
	refresh := func(token string) url.Values {
		return url.Values{"grant_type": {"refresh_token"}, "refresh_token": {token}}
	}
	rotated := decodeOIDCTestTokens(t, post(discovery.TokenEndpoint, refresh(tokens.RefreshToken), http.StatusOK))
	ssoCheck(t, rotated.RefreshToken != tokens.RefreshToken, "refresh token did not rotate")
	post(discovery.TokenEndpoint, refresh(tokens.RefreshToken), http.StatusUnauthorized)
	post(discovery.TokenEndpoint, refresh(rotated.RefreshToken), http.StatusUnauthorized)
	revoked := decodeOIDCTestTokens(t, post(discovery.TokenEndpoint, exchangeForm(authorize(kind+"-revoked"), verifier), http.StatusOK))
	ssoNoError(t, f.users.RevokeSessionByID(context.Background(), f.sessionID(kind+"-revoked")))
	post(discovery.TokenEndpoint, refresh(revoked.RefreshToken), http.StatusUnauthorized)
	logoutTokens := decodeOIDCTestTokens(t, post(discovery.TokenEndpoint, exchangeForm(authorize(kind+"-logout"), verifier), http.StatusOK))
	logout := url.Values{"id_token_hint": {logoutTokens.IDToken}, "post_logout_redirect_uri": {callbackURL + "/unregistered"}, "state": {"logout-state"}}
	logoutRequest, _ := http.NewRequest("GET", discovery.EndSessionEndpoint+"?"+logout.Encode(), nil)
	ssoHTTP(t, f.client, logoutRequest, http.StatusBadRequest)
	logout.Set("post_logout_redirect_uri", callbackURL+"/logout")
	logoutRequest, _ = http.NewRequest("GET", discovery.EndSessionEndpoint+"?"+logout.Encode(), nil)
	_, headers := ssoHTTP(t, f.client, logoutRequest, http.StatusFound)
	ssoCheck(t, headers.Get("Location") == callbackURL+"/logout?state=logout-state", "logout did not preserve registered callback/state")
	session, err := f.users.GetAuthSessionByID(context.Background(), f.sessionID(kind+"-logout"))
	ssoCheck(t, err == nil && session.Status == "revoked", "logout did not revoke platform session: %v", err)
	post(discovery.TokenEndpoint, refresh(logoutTokens.RefreshToken), http.StatusUnauthorized)
}
