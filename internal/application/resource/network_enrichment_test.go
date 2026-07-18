package resource

import (
	"context"
	"testing"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

type relatedKindAuthorizer map[string]bool

func (a relatedKindAuthorizer) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	return domainaccess.Decision{Allowed: a[request.Resource.Kind]}, nil
}

func TestFilterIngressEnrichmentHonorsRelatedResourceAuthorization(t *testing.T) {
	network := &Network{resourceAccess: &resourceAccess{
		resolver: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a"}}},
		authorizer: relatedKindAuthorizer{
			"Service": true, "EndpointSlice": false, "Pod": true,
			"Deployment": false, "StatefulSet": true,
		},
	}}
	item := domainresource.IngressDetailView{Backends: []domainresource.IngressBackendView{{
		ServiceName: "api",
		Endpoints:   []domainresource.ServiceEndpointView{{Address: "10.0.0.1"}},
		Pods: []domainresource.NetworkRelatedPodView{{
			PodView: domainresource.PodView{Name: "api-0"},
			Workloads: []domainresource.PodRelatedResourceView{
				{Kind: "Deployment", Name: "api"},
				{Kind: "StatefulSet", Name: "db"},
			},
		}},
	}}}

	network.filterIngressEnrichment(context.Background(), domainidentity.Principal{UserID: "user-a"}, "cluster-a", "team-a", &item)

	if len(item.Backends) != 1 || len(item.Backends[0].Endpoints) != 0 || len(item.Backends[0].Pods) != 1 {
		t.Fatalf("filtered backend = %#v", item.Backends)
	}
	workloads := item.Backends[0].Pods[0].Workloads
	if len(workloads) != 1 || workloads[0].Kind != "StatefulSet" {
		t.Fatalf("filtered workloads = %#v", workloads)
	}
}

func TestFilterIngressEnrichmentOmitsBackendsWithoutServiceAccess(t *testing.T) {
	network := &Network{resourceAccess: &resourceAccess{
		resolver:   stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a"}}},
		authorizer: relatedKindAuthorizer{},
	}}
	item := domainresource.IngressDetailView{Backends: []domainresource.IngressBackendView{{ServiceName: "api"}}}

	network.filterIngressEnrichment(context.Background(), domainidentity.Principal{UserID: "user-a"}, "cluster-a", "team-a", &item)

	if item.Backends != nil {
		t.Fatalf("backends = %#v, want nil", item.Backends)
	}
}

func TestFilterServiceEnrichmentOmitsUnauthorizedPods(t *testing.T) {
	network := &Network{resourceAccess: &resourceAccess{
		resolver:   stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a"}}},
		authorizer: relatedKindAuthorizer{"EndpointSlice": true, "Pod": false, "Node": false},
	}}
	item := domainresource.ServiceDetailView{
		Endpoints:   []domainresource.ServiceEndpointView{{Address: "10.0.0.1", TargetRef: "Pod/api-0", NodeName: "node-a"}},
		BackendPods: []domainresource.PodView{{Name: "api-0"}},
	}

	network.filterServiceEnrichment(context.Background(), domainidentity.Principal{UserID: "user-a"}, "cluster-a", "team-a", &item)

	if len(item.Endpoints) != 1 || item.Endpoints[0].TargetRef != "" || item.Endpoints[0].NodeName != "" || item.BackendPods != nil {
		t.Fatalf("filtered service detail = %#v", item)
	}
}

func TestFilterIngressClassRelationsHonorsNamespaceAuthorization(t *testing.T) {
	access := &resourceAccess{
		resolver:   stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-a"}}},
		authorizer: namespaceIngressAuthorizer{"team-a": true},
	}
	items := filterIngressClassRelations(context.Background(), access, domainidentity.Principal{UserID: "user-a"}, "cluster-a", []domainresource.IngressView{
		{Name: "visible", Namespace: "team-a"},
		{Name: "private", Namespace: "team-b"},
	})
	if len(items) != 1 || items[0].Name != "visible" {
		t.Fatalf("ingress class relations = %#v", items)
	}
}

type namespaceIngressAuthorizer map[string]bool

func (a namespaceIngressAuthorizer) Authorize(_ context.Context, request domainaccess.Request) (domainaccess.Decision, error) {
	return domainaccess.Decision{Allowed: request.Resource.Kind == "Ingress" && a[request.Namespace.Namespace]}, nil
}
