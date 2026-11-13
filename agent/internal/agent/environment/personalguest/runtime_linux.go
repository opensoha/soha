//go:build linux

package personalguest

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	env "github.com/opensoha/soha-contracts/agent/environment"
	guest "github.com/opensoha/soha-contracts/agent/guest"
	"github.com/opensoha/soha-contracts/streamlimit"
)

func loadRuntimePlan(path string) (guest.ToolsPlan, error) {
	var plan guest.ToolsPlan
	if filepath.Dir(path) != "/etc/soha/personal-guest-tools" || rootDataPath(path, false) != nil {
		return plan, errors.New("untrusted runtime tools file")
	}
	root, err := os.OpenRoot("/etc/soha/personal-guest-tools")
	if err != nil {
		return plan, err
	}
	defer func() { _ = root.Close() }()
	file, err := root.Open(filepath.Base(path))
	if err != nil {
		return plan, err
	}
	defer func() { _ = file.Close() }()
	raw, err := io.ReadAll(io.LimitReader(file, 8193))
	if err != nil || len(raw) > 8192 || decodeDataJSON(raw, &plan) != nil || plan.Validate(plan.Binding.IssuedAt) != nil || filepath.Base(path) != plan.Binding.DataRef+".json" {
		return plan, errors.New("invalid fixed runtime tools manifest")
	}
	self, err := os.Executable()
	if err != nil || rootDataPath(self, false) != nil {
		return plan, errors.New("untrusted runtime artifact")
	}
	binary, err := os.Open("/proc/self/exe")
	if err != nil {
		return plan, err
	}
	defer func() { _ = binary.Close() }()
	hash := sha256.New()
	if _, err := io.Copy(hash, io.LimitReader(binary, 64<<20+1)); err != nil || fmt.Sprintf("%x", hash.Sum(nil)) != plan.ArtifactSHA256 {
		return plan, errors.New("runtime artifact digest mismatch")
	}
	return plan, nil
}

func (b *runtimeBroker) markDispatched(op guest.RuntimeOperation, digest string) error {
	root, err := os.OpenRoot(filepath.Dir(b.cfg.StateFile))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	file, err := root.OpenFile("runtime-dispatched-"+op.OperationID+".json", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return errors.New("original runtime dispatch already exists")
	}
	defer func() { _ = file.Close() }()
	if err := json.NewEncoder(file).Encode(map[string]string{"operationId": op.OperationID, "digest": digest}); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	return directory.Sync()
}

func (b *runtimeBroker) execute(ctx context.Context, op guest.RuntimeOperation) (guest.RuntimeResult, error) {
	result := guest.RuntimeResult{SchemaVersion: guest.RuntimeResultVersion, OperationID: op.OperationID, BindingID: op.Binding.ID}
	if _, err := b.usage(ctx); err != nil {
		return result, err
	}
	var err error
	switch op.Kind {
	case "environment":
		switch op.Request.Action {
		case "inspect", "acquire":
			result.Proof, err = b.probe(ctx, op)
		case "execute":
			result.Environment.Output, result.Environment.ExitCode, err = b.command(ctx, op)
		case "release":
			result.Environment = env.Result{SchemaVersion: env.ResultVersion, LeaseID: op.Request.Lease.ID, RunID: op.Request.Lease.RunID, State: "released", ObservedAt: time.Now().UTC()}
			return result, nil
		default:
			err = errors.New("unsupported VM runtime lifecycle")
		}
	case "file_read", "file_write":
		result.File, err = b.file(ctx, op)
	case "docker":
		result.Environment.Output, result.Environment.ExitCode, err = b.docker(ctx, op)
	case "browser":
		result.Browser, err = b.browser(ctx, op)
	default:
		err = errors.New("unsupported runtime operation")
	}
	if err != nil {
		return result, err
	}
	usage, err := b.usage(ctx)
	if err != nil {
		return result, err
	}
	now := time.Now().UTC()
	result.Environment.SchemaVersion, result.Environment.LeaseID, result.Environment.RunID, result.Environment.TaskID, result.Environment.State, result.Environment.ObservedAt = env.ResultVersion, op.Request.Lease.ID, op.Request.Lease.RunID, op.Request.TaskID, "active", now
	result.Environment.Snapshot = &env.Snapshot{SchemaVersion: env.SnapshotVersion, LeaseID: op.Request.Lease.ID, RunID: op.Request.Lease.RunID, State: "active", ResourceUsage: usage, ObservedAt: now}
	return result, result.ValidateFor(op)
}

