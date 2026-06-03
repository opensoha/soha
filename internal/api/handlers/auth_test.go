package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	apiMiddleware "github.com/soha/soha/internal/api/middleware"
	domainaccess "github.com/soha/soha/internal/domain/access"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainsettings "github.com/soha/soha/internal/domain/settings"
	cfgpkg "github.com/soha/soha/internal/infrastructure/config"
)

type stubIdentityService struct {
	current       domainidentity.Principal
	profile       domainidentity.UserProfile
	loginResult   domainidentity.AuthResult
	refreshResult domainidentity.AuthResult
	loginLogin    string
	loginPassword string
	logoutAccess  string
	logoutRefresh string
	streamReq     domainidentity.StreamTicketRequest
	streamTicket  domainidentity.StreamTicket
	loginErr      error
	refreshErr    error
	logoutErr     error
	streamErr     error
}

func (s stubIdentityService) ListProviders(context.Context) []domainidentity.Provider {
	return nil
}

func (s stubIdentityService) LoginWithPassword(context.Context, string, string) (domainidentity.AuthResult, error) {
	return s.loginResult, s.loginErr
}

func (s stubIdentityService) RefreshSession(context.Context, string) (domainidentity.AuthResult, error) {
	return s.refreshResult, s.refreshErr
}

func (s stubIdentityService) Logout(context.Context, string, string) error {
	return s.logoutErr
}

func (s stubIdentityService) CurrentPrincipal(context.Context, string) (domainidentity.Principal, error) {
	return s.current, nil
}

