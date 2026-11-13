package dockerruntime

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"regexp"

	"github.com/moby/moby/api/types/container"
)

var terminalExecID = regexp.MustCompile(`^[0-9a-f]{64}$`)
var errTerminalInspection = errors.New("docker terminal process outcome could not be verified")

func (engine *terminalEngine) inspectOriginalExec(ctx context.Context, id, containerID string) (container.ExecInspectResponse, error) {
	if !terminalExecID.MatchString(id) || !terminalExecID.MatchString(containerID) {
		return container.ExecInspectResponse{}, errTerminalInspection
	}
	raw, err := engine.readTerminalJSON(ctx, "exec", id, 64<<10)
	if err != nil {
		return container.ExecInspectResponse{}, err
	}
	item, err := decodeOriginalExec(raw)
	if err != nil || item.ID != id || item.ContainerID != containerID {
		return container.ExecInspectResponse{}, errTerminalInspection
	}
	return item, nil
}

func decodeOriginalExec(raw []byte) (container.ExecInspectResponse, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return container.ExecInspectResponse{}, errTerminalInspection
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || len(fields) >= 64 {
			return container.ExecInspectResponse{}, errTerminalInspection
		}
		if _, duplicate := fields[key]; duplicate {
			return container.ExecInspectResponse{}, errTerminalInspection
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return container.ExecInspectResponse{}, errTerminalInspection
		}
		fields[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return container.ExecInspectResponse{}, errTerminalInspection
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return container.ExecInspectResponse{}, errTerminalInspection
	}
	var running *bool
	if json.Unmarshal(fields["Running"], &running) != nil || running == nil {
		return container.ExecInspectResponse{}, errTerminalInspection
	}
	var item container.ExecInspectResponse
	if json.Unmarshal(raw, &item) != nil || item.Running != *running || !terminalExecID.MatchString(item.ID) || !terminalExecID.MatchString(item.ContainerID) {
		return item, errTerminalInspection
	}
	// Docker can finish an attempted start with a known nonzero ExitCode before
	// allocating a PID. ExitCode presence, not PID lifetime, proves the outcome.
	if !item.Running && (item.ExitCode == nil || *item.ExitCode < 0) {
		return item, errTerminalInspection
	}
	return item, nil
}
