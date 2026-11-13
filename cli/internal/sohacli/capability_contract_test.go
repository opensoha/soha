package sohacli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func TestToolCallPinsDiscoveredVersionAndPreservesTask(t *testing.T) {
	for _, version := range []string{"", "1", "old"} {
		t.Run("requested-"+version, func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/v1/ai-gateway/capabilities" {
					writeJSON(t, w, map[string]any{"data": map[string]any{"tools": []map[string]any{{"name": "docker.operations.get", "version": "1", "riskLevel": "read"}}}})
					return
				}
				if r.URL.Path != "/api/v1/ai-gateway/tools/docker.operations.get/invoke" {
					t.Errorf("unexpected request: %s", r.URL.Path)
					http.NotFound(w, r)
					return
				}
				calls++
				var input sohaapi.ToolInvocationRequest
				if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.CapabilityVersion != "1" || input.Input["operationId"] != "operation-1" {
					t.Errorf("missing pinned call: %+v (%v)", input, err)
				}
				writeJSON(t, w, map[string]any{"data": map[string]any{
					"toolName": "docker.operations.get", "capabilityVersion": "1", "result": "success", "riskLevel": "read", "requiresApproval": false,
					"task": map[string]any{"kind": "docker.operation", "id": "operation-1", "status": "running", "terminal": false, "statusCall": map[string]any{"toolName": "docker.operations.get", "capabilityVersion": "1", "input": map[string]any{"operationId": "operation-1"}}},
				}})
			}))
			defer server.Close()
			args := []string{"tool", "call", "docker.operations.get", "--profile", "dev", "--input-json", `{"operationId":"operation-1"}`}
			if version != "" {
				args = append(args, "--capability-version", version)
			}
			var out, stderr bytes.Buffer
			code := Run(context.Background(), args, Runtime{Out: &out, Err: &stderr, ConfigPath: writeTestConfig(t, server.URL)})
			if version == "old" {
				if code == 0 || calls != 0 {
					t.Fatal("stale plan was executed")
				}
				return
			}
			var result ToolInvocationResult
			if code != 0 || calls != 1 || json.Unmarshal(out.Bytes(), &result) != nil || result.Task == nil || result.Task.ID != "operation-1" || result.Task.Terminal {
				t.Fatalf("task reference was lost: %s %s", out.String(), stderr.String())
			}
		})
	}
}

func TestMCPIdempotencyRequiresAnExplicitContract(t *testing.T) {
	for _, risk := range []string{"read", "analyze", "execute"} {
		tool := ToolCapability{Name: "test", RiskLevel: risk}
		if mcpToolAnnotations(tool)["idempotentHint"] != false {
			t.Fatalf("inferred idempotency from %s risk", risk)
		}
		tool.Execution = &sohaapi.ToolExecutionContract{Mode: "async", Idempotent: true, IdempotencyKeyField: "idempotencyKey"}
		if mcpToolAnnotations(tool)["idempotentHint"] != true {
			t.Fatal("explicit idempotency contract was ignored")
		}
	}
}

func TestInvocationSanitizerPreservesTaskWithoutLeakingArguments(t *testing.T) {
	input := ToolInvocationResult{
		Task:                 &sohaapi.CapabilityTaskRef{Kind: "reports.query", ID: "query-1", StatusCall: sohaapi.CapabilityCall{ToolName: "reports.query.get", Input: map[string]any{"id": "query-1", "password": "hidden-value"}}},
		AdditionalProperties: map[string]any{"newMetadata": "visible", "apiKey": "hidden-value"},
	}
	got := sanitizeAs(input)
	if got.Task == nil || got.Task.ID != "query-1" || got.Task.StatusCall.Input["password"] != "[REDACTED]" || got.AdditionalProperties["apiKey"] != "[REDACTED]" || got.AdditionalProperties["newMetadata"] != "visible" {
		t.Fatalf("result metadata was lost or unredacted: %+v", got)
	}
	if input.Task.StatusCall.Input["password"] != "hidden-value" {
		t.Fatal("sanitizer mutated the original task")
	}
}
