package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	cr "github.com/opensoha/soha/contracts/resource"
)

func networkFixtureGraph(t *testing.T, root cr.NetworkTopologySourceResult, sources ...cr.NetworkTopologySourceResult) cr.ResourceGraph {
	t.Helper()
	sources = append([]cr.NetworkTopologySourceResult{root}, sources...)
	graph, err := BuildNetworkResourceGraph(context.Background(), root.Objects[0].Resource, cr.NetworkTopologySnapshot{Sources: sources})
	if err != nil {
		t.Fatal(err)
	}
	return graph
}
func networkEdges(graph cr.ResourceGraph, relation string) []cr.ResourceGraphEdge {
	result := []cr.ResourceGraphEdge{}
	for _, e := range graph.Edges {
		if e.Relation == relation {
			result = append(result, e)
		}
	}
	return result
}

func TestNetworkGraphEndpointFactsKeepSelectorReadinessAndUIDSeparate(t *testing.T) {
	service := topologyFixture("kubernetes.services", "team", "web", `{"spec":{"selector":{"app":"web"},"ports":[{"name":"http","port":80}]}}`)
	pod := topologyFixture("kubernetes.pods", "team", "pod", `{"metadata":{"labels":{"app":"web"}}}`)
	for _, tc := range []struct{ name, uid, ready, state string }{{"ready", "pod-uid", "true", "resolved"}, {"notReady", "pod-uid", "false", "no_ready_endpoints"}, {"stale", "old-uid", "true", "unresolved"}, {"missingUID", "", "true", "unresolved"}} {
		t.Run(tc.name, func(t *testing.T) {
			slice := topologyFixture("kubernetes.endpointslices", "team", "eps", fmt.Sprintf(`{"metadata":{"labels":{"kubernetes.io/service-name":"web"}},"ports":[{"name":"http","port":8080}],"endpoints":[{"targetRef":{"apiVersion":"v1","kind":"Pod","name":"pod","uid":%q},"conditions":{"ready":%s}}]}`, tc.uid, tc.ready))
			graph := networkFixtureGraph(t, service, slice, pod)
			if len(networkEdges(graph, "selects")) != 1 {
				t.Fatal("lost independent selector")
			}
			found := false
			for _, e := range networkEdges(graph, "resolves") {
				if strings.Contains(e.SourceID, "EndpointSlice") && e.ResolutionState == tc.state {
					found = true
				}
			}
			if !found {
				t.Fatalf("endpoint state %s missing: %+v", tc.state, graph.Edges)
			}
		})
	}
	slice := topologyFixture("kubernetes.endpointslices", "team", "unlabelled", `{"endpoints":[{"targetRef":{"kind":"Pod","name":"pod","uid":"pod-uid"}}]}`)
	graph := networkFixtureGraph(t, service, slice, pod)
	if len(networkEdges(graph, "resolves")) != 0 {
		t.Fatal("unlabelled slice associated by name")
	}
}
func TestNetworkGraphGrantFromAndToAndTargetNamespace(t *testing.T) {
	route := topologyFixture("gateway.httproutes", "team", "route", `{"spec":{"rules":[{"backendRefs":[{"name":"backend","namespace":"other","port":80}]}]}}`)
	backend := topologyFixture("kubernetes.services", "other", "backend", `{"spec":{"ports":[{"port":80}]}}`)
	for _, tc := range []struct{ name, ns, from, to, state, want string }{{"valid", "other", "team", "backend", "available", "resolved"}, {"wrongNamespace", "elsewhere", "team", "backend", "available", "unknown"}, {"wrongFrom", "other", "elsewhere", "backend", "available", "unresolved"}, {"wrongTo", "other", "team", "elsewhere", "available", "unresolved"}, {"partial", "other", "team", "backend", "partial", "unknown"}} {
		t.Run(tc.name, func(t *testing.T) {
			grant := topologyFixture("gateway.referencegrants", tc.ns, "grant", fmt.Sprintf(`{"spec":{"from":[{"group":"wrong","kind":"HTTPRoute","namespace":"team"},{"group":"gateway.networking.k8s.io","kind":"HTTPRoute","namespace":%q}],"to":[{"group":"","kind":"Service","name":%q}]}}`, tc.from, tc.to))
			grant.Coverage.State = tc.state
			graph := networkFixtureGraph(t, route, backend, grant)
			edges := networkEdges(graph, "routes-to")
			if len(edges) != 1 || edges[0].ResolutionState != tc.want {
				t.Fatalf("grant mismatch: %+v", edges)
			}
			auth := networkEdges(graph, "authorizes-reference")
			if (len(auth) > 0) != (tc.want == "resolved") {
				t.Fatalf("incorrect authorization fact: %+v", auth)
			}
		})
	}
}
func TestNetworkGraphGatewayListenerSelectorsTLSAndNamedCoverage(t *testing.T) {
	route := topologyFixture("gateway.httproutes", "team", "route", `{"spec":{"parentRefs":[{"name":"gw","namespace":"infra","sectionName":"https","port":443}]}}`)
	gateway := topologyFixture("gateway.gateways", "infra", "gw", `{"spec":{"gatewayClassName":"managed","listeners":[{"name":"https","port":443,"protocol":"HTTPS","allowedRoutes":{"namespaces":{"from":"Selector","selector":{"matchLabels":{"access":"yes"}}}},"tls":{"certificateRefs":[{"name":"tls"}]}}]}}`)
	namespace := topologyFixture("kubernetes.namespaces", "", "team", `{"metadata":{"labels":{"access":"yes"}}}`)
	graph := networkFixtureGraph(t, route, gateway, namespace)
	edges := networkEdges(graph, "attaches-to")
	if len(edges) != 1 || edges[0].ResolutionState != "resolved" || edges[0].Port != "443" || edges[0].SectionName != "https" || edges[0].SourceRef.Kind != "HTTPRoute" {
		t.Fatalf("lost parent attachment: %+v", edges)
	}
	graph = networkFixtureGraph(t, gateway, route, namespace)
	secrets := networkEdges(graph, "uses-secret")
	if len(secrets) != 1 || secrets[0].ResolutionState != "unresolved" || secrets[0].SectionName != "https" {
		t.Fatalf("lost metadata-only TLS declaration: %+v", secrets)
	}
	namespace.Coverage.Name = "team"
	graph = networkFixtureGraph(t, route, gateway, namespace)
	if edges = networkEdges(graph, "attaches-to"); len(edges) != 1 || edges[0].ResolutionState == "resolved" {
		t.Fatalf("named namespace falsely proved all selector coverage: %+v", edges)
	}
}
func TestNetworkGraphBackendTLSAndIngressDeclarations(t *testing.T) {
	policy := topologyFixture("gateway.backendtlspolicies", "team", "tls", `{"spec":{"targetRefs":[{"group":"","kind":"Service","name":"web","sectionName":"https"}],"validation":{"caCertificateRefs":[{"group":"","kind":"ConfigMap","name":"ca"}]}}}`)
	service := topologyFixture("kubernetes.services", "team", "web", `{"spec":{"ports":[{"name":"https","port":443}]}}`)
	config := topologyFixture("kubernetes.configmaps", "team", "ca", `{"data":{"private":"MUST_NOT_ESCAPE"}}`)
	graph := networkFixtureGraph(t, policy, service, config)
	targets := networkEdges(graph, "targets")
	if len(targets) != 1 || targets[0].ResolutionState != "resolved" || targets[0].SectionName != "https" || targets[0].Port != "443" || len(networkEdges(graph, "uses-config")) != 1 {
		t.Fatalf("lost TLS policy refs: %+v", graph)
	}
	encoded, _ := json.Marshal(graph)
	if strings.Contains(string(encoded), "MUST_NOT_ESCAPE") {
		t.Fatal("ConfigMap contents escaped")
	}
	ingress := topologyFixture("kubernetes.ingresses", "team", "ing", `{"spec":{"ingressClassName":"nginx","tls":[{"secretName":"tls"}],"defaultBackend":{"service":{"name":"web","port":{"number":80}}}}}`)
	graph = networkFixtureGraph(t, ingress, service)
	if len(networkEdges(graph, "uses-class")) != 1 || len(networkEdges(graph, "uses-secret")) != 1 || len(networkEdges(graph, "routes-to")) != 1 {
		t.Fatalf("lost ingress refs: %+v", graph.Edges)
	}
}

