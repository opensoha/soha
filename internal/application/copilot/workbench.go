package copilot

import (
	"context"
	"fmt"
	"sort"
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
	graph := buildRootCauseGraph(scope, run.Evidence, run.Hypotheses, run.DataSourceSnapshot)
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
		Graph:              graph,
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
		graph := buildMetricsGraph(scope, source.ID, summary.Signals, evidence)
		return []domaincopilot.ToolExecution{tool}, domaincopilot.AnalysisArtifact{
			Kind:            "performance",
			RunID:           runID,
			Title:           "Performance Analysis",
			Summary:         summary.Summary,
			Scope:           scope,
			Evidence:        evidence,
			Recommendations: run.Recommendations,
			ToolExecutions:  []domaincopilot.ToolExecution{tool},
			Graph:           graph,
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
					"traceId":      item.TraceID,
					"spanId":       item.SpanID,
					"parentSpanId": item.ParentSpanID,
					"durationMs":   item.DurationMS,
					"error":        item.Error,
					"tags":         item.Tags,
					"service":      item.Service,
					"operation":    item.Operation,
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
		graph := buildTraceGraph(scope, source.ID, result.Spans, evidence)
		return []domaincopilot.ToolExecution{tool}, domaincopilot.AnalysisArtifact{
			Kind:            "trace",
			RunID:           runID,
			Title:           "Trace Analysis",
			Summary:         result.Summary,
			Scope:           scope,
			Evidence:        evidence,
			Recommendations: run.Recommendations,
			ToolExecutions:  []domaincopilot.ToolExecution{tool},
			Graph:           graph,
			DataSourceSnapshot: map[string]any{"sourceId": source.ID, "backendType": source.BackendType, "hotspots": result.Hotspots},
		}, nil
	}
	return nil, domaincopilot.AnalysisArtifact{}, fmt.Errorf("no enabled traces.v1 data source found")
}

