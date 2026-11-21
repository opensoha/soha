package appgateway_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"path"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	apphostinghandler "github.com/opensoha/soha/internal/api/handlers/apphosting"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	"github.com/opensoha/soha/internal/appgateway"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appapphosting "github.com/opensoha/soha/internal/application/apphosting"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	cfgpkg "github.com/opensoha/soha/internal/infrastructure/config"
	"github.com/opensoha/soha/internal/platform/apperrors"
	apphostingrepo "github.com/opensoha/soha/internal/repository/apphosting"
)

type sharedMemoryBucket struct {
	mu      sync.RWMutex
	objects map[string]storedObject
}

type storedObject struct {
	body        []byte
	contentType string
	etag        string
}

func newSharedMemoryBucket() *sharedMemoryBucket {
	return &sharedMemoryBucket{objects: make(map[string]storedObject)}
}

func (b *sharedMemoryBucket) PutObject(_ context.Context, integrationID, key, contentType string, content io.Reader) (int64, string, string, error) {
	if integrationID != "minio-primary" {
		return 0, "", "", apperrors.ErrNotFound
	}
	raw, err := io.ReadAll(content)
	if err != nil {
		return 0, "", "", err
	}
	sum := sha256.Sum256(raw)
	hexDigest := hex.EncodeToString(sum[:])
	etag := `"sha256:` + hexDigest + `"`
	cleanKey := strings.Trim(path.Clean("/"+key), "/")

	b.mu.Lock()
	b.objects[cleanKey] = storedObject{
		body:        bytes.Clone(raw),
		contentType: contentType,
		etag:        etag,
	}
	b.mu.Unlock()
	return int64(len(raw)), hexDigest, etag, nil
}

func (b *sharedMemoryBucket) DeleteKeys(_ context.Context, _ string, keys []string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, k := range keys {
		cleanKey := strings.Trim(path.Clean("/"+k), "/")
		delete(b.objects, cleanKey)
	}
	return nil
}

func (b *sharedMemoryBucket) GetObject(_ context.Context, key string) (*appgateway.ObjectStream, error) {
	cleanKey := strings.Trim(path.Clean("/"+key), "/")
	b.mu.RLock()
	obj, ok := b.objects[cleanKey]
	b.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("object %q not found", cleanKey)
	}
	return &appgateway.ObjectStream{
		Body:          io.NopCloser(bytes.NewReader(obj.body)),
		ContentLength: int64(len(obj.body)),
		ContentType:   obj.contentType,
		ETag:          obj.etag,
	}, nil
}

type stubTokenParser struct {
	tokens map[string]domainidentity.Principal
}

func (p *stubTokenParser) ParseAccessToken(_ context.Context, token string) (domainidentity.Principal, domainidentity.AccessContext, error) {
	trimmed := strings.TrimSpace(token)
	principal, ok := p.tokens[trimmed]
	if !ok {
		return domainidentity.Principal{}, domainidentity.AccessContext{}, errors.New("invalid token")
	}
	if strings.HasPrefix(trimmed, "pat-") {
		return principal, domainidentity.AccessContext{TokenKind: "pat"}, nil
	}
	return principal, domainidentity.AccessContext{
		TokenKind: "session_access",
		SessionID: "sess-" + principal.UserID,
	}, nil
}

type e2eSessionStore struct {
	mu      sync.Mutex
	revoked map[string]bool
}

func (s *e2eSessionStore) Revoke(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revoked[sessionID] = true
}

func (s *e2eSessionStore) ValidateBrowserSession(_ context.Context, sessionID, expectedUserID string) (appapphosting.BrowserSessionState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.revoked[sessionID] || sessionID != "sess-"+expectedUserID {
		return appapphosting.BrowserSessionState{Active: false}, nil
	}
	return appapphosting.BrowserSessionState{
		Active:    true,
		UserID:    expectedUserID,
		ExpiresAt: time.Now().UTC().Add(4 * time.Hour),
	}, nil
}

type stubRolePermChecker struct{}

func (stubRolePermChecker) Authorize(_ context.Context, principal domainidentity.Principal, permissionKey string) error {
	if appaccess.HasPermission(principal.Roles, permissionKey) {
		return nil
	}
	return apperrors.ErrAccessDenied
}

func (stubRolePermChecker) AuthorizeAny(_ context.Context, principal domainidentity.Principal, permissionKeys ...string) error {
	for _, key := range permissionKeys {
		if appaccess.HasPermission(principal.Roles, key) {
			return nil
		}
	}
	return apperrors.ErrAccessDenied
}

type auditCapture struct {
	mu      sync.Mutex
	entries []domainaudit.Entry
}

func (a *auditCapture) Record(_ context.Context, entry domainaudit.Entry) error {
	a.mu.Lock()
	a.entries = append(a.entries, entry)
	a.mu.Unlock()
	return nil
}

