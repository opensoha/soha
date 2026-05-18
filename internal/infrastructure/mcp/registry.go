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
				ID:                      "platform-native.v1",
				SourceKind:              "platform-native",
				SupportedBackends:       []string{"platform"},
				Name:                    "platform-native.v1",
				Description:             "Platform-native inventory, event, alert, and delivery evidence contract",
				Category:                "platform",
				RequiresConfig:          false,
				SupportsSessionOverride: true,
				Scopes:                  []string{"clusters:read", "alerts:read", "builds:read", "releases:read"},
				Tools: []domainmcp.Tool{
					{Name: "k8s.events", Description: "Read Kubernetes and platform event signals within a scope and time range", SchemaHint: "scope + timeRange"},
					{Name: "deployments.recent_changes", Description: "Inspect recent deployment and release changes for a scope", SchemaHint: "cluster + namespace + workload"},
					{Name: "alerts.related", Description: "Resolve alerts related to a given alert, scope, or incident window", SchemaHint: "alert + scope + timeRange"},
				},
				DefaultBudget: map[string]any{"maxQueries": 5, "timeoutSeconds": 10},
				ToolSchemaSummary: map[string]string{
					"k8s.events":                 "scope + timeRange",
					"deployments.recent_changes": "cluster + namespace + workload",
					"alerts.related":             "alert + scope + timeRange",
				},
			},
			{
				ID:                      "logs.v1",
				SourceKind:              "logs",
				SupportedBackends:       []string{"es", "loki", "clickhouse"},
				Name:                    "logs.v1",
				Description:             "Unified structured log analysis contract across multiple log backends",
				Category:                "observability",
				RequiresConfig:          true,
				SupportsSessionOverride: true,
				Scopes:                  []string{"logs:read"},
				Tools: []domainmcp.Tool{
					{Name: "logs.search", Description: "Search structured logs within a scope and time range", SchemaHint: "scope + query + timeRange"},
					{Name: "logs.histogram", Description: "Aggregate log volume and error spikes by time bucket", SchemaHint: "scope + timeRange + groupBy"},
					{Name: "logs.top_signatures", Description: "Return top exception or error signatures in a scope", SchemaHint: "scope + query + timeRange"},
					{Name: "logs.context_window", Description: "Return focused log windows around a timestamp", SchemaHint: "scope + timestamp"},
					{Name: "logs.correlation", Description: "Summarize logs related to an alert or workload", SchemaHint: "alert/workload + scope + timeRange"},
				},
				DefaultBudget: map[string]any{"maxQueries": 10, "maxLogBytes": 65536, "timeoutSeconds": 12},
				ToolSchemaSummary: map[string]string{
					"logs.search":         "scope + query + timeRange",
					"logs.histogram":      "scope + timeRange + groupBy",
					"logs.top_signatures": "scope + query + timeRange",
					"logs.context_window": "scope + timestamp",
					"logs.correlation":    "alert/workload + scope + timeRange",
				},
			},
			{
				ID:                      "metrics.v1",
				SourceKind:              "metrics",
				SupportedBackends:       []string{"prometheus"},
				Name:                    "metrics.v1",
				Description:             "Unified metrics analysis contract across metric backends",
				Category:                "observability",
				RequiresConfig:          true,
				SupportsSessionOverride: true,
				Scopes:                  []string{"metrics:read"},
				Tools: []domainmcp.Tool{
					{Name: "metrics.range_query", Description: "Read a range metric series for a scope and time range", SchemaHint: "scope + metricKey + timeRange"},
					{Name: "metrics.anomaly_summary", Description: "Return summarized anomalies and spikes for a scope", SchemaHint: "scope + timeRange + metricPreset"},
				},
				DefaultBudget: map[string]any{"maxQueries": 8, "timeoutSeconds": 10},
				ToolSchemaSummary: map[string]string{
					"metrics.range_query":     "scope + metricKey + timeRange",
					"metrics.anomaly_summary": "scope + timeRange + metricPreset",
				},
			},
			{
				ID:                      "traces.v1",
				SourceKind:              "traces",
				SupportedBackends:       []string{"jaeger", "skywalking"},
				Name:                    "traces.v1",
				Description:             "Unified tracing analysis contract across trace backends",
				Category:                "observability",
				RequiresConfig:          true,
				SupportsSessionOverride: true,
				Scopes:                  []string{"traces:read"},
				Tools: []domainmcp.Tool{
					{Name: "traces.find_slow_spans", Description: "Find slow spans and latency hotspots in a scope", SchemaHint: "scope + timeRange + minDuration"},
				},
				DefaultBudget: map[string]any{"maxQueries": 6, "timeoutSeconds": 10},
				ToolSchemaSummary: map[string]string{
					"traces.find_slow_spans": "scope + timeRange + minDuration",
				},
			},
			{
				ID:                      "delivery.v1",
				SourceKind:              "delivery",
				SupportedBackends:       []string{"platform"},
				Name:                    "delivery.v1",
				Description:             "Unified delivery bootstrap and spec-render contract for application onboarding",
				Category:                "delivery",
				RequiresConfig:          false,
				SupportsSessionOverride: true,
				Scopes:                  []string{"applications:write", "delivery:write"},
				Tools: []domainmcp.Tool{
					{Name: "delivery.blueprints.list", Description: "List available delivery blueprints for application onboarding", SchemaHint: "workspace + module"},
					{Name: "delivery.spec.render", Description: "Render a delivery specification and scaffold files from a blueprint", SchemaHint: "blueprint + scope + template"},
					{Name: "delivery.targets.list", Description: "List target candidates for an application environment binding", SchemaHint: "cluster + namespace + search"},
					{Name: "delivery.application.bootstrap", Description: "Create or update platform delivery objects from a rendered blueprint", SchemaHint: "renderedSpec + mode"},
					{Name: "delivery.execution.start", Description: "Start delivery execution tasks for build or release orchestration", SchemaHint: "application + environment + action"},
				},
				DefaultBudget: map[string]any{"maxQueries": 12, "timeoutSeconds": 12},
				ToolSchemaSummary: map[string]string{
					"delivery.blueprints.list":       "workspace + module",
					"delivery.spec.render":           "blueprint + scope + template",
					"delivery.targets.list":          "cluster + namespace + search",
					"delivery.application.bootstrap": "renderedSpec + mode",
					"delivery.execution.start":       "application + environment + action",
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
