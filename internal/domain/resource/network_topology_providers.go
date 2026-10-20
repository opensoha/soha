package resource

import (
	"fmt"
	cr "github.com/opensoha/soha-contracts/resource"
	"strings"
)

func (g *topologyGraph) traefikRoute(o topologyObject) {
	protocol := map[string]string{"IngressRoute": "HTTP", "IngressRouteTCP": "TCP", "IngressRouteUDP": "UDP"}[o.ref.Kind]
	entry := g.objectNode(o, "entry", "traefik", protocol)
	g.properties(entry, cr.NetworkTopologyProperty{Name: "Entry points", Value: strings.Join(texts(at(o.body, "spec", "entryPoints")), ", ")})
	for index, raw := range items(at(o.body, "spec", "routes")) {
		rule := g.rule(o, "traefik", protocol, index, "", nil)
		node := g.nodes[rule]
		if node.Rule != nil {
			node.Rule.MatchSummary = text(raw["match"])
			priority := integer(raw["priority"], 0)
			node.Rule.Priority = &priority
			node.Rule.TLSMode = ""
			if at(o.body, "spec", "tls") != nil {
				node.Rule.TLSMode = "terminate"
				if at(o.body, "spec", "tls", "passthrough") == true {
					node.Rule.TLSMode = "passthrough"
				}
			}
		}
		g.nodes[rule] = node
		if text(raw["match"]) == "[redacted match expression]" {
			g.state(rule, "unsupported_fragment")
		}
		g.link(entry, rule, "matches", protocol, "")
		tail := rule
		kind := "Middleware"
		if protocol != "HTTP" {
			kind = "MiddlewareTCP"
		}
		for _, middleware := range items(raw["middlewares"]) {
			tail = g.traefikMiddleware(o, tail, middleware, kind, protocol, map[string]bool{}, 0)
		}
		total := int64(0)
		for _, backend := range items(raw["services"]) {
			total += integer(backend["weight"], 1)
		}
		for _, backend := range items(raw["services"]) {
			g.traefikBackend(o, tail, backend, protocol, "weights", total, map[string]bool{}, 0)
		}
	}
}

func (g *topologyGraph) traefikMiddleware(o topologyObject, parent string, ref map[string]any, kind, protocol string, seen map[string]bool, depth int) string {
	if g.budgetReached() {
		return ""
	}
	namespace := firstTopology(text(ref["namespace"]), o.ref.Namespace)
	name := text(ref["name"])
	middleware, ok := g.objects[topologyKey(kind, namespace, name)]
	if !ok {
		target := g.unknown(parent, kind, namespace, name, "")
		g.link(parent, target, "processes", protocol, "")
		return target
	}
	// Each occurrence retains its position in this configured middleware chain.
	target := g.addNode(cr.NetworkTopologyGraphNode{ID: parent + "/middleware/" + middleware.id(), Type: "processor", Label: name, Provider: "traefik", Protocol: protocol, ResolutionState: "resolved", Resource: &middleware.ref})
	edge := g.link(parent, target, "processes", protocol, "")
	if namespace != o.ref.Namespace {
		edge.ResolutionState = "unknown"
		edge.Note = "Controller allowCrossNamespace setting unverified"
	}
	g.properties(target, cr.NetworkTopologyProperty{Name: "Types", Value: strings.Join(texts(middleware.body["processorTypes"]), ", ")})
	if depth >= 8 || seen[middleware.id()] {
		g.state(target, "unsupported_fragment")
		return target
	}
	next := cloneTopologySeen(seen)
	next[middleware.id()] = true
	for _, child := range items(at(middleware.body, "spec", "chain", "middlewares")) {
		target = g.traefikMiddleware(middleware, target, child, kind, protocol, next, depth+1)
	}
	return target
}

func (g *topologyGraph) traefikBackend(o topologyObject, parent string, ref map[string]any, protocol, relation string, total int64, seen map[string]bool, depth int) {
	if g.budgetReached() {
		return
	}

	namespace := firstTopology(text(ref["namespace"]), o.ref.Namespace)
	name := text(ref["name"])
	kind := firstTopology(text(ref["kind"]), "Service")
	port := text(ref["port"])
	target := ""
	switch kind {
	case "TraefikService":
		target = g.traefikService(o, parent, namespace, name, protocol, seen, depth)
	case "Service":
		target = g.backend(parent, namespace, name, port, protocol)
	default:
		target = g.unknown(parent, kind, namespace, name, "unsupported_fragment")
	}
	edge := g.link(parent, target, relation, protocol, port)
	weight := integer(ref["weight"], 1)
	edge.Weight = &weight
	if relation == "weights" {
		edge.TotalWeight = &total
	}
	if value, ok := ref["percent"].(float64); ok {
		edge.MirrorPercent = &value
	}
	if namespace != o.ref.Namespace {
		edge.ResolutionState = "unknown"
		edge.Note = "Controller allowCrossNamespace setting unverified"
	}
}
func (g *topologyGraph) traefikService(o topologyObject, parent, namespace, name, protocol string, seen map[string]bool, depth int) string {
	if g.budgetReached() {
		return ""
	}

	item, ok := g.objects[topologyKey("TraefikService", namespace, name)]
	if !ok {
		return g.unknown(parent, "TraefikService", namespace, name, "")
	}
	target := g.objectNode(item, "backend-group", "traefik", protocol)
	if depth >= 8 || seen[item.id()] {
		g.state(target, "unsupported_fragment")
		return target
	}
	next := cloneTopologySeen(seen)
	next[item.id()] = true
	weighted := items(at(item.body, "spec", "weighted", "services"))
	total := int64(0)
	for _, backend := range weighted {
		total += integer(backend["weight"], 1)
	}
	for _, backend := range weighted {
		g.traefikBackend(item, target, backend, protocol, "weights", total, next, depth+1)
	}
	if mirror := object(at(item.body, "spec", "mirroring")); len(mirror) > 0 {
		g.traefikBackend(item, target, mirror, protocol, "forwards", 0, next, depth+1)
		for _, backend := range items(mirror["mirrors"]) {
			g.traefikBackend(item, target, backend, protocol, "mirrors", 0, next, depth+1)
		}
	}
	return target
}
func cloneTopologySeen(seen map[string]bool) map[string]bool {
	next := map[string]bool{}
	for key, value := range seen {
		next[key] = value
	}
	return next
}

