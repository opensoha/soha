package resourcebackend

import (
	"context"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	appresource "github.com/opensoha/soha/internal/application/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var gatewayAPIVersions = []string{"v1", "v1beta1"}
var httpRouteAPIVersions = []string{"v1", "v1beta1"}
var backendTLSPolicyAPIVersions = []string{"v1", "v1alpha3"}
var grpcRouteAPIVersions = []string{"v1", "v1alpha2"}
var referenceGrantAPIVersions = []string{"v1", "v1beta1", "v1alpha2"}

func (d *Direct) ListGatewayClasses(ctx context.Context, clusterID string) ([]domainresource.GatewayClassView, error) {
	items, err := d.listGatewayResources(ctx, clusterID, "", gatewayAPIVersions, "gatewayclasses", false)
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.GatewayClassView, 0, len(items))
	for _, item := range items {
		views = append(views, mapGatewayClass(item))
	}
	return views, nil
}

func (d *Direct) GetGatewayClassDetail(ctx context.Context, clusterID, name string) (domainresource.GatewayClassDetailView, error) {
	item, err := d.getGatewayResource(ctx, clusterID, "", name, gatewayAPIVersions, "gatewayclasses", false)
	if err != nil {
		return domainresource.GatewayClassDetailView{}, err
	}
	gatewayItems, err := d.listGatewayRelationshipPage(ctx, clusterID, "", gatewayAPIVersions, "gateways")
	if err != nil {
		return domainresource.GatewayClassDetailView{}, err
	}
	matched := make([]domainresource.GatewayView, 0)
	for _, item := range gatewayItems {
		gateway := mapGateway(item)
		if gateway.GatewayClass == name {
			matched = append(matched, gateway)
		}
	}
	return domainresource.GatewayClassDetailView{GatewayClassView: mapGatewayClass(item), Labels: item.GetLabels(), Annotations: item.GetAnnotations(), Conditions: gatewayConditions(item), Gateways: matched}, nil
}

func (d *Direct) ListGateways(ctx context.Context, clusterID, namespace string) ([]domainresource.GatewayView, error) {
	items, err := d.listGatewayResources(ctx, clusterID, namespace, gatewayAPIVersions, "gateways", true)
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.GatewayView, 0, len(items))
	for _, item := range items {
		views = append(views, mapGateway(item))
	}
	return views, nil
}

func (d *Direct) GetGatewayDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.GatewayDetailView, error) {
	item, err := d.getGatewayResource(ctx, clusterID, namespace, name, gatewayAPIVersions, "gateways", true)
	if err != nil {
		return domainresource.GatewayDetailView{}, err
	}
	routes, err := d.gatewayRouteReferences(ctx, clusterID, namespace, name)
	if err != nil {
		return domainresource.GatewayDetailView{}, err
	}
	return domainresource.GatewayDetailView{GatewayView: mapGateway(item), Labels: item.GetLabels(), Annotations: item.GetAnnotations(), Conditions: gatewayConditions(item), Listeners: gatewayListeners(item), Routes: routes}, nil
}

func (d *Direct) ListHTTPRoutes(ctx context.Context, clusterID, namespace string) ([]domainresource.HTTPRouteView, error) {
	items, err := d.listGatewayResources(ctx, clusterID, namespace, httpRouteAPIVersions, "httproutes", true)
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.HTTPRouteView, 0, len(items))
	for _, item := range items {
		views = append(views, mapHTTPRoute(item))
	}
	return views, nil
}

func (d *Direct) GetHTTPRouteDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.HTTPRouteDetailView, error) {
	item, err := d.getGatewayResource(ctx, clusterID, namespace, name, httpRouteAPIVersions, "httproutes", true)
	if err != nil {
		return domainresource.HTTPRouteDetailView{}, err
	}
	detail := domainresource.HTTPRouteDetailView{HTTPRouteView: mapHTTPRoute(item), Labels: item.GetLabels(), Annotations: item.GetAnnotations(), Conditions: gatewayConditions(item), ParentStatuses: gatewayParentStatuses(item), Rules: gatewayRouteRules(item, "HTTPRoute")}
	enrichGatewayBackends(ctx, d, clusterID, &detail.Rules)
	return detail, nil
}

