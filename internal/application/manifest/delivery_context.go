package manifest

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type deliveryContextReader interface {
	CurrentManifestDeliveryContext(context.Context, domainidentity.Principal, string, string, string) (domainmanifest.DeliveryContext, error)
}

type operationTaskReader interface {
	GetOperationTask(context.Context, string) (domainmanifest.OperationRun, error)
}

type legacyDeliveryAdmission interface {
	ValidateLegacyManifestDelivery(context.Context, domainmanifest.TaskPayload) error
}

func (s *DeclarativeService) currentDeliveryContext(ctx context.Context, principal domainidentity.Principal, applicationID, environmentID, targetID string) (domainmanifest.DeliveryContext, error) {
	reader, ok := s.delivery.(deliveryContextReader)
	if !ok || reflect.ValueOf(reader).Kind() == reflect.Pointer && reflect.ValueOf(reader).IsNil() {
		return domainmanifest.DeliveryContext{}, fmt.Errorf("%w: publication control reader is unavailable", apperrors.ErrUnsupportedOperation)
	}
	return reader.CurrentManifestDeliveryContext(ctx, principal, applicationID, environmentID, targetID)
}

func (s *DeclarativeService) freezeDeliveryContext(ctx context.Context, principal domainidentity.Principal, applicationID, environmentID, planID string, target domaincatalog.ReleaseTarget) (*domainmanifest.DeliveryContext, error) {
	current, err := s.currentDeliveryContext(ctx, principal, applicationID, environmentID, target.ID)
	if err != nil {
		return nil, err
	}
	expected, err := target.ConfigurationRevision()
	if err != nil || current.TargetID != target.ID || current.TargetRevision != expected {
		return nil, fmt.Errorf("%w: publication target changed during planning", apperrors.ErrConflict)
	}
	current.PlanID = planID
	if err := current.Validate(); err != nil {
		return nil, fmt.Errorf("%w: publication identity is incomplete", apperrors.ErrConflict)
	}
	return &current, nil
}

func (s *DeclarativeService) operationTask(ctx context.Context, taskID string) (domainmanifest.OperationRun, error) {
	reader, ok := s.repository.(operationTaskReader)
	if !ok || reflect.ValueOf(reader).Kind() == reflect.Pointer && reflect.ValueOf(reader).IsNil() {
		return domainmanifest.OperationRun{}, apperrors.ErrUnsupportedOperation
	}
	return reader.GetOperationTask(ctx, taskID)
}

func (s *DeclarativeService) snapshotDeliveryContext(ctx context.Context, snapshot domainmanifest.DeliverySnapshot) (*domainmanifest.DeliveryContext, error) {
	operation, err := s.operationTask(ctx, snapshot.PreflightTaskID)
	if err != nil {
		return nil, err
	}
	control := operation.DeliveryContext
	if control == nil || control.Validate() != nil || control.PlanID != snapshot.DeliveryPlanID || control.TargetID != snapshot.TargetID || operation.ExecutionTaskID != snapshot.PreflightTaskID || operation.PackageID != snapshot.PackageID || operation.BindingID != snapshot.BindingID || operation.Action != domainmanifest.TaskActionPreflight {
		return nil, fmt.Errorf("%w: create a new Plan with a current publication preflight", apperrors.ErrConflict)
	}
	copy := *control
	return &copy, nil
}

// ValidateDeliveryControl needs read access only. Approval never grants the
// approver the publisher's deployment permission.
func (s *DeclarativeService) ValidateDeliveryControl(ctx context.Context, principal domainidentity.Principal, snapshot domainmanifest.DeliverySnapshot) error {
	control, err := s.snapshotDeliveryContext(ctx, snapshot)
	if err != nil {
		return err
	}
	item, err := s.base.get(ctx, snapshot.PackageID)
	if err != nil {
		return err
	}
	return s.validateCurrentDeliveryContext(ctx, principal, item.ApplicationID, snapshot.ApplicationEnvironmentID, *control)
}

func (s *DeclarativeService) validateCurrentDeliveryContext(ctx context.Context, principal domainidentity.Principal, applicationID, environmentID string, expected domainmanifest.DeliveryContext) error {
	current, err := s.currentDeliveryContext(ctx, principal, applicationID, environmentID, expected.TargetID)
	if err != nil {
		return err
	}
	current.PlanID = expected.PlanID
	if current != expected {
		return fmt.Errorf("%w: target or publication policy changed; create a new Plan", apperrors.ErrConflict)
	}
	return nil
}

func (s *DeclarativeService) prepareOperationContext(ctx context.Context, deployment domainmanifest.Deployment, payload *domainmanifest.TaskPayload) error {
	if payload.DeliveryContext == nil && deployment.Spec.DeliverySnapshot != nil {
		control, err := s.snapshotDeliveryContext(ctx, *deployment.Spec.DeliverySnapshot)
		if err != nil {
			return err
		}
		payload.DeliveryContext = control
	}
	if payload.DeliveryContext == nil {
		if validator, ok := s.importedRoots.(legacyDeliveryAdmission); ok {
			return validator.ValidateLegacyManifestDelivery(ctx, *payload)
		}
	}
	return nil
}

