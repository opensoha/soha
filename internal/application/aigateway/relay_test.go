package aigateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	appaccess "github.com/opensoha/soha/internal/application/access"
	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainaudit "github.com/opensoha/soha/internal/domain/audit"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
	"github.com/opensoha/soha/internal/platform/secretcrypto"
)

const relayTestEncryptionKey = "relay-test-encryption-key"

type relayTestRepository struct {
	mu sync.Mutex

	upstreams []domainaigateway.LLMUpstream
	routes    []domainaigateway.LLMModelRoute
	callLogs  []domainaigateway.LLMCallLog
	caches    []domainaigateway.LLMCacheEntry
	events    []domainaigateway.LLMHealthEvent

	createCallLogSawCanceledContext bool
}

func (r *relayTestRepository) ListLLMUpstreams(_ context.Context, filter domainaigateway.LLMUpstreamFilter) ([]domainaigateway.LLMUpstream, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]domainaigateway.LLMUpstream, 0, len(r.upstreams))
	for _, item := range r.upstreams {
		if filter.ProviderKind != "" && !relayUpstreamActiveForProvider(item, filter.ProviderKind) {
			continue
		}
		if filter.Status != "" && !strings.EqualFold(item.Status, filter.Status) {
			continue
		}
		if !filter.IncludeAll && !strings.EqualFold(item.Status, "active") {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *relayTestRepository) GetLLMUpstream(_ context.Context, upstreamID string) (domainaigateway.LLMUpstream, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range r.upstreams {
		if item.ID == upstreamID {
			return item, nil
		}
	}
	return domainaigateway.LLMUpstream{}, fmt.Errorf("%w: upstream not found", apperrors.ErrNotFound)
}

func (r *relayTestRepository) CreateLLMUpstream(_ context.Context, item domainaigateway.LLMUpstream) (domainaigateway.LLMUpstream, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.upstreams = append(r.upstreams, item)
	return item, nil
}

func (r *relayTestRepository) UpdateLLMUpstream(_ context.Context, item domainaigateway.LLMUpstream) (domainaigateway.LLMUpstream, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index := range r.upstreams {
		if r.upstreams[index].ID == item.ID {
			r.upstreams[index] = item
			return item, nil
		}
	}
	return domainaigateway.LLMUpstream{}, fmt.Errorf("%w: upstream not found", apperrors.ErrNotFound)
}

func (r *relayTestRepository) ListLLMModelRoutes(_ context.Context, filter domainaigateway.LLMModelRouteFilter) ([]domainaigateway.LLMModelRoute, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]domainaigateway.LLMModelRoute, 0, len(r.routes))
	for _, item := range r.routes {
		if filter.PublicModel != "" && item.PublicModel != filter.PublicModel {
			continue
		}
		if filter.ProviderKind != "" && !relayRouteProviderMatches(item.ProviderKind, filter.ProviderKind) {
			continue
		}
		if filter.UpstreamID != "" && item.UpstreamID != filter.UpstreamID {
			continue
		}
		if filter.RouteGroup != "" && item.RouteGroup != filter.RouteGroup {
			continue
		}
		if !filter.IncludeDisabled && !item.Enabled {
			continue
		}
		items = append(items, item)
	}
	return items, nil
}

func (r *relayTestRepository) GetLLMModelRoute(_ context.Context, routeID string) (domainaigateway.LLMModelRoute, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range r.routes {
		if item.ID == routeID {
			return item, nil
		}
	}
	return domainaigateway.LLMModelRoute{}, fmt.Errorf("%w: route not found", apperrors.ErrNotFound)
}

func (r *relayTestRepository) CreateLLMModelRoute(_ context.Context, item domainaigateway.LLMModelRoute) (domainaigateway.LLMModelRoute, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.routes = append(r.routes, item)
	return item, nil
}

func (r *relayTestRepository) UpdateLLMModelRoute(_ context.Context, item domainaigateway.LLMModelRoute) (domainaigateway.LLMModelRoute, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index := range r.routes {
		if r.routes[index].ID == item.ID {
			r.routes[index] = item
			return item, nil
		}
	}
	return domainaigateway.LLMModelRoute{}, fmt.Errorf("%w: route not found", apperrors.ErrNotFound)
}

func (r *relayTestRepository) DeleteLLMModelRoute(_ context.Context, routeID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index := range r.routes {
		if r.routes[index].ID == routeID {
			r.routes = append(r.routes[:index], r.routes[index+1:]...)
			return nil
		}
	}
	return fmt.Errorf("%w: route not found", apperrors.ErrNotFound)
}

func (r *relayTestRepository) ListLLMCallLogs(_ context.Context, filter domainaigateway.LLMCallLogFilter) ([]domainaigateway.LLMCallLog, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]domainaigateway.LLMCallLog, 0, len(r.callLogs))
	for _, item := range r.callLogs {
		if !relayCallLogMatchesFilter(item, filter) {
			continue
		}
		items = append(items, item)
		if filter.Limit > 0 && len(items) >= filter.Limit {
			break
		}
	}
	return items, nil
}

func relayCallLogMatchesFilter(item domainaigateway.LLMCallLog, filter domainaigateway.LLMCallLogFilter) bool {
	return (filter.TokenID == "" || item.TokenID == filter.TokenID) &&
		(filter.TokenPrefix == "" || item.TokenPrefix == filter.TokenPrefix) &&
		(filter.TokenKind == "" || item.TokenKind == filter.TokenKind) &&
		(filter.Status == "" || item.Status == filter.Status) &&
		(filter.PublicModel == "" || item.PublicModel == filter.PublicModel) &&
		(filter.UpstreamID == "" || item.UpstreamID == filter.UpstreamID) &&
		(filter.ProviderKind == "" || item.ProviderKind == filter.ProviderKind) &&
		(filter.From == nil || !item.CreatedAt.Before(*filter.From)) &&
		(filter.To == nil || !item.CreatedAt.After(*filter.To))
}

func (r *relayTestRepository) LLMRelayCallLogMetrics(ctx context.Context, filter domainaigateway.LLMCallLogFilter) (domainaigateway.LLMRelayCallLogMetrics, error) {
	filter.Limit = 0
	logs, err := r.ListLLMCallLogs(ctx, filter)
	if err != nil {
		return domainaigateway.LLMRelayCallLogMetrics{}, err
	}
	metrics := relayMetricsFromLogs(logs, nil, time.Now().UTC())
	return domainaigateway.LLMRelayCallLogMetrics{
		TotalCalls:        metrics.TotalCalls,
		SuccessCount:      metrics.SuccessCount,
		FailureCount:      metrics.FailureCount,
		AverageTTFBMs:     metrics.AverageTTFBMs,
		AverageTTFTMs:     metrics.AverageTTFTMs,
		AverageDurationMs: metrics.AverageDurationMs,
		TokensPerSecond:   metrics.TokensPerSecond,
		CacheHitCount:     metrics.CacheHitCount,
		CacheReadTokens:   metrics.CacheReadTokens,
		CacheWriteTokens:  metrics.CacheWriteTokens,
		ModelRanking:      metrics.ModelRanking,
		RecentErrors:      metrics.RecentErrors,
	}, nil
}

func (r *relayTestRepository) SumLLMCallTokens(_ context.Context, filter domainaigateway.LLMCallLogFilter) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	total := 0
	for _, item := range r.callLogs {
		if filter.TokenID != "" && item.TokenID != filter.TokenID {
			continue
		}
		if filter.TokenPrefix != "" && item.TokenPrefix != filter.TokenPrefix {
			continue
		}
		if filter.TokenKind != "" && item.TokenKind != filter.TokenKind {
			continue
		}
		if filter.Status != "" && item.Status != filter.Status {
			continue
		}
		if filter.PublicModel != "" && item.PublicModel != filter.PublicModel {
			continue
		}
		if filter.UpstreamID != "" && item.UpstreamID != filter.UpstreamID {
			continue
		}
		if filter.ProviderKind != "" && item.ProviderKind != filter.ProviderKind {
			continue
		}
		if filter.From != nil && item.CreatedAt.Before(*filter.From) {
			continue
		}
		if filter.To != nil && item.CreatedAt.After(*filter.To) {
			continue
		}
		total += relayCallLogTotalTokens(item)
	}
	return total, nil
}

func (r *relayTestRepository) LLMRelayCacheLogStats(_ context.Context, filter domainaigateway.LLMCallLogFilter) (domainaigateway.LLMRelayCacheLogStats, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	stats := domainaigateway.LLMRelayCacheLogStats{}
	byModel := map[string]map[string]any{}
	byUpstream := map[string]map[string]any{}
	for _, item := range r.callLogs {
		if filter.PublicModel != "" && item.PublicModel != filter.PublicModel {
			continue
		}
		if filter.UpstreamID != "" && item.UpstreamID != filter.UpstreamID {
			continue
		}
		if filter.From != nil && item.CreatedAt.Before(*filter.From) {
			continue
		}
		if filter.To != nil && item.CreatedAt.After(*filter.To) {
			continue
		}
		switch item.CacheStatus {
		case relayCacheHit:
			stats.ResponseCacheHits++
		case relayCacheMiss:
			stats.ResponseCacheMisses++
		case relayCacheWrite:
			stats.ResponseCacheWrites++
		case relayCacheBypass:
			stats.ResponseCacheBypasses++
		}
		stats.ProviderCachedReadTokens += item.CachedReadTokens
		stats.ProviderCachedWriteTokens += item.CachedWriteTokens
		mergeRelayTestCacheBreakdown(byModel, "publicModel", item.PublicModel, item)
		mergeRelayTestCacheBreakdown(byUpstream, "upstreamId", item.UpstreamID, item)
	}
	for _, item := range byModel {
		stats.ByModel = append(stats.ByModel, item)
	}
	for _, item := range byUpstream {
		stats.ByUpstream = append(stats.ByUpstream, item)
	}
	return stats, nil
}

func mergeRelayTestCacheBreakdown(items map[string]map[string]any, label, value string, log domainaigateway.LLMCallLog) {
	if strings.TrimSpace(value) == "" {
		return
	}
	item := items[value]
	if item == nil {
		item = map[string]any{label: value}
		items[value] = item
	}
	for _, key := range []string{"responseCacheHits", "responseCacheMisses", "responseCacheWrites", "responseCacheBypasses", "providerCachedReadTokens", "providerCachedWriteTokens"} {
		if item[key] == nil {
			item[key] = 0
		}
	}
	switch log.CacheStatus {
	case relayCacheHit:
		item["responseCacheHits"] = intFromAny(item["responseCacheHits"]) + 1
	case relayCacheMiss:
		item["responseCacheMisses"] = intFromAny(item["responseCacheMisses"]) + 1
	case relayCacheWrite:
		item["responseCacheWrites"] = intFromAny(item["responseCacheWrites"]) + 1
	case relayCacheBypass:
		item["responseCacheBypasses"] = intFromAny(item["responseCacheBypasses"]) + 1
	}
	item["providerCachedReadTokens"] = intFromAny(item["providerCachedReadTokens"]) + log.CachedReadTokens
	item["providerCachedWriteTokens"] = intFromAny(item["providerCachedWriteTokens"]) + log.CachedWriteTokens
}

func (r *relayTestRepository) CreateLLMCallLog(ctx context.Context, item domainaigateway.LLMCallLog) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ctx.Err() != nil {
		r.createCallLogSawCanceledContext = true
		return ctx.Err()
	}
	r.callLogs = append(r.callLogs, item)
	return nil
}

func (r *relayTestRepository) GetLLMCacheEntryByKey(_ context.Context, cacheKey string) (domainaigateway.LLMCacheEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, item := range r.caches {
		if item.CacheKey == cacheKey {
			return item, nil
		}
	}
	return domainaigateway.LLMCacheEntry{}, fmt.Errorf("%w: cache entry not found", apperrors.ErrNotFound)
}

func (r *relayTestRepository) CountLLMCacheEntries(_ context.Context, filter domainaigateway.LLMCacheEntryFilter) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for _, item := range r.caches {
		if relayTestCacheEntryMatchesFilter(item, filter) {
			count++
		}
	}
	return count, nil
}

func (r *relayTestRepository) CreateLLMCacheEntry(_ context.Context, item domainaigateway.LLMCacheEntry) (domainaigateway.LLMCacheEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.caches = append(r.caches, item)
	return item, nil
}

func (r *relayTestRepository) UpdateLLMCacheEntry(_ context.Context, item domainaigateway.LLMCacheEntry) (domainaigateway.LLMCacheEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for index := range r.caches {
		if r.caches[index].ID == item.ID {
			r.caches[index] = item
			return item, nil
		}
	}
	return domainaigateway.LLMCacheEntry{}, fmt.Errorf("%w: cache entry not found", apperrors.ErrNotFound)
}

func (r *relayTestRepository) DeleteLLMCacheEntries(_ context.Context, filter domainaigateway.LLMCacheEntryFilter) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	kept := r.caches[:0]
	deleted := 0
	for _, item := range r.caches {
		if relayTestCacheEntryMatchesFilter(item, filter) {
			deleted++
			continue
		}
		kept = append(kept, item)
	}
	r.caches = kept
	return deleted, nil
}

func relayTestCacheEntryMatchesFilter(item domainaigateway.LLMCacheEntry, filter domainaigateway.LLMCacheEntryFilter) bool {
	if filter.PublicModel != "" && item.PublicModel != filter.PublicModel {
		return false
	}
	if filter.UpstreamID != "" && item.UpstreamID != filter.UpstreamID {
		return false
	}
	if filter.Status != "" && item.Status != filter.Status {
		return false
	}
	if filter.UpdatedBefore != nil && item.UpdatedAt.After(*filter.UpdatedBefore) {
		return false
	}
	return true
}

func (r *relayTestRepository) CreateLLMHealthEvent(_ context.Context, item domainaigateway.LLMHealthEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, item)
	return nil
}

func (r *relayTestRepository) logs() []domainaigateway.LLMCallLog {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domainaigateway.LLMCallLog(nil), r.callLogs...)
}

func (r *relayTestRepository) cacheEntries() []domainaigateway.LLMCacheEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domainaigateway.LLMCacheEntry(nil), r.caches...)
}

func (r *relayTestRepository) healthEvents() []domainaigateway.LLMHealthEvent {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]domainaigateway.LLMHealthEvent(nil), r.events...)
}

func (r *relayTestRepository) sawCanceledLogContext() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.createCallLogSawCanceledContext
}

func TestRelayLLMHTTPListsOpenAIModelsWithRestrictedToken(t *testing.T) {
	repo := &relayTestRepository{
		routes: []domainaigateway.LLMModelRoute{
			{ID: "route-1", PublicModel: "gpt-public", ProviderKind: "openai", UpstreamModel: "gpt-upstream", Enabled: true},
			{ID: "route-2", PublicModel: "gpt-private", ProviderKind: "openai", UpstreamModel: "gpt-private", Enabled: true},
			{ID: "route-3", PublicModel: "gpt-transformed", ProviderKind: "anthropic", UpstreamModel: "claude-upstream", Enabled: true, TransformPolicy: map[string]any{"mode": "convert", "targetProviderKind": "anthropic"}},
		},
	}
	service := newRelayRuntimeTestService(repo, nil)
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
		"purpose":            LLMRelayTokenPurpose,
		"allowedModels":      []string{"gpt-public", "gpt-transformed"},
		"allowedUpstreamIds": []string{"upstream-openai"},
	}), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "models",
		Method:       http.MethodGet,
		Headers:      http.Header{},
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP models returned error: %v", err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var payload struct {
		Object string `json:"object"`
		Data   []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode models response: %v", err)
	}
	if payload.Object != "list" || len(payload.Data) != 2 || payload.Data[0].ID != "gpt-public" || payload.Data[1].ID != "gpt-transformed" {
		t.Fatalf("models response = %#v", payload)
	}
}

func TestRelayLLMHTTPModelsHideTeamRestrictedRoutes(t *testing.T) {
	repo := &relayTestRepository{
		upstreams: []domainaigateway.LLMUpstream{
			{ID: "upstream-platform", Name: "platform", ProviderKind: "openai", Status: "active", Metadata: map[string]any{
				"allowedTeams": []string{"platform"},
			}},
			{ID: "upstream-finance", Name: "finance", ProviderKind: "openai", Status: "active", Metadata: map[string]any{
				"allowedTeams": []string{"finance"},
			}},
		},
		routes: []domainaigateway.LLMModelRoute{
			{ID: "route-platform", PublicModel: "gpt-platform", ProviderKind: "openai", UpstreamID: "upstream-platform", UpstreamModel: "gpt-upstream", Enabled: true, Metadata: map[string]any{
				"teamPolicy": map[string]any{"allowedTeams": []string{"platform"}},
			}},
			{ID: "route-finance", PublicModel: "gpt-finance", ProviderKind: "openai", UpstreamID: "upstream-platform", UpstreamModel: "gpt-upstream", Enabled: true, Metadata: map[string]any{
				"allowedTeams": []string{"finance"},
			}},
			{ID: "route-denied", PublicModel: "gpt-denied", ProviderKind: "openai", UpstreamID: "upstream-platform", UpstreamModel: "gpt-upstream", Enabled: true, Metadata: map[string]any{
				"deniedTeams": []string{"platform"},
			}},
			{ID: "route-upstream-denied", PublicModel: "gpt-upstream-denied", ProviderKind: "openai", UpstreamID: "upstream-finance", UpstreamModel: "gpt-upstream", Enabled: true},
		},
	}
	service := newRelayRuntimeTestService(repo, nil)
	principal := relayTestPrincipal()
	principal.Teams = []string{"platform"}
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), principal, relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "models",
		Method:       http.MethodGet,
		Headers:      http.Header{},
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP models returned error: %v", err)
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode models response: %v", err)
	}
	if len(payload.Data) != 1 || payload.Data[0].ID != "gpt-platform" {
		t.Fatalf("models response = %#v", payload)
	}
}

func TestRelayLLMHTTPProxiesOpenAIChatCompletionAndRecordsUsage(t *testing.T) {
	const upstreamKey = "sk-openai-upstream-test"
	requests := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s, want /v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+upstreamKey {
			t.Errorf("Authorization = %q", got)
		}
		payload := decodeRelayTestJSON(t, r.Body)
		requests <- payload
		time.Sleep(10 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "upstream-request-1")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-1","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":7,"completion_tokens":11,"total_tokens":18,"prompt_tokens_details":{"cached_tokens":3},"completion_tokens_details":{"reasoning_tokens":2}}}`)
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	service := newRelayRuntimeTestService(repo, upstream.Client())
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
		"purpose":              LLMRelayTokenPurpose,
		"allowedModels":        []string{"gpt-public"},
		"allowedProviderKinds": []string{"openai"},
		"allowedUpstreamIds":   []string{"upstream-openai"},
	}), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{"Accept": []string{"application/json"}},
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"hi"}],"custom":"preserved"}`),
		RequestID:    "req-openai-nonstream",
		SourceIP:     "203.0.113.8",
		UserAgent:    "relay-test",
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	upstreamPayload := <-requests
	assertOpenAIChatRelayResult(t, recorder, upstreamPayload, singleRelayLog(t, repo))
}

func TestInvokeWorkbenchModelUsesRelayRouteAndRecordsWorkbenchMetadata(t *testing.T) {
	const upstreamKey = "sk-workbench-upstream-test"
	requests := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s, want /v1/chat/completions", r.URL.Path)
		}
		payload := decodeRelayTestJSON(t, r.Body)
		requests <- payload
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-workbench","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"workbench answer"}}],"usage":{"prompt_tokens":3,"completion_tokens":5,"total_tokens":8}}`)
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	repo.routes[0].ID = "route-workbench"
	service := newRelayRuntimeTestService(repo, upstream.Client())
	principal := relayWorkbenchTestPrincipal()
	principal.Teams = []string{"platform"}
	resp, err := service.InvokeWorkbenchModel(context.Background(), principal, WorkbenchRelayRequest{
		PublicModel: "gpt-public",
		RouteID:     "route-workbench",
		Endpoint:    "chat/completions",
		SessionID:   "session-1",
		Mode:        "general",
		Messages: []WorkbenchRelayMessage{
			{Role: "system", Content: "be concise"},
			{Role: "user", Content: "hi"},
		},
	})

	if err != nil {
		t.Fatalf("InvokeWorkbenchModel returned error: %v", err)
	}
	if resp.Content != "workbench answer" || resp.PublicModel != "gpt-public" || resp.RouteID != "route-workbench" {
		t.Fatalf("unexpected workbench response: %#v", resp)
	}
	upstreamPayload := <-requests
	if upstreamPayload["model"] != "gpt-upstream" {
		t.Fatalf("upstream model = %#v, want gpt-upstream", upstreamPayload["model"])
	}
	messages, _ := upstreamPayload["messages"].([]any)
	if len(messages) != 2 {
		t.Fatalf("expected system and user messages, got %#v", upstreamPayload["messages"])
	}
	log := singleRelayLog(t, repo)
	if log.TokenKind != workbenchRelayTokenKind || log.ActorID != "user-1" || log.RouteTrace["routeId"] != "route-workbench" {
		t.Fatalf("unexpected workbench call log identity/route fields: %#v", log)
	}
	if log.Metadata["source"] != workbenchRelaySource || log.Metadata["sessionId"] != "session-1" || log.Metadata["workbenchMode"] != "general" || log.Metadata["internal"] != true {
		t.Fatalf("expected workbench metadata, got %#v", log.Metadata)
	}
	if _, ok := log.Metadata["prompt"]; ok {
		t.Fatalf("call log must not store prompt text: %#v", log.Metadata)
	}
}

func TestInvokeWorkbenchModelStreamParsesOpenAIDeltasAndRecordsStreamCall(t *testing.T) {
	const upstreamKey = "sk-workbench-stream-upstream-test"
	requests := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s, want /v1/chat/completions", r.URL.Path)
		}
		payload := decodeRelayTestJSON(t, r.Body)
		requests <- payload
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hello\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\" stream\"}}],\"usage\":{\"prompt_tokens\":3,\"completion_tokens\":2,\"total_tokens\":5}}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	repo.routes[0].ID = "route-workbench-stream"
	service := newRelayRuntimeTestService(repo, upstream.Client())
	deltas := []string{}
	resp, err := service.InvokeWorkbenchModelStream(context.Background(), relayWorkbenchTestPrincipal(), WorkbenchRelayRequest{
		PublicModel: "gpt-public",
		RouteID:     "route-workbench-stream",
		Endpoint:    "chat/completions",
		SessionID:   "session-1",
		Mode:        "general",
		Messages:    []WorkbenchRelayMessage{{Role: "user", Content: "hi"}},
	}, func(delta WorkbenchRelayStreamDelta) bool {
		deltas = append(deltas, delta.ContentDelta)
		return true
	})

	if err != nil {
		t.Fatalf("InvokeWorkbenchModelStream returned error: %v", err)
	}
	if resp.Content != "hello stream" || resp.RouteID != "route-workbench-stream" {
		t.Fatalf("unexpected stream response: %#v", resp)
	}
	if strings.Join(deltas, "") != "hello stream" || len(deltas) != 2 {
		t.Fatalf("unexpected stream deltas: %#v", deltas)
	}
	upstreamPayload := <-requests
	if upstreamPayload["stream"] != true || upstreamPayload["model"] != "gpt-upstream" {
		t.Fatalf("upstream stream payload = %#v", upstreamPayload)
	}
	log := singleRelayLog(t, repo)
	if !log.Stream || log.Status != "success" || log.PromptTokens != 3 || log.CompletionTokens != 2 || log.TotalTokens != 5 {
		t.Fatalf("unexpected workbench stream log: %#v", log)
	}
}

func TestInvokeWorkbenchModelStreamReturnsStoppedWhenDeltaSinkStops(t *testing.T) {
	const upstreamKey = "sk-workbench-stream-stop-test"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\" should not persist\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	repo.routes[0].ID = "route-workbench-stream-stop"
	service := newRelayRuntimeTestService(repo, upstream.Client())
	deltas := []string{}
	resp, err := service.InvokeWorkbenchModelStream(context.Background(), relayWorkbenchTestPrincipal(), WorkbenchRelayRequest{
		PublicModel: "gpt-public",
		RouteID:     "route-workbench-stream-stop",
		Endpoint:    "chat/completions",
		SessionID:   "session-1",
		Mode:        "general",
		Messages:    []WorkbenchRelayMessage{{Role: "user", Content: "hi"}},
	}, func(delta WorkbenchRelayStreamDelta) bool {
		deltas = append(deltas, delta.ContentDelta)
		return false
	})

	if !errors.Is(err, ErrWorkbenchRelayStreamStopped) {
		t.Fatalf("InvokeWorkbenchModelStream error = %v, want ErrWorkbenchRelayStreamStopped", err)
	}
	if resp.Content != "" {
		t.Fatalf("stopped stream must not return partial content: %#v", resp)
	}
	if strings.Join(deltas, "") != "partial" {
		t.Fatalf("unexpected deltas before stop: %#v", deltas)
	}
	log := singleRelayLog(t, repo)
	if log.Status != "client_cancelled" || log.ErrorCode != "client_cancelled" {
		t.Fatalf("expected client_cancelled relay log, got %#v", log)
	}
}

