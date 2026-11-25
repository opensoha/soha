package apphosting_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	"github.com/opensoha/soha/internal/application/access"
	apphosting "github.com/opensoha/soha/internal/application/apphosting"
	domain "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/platform/apperrors"
	repository "github.com/opensoha/soha/internal/repository/apphosting"
	"github.com/stretchr/testify/require"
)

type dynamicDeploymentProbe struct {
	buildFailed bool
	started     string
	prepared    string
	promoted    string
	discarded   string
}

func (b *dynamicDeploymentProbe) StartBuild(_ context.Context, revision domain.Revision) error {
	b.started = revision.SourceKey
	return nil
}

func (b *dynamicDeploymentProbe) BuildResult(context.Context, domain.Revision) (string, bool, error) {
	if b.buildFailed {
		return "", false, apphosting.ErrDynamicRevisionFailed
	}
	return "registry.example/hosted@sha256:" + strings.Repeat("a", 64), true, nil
}

func (b *dynamicDeploymentProbe) PrepareRuntime(_ context.Context, rev domain.Revision) error {
	if !rev.Database || !strings.Contains(rev.ImageRef, "@sha256:") {
		return errors.New("data or image dependency was not bound")
	}
	b.prepared = rev.ID
	return nil
}

func (b *dynamicDeploymentProbe) RuntimeReady(_ context.Context, rev domain.Revision) (bool, error) {
	return b.prepared == rev.ID, nil
}

func (b *dynamicDeploymentProbe) ActivateRuntime(_ context.Context, rev domain.Revision) error {
	if b.prepared != rev.ID {
		return errors.New("candidate was not probed")
	}
	b.promoted = rev.ID
	return nil
}

func (*dynamicDeploymentProbe) CheckRollback(context.Context, domain.Revision) error   { return nil }
func (*dynamicDeploymentProbe) RestrictRuntime(context.Context, domain.Revision) error { return nil }
func (*dynamicDeploymentProbe) DeleteBuild(context.Context, domain.Revision) error     { return nil }

func (b *dynamicDeploymentProbe) DeleteRuntime(_ context.Context, rev domain.Revision) error {
	b.discarded = rev.ID
	return nil
}

func TestDynamicHostingQueuesReconcilesAndKeepsPreviousVersion(t *testing.T) {
	ctx := t.Context()
	backend := &dynamicDeploymentProbe{}
	h := newTestHarness(t, apphosting.WithClock(time.Now), apphosting.WithDynamicBackend(backend))
	app, _, err := h.svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{Name: "Dynamic form", Kind: sohaapi.HostedAppKindDynamic})
	if err != nil {
		t.Fatal(err)
	}
	source := makeTarGz(t, []tarEntrySpec{
		{name: "soha-host.json", body: `{"template":"python","database":true}`},
		{name: "app.py", body: "print('hello')"},
		{name: "requirements.txt", body: ""},
	})
	h.store.onPut = func(string) error { return errors.New("temporarily unavailable") }
	if _, err := h.svc.UploadRevision(ctx, h.alice, app.ID, domain.UploadRevisionInput{IdempotencyKey: "first"}, bytes.NewReader(source)); err == nil {
		t.Fatal("interrupted upload unexpectedly succeeded")
	}
	h.store.onPut = nil
	first := uploadTestDynamic(t, h, app.ID, "first", source)
	if first.Revision.Status != sohaapi.HostedAppRevisionStatusValidating || first.Revision.BuildPhase != "queued" || first.App.ActiveRevisionID != "" {
		t.Fatalf("unexpected queued revision: %+v", first)
	}
	if retry := uploadTestDynamic(t, h, app.ID, "first", source); !retry.ReusedIdempotencyKey || retry.Revision.ID != first.Revision.ID {
		t.Fatalf("idempotent upload changed revision: %+v", retry)
	}
	reconcileDynamicSteps(t, h, 3)
	online, err := h.svc.GetApp(ctx, h.alice, app.ID)
	if err != nil || online.ActiveRevisionID != first.Revision.ID || backend.promoted != first.Revision.ID || !strings.HasSuffix(backend.started, "/source.tar.gz") {
		t.Fatalf("candidate not activated after verification: app=%+v err=%v", online, err)
	}
	backend.buildFailed = true
	second := uploadTestDynamic(t, h, app.ID, "second", source)
	reconcileDynamicSteps(t, h, 2)
	failed, err := h.svc.GetRevision(ctx, h.alice, app.ID, second.Revision.ID)
	if err != nil || failed.Status != sohaapi.HostedAppRevisionStatusFailed || backend.discarded != second.Revision.ID {
		t.Fatalf("failed revision not recorded and cleaned: %+v, %v", failed, err)
	}
	online, err = h.svc.GetApp(ctx, h.alice, app.ID)
	if err != nil || online.ActiveRevisionID != first.Revision.ID {
		t.Fatalf("failed build displaced active revision: %+v, %v", online, err)
	}
}

