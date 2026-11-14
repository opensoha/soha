package dockerruntime

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"path"
	"regexp"

	protocol "github.com/opensoha/soha/contracts/docker/runtime"
)

var terminalAPIVersion = regexp.MustCompile(`^[1-9][0-9]{0,2}\.[0-9]{1,3}$`)

// All reads use the same immutable transport and negotiated API as SDK exec
// creation/attach. Only fixed operation paths and complete IDs are accepted.
func (engine *terminalEngine) readTerminalJSON(ctx context.Context, kind, id string, limit int64) ([]byte, error) {
	version := engine.client.ClientVersion()
	if !terminalAPIVersion.MatchString(version) {
		return nil, errTerminalInspection
	}
	var target string
	switch kind {
	case "exec", "containers":
		if !terminalExecID.MatchString(id) {
			return nil, errTerminalInspection
		}
		target = path.Join(kind, id, "json")
	case "info":
		if id != "" {
			return nil, errTerminalInspection
		}
		target = "info"
	default:
		return nil, errTerminalInspection
	}
	endpoint := url.URL{Scheme: "http", Host: "docker.invalid", Path: path.Join(engine.host.Path, "v"+version, target)}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return nil, errTerminalInspection
	}
	request.Header.Set("Accept", "application/json")
	response, err := engine.inspect.Do(request)
	if err != nil {
		return nil, errTerminalInspection
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, errTerminalInspection
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(raw)) > limit {
		return nil, errTerminalInspection
	}
	return raw, nil
}

func (engine *terminalEngine) verifyTerminalContainer(ctx context.Context, binding protocol.ContainerBinding, requireRunning bool) error {
	if binding.Validate() != nil {
		return errTerminalInspection
	}
	raw, err := engine.readTerminalJSON(ctx, "containers", binding.ContainerID, 1<<20)
	if err != nil {
		return err
	}
	// A projection only: neither Env nor raw provider inspect leaves this read.
	var item struct {
		ID       string `json:"Id"`
		Platform string `json:"Platform"`
		State    *struct {
			Running *bool `json:"Running"`
		} `json:"State"`
		Config *struct {
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
	}
	if json.Unmarshal(raw, &item) != nil || item.State == nil || item.State.Running == nil || requireRunning && !*item.State.Running || item.Config == nil || item.Platform != "linux" || !binding.MatchesContainer(item.ID, item.Config.Labels) {
		return errTerminalInspection
	}
	return nil
}
