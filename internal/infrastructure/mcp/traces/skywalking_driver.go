package traces

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type skyWalkingDriver struct {
	http *http.Client
}

func newSkyWalkingDriver() Driver {
	return skyWalkingDriver{http: &http.Client{Timeout: 8 * time.Second}}
}

func (skyWalkingDriver) BackendType() string {
	return "skywalking"
}

func (skyWalkingDriver) ValidateConfig(config map[string]any) error {
	if config == nil {
		return fmt.Errorf("skywalking config is required")
	}
	endpoint, _ := config["endpoint"].(string)
	if strings.TrimSpace(endpoint) == "" {
		return fmt.Errorf("skywalking endpoint is required")
	}
	return nil
}

func (d skyWalkingDriver) FindSlowSpans(ctx context.Context, sourceID string, config map[string]any, query Query) (Result, error) {
	if err := d.ValidateConfig(config); err != nil {
		return Result{}, err
	}
	service := skyWalkingService(config, query)
	if service == "" && query.TraceID == "" {
		return Result{}, fmt.Errorf("skywalking serviceName or query scope service is required")
	}
	query = normalizeSlowSpanQuery(query)
	if strings.TrimSpace(stringValue(config["zipkinEndpoint"], "")) != "" {
		return d.findZipkinSpans(ctx, sourceID, config, service, query)
	}
	req, err := newSkyWalkingRequest(ctx, config, service, query)
	if err != nil {
		return Result{}, err
	}
	payload, err := d.querySkyWalking(req)
	if err != nil {
		return Result{}, err
	}
	if len(payload.Errors) > 0 {
		return Result{}, fmt.Errorf("skywalking query failed: %s", payload.Errors[0].Message)
	}
	spans := skyWalkingSpans(payload, service, query.TimeTo)
	hotspotItems := traceHotspots(spans)
	summary := "no trace hotspots detected"
	if len(spans) > 0 {
		summary = fmt.Sprintf("%d skywalking traces matched, %d hotspot groups", len(spans), len(hotspotItems))
	}
	return Result{
		SourceID: sourceID,
		Summary:  summary,
		Spans:    spans,
		Hotspots: hotspotItems,
		QueryCost: map[string]any{
			"backendType": "skywalking",
			"spanCount":   len(spans),
			"limit":       query.Limit,
		},
		SampleWindow: map[string]any{
			"timeFrom": query.TimeFrom.Format(time.RFC3339),
			"timeTo":   query.TimeTo.Format(time.RFC3339),
		},
	}, nil
}

type skyWalkingZipkinSpan struct {
	TraceID       string `json:"traceId"`
	ID            string `json:"id"`
	ParentID      string `json:"parentId"`
	Name          string `json:"name"`
	Timestamp     int64  `json:"timestamp"`
	Duration      int64  `json:"duration"`
	LocalEndpoint struct {
		ServiceName string `json:"serviceName"`
	} `json:"localEndpoint"`
	Tags map[string]string `json:"tags"`
}

func (d skyWalkingDriver) findZipkinSpans(ctx context.Context, sourceID string, config map[string]any, service string, query Query) (Result, error) {
	requestURL, err := skyWalkingZipkinURL(config, service, query)
	if err != nil {
		return Result{}, err
	}
	req, err := newJaegerHTTPRequest(ctx, config, requestURL)
	if err != nil {
		return Result{}, err
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("skywalking zipkin query failed with status %d", resp.StatusCode)
	}
	var traces [][]skyWalkingZipkinSpan
	if query.TraceID != "" {
		var spans []skyWalkingZipkinSpan
		if err := json.NewDecoder(resp.Body).Decode(&spans); err != nil {
			return Result{}, err
		}
		traces = append(traces, spans)
	} else if err := json.NewDecoder(resp.Body).Decode(&traces); err != nil {
		return Result{}, err
	}
	spans := skyWalkingZipkinSpans(traces)
	hotspotItems := traceHotspots(spans)
	summary := "no trace hotspots detected"
	if len(spans) > 0 {
		summary = fmt.Sprintf("%d skywalking OTLP spans matched, %d hotspot groups", len(spans), len(hotspotItems))
	}
	return Result{
		SourceID: sourceID, Summary: summary, Spans: spans, Hotspots: hotspotItems,
		QueryCost:    map[string]any{"backendType": "skywalking", "queryMode": "zipkin", "spanCount": len(spans), "limit": query.Limit},
		SampleWindow: map[string]any{"timeFrom": query.TimeFrom.Format(time.RFC3339), "timeTo": query.TimeTo.Format(time.RFC3339)},
	}, nil
}

func skyWalkingZipkinURL(config map[string]any, service string, query Query) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(stringValue(config["zipkinEndpoint"], "")), "/")
	path := "/api/v2/traces"
	if query.TraceID != "" {
		path = "/api/v2/trace/" + url.PathEscape(query.TraceID)
	}
	queryURL, err := url.Parse(base + path)
	if err != nil {
		return "", err
	}
	if query.TraceID == "" {
		params := queryURL.Query()
		params.Set("serviceName", service)
		params.Set("endTs", strconv.FormatInt(query.TimeTo.UnixMilli(), 10))
		params.Set("lookback", strconv.FormatInt(query.TimeTo.Sub(query.TimeFrom).Milliseconds(), 10))
		params.Set("limit", strconv.Itoa(query.Limit))
		params.Set("minDuration", strconv.FormatInt(query.MinDuration.Microseconds(), 10))
		queryURL.RawQuery = params.Encode()
	}
	return queryURL.String(), nil
}