func TestDynamicUploadCleansSourceWhenAppIsDeletedDuringPut(t *testing.T) {
	ctx := t.Context()
	h := newTestHarness(t, apphosting.WithClock(time.Now), apphosting.WithDynamicBackend(&dynamicDeploymentProbe{}))
	app, _, err := h.svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{Name: "In-flight upload", Kind: sohaapi.HostedAppKindDynamic})
	if err != nil {
		t.Fatal(err)
	}
	var key string
	h.store.onPut = func(objectKey string) error {
		key = objectKey
		_, err := h.svc.DeleteApp(ctx, h.alice, app.ID, sohaapi.DeleteHostedAppRequest{ConfirmationName: app.Name})
		require.NoError(t, err)
		// The cleanup must wait while a late upload can still create the object.
		require.NoError(t, h.svc.ReconcileDynamicRevision(ctx))
		_, err = h.repo.ClaimDeletedDynamicRevision(ctx, "check-before-put", time.Now(), time.Minute)
		require.ErrorIs(t, err, apperrors.ErrNotFound)
		return nil
	}
	source := makeTarGz(t, []tarEntrySpec{
		{name: "soha-host.json", body: `{"template":"python","database":false}`},
		{name: "app.py", body: "print('hello')"},
		{name: "requirements.txt", body: ""},
	})
	if _, err := h.svc.UploadRevision(ctx, h.alice, app.ID, domain.UploadRevisionInput{IdempotencyKey: "deleted"}, bytes.NewReader(source)); err == nil {
		t.Fatal("upload to removed app unexpectedly succeeded")
	}
	if len(h.store.objects) != 0 {
		t.Fatal("source was recreated after the app was deleted")
	}
	work, err := h.repo.ClaimDeletedDynamicRevision(ctx, "check-late-put", time.Now().Add(apphosting.DynamicSourceCleanupDelay), time.Minute)
	require.NoError(t, err)
	require.Equal(t, key, work.SourceKey)
}

type uploadOutageRepository struct {
	*repository.MemoryRepository
	unavailable bool
}

func (r *uploadOutageRepository) GetRevision(ctx context.Context, appID, revisionID string) (domain.Revision, error) {
	if r.unavailable {
		return domain.Revision{}, apperrors.ErrServiceUnavailable
	}
	return r.MemoryRepository.GetRevision(ctx, appID, revisionID)
}

type uploadDeadlineStore struct {
	*stubObjectStore
	deadline time.Time
}

func (s *uploadDeadlineStore) PutObject(ctx context.Context, integrationID, key, contentType string, body io.Reader) (int64, string, string, error) {
	s.deadline, _ = ctx.Deadline()
	return s.stubObjectStore.PutObject(ctx, integrationID, key, contentType, body)
}

func TestDynamicUploadDeletionKeepsCleanupThroughCompensationOutage(t *testing.T) {
	for _, phase := range []string{"uploading", "queued by overlapping upload"} {
		t.Run(phase, func(t *testing.T) { testDynamicUploadDeletionWithCompensationOutage(t, phase != "uploading") })
	}
}

