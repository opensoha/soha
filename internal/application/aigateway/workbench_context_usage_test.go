package aigateway

import (
	"context"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
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
	repo.routes[0].Metadata = map[string]any{"contextWindowTokens": 32000}
	repo.routes[1].Metadata = map[string]any{"contextWindowTokens": 128000}
	service := newRelayRuntimeTestService(repo, first.Client())
	request := WorkbenchRelayRequest{PublicModel: "gpt-public", Messages: []WorkbenchRelayMessage{{Role: "user", Content: "fixture question"}}}
	response, err := service.InvokeWorkbenchModel(t.Context(), relayWorkbenchTestPrincipal(), request)
	if err != nil {
		t.Fatal(err)
	}
	if response.RouteID != "route-second" || response.ContextWindowTokens != 128000 || response.Usage["inputTokens"] != 8000 || response.UsageEstimated {
		t.Fatalf("used initial route instead of actual fallback: %+v", response)
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
	service := newRelayRuntimeTestService(repo, upstream.Client())
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
