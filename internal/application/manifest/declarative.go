package manifest

import (
	"context"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type DeclarativeRepository interface {
	GetSource(context.Context, string) (domainmanifest.Source, error)
	UpdateSource(context.Context, string, domainmanifest.SourceInput) (domainmanifest.Source, error)
	ListBindings(context.Context, string) ([]domainmanifest.EnvironmentBinding, error)
	GetBinding(context.Context, string) (domainmanifest.EnvironmentBinding, error)
	CreateBinding(context.Context, domainmanifest.EnvironmentBinding) (domainmanifest.EnvironmentBinding, error)
	UpdateBinding(context.Context, domainmanifest.EnvironmentBinding, int64) (domainmanifest.EnvironmentBinding, error)
	DeleteBinding(context.Context, string) error
	ListDeployments(context.Context, domainmanifest.DeploymentFilter) (domainmanifest.DeploymentPage, error)
	GetDeployment(context.Context, string) (domainmanifest.Deployment, error)
	GetDeploymentByBinding(context.Context, string) (domainmanifest.Deployment, error)
	SetDesiredRevision(context.Context, domainmanifest.Deployment, int64) (domainmanifest.Deployment, error)
	CreateOperationTask(context.Context, domainmanifest.OperationRun, domaindelivery.ExecutionTask) (string, bool, error)
	UpdateDeploymentStatus(context.Context, string, int64, domainmanifest.DeploymentStatus) error
	GetSourceByID(context.Context, string) (domainmanifest.Source, error)
	CreateSyncTask(context.Context, domainmanifest.SyncRun, domaindelivery.ExecutionTask) (string, bool, error)
	ListSyncRuns(context.Context, string, int) ([]domainmanifest.SyncRun, error)
	ListDueSources(context.Context, int) ([]domainmanifest.Source, error)
	UpdateSyncTask(context.Context, domaindelivery.ExecutionTask, domainmanifest.TaskPayload, domainmanifest.TaskResult) error
	ListContinuousDeployments(context.Context, int) ([]domainmanifest.Deployment, error)
	ApplyAdoptedFiles(context.Context, string, int64, []domainmanifest.File, string) error
	CreateDeliveryIntent(context.Context, domainmanifest.DeliveryIntent) (domainmanifest.DeliveryIntent, error)
	ListDeliveryIntents(context.Context, string) ([]domainmanifest.DeliveryIntent, error)
	GetDeliveryIntent(context.Context, string) (domainmanifest.DeliveryIntent, error)
	DecideDeliveryIntent(context.Context, domainmanifest.DeliveryIntent, domainmanifest.DeliveryIntentDecisionInput) (domainmanifest.DeliveryIntent, error)
}

type SourceRepositoryReader interface {
	GetRepository(context.Context, string) (domainapp.SourceRepository, error)
}

type ManifestRenderer interface {
	Render(context.Context, domainmanifest.Package, domainmanifest.EnvironmentBinding, []domainmanifest.File, int) (domainmanifest.RenderResult, error)
}

type ManifestRuntime interface {
	Execute(context.Context, domainmanifest.TaskPayload) (domainmanifest.TaskResult, error)
}

type ManifestTaskRuntime interface {
	ClaimExecutionTask(context.Context, []string, string, string) (domaindelivery.ExecutionTask, error)
	RecordCallback(context.Context, domaindelivery.ExecutionCallbackInput) (domaindelivery.ExecutionTask, error)
	GetExecutionTaskInternal(context.Context, string) (domaindelivery.ExecutionTask, error)
}

type DeclarativeRuntimeDependencies struct {
	Renderer ManifestRenderer
	Direct   ManifestRuntime
	Git      ManifestRuntime
	Tasks    ManifestTaskRuntime
	Sources  SourceRepositoryReader
}

type DeclarativeService struct {
	base       *Service
	repository DeclarativeRepository
	renderer   ManifestRenderer
	direct     ManifestRuntime
	git        ManifestRuntime
	tasks      ManifestTaskRuntime
	sources    SourceRepositoryReader
}

func NewDeclarative(base *Service, repository DeclarativeRepository, runtime ...DeclarativeRuntimeDependencies) *DeclarativeService {
	service := &DeclarativeService{base: base, repository: repository}
	if len(runtime) > 0 {
		service.renderer = runtime[0].Renderer
		service.direct = runtime[0].Direct
		service.git = runtime[0].Git
		service.tasks = runtime[0].Tasks
		service.sources = runtime[0].Sources
	}
	return service
}

func (s *DeclarativeService) GetSource(ctx context.Context, principal domainidentity.Principal, packageID string) (domainmanifest.Source, error) {
	if _, err := s.base.Get(ctx, principal, packageID); err != nil {
		return domainmanifest.Source{}, err
	}
	return s.repository.GetSource(ctx, strings.TrimSpace(packageID))
}

func (s *DeclarativeService) UpdateSource(ctx context.Context, principal domainidentity.Principal, packageID string, input domainmanifest.SourceInput) (domainmanifest.Source, error) {
	if err := s.base.authorize(ctx, principal, appaccess.PermDeliveryManifestSourcesManage); err != nil {
		return domainmanifest.Source{}, err
	}
	item, err := s.base.get(ctx, packageID)
	if err != nil {
		return domainmanifest.Source{}, err
	}
	if _, err := s.base.authorizePackage(ctx, principal, domainaccess.ActionUpdate, item); err != nil {
		return domainmanifest.Source{}, err
	}
	existing, err := s.repository.GetSource(ctx, item.ID)
	if err != nil {
		return domainmanifest.Source{}, err
	}
	normalized, err := normalizeSourceInput(input)
	if err != nil {
		return domainmanifest.Source{}, err
	}
	if existing.Mode != normalized.Mode {
		if item.CurrentRevision > 0 {
			return domainmanifest.Source{}, fmt.Errorf("%w: source mode switching is only allowed before the first published revision", apperrors.ErrConflict)
		}
	}
	updated, err := s.repository.UpdateSource(ctx, item.ID, normalized)
	if err != nil {
		return domainmanifest.Source{}, err
	}
	s.base.record(ctx, principal, "delivery.manifest.source.update", item, "updated manifest source configuration")
	return updated, nil
}

func (s *DeclarativeService) ListBindings(ctx context.Context, principal domainidentity.Principal, packageID string) ([]domainmanifest.EnvironmentBinding, error) {
	if _, err := s.base.Get(ctx, principal, packageID); err != nil {
		return nil, err
	}
	return s.repository.ListBindings(ctx, strings.TrimSpace(packageID))
}

func (s *DeclarativeService) CreateBinding(ctx context.Context, principal domainidentity.Principal, packageID string, input domainmanifest.BindingInput) (domainmanifest.EnvironmentBinding, error) {
	if err := s.base.authorize(ctx, principal, appaccess.PermDeliveryManifestDeploymentsManage); err != nil {
		return domainmanifest.EnvironmentBinding{}, err
	}
	item, app, err := s.bindingPackage(ctx, principal, packageID, domainaccess.ActionUpdate)
	if err != nil {
		return domainmanifest.EnvironmentBinding{}, err
	}
	binding, err := normalizeBindingInput(input)
	if err != nil {
		return domainmanifest.EnvironmentBinding{}, err
	}
	if err := s.validateBinding(ctx, principal, domainaccess.ActionUpdate, item, app, &binding); err != nil {
		return domainmanifest.EnvironmentBinding{}, err
	}
	now := time.Now().UTC()
	binding.ID = uuid.NewString()
	binding.PackageID = item.ID
	binding.Version = 1
	binding.CreatedAt = now
	binding.UpdatedAt = now
	created, err := s.repository.CreateBinding(ctx, binding)
	if err != nil {
		return domainmanifest.EnvironmentBinding{}, err
	}
	s.base.record(ctx, principal, "delivery.manifest.binding.create", item, "created manifest environment binding")
	return created, nil
}

func (s *DeclarativeService) UpdateBinding(ctx context.Context, principal domainidentity.Principal, bindingID string, input domainmanifest.BindingUpdateInput) (domainmanifest.EnvironmentBinding, error) {
	if err := s.base.authorize(ctx, principal, appaccess.PermDeliveryManifestDeploymentsManage); err != nil {
		return domainmanifest.EnvironmentBinding{}, err
	}
	existing, err := s.repository.GetBinding(ctx, strings.TrimSpace(bindingID))
	if err != nil {
		return domainmanifest.EnvironmentBinding{}, err
	}
	item, app, err := s.bindingPackage(ctx, principal, existing.PackageID, domainaccess.ActionUpdate)
	if err != nil {
		return domainmanifest.EnvironmentBinding{}, err
	}
	binding, err := normalizeBindingInput(input.BindingInput)
	if err != nil {
		return domainmanifest.EnvironmentBinding{}, err
	}
	if input.ExpectedVersion < 1 {
		return domainmanifest.EnvironmentBinding{}, fmt.Errorf("%w: expectedVersion must be positive", apperrors.ErrInvalidArgument)
	}
	if err := s.validateBinding(ctx, principal, domainaccess.ActionUpdate, item, app, &binding); err != nil {
		return domainmanifest.EnvironmentBinding{}, err
	}
	binding.ID = existing.ID
	binding.PackageID = existing.PackageID
	binding.Version = existing.Version
	binding.CreatedAt = existing.CreatedAt
	binding.UpdatedAt = time.Now().UTC()
	updated, err := s.repository.UpdateBinding(ctx, binding, input.ExpectedVersion)
	if err != nil {
		return domainmanifest.EnvironmentBinding{}, err
	}
	s.base.record(ctx, principal, "delivery.manifest.binding.update", item, "updated manifest environment binding")
	return updated, nil
}

func (s *DeclarativeService) DeleteBinding(ctx context.Context, principal domainidentity.Principal, bindingID string) error {
	if err := s.base.authorize(ctx, principal, appaccess.PermDeliveryManifestDeploymentsManage); err != nil {
		return err
	}
	existing, err := s.repository.GetBinding(ctx, strings.TrimSpace(bindingID))
	if err != nil {
		return err
	}
	item, _, err := s.bindingPackage(ctx, principal, existing.PackageID, domainaccess.ActionDelete)
	if err != nil {
		return err
	}
	if err := s.repository.DeleteBinding(ctx, existing.ID); err != nil {
		return err
	}
	s.base.record(ctx, principal, "delivery.manifest.binding.delete", item, "deleted manifest environment binding")
	return nil
}

func (s *DeclarativeService) ListDeployments(ctx context.Context, principal domainidentity.Principal, filter domainmanifest.DeploymentFilter) (domainmanifest.DeploymentPage, error) {
	if err := s.base.authorize(ctx, principal, appaccess.PermDeliveryApplicationsView); err != nil {
		return domainmanifest.DeploymentPage{}, err
	}
	filter.Page, filter.PageSize = normalizePagination(filter.Page, filter.PageSize, 0)
	cluster, err := s.base.loadCluster(ctx, filter.ClusterID)
	if err != nil {
		return domainmanifest.DeploymentPage{}, err
	}
	applicationIDs, err := s.base.authorizedApplicationIDs(ctx, principal, filter.ApplicationID, cluster, filter.Namespace)
	if err != nil {
		return domainmanifest.DeploymentPage{}, err
	}
	filter.ApplicationIDs = applicationIDs
	return s.repository.ListDeployments(ctx, filter)
}

func (s *DeclarativeService) GetDeployment(ctx context.Context, principal domainidentity.Principal, deploymentID string) (domainmanifest.Deployment, error) {
	if err := s.base.authorize(ctx, principal, appaccess.PermDeliveryApplicationsView); err != nil {
		return domainmanifest.Deployment{}, err
	}
	item, err := s.repository.GetDeployment(ctx, strings.TrimSpace(deploymentID))
	if err != nil {
		return domainmanifest.Deployment{}, err
	}
	if _, err := s.base.Get(ctx, principal, item.PackageID); err != nil {
		return domainmanifest.Deployment{}, err
	}
	return item, nil
}

func (s *DeclarativeService) bindingPackage(ctx context.Context, principal domainidentity.Principal, packageID string, action domainaccess.Action) (domainmanifest.Package, domainapp.App, error) {
	item, err := s.base.get(ctx, packageID)
	if err != nil {
		return domainmanifest.Package{}, domainapp.App{}, err
	}
	app, err := s.base.authorizePackage(ctx, principal, action, item)
	return item, app, err
}

func (s *DeclarativeService) validateBinding(ctx context.Context, principal domainidentity.Principal, action domainaccess.Action, item domainmanifest.Package, app domainapp.App, binding *domainmanifest.EnvironmentBinding) error {
	legacy := domainmanifest.Binding{
		ID: binding.ID, ApplicationEnvironmentID: binding.ApplicationEnvironmentID,
		ClusterID: binding.ClusterID, Namespace: binding.Namespace, Overlay: binding.Overlay,
	}
	item.Bindings = []domainmanifest.Binding{legacy}
	if err := s.base.validateBindings(ctx, principal, action, &item, app); err != nil {
		return err
	}
	binding.EnvironmentKey = item.Bindings[0].EnvironmentKey
	return nil
}

func normalizeSourceInput(input domainmanifest.SourceInput) (domainmanifest.SourceInput, error) {
	input.Mode = strings.TrimSpace(input.Mode)
	input.RepositoryID = strings.TrimSpace(input.RepositoryID)
	input.RefType = strings.TrimSpace(input.RefType)
	input.RefValue = strings.TrimSpace(input.RefValue)
	input.Path = strings.TrimSpace(input.Path)
	input.SyncPolicy = strings.TrimSpace(input.SyncPolicy)
	input.IncludePatterns = normalizeStringList(input.IncludePatterns)
	input.ExcludePatterns = normalizeStringList(input.ExcludePatterns)
	if input.ExpectedGeneration < 1 {
		return domainmanifest.SourceInput{}, fmt.Errorf("%w: expectedGeneration must be positive", apperrors.ErrInvalidArgument)
	}
	switch input.Mode {
	case domainmanifest.SourceModeSohaManaged:
		return validateSohaManagedSource(input)
	case domainmanifest.SourceModeGitSynced:
		return normalizeGitSyncedSource(input)
	default:
		return domainmanifest.SourceInput{}, fmt.Errorf("%w: unsupported manifest source mode", apperrors.ErrInvalidArgument)
	}
}

func validateSohaManagedSource(input domainmanifest.SourceInput) (domainmanifest.SourceInput, error) {
	if input.RepositoryID != "" || input.RefType != "" || input.RefValue != "" || input.Path != "" {
		return domainmanifest.SourceInput{}, fmt.Errorf("%w: Soha-managed sources cannot contain Git fields", apperrors.ErrInvalidArgument)
	}
	if input.SyncPolicy != domainmanifest.SyncPolicyManual || input.PollIntervalSeconds != 0 || input.AutoPublish || input.AutoDeploy {
		return domainmanifest.SourceInput{}, fmt.Errorf("%w: Soha-managed sources require manual sync and autoPublish=false", apperrors.ErrInvalidArgument)
	}
	return input, nil
}

func normalizeGitSyncedSource(input domainmanifest.SourceInput) (domainmanifest.SourceInput, error) {
	if input.RepositoryID == "" || input.RefValue == "" || input.Path == "" {
		return domainmanifest.SourceInput{}, fmt.Errorf("%w: Git-synced sources require repositoryId, refValue, and path", apperrors.ErrInvalidArgument)
	}
	if !slices.Contains([]string{domainmanifest.SourceRefBranch, domainmanifest.SourceRefTag, domainmanifest.SourceRefCommit}, input.RefType) {
		return domainmanifest.SourceInput{}, fmt.Errorf("%w: unsupported Git refType", apperrors.ErrInvalidArgument)
	}
	if !slices.Contains([]string{domainmanifest.SyncPolicyManual, domainmanifest.SyncPolicyWebhook, domainmanifest.SyncPolicyPoll}, input.SyncPolicy) {
		return domainmanifest.SourceInput{}, fmt.Errorf("%w: unsupported manifest syncPolicy", apperrors.ErrInvalidArgument)
	}
	if input.SyncPolicy == domainmanifest.SyncPolicyPoll && input.PollIntervalSeconds < 30 {
		return domainmanifest.SourceInput{}, fmt.Errorf("%w: pollIntervalSeconds must be at least 30", apperrors.ErrInvalidArgument)
	}
	if input.AutoDeploy && !input.AutoPublish {
		return domainmanifest.SourceInput{}, fmt.Errorf("%w: autoDeploy requires autoPublish", apperrors.ErrInvalidArgument)
	}
	if input.SyncPolicy != domainmanifest.SyncPolicyPoll && input.PollIntervalSeconds != 0 {
		return domainmanifest.SourceInput{}, fmt.Errorf("%w: pollIntervalSeconds requires poll syncPolicy", apperrors.ErrInvalidArgument)
	}
	input.Path = path.Clean(input.Path)
	if path.IsAbs(input.Path) || input.Path == ".." || strings.HasPrefix(input.Path, "../") {
		return domainmanifest.SourceInput{}, fmt.Errorf("%w: source path must stay within the repository", apperrors.ErrInvalidArgument)
	}
	return input, nil
}

func normalizeBindingInput(input domainmanifest.BindingInput) (domainmanifest.EnvironmentBinding, error) {
	item := domainmanifest.EnvironmentBinding{
		ApplicationEnvironmentID: strings.TrimSpace(input.ApplicationEnvironmentID),
		ClusterID:                strings.TrimSpace(input.ClusterID), Namespace: strings.TrimSpace(input.Namespace),
		Overlay: input.Overlay, RolloutStrategyID: strings.TrimSpace(input.RolloutStrategyID),
		VerificationPolicyID: strings.TrimSpace(input.VerificationPolicyID),
		DriftPolicy:          strings.TrimSpace(input.DriftPolicy), DeletionPolicy: strings.TrimSpace(input.DeletionPolicy),
		Enabled: input.Enabled,
	}
	if item.ApplicationEnvironmentID == "" || item.ClusterID == "" || item.Namespace == "" {
		return domainmanifest.EnvironmentBinding{}, fmt.Errorf("%w: applicationEnvironmentId, clusterId, and namespace are required", apperrors.ErrInvalidArgument)
	}
	if !slices.Contains([]string{domainmanifest.DriftPolicyReport, domainmanifest.DriftPolicyRepair, domainmanifest.DriftPolicyAdopt}, item.DriftPolicy) {
		return domainmanifest.EnvironmentBinding{}, fmt.Errorf("%w: unsupported driftPolicy", apperrors.ErrInvalidArgument)
	}
	if !slices.Contains([]string{domainmanifest.DeletionPolicyOrphan, domainmanifest.DeletionPolicyDeleteManaged}, item.DeletionPolicy) {
		return domainmanifest.EnvironmentBinding{}, fmt.Errorf("%w: unsupported deletionPolicy", apperrors.ErrInvalidArgument)
	}
	if item.Overlay == nil {
		item.Overlay = map[string]string{}
	}
	return item, nil
}

func normalizeStringList(values []string) []string {
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
