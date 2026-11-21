package apphosting_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	appapphosting "github.com/opensoha/soha/internal/application/apphosting"
	domainapphosting "github.com/opensoha/soha/internal/domain/apphosting"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	apphostingrepo "github.com/opensoha/soha/internal/repository/apphosting"
)

type b2TestHarness struct {
	*testHarness
	perms     *stubPermissions
	employee  domainidentity.Principal
	adminPeer domainidentity.Principal
}

func newB2TestHarness(t *testing.T) *b2TestHarness {
	t.Helper()
	repo := apphostingrepo.NewMemoryRepository()
	store := newStubObjectStore()
	audience := apphostingrepo.NewMemoryAudienceDirectory()
	sessions := newStubSessionValidator()
	audit := &stubAudit{}
	now := time.Date(2026, 11, 21, 10, 0, 0, 0, time.UTC)
	sessions.Set("sess_alice", "usr_alice", true, now.Add(6*time.Hour))
	sessions.Set("sess_bob", "usr_bob", true, now.Add(6*time.Hour))
	perms := seedB2PermissionsAndAudience(audience)

	svc, err := appapphosting.New(
		repo,
		store,
		audience,
		perms,
		audit,
		appapphosting.WithFallbackGatewayToken("bootstrap-gw-token"),
		appapphosting.WithSessionValidator(sessions),
		appapphosting.WithVisitorIDSecret("b2-visitor-hmac-secret-key"),
		appapphosting.WithClock(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatal(err)
	}

	base := &testHarness{
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
		carol:    domainidentity.Principal{UserID: "usr_auditor", UserName: "auditor"},
	}

	if _, err := svc.UpdateSettings(t.Context(), base.admin, sohaapi.AppHostingSettingsUpdateRequest{
		Enabled:                 true,
		AppsDomain:              "apps.local",
		PublicScheme:            "http",
		StorageIntegrationID:    "int_minio_1",
		StoragePrefix:           "hosted",
		RetainedRevisionsPerApp: 5,
		MaxAppsPerOwner:         10,
	}); err != nil {
		t.Fatalf("UpdateSettings setup: %v", err)
	}

	return &b2TestHarness{
		testHarness: base,
		perms:       perms,
		employee:    domainidentity.Principal{UserID: "usr_emp", UserName: "emp", Teams: []string{"team_fin"}},
		adminPeer:   domainidentity.Principal{UserID: "usr_admin2", UserName: "admin2"},
	}
}

func seedB2PermissionsAndAudience(audience *apphostingrepo.MemoryAudienceDirectory) *stubPermissions {
	audience.UpsertTeam(sohaapi.AppHostingAudienceTeam{ID: "team_eng", Name: "Engineering", Slug: "eng"})
	audience.UpsertTeam(sohaapi.AppHostingAudienceTeam{ID: "team_fin", Name: "Finance", Slug: "fin"})
	audience.UpsertTeam(sohaapi.AppHostingAudienceTeam{ID: "team_hr", Name: "People Ops", Slug: "hr"})

	audience.UpsertUser(apphostingrepo.MemoryUser{ID: "usr_admin", Username: "admin", DisplayName: "Chief Admin", Status: "active", TeamIDs: []string{"team_eng"}})
	audience.UpsertUser(apphostingrepo.MemoryUser{ID: "usr_admin2", Username: "admin2", DisplayName: "Peer Admin", Status: "active", TeamIDs: []string{"team_eng"}})
	audience.UpsertUser(apphostingrepo.MemoryUser{ID: "usr_alice", Username: "alice", DisplayName: "Alice Zhang", AvatarURL: "/avatars/alice.png", Status: "active", TeamIDs: []string{"team_eng"}})
	audience.UpsertUser(apphostingrepo.MemoryUser{ID: "usr_bob", Username: "bob", DisplayName: "Bob Chen", Status: "active", TeamIDs: []string{"team_fin"}})
	audience.UpsertUser(apphostingrepo.MemoryUser{ID: "usr_emp", Username: "emp", DisplayName: "Eva Lin", Status: "active", TeamIDs: []string{"team_fin"}})
	audience.UpsertUser(apphostingrepo.MemoryUser{ID: "usr_silent", Username: "silent", DisplayName: "Silent Colleague", Status: "active", TeamIDs: []string{"team_hr"}})

	return &stubPermissions{
		grants: map[string][]string{
			"usr_admin": {
				appaccess.PermAppHostingAdminManage, appaccess.PermAppHostingAdminView,
				appaccess.PermAppHostingAppsPublish, appaccess.PermAppHostingAppsView, appaccess.PermIdentityPortalView,
			},
			"usr_admin2":  {appaccess.PermAppHostingAdminManage, appaccess.PermAppHostingAdminView, appaccess.PermIdentityPortalView},
			"usr_auditor": {appaccess.PermAppHostingAdminView, appaccess.PermIdentityPortalView},
			"usr_alice":   {appaccess.PermAppHostingAppsPublish, appaccess.PermAppHostingAppsView, appaccess.PermObserveAIChatUse, appaccess.PermIdentityPortalView},
			"usr_bob":     {appaccess.PermAppHostingAppsPublish, appaccess.PermAppHostingAppsView, appaccess.PermObserveAIChatUse, appaccess.PermIdentityPortalView},
			"usr_emp":     {appaccess.PermIdentityPortalView},
		},
	}
}

func setOrgVisibilityPolicy(t *testing.T, ctx context.Context, h *b2TestHarness, policy sohaapi.AppHostingOrganizationVisibilityPolicy) sohaapi.AppHostingSettings {
	t.Helper()
	got, err := h.svc.UpdateSettings(ctx, h.admin, sohaapi.AppHostingSettingsUpdateRequest{
		Enabled:                      true,
		AppsDomain:                   "apps.local",
		PublicScheme:                 "http",
		StorageIntegrationID:         "int_minio_1",
		StoragePrefix:                "hosted",
		RetainedRevisionsPerApp:      5,
		MaxAppsPerOwner:              10,
		OrganizationVisibilityPolicy: policy,
	})
	if err != nil {
		t.Fatalf("UpdateSettings(%s): %v", policy, err)
	}
	return got
}

func TestVisibilityApprovalPolicyStateMachineAndCounterexamples(t *testing.T) {
	h := newB2TestHarness(t)
	ctx := t.Context()

	assertPolicyBidirectionalSyncAndDisabledRejection(t, ctx, h)
	assertWithdrawRejectSelfApprovalAndNarrowInvalidation(t, ctx, h)
	assertPolicyChangeRouteDriftAndRequesterEligibility(t, ctx, h)
}

func assertPolicyBidirectionalSyncAndDisabledRejection(t *testing.T, ctx context.Context, h *b2TestHarness) {
	t.Helper()
	s1 := setOrgVisibilityPolicy(t, ctx, h, sohaapi.AppHostingOrganizationVisibilityPolicyDisabled)
	if s1.AllowOrganizationVisibility || s1.OrganizationVisibilityPolicy != sohaapi.AppHostingOrganizationVisibilityPolicyDisabled || s1.PolicyVersion < 2 {
		t.Fatalf("disabled settings mismatch: %#v", s1)
	}
	if _, _, err := h.svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{
		Name:       "Blocked Org App",
		Visibility: &sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeOrganization},
	}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("CreateApp under disabled policy = %v, want ErrAccessDenied", err)
	}

	legacyTrue := true
	s2, err := h.svc.UpdateSettings(ctx, h.admin, sohaapi.AppHostingSettingsUpdateRequest{
		Enabled:                     true,
		AppsDomain:                  "apps.local",
		PublicScheme:                "http",
		StorageIntegrationID:        "int_minio_1",
		AllowOrganizationVisibility: &legacyTrue,
	})
	if err != nil || s2.OrganizationVisibilityPolicy != sohaapi.AppHostingOrganizationVisibilityPolicyAllowDirect || !s2.AllowOrganizationVisibility {
		t.Fatalf("legacy allowOrganizationVisibility=true sync failed: %#v (%v)", s2, err)
	}
}

