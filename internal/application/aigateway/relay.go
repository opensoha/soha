package aigateway

import (
	"context"
	cryptorand "crypto/rand"
	"fmt"
	"math/big"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
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
	return s.listLLMCallLogs(ctx, filter)
}

func (s *Service) listLLMCallLogsForGatewayRuntimeTool(ctx context.Context, principal domainidentity.Principal, filter domainaigateway.LLMCallLogFilter) ([]domainaigateway.LLMCallLog, error) {
	if err := appaccess.AuthorizeRuntimePermission(ctx, s.permissions, principal, appaccess.PermAIGatewayRelayView); err != nil {
		return nil, err
	}
	return s.listLLMCallLogs(ctx, filter)
}

func (s *Service) listLLMCallLogs(ctx context.Context, filter domainaigateway.LLMCallLogFilter) ([]domainaigateway.LLMCallLog, error) {
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
	ciphertext, err := s.encryptCredential(apiKey)
	if err != nil {
		return "", "", fmt.Errorf("%w: relay upstream API key encryption failed", apperrors.ErrInvalidArgument)
	}
	return ciphertext, relaySecretPrefix(apiKey), nil
}

func (s *Service) decryptRelayAPIKey(ciphertext string) (string, error) {
	apiKey, err := s.decryptCredential(ciphertext)
	if err != nil {
		return "", fmt.Errorf("%w: relay upstream API key decrypt failed", apperrors.ErrInvalidArgument)
	}
	return apiKey, nil
}

type relayCredentialCodec struct {
	config LLMRelayConfig
}

func newRelayCredentialCodec(config LLMRelayConfig) *relayCredentialCodec {
	return &relayCredentialCodec{config: config}
}

func (c *relayCredentialCodec) encrypt(value string) (string, error) {
	if c.config.CredentialEncryptionKeys.Active().ID() != "" {
		return secretcrypto.EncryptStringWithKeyring(c.config.CredentialEncryptionKeys, value)
	}
	return secretcrypto.EncryptString(c.config.CredentialEncryptionKey, value)
}

func (c *relayCredentialCodec) decrypt(value string) (string, error) {
	if c.config.CredentialEncryptionKeys.Active().ID() != "" {
		return secretcrypto.DecryptStringWithKeyring(c.config.CredentialEncryptionKeys, value)
	}
	return secretcrypto.DecryptString(c.config.CredentialEncryptionKey, value)
}

func (s *Service) relayCredentialCodec() *relayCredentialCodec {
	if s.relayCredentials != nil {
		return s.relayCredentials
	}
	return newRelayCredentialCodec(s.relayConfig)
}

func (s *Service) encryptCredential(value string) (string, error) {
	return s.relayCredentialCodec().encrypt(value)
}

func (s *Service) decryptCredential(value string) (string, error) {
	return s.relayCredentialCodec().decrypt(value)
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
