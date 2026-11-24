package appgateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/stretchr/testify/require"
)

const (
	testRuntimeRevision = "rev_0123456789abcdef0123456789abcdef"
	testRuntimeService  = "soha-app-0123456789abcdef0123456789abcdef"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func runtimeDecision() sohaapi.HostedAppGatewayAccessDecision {
	decision := sampleAllowDecision(false)
	decision.ActiveRevisionID = testRuntimeRevision
	decision.RuntimeService = testRuntimeService
	decision.Files = nil
	return decision
}

func newDynamicTestServer(t *testing.T, core *fakeCoreClient, transport http.RoundTripper, mutate func(*Config)) *Server {
	t.Helper()
	srv := newTestServer(t, core, newMemoryObjectReader(nil), func(cfg *Config) {
		cfg.RuntimeNamespace = "app-runtime"
		cfg.RuntimeClusterDomain = "cluster.local"
		if mutate != nil {
			mutate(cfg)
		}
	})
	if transport != nil {
		srv.runtimeTransport = transport
	}
	return srv
}

func TestDynamicProxyTargetHeadersCookiesAndResponseFiltering(t *testing.T) {
	t.Parallel()

	now := time.Now()
	decision := runtimeDecision()
	core := &fakeCoreClient{accessFn: func(_ context.Context, req sohaapi.HostedAppGatewayAccessCheckRequest, _ string) (sohaapi.HostedAppGatewayAccessDecision, error) {
		if req.SessionID == "" {
			return sohaapi.HostedAppGatewayAccessDecision{Decision: sohaapi.HostedAppAccessDecisionKindUnauthenticated, Host: req.Host}, nil
		}
		return decision, nil
	}}
	var captured *http.Request
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		captured = r.Clone(r.Context())
		header := make(http.Header)
		header.Set("Content-Type", "text/plain")
		header.Set("Location", "http://"+r.Host+"/api/items/?q=1")
		header.Add("Set-Cookie", "_soha_app_session=leak; Path=/; Domain=.apps.example.com")
		header.Add("Set-Cookie", "app_session=ok; Path=/; Domain=.apps.example.com; HttpOnly")
		return &http.Response{
			StatusCode:    http.StatusTemporaryRedirect,
			Header:        header,
			Body:          io.NopCloser(strings.NewReader("upstream-ok")),
			ContentLength: -1,
			Request:       r,
		}, nil
	})
	srv := newDynamicTestServer(t, core, transport, nil)

	req := newTLSRequest(http.MethodPost, "https://"+testAppHost+"/api/items?q=1", strings.NewReader("payload"))
	req.Header.Set("Origin", "https://"+testAppHost)
	req.Header.Set("X-Soha-User-ID", "forged")
	req.Header.Set("Authorization", "Bearer framework-secret")
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	req.Header.Set("Connection", "X-Drop-Me")
	req.Header.Set("X-Drop-Me", "hop-by-hop")
	req.Header.Set("Proxy-Connection", "keep-alive")
	req.AddCookie(mustTestSessionCookie(t, testCookieSecret, testAppHost, true, now))
	req.Header.Add("Cookie", "theme=dark; soha_refresh_token=fixture-only")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	require.Equal(t, http.StatusTemporaryRedirect, rec.Code)
	require.Equal(t, "https://"+testAppHost+"/api/items/?q=1", rec.Header().Get("Location"))
	require.Equal(t, "upstream-ok", rec.Body.String())
	require.Equal(t, "private, no-store", rec.Header().Get("Cache-Control"))
	require.NotNil(t, captured)
	require.Equal(t, "http", captured.URL.Scheme)
	require.Equal(t, testRuntimeService+".app-runtime.svc.cluster.local", captured.URL.Host)
	require.Equal(t, captured.URL.Host, captured.Host)
	require.Equal(t, "/api/items", captured.URL.Path)
	require.Equal(t, "q=1", captured.URL.RawQuery)
	require.Equal(t, decision.User.ID, captured.Header.Get("X-Soha-User-ID"))
	require.Equal(t, decision.User.DisplayName, captured.Header.Get("X-Soha-User"))
	require.Empty(t, captured.Header.Get("Authorization"))
	require.Empty(t, captured.Header.Get("X-Forwarded-For"))
	require.Empty(t, captured.Header.Values("Connection"))
	require.Empty(t, captured.Header.Get("Proxy-Connection"))
	require.Empty(t, captured.Header.Get("X-Drop-Me"))
	cookie := captured.Header.Get("Cookie")
	require.Contains(t, cookie, "theme=dark")
	require.NotContains(t, cookie, SessionCookieName)
	require.NotContains(t, cookie, "soha_refresh_token")

	cookies := rec.Result().Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, "app_session", cookies[0].Name)
	require.Empty(t, cookies[0].Domain)
	require.NotContains(t, rec.Header().Values("Set-Cookie"), "_soha_app_session")
}

