package app

import (
	"context"
	"fmt"
	"strings"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type DeploymentTemplateReader interface {
	GetDeploymentTemplate(context.Context, domainidentity.Principal, string) (domaincatalog.DeploymentTemplate, error)
	GetDeploymentTemplateVersion(context.Context, domainidentity.Principal, string, int64) (domaincatalog.DeploymentTemplate, error)
}

type DeploymentPackageReader interface {
	Get(context.Context, string) (domainmanifest.Package, error)
}

func (s *Service) SetDeploymentTemplateReaders(templates DeploymentTemplateReader, packages DeploymentPackageReader) {
	s.deploymentTemplates, s.deploymentPackages = templates, packages
}

func (s *Service) prepareServiceDeploymentTemplate(ctx context.Context, principal domainidentity.Principal, applicationID, serviceID string, input *domainapp.ServiceInput) error {
	if input.DeploymentTemplate == nil {
		return nil
	}
	if input.DeploymentTemplate.Version < 1 || strings.TrimSpace(input.DeploymentTemplate.TemplateID) == "" || input.DeploymentTemplate.Parameters == nil {
		return fmt.Errorf("%w: a published deployment template version and parameters are required", apperrors.ErrInvalidArgument)
	}
	if serviceID != "" && input.ExpectedVersion == nil {
		return fmt.Errorf("%w: deployment template changes require expectedVersion", apperrors.ErrInvalidArgument)
	}
	binding := *input.DeploymentTemplate
	snapshot, err := s.serviceDeploymentTemplate(ctx, principal, applicationID, serviceID, binding)
	if err != nil {
		return err
	}
	// A request can select a published version, never supply its trusted snapshot.
	binding.DetachedTemplate = nil
	if binding.Detached {
		binding.DetachedTemplate = &snapshot
	}
	binding.Parameters, err = snapshot.ParameterSchema.ResolveParameters(snapshot.Defaults, binding.Parameters, nil, nil)
	if err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	if err := validateServiceTemplateArtifacts(snapshot.Artifacts, input.Containers); err != nil {
		return err
	}
	if err := s.validateServiceTemplatePackage(ctx, applicationID, serviceID, binding); err != nil {
		return err
	}
	input.DeploymentTemplate = &binding
	return nil
}

func (s *Service) serviceDeploymentTemplate(ctx context.Context, principal domainidentity.Principal, applicationID, serviceID string, binding domaincatalog.DeploymentTemplateBinding) (domaincatalog.DeploymentTemplate, error) {
	if binding.Detached && serviceID != "" {
		current, err := s.repo.GetService(ctx, applicationID, serviceID)
		if err != nil {
			return domaincatalog.DeploymentTemplate{}, err
		}
		previous := current.DeploymentTemplate
		if previous != nil && previous.Detached && previous.TemplateID == binding.TemplateID && previous.Version == binding.Version && previous.ManifestPackageID == binding.ManifestPackageID {
			if previous.DetachedTemplate == nil {
				return domaincatalog.DeploymentTemplate{}, fmt.Errorf("%w: independent deployment configuration is missing its source snapshot", apperrors.ErrConflict)
			}
			return *previous.DetachedTemplate, nil
		}
	}
	if s.deploymentTemplates == nil {
		return domaincatalog.DeploymentTemplate{}, fmt.Errorf("%w: deployment template reader is unavailable", apperrors.ErrInvalidArgument)
	}
	head, err := s.deploymentTemplates.GetDeploymentTemplate(ctx, principal, binding.TemplateID)
	if err != nil {
		return domaincatalog.DeploymentTemplate{}, err
	}
	if !head.Enabled || head.PublicationState == "deprecated" {
		if err := s.requireExistingDeploymentReference(ctx, applicationID, serviceID, binding); err != nil {
			return domaincatalog.DeploymentTemplate{}, err
		}
	}
	return s.deploymentTemplates.GetDeploymentTemplateVersion(ctx, principal, binding.TemplateID, binding.Version)
}

func (s *Service) requireExistingDeploymentReference(ctx context.Context, applicationID, serviceID string, binding domaincatalog.DeploymentTemplateBinding) error {
	if serviceID == "" {
		return fmt.Errorf("%w: disabled or deprecated deployment template cannot be newly selected", apperrors.ErrInvalidArgument)
	}
	current, err := s.repo.GetService(ctx, applicationID, serviceID)
	if err != nil {
		return err
	}
	if current.DeploymentTemplate == nil || current.DeploymentTemplate.TemplateID != binding.TemplateID || current.DeploymentTemplate.Version != binding.Version {
		return fmt.Errorf("%w: disabled or deprecated deployment template cannot be newly selected", apperrors.ErrInvalidArgument)
	}
	return nil
}

func validateServiceTemplateArtifacts(artifacts map[string]string, containers []domainapp.ServiceContainerInput) error {
	names := make(map[string]bool, len(containers))
	for _, container := range containers {
		names[container.Name] = true
	}
	for _, name := range artifacts {
		if !names[name] {
			return fmt.Errorf("%w: deployment template requires container %q", apperrors.ErrInvalidArgument, name)
		}
	}
	return nil
}

func (s *Service) validateServiceTemplatePackage(ctx context.Context, applicationID, serviceID string, binding domaincatalog.DeploymentTemplateBinding) error {
	if binding.ManifestPackageID == "" {
		if binding.Detached {
			return fmt.Errorf("%w: independent deployment configuration requires a saved manifest package", apperrors.ErrInvalidArgument)
		}
		return nil
	}
	if s.deploymentPackages == nil {
		return fmt.Errorf("%w: deployment configuration reader is unavailable", apperrors.ErrInvalidArgument)
	}
	item, err := s.deploymentPackages.Get(ctx, binding.ManifestPackageID)
	if err != nil {
		return err
	}
	if serviceID == "" || item.ApplicationID != applicationID || item.ServiceID != serviceID {
		return fmt.Errorf("%w: manifest package must belong to this service", apperrors.ErrAccessDenied)
	}
	return nil
}
