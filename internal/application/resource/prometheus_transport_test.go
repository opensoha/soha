package resource

import (
	"context"
	"encoding/json"
	"errors"
	contractresource "github.com/opensoha/soha-contracts/resource"
	domaincluster "github.com/opensoha/soha/internal/domain/cluster"
	agentinfra "github.com/opensoha/soha/internal/infrastructure/agent"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPrometheusSelectedAgentTransportNeverUsesCoreDirect(t *testing.T) {
	directCalls := 0
	direct := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { directCalls++ }))
	defer direct.Close()
	agentCalls := 0
	agentServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		agentCalls++
		if r.URL.Path != "/api/v1/platform/metrics/prometheus/query" {
			t.Error("wrong proxy route")
		}
		var query contractresource.PrometheusQuery
		if err := json.NewDecoder(r.Body).Decode(&query); err != nil {
			t.Error(err)
		}
		if query.Endpoint != direct.URL || query.Kind != "range" || query.Query != "up" {
			t.Errorf("wrong query: %#v", query)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"status": "success", "data": map[string]any{"result": []any{}}}})
	}))
	defer agentServer.Close()
	connection := domaincluster.Connection{Summary: domaincluster.Summary{ID: "private", ConnectionMode: domaincluster.ConnectionModeAgent}, Metadata: map[string]any{"endpoint": agentServer.URL, "prometheus_transport": "agent", "prometheus_url": direct.URL}}
	registry := agentinfra.NewRegistry(time.Second)
	service := &metricsSupport{resolver: stubConnectionResolver{connection: connection}, httpClient: direct.Client(), agent: func(c domaincluster.Connection) (PrometheusAgent, error) { return registry.ClientFor(c) }}
	ctx := context.WithValue(context.Background(), prometheusClusterKey{}, "private")
	if _, _, err := service.queryPrometheusRange(ctx, direct.URL, "core-secret", "up", time.Minute, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	service.agent = func(domaincluster.Connection) (PrometheusAgent, error) { return nil, errors.New("offline") }
	if _, _, err := service.queryPrometheusRange(ctx, direct.URL, "core-secret", "up", time.Minute, 10*time.Second); err == nil {
		t.Fatal("offline Agent accepted")
	}
	if directCalls != 0 || agentCalls != 1 {
		t.Fatalf("transport fallback: direct=%d agent=%d", directCalls, agentCalls)
	}
}

func TestPrometheusOldConfigurationUsesDirect(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"status":"success","data":{"result":[]}}`))
	}))
	defer server.Close()
	service := &metricsSupport{resolver: stubConnectionResolver{connection: domaincluster.Connection{Summary: domaincluster.Summary{ConnectionMode: domaincluster.ConnectionModeAgent}}}, httpClient: server.Client()}
	ctx := context.WithValue(context.Background(), prometheusClusterKey{}, "legacy")
	if _, _, err := service.queryPrometheusRange(ctx, server.URL, "", "up", time.Minute, 10*time.Second); err != nil || !called {
		t.Fatalf("legacy direct mode changed: %v", err)
	}
}
