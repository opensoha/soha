package aigateway

import (
	"bytes"
	"context"
	"crypto/hmac"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"net/netip"
	"net/textproto"
	"net/url"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

const (
	relayCacheBypass       = "bypass"
	relayCacheHit          = "hit"
	relayCacheMiss         = "miss"
	relayCacheWrite        = "write"
	relayCacheWriteSkipped = "write_skipped"
)

const (
	relayHeaderUpstreamID = "X-Soha-Upstream-ID"
	relayHeaderRouteTrace = "X-Soha-Route-Trace"
	relayHeaderCacheMode  = "X-Soha-Cache-Mode"
)

var relayRandomIntn = func(n int) int {
	if n <= 0 {
		return 0
	}
	value, err := cryptorand.Int(cryptorand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return int(time.Now().UnixNano() % int64(n))
	}
	return int(value.Int64())
}

type LLMRelayHTTPRequest struct {
	ProviderKind string
	Endpoint     string
	PathModel    string
	QueryModel   string
	Method       string
	Headers      http.Header
	Body         []byte
	RequestID    string
	SourceIP     string
	UserAgent    string
}

func (s *Service) ListLLMUpstreams(ctx context.Context, principal domainidentity.Principal, filter domainaigateway.LLMUpstreamFilter) ([]domainaigateway.LLMUpstream, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayRelayView); err != nil {
		return nil, err
	}
	repo := s.llmRelayRepository()
	if repo == nil {
		return nil, fmt.Errorf("%w: AI Gateway relay repository is not configured", apperrors.ErrInvalidArgument)
	}
	return repo.ListLLMUpstreams(ctx, filter)
}

func (s *Service) CreateLLMUpstream(ctx context.Context, principal domainidentity.Principal, input domainaigateway.LLMUpstreamInput) (domainaigateway.LLMUpstream, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayRelayManage); err != nil {
		return domainaigateway.LLMUpstream{}, err
	}
	repo := s.llmRelayRepository()
	if repo == nil {
		return domainaigateway.LLMUpstream{}, fmt.Errorf("%w: AI Gateway relay repository is not configured", apperrors.ErrInvalidArgument)
	}
	item, err := s.normalizeLLMUpstreamInput(ctx, principal, "", input)
	if err != nil {
		return domainaigateway.LLMUpstream{}, err
	}
	created, err := repo.CreateLLMUpstream(ctx, item)
	if err != nil {
		return domainaigateway.LLMUpstream{}, err
	}
	_ = s.recordRelayAudit(ctx, principal, "ai_gateway.relay.upstream.create", "success", "created AI Gateway relay upstream", map[string]any{
		"upstreamId":   created.ID,
		"providerKind": created.ProviderKind,
		"apiKeyPrefix": created.APIKeyPrefix,
	})
	return created, nil
}

func (s *Service) UpdateLLMUpstream(ctx context.Context, principal domainidentity.Principal, upstreamID string, input domainaigateway.LLMUpstreamInput) (domainaigateway.LLMUpstream, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayRelayManage); err != nil {
		return domainaigateway.LLMUpstream{}, err
	}
	repo := s.llmRelayRepository()
	if repo == nil {
		return domainaigateway.LLMUpstream{}, fmt.Errorf("%w: AI Gateway relay repository is not configured", apperrors.ErrInvalidArgument)
	}
	upstreamID = strings.TrimSpace(upstreamID)
	if upstreamID == "" {
		return domainaigateway.LLMUpstream{}, fmt.Errorf("%w: upstream ID is required", apperrors.ErrInvalidArgument)
	}
	item, err := s.normalizeLLMUpstreamInput(ctx, principal, upstreamID, input)
	if err != nil {
		return domainaigateway.LLMUpstream{}, err
	}
	if strings.TrimSpace(input.APIKey) == "" {
		existing, err := repo.GetLLMUpstream(ctx, upstreamID)
		if err != nil {
			return domainaigateway.LLMUpstream{}, err
		}
		item.APIKeyCiphertext = existing.APIKeyCiphertext
		item.APIKeyPrefix = existing.APIKeyPrefix
		item.CreatedBy = existing.CreatedBy
		item.CreatedAt = existing.CreatedAt
	}
	updated, err := repo.UpdateLLMUpstream(ctx, item)
	if err != nil {
		return domainaigateway.LLMUpstream{}, err
	}
	_ = s.recordRelayAudit(ctx, principal, "ai_gateway.relay.upstream.update", "success", "updated AI Gateway relay upstream", map[string]any{
		"upstreamId":   updated.ID,
		"providerKind": updated.ProviderKind,
		"apiKeyPrefix": updated.APIKeyPrefix,
	})
	return updated, nil
}

func (s *Service) TestLLMUpstream(ctx context.Context, principal domainidentity.Principal, upstreamID string) (domainaigateway.LLMUpstreamTestResult, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayRelayManage); err != nil {
		return domainaigateway.LLMUpstreamTestResult{}, err
	}
	repo := s.llmRelayRepository()
	if repo == nil {
		return domainaigateway.LLMUpstreamTestResult{}, fmt.Errorf("%w: AI Gateway relay repository is not configured", apperrors.ErrInvalidArgument)
	}
	upstreamID = strings.TrimSpace(upstreamID)
	if upstreamID == "" {
		return domainaigateway.LLMUpstreamTestResult{}, fmt.Errorf("%w: upstream ID is required", apperrors.ErrInvalidArgument)
	}
	upstream, err := repo.GetLLMUpstream(ctx, upstreamID)
	if err != nil {
		return domainaigateway.LLMUpstreamTestResult{}, err
	}
	result, err := s.testRelayUpstream(ctx, upstream)
	if err != nil {
		_ = s.recordRelayAudit(ctx, principal, "ai_gateway.relay.upstream.test", "failure", "tested AI Gateway relay upstream", map[string]any{
			"upstreamId":   upstream.ID,
			"providerKind": upstream.ProviderKind,
			"error":        redactRelayText(err.Error()),
		})
		return domainaigateway.LLMUpstreamTestResult{}, err
	}
	_ = s.recordRelayAudit(ctx, principal, "ai_gateway.relay.upstream.test", result.Status, "tested AI Gateway relay upstream", map[string]any{
		"upstreamId":   upstream.ID,
		"providerKind": upstream.ProviderKind,
		"httpStatus":   result.HTTPStatus,
		"durationMs":   result.DurationMs,
	})
	return result, nil
}

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

func (s *Service) ListLLMModelRoutes(ctx context.Context, principal domainidentity.Principal, filter domainaigateway.LLMModelRouteFilter) ([]domainaigateway.LLMModelRoute, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayRelayView); err != nil {
		return nil, err
	}
	repo := s.llmRelayRepository()
	if repo == nil {
		return nil, fmt.Errorf("%w: AI Gateway relay repository is not configured", apperrors.ErrInvalidArgument)
	}
	return repo.ListLLMModelRoutes(ctx, filter)
}

func (s *Service) CreateLLMModelRoute(ctx context.Context, principal domainidentity.Principal, input domainaigateway.LLMModelRouteInput) (domainaigateway.LLMModelRoute, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayRelayManage); err != nil {
		return domainaigateway.LLMModelRoute{}, err
	}
	repo := s.llmRelayRepository()
	if repo == nil {
		return domainaigateway.LLMModelRoute{}, fmt.Errorf("%w: AI Gateway relay repository is not configured", apperrors.ErrInvalidArgument)
	}
	item, err := normalizeLLMModelRouteInput("", input)
	if err != nil {
		return domainaigateway.LLMModelRoute{}, err
	}
	created, err := repo.CreateLLMModelRoute(ctx, item)
	if err != nil {
		return domainaigateway.LLMModelRoute{}, err
	}
	_ = s.recordRelayAudit(ctx, principal, "ai_gateway.relay.model_route.create", "success", "created AI Gateway relay model route", map[string]any{
		"routeId":     created.ID,
		"publicModel": created.PublicModel,
		"upstreamId":  created.UpstreamID,
	})
	return created, nil
}

func (s *Service) UpdateLLMModelRoute(ctx context.Context, principal domainidentity.Principal, routeID string, input domainaigateway.LLMModelRouteInput) (domainaigateway.LLMModelRoute, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayRelayManage); err != nil {
		return domainaigateway.LLMModelRoute{}, err
	}
	repo := s.llmRelayRepository()
	if repo == nil {
		return domainaigateway.LLMModelRoute{}, fmt.Errorf("%w: AI Gateway relay repository is not configured", apperrors.ErrInvalidArgument)
	}
	item, err := normalizeLLMModelRouteInput(routeID, input)
	if err != nil {
		return domainaigateway.LLMModelRoute{}, err
	}
	updated, err := repo.UpdateLLMModelRoute(ctx, item)
	if err != nil {
		return domainaigateway.LLMModelRoute{}, err
	}
	_ = s.recordRelayAudit(ctx, principal, "ai_gateway.relay.model_route.update", "success", "updated AI Gateway relay model route", map[string]any{
		"routeId":     updated.ID,
		"publicModel": updated.PublicModel,
		"upstreamId":  updated.UpstreamID,
	})
	return updated, nil
}

func (s *Service) DeleteLLMModelRoute(ctx context.Context, principal domainidentity.Principal, routeID string) error {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayRelayManage); err != nil {
		return err
	}
	repo := s.llmRelayRepository()
	if repo == nil {
		return fmt.Errorf("%w: AI Gateway relay repository is not configured", apperrors.ErrInvalidArgument)
	}
	routeID = strings.TrimSpace(routeID)
	if routeID == "" {
		return fmt.Errorf("%w: route ID is required", apperrors.ErrInvalidArgument)
	}
	if err := repo.DeleteLLMModelRoute(ctx, routeID); err != nil {
		return err
	}
	_ = s.recordRelayAudit(ctx, principal, "ai_gateway.relay.model_route.delete", "success", "deleted AI Gateway relay model route", map[string]any{
		"routeId": routeID,
	})
	return nil
}

func (s *Service) ListLLMCallLogs(ctx context.Context, principal domainidentity.Principal, filter domainaigateway.LLMCallLogFilter) ([]domainaigateway.LLMCallLog, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayRelayManage); err != nil {
		return nil, err
	}
	repo := s.llmRelayRepository()
	if repo == nil {
		return nil, fmt.Errorf("%w: AI Gateway relay repository is not configured", apperrors.ErrInvalidArgument)
	}
	return repo.ListLLMCallLogs(ctx, filter)
}

func (s *Service) LLMRelayMetrics(ctx context.Context, principal domainidentity.Principal) (domainaigateway.LLMRelayMetrics, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayRelayView); err != nil {
		return domainaigateway.LLMRelayMetrics{}, err
	}
	repo := s.llmRelayRepository()
	if repo == nil {
		return domainaigateway.LLMRelayMetrics{}, fmt.Errorf("%w: AI Gateway relay repository is not configured", apperrors.ErrInvalidArgument)
	}
	now := time.Now().UTC()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	logMetrics, err := repo.LLMRelayCallLogMetrics(ctx, domainaigateway.LLMCallLogFilter{From: &startOfDay})
	if err != nil {
		return domainaigateway.LLMRelayMetrics{}, err
	}
	upstreams, err := repo.ListLLMUpstreams(ctx, domainaigateway.LLMUpstreamFilter{IncludeAll: true})
	if err != nil {
		return domainaigateway.LLMRelayMetrics{}, err
	}
	return relayMetricsFromCallLogMetrics(logMetrics, upstreams, now), nil
}

func (s *Service) LLMRelayMaxRequestBodyBytes() int64 {
	if s == nil {
		return defaultRelayMaxRequestBytes
	}
	return s.relayConfig.MaxRequestBodyBytes
}

func (s *Service) LLMRelayCacheStats(ctx context.Context, principal domainidentity.Principal, req domainaigateway.LLMRelayCacheStatsRequest) (domainaigateway.LLMRelayCacheStats, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayRelayView); err != nil {
		return domainaigateway.LLMRelayCacheStats{}, err
	}
	repo := s.llmRelayRepository()
	if repo == nil {
		return domainaigateway.LLMRelayCacheStats{}, fmt.Errorf("%w: AI Gateway relay repository is not configured", apperrors.ErrInvalidArgument)
	}
	windowHours := req.WindowHours
	if windowHours <= 0 {
		windowHours = 24
	}
	if windowHours > 168 {
		windowHours = 168
	}
	now := time.Now().UTC()
	from := now.Add(-time.Duration(windowHours) * time.Hour)
	logStats, err := repo.LLMRelayCacheLogStats(ctx, domainaigateway.LLMCallLogFilter{
		PublicModel: strings.TrimSpace(req.PublicModel),
		UpstreamID:  strings.TrimSpace(req.UpstreamID),
		From:        &from,
		To:          &now,
	})
	if err != nil {
		return domainaigateway.LLMRelayCacheStats{}, err
	}
	enabled, err := s.llmRelayResponseCacheEnabled(ctx, req)
	if err != nil {
		return domainaigateway.LLMRelayCacheStats{}, err
	}
	return domainaigateway.LLMRelayCacheStats{
		GeneratedAt:               now,
		WindowHours:               windowHours,
		ResponseCacheEnabled:      enabled,
		ResponseCacheHits:         logStats.ResponseCacheHits,
		ResponseCacheMisses:       logStats.ResponseCacheMisses,
		ResponseCacheWrites:       logStats.ResponseCacheWrites,
		ResponseCacheBypasses:     logStats.ResponseCacheBypasses,
		ProviderCachedReadTokens:  logStats.ProviderCachedReadTokens,
		ProviderCachedWriteTokens: logStats.ProviderCachedWriteTokens,
		ByModel:                   logStats.ByModel,
		ByUpstream:                logStats.ByUpstream,
	}, nil
}

func (s *Service) llmRelayResponseCacheEnabled(ctx context.Context, req domainaigateway.LLMRelayCacheStatsRequest) (bool, error) {
	repo := s.llmRelayRepository()
	if repo == nil {
		return false, nil
	}
	routes, err := repo.ListLLMModelRoutes(ctx, domainaigateway.LLMModelRouteFilter{
		PublicModel: strings.TrimSpace(req.PublicModel),
		UpstreamID:  strings.TrimSpace(req.UpstreamID),
	})
	if err != nil {
		return false, err
	}
	for _, route := range routes {
		if relayResponseCachePolicyFromRoute(route).enabled {
			return true, nil
		}
	}
	return false, nil
}

func (s *Service) PurgeLLMRelayCache(ctx context.Context, principal domainidentity.Principal, req domainaigateway.LLMRelayCachePurgeRequest) (domainaigateway.LLMRelayCachePurgeResult, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayRelayManage); err != nil {
		return domainaigateway.LLMRelayCachePurgeResult{}, err
	}
	repo := s.llmRelayRepository()
	if repo == nil {
		return domainaigateway.LLMRelayCachePurgeResult{}, fmt.Errorf("%w: AI Gateway relay repository is not configured", apperrors.ErrInvalidArgument)
	}
	result, err := s.purgeLLMRelayCache(ctx, req)
	if err != nil {
		return domainaigateway.LLMRelayCachePurgeResult{}, err
	}
	_ = s.recordRelayAudit(ctx, principal, "ai_gateway.relay.cache.purge", "success", "purged AI Gateway relay response cache", map[string]any{
		"publicModel": strings.TrimSpace(req.PublicModel),
		"upstreamId":  strings.TrimSpace(req.UpstreamID),
		"routeGroup":  strings.TrimSpace(req.RouteGroup),
		"olderThan":   relayOptionalTimeString(req.OlderThan),
		"dryRun":      req.DryRun,
		"purgedCount": result.PurgedCount,
	})
	return result, nil
}

func (s *Service) purgeLLMRelayCache(ctx context.Context, req domainaigateway.LLMRelayCachePurgeRequest) (domainaigateway.LLMRelayCachePurgeResult, error) {
	repo := s.llmRelayRepository()
	if strings.TrimSpace(req.RouteGroup) == "" {
		count, err := purgeLLMRelayCacheWithFilter(ctx, repo, relayCacheEntryFilterFromPurgeRequest(req), req.DryRun)
		if err != nil {
			return domainaigateway.LLMRelayCachePurgeResult{}, err
		}
		return domainaigateway.LLMRelayCachePurgeResult{Status: relayCachePurgeStatus(req.DryRun), PurgedCount: count, DryRun: req.DryRun}, nil
	}
	routes, err := repo.ListLLMModelRoutes(ctx, domainaigateway.LLMModelRouteFilter{
		PublicModel:     strings.TrimSpace(req.PublicModel),
		UpstreamID:      strings.TrimSpace(req.UpstreamID),
		RouteGroup:      strings.TrimSpace(req.RouteGroup),
		IncludeDisabled: true,
	})
	if err != nil {
		return domainaigateway.LLMRelayCachePurgeResult{}, err
	}
	seen := map[string]struct{}{}
	total := 0
	for _, route := range routes {
		filter := relayCacheEntryFilterFromPurgeRequest(req)
		filter.PublicModel = strings.TrimSpace(route.PublicModel)
		if strings.TrimSpace(route.UpstreamID) != "" {
			filter.UpstreamID = strings.TrimSpace(route.UpstreamID)
		}
		key := filter.PublicModel + "\x00" + filter.UpstreamID
		if _, ok := seen[key]; ok || filter.PublicModel == "" {
			continue
		}
		seen[key] = struct{}{}
		count, err := purgeLLMRelayCacheWithFilter(ctx, repo, filter, req.DryRun)
		if err != nil {
			return domainaigateway.LLMRelayCachePurgeResult{}, err
		}
		total += count
	}
	return domainaigateway.LLMRelayCachePurgeResult{Status: relayCachePurgeStatus(req.DryRun), PurgedCount: total, DryRun: req.DryRun}, nil
}

func purgeLLMRelayCacheWithFilter(ctx context.Context, repo LLMRelayRepository, filter domainaigateway.LLMCacheEntryFilter, dryRun bool) (int, error) {
	if dryRun {
		return repo.CountLLMCacheEntries(ctx, filter)
	}
	return repo.DeleteLLMCacheEntries(ctx, filter)
}

func relayCacheEntryFilterFromPurgeRequest(req domainaigateway.LLMRelayCachePurgeRequest) domainaigateway.LLMCacheEntryFilter {
	return domainaigateway.LLMCacheEntryFilter{
		PublicModel:   strings.TrimSpace(req.PublicModel),
		UpstreamID:    strings.TrimSpace(req.UpstreamID),
		UpdatedBefore: req.OlderThan,
	}
}

func relayCachePurgeStatus(dryRun bool) string {
	if dryRun {
		return "dry_run"
	}
	return "purged"
}

