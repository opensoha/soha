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
}

func (s *stubMetricTelemetry) RangeQuery(context.Context, string, string, map[string]any, telemetry.MetricRangeQuery) ([]telemetry.MetricSeries, map[string]any, error) {
	s.called = true
	return []telemetry.MetricSeries{{Key: "cpu_usage", Label: "CPU", Latest: 0.5}}, nil, nil
}

func (s *stubMetricTelemetry) Analyze(context.Context, string, string, map[string]any, telemetry.MetricRangeQuery) (telemetry.MetricAnomalySummary, error) {
	return telemetry.MetricAnomalySummary{}, nil
}

type stubTraceTelemetry struct{}

func (stubTraceTelemetry) FindSlowSpans(context.Context, string, string, map[string]any, telemetry.TraceQuery) (telemetry.TraceResult, error) {
	return telemetry.TraceResult{
		Summary: "2 spans",
		Spans: []telemetry.TraceSpan{
			{TraceID: "trace-1", SpanID: "span-1", Service: "worker"},
			{TraceID: "trace-1", SpanID: "span-2", Service: "api"},
			{TraceID: "trace-2", SpanID: "span-3", Service: "worker"},
		},
	}, nil
}

func TestQueryMetricsRequiresPermissionAndUsesEnabledPrometheusSource(t *testing.T) {
	metricBackend := &stubMetricTelemetry{}
	service := &Service{
		dataSources: stubSignalDataSources{items: []domaincopilot.DataSource{
			{ID: "disabled", BackendType: "prometheus", Enabled: false},
			{ID: "metrics", BackendType: "prometheus", Enabled: true},
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
			ID: "traces", BackendType: "jaeger", Enabled: true,
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
