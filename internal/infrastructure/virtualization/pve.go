package virtualization

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
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
			vmid := stringFromAny(vm["vmid"])
			vmName := firstNonEmpty(stringFromAny(vm["name"]), "vm-"+vmid)
			assetType := "qemu"
			status := stringFromMap(vm, "status")
			if boolFromAny(vm["template"]) {
				assetType = "template"
				status = firstNonEmpty(status, "template")
			}
			metadata := map[string]string{
				"vmid":      vmid,
				"sourceRef": vmid,
				"node":      nodeName,
			}
			if cpus := stringFromAny(firstNonNil(vm["cpus"], vm["cores"])); cpus != "" {
				metadata["cpu"] = cpus
			}
			if memory := stringFromAny(firstNonNil(vm["maxmem"], vm["mem"])); memory != "" {
				metadata["memory"] = memory
			}
			assets = append(assets, Asset{
				Type:     assetType,
				Name:     vmName,
				Node:     nodeName,
				Status:   status,
				Metadata: metadata,
			})
		}
		var storages pveDataEnvelope
		if err := a.do(ctx, connection, http.MethodGet, fmt.Sprintf("/nodes/%s/storage", url.PathEscape(nodeName)), nil, &storages); err == nil {
			for _, item := range storages.Data {
				storageName := stringFromAny(item["storage"])
				assets = append(assets, Asset{
					Type:   "storage",
					Name:   storageName,
					Node:   nodeName,
					Status: stringFromMap(item, "type"),
					Metadata: map[string]string{
						"storage": storageName,
						"type":    stringFromMap(item, "type"),
						"content": stringFromMap(item, "content"),
					},
				})
				if storageName == "" || !pveStorageSupportsContent(item) {
					continue
				}
				var storageContent pveDataEnvelope
				if err := a.do(ctx, connection, http.MethodGet, fmt.Sprintf("/nodes/%s/storage/%s/content", url.PathEscape(nodeName), url.PathEscape(storageName)), nil, &storageContent); err != nil {
					continue
				}
				for _, content := range storageContent.Data {
					contentType := stringFromMap(content, "content")
					volID := firstNonEmpty(stringFromAny(content["volid"]), stringFromAny(content["name"]))
					assetType := "storage_content"
					lowerVolID := strings.ToLower(volID)
					if contentType == "iso" {
						assetType = "iso"
					} else if contentType == "vztmpl" {
						assetType = "lxc_template"
					} else if strings.Contains(lowerVolID, "template") {
						assetType = "template"
					} else if contentType == "images" || contentType == "rootdir" {
						assetType = "image"
					}
					metadata := map[string]string{
						"volid":       volID,
						"sourceRef":   volID,
						"contentType": contentType,
						"node":        nodeName,
						"storage":     storageName,
					}
					if format := stringFromAny(content["format"]); format != "" {
						metadata["format"] = format
					}
					if size := stringFromAny(content["size"]); size != "" {
						metadata["size"] = size
					}
					assets = append(assets, Asset{
						Type:     assetType,
						Name:     volID,
						Node:     nodeName,
						Status:   contentType,
						Metadata: metadata,
					})
				}
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
	payload := map[string]any{"name": input.Name}
	if input.CPU > 0 {
		payload["cores"] = input.CPU
	}
	if memoryMB := normalizePVEMemoryMB(input.Memory); memoryMB > 0 {
		payload["memory"] = memoryMB
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
		diskRef := normalizePVEDiskSize(input.DiskSize)
		if providerStorage != "" {
			diskRef = fmt.Sprintf("%s:%s", providerStorage, diskRef)
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
		payload["ide2"] = normalizePVEISORef(providerISO)
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
	resp, err := a.clientFor(connection).Do(req)
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

func (a *PVEAdapter) clientFor(connection Connection) *http.Client {
	if !connection.InsecureSkipTLSVerify {
		return a.client
	}
	clone := *a.client
	clone.Transport = pveInsecureTransport(a.client.Transport)
	return &clone
}

func pveInsecureTransport(base http.RoundTripper) http.RoundTripper {
	transport, ok := base.(*http.Transport)
	if !ok || transport == nil {
		transport = http.DefaultTransport.(*http.Transport)
	}
	clone := transport.Clone()
	if clone.TLSClientConfig == nil {
		clone.TLSClientConfig = &tls.Config{}
	} else {
		clone.TLSClientConfig = clone.TLSClientConfig.Clone()
	}
	clone.TLSClientConfig.InsecureSkipVerify = true
	return clone
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

func boolFromAny(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		normalized := strings.ToLower(strings.TrimSpace(typed))
		return normalized == "1" || normalized == "true" || normalized == "yes"
	case float64:
		return typed != 0
	case int:
		return typed != 0
	case int64:
		return typed != 0
	default:
		return false
	}
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func pveStorageSupportsContent(item map[string]any) bool {
	content := strings.ToLower(strings.TrimSpace(stringFromMap(item, "content")))
	if content == "" {
		return true
	}
	for _, part := range strings.Split(content, ",") {
		switch strings.TrimSpace(part) {
		case "iso", "vztmpl", "images", "rootdir", "snippets":
			return true
		}
	}
	return false
}

func normalizePVEMemoryMB(value string) int {
	raw := strings.TrimSpace(strings.ToLower(value))
	if raw == "" {
		return 0
	}
	multiplier := 1.0
	for _, suffix := range []struct {
		text       string
		multiplier float64
	}{
		{"gib", 1024},
		{"gi", 1024},
		{"gb", 1024},
		{"g", 1024},
		{"mib", 1},
		{"mi", 1},
		{"mb", 1},
		{"m", 1},
	} {
		if strings.HasSuffix(raw, suffix.text) {
			raw = strings.TrimSpace(strings.TrimSuffix(raw, suffix.text))
			multiplier = suffix.multiplier
			break
		}
	}
	parsed, err := strconv.ParseFloat(raw, 64)
	if err != nil || parsed <= 0 {
		return 0
	}
	return int(parsed*multiplier + 0.5)
}

func normalizePVEDiskSize(value string) string {
	raw := strings.TrimSpace(value)
	lower := strings.ToLower(raw)
	for _, suffix := range []struct {
		text string
		unit string
	}{
		{"gib", "G"},
		{"gi", "G"},
		{"gb", "G"},
		{"g", "G"},
		{"mib", "M"},
		{"mi", "M"},
		{"mb", "M"},
		{"m", "M"},
	} {
		if strings.HasSuffix(lower, suffix.text) {
			number := strings.TrimSpace(raw[:len(raw)-len(suffix.text)])
			if number == "" {
				return raw
			}
			return number + suffix.unit
		}
	}
	if _, err := strconv.ParseFloat(raw, 64); err == nil {
		return raw + "G"
	}
	return raw
}

func normalizePVEISORef(value string) string {
	raw := strings.TrimSpace(value)
	if raw == "" || strings.Contains(strings.ToLower(raw), "media=") {
		return raw
	}
	lower := strings.ToLower(raw)
	if strings.Contains(lower, ":iso/") || strings.HasSuffix(lower, ".iso") {
		return raw + ",media=cdrom"
	}
	return raw
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
		Type:                  "novnc",
		URL:                   fmt.Sprintf("/api/v1/virtualization/vms/%s/console/novnc", vm.ID),
		BackendURL:            base.String(),
		Token:                 ticketResponse.Data.Ticket,
		Ready:                 true,
		Provider:              "pve",
		ProxyMode:             "backend-ws-proxy",
		InsecureSkipTLSVerify: connection.InsecureSkipTLSVerify,
	}, nil
}
