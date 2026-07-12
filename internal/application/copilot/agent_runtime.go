package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainalert "github.com/opensoha/soha/internal/domain/alert"
	domainbuild "github.com/opensoha/soha/internal/domain/build"
	domaincopilot "github.com/opensoha/soha/internal/domain/copilot"
	domaindelivery "github.com/opensoha/soha/internal/domain/delivery"
	domaindocker "github.com/opensoha/soha/internal/domain/docker"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	domainrelease "github.com/opensoha/soha/internal/domain/release"
	domainresource "github.com/opensoha/soha/internal/domain/resource"
	domainvirtualization "github.com/opensoha/soha/internal/domain/virtualization"
	aperrors "github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/telemetry"
	"go.uber.org/zap"
)

const (
	agentProviderInternal = "internal"
	agentProviderHermes   = "hermes"
)

var gatewayArtifactSensitiveValuePattern = regexp.MustCompile(`(?i)(token|password|passwd|secret|api[_-]?key|authorization|credential)(\s*[:=]\s*)([^\s,;]+)`)

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
	items, err := s.agentRuns.ListAgentRuns(ctx, domaincopilot.AgentRunFilter{Limit: 50})
	if err != nil {
		return nil, err
	}
	for index := range items {
		items[index].CallbackToken = ""
	}
	return domaincopilot.WithOperationStates(items, time.Now().UTC()), nil
}

func (s *Service) ClaimAgentRun(ctx context.Context, input domaincopilot.AgentRunClaimInput) (domaincopilot.AgentRun, error) {
	if strings.TrimSpace(input.AgentID) == "" {
		return domaincopilot.AgentRun{}, fmt.Errorf("%w: agentId is required", aperrors.ErrInvalidArgument)
	}
	if len(input.ProviderIDs) == 0 && len(input.Kinds) == 0 {
		input.ProviderIDs = []string{agentProviderHermes}
		input.Kinds = []string{agentProviderHermes}
	}
	run, err := s.agentRuns.ClaimAgentRun(ctx, input)
	if err != nil {
		return domaincopilot.AgentRun{}, err
	}
	return domaincopilot.WithOperationState(run, time.Now().UTC()), nil
}

