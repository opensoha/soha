package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDockerVolumeReaderRequiresPinnedImageAndStableOwner(t *testing.T) {
	image := "sha256:" + strings.Repeat("a", 64)
	owner := "11111111-1111-4111-8111-111111111111"
	for _, cfg := range []DockerRuntimeConfig{{}, {VolumeReaderImage: image, VolumeReaderOwner: owner}} {
		if err := validateDockerRuntime(cfg); err != nil {
			t.Fatal(err)
		}
	}
	for _, cfg := range []DockerRuntimeConfig{
		{VolumeReaderImage: "reader:latest", VolumeReaderOwner: owner},
		{VolumeReaderImage: image},
		{VolumeReaderOwner: owner},
		{VolumeReaderImage: image, VolumeReaderOwner: "00000000-0000-0000-0000-000000000000"},
		{VolumeReaderImage: "sha256:" + strings.Repeat("A", 64), VolumeReaderOwner: owner},
		{VolumeReaderImage: image, VolumeReaderOwner: "not-an-owner"},
		{VolumeReaderImage: image, VolumeReaderOwner: owner, VolumeReaderCleanupImages: []string{image}},
		{VolumeReaderImage: image, VolumeReaderOwner: owner, VolumeReaderCleanupImages: []string{"old:latest"}},
		{VolumeReaderCleanupImages: []string{image}},
	} {
		if validateDockerRuntime(cfg) == nil {
			t.Fatal("unsafe reader configuration accepted")
		}
	}
}

func TestDockerVolumeReaderLoadsExplicitEnvironmentAndCleanupOnlyYAMLList(t *testing.T) {
	file := filepath.Join(t.TempDir(), "agent.yaml")
	old := "sha256:" + strings.Repeat("b", 64)
	raw := "app:\n  env: development\nhttp:\n  addr: 127.0.0.1:18080\ncontrol_plane:\n  enabled: false\n  session:\n    enabled: false\ndocker_runtime:\n  volume_reader_cleanup_images:\n    - " + old + "\n"
	if err := os.WriteFile(file, []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOHA_AGENT_CONFIG_FILE", file)
	image := "sha256:" + strings.Repeat("a", 64)
	owner := "11111111-1111-4111-8111-111111111111"
	t.Setenv("SOHA_AGENT_DOCKER_RUNTIME_VOLUME_READER_IMAGE", image)
	t.Setenv("SOHA_AGENT_DOCKER_RUNTIME_VOLUME_READER_OWNER", owner)
	cfg, err := Load()
	if err != nil || cfg.DockerRuntime.VolumeReaderImage != image || cfg.DockerRuntime.VolumeReaderOwner != owner || len(cfg.DockerRuntime.VolumeReaderCleanupImages) != 1 || cfg.DockerRuntime.VolumeReaderCleanupImages[0] != old {
		t.Fatal(cfg.DockerRuntime, err)
	}
}
