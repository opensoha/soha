package monitoring

import (
	"context"
	"errors"
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
	Meta         *QueryMeta
}

type TraceQueryResult struct {
	DataSourceID string
	BackendType  string
	Summary      string
	Services     []string
	Spans        []telemetry.TraceSpan
}

type MetricDefinition struct {
	Key, Label, Description, Unit, SemanticConvention string
	Available                                         bool
}

type QueryMeta struct {
	State           string
	Warnings        []string
	ObservedAt      time.Time
	ScopeRestricted bool
	Snapshot        map[string]any
}

type ServiceListResult struct {
	DataSourceID string
	BackendType  string
	Items        []telemetry.Service
	Scope        telemetry.TraceScope
	Environment  string
	ClusterIDs   []string
	Meta         QueryMeta
}

type ServiceDetailResult struct {
	DataSourceID string
	BackendType  string
	Item         telemetry.Service
	Scope        telemetry.TraceScope
	Environment  string
	ClusterIDs   []string
	Meta         QueryMeta
}

type ServiceTopologyResult struct {
	DataSourceID string
	BackendType  string
	Topology     telemetry.ServiceTopology
	Meta         QueryMeta
}

var metricCatalog = []MetricDefinition{
	{Key: "cpu_usage", Label: "CPU Usage", Description: "Container CPU usage rate from Prometheus", Unit: "cores"},
	{Key: "memory_usage", Label: "Memory Usage", Description: "Container working set memory from Prometheus", Unit: "bytes"},
	{Key: "restart_rate", Label: "Restart Rate", Description: "Container restarts in the query window", Unit: "count"},
	{Key: "error_rate", Label: "Error Rate", Description: "HTTP request error signal from Prometheus", Unit: "ratio"},
	{Key: "latency_p95", Label: "Latency P95", Description: "P95 HTTP server request duration", Unit: "seconds"},
}