func relayOptionalTimeString(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func (s *Service) RelayLLMHTTP(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, writer http.ResponseWriter) error {
	if !s.relayConfig.Enabled {
		return fmt.Errorf("%w: AI Gateway LLM relay is disabled", apperrors.ErrNotFound)
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayRelayInvoke); err != nil {
		return err
	}
	if _, err := s.AuthorizeLLMRelayToken(ctx, principal, accessCtx, LLMRelayAccessRequest{
		ProviderKind: req.ProviderKind,
		SourceIP:     req.SourceIP,
	}); err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(req.Method), http.MethodGet) && strings.EqualFold(req.Endpoint, "models") {
		return s.writeRelayModels(ctx, principal, accessCtx, req, writer)
	}
	model, stream, err := relayRequestModel(req)
	if err != nil {
		return err
	}
	if _, err := s.AuthorizeLLMRelayToken(ctx, principal, accessCtx, LLMRelayAccessRequest{
		Model:        model,
		ProviderKind: req.ProviderKind,
		SourceIP:     req.SourceIP,
	}); err != nil {
		return err
	}
	if err := s.authorizeRelayRouteTrace(ctx, principal, accessCtx, req); err != nil {
		return err
	}
	selections, err := s.selectRelayUpstreamCandidatesForPrincipal(ctx, principal, req.ProviderKind, model)
	if err != nil {
		return err
	}
	requestedUpstreamID := relayRequestedUpstreamID(req)
	if requestedUpstreamID != "" {
		if err := s.authorizeRelayExplicitUpstream(ctx, principal, accessCtx, requestedUpstreamID); err != nil {
			return err
		}
		selections = filterRelaySelectionsByUpstream(selections, requestedUpstreamID)
		if len(selections) == 0 {
			return fmt.Errorf("%w: requested relay upstream is not available for model %s", apperrors.ErrNotFound, model)
		}
	}
	authorized := make([]relaySelection, 0, len(selections))
	var authErr error
	for _, selection := range selections {
		if _, err := s.AuthorizeLLMRelayToken(ctx, principal, accessCtx, LLMRelayAccessRequest{
			Model:        model,
			ProviderKind: req.ProviderKind,
			UpstreamID:   selection.upstream.ID,
			SourceIP:     req.SourceIP,
		}); err != nil {
			authErr = err
			continue
		}
		authorized = append(authorized, selection)
	}
	if len(authorized) == 0 {
		if authErr != nil {
			return authErr
		}
		return fmt.Errorf("%w: no authorized relay upstream for model %s", apperrors.ErrNotFound, model)
	}
	releaseTokenConcurrency, tokenConcurrencyCode, tokenConcurrencyMessage, acquired := s.tryAcquireRelayTokenConcurrency(accessCtx, stream)
	if !acquired {
		s.recordRelayCall(ctx, principal, accessCtx, req, authorized[0], model, stream, domainaigateway.LLMCallLog{
			Status:       "rate_limited",
			HTTPStatus:   http.StatusTooManyRequests,
			ErrorCode:    tokenConcurrencyCode,
			ErrorMessage: tokenConcurrencyMessage,
			InputBytes:   int64(len(req.Body)),
			CreatedAt:    time.Now().UTC(),
		})
		return fmt.Errorf("%w: %s", apperrors.ErrAccessDenied, tokenConcurrencyMessage)
	}
	defer releaseTokenConcurrency()
	return s.proxyRelayRequestWithFallback(ctx, principal, accessCtx, req, authorized, model, stream, writer)
}

func (s *Service) RelayLLMWebSocket(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, writer http.ResponseWriter, clientRequest *http.Request) error {
	if !s.relayConfig.Enabled {
		return fmt.Errorf("%w: AI Gateway LLM relay is disabled", apperrors.ErrNotFound)
	}
	if normalizeRelayProviderKind(req.ProviderKind) != "openai" || strings.TrimSpace(req.Endpoint) != "realtime" {
		return fmt.Errorf("%w: realtime relay only supports OpenAI-compatible websocket requests", apperrors.ErrInvalidArgument)
	}
	if clientRequest == nil {
		return fmt.Errorf("%w: realtime relay request is required", apperrors.ErrInvalidArgument)
	}
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayRelayInvoke); err != nil {
		return err
	}
	if _, err := s.AuthorizeLLMRelayToken(ctx, principal, accessCtx, LLMRelayAccessRequest{
		ProviderKind: req.ProviderKind,
		SourceIP:     req.SourceIP,
	}); err != nil {
		return err
	}
	model, err := relayRealtimeRequestModel(req)
	if err != nil {
		return err
	}
	if _, err := s.AuthorizeLLMRelayToken(ctx, principal, accessCtx, LLMRelayAccessRequest{
		Model:        model,
		ProviderKind: req.ProviderKind,
		SourceIP:     req.SourceIP,
	}); err != nil {
		return err
	}
	if err := s.authorizeRelayRouteTrace(ctx, principal, accessCtx, req); err != nil {
		return err
	}
	selections, err := s.selectRelayUpstreamCandidatesForPrincipal(ctx, principal, req.ProviderKind, model)
	if err != nil {
		return err
	}
	selections = filterRelayRealtimeSelections(selections, req.ProviderKind)
	if len(selections) == 0 {
		return fmt.Errorf("%w: no active realtime relay route for model %s", apperrors.ErrNotFound, model)
	}
	requestedUpstreamID := relayRequestedUpstreamID(req)
	if requestedUpstreamID != "" {
		if err := s.authorizeRelayExplicitUpstream(ctx, principal, accessCtx, requestedUpstreamID); err != nil {
			return err
		}
		selections = filterRelaySelectionsByUpstream(selections, requestedUpstreamID)
		if len(selections) == 0 {
			return fmt.Errorf("%w: requested relay upstream is not available for model %s", apperrors.ErrNotFound, model)
		}
	}
	authorized := make([]relaySelection, 0, len(selections))
	var authErr error
	for _, selection := range selections {
		if _, err := s.AuthorizeLLMRelayToken(ctx, principal, accessCtx, LLMRelayAccessRequest{
			Model:        model,
			ProviderKind: req.ProviderKind,
			UpstreamID:   selection.upstream.ID,
			SourceIP:     req.SourceIP,
		}); err != nil {
			authErr = err
			continue
		}
		authorized = append(authorized, selection)
	}
	if len(authorized) == 0 {
		if authErr != nil {
			return authErr
		}
		return fmt.Errorf("%w: no authorized relay upstream for model %s", apperrors.ErrNotFound, model)
	}
	const stream = true
	releaseTokenConcurrency, tokenConcurrencyCode, tokenConcurrencyMessage, acquired := s.tryAcquireRelayTokenConcurrency(accessCtx, stream)
	if !acquired {
		s.recordRelayCall(ctx, principal, accessCtx, req, authorized[0], model, stream, domainaigateway.LLMCallLog{
			Status:       "rate_limited",
			HTTPStatus:   http.StatusTooManyRequests,
			ErrorCode:    tokenConcurrencyCode,
			ErrorMessage: tokenConcurrencyMessage,
			CacheStatus:  relayCacheBypass,
			CreatedAt:    time.Now().UTC(),
		})
		return fmt.Errorf("%w: %s", apperrors.ErrAccessDenied, tokenConcurrencyMessage)
	}
	defer releaseTokenConcurrency()
	return s.proxyRelayWebSocketWithFallback(ctx, principal, accessCtx, req, authorized, model, writer, clientRequest)
}

func filterRelayRealtimeSelections(selections []relaySelection, providerKind string) []relaySelection {
	out := make([]relaySelection, 0, len(selections))
	for _, selection := range selections {
		if relayTransformPlanForRoute(selection.route, providerKind).enabled {
			continue
		}
		if !relayProviderUsesOpenAIWireProtocol(selection.upstream.ProviderKind) {
			continue
		}
		out = append(out, selection)
	}
	return out
}

type relaySelection struct {
	route    domainaigateway.LLMModelRoute
	upstream domainaigateway.LLMUpstream
}

func (s relaySelection) upstreamProviderKind() string {
	return normalizeRelayProviderKind(s.upstream.ProviderKind)
}

func (s *Service) enforceRelayRateLimits(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selection relaySelection, publicModel string, stream bool) error {
	if s == nil {
		return nil
	}
	toolName := relayRateLimitToolName(publicModel, selection.upstream.ID)
	if err := s.enforceRelayRateLimitRules(ctx, principal, relayRateLimitTokenClientID(accessCtx), toolName, accessCtx.Metadata, "token"); err != nil {
		return err
	}
	if err := s.enforceRelayRateLimitRules(ctx, principal, relayRateLimitRouteClientID(selection.route), relayRateLimitModelToolName(publicModel), selection.route.Metadata, "route"); err != nil {
		return err
	}
	if err := s.enforceRelayRateLimitRules(ctx, principal, relayRateLimitUpstreamClientID(selection.upstream), relayRateLimitProviderToolName(selection.upstreamProviderKind()), selection.upstream.Metadata, "upstream"); err != nil {
		return err
	}
	if err := s.enforceRelayTokenPerMinuteLimits(ctx, principal, accessCtx, req, selection, publicModel, stream); err != nil {
		return err
	}
	return nil
}

func (s *Service) enforceRelayRateLimitRules(ctx context.Context, principal domainidentity.Principal, clientID, toolName string, metadata map[string]any, source string) error {
	for _, limit := range relayRateLimitRules(metadata, source) {
		if err := s.enforceGatewayInvocationLimit(ctx, principal, clientID, toolName, limit); err != nil {
			return err
		}
	}
	return nil
}

func relayRateLimitRules(metadata map[string]any, source string) []gatewayInvocationLimit {
	if len(metadata) == 0 {
		return nil
	}
	policyID := "relay-" + strings.TrimSpace(source) + "-rate-limit"
	rules := gatewayRateLimitRules(metadata, policyID)
	for index := range rules {
		if strings.TrimSpace(rules[index].Scope) == "" || rules[index].Scope == "actor_client_tool" {
			rules[index].Scope = relayRateLimitDefaultScope(source)
		}
	}
	return rules
}

func relayRateLimitDefaultScope(source string) string {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "route", "upstream":
		return "client_tool"
	default:
		return "actor_client_tool"
	}
}

func relayRateLimitTokenClientID(accessCtx domainidentity.AccessContext) string {
	tokenID := strings.TrimSpace(accessCtx.TokenID)
	if tokenID == "" {
		tokenID = strings.TrimSpace(accessCtx.TokenPrefix)
	}
	if tokenID == "" {
		tokenID = strings.TrimSpace(accessCtx.SubjectID)
	}
	return "relay-token:" + tokenID
}

func relayRateLimitRouteClientID(route domainaigateway.LLMModelRoute) string {
	routeID := strings.TrimSpace(route.ID)
	if routeID == "" {
		routeID = strings.TrimSpace(route.PublicModel)
	}
	return "relay-route:" + routeID
}

func relayRateLimitUpstreamClientID(upstream domainaigateway.LLMUpstream) string {
	upstreamID := strings.TrimSpace(upstream.ID)
	if upstreamID == "" {
		upstreamID = strings.TrimSpace(upstream.Name)
	}
	return "relay-upstream:" + upstreamID
}

func relayRateLimitToolName(publicModel, upstreamID string) string {
	return "llm-relay:" + strings.TrimSpace(publicModel) + ":upstream:" + strings.TrimSpace(upstreamID)
}

func relayRateLimitModelToolName(publicModel string) string {
	return "llm-relay:model:" + strings.TrimSpace(publicModel)
}

func relayRateLimitProviderToolName(providerKind string) string {
	return "llm-relay:provider:" + normalizeRelayProviderKind(providerKind)
}

type relayTokenPerMinuteLimit struct {
	source string
	limit  int
}

func (s *Service) enforceRelayTokenPerMinuteLimits(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selection relaySelection, publicModel string, stream bool) error {
	if s == nil || s.llmRelayRepository() == nil {
		return nil
	}
	checks := []relayTokenPerMinuteLimit{
		{source: "token", limit: relayTokenPerMinuteLimitFromMetadata(accessCtx.Metadata)},
		{source: "route", limit: relayTokenPerMinuteLimitFromMetadata(selection.route.Metadata)},
		{source: "upstream", limit: relayTokenPerMinuteLimitFromMetadata(selection.upstream.Metadata)},
	}
	for _, check := range checks {
		if check.limit <= 0 {
			continue
		}
		used, err := s.relayTokensUsedInWindow(ctx, accessCtx, selection, publicModel, check.source, time.Minute)
		if err != nil {
			return err
		}
		if used < check.limit {
			continue
		}
		message := fmt.Sprintf("relay %s token-per-minute limit exceeded (%d/%d tokens in 1m)", check.source, used, check.limit)
		s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, stream, domainaigateway.LLMCallLog{
			Status:       "rate_limited",
			HTTPStatus:   http.StatusTooManyRequests,
			ErrorCode:    "token_per_minute_limited",
			ErrorMessage: message,
			InputBytes:   int64(len(req.Body)),
			CreatedAt:    time.Now().UTC(),
		})
		return fmt.Errorf("%w: %s", apperrors.ErrAccessDenied, message)
	}
	return nil
}

func relayTokenPerMinuteLimitFromMetadata(metadata map[string]any) int {
	if len(metadata) == 0 {
		return 0
	}
	values := gatewayConditionValues(metadata, "rateLimit", "rate_limit", "rateLimits", "tokenRateLimit", "token_rate_limit", "tokenLimits", "token_limits", "limits")
	limit, _ := gatewayFirstPositiveInt(values,
		"tpm",
		"maxTPM",
		"tokensPerMinute",
		"tokensPerMin",
		"tokenPerMinute",
		"tokenPerMin",
		"maxTokensPerMinute",
		"maxTokenPerMinute",
		"maxTotalTokensPerMinute",
		"totalTokensPerMinute",
		"perMinuteTokens",
		"minuteTokens",
	)
	return limit
}

func (s *Service) relayTokensUsedInWindow(ctx context.Context, accessCtx domainidentity.AccessContext, selection relaySelection, publicModel, source string, window time.Duration) (int, error) {
	repo := s.llmRelayRepository()
	if repo == nil || window <= 0 {
		return 0, nil
	}
	now := time.Now().UTC()
	from := now.Add(-window)
	filter := domainaigateway.LLMCallLogFilter{From: &from, To: &now, Limit: 500}
	switch source {
	case "token":
		if strings.TrimSpace(accessCtx.TokenID) != "" {
			filter.TokenID = strings.TrimSpace(accessCtx.TokenID)
		} else if strings.TrimSpace(accessCtx.TokenPrefix) != "" {
			filter.TokenPrefix = strings.TrimSpace(accessCtx.TokenPrefix)
		}
		filter.TokenKind = strings.TrimSpace(accessCtx.TokenKind)
	case "route":
		filter.PublicModel = strings.TrimSpace(publicModel)
	case "upstream":
		filter.UpstreamID = strings.TrimSpace(selection.upstream.ID)
	}
	return repo.SumLLMCallTokens(ctx, filter)
}

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

func (s *Service) normalizeLLMUpstreamInput(ctx context.Context, principal domainidentity.Principal, upstreamID string, input domainaigateway.LLMUpstreamInput) (domainaigateway.LLMUpstream, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return domainaigateway.LLMUpstream{}, fmt.Errorf("%w: upstream name is required", apperrors.ErrInvalidArgument)
	}
	providerKind := normalizeRelayProviderKind(input.ProviderKind)
	if providerKind == "" {
		return domainaigateway.LLMUpstream{}, fmt.Errorf("%w: provider kind is required", apperrors.ErrInvalidArgument)
	}
	baseURL := strings.TrimSpace(input.BaseURL)
	if err := s.validateRelayUpstreamURL(baseURL); err != nil {
		return domainaigateway.LLMUpstream{}, err
	}
	id := strings.TrimSpace(upstreamID)
	if id == "" {
		id = strings.TrimSpace(input.ID)
	}
	if id == "" {
		id = uuid.NewString()
	}
	status := strings.TrimSpace(input.Status)
	if status == "" {
		status = "active"
	}
	weight := input.Weight
	if weight <= 0 {
		weight = 1
	}
	timeoutSeconds := input.TimeoutSeconds
	if timeoutSeconds <= 0 {
		timeoutSeconds = int(s.relayConfig.DefaultTimeout.Seconds())
	}
	streamTimeoutSeconds := input.StreamTimeoutSeconds
	if streamTimeoutSeconds <= 0 {
		streamTimeoutSeconds = int(s.relayConfig.StreamTimeout.Seconds())
	}
	apiKeyCiphertext, apiKeyPrefix, err := s.encryptRelayAPIKey(input.APIKey)
	if err != nil {
		return domainaigateway.LLMUpstream{}, err
	}
	now := time.Now().UTC()
	return domainaigateway.LLMUpstream{
		ID:                   id,
		Name:                 name,
		ProviderKind:         providerKind,
		BaseURL:              baseURL,
		APIKeyCiphertext:     apiKeyCiphertext,
		APIKeyPrefix:         apiKeyPrefix,
		Status:               status,
		Priority:             input.Priority,
		Weight:               weight,
		TimeoutSeconds:       timeoutSeconds,
		StreamTimeoutSeconds: streamTimeoutSeconds,
		MaxConcurrency:       input.MaxConcurrency,
		SupportedModels:      normalizeStringSlice(input.SupportedModels),
		DefaultHeaders:       sanitizeRelayHeadersMap(input.DefaultHeaders),
		ProxyURL:             strings.TrimSpace(input.ProxyURL),
		Health:               emptyMap(input.Health),
		Metadata:             emptyMap(input.Metadata),
		CreatedBy:            principal.UserID,
		CreatedAt:            now,
		UpdatedAt:            now,
	}, nil
}

func normalizeLLMModelRouteInput(routeID string, input domainaigateway.LLMModelRouteInput) (domainaigateway.LLMModelRoute, error) {
	publicModel := strings.TrimSpace(input.PublicModel)
	if publicModel == "" {
		return domainaigateway.LLMModelRoute{}, fmt.Errorf("%w: public model is required", apperrors.ErrInvalidArgument)
	}
	upstreamModel := strings.TrimSpace(input.UpstreamModel)
	if upstreamModel == "" {
		upstreamModel = publicModel
	}
	id := strings.TrimSpace(routeID)
	if id == "" {
		id = strings.TrimSpace(input.ID)
	}
	if id == "" {
		id = uuid.NewString()
	}
	weight := input.Weight
	if weight <= 0 {
		weight = 1
	}
	enabled := true
	if input.Enabled != nil {
		enabled = *input.Enabled
	}
	now := time.Now().UTC()
	return domainaigateway.LLMModelRoute{
		ID:                 id,
		PublicModel:        publicModel,
		ProviderKind:       normalizeRelayProviderKind(input.ProviderKind),
		UpstreamID:         strings.TrimSpace(input.UpstreamID),
		UpstreamModel:      upstreamModel,
		RouteGroup:         strings.TrimSpace(input.RouteGroup),
		Priority:           input.Priority,
		Weight:             weight,
		Enabled:            enabled,
		TransformPolicy:    emptyMap(input.TransformPolicy),
		FallbackPolicy:     emptyMap(input.FallbackPolicy),
		CachePolicy:        emptyMap(input.CachePolicy),
		RateLimitProfileID: strings.TrimSpace(input.RateLimitProfileID),
		Metadata:           emptyMap(input.Metadata),
		CreatedAt:          now,
		UpdatedAt:          now,
	}, nil
}

func (s *Service) encryptRelayAPIKey(apiKey string) (string, string, error) {
	apiKey = strings.TrimSpace(apiKey)
	if apiKey == "" {
		return "", "", nil
	}
	ciphertext, err := secretcrypto.EncryptString(s.relayConfig.CredentialEncryptionKey, apiKey)
	if err != nil {
		return "", "", fmt.Errorf("%w: relay upstream API key encryption failed", apperrors.ErrInvalidArgument)
	}
	return ciphertext, relaySecretPrefix(apiKey), nil
}

func (s *Service) decryptRelayAPIKey(ciphertext string) (string, error) {
	apiKey, err := secretcrypto.DecryptString(s.relayConfig.CredentialEncryptionKey, ciphertext)
	if err != nil {
		return "", fmt.Errorf("%w: relay upstream API key decrypt failed", apperrors.ErrInvalidArgument)
	}
	return apiKey, nil
}

