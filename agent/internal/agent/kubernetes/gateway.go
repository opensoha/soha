package kubernetes

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	domainresource "github.com/opensoha/soha-agent/internal/domain/resource"
)

var gatewayVersions = []string{"v1", "v1beta1"}
var httpRouteVersions = []string{"v1", "v1beta1"}
var backendTLSPolicyVersions = []string{"v1", "v1alpha3"}
var grpcRouteVersions = []string{"v1", "v1alpha2"}
var referenceGrantVersions = []string{"v1", "v1beta1", "v1alpha2"}

func (c *Client) ListGatewayClasses(ctx context.Context) ([]domainresource.GatewayClassView, error) {
	items, err := c.listClusterDynamicResources(ctx, "gateway.networking.k8s.io", gatewayVersions, "gatewayclasses")
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.GatewayClassView, 0, len(items))
	for _, item := range items {
		views = append(views, mapGatewayClassResource(item))
	}
	return views, nil
}

func (c *Client) ListGateways(ctx context.Context, namespace string) ([]domainresource.GatewayView, error) {
	items, err := c.listNamespacedDynamicResources(ctx, namespace, "gateway.networking.k8s.io", gatewayVersions, "gateways")
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.GatewayView, 0, len(items))
	for _, item := range items {
		views = append(views, mapGatewayResource(item))
	}
	return views, nil
}

func (c *Client) ListHTTPRoutes(ctx context.Context, namespace string) ([]domainresource.HTTPRouteView, error) {
	items, err := c.listNamespacedDynamicResources(ctx, namespace, "gateway.networking.k8s.io", httpRouteVersions, "httproutes")
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.HTTPRouteView, 0, len(items))
	for _, item := range items {
		views = append(views, mapHTTPRouteResource(item))
	}
	return views, nil
}

func (c *Client) ListBackendTLSPolicies(ctx context.Context, namespace string) ([]domainresource.BackendTLSPolicyView, error) {
	items, err := c.listNamespacedDynamicResources(ctx, namespace, "gateway.networking.k8s.io", backendTLSPolicyVersions, "backendtlspolicies")
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.BackendTLSPolicyView, 0, len(items))
	for _, item := range items {
		views = append(views, mapBackendTLSPolicyResource(item))
	}
	return views, nil
}

func (c *Client) ListGRPCRoutes(ctx context.Context, namespace string) ([]domainresource.GRPCRouteView, error) {
	items, err := c.listNamespacedDynamicResources(ctx, namespace, "gateway.networking.k8s.io", grpcRouteVersions, "grpcroutes")
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.GRPCRouteView, 0, len(items))
	for _, item := range items {
		views = append(views, mapGRPCRouteResource(item))
	}
	return views, nil
}

func (c *Client) ListReferenceGrants(ctx context.Context, namespace string) ([]domainresource.ReferenceGrantView, error) {
	items, err := c.listNamespacedDynamicResources(ctx, namespace, "gateway.networking.k8s.io", referenceGrantVersions, "referencegrants")
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.ReferenceGrantView, 0, len(items))
	for _, item := range items {
		views = append(views, mapReferenceGrantResource(item))
	}
	return views, nil
}

func (c *Client) GetGatewayClassDetail(ctx context.Context, name string) (domainresource.GatewayClassDetailView, error) {
	item, err := c.getDynamicResource(ctx, "", "gateway.networking.k8s.io", gatewayVersions, "gatewayclasses", name)
	if err != nil {
		return domainresource.GatewayClassDetailView{}, err
	}
	gatewayItems, err := c.listAllNamespacedDynamicResources(ctx, "gateway.networking.k8s.io", gatewayVersions, "gateways")
	if err != nil {
		return domainresource.GatewayClassDetailView{}, err
	}
	related := make([]domainresource.GatewayView, 0)
	for _, gatewayItem := range gatewayItems {
		gateway := mapGatewayResource(gatewayItem)
		if gateway.GatewayClass == name {
			related = append(related, gateway)
		}
	}
	return domainresource.GatewayClassDetailView{
		GatewayClassView: mapGatewayClassResource(item),
		Labels:           item.GetLabels(),
		Annotations:      item.GetAnnotations(),
		Conditions:       gatewayConditionViews(item, ""),
		Gateways:         related,
	}, nil
}

