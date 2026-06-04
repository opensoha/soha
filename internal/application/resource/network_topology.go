package resource

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	domainidentity "github.com/soha/soha/internal/domain/identity"
	domainresource "github.com/soha/soha/internal/domain/resource"
)

func (s *Service) GetNetworkTopology(ctx context.Context, principal domainidentity.Principal, clusterID, namespace string) (domainresource.NetworkTopologyView, error) {
	view := domainresource.NetworkTopologyView{
		ClusterID:   clusterID,
		Namespace:   namespace,
		Source:      "backend-aggregate",
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Services:    []domainresource.ServiceView{},
		Ingresses:   []domainresource.IngressView{},
		HTTPRoutes:  []domainresource.HTTPRouteView{},
		Gateways:    []domainresource.GatewayView{},
		Pods:        []domainresource.PodView{},
	}

	var (
		firstErr error
		mu       sync.Mutex
		wg       sync.WaitGroup
	)
	recordWarning := func(kind string, err error) {
		if err == nil {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if firstErr == nil {
			firstErr = err
		}
		view.Warnings = append(view.Warnings, fmt.Sprintf("%s: %v", kind, err))
	}
	run := func(fn func() error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = fn()
		}()
	}

	run(func() error {
		items, err := s.ListServices(ctx, principal, clusterID, namespace)
		mu.Lock()
		view.Services = items
		mu.Unlock()
		recordWarning("services", err)
		return err
	})
	run(func() error {
		items, err := s.ListIngresses(ctx, principal, clusterID, namespace)
		mu.Lock()
		view.Ingresses = items
		mu.Unlock()
		recordWarning("ingresses", err)
		return err
	})
	run(func() error {
		items, err := s.ListHTTPRoutes(ctx, principal, clusterID, namespace)
		mu.Lock()
		view.HTTPRoutes = items
		mu.Unlock()
		recordWarning("httpRoutes", err)
		return err
	})
	run(func() error {
		items, err := s.ListGateways(ctx, principal, clusterID, namespace)
		mu.Lock()
		view.Gateways = items
		mu.Unlock()
		recordWarning("gateways", err)
		return err
	})
	run(func() error {
		items, err := s.ListPods(ctx, principal, clusterID, namespace)
		mu.Lock()
		view.Pods = items
		mu.Unlock()
		recordWarning("pods", err)
		return err
	})

	wg.Wait()
	if firstErr != nil && len(view.Services) == 0 && len(view.Ingresses) == 0 && len(view.HTTPRoutes) == 0 && len(view.Gateways) == 0 && len(view.Pods) == 0 {
		return domainresource.NetworkTopologyView{}, firstErr
	}
	view.Traces = buildNetworkTopologyTraces(view.Services, view.Ingresses, view.HTTPRoutes, view.Gateways, view.Pods)
	view.Summary = summarizeNetworkTopologyTraces(view.Traces)
	return view, nil
}

type topologyGatewayRef struct {
	ID             string
	Name           string
	Namespace      string
	AddressSummary string
	GatewayClass   string
	Visible        bool
}

