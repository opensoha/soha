package resource

import (
	"fmt"
	"strings"
	"testing"
)

func TestGatewayL4TopologyKeepsListenerPortsWeightsAndTLSMatches(t *testing.T) {
	for _, protocol := range []string{"TCP", "TLS", "UDP"} {
		t.Run(protocol, func(t *testing.T) {
			kind := protocol + "Route"
			source := "gateway." + strings.ToLower(protocol) + "routes"
			tls := ""
			if protocol == "TLS" {
				tls = `,"tls":{"mode":"Passthrough"},"hostname":"*.example.test"`
			}
			view := fixtureView(
				topologyFixture("gateway.gateways", "team", "gw", fmt.Sprintf(`{"spec":{"listeners":[{"name":"l4","port":9000,"protocol":%q%s},{"name":"other","port":80,"protocol":"HTTP"}]}}`, protocol, tls)),
				topologyFixture(source, "team", "route", `{"spec":{"hostnames":["app.example.test"],"parentRefs":[{"name":"gw"}],"rules":[{"backendRefs":[{"name":"web","port":9000,"weight":0},{"name":"web","port":9000,"weight":3}]}]}}`),
				topologyFixture("kubernetes.services", "team", "web", fmt.Sprintf(`{"spec":{"ports":[{"port":9000,"protocol":%q}]}}`, map[bool]string{true: "UDP", false: "TCP"}[protocol == "UDP"])),
			)
			parent, zero, total, ruleFound := false, false, false, false
			for _, node := range view.Nodes {
				if node.Resource != nil && node.Resource.Kind == kind && node.Rule != nil {
					ruleFound = node.Protocol == protocol
					if protocol == "TLS" && (node.Rule.TLSMode != "Passthrough" || len(node.Rule.MatchGroups) != 1 || node.Rule.MatchGroups[0][0].Value != "app.example.test") {
						t.Fatalf("TLS evidence lost: %+v", node.Rule)
					}
				}
			}
			for _, edge := range view.Edges {
				if edge.Relation == "matches" && edge.Port == "9000" && edge.Protocol == protocol && edge.ResolutionState == "resolved" {
					parent = true
				}
				if edge.Relation == "weights" && edge.Protocol == protocol && edge.Port == "9000" {
					zero = zero || edge.Weight != nil && *edge.Weight == 0
					total = total || edge.TotalWeight != nil && *edge.TotalWeight == 3
				}
			}
			if !parent || !zero || !total || !ruleFound {
				t.Fatalf("lost L4 evidence: %+v", view)
			}
		})
	}
}

func TestGatewayL4TopologyRejectsWrongListenerSNIAndServiceProtocol(t *testing.T) {
	for _, test := range []struct{ name, kind, listener, backendProtocol string }{
		{"tcp-vs-udp", "TCP", `{"name":"entry","protocol":"UDP","port":9000}`, "TCP"},
		{"tls-termination", "TLS", `{"name":"entry","protocol":"TLS","port":9000,"tls":{"mode":"Terminate"}}`, "TCP"},
		{"wrong-sni", "TLS", `{"name":"entry","protocol":"TLS","port":9000,"hostname":"other.test","tls":{"mode":"Passthrough"}}`, "TCP"},
		{"wrong-service-protocol", "UDP", `{"name":"entry","protocol":"UDP","port":9000}`, "TCP"},
	} {
		t.Run(test.name, func(t *testing.T) {
			view := fixtureView(topologyFixture("gateway.gateways", "team", "gw", `{"spec":{"listeners":[`+test.listener+`]}}`), topologyFixture("gateway."+strings.ToLower(test.kind)+"routes", "team", "route", `{"spec":{"hostnames":["app.test"],"parentRefs":[{"name":"gw","sectionName":"entry"}],"rules":[{"backendRefs":[{"name":"web","port":9000}]}]}}`), topologyFixture("kubernetes.services", "team", "web", fmt.Sprintf(`{"spec":{"ports":[{"port":9000,"protocol":%q}]}}`, test.backendProtocol)))
			found := false
			for _, edge := range view.Edges {
				if test.name == "wrong-service-protocol" {
					found = found || edge.Relation == "weights" && edge.ResolutionState == "unresolved"
				} else {
					found = found || edge.Relation == "matches" && edge.Port == "" && edge.ResolutionState != "resolved"
				}
			}
			if !found {
				t.Fatalf("invalid L4 relationship accepted: %+v", view.Edges)
			}
		})
	}
}

func TestGatewayL4CrossNamespaceBackendRequiresMatchingReferenceGrant(t *testing.T) {
	route := topologyFixture("gateway.tcproutes", "team", "route", `{"spec":{"rules":[{"backendRefs":[{"name":"private-backend","namespace":"other","port":9000}]}]}}`)
	service := topologyFixture("kubernetes.services", "other", "private-backend", `{"spec":{"ports":[{"port":9000}]}}`)
	for _, kind := range []string{"HTTPRoute", "TCPRoute"} {
		grant := topologyFixture("gateway.referencegrants", "other", "grant", fmt.Sprintf(`{"spec":{"from":[{"group":"gateway.networking.k8s.io","kind":%q,"namespace":"team"}],"to":[{"group":"","kind":"Service","name":"private-backend"}]}}`, kind))
		view := fixtureView(route, service, grant)
		linked := false
		for _, edge := range view.Edges {
			linked = linked || edge.Relation == "weights" && strings.Contains(edge.Target, "|Service|other|private-backend|")
		}
		if linked != (kind == "TCPRoute") {
			t.Fatalf("ReferenceGrant kind boundary lost for %s: %+v", kind, view.Edges)
		}
	}
}
