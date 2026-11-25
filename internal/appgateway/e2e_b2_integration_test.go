package appgateway_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type stubE2EWorkspaceBundleSource struct {
	t *testing.T
}

func (s stubE2EWorkspaceBundleSource) BundlePersonalWorkspace(
	_ context.Context,
	principal domainidentity.Principal,
	sourcePath, _ string,
	_ int64,
	_ sohaapi.HostedAppKind,
) (io.ReadCloser, error) {
	if principal.UserID == "" {
		return nil, apperrors.ErrUnauthorized
	}
	if strings.Contains(sourcePath, "leak") {
		raw := buildTarGzBundle(s.t, map[string]string{
			"index.html": "<!doctype html><html><body>leak</body></html>",
			".env":       "API_SECRET=super-secret-value\n",
		})
		return io.NopCloser(bytes.NewReader(raw)), nil
	}
	raw := buildTarGzBundle(s.t, map[string]string{
		"index.html": "<!doctype html><html><body><h1>Heya Workspace Schedule Tool</h1></body></html>",
		"app.js":     "console.log('workspace published for " + principal.UserID + "');\n",
	})
	return io.NopCloser(bytes.NewReader(raw)), nil
}

func (s stubE2EWorkspaceBundleSource) BundleConversationArtifact(
	_ context.Context,
	principal domainidentity.Principal,
	sessionID, artifactID, _ string,
) (io.ReadCloser, string, error) {
	if principal.UserID == "" || sessionID == "" || artifactID == "" {
		return nil, "", apperrors.ErrAccessDenied
	}
	raw := buildTarGzBundle(s.t, map[string]string{
		"index.html": "<!doctype html><html><body><h1>Artifact Report Card</h1></body></html>",
	})
	return io.NopCloser(bytes.NewReader(raw)), "对话成果报表", nil
}

func TestB2WorkspaceAppHostingGovernancePortalAndWorkspaceFlow(t *testing.T) {
	env := setupE2EEnv(t)
	t.Cleanup(func() { appaccess.SetRolePermissionMatrix(nil) })
	appaccess.SetRolePermissionKeys("portal-publisher", []string{
		appaccess.PermWorkbenchHomeView,
		appaccess.PermIdentityPortalView,
		appaccess.PermAppHostingAppsView,
		appaccess.PermAppHostingAppsPublish,
		appaccess.PermObserveAIChatUse,
	})
	env.svc.SetWorkspaceBundleSource(stubE2EWorkspaceBundleSource{t: t})

	ctx := context.Background()
	pubClient := sohaapi.NewClient(env.coreServer.URL+"/api/v1", sohaapi.WithBearerToken("tok-pub"))
	collabClient := sohaapi.NewClient(env.coreServer.URL+"/api/v1", sohaapi.WithBearerToken("tok-collab"))
	admin2Client := sohaapi.NewClient(env.coreServer.URL+"/api/v1", sohaapi.WithBearerToken("tok-admin2"))
	bobClient := sohaapi.NewClient(env.coreServer.URL+"/api/v1", sohaapi.WithBearerToken("tok-bob"))
	daveClient := sohaapi.NewClient(env.coreServer.URL+"/api/v1", sohaapi.WithBearerToken("tok-dave"))
	eveClient := sohaapi.NewClient(env.coreServer.URL+"/api/v1", sohaapi.WithBearerToken("tok-eve"))

	verifyOrdinaryPublisherAdminBoundary(t, ctx, pubClient)
	wsApp := verifyWorkspaceAndArtifactDirectDeploy(t, ctx, env, pubClient)
	teamApp := verifyPortalCatalogAudienceMatrix(t, ctx, env, pubClient, bobClient, daveClient, eveClient)
	verifyCollaboratorOwnershipTransferAndDelete(t, ctx, env, pubClient, collabClient, bobClient, wsApp, teamApp)
	verifyOrganizationVisibilityApprovalWorkflow(t, ctx, env, pubClient, admin2Client, eveClient)
}

