package resource

import cr "github.com/opensoha/soha-contracts/resource"

// Legacy traces are a bounded projection of the graph, never a separately inferred topology.
func (g *topologyGraph) projectLegacy() {
	outgoing := map[string][]cr.NetworkTopologyGraphEdge{}
	for _, edge := range g.view.Edges {
		outgoing[edge.Source] = append(outgoing[edge.Source], edge)
	}
	for _, node := range g.view.Nodes {
		if node.Type != "rule" {
			continue
		}
		entry := node
		for _, edge := range g.view.Edges {
			if edge.Target == node.ID && g.nodes[edge.Source].Type == "entry" {
				entry = g.nodes[edge.Source]
				break
			}
		}
		g.walkLegacy(entry, node, node.ID, outgoing, map[string]bool{}, 0)
	}
}
func (g *topologyGraph) walkLegacy(entry, rule cr.NetworkTopologyGraphNode, id string, outgoing map[string][]cr.NetworkTopologyGraphEdge, seen map[string]bool, depth int) {
	g.legacyWalks++
	if g.legacyWalks > 2000 {
		g.view.Truncated = true
		return
	}
	if depth > 12 || seen[id] || len(g.view.Traces) >= cr.NetworkTopologyDefaultEdges {
		return
	}
	next := cloneTopologySeen(seen)
	next[id] = true
	node := g.nodes[id]
	if node.Type == "service" || len(outgoing[id]) == 0 {
		state := "pending"
		if node.ResolutionState == "resolved" {
			state = "live"
		}
		sourceType := "ingress"
		if rule.Resource != nil && (rule.Resource.Kind == "HTTPRoute" || rule.Resource.Kind == "GRPCRoute") {
			sourceType = "httproute"
		}
		service := legacyTopologyNode(node, "service")
		g.view.Traces = append(g.view.Traces, cr.NetworkTopologyTraceView{ID: rule.ID + ">" + id, SourceType: sourceType, State: state, Entry: legacyTopologyNode(entry, "entry"), Route: legacyTopologyNode(rule, "ingress-route"), Service: &service, Note: "Projected from rule topology; consult graph evidence for ports, weights and resolution state."})
		return
	}
	for _, edge := range outgoing[id] {
		if edge.Relation != "mirrors" {
			g.walkLegacy(entry, rule, edge.Target, outgoing, next, depth+1)
		}
	}
}
func legacyTopologyNode(node cr.NetworkTopologyGraphNode, kind string) cr.NetworkTopologyNodeView {
	result := cr.NetworkTopologyNodeView{ID: node.ID, Name: node.Label, Kind: kind, State: "pending", Badge: node.Provider, Subtitle: node.ResolutionState}
	if node.ResolutionState == "resolved" {
		result.State = "live"
	}
	if node.Resource != nil {
		result.Namespace = node.Resource.Namespace
		result.ResourceName = node.Resource.Name
	}
	return result
}
