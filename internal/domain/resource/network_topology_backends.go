package resource

import (
	"fmt"
	cr "github.com/opensoha/soha-contracts/resource"
	"strings"
)

func (g *topologyGraph) backend(parent, namespace, name, port, protocol string) string {
	service, ok := g.objects[topologyKey("Service", namespace, name)]
	if !ok {
		return g.unknown(parent, "Service", namespace, name, "")
	}
	id := g.service(service, port)
	if port != "" && !servicePortExists(service, port, protocol) {
		g.properties(id, cr.NetworkTopologyProperty{Name: "Unresolved service port", Value: port})
	}
	return id
}
func servicePortExists(service topologyObject, port, protocol string) bool {
	for _, value := range items(at(service.body, "spec", "ports")) {
		if text(value["name"]) == port || text(value["port"]) == port {
			expected := "TCP"
			if protocol == "UDP" {
				expected = "UDP"
			}
			return protocol == "" || firstTopology(text(value["protocol"]), "TCP") == expected
		}
	}
	return false
}

func (g *topologyGraph) service(service topologyObject, port string) string {
	id := g.objectNode(service, "service", "kubernetes", "")
	if g.expanded[id+":"+port] {
		return id
	}
	g.expanded[id+":"+port] = true
	if text(at(service.body, "spec", "type")) == "ExternalName" {
		target := g.addNode(cr.NetworkTopologyGraphNode{ID: id + "/external", Type: "external", Label: text(at(service.body, "spec", "externalName")), Provider: "kubernetes", ResolutionState: "external"})
		g.link(id, target, "resolves", "", port)
		return id
	}
	if port != "" && !servicePortExists(service, port, "") {
		return id
	}
	slices := []topologyObject{}
	for _, object := range g.ordered {
		if object.ref.Kind == "EndpointSlice" && object.ref.Namespace == service.ref.Namespace && text(at(object.body, "metadata", "labels", "kubernetes.io/service-name")) == service.ref.Name {
			slices = append(slices, object)
		}
	}
	ready := 0
	for _, slice := range slices {
		ready += g.endpointSlice(service, slice, id, port)
	}
	if g.completeSource("kubernetes.endpointslices", service.ref.Namespace) && ready == 0 {
		g.state(id, "no_ready_endpoints")
	} else if ready > 0 {
		g.state(id, "resolved")
	}
	if len(slices) == 0 && !g.completeSource("kubernetes.endpointslices", service.ref.Namespace) {
		g.state(id, "unknown")
		g.selectorFallback(service, id)
	}
	return id
}

func (g *topologyGraph) endpointSlice(service, slice topologyObject, serviceID, port string) int {
	ready := 0
	if !endpointPortMatches(service, slice, port) {
		return 0
	}
	groupID := g.objectNode(slice, "endpoint-group", "kubernetes", "")
	g.link(serviceID, groupID, "resolves", "", port)
	for index, endpoint := range items(slice.body["endpoints"]) {
		conditions := object(endpoint["conditions"])
		eligible := conditions["ready"] != false
		if eligible {
			ready++
		}
		podRef := object(endpoint["targetRef"])
		state := "resolved"
		if !eligible {
			state = "no_ready_endpoints"
		}
		if text(podRef["kind"]) == "Pod" {
			g.endpointPod(slice, groupID, podRef, state, index)
			continue
		}
		target := g.addNode(cr.NetworkTopologyGraphNode{ID: fmt.Sprintf("%s/endpoint/%d", slice.id(), index), Type: "external", Label: strings.Join(texts(endpoint["addresses"]), ", "), Provider: "kubernetes", ResolutionState: "external"})
		edge := g.link(groupID, target, "resolves", "", port)
		edge.ResolutionState = state
		edge.Note = fmt.Sprintf("ready=%v, serving=%v, terminating=%v", eligible, conditions["serving"] != false, conditions["terminating"] == true)
	}
	if ready == 0 {
		g.state(groupID, "no_ready_endpoints")
	}
	return ready
}
func endpointPortMatches(service, slice topologyObject, port string) bool {
	if port == "" {
		return true
	}
	name := ""
	protocol := "TCP"
	for _, servicePort := range items(at(service.body, "spec", "ports")) {
		if text(servicePort["name"]) == port || text(servicePort["port"]) == port {
			name = text(servicePort["name"])
			protocol = firstTopology(text(servicePort["protocol"]), "TCP")
		}
	}
	for _, endpointPort := range items(slice.body["ports"]) {
		if text(endpointPort["name"]) == name && firstTopology(text(endpointPort["protocol"]), "TCP") == protocol && endpointPort["port"] != nil {
			return true
		}
	}
	return false
}
func (g *topologyGraph) endpointPod(slice topologyObject, group string, ref map[string]any, state string, index int) {
	namespace := firstTopology(text(ref["namespace"]), slice.ref.Namespace)
	pod, ok := g.objects[topologyKey("Pod", namespace, text(ref["name"]))]
	if !ok {
		g.link(group, g.unknown(fmt.Sprintf("%s/%d", group, index), "Pod", namespace, text(ref["name"]), ""), "resolves", "", "")
		return
	}
	if uid := text(ref["uid"]); uid != "" && uid != pod.ref.UID {
		g.link(group, g.unknown(group, "Pod", namespace, pod.ref.Name, "unresolved"), "resolves", "", "")
		return
	}
	id := g.objectNode(pod, "pod", "kubernetes", "")
	edge := g.link(group, id, "resolves", "", "")
	edge.ResolutionState = state
}
func (g *topologyGraph) selectorFallback(service topologyObject, id string) {
	selector := object(at(service.body, "spec", "selector"))
	if len(selector) == 0 {
		return
	}
	for _, pod := range g.ordered {
		if pod.ref.Kind != "Pod" || pod.ref.Namespace != service.ref.Namespace {
			continue
		}
		labels := object(at(pod.body, "metadata", "labels"))
		matches := true
		for key, value := range selector {
			if labels[key] != value {
				matches = false
			}
		}
		if matches {
			target := g.objectNode(pod, "pod", "kubernetes", "")
			edge := g.link(id, target, "selects", "", "")
			edge.ResolutionState = "unknown"
			edge.Note = "Selector match only; endpoint readiness was not observed"
		}
	}
}
