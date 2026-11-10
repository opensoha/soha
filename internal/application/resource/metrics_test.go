package resource

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
)

func TestListPodUsageValuesSupportsDedicatedEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/api/v1/query") {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		query := r.URL.Query().Get("query")
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
			Enabled: true,
			BaseURL: server.URL,
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

func TestPodMetricDefinitionsKeepNetworkAtPodScope(t *testing.T) {
	t.Parallel()

	definitionSets := [][]metricDefinition{
		buildPodMetricDefinitions("team-a", "api-0", "cluster-a", "cluster"),
		buildPodSetMetricDefinitions("team-a", []string{"api-0", "api-1"}, "cluster-a", "cluster"),
	}
	for _, definitions := range definitionSets {
		byKey := make(map[string]string, len(definitions))
		for _, definition := range definitions {
			byKey[definition.Key] = definition.Query
		}
		for _, key := range []string{"network_rx", "network_tx"} {
			if strings.Contains(byKey[key], `container!=`) {
				t.Fatalf("%s query excludes pod-level network samples: %s", key, byKey[key])
			}
		}
		if !strings.Contains(byKey["cpu"], `container!=""`) {
			t.Fatalf("cpu query includes pod sandbox samples: %s", byKey["cpu"])
		}
	}
}

func TestPodMetricDefinitionsOptionalDiagnosticsRespectTargetScope(t *testing.T) {
	t.Parallel()
	definitions := buildPodMetricDefinitions("team-a", "api-0", "cluster-a", "cluster")
	byKey := make(map[string]metricDefinition, len(definitions))
	for _, definition := range definitions {
		byKey[definition.Key] = definition
	}
	for _, test := range []struct{ key, metric, unit string }{
		{"filesystem", "container_fs_usage_bytes", "bytes"},
		{"restarts", "kube_pod_container_status_restarts_total", "count"},
		{"cpu_throttled", "container_cpu_cfs_throttled_periods_total", "%"},
		{"oom_events", "container_oom_events_total", "count"},
	} {
		t.Run(test.key, func(t *testing.T) {
			definition, ok := byKey[test.key]
			if !ok || definition.Unit != test.unit || !strings.Contains(definition.Query, test.metric) {
				t.Fatalf("missing diagnostic definition %s: %#v", test.key, definition)
			}
			for _, matcher := range []string{`namespace="team-a"`, `pod="api-0"`, `cluster="cluster-a"`} {
				if !strings.Contains(definition.Query, matcher) {
					t.Fatalf("%s escapes target scope: %s", test.key, definition.Query)
				}
			}
			if test.key == "cpu_throttled" && !strings.Contains(definition.Query, "> 0)") {
				t.Fatalf("CPU throttling must omit undefined ratios: %s", definition.Query)
			}
			if strings.Contains(definition.Query, "vector(0)") || strings.Contains(definition.Query, "increase(") {
				t.Fatalf("%s must preserve missing and cumulative semantics: %s", test.key, definition.Query)
			}
		})
	}
}
