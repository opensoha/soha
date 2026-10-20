package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	cr "github.com/opensoha/soha-contracts/resource"
)

func TestTopologyL4CapabilityKeepsOldAgentSourcesWorking(t *testing.T) {
	for _, supports := range []bool{false, true} {
		posted := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				caps := []string{cr.NetworkTopologySnapshotFeature}
				if supports {
					caps = append(caps, cr.NetworkTopologyGatewayL4Feature)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"capabilities": caps}})
				return
			}
			var input cr.NetworkTopologySnapshotRequest
			_ = json.NewDecoder(r.Body).Decode(&input)
			posted = len(input.Queries)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": cr.NetworkTopologySnapshot{Version: cr.NetworkTopologySnapshotFeature}})
		}))
		client := &Client{baseURL: server.URL, httpClient: server.Client()}
		view, err := client.NetworkTopologySnapshot(context.Background(), cr.NetworkTopologySnapshotRequest{Queries: []cr.NetworkTopologySourceQuery{{SourceID: "kubernetes.services", Namespace: "team"}, {SourceID: "gateway.tcproutes", Namespace: "team"}, {SourceID: "gateway.tlsroutes", Namespace: "team"}, {SourceID: "gateway.udproutes", Namespace: "team"}}})
		server.Close()
		if err != nil {
			t.Fatal(err)
		}
		if supports && posted != 4 || !supports && (posted != 1 || len(view.Sources) != 3) {
			t.Fatalf("capability isolation failed: supports=%v posted=%d view=%+v", supports, posted, view)
		}
		for _, source := range view.Sources {
			if source.Coverage.State != "unsupported" || source.Coverage.ReasonCode != "agent_upgrade_required" {
				t.Fatalf("missing upgrade evidence: %+v", source)
			}
		}
	}
}
