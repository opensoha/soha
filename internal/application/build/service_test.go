package build

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	execution "github.com/opensoha/soha/internal/application/execution"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaincatalog "github.com/opensoha/soha/internal/domain/catalog"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestContainerBuildExecutionCommandsPushAndPersistDigest(t *testing.T) {
	source := &domainapp.BuildSource{
		Type: domainapp.BuildSourceTypeRepoDockerfile,
		Config: map[string]any{
			"builderKind":    "docker",
			"providerKind":   "ci_agent_runner",
			"dockerfilePath": "docker/Dockerfile",
			"contextDir":     "services/api",
		},
	}
	commands := containerBuildExecutionCommands(source, "registry.example/api:v1", map[string]any{"VERSION": "1.0", "quoted": "a'b"})
	if len(commands) != 3 {
		t.Fatalf("commands = %#v, want build, push, digest inspection", commands)
	}
	if !strings.Contains(commands[0], "--build-arg='VERSION=1.0'") || !strings.Contains(commands[0], "--build-arg='quoted=a'\"'\"'b'") {
		t.Fatalf("build args were not shell-quoted: %q", commands[0])
	}
	if !strings.Contains(commands[1], "docker push 'registry.example/api:v1'") {
		t.Fatalf("push command = %q", commands[1])
	}
	if !strings.Contains(commands[2], ".soha-image-digest") {
		t.Fatalf("digest command = %q", commands[2])
	}
}

func TestKanikoBuildExecutionCommandPushesAndWritesDigest(t *testing.T) {
	source := &domainapp.BuildSource{
		Type:   domainapp.BuildSourceTypeRepoDockerfile,
		Config: map[string]any{"builderKind": "kaniko"},
	}
	commands := containerBuildExecutionCommands(source, "registry.example/api:v1", nil)
	if len(commands) != 1 || !strings.Contains(commands[0], "--destination='registry.example/api:v1'") || !strings.Contains(commands[0], "--digest-file=.soha-image-digest") {
		t.Fatalf("kaniko command = %#v", commands)
	}
}

func TestBuildExecutionWorkspaceAlwaysCollectsImageDigest(t *testing.T) {
	app := domainapp.App{ID: "app-1", Key: "api", RepositoryPath: "group/api"}
	workspace, err := (&Service{}).buildExecutionWorkspace(context.Background(), app, &domainapp.BuildSource{Type: domainapp.BuildSourceTypeRepoDockerfile}, structTriggerInput("main"))
	if err != nil {
		t.Fatalf("buildExecutionWorkspace() error = %v", err)
	}
	files, ok := workspace["artifactFiles"].([]string)
	if !ok || len(files) != 1 || files[0] != ".soha-image-digest" {
		t.Fatalf("artifact files = %#v", workspace["artifactFiles"])
	}
}

func TestBuildExecutionWorkspaceResolvesRepositoryBindings(t *testing.T) {
	app := domainapp.App{ID: "app-1", Key: "api", DefaultBranch: "main"}
	service := &Service{apps: buildAppFake{repositories: map[string]domainapp.SourceRepository{
		"repo-api": {ID: "repo-api", URL: "https://git.example/api.git", Path: "team/api", DefaultBranch: "main", ApplicationIDs: []string{"app-1"}},
		"repo-lib": {ID: "repo-lib", URL: "https://git.example/lib.git", Path: "team/lib", DefaultBranch: "develop", ApplicationIDs: []string{"app-1"}},
	}}}
	source := &domainapp.BuildSource{Type: domainapp.BuildSourceTypeRepoDockerfile, Config: map[string]any{
		"repositoryBindings": []any{
			map[string]any{"repositoryId": "repo-api", "allowCommitSelection": true},
			map[string]any{"repositoryId": "repo-lib", "checkoutPath": "shared/lib", "submodules": true},
		},
	}}
	workspace, err := service.buildExecutionWorkspace(context.Background(), app, source, domainbuild.TriggerInput{
		RefType: "branch",
		RefName: "main",
		RepositoryRefs: []domainbuild.RepositoryRef{
			{RepositoryID: "repo-api", RefType: "commit", RefName: "abc123"},
			{RepositoryID: "repo-lib", RefType: "tag", RefName: "v1.0.0"},
		},
	})
	if err != nil {
		t.Fatalf("buildExecutionWorkspace() error = %v", err)
	}
	checkouts, ok := workspace["checkouts"].([]map[string]any)
	if !ok || len(checkouts) != 2 {
		t.Fatalf("checkouts = %#v", workspace["checkouts"])
	}
	if checkouts[0]["repositoryURL"] != "https://git.example/api.git" || checkouts[0]["refType"] != "commit" || checkouts[0]["refName"] != "abc123" {
		t.Fatalf("primary checkout = %#v", checkouts[0])
	}
	if checkouts[1]["checkoutPath"] != "shared/lib" || checkouts[1]["submodules"] != true || checkouts[1]["refName"] != "v1.0.0" {
		t.Fatalf("secondary checkout = %#v", checkouts[1])
	}
}

