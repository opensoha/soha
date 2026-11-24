package apphosting

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	staticArtifactUploadTimeout = 5 * time.Minute
	// StaticArtifactCleanupDelay allows a bounded upload and late delivery to settle.
	StaticArtifactCleanupDelay = staticArtifactUploadTimeout + time.Minute
)

func (s *Service) failStaticUpload(ctx context.Context, rev domain.Revision, stage, reason string, cause error) error {
	write, cancel := context.WithTimeout(context.WithoutCancel(ctx), revisionStepTimeout)
	defer cancel()
	err := s.repo.FailRevision(write, rev.AppID, rev.ID, stage, reason)
	// A ready revision may have committed before the activation acknowledgment was lost.
	// A deleted or already-failed revision has its own cleanup entry.
	if err == nil || errors.Is(err, apperrors.ErrNotFound) {
		return cause
	}
	return errors.Join(cause, err)
}

type staticCleanupRepository interface {
	ClaimStaticArtifactCleanup(context.Context, string, time.Time, time.Duration) (domain.Revision, error)
	CompleteStaticArtifactCleanup(context.Context, string, string) error
}

func (s *Service) StaticCleanupEnabled() bool {
	if s == nil || s.objectStore == nil {
		return false
	}
	_, ok := s.repo.(staticCleanupRepository)
	return ok
}

func (s *Service) RunStaticArtifactCleanup(ctx context.Context, report func(error)) {
	if !s.StaticCleanupEnabled() {
		return
	}
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			if err := s.ReconcileStaticArtifactCleanup(ctx); err != nil && ctx.Err() == nil && report != nil {
				report(err)
			}
		}
	}
}

func (s *Service) ReconcileStaticArtifactCleanup(ctx context.Context) error {
	if !s.StaticCleanupEnabled() {
		return nil
	}
	work, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	repo := s.repo.(staticCleanupRepository)
	claimID := uuid.NewString()
	revision, err := repo.ClaimStaticArtifactCleanup(work, claimID, s.now(), revisionClaimTTL)
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	prefix := revision.AppID + "/" + revision.ID + "/"
	keys := make([]string, 0, len(revision.Files))
	for _, file := range revision.Files {
		relative, err := normalizeBundleRelativePath(file.Path, false)
		if err != nil || file.ObjectKey != prefix+relative {
			return apperrors.ErrConflict
		}
		keys = append(keys, joinStoragePrefixKey(revision.StoragePrefix, file.ObjectKey))
	}
	if len(keys) > 0 {
		if err := s.objectStore.DeleteKeys(work, revision.StorageIntegrationID, keys); err != nil {
			return err
		}
	}
	return repo.CompleteStaticArtifactCleanup(work, revision.ID, claimID)
}
