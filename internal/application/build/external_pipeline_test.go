package build

import (
	"context"
	"errors"
	"strings"
	"testing"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type externalPipelinePreflight struct {
	denied   bool
	registry string
}

func (f *externalPipelinePreflight) ValidateBuildImage(_ context.Context, _ domainidentity.Principal, registry, _ string) error {
	f.registry = registry
	if f.denied {
		return apperrors.ErrAccessDenied
	}
	return nil
}

func (*externalPipelinePreflight) PrepareExternalPipeline(_ context.Context, repository domainapp.SourceRepository, configuration sohaapi.ExternalPipelineConfiguration, commit string) (sohaapi.ExternalPipelineExecutionSpec, error) {
	return sohaapi.ExternalPipelineExecutionSpec{Configuration: configuration, SourceConnectionID: "gitlab", RepositoryID: repository.ID, ProviderProjectID: "42", ConnectionEndpoint: "https://gitlab.example", SourceCommit: commit, PipelineCommit: strings.Repeat("b", 40)}, nil
}

func TestFrozenExternalPipelineRechecksItsRegistryAfterConfigurationChanges(t *testing.T) {
	pipeline := map[string]any{"provider": "gitlab", "pipelineTag": "ci-v1", "artifactJob": "publish", "registryId": "frozen-registry"}
	config := map[string]any{"externalPipeline": pipeline, "repositoryBindings": []any{map[string]any{"repositoryId": "repo", "checkoutPath": "", "defaultBranch": "main"}}}
	app := buildAppFake{app: domainapp.App{ID: "app", Key: "app", DefaultTag: "v1", BuildSources: []domainapp.BuildSource{{ID: "source", IsDefault: true, Type: domainapp.BuildSourceTypeExternalPipeline, BuildImage: "registry.example/api", Config: config}}},
		service:      domainapp.Service{ID: "service", ApplicationID: "app", BuildSourceID: "source", Containers: []domainapp.ServiceContainer{{Name: "api", ImageRepository: "registry.example/api"}}},
		repositories: map[string]domainapp.SourceRepository{"repo": {ID: "repo", URL: "https://gitlab.example/team/api.git", ApplicationIDs: []string{"app"}}}}
	runner, preflight := &frozenExecutionFake{}, &externalPipelinePreflight{}
	s := New(&buildRepoFake{}, app, nil, runner, nil, nil, nil, nil)
	s.SetRepositoryRefResolver(&buildRefFake{commit: strings.Repeat("a", 40)})
	s.SetExternalPipeline(preflight, preflight)
	prepared, err := s.PrepareDeliveryBuild(t.Context(), domainidentity.Principal{}, domainbuild.TriggerInput{ApplicationID: "app", ServiceID: "service", BuildSourceID: "source", RefType: "branch", RefName: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if prepared.ProviderKind != domainbuild.ExternalPipelineProvider || prepared.Metadata["commands"] != nil {
		t.Fatal("external pipeline used agent commands")
	}
	defaultSource, err := s.PrepareDeliveryBuild(t.Context(), domainidentity.Principal{}, domainbuild.TriggerInput{ApplicationID: "app", RefName: "main"})
	if err != nil || defaultSource.Input.BuildSourceID != "source" || defaultSource.Metadata["buildSourceId"] != "source" {
		t.Fatalf("default build source was not frozen: %s %v", defaultSource.Input.BuildSourceID, err)
	}
	pipeline["registryId"] = "edited-registry"
	preflight.denied = true
	ctx := domainworkflow.WithNodeExecution(t.Context(), domainworkflow.Run{ID: "run"}, domainworkflow.NodeRun{NodeID: "service:build", TargetID: "service", Stage: "build"})
	if _, err := s.TriggerFrozen(ctx, domainidentity.Principal{}, prepared); !errors.Is(err, apperrors.ErrAccessDenied) || preflight.registry != "frozen-registry" || runner.plan.ApplicationID != "" {
		t.Fatalf("frozen registry permission bypassed: %s %v", preflight.registry, err)
	}
	preflight.denied = false
	if _, err := s.TriggerFrozen(ctx, domainidentity.Principal{}, prepared); err != nil {
		t.Fatal(err)
	}
	if runner.plan.ProviderKind != domainbuild.ExternalPipelineProvider {
		t.Fatal("lost CI provider")
	}
}