func verifyOrdinaryPublisherAdminBoundary(t *testing.T, ctx context.Context, pubClient *sohaapi.Client) {
	t.Helper()
	if _, err := pubClient.ListHostedApps(ctx, sohaapi.ListHostedAppsParams{Scope: sohaapi.HostedAppListScopeAll}); err == nil {
		t.Fatal("expected ordinary publisher without admin permission to be rejected on scope=all")
	}
	allow := true
	if _, err := pubClient.UpdateAppHostingSettings(ctx, sohaapi.AppHostingSettingsUpdateRequest{
		Enabled:                     true,
		AppsDomain:                  "apps.local",
		PublicScheme:                "http",
		StorageIntegrationID:        "minio-primary",
		StoragePrefix:               "hosted-apps",
		AllowOrganizationVisibility: &allow,
		MaxBundleCompressedBytes:    10 << 20,
		MaxBundleUncompressedBytes:  25 << 20,
		MaxBundleFileCount:          200,
		MaxAppsPerOwner:             10,
		RetainedRevisionsPerApp:     5,
	}); err == nil {
		t.Fatal("expected ordinary publisher to be rejected when updating platform settings")
	}
}

func verifyWorkspaceAndArtifactDirectDeploy(
	t *testing.T,
	ctx context.Context,
	env *e2eEnv,
	pubClient *sohaapi.Client,
) sohaapi.HostedApp {
	t.Helper()
	// 1. DLP-blocked workspace bundle (.env) must be rejected cleanly.
	if _, err := pubClient.DeployHostedAppFromWorkspace(ctx, sohaapi.WorkspaceHostedAppDeploymentRequest{
		Name:           "含泄漏配置应用",
		IdempotencyKey: "b2-ws-leak-1",
		Source: sohaapi.WorkspaceHostedAppSource{
			Kind:       sohaapi.WorkspaceHostedAppSourceKindPersonalEnvironment,
			SourcePath: "leak-dir",
		},
	}); err == nil {
		t.Fatal("expected workspace deploy containing .env secret file to be blocked")
	}

	// 2. Personal workspace directory deploy + idempotent replay.
	spa := true
	deployReq := sohaapi.WorkspaceHostedAppDeploymentRequest{
		Name:           "排班助手小工具",
		Description:    "个人助手工作区一键发布",
		SpaFallback:    &spa,
		IdempotencyKey: "b2-ws-deploy-1",
		Visibility:     &sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModePrivate},
		Source: sohaapi.WorkspaceHostedAppSource{
			Kind:       sohaapi.WorkspaceHostedAppSourceKindPersonalEnvironment,
			SourcePath: "schedule-tool",
		},
	}
	first, err := pubClient.DeployHostedAppFromWorkspace(ctx, deployReq)
	if err != nil || !first.Created || first.App.Status != sohaapi.HostedAppStatusOnline {
		t.Fatalf("DeployHostedAppFromWorkspace first = (%#v, %v)", first, err)
	}
	replay, err := pubClient.DeployHostedAppFromWorkspace(ctx, deployReq)
	if err != nil || replay.App.ID != first.App.ID || replay.Revision.ID != first.Revision.ID {
		t.Fatalf("DeployHostedAppFromWorkspace replay = (%#v, %v), want (%s, %s)", replay, err, first.App.ID, first.Revision.ID)
	}

	// 3. Conversation artifact deploy.
	artRes, err := pubClient.DeployHostedAppFromWorkspace(ctx, sohaapi.WorkspaceHostedAppDeploymentRequest{
		IdempotencyKey: "b2-art-deploy-1",
		Source: sohaapi.WorkspaceHostedAppSource{
			Kind:       sohaapi.WorkspaceHostedAppSourceKindConversationArtifact,
			SessionID:  "sess-heya-1",
			ArtifactID: "artifact:rep-1",
		},
	})
	if err != nil || artRes.App.Name != "对话成果报表" {
		t.Fatalf("DeployHostedAppFromWorkspace artifact = (%#v, %v)", artRes, err)
	}

	// Author accesses private workspace app via gateway SSO.
	pubHTTP := newLoggedInGatewayBrowser(t, env, "tok-pub", env.gwURL)
	page := followAllRedirects(t, pubHTTP, "http://"+first.App.DNSLabel+".apps.local:"+env.gwPort+"/")
	if page.status != http.StatusOK || !strings.Contains(page.body, "Heya Workspace Schedule Tool") {
		t.Fatalf("author gateway visit to workspace app = (%d, %q)", page.status, page.body)
	}
	return first.App
}

