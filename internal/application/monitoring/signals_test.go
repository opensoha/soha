package monitoring

import (
	"context"
	"errors"
	"testing"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/telemetry"
)

type stubSignalDataSources struct {
	items []domaincopilot.DataSource
}

func (s stubSignalDataSources) ListDataSources(context.Context) ([]domaincopilot.DataSource, error) {
	return append([]domaincopilot.DataSource(nil), s.items...), nil
}

func (s stubSignalDataSources) GetDataSource(_ context.Context, id string) (domaincopilot.DataSource, error) {
	for _, item := range s.items {
		if item.ID == id {
			return item, nil
		}
	}
	return domaincopilot.DataSource{}, apperrors.ErrNotFound
}

type stubMetricTelemetry struct {
	called bool
	query  telemetry.MetricRangeQuery
}

func (s *stubMetricTelemetry) RangeQuery(_ context.Context, _ string, _ string, _ map[string]any, query telemetry.MetricRangeQuery) ([]telemetry.MetricSeries, map[string]any, error) {
	s.called = true
	s.query = query
	return []telemetry.MetricSeries{{Key: "cpu_usage", Label: "CPU", Latest: 0.5}}, nil, nil
}

func (s *stubMetricTelemetry) Analyze(context.Context, string, string, map[string]any, telemetry.MetricRangeQuery) (telemetry.MetricAnomalySummary, error) {
	return telemetry.MetricAnomalySummary{}, nil
}

type stubTraceTelemetry struct {
	called *bool
}

func (s stubTraceTelemetry) FindSlowSpans(context.Context, string, string, map[string]any, telemetry.TraceQuery) (telemetry.TraceResult, error) {
	if s.called != nil {
		*s.called = true
	}
	return telemetry.TraceResult{
		Summary: "2 spans",
		Spans: []telemetry.TraceSpan{
			{TraceID: "trace-1", SpanID: "span-1", Service: "worker"},
			{TraceID: "trace-1", SpanID: "span-2", Service: "api"},
			{TraceID: "trace-2", SpanID: "span-3", Service: "worker"},
		},
	}, nil
}

type stubServiceTelemetry struct{}

func (stubServiceTelemetry) ListServices(context.Context, string, string, map[string]any, telemetry.ServiceQuery) (telemetry.ServiceResult, error) {
	return telemetry.ServiceResult{Services: []telemetry.Service{{ID: "svc-1", Name: "orders", Instances: []telemetry.ServiceInstance{}, Endpoints: []telemetry.ServiceEndpoint{}}}}, nil
}

func (stubServiceTelemetry) GetService(context.Context, string, string, map[string]any, telemetry.ServiceQuery) (telemetry.Service, error) {
	return telemetry.Service{ID: "svc-1", Name: "orders", Instances: []telemetry.ServiceInstance{}, Endpoints: []telemetry.ServiceEndpoint{}}, nil
}

func (stubServiceTelemetry) GetServiceTopology(context.Context, string, string, map[string]any, telemetry.ServiceQuery) (telemetry.ServiceTopology, error) {
	return telemetry.ServiceTopology{Nodes: []telemetry.ServiceTopologyNode{{ServiceID: "svc-1", Name: "orders"}}, Edges: []telemetry.ServiceTopologyEdge{}}, nil
}

func TestQueryMetricsRequiresPermissionAndUsesEnabledPrometheusSource(t *testing.T) {
	metricBackend := &stubMetricTelemetry{}
	service := &Service{
		dataSources: stubSignalDataSources{items: []domaincopilot.DataSource{
			{ID: "disabled", BackendType: "prometheus", Enabled: false},
			{ID: "metrics", BackendType: "prometheus", Enabled: true, Config: map[string]any{"endpoint": "http://prometheus:9090"}},
		}},
		permissions: monitoringCompatPermissions(appaccess.PermObserveMonitoringView),
		metrics:     metricBackend,
	}
	now := time.Now().UTC()
	result, err := service.QueryMetrics(context.Background(), monitoringCompatPrincipal(), "", telemetry.MetricRangeQuery{
		MetricKey: "cpu_usage", TimeFrom: now.Add(-time.Hour), TimeTo: now, Step: time.Minute,
	})
	if err != nil {
		t.Fatalf("query metrics: %v", err)
	}
	if result.DataSourceID != "metrics" || !metricBackend.called || len(result.Series) != 1 {
		t.Fatalf("unexpected metric result: %#v", result)
	}

	service.permissions = monitoringCompatPermissions()
	_, err = service.QueryMetrics(context.Background(), monitoringCompatPrincipal(), "", telemetry.MetricRangeQuery{
		MetricKey: "cpu_usage", TimeFrom: now.Add(-time.Hour), TimeTo: now, Step: time.Minute,
	})
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("expected access denied, got %v", err)
	}
}