type e2eEnv struct {
	coreServer   *httptest.Server
	gatewayHTTP  *httptest.Server
	gatewayHTTP2 *httptest.Server
	coreURL      *url.URL
	gwURL        *url.URL
	gwURL2       *url.URL
	gwPort       string
	bucket       *sharedMemoryBucket
	svc          *appapphosting.Service
	audience     *apphostingrepo.MemoryAudienceDirectory
	sessions     *e2eSessionStore
	adminClient  *sohaapi.Client
	aliceClient  *sohaapi.Client
}

func setupE2EEnv(t *testing.T) *e2eEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	appaccess.SetRolePermissionMatrix(nil)

	bucket := newSharedMemoryBucket()
	repo := apphostingrepo.NewMemoryRepository()
	audience := seedE2EAudience(apphostingrepo.NewMemoryAudienceDirectory())
	sessions := &e2eSessionStore{revoked: make(map[string]bool)}
	parser := newE2ETokenParser()
	svc, err := appapphosting.New(
		repo,
		bucket,
		audience,
		stubRolePermChecker{},
		&auditCapture{},
		appapphosting.WithSessionValidator(sessions),
		appapphosting.WithVisitorIDSecret("e2e-pairwise-visitor-secret-key-01"),
	)
	if err != nil {
		t.Fatalf("new apphosting service: %v", err)
	}

	coreServer := httptest.NewServer(buildE2ECoreRouter(svc, parser))
	t.Cleanup(coreServer.Close)
	coreURL, err := url.Parse(coreServer.URL)
	if err != nil {
		t.Fatalf("parse core URL: %v", err)
	}

	adminClient := sohaapi.NewClient(coreServer.URL+"/api/v1", sohaapi.WithBearerToken("tok-admin"))
	aliceClient := sohaapi.NewClient(coreServer.URL+"/api/v1", sohaapi.WithBearerToken("tok-alice"))
	gwToken := configureE2ESettingsAndToken(t, adminClient)

	gwCfg := appgateway.Config{
		ListenAddr:    ":8090",
		HealthAddr:    ":8091",
		AppsDomain:    "apps.local",
		PublicScheme:  "http",
		CoreURL:       coreServer.URL + "/api/v1",
		PublicCoreURL: coreServer.URL,
		Token:         gwToken,
		CookieSecret:  "0123456789abcdef0123456789abcdef-cookie-secret",
		S3Prefix:      "hosted-apps",
	}
	gwSrv1, err := appgateway.NewServer(gwCfg, appgateway.WithObjectReader(bucket))
	if err != nil {
		t.Fatalf("NewServer app-gateway replica 1: %v", err)
	}
	gwSrv2, err := appgateway.NewServer(gwCfg, appgateway.WithObjectReader(bucket))
	if err != nil {
		t.Fatalf("NewServer app-gateway replica 2: %v", err)
	}
	gatewayHTTP := httptest.NewServer(gwSrv1.Handler())
	t.Cleanup(gatewayHTTP.Close)
	gatewayHTTP2 := httptest.NewServer(gwSrv2.Handler())
	t.Cleanup(gatewayHTTP2.Close)
	gwURL, _ := url.Parse(gatewayHTTP.URL)
	gwURL2, _ := url.Parse(gatewayHTTP2.URL)
	_, gwPort, _ := strings.Cut(gwURL.Host, ":")

	return &e2eEnv{
		coreServer:   coreServer,
		gatewayHTTP:  gatewayHTTP,
		gatewayHTTP2: gatewayHTTP2,
		coreURL:      coreURL,
		gwURL:        gwURL,
		gwURL2:       gwURL2,
		gwPort:       gwPort,
		bucket:       bucket,
		svc:          svc,
		audience:     audience,
		sessions:     sessions,
		adminClient:  adminClient,
		aliceClient:  aliceClient,
	}
}