func TestInvokeWorkbenchModelFiltersByRouteID(t *testing.T) {
	const upstreamKey = "sk-workbench-route-test"
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "wrong route", http.StatusInternalServerError)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-workbench-route","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"selected route"}}]}`)
	}))
	defer second.Close()

	repo := relayRepoForTwoUpstreams(t, first.URL, second.URL, upstreamKey)
	repo.routes[0].ID = "route-wrong"
	repo.routes[1].ID = "route-selected"
	service := newRelayRuntimeTestService(repo, first.Client())
	resp, err := service.InvokeWorkbenchModel(context.Background(), relayWorkbenchTestPrincipal(), WorkbenchRelayRequest{
		PublicModel: "gpt-public",
		RouteID:     "route-selected",
		Endpoint:    "chat/completions",
		Messages:    []WorkbenchRelayMessage{{Role: "user", Content: "hi"}},
	})

	if err != nil {
		t.Fatalf("InvokeWorkbenchModel returned error: %v", err)
	}
	if resp.Content != "selected route" || resp.RouteID != "route-selected" || resp.UpstreamID != "upstream-second" {
		t.Fatalf("unexpected selected route response: %#v", resp)
	}
}

func TestInvokeWorkbenchModelUsesFirstClassOpenAICompatibleRouteProvider(t *testing.T) {
	const upstreamKey = "sk-workbench-deepseek-test"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s, want /v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+upstreamKey {
			t.Errorf("Authorization = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-workbench-deepseek","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"deepseek route"}}]}`)
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "deepseek", upstreamKey)
	repo.routes[0].ID = "route-deepseek"
	service := newRelayRuntimeTestService(repo, upstream.Client())
	resp, err := service.InvokeWorkbenchModel(context.Background(), relayWorkbenchTestPrincipal(), WorkbenchRelayRequest{
		PublicModel: "gpt-public",
		RouteID:     "route-deepseek",
		Endpoint:    "chat/completions",
		Messages:    []WorkbenchRelayMessage{{Role: "user", Content: "hi"}},
	})

	if err != nil {
		t.Fatalf("InvokeWorkbenchModel returned error: %v", err)
	}
	if resp.Content != "deepseek route" || resp.RouteID != "route-deepseek" || resp.ProviderKind != "deepseek" {
		t.Fatalf("unexpected workbench response: %#v", resp)
	}
	log := singleRelayLog(t, repo)
	if log.ProviderKind != "deepseek" || log.Endpoint != "chat/completions" {
		t.Fatalf("unexpected workbench call log provider fields: %#v", log)
	}
}

func TestInvokeWorkbenchModelPreservesOpenAIToAnthropicTransformRoute(t *testing.T) {
	const upstreamKey = "sk-workbench-anthropic-transform-test"
	requests := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s, want /v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != upstreamKey {
			t.Errorf("x-api-key = %q", got)
		}
		requests <- decodeRelayTestJSON(t, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg-workbench-transform","type":"message","role":"assistant","content":[{"type":"text","text":"converted route"}],"stop_reason":"end_turn","usage":{"input_tokens":4,"output_tokens":6}}`)
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "anthropic", upstreamKey)
	repo.routes[0].ID = "route-anthropic-transform"
	repo.routes[0].ProviderKind = "anthropic"
	repo.routes[0].TransformPolicy = map[string]any{"mode": "convert", "targetProviderKind": "anthropic"}
	service := newRelayRuntimeTestService(repo, upstream.Client())
	resp, err := service.InvokeWorkbenchModel(context.Background(), relayWorkbenchTestPrincipal(), WorkbenchRelayRequest{
		PublicModel: "gpt-public",
		RouteID:     "route-anthropic-transform",
		Endpoint:    "chat/completions",
		Messages: []WorkbenchRelayMessage{
			{Role: "system", Content: "be concise"},
			{Role: "user", Content: "hi"},
		},
	})

	if err != nil {
		t.Fatalf("InvokeWorkbenchModel returned error: %v", err)
	}
	if resp.Content != "converted route" || resp.RouteID != "route-anthropic-transform" || resp.ProviderKind != "anthropic" {
		t.Fatalf("unexpected workbench response: %#v", resp)
	}
	upstreamPayload := <-requests
	if upstreamPayload["system"] != "be concise" || upstreamPayload["model"] != "gpt-upstream" {
		t.Fatalf("upstream payload = %#v", upstreamPayload)
	}
	log := singleRelayLog(t, repo)
	if log.ProviderKind != "openai" || log.Metadata["upstreamProviderKind"] != "anthropic" {
		t.Fatalf("unexpected workbench transform log fields: %#v", log)
	}
}

func realtimeRelayHandler(t *testing.T, service *Service, claims map[string]any, requestID string) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		err := service.RelayLLMWebSocket(r.Context(), relayTestPrincipal(), relayTestAccessContext(claims), LLMRelayHTTPRequest{
			ProviderKind: "openai", Endpoint: "realtime", QueryModel: r.URL.Query().Get("model"), Method: r.Method,
			Headers: r.Header.Clone(), RequestID: requestID, SourceIP: "203.0.113.8", UserAgent: "relay-test",
		}, w, r)
		if err != nil {
			t.Errorf("RelayLLMWebSocket returned error: %v", err)
		}
	}
}

func realtimeEchoUpstreamHandler(t *testing.T, seen chan<- *http.Request) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, http.Header{"X-Request-Id": []string{"upstream-realtime-1"}})
		if err != nil {
			t.Errorf("upgrade upstream websocket: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		seen <- r.Clone(context.Background())
		messageType, payload, err := conn.ReadMessage()
		if err != nil || messageType != websocket.TextMessage || string(payload) != `{"type":"session.update"}` {
			t.Errorf("upstream message = type %d body %s err=%v", messageType, payload, err)
			return
		}
		if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"session.created"}`)); err != nil {
			t.Errorf("write upstream websocket: %v", err)
		}
	}
}

func realtimeReadyUpstreamHandler(t *testing.T, seen chan<- *http.Request) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade second upstream websocket: %v", err)
			return
		}
		defer func() { _ = conn.Close() }()
		seen <- r.Clone(context.Background())
		if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"ready"}`)); err != nil {
			t.Errorf("write second upstream websocket: %v", err)
		}
	}
}

func TestRelayLLMWebSocketProxiesOpenAIRealtimeAndRecordsCall(t *testing.T) {
	const upstreamKey = "sk-realtime-upstream-test"
	seen := make(chan *http.Request, 1)
	upstream := httptest.NewServer(realtimeEchoUpstreamHandler(t, seen))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	service := newRelayRuntimeTestService(repo, upstream.Client())
	router := http.NewServeMux()
	router.HandleFunc("/api/v1/ai-gateway/llm/openai/v1/realtime", realtimeRelayHandler(t, service, map[string]any{
		"purpose": LLMRelayTokenPurpose, "allowedModels": []string{"gpt-public"},
		"allowedProviderKinds": []string{"openai"}, "allowedUpstreamIds": []string{"upstream-openai"}, "allowRouteTrace": true,
	}, "req-realtime"))
	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/ai-gateway/llm/openai/v1/realtime?model=gpt-public"
	clientConn := dialRealtimeRelay(t, wsURL, http.Header{
		"OpenAI-Organization": []string{"org-realtime"},
		"X-Soha-Route-Trace":  []string{"true"},
	})
	closeWebSocketOnCleanup(t, clientConn)
	if err := clientConn.WriteMessage(websocket.TextMessage, []byte(`{"type":"session.update"}`)); err != nil {
		t.Fatalf("write client websocket: %v", err)
	}
	messageType, payload, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("read client websocket: %v", err)
	}
	if messageType != websocket.TextMessage || string(payload) != `{"type":"session.created"}` {
		t.Fatalf("client message = type %d body %s", messageType, payload)
	}
	_ = clientConn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"))

	assertRealtimeUpstreamRequest(t, seen, upstreamKey)
	waitForRelayCallLog(t, repo, 1)
	log := singleRelayLog(t, repo)
	if log.Endpoint != "realtime" || !log.Stream || log.CacheStatus != relayCacheBypass {
		t.Fatalf("unexpected realtime log fields: %#v", log)
	}
	if log.PublicModel != "gpt-public" || log.UpstreamModel != "gpt-upstream" || log.HTTPStatus != http.StatusSwitchingProtocols {
		t.Fatalf("unexpected route log fields: %#v", log)
	}
	if log.InputBytes == 0 || log.OutputBytes == 0 {
		t.Fatalf("expected websocket byte counts in log: %#v", log)
	}
}

func dialRealtimeRelay(t *testing.T, wsURL string, headers http.Header) *websocket.Conn {
	t.Helper()
	connection, response, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if response != nil && response.Body != nil {
		defer func() { _ = response.Body.Close() }()
	}
	if err != nil {
		if response != nil {
			t.Fatalf("dial relay websocket: status=%d err=%v", response.StatusCode, err)
		}
		t.Fatalf("dial relay websocket: %v", err)
	}
	if response == nil || response.StatusCode != http.StatusSwitchingProtocols || response.Header.Get("X-Soha-Relay-Endpoint") != "realtime" || response.Header.Get("X-Soha-Cache-Status") != relayCacheBypass {
		t.Fatalf("unexpected relay upgrade response: %#v", response)
	}
	return connection
}

func assertRealtimeUpstreamRequest(t *testing.T, seen <-chan *http.Request, upstreamKey string) {
	t.Helper()
	select {
	case request := <-seen:
		if request.URL.Path != "/v1/realtime" || request.URL.Query().Get("model") != "gpt-upstream" {
			t.Fatalf("upstream URL = %s", request.URL.String())
		}
		if request.Header.Get("Authorization") != "Bearer "+upstreamKey || request.Header.Get("OpenAI-Organization") != "org-realtime" {
			t.Fatalf("upstream headers = %#v", request.Header)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for upstream request")
	}
}

func waitForRelayCallLog(t *testing.T, repo *relayTestRepository, minimum int) {
	t.Helper()
	deadline := time.After(time.Second)
	for {
		repo.mu.Lock()
		logCount := len(repo.callLogs)
		repo.mu.Unlock()
		if logCount >= minimum {
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for relay call log")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
}

func TestRelayLLMWebSocketRealtimeFallsBackBeforeClientUpgrade(t *testing.T) {
	const upstreamKey = "sk-realtime-upstream-test"
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "busy", http.StatusServiceUnavailable)
	}))
	defer first.Close()
	secondSeen := make(chan *http.Request, 1)
	second := httptest.NewServer(realtimeReadyUpstreamHandler(t, secondSeen))
	defer second.Close()

	repo := relayRepoForTwoUpstreams(t, first.URL, second.URL, upstreamKey)
	service := newRelayRuntimeTestService(repo, first.Client())
	restore := stubRelayRandomIntn(func(int) int { return 0 })
	defer restore()
	router := http.NewServeMux()
	router.HandleFunc("/api/v1/ai-gateway/llm/openai/v1/realtime", realtimeRelayHandler(t, service, map[string]any{
		"purpose": LLMRelayTokenPurpose, "allowedModels": []string{"gpt-public"},
		"allowedUpstreamIds": []string{"upstream-first", "upstream-second"},
	}, ""))
	server := httptest.NewServer(router)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/api/v1/ai-gateway/llm/openai/v1/realtime?model=gpt-public"
	clientConn, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if resp != nil && resp.Body != nil {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				t.Errorf("close fallback WebSocket upgrade response: %v", err)
			}
		}()
	}
	if err != nil {
		t.Fatalf("dial relay websocket: %v", err)
	}
	closeWebSocketOnCleanup(t, clientConn)
	_, payload, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("read client websocket: %v", err)
	}
	if string(payload) != `{"type":"ready"}` {
		t.Fatalf("client payload = %s", payload)
	}
	if err := clientConn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done")); err != nil {
		t.Fatalf("write client close message: %v", err)
	}

	select {
	case req := <-secondSeen:
		if req.URL.Path != "/v1/realtime" || req.URL.Query().Get("model") != "gpt-upstream" {
			t.Fatalf("second upstream URL = %s", req.URL.String())
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for second upstream request")
	}
	deadline := time.After(time.Second)
	for {
		repo.mu.Lock()
		logCount := len(repo.callLogs)
		repo.mu.Unlock()
		if logCount >= 2 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for fallback call logs")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}
	firstLog := relayLogWithUpstream(t, repo, "upstream-first")
	if firstLog.Status != "failure" || firstLog.ErrorCode != "upstream_5xx" {
		t.Fatalf("first upstream log = %#v", firstLog)
	}
	secondLog := relayLogWithUpstream(t, repo, "upstream-second")
	if secondLog.Endpoint != "realtime" || secondLog.HTTPStatus != http.StatusSwitchingProtocols {
		t.Fatalf("second upstream log = %#v", secondLog)
	}
}

func TestRelayLLMWebSocketRealtimeRequiresModel(t *testing.T) {
	repo := relayRepoForUpstream(t, "https://example.com", "openai", "sk-test")
	service := newRelayRuntimeTestService(repo, nil)
	err := service.RelayLLMWebSocket(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "realtime",
		Method:       http.MethodGet,
		Headers:      http.Header{},
	}, httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/ai-gateway/llm/openai/v1/realtime", nil))
	if err == nil || !strings.Contains(err.Error(), "model query parameter is required") {
		t.Fatalf("RelayLLMWebSocket error = %v", err)
	}
}

func TestRelayLLMHTTPProxiesFirstClassOpenAICompatibleChatCompletion(t *testing.T) {
	for _, providerKind := range []string{"deepseek", "qwen", "openrouter"} {
		t.Run(providerKind, func(t *testing.T) {
			const upstreamKey = "sk-openai-compatible-upstream-test"
			requests := make(chan map[string]any, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1/chat/completions" {
					t.Errorf("path = %s, want /v1/chat/completions", r.URL.Path)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer "+upstreamKey {
					t.Errorf("Authorization = %q", got)
				}
				if got := r.Header.Get("x-api-key"); got != "" {
					t.Errorf("x-api-key should not be forwarded to OpenAI-compatible upstream, got %q", got)
				}
				requests <- decodeRelayTestJSON(t, r.Body)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"chatcmpl-compatible","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":5,"completion_tokens":8,"total_tokens":13}}`)
			}))
			defer upstream.Close()

			repo := relayRepoForUpstream(t, upstream.URL, providerKind, upstreamKey)
			service := newRelayRuntimeTestService(repo, upstream.Client())
			recorder := httptest.NewRecorder()

			err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
				"purpose":              LLMRelayTokenPurpose,
				"allowedModels":        []string{"gpt-public"},
				"allowedProviderKinds": []string{providerKind},
				"allowedUpstreamIds":   []string{"upstream-openai"},
			}), LLMRelayHTTPRequest{
				ProviderKind: providerKind,
				Endpoint:     "chat/completions",
				Method:       http.MethodPost,
				Headers:      http.Header{},
				Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"hi"}],"custom":"preserved"}`),
				RequestID:    "req-" + providerKind,
			}, recorder)

			if err != nil {
				t.Fatalf("RelayLLMHTTP returned error: %v", err)
			}
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
			}
			upstreamPayload := <-requests
			if upstreamPayload["model"] != "gpt-upstream" || upstreamPayload["custom"] != "preserved" {
				t.Fatalf("upstream payload = %#v", upstreamPayload)
			}
			log := singleRelayLog(t, repo)
			if log.ProviderKind != providerKind || log.Endpoint != "chat/completions" || log.UpstreamID != "upstream-openai" {
				t.Fatalf("unexpected log fields: %#v", log)
			}
			if log.PromptTokens != 5 || log.CompletionTokens != 8 || log.TotalTokens != 13 {
				t.Fatalf("unexpected usage log fields: %#v", log)
			}
		})
	}
}

func TestRelayLLMHTTPProxiesAzureOpenAIChatCompletionV1(t *testing.T) {
	const upstreamKey = "sk-azure-openai-upstream-test"
	requests := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openai/v1/chat/completions" {
			t.Errorf("path = %s, want /openai/v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("api-key"); got != upstreamKey {
			t.Errorf("api-key = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization should not be forwarded to Azure OpenAI upstream, got %q", got)
		}
		requests <- decodeRelayTestJSON(t, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-azure","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":6,"completion_tokens":9,"total_tokens":15}}`)
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "azure-openai", upstreamKey)
	service := newRelayRuntimeTestService(repo, upstream.Client())
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
		"purpose":              LLMRelayTokenPurpose,
		"allowedModels":        []string{"gpt-public"},
		"allowedProviderKinds": []string{"azure-openai"},
		"allowedUpstreamIds":   []string{"upstream-openai"},
	}), LLMRelayHTTPRequest{
		ProviderKind: "azure-openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"hi"}]}`),
		RequestID:    "req-azure-openai-v1",
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	upstreamPayload := <-requests
	if upstreamPayload["model"] != "gpt-upstream" {
		t.Fatalf("upstream payload = %#v", upstreamPayload)
	}
	log := singleRelayLog(t, repo)
	if log.ProviderKind != "azure-openai" || log.Endpoint != "chat/completions" || log.UpstreamID != "upstream-openai" {
		t.Fatalf("unexpected log fields: %#v", log)
	}
	if log.PromptTokens != 6 || log.CompletionTokens != 9 || log.TotalTokens != 15 {
		t.Fatalf("unexpected usage log fields: %#v", log)
	}
}

func TestRelayLLMHTTPProxiesAzureOpenAIDeploymentChatCompletion(t *testing.T) {
	const upstreamKey = "sk-azure-openai-deployment-test"
	requests := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/openai/deployments/gpt-upstream/chat/completions" {
			t.Errorf("path = %s, want /openai/deployments/gpt-upstream/chat/completions", r.URL.Path)
		}
		if got := r.URL.Query().Get("api-version"); got != "2024-10-21" {
			t.Errorf("api-version = %q", got)
		}
		if got := r.Header.Get("api-key"); got != upstreamKey {
			t.Errorf("api-key = %q", got)
		}
		requests <- decodeRelayTestJSON(t, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-azure-deployment","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":3,"completion_tokens":4,"total_tokens":7}}`)
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "azure-openai", upstreamKey)
	repo.upstreams[0].Metadata = map[string]any{
		"azureOpenAI": map[string]any{
			"apiVersion": "2024-10-21",
		},
	}
	service := newRelayRuntimeTestService(repo, upstream.Client())
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
		"purpose":              LLMRelayTokenPurpose,
		"allowedModels":        []string{"gpt-public"},
		"allowedProviderKinds": []string{"azure_openai"},
		"allowedUpstreamIds":   []string{"upstream-openai"},
	}), LLMRelayHTTPRequest{
		ProviderKind: "azure_openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"hi"}]}`),
		RequestID:    "req-azure-openai-deployment",
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	upstreamPayload := <-requests
	if upstreamPayload["model"] != "gpt-upstream" {
		t.Fatalf("upstream payload = %#v", upstreamPayload)
	}
	log := singleRelayLog(t, repo)
	if log.ProviderKind != "azure-openai" || log.TotalTokens != 7 {
		t.Fatalf("unexpected log fields: %#v", log)
	}
}

func TestRelayLLMHTTPProxiesGeminiGenerateContent(t *testing.T) {
	const upstreamKey = "gemini-upstream-test-key"
	requests := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-upstream:generateContent" {
			t.Errorf("path = %s, want /v1beta/models/gemini-upstream:generateContent", r.URL.Path)
		}
		if got := r.Header.Get("x-goog-api-key"); got != upstreamKey {
			t.Errorf("x-goog-api-key = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization should not be forwarded to Gemini upstream, got %q", got)
		}
		requests <- decodeRelayTestJSON(t, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"ok"}]}}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":8,"totalTokenCount":15,"thoughtsTokenCount":2,"cachedContentTokenCount":3}}`)
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "gemini", upstreamKey)
	repo.routes[0].PublicModel = "gemini-public"
	repo.routes[0].UpstreamModel = "gemini-upstream"
	service := newRelayRuntimeTestService(repo, upstream.Client())
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
		"purpose":              LLMRelayTokenPurpose,
		"allowedModels":        []string{"gemini-public"},
		"allowedProviderKinds": []string{"gemini"},
		"allowedUpstreamIds":   []string{"upstream-openai"},
	}), LLMRelayHTTPRequest{
		ProviderKind: "gemini",
		Endpoint:     "generateContent",
		PathModel:    "gemini-public",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"contents":[{"role":"user","parts":[{"text":"hello"}]}],"generationConfig":{"temperature":0.2}}`),
		RequestID:    "req-gemini-generate-content",
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	upstreamPayload := <-requests
	if _, hasModel := upstreamPayload["model"]; hasModel {
		t.Fatalf("Gemini native payload should not include model field, got %#v", upstreamPayload)
	}
	if _, ok := upstreamPayload["contents"]; !ok {
		t.Fatalf("upstream payload = %#v, want contents preserved", upstreamPayload)
	}
	log := singleRelayLog(t, repo)
	if log.ProviderKind != "gemini" || log.Endpoint != "generateContent" || log.PublicModel != "gemini-public" || log.UpstreamModel != "gemini-upstream" {
		t.Fatalf("unexpected log fields: %#v", log)
	}
	if log.PromptTokens != 5 || log.CompletionTokens != 8 || log.TotalTokens != 15 || log.ReasoningTokens != 2 || log.CachedReadTokens != 3 {
		t.Fatalf("unexpected usage log fields: %#v", log)
	}
}

type geminiAudioInputCase struct {
	name       string
	audioPart  string
	assertPart func(t *testing.T, part map[string]any)
}

func geminiAudioInputCases(audioData string) []geminiAudioInputCase {
	return []geminiAudioInputCase{
		{
			name:      "inline data",
			audioPart: `{"inlineData":{"mimeType":"audio/wav","data":"` + audioData + `"}}`,
			assertPart: func(t *testing.T, part map[string]any) {
				t.Helper()
				inlineData, _ := part["inlineData"].(map[string]any)
				if inlineData["mimeType"] != "audio/wav" || inlineData["data"] != audioData {
					t.Fatalf("inlineData not preserved: %#v", inlineData)
				}
			},
		},
		{
			name:      "file data",
			audioPart: `{"fileData":{"mimeType":"audio/mpeg","fileUri":"gs://bucket/audio.mp3"}}`,
			assertPart: func(t *testing.T, part map[string]any) {
				t.Helper()
				fileData, _ := part["fileData"].(map[string]any)
				if fileData["mimeType"] != "audio/mpeg" || fileData["fileUri"] != "gs://bucket/audio.mp3" {
					t.Fatalf("fileData not preserved: %#v", fileData)
				}
			},
		},
	}
}

func TestRelayLLMHTTPGeminiGenerateContentAudioInputBypassesCacheAndBase64Estimation(t *testing.T) {
	const upstreamKey = "gemini-audio-upstream-test-key"
	const audioData = "QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVo="
	for _, tt := range geminiAudioInputCases(audioData) {
		t.Run(tt.name, func(t *testing.T) {
			requests := make(chan map[string]any, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/v1beta/models/gemini-audio-upstream:generateContent" {
					t.Errorf("path = %s, want /v1beta/models/gemini-audio-upstream:generateContent", r.URL.Path)
				}
				if got := r.Header.Get("x-goog-api-key"); got != upstreamKey {
					t.Errorf("x-goog-api-key = %q", got)
				}
				requests <- decodeRelayTestJSON(t, r.Body)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"heard"}]}}]}`)
			}))
			defer upstream.Close()

			repo := relayRepoForUpstream(t, upstream.URL, "gemini", upstreamKey)
			repo.routes[0].PublicModel = "gemini-audio-public"
			repo.routes[0].UpstreamModel = "gemini-audio-upstream"
			repo.routes[0].CachePolicy = map[string]any{"enabled": true, "ttlSeconds": 60}
			service := newRelayRuntimeTestService(repo, upstream.Client())
			recorder := httptest.NewRecorder()

			err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
				"purpose":              LLMRelayTokenPurpose,
				"allowedModels":        []string{"gemini-audio-public"},
				"allowedProviderKinds": []string{"gemini"},
				"allowedUpstreamIds":   []string{"upstream-openai"},
			}), LLMRelayHTTPRequest{
				ProviderKind: "gemini",
				Endpoint:     "generateContent",
				PathModel:    "gemini-audio-public",
				Method:       http.MethodPost,
				Headers:      http.Header{},
				Body:         []byte(`{"contents":[{"role":"user","parts":[{"text":"transcribe briefly"},` + tt.audioPart + `]}],"generationConfig":{"temperature":0}}`),
				RequestID:    "req-gemini-audio-generate-content",
			}, recorder)

			if err != nil {
				t.Fatalf("RelayLLMHTTP returned error: %v", err)
			}
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
			}
			upstreamPayload := <-requests
			contents, _ := upstreamPayload["contents"].([]any)
			if len(contents) != 1 {
				t.Fatalf("upstream contents = %#v", upstreamPayload["contents"])
			}
			content, _ := contents[0].(map[string]any)
			parts, _ := content["parts"].([]any)
			if len(parts) != 2 {
				t.Fatalf("upstream parts = %#v", content["parts"])
			}
			audioPart, _ := parts[1].(map[string]any)
			tt.assertPart(t, audioPart)
			if caches := repo.cacheEntries(); len(caches) != 0 {
				t.Fatalf("cache entries = %#v, want none for audio native request", caches)
			}
			log := singleRelayLog(t, repo)
			if log.CacheStatus != relayCacheBypass {
				t.Fatalf("cache status = %q, want bypass", log.CacheStatus)
			}
			if log.PromptTokens != estimateRelayTextTokens("transcribe briefly") || log.CompletionTokens != estimateRelayTextTokens("heard") {
				t.Fatalf("unexpected estimated token fields: %#v", log)
			}
			if log.TotalTokens != log.PromptTokens+log.CompletionTokens || !log.EstimatedTokens {
				t.Fatalf("unexpected estimated token summary: %#v", log)
			}
			if log.PromptTokens >= estimateRelayTextTokens(audioData) {
				t.Fatalf("audio payload appears to be counted as prompt tokens: %#v", log)
			}
		})
	}
}