func (s *Service) RecordAgentRunCallback(ctx context.Context, input domaincopilot.AgentRunCallbackInput) (domaincopilot.AgentRun, error) {
	if strings.TrimSpace(input.RunID) == "" || strings.TrimSpace(input.CallbackToken) == "" {
		return domaincopilot.AgentRun{}, fmt.Errorf("%w: runId and callbackToken are required", aperrors.ErrInvalidArgument)
	}
	input = domaincopilot.SanitizeAgentRunCallbackInput(input)
	input.Payload = normalizeAgentRunCallbackProviderUsage(input.Payload)
	input.AnalysisArtifacts = normalizeAgentRunCallbackProviderUsageArtifacts(input.AnalysisArtifacts, input.Payload)
	if agentRunCallbackProducesArtifact(input.Status) && len(input.AnalysisArtifacts) == 0 {
		if current, err := s.agentRuns.GetAgentRun(ctx, "", input.RunID); err == nil {
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
	updated, err := s.agentRuns.UpdateAgentRunCallback(ctx, input)
	if err != nil {
		return domaincopilot.AgentRun{}, err
	}
	if agentRunCallbackShouldPersistMessage(updated) {
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
	return domaincopilot.WithOperationState(updated, time.Now().UTC()), nil
}

func (s *Service) CancelAgentRun(ctx context.Context, principal domainidentity.Principal, runID string) (domaincopilot.AgentRun, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domaincopilot.AgentRun{}, err
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return domaincopilot.AgentRun{}, fmt.Errorf("%w: runId is required", aperrors.ErrInvalidArgument)
	}
	canceled, err := s.agentRuns.CancelAgentRun(ctx, domaincopilot.AgentRunCancelInput{
		RunID:       runID,
		RequestedBy: firstNonEmpty(principal.UserID, principal.UserName, "unknown"),
		Reason:      "canceled by user",
	})
	if err != nil {
		return domaincopilot.AgentRun{}, err
	}
	if strings.TrimSpace(canceled.RootCauseRunID) != "" {
		_ = s.persistAgentRunRootCauseResult(ctx, canceled)
	}
	if strings.TrimSpace(canceled.SessionID) != "" {
		_ = s.persistAgentRunMessage(ctx, canceled)
	}
	return domaincopilot.WithOperationState(canceled, time.Now().UTC()), nil
}

func (s *Service) RecordAgentToolCall(ctx context.Context, input domaincopilot.AgentToolCallInput) (domaincopilot.AgentToolCallResult, error) {
	if strings.TrimSpace(input.RunID) == "" || strings.TrimSpace(input.CallbackToken) == "" {
		return domaincopilot.AgentToolCallResult{}, fmt.Errorf("%w: runId and callbackToken are required", aperrors.ErrInvalidArgument)
	}
	run, err := s.agentRuns.GetAgentRun(ctx, "", input.RunID)
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
	updated, persistErr := s.agentRuns.UpdateAgentRunCallback(ctx, domaincopilot.AgentRunCallbackInput{
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
	if s == nil || s.agentRuns == nil {
		return 0, nil
	}
	now := time.Now().UTC()
	runs, err := s.agentRuns.ListAgentRuns(ctx, domaincopilot.AgentRunFilter{Status: domaincopilot.AgentRunStatusQueued, Limit: 200})
	if err != nil {
		return 0, err
	}
	runningRuns, err := s.agentRuns.ListAgentRuns(ctx, domaincopilot.AgentRunFilter{Status: domaincopilot.AgentRunStatusRunning, Limit: 200})
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
			AgentID:           firstNonEmpty(strings.TrimSpace(run.ClaimedByAgentID), "soha-control-plane"),
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

func agentRunCallbackShouldPersistMessage(run domaincopilot.AgentRun) bool {
	switch run.CallbackTransition {
	case domaincopilot.AgentRunCallbackTransitionNoopTerminal:
		return false
	}
	switch strings.ToLower(strings.TrimSpace(run.Status)) {
	case domaincopilot.AgentRunStatusCompleted, domaincopilot.AgentRunStatusFailed, domaincopilot.AgentRunStatusCanceled, domaincopilot.AgentRunStatusCallbackTimeout:
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

func normalizeAgentRunCallbackProviderUsage(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	out := mergeAgentRunCallbackPayload(payload, nil)
	if usage := agentProviderUsageSummaryFromRawCallback(out); len(usage) > 0 {
		out["providerUsage"] = usage
		out["usage"] = usage
	}
	return out
}

func normalizeAgentRunCallbackProviderUsageArtifacts(items []domaincopilot.AnalysisArtifact, payload map[string]any) []domaincopilot.AnalysisArtifact {
	if len(items) == 0 {
		return items
	}
	usage := agentProviderUsageSummaryFromPayload(payload)
	if len(usage) == 0 {
		return items
	}
	out := append([]domaincopilot.AnalysisArtifact(nil), items...)
	for index := range out {
		if out[index].DataSourceSnapshot == nil {
			out[index].DataSourceSnapshot = map[string]any{}
		}
		if len(mapValue(out[index].DataSourceSnapshot["providerUsage"])) == 0 {
			out[index].DataSourceSnapshot["providerUsage"] = usage
		}
		if len(mapValue(out[index].DataSourceSnapshot["usage"])) == 0 {
			out[index].DataSourceSnapshot["usage"] = usage
		}
	}
	return out
}

func agentProviderUsageSummaryFromPayload(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	for _, key := range []string{"providerUsage", "usage"} {
		if usage := agentUsageNumbersOnly(mapValue(payload[key])); len(usage) > 0 {
			return normalizeAgentProviderUsageSummary(usage)
		}
	}
	return agentProviderUsageSummary(payload)
}

func agentProviderUsageSummaryFromRawCallback(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	var standard map[string]any
	for _, key := range []string{"providerUsage", "usage"} {
		if usage := agentUsageNumbersOnly(mapValue(payload[key])); len(usage) > 0 {
			standard = normalizeAgentProviderUsageSummary(usage)
			break
		}
	}
	rawPayload := mergeAgentRunCallbackPayload(payload, nil)
	delete(rawPayload, "providerUsage")
	delete(rawPayload, "usage")
	if usage := agentProviderUsageSummary(rawPayload); len(usage) > 0 {
		if len(standard) > 0 {
			mergeAgentProviderUsageSummary(usage, standard)
			return normalizeAgentProviderUsageSummary(usage)
		}
		return usage
	}
	return standard
}

func agentProviderUsageSummary(value any) map[string]any {
	summary := map[string]any{}
	for _, usage := range agentProviderUsageCandidates(value) {
		mergeAgentProviderUsageSummary(summary, usage)
	}
	if len(summary) == 0 {
		return nil
	}
	return normalizeAgentProviderUsageSummary(summary)
}

func normalizeAgentProviderUsageSummary(summary map[string]any) map[string]any {
	if len(summary) == 0 {
		return nil
	}
	if _, ok := positiveFloat(summary["totalTokens"]); !ok {
		if total := positiveFloatSum(summary, "inputTokens", "outputTokens"); total > 0 {
			summary["totalTokens"] = total
		}
	}
	if _, ok := positiveFloat(summary["totalCost"]); !ok {
		if total := positiveFloatSum(summary, "inputCost", "outputCost"); total > 0 {
			summary["totalCost"] = total
		}
	}
	out := map[string]any{}
	for _, key := range []string{"totalTokens", "inputTokens", "outputTokens", "totalCost", "inputCost", "outputCost"} {
		if value, ok := positiveFloat(summary[key]); ok {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func agentProviderUsageCandidates(value any) []map[string]any {
	out := make([]map[string]any, 0)
	collectAgentProviderUsageCandidates(value, "$", 0, &out)
	return out
}

func collectAgentProviderUsageCandidates(value any, key string, depth int, out *[]map[string]any) {
	if out == nil || depth > 5 || value == nil {
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		collectAgentProviderUsageMap(typed, key, depth, out)
	case []any:
		for _, item := range typed {
			collectAgentProviderUsageCandidates(item, key, depth+1, out)
		}
	case []map[string]any:
		for _, item := range typed {
			collectAgentProviderUsageCandidates(item, key, depth+1, out)
		}
	}
}

func collectAgentProviderUsageMap(typed map[string]any, key string, depth int, out *[]map[string]any) {
	if context := agentUsageDetailContext(key); context != "" {
		if usage := agentProviderUsageDetailNumbers(typed, context); len(usage) > 0 {
			*out = append(*out, usage)
			collectAgentProviderUsageChildren(typed, depth, out, false)
			return
		}
	}
	if agentUsageContainerKey(key) {
		if usage := preferredAgentBilledUsageNumbers(typed); len(usage) > 0 {
			*out = append(*out, usage)
			collectAgentProviderUsageChildren(typed, depth, out, true)
			return
		}
		if usage := agentUsageNumbersOnly(typed); len(usage) > 0 {
			*out = append(*out, usage)
			collectAgentProviderUsageChildren(typed, depth, out, false)
			return
		}
	}
	if usage := agentProviderNativeUsageNumbers(typed); len(usage) > 0 {
		*out = append(*out, usage)
		return
	}
	for childKey, child := range typed {
		collectAgentProviderUsageCandidates(child, childKey, depth+1, out)
	}
}

func collectAgentProviderUsageChildren(typed map[string]any, depth int, out *[]map[string]any, skipBilled bool) {
	for childKey, child := range typed {
		if skipBilled && slices.Contains([]string{"billedunits", "billedunit", "tokens"}, normalizeAgentUsageKey(childKey)) {
			continue
		}
		switch child.(type) {
		case map[string]any, []any, []map[string]any:
			collectAgentProviderUsageCandidates(child, childKey, depth+1, out)
		}
	}
}

func preferredAgentBilledUsageNumbers(values map[string]any) map[string]any {
	rawBilled, ok := conditionRaw(values, "billed_units")
	if !ok {
		return nil
	}
	rawTokens, ok := conditionRaw(values, "tokens")
	if !ok || len(mapValue(rawTokens)) == 0 {
		return nil
	}
	billed := mapValue(rawBilled)
	if len(billed) == 0 {
		return nil
	}
	out := agentUsageNumbersOnly(values)
	if out == nil {
		out = map[string]any{}
	}
	mergeAgentProviderUsageSummary(out, agentUsageNumbersOnly(billed))
	if len(out) == 0 {
		return nil
	}
	return normalizeAgentProviderUsageSummary(out)
}

func agentUsageContainerKey(key string) bool {
	switch normalizeAgentUsageKey(key) {
	case "usage", "tokenusage", "aiusage", "providerusage", "llmusage", "metering", "billing", "costusage", "usagemetadata", "tokenmetadata", "tokencount", "tokencounts", "tokens", "billedunits", "billedunit":
		return true
	default:
		return false
	}
}

func agentUsageDetailContext(key string) string {
	switch normalizeAgentUsageKey(key) {
	case "prompttokendetails", "prompttokensdetails", "inputtokendetails", "inputtokensdetails", "requesttokendetails", "requesttokensdetails":
		return "input"
	case "completiontokendetails", "completiontokensdetails", "outputtokendetails", "outputtokensdetails", "responsetokendetails", "responsetokensdetails", "candidatestokendetails", "candidatestokensdetails":
		return "output"
	default:
		return ""
	}
}

func agentProviderUsageDetailNumbers(values map[string]any, context string) map[string]any {
	out := map[string]any{}
	for key, value := range values {
		number, ok := positiveFloat(value)
		if !ok {
			continue
		}
		normalized := normalizeAgentUsageKey(key)
		switch context {
		case "input":
			switch normalized {
			case "cachedtokens", "cachetokens", "cachecreationtokens", "cachereadtokens", "cachewritetokens", "audiotokens", "texttokens", "imagetokens":
				existing, _ := positiveFloat(out["inputTokens"])
				out["inputTokens"] = existing + normalizeNativeAgentUsageNumber(key, number)
			}
		case "output":
			switch normalized {
			case "reasoningtokens", "acceptedpredictiontokens", "rejectedpredictiontokens", "audiotokens", "texttokens", "imagetokens":
				existing, _ := positiveFloat(out["outputTokens"])
				out["outputTokens"] = existing + normalizeNativeAgentUsageNumber(key, number)
			}
		}
	}
	return normalizeAgentProviderUsageSummary(out)
}

func agentUsageNumbersOnly(values map[string]any) map[string]any {
	out := map[string]any{}
	hasGenericInput := agentNativeUsageHasAny(values, "inputTokens", "input_tokens", "inputTokensCount", "input_tokens_count", "inputTokenUsage", "input_token_usage", "promptTokens", "prompt_tokens", "promptTokensCount", "prompt_tokens_count", "promptTokenUsage", "prompt_token_usage", "promptTokenCount", "prompt_token_count", "inputTokenCount", "input_token_count", "promptEvalCount", "prompt_eval_count")
	hasGenericOutput := agentNativeUsageHasAny(values, "outputTokens", "output_tokens", "outputTokensCount", "output_tokens_count", "outputTokenUsage", "output_token_usage", "completionTokens", "completion_tokens", "completionTokensCount", "completion_tokens_count", "completionTokenUsage", "completion_token_usage", "candidatesTokenCount", "candidates_token_count", "outputTokenCount", "output_token_count", "evalCount", "eval_count")
	seen := map[string]struct{}{}
	for _, key := range agentUsageSummaryKeys() {
		normalized := normalizeAgentUsageKey(key)
		if _, ok := seen[normalized]; ok {
			continue
		}
		value, ok := conditionRaw(values, key)
		if !ok {
			continue
		}
		seen[normalized] = struct{}{}
		number, ok := positiveFloat(value)
		if !ok {
			continue
		}
		addNativeAgentUsageNumber(out, key, number, hasGenericInput, hasGenericOutput)
	}
	if supplemental := supplementalAgentInputTokenUsage(values); supplemental > 0 {
		existing, _ := positiveFloat(out["inputTokens"])
		out["inputTokens"] = existing + supplemental
	}
	if supplemental := supplementalAgentOutputTokenUsage(values); supplemental > 0 {
		existing, _ := positiveFloat(out["outputTokens"])
		out["outputTokens"] = existing + supplemental
	}
	return out
}

func mergeAgentProviderUsageSummary(dst map[string]any, src map[string]any) {
	if dst == nil || len(src) == 0 {
		return
	}
	for key, value := range agentUsageWithDerivedTotals(src) {
		number, ok := positiveFloat(value)
		if !ok {
			continue
		}
		canonical := canonicalAgentUsageKey(key)
		if existing, ok := positiveFloat(dst[canonical]); ok {
			dst[canonical] = existing + number
		} else {
			dst[canonical] = number
		}
	}
}

func agentProviderNativeUsageNumbers(values map[string]any) map[string]any {
	out := map[string]any{}
	hasGenericInput := agentNativeUsageHasAny(values, "inputTokens", "input_tokens", "inputTokensCount", "input_tokens_count", "inputTokenUsage", "input_token_usage", "promptTokens", "prompt_tokens", "promptTokensCount", "prompt_tokens_count", "promptTokenUsage", "prompt_token_usage", "promptTokenCount", "prompt_token_count", "inputTokenCount", "input_token_count", "promptEvalCount", "prompt_eval_count")
	hasGenericOutput := agentNativeUsageHasAny(values, "outputTokens", "output_tokens", "outputTokensCount", "output_tokens_count", "outputTokenUsage", "output_token_usage", "completionTokens", "completion_tokens", "completionTokensCount", "completion_tokens_count", "completionTokenUsage", "completion_token_usage", "candidatesTokenCount", "candidates_token_count", "outputTokenCount", "output_token_count", "evalCount", "eval_count")
	tokenKeys := []string{
		"promptTokenCount", "prompt_token_count", "promptTokensCount", "prompt_tokens_count", "promptTokenUsage", "prompt_token_usage", "inputTokenCount", "input_token_count", "inputTokensCount", "input_tokens_count", "inputTokenUsage", "input_token_usage",
		"cachedContentTokenCount", "cached_content_token_count", "cachedContentTokens", "cached_content_tokens",
		"toolUsePromptTokenCount", "tool_use_prompt_token_count", "toolUsePromptTokens", "tool_use_prompt_tokens",
		"promptTokensDetailsCachedTokens", "prompt_tokens_details_cached_tokens",
		"cachedTokens", "cached_tokens", "promptCacheHitTokens", "prompt_cache_hit_tokens", "promptCacheMissTokens", "prompt_cache_miss_tokens",
		"cacheReadTokens", "cache_read_tokens",
		"readUnits", "read_units", "inputUnits", "input_units", "requestUnits", "request_units",
		"textInputTokens", "text_input_tokens", "imageInputTokens", "image_input_tokens", "imageTokens", "image_tokens", "videoTokens", "video_tokens", "audioInputTokens", "audio_input_tokens", "audioTokens", "audio_tokens",
		"candidatesTokenCount", "candidates_token_count", "outputTokenCount", "output_token_count", "outputTokensCount", "output_tokens_count", "outputTokenUsage", "output_token_usage", "completionTokensCount", "completion_tokens_count", "completionTokenUsage", "completion_token_usage",
		"completionTokensDetailsReasoningTokens", "completion_tokens_details_reasoning_tokens",
		"reasoningTokens", "reasoning_tokens",
		"thoughtsTokenCount", "thoughts_token_count", "thoughtsTokens", "thoughts_tokens",
		"acceptedPredictionTokens", "accepted_prediction_tokens", "rejectedPredictionTokens", "rejected_prediction_tokens",
		"outputTokenDetailsReasoningTokens", "output_token_details_reasoning_tokens",
		"completionReasoningTokens", "completion_reasoning_tokens", "outputReasoningTokens", "output_reasoning_tokens",
		"writeUnits", "write_units", "outputUnits", "output_units", "responseUnits", "response_units",
		"totalTokenCount", "total_token_count", "billableTokens", "billable_tokens", "billedTokens", "billed_tokens", "usageTokens", "usage_tokens",
		"totalUnits", "total_units", "usageUnits", "usage_units", "searchUnits", "search_units", "classificationUnits", "classification_units", "classifications",
		"embeddingTokens", "embedding_tokens", "rerankTokens", "rerank_tokens",
		"queryUnits", "query_units", "searchRequests", "search_requests", "searchCredits", "search_credits", "serpapiSearches", "serpapi_searches", "braveSearchUnits", "brave_search_units",
		"browserMinutes", "browser_minutes", "browserSessions", "browser_sessions", "sessionMinutes", "session_minutes", "browserbaseMinutes", "browserbase_minutes", "pageLoads", "page_loads",
		"documentPages", "document_pages", "parsePages", "parse_pages", "llamaParsePages", "llama_parse_pages",
		"promptEvalCount", "prompt_eval_count", "evalCount", "eval_count",
		"inputTextTokens", "outputTextTokens",
		"inputImageTokens", "outputImageTokens",
		"inputAudioTokens", "outputAudioTokens",
		"textOutputTokens", "text_output_tokens", "imageOutputTokens", "image_output_tokens", "audioOutputTokens", "audio_output_tokens",
	}
	seen := map[string]struct{}{}
	for _, key := range tokenKeys {
		normalized := normalizeAgentUsageKey(key)
		if _, ok := seen[normalized]; ok {
			continue
		}
		raw, ok := conditionRaw(values, key)
		if !ok {
			continue
		}
		seen[normalized] = struct{}{}
		number, ok := positiveFloat(raw)
		if !ok {
			continue
		}
		addNativeAgentUsageNumber(out, key, number, hasGenericInput, hasGenericOutput)
	}
	if supplemental := supplementalAgentInputTokenUsage(values); supplemental > 0 {
		existing, _ := positiveFloat(out["inputTokens"])
		out["inputTokens"] = existing + supplemental
	}
	if supplemental := supplementalAgentOutputTokenUsage(values); supplemental > 0 {
		existing, _ := positiveFloat(out["outputTokens"])
		out["outputTokens"] = existing + supplemental
	}
	costKeys := []string{"responseCost", "response_cost", "totalCostUsd", "total_cost_usd", "totalCostUSD", "total_cost_USD", "estimatedCost", "estimated_cost", "estimatedCostUsd", "estimated_cost_usd", "billedAmount", "billed_amount", "chargeAmount", "charge_amount", "creditsUsed", "credits_used", "costMicros", "cost_micros", "totalCostMicros", "total_cost_micros", "estimatedCostMicros", "estimated_cost_micros", "costCents", "cost_cents", "totalCostCents", "total_cost_cents", "estimatedCostCents", "estimated_cost_cents", "inputCost", "input_cost", "promptCost", "prompt_cost", "inputCostUsd", "input_cost_usd", "promptCostUsd", "prompt_cost_usd", "inputCostMicros", "input_cost_micros", "promptCostMicros", "prompt_cost_micros", "inputCostCents", "input_cost_cents", "promptCostCents", "prompt_cost_cents", "outputCost", "output_cost", "completionCost", "completion_cost", "outputCostUsd", "output_cost_usd", "completionCostUsd", "completion_cost_usd", "outputCostMicros", "output_cost_micros", "completionCostMicros", "completion_cost_micros", "outputCostCents", "output_cost_cents", "completionCostCents", "completion_cost_cents"}
	clear(seen)
	for _, key := range costKeys {
		normalized := normalizeAgentUsageKey(key)
		if _, ok := seen[normalized]; ok {
			continue
		}
		raw, ok := conditionRaw(values, key)
		if !ok {
			continue
		}
		seen[normalized] = struct{}{}
		number, ok := positiveFloat(raw)
		if !ok {
			continue
		}
		addNativeAgentUsageNumber(out, key, number, false, false)
	}
	if len(out) == 0 {
		return nil
	}
	return normalizeAgentProviderUsageSummary(out)
}

func agentNativeUsageHasAny(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := conditionRaw(values, key); ok {
			return true
		}
	}
	return false
}

func addNativeAgentUsageNumber(out map[string]any, key string, number float64, hasGenericInput, hasGenericOutput bool) {
	canonical := canonicalAgentUsageKey(key)
	if !canonicalAgentUsageKeyEnabled(canonical) {
		return
	}
	number = normalizeNativeAgentUsageNumber(key, number)
	normalized := normalizeAgentUsageKey(key)
	if supplementalAgentInputTokenKey(normalized) || supplementalAgentOutputTokenKey(normalized) {
		return
	}
	additiveInput := !hasGenericInput && slices.Contains(agentAdditiveInputUsageKeys, normalized)
	additiveOutput := !hasGenericOutput && slices.Contains(agentAdditiveOutputUsageKeys, normalized)
	if additiveInput || additiveOutput {
		existing, _ := positiveFloat(out[canonical])
		out[canonical] = existing + number
		return
	}
	if existing, ok := positiveFloat(out[canonical]); !ok || number > existing {
		out[canonical] = number
	}
}

var agentAdditiveInputUsageKeys = []string{
	"inputtexttokens", "inputimagetokens", "inputaudiotokens", "textinputtokens", "imageinputtokens",
	"audioinputtokens", "imagetokens", "videotokens", "audiotokens",
}

var agentAdditiveOutputUsageKeys = []string{
	"outputtexttokens", "outputimagetokens", "outputaudiotokens", "textoutputtokens", "imageoutputtokens", "audiooutputtokens",
}

func normalizeNativeAgentUsageNumber(key string, number float64) float64 {
	switch normalizeAgentUsageKey(key) {
	case "costmicros", "totalcostmicros", "estimatedcostmicros", "inputcostmicros", "promptcostmicros", "outputcostmicros", "completioncostmicros":
		return number / 1_000_000
	case "costcents", "totalcostcents", "estimatedcostcents", "inputcostcents", "promptcostcents", "outputcostcents", "completioncostcents":
		return number / 100
	default:
		return number
	}
}

func agentUsageWithDerivedTotals(values map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range values {
		canonical := canonicalAgentUsageKey(key)
		if number, ok := positiveFloat(value); ok {
			number = normalizeNativeAgentUsageNumber(key, number)
			if existing, ok := positiveFloat(out[canonical]); !ok || number > existing {
				out[canonical] = number
			}
			continue
		}
		if _, exists := out[canonical]; !exists {
			out[canonical] = value
		}
	}
	if _, ok := positiveFloat(out["totalTokens"]); !ok {
		if total := positiveFloatSum(out, "inputTokens", "outputTokens"); total > 0 {
			out["totalTokens"] = total
		}
	}
	if _, ok := positiveFloat(out["totalCost"]); !ok {
		if total := positiveFloatSum(out, "inputCost", "outputCost"); total > 0 {
			out["totalCost"] = total
		}
	}
	return out
}

func supplementalAgentInputTokenUsage(values map[string]any) float64 {
	total := 0.0
	for key, value := range values {
		if supplementalAgentInputTokenKey(normalizeAgentUsageKey(key)) {
			if number, ok := positiveFloat(value); ok {
				total += number
			}
		}
	}
	return total
}

func supplementalAgentOutputTokenUsage(values map[string]any) float64 {
	total := 0.0
	for key, value := range values {
		if supplementalAgentOutputTokenKey(normalizeAgentUsageKey(key)) {
			if number, ok := positiveFloat(value); ok {
				total += number
			}
		}
	}
	return total
}

func supplementalAgentInputTokenKey(normalized string) bool {
	switch normalized {
	case "cachedtokens", "cachetokens", "cachecreationinputtokens", "cachereadinputtokens", "cachewriteinputtokens", "cachecreationtokens", "cachereadtokens", "cachewritetokens", "cachedcontenttokencount", "cachedcontenttokens", "tooluseprompttokencount", "tooluseprompttokens", "promptcachereadtokens", "promptcachewritetokens", "promptcachehittokens", "promptcachemisstokens", "inputcachereadtokens", "inputcachewritetokens", "inputcachedtokens":
		return true
	default:
		return false
	}
}

func supplementalAgentOutputTokenKey(normalized string) bool {
	switch normalized {
	case "thoughtstokencount", "thoughtstokens", "acceptedpredictiontokens", "rejectedpredictiontokens":
		return true
	default:
		return false
	}
}

func agentUsageSummaryKeys() []string {
	return []string{
		"totalTokens", "total_tokens", "tokens", "tokenCount", "totalTokenCount", "total_token_count", "tokenUsage", "token_usage", "billableTokens", "billable_tokens", "billedTokens", "billed_tokens", "usageTokens", "usage_tokens", "totalUnits", "total_units", "usageUnits", "usage_units", "searchUnits", "search_units", "classificationUnits", "classification_units", "classifications", "embeddingTokens", "embedding_tokens", "rerankTokens", "rerank_tokens", "queryUnits", "query_units", "queries", "searchRequests", "search_requests", "searchCredits", "search_credits", "serpapiSearches", "serpapi_searches", "braveSearchUnits", "brave_search_units", "browserMinutes", "browser_minutes", "browserSessions", "browser_sessions", "sessionMinutes", "session_minutes", "browserbaseMinutes", "browserbase_minutes", "pageLoads", "page_loads", "documentPages", "document_pages", "parsePages", "parse_pages", "llamaParsePages", "llama_parse_pages", "documents", "chunks", "characters", "chars", "requestCount", "request_count", "requests", "providerRequests", "provider_requests",
		"inputTokens", "input_tokens", "inputTokensCount", "input_tokens_count", "inputTokenUsage", "input_token_usage", "promptTokens", "prompt_tokens", "promptTokensCount", "prompt_tokens_count", "promptTokenUsage", "prompt_token_usage", "promptTokenCount", "prompt_token_count", "inputTokenCount", "input_token_count", "promptEvalCount", "prompt_eval_count", "cachedContentTokenCount", "cached_content_token_count", "cachedContentTokens", "cached_content_tokens", "toolUsePromptTokenCount", "tool_use_prompt_token_count", "toolUsePromptTokens", "tool_use_prompt_tokens", "inputTextTokens", "input_text_tokens", "textInputTokens", "text_input_tokens", "inputImageTokens", "input_image_tokens", "imageInputTokens", "image_input_tokens", "imageTokens", "image_tokens", "videoTokens", "video_tokens", "inputAudioTokens", "input_audio_tokens", "audioInputTokens", "audio_input_tokens", "audioTokens", "audio_tokens", "readUnits", "read_units", "inputUnits", "input_units", "requestUnits", "request_units", "promptCacheReadTokens", "prompt_cache_read_tokens", "promptCacheWriteTokens", "prompt_cache_write_tokens", "promptCacheHitTokens", "prompt_cache_hit_tokens", "promptCacheMissTokens", "prompt_cache_miss_tokens", "inputCacheReadTokens", "input_cache_read_tokens", "inputCacheWriteTokens", "input_cache_write_tokens", "inputCachedTokens", "input_cached_tokens",
		"outputTokens", "output_tokens", "outputTokensCount", "output_tokens_count", "outputTokenUsage", "output_token_usage", "completionTokens", "completion_tokens", "completionTokensCount", "completion_tokens_count", "completionTokenUsage", "completion_token_usage", "candidatesTokenCount", "candidates_token_count", "outputTokenCount", "output_token_count", "evalCount", "eval_count", "outputTextTokens", "output_text_tokens", "textOutputTokens", "text_output_tokens", "outputImageTokens", "output_image_tokens", "imageOutputTokens", "image_output_tokens", "outputAudioTokens", "output_audio_tokens", "audioOutputTokens", "audio_output_tokens", "thoughtsTokenCount", "thoughts_token_count", "thoughtsTokens", "thoughts_tokens", "reasoningTokens", "reasoning_tokens", "completionReasoningTokens", "completion_reasoning_tokens", "outputReasoningTokens", "output_reasoning_tokens", "acceptedPredictionTokens", "accepted_prediction_tokens", "rejectedPredictionTokens", "rejected_prediction_tokens", "writeUnits", "write_units", "outputUnits", "output_units", "responseUnits", "response_units",
		"totalCost", "total_cost", "cost", "costUsd", "costUSD", "usd", "estimatedCost", "estimated_cost", "estimatedCostUsd", "estimated_cost_usd", "responseCost", "response_cost", "totalCostUsd", "total_cost_usd", "totalCostUSD", "total_cost_USD", "billedAmount", "billed_amount", "chargeAmount", "charge_amount", "creditsUsed", "credits_used", "costMicros", "cost_micros", "totalCostMicros", "total_cost_micros", "estimatedCostMicros", "estimated_cost_micros", "costCents", "cost_cents", "totalCostCents", "total_cost_cents", "estimatedCostCents", "estimated_cost_cents",
		"inputCost", "input_cost", "promptCost", "prompt_cost", "inputCostUsd", "input_cost_usd", "promptCostUsd", "prompt_cost_usd", "inputCostMicros", "input_cost_micros", "promptCostMicros", "prompt_cost_micros", "inputCostCents", "input_cost_cents", "promptCostCents", "prompt_cost_cents",
		"outputCost", "output_cost", "completionCost", "completion_cost", "outputCostUsd", "output_cost_usd", "completionCostUsd", "completion_cost_usd", "outputCostMicros", "output_cost_micros", "completionCostMicros", "completion_cost_micros", "outputCostCents", "output_cost_cents", "completionCostCents", "completion_cost_cents",
	}
}

func canonicalAgentUsageKey(key string) string {
	switch normalizeAgentUsageKey(key) {
	case "totaltokens", "tokens", "tokencount", "totaltokencount", "tokenusage", "billabletokens", "billedtokens", "usagetokens", "totalunits", "usageunits", "searchunits", "classificationunits", "classifications", "embeddingtokens", "reranktokens", "queryunits", "queries", "searchrequests", "searchcredits", "serpapisearches", "bravesearchunits", "browserminutes", "browsersessions", "sessionminutes", "browserbaseminutes", "pageloads", "documentpages", "parsepages", "llamaparsepages", "documents", "chunks", "characters", "chars", "requestcount", "requests", "providerrequests":
		return "totalTokens"
	case "inputtokens", "inputtokenscount", "inputtokenusage", "prompttokens", "prompttokenscount", "prompttokenusage", "prompttokencount", "inputtokencount", "promptevalcount", "cachedcontenttokencount", "cachedcontenttokens", "tooluseprompttokencount", "tooluseprompttokens", "inputtexttokens", "textinputtokens", "inputimagetokens", "imageinputtokens", "imagetokens", "videotokens", "inputaudiotokens", "audioinputtokens", "audiotokens", "prompttokensdetailscachedtokens", "cachedtokens", "cachereadtokens", "promptcachehittokens", "promptcachemisstokens", "readunits", "inputunits", "requestunits":
		return "inputTokens"
	case "outputtokens", "outputtokenscount", "outputtokenusage", "completiontokens", "completiontokenscount", "completiontokenusage", "candidatestokencount", "outputtokencount", "evalcount", "outputtexttokens", "textoutputtokens", "outputimagetokens", "imageoutputtokens", "outputaudiotokens", "audiooutputtokens", "thoughtstokencount", "thoughtstokens", "completiontokensdetailsreasoningtokens", "completionreasoningtokens", "outputtokendetailsreasoningtokens", "outputreasoningtokens", "reasoningtokens", "acceptedpredictiontokens", "rejectedpredictiontokens", "writeunits", "outputunits", "responseunits":
		return "outputTokens"
	case "totalcost", "cost", "costusd", "usd", "estimatedcost", "estimatedcostusd", "responsecost", "totalcostusd", "billedamount", "chargeamount", "creditsused", "costmicros", "totalcostmicros", "estimatedcostmicros", "costcents", "totalcostcents", "estimatedcostcents":
		return "totalCost"
	case "inputcost", "promptcost", "inputcostusd", "promptcostusd", "inputcostmicros", "promptcostmicros", "inputcostcents", "promptcostcents":
		return "inputCost"
	case "outputcost", "completioncost", "outputcostusd", "completioncostusd", "outputcostmicros", "completioncostmicros", "outputcostcents", "completioncostcents":
		return "outputCost"
	default:
		return strings.TrimSpace(key)
	}
}

func canonicalAgentUsageKeyEnabled(key string) bool {
	switch key {
	case "totalTokens", "inputTokens", "outputTokens", "totalCost", "inputCost", "outputCost":
		return true
	default:
		return false
	}
}

func normalizeAgentUsageKey(key string) string {
	replacer := strings.NewReplacer("_", "", "-", "", " ", "", ".", "")
	return replacer.Replace(strings.ToLower(strings.TrimSpace(key)))
}

func conditionRaw(values map[string]any, key string) (any, bool) {
	if values == nil {
		return nil, false
	}
	if value, ok := values[key]; ok {
		return value, true
	}
	normalized := normalizeAgentUsageKey(key)
	for candidate, value := range values {
		if normalizeAgentUsageKey(candidate) == normalized {
			return value, true
		}
	}
	return nil, false
}

func positiveFloatSum(values map[string]any, keys ...string) float64 {
	total := 0.0
	for _, key := range keys {
		if value, ok := positiveFloat(values[key]); ok {
			total += value
		}
	}
	return total
}

func positiveFloat(value any) (float64, bool) {
	switch typed := value.(type) {
	case int:
		return float64(typed), typed > 0
	case int32:
		return float64(typed), typed > 0
	case int64:
		return float64(typed), typed > 0
	case float32:
		return float64(typed), typed > 0
	case float64:
		return typed, typed > 0
	case json.Number:
		parsed, err := typed.Float64()
		return parsed, err == nil && parsed > 0
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(typed), 64)
		if err != nil {
			return 0, false
		}
		return parsed, parsed > 0
	default:
		return 0, false
	}
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
	case "delivery.execution_tasks.list":
		return s.executeAgentExecutionTasksTool(ctx, run, input)
	case "platform.resources.snapshot":
		return s.executeAgentPlatformResourcesTool(ctx, run, input)
	case "docker.operations.list":
		return s.executeAgentDockerOperationsTool(ctx, run, input)
	case "docker.services.list":
		return s.executeAgentDockerServicesTool(ctx, run, input)
	case "virtualization.operations.list":
		return s.executeAgentVirtualizationOperationsTool(ctx, run, input)
	case "alerts.list":
		return s.executeAgentAlertsTool(ctx, run, input)
	case "oncall.routes.resolve":
		return s.executeAgentOnCallResolveTool(ctx, run, input)
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
	result, err := s.logBackend().Correlate(ctx, source.BackendType, source.ID, source.Config, telemetry.LogCorrelationQuery{
		Scope: telemetry.LogScope{
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
	summary, err := s.metricBackend().Analyze(ctx, source.BackendType, source.ID, source.Config, telemetry.MetricRangeQuery{
		Scope: telemetry.MetricScope{
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
	result, err := s.traceBackend().FindSlowSpans(ctx, source.BackendType, source.ID, source.Config, telemetry.TraceQuery{
		Scope: telemetry.TraceScope{
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

func (s *Service) executeAgentExecutionTasksTool(ctx context.Context, run domaincopilot.AgentRun, input map[string]any) (map[string]any, error) {
	if s.execution == nil {
		return nil, fmt.Errorf("%w: execution task reader is not configured", aperrors.ErrNotFound)
	}
	limit := firstPositive(intCondition(input["limit"]), 20)
	items, err := s.execution.ListExecutionTasks(ctx, agentToolPrincipal(), domaindelivery.ExecutionTaskFilter{
		ApplicationID:            firstNonEmpty(stringValue(input["applicationId"]), stringValue(run.Input["applicationId"])),
		ApplicationEnvironmentID: firstNonEmpty(stringValue(input["applicationEnvironmentId"]), stringValue(run.Input["applicationEnvironmentId"])),
		ReleaseBundleID:          firstNonEmpty(stringValue(input["releaseBundleId"]), stringValue(run.Input["releaseBundleId"])),
		Status:                   stringValue(input["status"]),
		ProviderKind:             stringValue(input["providerKind"]),
		Limit:                    limit,
	})
	if err != nil {
		return nil, err
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return map[string]any{"executionTasks": agentExecutionTaskSummaries(items), "count": len(items)}, nil
}

func (s *Service) executeAgentPlatformResourcesTool(ctx context.Context, run domaincopilot.AgentRun, input map[string]any) (map[string]any, error) {
	if s.resources == nil {
		return nil, fmt.Errorf("%w: platform resource reader is not configured", aperrors.ErrNotFound)
	}
	clusterID := firstNonEmpty(stringValue(input["clusterId"]), run.Scope.ClusterID)
	if strings.TrimSpace(clusterID) == "" {
		return nil, fmt.Errorf("%w: clusterId is required for platform resource snapshot", aperrors.ErrInvalidArgument)
	}
	namespace := firstNonEmpty(stringValue(input["namespace"]), run.Scope.Namespace)
	limit := firstPositive(intCondition(input["limit"]), evidenceBudget(run.Toolset, 20), 20)
	principal := agentToolPrincipal()
	out := map[string]any{
		"clusterId":   clusterID,
		"namespace":   namespace,
		"generatedAt": time.Now().UTC().Format(time.RFC3339),
	}
	if nodes, err := s.resources.ListNodes(ctx, principal, clusterID); err == nil {
		out["nodes"] = agentNodeSummaries(limitNodes(nodes, minPositive(limit, 5)))
		out["nodeCount"] = len(nodes)
	} else {
		out["nodeError"] = err.Error()
	}
	if pods, err := s.resources.ListPods(ctx, principal, clusterID, namespace); err == nil {
		pods = filterAgentPods(pods, run)
		out["pods"] = agentPodSummaries(limitPods(pods, limit))
		out["podCount"] = len(pods)
	} else {
		out["podError"] = err.Error()
	}
	if deployments, err := s.resources.ListDeployments(ctx, principal, clusterID, namespace); err == nil {
		deployments = filterAgentDeployments(deployments, run)
		out["deployments"] = agentDeploymentSummaries(limitDeployments(deployments, limit))
		out["deploymentCount"] = len(deployments)
	} else {
		out["deploymentError"] = err.Error()
	}
	if services, err := s.resources.ListServices(ctx, principal, clusterID, namespace); err == nil {
		services = filterAgentServices(services, run)
		out["services"] = agentServiceSummaries(limitServices(services, limit))
		out["serviceCount"] = len(services)
	} else {
		out["serviceError"] = err.Error()
	}
	return out, nil
}

func (s *Service) executeAgentDockerOperationsTool(ctx context.Context, run domaincopilot.AgentRun, input map[string]any) (map[string]any, error) {
	if s.docker == nil {
		return nil, fmt.Errorf("%w: docker reader is not configured", aperrors.ErrNotFound)
	}
	limit := firstPositive(intCondition(input["limit"]), 20)
	page, err := s.docker.ListOperations(ctx, agentToolPrincipal(), domaindocker.OperationFilter{
		HostID:        firstNonEmpty(stringValue(input["hostId"]), stringValue(run.Input["dockerHostId"])),
		ProjectID:     firstNonEmpty(stringValue(input["projectId"]), stringValue(run.Input["composeProjectId"])),
		ServiceID:     firstNonEmpty(stringValue(input["serviceId"]), stringValue(run.Input["dockerServiceId"])),
		Status:        stringValue(input["status"]),
		OperationKind: stringValue(input["operationKind"]),
		Limit:         limit,
	})
	if err != nil {
		return nil, err
	}
	items := page.Items
	if len(items) > limit {
		items = items[:limit]
	}
	return map[string]any{"operations": agentDockerOperationSummaries(items), "count": len(items), "total": page.Total}, nil
}

func (s *Service) executeAgentDockerServicesTool(ctx context.Context, run domaincopilot.AgentRun, input map[string]any) (map[string]any, error) {
	if s.docker == nil {
		return nil, fmt.Errorf("%w: docker reader is not configured", aperrors.ErrNotFound)
	}
	limit := firstPositive(intCondition(input["limit"]), 20)
	page, err := s.docker.ListServices(ctx, agentToolPrincipal(), domaindocker.ServiceFilter{
		HostID:    firstNonEmpty(stringValue(input["hostId"]), stringValue(run.Input["dockerHostId"])),
		ProjectID: firstNonEmpty(stringValue(input["projectId"]), stringValue(run.Input["composeProjectId"])),
		Status:    stringValue(input["status"]),
		Search:    firstNonEmpty(stringValue(input["search"]), run.Scope.Service),
		Limit:     limit,
	})
	if err != nil {
		return nil, err
	}
	items := page.Items
	if len(items) > limit {
		items = items[:limit]
	}
	return map[string]any{"services": agentDockerServiceSummaries(items), "count": len(items), "total": page.Total}, nil
}

func (s *Service) executeAgentVirtualizationOperationsTool(ctx context.Context, run domaincopilot.AgentRun, input map[string]any) (map[string]any, error) {
	if s.virtualization == nil {
		return nil, fmt.Errorf("%w: virtualization reader is not configured", aperrors.ErrNotFound)
	}
	limit := firstPositive(intCondition(input["limit"]), 20)
	items, err := s.virtualization.ListOperations(ctx, agentToolPrincipal(), domainvirtualization.TaskFilter{
		Provider:     stringValue(input["provider"]),
		ConnectionID: firstNonEmpty(stringValue(input["connectionId"]), stringValue(run.Input["virtualizationConnectionId"])),
		VMID:         firstNonEmpty(stringValue(input["vmId"]), stringValue(run.Input["vmId"])),
		Status:       stringValue(input["status"]),
		TaskKind:     stringValue(input["taskKind"]),
		Limit:        limit,
	})
	if err != nil {
		return nil, err
	}
	if len(items) > limit {
		items = items[:limit]
	}
	return map[string]any{"operations": agentVirtualizationTaskSummaries(items), "count": len(items)}, nil
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

func (s *Service) executeAgentOnCallResolveTool(ctx context.Context, run domaincopilot.AgentRun, input map[string]any) (map[string]any, error) {
	if s.oncall == nil {
		return nil, fmt.Errorf("%w: on-call resolver is not configured", aperrors.ErrNotFound)
	}
	labels := map[string]string{}
	if raw, ok := input["labels"].(map[string]string); ok {
		for key, value := range raw {
			labels[key] = value
		}
	} else if rawMap, ok := input["labels"].(map[string]any); ok {
		for key, value := range rawMap {
			if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
				labels[key] = text
			}
		}
	}
	result, err := s.oncall.ResolveOnCall(ctx, agentToolPrincipal(), domainalert.OnCallResolveInput{
		AlertID:         firstNonEmpty(stringValue(input["alertId"]), run.Scope.AlertID),
		IntegrationID:   stringValue(input["integrationId"]),
		IntegrationType: stringValue(input["integrationType"]),
		BusinessLineID:  firstNonEmpty(stringValue(input["businessLineId"]), stringValue(run.Input["businessLineId"])),
		AlertCategory:   stringValue(input["alertCategory"]),
		AlertName:       firstNonEmpty(stringValue(input["alertName"]), stringValue(run.Input["alertName"])),
		Severity:        stringValue(input["severity"]),
		Service:         firstNonEmpty(stringValue(input["service"]), run.Scope.Service, run.Scope.Workload),
		Role:            stringValue(input["role"]),
		ClusterID:       firstNonEmpty(stringValue(input["clusterId"]), run.Scope.ClusterID),
		Namespace:       firstNonEmpty(stringValue(input["namespace"]), run.Scope.Namespace),
		Labels:          labels,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"resolution": result}, nil
}

func (s *Service) findAgentDataSource(ctx context.Context, sourceKind, adapterID string, input map[string]any) (domaincopilot.DataSource, bool, error) {
	sources, err := s.dataSources.ListDataSources(ctx)
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

func filterAgentPods(items []domainresource.PodView, run domaincopilot.AgentRun) []domainresource.PodView {
	if strings.TrimSpace(run.Scope.Workload) == "" {
		return items
	}
	workload := strings.ToLower(strings.TrimSpace(run.Scope.Workload))
	out := make([]domainresource.PodView, 0, len(items))
	for _, item := range items {
		haystack := strings.ToLower(item.Name + " " + item.Namespace)
		if strings.Contains(haystack, workload) {
			out = append(out, item)
		}
	}
	return out
}

func filterAgentDeployments(items []domainresource.DeploymentView, run domaincopilot.AgentRun) []domainresource.DeploymentView {
	if strings.TrimSpace(run.Scope.Workload) == "" {
		return items
	}
	out := make([]domainresource.DeploymentView, 0, len(items))
	for _, item := range items {
		if item.Name == run.Scope.Workload {
			out = append(out, item)
		}
	}
	return out
}

func filterAgentServices(items []domainresource.ServiceView, run domaincopilot.AgentRun) []domainresource.ServiceView {
	if strings.TrimSpace(run.Scope.Service) == "" && strings.TrimSpace(run.Scope.Workload) == "" {
		return items
	}
	needle := firstNonEmpty(run.Scope.Service, run.Scope.Workload)
	out := make([]domainresource.ServiceView, 0, len(items))
	for _, item := range items {
		if item.Name == needle {
			out = append(out, item)
			continue
		}
		for _, value := range item.Selector {
			if value == needle {
				out = append(out, item)
				break
			}
		}
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

func agentExecutionTaskSummaries(items []domaindelivery.ExecutionTask) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"id":                       item.ID,
			"releaseBundleId":          item.ReleaseBundleID,
			"applicationId":            item.ApplicationID,
			"applicationEnvironmentId": item.ApplicationEnvironmentID,
			"taskKind":                 item.TaskKind,
			"providerKind":             item.ProviderKind,
			"targetKind":               item.TargetKind,
			"status":                   item.Status,
			"attemptCount":             item.AttemptCount,
			"maxRetries":               item.MaxRetries,
			"timeoutSeconds":           item.TimeoutSeconds,
			"claimedByAgentId":         item.ClaimedByAgentID,
			"startedAt":                optionalAgentTime(item.StartedAt),
			"lastHeartbeatAt":          optionalAgentTime(item.LastHeartbeatAt),
			"finishedAt":               optionalAgentTime(item.FinishedAt),
			"createdAt":                agentTime(item.CreatedAt),
			"result":                   item.Result,
			"artifacts":                item.Artifacts,
		})
	}
	return out
}

func agentNodeSummaries(items []domainresource.NodeView) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"name":       item.Name,
			"status":     item.Status,
			"roles":      item.Roles,
			"version":    item.Version,
			"internalIp": item.InternalIP,
			"podCount":   item.PodCount,
			"resources":  item.Resources,
			"ageSeconds": item.AgeSeconds,
		})
	}
	return out
}

func agentPodSummaries(items []domainresource.PodView) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"name":            item.Name,
			"namespace":       item.Namespace,
			"phase":           item.Phase,
			"nodeName":        item.NodeName,
			"podIp":           item.PodIP,
			"readyContainers": item.ReadyContainers,
			"restarts":        item.Restarts,
			"requests":        item.Requests,
			"limits":          item.Limits,
			"ageSeconds":      item.AgeSeconds,
		})
	}
	return out
}

func agentDeploymentSummaries(items []domainresource.DeploymentView) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"name":            item.Name,
			"namespace":       item.Namespace,
			"desiredReplicas": item.DesiredReplicas,
			"readyReplicas":   item.ReadyReplicas,
			"updatedReplicas": item.UpdatedReplicas,
			"available":       item.Available,
			"labels":          item.Labels,
			"ageSeconds":      item.AgeSeconds,
		})
	}
	return out
}

func agentServiceSummaries(items []domainresource.ServiceView) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"name":       item.Name,
			"namespace":  item.Namespace,
			"type":       item.Type,
			"clusterIp":  item.ClusterIP,
			"ports":      item.Ports,
			"selector":   item.Selector,
			"ageSeconds": item.AgeSeconds,
		})
	}
	return out
}

func agentDockerOperationSummaries(items []domaindocker.Operation) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"id":                item.ID,
			"hostId":            item.HostID,
			"projectId":         item.ProjectID,
			"serviceId":         item.ServiceID,
			"operationKind":     item.OperationKind,
			"status":            item.Status,
			"requestedBy":       item.RequestedBy,
			"claimedByWorkerId": item.ClaimedByWorkerID,
			"attemptCount":      item.AttemptCount,
			"maxRetries":        item.MaxRetries,
			"timeoutSeconds":    item.TimeoutSeconds,
			"startedAt":         optionalAgentTime(item.StartedAt),
			"lastHeartbeatAt":   optionalAgentTime(item.LastHeartbeatAt),
			"finishedAt":        optionalAgentTime(item.FinishedAt),
			"createdAt":         agentTime(item.CreatedAt),
			"result":            item.Result,
		})
	}
	return out
}

