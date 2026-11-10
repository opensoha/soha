package aigateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/opensoha/soha/internal/platform/apperrors"
)

func TestWorkbenchStreamCompletion(t *testing.T) {
	for _, test := range []struct {
		name, endpoint, body string
		wantError            bool
	}{
		{"openai_eof", "chat/completions", "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\n", true},
		{"openai_done", "chat/completions", "data: [DONE]\n\n", false},
		{"openai_stop", "chat/completions", "data: {\"choices\":[{\"finish_reason\":\"stop\"}]}\n\n", false},
		{"openai_tool_calls", "chat/completions", "data: {\"choices\":[{\"finish_reason\":\"tool_calls\"}]}\n\n", false},
		{"openai_length_done", "chat/completions", "data: {\"choices\":[{\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n", true},
		{"openai_filter", "chat/completions", "data: {\"choices\":[{\"finish_reason\":\"content_filter\"}]}\n\n", true},
		{"responses_eof", "responses", "data: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n", true},
		{"responses_completed", "responses", "data: {\"type\":\"response.completed\"}\n\n", false},
		{"responses_incomplete", "responses", "data: {\"type\":\"response.incomplete\"}\n\n", true},
		{"responses_failed", "responses", "data: {\"type\":\"response.failed\"}\n\n", true},
		{"anthropic_eof", "messages", "data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"partial\"}}\n\n", true},
		{"anthropic_stop", "messages", "data: {\"type\":\"message_stop\"}\n\n", false},
		{"anthropic_tool_use", "messages", "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"}}\n\n", false},
		{"anthropic_max_tokens", "messages", "data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"max_tokens\"}}\n\ndata: {\"type\":\"message_stop\"}\n\n", true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := workbenchRelayStreamOutput(test.endpoint, []byte(test.body))
			if (err != nil) != test.wantError || (err != nil && !errors.Is(err, apperrors.ErrClusterUnready)) {
				t.Fatalf("completion error = %v, wantError %v", err, test.wantError)
			}
		})
	}
}

func TestInvokeWorkbenchRejectsIncompleteStream(t *testing.T) {
	const delta = "data: {\"choices\":[{\"delta\":{\"content\":\"fixture answer\"}}]}\n\n"
	const done = "data: [DONE]\n\n"
	for _, test := range []struct {
		name, ending, status, code string
		wantErr                    error
		wantFailures               int
	}{
		{"clean_eof", "", "failure", "upstream_incomplete_stream", apperrors.ErrClusterUnready, 2},
		{"token_limit", "data: {\"choices\":[{\"finish_reason\":\"length\"}]}\n\n" + done, "failure", "upstream_incomplete_stream", apperrors.ErrClusterUnready, 2},
		{"completed", done, "success", "", nil, 0},
		{"callback_stopped", done, "client_cancelled", "client_cancelled", ErrWorkbenchRelayStreamStopped, 1},
		{"context_cancelled", done, "client_cancelled", "client_cancelled", context.Canceled, 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, delta+test.ending)
			}))
			defer upstream.Close()
			repo := relayRepoForTwoUpstreams(t, upstream.URL, upstream.URL, "sk-fixture")
			repo.routes[1].Priority = 2
			const previousFailure = "fixture_previous_failure"
			repo.upstreams[0].Health = map[string]any{"consecutiveFailures": 1, "lastErrorCode": previousFailure}
			service := newRelayRuntimeTestService(repo)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var partial string
			response, err := service.InvokeWorkbenchModelStream(ctx, relayWorkbenchTestPrincipal(), WorkbenchRelayRequest{
				PublicModel: "gpt-public", AgentRunID: "owned-native-run", Messages: []WorkbenchRelayMessage{{Role: "user", Content: "synthetic"}},
			}, func(delta WorkbenchRelayStreamDelta) bool {
				partial += delta.ContentDelta
				if test.name == "context_cancelled" {
					cancel()
				}
				return test.name != "callback_stopped"
			})
			if !errors.Is(err, test.wantErr) || partial != "fixture answer" || calls.Load() != 1 {
				t.Fatalf("response=%+v partial=%q err=%v, want error %v and one attempt; attempts=%d", response, partial, err, test.wantErr, calls.Load())
			}
			if test.wantErr == nil {
				if response.Content != partial || response.RouteID != "route-first" || response.UpstreamID != "upstream-first" {
					t.Fatalf("incorrect completed response: %+v", response)
				}
			} else if response.RouteID != "" || response.Content != "" {
				t.Fatalf("failed response carried a success receipt: %+v", response)
			}
			log := singleRelayLog(t, repo)
			if log.Status != test.status || log.ErrorCode != test.code || log.RouteTrace["routeId"] != "route-first" ||
				log.HTTPStatus != http.StatusOK || log.OutputBytes == 0 {
				t.Fatalf("incorrect call log: %+v", log)
			}
			updated, err := repo.GetLLMUpstream(t.Context(), "upstream-first")
			if err != nil {
				t.Fatal(err)
			}
			health := updated.Health
			wantLastError := any(test.code)
			switch test.status {
			case "success":
				wantLastError = nil
			case "client_cancelled":
				wantLastError = previousFailure
			}
			if health["lastErrorCode"] != wantLastError || intFromAny(health["consecutiveFailures"]) != test.wantFailures ||
				(health["lastSuccessAt"] != nil) != (test.status == "success") {
				t.Fatalf("incorrect upstream health: %+v", health)
			}
		})
	}
}

func TestRelayLLMHTTPPreservesRawStreamWithCleanEOF(t *testing.T) {
	const body = "data: {\"choices\":[{\"delta\":{\"content\":\"fixture answer\"}}]}\n\n"
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, body)
	}))
	defer upstream.Close()
	repo := relayRepoForUpstream(t, upstream.URL, "openai", "fixture-key")
	service := newRelayRuntimeTestService(repo)
	recorder := httptest.NewRecorder()
	err := service.RelayLLMHTTP(t.Context(), relayTestPrincipal(), relayTestAccessContext(nil), LLMRelayHTTPRequest{
		ProviderKind: "openai",
		Endpoint:     "chat/completions",
		Method:       http.MethodPost,
		Headers:      http.Header{"Accept": []string{"text/event-stream"}},
		Body:         []byte(`{"model":"gpt-public","stream":true,"messages":[{"role":"user","content":"hi"}]}`),
	}, recorder)
	if err != nil || recorder.Code != http.StatusOK || recorder.Body.String() != body {
		t.Fatalf("raw stream changed: status=%d body=%q err=%v", recorder.Code, recorder.Body.String(), err)
	}
}