func TestDynamicOriginRequiredForUnsafeAndWebSocketRequests(t *testing.T) {
	t.Parallel()

	now := time.Now()
	decision := runtimeDecision()
	var calls atomic.Int64
	core := &fakeCoreClient{accessFn: func(_ context.Context, req sohaapi.HostedAppGatewayAccessCheckRequest, _ string) (sohaapi.HostedAppGatewayAccessDecision, error) {
		return decision, nil
	}}
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls.Add(1)
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), ContentLength: -1, Request: r}, nil
	})
	srv := newDynamicTestServer(t, core, transport, nil)
	session := mustTestSessionCookie(t, testCookieSecret, testAppHost, true, now)

	for _, tc := range []struct {
		name    string
		method  string
		origin  string
		upgrade bool
		want    int
	}{
		{name: "unsafe missing origin", method: http.MethodPost, want: http.StatusForbidden},
		{name: "unsafe cross subdomain", method: http.MethodPost, origin: "https://other.apps.example.com", want: http.StatusForbidden},
		{name: "unsafe exact origin", method: http.MethodPost, origin: "https://" + testAppHost, want: http.StatusOK},
		{name: "websocket cross subdomain", method: http.MethodGet, origin: "https://other.apps.example.com", upgrade: true, want: http.StatusForbidden},
		{name: "websocket exact origin", method: http.MethodGet, origin: "https://" + testAppHost, upgrade: true, want: http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := calls.Load()
			req := newTLSRequest(tc.method, "https://"+testAppHost+"/socket", nil)
			req.AddCookie(session)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.upgrade {
				req.Header.Set("Connection", "Upgrade")
				req.Header.Set("Upgrade", "websocket")
			}
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			require.Equal(t, tc.want, rec.Code)
			if tc.want == http.StatusForbidden {
				require.Equal(t, before, calls.Load())
			}
		})
	}
}

func TestDynamicTrustedVisitorHeaders(t *testing.T) {
	t.Parallel()

	now := time.Now()
	overlongEmail := strings.Repeat("e", visitorEmailHeaderCap+1)
	overlongDept := strings.Repeat("d", visitorDeptHeaderCap+1)
	cases := []struct {
		name           string
		email          string
		department     string
		wantEmail      string
		wantDepartment string
	}{
		{name: "valid values", email: "carol@example.com", department: "Sales", wantEmail: "carol@example.com", wantDepartment: "Sales"},
		{name: "empty values", wantEmail: "", wantDepartment: ""},
		{name: "crlf rejected", email: "carol@example.com\r\nX-Soha-Admin: 1", department: "Sales", wantEmail: "", wantDepartment: "Sales"},
		{name: "control rejected", email: "carol@example.com", department: "Sales\x00", wantEmail: "carol@example.com", wantDepartment: ""},
		{name: "invalid utf8 rejected", email: string([]byte{0xff, 0xfe}), department: "Sales", wantEmail: "", wantDepartment: "Sales"},
		{name: "overlong rejected", email: overlongEmail, department: overlongDept, wantEmail: "", wantDepartment: ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision := runtimeDecision()
			decision.User.Email = tc.email
			decision.User.Department = tc.department
			core := &fakeCoreClient{accessFn: func(_ context.Context, _ sohaapi.HostedAppGatewayAccessCheckRequest, _ string) (sohaapi.HostedAppGatewayAccessDecision, error) {
				return decision, nil
			}}
			var captured *http.Request
			transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				captured = r.Clone(r.Context())
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("ok")), Request: r}, nil
			})
			srv := newDynamicTestServer(t, core, transport, nil)
			req := newTLSRequest(http.MethodGet, "https://"+testAppHost+"/", nil)
			req.Header.Set("X-Soha-User-Email", "forged@example.com")
			req.Header.Set("X-Soha-User-Department", "Forged")
			req.AddCookie(mustTestSessionCookie(t, testCookieSecret, testAppHost, true, now))
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)

			require.Equal(t, http.StatusOK, rec.Code)
			require.NotNil(t, captured)
			require.Equal(t, decision.User.ID, captured.Header.Get("X-Soha-User-ID"))
			require.Equal(t, decision.User.DisplayName, captured.Header.Get("X-Soha-User"))
			require.Equal(t, tc.wantEmail, captured.Header.Get("X-Soha-User-Email"))
			require.Equal(t, tc.wantDepartment, captured.Header.Get("X-Soha-User-Department"))
		})
	}
}

