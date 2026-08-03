package logs

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
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

func TestESDriverCorrelate(t *testing.T) {
	driver := esDriver{http: testHTTPClient(func(req *http.Request) (*http.Response, error) {
		if req.URL.Path != "/app-logs/_search" {
			t.Fatalf("path = %s, want /app-logs/_search", req.URL.Path)
		}
		return newJSONResponse(http.StatusOK, `{"hits":{"hits":[{"_source":{"@timestamp":"2026-01-01T00:00:00Z","level":"error","message":"timeout talking to upstream","service":"payments","workload":"pay-api","namespace":"prod","cluster":"cluster-a"}}]}}`), nil
	})}

	result, err := driver.Correlate(context.Background(), "ds-1", map[string]any{
		"endpoint": "http://logs.example",
		"index":    "app-logs",
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
	if result.Records[0].Service != "payments" {
		t.Fatalf("service = %q, want payments", result.Records[0].Service)
	}
	if len(result.Signatures) != 1 {
		t.Fatalf("signatures len = %d, want 1", len(result.Signatures))
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
}

func TestClickHouseDriverCorrelate(t *testing.T) {
	driver := clickHouseDriver{http: testHTTPClient(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", req.Method)
		}
		return newJSONResponse(http.StatusOK, `{"timestamp":"2026-01-01T00:00:00Z","severity":"error","message":"exception while calling upstream","service":"checkout","workload":"checkout-api","namespace":"prod","cluster":"cluster-a"}`+"\n"), nil
	})}

	result, err := driver.Correlate(context.Background(), "ds-3", map[string]any{
		"endpoint": "http://logs.example",
		"table":    "app_logs",
	}, CorrelationQuery{
		Scope:    Scope{ClusterID: "cluster-a", Namespace: "prod", Workload: "checkout-api"},
		Query:    "exception",
		Limit:    10,
		TimeFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		TimeTo:   time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Correlate() error = %v", err)
	}
	if len(result.Records) != 1 {
		t.Fatalf("records len = %d, want 1", len(result.Records))
	}
	if result.Records[0].Workload != "checkout-api" {
		t.Fatalf("workload = %q, want checkout-api", result.Records[0].Workload)
	}
}

func TestESSearchScopesCredentialsAndPaginates(t *testing.T) {
	var body map[string]any
	driver := esDriver{http: testHTTPClient(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "Bearer token-1" {
			t.Fatalf("authorization = %q", req.Header.Get("Authorization"))
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return newJSONResponse(http.StatusOK, `{"hits":{"hits":[`+
			`{"_source":{"@timestamp":"2026-01-01T00:00:02Z","message":"second","pod":"api-0","container":"api"}},`+
			`{"_source":{"@timestamp":"2026-01-01T00:00:01Z","message":"first","pod":"api-0","container":"api"}}]}}`), nil
	})}

	result, err := driver.Search(context.Background(), "ds-1", map[string]any{
		"endpoint": "https://logs.example", "index": "app-logs", "bearerToken": "token-1",
	}, SearchQuery{Scope: Scope{ClusterID: "cluster-a", Namespace: "team-a", Pod: "api-0", Container: "api"}, Limit: 1})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	payload, _ := json.Marshal(body)
	for _, expected := range []string{"cluster-a", "team-a", "api-0", "container"} {
		if !strings.Contains(string(payload), expected) {
			t.Fatalf("search body = %s, missing %q", payload, expected)
		}
	}
	if len(result.Records) != 1 || result.NextPageToken == "" || !result.Truncated {
		t.Fatalf("result = %#v", result)
	}
}

func TestLokiSearchUsesTenantScopeAndPaginates(t *testing.T) {
	driver := lokiDriver{http: testHTTPClient(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("X-Scope-OrgID") != "tenant-a" {
			t.Fatalf("tenant header = %q", req.Header.Get("X-Scope-OrgID"))
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
		"endpoint": "https://logs.example", "tenantId": "tenant-a",
		"labelKeys": map[string]any{"cluster": "cluster", "namespace": "namespace", "pod": "pod", "container": "container"},
	}, SearchQuery{Scope: Scope{ClusterID: "cluster-a", Namespace: "team-a", Pod: "api-0", Container: "api"}, Limit: 1})
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if len(result.Records) != 1 || result.NextPageToken == "" || !result.Truncated {
		t.Fatalf("result = %#v", result)
	}
}

func TestClickHouseRejectsInjectedIdentifiers(t *testing.T) {
	driver := clickHouseDriver{}
	err := driver.ValidateConfig(map[string]any{"endpoint": "https://logs.example", "table": "logs; DROP TABLE users"})
	if err == nil || !strings.Contains(err.Error(), "valid identifier") {
		t.Fatalf("ValidateConfig() error = %v", err)
	}
}

func TestSearchBuildersSupportMultipleScopesAndExactCorrelation(t *testing.T) {
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

	es, err := json.Marshal(buildESSearchBody(query, timestampCursor{}, "@timestamp", "message", "cluster", "namespace", "service", "workload", "pod", "container", "trace.id", "span.id", 100))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{`"terms":{"pod":["api-1","api-2"]}`, `"trace.id":"trace-1"`, `"span.id":"span-1"`} {
		if !strings.Contains(string(es), expected) {
			t.Fatalf("es query = %s", es)
		}
	}

	clickhouse := buildClickHouseSearchSQL("logs", "timestamp", "message", "severity", "service", "workload", "namespace", "cluster", "pod", "container", "trace_id", "span_id", query, timestampCursor{}, 100)
	for _, expected := range []string{"pod IN ('api-1', 'api-2')", "container IN ('api', 'side.car')", "trace_id = 'trace-1'", "span_id = 'span-1'"} {
		if !strings.Contains(clickhouse, expected) {
			t.Fatalf("clickhouse query = %s", clickhouse)
		}
	}
}
