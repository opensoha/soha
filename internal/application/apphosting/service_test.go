package apphosting_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"io"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appapphosting "github.com/opensoha/soha/internal/application/apphosting"
	domainapphosting "github.com/opensoha/soha/internal/domain/apphosting"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	apphostingrepo "github.com/opensoha/soha/internal/repository/apphosting"
)

type stubPermissions struct {
	grants map[string][]string
}

func (p *stubPermissions) Authorize(_ context.Context, principal domainidentity.Principal, permissionKey string) error {
	if slices.Contains(p.grants[principal.UserID], permissionKey) || slices.Contains(principal.PermissionKeys, permissionKey) {
		return nil
	}
	return apperrors.ErrAccessDenied
}

func (p *stubPermissions) AuthorizeAny(ctx context.Context, principal domainidentity.Principal, permissionKeys ...string) error {
	for _, key := range permissionKeys {
		if p.Authorize(ctx, principal, key) == nil {
			return nil
		}
	}
	return apperrors.ErrAccessDenied
}

type stubObjectStore struct {
	mu          sync.Mutex
	objects     map[string][]byte
	contentType map[string]string
	deletedKeys []string
	onPut       func(key string) error
}

func newStubObjectStore() *stubObjectStore {
	return &stubObjectStore{
		objects:     make(map[string][]byte),
		contentType: make(map[string]string),
	}
}

func (s *stubObjectStore) PutObject(_ context.Context, _, key, contentType string, content io.Reader) (int64, string, string, error) {
	if s.onPut != nil {
		if err := s.onPut(key); err != nil {
			return 0, "", "", err
		}
	}
	data, err := io.ReadAll(content)
	if err != nil {
		return 0, "", "", err
	}
	s.mu.Lock()
	s.objects[key] = data
	s.contentType[key] = contentType
	s.mu.Unlock()
	sum := strings.Repeat("a", 64)
	return int64(len(data)), sum, `"sha256:` + sum + `"`, nil
}

func (s *stubObjectStore) DeleteKeys(_ context.Context, _ string, keys []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, k := range keys {
		delete(s.objects, k)
		s.deletedKeys = append(s.deletedKeys, k)
	}
	return nil
}

type stubAudit struct {
	mu      sync.Mutex
	entries []domainaudit.Entry
}

func (a *stubAudit) Record(_ context.Context, entry domainaudit.Entry) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.entries = append(a.entries, entry)
	return nil
}

type tarEntrySpec struct {
	name     string
	typeflag byte
	body     string
	linkname string
}

