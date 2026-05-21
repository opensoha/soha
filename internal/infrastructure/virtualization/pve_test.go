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
	if result.Health.Status != "healthy" || len(result.Assets) != 3 {
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
	}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("paths = %#v", paths)
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