func TestBuildExecutionWorkspaceUsesRepositoryRefsWithoutBindings(t *testing.T) {
	app := domainapp.App{ID: "app-1", DefaultBranch: "main", RepositoryIDs: []string{"repo-api", "repo-lib"}}
	service := &Service{apps: buildAppFake{repositories: map[string]domainapp.SourceRepository{
		"repo-api": {ID: "repo-api", URL: "https://git.example/api.git", DefaultBranch: "main", ApplicationIDs: []string{"app-1"}},
		"repo-lib": {ID: "repo-lib", URL: "https://git.example/lib.git", DefaultBranch: "main", ApplicationIDs: []string{"app-1"}},
	}}}
	workspace, err := service.buildExecutionWorkspace(context.Background(), app, &domainapp.BuildSource{Type: domainapp.BuildSourceTypeRepoDockerfile}, domainbuild.TriggerInput{
		RefType: "branch",
		RefName: "main",
		RepositoryRefs: []domainbuild.RepositoryRef{
			{RepositoryID: "repo-api", RefType: "commit", RefName: "abc123"},
			{RepositoryID: "repo-lib", RefType: "tag", RefName: "v1.0.0"},
		},
	})
	if err != nil {
		t.Fatalf("buildExecutionWorkspace() error = %v", err)
	}
	checkouts, _ := workspace["checkouts"].([]map[string]any)
	if len(checkouts) != 2 || checkouts[0]["refName"] != "abc123" || checkouts[1]["refName"] != "v1.0.0" {
		t.Fatalf("checkouts = %#v", workspace["checkouts"])
	}
}

func TestNormalizeCheckoutPathRejectsTraversalSegments(t *testing.T) {
	for _, value := range []string{"../escape", "services/../escape", `services\..\escape`} {
		if _, err := normalizeCheckoutPath(value); err == nil {
			t.Fatalf("normalizeCheckoutPath(%q) error = nil", value)
		}
	}
}

func TestMergeBuildMetadataPreservesNestedWorkspace(t *testing.T) {
	metadata := mergeBuildMetadata(
		map[string]any{"workspace": map[string]any{
			"checkouts": []map[string]any{{"repositoryId": "repo-api"}, {"repositoryId": "repo-lib"}},
		}},
		map[string]any{"workspace": map[string]any{
			"artifactFiles": []string{".soha-image-digest"},
		}},
	)
	workspace := metadataMap(metadata, "workspace")
	checkouts, _ := workspace["checkouts"].([]map[string]any)
	if len(checkouts) != 2 || checkouts[1]["repositoryId"] != "repo-lib" {
		t.Fatalf("checkouts = %#v", workspace["checkouts"])
	}
	artifacts, _ := workspace["artifactFiles"].([]string)
	if len(artifacts) != 1 || artifacts[0] != ".soha-image-digest" {
		t.Fatalf("artifact files = %#v", workspace["artifactFiles"])
	}
}

