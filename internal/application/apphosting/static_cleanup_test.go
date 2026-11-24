package apphosting_test

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	app "github.com/opensoha/soha/internal/application/apphosting"
	domain "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/platform/apperrors"
	apphostingrepo "github.com/opensoha/soha/internal/repository/apphosting"
	"github.com/stretchr/testify/require"
)

func TestStaticArtifactCleanupRetriesWithoutRemovingActiveAssets(t *testing.T) {
	ctx := t.Context()
	now := time.Now().UTC()
	h := newTestHarness(t, app.WithClock(func() time.Time { return now }))
	_, err := h.repo.SaveSettings(ctx, func(settings domain.Settings) (domain.Settings, error) {
		settings.RetainedRevisionsPerApp = 1
		return settings, nil
	})
	require.NoError(t, err)
	hosted, _, err := h.svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{Name: "Cleanup proof"})
	require.NoError(t, err)
	source := makeTarGz(t, []tarEntrySpec{{name: "index.html", body: "<h1>fixture</h1>"}})
	first, err := h.svc.UploadRevision(ctx, h.alice, hosted.ID, domain.UploadRevisionInput{IdempotencyKey: "first"}, bytes.NewReader(source))
	require.NoError(t, err)
	second, err := h.svc.UploadRevision(ctx, h.alice, hosted.ID, domain.UploadRevisionInput{IdempotencyKey: "second"}, bytes.NewReader(source))
	require.NoError(t, err)
	oldKey := "hosted/" + hosted.ID + "/" + first.Revision.ID + "/index.html"
	liveKey := "hosted/" + hosted.ID + "/" + second.Revision.ID + "/index.html"

	unavailable := errors.New("object store is unavailable")
	h.store.onDelete = func() error { return unavailable }
	require.ErrorIs(t, h.svc.ReconcileStaticArtifactCleanup(ctx), unavailable)
	require.Contains(t, h.store.objects, oldKey)
	h.store.onDelete = nil
	require.NoError(t, h.svc.ReconcileStaticArtifactCleanup(ctx))
	require.Contains(t, h.store.objects, oldKey)

	now = now.Add(time.Minute)
	require.NoError(t, h.svc.ReconcileStaticArtifactCleanup(ctx))
	require.NotContains(t, h.store.objects, oldKey)
	require.Contains(t, h.store.objects, liveKey)
	require.NoError(t, h.svc.ReconcileStaticArtifactCleanup(ctx))
	require.Equal(t, []string{oldKey}, h.store.deletedKeys)
}

func TestStaticArtifactCleanupRetriesExactHardDeletedManifest(t *testing.T) {
	ctx := t.Context()
	now := time.Now().UTC()
	h := newTestHarness(t, app.WithClock(func() time.Time { return now }))
	hosted, _, err := h.svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{Name: "Exact cleanup proof"})
	require.NoError(t, err)
	source := makeTarGz(t, []tarEntrySpec{{name: "index.html", body: "<h1>fixture</h1>"}, {name: "assets/main.js", body: "void 0"}})
	ready, err := h.svc.UploadRevision(ctx, h.alice, hosted.ID, domain.UploadRevisionInput{}, bytes.NewReader(source))
	require.NoError(t, err)
	prefix := "hosted/" + hosted.ID + "/" + ready.Revision.ID + "/"
	h.store.objects[prefix+"decoy.html"] = []byte("outside the manifest")
	unavailable := errors.New("object store is unavailable")
	var attempts int
	h.store.onDelete = func() error { attempts++; return unavailable }
	result, err := h.svc.DeleteApp(ctx, h.alice, hosted.ID, sohaapi.DeleteHostedAppRequest{ConfirmationName: hosted.Name})
	require.NoError(t, err)
	require.True(t, result.Deleted)
	require.Zero(t, attempts)
	_, err = h.repo.GetRevision(ctx, hosted.ID, ready.Revision.ID)
	require.ErrorIs(t, err, apperrors.ErrNotFound)

	require.ErrorIs(t, h.svc.ReconcileStaticArtifactCleanup(ctx), unavailable)
	require.Equal(t, 1, attempts)
	h.store.onDelete = nil
	require.NoError(t, h.svc.ReconcileStaticArtifactCleanup(ctx))
	require.Contains(t, h.store.objects, prefix+"index.html")
	now = now.Add(time.Minute)
	require.NoError(t, h.svc.ReconcileStaticArtifactCleanup(ctx))
	require.ElementsMatch(t, []string{prefix + "index.html", prefix + "assets/main.js"}, h.store.deletedKeys)
	require.Equal(t, map[string][]byte{prefix + "decoy.html": []byte("outside the manifest")}, h.store.objects)
}

