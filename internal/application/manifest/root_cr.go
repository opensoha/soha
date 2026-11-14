package manifest

import (
	"context"
	"encoding/json"
	"fmt"
	resourceruntime "github.com/opensoha/soha/contracts/resource/runtime"
	"reflect"
	"sigs.k8s.io/yaml"
	"slices"

	"github.com/opensoha/soha/contracts/gen/go/sohaapi"
	contractresource "github.com/opensoha/soha/contracts/resource"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type ExecutionPrincipalReader interface {
	CurrentExecutionPrincipal(context.Context, string, string) (domainidentity.Principal, error)
}

type ImportedRootAdmission interface {
	ValidateImportedManifestTargets(context.Context, domainmanifest.TaskPayload) error
}

type RootResourceReader interface {
	GetImportSource(context.Context, domainidentity.Principal, string, string, string, string, string) (domainresource.ResourceSourceSnapshot, error)
	AuthorizeRootCRUpdate(context.Context, domainidentity.Principal, domainresource.ResourceSourceSnapshot) error
	GetApplicationSourceYAML(context.Context, domainidentity.Principal, domainresource.ResourceSourceSnapshot) (domainresource.ResourceYAMLView, error)
}

func (s *DeclarativeService) freezeRootCR(ctx context.Context, p domainidentity.Principal, target domaincatalog.ReleaseTarget, planID string, documents []domainmanifest.RenderedDocument, appliedRetry ...bool) (*sohaapi.ManifestRootResource, error) {
	if target.WorkloadKind != "CustomResource" {
		return nil, nil
	}
	imported, err := importedRootTarget(target)
	if err != nil {
		return nil, err
	}
	if s.rootResources == nil {
		return nil, apperrors.ErrUnsupportedOperation
	}
	current, err := s.rootResources.GetImportSource(ctx, p, target.ClusterID, target.Namespace, "CustomResource", target.WorkloadName, imported.CRDName)
	if err != nil {
		return nil, err
	}
	if current.Resource.UID != imported.Resource.UID || current.Resource.APIVersion != imported.Resource.APIVersion || current.Resource.Kind != imported.Resource.Kind || current.Generation < 1 {
		return nil, fmt.Errorf("%w: root CR identity changed", apperrors.ErrConflict)
	}
	var expectedGeneration int64
	expected, marshalErr := json.Marshal(target.Metadata["rootCRGeneration"])
	if marshalErr != nil || json.Unmarshal(expected, &expectedGeneration) != nil || expectedGeneration < 1 {
		return nil, apperrors.ErrConflict
	}
	if current.Generation != expectedGeneration && !s.rootRetryAlreadyApplied(ctx, p, current, documents, appliedRetry) {
		return nil, fmt.Errorf("%w: review the current root CR configuration before publishing", apperrors.ErrConflict)
	}
	if current.ExternallyManaged || len(current.Owners) > 0 {
		return nil, fmt.Errorf("%w: update the external authoritative source or owning CR", apperrors.ErrConflict)
	}
	if err = s.rootResources.AuthorizeRootCRUpdate(ctx, p, current); err != nil {
		return nil, err
	}
	root := &sohaapi.ManifestRootResource{APIVersion: current.Resource.APIVersion, Kind: current.Resource.Kind, Namespace: target.Namespace, Name: target.WorkloadName, UID: current.Resource.UID, Generation: expectedGeneration, CrdName: current.CRDName, TargetID: target.ID, DeliveryPlanID: planID}
	if err = validateRootDocuments(root, documents); err != nil {
		return nil, err
	}
	return root, nil
}

func (s *DeclarativeService) rootRetryAlreadyApplied(ctx context.Context, p domainidentity.Principal, current domainresource.ResourceSourceSnapshot, documents []domainmanifest.RenderedDocument, retry []bool) bool {
	if len(retry) == 0 || !retry[0] || len(documents) != 1 {
		return false
	}
	view, err := s.rootResources.GetApplicationSourceYAML(ctx, p, current)
	if err != nil {
		return false
	}
	raw, err := yaml.YAMLToJSON([]byte(view.Content))
	if err != nil {
		return false
	}
	var live, desired struct {
		Spec map[string]any `json:"spec"`
	}
	if json.Unmarshal(raw, &live) != nil || json.Unmarshal([]byte(documents[0].Content), &desired) != nil || live.Spec == nil || desired.Spec == nil {
		return false
	}
	return reflect.DeepEqual(live.Spec, desired.Spec)
}

func validateRootDocuments(root *sohaapi.ManifestRootResource, documents []domainmanifest.RenderedDocument) error {
	if root == nil {
		return nil
	}
	if len(documents) != 1 {
		return fmt.Errorf("%w: root CR publication requires exactly one resource", apperrors.ErrInvalidArgument)
	}
	var object map[string]any
	if err := json.Unmarshal([]byte(documents[0].Content), &object); err != nil {
		return apperrors.ErrInvalidArgument
	}
	if err := contractresource.ValidateRootCRDocument(object, *root); err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	return nil
}

func (s *DeclarativeService) validateRootCRSnapshot(ctx context.Context, p domainidentity.Principal, snapshot domainmanifest.DeliverySnapshot, appliedRetry ...bool) error {
	if snapshot.RootResource == nil && s.rootResources == nil {
		return nil
	}
	environment, err := s.base.environments.GetApplicationEnvironment(ctx, p, snapshot.ApplicationEnvironmentID)
	if err != nil {
		return err
	}
	for _, target := range environment.Targets {
		if target.ID != snapshot.TargetID {
			continue
		}
		if snapshot.RootResource == nil {
			if target.WorkloadKind == "CustomResource" {
				return apperrors.ErrConflict
			}
			return nil
		}
		if !target.Enabled || target.ExecutorKind != "manifest_ssa" || target.ConfigRef != snapshot.BindingID || target.WorkloadKind != "CustomResource" || target.ClusterID != snapshot.ClusterID || target.Namespace != snapshot.Namespace {
			return apperrors.ErrConflict
		}
		current, err := s.freezeRootCR(ctx, p, target, snapshot.DeliveryPlanID, snapshot.Documents, appliedRetry...)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current, snapshot.RootResource) {
			return fmt.Errorf("%w: root CR configuration or publication control changed; create a new plan", apperrors.ErrConflict)
		}
		return nil
	}
	return apperrors.ErrConflict
}

