package resource

import (
	"encoding/json"
	"testing"
)

func TestGatewayListViewsStayLightweight(t *testing.T) {
	views := []any{
		GatewayClassView{},
		GatewayView{},
		HTTPRouteView{},
		GRPCRouteView{},
		BackendTLSPolicyView{},
		ReferenceGrantView{},
		IngressClassView{},
	}
	for _, view := range views {
		raw, err := json.Marshal(view)
		if err != nil {
			t.Fatal(err)
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"labels", "annotations", "conditions", "listeners", "routes", "rules", "gateways", "ingresses", "fromRefs", "toRefs"} {
			if _, ok := payload[key]; ok {
				t.Errorf("%T list response contains detail-only field %q", view, key)
			}
		}
	}
}

func TestGatewayDetailViewsExposeRelationships(t *testing.T) {
	view := GatewayDetailView{
		GatewayView: GatewayView{Name: "edge", Namespace: "default"},
		Labels:      map[string]string{"app": "edge"},
		Listeners:   []GatewayListenerView{{Name: "https", Protocol: "HTTPS", Port: 443}},
		Routes:      []GatewayRouteReferenceView{{Kind: "HTTPRoute", Name: "api"}},
	}
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"name", "namespace", "labels", "listeners", "routes"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("gateway detail response omits %q", key)
		}
	}
}