func TestRelayLLMHTTPGeminiStreamGenerateContentAudioInputBypassesCacheAndParsesSSEUsage(t *testing.T) {
	const upstreamKey = "gemini-audio-stream-upstream-test-key"
	const audioData = "QUJDREVGR0hJSktMTU5PUFFSU1RVVldYWVo="
	requests := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/models/gemini-audio-upstream:streamGenerateContent" {
			t.Errorf("path = %s, want /v1beta/models/gemini-audio-upstream:streamGenerateContent", r.URL.Path)
		}
		if got := r.URL.Query().Get("alt"); got != "sse" {
			t.Errorf("alt = %q, want sse", got)
		}
		requests <- decodeRelayTestJSON(t, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"heard\"}]}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"usageMetadata\":{\"promptTokenCount\":7,\"candidatesTokenCount\":9,\"totalTokenCount\":16,\"cachedContentTokenCount\":2}}\n\n")
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "gemini", upstreamKey)
	repo.routes[0].PublicModel = "gemini-audio-public"
	repo.routes[0].UpstreamModel = "gemini-audio-upstream"
	repo.routes[0].CachePolicy = map[string]any{"enabled": true, "ttlSeconds": 60}
	service := newRelayRuntimeTestService(repo, upstream.Client())
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
		"purpose":              LLMRelayTokenPurpose,
		"allowedModels":        []string{"gemini-audio-public"},
		"allowedProviderKinds": []string{"gemini"},
		"allowedUpstreamIds":   []string{"upstream-openai"},
	}), LLMRelayHTTPRequest{
		ProviderKind: "gemini",
		Endpoint:     "streamGenerateContent",
		PathModel:    "gemini-audio-public",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"contents":[{"role":"user","parts":[{"text":"transcribe briefly"},{"inlineData":{"mimeType":"audio/wav","data":"` + audioData + `"}}]}]}`),
		RequestID:    "req-gemini-audio-stream-generate-content",
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if got := recorder.Header().Get("Content-Type"); got != "text/event-stream" {
		t.Fatalf("content type = %q, want text/event-stream", got)
	}
	upstreamPayload := <-requests
	contents, _ := upstreamPayload["contents"].([]any)
	content, _ := contents[0].(map[string]any)
	parts, _ := content["parts"].([]any)
	audioPart, _ := parts[1].(map[string]any)
	inlineData, _ := audioPart["inlineData"].(map[string]any)
	if inlineData["data"] != audioData {
		t.Fatalf("inlineData not preserved: %#v", inlineData)
	}
	if caches := repo.cacheEntries(); len(caches) != 0 {
		t.Fatalf("cache entries = %#v, want none for audio native stream request", caches)
	}
	log := singleRelayLog(t, repo)
	if !log.Stream || log.Endpoint != "streamGenerateContent" || log.CacheStatus != relayCacheBypass {
		t.Fatalf("unexpected stream log fields: %#v", log)
	}
	if log.PromptTokens != 7 || log.CompletionTokens != 9 || log.TotalTokens != 16 || log.CachedReadTokens != 2 || log.EstimatedTokens {
		t.Fatalf("unexpected SSE usage log fields: %#v", log)
	}
}

func TestRelayLLMHTTPProxiesGeminiInteractions(t *testing.T) {
	const upstreamKey = "gemini-image-upstream-test-key"
	requests := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1beta/interactions" {
			t.Errorf("path = %s, want /v1beta/interactions", r.URL.Path)
		}
		if got := r.Header.Get("x-goog-api-key"); got != upstreamKey {
			t.Errorf("x-goog-api-key = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization should not be forwarded to Gemini upstream, got %q", got)
		}
		requests <- decodeRelayTestJSON(t, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"responseId":"interaction-1","output":[{"mimeType":"image/png","data":"image-bytes"}],"usage":{"total_input_tokens":11,"total_output_tokens":13,"total_tokens":24,"total_cached_tokens":3,"total_thought_tokens":2}}`)
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "gemini", upstreamKey)
	repo.routes[0].PublicModel = "gemini-image-public"
	repo.routes[0].UpstreamModel = "gemini-image-upstream"
	service := newRelayRuntimeTestService(repo, upstream.Client())
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
		"purpose":              LLMRelayTokenPurpose,
		"allowedModels":        []string{"gemini-image-public"},
		"allowedProviderKinds": []string{"gemini"},
		"allowedUpstreamIds":   []string{"upstream-openai"},
	}), LLMRelayHTTPRequest{
		ProviderKind: "gemini",
		Endpoint:     "interactions",
		Method:       http.MethodPost,
		Headers:      http.Header{"Accept": []string{"application/json"}},
		Body:         []byte(`{"model":"gemini-image-public","input":"Create a square product icon","custom":"preserved"}`),
		RequestID:    "req-gemini-interactions",
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	upstreamPayload := <-requests
	if upstreamPayload["model"] != "gemini-image-upstream" || upstreamPayload["input"] != "Create a square product icon" || upstreamPayload["custom"] != "preserved" {
		t.Fatalf("upstream payload = %#v", upstreamPayload)
	}
	assertNativeRelayBody(t, recorder.Body.String(), "image-bytes")
	log := singleRelayLog(t, repo)
	if log.Status != "success" || log.ProviderKind != "gemini" || log.Endpoint != "interactions" || log.PublicModel != "gemini-image-public" || log.UpstreamModel != "gemini-image-upstream" {
		t.Fatalf("unexpected status log fields: %#v", log)
	}
	if log.PromptTokens != 11 || log.CompletionTokens != 13 || log.TotalTokens != 24 || log.CachedReadTokens != 3 || log.ReasoningTokens != 2 {
		t.Fatalf("unexpected interactions usage log fields: %#v", log)
	}
}

func TestRelayLLMHTTPGeminiInteractionsBypassResponseCache(t *testing.T) {
	const upstreamKey = "gemini-image-cache-test-key"
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"responseId":"interaction-cache","output":[{"mimeType":"image/png","data":"image-%d"}]}`, upstreamCalls)
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "gemini", upstreamKey)
	repo.routes[0].PublicModel = "gemini-image-public"
	repo.routes[0].UpstreamModel = "gemini-image-upstream"
	repo.routes[0].CachePolicy = map[string]any{"enabled": true, "ttlSeconds": 60}
	service := newRelayRuntimeTestService(repo, upstream.Client())
	req := LLMRelayHTTPRequest{
		ProviderKind: "gemini",
		Endpoint:     "interactions",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gemini-image-public","input":"Create a square product icon"}`),
	}

	for i := 0; i < 2; i++ {
		if err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
			"purpose":              LLMRelayTokenPurpose,
			"allowedModels":        []string{"gemini-image-public"},
			"allowedProviderKinds": []string{"gemini"},
			"allowedUpstreamIds":   []string{"upstream-openai"},
		}), req, httptest.NewRecorder()); err != nil {
			t.Fatalf("RelayLLMHTTP #%d returned error: %v", i+1, err)
		}
	}
	if upstreamCalls != 2 {
		t.Fatalf("upstream calls = %d, want 2", upstreamCalls)
	}
	if caches := repo.cacheEntries(); len(caches) != 0 {
		t.Fatalf("cache entries = %#v, want none", caches)
	}
	logs := repo.logs()
	if len(logs) != 2 || logs[0].CacheStatus != relayCacheBypass || logs[1].CacheStatus != relayCacheBypass {
		t.Fatalf("cache log statuses = %#v, want bypass/bypass", logs)
	}
}

func TestRelayLLMHTTPRejectsGeminiInteractionsUnsupportedModes(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
	}{
		{name: "streaming", body: `{"model":"gemini-image-public","input":"draw","stream":true}`},
		{name: "background", body: `{"model":"gemini-image-public","input":"draw","background":true}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := relayRepoForUpstream(t, "https://generativelanguage.googleapis.com", "gemini", "gemini-key")
			repo.routes[0].PublicModel = "gemini-image-public"
			service := newRelayRuntimeTestService(repo, nil)
			err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
				"purpose":              LLMRelayTokenPurpose,
				"allowedModels":        []string{"gemini-image-public"},
				"allowedProviderKinds": []string{"gemini"},
				"allowedUpstreamIds":   []string{"upstream-openai"},
			}), LLMRelayHTTPRequest{
				ProviderKind: "gemini",
				Endpoint:     "interactions",
				Method:       http.MethodPost,
				Headers:      http.Header{},
				Body:         []byte(tt.body),
			}, httptest.NewRecorder())

			if !errors.Is(err, apperrors.ErrInvalidArgument) {
				t.Fatalf("error = %v, want invalid argument", err)
			}
			if logs := repo.logs(); len(logs) != 0 {
				t.Fatalf("logs = %#v, want none before upstream call", logs)
			}
		})
	}
}

func TestRelayLLMHTTPProxiesCohereRerank(t *testing.T) {
	const upstreamKey = "cohere-upstream-test-key"
	requests := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/rerank" {
			t.Errorf("path = %s, want /v2/rerank", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+upstreamKey {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Cohere-Api-Key"); got != "" {
			t.Errorf("Cohere-Api-Key should not be forwarded, got %q", got)
		}
		requests <- decodeRelayTestJSON(t, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"rerank-1","results":[{"index":0,"relevance_score":0.98}],"meta":{"billed_units":{"search_units":1}}}`)
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "cohere", upstreamKey)
	repo.routes[0].PublicModel = "rerank-public"
	repo.routes[0].UpstreamModel = "rerank-v3.5"
	service := newRelayRuntimeTestService(repo, upstream.Client())
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
		"purpose":              LLMRelayTokenPurpose,
		"allowedModels":        []string{"rerank-public"},
		"allowedProviderKinds": []string{"cohere"},
		"allowedUpstreamIds":   []string{"upstream-openai"},
	}), LLMRelayHTTPRequest{
		ProviderKind: "cohere",
		Endpoint:     "rerank",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"rerank-public","query":"deployment failed","documents":["pod crashloop","certificate expired"],"top_n":1}`),
		RequestID:    "req-cohere-rerank",
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	upstreamPayload := <-requests
	if upstreamPayload["model"] != "rerank-v3.5" || upstreamPayload["query"] != "deployment failed" {
		t.Fatalf("upstream payload = %#v", upstreamPayload)
	}
	log := singleRelayLog(t, repo)
	if log.ProviderKind != "cohere" || log.Endpoint != "rerank" || log.PublicModel != "rerank-public" || log.UpstreamModel != "rerank-v3.5" {
		t.Fatalf("unexpected log fields: %#v", log)
	}
	if !log.EstimatedTokens || log.TotalTokens <= 0 {
		t.Fatalf("expected local token estimate for Cohere rerank without token usage, got %#v", log)
	}
}

func TestRelayLLMHTTPListsModelsForFirstClassOpenAICompatibleProvider(t *testing.T) {
	repo := relayRepoForUpstream(t, "https://deepseek.example", "deepseek", "sk-deepseek")
	repo.routes = append(repo.routes,
		domainaigateway.LLMModelRoute{
			ID:            "route-qwen",
			PublicModel:   "qwen-public",
			ProviderKind:  "qwen",
			UpstreamID:    "upstream-openai",
			UpstreamModel: "qwen-upstream",
			Enabled:       true,
			Priority:      1,
			Weight:        1,
		},
		domainaigateway.LLMModelRoute{
			ID:            "route-openai",
			PublicModel:   "gpt-public",
			ProviderKind:  "openai",
			UpstreamID:    "upstream-openai",
			UpstreamModel: "gpt-upstream",
			Enabled:       true,
			Priority:      1,
			Weight:        1,
		},
	)
	service := newRelayRuntimeTestService(repo, nil)
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
		"purpose":              LLMRelayTokenPurpose,
		"allowedModels":        []string{"gpt-public", "qwen-public"},
		"allowedProviderKinds": []string{"deepseek"},
	}), LLMRelayHTTPRequest{
		ProviderKind: "deepseek",
		Endpoint:     "models",
		Method:       http.MethodGet,
		Headers:      http.Header{},
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP models returned error: %v", err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var payload struct {
		Object string `json:"object"`
		Data   []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode models response: %v", err)
	}
	if payload.Object != "list" || len(payload.Data) != 1 || payload.Data[0].ID != "gpt-public" {
		t.Fatalf("models response = %#v", payload)
	}
}

func TestRelayLLMHTTPListsGeminiModels(t *testing.T) {
	repo := relayRepoForUpstream(t, "https://generativelanguage.googleapis.com", "gemini", "gemini-key")
	repo.routes[0].PublicModel = "gemini-public"
	repo.routes[0].UpstreamModel = "gemini-upstream"
	service := newRelayRuntimeTestService(repo, nil)
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
		"purpose":              LLMRelayTokenPurpose,
		"allowedModels":        []string{"gemini-public"},
		"allowedProviderKinds": []string{"gemini"},
	}), LLMRelayHTTPRequest{
		ProviderKind: "gemini",
		Endpoint:     "models",
		Method:       http.MethodGet,
		Headers:      http.Header{},
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP models returned error: %v", err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var payload struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode Gemini models response: %v", err)
	}
	if len(payload.Models) != 1 || payload.Models[0].Name != "models/gemini-public" {
		t.Fatalf("models response = %#v", payload)
	}
}

func TestRelayLLMHTTPConvertsOpenAIChatToAnthropicMessagesNonStream(t *testing.T) {
	const upstreamKey = "sk-anthropic-transform-upstream-test"
	requests := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s, want /v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != upstreamKey {
			t.Errorf("x-api-key = %q", got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("Authorization should not be forwarded to Anthropic upstream, got %q", got)
		}
		requests <- decodeRelayTestJSON(t, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg-transform","type":"message","role":"assistant","content":[{"type":"text","text":"converted anthropic"}],"stop_reason":"end_turn","usage":{"input_tokens":8,"output_tokens":13}}`)
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "anthropic", upstreamKey)
	repo.routes[0].ProviderKind = "anthropic"
	repo.routes[0].TransformPolicy = map[string]any{"mode": "convert", "targetProviderKind": "anthropic"}
	service := newRelayRuntimeTestService(repo, upstream.Client())
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
		"purpose":              LLMRelayTokenPurpose,
		"allowedModels":        []string{"gpt-public"},
		"allowedProviderKinds": []string{"openai"},
		"allowedUpstreamIds":   []string{"upstream-openai"},
	}), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","max_tokens":64,"temperature":0.2,"messages":[{"role":"system","content":"be concise"},{"role":"user","content":"hello"}]}`),
		RequestID:    "req-openai-to-anthropic",
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	upstreamPayload := <-requests
	assertOpenAIToAnthropicResult(t, recorder, upstreamPayload, singleRelayLog(t, repo))
}

func TestRelayLLMHTTPRejectsStreamingTransform(t *testing.T) {
	repo := relayRepoForUpstream(t, "https://anthropic.example", "anthropic", "sk-ant-stream-transform")
	repo.routes[0].ProviderKind = "anthropic"
	repo.routes[0].TransformPolicy = map[string]any{"mode": "convert", "targetProviderKind": "anthropic"}
	service := newRelayRuntimeTestService(repo, nil)

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","stream":true,"messages":[{"role":"user","content":"stream"}]}`),
		RequestID:    "req-transform-stream",
	}, httptest.NewRecorder())

	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("error = %v, want invalid argument", err)
	}
	if logs := repo.logs(); len(logs) != 0 {
		t.Fatalf("logs = %#v, want none before upstream call", logs)
	}
}

func TestRelayLLMHTTPRejectsToolTransform(t *testing.T) {
	repo := relayRepoForUpstream(t, "https://anthropic.example", "anthropic", "sk-ant-tool-transform")
	repo.routes[0].ProviderKind = "anthropic"
	repo.routes[0].TransformPolicy = map[string]any{"mode": "convert", "targetProviderKind": "anthropic"}
	service := newRelayRuntimeTestService(repo, nil)

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"tool"}],"tools":[{"type":"function","function":{"name":"lookup"}}]}`),
		RequestID:    "req-transform-tool",
	}, httptest.NewRecorder())

	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("error = %v, want invalid argument", err)
	}
	if logs := repo.logs(); len(logs) != 0 {
		t.Fatalf("logs = %#v, want none before upstream call", logs)
	}
}

func TestRelayLLMHTTPResponseCacheWritesThenHits(t *testing.T) {
	const upstreamKey = "sk-openai-cache-upstream-test"
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s, want /v1/chat/completions", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-Id", "cache-origin")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-cache","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"cached"}}],"usage":{"prompt_tokens":5,"completion_tokens":7,"total_tokens":12}}`)
	}))
	defer upstream.Close()
	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	repo.routes[0].CachePolicy = map[string]any{"enabled": true, "ttlSeconds": 60}
	service := newRelayRuntimeTestService(repo, upstream.Client())
	accessCtx := relayTestAccessContext(map[string]any{
		"purpose":         LLMRelayTokenPurpose,
		"allowRouteTrace": true,
	})
	req := LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{"X-Soha-Route-Trace": []string{"true"}},
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"cache me"}]}`),
		RequestID:    "req-cache",
	}

	first := httptest.NewRecorder()
	if err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), accessCtx, req, first); err != nil {
		t.Fatalf("first RelayLLMHTTP returned error: %v", err)
	}
	if upstreamCalls != 1 {
		t.Fatalf("upstream calls after first = %d, want 1", upstreamCalls)
	}
	if got := first.Header().Get("X-Soha-Cache-Status"); got != relayCacheWrite {
		t.Fatalf("first cache trace status = %q, want %q", got, relayCacheWrite)
	}
	caches := repo.cacheEntries()
	if len(caches) != 1 {
		t.Fatalf("cache entries = %#v, want one", caches)
	}
	if caches[0].ResponseBodyCiphertext == "" || strings.Contains(caches[0].ResponseBodyCiphertext, "cached") {
		t.Fatalf("cache response body should be encrypted, got %q", caches[0].ResponseBodyCiphertext)
	}
	if caches[0].Metadata["messages"] != nil || caches[0].Metadata["prompt"] != nil {
		t.Fatalf("cache metadata should not contain prompt text: %#v", caches[0].Metadata)
	}

	second := httptest.NewRecorder()
	if err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), accessCtx, req, second); err != nil {
		t.Fatalf("second RelayLLMHTTP returned error: %v", err)
	}
	if upstreamCalls != 1 {
		t.Fatalf("upstream calls after cache hit = %d, want 1", upstreamCalls)
	}
	if second.Body.String() != first.Body.String() {
		t.Fatalf("cached body = %s, want %s", second.Body.String(), first.Body.String())
	}
	if got := second.Header().Get("X-Soha-Cache-Status"); got != relayCacheHit {
		t.Fatalf("second cache trace status = %q, want %q", got, relayCacheHit)
	}
	logs := repo.logs()
	if len(logs) != 2 {
		t.Fatalf("logs = %#v, want two", logs)
	}
	if logs[0].CacheStatus != relayCacheWrite || logs[1].CacheStatus != relayCacheHit {
		t.Fatalf("cache log statuses = %q/%q, want write/hit", logs[0].CacheStatus, logs[1].CacheStatus)
	}
	caches = repo.cacheEntries()
	if caches[0].HitCount != 1 || caches[0].LastHitAt == nil {
		t.Fatalf("cache hit counters not updated: %#v", caches[0])
	}
}

func TestRelayLLMHTTPResponseCacheStoresConvertedClientBody(t *testing.T) {
	const upstreamKey = "sk-anthropic-cache-transform-upstream-test"
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s, want /v1/messages", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg-cache-transform","type":"message","role":"assistant","content":[{"type":"text","text":"cached converted"}],"usage":{"input_tokens":3,"output_tokens":4}}`)
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "anthropic", upstreamKey)
	repo.routes[0].ProviderKind = "anthropic"
	repo.routes[0].TransformPolicy = map[string]any{"mode": "convert", "targetProviderKind": "anthropic"}
	repo.routes[0].CachePolicy = map[string]any{"enabled": true, "ttlSeconds": 60}
	service := newRelayRuntimeTestService(repo, upstream.Client())
	accessCtx := relayTestAccessContext(map[string]any{
		"purpose":         LLMRelayTokenPurpose,
		"allowRouteTrace": true,
	})
	req := LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{"X-Soha-Route-Trace": []string{"true"}},
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"cache converted"}]}`),
		RequestID:    "req-cache-transform",
	}

	first := httptest.NewRecorder()
	if err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), accessCtx, req, first); err != nil {
		t.Fatalf("first RelayLLMHTTP returned error: %v", err)
	}
	if upstreamCalls != 1 {
		t.Fatalf("upstream calls after first = %d, want 1", upstreamCalls)
	}
	if got := first.Header().Get("X-Soha-Cache-Status"); got != relayCacheWrite {
		t.Fatalf("first cache trace status = %q, want %q", got, relayCacheWrite)
	}
	var firstPayload map[string]any
	if err := json.Unmarshal(first.Body.Bytes(), &firstPayload); err != nil {
		t.Fatalf("decode first response: %v", err)
	}
	if firstPayload["object"] != "chat.completion" {
		t.Fatalf("first response = %#v, want OpenAI client format", firstPayload)
	}
	caches := repo.cacheEntries()
	if len(caches) != 1 {
		t.Fatalf("cache entries = %#v, want one", caches)
	}
	if caches[0].Metadata["providerKind"] != "openai" || caches[0].Metadata["endpoint"] != "chat/completions" {
		t.Fatalf("cache metadata = %#v, want client provider/endpoint", caches[0].Metadata)
	}

	second := httptest.NewRecorder()
	if err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), accessCtx, req, second); err != nil {
		t.Fatalf("second RelayLLMHTTP returned error: %v", err)
	}
	if upstreamCalls != 1 {
		t.Fatalf("upstream calls after cache hit = %d, want 1", upstreamCalls)
	}
	if second.Body.String() != first.Body.String() {
		t.Fatalf("cached body = %s, want %s", second.Body.String(), first.Body.String())
	}
	if got := second.Header().Get("X-Soha-Cache-Status"); got != relayCacheHit {
		t.Fatalf("second cache trace status = %q, want %q", got, relayCacheHit)
	}
	logs := repo.logs()
	if len(logs) != 2 {
		t.Fatalf("logs = %#v, want two", logs)
	}
	if logs[0].ProviderKind != "openai" || logs[0].Metadata["upstreamProviderKind"] != "anthropic" || logs[1].CacheStatus != relayCacheHit {
		t.Fatalf("unexpected converted cache logs: %#v", logs)
	}
}

func TestRelayLLMHTTPResponseCacheDefaultBypassDoesNotWrite(t *testing.T) {
	const upstreamKey = "sk-openai-cache-default-test"
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-no-cache","object":"chat.completion","choices":[]}`)
	}))
	defer upstream.Close()
	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	service := newRelayRuntimeTestService(repo, upstream.Client())
	req := LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"do not cache by default"}]}`),
	}

	for i := 0; i < 2; i++ {
		if err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), req, httptest.NewRecorder()); err != nil {
			t.Fatalf("RelayLLMHTTP #%d returned error: %v", i+1, err)
		}
	}
	if upstreamCalls != 2 {
		t.Fatalf("upstream calls = %d, want 2", upstreamCalls)
	}
	if caches := repo.cacheEntries(); len(caches) != 0 {
		t.Fatalf("cache entries = %#v, want none", caches)
	}
	for _, log := range repo.logs() {
		if log.CacheStatus != relayCacheBypass {
			t.Fatalf("cache status = %q, want bypass", log.CacheStatus)
		}
	}
}

func TestRelayLLMHTTPResponseCacheModes(t *testing.T) {
	tests := []struct {
		name              string
		mode              string
		wantFirstStatus   string
		wantSecondStatus  string
		wantUpstreamCalls int
	}{
		{name: "bypass", mode: "bypass", wantFirstStatus: relayCacheBypass, wantSecondStatus: relayCacheBypass, wantUpstreamCalls: 2},
		{name: "read only", mode: "read-only", wantFirstStatus: relayCacheWriteSkipped, wantSecondStatus: relayCacheWriteSkipped, wantUpstreamCalls: 2},
		{name: "refresh", mode: "refresh", wantFirstStatus: relayCacheWrite, wantSecondStatus: relayCacheWrite, wantUpstreamCalls: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const upstreamKey = "sk-openai-cache-mode-test"
			upstreamCalls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamCalls++
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"id":"chatcmpl-cache-mode","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"call-%d"}}]}`, upstreamCalls)
			}))
			defer upstream.Close()
			repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
			repo.routes[0].CachePolicy = map[string]any{"enabled": true, "ttlSeconds": 60}
			service := newRelayRuntimeTestService(repo, upstream.Client())
			req := LLMRelayHTTPRequest{
				ProviderKind: "openai",
				Endpoint:     "chat/completions",
				Method:       http.MethodPost,
				Headers:      http.Header{"X-Soha-Cache-Mode": []string{tt.mode}},
				Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"cache mode"}]}`),
			}
			if err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), req, httptest.NewRecorder()); err != nil {
				t.Fatalf("first RelayLLMHTTP returned error: %v", err)
			}
			if err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), req, httptest.NewRecorder()); err != nil {
				t.Fatalf("second RelayLLMHTTP returned error: %v", err)
			}
			if upstreamCalls != tt.wantUpstreamCalls {
				t.Fatalf("upstream calls = %d, want %d", upstreamCalls, tt.wantUpstreamCalls)
			}
			logs := repo.logs()
			if len(logs) != 2 || logs[0].CacheStatus != tt.wantFirstStatus || logs[1].CacheStatus != tt.wantSecondStatus {
				t.Fatalf("cache logs = %#v, want %s/%s", logs, tt.wantFirstStatus, tt.wantSecondStatus)
			}
		})
	}
}

func TestRelayLLMHTTPResponseCacheSkipsStreamingAndToolRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "streaming", body: `{"model":"gpt-public","stream":true,"messages":[{"role":"user","content":"stream"}]}`},
		{name: "tools", body: `{"model":"gpt-public","messages":[{"role":"user","content":"tool"}],"tools":[{"type":"function","function":{"name":"lookup"}}]}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const upstreamKey = "sk-openai-cache-skip-test"
			upstreamCalls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamCalls++
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(tt.body, `"stream":true`) {
					_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\n")
					return
				}
				_, _ = io.WriteString(w, `{"id":"chatcmpl-cache-skip","object":"chat.completion","choices":[]}`)
			}))
			defer upstream.Close()
			repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
			repo.routes[0].CachePolicy = map[string]any{"enabled": true, "ttlSeconds": 60}
			service := newRelayRuntimeTestService(repo, upstream.Client())
			req := LLMRelayHTTPRequest{
				ProviderKind: "openai",
				Endpoint:     "chat/completions",
				Method:       http.MethodPost,
				Headers:      http.Header{},
				Body:         []byte(tt.body),
			}

			if err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), req, httptest.NewRecorder()); err != nil {
				t.Fatalf("RelayLLMHTTP returned error: %v", err)
			}
			if upstreamCalls != 1 {
				t.Fatalf("upstream calls = %d, want 1", upstreamCalls)
			}
			if caches := repo.cacheEntries(); len(caches) != 0 {
				t.Fatalf("cache entries = %#v, want none", caches)
			}
			log := singleRelayLog(t, repo)
			if log.CacheStatus != relayCacheBypass {
				t.Fatalf("cache status = %q, want bypass", log.CacheStatus)
			}
		})
	}
}

