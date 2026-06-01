package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/soha/soha/internal/api/middleware"
	domainaccess "github.com/soha/soha/internal/domain/access"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainsettings "github.com/soha/soha/internal/domain/settings"
	cfgpkg "github.com/soha/soha/internal/infrastructure/config"
)

type stubIdentityService struct {
	current       domainidentity.Principal
	loginResult   domainidentity.AuthResult
	loginLogin    string
	loginPassword string
	logoutAccess  string
	logoutRefresh string
	loginErr      error
	logoutErr     error
}

func (s stubIdentityService) ListProviders(context.Context) []domainidentity.Provider {
	return nil
}

func (s stubIdentityService) LoginWithPassword(context.Context, string, string) (domainidentity.AuthResult, error) {
	return s.loginResult, s.loginErr
}

func (s stubIdentityService) RefreshSession(context.Context, string) (domainidentity.AuthResult, error) {
	return domainidentity.AuthResult{}, nil
}

func (s stubIdentityService) Logout(context.Context, string, string) error {
	return s.logoutErr
}

func (s stubIdentityService) CurrentPrincipal(context.Context, string) (domainidentity.Principal, error) {
	return s.current, nil
}

func (s stubIdentityService) BeginOIDCLogin(context.Context) (string, error) {
	return "", nil
}

func (s stubIdentityService) BeginProviderLogin(context.Context, string) (string, error) {
	return "", nil
}

func (s stubIdentityService) HandleOIDCCallback(context.Context, string, string) (string, error) {
	return "", nil
}

func (s stubIdentityService) HandleProviderCallback(context.Context, string, string, string) (string, error) {
	return "", nil
}

func (s stubIdentityService) ConsumeOIDCExchange(context.Context, string) (domainidentity.AuthResult, error) {
	return domainidentity.AuthResult{}, nil
}

func (s stubIdentityService) ListActiveSessions(context.Context, domainidentity.Principal, int) ([]domainidentity.SessionRecord, error) {
	return nil, nil
}

func (s stubIdentityService) RevokeSessionByID(context.Context, domainidentity.Principal, string) error {
	return nil
}

type stubAuthBootstrapAccessService struct {
	snapshot domainaccess.PermissionSnapshot
}

func (s stubAuthBootstrapAccessService) PermissionSnapshot(context.Context, domainidentity.Principal) (domainaccess.PermissionSnapshot, error) {
	return s.snapshot, nil
}

type stubAuthBootstrapSettingsService struct {
	branding domainsettings.BrandingSettings
}

func (s stubAuthBootstrapSettingsService) GetBrandingSettings(context.Context, domainidentity.Principal) (domainsettings.BrandingSettings, error) {
	return s.branding, nil
}

