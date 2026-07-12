package monitoring

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/telemetry"
)

var errMonitoringTelemetry = errors.New("monitoring telemetry failure")

type monitoringContextKey struct{}

type monitoringTelemetryStub struct {
	context context.Context
	query   any
}

type monitoringDataSourceStub struct{}

func (*monitoringDataSourceStub) ListDataSources(context.Context) ([]domaincopilot.DataSource, error) {
	return nil, nil
}

func (*monitoringDataSourceStub) GetDataSource(context.Context, string) (domaincopilot.DataSource, error) {
	return domaincopilot.DataSource{}, nil
}

func validMonitoringDependencies() Dependencies {
	repo := &stubMonitoringCompatRepository{}
	return Dependencies{
		AlertReader:           repo,
		AlertWriter:           repo,
		Channels:              repo,
		Silences:              repo,
		DeliveryLogs:          repo,
		Rules:                 repo,
		RuleRuns:              repo,
		AlertEvents:           repo,
		NotificationPolicies:  repo,
		NotificationTemplates: repo,
		HealingPolicies:       repo,
		HealingRuns:           repo,
		OnCallSchedules:       repo,
		OnCallRotations:       repo,
		OnCallEscalations:     repo,
		OnCallAssignments:     repo,
		Integrations:          repo,
		Events:                &stubMonitoringEventWriter{},
		DataSources:           &monitoringDataSourceStub{},
		Permissions:           monitoringCompatPermissions(),
		Enabled:               true,
	}
}

func TestNewRejectsMissingDependency(t *testing.T) {
	service, err := New(Dependencies{})
	if service != nil {
		t.Fatalf("service = %#v, want nil", service)
	}
	if !errors.Is(err, apperrors.ErrInvalidArgument) || !strings.Contains(err.Error(), "alert reader dependency is required") {
		t.Fatalf("New() error = %v, want missing alert reader error", err)
	}
}

func TestNewRejectsTypedNilDependency(t *testing.T) {
	deps := validMonitoringDependencies()
	var channels *stubMonitoringCompatRepository
	deps.Channels = channels

	service, err := New(deps)
	if service != nil {
		t.Fatalf("service = %#v, want nil", service)
	}
	if !errors.Is(err, apperrors.ErrInvalidArgument) || !strings.Contains(err.Error(), "channels dependency is required") {
		t.Fatalf("New() error = %v, want typed nil channels error", err)
	}
}

func (s *monitoringTelemetryStub) Correlate(ctx context.Context, _ string, _ string, _ map[string]any, query telemetry.LogCorrelationQuery) (telemetry.LogCorrelationResult, error) {
	s.context = ctx
	s.query = query
	return telemetry.LogCorrelationResult{Summary: "logs"}, errMonitoringTelemetry
}

func (s *monitoringTelemetryStub) Analyze(ctx context.Context, _ string, _ string, _ map[string]any, query telemetry.MetricRangeQuery) (telemetry.MetricAnomalySummary, error) {
	s.context = ctx
	s.query = query
	return telemetry.MetricAnomalySummary{Summary: "metrics"}, errMonitoringTelemetry
}

func (s *monitoringTelemetryStub) FindSlowSpans(ctx context.Context, _ string, _ string, _ map[string]any, query telemetry.TraceQuery) (telemetry.TraceResult, error) {
	s.context = ctx
	s.query = query
	return telemetry.TraceResult{Summary: "traces"}, errMonitoringTelemetry
}

func TestNewUsesExplicitUnavailableTelemetryDefaults(t *testing.T) {
	service, err := New(validMonitoringDependencies())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if _, err = service.logBackend().Correlate(t.Context(), "", "", nil, telemetry.LogCorrelationQuery{}); err == nil {
		t.Fatal("log backend returned nil error without a configured dependency")
	}
	if _, err := service.metricBackend().Analyze(t.Context(), "", "", nil, telemetry.MetricRangeQuery{}); err == nil {
		t.Fatal("metric backend returned nil error without a configured dependency")
	}
	if _, err := service.traceBackend().FindSlowSpans(t.Context(), "", "", nil, telemetry.TraceQuery{}); err == nil {
		t.Fatal("trace backend returned nil error without a configured dependency")
	}
}

func TestWithTelemetryBackendsPreservesContextDTOResultAndError(t *testing.T) {
	tests := []struct {
		name  string
		query any
		call  func(*Service, context.Context) (string, error)
	}{
		{
			name:  "logs",
			query: telemetry.LogCorrelationQuery{AlertID: "alert-1", Limit: 7},
			call: func(service *Service, ctx context.Context) (string, error) {
				result, err := service.logBackend().Correlate(ctx, "loki", "source-1", map[string]any{"tenant": "a"}, telemetry.LogCorrelationQuery{AlertID: "alert-1", Limit: 7})
				return result.Summary, err
			},
		},
		{
			name:  "metrics",
			query: telemetry.MetricRangeQuery{MetricKey: "cpu"},
			call: func(service *Service, ctx context.Context) (string, error) {
				result, err := service.metricBackend().Analyze(ctx, "prometheus", "source-2", map[string]any{"tenant": "a"}, telemetry.MetricRangeQuery{MetricKey: "cpu"})
				return result.Summary, err
			},
		},
		{
			name:  "traces",
			query: telemetry.TraceQuery{Limit: 9},
			call: func(service *Service, ctx context.Context) (string, error) {
				result, err := service.traceBackend().FindSlowSpans(ctx, "tempo", "source-3", map[string]any{"tenant": "a"}, telemetry.TraceQuery{Limit: 9})
				return result.Summary, err
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			backend := &monitoringTelemetryStub{}
			service, newErr := New(validMonitoringDependencies(), WithTelemetryBackends(backend, backend, backend))
			if newErr != nil {
				t.Fatalf("New() error = %v", newErr)
			}
			ctx := context.WithValue(t.Context(), monitoringContextKey{}, test.name)

			summary, err := test.call(service, ctx)
			if !errors.Is(err, errMonitoringTelemetry) {
				t.Fatalf("error = %v, want %v", err, errMonitoringTelemetry)
			}
			if summary != test.name {
				t.Fatalf("summary = %q, want %q", summary, test.name)
			}
			if backend.context != ctx {
				t.Fatal("backend received a different context")
			}
			if !reflect.DeepEqual(backend.query, test.query) {
				t.Fatalf("query = %#v, want %#v", backend.query, test.query)
			}
		})
	}
}