func (d *Direct) ListBackendTLSPolicies(ctx context.Context, clusterID, namespace string) ([]domainresource.BackendTLSPolicyView, error) {
	items, err := d.listGatewayResources(ctx, clusterID, namespace, backendTLSPolicyAPIVersions, "backendtlspolicies", true)
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.BackendTLSPolicyView, 0, len(items))
	for _, item := range items {
		views = append(views, mapBackendTLSPolicy(item))
	}
	return views, nil
}

func (d *Direct) GetBackendTLSPolicyDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.BackendTLSPolicyDetailView, error) {
	item, err := d.getGatewayResource(ctx, clusterID, namespace, name, backendTLSPolicyAPIVersions, "backendtlspolicies", true)
	if err != nil {
		return domainresource.BackendTLSPolicyDetailView{}, err
	}
	return domainresource.BackendTLSPolicyDetailView{BackendTLSPolicyView: mapBackendTLSPolicy(item), Labels: item.GetLabels(), Annotations: item.GetAnnotations(), Conditions: gatewayConditions(item)}, nil
}

func (d *Direct) ListGRPCRoutes(ctx context.Context, clusterID, namespace string) ([]domainresource.GRPCRouteView, error) {
	items, err := d.listGatewayResources(ctx, clusterID, namespace, grpcRouteAPIVersions, "grpcroutes", true)
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.GRPCRouteView, 0, len(items))
	for _, item := range items {
		views = append(views, mapGRPCRoute(item))
	}
	return views, nil
}

func (d *Direct) GetGRPCRouteDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.GRPCRouteDetailView, error) {
	item, err := d.getGatewayResource(ctx, clusterID, namespace, name, grpcRouteAPIVersions, "grpcroutes", true)
	if err != nil {
		return domainresource.GRPCRouteDetailView{}, err
	}
	detail := domainresource.GRPCRouteDetailView{GRPCRouteView: mapGRPCRoute(item), Labels: item.GetLabels(), Annotations: item.GetAnnotations(), Conditions: gatewayConditions(item), ParentStatuses: gatewayParentStatuses(item), Rules: gatewayRouteRules(item, "GRPCRoute")}
	enrichGatewayBackends(ctx, d, clusterID, &detail.Rules)
	return detail, nil
}

func (d *Direct) ListReferenceGrants(ctx context.Context, clusterID, namespace string) ([]domainresource.ReferenceGrantView, error) {
	items, err := d.listGatewayResources(ctx, clusterID, namespace, referenceGrantAPIVersions, "referencegrants", true)
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.ReferenceGrantView, 0, len(items))
	for _, item := range items {
		views = append(views, mapReferenceGrant(item))
	}
	return views, nil
}

func (d *Direct) GetReferenceGrantDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.ReferenceGrantDetailView, error) {
	item, err := d.getGatewayResource(ctx, clusterID, namespace, name, referenceGrantAPIVersions, "referencegrants", true)
	if err != nil {
		return domainresource.ReferenceGrantDetailView{}, err
	}
	return domainresource.ReferenceGrantDetailView{ReferenceGrantView: mapReferenceGrant(item), Labels: item.GetLabels(), Annotations: item.GetAnnotations(), FromRefs: referenceGrantFromRefs(item), ToRefs: referenceGrantToRefs(item)}, nil
}