type buildRepoFake struct{ record domainbuild.Record }

func (r *buildRepoFake) List(context.Context, domainbuild.Filter) ([]domainbuild.Record, error) {
	return []domainbuild.Record{r.record}, nil
}
func (r *buildRepoFake) Get(context.Context, string) (domainbuild.Record, error) {
	return r.record, nil
}
func (r *buildRepoFake) GetByExecutionTaskID(context.Context, string) (domainbuild.Record, error) {
	return r.record, nil
}
func (r *buildRepoFake) Create(_ context.Context, _ domainbuild.TriggerInput, metadata map[string]any) (domainbuild.Record, error) {
	r.record = domainbuild.Record{ID: "build-1", ApplicationID: "app-1", Status: "queued", Metadata: metadata}
	return r.record, nil
}
func (r *buildRepoFake) Update(_ context.Context, item domainbuild.Record) (domainbuild.Record, error) {
	r.record = item
	return item, nil
}

type buildAppFake struct {
	app          domainapp.App
	repositories map[string]domainapp.SourceRepository
	service      domainapp.Service
}

func (r buildAppFake) GetService(context.Context, string, string) (domainapp.Service, error) {
	return r.service, nil
}

func (r buildAppFake) Get(context.Context, string) (domainapp.App, error) { return r.app, nil }
func (r buildAppFake) GetRepository(_ context.Context, id string) (domainapp.SourceRepository, error) {
	if item, ok := r.repositories[id]; ok {
		return item, nil
	}
	return domainapp.SourceRepository{}, fmt.Errorf("repository %s not found", id)
}

type executionFake struct{}

func (executionFake) StartBuildExecution(context.Context, execution.BuildPlan) (domaindelivery.ReleaseBundle, domaindelivery.ExecutionTask, error) {
	return domaindelivery.ReleaseBundle{ID: "bundle-1"}, domaindelivery.ExecutionTask{ID: "task-1", ProviderKind: "ci_agent_runner", Status: "queued", Result: map[string]any{}}, nil
}

func TestTriggerLinksBuildToQueuedExecutionTask(t *testing.T) {
	repo := &buildRepoFake{}
	service := New(repo, buildAppFake{app: domainapp.App{ID: "app-1", Name: "api", DefaultBranch: "main", DefaultTag: "v1", BuildImage: "registry.example/api"}}, nil, executionFake{}, nil, nil, nil, nil)
	record, err := service.Trigger(context.Background(), domainidentity.Principal{}, domainbuild.TriggerInput{ApplicationID: "app-1", RefType: "branch", RefName: "main"})
	if err != nil {
		t.Fatalf("Trigger() error = %v", err)
	}
	if record.Status != "queued" || record.Metadata["executionTaskId"] != "task-1" || record.Metadata["releaseBundleId"] != "bundle-1" {
		t.Fatalf("record = %#v", record)
	}
}

func TestWorkflowBuildPersistsParentBeforeExecution(t *testing.T) {
	repo := &buildRepoFake{}
	service := New(repo, buildAppFake{app: domainapp.App{ID: "app-1", DefaultTag: "v1", BuildImage: "registry.example/api"}}, nil, executionFake{}, nil, nil, nil, nil)
	record, err := service.Trigger(context.Background(), domainidentity.Principal{}, domainbuild.TriggerInput{ApplicationID: "app-1", RefType: "branch", RefName: "main", TriggeredByWorkflowRunID: "workflow-1"})
	if err != nil || record.Metadata["workflowRunId"] != "workflow-1" {
		t.Fatalf("workflow association missing: %+v %v", record, err)
	}
}

