package resource

import (
	"context"
	"fmt"
	"time"

	shared "github.com/opensoha/soha-contracts/resource/runtime"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (g *GenericResources) GetResourceGraph(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind, name string) (domainresource.ResourceGraph, error) {
	ctx = context.WithValue(ctx, graphNamespacePolicyKey{}, true)
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	root, err := shared.NormalizeResourceGraphRoot(domainresource.ResourceRef{ClusterID: clusterID, Namespace: namespace, Kind: kind, Name: name})
	if err != nil {
		return domainresource.ResourceGraph{}, fmt.Errorf("%w: %w", apperrors.ErrInvalidArgument, err)
	}
	clusterID, kind, name, namespace = root.ClusterID, root.Kind, root.Name, root.Namespace
	rbac := graphRBACAPIVersion(kind) != ""
	connection, err := g.authorizeGraphView(ctx, principal, clusterID, root, kind == "Namespace" || graphStorageKind(kind) || rbac)
	if err != nil {
		return domainresource.ResourceGraph{}, err
	}
	var graph domainresource.ResourceGraph
	var snapshotAllowed map[string]bool
	if graphNetworkRoot(root) {
		graph, snapshotAllowed, err = g.getNetworkResourceGraph(ctx, principal, connection, root)
	} else if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		graph, err = g.readAgentResourceGraph(ctx, connection, root)
	} else if g.resourceGraph == nil {
		return domainresource.ResourceGraph{}, fmt.Errorf("%w: direct resource graph is not configured", apperrors.ErrClusterUnready)
	} else {
		graph, err = g.resourceGraph.GetResourceGraph(ctx, clusterID, namespace, kind, name)
	}
	if err != nil {
		return domainresource.ResourceGraph{}, err
	}
	// The selected connection owns identity; remote Agent IDs are local configuration.
	graph.ClusterID = clusterID
	for i := range graph.Nodes {
		graph.Nodes[i].Resource.ClusterID = clusterID
	}
	for i := range graph.Edges {
		if graph.Edges[i].SourceRef != nil {
			ref := *graph.Edges[i].SourceRef
			ref.ClusterID = clusterID
			graph.Edges[i].SourceRef = &ref
		}
	}
	if graphStorageKind(kind) {
		return g.finishStorageResourceGraph(ctx, principal, connection, root, graph)
	}
	if rbac {
		return g.finishRBACResourceGraph(ctx, principal, connection, root, graph)
	}
	graph, customAllowed, err := g.enrichCustomResourceGraph(ctx, principal, connection, graph)
	if err != nil {
		return domainresource.ResourceGraph{}, err
	}
	for identity, allowed := range snapshotAllowed {
		if allowed {
			customAllowed[identity] = true
		}
	}
	graph = g.filterAuthorizedGraph(ctx, principal, clusterID, graph, customAllowed)
	if err := ctx.Err(); err != nil {
		return domainresource.ResourceGraph{}, err
	}
	_ = g.recordAudit(ctx, principal, connection.Summary.ID, namespace, kind, name, string(domainaccess.ActionView), "success", "viewed Kubernetes resource relationship graph")
	return graph, nil
}

func (g *GenericResources) readAgentResourceGraph(ctx context.Context, connection domaincluster.Connection, root domainresource.ResourceRef) (domainresource.ResourceGraph, error) {
	client, err := g.genericResourceAgentClient(connection)
	if err != nil {
		return domainresource.ResourceGraph{}, err
	}
	graph, err := client.GetResourceGraph(ctx, root.Namespace, root.Kind, root.Name)
	if err != nil {
		return domainresource.ResourceGraph{}, err
	}
	if !graphMatchesRequestedBuiltinRoot(graph, root) {
		return domainresource.ResourceGraph{}, fmt.Errorf("%w: Agent graph root observation does not match the request", apperrors.ErrClusterUnready)
	}
	return graph, nil
}

