package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
	"sort"
	"strings"

	cr "github.com/opensoha/soha-contracts/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

// ResourceGraphEnrichment contains an authorized observation, not a discovery
// request. Callers must establish the CRD's group/kind/version from its spec.
// Only the instance's GVK and metadata are read; unknown payload is ignored.
type ResourceGraphEnrichment struct {
	TargetID   string
	Object     *unstructured.Unstructured
	Definition cr.ResourceRef
}

var ErrInvalidResourceGraphEnrichment = errors.New("invalid resource graph enrichment")

func invalidGraphEnrichment(reason string) error {
	return fmt.Errorf("%w: %s", ErrInvalidResourceGraphEnrichment, reason)
}

// EnrichResourceGraph resolves authorized custom owner observations and adds
// metadata owner relationships and CRD defines edges. Existing IDs, edges,
// evidence and statuses are retained; no discovery, payload parsing or I/O occurs.
func EnrichResourceGraph(ctx context.Context, graph cr.ResourceGraph, enrichments []ResourceGraphEnrichment) (cr.ResourceGraph, error) {
	ctx, cancel := context.WithTimeout(ctx, ResourceGraphTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return cr.ResourceGraph{}, err
	}
	if len(graph.Nodes) > ResourceGraphNodeLimit || len(graph.Edges) > ResourceGraphEdgeLimit || len(enrichments) > ResourceGraphWorkingNodeLimit {
		return cr.ResourceGraph{}, ErrResourceGraphBudget
	}
	result := cloneEnrichmentGraph(graph)
	nodes, err := enrichmentNodes(result)
	if err != nil {
		return cr.ResourceGraph{}, err
	}
	inputs, err := validateEnrichments(ctx, result.ClusterID, nodes, enrichments)
	if err != nil {
		return cr.ResourceGraph{}, err
	}
	for _, input := range inputs {
		node := nodes[input.TargetID]
		node.Resource = enrichmentRef(result.ClusterID, input.Object)
		if node.Status == "unresolved" {
			node.Status = ""
		}
		nodes[input.TargetID] = node
	}
	builder := enrichmentBuilder(ctx, result.ClusterID, nodes, inputs)
	for _, input := range inputs {
		if err := ctx.Err(); err != nil {
			return cr.ResourceGraph{}, err
		}
		builder.ownerRelations(input.Object, input.TargetID)
		definitionID := builder.node(input.Definition, "")
		builder.edge(definitionID, input.TargetID, "defines")
	}
	if builder.err != nil {
		return cr.ResourceGraph{}, builder.err
	}
	return mergeEnrichment(ctx, result, nodes, builder)
}
func cloneEnrichmentGraph(graph cr.ResourceGraph) cr.ResourceGraph {
	graph.Nodes = append([]cr.ResourceGraphNode{}, graph.Nodes...)
	graph.Edges = append([]cr.ResourceGraphEdge{}, graph.Edges...)
	graph.Evidence = append([]cr.ResourceEvidence{}, graph.Evidence...)
	graph.Warnings = append([]string{}, graph.Warnings...)
	return graph
}
func enrichmentNodes(graph cr.ResourceGraph) (map[string]cr.ResourceGraphNode, error) {
	nodes := map[string]cr.ResourceGraphNode{}
	for _, node := range graph.Nodes {
		if node.ID == "" || node.Resource.ClusterID != graph.ClusterID {
			return nil, invalidGraphEnrichment("node identity or cluster mismatch")
		}
		if _, exists := nodes[node.ID]; exists {
			return nil, invalidGraphEnrichment("duplicate node ID")
		}
		nodes[node.ID] = node
	}
	if graph.ClusterID == "" || graph.RootID == "" {
		return nil, invalidGraphEnrichment("graph requires cluster and root")
	}
	if _, ok := nodes[graph.RootID]; !ok {
		return nil, invalidGraphEnrichment("root not present")
	}
	return nodes, nil
}
func validateEnrichments(ctx context.Context, cluster string, nodes map[string]cr.ResourceGraphNode, inputs []ResourceGraphEnrichment) ([]ResourceGraphEnrichment, error) {
	byID := map[string]ResourceGraphEnrichment{}
	for _, input := range inputs {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		node, exists := nodes[input.TargetID]
		if !exists {
			return nil, invalidGraphEnrichment("target not present")
		}
		if err := validateEnrichment(cluster, node, input); err != nil {
			return nil, err
		}
		if previous, exists := byID[input.TargetID]; exists {
			if !sameEnrichment(previous, input) {
				return nil, invalidGraphEnrichment("conflicting duplicate observation")
			}
			continue
		}
		byID[input.TargetID] = input
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]ResourceGraphEnrichment, 0, len(ids))
	for _, id := range ids {
		result = append(result, byID[id])
	}
	return result, nil
}
func validateEnrichment(cluster string, node cr.ResourceGraphNode, input ResourceGraphEnrichment) error {
	o := input.Object
	if o == nil {
		return invalidGraphEnrichment("object required")
	}
	if graphBuiltinSpec(o) {
		return invalidGraphEnrichment("built-in GVK is not a custom instance")
	}
	expected := node.Resource
	if expected.APIVersion != o.GetAPIVersion() || expected.Kind != o.GetKind() || expected.Name != o.GetName() {
		return invalidGraphEnrichment("GVK or name mismatch")
	}
	if expected.UID == "" || string(o.GetUID()) != expected.UID {
		return invalidGraphEnrichment("UID mismatch")
	}
	if o.GetNamespace() != expected.Namespace && o.GetNamespace() != "" {
		return invalidGraphEnrichment("namespace mismatch")
	}
	if node.Status != "unresolved" && o.GetNamespace() != expected.Namespace {
		return invalidGraphEnrichment("resolved scope changed")
	}
	return validateEnrichmentDefinition(cluster, o.GetAPIVersion(), input.Definition)
}
func validateEnrichmentDefinition(cluster, api string, definition cr.ResourceRef) error {
	group, _, ok := strings.Cut(api, "/")
	if !ok || group == "" {
		return invalidGraphEnrichment("custom API group required")
	}
	if definition.ClusterID != cluster || definition.APIVersion != "apiextensions.k8s.io/v1" || definition.Kind != "CustomResourceDefinition" {
		return invalidGraphEnrichment("definition identity mismatch")
	}
	if definition.Namespace != "" || definition.ScopeMode != cr.ResourceScopeModeCluster || definition.UID == "" {
		return invalidGraphEnrichment("definition requires observed cluster identity")
	}
	suffix := "." + group
	if !strings.HasSuffix(definition.Name, suffix) || len(definition.Name) <= len(suffix) {
		return invalidGraphEnrichment("definition group mismatch")
	}
	return nil
}
func sameEnrichment(a, b ResourceGraphEnrichment) bool {
	return a.Definition == b.Definition && a.Object.GetNamespace() == b.Object.GetNamespace() && reflect.DeepEqual(a.Object.GetOwnerReferences(), b.Object.GetOwnerReferences())
}
func enrichmentRef(cluster string, o *unstructured.Unstructured) cr.ResourceRef {
	scope := cr.ResourceScopeModeNamespace
	if o.GetNamespace() == "" {
		scope = cr.ResourceScopeModeCluster
	}
	return cr.ResourceRef{ClusterID: cluster, APIVersion: o.GetAPIVersion(), Kind: o.GetKind(), Name: o.GetName(), Namespace: o.GetNamespace(), ScopeMode: scope, UID: string(o.GetUID()), ResourceVersion: o.GetResourceVersion()}
}
func enrichmentBuilder(ctx context.Context, cluster string, nodes map[string]cr.ResourceGraphNode, inputs []ResourceGraphEnrichment) *graphRules {
	objects := map[string]*unstructured.Unstructured{}
	for _, node := range nodes {
		ref := node.Resource
		if node.Status == "unresolved" || ref.UID == "" {
			continue
		}
		o := &unstructured.Unstructured{}
		o.SetAPIVersion(ref.APIVersion)
		o.SetKind(ref.Kind)
		o.SetName(ref.Name)
		o.SetNamespace(ref.Namespace)
		o.SetUID(types.UID(ref.UID))
		objects[graphIdentity(ref.APIVersion, ref.Kind, ref.Namespace, ref.Name)] = o
	}
	for _, input := range inputs {
		o := input.Object
		objects[graphIdentity(o.GetAPIVersion(), o.GetKind(), o.GetNamespace(), o.GetName())] = o
	}
	return &graphRules{ctx: ctx, cluster: cluster, objects: objects, nodes: map[string]cr.ResourceGraphNode{}, edges: map[string]cr.ResourceGraphEdge{}}
}

