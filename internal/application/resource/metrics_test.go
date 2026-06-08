package resource

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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

	service := &Service{httpClient: server.Client()}
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

	service := &Service{httpClient: server.Client()}
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

	service := &Service{httpClient: server.Client()}
	values, err := service.listPodUsageValues(
		context.Background(),
		domainsettings.PrometheusSettings{
			Enabled:      true,
			BaseURL:      server.URL,
			ClusterLabel: "cluster",
		},
		"cluster-a",
		[]corev1.Pod{{ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "team-a"}}},
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

type stubMonitoringSettingsResolver struct {
	settings domainsettings.MonitoringSettings
}

func (s stubMonitoringSettingsResolver) ResolveMonitoringSettings(context.Context) (domainsettings.MonitoringSettings, error) {
	return s.settings, nil
}

type stubConnectionResolver struct {
	connection domaincluster.Connection
}

func (s stubConnectionResolver) GetConnection(context.Context, string) (domaincluster.Connection, error) {
	return s.connection, nil
}

func TestResolveClusterPrometheusSettingsPrefersClusterOverride(t *testing.T) {
	t.Parallel()

	service := &Service{
		settings: stubMonitoringSettingsResolver{
			settings: domainsettings.MonitoringSettings{
				Prometheus: domainsettings.PrometheusSettings{
					Enabled:             true,
					BaseURL:             "http://global-prometheus:9090",
					BearerToken:         "global-token",
					DefaultRangeMinutes: 30,
					StepSeconds:         15,
					ClusterLabel:        "cluster",
					GrafanaBaseURL:      "http://global-grafana:3000",
				},
			},
		},
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

	settings, err := service.resolveClusterPrometheusSettings(context.Background(), "cluster-a")
	if err != nil {
		t.Fatalf("resolveClusterPrometheusSettings() error = %v", err)
	}
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
	if settings.DefaultRangeMinutes != 30 {
		t.Fatalf("DefaultRangeMinutes = %d, want 30", settings.DefaultRangeMinutes)
	}
	if settings.StepSeconds != 15 {
		t.Fatalf("StepSeconds = %d, want 15", settings.StepSeconds)
	}
}

func TestResolveClusterPrometheusSettingsFallsBackToGlobalSettings(t *testing.T) {
	t.Parallel()

	service := &Service{
		settings: stubMonitoringSettingsResolver{
			settings: domainsettings.MonitoringSettings{
				Prometheus: domainsettings.PrometheusSettings{
					Enabled:             true,
					BaseURL:             "http://global-prometheus:9090",
					BearerToken:         "global-token",
					DefaultRangeMinutes: 30,
					StepSeconds:         15,
					ClusterLabel:        "cluster",
					GrafanaBaseURL:      "http://global-grafana:3000",
				},
			},
		},
	}

	settings, err := service.resolveClusterPrometheusSettings(context.Background(), "cluster-a")
	if err != nil {
		t.Fatalf("resolveClusterPrometheusSettings() error = %v", err)
	}
	if settings.BaseURL != "http://global-prometheus:9090" {
		t.Fatalf("BaseURL = %q, want global settings value", settings.BaseURL)
	}
	if settings.BearerToken != "global-token" {
		t.Fatalf("BearerToken = %q, want global settings value", settings.BearerToken)
	}
	if settings.ClusterLabel != "cluster" {
		t.Fatalf("ClusterLabel = %q, want global settings value", settings.ClusterLabel)
	}
	if settings.GrafanaBaseURL != "http://global-grafana:3000" {
		t.Fatalf("GrafanaBaseURL = %q, want global settings value", settings.GrafanaBaseURL)
	}
}
