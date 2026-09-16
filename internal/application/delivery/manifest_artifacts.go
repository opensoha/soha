package delivery

import (
	"context"
	"fmt"
	"reflect"

	appexecution "github.com/opensoha/soha/internal/application/execution"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) manifestArtifacts(ctx context.Context, principal domainidentity.Principal, applicationID, serviceID, bundleID string) (domainmanifest.DeliveryArtifacts, error) {
	if bundleID == "" {
		return domainmanifest.DeliveryArtifacts{}, nil
	}
	bundle, err := s.GetReleaseBundle(ctx, principal, bundleID)
	if err != nil {
		return domainmanifest.DeliveryArtifacts{}, err
	}
	if bundle.ApplicationID != applicationID || bundle.Status != "ready" {
		return domainmanifest.DeliveryArtifacts{}, fmt.Errorf("%w: selected bundle must be ready and belong to this application", apperrors.ErrInvalidArgument)
	}
	build, err := s.bundleBuildTask(ctx, principal, bundle)
	if err != nil {
		return domainmanifest.DeliveryArtifacts{}, err
	}
	if serviceID == "" {
		serviceID, _ = build.Payload["serviceId"].(string)
	}
	services, err := s.applications.ListServices(ctx, principal, applicationID)
	if err != nil {
		return domainmanifest.DeliveryArtifacts{}, err
	}
	for _, service := range services {
		if service.ID == serviceID && service.ApplicationID == applicationID {
			return serviceBundleArtifacts(bundle, build, service)
		}
	}
	return domainmanifest.DeliveryArtifacts{}, fmt.Errorf("%w: select a service associated with this build", apperrors.ErrInvalidArgument)
}

func (s *Service) bundleBuildTask(ctx context.Context, principal domainidentity.Principal, bundle domaindelivery.ReleaseBundle) (domaindelivery.ExecutionTask, error) {
	source := bundle
	if bundle.SourceType == "promotion" {
		sourceID, _ := bundle.Metadata["sourceReleaseBundleId"].(string)
		var err error
		source, err = s.GetReleaseBundle(ctx, principal, sourceID)
		if err != nil {
			return domaindelivery.ExecutionTask{}, err
		}
		if source.ID == bundle.ID || source.SourceType == "promotion" || source.ApplicationID != bundle.ApplicationID || source.Status != "ready" || source.ArtifactRef != bundle.ArtifactRef || source.ArtifactDigest != bundle.ArtifactDigest {
			return domaindelivery.ExecutionTask{}, fmt.Errorf("%w: promoted bundle disagrees with its original build bundle", apperrors.ErrInvalidArgument)
		}
	}
	tasks, err := s.repository.ListExecutionTasks(ctx, domaindelivery.ExecutionTaskFilter{ApplicationID: source.ApplicationID, ReleaseBundleID: source.ID, Limit: 1000})
	if err != nil {
		return domaindelivery.ExecutionTask{}, err
	}
	var build *domaindelivery.ExecutionTask
	for _, task := range tasks {
		if task.TaskKind != "build" || task.ApplicationID != source.ApplicationID || task.ReleaseBundleID != source.ID {
			continue
		}
		if build != nil || task.Status != "completed" {
			return domaindelivery.ExecutionTask{}, fmt.Errorf("%w: bundle build result is ambiguous or incomplete", apperrors.ErrInvalidArgument)
		}
		build = &task
	}
	if build == nil {
		return domaindelivery.ExecutionTask{}, fmt.Errorf("%w: bundle has no completed build provenance", apperrors.ErrInvalidArgument)
	}
	if bundle.SourceType == "promotion" && bundle.Metadata["sourceBuildTaskId"] != build.ID {
		return domaindelivery.ExecutionTask{}, fmt.Errorf("%w: promoted bundle build provenance changed", apperrors.ErrInvalidArgument)
	}
	return *build, nil
}

func serviceBundleArtifacts(bundle domaindelivery.ReleaseBundle, task domaindelivery.ExecutionTask, service domainapp.Service) (domainmanifest.DeliveryArtifacts, error) {
	result := domainmanifest.DeliveryArtifacts{ReleaseBundleID: bundle.ID, ServiceID: service.ID, ContainerImages: map[string]string{}}
	if service.BuildSourceID == "" || task.Payload["buildSourceId"] != service.BuildSourceID {
		return result, fmt.Errorf("%w: bundle build source is not bound to the selected service", apperrors.ErrInvalidArgument)
	}
	image, err := domaindelivery.ImmutableImageReference(bundle.ArtifactRef, bundle.ArtifactDigest)
	if err != nil {
		return result, fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	actual, err := appexecution.BuildArtifactImage(task)
	if err != nil || actual != image {
		return result, fmt.Errorf("%w: bundle digest disagrees with its completed build", apperrors.ErrInvalidArgument)
	}
	expected, _ := task.Payload["image"].(string)
	output, err := domaindelivery.ImmutableImageReference(expected, bundle.ArtifactDigest)
	if err != nil || output != image {
		return result, fmt.Errorf("%w: bundle output disagrees with its build task", apperrors.ErrInvalidArgument)
	}
	for _, container := range service.Containers {
		output, err := domaindelivery.ImmutableImageReference(container.ImageRepository, bundle.ArtifactDigest)
		if err == nil && output == image {
			result.ContainerImages[container.Name] = image
		}
	}
	if len(result.ContainerImages) == 0 {
		return result, fmt.Errorf("%w: bundle has no matching service container output", apperrors.ErrInvalidArgument)
	}
	return result, nil
}

func (s *Service) validateManifestArtifacts(ctx context.Context, principal domainidentity.Principal, applicationID string, target domaincatalog.ReleaseTarget, snapshot domainmanifest.DeliverySnapshot) error {
	if snapshot.TemplateInputs == nil || snapshot.TemplateInputs.ReleaseBundleID == "" {
		return nil
	}
	artifacts, err := s.manifestArtifacts(ctx, principal, applicationID, snapshot.ServiceID, snapshot.TemplateInputs.ReleaseBundleID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(artifacts.ContainerImages, snapshot.TemplateInputs.ArtifactImages) {
		return fmt.Errorf("%w: artifacts changed for target %s; create a new delivery plan", apperrors.ErrConflict, target.ID)
	}
	return nil
}
