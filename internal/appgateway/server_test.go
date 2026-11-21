package appgateway

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/stretchr/testify/require"
)

const (
	testAppsDomain   = "apps.example.com"
	testAppHost      = "sales-dashboard-a1b2.apps.example.com"
	testGatewayToken = "gw-secret-token-v1"
	testCookieSecret = "0123456789abcdef0123456789abcdef-secret"
)

type fakeCoreClient struct {
	mu           sync.Mutex
	readyErr     error
	exchangeFn   func(ctx context.Context, req sohaapi.HostedAppTicketExchangeRequest, token string) (sohaapi.HostedAppTicketExchangeResult, error)
	accessFn     func(ctx context.Context, req sohaapi.HostedAppGatewayAccessCheckRequest, token string) (sohaapi.HostedAppGatewayAccessDecision, error)
	accessCalls  atomic.Int64
	lastCheckReq sohaapi.HostedAppGatewayAccessCheckRequest
	lastToken    string
}

func (f *fakeCoreClient) ExchangeHostedAppGatewayTicket(ctx context.Context, req sohaapi.HostedAppTicketExchangeRequest, gatewayToken ...string) (sohaapi.HostedAppTicketExchangeResult, error) {
	tok := firstToken(gatewayToken)
	f.mu.Lock()
	f.lastToken = tok
	fn := f.exchangeFn
	f.mu.Unlock()
	if fn == nil {
		return sohaapi.HostedAppTicketExchangeResult{}, errors.New("POST /app-hosting/gateway/tickets/exchange failed: 403 Forbidden: FORBIDDEN: unexpected exchange")
	}
	return fn(ctx, req, tok)
}

func (f *fakeCoreClient) CheckHostedAppGatewayAccess(ctx context.Context, req sohaapi.HostedAppGatewayAccessCheckRequest, gatewayToken ...string) (sohaapi.HostedAppGatewayAccessDecision, error) {
	f.accessCalls.Add(1)
	tok := firstToken(gatewayToken)
	f.mu.Lock()
	f.lastCheckReq = req
	f.lastToken = tok
	fn := f.accessFn
	f.mu.Unlock()
	if fn == nil {
		return sohaapi.HostedAppGatewayAccessDecision{
			Decision: sohaapi.HostedAppAccessDecisionKindNotFound,
			Host:     req.Host,
		}, nil
	}
	return fn(ctx, req, tok)
}

func (f *fakeCoreClient) GetReadyz(_ context.Context) (sohaapi.GenericObject, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.readyErr != nil {
		return nil, f.readyErr
	}
	return sohaapi.GenericObject{"status": "ready"}, nil
}

func firstToken(tokens []string) string {
	if len(tokens) == 0 {
		return ""
	}
	return tokens[0]
}

type memoryObjectReader struct {
	mu      sync.Mutex
	objects map[string][]byte
	gets    atomic.Int64
	lastKey string
}

func newMemoryObjectReader(items map[string]string) *memoryObjectReader {
	raw := make(map[string][]byte, len(items))
	for k, v := range items {
		raw[k] = []byte(v)
	}
	return &memoryObjectReader{objects: raw}
}

func (m *memoryObjectReader) GetObject(_ context.Context, key string) (*ObjectStream, error) {
	m.gets.Add(1)
	m.mu.Lock()
	m.lastKey = key
	body, ok := m.objects[key]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("object %q not found", key)
	}
	return &ObjectStream{
		Body:          io.NopCloser(bytes.NewReader(body)),
		ContentLength: int64(len(body)),
	}, nil
}

func defaultStaticObjects() *memoryObjectReader {
	return newMemoryObjectReader(map[string]string{
		"hosted-apps/app_sales/rev_01/index.html":       "<!doctype html><h1>Sales Dashboard</h1>",
		"hosted-apps/app_sales/rev_01/about/index.html": "<h1>About Sales</h1>",
		"hosted-apps/app_sales/rev_01/assets/app.js":    "console.log('sales-app');\n",
		"hosted-apps/app_sales/rev_01/assets/style.css": "body{color:#0f172a;}\n",
	})
}