func (s *Service) ListMetricCatalog(ctx context.Context, principal domainidentity.Principal, dataSourceID string) ([]MetricDefinition, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveMonitoringView); err != nil {
		return nil, err
	}
	available := false
	_, err := s.selectSignalDataSource(ctx, dataSourceID, map[string]struct{}{"prometheus": {}})
	if err == nil {
		available = true
	} else if strings.TrimSpace(dataSourceID) != "" || !errors.Is(err, apperrors.ErrNotFound) {
		return nil, err
	}
	items := append([]MetricDefinition(nil), metricCatalog...)
	for index := range items {
		items[index].Available = available
	}
	return items, nil
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
	if !serviceScopeSupported(source.Scope, "clusterIds", query.Scope.ClusterID) ||
		!serviceScopeSupported(source.Scope, "namespaces", query.Scope.Namespace) {
		return TraceQueryResult{}, fmt.Errorf("%w: trace query scope is not isolated by the data source", apperrors.ErrInvalidArgument)
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

func (s *Service) ListServices(ctx context.Context, principal domainidentity.Principal, dataSourceID string, query telemetry.ServiceQuery) (ServiceListResult, error) {
	source, query, warnings, err := s.prepareServiceQuery(ctx, principal, dataSourceID, query)
	if err != nil {
		return ServiceListResult{}, err
	}
	result, err := s.serviceBackend().ListServices(ctx, source.BackendType, source.ID, source.Config, query)
	if err != nil {
		return ServiceListResult{}, fmt.Errorf("%w: SkyWalking service query failed", apperrors.ErrClusterUnready)
	}
	return ServiceListResult{
		DataSourceID: source.ID, BackendType: source.BackendType, Items: result.Services,
		Scope: query.Scope, Environment: query.Environment, ClusterIDs: serviceClusterIDs(source, query.Scope.ClusterID),
		Meta: serviceQueryMeta(len(result.Services), warnings),
	}, nil
}

func (s *Service) GetService(ctx context.Context, principal domainidentity.Principal, dataSourceID string, query telemetry.ServiceQuery) (ServiceDetailResult, error) {
	source, query, warnings, err := s.prepareServiceQuery(ctx, principal, dataSourceID, query)
	if err != nil {
		return ServiceDetailResult{}, err
	}
	item, err := s.serviceBackend().GetService(ctx, source.BackendType, source.ID, source.Config, query)
	if err != nil {
		return ServiceDetailResult{}, fmt.Errorf("%w: SkyWalking service query failed", apperrors.ErrClusterUnready)
	}
	if strings.TrimSpace(item.ID) == "" {
		return ServiceDetailResult{}, fmt.Errorf("%w: observability service not found", apperrors.ErrNotFound)
	}
	return ServiceDetailResult{
		DataSourceID: source.ID, BackendType: source.BackendType, Item: item,
		Scope: query.Scope, Environment: query.Environment, ClusterIDs: serviceClusterIDs(source, query.Scope.ClusterID),
		Meta: serviceQueryMeta(1, warnings),
	}, nil
}

func (s *Service) GetServiceTopology(ctx context.Context, principal domainidentity.Principal, dataSourceID string, query telemetry.ServiceQuery) (ServiceTopologyResult, error) {
	source, query, warnings, err := s.prepareServiceQuery(ctx, principal, dataSourceID, query)
	if err != nil {
		return ServiceTopologyResult{}, err
	}
	topology, err := s.serviceBackend().GetServiceTopology(ctx, source.BackendType, source.ID, source.Config, query)
	if err != nil {
		return ServiceTopologyResult{}, fmt.Errorf("%w: SkyWalking topology query failed", apperrors.ErrClusterUnready)
	}
	return ServiceTopologyResult{
		DataSourceID: source.ID, BackendType: source.BackendType, Topology: topology,
		Meta: serviceQueryMeta(len(topology.Nodes), warnings),
	}, nil
}

func (s *Service) prepareServiceQuery(ctx context.Context, principal domainidentity.Principal, dataSourceID string, query telemetry.ServiceQuery) (domaincopilot.DataSource, telemetry.ServiceQuery, []string, error) {
	if err := s.authorize(ctx, principal, appaccess.PermObserveMonitoringView); err != nil {
		return domaincopilot.DataSource{}, telemetry.ServiceQuery{}, nil, err
	}
	query.ServiceID = strings.TrimSpace(query.ServiceID)
	query.ServiceName = strings.TrimSpace(query.ServiceName)
	query.Environment = strings.TrimSpace(query.Environment)
	if err := validateSignalTimeRange(query.TimeFrom, query.TimeTo); err != nil {
		return domaincopilot.DataSource{}, telemetry.ServiceQuery{}, nil, err
	}
	if len(query.ServiceID) > 253 || len(query.ServiceName) > 253 || len(query.Environment) > 128 {
		return domaincopilot.DataSource{}, telemetry.ServiceQuery{}, nil, fmt.Errorf("%w: service query is too long", apperrors.ErrInvalidArgument)
	}
	source, err := s.selectSignalDataSource(ctx, dataSourceID, map[string]struct{}{"skywalking": {}})
	if err != nil {
		return domaincopilot.DataSource{}, telemetry.ServiceQuery{}, nil, err
	}
	if !serviceScopeSupported(source.Scope, "clusterIds", query.Scope.ClusterID) || !serviceScopeSupported(source.Scope, "namespaces", query.Scope.Namespace) {
		return domaincopilot.DataSource{}, telemetry.ServiceQuery{}, nil, fmt.Errorf("%w: SkyWalking data source cannot isolate the requested scope", apperrors.ErrInvalidArgument)
	}
	if query.Environment != "" {
		return domaincopilot.DataSource{}, telemetry.ServiceQuery{}, nil, fmt.Errorf("%w: SkyWalking Metadata V2 cannot isolate environment scope", apperrors.ErrInvalidArgument)
	}
	warnings := []string{"SkyWalking Metadata V2 provides identity only; RED health is unavailable"}
	return source, query, warnings, nil
}

func serviceQueryMeta(count int, warnings []string) QueryMeta {
	state := "success"
	if count == 0 {
		state = "empty"
	}
	return QueryMeta{State: state, Warnings: warnings, ObservedAt: time.Now().UTC()}
}

func serviceClusterIDs(source domaincopilot.DataSource, requested string) []string {
	declared := signalScopeValues(source.Scope["clusterIds"])
	if requested = strings.TrimSpace(requested); requested == "" {
		return declared
	}
	if len(declared) == 1 && declared[0] == requested {
		return []string{requested}
	}
	return []string{}
}

func serviceScopeSupported(scope map[string]any, key, requested string) bool {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		return true
	}
	values := signalScopeValues(scope[key])
	return len(values) == 1 && values[0] == requested
}

func signalScopeValues(value any) []string {
	items := []string{}
	switch values := value.(type) {
	case []string:
		items = values
	case []any:
		for _, item := range values {
			items = append(items, fmt.Sprint(item))
		}
	}
	result := make([]string, 0, len(items))
	for _, item := range items {
		if item = strings.TrimSpace(item); item != "" {
			result = append(result, item)
		}
	}
	return result
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
		if signalDataSourceReady(source, backends) {
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
		if signalDataSourceReady(source, backends) {
			return source, nil
		}
	}
	return domaincopilot.DataSource{}, fmt.Errorf("%w: no enabled telemetry data source", apperrors.ErrNotFound)
}

func signalDataSourceReady(source domaincopilot.DataSource, allowed map[string]struct{}) bool {
	if !source.Enabled || !supportsSignalBackend(source.BackendType, allowed) {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(source.BackendType)) {
	case "prometheus", "jaeger", "skywalking":
		endpoint, _ := source.Config["endpoint"].(string)
		return strings.TrimSpace(endpoint) != ""
	}
	return true
}

func supportsSignalBackend(backend string, allowed map[string]struct{}) bool {
	_, ok := allowed[strings.ToLower(strings.TrimSpace(backend))]
	return ok
}
