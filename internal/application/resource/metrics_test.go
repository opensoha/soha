package resource

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
)

func TestQueryMetricSeriesWithFallbackAllowsUnambiguousFallback(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/query_range"):
			query := r.URL.Query().Get("query")
			if strings.Contains(query, `cluster="cluster-a"`) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"status": "success",
					"data": map[string]any{
						"resultType": "matrix",
						"result":     []any{},
					},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"resultType": "matrix",
					"result": []any{
						map[string]any{
							"values": [][]any{
								{float64(1713225600), "0.25"},
								{float64(1713225660), "0.5"},
							},
						},
					},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/query"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"resultType": "vector",
					"result": []any{
						map[string]any{
							"metric": map[string]string{"pod": "api"},
							"value":  []any{float64(1713225600), "1"},
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	service := &metricsSupport{httpClient: server.Client()}
	series, firstError := service.queryMetricSeriesWithFallback(
		context.Background(),
		server.URL,
		"",
		[]metricDefinition{
			{Key: "cpu", Label: "CPU Usage", Unit: "cores", Query: `sum(rate(container_cpu_usage_seconds_total{namespace="team-a",pod="api",cluster="cluster-a"}[5m]))`},
		},
		[]metricDefinition{
			{Key: "cpu", Label: "CPU Usage", Unit: "cores", Query: `sum(rate(container_cpu_usage_seconds_total{namespace="team-a",pod="api"}[5m]))`},
		},
		"team-a",
		[]string{"api"},
		time.Hour,
		time.Minute,
	)

	if firstError != "" {
		t.Fatalf("firstError = %q, want empty", firstError)
	}
	if len(series) != 1 {
		t.Fatalf("len(series) = %d, want 1", len(series))
	}
	if series[0].Latest != 0.5 {
		t.Fatalf("Latest = %v, want 0.5", series[0].Latest)
	}
}

func TestQueryMetricSeriesWithFallbackRejectsAmbiguousFallback(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/query_range"):
			query := r.URL.Query().Get("query")
			if !strings.Contains(query, `cluster="cluster-a"`) {
				t.Fatalf("unexpected unscoped fallback query %q", query)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"resultType": "matrix",
					"result":     []any{},
				},
			})
		case strings.HasPrefix(r.URL.Path, "/api/v1/query"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"resultType": "vector",
					"result": []any{
						map[string]any{
							"metric": map[string]string{"pod": "api"},
							"value":  []any{float64(1713225600), "2"},
						},
					},
				},
			})
		default:
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
	}))
	defer server.Close()

	service := &metricsSupport{httpClient: server.Client()}
	series, firstError := service.queryMetricSeriesWithFallback(
		context.Background(),
		server.URL,
		"",
		[]metricDefinition{
			{Key: "cpu", Label: "CPU Usage", Unit: "cores", Query: `sum(rate(container_cpu_usage_seconds_total{namespace="team-a",pod="api",cluster="cluster-a"}[5m]))`},
		},
		[]metricDefinition{
			{Key: "cpu", Label: "CPU Usage", Unit: "cores", Query: `sum(rate(container_cpu_usage_seconds_total{namespace="team-a",pod="api"}[5m]))`},
		},
		"team-a",
		[]string{"api"},
		time.Hour,
		time.Minute,
	)

	if len(series) != 0 {
		t.Fatalf("len(series) = %d, want 0", len(series))
	}
	if !strings.Contains(firstError, "ambiguous") {
		t.Fatalf("firstError = %q, want ambiguity message", firstError)
	}
}

