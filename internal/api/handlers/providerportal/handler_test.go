package providerportal

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainprovider "github.com/opensoha/soha/internal/domain/identityprovider"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestProxySessionCookieUsesSecureHTTPOnlySettings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodGet, "http://soha.example/api/v1/provider/proxy/callback", nil)
	c.Request.Header.Set("X-Forwarded-Proto", "https")

	setProxySessionCookie(c, domainprovider.ProxySession{
		Token:     "proxy-token-1",
		ExpiresAt: time.Now().Add(time.Hour),
	}, "example.com")

	cookies := recorder.Header().Values("Set-Cookie")
	if len(cookies) != 1 {
		t.Fatalf("Set-Cookie count = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	for _, want := range []string{
		proxySessionCookieName + "=proxy-token-1",
		"Path=/",
		"Domain=example.com",
		"HttpOnly",
		"Secure",
		"SameSite=Lax",
	} {
		if !strings.Contains(cookie, want) {
			t.Fatalf("Set-Cookie = %q, want substring %q", cookie, want)
		}
	}
}

func TestClearProxySessionCookieExpiresCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "http://soha.example/api/v1/provider/proxy/logout", nil)

	clearProxySessionCookie(c, "example.com")

	cookies := recorder.Header().Values("Set-Cookie")
	if len(cookies) != 1 {
		t.Fatalf("Set-Cookie count = %d, want 1", len(cookies))
	}
	cookie := cookies[0]
	for _, want := range []string{
		proxySessionCookieName + "=",
		"Path=/",
		"Domain=example.com",
		"HttpOnly",
		"Max-Age=0",
		"SameSite=Lax",
	} {
		if !strings.Contains(cookie, want) {
			t.Fatalf("Set-Cookie = %q, want substring %q", cookie, want)
		}
	}
}

func TestProxyAuthInputReadsProxySessionCookie(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodGet, "http://soha.example/api/v1/provider/proxy/auth", nil)
	request.AddCookie(&http.Cookie{Name: proxySessionCookieName, Value: "proxy-token-1"})
	c.Request = request

	input := proxyAuthInputFromRequest(c)
	if input.SessionToken != "proxy-token-1" {
		t.Fatalf("SessionToken = %q, want proxy-token-1", input.SessionToken)
	}
}

func TestOutpostTokenFromRequestPrefersBearerToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "http://soha.example/api/v1/provider/outposts/claim", nil)
	request.Header.Set("Authorization", "Bearer outpost-token-1")
	c.Request = request

	if token := outpostTokenFromRequest(c, "body-token"); token != "outpost-token-1" {
		t.Fatalf("outpost token = %q, want bearer token", token)
	}
}

func TestOIDCClientAuthInputFromRequestPrefersBasicAuth(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "http://soha.example/oauth2/introspect", strings.NewReader("client_id=post-client&client_secret=post-secret"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	request.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte("basic-client:basic-secret")))
	c.Request = request

	auth := oidcClientAuthInputFromRequest(c)
	if auth.ClientID != "basic-client" || auth.ClientSecret != "basic-secret" {
		t.Fatalf("client auth = %#v, want basic credentials", auth)
	}
}

func TestOIDCClientAuthInputFromRequestReadsPostBody(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	request := httptest.NewRequest(http.MethodPost, "http://soha.example/oauth2/revoke", strings.NewReader("client_id=post-client&client_secret=post-secret"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	c.Request = request

	auth := oidcClientAuthInputFromRequest(c)
	if auth.ClientID != "post-client" || auth.ClientSecret != "post-secret" {
		t.Fatalf("client auth = %#v, want post credentials", auth)
	}
}

func TestOIDCAuthorizeRedirectsSafeOAuthError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := newProtocolTestHandler(&stubProviderPortalIdentityProvider{
		authorizeFunc: func(_ context.Context, _ string, principal domainidentity.Principal, input domainprovider.AuthorizeInput) (domainprovider.AuthorizeResult, error) {
			if principal.UserID != "user-1" {
				t.Fatalf("principal user = %q, want user-1", principal.UserID)
			}
			if input.ClientID != "client-1" || input.State != "state-1" {
				t.Fatalf("authorize input = %#v", input)
			}
			err := fmt.Errorf("%w: scope is not allowed", apperrors.ErrInvalidArgument)
			return domainprovider.AuthorizeResult{}, &domainprovider.AuthorizeRedirectError{
				RedirectURI: "https://app.example/callback?existing=1",
				State:       input.State,
				Code:        "invalid_scope",
				Description: err.Error(),
				Err:         err,
			}
		},
	})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("principal", domainidentity.Principal{UserID: "user-1"})
		c.Next()
	})
	router.GET("/oauth2/authorize", handler.OIDCAuthorize)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/oauth2/authorize?response_type=code&client_id=client-1&redirect_uri="+url.QueryEscape("https://app.example/callback")+"&state=state-1", nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusFound {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusFound, recorder.Body.String())
	}
	location := recorder.Header().Get("Location")
	parsed, err := url.Parse(location)
	if err != nil {
		t.Fatalf("parse Location %q: %v", location, err)
	}
	if parsed.Scheme != "https" || parsed.Host != "app.example" || parsed.Path != "/callback" {
		t.Fatalf("Location = %q, want app callback", location)
	}
	query := parsed.Query()
	if query.Get("existing") != "1" || query.Get("error") != "invalid_scope" || query.Get("state") != "state-1" {
		t.Fatalf("Location query = %s", parsed.RawQuery)
	}
	if query.Get("error_description") != "invalid argument: scope is not allowed" {
		t.Fatalf("error_description = %q", query.Get("error_description"))
	}
}

