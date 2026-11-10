package logs

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func testHTTPClient(fn roundTripFunc) *http.Client {
	return &http.Client{Transport: fn}
}

func newJSONResponse(statusCode int, body string) *http.Response {
	return &http.Response{
		StatusCode: statusCode,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestRegistryOnlyRegistersLoki(t *testing.T) {
	registry := NewRegistry()
	if _, ok := registry.Get("Loki"); !ok {
		t.Fatal("Loki driver is missing")
	}
	for _, backend := range []string{"es", "elasticsearch", "clickhouse"} {
		t.Run(backend, func(t *testing.T) {
			if _, ok := registry.Get(backend); ok {
				t.Fatalf("retired log driver %q is registered", backend)
			}
		})
	}
}

func TestLokiDriverCorrelate(t *testing.T) {
	driver := lokiDriver{http: testHTTPClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/loki/api/v1/query_range" {
			t.Fatalf("path = %s, want /loki/api/v1/query_range", req.URL.Path)
		}
		query := req.URL.Query().Get("query")
		if !strings.Contains(query, `cluster="cluster-a"`) {
			t.Fatalf("query = %s, want cluster selector", query)
		}
		return newJSONResponse(http.StatusOK, `{"status":"success","data":{"result":[{"stream":{"cluster":"cluster-a","namespace":"prod","service":"payments","workload":"pay-api","level":"error"},"values":[["1735689600000000000","request timeout to upstream"]]}]}}`), nil
	})}

	result, err := driver.Correlate(context.Background(), "ds-2", map[string]any{
		"endpoint": "http://logs.example",
		"labelKeys": map[string]any{
			"cluster":   "cluster",
			"namespace": "namespace",
			"service":   "service",
			"workload":  "workload",
			"severity":  "level",
		},
	}, CorrelationQuery{
		Scope: Scope{ClusterID: "cluster-a", Namespace: "prod", Workload: "pay-api"},
		Query: "timeout",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("Correlate() error = %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records len = %d, want 1", len(result.Records))
	}
	if result.Records[0].Severity != "warning" {
		t.Fatalf("severity = %q, want warning", result.Records[0].Severity)
	}
	if len(result.Signatures) != 1 {
		t.Fatalf("signatures len = %d, want 1", len(result.Signatures))
	}
}

func TestLokiSearchUsesTenantScopeAndPaginates(t *testing.T) {
	driver := lokiDriver{http: testHTTPClient(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("X-Scope-OrgID") != "tenant-a" {
			t.Fatalf("tenant header = %q", req.Header.Get("X-Scope-OrgID"))
		}
		if req.Header.Get("Authorization") != "Bearer token-1" {
			t.Fatalf("authorization = %q", req.Header.Get("Authorization"))
		}
		query := req.URL.Query().Get("query")
		for _, expected := range []string{`cluster="cluster-a"`, `namespace="team-a"`, `pod="api-0"`, `container="api"`} {
			if !strings.Contains(query, expected) {
				t.Fatalf("query = %s, missing %s", query, expected)
			}
		}
		return newJSONResponse(http.StatusOK, `{"status":"success","data":{"result":[{"stream":{"cluster":"cluster-a","namespace":"team-a","pod":"api-0","container":"api"},"values":[["1735689602000000000","second"],["1735689601000000000","first"]]}]}}`), nil
	})}
	result, err := driver.Search(context.Background(), "ds-2", map[string]any{
		"endpoint": "https://logs.example", "tenantId": "tenant-a", "bearerToken": "token-1",
		"labelKeys": map[string]any{"cluster": "cluster", "namespace": "namespace", "pod": "pod", "container": "container"},
	}, SearchQuery{Scope: Scope{ClusterID: "cluster-a", Namespace: "team-a", Pod: "api-0", Container: "api"}, Limit: 1})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Records) != 1 || result.NextPageToken == "" || !result.Truncated {
		t.Fatalf("result = %#v", result)
	}
}

func TestLokiSearchSupportsMultipleScopesAndExactCorrelation(t *testing.T) {
	query := SearchQuery{
		Scope:   Scope{Pods: []string{"api-1", "api-2"}, Containers: []string{"api", "side.car"}},
		TraceID: "trace-1", SpanID: "span-1",
	}

	loki := buildLokiSearchQuery(query, nil)
	for _, expected := range []string{`pod=~"api-1|api-2"`, `container=~"api|side\\.car"`, `trace_id="trace-1"`, `span_id="span-1"`} {
		if !strings.Contains(loki, expected) {
			t.Fatalf("loki query = %s", loki)
		}
	}

}
