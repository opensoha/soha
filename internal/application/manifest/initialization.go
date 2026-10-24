package manifest

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	resourceruntime "github.com/opensoha/soha-contracts/resource/runtime"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type initializationHistory interface {
	HasTargetWriteHistory(context.Context, string, string, []string, string, string) (bool, error)
}

func (s *DeclarativeService) validateInitializationTask(ctx context.Context, task domaindelivery.ExecutionTask, payload domainmanifest.TaskPayload) error {
	if payload.InitializationPlanID == "" {
		if task.ProviderKind == domainmanifest.TaskProviderDirectInitialization || strings.HasPrefix(task.ProviderKind, resourceruntime.ManifestInitializationProviderPrefix) {
			return apperrors.ErrConflict
		}
		if payload.DeploymentID != "" {
			current, err := s.repository.GetDeployment(ctx, payload.DeploymentID)
			if err != nil {
				return err
			}
			if initializationPlan(current.Spec.DeliverySnapshot) != "" {
				return apperrors.ErrConflict
			}
		}
		return nil
	}
	if err := s.validateInitializationScope(ctx, task, payload); err != nil {
		return err
	}
	return s.validateInitializationDeployment(ctx, payload)
}

func (s *DeclarativeService) validateInitializationScope(ctx context.Context, task domaindelivery.ExecutionTask, payload domainmanifest.TaskPayload) error {
	if err := validateInitializationPayload(payload); err != nil {
		return err
	}
	provider, err := s.initializationProvider(ctx, payload.ClusterID)
	if err != nil {
		return err
	}
	if task.ProviderKind != provider {
		return apperrors.ErrConflict
	}
	if s.executionPrincipals == nil {
		return apperrors.ErrUnsupportedOperation
	}
	p, err := s.executionPrincipals.CurrentExecutionPrincipal(ctx, task.SecretPrincipal.UserID, task.SecretPrincipal.AccessTokenID)
	if err != nil {
		return err
	}
	if p.UserID != task.SecretPrincipal.UserID {
		return apperrors.ErrUnauthorized
	}
	permission := appaccess.ManagedActionPermission(appaccess.PermDeliveryManifestDeploymentsManage, "trigger")
	action := domainaccess.ActionTrigger
	if payload.Action == domainmanifest.TaskActionPreflight {
		permission = appaccess.ManagedActionPermission(appaccess.PermDeliveryManifestDeploymentsManage, "preflight")
	}
	if payload.Action == domainmanifest.TaskActionObserve {
		permission = appaccess.PermDeliveryApplicationsView
		action = domainaccess.ActionView
	}
	if err := s.base.authorize(ctx, p, permission); err != nil {
		return err
	}
	binding, err := s.repository.GetBinding(ctx, payload.BindingID)
	if err != nil {
		return err
	}
	item, app, err := s.bindingPackage(ctx, p, binding.PackageID, action, permission)
	if err != nil {
		return err
	}
	if item.ID != payload.PackageID || item.ApplicationID != task.ApplicationID || binding.ApplicationEnvironmentID != task.ApplicationEnvironmentID || binding.ClusterID != payload.ClusterID || binding.Namespace != payload.Namespace {
		return apperrors.ErrConflict
	}
	if err := s.validateBinding(ctx, p, action, item, app, &binding, permission); err != nil {
		return err
	}
	return nil
}

func (s *DeclarativeService) validateInitializationDeployment(ctx context.Context, payload domainmanifest.TaskPayload) error {
	var err error
	snapshot := domainmanifest.DeliverySnapshot{DeliveryPlanID: payload.InitializationPlanID, ClusterID: payload.ClusterID, Namespace: payload.Namespace, Documents: payload.Documents}
	current := domainmanifest.Deployment{}
	if payload.DeploymentID != "" {
		current, err = s.repository.GetDeployment(ctx, payload.DeploymentID)
		if err != nil {
			return err
		}
		if current.Generation != payload.Generation || current.Spec.DeliverySnapshot == nil || !migrationTaskMatchesSnapshot(payload, *current.Spec.DeliverySnapshot) {
			return apperrors.ErrConflict
		}
		snapshot = *current.Spec.DeliverySnapshot
		if payload.Action == domainmanifest.TaskActionObserve && (current.Status.AppliedDigest != payload.RenderedDigest || !sameInitializationInventory(payload.Inventory, current.Status.Inventory)) {
			return apperrors.ErrConflict
		}
	}
	return s.checkInitialization(ctx, current, snapshot)
}

func sameInitializationInventory(a, b []domainmanifest.ResourceInventory) bool {
	if len(a) == 0 || len(a) != len(b) {
		return false
	}
	uids := map[string]string{}
	for _, item := range a {
		key := item.APIVersion + "/" + item.Kind + "/" + item.Namespace + "/" + item.Name
		if item.UID == "" || uids[key] != "" {
			return false
		}
		uids[key] = item.UID
	}
	for _, item := range b {
		key := item.APIVersion + "/" + item.Kind + "/" + item.Namespace + "/" + item.Name
		if item.UID == "" || uids[key] != item.UID {
			return false
		}
		delete(uids, key)
	}
	return true
}

