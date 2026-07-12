package copilot

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/opensoha/soha/internal/platform/telemetry"
)

var errCopilotTelemetry = errors.New("copilot telemetry failure")

type copilotTelemetryStub struct {
	context context.Context
	query   any
}

type copilotContextKey struct{}

func (*copilotTelemetryStub) Validate(string, map[string]any) error {
	return errCopilotTelemetry
}

func (s *copilotTelemetryStub) Correlate(ctx context.Context, _ string, _ string, _ map[string]any, query telemetry.LogCorrelationQuery) (telemetry.LogCorrelationResult, error) {
	s.context = ctx
	s.query = query
	return telemetry.LogCorrelationResult{Summary: "logs"}, errCopilotTelemetry
}

func (s *copilotTelemetryStub) Analyze(ctx context.Context, _ string, _ string, _ map[string]any, query telemetry.MetricRangeQuery) (telemetry.MetricAnomalySummary, error) {
	s.context = ctx
	s.query = query
	return telemetry.MetricAnomalySummary{Summary: "metrics"}, errCopilotTelemetry
}

func (s *copilotTelemetryStub) FindSlowSpans(ctx context.Context, _ string, _ string, _ map[string]any, query telemetry.TraceQuery) (telemetry.TraceResult, error) {
	s.context = ctx
	s.query = query
	return telemetry.TraceResult{Summary: "traces"}, errCopilotTelemetry
}

func TestNewUsesExplicitUnavailableTelemetryDefaults(t *testing.T) {
	service := newTestService(&inspectionAuthzTestRepository{})

	if err := service.logBackend().Validate("loki", nil); err == nil {
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
			backend := &copilotTelemetryStub{}
			service := newTestService(&inspectionAuthzTestRepository{}, WithTelemetryBackends(backend, backend, backend))
			ctx := context.WithValue(t.Context(), copilotContextKey{}, test.name)

			summary, err := test.call(service, ctx)
			if !errors.Is(err, errCopilotTelemetry) {
				t.Fatalf("error = %v, want %v", err, errCopilotTelemetry)
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

func TestNewRejectsMissingRepositoryDependency(t *testing.T) {
	t.Parallel()

	_, err := New(Dependencies{})
	if err == nil || !strings.Contains(err.Error(), "sessions dependency is required") {
		t.Fatalf("New() error = %v, want missing sessions error", err)
	}
}

func TestNewRejectsTypedNilRepositoryDependency(t *testing.T) {
	t.Parallel()

	repo := &inspectionAuthzTestRepository{}
	deps := newTestDependencies(repo)
	var messages *inspectionAuthzTestRepository
	deps.Messages = messages
	_, err := New(deps)
	if err == nil || !strings.Contains(err.Error(), "messages dependency is required") {
		t.Fatalf("New() error = %v, want typed nil messages error", err)
	}
}
