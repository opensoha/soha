package manifest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *DeclarativeService) CreateDeliverySnapshot(ctx context.Context, principal domainidentity.Principal, applicationID, environmentID string, target domaincatalog.ReleaseTarget, revision int, planID string, artifacts domainmanifest.DeliveryArtifacts) (domainmanifest.DeliverySnapshot, error) {
	if err := s.base.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermDeliveryManifestDeploymentsManage, "preflight")); err != nil {
		return domainmanifest.DeliverySnapshot{}, err
	}
	binding, err := s.repository.GetBinding(ctx, strings.TrimSpace(target.ConfigRef))
	if err != nil {
		return domainmanifest.DeliverySnapshot{}, err
	}
	item, app, err := s.bindingPackage(ctx, principal, binding.PackageID, domainaccess.ActionTrigger)
	if err != nil {
		return domainmanifest.DeliverySnapshot{}, err
	}
	if !binding.Enabled || binding.Namespace == "" || !deliveryBindingMatchesTarget(item, binding, applicationID, environmentID, target) {
		return domainmanifest.DeliverySnapshot{}, fmt.Errorf("%w: Manifest binding must belong to the selected application, environment and namespace", apperrors.ErrInvalidArgument)
	}
	if err := s.validateBinding(ctx, principal, domainaccess.ActionTrigger, item, app, &binding); err != nil {
		return domainmanifest.DeliverySnapshot{}, err
	}
	if err := s.validateDeliverySource(ctx, item.ID); err != nil {
		return domainmanifest.DeliverySnapshot{}, err
	}
	if revision == 0 {
		revision = item.CurrentRevision
	}
	published, err := s.deliveryRevision(ctx, item.ID, revision)
	if err != nil {
		return domainmanifest.DeliverySnapshot{}, err
	}
	files, templateInputs, err := s.deliveryTemplateFiles(ctx, principal, item, binding, published.Files, artifacts)
	if err != nil {
		return domainmanifest.DeliverySnapshot{}, err
	}
	rendered, err := s.renderer.Render(ctx, item, binding, files, revision)
	if err != nil {
		return domainmanifest.DeliverySnapshot{}, err
	}
	if err := validateDeliveryDocuments(rendered.Documents, binding.Namespace); err != nil {
		return domainmanifest.DeliverySnapshot{}, err
	}
	gitOpsDocuments, err := s.freezeGitOpsDocuments(ctx, app, binding, rendered.Documents)
	if err != nil {
		return domainmanifest.DeliverySnapshot{}, err
	}
	current, err := s.deliveryDeployment(ctx, binding.ID)
	if err != nil {
		return domainmanifest.DeliverySnapshot{}, err
	}
	commit, err := s.repository.GetRevisionSourceCommit(ctx, item.ID, revision)
	if err != nil {
		return domainmanifest.DeliverySnapshot{}, err
	}
	snapshot := domainmanifest.DeliverySnapshot{
		GitOpsDocuments: gitOpsDocuments,
		TemplateInputs:  templateInputs,
		DeliveryPlanID:  planID, TargetID: target.ID, PackageID: item.ID, ServiceID: item.ServiceID,
		BindingID: binding.ID, BindingVersion: binding.Version, ApplicationEnvironmentID: environmentID,
		ClusterID: binding.ClusterID, Namespace: binding.Namespace, Revision: revision, RevisionDigest: published.Digest,
		SourceCommit: commit, PackageUpdatedAt: item.UpdatedAt, RendererVersion: rendered.RendererVersion,
		InputDigest: rendered.InputDigest, RenderedDigest: rendered.RenderedDigest, Documents: rendered.Documents,
		ExpectedGeneration: current.Generation,
	}
	payload := s.taskPayload(domainmanifest.TaskActionPreflight, item, binding, domainmanifest.Deployment{}, rendered, max(int64(1), current.Generation), false, principal.UserID)
	payload.FieldManager = "opensoha-manifest/" + binding.ID
	payload.GitOpsDocuments = gitOpsDocuments
	payload.IdempotencyKey = "delivery-plan:" + planID + ":" + target.ID + ":preflight"
	task, err := s.queueOperation(ctx, item, binding, domainmanifest.Deployment{}, payload)
	if err != nil {
		return domainmanifest.DeliverySnapshot{}, err
	}
	snapshot.PreflightTaskID = task.ID
	return snapshot, nil
}