func (s *DeclarativeService) validateManifestTaskAdmission(ctx context.Context, task domaindelivery.ExecutionTask, payload domainmanifest.TaskPayload) (domaindelivery.ExecutionTask, error) {
	if payload.Action == domainmanifest.TaskActionSync {
		if domaindelivery.RequiresManifestAdmission(task) {
			return task, apperrors.ErrConflict
		}
		return task, nil
	}
	operation, err := s.operationTask(ctx, task.ID)
	if err != nil && !errors.Is(err, apperrors.ErrNotFound) && !errors.Is(err, apperrors.ErrUnsupportedOperation) {
		return task, err
	}
	if operation.DeliveryContext == nil {
		if domaindelivery.RequiresManifestAdmission(task) {
			return task, fmt.Errorf("%w: protected Manifest task has no durable publication context", apperrors.ErrConflict)
		}
		return task, s.validateLegacyManifestTask(ctx, task, payload)
	}
	if err != nil || !operationMatchesTask(operation, task, payload) {
		return task, fmt.Errorf("%w: task differs from its durable publication operation", apperrors.ErrConflict)
	}
	principal, err := s.currentPublicationPrincipal(ctx, task)
	if err != nil {
		return task, err
	}
	task.SecretPrincipal = principal
	payload.DeliveryContext = operation.DeliveryContext
	if err := s.authorizePublicationTask(ctx, principal, task, payload); err != nil {
		return task, err
	}
	if err := s.validateCurrentDeliveryContext(ctx, principal, task.ApplicationID, task.ApplicationEnvironmentID, *operation.DeliveryContext); err != nil {
		return task, err
	}
	impact, ok := s.delivery.(interface {
		ValidateDeliveryPlanServiceReferenceImpact(context.Context, domainidentity.Principal, string) error
	})
	if !ok {
		return task, apperrors.ErrUnsupportedOperation
	}
	if err := impact.ValidateDeliveryPlanServiceReferenceImpact(ctx, principal, operation.DeliveryContext.PlanID); err != nil {
		return task, err
	}
	if payload.Action != domainmanifest.TaskActionPreflight {
		err = s.validatePlannedManifestOperation(ctx, principal, task, payload, *operation.DeliveryContext)
	}
	return task, err
}

func (s *DeclarativeService) validateLegacyManifestTask(ctx context.Context, task domaindelivery.ExecutionTask, payload domainmanifest.TaskPayload) error {
	// Older root observations already carry an explicit target/UID and keep
	// the existing current-principal read guard. They grant no publication.
	if payload.RootResource != nil && payload.Action == domainmanifest.TaskActionObserve && !strings.HasPrefix(payload.FieldManager, "opensoha-manifest/") {
		return nil
	}
	if strings.HasPrefix(payload.FieldManager, "opensoha-manifest/") || payload.RootResource != nil {
		principal, principalErr := s.currentPublicationPrincipal(ctx, task)
		if principalErr != nil {
			return principalErr
		}
		if permissionErr := s.base.authorize(ctx, principal, appaccess.ManagedActionPermission(appaccess.PermDeliveryManifestDeploymentsManage, "trigger")); permissionErr != nil {
			return permissionErr
		}
		return fmt.Errorf("%w: legacy Plan task requires a new publication preflight", apperrors.ErrConflict)
	}
	if validator, ok := s.importedRoots.(legacyDeliveryAdmission); ok {
		return validator.ValidateLegacyManifestDelivery(ctx, payload)
	}
	return nil
}

func operationMatchesTask(operation domainmanifest.OperationRun, task domaindelivery.ExecutionTask, payload domainmanifest.TaskPayload) bool {
	return operation.DeliveryContext.Validate() == nil && operation.ExecutionTaskID == task.ID && operation.PackageID == payload.PackageID && operation.BindingID == payload.BindingID && operation.DeploymentID == payload.DeploymentID && operation.Generation == payload.Generation && operation.Action == payload.Action && operation.IdempotencyKey == payload.IdempotencyKey && task.TaskKind == taskKind(payload.Action) && !payload.ForceConflicts && payload.FieldManager == "opensoha-manifest/"+payload.BindingID
}

func (s *DeclarativeService) currentPublicationPrincipal(ctx context.Context, task domaindelivery.ExecutionTask) (domainidentity.Principal, error) {
	if s.executionPrincipals == nil || task.SecretPrincipal.UserID == "" {
		return domainidentity.Principal{}, apperrors.ErrUnauthorized
	}
	principal, err := s.executionPrincipals.CurrentExecutionPrincipal(ctx, task.SecretPrincipal.UserID, task.SecretPrincipal.AccessTokenID)
	if err != nil {
		return principal, err
	}
	if principal.UserID != task.SecretPrincipal.UserID {
		return domainidentity.Principal{}, apperrors.ErrUnauthorized
	}
	return principal, nil
}

