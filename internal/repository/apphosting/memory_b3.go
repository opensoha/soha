package apphosting

import (
	"context"
	"maps"
	"time"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	appapphosting "github.com/opensoha/soha/internal/application/apphosting"
	domainapphosting "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (r *MemoryRepository) WithLockedDynamicRuntime(ctx context.Context, appID string, use func(context.Context, domainapphosting.App, domainapphosting.RuntimeConfig) error) error {
	return r.WithLockedRuntimeConfig(ctx, appID, func(app domainapphosting.App, config domainapphosting.RuntimeConfig) error {
		if !r.settings.Enabled {
			return appapphosting.ErrDynamicRevisionFailed
		}
		return use(ctx, app, config)
	})
}

func (r *MemoryRepository) QueueDynamicRevision(_ context.Context, appID, revID, sourceKey, template string, database bool, dataSchema int, now time.Time) error {
	if sourceKey == "" || template == "" || !validDynamicDataSchema(database, dataSchema) {
		return apperrors.ErrInvalidArgument
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	list := r.revisions[appID]
	i := findRevisionIndex(list, revID)
	if i < 0 || list[i].Kind != sohaapi.HostedAppKindDynamic || list[i].Status != sohaapi.HostedAppRevisionStatusUploading {
		return apperrors.ErrConflict
	}
	rev := &list[i]
	app := r.appsByID[appID]
	config := r.runtimeConfigs[appID]
	if !canPublishDynamicRevision(app, config, rev.CreatedBy, database, len(config.Environment) > 0) {
		return apperrors.ErrAccessDenied
	}
	rev.Status = sohaapi.HostedAppRevisionStatusValidating
	rev.SourceKey = sourceKey
	rev.BuildTemplate = template
	rev.Database = database
	rev.DataSchema = dataSchema
	rev.BuildPhase = "queued"
	rev.NextCheckAt = now.UTC()
	if len(config.Environment) > 0 {
		rev.RuntimeEnvironment = maps.Clone(config.Environment)
	}
	return nil
}

func (r *MemoryRepository) ClaimDynamicRevision(_ context.Context, claimID string, now time.Time, ttl time.Duration) (domainapphosting.Revision, error) {
	if claimID == "" || ttl <= 0 {
		return domainapphosting.Revision{}, apperrors.ErrInvalidArgument
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	var candidate *domainapphosting.Revision
	for _, list := range r.revisions {
		for i := range list {
			rev := &list[i]
			if !dynamicRevisionDue(*rev, now) {
				continue
			}
			if candidate == nil || rev.NextCheckAt.Before(candidate.NextCheckAt) || (rev.NextCheckAt.Equal(candidate.NextCheckAt) && rev.ID < candidate.ID) {
				candidate = rev
			}
		}
	}
	if candidate == nil {
		return domainapphosting.Revision{}, apperrors.ErrNotFound
	}
	candidate.ClaimID = claimID
	candidate.ClaimExpiresAt = now.Add(ttl).UTC()
	return cloneRevision(*candidate), nil
}

func dynamicRevisionDue(rev domainapphosting.Revision, now time.Time) bool {
	return rev.Kind == sohaapi.HostedAppKindDynamic && (rev.Status == sohaapi.HostedAppRevisionStatusValidating || dynamicCleanupStatus(rev)) &&
		!rev.NextCheckAt.IsZero() && !rev.NextCheckAt.After(now) && !rev.ClaimExpiresAt.After(now)
}

func (r *MemoryRepository) AdvanceDynamicRevision(_ context.Context, revID, claimID, phase, imageRef string, next time.Time) error {
	previous, err := previousDynamicBuildPhase(phase, imageRef)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	rev := r.findClaimedRevisionLocked(revID, claimID)
	if rev == nil || (rev.BuildPhase != phase && rev.BuildPhase != previous) {
		return apperrors.ErrConflict
	}
	if imageRef == "" {
		imageRef = rev.ImageRef
	}
	if (rev.ImageRef != "" && rev.ImageRef != imageRef) || (phase == "starting" && imageRef == "") {
		return apperrors.ErrConflict
	}
	rev.ImageRef = imageRef
	rev.BuildPhase = phase
	rev.ClaimID = ""
	rev.ClaimExpiresAt = time.Time{}
	rev.NextCheckAt = next.UTC()
	return nil
}

func (r *MemoryRepository) FailDynamicRevision(_ context.Context, revID, claimID, stage, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	rev := r.findClaimedRevisionLocked(revID, claimID)
	if rev == nil {
		return apperrors.ErrConflict
	}
	rev.Status = sohaapi.HostedAppRevisionStatusFailed
	rev.FailureStage = stage
	rev.FailureReason = truncateString(reason, 1024)
	rev.BuildPhase = ""
	return nil
}

func (r *MemoryRepository) CompleteDynamicCleanup(_ context.Context, revID, claimID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	for _, list := range r.revisions {
		if i := findRevisionIndex(list, revID); i >= 0 && dynamicCleanupStatus(list[i]) && hasRevisionClaim(list[i], claimID, now) {
			clearDynamicRevisionWork(&list[i])
			return nil
		}
	}
	return apperrors.ErrConflict
}

func deletedRuntimeWork(rev domainapphosting.Revision, now time.Time) domainapphosting.Revision {
	if rev.ClaimExpiresAt.After(now) {
		now = rev.ClaimExpiresAt
	}
	sourceCleanupAt := rev.CreatedAt.Add(appapphosting.DynamicSourceCleanupDelay)
	if sourceCleanupAt.After(now) {
		now = sourceCleanupAt
	}
	return domainapphosting.Revision{
		ID: rev.ID, AppID: rev.AppID, Kind: sohaapi.HostedAppKindDynamic, Database: rev.Database,
		StorageIntegrationID: rev.StorageIntegrationID, StoragePrefix: rev.StoragePrefix, SourceKey: rev.SourceKey,
		NextCheckAt: now,
	}
}

func (r *MemoryRepository) ClaimDeletedDynamicRevision(_ context.Context, claimID string, now time.Time, ttl time.Duration) (domainapphosting.Revision, error) {
	if claimID == "" || ttl <= 0 {
		return domainapphosting.Revision{}, apperrors.ErrInvalidArgument
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	var candidate domainapphosting.Revision
	for _, rev := range r.deletedRuntimeWork {
		if rev.NextCheckAt.After(now) || rev.ClaimExpiresAt.After(now) {
			continue
		}
		if candidate.ID == "" || rev.NextCheckAt.Before(candidate.NextCheckAt) || (rev.NextCheckAt.Equal(candidate.NextCheckAt) && rev.ID < candidate.ID) {
			candidate = rev
		}
	}
	if candidate.ID == "" {
		return candidate, apperrors.ErrNotFound
	}
	candidate.ClaimID = claimID
	candidate.ClaimExpiresAt = now.Add(ttl)
	r.deletedRuntimeWork[candidate.ID] = candidate
	return candidate, nil
}

func (r *MemoryRepository) CompleteDeletedDynamicCleanup(_ context.Context, revID, claimID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !hasRevisionClaim(r.deletedRuntimeWork[revID], claimID, time.Now()) {
		return apperrors.ErrConflict
	}
	delete(r.deletedRuntimeWork, revID)
	return nil
}

func (r *MemoryRepository) RequeueDeletedDynamicCleanup(_ context.Context, rev domainapphosting.Revision) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.appsByID[rev.AppID]; exists {
		return nil
	}
	if work, exists := r.deletedRuntimeWork[rev.ID]; exists {
		work.ClaimID = ""
		work.ClaimExpiresAt = time.Time{}
		if rev.SourceKey != "" {
			work.SourceKey = rev.SourceKey
		}
		r.deletedRuntimeWork[rev.ID] = work
	} else {
		r.deletedRuntimeWork[rev.ID] = deletedRuntimeWork(rev, time.Now())
	}
	return nil
}

func (r *MemoryRepository) findClaimedRevisionLocked(revID, claimID string) *domainapphosting.Revision {
	now := time.Now()
	for _, list := range r.revisions {
		if i := findRevisionIndex(list, revID); i >= 0 && hasActiveDynamicClaim(list[i], claimID, now) {
			return &list[i]
		}
	}
	return nil
}
