package monitoring

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainobservability "github.com/opensoha/soha/internal/domain/observability"
)

func TestListMetricDataSourcesOnlyReturnsEnabledPrometheusSources(t *testing.T) {
	service := &Service{
		dataSources: stubSignalDataSources{items: []domaincopilot.DataSource{
			{ID: "logs", Name: "Logs", BackendType: "loki", Enabled: true},
			{ID: "disabled", Name: "Disabled", BackendType: "prometheus", Enabled: false},
			{ID: "missing-endpoint", Name: "Missing endpoint", BackendType: "prometheus", Enabled: true},
			{ID: "prom-b", Name: "Zeta", BackendType: "PROMETHEUS", Enabled: true, Config: map[string]any{"endpoint": "http://prometheus-b:9090"}},
			{ID: "prom-a", Name: "Alpha", BackendType: "prometheus", Enabled: true, Config: map[string]any{"endpoint": "http://prometheus-a:9090"}},
		}},
		permissions: monitoringCompatPermissions(appaccess.PermObserveMonitoringView),
	}

	items, err := service.ListMetricDataSources(context.Background(), monitoringCompatPrincipal())
	if err != nil {
		t.Fatalf("list metric data sources: %v", err)
	}
	if len(items) != 2 || items[0].ID != "prom-a" || items[1].ID != "prom-b" {
		t.Fatalf("metric data sources = %#v", items)
	}
}

func TestImportGrafanaDashboardSkipsExplicitNonPrometheusTargets(t *testing.T) {
	raw := `{
		"title":"Mixed Sources",
		"panels":[{
			"id":1,
			"title":"Requests",
			"type":"timeseries",
			"targets":[
				{"refId":"A","expr":"sum(rate(http_requests_total[5m]))","datasource":{"type":"prometheus","uid":"prom"}},
				{"refId":"B","expr":"{app=\"api\"}","datasource":{"type":"loki","uid":"logs"}}
			]
		}]
	}`

	result, err := importGrafanaDashboard(raw, "prometheus-main", time.Now())
	if err != nil {
		t.Fatalf("import mixed dashboard: %v", err)
	}
	if len(result.Dashboard.Panels[0].Targets) != 1 || result.Dashboard.Panels[0].Targets[0].RefID != "A" {
		t.Fatalf("targets = %#v", result.Dashboard.Panels[0].Targets)
	}
	if !slices.ContainsFunc(result.Warnings, func(item domainobservability.DashboardImportWarning) bool {
		return item.Code == "skipped_target" && strings.Contains(item.Message, "non-Prometheus")
	}) {
		t.Fatalf("warnings = %#v", result.Warnings)
	}
}

func TestImportGrafanaDashboardNormalizesSupportedPanels(t *testing.T) {
	raw := `{
		"title":"Kubernetes Overview",
		"schemaVersion":39,
		"tags":["kubernetes","kubernetes"],
		"templating":{"list":[{"name":"namespace"}]},
		"panels":[
			{"id":1,"title":"CPU","type":"timeseries","gridPos":{"x":0,"y":0,"w":12,"h":8},"targets":[
				{"refId":"A","expr":"sum(rate(container_cpu_usage_seconds_total[$__rate_interval]))","legendFormat":"{{pod}}"},
				{"refId":"B","expr":"sum(rate(http_requests_total{namespace=\"$namespace\"}[5m]))"}
			]},
			{"id":2,"title":"Requests","type":"table","targets":[]}
		]
	}`

	result, err := importGrafanaDashboard(raw, "prometheus-main", time.Date(2026, 8, 8, 8, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("import dashboard: %v", err)
	}
	if result.ImportedPanelCount != 1 || result.SkippedPanelCount != 1 {
		t.Fatalf("counts = imported %d skipped %d", result.ImportedPanelCount, result.SkippedPanelCount)
	}
	panel := result.Dashboard.Panels[0]
	if !panel.Queryable || len(panel.Targets) != 1 || panel.Targets[0].RefID != "A" {
		t.Fatalf("panel = %#v", panel)
	}
	if len(result.Dashboard.Tags) != 1 || len(result.Warnings) < 3 {
		t.Fatalf("tags/warnings = %#v / %#v", result.Dashboard.Tags, result.Warnings)
	}
}

func TestImportGrafanaDashboardRequiresDataSource(t *testing.T) {
	_, err := importGrafanaDashboard(`{"title":"Missing source","panels":[{"id":1,"title":"CPU","type":"timeseries"}]}`, "", time.Now())
	if err == nil || !strings.Contains(err.Error(), "Prometheus data source is required") {
		t.Fatalf("expected missing data source error, got %v", err)
	}
}

func TestImportGrafanaDashboardAcceptsV1Spec(t *testing.T) {
	result, err := importGrafanaDashboard(`{"kind":"Dashboard","spec":{"title":"Wrapped","panels":[{"id":"text","title":"Notes","type":"text","options":{"content":"hello"}}]}}`, "prometheus-main", time.Now())
	if err != nil {
		t.Fatalf("import wrapped dashboard: %v", err)
	}
	if result.Dashboard.Name != "Wrapped" || result.Dashboard.Panels[0].Markdown != "hello" {
		t.Fatalf("dashboard = %#v", result.Dashboard)
	}
}

func TestExpandGrafanaBuiltins(t *testing.T) {
	from := time.Date(2026, 8, 8, 8, 0, 0, 0, time.UTC)
	to := from.Add(15 * time.Minute)
	expression := expandGrafanaBuiltins(`rate(requests[$__rate_interval]) / $__range_s`, from, to, 30*time.Second)
	if expression != `rate(requests[2m]) / 900` || strings.Contains(expression, "$__") {
		t.Fatalf("expression = %q", expression)
	}
}