func testDynamicUploadDeletionWithCompensationOutage(t *testing.T, alreadyQueued bool) {
	t.Helper()
	ctx := t.Context()
	createdAt := time.Now().UTC().Truncate(time.Microsecond)
	now := createdAt
	clock := func() time.Time { return now }
	backend := &dynamicDeploymentProbe{}
	h := newTestHarness(t, apphosting.WithClock(clock), apphosting.WithDynamicBackend(backend))
	repo := &uploadOutageRepository{MemoryRepository: h.repo}
	objects := &uploadDeadlineStore{stubObjectStore: h.store}
	perms := &stubPermissions{grants: map[string][]string{h.alice.UserID: {access.PermAppHostingAppsPublish}}}
	svc, err := apphosting.New(repo, objects, h.audience, perms, h.audit, apphosting.WithClock(clock), apphosting.WithDynamicBackend(backend))
	require.NoError(t, err)
	app, _, err := h.svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{Name: "In-flight upload outage", Kind: sohaapi.HostedAppKindDynamic})
	require.NoError(t, err)
	objects.onPut = func(sourceKey string) error {
		if alreadyQueued {
			items, _, err := repo.ListRevisions(ctx, app.ID, 1, 0)
			require.NoError(t, err)
			require.Len(t, items, 1)
			require.NoError(t, repo.QueueDynamicRevision(ctx, app.ID, items[0].ID, sourceKey, "python", true, 1, now))
		}
		_, err := svc.DeleteApp(ctx, h.alice, app.ID, sohaapi.DeleteHostedAppRequest{ConfirmationName: app.Name})
		require.NoError(t, err)
		repo.unavailable = true
		return nil
	}
	_, err = svc.UploadRevision(ctx, h.alice, app.ID, domain.UploadRevisionInput{IdempotencyKey: "upload-outage"}, bytes.NewReader(dynamicDataTestBundle(t)))
	require.Error(t, err)
	require.Equal(t, createdAt.Add(20*time.Minute), objects.deadline)
	require.Len(t, h.store.objects, 1)
	now = createdAt.Add(apphosting.DynamicSourceCleanupDelay - time.Second)
	require.NoError(t, svc.ReconcileDynamicRevision(ctx))
	require.Len(t, h.store.objects, 1)
	repo.unavailable = false
	now = createdAt.Add(apphosting.DynamicSourceCleanupDelay)
	require.NoError(t, svc.ReconcileDynamicRevision(ctx))
	require.Empty(t, h.store.objects)
}

func TestDynamicLateUploadRemovesSourceAfterTerminalCleanup(t *testing.T) {
	ctx := t.Context()
	h := newTestHarness(t, apphosting.WithClock(time.Now), apphosting.WithDynamicBackend(&dynamicDeploymentProbe{}))
	app, _, err := h.svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{Name: "Late terminal upload", Kind: sohaapi.HostedAppKindDynamic})
	require.NoError(t, err)
	h.store.onPut = func(sourceKey string) error {
		now := time.Now()
		items, _, err := h.repo.ListRevisions(ctx, app.ID, 1, 0)
		require.NoError(t, err)
		require.Len(t, items, 1)
		require.NoError(t, h.repo.QueueDynamicRevision(ctx, app.ID, items[0].ID, sourceKey, "python", true, 1, now))
		rev, err := h.repo.ClaimDynamicRevision(ctx, "terminal", now, time.Minute)
		require.NoError(t, err)
		require.NoError(t, h.repo.FailDynamicRevision(ctx, rev.ID, rev.ClaimID, "queued", "build rejected"))
		return h.repo.CompleteDynamicCleanup(ctx, rev.ID, rev.ClaimID)
	}
	receipt := uploadTestDynamic(t, h, app.ID, "terminal", dynamicDataTestBundle(t))
	require.True(t, receipt.ReusedIdempotencyKey)
	require.Equal(t, sohaapi.HostedAppRevisionStatusFailed, receipt.Revision.Status)
	require.Empty(t, h.store.objects)
}

