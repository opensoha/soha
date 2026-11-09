package networkproxyruntime

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"time"

	api "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	"github.com/opensoha/soha-contracts/proxyadapter"
	domain "github.com/opensoha/soha/internal/domain/networkproxy"
	"github.com/opensoha/soha/internal/platform/securefile"
)

type commandReceiptEntry struct {
	input   api.NetworkProxyRuntimeCommandInput
	receipt api.NetworkProxyRuntimeCommandReceipt
	settled bool
}
type hostLog struct {
	Sequence   int64     `json:"sequence"`
	Level      string    `json:"level"`
	Message    string    `json:"message"`
	OccurredAt time.Time `json:"occurredAt"`
}

func (r *Runner) recordEvent(code string) {
	r.logSequence++
	r.logs = append(r.logs, hostLog{r.logSequence, "info", code, time.Now().UTC()})
	if len(r.logs) > 200 {
		r.logs = r.logs[len(r.logs)-200:]
	}
}

func (r *Runner) executeCommand(ctx context.Context) error {
	if !r.enabled || r.engine.process == nil {
		return nil
	}
	r.retryCommandReceipt(ctx)
	command, err := r.client.NextCommand(ctx, r.engine.processEpoch)
	if err != nil || command == nil {
		return err
	}
	if err := proxyadapter.ValidateCommand(*command, r.engine.processEpoch, time.Now()); err != nil {
		return errors.New("runtime command rejected")
	}
	if entry, ok := r.commandReceipts[command.RequestID]; ok {
		if !reflect.DeepEqual(entry.input, *command) {
			return errors.New("runtime request identity reused")
		}
		return nil
	}
	receipt := api.NetworkProxyRuntimeCommandReceipt{CommandID: command.RequestID, RequestID: command.RequestID, ProcessEpoch: command.ProcessEpoch, Kind: command.Kind, ExpiresAt: command.ExpiresAt}
	commandCtx, cancel := context.WithDeadline(ctx, command.ExpiresAt)
	result, status, reason := r.runCommand(commandCtx, *command)
	cancel()
	receipt.Result, receipt.Status, receipt.ReasonCode = result, api.NetworkProxyRuntimeCommandReceiptStatus(status), reason
	if !command.ExpiresAt.After(time.Now()) {
		return errors.New("runtime command result expired")
	}
	if r.commandReceipts == nil {
		r.commandReceipts = map[string]commandReceiptEntry{}
	}
	entry := commandReceiptEntry{input: *command, receipt: receipt}
	return r.deliverCommandReceipt(ctx, command.RequestID, entry)
}

// Retry one uncertain delivery per poll. A failed receipt must not block claiming
// another command. Settled entries remain as request-identity fences until expiry.
func (r *Runner) retryCommandReceipt(ctx context.Context) {
	attempted := false
	for id, entry := range r.commandReceipts {
		if !entry.input.ExpiresAt.After(time.Now()) || entry.input.ProcessEpoch != r.engine.processEpoch {
			delete(r.commandReceipts, id)
			continue
		}
		if attempted || entry.settled {
			continue
		}
		attempted = true
		retryCtx, cancel := context.WithTimeout(ctx, time.Second)
		err := r.deliverCommandReceipt(retryCtx, id, entry)
		cancel()
		if err != nil {
			r.recordEvent("command_receipt_retry_failed")
		}
	}
}

func (r *Runner) deliverCommandReceipt(ctx context.Context, id string, entry commandReceiptEntry) error {
	ctx, cancel := context.WithDeadline(ctx, entry.input.ExpiresAt)
	defer cancel()
	err := r.client.CompleteCommand(ctx, entry.receipt)
	var status controlStatusError
	entry.settled = err == nil || errors.As(err, &status) && (status == http.StatusConflict || status == http.StatusBadRequest)
	r.commandReceipts[id] = entry
	return err
}

