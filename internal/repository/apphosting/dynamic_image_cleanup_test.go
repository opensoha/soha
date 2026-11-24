package apphosting

import (
	"strings"
	"testing"
	"time"

	appapphosting "github.com/opensoha/soha/internal/application/apphosting"
	domain "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/stretchr/testify/require"
)

func testPostgresManagedImageCleanup(t *testing.T, repo *PostgresRepository) {
	ctx := t.Context()
	now := time.Now().UTC()
	ttl := time.Minute
	digest := "sha256:" + strings.Repeat("b", 64)
	image := "registry.example/platform/hosted/soha-hosted-build-origin@" + digest
	shared := "REGISTRY.example/platform/hosted/soha-hosted-build-origin:alias@" + digest
	exec := func(query string, args ...any) {
		t.Helper()
		require.NoError(t, repo.db.WithContext(ctx).Exec(query, args...).Error)
	}
	assertProtected := func(at time.Time) {
		t.Helper()
		_, _, err := repo.ClaimManagedImageCleanup(ctx, "too-early", at, ttl)
		require.ErrorIs(t, err, apperrors.ErrNotFound)
	}
	exec(`INSERT INTO public.hosted_apps (id, name, dns_label, owner_user_id, kind)
		VALUES ('oci-origin', 'Origin', 'oci-origin', 'author', 'dynamic'),
		       ('oci-shared', 'Shared', 'oci-shared', 'author', 'dynamic')`)
	exec(`INSERT INTO public.hosted_app_revisions (id, app_id, revision_number, status, created_by,
		kind, build_phase, claim_id, claim_expires_at, next_check_at, created_at)
		VALUES ('oci-image', 'oci-origin', 1, 'validating', 'author', 'dynamic', 'building', 'build', ?, ?, ?),
		       ('oci-external', 'oci-origin', 2, 'validating', 'author', 'dynamic', 'building', 'external', ?, ?, ?)`,
		now.Add(ttl), now, now, now.Add(ttl), now, now)
	require.NoError(t, repo.AdvanceManagedDynamicBuild(ctx, "oci-image", "build", image, now))
	external := "registry.example/outside/unmanaged@" + digest
	require.NoError(t, repo.AdvanceDynamicRevision(ctx, "oci-external", "external", "starting", external, now))
	exec(`UPDATE public.hosted_app_revisions SET status = 'failed', next_check_at = NULL WHERE id = 'oci-external'`)
	assertProtected(now) // validating build result has not been activated
	exec(`UPDATE public.hosted_app_revisions SET status = 'evicted' WHERE id = 'oci-image'`)
	assertProtected(now) // terminal runtime cleanup is still due
	exec(`UPDATE public.hosted_app_revisions SET claim_id = 'finished-runtime', claim_expires_at = ? WHERE id = 'oci-image'`, now.Add(ttl))
	require.NoError(t, repo.CompleteDynamicCleanup(ctx, "oci-image", "finished-runtime"))
	exec(`UPDATE public.hosted_apps SET active_revision_id = 'oci-image' WHERE id = 'oci-origin'`)
	assertProtected(now) // the application still points to this image
	exec(`UPDATE public.hosted_apps SET active_revision_id = '' WHERE id = 'oci-origin'`)
	exec(`INSERT INTO public.hosted_app_revisions (id, app_id, revision_number, status, created_by, kind, image_ref, created_at)
		VALUES ('oci-alias', 'oci-shared', 1, 'ready', 'author', 'dynamic', ?, ?)`, shared, now)
	assertProtected(now) // another app can roll back to the same repository and digest
	exec(`UPDATE public.hosted_app_revisions SET status = 'validating', build_phase = 'queued' WHERE id = 'oci-alias'`)
	assertProtected(now) // another app has a queued reference including a tag and mixed-case registry host
	_, _, err := repo.DeleteApp(ctx, "oci-shared", func(domain.App) error { return nil })
	require.NoError(t, err)
	assertProtected(now) // deleted-app runtime cleanup still references the image
	later := now.Add(appapphosting.DynamicSourceCleanupDelay + time.Second)
	removed, err := repo.ClaimDeletedDynamicRevision(ctx, "runtime-gone", later, ttl)
	require.NoError(t, err)
	require.Equal(t, "oci-alias", removed.ID)
	require.Equal(t, shared, removed.ImageRef)
	require.NoError(t, repo.CompleteDeletedDynamicCleanup(ctx, removed.ID, removed.ClaimID))

	revisionID, imageRef, err := repo.ClaimManagedImageCleanup(ctx, "first-delete", later, ttl)
	require.NoError(t, err)
	require.Equal(t, "oci-image", revisionID)
	require.Equal(t, image, imageRef)
	assertProtected(later) // an ambiguous registry response leaves the claim in place
	exec(`UPDATE public.hosted_app_revisions SET status = 'validating', build_phase = 'building', image_ref = '',
		claim_id = 'late-reference', claim_expires_at = ? WHERE id = 'oci-external'`, later.Add(ttl))
	require.ErrorIs(t, repo.AdvanceDynamicRevision(ctx, "oci-external", "late-reference", "starting", shared, later), apperrors.ErrConflict)
	revisionID, imageRef, err = repo.ClaimManagedImageCleanup(ctx, "retry-delete", later.Add(ttl), ttl)
	require.NoError(t, err)
	require.Equal(t, "oci-image", revisionID)
	require.Equal(t, image, imageRef)
	require.ErrorIs(t, repo.CompleteManagedImageCleanup(ctx, revisionID, "first-delete"), apperrors.ErrConflict)
	require.NoError(t, repo.CompleteManagedImageCleanup(ctx, revisionID, "retry-delete"))
	assertProtected(later.Add(2 * ttl)) // completion survives restarts and the external image was never registered

	t.Run("hard-delete-before-build-result", func(t *testing.T) {
		exec(`INSERT INTO public.hosted_apps (id, name, dns_label, owner_user_id, kind)
			VALUES ('oci-gone', 'Gone', 'oci-gone', 'author', 'dynamic')`)
		exec(`INSERT INTO public.hosted_app_revisions (id, app_id, revision_number, status, created_by, kind, build_phase, created_at)
			VALUES ('oci-late-image', 'oci-gone', 1, 'validating', 'author', 'dynamic', 'building', ?)`, now)
		_, _, err := repo.DeleteApp(ctx, "oci-gone", func(domain.App) error { return nil })
		require.NoError(t, err)
		removed, err := repo.ClaimDeletedDynamicRevision(ctx, "capture", later, ttl)
		require.NoError(t, err)
		require.Equal(t, "oci-late-image", removed.ID)
		lateImage := "registry.example/platform/hosted/soha-hosted-build-late@" + digest
		require.ErrorIs(t, repo.RecordTerminalManagedImage(ctx, removed.ID, "stale", lateImage, true), apperrors.ErrConflict)
		require.NoError(t, repo.RecordTerminalManagedImage(ctx, removed.ID, removed.ClaimID, lateImage, true))
		assertProtected(later)
		require.NoError(t, repo.CompleteDeletedDynamicCleanup(ctx, removed.ID, removed.ClaimID))
		revisionID, imageRef, err := repo.ClaimManagedImageCleanup(ctx, "late-delete", later, ttl)
		require.NoError(t, err)
		require.Equal(t, removed.ID, revisionID)
		require.Equal(t, lateImage, imageRef)
		require.NoError(t, repo.CompleteManagedImageCleanup(ctx, revisionID, "late-delete"))
	})
}