func TestDynamicRuntimeTargetFailsClosed(t *testing.T) {
	t.Parallel()

	now := time.Now()
	cases := []struct {
		name   string
		mutate func(*sohaapi.HostedAppGatewayAccessDecision)
		config func(*Config)
	}{
		{name: "service mismatch", mutate: func(d *sohaapi.HostedAppGatewayAccessDecision) {
			d.RuntimeService = "soha-app-ffffffffffffffffffffffffffffffff"
		}},
		{name: "invalid revision", mutate: func(d *sohaapi.HostedAppGatewayAccessDecision) {
			d.ActiveRevisionID = "rev_short"
		}},
		{name: "missing namespace", config: func(c *Config) { c.RuntimeNamespace = ""; c.RuntimeClusterDomain = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			decision := runtimeDecision()
			if tc.mutate != nil {
				tc.mutate(&decision)
			}
			core := &fakeCoreClient{accessFn: func(_ context.Context, _ sohaapi.HostedAppGatewayAccessCheckRequest, _ string) (sohaapi.HostedAppGatewayAccessDecision, error) {
				return decision, nil
			}}
			var calls atomic.Int64
			transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				calls.Add(1)
				return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok")), Request: r}, nil
			})
			srv := newDynamicTestServer(t, core, transport, tc.config)
			req := newTLSRequest(http.MethodGet, "https://"+testAppHost+"/", nil)
			req.AddCookie(mustTestSessionCookie(t, testCookieSecret, testAppHost, true, now))
			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)
			require.Equal(t, http.StatusServiceUnavailable, rec.Code)
			require.Zero(t, calls.Load())
			require.NotContains(t, rec.Body.String(), testRuntimeService)
		})
	}
}

type cancelAwareBody struct {
	ctx context.Context
}

func (b *cancelAwareBody) Read([]byte) (int, error) {
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *cancelAwareBody) Close() error { return nil }

func TestDynamicStreamingRevocationCancelsUpstream(t *testing.T) {
	t.Parallel()

	now := time.Now()
	decision := runtimeDecision()
	var calls atomic.Int64
	core := &fakeCoreClient{accessFn: func(_ context.Context, req sohaapi.HostedAppGatewayAccessCheckRequest, _ string) (sohaapi.HostedAppGatewayAccessDecision, error) {
		if calls.Add(1) == 1 {
			return decision, nil
		}
		return sohaapi.HostedAppGatewayAccessDecision{Decision: sohaapi.HostedAppAccessDecisionKindForbidden, Host: req.Host}, nil
	}}
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		header := make(http.Header)
		header.Set("Content-Type", "text/event-stream")
		return &http.Response{StatusCode: http.StatusOK, Header: header, Body: &cancelAwareBody{ctx: r.Context()}, ContentLength: -1, Request: r}, nil
	})
	srv := newDynamicTestServer(t, core, transport, nil)
	req := newTLSRequest(http.MethodGet, "https://"+testAppHost+"/events", nil)
	req.AddCookie(mustTestSessionCookie(t, testCookieSecret, testAppHost, true, now))
	rec := httptest.NewRecorder()
	done := make(chan struct{})
	start := time.Now()
	go func() {
		defer close(done)
		srv.ServeHTTP(rec, req)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("streaming request was not canceled after access revocation")
	}
	require.Less(t, time.Since(start), 5*time.Second)
	require.GreaterOrEqual(t, calls.Load(), int64(2))
}