func TestLLMRelayCacheStatsAggregatesLogsAndPolicy(t *testing.T) {
	now := time.Now().UTC()
	repo := &relayTestRepository{
		routes: []domainaigateway.LLMModelRoute{
			{ID: "route-1", PublicModel: "gpt-public", UpstreamID: "upstream-openai", UpstreamModel: "gpt-upstream", Enabled: true, CachePolicy: map[string]any{"enabled": true}},
		},
		callLogs: []domainaigateway.LLMCallLog{
			{ID: "hit", PublicModel: "gpt-public", UpstreamID: "upstream-openai", CacheStatus: relayCacheHit, CachedReadTokens: 3, CreatedAt: now.Add(-time.Hour)},
			{ID: "write", PublicModel: "gpt-public", UpstreamID: "upstream-openai", CacheStatus: relayCacheWrite, CachedWriteTokens: 2, CreatedAt: now.Add(-time.Hour)},
			{ID: "old", PublicModel: "gpt-public", UpstreamID: "upstream-openai", CacheStatus: relayCacheHit, CreatedAt: now.Add(-48 * time.Hour)},
		},
	}
	service := newRelayRuntimeTestService(repo, nil)

	stats, err := service.LLMRelayCacheStats(context.Background(), relayTestViewPrincipal(), domainaigateway.LLMRelayCacheStatsRequest{
		WindowHours: 24,
	})

	if err != nil {
		t.Fatalf("LLMRelayCacheStats returned error: %v", err)
	}
	if !stats.ResponseCacheEnabled || stats.WindowHours != 24 {
		t.Fatalf("unexpected cache policy stats: %#v", stats)
	}
	if stats.ResponseCacheHits != 1 || stats.ResponseCacheWrites != 1 || stats.ProviderCachedReadTokens != 3 || stats.ProviderCachedWriteTokens != 2 {
		t.Fatalf("unexpected cache counters: %#v", stats)
	}
	if len(stats.ByModel) == 0 || stats.ByModel[0]["publicModel"] != "gpt-public" {
		t.Fatalf("expected model breakdown, got %#v", stats.ByModel)
	}
}

func TestLLMRelayMetricsAggregatesBeyondLogListLimit(t *testing.T) {
	now := time.Now().UTC()
	logs := make([]domainaigateway.LLMCallLog, 0, 501)
	for index := 0; index < 501; index++ {
		logs = append(logs, domainaigateway.LLMCallLog{
			ID:                   fmt.Sprintf("call-%d", index),
			PublicModel:          "gpt-public",
			Status:               "success",
			TotalTokens:          3,
			DurationMilliseconds: 100,
			CacheStatus:          relayCacheHit,
			CachedReadTokens:     1,
			CreatedAt:            now.Add(-time.Minute),
		})
	}
	repo := &relayTestRepository{
		upstreams: []domainaigateway.LLMUpstream{{ID: "upstream-openai", Status: "active"}},
		callLogs:  logs,
	}
	service := newRelayRuntimeTestService(repo, nil)

	metrics, err := service.LLMRelayMetrics(context.Background(), relayTestViewPrincipal())

	if err != nil {
		t.Fatalf("LLMRelayMetrics() error = %v", err)
	}
	if metrics.RequestsToday != 501 || metrics.TotalCalls != 501 || metrics.SuccessCount != 501 {
		t.Fatalf("metrics counts = %#v, want 501 requests", metrics)
	}
	if metrics.CacheHitCount != 501 || metrics.CacheReadTokens != 501 {
		t.Fatalf("cache metrics = %#v, want full 501-log aggregation", metrics)
	}
	if len(metrics.ModelRanking) != 1 || metrics.ModelRanking[0].Count != 501 {
		t.Fatalf("model ranking = %#v, want 501", metrics.ModelRanking)
	}
}

func TestPurgeLLMRelayCacheDryRunAndDelete(t *testing.T) {
	oldTime := time.Now().UTC().Add(-2 * time.Hour)
	olderThan := time.Now().UTC().Add(-time.Hour)
	newTime := time.Now().UTC()
	repo := &relayTestRepository{
		caches: []domainaigateway.LLMCacheEntry{
			{ID: "cache-old", PublicModel: "gpt-public", UpstreamID: "upstream-openai", UpdatedAt: oldTime},
			{ID: "cache-new", PublicModel: "gpt-public", UpstreamID: "upstream-openai", UpdatedAt: newTime},
			{ID: "cache-other", PublicModel: "gpt-other", UpstreamID: "upstream-openai", UpdatedAt: oldTime},
		},
	}
	service := newRelayRuntimeTestService(repo, nil)

	dryRun, err := service.PurgeLLMRelayCache(context.Background(), relayTestManagePrincipal(), domainaigateway.LLMRelayCachePurgeRequest{
		PublicModel: "gpt-public",
		OlderThan:   &olderThan,
		DryRun:      true,
	})
	if err != nil {
		t.Fatalf("dry-run PurgeLLMRelayCache returned error: %v", err)
	}
	if dryRun.Status != "dry_run" || dryRun.PurgedCount != 1 || !dryRun.DryRun {
		t.Fatalf("unexpected dry-run result: %#v", dryRun)
	}
	if len(repo.cacheEntries()) != 3 {
		t.Fatalf("dry run should not delete entries: %#v", repo.cacheEntries())
	}

	purged, err := service.PurgeLLMRelayCache(context.Background(), relayTestManagePrincipal(), domainaigateway.LLMRelayCachePurgeRequest{
		PublicModel: "gpt-public",
		OlderThan:   &olderThan,
	})
	if err != nil {
		t.Fatalf("PurgeLLMRelayCache returned error: %v", err)
	}
	if purged.Status != "purged" || purged.PurgedCount != 1 || purged.DryRun {
		t.Fatalf("unexpected purge result: %#v", purged)
	}
	caches := repo.cacheEntries()
	if len(caches) != 2 || slices.ContainsFunc(caches, func(item domainaigateway.LLMCacheEntry) bool { return item.ID == "cache-old" }) {
		t.Fatalf("unexpected remaining cache entries: %#v", caches)
	}
}

func TestPurgeLLMRelayCacheRequiresManagePermission(t *testing.T) {
	repo := &relayTestRepository{}
	service := newRelayRuntimeTestService(repo, nil)

	_, err := service.PurgeLLMRelayCache(context.Background(), relayTestPrincipal(), domainaigateway.LLMRelayCachePurgeRequest{DryRun: true})

	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("error = %v, want access denied", err)
	}
}

func TestRelayLLMHTTPEstimatesOpenAIChatUsageWhenProviderOmitsUsage(t *testing.T) {
	const upstreamKey = "sk-openai-estimate-upstream-test"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s, want /v1/chat/completions", r.URL.Path)
		}
		_ = decodeRelayTestJSON(t, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-estimated","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"hello from upstream without usage"}}]}`)
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	service := newRelayRuntimeTestService(repo, upstream.Client())
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{"Accept": []string{"application/json"}},
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"estimate local tokens please"}]}`),
		RequestID:    "req-openai-estimated",
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	log := singleRelayLog(t, repo)
	if !log.EstimatedTokens {
		t.Fatalf("estimatedTokens = false, want true: %#v", log)
	}
	if log.PromptTokens <= 0 || log.CompletionTokens <= 0 || log.TotalTokens != log.PromptTokens+log.CompletionTokens {
		t.Fatalf("unexpected estimated usage fields: %#v", log)
	}
	if log.Metadata["messages"] != nil || log.Metadata["prompt"] != nil || log.RouteTrace["messages"] != nil {
		t.Fatalf("estimated usage should not persist prompt text in metadata: metadata=%#v routeTrace=%#v", log.Metadata, log.RouteTrace)
	}
}

func TestRelayLLMHTTPProxiesOpenAIEmbeddingsAndRecordsUsage(t *testing.T) {
	const upstreamKey = "sk-openai-embedding-upstream-test"
	requests := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings" {
			t.Errorf("path = %s, want /v1/embeddings", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+upstreamKey {
			t.Errorf("Authorization = %q", got)
		}
		requests <- decodeRelayTestJSON(t, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"model":"gpt-upstream","usage":{"prompt_tokens":4,"total_tokens":4}}`)
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	service := newRelayRuntimeTestService(repo, upstream.Client())
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
		"purpose":              LLMRelayTokenPurpose,
		"allowedModels":        []string{"gpt-public"},
		"allowedProviderKinds": []string{"openai"},
		"allowedUpstreamIds":   []string{"upstream-openai"},
	}), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "embeddings",
		Method:       http.MethodPost,
		Headers:      http.Header{"Accept": []string{"application/json"}},
		Body:         []byte(`{"model":"gpt-public","input":"hello","encoding_format":"float","stream":true,"custom":"preserved"}`),
		RequestID:    "req-openai-embedding",
		SourceIP:     "203.0.113.8",
		UserAgent:    "relay-test",
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	upstreamPayload := <-requests
	if upstreamPayload["model"] != "gpt-upstream" || upstreamPayload["input"] != "hello" || upstreamPayload["custom"] != "preserved" {
		t.Fatalf("upstream payload = %#v", upstreamPayload)
	}
	assertNativeRelayBody(t, recorder.Body.String(), "embedding")
	log := singleRelayLog(t, repo)
	if log.Status != "success" || log.ProviderKind != "openai" || log.Endpoint != "embeddings" {
		t.Fatalf("unexpected status log fields: %#v", log)
	}
	if log.Stream {
		t.Fatalf("embeddings relay log stream = true, want false")
	}
	if log.PublicModel != "gpt-public" || log.UpstreamID != "upstream-openai" || log.UpstreamModel != "gpt-upstream" {
		t.Fatalf("unexpected route log fields: %#v", log)
	}
	if log.PromptTokens != 4 || log.CompletionTokens != 0 || log.TotalTokens != 4 {
		t.Fatalf("unexpected usage log fields: %#v", log)
	}
}

func TestRelayLLMHTTPProxiesOpenAIImageGenerations(t *testing.T) {
	const upstreamKey = "sk-openai-image-upstream-test"
	requests := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/images/generations" {
			t.Errorf("path = %s, want /v1/images/generations", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+upstreamKey {
			t.Errorf("Authorization = %q", got)
		}
		requests <- decodeRelayTestJSON(t, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"created":1710000000,"data":[{"b64_json":"image-bytes"}]}`)
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	service := newRelayRuntimeTestService(repo, upstream.Client())
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
		"purpose":              LLMRelayTokenPurpose,
		"allowedModels":        []string{"gpt-public"},
		"allowedProviderKinds": []string{"openai"},
		"allowedUpstreamIds":   []string{"upstream-openai"},
	}), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "images/generations",
		Method:       http.MethodPost,
		Headers:      http.Header{"Accept": []string{"application/json"}},
		Body:         []byte(`{"model":"gpt-public","prompt":"draw a clean product diagram","size":"1024x1024","n":1,"custom":"preserved"}`),
		RequestID:    "req-openai-image-generation",
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	upstreamPayload := <-requests
	if upstreamPayload["model"] != "gpt-upstream" || upstreamPayload["prompt"] != "draw a clean product diagram" || upstreamPayload["custom"] != "preserved" {
		t.Fatalf("upstream payload = %#v", upstreamPayload)
	}
	assertNativeRelayBody(t, recorder.Body.String(), "image-bytes")
	log := singleRelayLog(t, repo)
	if log.Status != "success" || log.ProviderKind != "openai" || log.Endpoint != "images/generations" {
		t.Fatalf("unexpected status log fields: %#v", log)
	}
	if log.Stream {
		t.Fatalf("image generation relay log stream = true, want false")
	}
	if log.PublicModel != "gpt-public" || log.UpstreamID != "upstream-openai" || log.UpstreamModel != "gpt-upstream" {
		t.Fatalf("unexpected route log fields: %#v", log)
	}
	if !log.EstimatedTokens || log.PromptTokens <= 0 || log.CompletionTokens != 0 || log.TotalTokens != log.PromptTokens {
		t.Fatalf("unexpected estimated usage fields: %#v", log)
	}
}

func TestRelayLLMHTTPProxiesOpenAIAudioSpeech(t *testing.T) {
	const upstreamKey = "sk-openai-audio-upstream-test"
	const audioBody = "fake-mp3-bytes"
	requests := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/audio/speech" {
			t.Errorf("path = %s, want /v1/audio/speech", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+upstreamKey {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("Accept"); got != "*/*" {
			t.Errorf("Accept = %q, want */*", got)
		}
		requests <- decodeRelayTestJSON(t, r.Body)
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = io.WriteString(w, audioBody)
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	service := newRelayRuntimeTestService(repo, upstream.Client())
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
		"purpose":              LLMRelayTokenPurpose,
		"allowedModels":        []string{"gpt-public"},
		"allowedProviderKinds": []string{"openai"},
		"allowedUpstreamIds":   []string{"upstream-openai"},
	}), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "audio/speech",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","input":"hello from speech","voice":"alloy","response_format":"mp3","custom":"preserved"}`),
		RequestID:    "req-openai-audio-speech",
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	upstreamPayload := <-requests
	assertOpenAIAudioSpeechResult(t, recorder, upstreamPayload, singleRelayLog(t, repo), audioBody)
}

func TestRelayLLMHTTPProxiesOpenAIAudioMultipartEndpoints(t *testing.T) {
	for _, tt := range []struct {
		name     string
		endpoint string
		path     string
		response string
	}{
		{
			name:     "transcriptions",
			endpoint: "audio/transcriptions",
			path:     "/v1/audio/transcriptions",
			response: `{"text":"hello world"}`,
		},
		{
			name:     "translations",
			endpoint: "audio/translations",
			path:     "/v1/audio/translations",
			response: `{"text":"hello world"}`,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const upstreamKey = "sk-openai-audio-multipart-test"
			requests := make(chan relayMultipartFields, 1)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != tt.path {
					t.Errorf("path = %s, want %s", r.URL.Path, tt.path)
				}
				if got := r.Header.Get("Authorization"); got != "Bearer "+upstreamKey {
					t.Errorf("Authorization = %q", got)
				}
				if !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data; boundary=") {
					t.Errorf("Content-Type = %q, want multipart/form-data with boundary", r.Header.Get("Content-Type"))
				}
				requests <- decodeRelayTestMultipart(t, r)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, tt.response)
			}))
			defer upstream.Close()

			body, contentType := relayTestMultipartBody(t, map[string]string{
				"model":           "gpt-public",
				"prompt":          "transcribe carefully",
				"response_format": "json",
				"custom":          "preserved",
			}, "file", "sample.wav", "audio/wav", "fake-audio-bytes")
			repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
			service := newRelayRuntimeTestService(repo, upstream.Client())
			recorder := httptest.NewRecorder()

			err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
				"purpose":              LLMRelayTokenPurpose,
				"allowedModels":        []string{"gpt-public"},
				"allowedProviderKinds": []string{"openai"},
				"allowedUpstreamIds":   []string{"upstream-openai"},
			}), LLMRelayHTTPRequest{
				ProviderKind: "openai",
				Endpoint:     tt.endpoint,
				Method:       http.MethodPost,
				Headers:      http.Header{"Content-Type": []string{contentType}},
				Body:         body,
				RequestID:    "req-openai-audio-multipart",
			}, recorder)

			if err != nil {
				t.Fatalf("RelayLLMHTTP returned error: %v", err)
			}
			if recorder.Code != http.StatusOK {
				t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
			}
			assertNativeRelayBody(t, recorder.Body.String(), "hello world")
			upstreamPayload := <-requests
			if upstreamPayload.fields["model"] != "gpt-upstream" ||
				upstreamPayload.fields["prompt"] != "transcribe carefully" ||
				upstreamPayload.fields["custom"] != "preserved" {
				t.Fatalf("upstream multipart fields = %#v", upstreamPayload.fields)
			}
			if upstreamPayload.fileField != "file" || upstreamPayload.fileName != "sample.wav" || upstreamPayload.fileBody != "fake-audio-bytes" {
				t.Fatalf("upstream file part = %#v", upstreamPayload)
			}
			log := singleRelayLog(t, repo)
			if log.Status != "success" || log.ProviderKind != "openai" || log.Endpoint != tt.endpoint {
				t.Fatalf("unexpected status log fields: %#v", log)
			}
			if log.Stream {
				t.Fatalf("audio multipart relay log stream = true, want false")
			}
			if log.PublicModel != "gpt-public" || log.UpstreamID != "upstream-openai" || log.UpstreamModel != "gpt-upstream" {
				t.Fatalf("unexpected route log fields: %#v", log)
			}
		})
	}
}

type imageMultipartCase struct {
	name       string
	endpoint   string
	path       string
	fields     map[string]string
	fileField  string
	fileName   string
	fileBody   string
	maskName   string
	maskBody   string
	wantPrompt bool
}

func TestRelayLLMHTTPProxiesOpenAIImageMultipartEndpoints(t *testing.T) {
	for _, tt := range []imageMultipartCase{
		{
			name:     "edits",
			endpoint: "images/edits",
			path:     "/v1/images/edits",
			fields: map[string]string{
				"model":  "gpt-public",
				"prompt": "replace the background",
				"size":   "1024x1024",
				"custom": "preserved",
			},
			fileField:  "image",
			fileName:   "input.png",
			fileBody:   "fake-image-bytes",
			maskName:   "mask.png",
			maskBody:   "fake-mask-bytes",
			wantPrompt: true,
		},
		{
			name:     "variations",
			endpoint: "images/variations",
			path:     "/v1/images/variations",
			fields: map[string]string{
				"model":  "gpt-public",
				"size":   "1024x1024",
				"custom": "preserved",
			},
			fileField: "image",
			fileName:  "source.png",
			fileBody:  "fake-source-image-bytes",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			testOpenAIImageMultipartEndpoint(t, tt)

		})
	}
}

func testOpenAIImageMultipartEndpoint(t *testing.T, tt imageMultipartCase) {
	const upstreamKey = "sk-openai-image-multipart-test"
	requests := make(chan relayMultipartFields, 1)
	upstream := httptest.NewServer(imageMultipartUpstreamHandler(t, tt, upstreamKey, requests))
	defer upstream.Close()

	body, contentType := imageMultipartRequestBody(t, tt)
	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	service := newRelayRuntimeTestService(repo, upstream.Client())
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
		"purpose":              LLMRelayTokenPurpose,
		"allowedModels":        []string{"gpt-public"},
		"allowedProviderKinds": []string{"openai"},
		"allowedUpstreamIds":   []string{"upstream-openai"},
	}), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     tt.endpoint,
		Method:       http.MethodPost,
		Headers:      http.Header{"Content-Type": []string{contentType}},
		Body:         body,
		RequestID:    "req-openai-image-multipart",
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	assertImageMultipartResponse(t, recorder, <-requests, tt)
	assertImageMultipartLog(t, singleRelayLog(t, repo), tt)
}

func imageMultipartUpstreamHandler(t *testing.T, tt imageMultipartCase, upstreamKey string, requests chan<- relayMultipartFields) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != tt.path || r.Header.Get("Authorization") != "Bearer "+upstreamKey || !strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data; boundary=") {
			t.Errorf("unexpected multipart request path=%s headers=%#v", r.URL.Path, r.Header)
		}
		requests <- decodeRelayTestMultipart(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"created":1710000000,"data":[{"b64_json":"image-bytes"}]}`)
	}
}

func imageMultipartRequestBody(t *testing.T, tt imageMultipartCase) ([]byte, string) {
	t.Helper()
	if tt.maskName == "" {
		return relayTestMultipartBody(t, tt.fields, tt.fileField, tt.fileName, "image/png", tt.fileBody)
	}
	parts := make([]relayTestMultipartPart, 0, len(tt.fields)+2)
	for key, value := range tt.fields {
		parts = append(parts, relayTestMultipartPart{name: key, body: value})
	}
	parts = append(parts,
		relayTestMultipartPart{name: tt.fileField, filename: tt.fileName, contentType: "image/png", body: tt.fileBody},
		relayTestMultipartPart{name: "mask", filename: tt.maskName, contentType: "image/png", body: tt.maskBody},
	)
	return relayTestMultipartBodyWithParts(t, parts)
}

func assertImageMultipartResponse(t *testing.T, recorder *httptest.ResponseRecorder, upstreamPayload relayMultipartFields, tt imageMultipartCase) {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	assertNativeRelayBody(t, recorder.Body.String(), "image-bytes")
	if upstreamPayload.fields["model"] != "gpt-upstream" || upstreamPayload.fields["custom"] != "preserved" {
		t.Fatalf("upstream multipart fields = %#v", upstreamPayload.fields)
	}
	if tt.wantPrompt && upstreamPayload.fields["prompt"] != "replace the background" {
		t.Fatalf("upstream prompt = %q", upstreamPayload.fields["prompt"])
	}
	image := upstreamPayload.files[tt.fileField]
	if image.name != tt.fileName || image.body != tt.fileBody {
		t.Fatalf("upstream image part = %#v", image)
	}
	if tt.maskName != "" {
		mask := upstreamPayload.files["mask"]
		if mask.name != tt.maskName || mask.body != tt.maskBody {
			t.Fatalf("upstream mask part = %#v", mask)
		}
	}
}

func assertImageMultipartLog(t *testing.T, log domainaigateway.LLMCallLog, tt imageMultipartCase) {
	t.Helper()
	if log.Status != "success" || log.ProviderKind != "openai" || log.Endpoint != tt.endpoint {
		t.Fatalf("unexpected status log fields: %#v", log)
	}
	if log.Stream {
		t.Fatalf("image multipart relay log stream = true, want false")
	}
	if log.PublicModel != "gpt-public" || log.UpstreamID != "upstream-openai" || log.UpstreamModel != "gpt-upstream" {
		t.Fatalf("unexpected route log fields: %#v", log)
	}
	if tt.wantPrompt {
		if !log.EstimatedTokens || log.PromptTokens <= 0 || log.CompletionTokens != 0 || log.TotalTokens != log.PromptTokens {
			t.Fatalf("unexpected estimated usage fields: %#v", log)
		}
	} else if log.EstimatedTokens || log.PromptTokens != 0 || log.CompletionTokens != 0 || log.TotalTokens != 0 {
		t.Fatalf("unexpected variation usage fields: %#v", log)
	}
}

func TestRelayLLMHTTPAudioSpeechDefaultsBinaryContentType(t *testing.T) {
	const upstreamKey = "sk-openai-audio-content-type-test"
	repo := relayRepoForUpstream(t, "https://upstream.example", "openai", upstreamKey)
	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() != "https://upstream.example/v1/audio/speech" {
			t.Fatalf("url = %s, want https://upstream.example/v1/audio/speech", r.URL.String())
		}
		_ = decodeRelayTestJSON(t, r.Body)
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{},
			Body:       io.NopCloser(strings.NewReader("audio-bytes")),
			Request:    r,
		}, nil
	})}
	service := newRelayRuntimeTestService(repo, client)
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
		"purpose":              LLMRelayTokenPurpose,
		"allowedModels":        []string{"gpt-public"},
		"allowedProviderKinds": []string{"openai"},
		"allowedUpstreamIds":   []string{"upstream-openai"},
	}), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "audio/speech",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","input":"hello","voice":"alloy"}`),
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	if recorder.Header().Get("Content-Type") != "application/octet-stream" {
		t.Fatalf("Content-Type = %q, want application/octet-stream", recorder.Header().Get("Content-Type"))
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestRelayLLMHTTPRejectsOpenAIAudioSpeechStreamingAndMultipart(t *testing.T) {
	const upstreamKey = "sk-openai-audio-stream-test"
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	for _, tt := range []struct {
		name    string
		headers http.Header
		body    []byte
	}{
		{
			name: "stream flag",
			body: []byte(`{"model":"gpt-public","input":"hello","voice":"alloy","stream":true}`),
		},
		{
			name: "stream format sse",
			body: []byte(`{"model":"gpt-public","input":"hello","voice":"alloy","stream_format":"sse"}`),
		},
		{
			name:    "multipart content type",
			headers: http.Header{"Content-Type": []string{"multipart/form-data; boundary=test"}},
			body:    []byte(`{"model":"gpt-public","input":"hello","voice":"alloy"}`),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
			service := newRelayRuntimeTestService(repo, upstream.Client())
			err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
				"purpose":              LLMRelayTokenPurpose,
				"allowedModels":        []string{"gpt-public"},
				"allowedProviderKinds": []string{"openai"},
				"allowedUpstreamIds":   []string{"upstream-openai"},
			}), LLMRelayHTTPRequest{
				ProviderKind: "openai",
				Endpoint:     "audio/speech",
				Method:       http.MethodPost,
				Headers:      tt.headers,
				Body:         tt.body,
				RequestID:    "req-openai-audio-speech-invalid",
			}, httptest.NewRecorder())

			if !errors.Is(err, apperrors.ErrInvalidArgument) {
				t.Fatalf("RelayLLMHTTP error = %v, want invalid argument", err)
			}
		})
	}
	if upstreamCalls != 0 {
		t.Fatalf("upstream calls = %d, want 0", upstreamCalls)
	}
}