func graphMatchesRequestedBuiltinRoot(graph domainresource.ResourceGraph, root domainresource.ResourceRef) bool {
	if graph.RootID == "" {
		return false
	}
	found := false
	for _, node := range graph.Nodes {
		if node.ID != graph.RootID {
			continue
		}
		ref := node.Resource
		if found || node.Status == "unresolved" || ref.Kind != root.Kind || ref.Name != root.Name || ref.Namespace != root.Namespace || !graphBuiltinResource(ref) {
			return false
		}
		found = true
	}
	return found
}

func (g *GenericResources) GetSecurityPosture(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string, limit int) (domainresource.SecurityPosture, error) {
	connection, _, err := g.authorize(ctx, principal, clusterID, "", "Cluster", domainaccess.ActionView)
	if err != nil {
		return domainresource.SecurityPosture{}, err
	}
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var posture domainresource.SecurityPosture
	if connection.Summary.ConnectionMode == domaincluster.ConnectionModeAgent {
		client, clientErr := g.genericResourceAgentClient(connection)
		if clientErr != nil {
			return domainresource.SecurityPosture{}, clientErr
		}
		posture, err = client.GetSecurityPosture(ctx, namespace, limit)
	} else if g.security == nil {
		return domainresource.SecurityPosture{}, fmt.Errorf("%w: direct security posture is not configured", apperrors.ErrClusterUnready)
	} else {
		posture, err = g.security.GetSecurityPosture(ctx, clusterID, namespace, limit)
	}
	if err != nil {
		return domainresource.SecurityPosture{}, err
	}
	posture = g.filterAuthorizedFindings(ctx, principal, clusterID, posture)
	_ = g.recordAudit(ctx, principal, connection.Summary.ID, namespace, "SecurityPosture", "", string(domainaccess.ActionView), "success", "viewed read-only Kubescape security posture")
	return posture, nil
}

func (g *GenericResources) filterAuthorizedGraph(ctx context.Context, principal domainidentity.Principal, clusterID string, graph domainresource.ResourceGraph, customAllowed map[string]bool) domainresource.ResourceGraph {
	networkGraph := graphHasNetworkRoot(graph)
	storageGraph := graphHasStorageRoot(graph)
	referenceAllowed := func(ref domainresource.ResourceRef) bool {
		return g.graphNodeReferenceAllowed(ctx, principal, clusterID, ref, customAllowed, networkGraph)
	}
	if storageGraph {
		referenceAllowed = g.storageGraphReferenceFilter(ctx, principal, clusterID)
	}
	return filterGraphReferences(graph, referenceAllowed, storageGraph)
}

func filterGraphReferences(graph domainresource.ResourceGraph, referenceAllowed func(domainresource.ResourceRef) bool, storageGraph bool) domainresource.ResourceGraph {
	allowed := make(map[string]struct{}, len(graph.Nodes))
	nodes := make([]domainresource.ResourceGraphNode, 0, len(graph.Nodes))
	hidden := 0
	for _, node := range graph.Nodes {
		if !referenceAllowed(node.Resource) {
			hidden++
			continue
		}
		allowed[node.ID] = struct{}{}
		nodes = append(nodes, node)
	}
	edges := make([]domainresource.ResourceGraphEdge, 0, len(graph.Edges))
	for _, edge := range graph.Edges {
		_, source := allowed[edge.SourceID]
		_, target := allowed[edge.TargetID]
		if source && target {
			if edge.SourceRef != nil && !referenceAllowed(*edge.SourceRef) {
				hidden++
				continue
			}
			edges = append(edges, edge)
		}
	}
	evidence := make([]domainresource.ResourceEvidence, 0, len(graph.Evidence))
	for _, item := range graph.Evidence {
		if item.ResourceID == "" {
			evidence = append(evidence, item)
			continue
		}
		if _, ok := allowed[item.ResourceID]; ok {
			evidence = append(evidence, item)
		}
	}
	if hidden > 0 && (storageGraph || graphHasNetworkEvidence(graph)) {
		for i := range edges {
			edges[i].Note = ""
			edges[i].FieldPath = ""
		}
		evidence = []domainresource.ResourceEvidence{}
	}
	graph.Nodes, graph.Edges, graph.Evidence = nodes, edges, evidence
	if hidden > 0 && storageGraph {
		// Source failures and truncation can distinguish absent and present
		// references even when both identities were filtered out.
		graph.Warnings = []string{graphStorageAccessWarning}
	} else if hidden > 0 {
		graph.Warnings = append(graph.Warnings, "Some related resources were hidden by access policy.")
	}
	return graph
}

