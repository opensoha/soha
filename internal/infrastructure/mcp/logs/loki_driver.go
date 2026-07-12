package logs

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
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
	endpoint, _ := config["endpoint"].(string)
	labelKeys, _ := config["labelKeys"].(map[string]any)
	if strings.TrimSpace(endpoint) == "" {
		return fmt.Errorf("loki endpoint is required")
	}
	if labelKeys == nil {
		return fmt.Errorf("loki labelKeys config is required")
	}
	return nil
}

func (d lokiDriver) Correlate(ctx context.Context, sourceID string, config map[string]any, query CorrelationQuery) (CorrelationResult, error) {
	if err := d.ValidateConfig(config); err != nil {
		return CorrelationResult{}, err
	}
	endpoint, _ := config["endpoint"].(string)
	limit := intConfig(config, "correlationLimit", query.Limit)
	if limit <= 0 {
		limit = 20
	}
	labelKeys := mapConfig(config["labelKeys"])
	queryURL, err := url.Parse(strings.TrimRight(strings.TrimSpace(endpoint), "/") + "/loki/api/v1/query_range")
	if err != nil {
		return CorrelationResult{}, err
	}
	params := queryURL.Query()
	params.Set("query", buildLokiCorrelationQuery(query, labelKeys))
	if !query.TimeFrom.IsZero() {
		params.Set("start", strconv.FormatInt(query.TimeFrom.UnixNano(), 10))
	}
	if !query.TimeTo.IsZero() {
		params.Set("end", strconv.FormatInt(query.TimeTo.UnixNano(), 10))
	}
	params.Set("limit", strconv.Itoa(limit))
	queryURL.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, queryURL.String(), nil)
	if err != nil {
		return CorrelationResult{}, err
	}
	if bearerToken := stringConfig(config, "bearerToken", ""); bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearerToken))
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return CorrelationResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return CorrelationResult{}, fmt.Errorf("loki correlate failed with status %d", resp.StatusCode)
	}
	var payload lokiQueryRangePayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return CorrelationResult{ErrorKind: "decode_failed"}, fmt.Errorf("decode loki correlate response: %w", err)
	}
	records := lokiRecords(payload.Data.Result, labelKeys, limit)
	signatures := summarizeSignatures(records)
	summary := "no correlated logs found"
	if len(records) > 0 {
		summary = fmt.Sprintf("%d correlated logs found", len(records))
	}
	return CorrelationResult{
		SourceID:   sourceID,
		Summary:    summary,
		Records:    records,
		Signatures: signatures,
		Truncated:  len(records) >= limit,
		QueryCost: map[string]any{
			"backendType": "loki",
			"limit":       limit,
			"recordCount": len(records),
		},
		SampleWindow: map[string]any{
			"timeFrom": query.TimeFrom.Format(time.RFC3339),
			"timeTo":   query.TimeTo.Format(time.RFC3339),
		},
	}, nil
}

func lokiRecords(streams []lokiStreamResult, labelKeys map[string]string, limit int) []Record {
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
	sort.Slice(records, func(i, j int) bool {
		return records[i].Timestamp.After(records[j].Timestamp)
	})
	if len(records) > limit {
		records = records[:limit]
	}
	return records
}

func buildLokiCorrelationQuery(query CorrelationQuery, labelKeys map[string]string) string {
	labels := make([]string, 0)
	for logicalKey, value := range map[string]string{
		"cluster":   query.Scope.ClusterID,
		"namespace": query.Scope.Namespace,
		"service":   query.Scope.Service,
		"workload":  query.Scope.Workload,
	} {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		labels = append(labels, fmt.Sprintf(`%s="%s"`, labelKey(labelKeys, logicalKey), escapeLogQL(value)))
	}
	expr := "{}"
	if len(labels) > 0 {
		expr = "{" + strings.Join(labels, ",") + "}"
	}
	for _, term := range correlationTerms(query) {
		expr += fmt.Sprintf(` |= "%s"`, escapeLogQL(term))
	}
	return expr
}

func labelKey(labelKeys map[string]string, logicalKey string) string {
	if value := strings.TrimSpace(labelKeys[logicalKey]); value != "" {
		return value
	}
	return logicalKey
}

func escapeLogQL(value string) string {
	return strings.ReplaceAll(value, `"`, `\"`)
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
