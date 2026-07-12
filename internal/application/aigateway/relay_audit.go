package aigateway

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
)

func relayCallLogTotalTokens(log domainaigateway.LLMCallLog) int {
	if log.TotalTokens > 0 {
		return log.TotalTokens
	}
	total := 0
	if log.PromptTokens > 0 {
		total += log.PromptTokens
	}
	if log.CompletionTokens > 0 {
		total += log.CompletionTokens
	}
	if total == 0 && log.ReasoningTokens > 0 {
		total = log.ReasoningTokens
	}
	return total
}

func relayMetricsFromLogs(logs []domainaigateway.LLMCallLog, upstreams []domainaigateway.LLMUpstream, generatedAt time.Time) domainaigateway.LLMRelayMetrics {
	out := domainaigateway.LLMRelayMetrics{GeneratedAt: generatedAt}
	modelCounts := make(map[string]int)
	upstreamHealth := make(map[string]int)
	var ttfbTotal, ttftTotal, durationTotal int64
	var ttfbCount, ttftCount, durationCount int
	var totalDurationMs int64
	for _, upstream := range upstreams {
		status := strings.TrimSpace(upstream.Status)
		if status == "" {
			status = "unknown"
		}
		upstreamHealth[status]++
	}
	for _, item := range logs {
		out.TotalCalls++
		if strings.EqualFold(item.Status, "success") {
			out.SuccessCount++
		} else {
			out.FailureCount++
			if len(out.RecentErrors) < 5 {
				out.RecentErrors = append(out.RecentErrors, item)
			}
		}
		if item.PublicModel != "" {
			modelCounts[item.PublicModel]++
		}
		if item.TTFBMilliseconds > 0 {
			ttfbTotal += item.TTFBMilliseconds
			ttfbCount++
		}
		if item.TTFTMilliseconds > 0 {
			ttftTotal += item.TTFTMilliseconds
			ttftCount++
		}
		if item.DurationMilliseconds > 0 {
			durationTotal += item.DurationMilliseconds
			totalDurationMs += item.DurationMilliseconds
			durationCount++
		}
		if strings.EqualFold(item.CacheStatus, "hit") {
			out.CacheHitCount++
		}
		out.CacheReadTokens += item.CachedReadTokens
		out.CacheWriteTokens += item.CachedWriteTokens
	}
	out.RequestsToday = out.TotalCalls
	if out.TotalCalls > 0 {
		out.SuccessRate = float64(out.SuccessCount) / float64(out.TotalCalls)
	}
	out.AverageTTFBMs = relayAverageMilliseconds(ttfbTotal, ttfbCount)
	out.AverageTTFTMs = relayAverageMilliseconds(ttftTotal, ttftCount)
	out.AverageDurationMs = relayAverageMilliseconds(durationTotal, durationCount)
	if totalDurationMs > 0 {
		totalTokens := 0
		for _, item := range logs {
			totalTokens += item.TotalTokens
		}
		out.TokensPerSecond = float64(totalTokens) / (float64(totalDurationMs) / 1000)
	}
	out.ModelRanking = topGovernanceCounts(modelCounts, 10)
	out.TopModels = out.ModelRanking
	out.UpstreamHealth = topGovernanceCounts(upstreamHealth, 10)
	return out
}

func relayMetricsFromCallLogMetrics(logMetrics domainaigateway.LLMRelayCallLogMetrics, upstreams []domainaigateway.LLMUpstream, generatedAt time.Time) domainaigateway.LLMRelayMetrics {
	upstreamHealth := make(map[string]int)
	for _, upstream := range upstreams {
		status := strings.TrimSpace(upstream.Status)
		if status == "" {
			status = "unknown"
		}
		upstreamHealth[status]++
	}
	out := domainaigateway.LLMRelayMetrics{
		RequestsToday:     logMetrics.TotalCalls,
		TotalCalls:        logMetrics.TotalCalls,
		SuccessCount:      logMetrics.SuccessCount,
		FailureCount:      logMetrics.FailureCount,
		AverageTTFBMs:     logMetrics.AverageTTFBMs,
		AverageTTFTMs:     logMetrics.AverageTTFTMs,
		AverageDurationMs: logMetrics.AverageDurationMs,
		TokensPerSecond:   logMetrics.TokensPerSecond,
		CacheHitCount:     logMetrics.CacheHitCount,
		CacheReadTokens:   logMetrics.CacheReadTokens,
		CacheWriteTokens:  logMetrics.CacheWriteTokens,
		ModelRanking:      append([]domainaigateway.GovernanceMetricCount(nil), logMetrics.ModelRanking...),
		RecentErrors:      append([]domainaigateway.LLMCallLog(nil), logMetrics.RecentErrors...),
		GeneratedAt:       generatedAt,
	}
	if out.TotalCalls > 0 {
		out.SuccessRate = float64(out.SuccessCount) / float64(out.TotalCalls)
	}
	out.TopModels = out.ModelRanking
	out.UpstreamHealth = topGovernanceCounts(upstreamHealth, 10)
	return out
}

func relayAverageMilliseconds(total int64, count int) float64 {
	if count <= 0 {
		return 0
	}
	return float64(total) / float64(count)
}

