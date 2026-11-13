package runner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	sohaapi "github.com/opensoha/soha-contracts/gen/go/sohaapi"
)

func TestChatToolGrantBindsIdentityBudgetAndLifetime(t *testing.T) {
	runner := New(cfgpkg.ControlPlaneConfig{BaseURL: "http://control-plane", BearerToken: "runner-token", AgentID: "runner"}, nil)
	var calls atomic.Int32
	runner.httpClient = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		var input sohaapi.AgentRunToolCallRequest
		if err := json.NewDecoder(request.Body).Decode(&input); err != nil {
			t.Error(err)
		}
		if input.RunID != "run-a" || input.CallbackToken != "callback-a" || input.ToolBindingID != "knowledge" || request.Header.Get("Authorization") != "Bearer runner-token" {
			t.Error("tool call escaped its server-owned identity")
		}
		calls.Add(1)
		return &http.Response{StatusCode: http.StatusAccepted, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"data":{"runId":"run-a","toolExecution":{"status":"success"}}}`))}, nil
	})}
	run := AgentRun{ID: "run-a", CallbackToken: "callback-a", ToolBindings: []map[string]any{{"id": "knowledge", "toolName": "knowledge.search"}}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	key, release := runner.beginChatToolGrant(ctx, run)
	defer release()
	input := sohaapi.AgentRunnerToolCallRequest{ToolName: "knowledge.search", Input: sohaapi.AgentRunnerToolInput{Query: "maintenance"}}
	if _, err := runner.CallChatTool(ctx, "run-a", input); err == nil {
		t.Fatal("run ID must not authorize a tool")
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() { _, _ = runner.CallChatTool(ctx, key, input) })
	}
	wg.Wait()
	if calls.Load() != maxChatToolCalls {
		t.Fatalf("parallel calls bypassed budget: %d", calls.Load())
	}
	key, releaseNext := runner.beginChatToolGrant(ctx, run)
	releaseNext()
	if _, err := runner.CallChatTool(ctx, key, input); err == nil {
		t.Fatal("released grant remained usable")
	}
	key, releaseCanceled := runner.beginChatToolGrant(ctx, run)
	defer releaseCanceled()
	cancel()
	if _, err := runner.CallChatTool(context.Background(), key, input); err == nil {
		t.Fatal("canceled run remained usable")
	}
	if calls.Load() != maxChatToolCalls {
		t.Fatal("invalid grants reached control plane")
	}
}
