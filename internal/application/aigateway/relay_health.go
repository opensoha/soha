package aigateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func (s *Service) RunLLMRelayHealthChecks(ctx context.Context, principal domainidentity.Principal) (domainaigateway.LLMRelayHealthCheckRun, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayRelayManage); err != nil {
		return domainaigateway.LLMRelayHealthCheckRun{}, err
	}
	return s.runLLMRelayHealthChecks(ctx, principal)
}

func (s *Service) StartRelayHealthChecks(ctx context.Context) {
	if s == nil || !s.relayConfig.Enabled || !s.relayConfig.HealthCheckEnabled || s.llmRelayRepository() == nil {
		return
	}
	interval := s.relayConfig.HealthCheckInterval
	if interval <= 0 {
		interval = time.Minute
	}
	s.relayHealthOnce.Do(func() {
		go func() {
			s.runScheduledRelayHealthChecks(ctx)
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					s.runScheduledRelayHealthChecks(ctx)
				}
			}
		}()
	})
}

func (s *Service) runScheduledRelayHealthChecks(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	_, _ = s.runLLMRelayHealthChecks(ctx, relayHealthCheckPrincipal())
}

func relayHealthCheckPrincipal() domainidentity.Principal {
	return domainidentity.Principal{
		UserID:   "system:ai-gateway-relay-health-check",
		UserName: "AI Gateway Relay Health Check",
		Roles:    []string{"system"},
	}
}

func (s *Service) runLLMRelayHealthChecks(ctx context.Context, principal domainidentity.Principal) (domainaigateway.LLMRelayHealthCheckRun, error) {
	repo := s.llmRelayRepository()
	if repo == nil {
		return domainaigateway.LLMRelayHealthCheckRun{}, fmt.Errorf("%w: AI Gateway relay repository is not configured", apperrors.ErrInvalidArgument)
	}
	run := domainaigateway.LLMRelayHealthCheckRun{CheckedAt: time.Now().UTC()}
	upstreams, err := repo.ListLLMUpstreams(ctx, domainaigateway.LLMUpstreamFilter{IncludeAll: true})
	if err != nil {
		return run, err
	}
	run.Total = len(upstreams)
	for _, upstream := range upstreams {
		if ctx.Err() != nil {
			return run, ctx.Err()
		}
		policy := relayHealthCheckPolicyForUpstream(upstream)
		if !policy.enabled || strings.EqualFold(strings.TrimSpace(upstream.Status), "disabled") {
			run.Skipped++
			continue
		}
		result, checkErr := s.testRelayUpstream(ctx, upstream)
		if checkErr != nil {
			result = relayHealthCheckFailedResult(upstream, checkErr)
		}
		run.Checked++
		run.Results = append(run.Results, result)
		transition, err := s.updateRelayHealthCheckState(ctx, principal, upstream, result, checkErr, policy)
		if err != nil {
			return run, err
		}
		if relayHealthCheckSucceeded(result, checkErr) {
			run.Healthy++
			if transition == "recovered" {
				run.Recovered++
			}
			continue
		}
		run.Failed++
		if transition == "degraded" {
			run.Degraded++
		}
	}
	return run, nil
}

type relayHealthCheckPolicy struct {
	enabled               bool
	degradeAfterFailures  int
	recoverAfterSuccesses int
}

func relayHealthCheckPolicyForUpstream(upstream domainaigateway.LLMUpstream) relayHealthCheckPolicy {
	policy := relayHealthCheckPolicy{enabled: true, degradeAfterFailures: 1, recoverAfterSuccesses: 1}
	values := gatewayConditionValues(upstream.Metadata, "healthCheck", "health_check", "health")
	if raw, ok := gatewayConditionRaw(values, "enabled"); ok {
		policy.enabled = boolFromAny(raw)
	}
	if value, ok := gatewayFirstPositiveInt(values, "degradeAfterFailures", "degrade_after_failures", "failureThreshold", "failure_threshold"); ok {
		policy.degradeAfterFailures = value
	}
	if value, ok := gatewayFirstPositiveInt(values, "recoverAfterSuccesses", "recover_after_successes", "successThreshold", "success_threshold"); ok {
		policy.recoverAfterSuccesses = value
	}
	return policy
}

func relayHealthCheckFailedResult(upstream domainaigateway.LLMUpstream, err error) domainaigateway.LLMUpstreamTestResult {
	return domainaigateway.LLMUpstreamTestResult{
		UpstreamID:   upstream.ID,
		ProviderKind: normalizeRelayProviderKind(upstream.ProviderKind),
		Status:       "failure",
		CheckedAt:    time.Now().UTC(),
	}
}

