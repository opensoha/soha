package aigateway

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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
	for _, test := range []struct{ name, ending string }{
		{"clean_eof", ""},
		{"token_limit", "data: {\"choices\":[{\"finish_reason\":\"length\"}]}\n\ndata: [DONE]\n\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial answer\"}}]}\n\n"+test.ending)
			}))
			defer upstream.Close()
			repo := relayRepoForUpstream(t, upstream.URL, "openai", "sk-fixture")
			service := newRelayRuntimeTestService(repo, upstream.Client())
			var partial string
			response, err := service.InvokeWorkbenchModelStream(t.Context(), relayWorkbenchTestPrincipal(), WorkbenchRelayRequest{
				PublicModel: "gpt-public", AgentRunID: "owned-native-run", Messages: []WorkbenchRelayMessage{{Role: "user", Content: "synthetic"}},
			}, func(delta WorkbenchRelayStreamDelta) bool { partial += delta.ContentDelta; return true })
			if !errors.Is(err, apperrors.ErrClusterUnready) || response.Content != "" || partial != "partial answer" {
				t.Fatalf("incomplete response became successful: response=%q partial=%q err=%v", response.Content, partial, err)
			}
		})
	}
}
