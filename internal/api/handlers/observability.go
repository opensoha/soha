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
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainobservability "github.com/opensoha/soha/internal/domain/observability"
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
	ListMetricCatalog(context.Context, domainidentity.Principal, string) ([]appmonitoring.MetricDefinition, error)
	ListServices(context.Context, domainidentity.Principal, string, telemetry.ServiceQuery) (appmonitoring.ServiceListResult, error)
	GetService(context.Context, domainidentity.Principal, string, telemetry.ServiceQuery) (appmonitoring.ServiceDetailResult, error)
	GetServiceTopology(context.Context, domainidentity.Principal, string, telemetry.ServiceQuery) (appmonitoring.ServiceTopologyResult, error)
}

type ObservabilityDashboards interface {
	ListDashboards(context.Context, domainidentity.Principal) ([]domainobservability.Dashboard, error)
	ListMetricDataSources(context.Context, domainidentity.Principal) ([]domaincopilot.DataSource, error)
	GetDashboard(context.Context, domainidentity.Principal, string) (domainobservability.Dashboard, error)
	ImportGrafanaDashboard(context.Context, domainidentity.Principal, string, string) (domainobservability.DashboardImportResult, error)
	DeleteDashboard(context.Context, domainidentity.Principal, string) error
	QueryDashboardPanel(context.Context, domainidentity.Principal, string, string, time.Time, time.Time, time.Duration, map[string]string) (appmonitoring.MetricQueryResult, error)
}

func (h *ObservabilityHandler) ListMetricDataSources(c *gin.Context) {
	if h.dashboards == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "dashboard service is unavailable")
		return
	}
	items, err := h.dashboards.ListMetricDataSources(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	result := make([]sohaapi.ObservabilityMetricDataSource, 0, len(items))
	for _, item := range items {
		result = append(result, sohaapi.ObservabilityMetricDataSource{ID: item.ID, Name: item.Name})
	}
	apiresponse.Items(c, http.StatusOK, result)
}

type ObservabilityHandler struct {
	dataSources ObservabilityDataSources
	collection  ObservabilityLogCollection
	signals     ObservabilitySignalQueries
	dashboards  ObservabilityDashboards
}

func NewObservabilityHandler(dataSources ObservabilityDataSources, signals ...ObservabilitySignalQueries) *ObservabilityHandler {
	collection, _ := dataSources.(ObservabilityLogCollection)
	handler := &ObservabilityHandler{dataSources: dataSources, collection: collection}
	if len(signals) > 0 {
		handler.signals = signals[0]
		handler.dashboards, _ = signals[0].(ObservabilityDashboards)
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
	writeObservabilityMetricResult(c, result)
}

func (h *ObservabilityHandler) ListMetricCatalog(c *gin.Context) {
	if h.signals == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "metric catalog service is unavailable")
		return
	}
	items, err := h.signals.ListMetricCatalog(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Query("dataSourceId"))
	if err != nil {
		writeError(c, err)
		return
	}
	result := make([]sohaapi.ObservabilityMetricDefinition, 0, len(items))
	for _, item := range items {
		result = append(result, sohaapi.ObservabilityMetricDefinition{
			Key: item.Key, Label: item.Label, Description: item.Description, Unit: item.Unit,
			SemanticConvention: item.SemanticConvention, Signal: sohaapi.ObservabilitySignalMetrics, Available: item.Available,
		})
	}
	apiresponse.Items(c, http.StatusOK, result)
}

func (h *ObservabilityHandler) ListServices(c *gin.Context) {
	if h.signals == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "service catalog is unavailable")
		return
	}
	query, ok := bindServiceQuery(c, "")
	if !ok {
		return
	}
	result, err := h.signals.ListServices(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Query("dataSourceId"), query)
	if err != nil {
		writeError(c, err)
		return
	}
	items := make([]sohaapi.ObservabilityService, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, publicObservabilityService(item, result.Scope, result.Environment, result.ClusterIDs))
	}
	c.JSON(http.StatusOK, sohaapi.ObservabilityServiceListEnvelope{Items: items, Meta: publicQueryMeta(result.Meta)})
}

