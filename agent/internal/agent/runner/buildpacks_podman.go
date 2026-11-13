package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/BurntSushi/toml"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

var buildpacksBuilderUser = regexp.MustCompile(`^[1-9][0-9]{0,8}:[1-9][0-9]{0,8}$`)

func (r *Runner) buildpacksPodman(ctx context.Context, home string, args ...string) (string, error) {
	// Explicit local storage and --remote=false prevent inherited connections to
	// a host daemon. The administrator assigns this store to this runner alone.
	root := r.cfg.Buildpacks.PodmanRoot
	base := []string{"--remote=false", "--root", root, "--runroot", root + "-run", "--cgroup-manager=cgroupfs", "--events-backend=file"}
	return buildpacksCommand(ctx, "", r.buildpacksEnvironment(home), "podman", append(base, args...)...)
}

func (r *Runner) checkBuildpacksPodman(ctx context.Context) (probeErr error) {
	if runtime.GOOS != "linux" || os.Geteuid() != 0 {
		return fmt.Errorf("daemonless Buildpacks requires a dedicated Linux rootful runner")
	}
	if "linux/"+runtime.GOARCH != r.cfg.Buildpacks.Platform && !r.cfg.Buildpacks.AllowPlatformEmulation {
		return fmt.Errorf("buildpacks native platform mismatch")
	}
	home, err := os.MkdirTemp("", "soha-buildpacks-probe-")
	if err != nil {
		return err
	}
	defer func() { probeErr = errors.Join(probeErr, os.RemoveAll(home)) }()
	version, err := r.buildpacksPodman(ctx, home, "version", "--format", "{{.Client.Version}}")
	if err != nil || strings.TrimSpace(version) != cfgpkg.BuildpacksPodmanVersion {
		return fmt.Errorf("podman version mismatch")
	}
	for _, image := range []string{r.cfg.Buildpacks.BuilderImage, r.cfg.Buildpacks.RunImage, r.cfg.Buildpacks.LifecycleImage} {
		output, err := r.buildpacksPodman(ctx, home, "image", "inspect", "--format", "{{.Os}}/{{.Architecture}}", image)
		if err != nil || strings.TrimSpace(output) != r.cfg.Buildpacks.Platform {
			return fmt.Errorf("approved Buildpacks image unavailable for this platform")
		}
	}
	_, err = r.buildpacksPodmanUser(ctx, home)
	return err
}

func (r *Runner) buildpacksPodmanUser(ctx context.Context, home string) (string, error) {
	output, err := r.buildpacksPodman(ctx, home, "image", "inspect", "--format", "{{.Config.User}}", r.cfg.Buildpacks.BuilderImage)
	user := strings.TrimSpace(output)
	if err != nil || !buildpacksBuilderUser.MatchString(user) {
		return "", fmt.Errorf("daemonless Buildpacks requires a numeric non-root builder UID:GID")
	}
	return user, nil
}

func (r *Runner) buildpacksPodmanContainers(ctx context.Context) ([]string, error) {
	output, err := r.buildpacksPodman(ctx, "", "container", "ls", "--all", "--quiet", "--no-trunc")
	if err != nil {
		return nil, err
	}
	ids := strings.Fields(output)
	if len(ids) > 32 {
		return nil, fmt.Errorf("unexpected containers in Buildpacks store")
	}
	for _, id := range ids {
		if len(id) != 64 || !imageDigestPattern.MatchString("sha256:"+id) {
			return nil, fmt.Errorf("invalid Buildpacks container identity")
		}
	}
	return ids, nil
}

func (r *Runner) prepareBuildpacksLifecycle(ctx context.Context, home string) error {
	for _, directory := range []string{"layers", "lifecycle", "platform/env"} {
		// #nosec G301 -- Non-root lifecycle containers read these mounts; the host parent is private (0700).
		if err := os.MkdirAll(filepath.Join(home, directory), 0o755); err != nil {
			return err
		}
	}
	data, err := readBuildpacksFile(filepath.Join(home, "build.env"), 3<<20)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		key, value, found := strings.Cut(line, "=")
		if !found || !buildpacksEnvironmentKey.MatchString(key) {
			return fmt.Errorf("invalid Buildpacks platform environment")
		}
		if err := os.WriteFile(filepath.Join(home, "platform", "env", key), []byte(value), 0o600); err != nil {
			return err
		}
	}
	// Extract the frozen lifecycle without executing the builder's own lifecycle.
	id, err := r.buildpacksPodman(ctx, home, "create", "--pull=never", "--image-volume=ignore", "--platform", r.cfg.Buildpacks.Platform, r.cfg.Buildpacks.LifecycleImage)
	id = strings.TrimSpace(id)
	if err != nil || len(id) != 64 || !imageDigestPattern.MatchString("sha256:"+id) {
		return fmt.Errorf("create approved Buildpacks lifecycle container: %w", err)
	}
	if _, err := r.buildpacksPodman(ctx, home, "cp", id+":/cnb/lifecycle/.", filepath.Join(home, "lifecycle")); err != nil {
		return err
	}
	_, err = r.buildpacksPodman(ctx, home, "container", "rm", id)
	return err
}

