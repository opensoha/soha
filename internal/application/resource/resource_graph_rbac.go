package resource

import (
	"context"
	"fmt"

	shared "github.com/opensoha/soha-contracts/resource/runtime"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const graphRBACPartial = "Some RBAC relationship sources were unavailable, unauthorized or incomplete."

func graphRBACAPIVersion(kind string) string {
	switch kind {
	case "ServiceAccount":
		return "v1"
	case "Role", "RoleBinding", "ClusterRole", "ClusterRoleBinding":
		return "rbac.authorization.k8s.io/v1"
	default:
		return ""
	}
}

func graphRBACResource(ref domainresource.ResourceRef) bool {
	api := graphRBACAPIVersion(ref.Kind)
	return api != "" && ref.APIVersion == api && graphResourceScopeValid(ref, ref.Kind == "ClusterRole" || ref.Kind == "ClusterRoleBinding")
}

type rbacGraphScope struct{ kind, namespace, name string }

func (g *GenericResources) finishRBACResourceGraph(ctx context.Context, principal domainidentity.Principal, connection domaincluster.Connection, root domainresource.ResourceRef, graph domainresource.ResourceGraph) (domainresource.ResourceGraph, error) {
	node, ok := resourceGraphRootNode(graph)
	ref := node.Resource
	if !ok || node.Status == "unresolved" || !graphRBACResource(ref) || ref.Kind != root.Kind || ref.Namespace != root.Namespace || ref.Name != root.Name {
		return domainresource.ResourceGraph{}, fmt.Errorf("%w: RBAC graph root observation does not match the request", apperrors.ErrClusterUnready)
	}

	decisions := map[rbacGraphScope]bool{{root.Kind, root.Namespace, root.Name}: true}
	allowed := func(scope rbacGraphScope) bool {
		if decision, ok := decisions[scope]; ok {
			return decision
		}
		_, decision, err := g.decide(graphResourceName(ctx, scope.name), principal, root.ClusterID, scope.namespace, resourceGroupForKind(scope.kind), scope.kind, domainaccess.ActionView)
		decisions[scope] = err == nil && graphNamedReferenceScopeAllowed(scope.namespace, decision)
		return decisions[scope]
	}

	graph.Namespace = root.Namespace
	graph, partial := filterRBACGraph(graph, allowed)
	if !rbacGraphSourcesVisible(root, allowed) || partial || len(graph.Warnings) != 0 {
		graph.Warnings = []string{graphRBACPartial}
	} else {
		graph.Warnings = []string{}
	}
	if root.Kind == "ServiceAccount" {
		graph.Warnings = append(graph.Warnings, "RoleBinding reverse lookup only covers the ServiceAccount namespace.")
	}
	if err := ctx.Err(); err != nil {
		return domainresource.ResourceGraph{}, err
	}
	_ = g.recordAudit(ctx, principal, connection.Summary.ID, root.Namespace, root.Kind, root.Name, string(domainaccess.ActionView), "success", "viewed Kubernetes resource relationship graph")
	return graph, nil
}

func filterRBACGraph(graph domainresource.ResourceGraph, allowed func(rbacGraphScope) bool) (domainresource.ResourceGraph, bool) {
	nodes := make([]domainresource.ResourceGraphNode, 0, len(graph.Nodes))
	refs := make(map[string]domainresource.ResourceRef, len(graph.Nodes))
	for _, node := range graph.Nodes {
		ref := node.Resource
		pod := ref.Kind == "Pod" && ref.APIVersion == "v1" && graphResourceScopeValid(ref, false)
		if node.ID != "" && ref.Name != "" && (graphRBACResource(ref) || pod) && allowed(rbacGraphScope{ref.Kind, ref.Namespace, ref.Name}) {
			nodes = append(nodes, node)
			refs[node.ID] = ref
		}
	}

	edges := make([]domainresource.ResourceGraphEdge, 0, len(graph.Edges))
	for _, edge := range graph.Edges {
		source, target := refs[edge.SourceID], refs[edge.TargetID]
		field := rbacGraphDeclarationField(edge.Relation, source, target)
		if field == "" {
			continue
		}
		var sourceRef *domainresource.ResourceRef
		if edge.SourceRef != nil {
			if !rbacGraphDeclarationSource(*edge.SourceRef, source) {
				continue
			}
			sourceRef = &source
		}
		// Canonical declaration paths omit array ordinals that can expose
		// hidden subjects or selectors. Free-text transport fields are unnecessary.
		edges = append(edges, domainresource.ResourceGraphEdge{
			ID: edge.ID, SourceID: edge.SourceID, TargetID: edge.TargetID, Relation: edge.Relation,
			FieldPath: field, ResolutionState: rbacGraphResolutionState(edge.ResolutionState), SourceRef: sourceRef,
		})
	}
	beforeNodes, beforeEdges := len(graph.Nodes), len(graph.Edges)
	graph.Nodes, graph.Edges = nodes, edges
	// RBAC uses the typed declaration fields on edges. Evidence can carry free
	// text and an opaque source, neither of which proves reference visibility.
	graph.Evidence = []domainresource.ResourceEvidence{}
	graph = retainRootConnectedGraph(graph)
	return graph, len(graph.Nodes) != beforeNodes || len(graph.Edges) != beforeEdges
}

func rbacGraphDeclarationField(relation string, source, target domainresource.ResourceRef) string {
	binding := source.Kind == "RoleBinding" || source.Kind == "ClusterRoleBinding"
	switch relation {
	case shared.ResourceGraphRelationBindsRole:
		if binding && target.Kind == "ClusterRole" || source.Kind == "RoleBinding" && target.Kind == "Role" && source.Namespace == target.Namespace {
			return "roleRef"
		}
	case shared.ResourceGraphRelationBindsSubject:
		if binding && target.Kind == "ServiceAccount" {
			return "subjects"
		}
	case shared.ResourceGraphRelationAggregatesRole:
		if source.Kind == "ClusterRole" && target.Kind == "ClusterRole" && source.Name != target.Name {
			return "aggregationRule.clusterRoleSelectors"
		}
	case shared.ResourceGraphRelationUsesServiceAccount:
		if source.Kind == "Pod" && target.Kind == "ServiceAccount" && source.Namespace == target.Namespace {
			return "spec.serviceAccountName"
		}
	}
	return ""
}

func rbacGraphDeclarationSource(ref, source domainresource.ResourceRef) bool {
	return graphAuthorizedIdentity(ref) == graphAuthorizedIdentity(source) &&
		graphResourceScopeValid(ref, source.Namespace == "")
}

func rbacGraphResolutionState(state string) string {
	switch state {
	case "resolved", "unresolved":
		return state
	default:
		return ""
	}
}

// Assess visibility for possible sources even if the graph has no matching
// objects. Otherwise the presence of a warning could reveal hidden bindings.
func rbacGraphSourcesVisible(root domainresource.ResourceRef, allowed func(rbacGraphScope) bool) bool {
	var sources []rbacGraphScope
	switch root.Kind {
	case "ServiceAccount":
		// Collection of RoleBindings only covers the ServiceAccount namespace.
		return false
	case "Role":
		sources = []rbacGraphScope{{kind: "RoleBinding", namespace: root.Namespace}}
	case "RoleBinding":
		sources = []rbacGraphScope{{kind: "Role", namespace: root.Namespace}, {kind: "ClusterRole"}, {kind: "ServiceAccount"}}
	case "ClusterRole":
		sources = []rbacGraphScope{{kind: "RoleBinding"}, {kind: "ClusterRoleBinding"}}
	case "ClusterRoleBinding":
		sources = []rbacGraphScope{{kind: "ClusterRole"}, {kind: "ServiceAccount"}}
	}
	for _, scope := range sources {
		if !allowed(scope) {
			return false
		}
	}
	return true
}
