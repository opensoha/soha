package aigateway

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	domainaigateway "github.com/opensoha/soha/internal/domain/aigateway"
)

func TestWorkbenchContextWindowUsesActualFallbackReceipt(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "fixture unavailable", http.StatusServiceUnavailable)
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) == "" {
			t.Error("empty model input")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"fixture answer"}}],"usage":{"prompt_tokens":8000,"completion_tokens":200,"total_tokens":8200}}`)
	}))
	defer second.Close()
	repo := relayRepoForTwoUpstreams(t, first.URL, second.URL, "fixture-key")
	repo.routes[1].Priority = 2
	repo.routes[0].Metadata = map[string]any{"contextWindowTokens": 32000}
	repo.routes[1].Metadata = map[string]any{"contextWindowTokens": 128000}
	service := newRelayRuntimeTestService(repo)
	request := WorkbenchRelayRequest{PublicModel: "gpt-public", Messages: []WorkbenchRelayMessage{{Role: "user", Content: "fixture question"}}}
	response, err := service.InvokeWorkbenchModel(t.Context(), relayWorkbenchTestPrincipal(), request)
	if err != nil {
		t.Fatal(err)
	}
	if response.RouteID != "route-second" || response.ContextWindowTokens != 128000 || response.Usage["inputTokens"] != 8000 || response.UsageEstimated {
		t.Fatalf("used initial route instead of actual fallback: %+v", response)
	}
}

func TestWorkbenchPrivateCacheSeparatesExplicitRoutes(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"choices":[{"message":{"content":"answer %d"}}]}`, calls.Add(1))
	}))
	defer upstream.Close()
	repo := relayRepoForUpstream(t, upstream.URL, "openai", "fixture-key")
	repo.routes[0].CachePolicy = map[string]any{"enabled": true, "scope": "private", "version": "same-version", "ttlSeconds": 60}
	repo.routes[0].Metadata = map[string]any{"contextWindowTokens": 32000}
	second := repo.routes[0]
	second.ID = "route-2"
	second.Metadata = map[string]any{"contextWindowTokens": 128000}
	repo.routes = append(repo.routes, second)
	service := newRelayRuntimeTestService(repo)
	request := WorkbenchRelayRequest{
		PublicModel: "gpt-public", Messages: []WorkbenchRelayMessage{{Role: "user", Content: "same prompt"}},
	}
	for _, test := range []struct {
		route, content, cacheStatus string
		window                      int64
	}{
		{"route-1", "answer 1", relayCacheWrite, 32000},
		{"route-2", "answer 2", relayCacheWrite, 128000},
		{"route-1", "answer 1", relayCacheHit, 32000},
		{"route-2", "answer 2", relayCacheHit, 128000},
	} {
		request.RouteID = test.route
		response, err := service.InvokeWorkbenchModel(t.Context(), relayWorkbenchTestPrincipal(), request)
		if err != nil {
			t.Fatal(err)
		}
		if response.RouteID != test.route || response.UpstreamID != "upstream-openai" ||
			response.ContextWindowTokens != test.window || response.Content != test.content {
			t.Fatalf("incorrect response for route %s: %+v", test.route, response)
		}
		logs := repo.logs()
		log := logs[len(logs)-1]
		if log.RouteTrace["routeId"] != test.route || log.CacheStatus != test.cacheStatus {
			t.Fatalf("incorrect cache log for route %s: %+v", test.route, log)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("expected one upstream call per route, got %d", calls.Load())
	}
}

func TestWorkbenchContextWindowStreamAndCatalog(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"ok\"}}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":8,\"completion_tokens\":2,\"total_tokens\":10}}\n\ndata: [DONE]\n\n")
	}))
	defer upstream.Close()
	repo := relayRepoForUpstream(t, upstream.URL, "openai", "fixture-key")
	repo.routes[0].Metadata = map[string]any{"contextWindowTokens": float64(128000)}
	service := newRelayRuntimeTestService(repo)
	options, err := service.ListWorkbenchModels(context.Background(), relayWorkbenchTestPrincipal(), "chat/completions")
	if err != nil || len(options) != 1 || options[0].ContextWindowTokens != 128000 {
		t.Fatalf("catalog: %+v %v", options, err)
	}
	response, err := service.InvokeWorkbenchModelStream(t.Context(), relayWorkbenchTestPrincipal(), WorkbenchRelayRequest{PublicModel: "gpt-public", Messages: []WorkbenchRelayMessage{{Role: "user", Content: "hi"}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if response.ContextWindowTokens != 128000 || response.Usage["inputTokens"] != 8 || response.UsageEstimated {
		t.Fatalf("stream usage: %+v", response)
	}
}

func TestWorkbenchContextWindowRejectsUnknownAndMalformedCapacity(t *testing.T) {
	for _, value := range []any{nil, "128000", -1, 0, 0.5, math.NaN(), math.Inf(1), float64(9007199254740992)} {
		if workbenchContextWindow(map[string]any{"contextWindowTokens": value}) != 0 {
			t.Fatalf("invalid capacity accepted: %v", value)
		}
	}
	selections := []relaySelection{
		{route: domainaigateway.LLMModelRoute{Metadata: map[string]any{"contextWindowTokens": 128000}}},
		{route: domainaigateway.LLMModelRoute{Metadata: map[string]any{"contextWindowTokens": 64000}}},
	}
	if workbenchCatalogContextWindow(selections) != 64000 {
		t.Fatal("catalog must use the smallest eligible capacity")
	}
	unknownFallback := []relaySelection{{route: domainaigateway.LLMModelRoute{Metadata: map[string]any{"contextWindowTokens": 128000}}}, {}}
	if workbenchCatalogContextWindow(unknownFallback) != 0 {
		t.Fatal("catalog must not hide an unknown fallback capacity")
	}
}