func (r *Runner) runBuildpacksLifecycle(ctx context.Context, image, root, source string, spec sohaapi.BuildpacksExecutionSpec) (string, error) {
	home := filepath.Join(root, "credentials")
	if err := r.prepareBuildpacksLifecycle(ctx, home); err != nil {
		return "", err
	}
	user, err := r.buildpacksPodmanUser(ctx, home)
	if err != nil {
		return "", err
	}
	uid, gid, _ := strings.Cut(user, ":")
	registryArgs := []string{"-uid", uid, "-gid", gid}
	for _, registry := range r.cfg.Buildpacks.InsecureRegistries {
		registryArgs = append(registryArgs, "-insecure-registry", registry)
	}
	exportArgs := append([]string{"-report", "/report/report.toml"}, registryArgs...)
	if spec.Configuration.ProcessType != "" {
		exportArgs = append(exportArgs, "-process-type", spec.Configuration.ProcessType)
	}
	phases := []struct {
		name     string
		registry bool
		args     []string
	}{
		{"analyzer", true, append(append([]string{"-run-image", spec.Configuration.RunImage}, registryArgs...), image)},
		{"detector", false, []string{"-order", "/cnb/order.toml"}},
		{"restorer", true, registryArgs},
		{"builder", false, nil},
		{"exporter", true, append(exportArgs, image)},
	}
	var logs buildpacksOutput
	for _, phase := range phases {
		args := buildpacksLifecycleArguments(root, source, user, phase.name, phase.registry, spec)
		output, err := r.buildpacksPodman(ctx, home, append(args, phase.args...)...)
		_, _ = fmt.Fprintf(&logs, "[%s]\n%s\n", phase.name, output)
		if err != nil {
			return logs.String(), err
		}
		if phase.name == "analyzer" || phase.name == "detector" || phase.name == "restorer" || phase.name == "builder" {
			if err := validateBuildpacksAnalyzed(filepath.Join(home, "layers", "analyzed.toml"), spec.Configuration.RunImage); err != nil {
				return logs.String(), err
			}
		}
	}
	// The lifecycle recreates /layers/sbom; it must not be a bind-mount root.
	// Every phase has exited before validating and retaining this bounded output.
	sbom := filepath.Join(home, "layers", "sbom")
	if _, err := buildpacksSBOMFiles(sbom); err != nil {
		return logs.String(), err
	}
	return logs.String(), os.CopyFS(filepath.Join(root, "sbom"), os.DirFS(sbom))
}

func validateBuildpacksAnalyzed(path, runImage string) error {
	data, err := readBuildpacksFile(path, 1<<20)
	if err != nil {
		return err
	}
	var analyzed struct {
		RunImage struct {
			Reference string `toml:"reference"`
			Extend    bool   `toml:"extend"`
		} `toml:"run-image"`
	}
	if _, err := toml.Decode(string(data), &analyzed); err != nil {
		return err
	}
	normalize := func(image string) string {
		return strings.TrimPrefix(strings.TrimPrefix(image, "index.docker.io/"), "docker.io/")
	}
	if analyzed.RunImage.Extend || normalize(analyzed.RunImage.Reference) != normalize(runImage) {
		return fmt.Errorf("buildpacks analysis changed the approved run image or requested an unsupported extension")
	}
	return nil
}

func buildpacksLifecycleArguments(root, source, user, phase string, registry bool, spec sohaapi.BuildpacksExecutionSpec) []string {
	home := filepath.Join(root, "credentials")
	// ponytail: one build inherits the dedicated runner's CPU/memory cgroup;
	// provision separate runners for independent budgets, never share this host.
	args := []string{"run", "--rm", "--pull=never", "--image-volume=ignore", "--platform", string(spec.Configuration.Platform), "--cgroups=disabled", "--network=host", "--cap-drop=all", "--security-opt=no-new-privileges", "--user", user,
		"--env", "CNB_PLATFORM_API=0.14", "--env", "HOME=/tmp", "--entrypoint", "/cnb/lifecycle/" + phase,
		"--volume", filepath.Join(home, "layers") + ":/layers:U", "--volume", source + ":/workspace:U"}
	containerImage := spec.Configuration.BuilderImage
	if registry {
		containerImage = spec.LifecycleImage
		args = append(args, "--env", "DOCKER_CONFIG=/registry", "--volume", filepath.Join(home, "config.json")+":/registry/config.json:ro,U")
	} else {
		args = append(args, "--volume", filepath.Join(home, "platform")+":/platform:ro,U", "--volume", filepath.Join(home, "lifecycle")+":/cnb/lifecycle:ro")
	}
	if phase == "exporter" {
		args = append(args, "--volume", filepath.Join(root, "report")+":/report:U")
	}
	return append(args, containerImage)
}