func TestListPodUsageValuesFallsBackWithoutClusterMatcher(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/query") {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		query := r.URL.Query().Get("query")
		if strings.Contains(query, `cluster="cluster-a"`) {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"status": "success",
				"data": map[string]any{
					"resultType": "vector",
					"result":     []any{},
				},
			})
			return
		}

		result := []map[string]any(nil)
		switch {
		case strings.Contains(query, "container_cpu_usage_seconds_total"):
			result = []map[string]any{
				{
					"metric": map[string]string{"pod": "api"},
					"value":  []any{float64(1713225600), "0.25"},
				},
			}
		case strings.Contains(query, "container_memory_working_set_bytes"):
			result = []map[string]any{
				{
					"metric": map[string]string{"pod": "api"},
					"value":  []any{float64(1713225600), "1048576"},
				},
			}
		}

		_ = json.NewEncoder(w).Encode(map[string]any{
			"status": "success",
			"data": map[string]any{
				"resultType": "vector",
				"result":     result,
			},
		})
	}))
	defer server.Close()

	service := &metricsSupport{httpClient: server.Client()}
	values, err := service.listPodUsageValues(
		context.Background(),
		domainsettings.PrometheusSettings{
			Enabled:      true,
			BaseURL:      server.URL,
			ClusterLabel: "cluster",
		},
		"cluster-a",
		[]podIdentity{{Name: "api", Namespace: "team-a"}},
	)
	if err != nil {
		t.Fatalf("listPodUsageValues() error = %v, want nil", err)
	}

	value, ok := values["team-a/api"]
	if !ok {
		t.Fatalf("values missing team-a/api: %+v", values)
	}
	if value.CPUCores != 0.25 {
		t.Fatalf("CPUCores = %v, want 0.25", value.CPUCores)
	}
	if value.MemoryBytes != 1048576 {
		t.Fatalf("MemoryBytes = %v, want 1048576", value.MemoryBytes)
	}
}

type stubConnectionResolver struct {
	connection domaincluster.Connection
}

func (s stubConnectionResolver) GetConnection(context.Context, string) (domaincluster.Connection, error) {
	return s.connection, nil
}

func TestResolveClusterPrometheusSettingsUsesClusterMetadata(t *testing.T) {
	t.Parallel()

	service := &metricsSupport{
		resolver: stubConnectionResolver{
			connection: domaincluster.Connection{
				Metadata: map[string]any{
					"prometheus_url":           "http://cluster-prometheus:9090",
					"prometheus_bearer_token":  "cluster-token",
					"prometheus_cluster_label": "k8s_cluster",
					"grafana_base_url":         "http://cluster-grafana:3000",
				},
			},
		},
	}

	settings := service.resolveClusterPrometheusSettings(context.Background(), "cluster-a")
	if settings.BaseURL != "http://cluster-prometheus:9090" {
		t.Fatalf("BaseURL = %q, want cluster override value", settings.BaseURL)
	}
	if settings.BearerToken != "cluster-token" {
		t.Fatalf("BearerToken = %q, want cluster override value", settings.BearerToken)
	}
	if settings.ClusterLabel != "k8s_cluster" {
		t.Fatalf("ClusterLabel = %q, want cluster override value", settings.ClusterLabel)
	}
	if settings.GrafanaBaseURL != "http://cluster-grafana:3000" {
		t.Fatalf("GrafanaBaseURL = %q, want cluster override value", settings.GrafanaBaseURL)
	}
	if settings.DefaultRangeMinutes != 60 {
		t.Fatalf("DefaultRangeMinutes = %d, want 60", settings.DefaultRangeMinutes)
	}
	if settings.StepSeconds != 60 {
		t.Fatalf("StepSeconds = %d, want 60", settings.StepSeconds)
	}
}

func TestResolveClusterPrometheusSettingsDoesNotUseGlobalFallback(t *testing.T) {
	t.Parallel()

	settings := (&metricsSupport{}).resolveClusterPrometheusSettings(context.Background(), "cluster-a")
	if settings.Enabled || settings.BaseURL != "" || settings.BearerToken != "" || settings.GrafanaBaseURL != "" {
		t.Fatalf("unexpected global Prometheus fallback: %#v", settings)
	}
	if settings.DefaultRangeMinutes != 60 || settings.StepSeconds != 60 {
		t.Fatalf("query defaults = range %d step %d, want 60/60", settings.DefaultRangeMinutes, settings.StepSeconds)
	}
}