func (s *DeclarativeService) authorizePublicationTask(ctx context.Context, principal domainidentity.Principal, task domaindelivery.ExecutionTask, payload domainmanifest.TaskPayload) error {
	permission, action := appaccess.ManagedActionPermission(appaccess.PermDeliveryManifestDeploymentsManage, "trigger"), domainaccess.ActionTrigger
	switch payload.Action {
	case domainmanifest.TaskActionPreflight:
		permission = appaccess.ManagedActionPermission(appaccess.PermDeliveryManifestDeploymentsManage, "preflight")
	case domainmanifest.TaskActionObserve:
		permission, action = appaccess.PermDeliveryApplicationsView, domainaccess.ActionView
	case domainmanifest.TaskActionApply, domainmanifest.TaskActionRolloutControl:
	default:
		return apperrors.ErrUnsupportedOperation
	}
	if err := s.base.authorize(ctx, principal, permission); err != nil {
		return err
	}
	item, app, err := s.bindingPackage(ctx, principal, payload.PackageID, action, permission)
	if err != nil {
		return err
	}
	binding, err := s.repository.GetBinding(ctx, payload.BindingID)
	if err != nil {
		return err
	}
	if item.ApplicationID != task.ApplicationID || !binding.Enabled || binding.PackageID != item.ID || binding.ApplicationEnvironmentID != task.ApplicationEnvironmentID || binding.ClusterID != payload.ClusterID || binding.Namespace != payload.Namespace {
		return apperrors.ErrConflict
	}
	if err := s.validateBinding(ctx, principal, action, item, app, &binding, permission); err != nil {
		return err
	}
	provider, err := s.operationProvider(ctx, binding, payload)
	if err != nil {
		return err
	}
	if provider != task.ProviderKind {
		return fmt.Errorf("%w: Manifest runtime routing changed", apperrors.ErrConflict)
	}
	return nil
}

func (s *DeclarativeService) validatePlannedManifestOperation(ctx context.Context, principal domainidentity.Principal, task domaindelivery.ExecutionTask, payload domainmanifest.TaskPayload, control domainmanifest.DeliveryContext) error {
	if s.delivery == nil {
		return apperrors.ErrUnsupportedOperation
	}
	var plan domaindelivery.DeliveryPlan
	var err error
	if payload.Action == domainmanifest.TaskActionObserve {
		plan, err = s.delivery.GetConfirmedDeliveryPlan(ctx, principal, control.PlanID)
	} else {
		plan, err = s.delivery.GetExecutingDeliveryPlan(ctx, principal, control.PlanID)
	}
	if err != nil {
		return err
	}
	if plan.ApplicationID != task.ApplicationID || plan.ApplicationEnvironmentID != task.ApplicationEnvironmentID {
		return apperrors.ErrConflict
	}
	for _, snapshot := range plan.ManifestSnapshots {
		if snapshot.TargetID == control.TargetID && snapshot.DeliveryPlanID == control.PlanID && plannedTaskMatchesSnapshot(payload, snapshot) {
			frozen, err := s.snapshotDeliveryContext(ctx, snapshot)
			if err != nil {
				return err
			}
			if *frozen != control {
				return apperrors.ErrConflict
			}
			return s.validatePlannedTaskSelection(ctx, snapshot, payload)
		}
	}
	return fmt.Errorf("%w: task is not part of the approved Manifest plan", apperrors.ErrConflict)
}

func plannedTaskMatchesSnapshot(payload domainmanifest.TaskPayload, snapshot domainmanifest.DeliverySnapshot) bool {
	return payload.PackageID == snapshot.PackageID && payload.BindingID == snapshot.BindingID && payload.ClusterID == snapshot.ClusterID && payload.Namespace == snapshot.Namespace && payload.Revision == snapshot.Revision && payload.RenderedDigest == snapshot.RenderedDigest && reflect.DeepEqual(payload.Documents, snapshot.Documents) && reflect.DeepEqual(payload.GitOpsDocuments, snapshot.GitOpsDocuments) && reflect.DeepEqual(payload.RootResource, snapshot.RootResource)
}

func (s *DeclarativeService) validatePlannedTaskSelection(ctx context.Context, snapshot domainmanifest.DeliverySnapshot, payload domainmanifest.TaskPayload) error {
	item, err := s.base.get(ctx, snapshot.PackageID)
	if err != nil {
		return err
	}
	binding, err := s.repository.GetBinding(ctx, snapshot.BindingID)
	if err != nil {
		return err
	}
	if !deliveryBindingMatchesSnapshot(item, binding, snapshot) {
		return apperrors.ErrConflict
	}
	current, err := s.repository.GetDeployment(ctx, payload.DeploymentID)
	if err != nil {
		return err
	}
	if current.Generation != payload.Generation || payload.Generation != snapshot.ExpectedGeneration+1 || !reflect.DeepEqual(current.Spec.DeliverySnapshot, &snapshot) {
		return fmt.Errorf("%w: selected Manifest plan changed", apperrors.ErrConflict)
	}
	return s.validateDeliveryPreflight(ctx, item.ApplicationID, snapshot)
}
