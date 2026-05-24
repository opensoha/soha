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
		var storages pveDataEnvelope
		if err := a.do(ctx, connection, http.MethodGet, fmt.Sprintf("/nodes/%s/storage", url.PathEscape(nodeName)), nil, &storages); err == nil {
			for _, item := range storages.Data {
				assets = append(assets, Asset{
					Type:   "storage",
					Name:   stringFromAny(item["storage"]),
					Node:   nodeName,
					Status: stringFromMap(item, "type"),
					Metadata: map[string]string{
						"storage": stringFromAny(item["storage"]),
						"type":    stringFromMap(item, "type"),
					},
				})
			}
		}
		var storageContent pveDataEnvelope
		if err := a.do(ctx, connection, http.MethodGet, fmt.Sprintf("/nodes/%s/storage/local/content", url.PathEscape(nodeName)), nil, &storageContent); err == nil {
			for _, item := range storageContent.Data {
				contentType := stringFromMap(item, "content")
				assetType := "storage_content"
				if contentType == "iso" {
					assetType = "iso"
				} else if contentType == "vztmpl" || strings.Contains(strings.ToLower(stringFromAny(item["volid"])), "template") {
					assetType = "template"
				}
				assets = append(assets, Asset{
					Type:   assetType,
					Name:   stringFromAny(item["volid"]),
					Node:   nodeName,
					Status: contentType,
					Metadata: map[string]string{
						"volid":       stringFromAny(item["volid"]),
						"contentType": contentType,
						"node":        nodeName,
					},
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
		node = stringOptionValue(connection.Options, "defaultNode")
	}
	if node == "" {
		return VM{}, invalidf("node is required")
	}
	providerStorage := stringFromAny(input.ProviderParams["storage"])
	if providerStorage == "" {
		providerStorage = stringOptionValue(connection.Options, "defaultStorage")
	}
	providerBridge := stringFromAny(input.ProviderParams["bridge"])
	if providerBridge == "" {
		providerBridge = stringOptionValue(connection.Options, "defaultBridge")
	}
	providerISO := stringFromAny(input.ProviderParams["iso"])
	vmid := stringFromAny(input.ProviderParams["vmid"])
	if vmid == "" {
		vmid = stringOptionValue(connection.Options, "vmid")
	}
	if vmid == "" {
		vmid = stringOptionValue(connection.Options, "nextVmid")
	}
	if vmid == "" {
		nextID, err := a.nextVMID(ctx, connection)
		if err != nil {
			return VM{}, err
		}
		vmid = nextID
	}
	payload := map[string]any{
		"name":   input.Name,
		"cores":  input.CPU,
		"memory": input.Memory,
	}
	if input.SourceMode == "template_clone" || input.TemplateID != "" {
		templateID := firstNonEmpty(input.TemplateID, input.SourceRef, input.BootImage)
		if templateID == "" {
			return VM{}, invalidf("template source is required")
		}
		if providerStorage != "" {
			payload["storage"] = providerStorage
		}
		endpoint := fmt.Sprintf("/nodes/%s/qemu/%s/clone", url.PathEscape(node), url.PathEscape(templateID))
		payload["newid"] = vmid
		if err := a.do(ctx, connection, http.MethodPost, endpoint, payload, nil); err != nil {
			return VM{}, err
		}
		if input.StartAfterCreate {
			if err := a.do(ctx, connection, http.MethodPost, fmt.Sprintf("/nodes/%s/qemu/%s/status/start", url.PathEscape(node), url.PathEscape(vmid)), nil, nil); err != nil {
				return VM{}, err
			}
		}
		return a.fetchVM(ctx, connection, node, vmid, input.Name)
	}
	payload["vmid"] = vmid
	if input.DiskSize != "" {
		diskRef := input.DiskSize
		if providerStorage != "" {
			diskRef = fmt.Sprintf("%s:%s", providerStorage, input.DiskSize)
		}
		payload["scsi0"] = diskRef
	}
	if providerBridge != "" {
		payload["net0"] = fmt.Sprintf("virtio,bridge=%s", providerBridge)
	} else if input.Network != "" {
		payload["net0"] = input.Network
	}
	if providerISO == "" {
		providerISO = firstNonEmpty(input.SourceRef, input.BootImage)
	}
	if providerISO != "" {
		payload["ide2"] = providerISO
	}
	if ciuser := stringFromAny(input.ProviderParams["ciuser"]); ciuser != "" {
		payload["ciuser"] = ciuser
	}
	if sshKeys := stringFromAny(input.ProviderParams["sshkeys"]); sshKeys != "" {
		payload["sshkeys"] = sshKeys
	}
	endpoint := fmt.Sprintf("/nodes/%s/qemu", url.PathEscape(node))
	if err := a.do(ctx, connection, http.MethodPost, endpoint, payload, nil); err != nil {
		return VM{}, err
	}
	if input.StartAfterCreate {
		if err := a.do(ctx, connection, http.MethodPost, fmt.Sprintf("/nodes/%s/qemu/%s/status/start", url.PathEscape(node), url.PathEscape(vmid)), nil, nil); err != nil {
			return VM{}, err
		}
	}
	return a.fetchVM(ctx, connection, node, vmid, input.Name)
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

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (a *PVEAdapter) nextVMID(ctx context.Context, connection Connection) (string, error) {
	var payload struct {
		Data any `json:"data"`
	}
	if err := a.do(ctx, connection, http.MethodGet, "/cluster/nextid", nil, &payload); err != nil {
		return "", err
	}
	vmid := stringFromAny(payload.Data)
	if vmid == "" {
		return "", invalidf("pve next vmid is empty")
	}
	return vmid, nil
}

func (a *PVEAdapter) fetchVM(ctx context.Context, connection Connection, node, vmid, fallbackName string) (VM, error) {
	var payload struct {
		Data map[string]any `json:"data"`
	}
	if err := a.do(ctx, connection, http.MethodGet, fmt.Sprintf("/nodes/%s/qemu/%s/status/current", url.PathEscape(node), url.PathEscape(vmid)), nil, &payload); err != nil {
		return VM{ID: vmid, Name: fallbackName, Node: node, Status: "created", Metadata: map[string]string{"vmid": vmid}}, nil
	}
	status := firstNonEmpty(stringFromMap(payload.Data, "status"), "created")
	name := firstNonEmpty(stringFromMap(payload.Data, "name"), fallbackName)
	metadata := map[string]string{"vmid": vmid}
	if qmpStatus := stringFromMap(payload.Data, "qmpstatus"); qmpStatus != "" {
		metadata["qmpstatus"] = qmpStatus
	}
	return VM{ID: vmid, Name: name, Node: node, Status: status, Metadata: metadata}, nil
}

func (a *PVEAdapter) GetVMMetrics(ctx context.Context, connection Connection, vm VM, rangeMinutes, stepSeconds int) (VMMetricsResult, error) {
	vmid := vm.Metadata["vmid"]
	node := vm.Node
	if vmid == "" || node == "" {
		return VMMetricsResult{Message: "VM metadata incomplete", Ready: false, Source: "none"}, nil
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
		return VMMetricsResult{Message: err.Error(), Ready: false, Source: "pve-rrd"}, nil
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

	return VMMetricsResult{Series: series, Ready: len(series) > 0, Source: "pve-rrd"}, nil
}

func (a *PVEAdapter) GetConsoleURL(ctx context.Context, connection Connection, vm VM) (ConsoleURLResult, error) {
	vmid := vm.Metadata["vmid"]
	node := vm.Node
	if vmid == "" || node == "" {
		return ConsoleURLResult{Message: "VM metadata incomplete", Ready: false, Provider: "pve", Type: "novnc", ProxyMode: "backend-ws-proxy"}, nil
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
		return ConsoleURLResult{Message: err.Error(), Ready: false, Provider: "pve", Type: "novnc", ProxyMode: "backend-ws-proxy"}, err
	}

	base, err := url.Parse(strings.TrimRight(connection.Endpoint, "/"))
	if err != nil || base.Scheme == "" || base.Host == "" {
		return ConsoleURLResult{Message: "valid pve endpoint is required", Ready: false, Provider: "pve", Type: "novnc", ProxyMode: "backend-ws-proxy"}, invalidf("valid pve endpoint is required")
	}
	base.Path = path.Join(base.Path, fmt.Sprintf("/api2/json/nodes/%s/qemu/%s/vncwebsocket", url.PathEscape(node), url.PathEscape(vmid)))
	query := base.Query()
	query.Set("port", fmt.Sprintf("%d", ticketResponse.Data.Port))
	query.Set("vncticket", ticketResponse.Data.Ticket)
	base.RawQuery = query.Encode()

	return ConsoleURLResult{
		Type:       "novnc",
		URL:        fmt.Sprintf("/api/v1/virtualization/vms/%s/console/novnc", vm.ID),
		BackendURL: base.String(),
		Token:      ticketResponse.Data.Ticket,
		Ready:      true,
		Provider:   "pve",
		ProxyMode:  "backend-ws-proxy",
	}, nil
}
