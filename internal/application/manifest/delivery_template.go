package manifest

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"regexp"
	"strings"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type deploymentTemplateRenderer interface {
	RenderDeploymentTemplate(context.Context, domaincatalog.DeploymentTemplateSource, map[string]any, map[string]string, map[string]string) (domaincatalog.DeploymentTemplateSource, error)
}

var templateImageDigest = regexp.MustCompile(`^[^\s@]+@sha256:[a-f0-9]{64}$`)

func (s *DeclarativeService) deliveryTemplateFiles(ctx context.Context, principal domainidentity.Principal, item domainmanifest.Package, binding domainmanifest.EnvironmentBinding, files []domainmanifest.File, artifacts domainmanifest.DeliveryArtifacts) ([]domainmanifest.File, *domainmanifest.ServiceTemplateInputs, error) {
	return s.renderDeliveryTemplateFiles(ctx, principal, item, binding, files, artifacts, false)
}

func (s *DeclarativeService) renderDeliveryTemplateFiles(ctx context.Context, principal domainidentity.Principal, item domainmanifest.Package, binding domainmanifest.EnvironmentBinding, files []domainmanifest.File, artifacts domainmanifest.DeliveryArtifacts, configurationOnly bool) ([]domainmanifest.File, *domainmanifest.ServiceTemplateInputs, error) {
	if item.ServiceID == "" {
		return files, nil, nil
	}
	service, err := s.base.applications.GetService(ctx, principal, item.ApplicationID, item.ServiceID)
	if err != nil {
		return nil, nil, err
	}
	reference := service.DeploymentTemplate
	if reference == nil || reference.ManifestPackageID != item.ID {
		return files, nil, nil
	}
	if artifacts.ReleaseBundleID != "" && artifacts.ServiceID != item.ServiceID {
		return nil, nil, fmt.Errorf("%w: selected artifacts belong to another service", apperrors.ErrInvalidArgument)
	}
	template, err := s.base.serviceDeploymentTemplate(ctx, principal, *reference)
	if err != nil {
		return nil, nil, err
	}
	if len(binding.Overlay) != 0 || template.Source.Renderer != item.Renderer {
		return nil, nil, fmt.Errorf("%w: service templates require typed parameters and their selected renderer", apperrors.ErrInvalidArgument)
	}
	parameters, err := template.ParameterSchema.ResolveParameters(template.Defaults, reference.Parameters, binding.TemplateParameters, template.EnvironmentOverrides)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	images := make(map[string]string, len(template.Artifacts))
	for name, container := range template.Artifacts {
		image := artifacts.ContainerImages[container]
		if configurationOnly {
			images[name] = configurationPreviewImage(service, container)
			continue
		}
		if !templateImageDigest.MatchString(image) || artifacts.ReleaseBundleID == "" {
			return nil, nil, fmt.Errorf("%w: container %q requires a verified immutable artifact", apperrors.ErrInvalidArgument, container)
		}
		images[name] = image
	}
	renderer, ok := s.renderer.(deploymentTemplateRenderer)
	if !ok {
		return nil, nil, fmt.Errorf("%w: deployment template renderer is unavailable", apperrors.ErrInvalidArgument)
	}
	// Published package files are the execution source, including an independently
	// edited copy. Never re-read a template's current draft or moving Git ref.
	source := template.Source
	source.Files, source.Git, source.Kustomize = files, nil, binding.Kustomize
	rendered, err := renderer.RenderDeploymentTemplate(ctx, source, parameters,
		map[string]string{"applicationId": item.ApplicationID, "serviceKey": service.Key, "namespace": binding.Namespace}, images)
	if err != nil {
		return nil, nil, err
	}
	if configurationOnly {
		return rendered.Files, nil, nil
	}
	inputs := &domainmanifest.ServiceTemplateInputs{
		ServiceVersion: service.Version, TemplateID: reference.TemplateID, TemplateVersion: reference.Version,
		TemplateDigest: template.ContentDigest, Parameters: parameters, ArtifactImages: maps.Clone(artifacts.ContainerImages), ReleaseBundleID: artifacts.ReleaseBundleID,
	}
	if inputs.ArtifactImages == nil {
		inputs.ArtifactImages = map[string]string{}
	}
	return rendered.Files, inputs, nil
}

func configurationPreviewImage(service domainapp.Service, container string) string {
	repository := "preview.invalid/" + service.Key
	for _, item := range service.Containers {
		if item.Name == container && item.ImageRepository != "" {
			repository = item.ImageRepository
			break
		}
	}
	return repository + "@sha256:" + strings.Repeat("0", 64)
}

func (s *DeclarativeService) validateDeliveryTemplateInputs(ctx context.Context, principal domainidentity.Principal, item domainmanifest.Package, binding domainmanifest.EnvironmentBinding, files []domainmanifest.File, snapshot domainmanifest.DeliverySnapshot) ([]domainmanifest.File, error) {
	artifacts := domainmanifest.DeliveryArtifacts{}
	if snapshot.TemplateInputs != nil {
		artifacts.ReleaseBundleID = snapshot.TemplateInputs.ReleaseBundleID
		artifacts.ServiceID = snapshot.ServiceID
		artifacts.ContainerImages = snapshot.TemplateInputs.ArtifactImages
	}
	files, current, err := s.deliveryTemplateFiles(ctx, principal, item, binding, files, artifacts)
	if err != nil {
		return nil, err
	}
	// JSON normalization also covers integer values restored from persisted maps.
	currentJSON, err := json.Marshal(current)
	if err != nil {
		return nil, err
	}
	frozenJSON, err := json.Marshal(snapshot.TemplateInputs)
	if err != nil {
		return nil, err
	}
	if string(currentJSON) != string(frozenJSON) {
		return nil, fmt.Errorf("%w: service template inputs changed; create a new delivery plan", apperrors.ErrConflict)
	}
	return files, nil
}
