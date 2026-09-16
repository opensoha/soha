package systemintegration

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domain "github.com/opensoha/soha/internal/domain/systemintegration"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/netguard"
)

// Called after application/repository authorization, before an execution exists.
func (s *Service) PrepareExternalPipeline(ctx context.Context, repository domainapp.SourceRepository, configuration sohaapi.ExternalPipelineConfiguration, sourceCommit string) (sohaapi.ExternalPipelineExecutionSpec, error) {
	spec := sohaapi.ExternalPipelineExecutionSpec{Configuration: configuration, RepositoryID: repository.ID, RepositoryURL: repository.URL, SourceConnectionID: repository.SourceConnectionID, ProviderProjectID: repository.ProviderRepositoryID, SourceCommit: sourceCommit}
	project, err := strconv.ParseInt(spec.ProviderProjectID, 10, 64)
	if err != nil || project <= 0 || repository.Provider != domain.ProviderGitLab || !validAnalysisCommit(sourceCommit) || configuration.Provider != sohaapi.ExternalPipelineGitLab {
		return spec, fmt.Errorf("%w: external pipeline requires a registered GitLab project and frozen source commit", apperrors.ErrInvalidArgument)
	}
	item, err := s.repo.Get(ctx, spec.SourceConnectionID)
	if err != nil {
		return spec, err
	}
	spec.ConnectionEndpoint = strings.TrimRight(configurationMap(item.Configuration)["base_url"], "/")
	provider, closeClient, err := s.ExternalPipelineProvider(ctx, spec)
	if err != nil {
		return spec, err
	}
	defer closeClient()
	spec.PipelineCommit, err = provider.ResolvePipelineTag(ctx, spec.ProviderProjectID, configuration.PipelineTag)
	if err == nil && !validAnalysisCommit(spec.PipelineCommit) {
		err = fmt.Errorf("%w: invalid CI definition commit", apperrors.ErrInvalidArgument)
	}
	return spec, err
}

// Recreate the authenticated adapter per action so connection revocation, OAuth
// rotation and endpoint policy are checked without persisting plaintext tokens.
func (s *Service) ExternalPipelineProvider(ctx context.Context, spec sohaapi.ExternalPipelineExecutionSpec) (domainbuild.PipelineProvider, func(), error) {
	item, err := s.repo.Get(ctx, spec.SourceConnectionID)
	if err != nil {
		return nil, nil, err
	}
	configuration := configurationMap(item.Configuration)
	if !item.Enabled || item.Category != domain.CategorySourceControl || item.ProviderType != domain.ProviderGitLab || spec.Configuration.Provider != sohaapi.ExternalPipelineGitLab || strings.TrimRight(configuration["base_url"], "/") != spec.ConnectionEndpoint {
		return nil, nil, fmt.Errorf("%w: frozen pipeline connection is unavailable or changed", apperrors.ErrAccessDenied)
	}
	_, base, allowed, err := deliveryGitPolicy(item, spec.RepositoryURL)
	if err != nil {
		return nil, nil, err
	}
	client, err := netguard.PinnedHTTPSClient(ctx, base.Hostname(), sourceGitPort(base), allowed, configuration["git_ca_certificate"])
	if err != nil {
		return nil, nil, err
	}
	provider, err := s.pipelineAdapter(ctx, item, spec, client)
	if err != nil {
		client.CloseIdleConnections()
		return nil, nil, err
	}
	return provider, client.CloseIdleConnections, nil
}

func (s *Service) pipelineAdapter(ctx context.Context, item domain.Integration, spec sohaapi.ExternalPipelineExecutionSpec, client *http.Client) (domainbuild.PipelineProvider, error) {
	credentials, err := s.decryptCredentials(ctx, item.ID)
	if err != nil {
		return nil, err
	}
	item, credentials, err = s.refreshOAuthCredentials(ctx, item, credentials, client)
	if err != nil {
		return nil, err
	}
	factory := s.adapters[item.ProviderType]
	if factory == nil {
		return nil, apperrors.ErrAccessDenied
	}
	adapter, err := factory.Build(item, credentials, client)
	if err != nil {
		return nil, err
	}
	reader, ok := adapter.(domainapp.SourceMetadataReader)
	if !ok {
		return nil, apperrors.ErrAccessDenied
	}
	if err := validateSourceIdentity(ctx, reader, spec.ProviderProjectID, spec.RepositoryURL); err != nil {
		return nil, err
	}
	provider, ok := adapter.(domainbuild.PipelineProvider)
	if !ok {
		return nil, fmt.Errorf("%w: source connection has no pipeline adapter", apperrors.ErrInvalidArgument)
	}
	return provider, nil
}