func makeTarGz(t *testing.T, entries []tarEntrySpec) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, e := range entries {
		tf := e.typeflag
		if tf == 0 {
			tf = tar.TypeReg
		}
		hdr := &tar.Header{
			Name:     e.name,
			Mode:     0o644,
			Size:     int64(len(e.body)),
			Typeflag: tf,
			Linkname: e.linkname,
		}
		if tf != tar.TypeReg && tf != '\x00' {
			hdr.Size = 0
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if tf == tar.TypeReg || tf == '\x00' {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type stubSessionValidator struct {
	mu       sync.Mutex
	sessions map[string]appapphosting.BrowserSessionState
}

func newStubSessionValidator() *stubSessionValidator {
	return &stubSessionValidator{
		sessions: make(map[string]appapphosting.BrowserSessionState),
	}
}

func (v *stubSessionValidator) Set(sessionID, userID string, active bool, expiresAt time.Time) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.sessions[sessionID] = appapphosting.BrowserSessionState{Active: active, UserID: userID, ExpiresAt: expiresAt}
}

func (v *stubSessionValidator) ValidateBrowserSession(_ context.Context, sessionID, expectedUserID string) (appapphosting.BrowserSessionState, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	state, ok := v.sessions[sessionID]
	if !ok || state.UserID != expectedUserID {
		return appapphosting.BrowserSessionState{Active: false}, nil
	}
	return state, nil
}

type testHarness struct {
	svc      *appapphosting.Service
	repo     *apphostingrepo.MemoryRepository
	store    *stubObjectStore
	audience *apphostingrepo.MemoryAudienceDirectory
	sessions *stubSessionValidator
	audit    *stubAudit
	now      time.Time
	admin    domainidentity.Principal
	alice    domainidentity.Principal
	bob      domainidentity.Principal
	carol    domainidentity.Principal
}

func newTestHarness(t *testing.T, extra ...appapphosting.Option) *testHarness {
	t.Helper()
	repo := apphostingrepo.NewMemoryRepository()
	store := newStubObjectStore()
	audience := apphostingrepo.NewMemoryAudienceDirectory()
	sessions := newStubSessionValidator()
	audit := &stubAudit{}
	now := time.Date(2026, 11, 20, 15, 0, 0, 0, time.UTC)
	sessions.Set("sess_alice", "usr_alice", true, now.Add(6*time.Hour))
	sessions.Set("sess_bob", "usr_bob", true, now.Add(6*time.Hour))
	sessions.Set("sess_carol", "usr_carol", true, now.Add(6*time.Hour))

	perms := &stubPermissions{
		grants: map[string][]string{
			"usr_admin": {appaccess.PermAppHostingAdminManage, appaccess.PermAppHostingAdminView},
			"usr_alice": {appaccess.PermAppHostingAppsPublish, appaccess.PermAppHostingAppsView},
			"usr_bob":   {appaccess.PermAppHostingAppsPublish, appaccess.PermAppHostingAppsView},
			"usr_carol": {appaccess.PermAppHostingAppsView},
		},
	}
	audience.UpsertTeam(sohaapi.AppHostingAudienceTeam{ID: "team_fin", Name: "Finance", Slug: "finance"})
	audience.UpsertUser(apphostingrepo.MemoryUser{ID: "usr_alice", Username: "alice", DisplayName: "Alice", Department: "Engineering", Status: "active", TeamIDs: []string{"team_eng"}})
	audience.UpsertUser(apphostingrepo.MemoryUser{ID: "usr_bob", Username: "bob", DisplayName: "Bob", Department: "Finance", Status: "active", TeamIDs: []string{"team_fin"}})
	audience.UpsertUser(apphostingrepo.MemoryUser{ID: "usr_carol", Username: "carol", DisplayName: "Carol", Department: "HR", Status: "active", TeamIDs: []string{"team_hr"}})

	options := []appapphosting.Option{
		appapphosting.WithFallbackGatewayToken("bootstrap-gw-token"),
		appapphosting.WithSessionValidator(sessions),
		appapphosting.WithVisitorIDSecret("unit-test-visitor-hmac-secret-key"),
		appapphosting.WithClock(func() time.Time { return now }),
	}
	svc, err := appapphosting.New(repo, store, audience, perms, audit, append(options, extra...)...)
	if err != nil {
		t.Fatal(err)
	}
	h := &testHarness{
		svc:      svc,
		repo:     repo,
		store:    store,
		audience: audience,
		sessions: sessions,
		audit:    audit,
		now:      now,
		admin:    domainidentity.Principal{UserID: "usr_admin", UserName: "admin"},
		alice:    domainidentity.Principal{UserID: "usr_alice", UserName: "alice", Teams: []string{"team_eng"}},
		bob:      domainidentity.Principal{UserID: "usr_bob", UserName: "bob", Teams: []string{"team_fin"}},
		carol:    domainidentity.Principal{UserID: "usr_carol", UserName: "carol", Teams: []string{"team_hr"}},
	}

	_, err = svc.UpdateSettings(t.Context(), h.admin, sohaapi.AppHostingSettingsUpdateRequest{
		Enabled:                 true,
		AppsDomain:              "apps.local",
		PublicScheme:            "http",
		StorageIntegrationID:    "int_minio_1",
		StoragePrefix:           "hosted",
		RetainedRevisionsPerApp: 2,
		MaxAppsPerOwner:         4,
	})
	if err != nil {
		t.Fatalf("UpdateSettings setup: %v", err)
	}
	return h
}

func TestBundleValidationDLPIdempotencyEvictionAndVisibilityPreservation(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	app1 := assertCreateAppIdempotency(t, ctx, h)
	assertRejectedBundleCases(t, ctx, h, app1.ID)
	assertRevisionLifecycleAndRollback(t, ctx, h, app1.ID)
}

func assertCreateAppIdempotency(t *testing.T, ctx context.Context, h *testHarness) sohaapi.HostedApp {
	t.Helper()
	spaTrue := true
	createReq := sohaapi.CreateHostedAppRequest{
		Name:           "Quarterly Dashboard",
		Slug:           "q4-dash",
		SpaFallback:    &spaTrue,
		IdempotencyKey: "create-q4",
		Visibility: &sohaapi.HostedAppVisibilityInput{
			Mode:    sohaapi.HostedAppVisibilityModeSelected,
			TeamIDs: []string{"team_fin"},
		},
	}
	app1, reused, err := h.svc.CreateApp(ctx, h.alice, createReq)
	if err != nil || reused || !strings.HasPrefix(app1.DNSLabel, "q4-dash-") {
		t.Fatalf("CreateApp = %#v, reused=%v, err=%v", app1, reused, err)
	}
	if _, reusedAgain, err := h.svc.CreateApp(ctx, h.alice, createReq); err != nil || !reusedAgain {
		t.Fatalf("expected reused CreateApp, got reused=%v err=%v", reusedAgain, err)
	}
	if _, _, err := h.svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{
		Name:           "Different Name",
		IdempotencyKey: "create-q4",
	}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("expected ErrConflict on mismatched idempotencyKey, got %v", err)
	}
	return app1
}

func assertRejectedBundleCases(t *testing.T, ctx context.Context, h *testHarness, appID string) {
	t.Helper()
	syntheticAWSKey := "AKIA" + "IOSFODNN7EXAMPLE"
	badCases := []struct {
		name    string
		entries []tarEntrySpec
	}{
		{"missing index.html", []tarEntrySpec{{name: "about.html", body: "<h1>hi</h1>"}}},
		{"path traversal", []tarEntrySpec{{name: "../index.html", body: "<h1>hi</h1>"}}},
		{"symlink entry", []tarEntrySpec{{name: "index.html", body: "<h1>hi</h1>"}, {name: "leak", typeflag: tar.TypeSymlink, linkname: "/etc/passwd"}}},
		{"duplicate normalized path", []tarEntrySpec{{name: "./index.html", body: "a"}, {name: "index.html", body: "b"}}},
		{"dotenv credential file", []tarEntrySpec{{name: "index.html", body: "ok"}, {name: ".env.production", body: "FOO=1"}}},
		{"private key file", []tarEntrySpec{{name: "index.html", body: "ok"}, {name: "certs/server.key", body: "x"}}},
		{"DLP AWS key in JS", []tarEntrySpec{{name: "index.html", body: "ok"}, {name: "assets/app.js", body: "const k = '" + syntheticAWSKey + "';"}}},
		{"DLP private key in large >256KiB JS with control char", []tarEntrySpec{
			{name: "index.html", body: "ok"},
			{name: "assets/vendor.js", body: strings.Repeat("/*padding*/\n", 26000) + "\x0b-----BEGIN RSA PRIVATE KEY-----\nMIIE..."},
		}},
		{"DLP AWS key in >1MiB JS bundle", []tarEntrySpec{
			{name: "index.html", body: "ok"},
			{name: "assets/large-bundle.js", body: strings.Repeat("const noop = 1;\n", 80000) + "const k = '" + syntheticAWSKey + "';"},
		}},
		{"fail-closed >8MiB single JS file", []tarEntrySpec{
			{name: "index.html", body: "ok"},
			{name: "assets/huge.js", body: strings.Repeat("a", (8<<20)+64)},
		}},
		{"fail-closed invalid UTF-8 byte in JS with secret", []tarEntrySpec{
			{name: "index.html", body: "ok"},
			{name: "assets/smuggled.js", body: "/*\xff*/ const k = '" + syntheticAWSKey + "';"},
		}},
		{"nested zip archive forbidden", []tarEntrySpec{{name: "index.html", body: "ok"}, {name: "assets/nested.zip", body: "PK\x03\x04"}}},
		{"spoofed png header rejected", []tarEntrySpec{{name: "index.html", body: "ok"}, {name: "assets/fake.png", body: "<script>alert(1)</script>"}}},
		{"unknown executable format rejected", []tarEntrySpec{{name: "index.html", body: "ok"}, {name: "assets/tool.exe", body: "MZ\x90\x00"}}},
	}
	for _, tc := range badCases {
		t.Run(tc.name, func(t *testing.T) {
			raw := makeTarGz(t, tc.entries)
			_, err := h.svc.UploadRevision(ctx, h.alice, appID, domainapphosting.UploadRevisionInput{}, bytes.NewReader(raw))
			if !errors.Is(err, domainapphosting.ErrBundleRejected) {
				t.Fatalf("case %s: got err=%v, want ErrBundleRejected", tc.name, err)
			}
		})
	}
}

func assertRevisionLifecycleAndRollback(t *testing.T, ctx context.Context, h *testHarness, appID string) {
	t.Helper()
	largeCleanJS := strings.Repeat("export const answer = 42;\n", 12000)
	validV1 := makeTarGz(t, []tarEntrySpec{
		{name: ".", typeflag: tar.TypeDir},
		{name: "./index.html", body: "<!doctype html><html><body>v1</body></html>"},
		{name: "./assets/app.mjs", body: largeCleanJS},
		{name: "./assets/style.css", body: "body{margin:0}"},
		{name: "./assets/logo.svg", body: "<svg></svg>"},
		{name: "./assets/font.woff2", body: "wOF2\x00\x01\x00\x00"},
	})
	receipt1, err := h.svc.UploadRevision(ctx, h.alice, appID, domainapphosting.UploadRevisionInput{
		IdempotencyKey: "dep-v1",
		Description:    "initial release",
	}, bytes.NewReader(validV1))
	if err != nil || receipt1.App.Status != sohaapi.HostedAppStatusOnline || receipt1.App.Visibility.Mode != sohaapi.HostedAppVisibilityModeSelected {
		t.Fatalf("UploadRevision v1 = %#v, err=%v", receipt1, err)
	}
	replay1, err := h.svc.UploadRevision(ctx, h.alice, appID, domainapphosting.UploadRevisionInput{IdempotencyKey: "dep-v1"}, bytes.NewReader(validV1))
	if err != nil || !replay1.ReusedIdempotencyKey || replay1.Revision.ID != receipt1.Revision.ID {
		t.Fatalf("idempotent replay = %#v, %v", replay1, err)
	}

	validV2 := makeTarGz(t, []tarEntrySpec{{name: "index.html", body: "<!doctype html>v2"}})
	if _, err := h.svc.UploadRevision(ctx, h.alice, appID, domainapphosting.UploadRevisionInput{IdempotencyKey: "dep-v1"}, bytes.NewReader(validV2)); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("expected ErrConflict for reused idempotencyKey with different digest, got %v", err)
	}
	receipt2, err := h.svc.UploadRevision(ctx, h.alice, appID, domainapphosting.UploadRevisionInput{Description: "v2"}, bytes.NewReader(validV2))
	if err != nil {
		t.Fatal(err)
	}
	validV3 := makeTarGz(t, []tarEntrySpec{{name: "index.html", body: "<!doctype html>v3"}})
	receipt3, err := h.svc.UploadRevision(ctx, h.alice, appID, domainapphosting.UploadRevisionInput{Description: "v3"}, bytes.NewReader(validV3))
	if err != nil {
		t.Fatal(err)
	}

	assertV1EvictedAndRollbackToV2(t, ctx, h, appID, receipt1.Revision.ID, receipt2.Revision.ID, receipt3.App.RouteGeneration)
}

