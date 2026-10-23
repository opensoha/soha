package manifest

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"reflect"
)

func (s *DeclarativeService) migrationAssessment(ctx context.Context, p domainidentity.Principal, item domainmanifest.Package, current domainmanifest.Deployment, inputs *domainmanifest.ServiceTemplateInputs) (*sohaapi.TemplateMigrationAssessment, error) {
	if inputs == nil {
		return nil, nil
	}
	service, err := s.base.applications.GetService(ctx, p, item.ApplicationID, item.ServiceID)
	if err != nil {
		return nil, err
	}
	reference := service.DeploymentTemplate
	if reference == nil {
		return nil, apperrors.ErrConflict
	}
	versions := []domaincatalog.DeploymentTemplate{}
	if reference.Detached {
		if reference.DetachedTemplate == nil {
			return nil, apperrors.ErrConflict
		}
		versions = append(versions, *reference.DetachedTemplate)
	} else {
		if s.base.deploymentTemplates == nil {
			return nil, fmt.Errorf("%w: migration template reader is unavailable", apperrors.ErrConflict)
		}
		versions, err = s.base.deploymentTemplates.ListDeploymentTemplateVersions(ctx, p, inputs.TemplateID)
	}
	if err != nil {
		return nil, err
	}
	items := make([]domaincatalog.MigrationVersion, 0, len(versions))
	for _, v := range versions {
		items = append(items, domaincatalog.MigrationVersion{Number: v.PublishedVersion, Digest: v.ContentDigest, Policy: v.MigrationPolicy})
	}
	from := int64(0)
	if baseline := current.Status.AppliedTemplate; baseline != nil && baseline.TemplateID == inputs.TemplateID && (current.Status.Phase == domainmanifest.DeploymentPhaseConverged || baseline.TemplateVersion == inputs.TemplateVersion) {
		from = domaincatalog.MigrationBaselineVersion(items, inputs.TemplateID, baseline)
	}
	assessment := domaincatalog.AssessMigration(items, from, inputs.TemplateVersion)
	if assessment == nil && from > 0 {
		assessment = &sohaapi.TemplateMigrationAssessment{FromVersion: from, ToVersion: inputs.TemplateVersion, Steps: []sohaapi.TemplateMigrationStep{}}
	}
	if assessment != nil && assessment.Blocked {
		return assessment, fmt.Errorf("%w: %s", apperrors.ErrConflict, assessment.Reason)
	}
	return assessment, nil
}

func (s *DeclarativeService) validateMigrationSnapshot(ctx context.Context, p domainidentity.Principal, item domainmanifest.Package, current domainmanifest.Deployment, snapshot domainmanifest.DeliverySnapshot) error {
	if initializationPlan(&snapshot) != "" {
		if err := s.checkInitialization(ctx, current, snapshot); err != nil {
			return err
		}
		if own := current.Spec.DeliverySnapshot; own != nil && initializationPlan(own) == snapshot.DeliveryPlanID && reflect.DeepEqual(own.Migration, snapshot.Migration) {
			return nil
		}
	}
	// Recover the exact successful apply without interpreting it as a new migration.
	if current.Spec.DeliverySnapshot != nil && current.Spec.DeliverySnapshot.DeliveryPlanID == snapshot.DeliveryPlanID && current.Status.AppliedDigest == snapshot.RenderedDigest && current.Status.AppliedTemplate != nil && snapshot.TemplateInputs != nil && current.Status.AppliedTemplate.TemplateID == snapshot.TemplateInputs.TemplateID && current.Status.AppliedTemplate.TemplateVersion == snapshot.TemplateInputs.TemplateVersion && current.Status.AppliedTemplate.TemplateDigest == snapshot.TemplateInputs.TemplateDigest {
		return nil
	}
	assessment, err := s.assessDeliveryMigration(ctx, p, item, current, snapshot)
	if err != nil {
		return err
	}
	left, _ := json.Marshal(assessment)
	right, _ := json.Marshal(snapshot.Migration)
	if string(left) != string(right) {
		return fmt.Errorf("%w: 成功发布基线或迁移要求变化，请重新创建并审批Plan", apperrors.ErrConflict)
	}
	return nil
}

