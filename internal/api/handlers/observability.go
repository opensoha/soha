package handlers

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
	apiMiddleware "github.com/opensoha/soha/internal/api/middleware"
	apiresponse "github.com/opensoha/soha/internal/api/response"
	appmonitoring "github.com/opensoha/soha/internal/application/monitoring"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/telemetry"
)

type ObservabilityDataSources interface {
	ListProviders(context.Context, domainidentity.Principal) ([]sohaapi.ObservabilityProviderDefinition, error)
	ListDataSources(context.Context, domainidentity.Principal) ([]sohaapi.ObservabilityDataSource, error)
	CreateDataSource(context.Context, domainidentity.Principal, sohaapi.ObservabilityDataSourceInput) (sohaapi.ObservabilityDataSource, error)
	UpdateDataSource(context.Context, domainidentity.Principal, string, sohaapi.ObservabilityDataSourceInput) (sohaapi.ObservabilityDataSource, error)
	ValidateDataSource(context.Context, domainidentity.Principal, string) (sohaapi.ObservabilityDataSource, error)
}

type ObservabilityLogCollection interface {
	GetLogCollection(context.Context, domainidentity.Principal, string) (sohaapi.LogCollectionState, error)
	PreflightLogCollection(context.Context, domainidentity.Principal, string, sohaapi.LogCollectionPreflightInput) (sohaapi.LogCollectionPlan, error)
	EnableLogCollection(context.Context, domainidentity.Principal, string, sohaapi.LogCollectionEnableInput) (sohaapi.LogCollectionState, error)
	DisableLogCollection(context.Context, domainidentity.Principal, string, sohaapi.LogCollectionDisableInput) (sohaapi.LogCollectionState, error)
}

type ObservabilitySignalQueries interface {
	QueryMetrics(context.Context, domainidentity.Principal, string, telemetry.MetricRangeQuery) (appmonitoring.MetricQueryResult, error)
	QueryTraces(context.Context, domainidentity.Principal, string, telemetry.TraceQuery) (appmonitoring.TraceQueryResult, error)
}

type ObservabilityHandler struct {
	dataSources ObservabilityDataSources
	collection  ObservabilityLogCollection
	signals     ObservabilitySignalQueries
}

func NewObservabilityHandler(dataSources ObservabilityDataSources, signals ...ObservabilitySignalQueries) *ObservabilityHandler {
	collection, _ := dataSources.(ObservabilityLogCollection)
	handler := &ObservabilityHandler{dataSources: dataSources, collection: collection}
	if len(signals) > 0 {
		handler.signals = signals[0]
	}
	return handler
}

func (h *ObservabilityHandler) GetLogCollection(c *gin.Context) {
	item, err := h.collection.GetLogCollection(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ObservabilityHandler) PreflightLogCollection(c *gin.Context) {
	var input sohaapi.LogCollectionPreflightInput
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid log collection preflight payload")
		return
	}
	item, err := h.collection.PreflightLogCollection(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ObservabilityHandler) EnableLogCollection(c *gin.Context) {
	var input sohaapi.LogCollectionEnableInput
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid log collection confirmation payload")
		return
	}
	item, err := h.collection.EnableLogCollection(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ObservabilityHandler) DisableLogCollection(c *gin.Context) {
	var input sohaapi.LogCollectionDisableInput
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid log collection disable payload")
		return
	}
	item, err := h.collection.DisableLogCollection(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("clusterID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ObservabilityHandler) ListProviders(c *gin.Context) {
	items, err := h.dataSources.ListProviders(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *ObservabilityHandler) QueryMetrics(c *gin.Context) {
	var input sohaapi.ObservabilityMetricQueryInput
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid metric query payload")
		return
	}
	if h.signals == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "metric query service is unavailable")
		return
	}
	stepSeconds := input.StepSeconds
	if stepSeconds == 0 {
		stepSeconds = 60
	}
	result, err := h.signals.QueryMetrics(
		c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input.DataSourceID,
		telemetry.MetricRangeQuery{
			Scope: metricQueryScope(input.Scope), MetricKey: string(input.MetricKey),
			TimeFrom: input.TimeFrom, TimeTo: input.TimeTo, Step: time.Duration(stepSeconds) * time.Second,
		},
	)
	if err != nil {
		writeError(c, err)
		return
	}
	series := make([]sohaapi.ObservabilityMetricSeries, 0, len(result.Series))
	for _, item := range result.Series {
		points := make([]sohaapi.ObservabilityMetricPoint, 0, len(item.Points))
		for _, point := range item.Points {
			points = append(points, sohaapi.ObservabilityMetricPoint{Timestamp: point.Timestamp, Value: point.Value})
		}
		series = append(series, sohaapi.ObservabilityMetricSeries{
			Key: item.Key, Label: item.Label, Unit: item.Unit, Latest: item.Latest, Points: points,
		})
	}
	apiresponse.Item(c, http.StatusOK, sohaapi.ObservabilityMetricQueryResult{
		DataSourceID: result.DataSourceID,
		BackendType:  sohaapi.ObservabilityMetricQueryResultBackendType(result.BackendType),
		Series:       series,
	})
}