func assertV1EvictedAndRollbackToV2(t *testing.T, ctx context.Context, h *testHarness, appID, rev1ID, rev2ID string, v3Gen int64) {
	t.Helper()
	rev1After, err := h.svc.GetRevision(ctx, h.alice, appID, rev1ID)
	if err != nil || rev1After.Status != sohaapi.HostedAppRevisionStatusEvicted {
		t.Fatalf("expected v1 evicted, got %#v (%v)", rev1After, err)
	}
	if _, exists := h.store.objects["hosted/"+appID+"/"+rev1ID+"/index.html"]; exists {
		t.Fatal("expected evicted v1 S3 object to be deleted")
	}
	if _, err := h.svc.Rollback(ctx, h.alice, appID, sohaapi.HostedAppRollbackRequest{TargetRevisionID: rev1ID}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("rollback to evicted revision = %v, want ErrConflict", err)
	}
	rolled, err := h.svc.Rollback(ctx, h.alice, appID, sohaapi.HostedAppRollbackRequest{TargetRevisionID: rev2ID, Reason: "revert v3"})
	if err != nil || rolled.App.ActiveRevisionID != rev2ID || rolled.App.RouteGeneration <= v3Gen {
		t.Fatalf("Rollback = %#v, %v", rolled, err)
	}
	if rolled.App.Visibility.Mode != sohaapi.HostedAppVisibilityModeSelected || len(rolled.App.Visibility.TeamIDs) != 1 {
		t.Fatalf("Rollback altered visibility: %#v", rolled.App.Visibility)
	}
}