func TestRelayLLMHTTPRejectsOpenAIAudioMultipartInvalidRequests(t *testing.T) {
	const upstreamKey = "sk-openai-audio-invalid-multipart-test"
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	validBody, _ := relayTestMultipartBody(t, map[string]string{
		"model": "gpt-public",
	}, "file", "sample.wav", "audio/wav", "fake-audio-bytes")
	missingModelBody, missingModelContentType := relayTestMultipartBody(t, map[string]string{
		"prompt": "hello",
	}, "file", "sample.wav", "audio/wav", "fake-audio-bytes")
	missingFileBody, missingFileContentType := relayTestMultipartBody(t, map[string]string{
		"model": "gpt-public",
	}, "", "", "", "")
	duplicateModelBody, duplicateModelContentType := relayTestMultipartBodyWithParts(t, []relayTestMultipartPart{
		{name: "model", body: "gpt-public"},
		{name: "model", body: "other-model"},
		{name: "file", filename: "sample.wav", contentType: "audio/wav", body: "fake-audio-bytes"},
	})
	fileModelBody, fileModelContentType := relayTestMultipartBodyWithParts(t, []relayTestMultipartPart{
		{name: "model", filename: "model.txt", contentType: "text/plain", body: "gpt-public"},
		{name: "file", filename: "sample.wav", contentType: "audio/wav", body: "fake-audio-bytes"},
	})
	streamBody, streamContentType := relayTestMultipartBody(t, map[string]string{
		"model":  "gpt-public",
		"stream": "true",
	}, "file", "sample.wav", "audio/wav", "fake-audio-bytes")

	for _, tt := range []struct {
		name        string
		contentType string
		body        []byte
	}{
		{
			name:        "non multipart",
			contentType: "application/json",
			body:        []byte(`{"model":"gpt-public"}`),
		},
		{
			name:        "bad boundary",
			contentType: "multipart/form-data; boundary=missing",
			body:        validBody,
		},
		{
			name:        "missing model",
			contentType: missingModelContentType,
			body:        missingModelBody,
		},
		{
			name:        "missing file",
			contentType: missingFileContentType,
			body:        missingFileBody,
		},
		{
			name:        "duplicate model",
			contentType: duplicateModelContentType,
			body:        duplicateModelBody,
		},
		{
			name:        "model file part",
			contentType: fileModelContentType,
			body:        fileModelBody,
		},
		{
			name:        "stream flag",
			contentType: streamContentType,
			body:        streamBody,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
			service := newRelayRuntimeTestService(repo, upstream.Client())
			err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
				"purpose":              LLMRelayTokenPurpose,
				"allowedModels":        []string{"gpt-public"},
				"allowedProviderKinds": []string{"openai"},
				"allowedUpstreamIds":   []string{"upstream-openai"},
			}), LLMRelayHTTPRequest{
				ProviderKind: "openai",
				Endpoint:     "audio/transcriptions",
				Method:       http.MethodPost,
				Headers:      http.Header{"Content-Type": []string{tt.contentType}},
				Body:         tt.body,
				RequestID:    "req-openai-audio-multipart-invalid",
			}, httptest.NewRecorder())

			if !errors.Is(err, apperrors.ErrInvalidArgument) {
				t.Fatalf("RelayLLMHTTP error = %v, want invalid argument", err)
			}
		})
	}
	if upstreamCalls != 0 {
		t.Fatalf("upstream calls = %d, want 0", upstreamCalls)
	}
}

type invalidImageMultipartCase struct {
	name, endpoint, contentType string
	body                        []byte
}

func invalidImageMultipartCases(t *testing.T) []invalidImageMultipartCase {
	t.Helper()
	validBody, _ := relayTestMultipartBody(t, map[string]string{"model": "gpt-public", "prompt": "edit this image"}, "image", "input.png", "image/png", "fake-image-bytes")
	missingModelBody, missingModelType := relayTestMultipartBody(t, map[string]string{"prompt": "edit this image"}, "image", "input.png", "image/png", "fake-image-bytes")
	missingImageBody, missingImageType := relayTestMultipartBody(t, map[string]string{"model": "gpt-public", "prompt": "edit this image"}, "file", "input.png", "image/png", "fake-image-bytes")
	missingPromptBody, missingPromptType := relayTestMultipartBody(t, map[string]string{"model": "gpt-public"}, "image", "input.png", "image/png", "fake-image-bytes")
	duplicateModelBody, duplicateModelType := relayTestMultipartBodyWithParts(t, []relayTestMultipartPart{
		{name: "model", body: "gpt-public"}, {name: "model", body: "other-model"}, {name: "prompt", body: "edit this image"},
		{name: "image", filename: "input.png", contentType: "image/png", body: "fake-image-bytes"},
	})
	fileModelBody, fileModelType := relayTestMultipartBodyWithParts(t, []relayTestMultipartPart{
		{name: "model", filename: "model.txt", contentType: "text/plain", body: "gpt-public"}, {name: "prompt", body: "edit this image"},
		{name: "image", filename: "input.png", contentType: "image/png", body: "fake-image-bytes"},
	})
	filePromptBody, filePromptType := relayTestMultipartBodyWithParts(t, []relayTestMultipartPart{
		{name: "model", body: "gpt-public"}, {name: "prompt", filename: "prompt.txt", contentType: "text/plain", body: "edit this image"},
		{name: "image", filename: "input.png", contentType: "image/png", body: "fake-image-bytes"},
	})
	streamBody, streamType := relayTestMultipartBody(t, map[string]string{"model": "gpt-public", "prompt": "edit this image", "stream": "true"}, "image", "input.png", "image/png", "fake-image-bytes")
	streamFormatBody, streamFormatType := relayTestMultipartBody(t, map[string]string{"model": "gpt-public", "prompt": "edit this image", "stream_format": "sse"}, "image", "input.png", "image/png", "fake-image-bytes")
	return []invalidImageMultipartCase{
		{"non multipart", "images/edits", "application/json", []byte(`{"model":"gpt-public"}`)},
		{"bad boundary", "images/edits", "multipart/form-data; boundary=missing", validBody},
		{"missing model", "images/edits", missingModelType, missingModelBody},
		{"missing image", "images/edits", missingImageType, missingImageBody},
		{"missing prompt for edits", "images/edits", missingPromptType, missingPromptBody},
		{"variation missing image", "images/variations", missingImageType, missingImageBody},
		{"duplicate model", "images/edits", duplicateModelType, duplicateModelBody},
		{"model file part", "images/edits", fileModelType, fileModelBody},
		{"prompt file part", "images/edits", filePromptType, filePromptBody},
		{"stream flag", "images/edits", streamType, streamBody},
		{"stream format sse", "images/edits", streamFormatType, streamFormatBody},
	}
}

func TestRelayLLMHTTPRejectsOpenAIImageMultipartInvalidRequests(t *testing.T) {
	const upstreamKey = "sk-openai-image-invalid-multipart-test"
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	for _, tt := range invalidImageMultipartCases(t) {
		t.Run(tt.name, func(t *testing.T) {
			repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
			service := newRelayRuntimeTestService(repo, upstream.Client())
			err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
				"purpose":              LLMRelayTokenPurpose,
				"allowedModels":        []string{"gpt-public"},
				"allowedProviderKinds": []string{"openai"},
				"allowedUpstreamIds":   []string{"upstream-openai"},
			}), LLMRelayHTTPRequest{
				ProviderKind: "openai",
				Endpoint:     tt.endpoint,
				Method:       http.MethodPost,
				Headers:      http.Header{"Content-Type": []string{tt.contentType}},
				Body:         tt.body,
				RequestID:    "req-openai-image-multipart-invalid",
			}, httptest.NewRecorder())

			if !errors.Is(err, apperrors.ErrInvalidArgument) {
				t.Fatalf("RelayLLMHTTP error = %v, want invalid argument", err)
			}
		})
	}
	if upstreamCalls != 0 {
		t.Fatalf("upstream calls = %d, want 0", upstreamCalls)
	}
}

func runRelayCacheBypassTest(t *testing.T, contentType, responseFormat, endpoint string, body []byte) (int, *relayTestRepository) {
	t.Helper()
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls++
		w.Header().Set("Content-Type", contentType)
		_, _ = fmt.Fprintf(w, responseFormat, upstreamCalls)
	}))
	t.Cleanup(upstream.Close)
	repo := relayRepoForUpstream(t, upstream.URL, "openai", "sk-openai-cache-bypass-test")
	repo.routes[0].CachePolicy = map[string]any{"enabled": true, "ttlSeconds": 60}
	service := newRelayRuntimeTestService(repo, upstream.Client())
	request := LLMRelayHTTPRequest{ProviderKind: "openai", Endpoint: endpoint, Method: http.MethodPost, Headers: http.Header{}, Body: body}
	for call := range 2 {
		if err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
			"purpose": LLMRelayTokenPurpose, "allowedModels": []string{"gpt-public"}, "allowedProviderKinds": []string{"openai"}, "allowedUpstreamIds": []string{"upstream-openai"},
		}), request, httptest.NewRecorder()); err != nil {
			t.Fatalf("RelayLLMHTTP #%d returned error: %v", call+1, err)
		}
	}
	return upstreamCalls, repo
}

func TestRelayLLMHTTPAudioSpeechBypassResponseCache(t *testing.T) {
	upstreamCalls, repo := runRelayCacheBypassTest(t, "audio/mpeg", "audio-%d", "audio/speech", []byte(`{"model":"gpt-public","input":"hello","voice":"alloy"}`))
	if upstreamCalls != 2 {
		t.Fatalf("upstream calls = %d, want 2", upstreamCalls)
	}
	if caches := repo.cacheEntries(); len(caches) != 0 {
		t.Fatalf("cache entries = %#v, want none", caches)
	}
	logs := repo.logs()
	if len(logs) != 2 || logs[0].CacheStatus != relayCacheBypass || logs[1].CacheStatus != relayCacheBypass {
		t.Fatalf("cache log statuses = %#v, want bypass/bypass", logs)
	}
}

func TestRelayLLMHTTPRejectsOpenAIImageGenerationStreamFlag(t *testing.T) {
	const upstreamKey = "sk-openai-image-stream-test"
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	service := newRelayRuntimeTestService(repo, upstream.Client())
	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
		"purpose":              LLMRelayTokenPurpose,
		"allowedModels":        []string{"gpt-public"},
		"allowedProviderKinds": []string{"openai"},
		"allowedUpstreamIds":   []string{"upstream-openai"},
	}), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "images/generations",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","prompt":"draw","stream":true}`),
		RequestID:    "req-openai-image-generation-stream",
	}, httptest.NewRecorder())

	if !errors.Is(err, apperrors.ErrInvalidArgument) {
		t.Fatalf("RelayLLMHTTP error = %v, want invalid argument", err)
	}
	if upstreamCalls != 0 {
		t.Fatalf("upstream calls = %d, want 0", upstreamCalls)
	}
}

func TestRelayLLMHTTPAudioMultipartBypassResponseCache(t *testing.T) {
	const upstreamKey = "sk-openai-audio-multipart-cache-test"
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"text":"transcript-%d"}`, upstreamCalls)
	}))
	defer upstream.Close()

	body, contentType := relayTestMultipartBody(t, map[string]string{
		"model": "gpt-public",
	}, "file", "sample.wav", "audio/wav", "fake-audio-bytes")
	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	repo.routes[0].CachePolicy = map[string]any{"enabled": true, "ttlSeconds": 60}
	service := newRelayRuntimeTestService(repo, upstream.Client())
	req := LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "audio/transcriptions",
		Method:       http.MethodPost,
		Headers:      http.Header{"Content-Type": []string{contentType}},
		Body:         body,
	}

	for i := 0; i < 2; i++ {
		if err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
			"purpose":              LLMRelayTokenPurpose,
			"allowedModels":        []string{"gpt-public"},
			"allowedProviderKinds": []string{"openai"},
			"allowedUpstreamIds":   []string{"upstream-openai"},
		}), req, httptest.NewRecorder()); err != nil {
			t.Fatalf("RelayLLMHTTP #%d returned error: %v", i+1, err)
		}
	}
	if upstreamCalls != 2 {
		t.Fatalf("upstream calls = %d, want 2", upstreamCalls)
	}
	if caches := repo.cacheEntries(); len(caches) != 0 {
		t.Fatalf("cache entries = %#v, want none", caches)
	}
}

func TestRelayLLMHTTPImageMultipartBypassResponseCache(t *testing.T) {
	for _, tt := range []struct {
		name     string
		endpoint string
		fields   map[string]string
	}{
		{
			name:     "edits",
			endpoint: "images/edits",
			fields: map[string]string{
				"model":  "gpt-public",
				"prompt": "edit this image",
			},
		},
		{
			name:     "variations",
			endpoint: "images/variations",
			fields: map[string]string{
				"model": "gpt-public",
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const upstreamKey = "sk-openai-image-multipart-cache-test"
			upstreamCalls := 0
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				upstreamCalls++
				w.Header().Set("Content-Type", "application/json")
				_, _ = fmt.Fprintf(w, `{"created":1710000000,"data":[{"url":"https://example.test/image-%d.png"}]}`, upstreamCalls)
			}))
			defer upstream.Close()

			body, contentType := relayTestMultipartBody(t, tt.fields, "image", "input.png", "image/png", "fake-image-bytes")
			repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
			repo.routes[0].CachePolicy = map[string]any{"enabled": true, "ttlSeconds": 60}
			service := newRelayRuntimeTestService(repo, upstream.Client())
			req := LLMRelayHTTPRequest{
				ProviderKind: "openai",
				Endpoint:     tt.endpoint,
				Method:       http.MethodPost,
				Headers:      http.Header{"Content-Type": []string{contentType}},
				Body:         body,
			}

			for i := 0; i < 2; i++ {
				if err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
					"purpose":              LLMRelayTokenPurpose,
					"allowedModels":        []string{"gpt-public"},
					"allowedProviderKinds": []string{"openai"},
					"allowedUpstreamIds":   []string{"upstream-openai"},
				}), req, httptest.NewRecorder()); err != nil {
					t.Fatalf("RelayLLMHTTP #%d returned error: %v", i+1, err)
				}
			}
			if upstreamCalls != 2 {
				t.Fatalf("upstream calls = %d, want 2", upstreamCalls)
			}
			if caches := repo.cacheEntries(); len(caches) != 0 {
				t.Fatalf("cache entries = %#v, want none", caches)
			}
			logs := repo.logs()
			if len(logs) != 2 || logs[0].CacheStatus != relayCacheBypass || logs[1].CacheStatus != relayCacheBypass {
				t.Fatalf("cache log statuses = %#v, want bypass/bypass", logs)
			}
		})
	}
}

func TestRelayLLMHTTPImageGenerationsBypassResponseCache(t *testing.T) {
	responseFormat := `{"created":1710000000,"data":[{"url":"https://example.test/image-%d.png"}]}`
	upstreamCalls, repo := runRelayCacheBypassTest(t, "application/json", responseFormat, "images/generations", []byte(`{"model":"gpt-public","prompt":"draw a release badge"}`))
	if upstreamCalls != 2 {
		t.Fatalf("upstream calls = %d, want 2", upstreamCalls)
	}
	if caches := repo.cacheEntries(); len(caches) != 0 {
		t.Fatalf("cache entries = %#v, want none", caches)
	}
	logs := repo.logs()
	if len(logs) != 2 || logs[0].CacheStatus != relayCacheBypass || logs[1].CacheStatus != relayCacheBypass {
		t.Fatalf("cache log statuses = %#v, want bypass/bypass", logs)
	}
}

func TestRelayLLMHTTPEstimatesOpenAIResponsesUsageWhenProviderOmitsUsage(t *testing.T) {
	const upstreamKey = "sk-openai-response-estimate-upstream-test"
	requests := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			t.Errorf("path = %s, want /v1/responses", r.URL.Path)
		}
		requests <- decodeRelayTestJSON(t, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"resp-estimated","object":"response","output_text":"response output without provider usage"}`)
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	service := newRelayRuntimeTestService(repo, upstream.Client())
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "responses",
		Method:       http.MethodPost,
		Headers:      http.Header{"Accept": []string{"application/json"}},
		Body:         []byte(`{"model":"gpt-public","input":"estimate responses local tokens"}`),
		RequestID:    "req-openai-response-estimated",
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	upstreamPayload := <-requests
	if upstreamPayload["model"] != "gpt-upstream" || upstreamPayload["input"] != "estimate responses local tokens" {
		t.Fatalf("upstream payload = %#v", upstreamPayload)
	}
	log := singleRelayLog(t, repo)
	if log.Endpoint != "responses" || !log.EstimatedTokens {
		t.Fatalf("unexpected response estimate log: %#v", log)
	}
	if log.PromptTokens <= 0 || log.CompletionTokens <= 0 || log.TotalTokens != log.PromptTokens+log.CompletionTokens {
		t.Fatalf("unexpected estimated usage fields: %#v", log)
	}
}

func TestRelayLLMHTTPProxiesOpenAIChatStreamAndRecordsUsage(t *testing.T) {
	const upstreamKey = "sk-openai-stream-upstream-test"
	requests := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := decodeRelayTestJSON(t, r.Body)
		requests <- payload
		time.Sleep(10 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chatcmpl-stream\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"ok\"},\"index\":0}]}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chatcmpl-stream\",\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":7,\"total_tokens\":12}}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	service := newRelayRuntimeTestService(repo, upstream.Client())
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{"Accept": []string{"text/event-stream"}},
		Body:         []byte(`{"model":"gpt-public","stream":true,"stream_options":{"include_usage":false},"messages":[{"role":"user","content":"hi"}]}`),
		RequestID:    "req-openai-stream",
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	upstreamPayload := <-requests
	options, _ := upstreamPayload["stream_options"].(map[string]any)
	if options["include_usage"] != true {
		t.Fatalf("stream_options = %#v, want include_usage=true", options)
	}
	if !strings.Contains(recorder.Body.String(), "data: ") || !strings.Contains(recorder.Body.String(), "[DONE]") {
		t.Fatalf("expected native SSE response, got %s", recorder.Body.String())
	}
	log := singleRelayLog(t, repo)
	if !log.Stream || log.Status != "success" || log.PromptTokens != 5 || log.CompletionTokens != 7 || log.TotalTokens != 12 {
		t.Fatalf("unexpected stream log: %#v", log)
	}
}

func TestRelayLLMHTTPEstimatesOpenAIChatStreamUsageWhenProviderOmitsUsage(t *testing.T) {
	const upstreamKey = "sk-openai-stream-estimate-upstream-test"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s, want /v1/chat/completions", r.URL.Path)
		}
		_ = decodeRelayTestJSON(t, r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chatcmpl-stream-estimated\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"hello \"},\"index\":0}]}\n\n")
		flusher.Flush()
		_, _ = fmt.Fprint(w, "data: {\"id\":\"chatcmpl-stream-estimated\",\"object\":\"chat.completion.chunk\",\"choices\":[{\"delta\":{\"content\":\"stream estimate\"},\"index\":0}]}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	service := newRelayRuntimeTestService(repo, upstream.Client())
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{"Accept": []string{"text/event-stream"}},
		Body:         []byte(`{"model":"gpt-public","stream":true,"messages":[{"role":"user","content":"estimate streamed local usage"}]}`),
		RequestID:    "req-openai-stream-estimated",
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	log := singleRelayLog(t, repo)
	if !log.Stream || !log.EstimatedTokens {
		t.Fatalf("unexpected stream estimate log: %#v", log)
	}
	if log.PromptTokens <= 0 || log.CompletionTokens <= 0 || log.TotalTokens != log.PromptTokens+log.CompletionTokens {
		t.Fatalf("unexpected estimated stream usage fields: %#v", log)
	}
}

func TestRelayLLMHTTPProxiesAnthropicMessagesAndRecordsUsage(t *testing.T) {
	const upstreamKey = "sk-ant-upstream-test"
	requests := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s, want /v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != upstreamKey {
			t.Errorf("x-api-key = %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "2023-06-01" {
			t.Errorf("anthropic-version = %q", got)
		}
		requests <- decodeRelayTestJSON(t, r.Body)
		time.Sleep(10 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg-1","type":"message","role":"assistant","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":4,"output_tokens":6,"cache_read_input_tokens":2,"cache_creation_input_tokens":1}}`)
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "anthropic", upstreamKey)
	service := newRelayRuntimeTestService(repo, upstream.Client())
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
		"purpose": LLMRelayTokenPurpose,
	}), LLMRelayHTTPRequest{
		ProviderKind: "anthropic",
		Endpoint:     "messages",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","max_tokens":32,"messages":[{"role":"user","content":"hi"}],"custom":"preserved"}`),
		RequestID:    "req-anthropic",
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	upstreamPayload := <-requests
	if upstreamPayload["model"] != "gpt-upstream" || upstreamPayload["custom"] != "preserved" {
		t.Fatalf("upstream payload = %#v", upstreamPayload)
	}
	assertNativeRelayBody(t, recorder.Body.String(), "msg-1")
	log := singleRelayLog(t, repo)
	if log.ProviderKind != "anthropic" || log.Endpoint != "messages" || log.PromptTokens != 4 || log.CompletionTokens != 6 || log.TotalTokens != 10 || log.CachedReadTokens != 2 || log.CachedWriteTokens != 1 {
		t.Fatalf("unexpected anthropic call log: %#v", log)
	}
}

func TestRelayLLMHTTPConvertsAnthropicMessagesToOpenAIChatNonStream(t *testing.T) {
	const upstreamKey = "sk-openai-transform-upstream-test"
	requests := make(chan map[string]any, 1)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("path = %s, want /v1/chat/completions", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+upstreamKey {
			t.Errorf("Authorization = %q", got)
		}
		if got := r.Header.Get("x-api-key"); got != "" {
			t.Errorf("x-api-key should not be forwarded to OpenAI upstream, got %q", got)
		}
		requests <- decodeRelayTestJSON(t, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-transform","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"converted openai"},"finish_reason":"stop","index":0}],"usage":{"prompt_tokens":6,"completion_tokens":9,"total_tokens":15}}`)
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	repo.routes[0].ProviderKind = "openai"
	repo.routes[0].TransformPolicy = map[string]any{"mode": "convert", "targetProviderKind": "openai"}
	service := newRelayRuntimeTestService(repo, upstream.Client())
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
		"purpose":              LLMRelayTokenPurpose,
		"allowedModels":        []string{"gpt-public"},
		"allowedProviderKinds": []string{"anthropic"},
		"allowedUpstreamIds":   []string{"upstream-openai"},
	}), LLMRelayHTTPRequest{
		ProviderKind: "anthropic",
		Endpoint:     "messages",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","max_tokens":32,"system":"reply plainly","messages":[{"role":"user","content":[{"type":"text","text":"hello"}]}]}`),
		RequestID:    "req-anthropic-to-openai",
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	assertAnthropicToOpenAIResponse(t, recorder, <-requests, repo)
}

func assertAnthropicToOpenAIResponse(t *testing.T, recorder *httptest.ResponseRecorder, upstreamPayload map[string]any, repo *relayTestRepository) {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	assertAnthropicUpstreamMessages(t, upstreamPayload)
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	content, _ := response["content"].([]any)
	if response["type"] != "message" || response["role"] != "assistant" || len(content) != 1 {
		t.Fatalf("response = %#v, want Anthropic message", response)
	}
	textBlock, _ := content[0].(map[string]any)
	usage, _ := response["usage"].(map[string]any)
	if textBlock["type"] != "text" || textBlock["text"] != "converted openai" || jsonNumberInt(usage["input_tokens"]) != 6 || jsonNumberInt(usage["output_tokens"]) != 9 {
		t.Fatalf("response content or usage is invalid: %#v", response)
	}
	log := singleRelayLog(t, repo)
	if log.ProviderKind != "anthropic" || log.Endpoint != "messages" || log.PromptTokens != 6 || log.CompletionTokens != 9 || log.TotalTokens != 15 {
		t.Fatalf("unexpected log: %#v", log)
	}
}

