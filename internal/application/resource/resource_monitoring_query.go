package resource

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	domainsettings "github.com/opensoha/soha/internal/domain/settings"
)

type monitoringRawSeries struct {
	Metric map[string]string `json:"metric"`
	Values [][]any           `json:"values"`
}
type monitoringResult struct {
	series          []monitoringRawSeries
	quality, reason string
	age             []monitoringRawSeries
}

func (i *Inventory) queryMonitoringDefinitions(ctx context.Context, clusterID string, settings domainsettings.PrometheusSettings, definitions []monitoringDefinition, window monitoringWindow) map[string]monitoringResult {
	results := map[string]monitoringResult{}
	definitions = append(append([]monitoringDefinition(nil), definitions...), monitoringDefinition{key: "node.identity", query: "node_uname_info{" + settings.ClusterLabel + "=" + strconv.Quote(clusterID) + "}"})
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	// Each response is at most 1 MiB; at most 20 template requests are issued per view.
	for _, definition := range definitions {
		wg.Add(1)
		go func(definition monitoringDefinition) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				mu.Lock()
				results[definition.key] = monitoringResult{quality: "error", reason: "query_timeout"}
				mu.Unlock()
				return
			}
			result := i.queryMonitoringRange(ctx, settings, definition.query, window)
			if definition.ageQuery != "" && result.quality == "valid" {
				age := i.queryMonitoringRange(ctx, settings, definition.ageQuery, window)
				result.age = age.series
				if age.quality != "valid" {
					result.quality = "partial"
					result.reason = "freshness_unverified"
				}
			}
			mu.Lock()
			results[definition.key] = result
			mu.Unlock()
		}(definition)
	}
	wg.Wait()
	return results
}

func (i *Inventory) queryMonitoringRange(ctx context.Context, settings domainsettings.PrometheusSettings, query string, window monitoringWindow) monitoringResult {
	result := monitoringResult{quality: "valid"}
	endpoint, err := url.Parse(strings.TrimRight(settings.BaseURL, "/") + "/api/v1/query_range")
	if err != nil {
		return monitoringResult{quality: "error", reason: "invalid_endpoint"}
	}
	values := endpoint.Query()
	values.Set("query", query)
	values.Set("start", strconv.FormatInt(window.from.Unix(), 10))
	values.Set("end", strconv.FormatInt(window.to.Unix(), 10))
	values.Set("step", strconv.Itoa(window.step))
	endpoint.RawQuery = values.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return monitoringResult{quality: "error", reason: "invalid_request"}
	}
	if settings.BearerToken != "" {
		request.Header.Set("Authorization", "Bearer "+settings.BearerToken)
	}
	response, err := i.doPrometheusRequest(ctx, request)
	if err != nil {
		return monitoringResult{quality: "error", reason: "query_unavailable"}
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode == 401 || response.StatusCode == 403 {
		return monitoringResult{quality: "denied", reason: "prometheus_denied"}
	}
	if response.StatusCode != http.StatusOK {
		return monitoringResult{quality: "error", reason: "query_failed"}
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (1<<20)+1))
	if err != nil {
		return monitoringResult{quality: "error", reason: "invalid_response"}
	}
	if len(raw) > 1<<20 {
		return monitoringResult{quality: "partial", reason: "response_budget"}
	}
	var payload struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string                `json:"resultType"`
			Result     []monitoringRawSeries `json:"result"`
		} `json:"data"`
	}
	if json.Unmarshal(raw, &payload) != nil || payload.Status != "success" || payload.Data.ResultType != "matrix" {
		return monitoringResult{quality: "error", reason: "invalid_response"}
	}
	if len(payload.Data.Result) > 4096 {
		payload.Data.Result = payload.Data.Result[:4096]
		result.quality = "partial"
		result.reason = "series_budget"
	}
	expectedCluster, _ := ctx.Value(prometheusClusterKey{}).(string)
	for _, series := range payload.Data.Result {
		if series.Metric[settings.ClusterLabel] != expectedCluster {
			result.quality = "partial"
			result.reason = "cluster_identity_mismatch"
			continue
		}
		result.series = append(result.series, series)
	}
	if len(result.series) == 0 && result.quality == "valid" {
		result.quality = "not_collected"
		result.reason = "metric_not_collected"
	}
	return result
}
