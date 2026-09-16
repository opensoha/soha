package build

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainapp "github.com/opensoha/soha/internal/domain/application"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsecret "github.com/opensoha/soha/internal/domain/secret"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type BuildpacksCapabilityReader interface {
	BuildpacksCapability(context.Context, string) (sohaapi.BuildpacksCapability, error)
}

type BuildSecretPinner interface {
	PinReferences(context.Context, domainidentity.Principal, map[string]string, domainsecret.Target) ([]domainsecret.Reference, error)
}

func (s *Service) SetBuildpacksRuntime(reader BuildpacksCapabilityReader, secrets BuildSecretPinner) {
	s.buildpacks, s.secrets = reader, secrets
}

func (s *Service) BuildpacksCapability(ctx context.Context, principal domainidentity.Principal, applicationID string) (sohaapi.BuildpacksCapability, error) {
	if err := s.authorize(ctx, principal, domainaccess.ActionView, applicationID); err != nil {
		return sohaapi.BuildpacksCapability{}, err
	}
	if _, err := s.apps.Get(ctx, applicationID); err != nil {
		return sohaapi.BuildpacksCapability{}, err
	}
	return s.readBuildpacksCapability(ctx, applicationID)
}

func (s *Service) readBuildpacksCapability(ctx context.Context, applicationID string) (sohaapi.BuildpacksCapability, error) {
	if s.buildpacks == nil {
		return sohaapi.BuildpacksCapability{Reason: "buildpacks_runner_not_configured"}, nil
	}
	return s.buildpacks.BuildpacksCapability(ctx, applicationID)
}

func (s *Service) prepareBuildpacks(ctx context.Context, applicationID string, source *domainapp.BuildSource, metadata map[string]any) error {
	configuration, err := domainapp.BuildpacksConfiguration(source.Config)
	if err != nil {
		return err
	}
	capability, err := s.readBuildpacksCapability(ctx, applicationID)
	if err != nil {
		return err
	}
	if !capability.Ready || capability.Configuration == nil || capability.LifecycleImage == "" || !validBuildpacksRuntime(capability) || !strings.HasPrefix(capability.ProviderKind, "buildpacks_runner.") {
		return apperrors.NewBusiness(apperrors.ErrConflict, "buildpacks_runner_unavailable", "A ready dedicated Buildpacks runner is required.", "请先配置可用的专用 Buildpacks 构建节点。")
	}
	if err := validateBuildpacksWorkspace(metadata, capability); err != nil {
		return err
	}
	if capability.TimeoutSeconds < 0 || capability.TimeoutSeconds > 3600 {
		return fmt.Errorf("%w: invalid Buildpacks runner execution budget", apperrors.ErrConflict)
	}
	allowed := capability.Configuration
	if configuration.BuilderImage != allowed.BuilderImage || configuration.RunImage != allowed.RunImage || configuration.Platform != allowed.Platform {
		return apperrors.NewBusiness(apperrors.ErrConflict, "buildpacks_toolchain_unavailable", "The selected immutable Buildpacks toolchain is unavailable.", "当前构建节点不支持所选的固定 Buildpacks 工具链。")
	}
	environment, err := buildpacksEnvironment(metadataMap(source.Config, "variables"), metadataMap(metadata, "variables"))
	if err != nil {
		return err
	}
	if len(metadataMap(metadata, "buildArgs")) > 0 {
		return fmt.Errorf("%w: Buildpacks uses declared build variables, not Docker build arguments", apperrors.ErrInvalidArgument)
	}
	metadata["buildpacks"] = sohaapi.BuildpacksExecutionSpec{Configuration: configuration, LifecycleImage: capability.LifecycleImage,
		PackVersion: capability.PackVersion, Runtime: sohaapi.BuildpacksExecutionSpecRuntime(capability.Runtime), RuntimeVersion: capability.RuntimeVersion,
		ContextDir: path.Clean(firstNonEmptyString(configString(source, "contextDir"), ".")), Environment: environment}
	metadata["buildpacksProviderKind"] = capability.ProviderKind
	metadata["buildTimeoutSeconds"] = capability.TimeoutSeconds
	variables := make(map[string]any, len(environment))
	for key, value := range environment {
		variables[key] = value
	}
	metadata["variables"] = variables
	return nil
}

func validBuildpacksRuntime(capability sohaapi.BuildpacksCapability) bool {
	switch capability.Runtime {
	case "", "pack":
		return capability.PackVersion != ""
	case "podman":
		return capability.RuntimeVersion != "" && capability.PackVersion == ""
	default:
		return false
	}
}