func assertAnthropicUpstreamMessages(t *testing.T, payload map[string]any) {
	t.Helper()
	messages, _ := payload["messages"].([]any)
	if payload["model"] != "gpt-upstream" || jsonNumberInt(payload["max_tokens"]) != 32 || len(messages) != 2 {
		t.Fatalf("upstream payload = %#v", payload)
	}
	systemMessage, _ := messages[0].(map[string]any)
	userMessage, _ := messages[1].(map[string]any)
	if systemMessage["role"] != "system" || systemMessage["content"] != "reply plainly" || userMessage["role"] != "user" || userMessage["content"] != "hello" {
		t.Fatalf("upstream messages = %#v", messages)
	}
}

func TestRelayLLMHTTPEstimatesAnthropicMessagesUsageWhenProviderOmitsUsage(t *testing.T) {
	const upstreamKey = "sk-ant-estimate-upstream-test"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s, want /v1/messages", r.URL.Path)
		}
		_ = decodeRelayTestJSON(t, r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"msg-estimated","type":"message","role":"assistant","content":[{"type":"text","text":"estimated anthropic response"}]}`)
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "anthropic", upstreamKey)
	service := newRelayRuntimeTestService(repo, upstream.Client())
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "anthropic",
		Endpoint:     "messages",
		Method:       http.MethodPost,
		Headers:      http.Header{"Accept": []string{"application/json"}},
		Body:         []byte(`{"model":"gpt-public","max_tokens":32,"system":"estimate system tokens","messages":[{"role":"user","content":"estimate anthropic local usage"}]}`),
		RequestID:    "req-anthropic-estimated",
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	log := singleRelayLog(t, repo)
	if !log.EstimatedTokens {
		t.Fatalf("estimatedTokens = false, want true: %#v", log)
	}
	if log.PromptTokens <= 0 || log.CompletionTokens <= 0 || log.TotalTokens != log.PromptTokens+log.CompletionTokens {
		t.Fatalf("unexpected estimated usage fields: %#v", log)
	}
}

func TestRelayLLMHTTPUsesUpdatedRouteWithoutStaleConfig(t *testing.T) {
	const upstreamKey = "sk-openai-route-update-test"
	models := make(chan string, 2)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		payload := decodeRelayTestJSON(t, r.Body)
		model, _ := payload["model"].(string)
		models <- model
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-route","object":"chat.completion","choices":[],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	service := newRelayRuntimeTestService(repo, upstream.Client())
	call := func(requestID string) {
		t.Helper()
		err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), LLMRelayHTTPRequest{
			ProviderKind: "openai",
			Endpoint:     "chat/completions",
			Method:       http.MethodPost,
			Headers:      http.Header{},
			Body:         []byte(`{"model":"gpt-public","messages":[]}`),
			RequestID:    requestID,
		}, httptest.NewRecorder())
		if err != nil {
			t.Fatalf("RelayLLMHTTP %s returned error: %v", requestID, err)
		}
	}

	call("req-route-v1")
	repo.mu.Lock()
	repo.routes[0].UpstreamModel = "gpt-upstream-v2"
	repo.mu.Unlock()
	call("req-route-v2")

	if first, second := <-models, <-models; first != "gpt-upstream" || second != "gpt-upstream-v2" {
		t.Fatalf("upstream models = %q, %q", first, second)
	}
}

func TestRelayLLMHTTPProxiesAnthropicMessagesStreamAndRecordsUsage(t *testing.T) {
	const upstreamKey = "sk-ant-stream-success-upstream-test"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("path = %s, want /v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != upstreamKey {
			t.Errorf("x-api-key = %q", got)
		}
		time.Sleep(10 * time.Millisecond)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "event: message_start\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-stream\",\"type\":\"message\",\"usage\":{\"input_tokens\":4,\"cache_read_input_tokens\":1}}}\n\n")
		_, _ = fmt.Fprint(w, "event: content_block_delta\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\n")
		_, _ = fmt.Fprint(w, "event: message_delta\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":6}}\n\n")
		_, _ = fmt.Fprint(w, "event: message_stop\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"message_stop\"}\n\n")
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "anthropic", upstreamKey)
	service := newRelayRuntimeTestService(repo, upstream.Client())
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "anthropic",
		Endpoint:     "messages",
		Method:       http.MethodPost,
		Headers:      http.Header{"Accept": []string{"text/event-stream"}},
		Body:         []byte(`{"model":"gpt-public","stream":true,"max_tokens":32,"messages":[{"role":"user","content":"hi"}]}`),
		RequestID:    "req-anthropic-stream",
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	if !strings.Contains(recorder.Body.String(), "event: message_start") || !strings.Contains(recorder.Body.String(), "message_stop") {
		t.Fatalf("expected native Anthropic SSE response, got %s", recorder.Body.String())
	}
	log := singleRelayLog(t, repo)
	if !log.Stream || log.Status != "success" || log.PromptTokens != 4 || log.CompletionTokens != 6 || log.TotalTokens != 10 || log.CachedReadTokens != 1 {
		t.Fatalf("unexpected anthropic stream log: %#v", log)
	}
}

func TestRelayUsageFromAnthropicSSE(t *testing.T) {
	body := []byte("event: message_start\n" +
		"data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-stream\",\"type\":\"message\",\"usage\":{\"input_tokens\":4,\"cache_read_input_tokens\":1}}}\n\n" +
		"event: message_delta\n" +
		"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":6}}\n\n")

	usage := relayUsageFromBody(body)

	if usage.promptTokens != 4 || usage.completionTokens != 6 || usage.totalTokens != 10 || usage.cachedReadTokens != 1 {
		t.Fatalf("usage = %#v", usage)
	}
}

func TestRelayLLMHTTPCancelsUpstreamAndRecordsClientCancelled(t *testing.T) {
	const upstreamKey = "sk-ant-stream-upstream-test"
	upstreamCancelled := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		_, _ = fmt.Fprint(w, "event: message_start\n")
		_, _ = fmt.Fprint(w, "data: {\"type\":\"message_start\",\"message\":{\"id\":\"msg-stream\",\"type\":\"message\",\"usage\":{\"input_tokens\":3}}}\n\n")
		flusher.Flush()
		select {
		case <-r.Context().Done():
			close(upstreamCancelled)
		case <-time.After(2 * time.Second):
			t.Error("upstream request was not cancelled")
		}
	}))
	defer upstream.Close()

	repo := relayRepoForUpstream(t, upstream.URL, "anthropic", upstreamKey)
	service := newRelayRuntimeTestService(repo, upstream.Client())
	ctx, cancel := context.WithCancel(context.Background())
	writer := &cancelingRelayWriter{header: http.Header{}, cancel: cancel}

	err := service.RelayLLMHTTP(ctx, relayTestPrincipal(), relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "anthropic",
		Endpoint:     "messages",
		Method:       http.MethodPost,
		Headers:      http.Header{"Accept": []string{"text/event-stream"}},
		Body:         []byte(`{"model":"gpt-public","stream":true,"max_tokens":32,"messages":[{"role":"user","content":"hi"}]}`),
		RequestID:    "req-cancelled",
	}, writer)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	select {
	case <-upstreamCancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("upstream did not observe cancellation")
	}
	if repo.sawCanceledLogContext() {
		t.Fatal("call log write used the cancelled request context")
	}
	log := singleRelayLog(t, repo)
	if log.Status != "client_cancelled" || log.ErrorCode != "client_cancelled" {
		t.Fatalf("expected client_cancelled log, got %#v", log)
	}
}

func TestRelayLLMHTTPRequiresRelayInvokePermission(t *testing.T) {
	service := NewWithDeps(ServiceDeps{
		Permissions: appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{
			"developer": {appaccess.PermAIGatewayInvoke},
		}}),
		LLMRelay: &relayTestRepository{},
		RelayConfig: LLMRelayConfig{
			Enabled:                 true,
			CredentialEncryptionKey: relayTestEncryptionKey,
		},
	})
	principal := testPrincipal("developer")
	principal.PermissionKeys = []string{appaccess.PermAIGatewayInvoke}

	err := service.RelayLLMHTTP(context.Background(), principal, relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public"}`),
	}, httptest.NewRecorder())

	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("error = %v, want access denied", err)
	}
}

func TestRelayLLMHTTPSkipsTeamRestrictedRouteAndFallsBack(t *testing.T) {
	const upstreamKey = "sk-openai-upstream-test"
	firstCalls := 0
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls++
		http.Error(w, "should not call team restricted route", http.StatusInternalServerError)
	}))
	defer first.Close()
	secondCalls := 0
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-team-route","object":"chat.completion","choices":[],"usage":{"total_tokens":1}}`)
	}))
	defer second.Close()
	repo := relayRepoForTwoUpstreams(t, first.URL, second.URL, upstreamKey)
	repo.routes[0].Metadata = map[string]any{"allowedTeams": []string{"finance"}}
	repo.routes[1].Metadata = map[string]any{"teamPolicy": map[string]any{"allowedTeams": []string{"platform"}}}
	service := newRelayRuntimeTestService(repo, first.Client())
	principal := relayTestPrincipal()
	principal.Teams = []string{"platform"}

	err := service.RelayLLMHTTP(context.Background(), principal, relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"hi"}]}`),
	}, httptest.NewRecorder())

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	if firstCalls != 0 || secondCalls != 1 {
		t.Fatalf("calls first=%d second=%d, want 0/1", firstCalls, secondCalls)
	}
	log := singleRelayLog(t, repo)
	if log.RouteTrace["routeId"] != "route-second" {
		t.Fatalf("route trace = %#v, want route-second", log.RouteTrace)
	}
}

func TestRelayLLMHTTPSkipsTeamRestrictedUpstreamAndFallsBack(t *testing.T) {
	const upstreamKey = "sk-openai-upstream-test"
	firstCalls := 0
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls++
		http.Error(w, "should not call team restricted upstream", http.StatusInternalServerError)
	}))
	defer first.Close()
	secondCalls := 0
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-team-upstream","object":"chat.completion","choices":[],"usage":{"total_tokens":1}}`)
	}))
	defer second.Close()
	repo := relayRepoForTwoUpstreams(t, first.URL, second.URL, upstreamKey)
	repo.upstreams[0].Metadata = map[string]any{"deniedTeams": []string{"platform"}}
	repo.upstreams[1].Metadata = map[string]any{"teamPolicy": map[string]any{"allowedTeams": []string{"platform"}}}
	service := newRelayRuntimeTestService(repo, first.Client())
	principal := relayTestPrincipal()
	principal.Teams = []string{"platform"}

	err := service.RelayLLMHTTP(context.Background(), principal, relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"hi"}]}`),
	}, httptest.NewRecorder())

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	if firstCalls != 0 || secondCalls != 1 {
		t.Fatalf("calls first=%d second=%d, want 0/1", firstCalls, secondCalls)
	}
	log := singleRelayLog(t, repo)
	if log.UpstreamID != "upstream-second" {
		t.Fatalf("upstream = %s, want upstream-second", log.UpstreamID)
	}
}

func TestSelectRelayUpstreamCandidatesUsesWeightedChoiceWithinPriority(t *testing.T) {
	repo := relayRepoForWeightedSelection(t)
	service := newRelayRuntimeTestService(repo, nil)
	calls := 0
	restore := stubRelayRandomIntn(func(n int) int {
		calls++
		if calls == 1 && n != 6 {
			t.Fatalf("weighted total = %d, want 6", n)
		}
		if calls == 1 {
			return 1
		}
		return 0
	})
	defer restore()

	candidates, err := service.selectRelayUpstreamCandidates(context.Background(), "openai", "gpt-public")
	if err != nil {
		t.Fatalf("selectRelayUpstreamCandidates returned error: %v", err)
	}
	if len(candidates) != 2 {
		t.Fatalf("candidate count = %d, want 2", len(candidates))
	}
	if candidates[0].upstream.ID != "upstream-heavy" {
		t.Fatalf("first upstream = %s, want upstream-heavy", candidates[0].upstream.ID)
	}
}

func TestSelectRelayUpstreamCandidatesPreservesPriorityBeforeWeight(t *testing.T) {
	repo := relayRepoForWeightedSelection(t)
	repo.routes[0].Priority = 1
	repo.routes[0].Weight = 1
	repo.routes[1].Priority = 2
	repo.routes[1].Weight = 100
	service := newRelayRuntimeTestService(repo, nil)
	restore := stubRelayRandomIntn(func(n int) int { return n - 1 })
	defer restore()

	candidates, err := service.selectRelayUpstreamCandidates(context.Background(), "openai", "gpt-public")
	if err != nil {
		t.Fatalf("selectRelayUpstreamCandidates returned error: %v", err)
	}
	if candidates[0].upstream.ID != "upstream-light" {
		t.Fatalf("first upstream = %s, want upstream-light", candidates[0].upstream.ID)
	}
}

func TestSelectRelayUpstreamCandidatesSkipsCircuitOpenUpstream(t *testing.T) {
	repo := relayRepoForWeightedSelection(t)
	repo.upstreams[0].Health = map[string]any{"circuitOpenUntil": time.Now().UTC().Add(time.Hour).Format(time.RFC3339)}
	service := newRelayRuntimeTestService(repo, nil)

	candidates, err := service.selectRelayUpstreamCandidates(context.Background(), "openai", "gpt-public")
	if err != nil {
		t.Fatalf("selectRelayUpstreamCandidates returned error: %v", err)
	}
	if len(candidates) != 1 || candidates[0].upstream.ID != "upstream-heavy" {
		t.Fatalf("candidates = %#v, want only upstream-heavy", candidates)
	}
}

func TestNormalizeRelayProviderKindAcceptsFirstClassOpenAICompatibleProviders(t *testing.T) {
	for _, tt := range []struct {
		input string
		want  string
	}{
		{input: " DeepSeek ", want: "deepseek"},
		{input: "QWEN", want: "qwen"},
		{input: "openrouter", want: "openrouter"},
		{input: "azure-openai", want: "azure-openai"},
		{input: "azure_openai", want: "azure-openai"},
		{input: "gemini", want: "gemini"},
		{input: "Cohere", want: "cohere"},
	} {
		t.Run(tt.input, func(t *testing.T) {
			if got := normalizeRelayProviderKind(tt.input); got != tt.want {
				t.Fatalf("normalizeRelayProviderKind(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestRelayEndpointURLUsesOpenAIPathsForFirstClassOpenAICompatibleProviders(t *testing.T) {
	for _, tt := range []struct {
		name         string
		baseURL      string
		providerKind string
		endpoint     string
		want         string
	}{
		{
			name:         "deepseek chat appends v1",
			baseURL:      "https://api.deepseek.com",
			providerKind: "deepseek",
			endpoint:     "chat/completions",
			want:         "https://api.deepseek.com/v1/chat/completions",
		},
		{
			name:         "qwen compatible mode keeps v1 suffix",
			baseURL:      "https://dashscope.aliyuncs.com/compatible-mode/v1",
			providerKind: "qwen",
			endpoint:     "models",
			want:         "https://dashscope.aliyuncs.com/compatible-mode/v1/models",
		},
		{
			name:         "openrouter api v1 keeps v1 suffix",
			baseURL:      "https://openrouter.ai/api/v1",
			providerKind: "openrouter",
			endpoint:     "responses",
			want:         "https://openrouter.ai/api/v1/responses",
		},
		{
			name:         "deepseek image generation appends v1",
			baseURL:      "https://api.deepseek.com",
			providerKind: "deepseek",
			endpoint:     "images/generations",
			want:         "https://api.deepseek.com/v1/images/generations",
		},
		{
			name:         "openai compatible image edits appends v1",
			baseURL:      "https://api.openai.com",
			providerKind: "openai",
			endpoint:     "images/edits",
			want:         "https://api.openai.com/v1/images/edits",
		},
		{
			name:         "openai compatible image variations appends v1",
			baseURL:      "https://api.openai.com",
			providerKind: "openai",
			endpoint:     "images/variations",
			want:         "https://api.openai.com/v1/images/variations",
		},
		{
			name:         "openai compatible audio speech appends v1",
			baseURL:      "https://api.openai.com",
			providerKind: "openai",
			endpoint:     "audio/speech",
			want:         "https://api.openai.com/v1/audio/speech",
		},
		{
			name:         "openai compatible audio transcription appends v1",
			baseURL:      "https://api.openai.com",
			providerKind: "openai",
			endpoint:     "audio/transcriptions",
			want:         "https://api.openai.com/v1/audio/transcriptions",
		},
		{
			name:         "openai compatible audio translation appends v1",
			baseURL:      "https://api.openai.com",
			providerKind: "openai",
			endpoint:     "audio/translations",
			want:         "https://api.openai.com/v1/audio/translations",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := relayEndpointURL(tt.baseURL, tt.providerKind, tt.endpoint)
			if err != nil {
				t.Fatalf("relayEndpointURL returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("relayEndpointURL = %q, want %q", got, tt.want)
			}
		})
	}
}

var azureOpenAIPathCases = []struct {
	name     string
	upstream domainaigateway.LLMUpstream
	route    domainaigateway.LLMModelRoute
	endpoint string
	want     string
}{
	{
		name: "v1 chat appends openai v1",
		upstream: domainaigateway.LLMUpstream{
			BaseURL: "https://example.openai.azure.com",
		},
		endpoint: "chat/completions",
		want:     "https://example.openai.azure.com/openai/v1/chat/completions",
	},
	{
		name: "v1 models keeps existing openai v1 suffix",
		upstream: domainaigateway.LLMUpstream{
			BaseURL: "https://example.openai.azure.com/openai/v1",
		},
		endpoint: "models",
		want:     "https://example.openai.azure.com/openai/v1/models",
	},
	{
		name: "deployment chat uses route upstream model and api version",
		upstream: domainaigateway.LLMUpstream{
			BaseURL: "https://example.openai.azure.com",
			Metadata: map[string]any{
				"azureOpenAI": map[string]any{
					"apiVersion": "2024-10-21",
				},
			},
		},
		route: domainaigateway.LLMModelRoute{
			UpstreamModel: "gpt-4o-prod",
		},
		endpoint: "chat/completions",
		want:     "https://example.openai.azure.com/openai/deployments/gpt-4o-prod/chat/completions?api-version=2024-10-21",
	},
	{
		name: "route metadata overrides deployment",
		upstream: domainaigateway.LLMUpstream{
			BaseURL: "https://example.openai.azure.com/openai",
			Metadata: map[string]any{
				"azureOpenAI": map[string]any{
					"apiVersion": "2024-10-21",
					"deployment": "upstream-deployment",
				},
			},
		},
		route: domainaigateway.LLMModelRoute{
			UpstreamModel: "route-model",
			Metadata: map[string]any{
				"azureOpenAI": map[string]any{
					"deployment": "route-deployment",
				},
			},
		},
		endpoint: "embeddings",
		want:     "https://example.openai.azure.com/openai/deployments/route-deployment/embeddings?api-version=2024-10-21",
	},
	{
		name: "deployment images generations uses route upstream model and api version",
		upstream: domainaigateway.LLMUpstream{
			BaseURL: "https://example.openai.azure.com",
			Metadata: map[string]any{
				"azureOpenAI": map[string]any{
					"apiVersion": "2024-10-21",
				},
			},
		},
		route: domainaigateway.LLMModelRoute{
			UpstreamModel: "gpt-image-prod",
		},
		endpoint: "images/generations",
		want:     "https://example.openai.azure.com/openai/deployments/gpt-image-prod/images/generations?api-version=2024-10-21",
	},
	{
		name: "deployment images edits uses route upstream model and api version",
		upstream: domainaigateway.LLMUpstream{
			BaseURL: "https://example.openai.azure.com",
			Metadata: map[string]any{
				"azureOpenAI": map[string]any{
					"apiVersion": "2024-10-21",
				},
			},
		},
		route: domainaigateway.LLMModelRoute{
			UpstreamModel: "gpt-image-prod",
		},
		endpoint: "images/edits",
		want:     "https://example.openai.azure.com/openai/deployments/gpt-image-prod/images/edits?api-version=2024-10-21",
	},
	{
		name: "deployment images variations uses route upstream model and api version",
		upstream: domainaigateway.LLMUpstream{
			BaseURL: "https://example.openai.azure.com",
			Metadata: map[string]any{
				"azureOpenAI": map[string]any{
					"apiVersion": "2024-10-21",
				},
			},
		},
		route: domainaigateway.LLMModelRoute{
			UpstreamModel: "gpt-image-prod",
		},
		endpoint: "images/variations",
		want:     "https://example.openai.azure.com/openai/deployments/gpt-image-prod/images/variations?api-version=2024-10-21",
	},
	{
		name: "deployment audio speech uses route upstream model and api version",
		upstream: domainaigateway.LLMUpstream{
			BaseURL: "https://example.openai.azure.com",
			Metadata: map[string]any{
				"azureOpenAI": map[string]any{
					"apiVersion": "2024-10-21",
				},
			},
		},
		route: domainaigateway.LLMModelRoute{
			UpstreamModel: "tts-prod",
		},
		endpoint: "audio/speech",
		want:     "https://example.openai.azure.com/openai/deployments/tts-prod/audio/speech?api-version=2024-10-21",
	},
	{
		name: "deployment audio transcriptions uses route upstream model and api version",
		upstream: domainaigateway.LLMUpstream{
			BaseURL: "https://example.openai.azure.com",
			Metadata: map[string]any{
				"azureOpenAI": map[string]any{
					"apiVersion": "2024-10-21",
				},
			},
		},
		route: domainaigateway.LLMModelRoute{
			UpstreamModel: "whisper-prod",
		},
		endpoint: "audio/transcriptions",
		want:     "https://example.openai.azure.com/openai/deployments/whisper-prod/audio/transcriptions?api-version=2024-10-21",
	},
	{
		name: "deployment audio translations uses route upstream model and api version",
		upstream: domainaigateway.LLMUpstream{
			BaseURL: "https://example.openai.azure.com",
			Metadata: map[string]any{
				"azureOpenAI": map[string]any{
					"apiVersion": "2024-10-21",
				},
			},
		},
		route: domainaigateway.LLMModelRoute{
			UpstreamModel: "whisper-prod",
		},
		endpoint: "audio/translations",
		want:     "https://example.openai.azure.com/openai/deployments/whisper-prod/audio/translations?api-version=2024-10-21",
	},
}

func TestRelayEndpointURLUsesAzureOpenAIPaths(t *testing.T) {
	for _, tt := range azureOpenAIPathCases {
		t.Run(tt.name, func(t *testing.T) {
			got, err := relayEndpointURLForUpstream(tt.upstream, tt.route, "azure-openai", tt.endpoint)
			if err != nil {
				t.Fatalf("relayEndpointURLForUpstream returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("relayEndpointURLForUpstream = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRelayEndpointURLUsesGeminiNativePaths(t *testing.T) {
	for _, tt := range []struct {
		name     string
		upstream domainaigateway.LLMUpstream
		route    domainaigateway.LLMModelRoute
		endpoint string
		want     string
	}{
		{
			name: "generate content appends v1beta",
			upstream: domainaigateway.LLMUpstream{
				BaseURL: "https://generativelanguage.googleapis.com",
			},
			route: domainaigateway.LLMModelRoute{
				UpstreamModel: "gemini-2.0-flash",
			},
			endpoint: "generateContent",
			want:     "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:generateContent",
		},
		{
			name: "stream generate content keeps v1beta suffix and requests sse",
			upstream: domainaigateway.LLMUpstream{
				BaseURL: "https://generativelanguage.googleapis.com/v1beta",
			},
			route: domainaigateway.LLMModelRoute{
				UpstreamModel: "models/gemini-2.0-flash",
			},
			endpoint: "streamGenerateContent",
			want:     "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.0-flash:streamGenerateContent?alt=sse",
		},
		{
			name: "metadata version override",
			upstream: domainaigateway.LLMUpstream{
				BaseURL: "https://generativelanguage.googleapis.com",
				Metadata: map[string]any{
					"gemini": map[string]any{"apiVersion": "v1"},
				},
			},
			route: domainaigateway.LLMModelRoute{
				UpstreamModel: "gemini-2.0-flash",
			},
			endpoint: "models",
			want:     "https://generativelanguage.googleapis.com/v1/models",
		},
		{
			name: "interactions appends v1beta resource path",
			upstream: domainaigateway.LLMUpstream{
				BaseURL: "https://generativelanguage.googleapis.com",
			},
			route: domainaigateway.LLMModelRoute{
				UpstreamModel: "gemini-2.5-flash-image",
			},
			endpoint: "interactions",
			want:     "https://generativelanguage.googleapis.com/v1beta/interactions",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := relayEndpointURLForUpstream(tt.upstream, tt.route, "gemini", tt.endpoint)
			if err != nil {
				t.Fatalf("relayEndpointURLForUpstream returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("relayEndpointURLForUpstream = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRelayEndpointURLUsesCohereNativePaths(t *testing.T) {
	for _, tt := range []struct {
		name     string
		upstream domainaigateway.LLMUpstream
		endpoint string
		want     string
	}{
		{
			name: "rerank appends v2",
			upstream: domainaigateway.LLMUpstream{
				BaseURL: "https://api.cohere.com",
			},
			endpoint: "rerank",
			want:     "https://api.cohere.com/v2/rerank",
		},
		{
			name: "rerank keeps v2 suffix",
			upstream: domainaigateway.LLMUpstream{
				BaseURL: "https://api.cohere.com/v2",
			},
			endpoint: "rerank",
			want:     "https://api.cohere.com/v2/rerank",
		},
		{
			name: "models uses v1",
			upstream: domainaigateway.LLMUpstream{
				BaseURL: "https://api.cohere.com/v2",
			},
			endpoint: "models",
			want:     "https://api.cohere.com/v1/models",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			got, err := relayEndpointURLForUpstream(tt.upstream, domainaigateway.LLMModelRoute{}, "cohere", tt.endpoint)
			if err != nil {
				t.Fatalf("relayEndpointURLForUpstream returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("relayEndpointURLForUpstream = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestApplyRelayUpstreamHeadersUsesBearerForFirstClassOpenAICompatibleProviders(t *testing.T) {
	for _, providerKind := range []string{"deepseek", "qwen", "openrouter"} {
		t.Run(providerKind, func(t *testing.T) {
			headers := http.Header{}
			applyRelayUpstreamHeaders(headers, LLMRelayHTTPRequest{
				ProviderKind: providerKind,
				Endpoint:     "chat/completions",
				Headers:      http.Header{"Accept": []string{"application/json"}},
			}, domainaigateway.LLMUpstream{
				DefaultHeaders: map[string]any{
					"HTTP-Referer":  "https://soha.example",
					"Authorization": "Bearer should-not-forward",
				},
			}, "sk-provider")

			if got := headers.Get("Authorization"); got != "Bearer sk-provider" {
				t.Fatalf("Authorization = %q", got)
			}
			if got := headers.Get("x-api-key"); got != "" {
				t.Fatalf("x-api-key should be empty, got %q", got)
			}
			if got := headers.Get("HTTP-Referer"); got != "https://soha.example" {
				t.Fatalf("HTTP-Referer = %q", got)
			}
		})
	}
}

func TestApplyRelayUpstreamHeadersUsesGoogleAPIKeyForGemini(t *testing.T) {
	headers := http.Header{}
	applyRelayUpstreamHeaders(headers, LLMRelayHTTPRequest{
		ProviderKind: "gemini",
		Endpoint:     "generateContent",
		Headers:      http.Header{"Accept": []string{"application/json"}},
	}, domainaigateway.LLMUpstream{
		DefaultHeaders: map[string]any{
			"X-Goog-Api-Key":      "should-not-forward",
			"Authorization":       "Bearer should-not-forward",
			"X-Goog-User-Project": "soha-project",
		},
	}, "gemini-provider-key")

	if got := headers.Get("x-goog-api-key"); got != "gemini-provider-key" {
		t.Fatalf("x-goog-api-key = %q", got)
	}
	if got := headers.Get("Authorization"); got != "" {
		t.Fatalf("Authorization should be empty, got %q", got)
	}
	if got := headers.Get("X-Goog-User-Project"); got != "soha-project" {
		t.Fatalf("X-Goog-User-Project = %q", got)
	}
}

func TestApplyRelayUpstreamHeadersFiltersCohereAPIKeyHeader(t *testing.T) {
	headers := http.Header{}
	applyRelayUpstreamHeaders(headers, LLMRelayHTTPRequest{
		ProviderKind: "cohere",
		Endpoint:     "rerank",
		Headers: http.Header{
			"Accept":              []string{"application/json"},
			"OpenAI-Organization": []string{"org-should-not-forward"},
		},
	}, domainaigateway.LLMUpstream{
		DefaultHeaders: map[string]any{
			"Cohere-Api-Key": "should-not-forward",
			"X-Client-Name":  "opensoha",
		},
	}, "cohere-provider-key")

	if got := headers.Get("Authorization"); got != "Bearer cohere-provider-key" {
		t.Fatalf("Authorization = %q", got)
	}
	if got := headers.Get("Cohere-Api-Key"); got != "" {
		t.Fatalf("Cohere-Api-Key should be empty, got %q", got)
	}
	if got := headers.Get("OpenAI-Organization"); got != "" {
		t.Fatalf("OpenAI-Organization should be empty, got %q", got)
	}
	if got := headers.Get("X-Client-Name"); got != "opensoha" {
		t.Fatalf("X-Client-Name = %q", got)
	}
}

func TestApplyRelayUpstreamHeadersUsesAPIKeyForAzureOpenAI(t *testing.T) {
	headers := http.Header{}
	applyRelayUpstreamHeaders(headers, LLMRelayHTTPRequest{
		ProviderKind: "azure-openai",
		Endpoint:     "chat/completions",
		Headers:      http.Header{"Accept": []string{"application/json"}},
	}, domainaigateway.LLMUpstream{
		DefaultHeaders: map[string]any{
			"api-key":       "should-not-forward",
			"Authorization": "Bearer should-not-forward",
			"X-Client-Name": "soha",
		},
	}, "sk-azure")

	if got := headers.Get("api-key"); got != "sk-azure" {
		t.Fatalf("api-key = %q", got)
	}
	if got := headers.Get("Authorization"); got != "" {
		t.Fatalf("Authorization should be empty, got %q", got)
	}
	if got := headers.Get("X-Client-Name"); got != "soha" {
		t.Fatalf("X-Client-Name = %q", got)
	}
}

func TestRelayResponseCacheKeyIncludesAzureOpenAITargetFingerprint(t *testing.T) {
	route := domainaigateway.LLMModelRoute{
		ID:            "route-azure",
		PublicModel:   "gpt-public",
		ProviderKind:  "azure-openai",
		UpstreamModel: "gpt-upstream",
	}
	first := relaySelection{
		route: route,
		upstream: domainaigateway.LLMUpstream{
			ID:           "upstream-azure",
			BaseURL:      "https://example.openai.azure.com",
			ProviderKind: "azure-openai",
			Metadata: map[string]any{
				"azureOpenAI": map[string]any{
					"apiVersion": "2024-10-21",
				},
			},
		},
	}
	second := first
	second.upstream.Metadata = map[string]any{
		"azureOpenAI": map[string]any{
			"apiVersion": "2025-04-01-preview",
		},
	}
	req := LLMRelayHTTPRequest{
		ProviderKind: "azure_openai",
		Endpoint:     "chat/completions",
	}

	firstKey := relayResponseCacheKey("secret", "scope", req, first, "gpt-public", "request-hash", "v1")
	secondKey := relayResponseCacheKey("secret", "scope", req, second, "gpt-public", "request-hash", "v1")

	if firstKey == "" || secondKey == "" {
		t.Fatalf("cache keys should be populated, got first=%q second=%q", firstKey, secondKey)
	}
	if firstKey == secondKey {
		t.Fatalf("cache keys should differ when Azure OpenAI apiVersion changes: %s", firstKey)
	}
}

func TestRelayLLMHTTPRetriesNonStreamRetryableUpstreamStatus(t *testing.T) {
	const upstreamKey = "sk-openai-upstream-test"
	firstCalls := 0
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls++
		http.Error(w, `{"error":{"message":"temporary"}}`, http.StatusServiceUnavailable)
	}))
	defer first.Close()
	secondCalls := 0
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-2","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`)
	}))
	defer second.Close()
	repo := relayRepoForTwoUpstreams(t, first.URL, second.URL, upstreamKey)
	service := newRelayRuntimeTestService(repo, first.Client())
	restore := stubRelayRandomIntn(func(n int) int { return 0 })
	defer restore()
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"hi"}]}`),
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("calls first=%d second=%d, want 1/1", firstCalls, secondCalls)
	}
	logs := repo.logs()
	if len(logs) != 2 {
		t.Fatalf("log count = %d, want 2", len(logs))
	}
	if logs[0].Status != "failure" || logs[0].ErrorCode != "upstream_5xx" {
		t.Fatalf("first log = %#v, want retryable upstream failure", logs[0])
	}
	if logs[1].Status != "success" || logs[1].UpstreamID != "upstream-second" {
		t.Fatalf("second log = %#v, want success on upstream-second", logs[1])
	}
}

func TestRelayLLMHTTPEnforcesRateLimitOnFallbackSelection(t *testing.T) {
	const upstreamKey = "sk-openai-upstream-test"
	firstCalls := 0
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls++
		http.Error(w, `{"error":{"message":"temporary"}}`, http.StatusServiceUnavailable)
	}))
	defer first.Close()
	secondCalls := 0
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-2","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"total_tokens":1}}`)
	}))
	defer second.Close()
	now := time.Now().UTC()
	repo := relayRepoForTwoUpstreams(t, first.URL, second.URL, upstreamKey)
	repo.routes[1].Metadata = map[string]any{"rateLimit": map[string]any{"tpm": 1}}
	repo.callLogs = append(repo.callLogs, domainaigateway.LLMCallLog{
		ID:          "existing-usage",
		PublicModel: "gpt-public",
		TotalTokens: 1,
		CreatedAt:   now.Add(-10 * time.Second),
	})
	service := newRelayRuntimeTestService(repo, first.Client())
	restore := stubRelayRandomIntn(func(n int) int { return 0 })
	defer restore()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"hi"}]}`),
	}, httptest.NewRecorder())

	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("RelayLLMHTTP error = %v, want access denied", err)
	}
	if firstCalls != 1 || secondCalls != 0 {
		t.Fatalf("calls first=%d second=%d, want first retry then second blocked by rate limit", firstCalls, secondCalls)
	}
	assertRelayLogWithErrorCode(t, repo, "token_per_minute_limited")
}

func TestRelayLLMHTTPRejectsUnauthorizedExplicitUpstreamSelection(t *testing.T) {
	const upstreamKey = "sk-openai-upstream-test"
	firstCalls := 0
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls++
		http.Error(w, `{"error":{"message":"unexpected first upstream call"}}`, http.StatusInternalServerError)
	}))
	defer first.Close()
	secondCalls := 0
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls++
		http.Error(w, `{"error":{"message":"unexpected second upstream call"}}`, http.StatusInternalServerError)
	}))
	defer second.Close()
	repo := relayRepoForTwoUpstreams(t, first.URL, second.URL, upstreamKey)
	service := newRelayRuntimeTestService(repo, first.Client())

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{relayHeaderUpstreamID: []string{"upstream-second"}},
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"hi"}]}`),
	}, httptest.NewRecorder())

	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("RelayLLMHTTP error = %v, want access denied", err)
	}
	if firstCalls != 0 || secondCalls != 0 {
		t.Fatalf("upstream calls first=%d second=%d, want 0/0", firstCalls, secondCalls)
	}
	if logs := repo.logs(); len(logs) != 0 {
		t.Fatalf("expected no relay call log before authorized upstream selection, got %#v", logs)
	}
}

