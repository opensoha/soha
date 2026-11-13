package sohacli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	sohaapi "github.com/opensoha/soha/contracts/gen/go/sohaapi"
)

func directoryReceiptFixture(key uuid.UUID, status string) sohaapi.DirectorySyncRequestEnvelope {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	run := sohaapi.DirectorySyncRequestRun{ID: uuid.MustParse("143cbe75-a373-4ae6-a608-75bff78c2d53"), ConnectionID: "owned", Trigger: "manual", Status: sohaapi.DirectorySyncRunStatus(status)}
	if status != "queued" && status != "running" {
		run.StartedAt, run.FinishedAt = &now, &now
	}
	return sohaapi.DirectorySyncRequestEnvelope{Data: sohaapi.DirectorySyncRequestReceipt{Protocol: "directory-sync-request-v1", IdempotencyKey: key, Run: run}}
}

func TestDirectoryOriginalRequestUsesStableTypedProtocol(t *testing.T) {
	for _, action := range []string{"start", "request", "generated-start"} {
		t.Run(action, func(t *testing.T) {
			key := uuid.MustParse("1d397734-d897-484d-9009-21924e904080")
			var observed uuid.UUID
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.Header.Get("Authorization") != "Bearer profile-token" {
					t.Error("missing profile credential")
				}
				if action == "request" {
					observed = key
					if r.Method != "GET" || r.URL.Path != "/api/v1/access/directory-connections/owned/sync-requests/"+key.String() {
						t.Error("query did not use original-only GET")
					}
				} else {
					var body sohaapi.DirectorySyncRequestInput
					if r.Method != "POST" || r.URL.Path != "/api/v1/access/directory-connections/owned/sync-requests" || json.NewDecoder(r.Body).Decode(&body) != nil || body.IdempotencyKey == uuid.Nil {
						t.Error("invalid typed creation")
					}
					observed = body.IdempotencyKey
					if action == "start" && observed != key {
						t.Error("explicit original UUID changed")
					}
				}
				_ = json.NewEncoder(w).Encode(directoryReceiptFixture(observed, "succeeded"))
			}))
			defer server.Close()
			args := []string{"directory", "sync", "start", "--connection-id", "owned", "--yes"}
			switch action {
			case "request":
				args = []string{"directory", "sync", "request", key.String(), "--connection-id", "owned"}
			case "start":
				args = append(args, "--idempotency-key", key.String())
			}
			var out, stderr bytes.Buffer
			code := Run(t.Context(), args, Runtime{Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
			if code != 0 || calls.Load() != 1 || !strings.Contains(out.String(), `"id": "143cbe75-a373-4ae6-a608-75bff78c2d53"`) {
				t.Fatalf("original receipt missing: code=%d stderr=%s output=%s", code, &stderr, &out)
			}
			if action != "request" && !strings.Contains(stderr.String(), observed.String()) {
				t.Fatal("original UUID was not printed")
			}
		})
	}
}

func TestDirectoryOriginalRequestNeverFallsBackOrAutomaticallyReplays(t *testing.T) {
	for _, failure := range []string{"lost-response", "old-server", "wrong-request", "wrong-connection", "missing-terminal-time", "failed"} {
		t.Run(failure, func(t *testing.T) {
			key := uuid.MustParse("1d397734-d897-484d-9009-21924e904080")
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if !strings.HasSuffix(r.URL.Path, "/sync-requests") {
					t.Error("unsafe legacy fallback")
				}
				if failure == "lost-response" {
					hijacker, ok := w.(http.Hijacker)
					if !ok {
						t.Error("fixture writer does not support hijacking")
						return
					}
					conn, _, err := hijacker.Hijack()
					if err != nil {
						t.Error(err)
						return
					}
					_ = conn.Close()
					return
				}
				if failure == "old-server" {
					http.NotFound(w, r)
					return
				}
				receipt := directoryReceiptFixture(key, "succeeded")
				switch failure {
				case "wrong-request":
					receipt.Data.IdempotencyKey = uuid.New()
				case "wrong-connection":
					receipt.Data.Run.ConnectionID = "foreign"
				case "missing-terminal-time":
					receipt.Data.Run.FinishedAt = nil
				case "failed":
					receipt.Data.Run.Status = "failed"
				}
				_ = json.NewEncoder(w).Encode(receipt)
			}))
			defer server.Close()
			var out, stderr bytes.Buffer
			code := Run(t.Context(), []string{"directory", "sync", "start", "--connection-id", "owned", "--idempotency-key", key.String(), "--yes"}, Runtime{Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
			if code == 0 || calls.Load() != 1 || !strings.Contains(stderr.String(), key.String()) {
				t.Fatalf("failure replayed or lost reference: code=%d calls=%d stderr=%s", code, calls.Load(), &stderr)
			}
			if failure != "failed" && out.Len() != 0 {
				t.Fatal("invalid result printed as success")
			}
			if strings.Contains(out.String()+stderr.String(), "profile-token") {
				t.Fatal("credential leaked")
			}
		})
	}
}

func TestDirectoryOriginalRequestRejectsUnsafeArgumentsBeforeHTTP(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); _, _ = fmt.Fprint(w, `{}`) }))
	defer server.Close()
	for _, args := range [][]string{
		{"start", "--connection-id", "owned", "--idempotency-key", "unsafe/path", "--yes"},
		{"start", "--connection-id", "owned", "--idempotency-key", uuid.Nil.String(), "--yes"},
		{"start", "--connection-id", "owned"},
		{"request", "--connection-id", "owned"},
		{"request", "1d397734-d897-484d-9009-21924e904080", "--connection-id", "owned", "--yes"},
		{"start", "--yes"},
	} {
		var out, stderr bytes.Buffer
		code := Run(t.Context(), append([]string{"directory", "sync"}, args...), Runtime{In: strings.NewReader(""), Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
		if code == 0 {
			t.Fatal("unsafe arguments accepted", args)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid invocation reached HTTP")
	}
}
