package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/kubecrux/kubecrux/internal/application/access"
	domainalert "github.com/kubecrux/kubecrux/internal/domain/alert"
	domainbuild "github.com/kubecrux/kubecrux/internal/domain/build"
	domaincopilot "github.com/kubecrux/kubecrux/internal/domain/copilot"
	domainidentity "github.com/kubecrux/kubecrux/internal/domain/identity"
	domainrelease "github.com/kubecrux/kubecrux/internal/domain/release"
	mcplogs "github.com/kubecrux/kubecrux/internal/infrastructure/mcp/logs"
	mcpmetrics "github.com/kubecrux/kubecrux/internal/infrastructure/mcp/metrics"
	mcptraces "github.com/kubecrux/kubecrux/internal/infrastructure/mcp/traces"
	aperrors "github.com/kubecrux/kubecrux/internal/platform/apperrors"
	"go.uber.org/zap"
)

const (
	agentProviderInternal = "internal"
	agentProviderHermes   = "hermes"
)

func (s *Service) ListAgentProviders(ctx context.Context, principal domainidentity.Principal) ([]domaincopilot.AgentProvider, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIView); err != nil {
		return nil, err
	}
	return s.agentProviderCatalog(), nil
}

func (s *Service) ListAgentRuns(ctx context.Context, principal domainidentity.Principal) ([]domaincopilot.AgentRun, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIView); err != nil {
		return nil, err
	}
	items, err := s.repo.ListAgentRuns(ctx, domaincopilot.AgentRunFilter{Limit: 50})
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index].CallbackToken = ""
	}
	return items, nil
}

func (s *Service) ClaimAgentRun(ctx context.Context, input domaincopilot.AgentRunClaimInput) (domaincopilot.AgentRun, error) {
	if strings.TrimSpace(input.AgentID) == "" {
		return domaincopilot.AgentRun{}, fmt.Errorf("%w: agentId is required", aperrors.ErrInvalidArgument)
	}
	if len(input.ProviderIDs) == 0 && len(input.Kinds) == 0 {
		input.ProviderIDs = []string{agentProviderHermes}
		input.Kinds = []string{agentProviderHermes}
	}
	return s.repo.ClaimAgentRun(ctx, input)
}

func (s *Service) RecordAgentRunCallback(ctx context.Context, input domaincopilot.AgentRunCallbackInput) (domaincopilot.AgentRun, error) {
	if strings.TrimSpace(input.RunID) == "" || strings.TrimSpace(input.CallbackToken) == "" {
		return domaincopilot.AgentRun{}, fmt.Errorf("%w: runId and callbackToken are required", aperrors.ErrInvalidArgument)
	}
	if agentRunCallbackProducesArtifact(input.Status) && len(input.AnalysisArtifacts) == 0 {
		if current, err := s.repo.GetAgentRun(ctx, "", input.RunID); err == nil {
			synthetic := current
			synthetic.Output = mergeAgentRunCallbackPayload(current.Output, input.Payload)
			if len(input.ToolExecutions) > 0 {
				synthetic.ToolExecutions = input.ToolExecutions
			}
			if strings.TrimSpace(input.ExternalRunID) != "" {
				synthetic.ExternalRunID = strings.TrimSpace(input.ExternalRunID)
			}
			if strings.TrimSpace(input.ErrorMessage) != "" {
				synthetic.ErrorMessage = strings.TrimSpace(input.ErrorMessage)
			}
			input.AnalysisArtifacts = []domaincopilot.AnalysisArtifact{s.synthesizeAgentArtifact(synthetic)}
		}
	}
	updated, err := s.repo.UpdateAgentRunCallback(ctx, input)
	if err != nil {
		return domaincopilot.AgentRun{}, err
	}
	if agentRunCallbackShouldPersistMessage(updated.Status) {
		if len(updated.AnalysisArtifacts) == 0 {
			updated.AnalysisArtifacts = []domaincopilot.AnalysisArtifact{s.synthesizeAgentArtifact(updated)}
		}
		if strings.TrimSpace(updated.RootCauseRunID) != "" {
			_ = s.persistAgentRunRootCauseResult(ctx, updated)
		}
		if strings.TrimSpace(updated.SessionID) != "" {
			_ = s.persistAgentRunMessage(ctx, updated)
		}
	}
	return updated, nil
}

func (s *Service) RecordAgentToolCall(ctx context.Context, input domaincopilot.AgentToolCallInput) (domaincopilot.AgentToolCallResult, error) {
	if strings.TrimSpace(input.RunID) == "" || strings.TrimSpace(input.CallbackToken) == "" {
		return domaincopilot.AgentToolCallResult{}, fmt.Errorf("%w: runId and callbackToken are required", aperrors.ErrInvalidArgument)
	}
	run, err := s.repo.GetAgentRun(ctx, "", input.RunID)
	if err != nil {
		return domaincopilot.AgentToolCallResult{}, err
	}
	if strings.TrimSpace(run.CallbackToken) == "" || strings.TrimSpace(run.CallbackToken) != strings.TrimSpace(input.CallbackToken) {
		return domaincopilot.AgentToolCallResult{}, fmt.Errorf("%w: invalid ai agent callback token", aperrors.ErrAccessDenied)
	}
	if agentRunStatusTerminal(run.Status) {
		return domaincopilot.AgentToolCallResult{}, fmt.Errorf("%w: agent run is already terminal", aperrors.ErrInvalidArgument)
	}
	binding, ok := resolveAgentToolBinding(run, input)
	if !ok {
		return domaincopilot.AgentToolCallResult{}, fmt.Errorf("%w: tool binding is not allowed for this agent run", aperrors.ErrAccessDenied)
	}
	toolExecution, output, _ := s.executeAgentToolBinding(ctx, run, binding, input)
	nextExecutions := append(append([]domaincopilot.ToolExecution{}, run.ToolExecutions...), toolExecution)
	updated, persistErr := s.repo.UpdateAgentRunCallback(ctx, domaincopilot.AgentRunCallbackInput{
		RunID:          run.ID,
		CallbackToken:  run.CallbackToken,
		AgentID:        input.AgentID,
		Status:         domaincopilot.AgentRunStatusRunning,
		Payload:        map[string]any{"lastToolCallId": toolExecution.ID, "lastToolCallName": toolExecution.ToolName, "lastToolCallStatus": toolExecution.Status},
		ToolExecutions: nextExecutions,
	})
	if persistErr == nil {
		run = updated
	}
	if persistErr != nil {
		return domaincopilot.AgentToolCallResult{}, persistErr
	}
	return domaincopilot.AgentToolCallResult{RunID: run.ID, ToolExecution: toolExecution, Output: output}, nil
}

