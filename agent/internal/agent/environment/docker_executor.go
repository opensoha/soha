package environment

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/opensoha/soha/contracts/streamlimit"
)

const dockerEnvironmentLabel = "soha.environment"
const dockerEnvironmentBackend = "personal-docker-v1"
const dockerEnvironmentOutputLimit = 64 * 1024

var digestImagePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9./:_-]*@sha256:[a-f0-9]{64}$`)
var scopeDigestPattern = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)
var dockerContainerIDPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var dockerEnvironmentNamePattern = regexp.MustCompile(`^soha-env-[a-z0-9-]{1,64}$`)
var ErrEnvironmentCommandBusy = errors.New("environment command is already active")

type dockerEnvironmentRecord struct {
	ID     string            `json:"id"`
	Labels map[string]string `json:"labels"`
}

// DockerExecutor implements the existing LocalExecutor. Docker labels retain
// lease identity across Agent restarts; releasing a lease never deletes data.
type DockerExecutor struct {
	mu     sync.Mutex
	image  string
	helper string
	run    func(context.Context, ...string) ([]byte, error)
}

func NewDockerExecutor(image, quotaHelperImage string) (*DockerExecutor, error) {
	if !digestImagePattern.MatchString(image) || !digestImagePattern.MatchString(quotaHelperImage) {
		return nil, errors.New("environment images must use immutable SHA256 digests")
	}
	return &DockerExecutor{image: image, helper: quotaHelperImage, run: runEnvironmentDocker}, nil
}

func runEnvironmentDocker(ctx context.Context, args ...string) ([]byte, error) {
	stdout := streamlimit.NewLimitedBuffer(dockerEnvironmentOutputLimit)
	stderr := streamlimit.NewLimitedBuffer(dockerEnvironmentOutputLimit)
	cmd := exec.CommandContext(ctx, "docker", args...) //nolint:gosec // Fixed Docker CLI templates use validated leases and sandbox argv, without a host shell.
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	output := stdout.String()
	if len(args) > 0 && args[0] == "exec" && stderr.TotalBytes() > 0 {
		output += "\n[stderr]\n" + stderr.String()
	} else if stderr.TotalBytes() > 0 {
		err = errors.New("environment Docker operation reported an inconsistent receipt")
	}
	if err != nil || stdout.Truncated() || stderr.Truncated() || len(output) > dockerEnvironmentOutputLimit {
		// Quota inconsistency is reported on stderr, sometimes with exit code 0.
		// Never turn it into a successful acquisition or expose provider stderr.
		return nil, errors.New("environment Docker command failed or exceeded its output bound")
	}
	return []byte(output), nil
}

func (e *DockerExecutor) Start(ctx context.Context, request LocalRequest) (Handle, error) {
	if err := e.validateRequest(request, time.Now().UTC()); err != nil {
		return Handle{}, err
	}
	// ponytail: serialize local lifecycle work; Docker's owner writer name also
	// fences separate Agent processes. Split into owner locks if throughput needs it.
	e.mu.Lock()
	defer e.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, defaultOperationTimeout)
	defer cancel()
	info, err := e.run(ctx, "info", "--format", "{{.CgroupVersion}}")
	if err != nil || strings.TrimSpace(string(info)) != "2" {
		return Handle{}, errors.New("personal environments require Docker cgroup v2")
	}
	lease := request.Lease
	name, ref := dockerLeaseIdentity(lease)
	record, found, err := e.containerRecord(ctx, name)
	if err != nil {
		return Handle{}, err
	}
	if found {
		handle := Handle{ID: record.ID, Ref: ref, Backend: BackendLocalContainer}
		if err := validateDockerHandle(handle, record); err != nil {
			return Handle{}, err
		}
		if record.Labels[dockerEnvironmentLabel+".image"] != e.image {
			return Handle{}, errors.New("environment image revision changed")
		}
		if err := e.requireRunning(ctx, handle.ID); err != nil {
			return Handle{}, err
		}
		if _, err := e.ensureVolume(ctx, lease, false); err != nil {
			return Handle{}, err
		}
		return handle, nil
	}
	volume, err := e.ensureVolume(ctx, lease, !request.Spec.RequireExistingData)
	if err != nil {
		return Handle{}, err
	}
	if _, err = e.run(ctx, e.containerArgs(lease, name, ref, volume)...); err != nil {
		// A lost CLI receipt is an unknown outcome. Retain the named container so
		// the same lease can recover it; never launch a second writer or delete data.
		return Handle{}, err
	}
	record, found, err = e.containerRecord(ctx, name)
	if err != nil || !found {
		return Handle{}, errors.New("environment creation receipt is unavailable")
	}
	handle := Handle{ID: record.ID, Ref: ref, Backend: BackendLocalContainer}
	if err := validateDockerHandle(handle, record); err != nil {
		return Handle{}, err
	}
	if err := e.requireRunning(ctx, handle.ID); err != nil {
		return Handle{}, err
	}
	return handle, nil
}

func (e *DockerExecutor) validateRequest(request LocalRequest, now time.Time) error {
	lease, spec := request.Lease, request.Spec
	if err := validateLease(lease, now); err != nil {
		return err
	}
	if len(lease.ID) > 128 || len(lease.RunID) > 128 || len(lease.OwnerID) > 128 || !scopeDigestPattern.MatchString(lease.ScopeHash) || lease.ExpiresAt.After(now.Add(24*time.Hour)) {
		return errors.New("invalid bounded Docker environment lease")
	}
	if spec.Backend != BackendLocalContainer || spec.Image != e.image || len(spec.Env) != 0 || spec.Command != "" || len(spec.Args) != 0 {
		return errors.New("docker environment requires its configured image and controlled command execution")
	}
	q := lease.Quota
	if q.NetworkEgress || q.DiskBytes < 16<<20 || q.DiskBytes > 1<<40 || q.MemoryBytes < 32<<20 || q.MemoryBytes > 16<<30 || q.CPUUnits > 8000 || q.MaxProcesses < 4 || q.MaxProcesses > 256 {
		return errors.New("docker environment quota or network policy is unsupported")
	}
	return nil
}

func dockerHash(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func dockerLeaseIdentity(lease Lease) (string, string) {
	data, _ := json.Marshal(lease)
	name := "soha-env-read-" + dockerHash(lease.OwnerID + "\x00" + lease.ID)[:40]
	if lease.Mode == "restricted-write" {
		// One atomic Docker name prevents concurrent persistent writers, including
		// from independent Agent processes. Read leases have separate containers.
		name = "soha-env-write-" + dockerHash(lease.OwnerID)[:40]
	}
	return name, dockerHash(string(data))
}

func (e *DockerExecutor) containerArgs(lease Lease, name, ref, volume string) []string {
	data, _ := json.Marshal(lease)
	q := lease.Quota
	mount := "type=volume,src=" + volume + ",dst=/data,volume-subpath=data"
	if lease.Mode == "observe-only" {
		mount += ",readonly"
	}
	temporary := min(q.MemoryBytes/8, int64(8<<20))
	args := []string{"run", "--detach", "--name", name, "--label", dockerEnvironmentLabel + "=" + dockerEnvironmentBackend,
		"--label", dockerEnvironmentLabel + ".ref=" + ref, "--label", dockerEnvironmentLabel + ".name=" + name,
		"--label", dockerEnvironmentLabel + ".deadline=" + lease.ExpiresAt.UTC().Format(time.RFC3339Nano),
		"--label", dockerEnvironmentLabel + ".image=" + e.image, "--label", dockerEnvironmentLabel + ".lease=" + base64.RawURLEncoding.EncodeToString(data),
		// A separate non-root supervisor UID prevents task commands (UID 1000)
		// from stopping the lease timer. The timer survives an Agent crash.
		"--network", "none", "--user", "65534:65534", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--memory", strconv.FormatInt(q.MemoryBytes, 10), "--memory-swap", strconv.FormatInt(q.MemoryBytes, 10),
		"--cpus", fmt.Sprintf("%d.%03d", q.CPUUnits/1000, q.CPUUnits%1000), "--pids-limit", strconv.Itoa(q.MaxProcesses), "--shm-size", "1m", "--log-driver", "none",
		"--mount", mount, "--mount", "type=volume,src=" + volume + ",dst=/soha-control,readonly", "--tmpfs", fmt.Sprintf("/workspace:rw,nosuid,nodev,noexec,size=%d,uid=1000,gid=1000,mode=0700", temporary),
		"--tmpfs", fmt.Sprintf("/tmp:rw,nosuid,nodev,noexec,size=%d,uid=1000,gid=1000,mode=0700", temporary),
		// ponytail: whole-second clock stops up to two seconds early; use a
		// subsecond timer if short leases are needed.
		// Hold a shared kernel lock for the entire physical lease. Deletion and
		// first creation take the exclusive lock across independent Agents.
		"--workdir", "/", "--entrypoint", "/bin/sh", e.image, "-ec", `exec 9</soha-control/.soha-data-lock
