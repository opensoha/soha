package monitoring

import (
	"context"
	"errors"
	"testing"
	"time"

	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/telemetry"
)

type ruleMetricTelemetry struct {
	analyze func(string) (telemetry.MetricAnomalySummary, error)
	queries *[]telemetry.MetricRangeQuery
}

func (s ruleMetricTelemetry) RangeQuery(context.Context, string, string, map[string]any, telemetry.MetricRangeQuery) ([]telemetry.MetricSeries, map[string]any, error) {
	return nil, nil, nil
}

func (s ruleMetricTelemetry) Analyze(_ context.Context, _, sourceID string, _ map[string]any, query telemetry.MetricRangeQuery) (telemetry.MetricAnomalySummary, error) {
	if s.queries != nil {
		*s.queries = append(*s.queries, query)
	}
	return s.analyze(sourceID)
}

func metricRule(value float64) domainalert.AlertRule {
	return domainalert.AlertRule{
		ID:       "rule-1",
		Name:     "CPU high",
		RuleType: "metrics",
		DatasourceSelector: map[string]any{
			"sourceKind": "metrics",
		},
		QuerySpec:     map[string]any{"metricKey": "cpu_usage", "windowMinutes": 5, "stepSeconds": 60},
		ThresholdSpec: map[string]any{"operator": "gt", "reducer": "last", "value": value},
		Labels:        map[string]string{"severity": "warning"},
		Enabled:       true,
	}
}

func metricSources(ids ...string) []domaincopilot.DataSource {
	items := make([]domaincopilot.DataSource, 0, len(ids))
	for _, id := range ids {
		items = append(items, domaincopilot.DataSource{ID: id, SourceKind: "metrics", BackendType: "prometheus", Enabled: true})
	}
	return items
}

func metricSummary(latest float64) telemetry.MetricAnomalySummary {
	return telemetry.MetricAnomalySummary{
		Summary: "cpu_usage",
		Signals: []map[string]any{{
			"metricKey": "cpu_usage", "latest": latest, "average": latest, "max": latest, "min": latest, "trend": "stable",
		}},
	}
}

func TestEvaluateMetricRuleUsesConfiguredThreshold(t *testing.T) {
	service := &Service{metrics: ruleMetricTelemetry{analyze: func(string) (telemetry.MetricAnomalySummary, error) {
		return metricSummary(90), nil
	}}}

	matched, err := service.evaluateMetricRule(t.Context(), metricRule(80), metricSources("prom-main"))
	if err != nil || !matched.Matched || matched.State != "matched" {
		t.Fatalf("matched result = %#v, err = %v", matched, err)
	}
	clear, err := service.evaluateMetricRule(t.Context(), metricRule(95), metricSources("prom-main"))
	if err != nil || clear.Matched || clear.State != "clear" {
		t.Fatalf("clear result = %#v, err = %v", clear, err)
	}
}

func TestEvaluateMetricRulePassesAdvancedPromQLToBackend(t *testing.T) {
	queries := []telemetry.MetricRangeQuery{}
	service := &Service{metrics: ruleMetricTelemetry{
		queries: &queries,
		analyze: func(string) (telemetry.MetricAnomalySummary, error) { return metricSummary(90), nil },
	}}
	rule := metricRule(80)
	rule.QuerySpec = map[string]any{"query": `sum(rate(http_requests_total[5m]))`, "windowMinutes": 5, "stepSeconds": 60}

	result, err := service.evaluateMetricRule(t.Context(), rule, metricSources("prom-main"))
	if err != nil || !result.Matched || len(queries) != 1 {
		t.Fatalf("advanced rule result = %#v, queries = %#v, err = %v", result, queries, err)
	}
	if queries[0].Expression != `sum(rate(http_requests_total[5m]))` || queries[0].MetricKey != "" {
		t.Fatalf("advanced metric query = %#v", queries[0])
	}
}

func TestEvaluateRuleBuildsReusableQuerySnapshot(t *testing.T) {
	service := &Service{
		dataSources: stubSignalDataSources{items: metricSources("prom-main")},
		metrics: ruleMetricTelemetry{analyze: func(string) (telemetry.MetricAnomalySummary, error) {
			return metricSummary(90), nil
		}},
	}
	rule := metricRule(80)
	rule.DatasourceSelector["clusterId"] = "cluster-a"

	result, err := service.evaluateRule(t.Context(), rule)
	if err != nil {
		t.Fatalf("evaluateRule() error = %v", err)
	}
	if result.QuerySnapshot["signal"] != "metrics" || result.QuerySnapshot["dataSourceId"] != "prom-main" || result.QuerySnapshot["metricKey"] != "cpu_usage" {
		t.Fatalf("query snapshot = %#v", result.QuerySnapshot)
	}
	contextValue, ok := result.QuerySnapshot["context"].(map[string]any)
	if !ok {
		t.Fatalf("snapshot context = %#v", result.QuerySnapshot["context"])
	}
	scope, ok := contextValue["scope"].(map[string]any)
	if !ok || scope["clusterId"] != "cluster-a" {
		t.Fatalf("snapshot scope = %#v", contextValue["scope"])
	}
}

