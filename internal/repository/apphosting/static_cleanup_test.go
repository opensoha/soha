package apphosting

import (
	"testing"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	app "github.com/opensoha/soha/internal/application/apphosting"
	domain "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/stretchr/testify/require"
)

func testPostgresStaticArtifactCleanup(t *testing.T, repo *PostgresRepository) {
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	a, _, err := repo.CreateApp(ctx, domain.App{
		ID: "static-gc", Name: "Static GC", DNSLabel: "static-gc", OwnerUserID: "author",
		Kind: sohaapi.HostedAppKindStatic, Status: sohaapi.HostedAppStatusDraft,
		Visibility:      sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModePrivate},
		RouteGeneration: 1, CreatedAt: now, UpdatedAt: now,
	}, 40)
	require.NoError(t, err)
	for _, id := range []string{"static-gc-old", "static-gc-current"} {
		_, err := repo.CreateUploadingRevision(ctx, a.ID, domain.Revision{
			ID: id, AppID: a.ID, Kind: a.Kind, CreatedBy: a.OwnerUserID, CreatedAt: now, StorageIntegrationID: "fixture-store",
		})
		require.NoError(t, err)
		_, err = repo.ActivateRevision(ctx, app.ActivateRevisionInput{
			AppID: a.ID, RevisionID: id, ActorUserID: a.OwnerUserID, Now: now, RetainedRevisionsMax: 1,
			Files: []sohaapi.HostedAppManifestFile{{Path: "index.html", ObjectKey: a.ID + "/" + id + "/index.html"}},
		})
		require.NoError(t, err)
	}
	first, err := repo.ClaimStaticArtifactCleanup(ctx, "first", now, time.Minute)
	require.NoError(t, err)
	require.Equal(t, "static-gc-old", first.ID)
	require.Equal(t, a.ID+"/static-gc-old/index.html", first.Files[0].ObjectKey)
	_, err = repo.ClaimStaticArtifactCleanup(ctx, "early", now, time.Minute)
	require.ErrorIs(t, err, apperrors.ErrNotFound)

	retry, err := repo.ClaimStaticArtifactCleanup(ctx, "retry", now.Add(time.Minute), time.Minute)
	require.NoError(t, err)
	require.Equal(t, first.ID, retry.ID)
	require.ErrorIs(t, repo.CompleteStaticArtifactCleanup(ctx, first.ID, "first"), apperrors.ErrConflict)
	require.NoError(t, repo.CompleteStaticArtifactCleanup(ctx, retry.ID, "retry"))
	_, err = repo.ClaimStaticArtifactCleanup(ctx, "after", now.Add(2*time.Minute), time.Minute)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
	t.Run("hard-deleted-ready-and-in-flight", func(t *testing.T) { testPostgresHardDeletedStaticArtifacts(t, repo) })
	t.Run("failed-and-abandoned-uploads", func(t *testing.T) { testPostgresFailedStaticUploads(t, repo) })
}

func createStaticCleanupFixture(t *testing.T, repo *PostgresRepository, id string, now time.Time) domain.App {
	t.Helper()
	created, _, err := repo.CreateApp(t.Context(), domain.App{
		ID: id, Name: id, DNSLabel: id, OwnerUserID: "author", Kind: sohaapi.HostedAppKindStatic,
		Status: sohaapi.HostedAppStatusDraft, Visibility: sohaapi.HostedAppVisibilityScope{Mode: sohaapi.HostedAppVisibilityModePrivate},
		RouteGeneration: 1, CreatedAt: now, UpdatedAt: now,
	}, 40)
	require.NoError(t, err)
	return created
}

func createStaticCleanupUpload(t *testing.T, repo *PostgresRepository, hosted domain.App, id string, now time.Time) domain.Revision {
	t.Helper()
	stored, err := repo.CreateUploadingRevision(t.Context(), hosted.ID, domain.Revision{
		ID: id, AppID: hosted.ID, Kind: hosted.Kind, CreatedBy: hosted.OwnerUserID, CreatedAt: now,
		StorageIntegrationID: "fixture-store", StoragePrefix: "historic-prefix",
		Files: []sohaapi.HostedAppManifestFile{
			{Path: "index.html", ObjectKey: hosted.ID + "/" + id + "/index.html"},
			{Path: "static/main.js", ObjectKey: hosted.ID + "/" + id + "/static/main.js"},
		},
	})
	require.NoError(t, err)
	return stored
}