func TestNetworkGraphBackendTLSPolicySectionRequiresExactServicePortName(t *testing.T) {
	service := topologyFixture("kubernetes.services", "team", "web", `{"spec":{"ports":[{"name":"https","port":443,"targetPort":"backend"}]}}`)
	for _, section := range []string{"443", "backend", "missing"} {
		t.Run(section, func(t *testing.T) {
			policy := topologyFixture("gateway.backendtlspolicies", "team", "tls", fmt.Sprintf(`{"spec":{"targetRefs":[{"group":"","kind":"Service","name":"web","sectionName":%q}]}}`, section))
			edges := networkEdges(networkFixtureGraph(t, policy, service), "targets")
			if len(edges) != 1 || edges[0].ResolutionState != "unresolved" || edges[0].SectionName != section || edges[0].Port != "" {
				t.Fatalf("incorrect Service port match: %+v", edges)
			}
		})
	}
}
func TestNetworkGraphNetworkPolicyPeerANDAndIPBlockEvidence(t *testing.T) {
	policy := topologyFixture("kubernetes.networkpolicies", "team", "policy", `{"spec":{"podSelector":{},"ingress":[{"from":[{"namespaceSelector":{"matchLabels":{"role":"client"}},"podSelector":{"matchLabels":{"app":"client"}}},{"ipBlock":{"cidr":"10.0.0.0/8","except":["10.1.0.0/16"]}}]}]}}`)
	namespace := topologyFixture("kubernetes.namespaces", "", "other", `{"metadata":{"labels":{"role":"client"}}}`)
	pod := topologyFixture("kubernetes.pods", "team", "selected", `{}`)
	peer := topologyFixture("kubernetes.pods", "other", "peer", `{"metadata":{"labels":{"app":"client"}}}`)
	unrelated := topologyFixture("kubernetes.pods", "other", "unrelated", `{"metadata":{"labels":{"app":"wrong"}}}`)
	graph := networkFixtureGraph(t, policy, namespace, pod, peer, unrelated)
	if len(networkEdges(graph, "selects")) != 1 || len(networkEdges(graph, "policy-peer")) != 1 {
		t.Fatalf("policy selector AND lost: %+v", graph.Edges)
	}
	found := false
	for _, e := range graph.Evidence {
		if strings.Contains(e.Summary, "10.1.0.0/16") {
			found = true
		}
	}
	if !found {
		t.Fatal("IPBlock/except declaration missing")
	}
}
func TestNetworkGraphExactIdentityCancellationAndNamedRootPriority(t *testing.T) {
	root := topologyFixture("kubernetes.services", "team", "web", `{}`)
	root.Objects[0].Resource.APIVersion = "evil/v1"
	if _, err := BuildNetworkResourceGraph(context.Background(), root.Objects[0].Resource, cr.NetworkTopologySnapshot{Sources: []cr.NetworkTopologySourceResult{root}}); !errors.Is(err, ErrUnsupportedResourceGraphRoot) {
		t.Fatalf("accepted lookalike GVK: %v", err)
	}
	root = topologyFixture("kubernetes.services", "team", "web", `{}`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := BuildNetworkResourceGraph(ctx, root.Objects[0].Resource, cr.NetworkTopologySnapshot{Sources: []cr.NetworkTopologySourceResult{root}}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation ignored: %v", err)
	}
	stale := topologyFixture("kubernetes.services", "team", "web", `{}`)
	stale.Objects[0].Resource.UID = "old"
	stale.Objects[0].Content = strings.ReplaceAll(stale.Objects[0].Content, "web-uid", "old")
	root.Coverage.Name = "web"
	graph, err := BuildNetworkResourceGraph(context.Background(), root.Objects[0].Resource, cr.NetworkTopologySnapshot{Sources: []cr.NetworkTopologySourceResult{stale, root}})
	if err != nil || !strings.Contains(graph.RootID, "web-uid") {
		t.Fatalf("named root lost to stale list: %+v %v", graph, err)
	}
}
func TestNetworkGraphTraversalNeverReversesAndBounds(t *testing.T) {
	edges := []cr.ResourceGraphEdge{{ID: "a", SourceID: "root", TargetID: "shared"}, {ID: "b", SourceID: "unrelated", TargetID: "shared"}}
	for i := 0; i < 8; i++ {
		edges = append(edges, cr.ResourceGraphEdge{ID: fmt.Sprint(i), SourceID: fmt.Sprintf("n%d", i), TargetID: fmt.Sprintf("n%d", i+1)})
	}
	edges = append(edges, cr.ResourceGraphEdge{ID: "start", SourceID: "root", TargetID: "n0"})
	selected, _, depth, _, err := networkGraphTraverse(context.Background(), "root", edges)
	if err != nil || selected["unrelated"] || selected["n3"] || !depth {
		t.Fatalf("unbounded/reversed traversal: %+v %v", selected, err)
	}
	edges = nil
	for i := 0; i < 2000; i++ {
		edges = append(edges, cr.ResourceGraphEdge{ID: fmt.Sprint(i), SourceID: "root", TargetID: fmt.Sprint(i)})
	}
	selected, chosen, _, limit, err := networkGraphTraverse(context.Background(), "root", edges)
	if err != nil || !limit || len(selected) > 500 || len(chosen) > 1000 {
		t.Fatalf("output budget failed: %d %d %v", len(selected), len(chosen), err)
	}
}

func TestNetworkGraphSliceRootAndLegacyBackendTLS(t *testing.T) {
	slice := topologyFixture("kubernetes.endpointslices", "team", "eps", `{"ports":[{"protocol":"UDP","port":53}],"endpoints":[{"targetRef":{"kind":"Pod","name":"dns","uid":"dns-uid"}}]}`)
	pod := topologyFixture("kubernetes.pods", "team", "dns", `{}`)
	graph := networkFixtureGraph(t, slice, pod)
	edges := networkEdges(graph, "resolves")
	if len(edges) != 1 || edges[0].Port != "53" || edges[0].Protocol != "UDP" {
		t.Fatalf("slice root lost endpoint reference: %+v", graph)
	}
	policy := topologyFixture("gateway.backendtlspolicies", "team", "legacy", `{"spec":{"targetRef":{"group":"","kind":"Service","name":"dns"}}}`)
	policy.Objects[0].Resource.APIVersion = "gateway.networking.k8s.io/v1alpha2"
	service := topologyFixture("kubernetes.services", "team", "dns", `{}`)
	graph = networkFixtureGraph(t, policy, service)
	if edges := networkEdges(graph, "targets"); len(edges) != 1 || edges[0].FieldPath != "spec.targetRef" {
		t.Fatalf("legacy targetRef lost: %+v", graph)
	}
}

func TestNetworkGraphLateRootFactsPrecedeUnrelatedHighFanout(t *testing.T) {
	// 80 earlier Services selecting 150 Pods exhaust the 10000-edge working
	// set. The selected root sorts after those Services and must keep its facts.
	services := topologyFixture("kubernetes.services", "team", "a-noise-000", `{"spec":{"selector":{"noise":"yes"}}}`)
	for i := 1; i < 80; i++ {
		f := topologyFixture("kubernetes.services", "team", fmt.Sprintf("a-noise-%03d", i), `{"spec":{"selector":{"noise":"yes"}}}`)
		services.Objects = append(services.Objects, f.Objects...)
	}
	pods := topologyFixture("kubernetes.pods", "team", "chosen", `{"metadata":{"labels":{"app":"chosen"}}}`)
	for i := 0; i < 150; i++ {
		f := topologyFixture("kubernetes.pods", "team", fmt.Sprintf("noise-%03d", i), `{"metadata":{"labels":{"noise":"yes"}}}`)
		pods.Objects = append(pods.Objects, f.Objects...)
	}
	for _, tc := range []struct{ name, source, body, relation, path string }{
		{"service", "kubernetes.services", `{"spec":{"selector":{"app":"chosen"}}}`, "selects", "spec.selector"},
		{"policy", "kubernetes.networkpolicies", `{"spec":{"podSelector":{"matchLabels":{"app":"chosen"}}}}`, "selects", "spec.podSelector"},
		{"gateway", "gateway.gateways", `{"spec":{"gatewayClassName":"chosen"}}`, "uses-class", "spec.gatewayClassName"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := topologyFixture(tc.source, "team", "z-selected", tc.body)
			graph := networkFixtureGraph(t, root, services, pods)
			found := false
			for _, e := range graph.Edges {
				if e.SourceID == graph.RootID && e.Relation == tc.relation && e.FieldPath == tc.path {
					found = true
				}
			}
			if !found {
				t.Fatalf("unrelated facts starved selected root: %+v", graph)
			}
			if len(graph.Warnings) == 0 {
				t.Fatal("working-set truncation must stay visible")
			}
			if len(graph.Nodes) > 500 || len(graph.Edges) > 1000 {
				t.Fatal("root priority bypassed output budgets")
			}
		})
	}
}

