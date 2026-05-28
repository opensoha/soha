package runner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cfgpkg "github.com/soha/soha/internal/agent/config"
	"go.uber.org/zap"
)

func TestExecuteHermesAgentRunUsesConfiguredCommandSkillsAndParsesJSON(t *testing.T) {
	root := t.TempDir()
	commandPath := filepath.Join(root, "fake-hermes")
	command := `#!/bin/sh
printf '%s' "$*" > command.args
printf '%s' "$*" | grep 'soha.agentRuntime.v1' >/dev/null || { echo "missing contract" >&2; exit 2; }
printf '%s' "$*" | grep 'root_cause' >/dev/null || { echo "missing capability" >&2; exit 3; }
printf '%s' "$*" | grep 'cluster-a' >/dev/null || { echo "missing scope" >&2; exit 4; }
printf '%s' "$*" | grep -- '-s root-cause-investigation' >/dev/null || { echo "missing skill" >&2; exit 5; }
printf '%s' "$*" | grep 'logs.query' >/dev/null || { echo "missing tool binding" >&2; exit 6; }
printf '%s' "$*" | grep 'soha-root-cause' >/dev/null || { echo "missing skill binding" >&2; exit 7; }
cat <<'JSON'
{"summary":"Hermes identified a release regression.","recommendations":["Rollback release bundle"]}
JSON
`
	if err := os.WriteFile(commandPath, []byte(command), 0o755); err != nil {
		t.Fatalf("write fake hermes: %v", err)
	}
	workspaceRoot := filepath.Join(root, "workspace")
	runner := New(cfgpkg.ControlPlaneConfig{
		AgentRuntime: cfgpkg.AgentRuntimeConfig{
			HermesCommand: commandPath,
			WorkspaceRoot: workspaceRoot,
		},
	}, zap.NewNop())

	output, logs, err := runner.executeHermesAgentRun(context.Background(), AgentRun{
		ID:           "agent-run-1",
		ProviderID:   "hermes",
		ProviderKind: "hermes",
		CapabilityID: "root_cause",
		SkillIDs:     []string{"root-cause-investigation"},
		Scope:        map[string]any{"clusterId": "cluster-a", "namespace": "payments"},
		ToolBindings: []map[string]any{{
			"id":           "observability.logs",
			"capabilityId": "root_cause",
			"toolKind":     "mcp",
			"adapterId":    "logs.v1",
			"toolName":     "logs.query",
		}},
		SkillBindings: []map[string]any{{
			"id":               "skill.root-cause.hermes",
			"skillId":          "root-cause-investigation",
			"providerSkillRef": "soha-root-cause",
		}},
		Input: map[string]any{"question": "Investigate alert alert-1"},
	})
	if err != nil {
		t.Fatalf("executeHermesAgentRun() error = %v logs=%v", err, logs)
	}
	if output["summary"] != "Hermes identified a release regression." {
		t.Fatalf("summary = %#v", output["summary"])
	}
	recommendations := valueAsStringSlice(output["recommendations"])
	if len(recommendations) != 1 || recommendations[0] != "Rollback release bundle" {
		t.Fatalf("recommendations = %#v", output["recommendations"])
	}
	rawOutput := strings.TrimSpace(output["rawOutput"].(string))
	if !strings.Contains(rawOutput, "Hermes identified a release regression") {
		t.Fatalf("raw output = %q", rawOutput)
	}
	argsPath := filepath.Join(workspaceRoot, "agent-run-1", "command.args")
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read fake hermes args: %v", err)
	}
	if !strings.Contains(string(args), "-s root-cause-investigation") {
		t.Fatalf("expected skill arg in command args, got %q", args)
	}
}

