package app

import (
	"context"
	"fmt"
	"maps"
	"slices"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type BuildTemplateReader interface {
	GetBuildTemplate(context.Context, string) (domaincatalog.BuildTemplate, error)
	GetBuildTemplateVersion(context.Context, string, int64) (domaincatalog.BuildTemplate, error)
}

func (s *Service) SetBuildTemplateReader(reader BuildTemplateReader) { s.templates = reader }

func (s *Service) pinBuildTemplates(ctx context.Context, input *domainapp.UpsertInput, current domainapp.App) error {
	input.BuildSources = slices.Clone(input.BuildSources)
	for index, source := range input.BuildSources {
		if source.Type == domainapp.BuildSourceTypeExternalPipeline {
			if _, err := domainapp.ExternalPipelineConfiguration(source.Config); err != nil {
				return err
			}
		}
		if source.Type == domainapp.BuildSourceTypeBuildpacks {
			if _, err := domainapp.BuildpacksConfiguration(source.Config); err != nil {
				return err
			}
		}
		if source.Type != domainapp.BuildSourceTypePlatformTemplate {
			continue
		}
		if s.templates == nil {
			return fmt.Errorf("%w: build template catalog is unavailable", apperrors.ErrInvalidArgument)
		}
		id, version, err := domaincatalog.BuildTemplateReference(source.Config)
		if err != nil {
			return err
		}
		previousVersion := currentBuildTemplateVersion(current, source.ID, id)
		if version == 0 {
			version = previousVersion
		}
		template, err := s.templates.GetBuildTemplate(ctx, id)
		if err != nil {
			return err
		}
		if version == 0 || version != previousVersion {
			if !template.Enabled || template.PublicationState == "deprecated" {
				return fmt.Errorf("%w: build template is disabled or deprecated", apperrors.ErrInvalidArgument)
			}
		}
		if version == 0 {
			version = template.PublishedVersion
		}
		pinned, err := s.templates.GetBuildTemplateVersion(ctx, id, version)
		if err != nil {
			return err
		}
		parameters, _ := source.Config["variables"].(map[string]any)
		if _, err := domaincatalog.BuildVariables(pinned.VariableSchema, pinned.DefaultVariables, parameters, true); err != nil {
			return apperrors.NewBusiness(apperrors.ErrInvalidArgument, "build_template_parameters_invalid", "Check the required build template variables and their types.", "请检查构建模板的必填变量与类型。")
		}
		config := maps.Clone(source.Config)
		config["buildTemplateId"], config["buildTemplateVersion"] = id, version
		input.BuildSources[index].Config = config
	}
	return nil
}

func currentBuildTemplateVersion(current domainapp.App, sourceID, templateID string) int64 {
	for _, source := range current.BuildSources {
		if source.ID != sourceID || source.Type != domainapp.BuildSourceTypePlatformTemplate {
			continue
		}
		id, version, err := domaincatalog.BuildTemplateReference(source.Config)
		if err == nil && id == templateID {
			return version
		}
	}
	return 0
}