func sandboxArgs(workspace, taskID string, writable bool) []string {
	bind := "--ro-bind"
	if writable {
		bind = "--bind"
	}
	args := []string{"--unshare-all", "--new-session", "--die-with-parent", "--cap-drop", "ALL", "--clearenv", "--ro-bind", "/usr", "/usr", "--symlink", "usr/bin", "/bin", "--symlink", "usr/sbin", "/sbin", "--symlink", "usr/lib", "/lib", "--symlink", "usr/lib64", "/lib64", "--dir", "/etc", "--ro-bind", "/etc/ssl", "/etc/ssl", "--ro-bind", "/etc/ld.so.cache", "/etc/ld.so.cache", "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--tmpfs", "/run", "--dir", "/home", "--dir", "/home/soha", "--setenv", "HOME", "/home/soha", "--setenv", "PATH", "/usr/bin:/bin", "--setenv", "LANG", "C.UTF-8", bind, workspace, "/data"}
	wd := "/data"
	if taskID != "" {
		wd += "/tasks/" + taskID
	}
	return append(args, "--chdir", wd, "--")
}

func (b *runtimeBroker) command(ctx context.Context, op guest.RuntimeOperation) (*string, *int, error) {
	if op.Request.TaskID != "" {
		root, err := os.OpenRoot(runtimeWorkspace(b.plan))
		if err != nil {
			return nil, nil, err
		}
		err = root.MkdirAll("tasks/"+op.Request.TaskID, 0700)
		_ = root.Close()
		if err != nil {
			return nil, nil, err
		}
	}
	args := sandboxArgs(runtimeWorkspace(b.plan), op.Request.TaskID, op.Request.Lease.Mode == "restricted-write")
	return runSandboxCommand(ctx, "/usr/bin/bwrap", append(args, op.Request.Argv...)...)
}

func runSandboxCommand(ctx context.Context, tool string, args ...string) (*string, *int, error) {
	output := streamlimit.NewLimitedBuffer(64 << 10)
	command := exec.CommandContext(ctx, tool, args...) //nolint:gosec // Fixed sandbox/CLI boundary; model argv never executes in the host namespace.
	command.Env = []string{"PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	command.Stdout, command.Stderr = output, output
	command.WaitDelay = time.Second
	err := command.Run()
	if ctx.Err() != nil || output.Truncated() || command.ProcessState == nil {
		return nil, nil, errors.New("sandbox process result is unconfirmed")
	}
	if err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) {
			return nil, nil, errors.New("sandbox process did not start")
		}
	}
	code := command.ProcessState.ExitCode()
	if code < 0 || code > 255 {
		return nil, nil, errors.New("sandbox process terminated without an exit receipt")
	}
	text := output.String()
	return &text, &code, nil
}

func (b *runtimeBroker) usage(ctx context.Context) (env.Usage, error) {
	var usage env.Usage
	raw, err := os.ReadFile("/proc/self/cgroup")
	if err != nil {
		return usage, err
	}
	group := strings.TrimSpace(strings.TrimPrefix(string(raw), "0::"))
	if !strings.HasPrefix(group, "/soha.slice/soha-personal.slice/") || strings.Contains(group, "..") {
		return usage, errors.New("unconfirmed runtime aggregate resource scope")
	}
	scope := filepath.Join("/sys/fs/cgroup", group)
	// The slice is the shared aggregate for native, browser and Docker tasks.
	scope = filepath.Dir(scope)
	groupRoot, err := os.OpenRoot(scope)
	if err != nil {
		return usage, err
	}
	defer func() { _ = groupRoot.Close() }()
	if err := b.requireAggregateQuota(groupRoot); err != nil {
		return usage, err
	}
	for name, target := range map[string]*int64{"memory.current": &usage.MemoryBytes, "pids.current": new(int64)} {
		value, err := groupRoot.ReadFile(name)
		if err != nil {
			return usage, err
		}
		count, err := strconv.ParseInt(strings.TrimSpace(string(value)), 10, 64)
		if err != nil || count < 0 {
			return usage, errors.New("invalid runtime cgroup usage")
		}
		*target = count
		if name == "pids.current" {
			usage.Processes = int(count)
		}
	}
	stats, err := groupRoot.ReadFile("cpu.stat")
	if err != nil {
		return usage, err
	}
	for _, line := range strings.Split(string(stats), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "usage_usec" {
			us, err := strconv.ParseInt(fields[1], 10, 64)
			if err != nil || us < 0 || us > int64(^uint64(0)>>1)/1000 {
				return usage, errors.New("invalid runtime CPU usage")
			}
			usage.CPUTimeNanos = us * 1000
		}
	}
	for _, path := range []string{runtimeWorkspace(b.plan), filepath.Join(filepath.Dir(runtimeWorkspace(b.plan)), "browser")} {
		var size int64
		size, err = runtimeDiskUsage(ctx, path)
		if err != nil {
			return usage, err
		}
		usage.DiskBytes += size
	}
	return usage, err
}

