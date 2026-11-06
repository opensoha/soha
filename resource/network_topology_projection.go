package resource

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// ProjectNetworkTopologyObject is the wire allowlist shared by both Kubernetes readers.
// Wildcards copy scalar label values only; no arbitrary manifest subtree is transported.
func ProjectNetworkTopologyObject(source string, object map[string]any) (map[string]any, error) {
	paths, ok := topologyProjectionPaths[source]
	if !ok {
		return nil, fmt.Errorf("unknown topology source")
	}
	paths = append(strings.Fields("metadata.name metadata.namespace metadata.uid metadata.resourceVersion metadata.generation"), paths...)
	tree := map[string]any{}
	for _, path := range paths {
		addTopologyProjectionPath(tree, strings.Split(path, "."))
	}
	projected, _ := projectTopologyValue(object, tree).(map[string]any)
	redactTopologyMatches(projected, "")
	if source == "kubernetes.endpointslices" {
		if metadata, ok := projected["metadata"].(map[string]any); ok {
			if labels, ok := metadata["labels"].(map[string]any); ok {
				metadata["labels"] = map[string]any{"kubernetes.io/service-name": labels["kubernetes.io/service-name"]}
			}
		}
	}
	if source == "traefik.middlewares" || source == "traefik.middlewaretcps" {
		projected["processorTypes"] = topologyMiddlewareTypes(object)
	}
	raw, err := json.Marshal(projected)
	if err != nil || len(raw) > NetworkTopologyMaxObjectBytes {
		return nil, fmt.Errorf("topology object exceeds projection budget")
	}
	return projected, nil
}

func addTopologyProjectionPath(tree map[string]any, path []string) {
	if len(path) == 0 {
		return
	}
	if len(path) == 1 {
		tree[path[0]] = true
		return
	}
	next, _ := tree[path[0]].(map[string]any)
	if next == nil {
		next = map[string]any{}
		tree[path[0]] = next
	}
	addTopologyProjectionPath(next, path[1:])
}

func projectTopologyValue(value any, tree map[string]any) any {
	switch input := value.(type) {
	case map[string]any:
		output := map[string]any{}
		for key, value := range input {
			rule, ok := tree[key]
			if !ok {
				rule, ok = tree["*"]
			}
			if !ok {
				continue
			}
			if children, ok := rule.(map[string]any); ok {
				output[key] = projectTopologyValue(value, children)
			} else if safe, ok := topologyLeaf(value); ok {
				output[key] = safe
			}
		}
		return output
	case []any:
		output := make([]any, 0, len(input))
		for _, value := range input {
			if scalarTopologyValue(value) {
				output = append(output, value)
			} else {
				output = append(output, projectTopologyValue(value, tree))
			}
		}
		return output
	default:
		if scalarTopologyValue(value) {
			return value
		}
		return nil
	}
}

func scalarTopologyValue(value any) bool {
	switch value.(type) {
	case nil, string, bool, float64, json.Number, int, int32, int64:
		return true
	}
	return false
}

func redactTopologyMatches(value any, key string) {
	switch item := value.(type) {
	case map[string]any:
		if key == "headers" || key == "queryParams" || key == "exprs" {
			if _, ok := item["value"]; ok {
				item["value"] = "[redacted]"
			}
			if _, ok := item["set"]; ok {
				item["set"] = []any{"[redacted]"}
			}
		}
		if match, ok := item["match"].(string); ok && (strings.Contains(strings.ToLower(match), "header") || strings.Contains(strings.ToLower(match), "query")) {
			item["match"] = "[redacted match expression]"
		}
		for child, value := range item {
			redactTopologyMatches(value, child)
		}
	case []any:
		for _, value := range item {
			redactTopologyMatches(value, key)
		}
	}
}

