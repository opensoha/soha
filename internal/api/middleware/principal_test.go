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

func TestAllowsExternalBearerTokenIncludesConnectorEvents(t *testing.T) {
	t.Parallel()

	if !allowsExternalBearerToken("/api/v1/connectors/events") {
		t.Fatal("connectors event sink should allow handler-level bearer fallback")
	}
}

func TestAllowsExternalBearerTokenIncludesProviderOutposts(t *testing.T) {
	t.Parallel()

	if !allowsExternalBearerToken("/api/v1/provider/outposts/claim") {
		t.Fatal("outpost claim should allow handler-level bearer fallback")
	}
	if !allowsExternalBearerToken("/api/v1/provider/outposts/outpost-1/heartbeat") {
		t.Fatal("outpost heartbeat should allow handler-level bearer fallback")
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
