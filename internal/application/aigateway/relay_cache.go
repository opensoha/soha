package aigateway

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

type relayCacheRepository interface {
	GetLLMCacheEntryByKey(context.Context, string) (domainaigateway.LLMCacheEntry, error)
	CreateLLMCacheEntry(context.Context, domainaigateway.LLMCacheEntry) (domainaigateway.LLMCacheEntry, error)
	UpdateLLMCacheEntry(context.Context, domainaigateway.LLMCacheEntry) (domainaigateway.LLMCacheEntry, error)
}

type relayResponseCache struct {
	repository  relayCacheRepository
	credentials *relayCredentialCodec
	keySecret   string
}

func newRelayResponseCache(repository relayCacheRepository, credentials *relayCredentialCodec, config LLMRelayConfig) *relayResponseCache {
	keySecret := strings.TrimSpace(config.CredentialEncryptionKey)
	if active := config.CredentialEncryptionKeys.Active(); active.ID() != "" {
		keySecret = active.Secret()
	}
	return &relayResponseCache{
		repository:  repository,
		credentials: credentials,
		keySecret:   keySecret,
	}
}

func (s *Service) relayCacheComponent() *relayResponseCache {
	if s.relayCache != nil {
		return s.relayCache
	}
	return newRelayResponseCache(s.llmRelayRepository(), s.relayCredentialCodec(), s.relayConfig)
}

func (s *Service) relayResponseCacheAttempt(accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selection relaySelection, publicModel string, stream bool, body []byte) relayResponseCacheAttempt {
	return s.relayCacheComponent().attempt(accessCtx, req, selection, publicModel, stream, body)
}

func (c *relayResponseCache) attempt(accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selection relaySelection, publicModel string, stream bool, body []byte) relayResponseCacheAttempt {
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
	cacheKey := relayResponseCacheKey(c.keySecret, scopeKey, req, selection, publicModel, requestHash, policy.version)
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

func (c *relayResponseCache) read(ctx context.Context, attempt relayResponseCacheAttempt, now time.Time) (domainaigateway.LLMCacheEntry, string, bool) {
	if c.repository == nil {
		return domainaigateway.LLMCacheEntry{}, "", false
	}
	entry, err := c.repository.GetLLMCacheEntryByKey(ctx, attempt.cacheKey)
	if err != nil {
		return domainaigateway.LLMCacheEntry{}, "", false
	}
	if !relayCacheEntryActive(entry, now) || entry.ScopeKey != attempt.scopeKey || entry.RequestHash != attempt.requestHash {
		return domainaigateway.LLMCacheEntry{}, "", false
	}
	body, err := c.credentials.decrypt(entry.ResponseBodyCiphertext)
	if err != nil {
		return domainaigateway.LLMCacheEntry{}, "", false
	}
	return entry, body, true
}

func (c *relayResponseCache) recordHit(ctx context.Context, entry domainaigateway.LLMCacheEntry, now time.Time) {
	if c.repository == nil {
		return
	}
	entry.HitCount++
	entry.LastHitAt = &now
	_, _ = c.repository.UpdateLLMCacheEntry(ctx, entry)
}

func (c *relayResponseCache) store(ctx context.Context, attempt relayResponseCacheAttempt, resp *http.Response, body []byte) string {
	if !attempt.enabled {
		return relayCacheBypass
	}
	if attempt.readOnly || !attempt.policy.allowWrite {
		return relayCacheWriteSkipped
	}
	if !relayResponseCacheWritable(resp, body, attempt.policy) || c.repository == nil {
		return relayCacheWriteSkipped
	}
	ciphertext, err := c.credentials.encrypt(string(body))
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
	if existing, err := c.repository.GetLLMCacheEntryByKey(ctx, attempt.cacheKey); err == nil && existing.ID != "" {
		entry.ID = existing.ID
		entry.PublicModel = existing.PublicModel
		entry.UpstreamID = existing.UpstreamID
		entry.UpstreamModel = existing.UpstreamModel
		entry.HitCount = existing.HitCount
		entry.CreatedAt = existing.CreatedAt
		if _, err := c.repository.UpdateLLMCacheEntry(ctx, entry); err != nil {
			return relayCacheWriteSkipped
		}
		return relayCacheWrite
	}
	if _, err := c.repository.CreateLLMCacheEntry(ctx, entry); err != nil {
		return relayCacheWriteSkipped
	}
	return relayCacheWrite
}

func (s *Service) writeRelayCachedResponse(ctx context.Context, principal domainidentity.Principal, accessCtx domainidentity.AccessContext, req LLMRelayHTTPRequest, selection relaySelection, publicModel string, stream bool, attempt relayResponseCacheAttempt, started time.Time, writer http.ResponseWriter) (bool, error) {
	if !attempt.enabled || attempt.forceRefresh || !attempt.policy.allowRead || attempt.cacheKey == "" {
		return false, nil
	}
	now := time.Now().UTC()
	entry, body, found := s.relayCacheComponent().read(ctx, attempt, now)
	if !found {
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
	s.relayCacheComponent().recordHit(ctx, entry, now)
	usage, estimatedTokens := (relayUsageAnalyzer{}).analyze(req, []byte(body), http.StatusOK, "success")
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
	return s.relayCacheComponent().store(ctx, attempt, resp, body)
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

func (a relayResponseCacheAttempt) statusOnMiss() string {
	if !a.enabled || a.forceRefresh {
		return relayCacheBypass
	}
	if a.status != "" {
		return a.status
	}
	return relayCacheMiss
}

func relayNormalizedJSON(body []byte) ([]byte, error) {
	var payload any
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	return json.Marshal(payload)
}