func assertWithdrawRejectSelfApprovalAndNarrowInvalidation(t *testing.T, ctx context.Context, h *b2TestHarness) {
	t.Helper()
	appID := assertCreateUploadIdempotentAndWithdrawApproval(t, ctx, h)
	assertRejectPreservesScopeAndSelfApprovalForbidden(t, ctx, h, appID)
}

func assertCreateUploadIdempotentAndWithdrawApproval(t *testing.T, ctx context.Context, h *b2TestHarness) string {
	t.Helper()
	s := setOrgVisibilityPolicy(t, ctx, h, sohaapi.AppHostingOrganizationVisibilityPolicyRequireApproval)

	created, _, err := h.svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{
		Name:       "Quarterly Planner",
		Visibility: &sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeOrganization},
	})
	if err != nil {
		t.Fatalf("CreateApp under require_approval: %v", err)
	}
	if created.Visibility.Mode != sohaapi.HostedAppVisibilityModePrivate || created.PendingVisibilityApproval == nil || created.PendingVisibilityApproval.PolicyVersion != s.PolicyVersion {
		t.Fatalf("expected private effective visibility + pending approval, got %#v", created)
	}
	v1 := makeTarGz(t, []tarEntrySpec{{name: "index.html", body: "<!doctype html>v1"}})
	up1, err := h.svc.UploadRevision(ctx, h.alice, created.ID, domainapphosting.UploadRevisionInput{Description: "v1"}, bytes.NewReader(v1))
	if err != nil || up1.App.PendingVisibilityApproval == nil || up1.App.PendingVisibilityApproval.BaseRouteGeneration != up1.App.RouteGeneration {
		t.Fatalf("first upload must align draft pending approval baseRouteGeneration: %#v (%v)", up1, err)
	}

	dupVis, err := h.svc.UpdateVisibility(ctx, h.alice, created.ID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeOrganization},
		Reason:     "updated justification",
	})
	if err != nil || dupVis.PendingVisibilityApproval == nil || dupVis.PendingVisibilityApproval.ID != created.PendingVisibilityApproval.ID {
		t.Fatalf("duplicate pending visibility request did not reuse pending approval: %#v (%v)", dupVis, err)
	}

	withdrawn, err := h.svc.WithdrawVisibilityApproval(ctx, h.alice, created.ID, "not ready yet")
	if err != nil || withdrawn.PendingVisibilityApproval != nil {
		t.Fatalf("WithdrawVisibilityApproval = %#v, %v", withdrawn, err)
	}
	if _, err := h.svc.WithdrawVisibilityApproval(ctx, h.alice, created.ID, "again"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("duplicate withdraw = %v, want ErrConflict", err)
	}
	return created.ID
}

