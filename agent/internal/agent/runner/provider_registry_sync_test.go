package runner

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	"go.uber.org/zap"
)

func TestSyncAgentProviderRegistryAppliesSnapshotAndAcknowledgesRevision(t *testing.T) {
	snapshot := AgentProviderRegistry{
		SchemaVersion: AgentProviderRegistrySchemaVersion,
		Revision:      2,
		IssuedAt:      time.Now().UTC(),
		Providers: []AgentProviderDefinition{{
			SchemaVersion:   AgentProviderDefinitionSchemaVersion,
			ID:              "codex",
			Kind:            "codex",
			DisplayName:     "Codex",
			PluginID:        "agent.codex",
			PluginVersion:   "1.0.0",
			ProviderVersion: "1.0.0",
			AdapterProtocol: "opensoha.agent-provider.cli/v1",
			Runtime: AgentProviderRuntime{
				Kind: "cli", Command: "codex", Args: []string{"exec"}, PromptArg: "-p",
			},
			Capabilities: []string{"delivery"},
		}},
	}
	var err error
	snapshot.Digest, err = agentProviderRegistryDigest(snapshot)
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var received providerRegistryAcknowledgement
	transport := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.Header.Get("Authorization") != "Bearer runner-token" {
			t.Fatalf("authorization = %q", req.Header.Get("Authorization"))
		}
		switch {
		case req.Method == http.MethodGet && req.URL.Path == "/api/v1/ai/agent-providers/registry-snapshot":
			if req.URL.Query().Get("runnerId") != "runner-1" {
				t.Fatalf("runnerId = %q", req.URL.Query().Get("runnerId"))
			}
			return jsonResponse(t, http.StatusOK, map[string]any{"data": snapshot}), nil
		case req.Method == http.MethodPost && req.URL.Path == "/api/v1/ai/agent-providers/registry-acks":
			if err := json.NewDecoder(req.Body).Decode(&received); err != nil {
				t.Fatal(err)
			}
			return jsonResponse(t, http.StatusAccepted, map[string]any{"data": received}), nil
		default:
			t.Fatalf("unexpected request %s %s", req.Method, req.URL.Path)
			return jsonResponse(t, http.StatusNotFound, map[string]any{}), nil
		}
	})
	runner := New(cfgpkg.ControlPlaneConfig{
		BaseURL:     "http://control-plane/api/v1",
		BearerToken: "runner-token",
		AgentID:     "runner-1",
	}, zap.NewNop())
	runner.SetAgentProviderConformanceProbe(conformanceProbeFunc(func(context.Context, AgentProviderDefinition) error { return nil }))
	runner.httpClient = &http.Client{Transport: transport}
	runner.syncAgentProviderRegistry(t.Context())

	mu.Lock()
	defer mu.Unlock()
	if received.RunnerID != "runner-1" || received.Revision != 2 || received.DesiredRevision != 2 || received.ActiveRevision != 2 || received.LKGRevision != 2 || received.PreviousRevision != 1 || !received.Accepted || !received.Targeted || received.RolloutState != "active" || len(received.ProviderStatuses) != 1 || len(received.ConformanceChecks) != 1 {
		t.Fatalf("acknowledgement = %#v", received)
	}
	active := runner.providerRegistry.Snapshot()
	if active.Revision != 2 || len(active.Providers) != 1 || active.Providers[0].ID != "codex" {
		t.Fatalf("active registry = %#v", active)
	}
}