func verifyPortalCatalogAudienceMatrix(
	t *testing.T,
	ctx context.Context,
	env *e2eEnv,
	pubClient, bobClient, daveClient, eveClient *sohaapi.Client,
) sohaapi.HostedApp {
	t.Helper()
	deployQuickStaticApp(t, ctx, pubClient, "林悦私有草稿", "b2-m-priv", sohaapi.HostedAppVisibilityInput{
		Mode: sohaapi.HostedAppVisibilityModePrivate,
	})
	userApp := deployQuickStaticApp(t, ctx, pubClient, "给李博的专项表", "b2-m-bob", sohaapi.HostedAppVisibilityInput{
		Mode:    sohaapi.HostedAppVisibilityModeSelected,
		UserIDs: []string{"user-bob"},
	})
	teamApp := deployQuickStaticApp(t, ctx, pubClient, "数据组周报工具", "b2-m-team", sohaapi.HostedAppVisibilityInput{
		Mode:    sohaapi.HostedAppVisibilityModeSelected,
		TeamIDs: []string{"team-data"},
	})
	orgApp := deployQuickStaticApp(t, ctx, pubClient, "全员假期日历", "b2-m-org", sohaapi.HostedAppVisibilityInput{
		Mode: sohaapi.HostedAppVisibilityModeOrganization,
	})

	// Bob (user-bob, team-data): sees userApp, teamApp, orgApp (3 apps), never private!
	bobCat, err := bobClient.ListPortalHostedApps(ctx, sohaapi.ListPortalHostedAppsParams{AuthorUserID: "user-pub"})
	if err != nil || bobCat.Total != 3 || bobCat.SelectedAuthor == nil || bobCat.SelectedAuthor.VisibleAppCount != 3 {
		t.Fatalf("bob portal catalog = (%#v, %v), want 3 visible apps", bobCat, err)
	}
	assertCatalogIDs(t, bobCat.Items, userApp.ID, teamApp.ID, orgApp.ID)

	// Dave (team-data, not user-bob): sees teamApp & orgApp (2 apps).
	daveCat, err := daveClient.ListPortalHostedApps(ctx, sohaapi.ListPortalHostedAppsParams{Query: "林悦"})
	if err != nil || daveCat.Total != 2 {
		t.Fatalf("dave portal search by author = (%#v, %v), want 2 apps", daveCat, err)
	}
	assertCatalogIDs(t, daveCat.Items, teamApp.ID, orgApp.ID)

	// Eve (team-sales): sees only orgApp (1 app).
	eveCat, err := eveClient.ListPortalHostedApps(ctx, sohaapi.ListPortalHostedAppsParams{AuthorUserID: "user-pub"})
	if err != nil || eveCat.Total != 1 {
		t.Fatalf("eve portal catalog = (%#v, %v), want 1 org app", eveCat, err)
	}
	assertCatalogIDs(t, eveCat.Items, orgApp.ID)

	// Narrow teamApp back to private -> immediately vanishes from Dave's portal AND revokes gateway access!
	daveHTTP := newLoggedInGatewayBrowser(t, env, "tok-dave", env.gwURL)
	teamHost := teamApp.DNSLabel + ".apps.local:" + env.gwPort
	if got := followAllRedirects(t, daveHTTP, "http://"+teamHost+"/"); got.status != http.StatusOK {
		t.Fatalf("dave initial teamApp gateway visit = %d, want 200", got.status)
	}
	if _, err := pubClient.UpdateHostedAppVisibility(ctx, teamApp.ID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModePrivate},
	}); err != nil {
		t.Fatalf("narrow teamApp to private: %v", err)
	}
	daveAfter, err := daveClient.ListPortalHostedApps(ctx, sohaapi.ListPortalHostedAppsParams{AuthorUserID: "user-pub"})
	if err != nil || daveAfter.Total != 1 {
		t.Fatalf("dave portal after narrowing = (%#v, %v), want 1", daveAfter, err)
	}
	if got := followAllRedirects(t, daveHTTP, "http://"+teamHost+"/"); got.status != http.StatusForbidden {
		t.Fatalf("dave gateway after narrowing = %d, want 403", got.status)
	}
	return teamApp
}