func TestDynamicStreamingVisitorHeaderChangeCancelsUpstream(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		mutate func(*sohaapi.HostedAppVisitorIdentity)
	}{
		{name: "email changed", mutate: func(u *sohaapi.HostedAppVisitorIdentity) { u.Email = "eve@example.com" }},
		{name: "department lost", mutate: func(u *sohaapi.HostedAppVisitorIdentity) { u.Department = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			now := time.Now()
			decision := runtimeDecision()
			decision.User.Email = "carol@example.com"
			decision.User.Department = "Sales"
			next := decision
			nextUser := *decision.User
			tc.mutate(&nextUser)
			next.User = &nextUser
			var calls atomic.Int64
			core := &fakeCoreClient{accessFn: func(_ context.Context, _ sohaapi.HostedAppGatewayAccessCheckRequest, _ string) (sohaapi.HostedAppGatewayAccessDecision, error) {
				if calls.Add(1) == 1 {
					return decision, nil
				}
				return next, nil
			}}
			transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
				header := make(http.Header)
				header.Set("Content-Type", "text/event-stream")
				return &http.Response{StatusCode: http.StatusOK, Header: header, Body: &cancelAwareBody{ctx: r.Context()}, ContentLength: -1, Request: r}, nil
			})
			srv := newDynamicTestServer(t, core, transport, nil)
			req := newTLSRequest(http.MethodGet, "https://"+testAppHost+"/events", nil)
			req.AddCookie(mustTestSessionCookie(t, testCookieSecret, testAppHost, true, now))
			rec := httptest.NewRecorder()
			done := make(chan struct{})
			go func() {
				defer close(done)
				srv.ServeHTTP(rec, req)
			}()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("streaming request was not canceled after visitor header change")
			}
			require.GreaterOrEqual(t, calls.Load(), int64(2))
		})
	}
}

func TestRuntimeConfigDefaultsAndValidation(t *testing.T) {
	t.Parallel()

	env := baseTestEnv()
	env[EnvRuntimeNamespace] = "app-runtime"
	cfg, err := LoadConfigFromLookup(mapLookup(env))
	require.NoError(t, err)
	require.Equal(t, "app-runtime", cfg.RuntimeNamespace)
	require.Equal(t, "cluster.local", cfg.RuntimeClusterDomain)

	invalid := []struct {
		name   string
		mutate func(map[string]string)
	}{
		{"invalid namespace", func(m map[string]string) { m[EnvRuntimeNamespace] = "Bad_NS" }},
		{"invalid domain underscore", func(m map[string]string) { m[EnvRuntimeClusterDomain] = "cluster_local" }},
		{"invalid domain empty label", func(m map[string]string) { m[EnvRuntimeClusterDomain] = "cluster..local" }},
		{"nonzero cache ttl", func(m map[string]string) { m[EnvDecisionCacheTTL] = "2s" }},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			values := baseTestEnv()
			values[EnvRuntimeNamespace] = "app-runtime"
			tc.mutate(values)
			_, err := LoadConfigFromLookup(mapLookup(values))
			require.Error(t, err)
		})
	}
}

func TestRuntimeTransportUsesManagedCloneWithoutProxy(t *testing.T) {
	t.Parallel()

	srv := newDynamicTestServer(t, &fakeCoreClient{}, nil, nil)
	transport, ok := srv.runtimeTransport.(*http.Transport)
	require.True(t, ok)
	require.Nil(t, transport.Proxy)
}
