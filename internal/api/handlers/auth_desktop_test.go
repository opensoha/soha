package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
)

type desktopStubIdentityService struct {
	stubIdentityService
	createInput       domainidentity.DesktopAuthAttemptCreate
	createResult      domainidentity.DesktopAuthAttempt
	beginAttemptID    string
	beginURL          string
	exchangeAttemptID string
	exchangeCode      string
	exchangeVerifier  string
	exchangeResult    domainidentity.AuthResult
}

func (s *desktopStubIdentityService) CreateDesktopAuthAttempt(_ context.Context, input domainidentity.DesktopAuthAttemptCreate) (domainidentity.DesktopAuthAttempt, error) {
	s.createInput = input
	return s.createResult, nil
}

func (s *desktopStubIdentityService) BeginDesktopAuthAttempt(_ context.Context, attemptID string) (string, error) {
	s.beginAttemptID = attemptID
	return s.beginURL, nil
}

func (s *desktopStubIdentityService) ConsumeDesktopAuthAttempt(_ context.Context, attemptID, code, verifier string) (domainidentity.AuthResult, error) {
	s.exchangeAttemptID = attemptID
	s.exchangeCode = code
	s.exchangeVerifier = verifier
	return s.exchangeResult, nil
}

func TestCreateDesktopAuthAttemptUsesRequestHostNotForwardedHost(t *testing.T) {
	service := &desktopStubIdentityService{createResult: domainidentity.DesktopAuthAttempt{
		AttemptID: "attempt-1", ExpiresAt: time.Now().UTC().Add(10 * time.Minute),
	}}
	handler := NewAuthHandler(service, nil, nil, cfgpkg.AuthConfig{})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/desktop/attempts", strings.NewReader(`{
		"providerId":"oidc-main",
		"redirectUri":"http://127.0.0.1:49152/callback/abcdefghijklmnop",
		"codeChallenge":"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNO12",
		"codeChallengeMethod":"S256"
	}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Request.Header.Set("X-Forwarded-Host", "attacker.example")
	ctx.Request.Header.Set("X-Forwarded-Proto", "https")
	ctx.Request.Host = "soha.example"

	handler.CreateDesktopAuthAttempt(ctx)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("CreateDesktopAuthAttempt status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Data domainidentity.DesktopAuthAttempt `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Data.AuthorizationURL != "https://soha.example/api/v1/auth/desktop/attempts/attempt-1/start" {
		t.Fatalf("authorization URL = %q", envelope.Data.AuthorizationURL)
	}
	if service.createInput.ProviderID != "oidc-main" || service.createInput.CodeChallengeMethod != "S256" {
		t.Fatalf("desktop create input = %#v", service.createInput)
	}
}

func TestStartDesktopAuthAttemptRedirectsOnceThroughService(t *testing.T) {
	service := &desktopStubIdentityService{beginURL: "https://idp.example/authorize"}
	handler := NewAuthHandler(service, nil, nil, cfgpkg.AuthConfig{})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/desktop/attempts/attempt-1/start", nil)
	ctx.Params = gin.Params{{Key: "attemptID", Value: "attempt-1"}}

	handler.StartDesktopAuthAttempt(ctx)

	if recorder.Code != http.StatusTemporaryRedirect || recorder.Header().Get("Location") != service.beginURL || service.beginAttemptID != "attempt-1" {
		t.Fatalf("desktop start status=%d location=%q attempt=%q", recorder.Code, recorder.Header().Get("Location"), service.beginAttemptID)
	}
}

func TestExchangeDesktopAuthAttemptSetsStandardAuthCookies(t *testing.T) {
	expiresAt := time.Now().UTC().Add(5 * time.Minute)
	service := &desktopStubIdentityService{exchangeResult: domainidentity.AuthResult{
		User: domainidentity.Principal{UserID: "user-1", UserName: "Ada", Email: "ada@example.com"},
		Tokens: domainidentity.TokenSet{
			AccessToken: "access-token", RefreshToken: "refresh-token", TokenType: "Bearer", ExpiresIn: 300, ExpiresAt: expiresAt,
		},
	}}
	handler := NewAuthHandler(service, nil, nil, cfgpkg.AuthConfig{JWT: cfgpkg.JWTConfig{RefreshTTL: time.Hour}})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/desktop/attempts/attempt-1/exchange", strings.NewReader(`{"code":"callback-code","codeVerifier":"abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQ"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Request.Header.Set("X-Forwarded-Proto", "https")
	ctx.Params = gin.Params{{Key: "attemptID", Value: "attempt-1"}}

	handler.ExchangeDesktopAuthAttempt(ctx)

	if recorder.Code != http.StatusOK || service.exchangeAttemptID != "attempt-1" || service.exchangeCode != "callback-code" {
		t.Fatalf("desktop exchange status=%d attempt=%q code=%q", recorder.Code, service.exchangeAttemptID, service.exchangeCode)
	}
	for _, name := range []string{refreshCookieName, apiMiddleware.ProtocolAccessCookieName} {
		cookie := responseCookie(recorder, name)
		if cookie == nil || !cookie.HttpOnly || !cookie.Secure {
			t.Fatalf("desktop exchange cookie %q = %#v", name, cookie)
		}
	}
}