func seedE2EAudience(audience *apphostingrepo.MemoryAudienceDirectory) *apphostingrepo.MemoryAudienceDirectory {
	audience.UpsertTeam(sohaapi.AppHostingAudienceTeam{ID: "team-eng", Name: "研发工程部", Slug: "eng"})
	audience.UpsertTeam(sohaapi.AppHostingAudienceTeam{ID: "team-data", Name: "数据组", Slug: "data"})
	audience.UpsertTeam(sohaapi.AppHostingAudienceTeam{ID: "team-sales", Name: "销售组", Slug: "sales"})
	audience.UpsertUser(apphostingrepo.MemoryUser{ID: "user-admin", Username: "admin", DisplayName: "平台管理员", Email: "admin@example.com", Status: "active", Roles: []string{"admin"}, TeamIDs: []string{"team-eng"}})
	audience.UpsertUser(apphostingrepo.MemoryUser{ID: "user-admin2", Username: "admin2", DisplayName: "安全审批管理员", Email: "admin2@example.com", Status: "active", Roles: []string{"admin"}, TeamIDs: []string{"team-eng"}})
	audience.UpsertUser(apphostingrepo.MemoryUser{ID: "user-alice", Username: "alice", DisplayName: "张敏", Email: "alice@example.com", Department: "研发工程部", Status: "active", Roles: []string{"developer"}, TeamIDs: []string{"team-eng"}})
	audience.UpsertUser(apphostingrepo.MemoryUser{ID: "user-pub", Username: "lin", DisplayName: "林悦", Email: "lin@example.com", Department: "销售组", Status: "active", Roles: []string{"portal-publisher"}, TeamIDs: []string{"team-sales"}})
	audience.UpsertUser(apphostingrepo.MemoryUser{ID: "user-collab", Username: "zhou", DisplayName: "周凯", Email: "zhou@example.com", Department: "研发工程部", Status: "active", Roles: []string{"portal-publisher"}, TeamIDs: []string{"team-eng"}})
	audience.UpsertUser(apphostingrepo.MemoryUser{ID: "user-bob", Username: "bob", DisplayName: "李博", Email: "bob@example.com", Department: "数据组", Status: "active", Roles: []string{"readonly"}, TeamIDs: []string{"team-data"}})
	audience.UpsertUser(apphostingrepo.MemoryUser{ID: "user-dave", Username: "dave", DisplayName: "陈达", Email: "dave@example.com", Department: "数据组", Status: "active", Roles: []string{"readonly"}, TeamIDs: []string{"team-data"}})
	audience.UpsertUser(apphostingrepo.MemoryUser{ID: "user-eve", Username: "eve", DisplayName: "王芳", Email: "eve@example.com", Department: "销售组", Status: "active", Roles: []string{"readonly"}, TeamIDs: []string{"team-sales"}})
	return audience
}

func newE2ETokenParser() *stubTokenParser {
	return &stubTokenParser{
		tokens: map[string]domainidentity.Principal{
			"tok-admin":  {UserID: "user-admin", UserName: "admin", Email: "admin@example.com", Roles: []string{"admin"}, Teams: []string{"team-eng"}},
			"tok-admin2": {UserID: "user-admin2", UserName: "admin2", Email: "admin2@example.com", Roles: []string{"admin"}, Teams: []string{"team-eng"}},
			"tok-alice":  {UserID: "user-alice", UserName: "alice", Email: "alice@example.com", Roles: []string{"developer"}, Teams: []string{"team-eng"}},
			"tok-pub":    {UserID: "user-pub", UserName: "lin", Email: "lin@example.com", Roles: []string{"portal-publisher"}, Teams: []string{"team-sales"}},
			"tok-collab": {UserID: "user-collab", UserName: "zhou", Email: "zhou@example.com", Roles: []string{"portal-publisher"}, Teams: []string{"team-eng"}},
			"tok-bob":    {UserID: "user-bob", UserName: "bob", Email: "bob@example.com", Roles: []string{"readonly"}, Teams: []string{"team-data"}},
			"tok-dave":   {UserID: "user-dave", UserName: "dave", Email: "dave@example.com", Roles: []string{"readonly"}, Teams: []string{"team-data"}},
			"tok-eve":    {UserID: "user-eve", UserName: "eve", Email: "eve@example.com", Roles: []string{"readonly"}, Teams: []string{"team-sales"}},
		},
	}
}

