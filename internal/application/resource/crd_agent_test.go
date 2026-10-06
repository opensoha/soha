package resource

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	agentinfra "github.com/opensoha/soha/internal/infrastructure/agent"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type observedCRDDefinitionDirect struct {
	DirectCustomResource
	target string
}

func (d *observedCRDDefinitionDirect) DeleteCRDDefinition(_ context.Context, cluster, name, uid string) error {
	d.target = cluster + "/" + name + "/" + uid
	return nil
}

func TestCRDDefinitionDeletionRequiresExactPermissionBeforeDirectCall(t *testing.T) {
	for _, permitted := range []bool{false, true} {
		direct := &observedCRDDefinitionDirect{}
		service := New(Dependencies{DirectCustom: direct, Connections: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "direct-cluster", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}}}, Authorizer: exactPermissionAuthorizer{"platform.extensions.crds.delete": permitted, "platform.extensions.custom-resources.delete": true}, Audit: noopResourceAuditRecorder{}})
		err := service.CustomResources().DeleteCRDDefinition(context.Background(), domainidentity.Principal{}, "direct-cluster", "widgets.example.com", "observed-uid")
		if permitted {
			if err != nil || direct.target != "direct-cluster/widgets.example.com/observed-uid" {
				t.Fatalf("err=%v target=%s", err, direct.target)
			}
		} else if !errors.Is(err, apperrors.ErrAccessDenied) || direct.target != "" {
			t.Fatalf("unauthorized definition deletion: err=%v target=%s", err, direct.target)
		}
	}
}

func TestCRDCatalogIntersectsAgentAndUserActions(t *testing.T) {
	items := []domainresource.CRDView{{AllowedActions: []string{"list", "view", "delete"}}}
	populateAllowedActionsCRDs(items, domainaccess.Decision{AllowedActions: []domainaccess.Action{domainaccess.ActionList, domainaccess.ActionView}})
	if len(items[0].AllowedActions) != 2 {
		t.Fatalf("Agent action widened user permissions: %+v", items)
	}
}

func TestAgentCustomResourceGrantAndRuntimePermissionsIntersect(t *testing.T) {
	for _, tc := range []struct {
		name      string
		saved     bool
		runtime   []string
		wantCalls int
	}{
		{"revoked saved grant", false, []string{"update"}, 0},
		{"Kubernetes denies update", true, []string{"list", "view"}, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/api/v1/platform/extensions/custom-resources/access" {
					t.Errorf("unauthorized mutation reached Agent: %s", r.URL.Path)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"allowedActions": tc.runtime}})
			}))
			defer server.Close()
			connection := agentCRDConnection(server.URL)
			if !tc.saved {
				delete(connection.Metadata, "agent_custom_resource_rules")
			}
			service := New(Dependencies{Agents: testAgentClients(agentinfra.NewRegistry(0))})
			definition := crdResourceDefinition{Group: "example.com", Resource: "widgets", Kind: "Widget", Version: "v1", Namespaced: true}
			err := service.CustomResources().requireCustomResourceAction(context.Background(), connection, definition, "platform", "sample", "update")
			if !errors.Is(err, apperrors.ErrAccessDenied) || calls != tc.wantCalls {
				t.Fatalf("err=%v calls=%d", err, calls)
			}
		})
	}
}