func runtimeDiskUsage(ctx context.Context, mount string) (int64, error) {
	seen := map[[2]uint64]bool{}
	var blocks int64
	var count int
	err := filepath.WalkDir(mount, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		count++
		if count > 100000 {
			return errors.New("personal usage observation exceeded its bound")
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return errors.New("personal file allocation is unavailable")
		}
		key := [2]uint64{uint64(stat.Dev), stat.Ino}
		if !seen[key] {
			seen[key] = true
			blocks += stat.Blocks
		}
		return nil
	})
	return blocks * 512, err
}

func (b *runtimeBroker) probe(ctx context.Context, authorized guest.RuntimeOperation) (*guest.RuntimeProof, error) {
	parent := map[string]string{}
	for _, name := range []string{"pid", "mnt", "user", "net", "ipc"} {
		value, err := os.Readlink("/proc/self/ns/" + name)
		if err != nil {
			return nil, err
		}
		parent[name] = value
	}
	op := guest.RuntimeOperation{Request: env.Request{Argv: []string{"/usr/bin/python3", "-c", `import os,json; assert not os.path.exists('/var/lib/soha'); assert not os.path.exists('/home/soha/.soha-personal-guest'); assert not os.path.exists('/run/docker.sock'); assert 'NoNewPrivs:\t1' in open('/proc/self/status').read(); print(json.dumps({n:os.readlink('/proc/self/ns/'+n) for n in ['pid','mnt','user','net','ipc']}))`}}}
	output, code, err := b.command(ctx, op)
	if err != nil || *code != 0 {
		return nil, errors.New("runtime isolation probe failed")
	}
	var child map[string]string
	if json.Unmarshal([]byte(*output), &child) != nil {
		return nil, errors.New("invalid runtime namespace proof")
	}
	for name, value := range parent {
		if child[name] == "" || child[name] == value {
			return nil, errors.New("runtime namespace isolation not enforced")
		}
	}
	mount, code, err := runSandboxCommand(ctx, "/usr/bin/findmnt", "--json", "--output", "UUID,FSTYPE,OPTIONS", "--target", runtimeWorkspace(b.plan))
	if err != nil || *code != 0 {
		return nil, errors.New("current workspace identity unavailable")
	}
	var observed struct {
		Filesystems []struct{ UUID, FSType, Options string }
	}
	if json.Unmarshal([]byte(*mount), &observed) != nil || len(observed.Filesystems) != 1 || observed.Filesystems[0].UUID != b.plan.Binding.DataRef || observed.Filesystems[0].FSType != "xfs" {
		return nil, errors.New("current workspace filesystem identity changed")
	}
	proof := &guest.RuntimeProof{InstanceID: b.instance, ArtifactSHA256: b.plan.ArtifactSHA256, FilesystemID: b.plan.Binding.DataRef, Command: true, CredentialsHidden: true, NamespacesIsolated: true, DockerImage: b.plan.DockerImage}
	if authorized.Request.Lease.Mode != "restricted-write" {
		return proof, nil
	}
	if err := b.probeFiles(ctx); err != nil {
		return nil, err
	}
	proof.FileRead, proof.FileWrite = true, true
	if err := b.probeDocker(ctx, authorized); err != nil {
		return nil, err
	}
	proof.Docker = true
	if b.page == nil {
		page, err := b.startBrowser(b.ctx)
		if err != nil {
			return nil, err
		}
		b.page = page
	}
	if _, err := b.page.frame(ctx, b.instance); err != nil {
		return nil, err
	}
	proof.Browser = true
	return proof, nil
}

func (b *runtimeBroker) requireAggregateQuota(scope *os.Root) error {
	for file, expected := range map[string]int64{"memory.max": b.plan.MemoryBytes, "memory.swap.max": 0, "pids.max": b.plan.PidsLimit} {
		raw, err := scope.ReadFile(file)
		if err != nil {
			return err
		}
		actual, err := strconv.ParseInt(strings.TrimSpace(string(raw)), 10, 64)
		if err != nil || actual != expected {
			return errors.New("aggregate runtime hard quota changed")
		}
	}
	raw, err := scope.ReadFile("cpu.max")
	if err != nil {
		return err
	}
	parts := strings.Fields(string(raw))
	if len(parts) != 2 {
		return errors.New("invalid runtime CPU quota")
	}
	quota, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return errors.New("runtime CPU quota is unlimited")
	}
	period, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || period < 1 || quota < 1 || quota*1000 != period*b.plan.CPUQuotaMilli {
		return errors.New("aggregate runtime CPU quota changed")
	}
	return nil
}