func relayHealthCheckSucceeded(result domainaigateway.LLMUpstreamTestResult, err error) bool {
	return err == nil && strings.EqualFold(strings.TrimSpace(result.Status), "success") && result.HTTPStatus < http.StatusBadRequest
}

func (s *Service) updateRelayHealthCheckState(ctx context.Context, principal domainidentity.Principal, upstream domainaigateway.LLMUpstream, result domainaigateway.LLMUpstreamTestResult, checkErr error, policy relayHealthCheckPolicy) (string, error) {
	repo := s.llmRelayRepository()
	if repo == nil {
		return "", nil
	}
	now := time.Now().UTC()
	latest, err := repo.GetLLMUpstream(ctx, upstream.ID)
	if err == nil {
		upstream = latest
	}
	health := copyMap(upstream.Health)
	transition := ""
	success := relayHealthCheckSucceeded(result, checkErr)
	health["lastHealthCheckAt"] = result.CheckedAt.UTC().Format(time.RFC3339Nano)
	health["lastHealthStatus"] = result.Status
	health["lastHealthHTTPStatus"] = result.HTTPStatus
	health["lastHealthLatencyMs"] = result.DurationMs
	health["healthCheckUpdatedBy"] = "relay_health_check"
	if success {
		failures := 0
		successes := intFromAny(health["healthCheckConsecutiveSuccesses"]) + 1
		health["healthCheckConsecutiveFailures"] = failures
		health["healthCheckConsecutiveSuccesses"] = successes
		delete(health, "lastHealthErrorCode")
		delete(health, "lastHealthErrorMessage")
		if strings.EqualFold(strings.TrimSpace(upstream.Status), "degraded") && relayHealthCheckManagedDegraded(health) && successes >= policy.recoverAfterSuccesses {
			upstream.Status = "active"
			delete(health, "degradedBy")
			delete(health, "degradedAt")
			transition = "recovered"
		}
	} else {
		failures := intFromAny(health["healthCheckConsecutiveFailures"]) + 1
		health["healthCheckConsecutiveFailures"] = failures
		health["healthCheckConsecutiveSuccesses"] = 0
		health["lastHealthErrorCode"] = relayHealthCheckErrorCode(result, checkErr)
		health["lastHealthErrorMessage"] = relayHealthCheckErrorMessage(checkErr)
		if strings.EqualFold(strings.TrimSpace(upstream.Status), "active") && failures >= policy.degradeAfterFailures {
			upstream.Status = "degraded"
			health["degradedBy"] = "relay_health_check"
			health["degradedAt"] = now.Format(time.RFC3339Nano)
			transition = "degraded"
		}
	}
	upstream.Health = health
	upstream.UpdatedAt = now
	if _, err := repo.UpdateLLMUpstream(ctx, upstream); err != nil {
		return "", err
	}
	if transition != "" {
		s.recordRelayHealthCheckTransition(ctx, principal, upstream, result, transition)
	}
	return transition, nil
}

func relayHealthCheckManagedDegraded(health map[string]any) bool {
	return strings.EqualFold(strings.TrimSpace(fmt.Sprint(health["degradedBy"])), "relay_health_check")
}

func relayHealthCheckErrorCode(result domainaigateway.LLMUpstreamTestResult, err error) string {
	if result.HTTPStatus > 0 {
		if code := relayErrorCodeForStatus(result.HTTPStatus); code != "" {
			return code
		}
		return "upstream_unhealthy"
	}
	if errors.Is(err, apperrors.ErrClusterUnready) {
		return "upstream_unreachable"
	}
	if err != nil {
		return "health_check_failed"
	}
	return ""
}

func relayHealthCheckErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return redactRelayText(err.Error())
}

func (s *Service) recordRelayHealthCheckTransition(ctx context.Context, principal domainidentity.Principal, upstream domainaigateway.LLMUpstream, result domainaigateway.LLMUpstreamTestResult, transition string) {
	repo := s.llmRelayRepository()
	if repo == nil {
		return
	}
	action := "ai_gateway.relay.upstream.health_degraded"
	status := "failure"
	message := "relay upstream health check degraded upstream"
	if transition == "recovered" {
		action = "ai_gateway.relay.upstream.health_recovered"
		status = "success"
		message = "relay upstream health check recovered upstream"
	}
	metadata := map[string]any{
		"upstreamId":       upstream.ID,
		"providerKind":     upstream.ProviderKind,
		"httpStatus":       result.HTTPStatus,
		"latencyMs":        result.DurationMs,
		"lastHealthStatus": result.Status,
	}
	errorCode := strings.TrimSpace(fmt.Sprint(upstream.Health["lastHealthErrorCode"]))
	errorMessage := strings.TrimSpace(fmt.Sprint(upstream.Health["lastHealthErrorMessage"]))
	if errorCode != "" {
		metadata["errorCode"] = errorCode
	}
	if errorMessage != "" {
		metadata["errorMessage"] = errorMessage
	}
	_ = repo.CreateLLMHealthEvent(ctx, domainaigateway.LLMHealthEvent{
		ID:                  uuid.NewString(),
		UpstreamID:          upstream.ID,
		UpstreamName:        upstream.Name,
		ProviderKind:        upstream.ProviderKind,
		EventType:           action,
		Status:              status,
		HTTPStatus:          result.HTTPStatus,
		LatencyMilliseconds: result.DurationMs,
		ErrorCode:           errorCode,
		ErrorMessage:        errorMessage,
		Message:             message,
		Metadata:            copyMap(metadata),
		CreatedAt:           time.Now().UTC(),
	})
	_ = s.recordRelayAudit(ctx, principal, action, status, message, metadata)
}

