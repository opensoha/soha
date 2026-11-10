package workflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"

	domainworkflow "github.com/opensoha/soha/internal/domain/workflow"
)

func TestDAGDefinitionRestoresPersistedNodeConfiguration(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/owned-check" {
			t.Error("restored check contacted an unexpected target")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		calls.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	definition := dagWorkflowDefinition{SchemaVersion: 2, Mode: "release_dag", Nodes: []dagWorkflowNode{
		{ID: "review", Name: "Review", Type: "manual_approval"},
		{ID: "check", Name: "Check", Type: "check", TimeoutSeconds: 5,
			Config: map[string]any{"url": server.URL + "/owned-check", "expectedStatus": 200}},
	}, Edges: []dagWorkflowEdge{{ID: "review-check", Source: "review", Target: "check", Condition: "success"}}}
	// PostgreSQL stores typed runtime metadata as JSON and reopens generic maps.
	raw, err := json.Marshal(map[string]any{"mode": definition.Mode, "schemaVersion": definition.SchemaVersion,
		"nodes": definition.Nodes, "edges": definition.Edges})
	if err != nil {
		t.Fatal(err)
	}
	var metadata map[string]any
	if err := json.Unmarshal(raw, &metadata); err != nil {
		t.Fatal(err)
	}
	restored, ok := definitionFromRunMetadata(domainworkflow.Run{Metadata: metadata})
	if !ok || len(restored.Nodes) != 2 || len(restored.Edges) != 1 {
		t.Fatal("persisted workflow definition could not be restored")
	}
	node := restored.Nodes[1]
	if node.ID != "check" || node.Type != "check" || node.Config["url"] != server.URL+"/owned-check" ||
		restored.Edges[0] != definition.Edges[0] {
		t.Fatal("persisted workflow lost its node identity, configuration or dependency")
	}
	result := (checkDAGNodeHandler{client: server.Client()}).execute(context.Background(), dagNodeExecutionInput{node: node})
	if result.status != "completed" || calls.Load() != 1 {
		t.Fatal("restored workflow check did not produce its one real HTTP effect")
	}
}

func TestDAGMetadataRestorationPreservesCanonicalAndNestedFields(t *testing.T) {
	config := map[string]any{"CaseSensitive": "kept", "large": json.Number("9007199254740993")}
	metadata := map[string]any{"mode": "release_dag", "nodes": []map[string]any{
		{"ID": "legacy", "id": "canonical", "Type": "check", "Config": config, "TargetID": "owned-target", "": "ignored"},
	}, "edges": []map[string]any{}}
	before, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	definition, ok := definitionFromRunMetadata(domainworkflow.Run{Metadata: metadata})
	if !ok || len(definition.Nodes) != 1 || definition.Nodes[0].ID != "canonical" ||
		definition.Nodes[0].TargetID != "owned-target" || !reflect.DeepEqual(definition.Nodes[0].Config, config) {
		t.Fatal("restoration changed canonical identity, custom configuration or exact numeric values")
	}
	after, err := json.Marshal(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("restoration mutated the stored metadata or its public projection")
	}
}