func buildRootCauseGraph(scope domaincopilot.SessionScope, evidence []domaincopilot.RootCauseEvidence, hypotheses []domaincopilot.RootCauseHypothesis, snapshot map[string]any) *domaincopilot.AnalysisGraph {
	nodes := make([]domaincopilot.AnalysisGraphNode, 0)
	edges := make([]domaincopilot.AnalysisGraphEdge, 0)
	rootID := graphRootNodeID(scope)
	nodes = append(nodes, domaincopilot.AnalysisGraphNode{
		ID:       rootID,
		Kind:     "scope",
		Title:    graphRootTitle(scope),
		Subtitle: graphRootSubtitle(scope),
		SourceRefs: []string{"platform-native.v1"},
		Attributes: map[string]any{
			"clusterId": scope.ClusterID,
			"namespace": scope.Namespace,
			"workload":  scope.Workload,
			"alertId":   scope.AlertID,
		},
	})

	serviceNodeIDs := map[string]string{}
	traceNodeIDs := map[string]string{}
	logNodeIDs := map[string]string{}
	metricNodeIDs := map[string]string{}
	evidenceNodeIDs := map[string]string{}

	for _, item := range evidence {
		switch item.Kind {
		case "trace.span":
			service := strings.TrimSpace(fmt.Sprint(item.Attributes["service"]))
			operation := strings.TrimSpace(fmt.Sprint(item.Attributes["operation"]))
			traceID := strings.TrimSpace(fmt.Sprint(item.Attributes["traceId"]))
			parentSpanID := strings.TrimSpace(fmt.Sprint(item.Attributes["parentSpanId"]))
			serviceNodeID := ensureGraphServiceNode(&nodes, &edges, serviceNodeIDs, rootID, service, item)
			traceNodeID := "trace:" + traceID + ":" + strings.TrimSpace(fmt.Sprint(item.Attributes["spanId"]))
			traceNodeIDs[strings.TrimSpace(fmt.Sprint(item.Attributes["spanId"]))] = traceNodeID
			evidenceNodeIDs[item.ID] = traceNodeID
			nodes = appendGraphNode(nodes, domaincopilot.AnalysisGraphNode{
				ID:         traceNodeID,
				Kind:       "span",
				Title:      firstNonEmpty(operation, item.Title),
				Subtitle:   item.Summary,
				Severity:   item.Severity,
				EvidenceIDs: []string{item.ID},
				SourceRefs: []string{"traces.v1"},
				Attributes: item.Attributes,
			})
			edges = appendGraphEdge(edges, domaincopilot.AnalysisGraphEdge{
				ID:         serviceNodeID + "->" + traceNodeID,
				Source:     serviceNodeID,
				Target:     traceNodeID,
				Relation:   "contains",
				Severity:   item.Severity,
				EvidenceIDs: []string{item.ID},
			})
			if parentSpanID != "" {
				if parentNodeID, ok := traceNodeIDs[parentSpanID]; ok {
					edges = appendGraphEdge(edges, domaincopilot.AnalysisGraphEdge{
						ID:         parentNodeID + "->" + traceNodeID,
						Source:     parentNodeID,
						Target:     traceNodeID,
						Relation:   "calls",
						Severity:   item.Severity,
						EvidenceIDs: []string{item.ID},
					})
				}
			}
		case "logs.signature":
			service := strings.TrimSpace(fmt.Sprint(item.Attributes["service"]))
			workload := strings.TrimSpace(fmt.Sprint(item.Attributes["workload"]))
			anchorTitle := firstNonEmpty(service, workload, scope.Workload, "日志")
			anchorNodeID := ensureGraphServiceNode(&nodes, &edges, serviceNodeIDs, rootID, anchorTitle, item)
			logNodeID := "log:" + item.ID
			logNodeIDs[item.ID] = logNodeID
			evidenceNodeIDs[item.ID] = logNodeID
			nodes = appendGraphNode(nodes, domaincopilot.AnalysisGraphNode{
				ID:         logNodeID,
				Kind:       "log_signature",
				Title:      item.Title,
				Subtitle:   item.Summary,
				Severity:   item.Severity,
				EvidenceIDs: []string{item.ID},
				SourceRefs: []string{"logs.v1"},
				Attributes: item.Attributes,
			})
			edges = appendGraphEdge(edges, domaincopilot.AnalysisGraphEdge{
				ID:         anchorNodeID + "->" + logNodeID,
				Source:     anchorNodeID,
				Target:     logNodeID,
				Relation:   "emits",
				Severity:   item.Severity,
				EvidenceIDs: []string{item.ID},
			})
		case "metrics.signal":
			metricLabel := strings.TrimSpace(fmt.Sprint(item.Attributes["label"]))
			metricKey := strings.TrimSpace(fmt.Sprint(item.Attributes["metricKey"]))
			serviceNodeID := ensureGraphServiceNode(&nodes, &edges, serviceNodeIDs, rootID, firstNonEmpty(scope.Workload, scope.Service, "指标"), item)
			metricNodeID := "metric:" + firstNonEmpty(metricKey, item.ID)
			metricNodeIDs[item.ID] = metricNodeID
			evidenceNodeIDs[item.ID] = metricNodeID
			nodes = appendGraphNode(nodes, domaincopilot.AnalysisGraphNode{
				ID:         metricNodeID,
				Kind:       "metric_signal",
				Title:      firstNonEmpty(metricLabel, metricKey, item.Title),
				Subtitle:   item.Summary,
				Severity:   item.Severity,
				EvidenceIDs: []string{item.ID},
				SourceRefs: []string{"metrics.v1"},
				Attributes: item.Attributes,
			})
			edges = appendGraphEdge(edges, domaincopilot.AnalysisGraphEdge{
				ID:         serviceNodeID + "->" + metricNodeID,
				Source:     serviceNodeID,
				Target:     metricNodeID,
				Relation:   "measures",
				Severity:   item.Severity,
				EvidenceIDs: []string{item.ID},
			})
		default:
			nodeID := "evidence:" + item.ID
			evidenceNodeIDs[item.ID] = nodeID
			nodes = appendGraphNode(nodes, domaincopilot.AnalysisGraphNode{
				ID:         nodeID,
				Kind:       item.Kind,
				Title:      item.Title,
				Subtitle:   item.Summary,
				Severity:   item.Severity,
				EvidenceIDs: []string{item.ID},
				SourceRefs: []string{"platform-native.v1"},
				Attributes: item.Attributes,
			})
			edges = appendGraphEdge(edges, domaincopilot.AnalysisGraphEdge{
				ID:         rootID + "->" + nodeID,
				Source:     rootID,
				Target:     nodeID,
				Relation:   "observes",
				Severity:   item.Severity,
				EvidenceIDs: []string{item.ID},
			})
		}
	}

	for _, item := range hypotheses {
		hypothesisID := "hypothesis:" + item.ID
		nodes = appendGraphNode(nodes, domaincopilot.AnalysisGraphNode{
			ID:         hypothesisID,
			Kind:       "hypothesis",
			Title:      item.Title,
			Subtitle:   item.Summary,
			Severity:   confidenceSeverity(item.Confidence),
			EvidenceIDs: item.EvidenceIDs,
			SourceRefs: []string{"analysis"},
			Attributes: map[string]any{"confidence": item.Confidence},
		})
		edges = appendGraphEdge(edges, domaincopilot.AnalysisGraphEdge{
			ID:       rootID + "->" + hypothesisID,
			Source:   rootID,
			Target:   hypothesisID,
			Relation: "hypothesizes",
			Severity: confidenceSeverity(item.Confidence),
		})
		for _, evidenceID := range item.EvidenceIDs {
			if nodeID := graphEvidenceNodeID(evidenceID, evidenceNodeIDs, logNodeIDs, metricNodeIDs); nodeID != "" {
				edges = appendGraphEdge(edges, domaincopilot.AnalysisGraphEdge{
					ID:         nodeID + "->" + hypothesisID,
					Source:     nodeID,
					Target:     hypothesisID,
					Relation:   "supports",
					EvidenceIDs: []string{evidenceID},
				})
			}
		}
	}

	appendMissingSourceNodes(&nodes, &edges, rootID, snapshot)
	appendRecommendationNodes(&nodes, &edges, rootID, hypotheses)

	if len(nodes) == 0 {
		return nil
	}

	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	return &domaincopilot.AnalysisGraph{
		Layout:      "LR",
		FocusNodeID: rootID,
		Nodes:       nodes,
		Edges:       edges,
	}
}

