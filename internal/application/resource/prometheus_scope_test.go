package resource

import (
	"context"
	"fmt"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestMetricAliasUsesExactFilterAndKeepsSohaNodeIdentity(t *testing.T) {
	alias := `middleware"prod\east`
	settings := domainsettings.PrometheusSettings{ClusterLabel: "cluster", ClusterLabelValue: alias}
	matcher := "cluster=" + strconv.Quote(alias)
	for _, view := range []string{"overview", "cpu", "memory", "pods", "conntrack"} {
		for _, d := range monitoringDefinitions(view, prometheusClusterValue(settings, "real-id"), settings.ClusterLabel) {
			if !strings.Contains(d.query, matcher) {
				t.Fatalf("%s query lost quoted alias: %s", view, d.query)
			}
		}
	}
	for _, d := range buildPodMetricDefinitions("team", "app", prometheusClusterValue(settings, "real-id"), settings.ClusterLabel) {
		if !strings.Contains(d.Query, matcher) {
			t.Fatal("Pod alias absent")
		}
	}
	for _, d := range buildPodSetMetricDefinitions("team", []string{"app"}, prometheusClusterValue(settings, "real-id"), settings.ClusterLabel) {
		if !strings.Contains(d.Query, matcher) {
			t.Fatal("controller alias absent")
		}
	}
	if prometheusClusterValue(domainsettings.PrometheusSettings{}, "real-id") != "real-id" {
		t.Fatal("legacy default changed")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Query().Get("query"), `cluster="middleware-prod"`) {
			t.Error("scope missing")
		}
		_, _ = fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"cluster":"middleware-prod","node":"same-name"},"values":[[1000,"1"]]},{"metric":{"cluster":"other-cluster","node":"same-name"},"values":[[1000,"99"]]}]}}`)
	}))
	defer server.Close()
	inventory := &Inventory{metricsSupport: &metricsSupport{httpClient: server.Client()}}
	ctx := context.WithValue(context.Background(), prometheusClusterKey{}, "real-id")
	result := inventory.queryMonitoringRange(ctx, domainsettings.PrometheusSettings{BaseURL: server.URL, ClusterLabel: "cluster", ClusterLabelValue: "middleware-prod"}, `node_memory_MemTotal_bytes{cluster="middleware-prod"}`, monitoringWindow{from: time.Unix(1000, 0), to: time.Unix(1060, 0), step: 60})
	if len(result.series) != 1 || result.series[0].Metric["cluster"] != "middleware-prod" || result.reason != "cluster_identity_mismatch" {
		t.Fatalf("cross-cluster alias leak: %+v", result)
	}
	if ctx.Value(prometheusClusterKey{}) != "real-id" {
		t.Fatal("transport target became metric alias")
	}
}

func TestPodUsageNeverRetriesSharedSourceWithoutClusterMatcher(t *testing.T) {
	for _, alias := range []string{"", "wrong-alias"} {
		t.Run(alias, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				value := alias
				if value == "" {
					value = "real-id"
				}
				if !strings.Contains(r.URL.Query().Get("query"), "cluster="+strconv.Quote(value)) {
					t.Error("unscoped retry escaped explicit alias")
				}
				_, _ = fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[]}}`)
			}))
			defer server.Close()
			s := &metricsSupport{httpClient: server.Client()}
			_, err := s.listPodUsageValues(context.Background(), domainsettings.PrometheusSettings{Enabled: true, BaseURL: server.URL, ClusterLabel: "cluster", ClusterLabelValue: alias}, "real-id", []podIdentity{{Name: "same-name", Namespace: "team"}})
			if err != nil {
				t.Fatal(err)
			}
			if calls.Load() != 2 {
				t.Fatalf("unexpected fallback requests: %d", calls.Load())
			}
		})
	}
}

func TestMetricAliasResolverRejectsMissingLabelName(t *testing.T) {
	s := &metricsSupport{resolver: stubConnectionResolver{connection: domaincluster.Connection{Metadata: map[string]any{"prometheus_url": "http://metrics.invalid", "prometheus_cluster_label_value": "alias"}}}}
	if settings := s.resolveClusterPrometheusSettings(context.Background(), "real-id"); settings.Enabled {
		t.Fatal("invalid file configuration became an unscoped query")
	}
}

func TestSharedPodHistoryKeepsClusterMatcherOnEmptyResults(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/api/v1/query_range" || !strings.Contains(r.URL.Query().Get("query"), `cluster="real-id"`) {
			t.Errorf("unexpected unscoped query: %s", r.URL)
		}
		_, _ = fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[]}}`)
	}))
	defer server.Close()
	resolver := stubConnectionResolver{connection: domaincluster.Connection{
		Summary:  domaincluster.Summary{ID: "real-id"},
		Metadata: map[string]any{"prometheus_url": server.URL, "prometheus_cluster_label": "cluster"},
	}}
	access := &resourceAccess{resolver: resolver, authorizer: &recordingCreateAuthorizer{allow: true}, audit: discardAuditRecorder{}}
	workload := &Workloads{resourceAccess: access, metricsSupport: &metricsSupport{resourceAccess: access, resolver: resolver, httpClient: server.Client()}}
	view, err := workload.GetPodMetrics(t.Context(), domainidentity.Principal{}, "real-id", "team", "app", 60, 60)
	if err != nil || !view.Configured || len(view.Series) != 0 || int(calls.Load()) != len(buildPodMetricDefinitions("team", "app", "real-id", "cluster")) {
		t.Fatalf("unexpected history: %+v, %v, query count=%d", view, err, calls.Load())
	}
}