func assertRejectPreservesScopeAndSelfApprovalForbidden(t *testing.T, ctx context.Context, h *b2TestHarness, appID string) {
	t.Helper()
	if _, err := h.svc.UpdateVisibility(ctx, h.alice, appID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeSelected, TeamIDs: []string{"team_fin"}},
	}); err != nil {
		t.Fatal(err)
	}
	req2, err := h.svc.UpdateVisibility(ctx, h.alice, appID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeOrganization},
		Reason:     "share company-wide",
	})
	if err != nil || req2.PendingVisibilityApproval == nil {
		t.Fatalf("re-request approval: %#v (%v)", req2, err)
	}
	if _, err := h.svc.RejectVisibilityApproval(ctx, h.admin, req2.PendingVisibilityApproval.ID, "   "); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("empty reject comment = %v, want ErrInvalidArgument", err)
	}
	rejected, err := h.svc.RejectVisibilityApproval(ctx, h.admin, req2.PendingVisibilityApproval.ID, "please add usage disclaimer")
	if err != nil || rejected.Approval.Status != sohaapi.HostedAppVisibilityApprovalStatusRejected || rejected.App.Visibility.Mode != sohaapi.HostedAppVisibilityModeSelected {
		t.Fatalf("RejectVisibilityApproval = %#v, %v", rejected, err)
	}

	adminApp, _, err := h.svc.CreateApp(ctx, h.admin, sohaapi.CreateHostedAppRequest{
		Name:       "Admin Self App",
		Visibility: &sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeOrganization},
	})
	if err != nil || adminApp.PendingVisibilityApproval == nil {
		t.Fatalf("admin CreateApp: %#v (%v)", adminApp, err)
	}
	if _, err := h.svc.ApproveVisibilityApproval(ctx, h.admin, adminApp.PendingVisibilityApproval.ID, "lgtm to myself"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("self-approval = %v, want ErrAccessDenied", err)
	}
	narrowed, err := h.svc.UpdateVisibility(ctx, h.admin, adminApp.ID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModePrivate},
	})
	if err != nil || narrowed.PendingVisibilityApproval != nil {
		t.Fatalf("narrowing to private should clear pending approval: %#v (%v)", narrowed, err)
	}
	history, err := h.svc.ListVisibilityApprovals(ctx, h.carol, sohaapi.ListHostedAppVisibilityApprovalsParams{
		Scope: sohaapi.HostedAppApprovalListScopeAll,
		AppID: adminApp.ID,
	})
	if err != nil || len(history.Items) != 1 || history.Items[0].Status != sohaapi.HostedAppVisibilityApprovalStatusInvalidated || history.Items[0].InvalidationReason != "visibility_changed" {
		t.Fatalf("expected invalidated visibility_changed record, got %#v (%v)", history, err)
	}
}

func assertPolicyChangeRouteDriftAndRequesterEligibility(t *testing.T, ctx context.Context, h *b2TestHarness) {
	t.Helper()
	appID := assertPolicyChangeAndRouteDriftInvalidation(t, ctx, h)
	assertRequesterEligibilityAndApprovalSuccess(t, ctx, h, appID)
}

func assertPolicyChangeAndRouteDriftInvalidation(t *testing.T, ctx context.Context, h *b2TestHarness) string {
	t.Helper()
	app, _, err := h.svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{Name: "Ops Metrics"})
	if err != nil {
		t.Fatal(err)
	}
	v1 := makeTarGz(t, []tarEntrySpec{{name: "index.html", body: "<!doctype html>v1"}})
	if _, err := h.svc.UploadRevision(ctx, h.alice, app.ID, domainapphosting.UploadRevisionInput{}, bytes.NewReader(v1)); err != nil {
		t.Fatal(err)
	}

	pendingPolicy, err := h.svc.UpdateVisibility(ctx, h.alice, app.ID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeOrganization},
	})
	if err != nil || pendingPolicy.PendingVisibilityApproval == nil {
		t.Fatal(err)
	}
	policyApprovalID := pendingPolicy.PendingVisibilityApproval.ID
	setOrgVisibilityPolicy(t, ctx, h, sohaapi.AppHostingOrganizationVisibilityPolicyDisabled)
	setOrgVisibilityPolicy(t, ctx, h, sohaapi.AppHostingOrganizationVisibilityPolicyRequireApproval)
	if _, err := h.svc.ApproveVisibilityApproval(ctx, h.admin, policyApprovalID, "too late"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("approve after policy change = %v, want ErrConflict", err)
	}

	pendingDrift, err := h.svc.UpdateVisibility(ctx, h.alice, app.ID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeOrganization},
	})
	if err != nil || pendingDrift.PendingVisibilityApproval == nil {
		t.Fatal(err)
	}
	driftApprovalID := pendingDrift.PendingVisibilityApproval.ID
	v2 := makeTarGz(t, []tarEntrySpec{{name: "index.html", body: "<!doctype html>v2-unreviewed"}})
	up2, err := h.svc.UploadRevision(ctx, h.alice, app.ID, domainapphosting.UploadRevisionInput{}, bytes.NewReader(v2))
	if err != nil {
		t.Fatal(err)
	}
	if up2.App.PendingVisibilityApproval != nil {
		t.Fatalf("online UploadRevision must immediately clear PendingVisibilityApproval, got %#v", up2.App.PendingVisibilityApproval)
	}
	pendingQueue, err := h.svc.ListVisibilityApprovals(ctx, h.admin, sohaapi.ListHostedAppVisibilityApprovalsParams{
		Scope:  sohaapi.HostedAppApprovalListScopeAll,
		Status: sohaapi.HostedAppVisibilityApprovalStatusPending,
		AppID:  app.ID,
	})
	if err != nil || len(pendingQueue.Items) != 0 {
		t.Fatalf("expected 0 pending approvals after online UploadRevision, got %#v (%v)", pendingQueue, err)
	}
	if _, err := h.svc.ApproveVisibilityApproval(ctx, h.admin, driftApprovalID, "approve stale"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("approve after routeGeneration drift = %v, want ErrConflict", err)
	}
	if _, err := h.svc.RejectVisibilityApproval(ctx, h.admin, driftApprovalID, "reject stale"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("reject after routeGeneration drift = %v, want ErrConflict", err)
	}
	driftList, err := h.svc.ListVisibilityApprovals(ctx, h.alice, sohaapi.ListHostedAppVisibilityApprovalsParams{
		Scope:  sohaapi.HostedAppApprovalListScopeMine,
		Status: sohaapi.HostedAppVisibilityApprovalStatusInvalidated,
		AppID:  app.ID,
	})
	if err != nil || !slices.ContainsFunc(driftList.Items, func(a sohaapi.HostedAppVisibilityApproval) bool {
		return a.ID == driftApprovalID && a.InvalidationReason == "route_generation_changed"
	}) {
		t.Fatalf("expected persisted route_generation_changed invalidation, got %#v (%v)", driftList, err)
	}
	assertRollbackAndSuspendInvalidatePendingApprovals(t, ctx, h, app.ID, up2.Revision.ID)
	return app.ID
}

