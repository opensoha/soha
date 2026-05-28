package runner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
printf '%s' "$*" | grep -- '-s soha-root-cause' >/dev/null || { echo "missing provider skill" >&2; exit 5; }
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
	if !strings.Contains(string(args), "-s soha-root-cause") {
		t.Fatalf("expected provider skill arg in command args, got %q", args)
	}
}

func TestExecuteHermesAgentRunFallsBackToSohaSkillIDWhenProviderSkillRefMissing(t *testing.T) {
	root := t.TempDir()
	commandPath := filepath.Join(root, "fake-hermes")
	command := `#!/bin/sh
printf '%s' "$*" > command.args
printf '%s' "$*" | grep -- '-s root-cause-investigation' >/dev/null || { echo "missing fallback skill" >&2; exit 2; }
cat <<'JSON'
{"summary":"Hermes accepted fallback skill id."}
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
		ID:           "agent-run-fallback-skill",
		ProviderID:   "hermes",
		ProviderKind: "hermes",
		CapabilityID: "root_cause",
		SkillIDs:     []string{"root-cause-investigation"},
		Input:        map[string]any{"question": "Investigate alert alert-1"},
	})
	if err != nil {
		t.Fatalf("executeHermesAgentRun() error = %v logs=%v", err, logs)
	}
	if output["summary"] != "Hermes accepted fallback skill id." {
		t.Fatalf("summary = %#v", output["summary"])
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

func TestAgentRunClaimExecuteToolCallAndCallbackSmoke(t *testing.T) {
	root := t.TempDir()
	commandPath := filepath.Join(root, "fake-hermes")
	command := `#!/bin/sh
printf '%s' "$*" > command.args
printf '%s' "$*" | grep 'soha.agentRuntime.v1' >/dev/null || { echo "missing contract" >&2; exit 2; }
printf '%s' "$*" | grep 'prefetchedToolResults' >/dev/null || { echo "missing prefetched tool context" >&2; exit 3; }
printf '%s' "$*" | grep 'CrashLoopBackOff' >/dev/null || { echo "missing platform evidence" >&2; exit 4; }
cat <<'JSON'
{"summary":"Hermes POC found a pod restart regression.","recommendations":["Inspect payment-api pod logs"]}
JSON
`
	if err := os.WriteFile(commandPath, []byte(command), 0o755); err != nil {
		t.Fatalf("write fake hermes: %v", err)
	}

	var claimSeen bool
	var toolCallSeen bool
	var runningCallbackSeen bool
	var completedCallbackSeen bool
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.Header.Get("Authorization"); got != "Bearer runner-token" {
			t.Fatalf("unexpected authorization header %q for %s", got, r.URL.Path)
		}
		status := http.StatusAccepted
		body := map[string]any{}
		switch r.URL.Path {
		case "/api/v1/copilot/agent-runs/claim":
			claimSeen = true
			var req agentRunClaimRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode claim request: %v", err)
			}
			if req.AgentID != "smoke-hermes-runner" || len(req.ProviderIDs) != 1 || req.ProviderIDs[0] != "hermes" || len(req.Kinds) != 1 || req.Kinds[0] != "hermes" {
				t.Fatalf("unexpected claim request: %#v", req)
			}
			body = map[string]any{
				"data": map[string]any{
					"id":            "agent-run-smoke",
					"providerId":    "hermes",
					"providerKind":  "hermes",
					"capabilityId":  "root_cause",
					"skillIds":      []string{"root-cause-investigation"},
					"status":        "running",
					"callbackToken": "callback-token",
					"scope":         map[string]any{"clusterId": "cluster-a", "namespace": "payments", "workload": "payment-api"},
					"input":         map[string]any{"question": "Investigate payment-api restart"},
					"toolBindings": []map[string]any{{
						"id":        "platform.events",
						"adapterId": "platform-native.v1",
						"toolName":  "events.query",
					}},
					"skillBindings": []map[string]any{{
						"id":               "skill.root-cause.hermes",
						"skillId":          "root-cause-investigation",
						"providerSkillRef": "soha-root-cause",
					}},
					"timeoutSeconds": 30,
				},
			}
		case "/api/v1/copilot/agent-runs/tool-call":
			toolCallSeen = true
			var req agentRunToolCallRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode tool-call request: %v", err)
			}
			if req.RunID != "agent-run-smoke" || req.CallbackToken != "callback-token" || req.AgentID != "smoke-hermes-runner" {
				t.Fatalf("unexpected tool-call identity: %#v", req)
			}
			if req.ToolBindingID != "platform.events" || req.ToolName != "events.query" {
				t.Fatalf("unexpected tool-call binding: %#v", req)
			}
			body = map[string]any{
				"data": map[string]any{
					"runId": "agent-run-smoke",
					"toolExecution": map[string]any{
						"id":       "tool:events",
						"toolName": "events.query",
						"status":   "success",
						"summary":  "found restart warning",
					},
					"output": map[string]any{
						"events": []map[string]any{{
							"summary": "payment-api pod entered CrashLoopBackOff",
						}},
					},
				},
			}
		case "/api/v1/copilot/agent-runs/callback":
			var req agentRunCallbackRequest
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Fatalf("decode callback request: %v", err)
			}
			if req.RunID != "agent-run-smoke" || req.CallbackToken != "callback-token" || req.AgentID != "smoke-hermes-runner" {
				t.Fatalf("unexpected callback identity: %#v", req)
			}
			switch req.Status {
			case "running":
				runningCallbackSeen = true
			case "completed":
				completedCallbackSeen = true
				if req.ExternalRunID != "agent-run-smoke" {
					t.Fatalf("unexpected external run id %q", req.ExternalRunID)
				}
				if len(req.ToolExecutions) != 1 || len(req.AnalysisArtifacts) != 1 {
					t.Fatalf("expected final tool execution and artifact, got tools=%#v artifacts=%#v", req.ToolExecutions, req.AnalysisArtifacts)
				}
				if req.AnalysisArtifacts[0]["summary"] != "Hermes POC found a pod restart regression." {
					t.Fatalf("unexpected artifact summary: %#v", req.AnalysisArtifacts[0])
				}
			default:
				t.Fatalf("unexpected callback status %q", req.Status)
			}
			body = map[string]any{"data": map[string]any{"id": "agent-run-smoke"}}
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		responseBody, _ := json.Marshal(body)
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(string(responseBody))),
		}, nil
	})

	workspaceRoot := filepath.Join(root, "workspace")
	runner := New(cfgpkg.ControlPlaneConfig{
		BaseURL:     "http://control-plane",
		BearerToken: "runner-token",
		AgentRuntime: cfgpkg.AgentRuntimeConfig{
			WorkerID:      "smoke-hermes-runner",
			ProviderIDs:   []string{"hermes"},
			ProviderKinds: []string{"hermes"},
			HermesCommand: commandPath,
			WorkspaceRoot: workspaceRoot,
		},
	}, zap.NewNop())
	runner.httpClient = &http.Client{Transport: transport}

	run, ok := runner.claimAgentRun(context.Background())
	if !ok {
		t.Fatalf("expected smoke runner to claim agent run")
	}
	runner.executeAgentRun(context.Background(), run)

	if !claimSeen || !toolCallSeen || !runningCallbackSeen || !completedCallbackSeen {
		t.Fatalf("expected claim/tool-call/running/completed flow, got claim=%v tool=%v running=%v completed=%v", claimSeen, toolCallSeen, runningCallbackSeen, completedCallbackSeen)
	}
	args, err := os.ReadFile(filepath.Join(workspaceRoot, "agent-run-smoke", "command.args"))
	if err != nil {
		t.Fatalf("read fake hermes args: %v", err)
	}
	if !strings.Contains(string(args), "prefetchedToolResults") || !strings.Contains(string(args), "soha-root-cause") {
		t.Fatalf("expected prefetched context and skill binding in command args, got %q", args)
	}
}

func TestAgentRunArtifactPreservesStructuredProviderOutput(t *testing.T) {
	startedAt := time.Date(2026, 5, 28, 10, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(time.Minute)
	run := AgentRun{
		ID:           "agent-run-artifact",
		ProviderID:   "hermes",
		ProviderKind: "hermes",
		CapabilityID: "root_cause",
		SkillIDs:     []string{"root-cause-investigation"},
		ToolBindings: []map[string]any{{"id": "observability.logs", "toolName": "logs.query"}},
		SkillBindings: []map[string]any{{
			"id":               "skill.root-cause.hermes",
			"providerSkillRef": "soha-root-cause",
		}},
	}
	output := map[string]any{
		"summary": "Hermes found a failing deployment.",
		"evidence": []map[string]any{{
			"id":      "evidence:1",
			"kind":    "event",
			"title":   "Pod restarted",
			"summary": "payment-api restarted repeatedly",
		}},
		"hypotheses": []map[string]any{{
			"id":          "hypothesis:1",
			"title":       "Bad rollout",
			"summary":     "New image is unhealthy",
			"confidence":  0.8,
			"evidenceIds": []any{"evidence:1"},
		}},
		"recommendations": []any{"Rollback deployment"},
		"toolExecutions": []map[string]any{{
			"id":       "provider-tool:1",
			"toolName": "hermes.native.lookup",
			"status":   "success",
		}},
		"graph": map[string]any{
			"layout":      "LR",
			"focusNodeId": "deploy/payment-api",
			"nodes":       []map[string]any{{"id": "deploy/payment-api", "kind": "deployment", "title": "payment-api"}},
			"edges":       []map[string]any{},
		},
		"dataSourceSnapshot": map[string]any{
			"providerTraceId": "trace-1",
			"providerId":      "custom-provider-value",
		},
	}
	runnerTool := agentRunToolExecution(run, startedAt, completedAt, "completed", "runner command completed", output)

	artifact := agentRunArtifact(run, output, []string{"provider output"}, []map[string]any{runnerTool})
	if artifact["summary"] != "Hermes found a failing deployment." {
		t.Fatalf("unexpected artifact summary: %#v", artifact)
	}
	if len(valueAsMapSlice(artifact["evidence"])) != 1 || len(valueAsMapSlice(artifact["hypotheses"])) != 1 {
		t.Fatalf("expected evidence and hypotheses to be preserved, got %#v", artifact)
	}
	if graph := mapValue(artifact["graph"]); graph["focusNodeId"] != "deploy/payment-api" {
		t.Fatalf("expected graph to be preserved, got %#v", artifact["graph"])
	}
	tools := valueAsMapSlice(artifact["toolExecutions"])
	if len(tools) != 2 {
		t.Fatalf("expected provider and runner tool executions, got %#v", tools)
	}
	snapshot := mapValue(artifact["dataSourceSnapshot"])
	if snapshot["providerTraceId"] != "trace-1" || snapshot["providerId"] != "custom-provider-value" || snapshot["capabilityId"] != "root_cause" {
		t.Fatalf("expected provider snapshot plus runner metadata, got %#v", snapshot)
	}
}

func TestExecuteAgentRunStreamsHeartbeatsDuringLongProviderCommand(t *testing.T) {
	root := t.TempDir()
	commandPath := filepath.Join(root, "slow-hermes")
	command := `#!/bin/sh
sleep 1
cat <<'JSON'
{"summary":"slow Hermes completed after heartbeat"}
JSON
`
	if err := os.WriteFile(commandPath, []byte(command), 0o755); err != nil {
		t.Fatalf("write slow hermes: %v", err)
	}
	previousInterval := agentRunHeartbeatInterval
	agentRunHeartbeatInterval = 20 * time.Millisecond
	defer func() { agentRunHeartbeatInterval = previousInterval }()

	var runningCallbacks int32
	var completedCallbacks int32
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/copilot/agent-runs/callback" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var req agentRunCallbackRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode callback request: %v", err)
		}
		switch req.Status {
		case "running":
			atomic.AddInt32(&runningCallbacks, 1)
		case "completed":
			atomic.AddInt32(&completedCallbacks, 1)
		default:
			t.Fatalf("unexpected callback status %q", req.Status)
		}
		responseBody, _ := json.Marshal(map[string]any{"data": map[string]any{"id": "agent-run-heartbeat", "status": "running"}})
		return &http.Response{
			StatusCode: http.StatusAccepted,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(string(responseBody))),
		}, nil
	})
	runner := New(cfgpkg.ControlPlaneConfig{
		BaseURL:     "http://control-plane",
		BearerToken: "runner-token",
		AgentRuntime: cfgpkg.AgentRuntimeConfig{
			WorkerID:      "heartbeat-runner",
			HermesCommand: commandPath,
			WorkspaceRoot: filepath.Join(root, "workspace"),
		},
	}, zap.NewNop())
	runner.httpClient = &http.Client{Transport: transport}

	runner.executeAgentRun(context.Background(), AgentRun{
		ID:             "agent-run-heartbeat",
		ProviderID:     "hermes",
		ProviderKind:   "hermes",
		CapabilityID:   "root_cause",
		CallbackToken:  "callback-token",
		TimeoutSeconds: 5,
	})

	if atomic.LoadInt32(&runningCallbacks) < 2 {
		t.Fatalf("expected initial and streamed running callbacks, got %d", runningCallbacks)
	}
	if atomic.LoadInt32(&completedCallbacks) != 1 {
		t.Fatalf("expected completed callback, got %d", completedCallbacks)
	}
}

func TestExecuteAgentRunStopsWhenControlPlaneReturnsTerminalStatus(t *testing.T) {
	root := t.TempDir()
	commandPath := filepath.Join(root, "cancelable-hermes")
	command := `#!/bin/sh
sleep 5
echo '{"summary":"should not complete"}'
`
	if err := os.WriteFile(commandPath, []byte(command), 0o755); err != nil {
		t.Fatalf("write cancelable hermes: %v", err)
	}
	previousInterval := agentRunHeartbeatInterval
	agentRunHeartbeatInterval = 20 * time.Millisecond
	defer func() { agentRunHeartbeatInterval = previousInterval }()

	var runningCallbacks int32
	var completedCallbacks int32
	var failedCallbacks int32
	transport := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/api/v1/copilot/agent-runs/callback" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		var req agentRunCallbackRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode callback request: %v", err)
		}
		status := "running"
		switch req.Status {
		case "running":
			if atomic.AddInt32(&runningCallbacks, 1) >= 2 {
				status = "canceled"
			}
		case "completed":
			atomic.AddInt32(&completedCallbacks, 1)
		case "failed", "callback_timeout":
			atomic.AddInt32(&failedCallbacks, 1)
		default:
			t.Fatalf("unexpected callback status %q", req.Status)
		}
		responseBody, _ := json.Marshal(map[string]any{"data": map[string]any{"id": "agent-run-cancel", "status": status}})
		return &http.Response{
			StatusCode: http.StatusAccepted,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(string(responseBody))),
		}, nil
	})
	runner := New(cfgpkg.ControlPlaneConfig{
		BaseURL:     "http://control-plane",
		BearerToken: "runner-token",
		AgentRuntime: cfgpkg.AgentRuntimeConfig{
			WorkerID:      "cancel-runner",
			HermesCommand: commandPath,
			WorkspaceRoot: filepath.Join(root, "workspace"),
		},
	}, zap.NewNop())
	runner.httpClient = &http.Client{Transport: transport}

	startedAt := time.Now()
	runner.executeAgentRun(context.Background(), AgentRun{
		ID:             "agent-run-cancel",
		ProviderID:     "hermes",
		ProviderKind:   "hermes",
		CapabilityID:   "root_cause",
		CallbackToken:  "callback-token",
		TimeoutSeconds: 10,
	})
	if elapsed := time.Since(startedAt); elapsed > time.Second {
		t.Fatalf("expected terminal control-plane status to cancel provider quickly, elapsed=%s", elapsed)
	}
	if atomic.LoadInt32(&completedCallbacks) != 0 || atomic.LoadInt32(&failedCallbacks) != 0 {
		t.Fatalf("expected no final callback after remote terminal status, completed=%d failed=%d", completedCallbacks, failedCallbacks)
	}
	if atomic.LoadInt32(&runningCallbacks) < 2 {
		t.Fatalf("expected streamed heartbeat before cancellation, got %d", runningCallbacks)
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
		"delivery.execution_tasks.list",
		"platform.resources.snapshot",
		"docker.operations.list",
		"docker.services.list",
		"virtualization.operations.list",
		"alerts.list",
		"oncall.routes.resolve",
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
