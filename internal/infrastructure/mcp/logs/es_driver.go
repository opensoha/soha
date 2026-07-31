package logs

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

type esDriver struct {
	http *http.Client
}

func newESDriver() Driver {
	return esDriver{http: &http.Client{Timeout: 8 * time.Second}}
}

func (esDriver) BackendType() string {
	return "es"
}

func (esDriver) ValidateConfig(config map[string]any) error {
	if config == nil {
		return fmt.Errorf("es config is required")
	}
	index, _ := config["index"].(string)
	if err := validateHTTPConfig(config, "es"); err != nil {
		return err
	}
	if strings.TrimSpace(index) == "" {
		return fmt.Errorf("es index is required")
	}
	return nil
}

func (d esDriver) Correlate(ctx context.Context, sourceID string, config map[string]any, query CorrelationQuery) (CorrelationResult, error) {
	result, err := d.Search(ctx, sourceID, config, searchQueryFromCorrelation(query))
	if err != nil {
		return CorrelationResult{}, err
	}
	return correlationResultFromSearch(sourceID, query, result), nil
}

func (d esDriver) Search(ctx context.Context, sourceID string, config map[string]any, query SearchQuery) (SearchResult, error) {
	if err := d.ValidateConfig(config); err != nil {
		return SearchResult{}, err
	}
	query, cursor, err := normalizeSearchQuery(query)
	if err != nil {
		return SearchResult{}, err
	}
	endpoint, _ := config["endpoint"].(string)
	index, _ := config["index"].(string)
	timestampField := stringConfig(config, "timestampField", "@timestamp")
	messageField := stringConfig(config, "messageField", "message")
	severityField := stringConfig(config, "severityField", "level")
	serviceField := stringConfig(config, "serviceField", "service")
	workloadField := stringConfig(config, "workloadField", "workload")
	namespaceField := stringConfig(config, "namespaceField", "namespace")
	clusterField := stringConfig(config, "clusterField", "cluster")
	podField := stringConfig(config, "podField", "pod")
	containerField := stringConfig(config, "containerField", "container")

	searchURL := strings.TrimRight(strings.TrimSpace(endpoint), "/") + "/" + url.PathEscape(strings.TrimSpace(index)) + "/_search"
	body := buildESSearchBody(query, cursor, timestampField, messageField, clusterField, namespaceField, serviceField, workloadField, podField, containerField, providerFetchLimit(query, cursor))
	payload, err := json.Marshal(body)
	if err != nil {
		return SearchResult{}, fmt.Errorf("marshal es search body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, searchURL, bytes.NewReader(payload))
	if err != nil {
		return SearchResult{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	applyProviderHeaders(req, config)
	resp, err := d.http.Do(req)
	if err != nil {
		return SearchResult{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return SearchResult{}, fmt.Errorf("es search failed with status %d", resp.StatusCode)
	}
	var payloadResp struct {
		Hits struct {
			Hits []struct {
				Source map[string]any `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payloadResp); err != nil {
		return SearchResult{ErrorKind: "decode_failed"}, fmt.Errorf("decode es search response: %w", err)
	}
	records := make([]Record, 0, len(payloadResp.Hits.Hits))
	for _, item := range payloadResp.Hits.Hits {
		record := Record{
			Timestamp:  timeValue(item.Source, timestampField),
			Severity:   normalizeLogSeverity(nestedString(item.Source, severityField)),
			Message:    nestedString(item.Source, messageField),
			Service:    nestedString(item.Source, serviceField),
			Workload:   nestedString(item.Source, workloadField),
			Namespace:  nestedString(item.Source, namespaceField),
			ClusterID:  nestedString(item.Source, clusterField),
			Pod:        nestedString(item.Source, podField),
			Container:  nestedString(item.Source, containerField),
			Attributes: item.Source,
		}
		if record.Severity == "" || record.Severity == "info" {
			record.Severity = severityFromMessage(record.Message)
		}
		records = append(records, record)
	}
	records = filterSeverities(records, query.Severities)
	records, nextPageToken, truncated := paginateRecords(records, query, cursor)
	return SearchResult{
		SourceID:      sourceID,
		Records:       records,
		NextPageToken: nextPageToken,
		Truncated:     truncated,
		QueryCost: map[string]any{
			"backendType": "es",
			"limit":       query.Limit,
			"recordCount": len(records),
		},
	}, nil
}

func buildESSearchBody(query SearchQuery, cursor timestampCursor, timestampField, messageField, clusterField, namespaceField, serviceField, workloadField, podField, containerField string, limit int) map[string]any {
	filters := make([]map[string]any, 0)
	if !query.TimeFrom.IsZero() || !query.TimeTo.IsZero() || !cursor.Timestamp.IsZero() {
		rangeBody := map[string]any{}
		if !query.TimeFrom.IsZero() {
			rangeBody["gte"] = query.TimeFrom.Format(time.RFC3339)
		}
		if !query.TimeTo.IsZero() {
			rangeBody["lte"] = query.TimeTo.Format(time.RFC3339)
		}
		if !cursor.Timestamp.IsZero() {
			if query.Direction == "forward" {
				rangeBody["gte"] = cursor.Timestamp.Format(time.RFC3339Nano)
			} else {
				rangeBody["lte"] = cursor.Timestamp.Format(time.RFC3339Nano)
			}
		}
		filters = append(filters, map[string]any{"range": map[string]any{timestampField: rangeBody}})
	}
	for field, value := range map[string]string{
		clusterField:   query.Scope.ClusterID,
		namespaceField: query.Scope.Namespace,
		serviceField:   query.Scope.Service,
		workloadField:  query.Scope.Workload,
		podField:       query.Scope.Pod,
		containerField: query.Scope.Container,
	} {
		if strings.TrimSpace(field) == "" || strings.TrimSpace(value) == "" {
			continue
		}
		filters = append(filters, map[string]any{"term": map[string]any{field: value}})
	}
	should := make([]map[string]any, 0)
	for _, term := range query.Terms {
		should = append(should, map[string]any{
			"simple_query_string": map[string]any{
				"query":  term,
				"fields": []string{messageField},
			},
		})
	}
	boolQuery := map[string]any{
		"filter": filters,
	}
	if len(should) > 0 {
		boolQuery["should"] = should
		boolQuery["minimum_should_match"] = 1
	}
	order := "desc"
	if query.Direction == "forward" {
		order = "asc"
	}
	return map[string]any{
		"size": limit,
		"sort": []map[string]any{{timestampField: map[string]any{"order": order}}},
		"query": map[string]any{
			"bool": boolQuery,
		},
	}
}

func summarizeSignatures(records []Record) []Signature {
	type aggregate struct {
		count    int
		sample   string
		severity string
	}
	buckets := make(map[string]*aggregate)
	for _, item := range records {
		signature := normalizeSignature(item.Message)
		if signature == "" {
			continue
		}
		current := buckets[signature]
		if current == nil {
			current = &aggregate{sample: item.Message, severity: item.Severity}
			buckets[signature] = current
		}
		current.count++
	}
	signatures := make([]Signature, 0, len(buckets))
	for signature, current := range buckets {
		signatures = append(signatures, Signature{
			Signature: signature,
			Count:     current.count,
			Sample:    current.sample,
			Severity:  current.severity,
		})
	}
	sort.Slice(signatures, func(i, j int) bool {
		return signatures[i].Count > signatures[j].Count
	})
	if len(signatures) > 5 {
		signatures = signatures[:5]
	}
	return signatures
}

func normalizeSignature(message string) string {
	message = strings.TrimSpace(message)
	if message == "" {
		return ""
	}
	lines := strings.Split(message, "\n")
	signature := strings.TrimSpace(lines[0])
	if len(signature) > 160 {
		signature = signature[:160]
	}
	return signature
}

func nestedString(source map[string]any, path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	current := any(source)
	for _, part := range strings.Split(path, ".") {
		valueMap, ok := current.(map[string]any)
		if !ok {
			return ""
		}
		current = valueMap[part]
	}
	return fmt.Sprint(current)
}

func timeValue(source map[string]any, path string) time.Time {
	value := nestedString(source, path)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func stringConfig(config map[string]any, key, fallback string) string {
	if value, _ := config[key].(string); strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return fallback
}

func intConfig(config map[string]any, key string, fallback int) int {
	switch value := config[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return fallback
	}
}

func uniqueStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
