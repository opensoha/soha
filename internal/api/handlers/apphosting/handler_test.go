package apphosting_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/api/handlers/apphosting"
	"github.com/opensoha/soha/internal/api/middleware"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appapphosting "github.com/opensoha/soha/internal/application/apphosting"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	apphostingrepo "github.com/opensoha/soha/internal/repository/apphosting"
)

func TestHandlerCapabilitiesStayNarrow(t *testing.T) {
	t.Parallel()

	interfaces := map[string]reflect.Type{
		"SettingsService":    reflect.TypeOf((*apphosting.SettingsService)(nil)).Elem(),
		"AppService":         reflect.TypeOf((*apphosting.AppService)(nil)).Elem(),
		"LifecycleService":   reflect.TypeOf((*apphosting.LifecycleService)(nil)).Elem(),
		"DeploymentService":  reflect.TypeOf((*apphosting.DeploymentService)(nil)).Elem(),
		"GatewayAuthService": reflect.TypeOf((*apphosting.GatewayAuthService)(nil)).Elem(),
		"AccessTokenParser":  reflect.TypeOf((*apphosting.AccessTokenParser)(nil)).Elem(),
	}
	for name, iface := range interfaces {
		if got := iface.NumMethod(); got == 0 || got > 7 {
			t.Fatalf("%s has %d methods, want 1..7", name, got)
		}
	}

	handlerType := reflect.TypeOf(apphosting.Handler{})
	for i := 0; i < handlerType.NumField(); i++ {
		field := handlerType.Field(i)
		if field.Type.Kind() == reflect.Interface && field.Type.NumMethod() > 7 {
			t.Fatalf("Handler.%s stores a broad interface with %d methods", field.Name, field.Type.NumMethod())
		}
	}
}

type fakeTokenStore struct {
	tokens map[string]domainidentity.Principal
}

func (f *fakeTokenStore) ParseAccessToken(_ context.Context, token string) (domainidentity.Principal, domainidentity.AccessContext, error) {
	p, ok := f.tokens[token]
	if !ok {
		return domainidentity.Principal{}, domainidentity.AccessContext{}, apperrors.ErrUnauthorized
	}
	if strings.HasPrefix(token, "pat-") {
		return p, domainidentity.AccessContext{TokenKind: "pat"}, nil
	}
	return p, domainidentity.AccessContext{TokenKind: "session_access", SessionID: "sess_" + p.UserID}, nil
}

type staticGrantChecker struct {
	grants map[string][]string
}

func (g *staticGrantChecker) Authorize(_ context.Context, p domainidentity.Principal, perm string) error {
	if slices.Contains(g.grants[p.UserID], perm) {
		return nil
	}
	return apperrors.ErrAccessDenied
}

func (g *staticGrantChecker) AuthorizeAny(ctx context.Context, p domainidentity.Principal, perms ...string) error {
	for _, perm := range perms {
		if g.Authorize(ctx, p, perm) == nil {
			return nil
		}
	}
	return apperrors.ErrAccessDenied
}

type memObjStore struct {
	data map[string][]byte
}

func (m *memObjStore) PutObject(_ context.Context, _, key, _ string, content io.Reader) (int64, string, string, error) {
	b, err := io.ReadAll(content)
	if err != nil {
		return 0, "", "", err
	}
	m.data[key] = b
	sum := strings.Repeat("b", 64)
	return int64(len(b)), sum, `"sha256:` + sum + `"`, nil
}

func (m *memObjStore) DeleteKeys(_ context.Context, _ string, keys []string) error {
	for _, k := range keys {
		delete(m.data, k)
	}
	return nil
}

func buildSimpleTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, content := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	_ = tw.Close()
	_ = gz.Close()
	return buf.Bytes()
}

func TestAppHostingHTTPHandlerFullRoundTrip(t *testing.T) {
	srv, adminClient, aliceClient := setupHandlerRoundTripServer(t)
	defer srv.Close()
	ctx := t.Context()

	if _, err := adminClient.UpdateAppHostingSettings(ctx, sohaapi.AppHostingSettingsUpdateRequest{
		Enabled:                  true,
		AppsDomain:               "apps.local",
		PublicScheme:             "http",
		StorageIntegrationID:     "int_s3",
		MaxAppsPerOwner:          1,
		MaxBundleCompressedBytes: 4096,
	}); err != nil {
		t.Fatalf("UpdateAppHostingSettings: %v", err)
	}
	gwSecret, err := adminClient.RotateAppHostingGatewayToken(ctx, &sohaapi.AppHostingGatewayTokenRotateRequest{Reason: "e2e"})
	if err != nil {
		t.Fatalf("RotateAppHostingGatewayToken: %v", err)
	}

	app := verifyHandlerCreateIdempotencyAndQuota(t, ctx, srv.URL, aliceClient)
	verifyHandlerBundleUploads(t, ctx, srv.URL, aliceClient, app.ID)
	verifyHandlerGatewayAuthFlow(t, ctx, srv.URL, adminClient, app.DNSLabel, gwSecret.Token)
}

