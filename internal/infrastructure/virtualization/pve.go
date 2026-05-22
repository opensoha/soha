package virtualization

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

type PVEAdapter struct {
	client *http.Client
}

func NewPVEAdapter(client *http.Client) *PVEAdapter {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &PVEAdapter{client: client}
}

func (a *PVEAdapter) TestConnection(ctx context.Context, connection Connection) (ConnectionTestResult, error) {
	var payload pveDataEnvelope
	if err := a.do(ctx, connection, http.MethodGet, "/nodes", nil, &payload); err != nil {
		return ConnectionTestResult{Healthy: false, Status: "degraded", Message: err.Error()}, nil
	}
	return ConnectionTestResult{Healthy: true, Status: "healthy"}, nil
}

func (a *PVEAdapter) SyncAssets(ctx context.Context, connection Connection) (AssetSyncResult, error) {
	var nodes pveDataEnvelope
	if err := a.do(ctx, connection, http.MethodGet, "/nodes", nil, &nodes); err != nil {
		return AssetSyncResult{Health: AssetHealth{Status: "degraded", Message: err.Error()}}, nil
	}
	assets := make([]Asset, 0)
	for _, node := range nodes.Data {
		nodeName := stringFromMap(node, "node")
		if nodeName == "" {
			continue
		}
		assets = append(assets, Asset{Type: "node", Name: nodeName, Status: stringFromMap(node, "status")})
		var qemu pveDataEnvelope
		if err := a.do(ctx, connection, http.MethodGet, fmt.Sprintf("/nodes/%s/qemu", url.PathEscape(nodeName)), nil, &qemu); err != nil {
			return AssetSyncResult{Health: AssetHealth{Status: "degraded", Message: err.Error()}, Assets: assets}, nil
		}
		for _, vm := range qemu.Data {
			assets = append(assets, Asset{
				Type:   "qemu",
				Name:   stringFromAny(vm["name"]),
				Node:   nodeName,
				Status: stringFromMap(vm, "status"),
				Metadata: map[string]string{
					"vmid": stringFromAny(vm["vmid"]),
				},
			})
		}
		var storage pveDataEnvelope
		if err := a.do(ctx, connection, http.MethodGet, fmt.Sprintf("/nodes/%s/storage/local/content", url.PathEscape(nodeName)), nil, &storage); err == nil {
			for _, item := range storage.Data {
				assets = append(assets, Asset{
					Type:   "storage_content",
					Name:   stringFromAny(item["volid"]),
					Node:   nodeName,
					Status: stringFromMap(item, "content"),
				})
			}
		}
	}
	return AssetSyncResult{Health: AssetHealth{Status: "healthy"}, Assets: assets}, nil
}

func (a *PVEAdapter) CreateVM(ctx context.Context, connection Connection, input CreateVMInput) (VM, error) {
	if input.Name == "" {
		return VM{}, invalidf("vm name is required")
	}
	node := input.Node
	if node == "" {
		return VM{}, invalidf("node is required")
	}
	vmid := stringOptionValue(connection.Options, "vmid")
	if vmid == "" {
		vmid = stringOptionValue(connection.Options, "nextVmid")
	}
	if vmid == "" {
		return VM{}, invalidf("vmid option is required")
	}
	payload := map[string]any{
		"name":   input.Name,
		"cores":  input.CPU,
		"memory": input.Memory,
	}
	if input.DiskSize != "" {
		payload["scsi0"] = input.DiskSize
	}
	if input.Network != "" {
		payload["net0"] = input.Network
	}
	if input.CloudInit != "" {
		payload["ciuser"] = input.CloudInit
	}
	var endpoint string
	if input.TemplateID != "" {
		endpoint = fmt.Sprintf("/nodes/%s/qemu/%s/clone", url.PathEscape(node), url.PathEscape(input.TemplateID))
		payload["newid"] = vmid
	} else {
		endpoint = fmt.Sprintf("/nodes/%s/qemu", url.PathEscape(node))
		payload["vmid"] = vmid
		if input.BootImage != "" {
			payload["ide2"] = input.BootImage
		}
	}
	if err := a.do(ctx, connection, http.MethodPost, endpoint, payload, nil); err != nil {
		return VM{}, err
	}
	if input.StartAfterCreate {
		if err := a.do(ctx, connection, http.MethodPost, fmt.Sprintf("/nodes/%s/qemu/%s/status/start", url.PathEscape(node), url.PathEscape(vmid)), nil, nil); err != nil {
			return VM{}, err
		}
	}
	return VM{ID: vmid, Name: input.Name, Node: node, Status: "created"}, nil
}

