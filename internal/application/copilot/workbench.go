package copilot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	domaincopilot "github.com/kubecrux/kubecrux/internal/domain/copilot"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	mcpmetrics "github.com/kubecrux/kubecrux/internal/infrastructure/mcp/metrics"
	mcptraces "github.com/kubecrux/kubecrux/internal/infrastructure/mcp/traces"
)

func normalizeSessionMode(mode string) string {
	switch strings.TrimSpace(mode) {
	case "root_cause", "performance", "trace", "inspection_review", "general":
		return strings.TrimSpace(mode)
	default:
		return "general"
	}
}

func normalizeStringList(items []string) []string {
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		out = append(out, trimmed)
	}
	return out
}

func sessionMetadataMap(metadata domaincopilot.SessionMetadata) map[string]any {
	return map[string]any{
		"mode":            metadata.Mode,
		"status":          metadata.Status,
		"scope":           metadata.Scope,
		"pinnedContext":   metadata.PinnedContext,
		"toolset":         metadata.Toolset,
		"analysisRunRefs": metadata.AnalysisRunRefs,
		"summary":         metadata.Summary,
		"tags":            metadata.Tags,
		"archivedAt":      metadata.ArchivedAt,
		"source":          metadata.Source,
	}
}

func parseSessionMetadata(input map[string]any) domaincopilot.SessionMetadata {
	metadata := domaincopilot.SessionMetadata{}
	if input == nil {
		return metadata
	}
	metadata.Mode = stringValue(input["mode"])
	metadata.Status = stringValue(input["status"])
	metadata.Summary = stringValue(input["summary"])
	metadata.ArchivedAt = stringValue(input["archivedAt"])
	metadata.Source = stringValue(input["source"])
	if tags, ok := input["tags"].([]any); ok {
		values := make([]string, 0, len(tags))
		for _, item := range tags {
			values = append(values, fmt.Sprint(item))
		}
		metadata.Tags = normalizeStringList(values)
	}
	if scope, ok := input["scope"].(map[string]any); ok {
		metadata.Scope = scopeFromMap(scope)
	}
	if pinnedContext, ok := input["pinnedContext"].(map[string]any); ok {
		metadata.PinnedContext = pinnedContext
	}
	if toolset, ok := input["toolset"].(map[string]any); ok {
		metadata.Toolset = toolsetFromMap(toolset)
	}
	if refs, ok := input["analysisRunRefs"].([]any); ok {
		items := make([]domaincopilot.AnalysisRunRef, 0, len(refs))
		for _, item := range refs {
			current, ok := item.(map[string]any)
			if !ok {
				continue
			}
			items = append(items, domaincopilot.AnalysisRunRef{
				ID:        stringValue(current["id"]),
				Kind:      stringValue(current["kind"]),
				Status:    stringValue(current["status"]),
				CreatedAt: stringValue(current["createdAt"]),
			})
		}
		metadata.AnalysisRunRefs = items
	}
	return metadata
}

func scopeFromMap(scope map[string]any) domaincopilot.SessionScope {
	if scope == nil {
		return domaincopilot.SessionScope{}
	}
	return domaincopilot.SessionScope{
		ClusterID:        stringValue(scope["clusterId"]),
		Namespace:        stringValue(scope["namespace"]),
		Workload:         stringValue(scope["workload"]),
		Service:          stringValue(scope["service"]),
		AlertID:          stringValue(scope["alertId"]),
		TimeRangeMinutes: intValue(scope["timeRangeMinutes"], 60),
	}
}

func toolsetFromMap(toolset map[string]any) domaincopilot.SessionToolset {
	if toolset == nil {
		return domaincopilot.SessionToolset{}
	}
	return domaincopilot.SessionToolset{
		EnabledAdapterIDs: normalizeStringList(anyListToStrings(toolset["enabledAdapterIds"])),
		EnabledSkillIDs:   normalizeStringList(anyListToStrings(toolset["enabledSkillIds"])),
		DisabledToolNames: normalizeStringList(anyListToStrings(toolset["disabledToolNames"])),
		BudgetOverrides:   mapValue(toolset["budgetOverrides"]),
		ScopeOverrides:    mapValue(toolset["scopeOverrides"]),
	}
}

func anyListToStrings(value any) []string {
	items, ok := value.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		out = append(out, fmt.Sprint(item))
	}
	return out
}

func mapValue(value any) map[string]any {
	current, ok := value.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return current
}

func stringValue(value any) string {
	current, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(current)
}

