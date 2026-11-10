package sohacli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestAIWorkbenchUsesPublishedPathsAndTypedInputs(t *testing.T) {
	cases := []struct {
		args         []string
		method, path string
		input, reply string
	}{
		{[]string{"chat", "sessions", "list"}, "GET", "/sessions", "", `{"items":[]}`},
		{[]string{"chat", "sessions", "main", "--yes"}, "POST", "/sessions/main", "{}", `{"data":{"id":"main-1"}}`},
		{[]string{"chat", "sessions", "main", "--input", "-", "--yes"}, "POST", "/sessions/main", `{"agentProviderId":"internal"}`, `{"data":{"id":"main-1"}}`},
		{[]string{"chat", "sessions", "create", "--input", "-", "--yes"}, "POST", "/sessions", `{"title":"side","mode":"general"}`, `{"data":{"id":"side-1"}}`},
		{[]string{"chat", "sessions", "get", "session:1"}, "GET", "/sessions/session:1", "", `{"data":{"id":"session:1"}}`},
		{[]string{"chat", "messages", "list", "session:1"}, "GET", "/sessions/session:1/messages", "", `{"items":[]}`},
		{[]string{"chat", "messages", "window", "session:1", "--before", "message:2", "--limit", "20"}, "GET", "/sessions/session:1/messages/window?before=message%3A2&limit=20", "", `{"data":{"messages":[],"beforeCursor":"message:1","afterCursor":"message:2"}}`},
		{[]string{"chat", "request", "session:1", "--request-id", "turn:1"}, "GET", "/sessions/session:1/native-request?requestId=turn%3A1", "", `{"data":{"id":"run:1","status":"running","output":{"executionReadOnly":true,"stopReason":"legacy_reconciliation_required"}}}`},
		{[]string{"chat", "native-request", "session:1", "--request-id", "turn:1"}, "GET", "/sessions/session:1/native-request?requestId=turn%3A1", "", `{"data":{"id":"run:1","status":"running","output":{"executionReadOnly":true,"stopReason":"legacy_reconciliation_required"}}}`},
		{[]string{"chat", "native-request", "session:1", "--request-id", "turn/1?x=%"}, "GET", "/sessions/session:1/native-request?requestId=turn%2F1%3Fx%3D%25", "", `{"data":{"id":"run:1","status":"running"}}`},
		{[]string{"chat", "native-request", "session:1", "--request-id", strings.Repeat("验", 128)}, "GET", "/sessions/session:1/native-request?requestId=" + strings.Repeat("%E9%AA%8C", 128), "", `{"data":{"id":"run:1","status":"running"}}`},
		{[]string{"assistant", "access", "get"}, "GET", "/assistant/access", "", `{"data":{"status":"not_requested"}}`},
		{[]string{"assistant", "access", "request", "--yes"}, "POST", "/assistant/access", "", `{"data":{"status":"pending"}}`},
		{[]string{"assistant", "ensure", "--yes"}, "POST", "/assistant", "", `{"data":{"id":"assistant-1","name":"Soha","mainSessionId":"personal-main"}}`},
		{[]string{"assistant", "sessions", "list"}, "GET", "/assistant/sessions", "", `{"items":[]}`},
		{[]string{"assistant", "sessions", "create", "--input", "-", "--yes"}, "POST", "/assistant/sessions", `{"title":"personal side"}`, `{"data":{"id":"personal-side"}}`},
		{[]string{"assistant", "tasks", "list"}, "GET", "/assistant/tasks", "", `{"items":[]}`},
		{[]string{"assistant", "artifacts", "list"}, "GET", "/assistant/artifacts", "", `{"data":[]}`},
		{[]string{"runs", "list", "--session-id", "session:1"}, "GET", "/agent-runs?sessionId=session%3A1", "", `{"items":[]}`},
		{[]string{"runs", "list"}, "GET", "/agent-runs", "", `{"items":[]}`},
		{[]string{"runs", "events", "run:1", "--after-sequence", "15"}, "GET", "/agent-runs/run:1/events?afterSequence=15", "", `{"data":{"items":[],"hasMore":false,"nextSequence":15,"resyncRequired":false}}`},
		{[]string{"runs", "cancel", "run:1", "--yes"}, "POST", "/agent-runs/run:1/cancel", "", `{"data":{"id":"run:1","status":"running","phase":"cancel_requested"}}`},
		{[]string{"runs", "resume", "run:1", "--input", "-", "--yes"}, "POST", "/agent-runs/run:1/resume", `{"input":"reviewed reply"}`, `{"data":{"id":"run:1","status":"queued"}}`},
	}
	for _, test := range cases {
		t.Run(strings.Join(test.args, " "), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != test.method || r.URL.RequestURI() != copilotPath+test.path || r.Header.Get("Authorization") != "Bearer profile-token" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.RequestURI())
				}
				if r.Header.Get("X-Soha-AI-Client-ID") != "" || r.Header.Get("X-Soha-Skill-ID") != "" {
					t.Error("native ordinary chat injected AI governance identity headers")
				}
				raw, _ := io.ReadAll(r.Body)
				var got, want any
				if len(raw) > 0 {
					if err := json.Unmarshal(raw, &got); err != nil {
						t.Error(err)
					}
				}
				if test.input != "" {
					if err := json.Unmarshal([]byte(test.input), &want); err != nil {
						t.Fatal(err)
					}
				}
				if !reflect.DeepEqual(got, want) {
					t.Errorf("body = %#v, want %#v", got, want)
				}
				w.Header().Set("Content-Type", "application/json")
				if test.args[0] == "runs" && test.args[1] == "cancel" {
					w.WriteHeader(http.StatusAccepted)
				}
				_, _ = fmt.Fprint(w, test.reply)
			}))
			defer server.Close()
			var out, errOut bytes.Buffer
			args := append([]string{"ai"}, test.args...)
			args = append(args, "--profile", "dev")
			code := Run(t.Context(), args, Runtime{In: strings.NewReader(test.input), Out: &out, Err: &errOut, ConfigPath: writeTestConfig(t, server.URL)})
			if code != 0 || calls != 1 || out.Len() == 0 {
				t.Fatalf("code=%d calls=%d out=%s stderr=%s", code, calls, out.String(), errOut.String())
			}
			var actual, expected any
			if err := json.Unmarshal(out.Bytes(), &actual); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal([]byte(test.reply), &expected); err != nil {
				t.Fatal(err)
			}
			assertAIResponseFields(t, actual, expected)
			if test.args[0] == "runs" && test.args[1] == "cancel" && (!strings.Contains(out.String(), `"status": "running"`) || strings.Contains(out.String(), `"status": "canceled"`)) {
				t.Fatal("accepted cancellation was misrepresented as terminal")
			}
		})
	}
}