func (g *topologyGraph) apisixRoute(o topologyObject) {
	entry := g.objectNode(o, "entry", "apisix", "")
	for index, raw := range items(at(o.body, "spec", "http")) {
		rule := g.rule(o, "apisix", "HTTP", index, text(raw["name"]), apisixMatches(object(raw["match"])))
		node := g.nodes[rule]
		if node.Rule != nil {
			priority := integer(raw["priority"], 0)
			node.Rule.Priority = &priority
		}
		g.nodes[rule] = node
		g.link(entry, rule, "matches", "HTTP", "")
		for _, plugin := range items(raw["plugins"]) {
			g.properties(rule, cr.NetworkTopologyProperty{Name: "Configured plugin", Value: fmt.Sprintf("%s (enabled=%v; runtime priority not inferred)", text(plugin["name"]), plugin["enable"] == true)})
		}
		backends := items(raw["backends"])
		if backend := object(raw["backend"]); len(backend) > 0 {
			backends = append(backends, backend)
		}
		total := int64(0)
		for _, backend := range backends {
			total += integer(backend["weight"], 100)
		}
		for _, backend := range backends {
			g.apisixBackend(o, rule, backend, "HTTP", total)
		}
		if len(items(at(raw, "match", "exprs"))) > 0 {
			g.state(rule, "unsupported_fragment")
			g.properties(rule, cr.NetworkTopologyProperty{Name: "Expressions", Value: "Configured expression values redacted; evaluation not simulated"})
		}
	}
	for index, raw := range items(at(o.body, "spec", "stream")) {
		protocol := strings.ToUpper(firstTopology(text(raw["protocol"]), "TCP"))
		rule := g.rule(o, "apisix", protocol, index, "stream/"+firstTopology(text(raw["name"]), fmt.Sprint(index)), [][]cr.NetworkTopologyMatch{{{Type: "ingress-port", Value: text(at(raw, "match", "ingressPort"))}}})
		g.link(entry, rule, "matches", protocol, text(at(raw, "match", "ingressPort")))
		g.apisixBackend(o, rule, object(raw["backend"]), protocol, 100)
	}
}
func apisixMatches(match map[string]any) [][]cr.NetworkTopologyMatch {
	// APISIX arrays are OR within one dimension and AND across dimensions; do not fabricate their Cartesian product.
	group := []cr.NetworkTopologyMatch{}
	for _, kind := range []string{"hosts", "paths", "methods", "remoteAddrs"} {
		values := texts(match[kind])
		if len(values) > 0 {
			group = append(group, cr.NetworkTopologyMatch{Type: kind, Operator: "oneOf", Value: strings.Join(values, " | ")})
		}
	}
	return [][]cr.NetworkTopologyMatch{group}
}
func (g *topologyGraph) apisixBackend(o topologyObject, parent string, backend map[string]any, protocol string, total int64) {
	name := text(backend["serviceName"])
	port := text(backend["servicePort"])
	target := ""
	if upstream, ok := g.objects[topologyKey("ApisixUpstream", o.ref.Namespace, name)]; ok {
		if len(items(at(upstream.body, "spec", "externalNodes"))) == 0 {
			target = g.backend(parent, o.ref.Namespace, name, port, protocol)
		}
		target = g.apisixUpstream(upstream, target, protocol)
	} else {
		target = g.backend(parent, o.ref.Namespace, name, port, protocol)
	}
	edge := g.link(parent, target, "weights", protocol, port)
	weight := integer(backend["weight"], 100)
	edge.Weight = &weight
	edge.TotalWeight = &total
}
func (g *topologyGraph) apisixUpstream(upstream topologyObject, service, protocol string) string {
	id := g.objectNode(upstream, "backend-group", "apisix", protocol)
	if g.expanded[id] {
		return id
	}
	g.expanded[id] = true
	g.properties(id, cr.NetworkTopologyProperty{Name: "Scheme", Value: text(at(upstream.body, "spec", "scheme"))}, cr.NetworkTopologyProperty{Name: "Load balancer", Value: text(at(upstream.body, "spec", "loadbalancer", "type"))})
	nodes := items(at(upstream.body, "spec", "externalNodes"))
	if len(nodes) == 0 {
		g.link(id, service, "resolves", protocol, "")
	}
	for index, raw := range nodes {
		target := g.addNode(cr.NetworkTopologyGraphNode{ID: fmt.Sprintf("%s/external/%d", id, index), Type: "external", Label: text(raw["name"]), Provider: "apisix", Protocol: protocol, ResolutionState: "external"})
		edge := g.link(id, target, "forwards", protocol, text(raw["port"]))
		weight := integer(raw["weight"], 1)
		edge.Weight = &weight
	}
	for _, subset := range items(at(upstream.body, "spec", "subsets")) {
		g.properties(id, cr.NetworkTopologyProperty{Name: "Configured subset", Value: text(subset["name"]) + " (endpoint subset resolution unverified)"})
		g.state(id, "unsupported_fragment")
	}
	return id
}
