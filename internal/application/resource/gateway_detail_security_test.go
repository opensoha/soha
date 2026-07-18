package resource

import (
	"context"
	"testing"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

func TestFilterGatewayDetailRelationsRemovesUnauthorizedBackends(t *testing.T) {
	network := &Network{resourceAccess: &resourceAccess{
		resolver:   stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-1"}}},
		authorizer: relatedKindAuthorizer{"Service": false},
	}}
	detail := domainresource.HTTPRouteDetailView{Rules: []domainresource.GatewayRouteRuleView{{Backends: []domainresource.GatewayRouteBackendView{{Kind: "Service", Namespace: "team-a", Name: "private"}}}}}
	filterGatewayDetailRelations(context.Background(), network, domainidentity.Principal{UserID: "user"}, "cluster-1", "team-a", &detail)
	if len(detail.Rules[0].Backends) != 0 {
		t.Fatalf("unauthorized backend leaked: %#v", detail.Rules[0].Backends)
	}
}

func TestFilterGatewayDetailRelationsRedactsNestedResources(t *testing.T) {
	network := &Network{resourceAccess: &resourceAccess{
		resolver: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ID: "cluster-1"}}},
		authorizer: relatedKindAuthorizer{
			"Service": true, "EndpointSlice": false, "Pod": false, "Gateway": false,
		},
	}}
	detail := domainresource.HTTPRouteDetailView{
		HTTPRouteView: domainresource.HTTPRouteView{
			ParentRefs: []string{"team-a/edge"}, BackendServices: []string{"api"},
		},
		ParentStatuses: []domainresource.GatewayRouteParentStatusView{{ParentRef: "team-a:Gateway/edge"}},
		Rules: []domainresource.GatewayRouteRuleView{{Backends: []domainresource.GatewayRouteBackendView{{
			Kind: "Service", Namespace: "team-a", Name: "api",
			Endpoints:   []domainresource.ServiceEndpointView{{Address: "10.0.0.1"}},
			BackendPods: []domainresource.PodView{{Name: "api-0", Namespace: "team-a"}},
		}}}},
	}

	filterGatewayDetailRelations(context.Background(), network, domainidentity.Principal{UserID: "user"}, "cluster-1", "team-a", &detail)

	backend := detail.Rules[0].Backends[0]
	if detail.ParentRefs != nil || detail.ParentStatuses != nil || backend.Endpoints != nil || backend.BackendPods != nil {
		t.Fatalf("gateway relations were not redacted: %#v", detail)
	}
}
