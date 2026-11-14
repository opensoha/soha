package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	cr "github.com/opensoha/soha/contracts/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func customRootObservation(t *testing.T, object *unstructured.Unstructured, definition cr.ResourceRef) ResourceGraphMetadataEnrichment {
	t.Helper()
	content, err := object.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	return ResourceGraphMetadataEnrichment{Content: content, Definition: definition}
}

func TestBuildCustomResourceGraphDefinitionRoot(t *testing.T) {
	_, fixture := graphEnrichmentFixture()
	definition := fixture.Definition
	graph, err := BuildCustomResourceGraph(context.Background(), definition, definition, nil)
	if err != nil || graph.RootID != graphNodeID(definition) || len(graph.Nodes) != 1 || len(graph.Edges) != 0 || graph.Nodes[0].Resource != definition {
		t.Fatalf("empty definition graph: %+v, %v", graph, err)
	}
	instance := customRootObservation(t, fixture.Object, definition)
	other := graphTestObject("other.example/v1", "Widget", "owner", "unrelated-uid")
	otherDefinition := definition
	otherDefinition.Name, otherDefinition.UID = "widgets.other.example", "unrelated-crd-uid"
	graph, err = BuildCustomResourceGraph(context.Background(), definition, definition, []ResourceGraphMetadataEnrichment{instance, customRootObservation(t, other, otherDefinition), instance})
	if err != nil || graph.RootID != graphNodeID(definition) || len(graph.Nodes) != 2 || len(graph.Edges) != 1 || !graphTestEdge(graph, "defines", definition.Name, fixture.Object.GetName()) {
		t.Fatalf("definition graph included a non-instance, duplicate or omitted the exact instance: %+v, %v", graph, err)
	}
}

