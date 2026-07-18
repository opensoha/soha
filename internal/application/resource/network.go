package resource

import (
	"context"
	"fmt"
	"strings"

	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (n *Network) ListServices(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.ServiceView, error) {
	return listSourcedModeResources(ctx, n.resourceAccess, principal, namespacedListRequest(clusterID, namespace, "Service", "services"), n.networkAgentClient, n.directNetwork,
		bindNamespacedAgentList(ctx, namespace, NetworkAgent.ListServices),
		bindNamespacedSourcedDirectList(ctx, clusterID, namespace, DirectNetworkReader.ListServices),
		func(item domainresource.ServiceView) string { return item.Namespace },
		func(item domainresource.ServiceView) []string { return item.AllowedActions },
		func(item *domainresource.ServiceView, actions []string) { item.AllowedActions = actions },
	)
}

func (n *Network) GetServiceDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ServiceDetailView, error) {
	request := resourceDetailRequest{
		clusterID: clusterID, namespace: namespace, kind: "Service", name: name,
		summary: func(source string) string {
			return fmt.Sprintf("viewed service detail via %s in namespace %s", source, displayNamespace(namespace))
		},
	}
	item, err := getModeResource(ctx, n.resourceAccess, principal, request,
		func(connection domaincluster.Connection) (domainresource.ServiceDetailView, string, error) {
			return routeModeValue(connection, n.networkAgentClient, n.directNetwork,
				func(client NetworkAgent) (domainresource.ServiceDetailView, error) {
					return client.GetServiceDetail(ctx, namespace, name)
				},
				func(direct DirectNetworkReader) (domainresource.ServiceDetailView, error) {
					return direct.GetServiceDetail(ctx, clusterID, namespace, name)
				},
			)
		},
		func(item *domainresource.ServiceDetailView, actions []string) { item.AllowedActions = actions },
	)
	if err != nil {
		return domainresource.ServiceDetailView{}, err
	}
	n.filterServiceEnrichment(ctx, principal, clusterID, namespace, &item)
	return item, nil
}

func (n *Network) ListIngresses(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.IngressView, error) {
	return listSourcedModeResources(ctx, n.resourceAccess, principal, namespacedListRequest(clusterID, namespace, "Ingress", "ingresses"), n.networkAgentClient, n.directNetwork,
		bindNamespacedAgentList(ctx, namespace, NetworkAgent.ListIngresses),
		bindNamespacedSourcedDirectList(ctx, clusterID, namespace, DirectNetworkReader.ListIngresses),
		func(item domainresource.IngressView) string { return item.Namespace },
		func(item domainresource.IngressView) []string { return item.AllowedActions },
		func(item *domainresource.IngressView, actions []string) { item.AllowedActions = actions },
	)
}

func (n *Network) GetIngressDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.IngressDetailView, error) {
	item, err := getModeResource(ctx, n.resourceAccess, principal, resourceDetailRequest{
		clusterID: clusterID, namespace: namespace, kind: "Ingress", name: name,
		summary: func(source string) string {
			return fmt.Sprintf("viewed ingress detail via %s in namespace %s", source, displayNamespace(namespace))
		},
	}, func(connection domaincluster.Connection) (domainresource.IngressDetailView, string, error) {
		return routeModeValue(connection, n.networkAgentClient, n.directNetwork,
			func(client NetworkAgent) (domainresource.IngressDetailView, error) {
				return client.GetIngressDetail(ctx, namespace, name)
			},
			func(direct DirectNetworkReader) (domainresource.IngressDetailView, error) {
				return direct.GetIngressDetail(ctx, clusterID, namespace, name)
			})
	}, func(item *domainresource.IngressDetailView, actions []string) { item.AllowedActions = actions })
	if err != nil {
		return domainresource.IngressDetailView{}, err
	}
	n.filterIngressEnrichment(ctx, principal, clusterID, namespace, &item)
	return item, nil
}

