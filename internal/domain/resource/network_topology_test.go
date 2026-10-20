package resource

import (
	"encoding/json"
	"strings"
	"testing"

	cr "github.com/opensoha/soha-contracts/resource"
)

func topologyFixture(source, namespace, name, body string) cr.NetworkTopologySourceResult {
	definition, _ := cr.LookupNetworkTopologySource(source)
	var raw map[string]any
	_ = json.Unmarshal([]byte(body), &raw)
	metadata, _ := raw["metadata"].(map[string]any)
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadata["name"] = name
	metadata["namespace"] = namespace
	metadata["uid"] = name + "-uid"
	metadata["resourceVersion"] = "1"
	raw["metadata"] = metadata
	projected, err := cr.ProjectNetworkTopologyObject(source, raw)
	if err != nil {
		panic(err)
	}
	content, _ := json.Marshal(projected)
	return cr.NetworkTopologySourceResult{Coverage: cr.NetworkTopologyCoverage{SourceID: source, Provider: definition.Provider, Namespace: namespace, State: "available"}, Objects: []cr.NetworkTopologySourceObject{{Resource: cr.ResourceRef{ClusterID: "c", APIVersion: definition.Group + "/" + definition.Version, Kind: definition.Kind, Namespace: namespace, Name: name, UID: name + "-uid"}, Content: string(content)}}}
}
func fixtureView(sources ...cr.NetworkTopologySourceResult) cr.NetworkTopologyView {
	return BuildNetworkTopology("c", "team", cr.NetworkTopologySnapshot{Sources: sources})
}
func TestTopologyKeepsIngressRuleBackendPortsAndEndpointEvidence(t *testing.T) {
	view := fixtureView(
		topologyFixture("kubernetes.ingresses", "team", "entry", `{"spec":{"rules":[{"host":"a.test","http":{"paths":[{"path":"/a","pathType":"Prefix","backend":{"service":{"name":"web","port":{"number":80}}}}]}},{"host":"b.test","http":{"paths":[{"path":"/b","backend":{"service":{"name":"web","port":{"number":81}}}}]}}]}}`),
		topologyFixture("kubernetes.services", "team", "web", `{"spec":{"ports":[{"name":"http","port":80,"targetPort":8080},{"name":"other","port":81,"targetPort":8081}],"selector":{"app":"web"}}}`),
		topologyFixture("kubernetes.endpointslices", "team", "web-1", `{"ports":[{"name":"http","port":8080}],"endpoints":[{"addresses":["10.1.1.1"],"targetRef":{"kind":"Pod","name":"web-1","uid":"web-1-uid"},"conditions":{"serving":true}}]}`),
		topologyFixture("kubernetes.pods", "team", "web-1", `{"metadata":{"labels":{"app":"web"}},"status":{"phase":"Running"}}`),
	)
	rules := map[string]bool{}
	ports := map[string]bool{}
	for _, node := range view.Nodes {
		if node.Rule != nil {
			group := node.Rule.MatchGroups[0]
			rules[group[0].Value+group[1].Value] = true
		}
	}
	for _, edge := range view.Edges {
		if edge.Relation == "forwards" {
			ports[edge.Port] = true
		}
	}
	if len(rules) != 2 || !rules["a.test/a"] || !rules["b.test/b"] || !ports["80"] || !ports["81"] {
		t.Fatalf("flattened routes: %+v %+v", rules, ports)
	}
	// EndpointSlice without a service label is not proof of a Service backend.
	for _, edge := range view.Edges {
		if edge.Relation == "selects" {
			t.Fatal("selector fallback fabricated despite complete EndpointSlice coverage")
		}
	}
}
func TestTopologyPreservesZeroWeightMirrorAndCycles(t *testing.T) {
	view := fixtureView(
		topologyFixture("traefik.ingressroutes", "team", "route", `{"spec":{"entryPoints":["web"],"routes":[{"match":"Host(\"a.test\")","services":[{"name":"weighted","kind":"TraefikService","weight":0}]}]}}`),
		topologyFixture("traefik.traefikservices", "team", "weighted", `{"spec":{"weighted":{"services":[{"name":"web","port":80,"weight":0},{"name":"weighted","kind":"TraefikService","weight":2}]},"mirroring":{"name":"web","port":80,"mirrors":[{"name":"shadow","port":80,"percent":15}]}}}`),
		topologyFixture("kubernetes.services", "team", "web", `{"spec":{"type":"ExternalName","externalName":"upstream.example","ports":[{"port":80}]}}`),
	)
	zero := false
	mirror := false
	cycle := false
	for _, edge := range view.Edges {
		zero = zero || (edge.Weight != nil && *edge.Weight == 0)
		mirror = mirror || (edge.Relation == "mirrors" && edge.MirrorPercent != nil && *edge.MirrorPercent == 15)
	}
	for _, node := range view.Nodes {
		cycle = cycle || node.ResolutionState == "unsupported_fragment"
	}
	if !zero || !mirror || !cycle {
		t.Fatalf("lost semantics zero=%v mirror=%v cycle=%v", zero, mirror, cycle)
	}
	if len(view.Nodes) > 200 || len(view.Edges) > 400 {
		t.Fatal("graph budget exceeded")
	}
}
func TestTopologyNeverLeaksInvisibleCrossNamespaceReferences(t *testing.T) {
	view := fixtureView(cr.NetworkTopologySourceResult{Coverage: cr.NetworkTopologyCoverage{SourceID: "gateway.referencegrants", Namespace: "private", State: "available"}}, topologyFixture("gateway.httproutes", "team", "route", `{"spec":{"parentRefs":[{"name":"hidden-gateway","namespace":"private"}],"rules":[{"backendRefs":[{"name":"hidden-service","namespace":"private","port":80}]}]}}`))
	data, _ := json.Marshal(struct {
		Nodes  []cr.NetworkTopologyGraphNode
		Edges  []cr.NetworkTopologyGraphEdge
		Traces []cr.NetworkTopologyTraceView
	}{view.Nodes, view.Edges, view.Traces})
	if strings.Contains(string(data), "hidden-service") || strings.Contains(string(data), "hidden-gateway") || strings.Contains(string(data), "private") {
		t.Fatalf("hidden reference escaped: %s", data)
	}
}
func TestTopologyAPISIXStreamAndExternalUpstream(t *testing.T) {
	view := fixtureView(topologyFixture("apisix.apisixroutes", "team", "route", `{"spec":{"stream":[{"name":"dns","protocol":"UDP","match":{"ingressPort":53},"backend":{"serviceName":"dns","servicePort":53}}]}}`), topologyFixture("apisix.apisixupstreams", "team", "dns", `{"spec":{"scheme":"udp","externalNodes":[{"type":"Domain","name":"resolver.example","port":53,"weight":1}]}}`))
	udp := false
	external := false
	for _, node := range view.Nodes {
		udp = udp || node.Protocol == "UDP"
		external = external || (node.Type == "external" && node.Label == "resolver.example")
	}
	if !udp || !external {
		t.Fatalf("missing UDP/external: %+v", view)
	}
}