func TestNetworkGraphIngressBackendFieldPathsMatchDeclarations(t *testing.T) {
	ingress := topologyFixture("kubernetes.ingresses", "team", "ing", `{"spec":{"defaultBackend":{"service":{"name":"web","port":{"number":80}}},"rules":[{"http":{"paths":[{"backend":{"service":{"name":"web","port":{"number":80}}}},{"backend":{"service":{"name":"web","port":{"number":81}}}}]}},{"http":{"paths":[{"backend":{"service":{"name":"web","port":{"number":82}}}}]}}]}}`)
	service := topologyFixture("kubernetes.services", "team", "web", `{"spec":{"ports":[{"port":80},{"port":81},{"port":82}]}}`)
	graph := networkFixtureGraph(t, ingress, service)
	paths := map[string]bool{}
	for _, e := range networkEdges(graph, "routes-to") {
		paths[e.FieldPath] = true
	}
	for _, path := range []string{"spec.defaultBackend", "spec.rules[0].http.paths[0].backend", "spec.rules[0].http.paths[1].backend", "spec.rules[1].http.paths[0].backend"} {
		if !paths[path] {
			t.Fatalf("missing actual field path %s: %+v", path, graph.Edges)
		}
	}
	if len(paths) != 4 {
		t.Fatalf("unexpected flattened field paths: %+v", paths)
	}
}

