package sohacli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

func aiTestStreamEvent(sequence int, fields string) string {
	return fmt.Sprintf(`{"id":"event-%d","sessionId":"s:1","sequence":%d,"createdAt":"2026-10-25T00:00:00Z",%s}`, sequence, sequence, fields)
}

func aiTestSSE(events ...string) string {
	return "data: " + strings.Join(events, "\n\ndata: ") + "\n\n"
}

func TestAIChatStreamSubmissionAndTerminalReceipts(t *testing.T) {
	cases := []struct {
		name, stream, status string
		code                 int
	}{
		{"queued-native", aiTestSSE(aiTestStreamEvent(1, `"type":"agent.status","runId":"run:1","status":"queued"`)), "accepted", 0},
		{"queued-external", aiTestSSE(aiTestStreamEvent(1, `"type":"agent.status","runId":"run:1","status":"queued"`), aiTestStreamEvent(2, `"type":"message.done","role":"assistant","content":"Queued"`)), "accepted", 0},
		{"sync-success", aiTestSSE(aiTestStreamEvent(1, `"type":"agent.status","status":"running"`), aiTestStreamEvent(2, `"type":"message.delta","delta":"profile-token"`), aiTestStreamEvent(3, `"type":"agent.status","status":"succeeded"`)), "succeeded", 0},
		{"native-system-failed", aiTestSSE(aiTestStreamEvent(1, `"type":"message.done","role":"system","content":"failed"`), aiTestStreamEvent(2, `"type":"agent.status","runId":"run:1","status":"failed"`)), "failed", 1},
		{"cancelled", aiTestSSE(aiTestStreamEvent(1, `"type":"agent.status","status":"cancelled"`)), "cancelled", 1},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != "POST" || r.URL.RequestURI() != copilotPath+"/sessions/s:1/messages/stream" || r.Header.Get("Authorization") != "Bearer profile-token" || r.Header.Get("Accept") != "text/event-stream" || r.Header.Get("Content-Type") != "application/json" {
					t.Error("incorrect stream method, path or headers")
				}
				var body map[string]any
				if json.NewDecoder(r.Body).Decode(&body) != nil || body["content"] != "hello" || body["requestId"] != "turn/验?" || body["agentProviderId"] != "internal" {
					t.Error("typed submission changed")
				}
				w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
				_, _ = fmt.Fprint(w, ": keepalive\r\n\r\n"+test.stream)
			}))
			defer server.Close()
			var out, stderr bytes.Buffer
			code := Run(t.Context(), []string{"ai", "chat", "messages", "stream", "s:1", "--input", "-", "--request-id", "turn/验?", "--yes", "--profile", "dev"}, Runtime{In: strings.NewReader(`{"content":"hello","agentProviderId":"internal"}`), Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
			if code != test.code || calls != 1 {
				t.Fatalf("code=%d calls=%d stderr=%s", code, calls, stderr.String())
			}
			var last map[string]any
			for _, line := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte{'\n'}) {
				if err := json.Unmarshal(line, &last); err != nil {
					t.Fatal(err)
				}
				if last["source"] != "submission" {
					t.Fatal("submission and durable cursors were conflated")
				}
			}
			receipt, ok := last["receipt"].(map[string]any)
			if !ok || receipt["status"] != test.status || receipt["sessionId"] != "s:1" || receipt["requestId"] != "turn/验?" {
				t.Fatalf("incorrect receipt: %s", out.String())
			}
			if strings.Contains(out.String()+stderr.String(), "profile-token") {
				t.Fatal("bearer credential leaked")
			}
		})
	}
}

