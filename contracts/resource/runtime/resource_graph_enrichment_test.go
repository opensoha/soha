package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	cr "github.com/opensoha/soha/contracts/resource"
	"reflect"
	"strings"
	"testing"
)

func graphEnrichmentFixture() (cr.ResourceGraph, ResourceGraphEnrichment) {
	object := graphTestObject("custom.example/v1", "Widget", "owner", "owner-uid")
	ref := enrichmentRef("cluster-a", object)
	graph := cr.ResourceGraph{ClusterID: "cluster-a", Namespace: "ns", RootID: "opaque-pod", Nodes: []cr.ResourceGraphNode{{ID: "opaque-pod", Resource: cr.ResourceRef{ClusterID: "cluster-a", APIVersion: "v1", Kind: "Pod", Namespace: "ns", Name: "pod", UID: "pod-uid", ScopeMode: cr.ResourceScopeModeNamespace}, Status: "Running"}, {ID: "opaque-owner", Resource: ref, Status: "unresolved"}}, Edges: []cr.ResourceGraphEdge{{ID: "opaque-edge", SourceID: "opaque-owner", TargetID: "opaque-pod", Relation: "controls"}}, Warnings: []string{"existing warning"}, Evidence: []cr.ResourceEvidence{{ID: "evidence", Summary: "preserve evidence"}}}
	input := ResourceGraphEnrichment{TargetID: "opaque-owner", Object: object, Definition: cr.ResourceRef{ClusterID: "cluster-a", APIVersion: "apiextensions.k8s.io/v1", Kind: "CustomResourceDefinition", Name: "widgets.custom.example", UID: "crd-uid", ScopeMode: cr.ResourceScopeModeCluster}}
	return graph, input
}
func TestResourceGraphEnrichmentIdentityAndScope(t *testing.T) {
	for _, test := range []struct {
		name string
		edit func(*ResourceGraphEnrichment)
	}{
		{"wrong group", func(e *ResourceGraphEnrichment) { e.Object.SetAPIVersion("apps/v1") }},
		{"replacement", func(e *ResourceGraphEnrichment) { e.Object.SetUID("new-uid") }},
		{"wrong kind", func(e *ResourceGraphEnrichment) { e.Object.SetKind("Deployment") }},
		{"wrong name", func(e *ResourceGraphEnrichment) { e.Object.SetName("other") }},
		{"cross namespace", func(e *ResourceGraphEnrichment) { e.Object.SetNamespace("other") }},
		{"missing target", func(e *ResourceGraphEnrichment) { e.TargetID = "missing" }},
		{"unobserved definition", func(e *ResourceGraphEnrichment) { e.Definition.UID = "" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			graph, input := graphEnrichmentFixture()
			test.edit(&input)
			_, err := EnrichResourceGraph(context.Background(), graph, []ResourceGraphEnrichment{input})
			if !errors.Is(err, ErrInvalidResourceGraphEnrichment) {
				t.Fatalf("identity accepted: %v", err)
			}
			if graph.Nodes[1].Status != "unresolved" {
				t.Fatal("input graph mutated")
			}
		})
	}
	graph, input := graphEnrichmentFixture()
	input.Object.SetNamespace("")
	result, err := EnrichResourceGraph(context.Background(), graph, []ResourceGraphEnrichment{input})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, node := range result.Nodes {
		if node.ID == "opaque-owner" {
			found = true
			if node.Status == "unresolved" || node.Resource.Namespace != "" || node.Resource.ScopeMode != cr.ResourceScopeModeCluster {
				t.Fatalf("scope not corrected: %+v", node)
			}
		}
	}
	if !found {
		t.Fatal("opaque target ID changed")
	}
	if !graphTestEdge(result, "defines", "widgets.custom.example", "owner") {
		t.Fatal("CRD defines direction incorrect")
	}
	if !reflect.DeepEqual(result.Evidence, graph.Evidence) || result.Warnings[0] != "existing warning" {
		t.Fatal("evidence/warnings changed")
	}
	for _, edge := range result.Edges {
		if edge.Relation == "controls" && edge.ID != "opaque-edge" {
			t.Fatal("existing edge ID rewritten")
		}
	}
}
func TestResourceGraphEnrichmentOwnerMetadataAndIdempotency(t *testing.T) {
	for _, controller := range []bool{false, true} {
		t.Run(fmt.Sprint(controller), func(t *testing.T) {
			graph, input := graphEnrichmentFixture()
			graphTestOwner(input.Object, "custom.example/v1", "Parent", "parent", "parent-uid", controller)
			input.Object.Object["spec"] = map[string]interface{}{"secret": "MUST-NOT-LEAK"}
			input.Object.Object["data"] = map[string]interface{}{"token": "MUST-NOT-LEAK"}
			result, err := EnrichResourceGraph(context.Background(), graph, []ResourceGraphEnrichment{input, input})
			if err != nil {
				t.Fatal(err)
			}
			relation := "owns"
			if controller {
				relation = "controls"
			}
			if !graphTestEdge(result, relation, "parent", "owner") {
				t.Fatal("owner/controller relation missing")
			}
			for _, node := range result.Nodes {
				if node.Resource.Name == "parent" && node.Status != "unresolved" {
					t.Fatal("unobserved parent confirmed")
				}
			}
			encoded, _ := json.Marshal(result)
			if strings.Contains(string(encoded), "MUST-NOT-LEAK") {
				t.Fatal("unknown payload exposed")
			}
			again, err := EnrichResourceGraph(context.Background(), result, []ResourceGraphEnrichment{input})
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(result, again) {
				t.Fatalf("repeated enrichment not stable\n%+v\n%+v", result, again)
			}
		})
	}
}
func TestResourceGraphEnrichmentCancellationAndBudget(t *testing.T) {
	graph, input := graphEnrichmentFixture()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := EnrichResourceGraph(ctx, graph, []ResourceGraphEnrichment{input})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
	for len(graph.Nodes) < ResourceGraphNodeLimit {
		index := len(graph.Nodes)
		graph.Nodes = append(graph.Nodes, cr.ResourceGraphNode{ID: fmt.Sprintf("extra-%d", index), Resource: cr.ResourceRef{ClusterID: graph.ClusterID, APIVersion: "v1", Kind: "Pod", Name: fmt.Sprint(index), Namespace: "ns", ScopeMode: cr.ResourceScopeModeNamespace}})
	}
	result, err := EnrichResourceGraph(context.Background(), graph, []ResourceGraphEnrichment{input})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Nodes) > 500 || len(result.Edges) > 1000 || !strings.Contains(strings.Join(result.Warnings, " "), "enrichment is partial") {
		t.Fatal("budget overflow silent")
	}
	graph.Nodes = append(graph.Nodes, graph.Nodes[0])
	_, err = EnrichResourceGraph(context.Background(), graph, nil)
	if !errors.Is(err, ErrResourceGraphBudget) {
		t.Fatalf("invalid initial budget accepted: %v", err)
	}
}
func TestResourceGraphMetadataJSONBoundaries(t *testing.T) {
	graph, input := graphEnrichmentFixture()
	content := []byte(`{"apiVersion":"custom.example/v1","kind":"Widget","metadata":{"name":"owner","namespace":"ns","uid":"owner-uid","annotations":{"secret":"MUST-NOT-LEAK"}},"spec":{"credential":"MUST-NOT-LEAK"},"data":{"password":"MUST-NOT-LEAK"}}`)
	metadata := ResourceGraphMetadataEnrichment{TargetID: input.TargetID, Content: content, Definition: input.Definition}
	result, err := EnrichResourceGraphFromMetadataJSON(context.Background(), graph, []ResourceGraphMetadataEnrichment{metadata})
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(result)
	if strings.Contains(string(encoded), "MUST-NOT-LEAK") {
		t.Fatal("JSON payload exposed")
	}
	metadata.Content = make([]byte, ResourceGraphMetadataJSONLimit+1)
	_, err = EnrichResourceGraphFromMetadataJSON(context.Background(), graph, []ResourceGraphMetadataEnrichment{metadata})
	if !errors.Is(err, ErrResourceGraphBudget) {
		t.Fatal("JSON size unbounded")
	}
	_, err = EnrichResourceGraphFromMetadataJSON(context.Background(), graph, make([]ResourceGraphMetadataEnrichment, 17))
	if !errors.Is(err, ErrResourceGraphBudget) {
		t.Fatal("JSON item count unbounded")
	}
	metadata.Content = []byte(`{"metadata":`)
	_, err = EnrichResourceGraphFromMetadataJSON(context.Background(), graph, []ResourceGraphMetadataEnrichment{metadata})
	if !errors.Is(err, ErrInvalidResourceGraphEnrichment) {
		t.Fatal("malformed JSON accepted")
	}
}

