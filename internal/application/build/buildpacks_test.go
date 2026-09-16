package build

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type buildpacksCapabilityFake struct{ capability sohaapi.BuildpacksCapability }

func (f *buildpacksCapabilityFake) BuildpacksCapability(context.Context, string) (sohaapi.BuildpacksCapability, error) {
	return f.capability, nil
}

type buildSecretFake struct{ refs map[string]string }

func (f *buildSecretFake) PinReferences(_ context.Context, _ domainidentity.Principal, refs map[string]string, target domainsecret.Target) ([]domainsecret.Reference, error) {
	if target.Type != "project" || target.Ref != "app" {
		return nil, errors.New("incorrect secret target")
	}
	f.refs = refs
	return []domainsecret.Reference{{Alias: "REGISTRY_AUTH", SecretID: "registry", Version: 7, URI: "soha://secrets/registry/versions/7"}}, nil
}

func TestBuildpacksFreezesToolchainVariablesAndSourceWithoutDockerCommands(t *testing.T) {
	configuration := sohaapi.BuildpacksConfiguration{BuilderImage: "registry.example/builder@sha256:" + strings.Repeat("a", 64), RunImage: "registry.example/run@sha256:" + strings.Repeat("b", 64), Platform: sohaapi.LinuxArm64}
	config := map[string]any{"buildpacks": configuration, "contextDir": "services/api", "variables": map[string]any{"BP_GO_VERSION": "1.26.*"}, "repositoryId": "api", "secretRefs": map[string]string{"REGISTRY_AUTH": "soha://secrets/registry"}}
	app := buildAppFake{app: domainapp.App{ID: "app", Key: "app", DefaultTag: "v1", BuildSources: []domainapp.BuildSource{{ID: "source", Type: domainapp.BuildSourceTypeBuildpacks, BuildImage: "registry.example/api", Config: config}}},
		service:      domainapp.Service{ID: "service", ApplicationID: "app", BuildSourceID: "source", Containers: []domainapp.ServiceContainer{{Name: "api", ImageRepository: "registry.example/api"}}},
		repositories: map[string]domainapp.SourceRepository{"api": {ID: "api", URL: "https://git.example/api.git", ApplicationIDs: []string{"app"}}}}
	runtime := &buildpacksCapabilityFake{capability: sohaapi.BuildpacksCapability{Ready: true, ProviderKind: "buildpacks_runner.dedicated", PackVersion: "0.40.9", TimeoutSeconds: 900, LifecycleImage: "registry.example/lifecycle@sha256:" + strings.Repeat("c", 64), Configuration: &configuration}}
	secrets, repo, execution := &buildSecretFake{}, &buildRepoFake{}, &frozenExecutionFake{}
	s := New(repo, app, nil, execution, nil, nil, nil, nil)
	s.SetRepositoryRefResolver(&buildRefFake{commit: strings.Repeat("d", 40)})
	s.SetBuildpacksRuntime(runtime, secrets)
	input := domainbuild.TriggerInput{ApplicationID: "app", ServiceID: "service", BuildSourceID: "source", RefType: "branch", RefName: "main", Variables: map[string]any{"BP_GO_VERSION": "1.26.6"}}
	prepared, err := s.PrepareDeliveryBuild(context.Background(), domainidentity.Principal{}, input)
	if err != nil {
		t.Fatal(err)
	}
	if repo.record.ID != "" || prepared.ProviderKind != runtime.capability.ProviderKind || prepared.Input.ResolvedCommit != strings.Repeat("d", 40) {
		t.Fatal("preparation executed work or lost identity", prepared.ProviderKind)
	}
	if len(metadataStringSlice(prepared.Metadata, "commands")) != 0 {
		t.Fatal("Buildpacks generated Docker commands")
	}
	encoded, _ := json.Marshal(prepared.Metadata["buildpacks"])
	var spec sohaapi.BuildpacksExecutionSpec
	if err := json.Unmarshal(encoded, &spec); err != nil {
		t.Fatal(err)
	}
	if spec.Configuration != configuration || spec.ContextDir != "services/api" || spec.Environment["BP_GO_VERSION"] != "1.26.6" || spec.PackVersion != "0.40.9" {
		t.Fatalf("CNB inputs drifted: %+v", spec)
	}
	if metadataMap(prepared.Metadata, "secretRefs")["REGISTRY_AUTH"] != "soha://secrets/registry/versions/7" {
		t.Fatal("secret version was not frozen")
	}
	if metadataString(prepared.Metadata, "buildTimeoutSeconds") != "900" {
		t.Fatal("runner execution budget was not frozen")
	}
	assertBuildpacksVariableAndRunnerChanges(t, s, runtime, execution, input, prepared, configuration)
}