func (h *ObservabilityHandler) GetService(c *gin.Context) {
	if h.signals == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "service catalog is unavailable")
		return
	}
	query, ok := bindServiceQuery(c, c.Param("serviceID"))
	if !ok {
		return
	}
	result, err := h.signals.GetService(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Query("dataSourceId"), query)
	if err != nil {
		writeError(c, err)
		return
	}
	data := publicObservabilityService(result.Item, result.Scope, result.Environment, result.ClusterIDs)
	c.JSON(http.StatusOK, sohaapi.ObservabilityServiceEnvelope{Data: data, Meta: publicQueryMeta(result.Meta)})
}

func (h *ObservabilityHandler) GetServiceTopology(c *gin.Context) {
	if h.signals == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "service topology is unavailable")
		return
	}
	query, ok := bindServiceQuery(c, c.Param("serviceID"))
	if !ok {
		return
	}
	result, err := h.signals.GetServiceTopology(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Query("dataSourceId"), query)
	if err != nil {
		writeError(c, err)
		return
	}
	nodes := make([]sohaapi.ObservabilityTopologyNode, 0, len(result.Topology.Nodes))
	for _, item := range result.Topology.Nodes {
		nodes = append(nodes, sohaapi.ObservabilityTopologyNode{ServiceID: item.ServiceID, Name: item.Name, Status: sohaapi.ObservabilityServiceStatusUnknown})
	}
	edges := make([]sohaapi.ObservabilityTopologyEdge, 0, len(result.Topology.Edges))
	for _, item := range result.Topology.Edges {
		edges = append(edges, sohaapi.ObservabilityTopologyEdge{
			SourceServiceID: item.SourceServiceID, TargetServiceID: item.TargetServiceID, Status: sohaapi.ObservabilityServiceStatusUnknown,
		})
	}
	apiresponse.Item(c, http.StatusOK, sohaapi.ObservabilityTopology{
		Beta: true, Nodes: nodes, Edges: edges, Meta: publicQueryMeta(result.Meta),
	})
}

func bindServiceQuery(c *gin.Context, serviceID string) (telemetry.ServiceQuery, bool) {
	from, fromErr := time.Parse(time.RFC3339, c.Query("timeFrom"))
	to, toErr := time.Parse(time.RFC3339, c.Query("timeTo"))
	if fromErr != nil || toErr != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "valid timeFrom and timeTo are required")
		return telemetry.ServiceQuery{}, false
	}
	return telemetry.ServiceQuery{
		ServiceID: serviceID, ServiceName: c.Query("service"), Environment: c.Query("environment"),
		Scope:    telemetry.TraceScope{ClusterID: c.Query("clusterId"), Namespace: c.Query("namespace")},
		TimeFrom: from, TimeTo: to,
	}, true
}

func publicObservabilityService(item telemetry.Service, scope telemetry.TraceScope, environment string, clusterIDs []string) sohaapi.ObservabilityService {
	instances := make([]sohaapi.ObservabilityServiceInstance, 0, len(item.Instances))
	for _, instance := range item.Instances {
		instances = append(instances, sohaapi.ObservabilityServiceInstance{ID: instance.ID, Name: instance.Name, Status: sohaapi.ObservabilityServiceStatusUnknown})
	}
	endpoints := make([]sohaapi.ObservabilityServiceEndpoint, 0, len(item.Endpoints))
	for _, endpoint := range item.Endpoints {
		endpoints = append(endpoints, sohaapi.ObservabilityServiceEndpoint{ID: endpoint.ID, Name: endpoint.Name, Status: sohaapi.ObservabilityServiceStatusUnknown})
	}
	return sohaapi.ObservabilityService{
		ID: item.ID, Name: item.Name, DisplayName: item.DisplayName, Environment: environment,
		Namespace: scope.Namespace, ClusterIDs: clusterIDs, Status: sohaapi.ObservabilityServiceStatusUnknown,
		Instances: instances, Endpoints: endpoints,
	}
}

func publicQueryMeta(meta appmonitoring.QueryMeta) sohaapi.ObservabilityQueryResultMeta {
	observedAt := meta.ObservedAt
	return sohaapi.ObservabilityQueryResultMeta{
		State: sohaapi.ObservabilityQueryState(meta.State), Warnings: meta.Warnings,
		ObservedAt: &observedAt, ScopeRestricted: meta.ScopeRestricted,
	}
}

