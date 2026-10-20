package resource

import (
	cr "github.com/opensoha/soha-contracts/resource"
	"strings"
)

func (g *topologyGraph) ingress(o topologyObject) {
	provider := g.classProvider("IngressClass", text(at(o.body, "spec", "ingressClassName")), "kubernetes")
	entry := g.objectNode(o, "entry", provider, "HTTP")
	if len(items(at(o.body, "spec", "tls"))) > 0 {
		g.properties(entry, cr.NetworkTopologyProperty{Name: "TLS", Value: "Configured; certificate and controller execution unverified"})
	}
	index := 0
	add := func(host string, path map[string]any, backend map[string]any) {
		matches := [][]cr.NetworkTopologyMatch{{{Type: "host", Value: host}, {Type: "path", Operator: text(path["pathType"]), Value: text(path["path"])}}}
		rule := g.rule(o, provider, "HTTP", index, "", matches)
		index++
		g.link(entry, rule, "matches", "HTTP", "")
		service := object(backend["service"])
		if len(service) == 0 {
			g.state(rule, "unsupported_fragment")
			g.properties(rule, cr.NetworkTopologyProperty{Name: "Backend", Value: "Custom resource backend requires its provider resolver"})
			return
		}
		port := object(service["port"])
		target := g.backend(rule, o.ref.Namespace, text(service["name"]), firstTopology(text(port["name"]), text(port["number"])), "HTTP")
		g.link(rule, target, "forwards", "HTTP", firstTopology(text(port["name"]), text(port["number"])))
	}
	if backend := object(at(o.body, "spec", "defaultBackend")); len(backend) > 0 {
		add("", map[string]any{"path": "/", "pathType": "Default"}, backend)
	}
	for _, rule := range items(at(o.body, "spec", "rules")) {
		for _, path := range items(at(rule, "http", "paths")) {
			add(text(rule["host"]), path, object(path["backend"]))
		}
	}
	for _, tls := range items(at(o.body, "spec", "tls")) {
		g.properties(entry, cr.NetworkTopologyProperty{Name: "TLS hosts", Value: strings.Join(texts(tls["hosts"]), ", ")})
	}
}

func (g *topologyGraph) classProvider(kind, name, fallback string) string {
	class, ok := g.objects[topologyKey(kind, "", name)]
	if !ok {
		return fallback
	}
	controller := text(at(class.body, "spec", "controller"))
	if kind == "GatewayClass" {
		controller = text(at(class.body, "spec", "controllerName"))
	}
	switch controller {
	case "traefik.io/ingress-controller", "traefik.io/gateway-controller":
		return "traefik"
	case "apisix.apache.org/ingress-controller", "apisix.apache.org/apisix-ingress-controller":
		return "apisix"
	}
	return fallback
}

func (g *topologyGraph) gatewayRoute(o topologyObject) {
	protocol := gatewayRouteProtocol(o.ref.Kind)
	entry := g.objectNode(o, "entry", "gateway", protocol)
	g.properties(entry, cr.NetworkTopologyProperty{Name: "Hostnames", Value: strings.Join(texts(at(o.body, "spec", "hostnames")), ", ")})
	for _, parent := range items(at(o.body, "spec", "parentRefs")) {
		g.gatewayParent(o, entry, parent, protocol)
	}
	for index, raw := range items(at(o.body, "spec", "rules")) {
		groups := gatewayMatchGroups(raw)
		if o.ref.Kind == "TLSRoute" {
			for _, host := range texts(at(o.body, "spec", "hostnames")) {
				groups = append(groups, []cr.NetworkTopologyMatch{{Type: "sni", Value: host}})
			}
		}
		rule := g.rule(o, "gateway", protocol, index, text(raw["name"]), groups)
		if o.ref.Kind == "TLSRoute" && rule != "" {
			node := g.nodes[rule]
			node.Rule.TLSMode = "Passthrough"
			g.nodes[rule] = node
		}
		g.link(entry, rule, "matches", protocol, "")
		total := int64(0)
		for _, backend := range items(raw["backendRefs"]) {
			total += integer(backend["weight"], 1)
		}
		for _, backend := range items(raw["backendRefs"]) {
			edge := g.gatewayBackend(o, rule, backend, "weights", protocol)
			if total >= 0 {
				edge.TotalWeight = &total
			}
		}
		for _, filter := range items(raw["filters"]) {
			g.gatewayFilter(o, rule, filter, protocol)
		}
	}
	g.gatewayConditions(o, entry)
}

