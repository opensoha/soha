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
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

func (d *Direct) ListServices(ctx context.Context, clusterID, namespace string) ([]domainresource.ServiceView, string, error) {
	items, source, err := listCachedResources(ctx, clusterID, namespace, d.cache != nil, d.listCachedServices, d.cacheUnavailable, d.listServicesLive)
	if err != nil {
		return nil, source, err
	}
	return mapResourceItems(items, mapService), source, nil
}

func (d *Direct) GetService(ctx context.Context, clusterID, namespace, name string) (domainresource.ServiceView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.ServiceView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.CoreV1().Services(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ServiceView{}, err
	}
	return mapService(*item), nil
}

func (d *Direct) GetServiceDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.ServiceDetailView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.ServiceDetailView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	service, err := bundle.Typed.CoreV1().Services(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.ServiceDetailView{}, err
	}
	endpointSlices, err := bundle.Typed.DiscoveryV1().EndpointSlices(namespace).List(queryCtx, metav1.ListOptions{
		LabelSelector: labels.Set{discoveryv1.LabelServiceName: name}.AsSelector().String(),
	})
	if err != nil {
		endpointSlices = &discoveryv1.EndpointSliceList{}
	}
	backendPods, err := d.ListPodsBySelector(queryCtx, clusterID, namespace, service.Spec.Selector)
	if err != nil {
		backendPods = nil
	}
	return buildServiceDetail(*service, endpointSlices.Items, backendPods), nil
}

func (d *Direct) ListIngresses(ctx context.Context, clusterID, namespace string) ([]domainresource.IngressView, string, error) {
	items, source, err := listCachedResources(ctx, clusterID, namespace, d.cache != nil, d.listCachedIngresses, d.cacheUnavailable, d.listIngressesLive)
	if err != nil {
		return nil, source, err
	}
	return mapResourceItems(items, mapIngress), source, nil
}