func sampleAllowDecision(spaFallback bool) sohaapi.HostedAppGatewayAccessDecision {
	return sohaapi.HostedAppGatewayAccessDecision{
		Decision:         sohaapi.HostedAppAccessDecisionKindAllow,
		AppID:            "app_sales",
		DNSLabel:         "sales-dashboard-a1b2",
		Host:             testAppHost,
		AppStatus:        sohaapi.HostedAppStatusOnline,
		RouteGeneration:  3,
		SpaFallback:      spaFallback,
		ActiveRevisionID: "rev_01",
		StoragePrefix:    "hosted-apps",
		Files: []sohaapi.HostedAppManifestFile{
			{
				Path:        "index.html",
				SizeBytes:   38,
				Sha256:      "1111111111111111111111111111111111111111111111111111111111111111",
				ContentType: "text/html; charset=utf-8",
				Etag:        "\"etag-index-v1\"",
				ObjectKey:   "hosted-apps/app_sales/rev_01/index.html",
			},
			{
				Path:        "about/index.html",
				SizeBytes:   21,
				Sha256:      "2222222222222222222222222222222222222222222222222222222222222222",
				ContentType: "text/html; charset=utf-8",
				Etag:        "\"etag-about-v1\"",
				ObjectKey:   "app_sales/rev_01/about/index.html",
			},
			{
				Path:        "assets/app.js",
				SizeBytes:   27,
				Sha256:      "3333333333333333333333333333333333333333333333333333333333333333",
				ContentType: "text/javascript; charset=utf-8",
				Etag:        "",
				ObjectKey:   "hosted-apps/app_sales/rev_01/assets/app.js",
			},
			{
				Path:        "assets/style.css",
				SizeBytes:   22,
				Sha256:      "4444444444444444444444444444444444444444444444444444444444444444",
				ContentType: "text/css; charset=utf-8",
				Etag:        "\"etag-css-v1\"",
				ObjectKey:   "hosted-apps/app_sales/rev_01/assets/style.css",
			},
		},
		User: &sohaapi.HostedAppVisitorIdentity{
			ID:          "ahv_sales_carol_01",
			DisplayName: "Carol Zhang",
		},
	}
}

func newTestServer(t *testing.T, core CoreClient, objects ObjectReader, mutate func(*Config), extraOpts ...Option) *Server {
	t.Helper()
	cfg := Config{
		AppsDomain:       testAppsDomain,
		PublicScheme:     "https",
		TrustedProxies:   []string{"10.42.0.0/16"},
		CoreURL:          "http://soha-core.internal:8080",
		PublicCoreURL:    "https://soha.example.com",
		Token:            testGatewayToken,
		CookieSecret:     testCookieSecret,
		S3Prefix:         "hosted-apps",
		DecisionCacheTTL: 0,
	}
	if mutate != nil {
		mutate(&cfg)
	}
	opts := []Option{WithCoreClient(core), WithObjectReader(objects)}
	opts = append(opts, extraOpts...)
	srv, err := NewServer(cfg, opts...)
	require.NoError(t, err)
	return srv
}

func TestHostRejectionMatrix(t *testing.T) {
	t.Parallel()

	srv := newTestServer(t, &fakeCoreClient{}, newMemoryObjectReader(nil), nil)
	reserved := []string{
		"auth", "login", "logout", "admin", "api", "portal", "static", "assets",
		"sso", "console", "www", "mail", "status", "health", "_soha", "soha", "internal", "gateway",
	}
	invalidHosts := []string{
		"", "127.0.0.1", "127.0.0.1:8090", "[::1]:8090", "apps.example.com",
		"evil.com", "sales.apps.evil.com", "nested.sales.apps.example.com",
		"-leading.apps.example.com", "trailing-.apps.example.com", "sales.apps.example.com:99999",
	}
	for _, label := range reserved {
		invalidHosts = append(invalidHosts, label+"."+testAppsDomain)
	}

	for _, rawHost := range invalidHosts {
		t.Run("rejects_"+rawHost, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://placeholder/dashboard", nil)
			req.Host = rawHost
			req.TLS = &tls.ConnectionState{}
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			require.Equal(t, http.StatusNotFound, rec.Code)
			require.Equal(t, "private, no-store", rec.Header().Get("Cache-Control"))
		})
	}
}