func TestNetworkGraphPublicTopologyRedactsUnavailableDeclarationIdentities(t *testing.T) {
	class := topologyFixture("kubernetes.ingressclasses", "", "class", `{"spec":{"controller":"example.io/controller","parameters":{"apiGroup":"","kind":"ConfigMap","namespace":"private","name":"hidden-ca","scope":"Namespace"}}}`)
	denied := cr.NetworkTopologySourceResult{Coverage: cr.NetworkTopologyCoverage{SourceID: "kubernetes.configmaps", Namespace: "private", State: "denied"}}
	ingress := topologyFixture("kubernetes.ingresses", "team", "ing", `{"spec":{"tls":[{"secretName":"private-tls"}]}}`)
	for _, tc := range []struct {
		name    string
		sources []cr.NetworkTopologySourceResult
		hidden  []string
	}{
		{"deniedConfig", []cr.NetworkTopologySourceResult{class, denied}, []string{"hidden-ca", "private"}},
		{"absentConfig", []cr.NetworkTopologySourceResult{class}, []string{"hidden-ca", "private"}},
		{"secret", []cr.NetworkTopologySourceResult{ingress}, []string{"private-tls"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			view := fixtureView(tc.sources...)
			raw, _ := json.Marshal(struct {
				Nodes  []cr.NetworkTopologyGraphNode
				Edges  []cr.NetworkTopologyGraphEdge
				Traces []cr.NetworkTopologyTraceView
			}{view.Nodes, view.Edges, view.Traces})
			for _, hidden := range tc.hidden {
				if strings.Contains(string(raw), hidden) {
					t.Fatalf("private topology identity escaped (%s): %s", hidden, raw)
				}
			}
		})
	}
	graph := networkFixtureGraph(t, ingress)
	refs := networkEdges(graph, "uses-secret")
	if len(refs) != 1 || refs[0].ResolutionState != "unresolved" {
		t.Fatalf("Graph lost minimal Secret declaration: %+v", graph)
	}
	found := false
	for _, node := range graph.Nodes {
		if node.Resource.Kind == "Secret" && node.Resource.Name == "private-tls" && node.Resource.UID == "" && node.Status == "unresolved" {
			found = true
		}
	}
	if !found {
		t.Fatal("Graph declaration unavailable for Core permission filtering")
	}
}

