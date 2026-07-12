package copilot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	"github.com/opensoha/soha/internal/platform/telemetry"
)

const (
	metricEvidenceAdapterID = "metrics.v1"
	metricEvidenceToolName  = "metrics.anomaly_summary"
)

type metricEvidenceAnalysis struct {
	evidence        []domaincopilot.RootCauseEvidence
	hypotheses      []domaincopilot.RootCauseHypothesis
	recommendations []string
	playbookResults map[string]any
	toolExecutions  []domaincopilot.ToolExecution
}

type metricEvidenceRequest struct {
	input    domaincopilot.RootCauseRunInput
	source   domaincopilot.DataSource
	limit    int
	timeFrom time.Time
	timeTo   time.Time
	locale   string
}

type metricEvidenceBatchRequest struct {
	input   domaincopilot.RootCauseRunInput
	profile domaincopilot.AnalysisProfile
	toolset domaincopilot.SessionToolset
	locale  string
	setup   evidenceCollectionSetup
}

type evidenceCollectionSetupRequest struct {
	input     domaincopilot.RootCauseRunInput
	toolset   domaincopilot.SessionToolset
	adapterID string
	toolName  string
}

type evidenceCollectionSetup struct {
	dataSources []domaincopilot.DataSource
	limit       int
	timeFrom    time.Time
	timeTo      time.Time
}

func (s *Service) prepareEvidenceCollection(ctx context.Context, request evidenceCollectionSetupRequest) (evidenceCollectionSetup, string) {
	if !toolsetAllowsTool(request.toolset, request.adapterID, request.toolName) {
		return evidenceCollectionSetup{}, "tool_disabled"
	}
	dataSources, err := s.dataSources.ListDataSources(ctx)
	if err != nil {
		return evidenceCollectionSetup{}, "datasource_list_failed"
	}
	timeTo := time.Now().UTC()
	return evidenceCollectionSetup{
		dataSources: dataSources,
		limit:       evidenceBudget(request.toolset, 20),
		timeFrom:    timeTo.Add(-time.Duration(request.input.TimeRangeMinutes) * time.Minute),
		timeTo:      timeTo,
	}, ""
}

func (s *Service) collectRootCauseMetricEvidence(ctx context.Context, input domaincopilot.RootCauseRunInput, profile domaincopilot.AnalysisProfile, toolset domaincopilot.SessionToolset, locale string) metricEvidenceAnalysis {
	result := metricEvidenceAnalysis{
		evidence:        []domaincopilot.RootCauseEvidence{},
		hypotheses:      []domaincopilot.RootCauseHypothesis{},
		recommendations: []string{},
		playbookResults: map[string]any{},
		toolExecutions:  []domaincopilot.ToolExecution{},
	}
	setup, status := s.prepareEvidenceCollection(ctx, evidenceCollectionSetupRequest{
		input: input, toolset: toolset,
		adapterID: metricEvidenceAdapterID, toolName: metricEvidenceToolName,
	})
	if status != "" {
		result.playbookResults["metrics"] = status
		return result
	}
	s.collectMetricEvidenceBatch(ctx, metricEvidenceBatchRequest{
		input: input, profile: profile, toolset: toolset, locale: locale, setup: setup,
	}, &result)
	result.recommendations = uniqueStrings(result.recommendations, flattenRecommendations(result.hypotheses))
	return result
}

func (s *Service) collectMetricEvidenceBatch(ctx context.Context, request metricEvidenceBatchRequest, result *metricEvidenceAnalysis) {
	for _, source := range request.setup.dataSources {
		if !metricEvidenceSourceEnabled(source, request.profile, request.toolset, metricEvidenceAdapterID) {
			continue
		}
		s.collectMetricSourceEvidence(ctx, metricEvidenceRequest{
			input: request.input, source: source, limit: request.setup.limit,
			timeFrom: request.setup.timeFrom, timeTo: request.setup.timeTo, locale: request.locale,
		}, result)
	}
}