func (a *PVEAdapter) PowerAction(ctx context.Context, connection Connection, vm VM, action PowerAction) (PowerActionResult, error) {
	if vm.ID == "" {
		return PowerActionResult{}, invalidf("vm id is required")
	}
	if vm.Node == "" {
		return PowerActionResult{}, invalidf("node is required")
	}
	var endpoint string
	var method = http.MethodPost
	switch action {
	case PowerActionStart:
		endpoint = fmt.Sprintf("/nodes/%s/qemu/%s/status/start", url.PathEscape(vm.Node), url.PathEscape(vm.ID))
	case PowerActionStop:
		endpoint = fmt.Sprintf("/nodes/%s/qemu/%s/status/stop", url.PathEscape(vm.Node), url.PathEscape(vm.ID))
	case PowerActionRestart:
		endpoint = fmt.Sprintf("/nodes/%s/qemu/%s/status/reboot", url.PathEscape(vm.Node), url.PathEscape(vm.ID))
	case PowerActionDelete:
		method = http.MethodDelete
		endpoint = fmt.Sprintf("/nodes/%s/qemu/%s", url.PathEscape(vm.Node), url.PathEscape(vm.ID))
	default:
		return PowerActionResult{}, invalidf("unsupported power action %q", action)
	}
	if err := a.do(ctx, connection, method, endpoint, nil, nil); err != nil {
		return PowerActionResult{}, err
	}
	return PowerActionResult{Accepted: true, Action: action}, nil
}

func (a *PVEAdapter) do(ctx context.Context, connection Connection, method string, endpoint string, payload map[string]any, out any) error {
	base, err := url.Parse(strings.TrimRight(connection.Endpoint, "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return invalidf("valid pve endpoint is required")
	}
	base.Path = path.Join(base.Path, "/api2/json", endpoint)
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal pve payload: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	req, err := http.NewRequestWithContext(ctx, method, base.String(), body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	applyPVEAuth(req, connection.Credential)
	resp, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("pve api returned status %d", resp.StatusCode)
	}
	if out == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode pve response: %w", err)
	}
	return nil
}

func applyPVEAuth(req *http.Request, credential map[string]any) {
	tokenID := stringFromAny(credential["tokenID"])
	tokenSecret := stringFromAny(credential["tokenSecret"])
	if tokenID != "" && tokenSecret != "" {
		req.Header.Set("Authorization", "PVEAPIToken="+tokenID+"="+tokenSecret)
		return
	}
	ticket := stringFromAny(credential["ticket"])
	if ticket != "" {
		req.AddCookie(&http.Cookie{Name: "PVEAuthCookie", Value: ticket})
		if csrf := stringFromAny(credential["csrfToken"]); csrf != "" {
			req.Header.Set("CSRFPreventionToken", csrf)
		}
	}
}

type pveDataEnvelope struct {
	Data []map[string]any `json:"data"`
}

func stringOptionValue(options map[string]any, key string) string {
	value, ok := stringOption(options, key)
	if ok {
		return value
	}
	if options == nil {
		return ""
	}
	return stringFromAny(options[key])
}

func stringFromMap(values map[string]any, key string) string {
	return stringFromAny(values[key])
}

