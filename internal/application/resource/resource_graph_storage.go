package resource

import (
	"context"
	"fmt"
	"slices"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const graphStorageAccessWarning = "Some related resources were hidden by access policy."

func graphStorageKind(kind string) bool {
	return resourceGroupForKind(kind) == "storage"
}

func graphNamedReferenceScopeAllowed(namespace string, decision domainaccess.Decision) bool {
	if namespace == "" {
		return clusterWideResourceScope(decision)
	}
	if !decision.Allowed {
		return false
	}
	scope := decision.ResourceScope
	if scope == nil {
		return true
	}
	if slices.Contains(scope.ExcludedNamespaces, namespace) {
		return false
	}
	exactNamespace := slices.Contains(scope.Namespaces, namespace)
	// Access projects a named, authorized namespace after it checks labels.
	// A label selector without that projection cannot be enforced by the graph.
	if scope.LabelSelector != "" && (!exactNamespace || !slices.Contains(scope.NamespaceSelectors, scope.LabelSelector)) {
		return false
	}
	if len(scope.NamespaceSelectors) != 0 || len(scope.ExcludedNamespaceSelectors) != 0 {
		return exactNamespace
	}
	return exactNamespace || len(scope.Namespaces) == 0
}

func graphHasStorageRoot(graph domainresource.ResourceGraph) bool {
	node, ok := resourceGraphRootNode(graph)
	return ok && graphStorageKind(node.Resource.Kind) && graphBuiltinResource(node.Resource)
}

func (g *GenericResources) storageGraphReferenceFilter(ctx context.Context, principal domainidentity.Principal, clusterID string) func(domainresource.ResourceRef) bool {
	decisions := map[[3]string]bool{}
	return func(ref domainresource.ResourceRef) bool {
		if !graphBuiltinResource(ref) || (ref.Kind != "Pod" && !graphStorageKind(ref.Kind)) {
			return false
		}
		key := [3]string{ref.Kind, ref.Namespace, ref.Name}
		if allowed, ok := decisions[key]; ok {
			return allowed
		}
		_, err := g.authorizeGraphView(ctx, principal, clusterID, ref, true)
		decisions[key] = err == nil
		return decisions[key]
	}
}

func resourceGraphRootNode(graph domainresource.ResourceGraph) (domainresource.ResourceGraphNode, bool) {
	for _, node := range graph.Nodes {
		if graph.RootID != "" && node.ID == graph.RootID {
			return node, true
		}
	}
	return domainresource.ResourceGraphNode{}, false
}

func (g *GenericResources) finishStorageResourceGraph(ctx context.Context, principal domainidentity.Principal, connection domaincluster.Connection, root domainresource.ResourceRef, graph domainresource.ResourceGraph) (domainresource.ResourceGraph, error) {
	node, ok := resourceGraphRootNode(graph)
	ref := node.Resource
	if !ok || node.Status == "unresolved" || !graphBuiltinResource(ref) || ref.Kind != root.Kind || ref.Name != root.Name || ref.Namespace != root.Namespace {
		return domainresource.ResourceGraph{}, fmt.Errorf("%w: storage graph root observation does not match the request", apperrors.ErrClusterUnready)
	}
	graph = g.filterAuthorizedGraph(ctx, principal, root.ClusterID, graph, nil)
	if err := ctx.Err(); err != nil {
		return domainresource.ResourceGraph{}, err
	}
	if _, ok := resourceGraphRootNode(graph); !ok {
		return domainresource.ResourceGraph{}, apperrors.ErrAccessDenied
	}
	graph = retainRootConnectedGraph(graph)
	if !slices.Contains(graph.Warnings, graphStorageAccessWarning) {
		if root.Kind == "StorageClass" && !g.storageClassSourcesVisible(ctx, principal, root.ClusterID) {
			graph.Warnings = []string{graphStorageAccessWarning}
		} else if len(graph.Warnings) != 0 {
			graph.Warnings = []string{"Some storage relationship sources were unavailable or incomplete."}
		}
	}
	if err := ctx.Err(); err != nil {
		return domainresource.ResourceGraph{}, err
	}
	_ = g.recordAudit(ctx, principal, connection.Summary.ID, root.Namespace, root.Kind, root.Name, string(domainaccess.ActionView), "success", "viewed Kubernetes resource relationship graph")
	return graph, nil
}

func (g *GenericResources) storageClassSourcesVisible(ctx context.Context, principal domainidentity.Principal, clusterID string) bool {
	for _, kind := range []string{"PersistentVolume", "PersistentVolumeClaim", "Pod"} {
		_, decision, err := g.decide(ctx, principal, clusterID, "", resourceGroupForKind(kind), kind, domainaccess.ActionView)
		if err != nil || !clusterWideResourceScope(decision) {
			return false
		}
	}
	return true
}

// A readable resource must not disclose a path to the root through resources
// that were removed by authorization.
func retainRootConnectedGraph(graph domainresource.ResourceGraph) domainresource.ResourceGraph {
	adjacent := make(map[string][]string, len(graph.Nodes))
	for _, edge := range graph.Edges {
		adjacent[edge.SourceID] = append(adjacent[edge.SourceID], edge.TargetID)
		adjacent[edge.TargetID] = append(adjacent[edge.TargetID], edge.SourceID)
	}
	visible := map[string]bool{graph.RootID: true}
	queue := []string{graph.RootID}
	for i := 0; i < len(queue); i++ {
		for _, id := range adjacent[queue[i]] {
			if !visible[id] {
				visible[id] = true
				queue = append(queue, id)
			}
		}
	}
	graph.Nodes = slices.DeleteFunc(graph.Nodes, func(node domainresource.ResourceGraphNode) bool { return !visible[node.ID] })
	graph.Edges = slices.DeleteFunc(graph.Edges, func(edge domainresource.ResourceGraphEdge) bool {
		return !visible[edge.SourceID] || !visible[edge.TargetID]
	})
	graph.Evidence = slices.DeleteFunc(graph.Evidence, func(item domainresource.ResourceEvidence) bool { return !visible[item.ResourceID] })
	return graph
}