func (d *Direct) getGatewayResource(ctx context.Context, clusterID, namespace, name string, versions []string, resource string, namespaced bool) (unstructured.Unstructured, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return unstructured.Unstructured{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, version := range versions {
		client := bundle.Dynamic.Resource(schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: version, Resource: resource})
		var item *unstructured.Unstructured
		if namespaced {
			item, err = client.Namespace(namespace).Get(queryCtx, name, metav1.GetOptions{})
		} else {
			item, err = client.Get(queryCtx, name, metav1.GetOptions{})
		}
		if err == nil {
			return *item, nil
		}
		if !isOptionalGatewayAPIResourceMissing(err) {
			return unstructured.Unstructured{}, err
		}
	}
	return unstructured.Unstructured{}, apierrors.NewNotFound(schema.GroupResource{Group: "gateway.networking.k8s.io", Resource: resource}, name)
}

func (d *Direct) listGatewayRelationshipPage(ctx context.Context, clusterID, namespace string, versions []string, resource string) ([]unstructured.Unstructured, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// ponytail: Gateway API has no portable field selector for these relations; cap detail enrichment until an informer index owns it.
	const relationshipLimit = 500
	for _, version := range versions {
		client := bundle.Dynamic.Resource(schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: version, Resource: resource})
		var items *unstructured.UnstructuredList
		if namespace == "" {
			items, err = client.List(queryCtx, metav1.ListOptions{Limit: relationshipLimit})
		} else {
			items, err = client.Namespace(namespace).List(queryCtx, metav1.ListOptions{Limit: relationshipLimit})
		}
		if err == nil {
			return items.Items, nil
		}
		if !isOptionalGatewayAPIResourceMissing(err) {
			return nil, err
		}
	}
	return []unstructured.Unstructured{}, nil
}

