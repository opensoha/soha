package apphosting

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	domain "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type managedImageBackend interface {
	IsManagedImage(revisionID, imageRef string) bool
	ManagedImageForCleanup(ctx context.Context, revisionID string) (string, error)
	DeleteManagedImage(ctx context.Context, revisionID, imageRef string) error
}

type managedImageRepository interface {
	AdvanceManagedDynamicBuild(ctx context.Context, revisionID, claimID, imageRef string, next time.Time) error
	RecordTerminalManagedImage(ctx context.Context, revisionID, claimID, imageRef string, deleted bool) error
	ClaimManagedImageCleanup(ctx context.Context, claimID string, now time.Time, ttl time.Duration) (revisionID, imageRef string, err error)
	CompleteManagedImageCleanup(ctx context.Context, revisionID, claimID string) error
}

func (s *Service) managedImageCleanup() (managedImageRepository, managedImageBackend, bool) {
	repo, repoOK := s.repo.(managedImageRepository)
	backend, backendOK := s.dynamic.(managedImageBackend)
	return repo, backend, repoOK && backendOK
}

func (s *Service) recordTerminalManagedImage(ctx context.Context, rev domain.Revision, deleted bool) error {
	repo, backend, enabled := s.managedImageCleanup()
	if !enabled || rev.ImageRef != "" {
		return nil
	}
	// A build may have succeeded immediately before its app was deleted or the revision expired.
	image, err := backend.ManagedImageForCleanup(ctx, rev.ID)
	if err != nil {
		return err
	}
	if !backend.IsManagedImage(rev.ID, image) {
		return nil
	}
	return repo.RecordTerminalManagedImage(ctx, rev.ID, rev.ClaimID, image, deleted)
}

func (s *Service) reconcileManagedImageCleanup(ctx context.Context) error {
	repo, backend, enabled := s.managedImageCleanup()
	if !enabled {
		return nil
	}
	step, cancel := context.WithTimeout(ctx, revisionStepTimeout)
	defer cancel()
	claim := uuid.NewString()
	revisionID, imageRef, err := repo.ClaimManagedImageCleanup(step, claim, s.now(), revisionClaimTTL)
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := backend.DeleteManagedImage(step, revisionID, imageRef); err != nil {
		return fmt.Errorf("clean up hosted revision %s image: %w", revisionID, err)
	}
	return repo.CompleteManagedImageCleanup(step, revisionID, claim)
}
