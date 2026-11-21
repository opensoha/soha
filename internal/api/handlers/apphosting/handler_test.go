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
		"ApprovalService":    reflect.TypeOf((*apphosting.ApprovalService)(nil)).Elem(),
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

type fakeWorkspaceBundleSource struct {
	bundle []byte
}

func (f *fakeWorkspaceBundleSource) BundlePersonalWorkspace(_ context.Context, _ domainidentity.Principal, _, _ string, _ int64) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(f.bundle)), nil
}

func (f *fakeWorkspaceBundleSource) BundleConversationArtifact(_ context.Context, _ domainidentity.Principal, _, _, _ string) (io.ReadCloser, string, error) {
	return io.NopCloser(bytes.NewReader(f.bundle)), "Artifact Board", nil
}

func setupHandlerRoundTripServer(t *testing.T) (*httptest.Server, *sohaapi.Client, *sohaapi.Client) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	repo := apphostingrepo.NewMemoryRepository()
	audience := apphostingrepo.NewMemoryAudienceDirectory()
	audience.UpsertTeam(sohaapi.AppHostingAudienceTeam{ID: "team_ops", Name: "Operations", Slug: "ops"})
	audience.UpsertUser(apphostingrepo.MemoryUser{ID: "usr_admin", Username: "admin", DisplayName: "Admin", Status: "active", TeamIDs: []string{"team_ops"}})
	audience.UpsertUser(apphostingrepo.MemoryUser{ID: "usr_alice", Username: "alice", DisplayName: "Alice", Department: "Operations", Status: "active", TeamIDs: []string{"team_ops"}})
	audience.UpsertUser(apphostingrepo.MemoryUser{ID: "usr_bob", Username: "bob", DisplayName: "Bob", Department: "Operations", Status: "active", TeamIDs: []string{"team_ops"}})
	audience.UpsertUser(apphostingrepo.MemoryUser{ID: "usr_emp", Username: "emp", DisplayName: "Employee", Department: "Operations", Status: "active", TeamIDs: []string{"team_ops"}})

	perms := &staticGrantChecker{
		grants: map[string][]string{
			"usr_admin": {appaccess.PermAppHostingAdminManage, appaccess.PermAppHostingAdminView, appaccess.PermIdentityPortalView},
			"usr_alice": {appaccess.PermAppHostingAppsPublish, appaccess.PermAppHostingAppsView, appaccess.PermObserveAIChatUse, appaccess.PermIdentityPortalView},
			"usr_bob":   {appaccess.PermAppHostingAppsPublish, appaccess.PermAppHostingAppsView, appaccess.PermIdentityPortalView},
			"usr_emp":   {appaccess.PermIdentityPortalView},
		},
	}
	wsSrc := &fakeWorkspaceBundleSource{
		bundle: buildSimpleTarGz(t, map[string]string{"index.html": "<!doctype html><h1>workspace ok</h1>"}),
	}
	svc, err := appapphosting.New(repo, &memObjStore{data: map[string][]byte{}}, audience, perms, nil, appapphosting.WithWorkspaceBundleSource(wsSrc))
	if err != nil {
		t.Fatal(err)
	}

	tokenStore := &fakeTokenStore{
		tokens: map[string]domainidentity.Principal{
			"admin-tok":     {UserID: "usr_admin", UserName: "admin"},
			"alice-tok":     {UserID: "usr_alice", UserName: "alice", Teams: []string{"team_ops"}},
			"bob-tok":       {UserID: "usr_bob", UserName: "bob", Teams: []string{"team_ops"}},
			"emp-tok":       {UserID: "usr_emp", UserName: "emp", Teams: []string{"team_ops"}},
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
	authed.GET("/app-hosting/portal/apps", handler.ListPortalApps)
	authed.GET("/app-hosting/apps", handler.ListApps)
	authed.POST("/app-hosting/apps", handler.CreateApp)
	authed.GET("/app-hosting/apps/:appId", handler.GetApp)
	authed.PATCH("/app-hosting/apps/:appId", handler.UpdateApp)
	authed.PUT("/app-hosting/apps/:appId/visibility", handler.UpdateVisibility)
	authed.PUT("/app-hosting/apps/:appId/maintainers", handler.UpdateMaintainers)
	authed.POST("/app-hosting/apps/:appId/transfer", handler.TransferOwnership)
	authed.POST("/app-hosting/apps/:appId/delete", handler.DeleteApp)
	authed.POST("/app-hosting/apps/:appId/offline", handler.TakeOffline)
	authed.POST("/app-hosting/apps/:appId/online", handler.BringOnline)
	authed.POST("/app-hosting/apps/:appId/suspend", handler.SuspendApp)
	authed.POST("/app-hosting/apps/:appId/unsuspend", handler.UnsuspendApp)
	authed.POST("/app-hosting/apps/:appId/visibility-approval/withdraw", handler.WithdrawVisibilityApproval)
	authed.GET("/app-hosting/approvals", handler.ListVisibilityApprovals)
	authed.POST("/app-hosting/approvals/:approvalId/approve", handler.ApproveVisibilityApproval)
	authed.POST("/app-hosting/approvals/:approvalId/reject", handler.RejectVisibilityApproval)
	authed.POST("/app-hosting/workspace-deployments", handler.DeployFromWorkspace)
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

func TestAppHostingHTTPHandlerB2RoundTrip(t *testing.T) {
	srv, adminClient, aliceClient := setupHandlerRoundTripServer(t)
	defer srv.Close()
	ctx := t.Context()
	bobClient := sohaapi.NewClient(srv.URL, sohaapi.WithBearerToken("bob-tok"))
	empClient := sohaapi.NewClient(srv.URL, sohaapi.WithBearerToken("emp-tok"))

	if _, err := adminClient.UpdateAppHostingSettings(ctx, sohaapi.AppHostingSettingsUpdateRequest{
		Enabled:                      true,
		AppsDomain:                   "apps.local",
		PublicScheme:                 "http",
		StorageIntegrationID:         "int_s3",
		MaxAppsPerOwner:              5,
		MaxBundleCompressedBytes:     8192,
		OrganizationVisibilityPolicy: sohaapi.AppHostingOrganizationVisibilityPolicyRequireApproval,
	}); err != nil {
		t.Fatalf("UpdateAppHostingSettings B2: %v", err)
	}

	appID := verifyHandlerB2WorkspaceAndApprovalFlow(t, ctx, srv.URL, adminClient, aliceClient)
	verifyHandlerB2PortalTransferAndDelete(t, ctx, appID, adminClient, aliceClient, bobClient, empClient)
}

func verifyHandlerB2WorkspaceAndApprovalFlow(t *testing.T, ctx context.Context, baseURL string, adminClient, aliceClient *sohaapi.Client) string {
	t.Helper()
	wsPayload := []byte(`{"idempotencyKey":"ws-http-1","name":"OpsHelper","visibility":{"mode":"organization"},"visibilityReason":"company tool","source":{"kind":"personal_environment","sourcePath":"dist"}}`)
	assertAuthedPostStatus(t, ctx, baseURL+"/api/v1/app-hosting/workspace-deployments", "alice-tok", "application/json", wsPayload, http.StatusCreated)
	assertAuthedPostStatus(t, ctx, baseURL+"/api/v1/app-hosting/workspace-deployments", "alice-tok", "application/json", wsPayload, http.StatusOK)

	apps, err := aliceClient.ListHostedApps(ctx, sohaapi.ListHostedAppsParams{Scope: sohaapi.HostedAppListScopeMine})
	if err != nil || apps.Total != 1 || apps.Items[0].PendingVisibilityApproval == nil {
		t.Fatalf("ListHostedApps = %#v, %v", apps, err)
	}
	appID := apps.Items[0].ID

	if _, err := aliceClient.WithdrawHostedAppVisibilityApproval(ctx, appID, &sohaapi.HostedAppLifecycleActionRequest{Reason: "update copy"}); err != nil {
		t.Fatalf("WithdrawHostedAppVisibilityApproval: %v", err)
	}
	reReq, err := aliceClient.UpdateHostedAppVisibility(ctx, appID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeOrganization},
		Reason:     "v1 ready",
	})
	if err != nil || reReq.PendingVisibilityApproval == nil {
		t.Fatalf("UpdateHostedAppVisibility: %#v, %v", reReq, err)
	}
	if _, err := adminClient.RejectHostedAppVisibility(ctx, reReq.PendingVisibilityApproval.ID, sohaapi.RejectHostedAppVisibilityRequest{Comment: "needs owner contact"}); err != nil {
		t.Fatalf("RejectHostedAppVisibility: %v", err)
	}
	reReq2, err := aliceClient.UpdateHostedAppVisibility(ctx, appID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeOrganization},
		Reason:     "added contact",
	})
	if err != nil || reReq2.PendingVisibilityApproval == nil {
		t.Fatalf("second UpdateHostedAppVisibility: %#v, %v", reReq2, err)
	}
	approvals, err := adminClient.ListHostedAppVisibilityApprovals(ctx, sohaapi.ListHostedAppVisibilityApprovalsParams{
		Scope:  sohaapi.HostedAppApprovalListScopeAll,
		Status: sohaapi.HostedAppVisibilityApprovalStatusPending,
	})
	if err != nil || approvals.Total != 1 {
		t.Fatalf("ListHostedAppVisibilityApprovals = %#v, %v", approvals, err)
	}
	approved, err := adminClient.ApproveHostedAppVisibility(ctx, reReq2.PendingVisibilityApproval.ID, &sohaapi.ApproveHostedAppVisibilityRequest{Comment: "approved"})
	if err != nil || approved.App.Visibility.Mode != sohaapi.HostedAppVisibilityModeOrganization {
		t.Fatalf("ApproveHostedAppVisibility = %#v, %v", approved, err)
	}
	return appID
}