func relayCallLogFromResponse(req LLMRelayHTTPRequest, resp *http.Response, started, upstreamStarted, firstByteAt time.Time, output []byte, inputBytes int64, status, errorCode, errorMessage, cacheStatus string) domainaigateway.LLMCallLog {
	usage, estimatedTokens := (relayUsageAnalyzer{}).analyze(req, output, resp.StatusCode, status)
	if strings.TrimSpace(cacheStatus) == "" {
		cacheStatus = relayCacheBypass
	}
	now := time.Now().UTC()
	item := domainaigateway.LLMCallLog{
		Status:               status,
		HTTPStatus:           resp.StatusCode,
		UpstreamStatus:       resp.StatusCode,
		ErrorCode:            errorCode,
		ErrorMessage:         redactRelayText(errorMessage),
		PromptTokens:         usage.promptTokens,
		CompletionTokens:     usage.completionTokens,
		TotalTokens:          usage.totalTokens,
		ReasoningTokens:      usage.reasoningTokens,
		CachedReadTokens:     usage.cachedReadTokens,
		CachedWriteTokens:    usage.cachedWriteTokens,
		EstimatedTokens:      estimatedTokens,
		DurationMilliseconds: now.Sub(started).Milliseconds(),
		InputBytes:           inputBytes,
		OutputBytes:          int64(len(output)),
		CacheStatus:          cacheStatus,
		CreatedAt:            started,
	}
	if !firstByteAt.IsZero() {
		item.TTFBMilliseconds = firstByteAt.Sub(started).Milliseconds()
		item.TTFTMilliseconds = item.TTFBMilliseconds
	}
	_ = upstreamStarted
	return item
}

func relayDurationMilliseconds(started, at time.Time) int64 {
	if started.IsZero() || at.IsZero() {
		return 0
	}
	return at.Sub(started).Milliseconds()
}

func redactRelayText(value string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	return gatewaySensitiveValuePattern.ReplaceAllString(value, "$1$2[REDACTED]")
}

func relayStatusFromError(ctx context.Context, err error) string {
	if ctx.Err() != nil {
		return "client_cancelled"
	}
	return "failure"
}

func (s *Service) recordRelayCall(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selection relaySelection, publicModel string, stream bool, item domainaigateway.LLMCallLog) {
	repo := s.llmRelayRepository()
	if repo == nil {
		return
	}
	if item.ID == "" {
		item.ID = uuid.NewString()
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	item.RequestID = req.RequestID
	item.ActorType = accessCtx.SubjectType
	item.ActorID = accessCtx.SubjectID
	item.ActorName = principal.UserName
	item.TokenID = accessCtx.TokenID
	item.TokenPrefix = accessCtx.TokenPrefix
	item.TokenKind = accessCtx.TokenKind
	item.PublicModel = publicModel
	item.UpstreamID = selection.upstream.ID
	item.UpstreamName = selection.upstream.Name
	item.ProviderKind = normalizeRelayProviderKind(req.ProviderKind)
	item.UpstreamModel = selection.route.UpstreamModel
	item.Endpoint = req.Endpoint
	item.Stream = stream
	item.SourceIP = req.SourceIP
	item.UserAgent = req.UserAgent
	item.RouteTrace = map[string]any{
		"routeId":    selection.route.ID,
		"upstreamId": selection.upstream.ID,
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	for _, key := range []string{"source", "sessionId", "agentRunId", "analysisRunId", "workbenchMode"} {
		if value := strings.TrimSpace(fmt.Sprint(accessCtx.Metadata[key])); value != "" && value != "<nil>" {
			item.Metadata[key] = value
		}
	}
	if boolFromAny(accessCtx.Metadata["internal"]) {
		item.Metadata["internal"] = true
	}
	item.Metadata["upstreamProviderKind"] = selection.upstreamProviderKind()
	plan := relayTransformPlanForRoute(selection.route, req.ProviderKind)
	if plan.enabled {
		direction := plan.requestProvider + "_to_" + plan.upstreamProvider
		item.Metadata["transform"] = map[string]any{
			"direction":        direction,
			"requestProvider":  plan.requestProvider,
			"requestEndpoint":  plan.requestEndpoint,
			"upstreamProvider": plan.upstreamProvider,
			"upstreamEndpoint": plan.upstreamEndpoint,
			"mode":             "text_non_stream",
		}
		item.RouteTrace["transform"] = direction
		item.RouteTrace["upstreamProviderKind"] = plan.upstreamProvider
		item.RouteTrace["upstreamEndpoint"] = plan.upstreamEndpoint
	}
	logCtx := ctx
	var cancel context.CancelFunc
	if ctx.Err() != nil {
		logCtx, cancel = context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
	}
	_ = repo.CreateLLMCallLog(logCtx, item)
}

func (s *Service) recordRelayAudit(ctx context.Context, principal domainidentity.Principal, action, result, summary string, metadata map[string]any) error {
	if s.auditLogRepository() == nil {
		return nil
	}
	return s.recordTokenAudit(ctx, principal, action, result, summary, metadata)
}