func buildNetworkTopologyTraces(services []domainresource.ServiceView, ingresses []domainresource.IngressView, httpRoutes []domainresource.HTTPRouteView, gateways []domainresource.GatewayView, pods []domainresource.PodView) []domainresource.NetworkTopologyTraceView {
	serviceLookup := buildTopologyServiceLookup(services)
	podsByNamespace := buildTopologyPodsByNamespace(pods)
	gatewayRefs := buildTopologyGatewayRefs(gateways)
	referencedGateways := make(map[string]struct{})
	traces := make([]domainresource.NetworkTopologyTraceView, 0, len(ingresses)+len(httpRoutes)+len(gateways))

	for _, ingress := range ingresses {
		route := topologyNode(
			fmt.Sprintf("ingress-route:%s/%s", ingress.Namespace, ingress.Name),
			ingress.Name,
			"ingress-route",
			"live",
			ingress.Namespace,
			ingress.Name,
			ingress.Namespace,
			firstNonEmpty(ingress.ClassName, "Ingress"),
		)
		entries := firstNonEmptyStrings(ingress.Hosts, splitTopologyCSV(ingress.Address), []string{ingress.Name})
		backends := uniqueNonEmptySorted(ingress.BackendServices)
		for _, entryLabel := range entries {
			entry := topologyNode(
				fmt.Sprintf("entry:ingress:%s/%s/%s", ingress.Namespace, ingress.Name, entryLabel),
				entryLabel,
				"entry",
				"live",
				ingress.Namespace,
				ingress.Name,
				ingress.Namespace,
				firstNonEmpty(ingress.ClassName, "Ingress"),
			)
			if len(backends) == 0 {
				service := topologyNode(
					fmt.Sprintf("missing-service:%s/%s:no-backend", ingress.Namespace, ingress.Name),
					"No backend service",
					"missing-service",
					"pending",
					ingress.Namespace,
					"",
					ingress.Namespace,
					"Backend pending",
				)
				traces = append(traces, domainresource.NetworkTopologyTraceView{
					ID:         fmt.Sprintf("%s:%s:no-backend", entry.ID, route.ID),
					SourceType: "ingress",
					State:      "pending",
					Entry:      entry,
					Route:      route,
					Service:    &service,
					Note:       "Ingress has no resolved backend service.",
				})
				continue
			}
			for _, serviceName := range backends {
				service, backendPods, state, note := resolveTopologyService(ingress.Namespace, serviceName, serviceLookup, podsByNamespace)
				traces = append(traces, domainresource.NetworkTopologyTraceView{
					ID:          fmt.Sprintf("%s:%s:%s", entry.ID, route.ID, serviceName),
					SourceType:  "ingress",
					State:       state,
					Entry:       entry,
					Route:       route,
					Service:     service,
					BackendPods: backendPods,
					Note:        note,
				})
			}
		}
	}

	for _, routeItem := range httpRoutes {
		parents := uniqueNonEmptySorted(routeItem.ParentRefs)
		for _, parentRef := range parents {
			referencedGateways[parentRef] = struct{}{}
		}
		if len(parents) == 0 {
			parents = []string{fmt.Sprintf("unbound:%s/%s", routeItem.Namespace, routeItem.Name)}
		}
		backends := uniqueNonEmptySorted(routeItem.BackendServices)
		routeNode := topologyNode(
			fmt.Sprintf("http-route:%s/%s", routeItem.Namespace, routeItem.Name),
			routeItem.Name,
			"http-route",
			"live",
			routeItem.Namespace,
			routeItem.Name,
			strings.Join(firstNonEmptyStrings(routeItem.Hostnames, nil, []string{routeItem.Namespace}), ", "),
			"HTTPRoute",
		)
		for _, parentRef := range parents {
			parent := resolveTopologyGatewayRef(parentRef, routeItem.Namespace, gatewayRefs)
			entries := firstNonEmptyStrings(routeItem.Hostnames, splitTopologyCSV(parent.AddressSummary), []string{parent.Name})
			entryState := "live"
			if !parent.Visible {
				entryState = "pending"
			}
			for _, entryLabel := range entries {
				entry := topologyNode(
					fmt.Sprintf("entry:gateway:%s/%s", parent.ID, entryLabel),
					entryLabel,
					"entry",
					entryState,
					parent.Namespace,
					parent.Name,
					parent.Namespace,
					firstNonEmpty(parent.GatewayClass, "Gateway API"),
				)
				if len(backends) == 0 {
					service := topologyNode(
						fmt.Sprintf("missing-service:%s/%s:no-backend", routeItem.Namespace, routeItem.Name),
						"No backend service",
						"missing-service",
						"pending",
						routeItem.Namespace,
						"",
						routeItem.Namespace,
						"Backend pending",
					)
					traces = append(traces, domainresource.NetworkTopologyTraceView{
						ID:         fmt.Sprintf("%s:%s:no-backend", entry.ID, routeNode.ID),
						SourceType: "httproute",
						State:      "pending",
						Entry:      entry,
						Route:      routeNode,
						Service:    &service,
						Note:       joinTopologyNotes("HTTPRoute has no resolved backend service.", invisibleGatewayNote(parent)),
					})
					continue
				}
				for _, serviceName := range backends {
					service, backendPods, state, note := resolveTopologyService(routeItem.Namespace, serviceName, serviceLookup, podsByNamespace)
					if !parent.Visible {
						state = "pending"
						note = joinTopologyNotes(note, invisibleGatewayNote(parent))
					}
					traces = append(traces, domainresource.NetworkTopologyTraceView{
						ID:          fmt.Sprintf("%s:%s:%s", entry.ID, routeNode.ID, serviceName),
						SourceType:  "httproute",
						State:       state,
						Entry:       entry,
						Route:       routeNode,
						Service:     service,
						BackendPods: backendPods,
						Note:        note,
					})
				}
			}
		}
	}

	for _, gateway := range gatewayRefs {
		if _, ok := referencedGateways[gateway.ID]; ok {
			continue
		}
		entryName := firstNonEmpty(gateway.AddressSummary, gateway.Name)
		entry := topologyNode(
			fmt.Sprintf("entry:pending-gateway:%s", gateway.ID),
			entryName,
			"entry",
			"pending",
			gateway.Namespace,
			gateway.Name,
			gateway.Namespace,
			firstNonEmpty(gateway.GatewayClass, "Gateway API"),
		)
		route := topologyNode(
			fmt.Sprintf("pending-route:%s", gateway.ID),
			"HTTPRoute pending",
			"pending-route",
			"pending",
			gateway.Namespace,
			gateway.Name,
			gateway.Name,
			firstNonEmpty(gateway.GatewayClass, "Gateway API"),
		)
		traces = append(traces, domainresource.NetworkTopologyTraceView{
			ID:         fmt.Sprintf("pending-gateway:%s", gateway.ID),
			SourceType: "gateway",
			State:      "pending",
			Entry:      entry,
			Route:      route,
			Note:       "Gateway has no visible HTTPRoute binding.",
		})
	}

	sort.SliceStable(traces, func(i, j int) bool { return traces[i].ID < traces[j].ID })
	return traces
}