func TestAIChatStreamRefusesUnknownOutcomeWithoutReplaying(t *testing.T) {
	queued := aiTestStreamEvent(1, `"type":"agent.status","runId":"run:1","status":"queued"`)
	cases := []struct{ name, body, media string }{
		{"empty", "", "text/event-stream"},
		{"message-only", aiTestSSE(aiTestStreamEvent(1, `"type":"message.done","content":"not a terminal receipt"`)), "text/event-stream"},
		{"running-without-run", aiTestSSE(aiTestStreamEvent(1, `"type":"agent.status","status":"running"`)), "text/event-stream"},
		{"unterminated", "data: " + queued, "text/event-stream"},
		{"partial-after-accepted", aiTestSSE(queued) + "data: {", "text/event-stream"},
		{"duplicate-sequence", aiTestSSE(queued, queued), "text/event-stream"},
		{"wrong-session", aiTestSSE(strings.ReplaceAll(queued, "s:1", "other")), "text/event-stream"},
		{"missing-time", aiTestSSE(`{"id":"e","sessionId":"s:1","sequence":1,"type":"agent.status","status":"succeeded"}`), "text/event-stream"},
		{"unknown-status", aiTestSSE(aiTestStreamEvent(1, `"type":"agent.status","status":"possibly-done"`)), "text/event-stream"},
		{"invalid-utf8", "data: {\"type\":\"" + string([]byte{0xff}) + "\"}\n\n", "text/event-stream"},
		{"oversize-frame", "data: " + strings.Repeat("x", aiStreamFrameLimit+1) + "\n\n", "text/event-stream"},
		{"wrong-media", "profile-token", "application/json"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				w.Header().Set("Content-Type", test.media)
				_, _ = fmt.Fprint(w, test.body)
			}))
			defer server.Close()
			var out, stderr bytes.Buffer
			code := Run(t.Context(), []string{"ai", "chat", "messages", "stream", "s:1", "--input-json", `{"content":"hello","requestId":"turn-1"}`, "--yes"}, Runtime{Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
			if code != 1 || calls != 1 || !strings.Contains(stderr.String(), "outcome unknown") || strings.Contains(out.String(), `"receipt"`) || strings.Contains(stderr.String(), "profile-token") {
				t.Fatalf("code=%d calls=%d stdout=%s stderr=%s", code, calls, out.String(), stderr.String())
			}
		})
	}
}

func TestAIChatStreamRejectsInvalidInputBeforeHTTP(t *testing.T) {
	cases := [][]string{
		{"s:1", "--input-json", `{"content":"hello"}`},
		{"s:1", "--input-json", `{"content":"hello","requestId":"a"}`, "--request-id", "b"},
		{"s:1", "--input-json", `{"content":" ","requestId":"a"}`},
		{"s:1", "--input-json", `{"content":"hello","requestId":"` + strings.Repeat("验", 129) + `"}`},
		{"s:1", "--input-json", `{"content":"hello","requestId":"a","unknown":true}`},
		{"../other", "--input-json", `{"content":"hello","requestId":"a"}`},
		{"s:1", "--input-json", `{"content":"hello","requestId":"a"}`, "--output", "json"},
		{"s:1", "--input-json", `{"content":"hello","requestId":"a"}`, "--follow"},
		{"s:1", "--input-json", `{"content":"hello","requestId":"a"}`, "--stream-timeout", "0s"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var out, stderr bytes.Buffer
			code := Run(t.Context(), append([]string{"ai", "chat", "messages", "stream"}, args...), Runtime{Out: &out, Err: &stderr, ConfigPath: t.TempDir() + "/absent"})
			want := 2
			if strings.Contains(strings.Join(args, " "), `"unknown":true`) {
				want = 1 // Existing typed-input decoder errors retain their command-wide exit status.
			}
			if code != want || out.Len() != 0 {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, out.String(), stderr.String())
			}
		})
	}
}

func TestAIChatStreamTimeoutPolicyKeepsExplicitLimits(t *testing.T) {
	for _, test := range []struct {
		client APIClient
		want   time.Duration
	}{
		{APIClient{}, 0},
		{APIClient{Timeout: time.Second}, time.Second},
		{APIClient{Client: &http.Client{Timeout: 2 * time.Second}}, 2 * time.Second},
		{APIClient{Timeout: 3 * time.Second, Client: &http.Client{Timeout: 2 * time.Second}}, 3 * time.Second},
	} {
		if got := test.client.aiChatHTTPClient().Timeout; got != test.want {
			t.Fatalf("stream timeout=%v want=%v", got, test.want)
		}
	}
	if (APIClient{}).httpClient().Timeout != defaultHTTPTimeout {
		t.Fatal("ordinary API timeout changed")
	}
}

