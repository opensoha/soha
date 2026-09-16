package monitoring

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/telemetry"
)

func metricWindowFixture(now time.Time) (sohaapi.ObservabilityMetricAssessmentInput, telemetry.MetricSeries) {
	end := now.Truncate(time.Second).Add(-time.Second)
	input := sohaapi.ObservabilityMetricAssessmentInput{DataSourceID: "metrics", MetricKey: "error_rate", Unit: "ratio", WindowStart: end.Add(-time.Minute), WindowEnd: end, StepSeconds: 30, MaxAgeSeconds: 120}
	input.Scope.ClusterID, input.Scope.Namespace, input.Scope.Service = "cluster-a", "app", "api"
	input.Threshold.Operator, input.Threshold.Value = "lte", 0.01
	series := telemetry.MetricSeries{Key: "error_rate", Unit: "ratio", Lookback: 5 * time.Minute}
	for at := input.WindowStart; !at.After(end); at = at.Add(30 * time.Second) {
		series.Points = append(series.Points, telemetry.MetricPoint{Timestamp: at, Value: 0.001})
		series.SourceTimes = append(series.SourceTimes, telemetry.MetricPoint{Timestamp: at, Value: float64(at.Add(-10 * time.Second).Unix())})
	}
	return input, series
}

func TestMetricWindowRequiresSourceFreshnessCoverageUnitsAndPostReleaseLookback(t *testing.T) {
	now := time.Now().UTC()
	for _, mode := range []string{"satisfied", "breach", "stale-source", "stale-window", "future-window", "gap", "no-source", "nan", "duplicate", "unit", "pre-release", "post-release", "invalid-ratio", "gauge-pre-release-sample"} {
		t.Run(mode, func(t *testing.T) {
			input, series := metricWindowFixture(now)
			_, threshold, err := metricAssessmentQuery(&input)
			if err != nil {
				t.Fatal(err)
			}
			want := alterMetricWindowScenario(mode, &input, &series, now)
			verdict, through := assessMetricWindow(input, series, threshold, now)
			if verdict != want || verdict == "satisfied" && (through == nil || !through.Equal(input.WindowEnd.Add(-10*time.Second))) {
				t.Fatalf("%s: %s through=%v", mode, verdict, through)
			}
		})
	}
}

func alterMetricWindowScenario(mode string, input *sohaapi.ObservabilityMetricAssessmentInput, series *telemetry.MetricSeries, now time.Time) sohaapi.CapabilityAssessmentVerdict {
	want := sohaapi.CapabilityAssessmentVerdict("inconclusive")
	switch mode {
	case "satisfied":
		want = "satisfied"
	case "breach":
		series.Points[1].Value = 0.03
		want = "unsatisfied"
	case "stale-source":
		series.SourceTimes[1].Value -= 300
	case "stale-window":
		input.WindowEnd = now.Add(-time.Hour)
	case "future-window":
		input.WindowEnd = now.Add(time.Hour)
	case "gap":
		series.Points = append(series.Points[:1], series.Points[2:]...)
	case "no-source":
		series.SourceTimes = nil
	case "nan":
		series.Points[1].Value = math.NaN()
	case "duplicate":
		series.Points[1] = series.Points[0]
	case "unit":
		series.Unit = "seconds"
	case "pre-release":
		deployed := input.WindowStart
		input.NotBefore = &deployed
	case "post-release":
		deployed := input.WindowStart.Add(-5 * time.Minute)
		input.NotBefore = &deployed
		want = "satisfied"
	case "invalid-ratio":
		series.Points[1].Value = 1.5
	case "gauge-pre-release-sample":
		input.MetricKey, input.Unit, series.Key, series.Unit, series.Lookback = "memory_usage", "bytes", "memory_usage", "bytes", 0
		deployed := input.WindowStart
		input.NotBefore = &deployed
	}
	return want
}

func TestMetricAssessmentRejectsWindowWithAnUncoveredTail(t *testing.T) {
	input, _ := metricWindowFixture(time.Now().UTC())
	input.WindowEnd = input.WindowEnd.Add(5 * time.Second)
	if _, _, err := metricAssessmentQuery(&input); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatal("non-aligned window accepted without tail evidence")
	}
}

type assessmentMetricBackend struct {
	stubMetricTelemetry
	series []telemetry.MetricSeries
	err    error
}

func (s *assessmentMetricBackend) RangeQuery(_ context.Context, _, _ string, _ map[string]any, query telemetry.MetricRangeQuery) ([]telemetry.MetricSeries, map[string]any, error) {
	s.called, s.query = true, query
	return s.series, nil, s.err
}

func TestMetricAssessmentKeepsAuthorizationAndMissingDataInconclusive(t *testing.T) {
	input, series := metricWindowFixture(time.Now().UTC())
	backend := &assessmentMetricBackend{series: []telemetry.MetricSeries{series}}
	service := &Service{dataSources: stubSignalDataSources{items: []domaincopilot.DataSource{{ID: "metrics", BackendType: "prometheus", Enabled: true, Config: map[string]any{"endpoint": "https://metrics.invalid"}, Scope: map[string]any{"clusterIds": []string{"cluster-a"}, "namespaces": []string{"app"}}}}}, permissions: monitoringCompatPermissions(appaccess.PermObserveMonitoringView), metrics: backend}
	result, err := service.AssessMetrics(context.Background(), monitoringCompatPrincipal(), input)
	if err != nil || result.Verdict != "satisfied" || !backend.query.RequireEvidence || result.Evidence[0].Reference.Input["dataSourceId"] != "metrics" {
		t.Fatalf("authorized evidence failed: %+v %v", result, err)
	}
	backend.called = false
	service.permissions = monitoringCompatPermissions()
	if _, err := service.AssessMetrics(context.Background(), monitoringCompatPrincipal(), input); !errors.Is(err, apperrors.ErrAccessDenied) || backend.called {
		t.Fatal("query bypassed permission")
	}
	service.permissions = monitoringCompatPermissions(appaccess.PermObserveMonitoringView)
	input.Scope.ClusterID = "other"
	if _, err := service.AssessMetrics(context.Background(), monitoringCompatPrincipal(), input); !errors.Is(err, apperrors.ErrInvalidArgument) || backend.called {
		t.Fatal("query escaped data source scope")
	}
	input.Scope.ClusterID = "cluster-a"
	for _, backendError := range []error{nil, errors.New("private backend diagnostic")} {
		backend.series, backend.err = nil, backendError
		result, err := service.AssessMetrics(context.Background(), monitoringCompatPrincipal(), input)
		if err != nil || result.Verdict != "inconclusive" || !result.Evidence[0].Incomplete {
			t.Fatalf("missing data marked healthy: %+v %v", result, err)
		}
	}
	input.Unit = "bytes"
	if _, _, err := metricAssessmentQuery(&input); !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatal("threshold dimension mismatch accepted")
	}
}
