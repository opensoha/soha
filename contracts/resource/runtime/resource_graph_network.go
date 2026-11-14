package runtime

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	cr "github.com/opensoha/soha/contracts/resource"
)

// BuildNetworkResourceGraph consumes the same projected facts as topology.
// Callers own source authorization and bounded collection; no network I/O occurs.
func BuildNetworkResourceGraph(ctx context.Context, root cr.ResourceRef, snapshot cr.NetworkTopologySnapshot) (cr.ResourceGraph, error) {
	if err := ctx.Err(); err != nil {
		return cr.ResourceGraph{}, err
	}
	root, err := NormalizeResourceGraphRoot(root)
	if err != nil {
		return cr.ResourceGraph{}, err
	}
	if _, ok := topologySourceFor(root.Kind, root.APIVersion); !ok {
		return cr.ResourceGraph{}, ErrUnsupportedResourceGraphRoot
	}
	snapshot, err = prioritizeNetworkRoot(ctx, root, snapshot)
	if err != nil {
		return cr.ResourceGraph{}, err
	}
	g := buildNetworkTopology(ctx, root.ClusterID, root.Namespace, snapshot, ResourceGraphWorkingNodeLimit, ResourceGraphWorkingEdgeLimit, &root)
	if err := ctx.Err(); err != nil {
		return cr.ResourceGraph{}, err
	}
	actual := g.objects[topologyKey(root.Kind, root.Namespace, root.Name)]
	g.objectNode(actual, "backend-group", topologyProvider(actual.source), "")
	graph := cr.ResourceGraph{ClusterID: root.ClusterID, Namespace: root.Namespace, GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano), RootID: graphNodeID(actual.ref), Nodes: []cr.ResourceGraphNode{}, Edges: []cr.ResourceGraphEdge{}, Evidence: []cr.ResourceEvidence{}, Warnings: []string{}}
	nodes, edges, evidence, err := networkResourceFacts(ctx, g)
	if err != nil {
		return cr.ResourceGraph{}, err
	}
	selected, chosen, depth, limit, err := networkGraphTraverse(ctx, graph.RootID, edges)
	if err != nil {
		return cr.ResourceGraph{}, err
	}
	for id := range selected {
		graph.Nodes = append(graph.Nodes, nodes[id])
	}
	for _, edge := range edges {
		if chosen[edge.ID] {
			graph.Edges = append(graph.Edges, edge)
		}
	}
	for _, e := range evidence {
		if selected[e.ResourceID] {
			graph.Evidence = append(graph.Evidence, e)
		}
	}
	sort.Slice(graph.Nodes, func(i, j int) bool { return graph.Nodes[i].ID < graph.Nodes[j].ID })
	if depth {
		graph.Warnings = append(graph.Warnings, "network resource graph truncated by depth at 3 hops")
	}
	if limit || g.view.Truncated {
		graph.Warnings = append(graph.Warnings, "network resource graph is partial due to source identity or graph budgets")
	}
	seen := map[string]bool{}
	for _, c := range g.view.Coverage {
		if c.State != "available" || c.Truncated {
			w := fmt.Sprintf("network source %s namespace=%s name=%s: %s (%s)", c.SourceID, c.Namespace, c.Name, c.State, c.ReasonCode)
			if !seen[w] {
				graph.Warnings = append(graph.Warnings, w)
				seen[w] = true
			}
		}
	}
	for _, node := range graph.Nodes {
		if node.Resource.APIVersion == "v1" && node.Resource.Kind == "Secret" && node.Status != "observed" {
			graph.Warnings = append(graph.Warnings, "TLS Secret references are declarations; metadata and existence are unverified")
			break
		}
	}
	for _, node := range g.view.Nodes {
		if node.ResolutionState == "unsupported_fragment" {
			graph.Warnings = append(graph.Warnings, "Unsupported network reference fragments could not be resolved")
			break
		}
	}
	sort.Strings(graph.Warnings)
	return graph, ctx.Err()
}

func prioritizeNetworkRoot(ctx context.Context, root cr.ResourceRef, snapshot cr.NetworkTopologySnapshot) (cr.NetworkTopologySnapshot, error) {
	sources := append([]cr.NetworkTopologySourceResult(nil), snapshot.Sources...)
	sort.SliceStable(sources, func(i, j int) bool { return sources[i].Coverage.Name != "" && sources[j].Coverage.Name == "" })
	for _, s := range sources {
		for _, item := range s.Objects {
			if err := ctx.Err(); err != nil {
				return snapshot, err
			}
			if item.Resource.Kind != root.Kind || item.Resource.APIVersion != root.APIVersion || item.Resource.Namespace != root.Namespace || item.Resource.Name != root.Name {
				continue
			}
			if root.UID != "" && item.Resource.UID != root.UID {
				return snapshot, invalidGraphRoot("network root UID changed")
			}
			if _, ok := decodeTopologyObject(root.ClusterID, s, item); !ok {
				return snapshot, invalidGraphRoot("invalid projected network root")
			}
			first := cr.NetworkTopologySourceResult{Coverage: s.Coverage, Objects: []cr.NetworkTopologySourceObject{item}}
			snapshot.Sources = append([]cr.NetworkTopologySourceResult{first}, snapshot.Sources...)
			return snapshot, nil
		}
	}
	return snapshot, invalidGraphRoot("network resource graph root not present")
}

