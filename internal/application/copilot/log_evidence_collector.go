package copilot

import (
	"context"
	"fmt"
	"strings"
	"time"

	domaincopilot "github.com/soha/soha/internal/domain/copilot"
	mcplogs "github.com/soha/soha/internal/infrastructure/mcp/logs"
)

type logEvidenceAnalysis struct {
	evidence        []domaincopilot.RootCauseEvidence
	hypotheses      []domaincopilot.RootCauseHypothesis
	recommendations []string
	playbookResults map[string]any
	toolExecutions  []domaincopilot.ToolExecution
}

func (s *Service) collectRootCauseLogEvidence(ctx context.Context, input domaincopilot.RootCauseRunInput, profile domaincopilot.AnalysisProfile, toolset domaincopilot.SessionToolset, locale string) logEvidenceAnalysis {
	result := logEvidenceAnalysis{
		evidence:        []domaincopilot.RootCauseEvidence{},
		hypotheses:      []domaincopilot.RootCauseHypothesis{},
		recommendations: []string{},
		playbookResults: map[string]any{},
	}
	const adapterID = "logs.v1"
	const toolName = "logs.correlation"
	if !toolsetAllowsTool(toolset, adapterID, toolName) {
		result.playbookResults["logs"] = "tool_disabled"
		return result
	}
	dataSources, err := s.repo.ListDataSources(ctx)
	if err != nil {
		result.playbookResults["logs"] = "datasource_list_failed"
		return result
	}
	limit := evidenceBudget(toolset, 20)
	timeTo := time.Now().UTC()
	timeFrom := timeTo.Add(-time.Duration(input.TimeRangeMinutes) * time.Minute)
	for _, source := range dataSources {
		if !source.Enabled || source.SourceKind != "logs" || source.MCPAdapter != adapterID || !toolsetAllowsAdapter(toolset, source.MCPAdapter) {
			continue
		}
		if len(profile.EnabledSources) > 0 && !containsString(profile.EnabledSources, source.ID) && !containsString(profile.EnabledSources, "logs") {
			continue
		}
		correlation, err := mcplogs.DefaultRegistry().Correlate(ctx, source.BackendType, source.ID, source.Config, mcplogs.CorrelationQuery{
			Scope: mcplogs.Scope{
				ClusterID: input.ClusterID,
				Namespace: input.Namespace,
				Workload:  input.WorkloadName,
			},
			AlertID:  input.AlertID,
			Workload: input.WorkloadName,
			TimeFrom: timeFrom,
			TimeTo:   timeTo,
			Query:    input.Question,
			Limit:    limit,
		})
		if err != nil {
			result.playbookResults["logs:"+source.ID] = "query_failed"
			continue
		}
		if len(correlation.Records) == 0 && len(correlation.Signatures) == 0 {
			result.playbookResults["logs:"+source.ID] = "no_matches"
			continue
		}
		result.playbookResults["logs:"+source.ID] = "matched"
		now := time.Now().UTC()
		signatures := correlation.Signatures
		if limit > 0 && len(signatures) > limit {
			signatures = signatures[:limit]
		}
		sampleLimit := limit
		if sampleLimit <= 0 || sampleLimit > 5 {
			sampleLimit = 5
		}
		result.toolExecutions = append(result.toolExecutions, domaincopilot.ToolExecution{
			ID:          "tool:logs:" + source.ID + ":" + now.Format("150405.000"),
			AdapterID:   adapterID,
			ToolName:    toolName,
			Status:      "success",
			Summary:     correlation.Summary,
			Input:       map[string]any{"scope": input, "sourceId": source.ID},
			Output:      map[string]any{"signatures": signatures, "records": buildSampleRecordAttributes(correlation.Records, sampleLimit)},
			StartedAt:   now,
			CompletedAt: &now,
		})
		signatureEvidence := make([]domaincopilot.RootCauseEvidence, 0, len(signatures))
		for index, item := range signatures {
			attributes := map[string]any{
				"sourceId":      source.ID,
				"backendType":   source.BackendType,
				"count":         item.Count,
				"queryCost":     correlation.QueryCost,
				"sampleWindow":  correlation.SampleWindow,
				"truncated":     correlation.Truncated,
				"sampleRecords": buildSampleRecordAttributes(correlation.Records, 3),
				"clusterId":     input.ClusterID,
				"namespace":     input.Namespace,
				"workload":      input.WorkloadName,
				"service":       firstNonEmptyCorrelationService(correlation.Records, input.WorkloadName),
			}
			if strings.TrimSpace(correlation.ErrorKind) != "" {
				attributes["errorKind"] = correlation.ErrorKind
			}
			evidence := domaincopilot.RootCauseEvidence{
				ID:         fmt.Sprintf("logs:%s:%d", source.ID, index+1),
				Kind:       "logs.signature",
				Title:      item.Signature,
				Summary:    item.Sample,
				Severity:   normalizeLogSeverity(item.Severity),
				Attributes: attributes,
			}
			signatureEvidence = append(signatureEvidence, evidence)
			result.evidence = append(result.evidence, evidence)
		}
		if len(signatureEvidence) == 0 {
			continue
		}
		if likelyDependencyTimeout(signatures) {
			result.hypotheses = append(result.hypotheses, domaincopilot.RootCauseHypothesis{
				ID:          "dependency-timeout",
				Title:       localize(locale, "日志显示依赖超时或上游不可用", "Logs indicate dependency timeout or upstream unavailability"),
				Summary:     localize(locale, "相关日志中出现超时、连接拒绝或上游不可用特征，建议优先检查依赖链路。", "Correlated logs contain timeout, connection refused, or upstream unavailable patterns. Check dependencies first."),
				Confidence:  74,
				EvidenceIDs: collectEvidenceIDs(signatureEvidence...),
				Recommendations: []string{
					localize(locale, "检查上游服务健康状态和最近变更。", "Check upstream service health and recent changes."),
					localize(locale, "比对超时日志与告警触发时间是否一致。", "Compare timeout logs against the alert trigger timeline."),
				},
			})
			result.playbookResults["dependency-timeout"] = "matched"
		}
		if hasErrorBurst(signatures) {
			result.hypotheses = append(result.hypotheses, domaincopilot.RootCauseHypothesis{
				ID:          "error-burst",
				Title:       localize(locale, "应用日志存在错误突增", "Application logs show an error burst"),
				Summary:     localize(locale, "同类错误签名在短时间内大量出现，问题可能已经在应用层集中爆发。", "The same error signatures are spiking within a short window, suggesting the fault is concentrated in the application layer."),
				Confidence:  69,
				EvidenceIDs: collectEvidenceIDs(signatureEvidence...),
				Recommendations: []string{
					localize(locale, "优先核对最新异常签名对应的代码路径和依赖调用。", "Review the code path and dependency calls behind the top signatures."),
				},
			})
			result.playbookResults["error-burst"] = "matched"
		}
	}
	result.recommendations = uniqueStrings(result.recommendations, flattenRecommendations(result.hypotheses))
	return result
}