// ValidateRootCRExecutionTask is called by the existing execution hydrator at
// claim time, using the durable original principal and current authorization.
func (s *DeclarativeService) ValidateRootCRExecutionTask(ctx context.Context, task domaindelivery.ExecutionTask) error {
	payload, err := decodeTaskPayload(task.Payload)
	if err != nil {
		return err
	}
	task, err = s.validateManifestTaskAdmission(ctx, task, payload)
	if err != nil {
		return err
	}
	if err = s.validateInitializationTask(ctx, task, payload); err != nil {
		return err
	}
	if err = s.validateMigrationTask(ctx, task, payload); err != nil {
		return err
	}
	if s.importedRoots != nil {
		if err = s.importedRoots.ValidateImportedManifestTargets(ctx, payload); err != nil {
			return err
		}
	}
	if payload.RootResource == nil {
		return nil
	}
	if s.executionPrincipals == nil {
		return apperrors.ErrUnsupportedOperation
	}
	principal, err := s.executionPrincipals.CurrentExecutionPrincipal(ctx, task.SecretPrincipal.UserID, task.SecretPrincipal.AccessTokenID)
	if err != nil {
		return err
	}
	if principal.UserID != task.SecretPrincipal.UserID {
		return apperrors.ErrUnauthorized
	}
	if payload.Action == domainmanifest.TaskActionObserve {
		return s.validateRootCRObservation(ctx, principal, task.ApplicationEnvironmentID, payload)
	}
	permission := appaccess.ManagedActionPermission(appaccess.PermDeliveryManifestDeploymentsManage, "trigger")
	if payload.Action == domainmanifest.TaskActionPreflight {
		permission = appaccess.ManagedActionPermission(appaccess.PermDeliveryManifestDeploymentsManage, "preflight")
	}
	if err = s.base.authorize(ctx, principal, permission); err != nil {
		return err
	}
	snapshot := domainmanifest.DeliverySnapshot{RootResource: payload.RootResource, DeliveryPlanID: payload.RootResource.DeliveryPlanID, TargetID: payload.RootResource.TargetID, BindingID: payload.BindingID, ApplicationEnvironmentID: task.ApplicationEnvironmentID, ClusterID: payload.ClusterID, Namespace: payload.Namespace, Documents: payload.Documents}
	return s.validateRootCRSnapshot(ctx, principal, snapshot, payload.Action == domainmanifest.TaskActionApply && task.AttemptCount > 1)
}

func (s *DeclarativeService) validateRootCRObservation(ctx context.Context, p domainidentity.Principal, environmentID string, payload domainmanifest.TaskPayload) error {
	if _, err := s.base.Get(ctx, p, payload.PackageID); err != nil {
		return err
	}
	if err := s.validateRootObservationBinding(ctx, p, environmentID, payload); err != nil {
		return err
	}
	if s.rootResources == nil {
		return apperrors.ErrUnsupportedOperation
	}
	root := payload.RootResource
	current, err := s.rootResources.GetImportSource(ctx, p, payload.ClusterID, root.Namespace, "CustomResource", root.Name, root.CrdName)
	if err != nil {
		return err
	}
	if current.Resource.UID != root.UID || current.Resource.APIVersion != root.APIVersion || current.Resource.Kind != root.Kind {
		return apperrors.ErrConflict
	}
	return nil
}