func intValue(value any, fallback int) int {
	switch current := value.(type) {
	case int:
		return current
	case int32:
		return int(current)
	case int64:
		return int(current)
	case float64:
		return int(current)
	case string:
		trimmed := strings.TrimSpace(current)
		if trimmed == "" {
			return fallback
		}
		var number int
		_, err := fmt.Sscanf(trimmed, "%d", &number)
		if err == nil {
			return number
		}
	}
	return fallback
}

func (s *Service) analyzeConversation(ctx context.Context, principal domainidentity.Principal, session domaincopilot.Session, prompt, locale string) ([]domaincopilot.ToolExecution, []domaincopilot.AnalysisArtifact, map[string]any) {
	metadata := parseSessionMetadata(session.Metadata)
	scope := metadata.Scope
	toolCalls := make([]domaincopilot.ToolExecution, 0)
	artifacts := make([]domaincopilot.AnalysisArtifact, 0)
	refs := append([]domaincopilot.AnalysisRunRef{}, metadata.AnalysisRunRefs...)

	switch metadata.Mode {
	case "root_cause":
		run, toolCall, artifact, err := s.runSessionRootCause(ctx, principal, session.ID, scope, prompt, locale)
		if err == nil {
			toolCalls = append(toolCalls, toolCall...)
			artifacts = append(artifacts, artifact)
			refs = append(refs, domaincopilot.AnalysisRunRef{ID: run.ID, Kind: run.Kind, Status: run.Status, CreatedAt: run.CreatedAt.Format(time.RFC3339)})
		}
	case "performance":
		toolExecution, artifact, err := s.runSessionPerformance(ctx, session.ID, scope, prompt)
		if err == nil {
			toolCalls = append(toolCalls, toolExecution...)
			artifacts = append(artifacts, artifact)
			refs = append(refs, domaincopilot.AnalysisRunRef{ID: artifact.RunID, Kind: artifact.Kind, Status: "completed", CreatedAt: time.Now().UTC().Format(time.RFC3339)})
		}
	case "trace":
		toolExecution, artifact, err := s.runSessionTrace(ctx, session.ID, scope, prompt)
		if err == nil {
			toolCalls = append(toolCalls, toolExecution...)
			artifacts = append(artifacts, artifact)
			refs = append(refs, domaincopilot.AnalysisRunRef{ID: artifact.RunID, Kind: artifact.Kind, Status: "completed", CreatedAt: time.Now().UTC().Format(time.RFC3339)})
		}
	}

	patch := map[string]any{}
	if len(artifacts) > 0 {
		patch["summary"] = artifacts[0].Summary
		patch["analysisRunRefs"] = refs
	}
	return toolCalls, artifacts, patch
}

func (s *Service) runSessionRootCause(ctx context.Context, principal domainidentity.Principal, sessionID string, scope domaincopilot.SessionScope, prompt, locale string) (domaincopilot.RootCauseRun, []domaincopilot.ToolExecution, domaincopilot.AnalysisArtifact, error) {
	run, err := s.RunRootCauseAnalysis(ctx, principal, domaincopilot.RootCauseRunInput{
		Kind:             "root_cause",
		SessionID:        sessionID,
		ClusterID:        scope.ClusterID,
		Namespace:        scope.Namespace,
		WorkloadName:     scope.Workload,
		AlertID:          scope.AlertID,
		TimeRangeMinutes: scope.TimeRangeMinutes,
		Question:         prompt,
	}, locale)
	if err != nil {
		return domaincopilot.RootCauseRun{}, nil, domaincopilot.AnalysisArtifact{}, err
	}
	artifact := domaincopilot.AnalysisArtifact{
		Kind:               "root_cause",
		RunID:              run.ID,
		Title:              run.Title,
		Summary:            run.Summary,
		Scope:              scope,
		Evidence:           run.Evidence,
		Hypotheses:         run.Hypotheses,
		Recommendations:    run.Recommendations,
		ToolExecutions:     run.ToolExecutions,
		DataSourceSnapshot: run.DataSourceSnapshot,
	}
	return run, run.ToolExecutions, artifact, nil
}

