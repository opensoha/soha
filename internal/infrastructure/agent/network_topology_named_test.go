package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	cr "github.com/opensoha/soha-contracts/resource"
)

func TestTopologyNamedRootRequiresCapabilityAndResponseProof(t *testing.T) {
	for _, tc := range []struct {
		name              string
		capability, proof bool
		posts             int
		unsupported       bool
	}{
		{"old Agent", false, false, 0, true}, {"false capability", true, false, 1, true}, {"named support", true, true, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			posts := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					capabilities := []string{cr.NetworkTopologySnapshotFeature}
					if tc.capability {
						capabilities = append(capabilities, cr.NetworkTopologyNamedRootFeature)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"capabilities": capabilities}})
					return
				}
				posts++
				var input cr.NetworkTopologySnapshotRequest
				if json.NewDecoder(r.Body).Decode(&input) != nil || len(input.Queries) != 1 || input.Queries[0].Name != "api" {
					t.Error("named query lost")
				}
				version := cr.NetworkTopologySnapshotFeature
				if tc.proof {
					version = cr.NetworkTopologyNamedRootFeature
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": cr.NetworkTopologySnapshot{Version: version}})
			}))
			defer server.Close()
			client := &Client{baseURL: server.URL, httpClient: server.Client()}
			_, err := client.NetworkTopologySnapshot(context.Background(), cr.NetworkTopologySnapshotRequest{Queries: []cr.NetworkTopologySourceQuery{{SourceID: "kubernetes.services", Namespace: "team", Name: "api"}}})
			if errors.Is(err, cr.ErrNetworkTopologySnapshotUnsupported) != tc.unsupported || posts != tc.posts {
				t.Fatalf("old support guessed: posts=%d err=%v", posts, err)
			}
		})
	}
}