func buildE2ECoreRouter(svc *appapphosting.Service, parser *stubTokenParser) *gin.Engine {
	handler := apphostinghandler.New(svc, apphostinghandler.WithAccessTokenParser(parser))
	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(apiMiddleware.BuildPrincipalMiddleware(cfgpkg.AuthConfig{}, parser, "/api/v1"))

	v1Public := engine.Group("/api/v1")
	v1Public.GET("/readyz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	v1Public.GET("/app-hosting/auth/start", handler.StartBrowserAuth)
	v1Public.POST("/app-hosting/gateway/tickets/exchange", handler.ExchangeGatewayTicket)
	v1Public.POST("/app-hosting/gateway/access-check", handler.CheckGatewayAccess)

	v1Protected := engine.Group("/api/v1")
	v1Protected.Use(apiMiddleware.RequireAuth())
	v1Protected.GET("/app-hosting/settings", handler.GetSettings)
	v1Protected.PUT("/app-hosting/settings", handler.UpdateSettings)
	v1Protected.POST("/app-hosting/settings/gateway-token/rotate", handler.RotateGatewayToken)
	v1Protected.GET("/app-hosting/audience-candidates", handler.ListAudienceCandidates)
	v1Protected.GET("/app-hosting/portal/apps", handler.ListPortalApps)
	v1Protected.GET("/app-hosting/apps", handler.ListApps)
	v1Protected.POST("/app-hosting/apps", handler.CreateApp)
	v1Protected.GET("/app-hosting/apps/:appId", handler.GetApp)
	v1Protected.PATCH("/app-hosting/apps/:appId", handler.UpdateApp)
	v1Protected.PUT("/app-hosting/apps/:appId/visibility", handler.UpdateVisibility)
	v1Protected.POST("/app-hosting/apps/:appId/visibility-approval/withdraw", handler.WithdrawVisibilityApproval)
	v1Protected.PUT("/app-hosting/apps/:appId/maintainers", handler.UpdateMaintainers)
	v1Protected.POST("/app-hosting/apps/:appId/transfer", handler.TransferOwnership)
	v1Protected.POST("/app-hosting/apps/:appId/delete", handler.DeleteApp)
	v1Protected.POST("/app-hosting/apps/:appId/offline", handler.TakeOffline)
	v1Protected.POST("/app-hosting/apps/:appId/online", handler.BringOnline)
	v1Protected.POST("/app-hosting/apps/:appId/suspend", handler.SuspendApp)
	v1Protected.POST("/app-hosting/apps/:appId/unsuspend", handler.UnsuspendApp)
	v1Protected.GET("/app-hosting/approvals", handler.ListVisibilityApprovals)
	v1Protected.POST("/app-hosting/approvals/:approvalId/approve", handler.ApproveVisibilityApproval)
	v1Protected.POST("/app-hosting/approvals/:approvalId/reject", handler.RejectVisibilityApproval)
	v1Protected.POST("/app-hosting/workspace-deployments", handler.DeployFromWorkspace)
	v1Protected.POST("/app-hosting/apps/:appId/deployments/preflight", handler.PreflightDeployment)
	v1Protected.GET("/app-hosting/apps/:appId/revisions", handler.ListRevisions)
	v1Protected.POST("/app-hosting/apps/:appId/revisions", handler.UploadRevision)
	v1Protected.GET("/app-hosting/apps/:appId/revisions/:revisionId", handler.GetRevision)
	v1Protected.POST("/app-hosting/apps/:appId/rollback", handler.Rollback)
	return engine
}

func configureE2ESettingsAndToken(t *testing.T, adminClient *sohaapi.Client) string {
	t.Helper()
	ctx := context.Background()
	allowOrg := true
	_, err := adminClient.UpdateAppHostingSettings(ctx, sohaapi.AppHostingSettingsUpdateRequest{
		Enabled:                     true,
		AppsDomain:                  "apps.local",
		PublicScheme:                "http",
		StorageIntegrationID:        "minio-primary",
		StoragePrefix:               "hosted-apps",
		AllowOrganizationVisibility: &allowOrg,
		MaxBundleCompressedBytes:    10 << 20,
		MaxBundleUncompressedBytes:  25 << 20,
		MaxBundleFileCount:          200,
		MaxAppsPerOwner:             10,
		RetainedRevisionsPerApp:     5,
	})
	if err != nil {
		t.Fatalf("UpdateAppHostingSettings: %v", err)
	}
	tokenSecret, err := adminClient.RotateAppHostingGatewayToken(ctx, &sohaapi.AppHostingGatewayTokenRotateRequest{Reason: "b1-e2e-bootstrap"})
	if err != nil {
		t.Fatalf("RotateAppHostingGatewayToken: %v", err)
	}
	return tokenSecret.Token
}

func (e *e2eEnv) newBrowserClient(t *testing.T, jar http.CookieJar) *http.Client {
	t.Helper()
	return e.newBrowserClientForGateway(t, jar, e.gwURL)
}

func (e *e2eEnv) newBrowserClientForGateway(t *testing.T, jar http.CookieJar, targetGW *url.URL) *http.Client {
	t.Helper()
	stdTransport, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatal("default transport is not *http.Transport")
	}
	baseTransport := stdTransport.Clone()
	return &http.Client{
		Jar: jar,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Transport: roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			cloned := req.Clone(req.Context())
			if strings.HasSuffix(strings.ToLower(req.URL.Host), ".apps.local:"+e.gwPort) {
				cloned.Host = req.URL.Host
				cloned.URL.Host = targetGW.Host
				cloned.URL.Scheme = "http"
			}
			return baseTransport.RoundTrip(cloned)
		}),
	}
}

func seedCoreLoginCookie(jar http.CookieJar, coreURL *url.URL, token string) {
	secure := coreURL.Scheme == "https"
	ck := &http.Cookie{
		Name:     apiMiddleware.ProtocolAccessCookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	} // #nosec G124 -- loopback HTTP test server uses scheme-matched Secure flag
	jar.SetCookies(coreURL, []*http.Cookie{ck})
}

