package middleware

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
)

type stubAccessTokenParser struct {
	calls int
	token string
}

func (p *stubAccessTokenParser) ParseAccessToken(_ context.Context, token string) (domainidentity.Principal, domainidentity.AccessContext, error) {
	p.calls++
	p.token = token
	if token != "valid-key" {
		return domainidentity.Principal{}, domainidentity.AccessContext{}, fmt.Errorf("invalid token")
	}
	return domainidentity.Principal{UserID: "user-1", UserName: "Ada"}, domainidentity.AccessContext{TokenKind: "personal_access_token"}, nil
}

func TestAllowsExternalBearerToken(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want bool
	}{
		{path: "/api/v1/agent-sessions/connect", want: true},
		{path: "/api/v1/network/proxy-deployment-executor/claim", want: true},
		{path: "/api/v1/network/proxy-deployment-executor/status", want: true},
		{path: "/api/v1/network/proxy-deployment-executor/callback", want: true},
		{path: "/custom/v2/network/proxy-deployment-executor/claim", want: true},
		{path: "/api/v1/network/proxy-deployment-executor", want: false},
		{path: "/api/v1/network/proxy-deployment-executor/claim/extra", want: false},
		{path: "/api/v1/network/proxy-deployment-executor/other", want: false},
		{path: "/api/v1/network-access/proxy-instances/proxy-1/deployment/control", want: false},
		{path: "/api/v1/identity/outpost-runtime-executor/claim", want: true},
		{path: "/api/v1/identity/outpost-runtime-executor/status", want: true},
		{path: "/api/v1/identity/outpost-runtime-executor/callback", want: true},
		{path: "/api/v1/identity/outposts/edge/runtime", want: false},
		{path: "/api/v1/identity/outposts/edge/runtime/control", want: false},
		{path: "/api/v1/identity/outposts/runtime/claim", want: true},
		{path: "/api/v1/identity/outposts/edge/heartbeat", want: true},
		{path: "/api/v1/identity/outposts/edge/check", want: true},
		{path: "/api/v1/identity/outposts/edge/events", want: true},
		{path: "/api/v1/identity/outposts/edge/token/rotate", want: false},
		{path: "/api/v1/identity/outposts/edge/check/extra", want: false},
		{path: "/api/v1/identity/outposts", want: false},
		{path: "/api/v1/integrations/alerts/webhook", want: true},
		{path: "/api/v1/delivery/execution-callbacks", want: true},
		{path: "/api/v1/delivery/execution-tasks/claim", want: true},
		{path: "/api/v1/delivery/execution-tasks/task-1/runner-status", want: true},
		{path: "/api/v1/docker/operations/claim", want: true},
		{path: "/api/v1/docker/operations/task-1/runner-status", want: true},
		{path: "/api/v1/docker/operation-callbacks", want: true},
		{path: "/api/v1/copilot/agent-runs/claim", want: true},
		{path: "/api/v1/copilot/agent-runs/callback", want: true},
		{path: "/api/v1/copilot/agent-runs/tool-call", want: true},
		{path: "/api/v1/runner/secret-leases/lease-1/redeem", want: true},
		{path: "/api/v1/ai/agent-providers/registry-snapshot", want: true},
		{path: "/api/v1/ai/agent-providers/registry-acks", want: true},
		{path: "/api/v1/connectors/events", want: true},
		{path: "/api/v1/provider/outposts/claim", want: true},
		{path: "/api/v1/provider/outposts/outpost-1/heartbeat", want: true},
		{path: "/oauth2/userinfo", want: true},
		{path: "/api/v1/provider/oidc/userinfo", want: true},
		{path: "/api/v1/runner/secret-leases/lease-1", want: false},
		{path: "/api/v1/runner/secret-leases/lease-1/redeem/extra", want: false},
		{path: "/api/v1/auth/me", want: false},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			t.Parallel()
			if got := allowsExternalBearerToken(test.path); got != test.want {
				t.Fatalf("allowsExternalBearerToken(%q) = %t, want %t", test.path, got, test.want)
			}
		})
	}
}