func TestBrowserSSOGatewayTicketAndImmediateTeamOrSuspensionRevocation(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	app, _, err := h.svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{
		Name: "Finance Tracker",
		Visibility: &sohaapi.HostedAppVisibilityInput{
			Mode:    sohaapi.HostedAppVisibilityModeSelected,
			TeamIDs: []string{"team_fin"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	bundle := makeTarGz(t, []tarEntrySpec{{name: "index.html", body: "<html>finance</html>"}})
	deployed, err := h.svc.UploadRevision(ctx, h.alice, app.ID, domainapphosting.UploadRevisionInput{}, bytes.NewReader(bundle))
	if err != nil {
		t.Fatal(err)
	}
	hostWithPort := app.DNSLabel + ".apps.local:18090"

	rawTicket := assertStartBrowserAuthDecisions(t, ctx, h, app.DNSLabel, hostWithPort)
	rotatedToken, sessionID := assertTicketExchangeAndAccessCheck(t, ctx, h, app.ID, deployed.Revision.ID, hostWithPort, rawTicket)
	assertTeamDisableAndSuspendRevocations(t, ctx, h, app.ID, deployed.Revision.ID, hostWithPort, rotatedToken, sessionID)
}

func assertStartBrowserAuthDecisions(t *testing.T, ctx context.Context, h *testHarness, dnsLabel, hostWithPort string) string {
	t.Helper()
	unauthStart, err := h.svc.StartBrowserAuth(ctx, domainidentity.Principal{}, domainapphosting.BrowserAuthStartInput{
		Host:         hostWithPort,
		ReturnPath:   "/deep/link?x=1",
		GatewayState: "gw-state-1",
	})
	if err != nil || !unauthStart.LoginRequired {
		t.Fatalf("unauthenticated StartBrowserAuth = %#v, %v", unauthStart, err)
	}
	if _, err := h.svc.StartBrowserAuth(ctx, h.carol, domainapphosting.BrowserAuthStartInput{
		Host:            hostWithPort,
		SourceSessionID: "sess_carol",
	}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("Carol StartBrowserAuth = %v, want ErrAccessDenied", err)
	}
	if _, err := h.svc.StartBrowserAuth(ctx, h.bob, domainapphosting.BrowserAuthStartInput{
		Host: hostWithPort,
	}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("missing SourceSessionID StartBrowserAuth = %v, want ErrAccessDenied", err)
	}
	bobStart, err := h.svc.StartBrowserAuth(ctx, h.bob, domainapphosting.BrowserAuthStartInput{
		Host:            strings.ToUpper(dnsLabel) + ".APPS.LOCAL:18090",
		ReturnPath:      "/deep/link?x=1",
		GatewayState:    "gw-state-1",
		SourceSessionID: "sess_bob",
	})
	if err != nil || bobStart.LoginRequired {
		t.Fatalf("Bob StartBrowserAuth = %#v, %v", bobStart, err)
	}
	parsedRedirect, err := url.Parse(bobRedirectURL(t, bobStart.RedirectURL))
	if err != nil || parsedRedirect.Host != hostWithPort || parsedRedirect.Path != "/_soha/auth/callback" {
		t.Fatalf("unexpected callback URL %q (%v)", bobStart.RedirectURL, err)
	}
	rawTicket := parsedRedirect.Query().Get("ticket")
	if rawTicket == "" || parsedRedirect.Query().Get("state") != "gw-state-1" {
		t.Fatalf("missing ticket or state in %q", bobStart.RedirectURL)
	}
	return rawTicket
}

func assertTicketExchangeAndAccessCheck(t *testing.T, ctx context.Context, h *testHarness, appID, revID, hostWithPort, rawTicket string) (string, string) {
	t.Helper()
	rotated, err := h.svc.RotateGatewayToken(ctx, h.admin, "initial setup")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.ExchangeGatewayTicket(ctx, "bootstrap-gw-token", sohaapi.HostedAppTicketExchangeRequest{
		Host: hostWithPort, Ticket: rawTicket,
	}); !errors.Is(err, apperrors.ErrUnauthorized) {
		t.Fatalf("expected fallback token rejection after rotation, got %v", err)
	}
	exchanged, err := h.svc.ExchangeGatewayTicket(ctx, rotated.Token, sohaapi.HostedAppTicketExchangeRequest{
		Host:         hostWithPort,
		Ticket:       rawTicket,
		GatewayState: "gw-state-1",
	})
	if err != nil || exchanged.ReturnPath != "/deep/link?x=1" || exchanged.PrincipalUserID != "usr_bob" || !strings.HasPrefix(exchanged.User.ID, "ahv_") || exchanged.User.Email != "" || len(exchanged.User.Teams) != 0 {
		t.Fatalf("ExchangeGatewayTicket = %#v, %v", exchanged, err)
	}
	if _, err := h.svc.ExchangeGatewayTicket(ctx, rotated.Token, sohaapi.HostedAppTicketExchangeRequest{
		Host:         hostWithPort,
		Ticket:       rawTicket,
		GatewayState: "gw-state-1",
	}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("ticket replay = %v, want ErrAccessDenied", err)
	}
	decision, err := h.svc.CheckGatewayAccess(ctx, rotated.Token, sohaapi.HostedAppGatewayAccessCheckRequest{
		Host:      hostWithPort,
		UserID:    "usr_bob",
		SessionID: exchanged.SessionID,
	})
	if err != nil || decision.Decision != sohaapi.HostedAppAccessDecisionKindAllow || len(decision.Files) != 1 {
		t.Fatalf("CheckGatewayAccess allow = %#v, %v", decision, err)
	}
	if wantKey := appID + "/" + revID + "/index.html"; decision.Files[0].ObjectKey != wantKey {
		t.Fatalf("manifest ObjectKey = %q, want %q", decision.Files[0].ObjectKey, wantKey)
	}
	return rotated.Token, exchanged.SessionID
}

func assertTeamDisableAndSuspendRevocations(t *testing.T, ctx context.Context, h *testHarness, appID, revID, hostWithPort, gwToken, sessionID string) {
	t.Helper()
	h.audience.SetUserTeams("usr_bob", []string{"team_sales"})
	if dec, err := h.svc.CheckGatewayAccess(ctx, gwToken, sohaapi.HostedAppGatewayAccessCheckRequest{Host: hostWithPort, UserID: "usr_bob", SessionID: sessionID}); err != nil || dec.Decision != sohaapi.HostedAppAccessDecisionKindForbidden {
		t.Fatalf("expected immediate forbidden after team removal, got %#v (%v)", dec, err)
	}
	h.audience.SetUserTeams("usr_bob", []string{"team_fin"})
	h.sessions.Set(sessionID, "usr_bob", false, h.now.Add(time.Hour))
	if dec, err := h.svc.CheckGatewayAccess(ctx, gwToken, sohaapi.HostedAppGatewayAccessCheckRequest{Host: hostWithPort, UserID: "usr_bob", SessionID: sessionID}); err != nil || dec.Decision != sohaapi.HostedAppAccessDecisionKindUnauthenticated {
		t.Fatalf("expected unauthenticated after source session logout, got %#v (%v)", dec, err)
	}
	h.sessions.Set(sessionID, "usr_bob", true, h.now.Add(time.Hour))
	h.audience.SetUserStatus("usr_bob", "disabled")
	if dec, err := h.svc.CheckGatewayAccess(ctx, gwToken, sohaapi.HostedAppGatewayAccessCheckRequest{Host: hostWithPort, UserID: "usr_bob", SessionID: sessionID}); err != nil || dec.Decision != sohaapi.HostedAppAccessDecisionKindForbidden {
		t.Fatalf("expected forbidden when user disabled, got %#v (%v)", dec, err)
	}

	if suspended, err := h.svc.SuspendApp(ctx, h.admin, appID, "compliance review"); err != nil || suspended.Status != sohaapi.HostedAppStatusSuspendedByAdmin {
		t.Fatalf("SuspendApp = %#v, %v", suspended, err)
	}
	if _, err := h.svc.BringOnline(ctx, h.alice, appID, "try self-restore"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("author BringOnline while suspended = %v, want ErrConflict", err)
	}
	if dec, err := h.svc.CheckGatewayAccess(ctx, gwToken, sohaapi.HostedAppGatewayAccessCheckRequest{Host: hostWithPort, UserID: "usr_alice"}); err != nil || dec.Decision != sohaapi.HostedAppAccessDecisionKindOffline {
		t.Fatalf("suspended app decision = %#v, %v", dec, err)
	}
	if unsuspended, err := h.svc.UnsuspendApp(ctx, h.admin, appID, "cleared"); err != nil || unsuspended.Status != sohaapi.HostedAppStatusOffline {
		t.Fatalf("UnsuspendApp = %#v, %v", unsuspended, err)
	}
	if onlineAgain, err := h.svc.BringOnline(ctx, h.alice, appID, "reopen"); err != nil || onlineAgain.Status != sohaapi.HostedAppStatusOnline {
		t.Fatalf("BringOnline after unsuspend = %#v, %v", onlineAgain, err)
	}

	h.store.onPut = func(_ string) error {
		_, suspErr := h.svc.SuspendApp(ctx, h.admin, appID, "mid-upload emergency")
		return suspErr
	}
	midUploadBundle := makeTarGz(t, []tarEntrySpec{{name: "index.html", body: "<html>v2-late</html>"}})
	if _, err := h.svc.UploadRevision(ctx, h.alice, appID, domainapphosting.UploadRevisionInput{}, bytes.NewReader(midUploadBundle)); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("expected ErrConflict when app suspended mid-upload, got %v", err)
	}
	if afterRace, err := h.svc.GetApp(ctx, h.admin, appID); err != nil || afterRace.Status != sohaapi.HostedAppStatusSuspendedByAdmin || afterRace.ActiveRevisionID != revID {
		t.Fatalf("mid-upload race corrupted app state: %#v (%v)", afterRace, err)
	}
}

func bobRedirectURL(t *testing.T, raw string) string {
	t.Helper()
	if raw == "" {
		t.Fatal("empty redirect URL")
	}
	return raw
}

func TestCollaboratorMaintainerBoundariesRollbackIdempotencyAndPortGuard(t *testing.T) {
	h := newTestHarness(t)
	ctx := t.Context()

	app1, _, err := h.svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{Name: "App Alpha"})
	if err != nil {
		t.Fatal(err)
	}
	app2, _, err := h.svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{Name: "App Beta"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.repo.MutateApp(ctx, app1.ID, func(cur domainapphosting.App) (domainapphosting.App, error) {
		cur.MaintainerUserIDs = []string{"usr_alice", "usr_bob"}
		return cur, nil
	}); err != nil {
		t.Fatal(err)
	}

	assertMaintainerPermissions(t, ctx, h, app1.ID)
	assertRollbackIdempotency(t, ctx, h, app1.ID)
	assertCustomPortAndPreExchangeSessionRevocation(t, ctx, h, app1, app2)
}

func assertMaintainerPermissions(t *testing.T, ctx context.Context, h *testHarness, appID string) {
	t.Helper()
	newName := "Hijacked"
	if _, err := h.svc.UpdateApp(ctx, h.bob, appID, sohaapi.UpdateHostedAppRequest{Name: &newName}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("maintainer UpdateApp = %v, want ErrAccessDenied", err)
	}
	if _, err := h.svc.UpdateVisibility(ctx, h.bob, appID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeOrganization},
	}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("maintainer UpdateVisibility = %v, want ErrAccessDenied", err)
	}
	v1 := makeTarGz(t, []tarEntrySpec{{name: "index.html", body: "<html>v1</html>"}})
	if _, err := h.svc.UploadRevision(ctx, h.bob, appID, domainapphosting.UploadRevisionInput{Description: "maintainer v1"}, bytes.NewReader(v1)); err != nil {
		t.Fatalf("maintainer UploadRevision failed: %v", err)
	}
	if _, err := h.svc.TakeOffline(ctx, h.bob, appID, "try offline"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("maintainer TakeOffline = %v, want ErrAccessDenied", err)
	}
	if _, err := h.svc.TakeOffline(ctx, h.alice, appID, "owner offline"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.BringOnline(ctx, h.bob, appID, "try online"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("maintainer BringOnline = %v, want ErrAccessDenied", err)
	}
	if _, err := h.svc.BringOnline(ctx, h.alice, appID, "owner online"); err != nil {
		t.Fatal(err)
	}
}

func assertRollbackIdempotency(t *testing.T, ctx context.Context, h *testHarness, appID string) {
	t.Helper()
	revs, err := h.svc.ListRevisions(ctx, h.alice, appID, 10, 0)
	if err != nil || len(revs.Items) != 1 {
		t.Fatalf("ListRevisions = %#v, %v", revs, err)
	}
	rev1ID := revs.Items[0].ID
	v2 := makeTarGz(t, []tarEntrySpec{{name: "index.html", body: "<html>v2</html>"}})
	rev2, err := h.svc.UploadRevision(ctx, h.bob, appID, domainapphosting.UploadRevisionInput{Description: "v2"}, bytes.NewReader(v2))
	if err != nil {
		t.Fatal(err)
	}

	rb1, err := h.svc.Rollback(ctx, h.bob, appID, sohaapi.HostedAppRollbackRequest{
		TargetRevisionID: rev1ID,
		IdempotencyKey:   "rb-key-1",
	})
	if err != nil || rb1.ReusedIdempotencyKey || rb1.App.ActiveRevisionID != rev1ID {
		t.Fatalf("initial Rollback = %#v, %v", rb1, err)
	}
	replay, err := h.svc.Rollback(ctx, h.bob, appID, sohaapi.HostedAppRollbackRequest{
		TargetRevisionID: rev1ID,
		IdempotencyKey:   "rb-key-1",
	})
	if err != nil || !replay.ReusedIdempotencyKey || replay.App.RouteGeneration != rb1.App.RouteGeneration {
		t.Fatalf("replayed Rollback = %#v, %v", replay, err)
	}
	if _, err := h.svc.Rollback(ctx, h.bob, appID, sohaapi.HostedAppRollbackRequest{
		TargetRevisionID: rev2.Revision.ID,
		IdempotencyKey:   "rb-key-1",
	}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("conflicting Rollback idempotencyKey = %v, want ErrConflict", err)
	}
}

func assertCustomPortAndPreExchangeSessionRevocation(t *testing.T, ctx context.Context, h *testHarness, app1, app2 sohaapi.HostedApp) {
	t.Helper()
	v1 := makeTarGz(t, []tarEntrySpec{{name: "index.html", body: "<html>beta</html>"}})
	if _, err := h.svc.UploadRevision(ctx, h.alice, app2.ID, domainapphosting.UploadRevisionInput{}, bytes.NewReader(v1)); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.UpdateSettings(ctx, h.admin, sohaapi.AppHostingSettingsUpdateRequest{
		Enabled:              true,
		AppsDomain:           "apps.example.com",
		PublicScheme:         "https",
		StorageIntegrationID: "int_minio_1",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.StartBrowserAuth(ctx, h.alice, domainapphosting.BrowserAuthStartInput{
		Host:            app1.DNSLabel + ".apps.example.com:31337",
		SourceSessionID: "sess_alice",
	}); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("HTTPS non-standard port = %v, want ErrInvalidArgument", err)
	}

	id1 := domainapphosting.DerivePairwiseVisitorID([]byte("secret"), app1.ID, "usr_alice")
	id2 := domainapphosting.DerivePairwiseVisitorID([]byte("secret"), app2.ID, "usr_alice")
	if id1 == id2 || !strings.HasPrefix(id1, "ahv_") {
		t.Fatalf("pairwise IDs must be distinct per app: %q vs %q", id1, id2)
	}

	startRes, err := h.svc.StartBrowserAuth(ctx, h.alice, domainapphosting.BrowserAuthStartInput{
		Host:            app1.DNSLabel + ".apps.example.com",
		GatewayState:    "st-99",
		SourceSessionID: "sess_alice",
	})
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(startRes.RedirectURL)
	h.sessions.Set("sess_alice", "usr_alice", false, h.now.Add(time.Hour))
	if _, err := h.svc.ExchangeGatewayTicket(ctx, "bootstrap-gw-token", sohaapi.HostedAppTicketExchangeRequest{
		Host:         app1.DNSLabel + ".apps.example.com",
		Ticket:       u.Query().Get("ticket"),
		GatewayState: "st-99",
	}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("exchange after source session logout = %v, want ErrAccessDenied", err)
	}
}
