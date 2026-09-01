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
	domainportal "github.com/opensoha/soha/internal/domain/providerportal"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type browserHandoffStubIdentityService struct {
	stubIdentityService
	handoff        domainportal.BrowserHandoff
	completion     domainportal.BrowserHandoffCompletion
	result         domainidentity.AuthResult
	completeErr    error
	completeCalled bool
	origin         string
	expectedOrigin string
	accessToken    string
	refreshToken   string
	auditReasons   []string
}

func (s *browserHandoffStubIdentityService) GetBrowserHandoff(context.Context, string) (domainportal.BrowserHandoff, error) {
	return s.handoff, nil
}

func (s *browserHandoffStubIdentityService) CompleteBrowserHandoff(_ context.Context, _ string, origin, accessToken, refreshToken string) (domainportal.BrowserHandoffCompletion, domainidentity.AuthResult, error) {
	s.completeCalled = true
	s.origin = origin
	s.accessToken = accessToken
	s.refreshToken = refreshToken
	if origin != s.expectedOrigin {
		return domainportal.BrowserHandoffCompletion{}, domainidentity.AuthResult{}, apperrors.NewBusiness(apperrors.ErrInvalidArgument, "browser_handoff_origin_invalid", "browser handoff origin is invalid", "浏览器授权来源无效")
	}
	if s.completeErr != nil {
		return domainportal.BrowserHandoffCompletion{}, domainidentity.AuthResult{}, s.completeErr
	}
	return s.completion, s.result, nil
}

func TestCompleteBrowserHandoffPreservesCookiesOnAccountConflict(t *testing.T) {
	service := &browserHandoffStubIdentityService{
		expectedOrigin: "https://soha.example",
		completeErr: apperrors.NewBusiness(
			apperrors.ErrConflict,
			"browser_handoff_account_conflict",
			"browser is signed in as a different account",
			"浏览器已登录其他账号",
		),
	}
	handler := NewAuthHandler(service, nil, nil, cfgpkg.AuthConfig{})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "https://soha.example/api/v1/auth/browser-handoffs/handoff-1/complete", nil)
	ctx.Request.Header.Set("Origin", "https://soha.example")
	ctx.Request.AddCookie(&http.Cookie{Name: apiMiddleware.ProtocolAccessCookieName, Value: "existing-access"})
	ctx.Request.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "existing-refresh"})
	ctx.Params = gin.Params{{Key: "browserHandoffID", Value: "handoff-1"}}
	handler.CompleteBrowserHandoff(ctx)

	if recorder.Code != http.StatusConflict || len(recorder.Result().Cookies()) != 0 {
		t.Fatalf("account conflict status=%d cookies=%#v", recorder.Code, recorder.Result().Cookies())
	}
}

func (s *browserHandoffStubIdentityService) AuditBrowserHandoffFailure(_ context.Context, _ domainidentity.Principal, _ string, reason string) {
	s.auditReasons = append(s.auditReasons, reason)
}

func TestCompleteBrowserHandoffRequiresExactOriginAndReturnsNoTokens(t *testing.T) {
	service := &browserHandoffStubIdentityService{
		expectedOrigin: "https://soha.example",
		completion:     domainportal.BrowserHandoffCompletion{Status: "completed", DestinationURL: "/oauth2/authorize?client_id=console"},
		result: domainidentity.AuthResult{Tokens: domainidentity.TokenSet{
			AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresIn: 300, ExpiresAt: time.Now().UTC().Add(5 * time.Minute),
		}},
	}
	handler := NewAuthHandler(service, nil, nil, cfgpkg.AuthConfig{JWT: cfgpkg.JWTConfig{RefreshTTL: time.Hour}})

	badRecorder := httptest.NewRecorder()
	badContext, _ := gin.CreateTestContext(badRecorder)
	badContext.Request = httptest.NewRequest(http.MethodPost, "https://soha.example/api/v1/auth/browser-handoffs/handoff-1/complete", nil)
	badContext.Request.Header.Set("Origin", "https://evil.example")
	badContext.Params = gin.Params{{Key: "browserHandoffID", Value: "handoff-1"}}
	handler.CompleteBrowserHandoff(badContext)
	if badRecorder.Code != http.StatusBadRequest || !service.completeCalled || service.origin != "https://evil.example" {
		t.Fatalf("cross-origin completion status=%d service=%#v", badRecorder.Code, service)
	}
	if badRecorder.Header().Get("Cache-Control") != "no-store" || badRecorder.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("cross-origin response headers = %#v", badRecorder.Header())
	}
	service.completeCalled = false

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "https://soha.example/api/v1/auth/browser-handoffs/handoff-1/complete", nil)
	ctx.Request.Header.Set("Origin", "https://soha.example")
	ctx.Request.AddCookie(&http.Cookie{Name: apiMiddleware.ProtocolAccessCookieName, Value: "old-access"})
	ctx.Request.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "old-refresh"})
	ctx.Params = gin.Params{{Key: "browserHandoffID", Value: "handoff-1"}}
	handler.CompleteBrowserHandoff(ctx)

	if recorder.Code != http.StatusOK || !service.completeCalled || service.origin != "https://soha.example" || service.accessToken != "old-access" || service.refreshToken != "old-refresh" {
		t.Fatalf("completion status=%d service=%#v", recorder.Code, service)
	}
	if recorder.Body.String() == "" || strings.Contains(recorder.Body.String(), "new-access") || strings.Contains(recorder.Body.String(), "new-refresh") {
		t.Fatalf("completion leaked tokens: %s", recorder.Body.String())
	}
	for _, name := range []string{refreshCookieName, apiMiddleware.ProtocolAccessCookieName} {
		if cookie := responseCookie(recorder, name); cookie == nil || !cookie.HttpOnly || !cookie.Secure {
			t.Fatalf("handoff cookie %q = %#v", name, cookie)
		}
	}
	if recorder.Header().Get("Cache-Control") != "no-store" || recorder.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("completion response headers = %#v", recorder.Header())
	}
}

func TestGetBrowserHandoffReturnsConfirmationData(t *testing.T) {
	service := &browserHandoffStubIdentityService{handoff: domainportal.BrowserHandoff{
		ID: "handoff-1", Status: "pending", AccountName: "Ada",
		Application: domainportal.BrowserHandoffApplication{ID: "app-1", Name: "Soha Console"}, ExpiresAt: time.Now().UTC().Add(time.Minute),
	}}
	handler := NewAuthHandler(service, nil, nil, cfgpkg.AuthConfig{})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/browser-handoffs/handoff-1", nil)
	ctx.Params = gin.Params{{Key: "browserHandoffID", Value: "handoff-1"}}
	handler.GetBrowserHandoff(ctx)

	var envelope struct {
		Data domainportal.BrowserHandoff `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil || recorder.Code != http.StatusOK || envelope.Data.Application.ID != "app-1" || envelope.Data.AccountName != "Ada" {
		t.Fatalf("handoff response status=%d body=%s error=%v", recorder.Code, recorder.Body.String(), err)
	}
	if strings.Contains(recorder.Body.String(), "handoff-1") || strings.Contains(recorder.Body.String(), "principal") || strings.Contains(recorder.Body.String(), "email") {
		t.Fatalf("inspection exposed internal identity data: %s", recorder.Body.String())
	}
	if recorder.Header().Get("Cache-Control") != "no-store" || recorder.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatalf("inspection response headers = %#v", recorder.Header())
	}
}