func writeObservabilityMetricResult(c *gin.Context, result appmonitoring.MetricQueryResult) {
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
	response := sohaapi.ObservabilityMetricQueryResult{
		DataSourceID: result.DataSourceID,
		BackendType:  sohaapi.ObservabilityMetricQueryResultBackendType(result.BackendType),
		Series:       series,
	}
	if result.Meta != nil {
		meta := publicQueryMeta(*result.Meta)
		meta.Snapshot = publicMetricQuerySnapshot(result.Meta.Snapshot)
		response.Meta = &meta
	}
	apiresponse.Item(c, http.StatusOK, response)
}

func publicMetricQuerySnapshot(value map[string]any) *sohaapi.ObservabilityQuerySnapshot {
	contextValue, _ := value["context"].(map[string]any)
	timeRange, _ := contextValue["timeRange"].(map[string]any)
	from, fromOK := timeRange["from"].(time.Time)
	to, toOK := timeRange["to"].(time.Time)
	if !fromOK || !toOK {
		return nil
	}
	createdAt, _ := value["createdAt"].(time.Time)
	return &sohaapi.ObservabilityQuerySnapshot{
		Version: sohaapi.ObservabilityQuerySnapshotVersion("v1"), Signal: sohaapi.ObservabilitySignal("metrics"),
		DataSourceID: dashboardSnapshotString(value["dataSourceId"]), BackendType: dashboardSnapshotString(value["backendType"]),
		Context: sohaapi.ObservabilityContext{
			Version: sohaapi.ObservabilityContextVersion("v1"), Scope: sohaapi.ObservabilityQueryScope{},
			TimeRange: sohaapi.ObservabilityTimeRange{From: from, To: to},
		},
		QueryLanguage: sohaapi.ObservabilityQueryLanguage("promql"), Query: dashboardSnapshotString(value["query"]), CreatedAt: &createdAt,
	}
}

func dashboardSnapshotString(value any) string {
	text, _ := value.(string)
	return text
}

func (h *ObservabilityHandler) ListDashboards(c *gin.Context) {
	if h.dashboards == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "dashboard service is unavailable")
		return
	}
	items, err := h.dashboards.ListDashboards(c.Request.Context(), apiMiddleware.PrincipalFromContext(c))
	if err != nil {
		writeError(c, err)
		return
	}
	result := make([]sohaapi.ObservabilityDashboard, 0, len(items))
	for _, item := range items {
		result = append(result, publicObservabilityDashboard(item, false))
	}
	apiresponse.Items(c, http.StatusOK, result)
}

func (h *ObservabilityHandler) GetDashboard(c *gin.Context) {
	if h.dashboards == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "dashboard service is unavailable")
		return
	}
	item, err := h.dashboards.GetDashboard(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("dashboardID"))
	if err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, publicObservabilityDashboard(item, true))
}

func (h *ObservabilityHandler) ImportGrafanaDashboard(c *gin.Context) {
	if h.dashboards == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "dashboard service is unavailable")
		return
	}
	var input sohaapi.ObservabilityGrafanaDashboardImportInput
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid Grafana dashboard import payload")
		return
	}
	result, err := h.dashboards.ImportGrafanaDashboard(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), input.JSON, input.DataSourceID)
	if err != nil {
		writeError(c, err)
		return
	}
	warnings := make([]sohaapi.ObservabilityDashboardImportWarning, 0, len(result.Warnings))
	for _, warning := range result.Warnings {
		warnings = append(warnings, sohaapi.ObservabilityDashboardImportWarning{
			Code: sohaapi.ObservabilityDashboardImportWarningCode(warning.Code), Message: warning.Message, PanelID: warning.PanelID,
		})
	}
	apiresponse.Item(c, http.StatusCreated, sohaapi.ObservabilityDashboardImportResult{
		Dashboard: publicObservabilityDashboard(result.Dashboard, true), Warnings: warnings,
		ImportedPanelCount: result.ImportedPanelCount, SkippedPanelCount: result.SkippedPanelCount,
	})
}

func (h *ObservabilityHandler) DeleteDashboard(c *gin.Context) {
	if h.dashboards == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "dashboard service is unavailable")
		return
	}
	if err := h.dashboards.DeleteDashboard(c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("dashboardID")); err != nil {
		writeError(c, err)
		return
	}
	apiresponse.Item(c, http.StatusOK, sohaapi.OperationStatus{Status: "deleted"})
}

