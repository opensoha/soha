package resource

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"errors"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	agentinfra "github.com/opensoha/soha/internal/infrastructure/agent"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestCustomResourceListReviewsBroadGrantsOnce(t *testing.T) {
	for _, readOnly := range []bool{false, true} {
		t.Run(fmt.Sprintf("readOnly=%v", readOnly), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var request struct{ Namespace, Name string }
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if request.Name != "" || request.Namespace != "platform" {
					t.Errorf("unexpected review: %+v", request)
				}
				_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"allowedActions": []string{"list", "view", "create", "update", "delete"}}})
			}))
			defer server.Close()
			connection := agentCRDConnection(server.URL)
			if readOnly {
				connection.Metadata["agent_custom_resource_rules"] = []domaincluster.AgentCustomResourceRule{{APIGroup: "example.com", Resources: []string{"widgets"}, Verbs: []string{"get", "list"}, Namespaces: []string{"platform"}}}
			}
			service := New(Dependencies{Agents: testAgentClients(agentinfra.NewRegistry(0))})
			items := make([]domainresource.CustomResourceView, 100)
			for index := range items {
				items[index] = domainresource.CustomResourceView{Namespace: "platform", Name: fmt.Sprintf("widget-%d", index)}
			}
			allowed := []string{"list", "view", "create", "update", "delete"}
			err := service.CustomResources().populateCustomResourceListActions(context.Background(), connection, crdResourceDefinition{Group: "example.com", Resource: "widgets", Namespaced: true}, items, allowed, map[string][]string{})
			if err != nil || calls != 1 {
				t.Fatalf("100 rows: calls=%d err=%v", calls, err)
			}
			want := allowed
			if readOnly {
				want = []string{"list", "view"}
			}
			for _, item := range items {
				if !slices.Equal(item.AllowedActions, want) {
					t.Fatalf("row actions=%v want=%v", item.AllowedActions, want)
				}
			}
		})
	}
}

func TestCustomResourceNamespaceAggregationRespectsCallerAndRBAC(t *testing.T) {
	for _, denied := range []bool{false, true} {
		t.Run(fmt.Sprintf("denied=%v", denied), func(t *testing.T) {
			lists := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				var request struct{ Namespace string }
				if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
					t.Error(err)
				}
				if request.Namespace != "platform" {
					t.Errorf("queried namespace outside caller scope: %q", request.Namespace)
				}
				if r.URL.Path == "/api/v1/platform/extensions/custom-resources/access" {
					actions := []string{"list"}
					if denied {
						actions = nil
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"allowedActions": actions}})
				} else {
					lists++
					_ = json.NewEncoder(w).Encode(map[string]any{"items": []domainresource.CustomResourceView{{Name: "sample", Namespace: "platform"}}})
				}
			}))
			defer server.Close()
			connection := agentCRDConnection(server.URL)
			connection.Metadata["agent_custom_resource_rules"] = []domaincluster.AgentCustomResourceRule{{APIGroup: "example.com", Resources: []string{"widgets"}, Verbs: []string{"list"}, Namespaces: []string{"platform", "hidden", "platform"}}}
			service := New(Dependencies{Agents: testAgentClients(agentinfra.NewRegistry(0)), Connections: stubConnectionResolver{connection: connection}, Authorizer: allowAllResourceAuthorizer{}})
			items, err := service.CustomResources().listGrantedCustomResourceNamespaces(context.Background(), domainidentity.Principal{}, connection, crdResourceDefinition{Group: "example.com", Resource: "widgets", Kind: "Widget", Namespaced: true}, domainaccess.Decision{ResourceScope: &domainaccess.ResourceScope{Namespaces: []string{"platform"}}})
			if denied {
				if !errors.Is(err, apperrors.ErrAccessDenied) || lists != 0 {
					t.Fatalf("RBAC denial: lists=%d err=%v", lists, err)
				}
			} else if err != nil || len(items) != 1 || lists != 1 {
				t.Fatalf("scoped aggregation: items=%v lists=%d err=%v", items, lists, err)
			}
		})
	}
}

func TestCustomResourceListPreservesNamedGrants(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var request struct{ Name string }
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		actions := []string{"list"}
		if request.Name == "permitted" {
			actions = append(actions, "view")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"allowedActions": actions}})
	}))
	defer server.Close()
	service := New(Dependencies{Agents: testAgentClients(agentinfra.NewRegistry(0))})
	items := []domainresource.CustomResourceView{{Namespace: "platform", Name: "permitted"}, {Namespace: "platform", Name: "denied"}}
	err := service.CustomResources().populateCustomResourceListActions(context.Background(), agentCRDConnection(server.URL), crdResourceDefinition{Group: "example.com", Resource: "widgets", Namespaced: true}, items, []string{"list", "view"}, map[string][]string{})
	if err != nil || calls != 3 || len(items) != 2 {
		t.Fatalf("named grants: items=%+v calls=%d err=%v", items, calls, err)
	}
	if !slices.Contains(items[0].AllowedActions, "view") || slices.Contains(items[1].AllowedActions, "view") {
		t.Fatalf("named grants: items=%+v calls=%d err=%v", items, calls, err)
	}
}
