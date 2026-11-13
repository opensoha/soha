package environment

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	contractenv "github.com/opensoha/soha-contracts/agent/environment"
)

const personalProbeOwnerPrefix = "quota-probe:"
const personalProbeLabel = dockerEnvironmentLabel + ".probe"

var personalVolumeNamePattern = regexp.MustCompile(`^soha-env-data-[a-f0-9]{40}$`)

// Preflight exercises the configured Docker/quota path with a disposable owner.
// A successful reply requires verified hard limits and confirmed probe cleanup.
func (e *DockerExecutor) Preflight(ctx context.Context, request contractenv.PreflightRequest) (result contractenv.PreflightResult, err error) {
	if err := request.Validate(); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, defaultOperationTimeout)
	defer cancel()
	now := time.Now().UTC()
	q := request.Quota
	encoded, _ := json.Marshal(request)
	lease := Lease{ID: request.ID, RunID: request.ID, OwnerID: personalProbeOwnerPrefix + request.ID, ScopeHash: "sha256:" + dockerHash(string(encoded)), Mode: "restricted-write",
		IssuedAt: now, ExpiresAt: now.Add(2 * defaultOperationTimeout), Quota: Quota{DiskBytes: q.DiskBytes, MemoryBytes: q.MemoryBytes, CPUUnits: q.CPUUnits, MaxProcesses: q.MaxProcesses}}
	defer func() {
		cleanup, cleanupCancel := context.WithTimeout(context.Background(), defaultOperationTimeout)
		defer cleanupCancel()
		err = errors.Join(err, e.cleanupPersonalProbe(cleanup, lease))
		if err != nil {
			result = contractenv.PreflightResult{}
		}
	}()
	if err := e.createPersonalProbeVolume(ctx, lease); err != nil {
		return result, err
	}
	handle, err := e.Start(ctx, LocalRequest{Lease: lease, Spec: Spec{Backend: BackendLocalContainer, Image: e.image}})
	if err != nil {
		return result, err
	}
	if err := e.verifyPersonalProbeLimits(ctx, handle, lease.Quota); err != nil {
		return result, err
	}
	if _, _, err := e.Inspect(ctx, handle); err != nil {
		return result, err
	}
	return contractenv.PreflightResult{SchemaVersion: contractenv.PreflightResultVersion, ID: request.ID, Quota: q, Ready: true, CgroupVersion: "2", DiskMechanism: "btrfs-qgroup", ObservedAt: time.Now().UTC()}, nil
}

func (e *DockerExecutor) createPersonalProbeVolume(ctx context.Context, lease Lease) error {
	data, err := json.Marshal(lease)
	if err != nil {
		return err
	}
	_, err = e.run(ctx, "volume", "create", "--label", dockerEnvironmentLabel+"="+dockerEnvironmentBackend,
		"--label", dockerEnvironmentLabel+".owner="+dockerHash(lease.OwnerID), "--label", dockerEnvironmentLabel+".disk="+strconv.FormatInt(lease.Quota.DiskBytes, 10),
		"--label", personalProbeLabel+"="+lease.ID, "--label", dockerEnvironmentLabel+".deadline="+lease.ExpiresAt.Format(time.RFC3339Nano),
		"--label", dockerEnvironmentLabel+".lease="+base64.RawURLEncoding.EncodeToString(data), dockerPersonalVolume(lease.OwnerID))
	return err
}

func (e *DockerExecutor) verifyPersonalProbeLimits(ctx context.Context, handle Handle, quota Quota) error {
	data, err := e.run(ctx, "container", "inspect", "--format", "{{json .HostConfig}}", handle.ID)
	if err != nil {
		return err
	}
	var config struct {
		NetworkMode                             string
		ReadonlyRootfs                          bool
		Memory, MemorySwap, NanoCpus, PidsLimit int64
		CapDrop, SecurityOpt                    []string
	}
	if json.Unmarshal(data, &config) != nil || config.NetworkMode != "none" || !config.ReadonlyRootfs || config.Memory != quota.MemoryBytes || config.MemorySwap != quota.MemoryBytes || config.NanoCpus != quota.CPUUnits*1000000 || config.PidsLimit != int64(quota.MaxProcesses) || !containsProbeSetting(config.CapDrop, "ALL") || !containsProbeSetting(config.SecurityOpt, "no-new-privileges") {
		return errors.New("personal Docker hard limits are unavailable")
	}
	return nil
}

func containsProbeSetting(items []string, expected string) bool {
	for _, item := range items {
		if item == expected || item == expected+":true" {
			return true
		}
	}
	return false
}

func (e *DockerExecutor) cleanupPersonalProbe(ctx context.Context, lease Lease) error {
	id, idErr := uuid.Parse(lease.ID)
	if idErr != nil || id.String() != lease.ID || lease.OwnerID != personalProbeOwnerPrefix+lease.ID {
		return errors.New("refusing non-probe data cleanup")
	}
	handle, found, err := e.personalHandle(ctx, lease)
	if err != nil {
		return err
	}
	if found {
		if err := e.Stop(ctx, handle); err != nil {
			return err
		}
	}
	volume := dockerPersonalVolume(lease.OwnerID)
	names, err := e.run(ctx, "volume", "ls", "--filter", "name="+volume, "--format", "{{.Name}}")
	if err != nil || strings.TrimSpace(string(names)) == "" {
		return err
	}
	if strings.TrimSpace(string(names)) != volume {
		return errors.New("ambiguous quota probe volume")
	}
	data, err := e.run(ctx, "volume", "inspect", "--format", "{{json .Labels}}", volume)
	if err != nil {
		return err
	}
	var labels map[string]string
	if json.Unmarshal(data, &labels) != nil || labels[personalProbeLabel] != lease.ID {
		return errors.New("quota probe ownership changed")
	}
	if err := e.verifyVolume(ctx, lease); err != nil {
		return err
	}
	if _, err := e.quotaHelper(ctx, volume, "/bin/sh", "-ec", `if [ -e /probe/data ]; then btrfs subvolume delete /probe/data >/dev/null; fi`); err != nil {
		return err
	}
	_, err = e.run(ctx, "volume", "rm", volume)
	return err
}

func (e *DockerExecutor) gcPersonalProbes(ctx context.Context, now time.Time) error {
	data, err := e.run(ctx, "volume", "ls", "--filter", "label="+personalProbeLabel, "--format", `{{.Name}} {{.Label "soha.environment.deadline"}}`)
	if err != nil {
		return err
	}
	names, err := expiredDockerReceiptNames(string(data), now, personalVolumeNamePattern.MatchString)
	if err != nil {
		return err
	}
	var failures []error
	for _, name := range names {
		data, err := e.run(ctx, "volume", "inspect", "--format", "{{json .Labels}}", name)
		var labels map[string]string
		if err != nil || json.Unmarshal(data, &labels) != nil {
			failures = append(failures, errors.Join(err, errors.New("quota probe recovery labels unavailable")))
			continue
		}
		lease, err := dockerLeaseFromLabels(labels)
		if err != nil || lease.ExpiresAt.After(now) || dockerPersonalVolume(lease.OwnerID) != name || labels[personalProbeLabel] != lease.ID {
			failures = append(failures, errors.Join(err, errors.New("quota probe recovery identity changed")))
			continue
		}
		if err := e.cleanupPersonalProbe(ctx, lease); err != nil {
			failures = append(failures, err)
		}
	}
	return errors.Join(append(failures, ctx.Err())...)
}
