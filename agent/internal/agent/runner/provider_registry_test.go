package runner

import (
	"path/filepath"
	"testing"
	"time"

	cfgpkg "github.com/opensoha/soha-agent/internal/agent/config"
	"go.uber.org/zap"
)

func TestDefaultAgentProviderRegistryDeclaresHermesCapabilities(t *testing.T) {
	registry := DefaultAgentProviderRegistry()
	if err := validateAgentProviderRegistry(registry); err != nil {
		t.Fatalf("validateAgentProviderRegistry() error = %v", err)
	}
	if registry.SchemaVersion != AgentProviderRegistrySchemaVersion {
		t.Fatalf("SchemaVersion = %q, want %q", registry.SchemaVersion, AgentProviderRegistrySchemaVersion)
	}

	provider, ok := defaultAgentProviderDefinition("hermes")
	if !ok {
		t.Fatal("defaultAgentProviderDefinition(\"hermes\") not found")
	}
	if provider.ID != "hermes" || provider.Kind != "hermes" {
		t.Fatalf("provider identity = %#v", provider)
	}
	if provider.DefaultCommand.Command != "hermes" {
		t.Fatalf("default command = %#v, want hermes", provider.DefaultCommand)
	}

	capabilities := providerCapabilityIDs(provider)
	want := []string{"incident_handoff", "release_verification", "root_cause"}
	if len(capabilities) != len(want) {
		t.Fatalf("capabilities = %#v, want %#v", capabilities, want)
	}
	for index := range want {
		if capabilities[index] != want[index] {
			t.Fatalf("capabilities = %#v, want %#v", capabilities, want)
		}
	}
}

func TestDynamicAgentProviderRegistryRejectsInvalidSnapshotAndKeepsLastKnownGood(t *testing.T) {
	initial := DefaultAgentProviderRegistry()
	registry, err := NewDynamicAgentProviderRegistry(initial)
	if err != nil {
		t.Fatalf("NewDynamicAgentProviderRegistry() error = %v", err)
	}

	invalid := cloneAgentProviderRegistry(initial)
	invalid.Revision++
	invalid.Providers[0].Runtime.Command = "tampered"
	result := registry.Apply(invalid, time.Now())
	if result.Accepted || result.ActiveRevision != initial.Revision {
		t.Fatalf("Apply() = %#v, want NACK with revision %d", result, initial.Revision)
	}
	if got := registry.Snapshot(); got.Revision != initial.Revision || got.Providers[0].Runtime.Command != "hermes" {
		t.Fatalf("last-known-good changed after NACK: %#v", got)
	}
}

func TestDynamicAgentProviderRegistryDrainsWithoutInterruptingActiveRun(t *testing.T) {
	initial := DefaultAgentProviderRegistry()
	registry, err := NewDynamicAgentProviderRegistry(initial)
	if err != nil {
		t.Fatalf("NewDynamicAgentProviderRegistry() error = %v", err)
	}
	if _, err := registry.Acquire("hermes", "builtin-v1"); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	next := cloneAgentProviderRegistry(initial)
	next.Revision++
	next.Providers[0].Draining = true
	next.Digest, err = agentProviderRegistryDigest(next)
	if err != nil {
		t.Fatalf("agentProviderRegistryDigest() error = %v", err)
	}
	result := registry.Apply(next, time.Now())
	if !result.Accepted {
		t.Fatalf("Apply() = %#v, want ACK", result)
	}
	if _, err := registry.Acquire("hermes", "builtin-v1"); err == nil {
		t.Fatal("Acquire() succeeded for draining provider")
	}
	statuses := registry.Statuses()
	if len(statuses) != 1 || statuses[0].ActiveRuns != 1 || !statuses[0].Draining {
		t.Fatalf("Statuses() = %#v", statuses)
	}
	registry.Release("hermes")
	if got := registry.Statuses()[0].ActiveRuns; got != 0 {
		t.Fatalf("active runs after Release() = %d, want 0", got)
	}
}