func (d *Direct) GetIngressDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.IngressDetailView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.IngressDetailView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	ingress, err := bundle.Typed.NetworkingV1().Ingresses(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.IngressDetailView{}, err
	}
	backends := make([]domainresource.IngressBackendView, 0)
	for _, serviceName := range extractIngressBackendServices(*ingress) {
		service, getErr := bundle.Typed.CoreV1().Services(namespace).Get(queryCtx, serviceName, metav1.GetOptions{})
		if getErr != nil {
			backends = append(backends, domainresource.IngressBackendView{ServiceName: serviceName})
			continue
		}
		slices, listErr := bundle.Typed.DiscoveryV1().EndpointSlices(namespace).List(queryCtx, metav1.ListOptions{
			LabelSelector: labels.Set{discoveryv1.LabelServiceName: serviceName}.AsSelector().String(),
		})
		if listErr != nil {
			slices = &discoveryv1.EndpointSliceList{}
		}
		pods := []corev1.Pod{}
		if len(service.Spec.Selector) > 0 {
			podList, podErr := bundle.Typed.CoreV1().Pods(namespace).List(queryCtx, metav1.ListOptions{
				LabelSelector: labels.Set(service.Spec.Selector).AsSelector().String(),
			})
			if podErr != nil {
				pods = nil
			} else {
				pods = podList.Items
			}
		}
		backends = append(backends, domainresource.IngressBackendView{
			ServiceName: serviceName,
			Endpoints:   mapEndpointSliceEndpoints(slices.Items),
			Pods:        buildNetworkRelatedPods(queryCtx, bundle.Typed, pods),
		})
	}
	return buildIngressDetail(*ingress, backends), nil
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
	items, err := listNamespaced(ctx, d, clusterID, namespace, 4*time.Second, func(ctx context.Context, namespace string) ([]discoveryv1.EndpointSlice, error) {
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

func (d *Direct) GetEndpointSliceDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.EndpointSliceDetailView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.EndpointSliceDetailView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.DiscoveryV1().EndpointSlices(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.EndpointSliceDetailView{}, err
	}
	return buildEndpointSliceDetail(*item), nil
}

func (d *Direct) ListNetworkPolicies(ctx context.Context, clusterID, namespace string) ([]domainresource.NetworkPolicyView, error) {
	items, err := listNamespaced(ctx, d, clusterID, namespace, 4*time.Second, func(ctx context.Context, namespace string) ([]networkingv1.NetworkPolicy, error) {
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

func (d *Direct) GetNetworkPolicyDetail(ctx context.Context, clusterID, namespace, name string) (domainresource.NetworkPolicyDetailView, error) {
	bundle, err := d.directClients(ctx, clusterID)
	if err != nil {
		return domainresource.NetworkPolicyDetailView{}, err
	}
	queryCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	item, err := bundle.Typed.NetworkingV1().NetworkPolicies(namespace).Get(queryCtx, name, metav1.GetOptions{})
	if err != nil {
		return domainresource.NetworkPolicyDetailView{}, err
	}
	selector, err := metav1.LabelSelectorAsSelector(&item.Spec.PodSelector)
	if err != nil {
		return domainresource.NetworkPolicyDetailView{}, err
	}
	pods, err := bundle.Typed.CoreV1().Pods(namespace).List(queryCtx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return domainresource.NetworkPolicyDetailView{}, err
	}
	return buildNetworkPolicyDetail(*item, mapResourceItems(pods.Items, mapPodView)), nil
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

func buildServiceDetail(item corev1.Service, slices []discoveryv1.EndpointSlice, backendPods []domainresource.PodView) domainresource.ServiceDetailView {
	summary := mapService(item)
	endpoints := mapEndpointSliceEndpoints(slices)
	return domainresource.ServiceDetailView{
		Name: summary.Name, Namespace: summary.Namespace, Type: summary.Type, ClusterIP: summary.ClusterIP,
		Ports: summary.Ports, Selector: summary.Selector, Labels: cloneMap(item.Labels), Annotations: cloneMap(item.Annotations),
		Endpoints: endpoints, BackendPods: backendPods, AgeSeconds: summary.AgeSeconds,
	}
}

func mapEndpointSliceEndpoints(slices []discoveryv1.EndpointSlice) []domainresource.ServiceEndpointView {
	endpoints := make([]domainresource.ServiceEndpointView, 0)
	for _, slice := range slices {
		for _, endpoint := range slice.Endpoints {
			targetRef := ""
			if endpoint.TargetRef != nil {
				targetRef = strings.Trim(strings.Join([]string{endpoint.TargetRef.Kind, endpoint.TargetRef.Name}, "/"), "/")
			}
			for _, address := range endpoint.Addresses {
				view := domainresource.ServiceEndpointView{
					Address: address, Ready: endpoint.Conditions.Ready, Serving: endpoint.Conditions.Serving,
					Terminating: endpoint.Conditions.Terminating, TargetRef: targetRef,
				}
				if endpoint.NodeName != nil {
					view.NodeName = *endpoint.NodeName
				}
				if endpoint.Zone != nil {
					view.Zone = *endpoint.Zone
				}
				endpoints = append(endpoints, view)
			}
		}
	}
	return endpoints
}

func buildIngressDetail(item networkingv1.Ingress, backends []domainresource.IngressBackendView) domainresource.IngressDetailView {
	summary := mapIngress(item)
	tlsHosts := make([]string, 0)
	for _, tls := range item.Spec.TLS {
		tlsHosts = append(tlsHosts, tls.Hosts...)
	}
	routes := make([]domainresource.IngressRouteView, 0)
	addRoute := func(host, path, pathType string, backend networkingv1.IngressBackend) {
		if backend.Service == nil {
			return
		}
		routes = append(routes, domainresource.IngressRouteView{
			Host: host, Path: path, PathType: pathType, TLS: ingressHostUsesTLS(host, tlsHosts),
			ServiceName: backend.Service.Name, ServicePort: ingressServicePort(backend.Service.Port),
		})
	}
	if item.Spec.DefaultBackend != nil {
		addRoute("", "/", "Default", *item.Spec.DefaultBackend)
	}
	for _, rule := range item.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			pathType := ""
			if path.PathType != nil {
				pathType = string(*path.PathType)
			}
			addRoute(rule.Host, path.Path, pathType, path.Backend)
		}
	}
	return domainresource.IngressDetailView{
		Name: summary.Name, Namespace: summary.Namespace, ClassName: summary.ClassName, Address: summary.Address,
		Labels: cloneMap(item.Labels), Annotations: cloneMap(item.Annotations), Routes: routes, Backends: backends,
		AgeSeconds: summary.AgeSeconds,
	}
}

func ingressHostUsesTLS(host string, tlsHosts []string) bool {
	for _, tlsHost := range tlsHosts {
		if tlsHost == host {
			return true
		}
		if strings.HasPrefix(tlsHost, "*.") {
			suffix := strings.TrimPrefix(tlsHost, "*.")
			prefix := strings.TrimSuffix(host, "."+suffix)
			if prefix != host && prefix != "" && !strings.Contains(prefix, ".") {
				return true
			}
		}
	}
	return false
}

func ingressServicePort(port networkingv1.ServiceBackendPort) string {
	if port.Name != "" {
		return port.Name
	}
	if port.Number != 0 {
		return fmt.Sprint(port.Number)
	}
	return ""
}

func buildNetworkRelatedPods(ctx context.Context, client kubernetes.Interface, pods []corev1.Pod) []domainresource.NetworkRelatedPodView {
	replicaSetDeployments := make(map[string]string)
	jobCronJobs := make(map[string]string)
	for _, pod := range pods {
		for _, owner := range pod.OwnerReferences {
			switch owner.Kind {
			case "ReplicaSet":
				if _, ok := replicaSetDeployments[owner.Name]; !ok {
					replicaSetDeployments[owner.Name] = ""
					if item, err := client.AppsV1().ReplicaSets(pod.Namespace).Get(ctx, owner.Name, metav1.GetOptions{}); err == nil {
						replicaSetDeployments[owner.Name] = podOwnerName(item.OwnerReferences, "Deployment")
					}
				}
			case "Job":
				if _, ok := jobCronJobs[owner.Name]; !ok {
					jobCronJobs[owner.Name] = ""
					if item, err := client.BatchV1().Jobs(pod.Namespace).Get(ctx, owner.Name, metav1.GetOptions{}); err == nil {
						jobCronJobs[owner.Name] = podOwnerName(item.OwnerReferences, "CronJob")
					}
				}
			}
		}
	}
	views := make([]domainresource.NetworkRelatedPodView, 0, len(pods))
	for _, pod := range pods {
		workloads := make([]domainresource.PodRelatedResourceView, 0)
		for _, owner := range pod.OwnerReferences {
			switch owner.Kind {
			case "ReplicaSet":
				workloads = append(workloads, domainresource.PodRelatedResourceView{Kind: owner.Kind, Name: owner.Name, Namespace: pod.Namespace})
				if deployment := replicaSetDeployments[owner.Name]; deployment != "" {
					workloads = append(workloads, domainresource.PodRelatedResourceView{Kind: "Deployment", Name: deployment, Namespace: pod.Namespace})
				}
			case "StatefulSet", "DaemonSet", "Job", "CronJob":
				workloads = append(workloads, domainresource.PodRelatedResourceView{Kind: owner.Kind, Name: owner.Name, Namespace: pod.Namespace})
				if owner.Kind == "Job" && jobCronJobs[owner.Name] != "" {
					workloads = append(workloads, domainresource.PodRelatedResourceView{Kind: "CronJob", Name: jobCronJobs[owner.Name], Namespace: pod.Namespace})
				}
			}
		}
		views = append(views, domainresource.NetworkRelatedPodView{PodView: mapPodView(pod), Workloads: workloads})
	}
	return views
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

func buildEndpointSliceDetail(item discoveryv1.EndpointSlice) domainresource.EndpointSliceDetailView {
	summary := mapEndpointSlice(item)
	return domainresource.EndpointSliceDetailView{
		Name: summary.Name, Namespace: summary.Namespace, AddressType: summary.AddressType,
		ServiceName: item.Labels[discoveryv1.LabelServiceName], Ports: summary.Ports,
		Labels: cloneMap(item.Labels), Annotations: cloneMap(item.Annotations),
		Endpoints: mapEndpointSliceEndpoints([]discoveryv1.EndpointSlice{item}), AgeSeconds: summary.AgeSeconds,
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

func buildNetworkPolicyDetail(item networkingv1.NetworkPolicy, pods []domainresource.PodView) domainresource.NetworkPolicyDetailView {
	summary := mapNetworkPolicy(item)
	rules := make([]domainresource.NetworkPolicyRuleView, 0, len(item.Spec.Ingress)+len(item.Spec.Egress))
	for _, rule := range item.Spec.Ingress {
		rules = append(rules, domainresource.NetworkPolicyRuleView{Direction: "Ingress", Peers: mapPolicyPeers(rule.From), Ports: mapPolicyPorts(rule.Ports)})
	}
	for _, rule := range item.Spec.Egress {
		rules = append(rules, domainresource.NetworkPolicyRuleView{Direction: "Egress", Peers: mapPolicyPeers(rule.To), Ports: mapPolicyPorts(rule.Ports)})
	}
	selector, _ := metav1.LabelSelectorAsSelector(&item.Spec.PodSelector)
	return domainresource.NetworkPolicyDetailView{
		Name: summary.Name, Namespace: summary.Namespace, PolicyTypes: summary.PolicyTypes,
		PodSelector: selector.String(), Labels: cloneMap(item.Labels), Annotations: cloneMap(item.Annotations),
		Rules: rules, MatchingPods: pods, AgeSeconds: summary.AgeSeconds,
	}
}

func mapPolicyPeers(peers []networkingv1.NetworkPolicyPeer) []domainresource.NetworkPolicyPeerView {
	views := make([]domainresource.NetworkPolicyPeerView, 0, len(peers))
	for _, peer := range peers {
		view := domainresource.NetworkPolicyPeerView{}
		if peer.PodSelector != nil {
			selector, _ := metav1.LabelSelectorAsSelector(peer.PodSelector)
			view.PodSelector = selector.String()
		}
		if peer.NamespaceSelector != nil {
			selector, _ := metav1.LabelSelectorAsSelector(peer.NamespaceSelector)
			view.NamespaceSelector = selector.String()
		}
		if peer.IPBlock != nil {
			view.IPBlock = peer.IPBlock.CIDR
			if len(peer.IPBlock.Except) > 0 {
				view.IPBlock += " except " + strings.Join(peer.IPBlock.Except, ", ")
			}
		}
		views = append(views, view)
	}
	return views
}

func mapPolicyPorts(ports []networkingv1.NetworkPolicyPort) []domainresource.NetworkPolicyPortView {
	views := make([]domainresource.NetworkPolicyPortView, 0, len(ports))
	for _, port := range ports {
		view := domainresource.NetworkPolicyPortView{}
		if port.Protocol != nil {
			view.Protocol = string(*port.Protocol)
		}
		if port.Port != nil {
			view.Port = port.Port.String()
		}
		if port.EndPort != nil {
			view.EndPort = *port.EndPort
		}
		views = append(views, view)
	}
	return views
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
