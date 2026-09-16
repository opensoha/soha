package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type prometheusDriver struct {
	http *http.Client
}

func newPrometheusDriver() Driver {
	return prometheusDriver{http: &http.Client{Timeout: 8 * time.Second}}
}

func (prometheusDriver) BackendType() string {
	return "prometheus"
}

func (prometheusDriver) ValidateConfig(config map[string]any) error {
	if config == nil {
		return fmt.Errorf("prometheus config is required")
	}
	endpoint, _ := config["endpoint"].(string)
	if strings.TrimSpace(endpoint) == "" {
		return fmt.Errorf("prometheus endpoint is required")
	}
	return nil
}

func (d prometheusDriver) RangeQuery(ctx context.Context, sourceID string, config map[string]any, query RangeQuery) ([]Series, map[string]any, error) {
	if err := d.ValidateConfig(config); err != nil {
		return nil, nil, err
	}
	endpoint, _ := config["endpoint"].(string)
	clusterLabel := stringValue(config["clusterLabel"], "cluster")
	if !prometheusLabelName.MatchString(clusterLabel) {
		return nil, nil, fmt.Errorf("invalid Prometheus cluster label")
	}
	stepSeconds := intValue(config["stepSeconds"], 60)
	if query.Step <= 0 {
		query.Step = time.Duration(stepSeconds) * time.Second
	}
	if query.TimeTo.IsZero() {
		query.TimeTo = time.Now().UTC()
	}
	if query.TimeFrom.IsZero() {
		query.TimeFrom = query.TimeTo.Add(-60 * time.Minute)
	}
	if strings.TrimSpace(query.Expression) != "" {
		if query.RequireEvidence {
			return nil, nil, fmt.Errorf("sample evidence requires a registered metric definition")
		}
		return d.queryExpression(ctx, sourceID, endpoint, stringValue(config["bearerToken"], ""), query)
	}

	definitions := metricDefinitions(query.Scope, clusterLabel)
	var selected []metricDefinition
	for _, item := range definitions {
		if strings.TrimSpace(query.MetricKey) == "" || item.Key == strings.TrimSpace(query.MetricKey) {
			selected = append(selected, item)
		}
	}
	if len(selected) == 0 {
		return nil, nil, fmt.Errorf("unsupported metric key %s", query.MetricKey)
	}

	series := make([]Series, 0, len(selected))
	for _, definition := range selected {
		points, latest, err := d.queryRangeSeries(ctx, endpoint, stringValue(config["bearerToken"], ""), definition.Query, query.TimeFrom, query.TimeTo, query.Step, query.RequireEvidence)
		if err != nil {
			return nil, nil, err
		}
		item := Series{
			Key:      definition.Key,
			Label:    definition.Label,
			Unit:     definition.Unit,
			Points:   points,
			Latest:   latest,
			Lookback: definition.Lookback,
		}
		if query.RequireEvidence {
			item.SourceTimes, _, err = d.queryRangeSeries(ctx, endpoint, stringValue(config["bearerToken"], ""), "min(timestamp("+definition.SourceSelector+"))", query.TimeFrom, query.TimeTo, query.Step, true)
			if err != nil {
				return nil, nil, err
			}
		}
		series = append(series, item)
	}
	queryCount := len(selected)
	if query.RequireEvidence {
		queryCount *= 2
	}
	return series, map[string]any{
		"backendType": "prometheus",
		"sourceId":    sourceID,
		"queryCount":  queryCount,
	}, nil
}

func (d prometheusDriver) queryExpression(ctx context.Context, sourceID, endpoint, bearerToken string, query RangeQuery) ([]Series, map[string]any, error) {
	results, err := d.queryRangeResults(ctx, endpoint, bearerToken, strings.TrimSpace(query.Expression), query.TimeFrom, query.TimeTo, query.Step, false)
	if err != nil {
		return nil, nil, err
	}
	baseKey := strings.TrimSpace(query.MetricKey)
	if baseKey == "" {
		baseKey = "A"
	}
	series := make([]Series, 0, len(results))
	for index, result := range results {
		key := baseKey
		if len(results) > 1 {
			key = fmt.Sprintf("%s:%d", baseKey, index+1)
		}
		series = append(series, Series{
			Key: key, Label: metricSeriesLabel(query.Legend, result.Labels, key), Points: result.Points, Latest: result.Latest,
		})
	}
	return series, map[string]any{"backendType": "prometheus", "sourceId": sourceID, "queryCount": 1}, nil
}

type metricDefinition struct {
	Key            string
	Label          string
	Unit           string
	Query          string
	SourceSelector string
	Lookback       time.Duration
}

var prometheusLabelName = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

