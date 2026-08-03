package monitoring

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	appaccess "github.com/opensoha/soha/internal/application/access"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/telemetry"
)

const maxSignalQueryRange = 7 * 24 * time.Hour

var metricKeys = map[string]struct{}{
	"cpu_usage": {}, "memory_usage": {}, "restart_rate": {}, "error_rate": {}, "latency_p95": {},
}

type MetricQueryResult struct {
	DataSourceID string
	BackendType  string
	Series       []telemetry.MetricSeries
}

type TraceQueryResult struct {
	DataSourceID string
	BackendType  string
	Summary      string
	Services     []string
	Spans        []telemetry.TraceSpan
}

func (s *Service) QueryMetrics(ctx context.Context, principal domainidentity.Principal, dataSourceID string, query telemetry.MetricRangeQuery) (MetricQueryResult, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveMonitoringView); err != nil {
		return MetricQueryResult{}, err
	}
	if err := validateMetricQuery(query); err != nil {
		return MetricQueryResult{}, err
	}
	source, err := s.selectSignalDataSource(ctx, dataSourceID, map[string]struct{}{"prometheus": {}})
	if err != nil {
		return MetricQueryResult{}, err
	}
	series, _, err := s.metricBackend().RangeQuery(ctx, source.BackendType, source.ID, source.Config, query)
	if err != nil {
		return MetricQueryResult{}, err
	}
	return MetricQueryResult{DataSourceID: source.ID, BackendType: source.BackendType, Series: series}, nil
}

func (s *Service) QueryTraces(ctx context.Context, principal domainidentity.Principal, dataSourceID string, query telemetry.TraceQuery) (TraceQueryResult, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveMonitoringView); err != nil {
		return TraceQueryResult{}, err
	}
	query, err := normalizeTraceQuery(query)
	if err != nil {
		return TraceQueryResult{}, err
	}
	source, err := s.selectSignalDataSource(ctx, dataSourceID, map[string]struct{}{"jaeger": {}, "skywalking": {}})
	if err != nil {
		return TraceQueryResult{}, err
	}
	result, err := s.traceBackend().FindSlowSpans(ctx, source.BackendType, source.ID, source.Config, query)
	if err != nil {
		return TraceQueryResult{}, err
	}
	services := make([]string, 0)
	seen := map[string]struct{}{}
	for _, span := range result.Spans {
		service := strings.TrimSpace(span.Service)
		if service == "" {
			continue
		}
		if _, ok := seen[service]; ok {
			continue
		}
		seen[service] = struct{}{}
		services = append(services, service)
	}
	sort.Strings(services)
	return TraceQueryResult{
		DataSourceID: source.ID, BackendType: source.BackendType, Summary: result.Summary,
		Services: services, Spans: result.Spans,
	}, nil
}

func validateMetricQuery(query telemetry.MetricRangeQuery) error {
	if _, ok := metricKeys[strings.TrimSpace(query.MetricKey)]; !ok {
		return fmt.Errorf("%w: unsupported metric key", apperrors.ErrInvalidArgument)
	}
	if err := validateSignalTimeRange(query.TimeFrom, query.TimeTo); err != nil {
		return err
	}
	if query.Step < time.Second || query.Step > time.Hour {
		return fmt.Errorf("%w: metric step must be between 1 and 3600 seconds", apperrors.ErrInvalidArgument)
	}
	return nil
}

func normalizeTraceQuery(query telemetry.TraceQuery) (telemetry.TraceQuery, error) {
	query.TraceID = strings.TrimSpace(query.TraceID)
	if len(query.TraceID) > 128 {
		return telemetry.TraceQuery{}, fmt.Errorf("%w: trace ID is too long", apperrors.ErrInvalidArgument)
	}
	if err := validateSignalTimeRange(query.TimeFrom, query.TimeTo); err != nil {
		return telemetry.TraceQuery{}, err
	}
	if query.MinDuration < 0 || query.MinDuration > time.Hour {
		return telemetry.TraceQuery{}, fmt.Errorf("%w: minimum duration is out of range", apperrors.ErrInvalidArgument)
	}
	if query.Limit == 0 {
		query.Limit = 100
	}
	if query.Limit < 1 || query.Limit > 500 {
		return telemetry.TraceQuery{}, fmt.Errorf("%w: trace limit must be between 1 and 500", apperrors.ErrInvalidArgument)
	}
	return query, nil
}

func validateSignalTimeRange(from, to time.Time) error {
	if from.IsZero() || to.IsZero() || !from.Before(to) {
		return fmt.Errorf("%w: a valid time range is required", apperrors.ErrInvalidArgument)
	}
	if to.Sub(from) > maxSignalQueryRange {
		return fmt.Errorf("%w: time range exceeds seven days", apperrors.ErrInvalidArgument)
	}
	return nil
}

func (s *Service) selectSignalDataSource(ctx context.Context, dataSourceID string, backends map[string]struct{}) (domaincopilot.DataSource, error) {
	if id := strings.TrimSpace(dataSourceID); id != "" {
		source, err := s.dataSources.GetDataSource(ctx, id)
		if err != nil {
			return domaincopilot.DataSource{}, err
		}
		if source.Enabled && supportsSignalBackend(source.BackendType, backends) {
			return source, nil
		}
		return domaincopilot.DataSource{}, fmt.Errorf("%w: telemetry data source not found", apperrors.ErrNotFound)
	}
	items, err := s.dataSources.ListDataSources(ctx)
	if err != nil {
		return domaincopilot.DataSource{}, err
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	for _, source := range items {
		if source.Enabled && supportsSignalBackend(source.BackendType, backends) {
			return source, nil
		}
	}
	return domaincopilot.DataSource{}, fmt.Errorf("%w: no enabled telemetry data source", apperrors.ErrNotFound)
}

func supportsSignalBackend(backend string, allowed map[string]struct{}) bool {
	_, ok := allowed[strings.ToLower(strings.TrimSpace(backend))]
	return ok
}