func TestTopologyEndpointSliceUsesReadyPortAndPodUID(t *testing.T) {
	service := topologyFixture("kubernetes.services", "team", "web", `{"spec":{"ports":[{"name":"http","port":80,"targetPort":8080,"protocol":"TCP"}]}}`)
	endpoints := topologyFixture("kubernetes.endpointslices", "team", "web-endpoints", `{"metadata":{"labels":{"kubernetes.io/service-name":"web"}},"ports":[{"name":"http","port":8080,"protocol":"TCP"}],"endpoints":[{"targetRef":{"kind":"Pod","name":"web-1","uid":"web-1-uid"},"conditions":{"serving":true}}]}`)
	pod := topologyFixture("kubernetes.pods", "team", "web-1", `{}`)
	view := fixtureView(service, endpoints, pod)
	resolved := false
	for _, edge := range view.Edges {
		if edge.Relation == "resolves" && strings.Contains(edge.Target, "web-1-uid") && edge.ResolutionState == "resolved" {
			resolved = true
		}
	}
	if !resolved {
		t.Fatal("ready=nil EndpointSlice with matching service label, port and Pod UID did not resolve")
	}
	endpoints.Objects[0].Content = strings.Replace(endpoints.Objects[0].Content, `"uid":"web-1-uid"`, `"uid":"old-pod-uid"`, 1)
	view = fixtureView(service, endpoints, pod)
	for _, node := range view.Nodes {
		if node.Type == "pod" {
			t.Fatal("stale Pod UID resolved to its replacement")
		}
	}
}
func TestTopologyGatewayGrantListenerAndZeroWeight(t *testing.T) {
	route := topologyFixture("gateway.httproutes", "team", "route", `{"spec":{"parentRefs":[{"name":"gateway","namespace":"infra","sectionName":"https"}],"rules":[{"backendRefs":[{"name":"web","namespace":"backend","port":80,"weight":0},{"name":"web","namespace":"backend","port":80,"weight":2}]}]}}`)
	gateway := topologyFixture("gateway.gateways", "infra", "gateway", `{"spec":{"listeners":[{"name":"https","port":443,"protocol":"HTTPS","allowedRoutes":{"namespaces":{"from":"All"}}}]}}`)
	service := topologyFixture("kubernetes.services", "backend", "web", `{"spec":{"type":"ExternalName","externalName":"upstream.example","ports":[{"port":80}]}}`)
	grant := topologyFixture("gateway.referencegrants", "backend", "allow", `{"spec":{"from":[{"group":"gateway.networking.k8s.io","kind":"HTTPRoute","namespace":"team"}],"to":[{"group":"","kind":"Service","name":"web"}]}}`)
	view := fixtureView(route, gateway, service, grant)
	zero, parent := false, false
	for _, edge := range view.Edges {
		zero = zero || (edge.Relation == "weights" && edge.Weight != nil && *edge.Weight == 0 && edge.TotalWeight != nil && *edge.TotalWeight == 2 && edge.ResolutionState == "resolved")
		parent = parent || (edge.Relation == "matches" && strings.Contains(edge.Source, "gateway-uid") && edge.ResolutionState == "resolved")
	}
	if !zero || !parent {
		t.Fatalf("lost granted backend/listener/zero semantics: %+v", view.Edges)
	}
	view = fixtureView(route, gateway, service, cr.NetworkTopologySourceResult{Coverage: cr.NetworkTopologyCoverage{SourceID: "gateway.referencegrants", Namespace: "backend", State: "available"}})
	for _, edge := range view.Edges {
		if edge.Relation == "weights" && edge.ResolutionState == "resolved" {
			t.Fatal("cross-namespace backend resolved without ReferenceGrant")
		}
	}
}

func TestTopologyRejectsRouteProtocolServicePortMismatch(t *testing.T) {
	route := topologyFixture("kubernetes.ingresses", "team", "http", `{"spec":{"rules":[{"http":{"paths":[{"path":"/","backend":{"service":{"name":"dns","port":{"number":53}}}}]}}]}}`)
	service := topologyFixture("kubernetes.services", "team", "dns", `{"spec":{"type":"ExternalName","externalName":"dns.example","ports":[{"port":53,"protocol":"UDP"}]}}`)
	view := fixtureView(route, service)
	found := false
	for _, edge := range view.Edges {
		if edge.Relation == "forwards" {
			found = true
			if edge.ResolutionState != "unresolved" {
				t.Fatal("HTTP ingress accepted a UDP Service port")
			}
		}
	}
	if !found {
		t.Fatal("missing configured relationship")
	}
}