func TestDynamicAgentProviderRegistryRejectsVersionReplacementUntilDrained(t *testing.T) {
	initial := DefaultAgentProviderRegistry()
	registry, err := NewDynamicAgentProviderRegistry(initial)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Acquire("hermes", "builtin-v1"); err != nil {
		t.Fatal(err)
	}
	replacement := cloneAgentProviderRegistry(initial)
	replacement.Revision++
	replacement.Providers[0].ProviderVersion = "builtin-v2"
	replacement.Digest, err = agentProviderRegistryDigest(replacement)
	if err != nil {
		t.Fatal(err)
	}
	result := registry.Apply(replacement, time.Now())
	if result.Accepted || result.ActiveRevision != initial.Revision {
		t.Fatalf("Apply() = %#v, want replacement NACK", result)
	}
	registry.Release("hermes")
	replacement.Revision++
	replacement.Digest, err = agentProviderRegistryDigest(replacement)
	if err != nil {
		t.Fatal(err)
	}
	if result := registry.Apply(replacement, time.Now()); !result.Accepted {
		t.Fatalf("Apply() after drain = %#v", result)
	}
}

func TestDynamicAgentProviderRegistryCopiesSnapshots(t *testing.T) {
	initial := DefaultAgentProviderRegistry()
	registry, err := NewDynamicAgentProviderRegistry(initial)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := registry.Snapshot()
	snapshot.Providers[0].DefaultCommand.Args[0] = "tampered"
	if got := registry.Snapshot().Providers[0].DefaultCommand.Args[0]; got != "chat" {
		t.Fatalf("registry leaked mutable snapshot, got %q", got)
	}
}

func TestDynamicAgentProviderRegistryAcceptsCurrentSnapshotIdempotently(t *testing.T) {
	initial := DefaultAgentProviderRegistry()
	registry, err := NewDynamicAgentProviderRegistry(initial)
	if err != nil {
		t.Fatal(err)
	}
	result := registry.Apply(cloneAgentProviderRegistry(initial), time.Now())
	if !result.Accepted || result.ActiveRevision != initial.Revision {
		t.Fatalf("Apply() = %#v, want idempotent ACK", result)
	}
}

func TestRunnerRejectsUnhealthyRegisteredProviderBeforeExecution(t *testing.T) {
	runner := New(cfgpkg.ControlPlaneConfig{}, zap.NewNop())
	if err := runner.providerRegistry.SetHealth("hermes", "unhealthy", "health check failed", time.Now()); err != nil {
		t.Fatal(err)
	}
	executor := runner.resolveAgentProviderExecutor(AgentRun{ProviderID: "hermes", ProviderKind: "hermes"})
	output, _, err := executor(t.Context(), AgentRun{ProviderID: "hermes", ProviderKind: "hermes"})
	if err == nil || output["provider"] != "hermes" {
		t.Fatalf("executor output=%#v error=%v", output, err)
	}
}

func TestAgentProviderCommandSpecUsesRegistryDefaultAndHermesOverride(t *testing.T) {
	customHermes := filepath.Join(t.TempDir(), "custom-hermes")
	runner := New(cfgpkg.ControlPlaneConfig{
		AgentRuntime: cfgpkg.AgentRuntimeConfig{
			HermesCommand: customHermes,
		},
	}, zap.NewNop())

	spec := runner.agentProviderCommandSpec("hermes")
	if spec.Command != customHermes {
		t.Fatalf("Command = %q, want %q", spec.Command, customHermes)
	}
	if len(spec.Args) != 2 || spec.Args[0] != "chat" || spec.Args[1] != "-Q" {
		t.Fatalf("Args = %#v, want chat -Q", spec.Args)
	}
	if spec.PromptArg != "-q" || spec.ProviderSkillArg != "-s" {
		t.Fatalf("prompt/provider skill args = %#v", spec)
	}
}
