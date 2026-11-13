package sohacli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestKnowledgeConnectorCommandsUseProtectedEndpointsAndRedactConfigRef(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assertAICommandHeaders(t, r, "knowledge-connector-operator")
		switch calls {
		case 1:
			if r.Method != http.MethodPost || r.URL.Path != "/api/v1/ai/knowledge/connectors" {
				t.Fatalf("unexpected create request %s %s", r.Method, r.URL.Path)
			}
			var input KnowledgeConnectorInput
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			allowedHosts, _ := input.Config["allowedHosts"].([]any)
			if input.KnowledgeBaseID != "runbooks" || input.Name != "runbook-git" || input.Kind != "git" || input.SecretRef != "secret:knowledge/git" || len(allowedHosts) != 1 || input.SyncPolicy.Mode != "manual" {
				t.Fatalf("connector input = %#v", input)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"id": "connector-1", "name": input.Name, "status": "created", "secretRef": input.SecretRef}})
		case 2:
			if r.Method != http.MethodPost || r.URL.Path != "/api/v1/ai/knowledge/connectors/runbook-git/validate" {
				t.Fatalf("unexpected validate request %s %s", r.Method, r.URL.Path)
			}
			writeJSON(t, w, map[string]any{"data": map[string]any{"id": "runbook-git", "status": "valid"}})
		default:
			t.Fatalf("unexpected call %d", calls)
		}
	}))
	defer server.Close()
	configPath := writeAICommandProfile(t, server.URL)

	var out bytes.Buffer
	code := Run(context.Background(), []string{
		"knowledge", "connectors", "create", "--profile", "dev", "--base-id", "runbooks", "--name", "runbook-git", "--kind", "git",
		"--version", "v1", "--config-ref", "secret:knowledge/git", "--config-json", `{"repositoryUrl":"https://github.com/opensoha/docs","branch":"main","depth":1,"maxBytes":1048576}`, "--allowed-hosts", "github.com",
	}, Runtime{Out: &out, Err: &bytes.Buffer{}, ConfigPath: configPath})
	if code != 0 {
		t.Fatalf("connector create code = %d, output = %q", code, out.String())
	}
	if strings.Contains(out.String(), "secret:knowledge/git") {
		t.Fatalf("connector summary leaked configRef: %q", out.String())
	}

	out.Reset()
	code = Run(context.Background(), []string{"knowledge", "connectors", "validate", "--profile", "dev", "runbook-git"}, Runtime{Out: &out, Err: &bytes.Buffer{}, ConfigPath: configPath})
	if code != 0 || calls != 2 || !strings.Contains(out.String(), "status=valid") {
		t.Fatalf("connector validate code = %d, calls = %d, output = %q", code, calls, out.String())
	}
}

func TestKnowledgeSyncAndRebuildCommandsUseActionEndpoints(t *testing.T) {
	expected := []struct {
		method string
		path   string
	}{
		{http.MethodPost, "/api/v1/ai/knowledge-bases/runbooks/sync-jobs"},
		{http.MethodGet, "/api/v1/ai/knowledge/sync-jobs/sync-1"},
		{http.MethodPost, "/api/v1/ai/knowledge/sync-jobs/sync-1/cancel"},
		{http.MethodPost, "/api/v1/ai/knowledge/sync-jobs/sync-1/retry"},
		{http.MethodPost, "/api/v1/ai/knowledge-bases/runbooks/rebuild"},
	}
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls >= len(expected) {
			t.Fatalf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		want := expected[calls]
		calls++
		if r.Method != want.method || r.URL.Path != want.path {
			t.Fatalf("request %d = %s %s, want %s %s", calls, r.Method, r.URL.Path, want.method, want.path)
		}
		writeJSON(t, w, map[string]any{"data": map[string]any{"id": "sync-1", "status": "accepted"}})
	}))
	defer server.Close()
	configPath := writeAICommandProfile(t, server.URL)
	commands := [][]string{
		{"knowledge", "sync", "start", "--profile", "dev", "--base-id", "runbooks", "--source-id", "source-1"},
		{"knowledge", "sync", "status", "--profile", "dev", "sync-1"},
		{"knowledge", "sync", "cancel", "--profile", "dev", "sync-1"},
		{"knowledge", "sync", "retry", "--profile", "dev", "sync-1"},
		{"knowledge", "rebuild", "--profile", "dev", "--base-id", "runbooks", "--reason", "model update"},
	}
	for _, command := range commands {
		var errOut bytes.Buffer
		if code := Run(context.Background(), command, Runtime{Out: &bytes.Buffer{}, Err: &errOut, ConfigPath: configPath}); code != 0 {
			t.Fatalf("Run(%v) code = %d, stderr = %q", command, code, errOut.String())
		}
	}
	if calls != len(expected) {
		t.Fatalf("calls = %d, want %d", calls, len(expected))
	}
}