func assertRollbackAndSuspendInvalidatePendingApprovals(t *testing.T, ctx context.Context, h *b2TestHarness, appID, targetRevID string) {
	t.Helper()
	pendingRb, err := h.svc.UpdateVisibility(ctx, h.alice, appID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeOrganization},
	})
	if err != nil || pendingRb.PendingVisibilityApproval == nil {
		t.Fatal(err)
	}
	rbRes, err := h.svc.Rollback(ctx, h.alice, appID, sohaapi.HostedAppRollbackRequest{TargetRevisionID: targetRevID})
	if err != nil || rbRes.App.PendingVisibilityApproval != nil {
		t.Fatalf("Rollback must immediately clear PendingVisibilityApproval: %#v (%v)", rbRes, err)
	}

	pendingSusp, err := h.svc.UpdateVisibility(ctx, h.alice, appID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeOrganization},
	})
	if err != nil || pendingSusp.PendingVisibilityApproval == nil {
		t.Fatal(err)
	}
	suspApprovalID := pendingSusp.PendingVisibilityApproval.ID
	suspended, err := h.svc.SuspendApp(ctx, h.admin, appID, "security review")
	if err != nil || suspended.PendingVisibilityApproval != nil {
		t.Fatalf("SuspendApp must immediately clear PendingVisibilityApproval: %#v (%v)", suspended, err)
	}
	invList, err := h.svc.ListVisibilityApprovals(ctx, h.admin, sohaapi.ListHostedAppVisibilityApprovalsParams{
		Scope:  sohaapi.HostedAppApprovalListScopeAll,
		Status: sohaapi.HostedAppVisibilityApprovalStatusInvalidated,
		AppID:  appID,
	})
	if err != nil || !slices.ContainsFunc(invList.Items, func(a sohaapi.HostedAppVisibilityApproval) bool {
		return a.ID == suspApprovalID && a.InvalidationReason == "app_suspended"
	}) {
		t.Fatalf("expected app_suspended invalidation, got %#v (%v)", invList, err)
	}
	if _, err := h.svc.UnsuspendApp(ctx, h.admin, appID, "cleared"); err != nil {
		t.Fatal(err)
	}
	if _, err := h.svc.BringOnline(ctx, h.alice, appID, "resume"); err != nil {
		t.Fatal(err)
	}
}

func assertRequesterEligibilityAndApprovalSuccess(t *testing.T, ctx context.Context, h *b2TestHarness, appID string) {
	t.Helper()
	pendingValid, err := h.svc.UpdateVisibility(ctx, h.alice, appID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeOrganization},
		Reason:     "reviewed v2",
	})
	if err != nil || pendingValid.PendingVisibilityApproval == nil {
		t.Fatal(err)
	}
	validID := pendingValid.PendingVisibilityApproval.ID
	origGrants := slices.Clone(h.perms.grants["usr_alice"])
	h.perms.grants["usr_alice"] = []string{appaccess.PermIdentityPortalView}
	if _, err := h.svc.ApproveVisibilityApproval(ctx, h.admin, validID, "check perms"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("approve when requester lost publish perm = %v, want ErrConflict", err)
	}
	h.perms.grants["usr_alice"] = origGrants

	genBefore := pendingValid.RouteGeneration
	approved, err := h.svc.ApproveVisibilityApproval(ctx, h.adminPeer, validID, "approved for all employees")
	if err != nil {
		t.Fatalf("ApproveVisibilityApproval: %v", err)
	}
	if approved.App.Visibility.Mode != sohaapi.HostedAppVisibilityModeOrganization || approved.App.RouteGeneration <= genBefore || approved.Approval.Status != sohaapi.HostedAppVisibilityApprovalStatusApproved {
		t.Fatalf("unexpected approved state: %#v", approved)
	}
	if _, err := h.svc.ApproveVisibilityApproval(ctx, h.admin, validID, "double approve"); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("duplicate approve = %v, want ErrConflict", err)
	}
}