func TestRelayLLMHTTPRejectsUnauthorizedRouteTraceHeaders(t *testing.T) {
	const upstreamKey = "sk-openai-upstream-test"
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"chatcmpl-trace","object":"chat.completion","choices":[]}`)
	}))
	defer upstream.Close()
	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	service := newRelayRuntimeTestService(repo, upstream.Client())

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{relayHeaderRouteTrace: []string{"true"}},
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"hi"}]}`),
	}, httptest.NewRecorder())

	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("RelayLLMHTTP error = %v, want access denied", err)
	}
	if upstreamCalls != 0 {
		t.Fatalf("upstream calls = %d, want 0", upstreamCalls)
	}
}

func TestRelayLLMHTTPAllowsManagerExplicitUpstreamAndRouteTrace(t *testing.T) {
	const upstreamKey = "sk-openai-upstream-test"
	firstCalls := 0
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls++
		http.Error(w, `{"error":{"message":"should not be selected"}}`, http.StatusServiceUnavailable)
	}))
	defer first.Close()
	secondCalls := 0
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-manager-explicit","object":"chat.completion","choices":[],"usage":{"total_tokens":1}}`)
	}))
	defer second.Close()
	repo := relayRepoForTwoUpstreams(t, first.URL, second.URL, upstreamKey)
	service := newRelayRuntimeTestService(repo, first.Client())
	principal := relayTestPrincipal()
	principal.PermissionKeys = []string{appaccess.PermAIGatewayRelayInvoke, appaccess.PermAIGatewayRelayManage}
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), principal, relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers: http.Header{
			relayHeaderUpstreamID: []string{"upstream-second"},
			relayHeaderRouteTrace: []string{"true"},
		},
		Body: []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"hi"}]}`),
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	if firstCalls != 0 || secondCalls != 1 {
		t.Fatalf("calls first=%d second=%d, want 0/1", firstCalls, secondCalls)
	}
	if recorder.Header().Get("X-Soha-Upstream-ID") != "upstream-second" {
		t.Fatalf("missing manager route trace header: %#v", recorder.Header())
	}
}

func TestRelayLLMHTTPAllowsTokenExplicitUpstreamSelectionAndWritesTraceHeaders(t *testing.T) {
	const upstreamKey = "sk-openai-upstream-test"
	firstCalls := 0
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls++
		http.Error(w, `{"error":{"message":"should not be selected"}}`, http.StatusServiceUnavailable)
	}))
	defer first.Close()
	secondCalls := 0
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-explicit","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"total_tokens":1}}`)
	}))
	defer second.Close()
	repo := relayRepoForTwoUpstreams(t, first.URL, second.URL, upstreamKey)
	service := newRelayRuntimeTestService(repo, first.Client())
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
		"purpose":                LLMRelayTokenPurpose,
		"allowedUpstreamIds":     []string{"upstream-second"},
		"allowUpstreamSelection": true,
		"allowRouteTrace":        true,
	}), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers: http.Header{
			relayHeaderUpstreamID: []string{"upstream-second"},
			relayHeaderRouteTrace: []string{"true"},
		},
		Body: []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"hi"}]}`),
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d, body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	if firstCalls != 0 || secondCalls != 1 {
		t.Fatalf("calls first=%d second=%d, want 0/1", firstCalls, secondCalls)
	}
	header := recorder.Header()
	if header.Get("X-Soha-Route-ID") != "route-second" || header.Get("X-Soha-Upstream-ID") != "upstream-second" {
		t.Fatalf("unexpected route trace headers: %#v", header)
	}
	if header.Get("X-Soha-Provider-Kind") != "openai" || header.Get("X-Soha-Upstream-Model") != "gpt-upstream" || header.Get("X-Soha-Cache-Status") != relayCacheBypass {
		t.Fatalf("missing route trace metadata headers: %#v", header)
	}
	for name, values := range header {
		joined := strings.Join(values, ",")
		if strings.Contains(joined, upstreamKey) || strings.Contains(strings.ToLower(name), "authorization") {
			t.Fatalf("route trace leaked sensitive header %s=%q", name, joined)
		}
	}
	log := singleRelayLog(t, repo)
	if log.UpstreamID != "upstream-second" || log.RouteTrace["routeId"] != "route-second" {
		t.Fatalf("unexpected route trace log: %#v", log)
	}
}

func TestRelayLLMHTTPSeparatesTraceAndUpstreamSelectionTokenGrants(t *testing.T) {
	const upstreamKey = "sk-openai-upstream-test"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-grants","object":"chat.completion","choices":[],"usage":{"total_tokens":1}}`)
	}))
	defer upstream.Close()
	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	service := newRelayRuntimeTestService(repo, upstream.Client())
	baseRequest := LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"hi"}]}`),
	}

	traceOnlyRequest := baseRequest
	traceOnlyRequest.Headers = http.Header{relayHeaderUpstreamID: []string{"upstream-openai"}}
	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
		"purpose":            LLMRelayTokenPurpose,
		"allowedUpstreamIds": []string{"upstream-openai"},
		"allowRouteTrace":    true,
	}), traceOnlyRequest, httptest.NewRecorder())
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("trace-only grant upstream selection error = %v, want access denied", err)
	}

	upstreamOnlyRequest := baseRequest
	upstreamOnlyRequest.Headers = http.Header{relayHeaderRouteTrace: []string{"true"}}
	err = service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
		"purpose":                LLMRelayTokenPurpose,
		"allowedUpstreamIds":     []string{"upstream-openai"},
		"allowUpstreamSelection": true,
	}), upstreamOnlyRequest, httptest.NewRecorder())
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("upstream-only grant route trace error = %v, want access denied", err)
	}

	legacyDebugRequest := baseRequest
	legacyDebugRequest.Headers = http.Header{relayHeaderRouteTrace: []string{"true"}}
	err = service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(map[string]any{
		"purpose":           LLMRelayTokenPurpose,
		"allowDebugHeaders": true,
		"debug":             true,
	}), legacyDebugRequest, httptest.NewRecorder())
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("legacy debug aliases route trace error = %v, want access denied", err)
	}
}

func TestRelayLLMHTTPOpensCircuitBreakerAfterRetryableFailure(t *testing.T) {
	const upstreamKey = "sk-openai-upstream-test"
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"temporary"}}`, http.StatusServiceUnavailable)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-circuit","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"total_tokens":1}}`)
	}))
	defer second.Close()
	repo := relayRepoForTwoUpstreams(t, first.URL, second.URL, upstreamKey)
	repo.upstreams[0].Metadata = map[string]any{"circuitBreaker": map[string]any{"failureThreshold": 1, "openSeconds": 60}}
	audit := &captureAuditRecorder{}
	service := newRelayRuntimeTestServiceWithAudit(repo, first.Client(), audit, &memoryGatewayRepository{})
	restore := stubRelayRandomIntn(func(n int) int { return 0 })
	defer restore()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"hi"}]}`),
	}, httptest.NewRecorder())

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	firstUpstream, err := repo.GetLLMUpstream(context.Background(), "upstream-first")
	if err != nil {
		t.Fatalf("GetLLMUpstream returned error: %v", err)
	}
	if relayHealthStatus(firstUpstream.Health) != "open" || intFromAny(firstUpstream.Health["consecutiveFailures"]) != 1 {
		t.Fatalf("expected first upstream circuit to open, got %#v", firstUpstream.Health)
	}
	if until, ok := relayHealthUntil(firstUpstream.Health, time.Now().UTC()); !ok || !until.After(time.Now().UTC()) {
		t.Fatalf("expected future circuitOpenUntil, got %#v", firstUpstream.Health)
	}
	if !slices.ContainsFunc(audit.entries, func(entry domainaudit.Entry) bool {
		return entry.Action == "ai_gateway.relay.upstream.circuit_open" && entry.Result == "failure"
	}) {
		t.Fatalf("expected circuit_open audit entry, got %#v", audit.entries)
	}
	if !slices.ContainsFunc(repo.healthEvents(), func(event domainaigateway.LLMHealthEvent) bool {
		return event.EventType == "ai_gateway.relay.upstream.circuit_open" && event.Status == "failure" && event.UpstreamID == "upstream-first"
	}) {
		t.Fatalf("expected circuit_open health event, got %#v", repo.healthEvents())
	}
}

func TestRelayLLMHTTPClearsCircuitBreakerOnHalfOpenSuccess(t *testing.T) {
	const upstreamKey = "sk-openai-upstream-test"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-half-open","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"total_tokens":1}}`)
	}))
	defer upstream.Close()
	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	repo.upstreams[0].Health = map[string]any{
		"circuitState":        "open",
		"circuitOpenUntil":    time.Now().UTC().Add(-time.Minute).Format(time.RFC3339Nano),
		"consecutiveFailures": 3,
		"lastErrorCode":       "upstream_5xx",
	}
	audit := &captureAuditRecorder{}
	service := newRelayRuntimeTestServiceWithAudit(repo, upstream.Client(), audit, &memoryGatewayRepository{})

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"hi"}]}`),
	}, httptest.NewRecorder())

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	updated, err := repo.GetLLMUpstream(context.Background(), "upstream-openai")
	if err != nil {
		t.Fatalf("GetLLMUpstream returned error: %v", err)
	}
	if relayUpstreamCircuitOpen(updated, time.Now().UTC()) || intFromAny(updated.Health["consecutiveFailures"]) != 0 {
		t.Fatalf("expected circuit metadata to recover, got %#v", updated.Health)
	}
	if _, exists := updated.Health["circuitOpenUntil"]; exists {
		t.Fatalf("expected circuitOpenUntil to be cleared, got %#v", updated.Health)
	}
	if !slices.ContainsFunc(audit.entries, func(entry domainaudit.Entry) bool {
		return entry.Action == "ai_gateway.relay.upstream.circuit_recovered" && entry.Result == "success"
	}) {
		t.Fatalf("expected circuit_recovered audit entry, got %#v", audit.entries)
	}
	if !slices.ContainsFunc(repo.healthEvents(), func(event domainaigateway.LLMHealthEvent) bool {
		return event.EventType == "ai_gateway.relay.upstream.circuit_recovered" && event.Status == "success" && event.UpstreamID == "upstream-openai"
	}) {
		t.Fatalf("expected circuit_recovered health event, got %#v", repo.healthEvents())
	}
}

func TestRunLLMRelayHealthChecksMarksFailedActiveUpstreamDegraded(t *testing.T) {
	const upstreamKey = "sk-openai-upstream-test"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"message":"temporary"}}`, http.StatusServiceUnavailable)
	}))
	defer upstream.Close()
	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	repo.upstreams[0].Health = map[string]any{"circuitState": "closed", "consecutiveFailures": 1}
	repo.upstreams[0].Metadata = map[string]any{"healthCheck": map[string]any{"degradeAfterFailures": 1}}
	audit := &captureAuditRecorder{}
	service := newRelayRuntimeTestServiceWithAudit(repo, upstream.Client(), audit, &memoryGatewayRepository{})

	run, err := service.RunLLMRelayHealthChecks(context.Background(), relayTestManagePrincipal())

	if err != nil {
		t.Fatalf("RunLLMRelayHealthChecks returned error: %v", err)
	}
	if run.Checked != 1 || run.Failed != 1 || run.Degraded != 1 {
		t.Fatalf("unexpected health check run: %#v", run)
	}
	updated, err := repo.GetLLMUpstream(context.Background(), "upstream-openai")
	if err != nil {
		t.Fatalf("GetLLMUpstream returned error: %v", err)
	}
	if updated.Status != "degraded" {
		t.Fatalf("expected upstream degraded, got %#v", updated)
	}
	if updated.Health["circuitState"] != "closed" || intFromAny(updated.Health["consecutiveFailures"]) != 1 {
		t.Fatalf("expected circuit metadata to be preserved, got %#v", updated.Health)
	}
	if updated.Health["degradedBy"] != "relay_health_check" || updated.Health["lastHealthStatus"] != "failure" || updated.Health["lastHealthHTTPStatus"] != http.StatusServiceUnavailable {
		t.Fatalf("unexpected health metadata: %#v", updated.Health)
	}
	if !slices.ContainsFunc(repo.healthEvents(), func(event domainaigateway.LLMHealthEvent) bool {
		return event.EventType == "ai_gateway.relay.upstream.health_degraded" && event.Status == "failure" && event.UpstreamID == "upstream-openai"
	}) {
		t.Fatalf("expected health_degraded event, got %#v", repo.healthEvents())
	}
	if !slices.ContainsFunc(audit.entries, func(entry domainaudit.Entry) bool {
		return entry.Action == "ai_gateway.relay.upstream.health_degraded" && entry.Result == "failure"
	}) {
		t.Fatalf("expected health_degraded audit entry, got %#v", audit.entries)
	}
}

func TestRunLLMRelayHealthChecksRestoresHealthManagedDegradedUpstream(t *testing.T) {
	const upstreamKey = "sk-openai-upstream-test"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[]}`)
	}))
	defer upstream.Close()
	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	repo.upstreams[0].Status = "degraded"
	repo.upstreams[0].Health = map[string]any{
		"degradedBy":                     "relay_health_check",
		"healthCheckConsecutiveFailures": 2,
		"lastHealthErrorCode":            "upstream_5xx",
	}
	audit := &captureAuditRecorder{}
	service := newRelayRuntimeTestServiceWithAudit(repo, upstream.Client(), audit, &memoryGatewayRepository{})

	run, err := service.RunLLMRelayHealthChecks(context.Background(), relayTestManagePrincipal())

	if err != nil {
		t.Fatalf("RunLLMRelayHealthChecks returned error: %v", err)
	}
	if run.Checked != 1 || run.Healthy != 1 || run.Recovered != 1 {
		t.Fatalf("unexpected health check run: %#v", run)
	}
	updated, err := repo.GetLLMUpstream(context.Background(), "upstream-openai")
	if err != nil {
		t.Fatalf("GetLLMUpstream returned error: %v", err)
	}
	if updated.Status != "active" {
		t.Fatalf("expected upstream active, got %#v", updated)
	}
	if _, exists := updated.Health["degradedBy"]; exists {
		t.Fatalf("expected degradedBy cleared, got %#v", updated.Health)
	}
	if updated.Health["lastHealthStatus"] != "success" || intFromAny(updated.Health["healthCheckConsecutiveSuccesses"]) != 1 {
		t.Fatalf("unexpected health metadata: %#v", updated.Health)
	}
	if !slices.ContainsFunc(repo.healthEvents(), func(event domainaigateway.LLMHealthEvent) bool {
		return event.EventType == "ai_gateway.relay.upstream.health_recovered" && event.Status == "success" && event.UpstreamID == "upstream-openai"
	}) {
		t.Fatalf("expected health_recovered event, got %#v", repo.healthEvents())
	}
}

func TestRunLLMRelayHealthChecksSkipsDisabledUpstream(t *testing.T) {
	const upstreamKey = "sk-openai-upstream-test"
	calls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()
	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	repo.upstreams[0].Status = "disabled"
	service := newRelayRuntimeTestService(repo, upstream.Client())

	run, err := service.RunLLMRelayHealthChecks(context.Background(), relayTestManagePrincipal())

	if err != nil {
		t.Fatalf("RunLLMRelayHealthChecks returned error: %v", err)
	}
	if run.Total != 1 || run.Checked != 0 || run.Skipped != 1 || calls != 0 {
		t.Fatalf("expected disabled upstream skipped, run=%#v calls=%d", run, calls)
	}
}

