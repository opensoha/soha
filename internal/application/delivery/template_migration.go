package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/helmrelease"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) TemplateMigrationBaseline(ctx context.Context, p domainidentity.Principal, appID, envID, targetID string) (*sohaapi.TemplateVersionReference, error) {
	if err := s.authorizeRuntimeScope(ctx, p, appID, envID); err != nil {
		return nil, err
	}
	env, err := s.catalog.GetApplicationEnvironment(ctx, p, envID)
	if err != nil {
		return nil, err
	}
	if env.ApplicationID != appID {
		return nil, apperrors.ErrAccessDenied
	}
	for _, target := range env.Targets {
		if target.ID != targetID || !target.Enabled {
			continue
		}
		if target.Helm != nil {
			ref, _, err := s.helmMigrationBaseline(ctx, appID, envID, target)
			return ref, err
		}
		if target.ConfigRef != "" && s.manifestDelivery != nil {
			current, err := s.manifestDelivery.DeliveryDeployment(ctx, p, appID, envID, target)
			if errors.Is(err, apperrors.ErrNotFound) {
				return nil, nil
			}
			if err != nil {
				return nil, err
			}
			if current.Status.Phase == domainmanifest.DeploymentPhaseConverged {
				return current.Status.AppliedTemplate, nil
			}
		}
		return nil, nil
	}
	return nil, apperrors.ErrNotFound
}

func (s *Service) helmMigrationBaseline(ctx context.Context, appID, envID string, target domaincatalog.ReleaseTarget) (*sohaapi.TemplateVersionReference, int, error) {
	tasks, err := s.repository.ListExecutionTasks(ctx, domaindelivery.ExecutionTaskFilter{ApplicationID: appID, ApplicationEnvironmentID: envID, QueueKey: "helm:" + target.ClusterID + ":" + target.Namespace + ":" + target.Helm.ReleaseName, TaskKinds: []string{"helm_apply", "helm_observe"}, Status: "completed", Limit: 1000})
	if err != nil {
		return nil, 0, err
	}
	for _, task := range tasks {
		payload, err := decodeHelmPayload(task.Payload)
		previous := payload.Snapshot
		if err != nil {
			return nil, 0, nil
		}
		if !migrationHelmTaskMatches(task, payload, target, appID, envID) {
			continue
		}
		var result sohaapi.HelmExecutionTaskResult
		data, err := json.Marshal(task.Result["helm"])
		if err != nil || json.Unmarshal(data, &result) != nil || helmrelease.ValidateCompletedResult(payload, result) != nil {
			return nil, 0, nil
		}
		// The latest verified apply is authoritative even if it predates template tracking.
		if !result.Ready {
			return nil, result.Revision, nil
		}
		return previous.TemplateInputs, result.Revision, nil
	}
	return nil, 0, nil
}

func migrationHelmTaskMatches(task domaindelivery.ExecutionTask, payload sohaapi.HelmExecutionTaskPayload, target domaincatalog.ReleaseTarget, appID, envID string) bool {
	previous := payload.Snapshot
	serviceID, _ := target.Metadata["serviceId"].(string)
	return task.TaskKind == "helm_"+string(payload.Action) && task.Status == "completed" && (payload.Action == sohaapi.Apply || payload.Action == sohaapi.Observe) && previous.TargetID == target.ID && previous.ServiceID == serviceID && previous.ApplicationID == appID && previous.ApplicationEnvironmentID == envID && previous.ClusterID == target.ClusterID && previous.Namespace == target.Namespace && previous.ReleaseName == target.Helm.ReleaseName
}

func (s *Service) assessHelmMigration(ctx context.Context, p domainidentity.Principal, snapshot sohaapi.HelmDeliverySnapshot, target domaincatalog.ReleaseTarget) (*sohaapi.TemplateMigrationAssessment, error) {
	if snapshot.TemplateInputs == nil {
		return s.assessUntrackedHelmMigration(ctx, p, snapshot, target)
	}
	if s.templateUpdates == nil {
		return nil, fmt.Errorf("%w: migration owner unavailable", apperrors.ErrConflict)
	}
	ref, nativeRevision, err := s.helmMigrationBaseline(ctx, snapshot.ApplicationID, snapshot.ApplicationEnvironmentID, target)
	if err != nil {
		return nil, err
	}
	from := int64(0)
	if ref != nil && ref.TemplateID == snapshot.TemplateInputs.TemplateID && nativeRevision == snapshot.ExpectedRevision {
		published, err := s.templateUpdates.ReleaseReference(ctx, p, ref.TemplateID, ref.TemplateVersion)
		if err != nil {
			return nil, err
		}
		if published != nil && reflect.DeepEqual(published, ref) {
			from = ref.TemplateVersion
		}
	}
	assessment, err := s.templateUpdates.AssessRelease(ctx, p, "deployment", snapshot.TemplateInputs.TemplateID, from, snapshot.TemplateInputs.TemplateVersion)
	if err != nil {
		return nil, err
	}
	if assessment != nil && assessment.Blocked {
		return assessment, fmt.Errorf("%w: %s", apperrors.ErrConflict, assessment.Reason)
	}
	return assessment, nil
}

