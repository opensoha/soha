package aigateway

import (
	"context"
	"encoding/json"
	"errors"
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

type relayHTTPDoerFunc func(*http.Request) (*http.Response, error)

func (f relayHTTPDoerFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

var _ relayHTTPDoer = relayHTTPDoerFunc(nil)

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

func TestRelayHTTPTransportPropagatesParentCancellation(t *testing.T) {
	config := LLMRelayConfig{
		DefaultTimeout:          time.Minute,
		StreamTimeout:           time.Minute,
		CredentialEncryptionKey: "relay-component-test-key-1234567890",
	}
	credentials := newRelayCredentialCodec(config)
	ciphertext, err := credentials.encrypt("upstream-api-key")
	if err != nil {
		t.Fatalf("encrypt API key: %v", err)
	}
	var called bool
	transport := newRelayHTTPTransport(relayHTTPDoerFunc(func(req *http.Request) (*http.Response, error) {
		called = true
		<-req.Context().Done()
		return nil, req.Context().Err()
	}), config, credentials)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := transport.execute(ctx, relayTransportRequest{
		request: LLMRelayHTTPRequest{
			ProviderKind: "openai",
			Endpoint:     "chat/completions",
			Headers:      http.Header{},
		},
		selection: relaySelection{
			route: domainaigateway.LLMModelRoute{UpstreamModel: "gpt-test"},
			upstream: domainaigateway.LLMUpstream{
				BaseURL:          "https://api.example.com",
				ProviderKind:     "openai",
				APIKeyCiphertext: ciphertext,
			},
		},
		body:             []byte(`{"model":"gpt-test"}`),
		upstreamProvider: "openai",
		upstreamEndpoint: "chat/completions",
		writer:           httptest.NewRecorder(),
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !called {
		t.Fatal("transport did not call the HTTP dependency")
	}
	if result.failure == nil || result.failure.status != "client_cancelled" {
		t.Fatalf("failure = %#v, want client_cancelled", result.failure)
	}
	if !errors.Is(result.failure.publicError, apperrors.ErrClusterUnready) {
		t.Fatalf("public error = %v, want cluster unready", result.failure.publicError)
	}
}

func TestNormalizedHTTPClientDoesNotOverrideStreamDeadline(t *testing.T) {
	client := normalizedHTTPClient(nil)
	if client.Timeout != 0 {
		t.Fatalf("default HTTP client timeout = %v, want request context to own deadlines", client.Timeout)
	}
	custom := &http.Client{Timeout: 3 * time.Second}
	if got := normalizedHTTPClient(custom); got != custom {
		t.Fatal("normalizedHTTPClient() replaced caller-provided client")
	}
}

func TestRelayHTTPTransportEnforcesFirstByteTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(150 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	transport, selection := newRelayTimeoutTestTransport(t, server.URL, LLMRelayConfig{
		DefaultTimeout:            time.Second,
		StreamTimeout:             time.Second,
		FirstByteTimeout:          25 * time.Millisecond,
		StreamIdleTimeout:         time.Second,
		AllowPrivateUpstreamHosts: true,
	})
	started := time.Now()
	result, err := transport.execute(context.Background(), relayTransportRequest{
		request:          LLMRelayHTTPRequest{ProviderKind: "openai", Endpoint: "chat/completions", Headers: make(http.Header)},
		selection:        selection,
		body:             []byte(`{"model":"gpt-test"}`),
		upstreamProvider: "openai",
		upstreamEndpoint: "chat/completions",
		stream:           true,
		writer:           httptest.NewRecorder(),
	})
	if err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if result.failure == nil || result.failure.code != "upstream_request_failed" {
		t.Fatalf("failure = %#v, want first-byte request failure", result.failure)
	}
	if elapsed := time.Since(started); elapsed >= 120*time.Millisecond {
		t.Fatalf("first-byte timeout elapsed = %v", elapsed)
	}
}

func TestRelayHTTPTransportEnforcesStreamIdleTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Errorf("flush response headers: %v", err)
			return
		}
		select {
		case <-time.After(150 * time.Millisecond):
			_, _ = w.Write([]byte("data: late\n\n"))
		case <-r.Context().Done():
		}
	}))
	defer server.Close()

	transport, selection := newRelayTimeoutTestTransport(t, server.URL, LLMRelayConfig{
		DefaultTimeout:            time.Second,
		StreamTimeout:             time.Second,
		FirstByteTimeout:          100 * time.Millisecond,
		StreamIdleTimeout:         25 * time.Millisecond,
		AllowPrivateUpstreamHosts: true,
	})
	started := time.Now()
	result, err := transport.execute(context.Background(), relayTransportRequest{
		request:          LLMRelayHTTPRequest{ProviderKind: "openai", Endpoint: "chat/completions", Headers: make(http.Header)},
		selection:        selection,
		body:             []byte(`{"model":"gpt-test"}`),
		upstreamProvider: "openai",
		upstreamEndpoint: "chat/completions",
		stream:           true,
		writer:           httptest.NewRecorder(),
	})
	if err != nil {
		t.Fatalf("execute() error = %v", err)
	}
	if result.failure == nil || result.failure.code != "upstream_read_failed" {
		t.Fatalf("failure = %#v, want stream idle read failure", result.failure)
	}
	if elapsed := time.Since(started); elapsed >= 120*time.Millisecond {
		t.Fatalf("stream idle timeout elapsed = %v", elapsed)
	}
}