func verifyCollaboratorOwnershipTransferAndDelete(
	t *testing.T,
	ctx context.Context,
	env *e2eEnv,
	pubClient, collabClient, bobClient *sohaapi.Client,
	wsApp, deletableApp sohaapi.HostedApp,
) {
	t.Helper()
	// Add user-collab as maintainer on private wsApp.
	withCollab, err := pubClient.UpdateHostedAppMaintainers(ctx, wsApp.ID, sohaapi.UpdateHostedAppMaintainersRequest{
		MaintainerUserIDs: []string{"user-collab"},
	})
	if err != nil || len(withCollab.MaintainerUserIDs) != 1 {
		t.Fatalf("UpdateHostedAppMaintainers = (%#v, %v)", withCollab, err)
	}

	// Collaborator does NOT gain visitor access to private app on gateway!
	collabHTTP := newLoggedInGatewayBrowser(t, env, "tok-collab", env.gwURL)
	wsHost := wsApp.DNSLabel + ".apps.local:" + env.gwPort
	if got := followAllRedirects(t, collabHTTP, "http://"+wsHost+"/"); got.status != http.StatusForbidden {
		t.Fatalf("collaborator gateway visit to private app = %d, want 403", got.status)
	}

	// Collaborator CAN upload v2 and rollback to v1, CANNOT change visibility or delete!
	v1ID := wsApp.ActiveRevisionID
	v2Bundle := buildTarGzBundle(t, map[string]string{"index.html": "<!doctype html><html><body>v2-by-collab</body></html>"})
	v2Receipt, err := collabClient.UploadHostedAppRevision(ctx, wsApp.ID, bytes.NewReader(v2Bundle), sohaapi.UploadHostedAppRevisionParams{
		IdempotencyKey: "b2-collab-v2",
	})
	if err != nil || v2Receipt.Revision.RevisionNumber != 2 {
		t.Fatalf("collaborator UploadHostedAppRevision = (%#v, %v)", v2Receipt, err)
	}
	if _, err := collabClient.UpdateHostedAppVisibility(ctx, wsApp.ID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeOrganization},
	}); err == nil {
		t.Fatal("collaborator must not be allowed to change visibility")
	}
	if _, err := collabClient.DeleteHostedApp(ctx, wsApp.ID, sohaapi.DeleteHostedAppRequest{ConfirmationName: wsApp.Name}); err == nil {
		t.Fatal("collaborator must not be allowed to delete app")
	}
	if _, err := collabClient.RollbackHostedApp(ctx, wsApp.ID, sohaapi.HostedAppRollbackRequest{
		TargetRevisionID: v1ID,
		IdempotencyKey:   "b2-collab-rb-1",
	}); err != nil {
		t.Fatalf("collaborator RollbackHostedApp: %v", err)
	}

	// Warm author session on both gateway replicas, then admin transfers ownership to user-bob!
	pubJar, _ := cookiejar.New(nil)
	seedCoreLoginCookie(pubJar, env.coreURL, "tok-pub")
	pubGW1 := env.newBrowserClientForGateway(t, pubJar, env.gwURL)
	pubGW2 := env.newBrowserClientForGateway(t, pubJar, env.gwURL2)
	if got := followAllRedirects(t, pubGW1, "http://"+wsHost+"/"); got.status != http.StatusOK {
		t.Fatalf("old owner pre-transfer GW1 = %d, want 200", got.status)
	}

	transferred, err := env.adminClient.TransferHostedAppOwnership(ctx, wsApp.ID, sohaapi.TransferHostedAppOwnershipRequest{
		NewOwnerUserID: "user-bob",
		Reason:         "原作者调岗交接",
	})
	if err != nil || transferred.OwnerUserID != "user-bob" {
		t.Fatalf("TransferHostedAppOwnership = (%#v, %v)", transferred, err)
	}
	if got := followAllRedirects(t, pubGW1, "http://"+wsHost+"/"); got.status != http.StatusForbidden {
		t.Fatalf("old owner post-transfer GW1 = %d, want 403", got.status)
	}
	if got := followAllRedirects(t, pubGW2, "http://"+wsHost+"/"); got.status != http.StatusForbidden {
		t.Fatalf("old owner post-transfer GW2 = %d, want 403", got.status)
	}
	bobHTTP := newLoggedInGatewayBrowser(t, env, "tok-bob", env.gwURL)
	if got := followAllRedirects(t, bobHTTP, "http://"+wsHost+"/"); got.status != http.StatusOK {
		t.Fatalf("new owner bob gateway visit = %d, want 200", got.status)
	}
	_ = bobClient

	// Confirmed deletion on deletableApp: wrong name rejected, exact name purges app + S3 objects!
	delHost := deletableApp.DNSLabel + ".apps.local:" + env.gwPort
	if _, err := pubClient.DeleteHostedApp(ctx, deletableApp.ID, sohaapi.DeleteHostedAppRequest{
		ConfirmationName: "错误名称",
	}); err == nil {
		t.Fatal("expected DeleteHostedApp with mismatched confirmationName to fail")
	}
	delRes, err := pubClient.DeleteHostedApp(ctx, deletableApp.ID, sohaapi.DeleteHostedAppRequest{
		ConfirmationName: deletableApp.Name,
	})
	if err != nil || !delRes.Deleted {
		t.Fatalf("DeleteHostedApp = (%#v, %v)", delRes, err)
	}
	if got := followAllRedirects(t, pubGW1, "http://"+delHost+"/"); got.status != http.StatusNotFound {
		t.Fatalf("deleted app gateway visit = %d, want 404", got.status)
	}
}