func (s *Service) sweepAgentRunTimeouts(ctx context.Context) (int, error) {
	if s == nil || s.repo == nil {
		return 0, nil
	}
	now := time.Now().UTC()
	runs, err := s.repo.ListAgentRuns(ctx, domaincopilot.AgentRunFilter{Status: domaincopilot.AgentRunStatusQueued, Limit: 200})
	if err != nil {
		return 0, err
	}
	runningRuns, err := s.repo.ListAgentRuns(ctx, domaincopilot.AgentRunFilter{Status: domaincopilot.AgentRunStatusRunning, Limit: 200})
	if err != nil {
		return 0, err
	}
	runs = append(runs, runningRuns...)
	count := 0
	for _, run := range runs {
		if !agentRunHeartbeatExpired(run, now) {
			continue
		}
		timeoutSeconds := run.TimeoutSeconds
		if timeoutSeconds <= 0 {
			timeoutSeconds = 600
		}
		payload := map[string]any{
			"error":          fmt.Sprintf("agent run timed out after %d seconds without callback", timeoutSeconds),
			"timeoutSeconds": timeoutSeconds,
			"timedOutAt":     now.Format(time.RFC3339),
			"agentRunStatus": domaincopilot.AgentRunStatusCallbackTimeout,
		}
		if run.LastHeartbeatAt != nil {
			payload["lastHeartbeatAt"] = run.LastHeartbeatAt.UTC().Format(time.RFC3339)
		}
		_, callbackErr := s.RecordAgentRunCallback(ctx, domaincopilot.AgentRunCallbackInput{
			RunID:             run.ID,
			CallbackToken:     run.CallbackToken,
			AgentID:           firstNonEmpty(strings.TrimSpace(run.ClaimedByAgentID), "kubecrux-control-plane"),
			Status:            domaincopilot.AgentRunStatusCallbackTimeout,
			Payload:           payload,
			AnalysisArtifacts: []domaincopilot.AnalysisArtifact{s.synthesizeAgentArtifact(agentRunWithOutput(run, payload))},
			ErrorMessage:      stringValue(payload["error"]),
		})
		if callbackErr != nil {
			s.logWarn("copilot agent runtime timeout callback failed", zap.String("runID", run.ID), zap.Error(callbackErr))
			continue
		}
		count++
	}
	return count, nil
}

func agentRunHeartbeatExpired(run domaincopilot.AgentRun, now time.Time) bool {
	if run.Status != domaincopilot.AgentRunStatusQueued && run.Status != domaincopilot.AgentRunStatusRunning {
		return false
	}
	timeoutSeconds := run.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 600
	}
	reference := run.QueuedAt
	if run.LastHeartbeatAt != nil && !run.LastHeartbeatAt.IsZero() {
		reference = run.LastHeartbeatAt.UTC()
	} else if run.StartedAt != nil && !run.StartedAt.IsZero() {
		reference = run.StartedAt.UTC()
	}
	return now.After(reference.Add(time.Duration(timeoutSeconds) * time.Second))
}

func agentRunStatusTerminal(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case domaincopilot.AgentRunStatusCompleted, domaincopilot.AgentRunStatusFailed, domaincopilot.AgentRunStatusCanceled, domaincopilot.AgentRunStatusCallbackTimeout:
		return true
	default:
		return false
	}
}

func agentRunWithOutput(run domaincopilot.AgentRun, output map[string]any) domaincopilot.AgentRun {
	run.Output = mergeAgentRunCallbackPayload(run.Output, output)
	return run
}

func agentRunCallbackProducesArtifact(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case domaincopilot.AgentRunStatusCompleted, "succeeded", "success", domaincopilot.AgentRunStatusFailed, "error", domaincopilot.AgentRunStatusCallbackTimeout:
		return true
	default:
		return false
	}
}

func agentRunCallbackShouldPersistMessage(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case domaincopilot.AgentRunStatusCompleted, domaincopilot.AgentRunStatusFailed, domaincopilot.AgentRunStatusCallbackTimeout:
		return true
	default:
		return false
	}
}

func mergeAgentRunCallbackPayload(current map[string]any, patch map[string]any) map[string]any {
	merged := map[string]any{}
	for key, value := range current {
		merged[key] = value
	}
	for key, value := range patch {
		merged[key] = value
	}
	return merged
}

func resolveAgentToolBinding(run domaincopilot.AgentRun, input domaincopilot.AgentToolCallInput) (domaincopilot.AgentToolBinding, bool) {
	toolBindingID := strings.TrimSpace(input.ToolBindingID)
	adapterID := strings.TrimSpace(input.AdapterID)
	toolName := strings.TrimSpace(input.ToolName)
	for _, binding := range run.ToolBindings {
		if toolBindingID != "" && strings.TrimSpace(binding.ID) != toolBindingID {
			continue
		}
		if adapterID != "" && strings.TrimSpace(binding.AdapterID) != adapterID {
			continue
		}
		if toolName != "" && strings.TrimSpace(binding.ToolName) != toolName {
			continue
		}
		if !agentToolBindingReadOnly(binding) {
			return domaincopilot.AgentToolBinding{}, false
		}
		if !toolsetAllowsTool(run.Toolset, binding.AdapterID, binding.ToolName) {
			return domaincopilot.AgentToolBinding{}, false
		}
		return binding, true
	}
	return domaincopilot.AgentToolBinding{}, false
}

func agentToolBindingReadOnly(binding domaincopilot.AgentToolBinding) bool {
	if strings.TrimSpace(binding.ToolKind) == "" {
		return false
	}
	if strings.TrimSpace(binding.ToolKind) == "mcp" {
		return true
	}
	name := strings.ToLower(strings.TrimSpace(binding.ToolName))
	return strings.Contains(name, ".list") || strings.Contains(name, ".query") || strings.Contains(name, ".resolve") || strings.Contains(name, ".snapshot") || strings.Contains(name, ".read")
}

func (s *Service) executeAgentToolBinding(ctx context.Context, run domaincopilot.AgentRun, binding domaincopilot.AgentToolBinding, input domaincopilot.AgentToolCallInput) (domaincopilot.ToolExecution, map[string]any, error) {
	startedAt := time.Now().UTC()
	output, err := s.executeAgentToolBindingOutput(ctx, run, binding, input.Input)
	completedAt := time.Now().UTC()
	status := "success"
	summary := fmt.Sprintf("%s executed", binding.ToolName)
	if err != nil {
		status = "failed"
		summary = err.Error()
		output = map[string]any{"error": err.Error()}
	}
	toolExecution := domaincopilot.ToolExecution{
		ID:          "tool:" + uuid.NewString(),
		AdapterID:   firstNonEmpty(binding.AdapterID, binding.ToolKind),
		ToolName:    firstNonEmpty(binding.ToolName, binding.ID),
		Status:      status,
		Summary:     summary,
		Input:       map[string]any{"toolBindingId": binding.ID, "input": input.Input, "scope": run.Scope},
		Output:      output,
		StartedAt:   startedAt,
		CompletedAt: &completedAt,
	}
	if err != nil {
		return toolExecution, output, err
	}
	return toolExecution, output, nil
}

