package manifest

import (
	"context"
	"fmt"

	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainmanifest "github.com/opensoha/soha/internal/domain/manifest"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type DeploymentTemplateReader interface {
	GetDeploymentTemplateVersion(context.Context, domainidentity.Principal, string, int64) (domaincatalog.DeploymentTemplate, error)
}

func (s *DeclarativeService) SetDeploymentTemplateReader(reader DeploymentTemplateReader) {
	s.base.deploymentTemplates = reader
}

func (s *Service) validateTemplateDraftVersion(ctx context.Context, principal domainidentity.Principal, item domainmanifest.Package, input domainmanifest.Input) error {
	if input.ExpectedUpdatedAt != nil {
		if !input.ExpectedUpdatedAt.Equal(item.UpdatedAt) {
			return fmt.Errorf("%w: manifest draft changed; reload before saving", apperrors.ErrConflict)
		}
		return nil
	}
	if item.ServiceID == "" {
		return nil
	}
	service, err := s.applications.GetService(ctx, principal, item.ApplicationID, item.ServiceID)
	if err != nil {
		return err
	}
	reference := service.DeploymentTemplate
	if reference != nil && (reference.ManifestPackageID == "" || reference.ManifestPackageID == item.ID) {
		return fmt.Errorf("%w: expectedUpdatedAt is required for a service template configuration", apperrors.ErrConflict)
	}
	return nil
}

func (s *Service) validateTemplateParameters(ctx context.Context, principal domainidentity.Principal, item domainmanifest.Package, parameters map[string]any) error {
	if len(parameters) == 0 {
		return nil
	}
	if item.ServiceID == "" {
		return fmt.Errorf("%w: template parameters require a service deployment template", apperrors.ErrInvalidArgument)
	}
	service, err := s.applications.GetService(ctx, principal, item.ApplicationID, item.ServiceID)
	if err != nil {
		return err
	}
	reference := service.DeploymentTemplate
	if reference == nil || reference.ManifestPackageID != "" && reference.ManifestPackageID != item.ID {
		return fmt.Errorf("%w: manifest package is not bound to the service template", apperrors.ErrInvalidArgument)
	}
	template, err := s.serviceDeploymentTemplate(ctx, principal, *reference)
	if err != nil {
		return err
	}
	if _, err := template.ParameterSchema.ResolveParameters(template.Defaults, reference.Parameters, parameters, template.EnvironmentOverrides); err != nil {
		return fmt.Errorf("%w: %v", apperrors.ErrInvalidArgument, err)
	}
	return nil
}

func (s *Service) serviceDeploymentTemplate(ctx context.Context, principal domainidentity.Principal, reference domaincatalog.DeploymentTemplateBinding) (domaincatalog.DeploymentTemplate, error) {
	if reference.Detached {
		if reference.DetachedTemplate == nil {
			return domaincatalog.DeploymentTemplate{}, fmt.Errorf("%w: independent deployment configuration is missing its source snapshot", apperrors.ErrConflict)
		}
		return *reference.DetachedTemplate, nil
	}
	if s.deploymentTemplates == nil {
		return domaincatalog.DeploymentTemplate{}, fmt.Errorf("%w: deployment template reader is unavailable", apperrors.ErrInvalidArgument)
	}
	return s.deploymentTemplates.GetDeploymentTemplateVersion(ctx, principal, reference.TemplateID, reference.Version)
}
