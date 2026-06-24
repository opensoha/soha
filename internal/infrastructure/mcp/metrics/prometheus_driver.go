package metrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
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
		points, latest, err := d.queryRangeSeries(ctx, endpoint, stringValue(config["bearerToken"], ""), definition.Query, query.TimeFrom, query.TimeTo, query.Step)
		if err != nil {
			return nil, nil, err
		}
		series = append(series, Series{
			Key:    definition.Key,
			Label:  definition.Label,
			Unit:   definition.Unit,
			Points: points,
			Latest: latest,
		})
	}
	return series, map[string]any{
		"backendType": "prometheus",
		"sourceId":    sourceID,
		"queryCount":  len(selected),
	}, nil
}

type metricDefinition struct {
	Key   string
	Label string
	Unit  string
	Query string
}

func metricDefinitions(scope Scope, clusterLabel string) []metricDefinition {
	filter := []string{}
	if scope.Namespace != "" {
		filter = append(filter, fmt.Sprintf(`namespace="%s"`, scope.Namespace))
	}
	if scope.Workload != "" {
		filter = append(filter, fmt.Sprintf(`pod=~"%s-.*"`, regexpEscape(scope.Workload)))
	}
	if scope.ClusterID != "" && clusterLabel != "" {
		filter = append(filter, fmt.Sprintf(`%s="%s"`, clusterLabel, scope.ClusterID))
	}
	selector := strings.Join(filter, ",")
	if selector != "" {
		selector = "{" + selector + "}"
	} else {
		selector = "{}"
	}
	return []metricDefinition{
		{Key: "cpu_usage", Label: "CPU Usage", Unit: "cores", Query: fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total%s[5m]))`, selector)},
		{Key: "memory_usage", Label: "Memory Usage", Unit: "bytes", Query: fmt.Sprintf(`sum(container_memory_working_set_bytes%s)`, selector)},
		{Key: "restart_rate", Label: "Restart Rate", Unit: "count", Query: fmt.Sprintf(`sum(increase(kube_pod_container_status_restarts_total%s[15m]))`, selector)},
		{Key: "error_rate", Label: "Error Rate", Unit: "ratio", Query: fmt.Sprintf(`sum(rate(http_requests_total%s[5m]))`, selector)},
		{Key: "latency_p95", Label: "Latency P95", Unit: "seconds", Query: fmt.Sprintf(`histogram_quantile(0.95, sum(rate(http_request_duration_seconds_bucket%s[5m])) by (le))`, selector)},
	}
}

func (d prometheusDriver) queryRangeSeries(ctx context.Context, endpoint, bearerToken, query string, timeFrom, timeTo time.Time, step time.Duration) ([]Point, float64, error) {
	queryURL, err := url.Parse(strings.TrimRight(strings.TrimSpace(endpoint), "/") + "/api/v1/query_range")
	if err != nil {
		return nil, 0, err
	}
	params := queryURL.Query()
	params.Set("query", query)
	params.Set("start", strconv.FormatInt(timeFrom.Unix(), 10))
	params.Set("end", strconv.FormatInt(timeTo.Unix(), 10))
	params.Set("step", strconv.Itoa(int(step.Seconds())))
	queryURL.RawQuery = params.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, queryURL.String(), nil)
	if err != nil {
		return nil, 0, err
	}
	if strings.TrimSpace(bearerToken) != "" {
		req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(bearerToken))
	}

	resp, err := d.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, 0, fmt.Errorf("prometheus query_range failed with status %d", resp.StatusCode)
	}
	var payload struct {
		Status string `json:"status"`
		Data   struct {
			Result []struct {
				Values [][]any `json:"values"`
			} `json:"result"`
		} `json:"data"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, 0, err
	}
	if payload.Error != "" {
		return nil, 0, errors.New(payload.Error)
	}
	points := make([]Point, 0)
	latest := 0.0
	for _, result := range payload.Data.Result {
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
	}
	return points, latest, nil
}

func asFloat(value any) (float64, bool) {
	switch current := value.(type) {
	case float64:
		return current, true
	case string:
		number, err := strconv.ParseFloat(current, 64)
		return number, err == nil
	case json.Number:
		number, err := current.Float64()
		return number, err == nil
	default:
		return 0, false
	}
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

func regexpEscape(value string) string {
	replacer := strings.NewReplacer(".", "\\.", "-", "\\-", "_", "\\_")
	return replacer.Replace(value)
}