func TestMaintainersOwnershipTransferMidUploadRevocationAndDelete(t *testing.T) {
	h := newB2TestHarness(t)
	ctx := t.Context()

	appID, rev1ID := assertMaintainerValidationAndGatewayIsolation(t, ctx, h)
	rev2ID := assertMidUploadActorRevocationAndOwnershipTransfer(t, ctx, h, appID, rev1ID)
	assertConfirmedDeleteAndObjectCleanup(t, ctx, h, appID, rev2ID)
}

func assertMaintainerValidationAndGatewayIsolation(t *testing.T, ctx context.Context, h *b2TestHarness) (string, string) {
	t.Helper()
	setOrgVisibilityPolicy(t, ctx, h, sohaapi.AppHostingOrganizationVisibilityPolicyRequireApproval)

	app, _, err := h.svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{Name: "Release Console"})
	if err != nil {
		t.Fatal(err)
	}
	// Owner self-inclusion rejected.
	if _, err := h.svc.UpdateMaintainers(ctx, h.alice, app.ID, sohaapi.UpdateHostedAppMaintainersRequest{
		MaintainerUserIDs: []string{"usr_alice"},
	}); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("owner in maintainers = %v, want ErrInvalidArgument", err)
	}
	// >20 maintainers rejected.
	many := make([]string, 21)
	for i := range many {
		many[i] = fmt.Sprintf("usr_%d", i)
	}
	if _, err := h.svc.UpdateMaintainers(ctx, h.alice, app.ID, sohaapi.UpdateHostedAppMaintainersRequest{
		MaintainerUserIDs: many,
	}); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf(">20 maintainers = %v, want ErrInvalidArgument", err)
	}
	// Unknown user rejected.
	if _, err := h.svc.UpdateMaintainers(ctx, h.alice, app.ID, sohaapi.UpdateHostedAppMaintainersRequest{
		MaintainerUserIDs: []string{"usr_missing"},
	}); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("missing user maintainer = %v, want ErrInvalidArgument", err)
	}

	// Deduped valid collaborator added; enriched profile populated.
	updated, err := h.svc.UpdateMaintainers(ctx, h.alice, app.ID, sohaapi.UpdateHostedAppMaintainersRequest{
		MaintainerUserIDs: []string{" usr_bob ", "usr_bob"},
	})
	if err != nil || len(updated.MaintainerUserIDs) != 1 || len(updated.Maintainers) != 1 || updated.Maintainers[0].DisplayName != "Bob Chen" {
		t.Fatalf("UpdateMaintainers = %#v, %v", updated, err)
	}
	// Collaborator cannot edit maintainers.
	if _, err := h.svc.UpdateMaintainers(ctx, h.bob, app.ID, sohaapi.UpdateHostedAppMaintainersRequest{
		MaintainerUserIDs: []string{},
	}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("collaborator UpdateMaintainers = %v, want ErrAccessDenied", err)
	}

	// Collaborator CAN upload v1, but does NOT get gateway visitor access while app is private!
	v1 := makeTarGz(t, []tarEntrySpec{{name: "index.html", body: "<!doctype html>v1"}})
	up1, err := h.svc.UploadRevision(ctx, h.bob, app.ID, domainapphosting.UploadRevisionInput{Description: "bob v1"}, bytes.NewReader(v1))
	if err != nil {
		t.Fatalf("collaborator UploadRevision: %v", err)
	}
	host := app.DNSLabel + ".apps.local"
	bobDec, err := h.svc.CheckGatewayAccess(ctx, "bootstrap-gw-token", sohaapi.HostedAppGatewayAccessCheckRequest{
		Host:      host,
		UserID:    "usr_bob",
		SessionID: "sess_bob",
	})
	if err != nil || bobDec.Decision != sohaapi.HostedAppAccessDecisionKindForbidden {
		t.Fatalf("collaborator should be denied private gateway access: %#v (%v)", bobDec, err)
	}
	aliceDec, err := h.svc.CheckGatewayAccess(ctx, "bootstrap-gw-token", sohaapi.HostedAppGatewayAccessCheckRequest{
		Host:      host,
		UserID:    "usr_alice",
		SessionID: "sess_alice",
	})
	if err != nil || aliceDec.Decision != sohaapi.HostedAppAccessDecisionKindAllow {
		t.Fatalf("owner should be allowed private gateway access: %#v (%v)", aliceDec, err)
	}
	return app.ID, up1.Revision.ID
}

