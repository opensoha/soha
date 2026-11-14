package config

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/viper"
)

const BuildpacksPackVersion = "0.40.9"
const BuildpacksPodmanVersion = "5.7.0"

const BuildpacksDefaultCacheMaxBytes int64 = 4 << 30

type BuildpacksConfig struct {
	Enabled                bool     `mapstructure:"enabled"`
	Runtime                string   `mapstructure:"runtime"`
	PodmanRoot             string   `mapstructure:"podman_root"`
	BuilderImage           string   `mapstructure:"builder_image"`
	RunImage               string   `mapstructure:"run_image"`
	LifecycleImage         string   `mapstructure:"lifecycle_image"`
	Platform               string   `mapstructure:"platform"`
	AllowPlatformEmulation bool     `mapstructure:"allow_platform_emulation"`
	DockerHost             string   `mapstructure:"docker_host"`
	DaemonID               string   `mapstructure:"daemon_id"`
	AllowedApplicationIDs  []string `mapstructure:"allowed_application_ids"`
	InsecureRegistries     []string `mapstructure:"insecure_registries"`
	CacheMaxBytes          int64    `mapstructure:"cache_max_bytes"`
}

var pinnedBuildpacksImage = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:/-]*@sha256:[a-f0-9]{64}$`)
var buildpacksRunnerID = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`)

func validateBuildpacksConfig(cfg Config) error {
	cp, bp := cfg.ControlPlane, cfg.ControlPlane.Buildpacks
	if !bp.Enabled {
		return nil
	}
	if !cp.Enabled || cp.MaxConcurrency != 1 || !buildpacksRunnerID.MatchString(cp.AgentID) {
		return fmt.Errorf("buildpacks requires an enabled control plane, a valid agent_id and max_concurrency=1")
	}
	if len(cp.ProviderKinds) != 0 || cp.Docker.Enabled || cp.AgentRuntime.Enabled || cp.Outpost.Enabled || cfg.Kubernetes.Enabled {
		return fmt.Errorf("buildpacks requires a dedicated runner with provider_kinds=[] and other runtimes disabled")
	}
	if bp.Runtime != "" && bp.Runtime != "pack" && bp.Runtime != "podman" {
		return fmt.Errorf("unsupported Buildpacks runtime")
	}
	if bp.Runtime == "podman" && (!filepath.IsAbs(bp.PodmanRoot) || filepath.Clean(bp.PodmanRoot) == "/" || strings.ContainsAny(bp.PodmanRoot, ":,\x00\r\n") || bp.DockerHost != "" || bp.DaemonID != "") {
		return fmt.Errorf("daemonless Buildpacks requires a dedicated absolute podman_root and no Docker endpoint")
	}
	for _, image := range []string{bp.BuilderImage, bp.RunImage, bp.LifecycleImage} {
		if !pinnedBuildpacksImage.MatchString(image) {
			return fmt.Errorf("buildpacks builder, run and lifecycle images must use sha256 digests")
		}
	}
	if bp.Platform != "linux/amd64" && bp.Platform != "linux/arm64" {
		return fmt.Errorf("buildpacks platform must be linux/amd64 or linux/arm64")
	}
	if len(bp.AllowedApplicationIDs) == 0 || bp.Runtime != "podman" && strings.TrimSpace(bp.DaemonID) == "" {
		return fmt.Errorf("buildpacks requires daemon_id and allowed_application_ids")
	}
	if bp.CacheMaxBytes < 0 {
		return fmt.Errorf("buildpacks cache_max_bytes cannot be negative")
	}
	return validateBuildpacksEndpoints(bp)
}

func validateBuildpacksEndpoints(bp BuildpacksConfig) error {
	host, err := url.Parse(bp.DockerHost)
	if err != nil || host.User != nil || host.RawQuery != "" || host.Fragment != "" {
		return fmt.Errorf("buildpacks docker_host is invalid")
	}
	if bp.Runtime != "podman" && (host.Scheme != "unix" || !strings.HasPrefix(host.Path, "/")) && (host.Scheme != "tcp" || host.Host == "") {
		return fmt.Errorf("buildpacks docker_host must identify its dedicated unix or tcp daemon")
	}
	for _, app := range bp.AllowedApplicationIDs {
		if !buildpacksRunnerID.MatchString(app) {
			return fmt.Errorf("buildpacks allowed_application_ids must contain explicit application identifiers")
		}
	}
	for _, registry := range bp.InsecureRegistries {
		u, err := url.Parse("http://" + registry)
		if err != nil || u.Host != registry || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" {
			return fmt.Errorf("buildpacks insecure_registries must contain registry hosts only")
		}
	}
	return nil
}

func setBuildpacksDefaults(v *viper.Viper) {
	v.SetDefault("control_plane.buildpacks.enabled", false)
	v.SetDefault("control_plane.buildpacks.runtime", "pack")
	v.SetDefault("control_plane.buildpacks.podman_root", "")
	v.SetDefault("control_plane.buildpacks.allow_platform_emulation", false)
	for _, key := range []string{"builder_image", "run_image", "lifecycle_image", "platform", "docker_host", "daemon_id"} {
		v.SetDefault("control_plane.buildpacks."+key, "")
	}
	v.SetDefault("control_plane.buildpacks.allowed_application_ids", []string{})
	v.SetDefault("control_plane.buildpacks.insecure_registries", []string{})
	v.SetDefault("control_plane.buildpacks.cache_max_bytes", BuildpacksDefaultCacheMaxBytes)
}