func TestEvaluateMetricRuleKeepsLegacyAnomalyTrend(t *testing.T) {
	rule := metricRule(0)
	rule.ThresholdSpec = nil
	service := &Service{metrics: ruleMetricTelemetry{analyze: func(string) (telemetry.MetricAnomalySummary, error) {
		summary := metricSummary(90)
		summary.Signals[0]["trend"] = "spike"
		return summary, nil
	}}}

	result, err := service.evaluateMetricRule(t.Context(), rule, metricSources("prom-main"))
	if err != nil || result.State != "matched" || !result.Matched {
		t.Fatalf("legacy anomaly result = %#v, err = %v", result, err)
	}
}

func TestEvaluateMetricRuleDistinguishesNoDataErrorAndPartial(t *testing.T) {
	backendErr := errors.New("prometheus unavailable")
	service := &Service{metrics: ruleMetricTelemetry{analyze: func(sourceID string) (telemetry.MetricAnomalySummary, error) {
		switch sourceID {
		case "error":
			return telemetry.MetricAnomalySummary{}, backendErr
		case "empty":
			return telemetry.MetricAnomalySummary{}, nil
		default:
			return metricSummary(70), nil
		}
	}}}

	noData, err := service.evaluateMetricRule(t.Context(), metricRule(80), metricSources("empty"))
	if err != nil || noData.State != "no_data" {
		t.Fatalf("no-data result = %#v, err = %v", noData, err)
	}
	failed, err := service.evaluateMetricRule(t.Context(), metricRule(80), metricSources("error"))
	if err != nil || failed.State != "error" || len(failed.Errors) != 1 {
		t.Fatalf("error result = %#v, err = %v", failed, err)
	}
	partial, err := service.evaluateMetricRule(t.Context(), metricRule(80), metricSources("ok", "error"))
	if err != nil || partial.State != "partial" || partial.Matched {
		t.Fatalf("partial result = %#v, err = %v", partial, err)
	}
}

func TestMetricThresholdRejectsUnknownOperator(t *testing.T) {
	rule := metricRule(80)
	rule.ThresholdSpec["operator"] = "approximately"
	service := &Service{metrics: ruleMetricTelemetry{analyze: func(string) (telemetry.MetricAnomalySummary, error) {
		return metricSummary(90), nil
	}}}
	_, err := service.evaluateMetricRule(t.Context(), rule, metricSources("prom-main"))
	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("error = %v, want invalid argument", err)
	}
}

func TestEvaluateRuleRunOnlyClearResolvesActiveEvent(t *testing.T) {
	tests := []struct {
		name       string
		analyze    func(string) (telemetry.MetricAnomalySummary, error)
		wantRun    string
		wantStatus string
	}{
		{name: "provider error", analyze: func(string) (telemetry.MetricAnomalySummary, error) {
			return telemetry.MetricAnomalySummary{}, errors.New("unavailable")
		}, wantRun: "error", wantStatus: "firing"},
		{name: "no data", analyze: func(string) (telemetry.MetricAnomalySummary, error) {
			return telemetry.MetricAnomalySummary{}, nil
		}, wantRun: "no_data", wantStatus: "firing"},
		{name: "valid clear", analyze: func(string) (telemetry.MetricAnomalySummary, error) {
			return metricSummary(50), nil
		}, wantRun: "clear", wantStatus: "resolved"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fingerprint := internalRuleFingerprint("rule-1", nil)
			now := time.Now().UTC()
			repo := &stubMonitoringCompatRepository{alertEvents: map[string]domainalert.AlertEvent{
				"event-1": {ID: "event-1", RuleID: "rule-1", SourceType: "internal_rule", Fingerprint: fingerprint, Status: "firing", CurrentState: "firing", LastSeenAt: now},
			}}
			service := serviceWithCompatRepository(repo)
			service.dataSources = stubSignalDataSources{items: metricSources("prom-main")}
			service.metrics = ruleMetricTelemetry{analyze: test.analyze}

			service.evaluateRuleRun(t.Context(), metricRule(80))

			if len(repo.createdRuleRuns) != 1 || repo.createdRuleRuns[0].Status != test.wantRun {
				t.Fatalf("runs = %#v, want status %q", repo.createdRuleRuns, test.wantRun)
			}
			if got := repo.alertEvents["event-1"].Status; got != test.wantStatus {
				t.Fatalf("event status = %q, want %q", got, test.wantStatus)
			}
		})
	}
}