func gatewayRouteProtocol(kind string) string {
	switch kind {
	case "GRPCRoute":
		return "GRPC"
	case "TCPRoute":
		return "TCP"
	case "TLSRoute":
		return "TLS"
	case "UDPRoute":
		return "UDP"
	default:
		return "HTTP"
	}
}

func gatewayMatchGroups(rule map[string]any) [][]cr.NetworkTopologyMatch {
	result := [][]cr.NetworkTopologyMatch{}
	for _, match := range items(rule["matches"]) {
		group := []cr.NetworkTopologyMatch{}
		if path := object(match["path"]); len(path) > 0 {
			group = append(group, cr.NetworkTopologyMatch{Type: "path", Operator: text(path["type"]), Value: text(path["value"])})
		}
		if method, ok := match["method"].(string); ok {
			group = append(group, cr.NetworkTopologyMatch{Type: "method", Value: method})
		} else if method := object(match["method"]); len(method) > 0 {
			group = append(group, cr.NetworkTopologyMatch{Type: "grpc-method", Operator: text(method["type"]), Name: text(method["service"]), Value: text(method["method"])})
		}
		for _, kind := range []string{"headers", "queryParams"} {
			for _, value := range items(match[kind]) {
				group = append(group, cr.NetworkTopologyMatch{Type: kind, Name: text(value["name"]), Operator: text(value["type"]), Value: "[redacted]"})
			}
		}
		result = append(result, group)
	}
	return result
}

func (g *topologyGraph) gatewayBackend(o topologyObject, rule string, backend map[string]any, relation, protocol string) *cr.NetworkTopologyGraphEdge {
	ns := firstTopology(text(backend["namespace"]), o.ref.Namespace)
	kind := firstTopology(text(backend["kind"]), "Service")
	name := text(backend["name"])
	port := text(backend["port"])
	if kind != "Service" || text(backend["group"]) != "" {
		return g.link(rule, g.unknown(rule, kind, ns, name, "unsupported_fragment"), relation, protocol, port)
	}
	target := ""
	if ns != o.ref.Namespace && !g.grantsBackend(o, ns, name) {
		state := "unknown"
		if g.completeSource("gateway.referencegrants", ns) {
			state = "unresolved"
		}
		target = g.unknown(rule, kind, ns, "Cross-namespace reference requires ReferenceGrant", state)
	} else {
		target = g.backend(rule, ns, name, port, protocol)
	}
	edge := g.link(rule, target, relation, protocol, port)
	if service, ok := g.objects[topologyKey("Service", ns, name)]; ok && target == service.id() && port != "" && !servicePortExists(service, port, protocol) {
		edge.ResolutionState = "unresolved"
		edge.Note = "Service port/protocol does not match the route backend"
	}
	weight := integer(backend["weight"], 1)
	if weight >= 0 {
		edge.Weight = &weight
	} else {
		edge.ResolutionState = "unsupported_fragment"
	}
	return edge
}

func (g *topologyGraph) grantsBackend(o topologyObject, namespace, name string) bool {
	for _, grant := range g.ordered {
		if grant.ref.Kind != "ReferenceGrant" || grant.ref.Namespace != namespace {
			continue
		}
		from := false
		to := false
		for _, item := range items(at(grant.body, "spec", "from")) {
			if text(item["namespace"]) == o.ref.Namespace && text(item["kind"]) == o.ref.Kind && text(item["group"]) == "gateway.networking.k8s.io" {
				from = true
			}
		}
		for _, item := range items(at(grant.body, "spec", "to")) {
			if text(item["kind"]) == "Service" && text(item["group"]) == "" && (text(item["name"]) == "" || text(item["name"]) == name) {
				to = true
			}
		}
		if from && to {
			return true
		}
	}
	return false
}
func (g *topologyGraph) completeSource(source, namespace string) bool {
	for _, coverage := range g.view.Coverage {
		if coverage.SourceID == source && (coverage.Namespace == "" || coverage.Namespace == namespace) && coverage.State == "available" && !coverage.Truncated {
			return true
		}
	}
	return false
}