func summarizeNetworkTopologyTraces(traces []domainresource.NetworkTopologyTraceView) domainresource.NetworkTopologySummaryView {
	entryIDs := make(map[string]struct{})
	routeIDs := make(map[string]struct{})
	serviceIDs := make(map[string]struct{})
	missingServiceIDs := make(map[string]struct{})
	backendPodIDs := make(map[string]struct{})
	pendingRouteIDs := make(map[string]struct{})
	for _, trace := range traces {
		if trace.Entry.ID != "" {
			entryIDs[trace.Entry.ID] = struct{}{}
		}
		if trace.Route.ID != "" {
			routeIDs[trace.Route.ID] = struct{}{}
			if trace.Route.Kind == "pending-route" {
				pendingRouteIDs[trace.Route.ID] = struct{}{}
			}
		}
		if trace.Service != nil {
			switch trace.Service.Kind {
			case "service":
				serviceIDs[trace.Service.ID] = struct{}{}
			case "missing-service":
				missingServiceIDs[trace.Service.ID] = struct{}{}
			}
		}
		for _, pod := range trace.BackendPods {
			if pod.ID != "" {
				backendPodIDs[pod.ID] = struct{}{}
			}
		}
	}
	return domainresource.NetworkTopologySummaryView{
		EntryCount:          len(entryIDs),
		RouteCount:          len(routeIDs),
		ServiceCount:        len(serviceIDs),
		MissingServiceCount: len(missingServiceIDs),
		BackendPodCount:     len(backendPodIDs),
		PendingRouteCount:   len(pendingRouteIDs),
	}
}

func buildTopologyServiceLookup(services []domainresource.ServiceView) map[string]domainresource.ServiceView {
	out := make(map[string]domainresource.ServiceView, len(services))
	for _, service := range services {
		out[topologyNamespacedKey(service.Namespace, service.Name)] = service
	}
	return out
}

func buildTopologyPodsByNamespace(pods []domainresource.PodView) map[string][]domainresource.PodView {
	out := make(map[string][]domainresource.PodView)
	for _, pod := range pods {
		out[pod.Namespace] = append(out[pod.Namespace], pod)
	}
	return out
}

func buildTopologyGatewayRefs(gateways []domainresource.GatewayView) map[string]topologyGatewayRef {
	out := make(map[string]topologyGatewayRef, len(gateways))
	for _, gateway := range gateways {
		id := topologyNamespacedKey(gateway.Namespace, gateway.Name)
		out[id] = topologyGatewayRef{
			ID:             id,
			Name:           gateway.Name,
			Namespace:      gateway.Namespace,
			AddressSummary: strings.Join(gateway.Addresses, ", "),
			GatewayClass:   gateway.GatewayClass,
			Visible:        true,
		}
	}
	return out
}