func TestOIDCAuthorizeInvalidClientRemainsJSONError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := newProtocolTestHandler(&stubProviderPortalIdentityProvider{
		authorizeFunc: func(context.Context, string, domainidentity.Principal, domainprovider.AuthorizeInput) (domainprovider.AuthorizeResult, error) {
			return domainprovider.AuthorizeResult{}, fmt.Errorf("%w: oidc client not found", apperrors.ErrNotFound)
		},
	})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("principal", domainidentity.Principal{UserID: "user-1"})
		c.Next()
	})
	router.GET("/oauth2/authorize", handler.OIDCAuthorize)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/oauth2/authorize?response_type=token&client_id=missing-client&redirect_uri="+url.QueryEscape("https://evil.example/callback"), nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusNotFound)
	}
	if location := recorder.Header().Get("Location"); location != "" {
		t.Fatalf("Location = %q, want no redirect", location)
	}
	assertOIDCErrorCode(t, recorder.Body.String(), "invalid_request")
}

func TestProxyStartDoesNotRedirectWhenReturnToRejected(t *testing.T) {
	gin.SetMode(gin.TestMode)
	target := "https://evil.example/app"
	var gotInput domainprovider.ProxyAuthInput
	handler := newProtocolTestHandler(&stubProviderPortalIdentityProvider{
		proxyAuthFunc: func(_ context.Context, _ domainidentity.Principal, input domainprovider.ProxyAuthInput) (domainprovider.ProxyAuthResult, error) {
			gotInput = input
			return domainprovider.ProxyAuthResult{}, fmt.Errorf("%w: proxy host does not match provider", apperrors.ErrAccessDenied)
		},
	})
	router := gin.New()
	router.GET("/api/v1/provider/proxy/start", handler.ProxyStart)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/provider/proxy/start?provider_id=proxy-1&return_to="+url.QueryEscape(target), nil)
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
	if location := recorder.Header().Get("Location"); location != "" {
		t.Fatalf("Location = %q, want empty redirect", location)
	}
	if gotInput.ProviderID != "proxy-1" || gotInput.OriginalURL != target {
		t.Fatalf("proxy auth input = %#v, want provider_id proxy-1 and return_to %q", gotInput, target)
	}
}

