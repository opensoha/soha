package dshruntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func startInput() StartRequest {
	return StartRequest{Identity: Identity{RunID: "run-1", Attempt: 1, Fence: 2}, OwnerKey: "owner", SessionID: "session-1", RequestID: "req-1", InputHash: strings.Repeat("a", 64), ContextRevision: "ctx-1", Profile: "chat", Model: "model-1", Prompt: "hello", ToolCatalogRevision: "tools-1", Deadline: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano), MaxOutputTokens: 16}
}
func submission() Submission {
	s := startInput()
	return Submission{Identity: s.Identity, InputHash: s.InputHash, SubmissionID: "sub-1"}
}
func statusResult() Status {
	return Status{SchemaVersion: SchemaVersion, Kind: "status", Submission: submission(), Status: "running", Sequence: 1, UsageKnown: false}
}
func eventResult(seq int64) Event {
	return Event{SchemaVersion: SchemaVersion, Kind: "event", Identity: submission().Identity, Sequence: seq, Type: "message_delta", Data: EventData{Delta: "ok"}}
}
func serve(t *testing.T, h http.HandlerFunc, timeout time.Duration) *Client {
	t.Helper()
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	c, err := New(Config{BaseURL: server.URL, ServiceToken: "secret-service-token", Timeout: timeout})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}
func TestClientLifecycle(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-service-token" {
			t.Error("missing service auth")
		}
		var out any
		switch r.URL.Path {
		case "/v1/health":
			out = Health{SchemaVersion: SchemaVersion, Kind: "health", Ready: true, ProtocolVersion: SchemaVersion, RuntimeVersion: "test-1", Capabilities: []string{"events"}}
		case "/v1/runs":
			if r.Method != http.MethodPost {
				t.Error("start method")
			}
			var in startWire
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Error(err)
			}
			if in.ToolGrant != "grant-secret" || in.Kind != "start" || in.SchemaVersion != SchemaVersion {
				t.Error("start wire")
			}
			out = Receipt{SchemaVersion: SchemaVersion, Kind: "receipt", Submission: submission(), Status: "accepted", Sequence: 1}
			w.WriteHeader(202)
		case "/v1/runs/run-1/attempts/1":
			out = statusResult()
		case "/v1/runs/run-1/attempts/1/events":
			if r.URL.Query().Get("after") != "1" {
				t.Error("cursor")
			}
			out = EventPage{Items: []Event{eventResult(2), eventResult(3)}, NextSequence: 3, HasMore: true}
		case "/v1/runs/run-1/attempts/1/cancel":
			var in controlWire
			if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
				t.Error(err)
			}
			if in.Kind != "cancel" || in.Fence != 2 {
				t.Error("cancel binding")
			}
			s := statusResult()
			s.Status = "canceled"
			out = s
		default:
			t.Error("unexpected route")
			w.WriteHeader(404)
			return
		}
		if err := json.NewEncoder(w).Encode(out); err != nil {
			t.Error(err)
		}
	}, time.Second)
	ctx := context.Background()
	if h, err := c.Health(ctx); err != nil || !h.Ready {
		t.Fatalf("health: %v", err)
	}
	in := startInput()
	in.ToolGrant = "grant-secret"
	if r, err := c.Start(ctx, in); err != nil || r.Submission != submission() {
		t.Fatalf("start: %v", err)
	}
	if s, err := c.Status(ctx, submission()); err != nil || s.UsageKnown || s.Usage != nil || s.Terminal() {
		t.Fatalf("status: %v", err)
	}
	if p, err := c.Events(ctx, submission().Identity, 1); err != nil || p.NextSequence != 3 {
		t.Fatalf("events: %v", err)
	}
	if s, err := c.Cancel(ctx, submission(), "user"); err != nil || !s.Terminal() {
		t.Fatalf("cancel: %v", err)
	}
}
func encoded(v any) string { b, _ := json.Marshal(v); return string(b) }
func TestClientRejectsInvalidResponses(t *testing.T) {
	s := statusResult()
	wrongFence := s
	wrongFence.Fence++
	wrongHash := s
	wrongHash.InputHash = strings.Repeat("b", 64)
	wrongSubmission := s
	wrongSubmission.SubmissionID = "other"
	missingUsage := s
	missingUsage.UsageKnown = true
	zeroUsage := s
	zeroUsage.Usage = &Usage{}
	badTotal := s
	badTotal.UsageKnown = true
	badTotal.Usage = &Usage{InputTokens: 1, OutputTokens: 2, TotalTokens: 4}
	unsafe := s
	unsafe.Fence = maxSafeInteger + 1
	cases := []struct{ name, body string }{
		{"malformed", `{"`}, {"wrong fence", encoded(wrongFence)}, {"wrong hash", encoded(wrongHash)}, {"wrong submission", encoded(wrongSubmission)},
		{"missing usage", encoded(missingUsage)}, {"unknown zero usage", encoded(zeroUsage)}, {"bad usage sum", encoded(badTotal)}, {"unsafe integer", encoded(unsafe)},
		{"missing required", strings.Replace(encoded(s), `,"usageKnown":false`, "", 1)},
		{"null required", strings.Replace(encoded(s), `"usageKnown":false`, `"usageKnown":null`, 1)},
		{"duplicate key", strings.Replace(encoded(s), `"fence":2`, `"fence":3,"fence":2`, 1)},
		{"unknown field", strings.TrimSuffix(encoded(s), "}") + `,"toolGrant":"grant-secret"}`},
		{"trailing JSON", encoded(s) + " {}"}, {"oversized", strings.Repeat(" ", maxBody+1)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := serve(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, tc.body) }, time.Second)
			_, err := c.Status(context.Background(), submission())
			if !errors.Is(err, ErrProtocol) {
				t.Fatalf("expected protocol rejection, got %v", err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("secret in error")
			}
		})
	}
}
func TestEventsRejectSequenceCursorAndBinding(t *testing.T) {
	one, two := eventResult(1), eventResult(2)
	wrong := two
	wrong.Fence = 3
	large := two
	large.Data.Delta = strings.Repeat("界", 6000)
	for name, p := range map[string]EventPage{
		"not exclusive": {Items: []Event{one}, NextSequence: 1}, "descending": {Items: []Event{eventResult(3), two}, NextSequence: 2},
		"wrong cursor": {Items: []Event{two}, NextSequence: 3}, "wrong fence": {Items: []Event{wrong}, NextSequence: 2},
		"empty wrong cursor": {Items: []Event{}, NextSequence: 0}, "no progress": {Items: []Event{}, NextSequence: 1, HasMore: true},
		"delta bytes": {Items: []Event{large}, NextSequence: 2}, "too many": {Items: make([]Event, 257), NextSequence: 2},
	} {
		t.Run(name, func(t *testing.T) {
			c := serve(t, func(w http.ResponseWriter, r *http.Request) { json.NewEncoder(w).Encode(p) }, time.Second)
			if _, err := c.Events(context.Background(), submission().Identity, 1); !errors.Is(err, ErrProtocol) {
				t.Fatalf("got %v", err)
			}
		})
	}
}
func TestHTTPFailuresAndNoRetry(t *testing.T) {
	for _, code := range []int{401, 403, 409, 429, 503, 307} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			var calls atomic.Int32
			c := serve(t, func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Location", "https://untrusted.example/secret")
				w.WriteHeader(code)
				fmt.Fprint(w, "secret-service-token grant-secret")
			}, time.Second)
			_, err := c.Start(context.Background(), startInput())
			var h *HTTPError
			if !errors.As(err, &h) || h.StatusCode != code || calls.Load() != 1 || strings.Contains(err.Error(), "secret") {
				t.Fatalf("calls=%d err=%v", calls.Load(), err)
			}
		})
	}
	var calls atomic.Int32
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		select {
		case <-r.Context().Done():
		case <-time.After(100 * time.Millisecond):
		}
	}, 20*time.Millisecond)
	_, err := c.Start(context.Background(), startInput())
	if !errors.Is(err, context.DeadlineExceeded) || calls.Load() != 1 {
		t.Fatalf("timeout calls=%d err=%v", calls.Load(), err)
	}
}
func TestInputAndConfiguration(t *testing.T) {
	trusted, err := New(Config{BaseURL: "http://dsh-runtime:19091", ServiceToken: "token", Timeout: time.Second, AllowInsecureHTTP: true})
	if err != nil {
		t.Fatalf("explicit isolated HTTP: %v", err)
	}
	trusted.Close()
	for _, base := range []string{"http://example.com", "https://:80", "https://u:secret@example.com", "https://example.com?q=secret", "https://example.com#secret", "file:///tmp/a", "https://example.com?", "https://example.com#"} {
		if _, err := New(Config{BaseURL: base, ServiceToken: "token", Timeout: time.Second}); err == nil {
			t.Errorf("accepted %s", base)
		}
	}
	for _, cfg := range []Config{{BaseURL: "https://example.com", ServiceToken: "token"}, {BaseURL: "https://example.com", ServiceToken: "bad\r\n", Timeout: time.Second}} {
		if _, err := New(cfg); err == nil {
			t.Error("accepted bad config")
		}
	}
	var calls atomic.Int32
	c := serve(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(500) }, time.Second)
	for name, mutate := range map[string]func(*StartRequest){
		"expired":         func(s *StartRequest) { s.Deadline = time.Now().Add(-time.Minute).Format(time.RFC3339) },
		"unsafe fence":    func(s *StartRequest) { s.Fence = maxSafeInteger + 1 },
		"chat write tool": func(s *StartRequest) { s.Tools = []Tool{{Name: "write", InputSchema: json.RawMessage(`{}`)}} },
		"duplicate tool": func(s *StartRequest) {
			tool := Tool{Name: "read", ReadOnly: true, InputSchema: json.RawMessage(`{}`)}
			s.Tools = []Tool{tool, tool}
		},
		"remote schema": func(s *StartRequest) {
			s.Tools = []Tool{{Name: "read", ReadOnly: true, InputSchema: json.RawMessage(`{"$ref":"https://example.com"}`)}}
		},
		"huge body": func(s *StartRequest) {
			s.Context = make([]ContextMessage, 128)
			for i := range s.Context {
				s.Context[i] = ContextMessage{Role: "user", Content: strings.Repeat("x", 131072)}
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := startInput()
			mutate(&s)
			if _, err := c.Start(context.Background(), s); !errors.Is(err, ErrInput) {
				t.Fatalf("got %v", err)
			}
		})
	}
	if calls.Load() != 0 {
		t.Fatal("invalid inputs reached service")
	}
}
