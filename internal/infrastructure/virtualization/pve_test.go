package virtualization

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPVEAdapterTestConnection(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		if r.URL.Path != "/api2/json/nodes" {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		writePVEData(w, []map[string]any{{"node": "pve-a", "status": "online"}})
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	result, err := adapter.TestConnection(context.Background(), Connection{Endpoint: server.URL})
	if err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
	if !result.Healthy || result.Status != "healthy" {
		t.Fatalf("result = %#v, want healthy", result)
	}
	if strings.Join(seen, ",") != "GET /api2/json/nodes" {
		t.Fatalf("seen paths = %#v", seen)
	}
}

func TestPVEAdapterTestConnectionDegradesOnHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	result, err := adapter.TestConnection(context.Background(), Connection{Endpoint: server.URL})
	if err != nil {
		t.Fatalf("TestConnection() error = %v", err)
	}
	if result.Healthy || result.Status != "degraded" || result.Message == "" {
		t.Fatalf("result = %#v, want degraded with message", result)
	}
}

func TestPVEAdapterUsesTokenHeaderAndExpectedPaths(t *testing.T) {
	var seen []string
	var authHeaders []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		switch r.URL.Path {
		case "/api2/json/nodes":
			writePVEData(w, []map[string]any{{"node": "pve-a", "status": "online"}})
		case "/api2/json/nodes/pve-a/qemu":
			writePVEData(w, []map[string]any{{"vmid": 101, "name": "vm-a", "status": "running"}})
		case "/api2/json/nodes/pve-a/storage":
			writePVEData(w, []map[string]any{{"storage": "local", "type": "dir"}})
		case "/api2/json/nodes/pve-a/storage/local/content":
			writePVEData(w, []map[string]any{{"volid": "local:iso/demo.iso", "content": "iso"}})
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	result, err := adapter.SyncAssets(context.Background(), Connection{
		Endpoint: server.URL,
		Credential: map[string]any{
			"tokenID":     "root@pam!kubecrux",
			"tokenSecret": "secret-token",
		},
	})
	if err != nil {
		t.Fatalf("SyncAssets() error = %v", err)
	}
	if result.Health.Status != "healthy" || len(result.Assets) != 4 {
		t.Fatalf("result = %#v", result)
	}
	for _, header := range authHeaders {
		if header != "PVEAPIToken=root@pam!kubecrux=secret-token" {
			t.Fatalf("Authorization header = %q", header)
		}
	}
	want := []string{
		"GET /api2/json/nodes",
		"GET /api2/json/nodes/pve-a/qemu",
		"GET /api2/json/nodes/pve-a/storage",
		"GET /api2/json/nodes/pve-a/storage/local/content",
	}
	if strings.Join(seen, ",") != strings.Join(want, ",") {
		t.Fatalf("seen paths = %#v", seen)
	}
}

func TestPVEAdapterCreateClonePayloadDoesNotLeakToken(t *testing.T) {
	var body string
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 0 {
			body = string(raw)
		}
		if strings.Contains(body, "secret-token") {
			t.Fatalf("payload leaked token: %s", body)
		}
		writePVEData(w, []map[string]any{})
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	vm, err := adapter.CreateVM(context.Background(), Connection{
		Endpoint: server.URL,
		Credential: map[string]any{
			"tokenID":     "root@pam!kubecrux",
			"tokenSecret": "secret-token",
		},
		Options: map[string]any{"vmid": "200"},
	}, CreateVMInput{
		Name:             "clone-a",
		Node:             "pve-a",
		CPU:              2,
		Memory:           "4096",
		Network:          "virtio,bridge=vmbr0",
		TemplateID:       "9000",
		SourceMode:       "template_clone",
		SourceRef:        "9000",
		StartAfterCreate: true,
	})
	if err != nil {
		t.Fatalf("CreateVM() error = %v", err)
	}
	if vm.ID != "200" || vm.Node != "pve-a" {
		t.Fatalf("vm = %#v", vm)
	}
	if !strings.Contains(body, `"newid":"200"`) || strings.Contains(body, "token") {
		t.Fatalf("payload = %s", body)
	}
	want := []string{
		"POST /api2/json/nodes/pve-a/qemu/9000/clone",
		"POST /api2/json/nodes/pve-a/qemu/200/status/start",
		"GET /api2/json/nodes/pve-a/qemu/200/status/current",
	}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestPVEAdapterCreateISOPayloadUsesProviderParams(t *testing.T) {
	var body string
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		if len(raw) > 0 {
			body = string(raw)
		}
		writePVEData(w, []map[string]any{})
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	vm, err := adapter.CreateVM(context.Background(), Connection{
		Endpoint: server.URL,
		Options:  map[string]any{"vmid": "201", "defaultNode": "pve-1", "defaultStorage": "local-lvm"},
	}, CreateVMInput{
		Name:       "iso-a",
		SourceMode: "iso_install",
		SourceRef:  "local:iso/ubuntu.iso",
		DiskSize:   "20Gi",
		ProviderParams: map[string]any{
			"bridge":  "vmbr0",
			"storage": "local-lvm",
		},
	})
	if err != nil {
		t.Fatalf("CreateVM() error = %v", err)
	}
	if vm.ID != "201" || vm.Node != "pve-1" || vm.Metadata["vmid"] != "201" {
		t.Fatalf("vm = %#v", vm)
	}
	if !strings.Contains(body, `"ide2":"local:iso/ubuntu.iso"`) || !strings.Contains(body, `"net0":"virtio,bridge=vmbr0"`) || !strings.Contains(body, `"scsi0":"local-lvm:20Gi"`) {
		t.Fatalf("payload = %s", body)
	}
	want := []string{"POST /api2/json/nodes/pve-1/qemu", "GET /api2/json/nodes/pve-1/qemu/201/status/current"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestPVEAdapterCreateISOPayloadFallsBackToNextIDAndFetchesStatus(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		switch r.URL.Path {
		case "/api2/json/cluster/nextid":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":"310"}`))
		case "/api2/json/nodes/pve-a/qemu":
			writePVEData(w, []map[string]any{})
		case "/api2/json/nodes/pve-a/qemu/310/status/current":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"name":"iso-b","status":"running","qmpstatus":"running"}}`))
		default:
			writePVEData(w, []map[string]any{})
		}
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	vm, err := adapter.CreateVM(context.Background(), Connection{
		Endpoint: server.URL,
		Options:  map[string]any{"defaultNode": "pve-a", "defaultStorage": "local-lvm"},
	}, CreateVMInput{
		Name:       "iso-b",
		SourceMode: "iso_install",
		SourceRef:  "local:iso/debian.iso",
		DiskSize:   "10Gi",
	})
	if err != nil {
		t.Fatalf("CreateVM() error = %v", err)
	}
	if vm.ID != "310" || vm.Name != "iso-b" || vm.Status != "running" || vm.Metadata["qmpstatus"] != "running" {
		t.Fatalf("vm = %#v", vm)
	}
	want := []string{
		"GET /api2/json/cluster/nextid",
		"POST /api2/json/nodes/pve-a/qemu",
		"GET /api2/json/nodes/pve-a/qemu/310/status/current",
	}
	if strings.Join(paths[:3], ",") != strings.Join(want, ",") {
		t.Fatalf("paths = %#v", paths)
	}
}

func TestPVEAdapterGetConsoleURLReturnsBackendWebsocketURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api2/json/nodes/pve-a/qemu/101/vncproxy":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":{"ticket":"ticket-1","port":5901}}`))
		default:
			writePVEData(w, []map[string]any{})
		}
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	result, err := adapter.GetConsoleURL(context.Background(), Connection{Endpoint: server.URL}, VM{ID: "vm-local", Node: "pve-a", Metadata: map[string]string{"vmid": "101"}})
	if err != nil {
		t.Fatalf("GetConsoleURL() error = %v", err)
	}
	if !result.Ready || result.Provider != "pve" || result.ProxyMode != "backend-ws-proxy" {
		t.Fatalf("result = %#v", result)
	}
	if result.URL != "/api/v1/virtualization/vms/vm-local/console/novnc" {
		t.Fatalf("result.URL = %q", result.URL)
	}
	if !strings.Contains(result.BackendURL, "/api2/json/nodes/pve-a/qemu/101/vncwebsocket") || !strings.Contains(result.BackendURL, "port=5901") || !strings.Contains(result.BackendURL, "vncticket=ticket-1") {
		t.Fatalf("result.BackendURL = %q", result.BackendURL)
	}
}

func TestPVEAdapterPowerActions(t *testing.T) {
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		writePVEData(w, []map[string]any{})
	}))
	defer server.Close()

	adapter := NewPVEAdapter(server.Client())
	conn := Connection{Endpoint: server.URL, Credential: map[string]any{"ticket": "ticket-value", "csrfToken": "csrf"}}
	for _, action := range []PowerAction{PowerActionStart, PowerActionStop, PowerActionRestart, PowerActionDelete} {
		if _, err := adapter.PowerAction(context.Background(), conn, VM{ID: "100", Node: "pve-a"}, action); err != nil {
			t.Fatalf("PowerAction(%s) error = %v", action, err)
		}
	}
	want := []string{
		"POST /api2/json/nodes/pve-a/qemu/100/status/start",
		"POST /api2/json/nodes/pve-a/qemu/100/status/stop",
		"POST /api2/json/nodes/pve-a/qemu/100/status/reboot",
		"DELETE /api2/json/nodes/pve-a/qemu/100",
	}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("paths = %#v", paths)
	}
}

func writePVEData(w http.ResponseWriter, data []map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}
