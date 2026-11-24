package apphosting

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appapphosting "github.com/opensoha/soha/internal/application/apphosting"
	domainapphosting "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/dbtx"
	"gorm.io/gorm"
)

func (r *PostgresRepository) WithLockedDynamicRuntime(ctx context.Context, appID string, use func(context.Context, domainapphosting.App, domainapphosting.RuntimeConfig) error) error {
	return dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		tx := dbtx.DB(ctx, r.db)
		if err := lockEnabledDynamicHosting(ctx, tx); err != nil {
			return err
		}
		app, err := getAppWhere(ctx, tx, "id = ?", appID, true)
		if err != nil {
			return err
		}
		config, err := loadRuntimeConfig(tx, app.ID)
		if err != nil {
			return err
		}
		return use(ctx, app, config)
	})
}

func lockAppHostingSettings(tx *gorm.DB) error {
	// Acquire the settings lock before the app lock, in the same order as visibility updates.
	return tx.Exec(`SELECT pg_advisory_xact_lock_shared(hashtextextended('app_hosting_settings/default', 0))`).Error
}

func lockEnabledDynamicHosting(ctx context.Context, tx *gorm.DB) error {
	_, err := loadLockedAppHostingSettings(ctx, tx, true)
	return err
}

func loadLockedAppHostingSettings(ctx context.Context, tx *gorm.DB, requireEnabled bool) (domainapphosting.Settings, error) {
	if err := lockAppHostingSettings(tx); err != nil {
		return domainapphosting.Settings{}, err
	}
	settings, err := loadSettingsRow(ctx, tx)
	if err != nil {
		return domainapphosting.Settings{}, err
	}
	if requireEnabled && !settings.Enabled {
		return domainapphosting.Settings{}, appapphosting.ErrDynamicRevisionFailed
	}
	return settings, nil
}

func (r *PostgresRepository) QueueDynamicRevision(ctx context.Context, appID, revID, sourceKey, template string, database bool, dataSchema int, now time.Time) error {
	if sourceKey == "" || template == "" || !validDynamicDataSchema(database, dataSchema) {
		return apperrors.ErrInvalidArgument
	}
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		app, err := getAppWhere(ctx, tx, "id = ?", appID, true)
		if errors.Is(err, apperrors.ErrNotFound) {
			return apperrors.ErrConflict
		}
		if err != nil {
			return err
		}
		rev, err := getRevisionTx(ctx, tx, appID, revID, true)
		if errors.Is(err, apperrors.ErrNotFound) {
			return apperrors.ErrConflict
		}
		if err != nil {
			return err
		}
		if rev.Kind != sohaapi.HostedAppKindDynamic || rev.Status != sohaapi.HostedAppRevisionStatusUploading {
			return apperrors.ErrConflict
		}
		config, err := loadRuntimeConfig(tx, appID)
		if err != nil {
			return err
		}
		if !canPublishDynamicRevision(app, config, rev.CreatedBy, database, len(config.Environment) > 0) {
			return apperrors.ErrAccessDenied
		}
		return dynamicRevisionWriteResult(tx.Exec(`
			UPDATE public.hosted_app_revisions
			SET status = 'validating', build_phase = 'queued', source_key = ?, build_template = ?,
				database_enabled = ?, data_schema = ?, next_check_at = ?,
				runtime_environment = (SELECT NULLIF(environment_ciphertexts, '{}'::jsonb)
					FROM public.hosted_app_runtime_configs WHERE app_id = ?)
			WHERE app_id = ? AND id = ?
		`, sourceKey, template, database, dataSchema, now.UTC(), appID, appID, revID))
	})
}

func (r *PostgresRepository) ClaimDynamicRevision(ctx context.Context, claimID string, now time.Time, ttl time.Duration) (domainapphosting.Revision, error) {
	if claimID == "" || ttl <= 0 {
		return domainapphosting.Revision{}, apperrors.ErrInvalidArgument
	}
	rev, err := scanHostedRevision(r.db.WithContext(ctx).Raw(`
		UPDATE public.hosted_app_revisions
		SET claim_id = ?, claim_expires_at = ?
		WHERE id = (
			SELECT id FROM public.hosted_app_revisions
			WHERE kind = 'dynamic' AND status IN ('validating', 'failed', 'evicted') AND next_check_at <= ?
				AND (claim_expires_at IS NULL OR claim_expires_at <= ?)
			ORDER BY next_check_at, id
			LIMIT 1 FOR UPDATE SKIP LOCKED
		)
		RETURNING `+hostedRevisionColumns, claimID, now.Add(ttl).UTC(), now.UTC(), now.UTC()).Row())
	if errors.Is(err, sql.ErrNoRows) {
		return domainapphosting.Revision{}, apperrors.ErrNotFound
	}
	return rev, err
}

