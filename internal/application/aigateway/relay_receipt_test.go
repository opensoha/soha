package aigateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"

	appaccess "github.com/opensoha/soha/internal/application/access"
)

func TestKnowledgeModelUsesActualFallbackReceipt(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "fixture unavailable", http.StatusServiceUnavailable)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"embedding":[0.1,0.2]}],"usage":{"prompt_tokens":2,"total_tokens":2}}`)
	}))
	defer second.Close()
	repo := relayRepoForTwoUpstreams(t, first.URL, second.URL, "fixture-key")
	repo.routes[1].Priority = 2
	service := newRelayRuntimeTestService(repo)
	principal := testPrincipal("developer")
	principal.PermissionKeys = []string{appaccess.PermAIGatewayRelayInvoke}
	// Knowledge does not request HTTP route trace headers.
	response, err := service.InvokeKnowledgeModel(t.Context(), principal, KnowledgeModelRequest{
		PublicModel: "gpt-public", Endpoint: "embeddings", Payload: map[string]any{"input": "fixture"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.RouteID != "route-second" || response.UpstreamID != "upstream-second" || response.ProviderKind != "openai" {
		t.Fatalf("incorrect fallback receipt: %+v", response)
	}
	logs := repo.logs()
	if len(logs) != 2 || logs[1].RouteTrace["routeId"] != response.RouteID || logs[1].UpstreamID != response.UpstreamID {
		t.Fatalf("receipt disagrees with call attribution: %+v", logs)
	}
}

func TestWorkbenchCachedFallbackRetainsActualReceipt(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "fixture unavailable", http.StatusServiceUnavailable)
	}))
	defer first.Close()
	var calls atomic.Int32
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"fixture answer"}}],"usage":{"prompt_tokens":8,"completion_tokens":2,"total_tokens":10}}`)
	}))
	defer second.Close()
	repo := relayRepoForTwoUpstreams(t, first.URL, second.URL, "fixture-key")
	repo.routes[1].Priority = 2
	repo.routes[1].CachePolicy = map[string]any{"enabled": true, "ttlSeconds": 60}
	repo.routes[1].Metadata = map[string]any{"contextWindowTokens": 128000}
	service := newRelayRuntimeTestService(repo)
	for range 2 {
		response, err := service.InvokeWorkbenchModel(t.Context(), relayWorkbenchTestPrincipal(), WorkbenchRelayRequest{
			PublicModel: "gpt-public", Messages: []WorkbenchRelayMessage{{Role: "user", Content: "fixture"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if response.RouteID != "route-second" || response.UpstreamID != "upstream-second" || response.ContextWindowTokens != 128000 {
			t.Fatalf("incorrect cached fallback receipt: %+v", response)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("expected one upstream call and one cache hit, got %d upstream calls", calls.Load())
	}
}

func TestWorkbenchPartialStreamDoesNotReportSuccess(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		body := "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n"
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Content-Length", strconv.Itoa(len(body)+64))
		_, _ = io.WriteString(w, body)
	}))
	defer upstream.Close()
	repo := relayRepoForUpstream(t, upstream.URL, "openai", "fixture-key")
	service := newRelayRuntimeTestService(repo)
	_, err := service.InvokeWorkbenchModelStream(t.Context(), relayWorkbenchTestPrincipal(), WorkbenchRelayRequest{
		PublicModel: "gpt-public", Messages: []WorkbenchRelayMessage{{Role: "user", Content: "fixture"}},
	}, nil)
	if err == nil {
		t.Fatal("partial stream was reported as a successful model result")
	}
}