func setupHandlerRoundTripServer(t *testing.T) (*httptest.Server, *sohaapi.Client, *sohaapi.Client) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	repo := apphostingrepo.NewMemoryRepository()
	audience := apphostingrepo.NewMemoryAudienceDirectory()
	audience.UpsertTeam(sohaapi.AppHostingAudienceTeam{ID: "team_ops", Name: "Operations", Slug: "ops"})
	audience.UpsertUser(apphostingrepo.MemoryUser{ID: "usr_alice", Username: "alice", DisplayName: "Alice", Department: "Operations", Status: "active", TeamIDs: []string{"team_ops"}})

	perms := &staticGrantChecker{
		grants: map[string][]string{
			"usr_admin": {appaccess.PermAppHostingAdminManage, appaccess.PermAppHostingAdminView},
			"usr_alice": {appaccess.PermAppHostingAppsPublish, appaccess.PermAppHostingAppsView},
		},
	}
	svc, err := appapphosting.New(repo, &memObjStore{data: map[string][]byte{}}, audience, perms, nil)
	if err != nil {
		t.Fatal(err)
	}

	tokenStore := &fakeTokenStore{
		tokens: map[string]domainidentity.Principal{
			"admin-tok":     {UserID: "usr_admin", UserName: "admin"},
			"alice-tok":     {UserID: "usr_alice", UserName: "alice", Teams: []string{"team_ops"}},
			"pat-alice-tok": {UserID: "usr_alice", UserName: "alice", Teams: []string{"team_ops"}},
		},
	}
	handler := apphosting.New(svc, apphosting.WithAccessTokenParser(tokenStore))

	router := gin.New()
	v1 := router.Group("/api/v1")
	v1.GET("/app-hosting/auth/start", handler.StartBrowserAuth)
	v1.POST("/app-hosting/gateway/tickets/exchange", handler.ExchangeGatewayTicket)
	v1.POST("/app-hosting/gateway/access-check", handler.CheckGatewayAccess)

	authed := v1.Group("")
	authed.Use(func(c *gin.Context) {
		auth := c.GetHeader("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			if p, _, parseErr := tokenStore.ParseAccessToken(c.Request.Context(), strings.TrimPrefix(auth, "Bearer ")); parseErr == nil {
				c.Set("principal", p)
			}
		}
		c.Next()
	}, middleware.RequireAuth())
	authed.GET("/app-hosting/settings", handler.GetSettings)
	authed.PUT("/app-hosting/settings", handler.UpdateSettings)
	authed.POST("/app-hosting/settings/gateway-token/rotate", handler.RotateGatewayToken)
	authed.GET("/app-hosting/audience-candidates", handler.ListAudienceCandidates)
	authed.GET("/app-hosting/apps", handler.ListApps)
	authed.POST("/app-hosting/apps", handler.CreateApp)
	authed.GET("/app-hosting/apps/:appId", handler.GetApp)
	authed.PATCH("/app-hosting/apps/:appId", handler.UpdateApp)
	authed.PUT("/app-hosting/apps/:appId/visibility", handler.UpdateVisibility)
	authed.POST("/app-hosting/apps/:appId/offline", handler.TakeOffline)
	authed.POST("/app-hosting/apps/:appId/online", handler.BringOnline)
	authed.POST("/app-hosting/apps/:appId/suspend", handler.SuspendApp)
	authed.POST("/app-hosting/apps/:appId/unsuspend", handler.UnsuspendApp)
	authed.POST("/app-hosting/apps/:appId/deployments/preflight", handler.PreflightDeployment)
	authed.GET("/app-hosting/apps/:appId/revisions", handler.ListRevisions)
	authed.POST("/app-hosting/apps/:appId/revisions", handler.UploadRevision)
	authed.GET("/app-hosting/apps/:appId/revisions/:revisionId", handler.GetRevision)
	authed.POST("/app-hosting/apps/:appId/rollback", handler.Rollback)

	srv := httptest.NewServer(router)
	return srv,
		sohaapi.NewClient(srv.URL, sohaapi.WithBearerToken("admin-tok")),
		sohaapi.NewClient(srv.URL, sohaapi.WithBearerToken("alice-tok"))
}

func verifyHandlerCreateIdempotencyAndQuota(t *testing.T, ctx context.Context, baseURL string, aliceClient *sohaapi.Client) sohaapi.HostedApp {
	t.Helper()
	app, err := aliceClient.CreateHostedApp(ctx, sohaapi.CreateHostedAppRequest{Name: "Ops Board", IdempotencyKey: "k1"})
	if err != nil {
		t.Fatalf("CreateHostedApp: %v", err)
	}
	assertAuthedPostStatus(t, ctx, baseURL+"/api/v1/app-hosting/apps", "alice-tok", "application/json", []byte(`{"name":"Ops Board","idempotencyKey":"k1"}`), http.StatusOK)
	assertAuthedPostStatus(t, ctx, baseURL+"/api/v1/app-hosting/apps", "alice-tok", "application/json", []byte(`{"name":"Second App"}`), http.StatusTooManyRequests)
	return app
}

