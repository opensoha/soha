package kubernetes

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	domainresource "github.com/kubecrux/kubecrux/internal/domain/resource"
)

var gatewayVersions = []string{"v1", "v1beta1"}
var httpRouteVersions = []string{"v1", "v1beta1"}

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

func (c *Client) listNamespacedDynamicResources(ctx context.Context, namespace, group string, versions []string, resource string) ([]unstructured.Unstructured, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var lastErr error
	for _, version := range versions {
		gvr := schema.GroupVersionResource{Group: group, Version: version, Resource: resource}
		items, err := c.dynamic.Resource(gvr).Namespace(namespace).List(queryCtx, metav1.ListOptions{})
		if err == nil {
			return items.Items, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("resource %s/%s not available", group, resource)
	}
	return nil, lastErr
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