func buildMetricsGraph(scope domaincopilot.SessionScope, sourceID string, signals []map[string]any, evidence []domaincopilot.RootCauseEvidence) *domaincopilot.AnalysisGraph {
	rootID := graphRootNodeID(scope)
	nodes := []domaincopilot.AnalysisGraphNode{{
		ID:         rootID,
		Kind:       "scope",
		Title:      graphRootTitle(scope),
		Subtitle:   graphRootSubtitle(scope),
		SourceRefs: []string{"metrics.v1", sourceID},
	}}
	edges := make([]domaincopilot.AnalysisGraphEdge, 0, len(signals))
	for _, item := range signals {
		id := "metric:" + strings.TrimSpace(fmt.Sprint(item["metricKey"]))
		nodes = appendGraphNode(nodes, domaincopilot.AnalysisGraphNode{
			ID:         id,
			Kind:       "metric_signal",
			Title:      firstNonEmpty(strings.TrimSpace(fmt.Sprint(item["label"])), strings.TrimSpace(fmt.Sprint(item["metricKey"]))),
			Subtitle:   fmt.Sprintf("latest=%v avg=%v trend=%v", item["latest"], item["average"], item["trend"]),
			Severity:   metricTrendSeverity(strings.TrimSpace(fmt.Sprint(item["trend"]))),
			SourceRefs: []string{"metrics.v1", sourceID},
			Attributes: item,
		})
		edges = appendGraphEdge(edges, domaincopilot.AnalysisGraphEdge{
			ID:       rootID + "->" + id,
			Source:   rootID,
			Target:   id,
			Relation: "measures",
			Severity: metricTrendSeverity(strings.TrimSpace(fmt.Sprint(item["trend"]))),
		})
	}
	return &domaincopilot.AnalysisGraph{
		Layout:      "LR",
		FocusNodeID: rootID,
		Nodes:       nodes,
		Edges:       edges,
	}
}

