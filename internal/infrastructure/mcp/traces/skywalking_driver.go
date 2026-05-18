package traces

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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
	endpoint := strings.TrimRight(strings.TrimSpace(stringValue(config["endpoint"], "")), "/")
	service := strings.TrimSpace(stringValue(config["serviceName"], query.Scope.Service))
	if service == "" {
		service = strings.TrimSpace(query.Scope.Workload)
	}
	if service == "" {
		return Result{}, fmt.Errorf("skywalking serviceName or query scope service is required")
	}
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

	graphQL := map[string]any{
		"query": `
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
`,
		"variables": map[string]any{
			"condition": map[string]any{
				"serviceName": service,
				"queryDuration": map[string]any{
					"start": query.TimeFrom.UnixMilli(),
					"end":   query.TimeTo.UnixMilli(),
					"step":  "MINUTE",
				},
				"traceState": "ALL",
				"minTraceDuration": query.MinDuration.Milliseconds(),
				"maxTraceDuration": query.TimeTo.Sub(query.TimeFrom).Milliseconds(),
				"paging": map[string]any{
					"pageNum":   1,
					"pageSize":  query.Limit,
					"needTotal": false,
				},
			},
		},
	}
	body, err := json.Marshal(graphQL)
	if err != nil {
		return Result{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	if bearerToken := strings.TrimSpace(stringValue(config["bearerToken"], "")); bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+bearerToken)
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return Result{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return Result{}, fmt.Errorf("skywalking query failed with status %d", resp.StatusCode)
	}
	var payload struct {
		Data struct {
			QueryBasicTraces struct {
				Traces []struct {
					TraceIDs      []string `json:"key"`
					EndpointNames []string `json:"endpointNames"`
					Duration      int64    `json:"duration"`
					IsError       bool     `json:"isError"`
					Start         string   `json:"start"`
				} `json:"traces"`
			} `json:"queryBasicTraces"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return Result{}, err
	}
	if len(payload.Errors) > 0 {
		return Result{}, fmt.Errorf("skywalking query failed: %s", payload.Errors[0].Message)
	}

	spans := make([]Span, 0, len(payload.Data.QueryBasicTraces.Traces))
	hotspots := map[string]map[string]any{}
	for index, item := range payload.Data.QueryBasicTraces.Traces {
			traceID := ""
			if len(item.TraceIDs) > 0 {
				traceID = strings.TrimSpace(item.TraceIDs[0])
			}
			operation := ""
		if len(item.EndpointNames) > 0 {
			operation = strings.TrimSpace(item.EndpointNames[0])
		}
		startTime := query.TimeTo
		if parsed, parseErr := time.Parse(time.RFC3339, item.Start); parseErr == nil {
			startTime = parsed.UTC()
		}
		current := Span{
			TraceID:    traceID,
			SpanID:     fmt.Sprintf("skywalking-%d", index+1),
			Operation:  firstNonEmptyTraceValue(operation, service),
			Service:    service,
			DurationMS: float64(item.Duration),
			StartTime:  startTime,
			Tags: map[string]any{
				"traceIds": item.TraceIDs,
			},
			Error: item.IsError,
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
	hotspotItems := make([]map[string]any, 0, len(hotspots))
	for _, item := range hotspots {
		hotspotItems = append(hotspotItems, item)
	}
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

func firstNonEmptyTraceValue(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