func TestProxyCallbackSetsCookieOnlyAfterAllow(t *testing.T) {
	tests := []struct {
		name           string
		result         domainprovider.ProxyAuthResult
		wantStatus     int
		wantSetCookie  bool
		wantIssueToken bool
	}{
		{
			name: "denied callback does not issue proxy cookie",
			result: domainprovider.ProxyAuthResult{
				Decision: domainprovider.ProxyDecisionDeny,
				Reason:   "application access denied",
			},
			wantStatus: http.StatusForbidden,
		},
		{
			name: "allowed callback issues proxy cookie and redirects",
			result: domainprovider.ProxyAuthResult{
				Decision:     domainprovider.ProxyDecisionAllow,
				OriginalURL:  "https://app.example/dash",
				CookieDomain: "example.com",
			},
			wantStatus:     http.StatusFound,
			wantSetCookie:  true,
			wantIssueToken: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			issueCalled := false
			handler := newProtocolTestHandler(&stubProviderPortalIdentityProvider{
				proxyAuthFunc: func(_ context.Context, principal domainidentity.Principal, input domainprovider.ProxyAuthInput) (domainprovider.ProxyAuthResult, error) {
					if principal.UserID != "user-1" {
						t.Fatalf("principal user = %q, want user-1", principal.UserID)
					}
					if input.OriginalURL != "https://app.example/dash" {
						t.Fatalf("return_to = %q, want https://app.example/dash", input.OriginalURL)
					}
					return tt.result, nil
				},
				issueProxySessionFunc: func(_ context.Context, principal domainidentity.Principal) (domainprovider.ProxySession, error) {
					issueCalled = true
					if principal.UserID != "user-1" {
						t.Fatalf("session principal user = %q, want user-1", principal.UserID)
					}
					return domainprovider.ProxySession{
						Token:     "proxy-token-1",
						ExpiresAt: time.Now().Add(time.Hour),
					}, nil
				},
			})
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set("principal", domainidentity.Principal{UserID: "user-1", UserName: "Ada"})
				c.Next()
			})
			router.GET("/api/v1/provider/proxy/callback", handler.ProxyCallback)

			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/provider/proxy/callback?return_to="+url.QueryEscape("https://app.example/dash"), nil)
			request.Header.Set("X-Forwarded-Proto", "https")
			router.ServeHTTP(recorder, request)

			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.wantStatus)
			}
			cookie := strings.Join(recorder.Header().Values("Set-Cookie"), "\n")
			if tt.wantSetCookie && !strings.Contains(cookie, proxySessionCookieName+"=proxy-token-1") {
				t.Fatalf("Set-Cookie = %q, want proxy session cookie", cookie)
			}
			if !tt.wantSetCookie && cookie != "" {
				t.Fatalf("Set-Cookie = %q, want no cookie", cookie)
			}
			if issueCalled != tt.wantIssueToken {
				t.Fatalf("IssueProxySession called = %v, want %v", issueCalled, tt.wantIssueToken)
			}
		})
	}
}

func TestOIDCIntrospectClientAuthFailureReturnsInvalidClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var gotAuth domainprovider.ClientAuthInput
	handler := newProtocolTestHandler(&stubProviderPortalIdentityProvider{
		introspectFunc: func(_ context.Context, _ string, _ string, auth domainprovider.ClientAuthInput) (domainprovider.IntrospectionResponse, error) {
			gotAuth = auth
			return domainprovider.IntrospectionResponse{}, fmt.Errorf("%w: client authentication is required", apperrors.ErrUnauthorized)
		},
	})
	router := gin.New()
	router.POST("/oauth2/introspect", handler.OIDCIntrospect)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/oauth2/introspect", strings.NewReader("token=access-token"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if gotAuth.ClientID != "" || gotAuth.ClientSecret != "" {
		t.Fatalf("client auth = %#v, want empty credentials", gotAuth)
	}
	assertOIDCErrorCode(t, recorder.Body.String(), "invalid_client")
}

func TestOIDCRevokeClientAuthFailureReturnsInvalidClient(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var gotAuth domainprovider.ClientAuthInput
	handler := newProtocolTestHandler(&stubProviderPortalIdentityProvider{
		revokeFunc: func(_ context.Context, _ string, _ string, auth domainprovider.ClientAuthInput) error {
			gotAuth = auth
			return fmt.Errorf("%w: invalid client credentials", apperrors.ErrUnauthorized)
		},
	})
	router := gin.New()
	router.POST("/oauth2/revoke", handler.OIDCRevoke)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/oauth2/revoke", strings.NewReader("token=access-token&client_id=client-1&client_secret=wrong"))
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if gotAuth.ClientID != "client-1" || gotAuth.ClientSecret != "wrong" {
		t.Fatalf("client auth = %#v, want post credentials", gotAuth)
	}
	assertOIDCErrorCode(t, recorder.Body.String(), "invalid_client")
}

func assertOIDCErrorCode(t *testing.T, body string, want string) {
	t.Helper()
	var payload map[string]string
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode OIDC error body: %v; body=%q", err, body)
	}
	if payload["error"] != want {
		t.Fatalf("OIDC error = %q, want %q; body=%q", payload["error"], want, body)
	}
}

type stubProviderPortalIdentityProvider struct {
	authorizeFunc         func(context.Context, string, domainidentity.Principal, domainprovider.AuthorizeInput) (domainprovider.AuthorizeResult, error)
	proxyAuthFunc         func(context.Context, domainidentity.Principal, domainprovider.ProxyAuthInput) (domainprovider.ProxyAuthResult, error)
	issueProxySessionFunc func(context.Context, domainidentity.Principal) (domainprovider.ProxySession, error)
	introspectFunc        func(context.Context, string, string, domainprovider.ClientAuthInput) (domainprovider.IntrospectionResponse, error)
	revokeFunc            func(context.Context, string, string, domainprovider.ClientAuthInput) error
}