func relaySecretPrefix(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) <= 8 {
		return value
	}
	return value[:8]
}

func sanitizeRelayHeadersMap(headers map[string]any) map[string]any {
	out := make(map[string]any)
	for key, value := range headers {
		name := http.CanonicalHeaderKey(strings.TrimSpace(key))
		if name == "" || isSensitiveRelayHeader(name) {
			continue
		}
		out[name] = value
	}
	return out
}

func isSensitiveRelayHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "authorization", "proxy-authorization", "x-api-key", "x-goog-api-key", "google-api-key", "gemini-api-key", "cohere-api-key", "api-key", "openai-api-key", "anthropic-api-key", "cookie", "set-cookie":
		return true
	default:
		return false
	}
}

func normalizeRelayProviderKind(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "azure_openai" {
		value = "azure-openai"
	}
	switch value {
	case "openai", "anthropic", "openai-compatible", "deepseek", "qwen", "openrouter", "azure-openai", "gemini", "cohere":
		return value
	default:
		return ""
	}
}

func relayProviderUsesOpenAIWireProtocol(providerKind string) bool {
	switch normalizeRelayProviderKind(providerKind) {
	case "openai", "openai-compatible", "deepseek", "qwen", "openrouter", "azure-openai":
		return true
	default:
		return false
	}
}

func containsFold(values []string, candidate string) bool {
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), strings.TrimSpace(candidate)) {
			return true
		}
	}
	return false
}

func (s *Service) writeRelayModels(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, writer http.ResponseWriter) error {
	repo := s.llmRelayRepository()
	if repo == nil {
		return fmt.Errorf("%w: AI Gateway relay repository is not configured", apperrors.ErrInvalidArgument)
	}
	routes, err := repo.ListLLMModelRoutes(ctx, domainaigateway.LLMModelRouteFilter{})
	if err != nil {
		return err
	}
	metadata, err := ParseLLMTokenMetadata(accessCtx.Metadata)
	if err != nil {
		return err
	}
	models := make([]string, 0, len(routes))
	for _, route := range routes {
		if !route.Enabled || route.PublicModel == "" || slices.Contains(models, route.PublicModel) || !relayRouteMatchesRequestProvider(route, req.ProviderKind) {
			continue
		}
		if !relayAllowListAllows(metadata.AllowedModels, route.PublicModel, false) {
			continue
		}
		if !relayMetadataTeamPolicyAllows(principal.Teams, route.Metadata) {
			continue
		}
		if !s.relayRouteUpstreamTeamPolicyAllows(ctx, principal.Teams, route, req.ProviderKind) {
			continue
		}
		models = append(models, route.PublicModel)
	}
	sort.Strings(models)
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(http.StatusOK)
	if req.ProviderKind == "anthropic" {
		_ = json.NewEncoder(writer).Encode(anthropicModelsResponse(models))
		return nil
	}
	if normalizeRelayProviderKind(req.ProviderKind) == "gemini" {
		_ = json.NewEncoder(writer).Encode(geminiModelsResponse(models))
		return nil
	}
	_ = json.NewEncoder(writer).Encode(openAIModelsResponse(models))
	return nil
}

func openAIModelsResponse(models []string) map[string]any {
	data := make([]map[string]any, 0, len(models))
	for _, model := range models {
		data = append(data, map[string]any{
			"id":       model,
			"object":   "model",
			"created":  0,
			"owned_by": "soha",
		})
	}
	return map[string]any{"object": "list", "data": data}
}

func anthropicModelsResponse(models []string) map[string]any {
	data := make([]map[string]any, 0, len(models))
	for _, model := range models {
		data = append(data, map[string]any{
			"id":           model,
			"type":         "model",
			"display_name": model,
		})
	}
	return map[string]any{
		"data":     data,
		"has_more": false,
		"first_id": firstModelID(models),
		"last_id":  lastModelID(models),
	}
}

func geminiModelsResponse(models []string) map[string]any {
	data := make([]map[string]any, 0, len(models))
	for _, model := range models {
		data = append(data, map[string]any{
			"name":                       "models/" + model,
			"version":                    "",
			"displayName":                model,
			"supportedGenerationMethods": []string{"generateContent"},
		})
	}
	return map[string]any{"models": data}
}

func firstModelID(models []string) string {
	if len(models) == 0 {
		return ""
	}
	return models[0]
}

func lastModelID(models []string) string {
	if len(models) == 0 {
		return ""
	}
	return models[len(models)-1]
}

func relayRequestModel(req LLMRelayHTTPRequest) (string, bool, error) {
	if pathModel := strings.TrimSpace(req.PathModel); pathModel != "" {
		return pathModel, relayEndpointSupportsStreaming(req.Endpoint), nil
	}
	if relayEndpointRequiresMultipart(req.Endpoint) {
		return relayMultipartRequestModel(req)
	}
	var payload map[string]any
	if err := json.Unmarshal(req.Body, &payload); err != nil {
		return "", false, fmt.Errorf("%w: invalid relay request JSON", apperrors.ErrInvalidArgument)
	}
	model, _ := payload["model"].(string)
	model = strings.TrimSpace(model)
	if model == "" {
		return "", false, fmt.Errorf("%w: relay request model is required", apperrors.ErrInvalidArgument)
	}
	stream, _ := payload["stream"].(bool)
	switch strings.TrimSpace(req.Endpoint) {
	case "interactions":
		if stream {
			return "", false, fmt.Errorf("%w: Gemini interactions relay streaming is not supported", apperrors.ErrInvalidArgument)
		}
		if boolFromAny(payload["background"]) {
			return "", false, fmt.Errorf("%w: Gemini interactions relay background mode is not supported", apperrors.ErrInvalidArgument)
		}
	case "images/generations":
		if stream {
			return "", false, fmt.Errorf("%w: image generation relay streaming is not supported", apperrors.ErrInvalidArgument)
		}
	case "audio/speech":
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(req.Headers.Get("Content-Type"))), "multipart/") {
			return "", false, fmt.Errorf("%w: audio speech relay only supports JSON requests", apperrors.ErrInvalidArgument)
		}
		if stream {
			return "", false, fmt.Errorf("%w: audio speech relay streaming is not supported", apperrors.ErrInvalidArgument)
		}
		if strings.EqualFold(strings.TrimSpace(fmt.Sprint(payload["stream_format"])), "sse") {
			return "", false, fmt.Errorf("%w: audio speech relay SSE streaming is not supported", apperrors.ErrInvalidArgument)
		}
	}
	return model, stream && relayEndpointSupportsStreaming(req.Endpoint), nil
}

func relayRealtimeRequestModel(req LLMRelayHTTPRequest) (string, error) {
	model := strings.TrimSpace(req.QueryModel)
	if model == "" {
		model = strings.TrimSpace(req.PathModel)
	}
	if model == "" {
		return "", fmt.Errorf("%w: realtime relay model query parameter is required", apperrors.ErrInvalidArgument)
	}
	return model, nil
}

func relayEndpointRequiresMultipart(endpoint string) bool {
	switch strings.TrimSpace(endpoint) {
	case "audio/transcriptions", "audio/translations", "images/edits", "images/variations":
		return true
	default:
		return false
	}
}

func relayMultipartRequestModel(req LLMRelayHTTPRequest) (string, bool, error) {
	fields, err := relayMultipartRequestFields(req.Endpoint, req.Body, req.Headers.Get("Content-Type"))
	if err != nil {
		return "", false, err
	}
	model := strings.TrimSpace(fields["model"])
	if model == "" {
		return "", false, fmt.Errorf("%w: relay request model is required", apperrors.ErrInvalidArgument)
	}
	if relayMultipartBool(fields["stream"]) {
		return "", false, fmt.Errorf("%w: multipart relay streaming is not supported", apperrors.ErrInvalidArgument)
	}
	if strings.EqualFold(strings.TrimSpace(fields["stream_format"]), "sse") {
		return "", false, fmt.Errorf("%w: multipart relay SSE streaming is not supported", apperrors.ErrInvalidArgument)
	}
	return model, false, nil
}

func relayMultipartRequestFields(endpoint string, body []byte, contentType string) (map[string]string, error) {
	boundary, err := relayMultipartBoundary(contentType)
	if err != nil {
		return nil, err
	}
	policy := relayMultipartPolicyForEndpoint(endpoint)
	fields := make(map[string]string, 4)
	modelCount := 0
	fileFields := map[string]bool{}
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: invalid relay multipart request", apperrors.ErrInvalidArgument)
		}
		name := part.FormName()
		if part.FileName() != "" {
			fileFields[name] = true
			if name == "model" {
				return nil, fmt.Errorf("%w: relay request model must be a text field", apperrors.ErrInvalidArgument)
			}
			continue
		}
		if name == "model" {
			modelCount++
			if modelCount > 1 {
				return nil, fmt.Errorf("%w: relay request model must not be duplicated", apperrors.ErrInvalidArgument)
			}
		}
		if !policy.trackedTextFields[name] {
			continue
		}
		value, err := io.ReadAll(io.LimitReader(part, 64*1024+1))
		if err != nil {
			return nil, fmt.Errorf("%w: invalid relay multipart field", apperrors.ErrInvalidArgument)
		}
		if len(value) > 64*1024 {
			return nil, fmt.Errorf("%w: relay multipart field is too large", apperrors.ErrInvalidArgument)
		}
		fields[name] = string(value)
	}
	for _, field := range policy.requiredFileFields {
		if !fileFields[field] {
			return nil, fmt.Errorf("%w: relay multipart %s file is required", apperrors.ErrInvalidArgument, field)
		}
	}
	for _, field := range policy.requiredTextFields {
		if strings.TrimSpace(fields[field]) == "" {
			return nil, fmt.Errorf("%w: relay multipart %s field is required", apperrors.ErrInvalidArgument, field)
		}
	}
	return fields, nil
}

type relayMultipartEndpointPolicy struct {
	requiredFileFields []string
	requiredTextFields []string
	trackedTextFields  map[string]bool
}

func relayMultipartPolicyForEndpoint(endpoint string) relayMultipartEndpointPolicy {
	policy := relayMultipartEndpointPolicy{
		trackedTextFields: map[string]bool{
			"model":         true,
			"stream":        true,
			"stream_format": true,
			"prompt":        true,
		},
	}
	switch strings.TrimSpace(endpoint) {
	case "images/edits":
		policy.requiredFileFields = []string{"image"}
		policy.requiredTextFields = []string{"prompt"}
	case "images/variations":
		policy.requiredFileFields = []string{"image"}
	default:
		policy.requiredFileFields = []string{"file"}
	}
	return policy
}

func relayMultipartBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "on":
		return true
	default:
		return false
	}
}

func relayMultipartBoundary(contentType string) (string, error) {
	mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(contentType))
	if err != nil {
		return "", fmt.Errorf("%w: invalid relay multipart content type", apperrors.ErrInvalidArgument)
	}
	if !strings.EqualFold(mediaType, "multipart/form-data") {
		return "", fmt.Errorf("%w: relay endpoint requires multipart/form-data", apperrors.ErrInvalidArgument)
	}
	boundary := strings.TrimSpace(params["boundary"])
	if boundary == "" {
		return "", fmt.Errorf("%w: relay multipart boundary is required", apperrors.ErrInvalidArgument)
	}
	return boundary, nil
}

func relayEndpointSupportsStreaming(endpoint string) bool {
	switch endpoint {
	case "chat/completions", "responses", "messages", "streamGenerateContent":
		return true
	default:
		return false
	}
}

func rewriteRelayRequestModel(body []byte, upstreamModel string) ([]byte, error) {
	return rewriteRelayRequestBody(body, upstreamModel, "", false, false)
}

func rewriteRelayMultipartRequestBody(endpoint string, body []byte, contentType, upstreamModel string) ([]byte, string, error) {
	boundary, err := relayMultipartBoundary(contentType)
	if err != nil {
		return nil, "", err
	}
	policy := relayMultipartPolicyForEndpoint(endpoint)
	reader := multipart.NewReader(bytes.NewReader(body), boundary)
	var out bytes.Buffer
	writer := multipart.NewWriter(&out)
	if err := writer.SetBoundary(boundary); err != nil {
		return nil, "", fmt.Errorf("%w: invalid relay multipart boundary", apperrors.ErrInvalidArgument)
	}
	foundModel := false
	fileFields := map[string]bool{}
	for {
		part, err := reader.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, "", fmt.Errorf("%w: invalid relay multipart request", apperrors.ErrInvalidArgument)
		}
		if part.FileName() != "" {
			fileFields[part.FormName()] = true
		}
		if part.FormName() == "model" && part.FileName() != "" {
			return nil, "", fmt.Errorf("%w: relay request model must be a text field", apperrors.ErrInvalidArgument)
		}
		partWriter, err := writer.CreatePart(cloneMIMEHeader(part.Header))
		if err != nil {
			return nil, "", fmt.Errorf("create relay multipart part: %w", err)
		}
		if part.FormName() == "model" && part.FileName() == "" {
			if foundModel {
				return nil, "", fmt.Errorf("%w: relay request model must not be duplicated", apperrors.ErrInvalidArgument)
			}
			if _, err := io.WriteString(partWriter, upstreamModel); err != nil {
				return nil, "", fmt.Errorf("write relay multipart model: %w", err)
			}
			foundModel = true
			continue
		}
		if _, err := io.Copy(partWriter, part); err != nil {
			return nil, "", fmt.Errorf("copy relay multipart part: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("close relay multipart body: %w", err)
	}
	if !foundModel {
		return nil, "", fmt.Errorf("%w: relay request model is required", apperrors.ErrInvalidArgument)
	}
	for _, field := range policy.requiredFileFields {
		if !fileFields[field] {
			return nil, "", fmt.Errorf("%w: relay multipart %s file is required", apperrors.ErrInvalidArgument, field)
		}
	}
	return out.Bytes(), writer.FormDataContentType(), nil
}

func cloneMIMEHeader(header textproto.MIMEHeader) textproto.MIMEHeader {
	out := make(textproto.MIMEHeader, len(header))
	for key, values := range header {
		out[key] = append([]string(nil), values...)
	}
	return out
}

func rewriteRelayRequestBody(body []byte, upstreamModel, providerKind string, stream, includeOpenAIUsage bool) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: invalid relay request JSON", apperrors.ErrInvalidArgument)
	}
	payload["model"] = upstreamModel
	if stream && includeOpenAIUsage && relayProviderUsesOpenAIWireProtocol(providerKind) {
		options, _ := payload["stream_options"].(map[string]any)
		if options == nil {
			options = map[string]any{}
		}
		options["include_usage"] = true
		payload["stream_options"] = options
	}
	rewritten, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal relay request body: %w", err)
	}
	return rewritten, nil
}

func (s *Service) selectRelayUpstream(ctx context.Context, providerKind, publicModel string) (relaySelection, error) {
	candidates, err := s.selectRelayUpstreamCandidates(ctx, providerKind, publicModel)
	if err != nil {
		return relaySelection{}, err
	}
	if len(candidates) == 0 {
		return relaySelection{}, fmt.Errorf("%w: no active relay route for model %s", apperrors.ErrNotFound, publicModel)
	}
	return candidates[0], nil
}

func (s *Service) selectRelayUpstreamCandidates(ctx context.Context, providerKind, publicModel string) ([]relaySelection, error) {
	return s.selectRelayUpstreamCandidatesForPrincipal(ctx, domainidentity.Principal{}, providerKind, publicModel)
}

func (s *Service) selectRelayUpstreamCandidatesForPrincipal(ctx context.Context, principal domainidentity.Principal, providerKind, publicModel string) ([]relaySelection, error) {
	repo := s.llmRelayRepository()
	if repo == nil {
		return nil, fmt.Errorf("%w: AI Gateway relay repository is not configured", apperrors.ErrInvalidArgument)
	}
	requestProvider := normalizeRelayProviderKind(providerKind)
	routes, err := repo.ListLLMModelRoutes(ctx, domainaigateway.LLMModelRouteFilter{
		PublicModel: publicModel,
	})
	if err != nil {
		return nil, err
	}
	filteredRoutes := make([]domainaigateway.LLMModelRoute, 0, len(routes))
	for _, route := range routes {
		if !route.Enabled || !relayRouteMatchesRequestProvider(route, requestProvider) {
			continue
		}
		if !relayMetadataTeamPolicyAllows(principal.Teams, route.Metadata) {
			continue
		}
		filteredRoutes = append(filteredRoutes, route)
	}
	sort.SliceStable(filteredRoutes, func(i, j int) bool {
		if filteredRoutes[i].Priority != filteredRoutes[j].Priority {
			return filteredRoutes[i].Priority < filteredRoutes[j].Priority
		}
		return filteredRoutes[i].ID < filteredRoutes[j].ID
	})
	candidates := make([]relaySelection, 0, len(filteredRoutes))
	for _, route := range filteredRoutes {
		plan := relayTransformPlanForRoute(route, requestProvider)
		upstreamProvider := requestProvider
		if plan.enabled {
			upstreamProvider = plan.upstreamProvider
		}
		upstreams, err := s.routeUpstreamCandidates(ctx, route, upstreamProvider)
		if err != nil {
			continue
		}
		for _, upstream := range upstreams {
			if !relayMetadataTeamPolicyAllows(principal.Teams, upstream.Metadata) {
				continue
			}
			if !relayUpstreamSupportsModel(upstream, route.UpstreamModel) {
				continue
			}
			candidates = append(candidates, relaySelection{route: route, upstream: upstream})
		}
	}
	if len(candidates) == 0 {
		return nil, fmt.Errorf("%w: no active relay route for model %s", apperrors.ErrNotFound, publicModel)
	}
	return relayWeightedSelectionOrder(candidates), nil
}

func relayRequestedUpstreamID(req LLMRelayHTTPRequest) string {
	return strings.TrimSpace(relayHeaderValue(req.Headers, relayHeaderUpstreamID))
}

func filterRelaySelectionsByUpstream(selections []relaySelection, upstreamID string) []relaySelection {
	upstreamID = strings.TrimSpace(upstreamID)
	if upstreamID == "" {
		return selections
	}
	out := make([]relaySelection, 0, len(selections))
	for _, selection := range selections {
		if selection.upstream.ID == upstreamID {
			out = append(out, selection)
		}
	}
	return out
}

func relayMetadataTeamPolicyAllows(principalTeams []string, metadata map[string]any) bool {
	allowed, denied, err := relayMetadataTeamPolicy(metadata)
	if err != nil {
		return false
	}
	return relayTeamPolicyAllows(principalTeams, allowed, denied)
}

func relayMetadataTeamPolicy(metadata map[string]any) ([]string, []string, error) {
	if len(metadata) == 0 {
		return nil, nil, nil
	}
	values := gatewayConditionValues(
		metadata,
		"teamPolicy",
		"team_policy",
		"tenantPolicy",
		"tenant_policy",
		"accessPolicy",
		"access_policy",
	)
	allowed, err := relayMetadataTeamList(values, "allowedTeams", "allowedTeamIds", "teamIds", "teams", "organizations", "orgs")
	if err != nil {
		return nil, nil, err
	}
	denied, err := relayMetadataTeamList(values, "deniedTeams", "deniedTeamIds", "blockedTeams", "blockedTeamIds", "excludedTeams", "excludedTeamIds")
	if err != nil {
		return nil, nil, err
	}
	return allowed, denied, nil
}