func (s *Service) executeAgentToolBindingOutput(ctx context.Context, run domaincopilot.AgentRun, binding domaincopilot.AgentToolBinding, input map[string]any) (map[string]any, error) {
	switch strings.TrimSpace(binding.ToolName) {
	case "logs.query":
		return s.executeAgentLogsTool(ctx, run, input)
	case "metrics.query":
		return s.executeAgentMetricsTool(ctx, run, input)
	case "traces.query":
		return s.executeAgentTracesTool(ctx, run, input)
	case "events.query":
		return s.executeAgentEventsTool(ctx, run, input)
	case "delivery.releases.list":
		return s.executeAgentDeliveryReleasesTool(ctx, run, input)
	case "delivery.builds.list":
		return s.executeAgentDeliveryBuildsTool(ctx, run, input)
	case "alerts.list":
		return s.executeAgentAlertsTool(ctx, run, input)
	default:
		return nil, fmt.Errorf("%w: unsupported agent tool %s", aperrors.ErrInvalidArgument, binding.ToolName)
	}
}

func (s *Service) executeAgentLogsTool(ctx context.Context, run domaincopilot.AgentRun, input map[string]any) (map[string]any, error) {
	source, ok, err := s.findAgentDataSource(ctx, "logs", "logs.v1", input)
	if err != nil || !ok {
		return nil, firstNonNilError(err, fmt.Errorf("%w: no enabled logs data source", aperrors.ErrNotFound))
	}
	timeFrom, timeTo := agentToolTimeRange(run, input)
	limit := firstPositive(intCondition(input["limit"]), evidenceBudget(run.Toolset, 20))
	result, err := mcplogs.DefaultRegistry().Correlate(ctx, source.BackendType, source.ID, source.Config, mcplogs.CorrelationQuery{
		Scope: mcplogs.Scope{
			ClusterID: run.Scope.ClusterID,
			Namespace: run.Scope.Namespace,
			Workload:  run.Scope.Workload,
			Service:   run.Scope.Service,
		},
		AlertID:  firstNonEmpty(stringValue(input["alertId"]), run.Scope.AlertID),
		Workload: firstNonEmpty(stringValue(input["workload"]), run.Scope.Workload),
		TimeFrom: timeFrom,
		TimeTo:   timeTo,
		Query:    stringValue(input["query"]),
		Limit:    limit,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"sourceId":     source.ID,
		"summary":      result.Summary,
		"signatures":   result.Signatures,
		"records":      buildSampleRecordAttributes(result.Records, minPositive(limit, 5)),
		"truncated":    result.Truncated,
		"queryCost":    result.QueryCost,
		"sampleWindow": result.SampleWindow,
	}, nil
}

func (s *Service) executeAgentMetricsTool(ctx context.Context, run domaincopilot.AgentRun, input map[string]any) (map[string]any, error) {
	source, ok, err := s.findAgentDataSource(ctx, "metrics", "metrics.v1", input)
	if err != nil || !ok {
		return nil, firstNonNilError(err, fmt.Errorf("%w: no enabled metrics data source", aperrors.ErrNotFound))
	}
	timeFrom, timeTo := agentToolTimeRange(run, input)
	summary, err := mcpmetrics.DefaultRegistry().Analyze(ctx, source.BackendType, source.ID, source.Config, mcpmetrics.RangeQuery{
		Scope: mcpmetrics.Scope{
			ClusterID: run.Scope.ClusterID,
			Namespace: run.Scope.Namespace,
			Workload:  run.Scope.Workload,
			Service:   run.Scope.Service,
		},
		MetricKey: stringValue(input["metricKey"]),
		TimeFrom:  timeFrom,
		TimeTo:    timeTo,
		Step:      time.Duration(firstPositive(intCondition(input["stepSeconds"]), 60)) * time.Second,
	})
	if err != nil {
		return nil, err
	}
	limit := evidenceBudget(run.Toolset, len(summary.Signals))
	return map[string]any{
		"sourceId":     source.ID,
		"summary":      summary.Summary,
		"signals":      limitMapItems(summary.Signals, limit),
		"queryCost":    summary.QueryCost,
		"sampleWindow": summary.SampleWindow,
	}, nil
}

func (s *Service) executeAgentTracesTool(ctx context.Context, run domaincopilot.AgentRun, input map[string]any) (map[string]any, error) {
	source, ok, err := s.findAgentDataSource(ctx, "traces", "traces.v1", input)
	if err != nil || !ok {
		return nil, firstNonNilError(err, fmt.Errorf("%w: no enabled traces data source", aperrors.ErrNotFound))
	}
	timeFrom, timeTo := agentToolTimeRange(run, input)
	limit := firstPositive(intCondition(input["limit"]), evidenceBudget(run.Toolset, 20))
	result, err := mcptraces.DefaultRegistry().FindSlowSpans(ctx, source.BackendType, source.ID, source.Config, mcptraces.Query{
		Scope: mcptraces.Scope{
			ClusterID: run.Scope.ClusterID,
			Namespace: run.Scope.Namespace,
			Workload:  run.Scope.Workload,
			Service:   run.Scope.Service,
		},
		TimeFrom:    timeFrom,
		TimeTo:      timeTo,
		MinDuration: time.Duration(firstPositive(intCondition(input["minDurationMs"]), 250)) * time.Millisecond,
		Limit:       limit,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"sourceId":     source.ID,
		"summary":      result.Summary,
		"spans":        limitSpans(result.Spans, limit),
		"hotspots":     result.Hotspots,
		"queryCost":    result.QueryCost,
		"sampleWindow": result.SampleWindow,
	}, nil
}

func (s *Service) executeAgentEventsTool(ctx context.Context, run domaincopilot.AgentRun, input map[string]any) (map[string]any, error) {
	if s.events == nil {
		return nil, fmt.Errorf("%w: event reader is not configured", aperrors.ErrNotFound)
	}
	limit := firstPositive(intCondition(input["limit"]), 50)
	events, err := s.events.List(ctx, limit)
	if err != nil {
		return nil, err
	}
	filtered := filterRootCauseEvents(events, domaincopilot.RootCauseRunInput{
		ClusterID:    run.Scope.ClusterID,
		Namespace:    run.Scope.Namespace,
		WorkloadName: run.Scope.Workload,
	})
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	return map[string]any{"events": filtered, "count": len(filtered)}, nil
}

func (s *Service) executeAgentDeliveryReleasesTool(ctx context.Context, run domaincopilot.AgentRun, input map[string]any) (map[string]any, error) {
	if s.releases == nil {
		return nil, fmt.Errorf("%w: release reader is not configured", aperrors.ErrNotFound)
	}
	limit := firstPositive(intCondition(input["limit"]), 20)
	items, err := s.releases.List(ctx, domainrelease.Filter{
		ApplicationID: firstNonEmpty(stringValue(input["applicationId"]), stringValue(run.Input["applicationId"])),
		ClusterID:     firstNonEmpty(stringValue(input["clusterId"]), run.Scope.ClusterID),
		Limit:         limit,
	})
	if err != nil {
		return nil, err
	}
	items = filterAgentReleaseRecords(items, run)
	if len(items) > limit {
		items = items[:limit]
	}
	return map[string]any{"releases": agentReleaseRecordSummaries(items), "count": len(items)}, nil
}

func (s *Service) executeAgentDeliveryBuildsTool(ctx context.Context, run domaincopilot.AgentRun, input map[string]any) (map[string]any, error) {
	if s.builds == nil {
		return nil, fmt.Errorf("%w: build reader is not configured", aperrors.ErrNotFound)
	}
	limit := firstPositive(intCondition(input["limit"]), 20)
	items, err := s.builds.List(ctx, domainbuild.Filter{
		ApplicationID: firstNonEmpty(stringValue(input["applicationId"]), stringValue(run.Input["applicationId"])),
		Limit:         limit,
	})
	if err != nil {
		return nil, err
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return map[string]any{"builds": agentBuildRecordSummaries(items), "count": len(items)}, nil
}

func (s *Service) executeAgentAlertsTool(ctx context.Context, run domaincopilot.AgentRun, input map[string]any) (map[string]any, error) {
	if s.alerts == nil {
		return nil, fmt.Errorf("%w: alert reader is not configured", aperrors.ErrNotFound)
	}
	limit := firstPositive(intCondition(input["limit"]), 20)
	items, err := s.alerts.ListAlerts(ctx, agentToolPrincipal(), domainalert.Filter{
		Status:    firstNonEmpty(stringValue(input["status"]), "firing"),
		ClusterID: firstNonEmpty(stringValue(input["clusterId"]), run.Scope.ClusterID),
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}
	items = filterAgentAlerts(items, run)
	if len(items) > limit {
		items = items[:limit]
	}
	return map[string]any{"alerts": agentAlertSummaries(items), "count": len(items)}, nil
}

func (s *Service) findAgentDataSource(ctx context.Context, sourceKind, adapterID string, input map[string]any) (domaincopilot.DataSource, bool, error) {
	sources, err := s.repo.ListDataSources(ctx)
	if err != nil {
		return domaincopilot.DataSource{}, false, err
	}
	sourceID := strings.TrimSpace(stringValue(input["sourceId"]))
	for _, source := range sources {
		if !source.Enabled || source.SourceKind != sourceKind || source.MCPAdapter != adapterID {
			continue
		}
		if sourceID != "" && source.ID != sourceID {
			continue
		}
		return source, true, nil
	}
	return domaincopilot.DataSource{}, false, nil
}

func agentToolTimeRange(run domaincopilot.AgentRun, input map[string]any) (time.Time, time.Time) {
	to := time.Now().UTC()
	if value := parseRFC3339Time(input["timeTo"]); !value.IsZero() {
		to = value
	}
	minutes := firstPositive(intCondition(input["timeRangeMinutes"]), run.Scope.TimeRangeMinutes, 60)
	from := to.Add(-time.Duration(minutes) * time.Minute)
	if value := parseRFC3339Time(input["timeFrom"]); !value.IsZero() {
		from = value
	}
	return from, to
}

func parseRFC3339Time(value any) time.Time {
	trimmed := strings.TrimSpace(fmt.Sprint(value))
	if trimmed == "" || trimmed == "<nil>" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return time.Time{}
	}
	return parsed.UTC()
}

func firstNonNilError(values ...error) error {
	for _, err := range values {
		if err != nil {
			return err
		}
	}
	return nil
}

func minPositive(value, maxValue int) int {
	if value <= 0 {
		return maxValue
	}
	if maxValue > 0 && value > maxValue {
		return maxValue
	}
	return value
}

func agentToolPrincipal() domainidentity.Principal {
	return domainidentity.Principal{
		UserID:   "agent-runtime",
		UserName: "Agent Runtime",
		Roles:    []string{"admin"},
	}
}

func filterAgentReleaseRecords(items []domainrelease.Record, run domaincopilot.AgentRun) []domainrelease.Record {
	out := make([]domainrelease.Record, 0, len(items))
	for _, item := range items {
		if run.Scope.ClusterID != "" && item.ClusterID != run.Scope.ClusterID {
			continue
		}
		if run.Scope.Namespace != "" && item.Namespace != run.Scope.Namespace {
			continue
		}
		if run.Scope.Workload != "" && item.DeploymentName != run.Scope.Workload {
			continue
		}
		out = append(out, item)
	}
	return out
}

func filterAgentAlerts(items []domainalert.Instance, run domaincopilot.AgentRun) []domainalert.Instance {
	out := make([]domainalert.Instance, 0, len(items))
	for _, item := range items {
		if run.Scope.AlertID != "" && item.ID != run.Scope.AlertID {
			continue
		}
		if run.Scope.ClusterID != "" && item.ClusterID != run.Scope.ClusterID {
			continue
		}
		if run.Scope.Namespace != "" && item.Namespace != run.Scope.Namespace {
			continue
		}
		if run.Scope.Workload != "" && !strings.Contains(strings.ToLower(item.Title+" "+item.Summary), strings.ToLower(run.Scope.Workload)) {
			continue
		}
		out = append(out, item)
	}
	return out
}

func agentReleaseRecordSummaries(items []domainrelease.Record) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"id":             item.ID,
			"applicationId":  item.ApplicationID,
			"clusterId":      item.ClusterID,
			"namespace":      item.Namespace,
			"deploymentName": item.DeploymentName,
			"status":         item.Status,
			"deployedAt":     optionalAgentTime(item.DeployedAt),
			"createdAt":      agentTime(item.CreatedAt),
			"metadata":       item.Metadata,
		})
	}
	return out
}

