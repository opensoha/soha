package build

import (
	"context"
	"errors"
	"strings"
	"testing"

	appexecution "github.com/opensoha/soha/internal/application/execution"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type buildRefFake struct {
	calls  []domainbuild.RepositoryRef
	commit string
}

func (r *buildRefFake) ResolveRepositoryCommit(_ context.Context, id, kind, name string) (string, error) {
	r.calls = append(r.calls, domainbuild.RepositoryRef{RepositoryID: id, RefType: kind, RefName: name})
	return r.commit, nil
}

type frozenExecutionFake struct{ plan appexecution.BuildPlan }

func (e *frozenExecutionFake) StartBuildExecution(_ context.Context, plan appexecution.BuildPlan) (domaindelivery.ReleaseBundle, domaindelivery.ExecutionTask, error) {
	e.plan = plan
	return executionFake{}.StartBuildExecution(context.Background(), plan)
}

func TestFrozenBuildPinsRepositoriesAndSurvivesSourceEdits(t *testing.T) {
	config := map[string]any{"contextDir": "services/api", "buildArgs": map[string]any{"MODE": "release"}, "providerKind": "k8s_job_runner", "repositoryBindings": []any{
		map[string]any{"repositoryId": "api", "checkoutPath": "", "defaultBranch": "main", "allowCommitSelection": false},
		map[string]any{"repositoryId": "lib", "checkoutPath": "lib", "defaultBranch": "stable", "allowCommitSelection": true},
	}}
	app := buildAppFake{app: domainapp.App{ID: "app", Key: "app", DefaultBranch: "wrong-global-default", DefaultTag: "v1", BuildSources: []domainapp.BuildSource{{ID: "source", Type: domainapp.BuildSourceTypeRepoDockerfile, BuildImage: "registry.example/api", Config: config}}},
		service: domainapp.Service{ID: "service", ApplicationID: "app", BuildSourceID: "source", Containers: []domainapp.ServiceContainer{{Name: "api", ImageRepository: "registry.example/api"}}},
		repositories: map[string]domainapp.SourceRepository{
			"api": {ID: "api", URL: "https://git.example/api.git", ApplicationIDs: []string{"app"}},
			"lib": {ID: "lib", URL: "https://git.example/lib.git", ApplicationIDs: []string{"app"}},
		}}
	refs, runner, repo := &buildRefFake{commit: strings.Repeat("a", 40)}, &frozenExecutionFake{}, &buildRepoFake{}
	s := New(repo, app, nil, runner, nil, nil, nil, nil)
	s.SetRepositoryRefResolver(refs)
	input := domainbuild.TriggerInput{ApplicationID: "app", ServiceID: "service", ApplicationEnvironmentID: "dev", BuildSourceID: "source", RefType: "branch", RepositoryRefs: []domainbuild.RepositoryRef{{RepositoryID: "lib", RefType: "tag", RefName: "v2"}}}
	frozen, err := s.PrepareDeliveryBuild(context.Background(), domainidentity.Principal{}, input)
	if err != nil {
		t.Fatal(err)
	}
	if repo.record.ID != "" || runner.plan.ApplicationID != "" {
		t.Fatal("freeze started a build")
	}
	if len(refs.calls) != 2 || refs.calls[0].RefName != "main" || refs.calls[1].RefType != "tag" || frozen.Input.ResolvedCommit != refs.commit {
		t.Fatalf("refs: %+v", refs.calls)
	}
	runtime := metadataMap(frozen.Metadata, "runtime")
	if frozen.Input.BuildArgs["MODE"] != "release" || runtime["image"] != "gcr.io/kaniko-project/executor:v1.23.2-debug" || runtime["checkoutImage"] != "alpine/git:2.47.2" || metadataString(metadataMap(frozen.Metadata, "workspace"), "commandDir") != "" {
		t.Fatal("frozen defaults were lost", frozen.Metadata)
	}
	invalid := input
	invalid.BuildArgs = map[string]any{"UNKNOWN": "override"}
	if _, err := s.PrepareDeliveryBuild(context.Background(), domainidentity.Principal{}, invalid); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatal("undeclared build arg accepted", err)
	}
	input.ApplicationEnvironmentID = "prod"
	second, err := s.PrepareDeliveryBuild(context.Background(), domainidentity.Principal{}, input)
	if err != nil || second.Fingerprint != frozen.Fingerprint {
		t.Fatalf("same compile inputs cannot share: %v", err)
	}
	config["contextDir"] = "other"
	refs.commit = strings.Repeat("b", 40)
	changed, err := s.PrepareDeliveryBuild(context.Background(), domainidentity.Principal{}, input)
	if err != nil || changed.Fingerprint == frozen.Fingerprint {
		t.Fatalf("changed input reused build: %v", err)
	}
	verifyFrozenBuildExecution(t, s, frozen, runner, refs)
}

func verifyFrozenBuildExecution(t *testing.T, s *Service, frozen domainbuild.Prepared, runner *frozenExecutionFake, refs *buildRefFake) {
	t.Helper()
	if _, err := s.TriggerFrozen(context.Background(), domainidentity.Principal{}, frozen); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("unscoped frozen trigger accepted", err)
	}
	ctx := domainworkflow.WithNodeExecution(context.Background(), domainworkflow.Run{ID: "run"}, domainworkflow.NodeRun{NodeID: "service:build", TargetID: "service", Stage: "build"})
	if _, err := s.TriggerFrozen(ctx, domainidentity.Principal{}, frozen); err != nil {
		t.Fatal(err)
	}
	commands := metadataStringSlice(runner.plan.Metadata, "commands")
	if len(commands) != 1 || !strings.Contains(commands[0], "services/api") || strings.Contains(commands[0], "other") {
		t.Fatalf("commands drifted: %v", commands)
	}
	workspace := metadataMap(runner.plan.Metadata, "workspace")
	checkout := metadataMap(workspace, "checkout")
	if checkout["refType"] != "commit" || checkout["refName"] != strings.Repeat("a", 40) {
		t.Fatalf("checkout drifted: %+v", checkout)
	}
	if len(refs.calls) != 6 {
		t.Fatal("execution resolved moving refs again")
	}
	frozen.Metadata["commands"] = []string{"changed"}
	if _, err := s.TriggerFrozen(ctx, domainidentity.Principal{}, frozen); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("mutated snapshot accepted", err)
	}
}