func relayMetadataTeamList(values map[string]any, keys ...string) ([]string, error) {
	out := make([]string, 0)
	for _, key := range keys {
		items, err := metadataStringList(values, key, false)
		if err != nil {
			return nil, err
		}
		out = append(out, items...)
	}
	return normalizeStringSlice(out), nil
}

func (s *Service) relayRouteUpstreamTeamPolicyAllows(ctx context.Context, principalTeams []string, route domainaigateway.LLMModelRoute, providerKind string) bool {
	upstreamID := strings.TrimSpace(route.UpstreamID)
	if upstreamID == "" {
		return true
	}
	repo := s.llmRelayRepository()
	if repo == nil {
		return true
	}
	upstream, err := repo.GetLLMUpstream(ctx, upstreamID)
	if err != nil {
		return true
	}
	requestProvider := normalizeRelayProviderKind(providerKind)
	plan := relayTransformPlanForRoute(route, requestProvider)
	upstreamProvider := requestProvider
	if plan.enabled {
		upstreamProvider = plan.upstreamProvider
	}
	if !relayUpstreamActiveForProvider(upstream, upstreamProvider) {
		return true
	}
	return relayMetadataTeamPolicyAllows(principalTeams, upstream.Metadata)
}

func (s *Service) authorizeRelayExplicitUpstream(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, upstreamID string) error {
	upstreamID = strings.TrimSpace(upstreamID)
	if upstreamID == "" {
		return nil
	}
	hasManage, err := s.hasRuntimePermission(ctx, principal, appaccess.PermAIGatewayRelayManage)
	if err != nil {
		return err
	}
	if hasManage {
		return nil
	}
	if relayExplicitUpstreamAllowedByToken(accessCtx.Metadata, upstreamID) {
		return nil
	}
	return fmt.Errorf("%w: explicit relay upstream selection is not allowed", apperrors.ErrAccessDenied)
}

func (s *Service) authorizeRelayRouteTrace(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest) error {
	if !relayRouteTraceRequested(req) {
		return nil
	}
	hasManage, err := s.hasRuntimePermission(ctx, principal, appaccess.PermAIGatewayRelayManage)
	if err != nil {
		return err
	}
	if hasManage || relayDebugHeadersAllowedByToken(accessCtx.Metadata) {
		return nil
	}
	return fmt.Errorf("%w: relay route trace headers are not allowed", apperrors.ErrAccessDenied)
}

func relayExplicitUpstreamAllowedByToken(metadata map[string]any, upstreamID string) bool {
	upstreamID = strings.TrimSpace(upstreamID)
	if upstreamID == "" {
		return false
	}
	if !metadataBool(metadata, "allowUpstreamSelection") {
		return false
	}
	allowed, err := metadataStringList(metadata, "allowedUpstreamIds", false)
	if err != nil || len(allowed) == 0 {
		return false
	}
	return slices.Contains(allowed, upstreamID)
}

func relayDebugHeadersAllowedByToken(metadata map[string]any) bool {
	return metadataBool(metadata, "allowRouteTrace")
}

func relayRouteProviderMatches(routeProvider, providerKind string) bool {
	routeProvider = normalizeRelayProviderKind(routeProvider)
	providerKind = normalizeRelayProviderKind(providerKind)
	if routeProvider == "" {
		return true
	}
	if providerKind == "openai" {
		return routeProvider == "openai" || routeProvider == "openai-compatible"
	}
	return routeProvider == providerKind
}

func relayRouteMatchesRequestProvider(route domainaigateway.LLMModelRoute, requestProvider string) bool {
	plan := relayTransformPlanForRoute(route, requestProvider)
	if !plan.enabled {
		return relayRouteProviderMatches(route.ProviderKind, requestProvider)
	}
	return true
}

func (s *Service) routeUpstream(ctx context.Context, route domainaigateway.LLMModelRoute, providerKind string) (domainaigateway.LLMUpstream, error) {
	upstreams, err := s.routeUpstreamCandidates(ctx, route, providerKind)
	if err != nil {
		return domainaigateway.LLMUpstream{}, err
	}
	if len(upstreams) == 0 {
		return domainaigateway.LLMUpstream{}, fmt.Errorf("%w: no active relay upstream for provider %s", apperrors.ErrNotFound, providerKind)
	}
	return upstreams[0], nil
}

func (s *Service) routeUpstreamCandidates(ctx context.Context, route domainaigateway.LLMModelRoute, providerKind string) ([]domainaigateway.LLMUpstream, error) {
	repo := s.llmRelayRepository()
	if strings.TrimSpace(route.UpstreamID) != "" {
		upstream, err := repo.GetLLMUpstream(ctx, route.UpstreamID)
		if err != nil {
			return nil, err
		}
		if relayUpstreamActiveForProvider(upstream, providerKind) && !relayUpstreamCircuitOpen(upstream, time.Now().UTC()) {
			return []domainaigateway.LLMUpstream{upstream}, nil
		}
		return nil, fmt.Errorf("%w: relay upstream is not active", apperrors.ErrNotFound)
	}
	upstreams, err := repo.ListLLMUpstreams(ctx, domainaigateway.LLMUpstreamFilter{})
	if err != nil {
		return nil, err
	}
	sort.SliceStable(upstreams, func(i, j int) bool {
		if upstreams[i].Priority != upstreams[j].Priority {
			return upstreams[i].Priority < upstreams[j].Priority
		}
		return upstreams[i].ID < upstreams[j].ID
	})
	out := make([]domainaigateway.LLMUpstream, 0, len(upstreams))
	now := time.Now().UTC()
	for _, upstream := range upstreams {
		if relayUpstreamActiveForProvider(upstream, providerKind) && !relayUpstreamCircuitOpen(upstream, now) {
			out = append(out, upstream)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: no active relay upstream for provider %s", apperrors.ErrNotFound, providerKind)
	}
	return relayWeightedUpstreamOrder(out), nil
}

func relayUpstreamActiveForProvider(upstream domainaigateway.LLMUpstream, providerKind string) bool {
	if !strings.EqualFold(strings.TrimSpace(upstream.Status), "active") {
		return false
	}
	upstreamProvider := normalizeRelayProviderKind(upstream.ProviderKind)
	if providerKind == "openai" {
		return upstreamProvider == "openai" || upstreamProvider == "openai-compatible"
	}
	return upstreamProvider == providerKind
}

func relayUpstreamSupportsModel(upstream domainaigateway.LLMUpstream, model string) bool {
	if len(upstream.SupportedModels) == 0 {
		return true
	}
	return containsFold(upstream.SupportedModels, model)
}

type relayTransformPlan struct {
	enabled          bool
	requestProvider  string
	requestEndpoint  string
	upstreamProvider string
	upstreamEndpoint string
}

func relayTransformPlanForRoute(route domainaigateway.LLMModelRoute, requestProvider string) relayTransformPlan {
	requestProvider = normalizeRelayProviderKind(requestProvider)
	if requestProvider == "" {
		return relayTransformPlan{}
	}
	values := gatewayConditionValues(route.TransformPolicy, "transform", "conversion", "formatConversion", "format_conversion")
	mode := strings.ToLower(gatewayFirstString(values, "mode", "type", "strategy"))
	if mode == "" {
		if enabled, ok := gatewayConditionRaw(values, "enabled"); !ok || !boolFromAny(enabled) {
			return relayTransformPlan{}
		}
	}
	if mode == "passthrough" || mode == "native" || mode == "none" || mode == "off" {
		return relayTransformPlan{}
	}
	targetProvider := normalizeRelayProviderKind(gatewayFirstString(values, "targetProviderKind", "targetProvider", "target", "upstreamProviderKind", "upstreamProvider", "providerKind"))
	if targetProvider == "" {
		routeProvider := normalizeRelayProviderKind(route.ProviderKind)
		if routeProvider != "" && !relayRouteProviderMatches(routeProvider, requestProvider) {
			targetProvider = routeProvider
		}
	}
	if targetProvider == "" || targetProvider == requestProvider {
		return relayTransformPlan{}
	}
	if targetProvider == "openai-compatible" {
		targetProvider = "openai"
	}
	switch requestProvider + "->" + targetProvider {
	case "openai->anthropic":
		return relayTransformPlan{
			enabled:          true,
			requestProvider:  "openai",
			requestEndpoint:  "chat/completions",
			upstreamProvider: "anthropic",
			upstreamEndpoint: "messages",
		}
	case "anthropic->openai":
		return relayTransformPlan{
			enabled:          true,
			requestProvider:  "anthropic",
			requestEndpoint:  "messages",
			upstreamProvider: "openai",
			upstreamEndpoint: "chat/completions",
		}
	default:
		return relayTransformPlan{}
	}
}

func relayRequestTransformPlan(req LLMRelayHTTPRequest, selection relaySelection, stream bool) (relayTransformPlan, error) {
	plan := relayTransformPlanForRoute(selection.route, req.ProviderKind)
	if !plan.enabled {
		return relayTransformPlan{}, nil
	}
	if stream {
		return relayTransformPlan{}, fmt.Errorf("%w: relay format conversion only supports non-streaming requests", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(req.Endpoint) != plan.requestEndpoint {
		return relayTransformPlan{}, fmt.Errorf("%w: relay format conversion does not support endpoint %s", apperrors.ErrInvalidArgument, req.Endpoint)
	}
	if upstreamProvider := selection.upstreamProviderKind(); upstreamProvider != "" && !relayRouteProviderMatches(upstreamProvider, plan.upstreamProvider) {
		return relayTransformPlan{}, fmt.Errorf("%w: relay transform upstream provider mismatch", apperrors.ErrInvalidArgument)
	}
	return plan, nil
}

func relayTransformUpstreamRequest(req LLMRelayHTTPRequest, selection relaySelection, stream bool, includeOpenAIUsage bool) ([]byte, string, string, string, error) {
	plan, err := relayRequestTransformPlan(req, selection, stream)
	if err != nil {
		return nil, "", "", "", err
	}
	if !plan.enabled {
		if normalizeRelayProviderKind(req.ProviderKind) == "gemini" {
			body, err := relayGeminiRequestBody(req, selection.route.UpstreamModel)
			return body, "gemini", strings.TrimSpace(req.Endpoint), "", err
		}
		if relayEndpointRequiresMultipart(req.Endpoint) {
			body, contentType, err := rewriteRelayMultipartRequestBody(
				req.Endpoint,
				req.Body,
				req.Headers.Get("Content-Type"),
				selection.route.UpstreamModel,
			)
			return body, normalizeRelayProviderKind(req.ProviderKind), strings.TrimSpace(req.Endpoint), contentType, err
		}
		body, err := rewriteRelayRequestBody(req.Body, selection.route.UpstreamModel, req.ProviderKind, stream, includeOpenAIUsage)
		return body, normalizeRelayProviderKind(req.ProviderKind), strings.TrimSpace(req.Endpoint), "", err
	}
	body, err := relayTransformRequestBody(req.Body, selection.route.UpstreamModel, plan)
	if err != nil {
		return nil, "", "", "", err
	}
	return body, plan.upstreamProvider, plan.upstreamEndpoint, "", nil
}

func relayGeminiRequestBody(req LLMRelayHTTPRequest, upstreamModel string) ([]byte, error) {
	var payload any
	if err := json.Unmarshal(req.Body, &payload); err != nil {
		return nil, fmt.Errorf("%w: invalid relay request JSON", apperrors.ErrInvalidArgument)
	}
	if strings.TrimSpace(req.Endpoint) == "interactions" {
		return rewriteRelayRequestBody(req.Body, upstreamModel, "gemini", false, false)
	}
	return req.Body, nil
}

func relayTransformRequestBody(body []byte, upstreamModel string, plan relayTransformPlan) ([]byte, error) {
	switch plan.requestProvider + "->" + plan.upstreamProvider {
	case "openai->anthropic":
		return relayTransformOpenAIChatRequestToAnthropic(body, upstreamModel)
	case "anthropic->openai":
		return relayTransformAnthropicMessagesRequestToOpenAI(body, upstreamModel)
	default:
		return nil, fmt.Errorf("%w: unsupported relay format conversion", apperrors.ErrInvalidArgument)
	}
}

func relayTransformResponseBody(body []byte, plan relayTransformPlan) ([]byte, error) {
	if !plan.enabled {
		return body, nil
	}
	switch plan.requestProvider + "->" + plan.upstreamProvider {
	case "openai->anthropic":
		return relayTransformAnthropicMessageResponseToOpenAI(body)
	case "anthropic->openai":
		return relayTransformOpenAIChatResponseToAnthropic(body)
	default:
		return nil, fmt.Errorf("%w: unsupported relay format conversion", apperrors.ErrInvalidArgument)
	}
}

func relayTransformOpenAIChatRequestToAnthropic(body []byte, upstreamModel string) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: invalid relay request JSON", apperrors.ErrInvalidArgument)
	}
	if relayJSONContainsCacheUnsafeValue(payload) {
		return nil, fmt.Errorf("%w: relay format conversion only supports text-only requests", apperrors.ErrInvalidArgument)
	}
	messages, ok := payload["messages"].([]any)
	if !ok {
		return nil, fmt.Errorf("%w: openai messages are required for relay format conversion", apperrors.ErrInvalidArgument)
	}
	out := map[string]any{
		"model":      upstreamModel,
		"messages":   []any{},
		"max_tokens": relayOpenAIMaxTokens(payload),
	}
	if system := relayOpenAISystemPrompt(messages); system != "" {
		out["system"] = system
	}
	converted := make([]any, 0, len(messages))
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: relay format conversion only supports text messages", apperrors.ErrInvalidArgument)
		}
		role := strings.ToLower(strings.TrimSpace(fmt.Sprint(message["role"])))
		if role == "system" {
			continue
		}
		if role != "user" && role != "assistant" {
			return nil, fmt.Errorf("%w: relay format conversion only supports user and assistant messages", apperrors.ErrInvalidArgument)
		}
		text, ok := relayTextFromContent(message["content"])
		if !ok {
			return nil, fmt.Errorf("%w: relay format conversion only supports text message content", apperrors.ErrInvalidArgument)
		}
		converted = append(converted, map[string]any{"role": role, "content": text})
	}
	out["messages"] = converted
	if temperature, ok := payload["temperature"]; ok {
		out["temperature"] = temperature
	}
	if topP, ok := payload["top_p"]; ok {
		out["top_p"] = topP
	}
	return json.Marshal(out)
}

func relayTransformAnthropicMessagesRequestToOpenAI(body []byte, upstreamModel string) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("%w: invalid relay request JSON", apperrors.ErrInvalidArgument)
	}
	if relayJSONContainsCacheUnsafeValue(payload) {
		return nil, fmt.Errorf("%w: relay format conversion only supports text-only requests", apperrors.ErrInvalidArgument)
	}
	messages, ok := payload["messages"].([]any)
	if !ok {
		return nil, fmt.Errorf("%w: anthropic messages are required for relay format conversion", apperrors.ErrInvalidArgument)
	}
	converted := make([]any, 0, len(messages)+1)
	if system, ok := relayTextFromContent(payload["system"]); ok && system != "" {
		converted = append(converted, map[string]any{"role": "system", "content": system})
	}
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%w: relay format conversion only supports text messages", apperrors.ErrInvalidArgument)
		}
		role := strings.ToLower(strings.TrimSpace(fmt.Sprint(message["role"])))
		if role != "user" && role != "assistant" {
			return nil, fmt.Errorf("%w: relay format conversion only supports user and assistant messages", apperrors.ErrInvalidArgument)
		}
		text, ok := relayTextFromContent(message["content"])
		if !ok {
			return nil, fmt.Errorf("%w: relay format conversion only supports text message content", apperrors.ErrInvalidArgument)
		}
		converted = append(converted, map[string]any{"role": role, "content": text})
	}
	out := map[string]any{
		"model":    upstreamModel,
		"messages": converted,
	}
	if maxTokens := jsonNumberInt(payload["max_tokens"]); maxTokens > 0 {
		out["max_tokens"] = maxTokens
	}
	if temperature, ok := payload["temperature"]; ok {
		out["temperature"] = temperature
	}
	if topP, ok := payload["top_p"]; ok {
		out["top_p"] = topP
	}
	return json.Marshal(out)
}

func relayTransformAnthropicMessageResponseToOpenAI(body []byte) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode relay transform response: %w", err)
	}
	contentText, ok := relayTextFromContent(payload["content"])
	if !ok {
		return nil, fmt.Errorf("%w: relay format conversion only supports text response content", apperrors.ErrInvalidArgument)
	}
	out := map[string]any{
		"id":      firstNonEmpty(strings.TrimSpace(fmt.Sprint(payload["id"])), "chatcmpl-"+uuid.NewString()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   strings.TrimSpace(fmt.Sprint(payload["model"])),
		"choices": []any{
			map[string]any{
				"index": 0,
				"message": map[string]any{
					"role":    "assistant",
					"content": contentText,
				},
				"finish_reason": relayOpenAIFinishReasonFromAnthropic(payload["stop_reason"]),
			},
		},
	}
	if usage, ok := payload["usage"].(map[string]any); ok {
		promptTokens := jsonNumberInt(usage["input_tokens"])
		completionTokens := jsonNumberInt(usage["output_tokens"])
		out["usage"] = map[string]any{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      promptTokens + completionTokens,
		}
	}
	return json.Marshal(out)
}

func relayTransformOpenAIChatResponseToAnthropic(body []byte) ([]byte, error) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("decode relay transform response: %w", err)
	}
	choices, _ := payload["choices"].([]any)
	contentText := ""
	stopReason := "end_turn"
	if len(choices) > 0 {
		choice, _ := choices[0].(map[string]any)
		if reason := strings.TrimSpace(fmt.Sprint(choice["finish_reason"])); reason != "" && reason != "<nil>" {
			stopReason = relayAnthropicStopReasonFromOpenAI(reason)
		}
		if message, ok := choice["message"].(map[string]any); ok {
			text, ok := relayTextFromContent(message["content"])
			if !ok {
				return nil, fmt.Errorf("%w: relay format conversion only supports text response content", apperrors.ErrInvalidArgument)
			}
			contentText = text
		}
	}
	out := map[string]any{
		"id":            firstNonEmpty(strings.TrimSpace(fmt.Sprint(payload["id"])), "msg_"+uuid.NewString()),
		"type":          "message",
		"role":          "assistant",
		"model":         strings.TrimSpace(fmt.Sprint(payload["model"])),
		"content":       []any{map[string]any{"type": "text", "text": contentText}},
		"stop_reason":   stopReason,
		"stop_sequence": nil,
	}
	if usage, ok := payload["usage"].(map[string]any); ok {
		out["usage"] = map[string]any{
			"input_tokens":  jsonNumberInt(usage["prompt_tokens"]),
			"output_tokens": jsonNumberInt(usage["completion_tokens"]),
		}
	}
	return json.Marshal(out)
}

func relayTextFromContent(value any) (string, bool) {
	switch typed := value.(type) {
	case nil:
		return "", true
	case string:
		return typed, true
	case []any:
		var builder strings.Builder
		for _, item := range typed {
			part, ok := relayTextFromContentBlock(item)
			if !ok {
				return "", false
			}
			builder.WriteString(part)
		}
		return builder.String(), true
	default:
		return "", false
	}
}

