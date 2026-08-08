package metrics

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
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