func assertAIResponseFields(t *testing.T, actual, expected any) {
	t.Helper()
	if expectedObject, ok := expected.(map[string]any); ok {
		actualObject, ok := actual.(map[string]any)
		if !ok {
			t.Fatalf("response object was lost: %#v", expected)
			return
		}
		for key, value := range expectedObject {
			child, exists := actualObject[key]
			if !exists {
				t.Fatalf("response field %q was dropped", key)
			}
			assertAIResponseFields(t, child, value)
		}
		return
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("response value changed: got %#v want %#v", actual, expected)
	}
}

func TestAIWorkbenchRejectsInvalidTargetsAndIgnoredFlagsBeforeHTTP(t *testing.T) {
	cases := [][]string{
		{"chat", "sessions", "list", "unexpected"},
		{"chat", "sessions", "get", "../escape"},
		{"chat", "sessions", "get"},
		{"chat", "sessions", "list", "--yes"},
		{"chat", "sessions", "list", "--input-json", `{}`},
		{"chat", "sessions", "create", "--input-json", `{"unknown":true}`, "--yes"},
		{"chat", "messages", "window", "s1", "--before", "m1", "--after", "m2"},
		{"chat", "messages", "window", "s1", "--limit", "2"},
		{"chat", "messages", "window", "s1", "--limit", "101"},
		{"chat", "messages", "list", "s1", "--around", "m1"},
		{"chat", "native-request", "s1"},
		{"chat", "native-request", "s1", "--request-id", strings.Repeat("x", 129)},
		{"chat", "native-request", "s1", "--request-id", strings.Repeat("验", 129)},
		{"chat", "native-request", "s1", "--request-id", string([]byte{0xff})},
		{"assistant", "ensure", "ignored", "--yes"},
		{"assistant", "access", "approve", "--yes"},
		{"assistant", "sessions", "get", "s1"},
		{"runs", "get", "r1"},
		{"runs", "events", "r1", "--after-sequence", "-1"},
		{"runs", "cancel", "r1", "--session-id", "another-session", "--yes"},
		{"runs", "resume", "r1", "--input-json", `{}`, "--request-id", "new-turn", "--yes"},
	}
	for _, args := range cases {
		var out, errOut bytes.Buffer
		calls := 0
		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { calls++; return nil, fmt.Errorf("unexpected HTTP") })}
		code := Run(t.Context(), append([]string{"ai"}, args...), Runtime{In: strings.NewReader(""), Out: &out, Err: &errOut, HTTPClient: client, ConfigPath: t.TempDir() + "/unused.json"})
		if code == 0 || calls != 0 || out.Len() != 0 {
			t.Fatalf("args=%v code=%d calls=%d out=%s stderr=%s", args, code, calls, out.String(), errOut.String())
		}
	}
}

func TestAIWorkbenchDeclineAndServerErrorsNeverFallback(t *testing.T) {
	for _, scenario := range []string{"declined", "forbidden", "missing-native-request", "unsupported-resume"} {
		t.Run(scenario, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				status := http.StatusForbidden
				if scenario == "missing-native-request" {
					status = http.StatusNotFound
				}
				if scenario == "unsupported-resume" {
					status = http.StatusNotImplemented
				}
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = fmt.Fprint(w, `{"error":{"code":"access_denied","message":"token=PRIVATE_ERROR_VALUE"}}`)
			}))
			defer server.Close()
			args := []string{"ai", "assistant", "ensure"}
			input := "n\n"
			if scenario != "declined" {
				args = append(args, "--yes")
				input = ""
			}
			if scenario == "missing-native-request" {
				args = []string{"ai", "chat", "native-request", "s1", "--request-id", "original"}
			}
			if scenario == "unsupported-resume" {
				args = []string{"ai", "runs", "resume", "r1", "--input-json", `{}`, "--yes"}
			}
			args = append(args, "--profile", "dev")
			var out, errOut bytes.Buffer
			code := Run(t.Context(), args, Runtime{In: strings.NewReader(input), Out: &out, Err: &errOut, ConfigPath: writeTestConfig(t, server.URL)})
			want := 1
			if scenario == "declined" {
				want = 0
			}
			if code == 0 || calls != want || out.Len() != 0 || strings.Contains(errOut.String(), "PRIVATE_ERROR_VALUE") {
				t.Fatalf("code=%d calls=%d out=%s stderr=%s", code, calls, out.String(), errOut.String())
			}
		})
	}
}