flock -s -n 9 || exit 1
[ ! -e /soha-control/.soha-personal-retired ] || exit 1
remaining=$(($1 - $(date +%s) - 1))
[ "$remaining" -gt 0 ] || exit 1
exec /bin/sleep "$remaining"`, "personal-supervisor", strconv.FormatInt(lease.ExpiresAt.Unix(), 10)}
	return args
}

func (e *DockerExecutor) containerRecord(ctx context.Context, identifier string) (dockerEnvironmentRecord, bool, error) {
	var record dockerEnvironmentRecord
	filter, format := "name=^/"+identifier+"$", "{{.Names}}"
	if dockerContainerIDPattern.MatchString(identifier) {
		filter, format = "id="+identifier, "{{.ID}}"
	} else if !dockerEnvironmentNamePattern.MatchString(identifier) {
		return record, false, errors.New("invalid environment handle")
	}
	names, err := e.run(ctx, "container", "ls", "--all", "--no-trunc", "--filter", filter, "--format", format)
	if err != nil {
		return record, false, err
	}
	if strings.TrimSpace(string(names)) == "" {
		return record, false, nil
	}
	if strings.TrimSpace(string(names)) != identifier {
		return record, false, errors.New("ambiguous environment handle")
	}
	data, err := e.run(ctx, "container", "inspect", "--format", `{"id":"{{.Id}}","labels":{{json .Config.Labels}}}`, identifier)
	if err != nil {
		return record, false, err
	}
	if err := json.Unmarshal(data, &record); err != nil || !dockerContainerIDPattern.MatchString(record.ID) {
		return record, false, errors.New("invalid environment labels")
	}
	return record, true, nil
}

func validateDockerHandle(handle Handle, record dockerEnvironmentRecord) error {
	labels := record.Labels
	if handle.ID != record.ID || !dockerContainerIDPattern.MatchString(handle.ID) || handle.Ref == "" || labels[dockerEnvironmentLabel] != dockerEnvironmentBackend || labels[dockerEnvironmentLabel+".ref"] != handle.Ref {
		return errors.New("environment belongs to another lease")
	}
	lease, err := dockerLeaseFromLabels(labels)
	if err != nil {
		return err
	}
	name, ref := dockerLeaseIdentity(lease)
	if labels[dockerEnvironmentLabel+".name"] != name || ref != handle.Ref {
		return errors.New("environment lease identity changed")
	}
	return nil
}

func dockerLeaseFromLabels(labels map[string]string) (Lease, error) {
	var lease Lease
	encoded := labels[dockerEnvironmentLabel+".lease"]
	if len(encoded) > 8192 {
		return lease, errors.New("environment lease exceeds its bound")
	}
	data, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || json.Unmarshal(data, &lease) != nil {
		return lease, errors.New("invalid environment lease receipt")
	}
	return lease, nil
}

func (e *DockerExecutor) requireRunning(ctx context.Context, name string) error {
	state, err := e.run(ctx, "container", "inspect", "--format", "{{.State.Running}}", name)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(state)) != "true" {
		return ErrLeaseExpired
	}
	ready, err := e.run(ctx, "exec", "--user", "65534:65534", name, "/bin/sh", "-ec", `test "$(readlink /proc/1/fd/9)" = /soha-control/.soha-data-lock
