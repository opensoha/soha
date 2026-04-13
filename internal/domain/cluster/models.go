package cluster

import (
	"context"
	"time"
)

type ConnectionMode string

const (
	ConnectionModeDirectKubeconfig ConnectionMode = "direct_kubeconfig"
	ConnectionModeAgent            ConnectionMode = "agent"
)

type Health struct {
	Status      string    `json:"status"`
	Message     string    `json:"message,omitempty"`
	LastChecked time.Time `json:"lastChecked,omitempty"`
}

type Summary struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Region         string            `json:"region"`
	Environment    string            `json:"environment"`
	Labels         map[string]string `json:"labels"`
	ConnectionMode ConnectionMode    `json:"connectionMode"`
	Version        string            `json:"version,omitempty"`
	Capabilities   []string          `json:"capabilities,omitempty"`
	Health         Health            `json:"health"`
}

type Detail struct {
	Summary     Summary          `json:"summary"`
	Diagnostics Diagnostics      `json:"diagnostics"`
	Connection  ConnectionDetail `json:"connection"`
	Monitoring  MonitoringDetail `json:"monitoring"`
}

type Diagnostics struct {
	Transport       string    `json:"transport"`
	SyncStrategy    string    `json:"syncStrategy"`
	CacheStatus     string    `json:"cacheStatus"`
	CacheReady      bool      `json:"cacheReady"`
	LastChecked     time.Time `json:"lastChecked,omitempty"`
	ConnectionState string    `json:"connectionState"`
	Message         string    `json:"message,omitempty"`
}

type ConnectionDetail struct {
	Mode                ConnectionMode `json:"mode"`
	CredentialType      string         `json:"credentialType"`
	SourceType          string         `json:"sourceType"`
	SourceRef           string         `json:"sourceRef,omitempty"`
	Context             string         `json:"context,omitempty"`
	Endpoint            string         `json:"endpoint,omitempty"`
	HasInlineKubeconfig bool           `json:"hasInlineKubeconfig"`
	HasToken            bool           `json:"hasToken"`
	UsesInformerCache   bool           `json:"usesInformerCache"`
}

type MonitoringDetail struct {
	Prometheus PrometheusDetail `json:"prometheus"`
}

type PrometheusDetail struct {
	BaseURL        string `json:"baseUrl,omitempty"`
	ClusterLabel   string `json:"clusterLabel,omitempty"`
	GrafanaBaseURL string `json:"grafanaBaseUrl,omitempty"`
	HasBearerToken bool   `json:"hasBearerToken"`
}

type Connection struct {
	Summary        Summary        `json:"summary"`
	CredentialType string         `json:"credentialType"`
	SourceType     string         `json:"sourceType"`
	SourceRef      string         `json:"sourceRef,omitempty"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

type RegisterInput struct {
	ID                     string            `json:"id"`
	Name                   string            `json:"name"`
	Region                 string            `json:"region"`
	Environment            string            `json:"environment"`
	Labels                 map[string]string `json:"labels,omitempty"`
	ConnectionMode         ConnectionMode    `json:"connectionMode"`
	Kubeconfig             string            `json:"kubeconfig,omitempty"`
	Context                string            `json:"context,omitempty"`
	AgentEndpoint          string            `json:"agentEndpoint,omitempty"`
	AgentToken             string            `json:"agentToken,omitempty"`
	PrometheusBaseURL      string            `json:"prometheusBaseUrl,omitempty"`
	PrometheusBearerToken  string            `json:"prometheusBearerToken,omitempty"`
	PrometheusClusterLabel string            `json:"prometheusClusterLabel,omitempty"`
	GrafanaBaseURL         string            `json:"grafanaBaseUrl,omitempty"`
}

type UpdateInput struct {
	Name                   string            `json:"name"`
	Region                 string            `json:"region"`
	Environment            string            `json:"environment"`
	Labels                 map[string]string `json:"labels,omitempty"`
	ConnectionMode         ConnectionMode    `json:"connectionMode"`
	Kubeconfig             string            `json:"kubeconfig,omitempty"`
	Context                string            `json:"context,omitempty"`
	AgentEndpoint          string            `json:"agentEndpoint,omitempty"`
	AgentToken             string            `json:"agentToken,omitempty"`
	PrometheusBaseURL      string            `json:"prometheusBaseUrl,omitempty"`
	PrometheusBearerToken  string            `json:"prometheusBearerToken,omitempty"`
	PrometheusClusterLabel string            `json:"prometheusClusterLabel,omitempty"`
	GrafanaBaseURL         string            `json:"grafanaBaseUrl,omitempty"`
}

type Manager interface {
	ListClusters(context.Context) ([]Summary, error)
	GetCluster(context.Context, string) (Summary, error)
}