func TestResourceGraphEnrichmentDepthAndStableEdgeIDs(t *testing.T) {
	graph, input := graphEnrichmentFixture()
	middle := graph.Nodes[1]
	middle.ID = "middle"
	middle.Resource.Name = "middle"
	middle.Resource.UID = "middle-uid"
	middle.Status = ""
	far := middle
	far.ID = "far"
	far.Resource.Name = "far"
	far.Resource.UID = "far-uid"
	graph.Nodes = append(graph.Nodes, middle, far)
	graph.Edges = []cr.ResourceGraphEdge{{ID: "edge-one", SourceID: "middle", TargetID: "opaque-pod", Relation: "controls"}, {ID: "edge-two", SourceID: "far", TargetID: "middle", Relation: "controls"}, {ID: "edge-three", SourceID: "opaque-owner", TargetID: "far", Relation: "controls"}}
	graphTestOwner(input.Object, "custom.example/v1", "Parent", "beyond-depth", "parent-uid", true)
	result, err := EnrichResourceGraph(context.Background(), graph, []ResourceGraphEnrichment{input})
	if err != nil {
		t.Fatal(err)
	}
	if graphTestHas(result, "beyond-depth") || graphTestHas(result, "widgets.custom.example") {
		t.Fatal("enrichment crossed three-hop boundary")
	}
	if !strings.Contains(strings.Join(result.Warnings, " "), "enrichment is partial") {
		t.Fatal("depth omission silent")
	}
	for _, original := range graph.Edges {
		found := false
		for _, edge := range result.Edges {
			if edge == original {
				found = true
			}
		}
		if !found {
			t.Fatalf("existing opaque edge modified: %+v", original)
		}
	}
}
