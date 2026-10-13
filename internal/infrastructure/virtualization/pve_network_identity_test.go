package virtualization

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPVESyncKeepsSameNamedNetworkOnDifferentNodes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("inventory mutated provider: %s", r.Method)
		}
		switch r.URL.Path {
		case "/api2/json/nodes":
			writePVEData(w, []map[string]any{{"node": "pve-a"}, {"node": "pve-b"}})
		case "/api2/json/nodes/pve-a/network", "/api2/json/nodes/pve-b/network":
			writePVEData(w, []map[string]any{{"iface": "vmbr0", "type": "bridge", "active": 1}})
		default:
			t.Errorf("unexpected inventory request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	connection := Connection{Endpoint: server.URL, Options: map[string]any{"syncResourceTypes": "network"}}
	adapter := NewPVEAdapter(server.Client())
	seen := map[string]string{}
	for range 2 {
		result, err := adapter.SyncAssets(t.Context(), connection)
		if err != nil || result.Health.Status != "healthy" || len(result.Assets) != 2 {
			t.Fatalf("sync=%#v err=%v", result, err)
		}
		for _, asset := range result.Assets {
			uid := asset.Metadata["uid"]
			if uid == "" || asset.Name != "vmbr0" || asset.Metadata["sourceRef"] != "vmbr0" || asset.Metadata["network"] != "vmbr0" {
				t.Fatalf("network identity or provider name lost: %#v", asset)
			}
			if previous := seen[asset.Node]; previous != "" && previous != uid {
				t.Fatal("network identity changed across scans")
			}
			seen[asset.Node] = uid
		}
	}
	if seen["pve-a"] == seen["pve-b"] {
		t.Fatal("same-named networks share an upsert identity")
	}
}
