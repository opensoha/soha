package aigateway

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func gatewayRateLimitRules(conditions map[string]any, policyID string) []gatewayInvocationLimit {
	values := gatewayConditionValues(conditions, "rateLimit", "rate_limit", "rateLimits")
	scope := gatewayLimitScope(values, "actor_client_tool")
	mode := gatewayRateLimitMode(values)
	burst := gatewayRateLimitBurst(values)
	out := make([]gatewayInvocationLimit, 0, 4)
	if limit, ok := gatewayFirstPositiveInt(values, "qps", "maxCallsPerSecond", "maxInvocationsPerSecond", "callsPerSecond", "invocationsPerSecond", "perSecond", "second"); ok {
		out = append(out, gatewayInvocationLimit{Kind: "rate limit", PolicyID: policyID, Limit: limit, Burst: burst, Window: time.Second, WindowLabel: "1s", Scope: scope, Mode: mode})
	}
	if limit, ok := gatewayFirstPositiveInt(values, "maxCallsPerMinute", "maxInvocationsPerMinute", "callsPerMinute", "invocationsPerMinute", "perMinute", "minute", "rpm"); ok {
		out = append(out, gatewayInvocationLimit{Kind: "rate limit", PolicyID: policyID, Limit: limit, Burst: burst, Window: time.Minute, WindowLabel: "1m", Scope: scope, Mode: mode})
	}
	if limit, ok := gatewayFirstPositiveInt(values, "maxCallsPerHour", "maxInvocationsPerHour", "callsPerHour", "invocationsPerHour", "perHour", "hour", "rph"); ok {
		out = append(out, gatewayInvocationLimit{Kind: "rate limit", PolicyID: policyID, Limit: limit, Burst: burst, Window: time.Hour, WindowLabel: "1h", Scope: scope, Mode: mode})
	}
	if limit, ok := gatewayFirstPositiveInt(values, "maxCalls", "maxInvocations", "limit"); ok {
		window, label := gatewayConditionWindow(values, time.Minute, "1m")
		out = append(out, gatewayInvocationLimit{Kind: "rate limit", PolicyID: policyID, Limit: limit, Burst: burst, Window: window, WindowLabel: label, Scope: scope, Mode: mode})
	}
	return out
}
func gatewayRateLimitMode(values map[string]any) string {
	mode := strings.ToLower(strings.TrimSpace(gatewayFirstString(values, "mode", "algorithm", "strategy")))
	mode = strings.ReplaceAll(mode, "-", "_")
	switch mode {
	case "gcra", "token_bucket", "tokenbucket", "leaky_bucket", "leakybucket", "smooth", "strict":
		return "gcra"
	case "sliding_window", "slidingwindow", "rolling_window", "rollingwindow", "audit_window", "auditwindow":
		return "sliding_window"
	default:
		return "counter"
	}
}
func gatewayRateLimitBurst(values map[string]any) int {
	if burst, ok := gatewayFirstPositiveInt(values, "burst", "burstSize", "capacity", "bucketSize", "maxBurst"); ok {
		return burst
	}
	return 1
}
func gatewayBudgetLimitRules(conditions map[string]any, policyID string) []gatewayInvocationLimit {
	values := gatewayConditionValues(conditions, "budget", "budgets", "budgetPolicy")
	scope := gatewayLimitScope(values, "actor_client")
	out := make([]gatewayInvocationLimit, 0, 4)
	if limit, ok := gatewayFirstPositiveInt(values, "maxCallsPerHour", "maxInvocationsPerHour", "hourlyCalls", "hourlyInvocations", "hourlyInvocationBudget"); ok {
		out = append(out, gatewayInvocationLimit{Kind: "budget", PolicyID: policyID, Limit: limit, Window: time.Hour, WindowLabel: "1h", Scope: scope})
	}
	if limit, ok := gatewayFirstPositiveInt(values, "maxCallsPerDay", "maxInvocationsPerDay", "maxDailyCalls", "maxDailyInvocations", "dailyCalls", "dailyInvocations", "dailyInvocationBudget", "dailyBudget", "daily"); ok {
		out = append(out, gatewayInvocationLimit{Kind: "budget", PolicyID: policyID, Limit: limit, Window: 24 * time.Hour, WindowLabel: "24h", Scope: scope})
	}
	if limit, ok := gatewayFirstPositiveInt(values, "maxCallsPerMonth", "maxInvocationsPerMonth", "maxMonthlyCalls", "maxMonthlyInvocations", "monthlyCalls", "monthlyInvocations", "monthlyInvocationBudget", "monthlyBudget", "monthly"); ok {
		out = append(out, gatewayInvocationLimit{Kind: "budget", PolicyID: policyID, Limit: limit, Window: 30 * 24 * time.Hour, WindowLabel: "30d", Scope: scope})
	}
	if limit, ok := gatewayFirstPositiveInt(values, "maxBudgetCalls", "maxBudgetInvocations"); ok {
		window, label := gatewayConditionWindow(values, 24*time.Hour, "24h")
		out = append(out, gatewayInvocationLimit{Kind: "budget", PolicyID: policyID, Limit: limit, Window: window, WindowLabel: label, Scope: scope})
	}
	return out
}
func gatewayUsageBudgetRules(conditions map[string]any, policyID string) []gatewayUsageBudget {
	values := gatewayConditionValues(conditions, "budget", "budgets", "budgetPolicy")
	scope := gatewayLimitScope(values, "actor_client")
	window, label := gatewayConditionWindow(values, 24*time.Hour, "24h")
	out := make([]gatewayUsageBudget, 0, 4)
	appendRule := func(kind string, limit float64, window time.Duration, label string) {
		if limit > 0 && window > 0 {
			out = append(out, gatewayUsageBudget{Kind: kind, PolicyID: policyID, Limit: limit, Window: window, WindowLabel: label, Scope: scope})
		}
	}
	if limit, ok := gatewayFirstPositiveFloat(values, "maxTokensPerHour", "hourlyTokens", "hourlyTokenBudget"); ok {
		appendRule("token budget", limit, time.Hour, "1h")
	}
	if limit, ok := gatewayFirstPositiveFloat(values, "maxTokensPerDay", "dailyTokens", "dailyTokenBudget"); ok {
		appendRule("token budget", limit, 24*time.Hour, "24h")
	}
	if limit, ok := gatewayFirstPositiveFloat(values, "maxTokensPerMonth", "monthlyTokens", "monthlyTokenBudget"); ok {
		appendRule("token budget", limit, 30*24*time.Hour, "30d")
	}
	if limit, ok := gatewayFirstPositiveFloat(values, "maxTokens", "maxTotalTokens", "tokenBudget", "maxBudgetTokens"); ok {
		appendRule("token budget", limit, window, label)
	}
	if limit, ok := gatewayFirstPositiveFloat(values, "maxCostPerHour", "hourlyCost", "hourlyCostBudget"); ok {
		appendRule("cost budget", limit, time.Hour, "1h")
	}
	if limit, ok := gatewayFirstPositiveFloat(values, "maxCostPerDay", "dailyCost", "dailyCostBudget"); ok {
		appendRule("cost budget", limit, 24*time.Hour, "24h")
	}
	if limit, ok := gatewayFirstPositiveFloat(values, "maxCostPerMonth", "monthlyCost", "monthlyCostBudget"); ok {
		appendRule("cost budget", limit, 30*24*time.Hour, "30d")
	}
	if limit, ok := gatewayFirstPositiveFloat(values, "maxCost", "maxSpend", "costBudget", "maxBudgetCost"); ok {
		appendRule("cost budget", limit, window, label)
	}
	return out
}
func (s *Service) enforceGatewayInvocationLimit(ctx context.Context, principal domainidentity.Principal, aiClientID, toolName string, limit gatewayInvocationLimit) error {
	if limit.Limit <= 0 || limit.Window <= 0 || s == nil || s.repo == nil {
		return nil
	}
	if strings.EqualFold(limit.Kind, "rate limit") && limit.Mode == "gcra" {
		state, err := s.applyGatewayRateLimitState(ctx, principal, aiClientID, toolName, limit)
		if err == nil {
			if state.Allowed {
				return nil
			}
			return fmt.Errorf("%w: AI Gateway %s policy %s exceeded for %s (retry after %s)", apperrors.ErrAccessDenied, limit.Kind, strings.TrimSpace(limit.PolicyID), toolName, formatGatewayRateLimitRetryAfter(state.RetryAfter))
		}
		if !gatewayRateLimitStateFallbackAllowed(err) {
			return err
		}
	}
	if strings.EqualFold(limit.Kind, "rate limit") && limit.Mode == "counter" {
		counter, err := s.incrementGatewayRateLimitCounter(ctx, principal, aiClientID, toolName, limit)
		if err == nil {
			if counter.Count <= limit.Limit {
				return nil
			}
			return fmt.Errorf("%w: AI Gateway %s policy %s exceeded for %s (%d/%d accepted calls in %s)", apperrors.ErrAccessDenied, limit.Kind, strings.TrimSpace(limit.PolicyID), toolName, counter.Count-1, limit.Limit, limit.WindowLabel)
		}
		if !gatewayRateLimitCounterFallbackAllowed(err) {
			return err
		}
	}
	count, err := s.gatewayAcceptedInvocationCount(ctx, principal, aiClientID, toolName, limit)
	if err != nil {
		return err
	}
	if count < limit.Limit {
		return nil
	}
	return fmt.Errorf("%w: AI Gateway %s policy %s exceeded for %s (%d/%d accepted calls in %s)", apperrors.ErrAccessDenied, limit.Kind, strings.TrimSpace(limit.PolicyID), toolName, count, limit.Limit, limit.WindowLabel)
}
func (s *Service) incrementGatewayRateLimitCounter(ctx context.Context, principal domainidentity.Principal, aiClientID, toolName string, limit gatewayInvocationLimit) (domainaigateway.RateLimitCounter, error) {
	actorType, actorID := gatewaySubject(principal)
	windowStart, windowEnd := gatewayRateLimitWindow(time.Now().UTC(), limit.Window)
	key := gatewayRateLimitCounterKey(actorType, actorID, aiClientID, toolName, limit, windowStart)
	counter := domainaigateway.RateLimitCounter{
		Key:         key,
		PolicyID:    strings.TrimSpace(limit.PolicyID),
		Scope:       normalizeGatewayLimitScope(limit.Scope),
		ActorType:   actorType,
		ActorID:     actorID,
		AIClientID:  strings.TrimSpace(aiClientID),
		ToolName:    strings.TrimSpace(toolName),
		WindowStart: windowStart,
		WindowEnd:   windowEnd,
		Limit:       limit.Limit,
		Metadata: map[string]any{
			"kind":        limit.Kind,
			"windowLabel": limit.WindowLabel,
		},
	}
	if s == nil {
		return counter, nil
	}
	if s.rateLimits != nil {
		next, err := s.rateLimits.IncrementRateLimitCounter(ctx, counter)
		if err == nil {
			return next, nil
		}
		if !gatewayExternalRateLimitFallbackAllowed(err) {
			return domainaigateway.RateLimitCounter{}, err
		}
	}
	if s.repo == nil {
		return counter, nil
	}
	return s.repo.IncrementRateLimitCounter(ctx, counter)
}
func (s *Service) applyGatewayRateLimitState(ctx context.Context, principal domainidentity.Principal, aiClientID, toolName string, limit gatewayInvocationLimit) (domainaigateway.RateLimitState, error) {
	burst := limit.Burst
	if burst <= 0 {
		burst = 1
	}
	actorType, actorID := gatewaySubject(principal)
	key := gatewayRateLimitStateKey(actorType, actorID, aiClientID, toolName, limit)
	state := domainaigateway.RateLimitState{
		Key:             key,
		PolicyID:        strings.TrimSpace(limit.PolicyID),
		Scope:           normalizeGatewayLimitScope(limit.Scope),
		ActorType:       actorType,
		ActorID:         actorID,
		AIClientID:      strings.TrimSpace(aiClientID),
		ToolName:        strings.TrimSpace(toolName),
		Limit:           limit.Limit,
		Burst:           burst,
		IntervalSeconds: limit.Window.Seconds() / float64(limit.Limit),
		Metadata: map[string]any{
			"kind":        limit.Kind,
			"mode":        "gcra",
			"windowLabel": limit.WindowLabel,
		},
	}
	if s == nil {
		return state, nil
	}
	if s.rateLimits != nil {
		next, err := s.rateLimits.ApplyRateLimitState(ctx, state)
		if err == nil {
			return next, nil
		}
		if !gatewayExternalRateLimitFallbackAllowed(err) {
			return domainaigateway.RateLimitState{}, err
		}
	}
	if s.repo == nil {
		return state, nil
	}
	return s.repo.ApplyRateLimitState(ctx, state)
}
func gatewayExternalRateLimitFallbackAllowed(err error) bool {
	if err == nil {
		return false
	}
	return !errors.Is(err, context.Canceled)
}
func gatewayRateLimitCounterFallbackAllowed(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "ai_gateway_rate_limit_counters") && strings.Contains(text, "does not exist")
}
func gatewayRateLimitStateFallbackAllowed(err error) bool {
	if err == nil {
		return false
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "ai_gateway_rate_limit_states") && strings.Contains(text, "does not exist")
}
func formatGatewayRateLimitRetryAfter(value time.Duration) string {
	if value <= 0 {
		return "now"
	}
	if value < time.Second {
		return value.String()
	}
	return value.Round(time.Second).String()
}
func (s *Service) enforceGatewayUsageBudget(ctx context.Context, principal domainidentity.Principal, aiClientID, toolName string, budget gatewayUsageBudget) error {
	if budget.Limit <= 0 || budget.Window <= 0 || s == nil || s.repo == nil {
		return nil
	}
	used, err := s.gatewayUsageBudgetConsumed(ctx, principal, aiClientID, toolName, budget)
	if err != nil {
		return err
	}
	if used < budget.Limit {
		return nil
	}
	return fmt.Errorf("%w: AI Gateway %s policy %s exceeded for %s (%s/%s in %s)", apperrors.ErrAccessDenied, budget.Kind, strings.TrimSpace(budget.PolicyID), toolName, formatGatewayBudgetValue(used), formatGatewayBudgetValue(budget.Limit), budget.WindowLabel)
}
func (s *Service) gatewayAcceptedInvocationCount(ctx context.Context, principal domainidentity.Principal, aiClientID, toolName string, limit gatewayInvocationLimit) (int, error) {
	now := time.Now().UTC()
	from := now.Add(-limit.Window)
	filter := gatewayLimitAuditFilter(principal, aiClientID, toolName, limit.Scope)
	filter.Action = "ai_gateway.tool.invoke"
	filter.From = &from
	filter.To = &now
	filter.Limit = limit.Limit + 1
	if filter.Limit <= 0 || filter.Limit > 500 {
		filter.Limit = 500
	}
	items, err := s.repo.ListAuditLogs(ctx, filter)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, item := range items {
		if gatewayAuditResultCountsTowardLimits(item.Result) {
			count++
		}
	}
	return count, nil
}
func (s *Service) gatewayUsageBudgetConsumed(ctx context.Context, principal domainidentity.Principal, aiClientID, toolName string, budget gatewayUsageBudget) (float64, error) {
	now := time.Now().UTC()
	from := now.Add(-budget.Window)
	filter := gatewayLimitAuditFilter(principal, aiClientID, toolName, budget.Scope)
	filter.Action = "ai_gateway.tool.invoke"
	filter.From = &from
	filter.To = &now
	filter.Limit = 500
	items, err := s.repo.ListAuditLogs(ctx, filter)
	if err != nil {
		return 0, err
	}
	used := 0.0
	for _, item := range items {
		if !gatewayAuditResultCountsTowardLimits(item.Result) {
			continue
		}
		switch budget.Kind {
		case "token budget":
			used += gatewayAuditTokenUsage(item)
		case "cost budget":
			used += gatewayAuditCostUsage(item)
		}
	}
	return used, nil
}
func gatewayLimitAuditFilter(principal domainidentity.Principal, aiClientID, toolName, scope string) domainaigateway.AuditLogFilter {
	actorType, actorID := gatewaySubject(principal)
	scope = normalizeGatewayLimitScope(scope)
	filter := domainaigateway.AuditLogFilter{}
	switch scope {
	case "global":
	case "client":
		filter.AIClientID = strings.TrimSpace(aiClientID)
	case "client_tool":
		filter.AIClientID = strings.TrimSpace(aiClientID)
		filter.ToolName = strings.TrimSpace(toolName)
	case "actor":
		filter.ActorType = actorType
		filter.ActorID = actorID
	case "actor_tool":
		filter.ActorType = actorType
		filter.ActorID = actorID
		filter.ToolName = strings.TrimSpace(toolName)
	case "actor_client":
		filter.ActorType = actorType
		filter.ActorID = actorID
		filter.AIClientID = strings.TrimSpace(aiClientID)
	default:
		filter.ActorType = actorType
		filter.ActorID = actorID
		filter.AIClientID = strings.TrimSpace(aiClientID)
		filter.ToolName = strings.TrimSpace(toolName)
	}
	return filter
}
func gatewayRateLimitWindow(now time.Time, window time.Duration) (time.Time, time.Time) {
	if window <= 0 {
		window = time.Minute
	}
	unixNano := now.UnixNano()
	windowNano := int64(window)
	if windowNano <= 0 {
		windowNano = int64(time.Minute)
	}
	startUnix := unixNano - unixNano%windowNano
	start := time.Unix(0, startUnix).UTC()
	return start, start.Add(window)
}
func gatewayRateLimitCounterKey(actorType, actorID, aiClientID, toolName string, limit gatewayInvocationLimit, windowStart time.Time) string {
	scope := normalizeGatewayLimitScope(limit.Scope)
	if scope == "" {
		scope = "actor_client_tool"
	}
	parts := append(gatewayLimitKeyParts(limit, scope),
		"window", windowStart.UTC().Format(time.RFC3339Nano),
	)
	switch scope {
	case "global":
	case "client":
		parts = append(parts, "client", strings.TrimSpace(aiClientID))
	case "client_tool":
		parts = append(parts, "client", strings.TrimSpace(aiClientID), "tool", strings.TrimSpace(toolName))
	case "actor":
		parts = append(parts, "actor", strings.TrimSpace(actorType), strings.TrimSpace(actorID))
	case "actor_tool":
		parts = append(parts, "actor", strings.TrimSpace(actorType), strings.TrimSpace(actorID), "tool", strings.TrimSpace(toolName))
	case "actor_client":
		parts = append(parts, "actor", strings.TrimSpace(actorType), strings.TrimSpace(actorID), "client", strings.TrimSpace(aiClientID))
	default:
		parts = append(parts, "actor", strings.TrimSpace(actorType), strings.TrimSpace(actorID), "client", strings.TrimSpace(aiClientID), "tool", strings.TrimSpace(toolName))
	}
	raw := strings.Join(parts, "|")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
func gatewayRateLimitStateKey(actorType, actorID, aiClientID, toolName string, limit gatewayInvocationLimit) string {
	scope := normalizeGatewayLimitScope(limit.Scope)
	if scope == "" {
		scope = "actor_client_tool"
	}
	parts := gatewayLimitKeyParts(limit, scope)
	switch scope {
	case "global":
	case "client":
		parts = append(parts, "client", strings.TrimSpace(aiClientID))
	case "client_tool":
		parts = append(parts, "client", strings.TrimSpace(aiClientID), "tool", strings.TrimSpace(toolName))
	case "actor":
		parts = append(parts, "actor", strings.TrimSpace(actorType), strings.TrimSpace(actorID))
	case "actor_tool":
		parts = append(parts, "actor", strings.TrimSpace(actorType), strings.TrimSpace(actorID), "tool", strings.TrimSpace(toolName))
	case "actor_client":
		parts = append(parts, "actor", strings.TrimSpace(actorType), strings.TrimSpace(actorID), "client", strings.TrimSpace(aiClientID))
	default:
		parts = append(parts, "actor", strings.TrimSpace(actorType), strings.TrimSpace(actorID), "client", strings.TrimSpace(aiClientID), "tool", strings.TrimSpace(toolName))
	}
	raw := strings.Join(parts, "|")
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
func gatewayLimitKeyParts(limit gatewayInvocationLimit, scope string) []string {
	return []string{
		"policy", strings.TrimSpace(limit.PolicyID),
		"kind", strings.ToLower(strings.TrimSpace(limit.Kind)),
		"mode", strings.ToLower(strings.TrimSpace(limit.Mode)),
		"scope", scope,
		"windowDuration", strconv.FormatInt(int64(limit.Window), 10),
		"windowLabel", strings.TrimSpace(limit.WindowLabel),
		"limit", strconv.Itoa(limit.Limit),
		"burst", strconv.Itoa(limit.Burst),
	}
}
func gatewayAuditResultCountsTowardLimits(result string) bool {
	switch strings.ToLower(strings.TrimSpace(result)) {
	case "success", "failure", "failed", "dry_run":
		return true
	default:
		return false
	}
}
func gatewayAuditTokenUsage(item domainaigateway.AuditLog) float64 {
	values := gatewayUsageWithDerivedTotals(gatewayAuditUsageValues(item.Metadata))
	if total, ok := gatewayFirstPositiveFloat(values, "totalTokens", "total_tokens", "tokens", "tokenCount", "billableTokens", "billable_tokens", "billedTokens", "billed_tokens", "usageTokens", "usage_tokens"); ok {
		return total
	}
	return gatewayPositiveFloatSum(values, "inputTokens", "input_tokens", "promptTokens", "prompt_tokens", "outputTokens", "output_tokens", "completionTokens", "completion_tokens")
}
func gatewayAuditCostUsage(item domainaigateway.AuditLog) float64 {
	values := gatewayUsageWithDerivedTotals(gatewayAuditUsageValues(item.Metadata))
	if total, ok := gatewayFirstPositiveFloat(values, "totalCost", "total_cost", "cost", "costUsd", "costUSD", "usd", "estimatedCost", "estimatedCostUsd", "responseCost", "response_cost", "totalCostMicros", "total_cost_micros", "costMicros", "cost_micros"); ok {
		return total
	}
	return gatewayPositiveFloatSum(values, "inputCost", "input_cost", "promptCost", "prompt_cost", "outputCost", "output_cost", "completionCost", "completion_cost")
}
func gatewayAuditUsageValues(metadata map[string]any) map[string]any {
	values := make(map[string]any, len(metadata)+8)
	for key, value := range metadata {
		values[key] = value
	}
	for _, key := range []string{"usage", "tokenUsage", "aiUsage", "providerUsage", "llmUsage", "metering", "billing", "costUsage"} {
		raw, ok := gatewayConditionRaw(metadata, key)
		if !ok {
			continue
		}
		for nestedKey, nestedValue := range mapValue(raw) {
			values[nestedKey] = nestedValue
		}
	}
	return values
}
func gatewayProviderUsageSummary(output any, relatedIDs map[string]any) map[string]any {
	summary := map[string]any{}
	for _, source := range []any{gatewayUsageSerializableValue(output), relatedIDs} {
		for _, usage := range gatewayProviderUsageCandidates(source) {
			gatewayMergeUsageSummary(summary, usage)
		}
	}
	if len(summary) == 0 {
		return nil
	}
	return gatewayNormalizeUsageSummary(summary)
}
func gatewayUsageSerializableValue(value any) any {
	switch value.(type) {
	case nil, map[string]any, []any, []map[string]any:
		return value
	default:
		raw, err := json.Marshal(value)
		if err != nil {
			return value
		}
		var out any
		if err := json.Unmarshal(raw, &out); err != nil {
			return value
		}
		return out
	}
}
func gatewayProviderUsageCandidates(value any) []map[string]any {
	out := make([]map[string]any, 0)
	gatewayCollectProviderUsageCandidates(value, "$", 0, &out)
	return out
}
func gatewayCollectProviderUsageCandidates(value any, key string, depth int, out *[]map[string]any) {
	if out == nil || depth > 5 || value == nil {
		return
	}
	switch typed := value.(type) {
	case map[string]any:
		if context := gatewayUsageDetailContext(key); context != "" {
			if usage := gatewayProviderUsageDetailNumbers(typed, context); len(usage) > 0 {
				*out = append(*out, usage)
				for childKey, child := range typed {
					switch child.(type) {
					case map[string]any, []any, []map[string]any:
						gatewayCollectProviderUsageCandidates(child, childKey, depth+1, out)
					}
				}
				return
			}
		}
		if gatewayUsageContainerKey(key) {
			if usage := gatewayPreferredBilledUsageNumbers(typed); len(usage) > 0 {
				*out = append(*out, usage)
				for childKey, child := range typed {
					switch normalizeGatewayConditionKey(childKey) {
					case "billedunits", "billedunit", "tokens":
						continue
					}
					switch child.(type) {
					case map[string]any, []any, []map[string]any:
						gatewayCollectProviderUsageCandidates(child, childKey, depth+1, out)
					}
				}
				return
			}
			if usage := gatewayUsageNumbersOnly(typed); len(usage) > 0 {
				*out = append(*out, usage)
				for childKey, child := range typed {
					switch child.(type) {
					case map[string]any, []any, []map[string]any:
						gatewayCollectProviderUsageCandidates(child, childKey, depth+1, out)
					}
				}
				return
			}
		}
		if usage := gatewayProviderNativeUsageNumbers(typed); len(usage) > 0 {
			*out = append(*out, usage)
			return
		}
		for childKey, child := range typed {
			gatewayCollectProviderUsageCandidates(child, childKey, depth+1, out)
		}
	case []any:
		for _, item := range typed {
			gatewayCollectProviderUsageCandidates(item, key, depth+1, out)
		}
	case []map[string]any:
		for _, item := range typed {
			gatewayCollectProviderUsageCandidates(item, key, depth+1, out)
		}
	}
}
func gatewayPreferredBilledUsageNumbers(values map[string]any) map[string]any {
	rawBilled, ok := gatewayConditionRaw(values, "billed_units")
	if !ok {
		return nil
	}
	rawTokens, ok := gatewayConditionRaw(values, "tokens")
	if !ok || len(mapValue(rawTokens)) == 0 {
		return nil
	}
	billed := mapValue(rawBilled)
	if len(billed) == 0 {
		return nil
	}
	out := gatewayUsageNumbersOnly(values)
	if out == nil {
		out = map[string]any{}
	}
	gatewayMergeUsageSummary(out, gatewayUsageNumbersOnly(billed))
	if len(out) == 0 {
		return nil
	}
	return gatewayNormalizeUsageSummary(out)
}
func gatewayUsageContainerKey(key string) bool {
	switch normalizeGatewayConditionKey(key) {
	case "usage", "tokenusage", "aiusage", "providerusage", "llmusage", "metering", "billing", "costusage", "usagemetadata", "tokenmetadata", "tokencount", "tokencounts", "tokens", "billedunits", "billedunit":
		return true
	default:
		return false
	}
}
func gatewayUsageDetailContext(key string) string {
	switch normalizeGatewayConditionKey(key) {
	case "prompttokendetails", "prompttokensdetails", "inputtokendetails", "inputtokensdetails", "requesttokendetails", "requesttokensdetails":
		return "input"
	case "completiontokendetails", "completiontokensdetails", "outputtokendetails", "outputtokensdetails", "responsetokendetails", "responsetokensdetails", "candidatestokendetails", "candidatestokensdetails":
		return "output"
	default:
		return ""
	}
}
func gatewayProviderUsageDetailNumbers(values map[string]any, context string) map[string]any {
	out := map[string]any{}
	for key, value := range values {
		number, ok := gatewayPositiveFloat(value)
		if !ok {
			continue
		}
		normalized := normalizeGatewayConditionKey(key)
		switch context {
		case "input":
			switch normalized {
			case "cachedtokens", "cachetokens", "cachecreationtokens", "cachereadtokens", "cachewritetokens", "audiotokens", "texttokens", "imagetokens":
				existing, _ := gatewayPositiveFloat(out["inputTokens"])
				out["inputTokens"] = existing + gatewayNormalizeNativeUsageNumber(key, number)
			}
		case "output":
			switch normalized {
			case "reasoningtokens", "acceptedpredictiontokens", "rejectedpredictiontokens", "audiotokens", "texttokens", "imagetokens":
				existing, _ := gatewayPositiveFloat(out["outputTokens"])
				out["outputTokens"] = existing + gatewayNormalizeNativeUsageNumber(key, number)
			}
		}
	}
	return gatewayNormalizeUsageSummary(out)
}
func gatewayUsageNumbersOnly(values map[string]any) map[string]any {
	out := map[string]any{}
	hasGenericInput := gatewayNativeUsageHasAny(values, "inputTokens", "input_tokens", "inputTokensCount", "input_tokens_count", "inputTokenUsage", "input_token_usage", "promptTokens", "prompt_tokens", "promptTokensCount", "prompt_tokens_count", "promptTokenUsage", "prompt_token_usage", "promptTokenCount", "prompt_token_count", "inputTokenCount", "input_token_count", "promptEvalCount", "prompt_eval_count")
	hasGenericOutput := gatewayNativeUsageHasAny(values, "outputTokens", "output_tokens", "outputTokensCount", "output_tokens_count", "outputTokenUsage", "output_token_usage", "completionTokens", "completion_tokens", "completionTokensCount", "completion_tokens_count", "completionTokenUsage", "completion_token_usage", "candidatesTokenCount", "candidates_token_count", "outputTokenCount", "output_token_count", "evalCount", "eval_count")
	seen := map[string]struct{}{}
	for _, key := range gatewayUsageSummaryKeys() {
		normalized := normalizeGatewayConditionKey(key)
		if _, ok := seen[normalized]; ok {
			continue
		}
		value, ok := gatewayConditionRaw(values, key)
		if !ok {
			continue
		}
		seen[normalized] = struct{}{}
		number, ok := gatewayPositiveFloat(value)
		if !ok {
			continue
		}
		gatewayAddNativeUsageNumber(out, key, number, hasGenericInput, hasGenericOutput)
	}
	if supplemental := gatewaySupplementalInputTokenUsage(values); supplemental > 0 {
		existing, _ := gatewayPositiveFloat(out["inputTokens"])
		out["inputTokens"] = existing + supplemental
	}
	if supplemental := gatewaySupplementalOutputTokenUsage(values); supplemental > 0 {
		existing, _ := gatewayPositiveFloat(out["outputTokens"])
		out["outputTokens"] = existing + supplemental
	}
	return out
}
func gatewayMergeUsageSummary(dst map[string]any, src map[string]any) {
	if dst == nil || len(src) == 0 {
		return
	}
	for key, value := range gatewayUsageWithDerivedTotals(src) {
		number, ok := gatewayPositiveFloat(value)
		if !ok {
			continue
		}
		canonical := gatewayCanonicalUsageKey(key)
		if existing, ok := gatewayPositiveFloat(dst[canonical]); ok {
			dst[canonical] = existing + number
		} else {
			dst[canonical] = number
		}
	}
}
func gatewayProviderNativeUsageNumbers(values map[string]any) map[string]any {
	out := map[string]any{}
	hasGenericInput := gatewayNativeUsageHasAny(values, "inputTokens", "input_tokens", "inputTokensCount", "input_tokens_count", "inputTokenUsage", "input_token_usage", "promptTokens", "prompt_tokens", "promptTokensCount", "prompt_tokens_count", "promptTokenUsage", "prompt_token_usage", "promptTokenCount", "prompt_token_count", "inputTokenCount", "input_token_count", "promptEvalCount", "prompt_eval_count")
	hasGenericOutput := gatewayNativeUsageHasAny(values, "outputTokens", "output_tokens", "outputTokensCount", "output_tokens_count", "outputTokenUsage", "output_token_usage", "completionTokens", "completion_tokens", "completionTokensCount", "completion_tokens_count", "completionTokenUsage", "completion_token_usage", "candidatesTokenCount", "candidates_token_count", "outputTokenCount", "output_token_count", "evalCount", "eval_count")
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
		normalized := normalizeGatewayConditionKey(key)
		if _, ok := seen[normalized]; ok {
			continue
		}
		raw, ok := gatewayConditionRaw(values, key)
		if !ok {
			continue
		}
		seen[normalized] = struct{}{}
		number, ok := gatewayPositiveFloat(raw)
		if !ok {
			continue
		}
		gatewayAddNativeUsageNumber(out, key, number, hasGenericInput, hasGenericOutput)
	}
	if supplemental := gatewaySupplementalInputTokenUsage(values); supplemental > 0 {
		existing, _ := gatewayPositiveFloat(out["inputTokens"])
		out["inputTokens"] = existing + supplemental
	}
	if supplemental := gatewaySupplementalOutputTokenUsage(values); supplemental > 0 {
		existing, _ := gatewayPositiveFloat(out["outputTokens"])
		out["outputTokens"] = existing + supplemental
	}
	costKeys := []string{"responseCost", "response_cost", "totalCostUsd", "total_cost_usd", "totalCostUSD", "total_cost_USD", "estimatedCost", "estimated_cost", "estimatedCostUsd", "estimated_cost_usd", "billedAmount", "billed_amount", "chargeAmount", "charge_amount", "creditsUsed", "credits_used", "costMicros", "cost_micros", "totalCostMicros", "total_cost_micros", "estimatedCostMicros", "estimated_cost_micros", "costCents", "cost_cents", "totalCostCents", "total_cost_cents", "estimatedCostCents", "estimated_cost_cents", "inputCost", "input_cost", "promptCost", "prompt_cost", "inputCostUsd", "input_cost_usd", "promptCostUsd", "prompt_cost_usd", "inputCostMicros", "input_cost_micros", "promptCostMicros", "prompt_cost_micros", "inputCostCents", "input_cost_cents", "promptCostCents", "prompt_cost_cents", "outputCost", "output_cost", "completionCost", "completion_cost", "outputCostUsd", "output_cost_usd", "completionCostUsd", "completion_cost_usd", "outputCostMicros", "output_cost_micros", "completionCostMicros", "completion_cost_micros", "outputCostCents", "output_cost_cents", "completionCostCents", "completion_cost_cents"}
	clear(seen)
	for _, key := range costKeys {
		normalized := normalizeGatewayConditionKey(key)
		if _, ok := seen[normalized]; ok {
			continue
		}
		raw, ok := gatewayConditionRaw(values, key)
		if !ok {
			continue
		}
		seen[normalized] = struct{}{}
		number, ok := gatewayPositiveFloat(raw)
		if !ok {
			continue
		}
		gatewayAddNativeUsageNumber(out, key, number, false, false)
	}
	if len(out) == 0 {
		return nil
	}
	return gatewayNormalizeUsageSummary(out)
}
func gatewayNativeUsageHasAny(values map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := gatewayConditionRaw(values, key); ok {
			return true
		}
	}
	return false
}
func gatewayAddNativeUsageNumber(out map[string]any, key string, number float64, hasGenericInput, hasGenericOutput bool) {
	canonical := gatewayCanonicalUsageKey(key)
	if !gatewayCanonicalUsageSummaryKey(canonical) {
		return
	}
	number = gatewayNormalizeNativeUsageNumber(key, number)
	normalized := normalizeGatewayConditionKey(key)
	if gatewaySupplementalInputTokenKey(normalized) || gatewaySupplementalOutputTokenKey(normalized) {
		return
	}
	additiveInput := !hasGenericInput && (normalized == "inputtexttokens" || normalized == "inputimagetokens" || normalized == "inputaudiotokens" || normalized == "textinputtokens" || normalized == "imageinputtokens" || normalized == "audioinputtokens" || normalized == "imagetokens" || normalized == "videotokens" || normalized == "audiotokens")
	additiveOutput := !hasGenericOutput && (normalized == "outputtexttokens" || normalized == "outputimagetokens" || normalized == "outputaudiotokens" || normalized == "textoutputtokens" || normalized == "imageoutputtokens" || normalized == "audiooutputtokens")
	if additiveInput || additiveOutput {
		existing, _ := gatewayPositiveFloat(out[canonical])
		out[canonical] = existing + number
		return
	}
	if existing, ok := gatewayPositiveFloat(out[canonical]); !ok || number > existing {
		out[canonical] = number
	}
}
func gatewayNormalizeNativeUsageNumber(key string, number float64) float64 {
	switch normalizeGatewayConditionKey(key) {
	case "costmicros", "totalcostmicros", "estimatedcostmicros", "inputcostmicros", "promptcostmicros", "outputcostmicros", "completioncostmicros":
		return number / 1_000_000
	case "costcents", "totalcostcents", "estimatedcostcents", "inputcostcents", "promptcostcents", "outputcostcents", "completioncostcents":
		return number / 100
	default:
		return number
	}
}
func gatewayUsageWithDerivedTotals(values map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range values {
		canonical := gatewayCanonicalUsageKey(key)
		if number, ok := gatewayPositiveFloat(value); ok {
			number = gatewayNormalizeNativeUsageNumber(key, number)
			if existing, ok := gatewayPositiveFloat(out[canonical]); !ok || number > existing {
				out[canonical] = number
			}
			continue
		}
		if _, exists := out[canonical]; !exists {
			out[canonical] = value
		}
	}
	if _, ok := gatewayPositiveFloat(out["totalTokens"]); !ok {
		if total := gatewayPositiveFloatSum(out, "inputTokens", "outputTokens"); total > 0 {
			out["totalTokens"] = total
		}
	}
	if _, ok := gatewayPositiveFloat(out["totalCost"]); !ok {
		if total := gatewayPositiveFloatSum(out, "inputCost", "outputCost"); total > 0 {
			out["totalCost"] = total
		}
	}
	return out
}
func gatewaySupplementalInputTokenUsage(values map[string]any) float64 {
	total := 0.0
	for key, value := range values {
		if gatewaySupplementalInputTokenKey(normalizeGatewayConditionKey(key)) {
			if number, ok := gatewayPositiveFloat(value); ok {
				total += number
			}
		}
	}
	return total
}
func gatewaySupplementalOutputTokenUsage(values map[string]any) float64 {
	total := 0.0
	for key, value := range values {
		if gatewaySupplementalOutputTokenKey(normalizeGatewayConditionKey(key)) {
			if number, ok := gatewayPositiveFloat(value); ok {
				total += number
			}
		}
	}
	return total
}
func gatewaySupplementalInputTokenKey(normalized string) bool {
	switch normalized {
	case "cachedtokens", "cachetokens", "cachecreationinputtokens", "cachereadinputtokens", "cachewriteinputtokens", "cachecreationtokens", "cachereadtokens", "cachewritetokens", "cachedcontenttokencount", "cachedcontenttokens", "tooluseprompttokencount", "tooluseprompttokens", "promptcachereadtokens", "promptcachewritetokens", "promptcachehittokens", "promptcachemisstokens", "inputcachereadtokens", "inputcachewritetokens", "inputcachedtokens":
		return true
	default:
		return false
	}
}
func gatewaySupplementalOutputTokenKey(normalized string) bool {
	switch normalized {
	case "thoughtstokencount", "thoughtstokens", "acceptedpredictiontokens", "rejectedpredictiontokens":
		return true
	default:
		return false
	}
}
func gatewayNormalizeUsageSummary(values map[string]any) map[string]any {
	values = gatewayUsageWithDerivedTotals(values)
	out := map[string]any{}
	for _, key := range gatewayCanonicalUsageSummaryKeys() {
		if value, ok := gatewayPositiveFloat(values[key]); ok {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
func gatewayUsageSummaryKeys() []string {
	return []string{
		"totalTokens", "total_tokens", "tokens", "tokenCount", "totalTokenCount", "total_token_count", "tokenUsage", "token_usage", "billableTokens", "billable_tokens", "billedTokens", "billed_tokens", "usageTokens", "usage_tokens", "totalUnits", "total_units", "usageUnits", "usage_units", "searchUnits", "search_units", "classificationUnits", "classification_units", "classifications", "embeddingTokens", "embedding_tokens", "rerankTokens", "rerank_tokens", "queryUnits", "query_units", "queries", "searchRequests", "search_requests", "searchCredits", "search_credits", "serpapiSearches", "serpapi_searches", "braveSearchUnits", "brave_search_units", "browserMinutes", "browser_minutes", "browserSessions", "browser_sessions", "sessionMinutes", "session_minutes", "browserbaseMinutes", "browserbase_minutes", "pageLoads", "page_loads", "documentPages", "document_pages", "parsePages", "parse_pages", "llamaParsePages", "llama_parse_pages", "documents", "chunks", "characters", "chars", "requestCount", "request_count", "requests", "providerRequests", "provider_requests",
		"inputTokens", "input_tokens", "inputTokensCount", "input_tokens_count", "inputTokenUsage", "input_token_usage", "promptTokens", "prompt_tokens", "promptTokensCount", "prompt_tokens_count", "promptTokenUsage", "prompt_token_usage", "promptTokenCount", "prompt_token_count", "inputTokenCount", "input_token_count", "promptEvalCount", "prompt_eval_count", "cachedContentTokenCount", "cached_content_token_count", "cachedContentTokens", "cached_content_tokens", "toolUsePromptTokenCount", "tool_use_prompt_token_count", "toolUsePromptTokens", "tool_use_prompt_tokens", "inputTextTokens", "input_text_tokens", "textInputTokens", "text_input_tokens", "inputImageTokens", "input_image_tokens", "imageInputTokens", "image_input_tokens", "imageTokens", "image_tokens", "videoTokens", "video_tokens", "inputAudioTokens", "input_audio_tokens", "audioInputTokens", "audio_input_tokens", "audioTokens", "audio_tokens", "readUnits", "read_units", "inputUnits", "input_units", "requestUnits", "request_units", "promptCacheReadTokens", "prompt_cache_read_tokens", "promptCacheWriteTokens", "prompt_cache_write_tokens", "promptCacheHitTokens", "prompt_cache_hit_tokens", "promptCacheMissTokens", "prompt_cache_miss_tokens", "inputCacheReadTokens", "input_cache_read_tokens", "inputCacheWriteTokens", "input_cache_write_tokens", "inputCachedTokens", "input_cached_tokens",
		"outputTokens", "output_tokens", "outputTokensCount", "output_tokens_count", "outputTokenUsage", "output_token_usage", "completionTokens", "completion_tokens", "completionTokensCount", "completion_tokens_count", "completionTokenUsage", "completion_token_usage", "candidatesTokenCount", "candidates_token_count", "outputTokenCount", "output_token_count", "evalCount", "eval_count", "outputTextTokens", "output_text_tokens", "textOutputTokens", "text_output_tokens", "outputImageTokens", "output_image_tokens", "imageOutputTokens", "image_output_tokens", "outputAudioTokens", "output_audio_tokens", "audioOutputTokens", "audio_output_tokens", "thoughtsTokenCount", "thoughts_token_count", "thoughtsTokens", "thoughts_tokens", "reasoningTokens", "reasoning_tokens", "completionReasoningTokens", "completion_reasoning_tokens", "outputReasoningTokens", "output_reasoning_tokens", "acceptedPredictionTokens", "accepted_prediction_tokens", "rejectedPredictionTokens", "rejected_prediction_tokens", "writeUnits", "write_units", "outputUnits", "output_units", "responseUnits", "response_units",
		"totalCost", "total_cost", "cost", "costUsd", "costUSD", "usd", "estimatedCost", "estimated_cost", "estimatedCostUsd", "estimated_cost_usd", "responseCost", "response_cost", "totalCostUsd", "total_cost_usd", "totalCostUSD", "total_cost_USD", "billedAmount", "billed_amount", "chargeAmount", "charge_amount", "creditsUsed", "credits_used", "costMicros", "cost_micros", "totalCostMicros", "total_cost_micros", "estimatedCostMicros", "estimated_cost_micros", "costCents", "cost_cents", "totalCostCents", "total_cost_cents", "estimatedCostCents", "estimated_cost_cents",
		"inputCost", "input_cost", "promptCost", "prompt_cost", "inputCostUsd", "input_cost_usd", "promptCostUsd", "prompt_cost_usd", "inputCostMicros", "input_cost_micros", "promptCostMicros", "prompt_cost_micros", "inputCostCents", "input_cost_cents", "promptCostCents", "prompt_cost_cents",
		"outputCost", "output_cost", "completionCost", "completion_cost", "outputCostUsd", "output_cost_usd", "completionCostUsd", "completion_cost_usd", "outputCostMicros", "output_cost_micros", "completionCostMicros", "completion_cost_micros", "outputCostCents", "output_cost_cents", "completionCostCents", "completion_cost_cents",
	}
}
func gatewayCanonicalUsageSummaryKeys() []string {
	return []string{"totalTokens", "inputTokens", "outputTokens", "totalCost", "inputCost", "outputCost"}
}
func gatewayCanonicalUsageSummaryKey(key string) bool {
	switch key {
	case "totalTokens", "inputTokens", "outputTokens", "totalCost", "inputCost", "outputCost":
		return true
	default:
		return false
	}
}
func gatewayCanonicalUsageKey(key string) string {
	switch normalizeGatewayConditionKey(key) {
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
func formatGatewayBudgetValue(value float64) string {
	if value == float64(int64(value)) {
		return strconv.FormatInt(int64(value), 10)
	}
	return strconv.FormatFloat(value, 'f', 4, 64)
}
