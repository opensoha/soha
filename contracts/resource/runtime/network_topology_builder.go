package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	cr "github.com/opensoha/soha/contracts/resource"
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
	ctx         context.Context
	nodeLimit   int
	edgeLimit   int
	hiddenRefs  map[string]cr.ResourceRef
}

// The public topology permits coverage for both issued and denied source queries.
const topologyCoverageLimit = 128

// BuildNetworkTopology joins authorized, projected sources. It never treats absent coverage as an empty resource list.
func BuildNetworkTopology(clusterID, namespace string, snapshot cr.NetworkTopologySnapshot) cr.NetworkTopologyView {
	return buildNetworkTopology(context.Background(), clusterID, namespace, snapshot, cr.NetworkTopologyDefaultNodes, cr.NetworkTopologyDefaultEdges, nil).view
}

func buildNetworkTopology(ctx context.Context, clusterID, namespace string, snapshot cr.NetworkTopologySnapshot, nodeLimit, edgeLimit int, root *cr.ResourceRef) *topologyGraph {
	now := time.Now().UTC().Format(time.RFC3339)
	g := &topologyGraph{hiddenRefs: map[string]cr.ResourceRef{}, ctx: ctx, nodeLimit: nodeLimit, edgeLimit: edgeLimit, view: cr.NetworkTopologyView{ClusterID: clusterID, Namespace: namespace, GraphVersion: "v1", Source: "kubernetes-snapshot", GeneratedAt: now, Nodes: []cr.NetworkTopologyGraphNode{}, Edges: []cr.NetworkTopologyGraphEdge{}, Traces: []cr.NetworkTopologyTraceView{}}, objects: map[string]topologyObject{}, nodes: map[string]cr.NetworkTopologyGraphNode{}, expanded: map[string]bool{}}
	totalBytes := 0
	for sourceIndex, source := range snapshot.Sources {
		if sourceIndex >= topologyCoverageLimit {
			g.view.Truncated = true
			break
		}
		g.view.Coverage = append(g.view.Coverage, source.Coverage)
		g.view.Truncated = g.view.Truncated || source.Coverage.Truncated
		if source.Coverage.ObservedAt > g.view.ObservedAt {
			g.view.ObservedAt = source.Coverage.ObservedAt
		}
		for objectIndex, item := range source.Objects {
			if objectIndex >= cr.NetworkTopologyMaxSourceItems {
				g.view.Truncated = true
				break
			}
			if _, exists := g.objects[topologyKey(item.Resource.Kind, item.Resource.Namespace, item.Resource.Name)]; exists {
				continue
			}
			totalBytes += len(item.Content)
			if totalBytes > cr.NetworkTopologyMaxTotalBytes {
				g.view.Truncated = true
				break
			}
			if ctx.Err() != nil || len(g.ordered) >= ResourceGraphWorkingNodeLimit {
				g.view.Truncated = true
				break
			}
			if object, ok := decodeTopologyObject(clusterID, source, item); ok {
				key := topologyKey(object.ref.Kind, object.ref.Namespace, object.ref.Name)
				if _, exists := g.objects[key]; !exists {
					g.objects[key] = object
					g.ordered = append(g.ordered, object)
					if root != nil && object.ref.Kind == root.Kind && object.ref.Namespace == root.Namespace && object.ref.Name == root.Name && object.ref.APIVersion == root.APIVersion {
						g.objectNode(object, "backend-group", topologyProvider(object.source), "")
					}
				}
			} else if source.Coverage.State == "available" || source.Coverage.State == "partial" {
				index := len(g.view.Coverage) - 1
				g.view.Coverage[index].State = "partial"
				g.view.Coverage[index].ReasonCode = "invalid_projection"
				g.view.Truncated = true
			}
		}
	}
	sort.Slice(g.ordered, func(i, j int) bool { return g.ordered[i].id() < g.ordered[j].id() })
	prioritized := g.prioritizeRoot(root)
	for _, object := range g.ordered {
		if object.id() == prioritized {
			continue
		}
		g.addRoute(object)
	}
	for _, object := range g.ordered {
		if object.ref.Kind == "Service" && object.id() != prioritized {
			g.service(object, "")
		}
	}
	for _, o := range g.ordered {
		if o.source == "kubernetes.endpointslices" && !g.expanded["slice:"+o.id()] {
			g.endpointSlice(topologyObject{}, o, "", "")
		}
	}
	g.declarations(prioritized)
	for i := range g.view.Edges {
		e := &g.view.Edges[i]
		e.ID = strings.Join([]string{e.Source, e.Target, e.Relation, e.Protocol, e.Port, e.SectionName, e.FieldPath, fmt.Sprint(i)}, "|")
	}
	for _, node := range g.nodes {
		g.view.Nodes = append(g.view.Nodes, node)
		g.countNode(node)
	}
	sort.Slice(g.view.Nodes, func(i, j int) bool { return g.view.Nodes[i].ID < g.view.Nodes[j].ID })
	sort.Slice(g.view.Edges, func(i, j int) bool { return g.view.Edges[i].ID < g.view.Edges[j].ID })
	if root == nil {
		g.projectLegacy()
	}
	return g
}