func (g *topologyGraph) gatewayParent(o topologyObject, entry string, parent map[string]any, protocol string) {
	ns := firstTopology(text(parent["namespace"]), o.ref.Namespace)
	name := text(parent["name"])
	if (text(parent["kind"]) != "" && text(parent["kind"]) != "Gateway") || (text(parent["group"]) != "" && text(parent["group"]) != "gateway.networking.k8s.io") {
		g.state(entry, "unsupported_fragment")
		return
	}
	gateway, ok := g.objects[topologyKey("Gateway", ns, name)]
	if !ok {
		g.link(g.unknown(entry, "Gateway", ns, name, ""), entry, "matches", protocol, "")
		return
	}
	parentID := g.objectNode(gateway, "entry", g.classProvider("GatewayClass", text(at(gateway.body, "spec", "gatewayClassName")), "gateway"), "")
	if !g.expanded["listeners:"+parentID] {
		g.expanded["listeners:"+parentID] = true
		for _, listener := range items(at(gateway.body, "spec", "listeners")) {
			g.properties(parentID, cr.NetworkTopologyProperty{Name: "Listener " + text(listener["name"]), Value: strings.Join([]string{text(listener["protocol"]), text(listener["port"]), text(listener["hostname"]), text(at(listener, "tls", "mode"))}, " · ")})
		}
	}
	section := text(parent["sectionName"])
	port := text(parent["port"])
	state := "unresolved"
	ports := []string{}
	for _, listener := range items(at(gateway.body, "spec", "listeners")) {
		if (section == "" || text(listener["name"]) == section) && (port == "" || text(listener["port"]) == port) {
			if gatewayListenerAllows(o, ns, listener) && gatewayListenerProtocolAllows(o, listener) {
				state = "resolved"
				ports = append(ports, text(listener["port"]))
			} else if state != "resolved" {
				state = "unknown"
			}
		}
	}
	edge := g.link(parentID, entry, "matches", protocol, firstTopology(port, strings.Join(ports, ",")))
	edge.ResolutionState = state
	edge.Note = "Listener " + section + "; attachment must also be accepted by the controller"
}

func gatewayListenerProtocolAllows(o topologyObject, listener map[string]any) bool {
	protocol := text(listener["protocol"])
	switch o.ref.Kind {
	case "TCPRoute":
		return protocol == "TCP"
	case "UDPRoute":
		return protocol == "UDP"
	case "TLSRoute":
		if protocol != "TLS" || text(at(listener, "tls", "mode")) != "Passthrough" {
			return false
		}
		listenerHost := text(listener["hostname"])
		for _, host := range texts(at(o.body, "spec", "hostnames")) {
			if listenerHost == "" || gatewayHostMatches(host, listenerHost) || gatewayHostMatches(listenerHost, host) {
				return true
			}
		}
		return false
	default:
		return protocol == "HTTP" || protocol == "HTTPS"
	}
}

func gatewayHostMatches(host, pattern string) bool {
	return host == pattern || (strings.HasPrefix(pattern, "*.") && strings.HasSuffix(host, pattern[1:]) && len(host) > len(pattern)-1)
}
func gatewayListenerAllows(o topologyObject, namespace string, listener map[string]any) bool {
	from := text(at(listener, "allowedRoutes", "namespaces", "from"))
	if from == "Selector" {
		return false
	}
	if o.ref.Namespace != namespace && from != "All" {
		return false
	}
	kinds := items(at(listener, "allowedRoutes", "kinds"))
	if len(kinds) == 0 {
		return true
	}
	for _, kind := range kinds {
		if text(kind["kind"]) == o.ref.Kind && (text(kind["group"]) == "" || text(kind["group"]) == "gateway.networking.k8s.io") {
			return true
		}
	}
	return false
}
func (g *topologyGraph) gatewayFilter(o topologyObject, rule string, filter map[string]any, protocol string) {
	kind := text(filter["type"])
	if kind == "RequestMirror" {
		mirror := object(filter["requestMirror"])
		edge := g.gatewayBackend(o, rule, object(mirror["backendRef"]), "mirrors", protocol)
		if value, ok := mirror["percent"].(float64); ok && value >= 0 && value <= 100 {
			edge.MirrorPercent = &value
		}
		return
	}
	g.properties(rule, cr.NetworkTopologyProperty{Name: "Configured filter", Value: kind + " (execution order not inferred)"})
	if kind != "RequestHeaderModifier" && kind != "ResponseHeaderModifier" {
		g.state(rule, "unsupported_fragment")
	}
}
func (g *topologyGraph) gatewayConditions(o topologyObject, id string) {
	node := g.nodes[id]
	for _, parent := range items(at(o.body, "status", "parents")) {
		for _, condition := range items(parent["conditions"]) {
			generation := integer(condition["observedGeneration"], 0)
			node.Conditions = append(node.Conditions, cr.NetworkTopologyCondition{Type: text(condition["type"]), Status: text(condition["status"]), Reason: text(condition["reason"]), ObservedGeneration: &generation, LastTransitionTime: text(condition["lastTransitionTime"])})
			if generation != o.generation || text(condition["status"]) != "True" {
				node.ResolutionState = "unresolved"
			}
		}
	}
	if len(node.Conditions) == 0 {
		node.ResolutionState = "unknown"
	}
	g.nodes[id] = node
}