func (c *Client) GetGatewayDetail(ctx context.Context, namespace, name string) (domainresource.GatewayDetailView, error) {
	item, err := c.getDynamicResource(ctx, namespace, "gateway.networking.k8s.io", gatewayVersions, "gateways", name)
	if err != nil {
		return domainresource.GatewayDetailView{}, err
	}
	routes, err := c.gatewayRelatedRoutes(ctx, namespace, name)
	if err != nil {
		return domainresource.GatewayDetailView{}, err
	}
	return domainresource.GatewayDetailView{
		GatewayView: mapGatewayResource(item),
		Labels:      item.GetLabels(),
		Annotations: item.GetAnnotations(),
		Conditions:  gatewayConditionViews(item, ""),
		Listeners:   gatewayListenerViews(item),
		Routes:      routes,
	}, nil
}

func (c *Client) GetHTTPRouteDetail(ctx context.Context, namespace, name string) (domainresource.HTTPRouteDetailView, error) {
	item, err := c.getDynamicResource(ctx, namespace, "gateway.networking.k8s.io", httpRouteVersions, "httproutes", name)
	if err != nil {
		return domainresource.HTTPRouteDetailView{}, err
	}
	detail := domainresource.HTTPRouteDetailView{
		HTTPRouteView:  mapHTTPRouteResource(item),
		Labels:         item.GetLabels(),
		Annotations:    item.GetAnnotations(),
		Conditions:     gatewayConditionViews(item, "parents"),
		ParentStatuses: gatewayRouteParentStatusViews(item),
		Rules:          gatewayRouteRuleViews(item),
	}
	enrichGatewayBackends(ctx, c, &detail.Rules)
	return detail, nil
}

func (c *Client) GetGRPCRouteDetail(ctx context.Context, namespace, name string) (domainresource.GRPCRouteDetailView, error) {
	item, err := c.getDynamicResource(ctx, namespace, "gateway.networking.k8s.io", grpcRouteVersions, "grpcroutes", name)
	if err != nil {
		return domainresource.GRPCRouteDetailView{}, err
	}
	detail := domainresource.GRPCRouteDetailView{
		GRPCRouteView:  mapGRPCRouteResource(item),
		Labels:         item.GetLabels(),
		Annotations:    item.GetAnnotations(),
		Conditions:     gatewayConditionViews(item, "parents"),
		ParentStatuses: gatewayRouteParentStatusViews(item),
		Rules:          gatewayRouteRuleViews(item),
	}
	enrichGatewayBackends(ctx, c, &detail.Rules)
	return detail, nil
}

func enrichGatewayBackends(ctx context.Context, client *Client, rules *[]domainresource.GatewayRouteRuleView) {
	if client == nil || client.typed == nil {
		return
	}
	for ruleIndex := range *rules {
		for backendIndex := range (*rules)[ruleIndex].Backends {
			backend := &(*rules)[ruleIndex].Backends[backendIndex]
			if !strings.EqualFold(backend.Kind, "Service") {
				continue
			}
			service, err := client.GetServiceDetail(ctx, backend.Namespace, backend.Name)
			if err != nil {
				continue
			}
			backend.Endpoints = service.Endpoints
			backend.BackendPods = service.BackendPods
		}
	}
}

func (c *Client) GetBackendTLSPolicyDetail(ctx context.Context, namespace, name string) (domainresource.BackendTLSPolicyDetailView, error) {
	item, err := c.getDynamicResource(ctx, namespace, "gateway.networking.k8s.io", backendTLSPolicyVersions, "backendtlspolicies", name)
	if err != nil {
		return domainresource.BackendTLSPolicyDetailView{}, err
	}
	return domainresource.BackendTLSPolicyDetailView{
		BackendTLSPolicyView: mapBackendTLSPolicyResource(item),
		Labels:               item.GetLabels(),
		Annotations:          item.GetAnnotations(),
		Conditions:           gatewayConditionViews(item, "ancestors"),
	}, nil
}