func TestDeliveryBuildMissingSourceHasActionablePublicError(t *testing.T) {
	s := New(&buildRepoFake{}, buildAppFake{app: domainapp.App{ID: "app"}}, nil, nil, nil, nil, nil, nil)
	_, err := s.PrepareDeliveryBuild(context.Background(), domainidentity.Principal{}, domainbuild.TriggerInput{ApplicationID: "app", ServiceID: "service"})
	var business *apperrors.BusinessError
	if !errors.Is(err, apperrors.ErrInvalidArgument) || !errors.As(err, &business) || business.Code() != "delivery_source_missing" || !strings.Contains(business.Message("zh"), "关联源码仓库") {
		t.Fatalf("missing source lost actionable reason: %v", err)
	}
}

func TestBatchRefFreezeReusesCommitButRechecksRepositoryScope(t *testing.T) {
	app := buildAppFake{app: domainapp.App{ID: "app", Key: "app", BuildSources: []domainapp.BuildSource{{ID: "source", Type: domainapp.BuildSourceTypeRepoDockerfile, BuildImage: "registry.example/api", Config: map[string]any{"repositoryId": "repo"}}}},
		service:      domainapp.Service{ID: "service", ApplicationID: "app", BuildSourceID: "source", Containers: []domainapp.ServiceContainer{{Name: "api", ImageRepository: "registry.example/api"}}},
		repositories: map[string]domainapp.SourceRepository{"repo": {ID: "repo", URL: "https://git.example/api.git", ApplicationIDs: []string{"app"}}}}
	refs := &buildRefFake{commit: strings.Repeat("a", 40)}
	s := New(&buildRepoFake{}, app, nil, nil, nil, nil, nil, nil)
	s.SetRepositoryRefResolver(refs)
	input := domainbuild.TriggerInput{ApplicationID: "app", ServiceID: "service", BuildSourceID: "source", RefType: "branch", RefName: "main"}
	ctx := WithRepositoryRefCache(context.Background())
	for i := range 20 {
		input.ApplicationEnvironmentID = []string{"dev", "prod"}[i%2]
		prepared, err := s.PrepareDeliveryBuild(ctx, domainidentity.Principal{}, input)
		if err != nil || prepared.Input.ResolvedCommit != strings.Repeat("a", 40) {
			t.Fatalf("target %d: commit %q, error %v", i, prepared.Input.ResolvedCommit, err)
		}
		refs.commit = strings.Repeat("b", 40)
	}
	if len(refs.calls) != 1 {
		t.Fatalf("same batch resolved a moving ref %d times", len(refs.calls))
	}
	prepared, err := s.PrepareDeliveryBuild(WithRepositoryRefCache(context.Background()), domainidentity.Principal{}, input)
	if err != nil || prepared.Input.ResolvedCommit != refs.commit || len(refs.calls) != 2 {
		t.Fatalf("new batch reused stale commit: %+v, %v", prepared.Input, err)
	}
	app.repositories["repo"] = domainapp.SourceRepository{ID: "repo", URL: "https://git.example/api.git", ApplicationIDs: []string{"other"}}
	if _, err := s.PrepareDeliveryBuild(ctx, domainidentity.Principal{}, input); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("cached ref bypassed repository authorization: %v", err)
	}
}

func TestServiceBuildOutputMismatchHasActionablePublicError(t *testing.T) {
	s := New(&buildRepoFake{}, buildAppFake{service: domainapp.Service{ID: "service", ApplicationID: "app", BuildSourceID: "source", Containers: []domainapp.ServiceContainer{{Name: "main", ImageRepository: "old.example/api"}}}}, nil, nil, nil, nil, nil, nil)
	err := s.validateServiceBuild(context.Background(), &domainbuild.TriggerInput{ApplicationID: "app", ServiceID: "service"}, &domainapp.BuildSource{ID: "source"}, "registry.example/api:v1")
	var business *apperrors.BusinessError
	if !errors.Is(err, apperrors.ErrInvalidArgument) || !errors.As(err, &business) || business.Code() != "delivery_build_output_mismatch" || !strings.Contains(business.Message("zh"), "镜像仓库") {
		t.Fatalf("mismatch reason: %v", err)
	}
}

func TestDeliveryBuildRejectsExternalPipelineWithoutLifecycleSupport(t *testing.T) {
	s := &Service{}
	for _, prepared := range []domainbuild.Prepared{
		{SourceType: string(domainapp.BuildSourceTypeExternalPipeline)},
		{ProviderKind: "external_pipeline_adapter"},
	} {
		_, err := s.freezePrepared(context.Background(), prepared)
		var business *apperrors.BusinessError
		if !errors.Is(err, apperrors.ErrInvalidArgument) || !errors.As(err, &business) || business.Code() != "delivery_external_pipeline_unavailable" {
			t.Fatalf("external pipeline accepted without confirmed cancellation: %v", err)
		}
	}
}
