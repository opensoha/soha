package kubernetes

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	domainresource "github.com/kubecrux/kubecrux/internal/domain/resource"
)

var gatewayVersions = []string{"v1", "v1beta1"}

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