func skyWalkingZipkinSpans(traces [][]skyWalkingZipkinSpan) []Span {
	spans := make([]Span, 0)
	for _, trace := range traces {
		for _, item := range trace {
			service := strings.TrimSpace(item.LocalEndpoint.ServiceName)
			spans = append(spans, Span{
				TraceID: item.TraceID, SpanID: item.ID, ParentSpanID: item.ParentID,
				Operation: item.Name, Service: service, DurationMS: float64(item.Duration) / 1000,
				StartTime: time.UnixMicro(item.Timestamp).UTC(), Tags: item.Tags,
				Error: item.Tags["error"] != "" || strings.EqualFold(item.Tags["otel.status_code"], "ERROR"),
			})
		}
	}
	return spans
}

const skyWalkingBasicTracesQuery = `
query QueryBasicTraces($condition: TraceQueryCondition!) {
  queryBasicTraces(condition: $condition) {
    traces {
      key: traceIds
      endpointNames
      duration
      isError
      start
    }
  }
}
`

type skyWalkingPayload struct {
	Data struct {
		QueryBasicTraces struct {
			Traces []skyWalkingTrace `json:"traces"`
		} `json:"queryBasicTraces"`
	} `json:"data"`
	Errors []skyWalkingError `json:"errors"`
}

type skyWalkingTrace struct {
	TraceIDs      []string `json:"key"`
	EndpointNames []string `json:"endpointNames"`
	Duration      int64    `json:"duration"`
	IsError       bool     `json:"isError"`
	Start         string   `json:"start"`
}

type skyWalkingError struct {
	Message string `json:"message"`
}

func skyWalkingService(config map[string]any, query Query) string {
	service := strings.TrimSpace(stringValue(config["serviceName"], query.Scope.Service))
	if service == "" {
		service = strings.TrimSpace(query.Scope.Workload)
	}
	return service
}

func newSkyWalkingRequest(
	ctx context.Context,
	config map[string]any,
	service string,
	query Query,
) (*http.Request, error) {
	return newSkyWalkingGraphQLRequest(ctx, config, skyWalkingGraphQLPayload(service, query))
}

func newSkyWalkingGraphQLRequest(ctx context.Context, config map[string]any, payload map[string]any) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(strings.TrimSpace(stringValue(config["endpoint"], "")), "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearerToken := strings.TrimSpace(stringValue(config["bearerToken"], "")); bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	return req, nil
}

func skyWalkingGraphQLPayload(service string, query Query) map[string]any {
	condition := map[string]any{
		"queryDuration": map[string]any{
			"start": query.TimeFrom.UnixMilli(),
			"end":   query.TimeTo.UnixMilli(),
			"step":  "MINUTE",
		},
		"traceState": "ALL", "minTraceDuration": query.MinDuration.Milliseconds(),
		"maxTraceDuration": query.TimeTo.Sub(query.TimeFrom).Milliseconds(),
		"paging": map[string]any{
			"pageNum": 1, "pageSize": query.Limit, "needTotal": false,
		},
	}
	if service != "" {
		condition["serviceName"] = service
	}
	if query.TraceID != "" {
		condition["traceId"] = query.TraceID
	}
	return map[string]any{
		"query": skyWalkingBasicTracesQuery,
		"variables": map[string]any{
			"condition": condition,
		},
	}
}

func (d skyWalkingDriver) querySkyWalking(req *http.Request) (skyWalkingPayload, error) {
	var payload skyWalkingPayload
	if err := d.doSkyWalking(req, &payload); err != nil {
		return skyWalkingPayload{}, err
	}
	return payload, nil
}

func (d skyWalkingDriver) doSkyWalking(req *http.Request, target any) error {
	resp, err := d.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf(
			"skywalking query failed with status %d", resp.StatusCode,
		)
	}
	return json.NewDecoder(resp.Body).Decode(target)
}

func skyWalkingSpans(payload skyWalkingPayload, service string, fallbackTime time.Time) []Span {
	items := payload.Data.QueryBasicTraces.Traces
	spans := make([]Span, 0, len(items))
	for index, item := range items {
		spans = append(spans, skyWalkingSpan(item, index, service, fallbackTime))
	}
	return spans
}

func skyWalkingSpan(
	item skyWalkingTrace,
	index int,
	service string,
	fallbackTime time.Time,
) Span {
	traceID := firstTraceValue(item.TraceIDs)
	operation := firstNonEmptyTraceValue(firstTraceValue(item.EndpointNames), service)
	startTime := fallbackTime
	if parsed, err := time.Parse(time.RFC3339, item.Start); err == nil {
		startTime = parsed.UTC()
	}
	return Span{
		TraceID: traceID, SpanID: fmt.Sprintf("skywalking-%d", index+1),
		Operation: operation, Service: service, DurationMS: float64(item.Duration),
		StartTime: startTime, Tags: map[string]string{"traceIds": strings.Join(item.TraceIDs, ",")}, Error: item.IsError,
	}
}

func firstTraceValue(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return strings.TrimSpace(values[0])
}

func firstNonEmptyTraceValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