func TestB1WorkspaceAppHostingEndToEndFlow(t *testing.T) {
	env := setupE2EEnv(t)
	ctx := context.Background()

	createdApp, v1RevID := publishAppAndVerifyDLPAndIdempotency(t, ctx, env)
	appHost := createdApp.DNSLabel + ".apps.local:" + env.gwPort

	bobHTTP, authStartLoc := verifyBobCleanBrowserSSOAndDeepLink(t, env, appHost)
	etag := verifyBobStaticAssetsAndVisitorIdentity(t, bobHTTP, appHost)
	verifyUnauthorizedEveBlocked(t, env, authStartLoc)
	verifyTeamAndPrivateVisibilityRevocations(t, ctx, env, bobHTTP, createdApp.ID, appHost, etag)
	verifyV2DeployRollbackAndAdminSuspension(t, ctx, env, bobHTTP, createdApp.ID, v1RevID, appHost)
}

func publishAppAndVerifyDLPAndIdempotency(t *testing.T, ctx context.Context, env *e2eEnv) (sohaapi.HostedApp, string) {
	t.Helper()
	candidates, err := env.aliceClient.ListAppHostingAudienceCandidates(ctx, sohaapi.ListAppHostingAudienceCandidatesParams{Query: "数据"})
	if err != nil || len(candidates.Teams) != 1 || candidates.Teams[0].ID != "team-data" {
		t.Fatalf("ListAppHostingAudienceCandidates = (%#v, %v), want team-data", candidates.Teams, err)
	}

	spaTrue := true
	createdApp, err := env.aliceClient.CreateHostedApp(ctx, sohaapi.CreateHostedAppRequest{
		Name:        "季度经营看板",
		Slug:        "q4-board",
		Description: "供数据组查看的经营分析",
		Kind:        sohaapi.HostedAppKindStatic,
		SpaFallback: &spaTrue,
		Visibility: &sohaapi.HostedAppVisibilityInput{
			Mode:    sohaapi.HostedAppVisibilityModeSelected,
			TeamIDs: []string{"team-data"},
		},
		IdempotencyKey: "create-q4-board-1",
	})
	if err != nil {
		t.Fatalf("CreateHostedApp: %v", err)
	}

	syntheticAWSKey := "AKIA" + "IOSFODNN7EXAMPLE"
	leakyBundle := buildTarGzBundle(t, map[string]string{
		"index.html": "<html><body>leak " + syntheticAWSKey + "</body></html>",
	})
	if _, err := env.aliceClient.UploadHostedAppRevision(ctx, createdApp.ID, bytes.NewReader(leakyBundle), sohaapi.UploadHostedAppRevisionParams{IdempotencyKey: "deploy-leak-1"}); err == nil {
		t.Fatal("expected DLP rejection for bundle containing AWS key")
	}

	v1Bundle := buildTarGzBundle(t, map[string]string{
		"index.html":       "<!doctype html><html><body><h1>Board v1</h1><script src=\"/assets/app.js\"></script></body></html>",
		"assets/app.js":    "console.log('board-v1');",
		"styles/theme.css": "body { color: #111; }",
	})
	deployV1, err := env.aliceClient.UploadHostedAppRevision(ctx, createdApp.ID, bytes.NewReader(v1Bundle), sohaapi.UploadHostedAppRevisionParams{
		IdempotencyKey: "deploy-v1",
		SpaFallback:    &spaTrue,
	})
	if err != nil || deployV1.App.Status != sohaapi.HostedAppStatusOnline || deployV1.ReusedIdempotencyKey {
		t.Fatalf("v1 deploy = (%#v, %v)", deployV1, err)
	}
	replayV1, err := env.aliceClient.UploadHostedAppRevision(ctx, createdApp.ID, bytes.NewReader(v1Bundle), sohaapi.UploadHostedAppRevisionParams{
		IdempotencyKey: "deploy-v1",
		SpaFallback:    &spaTrue,
	})
	if err != nil || !replayV1.ReusedIdempotencyKey || replayV1.Revision.ID != deployV1.Revision.ID {
		t.Fatalf("idempotent replay = (%#v, %v)", replayV1, err)
	}
	return createdApp, deployV1.Revision.ID
}

