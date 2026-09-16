package build

import (
	"context"
	"encoding/json"
	"fmt"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type ExternalPipelinePreparer interface {
	PrepareExternalPipeline(context.Context, domainapp.SourceRepository, sohaapi.ExternalPipelineConfiguration, string) (sohaapi.ExternalPipelineExecutionSpec, error)
}

type BuildImageRegistry interface {
	ValidateBuildImage(context.Context, domainidentity.Principal, string, string) error
}

func (s *Service) SetExternalPipeline(preparer ExternalPipelinePreparer, registries BuildImageRegistry) {
	s.pipelines, s.registries = preparer, registries
}

func (s *Service) validateExternalPipeline(ctx context.Context, principal domainidentity.Principal, source *domainapp.BuildSource, image string) error {
	if source == nil || source.Type != domainapp.BuildSourceTypeExternalPipeline {
		return nil
	}
	configuration, err := domainapp.ExternalPipelineConfiguration(source.Config)
	if err != nil {
		return err
	}
	if s.pipelines == nil || s.registries == nil {
		return fmt.Errorf("%w: external pipeline connections are unavailable", apperrors.ErrInvalidArgument)
	}
	return s.registries.ValidateBuildImage(ctx, principal, configuration.RegistryID, image)
}

func (s *Service) freezeExternalPipeline(ctx context.Context, prepared *domainbuild.Prepared) error {
	if prepared.SourceType != string(domainapp.BuildSourceTypeExternalPipeline) {
		return nil
	}
	configuration, err := domainapp.ExternalPipelineConfiguration(metadataMap(prepared.Metadata, "buildSourceConfig"))
	if err != nil {
		return err
	}
	if len(prepared.Input.RepositoryRefs) != 1 || s.pipelines == nil {
		return apperrors.ErrInvalidArgument
	}
	repository, err := s.apps.GetRepository(ctx, prepared.Input.RepositoryID)
	if err != nil {
		return err
	}
	spec, err := s.pipelines.PrepareExternalPipeline(ctx, repository, configuration, prepared.Input.ResolvedCommit)
	if err != nil {
		return err
	}
	prepared.ProviderKind, prepared.Metadata["externalPipeline"] = domainbuild.ExternalPipelineProvider, spec
	delete(prepared.Metadata, "commands")
	return nil
}

func (s *Service) authorizeFrozenExternalPipeline(ctx context.Context, principal domainidentity.Principal, prepared domainbuild.Prepared) error {
	if prepared.ProviderKind != domainbuild.ExternalPipelineProvider {
		return nil
	}
	var spec sohaapi.ExternalPipelineExecutionSpec
	data, err := json.Marshal(prepared.Metadata["externalPipeline"])
	if err != nil || json.Unmarshal(data, &spec) != nil || spec.Configuration.RegistryID == "" || s.registries == nil {
		return apperrors.ErrInvalidArgument
	}
	return s.registries.ValidateBuildImage(ctx, principal, spec.Configuration.RegistryID, prepared.ImageRef)
}
