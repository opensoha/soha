package copilot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domaincopilot "github.com/kubecrux/kubecrux/internal/domain/copilot"
	mcptraces "github.com/kubecrux/kubecrux/internal/infrastructure/mcp/traces"
)

type traceEvidenceAnalysis struct {
	evidence        []domaincopilot.RootCauseEvidence
	hypotheses      []domaincopilot.RootCauseHypothesis
	recommendations []string
	playbookResults map[string]any
	toolExecutions  []domaincopilot.ToolExecution
}

func (s *Service) collectRootCauseTraceEvidence(ctx context.Context, input domaincopilot.RootCauseRunInput, profile domaincopilot.AnalysisProfile, toolset domaincopilot.SessionToolset, locale string) traceEvidenceAnalysis {
	result := traceEvidenceAnalysis{
		evidence:        []domaincopilot.RootCauseEvidence{},
		hypotheses:      []domaincopilot.RootCauseHypothesis{},
		recommendations: []string{},
		playbookResults: map[string]any{},
		toolExecutions:  []domaincopilot.ToolExecution{},
	}
	const adapterID = "traces.v1"
	const toolName = "traces.find_slow_spans"
	if !toolsetAllowsTool(toolset, adapterID, toolName) {
		result.playbookResults["traces"] = "tool_disabled"
		return result
	}
	dataSources, err := s.repo.ListDataSources(ctx)
	if err != nil {
		result.playbookResults["traces"] = "datasource_list_failed"
		return result
	}
	limit := evidenceBudget(toolset, 20)
	timeTo := time.Now().UTC()
	timeFrom := timeTo.Add(-time.Duration(input.TimeRangeMinutes) * time.Minute)
	for _, source := range dataSources {
		if !source.Enabled || source.SourceKind != "traces" || source.MCPAdapter != adapterID || !toolsetAllowsAdapter(toolset, source.MCPAdapter) {
			continue
		}
		if len(profile.EnabledSources) > 0 && !containsString(profile.EnabledSources, source.ID) && !containsString(profile.EnabledSources, "traces") {
			continue
		}
		traceResult, err := mcptraces.DefaultRegistry().FindSlowSpans(ctx, source.BackendType, source.ID, source.Config, mcptraces.Query{
			Scope: mcptraces.Scope{
				ClusterID: input.ClusterID,
				Namespace: input.Namespace,
				Service:   input.WorkloadName,
				Workload:  input.WorkloadName,
			},
			TimeFrom:    timeFrom,
			TimeTo:      timeTo,
			MinDuration: 250 * time.Millisecond,
			Limit:       limit,
		})
		if err != nil {
			result.playbookResults["traces:"+source.ID] = "query_failed"
			continue
		}
		spans := limitSpans(traceResult.Spans, limit)
		now := time.Now().UTC()
		result.toolExecutions = append(result.toolExecutions, domaincopilot.ToolExecution{
			ID:          "tool:" + uuid.NewString(),
			AdapterID:   adapterID,
			ToolName:    toolName,
			Status:      "success",
			Summary:     traceResult.Summary,
			Input:       map[string]any{"scope": input, "sourceId": source.ID},
			Output:      map[string]any{"hotspots": traceResult.Hotspots},
			StartedAt:   now,
			CompletedAt: &now,
		})
		if len(spans) == 0 {
			result.playbookResults["traces:"+source.ID] = "no_spans"
			continue
		}
		result.playbookResults["traces:"+source.ID] = "matched"
		sourceEvidence := make([]domaincopilot.RootCauseEvidence, 0, len(spans))
		hotServices := make([]string, 0)
		for index, span := range spans {
			severity := ternarySeverity(span.Error || span.DurationMS >= 1000, "warning", "info")
			if span.DurationMS >= 1000 || span.Error {
				hotServices = append(hotServices, firstNonEmpty(span.Service, span.Operation))
			}
			evidence := domaincopilot.RootCauseEvidence{
				ID:       fmt.Sprintf("trace:%s:%d", source.ID, index+1),
				Kind:     "trace.span",
				Title:    fmt.Sprintf("%s / %s", span.Service, span.Operation),
				Summary:  fmt.Sprintf("duration=%.2fms trace=%s", span.DurationMS, span.TraceID),
				Severity: severity,
				Attributes: map[string]any{
					"sourceId":     source.ID,
					"backendType":  source.BackendType,
					"traceId":      span.TraceID,
					"spanId":       span.SpanID,
					"parentSpanId": span.ParentSpanID,
					"durationMs":   span.DurationMS,
					"error":        span.Error,
					"tags":         span.Tags,
					"service":      span.Service,
					"operation":    span.Operation,
					"clusterId":    input.ClusterID,
					"namespace":    input.Namespace,
					"workload":     input.WorkloadName,
				},
			}
			sourceEvidence = append(sourceEvidence, evidence)
			result.evidence = append(result.evidence, evidence)
		}
		if len(hotServices) > 0 {
			result.hypotheses = append(result.hypotheses, domaincopilot.RootCauseHypothesis{
				ID:          "trace-hotspot:" + source.ID,
				Title:       localize(locale, "调用链热点集中在少数节点", "A few spans dominate the trace hotspots"),
				Summary:     localize(locale, fmt.Sprintf("热点 span 集中在这些节点: %s。", strings.Join(uniqueStrings(nil, hotServices), "、")), fmt.Sprintf("Hotspot spans are concentrated around these nodes: %s.", strings.Join(uniqueStrings(nil, hotServices), ", "))),
				Confidence:  71,
				EvidenceIDs: collectEvidenceIDs(sourceEvidence...),
				Recommendations: []string{
					localize(locale, "检查热点 span 的上下游依赖和错误标记。", "Inspect the upstream and downstream dependencies behind the hotspot spans."),
					localize(locale, "结合日志与指标确认热点节点是否就是故障中心。", "Correlate the hotspot nodes with logs and metrics to confirm whether they are the failure center."),
				},
			})
		}
	}
	result.recommendations = uniqueStrings(result.recommendations, flattenRecommendations(result.hypotheses))
	return result
}