var topologyProjectionPaths = map[string][]string{
	"kubernetes.namespaces":      strings.Fields("metadata.labels.*"),
	"kubernetes.configmaps":      {},
	"kubernetes.networkpolicies": networkPolicyTopologyPaths(),
	"gateway.backendtlspolicies": strings.Fields("spec.targetRefs.group spec.targetRefs.kind spec.targetRefs.name spec.targetRefs.sectionName spec.targetRef.group spec.targetRef.kind spec.targetRef.name spec.targetRef.sectionName spec.validation.caCertificateRefs.group spec.validation.caCertificateRefs.kind spec.validation.caCertificateRefs.name"),
	"kubernetes.services":        strings.Fields("spec.type spec.externalName spec.clusterIP spec.selector.* spec.ports.name spec.ports.port spec.ports.targetPort spec.ports.protocol spec.publishNotReadyAddresses"),
	"kubernetes.pods":            strings.Fields("metadata.labels.* status.phase status.conditions.type status.conditions.status"),
	"kubernetes.endpointslices":  strings.Fields("metadata.labels.* addressType ports.name ports.port ports.protocol endpoints.addresses endpoints.conditions.ready endpoints.conditions.serving endpoints.conditions.terminating endpoints.targetRef.apiVersion endpoints.targetRef.kind endpoints.targetRef.namespace endpoints.targetRef.name endpoints.targetRef.uid"),
	"kubernetes.ingresses":       strings.Fields("spec.ingressClassName spec.defaultBackend.service.name spec.defaultBackend.service.port.name spec.defaultBackend.service.port.number spec.defaultBackend.resource.kind spec.rules.host spec.rules.http.paths.path spec.rules.http.paths.pathType spec.rules.http.paths.backend.service.name spec.rules.http.paths.backend.service.port.name spec.rules.http.paths.backend.service.port.number spec.rules.http.paths.backend.resource.kind spec.tls.hosts spec.tls.secretName"),
	"kubernetes.ingressclasses":  strings.Fields("spec.controller spec.parameters.apiGroup spec.parameters.kind spec.parameters.name spec.parameters.namespace spec.parameters.scope"),
	"gateway.gateways":           strings.Fields("spec.gatewayClassName spec.listeners.name spec.listeners.port spec.listeners.protocol spec.listeners.hostname spec.listeners.allowedRoutes.namespaces.from spec.listeners.allowedRoutes.namespaces.selector.matchLabels.* spec.listeners.allowedRoutes.kinds.kind spec.listeners.allowedRoutes.kinds.group spec.listeners.tls.mode spec.listeners.tls.certificateRefs.group spec.listeners.tls.certificateRefs.kind spec.listeners.tls.certificateRefs.name spec.listeners.tls.certificateRefs.namespace spec.listeners.allowedRoutes.namespaces.selector.matchExpressions.key spec.listeners.allowedRoutes.namespaces.selector.matchExpressions.operator spec.listeners.allowedRoutes.namespaces.selector.matchExpressions.values"),
	"gateway.gatewayclasses":     strings.Fields("spec.controllerName spec.parametersRef.group spec.parametersRef.kind spec.parametersRef.name spec.parametersRef.namespace"),
	"gateway.httproutes":         gatewayTopologyPaths(),
	"gateway.grpcroutes":         gatewayTopologyPaths(),
	"gateway.tcproutes":          gatewayL4TopologyPaths(),
	"gateway.tlsroutes":          gatewayL4TopologyPaths(),
	"gateway.udproutes":          gatewayL4TopologyPaths(),
	"gateway.referencegrants":    strings.Fields("spec.from.group spec.from.kind spec.from.namespace spec.to.group spec.to.kind spec.to.name"),
	"traefik.ingressroutes":      traefikTopologyPaths(),
	"traefik.ingressroutetcps":   traefikTopologyPaths(),
	"traefik.ingressrouteudps":   traefikTopologyPaths(),
	"traefik.traefikservices":    strings.Fields("spec.weighted.services.name spec.weighted.services.namespace spec.weighted.services.kind spec.weighted.services.port spec.weighted.services.weight spec.mirroring.name spec.mirroring.namespace spec.mirroring.kind spec.mirroring.port spec.mirroring.mirrors.name spec.mirroring.mirrors.namespace spec.mirroring.mirrors.kind spec.mirroring.mirrors.port spec.mirroring.mirrors.percent"),
	"traefik.middlewares":        strings.Fields("spec.chain.middlewares.name spec.chain.middlewares.namespace"),
	"traefik.middlewaretcps":     strings.Fields("spec.chain.middlewares.name spec.chain.middlewares.namespace"),
	"apisix.apisixroutes":        apisixTopologyPaths(),
	"apisix.apisixupstreams":     strings.Fields("spec.scheme spec.loadbalancer.type spec.externalNodes.type spec.externalNodes.name spec.externalNodes.port spec.externalNodes.weight spec.subsets.name spec.subsets.labels.*"),
	"apisix.apisixtls":           strings.Fields("spec.hosts spec.secret.name spec.secret.namespace"),
}