func TestHTTPSRedirectAndTrustedProxies(t *testing.T) {
	t.Parallel()

	core := &fakeCoreClient{
		accessFn: func(_ context.Context, req sohaapi.HostedAppGatewayAccessCheckRequest, _ string) (sohaapi.HostedAppGatewayAccessDecision, error) {
			return sohaapi.HostedAppGatewayAccessDecision{
				Decision: sohaapi.HostedAppAccessDecisionKindNotFound,
				Host:     req.Host,
			}, nil
		},
	}
	srv := newTestServer(t, core, newMemoryObjectReader(nil), nil)

	untrustedReq := httptest.NewRequest(http.MethodGet, "http://Sales-Dashboard-A1B2.apps.example.com/reports/q3?tab=2", nil)
	untrustedReq.RemoteAddr = "203.0.113.99:54321"
	untrustedReq.Header.Set("X-Forwarded-Proto", "https")
	untrustedReq.Header.Set("X-Forwarded-For", "10.42.0.1")
	untrustedRec := httptest.NewRecorder()
	srv.ServeHTTP(untrustedRec, untrustedReq)
	require.Equal(t, http.StatusPermanentRedirect, untrustedRec.Code)
	require.Equal(t, "https://sales-dashboard-a1b2.apps.example.com/reports/q3?tab=2", untrustedRec.Header().Get("Location"))

	trustedReq := httptest.NewRequest(http.MethodGet, "http://sales-dashboard-a1b2.apps.example.com/", nil)
	trustedReq.RemoteAddr = "10.42.1.8:40112"
	trustedReq.Header.Set("X-Forwarded-Proto", "http, https")
	trustedRec := httptest.NewRecorder()
	srv.ServeHTTP(trustedRec, trustedReq)
	require.Equal(t, http.StatusNotFound, trustedRec.Code)

	spoofedLeftHopReq := httptest.NewRequest(http.MethodGet, "http://sales-dashboard-a1b2.apps.example.com/reports", nil)
	spoofedLeftHopReq.RemoteAddr = "10.42.1.8:40112"
	spoofedLeftHopReq.Header.Set("X-Forwarded-Proto", "https, http")
	spoofedLeftHopRec := httptest.NewRecorder()
	srv.ServeHTTP(spoofedLeftHopRec, spoofedLeftHopReq)
	require.Equal(t, http.StatusPermanentRedirect, spoofedLeftHopRec.Code)
	require.Equal(t, "https://sales-dashboard-a1b2.apps.example.com/reports", spoofedLeftHopRec.Header().Get("Location"))

	validRedirReq := httptest.NewRequest(http.MethodGet, "http://sales-dashboard-a1b2.apps.example.com:8080/deep?a=1", nil)
	validRedirRec := httptest.NewRecorder()
	srv.RedirectHandler().ServeHTTP(validRedirRec, validRedirReq)
	require.Equal(t, http.StatusPermanentRedirect, validRedirRec.Code)
	require.Equal(t, "https://sales-dashboard-a1b2.apps.example.com/deep?a=1", validRedirRec.Header().Get("Location"))

	badRedirReq := httptest.NewRequest(http.MethodGet, "http://admin.apps.example.com:8080/", nil)
	badRedirRec := httptest.NewRecorder()
	srv.RedirectHandler().ServeHTTP(badRedirRec, badRedirReq)
	require.Equal(t, http.StatusNotFound, badRedirRec.Code)
}

func TestSSOCallbackFlowAndHostOnlyCookie(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 20, 16, 0, 0, 0, time.UTC)
	core := &fakeCoreClient{
		accessFn: func(_ context.Context, req sohaapi.HostedAppGatewayAccessCheckRequest, _ string) (sohaapi.HostedAppGatewayAccessDecision, error) {
			if req.SessionID == "" {
				return sohaapi.HostedAppGatewayAccessDecision{
					Decision: sohaapi.HostedAppAccessDecisionKindUnauthenticated,
					Host:     req.Host,
				}, nil
			}
			return sampleAllowDecision(true), nil
		},
	}
	srv := newTestServer(t, core, defaultStaticObjects(), nil, WithClock(func() time.Time { return now }))

	// Subresource .js without session returns 401 instead of HTML login redirect.
	jsUnauthRec := httptest.NewRecorder()
	srv.ServeHTTP(jsUnauthRec, newTLSRequest(http.MethodGet, "https://"+testAppHost+"/assets/app.js", nil))
	require.Equal(t, http.StatusUnauthorized, jsUnauthRec.Code)

	// Browser navigation redirects 302 to /api/v1/app-hosting/auth/start with host-only state cookie.
	deepLink := "/projects/q4/review?dept=finance&view=summary"
	navReq := newTLSRequest(http.MethodGet, "https://"+testAppHost+deepLink, nil)
	navReq.Header.Set("Accept", "text/html,application/xhtml+xml")
	navRec := httptest.NewRecorder()
	srv.ServeHTTP(navRec, navReq)
	require.Equal(t, http.StatusFound, navRec.Code)

	authStartLoc, err := url.Parse(navRec.Header().Get("Location"))
	require.NoError(t, err)
	require.Equal(t, "/api/v1/app-hosting/auth/start", authStartLoc.Path)
	require.Equal(t, testAppHost, authStartLoc.Query().Get("host"))
	require.Equal(t, deepLink, authStartLoc.Query().Get("returnPath"))
	gatewayState := authStartLoc.Query().Get("gatewayState")
	oauthCookie := findCookie(navRec.Result().Cookies(), SecureOAuthStateCookieName)
	require.NotNil(t, oauthCookie)
	require.Empty(t, oauthCookie.Domain)

	// Cross-host callback replay fails with 403.
	crossReq := newTLSRequest(http.MethodGet, "https://other-app-1234.apps.example.com/_soha/auth/callback?ticket=tkt_1&state="+url.QueryEscape(gatewayState), nil)
	crossReq.AddCookie(oauthCookie)
	crossRec := httptest.NewRecorder()
	srv.ServeHTTP(crossRec, crossReq)
	require.Equal(t, http.StatusForbidden, crossRec.Code)

	// Valid one-time ticket exchange sets host-only __Host-soha_app_session and redirects 302 to original deep link.
	used := map[string]bool{}
	core.exchangeFn = func(_ context.Context, req sohaapi.HostedAppTicketExchangeRequest, token string) (sohaapi.HostedAppTicketExchangeResult, error) {
		require.Equal(t, testGatewayToken, token)
		require.Equal(t, testAppHost, req.Host)
		require.Equal(t, gatewayState, req.GatewayState)
		if used[req.Ticket] {
			return sohaapi.HostedAppTicketExchangeResult{}, errors.New("POST /app-hosting/gateway/tickets/exchange failed: 403 Forbidden: FORBIDDEN: replayed")
		}
		used[req.Ticket] = true
		return sohaapi.HostedAppTicketExchangeResult{
			SessionID:       "sess_carol_01",
			PrincipalUserID: "usr_carol",
			AppID:           "app_sales",
			DNSLabel:        "sales-dashboard-a1b2",
			Host:            "Sales-Dashboard-A1B2.apps.example.com",
			RouteGeneration: 3,
			ReturnPath:      deepLink,
			ExpiresAt:       now.Add(8 * time.Hour),
			User:            *sampleAllowDecision(true).User,
		}, nil
	}

	cbReq := newTLSRequest(http.MethodGet, "https://"+testAppHost+"/_soha/auth/callback?ticket=tkt_valid_1&state="+url.QueryEscape(gatewayState), nil)
	cbReq.AddCookie(oauthCookie)
	cbRec := httptest.NewRecorder()
	srv.ServeHTTP(cbRec, cbReq)
	require.Equal(t, http.StatusFound, cbRec.Code)
	require.Equal(t, deepLink, cbRec.Header().Get("Location"))

	sessionCookie := findCookie(cbRec.Result().Cookies(), SecureSessionCookieName)
	require.NotNil(t, sessionCookie)
	require.Empty(t, sessionCookie.Domain)
	require.True(t, sessionCookie.HttpOnly)
	require.True(t, sessionCookie.Secure)
	require.Equal(t, http.SameSiteLaxMode, sessionCookie.SameSite)

	replayReq := newTLSRequest(http.MethodGet, "https://"+testAppHost+"/_soha/auth/callback?ticket=tkt_valid_1&state="+url.QueryEscape(gatewayState), nil)
	replayReq.AddCookie(oauthCookie)
	replayRec := httptest.NewRecorder()
	srv.ServeHTTP(replayRec, replayReq)
	require.Equal(t, http.StatusForbidden, replayRec.Code)
}

