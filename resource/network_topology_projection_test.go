package resource

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestTopologyProjectionExcludesCredentialsAndPreservesAlternatives(t *testing.T) {
	for _, test := range []struct {
		source, input string
		keep          []string
	}{
		{"kubernetes.configmaps", `{"metadata":{"name":"ca","uid":"ca-uid","annotations":{"key":"secret-value"}},"data":{"ca.crt":"secret-value"},"binaryData":{"key":"secret-value"}}`, []string{"ca-uid"}},
		{"kubernetes.namespaces", `{"metadata":{"name":"team","labels":{"access":"permitted"},"annotations":{"token":"secret-value"}}}`, []string{"permitted"}},
		{"kubernetes.networkpolicies", `{"spec":{"podSelector":{"matchLabels":{"app":"web"}},"ingress":[{"from":[{"namespaceSelector":{"matchLabels":{"role":"client"}},"podSelector":{"matchExpressions":[{"key":"tier","operator":"In","values":["frontend"]}]}},{"ipBlock":{"cidr":"10.0.0.0/8","except":["10.1.0.0/16"]}}]}]},"data":"secret-value"}`, []string{"frontend", "10.1.0.0/16"}},
		{"gateway.backendtlspolicies", `{"spec":{"targetRefs":[{"group":"","kind":"Service","name":"web","sectionName":"https"}],"targetRef":{"kind":"Service","name":"legacy"},"validation":{"caCertificateRefs":[{"kind":"ConfigMap","name":"ca"}],"privateKey":"secret-value"}}}`, []string{"https", "legacy", "ca"}},
		{"kubernetes.pods", `{"metadata":{"name":"p","namespace":"n","annotations":{"token":"secret-value"},"labels":{"app":"web"}},"spec":{"containers":[{"env":[{"value":"secret-value"}]}]},"status":{"phase":"Running"}}`, []string{"Running", "web"}},
		{"gateway.httproutes", `{"metadata":{"name":"r"},"spec":{"rules":[{"matches":[{"method":"GET","headers":[{"name":"Authorization","value":"secret-value"}],"queryParams":[{"name":"token","value":"secret-value"}]}],"backendRefs":[{"name":"web","weight":0,"port":80}]}]}}`, []string{"GET", "[redacted]", "\"weight\":0"}},
		{"gateway.tcproutes", `{"spec":{"parentRefs":[{"name":"gw","sectionName":"tcp","port":9000}],"rules":[{"backendRefs":[{"name":"web","port":9000,"weight":0}],"filters":[{"secret":"secret-value"}]}]}}`, []string{"tcp", "9000", "\"weight\":0"}},
		{"gateway.tlsroutes", `{"spec":{"hostnames":["secure.test"],"parentRefs":[{"name":"gw","sectionName":"tls"}],"rules":[{"backendRefs":[{"name":"web","port":9443}]}],"tls":{"key":"secret-value"}}}`, []string{"secure.test", "tls", "9443"}},
		{"gateway.udproutes", `{"spec":{"parentRefs":[{"name":"gw","sectionName":"udp"}],"rules":[{"backendRefs":[{"name":"dns","port":53,"weight":2}]}]},"status":{"parents":[{"conditions":[{"type":"Accepted","status":"True","message":"secret-value"}]}]}}`, []string{"udp", "53", "Accepted"}},
		{"apisix.apisixroutes", `{"metadata":{"name":"r"},"spec":{"http":[{"match":{"hosts":["a.test","b.test"],"paths":["/a","/b"],"exprs":[{"subject":{"scope":"Header","name":"token"},"value":"secret-value"}]},"plugins":[{"name":"key-auth","enable":true,"config":{"key":"secret-value"},"secretRef":"secret-value"}]}]}}`, []string{"a.test", "b.test", "/a", "/b", "key-auth"}},
		{"traefik.ingressroutes", `{"metadata":{"name":"r"},"spec":{"routes":[{"match":"Header(authorization,secret-value)","services":[{"name":"web","port":80}]}],"tls":{"secretName":"secret-value"}}}`, []string{"[redacted match expression]", "web"}},
		{"traefik.middlewares", `{"metadata":{"name":"m"},"spec":{"basicAuth":{"users":["secret-value"]},"chain":{"middlewares":[{"name":"next"}]}}}`, []string{"basicAuth", "next"}},
	} {
		t.Run(test.source, func(t *testing.T) {
			var raw map[string]any
			if err := json.Unmarshal([]byte(test.input), &raw); err != nil {
				t.Fatal(err)
			}
			projection, err := ProjectNetworkTopologyObject(test.source, raw)
			if err != nil {
				t.Fatal(err)
			}
			data, _ := json.Marshal(projection)
			if strings.Contains(string(data), "secret-value") {
				t.Fatalf("credential escaped: %s", data)
			}
			for _, value := range test.keep {
				if !strings.Contains(string(data), value) {
					t.Fatalf("lost %q: %s", value, data)
				}
			}
			var again map[string]any
			_ = json.Unmarshal(data, &again)
			second, err := ProjectNetworkTopologyObject(test.source, again)
			if err != nil {
				t.Fatal(err)
			}
			data2, _ := json.Marshal(second)
			if string(data) != string(data2) {
				t.Fatalf("projection is not stable: %s -> %s", data, data2)
			}
		})
	}
}
