package sohacli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRunLogsQueryRoutesAndRedacts(t *testing.T) {
	if _, err := buildLogQuery(logQueryOptions{since: -time.Second}); err == nil {
		t.Fatal("negative --since was accepted")
	}
	tests := []struct {
		name, path string
		args       []string
	}{
		{name: "cluster", path: "/api/v1/clusters/cluster-1/logs/query", args: []string{"--source", "cluster", "--cluster-id", "cluster-1"}},
		{name: "docker", path: "/api/v1/docker/projects/project-1/logs/query", args: []string{"--source", "docker", "--project-id", "project-1"}},
		{name: "delivery", path: "/api/v1/delivery/applications/app-1/environments/env-1/logs/query", args: []string{"--source", "delivery", "--application-id", "app-1", "--environment-id", "env-1"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != test.path {
					t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
				}
				writeJSON(t, w, map[string]any{"data": map[string]any{
					"entries": []map[string]any{{"timestamp": "2026-08-02T10:00:00Z", "message": "password=secret", "source": map[string]any{"domain": test.name}, "sourceMode": "durable"}},
					"partial": false, "scopeRestricted": false, "truncated": false,
				}})
			}))
			defer server.Close()

			var out, stderr bytes.Buffer
			args := append([]string{"logs", "query", "--profile", "dev", "--output", "ndjson"}, test.args...)
			if code := Run(context.Background(), args, Runtime{Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)}); code != 0 {
				t.Fatalf("logs query returned %d: %s", code, stderr.String())
			}
			if strings.Contains(out.String(), "secret") || !strings.Contains(out.String(), "[REDACTED]") {
				t.Fatalf("logs output was not redacted: %s", out.String())
			}
		})
	}
}

func TestTailLogsDeduplicatesBoundary(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	entry := LogEntry{Timestamp: time.Date(2026, 8, 2, 10, 0, 0, 0, time.UTC), Message: "ready"}
	calls := 0
	query := func(context.Context, LogQuery) (LogPage, error) {
		calls++
		if calls == 2 {
			cancel()
		}
		return LogPage{Entries: []LogEntry{entry}}, nil
	}
	var out bytes.Buffer
	err := tailLogs(ctx, &out, query, LogQuery{}, "ndjson", time.Millisecond)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("tail error = %v", err)
	}
	if got := strings.Count(strings.TrimSpace(out.String()), "\n") + 1; got != 1 {
		t.Fatalf("tail emitted %d duplicate rows: %q", got, out.String())
	}
}

