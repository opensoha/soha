package resource

import (
	"context"
	"fmt"

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

func (n *Network) ListIngresses(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.IngressView, error) {
	return listSourcedModeResources(ctx, n.resourceAccess, principal, namespacedListRequest(clusterID, namespace, "Ingress", "ingresses"), n.networkAgentClient, n.directNetwork,
		bindNamespacedAgentList(ctx, namespace, NetworkAgent.ListIngresses),
		bindNamespacedSourcedDirectList(ctx, clusterID, namespace, DirectNetworkReader.ListIngresses),
		func(item domainresource.IngressView) string { return item.Namespace },
		func(item domainresource.IngressView) []string { return item.AllowedActions },
		func(item *domainresource.IngressView, actions []string) { item.AllowedActions = actions },
	)
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

func (n *Network) ListNetworkPolicies(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.NetworkPolicyView, error) {
	return listRoutedModeResources(ctx, n.resourceAccess, principal, namespacedListRequest(clusterID, namespace, "NetworkPolicy", "network policies"), n.networkAgentClient, n.directNetwork,
		bindNamespacedAgentList(ctx, namespace, NetworkAgent.ListNetworkPolicies),
		bindNamespacedDirectList(ctx, clusterID, namespace, DirectNetworkReader.ListNetworkPolicies),
		func(item domainresource.NetworkPolicyView) string { return item.Namespace },
		func(item domainresource.NetworkPolicyView) []string { return item.AllowedActions },
		func(item *domainresource.NetworkPolicyView, actions []string) { item.AllowedActions = actions },
	)
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
