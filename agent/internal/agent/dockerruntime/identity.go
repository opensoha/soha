// Package dockerruntime owns bounded Docker Engine observations used by the
// local API and runner. It never authorizes callers from labels alone.
package dockerruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"strings"
	"time"

	protocol "github.com/opensoha/soha/contracts/docker/runtime"
)

type Mount struct {
	Type        string `json:"Type"`
	Name        string `json:"Name"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
	RW          bool   `json:"RW"`
}

type Container struct {
	ID         string            `json:"id"`
	Labels     map[string]string `json:"labels"`
	Mounts     []Mount           `json:"mounts"`
	Image      string            `json:"image"`
	Status     string            `json:"status"`
	Platform   string            `json:"platform"`
	Running    bool              `json:"running"`
	Privileged bool              `json:"privileged"`
	PIDMode    string            `json:"pidMode"`
	UserNSMode string            `json:"usernsMode"`
	CapAdd     []string          `json:"capAdd"`
}

type boundedOutput struct {
	bytes.Buffer
	limit int
}

func (b *boundedOutput) Write(value []byte) (int, error) {
	if len(value) > b.limit-b.Len() {
		return 0, errors.New("docker observation exceeds its limit")
	}
	return b.Buffer.Write(value)
}

func parseContainer(raw []byte, binding protocol.ContainerBinding) (Container, error) {
	var item Container
	if len(raw) > 1<<20 || json.Unmarshal(raw, &item) != nil || len(item.Mounts) > 256 || !binding.MatchesContainer(item.ID, item.Labels) {
		return Container{}, errors.New("docker container identity could not be confirmed")
	}
	return item, nil
}

func InspectContainer(ctx context.Context, binding protocol.ContainerBinding) (Container, error) {
	if binding.Validate() != nil {
		return Container{}, errors.New("docker container binding is invalid")
	}
	// Select immutable ID directly. The projection omits environment and other
	// private inspect fields; neither stderr nor raw output enters public errors.
	raw, err := observe(ctx, 1<<20, "container", "inspect", "--format", `{"id":{{json .Id}},"labels":{{json .Config.Labels}},"mounts":{{json .Mounts}},"image":{{json .Config.Image}},"status":{{json .State.Status}},"platform":{{json .Platform}},"running":{{json .State.Running}},"privileged":{{json .HostConfig.Privileged}},"pidMode":{{json .HostConfig.PidMode}},"usernsMode":{{json .HostConfig.UsernsMode}},"capAdd":{{json .HostConfig.CapAdd}}}`, binding.ContainerID)
	if err != nil {
		return Container{}, errors.New("docker container identity could not be observed")
	}
	return parseContainer(raw, binding)
}

func observe(ctx context.Context, limit int, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	// #nosec G204 -- fixed Docker executable and separate arguments; callers validate UID-derived namespaces and full container IDs, without a shell.
	command := exec.CommandContext(ctx, "docker", args...)
	command.WaitDelay = 2 * time.Second
	output := &boundedOutput{limit: limit}
	command.Stdout, command.Stderr = output, io.Discard
	if command.Run() != nil {
		return nil, errors.New("docker observation failed")
	}
	return output.Bytes(), nil
}

// Observe every container in the actual namespace before any project-wide
// mutation. A foreign or malformed member blocks the operation, never adoption.
func ListProjectContainers(ctx context.Context, binding protocol.ProjectBinding) ([]Container, error) {
	if binding.Validate() != nil {
		return nil, errors.New("docker project binding is invalid")
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	raw, err := observe(ctx, 64<<10, "container", "ls", "--all", "--no-trunc", "--filter", "label=com.docker.compose.project="+binding.Namespace, "--format", `{{.ID}} {{.Label "com.docker.compose.service"}}`)
	if err != nil {
		return nil, err
	}
	targets, err := parseProjectTargets(raw, binding)
	if err != nil {
		return nil, err
	}
	items := make([]Container, 0, len(targets))
	for _, target := range targets {
		item, err := InspectContainer(ctx, target)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, nil
}

func parseProjectTargets(raw []byte, binding protocol.ProjectBinding) ([]protocol.ContainerBinding, error) {
	var targets []protocol.ContainerBinding
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 || len(targets) >= 256 {
			return nil, errors.New("docker project inventory is ambiguous or exceeds its limit")
		}
		target := protocol.ContainerBinding{ProjectBinding: binding, ContainerID: fields[0], ServiceName: fields[1]}
		if target.Validate() != nil || seen[target.ContainerID] {
			return nil, errors.New("docker project inventory identity is invalid")
		}
		seen[target.ContainerID] = true
		targets = append(targets, target)
	}
	return targets, nil
}