func TestSystemMeAndLogoutRoutes(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 20, 16, 30, 0, 0, time.UTC)
	core := &fakeCoreClient{
		accessFn: func(_ context.Context, req sohaapi.HostedAppGatewayAccessCheckRequest, _ string) (sohaapi.HostedAppGatewayAccessDecision, error) {
			if req.SessionID == "" {
				return sohaapi.HostedAppGatewayAccessDecision{
					Decision: sohaapi.HostedAppAccessDecisionKindUnauthenticated,
					Host:     req.Host,
				}, nil
			}
			return sampleAllowDecision(true), nil
		},
	}
	srv := newTestServer(t, core, defaultStaticObjects(), func(cfg *Config) {
		cfg.DecisionCacheTTL = 2 * time.Second
	}, WithClock(func() time.Time { return now }))

	// Unauthenticated /_soha/me returns 401 JSON (never HTML redirect).
	meUnauthRec := httptest.NewRecorder()
	srv.ServeHTTP(meUnauthRec, newTLSRequest(http.MethodGet, "https://"+testAppHost+"/_soha/me", nil))
	require.Equal(t, http.StatusUnauthorized, meUnauthRec.Code)
	require.Contains(t, meUnauthRec.Header().Get("Content-Type"), "application/json")
	require.Equal(t, "private, no-store", meUnauthRec.Header().Get("Cache-Control"))

	sessionCookie, err := sealSessionCookie([]byte(testCookieSecret), testAppHost, sessionClaims{
		SessionID: "sess_carol_01",
		UserID:    "usr_carol",
		Host:      testAppHost,
		ExpiresAt: now.Add(4 * time.Hour),
	}, true, now)
	require.NoError(t, err)

	meReq := newTLSRequest(http.MethodGet, "https://"+testAppHost+"/_soha/me", nil)
	meReq.AddCookie(sessionCookie)
	meRec := httptest.NewRecorder()
	srv.ServeHTTP(meRec, meReq)
	require.Equal(t, http.StatusOK, meRec.Code)
	require.Equal(t, "private, no-store", meRec.Header().Get("Cache-Control"))
	var visitor sohaapi.HostedAppVisitorIdentity
	require.NoError(t, json.Unmarshal(meRec.Body.Bytes(), &visitor))
	require.Equal(t, "ahv_sales_carol_01", visitor.ID)
	require.Equal(t, "Carol Zhang", visitor.DisplayName)
	require.NotContains(t, meRec.Body.String(), `"email"`)
	require.NotContains(t, meRec.Body.String(), `"username"`)
	require.NotContains(t, meRec.Body.String(), `"teams"`)

	// Sibling subdomain Origin on /_soha/me and /_soha/logout is rejected with 403.
	crossMe := newTLSRequest(http.MethodGet, "https://"+testAppHost+"/_soha/me", nil)
	crossMe.AddCookie(sessionCookie)
	crossMe.Header.Set("Origin", "https://sibling.apps.example.com")
	crossMeRec := httptest.NewRecorder()
	srv.ServeHTTP(crossMeRec, crossMe)
	require.Equal(t, http.StatusForbidden, crossMeRec.Code)

	badLogout := newTLSRequest(http.MethodPost, "https://"+testAppHost+"/_soha/logout", nil)
	badLogout.AddCookie(sessionCookie)
	badLogout.Header.Set("Origin", "https://sibling.apps.example.com")
	badLogoutRec := httptest.NewRecorder()
	srv.ServeHTTP(badLogoutRec, badLogout)
	require.Equal(t, http.StatusForbidden, badLogoutRec.Code)

	goodLogout := newTLSRequest(http.MethodPost, "https://"+testAppHost+"/_soha/logout", nil)
	goodLogout.AddCookie(sessionCookie)
	goodLogout.Header.Set("Origin", "https://"+testAppHost)
	goodLogoutRec := httptest.NewRecorder()
	srv.ServeHTTP(goodLogoutRec, goodLogout)
	require.Equal(t, http.StatusNoContent, goodLogoutRec.Code)
	cleared := findCookie(goodLogoutRec.Result().Cookies(), SecureSessionCookieName)
	require.NotNil(t, cleared)
	require.Negative(t, cleared.MaxAge)

	unknownRec := httptest.NewRecorder()
	srv.ServeHTTP(unknownRec, newTLSRequest(http.MethodGet, "https://"+testAppHost+"/_soha/unknown", nil))
	require.Equal(t, http.StatusNotFound, unknownRec.Code)
}

