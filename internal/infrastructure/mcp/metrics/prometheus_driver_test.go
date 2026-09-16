package metrics

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestPrometheusDriverQueriesImportedExpression(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/api/v1/query_range" || request.URL.Query().Get("query") != "sum(rate(requests[2m])) by (pod)" {
			t.Fatalf("request = %s %s", request.URL.Path, request.URL.RawQuery)
		}
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatalf("authorization = %q", request.Header.Get("Authorization"))
		}
		_, _ = fmt.Fprint(w, `{"status":"success","data":{"result":[{"metric":{"pod":"api-1"},"values":[[1,"2"]]},{"metric":{"pod":"api-2"},"values":[[1,"3"]]}]}}`)
	}))
	defer server.Close()

	driver := prometheusDriver{http: server.Client()}
	series, _, err := driver.RangeQuery(context.Background(), "prometheus-main", map[string]any{
		"endpoint": server.URL, "bearerToken": "secret",
	}, RangeQuery{
		MetricKey: "A", Expression: "sum(rate(requests[2m])) by (pod)", Legend: "{{pod}}",
		TimeFrom: time.Unix(1, 0), TimeTo: time.Unix(61, 0), Step: time.Minute,
	})
	if err != nil {
		t.Fatalf("range query: %v", err)
	}
	if len(series) != 2 || series[0].Key != "A:1" || series[0].Label != "api-1" || series[1].Latest != 3 {
		t.Fatalf("series = %#v", series)
	}
}

func TestRegisteredMetricsEscapeScopesAndUseErrorRatio(t *testing.T) {
	scope := Scope{Namespace: `app"} or vector(1)`, ClusterID: "prod\nother", Workload: "api(.*)", Service: `svc"`}
	definitions := metricDefinitions(scope, "cluster")
	for _, definition := range definitions {
		for _, clause := range []string{"namespace=" + strconv.Quote(scope.Namespace), "cluster=" + strconv.Quote(scope.ClusterID), "service=" + strconv.Quote(scope.Service), `pod=~"api\\(\\.\\*\\)-.*"`} {
			if !strings.Contains(definition.Query, clause) {
				t.Fatalf("unescaped scope %q: %s", clause, definition.Query)
			}
		}
	}
	errorRate := definitions[3]
	if errorRate.Unit != "ratio" || !strings.Contains(errorRate.Query, `status=~"5[0-9][0-9]"`) || !strings.Contains(errorRate.Query, `) / sum(rate(http_requests_total`) || !strings.Contains(errorRate.SourceSelector, `status=~"[1-5][0-9][0-9]"`) {
		t.Fatalf("not a defined 5xx ratio: %+v", errorRate)
	}
	if _, _, err := (prometheusDriver{}).RangeQuery(context.Background(), "source", map[string]any{"endpoint": "https://metrics.invalid", "clusterLabel": "cluster}\nother"}, RangeQuery{}); err == nil {
		t.Fatal("invalid label key accepted")
	}
}

func TestMetricEvidenceReadsOriginalSampleTimeAndRejectsPartialResponses(t *testing.T) {
	var queries []string
	warnings := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query().Get("query")
		queries = append(queries, query)
		if warnings {
			_, _ = fmt.Fprint(w, `{"status":"success","warnings":["partial response"],"data":{"result":[]}}`)
			return
		}
		value := "0.1"
		if strings.HasPrefix(query, "min(timestamp(") {
			value = "800"
		}
		_, _ = fmt.Fprintf(w, `{"status":"success","data":{"result":[{"metric":{},"values":[[1000,%q],[1030,"NaN"]]}]}}`, value)
	}))
	defer server.Close()
	driver := prometheusDriver{http: server.Client()}
	query := RangeQuery{RequireEvidence: true, MetricKey: "cpu_usage", TimeFrom: time.Unix(1000, 0), TimeTo: time.Unix(1030, 0), Step: 30 * time.Second}
	series, metadata, err := driver.RangeQuery(context.Background(), "source", map[string]any{"endpoint": server.URL}, query)
	if err != nil || len(series) != 1 || len(series[0].Points) != 1 || len(series[0].SourceTimes) != 1 || series[0].SourceTimes[0].Value != 800 || series[0].SourceTimes[0].Timestamp.Unix() != 1000 || len(queries) != 2 || metadata["queryCount"] != 2 {
		t.Fatalf("source/evaluation time confused: %+v %+v %v", series, metadata, err)
	}
	warnings = true
	if _, _, err := driver.RangeQuery(context.Background(), "source", map[string]any{"endpoint": server.URL}, query); err == nil {
		t.Fatal("partial result accepted as evidence")
	}
	query.Expression = "sum(other_metric)"
	if _, _, err := driver.RangeQuery(context.Background(), "source", map[string]any{"endpoint": server.URL}, query); err == nil {
		t.Fatal("custom expression accepted without a freshness definition")
	}
}