func mergeEnrichment(ctx context.Context, result cr.ResourceGraph, nodes map[string]cr.ResourceGraphNode, builder *graphRules) (cr.ResourceGraph, error) {
	aliases, candidates, err := enrichmentCandidates(nodes, builder.nodes)
	if err != nil {
		return cr.ResourceGraph{}, err
	}
	existingEdges := map[string]bool{}
	edges := append([]cr.ResourceGraphEdge{}, result.Edges...)
	for _, edge := range result.Edges {
		existingEdges[edge.Relation+":"+edge.SourceID+":"+edge.TargetID] = true
	}
	generated := make([]cr.ResourceGraphEdge, 0, len(builder.edges))
	for _, edge := range builder.edges {
		if alias, ok := aliases[edge.SourceID]; ok {
			edge.SourceID = alias
		}
		if alias, ok := aliases[edge.TargetID]; ok {
			edge.TargetID = alias
		}
		if edge.SourceID == edge.TargetID {
			continue
		}
		edge.ID = edge.Relation + ":" + edge.SourceID + ":" + edge.TargetID
		generated = append(generated, edge)
	}
	sort.Slice(generated, func(i, j int) bool { return generated[i].ID < generated[j].ID })
	edges = append(edges, generated...)
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	selected, err := graphTraverse(ctx, result.RootID, edges)
	if err != nil {
		return cr.ResourceGraph{}, err
	}
	if err := includeRootScaleDefinitions(ctx, result, nodes, generated, selected); err != nil {
		return cr.ResourceGraph{}, err
	}
	acceptedEdges := append([]cr.ResourceGraphEdge{}, result.Edges...)
	partial := builder.partial
	for _, edge := range generated {
		if err := ctx.Err(); err != nil {
			return cr.ResourceGraph{}, err
		}
		if existingEdges[edge.ID] {
			continue
		}
		if !selected.chosen[edge.ID] {
			partial = true
			continue
		}
		if !includeEnrichmentEdge(edge, nodes, candidates, &acceptedEdges) {
			partial = true
			continue
		}
		existingEdges[edge.Relation+":"+edge.SourceID+":"+edge.TargetID] = true
	}
	result.Nodes = result.Nodes[:0]
	for _, node := range nodes {
		result.Nodes = append(result.Nodes, node)
	}
	result.Edges = acceptedEdges
	sort.Slice(result.Nodes, func(i, j int) bool { return result.Nodes[i].ID < result.Nodes[j].ID })
	sort.Slice(result.Edges, func(i, j int) bool { return result.Edges[i].ID < result.Edges[j].ID })
	if partial || selected.depthTruncated || selected.limitTruncated {
		result.Warnings = appendUniqueWarning(result.Warnings, "resource graph enrichment is partial due to depth or node/edge budget")
	}
	return result, ctx.Err()
}