func TestDynamicWorkspaceDeployRequiresPersonalSource(t *testing.T) {
	ctx := t.Context()
	h := newB2TestHarness(t, apphosting.WithClock(time.Now), apphosting.WithDynamicBackend(&dynamicDeploymentProbe{}))
	h.svc.SetWorkspaceBundleSource(&stubWorkspaceBundleSource{
		workspaceTarGz: makeTarGz(t, []tarEntrySpec{
			{name: "soha-host.json", body: `{"template":"python","database":true}`},
			{name: "app.py", body: "print('hello')"},
			{name: "requirements.txt", body: ""},
		}),
	})
	req := sohaapi.WorkspaceHostedAppDeploymentRequest{
		Kind: sohaapi.HostedAppKindDynamic, IdempotencyKey: "workspace-dynamic",
		Source: sohaapi.WorkspaceHostedAppSource{Kind: sohaapi.WorkspaceHostedAppSourceKindConversationArtifact, ArtifactID: "artifact"},
	}
	if _, err := h.svc.DeployFromWorkspace(ctx, h.alice, req); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("dynamic artifact returned %v", err)
	}
	req.Source = sohaapi.WorkspaceHostedAppSource{Kind: sohaapi.WorkspaceHostedAppSourceKindPersonalEnvironment, SourcePath: "."}
	result, err := h.svc.DeployFromWorkspace(ctx, h.alice, req)
	if err != nil || !result.Created || result.App.Kind != sohaapi.HostedAppKindDynamic || result.Revision.BuildPhase != "queued" {
		t.Fatalf("workspace dynamic result = %+v, %v", result, err)
	}
	req.Kind = sohaapi.HostedAppKindStatic
	if _, err := h.svc.DeployFromWorkspace(ctx, h.alice, req); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatalf("workspace deployment same key with another kind returned %v", err)
	}
}

func TestDynamicAppQuotaDoesNotConsumeStaticQuota(t *testing.T) {
	ctx := t.Context()
	h := newTestHarness(t, apphosting.WithDynamicBackend(&dynamicDeploymentProbe{}))
	req := sohaapi.CreateHostedAppRequest{Name: "Dynamic tool", Kind: sohaapi.HostedAppKindDynamic}
	for range 4 {
		if _, _, err := h.svc.CreateApp(ctx, h.alice, req); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := h.svc.CreateApp(ctx, h.alice, req); !errors.Is(err, domain.ErrQuotaExceeded) {
		t.Fatalf("dynamic quota returned %v", err)
	}
	req.Kind = sohaapi.HostedAppKindStatic
	if _, _, err := h.svc.CreateApp(ctx, h.alice, req); err != nil {
		t.Fatalf("static quota consumed by dynamic apps: %v", err)
	}
}

type retryCleanupProbe struct {
	dynamicDeploymentProbe
	discardedIDs []string
	cleanupError error
}

func (b *retryCleanupProbe) DeleteRuntime(_ context.Context, rev domain.Revision) error {
	b.discardedIDs = append(b.discardedIDs, rev.ID)
	return b.cleanupError
}

func TestDynamicTerminalCleanupRetriesWithoutReactivating(t *testing.T) {
	for _, status := range []sohaapi.HostedAppRevisionStatus{sohaapi.HostedAppRevisionStatusFailed, sohaapi.HostedAppRevisionStatusEvicted} {
		t.Run(string(status), func(t *testing.T) {
			ctx := t.Context()
			now := time.Now()
			clock := func() time.Time { return now }
			backend := &retryCleanupProbe{cleanupError: context.DeadlineExceeded}
			h := newTestHarness(t, apphosting.WithClock(clock), apphosting.WithDynamicBackend(backend))
			_, err := h.repo.SaveSettings(ctx, func(settings domain.Settings) (domain.Settings, error) {
				settings.RetainedRevisionsPerApp = 1
				return settings, nil
			})
			require.NoError(t, err)
			app, _, err := h.svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{Name: "Dynamic cleanup", Kind: sohaapi.HostedAppKindDynamic})
			require.NoError(t, err)
			source := dynamicDataTestBundle(t)
			first := uploadTestDynamic(t, h, app.ID, "first", source)
			reconcileDynamicSteps(t, h, 3)
			backend.buildFailed = status == sohaapi.HostedAppRevisionStatusFailed
			second := uploadTestDynamic(t, h, app.ID, "second", source)
			terminalID, activeID := second.Revision.ID, first.Revision.ID
			if !backend.buildFailed {
				reconcileDynamicSteps(t, h, 3)
				terminalID, activeID = activeID, terminalID
			} else {
				reconcileDynamicSteps(t, h, 1)
			}
			require.ErrorIs(t, h.svc.ReconcileDynamicRevision(ctx), context.DeadlineExceeded)
			require.Equal(t, []string{terminalID}, backend.discardedIDs)
			stored, err := h.repo.GetRevision(ctx, app.ID, terminalID)
			require.NoError(t, err)
			require.Equal(t, status, stored.Status)
			require.False(t, stored.NextCheckAt.IsZero())
			reconcileDynamicSteps(t, h, 1)
			require.Equal(t, []string{terminalID}, backend.discardedIDs)
			require.NoError(t, disableHosting(ctx, h, ""))
			now = now.Add(time.Minute)
			backend.cleanupError = nil
			restarted, err := apphosting.New(h.repo, h.store, h.audience, &stubPermissions{}, h.audit, apphosting.WithClock(clock), apphosting.WithDynamicBackend(backend))
			require.NoError(t, err)
			require.NoError(t, restarted.ReconcileDynamicRevision(ctx))
			require.NoError(t, restarted.ReconcileDynamicRevision(ctx))
			require.Equal(t, []string{terminalID, terminalID}, backend.discardedIDs)
			stored, err = h.repo.GetRevision(ctx, app.ID, terminalID)
			require.NoError(t, err)
			require.Equal(t, status, stored.Status)
			require.True(t, stored.NextCheckAt.IsZero())
			current, err := h.repo.GetAppByID(ctx, app.ID)
			require.NoError(t, err)
			require.Equal(t, activeID, current.ActiveRevisionID)
		})
	}
}

