package resource

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"

	domainaccess "github.com/kubecrux/kubecrux/internal/domain/access"
	domaincluster "github.com/kubecrux/kubecrux/internal/domain/cluster"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainresource "github.com/kubecrux/kubecrux/internal/domain/resource"
	"github.com/kubecrux/kubecrux/internal/platform/apperrors"
)

var gatewayAPIVersions = []string{"v1", "v1beta1"}
var httpRouteAPIVersions = []string{"v1", "v1beta1"}

func (s *Service) ListGateways(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.GatewayView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Gateway", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.GatewayView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListGateways(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		items, err = s.listDirectGateways(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.GatewayView) string { return item.Namespace })
	populateAllowedActionsGateways(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Gateway", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed gateways via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) ListHTTPRoutes(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.HTTPRouteView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "HTTPRoute", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.HTTPRouteView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListHTTPRoutes(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		items, err = s.listDirectHTTPRoutes(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.HTTPRouteView) string { return item.Namespace })
	populateAllowedActionsHTTPRoutes(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "HTTPRoute", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed httproutes via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}

func (s *Service) listDirectGateways(ctx context.Context, clusterID, namespace string) ([]domainresource.GatewayView, error) {
	items, err := s.listDynamicNamespacedResources(ctx, clusterID, namespace, "gateway.networking.k8s.io", gatewayAPIVersions, "gateways")
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.GatewayView, 0, len(items))
	for _, item := range items {
		views = append(views, mapGateway(item))
	}
	return views, nil
}

func (s *Service) listDirectHTTPRoutes(ctx context.Context, clusterID, namespace string) ([]domainresource.HTTPRouteView, error) {
	items, err := s.listDynamicNamespacedResources(ctx, clusterID, namespace, "gateway.networking.k8s.io", httpRouteAPIVersions, "httproutes")
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.HTTPRouteView, 0, len(items))
	for _, item := range items {
		views = append(views, mapHTTPRoute(item))
	}
	return views, nil
}

func (s *Service) listDynamicNamespacedResources(ctx context.Context, clusterID, namespace, group string, versions []string, resource string) ([]unstructured.Unstructured, error) {
	bundle, err := s.clusters.Bundle(ctx, clusterID)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
	}
	queryCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var lastErr error
	for _, version := range versions {
		gvr := schema.GroupVersionResource{Group: group, Version: version, Resource: resource}
		items, listErr := bundle.Dynamic.Resource(gvr).Namespace(namespace).List(queryCtx, metav1.ListOptions{})
		if listErr == nil {
			return items.Items, nil
		}
		lastErr = listErr
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("resource %s/%s not available", group, resource)
	}
	return nil, lastErr
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
		ListenerCount: int32(len(listeners)),
		AgeSeconds:    secondsSince(item.GetCreationTimestamp().Time),
	}
}

func mapHTTPRoute(item unstructured.Unstructured) domainresource.HTTPRouteView {
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

func populateAllowedActionsGateways(items []domainresource.GatewayView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}

func populateAllowedActionsHTTPRoutes(items []domainresource.HTTPRouteView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}
