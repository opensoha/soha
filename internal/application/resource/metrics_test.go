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

	domainsettings "github.com/kubecrux/kubecrux/internal/domain/settings"
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