func verifyOrganizationVisibilityApprovalWorkflow(
	t *testing.T,
	ctx context.Context,
	env *e2eEnv,
	pubClient, admin2Client, eveClient *sohaapi.Client,
) {
	t.Helper()
	enableRequireApprovalAndVerifyAntiSelfApproval(t, ctx, env)
	verifyApprovalInvalidationWithdrawAndApprove(t, ctx, env, pubClient, admin2Client, eveClient)
}

func enableRequireApprovalAndVerifyAntiSelfApproval(t *testing.T, ctx context.Context, env *e2eEnv) {
	t.Helper()
	policy := sohaapi.AppHostingOrganizationVisibilityPolicyRequireApproval
	if _, err := env.adminClient.UpdateAppHostingSettings(ctx, sohaapi.AppHostingSettingsUpdateRequest{
		Enabled:                      true,
		AppsDomain:                   "apps.local",
		PublicScheme:                 "http",
		StorageIntegrationID:         "minio-primary",
		StoragePrefix:                "hosted-apps",
		OrganizationVisibilityPolicy: policy,
		MaxBundleCompressedBytes:     10 << 20,
		MaxBundleUncompressedBytes:   25 << 20,
		MaxBundleFileCount:           200,
		MaxAppsPerOwner:              10,
		RetainedRevisionsPerApp:      5,
	}); err != nil {
		t.Fatalf("enable require_approval policy: %v", err)
	}

	adminOrgApp := deployQuickStaticApp(t, ctx, env.adminClient, "管理员自申看板", "b2-self-apr", sohaapi.HostedAppVisibilityInput{
		Mode: sohaapi.HostedAppVisibilityModeOrganization,
	})
	if adminOrgApp.Visibility.Mode != sohaapi.HostedAppVisibilityModePrivate || adminOrgApp.PendingVisibilityApproval == nil {
		t.Fatalf("adminOrgApp under require_approval = %#v, want private + pending approval", adminOrgApp)
	}
	if _, err := env.adminClient.ApproveHostedAppVisibility(ctx, adminOrgApp.PendingVisibilityApproval.ID, nil); err == nil {
		t.Fatal("expected self-approval by requester to be rejected")
	}
}

func verifyApprovalInvalidationWithdrawAndApprove(
	t *testing.T,
	ctx context.Context,
	env *e2eEnv,
	pubClient, admin2Client, eveClient *sohaapi.Client,
) {
	t.Helper()
	cand := deployQuickStaticApp(t, ctx, pubClient, "全公司报销助手", "b2-apr-flow", sohaapi.HostedAppVisibilityInput{
		Mode: sohaapi.HostedAppVisibilityModePrivate,
	})
	candHost := cand.DNSLabel + ".apps.local:" + env.gwPort
	eveHTTP := newLoggedInGatewayBrowser(t, env, "tok-eve", env.gwURL)

	req1, err := pubClient.UpdateHostedAppVisibility(ctx, cand.ID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeOrganization},
		Reason:     "供全员查询报销标准",
	})
	if err != nil || req1.Visibility.Mode != sohaapi.HostedAppVisibilityModePrivate || req1.PendingVisibilityApproval == nil {
		t.Fatalf("request org approval 1 = (%#v, %v)", req1, err)
	}
	if got := followAllRedirects(t, eveHTTP, "http://"+candHost+"/"); got.status != http.StatusForbidden {
		t.Fatalf("eve visit while pending approval = %d, want 403", got.status)
	}

	if _, err := pubClient.UpdateHostedAppVisibility(ctx, cand.ID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeSelected, TeamIDs: []string{"team-data"}},
	}); err != nil {
		t.Fatalf("change visibility to selected: %v", err)
	}
	if _, err := admin2Client.ApproveHostedAppVisibility(ctx, req1.PendingVisibilityApproval.ID, nil); err == nil {
		t.Fatal("expected late approval after visibility change to be rejected")
	}

	req2, err := pubClient.UpdateHostedAppVisibility(ctx, cand.ID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeOrganization},
	})
	if err != nil || req2.PendingVisibilityApproval == nil {
		t.Fatalf("request org approval 2 = (%#v, %v)", req2, err)
	}
	withdrawn, err := pubClient.WithdrawHostedAppVisibilityApproval(ctx, cand.ID, &sohaapi.HostedAppLifecycleActionRequest{Reason: "补充材料后再提"})
	if err != nil || withdrawn.PendingVisibilityApproval != nil {
		t.Fatalf("withdraw approval = (%#v, %v)", withdrawn, err)
	}

	approveAndVerifyOrganizationLive(t, ctx, pubClient, admin2Client, eveClient, eveHTTP, cand.ID, candHost)
}