func agentDockerServiceSummaries(items []domaindocker.Service) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"id":             item.ID,
			"hostId":         item.HostID,
			"projectId":      item.ProjectID,
			"name":           item.Name,
			"image":          item.Image,
			"status":         item.Status,
			"containerId":    item.ContainerID,
			"restartCount":   item.RestartCount,
			"cpuPercent":     item.CPUPercent,
			"memoryBytes":    item.MemoryBytes,
			"networkRxBytes": item.NetworkRxBytes,
			"networkTxBytes": item.NetworkTxBytes,
			"lastSeenAt":     optionalAgentTime(item.LastSeenAt),
		})
	}
	return out
}

func agentVirtualizationTaskSummaries(items []domainvirtualization.Task) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"id":                item.ID,
			"provider":          item.Provider,
			"connectionId":      item.ConnectionID,
			"vmId":              item.VMID,
			"taskKind":          item.TaskKind,
			"status":            item.Status,
			"requestedBy":       item.RequestedBy,
			"claimedByWorkerId": item.ClaimedByWorkerID,
			"attemptCount":      item.AttemptCount,
			"maxRetries":        item.MaxRetries,
			"timeoutSeconds":    item.TimeoutSeconds,
			"startedAt":         optionalAgentTime(item.StartedAt),
			"lastHeartbeatAt":   optionalAgentTime(item.LastHeartbeatAt),
			"finishedAt":        optionalAgentTime(item.FinishedAt),
			"createdAt":         agentTime(item.CreatedAt),
			"result":            item.Result,
		})
	}
	return out
}

