package apphosting_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/application/access"
	apphosting "github.com/opensoha/soha/internal/application/apphosting"
	domain "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/stretchr/testify/require"
)

func TestStaticPublicationVisibilityProtectsPreviousAudienceAndReplay(t *testing.T) {
	ctx := t.Context()
	h := newTestHarness(t)
	app, _, err := h.svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{Name: "Static report"})
	require.NoError(t, err)
	bundle := makeTarGz(t, []tarEntrySpec{{name: "index.html", body: "<h1>report</h1>"}})
	upload := func(key string, intent *sohaapi.UpdateHostedAppVisibilityRequest) (sohaapi.HostedAppDeploymentReceipt, error) {
		return h.svc.UploadRevision(ctx, h.alice, app.ID, domain.UploadRevisionInput{IdempotencyKey: key, PublishVisibility: intent}, bytes.NewReader(bundle))
	}
	initial, err := upload("initial", nil)
	require.NoError(t, err)
	share := sohaapi.UpdateHostedAppVisibilityRequest{Visibility: sohaapi.HostedAppVisibilityInput{
		Mode: sohaapi.HostedAppVisibilityModeSelected, UserIDs: []string{"usr_bob"},
	}}

	h.store.onPut = func(string) error { return errors.New("unavailable") }
	_, err = upload("storage-failure", &share)
	require.Error(t, err)
	old, err := h.svc.GetApp(ctx, h.alice, app.ID)
	require.NoError(t, err)
	require.Equal(t, initial.Revision.ID, old.ActiveRevisionID)
	require.Equal(t, sohaapi.HostedAppVisibilityModePrivate, old.Visibility.Mode)
	require.Nil(t, old.PendingVisibilityApproval)
	permissions := &dynamicPublisherPermissions{stubPermissions: stubPermissions{
		grants: map[string][]string{h.alice.UserID: {access.PermAppHostingAppsPublish, access.PermAppHostingAppsView}},
	}}
	h.svc, err = apphosting.New(h.repo, h.store, h.audience, permissions, h.audit)
	require.NoError(t, err)
	h.store.onPut = func(string) error {
		permissions.failure = apperrors.ErrAccessDenied
		return nil
	}
	_, err = upload("permission-revoked", &share)
	require.ErrorIs(t, err, apperrors.ErrAccessDenied)
	permissions.failure = nil
	_, err = upload("permission-revoked-gzip", nil)
	require.ErrorIs(t, err, apperrors.ErrAccessDenied)
	permissions.failure = nil
	h.store.onPut = nil
	unchanged, err := h.repo.GetAppByID(ctx, app.ID)
	require.NoError(t, err)
	require.Equal(t, initial.Revision.ID, unchanged.ActiveRevisionID)
	require.Equal(t, sohaapi.HostedAppVisibilityModePrivate, unchanged.Visibility.Mode)

	next, err := upload("successful", &share)
	require.NoError(t, err)
	require.Equal(t, next.Revision.ID, next.App.ActiveRevisionID)
	require.Equal(t, share.Visibility.Mode, next.App.Visibility.Mode)
	_, err = h.svc.UpdateVisibility(ctx, h.alice, app.ID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModePrivate},
	})
	require.NoError(t, err)
	replayed, err := upload("successful", &share)
	require.NoError(t, err)
	require.True(t, replayed.ReusedIdempotencyKey)
	require.Equal(t, next.Revision.ID, replayed.Revision.ID)
	require.Equal(t, sohaapi.HostedAppVisibilityModePrivate, replayed.App.Visibility.Mode)
	_, err = upload("successful", nil)
	require.ErrorIs(t, err, apperrors.ErrConflict)
}

type publicationStartupProbe struct {
	dynamicDeploymentProbe
	rejectStartup bool
}

func (b *publicationStartupProbe) RuntimeReady(ctx context.Context, rev domain.Revision) (bool, error) {
	if b.rejectStartup {
		return false, apphosting.ErrDynamicRevisionFailed
	}
	return b.dynamicDeploymentProbe.RuntimeReady(ctx, rev)
}

func TestDynamicWorkspacePublicationVisibilityWaitsForReady(t *testing.T) {
	ctx := t.Context()
	backend := &publicationStartupProbe{}
	h := newB2TestHarness(t, apphosting.WithClock(time.Now), apphosting.WithDynamicBackend(backend))
	h.svc.SetWorkspaceBundleSource(&stubWorkspaceBundleSource{workspaceTarGz: dynamicDataTestBundle(t)})
	app, _, err := h.svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{Name: "Dynamic report", Kind: sohaapi.HostedAppKindDynamic})
	require.NoError(t, err)
	req := sohaapi.WorkspaceHostedAppDeploymentRequest{
		AppID: app.ID, IdempotencyKey: "initial", Kind: app.Kind,
		Source: sohaapi.WorkspaceHostedAppSource{Kind: sohaapi.WorkspaceHostedAppSourceKindPersonalEnvironment, SourcePath: "."},
	}
	initial, err := h.svc.DeployFromWorkspace(ctx, h.alice, req)
	require.NoError(t, err)
	reconcileDynamicSteps(t, h.testHarness, 3)
	assertActive := func(id string, mode sohaapi.HostedAppVisibilityMode) {
		t.Helper()
		current, err := h.svc.GetApp(ctx, h.alice, app.ID)
		require.NoError(t, err)
		require.Equal(t, id, current.ActiveRevisionID)
		require.Equal(t, mode, current.Visibility.Mode)
	}

	req.Visibility = &sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModeSelected, UserIDs: []string{"usr_bob"}}
	req.IdempotencyKey = "startup-failed"
	backend.rejectStartup = true
	failed, err := h.svc.DeployFromWorkspace(ctx, h.alice, req)
	require.NoError(t, err)
	reconcileDynamicSteps(t, h.testHarness, 3)
	stored, err := h.svc.GetRevision(ctx, h.alice, app.ID, failed.Revision.ID)
	require.NoError(t, err)
	require.Equal(t, sohaapi.HostedAppRevisionStatusFailed, stored.Status)
	assertActive(initial.Revision.ID, sohaapi.HostedAppVisibilityModePrivate)

	req.IdempotencyKey = "revoked-before-ready"
	backend.rejectStartup = false
	queued, err := h.svc.DeployFromWorkspace(ctx, h.alice, req)
	require.NoError(t, err)
	reconcileDynamicSteps(t, h.testHarness, 2)
	assertActive(initial.Revision.ID, sohaapi.HostedAppVisibilityModePrivate)
	_, err = h.svc.UpdateVisibility(ctx, h.alice, app.ID, sohaapi.UpdateHostedAppVisibilityRequest{
		Visibility: sohaapi.HostedAppVisibilityInput{Mode: sohaapi.HostedAppVisibilityModePrivate},
	})
	require.NoError(t, err)
	retry, err := h.svc.DeployFromWorkspace(ctx, h.alice, req)
	require.NoError(t, err)
	require.Equal(t, queued.Revision.ID, retry.Revision.ID)
	reconcileDynamicSteps(t, h.testHarness, 1)
	assertActive(queued.Revision.ID, sohaapi.HostedAppVisibilityModePrivate)

	req.IdempotencyKey = "ready-and-shared"
	ready, err := h.svc.DeployFromWorkspace(ctx, h.alice, req)
	require.NoError(t, err)
	require.Equal(t, sohaapi.HostedAppVisibilityModePrivate, ready.App.Visibility.Mode)
	reconcileDynamicSteps(t, h.testHarness, 3)
	assertActive(ready.Revision.ID, sohaapi.HostedAppVisibilityModeSelected)
}
