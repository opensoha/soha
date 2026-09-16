package delivery

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type frozenBuildRuntime interface {
	PrepareDeliveryBuild(context.Context, domainidentity.Principal, domainbuild.TriggerInput) (domainbuild.Prepared, error)
	TriggerFrozen(context.Context, domainidentity.Principal, domainbuild.Prepared) (domainbuild.Record, error)
}

type manifestConfigurationRuntime interface {
	FreezeDeliveryConfiguration(context.Context, domainidentity.Principal, string, string, string, domaincatalog.ReleaseTarget, int) (domainmanifest.DeliveryConfiguration, error)
}

// ValidateDeliveryTarget checks saved references without resolving source refs,
// rendering manifests, contacting a runner, or creating any execution records.
func (s *Service) ValidateDeliveryTarget(ctx context.Context, principal domainidentity.Principal, target domainworkflow.DeliveryTargetInput) error {
	app, service, err := s.deliveryTargetService(ctx, principal, target)
	if err != nil {
		return err
	}
	if target.Action == "build" || target.Action == "build_deploy" {
		found := false
		for _, source := range app.BuildSources {
			found = found || source.ID == service.BuildSourceID
		}
		if service.BuildSourceID == "" || !found {
			return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "delivery_service_build_source_missing", "Bind a build source from this application in the service configuration before building.", "请先在服务配置中绑定本应用的构建源，再进行构建。")
		}
	}
	if target.ApplicationEnvironmentID != "" {
		binding, err := s.catalog.GetApplicationEnvironment(ctx, principal, target.ApplicationEnvironmentID)
		if err != nil {
			return err
		}
		if binding.ApplicationID != app.ID {
			return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "delivery_target_environment_mismatch", "Select an environment belonging to the target application.", "请选择属于目标应用的环境。")
		}
		if target.ReleaseTargetID != "" && !hasDeliveryReleaseTarget(binding, service.ID, target.ReleaseTargetID) {
			return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "delivery_target_binding_mismatch", "Select a deployment target bound to this service and environment.", "请选择已绑定到当前服务和环境的部署目标。")
		}
	}
	if target.ReleaseBundleID != "" {
		bundle, err := s.GetReleaseBundle(ctx, principal, target.ReleaseBundleID)
		if err != nil {
			return err
		}
		if bundle.ApplicationID != app.ID {
			return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "delivery_target_bundle_mismatch", "Select a release bundle belonging to the target application.", "请选择属于目标应用的版本包。")
		}
	}
	return nil
}

func hasDeliveryReleaseTarget(binding domaincatalog.ApplicationEnvironment, serviceID, targetID string) bool {
	for _, target := range binding.Targets {
		if target.ID == targetID && target.Metadata["serviceId"] == serviceID {
			return true
		}
	}
	return false
}

func (s *Service) FreezeDeliveryTarget(ctx context.Context, principal domainidentity.Principal, target domainworkflow.DeliveryTargetInput) (domainworkflow.DeliveryTargetSnapshot, error) {
	if target.HelmRevision < 0 || target.HelmRevision > 0 && (target.Action != "config_update" || target.ReleaseBundleID != "") {
		return domainworkflow.DeliveryTargetSnapshot{}, fmt.Errorf("%w: Helm rollback requires config_update without an artifact override", apperrors.ErrInvalidArgument)
	}
	app, service, err := s.deliveryTargetService(ctx, principal, target)
	if err != nil {
		return domainworkflow.DeliveryTargetSnapshot{}, err
	}
	snapshot := domainworkflow.DeliveryTargetSnapshot{Target: target, ApplicationName: app.Name, ServiceName: service.Name, ServiceVersion: service.Version, BuildSourceID: service.BuildSourceID, DeploymentTemplate: service.DeploymentTemplate}
	var binding domaincatalog.ApplicationEnvironment
	if target.ApplicationEnvironmentID != "" {
		binding, err = s.catalog.GetApplicationEnvironment(ctx, principal, target.ApplicationEnvironmentID)
		if err != nil {
			return snapshot, err
		}
		if binding.ApplicationID != app.ID {
			return snapshot, fmt.Errorf("%w: environment does not belong to the target application", apperrors.ErrInvalidArgument)
		}
		snapshot.HealthTimeoutSeconds = binding.ReleasePolicy.RolloutTimeoutSeconds
		snapshot.EnvironmentName = firstNonEmpty(binding.Alias, binding.EnvironmentKey, binding.ID)
	}
	if target.Action == "build" || target.Action == "build_deploy" {
		builder, ok := s.builds.(frozenBuildRuntime)
		if !ok {
			return snapshot, fmt.Errorf("%w: frozen build runtime is unavailable", apperrors.ErrInvalidArgument)
		}
		input := domainbuild.TriggerInput{ApplicationID: app.ID, ServiceID: service.ID, ApplicationEnvironmentID: target.ApplicationEnvironmentID, BuildSourceID: service.BuildSourceID, RefType: firstNonEmpty(binding.BuildPolicy.RefType, "branch"), RefName: binding.BuildPolicy.RefValue, ImageTag: binding.BuildPolicy.ImageTagTemplate, Variables: mergeActionMaps(binding.BuildPolicy.Variables, nil), RepositoryRefs: target.RepositoryRefs, BuildArgs: mergeActionMaps(binding.BuildPolicy.BuildArgs, nil)}
		for key, value := range target.BuildArgs {
			input.BuildArgs[key] = value
		}
		prepared, err := builder.PrepareDeliveryBuild(ctx, principal, input)
		if err != nil {
			return snapshot, err
		}
		snapshot.FrozenBuild, snapshot.BuildFingerprint, snapshot.RepositoryRefs = &prepared, prepared.Fingerprint, prepared.Input.RepositoryRefs
	}
	if target.Action != "build" {
		if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermDeliveryReleaseBundlesView); err != nil {
			return snapshot, err
		}
		if err := s.freezeDeliveryDeployment(ctx, principal, service, binding, &snapshot); err != nil {
			return snapshot, err
		}
	}
	snapshot.FrozenScope = deliveryTargetScope(app, snapshot.Target, snapshot.FrozenReleaseTarget)
	snapshot.ConfigurationDigest, err = s.deliveryConfigurationDigest(ctx, principal, service, binding, snapshot.FrozenManifest, snapshot.FrozenReleaseTarget)
	return snapshot, err
}

