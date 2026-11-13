package sohacli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNativeCatalogCommandsUseFixedTypedEndpoints(t *testing.T) {
	cases := []struct {
		name               string
		args               []string
		method, path, body string
	}{
		{"knowledge-bases", []string{"knowledge", "bases", "list"}, "GET", "/api/v1/ai/knowledge-bases", ""},
		{"knowledge-base-get", []string{"knowledge", "bases", "get", "base:1"}, "GET", "/api/v1/ai/knowledge-bases/base:1", ""},
		{"knowledge-base-create", []string{"knowledge", "bases", "create"}, "POST", "/api/v1/ai/knowledge-bases", `{"name":"demo","scope":{}}`},
		{"knowledge-base-update", []string{"knowledge", "bases", "update", "base:1"}, "PATCH", "/api/v1/ai/knowledge-bases/base:1", `{"name":"renamed"}`},
		{"knowledge-base-delete", []string{"knowledge", "bases", "delete", "base:1"}, "DELETE", "/api/v1/ai/knowledge-bases/base:1", ""},
		{"knowledge-source-list", []string{"knowledge", "sources", "list", "--base-id", "base:1"}, "GET", "/api/v1/ai/knowledge-bases/base:1/sources", ""},
		{"knowledge-source-create", []string{"knowledge", "sources", "create", "--base-id", "base:1"}, "POST", "/api/v1/ai/knowledge-bases/base:1/sources", `{"name":"docs","kind":"manual"}`},
		{"documents", []string{"knowledge", "documents", "list", "--base-id", "base:1", "--limit", "2"}, "GET", "/api/v1/ai/knowledge-bases/base:1/documents?limit=2", ""},
		{"sync-runs", []string{"knowledge", "sync-runs", "list", "--base-id", "base:1"}, "GET", "/api/v1/ai/knowledge-bases/base:1/sync-runs?limit=50", ""},
		{"index-revisions", []string{"knowledge", "index-revisions", "list", "--base-id", "base:1"}, "GET", "/api/v1/ai/knowledge-bases/base:1/index-revisions?limit=50", ""},
		{"evaluation-datasets", []string{"ai", "evaluation", "datasets", "list"}, "GET", "/api/v1/ai/evaluations/datasets", ""},
		{"evaluation-runs", []string{"ai", "evaluation", "runs", "list"}, "GET", "/api/v1/ai/evaluations/runs", ""},
		{"evaluation-get", []string{"ai", "evaluation", "runs", "get", "run:1"}, "GET", "/api/v1/ai/evaluations/runs/run:1", ""},
		{"evaluation-results", []string{"ai", "evaluation", "runs", "results", "run:1"}, "GET", "/api/v1/ai/evaluations/runs/run:1/results", ""},
		{"evaluation-attempts", []string{"ai", "evaluation", "runs", "attempts", "run:1"}, "GET", "/api/v1/ai/evaluations/runs/run:1/attempts", ""},
		{"environment", []string{"ai", "assistant", "environment", "get"}, "GET", "/api/v1/copilot/assistant/environment", ""},
		{"environment-templates", []string{"ai", "assistant", "environment", "templates"}, "GET", "/api/v1/copilot/assistant/environment/templates", ""},
		{"environment-change", []string{"ai", "assistant", "environment", "change"}, "POST", "/api/v1/copilot/assistant/environment", `{"action":"sleep"}`},
		{"workspace-runtime", []string{"ai", "assistant", "runtime", "get"}, "GET", "/api/v1/copilot/assistant/environment/runtime", ""},
		{"runtime-operation", []string{"ai", "assistant", "runtime", "operation", "operation:1"}, "GET", "/api/v1/copilot/assistant/environment/runtime/operation:1", ""},
		{"runtime-execute", []string{"ai", "assistant", "runtime", "execute"}, "POST", "/api/v1/copilot/assistant/environment/runtime", `{"kind":"exec","environmentVersion":9007199254740993,"idempotencyKey":"runtime-original","argv":["echo","hello"]}`},
		{"feed", []string{"ai", "assistant", "feed", "list"}, "GET", "/api/v1/copilot/assistant/feed?before=&limit=50", ""},
		{"feed-get", []string{"ai", "assistant", "feed", "get", "item:1"}, "GET", "/api/v1/copilot/assistant/feed/items/item:1", ""},
		{"feed-feedback", []string{"ai", "assistant", "feed", "feedback", "item:1"}, "PATCH", "/api/v1/copilot/assistant/feed/items/item:1", `{"liked":true}`},
		{"feed-refresh", []string{"ai", "assistant", "feed", "refresh"}, "POST", "/api/v1/copilot/assistant/feed/refresh", ""},
		{"monitoring-summary", []string{"observe", "summary"}, "GET", "/api/v1/monitoring/summary", ""},
		{"alert-list", []string{"observe", "alerts", "list", "--cluster-id", "cluster:1", "--status", "firing"}, "GET", "/api/v1/alerts?clusterId=cluster%3A1&limit=50&status=firing", ""},
		{"alert-assign", []string{"observe", "alerts", "assign", "alert:1"}, "PUT", "/api/v1/alerts/alert:1/ownership", `{"ownerTeam":"sre","assignee":"user:1"}`},
		{"alert-ack", []string{"observe", "alerts", "acknowledge", "alert:1"}, "POST", "/api/v1/alerts/alert:1/acknowledge", ""},
		{"event-resolve", []string{"observe", "events", "resolve", "event:1"}, "POST", "/api/v1/alert-events/event:1/resolve", ""},
		{"healing-approve", []string{"observe", "healing-runs", "approve", "run:1"}, "POST", "/api/v1/healing-runs/run:1/approve", `{"comment":"reviewed"}`},
		{"healing-retry", []string{"observe", "healing-runs", "retry", "run:1"}, "POST", "/api/v1/healing-runs/run:1/retry", ""},
		{"directory-runs", []string{"directory", "runs", "list", "--connection-id", "directory:1"}, "GET", "/api/v1/access/directory-connections/directory:1/runs", ""},
		{"directory-run-get", []string{"directory", "runs", "get", "run:1"}, "GET", "/api/v1/access/directory-runs/run:1", ""},
		{"directory-sync", []string{"directory", "sync", "start", "--connection-id", "directory:1", "--idempotency-key", "1d397734-d897-484d-9009-21924e904080"}, "POST", "/api/v1/access/directory-connections/directory:1/sync-requests", ""},
		{"directory-cancel", []string{"directory", "runs", "cancel", "run:1", "--connection-id", "directory:1"}, "POST", "/api/v1/access/directory-connections/directory:1/runs/run:1/cancel", ""},
		{"directory-runtime", []string{"directory", "runtime", "get", "--connection-id", "directory:1"}, "GET", "/api/v1/access/directory-connections/directory:1/runtime-status", ""},
		{"directory-retry", []string{"directory", "events", "retry", "event:1", "--connection-id", "directory:1"}, "POST", "/api/v1/access/directory-connections/directory:1/events/event:1/retry", ""},
		{"network-sessions", []string{"network-access", "sessions", "list"}, "GET", "/api/v1/network-access/sessions?limit=50", ""},
		{"network-command", []string{"network-access", "sessions", "command", "command:1", "--session-id", "session:1"}, "GET", "/api/v1/network-access/sessions/session:1/commands/command:1", ""},
		{"network-original", []string{"network-access", "sessions", "request", "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", "--session-id", "session:1"}, "GET", "/api/v1/network-access/sessions/session:1/command-request?planHash=sha256%3Aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", ""},
		{"network-telemetry", []string{"network-access", "telemetry", "get"}, "GET", "/api/v1/network-access/telemetry/summary", ""},
		{"network-enrollments", []string{"network-access", "enrollments", "list"}, "GET", "/api/v1/network-access/enrollments?limit=50", ""},
		{"network-grant-revoke", []string{"network-access", "access-grants", "revoke", "grant:1"}, "POST", "/api/v1/network-access/access-grants/grant:1/revoke", ""},
		{"network-policy", []string{"network-access", "policy", "snapshot"}, "GET", "/api/v1/network-access/policy/snapshot", ""},
		{"workflow-run-get", []string{"delivery", "executions", "get", "run:1"}, "GET", "/api/v1/workflows/run:1", ""},
		{"workflow-runtime", []string{"delivery", "executions", "runtime", "run:1"}, "GET", "/api/v1/delivery/runtime/workflows/run:1", ""},
		{"workflow-trigger", []string{"delivery", "executions", "trigger"}, "POST", "/api/v1/workflow-requests", `{"idempotencyKey":"original-request-1","workflow":{"applicationId":"app:1","triggerBuild":true}}`},
		{"workflow-approve", []string{"delivery", "executions", "approve", "run:1"}, "POST", "/api/v1/workflows/run:1/approve", `{"comment":"reviewed"}`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.Method != test.method || r.URL.RequestURI() != test.path || r.Header.Get("Authorization") != "Bearer profile-token" {
					t.Errorf("method/path/auth mismatch: %s %s", r.Method, r.URL.RequestURI())
				}
				if test.body != "" {
					var body map[string]json.RawMessage
					if json.NewDecoder(r.Body).Decode(&body) != nil {
						t.Error("missing typed body")
					}
					if test.name == "runtime-execute" && string(body["environmentVersion"]) != "9007199254740993" {
						t.Error("environment version lost precision")
					}
				}
				w.Header().Set("Content-Type", "application/json")
				switch test.name {
				case "workflow-trigger":
					_, _ = fmt.Fprint(w, `{"data":{"id":"request-1","requestKey":"original-request-1","runId":"workflow:original","submissionStatus":"accepted"}}`)
				case "workflow-run-get", "workflow-approve":
					_, _ = fmt.Fprint(w, `{"data":{"id":"run:1","status":"running"}}`)
				case "directory-sync":
					_, _ = fmt.Fprint(w, `{"data":{"protocol":"directory-sync-request-v1","idempotencyKey":"1d397734-d897-484d-9009-21924e904080","run":{"id":"143cbe75-a373-4ae6-a608-75bff78c2d53","connectionId":"directory:1","trigger":"manual","status":"running"}}}`)
				case "network-command", "network-original":
					_, _ = fmt.Fprint(w, `{"data":{"id":"command:1","sessionId":"session:1","status":"pending"}}`)
				case "environment-templates":
					_, _ = fmt.Fprint(w, `{"data":[]}`)
				default:
					_, _ = fmt.Fprint(w, `{"data":{},"items":[]}`)
				}
			}))
			defer server.Close()
			args := append([]string(nil), test.args...)
			if test.body != "" {
				args = append(args, "--input", "-")
			}
			if test.method != "GET" {
				args = append(args, "--yes")
			}
			var out, stderr bytes.Buffer
			code := Run(t.Context(), args, Runtime{In: strings.NewReader(test.body), Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
			if code != 0 || calls != 1 {
				t.Fatalf("code=%d calls=%d stderr=%s", code, calls, stderr.String())
			}
		})
	}
}

