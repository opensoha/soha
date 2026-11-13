//go:build linux

package personalguest

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strconv"
	"strings"
	"time"

	guest "github.com/opensoha/soha-contracts/agent/guest"
)

func (b *runtimeBroker) docker(ctx context.Context, op guest.RuntimeOperation) (*string, *int, error) {
	if op.DockerImage != b.plan.DockerImage {
		return nil, nil, errors.New("unapproved personal VM image")
	}
	name := "soha-personal-" + op.OperationID
	slice := "soha-personal-" + strings.ReplaceAll(b.plan.Binding.DataRef, "-", "") + ".slice"
	quota := op.Request.Lease.Quota
	args := []string{"create", "--name", name, "--label", "soha.personal.data=" + b.plan.Binding.DataRef, "--label", "soha.personal.operation=" + op.OperationID, "--log-driver", "none", "--user", "1000:1000", "--cap-drop", "ALL", "--security-opt", "no-new-privileges:true", "--read-only", "--network", "none", "--ipc", "private", "--cgroupns", "private", "--cgroup-parent", slice, "--cpus", strconv.FormatFloat(float64(quota.CPUUnits)/1000, 'f', 3, 64), "--memory", strconv.FormatInt(quota.MemoryBytes, 10), "--memory-swap", strconv.FormatInt(quota.MemoryBytes, 10), "--pids-limit", strconv.Itoa(quota.MaxProcesses), "--tmpfs", "/tmp:rw,nosuid,nodev,noexec,size=16777216,mode=1777", "--mount", "type=bind,source=" + runtimeWorkspace(b.plan) + ",target=/data", "--workdir", "/data", op.DockerImage}
	args = append(args, op.Request.Argv...)
	created, code, err := runSandboxCommand(ctx, "/usr/bin/docker", args...)
	if err != nil || *code != 0 || len(strings.TrimSpace(*created)) != 64 {
		return nil, nil, errors.New("docker creation is unconfirmed; retain original container")
	}
	defer func() {
		stopCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_, _, _ = runSandboxCommand(stopCtx, "/usr/bin/docker", "stop", "--time", "2", name)
	}()
	if err := b.inspectDocker(ctx, name, op); err != nil {
		return nil, nil, err
	}
	// Capture directly; daemon log files would escape the personal data quota.
	output, start, err := runSandboxCommand(ctx, "/usr/bin/docker", "start", "--attach", name)
	if err != nil {
		return nil, nil, errors.New("docker start is unconfirmed")
	}
	waited, code, err := runSandboxCommand(ctx, "/usr/bin/docker", "wait", name)
	if err != nil || *code != 0 {
		return nil, nil, errors.New("docker process exit is unconfirmed")
	}
	exit, err := strconv.Atoi(strings.TrimSpace(*waited))
	if err != nil || exit < 0 || exit > 255 || *start != exit {
		return nil, nil, errors.New("invalid Docker process exit")
	}
	if err := b.inspectDocker(ctx, name, op); err != nil {
		return nil, nil, err
	}
	return output, &exit, nil
}

func (b *runtimeBroker) inspectDocker(ctx context.Context, name string, op guest.RuntimeOperation) error {
	inspected, code, err := runSandboxCommand(ctx, "/usr/bin/docker", "inspect", "--format", "{{json .}}", name)
	if err != nil || *code != 0 {
		return errors.New("docker isolation receipt unavailable")
	}
	return validateDockerInspection(*inspected, b.plan, op)
}

func validateDockerInspection(raw string, plan guest.ToolsPlan, op guest.RuntimeOperation) error {
	slice := "soha-personal-" + strings.ReplaceAll(plan.Binding.DataRef, "-", "") + ".slice"
	quota := op.Request.Lease.Quota
	var observed struct {
		Config     struct{ User string }
		State      struct{ Error string }
		HostConfig struct {
			AutoRemove bool
			LogConfig  struct {
				Type   string
				Config map[string]string
			}
			Privileged                                       bool
			NetworkMode, PidMode, CgroupParent, CgroupnsMode string
			ReadonlyRootfs                                   bool
			CapDrop, CapAdd                                  []string
			SecurityOpt                                      []string
			NanoCpus, Memory, MemorySwap                     int64
			PidsLimit                                        *int64
			Devices                                          []any
		}
		Mounts []struct {
			Source, Destination string
			RW                  bool
		}
	}
	if json.Unmarshal([]byte(raw), &observed) != nil {
		return errors.New("invalid Docker isolation receipt")
	}
	config := observed.HostConfig
	if observed.State.Error != "" || config.AutoRemove || config.LogConfig.Type != "none" || len(config.LogConfig.Config) != 0 {
		return errors.New("docker retention or bounded output policy changed")
	}
	if observed.Config.User != "1000:1000" || config.Privileged || config.NetworkMode != "none" || config.PidMode != "" || config.CgroupParent != slice || config.CgroupnsMode != "private" || !config.ReadonlyRootfs || len(config.CapDrop) != 1 || config.CapDrop[0] != "ALL" || len(config.CapAdd) != 0 || len(config.Devices) != 0 || !slices.Contains(config.SecurityOpt, "no-new-privileges:true") || config.NanoCpus != quota.CPUUnits*1000000 || config.Memory != quota.MemoryBytes || config.MemorySwap != quota.MemoryBytes || config.PidsLimit == nil || *config.PidsLimit != int64(quota.MaxProcesses) || len(observed.Mounts) != 1 || observed.Mounts[0].Source != runtimeWorkspace(plan) || observed.Mounts[0].Destination != "/data" || !observed.Mounts[0].RW {
		return errors.New("docker hard policy differs from original request")
	}
	return nil
}
