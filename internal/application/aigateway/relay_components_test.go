package aigateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
	domainidentity "github.com/opensoha/soha/internal/domain/identity"
	"github.com/opensoha/soha/internal/platform/apperrors"
)

func assertOpenAIChatRelayResult(t *testing.T, recorder *httptest.ResponseRecorder, payload map[string]any, log domainaigateway.LLMCallLog) {
	t.Helper()
	if recorder.Code != http.StatusOK || payload["model"] != "gpt-upstream" || payload["custom"] != "preserved" {
		t.Fatalf("unexpected relay response or payload: status=%d payload=%#v", recorder.Code, payload)
	}
	assertNativeRelayBody(t, recorder.Body.String(), "chatcmpl-1")
	if log.PublicModel != "gpt-public" || log.UpstreamID != "upstream-openai" || log.UpstreamModel != "gpt-upstream" {
		t.Fatalf("route log fields: %#v", log)
	}
	if log.Status != "success" || log.HTTPStatus != http.StatusOK || log.ProviderKind != "openai" || log.Endpoint != "chat/completions" {
		t.Fatalf("status log fields: %#v", log)
	}
	if log.PromptTokens != 7 || log.CompletionTokens != 11 || log.TotalTokens != 18 || log.CachedReadTokens != 3 || log.ReasoningTokens != 2 || log.TTFBMilliseconds <= 0 || log.DurationMilliseconds <= 0 || log.EstimatedTokens {
		t.Fatalf("usage log fields: %#v", log)
	}
}

func assertOpenAIToAnthropicResult(t *testing.T, recorder *httptest.ResponseRecorder, payload map[string]any, log domainaigateway.LLMCallLog) {
	t.Helper()
	if recorder.Code != http.StatusOK || payload["model"] != "gpt-upstream" || payload["system"] != "be concise" || jsonNumberInt(payload["max_tokens"]) != 64 {
		t.Fatalf("unexpected transform response: status=%d payload=%#v", recorder.Code, payload)
	}
	messages := mustValueAs[[]any](t, payload["messages"])
	if len(messages) != 1 {
		t.Fatalf("upstream messages: %#v", messages)
	}
	first := mustValueAs[map[string]any](t, messages[0])
	if first["role"] != "user" || first["content"] != "hello" {
		t.Fatalf("upstream first message: %#v", first)
	}
	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode transformed response: %v", err)
	}
	usage := mustValueAs[map[string]any](t, response["usage"])
	if response["object"] != "chat.completion" || jsonNumberInt(usage["prompt_tokens"]) != 8 || jsonNumberInt(usage["completion_tokens"]) != 13 || jsonNumberInt(usage["total_tokens"]) != 21 {
		t.Fatalf("transformed response: %#v", response)
	}
	if log.ProviderKind != "openai" || log.Endpoint != "chat/completions" || log.UpstreamID != "upstream-openai" || log.PromptTokens != 8 || log.CompletionTokens != 13 || log.TotalTokens != 21 {
		t.Fatalf("transform call log: %#v", log)
	}
}

func assertOpenAIAudioSpeechResult(t *testing.T, recorder *httptest.ResponseRecorder, payload map[string]any, log domainaigateway.LLMCallLog, audioBody string) {
	t.Helper()
	if recorder.Code != http.StatusOK || recorder.Header().Get("Content-Type") != "audio/mpeg" || recorder.Body.String() != audioBody {
		t.Fatalf("audio response: status=%d headers=%#v body=%q", recorder.Code, recorder.Header(), recorder.Body.String())
	}
	if payload["model"] != "gpt-upstream" || payload["input"] != "hello from speech" || payload["voice"] != "alloy" || payload["custom"] != "preserved" {
		t.Fatalf("audio upstream payload: %#v", payload)
	}
	if log.Status != "success" || log.ProviderKind != "openai" || log.Endpoint != "audio/speech" || log.Stream {
		t.Fatalf("audio status log: %#v", log)
	}
	if log.PublicModel != "gpt-public" || log.UpstreamID != "upstream-openai" || log.UpstreamModel != "gpt-upstream" || !log.EstimatedTokens || log.PromptTokens <= 0 || log.CompletionTokens != 0 || log.TotalTokens != log.PromptTokens {
		t.Fatalf("audio usage log: %#v", log)
	}
}

type relaySelectorRepositoryStub struct {
	routes    []domainaigateway.LLMModelRoute
	upstreams []domainaigateway.LLMUpstream
}

func (s *relaySelectorRepositoryStub) ListLLMModelRoutes(context.Context, domainaigateway.LLMModelRouteFilter) ([]domainaigateway.LLMModelRoute, error) {
	return append([]domainaigateway.LLMModelRoute(nil), s.routes...), nil
}

func (s *relaySelectorRepositoryStub) ListLLMUpstreams(context.Context, domainaigateway.LLMUpstreamFilter) ([]domainaigateway.LLMUpstream, error) {
	return append([]domainaigateway.LLMUpstream(nil), s.upstreams...), nil
}

func (s *relaySelectorRepositoryStub) GetLLMUpstream(_ context.Context, id string) (domainaigateway.LLMUpstream, error) {
	for _, upstream := range s.upstreams {
		if upstream.ID == id {
			return upstream, nil
		}
	}
	return domainaigateway.LLMUpstream{}, apperrors.ErrNotFound
}

