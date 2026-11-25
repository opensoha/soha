package apphosting

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	app "github.com/opensoha/soha/internal/application/apphosting"
	domain "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"gorm.io/gorm"
)

const expiredStaticUpload = "static artifact upload did not complete before its deadline"

func staticArtifactCleanupAvailableAt(rev domain.Revision) time.Time {
	switch rev.Status {
	case sohaapi.HostedAppRevisionStatusReady, sohaapi.HostedAppRevisionStatusEvicted:
		return time.Time{}
	default:
		return rev.CreatedAt.Add(app.StaticArtifactCleanupDelay)
	}
}

func hasStaticArtifacts(rev domain.Revision) bool {
	return rev.Kind == sohaapi.HostedAppKindStatic && rev.StorageIntegrationID != "" && len(rev.Files) > 0
}

func queueStaticArtifactCleanup(tx *gorm.DB, rev domain.Revision, now time.Time) error {
	if !hasStaticArtifacts(rev) {
		return nil
	}
	files, err := json.Marshal(rev.Files)
	if err != nil {
		return err
	}
	var availableAt any
	if due := staticArtifactCleanupAvailableAt(rev); !due.IsZero() {
		availableAt = due.UTC()
	}
	return tx.Exec(`
		INSERT INTO public.hosted_app_static_artifact_cleanup
			(revision_id, app_id, storage_integration_id, storage_prefix, manifest_json, queued_at, available_at)
		VALUES (?, ?, ?, ?, ?::jsonb, ?, ?) ON CONFLICT (revision_id) DO NOTHING
	`, rev.ID, rev.AppID, rev.StorageIntegrationID, rev.StoragePrefix, string(files), now, availableAt).Error
}

func enqueueDeletedStaticRevisionsTx(tx *gorm.DB, revisions []domain.Revision, now time.Time) error {
	for _, rev := range revisions {
		if err := queueStaticArtifactCleanup(tx, rev, now); err != nil {
			return err
		}
	}
	return nil
}

func failUploadingRevisionTx(tx *gorm.DB, rev domain.Revision, stage, reason string, now time.Time) error {
	result := tx.Exec(`
		UPDATE public.hosted_app_revisions SET status = 'failed', failure_stage = ?, failure_reason = ?
		WHERE app_id = ? AND id = ? AND status = 'uploading'
	`, stage, truncateString(reason, 1024), rev.AppID, rev.ID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	rev.Status = sohaapi.HostedAppRevisionStatusFailed
	return queueStaticArtifactCleanup(tx, rev, now)
}

func failRejectedRevisionActivationTx(tx *gorm.DB, rev domain.Revision, input app.ActivateRevisionInput, reason string) error {
	if rev.Kind == sohaapi.HostedAppKindStatic {
		return failUploadingRevisionTx(tx, rev, "activate", reason, input.Now.UTC())
	}
	return markRevisionActivationFailedTx(tx, input, reason)
}

func (r *PostgresRepository) expireStaticUpload(ctx context.Context, now time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		rev, err := scanHostedRevision(tx.Raw(`
			SELECT `+hostedRevisionColumns+` FROM public.hosted_app_revisions
			WHERE kind = 'static' AND status = 'uploading' AND created_at <= ?
			ORDER BY created_at, id LIMIT 1 FOR UPDATE SKIP LOCKED
		`, now.Add(-app.StaticArtifactCleanupDelay)).Row())
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		return failUploadingRevisionTx(tx, rev, "upload", expiredStaticUpload, now)
	})
}