func assertBuildpacksVariableAndRunnerChanges(t *testing.T, s *Service, runtime *buildpacksCapabilityFake, execution *frozenExecutionFake, input domainbuild.TriggerInput, prepared domainbuild.Prepared, configuration sohaapi.BuildpacksConfiguration) {
	t.Helper()
	input.Variables["BP_GO_VERSION"] = "1.25.0"
	other, err := s.PrepareDeliveryBuild(context.Background(), domainidentity.Principal{}, input)
	if err != nil || other.Fingerprint == prepared.Fingerprint {
		t.Fatal("different build variables reused a build", err)
	}
	runtime.capability.Ready = false
	if _, err := s.PrepareDeliveryBuild(context.Background(), domainidentity.Principal{}, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("unavailable runner accepted", err)
	}
	runtime.capability.Ready = true
	changed := configuration
	changed.RunImage = "registry.example/run@sha256:" + strings.Repeat("e", 64)
	runtime.capability.Configuration = &changed
	if _, err := s.PrepareDeliveryBuild(context.Background(), domainidentity.Principal{}, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("unsupported toolchain accepted", err)
	}
	runtime.capability.Configuration = &configuration
	if _, err := s.Trigger(context.Background(), domainidentity.Principal{}, input); err != nil {
		t.Fatal(err)
	}
	if metadataString(execution.plan.Metadata, "resolvedCommit") != strings.Repeat("d", 40) {
		t.Fatal("normal trigger left a mutable checkout")
	}
	runtime.capability.Runtime, runtime.capability.RuntimeVersion, runtime.capability.PackVersion = "podman", "5.7.0", ""
	daemonless, err := s.PrepareDeliveryBuild(context.Background(), domainidentity.Principal{}, input)
	if err != nil || daemonless.Fingerprint == other.Fingerprint {
		t.Fatal("daemonless toolchain was unavailable or reused a pack build", err)
	}
	encoded, _ := json.Marshal(daemonless.Metadata["buildpacks"])
	spec := sohaapi.BuildpacksExecutionSpec{}
	if err := json.Unmarshal(encoded, &spec); err != nil || spec.Runtime != "podman" || spec.RuntimeVersion != "5.7.0" || spec.PackVersion != "" {
		t.Fatal("daemonless toolchain was not frozen", err)
	}
	runtime.capability.RuntimeVersion = ""
	if _, err := s.PrepareDeliveryBuild(context.Background(), domainidentity.Principal{}, input); !errors.Is(err, apperrors.ErrConflict) {
		t.Fatal("unversioned daemonless toolchain accepted", err)
	}
}

func TestBuildpacksRejectsConfigurationAndEnvironmentOverrides(t *testing.T) {
	base := map[string]any{"buildpacks": map[string]any{"builderImage": "registry/builder@sha256:" + strings.Repeat("a", 64), "runImage": "registry/run@sha256:" + strings.Repeat("b", 64), "platform": "linux/arm64"}}
	for _, test := range []struct {
		key   string
		value any
	}{{"providerKind", "k8s_job_runner"}, {"builderKind", "kaniko"}, {"contextDir", "../escape"}, {"contextDir", "/tmp"}, {"contextDir", "..\\escape"}, {"commands", []string{"echo escaped"}}} {
		t.Run(test.key+"-"+strings.ReplaceAll(strings.TrimSpace(metadataString(map[string]any{"v": test.value}, "v")), "/", "_"), func(t *testing.T) {
			config := map[string]any{"buildpacks": base["buildpacks"], test.key: test.value}
			if _, err := domainapp.BuildpacksConfiguration(config); !errors.Is(err, apperrors.ErrInvalidArgument) {
				t.Fatal("invalid source accepted", err)
			}
		})
	}
	for _, overrides := range []map[string]any{{"OTHER": "value"}, {"KEY": "value\nINJECT=1"}, {"KEY": []string{"x"}}} {
		if _, err := buildpacksEnvironment(map[string]any{"KEY": "default"}, overrides); !errors.Is(err, apperrors.ErrInvalidArgument) {
			t.Fatal("invalid variable accepted", err)
		}
	}
}

func TestBuildpacksSourceCapabilitiesRejectOlderRunners(t *testing.T) {
	for _, test := range []struct {
		address                           string
		submodules, ssh, modules, allowed bool
	}{
		{"https://git.example/app.git", false, false, false, true},
		{"ssh://git@git.example/app.git", false, false, false, false},
		{"git@git.example:app.git", false, true, false, true},
		{"https://git.example/app.git", true, false, false, false},
		{"https://git.example/app.git", true, false, true, true},
		{"https://user:password@git.example/app.git", false, true, true, false},
		{"ssh://git:password@git.example/app.git", false, true, true, false},
		{"file:///app.git", false, true, true, false},
	} {
		metadata := map[string]any{"workspace": map[string]any{"checkout": map[string]any{"repositoryURL": test.address, "submodules": test.submodules}}}
		err := validateBuildpacksWorkspace(metadata, sohaapi.BuildpacksCapability{SupportsSSH: test.ssh, SupportsSubmodules: test.modules})
		if (err == nil) != test.allowed {
			t.Fatalf("source capability allowed=%v, want %v: %v", err == nil, test.allowed, err)
		}
	}
}