func TestExecuteHermesAgentRunPrefetchesToolContextIntoPrompt(t *testing.T) {
	root := t.TempDir()
	commandPath := filepath.Join(root, "fake-hermes")
	command := `#!/bin/sh
printf '%s' "$*" > command.args
printf '%s' "$*" | grep 'prefetchedToolResults' >/dev/null || { echo "missing prefetched tool context" >&2; exit 2; }
printf '%s' "$*" | grep 'payment-api restart backoff' >/dev/null || { echo "missing event evidence" >&2; exit 3; }
cat <<'JSON'
{"summary":"Hermes used prefetched soha tool context."}
JSON
`
	if err := os.WriteFile(commandPath, []byte(command), 0o755); err != nil {
		t.Fatalf("write fake hermes: %v", err)
	}
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/copilot/agent-runs/tool-call" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		responseBody, _ := json.Marshal(map[string]any{
			"data": map[string]any{
				"runId": "agent-run-prefetch",
				"toolExecution": map[string]any{
					"id":       "tool:events",
					"toolName": "events.query",
					"status":   "success",
				},
				"output": map[string]any{
					"count":  1,
					"events": []map[string]any{{"summary": "payment-api restart backoff"}},
				},
			},
		})
		return &http.Response{
			StatusCode: http.StatusAccepted,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(string(responseBody))),
		}, nil
	})
	workspaceRoot := filepath.Join(root, "workspace")
	runner := New(cfgpkg.ControlPlaneConfig{
		BaseURL:     "http://control-plane",
		BearerToken: "runner-token",
		AgentRuntime: cfgpkg.AgentRuntimeConfig{
			HermesCommand: commandPath,
			WorkspaceRoot: workspaceRoot,
		},
	}, zap.NewNop())
	runner.httpClient = &http.Client{Transport: transport}

	output, logs, err := runner.executeHermesAgentRun(context.Background(), AgentRun{
		ID:            "agent-run-prefetch",
		ProviderID:    "hermes",
		ProviderKind:  "hermes",
		CapabilityID:  "root_cause",
		CallbackToken: "callback-token",
		ToolBindings: []map[string]any{{
			"id":        "platform.events",
			"adapterId": "platform-native.v1",
			"toolName":  "events.query",
		}},
		Input: map[string]any{"question": "Investigate payment-api"},
	})
	if err != nil {
		t.Fatalf("executeHermesAgentRun() error = %v logs=%v", err, logs)
	}
	if output["summary"] != "Hermes used prefetched soha tool context." {
		t.Fatalf("summary = %#v", output["summary"])
	}
	results := valueAsMapSlice(output["prefetchedToolResults"])
	if len(results) != 1 {
		t.Fatalf("expected prefetched tool result in output, got %#v", output["prefetchedToolResults"])
	}
}

func TestExecuteCLIAgentRunSupportsConfiguredProviderExecutor(t *testing.T) {
	root := t.TempDir()
	commandPath := filepath.Join(root, "fake-openclaw")
	command := `#!/bin/sh
printf '%s' "$*" > command.args
printf '%s' "$*" | grep 'soha.agentRuntime.v1' >/dev/null || { echo "missing contract" >&2; exit 2; }
printf '%s' "$*" | grep 'openclaw' >/dev/null || { echo "missing provider" >&2; exit 3; }
printf '%s' "$*" | grep -- '--prompt' >/dev/null || { echo "missing prompt arg" >&2; exit 4; }
printf '%s' "$*" | grep -- '--skill openclaw-root-cause' >/dev/null || { echo "missing provider skill arg" >&2; exit 5; }
cat <<'JSON'
{"summary":"OpenClaw executor accepted the soha contract."}
JSON
`
	if err := os.WriteFile(commandPath, []byte(command), 0o755); err != nil {
		t.Fatalf("write fake openclaw: %v", err)
	}
	workspaceRoot := filepath.Join(root, "workspace")
	runner := New(cfgpkg.ControlPlaneConfig{
		AgentRuntime: cfgpkg.AgentRuntimeConfig{
			WorkspaceRoot: workspaceRoot,
			Providers: map[string]cfgpkg.AgentProviderConfig{
				"openclaw": {
					Command:          commandPath,
					Args:             []string{"run"},
					PromptArg:        "--prompt",
					ProviderSkillArg: "--skill",
				},
			},
		},
	}, zap.NewNop())

	output, logs, err := runner.resolveAgentProviderExecutor(AgentRun{ProviderKind: "openclaw"})(context.Background(), AgentRun{
		ID:           "agent-run-openclaw",
		ProviderID:   "openclaw",
		ProviderKind: "openclaw",
		CapabilityID: "root_cause",
		SkillIDs:     []string{"root-cause-investigation"},
		Scope:        map[string]any{"clusterId": "cluster-a"},
		SkillBindings: []map[string]any{{
			"skillId":          "root-cause-investigation",
			"providerSkillRef": "openclaw-root-cause",
		}},
		Input: map[string]any{"question": "Investigate alert alert-1"},
	})
	if err != nil {
		t.Fatalf("execute configured provider() error = %v logs=%v", err, logs)
	}
	if output["summary"] != "OpenClaw executor accepted the soha contract." {
		t.Fatalf("summary = %#v", output["summary"])
	}
	argsPath := filepath.Join(workspaceRoot, "agent-run-openclaw", "command.args")
	args, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read fake openclaw args: %v", err)
	}
	if !strings.Contains(string(args), "--skill openclaw-root-cause") {
		t.Fatalf("expected provider skill arg in command args, got %q", args)
	}
}