func TestAgentCustomResourceOperationsResolveCRDThroughAgent(t *testing.T) {
	var seen []string
	server := newAgentCRDTestServer(t, &seen)
	defer server.Close()

	service := New(Dependencies{
		Agents:      testAgentClients(agentinfra.NewRegistry(0)),
		Connections: stubConnectionResolver{connection: agentCRDConnection(server.URL)},
		Authorizer:  allowAllResourceAuthorizer{},
		Audit:       noopResourceAuditRecorder{},
	})
	principal := domainidentity.Principal{UserID: "user-1"}

	items, err := service.CustomResources().ListCRDResources(context.Background(), principal, "agent-cluster", "widgets.example.com", "platform")
	if err != nil {
		t.Fatalf("ListCRDResources() error = %v", err)
	}
	if len(items) != 1 || items[0].Name != "sample" || items[0].Kind != "Widget" {
		t.Fatalf("items = %#v, want sample widget", items)
	}

	if _, err := service.CustomResources().ApplyCRDResourceYAML(context.Background(), principal, "agent-cluster", "widgets.example.com", "platform", "sample", `
apiVersion: example.com/v1
kind: Widget
metadata:
  name: sample
  namespace: platform
`); err != nil {
		t.Fatalf("ApplyCRDResourceYAML() error = %v", err)
	}
	if err := service.CustomResources().DeleteCRDResource(context.Background(), principal, "agent-cluster", "widgets.example.com", "platform", "sample", ""); err != nil {
		t.Fatalf("DeleteCRDResource() error = %v", err)
	}
	if len(seen) != 9 {
		t.Fatalf("request count = %d, want 9: %#v", len(seen), seen)
	}
}

func TestAgentCRDDefinitionDeletionForwardsIdentityAndErrors(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusForbidden, http.StatusConflict} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/platform/extensions/crds/widgets.example.com" || r.URL.Query().Get("expectedUid") != "observed-uid" {
					t.Errorf("incorrect deletion target: %s %s", r.Method, r.URL)
				}
				w.WriteHeader(status)
			}))
			defer server.Close()
			service := New(Dependencies{Agents: testAgentClients(agentinfra.NewRegistry(0)), Connections: stubConnectionResolver{connection: agentCRDConnection(server.URL)}, Authorizer: allowAllResourceAuthorizer{}, Audit: noopResourceAuditRecorder{}})
			principal := domainidentity.Principal{UserID: "user-1"}
			if err := service.CustomResources().DeleteCRDDefinition(context.Background(), principal, "agent-cluster", "widgets.example.com", ""); !errors.Is(err, apperrors.ErrInvalidArgument) || calls != 0 {
				t.Fatal("missing identity reached Agent")
			}
			err := service.CustomResources().DeleteCRDDefinition(context.Background(), principal, "agent-cluster", "widgets.example.com", "observed-uid")
			if (err == nil) != (status == http.StatusNoContent) || calls != 1 {
				t.Fatalf("status=%d err=%v calls=%d", status, err, calls)
			}
		})
	}
}

func TestAgentCustomResourceAllNamespacesUsesScopedGrant(t *testing.T) {
	var seen []string
	server := newAgentCRDTestServer(t, &seen)
	defer server.Close()
	service := New(Dependencies{
		Agents:      testAgentClients(agentinfra.NewRegistry(0)),
		Connections: stubConnectionResolver{connection: agentCRDConnection(server.URL)},
		Authorizer:  allowAllResourceAuthorizer{},
		Audit:       noopResourceAuditRecorder{},
	})
	items, err := service.CustomResources().ListCRDResources(context.Background(), domainidentity.Principal{UserID: "user-1"}, "agent-cluster", "widgets.example.com", "")
	if err != nil || len(items) != 1 || items[0].Namespace != "platform" || len(seen) != 3 {
		t.Fatalf("scoped all-namespace list: items=%+v requests=%v err=%v", items, seen, err)
	}
}

func newAgentCRDTestServer(t *testing.T, seen *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.Method+" "+r.URL.Path)
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
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/platform/extensions/custom-resources/access":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"allowedActions": []string{"list", "view", "create", "update", "delete"}}})
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
		case r.Method == http.MethodPut && r.URL.Path == "/api/v1/platform/ownership-v2/extensions/custom-resources/yaml":
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
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/platform/ownership-v2/extensions/custom-resources/delete-observed":
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
			"endpoint":                    endpoint,
			"agent_custom_resource_rules": []domaincluster.AgentCustomResourceRule{{APIGroup: "example.com", Resources: []string{"widgets"}, Verbs: []string{"get", "list", "create", "update", "delete"}, Namespaces: []string{"platform"}}},
		},
	}
}