func (s *DeclarativeService) validateRootObservationBinding(ctx context.Context, p domainidentity.Principal, environmentID string, payload domainmanifest.TaskPayload) error {
	environment, err := s.base.environments.GetApplicationEnvironment(ctx, p, environmentID)
	if err != nil {
		return err
	}
	root := payload.RootResource
	for _, target := range environment.Targets {
		if target.ID != root.TargetID {
			continue
		}
		imported, err := importedRootTarget(target)
		if err != nil {
			return err
		}
		if !target.Enabled || target.ExecutorKind != "manifest_ssa" || target.ConfigRef != payload.BindingID || target.ClusterID != payload.ClusterID || target.Namespace != root.Namespace || imported.Resource.UID != root.UID {
			return apperrors.ErrConflict
		}
		return nil
	}
	return apperrors.ErrConflict
}

// Automatic observations inherit the last operation's private identity. A
// revoked user/token cannot keep dispatching the frozen configuration to Agents.
func (s *DeclarativeService) rootObservationPrincipal(ctx context.Context, deployment domainmanifest.Deployment) (domainidentity.Principal, error) {
	task, err := s.tasks.GetExecutionTaskInternal(ctx, deployment.Status.LastExecutionTaskID)
	if err != nil {
		return domainidentity.Principal{}, err
	}
	payload, err := decodeTaskPayload(task.Payload)
	if err != nil || task.SecretPrincipal.UserID == "" || deployment.Spec.DeliverySnapshot == nil || !reflect.DeepEqual(payload.RootResource, deployment.Spec.DeliverySnapshot.RootResource) {
		return domainidentity.Principal{}, apperrors.ErrConflict
	}
	return task.SecretPrincipal, nil
}

func (s *DeclarativeService) authorizeRootDeploymentRead(ctx context.Context, p domainidentity.Principal, item domainmanifest.Deployment) error {
	if snapshot := item.Spec.DeliverySnapshot; snapshot != nil && (snapshot.RootResource != nil || domainmanifest.SensitiveDocuments(snapshot.Documents)) {
		return s.base.authorize(ctx, p, appaccess.PermPlatformConfigurationSecretDataView)
	}
	for _, resource := range item.Status.Inventory {
		if domainmanifest.SensitiveKind(resource.APIVersion, resource.Kind) {
			return s.base.authorize(ctx, p, appaccess.PermPlatformConfigurationSecretDataView)
		}
	}
	if item.Status.Drift != nil {
		for _, resource := range item.Status.Drift.Resources {
			if domainmanifest.SensitiveKind(resource.APIVersion, resource.Kind) {
				return s.base.authorize(ctx, p, appaccess.PermPlatformConfigurationSecretDataView)
			}
		}
	}
	return nil
}

func (s *DeclarativeService) rootTaskProvider(ctx context.Context, clusterID string, root bool) (string, error) {
	provider, err := s.taskProvider(ctx, clusterID)
	if err != nil || !root || provider == domainmanifest.TaskProviderDirect {
		return provider, err
	}
	connection, err := s.base.loadCluster(ctx, clusterID)
	if err != nil {
		return "", err
	}
	if !slices.Contains(connection.Summary.Capabilities, resourceruntime.RootCRAgentCapability) {
		return "", fmt.Errorf("%w: upgrade the Agent for UID-fenced root CR publication", apperrors.ErrUnsupportedOperation)
	}
	return resourceruntime.RootCRAgentProviderPrefix + clusterID, nil
}

func importedRootTarget(target domaincatalog.ReleaseTarget) (domainresource.ResourceSourceSnapshot, error) {
	var imported domainresource.ResourceSourceSnapshot
	source, _ := target.Metadata["resourceSource"].(string)
	if target.Metadata["importSource"] != "kubernetes" || target.Metadata["ownershipMode"] != "managed" || json.Unmarshal([]byte(source), &imported) != nil || imported.CRDName == "" || imported.Resource.UID == "" || imported.Resource.ClusterID != target.ClusterID || imported.Resource.Namespace != target.Namespace || imported.Resource.Name != target.WorkloadName {
		return imported, fmt.Errorf("%w: root CR requires explicit UID-bound Manifest publication control", apperrors.ErrConflict)
	}
	return imported, nil
}