func TestAgentRunToolCallPostsRunnerTokenAndRunToken(t *testing.T) {
	var request struct {
		RunID         string         `json:"runId"`
		CallbackToken string         `json:"callbackToken"`
		AgentID       string         `json:"agentId"`
		ToolBindingID string         `json:"toolBindingId"`
		AdapterID     string         `json:"adapterId"`
		ToolName      string         `json:"toolName"`
		Input         map[string]any `json:"input"`
	}
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/copilot/agent-runs/tool-call" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer runner-token" {
			t.Fatalf("unexpected authorization header %q", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		responseBody, _ := json.Marshal(map[string]any{
			"data": map[string]any{
				"runId": request.RunID,
				"toolExecution": map[string]any{
					"id":       "tool:1",
					"toolName": request.ToolName,
					"status":   "success",
				},
				"output": map[string]any{"count": 1},
			},
		})
		return &http.Response{
			StatusCode: http.StatusAccepted,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(string(responseBody))),
		}, nil
	})
	runner := New(cfgpkg.ControlPlaneConfig{
		BaseURL:     "http://control-plane",
		BearerToken: "runner-token",
		AgentID:     "local-agent",
		AgentRuntime: cfgpkg.AgentRuntimeConfig{
			WorkerID: "local-hermes-runner",
		},
	}, zap.NewNop())
	runner.httpClient = &http.Client{Transport: transport}

	result, ok := runner.agentRunToolCall(context.Background(), AgentRun{
		ID:            "agent-run-1",
		CallbackToken: "callback-token",
	}, map[string]any{
		"id":        "platform.events",
		"adapterId": "platform-native.v1",
		"toolName":  "events.query",
	}, map[string]any{"limit": 5})
	if !ok {
		t.Fatalf("expected tool call to succeed")
	}
	if request.RunID != "agent-run-1" || request.CallbackToken != "callback-token" || request.AgentID != "local-hermes-runner" {
		t.Fatalf("unexpected request identity: %#v", request)
	}
	if request.ToolBindingID != "platform.events" || request.AdapterID != "platform-native.v1" || request.ToolName != "events.query" {
		t.Fatalf("unexpected tool binding request: %#v", request)
	}
	if result.Output["count"] != float64(1) || result.ToolExecution["status"] != "success" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestAgentToolBindingPrefetchableIncludesBusinessContextTools(t *testing.T) {
	for _, toolName := range []string{
		"events.query",
		"logs.query",
		"metrics.query",
		"traces.query",
		"delivery.releases.list",
		"delivery.builds.list",
		"alerts.list",
	} {
		if !agentToolBindingPrefetchable(map[string]any{"toolName": toolName}) {
			t.Fatalf("expected %s to be prefetchable", toolName)
		}
	}
	if agentToolBindingPrefetchable(map[string]any{"toolName": "delivery.execution.start"}) {
		t.Fatalf("expected mutable delivery tool to remain non-prefetchable")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