func (s *Service) collectMetricSourceEvidence(ctx context.Context, request metricEvidenceRequest, result *metricEvidenceAnalysis) {
	source := request.source
	summary, err := s.metricBackend().Analyze(ctx, source.BackendType, source.ID, source.Config, telemetry.MetricRangeQuery{
		Scope: telemetry.MetricScope{
			ClusterID: request.input.ClusterID,
			Namespace: request.input.Namespace,
			Workload:  request.input.WorkloadName,
			Service:   request.input.WorkloadName,
		},
		TimeFrom: request.timeFrom,
		TimeTo:   request.timeTo,
		Step:     time.Minute,
	})
	if err != nil {
		result.playbookResults["metrics:"+source.ID] = "query_failed"
		return
	}
	signals := limitMapItems(summary.Signals, request.limit)
	now := time.Now().UTC()
	result.toolExecutions = append(result.toolExecutions, domaincopilot.ToolExecution{
		ID:          "tool:" + uuid.NewString(),
		AdapterID:   metricEvidenceAdapterID,
		ToolName:    metricEvidenceToolName,
		Status:      "success",
		Summary:     summary.Summary,
		Input:       map[string]any{"scope": request.input, "sourceId": source.ID},
		Output:      map[string]any{"signals": signals},
		StartedAt:   now,
		CompletedAt: &now,
	})
	if len(signals) == 0 {
		result.playbookResults["metrics:"+source.ID] = "no_signals"
		return
	}
	result.playbookResults["metrics:"+source.ID] = "matched"
	sourceEvidence, spikingMetrics := buildMetricSignalEvidence(request.input, source, signals)
	result.evidence = append(result.evidence, sourceEvidence...)
	if len(spikingMetrics) > 0 {
		result.hypotheses = append(result.hypotheses, metricAnomalyHypothesis(request.locale, source.ID, spikingMetrics, sourceEvidence))
	}
}

func buildMetricSignalEvidence(input domaincopilot.RootCauseRunInput, source domaincopilot.DataSource, signals []map[string]any) ([]domaincopilot.RootCauseEvidence, []string) {
	evidenceItems := make([]domaincopilot.RootCauseEvidence, 0, len(signals))
	spikingMetrics := make([]string, 0)
	for _, signal := range signals {
		trend := strings.TrimSpace(fmt.Sprint(signal["trend"]))
		metricKey := strings.TrimSpace(fmt.Sprint(signal["metricKey"]))
		label := strings.TrimSpace(fmt.Sprint(signal["label"]))
		severity := "info"
		if trend == "spike" {
			severity = "warning"
			spikingMetrics = append(spikingMetrics, firstNonEmpty(label, metricKey))
		}
		evidence := domaincopilot.RootCauseEvidence{
			ID:       fmt.Sprintf("metrics:%s:%s", source.ID, metricKey),
			Kind:     "metrics.signal",
			Title:    firstNonEmpty(label, metricKey),
			Summary:  fmt.Sprintf("latest=%v average=%v trend=%v", signal["latest"], signal["average"], trend),
			Severity: severity,
			Attributes: map[string]any{
				"sourceId":    source.ID,
				"backendType": source.BackendType,
				"metricKey":   metricKey,
				"label":       label,
				"latest":      signal["latest"],
				"average":     signal["average"],
				"max":         signal["max"],
				"min":         signal["min"],
				"trend":       trend,
				"clusterId":   input.ClusterID,
				"namespace":   input.Namespace,
				"workload":    input.WorkloadName,
				"service":     input.WorkloadName,
			},
		}
		evidenceItems = append(evidenceItems, evidence)
	}
	return evidenceItems, spikingMetrics
}

func metricAnomalyHypothesis(locale, sourceID string, spikingMetrics []string, evidence []domaincopilot.RootCauseEvidence) domaincopilot.RootCauseHypothesis {
	return domaincopilot.RootCauseHypothesis{
		ID:          "metrics-anomaly:" + sourceID,
		Title:       localize(locale, "关键指标出现明显异常", "Key metrics show visible anomalies"),
		Summary:     localize(locale, fmt.Sprintf("这些指标出现突增或偏离基线: %s。", strings.Join(spikingMetrics, "、")), fmt.Sprintf("These metrics are spiking or drifting away from baseline: %s.", strings.Join(spikingMetrics, ", "))),
		Confidence:  66,
		EvidenceIDs: collectEvidenceIDs(evidence...),
		Recommendations: []string{
			localize(locale, "对照最近变更和上游依赖，确认异常指标是否同步放大。", "Compare recent changes and upstream dependencies against the anomalous metrics."),
			localize(locale, "优先查看出现 spike 的指标所属节点。", "Inspect the graph nodes attached to the spiking metrics first."),
		},
	}
}

func metricEvidenceSourceEnabled(source domaincopilot.DataSource, profile domaincopilot.AnalysisProfile, toolset domaincopilot.SessionToolset, adapterID string) bool {
	if !source.Enabled || source.SourceKind != "metrics" || source.MCPAdapter != adapterID || !toolsetAllowsAdapter(toolset, source.MCPAdapter) {
		return false
	}
	return len(profile.EnabledSources) == 0 || containsString(profile.EnabledSources, source.ID) || containsString(profile.EnabledSources, "metrics")
}