func (d *Direct) listGatewayResources(ctx context.Context, clusterID, namespace string, versions []string, resource string, namespaced bool) ([]unstructured.Unstructured, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for _, version := range versions {
		gvr := schema.GroupVersionResource{Group: "gateway.networking.k8s.io", Version: version, Resource: resource}
		var items *unstructured.UnstructuredList
		var listErr error
		if namespaced {
			items, listErr = bundle.Dynamic.Resource(gvr).Namespace(namespace).List(queryCtx, metav1.ListOptions{})
		} else {
			items, listErr = bundle.Dynamic.Resource(gvr).List(queryCtx, metav1.ListOptions{})
		}
		if listErr == nil {
			return items.Items, nil
		}
		if isOptionalGatewayAPIResourceMissing(listErr) {
			continue
		}
		return nil, listErr
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

func mapGatewayClass(item unstructured.Unstructured) domainresource.GatewayClassView {
	controllerName, _, _ := unstructured.NestedString(item.Object, "spec", "controllerName")
	parametersRef := formatObjectRef("", nestedMap(item.Object, "spec", "parametersRef"))
	return domainresource.GatewayClassView{
		Name:           item.GetName(),
		ControllerName: controllerName,
		Accepted:       conditionStatus(item, "Accepted"),
		ParametersRef:  parametersRef,
		AgeSeconds:     secondsSince(item.GetCreationTimestamp().Time),
	}
}

func mapGateway(item unstructured.Unstructured) domainresource.GatewayView {
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
		ListenerCount: boundedInt32(len(listeners)),
		AgeSeconds:    secondsSince(item.GetCreationTimestamp().Time),
	}
}

func mapHTTPRoute(item unstructured.Unstructured) domainresource.HTTPRouteView {
	hostItems, _, _ := unstructured.NestedStringSlice(item.Object, "spec", "hostnames")
	ruleItems, _, _ := unstructured.NestedSlice(item.Object, "spec", "rules")

	parentRefs := extractGatewayParentRefs(item)
	backendServices := extractBackendServices(ruleItems)
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

func mapBackendTLSPolicy(item unstructured.Unstructured) domainresource.BackendTLSPolicyView {
	targetRefs := formatObjectRefList(item.GetNamespace(), nestedSlice(item.Object, "spec", "targetRefs"))
	if len(targetRefs) == 0 {
		if targetRef := formatObjectRef(item.GetNamespace(), nestedMap(item.Object, "spec", "targetRef")); targetRef != "" {
			targetRefs = append(targetRefs, targetRef)
		}
	}
	validation := nestedMap(item.Object, "spec", "validation")
	hostname, _ := validation["hostname"].(string)
	caCertificateRefs := formatObjectRefList(item.GetNamespace(), nestedSlice(validation, "caCertificateRefs"))
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

func mapGRPCRoute(item unstructured.Unstructured) domainresource.GRPCRouteView {
	hostItems, _, _ := unstructured.NestedStringSlice(item.Object, "spec", "hostnames")
	ruleItems, _, _ := unstructured.NestedSlice(item.Object, "spec", "rules")
	parentRefs := extractGatewayParentRefs(item)
	backendServices := extractBackendServices(ruleItems)
	slices.Sort(backendServices)
	slices.Sort(hostItems)
	slices.Sort(parentRefs)
	return domainresource.GRPCRouteView{
		Name:            item.GetName(),
		Namespace:       item.GetNamespace(),
		Hostnames:       hostItems,
		ParentRefs:      parentRefs,
		BackendServices: backendServices,
		RuleCount:       boundedInt32(len(ruleItems)),
		AgeSeconds:      secondsSince(item.GetCreationTimestamp().Time),
	}
}

func boundedInt32(value int) int32 {
	if value > math.MaxInt32 {
		return math.MaxInt32
	}
	if value < math.MinInt32 {
		return math.MinInt32
	}
	return int32(value)
}

func mapReferenceGrant(item unstructured.Unstructured) domainresource.ReferenceGrantView {
	fromRefs := formatObjectRefList(item.GetNamespace(), nestedSlice(item.Object, "spec", "from"))
	toRefs := formatObjectRefList(item.GetNamespace(), nestedSlice(item.Object, "spec", "to"))
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

func extractGatewayParentRefs(item unstructured.Unstructured) []string {
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

func extractBackendServices(ruleItems []any) []string {
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

func nestedMap(object map[string]any, fields ...string) map[string]any {
	value, _, _ := unstructured.NestedMap(object, fields...)
	return value
}

func nestedSlice(object map[string]any, fields ...string) []any {
	value, _, _ := unstructured.NestedSlice(object, fields...)
	return value
}

func formatObjectRef(defaultNamespace string, ref map[string]any) string {
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

func formatObjectRefList(defaultNamespace string, rawRefs []any) []string {
	refs := make([]string, 0, len(rawRefs))
	seen := make(map[string]struct{}, len(rawRefs))
	for _, raw := range rawRefs {
		ref, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		label := formatObjectRef(defaultNamespace, ref)
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

func conditionStatus(item unstructured.Unstructured, conditionType string) string {
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

func gatewayConditions(item unstructured.Unstructured) []domainresource.WorkloadConditionView {
	return mapGatewayConditions(nestedSlice(item.Object, "status", "conditions"))
}

func mapGatewayConditions(rawConditions []any) []domainresource.WorkloadConditionView {
	conditions := make([]domainresource.WorkloadConditionView, 0, len(rawConditions))
	for _, raw := range rawConditions {
		condition, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		conditions = append(conditions, domainresource.WorkloadConditionView{
			Type: stringValue(condition, "type"), Status: stringValue(condition, "status"),
			Reason: stringValue(condition, "reason"), Message: stringValue(condition, "message"),
			LastTransitionTime: stringValue(condition, "lastTransitionTime"),
		})
	}
	return conditions
}

func gatewayListeners(item unstructured.Unstructured) []domainresource.GatewayListenerView {
	statusByName := make(map[string]map[string]any)
	for _, raw := range nestedSlice(item.Object, "status", "listeners") {
		status, ok := raw.(map[string]any)
		if ok {
			statusByName[stringValue(status, "name")] = status
		}
	}
	listeners := make([]domainresource.GatewayListenerView, 0)
	for _, raw := range nestedSlice(item.Object, "spec", "listeners") {
		listener, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name := stringValue(listener, "name")
		status := statusByName[name]
		view := domainresource.GatewayListenerView{Name: name, Protocol: stringValue(listener, "protocol"), Port: int32Value(listener, "port"), Hostname: stringValue(listener, "hostname"), AttachedRoutes: int32Value(status, "attachedRoutes"), Conditions: mapGatewayConditions(nestedMapSlice(status, "conditions"))}
		tls := mapValue(listener, "tls")
		view.TLSMode = stringValue(tls, "mode")
		view.CertificateRefs = formatObjectRefList(item.GetNamespace(), nestedMapSlice(tls, "certificateRefs"))
		for _, rawKind := range nestedMapSlice(mapValue(listener, "allowedRoutes"), "kinds") {
			if kind, ok := rawKind.(map[string]any); ok {
				view.AllowedRouteKinds = append(view.AllowedRouteKinds, formatGroupKind(kind))
			}
		}
		listeners = append(listeners, view)
	}
	return listeners
}

func (d *Direct) gatewayRouteReferences(ctx context.Context, clusterID, namespace, gatewayName string) ([]domainresource.GatewayRouteReferenceView, error) {
	refs := make([]domainresource.GatewayRouteReferenceView, 0)
	httpItems, err := d.listGatewayRelationshipPage(ctx, clusterID, namespace, httpRouteAPIVersions, "httproutes")
	if err != nil {
		return nil, err
	}
	for _, item := range httpItems {
		route := mapHTTPRoute(item)
		if slices.Contains(route.ParentRefs, namespace+"/"+gatewayName) {
			refs = append(refs, domainresource.GatewayRouteReferenceView{Kind: "HTTPRoute", Namespace: route.Namespace, Name: route.Name, Hostnames: route.Hostnames})
		}
	}
	grpcItems, err := d.listGatewayRelationshipPage(ctx, clusterID, namespace, grpcRouteAPIVersions, "grpcroutes")
	if err != nil {
		return nil, err
	}
	for _, item := range grpcItems {
		route := mapGRPCRoute(item)
		if slices.Contains(route.ParentRefs, namespace+"/"+gatewayName) {
			refs = append(refs, domainresource.GatewayRouteReferenceView{Kind: "GRPCRoute", Namespace: route.Namespace, Name: route.Name, Hostnames: route.Hostnames})
		}
	}
	return refs, nil
}

func gatewayParentStatuses(item unstructured.Unstructured) []domainresource.GatewayRouteParentStatusView {
	parents := nestedSlice(item.Object, "status", "parents")
	out := make([]domainresource.GatewayRouteParentStatusView, 0, len(parents))
	for _, raw := range parents {
		parent, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, domainresource.GatewayRouteParentStatusView{ParentRef: formatObjectRef(item.GetNamespace(), mapValue(parent, "parentRef")), ControllerName: stringValue(parent, "controllerName"), Conditions: mapGatewayConditions(nestedMapSlice(parent, "conditions"))})
	}
	return out
}

func gatewayRouteRules(item unstructured.Unstructured, kind string) []domainresource.GatewayRouteRuleView {
	rawRules := nestedSlice(item.Object, "spec", "rules")
	rules := make([]domainresource.GatewayRouteRuleView, 0, len(rawRules))
	for _, raw := range rawRules {
		rule, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		view := domainresource.GatewayRouteRuleView{}
		for _, rawMatch := range nestedMapSlice(rule, "matches") {
			if match, ok := rawMatch.(map[string]any); ok {
				view.Matches = append(view.Matches, gatewayMatchLabel(match, kind))
			}
		}
		for _, rawFilter := range nestedMapSlice(rule, "filters") {
			if filter, ok := rawFilter.(map[string]any); ok {
				if label := stringValue(filter, "type"); label != "" {
					view.Filters = append(view.Filters, label)
				}
			}
		}
		for _, rawBackend := range nestedMapSlice(rule, "backendRefs") {
			backend, ok := rawBackend.(map[string]any)
			if !ok || stringValue(backend, "name") == "" {
				continue
			}
			ns := stringValue(backend, "namespace")
			if ns == "" {
				ns = item.GetNamespace()
			}
			backendKind := stringValue(backend, "kind")
			if backendKind == "" {
				backendKind = "Service"
			}
			view.Backends = append(view.Backends, domainresource.GatewayRouteBackendView{Kind: backendKind, Namespace: ns, Name: stringValue(backend, "name"), Port: int32Value(backend, "port"), Weight: int32Value(backend, "weight")})
		}
		rules = append(rules, view)
	}
	return rules
}

func enrichGatewayBackends(ctx context.Context, direct *Direct, clusterID string, rules *[]domainresource.GatewayRouteRuleView) {
	for ruleIndex := range *rules {
		for backendIndex := range (*rules)[ruleIndex].Backends {
			backend := &(*rules)[ruleIndex].Backends[backendIndex]
			if !strings.EqualFold(backend.Kind, "Service") {
				continue
			}
			service, err := direct.GetServiceDetail(ctx, clusterID, backend.Namespace, backend.Name)
			if err != nil {
				continue
			}
			backend.Endpoints = service.Endpoints
			backend.BackendPods = service.BackendPods
		}
	}
}

func gatewayMatchLabel(match map[string]any, kind string) string {
	if strings.EqualFold(kind, "GRPCRoute") {
		method := mapValue(match, "method")
		service, name := stringValue(method, "service"), stringValue(method, "method")
		return strings.Trim(strings.Join([]string{service, name}, "/"), "/")
	}
	path := mapValue(match, "path")
	value := stringValue(path, "value")
	if method := stringValue(match, "method"); method != "" {
		return strings.TrimSpace(method + " " + value)
	}
	return value
}

func referenceGrantFromRefs(item unstructured.Unstructured) []domainresource.ReferenceGrantFromView {
	refs := make([]domainresource.ReferenceGrantFromView, 0)
	for _, raw := range nestedSlice(item.Object, "spec", "from") {
		if ref, ok := raw.(map[string]any); ok {
			refs = append(refs, domainresource.ReferenceGrantFromView{Group: stringValue(ref, "group"), Kind: stringValue(ref, "kind"), Namespace: stringValue(ref, "namespace")})
		}
	}
	return refs
}

func referenceGrantToRefs(item unstructured.Unstructured) []domainresource.ReferenceGrantToView {
	refs := make([]domainresource.ReferenceGrantToView, 0)
	for _, raw := range nestedSlice(item.Object, "spec", "to") {
		if ref, ok := raw.(map[string]any); ok {
			refs = append(refs, domainresource.ReferenceGrantToView{Group: stringValue(ref, "group"), Kind: stringValue(ref, "kind"), Name: stringValue(ref, "name")})
		}
	}
	return refs
}

func mapValue(object map[string]any, key string) map[string]any {
	value, _ := object[key].(map[string]any)
	return value
}
func nestedMapSlice(object map[string]any, key string) []any {
	value, _ := object[key].([]any)
	return value
}
func stringValue(object map[string]any, key string) string {
	value, _ := object[key].(string)
	return strings.TrimSpace(value)
}
func int32Value(object map[string]any, key string) int32 {
	value, _ := object[key].(int64)
	return boundedInt32(int(value))
}
func formatGroupKind(ref map[string]any) string {
	group, kind := stringValue(ref, "group"), stringValue(ref, "kind")
	if group == "" {
		return kind
	}
	return group + "/" + kind
}

var _ appresource.DirectGatewayReader = (*Direct)(nil)