func TestAuthBootstrapReturnsCurrentUserSnapshotAndBranding(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	current := domainidentity.Principal{
		UserID:   "u-1",
		UserName: "admin",
		Email:    "admin@example.com",
		Roles:    []string{"admin"},
	}
	snapshot := domainaccess.PermissionSnapshot{
		PermissionKeys: []string{"settings.branding.view"},
		VisibleMenuIDs: []string{"settings"},
		VisibleMenus: []domainaccess.VisibleMenu{
			{ID: "settings", Path: "/settings"},
		},
	}
	branding := domainsettings.BrandingSettings{
		AppTitle:     "Soha Pro",
		SidebarTitle: "SOHA",
	}

	handler := NewAuthHandler(
		stubIdentityService{current: current},
		stubAuthBootstrapAccessService{snapshot: snapshot},
		stubAuthBootstrapSettingsService{branding: branding},
		cfgpkg.AuthConfig{},
	)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/bootstrap", nil)
	ctx.Set("principal", domainidentity.Principal{UserID: current.UserID})

	handler.Bootstrap(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload struct {
		Data authBootstrapResponse `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Data.User.UserID != current.UserID {
		t.Fatalf("user.userId = %q, want %q", payload.Data.User.UserID, current.UserID)
	}
	if payload.Data.CurrentUser.UserName != current.UserName {
		t.Fatalf("currentUser.userName = %q, want %q", payload.Data.CurrentUser.UserName, current.UserName)
	}
	if len(payload.Data.PermissionSnapshot.PermissionKeys) != 1 || payload.Data.PermissionSnapshot.PermissionKeys[0] != "settings.branding.view" {
		t.Fatalf("permissionSnapshot.permissionKeys = %v", payload.Data.PermissionSnapshot.PermissionKeys)
	}
	if payload.Data.Branding.AppTitle != branding.AppTitle {
		t.Fatalf("branding.appTitle = %q, want %q", payload.Data.Branding.AppTitle, branding.AppTitle)
	}
}

func TestLoginOptionsReturnSliderVerificationConfig(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	handler := NewAuthHandler(stubIdentityService{}, nil, nil, cfgpkg.AuthConfig{
		LoginVerification: cfgpkg.LoginVerificationConfig{SliderEnabled: true},
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/login-options", nil)

	handler.LoginOptions(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload struct {
		Data struct {
			Verification struct {
				SliderEnabled bool `json:"sliderEnabled"`
			} `json:"verification"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !payload.Data.Verification.SliderEnabled {
		t.Fatal("sliderEnabled = false, want true")
	}
}

type recordingIdentityService struct {
	stubIdentityService
}

func (s *recordingIdentityService) LoginWithPassword(_ context.Context, login, password string) (domainidentity.AuthResult, error) {
	s.loginLogin = login
	s.loginPassword = password
	return s.loginResult, s.loginErr
}

func (s *recordingIdentityService) Logout(_ context.Context, accessToken, refreshToken string) error {
	s.logoutAccess = accessToken
	s.logoutRefresh = refreshToken
	return s.logoutErr
}

func TestProLoginUsesUsernameAndReturnsAuthorityShape(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	identity := &recordingIdentityService{
		stubIdentityService: stubIdentityService{
			loginResult: domainidentity.AuthResult{
				User: domainidentity.Principal{
					UserID:   "u-1",
					UserName: "admin",
					Roles:    []string{"admin"},
				},
			},
		},
	}
	handler := NewAuthHandler(identity, nil, nil, cfgpkg.AuthConfig{})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/login/account", strings.NewReader(`{"username":"admin","password":"secret","type":"account"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	handler.ProLogin(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if identity.loginLogin != "admin" || identity.loginPassword != "secret" {
		t.Fatalf("login call = (%q, %q)", identity.loginLogin, identity.loginPassword)
	}

	var payload proLoginResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Status != "ok" {
		t.Fatalf("status = %q, want ok", payload.Status)
	}
	if payload.CurrentAuthority != "admin" {
		t.Fatalf("currentAuthority = %q, want admin", payload.CurrentAuthority)
	}
	if payload.Type != "account" {
		t.Fatalf("type = %q, want account", payload.Type)
	}
}

func TestProCurrentUserMapsPrincipalToProShape(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	current := domainidentity.Principal{
		UserID:   "u-1",
		UserName: "operator",
		Email:    "operator@example.com",
		Roles:    []string{"viewer"},
		Teams:    []string{"platform"},
		Tags:     []string{"oncall"},
	}
	handler := NewAuthHandler(stubIdentityService{current: current}, nil, nil, cfgpkg.AuthConfig{})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/currentUser", nil)
	ctx.Set("principal", domainidentity.Principal{UserID: current.UserID})

	handler.ProCurrentUser(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload struct {
		Data proCurrentUser `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Data.Name != current.UserName {
		t.Fatalf("name = %q, want %q", payload.Data.Name, current.UserName)
	}
	if payload.Data.UserID != current.UserID {
		t.Fatalf("userid = %q, want %q", payload.Data.UserID, current.UserID)
	}
	if payload.Data.Group != "platform" {
		t.Fatalf("group = %q, want platform", payload.Data.Group)
	}
	if len(payload.Data.Tags) != 1 || payload.Data.Tags[0].Key != "oncall" {
		t.Fatalf("tags = %+v", payload.Data.Tags)
	}
	if payload.Data.Access != "user" {
		t.Fatalf("access = %q, want user", payload.Data.Access)
	}
}

func TestProLogoutUsesNormalizedBearerToken(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	identity := &recordingIdentityService{}
	handler := NewAuthHandler(identity, nil, nil, cfgpkg.AuthConfig{})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/login/outLogin", strings.NewReader(`{"refreshToken":"refresh-1"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("access_token", "access-1")

	handler.ProLogout(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if identity.logoutAccess != "access-1" || identity.logoutRefresh != "refresh-1" {
		t.Fatalf("logout call = (%q, %q)", identity.logoutAccess, identity.logoutRefresh)
	}
}

func TestNormalizeBearerTokenTrimsHeaderPrefix(t *testing.T) {
	t.Parallel()

	value := apiMiddleware.BearerTokenFromContext(func() *gin.Context {
		recorder := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(recorder)
		ctx.Set("access_token", "token-1")
		return ctx
	}())
	if value != "token-1" {
		t.Fatalf("context token = %q, want token-1", value)
	}
}

func TestBuildPrincipalMiddlewareStripsBearerPrefix(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	type parser struct {
		token string
	}
	var parsed parser
	middleware := apiMiddleware.BuildPrincipalMiddleware(
		cfgpkg.AuthConfig{},
		accessTokenParserFunc(func(_ context.Context, token string) (domainidentity.Principal, domainidentity.AccessContext, error) {
			parsed.token = token
			return domainidentity.Principal{UserID: "u-1"}, domainidentity.AccessContext{}, nil
		}),
	)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
	ctx.Request.Header.Set("Authorization", "Bearer token-1")

	middleware(ctx)

	if parsed.token != "token-1" {
		t.Fatalf("parsed token = %q, want token-1", parsed.token)
	}
	if apiMiddleware.BearerTokenFromContext(ctx) != "token-1" {
		t.Fatalf("context token = %q, want token-1", apiMiddleware.BearerTokenFromContext(ctx))
	}
}

func TestBuildPrincipalMiddlewareAllowsRunnerBearerToken(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	called := false
	middleware := apiMiddleware.BuildPrincipalMiddleware(
		cfgpkg.AuthConfig{},
		accessTokenParserFunc(func(_ context.Context, token string) (domainidentity.Principal, domainidentity.AccessContext, error) {
			called = true
			return domainidentity.Principal{}, domainidentity.AccessContext{}, errors.New("invalid jwt")
		}),
	)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/docker/operations/claim", nil)
	ctx.Request.Header.Set("Authorization", "Bearer runner-token")

	middleware(ctx)

	if !called {
		t.Fatal("parser was not called")
	}
	if recorder.Code == http.StatusUnauthorized {
		t.Fatalf("status = %d, runner token endpoint should continue to handler", recorder.Code)
	}
	if ctx.IsAborted() {
		t.Fatal("runner token endpoint was aborted")
	}
}

type accessTokenParserFunc func(context.Context, string) (domainidentity.Principal, domainidentity.AccessContext, error)

func (f accessTokenParserFunc) ParseAccessToken(ctx context.Context, token string) (domainidentity.Principal, domainidentity.AccessContext, error) {
	return f(ctx, token)
}
