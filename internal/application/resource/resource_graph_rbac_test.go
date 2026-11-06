package resource

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type rbacGraphTestAuthorizer func(domainaccess.Request) domainaccess.Decision

func (a rbacGraphTestAuthorizer) Authorize(_ context.Context, req domainaccess.Request) (domainaccess.Decision, error) {
	return a(req), nil
}

func rbacGraphTestNode(id, api, kind, namespace string) domainresource.ResourceGraphNode {
	return domainresource.ResourceGraphNode{ID: id, Resource: domainresource.ResourceRef{
		APIVersion: api, Kind: kind, Namespace: namespace, Name: id, ClusterID: "agent-local",
	}}
}

func rbacGraphTestService(mode domaincluster.ConnectionMode, authorizer domainaccess.Authorizer, graph domainresource.ResourceGraph) (*GenericResources, *graphViewDirectSource, *configurationGraphAgentSource) {
	direct := &graphViewDirectSource{graph: graph}
	remote := &configurationGraphAgentSource{graph: graph}
	return &GenericResources{
		resourceAccess: &resourceAccess{
			resolver:   stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a", ConnectionMode: mode}}},
			audit:      discardAuditRecorder{},
			authorizer: authorizer,
		},
		resourceGraph: direct,
		agent:         func(domaincluster.Connection) (GenericResourceAgent, error) { return remote, nil },
	}, direct, remote
}

func TestRBACResourceGraphAuthorizesEveryRootBeforeReading(t *testing.T) {
	const api = "rbac.authorization.k8s.io/v1"
	for _, tc := range []struct{ kind, api, namespace, permission string }{
		{"ServiceAccount", "v1", "team-a", "service-accounts"},
		{"Role", api, "team-a", "roles"},
		{"RoleBinding", api, "team-a", "role-bindings"},
		{"ClusterRole", api, "", "cluster-roles"},
		{"ClusterRoleBinding", api, "", "cluster-role-bindings"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			for _, mode := range []domaincluster.ConnectionMode{domaincluster.ConnectionModeDirectKubeconfig, domaincluster.ConnectionModeAgent} {
				t.Run(string(mode), func(t *testing.T) {
					graph := domainresource.ResourceGraph{RootID: "root", Nodes: []domainresource.ResourceGraphNode{rbacGraphTestNode("root", tc.api, tc.kind, tc.namespace)}}
					service, direct, remote := rbacGraphTestService(mode, rbacGraphTestAuthorizer(func(req domainaccess.Request) domainaccess.Decision {
						if req.Action != domainaccess.ActionView || req.PermissionKey != "platform.access-control."+tc.permission+".view" ||
							req.Namespace.Namespace != tc.namespace || req.Resource.Kind != tc.kind || req.Resource.Group != "access-control" {
							t.Fatalf("unexpected authorization request: %+v", req)
						}
						// A different namespace grants neither a named namespace
						// nor any cluster-scoped RBAC root.
						return domainaccess.Decision{Allowed: true, ResourceScope: &domainaccess.ResourceScope{Namespaces: []string{"other"}}}
					}), graph)
					_, err := service.GetResourceGraph(context.Background(), domainidentity.Principal{}, "cluster-a", tc.namespace, tc.kind, "root")
					if !errors.Is(err, apperrors.ErrAccessDenied) || direct.called || remote.called {
						t.Fatalf("denied root reached a collector: %v direct=%t remote=%t", err, direct.called, remote.called)
					}
					invalidScope := "wrong"
					if tc.namespace != "" {
						invalidScope = ""
					}
					_, err = service.GetResourceGraph(context.Background(), domainidentity.Principal{}, "cluster-a", invalidScope, tc.kind, "root")
					if !errors.Is(err, apperrors.ErrInvalidArgument) || direct.called || remote.called {
						t.Fatalf("invalid scope reached a collector: %v", err)
					}
				})
			}
		})
	}
}