// A direct HPA scale target also exposes its observed definition, without
// reversing general traversal or following the definition's other instances.
func includeRootScaleDefinitions(ctx context.Context, graph cr.ResourceGraph, nodes map[string]cr.ResourceGraphNode, generated []cr.ResourceGraphEdge, selected *graphTraversal) error {
	root := nodes[graph.RootID].Resource
	if root.APIVersion != "autoscaling/v2" || root.Kind != "HorizontalPodAutoscaler" || root.Namespace == "" {
		return nil
	}
	if root.ScopeMode != "" && root.ScopeMode != cr.ResourceScopeModeNamespace {
		return nil
	}
	targets := map[string]bool{}
	for _, edge := range graph.Edges {
		if err := ctx.Err(); err != nil {
			return err
		}
		if edge.Relation == "scales" && edge.SourceID == graph.RootID && selected.chosen[edge.ID] && nodes[edge.TargetID].Resource.Namespace == root.Namespace {
			targets[edge.TargetID] = true
		}
	}
	for _, edge := range generated {
		if err := ctx.Err(); err != nil {
			return err
		}
		if edge.Relation == "defines" && targets[edge.TargetID] {
			selected.include(edge, graphTraversalState{id: edge.SourceID, depth: 2, direction: -1})
		}
	}
	return nil
}