func (c *Client) GetReferenceGrantDetail(ctx context.Context, namespace, name string) (domainresource.ReferenceGrantDetailView, error) {
	item, err := c.getDynamicResource(ctx, namespace, "gateway.networking.k8s.io", referenceGrantVersions, "referencegrants", name)
	if err != nil {
		return domainresource.ReferenceGrantDetailView{}, err
	}
	return domainresource.ReferenceGrantDetailView{
		ReferenceGrantView: mapReferenceGrantResource(item),
		Labels:             item.GetLabels(),
		Annotations:        item.GetAnnotations(),
		FromRefs:           gatewayReferenceGrantFromViews(item),
		ToRefs:             gatewayReferenceGrantToViews(item),
	}, nil
}

func (c *Client) GetIngressClassDetail(ctx context.Context, name string) (domainresource.IngressClassDetailView, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	item, err := c.typed.NetworkingV1().IngressClasses().Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.IngressClassDetailView{}, err
	}
	related := make([]domainresource.IngressView, 0)
	continueToken := ""
	// ponytail: reverse lookups scan pages of 500; replace with an informer index if this becomes a hot path.
	for {
		ingresses, listErr := c.typed.NetworkingV1().Ingresses("").List(queryCtx, metav1.ListOptions{
			Limit: int64(agentTablePageSize), Continue: continueToken,
		})
		if listErr != nil {
			return domainresource.IngressClassDetailView{}, listErr
		}
		for _, ingress := range ingresses.Items {
			className := ingress.Annotations["kubernetes.io/ingress.class"]
			if ingress.Spec.IngressClassName != nil {
				className = *ingress.Spec.IngressClassName
			}
			if className == name {
				related = append(related, mapIngress(ingress))
			}
		}
		if ingresses.Continue == "" {
			break
		}
		if ingresses.Continue == continueToken {
			return domainresource.IngressClassDetailView{}, fmt.Errorf("ingress listing returned a repeated continue token")
		}
		continueToken = ingresses.Continue
	}
	return domainresource.IngressClassDetailView{
		IngressClassView: mapIngressClass(*item),
		Labels:           item.Labels,
		Annotations:      item.Annotations,
		Ingresses:        related,
	}, nil
}

func (c *Client) getDynamicResource(
	ctx context.Context,
	namespace string,
	group string,
	versions []string,
	resource string,
	name string,
) (unstructured.Unstructured, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var lastErr error
	for _, version := range versions {
		gvr := schema.GroupVersionResource{Group: group, Version: version, Resource: resource}
		var item *unstructured.Unstructured
		var err error
		if namespace == "" {
			item, err = c.dynamic.Resource(gvr).Get(queryCtx, name, metav1.GetOptions{})
		} else {
			item, err = c.dynamic.Resource(gvr).Namespace(namespace).Get(queryCtx, name, metav1.GetOptions{})
		}
		if err == nil {
			return *item, nil
		}
		if !isOptionalGatewayAPIResourceMissing(err) {
			return unstructured.Unstructured{}, err
		}
		lastErr = err
	}
	return unstructured.Unstructured{}, lastErr
}

func (c *Client) listAllNamespacedDynamicResources(
	ctx context.Context,
	group string,
	versions []string,
	resource string,
) ([]unstructured.Unstructured, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// ponytail: reverse lookups scan pages of 500; replace with an informer index if this becomes a hot path.
	for _, version := range versions {
		gvr := schema.GroupVersionResource{Group: group, Version: version, Resource: resource}
		items := make([]unstructured.Unstructured, 0)
		continueToken := ""
		for {
			page, err := c.dynamic.Resource(gvr).Namespace("").List(queryCtx, metav1.ListOptions{
				Limit: int64(agentTablePageSize), Continue: continueToken,
			})
			if err != nil {
				if isOptionalGatewayAPIResourceMissing(err) {
					break
				}
				return nil, err
			}
			items = append(items, page.Items...)
			if page.GetContinue() == "" {
				return items, nil
			}
			if page.GetContinue() == continueToken {
				return nil, fmt.Errorf("%s listing returned a repeated continue token", resource)
			}
			continueToken = page.GetContinue()
		}
	}
	return []unstructured.Unstructured{}, nil
}