func relayHealthStatus(values map[string]any) string {
	for _, key := range []string{"circuitState", "circuit_state", "circuitBreaker", "circuit_breaker"} {
		if value, ok := values[key]; ok {
			text := strings.ToLower(strings.TrimSpace(fmt.Sprint(value)))
			if text == "open" || text == "circuit_open" {
				return "open"
			}
		}
	}
	return ""
}

func relayHealthUntil(values map[string]any, now time.Time) (time.Time, bool) {
	for _, key := range []string{"circuitOpenUntil", "circuit_open_until", "openUntil", "open_until", "retryAfter", "retry_after", "degradedUntil", "degraded_until"} {
		raw, ok := values[key]
		if !ok {
			continue
		}
		if parsed, ok := relayTimeFromAny(raw, now); ok {
			return parsed, true
		}
	}
	return time.Time{}, false
}

func relayTimeFromAny(value any, now time.Time) (time.Time, bool) {
	switch typed := value.(type) {
	case time.Time:
		return typed, true
	case string:
		text := strings.TrimSpace(typed)
		if text == "" {
			return time.Time{}, false
		}
		for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
			parsed, err := time.Parse(layout, text)
			if err == nil {
				return parsed.UTC(), true
			}
		}
	case int:
		return now.Add(time.Duration(typed) * time.Second), true
	case int64:
		return now.Add(time.Duration(typed) * time.Second), true
	case float64:
		return now.Add(time.Duration(typed) * time.Second), true
	case json.Number:
		seconds, err := typed.Int64()
		if err == nil {
			return now.Add(time.Duration(seconds) * time.Second), true
		}
	}
	return time.Time{}, false
}

func (s *Service) recordRelayUpstreamSuccess(ctx context.Context, principal domainidentity.Principal, selection relaySelection) {
	_ = s.updateRelayUpstreamHealth(ctx, principal, selection.upstream, func(upstream domainaigateway.LLMUpstream, health map[string]any, now time.Time) (domainaigateway.LLMUpstream, bool, string, map[string]any) {
		wasOpen := relayHealthStatus(upstream.Health) == "open" || relayHealthStatus(upstream.Metadata) == "open" || strings.EqualFold(strings.TrimSpace(fmt.Sprint(health["circuitState"])), "half_open")
		hadFailureState := intFromAny(health["consecutiveFailures"]) > 0 || strings.TrimSpace(fmt.Sprint(health["lastFailureAt"])) != "" || strings.TrimSpace(fmt.Sprint(health["lastErrorCode"])) != ""
		if !wasOpen && !hadFailureState {
			return upstream, false, "", nil
		}
		delete(health, "circuitState")
		delete(health, "circuitOpenUntil")
		delete(health, "retryAfter")
		delete(health, "degradedUntil")
		delete(health, "lastFailureAt")
		delete(health, "lastErrorCode")
		health["consecutiveFailures"] = 0
		health["lastSuccessAt"] = now.Format(time.RFC3339Nano)
		health["updatedBy"] = "relay_runtime"
		upstream.Health = health
		if !wasOpen {
			return upstream, true, "", nil
		}
		return upstream, true, "ai_gateway.relay.upstream.circuit_recovered", map[string]any{
			"upstreamId":   upstream.ID,
			"providerKind": upstream.ProviderKind,
		}
	})
}