func buildTraceGraph(scope domaincopilot.SessionScope, sourceID string, spans []mcptraces.Span, evidence []domaincopilot.RootCauseEvidence) *domaincopilot.AnalysisGraph {
	rootID := graphRootNodeID(scope)
	nodes := []domaincopilot.AnalysisGraphNode{{
		ID:         rootID,
		Kind:       "scope",
		Title:      graphRootTitle(scope),
		Subtitle:   graphRootSubtitle(scope),
		SourceRefs: []string{"traces.v1", sourceID},
	}}
	edges := make([]domaincopilot.AnalysisGraphEdge, 0)
	serviceNodes := map[string]string{}
	spanNodeIDs := map[string]string{}
	for _, item := range spans {
		serviceNodeID := ensureGraphServiceNode(&nodes, &edges, serviceNodes, rootID, item.Service, domaincopilot.RootCauseEvidence{
			Severity: ternarySeverity(item.Error, "critical", "info"),
		})
		spanNodeID := "trace:" + item.TraceID + ":" + item.SpanID
		spanNodeIDs[item.SpanID] = spanNodeID
		nodes = appendGraphNode(nodes, domaincopilot.AnalysisGraphNode{
			ID:         spanNodeID,
			Kind:       "span",
			Title:      firstNonEmpty(item.Operation, item.Service, item.SpanID),
			Subtitle:   fmt.Sprintf("trace=%s duration=%.2fms", item.TraceID, item.DurationMS),
			Severity:   ternarySeverity(item.Error, "critical", "info"),
			SourceRefs: []string{"traces.v1", sourceID},
			Attributes: map[string]any{
				"traceId":      item.TraceID,
				"spanId":       item.SpanID,
				"parentSpanId": item.ParentSpanID,
				"durationMs":   item.DurationMS,
				"service":      item.Service,
				"operation":    item.Operation,
				"error":        item.Error,
				"tags":         item.Tags,
			},
		})
		edges = appendGraphEdge(edges, domaincopilot.AnalysisGraphEdge{
			ID:       serviceNodeID + "->" + spanNodeID,
			Source:   serviceNodeID,
			Target:   spanNodeID,
			Relation: "contains",
			Severity: ternarySeverity(item.Error, "critical", "info"),
		})
	}
	for _, item := range spans {
		if item.ParentSpanID == "" {
			continue
		}
		parentID, parentOK := spanNodeIDs[item.ParentSpanID]
		childID, childOK := spanNodeIDs[item.SpanID]
		if !parentOK || !childOK {
			continue
		}
		edges = appendGraphEdge(edges, domaincopilot.AnalysisGraphEdge{
			ID:       parentID + "->" + childID,
			Source:   parentID,
			Target:   childID,
			Relation: "calls",
			Severity: ternarySeverity(item.Error, "critical", "info"),
		})
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].ID < nodes[j].ID })
	sort.Slice(edges, func(i, j int) bool { return edges[i].ID < edges[j].ID })
	return &domaincopilot.AnalysisGraph{
		Layout:      "LR",
		FocusNodeID: rootID,
		Nodes:       nodes,
		Edges:       edges,
	}
}

func graphRootNodeID(scope domaincopilot.SessionScope) string {
	if scope.AlertID != "" {
		return "scope:alert:" + scope.AlertID
	}
	if scope.Workload != "" {
		return "scope:workload:" + scope.Workload
	}
	if scope.Service != "" {
		return "scope:service:" + scope.Service
	}
	if scope.Namespace != "" {
		return "scope:namespace:" + scope.Namespace
	}
	if scope.ClusterID != "" {
		return "scope:cluster:" + scope.ClusterID
	}
	return "scope:general"
}

func graphRootTitle(scope domaincopilot.SessionScope) string {
	return firstNonEmpty(scope.Workload, scope.Service, scope.AlertID, scope.Namespace, scope.ClusterID, "当前调查")
}

func graphRootSubtitle(scope domaincopilot.SessionScope) string {
	parts := []string{}
	if scope.ClusterID != "" {
		parts = append(parts, scope.ClusterID)
	}
	if scope.Namespace != "" {
		parts = append(parts, scope.Namespace)
	}
	if scope.Workload != "" {
		parts = append(parts, scope.Workload)
	}
	if scope.Service != "" {
		parts = append(parts, scope.Service)
	}
	if scope.AlertID != "" {
		parts = append(parts, "alert:"+scope.AlertID)
	}
	return strings.Join(parts, " / ")
}

func ensureGraphServiceNode(nodes *[]domaincopilot.AnalysisGraphNode, edges *[]domaincopilot.AnalysisGraphEdge, serviceNodeIDs map[string]string, rootID, name string, evidence domaincopilot.RootCauseEvidence) string {
	serviceName := firstNonEmpty(strings.TrimSpace(name), strings.TrimSpace(fmt.Sprint(evidence.Attributes["service"])), strings.TrimSpace(fmt.Sprint(evidence.Attributes["workload"])), evidence.Namespace, "unknown-service")
	if nodeID, ok := serviceNodeIDs[serviceName]; ok {
		return nodeID
	}
	nodeID := "service:" + serviceName
	serviceNodeIDs[serviceName] = nodeID
	*nodes = appendGraphNode(*nodes, domaincopilot.AnalysisGraphNode{
		ID:         nodeID,
		Kind:       "service",
		Title:      serviceName,
		Subtitle:   firstNonEmpty(evidence.Namespace, strings.TrimSpace(fmt.Sprint(evidence.Attributes["namespace"]))),
		Severity:   evidence.Severity,
		SourceRefs: []string{"platform-native.v1"},
		Attributes: map[string]any{
			"clusterId": firstNonEmpty(evidence.ClusterID, strings.TrimSpace(fmt.Sprint(evidence.Attributes["clusterId"]))),
			"namespace": firstNonEmpty(evidence.Namespace, strings.TrimSpace(fmt.Sprint(evidence.Attributes["namespace"]))),
		},
	})
	*edges = appendGraphEdge(*edges, domaincopilot.AnalysisGraphEdge{
		ID:       rootID + "->" + nodeID,
		Source:   rootID,
		Target:   nodeID,
		Relation: "contains",
		Severity: evidence.Severity,
	})
	return nodeID
}

