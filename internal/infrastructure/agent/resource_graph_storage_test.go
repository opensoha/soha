package agent

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestStorageGraphRequiresAgentCapability(t *testing.T) {
	for _, tc := range []struct {
		label, kind, namespace string
		probeStatus            int
		expected               []string
		unsupported            bool
	}{
		{"old Agent", "PersistentVolumeClaim", "team-a", http.StatusNotFound, []string{"probe"}, true},
		{"generic HEAD response", "PersistentVolumeClaim", "team-a", http.StatusOK, []string{"probe"}, true},
		{"HEAD not modified", "PersistentVolumeClaim", "team-a", http.StatusNotModified, []string{"probe"}, true},
		{"redirected HEAD", "PersistentVolumeClaim", "team-a", http.StatusFound, []string{"probe"}, true},
		{"supported PVC", "PersistentVolumeClaim", "team-a", http.StatusNoContent, []string{"probe", "graph"}, false},
		{"supported PV", "PersistentVolume", "", http.StatusNoContent, []string{"probe", "graph"}, false},
		{"supported class", "StorageClass", "", http.StatusNoContent, []string{"probe", "graph"}, false},
		{"existing Pod", "Pod", "team-a", 0, []string{"graph"}, false},
	} {
		t.Run(tc.label, func(t *testing.T) {
			var mu sync.Mutex
			var calls []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				defer mu.Unlock()
				switch {
				case r.Method == http.MethodHead && r.URL.RequestURI() == storageGraphCapabilityPath:
					calls = append(calls, "probe")
					if tc.probeStatus == http.StatusFound {
						w.Header().Set("Location", "/not-the-probe")
					}
					w.WriteHeader(tc.probeStatus)
				case r.Method == http.MethodGet && r.URL.Path == "/api/v1/platform/resources/graph":
					calls = append(calls, "graph")
					query := r.URL.Query()
					if query.Get("namespace") != tc.namespace || query.Get("kind") != tc.kind || query.Get("name") != "sample" {
						t.Errorf("incorrect graph identity: %v", query)
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"rootId": "root"}})
				default:
					t.Errorf("unexpected path %s", r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			client := &Client{baseURL: server.URL, httpClient: server.Client()}
			graph, err := client.GetResourceGraph(context.Background(), tc.namespace, tc.kind, "sample")
			if tc.unsupported {
				if !errors.Is(err, apperrors.ErrUnsupportedOperation) {
					t.Fatalf("old Agent returned %v", err)
				}
			} else if err != nil || graph.RootID != "root" {
				t.Fatalf("graph unavailable: %+v %v", graph, err)
			}
			mu.Lock()
			defer mu.Unlock()
			if !reflect.DeepEqual(calls, tc.expected) {
				t.Fatalf("calls = %v, want %v", calls, tc.expected)
			}
		})
	}
}
