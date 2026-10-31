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
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func healingReceiptFixture(key uuid.UUID) sohaapi.HealingOriginalRequestEnvelope {
	now := time.Date(2026, 10, 29, 0, 0, 0, 0, time.UTC)
	return sohaapi.HealingOriginalRequestEnvelope{Data: sohaapi.HealingOriginalRequestReceipt{Protocol: "healing-request-v1", IdempotencyKey: key, Run: sohaapi.HealingOriginalRun{
		ID: "healing-run:owned", EventID: "event:owned", PolicyID: "policy:owned", Status: "pending_approval", ApprovalStatus: "pending", SubmissionStatus: "not_submitted", ExecutionGeneration: 1, CreatedAt: now, UpdatedAt: now,
	}}}
}

func TestHealingOriginalRequestUsesStableTypedProtocol(t *testing.T) {
	for _, action := range []string{"heal", "generated", "request"} {
		t.Run(action, func(t *testing.T) {
			key := uuid.MustParse("51a2f86e-d1c0-472a-a843-b7b13c0043c9")
			var calls atomic.Int32
			var seen atomic.Value
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				observed := key
				if r.Header.Get("Authorization") != "Bearer profile-token" {
					t.Error("missing bound credential")
				}
				if action == "request" {
					if r.Method != "GET" || r.URL.Path != "/api/v1/alert-events/event:owned/healing-requests/"+key.String() {
						t.Error("request lookup did not use exact GET")
					}
				} else {
					var input sohaapi.HealingOriginalRequestInput
					if r.Method != "POST" || r.URL.Path != "/api/v1/alert-events/event:owned/healing-requests" || json.NewDecoder(r.Body).Decode(&input) != nil || input.PolicyID != "policy:owned" || input.IdempotencyKey == uuid.Nil {
						t.Error("invalid typed request")
					}
					observed = input.IdempotencyKey
					if action == "heal" && observed != key {
						t.Error("original key changed")
					}
				}
				seen.Store(observed.String())
				_ = json.NewEncoder(w).Encode(healingReceiptFixture(observed))
			}))
			defer server.Close()
			args := []string{"observe", "events", "heal", "event:owned", "--policy-id", "policy:owned", "--yes"}
			if action == "heal" {
				args = append(args, "--idempotency-key", key.String())
			}
			if action == "request" {
				args = []string{"observe", "healing-runs", "request", key.String(), "--event-id", "event:owned"}
			}
			var out, stderr bytes.Buffer
			code := Run(t.Context(), args, Runtime{Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
			if code != 0 || calls.Load() != 1 || !strings.Contains(out.String(), `"id": "healing-run:owned"`) {
				t.Fatalf("code=%d calls=%d stderr=%s out=%s", code, calls.Load(), &stderr, &out)
			}
			if action != "request" {
				original, ok := seen.Load().(string)
				if !ok || !strings.Contains(stderr.String(), original) {
					t.Fatal("original UUID not printed")
				}
			}
		})
	}
}

func TestHealingRequestNeverFallsBackOrResubmitsUnknown(t *testing.T) {
	for _, fault := range []string{"lost-response", "old-server", "redirect", "wrong-key", "wrong-event", "wrong-policy", "missing-generation", "unknown-without-workflow", "missing-created-time"} {
		t.Run(fault, func(t *testing.T) {
			key := uuid.MustParse("51a2f86e-d1c0-472a-a843-b7b13c0043c9")
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/api/v1/alert-events/event:owned/healing-requests" {
					t.Error("legacy fallback or wrong target")
				}
				switch fault {
				case "lost-response":
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
				case "old-server":
					http.NotFound(w, r)
					return
				case "redirect":
					http.Redirect(w, r, "/unsafe", http.StatusTemporaryRedirect)
					return
				}
				receipt := healingReceiptFixture(key)
				switch fault {
				case "wrong-key":
					receipt.Data.IdempotencyKey = uuid.New()
				case "wrong-event":
					receipt.Data.Run.EventID = "foreign"
				case "wrong-policy":
					receipt.Data.Run.PolicyID = "foreign"
				case "missing-generation":
					receipt.Data.Run.ExecutionGeneration = 0
				case "unknown-without-workflow":
					receipt.Data.Run.SubmissionStatus = "unknown"
				case "missing-created-time":
					receipt.Data.Run.CreatedAt = time.Time{}
				}
				_ = json.NewEncoder(w).Encode(receipt)
			}))
			defer server.Close()
			var out, stderr bytes.Buffer
			code := Run(t.Context(), []string{"observe", "events", "heal", "event:owned", "--policy-id", "policy:owned", "--idempotency-key", key.String(), "--yes"}, Runtime{Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
			if code == 0 || calls.Load() != 1 || out.Len() != 0 || !strings.Contains(stderr.String(), key.String()) {
				t.Fatalf("unsafe outcome code=%d calls=%d stderr=%s out=%s", code, calls.Load(), &stderr, &out)
			}
			if strings.Contains(stderr.String(), "profile-token") {
				t.Fatal("credential leaked")
			}
		})
	}
}

func TestHealingRequestRejectsUnsafeArgumentsBeforeHTTP(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); _, _ = fmt.Fprint(w, `{}`) }))
	defer server.Close()
	for _, args := range [][]string{
		{"events", "heal", "event:owned", "--policy-id", "policy:owned", "--idempotency-key", "unsafe/path", "--yes"},
		{"events", "heal", "event:owned", "--policy-id", "policy:owned", "--idempotency-key", uuid.Nil.String(), "--yes"},
		{"events", "heal", "event:owned", "--policy-id", "policy:owned"},
		{"events", "heal", "event:owned", "--yes"},
		{"healing-runs", "request", "51a2f86e-d1c0-472a-a843-b7b13c0043c9", "--event-id", "event:owned", "--yes"},
		{"healing-runs", "request", "51a2f86e-d1c0-472a-a843-b7b13c0043c9"},
	} {
		var out, stderr bytes.Buffer
		if code := Run(t.Context(), append([]string{"observe"}, args...), Runtime{In: strings.NewReader(""), Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)}); code == 0 {
			t.Fatal("unsafe arguments accepted", args)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("invalid invocation reached HTTP")
	}
}