func (h *ObservabilityHandler) QueryDashboardPanel(c *gin.Context) {
	if h.dashboards == nil {
		apiresponse.Error(c, http.StatusServiceUnavailable, "service_unavailable", "dashboard service is unavailable")
		return
	}
	var input sohaapi.ObservabilityDashboardPanelQueryInput
	if err := c.ShouldBindJSON(&input); err != nil {
		apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "invalid dashboard panel query payload")
		return
	}
	result, err := h.dashboards.QueryDashboardPanel(
		c.Request.Context(), apiMiddleware.PrincipalFromContext(c), c.Param("dashboardID"), c.Param("panelID"),
		input.TimeFrom, input.TimeTo, time.Duration(input.StepSeconds)*time.Second, input.Variables,
	)
	if err != nil {
		writeError(c, err)
		return
	}
	writeObservabilityMetricResult(c, result)
}

func publicObservabilityDashboard(item domainobservability.Dashboard, includeRaw bool) sohaapi.ObservabilityDashboard {
	panels := make([]sohaapi.ObservabilityDashboardPanel, 0, len(item.Panels))
	for _, panel := range item.Panels {
		targets := make([]sohaapi.ObservabilityDashboardTarget, 0, len(panel.Targets))
		for _, target := range panel.Targets {
			targets = append(targets, sohaapi.ObservabilityDashboardTarget{
				RefID: target.RefID, Expression: target.Expression, Legend: target.Legend,
				DataSourceType: target.DataSourceType, DataSourceUID: target.DataSourceUID, DataSourceID: target.DataSourceID,
			})
		}
		rawJSON := ""
		if includeRaw {
			rawJSON = panel.RawJSON
		}
		panels = append(panels, sohaapi.ObservabilityDashboardPanel{
			ID: panel.ID, Title: panel.Title, Type: sohaapi.ObservabilityDashboardPanelType(panel.Type), Queryable: panel.Queryable,
			Layout:  sohaapi.ObservabilityDashboardPanelLayout{X: panel.Layout.X, Y: panel.Layout.Y, W: panel.Layout.W, H: panel.Layout.H},
			Targets: targets, Markdown: panel.Markdown, SourcePanelType: panel.SourcePanelType, Unsupported: panel.Unsupported, RawJSON: rawJSON,
		})
	}
	variables := make([]sohaapi.ObservabilityDashboardVariable, 0, len(item.Variables))
	for _, variable := range item.Variables {
		variables = append(variables, sohaapi.ObservabilityDashboardVariable{
			Name: variable.Name, Label: variable.Label, Type: sohaapi.ObservabilityDashboardVariableType(variable.Type), Current: variable.Current, Options: variable.Options,
		})
	}
	bindings := make([]sohaapi.ObservabilityDashboardDataSourceBinding, 0, len(item.DataSourceBindings))
	for _, binding := range item.DataSourceBindings {
		bindings = append(bindings, sohaapi.ObservabilityDashboardDataSourceBinding{Type: binding.Type, UID: binding.UID, DataSourceID: binding.DataSourceID})
	}
	warnings := make([]sohaapi.ObservabilityDashboardImportWarning, 0, len(item.ImportWarnings))
	for _, warning := range item.ImportWarnings {
		warnings = append(warnings, sohaapi.ObservabilityDashboardImportWarning{Code: sohaapi.ObservabilityDashboardImportWarningCode(warning.Code), Message: warning.Message, PanelID: warning.PanelID})
	}
	rawJSON := ""
	if includeRaw {
		rawJSON = item.RawJSON
	}
	return sohaapi.ObservabilityDashboard{
		ID: item.ID, Name: item.Name, Source: sohaapi.ObservabilityDashboardSource(item.Source), SourceFormat: sohaapi.ObservabilityDashboardSourceFormat(item.SourceFormat), SourceSchemaVersion: item.SourceSchemaVersion,
		DataSourceID: item.DataSourceID, Tags: item.Tags, Panels: panels, Variables: variables, DataSourceBindings: bindings,
		ImportWarnings: warnings, RawJSON: rawJSON, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
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