func relayTextFromContentBlock(value any) (string, bool) {
	block, ok := value.(map[string]any)
	if !ok {
		return "", false
	}
	blockType := strings.ToLower(strings.TrimSpace(fmt.Sprint(block["type"])))
	switch blockType {
	case "", "text", "input_text", "output_text":
		text, ok := block["text"].(string)
		return text, ok
	default:
		return "", false
	}
}

func relayOpenAISystemPrompt(messages []any) string {
	var parts []string
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(fmt.Sprint(message["role"])), "system") {
			if text, ok := relayTextFromContent(message["content"]); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n\n")
}

func relayOpenAIMaxTokens(payload map[string]any) int {
	value := firstNonZeroInt(jsonNumberInt(payload["max_tokens"]), jsonNumberInt(payload["max_completion_tokens"]))
	if value <= 0 {
		return 1024
	}
	return value
}

func relayOpenAIFinishReasonFromAnthropic(value any) string {
	switch strings.ToLower(strings.TrimSpace(fmt.Sprint(value))) {
	case "max_tokens":
		return "length"
	case "stop_sequence":
		return "stop"
	case "tool_use":
		return "tool_calls"
	default:
		return "stop"
	}
}

func relayAnthropicStopReasonFromOpenAI(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "length":
		return "max_tokens"
	case "tool_calls", "function_call":
		return "tool_use"
	default:
		return "end_turn"
	}
}

func relayWeightedSelectionOrder(items []relaySelection) []relaySelection {
	if len(items) <= 1 {
		return append([]relaySelection(nil), items...)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].route.Priority != items[j].route.Priority {
			return items[i].route.Priority < items[j].route.Priority
		}
		if items[i].upstream.Priority != items[j].upstream.Priority {
			return items[i].upstream.Priority < items[j].upstream.Priority
		}
		if items[i].route.ID != items[j].route.ID {
			return items[i].route.ID < items[j].route.ID
		}
		return items[i].upstream.ID < items[j].upstream.ID
	})
	out := make([]relaySelection, 0, len(items))
	for start := 0; start < len(items); {
		end := start + 1
		for end < len(items) && items[end].route.Priority == items[start].route.Priority && items[end].upstream.Priority == items[start].upstream.Priority {
			end++
		}
		out = append(out, relayWeightedSelectionBucket(items[start:end])...)
		start = end
	}
	return out
}

func relayWeightedSelectionBucket(items []relaySelection) []relaySelection {
	remaining := append([]relaySelection(nil), items...)
	out := make([]relaySelection, 0, len(remaining))
	for len(remaining) > 0 {
		index := relayWeightedSelectionIndex(remaining)
		out = append(out, remaining[index])
		remaining = append(remaining[:index], remaining[index+1:]...)
	}
	return out
}

func relayWeightedSelectionIndex(items []relaySelection) int {
	total := 0
	for _, item := range items {
		total += relaySelectionWeight(item)
	}
	if total <= 0 {
		return 0
	}
	pick := relayRandomIntn(total)
	for index, item := range items {
		weight := relaySelectionWeight(item)
		if pick < weight {
			return index
		}
		pick -= weight
	}
	return len(items) - 1
}

func relaySelectionWeight(item relaySelection) int {
	routeWeight := item.route.Weight
	if routeWeight <= 0 {
		routeWeight = 1
	}
	upstreamWeight := item.upstream.Weight
	if upstreamWeight <= 0 {
		upstreamWeight = 1
	}
	return routeWeight * upstreamWeight
}

func relayWeightedUpstreamOrder(items []domainaigateway.LLMUpstream) []domainaigateway.LLMUpstream {
	if len(items) <= 1 {
		return append([]domainaigateway.LLMUpstream(nil), items...)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Priority != items[j].Priority {
			return items[i].Priority < items[j].Priority
		}
		return items[i].ID < items[j].ID
	})
	out := make([]domainaigateway.LLMUpstream, 0, len(items))
	for start := 0; start < len(items); {
		end := start + 1
		for end < len(items) && items[end].Priority == items[start].Priority {
			end++
		}
		out = append(out, relayWeightedUpstreamBucket(items[start:end])...)
		start = end
	}
	return out
}

func relayWeightedUpstreamBucket(items []domainaigateway.LLMUpstream) []domainaigateway.LLMUpstream {
	remaining := append([]domainaigateway.LLMUpstream(nil), items...)
	out := make([]domainaigateway.LLMUpstream, 0, len(remaining))
	for len(remaining) > 0 {
		index := relayWeightedUpstreamIndex(remaining)
		out = append(out, remaining[index])
		remaining = append(remaining[:index], remaining[index+1:]...)
	}
	return out
}

func relayWeightedUpstreamIndex(items []domainaigateway.LLMUpstream) int {
	total := 0
	for _, item := range items {
		total += relayPositiveWeight(item.Weight)
	}
	if total <= 0 {
		return 0
	}
	pick := relayRandomIntn(total)
	for index, item := range items {
		weight := relayPositiveWeight(item.Weight)
		if pick < weight {
			return index
		}
		pick -= weight
	}
	return len(items) - 1
}

func relayPositiveWeight(weight int) int {
	if weight <= 0 {
		return 1
	}
	return weight
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

func (s *Service) proxyRelayRequestWithFallback(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selections []relaySelection, publicModel string, stream bool, writer http.ResponseWriter) error {
	var lastErr error
	attempts := relayFallbackMaxAttempts(selections)
	for index, selection := range selections {
		if attempts > 0 && index >= attempts {
			break
		}
		if err := s.enforceRelayRateLimits(ctx, principal, accessCtx, req, selection, publicModel, stream); err != nil {
			return err
		}
		wrote, retryable, err := s.proxyRelayRequest(ctx, principal, accessCtx, req, selection, publicModel, stream, writer)
		if err == nil || wrote || !retryable || index == len(selections)-1 || ctx.Err() != nil {
			return err
		}
		lastErr = err
	}
	return lastErr
}

func (s *Service) proxyRelayWebSocketWithFallback(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selections []relaySelection, publicModel string, writer http.ResponseWriter, clientRequest *http.Request) error {
	var lastErr error
	attempts := relayFallbackMaxAttempts(selections)
	for index, selection := range selections {
		if attempts > 0 && index >= attempts {
			break
		}
		if err := s.enforceRelayRateLimits(ctx, principal, accessCtx, req, selection, publicModel, true); err != nil {
			return err
		}
		upgraded, retryable, err := s.proxyRelayWebSocket(ctx, principal, accessCtx, req, selection, publicModel, writer, clientRequest)
		if err == nil || upgraded || !retryable || index == len(selections)-1 || ctx.Err() != nil {
			return err
		}
		lastErr = err
	}
	return lastErr
}

func (s *Service) proxyRelayWebSocket(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selection relaySelection, publicModel string, writer http.ResponseWriter, clientRequest *http.Request) (bool, bool, error) {
	started := time.Now().UTC()
	release, acquired := s.tryAcquireRelayUpstreamConcurrency(selection.upstream)
	if !acquired {
		s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, true, domainaigateway.LLMCallLog{
			Status:               "rate_limited",
			HTTPStatus:           http.StatusTooManyRequests,
			ErrorCode:            "upstream_concurrency_limited",
			ErrorMessage:         "relay upstream concurrency limit exceeded",
			CacheStatus:          relayCacheBypass,
			DurationMilliseconds: time.Since(started).Milliseconds(),
			CreatedAt:            started,
		})
		return false, true, fmt.Errorf("%w: relay upstream concurrency limit exceeded", apperrors.ErrAccessDenied)
	}
	defer release()

	targetURL, err := relayRealtimeEndpointURLForSelection(selection, publicModel)
	if err != nil {
		return false, false, err
	}
	if err := s.validateRelayWebSocketUpstreamURL(targetURL); err != nil {
		return false, false, err
	}
	apiKey, err := s.decryptRelayAPIKey(selection.upstream.APIKeyCiphertext)
	if err != nil {
		return false, false, err
	}
	if strings.TrimSpace(apiKey) == "" {
		return false, false, fmt.Errorf("%w: relay upstream API key is not configured", apperrors.ErrInvalidArgument)
	}

	timeout := s.relayConfig.StreamTimeout
	if timeout <= 0 {
		timeout = s.relayConfig.DefaultTimeout
	}
	dialCtx, cancelDial := context.WithTimeout(ctx, timeout)
	upstreamStarted := time.Now().UTC()
	upstreamConn, upstreamResp, err := relayWebSocketDialer().DialContext(dialCtx, targetURL, relayRealtimeUpstreamHeaders(req, selection.upstream, apiKey))
	cancelDial()
	if err != nil {
		status := http.StatusBadGateway
		errorCode := "upstream_request_failed"
		if upstreamResp != nil {
			status = upstreamResp.StatusCode
			errorCode = relayErrorCodeForStatus(upstreamResp.StatusCode)
			if errorCode == "" {
				errorCode = "upstream_ws_failed"
			}
		}
		s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, true, domainaigateway.LLMCallLog{
			Status:               relayStatusFromError(ctx, err),
			HTTPStatus:           status,
			UpstreamStatus:       status,
			ErrorCode:            errorCode,
			ErrorMessage:         redactRelayText(err.Error()),
			CacheStatus:          relayCacheBypass,
			DurationMilliseconds: time.Since(started).Milliseconds(),
			CreatedAt:            started,
		})
		s.recordRelayUpstreamFailure(ctx, principal, selection, errorCode)
		return false, true, fmt.Errorf("%w: relay realtime upstream connection failed", apperrors.ErrClusterUnready)
	}
	defer upstreamConn.Close()

	responseHeader := http.Header{}
	if upstreamResp != nil {
		copyRelayResponseHeaders(responseHeader, upstreamResp.Header, "application/octet-stream")
	}
	writeRelayRouteTraceHeaders(responseHeader, req, selection, publicModel, true, http.StatusSwitchingProtocols, relayCacheBypass)
	upgrader := relayWebSocketUpgrader()
	clientConn, err := upgrader.Upgrade(writer, clientRequest, responseHeader)
	if err != nil {
		s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, true, domainaigateway.LLMCallLog{
			Status:               "client_cancelled",
			HTTPStatus:           http.StatusBadRequest,
			UpstreamStatus:       http.StatusSwitchingProtocols,
			ErrorCode:            "client_upgrade_failed",
			ErrorMessage:         redactRelayText(err.Error()),
			CacheStatus:          relayCacheBypass,
			DurationMilliseconds: time.Since(started).Milliseconds(),
			CreatedAt:            started,
		})
		return false, false, nil
	}
	defer clientConn.Close()

	firstByteAt, clientBytes, upstreamBytes, bridgeErr := relayProxyWebSocketMessages(ctx, clientConn, upstreamConn)
	status := "success"
	errorCode := ""
	errorMessage := ""
	if bridgeErr != nil {
		status = "failure"
		errorCode = "realtime_ws_closed"
		if errors.Is(bridgeErr, context.Canceled) || relayWebSocketCloseError(bridgeErr) {
			status = "client_cancelled"
			errorCode = "client_cancelled"
		}
		errorMessage = bridgeErr.Error()
	}
	s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, true, domainaigateway.LLMCallLog{
		Status:               status,
		HTTPStatus:           http.StatusSwitchingProtocols,
		UpstreamStatus:       http.StatusSwitchingProtocols,
		ErrorCode:            errorCode,
		ErrorMessage:         redactRelayText(errorMessage),
		InputBytes:           clientBytes,
		OutputBytes:          upstreamBytes,
		CacheStatus:          relayCacheBypass,
		DurationMilliseconds: time.Since(started).Milliseconds(),
		CreatedAt:            started,
		TTFBMilliseconds:     relayDurationMilliseconds(started, firstByteAt),
		TTFTMilliseconds:     relayDurationMilliseconds(started, firstByteAt),
		Metadata: map[string]any{
			"upstreamConnectedAt": upstreamStarted.Format(time.RFC3339Nano),
			"transport":           "websocket",
		},
	})
	if status == "success" {
		s.recordRelayUpstreamSuccess(ctx, principal, selection)
	}
	return true, false, nil
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

func relayFallbackMaxAttempts(selections []relaySelection) int {
	if len(selections) == 0 {
		return 0
	}
	values := selections[0].route.FallbackPolicy
	attempts := firstNonZeroInt(
		intFromAny(values["maxAttempts"]),
		intFromAny(values["max_attempts"]),
		intFromAny(values["attempts"]),
	)
	if attempts <= 0 {
		return 0
	}
	if attempts > len(selections) {
		return len(selections)
	}
	return attempts
}

func (s *Service) tryAcquireRelayTokenConcurrency(accessCtx domainidentity.AccessContext, stream bool) (func(), string, string, bool) {
	tokenKey := relayRateLimitTokenClientID(accessCtx)
	releases := make([]func(), 0, 2)
	if limit := relayTokenConcurrencyLimit(accessCtx.Metadata); limit > 0 {
		release, ok := s.tryAcquireRelayConcurrency(tokenKey+":requests", limit)
		if !ok {
			return func() {}, "token_concurrency_limited", "relay token concurrency limit exceeded", false
		}
		releases = append(releases, release)
	}
	if stream {
		if limit := relayTokenStreamConcurrencyLimit(accessCtx.Metadata); limit > 0 {
			release, ok := s.tryAcquireRelayConcurrency(tokenKey+":streams", limit)
			if !ok {
				releaseRelayConcurrencyAll(releases)
				return func() {}, "token_stream_concurrency_limited", "relay token stream concurrency limit exceeded", false
			}
			releases = append(releases, release)
		}
	}
	return func() {
		releaseRelayConcurrencyAll(releases)
	}, "", "", true
}

func relayTokenConcurrencyLimit(metadata map[string]any) int {
	values := gatewayConditionValues(metadata, "concurrency", "concurrencyLimit", "concurrencyLimits", "limits")
	limit, _ := gatewayFirstPositiveInt(values,
		"maxConcurrentRequests",
		"maxConcurrency",
		"concurrentRequests",
		"concurrency",
		"requestConcurrency",
		"maxParallelRequests",
	)
	return limit
}

func relayTokenStreamConcurrencyLimit(metadata map[string]any) int {
	values := gatewayConditionValues(metadata, "concurrency", "concurrencyLimit", "concurrencyLimits", "streamConcurrency", "streamConcurrencyLimit", "limits")
	limit, _ := gatewayFirstPositiveInt(values,
		"maxConcurrentStreamingRequests",
		"maxConcurrentStreams",
		"maxStreamConcurrency",
		"streamConcurrency",
		"concurrentStreams",
		"streamingConcurrency",
	)
	return limit
}

func releaseRelayConcurrencyAll(releases []func()) {
	for i := len(releases) - 1; i >= 0; i-- {
		if releases[i] != nil {
			releases[i]()
		}
	}
}

func (s *Service) tryAcquireRelayUpstreamConcurrency(upstream domainaigateway.LLMUpstream) (func(), bool) {
	limit := upstream.MaxConcurrency
	if s == nil || limit <= 0 {
		return func() {}, true
	}
	key := strings.TrimSpace(upstream.ID)
	if key == "" {
		key = strings.TrimSpace(upstream.Name)
	}
	if key == "" {
		return func() {}, true
	}
	return s.tryAcquireRelayConcurrency("upstream:"+key, limit)
}

func (s *Service) tryAcquireRelayConcurrency(key string, limit int) (func(), bool) {
	key = strings.TrimSpace(key)
	if s == nil || limit <= 0 || key == "" {
		return func() {}, true
	}
	s.relayConcurrencyMu.Lock()
	if s.relayConcurrency == nil {
		s.relayConcurrency = map[string]int{}
	}
	if s.relayConcurrency[key] >= limit {
		s.relayConcurrencyMu.Unlock()
		return func() {}, false
	}
	s.relayConcurrency[key]++
	s.relayConcurrencyMu.Unlock()
	released := false
	return func() {
		s.relayConcurrencyMu.Lock()
		defer s.relayConcurrencyMu.Unlock()
		if released {
			return
		}
		released = true
		if s.relayConcurrency[key] <= 1 {
			delete(s.relayConcurrency, key)
			return
		}
		s.relayConcurrency[key]--
	}, true
}