func initializationPlan(snapshot *domainmanifest.DeliverySnapshot) string {
	if snapshot != nil && snapshot.Migration != nil && snapshot.Migration.Initialization {
		return snapshot.DeliveryPlanID
	}
	return ""
}

func (s *DeclarativeService) checkInitialization(ctx context.Context, current domainmanifest.Deployment, snapshot domainmanifest.DeliverySnapshot) error {
	own := initializationPlan(current.Spec.DeliverySnapshot) == snapshot.DeliveryPlanID && snapshot.DeliveryPlanID != ""
	if !own && (current.Status.AppliedRevision != 0 || current.Status.AppliedDigest != "" || current.Status.AppliedTemplate != nil || len(current.Status.Inventory) > 0 || current.Status.ObservedGeneration > 0 || current.Status.LastReconciledAt != nil || current.Status.Phase != "" && current.Status.Phase != domainmanifest.DeploymentPhasePending && current.Status.Phase != domainmanifest.DeploymentPhaseWaitingApproval) {
		return fmt.Errorf("%w: 已有部署但运行基线不明，不能按首次发布初始化", apperrors.ErrConflict)
	}
	reader, ok := s.repository.(initializationHistory)
	if !ok {
		return fmt.Errorf("%w: 初始化历史检查不可用", apperrors.ErrConflict)
	}
	previous, err := reader.HasTargetWriteHistory(ctx, snapshot.ClusterID, snapshot.Namespace, domainmanifest.ResourceKeys(snapshot.ClusterID, snapshot.Documents, snapshot.GitOpsDocuments), "", snapshot.DeliveryPlanID)
	if err != nil {
		return err
	}
	if previous {
		return fmt.Errorf("%w: 目标存在历史写入，必须核实运行基线", apperrors.ErrConflict)
	}
	payload := domainmanifest.TaskPayload{InitializationPlanID: snapshot.DeliveryPlanID, Action: domainmanifest.TaskActionPreflight, Documents: snapshot.Documents, GitOpsDocuments: snapshot.GitOpsDocuments, RootResource: snapshot.RootResource}
	return validateInitializationPayload(payload)
}

func validateInitializationPayload(payload domainmanifest.TaskPayload) error {
	data, _ := json.Marshal(payload)
	var wire sohaapi.ManifestExecutionTaskPayload
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if err := resourceruntime.ValidateManifestInitialization(wire); err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrConflict, err)
	}
	return nil
}

func (s *DeclarativeService) assessDeliveryMigration(ctx context.Context, p domainidentity.Principal, item domainmanifest.Package, current domainmanifest.Deployment, snapshot domainmanifest.DeliverySnapshot) (*sohaapi.TemplateMigrationAssessment, error) {
	assessment, err := s.migrationAssessment(ctx, p, item, current, snapshot.TemplateInputs)
	if err != nil && (assessment == nil || !assessment.Blocked || assessment.FromVersion != 0) {
		return assessment, err
	}
	if snapshot.TemplateInputs == nil || assessment != nil && assessment.FromVersion != 0 {
		return assessment, err
	}
	if err := s.checkInitialization(ctx, current, snapshot); err != nil {
		return nil, err
	}
	assessment = domaincatalog.AssessInitialization(assessment, snapshot.TemplateInputs.TemplateVersion)
	return assessment, nil
}

func (s *DeclarativeService) initializationProvider(ctx context.Context, clusterID string) (string, error) {
	connection, err := s.base.loadCluster(ctx, clusterID)
	if err != nil {
		return "", err
	}
	if connection != nil && connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		if !slices.Contains(connection.Summary.Capabilities, resourceruntime.ManifestInitializationCapability) {
			return "", fmt.Errorf("%w: Agent does not support create-only initialization", apperrors.ErrUnsupportedOperation)
		}
		return resourceruntime.ManifestInitializationProviderPrefix + strings.TrimSpace(clusterID), nil
	}
	return domainmanifest.TaskProviderDirectInitialization, nil
}

func validateInitializationResult(task domaindelivery.ExecutionTask, payload domainmanifest.TaskPayload, status domainmanifest.DeploymentStatus, result domainmanifest.TaskResult) error {
	if task.Status != "completed" || payload.InitializationPlanID == "" {
		return nil
	}
	if result.InitializationPlanID != payload.InitializationPlanID {
		return apperrors.ErrConflict
	}
	if payload.Action == domainmanifest.TaskActionObserve && !sameInitializationInventory(result.Inventory, status.Inventory) {
		return apperrors.ErrConflict
	}
	return nil
}

func appliedTemplateReference(deployment domainmanifest.Deployment, payload domainmanifest.TaskPayload, result domainmanifest.TaskResult) *sohaapi.TemplateVersionReference {
	snapshot := deployment.Spec.DeliverySnapshot
	if snapshot == nil || snapshot.TemplateInputs == nil || snapshot.RenderedDigest != payload.RenderedDigest {
		return nil
	}
	if payload.InitializationPlanID != "" && !inventoryHealthy(result.Inventory, payload.Documents, payload.GitOpsDocuments) {
		return nil
	}
	ref := snapshot.TemplateInputs
	return &sohaapi.TemplateVersionReference{TemplateID: ref.TemplateID, TemplateVersion: ref.TemplateVersion, TemplateDigest: ref.TemplateDigest}
}