func testPostgresHardDeletedStaticArtifacts(t *testing.T, repo *PostgresRepository) {
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	hosted := createStaticCleanupFixture(t, repo, "static-hard-delete", now)
	ready := createStaticCleanupUpload(t, repo, hosted, "static-hard-delete-ready", now)
	_, err := repo.ActivateRevision(ctx, app.ActivateRevisionInput{
		AppID: hosted.ID, RevisionID: ready.ID, ActorUserID: hosted.OwnerUserID, Now: now, Files: ready.Files,
	})
	require.NoError(t, err)
	inFlight := createStaticCleanupUpload(t, repo, hosted, "static-hard-delete-in-flight", now)
	require.ErrorIs(t, repo.FailRevision(ctx, hosted.ID, ready.ID, "activate", "lost acknowledgment"), apperrors.ErrNotFound)

	// The cascade rolls back if even one manifest cannot be queued.
	require.NoError(t, repo.db.WithContext(ctx).Exec(`
		ALTER TABLE public.hosted_app_static_artifact_cleanup ADD CONSTRAINT reject_fixture_manifest
		CHECK (revision_id <> 'static-hard-delete-ready') NOT VALID
	`).Error)
	_, _, deleteErr := repo.DeleteApp(ctx, hosted.ID, nil)
	require.NoError(t, repo.db.WithContext(ctx).Exec(`
		ALTER TABLE public.hosted_app_static_artifact_cleanup DROP CONSTRAINT reject_fixture_manifest
	`).Error)
	require.Error(t, deleteErr)
	stillActive, err := repo.GetAppByID(ctx, hosted.ID)
	require.NoError(t, err)
	require.Equal(t, ready.ID, stillActive.ActiveRevisionID)
	_, err = repo.ClaimStaticArtifactCleanup(ctx, "rolled-back", now.Add(app.StaticArtifactCleanupDelay-time.Microsecond), time.Minute)
	require.ErrorIs(t, err, apperrors.ErrNotFound)

	_, _, err = repo.DeleteApp(ctx, hosted.ID, nil)
	require.NoError(t, err)
	_, err = repo.GetRevision(ctx, hosted.ID, ready.ID)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
	first, err := repo.ClaimStaticArtifactCleanup(ctx, "ready", now, time.Minute)
	require.NoError(t, err)
	require.Equal(t, ready.ID, first.ID)
	require.Equal(t, hosted.ID, first.AppID)
	require.Equal(t, ready.StorageIntegrationID, first.StorageIntegrationID)
	require.Equal(t, ready.StoragePrefix, first.StoragePrefix)
	require.Equal(t, ready.Files, first.Files)
	_, err = repo.ClaimStaticArtifactCleanup(ctx, "before-retry", now, time.Minute)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
	retry, err := repo.ClaimStaticArtifactCleanup(ctx, "ready-retry", now.Add(time.Minute), time.Minute)
	require.NoError(t, err)
	require.Equal(t, first, retry)
	require.NoError(t, repo.CompleteStaticArtifactCleanup(ctx, retry.ID, "ready-retry"))
	claimed, err := repo.ClaimStaticArtifactCleanup(ctx, "in-flight", now.Add(app.StaticArtifactCleanupDelay), time.Minute)
	require.NoError(t, err)
	require.Equal(t, inFlight.ID, claimed.ID)
	require.Equal(t, inFlight.Files, claimed.Files)
	require.NoError(t, repo.CompleteStaticArtifactCleanup(ctx, claimed.ID, "in-flight"))
}

func testPostgresFailedStaticUploads(t *testing.T, repo *PostgresRepository) {
	ctx := t.Context()
	now := time.Now().UTC().Truncate(time.Microsecond)
	hosted := createStaticCleanupFixture(t, repo, "static-failed-uploads", now)
	failed := createStaticCleanupUpload(t, repo, hosted, "static-failed-put", now)
	require.NoError(t, repo.FailRevision(ctx, hosted.ID, failed.ID, "upload", "failed put"))
	rejected := createStaticCleanupUpload(t, repo, hosted, "static-failed-activation", now)
	activate := app.ActivateRevisionInput{AppID: hosted.ID, RevisionID: rejected.ID, ActorUserID: "outsider", Now: now, Files: rejected.Files}
	_, err := repo.ActivateRevision(ctx, activate)
	require.ErrorIs(t, err, apperrors.ErrAccessDenied)
	abandoned := createStaticCleanupUpload(t, repo, hosted, "static-abandoned-upload", now)

	due := now.Add(app.StaticArtifactCleanupDelay)
	_, err = repo.ClaimStaticArtifactCleanup(ctx, "before-grace", due.Add(-time.Microsecond), time.Minute)
	require.ErrorIs(t, err, apperrors.ErrNotFound)
	stored, err := repo.GetRevision(ctx, hosted.ID, abandoned.ID)
	require.NoError(t, err)
	require.Equal(t, sohaapi.HostedAppRevisionStatusUploading, stored.Status)

	proofs := map[string][]sohaapi.HostedAppManifestFile{failed.ID: failed.Files, rejected.ID: rejected.Files, abandoned.ID: abandoned.Files}
	for range 3 {
		claimed, err := repo.ClaimStaticArtifactCleanup(ctx, "after-grace", due, time.Minute)
		require.NoError(t, err)
		require.Equal(t, proofs[claimed.ID], claimed.Files)
		delete(proofs, claimed.ID)
		require.NoError(t, repo.CompleteStaticArtifactCleanup(ctx, claimed.ID, "after-grace"))
	}
	require.Empty(t, proofs)
	stored, err = repo.GetRevision(ctx, hosted.ID, abandoned.ID)
	require.NoError(t, err)
	require.Equal(t, sohaapi.HostedAppRevisionStatusFailed, stored.Status)
	_, err = repo.ActivateRevision(ctx, app.ActivateRevisionInput{
		AppID: hosted.ID, RevisionID: abandoned.ID, ActorUserID: hosted.OwnerUserID, Now: due, Files: abandoned.Files,
	})
	require.ErrorIs(t, err, apperrors.ErrConflict)
}