test "$(tr '\000' '\n' </proc/1/cmdline | head -n 1)" = /bin/sleep
printf ready`)
	if err != nil || string(ready) != "ready" {
		return errors.New("personal environment supervisor lock is unavailable")
	}
	return nil
}

func (e *DockerExecutor) Stop(ctx context.Context, handle Handle) error {
	ctx, cancel := context.WithTimeout(ctx, defaultOperationTimeout)
	defer cancel()
	e.mu.Lock()
	defer e.mu.Unlock()
	record, found, err := e.containerRecord(ctx, handle.ID)
	if err != nil {
		return err
	}
	if found {
		if err := validateDockerHandle(handle, record); err != nil {
			return err
		}
		if _, err = e.run(ctx, "container", "rm", "--force", handle.ID); err != nil {
			return err
		}
	}
	return e.removeParentCommandFence(ctx, handle)
}

// Execute accepts argv inside this lease's sandbox. Cancellation removes the
// whole task container because cancelling docker exec alone leaves its process.
func (e *DockerExecutor) Execute(ctx context.Context, handle Handle, argv []string) (output []byte, err error) {
	return e.executeTask(ctx, handle, "", argv)
}

func (e *DockerExecutor) executeTask(ctx context.Context, handle Handle, taskID string, argv []string) (output []byte, err error) {
	ctx, operationCancel := context.WithTimeout(ctx, defaultOperationTimeout)
	defer operationCancel()
	if len(argv) == 0 || len(argv) > maxEnvironmentArgs || !strings.HasPrefix(argv[0], "/") {
		return nil, errors.New("bounded absolute sandbox command is required")
	}
	for _, arg := range argv {
		if len(arg) > 4096 || strings.ContainsRune(arg, '\x00') {
			return nil, errors.New("environment command argument is invalid")
		}
	}
	lease, err := e.activeLease(ctx, handle)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithDeadline(ctx, lease.ExpiresAt)
	defer cancel()
	fence, err := e.acquireCommandFence(ctx, handle)
	if err != nil {
		return nil, err
	}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), defaultOperationTimeout)
		defer cleanupCancel()
		if err != nil || ctx.Err() != nil {
			// A failed command may have forked a surviving process. End the task
			// sandbox, preserving the user's persistent data and command reservation.
			err = errors.Join(err, ctx.Err(), e.Stop(cleanup, handle))
		}
		err = errors.Join(err, e.removeCommandFence(cleanup, fence))
	}()
	if _, err := e.ensureVolume(ctx, lease, false); err != nil {
		return nil, err
	}
	// Recheck after admission/provisioning: a concurrent release cannot dispatch
	// into a different container because every actual operation uses its full ID.
	if _, err := e.activeLease(ctx, handle); err != nil {
		return nil, err
	}
	if err := e.requireOnlySupervisor(ctx, handle); err != nil {
		return nil, err
	}
	workspace, err := e.taskWorkspace(ctx, handle, taskID)
	if err != nil {
		return nil, err
	}
	output, err = e.run(ctx, append([]string{"exec", "--user", "1000:1000", "--workdir", workspace, handle.ID}, argv...)...)
	if err == nil && !utf8.Valid(output) {
		err = errors.New("personal command output is not UTF-8")
	}
	if err == nil {
		err = e.requireOnlySupervisor(ctx, handle)
	}
	return output, err
}

func (e *DockerExecutor) taskWorkspace(ctx context.Context, handle Handle, taskID string) (string, error) {
	if taskID == "" {
		return "/workspace", nil
	}
	id, err := uuid.Parse(taskID)
	if err != nil || id.String() != taskID {
		return "", errors.New("invalid server task identity")
	}
	workspace := "/workspace/task-" + dockerHash(taskID)[:40]
	_, err = e.run(ctx, "exec", "--user", "1000:1000", handle.ID, "/bin/sh", "-ec", `[ -d "$1" ] || mkdir -m 700 "$1"; [ ! -L "$1" ] && [ "$(readlink -f "$1")" = "$1" ]`, "personal-task-workspace", workspace)
	return workspace, err
}

// A successful docker exec may leave background children. Such a command has
// not completed its bounded task; the existing failure cleanup stops the whole
// immutable container and keeps personal data. Inspect PIDs only, never argv.
func (e *DockerExecutor) requireOnlySupervisor(ctx context.Context, handle Handle) error {
	data, err := e.run(ctx, "top", handle.ID, "-eo", "pid")
	fields := strings.Fields(string(data))
	if err != nil || len(fields) != 2 || fields[0] != "PID" {
		return errors.New("personal command left processes or its completion is unconfirmed")
	}
	pid, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil || pid <= 0 {
		return errors.New("personal command process receipt is invalid")
	}
	return nil
}

type dockerCommandFence struct {
	ID, CallID string
	Parent     Handle
}

func dockerCommandFenceName(parent Handle) string {
	return "soha-env-command-" + dockerHash(parent.ID)[:40]
}

func (e *DockerExecutor) acquireCommandFence(ctx context.Context, parent Handle) (dockerCommandFence, error) {
	fence := dockerCommandFence{CallID: uuid.NewString(), Parent: parent}
	name := dockerCommandFenceName(parent)
	deadline, ok := ctx.Deadline()
	if !ok || !deadline.After(time.Now()) {
		return fence, errors.New("environment command deadline is unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, defaultOperationTimeout)
	defer cancel()
	_, createErr := e.run(ctx, "create", "--name", name, "--label", dockerEnvironmentLabel+"=personal-docker-command-v1",
		"--label", dockerEnvironmentLabel+".ref="+parent.Ref, "--label", dockerEnvironmentLabel+".parent="+parent.ID,
		"--label", dockerEnvironmentLabel+".call="+fence.CallID, "--label", dockerEnvironmentLabel+".deadline="+deadline.UTC().Format(time.RFC3339Nano),
		"--network", "none", "--user", "65534:65534", "--read-only", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--log-driver", "none", "--entrypoint", "/bin/true", e.image)
	record, found, err := e.containerRecord(ctx, name)
	if err != nil || !found {
		return fence, errors.Join(createErr, err, errors.New("environment command reservation is unavailable"))
	}
	labels := record.Labels
	if labels[dockerEnvironmentLabel] != "personal-docker-command-v1" || labels[dockerEnvironmentLabel+".parent"] != parent.ID || labels[dockerEnvironmentLabel+".ref"] != parent.Ref {
		return fence, errors.New("environment command reservation ownership changed")
	}
	if labels[dockerEnvironmentLabel+".call"] != fence.CallID {
		expiresAt, err := time.Parse(time.RFC3339Nano, labels[dockerEnvironmentLabel+".deadline"])
		if err != nil {
			return fence, errors.New("environment command deadline is invalid")
		}
		if !expiresAt.After(time.Now()) {
			// A crashed attempt may have written data. Terminate the original task;
			// never silently steal its reservation and replay the command.
			return fence, errors.Join(errors.New("environment command outcome is unknown"), e.Stop(ctx, parent))
		}
		return fence, ErrEnvironmentCommandBusy
	}
	fence.ID = record.ID
	if createErr != nil {
		return fence, errors.Join(createErr, e.removeCommandFence(ctx, fence))
	}
	return fence, nil
}

func (e *DockerExecutor) removeCommandFence(ctx context.Context, fence dockerCommandFence) error {
	record, found, err := e.containerRecord(ctx, fence.ID)
	if err != nil || !found {
		return err
	}
	labels := record.Labels
	if labels[dockerEnvironmentLabel] != "personal-docker-command-v1" || labels[dockerEnvironmentLabel+".parent"] != fence.Parent.ID || labels[dockerEnvironmentLabel+".ref"] != fence.Parent.Ref || labels[dockerEnvironmentLabel+".call"] != fence.CallID {
		return errors.New("environment command reservation ownership changed")
	}
	_, err = e.run(ctx, "container", "rm", "--force", fence.ID)
	return err
}

func (e *DockerExecutor) removeParentCommandFence(ctx context.Context, parent Handle) error {
	record, found, err := e.containerRecord(ctx, dockerCommandFenceName(parent))
	if err != nil || !found {
		return err
	}
	return e.removeCommandFence(ctx, dockerCommandFence{ID: record.ID, CallID: record.Labels[dockerEnvironmentLabel+".call"], Parent: parent})
}

func (e *DockerExecutor) activeLease(ctx context.Context, handle Handle) (Lease, error) {
	record, found, err := e.containerRecord(ctx, handle.ID)
	if err != nil {
		return Lease{}, err
	}
	if !found {
		return Lease{}, ErrLeaseNotFound
	}
	if err := validateDockerHandle(handle, record); err != nil {
		return Lease{}, err
	}
	if record.Labels[dockerEnvironmentLabel+".image"] != e.image {
		return Lease{}, errors.New("environment image revision changed")
	}
	lease, err := dockerLeaseFromLabels(record.Labels)
	if err != nil || !lease.ExpiresAt.After(time.Now().UTC()) {
		return Lease{}, ErrLeaseExpired
	}
	if err := e.requireRunning(ctx, handle.ID); err != nil {
		return Lease{}, err
	}
	return lease, nil
}

func (e *DockerExecutor) Inspect(ctx context.Context, handle Handle) (ResourceUsage, []string, error) {
	ctx, cancel := context.WithTimeout(ctx, defaultOperationTimeout)
	defer cancel()
	lease, err := e.activeLease(ctx, handle)
	if err != nil {
		return ResourceUsage{}, nil, err
	}
	disk, err := e.volumeUsage(ctx, lease)
	if err != nil {
		return ResourceUsage{}, nil, err
	}
	data, err := e.run(ctx, "exec", "--user", "1000:1000", handle.ID, "/bin/sh", "-ec", "cat /sys/fs/cgroup/memory.current; cat /sys/fs/cgroup/pids.current; cat /sys/fs/cgroup/cpu.stat")
	if err != nil {
		return ResourceUsage{}, nil, err
	}
	usage, err := parseDockerCgroupUsage(string(data))
	usage.DiskBytes = disk
	return usage, nil, err
}

func parseDockerCgroupUsage(data string) (ResourceUsage, error) {
	lines := strings.Fields(data)
	if len(lines) < 4 {
		return ResourceUsage{}, errors.New("environment usage is unavailable")
	}
	memory, memErr := strconv.ParseInt(lines[0], 10, 64)
	processes, pidErr := strconv.Atoi(lines[1])
	for i := 2; i+1 < len(lines); i += 2 {
		if lines[i] != "usage_usec" {
			continue
		}
		cpu, cpuErr := strconv.ParseInt(lines[i+1], 10, 64)
		if memErr != nil || pidErr != nil || cpuErr != nil || memory < 0 || processes < 0 || cpu < 0 || cpu > math.MaxInt64/1000 {
			break
		}
		return ResourceUsage{MemoryBytes: memory, Processes: processes, CPUTimeNanos: cpu * 1000}, nil
	}
	return ResourceUsage{}, errors.New("environment usage is unavailable")
}