func TestNativeCatalogRejectsInvalidScopeInputAndInapplicableFlagsBeforeHTTP(t *testing.T) {
	cases := [][]string{
		{"knowledge", "sources", "list"},
		{"knowledge", "bases", "get", "../other"},
		{"knowledge", "sources", "update", "source-1", "--base-id", "base-1"},
		{"knowledge", "documents", "list", "--base-id", "base-1", "--limit", "201"},
		{"ai", "assistant", "feed", "list", "--limit", "51"},
		{"ai", "assistant", "runtime", "execute", "--input-json", `{"environmentVersion":1,"unknown":true}`, "--yes"},
		{"observe", "alerts", "list", "--yes"},
		{"observe", "events", "heal", "event-1", "--yes"},
		{"observe", "alerts", "assign", "alert-1", "--input-json", `{"ownerTeam":"sre","assignee":"a","allow":true}`, "--yes"},
		{"directory", "runs", "cancel", "run-1", "--yes"},
		{"directory", "runs", "list", "--connection-id", "directory-1", "--limit", "1"},
		{"network-access", "sessions", "execute", "session-1", "--input-json", `{"action":"disconnect","planHash":"x","allow":true}`, "--yes"},
		{"network-access", "telemetry", "get", "--show-access-details"},
		{"delivery", "executions", "trigger", "--input-json", `{"applicationId":"app-1","shell":"sh"}`, "--yes"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls++ }))
			defer server.Close()
			var out, stderr bytes.Buffer
			code := Run(t.Context(), args, Runtime{In: strings.NewReader(""), Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
			if code == 0 || calls != 0 {
				t.Fatalf("code=%d calls=%d stderr=%s", code, calls, stderr.String())
			}
		})
	}
}