func validateBuildpacksWorkspace(metadata map[string]any, capability sohaapi.BuildpacksCapability) error {
	var workspace struct {
		Checkout struct {
			RepositoryURL string `json:"repositoryURL"`
			Submodules    bool   `json:"submodules"`
		} `json:"checkout"`
		Checkouts []struct {
			RepositoryURL string `json:"repositoryURL"`
			Submodules    bool   `json:"submodules"`
		} `json:"checkouts"`
	}
	data, err := json.Marshal(metadata["workspace"])
	if err != nil || json.Unmarshal(data, &workspace) != nil {
		return fmt.Errorf("%w: invalid Buildpacks workspace", apperrors.ErrInvalidArgument)
	}
	checkouts := append(workspace.Checkouts, workspace.Checkout)
	for _, source := range checkouts {
		if source.RepositoryURL == "" {
			continue
		}
		transport, err := buildpacksRepositoryTransport(source.RepositoryURL)
		if err != nil {
			return err
		}
		if (transport == "ssh" && !capability.SupportsSSH) || (source.Submodules && !capability.SupportsSubmodules) {
			return apperrors.NewBusiness(apperrors.ErrConflict, "buildpacks_source_unavailable", "The Buildpacks runner does not support the selected source transport or submodules.", "当前 Buildpacks 节点不支持所选源码协议或子模块，请升级构建节点。")
		}
	}
	return nil
}

func buildpacksRepositoryTransport(address string) (string, error) {
	if strings.HasPrefix(address, "git@") && !strings.Contains(address, "://") {
		host, repository, found := strings.Cut(strings.TrimPrefix(address, "git@"), ":")
		if found {
			address = "ssh://git@" + host + "/" + repository
		}
	}
	u, err := url.Parse(address)
	if err != nil || u.Host == "" || u.Path == "" || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(address, "\x00\r\n\\") || (u.Scheme != "https" && u.Scheme != "ssh") || (u.User != nil && (u.Scheme != "ssh" || u.User.String() != "git")) {
		return "", fmt.Errorf("%w: Buildpacks source requires a credential-free HTTPS or SSH URL", apperrors.ErrInvalidArgument)
	}
	return u.Scheme, nil
}

var buildpacksEnvironmentKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func buildpacksEnvironment(defaults, overrides map[string]any) (map[string]string, error) {
	values := make(map[string]any, len(defaults))
	for key, value := range defaults {
		values[key] = value
	}
	for key, value := range overrides {
		if _, ok := defaults[key]; !ok {
			return nil, fmt.Errorf("%w: Buildpacks variable is not declared in the source", apperrors.ErrInvalidArgument)
		}
		values[key] = value
	}
	if len(values) > 128 {
		return nil, fmt.Errorf("%w: too many Buildpacks variables", apperrors.ErrInvalidArgument)
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		if !buildpacksEnvironmentKey.MatchString(key) || len(key) > 128 {
			return nil, fmt.Errorf("%w: invalid Buildpacks variable name", apperrors.ErrInvalidArgument)
		}
		switch value.(type) {
		case string, bool, float64, int, int64:
		default:
			return nil, fmt.Errorf("%w: Buildpacks variables must be scalar values", apperrors.ErrInvalidArgument)
		}
		text := fmt.Sprint(value)
		if len(text) > 16384 || strings.ContainsAny(text, "\x00\r\n") {
			return nil, fmt.Errorf("%w: invalid Buildpacks variable value", apperrors.ErrInvalidArgument)
		}
		out[key] = text
	}
	return out, nil
}

func (s *Service) buildSecretContext(ctx context.Context, principal domainidentity.Principal, metadata map[string]any) (context.Context, error) {
	raw := metadata["secretRefs"]
	if raw == nil {
		raw = metadataMap(metadata, "buildSourceConfig")["secretRefs"]
	}
	if raw == nil {
		return ctx, nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return ctx, err
	}
	var refs map[string]string
	if err := json.Unmarshal(encoded, &refs); err != nil {
		return ctx, fmt.Errorf("%w: invalid build secret references", apperrors.ErrInvalidArgument)
	}
	if len(refs) == 0 {
		return ctx, nil
	}
	if s.secrets == nil {
		return ctx, fmt.Errorf("%w: build secret resolver is not configured", apperrors.ErrConflict)
	}
	target := domainsecret.Target{Type: "project", Ref: metadataString(metadata, "applicationId")}
	pinned, err := s.secrets.PinReferences(ctx, principal, refs, target)
	if err != nil {
		return ctx, err
	}
	refs = make(map[string]string, len(pinned))
	for _, ref := range pinned {
		refs[ref.Alias] = ref.URI
	}
	metadata["secretRefs"] = refs
	return domainsecret.WithExecutionContext(ctx, domainsecret.ExecutionContext{References: pinned, Principal: principal, Target: target}), nil
}
