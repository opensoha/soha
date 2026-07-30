package manifest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *DeclarativeService) Sync(ctx context.Context, principal domainidentity.Principal, packageID string, input domainmanifest.SyncInput, idempotencyKey string) (domainmanifest.SyncRun, domaindelivery.ExecutionTask, error) {
	if err := s.base.authorize(ctx, principal, appaccess.PermDeliveryManifestSourcesManage); err != nil {
		return domainmanifest.SyncRun{}, domaindelivery.ExecutionTask{}, err
	}
	item, err := s.base.Get(ctx, principal, strings.TrimSpace(packageID))
	if err != nil {
		return domainmanifest.SyncRun{}, domaindelivery.ExecutionTask{}, err
	}
	source, err := s.repository.GetSource(ctx, item.ID)
	if err != nil {
		return domainmanifest.SyncRun{}, domaindelivery.ExecutionTask{}, err
	}
	return s.queueSync(ctx, item, source, input, "manual", strings.TrimSpace(idempotencyKey), principal.UserID)
}

func (s *DeclarativeService) SyncWebhook(ctx context.Context, principal domainidentity.Principal, sourceID string, input domainmanifest.SyncWebhookInput) (domainmanifest.SyncRun, domaindelivery.ExecutionTask, error) {
	if err := s.base.authorize(ctx, principal, appaccess.PermDeliveryManifestSourcesManage); err != nil {
		return domainmanifest.SyncRun{}, domaindelivery.ExecutionTask{}, err
	}
	source, err := s.repository.GetSourceByID(ctx, strings.TrimSpace(sourceID))
	if err != nil {
		return domainmanifest.SyncRun{}, domaindelivery.ExecutionTask{}, err
	}
	if strings.TrimSpace(input.RepositoryID) != source.RepositoryID {
		return domainmanifest.SyncRun{}, domaindelivery.ExecutionTask{}, fmt.Errorf("%w: webhook repository does not match manifest source", apperrors.ErrInvalidArgument)
	}
	if source.SyncPolicy != domainmanifest.SyncPolicyWebhook {
		return domainmanifest.SyncRun{}, domaindelivery.ExecutionTask{}, fmt.Errorf("%w: manifest source is not configured for webhook synchronization", apperrors.ErrConflict)
	}
	expectedRef := source.RefValue
	if source.RefType == domainmanifest.SourceRefBranch {
		expectedRef = "refs/heads/" + source.RefValue
	} else if source.RefType == domainmanifest.SourceRefTag {
		expectedRef = "refs/tags/" + source.RefValue
	}
	if input.Ref != source.RefValue && input.Ref != expectedRef {
		return domainmanifest.SyncRun{}, domaindelivery.ExecutionTask{}, fmt.Errorf("%w: webhook ref does not match manifest source", apperrors.ErrConflict)
	}
	item, err := s.base.Get(ctx, principal, source.PackageID)
	if err != nil {
		return domainmanifest.SyncRun{}, domaindelivery.ExecutionTask{}, err
	}
	return s.queueSync(ctx, item, source, domainmanifest.SyncInput{ExpectedGeneration: source.Generation, RequestedCommit: input.Commit}, "webhook", "manifest-webhook:"+source.ID+":"+strings.TrimSpace(input.EventID), principal.UserID)
}

func (s *DeclarativeService) ListSyncRuns(ctx context.Context, principal domainidentity.Principal, packageID string) ([]domainmanifest.SyncRun, error) {
	if _, err := s.base.Get(ctx, principal, strings.TrimSpace(packageID)); err != nil {
		return nil, err
	}
	return s.repository.ListSyncRuns(ctx, strings.TrimSpace(packageID), 50)
}