func TestAIEvaluationCommandsUseBoundedExecutionReplayAndGateEndpoints(t *testing.T) {
	expectedPaths := []string{
		"/api/v1/ai/evaluations/runs/eval-1/execute",
		"/api/v1/ai/evaluations/replays",
		"/api/v1/ai/evaluations/gates/evaluate",
	}
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assertAICommandHeaders(t, r, "evaluation-release-gate-reviewer")
		if r.Method != http.MethodPost || calls >= len(expectedPaths) || r.URL.Path != expectedPaths[calls] {
			t.Fatalf("request %d = %s %s", calls, r.Method, r.URL.Path)
		}
		calls++
		switch r.URL.Path {
		case "/api/v1/ai/evaluations/runs/eval-1/execute":
			var input EvaluationExecuteInput
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input.ExecutorProfileID != "executor-1" {
				t.Fatalf("execute input = %#v", input)
			}
		case "/api/v1/ai/evaluations/replays":
			var input EvaluationReplayInput
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input.ID != "replay-1" || input.BaselineRunID != "baseline-1" || input.CandidateRunID != "candidate-1" || input.ExecutorProfileID != "executor-1" {
				t.Fatalf("replay input = %#v", input)
			}
		case "/api/v1/ai/evaluations/gates/evaluate":
			var input EvaluationGateInput
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			if input.PolicyID != "gate-1" || input.BaselineRunID != "baseline-1" || input.CandidateRunID != "candidate-1" {
				t.Fatalf("gate input = %#v", input)
			}
		}
		writeJSON(t, w, map[string]any{"data": map[string]any{"id": "operation-1", "status": "accepted"}})
	}))
	defer server.Close()
	configPath := writeAICommandProfile(t, server.URL)
	commands := [][]string{
		{"ai", "evaluation", "run", "--profile", "dev", "--executor-profile-id", "executor-1", "eval-1"},
		{"ai", "evaluation", "replay", "--profile", "dev", "--id", "replay-1", "--baseline-run-id", "baseline-1", "--candidate-run-id", "candidate-1", "--executor-profile-id", "executor-1"},
		{"ai", "evaluation", "gate", "--profile", "dev", "--policy-id", "gate-1", "--baseline-run-id", "baseline-1", "--candidate-run-id", "candidate-1"},
	}
	for _, command := range commands {
		var errOut bytes.Buffer
		if code := Run(context.Background(), command, Runtime{Out: &bytes.Buffer{}, Err: &errOut, ConfigPath: configPath}); code != 0 {
			t.Fatalf("Run(%v) code = %d, stderr = %q", command, code, errOut.String())
		}
	}
	if calls != len(expectedPaths) {
		t.Fatalf("calls = %d, want %d", calls, len(expectedPaths))
	}
}

func TestAIEvaluationLegacyFlagsMapToFinalWireFields(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		for _, obsolete := range []string{"sourceTraceRefs", "candidateRefs", "readOnly", "evaluationRunId", "candidateRef"} {
			if _, exists := body[obsolete]; exists {
				t.Fatalf("request body contains obsolete field %q: %#v", obsolete, body)
			}
		}
		switch calls {
		case 1:
			id, ok := body["id"].(string)
			if r.URL.Path != "/api/v1/ai/evaluations/replays" || body["baselineRunId"] != "baseline-legacy" || body["candidateRunId"] != "candidate-legacy" || body["executorProfileId"] != "executor-1" || !ok || !strings.HasPrefix(id, "replay-") {
				t.Fatalf("legacy replay request = %s %#v", r.URL.Path, body)
			}
		case 2:
			if r.URL.Path != "/api/v1/ai/evaluations/gates/evaluate" || body["policyId"] != "gate-1" || body["baselineRunId"] != "baseline-legacy" || body["candidateRunId"] != "candidate-legacy" {
				t.Fatalf("legacy gate request = %s %#v", r.URL.Path, body)
			}
		default:
			t.Fatalf("unexpected call %d", calls)
		}
		writeJSON(t, w, map[string]any{"data": map[string]any{"id": "operation-1", "status": "accepted"}})
	}))
	defer server.Close()
	configPath := writeAICommandProfile(t, server.URL)
	commands := [][]string{
		{"ai", "evaluation", "replay", "--profile", "dev", "--source-trace-refs", "baseline-legacy", "--candidate-refs-json", `{"evaluationRun":"candidate-legacy"}`, "--executor-profile-id", "executor-1"},
		{"ai", "evaluation", "gate", "--profile", "dev", "--policy-id", "gate-1", "--run-id", "baseline-legacy", "--candidate-ref", "candidate-legacy"},
	}
	for _, command := range commands {
		var errOut bytes.Buffer
		if code := Run(context.Background(), command, Runtime{Out: &bytes.Buffer{}, Err: &errOut, ConfigPath: configPath}); code != 0 {
			t.Fatalf("Run(%v) code = %d, stderr = %q", command, code, errOut.String())
		}
	}
}

