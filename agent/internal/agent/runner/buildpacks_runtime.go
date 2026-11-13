package runner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

const buildpacksOutputLimit = 1 << 20

// Keep draining process pipes after the limit so verbose builds cannot deadlock.
type buildpacksOutput struct {
	bytes.Buffer
	truncated bool
}

func (b *buildpacksOutput) Write(p []byte) (int, error) {
	n := len(p)
	remaining := buildpacksOutputLimit - b.Len()
	if len(p) > remaining {
		p, b.truncated = p[:remaining], true
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}

func buildpacksCommand(ctx context.Context, dir string, env []string, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	configureCommandCancellation(cmd)
	cmd.Dir, cmd.Env = dir, env
	var stdout, stderr buildpacksOutput
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return stdout.String() + "\n" + stderr.String(), fmt.Errorf("%s failed: %w", name, err)
	}
	if stdout.truncated {
		return "", fmt.Errorf("%s output exceeded the limit", name)
	}
	return stdout.String(), nil
}

func (r *Runner) buildpacksEnvironment(home string) []string {
	return []string{
		"PATH=" + os.Getenv("PATH"), "HOME=" + home,
		// pack/imgutil can retain full temporary images even after a successful build.
		"TMPDIR=" + home,
		"DOCKER_HOST=" + r.cfg.Buildpacks.DockerHost,
		"DOCKER_CONFIG=" + home, "PACK_HOME=" + home,
		"GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TERMINAL_PROMPT=0",
	}
}

func (r *Runner) BuildpacksCapability(ctx context.Context, applicationID string) sohaapi.BuildpacksCapability {
	bp := r.cfg.Buildpacks
	capability := sohaapi.BuildpacksCapability{PackVersion: cfgpkg.BuildpacksPackVersion, ProviderKind: "buildpacks_runner." + r.cfg.AgentID}
	if bp.Runtime == "podman" {
		capability.PackVersion, capability.Runtime, capability.RuntimeVersion = "", "podman", cfgpkg.BuildpacksPodmanVersion
	}
	if !bp.Enabled || !containsString(bp.AllowedApplicationIDs, applicationID) {
		capability.Reason = "buildpacks_not_enabled_for_application"
		return capability
	}
	if r.buildpacksBlocked.Load() {
		capability.Reason = "buildpacks_cleanup_unconfirmed"
		return capability
	}
	ctx, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	if err := r.checkBuildpacksToolchain(ctx); err != nil {
		capability.Reason = "buildpacks_toolchain_unavailable"
		return capability
	}
	capability.Ready, capability.Reason = true, "ready"
	_, sshErr := exec.LookPath("ssh")
	capability.SupportsSSH, capability.SupportsSubmodules = sshErr == nil, true
	capability.TimeoutSeconds = int(min(max(r.cfg.DefaultTimeout/time.Second, 1), 3600))
	capability.Configuration = &sohaapi.BuildpacksConfiguration{BuilderImage: bp.BuilderImage, RunImage: bp.RunImage, Platform: sohaapi.BuildpacksConfigurationPlatform(bp.Platform)}
	capability.LifecycleImage = bp.LifecycleImage
	return capability
}

func (r *Runner) checkBuildpacksToolchain(ctx context.Context) (probeErr error) {
	if r.cfg.Buildpacks.Runtime == "podman" {
		return r.checkBuildpacksPodman(ctx)
	}
	home, err := os.MkdirTemp("", "soha-buildpacks-probe-")
	if err != nil {
		return fmt.Errorf("create Buildpacks probe directory: %w", err)
	}
	defer func() { probeErr = errors.Join(probeErr, os.RemoveAll(home)) }()
	env := r.buildpacksEnvironment(home)
	version, err := buildpacksCommand(ctx, "", env, "pack", "version")
	if err != nil || !strings.HasPrefix(strings.TrimSpace(version), cfgpkg.BuildpacksPackVersion+"+") && strings.TrimSpace(version) != cfgpkg.BuildpacksPackVersion {
		return fmt.Errorf("pack version mismatch")
	}
	if err := r.checkBuildpacksDaemon(ctx); err != nil {
		return err
	}
	for _, image := range []string{r.cfg.Buildpacks.BuilderImage, r.cfg.Buildpacks.RunImage, r.cfg.Buildpacks.LifecycleImage} {
		output, err := buildpacksCommand(ctx, "", env, "docker", "image", "inspect", "--format", "{{.Os}}/{{.Architecture}}", image)
		if err != nil || strings.TrimSpace(output) != r.cfg.Buildpacks.Platform {
			return fmt.Errorf("approved Buildpacks image unavailable for this platform")
		}
	}
	return nil
}

func (r *Runner) checkBuildpacksDaemon(ctx context.Context) error {
	output, err := buildpacksCommand(ctx, "", r.buildpacksEnvironment(""), "docker", "info", "--format", "{{json .}}")
	if err != nil {
		return err
	}
	var info struct{ ID, OSType, Architecture string }
	if json.Unmarshal([]byte(output), &info) != nil || info.ID != r.cfg.Buildpacks.DaemonID || info.OSType != "linux" {
		return fmt.Errorf("dedicated Buildpacks daemon identity mismatch")
	}
	arch := strings.NewReplacer("aarch64", "arm64", "x86_64", "amd64").Replace(info.Architecture)
	if "linux/"+arch != r.cfg.Buildpacks.Platform && !r.cfg.Buildpacks.AllowPlatformEmulation {
		return fmt.Errorf("buildpacks daemon platform mismatch")
	}
	return nil
}

func (r *Runner) buildpacksContainers(ctx context.Context) ([]string, error) {
	if r.cfg.Buildpacks.Runtime == "podman" {
		return r.buildpacksPodmanContainers(ctx)
	}
	if err := r.checkBuildpacksDaemon(ctx); err != nil {
		return nil, err
	}
	output, err := buildpacksCommand(ctx, "", r.buildpacksEnvironment(""), "docker", "container", "ls", "--all", "--quiet", "--no-trunc")
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(output)
	if len(ids) > 32 {
		return nil, fmt.Errorf("unexpected containers in Buildpacks daemon")
	}
	for _, id := range ids {
		if len(id) != 64 || !imageDigestPattern.MatchString("sha256:"+id) {
			return nil, fmt.Errorf("invalid Buildpacks container identity")
		}
	}
	return ids, nil
}

// Called only after an empty dedicated daemon was verified and pack has exited.
func (r *Runner) cleanupBuildpacksContainers(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	ids, err := r.buildpacksContainers(ctx)
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		args := append([]string{"container", "rm", "--force"}, ids...)
		var err error
		if r.cfg.Buildpacks.Runtime == "podman" {
			_, err = r.buildpacksPodman(ctx, "", args...)
		} else {
			_, err = buildpacksCommand(ctx, "", r.buildpacksEnvironment(""), "docker", args...)
		}
		if err != nil {
			return err
		}
	}
	ids, err = r.buildpacksContainers(ctx)
	if err != nil || len(ids) != 0 {
		return fmt.Errorf("buildpacks container stop is unconfirmed")
	}
	return nil
}