func verifyBobCleanBrowserSSOAndDeepLink(t *testing.T, env *e2eEnv, appHost string) (*http.Client, string) {
	t.Helper()
	bobJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	bobHTTP := env.newBrowserClient(t, bobJar)
	deepLink := "http://" + appHost + "/reports/q4?region=east"

	req1, err := http.NewRequest(http.MethodGet, deepLink, nil)
	if err != nil {
		t.Fatal(err)
	}
	req1.Header.Set("Accept", "text/html")
	resp1, err := bobHTTP.Do(req1)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp1.Body.Close()
	authStartLoc := resp1.Header.Get("Location")
	if resp1.StatusCode != http.StatusFound || !strings.Contains(authStartLoc, "/api/v1/app-hosting/auth/start") {
		t.Fatalf("bob initial redirect = (%d, %q)", resp1.StatusCode, authStartLoc)
	}

	resp2, err := bobHTTP.Get(authStartLoc)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp2.Body.Close()
	loginLoc := resp2.Header.Get("Location")
	if resp2.StatusCode != http.StatusFound || !strings.HasPrefix(loginLoc, "/login?return_to=") {
		t.Fatalf("Core unauthenticated redirect = (%d, %q)", resp2.StatusCode, loginLoc)
	}

	seedCoreLoginCookie(bobJar, env.coreURL, "tok-bob")
	loginQuery, err := url.Parse(loginLoc)
	if err != nil {
		t.Fatal(err)
	}
	resp3, err := bobHTTP.Get(env.coreServer.URL + loginQuery.Query().Get("return_to"))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp3.Body.Close()
	callbackLoc := resp3.Header.Get("Location")
	if resp3.StatusCode != http.StatusFound || !strings.HasPrefix(callbackLoc, "http://"+appHost+"/_soha/auth/callback?") {
		t.Fatalf("Core ticket redirect = (%d, %q)", resp3.StatusCode, callbackLoc)
	}

	resp4, err := bobHTTP.Get(callbackLoc)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp4.Body.Close()
	if resp4.StatusCode != http.StatusFound || resp4.Header.Get("Location") != "/reports/q4?region=east" {
		t.Fatalf("callback redirect = (%d, %q)", resp4.StatusCode, resp4.Header.Get("Location"))
	}
	assertHostOnlyGatewayCookie(t, resp4.Cookies())

	page := followAllRedirects(t, bobHTTP, deepLink)
	if page.status != http.StatusOK || !strings.Contains(page.body, "Board v1") {
		t.Fatalf("SPA deep link = (%d, %q)", page.status, page.body)
	}
	return bobHTTP, authStartLoc
}

func assertHostOnlyGatewayCookie(t *testing.T, cookies []*http.Cookie) {
	t.Helper()
	for _, ck := range cookies {
		if ck.Name == appgateway.SessionCookieName {
			if ck.Domain != "" || !ck.HttpOnly {
				t.Fatalf("gateway session cookie must be Host-only HttpOnly, got Domain=%q HttpOnly=%v", ck.Domain, ck.HttpOnly)
			}
			return
		}
	}
	t.Fatalf("expected %s cookie on callback response", appgateway.SessionCookieName)
}

func verifyBobStaticAssetsAndVisitorIdentity(t *testing.T, bobHTTP *http.Client, appHost string) string {
	t.Helper()
	jsURL := "http://" + appHost + "/assets/app.js"
	respJS, err := bobHTTP.Get(jsURL)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(respJS.Body)
	_ = respJS.Body.Close()
	etag := respJS.Header.Get("ETag")
	if respJS.StatusCode != http.StatusOK || !strings.HasPrefix(respJS.Header.Get("Content-Type"), "text/javascript") || etag == "" || respJS.Header.Get("Cache-Control") != "private, no-cache" {
		t.Fatalf("JS headers = (%d, ct=%q, etag=%q, cc=%q)", respJS.StatusCode, respJS.Header.Get("Content-Type"), etag, respJS.Header.Get("Cache-Control"))
	}

	assertConditionalStatus(t, bobHTTP, jsURL, etag, http.StatusNotModified)

	respMissingJS, err := bobHTTP.Get("http://" + appHost + "/assets/missing.js")
	if err != nil {
		t.Fatal(err)
	}
	_ = respMissingJS.Body.Close()
	if respMissingJS.StatusCode != http.StatusNotFound {
		t.Fatalf("missing .js status = %d, want strict 404", respMissingJS.StatusCode)
	}

	reqMe, err := http.NewRequest(http.MethodGet, "http://"+appHost+"/_soha/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	reqMe.Header.Set("Sec-Fetch-Site", "same-origin")
	respMe, err := bobHTTP.Do(reqMe)
	if err != nil {
		t.Fatal(err)
	}
	rawBody, err := io.ReadAll(respMe.Body)
	_ = respMe.Body.Close()
	if err != nil {
		t.Fatalf("read /_soha/me: %v", err)
	}
	var visitor sohaapi.HostedAppVisitorIdentity
	if err := json.Unmarshal(rawBody, &visitor); err != nil {
		t.Fatalf("decode /_soha/me: %v", err)
	}
	if respMe.StatusCode != http.StatusOK || !strings.HasPrefix(visitor.ID, "ahv_") || visitor.DisplayName != "李博" {
		t.Fatalf("/_soha/me = (%d, %#v)", respMe.StatusCode, visitor)
	}
	bodyText := string(rawBody)
	if strings.Contains(bodyText, `"email"`) || strings.Contains(bodyText, `"username"`) || strings.Contains(bodyText, `"teams"`) || strings.Contains(bodyText, "user-bob") {
		t.Fatalf("/_soha/me leaked restricted identity fields: %s", bodyText)
	}
	return etag
}

func assertConditionalStatus(t *testing.T, client *http.Client, targetURL, etag string, wantStatus int) {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, targetURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("If-None-Match", etag)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("conditional GET %s status = %d, want %d", targetURL, resp.StatusCode, wantStatus)
	}
}

