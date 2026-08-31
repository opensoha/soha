package copilot

import (
	"net/url"
	"testing"
	"time"

	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/telemetry"
)

func TestWorkbenchSourceFromEvidenceBuildsBoundedSignalLinks(t *testing.T) {
	from := time.Date(2026, time.August, 30, 8, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	tests := []struct {
		name, kind, path string
		attributes       map[string]any
		expected         map[string]string
	}{
		{
			name: "logs", kind: "logs.signature", path: "/monitoring-workbench/logs",
			attributes: map[string]any{"sourceId": "loki-main", "clusterId": "cluster/a", "namespace": "payments", "workload": "api v1", "traceId": "trace-1", "spanId": "span-1", "query": "upstream timeout", "timeFrom": from, "timeTo": to},
			expected:   map[string]string{"dataSourceId": "loki-main", "cluster": "cluster/a", "namespace": "payments", "workload": "api v1", "traceId": "trace-1", "spanId": "span-1", "text": "upstream timeout"},
		},
		{
			name: "metrics", kind: "metrics.signal", path: "/monitoring-workbench/metrics",
			attributes: map[string]any{"sourceId": "prometheus-main", "service": "checkout", "metricKey": "error_rate", "timeFrom": from, "timeTo": to},
			expected:   map[string]string{"dataSourceId": "prometheus-main", "service": "checkout", "metricKey": "error_rate"},
		},
		{
			name: "traces", kind: "trace.span", path: "/monitoring-workbench/traces",
			attributes: map[string]any{"sourceId": "jaeger-main", "service": "checkout", "traceId": "trace-1", "spanId": "span-1", "timeFrom": from, "timeTo": to},
			expected:   map[string]string{"dataSourceId": "jaeger-main", "service": "checkout", "traceId": "trace-1", "spanId": "span-1"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := workbenchSourceFromEvidence(domaincopilot.RootCauseEvidence{ID: "evidence-1", Kind: test.kind, Title: "Evidence", Attributes: test.attributes})
			parsed, err := url.Parse(source.URL)
			if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.Path != test.path {
				t.Fatalf("unsafe or incorrect source URL %q: %#v, %v", source.URL, parsed, err)
			}
			query := parsed.Query()
			if query.Get("from") != from.Format(time.RFC3339) || query.Get("to") != to.Format(time.RFC3339) {
				t.Fatalf("source URL lost absolute time range: %q", source.URL)
			}
			for key, value := range test.expected {
				if query.Get(key) != value {
					t.Fatalf("source URL %q lost %s=%q", source.URL, key, value)
				}
			}
		})
	}
}

func TestWorkbenchSourceFromEvidenceRejectsIncompleteLocator(t *testing.T) {
	from := time.Date(2026, time.August, 30, 8, 0, 0, 0, time.UTC)
	tests := []domaincopilot.RootCauseEvidence{
		{Kind: "metrics.signal", Attributes: map[string]any{"sourceId": "prometheus", "metricKey": "cpu_usage"}},
		{Kind: "trace.span", Attributes: map[string]any{"sourceId": "jaeger", "traceId": "trace-1", "timeFrom": from, "timeTo": from.Add(8 * 24 * time.Hour)}},
		{Kind: "delivery.event", Attributes: map[string]any{"sourceId": "delivery", "timeFrom": from, "timeTo": from.Add(time.Hour)}},
	}
	for _, evidence := range tests {
		if source := workbenchSourceFromEvidence(evidence); source.URL != "" {
			t.Fatalf("incomplete evidence produced a source URL: %#v", source)
		}
	}
}

func TestDedicatedEvidenceBuildersCarrySourceWindows(t *testing.T) {
	from := time.Date(2026, time.August, 30, 8, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	input := domaincopilot.RootCauseRunInput{ClusterID: "cluster-a", Namespace: "payments", WorkloadName: "api", Question: "upstream timeout"}

	logs := buildLogSignatureEvidence(input, domaincopilot.DataSource{ID: "loki", BackendType: "loki"}, telemetry.LogCorrelationResult{}, []telemetry.LogSignature{{Signature: "timeout", Sample: "upstream timeout"}}, from, to)
	traces, _ := buildTraceSpanEvidence(input, domaincopilot.DataSource{ID: "jaeger", BackendType: "jaeger"}, []telemetry.TraceSpan{{TraceID: "trace-1", SpanID: "span-1", Service: "api"}}, from, to)
	metrics, _ := buildMetricSignalEvidence(input, domaincopilot.DataSource{ID: "prometheus", BackendType: "prometheus"}, []map[string]any{{"metricKey": "error_rate", "label": "Error rate"}}, from, to)

	for _, evidence := range []domaincopilot.RootCauseEvidence{logs[0], traces[0], metrics[0]} {
		if source := workbenchSourceFromEvidence(evidence); source.URL == "" {
			t.Fatalf("dedicated evidence lost its source window: %#v", evidence)
		}
	}
	if logs[0].Attributes["query"] != input.Question {
		t.Fatalf("log evidence query = %#v, want %q", logs[0].Attributes["query"], input.Question)
	}
}

func TestSessionEvidenceBuildersCarrySourceWindows(t *testing.T) {
	from := time.Date(2026, time.August, 30, 8, 0, 0, 0, time.UTC)
	to := from.Add(time.Hour)
	scope := domaincopilot.SessionScope{ClusterID: "cluster-a", Namespace: "payments", Workload: "api", Service: "checkout"}
	metrics := sessionMetricEvidence("prometheus", scope, []map[string]any{{"metricKey": "error_rate", "label": "Error rate"}}, from, to)
	traces := sessionTraceEvidence("jaeger", scope, []telemetry.TraceSpan{{TraceID: "trace-1", SpanID: "span-1", Service: "checkout"}}, from, to)

	for _, evidence := range []domaincopilot.RootCauseEvidence{metrics[0], traces[0]} {
		if source := workbenchSourceFromEvidence(evidence); source.URL == "" {
			t.Fatalf("session evidence lost its source window: %#v", evidence)
		}
	}
}