func TestEvaluateRuleRunTransitionsPendingToFiring(t *testing.T) {
	repo := &stubMonitoringCompatRepository{alertEvents: map[string]domainalert.AlertEvent{}}
	service := serviceWithCompatRepository(repo)
	service.dataSources = stubSignalDataSources{items: metricSources("prom-main")}
	service.metrics = ruleMetricTelemetry{analyze: func(string) (telemetry.MetricAnomalySummary, error) {
		return metricSummary(90), nil
	}}
	rule := metricRule(80)
	rule.ForSeconds = 120

	service.evaluateRuleRun(t.Context(), rule)
	if len(repo.createdRuleRuns) != 1 || repo.createdRuleRuns[0].Status != "pending" || len(repo.alertEvents) != 0 {
		t.Fatalf("first evaluation runs/events = %#v/%#v", repo.createdRuleRuns, repo.alertEvents)
	}
	repo.ruleRuns[0].CreatedAt = time.Now().UTC().Add(-121 * time.Second)
	service.evaluateRuleRun(t.Context(), rule)
	if len(repo.createdRuleRuns) != 2 || repo.createdRuleRuns[1].Status != "firing" {
		t.Fatalf("runs = %#v, want pending then firing", repo.createdRuleRuns)
	}
	if event := repo.alertEvents[internalRuleEventID(rule, internalRuleFingerprint(rule.ID, nil))]; event.Status != "firing" {
		t.Fatalf("firing event = %#v", event)
	} else if event.QuerySnapshot["metricKey"] != "cpu_usage" {
		t.Fatalf("event query snapshot = %#v", event.QuerySnapshot)
	}
	if repo.createdRuleRuns[1].QuerySnapshot["metricKey"] != "cpu_usage" {
		t.Fatalf("run query snapshot = %#v", repo.createdRuleRuns[1].QuerySnapshot)
	}
}

func TestEvaluateRuleRunShortForDurationStillStartsPending(t *testing.T) {
	repo := &stubMonitoringCompatRepository{alertEvents: map[string]domainalert.AlertEvent{}}
	service := serviceWithCompatRepository(repo)
	service.ruleInterval = time.Minute
	service.dataSources = stubSignalDataSources{items: metricSources("prom-main")}
	service.metrics = ruleMetricTelemetry{analyze: func(string) (telemetry.MetricAnomalySummary, error) {
		return metricSummary(90), nil
	}}
	rule := metricRule(80)
	rule.ForSeconds = 30

	service.evaluateRuleRun(t.Context(), rule)
	if len(repo.createdRuleRuns) != 1 || repo.createdRuleRuns[0].Status != "pending" {
		t.Fatalf("first run = %#v, want pending", repo.createdRuleRuns)
	}
	repo.ruleRuns[0].CreatedAt = time.Now().UTC().Add(-31 * time.Second)
	service.evaluateRuleRun(t.Context(), rule)
	if len(repo.createdRuleRuns) != 2 || repo.createdRuleRuns[1].Status != "firing" {
		t.Fatalf("runs = %#v, want pending then firing", repo.createdRuleRuns)
	}
}

func TestEvaluateRuleRunPartialMatchDoesNotResolveOtherEvents(t *testing.T) {
	rule := metricRule(80)
	matchedFingerprint := internalRuleFingerprint(rule.ID, nil)
	now := time.Now().UTC()
	repo := &stubMonitoringCompatRepository{alertEvents: map[string]domainalert.AlertEvent{
		"existing": {ID: "existing", RuleID: rule.ID, SourceType: "internal_rule", Fingerprint: "other-source", Status: "firing", CurrentState: "firing", LastSeenAt: now},
	}}
	service := serviceWithCompatRepository(repo)
	service.dataSources = stubSignalDataSources{items: metricSources("ok", "error")}
	service.metrics = ruleMetricTelemetry{analyze: func(sourceID string) (telemetry.MetricAnomalySummary, error) {
		if sourceID == "error" {
			return telemetry.MetricAnomalySummary{}, errors.New("unavailable")
		}
		return metricSummary(90), nil
	}}

	service.evaluateRuleRun(t.Context(), rule)

	if len(repo.createdRuleRuns) != 1 || repo.createdRuleRuns[0].Status != "partial" || !repo.createdRuleRuns[0].Matched {
		t.Fatalf("partial run = %#v", repo.createdRuleRuns)
	}
	if got := repo.alertEvents["existing"].Status; got != "firing" {
		t.Fatalf("unobserved event status = %q, want firing", got)
	}
	if got := repo.alertEvents[internalRuleEventID(rule, matchedFingerprint)].Status; got != "firing" {
		t.Fatalf("confirmed partial match event status = %q, want firing", got)
	}
}
