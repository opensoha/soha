package logs

import (
	"context"
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