func gatewayTopologyPaths() []string {
	return strings.Fields("spec.hostnames spec.parentRefs.group spec.parentRefs.kind spec.parentRefs.name spec.parentRefs.namespace spec.parentRefs.sectionName spec.parentRefs.port spec.rules.name spec.rules.matches.path.type spec.rules.matches.path.value spec.rules.matches.method spec.rules.matches.method.type spec.rules.matches.method.service spec.rules.matches.method.method spec.rules.matches.headers.name spec.rules.matches.headers.type spec.rules.matches.headers.value spec.rules.matches.queryParams.name spec.rules.matches.queryParams.type spec.rules.matches.queryParams.value spec.rules.backendRefs.group spec.rules.backendRefs.kind spec.rules.backendRefs.name spec.rules.backendRefs.namespace spec.rules.backendRefs.port spec.rules.backendRefs.weight spec.rules.filters.type spec.rules.filters.requestMirror.backendRef.group spec.rules.filters.requestMirror.backendRef.kind spec.rules.filters.requestMirror.backendRef.name spec.rules.filters.requestMirror.backendRef.namespace spec.rules.filters.requestMirror.backendRef.port spec.rules.filters.requestMirror.percent status.parents.parentRef.name status.parents.parentRef.namespace status.parents.parentRef.sectionName status.parents.conditions.type status.parents.conditions.status status.parents.conditions.reason status.parents.conditions.observedGeneration status.parents.conditions.lastTransitionTime")
}
func gatewayL4TopologyPaths() []string {
	return strings.Fields("spec.hostnames spec.parentRefs.group spec.parentRefs.kind spec.parentRefs.name spec.parentRefs.namespace spec.parentRefs.sectionName spec.parentRefs.port spec.rules.name spec.rules.backendRefs.group spec.rules.backendRefs.kind spec.rules.backendRefs.name spec.rules.backendRefs.namespace spec.rules.backendRefs.port spec.rules.backendRefs.weight status.parents.parentRef.name status.parents.parentRef.namespace status.parents.parentRef.sectionName status.parents.conditions.type status.parents.conditions.status status.parents.conditions.reason status.parents.conditions.observedGeneration status.parents.conditions.lastTransitionTime")
}
func traefikTopologyPaths() []string {
	return strings.Fields("spec.entryPoints spec.tls.passthrough spec.routes.kind spec.routes.match spec.routes.priority spec.routes.middlewares.name spec.routes.middlewares.namespace spec.routes.services.name spec.routes.services.namespace spec.routes.services.kind spec.routes.services.port spec.routes.services.weight spec.routes.services.scheme")
}
func apisixTopologyPaths() []string {
	return strings.Fields("spec.ingressClassName spec.http.name spec.http.priority spec.http.match.hosts spec.http.match.paths spec.http.match.methods spec.http.match.remoteAddrs spec.http.match.exprs.subject.scope spec.http.match.exprs.subject.name spec.http.match.exprs.op spec.http.match.exprs.value spec.http.match.exprs.set spec.http.backend.serviceName spec.http.backend.servicePort spec.http.backend.weight spec.http.backends.serviceName spec.http.backends.servicePort spec.http.backends.weight spec.http.plugins.name spec.http.plugins.enable spec.stream.name spec.stream.protocol spec.stream.match.ingressPort spec.stream.backend.serviceName spec.stream.backend.servicePort spec.stream.backend.weight")
}

func topologyLeaf(value any) (any, bool) {
	if scalarTopologyValue(value) {
		return value, true
	}
	if items, ok := value.([]any); ok {
		output := []any{}
		for _, item := range items {
			if !scalarTopologyValue(item) {
				return nil, false
			}
			output = append(output, item)
		}
		return output, true
	}
	return nil, false
}

func topologyMiddlewareTypes(input map[string]any) []string {
	allowed := strings.Fields("addPrefix basicAuth buffer circuitBreaker chain compress contentType digestAuth errors forwardAuth grpcWeb headers inFlightReq ipAllowList ipWhiteList passTLSClientCert rateLimit redirectRegex redirectScheme replacePath replacePathRegex retry stripPrefix stripPrefixRegex inFlightConn ipAllowListTCP")
	found := map[string]bool{}
	if spec, ok := input["spec"].(map[string]any); ok {
		for key := range spec {
			for _, name := range allowed {
				if key == name {
					found[name] = true
				}
			}
		}
	}
	if existing, ok := input["processorTypes"].([]any); ok {
		for _, value := range existing {
			for _, name := range allowed {
				if value == name {
					found[name] = true
				}
			}
		}
	}
	result := []string{}
	for name := range found {
		result = append(result, name)
	}
	sort.Strings(result)
	return result
}

func networkPolicyTopologyPaths() []string {
	paths := strings.Fields("spec.podSelector.matchLabels.* spec.podSelector.matchExpressions.key spec.podSelector.matchExpressions.operator spec.podSelector.matchExpressions.values spec.policyTypes")
	for _, direction := range []string{"ingress.from", "egress.to"} {
		for _, field := range strings.Fields("podSelector.matchLabels.* podSelector.matchExpressions.key podSelector.matchExpressions.operator podSelector.matchExpressions.values namespaceSelector.matchLabels.* namespaceSelector.matchExpressions.key namespaceSelector.matchExpressions.operator namespaceSelector.matchExpressions.values ipBlock.cidr ipBlock.except") {
			paths = append(paths, "spec."+direction+"."+field)
		}
	}
	return paths
}