func TestRBACResourceGraphFiltersHiddenReferencesAndDisconnectedResources(t *testing.T) {
	const api = "rbac.authorization.k8s.io/v1"
	for _, mode := range []domaincluster.ConnectionMode{domaincluster.ConnectionModeDirectKubeconfig, domaincluster.ConnectionModeAgent} {
		t.Run(string(mode), func(t *testing.T) {
			root := rbacGraphTestNode("root", api, "ClusterRole", "")
			binding := rbacGraphTestNode("visible-binding", api, "RoleBinding", "team-a")
			hidden := rbacGraphTestNode("hidden-binding", api, "RoleBinding", "hidden-team")
			graph := domainresource.ResourceGraph{
				RootID: "root", Namespace: "hidden-team",
				Nodes: []domainresource.ResourceGraphNode{
					root, binding, hidden,
					rbacGraphTestNode("visible-account", "v1", "ServiceAccount", "team-a"),
					rbacGraphTestNode("aggregate-match", api, "ClusterRole", ""),
					rbacGraphTestNode("detached-account", "v1", "ServiceAccount", "hidden-team"),
					rbacGraphTestNode("custom-binding", "example.test/v1", "RoleBinding", "team-a"),
					rbacGraphTestNode("wrong-scope", api, "ClusterRoleBinding", "team-a"),
					rbacGraphTestNode("blocked-source", api, "ClusterRole", ""),
				},
				Edges: []domainresource.ResourceGraphEdge{
					{ID: "role", SourceID: binding.ID, TargetID: root.ID, Relation: "binds-role", SourceRef: &binding.Resource},
					{ID: "subject", SourceID: binding.ID, TargetID: "visible-account", Relation: "binds-subject", FieldPath: "subjects[21]", Note: "hidden-binding"},
					{ID: "selector", SourceID: root.ID, TargetID: "aggregate-match", Relation: "aggregates-role", FieldPath: "aggregationRule.clusterRoleSelectors[4]", ResolutionState: "resolved"},
					{ID: "hidden-edge", SourceID: hidden.ID, TargetID: root.ID, Relation: "binds-role"},
					{ID: "hidden-path", SourceID: hidden.ID, TargetID: "detached-account", Relation: "binds-subject"},
					{ID: "custom-edge", SourceID: "custom-binding", TargetID: root.ID, Relation: "binds-role"},
					{ID: "wrong-scope-edge", SourceID: "wrong-scope", TargetID: root.ID, Relation: "binds-role"},
					{ID: "unauthorized-source", SourceID: root.ID, TargetID: "blocked-source", Relation: "aggregates-role", SourceRef: &hidden.Resource},
					{ID: "effective", SourceID: binding.ID, TargetID: root.ID, Relation: "effective-rights"},
				},
				Evidence: []domainresource.ResourceEvidence{
					{ResourceID: hidden.ID, Summary: "hidden-binding"},
					{ResourceID: binding.ID, SourceRef: "RoleBinding/hidden-team/hidden-binding"},
					{Summary: "unscoped hidden-binding"},
				},
				Warnings: []string{"provider detail: hidden-binding, subjects[21]"},
			}
			authorizer := rbacGraphTestAuthorizer(func(req domainaccess.Request) domainaccess.Decision {
				if req.Action != domainaccess.ActionView {
					t.Fatalf("unexpected action: %s", req.Action)
				}
				switch req.PermissionKey {
				case "platform.access-control.role-bindings.view":
					return domainaccess.Decision{Allowed: true, ResourceScope: &domainaccess.ResourceScope{Namespaces: []string{"team-a"}}}
				case "platform.access-control.cluster-roles.view", "platform.access-control.cluster-role-bindings.view", "platform.access-control.service-accounts.view":
					return domainaccess.Decision{Allowed: true}
				}
				t.Fatalf("unexpected authorization request: %+v", req)
				return domainaccess.Decision{}
			})
			service, direct, remote := rbacGraphTestService(mode, authorizer, graph)
			got, err := service.GetResourceGraph(context.Background(), domainidentity.Principal{}, "cluster-a", "", "ClusterRole", "root")
			if err != nil || got.ClusterID != "cluster-a" || got.Namespace != "" || len(got.Nodes) != 4 || len(got.Edges) != 3 || len(got.Evidence) != 0 || !slices.Equal(got.Warnings, []string{graphRBACPartial}) {
				t.Fatalf("unexpected filtered graph: %+v %v", got, err)
			}
			if direct.called != (mode == domaincluster.ConnectionModeDirectKubeconfig) || remote.called != (mode == domaincluster.ConnectionModeAgent) {
				t.Fatal("wrong graph source")
			}
			if got.Edges[0].SourceRef == nil || got.Edges[0].SourceRef.ClusterID != "cluster-a" ||
				got.Edges[1].FieldPath != "subjects" || got.Edges[2].FieldPath != "aggregationRule.clusterRoleSelectors" || got.Edges[2].ResolutionState != "resolved" {
				t.Fatalf("wrong declaration evidence: %+v", got.Edges)
			}
			body, err := json.Marshal(got)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{"hidden-binding", "hidden-team", "detached-account", "custom-binding", "wrong-scope", "blocked-source", "effective-rights", "subjects[21]", "agent-local"} {
				if strings.Contains(string(body), secret) {
					t.Errorf("filtered identifier %q appears in graph: %s", secret, body)
				}
			}
		})
	}
}