func newProtocolTestHandler(service *stubProviderPortalIdentityProvider) *Handler {
	return New(Services{
		OIDC:           service,
		Proxy:          service,
		OutpostRuntime: service,
	})
}

func (s *stubProviderPortalIdentityProvider) Discovery(string) domainprovider.DiscoveryDocument {
	return domainprovider.DiscoveryDocument{}
}

func (s *stubProviderPortalIdentityProvider) JWKS(context.Context) (domainprovider.JWKS, error) {
	return domainprovider.JWKS{}, apperrors.ErrUnsupportedOperation
}

func (s *stubProviderPortalIdentityProvider) Authorize(ctx context.Context, issuer string, principal domainidentity.Principal, input domainprovider.AuthorizeInput) (domainprovider.AuthorizeResult, error) {
	if s.authorizeFunc != nil {
		return s.authorizeFunc(ctx, issuer, principal, input)
	}
	return domainprovider.AuthorizeResult{}, apperrors.ErrUnsupportedOperation
}

func (s *stubProviderPortalIdentityProvider) Token(context.Context, string, domainprovider.TokenInput) (domainprovider.TokenResponse, error) {
	return domainprovider.TokenResponse{}, apperrors.ErrUnsupportedOperation
}

func (s *stubProviderPortalIdentityProvider) Introspect(ctx context.Context, issuer, token string, auth domainprovider.ClientAuthInput) (domainprovider.IntrospectionResponse, error) {
	if s.introspectFunc != nil {
		return s.introspectFunc(ctx, issuer, token, auth)
	}
	return domainprovider.IntrospectionResponse{}, apperrors.ErrUnsupportedOperation
}

func (s *stubProviderPortalIdentityProvider) Revoke(ctx context.Context, issuer, token string, auth domainprovider.ClientAuthInput) error {
	if s.revokeFunc != nil {
		return s.revokeFunc(ctx, issuer, token, auth)
	}
	return apperrors.ErrUnsupportedOperation
}

func (s *stubProviderPortalIdentityProvider) UserInfo(context.Context, string, string) (domainprovider.UserInfoResponse, error) {
	return domainprovider.UserInfoResponse{}, apperrors.ErrUnsupportedOperation
}

func (s *stubProviderPortalIdentityProvider) ProxyAuth(ctx context.Context, principal domainidentity.Principal, input domainprovider.ProxyAuthInput) (domainprovider.ProxyAuthResult, error) {
	if s.proxyAuthFunc != nil {
		return s.proxyAuthFunc(ctx, principal, input)
	}
	return domainprovider.ProxyAuthResult{}, apperrors.ErrUnsupportedOperation
}

func (s *stubProviderPortalIdentityProvider) ProxyCookieDomain(context.Context, domainprovider.ProxyAuthInput) (string, error) {
	return "", nil
}

func (s *stubProviderPortalIdentityProvider) IssueProxySession(ctx context.Context, principal domainidentity.Principal) (domainprovider.ProxySession, error) {
	if s.issueProxySessionFunc != nil {
		return s.issueProxySessionFunc(ctx, principal)
	}
	return domainprovider.ProxySession{}, apperrors.ErrUnsupportedOperation
}

func (s *stubProviderPortalIdentityProvider) ClaimOutpost(context.Context, domainprovider.OutpostClaimInput) (domainprovider.OutpostClaimResult, error) {
	return domainprovider.OutpostClaimResult{}, apperrors.ErrUnsupportedOperation
}

func (s *stubProviderPortalIdentityProvider) HeartbeatOutpost(context.Context, string, domainprovider.OutpostHeartbeatInput) (domainprovider.OutpostHeartbeatResult, error) {
	return domainprovider.OutpostHeartbeatResult{}, apperrors.ErrUnsupportedOperation
}

func (s *stubProviderPortalIdentityProvider) CheckOutpost(context.Context, string, domainprovider.OutpostCheckInput) (domainprovider.ProxyAuthResult, error) {
	return domainprovider.ProxyAuthResult{}, apperrors.ErrUnsupportedOperation
}

func (s *stubProviderPortalIdentityProvider) RecordOutpostEvents(context.Context, string, domainprovider.OutpostEventsInput) (domainprovider.OutpostEventsResult, error) {
	return domainprovider.OutpostEventsResult{}, apperrors.ErrUnsupportedOperation
}
