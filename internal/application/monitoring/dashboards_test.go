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

type stubDashboardRepository struct {
	item domainobservability.Dashboard
}

func (s stubDashboardRepository) ListDashboards(context.Context) ([]domainobservability.Dashboard, error) {
	return []domainobservability.Dashboard{s.item}, nil
}

func (s stubDashboardRepository) GetDashboard(context.Context, string) (domainobservability.Dashboard, error) {
	return s.item, nil
}

func (s stubDashboardRepository) CreateDashboard(_ context.Context, item domainobservability.Dashboard) (domainobservability.Dashboard, error) {
	return item, nil
}

func (stubDashboardRepository) DeleteDashboard(context.Context, string) error { return nil }

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
	if result.ImportedPanelCount != 2 || result.SkippedPanelCount != 0 {
		t.Fatalf("counts = imported %d skipped %d", result.ImportedPanelCount, result.SkippedPanelCount)
	}
	panel := result.Dashboard.Panels[0]
	if !panel.Queryable || len(panel.Targets) != 1 || panel.Targets[0].RefID != "A" {
		t.Fatalf("panel = %#v", panel)
	}
	if len(result.Dashboard.Tags) != 1 || len(result.Warnings) < 2 {
		t.Fatalf("tags/warnings = %#v / %#v", result.Dashboard.Tags, result.Warnings)
	}
}

func TestImportGrafanaDashboardPreservesVariablesBindingsAndUnsupportedPanels(t *testing.T) {
	raw := `{
		"title":"Compatibility",
		"templating":{"list":[{"name":"namespace","label":"Namespace","type":"custom","query":"default,production","current":{"value":"production"}}]},
		"panels":[
			{"id":1,"title":"Requests","type":"gauge","datasource":{"type":"prometheus","uid":"prom-main"},"targets":[{"refId":"A","expr":"sum(http_requests_total{namespace=\"$namespace\"})"}]},
			{"id":2,"title":"Plugin","type":"custom-plugin","pluginVersion":"1.2.3"}
		]
	}`

	result, err := importGrafanaDashboard(raw, "prometheus-main", time.Now())
	if err != nil {
		t.Fatalf("import compatibility dashboard: %v", err)
	}
	if result.Dashboard.SourceFormat != "classic" || result.Dashboard.RawJSON != raw || len(result.Dashboard.Variables) != 1 {
		t.Fatalf("dashboard fidelity = %#v", result.Dashboard)
	}
	if got := result.Dashboard.Variables[0]; got.Name != "namespace" || got.Current != "production" || len(got.Options) != 2 {
		t.Fatalf("variable = %#v", got)
	}
	if len(result.Dashboard.DataSourceBindings) != 1 || result.Dashboard.Panels[0].Targets[0].DataSourceUID != "prom-main" {
		t.Fatalf("bindings/panel = %#v / %#v", result.Dashboard.DataSourceBindings, result.Dashboard.Panels[0])
	}
	unsupported := result.Dashboard.Panels[1]
	if unsupported.Type != "unsupported" || !unsupported.Unsupported || !strings.Contains(unsupported.RawJSON, `"pluginVersion":"1.2.3"`) {
		t.Fatalf("unsupported panel = %#v", unsupported)
	}
}

func TestImportGrafanaDashboardRejectsV2Resource(t *testing.T) {
	_, err := importGrafanaDashboard(`{"apiVersion":"dashboard.grafana.app/v2beta1","kind":"Dashboard","spec":{"title":"V2"}}`, "prometheus-main", time.Now())
	if err == nil || !strings.Contains(err.Error(), "V2 resources are not supported") {
		t.Fatalf("expected explicit V2 rejection, got %v", err)
	}
}

func TestResolveDashboardVariablesRejectsUnapprovedValues(t *testing.T) {
	definitions := []domainobservability.DashboardVariable{{Name: "namespace", Current: "default", Options: []string{"default", "production"}}}
	values, err := resolveDashboardVariables(definitions, map[string]string{"namespace": "production"})
	if err != nil || values["namespace"] != "production" {
		t.Fatalf("resolved variables = %#v, %v", values, err)
	}
	if _, err := resolveDashboardVariables(definitions, map[string]string{"namespace": `production\"} or vector(1)`}); err == nil {
		t.Fatal("expected unapproved variable value to fail")
	}
}

func TestQueryDashboardPanelValidatesBindingsBeforeBackendAndReturnsSnapshot(t *testing.T) {
	now := time.Now().UTC()
	metricBackend := &stubMetricTelemetry{}
	dashboard := domainobservability.Dashboard{
		ID: "dashboard:1", DataSourceID: "metrics",
		Variables:          []domainobservability.DashboardVariable{{Name: "namespace", Current: "default", Options: []string{"default", "production"}}},
		DataSourceBindings: []domainobservability.DashboardDataSourceBinding{{Type: "prometheus", UID: "prom-main", DataSourceID: "metrics"}},
		Panels: []domainobservability.DashboardPanel{{
			ID: "requests", Type: "timeseries", Queryable: true,
			Targets: []domainobservability.DashboardTarget{{
				RefID: "A", Expression: `sum(http_requests_total{namespace="$namespace"})`,
				DataSourceType: "prometheus", DataSourceUID: "missing", DataSourceID: "metrics",
			}},
		}},
	}
	service := &Service{
		dashboards: stubDashboardRepository{item: dashboard}, metrics: metricBackend,
		dataSources: stubSignalDataSources{items: []domaincopilot.DataSource{{
			ID: "metrics", BackendType: "prometheus", Enabled: true, Config: map[string]any{"endpoint": "http://prometheus:9090"},
		}}},
		permissions: monitoringCompatPermissions(appaccess.PermObserveMonitoringView),
	}

	_, err := service.QueryDashboardPanel(context.Background(), monitoringCompatPrincipal(), dashboard.ID, "requests", now.Add(-time.Hour), now, time.Minute, map[string]string{"namespace": "production"})
	if err == nil || metricBackend.called {
		t.Fatalf("expected invalid binding before backend call, err=%v called=%v", err, metricBackend.called)
	}

	dashboard.Panels[0].Targets[0].DataSourceUID = "prom-main"
	service.dashboards = stubDashboardRepository{item: dashboard}
	result, err := service.QueryDashboardPanel(context.Background(), monitoringCompatPrincipal(), dashboard.ID, "requests", now.Add(-time.Hour), now, time.Minute, map[string]string{"namespace": "production"})
	if err != nil {
		t.Fatalf("query approved dashboard panel: %v", err)
	}
	if !metricBackend.called || !strings.Contains(metricBackend.query.Expression, `namespace="production"`) {
		t.Fatalf("backend query = %#v", metricBackend.query)
	}
	if result.Meta == nil || result.Meta.Snapshot["query"] != metricBackend.query.Expression {
		t.Fatalf("query meta = %#v", result.Meta)
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