func (g *GenericResources) filterAuthorizedFindings(ctx context.Context, principal domainidentity.Principal, clusterID string, posture domainresource.SecurityPosture) domainresource.SecurityPosture {
	findings := make([]domainresource.SecurityFinding, 0, len(posture.Findings))
	hidden := 0
	for _, finding := range posture.Findings {
		if finding.Resource == nil {
			findings = append(findings, finding)
			continue
		}
		if _, _, err := g.authorize(ctx, principal, clusterID, finding.Resource.Namespace, finding.Resource.Kind, domainaccess.ActionView); err != nil {
			hidden++
			continue
		}
		findings = append(findings, finding)
	}
	posture.Findings = findings
	if hidden > 0 {
		posture.Counts = domainresource.SecuritySeverityCounts{}
		posture.Warnings = append(posture.Warnings, "Some security findings and aggregate severity totals were hidden by access policy.")
	}
	return posture
}

// Graph reads need only a view decision. Expanding unrelated button actions for
// every node multiplies policy/database work without changing this response.
func (g *GenericResources) authorizeGraphView(ctx context.Context, principal domainidentity.Principal, clusterID string, ref domainresource.ResourceRef, requireNamedScope bool) (domaincluster.Connection, error) {
	namespace, kind := graphAuthorizationNamespace(ref), ref.Kind
	connection, decision, err := g.decide(graphResourceName(ctx, ref.Name), principal, clusterID, namespace, resourceGroupForKind(kind), kind, domainaccess.ActionView)
	if err != nil {
		return domaincluster.Connection{}, err
	}
	if !decision.Allowed || (requireNamedScope || resourceGroupForKind(kind) == "network") && !graphNamedReferenceScopeAllowed(namespace, decision) {
		reason := decision.Reason
		if decision.Allowed {
			reason = "resource graph reference scope unavailable"
		}
		_ = g.recordAudit(ctx, principal, connection.Summary.ID, namespace, kind, ref.Name, string(domainaccess.ActionView), "deny", reason)
		return domaincluster.Connection{}, fmt.Errorf("%w: %s", apperrors.ErrAccessDenied, reason)
	}
	return connection, nil
}

func (g *GenericResources) graphNodeReferenceAllowed(ctx context.Context, principal domainidentity.Principal, clusterID string, ref domainresource.ResourceRef, customAllowed map[string]bool, network bool) bool {
	if !graphBuiltinResource(ref) {
		return customAllowed[graphAuthorizedIdentity(ref)] && g.customGraphReferenceAllowed(ctx, principal, clusterID, ref)
	}
	requireNamedScope := network || ref.Kind == "Namespace" || graphRBACResource(ref) || (graphStorageKind(ref.Kind) && ref.Namespace == "")
	_, err := g.authorizeGraphView(ctx, principal, clusterID, ref, requireNamedScope)
	return err == nil
}

func graphAuthorizationNamespace(ref domainresource.ResourceRef) string {
	if ref.Kind == "Namespace" {
		return ref.Name
	}
	return ref.Namespace
}

func graphHasNetworkEvidence(graph domainresource.ResourceGraph) bool {
	for _, node := range graph.Nodes {
		if node.ID == graph.RootID && resourceGroupForKind(node.Resource.Kind) == "network" {
			return true
		}
	}
	for _, edge := range graph.Edges {
		if edge.SourceRef != nil || edge.FieldPath != "" || edge.ResolutionState != "" {
			return true
		}
	}
	return false
}

func graphHasNetworkRoot(graph domainresource.ResourceGraph) bool {
	for _, node := range graph.Nodes {
		if node.ID == graph.RootID {
			return graphNetworkRoot(node.Resource)
		}
	}
	return false
}