func (h *ObservabilityHandler) QueryTraces(c *gin.Context) {
	var input sohaapi.ObservabilityTraceQueryInput
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid trace query payload")
		return
	}
	if h.signals == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "trace query service is unavailable")
		return
	}
	result, err := h.signals.QueryTraces(
		c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input.DataSourceID,
		telemetry.TraceQuery{
			Scope: traceQueryScope(input.Scope), TimeFrom: input.TimeFrom, TimeTo: input.TimeTo,
			TraceID: input.TraceID, MinDuration: time.Duration(input.MinDurationMs) * time.Millisecond, Limit: input.Limit,
		},
	)
	if err != nil {
		writeError(c, err)
		return
	}
	spans := make([]sohaapi.ObservabilityTraceSpan, 0, len(result.Spans))
	for _, span := range result.Spans {
		tags := span.Tags
		if tags == nil {
			tags = map[string]string{}
		}
		spans = append(spans, sohaapi.ObservabilityTraceSpan{
			TraceID: span.TraceID, SpanID: span.SpanID, ParentSpanID: span.ParentSpanID,
			Operation: span.Operation, Service: span.Service, DurationMs: span.DurationMS,
			StartTime: span.StartTime, Tags: tags, Error: span.Error,
		})
	}
	apiresponse.Item(c, http.StatusOK, sohaapi.ObservabilityTraceQueryResult{
		DataSourceID: result.DataSourceID,
		BackendType:  sohaapi.ObservabilityTraceQueryResultBackendType(result.BackendType),
		Summary:      result.Summary,
		Services:     result.Services,
		Spans:        spans,
	})
}

func metricQueryScope(scope *sohaapi.ObservabilityQueryScope) telemetry.MetricScope {
	if scope == nil {
		return telemetry.MetricScope{}
	}
	return telemetry.MetricScope{
		ClusterID: scope.ClusterID, Namespace: scope.Namespace, Workload: scope.Workload, Service: scope.Service,
	}
}

func traceQueryScope(scope *sohaapi.ObservabilityQueryScope) telemetry.TraceScope {
	if scope == nil {
		return telemetry.TraceScope{}
	}
	return telemetry.TraceScope{
		ClusterID: scope.ClusterID, Namespace: scope.Namespace, Workload: scope.Workload, Service: scope.Service,
	}
}

func (h *ObservabilityHandler) ListDataSources(c *gin.Context) {
	items, err := h.dataSources.ListDataSources(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Items(c, http.StatusOK, items)
}

func (h *ObservabilityHandler) CreateDataSource(c *gin.Context) {
	input, ok := bindObservabilityDataSourceInput(c)
	if !ok {
		return
	}
	item, err := h.dataSources.CreateDataSource(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusCreated, item)
}

func (h *ObservabilityHandler) UpdateDataSource(c *gin.Context) {
	input, ok := bindObservabilityDataSourceInput(c)
	if !ok {
		return
	}
	item, err := h.dataSources.UpdateDataSource(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("dataSourceID"), input)
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func (h *ObservabilityHandler) ValidateDataSource(c *gin.Context) {
	item, err := h.dataSources.ValidateDataSource(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("dataSourceID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, item)
}

func bindObservabilityDataSourceInput(c *gin.Context) (sohaapi.ObservabilityDataSourceInput, bool) {
	var input sohaapi.ObservabilityDataSourceInput
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid observability data source payload")
		return sohaapi.ObservabilityDataSourceInput{}, false
	}
	return input, true
}