func graphEvidenceNodeID(evidenceID string, evidenceNodeIDs, logNodeIDs, metricNodeIDs map[string]string) string {
	if value := evidenceNodeIDs[evidenceID]; value != "" {
		return value
	}
	if value := logNodeIDs[evidenceID]; value != "" {
		return value
	}
	if value := metricNodeIDs[evidenceID]; value != "" {
		return value
	}
	return ""
}

func appendGraphNode(nodes []domaincopilot.AnalysisGraphNode, node domaincopilot.AnalysisGraphNode) []domaincopilot.AnalysisGraphNode {
	for _, current := range nodes {
		if current.ID == node.ID {
			return nodes
		}
	}
	return append(nodes, node)
}

func appendGraphEdge(edges []domaincopilot.AnalysisGraphEdge, edge domaincopilot.AnalysisGraphEdge) []domaincopilot.AnalysisGraphEdge {
	for _, current := range edges {
		if current.ID == edge.ID {
			return edges
		}
	}
	return append(edges, edge)
}

func confidenceSeverity(confidence int) string {
	switch {
	case confidence >= 80:
		return "critical"
	case confidence >= 60:
		return "warning"
	default:
		return "info"
	}
}

func metricTrendSeverity(trend string) string {
	switch strings.TrimSpace(trend) {
	case "spike":
		return "warning"
	case "drop":
		return "info"
	default:
		return "info"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func appendMissingSourceNodes(nodes *[]domaincopilot.AnalysisGraphNode, edges *[]domaincopilot.AnalysisGraphEdge, rootID string, snapshot map[string]any) {
	available, _ := snapshot["availableSources"].([]map[string]any)
	availableKinds := map[string]bool{}
	for _, item := range available {
		availableKinds[strings.TrimSpace(fmt.Sprint(item["sourceKind"]))] = true
	}
	for _, kind := range []string{"logs", "metrics", "traces"} {
		if availableKinds[kind] {
			continue
		}
		nodeID := "missing-source:" + kind
		*nodes = appendGraphNode(*nodes, domaincopilot.AnalysisGraphNode{
			ID:         nodeID,
			Kind:       "missing_source",
			Title:      fmt.Sprintf("%s 数据源未配置", strings.ToUpper(kind)),
			Subtitle:   fmt.Sprintf("当前根因会话还无法读取 %s 证据。", kind),
			Severity:   "info",
			SourceRefs: []string{kind + ".v1"},
			Attributes: map[string]any{
				"sourceKind": kind,
				"status":     "missing",
			},
		})
		*edges = appendGraphEdge(*edges, domaincopilot.AnalysisGraphEdge{
			ID:       rootID + "->" + nodeID,
			Source:   rootID,
			Target:   nodeID,
			Relation: "requires",
			Severity: "info",
		})
	}
}

func appendRecommendationNodes(nodes *[]domaincopilot.AnalysisGraphNode, edges *[]domaincopilot.AnalysisGraphEdge, rootID string, hypotheses []domaincopilot.RootCauseHypothesis) {
	if len(hypotheses) > 0 {
		return
	}
	nodeID := "recommendation:narrow-scope"
	*nodes = appendGraphNode(*nodes, domaincopilot.AnalysisGraphNode{
		ID:         nodeID,
		Kind:       "recommendation",
		Title:      "缩小排查范围",
		Subtitle:   "优先固定 namespace / workload，再重新运行根因分析。",
		Severity:   "info",
		SourceRefs: []string{"analysis"},
		Attributes: map[string]any{
			"action": "narrow_scope_and_rerun",
		},
	})
	*edges = appendGraphEdge(*edges, domaincopilot.AnalysisGraphEdge{
		ID:       rootID + "->" + nodeID,
		Source:   rootID,
		Target:   nodeID,
		Relation: "suggests",
		Severity: "info",
	})
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