func (n *Network) ListEndpointSlices(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.EndpointSliceView, error) {
	return listRoutedModeResources(ctx, n.resourceAccess, principal, namespacedListRequest(clusterID, namespace, "EndpointSlice", "endpoint slices"), n.networkAgentClient, n.directNetwork,
		bindNamespacedAgentList(ctx, namespace, NetworkAgent.ListEndpointSlices),
		bindNamespacedDirectList(ctx, clusterID, namespace, DirectNetworkReader.ListEndpointSlices),
		func(item domainresource.EndpointSliceView) string { return item.Namespace },
		func(item domainresource.EndpointSliceView) []string { return item.AllowedActions },
		func(item *domainresource.EndpointSliceView, actions []string) { item.AllowedActions = actions },
	)
}

func (n *Network) GetEndpointSliceDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.EndpointSliceDetailView, error) {
	item, err := getModeResource(ctx, n.resourceAccess, principal, resourceDetailRequest{
		clusterID: clusterID, namespace: namespace, kind: "EndpointSlice", name: name,
		summary: func(source string) string {
			return fmt.Sprintf("viewed endpoint slice detail via %s in namespace %s", source, displayNamespace(namespace))
		},
	}, func(connection domaincluster.Connection) (domainresource.EndpointSliceDetailView, string, error) {
		return routeModeValue(connection, n.networkAgentClient, n.directNetwork,
			func(client NetworkAgent) (domainresource.EndpointSliceDetailView, error) {
				return client.GetEndpointSliceDetail(ctx, namespace, name)
			},
			func(direct DirectNetworkReader) (domainresource.EndpointSliceDetailView, error) {
				return direct.GetEndpointSliceDetail(ctx, clusterID, namespace, name)
			})
	}, func(item *domainresource.EndpointSliceDetailView, actions []string) { item.AllowedActions = actions })
	if err != nil {
		return domainresource.EndpointSliceDetailView{}, err
	}
	if !n.canViewRelatedKind(ctx, principal, clusterID, namespace, "Service") {
		item.ServiceName = ""
	}
	n.filterEndpointReferences(ctx, principal, clusterID, namespace, item.Endpoints)
	return item, nil
}

func (n *Network) ListNetworkPolicies(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.NetworkPolicyView, error) {
	return listRoutedModeResources(ctx, n.resourceAccess, principal, namespacedListRequest(clusterID, namespace, "NetworkPolicy", "network policies"), n.networkAgentClient, n.directNetwork,
		bindNamespacedAgentList(ctx, namespace, NetworkAgent.ListNetworkPolicies),
		bindNamespacedDirectList(ctx, clusterID, namespace, DirectNetworkReader.ListNetworkPolicies),
		func(item domainresource.NetworkPolicyView) string { return item.Namespace },
		func(item domainresource.NetworkPolicyView) []string { return item.AllowedActions },
		func(item *domainresource.NetworkPolicyView, actions []string) { item.AllowedActions = actions },
	)
}

func (n *Network) GetNetworkPolicyDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.NetworkPolicyDetailView, error) {
	item, err := getModeResource(ctx, n.resourceAccess, principal, resourceDetailRequest{
		clusterID: clusterID, namespace: namespace, kind: "NetworkPolicy", name: name,
		summary: func(source string) string {
			return fmt.Sprintf("viewed network policy detail via %s in namespace %s", source, displayNamespace(namespace))
		},
	}, func(connection domaincluster.Connection) (domainresource.NetworkPolicyDetailView, string, error) {
		return routeModeValue(connection, n.networkAgentClient, n.directNetwork,
			func(client NetworkAgent) (domainresource.NetworkPolicyDetailView, error) {
				return client.GetNetworkPolicyDetail(ctx, namespace, name)
			},
			func(direct DirectNetworkReader) (domainresource.NetworkPolicyDetailView, error) {
				return direct.GetNetworkPolicyDetail(ctx, clusterID, namespace, name)
			})
	}, func(item *domainresource.NetworkPolicyDetailView, actions []string) { item.AllowedActions = actions })
	if err != nil {
		return domainresource.NetworkPolicyDetailView{}, err
	}
	if !n.canViewRelatedKind(ctx, principal, clusterID, namespace, "Pod") {
		item.MatchingPods = nil
	}
	return item, nil
}

