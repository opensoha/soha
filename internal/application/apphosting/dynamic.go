package apphosting

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	domain "github.com/opensoha/soha/internal/domain/apphosting"
	"github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const (
	revisionStepTimeout = 15 * time.Second
	revisionClaimTTL    = 45 * time.Second
	revisionDeadline    = 20 * time.Minute
	// DynamicSourceCleanupDelay lets a bounded upload finish before its last possible cleanup.
	DynamicSourceCleanupDelay = revisionDeadline + revisionClaimTTL
)

var (
	ErrDynamicRevisionFailed = errors.New("dynamic hosting revision failed")
	dynamicRevisionPattern   = regexp.MustCompile(`^rev_[0-9a-f]{32}$`)
)

type DynamicBackend interface {
	StartBuild(context.Context, domain.Revision) error
	BuildResult(context.Context, domain.Revision) (string, bool, error)
	PrepareRuntime(context.Context, domain.Revision) error
	RuntimeReady(context.Context, domain.Revision) (bool, error)
	ActivateRuntime(context.Context, domain.Revision) error
	RestrictRuntime(context.Context, domain.Revision) error
	CheckRollback(context.Context, domain.Revision) error
	DeleteRuntime(context.Context, domain.Revision) error
	DeleteBuild(context.Context, domain.Revision) error
}

type dynamicRepository interface {
	deletedDynamicRepository
	QueueDynamicRevision(context.Context, string, string, string, string, bool, int, time.Time) error
	ClaimDynamicRevision(context.Context, string, time.Time, time.Duration) (domain.Revision, error)
	AdvanceDynamicRevision(context.Context, string, string, string, string, time.Time) error
	FailDynamicRevision(context.Context, string, string, string, string) error
	CompleteDynamicCleanup(context.Context, string, string) error
	RollbackRevisionAs(context.Context, string, string, string, string, time.Time) (domain.App, domain.Revision, bool, error)
	// The callback may check live publisher identity and perform a bounded handoff. It cannot reenter the app repository.
	WithLockedDynamicRuntime(context.Context, string, func(context.Context, domain.App, domain.RuntimeConfig) error) error
}

type deletedDynamicRepository interface {
	ClaimDeletedDynamicRevision(context.Context, string, time.Time, time.Duration) (domain.Revision, error)
	CompleteDeletedDynamicCleanup(context.Context, string, string) error
	RequeueDeletedDynamicCleanup(context.Context, domain.Revision) error
}

func WithDynamicBackend(backend DynamicBackend) Option {
	return func(s *Service) { s.dynamic = nilIfTypedNil(backend) }
}

func (s *Service) DynamicEnabled() bool { return s != nil && s.dynamic != nil }

// RunDynamic stops claiming revisions when the Core lifecycle context ends.
func (s *Service) RunDynamic(ctx context.Context, report func(error)) {
	if !s.DynamicEnabled() {
		return
	}
	poll := time.NewTicker(2 * time.Second)
	defer poll.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-poll.C:
			if err := s.ReconcileDynamicRevision(ctx); err != nil && ctx.Err() == nil && report != nil {
				report(err)
			}
		}
	}
}

func (s *Service) ReconcileDynamicRevision(ctx context.Context) error {
	if !s.DynamicEnabled() {
		return nil
	}
	repo := s.repo.(dynamicRepository)
	return errors.Join(s.reconcileDeletedDynamicRevision(ctx, repo), s.reconcileDynamicRevision(ctx, repo))
}

func (s *Service) reconcileDeletedDynamicRevision(ctx context.Context, repo deletedDynamicRepository) error {
	step, cancel := context.WithTimeout(ctx, revisionStepTimeout)
	defer cancel()
	rev, err := repo.ClaimDeletedDynamicRevision(step, uuid.NewString(), s.now(), revisionClaimTTL)
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if rev.SourceKey == "" && rev.StorageIntegrationID != "" {
		rev.SourceKey = dynamicSourceKey(rev)
	}
	if err := s.cleanupDynamicRevision(step, rev); err != nil {
		return fmt.Errorf("clean up deleted hosted app %s revision %s: %w", rev.AppID, rev.ID, err)
	}
	return repo.CompleteDeletedDynamicCleanup(step, rev.ID, rev.ClaimID)
}

func (s *Service) reconcileDynamicRevision(ctx context.Context, repo dynamicRepository) error {
	step, cancel := context.WithTimeout(ctx, revisionStepTimeout)
	defer cancel()
	rev, err := repo.ClaimDynamicRevision(step, uuid.NewString(), s.now(), revisionClaimTTL)
	if errors.Is(err, apperrors.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if rev.Status != sohaapi.HostedAppRevisionStatusValidating {
		return s.completeDynamicCleanup(step, repo, rev)
	}
	err = s.reconcileDynamicStep(step, repo, rev)
	if errors.Is(err, apperrors.ErrNotFound) {
		if _, readErr := s.repo.GetRevision(step, rev.AppID, rev.ID); errors.Is(readErr, apperrors.ErrNotFound) {
			return s.cleanupDynamicRevision(step, rev)
		}
	}
	if !errors.Is(err, ErrDynamicRevisionFailed) {
		return err
	}
	if failErr := repo.FailDynamicRevision(step, rev.ID, rev.ClaimID, rev.BuildPhase, err.Error()); failErr != nil {
		return failErr
	}
	cleanup, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), revisionStepTimeout)
	defer cancelCleanup()
	return s.completeDynamicCleanup(cleanup, repo, rev)
}