func TestStaticServingMIMEAndSPAFallback(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 20, 17, 0, 0, 0, time.UTC)
	var spaEnabled atomic.Bool
	spaEnabled.Store(true)
	core := &fakeCoreClient{
		accessFn: func(_ context.Context, _ sohaapi.HostedAppGatewayAccessCheckRequest, _ string) (sohaapi.HostedAppGatewayAccessDecision, error) {
			return sampleAllowDecision(spaEnabled.Load()), nil
		},
	}
	srv := newTestServer(t, core, defaultStaticObjects(), nil, WithClock(func() time.Time { return now }))
	sessionCookie := mustTestSessionCookie(t, testCookieSecret, testAppHost, true, now)

	cases := []struct {
		name            string
		method          string
		path            string
		wantStatus      int
		wantContentType string
		wantETag        string
		wantBodySubstr  string
	}{
		{"root index", http.MethodGet, "/", http.StatusOK, "text/html; charset=utf-8", "\"etag-index-v1\"", "Sales Dashboard"},
		{"directory index", http.MethodGet, "/about/", http.StatusOK, "text/html; charset=utf-8", "\"etag-about-v1\"", "About Sales"},
		{"JS bundle sha256 etag", http.MethodGet, "/assets/app.js", http.StatusOK, "text/javascript; charset=utf-8", "\"sha256:3333333333333333333333333333333333333333333333333333333333333333\"", "console.log('sales-app')"},
		{"CSS bundle", http.MethodGet, "/assets/style.css", http.StatusOK, "text/css; charset=utf-8", "\"etag-css-v1\"", "color:#0f172a"},
		{"HEAD CSS bundle", http.MethodHead, "/assets/style.css", http.StatusOK, "text/css; charset=utf-8", "\"etag-css-v1\"", ""},
		{"SPA deep link fallback", http.MethodGet, "/deals/enterprise/q4", http.StatusOK, "text/html; charset=utf-8", "\"etag-index-v1\"", "Sales Dashboard"},
		{"missing .js returns 404", http.MethodGet, "/assets/missing.js", http.StatusNotFound, "", "", ""},
		{"missing .map returns 404", http.MethodGet, "/assets/app.js.map", http.StatusNotFound, "", "", ""},
		{"hidden dot-segment returns 404", http.MethodGet, "/.git/HEAD", http.StatusNotFound, "", "", ""},
		{"POST on static returns 405", http.MethodPost, "/", http.StatusMethodNotAllowed, "", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := newTLSRequest(tc.method, "https://"+testAppHost+tc.path, nil)
			req.AddCookie(sessionCookie)
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			require.Equal(t, tc.wantStatus, rec.Code)
			if tc.wantStatus != http.StatusOK {
				return
			}
			require.Equal(t, tc.wantContentType, rec.Header().Get("Content-Type"))
			require.Equal(t, "private, no-cache", rec.Header().Get("Cache-Control"))
			require.Equal(t, tc.wantETag, rec.Header().Get("ETag"))
			require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
			require.Equal(t, "same-origin", rec.Header().Get("Referrer-Policy"))
			if tc.method == http.MethodHead {
				require.Empty(t, rec.Body.String())
				require.Equal(t, "22", rec.Header().Get("Content-Length"))
			} else {
				require.Contains(t, rec.Body.String(), tc.wantBodySubstr)
			}
		})
	}

	spaEnabled.Store(false)
	noSpaReq := newTLSRequest(http.MethodGet, "https://"+testAppHost+"/deals/enterprise/q4", nil)
	noSpaReq.AddCookie(sessionCookie)
	noSpaRec := httptest.NewRecorder()
	srv.ServeHTTP(noSpaRec, noSpaReq)
	require.Equal(t, http.StatusNotFound, noSpaRec.Code)
}