func verifyUnauthorizedEveBlocked(t *testing.T, env *e2eEnv, authStartLoc string) {
	t.Helper()
	eveJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	seedCoreLoginCookie(eveJar, env.coreURL, "tok-eve")
	eveHTTP := env.newBrowserClient(t, eveJar)
	respEve, err := eveHTTP.Get(authStartLoc)
	if err != nil {
		t.Fatal(err)
	}
	_ = respEve.Body.Close()
	if respEve.StatusCode != http.StatusForbidden {
		t.Fatalf("unauthorized eve status = %d, want 403", respEve.StatusCode)
	}
}

func verifyTeamAndPrivateVisibilityRevocations(t *testing.T, ctx context.Context, env *e2eEnv, bobHTTP *http.Client, appID, appHost, etag string) {
	t.Helper()
	jsURL := "http://" + appHost + "/assets/app.js"
	bobReplica2 := env.newBrowserClientForGateway(t, bobHTTP.Jar, env.gwURL2)
	assertConditionalStatus(t, bobReplica2, jsURL, etag, http.StatusNotModified)

	env.audience.SetUserTeams("user-bob", []string{"team-sales"})
	assertConditionalStatus(t, bobHTTP, jsURL, etag, http.StatusForbidden)
	assertConditionalStatus(t, bobReplica2, jsURL, etag, http.StatusForbidden)
	env.audience.SetUserTeams("user-bob", []string{"team-data"})
	assertConditionalStatus(t, bobReplica2, jsURL, etag, http.StatusNotModified)

	if _, err := env.aliceClient.UpdateHostedAppVisibility(ctx, appID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModePrivate},
	}); err != nil {
		t.Fatalf("UpdateHostedAppVisibility private: %v", err)
	}
	assertConditionalStatus(t, bobHTTP, jsURL, etag, http.StatusForbidden)
	assertConditionalStatus(t, bobReplica2, jsURL, etag, http.StatusForbidden)
}

func verifyV2DeployRollbackAndAdminSuspension(t *testing.T, ctx context.Context, env *e2eEnv, bobHTTP *http.Client, appID, v1RevID, appHost string) {
	t.Helper()
	aliceJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	seedCoreLoginCookie(aliceJar, env.coreURL, "tok-alice")
	aliceHTTP := env.newBrowserClient(t, aliceJar)

	if page := followAllRedirects(t, aliceHTTP, "http://"+appHost+"/"); page.status != http.StatusOK || !strings.Contains(page.body, "Board v1") {
		t.Fatalf("alice zero-click SSO failed: (%d, %q)", page.status, page.body)
	}

	spaTrue := true
	v2Bundle := buildTarGzBundle(t, map[string]string{
		"index.html":    "<!doctype html><html><body><h1>Board v2</h1></body></html>",
		"assets/app.js": "console.log('board-v2');",
	})
	if _, err := env.aliceClient.UploadHostedAppRevision(ctx, appID, bytes.NewReader(v2Bundle), sohaapi.UploadHostedAppRevisionParams{
		IdempotencyKey: "deploy-v2",
		SpaFallback:    &spaTrue,
	}); err != nil {
		t.Fatalf("UploadHostedAppRevision v2: %v", err)
	}
	if page := followAllRedirects(t, aliceHTTP, "http://"+appHost+"/"); page.status != http.StatusOK || !strings.Contains(page.body, "Board v2") {
		t.Fatalf("alice v2 check failed: (%d, %q)", page.status, page.body)
	}

	if _, err := env.aliceClient.RollbackHostedApp(ctx, appID, sohaapi.HostedAppRollbackRequest{
		TargetRevisionID: v1RevID,
		IdempotencyKey:   "rollback-to-v1",
		Reason:           "restore v1 metrics",
	}); err != nil {
		t.Fatalf("RollbackHostedApp: %v", err)
	}
	if page := followAllRedirects(t, aliceHTTP, "http://"+appHost+"/"); page.status != http.StatusOK || !strings.Contains(page.body, "Board v1") {
		t.Fatalf("alice rollback check failed: (%d, %q)", page.status, page.body)
	}
	respBobAfterRollback, err := bobHTTP.Get("http://" + appHost + "/")
	if err != nil {
		t.Fatal(err)
	}
	_ = respBobAfterRollback.Body.Close()
	if respBobAfterRollback.StatusCode != http.StatusForbidden {
		t.Fatalf("rollback must not restore revoked visibility: status = %d", respBobAfterRollback.StatusCode)
	}

	assertAdminForceSuspensionLifecycle(t, ctx, env, aliceHTTP, appID, appHost)
}

