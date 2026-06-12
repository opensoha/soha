package resource

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	agentinfra "github.com/opensoha/soha/internal/infrastructure/agent"
)

func TestAgentCustomResourceOperationsResolveCRDThroughAgent(t *testing.T) {
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/platform/extensions/crds":
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{
				{
					"name":     "widgets.example.com",
					"group":    "example.com",
					"scope":    "Namespaced",
					"kind":     "Widget",
					"plural":   "widgets",
					"version":  "v1",
					"versions": []string{"v1"},
				},
			}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/platform/extensions/custom-resources/list":
			var req struct {
				Definition domainresource.CRDResourceDefinition `json:"definition"`
				Namespace  string                               `json:"namespace"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode list request: %v", err)
			}
			if req.Definition.Kind != "Widget" || req.Namespace != "platform" {
				t.Fatalf("unexpected list request: %#v", req)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []map[string]any{
				{"apiVersion": "example.com/v1", "kind": "Widget", "name": "sample", "namespace": "platform"},
			}})
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/platform/extensions/custom-resources/yaml":
			var req struct {
				Definition domainresource.CRDResourceDefinition `json:"definition"`
				Namespace  string                               `json:"namespace"`
				Name       string                               `json:"name"`
				Content    string                               `json:"content"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode apply request: %v", err)
			}
			if req.Name != "sample" || req.Definition.Resource != "widgets" {
				t.Fatalf("unexpected apply request: %#v", req)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
				"kind":      "Widget",
				"name":      "sample",
				"namespace": "platform",
				"content":   req.Content,
			}})
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/platform/extensions/custom-resources":
			var req struct {
				Definition domainresource.CRDResourceDefinition `json:"definition"`
				Namespace  string                               `json:"namespace"`
				Name       string                               `json:"name"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode delete request: %v", err)
			}
			if req.Name != "sample" || req.Definition.Kind != "Widget" {
				t.Fatalf("unexpected delete request: %#v", req)
			}
			w.WriteHeader(http.StatusOK)
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	service := &Service{
		agents:     agentinfra.NewRegistry(0),
		resolver:   stubConnectionResolver{connection: agentCRDConnection(server.URL)},
		authorizer: allowAllResourceAuthorizer{},
		audit:      noopResourceAuditRecorder{},
	}
	principal := domainidentity.Principal{UserID: "user-1"}

	items, err := service.ListCRDResources(context.Background(), principal, "agent-cluster", "widgets.example.com", "platform")
	if err != nil {
		t.Fatalf("ListCRDResources() error = %v", err)
	}
	if len(items) != 1 || items[0].Name != "sample" || items[0].Kind != "Widget" {
		t.Fatalf("items = %#v, want sample widget", items)
	}

	if _, err := service.ApplyCRDResourceYAML(context.Background(), principal, "agent-cluster", "widgets.example.com", "platform", "sample", `
apiVersion: example.com/v1
kind: Widget
metadata:
  name: sample
  namespace: platform
`); err != nil {
		t.Fatalf("ApplyCRDResourceYAML() error = %v", err)
	}
	if err := service.DeleteCRDResource(context.Background(), principal, "agent-cluster", "widgets.example.com", "platform", "sample"); err != nil {
		t.Fatalf("DeleteCRDResource() error = %v", err)
	}
	if len(seen) != 6 {
		t.Fatalf("request count = %d, want 6: %#v", len(seen), seen)
	}
}

type noopResourceAuditRecorder struct{}

func (noopResourceAuditRecorder) Record(context.Context, domainaudit.Entry) error {
	return nil
}

func agentCRDConnection(endpoint string) domaincluster.Connection {
	return domaincluster.Connection{
		Summary: domaincluster.Summary{
			ID:             "agent-cluster",
			ConnectionMode: domaincluster.ConnectionModeAgent,
		},
		Metadata: map[string]any{
			"endpoint": endpoint,
		},
	}
}
