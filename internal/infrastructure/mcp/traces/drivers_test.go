package traces

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestJaegerDriverFindSlowSpansBuildsQueryAndMapsHotspots(t *testing.T) {
	from := time.Date(2026, 7, 11, 1, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/traces" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		params := r.URL.Query()
		if params.Get("service") != "orders" || params.Get("operation") != "checkout" {
			t.Errorf("scope query = %v", params)
		}
		if params.Get("start") != "1783731600000000" || params.Get("end") != "1783735200000000" {
			t.Errorf("time query = %v", params)
		}
		if params.Get("limit") != "2" || params.Get("minDuration") != "1500ms" {
			t.Errorf("limit query = %v", params)
		}
		if r.Header.Get("Authorization") != "Bearer jaeger-token" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		_, _ = w.Write([]byte(`{
  "data": [{
    "traceID": "trace-1",
    "processes": {"p1": {"serviceName": "orders"}},
    "spans": [
      {
        "spanID": "span-1",
        "references": [{"refType": "CHILD_OF", "spanID": " parent-1 "}],
        "operationName": "checkout",
        "processID": "p1",
        "startTime": 1783731600000000,
        "duration": 2000000,
        "tags": [{"key": "error", "type": "bool", "value": true}]
      },
      {
        "spanID": "span-2",
        "operationName": "checkout",
        "processID": "p1",
        "startTime": 1783731601000000,
        "duration": 1000000,
        "tags": []
      }
    ]
  }]
}`))
	}))
	defer server.Close()

	driver := jaegerDriver{http: server.Client()}
	result, err := driver.FindSlowSpans(context.Background(), "source-1", map[string]any{
		"endpoint": server.URL + "/", "serviceName": "orders",
		"operation": "checkout", "bearerToken": "jaeger-token",
	}, Query{TimeFrom: from, TimeTo: to, MinDuration: 1500 * time.Millisecond, Limit: 2})
	if err != nil {
		t.Fatalf("FindSlowSpans() error = %v", err)
	}
	if result.Summary != "2 spans matched, 1 hotspot groups" || len(result.Spans) != 2 {
		t.Fatalf("result = %#v", result)
	}
	first := result.Spans[0]
	if first.ParentSpanID != "parent-1" || !first.Error || first.DurationMS != 2000 {
		t.Fatalf("first span = %#v", first)
	}
	if len(result.Hotspots) != 1 || result.Hotspots[0]["count"] != 2 ||
		result.Hotspots[0]["errorCount"] != 1 ||
		result.Hotspots[0]["maxDuration"] != float64(2000) {
		t.Fatalf("hotspots = %#v", result.Hotspots)
	}
}

func TestSkyWalkingDriverFindSlowSpansBuildsPayloadAndMapsTrace(t *testing.T) {
	from := time.Date(2026, 7, 11, 1, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request = %s content-type %q", r.Method, r.Header.Get("Content-Type"))
		}
		if r.Header.Get("Authorization") != "Bearer sky-token" {
			t.Errorf("Authorization = %q", r.Header.Get("Authorization"))
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		variables := traceTestMap(t, payload["variables"], "variables")
		condition := traceTestMap(t, variables["condition"], "condition")
		if condition["serviceName"] != "payments" || condition["traceState"] != "ALL" {
			t.Errorf("condition = %#v", condition)
		}
		paging := traceTestMap(t, condition["paging"], "paging")
		if paging["pageSize"] != float64(3) {
			t.Errorf("paging = %#v", paging)
		}
		_, _ = w.Write([]byte(`{
  "data": {"queryBasicTraces": {"traces": [{
    "key": ["trace-sky"],
    "endpointNames": [],
    "duration": 750,
    "isError": true,
    "start": "invalid"
  }]}}
}`))
	}))
	defer server.Close()

	driver := skyWalkingDriver{http: server.Client()}
	result, err := driver.FindSlowSpans(context.Background(), "source-sky", map[string]any{
		"endpoint": server.URL + "/", "bearerToken": "sky-token",
	}, Query{
		Scope: Scope{Workload: "payments"}, TimeFrom: from, TimeTo: to,
		MinDuration: time.Second, Limit: 3,
	})
	if err != nil {
		t.Fatalf("FindSlowSpans() error = %v", err)
	}
	if result.Summary != "1 skywalking traces matched, 1 hotspot groups" || len(result.Spans) != 1 {
		t.Fatalf("result = %#v", result)
	}
	span := result.Spans[0]
	if span.TraceID != "trace-sky" || span.Operation != "payments" || !span.Error ||
		!span.StartTime.Equal(to) {
		t.Fatalf("span = %#v", span)
	}
}

func TestTraceDriversPreserveBackendErrors(t *testing.T) {
	t.Run("jaeger status", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		defer server.Close()
		driver := jaegerDriver{http: server.Client()}
		_, err := driver.FindSlowSpans(
			context.Background(), "source", map[string]any{"endpoint": server.URL}, Query{},
		)
		if err == nil || !strings.Contains(err.Error(), "jaeger query failed with status 502") {
			t.Fatalf("FindSlowSpans() error = %v", err)
		}
	})

	t.Run("skywalking graphql", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"errors":[{"message":"query rejected"}]}`))
		}))
		defer server.Close()
		driver := skyWalkingDriver{http: server.Client()}
		_, err := driver.FindSlowSpans(context.Background(), "source", map[string]any{
			"endpoint": server.URL, "serviceName": "orders",
		}, Query{})
		if err == nil || err.Error() != "skywalking query failed: query rejected" {
			t.Fatalf("FindSlowSpans() error = %v", err)
		}
	})
}

func TestNormalizeSlowSpanQueryDefaults(t *testing.T) {
	query := normalizeSlowSpanQuery(Query{})
	if query.Limit != 20 || query.MinDuration != 250*time.Millisecond {
		t.Fatalf("query defaults = %#v", query)
	}
	if query.TimeTo.IsZero() || query.TimeTo.Sub(query.TimeFrom) != time.Hour {
		t.Fatalf("query window = %s - %s", query.TimeFrom, query.TimeTo)
	}
}

func traceTestMap(t *testing.T, value any, name string) map[string]any {
	t.Helper()
	mapped, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("%s = %#v, want object", name, value)
	}
	return mapped
}
