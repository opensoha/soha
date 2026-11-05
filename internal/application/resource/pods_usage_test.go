package resource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	cr "github.com/opensoha/soha-contracts/resource"
	da "github.com/opensoha/soha/internal/domain/access"
	dc "github.com/opensoha/soha/internal/domain/cluster"
	dr "github.com/opensoha/soha/internal/domain/resource"
)

func TestMergePodUsageAcceptsOnlyNonnegativeFiniteSamples(t *testing.T) {
	for _, test := range []struct {
		name  string
		value float64
		valid bool
	}{
		{"zero", 0, true},
		{"positive", 0.25, true},
		{"nan", math.NaN(), false},
		{"positive-infinity", math.Inf(1), false},
		{"negative-infinity", math.Inf(-1), false},
		{"negative", -0.25, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			out := make(map[string]podUsageValue)
			mergeNamespacePodUsage(out, "n", []string{"p"}, namespacePodUsage{
				cpu: map[string]float64{"p": test.value}, memory: map[string]float64{"p": test.value},
			})
			got, exists := out["n/p"]
			if exists != test.valid || got.CPUKnown != test.valid || got.MemoryKnown != test.valid {
				t.Fatalf("sample validity lost: value=%v exists=%v usage=%+v", test.value, exists, got)
			}
			if test.valid {
				if got.CPUCores != test.value || got.MemoryBytes != test.value {
					t.Fatalf("valid sample changed: %+v", got)
				}
				return
			}
			for _, cpuInvalid := range []bool{true, false} {
				out = make(map[string]podUsageValue)
				cpu, memory := test.value, 0.0
				if !cpuInvalid {
					cpu, memory = memory, cpu
				}
				mergeNamespacePodUsage(out, "n", []string{"p"}, namespacePodUsage{
					cpu: map[string]float64{"p": cpu}, memory: map[string]float64{"p": memory},
				})
				got = out["n/p"]
				if got.CPUKnown == cpuInvalid || got.MemoryKnown != cpuInvalid || got.CPUCores != 0 || got.MemoryBytes != 0 {
					t.Fatalf("invalid sample tainted valid zero: cpuInvalid=%v usage=%+v", cpuInvalid, got)
				}
			}
		})
	}
}

type usagePodAgent struct {
	WorkloadAgent
	items []dr.PodView
}

func (a usagePodAgent) ListPods(context.Context, string) ([]dr.PodView, error) {
	return append([]dr.PodView(nil), a.items...), nil
}

type usagePrometheusAgent struct {
	query func(context.Context, cr.PrometheusQuery) (json.RawMessage, error)
}

func (a usagePrometheusAgent) QueryPrometheus(ctx context.Context, query cr.PrometheusQuery) (json.RawMessage, error) {
	return a.query(ctx, query)
}

func TestPodListUsageScopeAndTransportParity(t *testing.T) {
	for _, mode := range []string{"direct", "agent-direct-metrics", "agent-proxy-metrics"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			queryResult := func(query string) []byte {
				calls.Add(1)
				if !strings.Contains(query, `cluster="metric-alias"`) || strings.Contains(query, "hidden") {
					t.Errorf("query escaped authorized identity: %s", query)
				}
				ns, value := "a", "0"
				if strings.Contains(query, `namespace="b"`) {
					ns, value = "b", "2"
				}
				return []byte(fmt.Sprintf(`{"status":"success","data":{"resultType":"vector","result":[{"metric":{"namespace":%q,"pod":"same"},"value":[1,%q]}]}}`, ns, value))
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if mode == "agent-proxy-metrics" {
					t.Error("selected Agent transport reached Core HTTP")
				}
				_, _ = w.Write(queryResult(r.URL.Query().Get("query")))
			}))
			defer server.Close()
			connection := dc.Connection{Summary: dc.Summary{ID: "soha-id", ConnectionMode: dc.ConnectionModeDirectKubeconfig}, Metadata: map[string]any{
				"prometheus_url": server.URL, "prometheus_cluster_label": "cluster", "prometheus_cluster_label_value": "metric-alias",
			}}
			if mode != "direct" {
				connection.Summary.ConnectionMode = dc.ConnectionModeAgent
			}
			if mode == "agent-proxy-metrics" {
				connection.Metadata["prometheus_transport"] = "agent"
			}
			pods := []dr.PodView{{Name: "same", Namespace: "a"}, {Name: "same", Namespace: "b"}, {Name: "secret", Namespace: "hidden"}}
			metrics := &metricsSupport{resolver: stubConnectionResolver{connection: connection}, httpClient: server.Client(), agent: func(c dc.Connection) (PrometheusAgent, error) {
				if c.Summary.ID != "soha-id" {
					t.Error("transport identity replaced with metric alias")
				}
				return usagePrometheusAgent{query: func(_ context.Context, q cr.PrometheusQuery) (json.RawMessage, error) {
					if q.Kind != "instant" || q.Endpoint != server.URL {
						t.Errorf("invalid proxy query: %+v", q)
					}
					return queryResult(q.Query), nil
				}}, nil
			}}
			workloads := &Workloads{metricsSupport: metrics, directPods: &stubDirectPods{items: pods}, agent: func(dc.Connection) (WorkloadAgent, error) { return usagePodAgent{items: pods}, nil }}
			decision := da.Decision{ResourceScope: &da.ResourceScope{Namespaces: []string{"a", "b"}}}
			items, _, err := workloads.listPodViews(t.Context(), "soha-id", "", connection, decision, true)
			if err != nil || len(items) != 2 {
				t.Fatalf("list = %+v, %v", items, err)
			}
			if items[0].CPU != "0" || items[0].Memory != "0 B" || items[1].CPU != "2.00" || items[1].Memory != "2 B" {
				t.Fatalf("namespace identities or zero usage lost: %+v", items)
			}
			if calls.Load() != 4 {
				t.Fatalf("queries = %d, want 2 per authorized namespace", calls.Load())
			}
			assertPodListUsageQueryScope(t, workloads, connection, decision, &calls)
		})
	}
}

