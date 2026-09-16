package monitoring

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/opensoha/soha-contracts/gen/go/sohaapi"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/telemetry"
)

func (s *Service) AssessMetrics(ctx context.Context, principal domainidentity.Principal, input sohaapi.ObservabilityMetricAssessmentInput) (sohaapi.CapabilityAssessment, error) {
	result := sohaapi.CapabilityAssessment{Verdict: "inconclusive", Summary: "a complete fresh metric window is required", Evidence: []sohaapi.CapabilityEvidence{}}
	query, threshold, err := metricAssessmentQuery(&input)
	if err != nil {
		return result, err
	}
	now := time.Now().UTC()
	scope := metricAssessmentScope(input)
	raw, _ := json.Marshal(input)
	var frozenInput map[string]any
	_ = json.Unmarshal(raw, &frozenInput)
	evidence := sohaapi.CapabilityEvidence{Kind: "metric_window", Source: "observability.prometheus", ObservedAt: now, Incomplete: true, Summary: result.Summary,
		Resource:  &sohaapi.CapabilityResourceRef{Kind: "observability.data_source", ID: input.DataSourceID, Scope: scope},
		Reference: &sohaapi.CapabilityCall{ToolName: "observability.metrics.assess", CapabilityVersion: "1", Input: frozenInput}}
	observed, err := s.QueryMetrics(ctx, principal, input.DataSourceID, query)
	if err != nil {
		if errors.Is(err, apperrors.ErrAccessDenied) || errors.Is(err, apperrors.ErrInvalidArgument) {
			return result, err
		}
		result.Evidence = append(result.Evidence, evidence)
		return result, nil
	}
	if len(observed.Series) == 1 && observed.DataSourceID == input.DataSourceID {
		result.Verdict, evidence.DataThrough = assessMetricWindow(input, observed.Series[0], threshold, now)
		evidence.Incomplete = result.Verdict == "inconclusive"
	}
	result.Summary = fmt.Sprintf("%s condition is %s over %s to %s; unit %s", input.MetricKey, result.Verdict, input.WindowStart.Format(time.RFC3339), input.WindowEnd.Format(time.RFC3339), input.Unit)
	evidence.Summary = result.Summary
	result.Evidence = append(result.Evidence, evidence)
	return result, nil
}

func metricAssessmentScope(input sohaapi.ObservabilityMetricAssessmentInput) map[string]string {
	result := map[string]string{"dataSourceId": input.DataSourceID}
	for key, value := range map[string]string{"clusterId": input.Scope.ClusterID, "namespace": input.Scope.Namespace, "workload": input.Scope.Workload, "service": input.Scope.Service} {
		if value != "" {
			result[key] = value
		}
	}
	return result
}

func metricAssessmentQuery(input *sohaapi.ObservabilityMetricAssessmentInput) (telemetry.MetricRangeQuery, metricThreshold, error) {
	if input.StepSeconds == 0 {
		input.StepSeconds = 30
	}
	if input.MaxAgeSeconds == 0 {
		input.MaxAgeSeconds = 120
	}
	query := telemetry.MetricRangeQuery{RequireEvidence: true, MetricKey: string(input.MetricKey), Scope: telemetry.MetricScope{ClusterID: input.Scope.ClusterID, Namespace: input.Scope.Namespace, Workload: input.Scope.Workload, Service: input.Scope.Service}, TimeFrom: input.WindowStart.Truncate(time.Second), TimeTo: input.WindowEnd.Truncate(time.Second), Step: time.Duration(input.StepSeconds) * time.Second}
	threshold, err := parseMetricThreshold(map[string]any{"operator": string(input.Threshold.Operator), "value": float64(input.Threshold.Value)})
	if err != nil {
		return query, threshold, err
	}
	if strings.TrimSpace(input.DataSourceID) == "" || len(metricAssessmentScope(*input)) < 2 || input.StepSeconds < 10 || input.StepSeconds > 300 || input.MaxAgeSeconds < 10 || input.MaxAgeSeconds > 600 || query.TimeTo.Sub(query.TimeFrom) < query.Step || query.TimeTo.Sub(query.TimeFrom) > time.Hour || query.TimeTo.Sub(query.TimeFrom)%query.Step != 0 || !threshold.configured || math.IsNaN(threshold.value) || math.IsInf(threshold.value, 0) {
		return query, threshold, apperrors.ErrInvalidArgument
	}
	for _, definition := range metricCatalog {
		if definition.Key == query.MetricKey && definition.Unit == string(input.Unit) {
			return query, threshold, validateMetricQuery(query)
		}
	}
	return query, threshold, fmt.Errorf("%w: threshold unit must match the registered metric", apperrors.ErrInvalidArgument)
}

func assessMetricWindow(input sohaapi.ObservabilityMetricAssessmentInput, series telemetry.MetricSeries, threshold metricThreshold, now time.Time) (sohaapi.CapabilityAssessmentVerdict, *time.Time) {
	from, to := input.WindowStart.Truncate(time.Second), input.WindowEnd.Truncate(time.Second)
	maxAge := time.Duration(input.MaxAgeSeconds) * time.Second
	if series.Key != string(input.MetricKey) || series.Unit != string(input.Unit) || now.Sub(to) > maxAge || to.After(now) || input.NotBefore != nil && from.Add(-series.Lookback).Before(*input.NotBefore) {
		return "inconclusive", nil
	}
	points, ok := uniqueMetricPoints(series.Points)
	if !ok {
		return "inconclusive", nil
	}
	sources, ok := uniqueMetricPoints(series.SourceTimes)
	if !ok {
		return "inconclusive", nil
	}
	verdict := sohaapi.CapabilityAssessmentVerdict("satisfied")
	var through time.Time
	for at := from; !at.After(to); at = at.Add(time.Duration(input.StepSeconds) * time.Second) {
		value, found := points[at.Unix()]
		sampleTimestamp, hasSource := sources[at.Unix()]
		if !found || !hasSource {
			return "inconclusive", nil
		}
		if !validMetricAssessmentValue(input.Unit, value) {
			return "inconclusive", nil
		}
		sampleTime := time.Unix(int64(sampleTimestamp), 0).UTC()
		if !validMetricSourceTime(sampleTime, at, maxAge, input.NotBefore) {
			return "inconclusive", &sampleTime
		}
		through = sampleTime
		matched, evaluated := metricSignalMatches(map[string]any{"latest": value}, threshold)
		if !evaluated {
			return "inconclusive", &through
		}
		if !matched {
			verdict = "unsatisfied"
		}
	}
	if now.Sub(through) > maxAge {
		return "inconclusive", &through
	}
	return verdict, &through
}

func validMetricSourceTime(sample, at time.Time, maxAge time.Duration, notBefore *time.Time) bool {
	return !sample.IsZero() && !sample.After(at.Add(5*time.Second)) && at.Sub(sample) <= maxAge && (notBefore == nil || !sample.Before(*notBefore))
}

func validMetricAssessmentValue(unit sohaapi.ObservabilityMetricAssessmentInputUnit, value float64) bool {
	return value >= 0 && (unit != "ratio" || value <= 1)
}

func uniqueMetricPoints(points []telemetry.MetricPoint) (map[int64]float64, bool) {
	result := make(map[int64]float64, len(points))
	for _, point := range points {
		if math.IsNaN(point.Value) || math.IsInf(point.Value, 0) {
			return nil, false
		}
		if _, duplicate := result[point.Timestamp.Unix()]; duplicate {
			return nil, false
		}
		result[point.Timestamp.Unix()] = point.Value
	}
	return result, true
}
