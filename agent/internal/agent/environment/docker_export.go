package environment

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"time"

	"github.com/google/uuid"
	contractenv "github.com/opensoha/soha-contracts/agent/environment"
	"github.com/opensoha/soha-contracts/streamlimit"
)

// ExportPersonalData reads a stopped owner's entire data tree, including link
// entries without following their targets. It never extracts or mutates files.
func (e *DockerExecutor) ExportPersonalData(ctx context.Context, request contractenv.Request, target io.Writer) (resultErr error) {
	if request.Validate() != nil || request.Action != "export_data" {
		return errors.New("invalid personal data export request")
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, defaultOperationTimeout)
	defer cancel()
	l := request.Lease
	lease := Lease{OwnerID: l.OwnerID, Quota: Quota{DiskBytes: l.Quota.DiskBytes}}
	if err := e.verifyVolume(ctx, lease); err != nil {
		return err
	}
	volume := dockerPersonalVolume(l.OwnerID)
	if err := e.requireUnusedPersonalVolume(ctx, volume); err != nil {
		return err
	}
	name, ref := "soha-env-export-"+uuid.NewString(), uuid.NewString()
	defer func() { resultErr = errors.Join(resultErr, e.removeExportContainer(name, ref)) }()
	q := l.Quota
	args := []string{"run", "--rm", "--name", name, "--label", dockerEnvironmentLabel + "=personal-docker-export-v1", "--label", dockerEnvironmentLabel + ".ref=" + ref,
		"--network", "none", "--read-only", "--user", "1000:1000", "--cap-drop", "ALL", "--security-opt", "no-new-privileges",
		"--memory", strconv.FormatInt(q.MemoryBytes, 10), "--memory-swap", strconv.FormatInt(q.MemoryBytes, 10),
		"--cpus", fmt.Sprintf("%d.%03d", q.CPUUnits/1000, q.CPUUnits%1000), "--pids-limit", strconv.Itoa(q.MaxProcesses), "--shm-size", "1m", "--log-driver", "none",
		"--mount", "type=volume,src=" + volume + ",dst=/data,volume-subpath=data,readonly", "--mount", "type=volume,src=" + volume + ",dst=/soha-control,readonly",
		"--workdir", "/", "--entrypoint", "/bin/timeout", e.image, "30", "/bin/sh", "-ec", `exec 9</soha-control/.soha-data-lock
flock -x -n 9 || exit 1
[ ! -e /soha-control/.soha-personal-retired ] || exit 1
exec /bin/tar -cf - -C /data .`}
	// Fixed program and script; all Docker values come from the validated lease
	// or immutable configured image, never caller paths or executable arguments.
	cmd := exec.CommandContext(ctx, "docker", args...) //nolint:gosec // G204: fixed program, controlled arguments above.
	stderr := streamlimit.NewLimitedBuffer(dockerEnvironmentOutputLimit)
	cmd.Stdout, cmd.Stderr, cmd.WaitDelay = target, stderr, time.Second
	if err := cmd.Run(); err != nil || stderr.TotalBytes() != 0 {
		return errors.New("personal data export did not complete; no archive receipt")
	}
	return nil
}

func (e *DockerExecutor) removeExportContainer(name, ref string) error {
	cleanup, cancel := context.WithTimeout(context.Background(), defaultOperationTimeout)
	defer cancel()
	record, found, err := e.containerRecord(cleanup, name)
	if err != nil || !found {
		return err
	}
	if record.Labels[dockerEnvironmentLabel] != "personal-docker-export-v1" || record.Labels[dockerEnvironmentLabel+".ref"] != ref {
		return errors.New("personal data export cleanup identity changed")
	}
	_, err = e.run(cleanup, "container", "rm", "--force", record.ID)
	return err
}