func (s *DeclarativeService) validateDeliverySource(ctx context.Context, packageID string) error {
	source, err := s.repository.GetSource(ctx, packageID)
	if err != nil {
		return err
	}
	if source.AutoDeploy {
		return fmt.Errorf("%w: disable automatic source deployment before using application delivery plans", apperrors.ErrConflict)
	}
	if s.renderer == nil || s.tasks == nil {
		return fmt.Errorf("%w: Manifest runtime is unavailable", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validateDeliveryDocuments(documents []domainmanifest.RenderedDocument, namespace string) error {
	if len(documents) == 0 || len(documents) > 50 {
		return fmt.Errorf("%w: delivery requires between 1 and 50 Manifest resources", apperrors.ErrInvalidArgument)
	}
	for _, document := range documents {
		if document.Namespace == "" || document.Namespace != namespace {
			return fmt.Errorf("%w: %s %s is outside the application namespace; install platform resources separately", apperrors.ErrAccessDenied, document.Kind, document.Name)
		}
	}
	return nil
}

func (s *DeclarativeService) deliveryRevision(ctx context.Context, packageID string, version int) (domainmanifest.Revision, error) {
	if version < 1 {
		return domainmanifest.Revision{}, fmt.Errorf("%w: publish a Manifest revision before creating a delivery plan", apperrors.ErrInvalidArgument)
	}
	revisions, err := s.base.repository.ListRevisions(ctx, packageID)
	if err != nil {
		return domainmanifest.Revision{}, err
	}
	for _, revision := range revisions {
		if revision.Version == version {
			return revision, nil
		}
	}
	return domainmanifest.Revision{}, fmt.Errorf("%w: Manifest revision does not exist", apperrors.ErrNotFound)
}

func (s *DeclarativeService) deliveryDeployment(ctx context.Context, bindingID string) (domainmanifest.Deployment, error) {
	current, err := s.repository.GetDeploymentByBinding(ctx, bindingID)
	if errors.Is(err, apperrors.ErrNotFound) {
		return domainmanifest.Deployment{}, nil
	}
	return current, err
}

func (s *DeclarativeService) DeliveryDeployment(ctx context.Context, principal domainidentity.Principal, applicationID, environmentID string, target domaincatalog.ReleaseTarget) (domainmanifest.Deployment, error) {
	if err := s.base.authorize(ctx, principal, appaccess.PermDeliveryApplicationsView); err != nil {
		return domainmanifest.Deployment{}, err
	}
	binding, err := s.repository.GetBinding(ctx, target.ConfigRef)
	if err != nil {
		return domainmanifest.Deployment{}, err
	}
	item, app, err := s.bindingPackage(ctx, principal, binding.PackageID, domainaccess.ActionView)
	if err != nil {
		return domainmanifest.Deployment{}, err
	}
	if !deliveryBindingMatchesTarget(item, binding, applicationID, environmentID, target) {
		return domainmanifest.Deployment{}, apperrors.ErrAccessDenied
	}
	if err := s.validateBinding(ctx, principal, domainaccess.ActionView, item, app, &binding); err != nil {
		return domainmanifest.Deployment{}, err
	}
	return s.repository.GetDeploymentByBinding(ctx, binding.ID)
}

func deliveryBindingMatchesTarget(item domainmanifest.Package, binding domainmanifest.EnvironmentBinding, applicationID, environmentID string, target domaincatalog.ReleaseTarget) bool {
	return item.ApplicationID == applicationID && binding.ApplicationEnvironmentID == environmentID && binding.ClusterID == target.ClusterID && binding.Namespace == target.Namespace
}

func (s *DeclarativeService) ValidateDeliverySnapshot(ctx context.Context, principal domainidentity.Principal, snapshot domainmanifest.DeliverySnapshot) error {
	if err := s.base.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermDeliveryManifestDeploymentsManage, "trigger")); err != nil {
		return err
	}
	item, app, err := s.bindingPackage(ctx, principal, snapshot.PackageID, domainaccess.ActionTrigger)
	if err != nil {
		return err
	}
	binding, err := s.repository.GetBinding(ctx, snapshot.BindingID)
	if err != nil {
		return err
	}
	if !deliveryBindingMatchesSnapshot(item, binding, snapshot) {
		return fmt.Errorf("%w: Manifest package or environment changed; create a new delivery plan", apperrors.ErrConflict)
	}
	if err := s.validateBinding(ctx, principal, domainaccess.ActionTrigger, item, app, &binding); err != nil {
		return err
	}
	published, err := s.deliveryRevision(ctx, item.ID, snapshot.Revision)
	if err != nil {
		return err
	}
	files, err := s.validateDeliveryTemplateInputs(ctx, principal, item, binding, published.Files, snapshot)
	if err != nil {
		return err
	}
	inputDigest, err := domainmanifest.RenderInputDigest(item.Renderer, binding, files)
	if err != nil {
		return err
	}
	if inputDigest != snapshot.InputDigest || published.Digest != snapshot.RevisionDigest {
		return fmt.Errorf("%w: Manifest inputs changed; create a new delivery plan", apperrors.ErrConflict)
	}
	if err := s.validateGitOpsSnapshot(ctx, app, snapshot); err != nil {
		return err
	}
	current, err := s.deliveryDeployment(ctx, binding.ID)
	if err != nil {
		return err
	}
	if current.Generation != snapshot.ExpectedGeneration && !deliveryAlreadySelected(current, snapshot) {
		return fmt.Errorf("%w: Manifest deployment changed; create a new delivery plan", apperrors.ErrConflict)
	}
	return s.validateDeliveryPreflight(ctx, item.ApplicationID, snapshot)
}

func deliveryBindingMatchesSnapshot(item domainmanifest.Package, binding domainmanifest.EnvironmentBinding, snapshot domainmanifest.DeliverySnapshot) bool {
	return binding.Enabled && binding.PackageID == item.ID && binding.Version == snapshot.BindingVersion && binding.ApplicationEnvironmentID == snapshot.ApplicationEnvironmentID && binding.ClusterID == snapshot.ClusterID && binding.Namespace == snapshot.Namespace && item.UpdatedAt.Equal(snapshot.PackageUpdatedAt)
}

func (s *DeclarativeService) validateDeliveryPreflight(ctx context.Context, applicationID string, snapshot domainmanifest.DeliverySnapshot) error {
	if s.tasks == nil {
		return fmt.Errorf("%w: Manifest runtime is unavailable", apperrors.ErrInvalidArgument)
	}
	task, err := s.tasks.GetExecutionTaskInternal(ctx, snapshot.PreflightTaskID)
	if err != nil {
		return err
	}
	payload, err := decodeTaskPayload(task.Payload)
	if err != nil {
		return err
	}
	result := decodeTaskResult(task.Result)
	if task.Status != "completed" || result.Preflight == nil || !result.Preflight.Ready || result.Preflight.RenderedDigest != snapshot.RenderedDigest {
		return fmt.Errorf("%w: Manifest preflight has not succeeded; inspect task %s", apperrors.ErrConflict, task.ID)
	}
	if task.ApplicationID != applicationID || task.ApplicationEnvironmentID != snapshot.ApplicationEnvironmentID || payload.Action != domainmanifest.TaskActionPreflight || payload.ForceConflicts || payload.BindingID != snapshot.BindingID || payload.ClusterID != snapshot.ClusterID || payload.Namespace != snapshot.Namespace || payload.RenderedDigest != snapshot.RenderedDigest || !reflect.DeepEqual(payload.Documents, snapshot.Documents) || !reflect.DeepEqual(payload.GitOpsDocuments, snapshot.GitOpsDocuments) {
		return fmt.Errorf("%w: Manifest preflight does not match the delivery snapshot", apperrors.ErrConflict)
	}
	return validateDeliveryDocuments(snapshot.Documents, snapshot.Namespace)
}

func deliveryAlreadySelected(current domainmanifest.Deployment, snapshot domainmanifest.DeliverySnapshot) bool {
	selected := current.Spec.DeliverySnapshot
	return current.Generation == snapshot.ExpectedGeneration+1 && selected != nil && selected.DeliveryPlanID == snapshot.DeliveryPlanID && selected.TargetID == snapshot.TargetID && selected.InputDigest == snapshot.InputDigest && current.Spec.DesiredDigest == snapshot.RenderedDigest
}

func (s *DeclarativeService) ApplyDeliverySnapshot(ctx context.Context, principal domainidentity.Principal, snapshot domainmanifest.DeliverySnapshot) (domainmanifest.Deployment, domaindelivery.ExecutionTask, error) {
	if err := s.ValidateDeliverySnapshot(ctx, principal, snapshot); err != nil {
		return domainmanifest.Deployment{}, domaindelivery.ExecutionTask{}, err
	}
	item, err := s.base.get(ctx, snapshot.PackageID)
	if err != nil {
		return domainmanifest.Deployment{}, domaindelivery.ExecutionTask{}, err
	}
	binding, err := s.repository.GetBinding(ctx, snapshot.BindingID)
	if err != nil {
		return domainmanifest.Deployment{}, domaindelivery.ExecutionTask{}, err
	}
	deployment, err := s.deliveryDeployment(ctx, binding.ID)
	if err != nil {
		return domainmanifest.Deployment{}, domaindelivery.ExecutionTask{}, err
	}
	if !deliveryAlreadySelected(deployment, snapshot) {
		if deployment.ID == "" {
			deployment = domainmanifest.Deployment{ID: uuid.NewString(), PackageID: item.ID, BindingID: binding.ID, CreatedAt: time.Now().UTC()}
		}
		deployment.Spec = domainmanifest.DeploymentSpec{DesiredRevision: snapshot.Revision, DesiredDigest: snapshot.RenderedDigest, DeliverySnapshot: &snapshot,
			ReconcilePolicy: domainmanifest.ReconcilePolicyContinuous, DriftPolicy: domainmanifest.DriftPolicyReport, DeletionPolicy: domainmanifest.DeletionPolicyOrphan}
		deployment.UpdatedAt = time.Now().UTC()
		deployment, err = s.repository.SetDesiredRevision(ctx, deployment, snapshot.ExpectedGeneration)
		if err != nil {
			return domainmanifest.Deployment{}, domaindelivery.ExecutionTask{}, err
		}
	}
	rendered := domainmanifest.RenderResult{Revision: snapshot.Revision, RenderedDigest: snapshot.RenderedDigest, Documents: snapshot.Documents}
	payload := s.taskPayload(domainmanifest.TaskActionApply, item, binding, deployment, rendered, deployment.Generation, false, principal.UserID)
	payload.IdempotencyKey = operationKey(deployment, domainmanifest.TaskActionApply)
	task, err := s.queueOperation(ctx, item, binding, deployment, payload)
	if err == nil {
		s.base.record(ctx, principal, "delivery.manifest.plan.apply", item, "queued delivery plan "+snapshot.DeliveryPlanID)
	}
	return deployment, task, err
}

func (s *DeclarativeService) requireLegacyDeployment(ctx context.Context, bindingID string) error {
	deployment, err := s.deliveryDeployment(ctx, bindingID)
	if err != nil {
		return err
	}
	if deployment.Spec.DeliverySnapshot != nil {
		return fmt.Errorf("%w: this deployment is governed by application delivery plans", apperrors.ErrConflict)
	}
	return nil
}

func (s *DeclarativeService) renderDeployment(ctx context.Context, item domainmanifest.Package, binding domainmanifest.EnvironmentBinding, deployment domainmanifest.Deployment) (domainmanifest.RenderResult, error) {
	if snapshot := deployment.Spec.DeliverySnapshot; snapshot != nil {
		if binding.ClusterID != snapshot.ClusterID || binding.Namespace != snapshot.Namespace {
			return domainmanifest.RenderResult{}, fmt.Errorf("%w: Manifest deployment scope changed", apperrors.ErrConflict)
		}
		return domainmanifest.RenderResult{PackageID: item.ID, BindingID: binding.ID, Revision: snapshot.Revision, Renderer: item.Renderer,
			RendererVersion: snapshot.RendererVersion, InputDigest: snapshot.InputDigest, RenderedDigest: snapshot.RenderedDigest, Documents: snapshot.Documents}, nil
	}
	files, revision, err := s.filesForRevision(ctx, item, deployment.Spec.DesiredRevision)
	if err != nil {
		return domainmanifest.RenderResult{}, err
	}
	return s.renderer.Render(ctx, item, binding, files, revision)
}