func limitNodes(items []domainresource.NodeView, limit int) []domainresource.NodeView {
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

func limitPods(items []domainresource.PodView, limit int) []domainresource.PodView {
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

func limitDeployments(items []domainresource.DeploymentView, limit int) []domainresource.DeploymentView {
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
}

func limitServices(items []domainresource.ServiceView, limit int) []domainresource.ServiceView {
	if limit > 0 && len(items) > limit {
		return items[:limit]
	}
	return items
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

func (s *Service) createAgentRun(ctx context.Context, principal domainidentity.Principal, input domaincopilot.AgentRunInput) (domaincopilot.AgentRun, error) {
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
	toolBindings, err := s.filterAgentToolBindingsByPrincipal(ctx, principal, toolBindings)
	if err != nil {
		return domaincopilot.AgentRun{}, err
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
	created, err := s.agentRuns.CreateAgentRun(ctx, run)
	if err != nil {
		return domaincopilot.AgentRun{}, err
	}
	return domaincopilot.WithOperationState(created, time.Now().UTC()), nil
}

func (s *Service) RecordGatewayAnalysisArtifact(ctx context.Context, principal domainidentity.Principal, input domaincopilot.GatewayAnalysisArtifactInput) (domaincopilot.AgentRun, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domaincopilot.AgentRun{}, err
	}
	capabilityID := strings.TrimSpace(input.CapabilityID)
	if capabilityID == "" {
		capabilityID = "delivery_failure"
	}
	summary := strings.TrimSpace(input.Summary)
	if summary == "" {
		summary = "Gateway analysis artifact recorded."
	}
	now := time.Now().UTC()
	runID := "agent:" + uuid.NewString()
	snapshot := sanitizeGatewayArtifactSnapshot(input.DataSourceSnapshot)
	snapshot["providerId"] = agentProviderInternal
	snapshot["providerKind"] = "internal"
	snapshot["capabilityId"] = capabilityID
	snapshot["agentRuntimeId"] = runID
	snapshot["generatedAt"] = now.Format(time.RFC3339)
	snapshot["analysisRuntime"] = "gateway_in_process"
	snapshot["artifactContract"] = "soha.analysisArtifact.v1"
	snapshot["redactionBoundary"] = "soha-gateway"
	snapshot["operationBoundary"] = "read_only_analysis"
	artifact := domaincopilot.AnalysisArtifact{
		Kind:               capabilityID,
		RunID:              runID,
		Title:              firstNonEmpty(input.Title, "Gateway analysis"),
		Summary:            summary,
		Scope:              input.Scope,
		Evidence:           append([]domaincopilot.RootCauseEvidence(nil), input.Evidence...),
		Hypotheses:         append([]domaincopilot.RootCauseHypothesis(nil), input.Hypotheses...),
		Recommendations:    normalizeStringList(input.Recommendations),
		ToolExecutions:     append([]domaincopilot.ToolExecution(nil), input.ToolExecutions...),
		Graph:              input.Graph,
		DataSourceSnapshot: snapshot,
	}
	run := domaincopilot.AgentRun{
		ID:                runID,
		ProviderID:        agentProviderInternal,
		ProviderKind:      "internal",
		CapabilityID:      capabilityID,
		SkillIDs:          normalizeStringList(input.SkillIDs),
		CreatedBy:         firstNonEmpty(principal.UserID, automationRootCauseCreatedBy),
		Status:            domaincopilot.AgentRunStatusCompleted,
		Scope:             input.Scope,
		Toolset:           input.Toolset,
		Input:             sanitizeGatewayArtifactSnapshot(input.Input),
		Output:            sanitizeGatewayArtifactSnapshot(input.Output),
		ToolExecutions:    artifact.ToolExecutions,
		AnalysisArtifacts: []domaincopilot.AnalysisArtifact{artifact},
		CallbackToken:     uuid.NewString(),
		TimeoutSeconds:    0,
		QueuedAt:          now,
		StartedAt:         &now,
		LastHeartbeatAt:   &now,
		CompletedAt:       &now,
		CreatedAt:         now,
		UpdatedAt:         now,
	}
	created, err := s.agentRuns.CreateAgentRun(ctx, run)
	if err != nil {
		return domaincopilot.AgentRun{}, err
	}
	return domaincopilot.WithOperationState(created, time.Now().UTC()), nil
}

func (s *Service) QueueGatewayAnalysisAgentRun(ctx context.Context, principal domainidentity.Principal, input domaincopilot.GatewayAnalysisAgentRunInput) (domaincopilot.AgentRun, error) {
	if err := s.authorizePrincipal(ctx, principal, appaccess.PermObserveAIChatUse); err != nil {
		return domaincopilot.AgentRun{}, err
	}
	providerID := strings.TrimSpace(input.AgentProviderID)
	if providerID == "" {
		providerID = s.defaultExternalAgentProviderID()
	} else {
		providerID = normalizeAgentProviderID(providerID)
	}
	if !s.shouldUseExternalAgent(providerID) {
		return domaincopilot.AgentRun{}, fmt.Errorf("%w: enabled external agent provider is required", aperrors.ErrInvalidArgument)
	}
	capabilityID := strings.TrimSpace(input.CapabilityID)
	if capabilityID == "" {
		capabilityID = "delivery_failure"
	}
	summary := strings.TrimSpace(input.Summary)
	if summary == "" {
		summary = "Gateway analysis queued for external Agent Runtime provider."
	}
	snapshot := sanitizeGatewayArtifactSnapshot(input.DataSourceSnapshot)
	snapshot["source"] = firstNonEmpty(stringValue(snapshot["source"]), "ai-gateway")
	snapshot["providerId"] = providerID
	snapshot["capabilityId"] = capabilityID
	snapshot["analysisRuntime"] = "agent_runtime_claim_callback"
	snapshot["artifactContract"] = "soha.analysisArtifact.v1"
	snapshot["redactionBoundary"] = "soha-gateway"
	snapshot["operationBoundary"] = "read_only_analysis"
	agentInput := sanitizeGatewayArtifactSnapshot(input.Input)
	agentInput["summary"] = summary
	agentInput["title"] = firstNonEmpty(input.Title, "Gateway analysis")
	agentInput["capabilityId"] = capabilityID
	agentInput["dataSourceSnapshot"] = snapshot
	agentInput["output"] = sanitizeGatewayArtifactSnapshot(input.Output)
	agentInput["evidence"] = input.Evidence
	agentInput["hypotheses"] = input.Hypotheses
	agentInput["recommendations"] = normalizeStringList(input.Recommendations)
	agentInput["toolExecutions"] = input.ToolExecutions
	if input.Graph != nil {
		agentInput["graph"] = input.Graph
	}
	agentInput = sanitizeGatewayArtifactSnapshot(agentInput)
	return s.createAgentRun(ctx, principal, domaincopilot.AgentRunInput{
		ProviderID:     providerID,
		CapabilityID:   capabilityID,
		SkillIDs:       automationAgentSkillIDs(capabilityID, input.SkillIDs),
		CreatedBy:      firstNonEmpty(principal.UserID, automationRootCauseCreatedBy),
		Scope:          input.Scope,
		Toolset:        input.Toolset,
		Input:          agentInput,
		TimeoutSeconds: firstPositive(input.TimeoutSeconds, 600),
	})
}

func (s *Service) defaultExternalAgentProviderID() string {
	for _, provider := range s.agentProviderCatalog() {
		if !provider.Enabled || !provider.SupportsAsync {
			continue
		}
		if !s.shouldUseExternalAgent(provider.ID) {
			continue
		}
		return provider.ID
	}
	return ""
}

func sanitizeGatewayArtifactSnapshot(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	bytes, err := json.Marshal(values)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(bytes, &out); err != nil {
		return map[string]any{}
	}
	return sanitizeGatewayArtifactMap(out)
}

func sanitizeGatewayArtifactMap(values map[string]any) map[string]any {
	out := make(map[string]any, len(values))
	for key, value := range values {
		if gatewayArtifactSensitiveKey(key) {
			out[key] = "[REDACTED]"
			continue
		}
		out[key] = sanitizeGatewayArtifactValue(value)
	}
	return out
}

func sanitizeGatewayArtifactValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		return sanitizeGatewayArtifactMap(typed)
	case []any:
		out := make([]any, len(typed))
		for index, item := range typed {
			out[index] = sanitizeGatewayArtifactValue(item)
		}
		return out
	case string:
		return gatewayArtifactSensitiveValuePattern.ReplaceAllString(typed, "$1$2[REDACTED]")
	default:
		return typed
	}
}

func gatewayArtifactSensitiveKey(key string) bool {
	normalized := strings.ToLower(strings.TrimSpace(key))
	for _, needle := range []string{"token", "password", "passwd", "secret", "credential", "apikey", "api_key", "authorization", "kubeconfig", "envvar", "environmentvariable"} {
		if strings.Contains(normalized, needle) {
			return true
		}
	}
	return false
}

func (s *Service) agentToolBindingsForRun(provider domaincopilot.AgentProvider, capabilityID string, toolset domaincopilot.SessionToolset) []domaincopilot.AgentToolBinding {
	bindings := filterToolBindings(defaultAgentToolBindings(), capabilityID)
	out := make([]domaincopilot.AgentToolBinding, 0, len(bindings))
	for _, binding := range bindings {
		if binding.ProviderID != "" && binding.ProviderID != provider.ID {
			continue
		}
		if binding.ProviderKind != "" && binding.ProviderKind != provider.Kind {
			continue
		}
		if !toolsetAllowsTool(toolset, binding.AdapterID, binding.ToolName) {
			continue
		}
		out = append(out, binding)
	}
	return out
}

func (s *Service) filterAgentToolBindingsByPrincipal(ctx context.Context, principal domainidentity.Principal, bindings []domaincopilot.AgentToolBinding) ([]domaincopilot.AgentToolBinding, error) {
	if len(bindings) == 0 {
		return nil, nil
	}
	keys, err := appaccess.RuntimePermissionKeys(ctx, s.permissions, principal)
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		allowed[strings.TrimSpace(key)] = struct{}{}
	}
	out := make([]domaincopilot.AgentToolBinding, 0, len(bindings))
	for _, binding := range bindings {
		permissionKey := strings.TrimSpace(binding.PermissionKey)
		if permissionKey == "" {
			out = append(out, binding)
			continue
		}
		if _, ok := allowed[permissionKey]; ok {
			out = append(out, binding)
		}
	}
	return out, nil
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
			Name:             "soha 内置分析",
			Description:      "使用 soha 已有平台聚合、MCP 数据源和规则化 playbook 执行同步分析。",
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
			Description:      "通过 soha agent runner 领取任务并调用 Hermes CLI 或 Hermes Agent 能力执行深度分析。",
			Enabled:          true,
			Capabilities:     append([]string(nil), capabilities...),
			SupportedModes:   []string{"root_cause", "performance", "trace", "inspection_review", "delivery_failure", "post_deploy_observation", "platform_resource_diagnosis", "docker_diagnosis", "virtualization_diagnosis", "oncall_brief"},
			SupportsAsync:    true,
			SupportsSkills:   true,
			SupportsToolsets: true,
			Config: map[string]any{
				"executionMode":  "runner_claim_callback",
				"runnerRequired": true,
				"resultContract": "soha.analysisArtifact.v1",
			},
		},
	}
}