func TestRunOperationGetWaitCancel(t *testing.T) {
	waitCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := "succeeded"
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/compute/tasks/virtualization/get-1":
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/compute/tasks/container_runtime/wait-1":
			waitCalls++
			if waitCalls == 1 {
				status = "running"
			}
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/compute/tasks/virtualization/failed-1":
			status = "failed"
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/compute/tasks/virtualization/cancel-1/cancel":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["reason"] != "operator request" {
				t.Fatalf("unexpected cancel body %#v, err=%v", body, err)
			}
			status = "canceled"
		default:
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		id := strings.Split(strings.TrimSuffix(r.URL.Path, "/cancel"), "/")
		writeJSON(t, w, map[string]any{"data": map[string]any{"id": id[len(id)-1], "domain": strings.Split(r.URL.Path, "/")[5], "normalizedStatus": status, "kind": "test"}})
	}))
	defer server.Close()
	configPath := writeTestConfig(t, server.URL)

	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "get", args: []string{"operation", "get", "virtualization", "get-1"}, want: `"normalizedStatus": "succeeded"`},
		{name: "wait", args: []string{"operation", "wait", "container_runtime", "wait-1", "--interval", "1ms", "--wait-timeout", "1s"}, want: `"normalizedStatus": "succeeded"`},
		{name: "cancel", args: []string{"operation", "cancel", "virtualization", "cancel-1", "--reason", "operator request", "--yes"}, want: `"normalizedStatus": "canceled"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var out, stderr bytes.Buffer
			if code := Run(context.Background(), test.args, Runtime{Out: &out, Err: &stderr, ConfigPath: configPath}); code != 0 {
				t.Fatalf("operation returned %d: %s", code, stderr.String())
			}
			if !strings.Contains(out.String(), test.want) {
				t.Fatalf("operation output missing %q: %s", test.want, out.String())
			}
		})
	}
	if waitCalls != 2 {
		t.Fatalf("wait calls = %d, want 2", waitCalls)
	}
	var failedOut bytes.Buffer
	if code := Run(context.Background(), []string{"operation", "wait", "virtualization", "failed-1", "--interval", "1ms"}, Runtime{Out: &failedOut, Err: &bytes.Buffer{}, ConfigPath: configPath}); code != 1 || !strings.Contains(failedOut.String(), `"normalizedStatus": "failed"`) {
		t.Fatalf("failed wait returned %d with output %s", code, failedOut.String())
	}
}

func TestRunToolCallProtection(t *testing.T) {
	if !protectedTool(ToolCapability{}) {
		t.Fatal("unknown tool risk must fail closed")
	}
	invocations := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/ai-gateway/capabilities":
			writeJSON(t, w, map[string]any{"data": map[string]any{"tools": []map[string]any{{"name": "danger.run", "riskLevel": "execute", "requiresApproval": true}}}})
		case "/api/v1/ai-gateway/tools/danger.run/invoke":
			invocations++
			writeJSON(t, w, map[string]any{"data": map[string]any{"toolName": "danger.run", "result": "success"}})
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	configPath := writeTestConfig(t, server.URL)

	var preview bytes.Buffer
	if code := Run(context.Background(), []string{"tool", "call", "danger.run", "--input-json", `{"password":"secret"}`, "--preview"}, Runtime{Out: &preview, Err: &bytes.Buffer{}, ConfigPath: configPath}); code != 0 {
		t.Fatalf("preview returned %d", code)
	}
	if invocations != 0 || strings.Contains(preview.String(), "secret") || !strings.Contains(preview.String(), "[REDACTED]") {
		t.Fatalf("unsafe preview: calls=%d output=%s", invocations, preview.String())
	}
	if code := Run(context.Background(), []string{"tool", "call", "danger.run"}, Runtime{In: strings.NewReader("n\n"), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, ConfigPath: configPath}); code == 0 || invocations != 0 {
		t.Fatalf("declined invocation returned %d with %d calls", code, invocations)
	}
	if code := Run(context.Background(), []string{"tool", "call", "danger.run", "--yes"}, Runtime{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, ConfigPath: configPath}); code != 0 || invocations != 1 {
		t.Fatalf("approved invocation returned %d with %d calls", code, invocations)
	}
}

func TestRunDiagnoseJSONReusesSetupCheck(t *testing.T) {
	t.Setenv("CODEX_HOME", t.TempDir())
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ai-gateway/capabilities" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if r.Header.Get("X-Soha-AI-Client") != "codex" {
			t.Fatalf("unexpected AI client header %q", r.Header.Get("X-Soha-AI-Client"))
		}
		writeJSON(t, w, map[string]any{"data": map[string]any{"name": "soha AI Gateway", "tools": []map[string]any{}}})
	}))
	defer server.Close()
	configPath := writeTestConfig(t, server.URL)
	if code := Run(context.Background(), []string{"setup", "--client", "codex", "--mode", "mcp", "--profile", "dev"}, Runtime{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, ConfigPath: configPath}); code != 0 {
		t.Fatalf("setup returned %d", code)
	}

	var out, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"diagnose", "--profile", "dev", "--client", "codex", "--output", "json"}, Runtime{Out: &out, Err: &stderr, ConfigPath: configPath}); code != 0 {
		t.Fatalf("diagnose returned %d: %s", code, stderr.String())
	}
	var report map[string]any
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("invalid diagnose JSON: %v\n%s", err, out.String())
	}
	check, _ := report["clientCheck"].(map[string]any)
	if check["status"] != "ok" || check["client"] != "codex" {
		t.Fatalf("unexpected client check %#v", check)
	}
}

func TestRunNestedHelpAndAdditionalCompletions(t *testing.T) {
	for _, test := range []struct {
		args []string
		want string
	}{
		{args: []string{"help", "cloud", "fleet", "diagnostics"}, want: "soha cloud fleet diagnostics"},
		{args: []string{"plugin", "search", "--help"}, want: "Search plugin marketplace entries"},
		{args: []string{"completion", "fish"}, want: "complete -c soha"},
		{args: []string{"completion", "powershell"}, want: "Register-ArgumentCompleter"},
	} {
		var out, stderr bytes.Buffer
		if code := Run(context.Background(), test.args, Runtime{Out: &out, Err: &stderr}); code != 0 {
			t.Fatalf("%v returned %d: %s", test.args, code, stderr.String())
		}
		if !strings.Contains(out.String(), test.want) {
			t.Fatalf("%v output missing %q: %s", test.args, test.want, out.String())
		}
	}
}

func TestRunAutomationExitCodes(t *testing.T) {
	for _, args := range [][]string{nil, {"unknown"}, {"version", "--unknown-flag"}, {"help", "logs", "bogus"}} {
		if code := Run(context.Background(), args, Runtime{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}}); code != 2 {
			t.Fatalf("%v returned %d, want usage exit 2", args, code)
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := Run(ctx, []string{"operation", "get", "virtualization", "task-1"}, Runtime{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, ConfigPath: writeTestConfig(t, "http://127.0.0.1:1")}); code != 130 {
		t.Fatalf("canceled operation returned %d, want 130", code)
	}
}
