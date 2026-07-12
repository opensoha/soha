package aigateway

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

const gatewayJSONRedactedValue = "[REDACTED]"

func (r *Repository) ListLLMUpstreams(ctx context.Context, filter domainaigateway.LLMUpstreamFilter) ([]domainaigateway.LLMUpstream, error) {
	query := `
		SELECT id, name, provider_kind, base_url, api_key_ciphertext, api_key_prefix, status, priority, weight,
			timeout_seconds, stream_timeout_seconds, max_concurrency, supported_models, default_headers, proxy_url, health, metadata,
			created_by, created_at, updated_at
		FROM ai_gateway_llm_upstreams
		WHERE 1 = 1
	`
	args := make([]any, 0)
	if filter.ProviderKind != "" {
		query += " AND provider_kind = ?"
		args = append(args, filter.ProviderKind)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	} else if !filter.IncludeAll {
		query += " AND status <> 'disabled'"
	}
	query += " ORDER BY priority ASC, weight DESC, name ASC, id ASC"
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanLLMUpstreamRows(rows)
}

func (r *Repository) GetLLMUpstream(ctx context.Context, upstreamID string) (domainaigateway.LLMUpstream, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, name, provider_kind, base_url, api_key_ciphertext, api_key_prefix, status, priority, weight,
			timeout_seconds, stream_timeout_seconds, max_concurrency, supported_models, default_headers, proxy_url, health, metadata,
			created_by, created_at, updated_at
		FROM ai_gateway_llm_upstreams
		WHERE id = ?
		LIMIT 1
	`, upstreamID).Row()
	return scanLLMUpstream(row)
}

func (r *Repository) CreateLLMUpstream(ctx context.Context, item domainaigateway.LLMUpstream) (domainaigateway.LLMUpstream, error) {
	normalizeLLMUpstreamDefaults(&item)
	supportedModels, defaultHeaders, health, metadata, err := marshalLLMUpstreamJSON(item.SupportedModels, item.DefaultHeaders, item.Health, item.Metadata)
	if err != nil {
		return domainaigateway.LLMUpstream{}, err
	}
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = now
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_gateway_llm_upstreams (
			id, name, provider_kind, base_url, api_key_ciphertext, api_key_prefix, status, priority, weight,
			timeout_seconds, stream_timeout_seconds, max_concurrency, supported_models, default_headers, proxy_url, health, metadata,
			created_by, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.Name, item.ProviderKind, item.BaseURL, item.APIKeyCiphertext, item.APIKeyPrefix, item.Status, item.Priority, item.Weight,
		item.TimeoutSeconds, item.StreamTimeoutSeconds, item.MaxConcurrency, supportedModels, defaultHeaders, nullableString(item.ProxyURL), health, metadata,
		item.CreatedBy, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainaigateway.LLMUpstream{}, err
	}
	return item, nil
}

func (r *Repository) UpdateLLMUpstream(ctx context.Context, item domainaigateway.LLMUpstream) (domainaigateway.LLMUpstream, error) {
	normalizeLLMUpstreamDefaults(&item)
	supportedModels, defaultHeaders, health, metadata, err := marshalLLMUpstreamJSON(item.SupportedModels, item.DefaultHeaders, item.Health, item.Metadata)
	if err != nil {
		return domainaigateway.LLMUpstream{}, err
	}
	item.UpdatedAt = time.Now().UTC()
	result := r.db.WithContext(ctx).Exec(`
		UPDATE ai_gateway_llm_upstreams
		SET name = ?,
			provider_kind = ?,
			base_url = ?,
			api_key_ciphertext = CASE WHEN ? <> '' THEN ? ELSE api_key_ciphertext END,
			api_key_prefix = CASE WHEN ? <> '' THEN ? ELSE api_key_prefix END,
			status = ?,
			priority = ?,
			weight = ?,
			timeout_seconds = ?,
			stream_timeout_seconds = ?,
			max_concurrency = ?,
			supported_models = ?,
			default_headers = ?,
			proxy_url = ?,
			health = ?,
			metadata = ?,
			updated_at = ?
		WHERE id = ?
	`, item.Name, item.ProviderKind, item.BaseURL, item.APIKeyCiphertext, item.APIKeyCiphertext, item.APIKeyPrefix, item.APIKeyPrefix,
		item.Status, item.Priority, item.Weight, item.TimeoutSeconds, item.StreamTimeoutSeconds, item.MaxConcurrency, supportedModels, defaultHeaders,
		nullableString(item.ProxyURL), health, metadata, item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainaigateway.LLMUpstream{}, result.Error
	}
	if result.RowsAffected == 0 {
		return domainaigateway.LLMUpstream{}, apperrors.ErrNotFound
	}
	return r.GetLLMUpstream(ctx, item.ID)
}

func (r *Repository) DeleteLLMUpstream(ctx context.Context, upstreamID string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM ai_gateway_llm_upstreams WHERE id = ?`, upstreamID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *Repository) ListLLMModelRoutes(ctx context.Context, filter domainaigateway.LLMModelRouteFilter) ([]domainaigateway.LLMModelRoute, error) {
	query := `
		SELECT id, public_model, provider_kind, upstream_id, upstream_model, route_group, priority, weight, enabled,
			transform_policy, fallback_policy, cache_policy, rate_limit_profile_id, metadata, created_at, updated_at
		FROM ai_gateway_llm_model_routes
		WHERE 1 = 1
	`
	args := make([]any, 0)
	appendStringFilter(&query, &args, "public_model", filter.PublicModel)
	appendStringFilter(&query, &args, "provider_kind", filter.ProviderKind)
	appendStringFilter(&query, &args, "upstream_id", filter.UpstreamID)
	appendStringFilter(&query, &args, "route_group", filter.RouteGroup)
	if !filter.IncludeDisabled {
		query += " AND enabled = TRUE"
	}
	query += " ORDER BY public_model ASC, priority ASC, weight DESC, id ASC"
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanLLMModelRouteRows(rows)
}