func assertMidUploadActorRevocationAndOwnershipTransfer(t *testing.T, ctx context.Context, h *b2TestHarness, appID, rev1ID string) string {
	t.Helper()
	// 1) Mid-upload collaborator removal prevents ActivateRevision from overwriting activeRevisionId and cleans S3 keys.
	h.store.onPut = func(_ string) error {
		_, err := h.svc.UpdateMaintainers(ctx, h.alice, appID, sohaapi.UpdateHostedAppMaintainersRequest{
			MaintainerUserIDs: []string{},
		})
		return err
	}
	lateBundle := makeTarGz(t, []tarEntrySpec{{name: "index.html", body: "<!doctype html>unauthorized-overwrite"}})
	if _, err := h.svc.UploadRevision(ctx, h.bob, appID, domainapphosting.UploadRevisionInput{}, bytes.NewReader(lateBundle)); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("removed collaborator mid-upload = %v, want ErrAccessDenied", err)
	}
	h.store.onPut = nil
	afterRemoval, err := h.svc.GetApp(ctx, h.alice, appID)
	if err != nil || afterRemoval.ActiveRevisionID != rev1ID {
		t.Fatalf("mid-upload collaborator removal altered activeRevisionId: %#v (%v)", afterRemoval, err)
	}

	// Re-add Bob as maintainer, queue pending org approval as Alice, then transfer ownership to Bob.
	if _, err := h.svc.UpdateMaintainers(ctx, h.alice, appID, sohaapi.UpdateHostedAppMaintainersRequest{
		MaintainerUserIDs: []string{"usr_bob"},
	}); err != nil {
		t.Fatal(err)
	}
	withPending, err := h.svc.UpdateVisibility(ctx, h.alice, appID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeOrganization},
	})
	if err != nil || withPending.PendingVisibilityApproval == nil {
		t.Fatal(err)
	}
	if _, err := h.svc.TransferOwnership(ctx, h.alice, appID, sohaapi.TransferHostedAppOwnershipRequest{NewOwnerUserID: "usr_bob"}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("non-admin TransferOwnership = %v, want ErrAccessDenied", err)
	}

	transferred, err := h.svc.TransferOwnership(ctx, h.admin, appID, sohaapi.TransferHostedAppOwnershipRequest{
		NewOwnerUserID: "usr_bob",
		Reason:         "Alice moved teams",
	})
	if err != nil {
		t.Fatalf("TransferOwnership: %v", err)
	}
	if transferred.OwnerUserID != "usr_bob" || slices.Contains(transferred.MaintainerUserIDs, "usr_bob") || slices.Contains(transferred.MaintainerUserIDs, "usr_alice") {
		t.Fatalf("unexpected owners/maintainers after transfer: %#v", transferred)
	}
	if transferred.RouteGeneration <= withPending.RouteGeneration || transferred.PendingVisibilityApproval != nil {
		t.Fatalf("TransferOwnership must bump routeGeneration and invalidate pending approval: %#v", transferred)
	}

	host := transferred.DNSLabel + ".apps.local"
	oldOwnerDec, err := h.svc.CheckGatewayAccess(ctx, "bootstrap-gw-token", sohaapi.HostedAppGatewayAccessCheckRequest{Host: host, UserID: "usr_alice", SessionID: "sess_alice"})
	if err != nil || oldOwnerDec.Decision != sohaapi.HostedAppAccessDecisionKindForbidden {
		t.Fatalf("previous owner must immediately lose private access: %#v (%v)", oldOwnerDec, err)
	}
	newOwnerDec, err := h.svc.CheckGatewayAccess(ctx, "bootstrap-gw-token", sohaapi.HostedAppGatewayAccessCheckRequest{Host: host, UserID: "usr_bob", SessionID: "sess_bob"})
	if err != nil || newOwnerDec.Decision != sohaapi.HostedAppAccessDecisionKindAllow {
		t.Fatalf("new owner must immediately gain private access: %#v (%v)", newOwnerDec, err)
	}

	v2 := makeTarGz(t, []tarEntrySpec{{name: "index.html", body: "<!doctype html>bob-owner-v2"}})
	up2, err := h.svc.UploadRevision(ctx, h.bob, appID, domainapphosting.UploadRevisionInput{Description: "owner v2"}, bytes.NewReader(v2))
	if err != nil {
		t.Fatalf("new owner upload: %v", err)
	}
	return up2.Revision.ID
}

func assertConfirmedDeleteAndObjectCleanup(t *testing.T, ctx context.Context, h *b2TestHarness, appID, rev2ID string) {
	t.Helper()
	current, err := h.svc.GetApp(ctx, h.bob, appID)
	if err != nil {
		t.Fatal(err)
	}
	// Former owner denied delete.
	if _, err := h.svc.DeleteApp(ctx, h.alice, appID, sohaapi.DeleteHostedAppRequest{ConfirmationName: current.Name}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("non-owner DeleteApp = %v, want ErrAccessDenied", err)
	}
	// Wrong confirmation name rejected with 409.
	if _, err := h.svc.DeleteApp(ctx, h.bob, appID, sohaapi.DeleteHostedAppRequest{ConfirmationName: "Wrong Name"}); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("wrong confirmationName = %v, want ErrConflict", err)
	}

	res, err := h.svc.DeleteApp(ctx, h.bob, appID, sohaapi.DeleteHostedAppRequest{
		ConfirmationName: current.Name,
		Reason:           "retire console",
	})
	if err != nil || !res.Deleted || res.AppID != appID {
		t.Fatalf("DeleteApp = %#v, %v", res, err)
	}
	if _, exists := h.store.objects["hosted/"+appID+"/"+rev2ID+"/index.html"]; exists {
		t.Fatal("expected DeleteApp to purge revision S3 objects")
	}
	for key := range h.store.objects {
		if len(key) > len("hosted/"+appID+"/") && key[:len("hosted/"+appID+"/")] == "hosted/"+appID+"/" {
			t.Fatalf("DeleteApp left residual S3 object key %q", key)
		}
	}
	assertDeleteAppPurgesFailedRevisionManifestKeys(t, ctx, h)
	dec, err := h.svc.CheckGatewayAccess(ctx, "bootstrap-gw-token", sohaapi.HostedAppGatewayAccessCheckRequest{
		Host:      current.DNSLabel + ".apps.local",
		UserID:    "usr_bob",
		SessionID: "sess_bob",
	})
	if err != nil || dec.Decision != sohaapi.HostedAppAccessDecisionKindNotFound {
		t.Fatalf("deleted app gateway check = %#v, %v (want not_found)", dec, err)
	}
}

