package runtime

import (
	"encoding/json"
	"testing"

	cr "github.com/opensoha/soha-contracts/resource"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestNetworkRelationshipServiceSelectorParityAndMalformedValues(t *testing.T) {
	for _, tc := range []struct {
		name, selector string
		match          bool
	}{
		{"missing", "null", false}, {"empty", "{}", false},
		{"match", `{"app":"web"}`, true}, {"different", `{"app":"other"}`, false},
		{"allLabelsRequired", `{"app":"web","tier":"api"}`, false},
		{"missingEmptyLabel", `{"missing":""}`, false},
		{"nonString", `{"app":42}`, false},
		{"doNotDropMalformedTerm", `{"app":"web","malformed":42}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pod := graphTestObject("v1", "Pod", "pod", "pod-uid")
			pod.SetLabels(map[string]string{"app": "web"})
			service := graphTestObject("v1", "Service", "svc", "svc-uid")
			var selector any
			if err := json.Unmarshal([]byte(tc.selector), &selector); err != nil {
				t.Fatal(err)
			}
			service.Object["spec"] = map[string]any{"selector": selector}
			old, err := BuildResourceGraphFromObjects(graphTestRoot("Pod", "pod"), []*unstructured.Unstructured{pod, service})
			if err != nil {
				t.Fatal(err)
			}
			if got := graphTestEdge(old, "selects", "svc", "pod"); got != tc.match {
				t.Fatalf("Workload selector match=%v want=%v", got, tc.match)
			}
			networkService := topologyFixture("kubernetes.services", "ns", "svc", `{"spec":{"selector":`+tc.selector+`}}`)
			networkPod := topologyFixture("kubernetes.pods", "ns", "pod", `{"metadata":{"labels":{"app":"web"}}}`)
			network := networkFixtureGraph(t, networkService, networkPod)
			if got := len(networkEdges(network, "selects")) > 0; got != tc.match {
				t.Fatalf("Network selector match=%v want=%v", got, tc.match)
			}
		})
	}
}

func TestNetworkRelationshipIngressDefaultAndPathsPreserveBothViews(t *testing.T) {
	for _, tc := range []struct {
		name, spec   string
		names, paths []string
	}{
		{"defaultOnly", `{"defaultBackend":{"service":{"name":"default","port":{"name":"http"}}}}`, []string{"default"}, []string{"spec.defaultBackend"}},
		{"pathsOnly", `{"rules":[{"host":"a.test","http":{"paths":[{"path":"/a","pathType":"Prefix","backend":{"service":{"name":"first","port":{"number":80}}}},{"path":"/b","backend":{"service":{"name":"second","port":{"number":80}}}}]}},{"host":"b.test","http":{"paths":[{"backend":{"service":{"name":"last","port":{"number":80}}}}]}}]}`, []string{"first", "second", "last"}, []string{"spec.rules[0].http.paths[0].backend", "spec.rules[0].http.paths[1].backend", "spec.rules[1].http.paths[0].backend"}},
		{"defaultAndPath", `{"defaultBackend":{"service":{"name":"default","port":{"name":"http"}}},"rules":[{"host":"a.test","http":{"paths":[{"path":"/a","backend":{"service":{"name":"first","port":{"number":80}}}}]}}]}`, []string{"default", "first"}, []string{"spec.defaultBackend", "spec.rules[0].http.paths[0].backend"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var spec map[string]any
			if err := json.Unmarshal([]byte(tc.spec), &spec); err != nil {
				t.Fatal(err)
			}
			pod := graphTestObject("v1", "Pod", "pod", "pod-uid")
			pod.SetLabels(map[string]string{"app": "web"})
			ingress := graphTestObject("networking.k8s.io/v1", "Ingress", "ing", "ing-uid")
			ingress.Object["spec"] = spec
			objects := []*unstructured.Unstructured{pod, ingress}
			sources := []cr.NetworkTopologySourceResult{}
			for _, name := range tc.names {
				service := graphTestObject("v1", "Service", name, name+"-uid")
				service.Object["spec"] = map[string]any{"selector": map[string]any{"app": "web"}}
				objects = append(objects, service)
				sources = append(sources, topologyFixture("kubernetes.services", "ns", name, `{"spec":{"ports":[{"name":"http","port":80}]}}`))
			}
			workload, err := BuildResourceGraphFromObjects(graphTestRoot("Pod", "pod"), objects)
			if err != nil {
				t.Fatal(err)
			}
			for _, name := range tc.names {
				if !graphTestEdge(workload, "routes-to", "ing", name) {
					t.Fatalf("Workload dropped backend %s: %+v", name, workload.Edges)
				}
			}
			if edges := networkEdges(workload, "routes-to"); len(edges) != len(tc.names) {
				t.Fatalf("Workload changed name-only edge count: %+v", edges)
			} else {
				for _, edge := range edges {
					if edge.Port != "" || edge.FieldPath != "" {
						t.Fatalf("Workload adopted network-only facts: %+v", edge)
					}
				}
			}
			networkIngress := topologyFixture("kubernetes.ingresses", "ns", "ing", `{"spec":`+tc.spec+`}`)
			network := networkFixtureGraph(t, networkIngress, sources...)
			actual := map[string]bool{}
			for _, edge := range networkEdges(network, "routes-to") {
				actual[edge.FieldPath] = true
				if edge.Port == "" {
					t.Fatal("Network dropped backend port")
				}
			}
			if len(actual) != len(tc.paths) {
				t.Fatalf("Network changed backend declarations: %+v", actual)
			}
			for _, path := range tc.paths {
				if !actual[path] {
					t.Fatalf("Network dropped declaration %s", path)
				}
			}
			view := fixtureView(append([]cr.NetworkTopologySourceResult{networkIngress}, sources...)...)
			if tc.name != "defaultOnly" {
				found := false
				for _, node := range view.Nodes {
					if node.Rule != nil {
						for _, group := range node.Rule.MatchGroups {
							for _, match := range group {
								if match.Type == "host" && match.Value == "a.test" {
									found = true
								}
							}
						}
					}
				}
				if !found {
					t.Fatal("Topology lost match context")
				}
			}
		})
	}
}

func TestNetworkRelationshipIngressIterationHonorsBudgetAndUnsupportedBackend(t *testing.T) {
	body := map[string]any{}
	if err := json.Unmarshal([]byte(`{"spec":{"defaultBackend":{"resource":{"apiGroup":"example.io","kind":"Bucket","name":"custom"}},"rules":[{"http":{"paths":[{"backend":{"service":{"name":"later"}}}]}}]}}`), &body); err != nil {
		t.Fatal(err)
	}
	count := 0
	forEachIngressBackendDeclaration(body, func() bool { return count == 0 }, func(ref ingressBackendDeclaration) {
		count++
		if ref.fieldPath != "spec.defaultBackend" || ref.serviceDeclared || ref.serviceName != "" {
			t.Fatalf("unsupported backend fabricated a Service: %+v", ref)
		}
	})
	if count != 1 {
		t.Fatalf("iterator ignored caller budget: %d", count)
	}
}