func (r *PostgresRepository) ClaimStaticArtifactCleanup(ctx context.Context, claim string, now time.Time, ttl time.Duration) (domain.Revision, error) {
	if err := r.expireStaticUpload(ctx, now); err != nil {
		return domain.Revision{}, err
	}
	var rev domain.Revision
	var manifest []byte
	err := r.db.WithContext(ctx).Raw(`
		UPDATE public.hosted_app_static_artifact_cleanup
		SET claim_id = ?, claim_expires_at = ?
		WHERE revision_id = (
			SELECT revision_id FROM public.hosted_app_static_artifact_cleanup
			WHERE (available_at IS NULL OR available_at <= ?)
				AND (claim_expires_at IS NULL OR claim_expires_at <= ?)
			ORDER BY queued_at, revision_id LIMIT 1 FOR UPDATE SKIP LOCKED
		)
		RETURNING revision_id, app_id, storage_integration_id, storage_prefix, manifest_json
	`, claim, now.Add(ttl), now, now).Row().Scan(&rev.ID, &rev.AppID, &rev.StorageIntegrationID, &rev.StoragePrefix, &manifest)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Revision{}, apperrors.ErrNotFound
	}
	if err != nil {
		return domain.Revision{}, err
	}
	rev.Kind = sohaapi.HostedAppKindStatic
	if err := json.Unmarshal(manifest, &rev.Files); err != nil {
		return domain.Revision{}, err
	}
	return rev, nil
}

func (r *PostgresRepository) CompleteStaticArtifactCleanup(ctx context.Context, id, claim string) error {
	return dynamicRevisionWriteResult(r.db.WithContext(ctx).Exec(`
		DELETE FROM public.hosted_app_static_artifact_cleanup WHERE revision_id = ? AND claim_id = ?
	`, id, claim))
}

func (r *MemoryRepository) ClaimStaticArtifactCleanup(_ context.Context, claim string, now time.Time, ttl time.Duration) (domain.Revision, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.expireStaticUploadLocked(now)
	var next domain.Revision
	for _, item := range r.staticCleanup {
		if item.NextCheckAt.After(now) || item.ClaimExpiresAt.After(now) {
			continue
		}
		if next.ID == "" || item.NextCheckAt.Before(next.NextCheckAt) || (item.NextCheckAt.Equal(next.NextCheckAt) && item.ID < next.ID) {
			next = item
		}
	}
	if next.ID == "" {
		return domain.Revision{}, apperrors.ErrNotFound
	}
	next.ClaimID, next.ClaimExpiresAt = claim, now.Add(ttl)
	r.staticCleanup[next.ID] = next
	return cloneRevision(next), nil
}

func (r *MemoryRepository) CompleteStaticArtifactCleanup(_ context.Context, id, claim string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	item, ok := r.staticCleanup[id]
	if !ok || item.ClaimID != claim {
		return apperrors.ErrConflict
	}
	delete(r.staticCleanup, id)
	return nil
}

func (r *MemoryRepository) queueStaticArtifactCleanupLocked(rev domain.Revision) {
	if _, queued := r.staticCleanup[rev.ID]; queued || !hasStaticArtifacts(rev) {
		return
	}
	rev.NextCheckAt = staticArtifactCleanupAvailableAt(rev)
	r.staticCleanup[rev.ID] = cloneRevision(rev)
}

func (r *MemoryRepository) markRevisionFailedLocked(rev *domain.Revision, stage, reason string) {
	rev.Status = sohaapi.HostedAppRevisionStatusFailed
	rev.FailureStage = stage
	rev.FailureReason = truncateString(reason, 1024)
	r.queueStaticArtifactCleanupLocked(*rev)
}

func (r *MemoryRepository) failRejectedRevisionActivationLocked(rev *domain.Revision, reason string, now time.Time) {
	r.markRevisionFailedLocked(rev, "activate", reason)
	clearDynamicRevisionWork(rev)
	if dynamicCleanupStatus(*rev) {
		rev.NextCheckAt = now
	}
}

func (r *MemoryRepository) expireStaticUploadLocked(now time.Time) {
	cutoff := now.Add(-app.StaticArtifactCleanupDelay)
	var oldest *domain.Revision
	for _, revisions := range r.revisions {
		for i := range revisions {
			rev := &revisions[i]
			if rev.Kind != sohaapi.HostedAppKindStatic || rev.Status != sohaapi.HostedAppRevisionStatusUploading || rev.CreatedAt.After(cutoff) {
				continue
			}
			if oldest == nil || rev.CreatedAt.Before(oldest.CreatedAt) || (rev.CreatedAt.Equal(oldest.CreatedAt) && rev.ID < oldest.ID) {
				oldest = rev
			}
		}
	}
	if oldest != nil {
		r.markRevisionFailedLocked(oldest, "upload", expiredStaticUpload)
	}
}
