package resource

import (
	"encoding/json"
	"testing"
)

func TestNetworkTopologyViewJSONOmitsAggregateInputs(t *testing.T) {
	raw, err := json.Marshal(NetworkTopologyView{
		ClusterID:   "cluster-a",
		Source:      "backend-aggregate",
		GeneratedAt: "2026-07-18T00:00:00Z",
	})
	if err != nil {
		t.Fatalf("marshal topology: %v", err)
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatalf("unmarshal topology: %v", err)
	}
	for _, key := range []string{"services", "ingresses", "httpRoutes", "gateways", "pods"} {
		if _, ok := payload[key]; ok {
			t.Errorf("topology response contains aggregate input %q: %s", key, raw)
		}
	}
}

func TestLightweightListViewsOmitDetailOnlyFields(t *testing.T) {
	views := []struct {
		name    string
		value   any
		omitted []string
	}{
		{"configmap", ConfigMapView{}, []string{"binaryEntries", "immutable"}},
		{"serviceaccount", ServiceAccountView{}, []string{"secrets", "imagePullSecrets", "automountServiceAccountToken"}},
		{"role", RoleView{}, []string{"rules"}},
		{"rolebinding", RoleBindingView{}, []string{"subjects"}},
		{"clusterrole", ClusterRoleView{}, []string{"rules", "aggregationRules"}},
		{"clusterrolebinding", ClusterRoleBindingView{}, []string{"subjects"}},
		{"storageclass", StorageClassView{}, []string{"parameters"}},
	}
	for _, view := range views {
		t.Run(view.name, func(t *testing.T) {
			raw, err := json.Marshal(view.value)
			if err != nil {
				t.Fatalf("marshal %s: %v", view.name, err)
			}
			var payload map[string]json.RawMessage
			if err := json.Unmarshal(raw, &payload); err != nil {
				t.Fatalf("unmarshal %s: %v", view.name, err)
			}
			for _, key := range view.omitted {
				if _, ok := payload[key]; ok {
					t.Errorf("%s list response contains detail-only field %q", view.name, key)
				}
			}
		})
	}
}

func TestAdmissionWebhookDetailDoesNotExposeCABundle(t *testing.T) {
	raw, err := json.Marshal(AdmissionWebhookConfigurationDetailView{
		Webhooks: []AdmissionWebhookView{{Name: "hook.example", CABundleConfigured: true}},
	})
	if err != nil {
		t.Fatalf("marshal webhook detail: %v", err)
	}
	if string(raw) != `{"name":"","ageSeconds":0,"webhooks":[{"name":"hook.example","clientTarget":"","caBundleConfigured":true}]}` {
		t.Fatalf("unexpected webhook detail JSON: %s", raw)
	}
}
