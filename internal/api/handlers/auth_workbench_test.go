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
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type workbenchRefreshAccess struct {
	t    *testing.T
	keys []string
	err  error
	read bool
}

func (s *workbenchRefreshAccess) PermissionSnapshot(_ context.Context, principal domainidentity.Principal) (domainaccess.PermissionSnapshot, error) {
	s.read = true
	assertWorkbenchRefreshPrincipal(s.t, principal)
	return domainaccess.PermissionSnapshot{
		PermissionKeys: s.keys, VisibleMenuIDs: []string{}, VisibleMenus: []domainaccess.VisibleMenu{},
	}, s.err
}

type workbenchRefreshReaders struct {
	t                              *testing.T
	appErr, environmentErr, svcErr error
	appRead, environmentRead       bool
	svcRead, empty                 bool
}

func (s *workbenchRefreshReaders) Get(_ context.Context, principal domainidentity.Principal, id string) (domainapp.App, error) {
	s.appRead = true
	s.assertScope(principal, id)
	return domainapp.App{ID: id, Key: "orders"}, s.appErr
}

func (s *workbenchRefreshReaders) ListApplicationEnvironmentNavigation(_ context.Context, principal domainidentity.Principal, id string) ([]domaindelivery.ApplicationEnvironmentNavigation, error) {
	s.environmentRead = true
	s.assertScope(principal, id)
	if s.empty {
		return nil, s.environmentErr
	}
	return []domaindelivery.ApplicationEnvironmentNavigation{{ApplicationEnvironmentID: "binding-1"}}, s.environmentErr
}

func (s *workbenchRefreshReaders) ListServiceIdentities(_ context.Context, principal domainidentity.Principal, id string) ([]domainapp.ServiceIdentity, error) {
	s.svcRead = true
	s.assertScope(principal, id)
	if s.empty {
		return nil, s.svcErr
	}
	return []domainapp.ServiceIdentity{{ID: "service-1", ApplicationID: id, ServiceKind: domainapp.ServiceKindKubernetesWorkload}}, s.svcErr
}

func (s *workbenchRefreshReaders) assertScope(principal domainidentity.Principal, id string) {
	s.t.Helper()
	assertWorkbenchRefreshPrincipal(s.t, principal)
	if id != "app-1" {
		s.t.Errorf("workbench read application %q, want app-1", id)
	}
}

func assertWorkbenchRefreshPrincipal(t *testing.T, principal domainidentity.Principal) {
	t.Helper()
	if principal.UserID != "u-1" || len(principal.Roles) != 1 || principal.Roles[0] != "current" {
		t.Errorf("workbench read did not use freshly parsed principal: %+v", principal)
	}
}

type workbenchRefreshModule bool

func (enabled workbenchRefreshModule) ModuleEnabled(module string) bool {
	return bool(enabled) && module == "delivery"
}

func workbenchRefreshIdentity() *recordingIdentityService {
	return &recordingIdentityService{stubIdentityService: stubIdentityService{
		refreshResult: domainidentity.AuthResult{
			User: domainidentity.Principal{UserID: "u-1", Roles: []string{"before-access-parse"}},
			Tokens: domainidentity.TokenSet{
				AccessToken: "fresh-access", RefreshToken: "new-refresh", TokenType: "Bearer",
				ExpiresIn: 900, ExpiresAt: time.Now().Add(time.Hour),
			},
		},
	}}
}

func workbenchRefreshRequest(cookied bool, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "https://console.example/api/v1/auth/refresh/application-workbench", strings.NewReader(body))
	req.Header.Set("Origin", "https://console.example")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept-Encoding", "gzip")
	if cookied {
		req.AddCookie(&http.Cookie{Name: refreshCookieName, Value: "existing-refresh"})
	}
	return req
}