func (c *Client) listClusterDynamicResources(ctx context.Context, group string, versions []string, resource string) ([]unstructured.Unstructured, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, version := range versions {
		gvr := schema.GroupVersionResource{Group: group, Version: version, Resource: resource}
		items, err := c.dynamic.Resource(gvr).List(queryCtx, metav1.ListOptions{})
		if err == nil {
			return items.Items, nil
		}
		if isOptionalGatewayAPIResourceMissing(err) {
			continue
		}
		return nil, err
	}
	return []unstructured.Unstructured{}, nil
}

func (c *Client) listNamespacedDynamicResources(ctx context.Context, namespace, group string, versions []string, resource string) ([]unstructured.Unstructured, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, version := range versions {
		gvr := schema.GroupVersionResource{Group: group, Version: version, Resource: resource}
		items, err := c.dynamic.Resource(gvr).Namespace(namespace).List(queryCtx, metav1.ListOptions{})
		if err == nil {
			return items.Items, nil
		}
		if isOptionalGatewayAPIResourceMissing(err) {
			continue
		}
		return nil, err
	}
	return []unstructured.Unstructured{}, nil
}

func isOptionalGatewayAPIResourceMissing(err error) bool {
	if err == nil {
		return false
	}
	if apierrors.IsNotFound(err) {
		return true
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "the server could not find the requested resource") ||
		strings.Contains(message, "no matches for kind") ||
		strings.Contains(message, "no resource type")
}

func mapGatewayClassResource(item unstructured.Unstructured) domainresource.GatewayClassView {
	controllerName, _, _ := unstructured.NestedString(item.Object, "spec", "controllerName")
	return domainresource.GatewayClassView{
		Name:           item.GetName(),
		ControllerName: controllerName,
		Accepted:       gatewayConditionStatus(item, "Accepted"),
		ParametersRef:  gatewayFormatObjectRef("", gatewayNestedMap(item.Object, "spec", "parametersRef")),
		AgeSeconds:     secondsSince(item.GetCreationTimestamp().Time),
	}
}

func mapGatewayResource(item unstructured.Unstructured) domainresource.GatewayView {
	className, _, _ := unstructured.NestedString(item.Object, "spec", "gatewayClassName")
	addressItems, _, _ := unstructured.NestedSlice(item.Object, "status", "addresses")
	addresses := make([]string, 0, len(addressItems))
	for _, raw := range addressItems {
		value, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		address, _ := value["value"].(string)
		address = strings.TrimSpace(address)
		if address != "" {
			addresses = append(addresses, address)
		}
	}
	listeners, _, _ := unstructured.NestedSlice(item.Object, "spec", "listeners")
	return domainresource.GatewayView{
		Name:          item.GetName(),
		Namespace:     item.GetNamespace(),
		GatewayClass:  className,
		Addresses:     addresses,
		ListenerCount: int32(len(listeners)),
		AgeSeconds:    secondsSince(item.GetCreationTimestamp().Time),
	}
}

func mapHTTPRouteResource(item unstructured.Unstructured) domainresource.HTTPRouteView {
	hostItems, _, _ := unstructured.NestedStringSlice(item.Object, "spec", "hostnames")
	ruleItems, _, _ := unstructured.NestedSlice(item.Object, "spec", "rules")

	parentRefs := gatewayExtractParentRefs(item)
	backendServices := gatewayExtractBackendServices(ruleItems)
	slices.Sort(backendServices)
	slices.Sort(hostItems)
	slices.Sort(parentRefs)

	return domainresource.HTTPRouteView{
		Name:            item.GetName(),
		Namespace:       item.GetNamespace(),
		Hostnames:       hostItems,
		ParentRefs:      parentRefs,
		BackendServices: backendServices,
		AgeSeconds:      secondsSince(item.GetCreationTimestamp().Time),
	}
}

