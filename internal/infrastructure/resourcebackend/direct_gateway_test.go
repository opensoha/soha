package resourcebackend

import (
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

func TestGatewayDetailMappingsPreserveRelationships(t *testing.T) {
	t.Parallel()
	item := unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "edge", "namespace": "infra"},
		"spec": map[string]any{
			"listeners": []any{map[string]any{
				"name": "https", "protocol": "HTTPS", "port": int64(443), "hostname": "api.example.com",
				"tls": map[string]any{"mode": "Terminate", "certificateRefs": []any{map[string]any{"kind": "Secret", "name": "edge-cert"}}},
			}},
		},
		"status": map[string]any{"listeners": []any{map[string]any{"name": "https", "attachedRoutes": int64(2)}}},
	}}

	listeners := gatewayListeners(item)
	if len(listeners) != 1 || listeners[0].Port != 443 || listeners[0].TLSMode != "Terminate" || listeners[0].AttachedRoutes != 2 {
		t.Fatalf("gatewayListeners() = %#v", listeners)
	}
	if len(listeners[0].CertificateRefs) != 1 || listeners[0].CertificateRefs[0] != "infra:Secret/edge-cert" {
		t.Fatalf("certificateRefs = %#v", listeners[0].CertificateRefs)
	}

	route := unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "api", "namespace": "infra"},
		"spec": map[string]any{"rules": []any{map[string]any{
			"matches":     []any{map[string]any{"method": "GET", "path": map[string]any{"value": "/v1"}}},
			"backendRefs": []any{map[string]any{"name": "api", "port": int64(8080), "weight": int64(1)}},
		}}},
	}}
	rules := gatewayRouteRules(route, "HTTPRoute")
	if len(rules) != 1 || len(rules[0].Backends) != 1 || rules[0].Backends[0].Namespace != "infra" || rules[0].Backends[0].Port != 8080 {
		t.Fatalf("gatewayRouteRules() = %#v", rules)
	}
}