func defaultAgentCapabilities() []domaincopilot.AgentCapability {
	bindings := defaultAgentToolBindings()
	skillBindings := defaultAgentSkillBindings()
	return []domaincopilot.AgentCapability{
		agentCapability(bindings, skillBindings, "root_cause", "根因分析", "observability", "汇总告警、事件、日志、指标、链路和发布上下文生成根因假设与建议。", []string{"cluster", "namespace", "workload", "alert"}, []string{"platform.events", "observability.logs", "observability.metrics", "observability.traces", "delivery.releases"}),
		agentCapability(bindings, skillBindings, "performance", "性能分析", "observability", "聚合指标、事件和资源范围，分析容量、延迟、错误率和瓶颈。", []string{"cluster", "namespace", "workload"}, []string{"observability.metrics", "platform.events"}),
		agentCapability(bindings, skillBindings, "trace", "链路分析", "observability", "通过 Trace/Metrics/事件上下文定位跨服务调用异常。", []string{"service", "workload"}, []string{"observability.traces", "observability.metrics", "observability.logs"}),
		agentCapability(bindings, skillBindings, "inspection_review", "巡检复盘", "inspection", "将定期巡检结果转换为风险摘要、证据和整改动作。", []string{"cluster", "namespace"}, []string{"platform.events", "observability.metrics"}),
		agentCapability(bindings, skillBindings, "delivery_failure", "交付失败分析", "delivery", "关联构建、发布、执行任务和运行态事件定位交付失败原因。", []string{"application", "environment", "release"}, []string{"delivery.builds", "delivery.releases", "delivery.execution_tasks", "platform.events"}),
		agentCapability(bindings, skillBindings, "post_deploy_observation", "发布后观察", "delivery", "围绕发布窗口检查告警、事件、指标与链路变化。", []string{"application", "cluster", "namespace"}, []string{"delivery.releases", "observability.metrics", "observability.traces", "platform.events"}),
		agentCapability(bindings, skillBindings, "platform_resource_diagnosis", "平台资源诊断", "platform", "针对 Kubernetes 资源、节点、事件和配置漂移生成诊断结论。", []string{"cluster", "namespace", "resource"}, []string{"platform.resources", "platform.events", "observability.metrics"}),
		agentCapability(bindings, skillBindings, "docker_diagnosis", "Docker 工作台诊断", "docker", "关联 Docker host、Compose 项目、服务和 operation 日志分析故障。", []string{"dockerHost", "composeProject"}, []string{"docker.operations", "docker.services"}),
		agentCapability(bindings, skillBindings, "virtualization_diagnosis", "虚拟化诊断", "virtualization", "关联虚拟机、连接、任务和运行时指标进行故障分析。", []string{"virtualizationConnection", "vm"}, []string{"virtualization.operations", "observability.metrics"}),
		agentCapability(bindings, skillBindings, "oncall_brief", "OnCall 处置简报", "oncall", "为告警组和值班任务生成可执行处置摘要。", []string{"alert", "oncallRoute"}, []string{"observability.alerts", "oncall.routes", "platform.events"}),
	}
}