func stringFromAny(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case float64:
		return fmt.Sprintf("%.0f", typed)
	case int:
		return fmt.Sprintf("%d", typed)
	case int64:
		return fmt.Sprintf("%d", typed)
	default:
		return ""
	}
}

func (a *PVEAdapter) GetVMMetrics(ctx context.Context, connection Connection, vm VM, rangeMinutes, stepSeconds int) (VMMetricsResult, error) {
	vmid := vm.Metadata["vmid"]
	node := vm.Node
	if vmid == "" || node == "" {
		return VMMetricsResult{Message: "VM metadata incomplete"}, nil
	}

	var rrdData pveDataEnvelope
	timeframe := "hour"
	if rangeMinutes > 60 {
		timeframe = "day"
	}

	err := a.do(ctx, connection, http.MethodGet,
		fmt.Sprintf("/nodes/%s/qemu/%s/rrddata?timeframe=%s", url.PathEscape(node), url.PathEscape(vmid), timeframe),
		nil, &rrdData)

	if err != nil {
		return VMMetricsResult{Message: err.Error()}, nil
	}

	var series []MetricSeries
	cpuPoints := []MetricPoint{}
	memPoints := []MetricPoint{}
	netRxPoints := []MetricPoint{}
	netTxPoints := []MetricPoint{}

	for _, point := range rrdData.Data {
		ts := int64(0)
		if timeVal, ok := point["time"].(float64); ok {
			ts = int64(timeVal)
		}
		if cpu, ok := point["cpu"].(float64); ok {
			cpuPoints = append(cpuPoints, MetricPoint{Timestamp: ts, Value: cpu})
		}
		if mem, ok := point["mem"].(float64); ok {
			memPoints = append(memPoints, MetricPoint{Timestamp: ts, Value: mem})
		}
		if netin, ok := point["netin"].(float64); ok {
			netRxPoints = append(netRxPoints, MetricPoint{Timestamp: ts, Value: netin})
		}
		if netout, ok := point["netout"].(float64); ok {
			netTxPoints = append(netTxPoints, MetricPoint{Timestamp: ts, Value: netout})
		}
	}

	if len(cpuPoints) > 0 {
		series = append(series, MetricSeries{Key: "cpu", Label: "CPU", Unit: "percent", Points: cpuPoints})
	}
	if len(memPoints) > 0 {
		series = append(series, MetricSeries{Key: "memory", Label: "Memory", Unit: "bytes", Points: memPoints})
	}
	if len(netRxPoints) > 0 {
		series = append(series, MetricSeries{Key: "networkRx", Label: "Network RX", Unit: "bytes/s", Points: netRxPoints})
	}
	if len(netTxPoints) > 0 {
		series = append(series, MetricSeries{Key: "networkTx", Label: "Network TX", Unit: "bytes/s", Points: netTxPoints})
	}

	return VMMetricsResult{Series: series}, nil
}

func (a *PVEAdapter) GetConsoleURL(ctx context.Context, connection Connection, vm VM) (ConsoleURLResult, error) {
	vmid := vm.Metadata["vmid"]
	node := vm.Node
	if vmid == "" || node == "" {
		return ConsoleURLResult{Message: "VM metadata incomplete"}, nil
	}

	var ticketResponse struct {
		Data struct {
			Ticket string `json:"ticket"`
			Port   int    `json:"port"`
		} `json:"data"`
	}

	err := a.do(ctx, connection, http.MethodPost,
		fmt.Sprintf("/nodes/%s/qemu/%s/vncproxy", url.PathEscape(node), url.PathEscape(vmid)),
		map[string]any{"websocket": "1"},
		&ticketResponse)

	if err != nil {
		return ConsoleURLResult{Message: err.Error()}, err
	}

	vncURL := fmt.Sprintf("/api/v1/virtualization/vms/%s/console/novnc", vm.ID)

	return ConsoleURLResult{
		Type:  "novnc",
		URL:   vncURL,
		Token: ticketResponse.Data.Ticket,
	}, nil
}
