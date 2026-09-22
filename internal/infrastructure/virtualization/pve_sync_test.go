package virtualization

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
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

func pveSyncHandler(t *testing.T, enabled, configFails bool, logins, reads, agentReads *int) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api2/json/access/ticket" {
			(*logins)++
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