func TestAIMemoryInspectAndDeleteUseGovernedEndpoints(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		assertAICommandHeaders(t, r, "memory-privacy-curator")
		switch calls {
		case 1:
			if r.Method != http.MethodGet || r.URL.Path != "/api/v1/ai/memory" || r.URL.Query().Get("ownerId") != "user-1" || r.URL.Query().Get("ownerType") != "user" || r.URL.Query().Has("limit") {
				t.Fatalf("inspect request = %s %s", r.Method, r.URL.String())
			}
			writeJSON(t, w, map[string]any{"items": []map[string]any{{"id": "memory-1", "kind": "user", "status": "active"}}})
		case 2:
			if r.Method != http.MethodDelete || r.URL.Path != "/api/v1/ai/memory/memory-1" {
				t.Fatalf("delete request = %s %s", r.Method, r.URL.Path)
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected call %d", calls)
		}
	}))
	defer server.Close()
	configPath := writeAICommandProfile(t, server.URL)

	var out bytes.Buffer
	if code := Run(context.Background(), []string{"ai", "memory", "inspect", "--profile", "dev", "--subject-id", "user-1", "--limit", "25"}, Runtime{Out: &out, Err: &bytes.Buffer{}, ConfigPath: configPath}); code != 0 {
		t.Fatalf("memory inspect code = %d", code)
	}
	if !strings.Contains(out.String(), "id=memory-1") {
		t.Fatalf("memory inspect output = %q", out.String())
	}
	out.Reset()
	if code := Run(context.Background(), []string{"ai", "memory", "delete", "--profile", "dev", "memory-1"}, Runtime{Out: &out, Err: &bytes.Buffer{}, ConfigPath: configPath}); code != 0 {
		t.Fatalf("memory delete code = %d", code)
	}
	if calls != 2 || !strings.Contains(out.String(), "operation: memory-deleted") {
		t.Fatalf("memory delete calls = %d, output = %q", calls, out.String())
	}
}

func TestAICommandsRejectUnsafeInputsBeforeHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("server must not be called for invalid input")
	}))
	defer server.Close()
	configPath := writeAICommandProfile(t, server.URL)
	tests := [][]string{
		{"knowledge", "connectors", "create", "--profile", "dev", "--id", "git", "--kind", "git", "--version", "v1", "--config-ref", "plaintext-token"},
		{"ai", "evaluation", "replay", "--profile", "dev", "--baseline-run-id", "baseline-1", "--candidate-run-id", "candidate-1"},
		{"ai", "memory", "inspect", "--profile", "dev", "--limit", "201"},
	}
	for _, args := range tests {
		if code := Run(context.Background(), args, Runtime{Out: &bytes.Buffer{}, Err: &bytes.Buffer{}, ConfigPath: configPath}); code == 0 {
			t.Fatalf("Run(%v) unexpectedly succeeded", args)
		}
	}
}

func writeAICommandProfile(t *testing.T, serverURL string) string {
	t.Helper()
	return writeTestConfigWithProfile(t, ProfileConfig{
		ServerURL: serverURL, AccessToken: "access-token", AIClientID: "codex", AIClientName: "Codex", Source: "soha-cli",
	})
}

func assertAICommandHeaders(t *testing.T, request *http.Request, skillID string) {
	t.Helper()
	if request.Header.Get("Authorization") != "Bearer access-token" || request.Header.Get("X-Soha-Skill-ID") != skillID || request.Header.Get("X-Soha-Source") != "soha-cli" {
		t.Fatalf("unexpected headers: Authorization=%q skill=%q source=%q", request.Header.Get("Authorization"), request.Header.Get("X-Soha-Skill-ID"), request.Header.Get("X-Soha-Source"))
	}
}