func (r *PostgresRepository) AdvanceDynamicRevision(ctx context.Context, revID, claimID, phase, imageRef string, next time.Time) error {
	previous, err := previousDynamicBuildPhase(phase, imageRef)
	if err != nil {
		return err
	}
	if imageRef == "" {
		return advanceDynamicRevisionTx(r.db.WithContext(ctx), revID, claimID, phase, previous, imageRef, next)
	}
	return dbtx.Within(ctx, r.db, func(ctx context.Context) error {
		tx := dbtx.DB(ctx, r.db)
		if err := lockAvailableImageReference(tx, imageReferenceKey(imageRef)); err != nil {
			return err
		}
		return advanceDynamicRevisionTx(tx, revID, claimID, phase, previous, imageRef, next)
	})
}

func advanceDynamicRevisionTx(tx *gorm.DB, revID, claimID, phase, previous, imageRef string, next time.Time) error {
	return dynamicRevisionWriteResult(tx.Exec(`
		UPDATE public.hosted_app_revisions
		SET build_phase = ?, image_ref = COALESCE(NULLIF(?, ''), image_ref), next_check_at = ?,
			claim_id = '', claim_expires_at = NULL
		WHERE id = ? AND claim_id = ? AND kind = 'dynamic' AND status = 'validating'
			AND claim_expires_at > pg_catalog.clock_timestamp()
			AND build_phase IN (?, ?)
			AND (image_ref = '' OR ? = '' OR image_ref = ?)
			AND (? <> 'starting' OR image_ref <> '' OR ? <> '')
	`, phase, imageRef, next.UTC(), revID, claimID, phase, previous, imageRef, imageRef, phase, imageRef))
}

func (r *PostgresRepository) FailDynamicRevision(ctx context.Context, revID, claimID, stage, reason string) error {
	return dynamicRevisionWriteResult(r.db.WithContext(ctx).Exec(`
		UPDATE public.hosted_app_revisions
		SET status = 'failed', failure_stage = ?, failure_reason = ?, build_phase = ''
		WHERE id = ? AND claim_id = ? AND kind = 'dynamic' AND status = 'validating'
			AND claim_expires_at > pg_catalog.clock_timestamp()
	`, stage, truncateString(reason, 1024), revID, claimID))
}

func (r *PostgresRepository) CompleteDynamicCleanup(ctx context.Context, revID, claimID string) error {
	return r.completeTerminalDynamicRuntimeCleanup(ctx, revID, claimID, `
		UPDATE public.hosted_app_revisions
		SET claim_id = '', claim_expires_at = NULL, next_check_at = NULL
		WHERE id = ? AND claim_id = ? AND kind = 'dynamic' AND status IN ('failed', 'evicted')
			AND claim_expires_at > pg_catalog.clock_timestamp()
	`)
}

func enqueueDeletedDynamicRevisionsTx(tx *gorm.DB, appID string) error {
	// DeleteApp locks revisions first, so cleanup can wait for an in-flight worker's lease to expire.
	return tx.Exec(`
		INSERT INTO public.hosted_app_deleted_runtime_cleanup (
			revision_id, app_id, storage_integration_id, storage_prefix, source_key, database_enabled, image_ref, next_check_at
		)
		SELECT id, app_id, storage_integration_id, storage_prefix, source_key, database_enabled, image_ref,
			GREATEST(pg_catalog.statement_timestamp(), claim_expires_at, created_at + (?::bigint * INTERVAL '1 second'))
		FROM public.hosted_app_revisions
		WHERE app_id = ? AND kind = 'dynamic'
	`, int64(appapphosting.DynamicSourceCleanupDelay/time.Second), appID).Error
}

func (r *PostgresRepository) ClaimDeletedDynamicRevision(ctx context.Context, claimID string, now time.Time, ttl time.Duration) (domainapphosting.Revision, error) {
	if claimID == "" || ttl <= 0 {
		return domainapphosting.Revision{}, apperrors.ErrInvalidArgument
	}
	rev := domainapphosting.Revision{Kind: sohaapi.HostedAppKindDynamic, ClaimID: claimID}
	err := r.db.WithContext(ctx).Raw(`
		UPDATE public.hosted_app_deleted_runtime_cleanup
		SET claim_id = ?, claim_expires_at = ?
		WHERE revision_id = (
			SELECT revision_id FROM public.hosted_app_deleted_runtime_cleanup
			WHERE next_check_at <= ? AND (claim_expires_at IS NULL OR claim_expires_at <= ?)
			ORDER BY next_check_at, revision_id LIMIT 1 FOR UPDATE SKIP LOCKED
		)
		RETURNING revision_id, app_id, storage_integration_id, storage_prefix, source_key, database_enabled, image_ref
	`, claimID, now.Add(ttl).UTC(), now.UTC(), now.UTC()).Row().Scan(&rev.ID, &rev.AppID, &rev.StorageIntegrationID,
		&rev.StoragePrefix, &rev.SourceKey, &rev.Database, &rev.ImageRef)
	if errors.Is(err, sql.ErrNoRows) {
		return domainapphosting.Revision{}, apperrors.ErrNotFound
	}
	return rev, err
}