func commandResult(value any) *api.NetworkProxyRuntimeCommandResult {
	raw, _ := json.Marshal(value)
	var result api.NetworkProxyRuntimeCommandResult
	_ = json.Unmarshal(raw, &result)
	return &result
}

func (r *Runner) runCommand(ctx context.Context, command api.NetworkProxyRuntimeCommandInput) (*api.NetworkProxyRuntimeCommandResult, string, string) {
	if ctx.Err() != nil {
		return nil, "cancelled", "cancelled_before_execution"
	}
	switch string(command.Kind) {
	case "logs":
		limit := command.Limit
		if limit <= 0 || limit > 200 {
			limit = 200
		}
		cursor, _ := strconv.ParseInt(command.Cursor, 10, 64)
		logs := []hostLog{}
		for _, entry := range r.logs {
			if entry.Sequence > cursor {
				logs = append(logs, entry)
			}
			if len(logs) >= limit {
				break
			}
		}
		return commandResult(map[string]any{"logs": logs}), "succeeded", ""
	case "diagnostics":
		state := "available"
		if r.engine.health(ctx) != nil {
			state = "unavailable"
		}
		diagnostics := []any{map[string]any{"code": "host_protection_and_process", "state": state, "message": "Host process, configuration digest and guarded egress verification"}}
		diagnostics = append(diagnostics, r.engine.kernelArtifactDiagnostics()...)
		return commandResult(map[string]any{"diagnostics": diagnostics}), "succeeded", ""
	case "refresh":
		return nil, "unsupported", "source_update_required"
	}
	if r.engine.dialect() != domain.EngineMihomo || r.engine.desired.Publication == nil || r.engine.controller == nil {
		return nil, "unsupported", "verified_logical_inventory_required"
	}
	switch string(command.Kind) {
	case "groups":
		groups, err := r.readGroups(ctx)
		if err != nil {
			return nil, "unavailable", "group_readback_unavailable"
		}
		return &api.NetworkProxyRuntimeCommandResult{Groups: groups}, "succeeded", ""
	case "select":
		return r.selectNode(ctx, command)
	case "rules":
		return &api.NetworkProxyRuntimeCommandResult{Rules: r.engine.desired.Publication.Rules}, "succeeded", ""
	case "probe":
		return r.probeProfile(ctx, command)
	case "dns":
		return r.dnsStatus(ctx)
	default:
		return nil, "unsupported", "unsupported_command"
	}
}

func (r *Runner) readGroups(ctx context.Context) ([]api.NetworkProxyGroup, error) {
	if r.ingress == nil || r.engine.desired.Publication == nil {
		return nil, errors.New("installed group inventory unavailable")
	}
	groups := append([]api.NetworkProxyGroup(nil), r.engine.desired.Publication.Groups...)
	for i, group := range groups {
		original, ok := installedGroup(r.ingress.publication.Groups, group.GroupID)
		if !ok {
			return nil, errors.New("installed group inventory unavailable")
		}
		raw, err := r.engine.controller.request(ctx, http.MethodGet, "/proxies/"+url.PathEscape(group.GroupID), nil)
		if err != nil {
			return nil, err
		}
		var value struct {
			Now string   `json:"now"`
			All []string `json:"all"`
		}
		if json.Unmarshal(raw, &value) != nil {
			return nil, errors.New("group readback invalid")
		}
		narrowed, err := narrowedGroupReadback(group, original, value.Now, value.All)
		if err != nil {
			return nil, err
		}
		groups[i] = narrowed
	}
	return groups, nil
}