func TestNativeGrantCreationCannotBypassMFAOrExportSecretsImplicitly(t *testing.T) {
	for _, mode := range []string{"default", "explicit", "mfa-denied", "redirect"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				switch mode {
				case "mfa-denied":
					w.WriteHeader(403)
					_, _ = fmt.Fprint(w, `{"code":"mfa_required","message":"Bearer profile-token"}`)
				case "redirect":
					w.Header().Set("Location", "/weaker")
					w.WriteHeader(307)
				default:
					_, _ = fmt.Fprint(w, `{"data":{"grant":{"id":"grant-1"},"token":"ONE_TIME_SECRET","echo":"profile-token"}}`)
				}
			}))
			defer server.Close()
			args := []string{"network-access", "access-grants", "create", "--input-json", `{"deviceId":"device-1","siteId":"site-1","networkSpaceId":"space-1","resourceIds":["resource-1"],"mode":"ztna"}`, "--yes"}
			if mode == "explicit" {
				args = append(args, "--show-access-details")
			}
			var out, stderr bytes.Buffer
			code := Run(t.Context(), args, Runtime{Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
			if calls != 1 || strings.Contains(out.String()+stderr.String(), "profile-token") {
				t.Fatal("credential leaked or request replayed")
			}
			if mode == "explicit" {
				if code != 0 || !strings.Contains(out.String(), "ONE_TIME_SECRET") {
					t.Fatal("explicit credential export failed")
				}
			} else if strings.Contains(out.String(), "ONE_TIME_SECRET") {
				t.Fatal("implicit credential export")
			}
			if (mode == "mfa-denied" || mode == "redirect") && code == 0 {
				t.Fatal("denied or redirected issuance succeeded")
			}
		})
	}
}