func newRelayTimeoutTestTransport(t *testing.T, baseURL string, config LLMRelayConfig) (*relayHTTPTransport, relaySelection) {
	t.Helper()
	config.CredentialEncryptionKey = "relay-timeout-test-key-1234567890"
	config.AllowInsecureUpstreamHTTP = true
	credentials := newRelayCredentialCodec(config)
	ciphertext, err := credentials.encrypt("upstream-api-key")
	if err != nil {
		t.Fatalf("encrypt API key: %v", err)
	}
	return newRelayHTTPTransport(normalizedHTTPClient(nil), config, credentials), relaySelection{
		route: domainaigateway.LLMModelRoute{UpstreamModel: "gpt-test"},
		upstream: domainaigateway.LLMUpstream{
			BaseURL:          baseURL,
			ProviderKind:     "openai",
			APIKeyCiphertext: ciphertext,
		},
	}
}

func TestRelayHTTPTransportRejectsOversizedNonStreamResponse(t *testing.T) {
	body := strings.Repeat("x", relayMaxNonStreamResponseBytes+1)
	transport := &relayHTTPTransport{client: relayHTTPDoerFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}

	result, err := transport.copyResponse(context.Background(), relayTransportRequest{}, relayTransportResult{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
		},
	}, func() {})
	if err != nil {
		t.Fatalf("copyResponse() error = %v", err)
	}
	if result.failure == nil || result.failure.code != "upstream_response_too_large" {
		t.Fatalf("failure = %#v, want upstream_response_too_large", result.failure)
	}
	if len(result.body) != relayMaxNonStreamResponseBytes || !result.bodyTruncated {
		t.Fatalf("buffered=%d truncated=%v", len(result.body), result.bodyTruncated)
	}
}

func TestRelayHTTPTransportBoundsStreamAuditBuffer(t *testing.T) {
	body := strings.Repeat("prefix", relayStreamAuditBufferBytes/6+100) + "usage-tail"
	writer := httptest.NewRecorder()
	transport := &relayHTTPTransport{}
	result, err := transport.copyResponse(context.Background(), relayTransportRequest{
		stream: true,
		writer: writer,
	}, relayTransportResult{
		response: &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
		},
	}, func() {})
	if err != nil {
		t.Fatalf("copyResponse() error = %v", err)
	}
	if got := int(result.outputBytes); got != len(body) {
		t.Fatalf("output bytes = %d, want %d", got, len(body))
	}
	if len(result.body) != relayStreamAuditBufferBytes || !result.bodyTruncated {
		t.Fatalf("buffered=%d truncated=%v", len(result.body), result.bodyTruncated)
	}
	if !strings.HasSuffix(string(result.body), "usage-tail") {
		t.Fatal("bounded audit buffer did not retain response tail")
	}
	if writer.Body.Len() != len(body) {
		t.Fatalf("forwarded bytes = %d, want %d", writer.Body.Len(), len(body))
	}
}

func TestRelayAllowWebSocketOrigin(t *testing.T) {
	tests := []struct {
		name    string
		host    string
		origins []string
		want    bool
	}{
		{name: "missing origin", host: "gateway.example", want: true},
		{name: "same public host and port", host: "gateway.example:8443", origins: []string{"https://gateway.example:8443"}, want: true},
		{name: "different public port", host: "gateway.example:8443", origins: []string{"https://gateway.example:9443"}},
		{name: "local cross port", host: "127.0.0.1:8080", origins: []string{"http://localhost:3000"}, want: true},
		{name: "duplicate origin", host: "gateway.example", origins: []string{"https://gateway.example", "https://evil.example"}},
		{name: "origin path", host: "gateway.example", origins: []string{"https://gateway.example/path"}},
		{name: "unsupported scheme", host: "gateway.example", origins: []string{"file://gateway.example"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "http://"+test.host+"/realtime", nil)
			for _, origin := range test.origins {
				req.Header.Add("Origin", origin)
			}
			if got := relayAllowWebSocketOrigin(req); got != test.want {
				t.Fatalf("relayAllowWebSocketOrigin() = %v, want %v", got, test.want)
			}
		})
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