func normalizeLogSeverity(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case "critical", "fatal", "panic":
		return "critical"
	case "warn", "warning", "error":
		return "warning"
	default:
		return "info"
	}
}

func likelyDependencyTimeout(signatures []mcplogs.Signature) bool {
	for _, item := range signatures {
		signature := strings.ToLower(item.Signature + " " + item.Sample)
		for _, marker := range []string{"timeout", "timed out", "connection refused", "upstream", "unavailable"} {
			if strings.Contains(signature, marker) {
				return true
			}
		}
	}
	return false
}

func hasErrorBurst(signatures []mcplogs.Signature) bool {
	for _, item := range signatures {
		if item.Count >= 3 {
			return true
		}
	}
	return false
}

func buildSampleRecordAttributes(records []mcplogs.Record, limit int) []map[string]any {
	if limit <= 0 || len(records) == 0 {
		return nil
	}
	if len(records) > limit {
		records = records[:limit]
	}
	items := make([]map[string]any, 0, len(records))
	for _, item := range records {
		items = append(items, map[string]any{
			"timestamp": item.Timestamp.Format(time.RFC3339),
			"severity":  item.Severity,
			"service":   item.Service,
			"workload":  item.Workload,
			"namespace": item.Namespace,
			"clusterId": item.ClusterID,
			"message":   item.Message,
		})
	}
	return items
}

func firstNonEmptyCorrelationService(records []mcplogs.Record, fallback string) string {
	for _, item := range records {
		if strings.TrimSpace(item.Service) != "" {
			return strings.TrimSpace(item.Service)
		}
	}
	return strings.TrimSpace(fallback)
}