func agentCapability(toolBindings []domaincopilot.AgentToolBinding, skillBindings []domaincopilot.AgentSkillBinding, id, name, category, description string, scopes, toolRefs []string) domaincopilot.AgentCapability {
	return domaincopilot.AgentCapability{
		ID: id, Name: name, Category: category, Description: description, AnalysisKinds: []string{id},
		RequiredScopes: scopes, ToolRefs: toolRefs,
		ToolBindings: filterToolBindings(toolBindings, id), SkillBindings: filterSkillBindings(skillBindings, id),
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
		{ID: "skill.root-cause.hermes", SkillID: "root-cause-investigation", ProviderID: agentProviderHermes, ProviderKind: "hermes", ProviderSkillRef: "soha-root-cause", CapabilityRefs: []string{"root_cause", "performance", "trace"}},
		{ID: "skill.inspection.hermes", SkillID: "inspection-review", ProviderID: agentProviderHermes, ProviderKind: "hermes", ProviderSkillRef: "soha-inspection-review", CapabilityRefs: []string{"inspection_review"}},
		{ID: "skill.delivery.hermes", SkillID: "delivery-failure-analysis", ProviderID: agentProviderHermes, ProviderKind: "hermes", ProviderSkillRef: "soha-delivery-failure", CapabilityRefs: []string{"delivery_failure", "post_deploy_observation"}},
		{ID: "skill.platform.hermes", SkillID: "platform-diagnosis", ProviderID: agentProviderHermes, ProviderKind: "hermes", ProviderSkillRef: "soha-platform-diagnosis", CapabilityRefs: []string{"platform_resource_diagnosis", "docker_diagnosis", "virtualization_diagnosis"}},
		{ID: "skill.oncall.hermes", SkillID: "oncall-brief", ProviderID: agentProviderHermes, ProviderKind: "hermes", ProviderSkillRef: "soha-oncall-brief", CapabilityRefs: []string{"oncall_brief"}},
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
	snapshot := map[string]any{
		"providerId":     run.ProviderID,
		"providerKind":   run.ProviderKind,
		"capabilityId":   run.CapabilityID,
		"skillIds":       run.SkillIDs,
		"toolset":        run.Toolset,
		"sessionId":      run.SessionID,
		"rootCauseRunId": run.RootCauseRunID,
		"externalRunId":  run.ExternalRunID,
		"agentRuntimeId": run.ID,
		"agentRunId":     run.ID,
		"analysisRunId":  firstNonEmpty(run.RootCauseRunID, run.ID),
		"analysisKind":   firstNonEmpty(run.CapabilityID, "agent_analysis"),
	}
	if usage := agentProviderUsageSummaryFromPayload(run.Output); len(usage) > 0 {
		snapshot["providerUsage"] = usage
		snapshot["usage"] = usage
	}
	return domaincopilot.AnalysisArtifact{
		Kind:               firstNonEmpty(run.CapabilityID, "agent_analysis"),
		RunID:              run.ID,
		Title:              fmt.Sprintf("%s analysis", firstNonEmpty(run.CapabilityID, "agent")),
		Summary:            summary,
		Scope:              run.Scope,
		Evidence:           anyListToEvidence(run.Output["evidence"]),
		Hypotheses:         anyListToHypotheses(run.Output["hypotheses"]),
		ToolExecutions:     toolExecutions,
		Graph:              anyToAnalysisGraph(run.Output["graph"]),
		Recommendations:    anyListToStrings(run.Output["recommendations"]),
		DataSourceSnapshot: snapshot,
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
	_, err := s.messages.CreateMessage(ctx, domaincopilot.Message{
		ID:        uuid.NewString(),
		SessionID: run.SessionID,
		Role:      "assistant",
		Content:   reply,
		Metadata: finalWorkbenchMessageMetadata(map[string]any{
			"mode":              run.CapabilityID,
			"source":            "agent-runtime",
			"agentRunId":        run.ID,
			"agentProviderId":   run.ProviderID,
			"analysisArtifacts": artifacts,
			"toolCalls":         run.ToolExecutions,
			"workbenchEvents":   workbenchEventsFromValue(run.Output["workbenchEvents"]),
		}, run.ToolExecutions, artifacts, map[string]any{
			"status":        run.Status,
			"providerId":    run.ProviderID,
			"providerKind":  run.ProviderKind,
			"runId":         firstNonEmpty(run.RootCauseRunID, run.ID),
			"agentRunId":    run.ID,
			"externalRunId": run.ExternalRunID,
		}),
		CreatedAt: time.Now().UTC(),
	})
	if err != nil {
		return err
	}
	session, err := s.sessions.GetSession(ctx, run.CreatedBy, run.SessionID)
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
	_, _ = s.sessions.UpdateSession(ctx, run.CreatedBy, run.SessionID, session)
	return nil
}

func (s *Service) persistAgentRunRootCauseResult(ctx context.Context, run domaincopilot.AgentRun) error {
	rootRunOwner := firstNonEmpty(stringValue(run.Input["rootCauseRunOwner"]), run.CreatedBy)
	rootRun, err := s.rootCauseRuns.GetRootCauseRun(ctx, rootRunOwner, run.RootCauseRunID)
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
	_, err = s.rootCauseRuns.UpdateRootCauseRun(ctx, rootRun)
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
