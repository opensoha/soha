package environment

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/google/uuid"
)

func dockerPersonalVolume(owner string) string {
	return "soha-env-data-" + dockerHash(owner)[:40]
}

func (e *DockerExecutor) ensureVolume(ctx context.Context, lease Lease, allowCreate bool) (string, error) {
	volume := dockerPersonalVolume(lease.OwnerID)
	quota := strconv.FormatInt(lease.Quota.DiskBytes, 10)
	// Docker returns an existing volume without replacing labels. Verify them
	// after create so an unrelated volume can never be adopted or relabelled.
	if allowCreate {
		if _, err := e.run(ctx, "volume", "create", "--label", dockerEnvironmentLabel+"="+dockerEnvironmentBackend,
			"--label", dockerEnvironmentLabel+".owner="+dockerHash(lease.OwnerID), "--label", dockerEnvironmentLabel+".disk="+quota, volume); err != nil {
			return "", err
		}
	}
	if err := e.verifyVolume(ctx, lease); err != nil {
		return "", err
	}
	return e.prepareVolumeQuota(ctx, lease, volume, quota, allowCreate)
}

func (e *DockerExecutor) verifyVolume(ctx context.Context, lease Lease) error {
	volume := dockerPersonalVolume(lease.OwnerID)
	quota := strconv.FormatInt(lease.Quota.DiskBytes, 10)
	data, err := e.run(ctx, "volume", "inspect", "--format", `{"labels":{{json .Labels}},"driver":{{json .Driver}},"options":{{json .Options}}}`, volume)
	if err != nil {
		return err
	}
	var record struct {
		Labels  map[string]string `json:"labels"`
		Driver  string            `json:"driver"`
		Options map[string]string `json:"options"`
	}
	if json.Unmarshal(data, &record) != nil || record.Driver != "local" || len(record.Options) != 0 || record.Labels[dockerEnvironmentLabel] != dockerEnvironmentBackend || record.Labels[dockerEnvironmentLabel+".owner"] != dockerHash(lease.OwnerID) || record.Labels[dockerEnvironmentLabel+".disk"] != quota {
		return errors.New("persistent environment data ownership, driver or quota changed")
	}
	return nil
}

func (e *DockerExecutor) prepareVolumeQuota(ctx context.Context, lease Lease, volume, quota string, allowCreate bool) (string, error) {
	// Only fixed commands in an operator-pinned helper can manage this owned
	// volume. No task argument, host path, socket, or global quota enable/rescan.
	output, err := e.quotaHelper(ctx, volume, "/bin/sh", "-ec", `
exec 9>/probe/.soha-data-lock
chmod 0644 /probe/.soha-data-lock
lock_mode=-s
[ -e /probe/data ] || lock_mode=-x
flock "$lock_mode" -n 9 || exit 1
[ ! -e /probe/.soha-personal-retired ] || exit 1
if [ ! -e /probe/data ]; then
  [ "$2" = true ] || exit 1
  btrfs subvolume create /probe/data >/dev/null
fi
[ ! -e /probe/.soha-personal-retired ] || exit 1
btrfs subvolume show /probe/data >/dev/null
btrfs qgroup limit "$1" /probe/data
chown 1000:1000 /probe/data
chmod 0700 /probe/data
btrfs qgroup show --sync -rf --raw /probe/data
`, "quota", quota, strconv.FormatBool(allowCreate))
	if err != nil {
		return "", err
	}
	if _, err := parseDockerVolumeQuota(string(output), lease.Quota.DiskBytes); err != nil {
		return "", err
	}
	return volume, nil
}

func (e *DockerExecutor) quotaHelper(ctx context.Context, volume string, command ...string) ([]byte, error) {
	name := "soha-env-helper-" + uuid.NewString()
	ref := uuid.NewString()
	args := []string{"run", "--rm", "--name", name, "--label", dockerEnvironmentLabel + "=personal-docker-quota-v1", "--label", dockerEnvironmentLabel + ".ref=" + ref,
		"--network", "none", "--read-only", "--cap-drop", "ALL", "--cap-add", "SYS_ADMIN", "--cap-add", "CHOWN", "--cap-add", "FOWNER", "--cap-add", "DAC_OVERRIDE",
		"--security-opt", "no-new-privileges", "--memory", "64m", "--memory-swap", "64m", "--cpus", "0.25", "--pids-limit", "16", "--log-driver", "none",
		"--mount", "type=volume,src=" + volume + ",dst=/probe", "--entrypoint", command[0], e.helper}
	output, err := e.run(ctx, append(args, command[1:]...)...)
	if ctx.Err() != nil {
		cleanup, cancel := context.WithTimeout(context.Background(), defaultOperationTimeout)
		defer cancel()
		record, found, inspectErr := e.containerRecord(cleanup, name)
		if inspectErr != nil {
			return output, errors.Join(err, inspectErr)
		}
		if found && record.Labels[dockerEnvironmentLabel] == "personal-docker-quota-v1" && record.Labels[dockerEnvironmentLabel+".ref"] == ref {
			_, cleanupErr := e.run(cleanup, "container", "rm", "--force", record.ID)
			return output, errors.Join(err, cleanupErr)
		}
	}
	return output, err
}

func (e *DockerExecutor) volumeUsage(ctx context.Context, lease Lease) (int64, error) {
	if err := e.verifyVolume(ctx, lease); err != nil {
		return 0, err
	}
	output, err := e.quotaHelper(ctx, dockerPersonalVolume(lease.OwnerID), "btrfs", "qgroup", "show", "--sync", "-rf", "--raw", "/probe/data")
	if err != nil {
		return 0, err
	}
	return parseDockerVolumeQuota(string(output), lease.Quota.DiskBytes)
}

func parseDockerVolumeQuota(output string, quota int64) (int64, error) {
	count := 0
	var usage int64
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(strings.ToLower(line), "warning") || strings.Contains(strings.ToLower(line), "inconsistent") {
			return 0, errors.New("persistent environment quota accounting is inconsistent")
		}
		fields := strings.Fields(line)
		if len(fields) == 0 || !strings.HasPrefix(fields[0], "0/") {
			continue
		}
		// Recent btrfs-progs adds a Path column after the same four quota fields.
		if len(fields) < 4 || len(fields) > 5 || (len(fields) == 5 && len(fields[4]) > 2048) {
			return 0, errors.New("persistent environment quota is unavailable")
		}
		charged, usageErr := strconv.ParseInt(fields[1], 10, 64)
		limit, limitErr := strconv.ParseInt(fields[3], 10, 64)
		if usageErr != nil || limitErr != nil || charged < 0 || limit != quota {
			return 0, errors.New("persistent environment hard quota is unavailable")
		}
		usage = charged
		count++
	}
	if count != 1 {
		return 0, errors.New("persistent environment hard quota is ambiguous")
	}
	return usage, nil
}