func TestBuildPrincipalMiddlewareHandsNetworkExecutorCredentialToEndpointAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, executor := range []string{"proxy-deployment-executor", "radius-deployment-executor"} {
		for _, endpoint := range []string{"claim", "status", "callback"} {
			for _, test := range []struct {
				name, token string
				duplicate   bool
				wantStatus  int
				wantHandler bool
			}{
				{name: "dedicated credential", token: "dedicated-executor-token", wantStatus: http.StatusOK, wantHandler: true},
				{name: "wrong credential still rejected", token: "wrong-token", wantStatus: http.StatusUnauthorized, wantHandler: true},
				{name: "anonymous still rejected", wantStatus: http.StatusUnauthorized, wantHandler: true},
				{name: "user token cannot replace executor", token: "valid-key", wantStatus: http.StatusUnauthorized, wantHandler: true},
				{name: "duplicate header rejected before handler", token: "dedicated-executor-token", duplicate: true, wantStatus: http.StatusUnauthorized},
			} {
				t.Run(executor+"/"+endpoint+"/"+test.name, func(t *testing.T) {
					parser := &stubAccessTokenParser{}
					router := gin.New()
					router.Use(BuildPrincipalMiddleware(cfgpkg.AuthConfig{}, parser))
					handled := false
					path := "/api/v1/network/" + executor + "/" + endpoint
					router.POST(path, func(c *gin.Context) {
						handled = true
						if c.GetHeader("Authorization") != "Bearer dedicated-executor-token" {
							c.Status(http.StatusUnauthorized)
							return
						}
						c.Status(http.StatusOK)
					})
					request := httptest.NewRequest(http.MethodPost, path, nil)
					if test.token != "" {
						request.Header.Set("Authorization", "Bearer "+test.token)
					}
					if test.duplicate {
						request.Header.Add("Authorization", "Bearer another-token")
					}
					recorder := httptest.NewRecorder()
					router.ServeHTTP(recorder, request)
					if recorder.Code != test.wantStatus || handled != test.wantHandler {
						t.Fatalf("status=%d handler=%t, want status=%d handler=%t", recorder.Code, handled, test.wantStatus, test.wantHandler)
					}
				})
			}
		}
	}
}

func TestBuildPrincipalMiddlewareAcceptsXAPIKeyOnlyForLLMRelayPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	parser := &stubAccessTokenParser{}
	router := gin.New()
	router.Use(BuildPrincipalMiddleware(cfgpkg.AuthConfig{}, parser), RequireAuth())
	router.GET("/api/v1/ai-gateway/llm/openai/v1/models", func(c *gin.Context) {
		c.String(http.StatusOK, BearerTokenFromContext(c))
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/ai-gateway/llm/openai/v1/models", nil)
	request.Header.Set("x-api-key", "valid-key")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if parser.calls != 1 || parser.token != "valid-key" {
		t.Fatalf("parser calls=%d token=%q, want x-api-key", parser.calls, parser.token)
	}
	if recorder.Body.String() != "valid-key" {
		t.Fatalf("stored token = %q, want x-api-key value", recorder.Body.String())
	}
}

func TestBuildPrincipalMiddlewareIgnoresXAPIKeyForAIGatewayManagementPath(t *testing.T) {
	gin.SetMode(gin.TestMode)
	parser := &stubAccessTokenParser{}
	router := gin.New()
	router.Use(BuildPrincipalMiddleware(cfgpkg.AuthConfig{}, parser), RequireAuth())
	router.GET("/api/v1/ai-gateway/personal-access-tokens", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/ai-gateway/personal-access-tokens", nil)
	request.Header.Set("x-api-key", "valid-key")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if parser.calls != 0 {
		t.Fatalf("parser calls=%d, want x-api-key ignored", parser.calls)
	}
}

func TestBuildPrincipalMiddlewareIgnoresAccessTokenQueryParameter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	parser := &stubAccessTokenParser{}
	router := gin.New()
	router.Use(BuildPrincipalMiddleware(cfgpkg.AuthConfig{}, parser), RequireAuth())
	router.GET("/api/v1/auth/me", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me?access_token=valid-key", nil)
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if parser.calls != 0 {
		t.Fatalf("parser calls=%d, want query token ignored", parser.calls)
	}
}

func TestBuildPrincipalMiddlewareRejectsMultipleAuthorizationHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	parser := &stubAccessTokenParser{}
	router := gin.New()
	router.Use(BuildPrincipalMiddleware(cfgpkg.AuthConfig{}, parser))
	router.GET("/api/v1/auth/me", func(c *gin.Context) { c.Status(http.StatusOK) })

	request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	request.Header["Authorization"] = []string{"Bearer valid-key", "Bearer attacker-key"}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if parser.calls != 0 {
		t.Fatalf("parser calls = %d, want duplicate header rejected before parsing", parser.calls)
	}
}

func TestBuildPrincipalMiddlewareRejectsMultipleXAPIKeyHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	parser := &stubAccessTokenParser{}
	router := gin.New()
	router.Use(BuildPrincipalMiddleware(cfgpkg.AuthConfig{}, parser))
	router.GET("/api/v1/ai-gateway/llm/openai/v1/models", func(c *gin.Context) { c.Status(http.StatusOK) })

	request := httptest.NewRequest(http.MethodGet, "/api/v1/ai-gateway/llm/openai/v1/models", nil)
	request.Header["X-Api-Key"] = []string{"valid-key", "attacker-key"}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
	if parser.calls != 0 {
		t.Fatalf("parser calls = %d, want duplicate header rejected before parsing", parser.calls)
	}
}

