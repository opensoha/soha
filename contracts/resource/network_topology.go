package resource

const (
	NetworkTopologyNamedRootFeature = "network.topology.named-root.v1"
	NetworkTopologySnapshotFeature  = "network.topology.snapshot.v1"
	NetworkTopologyGatewayL4Feature = "network.topology.gateway-l4.v1"
	NetworkTopologyMaxQueries       = 64
	NetworkTopologyMaxSourceItems   = 200
	NetworkTopologyMaxSourceBytes   = 1 << 20
	NetworkTopologyMaxTotalBytes    = 4 << 20
	NetworkTopologyMaxObjectBytes   = 64 << 10
	NetworkTopologyDefaultNodes     = 200
	NetworkTopologyDefaultEdges     = 400
	NetworkTopologyMaxNodes         = 500
	NetworkTopologyMaxEdges         = 1000
)

type NetworkTopologyGraphNode struct {
	ID              string                     `json:"id"`
	Type            string                     `json:"type"`
	Label           string                     `json:"label"`
	Provider        string                     `json:"provider"`
	Protocol        string                     `json:"protocol,omitempty"`
	ResolutionState string                     `json:"resolutionState"`
	Resource        *ResourceRef               `json:"resource,omitempty"`
	Rule            *NetworkTopologyRule       `json:"rule,omitempty"`
	Properties      []NetworkTopologyProperty  `json:"properties,omitempty"`
	Conditions      []NetworkTopologyCondition `json:"conditions,omitempty"`
	Generation      int64                      `json:"generation,omitempty"`
}

type NetworkTopologyGraphEdge struct {
	ID              string       `json:"id"`
	Source          string       `json:"source"`
	Target          string       `json:"target"`
	Relation        string       `json:"relation"`
	ResolutionState string       `json:"resolutionState"`
	Protocol        string       `json:"protocol,omitempty"`
	Port            string       `json:"port,omitempty"`
	SectionName     string       `json:"sectionName,omitempty"`
	FieldPath       string       `json:"fieldPath,omitempty"`
	Weight          *int64       `json:"weight,omitempty"`
	TotalWeight     *int64       `json:"totalWeight,omitempty"`
	MirrorPercent   *float64     `json:"mirrorPercent,omitempty"`
	RuleRef         string       `json:"ruleRef,omitempty"`
	SourceRef       *ResourceRef `json:"sourceRef,omitempty"`
	Note            string       `json:"note,omitempty"`
}

type NetworkTopologyRule struct {
	Name         string                   `json:"name,omitempty"`
	Index        int                      `json:"index"`
	MatchGroups  [][]NetworkTopologyMatch `json:"matchGroups,omitempty"`
	MatchSummary string                   `json:"matchSummary,omitempty"`
	Priority     *int64                   `json:"priority,omitempty"`
	TLSMode      string                   `json:"tlsMode,omitempty"`
}

type NetworkTopologyMatch struct {
	Type     string `json:"type"`
	Name     string `json:"name,omitempty"`
	Operator string `json:"operator,omitempty"`
	Value    string `json:"value,omitempty"`
}

type NetworkTopologyProperty struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

type NetworkTopologyCondition struct {
	Type               string `json:"type"`
	Status             string `json:"status"`
	Reason             string `json:"reason,omitempty"`
	ObservedGeneration *int64 `json:"observedGeneration,omitempty"`
	LastTransitionTime string `json:"lastTransitionTime,omitempty"`
}

type NetworkTopologyCoverage struct {
	Name           string   `json:"name,omitempty"`
	Provider       string   `json:"provider"`
	SourceID       string   `json:"sourceId"`
	State          string   `json:"state"`
	Namespace      string   `json:"namespace,omitempty"`
	SupportedKinds []string `json:"supportedKinds,omitempty"`
	ReasonCode     string   `json:"reasonCode,omitempty"`
	CheckedAt      string   `json:"checkedAt,omitempty"`
	ObservedAt     string   `json:"observedAt,omitempty"`
	Truncated      bool     `json:"truncated,omitempty"`
}

// Snapshot queries name a fixed source, never a caller-supplied GVR or field path.
type NetworkTopologySourceQuery struct {
	Name      string `json:"name,omitempty"`
	SourceID  string `json:"sourceId"`
	Namespace string `json:"namespace,omitempty"`
}

type NetworkTopologySnapshotRequest struct {
	Queries []NetworkTopologySourceQuery `json:"queries"`
}

