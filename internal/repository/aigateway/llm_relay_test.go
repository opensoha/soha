package aigateway

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRepositoryCreateUpdateAndFilterLLMUpstreams(t *testing.T) {
	repo, mock := newAIGatewayRepository(t)
	now := time.Date(2026, 6, 25, 10, 0, 0, 0, time.UTC)
	item := domainaigateway.LLMUpstream{
		ID:                   "upstream-1",
		Name:                 "OpenAI primary",
		ProviderKind:         "openai",
		BaseURL:              "https://api.openai.com/v1",
		APIKeyCiphertext:     "ciphertext-v1",
		APIKeyPrefix:         "sk-live...abcd",
		Status:               "active",
		Priority:             10,
		Weight:               3,
		TimeoutSeconds:       90,
		StreamTimeoutSeconds: 240,
		MaxConcurrency:       20,
		SupportedModels:      []string{"gpt-4.1"},
		DefaultHeaders: map[string]any{
			"Authorization": "Bearer sk-plaintext",
			"X-Trace":       "trace-1",
		},
		Health: map[string]any{
			"status": "ok",
		},
		Metadata: map[string]any{
			"owner":  "platform",
			"apiKey": "sk-plaintext",
		},
		CreatedBy: "admin",
		CreatedAt: now,
		UpdatedAt: now,
	}

	mock.ExpectExec(`(?s)INSERT INTO ai_gateway_llm_upstreams`).
		WithArgs(
			item.ID,
			item.Name,
			item.ProviderKind,
			item.BaseURL,
			item.APIKeyCiphertext,
			item.APIKeyPrefix,
			item.Status,
			item.Priority,
			item.Weight,
			item.TimeoutSeconds,
			item.StreamTimeoutSeconds,
			item.MaxConcurrency,
			jsonEqualArg(`["gpt-4.1"]`),
			jsonEqualArg(`{"Authorization":"[REDACTED]","X-Trace":"trace-1"}`),
			nil,
			jsonEqualArg(`{"status":"ok"}`),
			jsonEqualArg(`{"owner":"platform","apiKey":"[REDACTED]"}`),
			item.CreatedBy,
			item.CreatedAt,
			item.UpdatedAt,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	created, err := repo.CreateLLMUpstream(context.Background(), item)
	if err != nil {
		t.Fatalf("CreateLLMUpstream() error = %v", err)
	}
	encoded, err := json.Marshal(created)
	if err != nil {
		t.Fatalf("marshal upstream: %v", err)
	}
	if text := string(encoded); strings.Contains(text, "ciphertext-v1") || strings.Contains(text, "sk-plaintext") {
		t.Fatalf("upstream JSON leaked key material: %s", text)
	}

	updated := item
	updated.Name = "OpenAI primary updated"
	updated.APIKeyCiphertext = ""
	updated.APIKeyPrefix = ""
	mock.ExpectExec(`(?s)UPDATE ai_gateway_llm_upstreams`).
		WithArgs(
			updated.Name,
			updated.ProviderKind,
			updated.BaseURL,
			"",
			"",
			"",
			"",
			updated.Status,
			updated.Priority,
			updated.Weight,
			updated.TimeoutSeconds,
			updated.StreamTimeoutSeconds,
			updated.MaxConcurrency,
			jsonEqualArg(`["gpt-4.1"]`),
			jsonEqualArg(`{"Authorization":"[REDACTED]","X-Trace":"trace-1"}`),
			nil,
			jsonEqualArg(`{"status":"ok"}`),
			jsonEqualArg(`{"owner":"platform","apiKey":"[REDACTED]"}`),
			sqlmock.AnyArg(),
			updated.ID,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)SELECT .*FROM ai_gateway_llm_upstreams.*WHERE id = .*LIMIT 1`).
		WithArgs(updated.ID).
		WillReturnRows(llmUpstreamRows().AddRow(
			item.ID,
			updated.Name,
			item.ProviderKind,
			item.BaseURL,
			item.APIKeyCiphertext,
			item.APIKeyPrefix,
			item.Status,
			item.Priority,
			item.Weight,
			item.TimeoutSeconds,
			item.StreamTimeoutSeconds,
			item.MaxConcurrency,
			[]byte(`["gpt-4.1"]`),
			[]byte(`{"Authorization":"[REDACTED]","X-Trace":"trace-1"}`),
			nil,
			[]byte(`{"status":"ok"}`),
			[]byte(`{"owner":"platform","apiKey":"[REDACTED]"}`),
			item.CreatedBy,
			item.CreatedAt,
			time.Date(2026, 6, 25, 10, 1, 0, 0, time.UTC),
		))

	saved, err := repo.UpdateLLMUpstream(context.Background(), updated)
	if err != nil {
		t.Fatalf("UpdateLLMUpstream() error = %v", err)
	}
	if saved.APIKeyCiphertext != "ciphertext-v1" || saved.APIKeyPrefix != "sk-live...abcd" {
		t.Fatalf("updated upstream did not preserve stored key metadata: %#v", saved)
	}

	mock.ExpectQuery(`(?s)SELECT .*FROM ai_gateway_llm_upstreams.*provider_kind = .*status = .*ORDER BY`).
		WithArgs("openai", "active").
		WillReturnRows(llmUpstreamRows().AddRow(
			item.ID,
			updated.Name,
			item.ProviderKind,
			item.BaseURL,
			item.APIKeyCiphertext,
			item.APIKeyPrefix,
			item.Status,
			item.Priority,
			item.Weight,
			item.TimeoutSeconds,
			item.StreamTimeoutSeconds,
			item.MaxConcurrency,
			[]byte(`["gpt-4.1"]`),
			[]byte(`{"X-Trace":"trace-1"}`),
			nil,
			[]byte(`{"status":"ok"}`),
			[]byte(`{"owner":"platform"}`),
			item.CreatedBy,
			item.CreatedAt,
			item.UpdatedAt,
		))

	items, err := repo.ListLLMUpstreams(context.Background(), domainaigateway.LLMUpstreamFilter{
		ProviderKind: "openai",
		Status:       "active",
	})
	if err != nil {
		t.Fatalf("ListLLMUpstreams() error = %v", err)
	}
	if len(items) != 1 || items[0].ID != item.ID {
		t.Fatalf("unexpected upstream list: %#v", items)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRepositoryCreateUpdateAndFilterLLMModelRoutes(t *testing.T) {
	repo, mock := newAIGatewayRepository(t)
	now := time.Date(2026, 6, 25, 11, 0, 0, 0, time.UTC)
	item := domainaigateway.LLMModelRoute{
		ID:                 "route-1",
		PublicModel:        "gpt-4.1",
		ProviderKind:       "openai",
		UpstreamID:         "upstream-1",
		UpstreamModel:      "gpt-4.1",
		RouteGroup:         "prod",
		Priority:           10,
		Weight:             2,
		Enabled:            true,
		TransformPolicy:    map[string]any{"mode": "passthrough"},
		FallbackPolicy:     map[string]any{"maxAttempts": float64(2)},
		CachePolicy:        map[string]any{"enabled": false},
		RateLimitProfileID: "developer-default",
		Metadata:           map[string]any{"owner": "platform"},
		CreatedAt:          now,
		UpdatedAt:          now,
	}

	mock.ExpectExec(`(?s)INSERT INTO ai_gateway_llm_model_routes`).
		WithArgs(
			item.ID,
			item.PublicModel,
			item.ProviderKind,
			item.UpstreamID,
			item.UpstreamModel,
			item.RouteGroup,
			item.Priority,
			item.Weight,
			item.Enabled,
			jsonEqualArg(`{"mode":"passthrough"}`),
			jsonEqualArg(`{"maxAttempts":2}`),
			jsonEqualArg(`{"enabled":false}`),
			item.RateLimitProfileID,
			jsonEqualArg(`{"owner":"platform"}`),
			item.CreatedAt,
			item.UpdatedAt,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if _, err := repo.CreateLLMModelRoute(context.Background(), item); err != nil {
		t.Fatalf("CreateLLMModelRoute() error = %v", err)
	}

	item.Weight = 5
	mock.ExpectExec(`(?s)UPDATE ai_gateway_llm_model_routes`).
		WithArgs(
			item.PublicModel,
			item.ProviderKind,
			item.UpstreamID,
			item.UpstreamModel,
			item.RouteGroup,
			item.Priority,
			item.Weight,
			item.Enabled,
			jsonEqualArg(`{"mode":"passthrough"}`),
			jsonEqualArg(`{"maxAttempts":2}`),
			jsonEqualArg(`{"enabled":false}`),
			item.RateLimitProfileID,
			jsonEqualArg(`{"owner":"platform"}`),
			sqlmock.AnyArg(),
			item.ID,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`(?s)SELECT .*FROM ai_gateway_llm_model_routes.*WHERE id = .*LIMIT 1`).
		WithArgs(item.ID).
		WillReturnRows(llmModelRouteRows().AddRow(
			item.ID,
			item.PublicModel,
			item.ProviderKind,
			item.UpstreamID,
			item.UpstreamModel,
			item.RouteGroup,
			item.Priority,
			item.Weight,
			item.Enabled,
			[]byte(`{"mode":"passthrough"}`),
			[]byte(`{"maxAttempts":2}`),
			[]byte(`{"enabled":false}`),
			item.RateLimitProfileID,
			[]byte(`{"owner":"platform"}`),
			item.CreatedAt,
			time.Date(2026, 6, 25, 11, 1, 0, 0, time.UTC),
		))

	updated, err := repo.UpdateLLMModelRoute(context.Background(), item)
	if err != nil {
		t.Fatalf("UpdateLLMModelRoute() error = %v", err)
	}
	if updated.Weight != 5 {
		t.Fatalf("updated route weight = %d, want 5", updated.Weight)
	}

	mock.ExpectQuery(`(?s)SELECT .*FROM ai_gateway_llm_model_routes.*public_model = .*provider_kind = .*upstream_id = .*route_group = .*enabled = TRUE.*ORDER BY`).
		WithArgs("gpt-4.1", "openai", "upstream-1", "prod").
		WillReturnRows(llmModelRouteRows().AddRow(
			item.ID,
			item.PublicModel,
			item.ProviderKind,
			item.UpstreamID,
			item.UpstreamModel,
			item.RouteGroup,
			item.Priority,
			item.Weight,
			item.Enabled,
			[]byte(`{"mode":"passthrough"}`),
			[]byte(`{"maxAttempts":2}`),
			[]byte(`{"enabled":false}`),
			item.RateLimitProfileID,
			[]byte(`{"owner":"platform"}`),
			item.CreatedAt,
			item.UpdatedAt,
		))

	routes, err := repo.ListLLMModelRoutes(context.Background(), domainaigateway.LLMModelRouteFilter{
		PublicModel:  "gpt-4.1",
		ProviderKind: "openai",
		UpstreamID:   "upstream-1",
		RouteGroup:   "prod",
	})
	if err != nil {
		t.Fatalf("ListLLMModelRoutes() error = %v", err)
	}
	if len(routes) != 1 || routes[0].ID != item.ID {
		t.Fatalf("unexpected route list: %#v", routes)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRepositoryCreateAndListLLMCallLogsRedactsMetadata(t *testing.T) {
	repo, mock := newAIGatewayRepository(t)
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	item := domainaigateway.LLMCallLog{
		ID:                   "call-1",
		RequestID:            "req-1",
		ActorType:            "user",
		ActorID:              "user-1",
		ActorName:            "Ada",
		TokenID:              "token-1",
		TokenPrefix:          "soha_pat_1234",
		TokenKind:            "pat",
		AIClientID:           "client-1",
		PublicModel:          "gpt-4.1",
		UpstreamID:           "upstream-1",
		UpstreamName:         "OpenAI primary",
		ProviderKind:         "openai",
		UpstreamModel:        "gpt-4.1",
		Endpoint:             "chat/completions",
		Stream:               true,
		Status:               "success",
		HTTPStatus:           200,
		UpstreamStatus:       200,
		PromptTokens:         10,
		CompletionTokens:     20,
		TotalTokens:          30,
		CachedReadTokens:     4,
		CachedWriteTokens:    2,
		TTFBMilliseconds:     120,
		TTFTMilliseconds:     150,
		DurationMilliseconds: 900,
		InputBytes:           1000,
		OutputBytes:          2000,
		CacheStatus:          "miss",
		RouteTrace: map[string]any{
			"selected":      "upstream-1",
			"Authorization": "Bearer sk-plaintext",
		},
		SourceIP:  "203.0.113.10",
		UserAgent: "openai-go",
		Metadata: map[string]any{
			"apiKey": "sk-plaintext",
			"usage":  "summary",
		},
		CreatedAt: now,
	}

	mock.ExpectExec(`(?s)INSERT INTO ai_gateway_llm_call_logs`).
		WithArgs(
			item.ID,
			item.RequestID,
			item.ActorType,
			item.ActorID,
			item.ActorName,
			item.TokenID,
			item.TokenPrefix,
			item.TokenKind,
			item.AIClientID,
			item.PublicModel,
			item.UpstreamID,
			item.UpstreamName,
			item.ProviderKind,
			item.UpstreamModel,
			item.Endpoint,
			item.Stream,
			item.Status,
			item.HTTPStatus,
			item.UpstreamStatus,
			nil,
			nil,
			item.PromptTokens,
			item.CompletionTokens,
			item.TotalTokens,
			item.ReasoningTokens,
			item.CachedReadTokens,
			item.CachedWriteTokens,
			item.EstimatedTokens,
			item.TTFBMilliseconds,
			item.TTFTMilliseconds,
			item.DurationMilliseconds,
			item.InputBytes,
			item.OutputBytes,
			item.CacheStatus,
			jsonEqualArg(`{"Authorization":"[REDACTED]","selected":"upstream-1"}`),
			item.SourceIP,
			item.UserAgent,
			jsonEqualArg(`{"apiKey":"[REDACTED]","usage":"summary"}`),
			item.CreatedAt,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))

	if err := repo.CreateLLMCallLog(context.Background(), item); err != nil {
		t.Fatalf("CreateLLMCallLog() error = %v", err)
	}

	from := now.Add(-time.Hour)
	to := now.Add(time.Hour)
	mock.ExpectQuery(`(?s)SELECT .*FROM ai_gateway_llm_call_logs.*actor_type = .*actor_id = .*token_id = .*token_kind = .*ai_client_id = .*public_model = .*upstream_id = .*provider_kind = .*status = .*endpoint = .*created_at >= .*created_at <= .*ORDER BY`).
		WithArgs("user", "user-1", "token-1", "pat", "client-1", "gpt-4.1", "upstream-1", "openai", "success", "chat/completions", from, to, 20).
		WillReturnRows(llmCallLogRows().AddRow(
			item.ID,
			item.RequestID,
			item.ActorType,
			item.ActorID,
			item.ActorName,
			item.TokenID,
			item.TokenPrefix,
			item.TokenKind,
			item.AIClientID,
			item.PublicModel,
			item.UpstreamID,
			item.UpstreamName,
			item.ProviderKind,
			item.UpstreamModel,
			item.Endpoint,
			item.Stream,
			item.Status,
			item.HTTPStatus,
			item.UpstreamStatus,
			nil,
			nil,
			item.PromptTokens,
			item.CompletionTokens,
			item.TotalTokens,
			item.ReasoningTokens,
			item.CachedReadTokens,
			item.CachedWriteTokens,
			item.EstimatedTokens,
			item.TTFBMilliseconds,
			item.TTFTMilliseconds,
			item.DurationMilliseconds,
			item.InputBytes,
			item.OutputBytes,
			item.CacheStatus,
			[]byte(`{"Authorization":"[REDACTED]","selected":"upstream-1"}`),
			item.SourceIP,
			item.UserAgent,
			[]byte(`{"apiKey":"[REDACTED]","usage":"summary"}`),
			item.CreatedAt,
		))

	logs, err := repo.ListLLMCallLogs(context.Background(), domainaigateway.LLMCallLogFilter{
		ActorType:    "user",
		ActorID:      "user-1",
		TokenID:      "token-1",
		TokenKind:    "pat",
		AIClientID:   "client-1",
		PublicModel:  "gpt-4.1",
		UpstreamID:   "upstream-1",
		ProviderKind: "openai",
		Status:       "success",
		Endpoint:     "chat/completions",
		From:         &from,
		To:           &to,
		Limit:        20,
	})
	if err != nil {
		t.Fatalf("ListLLMCallLogs() error = %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("len(logs) = %d, want 1", len(logs))
	}
	if logs[0].Metadata["apiKey"] != "[REDACTED]" || logs[0].RouteTrace["Authorization"] != "[REDACTED]" {
		t.Fatalf("expected redacted log metadata, got %#v %#v", logs[0].Metadata, logs[0].RouteTrace)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRepositorySumLLMCallTokens(t *testing.T) {
	repo, mock := newAIGatewayRepository(t)
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	from := now.Add(-time.Minute)

	mock.ExpectQuery(`(?s)SELECT COALESCE\(SUM\(.*total_tokens.*prompt_tokens \+ completion_tokens.*reasoning_tokens.*FROM ai_gateway_llm_call_logs.*token_id = .*token_prefix = .*token_kind = .*public_model = .*upstream_id = .*created_at >= .*created_at <=`).
		WithArgs("token-1", "soha_pat_1234", "personal_access_token", "gpt-public", "upstream-1", from, now).
		WillReturnRows(sqlmock.NewRows([]string{"total"}).AddRow(42))

	total, err := repo.SumLLMCallTokens(context.Background(), domainaigateway.LLMCallLogFilter{
		TokenID:     "token-1",
		TokenPrefix: "soha_pat_1234",
		TokenKind:   "personal_access_token",
		PublicModel: "gpt-public",
		UpstreamID:  "upstream-1",
		From:        &from,
		To:          &now,
	})
	if err != nil {
		t.Fatalf("SumLLMCallTokens() error = %v", err)
	}
	if total != 42 {
		t.Fatalf("SumLLMCallTokens() = %d, want 42", total)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRepositoryAggregatesLLMRelayCallLogMetrics(t *testing.T) {
	repo, mock := newAIGatewayRepository(t)
	now := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	from := now.Add(-24 * time.Hour)

	mock.ExpectQuery(`(?s)SELECT\s+COUNT\(\*\).*SUM\(CASE WHEN status = 'success'.*FROM ai_gateway_llm_call_logs.*public_model = .*created_at >=`).
		WithArgs("gpt-public", from).
		WillReturnRows(sqlmock.NewRows([]string{
			"total_calls",
			"success_count",
			"failure_count",
			"avg_ttfb",
			"avg_ttft",
			"avg_duration",
			"total_duration",
			"total_tokens",
			"cache_hits",
			"cache_read_tokens",
			"cache_write_tokens",
		}).AddRow(501, 500, 1, 100.0, 150.0, 200.0, 100200, 300600, 10, 20, 30))
	mock.ExpectQuery(`(?s)SELECT public_model, COUNT\(\*\).*FROM ai_gateway_llm_call_logs.*public_model = .*created_at >= .*GROUP BY public_model`).
		WithArgs("gpt-public", from, 10).
		WillReturnRows(sqlmock.NewRows([]string{"public_model", "count"}).AddRow("gpt-public", 501))
	mock.ExpectQuery(`(?s)SELECT .*FROM ai_gateway_llm_call_logs.*WHERE status <> 'success'.*public_model = .*created_at >= .*ORDER BY`).
		WithArgs("gpt-public", from, 5).
		WillReturnRows(llmCallLogRows().AddRow(
			"call-failure",
			"req-1",
			"user",
			"user-1",
			"Ada",
			"token-1",
			"soha_pat_1234",
			"pat",
			"client-1",
			"gpt-public",
			"upstream-1",
			"OpenAI primary",
			"openai",
			"gpt-upstream",
			"chat/completions",
			false,
			"failure",
			503,
			503,
			"upstream_5xx",
			"temporary",
			0,
			0,
			0,
			0,
			0,
			0,
			false,
			0,
			0,
			0,
			100,
			0,
			"bypass",
			[]byte(`{}`),
			"203.0.113.10",
			"openai-go",
			[]byte(`{}`),
			now,
		))

	metrics, err := repo.LLMRelayCallLogMetrics(context.Background(), domainaigateway.LLMCallLogFilter{
		PublicModel: "gpt-public",
		From:        &from,
	})
	if err != nil {
		t.Fatalf("LLMRelayCallLogMetrics() error = %v", err)
	}
	if metrics.TotalCalls != 501 || metrics.SuccessCount != 500 || metrics.FailureCount != 1 {
		t.Fatalf("metrics counts = %#v, want 501/500/1", metrics)
	}
	if metrics.TokensPerSecond < 2999 || metrics.TokensPerSecond > 3001 {
		t.Fatalf("tokensPerSecond = %v, want about 3000", metrics.TokensPerSecond)
	}
	if len(metrics.ModelRanking) != 1 || metrics.ModelRanking[0].Key != "gpt-public" || metrics.ModelRanking[0].Count != 501 {
		t.Fatalf("model ranking = %#v", metrics.ModelRanking)
	}
	if len(metrics.RecentErrors) != 1 || metrics.RecentErrors[0].ID != "call-failure" {
		t.Fatalf("recent errors = %#v", metrics.RecentErrors)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRepositoryCreatesLLMCacheAndHealthRecordsWithRedaction(t *testing.T) {
	repo, mock := newAIGatewayRepository(t)
	now := time.Date(2026, 6, 25, 13, 0, 0, 0, time.UTC)
	expiresAt := now.Add(5 * time.Minute)
	cache := domainaigateway.LLMCacheEntry{
		ID:                     "cache-1",
		CacheKey:               "sha256:cache",
		ScopeKey:               "user:user-1",
		PublicModel:            "gpt-4.1",
		UpstreamID:             "upstream-1",
		UpstreamModel:          "gpt-4.1",
		RequestHash:            "sha256:req",
		ResponseBodyCiphertext: "cache-ciphertext",
		ResponseHeaders: map[string]any{
			"Authorization": "Bearer sk-plaintext",
			"Content-Type":  "application/json",
		},
		Status:    "active",
		ExpiresAt: &expiresAt,
		Metadata:  map[string]any{"token": "secret", "cachePolicy": "short"},
		CreatedAt: now,
		UpdatedAt: now,
	}
	mock.ExpectExec(`(?s)INSERT INTO ai_gateway_llm_cache_entries`).
		WithArgs(
			cache.ID,
			cache.CacheKey,
			cache.ScopeKey,
			cache.PublicModel,
			cache.UpstreamID,
			cache.UpstreamModel,
			cache.RequestHash,
			cache.ResponseBodyCiphertext,
			jsonEqualArg(`{"Authorization":"[REDACTED]","Content-Type":"application/json"}`),
			cache.Status,
			cache.HitCount,
			cache.ExpiresAt,
			cache.LastHitAt,
			jsonEqualArg(`{"token":"[REDACTED]","cachePolicy":"short"}`),
			cache.CreatedAt,
			cache.UpdatedAt,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if _, err := repo.CreateLLMCacheEntry(context.Background(), cache); err != nil {
		t.Fatalf("CreateLLMCacheEntry() error = %v", err)
	}

	health := domainaigateway.LLMHealthEvent{
		ID:                  "health-1",
		UpstreamID:          "upstream-1",
		UpstreamName:        "OpenAI primary",
		ProviderKind:        "openai",
		EventType:           "probe",
		Status:              "degraded",
		HTTPStatus:          429,
		LatencyMilliseconds: 250,
		ErrorCode:           "upstream_429",
		ErrorMessage:        "rate limited",
		Message:             "probe failed",
		Metadata:            map[string]any{"apiKey": "sk-plaintext", "retryAfter": float64(30)},
		CreatedAt:           now,
	}
	mock.ExpectExec(`(?s)INSERT INTO ai_gateway_llm_health_events`).
		WithArgs(
			health.ID,
			health.UpstreamID,
			health.UpstreamName,
			health.ProviderKind,
			health.EventType,
			health.Status,
			health.HTTPStatus,
			health.LatencyMilliseconds,
			health.ErrorCode,
			health.ErrorMessage,
			health.Message,
			jsonEqualArg(`{"apiKey":"[REDACTED]","retryAfter":30}`),
			health.CreatedAt,
		).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if err := repo.CreateLLMHealthEvent(context.Background(), health); err != nil {
		t.Fatalf("CreateLLMHealthEvent() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRepositoryCountsAndDeletesLLMCacheEntriesWithFilters(t *testing.T) {
	repo, mock := newAIGatewayRepository(t)
	olderThan := time.Date(2026, 6, 25, 12, 0, 0, 0, time.UTC)
	filter := domainaigateway.LLMCacheEntryFilter{
		PublicModel:   "gpt-public",
		UpstreamID:    "upstream-1",
		UpdatedBefore: &olderThan,
	}
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM ai_gateway_llm_cache_entries WHERE 1 = 1 AND public_model = \$1 AND upstream_id = \$2 AND updated_at <= \$3`).
		WithArgs(filter.PublicModel, filter.UpstreamID, olderThan).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	count, err := repo.CountLLMCacheEntries(context.Background(), filter)
	if err != nil {
		t.Fatalf("CountLLMCacheEntries() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("count = %d, want 2", count)
	}

	mock.ExpectExec(`DELETE FROM ai_gateway_llm_cache_entries WHERE 1 = 1 AND public_model = \$1 AND upstream_id = \$2 AND updated_at <= \$3`).
		WithArgs(filter.PublicModel, filter.UpstreamID, olderThan).
		WillReturnResult(sqlmock.NewResult(0, 2))
	deleted, err := repo.DeleteLLMCacheEntries(context.Background(), filter)
	if err != nil {
		t.Fatalf("DeleteLLMCacheEntries() error = %v", err)
	}
	if deleted != 2 {
		t.Fatalf("deleted = %d, want 2", deleted)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func TestRepositoryAggregatesLLMRelayCacheLogStats(t *testing.T) {
	repo, mock := newAIGatewayRepository(t)
	from := time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)
	filter := domainaigateway.LLMCallLogFilter{PublicModel: "gpt-public", From: &from, To: &to}

	mock.ExpectQuery(`(?s)SELECT.*SUM\(CASE WHEN cache_status = 'hit'.*FROM ai_gateway_llm_call_logs.*public_model = \$1.*created_at >= \$2.*created_at <= \$3`).
		WithArgs(filter.PublicModel, from, to).
		WillReturnRows(sqlmock.NewRows([]string{"hits", "misses", "writes", "bypasses", "read_tokens", "write_tokens"}).AddRow(3, 1, 2, 4, 11, 7))
	mock.ExpectQuery(`(?s)SELECT public_model,.*FROM ai_gateway_llm_call_logs.*public_model IS NOT NULL.*public_model = \$1.*created_at >= \$2.*created_at <= \$3.*GROUP BY public_model`).
		WithArgs(filter.PublicModel, from, to).
		WillReturnRows(sqlmock.NewRows([]string{"public_model", "hits", "misses", "writes", "bypasses", "read_tokens", "write_tokens"}).AddRow("gpt-public", 3, 1, 2, 4, 11, 7))
	mock.ExpectQuery(`(?s)SELECT upstream_id,.*FROM ai_gateway_llm_call_logs.*upstream_id IS NOT NULL.*public_model = \$1.*created_at >= \$2.*created_at <= \$3.*GROUP BY upstream_id`).
		WithArgs(filter.PublicModel, from, to).
		WillReturnRows(sqlmock.NewRows([]string{"upstream_id", "hits", "misses", "writes", "bypasses", "read_tokens", "write_tokens"}).AddRow("upstream-1", 2, 0, 1, 1, 5, 3))

	stats, err := repo.LLMRelayCacheLogStats(context.Background(), filter)
	if err != nil {
		t.Fatalf("LLMRelayCacheLogStats() error = %v", err)
	}
	if stats.ResponseCacheHits != 3 || stats.ResponseCacheMisses != 1 || stats.ResponseCacheWrites != 2 || stats.ResponseCacheBypasses != 4 || stats.ProviderCachedReadTokens != 11 || stats.ProviderCachedWriteTokens != 7 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
	if len(stats.ByModel) != 1 || stats.ByModel[0]["publicModel"] != "gpt-public" {
		t.Fatalf("unexpected model breakdown: %#v", stats.ByModel)
	}
	if len(stats.ByUpstream) != 1 || stats.ByUpstream[0]["upstreamId"] != "upstream-1" {
		t.Fatalf("unexpected upstream breakdown: %#v", stats.ByUpstream)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations: %v", err)
	}
}

func llmUpstreamRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"name",
		"provider_kind",
		"base_url",
		"api_key_ciphertext",
		"api_key_prefix",
		"status",
		"priority",
		"weight",
		"timeout_seconds",
		"stream_timeout_seconds",
		"max_concurrency",
		"supported_models",
		"default_headers",
		"proxy_url",
		"health",
		"metadata",
		"created_by",
		"created_at",
		"updated_at",
	})
}

func llmModelRouteRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"public_model",
		"provider_kind",
		"upstream_id",
		"upstream_model",
		"route_group",
		"priority",
		"weight",
		"enabled",
		"transform_policy",
		"fallback_policy",
		"cache_policy",
		"rate_limit_profile_id",
		"metadata",
		"created_at",
		"updated_at",
	})
}

func llmCallLogRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"request_id",
		"actor_type",
		"actor_id",
		"actor_name",
		"token_id",
		"token_prefix",
		"token_kind",
		"ai_client_id",
		"public_model",
		"upstream_id",
		"upstream_name",
		"provider_kind",
		"upstream_model",
		"endpoint",
		"stream",
		"status",
		"http_status",
		"upstream_status",
		"error_code",
		"error_message",
		"prompt_tokens",
		"completion_tokens",
		"total_tokens",
		"reasoning_tokens",
		"cached_read_tokens",
		"cached_write_tokens",
		"estimated_tokens",
		"ttfb_ms",
		"ttft_ms",
		"duration_ms",
		"input_bytes",
		"output_bytes",
		"cache_status",
		"route_trace",
		"source_ip",
		"user_agent",
		"metadata",
		"created_at",
	})
}

type jsonEqualArg string

func (expected jsonEqualArg) Match(actual driver.Value) bool {
	var actualRaw []byte
	switch typed := actual.(type) {
	case string:
		actualRaw = []byte(typed)
	case []byte:
		actualRaw = typed
	default:
		return false
	}
	var expectedValue any
	if err := json.Unmarshal([]byte(expected), &expectedValue); err != nil {
		return false
	}
	var actualValue any
	if err := json.Unmarshal(actualRaw, &actualValue); err != nil {
		return false
	}
	return reflect.DeepEqual(expectedValue, actualValue)
}

func newAIGatewayRepository(t *testing.T) (*Repository, sqlmock.Sqlmock) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("new sqlmock: %v", err)
	}
	t.Cleanup(func() {
		_ = sqlDB.Close()
	})
	db, err := gorm.Open(postgres.New(postgres.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open gorm postgres mock: %v", err)
	}
	return New(db), mock
}
