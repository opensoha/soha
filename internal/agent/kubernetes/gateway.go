package kubernetes

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	domainresource "github.com/kubecrux/kubecrux/internal/domain/resource"
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