func networkResourceFacts(ctx context.Context, g *topologyGraph) (map[string]cr.ResourceGraphNode, []cr.ResourceGraphEdge, []cr.ResourceEvidence, error) {
	nodes := map[string]cr.ResourceGraphNode{}
	ids := map[string]string{}
	evidence := []cr.ResourceEvidence{}
	keys := make([]string, 0, len(g.nodes))
	for id := range g.nodes {
		keys = append(keys, id)
	}
	sort.Strings(keys)
	for _, id := range keys {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		n := g.nodes[id]
		if ref, ok := g.hiddenRefs[id]; ok {
			n.Resource = &ref
		}
		if n.Resource == nil || n.Resource.Name == "" || n.Resource.APIVersion == "" {
			continue
		}
		ref := *n.Resource
		rid := graphNodeID(ref)
		ids[id] = rid
		state := "unresolved"
		if o, ok := g.objects[topologyKey(ref.Kind, ref.Namespace, ref.Name)]; ok && o.ref.APIVersion == ref.APIVersion && o.ref.UID == ref.UID {
			state = "observed"
		}
		if previous, ok := nodes[rid]; !ok || previous.Status != "observed" {
			nodes[rid] = cr.ResourceGraphNode{ID: rid, Resource: ref, Status: state}
		}
		if ref.Kind == "NetworkPolicy" && n.Rule == nil {
			for i, p := range n.Properties {
				evidence = append(evidence, cr.ResourceEvidence{ID: fmt.Sprintf("%s:policy:%d", rid, i), Type: "policy-declaration", Severity: "info", Summary: p.Name + ": " + p.Value, ObservedAt: g.view.GeneratedAt, ResourceID: rid})
			}
		}
	}
	edges := []cr.ResourceGraphEdge{}
	seen := map[string]bool{}
	for _, e := range g.view.Edges {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		source, target := ids[e.Source], ids[e.Target]
		if source == "" || target == "" || source == target {
			continue
		}
		relation := e.Relation
		if relation == "forwards" || relation == "weights" {
			relation = "routes-to"
		}
		if relation == "matches" {
			relation = "attaches-to"
		}
		edge := cr.ResourceGraphEdge{SourceID: source, TargetID: target, Relation: relation, ResolutionState: e.ResolutionState, Protocol: e.Protocol, Port: e.Port, SectionName: e.SectionName, FieldPath: e.FieldPath, Note: e.Note, SourceRef: e.SourceRef}
		edge.ID = strings.Join([]string{relation, source, target, e.Protocol, e.Port, e.SectionName, e.FieldPath}, "|")
		if !seen[edge.ID] {
			seen[edge.ID] = true
			edges = append(edges, edge)
		}
	}
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	return nodes, edges, evidence, nil
}

// The two walks never reverse direction at a selector, a class, or shared
// reference target. Work is bounded by the already bounded relationship set.
func networkGraphTraverse(ctx context.Context, root string, edges []cr.ResourceGraphEdge) (map[string]bool, map[string]bool, bool, bool, error) {
	adjacency := map[int]map[string][]cr.ResourceGraphEdge{1: {}, -1: {}}
	for _, e := range edges {
		adjacency[1][e.SourceID] = append(adjacency[1][e.SourceID], e)
		adjacency[-1][e.TargetID] = append(adjacency[-1][e.TargetID], e)
	}
	selected, chosen := map[string]bool{root: true}, map[string]bool{}
	depthTruncated, limitTruncated := false, false
	for _, direction := range []int{1, -1} {
		queue := []graphTraversalState{{id: root, direction: direction}}
		visited := map[string]bool{root: true}
		for len(queue) > 0 {
			if err := ctx.Err(); err != nil {
				return nil, nil, false, false, err
			}
			current := queue[0]
			queue = queue[1:]
			for _, edge := range adjacency[direction][current.id] {
				next := edge.TargetID
				if direction == -1 {
					next = edge.SourceID
				}
				if current.depth >= 3 {
					if !visited[next] {
						depthTruncated = true
					}
					continue
				}
				if !selected[next] && len(selected) >= cr.NetworkTopologyMaxNodes || !chosen[edge.ID] && len(chosen) >= cr.NetworkTopologyMaxEdges {
					limitTruncated = true
					continue
				}
				selected[next] = true
				chosen[edge.ID] = true
				if !visited[next] {
					visited[next] = true
					queue = append(queue, graphTraversalState{id: next, depth: current.depth + 1, direction: direction})
				}
			}
		}
	}
	return selected, chosen, depthTruncated, limitTruncated, nil
}