func mapBackendTLSPolicyResource(item unstructured.Unstructured) domainresource.BackendTLSPolicyView {
	targetRefs := gatewayFormatObjectRefList(item.GetNamespace(), gatewayNestedSlice(item.Object, "spec", "targetRefs"))
	if len(targetRefs) == 0 {
		if targetRef := gatewayFormatObjectRef(item.GetNamespace(), gatewayNestedMap(item.Object, "spec", "targetRef")); targetRef != "" {
			targetRefs = append(targetRefs, targetRef)
		}
	}
	validation := gatewayNestedMap(item.Object, "spec", "validation")
	hostname, _ := validation["hostname"].(string)
	caCertificateRefs := gatewayFormatObjectRefList(item.GetNamespace(), gatewayNestedSlice(validation, "caCertificateRefs"))
	wellKnownCACertificates, _ := validation["wellKnownCACertificates"].(string)
	slices.Sort(targetRefs)
	slices.Sort(caCertificateRefs)
	return domainresource.BackendTLSPolicyView{
		Name:                    item.GetName(),
		Namespace:               item.GetNamespace(),
		TargetRefs:              targetRefs,
		Hostname:                strings.TrimSpace(hostname),
		CACertificateRefs:       caCertificateRefs,
		WellKnownCACertificates: strings.TrimSpace(wellKnownCACertificates),
		AgeSeconds:              secondsSince(item.GetCreationTimestamp().Time),
	}
}

func mapGRPCRouteResource(item unstructured.Unstructured) domainresource.GRPCRouteView {
	hostItems, _, _ := unstructured.NestedStringSlice(item.Object, "spec", "hostnames")
	ruleItems, _, _ := unstructured.NestedSlice(item.Object, "spec", "rules")
	parentRefs := gatewayExtractParentRefs(item)
	backendServices := gatewayExtractBackendServices(ruleItems)
	slices.Sort(backendServices)
	slices.Sort(hostItems)
	slices.Sort(parentRefs)
	return domainresource.GRPCRouteView{
		Name:            item.GetName(),
		Namespace:       item.GetNamespace(),
		Hostnames:       hostItems,
		ParentRefs:      parentRefs,
		BackendServices: backendServices,
		RuleCount:       int32(len(ruleItems)),
		AgeSeconds:      secondsSince(item.GetCreationTimestamp().Time),
	}
}

func mapReferenceGrantResource(item unstructured.Unstructured) domainresource.ReferenceGrantView {
	fromRefs := gatewayFormatObjectRefList(item.GetNamespace(), gatewayNestedSlice(item.Object, "spec", "from"))
	toRefs := gatewayFormatObjectRefList(item.GetNamespace(), gatewayNestedSlice(item.Object, "spec", "to"))
	slices.Sort(fromRefs)
	slices.Sort(toRefs)
	return domainresource.ReferenceGrantView{
		Name:       item.GetName(),
		Namespace:  item.GetNamespace(),
		From:       fromRefs,
		To:         toRefs,
		AgeSeconds: secondsSince(item.GetCreationTimestamp().Time),
	}
}

func (c *Client) gatewayRelatedRoutes(ctx context.Context, namespace, gatewayName string) ([]domainresource.GatewayRouteReferenceView, error) {
	types := []struct {
		kind     string
		versions []string
		resource string
		mapRoute func(unstructured.Unstructured) ([]string, string)
	}{
		{
			kind: "HTTPRoute", versions: httpRouteVersions, resource: "httproutes",
			mapRoute: func(item unstructured.Unstructured) ([]string, string) {
				view := mapHTTPRouteResource(item)
				return view.Hostnames, gatewayRouteAccepted(item, namespace, gatewayName)
			},
		},
		{
			kind: "GRPCRoute", versions: grpcRouteVersions, resource: "grpcroutes",
			mapRoute: func(item unstructured.Unstructured) ([]string, string) {
				view := mapGRPCRouteResource(item)
				return view.Hostnames, gatewayRouteAccepted(item, namespace, gatewayName)
			},
		},
	}
	routes := make([]domainresource.GatewayRouteReferenceView, 0)
	for _, routeType := range types {
		items, err := c.listAllNamespacedDynamicResources(
			ctx,
			"gateway.networking.k8s.io",
			routeType.versions,
			routeType.resource,
		)
		if err != nil {
			return nil, err
		}
		for _, item := range items {
			if !gatewayHasParent(item, namespace, gatewayName) {
				continue
			}
			hostnames, accepted := routeType.mapRoute(item)
			routes = append(routes, domainresource.GatewayRouteReferenceView{
				Kind: routeType.kind, Namespace: item.GetNamespace(), Name: item.GetName(),
				Hostnames: hostnames, Accepted: accepted,
			})
		}
	}
	return routes, nil
}