func (s *Service) reconcileDynamicStep(ctx context.Context, repo dynamicRepository, rev domain.Revision) error {
	if s.now().Sub(rev.CreatedAt) >= revisionDeadline {
		return errors.Join(ErrDynamicRevisionFailed, errors.New("deployment deadline exceeded"))
	}
	app, settings, err := s.loadDynamicDeployment(ctx, rev)
	if err != nil {
		return err
	}
	switch rev.BuildPhase {
	case "queued":
		if err := s.dynamic.StartBuild(ctx, rev); err != nil {
			return err
		}
		return repo.AdvanceDynamicRevision(ctx, rev.ID, rev.ClaimID, "building", "", s.now())
	case "building":
		return s.observeDynamicBuild(ctx, repo, rev)
	case "starting":
		return s.observeDynamicRuntime(ctx, repo, app, settings, rev)
	default:
		return ErrDynamicRevisionFailed
	}
}

func (s *Service) loadDynamicDeployment(ctx context.Context, rev domain.Revision) (domain.App, domain.Settings, error) {
	app, err := s.repo.GetAppByID(ctx, rev.AppID)
	if err != nil {
		return app, domain.Settings{}, err
	}
	settings, err := s.repo.GetSettings(ctx)
	if err != nil {
		return app, settings, err
	}
	if !settings.Enabled || !app.IsOwnerOrMaintainer(rev.CreatedBy) || !canReconcileDynamicApp(app) {
		return app, settings, ErrDynamicRevisionFailed
	}
	if err := s.checkSensitiveDynamicRevision(ctx, app, rev, rev.CreatedBy); err != nil {
		return app, settings, err
	}
	return app, settings.WithDefaults(), s.checkDynamicPublisher(ctx, rev.CreatedBy)
}

func (s *Service) checkDynamicPublisher(ctx context.Context, publisherID string) error {
	err := s.checkCurrentPublisher(ctx, publisherID)
	if errors.Is(err, apperrors.ErrAccessDenied) {
		return errors.Join(ErrDynamicRevisionFailed, err)
	}
	return err
}

func canReconcileDynamicApp(app domain.App) bool {
	return app.Kind == sohaapi.HostedAppKindDynamic && (app.Status == sohaapi.HostedAppStatusDraft || app.Status == sohaapi.HostedAppStatusOnline)
}

func (s *Service) observeDynamicBuild(ctx context.Context, repo dynamicRepository, rev domain.Revision) error {
	image, ready, err := s.dynamic.BuildResult(ctx, rev)
	if err != nil {
		return err
	}
	if !ready {
		return repo.AdvanceDynamicRevision(ctx, rev.ID, rev.ClaimID, "building", "", s.now().Add(3*time.Second))
	}
	return repo.AdvanceDynamicRevision(ctx, rev.ID, rev.ClaimID, "starting", image, s.now())
}

func (s *Service) prepareDynamicRuntime(ctx context.Context, repo dynamicRepository, rev domain.Revision) error {
	return repo.WithLockedDynamicRuntime(ctx, rev.AppID, func(handoff context.Context, app domain.App, config domain.RuntimeConfig) error {
		if !canReconcileDynamicApp(app) || !app.IsOwnerOrMaintainer(rev.CreatedBy) {
			return ErrDynamicRevisionFailed
		}
		if hasRuntimeCredentials(rev) {
			if err := checkSensitiveRuntimeConfig(app, config, rev, rev.CreatedBy); err != nil {
				return err
			}
		}
		if err := s.checkDynamicPublisher(handoff, rev.CreatedBy); err != nil {
			return err
		}
		return s.dynamic.PrepareRuntime(handoff, rev)
	})
}