func TestConditionalETagInstantRevocationOfflineAndHeaderStripping(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 11, 20, 17, 30, 0, 0, time.UTC)
	var currentDecision atomic.Pointer[sohaapi.HostedAppGatewayAccessDecision]
	initialAllow := sampleAllowDecision(true)
	currentDecision.Store(&initialAllow)

	core := &fakeCoreClient{
		accessFn: func(_ context.Context, req sohaapi.HostedAppGatewayAccessCheckRequest, _ string) (sohaapi.HostedAppGatewayAccessDecision, error) {
			if req.SessionID == "" {
				return sohaapi.HostedAppGatewayAccessDecision{
					Decision: sohaapi.HostedAppAccessDecisionKindUnauthenticated,
					Host:     req.Host,
				}, nil
			}
			return *currentDecision.Load(), nil
		},
	}
	objects := defaultStaticObjects()
	srv := newTestServer(t, core, objects, nil, WithClock(func() time.Time { return now }))
	sessionCookie := mustTestSessionCookie(t, testCookieSecret, testAppHost, true, now)

	// Forged X-Soha-User-Id / Authorization / platform Cookie headers cannot spoof identity.
	forgedReq := newTLSRequest(http.MethodGet, "https://"+testAppHost+"/", nil)
	forgedReq.Header.Set("X-Soha-User-Id", "usr_admin")
	forgedReq.Header.Set("X-Soha-Session-Id", "sess_admin")
	forgedReq.Header.Set("Authorization", "Bearer forged-jwt")
	forgedReq.Header.Set("Cookie", "soha_access_token=forged-console-cookie")
	forgedRec := httptest.NewRecorder()
	srv.ServeHTTP(forgedRec, forgedReq)
	require.Equal(t, http.StatusFound, forgedRec.Code)
	require.Empty(t, core.lastCheckReq.UserID)

	cleaned := SanitizeUpstreamHeaders(http.Header{
		"Accept":        {"text/html"},
		"Cookie":        {"_soha_app_session=secret"},
		"Authorization": {"Bearer secret"},
		"X-Soha-User":   {"forged"},
	})
	require.Equal(t, "text/html", cleaned.Get("Accept"))
	require.Empty(t, cleaned.Get("Cookie"))
	require.Empty(t, cleaned.Get("Authorization"))
	require.Empty(t, cleaned.Get("X-Soha-User"))

	// Authorized matching If-None-Match returns 304 without calling S3 GetObject.
	s3GetsBefore := objects.gets.Load()
	condAllowReq := newTLSRequest(http.MethodGet, "https://"+testAppHost+"/", nil)
	condAllowReq.AddCookie(sessionCookie)
	condAllowReq.Header.Set("If-None-Match", "\"etag-index-v1\"")
	condAllowRec := httptest.NewRecorder()
	srv.ServeHTTP(condAllowRec, condAllowReq)
	require.Equal(t, http.StatusNotModified, condAllowRec.Code)
	require.Equal(t, s3GetsBefore, objects.gets.Load())

	// CRITICAL: Revoked audience returns 403 (NEVER 304) even with valid session cookie AND matching If-None-Match!
	forbiddenDecision := sohaapi.HostedAppGatewayAccessDecision{
		Decision:        sohaapi.HostedAppAccessDecisionKindForbidden,
		AppID:           "app_sales",
		DNSLabel:        "sales-dashboard-a1b2",
		Host:            testAppHost,
		AppStatus:       sohaapi.HostedAppStatusOnline,
		RouteGeneration: 4,
		Reason:          "audience_revoked",
	}
	currentDecision.Store(&forbiddenDecision)
	revokedReq := newTLSRequest(http.MethodGet, "https://"+testAppHost+"/", nil)
	revokedReq.AddCookie(sessionCookie)
	revokedReq.Header.Set("If-None-Match", "\"etag-index-v1\"")
	revokedRec := httptest.NewRecorder()
	srv.ServeHTTP(revokedRec, revokedReq)
	require.Equal(t, http.StatusForbidden, revokedRec.Code)
	require.Equal(t, "private, no-store", revokedRec.Header().Get("Cache-Control"))
	require.Empty(t, revokedRec.Header().Get("ETag"))
	require.Contains(t, revokedRec.Body.String(), "无权访问该应用")
	require.Equal(t, s3GetsBefore, objects.gets.Load())

	// Offline app returns 503 ("应用已下线或暂停访问") without leaking content.
	offlineDecision := sohaapi.HostedAppGatewayAccessDecision{
		Decision:        sohaapi.HostedAppAccessDecisionKindOffline,
		AppID:           "app_sales",
		DNSLabel:        "sales-dashboard-a1b2",
		Host:            testAppHost,
		AppStatus:       sohaapi.HostedAppStatusOffline,
		RouteGeneration: 5,
	}
	currentDecision.Store(&offlineDecision)
	offlineReq := newTLSRequest(http.MethodGet, "https://"+testAppHost+"/", nil)
	offlineReq.AddCookie(sessionCookie)
	offlineReq.Header.Set("If-None-Match", "\"etag-index-v1\"")
	offlineRec := httptest.NewRecorder()
	srv.ServeHTTP(offlineRec, offlineReq)
	require.Equal(t, http.StatusServiceUnavailable, offlineRec.Code)
	require.Contains(t, offlineRec.Body.String(), "应用已下线或暂停访问")
}