func (s *Service) proxyRelayRequest(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selection relaySelection, publicModel string, stream bool, writer http.ResponseWriter) (bool, bool, error) {
	started := time.Now().UTC()
	body, upstreamProvider, upstreamEndpoint, upstreamContentType, err := relayTransformUpstreamRequest(req, selection, stream, s.relayConfig.IncludeUsageForOpenAIStream)
	if err != nil {
		return false, false, err
	}
	cacheAttempt := s.relayResponseCacheAttempt(accessCtx, req, selection, publicModel, stream, body)
	if served, err := s.writeRelayCachedResponse(ctx, principal, accessCtx, req, selection, publicModel, stream, cacheAttempt, started, writer); served || err != nil {
		return served, false, err
	}
	cacheStatus := cacheAttempt.statusOnMiss()
	release, acquired := s.tryAcquireRelayUpstreamConcurrency(selection.upstream)
	if !acquired {
		s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, stream, domainaigateway.LLMCallLog{
			Status:               "rate_limited",
			HTTPStatus:           http.StatusTooManyRequests,
			ErrorCode:            "upstream_concurrency_limited",
			ErrorMessage:         "relay upstream concurrency limit exceeded",
			InputBytes:           int64(len(body)),
			CacheStatus:          cacheStatus,
			DurationMilliseconds: time.Since(started).Milliseconds(),
			CreatedAt:            started,
		})
		return false, true, fmt.Errorf("%w: relay upstream concurrency limit exceeded", apperrors.ErrAccessDenied)
	}
	defer release()
	targetURL, err := relayEndpointURLForSelection(selection, upstreamProvider, upstreamEndpoint)
	if err != nil {
		return false, false, err
	}
	if err := s.validateRelayUpstreamURL(targetURL); err != nil {
		return false, false, err
	}
	apiKey, err := s.decryptRelayAPIKey(selection.upstream.APIKeyCiphertext)
	if err != nil {
		return false, false, err
	}
	if strings.TrimSpace(apiKey) == "" {
		return false, false, fmt.Errorf("%w: relay upstream API key is not configured", apperrors.ErrInvalidArgument)
	}
	timeout := s.relayConfig.DefaultTimeout
	if stream {
		timeout = s.relayConfig.StreamTimeout
	}
	upstreamCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	upstreamReq, err := http.NewRequestWithContext(upstreamCtx, http.MethodPost, targetURL, bytes.NewReader(body))
	if err != nil {
		return false, false, fmt.Errorf("build relay upstream request: %w", err)
	}
	upstreamRelayReq := req
	upstreamRelayReq.ProviderKind = upstreamProvider
	upstreamRelayReq.Endpoint = upstreamEndpoint
	if upstreamContentType != "" {
		upstreamRelayReq.Headers = upstreamRelayReq.Headers.Clone()
		upstreamRelayReq.Headers.Set("Content-Type", upstreamContentType)
	}
	applyRelayUpstreamHeaders(upstreamReq.Header, upstreamRelayReq, selection.upstream, apiKey)
	upstreamStarted := time.Now().UTC()
	resp, err := s.httpClient.Do(upstreamReq)
	if err != nil {
		s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, stream, domainaigateway.LLMCallLog{
			Status:               relayStatusFromError(ctx, err),
			HTTPStatus:           http.StatusBadGateway,
			ErrorCode:            "upstream_request_failed",
			ErrorMessage:         redactRelayText(err.Error()),
			InputBytes:           int64(len(body)),
			CacheStatus:          cacheStatus,
			DurationMilliseconds: time.Since(started).Milliseconds(),
			CreatedAt:            started,
		})
		s.recordRelayUpstreamFailure(ctx, principal, selection, "upstream_request_failed")
		return false, true, fmt.Errorf("%w: relay upstream request failed", apperrors.ErrClusterUnready)
	}
	defer resp.Body.Close()
	if !stream && relayRetryableUpstreamStatus(resp.StatusCode) {
		var output bytes.Buffer
		_, _ = output.ReadFrom(io.LimitReader(resp.Body, 4*1024*1024))
		errorCode := relayErrorCodeForStatus(resp.StatusCode)
		s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, stream, relayCallLogFromResponse(req, resp, started, upstreamStarted, time.Time{}, output.Bytes(), int64(len(body)), "failure", errorCode, "retryable upstream response", cacheStatus))
		s.recordRelayUpstreamFailure(ctx, principal, selection, errorCode)
		return false, true, fmt.Errorf("%w: relay upstream returned retryable status %d", apperrors.ErrClusterUnready, resp.StatusCode)
	}
	if stream {
		copyRelayResponseHeaders(writer.Header(), resp.Header, relayDefaultResponseContentType(req.Endpoint))
		writeRelayRouteTraceHeaders(writer.Header(), req, selection, publicModel, stream, resp.StatusCode, cacheStatus)
		writer.WriteHeader(resp.StatusCode)
	}
	var output bytes.Buffer
	firstByteAt := time.Time{}
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if firstByteAt.IsZero() {
				firstByteAt = time.Now().UTC()
			}
			chunk := buf[:n]
			output.Write(chunk)
			if !stream {
				continue
			}
			if _, writeErr := writer.Write(chunk); writeErr != nil {
				s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, stream, relayCallLogFromResponse(req, resp, started, upstreamStarted, firstByteAt, output.Bytes(), int64(len(body)), "client_cancelled", "client_cancelled", writeErr.Error(), cacheStatus))
				return true, false, nil
			}
			if flusher, ok := writer.(http.Flusher); ok {
				flusher.Flush()
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, stream, relayCallLogFromResponse(req, resp, started, upstreamStarted, firstByteAt, output.Bytes(), int64(len(body)), "failure", "upstream_read_failed", readErr.Error(), cacheStatus))
			s.recordRelayUpstreamFailure(ctx, principal, selection, "upstream_read_failed")
			if stream {
				return !firstByteAt.IsZero(), false, nil
			}
			return false, true, fmt.Errorf("%w: relay upstream read failed", apperrors.ErrClusterUnready)
		}
	}
	status := "success"
	if resp.StatusCode >= 400 {
		status = "failure"
	}
	responseBody := output.Bytes()
	plan, transformPlanErr := relayRequestTransformPlan(req, selection, stream)
	if transformPlanErr != nil {
		return false, false, transformPlanErr
	}
	if status == "success" && plan.enabled {
		transformed, err := relayTransformResponseBody(responseBody, plan)
		if err != nil {
			s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, stream, relayCallLogFromResponse(req, resp, started, upstreamStarted, firstByteAt, responseBody, int64(len(body)), "failure", "relay_transform_failed", err.Error(), cacheStatus))
			return false, false, err
		}
		responseBody = transformed
	}
	if !stream {
		if status == "success" {
			cacheStatus = s.storeRelayResponseCache(ctx, cacheAttempt, resp, responseBody)
		}
		copyRelayResponseHeaders(writer.Header(), resp.Header, relayDefaultResponseContentType(req.Endpoint))
		writeRelayRouteTraceHeaders(writer.Header(), req, selection, publicModel, stream, resp.StatusCode, cacheStatus)
		writer.WriteHeader(resp.StatusCode)
		if _, writeErr := writer.Write(responseBody); writeErr != nil {
			s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, stream, relayCallLogFromResponse(req, resp, started, upstreamStarted, firstByteAt, responseBody, int64(len(body)), "client_cancelled", "client_cancelled", writeErr.Error(), cacheStatus))
			return true, false, nil
		}
	}
	s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, stream, relayCallLogFromResponse(req, resp, started, upstreamStarted, firstByteAt, responseBody, int64(len(body)), status, "", "", cacheStatus))
	if resp.StatusCode < http.StatusBadRequest {
		s.recordRelayUpstreamSuccess(ctx, principal, selection)
	}
	return true, false, nil
}

func relayEndpointURLForSelection(selection relaySelection, providerKind, endpoint string) (string, error) {
	return relayEndpointURLForUpstream(selection.upstream, selection.route, providerKind, endpoint)
}

func relayRealtimeEndpointURLForSelection(selection relaySelection, publicModel string) (string, error) {
	targetURL, err := relayEndpointURLForSelection(selection, "openai", "realtime")
	if err != nil {
		return "", err
	}
	parsed, err := url.Parse(targetURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("%w: relay realtime upstream URL is invalid", apperrors.ErrInvalidArgument)
	}
	switch parsed.Scheme {
	case "https":
		parsed.Scheme = "wss"
	case "http":
		parsed.Scheme = "ws"
	case "wss", "ws":
	default:
		return "", fmt.Errorf("%w: relay realtime upstream URL scheme is not supported", apperrors.ErrInvalidArgument)
	}
	query := parsed.Query()
	query.Set("model", strings.TrimSpace(firstNonEmpty(selection.route.UpstreamModel, publicModel)))
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

func relayEndpointURLForUpstream(upstream domainaigateway.LLMUpstream, route domainaigateway.LLMModelRoute, providerKind, endpoint string) (string, error) {
	providerKind = normalizeRelayProviderKind(providerKind)
	if providerKind == "azure-openai" {
		return azureOpenAIEndpointURL(
			upstream.BaseURL,
			route.UpstreamModel,
			endpoint,
			upstream.Metadata,
			route.Metadata,
		)
	}
	if providerKind == "gemini" {
		return geminiEndpointURL(
			upstream.BaseURL,
			route.UpstreamModel,
			endpoint,
			upstream.Metadata,
			route.Metadata,
		)
	}
	if providerKind == "cohere" {
		return cohereEndpointURL(upstream.BaseURL, endpoint)
	}
	return relayEndpointURL(upstream.BaseURL, providerKind, endpoint)
}

func relayEndpointURL(baseURL, providerKind, endpoint string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", fmt.Errorf("%w: relay upstream base URL is required", apperrors.ErrInvalidArgument)
	}
	switch providerKind {
	case "anthropic":
		if strings.HasSuffix(baseURL, "/v1") {
			return baseURL + anthropicEndpointPath(endpoint), nil
		}
		return baseURL + "/v1" + anthropicEndpointPath(endpoint), nil
	default:
		if strings.HasSuffix(baseURL, "/v1") {
			return baseURL + openAIEndpointPath(endpoint), nil
		}
		return baseURL + "/v1" + openAIEndpointPath(endpoint), nil
	}
}

func azureOpenAIEndpointURL(baseURL, deployment, endpoint string, upstreamMetadata, routeMetadata map[string]any) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", fmt.Errorf("%w: relay upstream base URL is required", apperrors.ErrInvalidArgument)
	}
	values := azureOpenAIConfigValues(upstreamMetadata, routeMetadata)
	apiStyle := strings.ToLower(strings.TrimSpace(gatewayFirstString(values, "apiStyle", "api_style", "style", "mode")))
	apiVersion := strings.TrimSpace(gatewayFirstString(values, "apiVersion", "api_version", "azureApiVersion", "azure_api_version"))
	if apiStyle == "" {
		apiStyle = "v1"
		if apiVersion != "" && azureOpenAIEndpointSupportsDeployment(endpoint) {
			apiStyle = "deployment"
		}
	}
	switch apiStyle {
	case "deployment", "deployments", "versioned", "api-version", "api_version":
		if azureOpenAIEndpointSupportsDeployment(endpoint) {
			return azureOpenAIDeploymentEndpointURL(baseURL, deployment, endpoint, apiVersion, values)
		}
	}
	return azureOpenAIV1EndpointURL(baseURL, endpoint), nil
}

func azureOpenAIConfigValues(upstreamMetadata, routeMetadata map[string]any) map[string]any {
	out := copyMap(gatewayConditionValues(upstreamMetadata, "azureOpenAI", "azure_openai", "azure"))
	for key, value := range gatewayConditionValues(routeMetadata, "azureOpenAI", "azure_openai", "azure") {
		out[key] = value
	}
	return out
}

func geminiEndpointURL(baseURL, model, endpoint string, upstreamMetadata, routeMetadata map[string]any) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", fmt.Errorf("%w: relay upstream base URL is required", apperrors.ErrInvalidArgument)
	}
	values := geminiConfigValues(upstreamMetadata, routeMetadata)
	apiVersion := strings.Trim(strings.TrimSpace(gatewayFirstString(values, "apiVersion", "api_version", "version")), "/")
	if apiVersion == "" {
		apiVersion = "v1beta"
	}
	resourceBaseURL := geminiResourceBaseURL(baseURL, apiVersion)
	switch endpoint {
	case "models":
		return resourceBaseURL + "/models", nil
	case "interactions":
		return resourceBaseURL + "/interactions", nil
	case "generateContent", "streamGenerateContent":
		model = strings.TrimSpace(strings.TrimPrefix(model, "models/"))
		if model == "" {
			return "", fmt.Errorf("%w: Gemini relay upstream model is required", apperrors.ErrInvalidArgument)
		}
		targetURL := resourceBaseURL + "/models/" + url.PathEscape(model) + ":" + endpoint
		if endpoint == "streamGenerateContent" {
			targetURL += "?alt=sse"
		}
		return targetURL, nil
	default:
		return "", fmt.Errorf("%w: Gemini relay endpoint %s is not supported", apperrors.ErrInvalidArgument, endpoint)
	}
}

func geminiResourceBaseURL(baseURL, apiVersion string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	lower := strings.ToLower(baseURL)
	apiVersion = strings.Trim(apiVersion, "/")
	if strings.HasSuffix(lower, "/"+strings.ToLower(apiVersion)) {
		return baseURL
	}
	return baseURL + "/" + apiVersion
}

func geminiConfigValues(upstreamMetadata, routeMetadata map[string]any) map[string]any {
	out := copyMap(gatewayConditionValues(upstreamMetadata, "gemini", "googleAI", "google_ai"))
	for key, value := range gatewayConditionValues(routeMetadata, "gemini", "googleAI", "google_ai") {
		out[key] = value
	}
	return out
}

func cohereEndpointURL(baseURL, endpoint string) (string, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return "", fmt.Errorf("%w: relay upstream base URL is required", apperrors.ErrInvalidArgument)
	}
	switch endpoint {
	case "models":
		baseURL = trimURLSuffixFold(baseURL, "/v2")
		if strings.HasSuffix(strings.ToLower(baseURL), "/v1") {
			return baseURL + "/models", nil
		}
		return baseURL + "/v1/models", nil
	case "rerank":
		baseURL = trimURLSuffixFold(baseURL, "/v1")
		if strings.HasSuffix(strings.ToLower(baseURL), "/v2") {
			return baseURL + "/rerank", nil
		}
		return baseURL + "/v2/rerank", nil
	default:
		return "", fmt.Errorf("%w: Cohere relay endpoint %s is not supported", apperrors.ErrInvalidArgument, endpoint)
	}
}

func trimURLSuffixFold(value, suffix string) string {
	value = strings.TrimRight(strings.TrimSpace(value), "/")
	if strings.HasSuffix(strings.ToLower(value), strings.ToLower(suffix)) {
		return value[:len(value)-len(suffix)]
	}
	return value
}

func azureOpenAIEndpointSupportsDeployment(endpoint string) bool {
	switch endpoint {
	case "chat/completions", "embeddings", "images/generations", "images/edits", "images/variations", "audio/speech", "audio/transcriptions", "audio/translations":
		return true
	default:
		return false
	}
}

func azureOpenAIV1EndpointURL(baseURL, endpoint string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	lower := strings.ToLower(baseURL)
	switch {
	case strings.HasSuffix(lower, "/openai/v1"):
		return baseURL + openAIEndpointPath(endpoint)
	case strings.HasSuffix(lower, "/openai"):
		return baseURL + "/v1" + openAIEndpointPath(endpoint)
	default:
		return baseURL + "/openai/v1" + openAIEndpointPath(endpoint)
	}
}

func azureOpenAIDeploymentEndpointURL(baseURL, deployment, endpoint, apiVersion string, values map[string]any) (string, error) {
	deployment = strings.TrimSpace(firstNonEmpty(
		gatewayFirstString(values, "deployment", "deploymentID", "deploymentId", "deploymentName"),
		deployment,
	))
	if deployment == "" {
		return "", fmt.Errorf("%w: Azure OpenAI deployment is required", apperrors.ErrInvalidArgument)
	}
	apiVersion = strings.TrimSpace(apiVersion)
	if apiVersion == "" {
		return "", fmt.Errorf("%w: Azure OpenAI apiVersion is required for deployment style", apperrors.ErrInvalidArgument)
	}
	resourceBaseURL := azureOpenAIResourceBaseURL(baseURL)
	targetURL := resourceBaseURL + "/openai/deployments/" + url.PathEscape(deployment) + openAIEndpointPath(endpoint)
	return appendRelayQuery(targetURL, "api-version", apiVersion), nil
}

func azureOpenAIResourceBaseURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	lower := strings.ToLower(baseURL)
	switch {
	case strings.HasSuffix(lower, "/openai/v1"):
		return strings.TrimRight(baseURL[:len(baseURL)-len("/openai/v1")], "/")
	case strings.HasSuffix(lower, "/openai"):
		return strings.TrimRight(baseURL[:len(baseURL)-len("/openai")], "/")
	default:
		return baseURL
	}
}

func appendRelayQuery(targetURL, key, value string) string {
	separator := "?"
	if strings.Contains(targetURL, "?") {
		separator = "&"
	}
	return targetURL + separator + url.QueryEscape(key) + "=" + url.QueryEscape(value)
}

func relayRetryableUpstreamStatus(status int) bool {
	return status == http.StatusTooManyRequests || status == http.StatusBadGateway || status == http.StatusServiceUnavailable || status == http.StatusGatewayTimeout || status >= 500
}

func relayErrorCodeForStatus(status int) string {
	switch {
	case status == http.StatusTooManyRequests:
		return "upstream_429"
	case status >= 500:
		return "upstream_5xx"
	case status >= 400:
		return "upstream_4xx"
	default:
		return ""
	}
}

func openAIEndpointPath(endpoint string) string {
	switch endpoint {
	case "models":
		return "/models"
	case "realtime":
		return "/realtime"
	case "responses":
		return "/responses"
	case "embeddings":
		return "/embeddings"
	case "images/generations":
		return "/images/generations"
	case "images/edits":
		return "/images/edits"
	case "images/variations":
		return "/images/variations"
	case "audio/speech":
		return "/audio/speech"
	case "audio/transcriptions":
		return "/audio/transcriptions"
	case "audio/translations":
		return "/audio/translations"
	default:
		return "/chat/completions"
	}
}

func anthropicEndpointPath(endpoint string) string {
	switch endpoint {
	case "models":
		return "/models"
	default:
		return "/messages"
	}
}

func (s *Service) testRelayUpstream(ctx context.Context, upstream domainaigateway.LLMUpstream) (domainaigateway.LLMUpstreamTestResult, error) {
	checkedAt := time.Now().UTC()
	providerKind := normalizeRelayProviderKind(upstream.ProviderKind)
	if providerKind == "" {
		return domainaigateway.LLMUpstreamTestResult{}, fmt.Errorf("%w: upstream provider kind is invalid", apperrors.ErrInvalidArgument)
	}
	targetURL, err := relayEndpointURLForUpstream(upstream, domainaigateway.LLMModelRoute{}, providerKind, "models")
	if err != nil {
		return domainaigateway.LLMUpstreamTestResult{}, err
	}
	if err := s.validateRelayUpstreamURL(targetURL); err != nil {
		return domainaigateway.LLMUpstreamTestResult{}, err
	}
	apiKey, err := s.decryptRelayAPIKey(upstream.APIKeyCiphertext)
	if err != nil {
		return domainaigateway.LLMUpstreamTestResult{}, err
	}
	if strings.TrimSpace(apiKey) == "" {
		return domainaigateway.LLMUpstreamTestResult{}, fmt.Errorf("%w: relay upstream API key is not configured", apperrors.ErrInvalidArgument)
	}
	timeout := s.relayConfig.DefaultTimeout
	if upstream.TimeoutSeconds > 0 {
		timeout = time.Duration(upstream.TimeoutSeconds) * time.Second
	}
	upstreamCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(upstreamCtx, http.MethodGet, targetURL, nil)
	if err != nil {
		return domainaigateway.LLMUpstreamTestResult{}, fmt.Errorf("build relay upstream test request: %w", err)
	}
	applyRelayUpstreamHeaders(req.Header, LLMRelayHTTPRequest{ProviderKind: providerKind, Endpoint: "models", Headers: http.Header{}}, upstream, apiKey)
	started := time.Now().UTC()
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return domainaigateway.LLMUpstreamTestResult{}, fmt.Errorf("%w: relay upstream test failed", apperrors.ErrClusterUnready)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	status := "success"
	if resp.StatusCode >= 400 {
		status = "failure"
	}
	return domainaigateway.LLMUpstreamTestResult{
		UpstreamID:   upstream.ID,
		ProviderKind: providerKind,
		Status:       status,
		HTTPStatus:   resp.StatusCode,
		DurationMs:   time.Since(started).Milliseconds(),
		CheckedAt:    checkedAt,
	}, nil
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

func applyRelayUpstreamHeaders(headers http.Header, req LLMRelayHTTPRequest, upstream domainaigateway.LLMUpstream, apiKey string) {
	contentType := "application/json"
	if relayEndpointRequiresMultipart(req.Endpoint) {
		contentType = strings.TrimSpace(req.Headers.Get("Content-Type"))
	}
	headers.Set("Accept", firstNonEmpty(req.Headers.Get("Accept"), relayDefaultUpstreamAccept(req.Endpoint)))
	for key, value := range upstream.DefaultHeaders {
		name := http.CanonicalHeaderKey(strings.TrimSpace(key))
		if name == "" || isSensitiveRelayHeader(name) {
			continue
		}
		headers.Set(name, fmt.Sprint(value))
	}
	headers.Set("Content-Type", firstNonEmpty(contentType, "application/json"))
	if req.ProviderKind == "anthropic" {
		headers.Set("x-api-key", apiKey)
		headers.Set("anthropic-version", firstNonEmpty(req.Headers.Get("anthropic-version"), headers.Get("anthropic-version"), "2023-06-01"))
		if beta := req.Headers.Get("anthropic-beta"); beta != "" {
			headers.Set("anthropic-beta", beta)
		}
		return
	}
	if normalizeRelayProviderKind(req.ProviderKind) == "azure-openai" {
		headers.Set("api-key", apiKey)
		return
	}
	if normalizeRelayProviderKind(req.ProviderKind) == "gemini" {
		headers.Set("x-goog-api-key", apiKey)
		return
	}
	headers.Set("Authorization", "Bearer "+apiKey)
	if organization := req.Headers.Get("OpenAI-Organization"); organization != "" && relayProviderUsesOpenAIWireProtocol(req.ProviderKind) {
		headers.Set("OpenAI-Organization", organization)
	}
}

func relayRealtimeUpstreamHeaders(req LLMRelayHTTPRequest, upstream domainaigateway.LLMUpstream, apiKey string) http.Header {
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+apiKey)
	for key, value := range upstream.DefaultHeaders {
		name := http.CanonicalHeaderKey(strings.TrimSpace(key))
		if name == "" || isSensitiveRelayHeader(name) || relayWebSocketHopHeader(name) {
			continue
		}
		headers.Set(name, fmt.Sprint(value))
	}
	if organization := req.Headers.Get("OpenAI-Organization"); organization != "" {
		headers.Set("OpenAI-Organization", organization)
	}
	if beta := req.Headers.Get("OpenAI-Beta"); beta != "" {
		headers.Set("OpenAI-Beta", beta)
	}
	return headers
}

