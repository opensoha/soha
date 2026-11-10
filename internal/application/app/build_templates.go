package app

import (
	"context"
	"fmt"
	"maps"
	"slices"

	domainapp "github.com/opensoha/soha/internal/domain/application"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domainupdate "github.com/opensoha/soha/internal/domain/templateupdate"
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
		if err := validateBuildSourceConfiguration(source); err != nil {
			return err
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
		if err := validateBuildTemplateReplacement(ctx, current, source.ID, id); err != nil {
			return err
		}
		previousVersion := currentBuildTemplateVersion(current, source.ID, id)
		if version == 0 {
			version = previousVersion
		}
		if previousVersion > 0 && version != previousVersion && !domainupdate.IsAdoption(ctx, "build", current.ID) {
			return fmt.Errorf("%w: adopt the build template version through a frozen template comparison and configuration Plan", apperrors.ErrConflict)
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

func validateBuildSourceConfiguration(source domainapp.BuildSourceInput) error {
	switch source.Type {
	case domainapp.BuildSourceTypeExternalPipeline:
		_, err := domainapp.ExternalPipelineConfiguration(source.Config)
		return err
	case domainapp.BuildSourceTypeBuildpacks:
		_, err := domainapp.BuildpacksConfiguration(source.Config)
		return err
	default:
		return nil
	}
}

func validateBuildTemplateReplacement(ctx context.Context, current domainapp.App, sourceID, templateID string) error {
	for _, previous := range current.BuildSources {
		if previous.ID == sourceID && previous.Type == domainapp.BuildSourceTypePlatformTemplate {
			oldID, oldVersion, err := domaincatalog.BuildTemplateReference(previous.Config)
			if err == nil && oldVersion > 0 && oldID != templateID && !domainupdate.IsAdoption(ctx, "build", current.ID) {
				return fmt.Errorf("%w: compare and adopt a build template before replacing a fixed reference", apperrors.ErrConflict)
			}
		}
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