func TestDynamicAppDeletionRetriesCleanupAfterRecordsAreGone(t *testing.T) {
	ctx := t.Context()
	now := time.Now()
	clock := func() time.Time { return now }
	backend := &retryCleanupProbe{cleanupError: context.DeadlineExceeded}
	h := newTestHarness(t, apphosting.WithClock(clock), apphosting.WithDynamicBackend(backend))
	app, _, err := h.svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{Name: "Deletion retry", Kind: sohaapi.HostedAppKindDynamic})
	require.NoError(t, err)
	receipt := uploadTestDynamic(t, h, app.ID, "deleting", dynamicDataTestBundle(t))
	reconcileDynamicSteps(t, h, 3)
	deleted, err := h.svc.DeleteApp(ctx, h.alice, app.ID, sohaapi.DeleteHostedAppRequest{ConfirmationName: app.Name})
	require.NoError(t, err)
	require.True(t, deleted.Deleted)
	_, err = h.repo.GetAppByDNSLabel(ctx, app.DNSLabel)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
	_, err = h.repo.GetRevision(ctx, app.ID, receipt.Revision.ID)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
	require.Empty(t, backend.discardedIDs)

	now = now.Add(apphosting.DynamicSourceCleanupDelay)
	require.ErrorIs(t, h.svc.ReconcileDynamicRevision(ctx), context.DeadlineExceeded)
	require.Equal(t, []string{receipt.Revision.ID}, backend.discardedIDs)
	require.Len(t, h.store.objects, 1)
	reconcileDynamicSteps(t, h, 1)
	require.Len(t, backend.discardedIDs, 1)
	now = now.Add(time.Minute)
	backend.cleanupError = nil
	restarted, err := apphosting.New(h.repo, h.store, h.audience, &stubPermissions{}, h.audit, apphosting.WithClock(clock), apphosting.WithDynamicBackend(backend))
	require.NoError(t, err)
	require.NoError(t, restarted.ReconcileDynamicRevision(ctx))
	require.NoError(t, restarted.ReconcileDynamicRevision(ctx))
	require.Equal(t, []string{receipt.Revision.ID, receipt.Revision.ID}, backend.discardedIDs)
	require.Empty(t, h.store.objects)
}

func dynamicDataTestBundle(t *testing.T) []byte {
	t.Helper()
	return makeTarGz(t, []tarEntrySpec{
		{name: "soha-host.json", body: `{"template":"python","database":true}`},
		{name: "app.py", body: "print('hello')"},
		{name: "requirements.txt", body: ""},
	})
}

func uploadTestDynamic(t *testing.T, h *testHarness, appID, key string, body []byte) sohaapi.HostedAppDeploymentReceipt {
	t.Helper()
	receipt, err := h.svc.UploadRevision(t.Context(), h.alice, appID, domain.UploadRevisionInput{IdempotencyKey: key}, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

func reconcileDynamicSteps(t *testing.T, h *testHarness, steps int) {
	t.Helper()
	for range steps {
		if err := h.svc.ReconcileDynamicRevision(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
}