func (s *Service) deliveryTargetService(ctx context.Context, principal domainidentity.Principal, target domainworkflow.DeliveryTargetInput) (domainapp.App, domainapp.Service, error) {
	app, err := s.applications.Get(ctx, principal, target.ApplicationID)
	if err != nil {
		return app, domainapp.Service{}, err
	}
	services, err := s.applications.ListServices(ctx, principal, app.ID)
	if err != nil {
		return app, domainapp.Service{}, err
	}
	for _, service := range services {
		if service.ID == target.ServiceID && service.ApplicationID == app.ID {
			return app, service, nil
		}
	}
	return app, domainapp.Service{}, fmt.Errorf("%w: service does not belong to the target application", apperrors.ErrInvalidArgument)
}

func (s *Service) freezeDeliveryDeployment(ctx context.Context, principal domainidentity.Principal, service domainapp.Service, binding domaincatalog.ApplicationEnvironment, snapshot *domainworkflow.DeliveryTargetSnapshot) error {
	target, err := selectedDeliveryTarget(binding, snapshot.Target)
	if err != nil {
		return err
	}
	if target.ExecutorKind == "docker_compose" && target.Docker != nil {
		return s.freezeDockerDeployment(ctx, principal, service, binding, target, snapshot)
	}
	if target.ExecutorKind == "helm_sdk" && target.Helm != nil {
		return s.freezeHelmDeployment(ctx, principal, service, binding, target, snapshot)
	}
	if snapshot.Target.HelmRevision > 0 {
		return fmt.Errorf("%w: helmRevision requires a Helm target", apperrors.ErrInvalidArgument)
	}
	if target.ExecutorKind != "manifest_ssa" {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "delivery_executor_unsupported", "This deployment target does not support delivery batches yet. Select a supported deployment method.", "此部署目标尚未支持交付批次，请选择已支持的部署方式。")
	}
	runtime, ok := s.manifestDelivery.(manifestConfigurationRuntime)
	if !ok {
		return fmt.Errorf("%w: Manifest delivery intent runtime is unavailable", apperrors.ErrInvalidArgument)
	}
	intent, err := runtime.FreezeDeliveryConfiguration(ctx, principal, service.ApplicationID, service.ID, binding.ID, target, 0)
	if err != nil {
		return err
	}
	snapshot.FrozenManifest, snapshot.FrozenReleaseTarget = &intent, &target
	snapshot.Target.ReleaseTargetID = target.ID
	snapshot.ManifestPackageID, snapshot.ManifestBindingID, snapshot.ManifestRevision = intent.PackageID, intent.BindingID, intent.Revision
	if snapshot.Target.ReleaseBundleID != "" {
		_, err = s.manifestArtifacts(ctx, principal, service.ApplicationID, service.ID, snapshot.Target.ReleaseBundleID)
		return err
	}
	if snapshot.Target.Action == "build_deploy" {
		return nil
	}
	if snapshot.Target.Action == "config_update" {
		deployment, err := s.manifestDelivery.DeliveryDeployment(ctx, principal, service.ApplicationID, binding.ID, target)
		if err != nil {
			return err
		}
		if selected := deployment.Spec.DeliverySnapshot; selected != nil && selected.TemplateInputs != nil {
			snapshot.Target.ReleaseBundleID = selected.TemplateInputs.ReleaseBundleID
		}
	}
	if strings.TrimSpace(snapshot.Target.ReleaseBundleID) == "" {
		return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "delivery_artifact_missing", "No verified artifact is selected or deployed. Build an artifact or select an existing one first.", "尚无已验证或已部署的产物，请先构建产物，或选择已有产物进行部署。")
	}
	_, err = s.manifestArtifacts(ctx, principal, service.ApplicationID, service.ID, snapshot.Target.ReleaseBundleID)
	return err
}

func (s *Service) deliveryConfigurationDigest(ctx context.Context, principal domainidentity.Principal, service domainapp.Service, binding domaincatalog.ApplicationEnvironment, intent *domainmanifest.DeliveryConfiguration, target *domaincatalog.ReleaseTarget) (string, error) {
	environmentApproval := false
	if binding.EnvironmentID != "" {
		environments, err := s.catalog.ListEnvironments(ctx, principal)
		if err != nil {
			return "", err
		}
		found := false
		for _, environment := range environments {
			if environment.ID == binding.EnvironmentID {
				environmentApproval, found = environment.RequiresApproval, environment.Enabled
				break
			}
		}
		if !found {
			return "", fmt.Errorf("%w: target environment is unavailable", apperrors.ErrInvalidArgument)
		}
	}
	// Unrelated targets and cosmetic environment edits do not change this target.
	data, err := json.Marshal(struct {
		Service             domainapp.Service
		BuildPolicy         domaincatalog.BuildPolicy
		ReleasePolicy       domaincatalog.ReleasePolicy
		EnvironmentApproval bool
		EnvironmentID       string
		Intent              *domainmanifest.DeliveryConfiguration
		Target              *domaincatalog.ReleaseTarget
	}{service, binding.BuildPolicy, binding.ReleasePolicy, environmentApproval, binding.ID, intent, target})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}