func gatewayListenerViews(item unstructured.Unstructured) []domainresource.GatewayListenerView {
	listenerItems := gatewayNestedSlice(item.Object, "spec", "listeners")
	statusItems := gatewayNestedSlice(item.Object, "status", "listeners")
	attached := make(map[string]int32, len(statusItems))
	conditions := make(map[string][]domainresource.WorkloadConditionView, len(statusItems))
	for _, raw := range statusItems {
		status, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := status["name"].(string)
		attached[name] = int32(gatewayInt64(status["attachedRoutes"]))
		conditions[name] = gatewayConditionViewsFromRaw(gatewayNestedSlice(status, "conditions"))
	}
	views := make([]domainresource.GatewayListenerView, 0, len(listenerItems))
	for _, raw := range listenerItems {
		listener, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := listener["name"].(string)
		protocol, _ := listener["protocol"].(string)
		hostname, _ := listener["hostname"].(string)
		tls := gatewayNestedMap(listener, "tls")
		tlsMode, _ := tls["mode"].(string)
		allowedKinds := gatewayFormatObjectRefList("", gatewayNestedSlice(listener, "allowedRoutes", "kinds"))
		views = append(views, domainresource.GatewayListenerView{
			Name:              name,
			Protocol:          protocol,
			Port:              int32(gatewayInt64(listener["port"])),
			Hostname:          hostname,
			TLSMode:           tlsMode,
			CertificateRefs:   gatewayFormatObjectRefList(item.GetNamespace(), gatewayNestedSlice(tls, "certificateRefs")),
			AllowedRouteKinds: allowedKinds,
			AttachedRoutes:    attached[name],
			Conditions:        conditions[name],
		})
	}
	return views
}

func gatewayRouteRuleViews(item unstructured.Unstructured) []domainresource.GatewayRouteRuleView {
	ruleItems := gatewayNestedSlice(item.Object, "spec", "rules")
	views := make([]domainresource.GatewayRouteRuleView, 0, len(ruleItems))
	for _, raw := range ruleItems {
		rule, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		views = append(views, domainresource.GatewayRouteRuleView{
			Matches:  gatewayJSONSummaries(gatewayNestedSlice(rule, "matches")),
			Filters:  gatewayJSONSummaries(gatewayNestedSlice(rule, "filters")),
			Backends: gatewayRouteBackendViews(item.GetNamespace(), gatewayNestedSlice(rule, "backendRefs")),
		})
	}
	return views
}

func gatewayRouteBackendViews(defaultNamespace string, rawBackends []any) []domainresource.GatewayRouteBackendView {
	views := make([]domainresource.GatewayRouteBackendView, 0, len(rawBackends))
	for _, raw := range rawBackends {
		backend, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name, _ := backend["name"].(string)
		kind, _ := backend["kind"].(string)
		namespace, _ := backend["namespace"].(string)
		if kind == "" {
			kind = "Service"
		}
		if namespace == "" {
			namespace = defaultNamespace
		}
		view := domainresource.GatewayRouteBackendView{
			Kind: kind, Namespace: namespace, Name: name,
			Port: int32(gatewayInt64(backend["port"])), Weight: int32(gatewayInt64(backend["weight"])),
		}
		views = append(views, view)
	}
	return views
}

func gatewayConditionViews(item unstructured.Unstructured, collectionField string) []domainresource.WorkloadConditionView {
	rawConditions := gatewayNestedSlice(item.Object, "status", "conditions")
	if collectionField != "" {
		for _, raw := range gatewayNestedSlice(item.Object, "status", collectionField) {
			parent, ok := raw.(map[string]any)
			if ok {
				rawConditions = append(rawConditions, gatewayNestedSlice(parent, "conditions")...)
			}
		}
	}
	return gatewayConditionViewsFromRaw(rawConditions)
}

func gatewayConditionViewsFromRaw(rawConditions []any) []domainresource.WorkloadConditionView {
	views := make([]domainresource.WorkloadConditionView, 0, len(rawConditions))
	for _, raw := range rawConditions {
		condition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		conditionType, _ := condition["type"].(string)
		status, _ := condition["status"].(string)
		reason, _ := condition["reason"].(string)
		message, _ := condition["message"].(string)
		lastTransitionTime, _ := condition["lastTransitionTime"].(string)
		views = append(views, domainresource.WorkloadConditionView{
			Type: conditionType, Status: status, Reason: reason, Message: message, LastTransitionTime: lastTransitionTime,
		})
	}
	return views
}

