package resource

import (
	"context"
	"fmt"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
)

type gatewayListSpec[T any] struct {
	kind        string
	auditText   string
	agentCall   func(NetworkAgent) ([]T, error)
	directCall  func(DirectGatewayReader) ([]T, error)
	namespaceOf func(T) string
	actionsOf   func(T) []string
	setActions  func(*T, []string)
}

func listGatewayResources[T any](ctx context.Context, network *Network, principal domainidentity.Principal, clusterID, namespace string, spec gatewayListSpec[T]) ([]T, error) {
	request := resourceListRequest{
		clusterID: clusterID,
		namespace: namespace,
		kind:      spec.kind,
		summary: func(source string) string {
			if namespace == "" {
				return fmt.Sprintf("%s via %s", spec.auditText, source)
			}
			return fmt.Sprintf("%s via %s in namespace %s", spec.auditText, source, displayNamespace(namespace))
		},
	}
	return listModeResources(
		ctx,
		network.resourceAccess,
		principal,
		request,
		func(connection domaincluster.Connection) ([]T, string, error) {
			return routeModeItems(
				connection,
				network.networkAgentClient,
				func() (DirectGatewayReader, error) {
					return requireDirect(network.gatewayReader, network.gatewayReader != nil, "gateway reader")
				},
				spec.agentCall,
				spec.directCall,
			)
		},
		spec.namespaceOf,
		spec.actionsOf,
		spec.setActions,
	)
}

func (n *Network) ListGatewayClasses(ctx context.Context, principal domainidentity.Principal, clusterID string) ([]domainresource.GatewayClassView, error) {
	return listGatewayResources(ctx, n, principal, clusterID, "", gatewayListSpec[domainresource.GatewayClassView]{
		kind:       "GatewayClass",
		auditText:  "listed gatewayclasses",
		agentCall:  bindClusterAgentList(ctx, NetworkAgent.ListGatewayClasses),
		directCall: bindClusterDirectList(ctx, clusterID, DirectGatewayReader.ListGatewayClasses),
		actionsOf:  gatewayClassActions,
		setActions: setGatewayClassActions,
	})
}

func (n *Network) ListGateways(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.GatewayView, error) {
	return listGatewayResources(ctx, n, principal, clusterID, namespace, gatewayListSpec[domainresource.GatewayView]{
		kind:        "Gateway",
		auditText:   "listed gateways",
		agentCall:   bindNamespacedAgentList(ctx, namespace, NetworkAgent.ListGateways),
		directCall:  bindNamespacedDirectList(ctx, clusterID, namespace, DirectGatewayReader.ListGateways),
		namespaceOf: gatewayNamespace,
		actionsOf:   gatewayActions,
		setActions:  setGatewayActions,
	})
}

func (n *Network) ListHTTPRoutes(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.HTTPRouteView, error) {
	return listGatewayResources(ctx, n, principal, clusterID, namespace, gatewayListSpec[domainresource.HTTPRouteView]{
		kind:        "HTTPRoute",
		auditText:   "listed httproutes",
		agentCall:   bindNamespacedAgentList(ctx, namespace, NetworkAgent.ListHTTPRoutes),
		directCall:  bindNamespacedDirectList(ctx, clusterID, namespace, DirectGatewayReader.ListHTTPRoutes),
		namespaceOf: httpRouteNamespace,
		actionsOf:   httpRouteActions,
		setActions:  setHTTPRouteActions,
	})
}

func (n *Network) ListBackendTLSPolicies(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.BackendTLSPolicyView, error) {
	return listGatewayResources(ctx, n, principal, clusterID, namespace, gatewayListSpec[domainresource.BackendTLSPolicyView]{
		kind:        "BackendTLSPolicy",
		auditText:   "listed backendtlspolicies",
		agentCall:   bindNamespacedAgentList(ctx, namespace, NetworkAgent.ListBackendTLSPolicies),
		directCall:  bindNamespacedDirectList(ctx, clusterID, namespace, DirectGatewayReader.ListBackendTLSPolicies),
		namespaceOf: backendTLSPolicyNamespace,
		actionsOf:   backendTLSPolicyActions,
		setActions:  setBackendTLSPolicyActions,
	})
}

func (n *Network) ListGRPCRoutes(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.GRPCRouteView, error) {
	return listGatewayResources(ctx, n, principal, clusterID, namespace, gatewayListSpec[domainresource.GRPCRouteView]{
		kind:        "GRPCRoute",
		auditText:   "listed grpcroutes",
		agentCall:   bindNamespacedAgentList(ctx, namespace, NetworkAgent.ListGRPCRoutes),
		directCall:  bindNamespacedDirectList(ctx, clusterID, namespace, DirectGatewayReader.ListGRPCRoutes),
		namespaceOf: grpcRouteNamespace,
		actionsOf:   grpcRouteActions,
		setActions:  setGRPCRouteActions,
	})
}

func (n *Network) ListReferenceGrants(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.ReferenceGrantView, error) {
	return listGatewayResources(ctx, n, principal, clusterID, namespace, gatewayListSpec[domainresource.ReferenceGrantView]{
		kind:        "ReferenceGrant",
		auditText:   "listed referencegrants",
		agentCall:   bindNamespacedAgentList(ctx, namespace, NetworkAgent.ListReferenceGrants),
		directCall:  bindNamespacedDirectList(ctx, clusterID, namespace, DirectGatewayReader.ListReferenceGrants),
		namespaceOf: referenceGrantNamespace,
		actionsOf:   referenceGrantActions,
		setActions:  setReferenceGrantActions,
	})
}

func gatewayClassActions(item domainresource.GatewayClassView) []string { return item.AllowedActions }
func setGatewayClassActions(item *domainresource.GatewayClassView, actions []string) {
	item.AllowedActions = actions
}
func gatewayNamespace(item domainresource.GatewayView) string { return item.Namespace }
func gatewayActions(item domainresource.GatewayView) []string { return item.AllowedActions }
func setGatewayActions(item *domainresource.GatewayView, actions []string) {
	item.AllowedActions = actions
}
func httpRouteNamespace(item domainresource.HTTPRouteView) string { return item.Namespace }
func httpRouteActions(item domainresource.HTTPRouteView) []string { return item.AllowedActions }
func setHTTPRouteActions(item *domainresource.HTTPRouteView, actions []string) {
	item.AllowedActions = actions
}
func backendTLSPolicyNamespace(item domainresource.BackendTLSPolicyView) string {
	return item.Namespace
}
func backendTLSPolicyActions(item domainresource.BackendTLSPolicyView) []string {
	return item.AllowedActions
}
func setBackendTLSPolicyActions(item *domainresource.BackendTLSPolicyView, actions []string) {
	item.AllowedActions = actions
}
func grpcRouteNamespace(item domainresource.GRPCRouteView) string { return item.Namespace }
func grpcRouteActions(item domainresource.GRPCRouteView) []string { return item.AllowedActions }
func setGRPCRouteActions(item *domainresource.GRPCRouteView, actions []string) {
	item.AllowedActions = actions
}
func referenceGrantNamespace(item domainresource.ReferenceGrantView) string { return item.Namespace }
func referenceGrantActions(item domainresource.ReferenceGrantView) []string {
	return item.AllowedActions
}
func setReferenceGrantActions(item *domainresource.ReferenceGrantView, actions []string) {
	item.AllowedActions = actions
}