func relayWebSocketHopHeader(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "connection", "upgrade", "sec-websocket-key", "sec-websocket-accept", "sec-websocket-version", "sec-websocket-protocol", "sec-websocket-extensions":
		return true
	default:
		return false
	}
}

func copyRelayResponseHeaders(dst, src http.Header, defaultContentType string) {
	for _, name := range []string{
		"Content-Type",
		"Cache-Control",
		"X-Request-Id",
		"Openai-Request-Id",
		"Request-Id",
		"Anthropic-Organization-Id",
	} {
		for _, value := range src.Values(name) {
			if strings.TrimSpace(value) != "" {
				dst.Add(name, value)
			}
		}
	}
	if dst.Get("Content-Type") == "" {
		dst.Set("Content-Type", defaultContentType)
	}
}

func relayDefaultUpstreamAccept(endpoint string) string {
	if strings.TrimSpace(endpoint) == "audio/speech" {
		return "*/*"
	}
	return "application/json"
}

func relayDefaultResponseContentType(endpoint string) string {
	if strings.TrimSpace(endpoint) == "audio/speech" {
		return "application/octet-stream"
	}
	return "application/json"
}

type relayResponseCachePolicy struct {
	enabled          bool
	allowRead        bool
	allowWrite       bool
	ttl              time.Duration
	version          string
	scope            string
	maxResponseBytes int
}

type relayResponseCacheAttempt struct {
	policy        relayResponseCachePolicy
	cacheKey      string
	scopeKey      string
	requestHash   string
	publicModel   string
	upstreamID    string
	upstreamModel string
	providerKind  string
	endpoint      string
	status        string
	enabled       bool
	forceRefresh  bool
	readOnly      bool
	skipWriteCode string
}

func (a relayResponseCacheAttempt) statusOnMiss() string {
	if !a.enabled || a.forceRefresh {
		return relayCacheBypass
	}
	if a.status != "" {
		return a.status
	}
	return relayCacheMiss
}

func (s *Service) relayResponseCacheAttempt(accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selection relaySelection, publicModel string, stream bool, body []byte) relayResponseCacheAttempt {
	policy := relayResponseCachePolicyFromRoute(selection.route)
	mode := relayCacheMode(req)
	attempt := relayResponseCacheAttempt{policy: policy, status: relayCacheBypass}
	if !policy.enabled || mode == "bypass" || stream || !relayEndpointResponseCacheable(req.Endpoint) || !relayRequestResponseCacheSafe(body) {
		return attempt
	}
	if mode == "read-only" {
		attempt.readOnly = true
	}
	if mode == "refresh" {
		attempt.forceRefresh = true
	}
	if !policy.allowRead && !attempt.forceRefresh {
		attempt.status = relayCacheBypass
	}
	if !policy.allowWrite && !attempt.readOnly {
		attempt.skipWriteCode = "policy_write_disabled"
	}
	if attempt.readOnly {
		attempt.skipWriteCode = "read_only"
	}
	scopeKey := relayResponseCacheScopeKey(accessCtx, selection.route)
	requestHash := relayResponseCacheRequestHash(body)
	cacheKey := relayResponseCacheKey(s.relayConfig.CredentialEncryptionKey, scopeKey, req, selection, publicModel, requestHash, policy.version)
	if scopeKey == "" || requestHash == "" || cacheKey == "" {
		return relayResponseCacheAttempt{policy: policy, status: relayCacheBypass}
	}
	attempt.enabled = true
	attempt.scopeKey = scopeKey
	attempt.requestHash = requestHash
	attempt.cacheKey = cacheKey
	attempt.publicModel = strings.TrimSpace(publicModel)
	attempt.upstreamID = strings.TrimSpace(selection.upstream.ID)
	attempt.upstreamModel = strings.TrimSpace(selection.route.UpstreamModel)
	attempt.providerKind = normalizeRelayProviderKind(req.ProviderKind)
	attempt.endpoint = strings.TrimSpace(req.Endpoint)
	if !attempt.forceRefresh && policy.allowRead {
		attempt.status = relayCacheMiss
	}
	return attempt
}

func relayResponseCachePolicyFromRoute(route domainaigateway.LLMModelRoute) relayResponseCachePolicy {
	values := gatewayConditionValues(route.CachePolicy, "responseCache", "response_cache", "cache")
	policy := relayResponseCachePolicy{
		enabled:          false,
		allowRead:        true,
		allowWrite:       true,
		ttl:              5 * time.Minute,
		version:          "v1",
		scope:            "private",
		maxResponseBytes: 1024 * 1024,
	}
	if raw, ok := gatewayConditionRaw(values, "enabled"); ok {
		policy.enabled = boolFromAny(raw)
	}
	if raw, ok := gatewayConditionRaw(values, "read"); ok {
		policy.allowRead = boolFromAny(raw)
	}
	if raw, ok := gatewayConditionRaw(values, "allowRead"); ok {
		policy.allowRead = boolFromAny(raw)
	}
	if raw, ok := gatewayConditionRaw(values, "write"); ok {
		policy.allowWrite = boolFromAny(raw)
	}
	if raw, ok := gatewayConditionRaw(values, "allowWrite"); ok {
		policy.allowWrite = boolFromAny(raw)
	}
	if ttl, _ := gatewayConditionWindow(values, policy.ttl, "5m"); ttl > 0 {
		policy.ttl = ttl
	}
	if seconds, ok := gatewayFirstPositiveInt(values, "ttlSeconds", "ttl_seconds", "ttl"); ok {
		policy.ttl = time.Duration(seconds) * time.Second
	}
	if maxBytes, ok := gatewayFirstPositiveInt(values, "maxResponseBytes", "max_response_bytes", "maxBytes", "max_bytes"); ok {
		policy.maxResponseBytes = maxBytes
	}
	if version := gatewayFirstString(values, "version", "policyVersion", "policy_version", "cachePolicyVersion", "cache_policy_version"); version != "" {
		policy.version = version
	}
	if scope := gatewayFirstString(values, "scope", "scopeMode", "scope_mode"); scope != "" {
		policy.scope = strings.ToLower(strings.TrimSpace(scope))
	}
	return policy
}

func relayCacheMode(req LLMRelayHTTPRequest) string {
	mode := strings.ToLower(strings.TrimSpace(relayHeaderValue(req.Headers, relayHeaderCacheMode)))
	switch mode {
	case "", "default":
		return "default"
	case "bypass", "read-only", "refresh":
		return mode
	default:
		return "bypass"
	}
}

func relayEndpointResponseCacheable(endpoint string) bool {
	switch strings.TrimSpace(endpoint) {
	case "chat/completions", "responses", "messages":
		return true
	default:
		return false
	}
}

func relayResponseCacheScopeKey(accessCtx domainidentity.AccessContext, route domainaigateway.LLMModelRoute) string {
	policy := relayResponseCachePolicyFromRoute(route)
	switch policy.scope {
	case "shared", "global":
		return "shared:" + strings.TrimSpace(route.ID)
	default:
		tokenID := strings.TrimSpace(accessCtx.TokenID)
		if tokenID == "" {
			tokenID = strings.TrimSpace(accessCtx.TokenPrefix)
		}
		if tokenID == "" {
			tokenID = strings.TrimSpace(accessCtx.SubjectID)
		}
		if tokenID == "" {
			return ""
		}
		return "private:" + strings.TrimSpace(accessCtx.SubjectType) + ":" + strings.TrimSpace(accessCtx.SubjectID) + ":" + tokenID
	}
}

func relayResponseCacheRequestHash(body []byte) string {
	normalized, err := relayNormalizedJSON(body)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(normalized)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func relayResponseCacheKey(secret, scopeKey string, req LLMRelayHTTPRequest, selection relaySelection, publicModel, requestHash, version string) string {
	if scopeKey == "" || requestHash == "" {
		return ""
	}
	keyMaterial := strings.Join([]string{
		scopeKey,
		normalizeRelayProviderKind(req.ProviderKind),
		strings.TrimSpace(req.Endpoint),
		strings.TrimSpace(publicModel),
		strings.TrimSpace(selection.upstream.ID),
		strings.TrimSpace(selection.route.UpstreamModel),
		relayResponseCacheTargetFingerprint(req, selection),
		strings.TrimSpace(requestHash),
		strings.TrimSpace(version),
	}, "\n")
	mac := hmac.New(sha256.New, []byte(firstNonEmpty(strings.TrimSpace(secret), "opensoha-relay-cache-key")))
	_, _ = mac.Write([]byte(keyMaterial))
	return "sha256:" + hex.EncodeToString(mac.Sum(nil))
}

func relayResponseCacheTargetFingerprint(req LLMRelayHTTPRequest, selection relaySelection) string {
	if normalizeRelayProviderKind(req.ProviderKind) != "azure-openai" {
		return ""
	}
	targetURL, err := relayEndpointURLForSelection(selection, "azure-openai", strings.TrimSpace(req.Endpoint))
	if err != nil || targetURL == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(targetURL))
	return hex.EncodeToString(sum[:])
}

func relayNormalizedJSON(body []byte) ([]byte, error) {
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return json.Marshal(payload)
}

func (s *Service) writeRelayCachedResponse(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selection relaySelection, publicModel string, stream bool, attempt relayResponseCacheAttempt, started time.Time, writer http.ResponseWriter) (bool, error) {
	if !attempt.enabled || attempt.forceRefresh || !attempt.policy.allowRead || attempt.cacheKey == "" {
		return false, nil
	}
	repo := s.llmRelayRepository()
	if repo == nil {
		return false, nil
	}
	entry, err := repo.GetLLMCacheEntryByKey(ctx, attempt.cacheKey)
	if err != nil {
		if !errors.Is(err, apperrors.ErrNotFound) {
			return false, nil
		}
		return false, nil
	}
	now := time.Now().UTC()
	if !relayCacheEntryActive(entry, now) || entry.ScopeKey != attempt.scopeKey || entry.RequestHash != attempt.requestHash {
		return false, nil
	}
	body, err := secretcrypto.DecryptString(s.relayConfig.CredentialEncryptionKey, entry.ResponseBodyCiphertext)
	if err != nil {
		return false, nil
	}
	for key, value := range entry.ResponseHeaders {
		name := http.CanonicalHeaderKey(strings.TrimSpace(key))
		if name == "" || isSensitiveRelayHeader(name) {
			continue
		}
		if text := strings.TrimSpace(fmt.Sprint(value)); text != "" {
			writer.Header().Set(name, text)
		}
	}
	if writer.Header().Get("Content-Type") == "" {
		writer.Header().Set("Content-Type", "application/json")
	}
	writeRelayRouteTraceHeaders(writer.Header(), req, selection, publicModel, stream, 0, relayCacheHit)
	writer.WriteHeader(http.StatusOK)
	if _, err := writer.Write([]byte(body)); err != nil {
		s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, stream, domainaigateway.LLMCallLog{
			Status:               "client_cancelled",
			HTTPStatus:           http.StatusOK,
			UpstreamStatus:       0,
			ErrorCode:            "client_cancelled",
			ErrorMessage:         redactRelayText(err.Error()),
			DurationMilliseconds: time.Since(started).Milliseconds(),
			InputBytes:           int64(len(req.Body)),
			OutputBytes:          int64(len(body)),
			CacheStatus:          relayCacheHit,
			CreatedAt:            started,
		})
		return true, nil
	}
	entry.HitCount++
	entry.LastHitAt = &now
	_, _ = repo.UpdateLLMCacheEntry(ctx, entry)
	usage := relayUsageFromBody([]byte(body))
	estimatedTokens := false
	if !relayUsageHasTokens(usage) {
		usage = estimateRelayUsage(req, []byte(body))
		estimatedTokens = relayUsageHasTokens(usage)
	}
	if usage.totalTokens == 0 && (usage.promptTokens > 0 || usage.completionTokens > 0) {
		usage.totalTokens = usage.promptTokens + usage.completionTokens
	}
	s.recordRelayCall(ctx, principal, accessCtx, req, selection, publicModel, stream, domainaigateway.LLMCallLog{
		Status:               "success",
		HTTPStatus:           http.StatusOK,
		UpstreamStatus:       0,
		PromptTokens:         usage.promptTokens,
		CompletionTokens:     usage.completionTokens,
		TotalTokens:          usage.totalTokens,
		ReasoningTokens:      usage.reasoningTokens,
		CachedReadTokens:     usage.cachedReadTokens,
		CachedWriteTokens:    usage.cachedWriteTokens,
		EstimatedTokens:      estimatedTokens,
		DurationMilliseconds: time.Since(started).Milliseconds(),
		InputBytes:           int64(len(req.Body)),
		OutputBytes:          int64(len(body)),
		CacheStatus:          relayCacheHit,
		CreatedAt:            started,
	})
	return true, nil
}

func relayCacheEntryActive(entry domainaigateway.LLMCacheEntry, now time.Time) bool {
	if !strings.EqualFold(strings.TrimSpace(entry.Status), "active") {
		return false
	}
	return entry.ExpiresAt == nil || entry.ExpiresAt.After(now)
}

func (s *Service) storeRelayResponseCache(ctx context.Context, attempt relayResponseCacheAttempt, resp *http.Response, body []byte) string {
	if !attempt.enabled {
		return relayCacheBypass
	}
	if attempt.readOnly || !attempt.policy.allowWrite {
		return relayCacheWriteSkipped
	}
	if !relayResponseCacheWritable(resp, body, attempt.policy) {
		return relayCacheWriteSkipped
	}
	repo := s.llmRelayRepository()
	if repo == nil {
		return relayCacheWriteSkipped
	}
	ciphertext, err := secretcrypto.EncryptString(s.relayConfig.CredentialEncryptionKey, string(body))
	if err != nil {
		return relayCacheWriteSkipped
	}
	now := time.Now().UTC()
	expiresAt := now.Add(attempt.policy.ttl)
	entry := domainaigateway.LLMCacheEntry{
		ID:                     uuid.NewString(),
		CacheKey:               attempt.cacheKey,
		ScopeKey:               attempt.scopeKey,
		PublicModel:            attempt.publicModel,
		UpstreamID:             attempt.upstreamID,
		UpstreamModel:          attempt.upstreamModel,
		RequestHash:            attempt.requestHash,
		ResponseBodyCiphertext: ciphertext,
		ResponseHeaders:        relayCacheResponseHeaders(resp.Header),
		Status:                 "active",
		ExpiresAt:              &expiresAt,
		Metadata: map[string]any{
			"providerKind":  attempt.providerKind,
			"endpoint":      attempt.endpoint,
			"policyVersion": attempt.policy.version,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}
	if existing, err := repo.GetLLMCacheEntryByKey(ctx, attempt.cacheKey); err == nil && existing.ID != "" {
		entry.ID = existing.ID
		entry.PublicModel = existing.PublicModel
		entry.UpstreamID = existing.UpstreamID
		entry.UpstreamModel = existing.UpstreamModel
		entry.HitCount = existing.HitCount
		entry.CreatedAt = existing.CreatedAt
		if _, err := repo.UpdateLLMCacheEntry(ctx, entry); err != nil {
			return relayCacheWriteSkipped
		}
		return relayCacheWrite
	}
	if _, err := repo.CreateLLMCacheEntry(ctx, entry); err != nil {
		return relayCacheWriteSkipped
	}
	return relayCacheWrite
}

func relayResponseCacheWritable(resp *http.Response, body []byte, policy relayResponseCachePolicy) bool {
	if resp == nil || resp.StatusCode < 200 || resp.StatusCode >= 300 || len(body) == 0 {
		return false
	}
	if policy.maxResponseBytes > 0 && len(body) > policy.maxResponseBytes {
		return false
	}
	if resp.Header.Get("Set-Cookie") != "" {
		return false
	}
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	return strings.Contains(contentType, "application/json") || strings.HasPrefix(strings.TrimSpace(string(body)), "{") || strings.HasPrefix(strings.TrimSpace(string(body)), "[")
}

func relayRequestResponseCacheSafe(body []byte) bool {
	var payload any
	if len(body) == 0 || json.Unmarshal(body, &payload) != nil {
		return false
	}
	return !relayJSONContainsCacheUnsafeValue(payload)
}

func relayJSONContainsCacheUnsafeValue(value any) bool {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			switch strings.ToLower(strings.TrimSpace(key)) {
			case "tools", "tool_choice", "tool_calls", "function_call", "functions", "file", "files", "file_id", "file_data", "filedata", "inlinedata", "inline_data", "cachedcontent", "cached_content", "image", "images", "image_url", "input_image", "audio", "input_audio":
				return true
			case "type":
				text := strings.ToLower(strings.TrimSpace(fmt.Sprint(item)))
				if strings.Contains(text, "image") || strings.Contains(text, "file") || strings.Contains(text, "audio") || strings.Contains(text, "tool") {
					return true
				}
			}
			if relayJSONContainsCacheUnsafeValue(item) {
				return true
			}
		}
	case []any:
		for _, item := range typed {
			if relayJSONContainsCacheUnsafeValue(item) {
				return true
			}
		}
	}
	return false
}

func relayCacheResponseHeaders(headers http.Header) map[string]any {
	out := map[string]any{}
	for _, name := range []string{"Content-Type", "Cache-Control", "X-Request-Id", "Openai-Request-Id", "Request-Id", "Anthropic-Organization-Id"} {
		if value := headers.Get(name); strings.TrimSpace(value) != "" && !isSensitiveRelayHeader(name) {
			out[http.CanonicalHeaderKey(name)] = value
		}
	}
	if out["Content-Type"] == nil {
		out["Content-Type"] = "application/json"
	}
	return out
}

func writeRelayRouteTraceHeaders(headers http.Header, req LLMRelayHTTPRequest, selection relaySelection, publicModel string, stream bool, upstreamStatus int, cacheStatus string) {
	if !relayRouteTraceRequested(req) {
		return
	}
	if strings.TrimSpace(cacheStatus) == "" {
		cacheStatus = relayCacheBypass
	}
	plan := relayTransformPlanForRoute(selection.route, req.ProviderKind)
	headers.Set("X-Soha-Route-ID", selection.route.ID)
	headers.Set("X-Soha-Upstream-ID", selection.upstream.ID)
	headers.Set("X-Soha-Upstream-Name", selection.upstream.Name)
	headers.Set("X-Soha-Provider-Kind", selection.upstream.ProviderKind)
	headers.Set("X-Soha-Public-Model", publicModel)
	headers.Set("X-Soha-Upstream-Model", selection.route.UpstreamModel)
	headers.Set("X-Soha-Relay-Endpoint", req.Endpoint)
	headers.Set("X-Soha-Relay-Provider-Kind", normalizeRelayProviderKind(req.ProviderKind))
	headers.Set("X-Soha-Relay-Stream", fmt.Sprint(stream))
	headers.Set("X-Soha-Upstream-Status", fmt.Sprint(upstreamStatus))
	headers.Set("X-Soha-Cache-Status", cacheStatus)
	if plan.enabled {
		headers.Set("X-Soha-Transform", plan.requestProvider+"-to-"+plan.upstreamProvider)
		headers.Set("X-Soha-Upstream-Endpoint", plan.upstreamEndpoint)
	}
}