func resolveTopologyGatewayRef(ref, defaultNamespace string, gateways map[string]topologyGatewayRef) topologyGatewayRef {
	if gateway, ok := gateways[ref]; ok {
		return gateway
	}
	namespace, name := splitTopologyNamespacedRef(ref, defaultNamespace)
	if strings.HasPrefix(ref, "unbound:") {
		return topologyGatewayRef{
			ID:        ref,
			Name:      "Unbound gateway",
			Namespace: namespace,
			Visible:   false,
		}
	}
	return topologyGatewayRef{
		ID:        ref,
		Name:      name,
		Namespace: namespace,
		Visible:   false,
	}
}

func resolveTopologyService(namespace, serviceName string, services map[string]domainresource.ServiceView, podsByNamespace map[string][]domainresource.PodView) (*domainresource.NetworkTopologyNodeView, []domainresource.NetworkTopologyNodeView, string, string) {
	key := topologyNamespacedKey(namespace, serviceName)
	service, ok := services[key]
	if !ok {
		node := topologyNode(
			fmt.Sprintf("missing-service:%s", key),
			serviceName,
			"missing-service",
			"pending",
			namespace,
			serviceName,
			namespace,
			"Out of current scope",
		)
		return &node, nil, "pending", "Referenced Service is not visible in the current scope."
	}
	node := topologyNode(
		fmt.Sprintf("service:%s", key),
		service.Name,
		"service",
		"live",
		service.Namespace,
		service.Name,
		service.Namespace,
		"Service",
	)
	pods := make([]domainresource.NetworkTopologyNodeView, 0)
	for _, pod := range podsByNamespace[service.Namespace] {
		if selectorMatchesPodLabels(service.Selector, pod.Labels) {
			pods = append(pods, topologyNode(
				fmt.Sprintf("pod:%s/%s", pod.Namespace, pod.Name),
				pod.Name,
				"pod",
				"live",
				pod.Namespace,
				pod.Name,
				pod.Namespace,
				"Pod",
			))
		}
	}
	sort.SliceStable(pods, func(i, j int) bool { return pods[i].ID < pods[j].ID })
	if len(pods) == 0 {
		return &node, pods, "live", "Service resolved, but selector matched no backend pods."
	}
	return &node, pods, "live", fmt.Sprintf("%d backend pods resolved.", len(pods))
}

func topologyNode(id, name, kind, state, namespace, resourceName, subtitle, badge string) domainresource.NetworkTopologyNodeView {
	return domainresource.NetworkTopologyNodeView{
		ID:           id,
		Name:         name,
		Kind:         kind,
		State:        state,
		Namespace:    namespace,
		ResourceName: resourceName,
		Subtitle:     subtitle,
		Badge:        badge,
	}
}

func topologyNamespacedKey(namespace, name string) string {
	return fmt.Sprintf("%s/%s", namespace, name)
}

func splitTopologyNamespacedRef(ref, defaultNamespace string) (string, string) {
	ref = strings.TrimPrefix(strings.TrimSpace(ref), "unbound:")
	namespace, name, ok := strings.Cut(ref, "/")
	if !ok {
		return defaultNamespace, ref
	}
	if strings.TrimSpace(namespace) == "" {
		namespace = defaultNamespace
	}
	return namespace, name
}

func firstNonEmptyStrings(values ...[]string) []string {
	for _, items := range values {
		out := uniqueNonEmptySorted(items)
		if len(out) > 0 {
			return out
		}
	}
	return []string{}
}

func uniqueNonEmptySorted(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func splitTopologyCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return strings.Split(value, ",")
}

func invisibleGatewayNote(parent topologyGatewayRef) string {
	if parent.Visible {
		return ""
	}
	if strings.HasPrefix(parent.ID, "unbound:") {
		return "HTTPRoute is not bound to a visible Gateway."
	}
	return "HTTPRoute parent Gateway is not visible in the current scope."
}

func joinTopologyNotes(notes ...string) string {
	out := make([]string, 0, len(notes))
	for _, note := range notes {
		note = strings.TrimSpace(note)
		if note != "" {
			out = append(out, note)
		}
	}
	return strings.Join(out, " ")
}