func TestResolveObjectKeyNoDoublePrefixingAndTraversalRejection(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		gatewayPrefix  string
		decisionPrefix string
		objectKey      string
		want           string
		wantErr        bool
	}{
		{"all three share hosted-apps prefix", "hosted-apps", "hosted-apps", "hosted-apps/app_1/rev_1/index.html", "hosted-apps/app_1/rev_1/index.html", false},
		{"objectKey relative to hosted-apps", "hosted-apps", "hosted-apps", "app_1/rev_1/index.html", "hosted-apps/app_1/rev_1/index.html", false},
		{"objectKey relative to revision prefix", "hosted-apps", "hosted-apps/app_1/rev_1", "assets/main.js", "hosted-apps/app_1/rev_1/assets/main.js", false},
		{"integration root prefix prepended once", "tenant-bucket-prefix", "hosted-apps", "hosted-apps/app_1/rev_1/index.html", "tenant-bucket-prefix/hosted-apps/app_1/rev_1/index.html", false},
		{"rejects dot-dot traversal", "hosted-apps", "hosted-apps", "app_1/../other_app/secret.txt", "", true},
		{"rejects backslash", "hosted-apps", "", "app_1\\rev_1\\index.html", "", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := resolveObjectKey(tc.gatewayPrefix, tc.decisionPrefix, tc.objectKey)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestLiveServerHotSecretReloadAndS3Streaming(t *testing.T) {
	t.Parallel()

	secretDir := t.TempDir()
	tokenFile := filepath.Join(secretDir, "gateway-token")
	cookieFile := filepath.Join(secretDir, "cookie-secret")
	akFile := filepath.Join(secretDir, "access-key-id")
	skFile := filepath.Join(secretDir, "secret-access-key")

	cookieV1Secret := "cookie-secret-v1-0123456789abcdef-long"
	cookieV2Secret := "cookie-secret-v2-fedcba9876543210-long"
	writeSecretFiles(t, tokenFile, cookieFile, akFile, skFile, "gw-live-token-v1\n", cookieV1Secret+"\n", "MINIOAKV1\n", "MINIOSKV1SECRET\n")

	var expectedCoreToken atomic.Pointer[string]
	tokV1 := "gw-live-token-v1"
	expectedCoreToken.Store(&tokV1)
	coreHTTP := newLiveCoreStub(t, &expectedCoreToken)
	var lastS3Auth atomic.Pointer[string]
	s3HTTP := newLiveS3Stub(t, &lastS3Auth)

	listenAddr := freeTCPAddr(t)
	redirectAddr := freeTCPAddr(t)
	healthAddr := freeTCPAddr(t)
	srv, err := NewServer(Config{
		ListenAddr:       listenAddr,
		HTTPRedirectAddr: redirectAddr,
		HealthAddr:       healthAddr,
		AppsDomain:       testAppsDomain,
		PublicScheme:     "http",
		CoreURL:          coreHTTP.URL,
		PublicCoreURL:    coreHTTP.URL,
		TokenFile:        tokenFile,
		CookieSecretFile: cookieFile,
		S3Endpoint:       s3HTTP.URL,
		S3Bucket:         "soha-hosted-apps",
		S3Prefix:         "hosted-apps",
		S3PathStyle:      true,
		S3AccessKeyFile:  akFile,
		S3SecretKeyFile:  skFile,
		DecisionCacheTTL: 0,
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runDone := make(chan error, 1)
	go func() { runDone <- srv.Run(ctx) }()

	waitForHTTP200(t, "http://"+healthAddr+"/healthz")
	waitForHTTP200(t, "http://"+healthAddr+"/readyz")

	now := time.Now().UTC()
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}

	cookieV1 := mustTestSessionCookie(t, cookieV1Secret, testAppHost, false, now)
	status1, body1, _ := doHostGet(t, client, "http://"+listenAddr+"/", testAppHost, cookieV1)
	require.Equal(t, http.StatusOK, status1)
	require.Contains(t, body1, "Live S3")
	require.Contains(t, *lastS3Auth.Load(), "Credential=MINIOAKV1/")

	// Rotate all four secret files in place without restarting.
	tokV2 := "gw-live-token-v2"
	expectedCoreToken.Store(&tokV2)
	writeSecretFiles(t, tokenFile, cookieFile, akFile, skFile, "gw-live-token-v2\n", cookieV2Secret+"\n", "MINIOAKV2\n", "MINIOSKV2SECRET\n")

	statusOld, _, cookiesOld := doHostGet(t, client, "http://"+listenAddr+"/", testAppHost, cookieV1)
	require.Equal(t, http.StatusFound, statusOld)
	cleared := findCookie(cookiesOld, SessionCookieName)
	require.NotNil(t, cleared)
	require.Negative(t, cleared.MaxAge)

	cookieV2 := mustTestSessionCookie(t, cookieV2Secret, testAppHost, false, now)
	status2, body2, _ := doHostGet(t, client, "http://"+listenAddr+"/", testAppHost, cookieV2)
	require.Equal(t, http.StatusOK, status2)
	require.Contains(t, body2, "Live S3")
	require.Contains(t, *lastS3Auth.Load(), "Credential=MINIOAKV2/")

	statusRedir, _, _ := doHostGet(t, client, "http://"+redirectAddr+"/welcome?x=1", testAppHost, nil)
	require.Equal(t, http.StatusPermanentRedirect, statusRedir)

	require.NoError(t, os.Remove(tokenFile))
	readyAfterRemove, _, _ := doHostGet(t, client, "http://"+healthAddr+"/readyz", "localhost", nil)
	require.Equal(t, http.StatusServiceUnavailable, readyAfterRemove)

	cancel()
	require.NoError(t, <-runDone)
}

func newLiveCoreStub(t *testing.T, expectedToken *atomic.Pointer[string]) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/readyz":
			_, _ = w.Write([]byte(`{"status":"ready"}`))
		case "/api/v1/app-hosting/gateway/access-check":
			if r.Header.Get("Authorization") != "Bearer "+*expectedToken.Load() {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"code":"UNAUTHORIZED","message":"bad gateway token"}}`))
				return
			}
			var req sohaapi.HostedAppGatewayAccessCheckRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.SessionID == "" {
				_ = json.NewEncoder(w).Encode(sohaapi.HostedAppGatewayAccessCheckResponse{
					Data: sohaapi.HostedAppGatewayAccessDecision{
						Decision: sohaapi.HostedAppAccessDecisionKindUnauthenticated,
						Host:     req.Host,
					},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(sohaapi.HostedAppGatewayAccessCheckResponse{
				Data: sampleAllowDecision(true),
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newLiveS3Stub(t *testing.T, lastAuth *atomic.Pointer[string]) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		lastAuth.Store(&auth)
		if r.Method == http.MethodGet && r.URL.Path == "/soha-hosted-apps/hosted-apps/app_sales/rev_01/index.html" {
			body := "<!doctype html><title>Live S3</title>"
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
			w.Header().Set("ETag", "\"etag-index-v1\"")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(body))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func doHostGet(t *testing.T, client *http.Client, targetURL, host string, cookie *http.Cookie) (int, string, []*http.Cookie) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, targetURL, nil)
	require.NoError(t, err)
	req.Host = host
	if cookie != nil {
		req.AddCookie(cookie)
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, resp.Body.Close())
	require.NoError(t, err)
	return resp.StatusCode, string(raw), resp.Cookies()
}

func mustTestSessionCookie(t *testing.T, secret, host string, secure bool, now time.Time) *http.Cookie {
	t.Helper()
	cookie, err := sealSessionCookie([]byte(secret), host, sessionClaims{
		SessionID: "sess_carol_01",
		UserID:    "usr_carol",
		Host:      host,
		ExpiresAt: now.Add(4 * time.Hour),
	}, secure, now)
	require.NoError(t, err)
	return cookie
}

func newTLSRequest(method, target string, body io.Reader) *http.Request {
	req := httptest.NewRequest(method, target, body)
	if strings.HasPrefix(target, "https://") {
		req.TLS = &tls.ConnectionState{}
	}
	return req
}

func findCookie(cookies []*http.Cookie, name string) *http.Cookie {
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func freeTCPAddr(t *testing.T) string {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := ln.Addr().String()
	require.NoError(t, ln.Close())
	return addr
}

func waitForHTTP200(t *testing.T, targetURL string) {
	t.Helper()
	client := &http.Client{Timeout: 500 * time.Millisecond}
	require.Eventually(t, func() bool {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, targetURL, nil)
		if err != nil {
			return false
		}
		resp, err := client.Do(req)
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 5*time.Second, 25*time.Millisecond)
}