func agentBuildRecordSummaries(items []domainbuild.Record) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"id":            item.ID,
			"applicationId": item.ApplicationID,
			"sourceSystem":  item.SourceSystem,
			"status":        item.Status,
			"startedAt":     optionalAgentTime(item.StartedAt),
			"finishedAt":    optionalAgentTime(item.FinishedAt),
			"createdAt":     agentTime(item.CreatedAt),
			"metadata":      item.Metadata,
		})
	}
	return out
}

func agentAlertSummaries(items []domainalert.Instance) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"id":          item.ID,
			"source":      item.Source,
			"fingerprint": item.Fingerprint,
			"title":       item.Title,
			"summary":     item.Summary,
			"severity":    item.Severity,
			"status":      item.Status,
			"clusterId":   item.ClusterID,
			"namespace":   item.Namespace,
			"receiver":    item.Receiver,
			"startsAt":    agentTime(item.StartsAt),
			"lastSeenAt":  agentTime(item.LastSeenAt),
			"labels":      item.Labels,
		})
	}
	return out
}

func agentTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func optionalAgentTime(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func (s *Service) createAgentRun(ctx context.Context, input domaincopilot.AgentRunInput) (domaincopilot.AgentRun, error) {
	provider := s.resolveAgentProvider(input.ProviderID)
	if !provider.Enabled {
		return domaincopilot.AgentRun{}, fmt.Errorf("%w: agent provider %s is disabled", aperrors.ErrInvalidArgument, provider.ID)
	}
	capabilityID := strings.TrimSpace(input.CapabilityID)
	if capabilityID == "" {
		capabilityID = "root_cause"
	}
	timeoutSeconds := input.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = 600
	}
	toolBindings := input.ToolBindings
	if len(toolBindings) == 0 {
		toolBindings = s.agentToolBindingsForRun(provider, capabilityID, input.Toolset)
	}
	skillBindings := input.SkillBindings
	if len(skillBindings) == 0 {
		skillBindings = s.agentSkillBindingsForRun(provider, capabilityID, input.SkillIDs)
	}
	now := time.Now().UTC()
	run := domaincopilot.AgentRun{
		ID:             "agent:" + uuid.NewString(),
		ProviderID:     provider.ID,
		ProviderKind:   firstNonEmpty(provider.Kind, provider.ID),
		CapabilityID:   capabilityID,
		SkillIDs:       normalizeStringList(input.SkillIDs),
		SessionID:      strings.TrimSpace(input.SessionID),
		RootCauseRunID: strings.TrimSpace(input.RootCauseRunID),
		CreatedBy:      strings.TrimSpace(input.CreatedBy),
		Status:         domaincopilot.AgentRunStatusQueued,
		Scope:          input.Scope,
		Toolset:        input.Toolset,
		ToolBindings:   toolBindings,
		SkillBindings:  skillBindings,
		Input:          input.Input,
		Output:         map[string]any{},
		CallbackToken:  uuid.NewString(),
		TimeoutSeconds: timeoutSeconds,
		QueuedAt:       now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if run.CreatedBy == "" {
		run.CreatedBy = automationRootCauseCreatedBy
	}
	return s.repo.CreateAgentRun(ctx, run)
}

func (s *Service) agentToolBindingsForRun(provider domaincopilot.AgentProvider, capabilityID string, toolset domaincopilot.SessionToolset) []domaincopilot.AgentToolBinding {
	bindings := filterToolBindings(defaultAgentToolBindings(), capabilityID)
	out := make([]domaincopilot.AgentToolBinding, 0, len(bindings))
	enabledAdapters := map[string]struct{}{}
	for _, adapterID := range toolset.EnabledAdapterIDs {
		enabledAdapters[strings.TrimSpace(adapterID)] = struct{}{}
	}
	for _, binding := range bindings {
		if binding.ProviderID != "" && binding.ProviderID != provider.ID {
			continue
		}
		if binding.ProviderKind != "" && binding.ProviderKind != provider.Kind {
			continue
		}
		if len(enabledAdapters) > 0 && binding.AdapterID != "" {
			if _, ok := enabledAdapters[strings.TrimSpace(binding.AdapterID)]; !ok {
				continue
			}
		}
		if !toolsetAllowsTool(toolset, binding.AdapterID, binding.ToolName) {
			continue
		}
		out = append(out, binding)
	}
	return out
}

func (s *Service) agentSkillBindingsForRun(provider domaincopilot.AgentProvider, capabilityID string, skillIDs []string) []domaincopilot.AgentSkillBinding {
	bindings := filterSkillBindings(defaultAgentSkillBindings(), capabilityID)
	enabledSkills := map[string]struct{}{}
	hasProviderSkillFilter := false
	for _, skillID := range skillIDs {
		trimmed := strings.TrimSpace(skillID)
		if trimmed != "" {
			enabledSkills[trimmed] = struct{}{}
			if agentProviderSkillIDForCapability(capabilityID, trimmed) == trimmed {
				hasProviderSkillFilter = true
			}
		}
	}
	out := make([]domaincopilot.AgentSkillBinding, 0, len(bindings))
	for _, binding := range bindings {
		if binding.ProviderID != "" && binding.ProviderID != provider.ID {
			continue
		}
		if binding.ProviderKind != "" && binding.ProviderKind != provider.Kind {
			continue
		}
		if hasProviderSkillFilter {
			if _, ok := enabledSkills[strings.TrimSpace(binding.SkillID)]; !ok {
				continue
			}
		}
		out = append(out, binding)
	}
	return out
}

func agentProviderSkillIDForCapability(capabilityID, skillID string) string {
	switch strings.TrimSpace(skillID) {
	case "root-cause-investigation", "inspection-review", "delivery-failure-analysis", "platform-diagnosis", "oncall-brief":
		return strings.TrimSpace(skillID)
	}
	switch strings.TrimSpace(capabilityID) {
	case "root_cause", "performance", "trace":
		return "root-cause-investigation"
	case "inspection_review":
		return "inspection-review"
	case "delivery_failure", "post_deploy_observation":
		return "delivery-failure-analysis"
	case "platform_resource_diagnosis", "docker_diagnosis", "virtualization_diagnosis":
		return "platform-diagnosis"
	case "oncall_brief":
		return "oncall-brief"
	default:
		return strings.TrimSpace(skillID)
	}
}

func (s *Service) shouldUseExternalAgent(providerID string) bool {
	providerID = normalizeAgentProviderID(providerID)
	return providerID != "" && providerID != agentProviderInternal
}

func (s *Service) agentProviderCatalog() []domaincopilot.AgentProvider {
	configured := append([]domaincopilot.AgentProvider(nil), s.agentProviders...)
	if len(configured) == 0 {
		configured = defaultAgentProviders()
	}
	capabilityIDs := make([]string, 0, len(defaultAgentCapabilities()))
	for _, capability := range defaultAgentCapabilities() {
		capabilityIDs = append(capabilityIDs, capability.ID)
	}
	for index := range configured {
		if len(configured[index].Capabilities) == 0 {
			configured[index].Capabilities = append([]string(nil), capabilityIDs...)
		}
	}
	return configured
}

func (s *Service) resolveAgentProvider(providerID string) domaincopilot.AgentProvider {
	providerID = normalizeAgentProviderID(providerID)
	for _, item := range s.agentProviderCatalog() {
		if item.ID == providerID || item.Kind == providerID {
			return item
		}
	}
	return domaincopilot.AgentProvider{ID: providerID, Kind: providerID, Name: providerID, Enabled: false}
}

func defaultAgentProviders() []domaincopilot.AgentProvider {
	capabilities := []string{"root_cause", "performance", "trace", "inspection_review", "delivery_failure", "post_deploy_observation", "platform_resource_diagnosis", "docker_diagnosis", "virtualization_diagnosis", "oncall_brief"}
	return []domaincopilot.AgentProvider{
		{
			ID:               agentProviderInternal,
			Kind:             "internal",
			Name:             "kubecrux 内置分析",
			Description:      "使用 kubecrux 已有平台聚合、MCP 数据源和规则化 playbook 执行同步分析。",
			Enabled:          true,
			Default:          true,
			Capabilities:     append([]string(nil), capabilities...),
			SupportedModes:   []string{"root_cause", "performance", "trace", "inspection_review"},
			SupportsAsync:    false,
			SupportsSkills:   true,
			SupportsToolsets: true,
			Config: map[string]any{
				"executionMode": "in_process",
			},
		},
		{
			ID:               agentProviderHermes,
			Kind:             "hermes",
			Name:             "Hermes Agent",
			Description:      "通过 kubecrux agent runner 领取任务并调用 Hermes CLI 或 Hermes Agent 能力执行深度分析。",
			Enabled:          true,
			Capabilities:     append([]string(nil), capabilities...),
			SupportedModes:   []string{"root_cause", "performance", "trace", "inspection_review", "delivery_failure", "post_deploy_observation", "platform_resource_diagnosis", "docker_diagnosis", "virtualization_diagnosis", "oncall_brief"},
			SupportsAsync:    true,
			SupportsSkills:   true,
			SupportsToolsets: true,
			Config: map[string]any{
				"executionMode":  "runner_claim_callback",
				"runnerRequired": true,
				"resultContract": "kubecrux.analysisArtifact.v1",
			},
		},
	}
}

func defaultAgentCapabilities() []domaincopilot.AgentCapability {
	bindings := defaultAgentToolBindings()
	skillBindings := defaultAgentSkillBindings()
	return []domaincopilot.AgentCapability{
		{
			ID:             "root_cause",
			Name:           "根因分析",
			Category:       "observability",
			Description:    "汇总告警、事件、日志、指标、链路和发布上下文生成根因假设与建议。",
			AnalysisKinds:  []string{"root_cause"},
			RequiredScopes: []string{"cluster", "namespace", "workload", "alert"},
			ToolRefs:       []string{"platform.events", "observability.logs", "observability.metrics", "observability.traces", "delivery.releases"},
			ToolBindings:   filterToolBindings(bindings, "root_cause"),
			SkillBindings:  filterSkillBindings(skillBindings, "root_cause"),
		},
		{
			ID:             "performance",
			Name:           "性能分析",
			Category:       "observability",
			Description:    "聚合指标、事件和资源范围，分析容量、延迟、错误率和瓶颈。",
			AnalysisKinds:  []string{"performance"},
			RequiredScopes: []string{"cluster", "namespace", "workload"},
			ToolRefs:       []string{"observability.metrics", "platform.events"},
			ToolBindings:   filterToolBindings(bindings, "performance"),
			SkillBindings:  filterSkillBindings(skillBindings, "performance"),
		},
		{
			ID:             "trace",
			Name:           "链路分析",
			Category:       "observability",
			Description:    "通过 Trace/Metrics/事件上下文定位跨服务调用异常。",
			AnalysisKinds:  []string{"trace"},
			RequiredScopes: []string{"service", "workload"},
			ToolRefs:       []string{"observability.traces", "observability.metrics", "observability.logs"},
			ToolBindings:   filterToolBindings(bindings, "trace"),
			SkillBindings:  filterSkillBindings(skillBindings, "trace"),
		},
		{
			ID:             "inspection_review",
			Name:           "巡检复盘",
			Category:       "inspection",
			Description:    "将定期巡检结果转换为风险摘要、证据和整改动作。",
			AnalysisKinds:  []string{"inspection_review"},
			RequiredScopes: []string{"cluster", "namespace"},
			ToolRefs:       []string{"platform.events", "observability.metrics"},
			ToolBindings:   filterToolBindings(bindings, "inspection_review"),
			SkillBindings:  filterSkillBindings(skillBindings, "inspection_review"),
		},
		{
			ID:             "delivery_failure",
			Name:           "交付失败分析",
			Category:       "delivery",
			Description:    "关联构建、发布、执行任务和运行态事件定位交付失败原因。",
			AnalysisKinds:  []string{"delivery_failure"},
			RequiredScopes: []string{"application", "environment", "release"},
			ToolRefs:       []string{"delivery.builds", "delivery.releases", "delivery.execution_tasks", "platform.events"},
			ToolBindings:   filterToolBindings(bindings, "delivery_failure"),
			SkillBindings:  filterSkillBindings(skillBindings, "delivery_failure"),
		},
		{
			ID:             "post_deploy_observation",
			Name:           "发布后观察",
			Category:       "delivery",
			Description:    "围绕发布窗口检查告警、事件、指标与链路变化。",
			AnalysisKinds:  []string{"post_deploy_observation"},
			RequiredScopes: []string{"application", "cluster", "namespace"},
			ToolRefs:       []string{"delivery.releases", "observability.metrics", "observability.traces", "platform.events"},
			ToolBindings:   filterToolBindings(bindings, "post_deploy_observation"),
			SkillBindings:  filterSkillBindings(skillBindings, "post_deploy_observation"),
		},
		{
			ID:             "platform_resource_diagnosis",
			Name:           "平台资源诊断",
			Category:       "platform",
			Description:    "针对 Kubernetes 资源、节点、事件和配置漂移生成诊断结论。",
			AnalysisKinds:  []string{"platform_resource_diagnosis"},
			RequiredScopes: []string{"cluster", "namespace", "resource"},
			ToolRefs:       []string{"platform.resources", "platform.events", "observability.metrics"},
			ToolBindings:   filterToolBindings(bindings, "platform_resource_diagnosis"),
			SkillBindings:  filterSkillBindings(skillBindings, "platform_resource_diagnosis"),
		},
		{
			ID:             "docker_diagnosis",
			Name:           "Docker 工作台诊断",
			Category:       "docker",
			Description:    "关联 Docker host、Compose 项目、服务和 operation 日志分析故障。",
			AnalysisKinds:  []string{"docker_diagnosis"},
			RequiredScopes: []string{"dockerHost", "composeProject"},
			ToolRefs:       []string{"docker.operations", "docker.services"},
			ToolBindings:   filterToolBindings(bindings, "docker_diagnosis"),
			SkillBindings:  filterSkillBindings(skillBindings, "docker_diagnosis"),
		},
		{
			ID:             "virtualization_diagnosis",
			Name:           "虚拟化诊断",
			Category:       "virtualization",
			Description:    "关联虚拟机、连接、任务和运行时指标进行故障分析。",
			AnalysisKinds:  []string{"virtualization_diagnosis"},
			RequiredScopes: []string{"virtualizationConnection", "vm"},
			ToolRefs:       []string{"virtualization.operations", "observability.metrics"},
			ToolBindings:   filterToolBindings(bindings, "virtualization_diagnosis"),
			SkillBindings:  filterSkillBindings(skillBindings, "virtualization_diagnosis"),
		},
		{
			ID:             "oncall_brief",
			Name:           "OnCall 处置简报",
			Category:       "oncall",
			Description:    "为告警组和值班任务生成可执行处置摘要。",
			AnalysisKinds:  []string{"oncall_brief"},
			RequiredScopes: []string{"alert", "oncallRoute"},
			ToolRefs:       []string{"observability.alerts", "oncall.routes", "platform.events"},
			ToolBindings:   filterToolBindings(bindings, "oncall_brief"),
			SkillBindings:  filterSkillBindings(skillBindings, "oncall_brief"),
		},
	}
}

func defaultAgentToolBindings() []domaincopilot.AgentToolBinding {
	return []domaincopilot.AgentToolBinding{
		{ID: "platform.events", CapabilityID: "root_cause", ToolKind: "mcp", AdapterID: "platform-native.v1", ToolName: "events.query", PermissionKey: appaccess.PermObserveEventsView},
		{ID: "observability.logs", CapabilityID: "root_cause", ToolKind: "mcp", AdapterID: "logs.v1", ToolName: "logs.query", PermissionKey: appaccess.PermObserveAIChatUse},
		{ID: "observability.metrics", CapabilityID: "performance", ToolKind: "mcp", AdapterID: "metrics.v1", ToolName: "metrics.query", PermissionKey: appaccess.PermObserveAIChatUse},
		{ID: "observability.traces", CapabilityID: "trace", ToolKind: "mcp", AdapterID: "traces.v1", ToolName: "traces.query", PermissionKey: appaccess.PermObserveAIChatUse},
		{ID: "delivery.releases", CapabilityID: "delivery_failure", ToolKind: "internal_api", ToolName: "delivery.releases.list", PermissionKey: appaccess.PermDeliveryReleasesView},
		{ID: "delivery.builds", CapabilityID: "delivery_failure", ToolKind: "internal_api", ToolName: "delivery.builds.list", PermissionKey: appaccess.PermDeliveryApplicationsView},
		{ID: "delivery.execution_tasks", CapabilityID: "delivery_failure", ToolKind: "internal_api", ToolName: "delivery.execution_tasks.list", PermissionKey: appaccess.PermDeliveryExecutionTasksView},
		{ID: "platform.resources", CapabilityID: "platform_resource_diagnosis", ToolKind: "internal_api", ToolName: "platform.resources.snapshot", PermissionKey: appaccess.PermWorkspaceResourceView},
		{ID: "docker.operations", CapabilityID: "docker_diagnosis", ToolKind: "internal_api", ToolName: "docker.operations.list", PermissionKey: appaccess.PermDockerOperationsView},
		{ID: "docker.services", CapabilityID: "docker_diagnosis", ToolKind: "internal_api", ToolName: "docker.services.list", PermissionKey: appaccess.PermDockerServicesView},
		{ID: "virtualization.operations", CapabilityID: "virtualization_diagnosis", ToolKind: "internal_api", ToolName: "virtualization.operations.list", PermissionKey: appaccess.PermVirtualizationOperationsView},
		{ID: "observability.alerts", CapabilityID: "oncall_brief", ToolKind: "internal_api", ToolName: "alerts.list", PermissionKey: appaccess.PermObserveAlertsView},
		{ID: "oncall.routes", CapabilityID: "oncall_brief", ToolKind: "internal_api", ToolName: "oncall.routes.resolve", PermissionKey: appaccess.PermObserveOncallView},
	}
}

func defaultAgentSkillBindings() []domaincopilot.AgentSkillBinding {
	return []domaincopilot.AgentSkillBinding{
		{ID: "skill.root-cause.hermes", SkillID: "root-cause-investigation", ProviderID: agentProviderHermes, ProviderKind: "hermes", ProviderSkillRef: "kubecrux-root-cause", CapabilityRefs: []string{"root_cause", "performance", "trace"}},
		{ID: "skill.inspection.hermes", SkillID: "inspection-review", ProviderID: agentProviderHermes, ProviderKind: "hermes", ProviderSkillRef: "kubecrux-inspection-review", CapabilityRefs: []string{"inspection_review"}},
		{ID: "skill.delivery.hermes", SkillID: "delivery-failure-analysis", ProviderID: agentProviderHermes, ProviderKind: "hermes", ProviderSkillRef: "kubecrux-delivery-failure", CapabilityRefs: []string{"delivery_failure", "post_deploy_observation"}},
		{ID: "skill.platform.hermes", SkillID: "platform-diagnosis", ProviderID: agentProviderHermes, ProviderKind: "hermes", ProviderSkillRef: "kubecrux-platform-diagnosis", CapabilityRefs: []string{"platform_resource_diagnosis", "docker_diagnosis", "virtualization_diagnosis"}},
		{ID: "skill.oncall.hermes", SkillID: "oncall-brief", ProviderID: agentProviderHermes, ProviderKind: "hermes", ProviderSkillRef: "kubecrux-oncall-brief", CapabilityRefs: []string{"oncall_brief"}},
	}
}

func filterToolBindings(items []domaincopilot.AgentToolBinding, capabilityID string) []domaincopilot.AgentToolBinding {
	out := make([]domaincopilot.AgentToolBinding, 0)
	for _, item := range items {
		if item.CapabilityID == capabilityID || agentToolSharedWithCapability(item.ID, capabilityID) {
			out = append(out, item)
		}
	}
	return out
}

func filterSkillBindings(items []domaincopilot.AgentSkillBinding, capabilityID string) []domaincopilot.AgentSkillBinding {
	out := make([]domaincopilot.AgentSkillBinding, 0)
	for _, item := range items {
		for _, ref := range item.CapabilityRefs {
			if ref == capabilityID {
				out = append(out, item)
				break
			}
		}
	}
	return out
}

func agentToolSharedWithCapability(toolID, capabilityID string) bool {
	switch capabilityID {
	case "root_cause":
		return toolID == "observability.metrics" || toolID == "observability.traces" || toolID == "delivery.releases"
	case "performance":
		return toolID == "platform.events"
	case "trace":
		return toolID == "observability.metrics" || toolID == "observability.logs"
	case "inspection_review":
		return toolID == "platform.events" || toolID == "observability.metrics"
	case "post_deploy_observation":
		return toolID == "delivery.releases" || toolID == "observability.metrics" || toolID == "observability.traces" || toolID == "platform.events"
	case "platform_resource_diagnosis":
		return toolID == "platform.events" || toolID == "observability.metrics"
	case "virtualization_diagnosis":
		return toolID == "observability.metrics"
	case "oncall_brief":
		return toolID == "platform.events"
	default:
		return false
	}
}

func flattenToolBindings(capabilities []domaincopilot.AgentCapability) []domaincopilot.AgentToolBinding {
	out := make([]domaincopilot.AgentToolBinding, 0)
	seen := map[string]struct{}{}
	for _, capability := range capabilities {
		for _, binding := range capability.ToolBindings {
			if _, ok := seen[binding.ID]; ok {
				continue
			}
			seen[binding.ID] = struct{}{}
			out = append(out, binding)
		}
	}
	return out
}

func flattenSkillBindings(capabilities []domaincopilot.AgentCapability) []domaincopilot.AgentSkillBinding {
	out := make([]domaincopilot.AgentSkillBinding, 0)
	seen := map[string]struct{}{}
	for _, capability := range capabilities {
		for _, binding := range capability.SkillBindings {
			if _, ok := seen[binding.ID]; ok {
				continue
			}
			seen[binding.ID] = struct{}{}
			out = append(out, binding)
		}
	}
	return out
}

func (s *Service) synthesizeAgentArtifact(run domaincopilot.AgentRun) domaincopilot.AnalysisArtifact {
	summary := strings.TrimSpace(stringValue(run.Output["summary"]))
	if summary == "" {
		summary = strings.TrimSpace(stringValue(run.Output["rawOutput"]))
	}
	if summary == "" {
		summary = strings.TrimSpace(firstNonEmpty(run.ErrorMessage, stringValue(run.Output["error"])))
	}
	if summary == "" {
		summary = fmt.Sprintf("Agent run %s finished with status %s.", run.ID, firstNonEmpty(run.Status, "unknown"))
	}
	toolExecutions := run.ToolExecutions
	if len(toolExecutions) == 0 {
		toolExecutions = anyListToToolExecutions(run.Output["toolExecutions"])
	}
	return domaincopilot.AnalysisArtifact{
		Kind:            firstNonEmpty(run.CapabilityID, "agent_analysis"),
		RunID:           run.ID,
		Title:           fmt.Sprintf("%s analysis", firstNonEmpty(run.CapabilityID, "agent")),
		Summary:         summary,
		Scope:           run.Scope,
		Evidence:        anyListToEvidence(run.Output["evidence"]),
		Hypotheses:      anyListToHypotheses(run.Output["hypotheses"]),
		ToolExecutions:  toolExecutions,
		Graph:           anyToAnalysisGraph(run.Output["graph"]),
		Recommendations: anyListToStrings(run.Output["recommendations"]),
		DataSourceSnapshot: map[string]any{
			"providerId":     run.ProviderID,
			"providerKind":   run.ProviderKind,
			"capabilityId":   run.CapabilityID,
			"skillIds":       run.SkillIDs,
			"toolset":        run.Toolset,
			"externalRunId":  run.ExternalRunID,
			"agentRuntimeId": run.ID,
		},
	}
}

func anyListToEvidence(value any) []domaincopilot.RootCauseEvidence {
	var out []domaincopilot.RootCauseEvidence
	if decodeStructuredValue(value, &out) {
		return out
	}
	return nil
}

func anyListToHypotheses(value any) []domaincopilot.RootCauseHypothesis {
	var out []domaincopilot.RootCauseHypothesis
	if decodeStructuredValue(value, &out) {
		return out
	}
	return nil
}

func anyListToToolExecutions(value any) []domaincopilot.ToolExecution {
	var out []domaincopilot.ToolExecution
	if decodeStructuredValue(value, &out) {
		return out
	}
	return nil
}

func anyToAnalysisGraph(value any) *domaincopilot.AnalysisGraph {
	var out domaincopilot.AnalysisGraph
	if !decodeStructuredValue(value, &out) {
		return nil
	}
	if len(out.Nodes) == 0 && len(out.Edges) == 0 && strings.TrimSpace(out.FocusNodeID) == "" {
		return nil
	}
	return &out
}

func decodeStructuredValue(value any, target any) bool {
	if value == nil {
		return false
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return false
	}
	return json.Unmarshal(raw, target) == nil
}

func (s *Service) persistAgentRunMessage(ctx context.Context, run domaincopilot.AgentRun) error {
	artifacts := run.AnalysisArtifacts
	if len(artifacts) == 0 {
		artifacts = []domaincopilot.AnalysisArtifact{s.synthesizeAgentArtifact(run)}
	}
	reply := artifacts[0].Summary
	if strings.TrimSpace(reply) == "" {
		reply = fmt.Sprintf("Agent run %s completed.", run.ID)
	}
	_, err := s.repo.CreateMessage(ctx, domaincopilot.Message{
		ID:        uuid.NewString(),
		SessionID: run.SessionID,
		Role:      "assistant",
		Content:   reply,
		Metadata: map[string]any{
			"mode":              run.CapabilityID,
			"source":            "agent-runtime",
			"agentRunId":        run.ID,
			"agentProviderId":   run.ProviderID,
			"analysisArtifacts": artifacts,
			"toolCalls":         run.ToolExecutions,
		},
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	session, err := s.repo.GetSession(ctx, run.CreatedBy, run.SessionID)
	if err != nil {
		return nil
	}
	metadata := parseSessionMetadata(session.Metadata)
	metadata.Summary = reply
	metadata.AnalysisRunRefs = append(metadata.AnalysisRunRefs, domaincopilot.AnalysisRunRef{
		ID:        run.ID,
		Kind:      run.CapabilityID,
		Status:    run.Status,
		CreatedAt: run.CreatedAt.Format(time.RFC3339),
	})
	session.Metadata = sessionMetadataMap(metadata)
	session.UpdatedAt = time.Now().UTC()
	_, _ = s.repo.UpdateSession(ctx, run.CreatedBy, run.SessionID, session)
	return nil
}

func (s *Service) persistAgentRunRootCauseResult(ctx context.Context, run domaincopilot.AgentRun) error {
	rootRunOwner := firstNonEmpty(stringValue(run.Input["rootCauseRunOwner"]), run.CreatedBy)
	rootRun, err := s.repo.GetRootCauseRun(ctx, rootRunOwner, run.RootCauseRunID)
	if err != nil {
		return nil
	}
	artifact := s.synthesizeAgentArtifact(run)
	if len(run.AnalysisArtifacts) > 0 {
		artifact = run.AnalysisArtifacts[0]
	}
	rootRun.Status = agentRunStatusToRootCauseStatus(run.Status)
	rootRun.Summary = artifact.Summary
	rootRun.Severity = rootCauseSeverityFromArtifact(artifact, run.Status)
	rootRun.Evidence = artifact.Evidence
	rootRun.Hypotheses = artifact.Hypotheses
	rootRun.Recommendations = artifact.Recommendations
	rootRun.ToolExecutions = run.ToolExecutions
	if len(artifact.ToolExecutions) > 0 {
		rootRun.ToolExecutions = artifact.ToolExecutions
	}
	rootRun.DataSourceSnapshot = mergeAgentRunCallbackPayload(rootRun.DataSourceSnapshot, artifact.DataSourceSnapshot)
	rootRun.DataSourceSnapshot = mergeAgentRunCallbackPayload(rootRun.DataSourceSnapshot, map[string]any{
		"agentRunId":     run.ID,
		"agentRuntimeId": run.ID,
		"providerId":     run.ProviderID,
		"providerKind":   run.ProviderKind,
		"capabilityId":   run.CapabilityID,
		"externalRunId":  run.ExternalRunID,
		"status":         run.Status,
	})
	if run.ErrorMessage != "" {
		rootRun.DataSourceSnapshot["error"] = run.ErrorMessage
	}
	rootRun.PlaybookResults = mergeAgentRunCallbackPayload(rootRun.PlaybookResults, map[string]any{
		"agentRuntime": map[string]any{
			"providerId":    run.ProviderID,
			"providerKind":  run.ProviderKind,
			"agentRunId":    run.ID,
			"externalRunId": run.ExternalRunID,
			"status":        run.Status,
		},
	})
	rootRun.RemediationPlan = mergeAgentRunCallbackPayload(rootRun.RemediationPlan, map[string]any{
		"policy":  "suggest_only",
		"actions": buildRemediationActions(rootRun.Recommendations),
	})
	rootRun.UpdatedAt = time.Now().UTC()
	_, err = s.repo.UpdateRootCauseRun(ctx, rootRun)
	return err
}

func agentRunStatusToRootCauseStatus(status string) string {
	switch strings.TrimSpace(status) {
	case domaincopilot.AgentRunStatusCompleted:
		return "completed"
	case domaincopilot.AgentRunStatusCallbackTimeout:
		return domaincopilot.AgentRunStatusCallbackTimeout
	case domaincopilot.AgentRunStatusFailed, domaincopilot.AgentRunStatusCanceled:
		return strings.TrimSpace(status)
	default:
		return strings.TrimSpace(status)
	}
}

func rootCauseSeverityFromArtifact(artifact domaincopilot.AnalysisArtifact, status string) string {
	if status == domaincopilot.AgentRunStatusFailed || status == domaincopilot.AgentRunStatusCallbackTimeout {
		return "warning"
	}
	for _, item := range artifact.Evidence {
		if strings.TrimSpace(item.Severity) != "" {
			return highestEvidenceSeverity(artifact.Evidence)
		}
	}
	return "info"
}
