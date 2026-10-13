package virtualization

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPVESyncReusesTicketAndSkipsDisabledGuestAgent(t *testing.T) {
	for _, scenario := range []struct {
		name        string
		enabled     bool
		configFails bool
	}{
		{name: "agent disabled"},
		{name: "agent enabled", enabled: true},
		{name: "config unavailable", configFails: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			logins, reads, agentReads := 0, 0, 0
			server := httptest.NewServer(pveSyncHandler(t, scenario.enabled, scenario.configFails, &logins, &reads, &agentReads))
			defer server.Close()
			adapter := NewPVEAdapter(server.Client())
			connection := Connection{Endpoint: server.URL, Credential: map[string]any{"username": "root@pam", "password": "test-password"}}
			for range 2 {
				result, err := adapter.SyncAssets(context.Background(), connection)
				if err != nil || result.Health.Status != "healthy" {
					t.Fatalf("sync = %#v, %v", result, err)
				}
				for _, asset := range result.Assets {
					if asset.Type == "qemu" && asset.Metadata["ipAddress"] != "10.0.0.21" {
						t.Fatalf("static IP lost: %#v", asset)
					}
				}
			}
			wantAgentReads := 0
			if scenario.enabled || scenario.configFails {
				wantAgentReads = 2
			}
			if logins != 2 || reads < 10 || agentReads != wantAgentReads {
				t.Fatalf("logins=%d reads=%d agent reads=%d", logins, reads, agentReads)
			}
			if len(connection.Credential) != 2 || connection.Credential["ticket"] != nil {
				t.Fatal("scan mutated saved credentials")
			}
		})
	}
}

func TestPVESyncAllowsSlowInventoryAndPreservesCancellation(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		deadline bool
		cancel   bool
	}{
		{name: "slow node succeeds"},
		{name: "caller deadline stops scan", deadline: true},
		{name: "caller cancellation stops scan", cancel: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := context.Background()
			cancel := context.CancelFunc(func() {})
			if scenario.deadline {
				ctx, cancel = context.WithTimeout(ctx, 20*time.Millisecond)
			} else if scenario.cancel {
				ctx, cancel = context.WithCancel(ctx)
			}
			defer cancel()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("inventory mutated provider: %s", r.Method)
				}
				switch r.URL.Path {
				case "/api2/json/nodes":
					writePVEData(w, []map[string]any{{"node": "pve-a", "status": "online"}})
				case "/api2/json/nodes/pve-a/qemu":
					if scenario.cancel {
						cancel()
					}
					select {
					case <-time.After(60 * time.Millisecond):
						writePVEData(w, []map[string]any{{"vmid": 101, "name": "vm", "status": "stopped"}})
					case <-r.Context().Done():
					}
				case "/api2/json/nodes/pve-a/qemu/101/config":
					writePVEAny(w, map[string]any{"name": "vm"})
				default:
					t.Errorf("unexpected inventory request: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			client := server.Client()
			client.Timeout = 20 * time.Millisecond
			adapter := NewPVEAdapter(client)
			result, err := adapter.SyncAssets(ctx, Connection{Endpoint: server.URL, Options: map[string]any{"syncResourceTypes": "vm"}})
			if err != nil {
				t.Fatal(err)
			}
			assertPVESlowSyncResult(t, ctx, result, scenario.deadline || scenario.cancel)
			if client.Timeout != 20*time.Millisecond || adapter.client != client {
				t.Fatal("sync changed the shared provider client")
			}
		})
	}
}

func assertPVESlowSyncResult(t *testing.T, ctx context.Context, result AssetSyncResult, canceled bool) {
	t.Helper()
	if canceled {
		if result.Health.Status != "degraded" || !strings.Contains(result.Health.Message, ctx.Err().Error()) {
			t.Fatalf("caller cancellation ignored: %#v", result)
		}
	} else if result.Health.Status != "healthy" || len(result.Assets) != 1 || result.Assets[0].Name != "vm" {
		t.Fatalf("slow node inventory failed: %#v", result)
	}
}

func TestPVESyncSkipsUnavailableStorageContent(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		storage map[string]any
	}{
		{name: "disabled on this node", storage: map[string]any{"enabled": 0, "active": 0}},
		{name: "disabled", storage: map[string]any{"enabled": false, "active": 1}},
		{name: "inactive", storage: map[string]any{"enabled": "1", "active": "0"}},
		{name: "available", storage: map[string]any{"enabled": 1, "active": 1}},
		{name: "availability unspecified", storage: map[string]any{}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			contentReads := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					t.Errorf("inventory mutated provider: %s", r.Method)
				}
				switch r.URL.Path {
				case "/api2/json/nodes":
					writePVEData(w, []map[string]any{{"node": "pve-a", "status": "online"}})
				case "/api2/json/nodes/pve-a/storage":
					item := map[string]any{"storage": "local", "type": "dir", "content": "iso"}
					for key, value := range scenario.storage {
						item[key] = value
					}
					writePVEData(w, []map[string]any{item})
				case "/api2/json/nodes/pve-a/storage/local/content":
					contentReads++
					writePVEData(w, []map[string]any{{"volid": "local:iso/demo.iso", "content": "iso"}})
				default:
					t.Errorf("unexpected inventory request: %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			result, err := NewPVEAdapter(server.Client()).SyncAssets(context.Background(), Connection{
				Endpoint: server.URL, Options: map[string]any{"syncResourceTypes": "storage,iso"},
			})
			wantReads := 0
			if scenario.name == "available" || scenario.name == "availability unspecified" {
				wantReads = 1
			}
			if err != nil || result.Health.Status != "healthy" || contentReads != wantReads || len(result.Assets) != 1+wantReads {
				t.Fatalf("sync = %#v, %v; content reads = %d, want %d", result, err, contentReads, wantReads)
			}
			if result.Assets[0].Type != "storage" || result.Assets[0].Name != "local" {
				t.Fatalf("storage inventory lost: %#v", result.Assets)
			}
		})
	}
}