func (s stubIdentityService) CurrentProfile(context.Context, domainidentity.Principal) (domainidentity.UserProfile, error) {
	return s.profile, nil
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

func (s stubIdentityService) IssueStreamTicket(context.Context, domainidentity.Principal, domainidentity.AccessContext, domainidentity.StreamTicketRequest) (domainidentity.StreamTicket, error) {
	return s.streamTicket, s.streamErr
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

func TestAuthProfileReturnsCurrentUserProfile(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	lastLoginAt := time.Date(2026, 6, 3, 2, 20, 0, 0, time.UTC)
	profile := domainidentity.UserProfile{
		UserID:      "u-1",
		Username:    "admin",
		DisplayName: "Admin",
		Email:       "admin@example.com",
		Status:      "active",
		Roles:       []string{"admin"},
		Identities: []domainidentity.LinkedIdentity{
			{
				ID:             "password:u-1",
				ProviderType:   "password",
				ProviderID:     "local",
				ProviderUserID: "admin",
			},
		},
		Sessions: []domainidentity.SessionRecord{
			{
				ID:           "s-1",
				UserID:       "u-1",
				UserName:     "Admin",
				Email:        "admin@example.com",
				ProviderType: "password",
				Status:       "active",
				CreatedAt:    lastLoginAt,
				LastSeenAt:   lastLoginAt,
				ExpiresAt:    lastLoginAt.Add(time.Hour),
			},
		},
		LastLoginAt: &lastLoginAt,
	}
	handler := NewAuthHandler(
		stubIdentityService{profile: profile},
		nil,
		nil,
		cfgpkg.AuthConfig{},
	)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/auth/profile", nil)
	ctx.Set("principal", domainidentity.Principal{UserID: profile.UserID, UserName: profile.DisplayName})

	handler.Profile(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}

	var payload struct {
		Data domainidentity.UserProfile `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Data.Username != profile.Username {
		t.Fatalf("username = %q, want %q", payload.Data.Username, profile.Username)
	}
	if len(payload.Data.Identities) != 1 || payload.Data.Identities[0].ProviderType != "password" {
		t.Fatalf("identities = %#v", payload.Data.Identities)
	}
	if len(payload.Data.Sessions) != 1 || payload.Data.Sessions[0].ID != "s-1" {
		t.Fatalf("sessions = %#v", payload.Data.Sessions)
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
	refreshToken string
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

func (s *recordingIdentityService) RefreshSession(_ context.Context, refreshToken string) (domainidentity.AuthResult, error) {
	s.refreshToken = refreshToken
	return s.refreshResult, s.refreshErr
}

func (s *recordingIdentityService) IssueStreamTicket(_ context.Context, _ domainidentity.Principal, _ domainidentity.AccessContext, req domainidentity.StreamTicketRequest) (domainidentity.StreamTicket, error) {
	s.streamReq = req
	return s.streamTicket, s.streamErr
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

func TestLoginSetsHttpOnlyRefreshCookie(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	identity := &recordingIdentityService{
		stubIdentityService: stubIdentityService{
			loginResult: domainidentity.AuthResult{
				User: domainidentity.Principal{
					UserID:   "u-1",
					UserName: "admin",
				},
				Tokens: domainidentity.TokenSet{
					AccessToken:  "access-1",
					RefreshToken: "refresh-1",
					TokenType:    "Bearer",
					ExpiresIn:    3600,
					ExpiresAt:    time.Now().Add(time.Hour),
				},
			},
		},
	}
	handler := NewAuthHandler(identity, nil, nil, cfgpkg.AuthConfig{
		JWT: cfgpkg.JWTConfig{RefreshTTL: time.Hour},
	})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", strings.NewReader(`{"login":"admin","password":"secret"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")

	handler.Login(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	cookie := responseCookie(recorder, refreshCookieName)
	if cookie == nil {
		t.Fatalf("missing %s cookie", refreshCookieName)
	}
	if cookie.Value != "refresh-1" {
		t.Fatalf("refresh cookie = %q, want refresh-1", cookie.Value)
	}
	if !cookie.HttpOnly {
		t.Fatal("refresh cookie should be HttpOnly")
	}
	if cookie.Path != "/api/v1/auth" {
		t.Fatalf("refresh cookie path = %q, want /api/v1/auth", cookie.Path)
	}
	if cookie.MaxAge != int(time.Hour/time.Second) {
		t.Fatalf("refresh cookie maxAge = %d, want %d", cookie.MaxAge, int(time.Hour/time.Second))
	}
}

func TestRefreshUsesRefreshCookieWhenBodyIsEmpty(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	identity := &recordingIdentityService{
		stubIdentityService: stubIdentityService{
			refreshResult: domainidentity.AuthResult{
				User: domainidentity.Principal{
					UserID:   "u-1",
					UserName: "admin",
				},
				Tokens: domainidentity.TokenSet{
					AccessToken:  "access-2",
					RefreshToken: "refresh-2",
					TokenType:    "Bearer",
					ExpiresIn:    3600,
					ExpiresAt:    time.Now().Add(time.Hour),
				},
			},
		},
	}
	handler := NewAuthHandler(identity, nil, nil, cfgpkg.AuthConfig{})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	ctx.Request.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "refresh-1"})

	handler.Refresh(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if identity.refreshToken != "refresh-1" {
		t.Fatalf("refresh token = %q, want refresh-1", identity.refreshToken)
	}
	cookie := responseCookie(recorder, refreshCookieName)
	if cookie == nil {
		t.Fatalf("missing %s cookie", refreshCookieName)
	}
	if cookie.Value != "refresh-2" {
		t.Fatalf("rotated refresh cookie = %q, want refresh-2", cookie.Value)
	}
	if !cookie.HttpOnly {
		t.Fatal("rotated refresh cookie should be HttpOnly")
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

func TestLogoutUsesRefreshCookieAndClearsCookie(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	identity := &recordingIdentityService{}
	handler := NewAuthHandler(identity, nil, nil, cfgpkg.AuthConfig{})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	ctx.Request.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "refresh-1"})
	ctx.Set("access_token", "access-1")

	handler.Logout(ctx)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusOK)
	}
	if identity.logoutAccess != "access-1" || identity.logoutRefresh != "refresh-1" {
		t.Fatalf("logout call = (%q, %q)", identity.logoutAccess, identity.logoutRefresh)
	}
	cookie := responseCookie(recorder, refreshCookieName)
	if cookie == nil {
		t.Fatalf("missing %s cookie clear header", refreshCookieName)
	}
	if cookie.MaxAge >= 0 {
		t.Fatalf("clear cookie maxAge = %d, want negative", cookie.MaxAge)
	}
}

func TestIssueStreamTicketPassesPrincipalAccessContextAndPath(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	expiresAt := time.Now().UTC().Add(time.Minute)
	identity := &recordingIdentityService{
		stubIdentityService: stubIdentityService{
			streamTicket: domainidentity.StreamTicket{
				Ticket:    "ticket-1",
				ExpiresAt: expiresAt,
			},
		},
	}
	handler := NewAuthHandler(identity, nil, nil, cfgpkg.AuthConfig{})

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/auth/stream-ticket", strings.NewReader(`{"path":"/api/v1/virtualization/operations/task-1/stream"}`))
	ctx.Request.Header.Set("Content-Type", "application/json")
	ctx.Set("principal", domainidentity.Principal{UserID: "u-1"})
	ctx.Set("access_context", domainidentity.AccessContext{TokenKind: "session_access", SessionID: "s-1"})

	handler.IssueStreamTicket(ctx)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusCreated)
	}
	if identity.streamReq.Path != "/api/v1/virtualization/operations/task-1/stream" {
		t.Fatalf("stream ticket path = %q", identity.streamReq.Path)
	}
	var payload struct {
		Data domainidentity.StreamTicket `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload.Data.Ticket != "ticket-1" {
		t.Fatalf("ticket = %q, want ticket-1", payload.Data.Ticket)
	}
}

type streamTicketParserStub struct {
	ticket string
	path   string
}

func (s *streamTicketParserStub) ParseAccessToken(context.Context, string) (domainidentity.Principal, domainidentity.AccessContext, error) {
	return domainidentity.Principal{}, domainidentity.AccessContext{}, errors.New("unexpected access token")
}

func (s *streamTicketParserStub) ParseStreamTicket(_ context.Context, ticket, path string) (domainidentity.Principal, domainidentity.AccessContext, error) {
	s.ticket = ticket
	s.path = path
	return domainidentity.Principal{UserID: "u-1"}, domainidentity.AccessContext{TokenKind: "stream_ticket", TokenID: ticket}, nil
}

func TestBuildPrincipalMiddlewareAcceptsStreamTicket(t *testing.T) {
	t.Parallel()

	gin.SetMode(gin.TestMode)

	parser := &streamTicketParserStub{}
	middleware := apiMiddleware.BuildPrincipalMiddleware(cfgpkg.AuthConfig{}, parser)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/v1/virtualization/operations/task-1/stream?stream_ticket=ticket-1", nil)

	middleware(ctx)

	if parser.ticket != "ticket-1" || parser.path != "/api/v1/virtualization/operations/task-1/stream" {
		t.Fatalf("stream parser call = (%q, %q)", parser.ticket, parser.path)
	}
	if apiMiddleware.PrincipalFromContext(ctx).UserID != "u-1" {
		t.Fatalf("principal = %#v", apiMiddleware.PrincipalFromContext(ctx))
	}
	if apiMiddleware.AccessContextFromContext(ctx).TokenKind != "stream_ticket" {
		t.Fatalf("access context = %#v", apiMiddleware.AccessContextFromContext(ctx))
	}
}

func responseCookie(recorder *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range recorder.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
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