func metricDefinitions(scope Scope, clusterLabel string) []metricDefinition {
	filter := []string{}
	if scope.Namespace != "" {
		filter = append(filter, "namespace="+strconv.Quote(scope.Namespace))
	}
	if scope.Workload != "" {
		filter = append(filter, "pod=~"+strconv.Quote(regexp.QuoteMeta(scope.Workload)+"-.*"))
	}
	if scope.ClusterID != "" && clusterLabel != "" {
		filter = append(filter, clusterLabel+"="+strconv.Quote(scope.ClusterID))
	}
	if scope.Service != "" {
		filter = append(filter, "service="+strconv.Quote(scope.Service))
	}
	selector := func(metric string, extra ...string) string {
		return metric + "{" + strings.Join(append(append([]string{}, filter...), extra...), ",") + "}"
	}
	cpu, memory, restarts := selector("container_cpu_usage_seconds_total"), selector("container_memory_working_set_bytes"), selector("kube_pod_container_status_restarts_total")
	requests, errors := selector("http_requests_total", `status=~"[1-5][0-9][0-9]"`), selector("http_requests_total", `status=~"5[0-9][0-9]"`)
	latency := selector("http_request_duration_seconds_bucket")
	requestRate := "sum(rate(" + requests + "[5m]))"
	return []metricDefinition{
		{Key: "cpu_usage", Label: "CPU Usage", Unit: "cores", Query: "sum(rate(" + cpu + "[5m]))", SourceSelector: cpu, Lookback: 5 * time.Minute},
		{Key: "memory_usage", Label: "Memory Usage", Unit: "bytes", Query: "sum(" + memory + ")", SourceSelector: memory},
		{Key: "restart_rate", Label: "Restart Rate", Unit: "count", Query: "sum(increase(" + restarts + "[15m]))", SourceSelector: restarts, Lookback: 15 * time.Minute},
		{Key: "error_rate", Label: "Error Rate", Unit: "ratio", Query: "(sum(rate(" + errors + "[5m])) or (0 * " + requestRate + ")) / " + requestRate, SourceSelector: requests, Lookback: 5 * time.Minute},
		{Key: "latency_p95", Label: "Latency P95", Unit: "seconds", Query: "histogram_quantile(0.95, sum(rate(" + latency + "[5m])) by (le))", SourceSelector: latency, Lookback: 5 * time.Minute},
	}
}

func (d prometheusDriver) queryRangeSeries(ctx context.Context, endpoint, bearerToken, query string, timeFrom, timeTo time.Time, step time.Duration, requireComplete bool) ([]Point, float64, error) {
	results, err := d.queryRangeResults(ctx, endpoint, bearerToken, query, timeFrom, timeTo, step, requireComplete)
	if err != nil {
		return nil, 0, err
	}
	points := make([]Point, 0)
	latest := 0.0
	for _, result := range results {
		points = append(points, result.Points...)
		latest = result.Latest
	}
	return points, latest, nil
}

type prometheusRangeResult struct {
	Labels map[string]string
	Points []Point
	Latest float64
}

func (d prometheusDriver) queryRangeResults(ctx context.Context, endpoint, bearerToken, query string, timeFrom, timeTo time.Time, step time.Duration, requireComplete bool) ([]prometheusRangeResult, error) {
	queryURL, err := url.Parse(strings.TrimRight(strings.TrimSpace(endpoint), "/") + "/api/v1/query_range")
	if err != nil {
		return nil, err
	}
	params := queryURL.Query()
	params.Set("query", query)
	params.Set("start", strconv.FormatInt(timeFrom.Unix(), 10))
	params.Set("end", strconv.FormatInt(timeTo.Unix(), 10))
	params.Set("step", strconv.Itoa(int(step.Seconds())))
	queryURL.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, queryURL.String(), nil)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(bearerToken) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearerToken))
	}

	resp, err := d.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("prometheus query_range failed with status %d", resp.StatusCode)
	}
	var payload struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Values [][]any           `json:"values"`
			} `json:"result"`
		} `json:"data"`
		Error    string   `json:"error"`
		Warnings []string `json:"warnings"`
		Infos    []string `json:"infos"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Error != "" {
		return nil, errors.New(payload.Error)
	}
	if payload.Status != "success" || requireComplete && (len(payload.Warnings) > 0 || len(payload.Infos) > 0) {
		return nil, fmt.Errorf("prometheus response is incomplete")
	}
	results := make([]prometheusRangeResult, 0, len(payload.Data.Result))
	for _, result := range payload.Data.Result {
		points := make([]Point, 0, len(result.Values))
		latest := 0.0
		for _, value := range result.Values {
			if len(value) < 2 {
				continue
			}
			seconds, ok := asFloat(value[0])
			if !ok {
				continue
			}
			number, ok := asFloat(value[1])
			if !ok {
				continue
			}
			points = append(points, Point{
				Timestamp: time.Unix(int64(seconds), 0).UTC(),
				Value:     number,
			})
			latest = number
		}
		results = append(results, prometheusRangeResult{Labels: result.Metric, Points: points, Latest: latest})
	}
	return results, nil
}

func metricSeriesLabel(template string, labels map[string]string, fallback string) string {
	label := strings.TrimSpace(template)
	if label != "" {
		for key, value := range labels {
			label = strings.ReplaceAll(label, "{{"+key+"}}", value)
			label = strings.ReplaceAll(label, "{{ "+key+" }}", value)
		}
		if label != "" {
			return label
		}
	}
	name := strings.TrimSpace(labels["__name__"])
	keys := make([]string, 0, len(labels))
	for key := range labels {
		if key != "__name__" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", key, labels[key]))
	}
	if name == "" {
		name = fallback
	}
	if len(parts) == 0 {
		return name
	}
	return fmt.Sprintf("%s{%s}", name, strings.Join(parts, ","))
}

func asFloat(value any) (float64, bool) {
	var number float64
	var err error
	switch current := value.(type) {
	case float64:
		number = current
	case string:
		number, err = strconv.ParseFloat(current, 64)
	case json.Number:
		number, err = current.Float64()
	default:
		return 0, false
	}
	return number, err == nil && !math.IsNaN(number) && !math.IsInf(number, 0)
}

func stringValue(value any, fallback string) string {
	current, ok := value.(string)
	if !ok || strings.TrimSpace(current) == "" {
		return fallback
	}
	return strings.TrimSpace(current)
}

func intValue(value any, fallback int) int {
	switch current := value.(type) {
	case int:
		return current
	case float64:
		return int(current)
	case string:
		number, err := strconv.Atoi(strings.TrimSpace(current))
		if err == nil {
			return number
		}
	}
	return fallback
}