func pveSyncHandler(t *testing.T, enabled, configFails bool, logins, reads, agentReads *int) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api2/json/access/ticket" {
			(*logins)++
			// #nosec G101 -- Synthetic PVE scan ticket; no external credential is used.
			writePVEAny(w, map[string]any{"ticket": "scan-ticket", "CSRFPreventionToken": "scan-csrf"})
			return
		}
		(*reads)++
		cookie, err := r.Cookie("PVEAuthCookie")
		if err != nil || cookie.Value != "scan-ticket" || r.Header.Get("CSRFPreventionToken") != "scan-csrf" {
			t.Error("inventory request did not reuse scan authentication")
		}
		if r.Method != http.MethodGet {
			t.Errorf("inventory request mutated provider: %s", r.Method)
		}
		switch r.URL.Path {
		case "/api2/json/nodes":
			writePVEData(w, []map[string]any{{"node": "pve-a"}})
		case "/api2/json/nodes/pve-a/network", "/api2/json/nodes/pve-a/storage":
			writePVEData(w, nil)
		case "/api2/json/nodes/pve-a/qemu":
			writePVEData(w, []map[string]any{{"vmid": 101, "name": "vm", "status": "running"}})
		case "/api2/json/nodes/pve-a/qemu/101/config":
			if configFails {
				http.Error(w, "config unavailable", http.StatusServiceUnavailable)
				return
			}
			agent := "0"
			if enabled {
				agent = "enabled=1"
			}
			writePVEAny(w, map[string]any{"agent": agent, "ipconfig0": "ip=10.0.0.21/24"})
		case "/api2/json/nodes/pve-a/qemu/101/agent/network-get-interfaces":
			(*agentReads)++
			writePVEAny(w, []map[string]any{{"name": "eth0", "ip-addresses": []map[string]any{{"ip-address": "10.0.0.21"}}}})
		default:
			t.Errorf("unexpected request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}
}

func TestPVESyncReportsIncompleteInventories(t *testing.T) {
	for _, failedPath := range []string{"network", "qemu", "storage", "storage/local/content"} {
		t.Run(failedPath, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api2/json/nodes/pve-a/"+failedPath {
					http.Error(w, "inventory unavailable", http.StatusServiceUnavailable)
					return
				}
				switch r.URL.Path {
				case "/api2/json/nodes":
					writePVEData(w, []map[string]any{{"node": "pve-a"}})
				case "/api2/json/nodes/pve-a/storage":
					writePVEData(w, []map[string]any{{"storage": "local", "type": "dir"}})
				default:
					writePVEData(w, nil)
				}
			}))
			defer server.Close()
			result, err := NewPVEAdapter(server.Client()).SyncAssets(context.Background(), Connection{Endpoint: server.URL})
			if err != nil || result.Health.Status != "degraded" || result.Health.HTTPStatus != http.StatusServiceUnavailable {
				t.Fatalf("incomplete scan reported as healthy: %#v, %v", result, err)
			}
		})
	}
}

func TestPVESyncKeepsSameNamedStorageOnDifferentNodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("inventory mutated provider: %s", r.Method)
		}
		switch r.URL.Path {
		case "/api2/json/nodes":
			writePVEData(w, []map[string]any{{"node": "pve-a"}, {"node": "pve-b"}})
		case "/api2/json/nodes/pve-a/storage", "/api2/json/nodes/pve-b/storage":
			writePVEData(w, []map[string]any{{"storage": "local", "type": "dir", "content": "images"}})
		case "/api2/json/nodes/pve-a/storage/local/content", "/api2/json/nodes/pve-b/storage/local/content":
			writePVEData(w, nil)
		default:
			t.Errorf("unexpected inventory request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	connection := Connection{Endpoint: server.URL, Options: map[string]any{"syncResourceTypes": "storage"}}
	adapter := NewPVEAdapter(server.Client())
	seen := map[string]string{}
	for range 2 {
		result, err := adapter.SyncAssets(t.Context(), connection)
		if err != nil || result.Health.Status != "healthy" || len(result.Assets) != 2 {
			t.Fatalf("sync=%#v err=%v", result, err)
		}
		for _, asset := range result.Assets {
			uid := asset.Metadata["uid"]
			if uid == "" || asset.Name != "local" || asset.Metadata["storage"] != "local" {
				t.Fatalf("storage identity or provider name lost: %#v", asset)
			}
			if previous := seen[asset.Node]; previous != "" && previous != uid {
				t.Fatal("storage identity changed across scans")
			}
			seen[asset.Node] = uid
		}
	}
	if seen["pve-a"] == seen["pve-b"] {
		t.Fatal("same-named storage on different nodes shares an upsert identity")
	}
}
