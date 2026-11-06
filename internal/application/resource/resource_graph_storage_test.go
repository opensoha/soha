package resource

import (
	"context"
	"errors"
	"testing"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type storageGraphAuthorizer struct {
	decide func(domainaccess.Request) domainaccess.Decision
}

func (a storageGraphAuthorizer) Authorize(_ context.Context, req domainaccess.Request) (domainaccess.Decision, error) {
	return a.decide(req), nil
}

func TestStorageResourceGraphFiltersScopesAndDisconnectedPaths(t *testing.T) {
	for _, mode := range []domaincluster.ConnectionMode{domaincluster.ConnectionModeDirectKubeconfig, domaincluster.ConnectionModeAgent} {
		t.Run(string(mode), func(t *testing.T) {
			node := func(id, api, kind, ns string) domainresource.ResourceGraphNode {
				return domainresource.ResourceGraphNode{ID: id, Resource: domainresource.ResourceRef{APIVersion: api, Kind: kind, Namespace: ns, Name: id}}
			}
			graph := domainresource.ResourceGraph{
				RootID: "class",
				Nodes: []domainresource.ResourceGraphNode{
					node("class", "storage.k8s.io/v1", "StorageClass", ""),
					node("allowed-claim", "v1", "PersistentVolumeClaim", "team-a"),
					node("allowed-pod", "v1", "Pod", "team-a"),
					node("excluded-claim", "v1", "PersistentVolumeClaim", "team-b"),
					node("visible-but-detached-pod", "v1", "Pod", "team-b"),
					node("restricted-volume", "v1", "PersistentVolume", ""),
					node("custom-volume", "example.test/v1", "PersistentVolume", ""),
				},
				Edges: []domainresource.ResourceGraphEdge{
					{ID: "allowed-class", SourceID: "allowed-claim", TargetID: "class", Relation: "uses-storage-class"},
					{ID: "allowed-mount", SourceID: "allowed-pod", TargetID: "allowed-claim", Relation: "mounts"},
					{ID: "excluded-class", SourceID: "excluded-claim", TargetID: "class", Relation: "uses-storage-class"},
					{ID: "excluded-mount", SourceID: "visible-but-detached-pod", TargetID: "excluded-claim", Relation: "mounts"},
					{ID: "restricted-class", SourceID: "restricted-volume", TargetID: "class", Relation: "uses-storage-class"},
					{ID: "custom-class", SourceID: "custom-volume", TargetID: "class", Relation: "uses-storage-class"},
				},
				Evidence: []domainresource.ResourceEvidence{{ID: "hidden-evidence", ResourceID: "excluded-claim", Summary: "private claim"}},
				Warnings: []string{"PersistentVolume reference source NotFound"},
			}
			direct := &graphViewDirectSource{graph: graph}
			agent := &configurationGraphAgentSource{graph: graph}
			service := &GenericResources{
				resourceAccess: &resourceAccess{
					audit:    discardAuditRecorder{},
					resolver: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a", ConnectionMode: mode}}},
					authorizer: storageGraphAuthorizer{decide: func(req domainaccess.Request) domainaccess.Decision {
						if req.Action != domainaccess.ActionView {
							t.Errorf("unexpected action %s", req.Action)
						}
						switch req.PermissionKey {
						case "platform.storage.storage-classes.view", "platform.pods.view":
							return domainaccess.Decision{Allowed: true}
						case "platform.storage.persistent-volumes.view":
							return domainaccess.Decision{Allowed: true, ResourceScope: &domainaccess.ResourceScope{Namespaces: []string{"team-a"}}}
						case "platform.storage.persistent-volume-claims.view":
							return domainaccess.Decision{Allowed: true, ResourceScope: &domainaccess.ResourceScope{ExcludedNamespaces: []string{"team-b"}}}
						}
						t.Errorf("unexpected permission %s", req.PermissionKey)
						return domainaccess.Decision{}
					}},
				},
				resourceGraph: direct,
				agent:         func(domaincluster.Connection) (GenericResourceAgent, error) { return agent, nil },
			}
			got, err := service.GetResourceGraph(context.Background(), domainidentity.Principal{}, "cluster-a", "", "StorageClass", "class")
			if err != nil {
				t.Fatal(err)
			}
			assertStorageGraphVisiblePaths(t, got)
			if direct.called != (mode == domaincluster.ConnectionModeDirectKubeconfig) || agent.called != (mode == domaincluster.ConnectionModeAgent) {
				t.Fatal("wrong graph source")
			}
			_, err = service.GetResourceGraph(context.Background(), domainidentity.Principal{}, "cluster-a", "", "PersistentVolume", "restricted-volume")
			if !errors.Is(err, apperrors.ErrAccessDenied) {
				t.Fatalf("namespace-only grant read a cluster root: %v", err)
			}
			_, err = service.GetResourceGraph(context.Background(), domainidentity.Principal{}, "cluster-a", "wrong", "StorageClass", "class")
			if !errors.Is(err, apperrors.ErrInvalidArgument) {
				t.Fatalf("cluster root accepted a namespace: %v", err)
			}
			_, err = service.GetResourceGraph(context.Background(), domainidentity.Principal{}, "cluster-a", "", "StorageClass", "different")
			if !errors.Is(err, apperrors.ErrClusterUnready) {
				t.Fatalf("mismatched observation accepted: %v", err)
			}
		})
	}
}

func assertStorageGraphVisiblePaths(t *testing.T, graph domainresource.ResourceGraph) {
	t.Helper()
	if len(graph.Nodes) != 3 || len(graph.Edges) != 2 || len(graph.Evidence) != 0 || len(graph.Warnings) != 1 || graph.Warnings[0] != graphStorageAccessWarning {
		t.Fatalf("unexpected visible graph: %+v", graph)
	}
	for _, item := range graph.Nodes {
		if item.ID != "class" && item.ID != "allowed-claim" && item.ID != "allowed-pod" {
			t.Errorf("unexpected resource returned: %s", item.ID)
		}
	}
}

func TestStorageGraphNamespaceSelectorProjection(t *testing.T) {
	for _, tc := range []struct {
		name  string
		scope domainaccess.ResourceScope
		allow bool
	}{
		{"resolved allow selector", domainaccess.ResourceScope{Namespaces: []string{"team-a"}, NamespaceSelectors: []string{"team=a"}, LabelSelector: "team=a"}, true},
		{"unresolved allow selector", domainaccess.ResourceScope{NamespaceSelectors: []string{"team=a"}, LabelSelector: "team=a"}, false},
		{"different namespace", domainaccess.ResourceScope{Namespaces: []string{"team-b"}, NamespaceSelectors: []string{"team=a"}, LabelSelector: "team=a"}, false},
		{"unresolved resource selector", domainaccess.ResourceScope{Namespaces: []string{"team-a"}, LabelSelector: "app=public"}, false},
		{"evaluated nonmatching deny selector", domainaccess.ResourceScope{Namespaces: []string{"team-a"}, ExcludedNamespaceSelectors: []string{"private=true"}}, true},
		{"unresolved deny selector", domainaccess.ResourceScope{ExcludedNamespaceSelectors: []string{"private=true"}}, false},
		{"excluded namespace", domainaccess.ResourceScope{Namespaces: []string{"team-a"}, ExcludedNamespaces: []string{"team-a"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := graphNamedReferenceScopeAllowed("team-a", domainaccess.Decision{Allowed: true, ResourceScope: &tc.scope}); got != tc.allow {
				t.Fatalf("allowed = %t, want %t", got, tc.allow)
			}
			if graphNamedReferenceScopeAllowed("team-a", domainaccess.Decision{Allowed: false, ResourceScope: &tc.scope}) {
				t.Fatal("denied decision was accepted")
			}
		})
	}
}

func TestStorageClassWarningsDoNotRevealInvisibleSourceTruncation(t *testing.T) {
	for _, warnings := range [][]string{nil, {"PersistentVolumeClaim source truncated at 500 objects"}} {
		direct := &graphViewDirectSource{graph: domainresource.ResourceGraph{
			RootID: "class",
			Nodes: []domainresource.ResourceGraphNode{{ID: "class", Resource: domainresource.ResourceRef{
				APIVersion: "storage.k8s.io/v1", Kind: "StorageClass", Name: "class",
			}}},
			Warnings: warnings,
		}}
		service := &GenericResources{
			resourceAccess: &resourceAccess{
				audit:      discardAuditRecorder{},
				resolver:   stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a", ConnectionMode: domaincluster.ConnectionModeDirectKubeconfig}}},
				authorizer: exactPermissionAuthorizer{"platform.storage.storage-classes.view": true},
			},
			resourceGraph: direct,
		}
		graph, err := service.GetResourceGraph(context.Background(), domainidentity.Principal{}, "cluster-a", "", "StorageClass", "class")
		if err != nil || len(graph.Warnings) != 1 || graph.Warnings[0] != graphStorageAccessWarning {
			t.Fatalf("invisible source truncation changed the response: %+v %v", graph.Warnings, err)
		}
	}
}