func assertPodListUsageQueryScope(t *testing.T, workloads *Workloads, connection dc.Connection, decision da.Decision, calls *atomic.Int32) {
	t.Helper()
	_, _, err := workloads.listPodViews(t.Context(), "soha-id", "", connection, decision, false)
	if err != nil || calls.Load() != 4 {
		t.Fatalf("overview queried metrics: calls=%d err=%v", calls.Load(), err)
	}
	decision.ResourceScope.Namespaces = []string{"a"}
	items, _, err := workloads.listPodViews(t.Context(), "soha-id", "a", connection, decision, true)
	if err != nil || len(items) != 1 || items[0].CPU != "0" || calls.Load() != 6 {
		t.Fatalf("namespaced list = %+v, calls=%d, err=%v", items, calls.Load(), err)
	}
	// A scope excluding every returned namespace must cause no metrics read.
	decision.ResourceScope.Namespaces = []string{"unrelated"}
	items, _, err = workloads.listPodViews(t.Context(), "soha-id", "", connection, decision, true)
	if err != nil || len(items) != 0 || calls.Load() != 6 {
		t.Fatalf("denied identities queried: items=%+v calls=%d err=%v", items, calls.Load(), err)
	}
}

func TestPodListUsageUnavailableAndPartial(t *testing.T) {
	for _, scenario := range []string{"unconfigured", "empty", "failed", "partial", "timeout", "agent-offline"} {
		t.Run(scenario, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if !strings.Contains(r.URL.Query().Get("query"), `cluster="c"`) {
					t.Error("empty/failure query dropped cluster matcher")
				}
				switch scenario {
				case "timeout":
					<-r.Context().Done()
					return
				case "failed":
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				case "partial":
					if strings.Contains(r.URL.Query().Get("query"), "container_cpu") {
						_, _ = w.Write([]byte(`{"status":"success","data":{"result":[{"metric":{"pod":"p"},"value":[1,"0"]}]}}`))
						return
					}
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				_, _ = w.Write([]byte(`{"status":"success","data":{"result":[]}}`))
			}))
			defer server.Close()
			connection := dc.Connection{Summary: dc.Summary{ID: "c"}, Metadata: map[string]any{"prometheus_url": server.URL, "prometheus_cluster_label": "cluster"}}
			if scenario == "unconfigured" {
				connection.Metadata = nil
			}
			if scenario == "agent-offline" {
				connection.Summary.ConnectionMode = dc.ConnectionModeAgent
				connection.Metadata["prometheus_transport"] = "agent"
			}
			metrics := &metricsSupport{resolver: stubConnectionResolver{connection: connection}, httpClient: server.Client(), agent: func(dc.Connection) (PrometheusAgent, error) { return nil, errors.New("offline") }}
			pods := []dr.PodView{{Name: "p", Namespace: "n"}, {Name: "missing", Namespace: "n"}, {Name: "late", Namespace: "z"}}
			workloads := &Workloads{metricsSupport: metrics, directPods: &stubDirectPods{items: pods}, agent: func(dc.Connection) (WorkloadAgent, error) { return usagePodAgent{items: pods}, nil }}
			start := time.Now()
			items, _, err := workloads.listPodViews(t.Context(), "c", "", connection, da.Decision{}, true)
			if err != nil || len(items) != 3 {
				t.Fatalf("metrics failure affected Pod inventory: %+v %v", items, err)
			}
			assertPodListUsageSamples(t, items, scenario == "partial")
			if (scenario == "unconfigured" || scenario == "agent-offline") && calls.Load() != 0 {
				t.Fatal("unavailable configuration reached direct Prometheus")
			}
			if scenario == "timeout" && (time.Since(start) > 2*time.Second || calls.Load() != 1) {
				t.Fatalf("budget was reset per query: elapsed=%v calls=%d", time.Since(start), calls.Load())
			}
		})
	}
}

func assertPodListUsageSamples(t *testing.T, items []dr.PodView, partial bool) {
	t.Helper()
	for _, item := range items {
		wantCPU := ""
		if partial && item.Name == "p" {
			wantCPU = "0"
		}
		if item.CPU != wantCPU || item.Memory != "" {
			t.Errorf("missing usage became zero: %+v", item)
		}
	}
}