func (s *Service) recordRelayUpstreamFailure(ctx context.Context, principal domainidentity.Principal, selection relaySelection, errorCode string) {
	_ = s.updateRelayUpstreamHealth(ctx, principal, selection.upstream, func(upstream domainaigateway.LLMUpstream, health map[string]any, now time.Time) (domainaigateway.LLMUpstream, bool, string, map[string]any) {
		policy := relayCircuitPolicy(upstream)
		failures := intFromAny(health["consecutiveFailures"]) + 1
		health["consecutiveFailures"] = failures
		health["lastFailureAt"] = now.Format(time.RFC3339Nano)
		health["lastErrorCode"] = strings.TrimSpace(errorCode)
		health["updatedBy"] = "relay_runtime"
		if failures < policy.failureThreshold {
			health["circuitState"] = "closed"
			upstream.Health = health
			return upstream, true, "", nil
		}
		openUntil := now.Add(policy.openDuration)
		health["circuitState"] = "open"
		health["circuitOpenUntil"] = openUntil.Format(time.RFC3339Nano)
		health["retryAfter"] = int(policy.openDuration.Seconds())
		upstream.Health = health
		return upstream, true, "ai_gateway.relay.upstream.circuit_open", map[string]any{
			"upstreamId":          upstream.ID,
			"providerKind":        upstream.ProviderKind,
			"errorCode":           strings.TrimSpace(errorCode),
			"consecutiveFailures": failures,
			"failureThreshold":    policy.failureThreshold,
			"circuitOpenUntil":    openUntil.Format(time.RFC3339Nano),
			"retryAfterSeconds":   int(policy.openDuration.Seconds()),
		}
	})
}

type relayCircuitBreakerPolicy struct {
	failureThreshold int
	openDuration     time.Duration
}

func relayCircuitPolicy(upstream domainaigateway.LLMUpstream) relayCircuitBreakerPolicy {
	values := gatewayConditionValues(upstream.Metadata, "circuitBreaker", "circuit_breaker", "breaker", "circuit")
	threshold, ok := gatewayFirstPositiveInt(values, "failureThreshold", "failure_threshold", "failures", "consecutiveFailures", "maxFailures")
	if !ok {
		threshold = 3
	}
	if threshold < 1 {
		threshold = 1
	}
	duration, _ := gatewayConditionWindow(values, 30*time.Second, "30s")
	if seconds, ok := gatewayFirstPositiveInt(values, "openSeconds", "open_seconds", "retryAfterSeconds", "retry_after_seconds", "cooldownSeconds", "cooldown_seconds"); ok {
		duration = time.Duration(seconds) * time.Second
	}
	if duration <= 0 {
		duration = 30 * time.Second
	}
	return relayCircuitBreakerPolicy{failureThreshold: threshold, openDuration: duration}
}

func (s *Service) updateRelayUpstreamHealth(ctx context.Context, principal domainidentity.Principal, fallback domainaigateway.LLMUpstream, mutate func(domainaigateway.LLMUpstream, map[string]any, time.Time) (domainaigateway.LLMUpstream, bool, string, map[string]any)) error {
	repo := s.llmRelayRepository()
	if repo == nil || strings.TrimSpace(fallback.ID) == "" {
		return nil
	}
	upstream, err := repo.GetLLMUpstream(ctx, fallback.ID)
	if err != nil {
		upstream = fallback
	}
	now := time.Now().UTC()
	health := copyMap(upstream.Health)
	next, changed, auditAction, auditMetadata := mutate(upstream, health, now)
	if !changed {
		return nil
	}
	next.UpdatedAt = now
	if _, err := repo.UpdateLLMUpstream(ctx, next); err != nil {
		return err
	}
	if auditAction != "" {
		result := "failure"
		if strings.Contains(auditAction, "recovered") {
			result = "success"
		}
		_ = repo.CreateLLMHealthEvent(ctx, domainaigateway.LLMHealthEvent{
			ID:           uuid.NewString(),
			UpstreamID:   next.ID,
			UpstreamName: next.Name,
			ProviderKind: next.ProviderKind,
			EventType:    auditAction,
			Status:       result,
			ErrorCode:    strings.TrimSpace(fmt.Sprint(auditMetadata["errorCode"])),
			Message:      "relay upstream circuit breaker state changed",
			Metadata:     copyMap(auditMetadata),
			CreatedAt:    now,
		})
		_ = s.recordRelayAudit(ctx, principal, auditAction, result, "updated AI Gateway relay upstream circuit breaker state", auditMetadata)
	}
	return nil
}

func relayUpstreamCircuitOpen(upstream domainaigateway.LLMUpstream, now time.Time) bool {
	healthOpen := relayHealthStatus(upstream.Health) == "open"
	metadataOpen := relayHealthStatus(upstream.Metadata) == "open"
	if healthOpen || metadataOpen {
		if until, ok := relayHealthUntil(upstream.Health, now); ok {
			return until.After(now)
		}
		if until, ok := relayHealthUntil(upstream.Metadata, now); ok {
			return until.After(now)
		}
		return true
	}
	if until, ok := relayHealthUntil(upstream.Health, now); ok {
		return until.After(now)
	}
	if until, ok := relayHealthUntil(upstream.Metadata, now); ok {
		return until.After(now)
	}
	return false
}