func verifyHandlerB2PortalTransferAndDelete(t *testing.T, ctx context.Context, appID string, adminClient, aliceClient, bobClient, empClient *sohaapi.Client) {
	t.Helper()
	portal, err := empClient.ListPortalHostedApps(ctx, sohaapi.ListPortalHostedAppsParams{Query: "opshelper"})
	if err != nil || portal.Total != 1 || portal.Items[0].ID != appID {
		t.Fatalf("ListPortalHostedApps = %#v, %v", portal, err)
	}
	if _, err := aliceClient.UpdateHostedAppMaintainers(ctx, appID, sohaapi.UpdateHostedAppMaintainersRequest{MaintainerUserIDs: []string{"usr_bob"}}); err != nil {
		t.Fatalf("UpdateHostedAppMaintainers: %v", err)
	}
	transferred, err := adminClient.TransferHostedAppOwnership(ctx, appID, sohaapi.TransferHostedAppOwnershipRequest{
		NewOwnerUserID: "usr_bob",
		Reason:         "handoff",
	})
	if err != nil || transferred.OwnerUserID != "usr_bob" {
		t.Fatalf("TransferHostedAppOwnership = %#v, %v", transferred, err)
	}
	delRes, err := bobClient.DeleteHostedApp(ctx, appID, sohaapi.DeleteHostedAppRequest{
		ConfirmationName: "OpsHelper",
		Reason:           "cleanup",
	})
	if err != nil || !delRes.Deleted {
		t.Fatalf("DeleteHostedApp = %#v, %v", delRes, err)
	}
}