func TestAIChatStreamRefreshesOnlyExplicitPreHandlerRejection(t *testing.T) {
	for _, rejected := range []bool{false, true} {
		t.Run(fmt.Sprint(rejected), func(t *testing.T) {
			calls, refreshes := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Header.Get("Authorization") == "Bearer old-access" {
					if rejected {
						w.Header().Set("WWW-Authenticate", `Bearer error="invalid_token"`)
					}
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = fmt.Fprint(w, `{"message":"old-access refresh-secret"}`)
					return
				}
				if r.Header.Get("Authorization") != "Bearer new-access" {
					t.Error("refresh bearer not used")
				}
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, aiTestSSE(aiTestStreamEvent(1, `"type":"agent.status","status":"succeeded"`)))
			}))
			defer server.Close()
			client := APIClient{ServerURL: server.URL, Token: "old-access", RefreshToken: "refresh-secret", refreshSession: func(context.Context, string) (refreshResponse, error) {
				refreshes++
				var result refreshResponse
				result.Data.Tokens.AccessToken = "new-access"
				return result, nil
			}}
			var out bytes.Buffer
			var input = aiStreamTestInput(t)
			err := client.streamAIChat(t.Context(), "s:1", input, &out)
			if rejected && (err != nil || calls != 2 || refreshes != 1) || !rejected && (err == nil || calls != 1 || refreshes != 0) {
				t.Fatalf("calls=%d refreshes=%d err=%v", calls, refreshes, err)
			}
			if err != nil && strings.Contains(err.Error(), "refresh-secret") {
				t.Fatal("refresh secret leaked")
			}
		})
	}
}

func aiStreamTestInput(t *testing.T) (input sohaapi.WorkbenchSendMessageStreamRequest) {
	t.Helper()
	if err := json.Unmarshal([]byte(`{"content":"hello","requestId":"turn-1"}`), &input); err != nil {
		t.Fatal(err)
	}
	return input
}

func TestAIChatStreamDoesNotFollowRedirectOrCancelDurableRunOnDetach(t *testing.T) {
	t.Run("redirect", func(t *testing.T) {
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			http.Redirect(w, r, "/replay", http.StatusTemporaryRedirect)
		}))
		defer server.Close()
		var out bytes.Buffer
		err := (APIClient{ServerURL: server.URL}).streamAIChat(t.Context(), "s:1", aiStreamTestInput(t), &out)
		if err == nil || calls != 1 || out.Len() != 0 {
			t.Fatalf("calls=%d err=%v", calls, err)
		}
	})
	t.Run("detach", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		calls := 0
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			calls++
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Error("fixture writer does not support flushing")
				return
			}
			flusher.Flush()
			cancel()
			<-r.Context().Done()
		}))
		defer server.Close()
		var out, stderr bytes.Buffer
		code := Run(t.Context(), []string{"ai", "chat", "messages", "stream", "s:1", "--input-json", `{"content":"hello","requestId":"a"}`, "--yes"}, Runtime{Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL), StreamingContext: func(context.Context) (context.Context, context.CancelFunc) { return ctx, cancel }})
		if code != 130 || calls != 1 || out.Len() != 0 {
			t.Fatalf("code=%d calls=%d stderr=%s", code, calls, stderr.String())
		}
	})
}

func TestAIEventStreamMultilineAndTotalBound(t *testing.T) {
	frames := 0
	err := readAIEventStream(strings.NewReader(": comment\r\nid: ignored\r\ndata: {\r\ndata: \"ok\": true}\r\n\r\n"), func(raw []byte) (bool, error) {
		frames++
		if !json.Valid(raw) {
			t.Error("SSE data lines were not joined")
		}
		return false, nil
	})
	if err != nil || frames != 1 {
		t.Fatalf("frames=%d err=%v", frames, err)
	}
	err = readAIEventStream(io.MultiReader(strings.NewReader(strings.Repeat(": bounded comment\n", aiStreamTotalLimit/18+2))), func([]byte) (bool, error) { return false, nil })
	if err == nil {
		t.Fatal("unbounded comment stream accepted")
	}
}
