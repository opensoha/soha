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
	traceEvidenceAdapterID = "traces.v1"
	traceEvidenceToolName  = "traces.find_slow_spans"
)

type traceEvidenceAnalysis struct {
	evidence        []domaincopilot.RootCauseEvidence
	hypotheses      []domaincopilot.RootCauseHypothesis
	recommendations []string
	playbookResults map[string]any
	toolExecutions  []domaincopilot.ToolExecution
}

type traceEvidenceRequest struct {
	input    domaincopilot.RootCauseRunInput
	source   domaincopilot.DataSource
	limit    int
	timeFrom time.Time
	timeTo   time.Time
	locale   string
}

func (s *Service) collectRootCauseTraceEvidence(ctx context.Context, input domaincopilot.RootCauseRunInput, profile domaincopilot.AnalysisProfile, toolset domaincopilot.SessionToolset, locale string) traceEvidenceAnalysis {
	result := traceEvidenceAnalysis{
		evidence:        []domaincopilot.RootCauseEvidence{},
		hypotheses:      []domaincopilot.RootCauseHypothesis{},
		recommendations: []string{},
		playbookResults: map[string]any{},
		toolExecutions:  []domaincopilot.ToolExecution{},
	}
	setup, status := s.prepareEvidenceCollection(ctx, evidenceCollectionSetupRequest{
		input: input, toolset: toolset,
		adapterID: traceEvidenceAdapterID, toolName: traceEvidenceToolName,
	})
	if status != "" {
		result.playbookResults["traces"] = status
		return result
	}
	for _, source := range setup.dataSources {
		if !traceEvidenceSourceEnabled(source, profile, toolset, traceEvidenceAdapterID) {
			continue
		}
		s.collectTraceSourceEvidence(ctx, traceEvidenceRequest{
			input: input, source: source, limit: setup.limit,
			timeFrom: setup.timeFrom, timeTo: setup.timeTo, locale: locale,
		}, &result)
	}
	result.recommendations = uniqueStrings(result.recommendations, flattenRecommendations(result.hypotheses))
	return result
}

func (s *Service) collectTraceSourceEvidence(ctx context.Context, request traceEvidenceRequest, result *traceEvidenceAnalysis) {
	source := request.source
	traceResult, err := s.traceBackend().FindSlowSpans(ctx, source.BackendType, source.ID, source.Config, telemetry.TraceQuery{
		Scope: telemetry.TraceScope{
			ClusterID: request.input.ClusterID,
			Namespace: request.input.Namespace,
			Service:   request.input.WorkloadName,
			Workload:  request.input.WorkloadName,
		},
		TimeFrom:    request.timeFrom,
		TimeTo:      request.timeTo,
		MinDuration: 250 * time.Millisecond,
		Limit:       request.limit,
	})
	if err != nil {
		result.playbookResults["traces:"+source.ID] = "query_failed"
		return
	}
	spans := limitSpans(traceResult.Spans, request.limit)
	now := time.Now().UTC()
	result.toolExecutions = append(result.toolExecutions, domaincopilot.ToolExecution{
		ID:          "tool:" + uuid.NewString(),
		AdapterID:   traceEvidenceAdapterID,
		ToolName:    traceEvidenceToolName,
		Status:      "success",
		Summary:     traceResult.Summary,
		Input:       map[string]any{"scope": request.input, "sourceId": source.ID},
		Output:      map[string]any{"hotspots": traceResult.Hotspots},
		StartedAt:   now,
		CompletedAt: &now,
	})
	if len(spans) == 0 {
		result.playbookResults["traces:"+source.ID] = "no_spans"
		return
	}
	result.playbookResults["traces:"+source.ID] = "matched"
	sourceEvidence, hotServices := buildTraceSpanEvidence(request.input, source, spans)
	result.evidence = append(result.evidence, sourceEvidence...)
	if len(hotServices) > 0 {
		result.hypotheses = append(result.hypotheses, traceHotspotHypothesis(request.locale, source.ID, hotServices, sourceEvidence))
	}
}

func buildTraceSpanEvidence(input domaincopilot.RootCauseRunInput, source domaincopilot.DataSource, spans []telemetry.TraceSpan) ([]domaincopilot.RootCauseEvidence, []string) {
	evidenceItems := make([]domaincopilot.RootCauseEvidence, 0, len(spans))
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
		evidenceItems = append(evidenceItems, evidence)
	}
	return evidenceItems, hotServices
}

func traceHotspotHypothesis(locale, sourceID string, hotServices []string, evidence []domaincopilot.RootCauseEvidence) domaincopilot.RootCauseHypothesis {
	return domaincopilot.RootCauseHypothesis{
		ID:          "trace-hotspot:" + sourceID,
		Title:       localize(locale, "调用链热点集中在少数节点", "A few spans dominate the trace hotspots"),
		Summary:     localize(locale, fmt.Sprintf("热点 span 集中在这些节点: %s。", strings.Join(uniqueStrings(nil, hotServices), "、")), fmt.Sprintf("Hotspot spans are concentrated around these nodes: %s.", strings.Join(uniqueStrings(nil, hotServices), ", "))),
		Confidence:  71,
		EvidenceIDs: collectEvidenceIDs(evidence...),
		Recommendations: []string{
			localize(locale, "检查热点 span 的上下游依赖和错误标记。", "Inspect the upstream and downstream dependencies behind the hotspot spans."),
			localize(locale, "结合日志与指标确认热点节点是否就是故障中心。", "Correlate the hotspot nodes with logs and metrics to confirm whether they are the failure center."),
		},
	}
}

func traceEvidenceSourceEnabled(source domaincopilot.DataSource, profile domaincopilot.AnalysisProfile, toolset domaincopilot.SessionToolset, adapterID string) bool {
	if !source.Enabled || source.SourceKind != "traces" || source.MCPAdapter != adapterID || !toolsetAllowsAdapter(toolset, source.MCPAdapter) {
		return false
	}
	return len(profile.EnabledSources) == 0 || containsString(profile.EnabledSources, source.ID) || containsString(profile.EnabledSources, "traces")
}
