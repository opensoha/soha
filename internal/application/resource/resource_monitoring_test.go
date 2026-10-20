package resource

import (
	"context"
	"errors"
	"fmt"
	domainsettings "github.com/opensoha/soha/internal/domain/settings"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	cr "github.com/opensoha/soha-contracts/resource"
	domainaccess "github.com/opensoha/soha/internal/domain/access"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestMonitoringRequiresExactViewAndClusterScope(t *testing.T) {
	connection := domaincluster.Connection{Summary: domaincluster.Summary{ID: "c"}}
	inventory := &Inventory{resourceAccess: &resourceAccess{resolver: stubConnectionResolver{connection: connection}, authorizer: exactPermissionAuthorizer{"platform.nodes.view": true, "platform.resource-monitoring.cpu.view": true}}}
	if _, err := inventory.authorizeMonitoring(context.Background(), domainidentity.Principal{}, "c", "cpu"); err != nil {
		t.Fatal(err)
	}
	if _, err := inventory.authorizeMonitoring(context.Background(), domainidentity.Principal{}, "c", "memory"); !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("sibling permission accepted: %v", err)
	}
	for _, scope := range []domainaccess.ResourceScope{{Namespaces: []string{"team"}}, {ExcludedNamespaces: []string{"private"}}, {NamespaceSelectors: []string{"team=a"}}, {LabelSelector: "team=a"}} {
		if clusterWideResourceScope(domainaccess.Decision{Allowed: true, ResourceScope: &scope}) {
			t.Fatal("namespace/resource scope widened to host metrics")
		}
	}
}
func TestMonitoringWindowAndBudgetAreBoundedAndFair(t *testing.T) {
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	_, window, err := normalizeMonitoringQuery(cr.ResourceMonitoringQuery{View: "conntrack"}, now)
	if err != nil {
		t.Fatal(err)
	}
	adjusted, count, truncated := fitMonitoringBudget(window, 500, 8)
	if truncated || count != 500 || adjusted.step <= 60 {
		t.Fatalf("default scope was not adapted fairly: %+v %d %v", adjusted, count, truncated)
	}
	points := int(adjusted.to.Sub(adjusted.from).Seconds())/adjusted.step + 1
	if points*count*8 > cr.ResourceMonitoringMaxTotalPoints {
		t.Fatal("total point budget exceeded")
	}
	window.from = now.Add(-24 * time.Hour)
	adjusted, count, truncated = fitMonitoringBudget(window, 500, 8)
	if !truncated || count >= 500 || adjusted.step > 3600 {
		t.Fatalf("hard budget not explicit: %+v %d %v", adjusted, count, truncated)
	}
	for _, input := range []cr.ResourceMonitoringQuery{{View: "raw"}, {View: "cpu", Node: "../node"}, {View: "cpu", RangeMinutes: 1441}, {View: "cpu", From: now.Format(time.RFC3339), To: now.Add(-time.Hour).Format(time.RFC3339)}} {
		if _, _, err := normalizeMonitoringQuery(input, now); err == nil {
			t.Fatalf("invalid query accepted: %+v", input)
		}
	}
}
func TestMonitoringMissingAndDuplicateSamplesNeverBecomeZeroOrDouble(t *testing.T) {
	window := monitoringWindow{from: time.Unix(1000, 0), to: time.Unix(1120, 0), step: 60}
	node := cr.ResourceMonitoringNode{NodeRef: cr.ResourceRef{Name: "n", UID: "uid"}, InternalIP: "10.0.0.1", OS: "linux"}
	cores := 1.0
	node.Capacity.CPUCores = &cores
	aliases := monitoringNodeAliases([]cr.ResourceMonitoringNode{node}, nil)
	definition := monitoringDefinition{key: "host.cpu.used", unit: "cores", dimension: "cpu"}
	sample := func(job, value string) monitoringRawSeries {
		return monitoringRawSeries{Metric: map[string]string{"instance": "10.0.0.1:9100", "job": job, "cpu": "0"}, Values: [][]any{{float64(1000), value}, {float64(1120), "0"}}}
	}
	result := monitoringResult{quality: "valid", series: []monitoringRawSeries{sample("a", "0.5"), sample("b", "0.5")}}
	budget := 100
	panel := buildMonitoringPanel(definition, result, []cr.ResourceMonitoringNode{node}, aliases, window, &budget)
	series := panel.Series[0]
	if series.Points[0].Value == nil || *series.Points[0].Value != 0.5 || series.Points[1].Value != nil || series.Latest == nil || *series.Latest != 0 || series.Quality != "partial" {
		t.Fatalf("missing/zero/duplicate changed meaning: %+v", series)
	}
	cores = 2
	budget = 100
	panel = buildMonitoringPanel(definition, result, []cr.ResourceMonitoringNode{node}, aliases, window, &budget)
	if panel.Series[0].Latest != nil || panel.Series[0].ReasonCode != "cpu_coverage_incomplete" {
		t.Fatal("a CPU missing for the whole window became valid usage")
	}
	duplicate := node
	duplicate.NodeRef.Name = "other"
	aliases = monitoringNodeAliases([]cr.ResourceMonitoringNode{node, duplicate}, nil)
	if monitoringSeriesNode(map[string]string{"instance": "10.0.0.1:9100"}, aliases) != "" {
		t.Fatal("ambiguous host selected first node")
	}
}
func TestMonitoringCounterTemplatesArePerSeriesAndFreshnessBounded(t *testing.T) {
	for _, definition := range monitoringDefinitions("conntrack", "cluster-a", "cluster") {
		if !strings.Contains(definition.query, `cluster="cluster-a"`) {
			t.Fatal("cluster matcher missing")
		}
		if definition.dimension == "counter" {
			counterTemplate := strings.HasPrefix(definition.query, "rate(") || strings.HasPrefix(definition.query, "increase(")
			if !counterTemplate || !strings.Contains(definition.ageQuery, "timestamp(") || !strings.Contains(definition.ageQuery, ">= 5") {
				t.Fatalf("counter aggregated before reset/freshness checks: %+v", definition)
			}
		}
	}
	quality, _ := monitoringPrerequisites(true, "http://metrics", "cluster", 0, "error")
	if quality != "error" {
		t.Fatal("inventory error masked as empty")
	}
	quality, _ = monitoringPrerequisites(true, "http://metrics", "", 1, "valid")
	if quality != "not_configured" {
		t.Fatal("cluster matcher silently removed")
	}
}

