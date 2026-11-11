package runtime

import (
	"fmt"
	cr "github.com/opensoha/soha-contracts/resource"
	"k8s.io/apimachinery/pkg/labels"
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
		if g.ctx.Err() != nil {
			break
		}
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
	g.serviceSelector(service, id)
	if len(slices) == 0 && !g.completeSource("kubernetes.endpointslices", service.ref.Namespace) {
		g.state(id, "unknown")
	}
	return id
}

func (g *topologyGraph) endpointSlice(service, slice topologyObject, serviceID, port string) int {
	ready := 0
	if !endpointPortMatches(service, slice, port) {
		return 0
	}
	groupID := g.objectNode(slice, "endpoint-group", "kubernetes", "")
	edge := g.link(serviceID, groupID, "resolves", "", port)
	edge.SourceRef = &slice.ref
	edge.FieldPath = "metadata.labels[kubernetes.io/service-name]"
	edge.Note = "Explicit service-name protocol label"
	g.expanded["slice:"+slice.id()] = true
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
		if text(podRef["kind"]) == "Pod" && (text(podRef["apiVersion"]) == "" || text(podRef["apiVersion"]) == "v1") {
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
		g.unresolvedEndpoint(slice, group, ref, namespace, index, g.referenceState("Pod", namespace))
		return
	}
	if uid := text(ref["uid"]); uid == "" || uid != pod.ref.UID {
		g.unresolvedEndpoint(slice, group, ref, namespace, index, "unresolved")
		return
	}
	id := g.objectNode(pod, "pod", "kubernetes", "")
	ports := items(slice.body["ports"])
	if len(ports) == 0 {
		ports = []map[string]any{{}}
	}
	for _, port := range ports {
		edge := g.link(group, id, "resolves", firstTopology(text(port["protocol"]), "TCP"), text(port["port"]))
		edge.ResolutionState = state
		edge.SectionName = text(port["name"])
		edge.FieldPath = fmt.Sprintf("endpoints[%d].targetRef", index)
		edge.Note = "EndpointSlice targetRef with matching UID; readiness belongs to this endpoint"
	}
}
func (g *topologyGraph) serviceSelector(service topologyObject, id string) {
	selector := servicePodSelector(object(at(service.body, "spec", "selector")))
	if selector == nil {
		return
	}
	for _, pod := range g.ordered {
		if g.ctx.Err() != nil || g.budgetReached() {
			return
		}
		if pod.ref.Kind != "Pod" || pod.ref.Namespace != service.ref.Namespace {
			continue
		}
		podLabels := labels.Set{}
		for key, value := range object(at(pod.body, "metadata", "labels")) {
			if label, ok := value.(string); ok {
				podLabels[key] = label
			}
		}
		if selector.Matches(podLabels) {
			target := g.objectNode(pod, "pod", "kubernetes", "")
			edge := g.link(id, target, "selects", "", "")
			edge.ResolutionState = "resolved"
			edge.Note = "Selector match only; does not establish endpoint readiness or traffic"
			edge.FieldPath = "spec.selector"
		}
	}
}

func (g *topologyGraph) unresolvedEndpoint(slice topologyObject, group string, ref map[string]any, namespace string, index int, state string) {
	target := g.unknown(fmt.Sprintf("%s/%d", group, index), "Pod", namespace, text(ref["name"]), state)
	if hidden, ok := g.hiddenRefs[target]; ok {
		hidden.UID = text(ref["uid"])
		g.hiddenRefs[target] = hidden
	}
	if n, ok := g.nodes[target]; ok && n.Resource != nil {
		n.Resource.UID = text(ref["uid"])
		g.nodes[target] = n
	}
	edge := g.link(group, target, "resolves", "", "")
	edge.ResolutionState = state
	edge.SourceRef = &slice.ref
	edge.FieldPath = fmt.Sprintf("endpoints[%d].targetRef", index)
	edge.Note = "Declared targetRef not confirmed by matching Pod UID"
}