func (r *Repository) GetLLMModelRoute(ctx context.Context, routeID string) (domainaigateway.LLMModelRoute, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, public_model, provider_kind, upstream_id, upstream_model, route_group, priority, weight, enabled,
			transform_policy, fallback_policy, cache_policy, rate_limit_profile_id, metadata, created_at, updated_at
		FROM ai_gateway_llm_model_routes
		WHERE id = ?
		LIMIT 1
	`, routeID).Row()
	return scanLLMModelRoute(row)
}

func (r *Repository) CreateLLMModelRoute(ctx context.Context, item domainaigateway.LLMModelRoute) (domainaigateway.LLMModelRoute, error) {
	normalizeLLMModelRouteDefaults(&item)
	transformPolicy, fallbackPolicy, cachePolicy, metadata, err := marshalLLMModelRouteJSON(item.TransformPolicy, item.FallbackPolicy, item.CachePolicy, item.Metadata)
	if err != nil {
		return domainaigateway.LLMModelRoute{}, err
	}
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = now
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_gateway_llm_model_routes (
			id, public_model, provider_kind, upstream_id, upstream_model, route_group, priority, weight, enabled,
			transform_policy, fallback_policy, cache_policy, rate_limit_profile_id, metadata, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.PublicModel, nullableString(item.ProviderKind), nullableString(item.UpstreamID), item.UpstreamModel, nullableString(item.RouteGroup),
		item.Priority, item.Weight, item.Enabled, transformPolicy, fallbackPolicy, cachePolicy, nullableString(item.RateLimitProfileID), metadata,
		item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainaigateway.LLMModelRoute{}, err
	}
	return item, nil
}

func (r *Repository) UpdateLLMModelRoute(ctx context.Context, item domainaigateway.LLMModelRoute) (domainaigateway.LLMModelRoute, error) {
	normalizeLLMModelRouteDefaults(&item)
	transformPolicy, fallbackPolicy, cachePolicy, metadata, err := marshalLLMModelRouteJSON(item.TransformPolicy, item.FallbackPolicy, item.CachePolicy, item.Metadata)
	if err != nil {
		return domainaigateway.LLMModelRoute{}, err
	}
	item.UpdatedAt = time.Now().UTC()
	result := r.db.WithContext(ctx).Exec(`
		UPDATE ai_gateway_llm_model_routes
		SET public_model = ?,
			provider_kind = ?,
			upstream_id = ?,
			upstream_model = ?,
			route_group = ?,
			priority = ?,
			weight = ?,
			enabled = ?,
			transform_policy = ?,
			fallback_policy = ?,
			cache_policy = ?,
			rate_limit_profile_id = ?,
			metadata = ?,
			updated_at = ?
		WHERE id = ?
	`, item.PublicModel, nullableString(item.ProviderKind), nullableString(item.UpstreamID), item.UpstreamModel, nullableString(item.RouteGroup),
		item.Priority, item.Weight, item.Enabled, transformPolicy, fallbackPolicy, cachePolicy, nullableString(item.RateLimitProfileID), metadata,
		item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainaigateway.LLMModelRoute{}, result.Error
	}
	if result.RowsAffected == 0 {
		return domainaigateway.LLMModelRoute{}, apperrors.ErrNotFound
	}
	return r.GetLLMModelRoute(ctx, item.ID)
}

func (r *Repository) DeleteLLMModelRoute(ctx context.Context, routeID string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM ai_gateway_llm_model_routes WHERE id = ?`, routeID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func (r *Repository) CreateLLMCallLog(ctx context.Context, item domainaigateway.LLMCallLog) error {
	routeTrace, metadata, err := marshalLLMCallLogJSON(item.RouteTrace, item.Metadata)
	if err != nil {
		return err
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_gateway_llm_call_logs (
			id, request_id, actor_type, actor_id, actor_name, token_id, token_prefix, token_kind, ai_client_id,
			public_model, upstream_id, upstream_name, provider_kind, upstream_model, endpoint, stream, status, http_status, upstream_status,
			error_code, error_message, prompt_tokens, completion_tokens, total_tokens, reasoning_tokens, cached_read_tokens, cached_write_tokens,
			estimated_tokens, ttfb_ms, ttft_ms, duration_ms, input_bytes, output_bytes, cache_status, route_trace, source_ip, user_agent, metadata, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, nullableString(item.RequestID), nullableString(item.ActorType), nullableString(item.ActorID), nullableString(item.ActorName),
		nullableString(item.TokenID), nullableString(item.TokenPrefix), nullableString(item.TokenKind), nullableString(item.AIClientID),
		nullableString(item.PublicModel), nullableString(item.UpstreamID), nullableString(item.UpstreamName), nullableString(item.ProviderKind),
		nullableString(item.UpstreamModel), nullableString(item.Endpoint), item.Stream, item.Status, item.HTTPStatus, item.UpstreamStatus,
		nullableString(item.ErrorCode), nullableString(item.ErrorMessage), item.PromptTokens, item.CompletionTokens, item.TotalTokens, item.ReasoningTokens,
		item.CachedReadTokens, item.CachedWriteTokens, item.EstimatedTokens, item.TTFBMilliseconds, item.TTFTMilliseconds, item.DurationMilliseconds,
		item.InputBytes, item.OutputBytes, nullableString(item.CacheStatus), routeTrace, nullableString(item.SourceIP), nullableString(item.UserAgent), metadata, item.CreatedAt).Error
}

func (r *Repository) ListLLMCallLogs(ctx context.Context, filter domainaigateway.LLMCallLogFilter) ([]domainaigateway.LLMCallLog, error) {
	query := `
		SELECT id, request_id, actor_type, actor_id, actor_name, token_id, token_prefix, token_kind, ai_client_id,
			public_model, upstream_id, upstream_name, provider_kind, upstream_model, endpoint, stream, status, http_status, upstream_status,
			error_code, error_message, prompt_tokens, completion_tokens, total_tokens, reasoning_tokens, cached_read_tokens, cached_write_tokens,
			estimated_tokens, ttfb_ms, ttft_ms, duration_ms, input_bytes, output_bytes, cache_status, route_trace, source_ip, user_agent, metadata, created_at
		FROM ai_gateway_llm_call_logs
		WHERE 1 = 1
	`
	args := make([]any, 0)
	query = appendLLMCallLogFilter(query, &args, filter)
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query += " ORDER BY created_at DESC, id ASC LIMIT ?"
	args = append(args, limit)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanLLMCallLogRows(rows)
}

func (r *Repository) LLMRelayCallLogMetrics(ctx context.Context, filter domainaigateway.LLMCallLogFilter) (domainaigateway.LLMRelayCallLogMetrics, error) {
	metrics := domainaigateway.LLMRelayCallLogMetrics{}
	query := `
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN status = 'success' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'success' THEN 0 ELSE 1 END), 0),
			COALESCE(AVG(CASE WHEN ttfb_ms > 0 THEN ttfb_ms ELSE NULL END), 0),
			COALESCE(AVG(CASE WHEN ttft_ms > 0 THEN ttft_ms ELSE NULL END), 0),
			COALESCE(AVG(CASE WHEN duration_ms > 0 THEN duration_ms ELSE NULL END), 0),
			COALESCE(SUM(CASE WHEN duration_ms > 0 THEN duration_ms ELSE 0 END), 0),
			COALESCE(SUM(total_tokens), 0),
			COALESCE(SUM(CASE WHEN cache_status = 'hit' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(cached_read_tokens), 0),
			COALESCE(SUM(cached_write_tokens), 0)
		FROM ai_gateway_llm_call_logs
		WHERE 1 = 1
	`
	args := make([]any, 0)
	query = appendLLMCallLogFilter(query, &args, filter)
	var totalDurationMs int64
	var totalTokens int
	if err := r.db.WithContext(ctx).Raw(query, args...).Row().Scan(
		&metrics.TotalCalls,
		&metrics.SuccessCount,
		&metrics.FailureCount,
		&metrics.AverageTTFBMs,
		&metrics.AverageTTFTMs,
		&metrics.AverageDurationMs,
		&totalDurationMs,
		&totalTokens,
		&metrics.CacheHitCount,
		&metrics.CacheReadTokens,
		&metrics.CacheWriteTokens,
	); err != nil {
		return domainaigateway.LLMRelayCallLogMetrics{}, err
	}
	if totalDurationMs > 0 {
		metrics.TokensPerSecond = float64(totalTokens) / (float64(totalDurationMs) / 1000)
	}
	modelRanking, err := r.llmRelayCallLogModelRanking(ctx, filter, 10)
	if err != nil {
		return domainaigateway.LLMRelayCallLogMetrics{}, err
	}
	metrics.ModelRanking = modelRanking
	recentErrors, err := r.llmRelayRecentErrorLogs(ctx, filter, 5)
	if err != nil {
		return domainaigateway.LLMRelayCallLogMetrics{}, err
	}
	metrics.RecentErrors = recentErrors
	return metrics, nil
}

func (r *Repository) llmRelayCallLogModelRanking(ctx context.Context, filter domainaigateway.LLMCallLogFilter, limit int) ([]domainaigateway.GovernanceMetricCount, error) {
	if limit <= 0 {
		return nil, nil
	}
	query := `
		SELECT public_model, COUNT(*)
		FROM ai_gateway_llm_call_logs
		WHERE public_model IS NOT NULL AND public_model <> ''
	`
	args := make([]any, 0)
	query = appendLLMCallLogFilter(query, &args, filter)
	query += " GROUP BY public_model ORDER BY COUNT(*) DESC, public_model ASC LIMIT ?"
	args = append(args, limit)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]domainaigateway.GovernanceMetricCount, 0)
	for rows.Next() {
		var item domainaigateway.GovernanceMetricCount
		if err := rows.Scan(&item.Key, &item.Count); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func (r *Repository) llmRelayRecentErrorLogs(ctx context.Context, filter domainaigateway.LLMCallLogFilter, limit int) ([]domainaigateway.LLMCallLog, error) {
	if limit <= 0 {
		return nil, nil
	}
	query := `
		SELECT id, request_id, actor_type, actor_id, actor_name, token_id, token_prefix, token_kind, ai_client_id,
			public_model, upstream_id, upstream_name, provider_kind, upstream_model, endpoint, stream, status, http_status, upstream_status,
			error_code, error_message, prompt_tokens, completion_tokens, total_tokens, reasoning_tokens, cached_read_tokens, cached_write_tokens,
			estimated_tokens, ttfb_ms, ttft_ms, duration_ms, input_bytes, output_bytes, cache_status, route_trace, source_ip, user_agent, metadata, created_at
		FROM ai_gateway_llm_call_logs
		WHERE status <> 'success'
	`
	args := make([]any, 0)
	query = appendLLMCallLogFilter(query, &args, filter)
	query += " ORDER BY created_at DESC, id ASC LIMIT ?"
	args = append(args, limit)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanLLMCallLogRows(rows)
}

func (r *Repository) SumLLMCallTokens(ctx context.Context, filter domainaigateway.LLMCallLogFilter) (int, error) {
	query := `
		SELECT COALESCE(SUM(
			CASE
				WHEN total_tokens > 0 THEN total_tokens
				WHEN prompt_tokens + completion_tokens > 0 THEN prompt_tokens + completion_tokens
				WHEN reasoning_tokens > 0 THEN reasoning_tokens
				ELSE 0
			END
		), 0)
		FROM ai_gateway_llm_call_logs
		WHERE 1 = 1
	`
	args := make([]any, 0)
	query = appendLLMCallLogFilter(query, &args, filter)
	var total int
	if err := r.db.WithContext(ctx).Raw(query, args...).Row().Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (r *Repository) LLMRelayCacheLogStats(ctx context.Context, filter domainaigateway.LLMCallLogFilter) (domainaigateway.LLMRelayCacheLogStats, error) {
	stats := domainaigateway.LLMRelayCacheLogStats{}
	query := `
		SELECT
			COALESCE(SUM(CASE WHEN cache_status = 'hit' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN cache_status = 'miss' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN cache_status = 'write' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN cache_status = 'bypass' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(cached_read_tokens), 0),
			COALESCE(SUM(cached_write_tokens), 0)
		FROM ai_gateway_llm_call_logs
		WHERE 1 = 1
	`
	args := make([]any, 0)
	query = appendLLMCallLogFilter(query, &args, filter)
	if err := r.db.WithContext(ctx).Raw(query, args...).Row().Scan(
		&stats.ResponseCacheHits,
		&stats.ResponseCacheMisses,
		&stats.ResponseCacheWrites,
		&stats.ResponseCacheBypasses,
		&stats.ProviderCachedReadTokens,
		&stats.ProviderCachedWriteTokens,
	); err != nil {
		return domainaigateway.LLMRelayCacheLogStats{}, err
	}
	byModel, err := r.llmRelayCacheStatsBreakdown(ctx, filter, "public_model", "publicModel")
	if err != nil {
		return domainaigateway.LLMRelayCacheLogStats{}, err
	}
	stats.ByModel = byModel
	byUpstream, err := r.llmRelayCacheStatsBreakdown(ctx, filter, "upstream_id", "upstreamId")
	if err != nil {
		return domainaigateway.LLMRelayCacheLogStats{}, err
	}
	stats.ByUpstream = byUpstream
	return stats, nil
}

func (r *Repository) llmRelayCacheStatsBreakdown(ctx context.Context, filter domainaigateway.LLMCallLogFilter, column, label string) ([]map[string]any, error) {
	query := fmt.Sprintf(`
		SELECT %s,
			COALESCE(SUM(CASE WHEN cache_status = 'hit' THEN 1 ELSE 0 END), 0) AS hits,
			COALESCE(SUM(CASE WHEN cache_status = 'miss' THEN 1 ELSE 0 END), 0) AS misses,
			COALESCE(SUM(CASE WHEN cache_status = 'write' THEN 1 ELSE 0 END), 0) AS writes,
			COALESCE(SUM(CASE WHEN cache_status = 'bypass' THEN 1 ELSE 0 END), 0) AS bypasses,
			COALESCE(SUM(cached_read_tokens), 0) AS provider_cached_read_tokens,
			COALESCE(SUM(cached_write_tokens), 0) AS provider_cached_write_tokens
		FROM ai_gateway_llm_call_logs
		WHERE %s IS NOT NULL AND %s <> ''
	`, column, column, column)
	args := make([]any, 0)
	query = appendLLMCallLogFilter(query, &args, filter)
	query += fmt.Sprintf(" GROUP BY %s ORDER BY hits DESC, writes DESC, %s ASC LIMIT 20", column, column)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var value string
		var hits, misses, writes, bypasses, readTokens, writeTokens int
		if err := rows.Scan(&value, &hits, &misses, &writes, &bypasses, &readTokens, &writeTokens); err != nil {
			return nil, err
		}
		items = append(items, map[string]any{
			label:                       value,
			"responseCacheHits":         hits,
			"responseCacheMisses":       misses,
			"responseCacheWrites":       writes,
			"responseCacheBypasses":     bypasses,
			"providerCachedReadTokens":  readTokens,
			"providerCachedWriteTokens": writeTokens,
		})
	}
	return items, rows.Err()
}

func appendLLMCallLogFilter(query string, args *[]any, filter domainaigateway.LLMCallLogFilter) string {
	if filter.ActorType != "" {
		query += " AND actor_type = ?"
		*args = append(*args, filter.ActorType)
	}
	if filter.ActorID != "" {
		query += " AND actor_id = ?"
		*args = append(*args, filter.ActorID)
	}
	if filter.TokenID != "" {
		query += " AND token_id = ?"
		*args = append(*args, filter.TokenID)
	}
	if filter.TokenPrefix != "" {
		query += " AND token_prefix = ?"
		*args = append(*args, filter.TokenPrefix)
	}
	if filter.TokenKind != "" {
		query += " AND token_kind = ?"
		*args = append(*args, filter.TokenKind)
	}
	if filter.AIClientID != "" {
		query += " AND ai_client_id = ?"
		*args = append(*args, filter.AIClientID)
	}
	if filter.PublicModel != "" {
		query += " AND public_model = ?"
		*args = append(*args, filter.PublicModel)
	}
	if filter.UpstreamID != "" {
		query += " AND upstream_id = ?"
		*args = append(*args, filter.UpstreamID)
	}
	if filter.ProviderKind != "" {
		query += " AND provider_kind = ?"
		*args = append(*args, filter.ProviderKind)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		*args = append(*args, filter.Status)
	}
	if filter.Endpoint != "" {
		query += " AND endpoint = ?"
		*args = append(*args, filter.Endpoint)
	}
	if filter.CacheStatus != "" {
		query += " AND cache_status = ?"
		*args = append(*args, filter.CacheStatus)
	}
	if filter.From != nil {
		query += " AND created_at >= ?"
		*args = append(*args, *filter.From)
	}
	if filter.To != nil {
		query += " AND created_at <= ?"
		*args = append(*args, *filter.To)
	}
	return query
}

func (r *Repository) ListLLMCacheEntries(ctx context.Context, filter domainaigateway.LLMCacheEntryFilter) ([]domainaigateway.LLMCacheEntry, error) {
	query := `
		SELECT id, cache_key, scope_key, public_model, upstream_id, upstream_model, request_hash, response_body_ciphertext,
			response_headers, status, hit_count, expires_at, last_hit_at, metadata, created_at, updated_at
		FROM ai_gateway_llm_cache_entries
		WHERE 1 = 1
	`
	args := make([]any, 0)
	if filter.CacheKey != "" {
		query += " AND cache_key = ?"
		args = append(args, filter.CacheKey)
	}
	if filter.ScopeKey != "" {
		query += " AND scope_key = ?"
		args = append(args, filter.ScopeKey)
	}
	if filter.PublicModel != "" {
		query += " AND public_model = ?"
		args = append(args, filter.PublicModel)
	}
	if filter.UpstreamID != "" {
		query += " AND upstream_id = ?"
		args = append(args, filter.UpstreamID)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}
	if filter.ExpiresAfter != nil {
		query += " AND (expires_at IS NULL OR expires_at >= ?)"
		args = append(args, *filter.ExpiresAfter)
	}
	if filter.ExpiresBefore != nil {
		query += " AND expires_at IS NOT NULL AND expires_at <= ?"
		args = append(args, *filter.ExpiresBefore)
	}
	if filter.UpdatedBefore != nil {
		query += " AND updated_at <= ?"
		args = append(args, *filter.UpdatedBefore)
	}
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query += " ORDER BY updated_at DESC, id ASC LIMIT ?"
	args = append(args, limit)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanLLMCacheEntryRows(rows)
}

func (r *Repository) GetLLMCacheEntry(ctx context.Context, entryID string) (domainaigateway.LLMCacheEntry, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, cache_key, scope_key, public_model, upstream_id, upstream_model, request_hash, response_body_ciphertext,
			response_headers, status, hit_count, expires_at, last_hit_at, metadata, created_at, updated_at
		FROM ai_gateway_llm_cache_entries
		WHERE id = ?
		LIMIT 1
	`, entryID).Row()
	return scanLLMCacheEntry(row)
}

func (r *Repository) GetLLMCacheEntryByKey(ctx context.Context, cacheKey string) (domainaigateway.LLMCacheEntry, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, cache_key, scope_key, public_model, upstream_id, upstream_model, request_hash, response_body_ciphertext,
			response_headers, status, hit_count, expires_at, last_hit_at, metadata, created_at, updated_at
		FROM ai_gateway_llm_cache_entries
		WHERE cache_key = ?
		LIMIT 1
	`, cacheKey).Row()
	return scanLLMCacheEntry(row)
}

func (r *Repository) CountLLMCacheEntries(ctx context.Context, filter domainaigateway.LLMCacheEntryFilter) (int, error) {
	query := `SELECT COUNT(*) FROM ai_gateway_llm_cache_entries WHERE 1 = 1`
	args := make([]any, 0)
	query = appendLLMCacheEntryFilter(query, &args, filter)
	var total int
	if err := r.db.WithContext(ctx).Raw(query, args...).Row().Scan(&total); err != nil {
		return 0, err
	}
	return total, nil
}

func (r *Repository) CreateLLMCacheEntry(ctx context.Context, item domainaigateway.LLMCacheEntry) (domainaigateway.LLMCacheEntry, error) {
	responseHeaders, metadata, err := marshalLLMCacheEntryJSON(item.ResponseHeaders, item.Metadata)
	if err != nil {
		return domainaigateway.LLMCacheEntry{}, err
	}
	if item.Status == "" {
		item.Status = "active"
	}
	now := time.Now().UTC()
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	if item.UpdatedAt.IsZero() {
		item.UpdatedAt = now
	}
	if err := r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_gateway_llm_cache_entries (
			id, cache_key, scope_key, public_model, upstream_id, upstream_model, request_hash, response_body_ciphertext,
			response_headers, status, hit_count, expires_at, last_hit_at, metadata, created_at, updated_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, item.CacheKey, item.ScopeKey, item.PublicModel, nullableString(item.UpstreamID), nullableString(item.UpstreamModel), item.RequestHash,
		item.ResponseBodyCiphertext, responseHeaders, item.Status, item.HitCount, item.ExpiresAt, item.LastHitAt, metadata, item.CreatedAt, item.UpdatedAt).Error; err != nil {
		return domainaigateway.LLMCacheEntry{}, err
	}
	return item, nil
}

func (r *Repository) UpdateLLMCacheEntry(ctx context.Context, item domainaigateway.LLMCacheEntry) (domainaigateway.LLMCacheEntry, error) {
	responseHeaders, metadata, err := marshalLLMCacheEntryJSON(item.ResponseHeaders, item.Metadata)
	if err != nil {
		return domainaigateway.LLMCacheEntry{}, err
	}
	if item.Status == "" {
		item.Status = "active"
	}
	item.UpdatedAt = time.Now().UTC()
	result := r.db.WithContext(ctx).Exec(`
		UPDATE ai_gateway_llm_cache_entries
		SET cache_key = ?,
			scope_key = ?,
			public_model = ?,
			upstream_id = ?,
			upstream_model = ?,
			request_hash = ?,
			response_body_ciphertext = CASE WHEN ? <> '' THEN ? ELSE response_body_ciphertext END,
			response_headers = ?,
			status = ?,
			hit_count = ?,
			expires_at = ?,
			last_hit_at = ?,
			metadata = ?,
			updated_at = ?
		WHERE id = ?
	`, item.CacheKey, item.ScopeKey, item.PublicModel, nullableString(item.UpstreamID), nullableString(item.UpstreamModel), item.RequestHash,
		item.ResponseBodyCiphertext, item.ResponseBodyCiphertext, responseHeaders, item.Status, item.HitCount, item.ExpiresAt, item.LastHitAt, metadata,
		item.UpdatedAt, item.ID)
	if result.Error != nil {
		return domainaigateway.LLMCacheEntry{}, result.Error
	}
	if result.RowsAffected == 0 {
		return domainaigateway.LLMCacheEntry{}, apperrors.ErrNotFound
	}
	return r.GetLLMCacheEntry(ctx, item.ID)
}

func (r *Repository) DeleteLLMCacheEntries(ctx context.Context, filter domainaigateway.LLMCacheEntryFilter) (int, error) {
	query := `DELETE FROM ai_gateway_llm_cache_entries WHERE 1 = 1`
	args := make([]any, 0)
	query = appendLLMCacheEntryFilter(query, &args, filter)
	result := r.db.WithContext(ctx).Exec(query, args...)
	if result.Error != nil {
		return 0, result.Error
	}
	return int(result.RowsAffected), nil
}

func (r *Repository) DeleteLLMCacheEntry(ctx context.Context, entryID string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM ai_gateway_llm_cache_entries WHERE id = ?`, entryID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func appendLLMCacheEntryFilter(query string, args *[]any, filter domainaigateway.LLMCacheEntryFilter) string {
	if filter.CacheKey != "" {
		query += " AND cache_key = ?"
		*args = append(*args, filter.CacheKey)
	}
	if filter.ScopeKey != "" {
		query += " AND scope_key = ?"
		*args = append(*args, filter.ScopeKey)
	}
	if filter.PublicModel != "" {
		query += " AND public_model = ?"
		*args = append(*args, filter.PublicModel)
	}
	if filter.UpstreamID != "" {
		query += " AND upstream_id = ?"
		*args = append(*args, filter.UpstreamID)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		*args = append(*args, filter.Status)
	}
	if filter.ExpiresAfter != nil {
		query += " AND (expires_at IS NULL OR expires_at >= ?)"
		*args = append(*args, *filter.ExpiresAfter)
	}
	if filter.ExpiresBefore != nil {
		query += " AND expires_at IS NOT NULL AND expires_at <= ?"
		*args = append(*args, *filter.ExpiresBefore)
	}
	if filter.UpdatedBefore != nil {
		query += " AND updated_at <= ?"
		*args = append(*args, *filter.UpdatedBefore)
	}
	return query
}

func (r *Repository) CreateLLMHealthEvent(ctx context.Context, item domainaigateway.LLMHealthEvent) error {
	metadata, err := marshalLLMHealthEventJSON(item.Metadata)
	if err != nil {
		return err
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now().UTC()
	}
	return r.db.WithContext(ctx).Exec(`
		INSERT INTO ai_gateway_llm_health_events (
			id, upstream_id, upstream_name, provider_kind, event_type, status, http_status, latency_ms, error_code, error_message, message, metadata, created_at
		)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, item.ID, nullableString(item.UpstreamID), nullableString(item.UpstreamName), nullableString(item.ProviderKind), item.EventType, item.Status,
		item.HTTPStatus, item.LatencyMilliseconds, nullableString(item.ErrorCode), nullableString(item.ErrorMessage), nullableString(item.Message), metadata, item.CreatedAt).Error
}

