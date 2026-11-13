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

func TestInspectionCommandsUseRegisteredReceipts(t *testing.T) {
	for _, action := range []string{"list", "get", "create", "update", "delete", "run", "runs"} {
		t.Run(action, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				expected := "/api/v1/copilot/inspection-tasks"
				switch action {
				case "get", "update", "delete":
					expected += "/inspect-1"
				case "run":
					expected += "/inspect-1/execute"
				case "runs":
					expected = "/api/v1/copilot/inspection-runs"
				}
				if r.URL.Path != expected {
					t.Errorf("path=%s", r.URL.Path)
				}
				if action == "run" && (r.URL.Query().Get("idempotencyKey") != "same-request" || r.URL.Query().Get("expectedRevision") != "3") {
					t.Error("run identity lost")
				}
				if action == "runs" && r.URL.Query().Get("taskId") != "inspect-1" {
					t.Error("run filter lost")
				}
				if action == "create" || action == "update" {
					var value map[string]any
					if json.NewDecoder(r.Body).Decode(&value) != nil || value["id"] != "inspect-1" || value["expectedRevision"] != float64(3) {
						t.Error("registration lost")
					}
				}
				if action == "delete" {
					w.WriteHeader(http.StatusNoContent)
					return
				}
				if action == "list" || action == "runs" {
					writeJSON(t, w, map[string]any{"items": []any{}})
					return
				}
				writeJSON(t, w, map[string]any{"data": map[string]any{"id": "receipt-1", "status": "handed_off", "report": map[string]any{"capabilityTaskId": "goal-1"}}})
			}))
			defer server.Close()
			args := []string{"ai", "inspection", action}
			if action != "list" && action != "create" {
				args = append(args, "inspect-1")
			}
			if action == "create" || action == "update" {
				args = append(args, "--input-json", `{"id":"inspect-1","title":"Goal","scopeType":"platform","enabled":false,"intervalMinutes":5,"expectedRevision":3}`)
			}
			if action == "run" {
				args = append(args, "--idempotency-key", "same-request", "--expected-revision", "3")
			}
			args = append(args, "--yes", "--profile", "dev")
			var out, stderr bytes.Buffer
			code := Run(context.Background(), args, Runtime{Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
			if code != 0 || calls != 1 {
				t.Fatalf("code=%d calls=%d error=%s", code, calls, stderr.String())
			}
		})
	}
}

func TestInspectionMCPRunsReturnCrossClientGoalReference(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/ai-gateway/capabilities":
			writeJSON(t, w, map[string]any{"data": map[string]any{"tools": []any{}, "resources": []any{}, "prompts": []any{}}})
		case "/api/v1/copilot/inspection-tasks/inspect-1/execute":
			if r.URL.Query().Get("idempotencyKey") != "same-request" || r.URL.Query().Get("expectedRevision") != "3" {
				t.Error("MCP execution key lost")
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"id": "receipt-1", "status": "handed_off", "report": map[string]any{"capabilityTaskId": "original-goal"}}})
		default:
			t.Errorf("unexpected backend %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":0,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"inspection-test","version":"1"}}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"soha.inspections.run","arguments":{"taskId":"inspect-1","idempotencyKey":"same-request","expectedRevision":3}}}`,
		"",
	}, "\n")
	stdio := newMCPTestIO(input, 3)
	var stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	code := Run(ctx, []string{"mcp", "--profile", "dev", "--base-url", server.URL}, Runtime{In: stdio, Out: stdio, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
	if code != 0 || !strings.Contains(stdio.String(), "original-goal") || !strings.Contains(stdio.String(), "structuredContent") {
		t.Fatalf("MCP code=%d output=%s error=%s", code, stdio.String(), stderr.String())
	}
}
