package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	apiresponse "github.com/opensoha/soha-agent/internal/api/response"
	contractresource "github.com/opensoha/soha-contracts/resource"
)

func registerPrometheusRoutes(platform *gin.RouterGroup, cfg cfgpkg.PrometheusConfig) {
	platform.POST("/metrics/prometheus/query", func(c *gin.Context) {
		var query contractresource.PrometheusQuery
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
		if c.ShouldBindJSON(&query) != nil || len(query.Query) == 0 || len(query.Query) > 16384 || (query.Kind != "instant" && query.Kind != "range") {
			apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "a bounded Prometheus query is required")
			return
		}
		endpoint := strings.TrimRight(cfg.BaseURL, "/")
		u, err := url.Parse(endpoint)
		if endpoint == "" || err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" || strings.TrimRight(query.Endpoint, "/") != endpoint {
			apiresponse.Error(c, http.StatusConflict, "prometheus_configuration_mismatch", "apply the Agent installation manifest with the saved Prometheus configuration")
			return
		}
		params := url.Values{"query": []string{query.Query}}
		path := "/api/v1/query"
		if query.Kind == "range" {
			if query.Start <= 0 || query.End <= query.Start || query.End-query.Start > 24*60*60 || query.Step < 1 || (query.End-query.Start)/query.Step > 11000 {
				apiresponse.Error(c, http.StatusBadRequest, "invalid_argument", "Prometheus range exceeds query limits")
				return
			}
			path = "/api/v1/query_range"
			params.Set("start", strconv.FormatInt(query.Start, 10))
			params.Set("end", strconv.FormatInt(query.End, 10))
			params.Set("step", strconv.FormatInt(query.Step, 10))
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+path+"?"+params.Encode(), nil)
		if err != nil {
			apiresponse.Error(c, 400, "invalid_argument", "invalid Prometheus endpoint")
			return
		}
		if cfg.BearerToken != "" {
			req.Header.Set("Authorization", "Bearer "+cfg.BearerToken)
		}
		client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
		response, err := client.Do(req)
		if err != nil {
			apiresponse.Error(c, 502, "prometheus_unavailable", "Agent could not reach Prometheus")
			return
		}
		defer func() { _ = response.Body.Close() }()
		if response.StatusCode != http.StatusOK {
			apiresponse.Error(c, 502, "prometheus_unavailable", "Prometheus rejected the query")
			return
		}
		data, err := io.ReadAll(io.LimitReader(response.Body, (8<<20)+1))
		if err != nil || len(data) > 8<<20 || !json.Valid(data) {
			apiresponse.Error(c, 502, "prometheus_invalid_response", "Prometheus returned an invalid or oversized response")
			return
		}
		apiresponse.Item(c, http.StatusOK, json.RawMessage(data))
	})
}