func assertAdminForceSuspensionLifecycle(t *testing.T, ctx context.Context, env *e2eEnv, aliceHTTP *http.Client, appID, appHost string) {
	t.Helper()
	if _, err := env.adminClient.SuspendHostedApp(ctx, appID, sohaapi.HostedAppSuspendRequest{Reason: "compliance check"}); err != nil {
		t.Fatalf("SuspendHostedApp: %v", err)
	}
	if page := followAllRedirects(t, aliceHTTP, "http://"+appHost+"/"); page.status != http.StatusServiceUnavailable {
		t.Fatalf("suspended app status for author = %d, want 503", page.status)
	}
	if _, err := env.aliceClient.BringHostedAppOnline(ctx, appID, &sohaapi.HostedAppLifecycleActionRequest{Reason: "try bypass"}); err == nil {
		t.Fatal("author must not bypass admin suspension via BringHostedAppOnline")
	}
	if _, err := env.adminClient.UnsuspendHostedApp(ctx, appID, &sohaapi.HostedAppLifecycleActionRequest{Reason: "cleared"}); err != nil {
		t.Fatalf("UnsuspendHostedApp: %v", err)
	}
	if _, err := env.aliceClient.BringHostedAppOnline(ctx, appID, &sohaapi.HostedAppLifecycleActionRequest{Reason: "resume"}); err != nil {
		t.Fatalf("BringHostedAppOnline after unsuspend: %v", err)
	}
	if page := followAllRedirects(t, aliceHTTP, "http://"+appHost+"/"); page.status != http.StatusOK || !strings.Contains(page.body, "Board v1") {
		t.Fatalf("resumed app failed: (%d, %q)", page.status, page.body)
	}

	env.sessions.Revoke("sess-user-alice")
	reqMeAfterLogout, err := http.NewRequest(http.MethodGet, "http://"+appHost+"/_soha/me", nil)
	if err != nil {
		t.Fatal(err)
	}
	reqMeAfterLogout.Header.Set("Sec-Fetch-Site", "same-origin")
	respMeAfterLogout, err := aliceHTTP.Do(reqMeAfterLogout)
	if err != nil {
		t.Fatal(err)
	}
	_ = respMeAfterLogout.Body.Close()
	if respMeAfterLogout.StatusCode != http.StatusUnauthorized {
		t.Fatalf("/_soha/me after source session logout = %d, want 401", respMeAfterLogout.StatusCode)
	}
}

func buildTarGzBundle(t *testing.T, files map[string]string) []byte {
	t.Helper()
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, name := range keys {
		data := []byte(files[name])
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(data)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("write tar header %s: %v", name, err)
		}
		if _, err := tw.Write(data); err != nil {
			t.Fatalf("write tar body %s: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := gz.Close(); err != nil {
		t.Fatalf("close gzip: %v", err)
	}
	return buf.Bytes()
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type pageResult struct {
	status int
	body   string
}

func followAllRedirects(t *testing.T, client *http.Client, target string) pageResult {
	t.Helper()
	current := target
	for range 8 {
		req, err := http.NewRequest(http.MethodGet, current, nil)
		if err != nil {
			t.Fatalf("new request %s: %v", current, err)
		}
		req.Header.Set("Accept", "text/html")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("do %s: %v", current, err)
		}
		bodyBytes, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			t.Fatalf("read body %s: %v", current, err)
		}
		if resp.StatusCode >= 300 && resp.StatusCode < 400 {
			loc := resp.Header.Get("Location")
			if loc == "" {
				t.Fatalf("redirect %d without Location at %s", resp.StatusCode, current)
			}
			base, err := url.Parse(current)
			if err != nil {
				t.Fatalf("parse base %s: %v", current, err)
			}
			nextURL, err := base.Parse(loc)
			if err != nil {
				t.Fatalf("parse redirect %q: %v", loc, err)
			}
			current = nextURL.String()
			continue
		}
		return pageResult{status: resp.StatusCode, body: string(bodyBytes)}
	}
	t.Fatalf("exceeded redirect hops starting at %s", target)
	return pageResult{}
}
