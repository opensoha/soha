package apphosting

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/distribution/reference"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/gorm"
)

const imageReferenceLockKey = `pg_catalog.hashtextextended('hosted_app_managed_image/' || ?::text, 0)`

func imageReferenceKey(imageRef string) string {
	named, err := reference.ParseNamed(imageRef)
	if err != nil || reference.Domain(named) == "" {
		return ""
	}
	digested, ok := named.(reference.Digested)
	if !ok || digested.Digest().Algorithm() != "sha256" {
		return ""
	}
	return strings.ToLower(reference.Domain(named)) + "/" + reference.Path(named) + "@" + digested.Digest().String()
}

func lockAvailableImageReference(tx *gorm.DB, imageKey string) error {
	if imageKey == "" {
		return apperrors.ErrInvalidArgument
	}
	if err := tx.Exec(`SELECT pg_catalog.pg_advisory_xact_lock(`+imageReferenceLockKey+`)`, imageKey).Error; err != nil {
		return err
	}
	var deleting bool
	err := tx.Raw(`SELECT EXISTS (SELECT 1 FROM public.hosted_app_managed_images WHERE image_ref = ? AND delete_started)`, imageKey).Row().Scan(&deleting)
	if err == nil && deleting {
		return apperrors.ErrConflict
	}
	return err
}

func (r *PostgresRepository) AdvanceManagedDynamicBuild(ctx context.Context, revisionID, claimID, imageRef string, next time.Time) error {
	if imageRef == "" || imageReferenceKey(imageRef) != imageRef {
		return apperrors.ErrInvalidArgument
	}
	return dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		tx := dbtx.DB(ctx, r.db)
		if err := lockAvailableImageReference(tx, imageRef); err != nil {
			return err
		}
		if err := advanceDynamicRevisionTx(tx, revisionID, claimID, "starting", "building", imageRef, next); err != nil {
			return err
		}
		return insertManagedImage(tx, `SELECT ?, ?`, revisionID, imageRef)
	})
}

func (r *PostgresRepository) RecordTerminalManagedImage(ctx context.Context, revisionID, claimID, imageRef string, deleted bool) error {
	if revisionID == "" || claimID == "" || imageRef == "" || imageReferenceKey(imageRef) != imageRef {
		return apperrors.ErrInvalidArgument
	}
	return dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		tx := dbtx.DB(ctx, r.db)
		if err := lockAvailableImageReference(tx, imageRef); err != nil {
			return err
		}
		if deleted {
			return insertManagedImage(tx, `
				SELECT revision_id, ? FROM public.hosted_app_deleted_runtime_cleanup
				WHERE revision_id = ? AND claim_id = ? AND claim_expires_at > pg_catalog.clock_timestamp()
				FOR UPDATE`, imageRef, revisionID, claimID)
		}
		return insertManagedImage(tx, `
			SELECT id, ? FROM public.hosted_app_revisions
			WHERE id = ? AND claim_id = ? AND claim_expires_at > pg_catalog.clock_timestamp()
				AND kind = 'dynamic' AND status IN ('failed', 'evicted')
			FOR UPDATE`, imageRef, revisionID, claimID)
	})
}

func insertManagedImage(tx *gorm.DB, source string, args ...any) error {
	return mapPostgresRevisionWriteError(dynamicRevisionWriteResult(tx.Exec(`
		INSERT INTO public.hosted_app_managed_images AS image (revision_id, image_ref) `+source+`
		ON CONFLICT (revision_id) DO UPDATE SET image_ref = EXCLUDED.image_ref
		WHERE image.image_ref = EXCLUDED.image_ref AND NOT image.delete_started`, args...)))
}

func (r *PostgresRepository) completeTerminalDynamicRuntimeCleanup(ctx context.Context, revisionID, claimID, query string) error {
	return dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		tx := dbtx.DB(ctx, r.db)
		if err := dynamicRevisionWriteResult(tx.Exec(query, revisionID, claimID)); err != nil {
			return err
		}
		return tx.Exec(`
			UPDATE public.hosted_app_managed_images SET cleanup_ready_at = pg_catalog.statement_timestamp()
			WHERE revision_id = ? AND cleanup_ready_at IS NULL`, revisionID).Error
	})
}

const managedImageCleanupEligible = `image.deleted_at IS NULL AND image.cleanup_ready_at IS NOT NULL
	AND (image.claim_expires_at IS NULL OR image.claim_expires_at <= ?)
	AND NOT EXISTS (
		SELECT 1 FROM public.hosted_app_revisions AS rev
		WHERE (rev.id = image.revision_id OR rev.image_key = image.image_ref)
			AND (rev.status NOT IN ('failed', 'evicted') OR rev.next_check_at IS NOT NULL)
	)
	AND NOT EXISTS (
		SELECT 1 FROM public.hosted_apps AS app
		JOIN public.hosted_app_revisions AS rev ON rev.id = app.active_revision_id
		WHERE rev.id = image.revision_id OR rev.image_key = image.image_ref
	)
	AND NOT EXISTS (
		SELECT 1 FROM public.hosted_app_deleted_runtime_cleanup AS cleanup
		WHERE cleanup.revision_id = image.revision_id OR cleanup.image_key = image.image_ref
	)`

func (r *PostgresRepository) ClaimManagedImageCleanup(ctx context.Context, claimID string, now time.Time, ttl time.Duration) (string, string, error) {
	if claimID == "" || ttl <= 0 {
		return "", "", apperrors.ErrInvalidArgument
	}
	var revisionID, imageRef string
	err := dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		tx := dbtx.DB(ctx, r.db)
		if err := tx.Raw(`
			SELECT image_ref FROM public.hosted_app_managed_images AS image
			WHERE `+managedImageCleanupEligible+`
			ORDER BY cleanup_ready_at, revision_id LIMIT 1`, now.UTC()).Row().Scan(&imageRef); err != nil {
			return err
		}
		var locked bool
		if err := tx.Raw(`SELECT pg_catalog.pg_try_advisory_xact_lock(`+imageReferenceLockKey+`)`, imageRef).Row().Scan(&locked); err != nil {
			return err
		}
		if !locked {
			return apperrors.ErrNotFound
		}
		// Commit the tombstone before sending DELETE. A lost registry response cannot allow
		// new revisions to start referencing an image whose deletion is already in flight.
		return tx.Raw(`
			UPDATE public.hosted_app_managed_images AS image
			SET delete_started = true, claim_id = ?, claim_expires_at = ?
			WHERE image_ref = ? AND `+managedImageCleanupEligible+`
			RETURNING revision_id`, claimID, now.Add(ttl).UTC(), imageRef, now.UTC()).Row().Scan(&revisionID)
	})
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", apperrors.ErrNotFound
	}
	return revisionID, imageRef, err
}

func (r *PostgresRepository) CompleteManagedImageCleanup(ctx context.Context, revisionID, claimID string) error {
	return dynamicRevisionWriteResult(r.db.WithContext(ctx).Exec(`
		UPDATE public.hosted_app_managed_images
		SET deleted_at = pg_catalog.statement_timestamp(), claim_id = '', claim_expires_at = NULL
		WHERE revision_id = ? AND claim_id = ? AND delete_started AND deleted_at IS NULL
			AND claim_expires_at > pg_catalog.clock_timestamp()`, revisionID, claimID))
}