func (n *Network) canViewRelatedKind(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, kind string) bool {
	_, decision, err := n.decide(ctx, principal, clusterID, namespace, resourceGroupForKind(kind), kind, domainaccess.ActionView)
	return err == nil && decision.Allowed
}

func (n *Network) filterServiceEnrichment(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string, item *domainresource.ServiceDetailView) {
	if !n.canViewRelatedKind(ctx, principal, clusterID, namespace, "EndpointSlice") {
		item.Endpoints = nil
	} else {
		n.filterEndpointReferences(ctx, principal, clusterID, namespace, item.Endpoints)
	}
	if !n.canViewRelatedKind(ctx, principal, clusterID, namespace, "Pod") {
		item.BackendPods = nil
	}
}

func (n *Network) filterIngressEnrichment(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string, item *domainresource.IngressDetailView) {
	if !n.canViewRelatedKind(ctx, principal, clusterID, namespace, "Service") {
		item.Backends = nil
		for i := range item.Routes {
			item.Routes[i].ServiceName = ""
			item.Routes[i].ServicePort = ""
		}
		return
	}
	canViewEndpoints := n.canViewRelatedKind(ctx, principal, clusterID, namespace, "EndpointSlice")
	canViewPods := n.canViewRelatedKind(ctx, principal, clusterID, namespace, "Pod")
	workloadAccess := make(map[string]bool)
	for i := range item.Backends {
		if !canViewEndpoints {
			item.Backends[i].Endpoints = nil
		} else {
			n.filterEndpointReferences(ctx, principal, clusterID, namespace, item.Backends[i].Endpoints)
		}
		if !canViewPods {
			item.Backends[i].Pods = nil
			continue
		}
		for j := range item.Backends[i].Pods {
			visible := item.Backends[i].Pods[j].Workloads[:0]
			for _, workload := range item.Backends[i].Pods[j].Workloads {
				allowed, ok := workloadAccess[workload.Kind]
				if !ok {
					allowed = n.canViewRelatedKind(ctx, principal, clusterID, namespace, workload.Kind)
					workloadAccess[workload.Kind] = allowed
				}
				if allowed {
					visible = append(visible, workload)
				}
			}
			item.Backends[i].Pods[j].Workloads = visible
		}
	}
}

func (n *Network) filterEndpointReferences(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string, endpoints []domainresource.ServiceEndpointView) {
	access := make(map[string]bool)
	canViewNode := n.canViewRelatedKind(ctx, principal, clusterID, "", "Node")
	for i := range endpoints {
		if !canViewNode {
			endpoints[i].NodeName = ""
		}
		kind, _, found := strings.Cut(endpoints[i].TargetRef, "/")
		if !found || kind == "" {
			endpoints[i].TargetRef = ""
			continue
		}
		allowed, ok := access[kind]
		if !ok {
			effectiveNamespace := namespace
			if kind == "Node" {
				effectiveNamespace = ""
			}
			allowed = n.canViewRelatedKind(ctx, principal, clusterID, effectiveNamespace, kind)
			access[kind] = allowed
		}
		if !allowed {
			endpoints[i].TargetRef = ""
		}
	}
}

func (n *Network) directNetwork() (DirectNetworkReader, error) {
	if n.directReader == nil {
		return nil, fmt.Errorf("%w: direct network reader is not configured", apperrors.ErrClusterUnready)
	}
	return n.directReader, nil
}

func namespacedListRequest(clusterID, namespace, kind, noun string) resourceListRequest {
	return resourceListRequest{
		clusterID: clusterID, namespace: namespace, kind: kind,
		summary: func(source string) string {
			return fmt.Sprintf("listed %s via %s in namespace %s", noun, source, displayNamespace(namespace))
		},
	}
}
