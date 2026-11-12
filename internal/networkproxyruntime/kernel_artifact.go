package networkproxyruntime

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	domain "github.com/opensoha/soha/internal/domain/networkproxy"
)

const maxKernelBytes int64 = 128 << 20
const kernelProbeTimeout = 3 * time.Second

var kernelVersionPattern = regexp.MustCompile(`^v?[0-9]+(?:\.[0-9]+){1,3}(?:[-+][0-9A-Za-z][0-9A-Za-z.-]{0,32})?$`)
var kernelSourcePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/@+-]{0,255}$`)

// Provenance is an installer-redacted reference, never an authenticated URL.
func validKernelSourceRef(value string) bool {
	if !kernelSourcePattern.MatchString(value) || strings.Contains(value, "://") {
		return false
	}
	if at := strings.IndexByte(value, '@'); at >= 0 {
		return configurationDigestPattern.MatchString(value[at+1:])
	}
	return !strings.Contains(value, ":") || !strings.Contains(value, "/")
}

func openKernelArtifact(path string) (*os.File, error) {
	if !filepath.IsAbs(path) {
		return nil, errors.New("kernel path must be absolute")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Mode().Perm()&0111 == 0 || !approvedBinaryOwner(info) {
		return nil, errors.New("kernel artifact permissions invalid")
	}
	file, err := os.Open(path) // #nosec G304 -- absolute installer artifact; Lstat validates owner/mode and SameFile checks opened identity.
	if err != nil {
		return nil, errors.New("kernel artifact unavailable")
	}
	actual, err := file.Stat()
	if err != nil || !os.SameFile(info, actual) || actual.Size() > maxKernelBytes {
		_ = file.Close()
		return nil, errors.New("kernel artifact identity invalid")
	}
	return file, nil
}

func hashKernelArtifact(path string) (string, error) {
	file, err := openKernelArtifact(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(file, maxKernelBytes+1))
	if err != nil || n > maxKernelBytes {
		return "", errors.New("kernel artifact read failed")
	}
	return fmt.Sprintf("sha256:%x", hash.Sum(nil)), nil
}

type installedKernel struct {
	path string
	hash string
}

func inspectInstalledKernel(config Config) (installedKernel, error) {
	path, err := exec.LookPath(config.EngineBinary)
	if err != nil {
		return installedKernel{}, err
	}
	if config.Engine != domain.EngineExternal {
		// Builtin installations may use a relative path or a package-manager
		// symlink. Validate the canonical executable and execute that same path.
		path, err = filepath.Abs(path)
		if err != nil {
			return installedKernel{}, err
		}
		path, err = filepath.EvalSymlinks(path)
		if err != nil {
			return installedKernel{}, err
		}
	}
	hash, err := hashKernelArtifact(path)
	return installedKernel{path: path, hash: hash}, err
}

func (k installedKernel) verify() error {
	if k.path == "" {
		return nil
	}
	digest, err := hashKernelArtifact(k.path)
	if err != nil || digest != k.hash {
		return errors.New("prepared engine artifact changed")
	}
	return nil
}

func (e *engine) bindInstalledKernel(kernel installedKernel) {
	e.kernel = kernel
	adapter := e.adapter
	if external, ok := adapter.(*externalEngineAdapter); ok {
		adapter = external.engineAdapter
	}
	if v2ray, ok := adapter.(*v2rayEngineAdapter); ok && kernel.path != "" {
		v2ray.config.EngineBinary = kernel.path
	}
}

type boundedKernelOutput struct {
	data     bytes.Buffer
	exceeded bool
}

func (b *boundedKernelOutput) Write(p []byte) (int, error) {
	remaining := 4096 - b.data.Len()
	if len(p) > remaining {
		b.exceeded = true
	}
	if remaining > 0 {
		_, _ = b.data.Write(p[:min(len(p), remaining)])
	}
	return len(p), nil
}

func nativeKernelCommand(ctx context.Context, binary, directory string, args []string, output io.Writer) error {
	ctx, cancel := context.WithTimeout(ctx, kernelProbeTimeout)
	defer cancel()
	command := exec.Command(binary, args...) // #nosec G204 -- installer-selected local artifact; fixed native argv, no shell.
	configureOwnedEngineProcess(command)
	command.WaitDelay = 500 * time.Millisecond
	command.Dir = directory
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C", "HOME=" + directory, "XDG_CONFIG_HOME=" + directory, "XDG_DATA_HOME=" + directory, "XDG_CACHE_HOME=" + directory}
	command.Stdout, command.Stderr = output, io.Discard
	if err := runOwnedCommand(ctx, command); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return errors.New("native kernel command failed")
	}
	return nil
}

func nativeKernelVersion(ctx context.Context, binary, engine, directory string) (string, error) {
	args := []string{"version"}
	if engine == domain.EngineMihomo {
		args = []string{"-v"}
	}
	var output boundedKernelOutput
	if err := nativeKernelCommand(ctx, binary, directory, args, &output); err != nil {
		return "", err
	}
	if output.exceeded {
		return "", errors.New("kernel version output invalid")
	}
	fields := strings.Fields(output.data.String())
	version := ""
	switch engine {
	case domain.EngineMihomo:
		if len(fields) >= 3 && fields[0] == "Mihomo" && fields[1] == "Meta" {
			version = fields[2]
		}
	case domain.EngineSingBox:
		if len(fields) >= 3 && fields[0] == "sing-box" && fields[1] == "version" {
			version = fields[2]
		}
	case domain.EngineV2Ray:
		if len(fields) >= 2 && fields[0] == "V2Ray" {
			version = fields[1]
		}
	}
	if len(version) > 64 || !kernelVersionPattern.MatchString(version) {
		return "", errors.New("kernel version output invalid")
	}
	return version, nil
}

type kernelArtifactMeasurement struct {
	hash, version, sourceRef, path string
	info                           os.FileInfo
}

func (e *engine) measureKernelArtifact(ctx context.Context) {
	e.artifact = nil
	kernel := e.kernel
	if kernel.path == "" {
		var err error
		kernel, err = inspectInstalledKernel(e.config)
		if err != nil {
			return
		}
	}
	path, hash := kernel.path, kernel.hash
	directory, err := os.MkdirTemp("", "soha-kernel-version-*")
	if err != nil {
		return
	}
	defer func(path string) { _ = os.RemoveAll(path) }(directory)
	directory, err = filepath.EvalSymlinks(directory)
	if err != nil {
		return
	}
	version, _ := nativeKernelVersion(ctx, path, e.dialect(), directory)
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	current, err := hashKernelArtifact(path)
	if err != nil || current != hash {
		return
	}
	source := e.config.KernelSourceRef
	if source != "" && !validKernelSourceRef(source) {
		source = ""
	}
	e.artifact = &kernelArtifactMeasurement{hash: hash, version: version, sourceRef: source, path: path, info: info}
}

// Explicit diagnostics re-read artifact bytes. Samples/observations never hash.
func (e *engine) kernelArtifactDiagnostics() []any {
	hash, version, source, reference := "unknown", "unknown", "unknown", ""
	state := "unavailable"
	if measured := e.artifact; measured != nil {
		current, err := inspectInstalledKernel(e.config)
		if err == nil && current.path == measured.path && current.hash == measured.hash {
			hash, source, reference, state = current.hash, "installer-owned-local-binary", measured.sourceRef, "available"
			if measured.version != "" {
				version = measured.version
			}
			if a, ok := e.adapter.(*externalEngineAdapter); ok {
				source = "unknown"
				adapterHash, err := hashKernelArtifact(a.policy.Binary)
				if err == nil && adapterHash == a.policy.SHA256 && current.hash == a.policy.EngineSHA256 {
					source = "approved-external-policy"
				}
			}
		}
	}
	versionState := "unavailable"
	if version != "unknown" {
		versionState = "available"
	}
	sourceState := "unavailable"
	if source != "unknown" {
		sourceState = "available"
	}
	if reference != "" {
		source += "; reference=" + reference
	}
	return []any{map[string]any{"code": "engine_binary_sha256", "state": state, "message": hash}, map[string]any{"code": "engine_binary_version", "state": versionState, "message": version}, map[string]any{"code": "engine_binary_source", "state": sourceState, "message": source}}
}

// The health hot path checks identity/metadata only. Changed metadata triggers a
// bounded re-hash; changed bytes fail closed until the owning lifecycle replaces
// the process. An unavailable legacy measurement remains explicitly unknown.
func (e *engine) verifyKernelArtifactDrift() error {
	m := e.artifact
	if m == nil {
		return nil
	}
	info, err := os.Lstat(m.path)
	if err != nil {
		return errors.New("engine artifact unavailable")
	}
	if os.SameFile(info, m.info) && info.Size() == m.info.Size() && info.ModTime().Equal(m.info.ModTime()) && info.Mode() == m.info.Mode() {
		return nil
	}
	current, err := hashKernelArtifact(m.path)
	if err != nil || current != m.hash {
		return errors.New("engine artifact drifted")
	}
	m.info = info
	return nil
}