func assertDeleteAppPurgesFailedRevisionManifestKeys(t *testing.T, ctx context.Context, h *b2TestHarness) {
	t.Helper()
	orphanApp, _, err := h.svc.CreateApp(ctx, h.bob, sohaapi.CreateHostedAppRequest{Name: "Orphan Cleanup App"})
	if err != nil {
		t.Fatal(err)
	}
	uploadingRev, err := h.repo.CreateUploadingRevision(ctx, orphanApp.ID, domainapphosting.Revision{
		ID:                   "rev_failed_orphan",
		AppID:                orphanApp.ID,
		Kind:                 sohaapi.HostedAppKindStatic,
		StorageIntegrationID: "int_minio_1",
		StoragePrefix:        "hosted",
		Files: []sohaapi.HostedAppManifestFile{{
			Path:      "index.html",
			ObjectKey: orphanApp.ID + "/rev_failed_orphan/index.html",
		}},
		CreatedBy: "usr_bob",
		CreatedAt: h.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = h.repo.FailRevision(ctx, orphanApp.ID, uploadingRev.ID, "upload", "transient error")
	orphanKey := "hosted/" + orphanApp.ID + "/rev_failed_orphan/index.html"
	h.store.objects[orphanKey] = []byte("orphan")
	if _, err := h.svc.DeleteApp(ctx, h.bob, orphanApp.ID, sohaapi.DeleteHostedAppRequest{ConfirmationName: orphanApp.Name}); err != nil {
		t.Fatal(err)
	}
	if _, exists := h.store.objects[orphanKey]; exists {
		t.Fatalf("expected DeleteApp to purge failed revision object %q", orphanKey)
	}
}

func TestPortalCatalogAudienceIsolationAuthorAntiEnumerationAndAdminFilters(t *testing.T) {
	h := newB2TestHarness(t)
	ctx := t.Context()
	setOrgVisibilityPolicy(t, ctx, h, sohaapi.AppHostingOrganizationVisibilityPolicyAllowDirect)

	privApp := deployPortalTestApp(t, ctx, h, h.alice, "Alice Secret Notes", sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModePrivate})
	empShared := deployPortalTestApp(t, ctx, h, h.alice, "Expense Reconciler", sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeSelected, UserIDs: []string{"usr_emp"}})
	engOnly := deployPortalTestApp(t, ctx, h, h.bob, "Eng Incident Wall", sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeSelected, TeamIDs: []string{"team_eng"}})
	orgWide := deployPortalTestApp(t, ctx, h, h.alice, "Company Holiday Calendar", sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeOrganization})

	assertPortalEmployeeAndOwnerIsolation(t, ctx, h, privApp.ID, empShared.ID, engOnly.ID, orgWide.ID)
	assertPortalAntiEnumerationAndAdminTeamFilter(t, ctx, h, engOnly.ID)
}

