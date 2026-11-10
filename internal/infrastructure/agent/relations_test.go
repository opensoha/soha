package agent

import (
	"context"
	"encoding/json"
	"errors"
	cr "github.com/opensoha/soha-contracts/resource"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestRelationSnapshotRequiresAdvertisedAgentProtocol(t *testing.T) {
	var reads atomic.Int64
	var enabled atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/platform/summary" {
			caps := []string{}
			if enabled.Load() {
				caps = append(caps, cr.RelationSnapshotFeature)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"capabilities": caps}})
			return
		}
		reads.Add(1)
		if r.URL.Path != "/api/v1/platform/resources/relations/snapshot" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": cr.RelationSnapshot{Version: cr.RelationSnapshotFeature, Sources: []cr.RelationSourceResult{}}})
	}))
	defer server.Close()
	client := &Client{baseURL: server.URL, httpClient: server.Client()}
	_, err := client.RelationSnapshot(context.Background(), cr.RelationSnapshotRequest{})
	if !errors.Is(err, cr.ErrRelationSnapshotUnsupported) || reads.Load() != 0 {
		t.Fatalf("old Agent accepted metadata protocol: %v", err)
	}
	enabled.Store(true)
	_, err = client.RelationSnapshot(context.Background(), cr.RelationSnapshotRequest{})
	if err != nil || reads.Load() != 1 {
		t.Fatalf("metadata request failed: %v", err)
	}
}