func (s *Service) assessUntrackedHelmMigration(ctx context.Context, p domainidentity.Principal, snapshot sohaapi.HelmDeliverySnapshot, target domaincatalog.ReleaseTarget) (*sohaapi.TemplateMigrationAssessment, error) {
	ref, _, err := s.helmMigrationBaseline(ctx, snapshot.ApplicationID, snapshot.ApplicationEnvironmentID, target)
	if err != nil {
		return nil, err
	}
	if ref == nil {
		env, err := s.catalog.GetApplicationEnvironment(ctx, p, snapshot.ApplicationEnvironmentID)
		if err != nil {
			return nil, err
		}
		ref, err = s.helmTemplateReference(ctx, p, snapshot.ServiceID, env, target)
		if err != nil {
			return nil, err
		}
	}
	if ref == nil {
		return nil, nil
	}
	if s.templateUpdates == nil {
		return nil, apperrors.ErrConflict
	}
	assessment, err := s.templateUpdates.AssessRelease(ctx, p, "deployment", ref.TemplateID, 0, ref.TemplateVersion)
	if err != nil {
		return nil, err
	}
	if assessment != nil {
		return assessment, fmt.Errorf("%w: 目标缺少历史模板身份，无法证明变更未跨迁移边界，请核实历史发布版本", apperrors.ErrConflict)
	}
	return nil, nil
}

func (s *Service) helmTemplateReference(ctx context.Context, p domainidentity.Principal, serviceID string, env domaincatalog.ApplicationEnvironment, target domaincatalog.ReleaseTarget) (*sohaapi.TemplateVersionReference, error) {
	service, err := s.helmTargetService(ctx, p, env.ApplicationID, serviceID)
	if err != nil {
		return nil, err
	}
	binding := service.DeploymentTemplate
	if binding == nil {
		return nil, nil
	}
	if binding.Detached {
		if v := binding.DetachedTemplate; v != nil && v.MigrationPolicy != nil && (v.MigrationPolicy.Sequential || v.MigrationPolicy.Irreversible) {
			return nil, fmt.Errorf("%w: 独立配置包含迁移声明，请先建立可核对的版本化模板再发布", apperrors.ErrConflict)
		}
		return nil, nil
	}
	id, version := binding.TemplateID, binding.Version
	if selection := target.Metadata["templateSelection"]; selection != nil {
		var selected domainmanifest.TemplateSelection
		data, err := json.Marshal(selection)
		if err != nil || json.Unmarshal(data, &selected) != nil || selected.TemplateID != id || selected.Version < 1 {
			return nil, apperrors.ErrConflict
		}
		version = selected.Version
	}
	if s.templateUpdates == nil {
		return nil, apperrors.ErrConflict
	}
	// Published history owns the digest; a caller cannot supply a template identity.
	assessment, err := s.templateUpdates.ReleaseReference(ctx, p, id, version)
	return assessment, err
}

func (s *Service) validateHelmMigration(ctx context.Context, p domainidentity.Principal, snapshot sohaapi.HelmDeliverySnapshot, target domaincatalog.ReleaseTarget) error {
	if snapshot.RollbackRevision == 0 {
		env, err := s.catalog.GetApplicationEnvironment(ctx, p, snapshot.ApplicationEnvironmentID)
		if err != nil {
			return err
		}
		ref, err := s.helmTemplateReference(ctx, p, snapshot.ServiceID, env, target)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(ref, snapshot.TemplateInputs) {
			return fmt.Errorf("%w: template selection changed", apperrors.ErrConflict)
		}
	}
	migration, err := s.assessHelmMigration(ctx, p, snapshot, target)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(migration, snapshot.Migration) {
		return fmt.Errorf("%w: migration baseline changed; create a new Plan", apperrors.ErrConflict)
	}
	return nil
}

func planHasMigration(plan domaindelivery.DeliveryPlan) bool {
	if plan.Impact["templateMigration"] != nil {
		return true
	}
	for _, snapshot := range plan.ManifestSnapshots {
		if snapshot.Migration != nil && snapshot.Migration.RequiresApproval {
			return true
		}
	}
	for _, snapshot := range plan.HelmSnapshots {
		if snapshot.Migration != nil && snapshot.Migration.RequiresApproval {
			return true
		}
	}
	return false
}

func freezePlanMigration(input *domaindelivery.DeliveryPlanInput) {
	steps := []*sohaapi.TemplateMigrationAssessment{}
	for _, snapshot := range input.ManifestSnapshots {
		if snapshot.Migration != nil {
			steps = append(steps, snapshot.Migration)
		}
	}
	for _, snapshot := range input.HelmSnapshots {
		if snapshot.Migration != nil {
			steps = append(steps, snapshot.Migration)
		}
	}
	for _, step := range steps {
		if step.RequiresApproval {
			input.RequiresApproval = true
			input.RiskLevel = "high"
		}
	}
	if len(steps) > 0 {
		input.Impact["templateMigrations"] = steps
	}
}