func TestRefreshApplicationWorkbenchUsesFreshSessionAndScopedReads(t *testing.T) {
	perms := []string{appaccess.PermWorkbenchDeliveryView, appaccess.PermDeliveryApplicationsView}
	denied := apperrors.ErrAccessDenied
	for _, tc := range []struct {
		name                                        string
		keys                                        []string
		parseErr, snapshotErr, appErr, envErr       error
		serviceErr                                  error
		disableModule, empty, wrongSubject          bool
		wantSnapshot, wantApp, wantLists, wantEntry bool
	}{
		{name: "warm entry", keys: perms, wantSnapshot: true, wantApp: true, wantLists: true, wantEntry: true},
		{name: "real empty arrays", keys: perms, empty: true, wantSnapshot: true, wantApp: true, wantLists: true, wantEntry: true},
		{name: "fresh token denied", keys: perms, parseErr: apperrors.ErrUnauthorized},
		{name: "subject changed", keys: perms, wrongSubject: true},
		{name: "snapshot unavailable", keys: perms, snapshotErr: denied},
		{name: "workbench gate", keys: perms[1:], wantSnapshot: true},
		{name: "application permission gate", keys: perms[:1], wantSnapshot: true},
		{name: "module disabled", keys: perms, disableModule: true, wantSnapshot: true},
		{name: "application outside scope", keys: perms, appErr: denied, wantSnapshot: true, wantApp: true},
		{name: "application missing", keys: perms, appErr: apperrors.ErrNotFound, wantSnapshot: true, wantApp: true},
		{name: "environment read denied", keys: perms, envErr: denied, wantSnapshot: true, wantApp: true, wantLists: true},
		{name: "service read denied", keys: perms, serviceErr: denied, wantSnapshot: true, wantApp: true, wantLists: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			identity := workbenchRefreshIdentity()
			access := &workbenchRefreshAccess{t: t, keys: tc.keys, err: tc.snapshotErr}
			readers := &workbenchRefreshReaders{t: t, appErr: tc.appErr, environmentErr: tc.envErr, svcErr: tc.serviceErr, empty: tc.empty}
			parser := accessTokenParserFunc(func(_ context.Context, token string) (domainidentity.Principal, domainidentity.AccessContext, error) {
				if token != "fresh-access" {
					t.Error("did not parse newly issued access token")
				}
				userID := "u-1"
				if tc.wrongSubject {
					userID = "u-2"
				}
				return domainidentity.Principal{UserID: userID, Roles: []string{"current"}},
					domainidentity.AccessContext{TokenKind: "session_access", SessionID: "session-1"}, tc.parseErr
			})
			handler := NewAuthHandler(identity, access, nil, cfgpkg.AuthConfig{}).WithApplicationWorkbench(AuthApplicationWorkbenchServices{
				Tokens: parser, Applications: readers, Environments: readers, Modules: workbenchRefreshModule(!tc.disableModule),
			})
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = workbenchRefreshRequest(true, `{"applicationId":"app-1"}`)

			handler.RefreshApplicationWorkbench(c)

			if recorder.Code != http.StatusOK || identity.refreshToken != "existing-refresh" {
				t.Fatalf("refresh status %d, cookie refresh called = %v", recorder.Code, identity.refreshToken != "")
			}
			headers := recorder.Result().Header
			if headers.Get("Cache-Control") != "no-store" || headers.Get("Content-Encoding") != "" {
				t.Fatalf("unexpected cache or compression headers: %s, %s", headers.Get("Cache-Control"), headers.Get("Content-Encoding"))
			}
			for _, name := range []string{refreshCookieName, apiMiddleware.ProtocolAccessCookieName} {
				cookie := responseCookie(recorder, name)
				if cookie == nil || !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
					t.Fatalf("cookie %s did not retain secure refresh policy", name)
				}
			}
			var envelope struct {
				Data applicationWorkbenchRefreshResponse `json:"data"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil || envelope.Data.Session.Tokens.AccessToken != "fresh-access" {
				t.Fatal("the refreshed session was not returned in the response")
			}
			got := envelope.Data
			if (got.PermissionSnapshot != nil) != tc.wantSnapshot || (got.Entry != nil) != tc.wantEntry {
				t.Fatalf("snapshot present = %v, entry present = %v", got.PermissionSnapshot != nil, got.Entry != nil)
			}
			if access.read != (tc.parseErr == nil && !tc.wrongSubject) || readers.appRead != tc.wantApp ||
				readers.environmentRead != tc.wantLists || readers.svcRead != tc.wantLists {
				t.Fatalf("unexpected scoped reads: snapshot=%v app=%v environments=%v services=%v", access.read, readers.appRead, readers.environmentRead, readers.svcRead)
			}
			if got.Entry != nil && (got.Entry.Application.ID != "app-1" || got.Entry.Environments == nil || got.Entry.Services == nil) {
				t.Fatalf("incorrect populated or empty app entry: %+v", got.Entry)
			}
		})
	}
}

func TestRefreshApplicationWorkbenchRequiresBrowserCookieAndOrigin(t *testing.T) {
	validBody := `{"applicationId":"app-1"}`
	for _, tc := range []struct {
		name, origin, fetchSite, body string
		cookie                        bool
		status                        int
	}{
		{name: "missing origin", body: validBody, cookie: true, status: http.StatusForbidden},
		{name: "other origin", origin: "https://other.example", body: validBody, cookie: true, status: http.StatusForbidden},
		{name: "cross site", origin: "https://console.example", fetchSite: "cross-site", body: validBody, cookie: true, status: http.StatusForbidden},
		{name: "missing cookie", origin: "https://console.example", body: validBody, status: http.StatusUnauthorized},
		{name: "injected refresh token", origin: "https://console.example", body: `{"applicationId":"app-1","refreshToken":"body-token"}`, cookie: true, status: http.StatusBadRequest},
		{name: "extra JSON document", origin: "https://console.example", body: validBody + "{}", cookie: true, status: http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			identity := workbenchRefreshIdentity()
			handler := NewAuthHandler(identity, nil, nil, cfgpkg.AuthConfig{})
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = workbenchRefreshRequest(tc.cookie, tc.body)
			c.Request.Header.Set("Origin", tc.origin)
			c.Request.Header.Set("Sec-Fetch-Site", tc.fetchSite)

			handler.RefreshApplicationWorkbench(c)

			if recorder.Code != tc.status || identity.refreshToken != "" || recorder.Header().Get("Cache-Control") != "no-store" ||
				len(recorder.Result().Cookies()) > 0 {
				t.Fatalf("unexpected response status %d, any refresh performed = %v", recorder.Code, identity.refreshToken != "")
			}
		})
	}
}

func TestRefreshApplicationWorkbenchAcceptsConfiguredConsoleOrigin(t *testing.T) {
	identity := workbenchRefreshIdentity()
	handler := NewAuthHandler(identity, nil, nil, cfgpkg.AuthConfig{})
	router := gin.New()
	router.Use(apiMiddleware.CORS([]string{"https://web.example"}))
	router.POST("/api/v1/auth/refresh/application-workbench", handler.RefreshApplicationWorkbench)
	request := workbenchRefreshRequest(true, `{"applicationId":"app-1"}`)
	request.Header.Set("Origin", "https://web.example")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || identity.refreshToken != "existing-refresh" {
		t.Fatalf("configured console origin refresh status = %d", recorder.Code)
	}
}