func TestExecuteDoesNotForgeCompletedBuild(t *testing.T) {
	repo := &buildRepoFake{}
	service := New(repo, buildAppFake{app: domainapp.App{ID: "app-1", Name: "api", DefaultBranch: "main", DefaultTag: "v1", BuildImage: "registry.example/api"}}, nil, executionFake{}, nil, nil, nil, nil)
	record, err := service.Execute(context.Background(), domainidentity.Principal{}, domainbuild.TriggerInput{ApplicationID: "app-1", RefType: "branch", RefName: "main"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if record.Status != "queued" || record.FinishedAt != nil {
		t.Fatalf("workflow execute forged completion: %#v", record)
	}
}

func TestServiceBuildChecksBoundSourceAndContainerOutputBeforeCreatingRecords(t *testing.T) {
	for _, wrong := range []string{"", "application", "source", "output"} {
		t.Run(wrong, func(t *testing.T) {
			source := domainapp.BuildSource{ID: "source", BuildImage: "registry.example/api"}
			component := domainapp.Service{ID: "api", ApplicationID: "app-1", BuildSourceID: "source", Containers: []domainapp.ServiceContainer{{Name: "main", ImageRepository: "registry.example/api"}}}
			switch wrong {
			case "application":
				component.ApplicationID = "other"
			case "source":
				component.BuildSourceID = "other"
			case "output":
				component.Containers[0].ImageRepository = "registry.example/other"
			}
			repo := &buildRepoFake{}
			s := New(repo, buildAppFake{app: domainapp.App{ID: "app-1", DefaultTag: "v1", BuildSources: []domainapp.BuildSource{source}}, service: component}, nil, executionFake{}, nil, nil, nil, nil)
			_, err := s.Trigger(context.Background(), domainidentity.Principal{}, domainbuild.TriggerInput{ApplicationID: "app-1", ServiceID: "api", BuildSourceID: "source", RefName: "main"})
			if (err == nil) != (wrong == "") || (repo.record.ID != "") != (wrong == "") {
				t.Fatalf("service build validation = %v, record %s", err, repo.record.ID)
			}
		})
	}
}

func structTriggerInput(ref string) domainbuild.TriggerInput {
	return domainbuild.TriggerInput{RefType: "branch", RefName: ref}
}

type buildTemplateVersionFake struct{ requested int64 }

func (f *buildTemplateVersionFake) GetBuildTemplateVersion(_ context.Context, id string, version int64) (domaincatalog.BuildTemplate, error) {
	f.requested = version
	if version != 2 {
		return domaincatalog.BuildTemplate{}, apperrors.ErrNotFound
	}
	return domaincatalog.BuildTemplate{ID: id, PublishedVersion: 2, BuildCommands: []string{"echo pinned"}, ContentDigest: "sha256:pinned"}, nil
}

func TestTriggerRequiresPinnedBuildTemplateBeforeCreatingExecution(t *testing.T) {
	for _, version := range []any{nil, 1, 1.5, "2", 2} {
		t.Run(fmt.Sprint(version), func(t *testing.T) {
			config := map[string]any{"buildTemplateId": "template-1"}
			if version != nil {
				config["buildTemplateVersion"] = version
			}
			app := domainapp.App{ID: "app-1", Name: "api", BuildSources: []domainapp.BuildSource{{ID: "source-1", IsDefault: true, Type: domainapp.BuildSourceTypePlatformTemplate, Config: config}}}
			repo, templates := &buildRepoFake{}, &buildTemplateVersionFake{}
			service := New(repo, buildAppFake{app: app}, templates, executionFake{}, nil, nil, nil, nil)
			record, err := service.Trigger(context.Background(), domainidentity.Principal{}, domainbuild.TriggerInput{ApplicationID: app.ID, RefName: "main"})
			if version != 2 {
				if err == nil || repo.record.ID != "" {
					t.Fatalf("invalid reference created execution: %#v, %v", repo.record, err)
				}
				if version == 1 && !errors.Is(err, apperrors.ErrNotFound) {
					t.Fatalf("lookup error was swallowed: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if templates.requested != 2 || record.Metadata["buildTemplateVersion"] != int64(2) || record.Metadata["buildTemplateContentDigest"] != "sha256:pinned" {
				t.Fatalf("unpinned build: %#v", record.Metadata)
			}
		})
	}
}