func (o topologyObject) id() string {
	return strings.Join([]string{o.ref.ClusterID, o.ref.APIVersion, o.ref.Kind, o.ref.Namespace, o.ref.Name, o.ref.UID}, "|")
}
func topologyKey(kind, namespace, name string) string { return kind + "/" + namespace + "/" + name }
func (g *topologyGraph) addNode(node cr.NetworkTopologyGraphNode) string {
	if g.ctx.Err() != nil {
		return ""
	}
	if _, ok := g.nodes[node.ID]; ok {
		return node.ID
	}
	if len(g.nodes) >= g.nodeLimit {
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
	if len(g.view.Edges) >= g.edgeLimit {
		g.view.Truncated = true
		return &cr.NetworkTopologyGraphEdge{}
	}
	edge := cr.NetworkTopologyGraphEdge{ID: fmt.Sprintf("%s>%s:%s:%d", source, target, relation, len(g.view.Edges)), Source: source, Target: target, Relation: relation, Protocol: protocol, Port: port, SourceRef: g.nodes[source].Resource, ResolutionState: g.nodes[target].ResolutionState}
	if ref := g.nodes[target].Resource; ref != nil && ref.Kind == "Service" {
		if service, ok := g.objects[topologyKey("Service", ref.Namespace, ref.Name)]; ok && target == service.id() {
			edge.ResolutionState = "resolved"
			if port != "" && !servicePortExists(service, port, protocol) {
				edge.ResolutionState = "unresolved"
			}
		}
	}
	g.view.Edges = append(g.view.Edges, edge)
	return &g.view.Edges[len(g.view.Edges)-1]
}
func (g *topologyGraph) unknown(parent, kind, namespace, name, state string) string {
	if state == "" {
		state = g.referenceState(kind, namespace)
	}
	if state == "unsupported_fragment" {
		return g.addNode(cr.NetworkTopologyGraphNode{ID: fmt.Sprintf("%s:unsupported:%d", parent, len(g.nodes)), Type: "backend-group", Label: "Unsupported reference", Provider: g.nodes[parent].Provider, ResolutionState: state})
	}
	label := name
	api := "v1"
	for _, d := range cr.NetworkTopologySources() {
		if d.Kind == kind {
			api = strings.TrimPrefix(d.Group+"/"+d.Version, "/")
			break
		}
	}
	ref := &cr.ResourceRef{ClusterID: g.view.ClusterID, Kind: kind, Namespace: namespace, Name: name, APIVersion: api, ScopeMode: cr.ResourceScopeModeNamespace}
	if namespace == "" {
		ref.ScopeMode = cr.ResourceScopeModeCluster
	}
	id := fmt.Sprintf("%s:unresolved:%d", parent, len(g.nodes))
	visibility := g.referenceState(kind, namespace)
	if state == "not_visible" || state == "unknown" || visibility == "not_visible" || visibility == "unknown" {
		g.hiddenRefs[id] = *ref
		ref = nil
		label = "Unresolved reference"
	}
	return g.addNode(cr.NetworkTopologyGraphNode{ID: id, Type: "backend-group", Label: label, Provider: g.nodes[parent].Provider, ResolutionState: state, Resource: ref})
}

func (g *topologyGraph) referenceState(kind, namespace string) string {
	definition := map[string]string{"Service": "kubernetes.services", "Pod": "kubernetes.pods", "Gateway": "gateway.gateways", "TraefikService": "traefik.traefikservices", "Middleware": "traefik.middlewares", "MiddlewareTCP": "traefik.middlewaretcps"}[kind]
	state := "not_visible"
	for _, coverage := range g.view.Coverage {
		if coverage.Name != "" || coverage.SourceID != definition || (coverage.Namespace != "" && coverage.Namespace != namespace) {
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
	if len(g.nodes) >= g.nodeLimit || len(g.view.Edges) >= g.edgeLimit {
		g.view.Truncated = true
		return true
	}
	return false
}

// decodeTopologyObject enforces the fixed source's exact GVK and re-applies the
// allowlist before any relationship interpretation (also for local snapshots).
func decodeTopologyObject(cluster string, source cr.NetworkTopologySourceResult, item cr.NetworkTopologySourceObject) (topologyObject, bool) {
	d, ok := cr.LookupNetworkTopologySource(source.Coverage.SourceID)

	ref := item.Resource
	ref.APIVersion = strings.TrimPrefix(ref.APIVersion, "/")
	if !ok || ref.ClusterID != cluster || ref.Kind != d.Kind || !topologyDefinitionAPI(d, ref.APIVersion) || ref.Name == "" || len(item.Content) > cr.NetworkTopologyMaxObjectBytes {
		return topologyObject{}, false
	}
	if d.Namespaced && ref.Namespace == "" || !d.Namespaced && ref.Namespace != "" || source.Coverage.Namespace != "" && ref.Namespace != source.Coverage.Namespace {
		return topologyObject{}, false
	}
	if source.Coverage.Name != "" && source.Coverage.Name != ref.Name {
		return topologyObject{}, false
	}
	if source.Coverage.State != "available" && source.Coverage.State != "partial" {
		return topologyObject{}, false
	}
	var body map[string]any
	if json.Unmarshal([]byte(item.Content), &body) != nil {
		return topologyObject{}, false
	}
	for key, want := range map[string]string{"name": ref.Name, "namespace": ref.Namespace, "uid": ref.UID} {
		if actual := text(at(body, "metadata", key)); actual != "" && actual != want {
			return topologyObject{}, false
		}
	}
	body, err := cr.ProjectNetworkTopologyObject(d.ID, body)
	if err != nil {
		return topologyObject{}, false
	}
	ref.ScopeMode = cr.ResourceScopeModeCluster
	if d.Namespaced {
		ref.ScopeMode = cr.ResourceScopeModeNamespace
	}
	return topologyObject{source: d.ID, ref: ref, generation: item.Generation, body: body}, true
}

func topologyDefinitionAPI(d cr.NetworkTopologySourceDefinition, api string) bool {
	for _, version := range cr.NetworkTopologySourceVersions(d) {
		if api == strings.TrimPrefix(d.Group+"/"+version, "/") {
			return true
		}
	}
	return false
}

// Interpret the selected root before unrelated high-fanout sources can consume
// the bounded relationship set. Reuse the same rule methods; do not rescan input.
func (g *topologyGraph) prioritizeRoot(root *cr.ResourceRef) string {
	if root == nil {
		return ""
	}
	o, ok := g.objects[topologyKey(root.Kind, root.Namespace, root.Name)]
	if !ok {
		return ""
	}
	g.addRoute(o)
	switch o.source {
	case "kubernetes.services":
		g.service(o, "")
	case "kubernetes.endpointslices":
		g.endpointSlice(topologyObject{}, o, "", "")
	}
	g.objectDeclarations(o)
	return o.id()
}