func TestFailedStaticUploadCleansPlannedManifestAfterPutGrace(t *testing.T) {
	ctx := t.Context()
	now := time.Now().UTC()
	h := newB2TestHarness(t, app.WithClock(func() time.Time { return now }))
	store := &uploadDeadlineStore{stubObjectStore: h.store}
	svc, err := app.New(h.repo, store, h.audience, h.perms, h.audit, app.WithClock(func() time.Time { return now }))
	require.NoError(t, err)
	hosted, _, err := svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{Name: "Upload deadline proof"})
	require.NoError(t, err)
	unavailable := errors.New("object upload acknowledgment lost")
	h.store.onPut = func(key string) error {
		h.store.objects[key] = []byte("PUT committed before transport failed")
		return unavailable
	}
	source := makeTarGz(t, []tarEntrySpec{{name: "index.html", body: "<h1>fixture</h1>"}, {name: "late.js", body: "void 0"}})
	const idem = "failed-static-put"
	_, err = svc.UploadRevision(ctx, h.alice, hosted.ID, domain.UploadRevisionInput{IdempotencyKey: idem}, bytes.NewReader(source))
	require.ErrorIs(t, err, unavailable)
	rev, err := h.repo.FindRevisionByIdempotencyKey(ctx, hosted.ID, idem)
	require.NoError(t, err)
	require.Equal(t, sohaapi.HostedAppRevisionStatusFailed, rev.Status)
	require.Equal(t, "upload", rev.FailureStage)
	require.WithinDuration(t, rev.CreatedAt.Add(app.StaticArtifactCleanupDelay-time.Minute), store.deadline, time.Second)

	now = rev.CreatedAt.Add(app.StaticArtifactCleanupDelay - time.Millisecond)
	require.NoError(t, svc.ReconcileStaticArtifactCleanup(ctx))
	require.Len(t, h.store.objects, 1)
	require.Empty(t, h.store.deletedKeys)
	now = rev.CreatedAt.Add(app.StaticArtifactCleanupDelay)
	require.NoError(t, svc.ReconcileStaticArtifactCleanup(ctx))
	require.Empty(t, h.store.objects)
	prefix := "hosted/" + hosted.ID + "/" + rev.ID + "/"
	require.ElementsMatch(t, []string{prefix + "index.html", prefix + "late.js"}, h.store.deletedKeys)
}

type ambiguousStaticActivationRepository struct {
	*apphostingrepo.MemoryRepository
	commitErr error
}

func (r *ambiguousStaticActivationRepository) ActivateRevision(ctx context.Context, input app.ActivateRevisionInput) (app.ActivateRevisionOutput, error) {
	_, err := r.MemoryRepository.ActivateRevision(ctx, input)
	if err != nil {
		return app.ActivateRevisionOutput{}, err
	}
	return app.ActivateRevisionOutput{}, r.commitErr
}

func TestStaticUploadLostActivationAcknowledgmentKeepsReadyAssets(t *testing.T) {
	ctx := t.Context()
	now := time.Now().UTC()
	h := newB2TestHarness(t)
	repo := &ambiguousStaticActivationRepository{MemoryRepository: h.repo, commitErr: errors.New("activation acknowledgment lost")}
	svc, err := app.New(repo, h.store, h.audience, h.perms, h.audit, app.WithClock(func() time.Time { return now }))
	require.NoError(t, err)
	hosted, _, err := svc.CreateApp(ctx, h.alice, sohaapi.CreateHostedAppRequest{Name: "Activation proof"})
	require.NoError(t, err)
	source := makeTarGz(t, []tarEntrySpec{{name: "index.html", body: "<h1>fixture</h1>"}})
	input := domain.UploadRevisionInput{IdempotencyKey: "activation"}
	_, err = svc.UploadRevision(ctx, h.alice, hosted.ID, input, bytes.NewReader(source))
	require.ErrorIs(t, err, repo.commitErr)
	rev, err := repo.FindRevisionByIdempotencyKey(ctx, hosted.ID, input.IdempotencyKey)
	require.NoError(t, err)
	require.Equal(t, sohaapi.HostedAppRevisionStatusReady, rev.Status)
	key := "hosted/" + hosted.ID + "/" + rev.ID + "/index.html"
	require.Contains(t, h.store.objects, key)

	now = now.Add(app.StaticArtifactCleanupDelay)
	require.NoError(t, svc.ReconcileStaticArtifactCleanup(ctx))
	require.Contains(t, h.store.objects, key)
	require.Empty(t, h.store.deletedKeys)
	replayed, err := svc.UploadRevision(ctx, h.alice, hosted.ID, input, bytes.NewReader(source))
	require.NoError(t, err)
	require.True(t, replayed.ReusedIdempotencyKey)
	require.Equal(t, rev.ID, replayed.App.ActiveRevisionID)
}
