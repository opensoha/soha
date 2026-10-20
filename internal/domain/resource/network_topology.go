package resource

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	cr "github.com/opensoha/soha-contracts/resource"
)

type topologyObject struct {
	source     string
	ref        cr.ResourceRef
	generation int64
	body       map[string]any
}
type topologyGraph struct {
	view        cr.NetworkTopologyView
	objects     map[string]topologyObject
	ordered     []topologyObject
	nodes       map[string]cr.NetworkTopologyGraphNode
	expanded    map[string]bool
	legacyWalks int
}

// BuildNetworkTopology joins authorized, projected sources. It never treats absent coverage as an empty resource list.
func BuildNetworkTopology(clusterID, namespace string, snapshot cr.NetworkTopologySnapshot) cr.NetworkTopologyView {
	now := time.Now().UTC().Format(time.RFC3339)
	g := &topologyGraph{view: cr.NetworkTopologyView{ClusterID: clusterID, Namespace: namespace, GraphVersion: "v1", Source: "kubernetes-snapshot", GeneratedAt: now, Nodes: []cr.NetworkTopologyGraphNode{}, Edges: []cr.NetworkTopologyGraphEdge{}, Traces: []cr.NetworkTopologyTraceView{}}, objects: map[string]topologyObject{}, nodes: map[string]cr.NetworkTopologyGraphNode{}, expanded: map[string]bool{}}
	for _, source := range snapshot.Sources {
		g.view.Coverage = append(g.view.Coverage, source.Coverage)
		g.view.Truncated = g.view.Truncated || source.Coverage.Truncated
		if source.Coverage.ObservedAt > g.view.ObservedAt {
			g.view.ObservedAt = source.Coverage.ObservedAt
		}
		for _, item := range source.Objects {
			var body map[string]any
			if json.Unmarshal([]byte(item.Content), &body) != nil {
				continue
			}
			object := topologyObject{source: source.Coverage.SourceID, ref: item.Resource, generation: item.Generation, body: body}
			g.objects[topologyKey(item.Resource.Kind, item.Resource.Namespace, item.Resource.Name)] = object
			g.ordered = append(g.ordered, object)
		}
	}
	sort.Slice(g.ordered, func(i, j int) bool { return g.ordered[i].id() < g.ordered[j].id() })
	for _, object := range g.ordered {
		g.addRoute(object)
	}
	for _, object := range g.ordered {
		if object.ref.Kind == "Service" {
			g.service(object, "")
		}
	}
	for _, node := range g.nodes {
		g.view.Nodes = append(g.view.Nodes, node)
		g.countNode(node)
	}
	sort.Slice(g.view.Nodes, func(i, j int) bool { return g.view.Nodes[i].ID < g.view.Nodes[j].ID })
	sort.Slice(g.view.Edges, func(i, j int) bool { return g.view.Edges[i].ID < g.view.Edges[j].ID })
	g.projectLegacy()
	return g.view
}

