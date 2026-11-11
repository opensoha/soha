package resource

// CustomResourceAccessRequest checks the Agent's own Kubernetes identity.
type CustomResourceAccessRequest struct {
	Definition CRDResourceDefinition `json:"definition"`
	Namespace  string                `json:"namespace"`
	Name       string                `json:"name,omitempty"`
}

type CustomResourceAccess struct {
	AllowedActions []string `json:"allowedActions"`
}

// PrometheusQuery carries a bounded query for an already configured endpoint.
// Endpoint must match the Agent configuration; it is never a proxy destination.
type PrometheusQuery struct {
	Endpoint string `json:"endpoint"`
	Query    string `json:"query"`
	Kind     string `json:"kind"`
	Start    int64  `json:"start,omitempty"`
	End      int64  `json:"end,omitempty"`
	Step     int64  `json:"step,omitempty"`
}

// AgentActionCheck separates binary support, authenticated action policy, and Kubernetes authorization.
// RBAC scope is cluster-wide; target-specific checks and mutations remain authoritative.
type AgentActionCheck struct {
	Action         string `json:"action"`
	Implementation string `json:"implementation"`
	Configured     string `json:"configured"`
	KubernetesRBAC string `json:"kubernetesRBAC"`
	CheckedAt      string `json:"checkedAt,omitempty"`
	ReasonCode     string `json:"reasonCode,omitempty"`
	Scope          string `json:"scope,omitempty"`
}