func gatewayRouteParentStatusViews(item unstructured.Unstructured) []domainresource.GatewayRouteParentStatusView {
	parents := gatewayNestedSlice(item.Object, "status", "parents")
	views := make([]domainresource.GatewayRouteParentStatusView, 0, len(parents))
	for _, raw := range parents {
		parent, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		controllerName, _ := parent["controllerName"].(string)
		parentRef := gatewayNestedMap(parent, "parentRef")
		views = append(views, domainresource.GatewayRouteParentStatusView{
			ParentRef:      gatewayParentRefName(item.GetNamespace(), parentRef),
			ControllerName: controllerName,
			Conditions:     gatewayConditionViewsFromRaw(gatewayNestedSlice(parent, "conditions")),
		})
	}
	return views
}

func gatewayParentRefName(defaultNamespace string, ref map[string]any) string {
	name, _ := ref["name"].(string)
	namespace, _ := ref["namespace"].(string)
	if namespace == "" {
		namespace = defaultNamespace
	}
	return strings.Trim(strings.Join([]string{namespace, name}, "/"), "/")
}

func gatewayReferenceGrantFromViews(item unstructured.Unstructured) []domainresource.ReferenceGrantFromView {
	rawRefs := gatewayNestedSlice(item.Object, "spec", "from")
	views := make([]domainresource.ReferenceGrantFromView, 0, len(rawRefs))
	for _, raw := range rawRefs {
		ref, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		group, _ := ref["group"].(string)
		kind, _ := ref["kind"].(string)
		namespace, _ := ref["namespace"].(string)
		views = append(views, domainresource.ReferenceGrantFromView{Group: group, Kind: kind, Namespace: namespace})
	}
	return views
}

func gatewayReferenceGrantToViews(item unstructured.Unstructured) []domainresource.ReferenceGrantToView {
	rawRefs := gatewayNestedSlice(item.Object, "spec", "to")
	views := make([]domainresource.ReferenceGrantToView, 0, len(rawRefs))
	for _, raw := range rawRefs {
		ref, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		group, _ := ref["group"].(string)
		kind, _ := ref["kind"].(string)
		name, _ := ref["name"].(string)
		views = append(views, domainresource.ReferenceGrantToView{Group: group, Kind: kind, Name: name})
	}
	return views
}

func gatewayHasParent(item unstructured.Unstructured, namespace, gatewayName string) bool {
	needle := namespace + "/" + gatewayName
	return slices.Contains(gatewayExtractParentRefs(item), needle)
}

func gatewayRouteAccepted(item unstructured.Unstructured, namespace, gatewayName string) string {
	for _, raw := range gatewayNestedSlice(item.Object, "status", "parents") {
		parent, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		ref := gatewayNestedMap(parent, "parentRef")
		name, _ := ref["name"].(string)
		kind, _ := ref["kind"].(string)
		refNamespace, _ := ref["namespace"].(string)
		if kind == "" {
			kind = "Gateway"
		}
		if refNamespace == "" {
			refNamespace = item.GetNamespace()
		}
		if !strings.EqualFold(kind, "Gateway") || refNamespace != namespace || name != gatewayName {
			continue
		}
		for _, rawCondition := range gatewayNestedSlice(parent, "conditions") {
			condition, ok := rawCondition.(map[string]any)
			if ok && condition["type"] == "Accepted" {
				status, _ := condition["status"].(string)
				return status
			}
		}
	}
	return ""
}

func gatewayJSONSummaries(items []any) []string {
	values := make([]string, 0, len(items))
	for _, item := range items {
		encoded, err := json.Marshal(item)
		if err == nil {
			values = append(values, string(encoded))
		}
	}
	return values
}

func gatewayInt64(value any) int64 {
	switch typed := value.(type) {
	case int64:
		return typed
	case int32:
		return int64(typed)
	case int:
		return int64(typed)
	case float64:
		return int64(typed)
	default:
		return 0
	}
}