func verifyHandlerBundleUploads(t *testing.T, ctx context.Context, baseURL string, aliceClient *sohaapi.Client, appID string) {
	t.Helper()
	badBundle := buildSimpleTarGz(t, map[string]string{".env": "SECRET=1", "index.html": "ok"})
	assertAuthedPostStatus(t, ctx, baseURL+"/api/v1/app-hosting/apps/"+appID+"/revisions", "alice-tok", "application/gzip", badBundle, http.StatusUnprocessableEntity)
	assertAuthedPostStatus(t, ctx, baseURL+"/api/v1/app-hosting/apps/"+appID+"/revisions", "alice-tok", "application/gzip", bytes.Repeat([]byte("z"), 5000), http.StatusRequestEntityTooLarge)

	goodBundle := buildSimpleTarGz(t, map[string]string{"index.html": "<!doctype html><h1>ok</h1>"})
	if _, err := aliceClient.UploadHostedAppRevision(ctx, appID, bytes.NewReader(goodBundle), sohaapi.UploadHostedAppRevisionParams{Description: "v1"}); err != nil {
		t.Fatalf("UploadHostedAppRevision: %v", err)
	}
}

func assertAuthedPostStatus(t *testing.T, ctx context.Context, targetURL, bearer, contentType string, body []byte, wantStatus int) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Content-Type", contentType)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = res.Body.Close()
	if res.StatusCode != wantStatus {
		t.Fatalf("POST %s status = %d, want %d", targetURL, res.StatusCode, wantStatus)
	}
}

func verifyHandlerGatewayAuthFlow(t *testing.T, ctx context.Context, baseURL string, adminClient *sohaapi.Client, dnsLabel, gwToken string) {
	t.Helper()
	noRedirectClient := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	authStartURL := baseURL + "/api/v1/app-hosting/auth/start?host=" + url.QueryEscape(dnsLabel+".apps.local:18090") + "&returnPath=%2Fitems%3Fp%3D2&gatewayState=st1"

	unauthRes, err := noRedirectClient.Get(authStartURL)
	if err != nil {
		t.Fatal(err)
	}
	_ = unauthRes.Body.Close()
	if unauthRes.StatusCode != http.StatusFound || !strings.HasPrefix(unauthRes.Header.Get("Location"), "/login?return_to=") {
		t.Fatalf("unauthenticated auth/start = %d %q", unauthRes.StatusCode, unauthRes.Header.Get("Location"))
	}

	patReq, err := http.NewRequestWithContext(ctx, http.MethodGet, authStartURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	patReq.AddCookie(&http.Cookie{Name: middleware.ProtocolAccessCookieName, Value: "pat-alice-tok", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	patRes, err := noRedirectClient.Do(patReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = patRes.Body.Close()
	if patRes.StatusCode != http.StatusForbidden {
		t.Fatalf("PAT auth/start status = %d, want 403", patRes.StatusCode)
	}

	cookieReq, err := http.NewRequestWithContext(ctx, http.MethodGet, authStartURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	cookieReq.AddCookie(&http.Cookie{Name: middleware.ProtocolAccessCookieName, Value: "alice-tok", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode})
	authedRes, err := noRedirectClient.Do(cookieReq)
	if err != nil {
		t.Fatal(err)
	}
	_ = authedRes.Body.Close()
	loc := authedRes.Header.Get("Location")
	parsedLoc, err := url.Parse(loc)
	if authedRes.StatusCode != http.StatusFound || err != nil || parsedLoc.Host != dnsLabel+".apps.local:18090" {
		t.Fatalf("authenticated auth/start = %d %q (%v)", authedRes.StatusCode, loc, err)
	}

	exchanged, err := adminClient.ExchangeHostedAppGatewayTicket(ctx, sohaapi.HostedAppTicketExchangeRequest{
		Host:         dnsLabel + ".apps.local:18090",
		Ticket:       parsedLoc.Query().Get("ticket"),
		GatewayState: "st1",
	}, gwToken)
	if err != nil || exchanged.ReturnPath != "/items?p=2" {
		t.Fatalf("ExchangeHostedAppGatewayTicket = %#v, %v", exchanged, err)
	}
	decision, err := adminClient.CheckHostedAppGatewayAccess(ctx, sohaapi.HostedAppGatewayAccessCheckRequest{
		Host:      dnsLabel + ".apps.local:18090",
		UserID:    "usr_alice",
		SessionID: exchanged.SessionID,
	}, gwToken)
	if err != nil || decision.Decision != sohaapi.HostedAppAccessDecisionKindAllow {
		t.Fatalf("CheckHostedAppGatewayAccess = %#v, %v", decision, err)
	}
}