func (r *Repository) GetLLMHealthEvent(ctx context.Context, eventID string) (domainaigateway.LLMHealthEvent, error) {
	row := r.db.WithContext(ctx).Raw(`
		SELECT id, upstream_id, upstream_name, provider_kind, event_type, status, http_status, latency_ms, error_code, error_message, message, metadata, created_at
		FROM ai_gateway_llm_health_events
		WHERE id = ?
		LIMIT 1
	`, eventID).Row()
	return scanLLMHealthEvent(row)
}

func (r *Repository) ListLLMHealthEvents(ctx context.Context, filter domainaigateway.LLMHealthEventFilter) ([]domainaigateway.LLMHealthEvent, error) {
	query := `
		SELECT id, upstream_id, upstream_name, provider_kind, event_type, status, http_status, latency_ms, error_code, error_message, message, metadata, created_at
		FROM ai_gateway_llm_health_events
		WHERE 1 = 1
	`
	args := make([]any, 0)
	if filter.UpstreamID != "" {
		query += " AND upstream_id = ?"
		args = append(args, filter.UpstreamID)
	}
	if filter.ProviderKind != "" {
		query += " AND provider_kind = ?"
		args = append(args, filter.ProviderKind)
	}
	if filter.EventType != "" {
		query += " AND event_type = ?"
		args = append(args, filter.EventType)
	}
	if filter.Status != "" {
		query += " AND status = ?"
		args = append(args, filter.Status)
	}
	if filter.From != nil {
		query += " AND created_at >= ?"
		args = append(args, *filter.From)
	}
	if filter.To != nil {
		query += " AND created_at <= ?"
		args = append(args, *filter.To)
	}
	limit := filter.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	query += " ORDER BY created_at DESC, id ASC LIMIT ?"
	args = append(args, limit)
	rows, err := r.db.WithContext(ctx).Raw(query, args...).Rows()
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	return scanLLMHealthEventRows(rows)
}

