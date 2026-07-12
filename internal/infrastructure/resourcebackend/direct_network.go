package resourcebackend

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	appresource "github.com/opensoha/soha/internal/application/resource"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	corev1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (d *Direct) ListServices(ctx context.Context, clusterID, namespace string) ([]domainresource.ServiceView, string, error) {
	items, source, err := listCachedResources(ctx, clusterID, namespace, shouldUseInformerCache(namespace) && d.cache != nil, d.listCachedServices, d.cacheUnavailable, d.listServicesLive)
	if err != nil {
		return nil, source, err
	}
	return mapResourceItems(items, mapService), source, nil
}

func (d *Direct) ListIngresses(ctx context.Context, clusterID, namespace string) ([]domainresource.IngressView, string, error) {
	items, source, err := listCachedResources(ctx, clusterID, namespace, shouldUseInformerCache(namespace) && d.cache != nil, d.listCachedIngresses, d.cacheUnavailable, d.listIngressesLive)
	if err != nil {
		return nil, source, err
	}
	return mapResourceItems(items, mapIngress), source, nil
}

func listCachedResources[T any](ctx context.Context, clusterID, namespace string, cacheEnabled bool, cacheList func(string, string) ([]T, error), cacheUnavailable func(error) bool, liveList func(context.Context, string, string) ([]T, error)) ([]T, string, error) {
	if cacheEnabled {
		items, err := cacheList(clusterID, namespace)
		if err == nil {
			return items, "cache", nil
		}
		if !cacheUnavailable(err) {
			return nil, "cache", err
		}
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	items, err := liveList(queryCtx, clusterID, namespace)
	return items, "live", err
}

func (d *Direct) listServicesLive(ctx context.Context, clusterID, namespace string) ([]corev1.Service, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	result, err := bundle.Typed.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (d *Direct) listCachedServices(clusterID, namespace string) ([]corev1.Service, error) {
	return d.cache.ListServices(clusterID, namespace)
}

func (d *Direct) listIngressesLive(ctx context.Context, clusterID, namespace string) ([]networkingv1.Ingress, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	result, err := bundle.Typed.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

func (d *Direct) listCachedIngresses(clusterID, namespace string) ([]networkingv1.Ingress, error) {
	return d.cache.ListIngresses(clusterID, namespace)
}

func (d *Direct) cacheUnavailable(err error) bool {
	return d.cache.CacheUnavailable(err)
}

func mapResourceItems[T any, V any](items []T, mapItem func(T) V) []V {
	views := make([]V, 0, len(items))
	for _, item := range items {
		views = append(views, mapItem(item))
	}
	return views
}

func (d *Direct) ListEndpointSlices(ctx context.Context, clusterID, namespace string) ([]domainresource.EndpointSliceView, error) {
	items, err := directNamespacedList(ctx, d, clusterID, namespace, func(ctx context.Context, namespace string) ([]discoveryv1.EndpointSlice, error) {
		bundle, err := d.directClients(ctx, clusterID)
		if err != nil {
			return nil, err
		}
		result, err := bundle.Typed.DiscoveryV1().EndpointSlices(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return result.Items, nil
	})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.EndpointSliceView, 0, len(items))
	for _, item := range items {
		views = append(views, mapEndpointSlice(item))
	}
	return views, nil
}

func (d *Direct) ListNetworkPolicies(ctx context.Context, clusterID, namespace string) ([]domainresource.NetworkPolicyView, error) {
	items, err := directNamespacedList(ctx, d, clusterID, namespace, func(ctx context.Context, namespace string) ([]networkingv1.NetworkPolicy, error) {
		bundle, err := d.directClients(ctx, clusterID)
		if err != nil {
			return nil, err
		}
		result, err := bundle.Typed.NetworkingV1().NetworkPolicies(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		return result.Items, nil
	})
	if err != nil {
		return nil, err
	}
	views := make([]domainresource.NetworkPolicyView, 0, len(items))
	for _, item := range items {
		views = append(views, mapNetworkPolicy(item))
	}
	return views, nil
}

func directNamespacedList[T any](ctx context.Context, d *Direct, clusterID, namespace string, list func(context.Context, string) ([]T, error)) ([]T, error) {
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	items, err := list(queryCtx, namespace)
	cancel()
	if strings.TrimSpace(namespace) != "" || err == nil {
		return items, err
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	bundle, bundleErr := d.directClients(ctx, clusterID)
	if bundleErr != nil {
		return nil, err
	}
	namespaceCtx, namespaceCancel := context.WithTimeout(ctx, 4*time.Second)
	namespaces, namespaceErr := bundle.Typed.CoreV1().Namespaces().List(namespaceCtx, metav1.ListOptions{})
	namespaceCancel()
	if namespaceErr != nil {
		return nil, err
	}
	all := make([]T, 0)
	for _, item := range namespaces.Items {
		queryCtx, queryCancel := context.WithTimeout(ctx, 4*time.Second)
		result, listErr := list(queryCtx, item.Name)
		queryCancel()
		if listErr != nil {
			return nil, listErr
		}
		all = append(all, result...)
	}
	return all, nil
}

func mapService(item corev1.Service) domainresource.ServiceView {
	ports := make([]string, 0, len(item.Spec.Ports))
	for _, port := range item.Spec.Ports {
		name := port.Name
		if name != "" {
			name += ":"
		}
		ports = append(ports, fmt.Sprintf("%s%d/%s", name, port.Port, strings.ToLower(string(port.Protocol))))
	}
	return domainresource.ServiceView{
		Name: item.Name, Namespace: item.Namespace, Type: string(item.Spec.Type),
		ClusterIP: item.Spec.ClusterIP, Ports: ports, Selector: item.Spec.Selector,
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapIngress(item networkingv1.Ingress) domainresource.IngressView {
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
		} else if ingress.IP != "" {
			addresses = append(addresses, ingress.IP)
		}
	}
	className := ""
	if item.Spec.IngressClassName != nil {
		className = *item.Spec.IngressClassName
	}
	return domainresource.IngressView{
		Name: item.Name, Namespace: item.Namespace, ClassName: className, Hosts: hosts,
		Address: strings.Join(addresses, ", "), BackendServices: extractIngressBackendServices(item),
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapEndpointSlice(item discoveryv1.EndpointSlice) domainresource.EndpointSliceView {
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
		Name: item.Name, Namespace: item.Namespace, AddressType: string(item.AddressType),
		Endpoints: len(item.Endpoints), Ports: ports, AgeSeconds: secondsSince(item.CreationTimestamp.Time),
	}
}

func mapNetworkPolicy(item networkingv1.NetworkPolicy) domainresource.NetworkPolicyView {
	policyTypes := make([]string, 0, len(item.Spec.PolicyTypes))
	for _, policyType := range item.Spec.PolicyTypes {
		policyTypes = append(policyTypes, string(policyType))
	}
	return domainresource.NetworkPolicyView{
		Name: item.Name, Namespace: item.Namespace, PolicyTypes: policyTypes,
		IngressRules: len(item.Spec.Ingress), EgressRules: len(item.Spec.Egress),
		AgeSeconds: secondsSince(item.CreationTimestamp.Time),
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

var _ appresource.DirectNetworkReader = (*Direct)(nil)