func TestRelayLLMHTTPHonorsFallbackPolicyMaxAttempts(t *testing.T) {
	const upstreamKey = "sk-openai-upstream-test"
	firstCalls := 0
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls++
		http.Error(w, `{"error":{"message":"temporary"}}`, http.StatusServiceUnavailable)
	}))
	defer first.Close()
	secondCalls := 0
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls++
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"id":"chatcmpl-2","object":"chat.completion","choices":[]}`)
	}))
	defer second.Close()
	repo := relayRepoForTwoUpstreams(t, first.URL, second.URL, upstreamKey)
	repo.routes[0].FallbackPolicy = map[string]any{"maxAttempts": 1}
	service := newRelayRuntimeTestService(repo, first.Client())
	restore := stubRelayRandomIntn(func(n int) int { return 0 })
	defer restore()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"hi"}]}`),
	}, httptest.NewRecorder())

	if !errors.Is(err, apperrors.ErrClusterUnready) {
		t.Fatalf("error = %v, want cluster unready", err)
	}
	if firstCalls != 1 || secondCalls != 0 {
		t.Fatalf("calls first=%d second=%d, want 1/0", firstCalls, secondCalls)
	}
}

func TestRelayLLMHTTPDoesNotRetryStreamingUpstreamStatus(t *testing.T) {
	const upstreamKey = "sk-openai-upstream-test"
	firstCalls := 0
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls++
		http.Error(w, `{"error":{"message":"stream failed"}}`, http.StatusServiceUnavailable)
	}))
	defer first.Close()
	secondCalls := 0
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls++
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `data: {"choices":[]}`)
	}))
	defer second.Close()
	repo := relayRepoForTwoUpstreams(t, first.URL, second.URL, upstreamKey)
	service := newRelayRuntimeTestService(repo, first.Client())
	restore := stubRelayRandomIntn(func(n int) int { return 0 })
	defer restore()
	recorder := httptest.NewRecorder()

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","stream":true,"messages":[{"role":"user","content":"hi"}]}`),
	}, recorder)

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	if firstCalls != 1 || secondCalls != 0 {
		t.Fatalf("calls first=%d second=%d, want 1/0", firstCalls, secondCalls)
	}
}

func TestRelayLLMHTTPEnforcesTokenRateLimit(t *testing.T) {
	const upstreamKey = "sk-openai-upstream-test"
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-rate","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"total_tokens":1}}`)
	}))
	defer upstream.Close()
	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	service := newRelayRuntimeTestService(repo, upstream.Client())
	accessCtx := relayTestAccessContext(map[string]any{
		"purpose":   LLMRelayTokenPurpose,
		"rateLimit": map[string]any{"rpm": 1, "mode": "counter"},
	})
	request := LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"hi"}]}`),
	}

	if err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), accessCtx, request, httptest.NewRecorder()); err != nil {
		t.Fatalf("first RelayLLMHTTP returned error: %v", err)
	}
	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), accessCtx, request, httptest.NewRecorder())
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("second error = %v, want access denied", err)
	}
	if upstreamCalls != 1 {
		t.Fatalf("upstream calls = %d, want 1", upstreamCalls)
	}
}

func TestRelayLLMHTTPEnforcesRouteModelRateLimitAcrossTokens(t *testing.T) {
	const upstreamKey = "sk-openai-upstream-test"
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-route-rate","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"total_tokens":1}}`)
	}))
	defer upstream.Close()
	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	repo.routes[0].Metadata = map[string]any{"rateLimit": map[string]any{"rpm": 1, "mode": "counter"}}
	service := newRelayRuntimeTestService(repo, upstream.Client())
	firstAccess := relayTestAccessContext(nil)
	secondAccess := relayTestAccessContext(nil)
	secondAccess.TokenID = "pat-2"
	secondAccess.TokenPrefix = "soha_pat_second"
	request := LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"hi"}]}`),
	}

	if err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), firstAccess, request, httptest.NewRecorder()); err != nil {
		t.Fatalf("first RelayLLMHTTP returned error: %v", err)
	}
	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), secondAccess, request, httptest.NewRecorder())
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("second error = %v, want access denied", err)
	}
	if upstreamCalls != 1 {
		t.Fatalf("upstream calls = %d, want 1", upstreamCalls)
	}
}

func TestRelayLLMHTTPEnforcesTokenPerMinuteLimit(t *testing.T) {
	const upstreamKey = "sk-openai-upstream-test"
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-token-tpm","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"total_tokens":1}}`)
	}))
	defer upstream.Close()
	now := time.Now().UTC()
	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	repo.callLogs = append(repo.callLogs,
		domainaigateway.LLMCallLog{ID: "same-token", TokenID: "pat-1", TokenKind: "personal_access_token", TotalTokens: 10, CreatedAt: now.Add(-10 * time.Second)},
		domainaigateway.LLMCallLog{ID: "other-token", TokenID: "pat-2", TokenKind: "personal_access_token", TotalTokens: 100, CreatedAt: now.Add(-10 * time.Second)},
		domainaigateway.LLMCallLog{ID: "old-token", TokenID: "pat-1", TokenKind: "personal_access_token", TotalTokens: 100, CreatedAt: now.Add(-2 * time.Minute)},
	)
	service := newRelayRuntimeTestService(repo, upstream.Client())
	accessCtx := relayTestAccessContext(map[string]any{
		"purpose":   LLMRelayTokenPurpose,
		"rateLimit": map[string]any{"tpm": 10},
	})

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), accessCtx, LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"hi"}]}`),
	}, httptest.NewRecorder())

	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("error = %v, want access denied", err)
	}
	if upstreamCalls != 0 {
		t.Fatalf("upstream calls = %d, want 0", upstreamCalls)
	}
	assertRelayLogWithErrorCode(t, repo, "token_per_minute_limited")
}

func TestRelayLLMHTTPIgnoresOldAndOtherTokenPerMinuteUsage(t *testing.T) {
	const upstreamKey = "sk-openai-upstream-test"
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-token-tpm-ok","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"total_tokens":1}}`)
	}))
	defer upstream.Close()
	now := time.Now().UTC()
	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	repo.callLogs = append(repo.callLogs,
		domainaigateway.LLMCallLog{ID: "other-token", TokenID: "pat-2", TokenKind: "personal_access_token", TotalTokens: 100, CreatedAt: now.Add(-10 * time.Second)},
		domainaigateway.LLMCallLog{ID: "old-token", TokenID: "pat-1", TokenKind: "personal_access_token", TotalTokens: 100, CreatedAt: now.Add(-2 * time.Minute)},
	)
	service := newRelayRuntimeTestService(repo, upstream.Client())
	accessCtx := relayTestAccessContext(map[string]any{
		"purpose":   LLMRelayTokenPurpose,
		"rateLimit": map[string]any{"tpm": 10},
	})

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), accessCtx, LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"hi"}]}`),
	}, httptest.NewRecorder())

	if err != nil {
		t.Fatalf("RelayLLMHTTP returned error: %v", err)
	}
	if upstreamCalls != 1 {
		t.Fatalf("upstream calls = %d, want 1", upstreamCalls)
	}
}

func TestRelayLLMHTTPEnforcesRouteModelTokenPerMinuteAcrossTokens(t *testing.T) {
	const upstreamKey = "sk-openai-upstream-test"
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-route-tpm","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"total_tokens":1}}`)
	}))
	defer upstream.Close()
	now := time.Now().UTC()
	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	repo.routes[0].Metadata = map[string]any{"rateLimit": map[string]any{"tokensPerMinute": 12}}
	repo.callLogs = append(repo.callLogs,
		domainaigateway.LLMCallLog{ID: "same-model", TokenID: "pat-1", PublicModel: "gpt-public", TotalTokens: 12, CreatedAt: now.Add(-10 * time.Second)},
		domainaigateway.LLMCallLog{ID: "other-model", TokenID: "pat-2", PublicModel: "gpt-other", TotalTokens: 100, CreatedAt: now.Add(-10 * time.Second)},
	)
	service := newRelayRuntimeTestService(repo, upstream.Client())
	accessCtx := relayTestAccessContext(nil)
	accessCtx.TokenID = "pat-2"
	accessCtx.TokenPrefix = "soha_pat_second"

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), accessCtx, LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"hi"}]}`),
	}, httptest.NewRecorder())

	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("error = %v, want access denied", err)
	}
	if upstreamCalls != 0 {
		t.Fatalf("upstream calls = %d, want 0", upstreamCalls)
	}
	assertRelayLogWithErrorCode(t, repo, "token_per_minute_limited")
}

func TestRelayLLMHTTPEnforcesUpstreamTokenPerMinuteLimit(t *testing.T) {
	const upstreamKey = "sk-openai-upstream-test"
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstreamCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-upstream-tpm","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"total_tokens":1}}`)
	}))
	defer upstream.Close()
	now := time.Now().UTC()
	repo := relayRepoForUpstream(t, upstream.URL, "openai", upstreamKey)
	repo.upstreams[0].Metadata = map[string]any{"rateLimit": map[string]any{"maxTokensPerMinute": 20}}
	repo.callLogs = append(repo.callLogs,
		domainaigateway.LLMCallLog{ID: "same-upstream", UpstreamID: "upstream-openai", TotalTokens: 20, CreatedAt: now.Add(-10 * time.Second)},
		domainaigateway.LLMCallLog{ID: "other-upstream", UpstreamID: "upstream-other", TotalTokens: 100, CreatedAt: now.Add(-10 * time.Second)},
	)
	service := newRelayRuntimeTestService(repo, upstream.Client())

	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"hi"}]}`),
	}, httptest.NewRecorder())

	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("error = %v, want access denied", err)
	}
	if upstreamCalls != 0 {
		t.Fatalf("upstream calls = %d, want 0", upstreamCalls)
	}
	assertRelayLogWithErrorCode(t, repo, "token_per_minute_limited")
}

func TestRelayLLMHTTPFallsBackWhenUpstreamConcurrencyLimitIsFull(t *testing.T) {
	const upstreamKey = "sk-openai-upstream-test"
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	firstCalls := 0
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		firstCalls++
		close(firstEntered)
		<-releaseFirst
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-first","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"first"}}],"usage":{"total_tokens":1}}`)
	}))
	defer first.Close()
	secondCalls := 0
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"chatcmpl-second","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"second"}}],"usage":{"total_tokens":1}}`)
	}))
	defer second.Close()
	repo := relayRepoForTwoUpstreams(t, first.URL, second.URL, upstreamKey)
	repo.upstreams[0].MaxConcurrency = 1
	repo.routes[0].Priority = 1
	repo.routes[1].Priority = 2
	service := newRelayRuntimeTestService(repo, first.Client())
	request := LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{},
		Body:         []byte(`{"model":"gpt-public","messages":[{"role":"user","content":"hi"}]}`),
	}
	firstErr := make(chan error, 1)
	go func() {
		firstErr <- service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), request, httptest.NewRecorder())
	}()
	select {
	case <-firstEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("first upstream request did not start")
	}

	recorder := httptest.NewRecorder()
	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), relayTestAccessContext(nil), request, recorder)
	close(releaseFirst)
	if err != nil {
		t.Fatalf("second RelayLLMHTTP returned error: %v", err)
	}
	if recorder.Code != http.StatusOK || !strings.Contains(recorder.Body.String(), "chatcmpl-second") {
		t.Fatalf("second response status=%d body=%s, want upstream-second success", recorder.Code, recorder.Body.String())
	}
	if err := <-firstErr; err != nil {
		t.Fatalf("first RelayLLMHTTP returned error: %v", err)
	}
	if firstCalls != 1 || secondCalls != 1 {
		t.Fatalf("calls first=%d second=%d, want 1/1", firstCalls, secondCalls)
	}
	logs := repo.logs()
	foundLimitLog := false
	for _, log := range logs {
		if log.Status == "rate_limited" && log.ErrorCode == "upstream_concurrency_limited" {
			foundLimitLog = true
		}
	}
	if !foundLimitLog {
		t.Fatalf("missing upstream concurrency rate_limited log: %#v", logs)
	}
}

func TestRelayLLMHTTPEnforcesTokenConcurrencyLimit(t *testing.T) {
	testRelayTokenConcurrencyLimit(t, false)
}

func TestRelayLLMHTTPEnforcesTokenStreamConcurrencyLimit(t *testing.T) {
	testRelayTokenConcurrencyLimit(t, true)
}

func testRelayTokenConcurrencyLimit(t *testing.T, stream bool) {
	t.Helper()
	firstEntered := make(chan struct{})
	releaseFirst := make(chan struct{})
	upstreamCalls := 0
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		upstreamCalls++
		close(firstEntered)
		<-releaseFirst
		if stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: [DONE]\n\n")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{ "id":"chatcmpl-token-concurrency","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"total_tokens":1}}`)
	}))
	defer upstream.Close()
	repo := relayRepoForUpstream(t, upstream.URL, "openai", "sk-openai-upstream-test")
	service := newRelayRuntimeTestService(repo, upstream.Client())
	claims := map[string]any{"purpose": LLMRelayTokenPurpose}
	errorCode := "token_concurrency_limited"
	if stream {
		claims["maxConcurrentStreams"] = 1
		errorCode = "token_stream_concurrency_limited"
	} else {
		claims["maxConcurrency"] = 1
	}
	accessCtx := relayTestAccessContext(claims)
	body := `{"model":"gpt-public","messages":[{"role":"user","content":"hi"}]}`
	if stream {
		body = `{"model":"gpt-public","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	}
	request := LLMRelayHTTPRequest{ProviderKind: "openai", Endpoint: "chat/completions", Method: http.MethodPost, Headers: http.Header{}, Body: []byte(body)}
	firstErr := make(chan error, 1)
	go func() {
		firstErr <- service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), accessCtx, request, httptest.NewRecorder())
	}()
	select {
	case <-firstEntered:
	case <-time.After(2 * time.Second):
		t.Fatal("first upstream request did not start")
	}
	err := service.RelayLLMHTTP(context.Background(), relayTestPrincipal(), accessCtx, request, httptest.NewRecorder())
	close(releaseFirst)
	if !errors.Is(err, apperrors.ErrAccessDenied) {
		t.Fatalf("second error = %v, want access denied", err)
	}
	if err := <-firstErr; err != nil {
		t.Fatalf("first RelayLLMHTTP returned error: %v", err)
	}
	if upstreamCalls != 1 {
		t.Fatalf("upstream calls = %d, want 1", upstreamCalls)
	}
	assertRelayLogWithErrorCode(t, repo, errorCode)
}

func newRelayRuntimeTestService(repo *relayTestRepository, client *http.Client) *Service {
	return newRelayRuntimeTestServiceWithAudit(repo, client, nil, nil)
}

func newRelayRuntimeTestServiceWithAudit(repo *relayTestRepository, client *http.Client, audit AuditRecorder, auditRepo AuditLogRepository) *Service {
	rateLimits := &fakeRateLimitBackend{}
	return NewWithDeps(ServiceDeps{
		Permissions: appaccess.NewPermissionResolver(stubRolePermissionReader{matrix: map[string][]string{
			"developer": {appaccess.PermAIGatewayRelayView, appaccess.PermAIGatewayRelayInvoke, appaccess.PermAIGatewayRelayManage, appaccess.PermObserveAIChatUse},
		}}),
		Audit:            audit,
		AuditLogs:        auditRepo,
		LLMRelay:         repo,
		HTTPClient:       client,
		RateLimits:       rateLimits,
		RateLimitBackend: rateLimits,
		RelayConfig: LLMRelayConfig{
			Enabled:                     true,
			DefaultTimeout:              2 * time.Second,
			StreamTimeout:               2 * time.Second,
			AllowInsecureUpstreamHTTP:   true,
			AllowPrivateUpstreamHosts:   true,
			IncludeUsageForOpenAIStream: true,
			CredentialEncryptionKey:     relayTestEncryptionKey,
		},
	})
}

func relayRepoForUpstream(t *testing.T, baseURL, providerKind, apiKey string) *relayTestRepository {
	t.Helper()
	ciphertext, err := secretcrypto.EncryptString(relayTestEncryptionKey, apiKey)
	if err != nil {
		t.Fatalf("encrypt upstream key: %v", err)
	}
	return &relayTestRepository{
		upstreams: []domainaigateway.LLMUpstream{{
			ID:               "upstream-openai",
			Name:             "test upstream",
			ProviderKind:     providerKind,
			BaseURL:          baseURL,
			APIKeyCiphertext: ciphertext,
			APIKeyPrefix:     relaySecretPrefix(apiKey),
			Status:           "active",
			Priority:         1,
			Weight:           1,
		}},
		routes: []domainaigateway.LLMModelRoute{{
			ID:            "route-1",
			PublicModel:   "gpt-public",
			ProviderKind:  providerKind,
			UpstreamID:    "upstream-openai",
			UpstreamModel: "gpt-upstream",
			Enabled:       true,
			Priority:      1,
			Weight:        1,
		}},
	}
}

func relayRepoForWeightedSelection(t *testing.T) *relayTestRepository {
	t.Helper()
	ciphertext, err := secretcrypto.EncryptString(relayTestEncryptionKey, "sk-test")
	if err != nil {
		t.Fatalf("encrypt upstream key: %v", err)
	}
	return &relayTestRepository{
		upstreams: []domainaigateway.LLMUpstream{
			{ID: "upstream-light", Name: "light", ProviderKind: "openai", BaseURL: "https://light.example", APIKeyCiphertext: ciphertext, Status: "active", Priority: 1, Weight: 1},
			{ID: "upstream-heavy", Name: "heavy", ProviderKind: "openai", BaseURL: "https://heavy.example", APIKeyCiphertext: ciphertext, Status: "active", Priority: 1, Weight: 1},
		},
		routes: []domainaigateway.LLMModelRoute{
			{ID: "route-light", PublicModel: "gpt-public", ProviderKind: "openai", UpstreamID: "upstream-light", UpstreamModel: "gpt-upstream", Enabled: true, Priority: 1, Weight: 1},
			{ID: "route-heavy", PublicModel: "gpt-public", ProviderKind: "openai", UpstreamID: "upstream-heavy", UpstreamModel: "gpt-upstream", Enabled: true, Priority: 1, Weight: 5},
		},
	}
}

func relayRepoForTwoUpstreams(t *testing.T, firstURL, secondURL, apiKey string) *relayTestRepository {
	t.Helper()
	ciphertext, err := secretcrypto.EncryptString(relayTestEncryptionKey, apiKey)
	if err != nil {
		t.Fatalf("encrypt upstream key: %v", err)
	}
	return &relayTestRepository{
		upstreams: []domainaigateway.LLMUpstream{
			{ID: "upstream-first", Name: "first", ProviderKind: "openai", BaseURL: firstURL, APIKeyCiphertext: ciphertext, APIKeyPrefix: relaySecretPrefix(apiKey), Status: "active", Priority: 1, Weight: 1},
			{ID: "upstream-second", Name: "second", ProviderKind: "openai", BaseURL: secondURL, APIKeyCiphertext: ciphertext, APIKeyPrefix: relaySecretPrefix(apiKey), Status: "active", Priority: 1, Weight: 1},
		},
		routes: []domainaigateway.LLMModelRoute{
			{ID: "route-first", PublicModel: "gpt-public", ProviderKind: "openai", UpstreamID: "upstream-first", UpstreamModel: "gpt-upstream", Enabled: true, Priority: 1, Weight: 1},
			{ID: "route-second", PublicModel: "gpt-public", ProviderKind: "openai", UpstreamID: "upstream-second", UpstreamModel: "gpt-upstream", Enabled: true, Priority: 1, Weight: 1},
		},
	}
}

func stubRelayRandomIntn(fn func(int) int) func() {
	previous := relayRandomIntn
	relayRandomIntn = fn
	return func() {
		relayRandomIntn = previous
	}
}

func relayTestPrincipal() domainidentity.Principal {
	principal := testPrincipal("developer")
	principal.PermissionKeys = []string{appaccess.PermAIGatewayRelayInvoke}
	return principal
}

func relayWorkbenchTestPrincipal() domainidentity.Principal {
	principal := testPrincipal("developer")
	principal.PermissionKeys = []string{appaccess.PermObserveAIChatUse}
	return principal
}

func relayTestViewPrincipal() domainidentity.Principal {
	principal := testPrincipal("developer")
	principal.PermissionKeys = []string{appaccess.PermAIGatewayRelayView}
	return principal
}

func relayTestManagePrincipal() domainidentity.Principal {
	principal := testPrincipal("developer")
	principal.PermissionKeys = []string{appaccess.PermAIGatewayRelayManage}
	return principal
}

func relayTestAccessContext(metadata map[string]any) domainidentity.AccessContext {
	if metadata == nil {
		metadata = map[string]any{"purpose": LLMRelayTokenPurpose}
	}
	return domainidentity.AccessContext{
		TokenID:     "pat-1",
		TokenKind:   "personal_access_token",
		TokenPrefix: "soha_pat_test",
		SubjectType: "user",
		SubjectID:   "user-1",
		Scopes:      []string{"relay"},
		Metadata:    metadata,
	}
}

func decodeRelayTestJSON(t *testing.T, body io.Reader) map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		t.Fatalf("decode upstream request: %v", err)
	}
	return payload
}

type relayTestMultipartPart struct {
	name        string
	filename    string
	contentType string
	body        string
}

type relayMultipartFields struct {
	fields    map[string]string
	files     map[string]relayMultipartFile
	fileField string
	fileName  string
	fileBody  string
}

type relayMultipartFile struct {
	name string
	body string
}

func relayTestMultipartBody(t *testing.T, fields map[string]string, fileField, fileName, fileContentType, fileBody string) ([]byte, string) {
	t.Helper()
	parts := make([]relayTestMultipartPart, 0, len(fields)+1)
	for key, value := range fields {
		parts = append(parts, relayTestMultipartPart{name: key, body: value})
	}
	if fileField != "" {
		parts = append(parts, relayTestMultipartPart{
			name:        fileField,
			filename:    fileName,
			contentType: fileContentType,
			body:        fileBody,
		})
	}
	return relayTestMultipartBodyWithParts(t, parts)
}

func relayTestMultipartBodyWithParts(t *testing.T, parts []relayTestMultipartPart) ([]byte, string) {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, part := range parts {
		if part.filename == "" {
			if err := writer.WriteField(part.name, part.body); err != nil {
				t.Fatalf("write multipart field: %v", err)
			}
			continue
		}
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, part.name, part.filename))
		if part.contentType != "" {
			header.Set("Content-Type", part.contentType)
		}
		partWriter, err := writer.CreatePart(header)
		if err != nil {
			t.Fatalf("create multipart part: %v", err)
		}
		if _, err := io.WriteString(partWriter, part.body); err != nil {
			t.Fatalf("write multipart part: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}
	return body.Bytes(), writer.FormDataContentType()
}

func decodeRelayTestMultipart(t *testing.T, r *http.Request) relayMultipartFields {
	t.Helper()
	if err := r.ParseMultipartForm(32 << 20); err != nil {
		t.Fatalf("parse upstream multipart request: %v", err)
	}
	out := relayMultipartFields{
		fields: map[string]string{},
		files:  map[string]relayMultipartFile{},
	}
	for key, values := range r.MultipartForm.Value {
		if len(values) > 0 {
			out.fields[key] = values[0]
		}
	}
	for key, files := range r.MultipartForm.File {
		if len(files) == 0 {
			continue
		}
		file, err := files[0].Open()
		if err != nil {
			t.Fatalf("open multipart file: %v", err)
		}
		data, err := io.ReadAll(file)
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
		if err != nil {
			t.Fatalf("read multipart file: %v", err)
		}
		uploaded := relayMultipartFile{
			name: files[0].Filename,
			body: string(data),
		}
		out.files[key] = uploaded
		if out.fileField == "" {
			out.fileField = key
			out.fileName = uploaded.name
			out.fileBody = uploaded.body
		}
	}
	return out
}

func assertNativeRelayBody(t *testing.T, body, expectedID string) {
	t.Helper()
	if strings.Contains(body, `"data":`) && !strings.Contains(body, expectedID) {
		t.Fatalf("body looks enveloped or missing provider id: %s", body)
	}
	if !strings.Contains(body, expectedID) {
		t.Fatalf("provider response was not relayed: %s", body)
	}
}

func singleRelayLog(t *testing.T, repo *relayTestRepository) domainaigateway.LLMCallLog {
	t.Helper()
	logs := repo.logs()
	if len(logs) != 1 {
		t.Fatalf("call logs = %#v, want one", logs)
	}
	return logs[0]
}

func assertRelayLogWithErrorCode(t *testing.T, repo *relayTestRepository, errorCode string) {
	t.Helper()
	for _, log := range repo.logs() {
		if log.ErrorCode == errorCode {
			return
		}
	}
	t.Fatalf("missing relay log with error code %s: %#v", errorCode, repo.logs())
}

func relayLogWithUpstream(t *testing.T, repo *relayTestRepository, upstreamID string) domainaigateway.LLMCallLog {
	t.Helper()
	for _, log := range repo.logs() {
		if log.UpstreamID == upstreamID {
			return log
		}
	}
	t.Fatalf("missing relay log for upstream %s: %#v", upstreamID, repo.logs())
	return domainaigateway.LLMCallLog{}
}

type cancelingRelayWriter struct {
	header http.Header
	status int
	cancel context.CancelFunc
}

func (w *cancelingRelayWriter) Header() http.Header {
	return w.header
}

func (w *cancelingRelayWriter) WriteHeader(status int) {
	w.status = status
}

func (w *cancelingRelayWriter) Write([]byte) (int, error) {
	w.cancel()
	return 0, errors.New("client connection closed")
}

func (w *cancelingRelayWriter) Flush() {}
