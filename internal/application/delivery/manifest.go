package delivery

import (
	"context"
	"errors"
	"fmt"

	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type ManifestDelivery interface {
	DeliveryDeployment(context.Context, domainidentity.Principal, string, string, domaincatalog.ReleaseTarget) (domainmanifest.Deployment, error)
	CreateDeliverySnapshot(context.Context, domainidentity.Principal, string, string, domaincatalog.ReleaseTarget, int, string, domainmanifest.DeliveryArtifacts) (domainmanifest.DeliverySnapshot, error)
	ValidateDeliverySnapshot(context.Context, domainidentity.Principal, domainmanifest.DeliverySnapshot) error
	ApplyDeliverySnapshot(context.Context, domainidentity.Principal, domainmanifest.DeliverySnapshot) (domainmanifest.Deployment, domaindelivery.ExecutionTask, error)
}

func (s *Service) manifestRuntimeBinding(ctx context.Context, principal domainidentity.Principal, binding domaincatalog.ApplicationEnvironment) (domaincatalog.ApplicationEnvironment, []domainmanifest.Deployment, error) {
	resolved := binding
	resolved.Targets = nil
	var deployments []domainmanifest.Deployment
	for _, target := range binding.Targets {
		if target.ExecutorKind != "manifest_ssa" {
			resolved.Targets = append(resolved.Targets, target)
			continue
		}
		if !target.Enabled {
			continue
		}
		if s.manifestDelivery == nil {
			return resolved, nil, fmt.Errorf("%w: Manifest runtime is unavailable", apperrors.ErrClusterUnready)
		}
		deployment, err := s.manifestDelivery.DeliveryDeployment(ctx, principal, binding.ApplicationID, binding.ID, target)
		if errors.Is(err, apperrors.ErrNotFound) {
			continue
		}
		if err != nil {
			return resolved, nil, err
		}
		deployments = append(deployments, deployment)
		for _, resource := range deployment.Status.Inventory {
			if resource.Kind != "Deployment" || resource.APIVersion != "apps/v1" || resource.Namespace != target.Namespace || resource.UID == "" {
				continue
			}
			actual := target
			actual.ExecutorKind = "manifest_inventory"
			actual.WorkloadKind, actual.WorkloadName = resource.Kind, resource.Name
			resolved.Targets = append(resolved.Targets, actual)
		}
	}
	return resolved, deployments, nil
}

func (s *Service) prepareManifestDelivery(ctx context.Context, principal domainidentity.Principal, applicationID, environmentID string, targets []domaincatalog.ReleaseTarget, input domaindelivery.DeliveryPlanInput) ([]domainmanifest.DeliverySnapshot, error) {
	if input.ManifestRevision < 0 || (input.ManifestRevision > 0 && len(targets) != 1) {
		return nil, fmt.Errorf("%w: manifestRevision requires a single target and a published revision", apperrors.ErrInvalidArgument)
	}
	var snapshots []domainmanifest.DeliverySnapshot
	for _, target := range targets {
		if target.ExecutorKind != "manifest_ssa" || input.Action == domaindelivery.ApplicationDeliveryActionBuild {
			continue
		}
		if input.Action != domaindelivery.ApplicationDeliveryActionDeploy || s.manifestDelivery == nil {
			return nil, fmt.Errorf("%w: Manifest delivery requires an available renderer and a deploy plan", apperrors.ErrInvalidArgument)
		}
		if err := s.authorizeApplicationDeliveryAction(ctx, principal, environmentID, input.Action); err != nil {
			return nil, err
		}
		serviceID, _ := target.Metadata["serviceId"].(string)
		artifacts, err := s.manifestArtifacts(ctx, principal, applicationID, serviceID, input.ReleaseBundleID)
		if err != nil {
			return nil, err
		}
		snapshot, err := s.manifestDelivery.CreateDeliverySnapshot(ctx, principal, applicationID, environmentID, target, input.ManifestRevision, input.ID, artifacts)
		if err != nil {
			return nil, err
		}
		snapshots = append(snapshots, snapshot)
	}
	if input.ManifestRevision > 0 && len(snapshots) == 0 {
		return nil, fmt.Errorf("%w: manifestRevision requires a Manifest deploy target", apperrors.ErrInvalidArgument)
	}
	return snapshots, nil
}

func (s *Service) validateManifestDelivery(ctx context.Context, principal domainidentity.Principal, applicationID, environmentID string, targets []domaincatalog.ReleaseTarget, input domaindelivery.ApplicationDeliveryActionInput) error {
	if input.Action == domaindelivery.ApplicationDeliveryActionBuild {
		return nil
	}
	count := 0
	for _, target := range targets {
		if target.ExecutorKind != "manifest_ssa" {
			continue
		}
		count++
		if input.Action != domaindelivery.ApplicationDeliveryActionDeploy || s.manifestDelivery == nil {
			return fmt.Errorf("%w: Manifest targets require a deploy plan", apperrors.ErrInvalidArgument)
		}
		found := false
		for _, snapshot := range input.ManifestSnapshots {
			if snapshot.TargetID != target.ID {
				continue
			}
			if snapshot.ApplicationEnvironmentID != environmentID || snapshot.BindingID != target.ConfigRef || snapshot.ClusterID != target.ClusterID || snapshot.Namespace != target.Namespace {
				return fmt.Errorf("%w: Manifest target changed; create a new delivery plan", apperrors.ErrConflict)
			}
			if err := s.validateManifestArtifacts(ctx, principal, applicationID, target, snapshot); err != nil {
				return err
			}
			if err := s.manifestDelivery.ValidateDeliverySnapshot(ctx, principal, snapshot); err != nil {
				return err
			}
			found = true
		}
		if !found {
			return fmt.Errorf("%w: Manifest target requires a preflighted delivery plan for application %s", apperrors.ErrInvalidArgument, applicationID)
		}
	}
	if count != len(input.ManifestSnapshots) {
		return fmt.Errorf("%w: Manifest target selection changed; create a new delivery plan", apperrors.ErrConflict)
	}
	return nil
}

func (s *Service) applyManifestDelivery(ctx context.Context, principal domainidentity.Principal, targetID string, snapshots []domainmanifest.DeliverySnapshot, result *domaindelivery.ApplicationDeliveryActionResult) error {
	for _, snapshot := range snapshots {
		if snapshot.TargetID != targetID {
			continue
		}
		deployment, task, err := s.manifestDelivery.ApplyDeliverySnapshot(ctx, principal, snapshot)
		if err != nil {
			return err
		}
		result.ManifestDeployments = append(result.ManifestDeployments, deployment)
		result.RelatedIDs.ExecutionTaskIDs = append(result.RelatedIDs.ExecutionTaskIDs, task.ID)
		if result.RelatedIDs.ExecutionTaskID == "" {
			result.RelatedIDs.ExecutionTaskID = task.ID
		}
		return nil
	}
	return fmt.Errorf("%w: Manifest snapshot is missing", apperrors.ErrConflict)
}
