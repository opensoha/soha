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
	endpoint, _ := config["endpoint"].(string)
	if query.TimeTo.IsZero() {
		query.TimeTo = time.Now().UTC()
	}
	if query.TimeFrom.IsZero() {
		query.TimeFrom = query.TimeTo.Add(-60 * time.Minute)
	}
	if query.MinDuration <= 0 {
		query.MinDuration = 250 * time.Millisecond
	}
	if query.Limit <= 0 {
		query.Limit = 20
	}

	queryURL, err := url.Parse(strings.TrimRight(strings.TrimSpace(endpoint), "/") + "/api/traces")
	if err != nil {
		return Result{}, err
	}
	params := queryURL.Query()
	service := strings.TrimSpace(stringValue(config["serviceName"], query.Scope.Service))
	if service != "" {
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
		return Result{}, err
	}
	if bearerToken := strings.TrimSpace(stringValue(config["bearerToken"], "")); bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("jaeger query failed with status %d", resp.StatusCode)
	}
	var payload struct {
		Data []struct {
			TraceID   string `json:"traceID"`
			Processes map[string]struct {
				ServiceName string `json:"serviceName"`
			} `json:"processes"`
			Spans []struct {
				SpanID       string            `json:"spanID"`
				References   []struct {
					RefType string `json:"refType"`
					SpanID  string `json:"spanID"`
				} `json:"references"`
				OperationName string           `json:"operationName"`
				ProcessID    string            `json:"processID"`
				StartTime    int64             `json:"startTime"`
				Duration     int64             `json:"duration"`
				Tags         []struct {
					Key   string `json:"key"`
					Type  string `json:"type"`
					Value any    `json:"value"`
				} `json:"tags"`
			} `json:"spans"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Result{}, err
	}
	spans := make([]Span, 0)
	hotspots := map[string]map[string]any{}
	for _, trace := range payload.Data {
		for _, span := range trace.Spans {
			serviceName := ""
			if process, ok := trace.Processes[span.ProcessID]; ok {
				serviceName = process.ServiceName
			}
			tagMap := map[string]any{}
			errorFlag := false
			for _, tag := range span.Tags {
				tagMap[tag.Key] = tag.Value
				if tag.Key == "error" {
					if current, ok := tag.Value.(bool); ok && current {
						errorFlag = true
					}
				}
			}
			parentSpanID := ""
			for _, reference := range span.References {
				if strings.EqualFold(reference.RefType, "CHILD_OF") && strings.TrimSpace(reference.SpanID) != "" {
					parentSpanID = strings.TrimSpace(reference.SpanID)
					break
				}
			}
			current := Span{
				TraceID:      trace.TraceID,
				SpanID:       span.SpanID,
				ParentSpanID: parentSpanID,
				Operation:    span.OperationName,
				Service:      serviceName,
				DurationMS:   float64(span.Duration) / 1000.0,
				StartTime:    time.UnixMicro(span.StartTime).UTC(),
				Tags:         tagMap,
				Error:        errorFlag,
			}
			spans = append(spans, current)
			key := fmt.Sprintf("%s::%s", current.Service, current.Operation)
			entry, ok := hotspots[key]
			if !ok {
				entry = map[string]any{
					"service":     current.Service,
					"operation":   current.Operation,
					"count":       0,
					"maxDuration": 0.0,
					"errorCount":  0,
				}
				hotspots[key] = entry
			}
			entry["count"] = entry["count"].(int) + 1
			if current.DurationMS > entry["maxDuration"].(float64) {
				entry["maxDuration"] = current.DurationMS
			}
			if current.Error {
				entry["errorCount"] = entry["errorCount"].(int) + 1
			}
		}
	}
	hotspotItems := make([]map[string]any, 0, len(hotspots))
	for _, item := range hotspots {
		hotspotItems = append(hotspotItems, item)
	}
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