func enrichmentCandidates(existing, generated map[string]cr.ResourceGraphNode) (map[string]string, map[string]cr.ResourceGraphNode, error) {
	aliases := map[string]string{}
	candidates := map[string]cr.ResourceGraphNode{}
	// Sorted existing IDs make alias resolution stable for opaque IDs.
	ids := make([]string, 0, len(existing))
	for id := range existing {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		key := graphNodeID(existing[id].Resource)
		if _, found := aliases[key]; !found {
			aliases[key] = id
		}
	}
	for id, node := range generated {
		if _, found := aliases[id]; found {
			continue
		}
		if current, collision := existing[id]; collision && graphNodeID(current.Resource) != graphNodeID(node.Resource) {
			return nil, nil, invalidGraphEnrichment("opaque node ID collision")
		}
		candidates[id] = node
	}
	return aliases, candidates, nil
}
func includeEnrichmentEdge(edge cr.ResourceGraphEdge, nodes, candidates map[string]cr.ResourceGraphNode, edges *[]cr.ResourceGraphEdge) bool {
	missing := []cr.ResourceGraphNode{}
	for _, id := range []string{edge.SourceID, edge.TargetID} {
		if _, exists := nodes[id]; exists {
			continue
		}
		node, ok := candidates[id]
		if !ok {
			return false
		}
		missing = append(missing, node)
	}
	if len(nodes)+len(missing) > ResourceGraphNodeLimit || len(*edges) >= ResourceGraphEdgeLimit {
		return false
	}
	for _, node := range missing {
		nodes[node.ID] = node
	}
	*edges = append(*edges, edge)
	return true
}
func appendUniqueWarning(warnings []string, message string) []string {
	for _, existing := range warnings {
		if existing == message {
			return warnings
		}
	}
	return append(warnings, message)
}

// ResourceGraphMetadataEnrichment keeps Kubernetes SDK types out of callers.
type ResourceGraphMetadataEnrichment struct {
	TargetID   string
	Content    []byte
	Definition cr.ResourceRef
}

const ResourceGraphMetadataEnrichmentLimit = 16
const ResourceGraphMetadataJSONLimit = 1 << 20

// EnrichResourceGraphFromMetadataJSON decodes only the relationship metadata
// fields. All other JSON, including spec/data/annotations, is ignored.
func EnrichResourceGraphFromMetadataJSON(ctx context.Context, graph cr.ResourceGraph, inputs []ResourceGraphMetadataEnrichment) (cr.ResourceGraph, error) {
	ctx, cancel := context.WithTimeout(ctx, ResourceGraphTimeout)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return cr.ResourceGraph{}, err
	}
	if len(inputs) > ResourceGraphMetadataEnrichmentLimit {
		return cr.ResourceGraph{}, ErrResourceGraphBudget
	}
	objects := make([]ResourceGraphEnrichment, 0, len(inputs))
	for _, input := range inputs {
		if err := ctx.Err(); err != nil {
			return cr.ResourceGraph{}, err
		}
		if len(input.Content) > ResourceGraphMetadataJSONLimit {
			return cr.ResourceGraph{}, ErrResourceGraphBudget
		}
		object, err := decodeEnrichmentMetadata(input.Content)
		if err != nil {
			return cr.ResourceGraph{}, err
		}
		objects = append(objects, ResourceGraphEnrichment{TargetID: input.TargetID, Object: object, Definition: input.Definition})
	}
	return EnrichResourceGraph(ctx, graph, objects)
}
func decodeEnrichmentMetadata(content []byte) (*unstructured.Unstructured, error) {
	var decoded struct {
		APIVersion string `json:"apiVersion"`
		Kind       string `json:"kind"`
		Metadata   struct {
			Name            string                  `json:"name"`
			Namespace       string                  `json:"namespace"`
			UID             types.UID               `json:"uid"`
			OwnerReferences []metav1.OwnerReference `json:"ownerReferences"`
		} `json:"metadata"`
	}
	if json.Unmarshal(content, &decoded) != nil {
		return nil, invalidGraphEnrichment("invalid metadata JSON")
	}
	object := &unstructured.Unstructured{}
	object.SetAPIVersion(decoded.APIVersion)
	object.SetKind(decoded.Kind)
	object.SetName(decoded.Metadata.Name)
	object.SetNamespace(decoded.Metadata.Namespace)
	object.SetUID(decoded.Metadata.UID)
	object.SetOwnerReferences(decoded.Metadata.OwnerReferences)
	return object, nil
}
