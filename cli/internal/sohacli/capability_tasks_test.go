package sohacli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestCapabilityTaskCommandsUseSharedAPI(t *testing.T) {
	for _, action := range []string{"validate", "create", "resume", "cancel", "get", "list"} {
		t.Run(action, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				expected := "/api/v1/ai-gateway/tasks"
				switch action {
				case "validate":
					expected = "/api/v1/ai-gateway/plans/validate"
				case "resume", "cancel":
					expected += "/task-1/" + action
				case "get":
					expected += "/task-1"
				}
				if r.URL.Path != expected {
					t.Errorf("path = %s", r.URL.Path)
				}
				if action == "get" && r.URL.Query().Get("planVersion") != "1" {
					t.Error("missing archive selector")
				}
				if action == "resume" {
					var value map[string]any
					if json.NewDecoder(r.Body).Decode(&value) != nil || value["expectedVersion"] != float64(7) {
						t.Error("missing revision fence")
					}
				}
				if action == "validate" {
					writeJSON(t, w, map[string]any{"data": map[string]any{"valid": true, "issues": []any{}}})
					return
				}
				if action == "list" {
					writeJSON(t, w, map[string]any{"items": []any{}})
					return
				}
				writeJSON(t, w, map[string]any{"data": map[string]any{"id": "task-1", "status": "queued", "planVersion": 2}})
			}))
			defer server.Close()
			args := []string{"ai", "task", action}
			switch action {
			case "get":
				args = append(args, "task-1", "--plan-version", "1")
			case "cancel":
				args = append(args, "task-1", "--yes")
			case "resume":
				args = append(args, "task-1", "--input-json", `{"expectedVersion":7,"plan":{"goal":"test"}}`, "--yes")
			case "create", "validate":
				args = append(args, "--input-json", `{"idempotencyKey":"same-goal","plan":{"goal":"test"}}`, "--yes")
			}
			args = append(args, "--profile", "dev")
			var out, stderr bytes.Buffer
			if code := Run(context.Background(), args, Runtime{Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)}); code != 0 || calls != 1 {
				t.Fatalf("code=%d calls=%d error=%s", code, calls, stderr.String())
			}
		})
	}
}

func TestCapabilityTaskWaitStopsAtApprovalWithoutCanceling(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Method != http.MethodGet {
			t.Fatal("client wait mutated the task")
		}
		status := "running"
		if calls == 2 {
			status = "waiting_approval"
		}
		writeJSON(t, w, map[string]any{"data": map[string]any{"id": "task-1", "status": status}})
	}))
	defer server.Close()
	var out, stderr bytes.Buffer
	code := Run(context.Background(), []string{"ai", "task", "wait", "task-1", "--interval", "1ms", "--wait-timeout", "1s", "--profile", "dev"}, Runtime{Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
	if code == 0 || calls != 2 || !strings.Contains(out.String(), "waiting_approval") {
		t.Fatalf("wait concealed attention state: %d %d %s %s", code, calls, out.String(), stderr.String())
	}
}

func TestCapabilityMCPTasksReturnStructuredSharedTask(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/ai-gateway/capabilities":
			writeJSON(t, w, map[string]any{"data": map[string]any{"tools": []any{}, "resources": []any{}, "prompts": []any{}}})
		case "/api/v1/ai-gateway/tasks/task-1":
			writeJSON(t, w, map[string]any{"data": map[string]any{"id": "task-1", "status": "blocked", "planVersion": 2}})
		default:
			t.Errorf("unexpected backend %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"task-test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"soha.tasks.get","arguments":{"taskId":"task-1"}}}`,
		"",
	}, "\n")
	stdio := newMCPTestIO(input, 3)
	var stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	code := Run(ctx, []string{"mcp", "--profile", "dev", "--base-url", server.URL}, Runtime{In: stdio, Out: stdio, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
	output := stdio.String()
	if code != 0 {
		t.Fatalf("MCP: %s", stderr.String())
	}
	for _, expected := range []string{"soha.tasks.resume", "soha.plans.validate", "expectedVersion", "structuredContent", "task-1", "blocked"} {
		if !strings.Contains(output, expected) {
			t.Fatalf("MCP omitted %s: %s", expected, output)
		}
	}
}
