package traces

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type jaegerDriver struct {
	http *http.Client
}

func newJaegerDriver() Driver {
	return jaegerDriver{http: &http.Client{Timeout: 8 * time.Second}}
}

func (jaegerDriver) BackendType() string {
	return "jaeger"
}

func (jaegerDriver) ValidateConfig(config map[string]any) error {
	if config == nil {
		return fmt.Errorf("jaeger config is required")
	}
	endpoint, _ := config["endpoint"].(string)
	if strings.TrimSpace(endpoint) == "" {
		return fmt.Errorf("jaeger endpoint is required")
	}
	return nil
}

func (d jaegerDriver) FindSlowSpans(ctx context.Context, sourceID string, config map[string]any, query Query) (Result, error) {
	if err := d.ValidateConfig(config); err != nil {
		return Result{}, err
	}
	query = normalizeSlowSpanQuery(query)
	req, err := newJaegerRequest(ctx, config, query)
	if err != nil {
		return Result{}, err
	}
	payload, err := d.queryJaeger(req)
	if err != nil {
		return Result{}, err
	}
	spans := jaegerSpans(payload)
	hotspotItems := traceHotspots(spans)
	summary := "no trace hotspots detected"
	if len(spans) > 0 {
		summary = fmt.Sprintf("%d spans matched, %d hotspot groups", len(spans), len(hotspotItems))
	}
	return Result{
		SourceID: sourceID,
		Summary:  summary,
		Spans:    spans,
		Hotspots: hotspotItems,
		QueryCost: map[string]any{
			"backendType": "jaeger",
			"spanCount":   len(spans),
			"limit":       query.Limit,
		},
		SampleWindow: map[string]any{
			"timeFrom": query.TimeFrom.Format(time.RFC3339),
			"timeTo":   query.TimeTo.Format(time.RFC3339),
		},
	}, nil
}

type jaegerPayload struct {
	Data []jaegerTrace `json:"data"`
}

type jaegerTrace struct {
	TraceID   string                   `json:"traceID"`
	Processes map[string]jaegerProcess `json:"processes"`
	Spans     []jaegerSpan             `json:"spans"`
}

type jaegerProcess struct {
	ServiceName string `json:"serviceName"`
}

type jaegerSpan struct {
	SpanID        string            `json:"spanID"`
	References    []jaegerReference `json:"references"`
	OperationName string            `json:"operationName"`
	ProcessID     string            `json:"processID"`
	StartTime     int64             `json:"startTime"`
	Duration      int64             `json:"duration"`
	Tags          []jaegerTag       `json:"tags"`
}

type jaegerReference struct {
	RefType string `json:"refType"`
	SpanID  string `json:"spanID"`
}

type jaegerTag struct {
	Key   string `json:"key"`
	Type  string `json:"type"`
	Value any    `json:"value"`
}

func newJaegerRequest(ctx context.Context, config map[string]any, query Query) (*http.Request, error) {
	endpoint := stringValue(config["endpoint"], "")
	queryURL, err := url.Parse(strings.TrimRight(strings.TrimSpace(endpoint), "/") + "/api/traces")
	if err != nil {
		return nil, err
	}
	params := queryURL.Query()
	if service := strings.TrimSpace(stringValue(config["serviceName"], query.Scope.Service)); service != "" {
		params.Set("service", service)
	}
	if operation := strings.TrimSpace(stringValue(config["operation"], "")); operation != "" {
		params.Set("operation", operation)
	}
	params.Set("start", strconv.FormatInt(query.TimeFrom.UnixMicro(), 10))
	params.Set("end", strconv.FormatInt(query.TimeTo.UnixMicro(), 10))
	params.Set("limit", strconv.Itoa(query.Limit))
	params.Set("minDuration", formatJaegerDuration(query.MinDuration))
	queryURL.RawQuery = params.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, queryURL.String(), nil)
	if err != nil {
		return nil, err
	}
	if bearerToken := strings.TrimSpace(stringValue(config["bearerToken"], "")); bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	return req, nil
}

func (d jaegerDriver) queryJaeger(req *http.Request) (jaegerPayload, error) {
	resp, err := d.http.Do(req)
	if err != nil {
		return jaegerPayload{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return jaegerPayload{}, fmt.Errorf("jaeger query failed with status %d", resp.StatusCode)
	}
	var payload jaegerPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return jaegerPayload{}, err
	}
	return payload, nil
}

func jaegerSpans(payload jaegerPayload) []Span {
	spans := make([]Span, 0)
	for _, trace := range payload.Data {
		for _, span := range trace.Spans {
			spans = append(spans, jaegerTraceSpan(trace, span))
		}
	}
	return spans
}

func jaegerTraceSpan(trace jaegerTrace, span jaegerSpan) Span {
	serviceName := ""
	if process, ok := trace.Processes[span.ProcessID]; ok {
		serviceName = process.ServiceName
	}
	tags, errorFlag := jaegerTags(span.Tags)
	return Span{
		TraceID: trace.TraceID, SpanID: span.SpanID, ParentSpanID: jaegerParentSpanID(span.References),
		Operation: span.OperationName, Service: serviceName,
		DurationMS: float64(span.Duration) / 1000.0,
		StartTime:  time.UnixMicro(span.StartTime).UTC(), Tags: tags, Error: errorFlag,
	}
}

func jaegerTags(tags []jaegerTag) (map[string]any, bool) {
	values := make(map[string]any, len(tags))
	errorFlag := false
	for _, tag := range tags {
		values[tag.Key] = tag.Value
		if current, ok := tag.Value.(bool); tag.Key == "error" && ok && current {
			errorFlag = true
		}
	}
	return values, errorFlag
}

func jaegerParentSpanID(references []jaegerReference) string {
	for _, reference := range references {
		if strings.EqualFold(reference.RefType, "CHILD_OF") &&
			strings.TrimSpace(reference.SpanID) != "" {
			return strings.TrimSpace(reference.SpanID)
		}
	}
	return ""
}

func formatJaegerDuration(duration time.Duration) string {
	if duration%time.Second == 0 {
		return fmt.Sprintf("%ds", int(duration.Seconds()))
	}
	return fmt.Sprintf("%dms", duration.Milliseconds())
}

func stringValue(value any, fallback string) string {
	current, ok := value.(string)
	if !ok || strings.TrimSpace(current) == "" {
		return fallback
	}
	return strings.TrimSpace(current)
}
