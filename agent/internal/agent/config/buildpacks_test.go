package config

import (
	"strings"
	"testing"
)

func TestBuildpacksRequiresDedicatedPinnedRuntime(t *testing.T) {
	image := "registry.example/builder@sha256:" + strings.Repeat("a", 64)
	valid := Config{ControlPlane: ControlPlaneConfig{Enabled: true, AgentID: "cnb-1", MaxConcurrency: 1, Buildpacks: BuildpacksConfig{Enabled: true, BuilderImage: image, RunImage: image, LifecycleImage: image, Platform: "linux/arm64", DockerHost: "unix:///run/cnb/docker.sock", DaemonID: "dedicated-id", AllowedApplicationIDs: []string{"app-1"}}}}
	if err := validateBuildpacksConfig(valid); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*Config){
		"mutable builder":      func(c *Config) { c.ControlPlane.Buildpacks.BuilderImage = "builder:latest" },
		"business runtime":     func(c *Config) { c.ControlPlane.Docker.Enabled = true },
		"general claim pool":   func(c *Config) { c.ControlPlane.ProviderKinds = []string{"ci_agent_runner"} },
		"shared concurrency":   func(c *Config) { c.ControlPlane.MaxConcurrency = 2 },
		"unknown daemon":       func(c *Config) { c.ControlPlane.Buildpacks.DaemonID = "" },
		"wildcard application": func(c *Config) { c.ControlPlane.Buildpacks.AllowedApplicationIDs = []string{"*"} },
		"negative cache limit": func(c *Config) { c.ControlPlane.Buildpacks.CacheMaxBytes = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := valid
			mutate(&cfg)
			if validateBuildpacksConfig(cfg) == nil {
				t.Fatal("unsafe runtime accepted")
			}
		})
	}
}

func TestBuildpacksDaemonlessStorageBoundary(t *testing.T) {
	image := "registry.example/builder@sha256:" + strings.Repeat("a", 64)
	valid := Config{ControlPlane: ControlPlaneConfig{Enabled: true, AgentID: "cnb-1", MaxConcurrency: 1, Buildpacks: BuildpacksConfig{Enabled: true, Runtime: "podman", PodmanRoot: "/var/lib/soha-cnb", BuilderImage: image, RunImage: image, LifecycleImage: image, Platform: "linux/amd64", AllowedApplicationIDs: []string{"app-1"}}}}
	if err := validateBuildpacksConfig(valid); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{"", ".", "/", "/tmp/store:/host", "/tmp/store\n"} {
		cfg := valid
		cfg.ControlPlane.Buildpacks.PodmanRoot = root
		if validateBuildpacksConfig(cfg) == nil {
			t.Fatalf("unsafe storage root accepted: %q", root)
		}
	}
	valid.ControlPlane.Buildpacks.DockerHost = "unix:///var/run/docker.sock"
	if validateBuildpacksConfig(valid) == nil {
		t.Fatal("daemonless runner accepted a host daemon")
	}
}