func relayRouteTraceRequested(req LLMRelayHTTPRequest) bool {
	return boolFromAny(relayHeaderValue(req.Headers, relayHeaderRouteTrace))
}

func relayHeaderValue(headers http.Header, name string) string {
	if headers == nil {
		return ""
	}
	if value := headers.Get(name); value != "" {
		return value
	}
	for key, values := range headers {
		if !strings.EqualFold(strings.TrimSpace(key), name) {
			continue
		}
		for _, value := range values {
			if strings.TrimSpace(value) != "" {
				return value
			}
		}
	}
	return ""
}

func relayCallLogFromResponse(req LLMRelayHTTPRequest, resp *http.Response, started, upstreamStarted, firstByteAt time.Time, output []byte, inputBytes int64, status, errorCode, errorMessage, cacheStatus string) domainaigateway.LLMCallLog {
	usage := relayUsageFromBody(output)
	estimatedTokens := false
	if !relayUsageHasTokens(usage) && relayShouldEstimateUsage(resp.StatusCode, status) {
		usage = estimateRelayUsage(req, output)
		estimatedTokens = relayUsageHasTokens(usage)
	}
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

func relayWebSocketDialer() *websocket.Dialer {
	dialer := *websocket.DefaultDialer
	return &dialer
}

func relayWebSocketUpgrader() websocket.Upgrader {
	return websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return relayAllowWebSocketOrigin(r)
		},
	}
}

func relayAllowWebSocketOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" {
		return false
	}
	requestHost := relayHostName(r.Host)
	originHost := relayHostName(parsed.Host)
	if strings.EqualFold(originHost, requestHost) {
		return true
	}
	return relayIsLocalHost(originHost) && relayIsLocalHost(requestHost)
}

func relayHostName(hostport string) string {
	host, _, err := net.SplitHostPort(hostport)
	if err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(hostport, "[]")
}

func relayIsLocalHost(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || strings.HasSuffix(host, ".localhost")
}

type relayWebSocketProxyResult struct {
	firstByteAt   time.Time
	clientBytes   int64
	upstreamBytes int64
	err           error
}

func relayProxyWebSocketMessages(ctx context.Context, clientConn, upstreamConn *websocket.Conn) (time.Time, int64, int64, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan relayWebSocketProxyResult, 2)
	var closeOnce sync.Once
	closeBoth := func() {
		closeOnce.Do(func() {
			_ = clientConn.Close()
			_ = upstreamConn.Close()
		})
	}
	go relayCopyWebSocketMessages(ctx, upstreamConn, clientConn, false, results, cancel, closeBoth)
	go relayCopyWebSocketMessages(ctx, clientConn, upstreamConn, true, results, cancel, closeBoth)

	var firstByteAt time.Time
	var clientBytes int64
	var upstreamBytes int64
	var firstErr error
	ctxDone := ctx.Done()
	for completed := 0; completed < 2; {
		select {
		case <-ctxDone:
			closeBoth()
			ctxDone = nil
		case result := <-results:
			completed++
			if !result.firstByteAt.IsZero() && firstByteAt.IsZero() {
				firstByteAt = result.firstByteAt
			}
			clientBytes += result.clientBytes
			upstreamBytes += result.upstreamBytes
			if firstErr == nil && result.err != nil {
				firstErr = result.err
			}
		}
	}
	return firstByteAt, clientBytes, upstreamBytes, firstErr
}

func relayCopyWebSocketMessages(ctx context.Context, dst, src *websocket.Conn, clientToUpstream bool, results chan<- relayWebSocketProxyResult, cancel context.CancelFunc, closeBoth func()) {
	result := relayWebSocketProxyResult{}
	defer func() {
		cancel()
		closeBoth()
		results <- result
	}()
	for {
		messageType, reader, err := src.NextReader()
		if err != nil {
			result.err = err
			return
		}
		writer, err := dst.NextWriter(messageType)
		if err != nil {
			result.err = err
			return
		}
		count, copyErr := io.Copy(writer, reader)
		closeErr := writer.Close()
		if clientToUpstream {
			result.clientBytes += count
		} else {
			result.upstreamBytes += count
			if count > 0 && result.firstByteAt.IsZero() {
				result.firstByteAt = time.Now().UTC()
			}
		}
		if copyErr != nil {
			result.err = copyErr
			return
		}
		if closeErr != nil {
			result.err = closeErr
			return
		}
		select {
		case <-ctx.Done():
			result.err = ctx.Err()
			return
		default:
		}
	}
}

func relayWebSocketCloseError(err error) bool {
	return errors.Is(err, net.ErrClosed) ||
		errors.Is(err, websocket.ErrCloseSent) ||
		websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway, websocket.CloseAbnormalClosure)
}

func redactRelayText(value string) string {
	if strings.TrimSpace(value) == "" {
		return value
	}
	return gatewaySensitiveValuePattern.ReplaceAllString(value, "$1$2[REDACTED]")
}

type relayUsage struct {
	promptTokens      int
	completionTokens  int
	totalTokens       int
	reasoningTokens   int
	cachedReadTokens  int
	cachedWriteTokens int
}

func relayUsageHasTokens(usage relayUsage) bool {
	return usage.promptTokens > 0 || usage.completionTokens > 0 || usage.totalTokens > 0 || usage.reasoningTokens > 0
}

func relayShouldEstimateUsage(statusCode int, status string) bool {
	return statusCode > 0 && statusCode < http.StatusBadRequest && strings.EqualFold(strings.TrimSpace(status), "success")
}

func relayUsageFromBody(body []byte) relayUsage {
	var payload map[string]any
	if len(body) == 0 {
		return relayUsage{}
	}
	if json.Unmarshal(body, &payload) != nil {
		return relayUsageFromSSE(body)
	}
	return relayUsageFromPayload(payload)
}

func relayUsageFromSSE(body []byte) relayUsage {
	var out relayUsage
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var payload map[string]any
		if json.Unmarshal([]byte(data), &payload) != nil {
			continue
		}
		mergeRelayUsage(&out, relayUsageFromPayload(payload))
	}
	if sum := out.promptTokens + out.completionTokens; sum > out.totalTokens {
		out.totalTokens = sum
	}
	return out
}

func relayUsageFromPayload(payload map[string]any) relayUsage {
	usageRaw, ok := payload["usage"].(map[string]any)
	if !ok {
		usageRaw, ok = payload["total_usage"].(map[string]any)
	}
	if !ok {
		message, hasMessage := payload["message"].(map[string]any)
		if hasMessage {
			usageRaw, ok = message["usage"].(map[string]any)
		}
	}
	if !ok {
		if geminiUsage, hasGeminiUsage := payload["usageMetadata"].(map[string]any); hasGeminiUsage {
			return relayUsageFromGeminiUsageMetadata(geminiUsage)
		}
		if geminiUsage, hasGeminiUsage := payload["usage_metadata"].(map[string]any); hasGeminiUsage {
			return relayUsageFromGeminiUsageMetadata(geminiUsage)
		}
	}
	if !ok {
		return relayUsage{}
	}
	return relayUsageFromUsageMap(usageRaw)
}

func relayUsageFromUsageMap(usageRaw map[string]any) relayUsage {
	out := relayUsage{
		promptTokens:     jsonNumberInt(usageRaw["prompt_tokens"], usageRaw["input_tokens"], usageRaw["total_input_tokens"]),
		completionTokens: jsonNumberInt(usageRaw["completion_tokens"], usageRaw["output_tokens"], usageRaw["total_output_tokens"]),
		totalTokens:      jsonNumberInt(usageRaw["total_tokens"]),
		reasoningTokens:  jsonNumberInt(usageRaw["total_thought_tokens"], usageRaw["thought_tokens"]),
		cachedReadTokens: jsonNumberInt(usageRaw["total_cached_tokens"], usageRaw["cached_tokens"]),
	}
	if out.totalTokens == 0 {
		out.totalTokens = out.promptTokens + out.completionTokens
	}
	if details, ok := usageRaw["completion_tokens_details"].(map[string]any); ok {
		out.reasoningTokens = jsonNumberInt(details["reasoning_tokens"])
	}
	if details, ok := usageRaw["prompt_tokens_details"].(map[string]any); ok {
		out.cachedReadTokens = jsonNumberInt(details["cached_tokens"])
	}
	out.cachedReadTokens = firstNonZeroInt(out.cachedReadTokens, jsonNumberInt(usageRaw["cache_read_input_tokens"]))
	out.cachedWriteTokens = firstNonZeroInt(out.cachedWriteTokens, jsonNumberInt(usageRaw["cache_creation_input_tokens"]))
	return out
}

func relayUsageFromGeminiUsageMetadata(usageRaw map[string]any) relayUsage {
	out := relayUsage{
		promptTokens:     jsonNumberInt(usageRaw["promptTokenCount"], usageRaw["prompt_token_count"]),
		completionTokens: jsonNumberInt(usageRaw["candidatesTokenCount"], usageRaw["candidates_token_count"]),
		totalTokens:      jsonNumberInt(usageRaw["totalTokenCount"], usageRaw["total_token_count"]),
		reasoningTokens:  jsonNumberInt(usageRaw["thoughtsTokenCount"], usageRaw["thoughts_token_count"]),
		cachedReadTokens: jsonNumberInt(usageRaw["cachedContentTokenCount"], usageRaw["cached_content_token_count"]),
	}
	if out.totalTokens == 0 {
		out.totalTokens = out.promptTokens + out.completionTokens
	}
	return out
}

func mergeRelayUsage(out *relayUsage, next relayUsage) {
	out.promptTokens = max(out.promptTokens, next.promptTokens)
	out.completionTokens = max(out.completionTokens, next.completionTokens)
	out.totalTokens = max(out.totalTokens, next.totalTokens)
	out.reasoningTokens = max(out.reasoningTokens, next.reasoningTokens)
	out.cachedReadTokens = max(out.cachedReadTokens, next.cachedReadTokens)
	out.cachedWriteTokens = max(out.cachedWriteTokens, next.cachedWriteTokens)
}

func estimateRelayUsage(req LLMRelayHTTPRequest, output []byte) relayUsage {
	out := relayUsage{
		promptTokens:     estimateRelayPromptTokens(req),
		completionTokens: estimateRelayCompletionTokens(req.Endpoint, output),
	}
	out.totalTokens = out.promptTokens + out.completionTokens
	return out
}

func estimateRelayPromptTokens(req LLMRelayHTTPRequest) int {
	endpoint := strings.TrimSpace(req.Endpoint)
	body := req.Body
	if len(body) == 0 {
		return 0
	}
	if relayEndpointRequiresMultipart(endpoint) {
		return estimateRelayMultipartPromptTokens(req)
	}
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return estimateRelayTextTokens(string(body))
	}
	switch endpoint {
	case "chat/completions":
		return estimateRelaySelectedJSONTokens(payload, "messages", "tools", "tool_choice", "response_format")
	case "responses":
		return estimateRelaySelectedJSONTokens(payload, "instructions", "input", "tools")
	case "messages":
		return estimateRelaySelectedJSONTokens(payload, "system", "messages", "tools")
	case "embeddings":
		return estimateRelaySelectedJSONTokens(payload, "input")
	case "generateContent", "streamGenerateContent":
		return estimateRelayGeminiPromptTokens(payload)
	case "audio/speech":
		return estimateRelaySelectedJSONTokens(payload, "input")
	case "images/generations":
		return estimateRelaySelectedJSONTokens(payload, "prompt")
	case "rerank":
		return estimateRelaySelectedJSONTokens(payload, "query", "documents", "rank_fields")
	default:
		return estimateRelayJSONTokens(payload)
	}
}

func estimateRelayMultipartPromptTokens(req LLMRelayHTTPRequest) int {
	fields, err := relayMultipartRequestFields(req.Endpoint, req.Body, req.Headers.Get("Content-Type"))
	if err != nil {
		return 0
	}
	switch strings.TrimSpace(req.Endpoint) {
	case "audio/transcriptions", "audio/translations", "images/edits":
		return estimateRelayTextTokens(fields["prompt"])
	default:
		return 0
	}
}

func estimateRelayCompletionTokens(endpoint string, body []byte) int {
	if len(body) == 0 {
		return 0
	}
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		if relayEndpointRequiresMultipart(endpoint) {
			return estimateRelayTextTokens(string(body))
		}
		return estimateRelayCompletionTokensFromSSE(endpoint, body)
	}
	return estimateRelayCompletionTokensFromPayload(endpoint, payload)
}

func estimateRelayCompletionTokensFromSSE(endpoint string, body []byte) int {
	total := 0
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		var payload map[string]any
		if json.Unmarshal([]byte(data), &payload) != nil {
			continue
		}
		if _, hasUsage := payload["usage"]; hasUsage {
			continue
		}
		total += estimateRelayCompletionTokensFromPayload(endpoint, payload)
	}
	return total
}

func estimateRelayCompletionTokensFromPayload(endpoint string, payload map[string]any) int {
	switch strings.TrimSpace(endpoint) {
	case "chat/completions":
		return estimateRelaySelectedJSONTokens(payload, "choices")
	case "responses":
		return estimateRelaySelectedJSONTokens(payload, "output_text", "output", "delta")
	case "messages":
		return estimateRelaySelectedJSONTokens(payload, "content", "delta")
	case "generateContent", "streamGenerateContent":
		return estimateRelayGeminiCompletionTokens(payload)
	case "audio/transcriptions", "audio/translations":
		return estimateRelaySelectedJSONTokens(payload, "text")
	case "embeddings", "images/generations", "images/edits", "images/variations", "audio/speech":
		return 0
	default:
		return estimateRelaySelectedJSONTokens(payload, "choices", "message", "content", "output", "output_text", "delta")
	}
}

func estimateRelayGeminiPromptTokens(payload map[string]any) int {
	total := estimateRelayGeminiTextPartsTokens(payload["systemInstruction"])
	total += estimateRelayGeminiTextPartsTokens(payload["system_instruction"])
	total += estimateRelayGeminiTextPartsTokens(payload["contents"])
	return total
}

func estimateRelayGeminiCompletionTokens(payload map[string]any) int {
	return estimateRelayGeminiTextPartsTokens(payload["candidates"])
}

func estimateRelayGeminiTextPartsTokens(value any) int {
	switch typed := value.(type) {
	case string:
		return estimateRelayTextTokens(typed)
	case []any:
		total := 0
		for _, item := range typed {
			total += estimateRelayGeminiTextPartsTokens(item)
		}
		return total
	case map[string]any:
		total := 0
		if text, ok := typed["text"].(string); ok {
			total += estimateRelayTextTokens(text)
		}
		for _, key := range []string{"content", "parts"} {
			if item, ok := typed[key]; ok {
				total += estimateRelayGeminiTextPartsTokens(item)
			}
		}
		return total
	default:
		return 0
	}
}

func estimateRelaySelectedJSONTokens(payload map[string]any, keys ...string) int {
	total := 0
	for _, key := range keys {
		if value, ok := payload[key]; ok {
			total += estimateRelayJSONTokens(value)
		}
	}
	return total
}

func estimateRelayJSONTokens(value any) int {
	switch typed := value.(type) {
	case string:
		return estimateRelayTextTokens(typed)
	case []any:
		total := 0
		for _, item := range typed {
			total += estimateRelayJSONTokens(item)
		}
		return total
	case map[string]any:
		total := 0
		for key, item := range typed {
			if relayEstimateSkipKey(key) {
				continue
			}
			total += estimateRelayJSONTokens(item)
		}
		return total
	default:
		return 0
	}
}

func relayEstimateSkipKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "id", "object", "model", "role", "type", "name", "index", "finish_reason", "usage", "stream", "stream_options", "metadata",
		"file", "files", "file_id", "file_data", "filedata", "inlinedata", "inline_data", "cachedcontent", "cached_content",
		"image", "images", "image_url", "input_image", "audio", "input_audio":
		return true
	default:
		return false
	}
}

func estimateRelayTextTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 0
	}
	asciiNonSpace := 0
	cjk := 0
	for _, r := range text {
		if r == ' ' || r == '\n' || r == '\r' || r == '\t' {
			continue
		}
		if relayEstimateCJKRune(r) {
			cjk++
			continue
		}
		asciiNonSpace++
	}
	total := cjk + ceilRelayDiv(asciiNonSpace, 4)
	if total <= 0 {
		return 0
	}
	return total
}

func relayEstimateCJKRune(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) ||
		(r >= 0x3400 && r <= 0x4DBF) ||
		(r >= 0x3040 && r <= 0x30FF) ||
		(r >= 0xAC00 && r <= 0xD7AF)
}

func ceilRelayDiv(value, divisor int) int {
	if value <= 0 || divisor <= 0 {
		return 0
	}
	return (value + divisor - 1) / divisor
}

func firstNonZeroInt(values ...int) int {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

func jsonNumberInt(values ...any) int {
	for _, value := range values {
		switch typed := value.(type) {
		case float64:
			return int(typed)
		case int:
			return typed
		case json.Number:
			n, _ := typed.Int64()
			return int(n)
		}
	}
	return 0
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

func (s *Service) validateRelayUpstreamURL(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%w: upstream base URL is invalid", apperrors.ErrInvalidArgument)
	}
	if parsed.Scheme == "http" && !s.relayConfig.AllowInsecureUpstreamHTTP {
		return fmt.Errorf("%w: insecure upstream HTTP is disabled", apperrors.ErrInvalidArgument)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("%w: upstream URL scheme is not supported", apperrors.ErrInvalidArgument)
	}
	if !s.relayConfig.AllowPrivateUpstreamHosts && relayHostBlocked(parsed.Hostname()) {
		return fmt.Errorf("%w: private upstream host is not allowed", apperrors.ErrInvalidArgument)
	}
	return nil
}

func (s *Service) validateRelayWebSocketUpstreamURL(rawURL string) error {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%w: upstream websocket URL is invalid", apperrors.ErrInvalidArgument)
	}
	if parsed.Scheme == "ws" && !s.relayConfig.AllowInsecureUpstreamHTTP {
		return fmt.Errorf("%w: insecure upstream websocket is disabled", apperrors.ErrInvalidArgument)
	}
	if parsed.Scheme != "wss" && parsed.Scheme != "ws" {
		return fmt.Errorf("%w: upstream websocket URL scheme is not supported", apperrors.ErrInvalidArgument)
	}
	if !s.relayConfig.AllowPrivateUpstreamHosts && relayHostBlocked(parsed.Hostname()) {
		return fmt.Errorf("%w: private upstream host is not allowed", apperrors.ErrInvalidArgument)
	}
	return nil
}

func relayHostBlocked(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return false
	}
	return addr.IsLoopback() || addr.IsLinkLocalUnicast() || addr.IsLinkLocalMulticast() || addr.IsPrivate() || addr.IsUnspecified()
}

func (s *Service) recordRelayAudit(ctx context.Context, principal domainidentity.Principal, action, result, summary string, metadata map[string]any) error {
	if s.auditLogRepository() == nil {
		return nil
	}
	return s.recordTokenAudit(ctx, principal, action, result, summary, metadata)
}