func gatewayExtractParentRefs(item unstructured.Unstructured) []string {
	parentItems, _, _ := unstructured.NestedSlice(item.Object, "spec", "parentRefs")
	parentRefs := make([]string, 0, len(parentItems))
	for _, raw := range parentItems {
		value, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		parentName, _ := value["name"].(string)
		parentName = strings.TrimSpace(parentName)
		if parentName == "" {
			continue
		}
		parentKind, _ := value["kind"].(string)
		if parentKind != "" && !strings.EqualFold(parentKind, "Gateway") {
			continue
		}
		parentNamespace, _ := value["namespace"].(string)
		parentNamespace = strings.TrimSpace(parentNamespace)
		if parentNamespace == "" {
			parentNamespace = item.GetNamespace()
		}
		parentRefs = append(parentRefs, fmt.Sprintf("%s/%s", parentNamespace, parentName))
	}
	return parentRefs
}

func gatewayExtractBackendServices(ruleItems []any) []string {
	backendServiceSet := make(map[string]struct{})
	for _, rawRule := range ruleItems {
		rule, ok := rawRule.(map[string]any)
		if !ok {
			continue
		}
		backendRefs, _, _ := unstructured.NestedSlice(rule, "backendRefs")
		for _, rawBackend := range backendRefs {
			backend, ok := rawBackend.(map[string]any)
			if !ok {
				continue
			}
			backendName, _ := backend["name"].(string)
			backendName = strings.TrimSpace(backendName)
			if backendName == "" {
				continue
			}
			backendKind, _ := backend["kind"].(string)
			if backendKind != "" && !strings.EqualFold(backendKind, "Service") {
				continue
			}
			backendGroup, _ := backend["group"].(string)
			if backendGroup != "" && !strings.EqualFold(backendGroup, "core") {
				continue
			}
			backendServiceSet[backendName] = struct{}{}
		}
	}
	backendServices := make([]string, 0, len(backendServiceSet))
	for serviceName := range backendServiceSet {
		backendServices = append(backendServices, serviceName)
	}
	return backendServices
}

func gatewayNestedMap(object map[string]any, fields ...string) map[string]any {
	value, _, _ := unstructured.NestedMap(object, fields...)
	return value
}

func gatewayNestedSlice(object map[string]any, fields ...string) []any {
	value, _, _ := unstructured.NestedSlice(object, fields...)
	return value
}

func gatewayFormatObjectRef(defaultNamespace string, ref map[string]any) string {
	if len(ref) == 0 {
		return ""
	}
	name, _ := ref["name"].(string)
	name = strings.TrimSpace(name)
	kind, _ := ref["kind"].(string)
	kind = strings.TrimSpace(kind)
	group, _ := ref["group"].(string)
	group = strings.TrimSpace(group)
	namespace, _ := ref["namespace"].(string)
	namespace = strings.TrimSpace(namespace)
	if namespace == "" {
		namespace = defaultNamespace
	}
	label := strings.Trim(kind, "/")
	if group != "" {
		if label == "" {
			label = group
		} else {
			label = fmt.Sprintf("%s.%s", label, group)
		}
	}
	if name != "" {
		if label == "" {
			label = name
		} else {
			label = fmt.Sprintf("%s/%s", label, name)
		}
	}
	if namespace != "" {
		if label == "" {
			label = namespace
		} else {
			label = fmt.Sprintf("%s:%s", namespace, label)
		}
	}
	return label
}

func gatewayFormatObjectRefList(defaultNamespace string, rawRefs []any) []string {
	refs := make([]string, 0, len(rawRefs))
	seen := make(map[string]struct{}, len(rawRefs))
	for _, raw := range rawRefs {
		ref, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		label := gatewayFormatObjectRef(defaultNamespace, ref)
		if label == "" {
			continue
		}
		if _, exists := seen[label]; exists {
			continue
		}
		seen[label] = struct{}{}
		refs = append(refs, label)
	}
	return refs
}

func gatewayConditionStatus(item unstructured.Unstructured, conditionType string) string {
	conditions, _, _ := unstructured.NestedSlice(item.Object, "status", "conditions")
	for _, raw := range conditions {
		condition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		currentType, _ := condition["type"].(string)
		if currentType != conditionType {
			continue
		}
		status, _ := condition["status"].(string)
		return strings.TrimSpace(status)
	}
	return ""
}
