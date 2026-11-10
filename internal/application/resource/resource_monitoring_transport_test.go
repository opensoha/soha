package resource

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	cr "github.com/opensoha/soha-contracts/resource"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	agentinfra "github.com/opensoha/soha/internal/infrastructure/agent"
)

type monitoringDirectFixture struct {
	snapshot cr.ResourceMonitoringInventory
	calls    atomic.Int32
}

func (f *monitoringDirectFixture) ResourceMonitoringInventory(context.Context, string) (cr.ResourceMonitoringInventory, error) {
	f.calls.Add(1)
	return f.snapshot, nil
}

func TestResourceMonitoringConnectionAndMetricTransportMatrix(t *testing.T) {
	for _, test := range []struct {
		mode      domaincluster.ConnectionMode
		transport string
		valid     bool
	}{
		{domaincluster.ConnectionModeDirectKubeconfig, "direct", true},
		{domaincluster.ConnectionModeAgent, "direct", true},
		{domaincluster.ConnectionModeAgent, "agent", true},
		{domaincluster.ConnectionModeDirectKubeconfig, "agent", false},
	} {
		t.Run(string(test.mode)+"/"+test.transport, func(t *testing.T) {
			var calls monitoringHTTPCalls
			prometheus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.direct.Add(1)
				if !strings.Contains(r.URL.Query().Get("query"), `cluster="c"`) {
					t.Error("cluster matcher removed")
				}
				start, _ := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
				_ = json.NewEncoder(w).Encode(monitoringTransportPayload(r.URL.Query().Get("query"), start))
			}))
			defer prometheus.Close()
			cores := 4.0
			snapshot := cr.ResourceMonitoringInventory{Quality: "valid", Nodes: []cr.ResourceMonitoringNode{{NodeRef: cr.ResourceRef{ClusterID: "c", Kind: "Node", Name: "worker", UID: "worker-uid"}, OS: "linux", Capacity: cr.ResourceMonitoringQuantity{CPUCores: &cores}}}}
			agentServer := httptest.NewServer(monitoringTransportAgentHandler(t, prometheus.URL, snapshot, &calls))
			defer agentServer.Close()
			connection := domaincluster.Connection{Summary: domaincluster.Summary{ID: "c", ConnectionMode: test.mode}, Metadata: map[string]any{"endpoint": agentServer.URL, "token": "fixture-token", "prometheus_url": prometheus.URL, "prometheus_cluster_label": "cluster", "prometheus_transport": test.transport}}
			resolver := stubConnectionResolver{connection: connection}
			access := &resourceAccess{resolver: resolver, authorizer: exactPermissionAuthorizer{"platform.nodes.view": true, "platform.resource-monitoring.conntrack.view": true}, audit: discardAuditRecorder{}}
			registry := agentinfra.NewRegistry(time.Second)
			direct := &monitoringDirectFixture{snapshot: snapshot}
			inventory := &Inventory{resourceAccess: access, monitoringDirect: direct, monitoringAgent: func(c domaincluster.Connection) (MonitoringInventoryAgent, error) { return registry.ClientFor(c) }, metricsSupport: &metricsSupport{resourceAccess: access, resolver: resolver, httpClient: prometheus.Client(), agent: func(c domaincluster.Connection) (PrometheusAgent, error) { return registry.ClientFor(c) }}}
			result, err := inventory.GetResourceMonitoring(context.Background(), domainidentity.Principal{}, "c", cr.ResourceMonitoringQuery{View: "conntrack", From: "2026-10-19T00:00:00Z", To: "2026-10-19T00:01:00Z"})
			if err != nil {
				t.Fatal(err)
			}
			if len(result.Nodes) != 1 || result.Nodes[0].NodeRef.UID != "worker-uid" {
				t.Fatal("snapshot identity was lost")
			}
			valid := false
			for _, panel := range result.Panels {
				if panel.Key == "conntrack.entries" && len(panel.Series) == 1 && panel.Series[0].Latest != nil && *panel.Series[0].Latest == 3 {
					valid = true
				}
			}
			if valid != test.valid {
				t.Fatalf("valid metric=%v want %v panels=%+v", valid, test.valid, result.Panels)
			}
			assertMonitoringTransportCalls(t, test.mode, test.transport, test.valid, &calls, direct.calls.Load())
		})
	}
}

type monitoringHTTPCalls struct{ direct, proxy, inventory atomic.Int32 }

func monitoringTransportPayload(query string, start int64) map[string]any {
	labels := map[string]string{"cluster": "c", "node": "worker"}
	value := "3"
	if strings.Contains(query, "timestamp(") {
		value = "0"
	}
	if strings.Contains(query, "count_over_time(") {
		value = "5"
	}
	return map[string]any{"status": "success", "data": map[string]any{"resultType": "matrix", "result": []any{map[string]any{"metric": labels, "values": [][]any{{start, value}}}}}}
}

func monitoringTransportAgentHandler(t *testing.T, prometheusURL string, snapshot cr.ResourceMonitoringInventory, calls *monitoringHTTPCalls) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer fixture-token" {
			t.Error("Agent authentication missing")
		}
		switch r.URL.Path {
		case "/api/v1/platform/summary":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": domaincluster.Summary{ID: "c", Capabilities: []string{cr.ResourceMonitoringSnapshotFeature}}})
		case "/api/v1/platform/infrastructure/resource-monitoring":
			calls.inventory.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"data": snapshot})
		case "/api/v1/platform/metrics/prometheus/query":
			calls.proxy.Add(1)
			var query cr.PrometheusQuery
			if json.NewDecoder(r.Body).Decode(&query) != nil || query.Endpoint != prometheusURL || query.Kind != "range" || !strings.Contains(query.Query, `cluster="c"`) {
				t.Error("proxy query lost explicit source/window/scope")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"data": monitoringTransportPayload(query.Query, query.Start)})
		default:
			t.Errorf("unexpected Agent route %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}
}

func assertMonitoringTransportCalls(t *testing.T, mode domaincluster.ConnectionMode, transport string, valid bool, calls *monitoringHTTPCalls, directInventory int32) {
	t.Helper()
	if mode == domaincluster.ConnectionModeAgent {
		if calls.inventory.Load() != 1 || directInventory != 0 {
			t.Fatal("Agent inventory fell back to direct")
		}
	} else if calls.inventory.Load() != 0 || directInventory != 1 {
		t.Fatal("direct inventory used Agent")
	}
	if !valid {
		if calls.direct.Load() != 0 || calls.proxy.Load() != 0 {
			t.Fatal("invalid Direct/Agent configuration queried a source")
		}
	} else if transport == "agent" {
		if calls.proxy.Load() == 0 || calls.direct.Load() != 0 {
			t.Fatal("Agent metric transport fell back to Core direct")
		}
	} else if calls.direct.Load() == 0 || calls.proxy.Load() != 0 {
		t.Fatal("direct metrics used Agent transport")
	}
}