func (r *PostgresRepository) CompleteDeletedDynamicCleanup(ctx context.Context, revID, claimID string) error {
	return r.completeTerminalDynamicRuntimeCleanup(ctx, revID, claimID, `
		DELETE FROM public.hosted_app_deleted_runtime_cleanup
		WHERE revision_id = ? AND claim_id = ? AND claim_expires_at > pg_catalog.clock_timestamp()
	`)
}

func (r *PostgresRepository) RequeueDeletedDynamicCleanup(ctx context.Context, rev domainapphosting.Revision) error {
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO public.hosted_app_deleted_runtime_cleanup AS cleanup (
			revision_id, app_id, storage_integration_id, storage_prefix, source_key, database_enabled, image_ref, next_check_at
		)
		SELECT ?, ?, ?, ?, ?, ?, ?, GREATEST(pg_catalog.statement_timestamp(), ?::timestamptz, ?::timestamptz)
		WHERE NOT EXISTS (SELECT 1 FROM public.hosted_apps WHERE id = ?)
		ON CONFLICT (revision_id) DO UPDATE SET claim_id = '', claim_expires_at = NULL,
			source_key = COALESCE(NULLIF(EXCLUDED.source_key, ''), cleanup.source_key)
	`, rev.ID, rev.AppID, rev.StorageIntegrationID, rev.StoragePrefix, rev.SourceKey, rev.Database, rev.ImageRef,
		rev.ClaimExpiresAt.UTC(), rev.CreatedAt.Add(appapphosting.DynamicSourceCleanupDelay).UTC(), rev.AppID).Error
}

func dynamicRevisionWriteResult(res *gorm.DB) error {
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return apperrors.ErrConflict
	}
	return nil
}

func revisionActivationRejectionTx(tx *gorm.DB, app domainapphosting.App, rev domainapphosting.Revision, input appapphosting.ActivateRevisionInput) (string, error, error) {
	var config domainapphosting.RuntimeConfig
	if len(rev.RuntimeEnvironment) > 0 || (rev.Database && !app.IsOwner(input.ActorUserID)) {
		var err error
		config, err = loadRuntimeConfig(tx, app.ID)
		if err != nil {
			return "", nil, err
		}
	}
	if reason, rejection := revisionActivationRejection(app, rev, input, config); rejection != nil {
		return reason, rejection, nil
	}
	if rev.Kind != sohaapi.HostedAppKindDynamic {
		return "", nil, nil
	}
	var newerActivated bool
	if err := tx.Raw(`
		SELECT EXISTS (
			SELECT 1 FROM public.hosted_app_revisions
			WHERE app_id = ? AND revision_number > ? AND activated_at IS NOT NULL
		)
	`, rev.AppID, rev.RevisionNumber).Row().Scan(&newerActivated); err != nil {
		return "", nil, err
	}
	if newerActivated {
		reason, rejection := revisionSuperseded()
		return reason, rejection, nil
	}
	return "", nil, nil
}

func markRevisionActivationFailedTx(tx *gorm.DB, input appapphosting.ActivateRevisionInput, reason string) error {
	return tx.Exec(`
		UPDATE public.hosted_app_revisions
		SET status = 'failed', failure_stage = 'activate', failure_reason = ?,
			build_phase = '', claim_id = '', claim_expires_at = NULL,
			next_check_at = CASE WHEN kind = 'dynamic' THEN ?::timestamptz ELSE NULL END
		WHERE app_id = ? AND id = ?
	`, reason, input.Now.UTC(), input.AppID, input.RevisionID).Error
}

func checkRollbackRuntimeTx(tx *gorm.DB, app domainapphosting.App, rev domainapphosting.Revision, actor string) error {
	var config domainapphosting.RuntimeConfig
	if len(rev.RuntimeEnvironment) > 0 || (rev.Database && !app.IsOwner(actor)) {
		var err error
		config, err = loadRuntimeConfig(tx, app.ID)
		if err != nil {
			return err
		}
	}
	return checkRollbackRuntime(app, rev, actor, config)
}