var _ relaySelectionRepository = (*relaySelectorRepositoryStub)(nil)

func TestRelaySelectorUsesFocusedRepositoryContract(t *testing.T) {
	repository := &relaySelectorRepositoryStub{
		routes: []domainaigateway.LLMModelRoute{{
			ID:            "route-1",
			ProviderKind:  "openai",
			PublicModel:   "public-model",
			UpstreamModel: "upstream-model",
			UpstreamID:    "upstream-1",
			Enabled:       true,
		}},
		upstreams: []domainaigateway.LLMUpstream{{
			ID:              "upstream-1",
			ProviderKind:    "openai",
			Status:          "active",
			SupportedModels: []string{"upstream-model"},
		}},
	}
	selector := newRelaySelector(repository)

	selections, err := selector.selectRelayUpstreamCandidates(
		context.Background(),
		"openai",
		"public-model",
	)
	if err != nil {
		t.Fatalf("select candidates: %v", err)
	}
	if len(selections) != 1 || selections[0].upstream.ID != "upstream-1" {
		t.Fatalf("selections = %#v, want upstream-1", selections)
	}
}

type relayCacheRepositoryStub struct {
	entry domainaigateway.LLMCacheEntry
}

func (s *relayCacheRepositoryStub) GetLLMCacheEntryByKey(_ context.Context, key string) (domainaigateway.LLMCacheEntry, error) {
	if s.entry.CacheKey != key {
		return domainaigateway.LLMCacheEntry{}, apperrors.ErrNotFound
	}
	return s.entry, nil
}

func (s *relayCacheRepositoryStub) CreateLLMCacheEntry(_ context.Context, entry domainaigateway.LLMCacheEntry) (domainaigateway.LLMCacheEntry, error) {
	s.entry = entry
	return entry, nil
}

func (s *relayCacheRepositoryStub) UpdateLLMCacheEntry(_ context.Context, entry domainaigateway.LLMCacheEntry) (domainaigateway.LLMCacheEntry, error) {
	s.entry = entry
	return entry, nil
}

var _ relayCacheRepository = (*relayCacheRepositoryStub)(nil)

func TestRelayResponseCacheOwnsEncryptedPersistence(t *testing.T) {
	config := LLMRelayConfig{CredentialEncryptionKey: "relay-cache-component-key-1234567890"}
	repository := &relayCacheRepositoryStub{}
	cache := newRelayResponseCache(repository, newRelayCredentialCodec(config), config)
	request := LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Headers:      http.Header{},
	}
	selection := relaySelection{
		route: domainaigateway.LLMModelRoute{
			ID:            "route-1",
			UpstreamModel: "gpt-upstream",
			CachePolicy:   map[string]any{"enabled": true},
		},
		upstream: domainaigateway.LLMUpstream{ID: "upstream-1"},
	}
	requestBody := []byte(`{"messages":[{"role":"user","content":"hello"}]}`)
	attempt := cache.attempt(
		domainidentity.AccessContext{SubjectType: "user", SubjectID: "user-1", TokenID: "token-1"},
		request,
		selection,
		"gpt-public",
		false,
		requestBody,
		"",
	)
	if !attempt.enabled {
		t.Fatal("cache attempt is disabled")
	}
	responseBody := []byte(`{"id":"response-1","usage":{"total_tokens":3}}`)
	status := cache.store(context.Background(), attempt, &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(string(responseBody))),
	}, responseBody)
	if status != relayCacheWrite {
		t.Fatalf("store status = %q, want %q", status, relayCacheWrite)
	}
	if repository.entry.ResponseBodyCiphertext == string(responseBody) {
		t.Fatal("cache persisted plaintext response body")
	}
	_, body, found := cache.read(context.Background(), attempt, time.Now().UTC())
	if !found || body != string(responseBody) {
		t.Fatalf("cache read found=%v body=%q", found, body)
	}
}

func TestRelayUsageAnalyzerParsesAndEstimates(t *testing.T) {
	tests := []struct {
		name          string
		request       LLMRelayHTTPRequest
		response      string
		wantPrompt    int
		wantComplete  int
		wantTotal     int
		wantEstimated bool
	}{
		{
			name:         "provider usage",
			response:     `{"usage":{"prompt_tokens":7,"completion_tokens":5,"total_tokens":12}}`,
			wantPrompt:   7,
			wantComplete: 5,
			wantTotal:    12,
		},
		{
			name: "fallback estimation",
			request: LLMRelayHTTPRequest{
				Endpoint: "chat/completions",
				Body:     []byte(`{"messages":[{"role":"user","content":"estimate these words"}]}`),
			},
			response:      `{"choices":[{"message":{"content":"estimated output"}}]}`,
			wantEstimated: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			usage, estimated := (relayUsageAnalyzer{}).analyze(
				test.request,
				[]byte(test.response),
				http.StatusOK,
				"success",
			)
			if usage.promptTokens != test.wantPrompt || usage.completionTokens != test.wantComplete || usage.totalTokens != test.wantTotal {
				if !test.wantEstimated {
					t.Fatalf("usage = %#v", usage)
				}
			}
			if estimated != test.wantEstimated {
				t.Fatalf("estimated = %v, want %v", estimated, test.wantEstimated)
			}
			if test.wantEstimated && usage.totalTokens == 0 {
				t.Fatal("estimated usage has no tokens")
			}
		})
	}
}