func TestWorkflowRequestUnknownAndOldServerNeverFallBackOrReplay(t *testing.T) {
	for _, status := range []int{202, 404, 503} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				if r.URL.Path != "/api/v1/workflow-requests" {
					t.Error("legacy fallback attempted")
				}
				w.WriteHeader(status)
				if status == 202 {
					_, _ = fmt.Fprint(w, `{"data":{"id":"request-1","requestKey":"original-request-1","runId":"workflow:original","submissionStatus":"unknown"}}`)
				} else {
					_, _ = fmt.Fprint(w, `{"code":"unavailable","message":"request unavailable"}`)
				}
			}))
			defer server.Close()
			var out, stderr bytes.Buffer
			code := Run(t.Context(), []string{"delivery", "executions", "trigger", "--input-json", `{"idempotencyKey":"original-request-1","workflow":{"applicationId":"app-1"}}`, "--yes"}, Runtime{Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
			if code != 1 || calls != 1 {
				t.Fatalf("uncertain request replayed: code=%d calls=%d", code, calls)
			}
			if status == 202 && (!strings.Contains(out.String(), "original-request-1") || !strings.Contains(stderr.String(), "no automatic replay")) {
				t.Fatal("original receipt lost")
			}
			if status == 503 && !strings.Contains(stderr.String(), "executions request original-request-1") {
				t.Fatal("original lookup instruction missing")
			}
		})
	}
}
