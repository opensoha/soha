package mcp

import (
	"time"

	domainmcp "github.com/kubecrux/kubecrux/internal/domain/mcp"
)

type Adapter = domainmcp.Adapter

type Registry struct {
	adapters []Adapter
}

func NewRegistry(defaultTimeout time.Duration) *Registry {
	_ = defaultTimeout
	return &Registry{
		adapters: []Adapter{
			{
				ID:                "platform-native.v1",
				SourceKind:        "platform-native",
				SupportedBackends: []string{"platform"},
				Name:              "platform-native.v1",
				Description:       "Platform-native inventory, event, alert, and delivery evidence contract",
				Scopes:            []string{"clusters:read", "alerts:read", "builds:read", "releases:read"},
				Tools: []domainmcp.Tool{
					{Name: "k8s.events", Description: "Read Kubernetes and platform event signals within a scope and time range"},
					{Name: "deployments.recent_changes", Description: "Inspect recent deployment and release changes for a scope"},
					{Name: "alerts.related", Description: "Resolve alerts related to a given alert, scope, or incident window"},
				},
			},
			{
				ID:                "logs.v1",
				SourceKind:        "logs",
				SupportedBackends: []string{"es", "loki", "clickhouse"},
				Name:              "logs.v1",
				Description:       "Unified structured log analysis contract across multiple log backends",
				Scopes:            []string{"logs:read"},
				Tools: []domainmcp.Tool{
					{Name: "logs.search", Description: "Search structured logs within a scope and time range"},
					{Name: "logs.histogram", Description: "Aggregate log volume and error spikes by time bucket"},
					{Name: "logs.top_signatures", Description: "Return top exception or error signatures in a scope"},
					{Name: "logs.context_window", Description: "Return focused log windows around a timestamp"},
					{Name: "logs.correlation", Description: "Summarize logs related to an alert or workload"},
				},
			},
			{
				ID:                "metrics.v1",
				SourceKind:        "metrics",
				SupportedBackends: []string{"prometheus"},
				Name:              "metrics.v1",
				Description:       "Unified metrics analysis contract across metric backends",
				Scopes:            []string{"metrics:read"},
				Tools: []domainmcp.Tool{
					{Name: "metrics.range_query", Description: "Read a range metric series for a scope and time range"},
					{Name: "metrics.anomaly_summary", Description: "Return summarized anomalies and spikes for a scope"},
				},
			},
			{
				ID:                "traces.v1",
				SourceKind:        "traces",
				SupportedBackends: []string{"jaeger"},
				Name:              "traces.v1",
				Description:       "Unified tracing analysis contract across trace backends",
				Scopes:            []string{"traces:read"},
				Tools: []domainmcp.Tool{
					{Name: "traces.find_slow_spans", Description: "Find slow spans and latency hotspots in a scope"},
				},
			},
		},
	}
}

func (r *Registry) List() []Adapter {
	out := make([]Adapter, len(r.adapters))
	copy(out, r.adapters)
	return out
}

func (r *Registry) Get(adapterID string) (Adapter, bool) {
	for _, item := range r.adapters {
		if item.ID == adapterID {
			return item, true
		}
	}
	return Adapter{}, false
}

func (r *Registry) ListCapabilities() []domainmcp.Capability {
	items := make([]domainmcp.Capability, 0)
	for _, adapter := range r.adapters {
		for _, tool := range adapter.Tools {
			items = append(items, domainmcp.Capability{
				AdapterID:   adapter.ID,
				Name:        tool.Name,
				Description: tool.Description,
				Scopes:      adapter.Scopes,
			})
		}
	}
	return items
}