func TestMonitoringContainerCPUDoesNotSumAggregateAndPerCoreCounters(t *testing.T) {
	window := monitoringWindow{from: time.Unix(1000, 0), to: time.Unix(1060, 0), step: 60}
	node := cr.ResourceMonitoringNode{NodeRef: cr.ResourceRef{Name: "n", UID: "uid"}, OS: "linux"}
	raw := func(cpu string, value string, all bool) monitoringRawSeries {
		values := [][]any{{float64(1000), value}}
		if all {
			values = append(values, []any{float64(1060), value})
		}
		return monitoringRawSeries{Metric: map[string]string{"node": "n", "uid": "pod-uid", "container": "app", "cpu": cpu}, Values: values}
	}
	budget := 100
	panel := buildMonitoringPanel(monitoringDefinition{key: "pod.cpu.used", dimension: "container"}, monitoringResult{quality: "valid", series: []monitoringRawSeries{raw("total", "2", true), raw("0", "1", true), raw("1", "1", false)}}, []cr.ResourceMonitoringNode{node}, monitoringNodeAliases([]cr.ResourceMonitoringNode{node}, nil), window, &budget)
	if panel.Series[0].Latest == nil || *panel.Series[0].Latest != 2 {
		t.Fatalf("aggregate and per-core counters summed twice: %+v", panel.Series[0])
	}
	if panel.Series[0].Quality != "partial" || panel.Series[0].ReasonCode != "container_coverage_unverified" {
		t.Fatal("observed containers incorrectly proved complete collection")
	}
	budget = 100
	panel = buildMonitoringPanel(monitoringDefinition{key: "pod.cpu.used", dimension: "container"}, monitoringResult{quality: "valid", series: []monitoringRawSeries{raw("0", "1", true), raw("1", "1", false)}}, []cr.ResourceMonitoringNode{node}, monitoringNodeAliases([]cr.ResourceMonitoringNode{node}, nil), window, &budget)
	if panel.Series[0].Quality != "partial" {
		t.Fatal("missing known container dimension was treated as complete")
	}
}

func TestMonitoringRejectsWrongClusterPayloadEvenWithSameNodeName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Query().Get("query"), `cluster="cluster-a"`) {
			t.Error("query removed cluster scope")
		}
		_, _ = fmt.Fprint(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{"cluster":"cluster-b","node":"same-name"},"values":[[1000,"99"]]}]}}`)
	}))
	defer server.Close()
	inventory := &Inventory{metricsSupport: &metricsSupport{httpClient: server.Client()}}
	ctx := context.WithValue(context.Background(), prometheusClusterKey{}, "cluster-a")
	result := inventory.queryMonitoringRange(ctx, domainsettings.PrometheusSettings{BaseURL: server.URL, ClusterLabel: "cluster"}, `node_nf_conntrack_entries{cluster="cluster-a"}`, monitoringWindow{from: time.Unix(1000, 0), to: time.Unix(1060, 0), step: 60})
	if result.quality != "partial" || result.reason != "cluster_identity_mismatch" || len(result.series) != 0 {
		t.Fatalf("cross-cluster values escaped: %+v", result)
	}
}
