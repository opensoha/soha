package kubernetes

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
)

func TestGetHTTPRouteDetailIncludesBackendRelationships(t *testing.T) {
	t.Parallel()
	route := &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "gateway.networking.k8s.io/v1",
		"kind":       "HTTPRoute",
		"metadata": map[string]any{
			"name": "api", "namespace": "apps", "labels": map[string]any{"app": "api"},
		},
		"spec": map[string]any{
			"hostnames": []any{"api.example.com"},
			"rules": []any{map[string]any{
				"backendRefs": []any{map[string]any{"name": "api", "port": int64(8080)}},
			}},
		},
		"status": map[string]any{"parents": []any{map[string]any{
			"conditions": []any{map[string]any{"type": "Accepted", "status": "True"}},
		}}},
	}}
	client := &Client{
		dynamic: dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), route),
	}

	detail, err := client.GetHTTPRouteDetail(context.Background(), "apps", "api")
	if err != nil {
		t.Fatalf("GetHTTPRouteDetail() error = %v", err)
	}
	if detail.Labels["app"] != "api" || len(detail.Conditions) != 1 || len(detail.Rules) != 1 {
		t.Fatalf("GetHTTPRouteDetail() = %#v", detail)
	}
	backend := detail.Rules[0].Backends[0]
	if backend.Kind != "Service" || backend.Namespace != "apps" || backend.Name != "api" || backend.Port != 8080 {
		t.Fatalf("backend = %#v", backend)
	}
}

func TestGatewayDetailMappersPreserveStructuredFields(t *testing.T) {
	t.Parallel()
	item := unstructured.Unstructured{Object: map[string]any{
		"metadata": map[string]any{"name": "edge", "namespace": "infra"},
		"spec": map[string]any{
			"listeners": []any{map[string]any{
				"name": "https", "protocol": "HTTPS", "port": int64(443), "hostname": "example.com",
				"tls": map[string]any{
					"mode": "Terminate", "certificateRefs": []any{map[string]any{"kind": "Secret", "name": "edge-tls"}},
				},
			}},
			"from": []any{map[string]any{"group": "gateway.networking.k8s.io", "kind": "HTTPRoute", "namespace": "apps"}},
			"to":   []any{map[string]any{"group": "", "kind": "Service", "name": "api"}},
		},
		"status": map[string]any{"listeners": []any{map[string]any{"name": "https", "attachedRoutes": int64(2)}}},
	}}

	listeners := gatewayListenerViews(item)
	fromRefs := gatewayReferenceGrantFromViews(item)
	toRefs := gatewayReferenceGrantToViews(item)
	if len(listeners) != 1 || listeners[0].AttachedRoutes != 2 || listeners[0].CertificateRefs[0] != "infra:Secret/edge-tls" {
		t.Fatalf("gatewayListenerViews() = %#v", listeners)
	}
	if len(fromRefs) != 1 || fromRefs[0].Namespace != "apps" || len(toRefs) != 1 || toRefs[0].Name != "api" {
		t.Fatalf("reference grant refs = from %#v, to %#v", fromRefs, toRefs)
	}
}