func (r *Runner) selectNode(ctx context.Context, command api.NetworkProxyRuntimeCommandInput) (*api.NetworkProxyRuntimeCommandResult, string, string) {
	groups, err := r.readGroups(ctx)
	if err != nil {
		return nil, "unavailable", "group_readback_unavailable"
	}
	valid := false
	for _, group := range groups {
		if group.GroupID == command.GroupID && group.Type == "select" {
			for _, node := range group.NodeIDs {
				if node == command.NodeID {
					valid = true
				}
			}
		}
	}
	if !valid {
		return nil, "unsupported", "logical_selection_invalid"
	}
	_, err = r.engine.controller.request(ctx, http.MethodPut, "/proxies/"+url.PathEscape(command.GroupID), map[string]string{"name": command.NodeID})
	if err != nil {
		return nil, "unknown", "selection_result_unknown"
	}
	groups, err = r.readGroups(ctx)
	if err != nil {
		return nil, "unknown", "selection_readback_unknown"
	}
	for _, group := range groups {
		if group.GroupID == command.GroupID && group.SelectedNodeID == command.NodeID {
			return &api.NetworkProxyRuntimeCommandResult{SelectedNodeID: command.NodeID}, "succeeded", ""
		}
	}
	return nil, "failed", "selection_readback_mismatch"
}

type probeProfile struct {
	URL    string `json:"url"`
	NodeID string `json:"nodeId"`
}

func (r *Runner) profiles() (map[string]probeProfile, error) {
	if r.config.ProbeProfilesFile == "" {
		return nil, errors.New("probe profiles unavailable")
	}
	raw, err := securefile.Read(r.config.ProbeProfilesFile, 32<<10, true)
	if err != nil {
		return nil, err
	}
	var profiles map[string]probeProfile
	if json.Unmarshal(raw, &profiles) != nil || len(profiles) > 128 {
		return nil, errors.New("invalid probe profiles")
	}
	for id, profile := range profiles {
		u, err := url.Parse(profile.URL)
		if !bindingID.MatchString(id) || err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
			return nil, errors.New("invalid probe profile")
		}
	}
	return profiles, nil
}

func (r *Runner) probeProfile(ctx context.Context, command api.NetworkProxyRuntimeCommandInput) (*api.NetworkProxyRuntimeCommandResult, string, string) {
	profiles, err := r.profiles()
	if err != nil {
		return nil, "unsupported", "approved_probe_profile_required"
	}
	profile, ok := profiles[command.ProbeProfileID]
	if !ok || (command.NodeID != "" && profile.NodeID != command.NodeID) || r.ingress == nil {
		return nil, "unsupported", "approved_probe_profile_required"
	}
	var outbound string
	for _, node := range r.engine.desired.Publication.Nodes {
		if node.NodeID == profile.NodeID {
			outbound = node.OutboundID
		}
	}
	b := r.engine.egress.backends[outbound]
	if b == nil {
		return nil, "unsupported", "probe_node_unavailable"
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
		if network != "tcp" {
			return nil, errors.New("probe transport unsupported")
		}
		return r.ingress.dial(ctx, address, profile.NodeID, outbound, b)
	}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, profile.URL, nil)
	if err != nil {
		return nil, "unsupported", "probe_profile_invalid"
	}
	start := time.Now()
	response, err := client.Do(request)
	if err != nil {
		return nil, "unavailable", "probe_failed"
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 16<<10))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, "unavailable", "probe_response_rejected"
	}
	return &api.NetworkProxyRuntimeCommandResult{LatencyMilliseconds: float32(time.Since(start).Microseconds()) / 1000}, "succeeded", ""
}

func (r *Runner) dnsStatus(ctx context.Context) (*api.NetworkProxyRuntimeCommandResult, string, string) {
	profiles, err := r.profiles()
	if err != nil || r.ingress == nil {
		return nil, "unavailable", "approved_dns_probe_required"
	}
	values := []any{}
	for id, profile := range profiles {
		u, _ := url.Parse(profile.URL)
		ip, err := r.ingress.scope.Resolve(ctx, u.Hostname())
		if err != nil {
			return nil, "unavailable", "guarded_dns_probe_failed"
		}
		values = append(values, map[string]any{"name": id, "addresses": []string{ip.String()}, "expiresAt": time.Now().Add(10 * time.Second)})
	}
	return commandResult(map[string]any{"dns": values}), "succeeded", ""
}