func (s *Service) observeDynamicRuntime(ctx context.Context, repo dynamicRepository, app domain.App, settings domain.Settings, rev domain.Revision) error {
	if err := s.prepareDynamicRuntime(ctx, repo, rev); err != nil {
		return err
	}
	ready, err := s.dynamic.RuntimeReady(ctx, rev)
	if err != nil {
		return err
	}
	if !ready {
		return repo.AdvanceDynamicRevision(ctx, rev.ID, rev.ClaimID, "starting", rev.ImageRef, s.now().Add(3*time.Second))
	}
	grantAttempted := false
	activated, err := s.repo.ActivateRevision(ctx, ActivateRevisionInput{
		AppID: app.ID, RevisionID: rev.ID, ActorUserID: rev.CreatedBy, Now: s.now(),
		RetainedRevisionsMax: settings.RetainedRevisionsPerApp, DynamicClaimID: rev.ClaimID,
		BeforeActivate: func(activation context.Context) error {
			if err := s.checkDynamicPublisher(activation, rev.CreatedBy); err != nil {
				return err
			}
			grantAttempted = true
			promote, cancel := context.WithTimeout(activation, 5*time.Second)
			defer cancel()
			return s.dynamic.ActivateRuntime(promote, rev)
		},
	})
	if err != nil {
		if grantAttempted {
			return errors.Join(err, s.restoreCandidateAccess(ctx, rev))
		}
		return err
	}
	s.recordVisibilityUpdate(ctx, identity.Principal{UserID: rev.CreatedBy}, activated.Visibility)
	return s.dynamic.DeleteBuild(ctx, rev)
}

func (s *Service) restoreCandidateAccess(ctx context.Context, rev domain.Revision) error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	current, err := s.repo.GetRevision(cleanup, rev.AppID, rev.ID)
	if err != nil {
		return err
	}
	if current.Status == sohaapi.HostedAppRevisionStatusValidating && current.ClaimID == rev.ClaimID {
		return s.dynamic.RestrictRuntime(cleanup, rev)
	}
	return nil
}

func (s *Service) completeDynamicCleanup(ctx context.Context, repo dynamicRepository, rev domain.Revision) error {
	if err := s.cleanupDynamicRevision(ctx, rev); err != nil {
		return err
	}
	return repo.CompleteDynamicCleanup(ctx, rev.ID, rev.ClaimID)
}

func (s *Service) cleanupDynamicRevision(ctx context.Context, rev domain.Revision) error {
	if !s.DynamicEnabled() || rev.Kind != sohaapi.HostedAppKindDynamic {
		return nil
	}
	err := errors.Join(s.dynamic.DeleteRuntime(ctx, rev), s.dynamic.DeleteBuild(ctx, rev))
	if err != nil || rev.SourceKey == "" {
		return err
	}
	if s.objectStore == nil {
		return apperrors.ErrServiceUnavailable
	}
	return s.objectStore.DeleteKeys(ctx, rev.StorageIntegrationID, []string{rev.SourceKey})
}

func (s *Service) rollbackRevision(ctx context.Context, app domain.App, user, revisionID, idempotencyKey string) (domain.App, domain.Revision, bool, error) {
	if app.Kind == sohaapi.HostedAppKindDynamic {
		if !s.DynamicEnabled() {
			return domain.App{}, domain.Revision{}, false, apperrors.ErrServiceUnavailable
		}
		rev, err := s.repo.GetRevision(ctx, app.ID, revisionID)
		if err != nil {
			return domain.App{}, domain.Revision{}, false, err
		}
		if err := s.checkSensitiveDynamicRevision(ctx, app, rev, user); err != nil {
			return domain.App{}, domain.Revision{}, false, err
		}
		if err := s.dynamic.CheckRollback(ctx, rev); err != nil {
			return domain.App{}, domain.Revision{}, false, err
		}
	}
	if repository, ok := s.repo.(dynamicRepository); ok {
		return repository.RollbackRevisionAs(ctx, app.ID, revisionID, idempotencyKey, user, s.now())
	}
	return s.repo.RollbackRevision(ctx, app.ID, revisionID, idempotencyKey, s.now())
}

func (s *Service) checkSensitiveDynamicRevision(ctx context.Context, app domain.App, rev domain.Revision, publisherID string) error {
	if !hasRuntimeCredentials(rev) {
		return nil
	}
	var config domain.RuntimeConfig
	if repository, ok := s.repo.(runtimeConfigRepository); ok {
		var err error
		config, err = repository.GetRuntimeConfig(ctx, app.ID)
		if err != nil {
			return err
		}
	}
	return checkSensitiveRuntimeConfig(app, config, rev, publisherID)
}

func hasRuntimeCredentials(rev domain.Revision) bool {
	return rev.Database || len(rev.RuntimeEnvironment) > 0
}

func checkSensitiveRuntimeConfig(app domain.App, config domain.RuntimeConfig, rev domain.Revision, publisherID string) error {
	if !config.AllowsSensitivePublisher(app, publisherID) {
		return errors.Join(ErrDynamicRevisionFailed, apperrors.NewBusiness(apperrors.ErrAccessDenied, "sensitive_publish_authorization_required", "publishing this revision requires owner consent", "该版本使用运行凭据或业务数据库，发布或回滚需要作者授权"))
	}
	if !config.ContainsEnvironmentSnapshot(rev.RuntimeEnvironment) {
		return errors.Join(ErrDynamicRevisionFailed, apperrors.NewBusiness(apperrors.ErrConflict, "revision_environment_revoked", "a configured credential was replaced or removed", "该版本的环境变量已更换或移除"))
	}
	return nil
}
