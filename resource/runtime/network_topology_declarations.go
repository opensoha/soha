package runtime

import (
	"encoding/json"
	"fmt"
	"strings"

	cr "github.com/opensoha/soha-contracts/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

// declarations adds only structured Kubernetes references. It never interprets
// arbitrary provider spec, certificate bytes, or policy as live connectivity.
func (g *topologyGraph) declarations(prioritized string) {
	for _, o := range g.ordered {
		if g.budgetReached() || g.ctx.Err() != nil {
			return
		}
		if o.id() != prioritized {
			g.objectDeclarations(o)
		}
	}
}

func (g *topologyGraph) objectDeclarations(o topologyObject) {
	switch o.ref.Kind {
	case "Ingress":
		g.declaredRef(o, "networking.k8s.io/v1", "IngressClass", "", text(at(o.body, "spec", "ingressClassName")), "uses-class", "spec.ingressClassName", "")
		for i, tls := range items(at(o.body, "spec", "tls")) {
			g.declaredRef(o, "v1", "Secret", o.ref.Namespace, text(tls["secretName"]), "uses-secret", fmt.Sprintf("spec.tls[%d].secretName", i), "")
		}
	case "Gateway":
		g.declaredRef(o, "gateway.networking.k8s.io/v1", "GatewayClass", "", text(at(o.body, "spec", "gatewayClassName")), "uses-class", "spec.gatewayClassName", "")
		g.gatewayCertificates(o)
	case "IngressClass":
		g.classParameters(o, object(at(o.body, "spec", "parameters")), "spec.parameters")
	case "GatewayClass":
		g.classParameters(o, object(at(o.body, "spec", "parametersRef")), "spec.parametersRef")
	case "BackendTLSPolicy":
		g.backendTLSPolicy(o)
	case "NetworkPolicy":
		g.networkPolicy(o)
	}
}

func topologySourceFor(kind, api string) (cr.NetworkTopologySourceDefinition, bool) {
	for _, d := range cr.NetworkTopologySources() {
		if d.Kind == kind && topologyDefinitionAPI(d, api) {
			return d, true
		}
	}
	return cr.NetworkTopologySourceDefinition{}, false
}

func (g *topologyGraph) declaredRef(o topologyObject, api, kind, namespace, name, relation, path, section string) *cr.NetworkTopologyGraphEdge {
	if name == "" {
		return &cr.NetworkTopologyGraphEdge{}
	}
	source := g.objectNode(o, "backend-group", topologyProvider(o.source), "")
	ref := cr.ResourceRef{ClusterID: g.view.ClusterID, APIVersion: api, Kind: kind, Namespace: namespace, Name: name, ScopeMode: cr.ResourceScopeModeNamespace}
	if namespace == "" {
		ref.ScopeMode = cr.ResourceScopeModeCluster
	}
	target := topologyObject{ref: ref}
	state := "unknown"
	if api == "" {
		state = "unsupported_fragment"
	}
	if d, ok := topologySourceFor(kind, api); ok {
		if g.completeSource(d.ID, namespace) {
			state = "not_found"
		}
		if actual, ok := g.objects[topologyKey(kind, namespace, name)]; ok && actual.ref.APIVersion == api {
			target = actual
			state = "resolved"
		}
	}
	// Secret existence is never inferred from an ordinary object projection.
	if kind == "Secret" && api == "v1" {
		state = "unresolved"
	}
	id := ""
	if state == "unsupported_fragment" {
		id = g.unknown(source, kind, namespace, name, state)
	} else if state == "unknown" || kind == "Secret" && api == "v1" {
		// The public topology has no per-node product permission filter. Keep a
		// declaration identity private for Graph's later authorization pass only.
		id = g.unknown(source, kind, namespace, name, state)
		g.hiddenRefs[id] = target.ref
	} else {
		id = g.objectNode(target, "backend-group", topologyProvider(o.source), "")
		if state != "resolved" {
			g.state(id, state)
		}
	}
	edge := g.link(source, id, relation, "", "")
	edge.SourceRef = &o.ref
	edge.FieldPath = path
	edge.SectionName = section
	edge.ResolutionState = state
	return edge
}
func topologyProvider(source string) string {
	d, _ := cr.LookupNetworkTopologySource(source)
	return firstTopology(d.Provider, "kubernetes")
}

func (g *topologyGraph) classParameters(o topologyObject, ref map[string]any, path string) {
	kind, name := text(ref["kind"]), text(ref["name"])
	group := firstTopology(text(ref["group"]), text(ref["apiGroup"]))
	if kind == "" || name == "" {
		return
	}
	namespace := text(ref["namespace"])
	if text(ref["scope"]) == "Namespace" && namespace == "" {
		return
	}
	api := topologyReferenceAPI(group, kind)
	g.declaredRef(o, api, kind, namespace, name, "parameters", path, "")
}
func topologyReferenceAPI(group, kind string) string {
	for _, d := range cr.NetworkTopologySources() {
		if d.Group == group && d.Kind == kind {
			return strings.TrimPrefix(d.Group+"/"+d.Version, "/")
		}
	}
	if group == "" && kind == "Secret" {
		return "v1"
	}
	// No version is guessed for an unsupported parameter kind.
	return ""
}
func (g *topologyGraph) gatewayCertificates(o topologyObject) {
	for i, l := range items(at(o.body, "spec", "listeners")) {
		for j, r := range items(at(l, "tls", "certificateRefs")) {
			group, kind := text(r["group"]), firstTopology(text(r["kind"]), "Secret")
			ns := firstTopology(text(r["namespace"]), o.ref.Namespace)
			authorized := ns == o.ref.Namespace || g.referenceAuthorized(o, group, kind, ns, text(r["name"]))
			edge := g.declaredRef(o, topologyReferenceAPI(group, kind), kind, ns, text(r["name"]), "uses-secret", fmt.Sprintf("spec.listeners[%d].tls.certificateRefs[%d]", i, j), text(l["name"]))
			edge.Protocol = text(l["protocol"])
			edge.Port = text(l["port"])
			if !authorized {
				edge.ResolutionState = g.grantResolution(ns)
			}
			if group != "" || kind != "Secret" {
				edge.ResolutionState = "unsupported_fragment"
			}
			edge.Note = "Certificate reference only; TLS activation is unverified"
		}
	}
}
func (g *topologyGraph) backendTLSPolicy(o topologyObject) {
	refs := items(at(o.body, "spec", "targetRefs"))
	path := "spec.targetRefs"
	if len(refs) == 0 {
		if ref := object(at(o.body, "spec", "targetRef")); len(ref) > 0 {
			refs = append(refs, ref)
			path = "spec.targetRef"
		}
	}
	for i, r := range refs {
		targetPath := path
		if path == "spec.targetRefs" {
			targetPath = fmt.Sprintf("%s[%d]", path, i)
		}
		kind, name, section := text(r["kind"]), text(r["name"]), text(r["sectionName"])
		api := topologyReferenceAPI(text(r["group"]), kind)
		edge := g.declaredRef(o, api, kind, o.ref.Namespace, name, "targets", targetPath, section)
		if api == "v1" && kind == "Service" && section != "" && edge.ResolutionState == "resolved" {
			service := g.objects[topologyKey(kind, o.ref.Namespace, name)]
			resolveBackendTLSServiceSection(edge, service, section)
		}
	}
	for i, r := range items(at(o.body, "spec", "validation", "caCertificateRefs")) {
		group, kind := text(r["group"]), firstTopology(text(r["kind"]), "ConfigMap")
		edge := g.declaredRef(o, topologyReferenceAPI(group, kind), kind, o.ref.Namespace, text(r["name"]), "uses-config", fmt.Sprintf("spec.validation.caCertificateRefs[%d]", i), "")
		if group != "" || kind != "ConfigMap" {
			edge.ResolutionState = "unsupported_fragment"
		}
		edge.Note = "CA reference only; trust and certificate validity are unverified"
	}
}

func resolveBackendTLSServiceSection(edge *cr.NetworkTopologyGraphEdge, service topologyObject, section string) {
	for _, port := range items(at(service.body, "spec", "ports")) {
		if text(port["name"]) == section {
			edge.Port = text(port["port"])
			edge.Protocol = firstTopology(text(port["protocol"]), "TCP")
			return
		}
	}
	edge.ResolutionState = "unresolved"
	edge.Note = "Declared Service port name was not observed"
}

// A grant is an OR of from entries AND an OR of to entries, scoped to
// the target namespace. Partial/unavailable grant coverage never confirms it.
func (g *topologyGraph) referenceAuthorized(o topologyObject, group, kind, namespace, name string) bool {
	if namespace == o.ref.Namespace {
		return true
	}
	if !g.completeSource("gateway.referencegrants", namespace) {
		return false
	}
	fromGroup := strings.Split(o.ref.APIVersion, "/")[0]
	for _, grant := range g.ordered {
		if g.ctx.Err() != nil || g.budgetReached() {
			return false
		}
		if grant.source != "gateway.referencegrants" || grant.ref.Namespace != namespace {
			continue
		}
		from, to := false, false
		for _, r := range items(at(grant.body, "spec", "from")) {
			if text(r["namespace"]) == o.ref.Namespace && text(r["kind"]) == o.ref.Kind && text(r["group"]) == fromGroup {
				from = true
			}
		}
		for _, r := range items(at(grant.body, "spec", "to")) {
			if text(r["group"]) == group && text(r["kind"]) == kind && (text(r["name"]) == "" || text(r["name"]) == name) {
				to = true
			}
		}
		if from && to {
			edge := g.link(g.objectNode(grant, "backend-group", "gateway", ""), g.objectNode(o, "entry", "gateway", ""), "authorizes-reference", "", "")
			edge.SourceRef = &grant.ref
			edge.FieldPath = "spec.from+spec.to"
			edge.Note = "Authorizes a declared reference; does not carry traffic"
			return true
		}
	}
	return false
}
func (g *topologyGraph) grantResolution(namespace string) string {
	if g.completeSource("gateway.referencegrants", namespace) {
		return "unresolved"
	}
	return "unknown"
}

func topologySelectorMatches(selector, values map[string]any) bool {
	if selector == nil {
		return false
	}
	raw, err := json.Marshal(selector)
	if err != nil {
		return false
	}
	var s metav1.LabelSelector
	if json.Unmarshal(raw, &s) != nil {
		return false
	}
	compiled, err := metav1.LabelSelectorAsSelector(&s)
	if err != nil {
		return false
	}
	set := labels.Set{}
	for k, v := range values {
		if value, ok := v.(string); ok {
			set[k] = value
		}
	}
	return compiled.Matches(set)
}

func (g *topologyGraph) networkPolicy(o topologyObject) {
	id := g.objectNode(o, "backend-group", "kubernetes", "")
	selected := object(at(o.body, "spec", "podSelector"))
	for _, pod := range g.ordered {
		if g.budgetReached() || g.ctx.Err() != nil {
			return
		}
		if pod.source == "kubernetes.pods" && pod.ref.Namespace == o.ref.Namespace && topologySelectorMatches(selected, object(at(pod.body, "metadata", "labels"))) {
			edge := g.link(id, g.objectNode(pod, "pod", "kubernetes", ""), "selects", "", "")
			edge.FieldPath = "spec.podSelector"
			edge.Note = "Selected Pod only; policy enforcement and connectivity are unverified"
		}
	}
	for _, direction := range []string{"ingress", "egress"} {
		peerField := "from"
		if direction == "egress" {
			peerField = "to"
		}
		for i, rule := range items(at(o.body, "spec", direction)) {
			if len(items(rule[peerField])) == 0 {
				g.properties(id, cr.NetworkTopologyProperty{Name: fmt.Sprintf("spec.%s[%d].%s", direction, i, peerField), Value: "All peers declared; connectivity is unverified"})
			}
			for j, peer := range items(rule[peerField]) {
				path := fmt.Sprintf("spec.%s[%d].%s[%d]", direction, i, peerField, j)
				if block := object(peer["ipBlock"]); len(block) > 0 {
					g.properties(id, cr.NetworkTopologyProperty{Name: path + ".ipBlock", Value: text(block["cidr"]) + " except " + strings.Join(texts(block["except"]), ",") + "; declared CIDRs only"})
					continue
				}
				g.policyPeers(o, id, peer, path)
			}
		}
	}
}
func (g *topologyGraph) policyPeers(o topologyObject, id string, peer map[string]any, path string) {
	ps, hasPod := peer["podSelector"]
	ns, hasNS := peer["namespaceSelector"]
	if !hasPod && !hasNS {
		g.properties(id, cr.NetworkTopologyProperty{Name: path, Value: "All peers declared; connectivity is unverified"})
		return
	}
	for _, pod := range g.ordered {
		if g.budgetReached() || g.ctx.Err() != nil {
			return
		}
		if pod.source != "kubernetes.pods" {
			continue
		}
		if hasNS {
			namespace, ok := g.objects[topologyKey("Namespace", "", pod.ref.Namespace)]
			if !ok || !g.completeSource("kubernetes.namespaces", "") || !topologySelectorMatches(object(ns), object(at(namespace.body, "metadata", "labels"))) {
				continue
			}
		} else if pod.ref.Namespace != o.ref.Namespace {
			continue
		}
		if hasPod && !topologySelectorMatches(object(ps), object(at(pod.body, "metadata", "labels"))) {
			continue
		}
		edge := g.link(id, g.objectNode(pod, "pod", "kubernetes", ""), "policy-peer", "", "")
		edge.FieldPath = path
		edge.Note = "Peer selector match (namespace AND pod within this entry; entries OR); no live connectivity inferred"
	}
}