func TestBuildCustomResourceGraphOwnerReferences(t *testing.T) {
	_, fixture := graphEnrichmentFixture()
	definition := fixture.Definition
	root := fixture.Object
	root.SetName("root")
	root.SetUID("root-uid")
	root.Object["spec"] = map[string]interface{}{"secretRef": "MUST-NOT-LEAK"}
	root.SetLabels(map[string]string{"targetRef": "MUST-NOT-LEAK"})
	parent := graphTestObject("parents.example/v1", "Parent", "parent", "parent-uid")
	parent.SetNamespace("")
	parentDefinition := definition
	parentDefinition.Name, parentDefinition.UID = "parents.parents.example", "parent-definition-uid"
	graphTestOwner(root, parent.GetAPIVersion(), parent.GetKind(), parent.GetName(), string(parent.GetUID()), true)
	child := graphTestObject("v1", "Pod", "child", "child-uid")
	graphTestOwner(child, root.GetAPIVersion(), root.GetKind(), root.GetName(), string(root.GetUID()), false)
	stale := graphTestObject("v1", "Pod", "stale-child", "stale-child-uid")
	graphTestOwner(stale, root.GetAPIVersion(), root.GetKind(), root.GetName(), "replaced-root-uid", false)
	otherNamespace := graphTestObject("v1", "Pod", "other-namespace-child", "other-namespace-child-uid")
	otherNamespace.SetNamespace("other")
	graphTestOwner(otherNamespace, root.GetAPIVersion(), root.GetKind(), root.GetName(), string(root.GetUID()), false)
	rootRef := enrichmentRef(definition.ClusterID, root)
	rootRef.ResourceVersion = "9"
	rootInput := customRootObservation(t, root, definition)
	rootInput.TargetID = graphNodeID(rootRef)
	graph, err := BuildCustomResourceGraph(context.Background(), rootRef, definition, []ResourceGraphMetadataEnrichment{
		rootInput, customRootObservation(t, parent, parentDefinition), customRootObservation(t, child, cr.ResourceRef{}), customRootObservation(t, stale, cr.ResourceRef{}), customRootObservation(t, otherNamespace, cr.ResourceRef{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if graph.RootID != graphNodeID(rootRef) || len(graph.Nodes) != 5 || len(graph.Edges) != 4 || !graphTestEdge(graph, "defines", definition.Name, "root") || !graphTestEdge(graph, "defines", parentDefinition.Name, "parent") || !graphTestEdge(graph, "controls", "parent", "root") || !graphTestEdge(graph, "owns", "root", "child") {
		t.Fatalf("unexpected custom graph: %+v", graph)
	}
	for _, node := range graph.Nodes {
		if node.ID == graph.RootID && node.Resource != rootRef {
			t.Fatalf("root identity changed: %+v", node)
		}
		if node.Resource.Name == "parent" && (node.Status == "unresolved" || node.Resource.Namespace != "" || node.Resource.ScopeMode != cr.ResourceScopeModeCluster) {
			t.Fatalf("cluster-scoped owner failed exact resolution: %+v", node)
		}
	}
	content, err := json.Marshal(graph)
	if err != nil || strings.Contains(string(content), "MUST-NOT-LEAK") {
		t.Fatalf("spec or labels reached graph output: %s, %v", content, err)
	}
	parent.SetUID("new-parent-uid")
	graph, err = BuildCustomResourceGraph(context.Background(), rootRef, definition, []ResourceGraphMetadataEnrichment{rootInput, customRootObservation(t, parent, parentDefinition)})
	if err != nil || len(graph.Nodes) != 3 || !graphTestEdge(graph, "controls", "parent", "root") {
		t.Fatalf("stale owner declaration missing: %+v, %v", graph, err)
	}
	for _, node := range graph.Nodes {
		if node.Resource.Name == "parent" && (node.Status != "unresolved" || node.Resource.UID != "parent-uid") {
			t.Fatalf("replacement owner incorrectly resolved: %+v", node)
		}
	}
}

func TestBuildCustomResourceGraphExactIdentity(t *testing.T) {
	_, fixture := graphEnrichmentFixture()
	for _, test := range []struct {
		name string
		edit func(*cr.ResourceRef, *cr.ResourceRef, *[]ResourceGraphMetadataEnrichment)
		want error
	}{
		{"missing root UID", func(root, _ *cr.ResourceRef, _ *[]ResourceGraphMetadataEnrichment) { root.UID = "" }, ErrInvalidResourceGraphRoot},
		{"wrong root UID", func(root, _ *cr.ResourceRef, _ *[]ResourceGraphMetadataEnrichment) { root.UID = "stale" }, ErrInvalidResourceGraphRoot},
		{"wrong root GVK", func(root, _ *cr.ResourceRef, _ *[]ResourceGraphMetadataEnrichment) {
			root.APIVersion = "custom.example/v2"
		}, ErrInvalidResourceGraphRoot},
		{"wrong root scope", func(root, _ *cr.ResourceRef, _ *[]ResourceGraphMetadataEnrichment) {
			root.ScopeMode = cr.ResourceScopeModeCluster
		}, ErrInvalidResourceGraphRoot},
		{"wrong root namespace", func(root, _ *cr.ResourceRef, _ *[]ResourceGraphMetadataEnrichment) { root.Namespace = "other" }, ErrInvalidResourceGraphRoot},
		{"missing root metadata", func(_, _ *cr.ResourceRef, inputs *[]ResourceGraphMetadataEnrichment) { *inputs = nil }, ErrInvalidResourceGraphRoot},
		{"wrong definition cluster", func(_, definition *cr.ResourceRef, _ *[]ResourceGraphMetadataEnrichment) {
			definition.ClusterID = "other"
		}, ErrInvalidResourceGraphRoot},
		{"wrong definition root UID", func(root, definition *cr.ResourceRef, _ *[]ResourceGraphMetadataEnrichment) {
			*root = *definition
			root.UID = "stale"
		}, ErrInvalidResourceGraphRoot},
		{"wrong observation ID", func(_, _ *cr.ResourceRef, inputs *[]ResourceGraphMetadataEnrichment) {
			(*inputs)[0].TargetID = "unrelated"
		}, ErrInvalidResourceGraphEnrichment},
		{"conflicting definition UID", func(_, _ *cr.ResourceRef, inputs *[]ResourceGraphMetadataEnrichment) {
			(*inputs)[0].Definition.UID = "stale"
		}, ErrInvalidResourceGraphEnrichment},
		{"lookalike definition group", func(_, _ *cr.ResourceRef, inputs *[]ResourceGraphMetadataEnrichment) {
			(*inputs)[0].Definition.Name = "widgets.fake.custom.example"
		}, ErrInvalidResourceGraphEnrichment},
		{"missing observation UID", func(_, _ *cr.ResourceRef, inputs *[]ResourceGraphMetadataEnrichment) {
			(*inputs)[0].Content = []byte(`{"apiVersion":"custom.example/v1","kind":"Widget","metadata":{"name":"owner","namespace":"ns"}}`)
		}, ErrInvalidResourceGraphEnrichment},
		{"invalid metadata", func(_, _ *cr.ResourceRef, inputs *[]ResourceGraphMetadataEnrichment) {
			(*inputs)[0].Content = []byte("{")
		}, ErrInvalidResourceGraphEnrichment},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition := fixture.Definition
			root := enrichmentRef(definition.ClusterID, fixture.Object)
			inputs := []ResourceGraphMetadataEnrichment{customRootObservation(t, fixture.Object, definition)}
			test.edit(&root, &definition, &inputs)
			_, err := BuildCustomResourceGraph(context.Background(), root, definition, inputs)
			if !errors.Is(err, test.want) {
				t.Fatalf("expected %v, got %v", test.want, err)
			}
		})
	}
}

func TestBuildCustomResourceGraphBudgets(t *testing.T) {
	_, fixture := graphEnrichmentFixture()
	definition := fixture.Definition
	root := enrichmentRef(definition.ClusterID, fixture.Object)
	for _, inputs := range [][]ResourceGraphMetadataEnrichment{
		make([]ResourceGraphMetadataEnrichment, ResourceGraphMetadataEnrichmentLimit+1),
		{{Content: make([]byte, ResourceGraphMetadataJSONLimit+1)}},
	} {
		_, err := BuildCustomResourceGraph(context.Background(), root, definition, inputs)
		if !errors.Is(err, ErrResourceGraphBudget) {
			t.Fatalf("expected budget error, got %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := BuildCustomResourceGraph(ctx, definition, definition, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
	owners := make([]metav1.OwnerReference, ResourceGraphNodeLimit)
	for i := range owners {
		owners[i] = metav1.OwnerReference{APIVersion: "custom.example/v1", Kind: "Parent", Name: fmt.Sprintf("parent-%03d", i), UID: "unobserved"}
	}
	fixture.Object.SetOwnerReferences(owners)
	graph, err := BuildCustomResourceGraph(context.Background(), root, definition, []ResourceGraphMetadataEnrichment{customRootObservation(t, fixture.Object, definition)})
	if err != nil || len(graph.Nodes) != ResourceGraphNodeLimit || len(graph.Edges) >= ResourceGraphEdgeLimit || !strings.Contains(strings.Join(graph.Warnings, " "), "truncated") {
		t.Fatalf("budget was not enforced: nodes=%d edges=%d warnings=%v err=%v", len(graph.Nodes), len(graph.Edges), graph.Warnings, err)
	}
}

func TestBuildCustomResourceGraphDepth(t *testing.T) {
	_, fixture := graphEnrichmentFixture()
	definition := fixture.Definition
	root := enrichmentRef(definition.ClusterID, fixture.Object)
	inputs := []ResourceGraphMetadataEnrichment{customRootObservation(t, fixture.Object, definition)}
	owner := fixture.Object
	for i := 1; i <= ResourceGraphDepth+1; i++ {
		child := graphTestObject("v1", "Pod", fmt.Sprintf("child-%d", i), fmt.Sprintf("child-uid-%d", i))
		graphTestOwner(child, owner.GetAPIVersion(), owner.GetKind(), owner.GetName(), string(owner.GetUID()), true)
		inputs = append(inputs, customRootObservation(t, child, cr.ResourceRef{}))
		owner = child
	}
	graph, err := BuildCustomResourceGraph(context.Background(), root, definition, inputs)
	if err != nil || len(graph.Nodes) != ResourceGraphDepth+2 || len(graph.Edges) != ResourceGraphDepth+1 || !strings.Contains(strings.Join(graph.Warnings, " "), "depth") {
		t.Fatalf("depth was not enforced: %+v, %v", graph, err)
	}
}