func deployPortalTestApp(t *testing.T, ctx context.Context, h *b2TestHarness, owner domainidentity.Principal, name string, vis sohaapi.HostedAppVisibilityInput) sohaapi.HostedApp {
	t.Helper()
	app, _, err := h.svc.CreateApp(ctx, owner, sohaapi.CreateHostedAppRequest{
		Name:        name,
		Description: "Tool for " + name,
		Visibility:  &vis,
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := makeTarGz(t, []tarEntrySpec{{name: "index.html", body: "<!doctype html>" + name}})
	up, err := h.svc.UploadRevision(ctx, owner, app.ID, domainapphosting.UploadRevisionInput{}, bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	return up.App
}

func assertPortalEmployeeAndOwnerIsolation(t *testing.T, ctx context.Context, h *b2TestHarness, privID, empID, engID, orgID string) {
	t.Helper()
	catalog, err := h.svc.ListPortalApps(ctx, h.employee, sohaapi.ListPortalHostedAppsParams{})
	if err != nil {
		t.Fatalf("employee ListPortalApps: %v", err)
	}
	if catalog.Total != 2 || catalog.MyAppsCount != 0 || len(catalog.Authors) != 1 || catalog.Authors[0].UserID != "usr_alice" || catalog.Authors[0].VisibleAppCount != 2 {
		t.Fatalf("unexpected employee portal catalog: %#v", catalog)
	}
	gotIDs := []string{catalog.Items[0].ID, catalog.Items[1].ID}
	if slices.Contains(gotIDs, privID) || slices.Contains(gotIDs, engID) || !slices.Contains(gotIDs, empID) || !slices.Contains(gotIDs, orgID) {
		t.Fatalf("portal leaked unauthorized or private apps: %v", gotIDs)
	}

	aliceCatalog, err := h.svc.ListPortalApps(ctx, h.alice, sohaapi.ListPortalHostedAppsParams{})
	if err != nil || aliceCatalog.MyAppsCount != 3 {
		t.Fatalf("alice ListPortalApps = %#v (%v)", aliceCatalog, err)
	}
	if slices.ContainsFunc(aliceCatalog.Items, func(item sohaapi.PortalHostedAppItem) bool { return item.ID == privID }) {
		t.Fatal("private app must never appear in Colleague Works catalog even for its owner")
	}
}

func assertPortalAntiEnumerationAndAdminTeamFilter(t *testing.T, ctx context.Context, h *b2TestHarness, engOnlyID string) {
	t.Helper()
	for _, hiddenAuthor := range []string{"usr_silent", "usr_bob"} {
		res, err := h.svc.ListPortalApps(ctx, h.employee, sohaapi.ListPortalHostedAppsParams{AuthorUserID: hiddenAuthor})
		if err != nil || res.Total != 0 || res.SelectedAuthor != nil {
			t.Fatalf("author %s enumeration leaked selectedAuthor: %#v (%v)", hiddenAuthor, res, err)
		}
	}

	byTeamQuery, err := h.svc.ListPortalApps(ctx, h.employee, sohaapi.ListPortalHostedAppsParams{Query: "engineering"})
	if err != nil || byTeamQuery.Total != 2 {
		t.Fatalf("query by author teamName = %#v, %v", byTeamQuery, err)
	}

	finApps, err := h.svc.ListApps(ctx, h.carol, domainapphosting.AppListFilter{
		Scope:  sohaapi.HostedAppListScopeAll,
		TeamID: "team_fin",
	})
	if err != nil || finApps.Total != 1 || finApps.Items[0].ID != engOnlyID {
		t.Fatalf("admin ListApps by teamId=team_fin = %#v, %v", finApps, err)
	}
	if finApps.Items[0].OwnerProfile == nil || finApps.Items[0].ActiveRevisionNumber != 1 || finApps.Items[0].TotalBytes <= 0 {
		t.Fatalf("expected enriched ownerProfile, activeRevisionNumber, totalBytes: %#v", finApps.Items[0])
	}
}

type stubWorkspaceBundleSource struct {
	workspaceTarGz []byte
	artifactTarGz  []byte
	artifactTitle  string
}

func (s *stubWorkspaceBundleSource) BundlePersonalWorkspace(_ context.Context, _ domainidentity.Principal, _, _ string, _ int64) (io.ReadCloser, error) {
	return io.NopCloser(bytes.NewReader(s.workspaceTarGz)), nil
}

func (s *stubWorkspaceBundleSource) BundleConversationArtifact(_ context.Context, _ domainidentity.Principal, _, _, _ string) (io.ReadCloser, string, error) {
	return io.NopCloser(bytes.NewReader(s.artifactTarGz)), s.artifactTitle, nil
}

func TestDeployFromWorkspacePermissionsDLPAndIdempotency(t *testing.T) {
	h := newB2TestHarness(t)
	ctx := t.Context()
	setOrgVisibilityPolicy(t, ctx, h, sohaapi.AppHostingOrganizationVisibilityPolicyRequireApproval)

	wsSrc := &stubWorkspaceBundleSource{
		workspaceTarGz: makeTarGz(t, []tarEntrySpec{{name: "index.html", body: "<!doctype html><h1>Workspace App</h1>"}}),
		artifactTarGz:  makeTarGz(t, []tarEntrySpec{{name: "index.html", body: "<!doctype html><h1>Artifact Chart</h1>"}}),
		artifactTitle:  "Monthly Latency Report",
	}
	h.svc.SetWorkspaceBundleSource(wsSrc)

	// Requires BOTH observe.ai.chat and apphosting.apps.publish.
	if _, err := h.svc.DeployFromWorkspace(ctx, h.employee, sohaapi.WorkspaceHostedAppDeploymentRequest{
		IdempotencyKey: "ws-1",
		Source:         sohaapi.WorkspaceHostedAppSource{Kind: sohaapi.WorkspaceHostedAppSourceKindPersonalEnvironment, SourcePath: "dist"},
	}); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("employee without publish/chat perms = %v, want ErrAccessDenied", err)
	}

	// Create + upload from conversation_artifact under require_approval creates online private app + pending approval; replay returns Created=false.
	req := sohaapi.WorkspaceHostedAppDeploymentRequest{
		IdempotencyKey:   "ws-art-1",
		Visibility:       &sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeOrganization},
		VisibilityReason: "share latency dashboard",
		Source: sohaapi.WorkspaceHostedAppSource{
			Kind:       sohaapi.WorkspaceHostedAppSourceKindConversationArtifact,
			SessionID:  "sess_1",
			ArtifactID: "art_1",
		},
	}
	res1, err := h.svc.DeployFromWorkspace(ctx, h.alice, req)
	if err != nil {
		t.Fatalf("DeployFromWorkspace artifact: %v", err)
	}
	if !res1.Created || res1.App.Name != "Monthly Latency Report" || res1.App.Status != sohaapi.HostedAppStatusOnline || res1.App.Visibility.Mode != sohaapi.HostedAppVisibilityModePrivate || res1.App.PendingVisibilityApproval == nil {
		t.Fatalf("unexpected workspace deploy result: %#v", res1)
	}
	replay, err := h.svc.DeployFromWorkspace(ctx, h.alice, req)
	if err != nil || replay.Created || replay.App.ID != res1.App.ID || replay.Revision.ID != res1.Revision.ID {
		t.Fatalf("idempotent workspace deploy replay = %#v, %v", replay, err)
	}

	// Secret DLP scanning still rejects leaked credentials in workspace bundles!
	wsSrc.workspaceTarGz = makeTarGz(t, []tarEntrySpec{
		{name: "index.html", body: "<!doctype html>ok"},
		{name: "config.js", body: "export const token = 'AKIA" + "IOSFODNN7EXAMPLE';"},
	})
	if _, err := h.svc.DeployFromWorkspace(ctx, h.alice, sohaapi.WorkspaceHostedAppDeploymentRequest{
		AppID:          res1.App.ID,
		IdempotencyKey: "ws-dlp-block",
		Source:         sohaapi.WorkspaceHostedAppSource{Kind: sohaapi.WorkspaceHostedAppSourceKindPersonalEnvironment, SourcePath: "app"},
	}); !errors.Is(err, domainapphosting.ErrBundleRejected) {
		t.Fatalf("workspace bundle with AWS key = %v, want ErrBundleRejected", err)
	}
}