func TestRBACResourceGraphUsesNamespaceLabelProof(t *testing.T) {
	const api = "rbac.authorization.k8s.io/v1"
	for _, tc := range []struct {
		name      string
		projected bool
		wantAllow bool
	}{
		{"authorized projection", true, true},
		{"missing namespace projection", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			graph := domainresource.ResourceGraph{RootID: "root", Nodes: []domainresource.ResourceGraphNode{rbacGraphTestNode("root", api, "Role", "team-a")}}
			authorizer := rbacGraphTestAuthorizer(func(req domainaccess.Request) domainaccess.Decision {
				if req.Resource.Kind != "Role" {
					return domainaccess.Decision{Allowed: true}
				}
				if req.Namespace.Labels["team"] != "a" {
					return domainaccess.Decision{Reason: "namespace selector requires observed labels", ReasonCode: domainaccess.ReasonNamespaceLabelsRequired}
				}
				scope := &domainaccess.ResourceScope{LabelSelector: "team=a", NamespaceSelectors: []string{"team=a"}}
				if tc.projected {
					scope.Namespaces = []string{"team-a"}
				}
				return domainaccess.Decision{Allowed: true, ResourceScope: scope}
			})
			service, direct, _ := rbacGraphTestService(domaincluster.ConnectionModeDirectKubeconfig, authorizer, graph)
			labels := &namespaceLabelResolverStub{labels: map[string]string{"team": "a"}}
			service.namespaceLabels = labels
			result, err := service.GetResourceGraph(context.Background(), domainidentity.Principal{}, "cluster-a", "team-a", "Role", "root")
			if tc.wantAllow && (err != nil || len(result.Nodes) != 1 || len(result.Warnings) != 0) {
				t.Fatalf("proven named scope rejected: %+v %v", result, err)
			}
			if !tc.wantAllow && !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("scope without named projection accepted: %+v %v", result, err)
			}
			if labels.calls != 1 || direct.called != tc.wantAllow {
				t.Fatalf("namespace proof read=%d source called=%t", labels.calls, direct.called)
			}
		})
	}
}

func TestRBACResourceGraphEmptyRestrictedSourcesAndMismatchedRoot(t *testing.T) {
	const api = "rbac.authorization.k8s.io/v1"
	for _, observed := range []string{api, "example.test/v1"} {
		graph := domainresource.ResourceGraph{RootID: "root", Nodes: []domainresource.ResourceGraphNode{rbacGraphTestNode("root", observed, "Role", "team-a")}}
		authorizer := exactPermissionAuthorizer{"platform.access-control.roles.view": true}
		service, _, _ := rbacGraphTestService(domaincluster.ConnectionModeDirectKubeconfig, authorizer, graph)
		result, err := service.GetResourceGraph(context.Background(), domainidentity.Principal{}, "cluster-a", "team-a", "Role", "root")
		if observed != api {
			if !errors.Is(err, apperrors.ErrClusterUnready) {
				t.Fatalf("same-kind custom root accepted: %+v %v", result, err)
			}
		} else if err != nil || len(result.Nodes) != 1 || !slices.Equal(result.Warnings, []string{graphRBACPartial}) {
			t.Fatalf("empty restricted sources must be partial: %+v %v", result, err)
		}
	}
}