func (s *DeclarativeService) queueSync(ctx context.Context, item domainmanifest.Package, source domainmanifest.Source, input domainmanifest.SyncInput, trigger, idempotencyKey, actor string) (domainmanifest.SyncRun, domaindelivery.ExecutionTask, error) {
	if s.sources == nil || s.tasks == nil || s.git == nil {
		return domainmanifest.SyncRun{}, domaindelivery.ExecutionTask{}, fmt.Errorf("%w: manifest Git synchronization is unavailable", apperrors.ErrInvalidArgument)
	}
	if source.Mode != domainmanifest.SourceModeGitSynced {
		return domainmanifest.SyncRun{}, domaindelivery.ExecutionTask{}, fmt.Errorf("%w: manifest source is not Git-synchronized", apperrors.ErrConflict)
	}
	if input.ExpectedGeneration != source.Generation {
		return domainmanifest.SyncRun{}, domaindelivery.ExecutionTask{}, fmt.Errorf("%w: manifest source generation changed", apperrors.ErrConflict)
	}
	if strings.TrimSpace(idempotencyKey) == "" {
		return domainmanifest.SyncRun{}, domaindelivery.ExecutionTask{}, fmt.Errorf("%w: Idempotency-Key is required", apperrors.ErrInvalidArgument)
	}
	repository, err := s.sources.GetRepository(ctx, source.RepositoryID)
	if err != nil {
		return domainmanifest.SyncRun{}, domaindelivery.ExecutionTask{}, err
	}
	payload := domainmanifest.TaskPayload{
		Action: domainmanifest.TaskActionSync, PackageID: item.ID, SourceID: source.ID,
		Generation: source.Generation, IdempotencyKey: idempotencyKey,
		RepositoryID: source.RepositoryID, RepositoryURL: repository.URL,
		Renderer: item.Renderer,
		RefType:  source.RefType, RefValue: source.RefValue, Path: source.Path,
		IncludePatterns: source.IncludePatterns, ExcludePatterns: source.ExcludePatterns,
		RequestedCommit: strings.TrimSpace(input.RequestedCommit), RequestedBy: actor,
	}
	now := time.Now().UTC()
	task := domaindelivery.ExecutionTask{
		ID: "task:" + uuid.NewString(), ApplicationID: item.ApplicationID,
		TaskKind: domainmanifest.TaskKindSync, ProviderKind: domainmanifest.TaskProviderGit,
		TargetKind: "manifest_source", Status: "queued", QueueKey: source.ID,
		LockKey: idempotencyKey, MaxRetries: 1, TimeoutSeconds: 180,
		CallbackToken: uuid.NewString(), Payload: structMap(payload), Result: map[string]any{},
		CreatedAt: now, UpdatedAt: now,
	}
	run := domainmanifest.SyncRun{
		ID: uuid.NewString(), SourceID: source.ID, PackageID: item.ID,
		ExecutionTaskID: task.ID, SourceGeneration: source.Generation, Trigger: trigger,
		Status: domainmanifest.SyncRunQueued, IdempotencyKey: idempotencyKey,
		RequestedCommit: payload.RequestedCommit, Files: []string{}, Actor: actor,
		CreatedAt: now, UpdatedAt: now,
	}
	existingID, created, err := s.repository.CreateSyncTask(ctx, run, task)
	if err != nil {
		return domainmanifest.SyncRun{}, domaindelivery.ExecutionTask{}, err
	}
	if !created {
		task, err = s.tasks.GetExecutionTaskInternal(ctx, existingID)
		if err != nil {
			return domainmanifest.SyncRun{}, domaindelivery.ExecutionTask{}, err
		}
		runs, listErr := s.repository.ListSyncRuns(ctx, item.ID, 100)
		if listErr != nil {
			return domainmanifest.SyncRun{}, domaindelivery.ExecutionTask{}, listErr
		}
		for _, existing := range runs {
			if existing.ExecutionTaskID == existingID {
				return existing, task, nil
			}
		}
	}
	return run, task, nil
}

func (s *DeclarativeService) pollGitSources(ctx context.Context) {
	sources, err := s.repository.ListDueSources(ctx, 20)
	if err != nil {
		return
	}
	for _, source := range sources {
		item, getErr := s.base.get(ctx, source.PackageID)
		if getErr != nil {
			continue
		}
		bucket := time.Now().UTC().Unix() / int64(max(source.PollIntervalSeconds, 30))
		_, _, _ = s.queueSync(ctx, item, source, domainmanifest.SyncInput{ExpectedGeneration: source.Generation}, "poll", fmt.Sprintf("manifest-poll:%s:%d:%d", source.ID, source.Generation, bucket), "system:manifest-poll")
	}
}

func (s *DeclarativeService) autoDeploySyncedRevision(ctx context.Context, payload domainmanifest.TaskPayload, taskID string) error {
	source, err := s.repository.GetSourceByID(ctx, payload.SourceID)
	if err != nil || !source.AutoDeploy {
		return err
	}
	runs, err := s.repository.ListSyncRuns(ctx, payload.PackageID, 100)
	if err != nil {
		return err
	}
	revision := 0
	for _, run := range runs {
		if run.ExecutionTaskID == taskID && run.Status == domainmanifest.SyncRunSucceeded {
			revision = run.Revision
			break
		}
	}
	if revision < 1 {
		return nil
	}
	item, err := s.base.get(ctx, payload.PackageID)
	if err != nil {
		return err
	}
	bindings, err := s.repository.ListBindings(ctx, item.ID)
	if err != nil {
		return err
	}
	for _, binding := range bindings {
		if !binding.Enabled {
			continue
		}
		expectedGeneration := int64(0)
		deployment, getErr := s.repository.GetDeploymentByBinding(ctx, binding.ID)
		if getErr == nil {
			expectedGeneration = deployment.Generation
		} else if !errors.Is(getErr, apperrors.ErrNotFound) {
			return getErr
		} else {
			now := time.Now().UTC()
			deployment = domainmanifest.Deployment{
				ID: uuid.NewString(), PackageID: item.ID, BindingID: binding.ID,
				Status:    domainmanifest.DeploymentStatus{Phase: domainmanifest.DeploymentPhasePending, Conditions: []domainmanifest.Condition{}, Inventory: []domainmanifest.ResourceInventory{}},
				CreatedAt: now, UpdatedAt: now,
			}
		}
		files, resolvedRevision, err := s.filesForRevision(ctx, item, revision)
		if err != nil {
			return err
		}
		rendered, err := s.renderer.Render(ctx, item, binding, files, resolvedRevision)
		if err != nil {
			return err
		}
		deployment.Spec = domainmanifest.DeploymentSpec{
			DesiredRevision: resolvedRevision, DesiredDigest: rendered.RenderedDigest,
			ReconcilePolicy: domainmanifest.ReconcilePolicyContinuous,
			DriftPolicy:     binding.DriftPolicy, DeletionPolicy: binding.DeletionPolicy,
		}
		deployment.UpdatedAt = time.Now().UTC()
		deployment, err = s.repository.SetDesiredRevision(ctx, deployment, expectedGeneration)
		if err != nil {
			return err
		}
		applyPayload := s.taskPayload(domainmanifest.TaskActionApply, item, binding, deployment, rendered, deployment.Generation, false, "system:manifest-sync")
		applyPayload.IdempotencyKey = operationKey(deployment, domainmanifest.TaskActionApply)
		if _, err := s.queueOperation(ctx, item, binding, deployment, applyPayload); err != nil {
			return err
		}
	}
	return nil
}