func approveAndVerifyOrganizationLive(
	t *testing.T,
	ctx context.Context,
	pubClient, admin2Client, eveClient *sohaapi.Client,
	eveHTTP *http.Client,
	appID, candHost string,
) {
	t.Helper()
	req3, err := pubClient.UpdateHostedAppVisibility(ctx, appID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeOrganization},
		Reason:     "材料齐全申请公开",
	})
	if err != nil || req3.PendingVisibilityApproval == nil {
		t.Fatalf("request org approval 3 = (%#v, %v)", req3, err)
	}
	approved, err := admin2Client.ApproveHostedAppVisibility(ctx, req3.PendingVisibilityApproval.ID, &sohaapi.ApproveHostedAppVisibilityRequest{
		Comment: "审核通过",
	})
	if err != nil || approved.App.Visibility.Mode != sohaapi.HostedAppVisibilityModeOrganization {
		t.Fatalf("ApproveHostedAppVisibility = (%#v, %v)", approved, err)
	}
	if _, err := admin2Client.ApproveHostedAppVisibility(ctx, req3.PendingVisibilityApproval.ID, nil); err == nil {
		t.Fatal("expected duplicate approval to be rejected")
	}
	if got := followAllRedirects(t, eveHTTP, "http://"+candHost+"/"); got.status != http.StatusOK {
		t.Fatalf("eve gateway visit after approval = %d, want 200", got.status)
	}
	evePortal, err := eveClient.ListPortalHostedApps(ctx, sohaapi.ListPortalHostedAppsParams{Query: "全公司报销助手"})
	if err != nil || evePortal.Total != 1 {
		t.Fatalf("eve portal search after approval = (%#v, %v), want 1", evePortal, err)
	}
}

func deployQuickStaticApp(
	t *testing.T,
	ctx context.Context,
	client *sohaapi.Client,
	name, idemPrefix string,
	vis sohaapi.HostedAppVisibilityInput,
) sohaapi.HostedApp {
	t.Helper()
	created, err := client.CreateHostedApp(ctx, sohaapi.CreateHostedAppRequest{
		Name:           name,
		Kind:           sohaapi.HostedAppKindStatic,
		Visibility:     &vis,
		IdempotencyKey: idemPrefix + "-create",
	})
	if err != nil {
		t.Fatalf("CreateHostedApp(%s): %v", name, err)
	}
	bundle := buildTarGzBundle(t, map[string]string{
		"index.html": "<!doctype html><html><body><h1>" + name + "</h1></body></html>",
	})
	receipt, err := client.UploadHostedAppRevision(ctx, created.ID, bytes.NewReader(bundle), sohaapi.UploadHostedAppRevisionParams{
		IdempotencyKey: idemPrefix + "-rev1",
	})
	if err != nil {
		t.Fatalf("UploadHostedAppRevision(%s): %v", name, err)
	}
	return receipt.App
}

func newLoggedInGatewayBrowser(t *testing.T, env *e2eEnv, coreToken string, targetGW *url.URL) *http.Client {
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	seedCoreLoginCookie(jar, env.coreURL, coreToken)
	return env.newBrowserClientForGateway(t, jar, targetGW)
}

func assertCatalogIDs(t *testing.T, items []sohaapi.PortalHostedAppItem, wantIDs ...string) {
	t.Helper()
	if len(items) != len(wantIDs) {
		t.Fatalf("catalog items len = %d, want %d (%v)", len(items), len(wantIDs), wantIDs)
	}
	gotSet := make(map[string]bool, len(items))
	for _, it := range items {
		gotSet[it.ID] = true
	}
	for _, id := range wantIDs {
		if !gotSet[id] {
			t.Fatalf("catalog items missing %q; got %v", id, gotSet)
		}
	}
}