func TestNetworkGraphUnknownBackendGroupCannotBecomeCoreService(t *testing.T) {
	route := topologyFixture("gateway.httproutes", "team", "route", `{"spec":{"rules":[{"backendRefs":[{"group":"example.io","kind":"Service","name":"custom-backend","port":80}]}]}}`)
	service := topologyFixture("kubernetes.services", "team", "custom-backend", `{"spec":{"ports":[{"port":80}]}}`)
	snapshot := cr.NetworkTopologySnapshot{Sources: []cr.NetworkTopologySourceResult{route, service}}
	view := BuildNetworkTopology("c", "team", snapshot)
	for _, edge := range view.Edges {
		if edge.ResolutionState == "unsupported_fragment" {
			for _, node := range view.Nodes {
				if node.ID == edge.Target && node.Resource != nil {
					t.Fatalf("unknown group received fabricated resource identity: %+v", node)
				}
			}
		}
	}
	graph := networkFixtureGraph(t, route, service)
	if len(networkEdges(graph, "routes-to")) != 0 {
		t.Fatalf("unknown group became a core Service route: %+v", graph.Edges)
	}
	for _, node := range graph.Nodes {
		if node.Resource.Kind == "Service" {
			t.Fatalf("unrelated core Service became navigable: %+v", node)
		}
	}
	warned := false
	for _, warning := range graph.Warnings {
		if strings.Contains(warning, "Unsupported network reference") {
			warned = true
		}
	}
	if !warned {
		t.Fatal("unsupported fragment silently disappeared")
	}
}
