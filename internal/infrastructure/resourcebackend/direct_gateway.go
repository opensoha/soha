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

var _ appresource.DirectGatewayReader = (*Direct)(nil)
