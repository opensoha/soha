package mcp_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	logsinfra "github.com/opensoha/soha/internal/infrastructure/mcp/logs"
	metricsinfra "github.com/opensoha/soha/internal/infrastructure/mcp/metrics"
	tracesinfra "github.com/opensoha/soha/internal/infrastructure/mcp/traces"
	"github.com/opensoha/soha/internal/platform/telemetry"
)

func TestLiveObservabilitySmoke(t *testing.T) {
	if os.Getenv("SOHA_LIVE_OBSERVABILITY_SMOKE") != "1" {
		t.Skip("set SOHA_LIVE_OBSERVABILITY_SMOKE=1 to query live telemetry backends")
	}
	required := func(name string) string {
		value := strings.TrimSpace(os.Getenv(name))
		if value == "" {
			t.Fatalf("%s is required for the live observability smoke", name)
		}
		return value
	}

	const service = "soha-observability-smoke"
	marker := required("SOHA_SMOKE_LOG_MARKER")
	lokiEndpoint := required("SOHA_SMOKE_LOKI_ENDPOINT")
	prometheusEndpoint := required("SOHA_SMOKE_PROMETHEUS_ENDPOINT")
	skyWalkingEndpoint := required("SOHA_SMOKE_SKYWALKING_ENDPOINT")
	zipkinEndpoint := required("SOHA_SMOKE_ZIPKIN_ENDPOINT")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	to := time.Now().UTC()
	from := to.Add(-15 * time.Minute)

	t.Run("logs", func(t *testing.T) {
		result, err := logsinfra.DefaultRegistry().Search(ctx, "loki", "live-loki", map[string]any{
			"endpoint": lokiEndpoint,
			"labelKeys": map[string]any{
				"service": "service_name", "traceId": "trace_id", "spanId": "span_id",
			},
		}, telemetry.LogSearchQuery{
			Scope: telemetry.LogScope{Service: service}, TimeFrom: from, TimeTo: to,
			Terms: []string{marker}, Limit: 20, Direction: "backward",
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Records) == 0 || result.Records[0].Service != service || !strings.Contains(result.Records[0].Message, marker) {
			t.Fatalf("expected correlated live log, got %#v", result.Records)
		}
	})

	t.Run("metrics", func(t *testing.T) {
		series, _, err := metricsinfra.DefaultRegistry().RangeQuery(ctx, "prometheus", "live-prometheus", map[string]any{
			"endpoint": prometheusEndpoint,
		}, telemetry.MetricRangeQuery{
			MetricKey:  "smoke_value",
			Expression: `soha_observability_smoke_value{service_name="` + service + `"}`,
			TimeFrom:   from, TimeTo: to, Step: 15 * time.Second,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(series) == 0 || len(series[0].Points) == 0 {
			t.Fatalf("expected live metric series, got %#v", series)
		}
	})

	t.Run("traces", func(t *testing.T) {
		result, err := tracesinfra.DefaultRegistry().FindSlowSpans(ctx, "skywalking", "live-skywalking", map[string]any{
			"endpoint": skyWalkingEndpoint, "zipkinEndpoint": zipkinEndpoint,
		}, telemetry.TraceQuery{
			Scope: telemetry.TraceScope{Service: service}, TimeFrom: from, TimeTo: to,
			MinDuration: time.Millisecond, Limit: 20,
		})
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Spans) == 0 || result.Spans[0].Service != service {
			t.Fatalf("expected live OTLP trace, got %#v", result.Spans)
		}
	})
}