type NetworkTopologySourceObject struct {
	Resource   ResourceRef `json:"resource"`
	Generation int64       `json:"generation,omitempty"`
	// Content contains only the source's contract-allowlisted projection, not an original manifest.
	Content string `json:"content"`
}

type NetworkTopologySourceResult struct {
	Coverage NetworkTopologyCoverage       `json:"coverage"`
	Objects  []NetworkTopologySourceObject `json:"objects"`
}

type NetworkTopologySnapshot struct {
	Version string                        `json:"version"`
	Sources []NetworkTopologySourceResult `json:"sources"`
}

type NetworkTopologySourceDefinition struct {
	ID              string
	Provider        string
	FallbackVersion string
	CRDResourceDefinition
}

var networkTopologySources = [...]NetworkTopologySourceDefinition{
	{ID: "kubernetes.networkpolicies", Provider: "kubernetes", CRDResourceDefinition: CRDResourceDefinition{Kind: "NetworkPolicy", Group: "networking.k8s.io", Version: "v1", Resource: "networkpolicies", Namespaced: true}},
	{ID: "kubernetes.namespaces", Provider: "kubernetes", CRDResourceDefinition: CRDResourceDefinition{Kind: "Namespace", Version: "v1", Resource: "namespaces"}},
	{ID: "kubernetes.configmaps", Provider: "kubernetes", CRDResourceDefinition: CRDResourceDefinition{Kind: "ConfigMap", Version: "v1", Resource: "configmaps", Namespaced: true}},
	{ID: "gateway.backendtlspolicies", Provider: "gateway", FallbackVersion: "v1alpha3", CRDResourceDefinition: CRDResourceDefinition{Kind: "BackendTLSPolicy", Group: "gateway.networking.k8s.io", Version: "v1", Resource: "backendtlspolicies", Namespaced: true}},
	{ID: "kubernetes.services", Provider: "kubernetes", CRDResourceDefinition: CRDResourceDefinition{Kind: "Service", Version: "v1", Resource: "services", Namespaced: true}},
	{ID: "kubernetes.pods", Provider: "kubernetes", CRDResourceDefinition: CRDResourceDefinition{Kind: "Pod", Version: "v1", Resource: "pods", Namespaced: true}},
	{ID: "kubernetes.endpointslices", Provider: "kubernetes", CRDResourceDefinition: CRDResourceDefinition{Kind: "EndpointSlice", Group: "discovery.k8s.io", Version: "v1", Resource: "endpointslices", Namespaced: true}},
	{ID: "kubernetes.ingresses", Provider: "kubernetes", CRDResourceDefinition: CRDResourceDefinition{Kind: "Ingress", Group: "networking.k8s.io", Version: "v1", Resource: "ingresses", Namespaced: true}},
	{ID: "kubernetes.ingressclasses", Provider: "kubernetes", CRDResourceDefinition: CRDResourceDefinition{Kind: "IngressClass", Group: "networking.k8s.io", Version: "v1", Resource: "ingressclasses"}},
	{ID: "gateway.gateways", Provider: "gateway", CRDResourceDefinition: CRDResourceDefinition{Kind: "Gateway", Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gateways", Namespaced: true}},
	{ID: "gateway.gatewayclasses", Provider: "gateway", CRDResourceDefinition: CRDResourceDefinition{Kind: "GatewayClass", Group: "gateway.networking.k8s.io", Version: "v1", Resource: "gatewayclasses"}},
	{ID: "gateway.httproutes", Provider: "gateway", CRDResourceDefinition: CRDResourceDefinition{Kind: "HTTPRoute", Group: "gateway.networking.k8s.io", Version: "v1", Resource: "httproutes", Namespaced: true}},
	{ID: "gateway.grpcroutes", Provider: "gateway", CRDResourceDefinition: CRDResourceDefinition{Kind: "GRPCRoute", Group: "gateway.networking.k8s.io", Version: "v1", Resource: "grpcroutes", Namespaced: true}},
	{ID: "gateway.tcproutes", Provider: "gateway", FallbackVersion: "v1alpha2", CRDResourceDefinition: CRDResourceDefinition{Kind: "TCPRoute", Group: "gateway.networking.k8s.io", Version: "v1", Resource: "tcproutes", Namespaced: true}},
	{ID: "gateway.tlsroutes", Provider: "gateway", FallbackVersion: "v1alpha2", CRDResourceDefinition: CRDResourceDefinition{Kind: "TLSRoute", Group: "gateway.networking.k8s.io", Version: "v1", Resource: "tlsroutes", Namespaced: true}},
	{ID: "gateway.udproutes", Provider: "gateway", FallbackVersion: "v1alpha2", CRDResourceDefinition: CRDResourceDefinition{Kind: "UDPRoute", Group: "gateway.networking.k8s.io", Version: "v1", Resource: "udproutes", Namespaced: true}},
	{ID: "gateway.referencegrants", Provider: "gateway", FallbackVersion: "v1beta1", CRDResourceDefinition: CRDResourceDefinition{Kind: "ReferenceGrant", Group: "gateway.networking.k8s.io", Version: "v1", Resource: "referencegrants", Namespaced: true}},
	{ID: "traefik.ingressroutes", Provider: "traefik", CRDResourceDefinition: CRDResourceDefinition{Kind: "IngressRoute", Group: "traefik.io", Version: "v1alpha1", Resource: "ingressroutes", Namespaced: true}},
	{ID: "traefik.ingressroutetcps", Provider: "traefik", CRDResourceDefinition: CRDResourceDefinition{Kind: "IngressRouteTCP", Group: "traefik.io", Version: "v1alpha1", Resource: "ingressroutetcps", Namespaced: true}},
	{ID: "traefik.ingressrouteudps", Provider: "traefik", CRDResourceDefinition: CRDResourceDefinition{Kind: "IngressRouteUDP", Group: "traefik.io", Version: "v1alpha1", Resource: "ingressrouteudps", Namespaced: true}},
	{ID: "traefik.traefikservices", Provider: "traefik", CRDResourceDefinition: CRDResourceDefinition{Kind: "TraefikService", Group: "traefik.io", Version: "v1alpha1", Resource: "traefikservices", Namespaced: true}},
	{ID: "traefik.middlewares", Provider: "traefik", CRDResourceDefinition: CRDResourceDefinition{Kind: "Middleware", Group: "traefik.io", Version: "v1alpha1", Resource: "middlewares", Namespaced: true}},
	{ID: "traefik.middlewaretcps", Provider: "traefik", CRDResourceDefinition: CRDResourceDefinition{Kind: "MiddlewareTCP", Group: "traefik.io", Version: "v1alpha1", Resource: "middlewaretcps", Namespaced: true}},
	{ID: "apisix.apisixroutes", Provider: "apisix", CRDResourceDefinition: CRDResourceDefinition{Kind: "ApisixRoute", Group: "apisix.apache.org", Version: "v2", Resource: "apisixroutes", Namespaced: true}},
	{ID: "apisix.apisixupstreams", Provider: "apisix", CRDResourceDefinition: CRDResourceDefinition{Kind: "ApisixUpstream", Group: "apisix.apache.org", Version: "v2", Resource: "apisixupstreams", Namespaced: true}},
	{ID: "apisix.apisixtls", Provider: "apisix", CRDResourceDefinition: CRDResourceDefinition{Kind: "ApisixTls", Group: "apisix.apache.org", Version: "v2", Resource: "apisixtls", Namespaced: true}},
}

func NetworkTopologySources() []NetworkTopologySourceDefinition {
	return append([]NetworkTopologySourceDefinition(nil), networkTopologySources[:]...)
}

func IsGatewayL4TopologySource(id string) bool {
	return id == "gateway.tcproutes" || id == "gateway.tlsroutes" || id == "gateway.udproutes"
}

func (d NetworkTopologySourceDefinition) RequiresCustomResourceAccess() bool {
	return d.Provider == "traefik" || d.Provider == "apisix" || IsGatewayL4TopologySource(d.ID)
}

func LookupNetworkTopologySource(id string) (NetworkTopologySourceDefinition, bool) {
	for _, definition := range networkTopologySources {
		if definition.ID == id {
			return definition, true
		}
	}
	return NetworkTopologySourceDefinition{}, false
}

// NetworkTopologySourceVersions is the explicit served-version allowlist. Legacy
// BackendTLSPolicy v1alpha2 uses targetRef rather than targetRefs.
func NetworkTopologySourceVersions(d NetworkTopologySourceDefinition) []string {
	versions := []string{d.Version}
	if d.FallbackVersion != "" {
		versions = append(versions, d.FallbackVersion)
	}
	if d.ID == "gateway.backendtlspolicies" {
		versions = append(versions, "v1alpha2")
	}
	return versions
}
