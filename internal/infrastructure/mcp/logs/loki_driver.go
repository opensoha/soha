package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type lokiDriver struct {
	http *http.Client
}

type lokiQueryRangePayload struct {
	Status string `json:"status"`
	Data   struct {
		Result []lokiStreamResult `json:"result"`
	} `json:"data"`
}

type lokiStreamResult struct {
	Stream map[string]string `json:"stream"`
	Values [][]string        `json:"values"`
}

func newLokiDriver() Driver {
	return lokiDriver{http: &http.Client{Timeout: 8 * time.Second}}
}

func (lokiDriver) BackendType() string {
	return "loki"
}

func (lokiDriver) ValidateConfig(config map[string]any) error {
	if config == nil {
		return fmt.Errorf("loki config is required")
	}
	labelKeys, _ := config["labelKeys"].(map[string]any)
	if err := validateHTTPConfig(config, "loki"); err != nil {
		return err
	}
	if labelKeys == nil {
		return fmt.Errorf("loki labelKeys config is required")
	}
	return nil
}

func (d lokiDriver) Correlate(ctx context.Context, sourceID string, config map[string]any, query CorrelationQuery) (CorrelationResult, error) {
	result, err := d.Search(ctx, sourceID, config, searchQueryFromCorrelation(query))
	if err != nil {
		return CorrelationResult{}, err
	}
	return correlationResultFromSearch(sourceID, query, result), nil
}

func (d lokiDriver) Search(ctx context.Context, sourceID string, config map[string]any, query SearchQuery) (SearchResult, error) {
	if err := d.ValidateConfig(config); err != nil {
		return SearchResult{}, err
	}
	query, cursor, err := normalizeSearchQuery(query)
	if err != nil {
		return SearchResult{}, err
	}
	endpoint, _ := config["endpoint"].(string)
	labelKeys := mapConfig(config["labelKeys"])
	queryURL, err := url.Parse(strings.TrimRight(strings.TrimSpace(endpoint), "/") + "/loki/api/v1/query_range")
	if err != nil {
		return SearchResult{}, err
	}
	params := queryURL.Query()
	params.Set("query", buildLokiSearchQuery(query, labelKeys))
	if !query.TimeFrom.IsZero() {
		params.Set("start", strconv.FormatInt(query.TimeFrom.UnixNano(), 10))
	}
	if !query.TimeTo.IsZero() {
		params.Set("end", strconv.FormatInt(query.TimeTo.UnixNano(), 10))
	}
	if !cursor.Timestamp.IsZero() {
		if query.Direction == "forward" {
			params.Set("start", strconv.FormatInt(cursor.Timestamp.UnixNano(), 10))
		} else {
			params.Set("end", strconv.FormatInt(cursor.Timestamp.UnixNano(), 10))
		}
	}
	params.Set("direction", query.Direction)
	params.Set("limit", strconv.Itoa(providerFetchLimit(query, cursor)))
	queryURL.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, queryURL.String(), nil)
	if err != nil {
		return SearchResult{}, err
	}
	applyProviderHeaders(req, config)
	resp, err := d.http.Do(req)
	if err != nil {
		return SearchResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return SearchResult{}, fmt.Errorf("loki search failed with status %d", resp.StatusCode)
	}
	var payload lokiQueryRangePayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return SearchResult{ErrorKind: "decode_failed"}, fmt.Errorf("decode loki search response: %w", err)
	}
	records := filterSeverities(lokiRecords(payload.Data.Result, labelKeys), query.Severities)
	records, nextPageToken, truncated := paginateRecords(records, query, cursor)
	return SearchResult{
		SourceID:      sourceID,
		Records:       records,
		NextPageToken: nextPageToken,
		Truncated:     truncated,
		QueryCost: map[string]any{
			"backendType": "loki",
			"limit":       query.Limit,
			"recordCount": len(records),
		},
	}, nil
}