func TestQueryTracesReturnsSortedUniqueServices(t *testing.T) {
	service := &Service{
		dataSources: stubSignalDataSources{items: []domaincopilot.DataSource{{
			ID: "traces", BackendType: "jaeger", Enabled: true, Config: map[string]any{"endpoint": "http://jaeger:16686"},
		}}},
		permissions: monitoringCompatPermissions(appaccess.PermObserveMonitoringView),
		traces:      stubTraceTelemetry{},
	}
	now := time.Now().UTC()
	result, err := service.QueryTraces(context.Background(), monitoringCompatPrincipal(), "", telemetry.TraceQuery{
		TimeFrom: now.Add(-time.Hour), TimeTo: now,
	})
	if err != nil {
		t.Fatalf("query traces: %v", err)
	}
	if len(result.Services) != 2 || result.Services[0] != "api" || result.Services[1] != "worker" {
		t.Fatalf("unexpected services: %#v", result.Services)
	}
}

func TestQueryTracesRejectsScopeNotIsolatedByDataSource(t *testing.T) {
	called := false
	service := &Service{
		dataSources: stubSignalDataSources{items: []domaincopilot.DataSource{{
			ID: "traces", BackendType: "skywalking", Enabled: true,
			Config: map[string]any{"endpoint": "http://oap:12800/graphql"},
			Scope:  map[string]any{"clusterIds": []string{"cluster-a"}, "namespaces": []string{"team-a"}},
		}}},
		permissions: monitoringCompatPermissions(appaccess.PermObserveMonitoringView),
		traces:      stubTraceTelemetry{called: &called},
	}
	now := time.Now().UTC()
	_, err := service.QueryTraces(context.Background(), monitoringCompatPrincipal(), "", telemetry.TraceQuery{
		Scope:    telemetry.TraceScope{ClusterID: "cluster-b", Namespace: "team-a"},
		TimeFrom: now.Add(-time.Hour), TimeTo: now,
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("expected unsupported scope error, got %v", err)
	}
	if called {
		t.Fatal("trace backend must not receive a scope outside the data source boundary")
	}
}

func TestSignalQueriesRejectInvalidRanges(t *testing.T) {
	now := time.Now().UTC()
	if err := validateMetricQuery(telemetry.MetricRangeQuery{
		MetricKey: "custom", TimeFrom: now.Add(-time.Hour), TimeTo: now, Step: time.Minute,
	}); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("expected invalid metric key, got %v", err)
	}
	if _, err := normalizeTraceQuery(telemetry.TraceQuery{
		TimeFrom: now.Add(-8 * 24 * time.Hour), TimeTo: now,
	}); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("expected invalid trace range, got %v", err)
	}
	query, err := normalizeTraceQuery(telemetry.TraceQuery{
		TraceID: " trace-1 ", TimeFrom: now.Add(-time.Hour), TimeTo: now,
	})
	if err != nil || query.TraceID != "trace-1" {
		t.Fatalf("normalized trace query = %#v, %v", query, err)
	}
}

func TestMetricCatalogAndServiceScopeReflectConfiguredBackends(t *testing.T) {
	now := time.Now().UTC()
	service := &Service{
		dataSources: stubSignalDataSources{items: []domaincopilot.DataSource{
			{ID: "metrics", BackendType: "prometheus", Enabled: true, Config: map[string]any{"endpoint": "http://prometheus:9090"}},
			{ID: "oap", BackendType: "skywalking", Enabled: true, Config: map[string]any{"endpoint": "http://oap:12800/graphql"}, Scope: map[string]any{"clusterIds": []string{"cluster-a"}}},
		}},
		permissions: monitoringCompatPermissions(appaccess.PermObserveMonitoringView),
		services:    stubServiceTelemetry{},
	}
	catalog, err := service.ListMetricCatalog(context.Background(), monitoringCompatPrincipal(), "")
	if err != nil || len(catalog) != 5 || !catalog[0].Available {
		t.Fatalf("ListMetricCatalog() = %#v, %v", catalog, err)
	}
	result, err := service.ListServices(context.Background(), monitoringCompatPrincipal(), "", telemetry.ServiceQuery{
		Scope: telemetry.TraceScope{ClusterID: "cluster-a"}, TimeFrom: now.Add(-time.Hour), TimeTo: now,
	})
	if err != nil || len(result.Items) != 1 || result.Meta.State != "success" || len(result.Meta.Warnings) == 0 {
		t.Fatalf("ListServices() = %#v, %v", result, err)
	}
	_, err = service.ListServices(context.Background(), monitoringCompatPrincipal(), "", telemetry.ServiceQuery{
		Scope: telemetry.TraceScope{ClusterID: "cluster-b"}, TimeFrom: now.Add(-time.Hour), TimeTo: now,
	})
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("expected unsupported scope error, got %v", err)
	}
}