func (s *Service) runSessionPerformance(ctx context.Context, sessionID string, scope domaincopilot.SessionScope, prompt string) ([]domaincopilot.ToolExecution, domaincopilot.AnalysisArtifact, error) {
	dataSources, err := s.repo.ListDataSources(ctx)
	if err != nil {
		return nil, domaincopilot.AnalysisArtifact{}, err
	}
	for _, source := range dataSources {
		if !source.Enabled || source.SourceKind != "metrics" || source.MCPAdapter != "metrics.v1" {
			continue
		}
		timeTo := time.Now().UTC()
		timeFrom := timeTo.Add(-time.Duration(sessionScopeTimeRange(scope)) * time.Minute)
		summary, err := mcpmetrics.DefaultRegistry().Analyze(ctx, source.BackendType, source.ID, source.Config, mcpmetrics.RangeQuery{
			Scope:      mcpmetrics.Scope{ClusterID: scope.ClusterID, Namespace: scope.Namespace, Workload: scope.Workload, Service: scope.Service},
			MetricKey:  "",
			TimeFrom:   timeFrom,
			TimeTo:     timeTo,
			Step:       time.Minute,
		})
		if err != nil {
			return nil, domaincopilot.AnalysisArtifact{}, err
		}
		now := time.Now().UTC()
		tool := domaincopilot.ToolExecution{
			ID:         "tool:" + uuid.NewString(),
			AdapterID:  "metrics.v1",
			ToolName:   "metrics.anomaly_summary",
			Status:     "success",
			Summary:    summary.Summary,
			Input:      map[string]any{"prompt": prompt, "scope": scope},
			Output:     map[string]any{"signals": summary.Signals},
			StartedAt:  now,
			CompletedAt: &now,
		}
		evidence := make([]domaincopilot.RootCauseEvidence, 0, len(summary.Signals))
		for _, item := range summary.Signals {
			evidence = append(evidence, domaincopilot.RootCauseEvidence{
				ID:      fmt.Sprintf("metrics:%s:%s", source.ID, item["metricKey"]),
				Kind:    "metrics.signal",
				Title:   fmt.Sprintf("%v", item["label"]),
				Summary: fmt.Sprintf("latest=%v average=%v trend=%v", item["latest"], item["average"], item["trend"]),
				Attributes: item,
			})
		}
		runID := "perf:" + uuid.NewString()
		run := domaincopilot.RootCauseRun{
			ID:                 runID,
			Kind:               "performance",
			SessionID:          sessionID,
			Title:              "Performance Analysis",
			CreatedBy:          "session:" + sessionID,
			Status:             "completed",
			Severity:           deriveArtifactSeverity(evidence),
			Summary:            summary.Summary,
			ClusterID:          scope.ClusterID,
			Namespace:          scope.Namespace,
			WorkloadName:       scope.Workload,
			AlertID:            scope.AlertID,
			TimeRangeMinutes:   sessionScopeTimeRange(scope),
			Question:           prompt,
			Evidence:           evidence,
			Recommendations:    []string{"Review the top spiking metrics and compare them against deployment changes."},
			ToolExecutions:     []domaincopilot.ToolExecution{tool},
			DataSourceSnapshot: map[string]any{"sourceId": source.ID, "backendType": source.BackendType},
			PlaybookResults:    map[string]any{"metrics.anomaly_summary": summary.Signals},
			RemediationPlan:    map[string]any{"policy": "suggest_only"},
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		_, _ = s.repo.CreateRootCauseRun(ctx, run)
		return []domaincopilot.ToolExecution{tool}, domaincopilot.AnalysisArtifact{
			Kind: "performance",
			RunID: runID,
			Title: "Performance Analysis",
			Summary: summary.Summary,
			Scope: scope,
			Evidence: evidence,
			Recommendations: run.Recommendations,
			ToolExecutions: []domaincopilot.ToolExecution{tool},
			DataSourceSnapshot: map[string]any{"sourceId": source.ID, "backendType": source.BackendType},
		}, nil
	}
	return nil, domaincopilot.AnalysisArtifact{}, fmt.Errorf("no enabled metrics.v1 data source found")
}

func (s *Service) runSessionTrace(ctx context.Context, sessionID string, scope domaincopilot.SessionScope, prompt string) ([]domaincopilot.ToolExecution, domaincopilot.AnalysisArtifact, error) {
	dataSources, err := s.repo.ListDataSources(ctx)
	if err != nil {
		return nil, domaincopilot.AnalysisArtifact{}, err
	}
	for _, source := range dataSources {
		if !source.Enabled || source.SourceKind != "traces" || source.MCPAdapter != "traces.v1" {
			continue
		}
		timeTo := time.Now().UTC()
		timeFrom := timeTo.Add(-time.Duration(sessionScopeTimeRange(scope)) * time.Minute)
		result, err := mcptraces.DefaultRegistry().FindSlowSpans(ctx, source.BackendType, source.ID, source.Config, mcptraces.Query{
			Scope:       mcptraces.Scope{ClusterID: scope.ClusterID, Namespace: scope.Namespace, Service: scope.Service, Workload: scope.Workload},
			TimeFrom:    timeFrom,
			TimeTo:      timeTo,
			MinDuration: 250 * time.Millisecond,
			Limit:       20,
		})
		if err != nil {
			return nil, domaincopilot.AnalysisArtifact{}, err
		}
		now := time.Now().UTC()
		tool := domaincopilot.ToolExecution{
			ID:         "tool:" + uuid.NewString(),
			AdapterID:  "traces.v1",
			ToolName:   "traces.find_slow_spans",
			Status:     "success",
			Summary:    result.Summary,
			Input:      map[string]any{"prompt": prompt, "scope": scope},
			Output:     map[string]any{"hotspots": result.Hotspots},
			StartedAt:  now,
			CompletedAt: &now,
		}
		evidence := make([]domaincopilot.RootCauseEvidence, 0, len(result.Spans))
		for index, item := range result.Spans {
			evidence = append(evidence, domaincopilot.RootCauseEvidence{
				ID:      fmt.Sprintf("trace:%s:%d", source.ID, index+1),
				Kind:    "trace.span",
				Title:   fmt.Sprintf("%s / %s", item.Service, item.Operation),
				Summary: fmt.Sprintf("duration=%.2fms trace=%s", item.DurationMS, item.TraceID),
				Attributes: map[string]any{
					"traceId":    item.TraceID,
					"spanId":     item.SpanID,
					"durationMs": item.DurationMS,
					"error":      item.Error,
					"tags":       item.Tags,
				},
			})
		}
		runID := "trace:" + uuid.NewString()
		run := domaincopilot.RootCauseRun{
			ID:                 runID,
			Kind:               "trace",
			SessionID:          sessionID,
			Title:              "Trace Analysis",
			CreatedBy:          "session:" + sessionID,
			Status:             "completed",
			Severity:           deriveArtifactSeverity(evidence),
			Summary:            result.Summary,
			ClusterID:          scope.ClusterID,
			Namespace:          scope.Namespace,
			WorkloadName:       scope.Workload,
			AlertID:            scope.AlertID,
			TimeRangeMinutes:   sessionScopeTimeRange(scope),
			Question:           prompt,
			Evidence:           evidence,
			Recommendations:    []string{"Review the slowest spans and compare them against downstream dependency errors."},
			ToolExecutions:     []domaincopilot.ToolExecution{tool},
			DataSourceSnapshot: map[string]any{"sourceId": source.ID, "backendType": source.BackendType, "hotspots": result.Hotspots},
			PlaybookResults:    map[string]any{"traces.find_slow_spans": result.Hotspots},
			RemediationPlan:    map[string]any{"policy": "suggest_only"},
			CreatedAt:          now,
			UpdatedAt:          now,
		}
		_, _ = s.repo.CreateRootCauseRun(ctx, run)
		return []domaincopilot.ToolExecution{tool}, domaincopilot.AnalysisArtifact{
			Kind: "trace",
			RunID: runID,
			Title: "Trace Analysis",
			Summary: result.Summary,
			Scope: scope,
			Evidence: evidence,
			Recommendations: run.Recommendations,
			ToolExecutions: []domaincopilot.ToolExecution{tool},
			DataSourceSnapshot: map[string]any{"sourceId": source.ID, "backendType": source.BackendType, "hotspots": result.Hotspots},
		}, nil
	}
	return nil, domaincopilot.AnalysisArtifact{}, fmt.Errorf("no enabled traces.v1 data source found")
}

func sessionScopeTimeRange(scope domaincopilot.SessionScope) int {
	if scope.TimeRangeMinutes > 0 {
		return scope.TimeRangeMinutes
	}
	return 60
}

func deriveArtifactSeverity(evidence []domaincopilot.RootCauseEvidence) string {
	best := "info"
	rank := map[string]int{"info": 1, "warning": 2, "critical": 3, "error": 3}
	bestRank := 1
	for _, item := range evidence {
		current := strings.TrimSpace(item.Severity)
		if current == "" {
			continue
		}
		if rank[current] > bestRank {
			best = current
			bestRank = rank[current]
		}
	}
	return best
}
