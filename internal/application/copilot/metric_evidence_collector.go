package copilot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domaincopilot "github.com/kubecrux/kubecrux/internal/domain/copilot"
	mcpmetrics "github.com/kubecrux/kubecrux/internal/infrastructure/mcp/metrics"
)

type metricEvidenceAnalysis struct {
	evidence        []domaincopilot.RootCauseEvidence
	hypotheses      []domaincopilot.RootCauseHypothesis
	recommendations []string
	playbookResults map[string]any
	toolExecutions  []domaincopilot.ToolExecution
}

func (s *Service) collectRootCauseMetricEvidence(ctx context.Context, input domaincopilot.RootCauseRunInput, profile domaincopilot.AnalysisProfile, locale string) metricEvidenceAnalysis {
	result := metricEvidenceAnalysis{
		evidence:        []domaincopilot.RootCauseEvidence{},
		hypotheses:      []domaincopilot.RootCauseHypothesis{},
		recommendations: []string{},
		playbookResults: map[string]any{},
		toolExecutions:  []domaincopilot.ToolExecution{},
	}
	dataSources, err := s.repo.ListDataSources(ctx)
	if err != nil {
		result.playbookResults["metrics"] = "datasource_list_failed"
		return result
	}
	timeTo := time.Now().UTC()
	timeFrom := timeTo.Add(-time.Duration(input.TimeRangeMinutes) * time.Minute)
	for _, source := range dataSources {
		if !source.Enabled || source.SourceKind != "metrics" || source.MCPAdapter != "metrics.v1" {
			continue
		}
		if len(profile.EnabledSources) > 0 && !containsString(profile.EnabledSources, source.ID) && !containsString(profile.EnabledSources, "metrics") {
			continue
		}
		summary, err := mcpmetrics.DefaultRegistry().Analyze(ctx, source.BackendType, source.ID, source.Config, mcpmetrics.RangeQuery{
			Scope: mcpmetrics.Scope{
				ClusterID: input.ClusterID,
				Namespace: input.Namespace,
				Workload:  input.WorkloadName,
				Service:   input.WorkloadName,
			},
			TimeFrom: timeFrom,
			TimeTo:   timeTo,
			Step:     time.Minute,
		})
		if err != nil {
			result.playbookResults["metrics:"+source.ID] = "query_failed"
			continue
		}
		now := time.Now().UTC()
		result.toolExecutions = append(result.toolExecutions, domaincopilot.ToolExecution{
			ID:         "tool:" + uuid.NewString(),
			AdapterID:  "metrics.v1",
			ToolName:   "metrics.anomaly_summary",
			Status:     "success",
			Summary:    summary.Summary,
			Input:      map[string]any{"scope": input, "sourceId": source.ID},
			Output:     map[string]any{"signals": summary.Signals},
			StartedAt:  now,
			CompletedAt: &now,
		})
		if len(summary.Signals) == 0 {
			result.playbookResults["metrics:"+source.ID] = "no_signals"
			continue
		}
		result.playbookResults["metrics:"+source.ID] = "matched"
		sourceEvidence := make([]domaincopilot.RootCauseEvidence, 0, len(summary.Signals))
		spikingMetrics := make([]string, 0)
		for _, signal := range summary.Signals {
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
					"sourceId":   source.ID,
					"backendType": source.BackendType,
					"metricKey":  metricKey,
					"label":      label,
					"latest":     signal["latest"],
					"average":    signal["average"],
					"max":        signal["max"],
					"min":        signal["min"],
					"trend":      trend,
					"clusterId":  input.ClusterID,
					"namespace":  input.Namespace,
					"workload":   input.WorkloadName,
					"service":    input.WorkloadName,
				},
			}
			sourceEvidence = append(sourceEvidence, evidence)
			result.evidence = append(result.evidence, evidence)
		}
		if len(spikingMetrics) > 0 {
			result.hypotheses = append(result.hypotheses, domaincopilot.RootCauseHypothesis{
				ID:          "metrics-anomaly:" + source.ID,
				Title:       localize(locale, "关键指标出现明显异常", "Key metrics show visible anomalies"),
				Summary:     localize(locale, fmt.Sprintf("这些指标出现突增或偏离基线: %s。", strings.Join(spikingMetrics, "、")), fmt.Sprintf("These metrics are spiking or drifting away from baseline: %s.", strings.Join(spikingMetrics, ", "))),
				Confidence:  66,
				EvidenceIDs: collectEvidenceIDs(sourceEvidence...),
				Recommendations: []string{
					localize(locale, "对照最近变更和上游依赖，确认异常指标是否同步放大。", "Compare recent changes and upstream dependencies against the anomalous metrics."),
					localize(locale, "优先查看出现 spike 的指标所属节点。", "Inspect the graph nodes attached to the spiking metrics first."),
				},
			})
		}
	}
	result.recommendations = uniqueStrings(result.recommendations, flattenRecommendations(result.hypotheses))
	return result
}