func TestBuildPrincipalMiddlewareIgnoresBasicAuthForOIDCTokenEndpoint(t *testing.T) {
	gin.SetMode(gin.TestMode)
	parser := &stubAccessTokenParser{}
	router := gin.New()
	router.Use(BuildPrincipalMiddleware(cfgpkg.AuthConfig{}, parser))
	router.POST("/oauth2/token", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodPost, "/oauth2/token", nil)
	request.Header.Set("Authorization", "Basic Y2xpZW50OnNlY3JldA==")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if parser.calls != 0 {
		t.Fatalf("parser calls=%d, want Basic auth ignored by principal middleware", parser.calls)
	}
}

func TestBuildPrincipalMiddlewareAllowsOIDCUserInfoBearerTokenFallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	parser := &stubAccessTokenParser{}
	router := gin.New()
	router.Use(BuildPrincipalMiddleware(cfgpkg.AuthConfig{}, parser))
	router.GET("/oauth2/userinfo", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	request := httptest.NewRequest(http.MethodGet, "/oauth2/userinfo", nil)
	request.Header.Set("Authorization", "Bearer oidc-access-token")
	recorder := httptest.NewRecorder()

	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if parser.calls != 1 || parser.token != "oidc-access-token" {
		t.Fatalf("parser calls=%d token=%q, want attempted bearer fallback", parser.calls, parser.token)
	}
}

func TestBuildPrincipalMiddlewareAcceptsProtocolCookieOnlyOnProtocolReturnPaths(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("oauth authorize", func(t *testing.T) {
		parser := &stubAccessTokenParser{}
		router := gin.New()
		router.Use(BuildPrincipalMiddleware(cfgpkg.AuthConfig{}, parser), RequireAuth())
		router.GET("/oauth2/authorize", func(c *gin.Context) {
			c.String(http.StatusOK, PrincipalFromContext(c).UserID)
		})

		request := httptest.NewRequest(http.MethodGet, "/oauth2/authorize", nil)
		request.AddCookie(&http.Cookie{Name: ProtocolAccessCookieName, Value: "valid-key"})
		recorder := httptest.NewRecorder()

		router.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
		}
		if parser.calls != 1 || parser.token != "valid-key" {
			t.Fatalf("parser calls=%d token=%q, want protocol cookie", parser.calls, parser.token)
		}
		if recorder.Body.String() != "user-1" {
			t.Fatalf("body = %q, want user-1", recorder.Body.String())
		}
	})

	t.Run("management api", func(t *testing.T) {
		parser := &stubAccessTokenParser{}
		router := gin.New()
		router.Use(BuildPrincipalMiddleware(cfgpkg.AuthConfig{}, parser), RequireAuth())
		router.GET("/api/v1/auth/me", func(c *gin.Context) {
			c.Status(http.StatusOK)
		})

		request := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
		request.AddCookie(&http.Cookie{Name: ProtocolAccessCookieName, Value: "valid-key"})
		recorder := httptest.NewRecorder()

		router.ServeHTTP(recorder, request)

		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
		}
		if parser.calls != 0 {
			t.Fatalf("parser calls=%d, want protocol cookie ignored", parser.calls)
		}
	})
}

func TestAuthenticationRejectionMarksRequestBeforeProtectedHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	parser := &stubAccessTokenParser{}
	router := gin.New()
	router.Use(BuildPrincipalMiddleware(cfgpkg.AuthConfig{}, parser), RequireAuth())
	called := false
	router.POST("/api/v1/test-write", func(c *gin.Context) { called = true; c.Status(http.StatusOK) })
	req := httptest.NewRequest(http.MethodPost, "/api/v1/test-write", nil)
	req.Header.Set("Authorization", "Bearer rejected-test-token")
	out := httptest.NewRecorder()
	router.ServeHTTP(out, req)
	if called || out.Code != http.StatusUnauthorized || out.Header().Get("WWW-Authenticate") != `Bearer realm="soha", error="invalid_token"` {
		t.Fatal("authentication rejection did not identify a pre-handler failure")
	}
}