func lokiRecords(streams []lokiStreamResult, labelKeys map[string]string) []Record {
	records := make([]Record, 0)
	for _, stream := range streams {
		for _, value := range stream.Values {
			if len(value) < 2 {
				continue
			}
			timestamp, _ := strconv.ParseInt(value[0], 10, 64)
			record := Record{
				Timestamp: time.Unix(0, timestamp).UTC(),
				Severity:  normalizeLogSeverity(stream.Stream[labelKey(labelKeys, "severity")]),
				Message:   value[1],
				Service:   stream.Stream[labelKey(labelKeys, "service")],
				Workload:  stream.Stream[labelKey(labelKeys, "workload")],
				Namespace: stream.Stream[labelKey(labelKeys, "namespace")],
				ClusterID: stream.Stream[labelKey(labelKeys, "cluster")],
				Pod:       stream.Stream[labelKey(labelKeys, "pod")],
				Container: stream.Stream[labelKey(labelKeys, "container")],
				TraceID:   stream.Stream[labelKey(labelKeys, "traceId")],
				SpanID:    stream.Stream[labelKey(labelKeys, "spanId")],
				Attributes: map[string]any{
					"labels": stream.Stream,
				},
			}
			if record.Severity == "" || record.Severity == "info" {
				record.Severity = severityFromMessage(record.Message)
			}
			records = append(records, record)
		}
	}
	return records
}

func buildLokiSearchQuery(query SearchQuery, labelKeys map[string]string) string {
	labels := make([]string, 0)
	pods := scopePodValues(query.Scope)
	pod := ""
	if len(pods) == 1 {
		pod = pods[0]
	}
	containers := scopeContainerValues(query.Scope)
	container := ""
	if len(containers) == 1 {
		container = containers[0]
	}
	for logicalKey, value := range map[string]string{
		"cluster":   query.Scope.ClusterID,
		"namespace": query.Scope.Namespace,
		"service":   query.Scope.Service,
		"workload":  query.Scope.Workload,
		"pod":       pod,
		"container": container,
		"traceId":   query.TraceID,
		"spanId":    query.SpanID,
	} {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		labels = append(labels, fmt.Sprintf(`%s="%s"`, labelKey(labelKeys, logicalKey), escapeLogQL(value)))
	}
	if len(pods) > 1 {
		patterns := make([]string, 0, len(pods))
		for _, value := range pods {
			patterns = append(patterns, regexp.QuoteMeta(value))
		}
		labels = append(labels, fmt.Sprintf(`%s=~"%s"`, labelKey(labelKeys, "pod"), escapeLogQL(strings.Join(patterns, "|"))))
	}
	if len(containers) > 1 {
		patterns := make([]string, 0, len(containers))
		for _, value := range containers {
			patterns = append(patterns, regexp.QuoteMeta(value))
		}
		labels = append(labels, fmt.Sprintf(`%s=~"%s"`, labelKey(labelKeys, "container"), escapeLogQL(strings.Join(patterns, "|"))))
	}
	expr := "{}"
	if len(labels) > 0 {
		expr = "{" + strings.Join(labels, ",") + "}"
	}
	for _, term := range query.Terms {
		expr += fmt.Sprintf(` |= "%s"`, escapeLogQL(term))
	}
	return expr
}

func labelKey(labelKeys map[string]string, logicalKey string) string {
	if value := strings.TrimSpace(labelKeys[logicalKey]); value != "" {
		return value
	}
	if logicalKey == "traceId" {
		return "trace_id"
	}
	if logicalKey == "spanId" {
		return "span_id"
	}
	return logicalKey
}

func escapeLogQL(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value)
}

func mapConfig(value any) map[string]string {
	current, ok := value.(map[string]any)
	if !ok {
		return map[string]string{}
	}
	out := make(map[string]string, len(current))
	for key, item := range current {
		out[key] = fmt.Sprint(item)
	}
	return out
}
