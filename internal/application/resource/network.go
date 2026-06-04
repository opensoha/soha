package resource

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	domainaccess "github.com/soha/soha/internal/domain/access"
	domaincluster "github.com/soha/soha/internal/domain/cluster"
	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainresource "github.com/soha/soha/internal/domain/resource"
	informerinfra "github.com/soha/soha/internal/infrastructure/informer"
	k8sinfra "github.com/soha/soha/internal/infrastructure/kubernetes"
	"github.com/soha/soha/internal/platform/apperrors"
)

func (s *Service) ListServices(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.ServiceView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Service", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.ServiceView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListServices(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, rawSource, err := s.listDirectServices(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.ServiceView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapService(item, decision))
		}
		source = rawSource
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.ServiceView) string { return item.Namespace })
	populateAllowedActionsServices(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Service", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed services via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}
func (s *Service) ListIngresses(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.IngressView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "Ingress", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.IngressView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListIngresses(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, rawSource, err := s.listDirectIngresses(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.IngressView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapIngress(item, decision))
		}
		source = rawSource
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.IngressView) string { return item.Namespace })
	populateAllowedActionsIngresses(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "Ingress", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed ingresses via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}
func (s *Service) ListEndpointSlices(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.EndpointSliceView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "EndpointSlice", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.EndpointSliceView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListEndpointSlices(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectEndpointSlices(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.EndpointSliceView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapEndpointSlice(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.EndpointSliceView) string { return item.Namespace })
	populateAllowedActionsEndpointSlices(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "EndpointSlice", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed endpoint slices via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}
func (s *Service) ListNetworkPolicies(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) ([]domainresource.NetworkPolicyView, error) {
	connection, decision, err := s.authorize(ctx, principal, clusterID, namespace, "NetworkPolicy", domainaccess.ActionList)
	if err != nil {
		return nil, err
	}
	var (
		items  []domainresource.NetworkPolicyView
		source string
	)
	switch connection.Summary.ConnectionMode {
	case domaincluster.ConnectionModeAgent:
		client, err := s.agentClient(connection)
		if err != nil {
			return nil, err
		}
		items, err = client.ListNetworkPolicies(ctx, namespace)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", apperrors.ErrClusterUnready, err)
		}
		source = "agent"
	default:
		rawItems, err := s.listDirectNetworkPolicies(ctx, clusterID, namespace)
		if err != nil {
			return nil, err
		}
		items = make([]domainresource.NetworkPolicyView, 0, len(rawItems))
		for _, item := range rawItems {
			items = append(items, mapNetworkPolicy(item, decision))
		}
		source = "live"
	}
	items = filterScopedNamespaceItems(items, decision, func(item domainresource.NetworkPolicyView) string { return item.Namespace })
	populateAllowedActionsNetworkPolicies(items, decision)
	_ = s.recordAudit(ctx, principal, connection.Summary.ID, namespace, "NetworkPolicy", "", string(domainaccess.ActionList), "success", fmt.Sprintf("listed network policies via %s in namespace %s", source, displayNamespace(namespace)))
	return items, nil
}
func (s *Service) listDirectServices(ctx context.Context, clusterID, namespace string) ([]corev1.Service, string, error) {
	if shouldUseInformerCache(namespace) {
		if items, err := s.cache.ListServices(clusterID, namespace); err == nil {
			return items, "cache", nil
		} else if !errors.Is(err, informerinfra.ErrCacheNotReady) {
			return nil, "cache", err
		}
	}
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, "live", err
	}
	defer cancel()
	items, err := bundle.Typed.CoreV1().Services(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, "live", err
	}
	return items.Items, "live", nil
}
func (s *Service) listDirectIngresses(ctx context.Context, clusterID, namespace string) ([]networkingv1.Ingress, string, error) {
	if shouldUseInformerCache(namespace) {
		if items, err := s.cache.ListIngresses(clusterID, namespace); err == nil {
			return items, "cache", nil
		} else if !errors.Is(err, informerinfra.ErrCacheNotReady) {
			return nil, "cache", err
		}
	}
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, "live", err
	}
	defer cancel()
	items, err := bundle.Typed.NetworkingV1().Ingresses(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, "live", err
	}
	return items.Items, "live", nil
}
func (s *Service) listDirectEndpointSlices(ctx context.Context, clusterID, namespace string) ([]discoveryv1.EndpointSlice, error) {
	if strings.TrimSpace(namespace) == "" {
		return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]discoveryv1.EndpointSlice, error) {
			items, err := bundle.Typed.DiscoveryV1().EndpointSlices(namespace).List(queryCtx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			return items.Items, nil
		})
	}
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := bundle.Typed.DiscoveryV1().EndpointSlices(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}
func (s *Service) listDirectNetworkPolicies(ctx context.Context, clusterID, namespace string) ([]networkingv1.NetworkPolicy, error) {
	if strings.TrimSpace(namespace) == "" {
		return listAcrossNamespaces(ctx, s, clusterID, func(queryCtx context.Context, bundle *k8sinfra.Bundle, namespace string) ([]networkingv1.NetworkPolicy, error) {
			items, err := bundle.Typed.NetworkingV1().NetworkPolicies(namespace).List(queryCtx, metav1.ListOptions{})
			if err != nil {
				return nil, err
			}
			return items.Items, nil
		})
	}
	bundle, queryCtx, cancel, err := s.directKubeQueryContext(ctx, clusterID, 4*time.Second)
	if err != nil {
		return nil, err
	}
	defer cancel()
	items, err := bundle.Typed.NetworkingV1().NetworkPolicies(namespace).List(queryCtx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return items.Items, nil
}
func mapService(item corev1.Service, decision domainaccess.Decision) domainresource.ServiceView {
	ports := make([]string, 0, len(item.Spec.Ports))
	for _, port := range item.Spec.Ports {
		name := port.Name
		if name != "" {
			name = name + ":"
		}
		ports = append(ports, fmt.Sprintf("%s%d/%s", name, port.Port, strings.ToLower(string(port.Protocol))))
	}
	return domainresource.ServiceView{Name: item.Name, Namespace: item.Namespace, Type: string(item.Spec.Type), ClusterIP: item.Spec.ClusterIP, Ports: ports, Selector: item.Spec.Selector, AgeSeconds: secondsSince(item.CreationTimestamp.Time), AllowedActions: stringifyActions(decision.AllowedActions)}
}
func mapIngress(item networkingv1.Ingress, decision domainaccess.Decision) domainresource.IngressView {
	hosts := make([]string, 0, len(item.Spec.Rules))
	for _, rule := range item.Spec.Rules {
		if strings.TrimSpace(rule.Host) != "" {
			hosts = append(hosts, rule.Host)
		}
	}
	addresses := make([]string, 0, len(item.Status.LoadBalancer.Ingress))
	for _, ingress := range item.Status.LoadBalancer.Ingress {
		if ingress.Hostname != "" {
			addresses = append(addresses, ingress.Hostname)
			continue
		}
		if ingress.IP != "" {
			addresses = append(addresses, ingress.IP)
		}
	}
	className := ""
	if item.Spec.IngressClassName != nil {
		className = *item.Spec.IngressClassName
	}
	return domainresource.IngressView{
		Name:            item.Name,
		Namespace:       item.Namespace,
		ClassName:       className,
		Hosts:           hosts,
		Address:         strings.Join(addresses, ", "),
		BackendServices: extractIngressBackendServices(item),
		AgeSeconds:      secondsSince(item.CreationTimestamp.Time),
		AllowedActions:  stringifyActions(decision.AllowedActions),
	}
}
func mapEndpointSlice(item discoveryv1.EndpointSlice, decision domainaccess.Decision) domainresource.EndpointSliceView {
	ports := make([]string, 0, len(item.Ports))
	for _, port := range item.Ports {
		if port.Port == nil {
			continue
		}
		name := ""
		if port.Name != nil && strings.TrimSpace(*port.Name) != "" {
			name = *port.Name + ":"
		}
		protocol := ""
		if port.Protocol != nil {
			protocol = strings.ToLower(string(*port.Protocol))
		}
		ports = append(ports, fmt.Sprintf("%s%d/%s", name, *port.Port, protocol))
	}
	return domainresource.EndpointSliceView{
		Name:           item.Name,
		Namespace:      item.Namespace,
		AddressType:    string(item.AddressType),
		Endpoints:      len(item.Endpoints),
		Ports:          ports,
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}
func mapNetworkPolicy(item networkingv1.NetworkPolicy, decision domainaccess.Decision) domainresource.NetworkPolicyView {
	policyTypes := make([]string, 0, len(item.Spec.PolicyTypes))
	for _, policyType := range item.Spec.PolicyTypes {
		policyTypes = append(policyTypes, string(policyType))
	}
	return domainresource.NetworkPolicyView{
		Name:           item.Name,
		Namespace:      item.Namespace,
		PolicyTypes:    policyTypes,
		IngressRules:   len(item.Spec.Ingress),
		EgressRules:    len(item.Spec.Egress),
		AgeSeconds:     secondsSince(item.CreationTimestamp.Time),
		AllowedActions: stringifyActions(decision.AllowedActions),
	}
}
func extractIngressBackendServices(item networkingv1.Ingress) []string {
	services := make([]string, 0, len(item.Spec.Rules)+1)
	seen := make(map[string]struct{}, len(item.Spec.Rules)+1)
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, ok := seen[name]; ok {
			return
		}
		seen[name] = struct{}{}
		services = append(services, name)
	}
	if item.Spec.DefaultBackend != nil && item.Spec.DefaultBackend.Service != nil {
		add(item.Spec.DefaultBackend.Service.Name)
	}
	for _, rule := range item.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			if path.Backend.Service != nil {
				add(path.Backend.Service.Name)
			}
		}
	}
	sort.Strings(services)
	return services
}
func populateAllowedActionsServices(items []domainresource.ServiceView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}
func populateAllowedActionsIngresses(items []domainresource.IngressView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}
func populateAllowedActionsEndpointSlices(items []domainresource.EndpointSliceView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}
func populateAllowedActionsNetworkPolicies(items []domainresource.NetworkPolicyView, decision domainaccess.Decision) {
	for i := range items {
		if len(items[i].AllowedActions) == 0 {
			items[i].AllowedActions = stringifyActions(decision.AllowedActions)
		}
	}
}