func (o topologyObject) id() string {
	return strings.Join([]string{o.ref.ClusterID, o.ref.APIVersion, o.ref.Kind, o.ref.Namespace, o.ref.Name, o.ref.UID}, "|")
}
func topologyKey(kind, namespace, name string) string { return kind + "/" + namespace + "/" + name }
func (g *topologyGraph) addNode(node cr.NetworkTopologyGraphNode) string {
	if _, ok := g.nodes[node.ID]; ok {
		return node.ID
	}
	if len(g.nodes) >= cr.NetworkTopologyDefaultNodes {
		g.view.Truncated = true
		return ""
	}
	g.nodes[node.ID] = node
	return node.ID
}
func (g *topologyGraph) objectNode(o topologyObject, nodeType, provider, protocol string) string {
	ref := o.ref
	return g.addNode(cr.NetworkTopologyGraphNode{ID: o.id(), Type: nodeType, Label: ref.Name, Provider: provider, Protocol: protocol, Resource: &ref, Generation: o.generation, ResolutionState: "resolved"})
}
func (g *topologyGraph) link(source, target, relation, protocol, port string) *cr.NetworkTopologyGraphEdge {
	if source == "" || target == "" {
		return &cr.NetworkTopologyGraphEdge{}
	}
	if _, ok := g.nodes[source]; !ok {
		return &cr.NetworkTopologyGraphEdge{}
	}
	if _, ok := g.nodes[target]; !ok {
		return &cr.NetworkTopologyGraphEdge{}
	}
	if len(g.view.Edges) >= cr.NetworkTopologyDefaultEdges {
		g.view.Truncated = true
		return &cr.NetworkTopologyGraphEdge{}
	}
	edge := cr.NetworkTopologyGraphEdge{ID: fmt.Sprintf("%s>%s:%s:%d", source, target, relation, len(g.view.Edges)), Source: source, Target: target, Relation: relation, Protocol: protocol, Port: port, ResolutionState: g.nodes[target].ResolutionState}
	if ref := g.nodes[target].Resource; ref != nil && ref.Kind == "Service" && port != "" {
		if service, ok := g.objects[topologyKey("Service", ref.Namespace, ref.Name)]; ok && !servicePortExists(service, port, protocol) {
			edge.ResolutionState = "unresolved"
		}
	}
	g.view.Edges = append(g.view.Edges, edge)
	return &g.view.Edges[len(g.view.Edges)-1]
}
func (g *topologyGraph) unknown(parent, kind, namespace, name, state string) string {
	if state == "" {
		state = g.referenceState(kind, namespace)
	}
	label := name
	var ref *cr.ResourceRef
	visibility := g.referenceState(kind, namespace)
	if state == "not_visible" || state == "unknown" || visibility == "not_visible" || visibility == "unknown" {
		label = "Unresolved reference"
	} else {
		ref = &cr.ResourceRef{ClusterID: g.view.ClusterID, Kind: kind, Namespace: namespace, Name: name, APIVersion: "v1", ScopeMode: cr.ResourceScopeModeNamespace}
	}
	return g.addNode(cr.NetworkTopologyGraphNode{ID: fmt.Sprintf("%s:unresolved:%d", parent, len(g.nodes)), Type: "backend-group", Label: label, Provider: g.nodes[parent].Provider, ResolutionState: state, Resource: ref})
}
func (g *topologyGraph) referenceState(kind, namespace string) string {
	definition := map[string]string{"Service": "kubernetes.services", "Pod": "kubernetes.pods", "Gateway": "gateway.gateways", "TraefikService": "traefik.traefikservices", "Middleware": "traefik.middlewares", "MiddlewareTCP": "traefik.middlewaretcps"}[kind]
	state := "not_visible"
	for _, coverage := range g.view.Coverage {
		if coverage.SourceID != definition || (coverage.Namespace != "" && coverage.Namespace != namespace) {
			continue
		}
		if coverage.State == "available" {
			return "not_found"
		}
		if coverage.State == "partial" {
			state = "unknown"
		}
	}
	return state
}
func (g *topologyGraph) addRoute(object topologyObject) {
	switch object.ref.Kind {
	case "Ingress":
		g.ingress(object)
	case "HTTPRoute", "GRPCRoute", "TCPRoute", "TLSRoute", "UDPRoute":
		g.gatewayRoute(object)
	case "IngressRoute", "IngressRouteTCP", "IngressRouteUDP":
		g.traefikRoute(object)
	case "ApisixRoute":
		g.apisixRoute(object)
	case "ApisixTls":
		id := g.objectNode(object, "entry", "apisix", "TLS")
		g.properties(id, cr.NetworkTopologyProperty{Name: "SNI", Value: strings.Join(texts(at(object.body, "spec", "hosts")), ", ")})
		if text(at(object.body, "spec", "secret", "name")) != "" {
			g.properties(id, cr.NetworkTopologyProperty{Name: "Certificate reference", Value: "Configured; Secret existence and TLS activation unverified"})
		}
	}
}
func (g *topologyGraph) rule(object topologyObject, provider, protocol string, index int, name string, groups [][]cr.NetworkTopologyMatch) string {
	suffix := fmt.Sprint(index)
	if name != "" {
		suffix = name
	}
	ref := object.ref
	return g.addNode(cr.NetworkTopologyGraphNode{ID: object.id() + "/rule/" + suffix, Type: "rule", Label: firstTopology(name, fmt.Sprintf("Rule %d", index+1)), Provider: provider, Protocol: protocol, ResolutionState: "resolved", Resource: &ref, Generation: object.generation, Rule: &cr.NetworkTopologyRule{Name: name, Index: index, MatchGroups: groups}})
}
func (g *topologyGraph) properties(id string, values ...cr.NetworkTopologyProperty) {
	node, ok := g.nodes[id]
	if !ok {
		return
	}
	node.Properties = append(node.Properties, values...)
	g.nodes[id] = node
}
func (g *topologyGraph) state(id, state string) {
	node, ok := g.nodes[id]
	if ok {
		node.ResolutionState = state
		g.nodes[id] = node
	}
}
func (g *topologyGraph) countNode(node cr.NetworkTopologyGraphNode) {
	switch node.Type {
	case "entry":
		g.view.Summary.EntryCount++
	case "rule":
		g.view.Summary.RouteCount++
		if node.ResolutionState != "resolved" {
			g.view.Summary.PendingRouteCount++
		}
	case "service":
		g.view.Summary.ServiceCount++
	case "pod":
		g.view.Summary.BackendPodCount++
	case "backend-group":
		if node.ResolutionState == "not_found" {
			g.view.Summary.MissingServiceCount++
		}
	}
}
func at(object map[string]any, path ...string) any {
	var value any = object
	for _, key := range path {
		item, ok := value.(map[string]any)
		if !ok {
			return nil
		}
		value = item[key]
	}
	return value
}
func object(value any) map[string]any { item, _ := value.(map[string]any); return item }
func items(value any) []map[string]any {
	array, _ := value.([]any)
	result := []map[string]any{}
	for _, item := range array {
		if row, ok := item.(map[string]any); ok {
			result = append(result, row)
		}
	}
	return result
}
func texts(value any) []string {
	array, _ := value.([]any)
	result := []string{}
	for _, item := range array {
		if text, ok := item.(string); ok {
			result = append(result, text)
		}
	}
	return result
}
func text(value any) string {
	if value == nil {
		return ""
	}
	return fmt.Sprint(value)
}
func integer(value any, defaultValue int64) int64 {
	if number, ok := value.(float64); ok {
		return int64(number)
	}
	return defaultValue
}
func firstTopology(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func (g *topologyGraph) budgetReached() bool {
	if len(g.nodes) >= cr.NetworkTopologyDefaultNodes || len(g.view.Edges) >= cr.NetworkTopologyDefaultEdges {
		g.view.Truncated = true
		return true
	}
	return false
}
