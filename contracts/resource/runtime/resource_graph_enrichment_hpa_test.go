package runtime

import (
	"context"
	"fmt"
	"strings"
	"testing"

	cr "github.com/opensoha/soha/contracts/resource"
)

func graphHPAScaleEnrichmentFixture() (cr.ResourceGraph, ResourceGraphEnrichment) {
	graph, input := graphEnrichmentFixture()
	graph.Nodes[0].Resource.APIVersion = "autoscaling/v2"
	graph.Nodes[0].Resource.Kind = "HorizontalPodAutoscaler"
	graph.Edges[0].SourceID, graph.Edges[0].TargetID = graph.RootID, input.TargetID
	graph.Edges[0].Relation = "scales"
	return graph, input
}

func TestResourceGraphEnrichmentHPADirectScaleDefinition(t *testing.T) {
	graph, input := graphHPAScaleEnrichmentFixture()
	result, err := EnrichResourceGraph(context.Background(), graph, []ResourceGraphEnrichment{input})
	if err != nil || len(result.Nodes) != 3 || len(result.Edges) != 2 || !graphTestEdge(result, "defines", "widgets.custom.example", "owner") {
		t.Fatalf("direct scale definition missing: graph=%+v err=%v", result, err)
	}
	if strings.Contains(strings.Join(result.Warnings, " "), "enrichment is partial") {
		t.Fatalf("complete definition reported partial: %+v", result.Warnings)
	}
	if result.Edges[0] != graph.Edges[0] && result.Edges[1] != graph.Edges[0] {
		t.Fatal("opaque scales edge changed")
	}
}

func TestResourceGraphEnrichmentHPADefinitionAdjacencyIsLimited(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*cr.ResourceGraph, *ResourceGraphEnrichment)
	}{
		{"other source kind", func(g *cr.ResourceGraph, _ *ResourceGraphEnrichment) {
			g.Nodes[0].Resource.APIVersion, g.Nodes[0].Resource.Kind = "v1", "Pod"
		}},
		{"custom HPA source", func(g *cr.ResourceGraph, _ *ResourceGraphEnrichment) {
			g.Nodes[0].Resource.APIVersion = "custom.example/v1"
		}},
		{"invalid root scope", func(g *cr.ResourceGraph, _ *ResourceGraphEnrichment) {
			g.Nodes[0].Resource.ScopeMode = cr.ResourceScopeModeCluster
		}},
		{"cross namespace target", func(g *cr.ResourceGraph, e *ResourceGraphEnrichment) {
			g.Nodes[1].Resource.Namespace = "other"
			e.Object.SetNamespace("other")
		}},
		{"indirect scale target", func(g *cr.ResourceGraph, _ *ResourceGraphEnrichment) {
			middle := g.Nodes[0]
			middle.ID, middle.Resource.APIVersion, middle.Resource.Kind, middle.Resource.Name = "middle", "v1", "Pod", "middle"
			g.Nodes = append(g.Nodes, middle)
			g.Edges[0].SourceID = "middle"
			g.Edges = append(g.Edges, cr.ResourceGraphEdge{ID: "root-middle", SourceID: g.RootID, TargetID: "middle", Relation: "uses-configmap"})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			graph, input := graphHPAScaleEnrichmentFixture()
			tc.edit(&graph, &input)
			result, err := EnrichResourceGraph(context.Background(), graph, []ResourceGraphEnrichment{input})
			if err != nil {
				t.Fatal(err)
			}
			if graphTestHas(result, "widgets.custom.example") || graphTestEdge(result, "defines", "widgets.custom.example", "owner") {
				t.Fatalf("definition adjacency expanded outside direct namespaced HPA target: %+v", result)
			}
		})
	}
}

func TestResourceGraphEnrichmentHPADefinitionDoesNotFanOut(t *testing.T) {
	graph, input := graphHPAScaleEnrichmentFixture()
	other := graphTestObject("custom.example/v1", "Widget", "other-instance", "other-uid")
	graph.Nodes = append(graph.Nodes, cr.ResourceGraphNode{ID: "unrelated-instance", Resource: enrichmentRef(graph.ClusterID, other), Status: "unresolved"})
	unrelated := ResourceGraphEnrichment{TargetID: "unrelated-instance", Object: other, Definition: input.Definition}
	result, err := EnrichResourceGraph(context.Background(), graph, []ResourceGraphEnrichment{input, unrelated})
	if err != nil {
		t.Fatal(err)
	}
	if !graphTestEdge(result, "defines", "widgets.custom.example", "owner") {
		t.Fatal("root's direct scale definition missing")
	}
	if graphTestEdge(result, "defines", "widgets.custom.example", "other-instance") {
		t.Fatalf("definition expanded to an unrelated observed instance: %+v", result)
	}
}

func TestResourceGraphEnrichmentHPADefinitionRespectsOutputBudgets(t *testing.T) {
	for _, budget := range []string{"nodes", "edges"} {
		t.Run(budget, func(t *testing.T) {
			graph, input := graphHPAScaleEnrichmentFixture()
			if budget == "nodes" {
				for len(graph.Nodes) < ResourceGraphNodeLimit {
					id := fmt.Sprintf("unrelated-%03d", len(graph.Nodes))
					graph.Nodes = append(graph.Nodes, cr.ResourceGraphNode{ID: id, Resource: cr.ResourceRef{ClusterID: graph.ClusterID, APIVersion: "v1", Kind: "Pod", Name: id, Namespace: "ns", ScopeMode: cr.ResourceScopeModeNamespace}})
				}
			} else {
				for len(graph.Edges) < ResourceGraphEdgeLimit {
					edge := graph.Edges[0]
					edge.ID = fmt.Sprintf("opaque-scale-%04d", len(graph.Edges))
					graph.Edges = append(graph.Edges, edge)
				}
			}
			result, err := EnrichResourceGraph(context.Background(), graph, []ResourceGraphEnrichment{input})
			if err != nil || len(result.Nodes) > ResourceGraphNodeLimit || len(result.Edges) > ResourceGraphEdgeLimit {
				t.Fatalf("budget overflow: nodes=%d edges=%d err=%v", len(result.Nodes), len(result.Edges), err)
			}
			if graphTestHas(result, "widgets.custom.example") || !strings.Contains(strings.Join(result.Warnings, " "), "enrichment is partial") {
				t.Fatalf("definition bypassed exhausted %s budget: %+v", budget, result)
			}
		})
	}
}
