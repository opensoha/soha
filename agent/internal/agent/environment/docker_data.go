package environment

import (
	"context"
	"errors"
	"strings"
)

// The Core owner must require an explicit confirmation and a stopped personal
// environment. A retained private tombstone prevents delayed acquisition or
// restart from silently recreating an empty personal data directory.
func (e *DockerExecutor) DeletePersonalData(ctx context.Context, lease Lease) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, defaultOperationTimeout)
	defer cancel()
	if err := e.verifyVolume(ctx, lease); err != nil {
		return err
	}
	volume := dockerPersonalVolume(lease.OwnerID)
	if err := e.requireUnusedPersonalVolume(ctx, volume); err != nil {
		return err
	}
	if _, err := e.quotaHelper(ctx, volume, "/bin/sh", "-ec", `exec 9>/probe/.soha-data-lock
flock -n 9 || exit 1
touch /probe/.soha-personal-retired
if [ -e /probe/data ]; then btrfs subvolume delete /probe/data >/dev/null; fi
test ! -e /probe/data`); err != nil {
		return err
	}
	// A late original acquisition must not turn an in-use volume into a
	// confirmed deletion. No live container is stopped implicitly here.
	return e.requireUnusedPersonalVolume(ctx, volume)
}

func (e *DockerExecutor) requireUnusedPersonalVolume(ctx context.Context, volume string) error {
	data, err := e.run(ctx, "container", "ls", "--all", "--quiet", "--no-trunc", "--filter", "volume="+volume)
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(data)) != "" {
		return errors.New("personal data is still attached to a container")
	}
	return nil
}

func (e *DockerExecutor) personalDataRetired(ctx context.Context, lease Lease) (bool, error) {
	volume := dockerPersonalVolume(lease.OwnerID)
	names, err := e.run(ctx, "volume", "ls", "--filter", "name="+volume, "--format", "{{.Name}}")
	if err != nil || strings.TrimSpace(string(names)) == "" {
		return false, err
	}
	if strings.TrimSpace(string(names)) != volume {
		return false, errors.New("ambiguous personal data identity")
	}
	if err := e.verifyVolume(ctx, lease); err != nil {
		return false, err
	}
	data, err := e.quotaHelper(ctx, volume, "/bin/sh", "-ec", `if [ -f /probe/.soha-personal-retired ]; then
  [ ! -e /probe/data ] || exit 1
  printf retired
else printf retained; fi`)
	if err != nil {
		return false, err
	}
	switch string(data) {
	case "retired":
		return true, nil
	case "retained":
		return false, nil
	default:
		return false, errors.New("personal data receipt is unavailable")
	}
}

// Deletion can inspect an expired original lease; its identity and quotas still
// come from the validated private request, never from a caller-selected path.
func personalDataDeletedResult(retired bool) string {
	if retired {
		return "deleted"
	}
	return "missing"
}