func (s *DeclarativeService) validateMigrationTask(ctx context.Context, task domaindelivery.ExecutionTask, payload domainmanifest.TaskPayload) error {
	if payload.DeploymentID == "" || (payload.Action != domainmanifest.TaskActionApply && payload.Action != domainmanifest.TaskActionRepair && payload.Action != domainmanifest.TaskActionRollback) {
		return nil
	}
	current, err := s.repository.GetDeployment(ctx, payload.DeploymentID)
	if err != nil {
		return err
	}
	snapshot := current.Spec.DeliverySnapshot
	if snapshot == nil {
		return s.requireLegacyMigration(ctx, task.SecretPrincipal, payload.PackageID)
	}
	if s.executionPrincipals == nil || s.delivery == nil {
		return apperrors.ErrUnsupportedOperation
	}
	p, err := s.executionPrincipals.CurrentExecutionPrincipal(ctx, task.SecretPrincipal.UserID, task.SecretPrincipal.AccessTokenID)
	if err != nil {
		return err
	}
	if p.UserID != task.SecretPrincipal.UserID {
		return apperrors.ErrUnauthorized
	}
	plan, err := s.delivery.GetExecutingDeliveryPlan(ctx, p, snapshot.DeliveryPlanID)
	if err != nil {
		return err
	}
	if plan.ApplicationID != task.ApplicationID || plan.ApplicationEnvironmentID != task.ApplicationEnvironmentID || payload.Generation != current.Generation || !migrationTaskMatchesSnapshot(payload, *snapshot) {
		return apperrors.ErrConflict
	}
	if !migrationPlanContainsSnapshot(plan, *snapshot) {
		return apperrors.ErrConflict
	}
	return s.ValidateDeliverySnapshot(ctx, p, *snapshot)
}

func migrationPlanContainsSnapshot(plan domaindelivery.DeliveryPlan, snapshot domainmanifest.DeliverySnapshot) bool {
	if snapshot.Migration != nil && snapshot.Migration.RequiresApproval && !plan.RequiresApproval {
		return false
	}
	expected, _ := json.Marshal(snapshot)
	for _, value := range plan.ManifestSnapshots {
		data, _ := json.Marshal(value)
		if string(data) == string(expected) {
			return true
		}
	}
	return false
}

func migrationTaskMatchesSnapshot(payload domainmanifest.TaskPayload, snapshot domainmanifest.DeliverySnapshot) bool {
	return payload.InitializationPlanID == initializationPlan(&snapshot) && payload.PackageID == snapshot.PackageID && payload.BindingID == snapshot.BindingID && payload.ClusterID == snapshot.ClusterID && payload.Namespace == snapshot.Namespace && payload.Revision == snapshot.Revision && payload.RenderedDigest == snapshot.RenderedDigest && !payload.ForceConflicts && payload.FieldManager == "opensoha-manifest/"+snapshot.BindingID && reflect.DeepEqual(payload.Documents, snapshot.Documents) && reflect.DeepEqual(payload.GitOpsDocuments, snapshot.GitOpsDocuments) && reflect.DeepEqual(payload.RootResource, snapshot.RootResource)
}

func (s *DeclarativeService) requireLegacyMigration(ctx context.Context, p domainidentity.Principal, packageID string) error {
	item, err := s.base.get(ctx, packageID)
	if err != nil {
		return err
	}
	if item.ServiceID == "" {
		return nil
	}
	if s.templateMigrations != nil {
		required, err := s.templateMigrations.ServiceTemplateMigrationRequired(ctx, item.ApplicationID, item.ServiceID)
		if err != nil {
			return err
		}
		if required {
			return fmt.Errorf("%w: 该模板声明迁移要求，发布/回退必须使用应用Plan", apperrors.ErrConflict)
		}
		return nil
	}
	if _, err = s.base.authorizePackage(ctx, p, domainaccess.ActionTrigger, item); err != nil {
		return err
	}
	service, err := s.base.applications.GetService(ctx, p, item.ApplicationID, item.ServiceID)
	if err != nil {
		return err
	}
	ref := service.DeploymentTemplate
	if ref == nil {
		return nil
	}
	versions := []domaincatalog.DeploymentTemplate{}
	if ref.Detached {
		if ref.DetachedTemplate != nil {
			versions = append(versions, *ref.DetachedTemplate)
		}
	} else {
		if s.base.deploymentTemplates == nil {
			return apperrors.ErrUnsupportedOperation
		}
		versions, err = s.base.deploymentTemplates.ListDeploymentTemplateVersions(ctx, p, ref.TemplateID)
		if err != nil {
			return err
		}
	}
	for _, v := range versions {
		if v.MigrationPolicy != nil && (v.MigrationPolicy.Sequential || v.MigrationPolicy.Irreversible) {
			return fmt.Errorf("%w: 该模板声明迁移要求，发布/回退必须使用应用Plan", apperrors.ErrConflict)
		}
	}
	return nil
}
