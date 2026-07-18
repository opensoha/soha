package resource

import (
	"context"
	"fmt"
	"strings"

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

type gatewayDetailSpec[T any] struct {
	kind       string
	auditText  string
	agentCall  func(NetworkAgent) (T, error)
	directCall func(DirectGatewayReader) (T, error)
	setActions func(*T, []string)
}

func getGatewayResource[T any](ctx context.Context, network *Network, principal domainidentity.Principal, clusterID, namespace, name string, spec gatewayDetailSpec[T]) (T, error) {
	request := resourceDetailRequest{
		clusterID: clusterID, namespace: namespace, kind: spec.kind, name: name,
		summary: func(source string) string { return fmt.Sprintf("%s via %s", spec.auditText, source) },
	}
	return getModeResource(ctx, network.resourceAccess, principal, request,
		func(connection domaincluster.Connection) (T, string, error) {
			item, source, err := routeModeValue(connection, network.networkAgentClient,
				func() (DirectGatewayReader, error) {
					return requireDirect(network.gatewayReader, network.gatewayReader != nil, "gateway reader")
				},
				spec.agentCall, spec.directCall,
			)
			if err != nil {
				return item, source, err
			}
			filterGatewayDetailRelations(ctx, network, principal, clusterID, namespace, &item)
			return item, source, nil
		},
		spec.setActions,
	)
}

func filterGatewayDetailRelations[T any](ctx context.Context, n *Network, principal domainidentity.Principal, clusterID, namespace string, item *T) {
	decisions := make(map[string]bool)
	canView := func(namespace, kind string) bool {
		key := namespace + "\x00" + kind
		allowed, ok := decisions[key]
		if !ok {
			allowed = canViewRelatedResource(ctx, n.resourceAccess, principal, clusterID, namespace, kind)
			decisions[key] = allowed
		}
		return allowed
	}
	switch detail := any(item).(type) {
	case *domainresource.GatewayClassDetailView:
		detail.Gateways = filterRelatedItems(detail.Gateways, func(item domainresource.GatewayView) bool {
			return canView(item.Namespace, "Gateway")
		})
	case *domainresource.GatewayDetailView:
		if !canView("", "GatewayClass") {
			detail.GatewayClass = ""
		}
		detail.Routes = filterRelatedItems(detail.Routes, func(item domainresource.GatewayRouteReferenceView) bool {
			return canView(item.Namespace, item.Kind)
		})
		for index := range detail.Listeners {
			detail.Listeners[index].CertificateRefs = filterRelatedItems(detail.Listeners[index].CertificateRefs, func(ref string) bool {
				return canView(formattedRefNamespace(ref, namespace), "Secret")
			})
		}
	case *domainresource.HTTPRouteDetailView:
		n.filterGatewayRouteRelations(ctx, principal, clusterID, namespace, &detail.ParentRefs, &detail.BackendServices, &detail.ParentStatuses, detail.Rules)
	case *domainresource.GRPCRouteDetailView:
		n.filterGatewayRouteRelations(ctx, principal, clusterID, namespace, &detail.ParentRefs, &detail.BackendServices, &detail.ParentStatuses, detail.Rules)
	case *domainresource.BackendTLSPolicyDetailView:
		if !canView(detail.Namespace, "Service") {
			detail.TargetRefs = nil
		}
	}
}

func (n *Network) filterGatewayRouteRelations(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string, parentRefs, backendServices *[]string, parentStatuses *[]domainresource.GatewayRouteParentStatusView, rules []domainresource.GatewayRouteRuleView) {
	decisions := make(map[string]bool)
	canView := func(itemNamespace, kind string) bool {
		key := itemNamespace + "\x00" + kind
		allowed, ok := decisions[key]
		if !ok {
			allowed = canViewRelatedResource(ctx, n.resourceAccess, principal, clusterID, itemNamespace, kind)
			decisions[key] = allowed
		}
		return allowed
	}
	*parentRefs = filterRelatedItems(*parentRefs, func(ref string) bool {
		return canView(routeParentNamespace(ref, namespace), "Gateway")
	})
	*parentStatuses = filterRelatedItems(*parentStatuses, func(status domainresource.GatewayRouteParentStatusView) bool {
		return canView(formattedRefNamespace(status.ParentRef, namespace), "Gateway")
	})
	if !canView(namespace, "Service") {
		*backendServices = nil
	}
	for ruleIndex := range rules {
		visible := make([]domainresource.GatewayRouteBackendView, 0, len(rules[ruleIndex].Backends))
		for _, backend := range rules[ruleIndex].Backends {
			backendNamespace := backend.Namespace
			if backendNamespace == "" {
				backendNamespace = namespace
			}
			kind := backend.Kind
			if kind == "" {
				kind = "Service"
			}
			if !canView(backendNamespace, kind) {
				continue
			}
			if !canView(backendNamespace, "EndpointSlice") {
				backend.Endpoints = nil
			} else {
				n.filterEndpointReferences(ctx, principal, clusterID, backendNamespace, backend.Endpoints)
			}
			if !canView(backendNamespace, "Pod") {
				backend.BackendPods = nil
			}
			visible = append(visible, backend)
		}
		rules[ruleIndex].Backends = visible
	}
}

func routeParentNamespace(ref, fallback string) string {
	namespace, _, found := strings.Cut(ref, "/")
	if !found || namespace == "" {
		return fallback
	}
	return namespace
}

func formattedRefNamespace(ref, fallback string) string {
	namespace, _, found := strings.Cut(ref, ":")
	if !found || namespace == "" {
		return fallback
	}
	return namespace
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

func (n *Network) GetGatewayClassDetail(ctx context.Context, principal domainidentity.Principal, clusterID, name string) (domainresource.GatewayClassDetailView, error) {
	return getGatewayResource(ctx, n, principal, clusterID, "", name, gatewayDetailSpec[domainresource.GatewayClassDetailView]{
		kind: "GatewayClass", auditText: "viewed gatewayclass detail",
		agentCall: func(client NetworkAgent) (domainresource.GatewayClassDetailView, error) {
			return client.GetGatewayClassDetail(ctx, name)
		},
		directCall: func(direct DirectGatewayReader) (domainresource.GatewayClassDetailView, error) {
			return direct.GetGatewayClassDetail(ctx, clusterID, name)
		},
		setActions: func(item *domainresource.GatewayClassDetailView, actions []string) { item.AllowedActions = actions },
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

func (n *Network) GetGatewayDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.GatewayDetailView, error) {
	return getGatewayResource(ctx, n, principal, clusterID, namespace, name, gatewayDetailSpec[domainresource.GatewayDetailView]{
		kind: "Gateway", auditText: "viewed gateway detail",
		agentCall: func(client NetworkAgent) (domainresource.GatewayDetailView, error) {
			return client.GetGatewayDetail(ctx, namespace, name)
		},
		directCall: func(direct DirectGatewayReader) (domainresource.GatewayDetailView, error) {
			return direct.GetGatewayDetail(ctx, clusterID, namespace, name)
		},
		setActions: func(item *domainresource.GatewayDetailView, actions []string) { item.AllowedActions = actions },
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

func (n *Network) GetHTTPRouteDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.HTTPRouteDetailView, error) {
	return getGatewayResource(ctx, n, principal, clusterID, namespace, name, gatewayDetailSpec[domainresource.HTTPRouteDetailView]{
		kind: "HTTPRoute", auditText: "viewed httproute detail",
		agentCall: func(client NetworkAgent) (domainresource.HTTPRouteDetailView, error) {
			return client.GetHTTPRouteDetail(ctx, namespace, name)
		},
		directCall: func(direct DirectGatewayReader) (domainresource.HTTPRouteDetailView, error) {
			return direct.GetHTTPRouteDetail(ctx, clusterID, namespace, name)
		},
		setActions: func(item *domainresource.HTTPRouteDetailView, actions []string) { item.AllowedActions = actions },
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

func (n *Network) GetBackendTLSPolicyDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.BackendTLSPolicyDetailView, error) {
	return getGatewayResource(ctx, n, principal, clusterID, namespace, name, gatewayDetailSpec[domainresource.BackendTLSPolicyDetailView]{
		kind: "BackendTLSPolicy", auditText: "viewed backendtlspolicy detail",
		agentCall: func(client NetworkAgent) (domainresource.BackendTLSPolicyDetailView, error) {
			return client.GetBackendTLSPolicyDetail(ctx, namespace, name)
		},
		directCall: func(direct DirectGatewayReader) (domainresource.BackendTLSPolicyDetailView, error) {
			return direct.GetBackendTLSPolicyDetail(ctx, clusterID, namespace, name)
		},
		setActions: func(item *domainresource.BackendTLSPolicyDetailView, actions []string) { item.AllowedActions = actions },
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

func (n *Network) GetGRPCRouteDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.GRPCRouteDetailView, error) {
	return getGatewayResource(ctx, n, principal, clusterID, namespace, name, gatewayDetailSpec[domainresource.GRPCRouteDetailView]{
		kind: "GRPCRoute", auditText: "viewed grpcroute detail",
		agentCall: func(client NetworkAgent) (domainresource.GRPCRouteDetailView, error) {
			return client.GetGRPCRouteDetail(ctx, namespace, name)
		},
		directCall: func(direct DirectGatewayReader) (domainresource.GRPCRouteDetailView, error) {
			return direct.GetGRPCRouteDetail(ctx, clusterID, namespace, name)
		},
		setActions: func(item *domainresource.GRPCRouteDetailView, actions []string) { item.AllowedActions = actions },
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

func (n *Network) GetReferenceGrantDetail(ctx context.Context, principal domainidentity.Principal, clusterID, namespace, name string) (domainresource.ReferenceGrantDetailView, error) {
	return getGatewayResource(ctx, n, principal, clusterID, namespace, name, gatewayDetailSpec[domainresource.ReferenceGrantDetailView]{
		kind: "ReferenceGrant", auditText: "viewed referencegrant detail",
		agentCall: func(client NetworkAgent) (domainresource.ReferenceGrantDetailView, error) {
			return client.GetReferenceGrantDetail(ctx, namespace, name)
		},
		directCall: func(direct DirectGatewayReader) (domainresource.ReferenceGrantDetailView, error) {
			return direct.GetReferenceGrantDetail(ctx, clusterID, namespace, name)
		},
		setActions: func(item *domainresource.ReferenceGrantDetailView, actions []string) { item.AllowedActions = actions },
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
