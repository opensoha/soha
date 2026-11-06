package resource

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	cr "github.com/opensoha/soha-contracts/resource"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

type explicitGraphDirect struct {
	graphCRDDirect
	page      cr.CustomResourceGraphPage
	pageError error
	pageCalls int
}

func (d *explicitGraphDirect) ListResourceGraphCRDs(ctx context.Context, cluster string) ([]cr.CRDView, error) {
	return d.ListCRDs(ctx, cluster)
}

func (d *explicitGraphDirect) ReadCustomResourceGraphPage(_ context.Context, _ string, _ cr.CRDResourceDefinition, _ string) (cr.CustomResourceGraphPage, error) {
	d.pageCalls++
	return d.page, d.pageError
}

func newExplicitGraphService(view cr.CRDView, permissions domainaccess.Authorizer) (*GenericResources, *explicitGraphDirect) {
	state := &graphCRDObservation{views: []cr.CRDView{view}}
	g := graphCRDService(domaincluster.ConnectionModeDirectKubeconfig, cr.ResourceGraph{}, state, permissions, false)
	direct := &explicitGraphDirect{graphCRDDirect: graphCRDDirect{s: state}}
	g.customResources.direct = direct
	return g, direct
}

func TestExplicitResourceGraphCRDDefinitionFiltersOwnerAndSpec(t *testing.T) {
	service, direct := newExplicitGraphService(graphCRDView("Widget"), exactPermissionAuthorizer{appaccess.PermPlatformExtensionsView: true})
	direct.page = cr.CustomResourceGraphPage{Truncated: true, Items: []json.RawMessage{
		json.RawMessage(`{"apiVersion":"example.io/v1","kind":"Widget","metadata":{"name":"sample","namespace":"team","uid":"observed","ownerReferences":[{"apiVersion":"v1","kind":"Pod","name":"hidden-owner","uid":"private-owner-uid"}]},"spec":{"secret":"hidden-payload"}}`),
	}}
	root := cr.ResourceRef{ClusterID: "cluster-a", APIVersion: graphCRDAPI, Kind: graphCRDKind, Name: "widgets.example.io"}
	graph, err := service.GetCustomResourceGraph(context.Background(), domainidentity.Principal{UserID: "reader"}, root, "customresourcedefinitions")
	if err != nil || direct.pageCalls != 1 || len(graph.Nodes) != 2 || len(graph.Edges) != 1 || graph.Edges[0].Relation != "defines" || !slices.Contains(graph.Warnings, graphCustomPartial) {
		t.Fatalf("graph=%+v pageReads=%d err=%v", graph, direct.pageCalls, err)
	}
	raw, _ := json.Marshal(graph)
	if strings.Contains(string(raw), "hidden-") || strings.Contains(string(raw), "private-owner-uid") {
		t.Fatalf("unexpected metadata in graph: %s", raw)
	}
}

func TestExplicitResourceGraphCRDInstanceRequiresExactDiscovery(t *testing.T) {
	for _, tc := range []struct {
		api, plural, ns string
		expected        error
	}{
		{"example.io/v1", "widgets", "team", nil},
		{"example.io/v2", "widgets", "team", apperrors.ErrNotFound},
		{"example.io/v1", "podwidgets", "team", apperrors.ErrNotFound},
		{"example.io/v1", "widgets", "", apperrors.ErrInvalidArgument},
	} {
		service, direct := newExplicitGraphService(graphCRDView("Pod"), exactPermissionAuthorizer{appaccess.PermPlatformExtensionsView: true})
		root := cr.ResourceRef{ClusterID: "cluster-a", APIVersion: tc.api, Kind: "Pod", Name: "sample", Namespace: tc.ns}
		graph, err := service.GetCustomResourceGraph(context.Background(), domainidentity.Principal{UserID: "reader"}, root, tc.plural)
		if !errors.Is(err, tc.expected) {
			t.Fatalf("root=%+v plural=%s: %v", root, tc.plural, err)
		}
		if tc.expected != nil {
			if direct.s.reads != 0 || direct.pageCalls != 0 {
				t.Fatalf("incorrect scope performed reads")
			}
			continue
		}
		if direct.s.reads != 1 || !slices.Contains(graph.Warnings, graphCustomSpecUnsupported) || len(graph.Nodes) != 2 {
			t.Fatalf("exact CR observation failed: %+v", graph)
		}
		observed, ok := resourceGraphRootNode(graph)
		if !ok || observed.Resource.APIVersion != tc.api || observed.Resource.UID != "sample-uid" {
			t.Fatalf("wrong root identity: %+v", observed)
		}
	}
}

func TestExplicitResourceGraphAppliesNamedCustomPolicy(t *testing.T) {
	authorizer := rbacGraphTestAuthorizer(func(req domainaccess.Request) domainaccess.Decision {
		return domainaccess.Decision{Allowed: req.PermissionKey == appaccess.PermPlatformExtensionsView && req.Resource.Name != "private"}
	})
	service, direct := newExplicitGraphService(graphCRDView("Widget"), authorizer)
	root := cr.ResourceRef{ClusterID: "cluster-a", APIVersion: "example.io/v1", Kind: "Widget", Namespace: "team", Name: "private"}
	_, err := service.GetCustomResourceGraph(context.Background(), domainidentity.Principal{}, root, "widgets")
	if !errors.Is(err, apperrors.ErrAccessDenied) || direct.s.reads != 0 || direct.pageCalls != 0 {
		t.Fatalf("named deny reached custom resource reads: reads=%d page=%d err=%v", direct.s.reads, direct.pageCalls, err)
	}
	direct.page.Items = []json.RawMessage{
		json.RawMessage(`{"apiVersion":"example.io/v1","kind":"Widget","metadata":{"name":"public","namespace":"team","uid":"public-uid"}}`),
		json.RawMessage(`{"apiVersion":"example.io/v1","kind":"Widget","metadata":{"name":"private","namespace":"team","uid":"private-uid"}}`),
	}
	root = cr.ResourceRef{ClusterID: "cluster-a", APIVersion: graphCRDAPI, Kind: graphCRDKind, Name: "widgets.example.io"}
	graph, err := service.GetCustomResourceGraph(context.Background(), domainidentity.Principal{}, root, "customresourcedefinitions")
	if err != nil || len(graph.Nodes) != 2 || len(graph.Edges) != 1 || direct.pageCalls != 1 {
		t.Fatalf("custom page was not filtered by resource name: graph=%+v err=%v", graph, err)
	}
	for _, node := range graph.Nodes {
		if node.Resource.Name == "private" {
			t.Fatal("named deny leaked a custom page neighbor")
		}
	}
}