func (r *Repository) DeleteLLMHealthEvent(ctx context.Context, eventID string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM ai_gateway_llm_health_events WHERE id = ?`, eventID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return apperrors.ErrNotFound
	}
	return nil
}

func scanLLMUpstreamRows(rows *sql.Rows) ([]domainaigateway.LLMUpstream, error) {
	items := make([]domainaigateway.LLMUpstream, 0)
	for rows.Next() {
		item, err := scanLLMUpstreamScanner(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanLLMUpstream(row *sql.Row) (domainaigateway.LLMUpstream, error) {
	item, err := scanLLMUpstreamScanner(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domainaigateway.LLMUpstream{}, apperrors.ErrNotFound
	}
	return item, err
}

func scanLLMUpstreamScanner(scanner interface {
	Scan(dest ...any) error
}) (domainaigateway.LLMUpstream, error) {
	var item domainaigateway.LLMUpstream
	var proxyURL sql.NullString
	var supportedModels, defaultHeaders, health, metadata []byte
	if err := scanner.Scan(
		&item.ID,
		&item.Name,
		&item.ProviderKind,
		&item.BaseURL,
		&item.APIKeyCiphertext,
		&item.APIKeyPrefix,
		&item.Status,
		&item.Priority,
		&item.Weight,
		&item.TimeoutSeconds,
		&item.StreamTimeoutSeconds,
		&item.MaxConcurrency,
		&supportedModels,
		&defaultHeaders,
		&proxyURL,
		&health,
		&metadata,
		&item.CreatedBy,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return domainaigateway.LLMUpstream{}, err
	}
	item.ProxyURL = proxyURL.String
	unmarshalJSON(supportedModels, &item.SupportedModels)
	unmarshalJSON(defaultHeaders, &item.DefaultHeaders)
	unmarshalJSON(health, &item.Health)
	unmarshalJSON(metadata, &item.Metadata)
	if item.SupportedModels == nil {
		item.SupportedModels = []string{}
	}
	if item.DefaultHeaders == nil {
		item.DefaultHeaders = map[string]any{}
	}
	if item.Health == nil {
		item.Health = map[string]any{}
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func scanLLMModelRouteRows(rows *sql.Rows) ([]domainaigateway.LLMModelRoute, error) {
	items := make([]domainaigateway.LLMModelRoute, 0)
	for rows.Next() {
		item, err := scanLLMModelRouteScanner(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanLLMModelRoute(row *sql.Row) (domainaigateway.LLMModelRoute, error) {
	item, err := scanLLMModelRouteScanner(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domainaigateway.LLMModelRoute{}, apperrors.ErrNotFound
	}
	return item, err
}

func scanLLMModelRouteScanner(scanner interface {
	Scan(dest ...any) error
}) (domainaigateway.LLMModelRoute, error) {
	var item domainaigateway.LLMModelRoute
	var providerKind, upstreamID, routeGroup, rateLimitProfileID sql.NullString
	var transformPolicy, fallbackPolicy, cachePolicy, metadata []byte
	if err := scanner.Scan(
		&item.ID,
		&item.PublicModel,
		&providerKind,
		&upstreamID,
		&item.UpstreamModel,
		&routeGroup,
		&item.Priority,
		&item.Weight,
		&item.Enabled,
		&transformPolicy,
		&fallbackPolicy,
		&cachePolicy,
		&rateLimitProfileID,
		&metadata,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return domainaigateway.LLMModelRoute{}, err
	}
	item.ProviderKind = providerKind.String
	item.UpstreamID = upstreamID.String
	item.RouteGroup = routeGroup.String
	item.RateLimitProfileID = rateLimitProfileID.String
	unmarshalJSON(transformPolicy, &item.TransformPolicy)
	unmarshalJSON(fallbackPolicy, &item.FallbackPolicy)
	unmarshalJSON(cachePolicy, &item.CachePolicy)
	unmarshalJSON(metadata, &item.Metadata)
	if item.TransformPolicy == nil {
		item.TransformPolicy = map[string]any{}
	}
	if item.FallbackPolicy == nil {
		item.FallbackPolicy = map[string]any{}
	}
	if item.CachePolicy == nil {
		item.CachePolicy = map[string]any{}
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func scanLLMCallLogRows(rows *sql.Rows) ([]domainaigateway.LLMCallLog, error) {
	items := make([]domainaigateway.LLMCallLog, 0)
	for rows.Next() {
		item, err := scanLLMCallLogScanner(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanLLMCallLogScanner(scanner interface {
	Scan(dest ...any) error
}) (domainaigateway.LLMCallLog, error) {
	var item domainaigateway.LLMCallLog
	var requestID, actorType, actorID, actorName, tokenID, tokenPrefix, tokenKind, aiClientID sql.NullString
	var publicModel, upstreamID, upstreamName, providerKind, upstreamModel, endpoint sql.NullString
	var errorCode, errorMessage, cacheStatus, sourceIP, userAgent sql.NullString
	var routeTrace, metadata []byte
	if err := scanner.Scan(
		&item.ID,
		&requestID,
		&actorType,
		&actorID,
		&actorName,
		&tokenID,
		&tokenPrefix,
		&tokenKind,
		&aiClientID,
		&publicModel,
		&upstreamID,
		&upstreamName,
		&providerKind,
		&upstreamModel,
		&endpoint,
		&item.Stream,
		&item.Status,
		&item.HTTPStatus,
		&item.UpstreamStatus,
		&errorCode,
		&errorMessage,
		&item.PromptTokens,
		&item.CompletionTokens,
		&item.TotalTokens,
		&item.ReasoningTokens,
		&item.CachedReadTokens,
		&item.CachedWriteTokens,
		&item.EstimatedTokens,
		&item.TTFBMilliseconds,
		&item.TTFTMilliseconds,
		&item.DurationMilliseconds,
		&item.InputBytes,
		&item.OutputBytes,
		&cacheStatus,
		&routeTrace,
		&sourceIP,
		&userAgent,
		&metadata,
		&item.CreatedAt,
	); err != nil {
		return domainaigateway.LLMCallLog{}, err
	}
	item.RequestID = requestID.String
	item.ActorType = actorType.String
	item.ActorID = actorID.String
	item.ActorName = actorName.String
	item.TokenID = tokenID.String
	item.TokenPrefix = tokenPrefix.String
	item.TokenKind = tokenKind.String
	item.AIClientID = aiClientID.String
	item.PublicModel = publicModel.String
	item.UpstreamID = upstreamID.String
	item.UpstreamName = upstreamName.String
	item.ProviderKind = providerKind.String
	item.UpstreamModel = upstreamModel.String
	item.Endpoint = endpoint.String
	item.ErrorCode = errorCode.String
	item.ErrorMessage = errorMessage.String
	item.CacheStatus = cacheStatus.String
	item.SourceIP = sourceIP.String
	item.UserAgent = userAgent.String
	unmarshalJSON(routeTrace, &item.RouteTrace)
	unmarshalJSON(metadata, &item.Metadata)
	if item.RouteTrace == nil {
		item.RouteTrace = map[string]any{}
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func scanLLMCacheEntryRows(rows *sql.Rows) ([]domainaigateway.LLMCacheEntry, error) {
	items := make([]domainaigateway.LLMCacheEntry, 0)
	for rows.Next() {
		item, err := scanLLMCacheEntryScanner(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanLLMCacheEntry(row *sql.Row) (domainaigateway.LLMCacheEntry, error) {
	item, err := scanLLMCacheEntryScanner(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domainaigateway.LLMCacheEntry{}, apperrors.ErrNotFound
	}
	return item, err
}

func scanLLMCacheEntryScanner(scanner interface {
	Scan(dest ...any) error
}) (domainaigateway.LLMCacheEntry, error) {
	var item domainaigateway.LLMCacheEntry
	var upstreamID, upstreamModel sql.NullString
	var expiresAt, lastHitAt sql.NullTime
	var responseHeaders, metadata []byte
	if err := scanner.Scan(
		&item.ID,
		&item.CacheKey,
		&item.ScopeKey,
		&item.PublicModel,
		&upstreamID,
		&upstreamModel,
		&item.RequestHash,
		&item.ResponseBodyCiphertext,
		&responseHeaders,
		&item.Status,
		&item.HitCount,
		&expiresAt,
		&lastHitAt,
		&metadata,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return domainaigateway.LLMCacheEntry{}, err
	}
	item.UpstreamID = upstreamID.String
	item.UpstreamModel = upstreamModel.String
	item.ExpiresAt = nullTimePointer(expiresAt)
	item.LastHitAt = nullTimePointer(lastHitAt)
	unmarshalJSON(responseHeaders, &item.ResponseHeaders)
	unmarshalJSON(metadata, &item.Metadata)
	if item.ResponseHeaders == nil {
		item.ResponseHeaders = map[string]any{}
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func scanLLMHealthEventRows(rows *sql.Rows) ([]domainaigateway.LLMHealthEvent, error) {
	items := make([]domainaigateway.LLMHealthEvent, 0)
	for rows.Next() {
		item, err := scanLLMHealthEventScanner(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func scanLLMHealthEvent(row *sql.Row) (domainaigateway.LLMHealthEvent, error) {
	item, err := scanLLMHealthEventScanner(row)
	if errors.Is(err, sql.ErrNoRows) {
		return domainaigateway.LLMHealthEvent{}, apperrors.ErrNotFound
	}
	return item, err
}

func scanLLMHealthEventScanner(scanner interface {
	Scan(dest ...any) error
}) (domainaigateway.LLMHealthEvent, error) {
	var item domainaigateway.LLMHealthEvent
	var upstreamID, upstreamName, providerKind, errorCode, errorMessage, message sql.NullString
	var metadata []byte
	if err := scanner.Scan(
		&item.ID,
		&upstreamID,
		&upstreamName,
		&providerKind,
		&item.EventType,
		&item.Status,
		&item.HTTPStatus,
		&item.LatencyMilliseconds,
		&errorCode,
		&errorMessage,
		&message,
		&metadata,
		&item.CreatedAt,
	); err != nil {
		return domainaigateway.LLMHealthEvent{}, err
	}
	item.UpstreamID = upstreamID.String
	item.UpstreamName = upstreamName.String
	item.ProviderKind = providerKind.String
	item.ErrorCode = errorCode.String
	item.ErrorMessage = errorMessage.String
	item.Message = message.String
	unmarshalJSON(metadata, &item.Metadata)
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
	return item, nil
}

func normalizeLLMUpstreamDefaults(item *domainaigateway.LLMUpstream) {
	if item.Status == "" {
		item.Status = "active"
	}
	if item.Weight <= 0 {
		item.Weight = 1
	}
	if item.TimeoutSeconds <= 0 {
		item.TimeoutSeconds = 120
	}
	if item.StreamTimeoutSeconds <= 0 {
		item.StreamTimeoutSeconds = 300
	}
	item.DefaultHeaders = redactedGatewayJSONMap(item.DefaultHeaders)
	item.Health = redactedGatewayJSONMap(item.Health)
	item.Metadata = redactedGatewayJSONMap(item.Metadata)
}

func normalizeLLMModelRouteDefaults(item *domainaigateway.LLMModelRoute) {
	if item.Weight <= 0 {
		item.Weight = 1
	}
	if item.TransformPolicy == nil {
		item.TransformPolicy = map[string]any{}
	}
	if item.FallbackPolicy == nil {
		item.FallbackPolicy = map[string]any{}
	}
	if item.CachePolicy == nil {
		item.CachePolicy = map[string]any{}
	}
	if item.Metadata == nil {
		item.Metadata = map[string]any{}
	}
}

func marshalLLMUpstreamJSON(supportedModels []string, defaultHeaders, health, metadata map[string]any) (string, string, string, string, error) {
	modelsRaw, err := json.Marshal(emptyStringSlice(supportedModels))
	if err != nil {
		return "", "", "", "", fmt.Errorf("marshal supported models: %w", err)
	}
	headersRaw, err := json.Marshal(redactedGatewayJSONMap(defaultHeaders))
	if err != nil {
		return "", "", "", "", fmt.Errorf("marshal default headers: %w", err)
	}
	healthRaw, err := json.Marshal(redactedGatewayJSONMap(health))
	if err != nil {
		return "", "", "", "", fmt.Errorf("marshal health: %w", err)
	}
	metadataRaw, err := json.Marshal(redactedGatewayJSONMap(metadata))
	if err != nil {
		return "", "", "", "", fmt.Errorf("marshal metadata: %w", err)
	}
	return string(modelsRaw), string(headersRaw), string(healthRaw), string(metadataRaw), nil
}

func marshalLLMModelRouteJSON(transformPolicy, fallbackPolicy, cachePolicy, metadata map[string]any) (string, string, string, string, error) {
	transformRaw, err := json.Marshal(emptyMap(transformPolicy))
	if err != nil {
		return "", "", "", "", fmt.Errorf("marshal transform policy: %w", err)
	}
	fallbackRaw, err := json.Marshal(emptyMap(fallbackPolicy))
	if err != nil {
		return "", "", "", "", fmt.Errorf("marshal fallback policy: %w", err)
	}
	cacheRaw, err := json.Marshal(emptyMap(cachePolicy))
	if err != nil {
		return "", "", "", "", fmt.Errorf("marshal cache policy: %w", err)
	}
	metadataRaw, err := json.Marshal(redactedGatewayJSONMap(metadata))
	if err != nil {
		return "", "", "", "", fmt.Errorf("marshal metadata: %w", err)
	}
	return string(transformRaw), string(fallbackRaw), string(cacheRaw), string(metadataRaw), nil
}

func marshalLLMCallLogJSON(routeTrace, metadata map[string]any) (string, string, error) {
	routeRaw, err := json.Marshal(redactedGatewayJSONMap(routeTrace))
	if err != nil {
		return "", "", fmt.Errorf("marshal route trace: %w", err)
	}
	metadataRaw, err := json.Marshal(redactedGatewayJSONMap(metadata))
	if err != nil {
		return "", "", fmt.Errorf("marshal metadata: %w", err)
	}
	return string(routeRaw), string(metadataRaw), nil
}

func marshalLLMCacheEntryJSON(responseHeaders, metadata map[string]any) (string, string, error) {
	headersRaw, err := json.Marshal(redactedGatewayJSONMap(responseHeaders))
	if err != nil {
		return "", "", fmt.Errorf("marshal response headers: %w", err)
	}
	metadataRaw, err := json.Marshal(redactedGatewayJSONMap(metadata))
	if err != nil {
		return "", "", fmt.Errorf("marshal metadata: %w", err)
	}
	return string(headersRaw), string(metadataRaw), nil
}

func marshalLLMHealthEventJSON(metadata map[string]any) (string, error) {
	raw, err := json.Marshal(redactedGatewayJSONMap(metadata))
	if err != nil {
		return "", fmt.Errorf("marshal metadata: %w", err)
	}
	return string(raw), nil
}

func redactedGatewayJSONMap(values map[string]any) map[string]any {
	if values == nil {
		return map[string]any{}
	}
	redacted, ok := redactGatewayJSONValue(values).(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return redacted
}

func redactGatewayJSONValue(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			if gatewayJSONKeySensitive(key) {
				out[key] = gatewayJSONRedactedValue
				continue
			}
			out[key] = redactGatewayJSONValue(child)
		}
		return out
	case map[string]string:
		out := make(map[string]any, len(typed))
		for key, child := range typed {
			if gatewayJSONKeySensitive(key) {
				out[key] = gatewayJSONRedactedValue
				continue
			}
			out[key] = child
		}
		return out
	case []any:
		out := make([]any, 0, len(typed))
		for _, child := range typed {
			out = append(out, redactGatewayJSONValue(child))
		}
		return out
	default:
		return value
	}
}

func gatewayJSONKeySensitive(key string) bool {
	normalized := strings.NewReplacer("-", "", "_", "", " ", "").Replace(strings.ToLower(key))
	for _, fragment := range []string{
		"authorization",
		"apikey",
		"accesskey",
		"secret",
		"token",
		"password",
		"passwd",
		"credential",
		"cookie",
		"setcookie",
	} {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}
